package channel

// Тесты EFR-4: правила лобби (BR-4.1–4.6) через публичные методы канала.
//
// Все тесты идут в testing/synctest:
//   - Receive не ждёт ответа канала, поэтому после команды synctest.Wait
//     ждёт, пока горутина канала её обработает (send);
//   - тикер канала идёт по фейковым часам: time.Sleep(time.Second) двигает
//     их мгновенно (advance).
//
// Реестр создаётся внутри пузыря (bubbleRegistry), чтобы горутины каналов
// были в нём же.
//
// Синхронные тесты самого room, без горутин, - в room_test.go.

import (
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bubbleRegistry - реестр внутри пузыря synctest. Cleanup пузыря закрывает
// его до того, как synctest.Test начнёт ждать горутины каналов.
func bubbleRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry(testMaxChannels)
	t.Cleanup(r.Close)
	return r
}

// send передаёт сообщение клиента и ждёт, пока канал его обработает.
func send(conn *Connection, raw string) {
	conn.Receive([]byte(raw))
	synctest.Wait()
}

// advance двигает фейковые часы и ждёт, пока канал отработает такты.
func advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// lobbyPair - канал на двоих: оба подключены, начальные лобби вычитаны.
type lobbyPair struct {
	ch                  *Channel
	host, guest         Token
	hostConn, guestConn *Connection
	hostSock, guestSock *fakeSocket
}

// newLobbyPair открывает канал на двоих и подключает хозяина и гостя.
// После возврата в сокетах нет непрочитанных сообщений.
func newLobbyPair(t *testing.T, r *Registry) lobbyPair {
	t.Helper()
	ch, host, guest := coopWithGuest(t, r)
	p := lobbyPair{ch: ch, host: host, guest: guest, hostSock: newFakeSocket(), guestSock: newFakeSocket()}
	p.hostConn = mustConnect(t, ch, host, p.hostSock)
	p.guestConn = mustConnect(t, ch, guest, p.guestSock)
	drain(p.hostSock)
	drain(p.guestSock)
	return p
}

// readyBoth жмёт "Готов" у обоих и вычитывает лобби после этого.
func (p lobbyPair) readyBoth() {
	send(p.hostConn, `{"type":"set_ready","ready":true}`)
	send(p.guestConn, `{"type":"set_ready","ready":true}`)
	drain(p.hostSock)
	drain(p.guestSock)
}

// drain забирает всё, что уже лежит в сокете, не дожидаясь новых сообщений.
func drain(f *fakeSocket) []Message {
	var out []Message
	for {
		select {
		case m := <-f.msgs:
			out = append(out, m)
		default:
			return out
		}
	}
}

// lobbies - все лобби из сообщений; не-лобби валят тест.
func lobbies(t *testing.T, msgs []Message) []LobbyState {
	t.Helper()
	out := make([]LobbyState, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, decodeLobby(t, m).Lobby)
	}
	return out
}

// lastLobby - последнее лобби из уже пришедших сообщений.
func lastLobby(t *testing.T, f *fakeSocket) LobbyState {
	t.Helper()
	ls := lobbies(t, drain(f))
	require.NotEmpty(t, ls, "лобби не пришло")
	return ls[len(ls)-1]
}

// onlyError - в сокете ровно одно сообщение, и это error.
func onlyError(t *testing.T, f *fakeSocket) ErrorMessage {
	t.Helper()
	msgs := drain(f)
	require.Len(t, msgs, 1)
	em, ok := msgs[0].Payload.(ErrorMessage)
	require.True(t, ok, "payload %T, want ErrorMessage", msgs[0].Payload)
	return em
}

// BR-4.1: после каждого изменения оба игрока получают лобби, каждый со своим you.
func TestLobby_EveryChangeBroadcastsToBoth(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)

		for _, cmd := range []string{
			`{"type":"set_callsign","callsign":"Сокол"}`,
			`{"type":"set_ready","ready":true}`,
			`{"type":"set_ready","ready":false}`,
		} {
			send(p.guestConn, cmd)

			hl := lobbies(t, drain(p.hostSock))
			gl := lobbies(t, drain(p.guestSock))
			require.Len(t, hl, 1, "хозяин: одно лобби на изменение (%s)", cmd)
			require.Len(t, gl, 1, "гость: одно лобби на изменение (%s)", cmd)
			assert.Equal(t, PostWest, hl[0].You)
			assert.Equal(t, PostEast, gl[0].You)
			assert.Equal(t, hl[0].Players, gl[0].Players, "посты оба видят одинаково")
		}

		guest, ok := p.ch.Player(PostEast)
		require.True(t, ok)
		assert.Equal(t, Callsign("Сокол"), guest.Callsign)
	})
}

// BR-4.2: неизвестный позывной игнорируется - ни ошибки, ни лобби.
func TestLobby_SetCallsign_UnknownIgnored(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)

		for _, cs := range []string{`"Нет такого"`, `""`, `"первый"`, `42`} {
			send(p.hostConn, `{"type":"set_callsign","callsign":`+cs+`}`)
		}
		assert.Empty(t, drain(p.hostSock), "отправителю ничего не шлём")
		assert.Empty(t, drain(p.guestSock), "напарнику ничего не шлём")
	})
}

