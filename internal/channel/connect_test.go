package channel

// Тесты EFR-3.a: подключение игроков к каналу. У каждого канала своя горутина,
// которая одна владеет его состоянием (ADR-002); подключение, входящие
// сообщения и отключение идут к ней через inbox.
//
// Сеть здесь не нужна: вместо сокета - fakeSocket.
//
// Горутину канала запускает Registry.Create.

import (
	"encoding/json"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/game"
)

// waitTimeout - сколько ждать сообщения от горутины канала.
const waitTimeout = 2 * time.Second

// fakeSocket - подключение игрока без сети. Send не блокирует, как и
// настоящий ws.Conn: при переполнении буфера сообщение теряется, и тест
// упадет на ожидании.
type fakeSocket struct {
	msgs   chan Message
	closed chan CloseReason
	once   sync.Once
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{
		msgs:   make(chan Message, 256),
		closed: make(chan CloseReason, 1),
	}
}

func (f *fakeSocket) Send(m Message) {
	select {
	case f.msgs <- m:
	default:
	}
}

func (f *fakeSocket) Close(r CloseReason) {
	f.once.Do(func() { f.closed <- r })
}

// decodeLobby достает лобби из сообщения канала.
func decodeLobby(t *testing.T, m Message) LobbyMessage {
	t.Helper()
	lm, ok := m.Payload.(LobbyMessage)
	require.True(t, ok, "payload %T, want LobbyMessage", m.Payload)
	require.Equal(t, ServerMessageLobby, lm.Type)
	// BR-4.1: лобби приходит после каждого изменения, выбрасывать его нельзя.
	assert.False(t, m.Droppable, "лобби не должно быть Droppable")
	return lm
}

// nextLobby ждет следующее сообщение клиенту и разбирает его как лобби.
func nextLobby(t *testing.T, f *fakeSocket) LobbyMessage {
	t.Helper()
	select {
	case m := <-f.msgs:
		return decodeLobby(t, m)
	case <-time.After(waitTimeout):
		require.FailNow(t, "нет сообщения лобби")
		return LobbyMessage{}
	}
}

// waitLobby читает лобби, пока не выполнится cond. Промежуточные
// рассылки не важны: важно, что нужное состояние доходит.
func waitLobby(t *testing.T, f *fakeSocket, cond func(LobbyMessage) bool) LobbyMessage {
	t.Helper()
	deadline := time.After(waitTimeout)
	for {
		select {
		case m := <-f.msgs:
			if lm := decodeLobby(t, m); cond(lm) {
				return lm
			}
		case <-deadline:
			require.FailNow(t, "лобби не дошло до нужного состояния")
			return LobbyMessage{}
		}
	}
}

// waitClosed ждет, что канал закроет клиента, и возвращает причину.
func waitClosed(t *testing.T, f *fakeSocket) CloseReason {
	t.Helper()
	select {
	case r := <-f.closed:
		return r
	case <-time.After(waitTimeout):
		require.FailNow(t, "клиент не закрыт")
		return ""
	}
}

// newTestRegistry - реестр, горутины каналов которого остановятся в конце теста.
func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry(testMaxChannels)
	t.Cleanup(r.Close)
	return r
}

// coopWithGuest открывает канал на двоих и сажает гостя.
// Возвращает канал, токен хозяина и токен гостя.
//
//nolint:revive // confusing-results: именованные результаты запрещает nonamedreturns
func coopWithGuest(t *testing.T, r *Registry) (*Channel, Token, Token) {
	t.Helper()
	ch, host, err := r.Create(t.Context(), game.ModeCoop)
	require.NoError(t, err)
	guest, err := ch.Join(t.Context())
	require.NoError(t, err)
	return ch, host, guest
}

func mustConnect(t *testing.T, ch *Channel, tok Token, sock Socket) *Connection {
	t.Helper()
	conn, err := ch.Connect(t.Context(), tok, sock)
	require.NoError(t, err)
	return conn
}

