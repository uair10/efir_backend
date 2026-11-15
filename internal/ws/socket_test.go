package ws

// Тесты EFR-3.c: ws.Socket - горутины чтения и записи сокета, ping/pong,

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/channel"
)

const waitTimeout = 3 * time.Second

// testOptions - ping так редко, что он не мешает тестам, которые не про него.
func testOptions() Options {
	return Options{
		MaxMessageBytes: 256,
		PingInterval:    time.Hour,
		PongTimeout:     time.Hour,
		OutboxSize:      8,
		Logger:          slog.New(slog.DiscardHandler),
	}
}

// pair - соединение, которое тестируем, и клиент на другом конце.
type pair struct {
	server *Socket         // ws.Socket этого пакета: обертка над серверным сокетом
	client *websocket.Conn // сокет coder/websocket со стороны браузера
	got    chan []byte     // что сервер получил в onMessage
	done   chan error      // результат server.Run
}

// startPair поднимает httptest.Server, где каждое подключение обслуживает Conn.
func startPair(t *testing.T, opts Options) *pair {
	t.Helper()

	p := &pair{got: make(chan []byte, 64), done: make(chan error, 1)}
	ready := make(chan *Socket, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn := New(c, opts)
		ready <- conn
		p.done <- conn.Run(r.Context(), func(b []byte) {
			p.got <- append([]byte(nil), b...)
		})
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	client, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	client.SetReadLimit(1 << 24)
	t.Cleanup(func() { _ = client.CloseNow() })
	p.client = client

	select {
	case p.server = <-ready:
	case <-time.After(waitTimeout):
		require.FailNow(t, "сервер не принял подключение")
	}
	return p
}

// read читает одно сообщение клиентом.
func (p *pair) read(t *testing.T) (websocket.MessageType, []byte, error) {
	t.Helper()
	return readWatched(t, p.client)
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
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	res := make(chan readResult, 1)
	go func() {
		typ, b, err := c.Read(ctx)
		res <- readResult{typ, b, err}
	}()
	select {
	case r := <-res:
		return r.typ, r.b, r.err
	case <-time.After(waitTimeout + time.Second):
		go func() { _ = c.CloseNow() }() // отпустить зависший Read
		require.FailNow(t, "Read завис дольше таймаута")
		return 0, nil, nil
	}
}

// readJSON читает одно сообщение и разбирает его как JSON-объект.
func (p *pair) readJSON(t *testing.T) map[string]any {
	t.Helper()
	typ, b, err := p.read(t)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageText, typ, "NFR-2: JSON идет текстовым кадром")
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m), "не JSON: %s", b)
	return m
}

// write шлет текстовое сообщение от клиента.
func (p *pair) write(t *testing.T, s string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	require.NoError(t, p.client.Write(ctx, websocket.MessageText, []byte(s)))
}

// waitRun ждет завершения server.Run.
func (p *pair) waitRun(t *testing.T, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-p.done:
		return err
	case <-time.After(timeout):
		require.FailNow(t, "Run не вернулся")
		return nil
	}
}

func msg(payload any) channel.Message { return channel.Message{Payload: payload} }

func snapshot(payload any) channel.Message {
	return channel.Message{Payload: payload, Droppable: true}
}

// NFR-2: каждое сообщение - один JSON-объект в отдельном кадре, порядок сохраняется.
func TestSend_JSONFramePerMessage(t *testing.T) {
	t.Parallel()

	p := startPair(t, testOptions())
	p.server.Send(msg(map[string]any{"type": "lobby", "n": 1}))
	p.server.Send(msg(map[string]any{"type": "error", "code": "callsign_taken"}))

	assert.Equal(t, map[string]any{"type": "lobby", "n": float64(1)}, p.readJSON(t))
	assert.Equal(t, map[string]any{"type": "error", "code": "callsign_taken"}, p.readJSON(t))
}

// Входящие сообщения доходят до onMessage как есть.
func TestRun_PassesIncoming(t *testing.T) {
	t.Parallel()

	p := startPair(t, testOptions())
	p.write(t, `{"type":"set_ready","ready":true}`)
	p.write(t, `not json`) // разбирает канал, ws пропускает как есть

	for _, want := range []string{`{"type":"set_ready","ready":true}`, `not json`} {
		select {
		case got := <-p.got:
			assert.Equal(t, want, string(got))
		case <-time.After(waitTimeout):
			require.FailNow(t, "сообщение не дошло до onMessage")
		}
	}
}