// BR-4.2: позывной напарника - callsign_taken, и только отправителю.
// Лобби при отказе не рассылается: ничего не изменилось.
func TestLobby_SetCallsign_TakenGoesToSenderOnly(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)

		send(p.guestConn, `{"type":"set_callsign","callsign":"Первый"}`)

		assert.Equal(t, ErrorMessage{Type: ServerMessageError, Code: ErrorCallsignTaken, Message: ErrCallsignTaken.Error()},
			onlyError(t, p.guestSock))
		assert.Empty(t, drain(p.hostSock), "напарнику отказ не шлём")
	})
}

// BR-4.2: свой текущий позывной - не "занят напарником".
func TestLobby_SetCallsign_OwnIsNotTaken(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)

		send(p.hostConn, `{"type":"set_callsign","callsign":"Первый"}`)

		assert.Equal(t, CallsignFirst, lastLobby(t, p.hostSock).Players[PostWest].Callsign)
	})
}

// BR-4.2, BR-1.3: в одиночной смене позывной бота занят.
func TestLobby_SetCallsign_BotsCallsignTaken(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		ch, host, err := r.Create(t.Context(), GameModeSolo)
		require.NoError(t, err)
		sock := newFakeSocket()
		conn := mustConnect(t, ch, host, sock)
		drain(sock)

		send(conn, `{"type":"set_callsign","callsign":"Второй"}`)
		assert.Equal(t, ErrorCallsignTaken, onlyError(t, sock).Code)
	})
}

// BR-4.2: после "Готов" позывной заблокирован, после снятия - снова меняется.
func TestLobby_SetCallsign_LockedWhileReady(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)

		send(p.hostConn, `{"type":"set_ready","ready":true}`)
		drain(p.hostSock)
		send(p.hostConn, `{"type":"set_callsign","callsign":"Заря"}`)
		assert.Equal(t, ErrorCallsignLocked, onlyError(t, p.hostSock).Code)

		send(p.hostConn, `{"type":"set_ready","ready":false}`)
		drain(p.hostSock)
		send(p.hostConn, `{"type":"set_callsign","callsign":"Заря"}`)
		assert.Equal(t, Callsign("Заря"), lastLobby(t, p.hostSock).Players[PostWest].Callsign)
	})
}

// BR-4.3: "Готов", когда в канале один, - partner_missing; готовность не ставится.
func TestLobby_SetReady_PartnerMissing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		ch, host, err := r.Create(t.Context(), GameModeCoop)
		require.NoError(t, err)
		sock := newFakeSocket()
		conn := mustConnect(t, ch, host, sock)
		drain(sock)

		send(conn, `{"type":"set_ready","ready":true}`)
		assert.Equal(t, ErrorPartnerMissing, onlyError(t, sock).Code, "лобби не меняется")

		// "Не готов" в одиночестве - не ошибка.
		send(conn, `{"type":"set_ready","ready":false}`)
		assert.False(t, lastLobby(t, sock).Players[PostWest].Ready)

		// Гость пришёл - теперь можно.
		_, err = ch.Join(t.Context())
		require.NoError(t, err)
		drain(sock)
		send(conn, `{"type":"set_ready","ready":true}`)
		assert.True(t, lastLobby(t, sock).Players[PostWest].Ready)
	})
}

