package httpapi

// Тесты EFR-3.b и приемки EFR-3: GET /api/channels/{channelId}/ws?token=…
// через настоящий HTTP и клиент coder/websocket.
//
// Предполагаемое API (единственное место, которое надо поправить, если
// сигнатуры выйдут иначе, - newWSEnv и newWSServer):
//
//	func newHandler(logger, cfg config.Server, reg *channel.Registry, opts ...Option) http.Handler
//	func New(logger, cfg config.Server, reg *channel.Registry, opts ...Option) *Server
//	func WithGame(cfg config.Game) Option // GAME_WS_MAX_MESSAGE, GAME_WS_ALLOWED_ORIGINS
//
// Ошибки апгрейда (401, 403) - в модели huma, как любые ошибки HTTP (ADR-005):
// проверки идут до websocket.Accept.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/channel"
	"github.com/uair10/efir_backend/internal/infrastructure/config"
	"github.com/uair10/efir_backend/internal/ws"
)

const (
	wsWait            = 3 * time.Second
	testWSMaxMsgBytes = 256
	allowedOrigin     = "http://localhost:5173"
	forbiddenOrigin   = "http://evil.example"
)

// testGameConfig - настройки игры для тестов сокета.
func testGameConfig() config.Game {
	return config.Game{
		MaxChannels:       10,
		WSMaxMessageBytes: testWSMaxMsgBytes,
		WSAllowedOrigins:  []string{"localhost:5173"},
	}
}

// lockedBuffer - буфер для логов: пишут горутины сокетов и запросов одновременно.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// wsEnv - сервер с реестром и логом на уровне debug.
type wsEnv struct {
	handler http.Handler
	baseURL string // ws://127.0.0.1:port
	reg     *channel.Registry
	logs    *lockedBuffer
}

func newWSEnv(t *testing.T) *wsEnv {
	t.Helper()
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reg := channel.NewRegistry(10)
	h := newHandler(logger, testConfig(), reg, WithGame(testGameConfig()))

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	// Cleanup LIFO: сначала горутины каналов закрывают сокеты, потом сервер.
	t.Cleanup(reg.Close)
	return &wsEnv{handler: h, baseURL: "ws" + strings.TrimPrefix(srv.URL, "http"), reg: reg, logs: logs}
}

func (e *wsEnv) create(t *testing.T, mode channel.GameMode) channelResp {
	t.Helper()
	return mustCreate(t, e.handler, mode)
}

func (e *wsEnv) join(t *testing.T, code string) channelResp {
	t.Helper()
	return decodeChannel(t, joinChannel(t, e.handler, code), http.StatusOK)
}

func wsPath(channelID, token string) string {
	return "/api/channels/" + channelID + "/ws?token=" + url.QueryEscape(token)
}

// dial подключается с разрешенным Origin.
func (e *wsEnv) dial(t *testing.T, s channelResp) *websocket.Conn {
	t.Helper()
	return dialURL(t, e.baseURL+wsPath(s.ChannelID, s.Token))
}

func dialURL(t *testing.T, u string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wsWait)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {allowedOrigin}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err, "dial %s", redact(u))
	c.SetReadLimit(1 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

// redact убирает токен из URL для сообщений теста.
func redact(u string) string {
	if before, _, ok := strings.Cut(u, "token="); ok {
		return before + "token=***"
	}
	return u
}

// readRaw читает одно сообщение сервера.
func readRaw(t *testing.T, c *websocket.Conn) []byte {
	t.Helper()
	typ, b, err := readWatched(t, c)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageText, typ, "NFR-2: текстовый кадр")
	return b
}

// readLobby читает сообщение как channel.LobbyMessage. Формат JSON
// закрепляют TestWS_ConnectGetsLobby и TestWS_SoloLobby (строкой из контракта).
func readLobby(t *testing.T, c *websocket.Conn) channel.LobbyState {
	t.Helper()
	b := readRaw(t, c)
	var m channel.LobbyMessage
	require.NoError(t, json.Unmarshal(b, &m), "не JSON: %s", b)
	require.Equal(t, "lobby", m.Type, "сообщение: %s", b)
	return m.Lobby
}

