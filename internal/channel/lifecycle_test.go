package channel

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/game"
)

const (
	specReconnectGrace = 60 * time.Second
	specLobbyIdleTTL   = 10 * time.Minute
)

// BR-5.1–5.3: время отсчитывается от обрыва, а успешный возврат
// отменяет старый срок. После второго обрыва начинается новый срок.
func TestLifecycle_ReconnectGrace(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)

		p.guestConn.Disconnect()
		synctest.Wait()
		east := lastLobby(t, p.hostSock).Players[game.PostEast]
		require.NotNil(t, east)
		assert.False(t, east.Connected)

		advance(specReconnectGrace - time.Second)
		_, err := p.ch.Join(t.Context())
		require.ErrorIs(t, err, ErrFull)

		guestSock := newFakeSocket()
		guestConn := mustConnect(t, p.ch, p.guest, guestSock)
		lobby := nextLobby(t, guestSock).Lobby
		require.Equal(t, game.PostEast, lobby.You)
		require.NotNil(t, lobby.Players[game.PostEast])
		assert.True(t, lobby.Players[game.PostEast].Connected)
		east = lastLobby(t, p.hostSock).Players[game.PostEast]
		require.NotNil(t, east)
		assert.True(t, east.Connected)

		advance(2 * time.Second)
		_, ok := p.ch.PostOf(p.guest)
		require.True(t, ok, "старый срок истёк после успешного возврата")
		require.Empty(t, drain(p.hostSock))

		guestConn.Disconnect()
		synctest.Wait()
		lastLobby(t, p.hostSock)
		advance(specReconnectGrace - time.Second)
		_, ok = p.ch.PostOf(p.guest)
		require.True(t, ok, "второму обрыву полагается полный срок возврата")

		advance(2 * time.Second)
		_, ok = p.ch.PostOf(p.guest)
		require.False(t, ok, "старый токен перестаёт действовать")
		_, err = p.ch.Connect(t.Context(), p.guest, newFakeSocket())
		require.ErrorIs(t, err, ErrUnauthorized)
		assert.Nil(t, lastLobby(t, p.hostSock).Players[game.PostEast])
		_, err = p.ch.Join(t.Context())
		require.NoError(t, err, "пост доступен новому гостю")
	})
}

// BR-5.3: истечение срока хозяина освобождает west и сохраняет лобби гостя.
func TestLifecycle_HostGraceExpires(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const grace = 5 * time.Second
		r := NewRegistry(testMaxChannels, WithReconnectGrace(grace))
		t.Cleanup(r.Close)
		p := newLobbyPair(t, r)

		p.hostConn.Disconnect()
		synctest.Wait()
		lobby := lastLobby(t, p.guestSock)
		require.NotNil(t, lobby.Players[game.PostWest])
		assert.False(t, lobby.Players[game.PostWest].Connected)
		advance(grace + defaultTick)

		assert.Nil(t, lastLobby(t, p.guestSock).Players[game.PostWest])
		_, ok := p.ch.PostOf(p.host)
		assert.False(t, ok)
		_, err := r.ByID(p.ch.ID())
		require.NoError(t, err)
	})
}

// BR-5.5: канал, в который так и не подключились, занимает слот только до TTL.
func TestLifecycle_IdleLobbyExpires(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		codes := newCodeSeq(t, "111111", "111111")
		r := NewRegistry(1, WithCodeGenerator(codes.next))
		t.Cleanup(r.Close)

		ch, _, err := r.Create(t.Context(), game.ModeCoop)
		require.NoError(t, err)

		advance(specLobbyIdleTTL - time.Second)
		_, err = r.ByID(ch.ID())
		require.NoError(t, err)

		advance(2 * time.Second)
		_, err = r.ByID(ch.ID())
		require.ErrorIs(t, err, ErrNotFound)
		_, err = r.ByCode(ch.Code())
		require.ErrorIs(t, err, ErrNotFound)
		select {
		case <-ch.done:
		default:
			t.Error("горутина канала живёт после истечения TTL")
		}

		replacement, _, err := r.Create(t.Context(), game.ModeCoop)
		require.NoError(t, err, "слот и код должны освободиться")
		assert.Equal(t, ch.Code(), replacement.Code())
	})
}

// BR-5.5: подключение прерывает период простоя, а обрыв начинает новый.
func TestLifecycle_IdleLobbyTimerResets(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		ch, tok, err := r.Create(t.Context(), game.ModeSolo)
		require.NoError(t, err)

		advance(specLobbyIdleTTL - time.Second)
		conn := mustConnect(t, ch, tok, newFakeSocket())
		advance(specLobbyIdleTTL + time.Second)
		_, err = r.ByID(ch.ID())
		require.NoError(t, err, "подключённый игрок удерживает канал")

		conn.Disconnect()
		synctest.Wait()
		advance(specLobbyIdleTTL - time.Second)
		_, err = r.ByID(ch.ID())
		require.NoError(t, err, "после обрыва начинается новый период простоя")

		advance(2 * time.Second)
		_, err = r.ByID(ch.ID())
		require.ErrorIs(t, err, ErrNotFound, "бот не удерживает пустое лобби")
	})
}