// Контракт 3.1: ошибка кодируется ровно так, как ждёт фронтенд.
func TestErrorMessage_JSON(t *testing.T) {
	t.Parallel()

	b, err := json.Marshal(newErrorMessage("callsign_taken", nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"error","code":"callsign_taken"}`, string(b))
}

// BR-4.4: оба готовы - starting/3, раз в секунду 2, 1, на нуле shift/null.
func TestLobby_Countdown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)

		send(p.hostConn, `{"type":"set_ready","ready":true}`)
		l := lastLobby(t, p.guestSock)
		assert.Equal(t, LobbyPhaseLobby, l.Phase, "готов один - отсчёта нет")
		assert.Nil(t, l.Countdown)

		send(p.guestConn, `{"type":"set_ready","ready":true}`)
		l = lastLobby(t, p.hostSock)
		assert.Equal(t, LobbyPhaseStarting, l.Phase)
		assert.Equal(t, new(3), l.Countdown)
		drain(p.guestSock)

		// Тикер канала отсчитывает от своего запуска, а не от "Готов":
		// первая смена числа - не позже чем через секунду и такт.
		advance(time.Second + defaultTick)
		for _, sock := range []*fakeSocket{p.hostSock, p.guestSock} {
			ls := lobbies(t, drain(sock))
			require.Len(t, ls, 1)
			assert.Equal(t, LobbyPhaseStarting, ls[0].Phase)
			assert.Equal(t, new(2), ls[0].Countdown)
		}

		advance(time.Second)
		ls := lobbies(t, drain(p.hostSock))
		require.Len(t, ls, 1)
		assert.Equal(t, new(1), ls[0].Countdown)
		drain(p.guestSock)

		advance(time.Second)
		for _, sock := range []*fakeSocket{p.hostSock, p.guestSock} {
			ls := lobbies(t, drain(sock))
			require.Len(t, ls, 1)
			assert.Equal(t, LobbyPhaseShift, ls[0].Phase)
			assert.Nil(t, ls[0].Countdown)
		}

		advance(10 * time.Second)
		assert.Empty(t, drain(p.hostSock), "после shift отсчёт не тикает")
	})
}

// BR-4.5: "Не готов" во время отсчёта - lobby/null, отсчёт остановлен.
// Повторный "Готов" начинает отсчёт заново с 3.
func TestLobby_CountdownCancelledByUnready(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)
		p.readyBoth()

		advance(1500 * time.Millisecond) // на табло 2 или 1
		drain(p.hostSock)

		send(p.guestConn, `{"type":"set_ready","ready":false}`)
		for _, sock := range []*fakeSocket{p.hostSock, p.guestSock} {
			l := lastLobby(t, sock)
			assert.Equal(t, LobbyPhaseLobby, l.Phase)
			assert.Nil(t, l.Countdown)
		}

		advance(10 * time.Second)
		assert.Empty(t, drain(p.hostSock), "отменённый отсчёт не тикает")

		send(p.guestConn, `{"type":"set_ready","ready":true}`)
		l := lastLobby(t, p.hostSock)
		assert.Equal(t, LobbyPhaseStarting, l.Phase)
		assert.Equal(t, new(3), l.Countdown, "отсчёт заново, а не с места остановки")
	})
}

// Отсчёт требует обоих на связи: гость отвалился - отсчёт отменён
// (иначе смена начнётся без него). Контракт об этом молчит, решение сервера.
func TestLobby_CountdownCancelledByDisconnect(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)
		p.readyBoth()

		p.guestConn.Disconnect()
		synctest.Wait()
		l := lastLobby(t, p.hostSock)
		assert.Equal(t, LobbyPhaseLobby, l.Phase)
		assert.Nil(t, l.Countdown)
		assert.True(t, l.Players[PostEast].Ready, "готовность за гостем остаётся")

		advance(10 * time.Second)
		assert.Empty(t, drain(p.hostSock))
	})
}

// BR-4.6: одиночная смена - "Готов" сразу даёт shift, без отсчёта.
func TestLobby_SoloStartsImmediately(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		ch, host, err := r.Create(t.Context(), GameModeSolo)
		require.NoError(t, err)
		sock := newFakeSocket()
		conn := mustConnect(t, ch, host, sock)
		drain(sock)

		send(conn, `{"type":"set_ready","ready":true}`)
		ls := lobbies(t, drain(sock))
		require.Len(t, ls, 1)
		assert.Equal(t, LobbyPhaseShift, ls[0].Phase, "starting в одиночной смене не бывает")
		assert.Nil(t, ls[0].Countdown)

		advance(10 * time.Second)
		assert.Empty(t, drain(sock))
	})
}

// После shift лобби закрыто: переподключение не возвращает отсчёт,
// а set_ready/set_callsign игнорируются (команды не в своей фазе, ср. BR-8.8).
func TestLobby_ShiftIsFinal(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)
		p.readyBoth()
		advance(4 * time.Second)
		require.Equal(t, LobbyPhaseShift, lastLobby(t, p.hostSock).Phase)
		drain(p.guestSock)

		// Обрыв и возврат гостя посреди смены.
		p.guestConn.Disconnect()
		sock := newFakeSocket()
		mustConnect(t, p.ch, p.guest, sock)
		l := nextLobby(t, sock).Lobby
		assert.Equal(t, LobbyPhaseShift, l.Phase, "переподключение не перезапускает отсчёт")
		assert.Nil(t, l.Countdown)
		drain(p.hostSock)

		send(p.hostConn, `{"type":"set_ready","ready":false}`)
		send(p.hostConn, `{"type":"set_callsign","callsign":"Заря"}`)
		assert.Empty(t, drain(p.hostSock), "команды лобби в смене молча игнорируются")

		host, ok := p.ch.Player(PostWest)
		require.True(t, ok)
		assert.True(t, host.Ready)
		assert.Equal(t, CallsignFirst, host.Callsign)
	})
}

// BR-3.2: старая вкладка после замены ничего не решает - её команды
// игнорируются, хотя её горутина чтения ещё жива.
func TestLobby_ReplacedConnectionCommandsIgnored(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)
		newSock := newFakeSocket()
		mustConnect(t, p.ch, p.host, newSock)
		drain(newSock)
		drain(p.guestSock)

		send(p.hostConn, `{"type":"set_callsign","callsign":"Заря"}`)
		assert.Empty(t, drain(p.guestSock))

		host, ok := p.ch.Player(PostWest)
		require.True(t, ok)
		assert.Equal(t, CallsignFirst, host.Callsign)
	})
}