// waitLobby читает лобби, пока не выполнится cond.
func waitLobby(t *testing.T, c *websocket.Conn, cond func(channel.LobbyState) bool) channel.LobbyState {
	t.Helper()
	deadline := time.Now().Add(wsWait)
	for time.Now().Before(deadline) {
		if l := readLobby(t, c); cond(l) {
			return l
		}
	}
	require.FailNow(t, "лобби не дошло до нужного состояния")
	return channel.LobbyState{}
}

func eastConnected(l channel.LobbyState) bool {
	return l.Players[channel.PostEast] != nil && l.Players[channel.PostEast].Connected
}

// expectClose читает до закрытия и возвращает код.
func expectClose(t *testing.T, c *websocket.Conn) websocket.StatusCode {
	t.Helper()
	deadline := time.Now().Add(wsWait)
	for time.Now().Before(deadline) {
		if _, _, err := readWatched(t, c); err != nil {
			return websocket.CloseStatus(err)
		}
	}
	require.FailNow(t, "сокет не закрыт")
	return 0
}

// readResult - итог одного websocket.Conn.Read.
type readResult struct {
	typ websocket.MessageType
	b   []byte
	err error
}

// readWatched - Read с собственным сторожем. Таймаут ctx у coder/websocket
// на сломанном сервере срабатывает не всегда, и тест висел бы до -timeout
// (10 мин) вместо того, чтобы упасть за секунды.
func readWatched(t *testing.T, c *websocket.Conn) (websocket.MessageType, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wsWait)
	defer cancel()
	res := make(chan readResult, 1)
	go func() {
		typ, b, err := c.Read(ctx)
		res <- readResult{typ, b, err}
	}()
	select {
	case r := <-res:
		return r.typ, r.b, r.err
	case <-time.After(wsWait + time.Second):
		go func() { _ = c.CloseNow() }() // отпустить зависший Read
		require.FailNow(t, "Read завис дольше таймаута")
		return 0, nil, nil
	}
}

// writeText шлет текстовое сообщение.
func writeText(t *testing.T, c *websocket.Conn, s string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), wsWait)
	defer cancel()
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(s)))
}

// upgradeRequest - запрос апгрейда для проверки отказов без настоящего сокета.
func upgradeRequest(t *testing.T, target, origin string) *http.Request {
	t.Helper()
	req := newRequest(t, http.MethodGet, target, "")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==") // gitleaks:allow
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

// BR-3.1: отказы при апгрейде. Тело - модель huma, токена в нем нет.
func TestWS_UpgradeRejected(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeCoop)
	other := e.create(t, channel.GameModeCoop)
	missingID := "c_AAAAAAAAAAAAAAAAAAAAAA"

	tests := []struct {
		name       string
		target     string
		origin     string
		wantStatus int
	}{
		{"нет токена", "/api/channels/" + host.ChannelID + "/ws", allowedOrigin, http.StatusUnauthorized},
		{"пустой токен", wsPath(host.ChannelID, ""), allowedOrigin, http.StatusUnauthorized},
		{"токен не base64url", wsPath(host.ChannelID, "!!!not-a-token!!!"), allowedOrigin, http.StatusUnauthorized},
		{"токен другого канала", wsPath(host.ChannelID, other.Token), allowedOrigin, http.StatusUnauthorized},
		// id неугадываем (BR-2.4); 401, а не 404: не подтверждаем, есть ли канал.
		{"нет такого канала", wsPath(missingID, host.Token), allowedOrigin, http.StatusUnauthorized},
		{"чужой Origin", wsPath(host.ChannelID, host.Token), forbiddenOrigin, http.StatusForbidden},
		{"Origin с другим портом", wsPath(host.ChannelID, host.Token), "http://localhost:5174", http.StatusForbidden},
		{"Origin не URL", wsPath(host.ChannelID, host.Token), "::::", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doRequest(e.handler, upgradeRequest(t, tt.target, tt.origin))
			checkError(t, rec, tt.wantStatus)
			assert.NotContains(t, rec.Body.String(), host.Token, "токен в теле ошибки")
			assert.NotContains(t, rec.Body.String(), other.Token, "токен в теле ошибки")
		})
	}
}