// BR-3.3: сообщение ровно в лимит проходит, на байт больше - закрытие 1009.
func TestRun_ReadLimit(t *testing.T) {
	t.Parallel()

	opts := testOptions()
	p := startPair(t, opts)

	exact := strings.Repeat("a", int(opts.MaxMessageBytes))
	p.write(t, exact)
	select {
	case got := <-p.got:
		assert.Len(t, got, int(opts.MaxMessageBytes))
	case <-time.After(waitTimeout):
		require.FailNow(t, "сообщение ровно в лимит не дошло")
	}

	p.write(t, exact+"a")
	_, _, err := p.read(t)
	assert.Equal(t, websocket.StatusMessageTooBig, websocket.CloseStatus(err), "err: %v", err)

	_ = p.waitRun(t, waitTimeout)
	assert.Empty(t, p.got, "слишком большое сообщение не должно дойти")
}

// BR-3.2, BR-5.4, BR-5.6: причины закрытия и коды.
func TestClose_Codes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason channel.CloseReason
		want   websocket.StatusCode
	}{
		{channel.CloseReasonReplaced, StatusReplaced},
		{channel.CloseReasonChannelClosed, websocket.StatusNormalClosure},
		{channel.CloseReasonShutdown, websocket.StatusGoingAway},
		{channel.CloseReasonInternal, websocket.StatusInternalError},
	}
	for _, tt := range tests {
		t.Run(tt.want.String(), func(t *testing.T) {
			t.Parallel()

			p := startPair(t, testOptions())
			p.server.Close(tt.reason)

			_, _, err := p.read(t)
			assert.Equal(t, tt.want, websocket.CloseStatus(err), "err: %v", err)
			_ = p.waitRun(t, waitTimeout)

			// После закрытия Send и Close безопасны и не блокируют.
			p.server.Send(msg(map[string]any{"type": "lobby"}))
			p.server.Close(tt.reason)
		})
	}
}

// Сообщения, отправленные до Close, доходят раньше кадра закрытия:
// гость должен получить последнее лобби, а потом 1000.
func TestClose_FlushesOutbox(t *testing.T) {
	t.Parallel()

	p := startPair(t, testOptions())
	p.server.Send(msg(map[string]any{"type": "lobby"}))
	p.server.Close(channel.CloseReasonChannelClosed)

	assert.Equal(t, "lobby", p.readJSON(t)["type"])
	_, _, err := p.read(t)
	assert.Equal(t, websocket.StatusNormalClosure, websocket.CloseStatus(err))
}

// Клиент закрыл сокет кодом 1000 - Run возвращается, код виден вызывающему
// (BR-5.4: по нему канал решит, освобождать ли пост).
func TestRun_ClientClosedNormally(t *testing.T) {
	t.Parallel()

	p := startPair(t, testOptions())
	require.NoError(t, p.client.Close(websocket.StatusNormalClosure, "bye"))

	err := p.waitRun(t, waitTimeout)
	assert.Equal(t, websocket.StatusNormalClosure, websocket.CloseStatus(err), "err: %v", err)
}

// Обрыв без кадра закрытия - Run возвращается, это не 1000.
func TestRun_ClientDropped(t *testing.T) {
	t.Parallel()

	p := startPair(t, testOptions())
	require.NoError(t, p.client.CloseNow())

	err := p.waitRun(t, waitTimeout)
	require.Error(t, err)
	assert.NotEqual(t, websocket.StatusNormalClosure, websocket.CloseStatus(err))
}

// Отмена контекста Run (сервер выключается) закрывает соединение.
func TestRun_ContextCanceled(t *testing.T) {
	t.Parallel()

	opts := testOptions()
	srvDone := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		srvDone <- New(c, opts).Run(ctx, func([]byte) {})
	}))
	t.Cleanup(srv.Close)

	client, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil) //nolint:bodyclose // тело закрывает websocket.Dial при успехе
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.CloseNow() })

	cancel()
	select {
	case <-srvDone:
	case <-time.After(waitTimeout):
		require.FailNow(t, "Run не вернулся после отмены контекста")
	}
}

// BR-3.4: клиент, который отвечает на ping (читает сокет), живет дольше
// нескольких интервалов ping.
func TestPing_AliveClientStays(t *testing.T) {
	t.Parallel()

	opts := testOptions()
	opts.PingInterval = 20 * time.Millisecond
	opts.PongTimeout = 50 * time.Millisecond
	p := startPair(t, opts)

	// Клиент coder/websocket отвечает pong только внутри Read.
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := p.client.Read(t.Context()); err != nil {
				readErr <- err
				return
			}
		}
	}()

	select {
	case err := <-p.done:
		require.FailNow(t, "живой клиент отключен", "Run: %v", err)
	case err := <-readErr:
		require.FailNow(t, "живой клиент отключен", "read: %v", err)
	case <-time.After(20 * opts.PingInterval):
	}
}