// BR-5.2–5.3: используем поля протокола, чтобы тест не зависел от будущих Go-типов смены.
func TestLifecycle_ShiftReconnectAndExpiry(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := bubbleRegistry(t)
		p := newLobbyPair(t, r)
		p.readyBoth()
		advance(4 * time.Second)
		drain(p.hostSock)
		drain(p.guestSock)

		p.guestConn.Disconnect()
		synctest.Wait()
		guestSock := newFakeSocket()
		reconnected := mustConnect(t, p.ch, p.guest, guestSock)
		synctest.Wait()

		lobby := nextLobby(t, guestSock).Lobby
		require.Equal(t, LobbyPhaseShift, lobby.Phase)
		require.NotNil(t, lobby.Players[game.PostEast])
		assert.True(t, lobby.Players[game.PostEast].Connected)

		snapshot := lastLifecycleSnapshot(t, drain(guestSock))
		require.NotNil(t, snapshot, "возврат в смену сразу отдаёт снимок")
		assert.Equal(t, "briefing", snapshot.Phase)
		assert.Equal(t, 40, snapshot.Energy)

		reconnected.Disconnect()
		synctest.Wait()
		assertShiftRunningDuringGrace(t, p.hostSock)

		advance(2 * time.Second)
		msgs := drain(p.hostSock)
		snapshot = lastLifecycleSnapshot(t, msgs)
		require.NotNil(t, snapshot)
		assert.True(t, snapshot.Paused, "после истечения срока смена на паузе")
		assertLostPartnerRadio(t, msgs)
	})
}

// assertShiftRunningDuringGrace вычитывает снимки порциями, чтобы буфер
// fakeSocket не переполнился до истечения срока.
func assertShiftRunningDuringGrace(t *testing.T, sock *fakeSocket) {
	t.Helper()
	for remaining := specReconnectGrace - time.Second; remaining > 0; {
		step := min(remaining, 5*time.Second)
		advance(step)
		remaining -= step
		for _, msg := range drain(sock) {
			decoded := decodeLifecycleMessage(t, msg)
			if decoded.Type == "game" && decoded.Snapshot != nil {
				require.False(t, decoded.Snapshot.Paused, "смена ещё в пределах срока возврата")
			}
		}
	}
}

// assertLostPartnerRadio проверяет, что системная реплика пришла один раз.
func assertLostPartnerRadio(t *testing.T, msgs []Message) {
	t.Helper()
	lines := 0
	for _, msg := range msgs {
		decoded := decodeLifecycleMessage(t, msg)
		if decoded.Type != "events" {
			continue
		}
		for _, ev := range decoded.Events {
			if ev.Kind == "radio" && ev.From == "sys" && ev.Text == "Напарник пропал из эфира" {
				lines++
			}
		}
	}
	assert.Equal(t, 1, lines, "напарник получает одну системную реплику")
}

// lastLifecycleSnapshot находит самый свежий игровой снимок в пачке сообщений.
func lastLifecycleSnapshot(t *testing.T, msgs []Message) *lifecycleSnapshot {
	t.Helper()
	for _, msg := range slices.Backward(msgs) {
		decoded := decodeLifecycleMessage(t, msg)
		if decoded.Type == "game" && decoded.Snapshot != nil {
			return decoded.Snapshot
		}
	}
	return nil
}

type lifecycleMessage struct {
	Type     string
	Snapshot *lifecycleSnapshot
	Events   []lifecycleEvent
}

type lifecycleSnapshot struct {
	Phase  string
	Energy int
	Paused bool
}

type lifecycleEvent struct {
	Kind string
	From string
	Text string
}

// decodeLifecycleMessage extracts the domain messages relevant to the lifecycle.
func decodeLifecycleMessage(t *testing.T, msg Message) lifecycleMessage {
	t.Helper()
	switch payload := msg.Payload.(type) {
	case game.Snapshot:
		return lifecycleMessage{Type: "game", Snapshot: &lifecycleSnapshot{
			Phase: string(payload.Phase), Energy: payload.Energy, Paused: payload.Paused,
		}}
	case []game.Event:
		decoded := lifecycleMessage{Type: "events"}
		for _, event := range payload {
			if radio, ok := event.(game.RadioEvent); ok {
				decoded.Events = append(decoded.Events, lifecycleEvent{Kind: "radio", From: string(radio.From), Text: radio.Text})
			}
		}
		return decoded
	default:
		return lifecycleMessage{}
	}
}