// BR-3.1: Origin из списка и запрос без Origin (не браузер: loadtest, EFR-21)
// проходят. Браузер Origin шлет всегда, так что CSWSH это не открывает.
func TestWS_OriginAllowed(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)

	for _, origin := range []string{allowedOrigin, ""} {
		t.Run("origin="+origin, func(t *testing.T) {
			t.Parallel()

			host := e.create(t, channel.GameModeSolo)
			ctx, cancel := context.WithTimeout(t.Context(), wsWait)
			defer cancel()
			h := http.Header{}
			if origin != "" {
				h.Set("Origin", origin)
			}
			c, _, err := websocket.Dial(ctx, e.baseURL+wsPath(host.ChannelID, host.Token), //nolint:bodyclose // тело закрывает websocket.Dial при успехе
				&websocket.DialOptions{HTTPHeader: h})
			require.NoError(t, err)
			defer c.CloseNow() //nolint:errcheck // закрытие в конце теста
			readLobby(t, c)
		})
	}
}

// EFR-3.d, приемка: подключиться и получить лобби целиком, как в контракте.
func TestWS_ConnectGetsLobby(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeCoop)
	c := e.dial(t, host)

	want := `{"type":"lobby","lobby":{
		"code":"` + host.Code + `","mode":"coop","phase":"lobby","countdown":null,"you":"west",
		"players":{
			"west":{"callsign":"Первый","ready":false,"bot":false,"connected":true},
			"east":null
		}}}`
	assert.JSONEq(t, want, string(readRaw(t, c)))
}

// EFR-3.d, BR-1.3: одиночная смена - на east готовый бот.
func TestWS_SoloLobby(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeSolo)
	c := e.dial(t, host)

	want := `{"type":"lobby","lobby":{
		"code":"` + host.Code + `","mode":"solo","phase":"lobby","countdown":null,"you":"west",
		"players":{
			"west":{"callsign":"Первый","ready":false,"bot":false,"connected":true},
			"east":{"callsign":"Второй","ready":true,"bot":true,"connected":true}
		}}}`
	assert.JSONEq(t, want, string(readRaw(t, c)))
}

// EFR-3.a: гость подключился - оба получают лобби, у каждого свой you.
func TestWS_GuestConnectBroadcast(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeCoop)
	hc := e.dial(t, host)
	readLobby(t, hc)

	guest := e.join(t, host.Code)
	gc := e.dial(t, guest)

	gl := readLobby(t, gc)
	assert.Equal(t, channel.PostEast, gl.You)
	assert.Equal(t, &channel.LobbyPlayer{Callsign: channel.CallsignFirst, Connected: true}, gl.Players[channel.PostWest])
	assert.Equal(t, &channel.LobbyPlayer{Callsign: channel.CallsignSecond, Connected: true}, gl.Players[channel.PostEast])

	hl := waitLobby(t, hc, eastConnected)
	assert.Equal(t, channel.PostWest, hl.You)
}

// BR-3.2: второе подключение с тем же токеном закрывает первое кодом 4001,
// новое получает лобби и остается подписанным.
func TestWS_ReconnectReplacesOld(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeCoop)

	first := e.dial(t, host)
	readLobby(t, first)

	second := e.dial(t, host)
	assert.Equal(t, channel.PostWest, readLobby(t, second).You)
	assert.Equal(t, ws.StatusReplaced, expectClose(t, first))

	// Закрытие старого сокета не выбило хозяина: гость видит его на связи,
	// а новое соединение получает рассылку.
	guest := e.join(t, host.Code)
	gl := readLobby(t, e.dial(t, guest))
	assert.True(t, gl.Players[channel.PostWest].Connected, "старый сокет отключил хозяина")
	waitLobby(t, second, eastConnected)
}