// BR-3.4: нет pong за PongTimeout - соединение оборвано, Run возвращается.
func TestPing_DeadClientDropped(t *testing.T) {
	t.Parallel()

	opts := testOptions()
	opts.PingInterval = 20 * time.Millisecond
	opts.PongTimeout = 50 * time.Millisecond
	p := startPair(t, opts) // клиент не читает - и не отвечает на ping

	start := time.Now()
	err := p.waitRun(t, waitTimeout)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 20*(opts.PingInterval+opts.PongTimeout),
		"оборванный клиент держался слишком долго")
}

// bigSnapshot - снимок, которого хватит, чтобы быстро забить буферы TCP.
func bigSnapshot(seq int) channel.Message {
	return snapshot(map[string]any{"type": "game", "seq": seq, "pad": strings.Repeat("x", 64<<10)})
}

// BR-3.5, ADR-002: клиент не читает, буферы TCP забиты, а Send не блокирует.
// Старые снимки выбрасываются, обычные сообщения - нет, последний снимок доходит.
func TestSend_SlowClientDropsOldSnapshots(t *testing.T) {
	t.Parallel()

	opts := testOptions()
	p := startPair(t, opts)

	const snapshots = 300 // ~19 МБ: столько локальный TCP не вместит
	start := time.Now()
	for i := range snapshots {
		p.server.Send(bigSnapshot(i))
		if i == snapshots/2 {
			p.server.Send(msg(map[string]any{"type": "events", "n": "mid"}))
		}
	}
	p.server.Send(msg(map[string]any{"type": "events", "n": "end"}))
	assert.Less(t, time.Since(start), time.Second, "Send блокирует на медленном клиенте")

	// Теперь клиент начинает читать.
	var (
		events   []string
		lastSeq  = -1
		received int
	)
	for len(events) < 2 {
		m := p.readJSON(t)
		switch m["type"] {
		case "game":
			seq := int(m["seq"].(float64)) //nolint:errcheck // формат задан тестом
			assert.Greater(t, seq, lastSeq, "снимки идут по порядку")
			lastSeq = seq
			received++
		case "events":
			events = append(events, m["n"].(string)) //nolint:errcheck // формат задан тестом
		}
	}

	assert.Equal(t, []string{"mid", "end"}, events, "события не выбрасываются и идут по порядку")
	assert.Less(t, received, snapshots, "старые снимки должны выбрасываться")
	assert.Equal(t, snapshots-1, lastSeq, "последний снимок не выбрасывается")
}

// BR-3.5: медленный клиент не тормозит соседа. Два соединения, общий
// отправитель (как горутина канала): первый не читает, второй получает все.
func TestSend_SlowClientDoesNotBlockOthers(t *testing.T) {
	t.Parallel()

	const n = 200
	slow := startPair(t, testOptions())
	fastOpts := testOptions()
	fastOpts.OutboxSize = n // залп из n сообщений не должен упереться в политику 1008
	fast := startPair(t, fastOpts)

	go func() {
		for i := range n {
			slow.server.Send(bigSnapshot(i))
			fast.server.Send(msg(map[string]any{"type": "lobby", "seq": i}))
		}
	}()

	// Если Send ждет медленного, отправитель встанет, и readJSON упадет по таймауту.
	for i := range n {
		m := fast.readJSON(t)
		assert.EqualValues(t, i, m["seq"])
	}
}

// ADR-002: если outbox забит тем, что выбрасывать нельзя, клиент безнадежно
// отстал - соединение закрывается (1008), а не растет без предела и не блокирует.
func TestSend_OverflowOfNonDroppableCloses(t *testing.T) {
	t.Parallel()

	opts := testOptions()
	p := startPair(t, opts) // клиент не читает

	pad := strings.Repeat("x", 64<<10)
	start := time.Now()
	for i := range 300 {
		p.server.Send(msg(map[string]any{"type": "events", "seq": i, "pad": pad}))
	}
	assert.Less(t, time.Since(start), time.Second, "Send блокирует")

	// Кадр 1008 может не дойти: клиент не читает и TCP забит. Важно, что
	// сервер отпустил соединение.
	err := p.waitRun(t, 10*time.Second)
	require.Error(t, err)
}

// -race: Send и Close из многих горутин одновременно.
func TestSocket_ConcurrentSendClose(t *testing.T) {
	t.Parallel()

	p := startPair(t, testOptions())
	go func() {
		for {
			if _, _, err := p.client.Read(t.Context()); err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			for j := range 50 {
				p.server.Send(msg(map[string]any{"type": "lobby", "i": i, "j": j}))
			}
		})
	}
	wg.Go(func() { p.server.Close(channel.CloseReasonShutdown) })
	wg.Wait()
	_ = p.waitRun(t, waitTimeout)
}