// EFR-3.d: сразу после подключения хозяин получает лобби целиком.
func TestConnect_HostGetsLobby(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, err := r.Create(t.Context(), game.ModeCoop)
	require.NoError(t, err)

	hostSock := newFakeSocket()
	conn := mustConnect(t, ch, host, hostSock)
	assert.Equal(t, game.PostWest, conn.Post())

	lm := nextLobby(t, hostSock).Lobby
	assert.Equal(t, ch.Code().String(), lm.Code)
	assert.Equal(t, game.ModeCoop, lm.Mode)
	assert.Equal(t, game.PhaseLobby, lm.Phase)
	assert.Nil(t, lm.Countdown)
	assert.Equal(t, game.PostWest, lm.You)

	require.Contains(t, lm.Players, game.PostWest)
	require.Contains(t, lm.Players, game.PostEast, "свободный пост - null, но ключ есть")
	assert.Equal(t, &LobbyPlayer{Callsign: CallsignFirst, Connected: true}, lm.Players[game.PostWest])
	assert.Nil(t, lm.Players[game.PostEast])
}

// EFR-3.d, BR-1.3: в одиночной смене east сразу занят готовым ботом.
func TestConnect_SoloLobbyHasBot(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, err := r.Create(t.Context(), game.ModeSolo)
	require.NoError(t, err)

	hostSock := newFakeSocket()
	mustConnect(t, ch, host, hostSock)

	lm := nextLobby(t, hostSock).Lobby
	assert.Equal(t, game.ModeSolo, lm.Mode)
	assert.Equal(t, &LobbyPlayer{Callsign: CallsignFirst, Connected: true}, lm.Players[game.PostWest])
	assert.Equal(t, &LobbyPlayer{Callsign: CallsignSecond, Ready: true, Bot: true, Connected: true}, lm.Players[game.PostEast])
}

// Раздел 3.1 контракта: LobbyMessage кодируется ровно так, как ждет фронтенд.
// Эталон - строка, а не Go-типы: только так ловится опечатка в теге JSON.
// Остальные тесты пакета работают с LobbyMessage напрямую.
func TestLobbyMessage_JSON(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, _ := coopWithGuest(t, r)
	hostSock := newFakeSocket()
	mustConnect(t, ch, host, hostSock)

	b, err := json.Marshal(nextLobby(t, hostSock))
	require.NoError(t, err)
	want := `{"type":"lobby","lobby":{
		"code":"` + ch.Code().String() + `","mode":"coop","phase":"lobby","countdown":null,"you":"west",
		"players":{
			"west":{"callsign":"Первый","ready":false,"bot":false,"connected":true},
			"east":{"callsign":"Второй","ready":false,"bot":false,"connected":false}
		}}}`
	assert.JSONEq(t, want, string(b))
}

// EFR-3.a: подключение гостя - оба получают лобби, каждый со своим you.
func TestConnect_GuestBroadcast(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, guest := coopWithGuest(t, r)

	hostSock := newFakeSocket()
	mustConnect(t, ch, host, hostSock)
	first := nextLobby(t, hostSock).Lobby
	// Гость уже сидит на посту, но сокет не открыл.
	require.NotNil(t, first.Players[game.PostEast])
	assert.False(t, first.Players[game.PostEast].Connected)

	guestSock := newFakeSocket()
	guestConn := mustConnect(t, ch, guest, guestSock)
	assert.Equal(t, game.PostEast, guestConn.Post())

	gl := nextLobby(t, guestSock).Lobby
	assert.Equal(t, game.PostEast, gl.You)
	assert.True(t, gl.Players[game.PostWest].Connected)
	assert.True(t, gl.Players[game.PostEast].Connected)
	assert.Equal(t, CallsignSecond, gl.Players[game.PostEast].Callsign)

	hl := waitLobby(t, hostSock, func(l LobbyMessage) bool { return l.Lobby.Players[game.PostEast].Connected }).Lobby
	assert.Equal(t, game.PostWest, hl.You, "у каждого свой you")
}

// BR-3.1: чужой, пустой и ботовый токены в канал не пускают.
func TestConnect_Unauthorized(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, _, err := r.Create(t.Context(), game.ModeSolo)
	require.NoError(t, err)
	_, foreign, err := r.Create(t.Context(), game.ModeCoop)
	require.NoError(t, err)

	for name, tok := range map[string]Token{
		"токен другого канала":      foreign,
		"пустой токен (как у бота)": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sock := newFakeSocket()
			conn, err := ch.Connect(t.Context(), tok, sock)
			require.ErrorIs(t, err, ErrUnauthorized)
			assert.Nil(t, conn)
			assert.Empty(t, sock.msgs, "отказанному клиенту ничего не шлем")
		})
	}
}

