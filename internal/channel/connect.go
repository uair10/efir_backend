package channel

import (
	"encoding/json"
	"errors"
	"log/slog"
)

// Message - исходящее сообщение канала для сокета.
type Message struct {
	Payload   any  // кодируется в JSON пакетом ws
	Droppable bool // снимок: при переполнении outbox его можно выбросить
}

// LobbyMessage - сообщение "lobby": состояние лобби для одного игрока.
type LobbyMessage struct {
	Type  string     `json:"type"` // "lobby"
	Lobby LobbyState `json:"lobby"`
}

// LobbyPhase - состояние лобби.
type LobbyPhase string

// Фазы лобби. EFR-3 шлёт только LobbyPhaseLobby, остальные - для EFR-4+.
const (
	LobbyPhaseLobby    LobbyPhase = "lobby"
	LobbyPhaseStarting LobbyPhase = "starting"
	LobbyPhaseShift    LobbyPhase = "shift"
)

// LobbyState - лобби глазами игрока на посту You.
type LobbyState struct {
	Code      string                `json:"code"`
	Mode      GameMode              `json:"mode"`
	Phase     LobbyPhase            `json:"phase"`     // "lobby" | "starting" | "shift"
	Countdown *int                  `json:"countdown"` // секунды до старта; nil - отсчёта нет
	You       Post                  `json:"you"`       // пост получателя: лобби у каждого своё
	Players   map[Post]*LobbyPlayer `json:"players"`   // nil - пост свободен
}

// LobbyPlayer - игрок на посту, как его видит лобби.
type LobbyPlayer struct {
	Callsign  Callsign `json:"callsign"`
	Ready     bool     `json:"ready"`
	Bot       bool     `json:"bot"`
	Connected bool     `json:"connected"` // сокет подключён; бот - всегда true
}

// CloseReason - почему канал закрывает сокет. Код закрытия выбирает пакет ws.
type CloseReason string

// Причины закрытия сокета каналом.
const (
	CloseReasonReplaced      CloseReason = "replaced"       // тот же игрок подключился заново
	CloseReasonChannelClosed CloseReason = "channel_closed" // канал удалён
	CloseReasonShutdown      CloseReason = "shutdown"       // сервер останавливается
	CloseReasonInternal      CloseReason = "internal"       // баг на сервере при разборе сообщения
)

// Socket - что канал ждёт от сокета. Реализует пакет ws.
// Оба метода не блокируют: канал не должен ждать медленного клиента.
type Socket interface {
	Send(msg Message)
	Close(reason CloseReason)
}

// Connection - одно подключение игрока к каналу.
//
// Это ручка, а не игрок. Если игрок переподключился, старая ручка устарела:
// её Disconnect не должен сбить новое подключение.
type Connection struct {
	ch   *Channel
	post Post
	sock Socket
}

// Post - пост, к которому привязано подключение.
func (conn *Connection) Post() Post { return conn.post }

// Receive принимает сырое входящее сообщение сокета и разбирает его.
// Мусор (не JSON) игнорируется: клиент не должен ронять канал.
func (conn *Connection) Receive(data []byte) {
	if !json.Valid(data) {
		conn.ch.logger.Debug("garbage ignored", slog.String("post", string(conn.post)))
		return
	}
	// EFR-3: команд ещё нет. Позже: разобрать "type" и передать
	// действие в канал через conn.ch.do(...).
}

// Disconnect вызывается, когда сокет закрыт или оборвался. Идемпотентен:
// повторный вызов (или вызов от устаревшей ручки) ничего не делает.
func (conn *Connection) Disconnect() {
	err := conn.ch.do(func() error {
		// Устаревшая ручка: игрок уже переподключился, на посту другая Connection.
		if conn.ch.conns[conn.post] != conn {
			return nil
		}
		delete(conn.ch.conns, conn.post)
		conn.ch.broadcastLobby()
		return nil
	})
	// ErrChannelClosed - канал уже остановлен, отключать некого.
	if err != nil && !errors.Is(err, ErrChannelClosed) {
		conn.ch.logger.Error("disconnect", slog.Any("err", err))
	}
}