// BR-3.3: сообщение больше GAME_WS_MAX_MESSAGE - закрытие 1009.
// Ровно в лимит - соединение живо.
func TestWS_MaxMessage(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeCoop)
	c := e.dial(t, host)
	readLobby(t, c)

	// Валидный JSON ровно в лимит: неизвестный type, канал его проигнорирует.
	prefix := `{"type":"unknown","pad":"`
	exact := prefix + strings.Repeat("x", testWSMaxMsgBytes-len(prefix)-2) + `"}`
	require.Len(t, exact, testWSMaxMsgBytes)
	writeText(t, c, exact)

	guest := e.join(t, host.Code)
	e.dial(t, guest)
	waitLobby(t, c, eastConnected) // соединение живо

	writeText(t, c, exact+" ")
	assert.Equal(t, websocket.StatusMessageTooBig, expectClose(t, c))
}

// Раздел 5: не JSON и неизвестный type игнорируются, сокет не закрывается.
func TestWS_GarbageIgnored(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeCoop)
	c := e.dial(t, host)
	readLobby(t, c)

	for _, s := range []string{`not json`, `{"type":"unknown"}`, `[]`, `{}`} {
		writeText(t, c, s)
	}
	ctx, cancel := context.WithTimeout(t.Context(), wsWait)
	defer cancel()
	require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte{0xff, 0x00}))

	guest := e.join(t, host.Code)
	e.dial(t, guest)
	waitLobby(t, c, eastConnected)
}

// BR-3.6: ReadTimeout и WriteTimeout http.Server не обрывают сокет.
func TestWS_ServerTimeoutsDoNotKillSocket(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.ReadTimeout = 100 * time.Millisecond
	cfg.WriteTimeout = 100 * time.Millisecond

	reg := channel.NewRegistry(10)
	t.Cleanup(reg.Close)
	h := newHandler(slog.New(slog.DiscardHandler), cfg, reg, WithGame(testGameConfig()))

	// Нужен именно http.Server пакета с этими таймаутами, а не httptest.
	srv := New(slog.New(slog.DiscardHandler), cfg, reg, WithGame(testGameConfig()))
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	base := "ws://" + ln.Addr().String()

	host := mustCreate(t, h, channel.GameModeCoop)
	c := dialURL(t, base+wsPath(host.ChannelID, host.Token))
	readLobby(t, c)

	time.Sleep(5 * cfg.ReadTimeout) // дольше обоих таймаутов

	// Запись клиента (чтение сервера) живет.
	writeText(t, c, `{"type":"unknown"}`)
	// Запись сервера живет.
	guest := decodeChannel(t, joinChannel(t, h, host.Code), http.StatusOK)
	dialURL(t, base+wsPath(guest.ChannelID, guest.Token))
	waitLobby(t, c, eastConnected)
}

// BR-3.7, NFR-3: токен не попадает в логи - ни в access log, ни в debug-строки
// про мусорные сообщения и отказы.
func TestWS_TokenNotLogged(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeCoop)
	guest := e.join(t, host.Code)

	hc := e.dial(t, host)
	readLobby(t, hc)
	gc := e.dial(t, guest)
	readLobby(t, gc)

	// Тела входящих сообщений не логируем: в них бывает что угодно (SDP, EFR-18).
	writeText(t, hc, `not json `+host.Token)
	writeText(t, hc, `{"type":"unknown","token":"`+host.Token+`"}`)
	e.dial(t, host) // замена соединения (BR-3.2)
	doRequest(e.handler, upgradeRequest(t, wsPath(host.ChannelID, guest.Token+"x"), allowedOrigin))
	doRequest(e.handler, upgradeRequest(t, wsPath(host.ChannelID, host.Token), forbiddenOrigin))
	require.NoError(t, gc.Close(websocket.StatusNormalClosure, ""))

	// Дать горутинам сокетов дописать логи.
	assert.Eventually(t, func() bool { return strings.Contains(e.logs.String(), "/ws") },
		wsWait, 10*time.Millisecond, "access log для апгрейда")
	time.Sleep(100 * time.Millisecond)

	logs := e.logs.String()
	for name, tok := range map[string]string{"host": host.Token, "guest": guest.Token} {
		assert.NotContains(t, logs, tok, "токен %s в логе", name)
		assert.NotContains(t, logs, url.QueryEscape(tok), "токен %s в логе (escaped)", name)
	}
	assert.NotContains(t, logs, "token=", "query с токеном в логе")
}

