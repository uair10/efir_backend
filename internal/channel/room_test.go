package channel

// Тесты room напрямую: обычный синхронный код, без горутин, тикеров
// и synctest. Время двигается вызовом tick(dt) (ADR-002, ADR-003).

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

// testRoom - room с хозяином и сокетом хозяина.
type testRoom struct {
	*room

	hostConn *Connection
	hostSock *fakeSocket
}

func newTestRoom(t *testing.T, mode GameMode) testRoom {
	t.Helper()
	r, host, err := newRoom(mustCode(t, "482117"), mode, logging.Discard())
	require.NoError(t, err)
	tr := testRoom{room: r, hostSock: newFakeSocket()}
	tr.hostConn = tr.mustConnect(t, host, tr.hostSock)
	drain(tr.hostSock)
	return tr
}

func (r testRoom) mustConnect(t *testing.T, tok Token, sock Socket) *Connection {
	t.Helper()
	conn, err := r.connect(connectEvent{tok: tok, sock: sock, log: logging.Discard()})
	require.NoError(t, err)
	return conn
}

// readyCoop - гость сел, подключился, оба готовы.
func (r testRoom) readyCoop(t *testing.T) *Connection {
	t.Helper()
	guest, err := r.join(logging.Discard())
	require.NoError(t, err)
	guestConn := r.mustConnect(t, guest, newFakeSocket())
	r.command(r.hostConn, setReady{Ready: true})
	r.command(guestConn, setReady{Ready: true})
	drain(r.hostSock)
	return guestConn
}

// BR-4.4: отсчёт считается от тактов; лобби уходит только при смене числа.
func TestRoom_CountdownByTicks(t *testing.T) {
	t.Parallel()

	r := newTestRoom(t, GameModeCoop)
	r.readyCoop(t)
	require.Equal(t, LobbyPhaseStarting, r.phase)

	step := 50 * time.Millisecond
	var seen []any
	for range 3 * time.Second / step {
		r.tick(step)
		for _, l := range lobbies(t, drain(r.hostSock)) {
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
func TestRoom_UnreadyCancelsCountdown(t *testing.T) {
	t.Parallel()

	r := newTestRoom(t, GameModeCoop)
	guestConn := r.readyCoop(t)
	r.tick(time.Second)
	drain(r.hostSock)

	r.command(guestConn, setReady{Ready: false})
	assert.Equal(t, LobbyPhaseLobby, r.phase)
	drain(r.hostSock)

	r.tick(time.Minute)
	assert.Empty(t, drain(r.hostSock))
}

// BR-4.6: solo - shift сразу на "Готов".
func TestRoom_SoloShiftOnReady(t *testing.T) {
	t.Parallel()

	r := newTestRoom(t, GameModeSolo)
	r.command(r.hostConn, setReady{Ready: true})
	assert.Equal(t, LobbyPhaseShift, r.phase)
}

// Shift окончательный: обрыв и возврат игрока не запускают отсчёт снова.
func TestRoom_ShiftIsFinal(t *testing.T) {
	t.Parallel()

	r := newTestRoom(t, GameModeCoop)
	r.readyCoop(t)
	r.tick(countdownFrom)
	require.Equal(t, LobbyPhaseShift, r.phase)

	r.disconnect(r.hostConn, false)
	r.mustConnect(t, r.west.token, newFakeSocket())
	assert.Equal(t, LobbyPhaseShift, r.phase)
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
	conn.Receive([]byte(`{"type":"set_callsign","callsign":"Заря"}`))

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