// После Run не остается горутин соединения (чтение, запись, ping).
//
//nolint:paralleltest // runtime.NumGoroutine считает горутины всего процесса
func TestRun_NoGoroutineLeak(t *testing.T) {
	base := runtime.NumGoroutine()

	func() {
		opts := testOptions()
		opts.PingInterval = 10 * time.Millisecond
		srvDone := make(chan error, 10)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			conn := New(c, opts)
			conn.Send(msg(map[string]any{"type": "lobby"}))
			srvDone <- conn.Run(r.Context(), func([]byte) {})
		}))
		defer srv.Close()

		for range 10 {
			c, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil) //nolint:bodyclose // тело закрывает websocket.Dial при успехе
			require.NoError(t, err)
			_, _, err = c.Read(t.Context())
			require.NoError(t, err)
			require.NoError(t, c.Close(websocket.StatusNormalClosure, ""))
		}
		for range 10 {
			select {
			case <-srvDone:
			case <-time.After(waitTimeout):
				require.FailNow(t, "Run не вернулся")
			}
		}
	}()

	waitGoroutines(t, base, "горутины соединений не завершились")
}

// waitGoroutines ждет, пока горутин станет не больше base. Не через
// assert.Eventually: тот гоняет условие в своей горутине и сбивает счет.
func waitGoroutines(t *testing.T, base int, msg string) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			assert.Failf(t, msg, "было %d, стало %d", base, runtime.NumGoroutine())
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Run отдает ошибку, из которой виден код, который видел клиент. Ошибка с
// кодом закрытия важнее ошибки без кода; при равенстве - ошибка reader.
func TestPickRunErr(t *testing.T) {
	t.Parallel()

	closeErr := func(code websocket.StatusCode) error {
		return fmt.Errorf("ws: read: %w", websocket.CloseError{Code: code})
	}
	plain := errors.New("use of closed network connection")

	tests := []struct {
		name     string
		read     error
		write    error
		wantCode websocket.StatusCode
		wantErr  error
	}{
		{"клиент закрыл, writer опоздал", closeErr(websocket.StatusNormalClosure), plain, websocket.StatusNormalClosure, nil},
		{"обрыв, writer закрыл кодом", plain, closeErr(websocket.StatusGoingAway), websocket.StatusGoingAway, nil},
		{"оба с кодом - reader", closeErr(websocket.StatusMessageTooBig), closeErr(websocket.StatusNormalClosure), websocket.StatusMessageTooBig, nil},
		{"оба без кода - reader", plain, errors.New("write"), -1, plain},
		{"reader nil - writer", nil, plain, -1, plain},
		{"оба nil", nil, nil, -1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := pickRunErr(tt.read, tt.write)
			assert.Equal(t, tt.wantCode, websocket.CloseStatus(got), "err: %v", got)
			if tt.wantErr != nil {
				assert.Same(t, tt.wantErr, got)
			}
			if tt.read == nil && tt.write == nil {
				assert.NoError(t, got)
			}
		})
	}
}

// Каждая причина канала превращается в свой код (BR-5.6).
func TestCloseSpecFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason channel.CloseReason
		want   websocket.StatusCode
	}{
		{channel.CloseReasonReplaced, StatusReplaced},
		{channel.CloseReasonChannelClosed, websocket.StatusNormalClosure},
		{channel.CloseReasonShutdown, websocket.StatusGoingAway},
		{channel.CloseReasonInternal, websocket.StatusInternalError},
		{channel.CloseReason("unknown"), websocket.StatusInternalError},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, closeSpecFor(tt.reason).code, "reason %q", tt.reason)
	}
}

// После выхода Run Send и Close - no-op: канал может звать их когда угодно.
func TestSocket_SendCloseAfterRun(t *testing.T) {
	t.Parallel()

	p := startPair(t, testOptions())
	require.NoError(t, p.client.Close(websocket.StatusNormalClosure, "bye"))
	_ = p.waitRun(t, waitTimeout)

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.server.Send(msg(map[string]any{"type": "lobby"}))
		p.server.Close(channel.CloseReasonShutdown)
	}()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		require.FailNow(t, "Send/Close после Run заблокировались")
	}
}

// Run сам гасит pinger при закрытии - это не pong timeout, и в лог как
// обрыв клиента не пишется.
func TestPing_NoFalseTimeoutOnClose(t *testing.T) {
	t.Parallel()

	var logs lockedLog
	opts := testOptions()
	opts.PingInterval = time.Millisecond // pinger почти всегда посреди Ping
	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	for range 20 {
		p := startPair(t, opts)
		// Клиент должен читать, иначе pong не придет и timeout будет честным.
		go func() {
			for {
				if _, _, err := p.client.Read(context.Background()); err != nil {
					return
				}
			}
		}()
		p.server.Close(channel.CloseReasonShutdown)
		_ = p.waitRun(t, waitTimeout)
	}
	assert.NotContains(t, logs.String(), "pong timeout")
}

// lockedLog - буфер для логов из горутин сокета.
type lockedLog struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}