// BR-3.2: новое подключение с тем же токеном закрывает старое
// (CloseReasonReplaced -> код 1000) и занимает его место.
func TestConnect_ReplacesOldConnection(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, guest := coopWithGuest(t, r)

	oldSock := newFakeSocket()
	oldConn := mustConnect(t, ch, host, oldSock)
	nextLobby(t, oldSock)

	newSock := newFakeSocket()
	newConn := mustConnect(t, ch, host, newSock)
	assert.Equal(t, game.PostWest, newConn.Post())

	assert.Equal(t, CloseReasonReplaced, waitClosed(t, oldSock))
	assert.Equal(t, game.PostWest, nextLobby(t, newSock).Lobby.You)

	// Читатель старого сокета узнает о закрытии позже и вызывает Disconnect.
	// Это не должно выбить нового владельца поста.
	oldConn.Disconnect()

	guestSock := newFakeSocket()
	mustConnect(t, ch, guest, guestSock)
	gl := nextLobby(t, guestSock).Lobby
	assert.True(t, gl.Players[game.PostWest].Connected, "Disconnect старого подключения отключил новое")

	// Новое соединение подписано, старое - нет.
	waitLobby(t, newSock, func(l LobbyMessage) bool { return l.Lobby.Players[game.PostEast].Connected })
	drainNoLobby(t, oldSock)
}

// drainNoLobby проверяет, что замененному клиенту больше ничего не шлют.
// К этому моменту рассылка, которая могла бы до него дойти, уже прошла.
func drainNoLobby(t *testing.T, f *fakeSocket) {
	t.Helper()
	assert.Empty(t, f.msgs, "замененный клиент все еще подписан")
}

// EFR-3.a, основа BR-5.1: после Disconnect напарник видит connected: false,
// а пост за игроком остается.
func TestDisconnect_PartnerSeesDisconnected(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, guest := coopWithGuest(t, r)

	hostSock := newFakeSocket()
	mustConnect(t, ch, host, hostSock)
	guestSock := newFakeSocket()
	guestConn := mustConnect(t, ch, guest, guestSock)
	waitLobby(t, hostSock, func(l LobbyMessage) bool { return l.Lobby.Players[game.PostEast].Connected })

	guestConn.Disconnect()

	hl := waitLobby(t, hostSock, func(l LobbyMessage) bool { return !l.Lobby.Players[game.PostEast].Connected }).Lobby
	require.NotNil(t, hl.Players[game.PostEast], "пост за гостем сохраняется")
	assert.Equal(t, CallsignSecond, hl.Players[game.PostEast].Callsign)

	// Повторный Disconnect того же подключения безопасен.
	guestConn.Disconnect()
}

// nextError ждет следующее сообщение клиенту и разбирает его как ошибку.
func nextError(t *testing.T, f *fakeSocket) ErrorMessage {
	t.Helper()
	select {
	case m := <-f.msgs:
		em, ok := m.Payload.(ErrorMessage)
		require.True(t, ok, "payload %T, want ErrorMessage", m.Payload)
		return em
	case <-time.After(waitTimeout):
		require.FailNow(t, "нет сообщения error")
		return ErrorMessage{}
	}
}

// Раздел 3.1 контракта: команды плоские, поля рядом с type. BR-4.2: неизвестный
// позывной игнорируется, позывной напарника - callsign_taken, после "Готов" -
// callsign_locked.
func TestReceive_SetCallsign(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, guest := coopWithGuest(t, r)
	hostSock := newFakeSocket()
	hostConn := mustConnect(t, ch, host, hostSock)
	mustConnect(t, ch, guest, newFakeSocket())
	waitLobby(t, hostSock, func(l LobbyMessage) bool { return l.Lobby.Players[game.PostEast].Connected })

	hostConn.Receive(game.SetCallsign{Callsign: "Сокол"}, "set_callsign")
	lm := nextLobby(t, hostSock).Lobby
	assert.Equal(t, Callsign("Сокол"), lm.Players[game.PostWest].Callsign)

	hostConn.Receive(game.SetCallsign{Callsign: "Нет такого"}, "set_callsign")
	hostConn.Receive(game.SetCallsign{Callsign: "Второй"}, "set_callsign")
	assert.Equal(t, ErrorMessage{Type: ServerMessageError, Code: ErrorCallsignTaken, Message: ErrCallsignTaken.Error()},
		nextError(t, hostSock), "неизвестный позывной молча пропущен, следом - отказ")

	hostConn.Receive(game.SetReady{Ready: true}, "set_ready")
	assert.True(t, nextLobby(t, hostSock).Lobby.Players[game.PostWest].Ready)
	hostConn.Receive(game.SetCallsign{Callsign: "Заря"}, "set_callsign")
	assert.Equal(t, ErrorCallsignLocked, nextError(t, hostSock).Code)
}

