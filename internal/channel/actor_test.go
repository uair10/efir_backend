package channel

// Тесты channelActor напрямую: обычный синхронный код, без горутин, тикеров
// и synctest. Время двигается вызовом tick(dt) (ADR-002, ADR-003).

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/game"

	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

// testActor - channelActor с хозяином и сокетом хозяина.
type testActor struct {
	*channelActor

	hostConn *Connection
	hostSock *fakeSocket
}

// newTestActor создаёт актор для проверки событий и тактов без фоновой горутины.
func newTestActor(t *testing.T, mode game.Mode) testActor {
	t.Helper()
	actor, host, err := newChannelActor(mustCode(t, "482117"), mode, logging.Discard())
	require.NoError(t, err)
	ta := testActor{channelActor: actor, hostSock: newFakeSocket()}
	ta.hostConn = ta.mustConnect(t, host, ta.hostSock)
	drain(ta.hostSock)
	return ta
}

// mustConnect подключает тестовый сокет и прерывает тест при ошибке.
func (actor testActor) mustConnect(t *testing.T, tok Token, sock Socket) *Connection {
	t.Helper()
	conn, err := actor.connect(connectEvent{tok: tok, sock: sock, log: logging.Discard()})
	require.NoError(t, err)
	return conn
}

// readyCoop - гость сел, подключился, оба готовы.
func (actor testActor) readyCoop(t *testing.T) *Connection {
	t.Helper()
	guest, err := actor.join(logging.Discard())
	require.NoError(t, err)
	guestConn := actor.mustConnect(t, guest, newFakeSocket())
	actor.command(actor.hostConn, game.SetReady{Ready: true}, "set_ready")
	actor.command(guestConn, game.SetReady{Ready: true}, "set_ready")
	drain(actor.hostSock)
	return guestConn
}

// BR-4.4: отсчёт считается от тактов; лобби уходит только при смене числа.
func TestActor_CountdownByTicks(t *testing.T) {
	t.Parallel()

	actor := newTestActor(t, game.ModeCoop)
	actor.readyCoop(t)
	require.Equal(t, LobbyPhaseStarting, actor.shift.LobbyPhase())

	step := 50 * time.Millisecond
	var seen []any
	for range 3 * time.Second / step {
		actor.tick(step)
		for _, l := range lobbies(t, drain(actor.hostSock)) {
			if l.Countdown == nil {
				seen = append(seen, l.Phase)
			} else {
				seen = append(seen, *l.Countdown)
			}
		}
	}
	assert.Equal(t, []any{2, 1, LobbyPhaseShift}, seen)
}

// BR-4.5: снятие готовности в отсчёте возвращает лобби; такты больше ничего не шлют.
func TestActor_UnreadyCancelsCountdown(t *testing.T) {
	t.Parallel()

	actor := newTestActor(t, game.ModeCoop)
	guestConn := actor.readyCoop(t)
	actor.tick(time.Second)
	drain(actor.hostSock)

	actor.command(guestConn, game.SetReady{Ready: false}, "set_ready")
	assert.Equal(t, LobbyPhaseLobby, actor.shift.LobbyPhase())
	drain(actor.hostSock)

	actor.tick(time.Minute)
	assert.Empty(t, drain(actor.hostSock))
}

// BR-4.6: solo - shift сразу на "Готов".
func TestActor_SoloShiftOnReady(t *testing.T) {
	t.Parallel()

	actor := newTestActor(t, game.ModeSolo)
	actor.command(actor.hostConn, game.SetReady{Ready: true}, "set_ready")
	assert.Equal(t, LobbyPhaseShift, actor.shift.LobbyPhase())
}

// Shift окончательный: обрыв и возврат игрока не запускают отсчёт снова.
func TestActor_ShiftIsFinal(t *testing.T) {
	t.Parallel()

	actor := newTestActor(t, game.ModeCoop)
	actor.readyCoop(t)
	actor.tick(3 * time.Second)
	require.Equal(t, LobbyPhaseShift, actor.shift.LobbyPhase())

	actor.disconnect(actor.hostConn, false)
	actor.mustConnect(t, actor.tokens[game.PostWest], newFakeSocket())
	assert.Equal(t, LobbyPhaseShift, actor.shift.LobbyPhase())
}

// Команда паникует - Channel пишет error, закрывает сокет отправителя (1011),
// канал продолжает работу (раздел 5).
func TestChannel_CommandPanicClosesSender(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	ch, host, guest := coopWithGuest(t, r)
	hostSock := newFakeSocket()
	mustConnect(t, ch, host, hostSock)

	// Рассылка лобби после команды гостя паникует на его сокете.
	panicky := &panicSocket{fakeSocket: newFakeSocket()}
	conn := mustConnect(t, ch, guest, panicky)
	conn.Receive(game.SetCallsign{Callsign: "Заря"}, "set_callsign")

	assert.Equal(t, CloseReasonInternal, waitClosed(t, panicky.fakeSocket))

	// Канал жив.
	_, ok := ch.PostOf(host)
	assert.True(t, ok)
}

// panicSocket паникует на Send после первого вызова (первый - лобби при подключении).
type panicSocket struct {
	*fakeSocket

	sends int
}

func (p *panicSocket) Send(m Message) {
	p.sends++
	if p.sends > 1 {
		panic("boom")
	}
	p.fakeSocket.Send(m)
}