// BR-5.6, NFR-5: при остановке сервера (Registry.Close в main) клиенты
// получают 1001 по настоящему сокету.
func TestWS_RegistryCloseSendsGoingAway(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeCoop)
	guest := e.join(t, host.Code)
	hc := e.dial(t, host)
	readLobby(t, hc)
	gc := e.dial(t, guest)
	readLobby(t, gc)

	e.reg.Close()

	assert.Equal(t, websocket.StatusGoingAway, expectClose(t, hc))
	assert.Equal(t, websocket.StatusGoingAway, expectClose(t, gc))
}

// BR-5.6: канал удален (Registry.Remove) - клиенты получают 1000, а
// повторное подключение по тому же токену - 401: канала больше нет.
func TestWS_RemoveSendsNormalClosure(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeSolo)
	c := e.dial(t, host)
	readLobby(t, c)

	e.reg.Remove(host.ChannelID)

	assert.Equal(t, websocket.StatusNormalClosure, expectClose(t, c))
	rec := doRequest(e.handler, upgradeRequest(t, wsPath(host.ChannelID, host.Token), allowedOrigin))
	checkError(t, rec, http.StatusUnauthorized)
}

// BR-5.4: гость отключился - хозяин видит connected=false по настоящему сокету.
func TestWS_DisconnectBroadcast(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	host := e.create(t, channel.GameModeCoop)
	guest := e.join(t, host.Code)
	hc := e.dial(t, host)
	readLobby(t, hc)
	gc := e.dial(t, guest)
	waitLobby(t, hc, eastConnected)

	require.NoError(t, gc.Close(websocket.StatusNormalClosure, "bye"))

	waitLobby(t, hc, func(l channel.LobbyState) bool { return !eastConnected(l) })
}

// WithGame: шаблон "*" пропускает любой Origin и в ручной проверке, и в
// websocket.Accept (раньше Accept отвечал 403 сам).
func TestWS_WildcardOrigin(t *testing.T) {
	t.Parallel()

	game := testGameConfig()
	game.WSAllowedOrigins = []string{"*"}
	reg := channel.NewRegistry(10)
	h := newHandler(slog.New(slog.DiscardHandler), testConfig(), reg, WithGame(game))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(reg.Close)

	host := mustCreate(t, h, channel.GameModeSolo)
	ctx, cancel := context.WithTimeout(t.Context(), wsWait)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+wsPath(host.ChannelID, host.Token), //nolint:bodyclose // тело закрывает websocket.Dial при успехе
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://anything.test"}}})
	require.NoError(t, err)
	defer c.CloseNow() //nolint:errcheck // закрытие в конце теста
	readLobby(t, c)
}

// Сервер останавливается (реестр закрыт) - создание канала отвечает 503.
func TestCreateChannel_AfterRegistryClose(t *testing.T) {
	t.Parallel()

	e := newWSEnv(t)
	e.reg.Close()

	checkError(t, createChannel(t, e.handler, `{"mode":"coop"}`), http.StatusServiceUnavailable)
}

// Порядок остановки из main: Shutdown -> Registry.Close -> WaitWebSockets.
// Пока сокет жив, WaitWebSockets ждет; после Close реестра - возвращается,
// а клиент получает 1001, а не обрыв.
func TestServer_WaitWebSockets(t *testing.T) {
	t.Parallel()

	reg := channel.NewRegistry(10)
	t.Cleanup(reg.Close)
	srv := New(slog.New(slog.DiscardHandler), testConfig(), reg, WithGame(testGameConfig()))
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()

	h := newHandler(slog.New(slog.DiscardHandler), testConfig(), reg, WithGame(testGameConfig()))
	host := mustCreate(t, h, channel.GameModeSolo)
	c := dialURL(t, "ws://"+ln.Addr().String()+wsPath(host.ChannelID, host.Token))
	readLobby(t, c)

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), wsWait)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "Shutdown не ждет перехваченные сокеты")

	short, cancelShort := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancelShort()
	require.ErrorIs(t, srv.WaitWebSockets(short), context.DeadlineExceeded, "сокет еще жив - ждать")

	reg.Close()
	assert.Equal(t, websocket.StatusGoingAway, expectClose(t, c))
	require.NoError(t, srv.WaitWebSockets(ctx))
}