// BR-5.6, NFR-5: Registry.Close закрывает клиентов с CloseReasonShutdown (-> 1001).
func TestRegistryClose_ShutsClients(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels)
	ch, host, guest := coopWithGuest(t, r)
	hostSock, guestSock := newFakeSocket(), newFakeSocket()
	mustConnect(t, ch, host, hostSock)
	mustConnect(t, ch, guest, guestSock)

	r.Close()

	assert.Equal(t, CloseReasonShutdown, waitClosed(t, hostSock))
	assert.Equal(t, CloseReasonShutdown, waitClosed(t, guestSock))
	r.Close() // повторный Close не паникует и не виснет
}

// После остановки канала его методы не виснут: горутины чтения сокетов
// могут вызвать их в любой момент.
func TestChannelStopped_NoDeadlock(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels)
	ch, host, guest := coopWithGuest(t, r)
	hostConn := mustConnect(t, ch, host, newFakeSocket())
	r.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := ch.Connect(t.Context(), guest, newFakeSocket())
		assert.Error(t, err, "Connect к остановленному каналу")
		hostConn.Receive(game.SetReady{Ready: true}, "set_ready")
		hostConn.Disconnect()
	}()

	select {
	case <-done:
	case <-time.After(waitTimeout):
		require.FailNow(t, "методы остановленного канала зависли")
	}
}

// BR-1.8, ADR-002: одновременные Connect/Receive/Disconnect из многих горутин -
// без гонок (-race) и без зависаний.
func TestConnect_Concurrent(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, guest := coopWithGuest(t, r)

	var wg sync.WaitGroup
	for i := range 50 {
		tok := host
		if i%2 == 1 {
			tok = guest
		}
		wg.Go(func() {
			conn, err := ch.Connect(t.Context(), tok, newFakeSocket())
			if !assert.NoError(t, err) { //nolint:testifylint // require в горутине нельзя: FailNow только из горутины теста
				return
			}
			conn.Receive(game.SetCallsign{Callsign: "Заря"}, "set_callsign")
			conn.Disconnect()
		})
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "одновременные подключения зависли")
	}

	// Канал все еще обслуживает подключения.
	sock := newFakeSocket()
	mustConnect(t, ch, host, sock)
	nextLobby(t, sock)
}

// EFR-5 (приемка), ADR-002: после Registry.Close горутины каналов завершаются.
// Не параллельный: считает горутины всего процесса.
//
//nolint:paralleltest // runtime.NumGoroutine считает горутины всего процесса
func TestRegistryClose_NoGoroutineLeak(t *testing.T) {
	base := runtime.NumGoroutine()

	r := NewRegistry(testMaxChannels)
	for range 20 {
		ch, host, guest := coopWithGuest(t, r)
		mustConnect(t, ch, host, newFakeSocket())
		mustConnect(t, ch, guest, newFakeSocket())
	}
	_, _, err := r.Create(t.Context(), game.ModeSolo)
	require.NoError(t, err)
	require.Greater(t, runtime.NumGoroutine(), base, "у каналов должны быть свои горутины")

	r.Close()

	waitGoroutines(t, base, "горутины каналов не завершились")
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

// BR-5.6: Registry.Remove останавливает канал. Клиенты закрываются с
// CloseReasonChannelClosed (-> 1000), горутина канала выходит.
func TestRegistryRemove_ClosesClients(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, guest := coopWithGuest(t, r)
	hostSock, guestSock := newFakeSocket(), newFakeSocket()
	mustConnect(t, ch, host, hostSock)
	mustConnect(t, ch, guest, guestSock)

	r.Remove(ch.ID())

	assert.Equal(t, CloseReasonChannelClosed, waitClosed(t, hostSock))
	assert.Equal(t, CloseReasonChannelClosed, waitClosed(t, guestSock))

	done := make(chan struct{})
	go func() { ch.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		require.FailNow(t, "горутина канала не вышла после Remove")
	}
}

// Connect к удаленному каналу: ErrChannelClosed, сокет закрыт каналом,
// чтобы ws-слой не держал его вечно.
func TestConnect_AfterRemove(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, _ := coopWithGuest(t, r)
	r.Remove(ch.ID())
	ch.Wait()

	sock := newFakeSocket()
	_, err := ch.Connect(t.Context(), host, sock)
	require.ErrorIs(t, err, ErrChannelClosed)
	assert.Equal(t, CloseReasonChannelClosed, waitClosed(t, sock))
}

// Connect и Join идут через горутину канала: одновременно они не дают гонки (-race),
// и гость, севший во время подключения хозяина, виден в лобби.
func TestConnect_ConcurrentWithJoin(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	for range 20 {
		ch, host, err := r.Create(t.Context(), game.ModeCoop)
		require.NoError(t, err)

		hostSock := newFakeSocket()
		var wg sync.WaitGroup
		wg.Go(func() {
			_, err := ch.Connect(t.Context(), host, hostSock)
			assert.NoError(t, err)
		})
		var guest Token
		wg.Go(func() {
			var err error
			guest, err = ch.Join(t.Context())
			assert.NoError(t, err)
		})
		wg.Wait()

		post, ok := ch.PostOf(guest)
		require.True(t, ok)
		assert.Equal(t, game.PostEast, post)
		mustConnect(t, ch, guest, newFakeSocket())
		waitLobby(t, hostSock, func(lm LobbyMessage) bool {
			east := lm.Lobby.Players[game.PostEast]
			return east != nil && east.Connected
		})
	}
}

// Лобби уходит не-Droppable и при каждой рассылке (BR-4.1): переполненный
// outbox не должен его выбросить.
func TestBroadcastLobby_NotDroppable(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, guest := coopWithGuest(t, r)
	hostSock := newFakeSocket()
	mustConnect(t, ch, host, hostSock)
	nextLobby(t, hostSock) // decodeLobby проверяет Droppable == false

	guestConn := mustConnect(t, ch, guest, newFakeSocket())
	nextLobby(t, hostSock) // рассылка при подключении
	guestConn.Disconnect()
	nextLobby(t, hostSock) // рассылка при отключении
}

// После Registry.Close новые каналы не создаются: их горутины уже никто
// не остановил бы.
func TestRegistry_CreateAfterClose(t *testing.T) {
	t.Parallel()

	r := NewRegistry(testMaxChannels)
	r.Close()

	_, _, err := r.Create(t.Context(), game.ModeCoop)
	require.ErrorIs(t, err, ErrRegistryClosed)
	assert.Equal(t, 0, r.Len())
}

// BR-4.1, контракт 3.1: вход гостя - изменение лобби. Подключённый хост
// сразу видит гостя на east (connected: false), до его сокета.
func TestJoin_BroadcastsLobby(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, err := r.Create(t.Context(), game.ModeCoop)
	require.NoError(t, err)

	hostSock := newFakeSocket()
	mustConnect(t, ch, host, hostSock)
	require.Nil(t, nextLobby(t, hostSock).Lobby.Players[game.PostEast], "гостя ещё нет")

	_, err = ch.Join(t.Context())
	require.NoError(t, err)

	lm := nextLobby(t, hostSock).Lobby
	require.NotNil(t, lm.Players[game.PostEast])
	assert.Equal(t, CallsignSecond, lm.Players[game.PostEast].Callsign)
	assert.False(t, lm.Players[game.PostEast].Connected, "сокет гостя ещё не открыт")
	assert.Equal(t, game.PostWest, lm.You)
}

// Join в остановленный канал: места раздаёт горутина канала, её уже нет - Join не
// зависает и возвращает ErrChannelClosed. Токен мёртвого канала гостю не нужен.
func TestJoin_StoppedChannel(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, _, err := r.Create(t.Context(), game.ModeCoop)
	require.NoError(t, err)
	r.Remove(ch.ID())
	ch.Wait()

	_, err = ch.Join(t.Context())
	assert.ErrorIs(t, err, ErrChannelClosed)
}
