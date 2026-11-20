package channel

import (
	"errors"
	"log/slog"

	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

// Message - исходящее сообщение канала для сокета.
type Message struct {
	Payload   any  // кодируется в JSON пакетом ws
	Droppable bool // снимок: при переполнении outbox его можно выбросить
}

// ServerMessageType определяет тип исходящего WebSocket-сообщения.
type ServerMessageType string

// Типы сообщений сервера.
const (
	ServerMessageLobby  ServerMessageType = "lobby"
	ServerMessageError  ServerMessageType = "error"
	ServerMessageGame   ServerMessageType = "game"
	ServerMessageEvents ServerMessageType = "events"
)

// LobbyMessage - сообщение "lobby": состояние лобби для одного игрока.
type LobbyMessage struct {
	Type  ServerMessageType `json:"type"`
	Lobby LobbyState        `json:"lobby"`
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

// ErrorCode обозначает прикладную ошибку протокола.
type ErrorCode string

// Коды ошибок лобби.
const (
	ErrorCallsignTaken  ErrorCode = "callsign_taken"
	ErrorCallsignLocked ErrorCode = "callsign_locked"
	ErrorPartnerMissing ErrorCode = "partner_missing"
)

// ErrorMessage - прикладная ошибка клиенту. Не путать с кодом закрытия
// WebSocket: тот - транспорт (runErr), этот - JSON до/без закрытия.
//
// Коды - только из контракта (раздел 3.1): клиент не знает других.
type ErrorMessage struct {
	Type    ServerMessageType `json:"type"`
	Code    ErrorCode         `json:"code"`
	Message string            `json:"message,omitempty"` // текст для логов клиента
}

// newErrorMessage собирает ошибку ErrorMessage.
func newErrorMessage(code ErrorCode, err error) ErrorMessage {
	msg := ErrorMessage{Type: ServerMessageError, Code: code}
	if err != nil {
		msg.Message = err.Error()
	}
	return msg
}

// errorCode - код контракта для отказа команды. ok == false - отказ клиенту
// не сообщается (мусор, неизвестный позывной, чужой пост): только лог.
func errorCode(err error) (ErrorCode, bool) {
	switch {
	case errors.Is(err, ErrCallsignTaken):
		return ErrorCallsignTaken, true
	case errors.Is(err, ErrCallsignLocked):
		return ErrorCallsignLocked, true
	case errors.Is(err, ErrPartnerMissing):
		return ErrorPartnerMissing, true
	default:
		return "", false
	}
}

// Socket - что канал ждёт от сокета. Реализует пакет ws.
// Оба метода не блокируют: канал не должен ждать медленного клиента.
type Socket interface {
	Send(msg Message)
	Close(reason CloseReason)
}

// Connection - одно подключение игрока к каналу.
//
// Это ручка, а не игрок. Если игрок переподключился, старая ручка устарела:
// её команды и Disconnect не трогают новое подключение.
//
// Поля неизменяемы после создания: ручкой пользуются и горутина чтения
// сокета, и горутина канала.
type Connection struct {
	ch   *Channel
	post Post
	sock Socket
	log  *slog.Logger // логгер сессии: request_id апгрейда, channel_id, post
}

// Post - пост, к которому привязано подключение.
func (conn *Connection) Post() Post { return conn.post }

// Receive принимает сырое входящее сообщение сокета. Разбор - здесь, в
// горутине чтения; выполнение - в горутине канала. Ответа не ждёт: отказ
// канал сам отправит в сокет.
//
// Мусор (не JSON, неизвестный type) игнорируется с записью в debug,
// соединение живёт (SPEC, раздел 5).
func (conn *Connection) Receive(data []byte) {
	cmd, err := parseCommand(data)
	if err != nil {
		conn.log.Debug("message ignored", slog.Int("size", len(data)), logging.Err(err))
		return
	}
	conn.log.Debug("message received", slog.String("type", string(cmd.kind())), slog.Int("size", len(data)))
	conn.ch.tell(commandEvent{conn: conn, cmd: cmd}) // канал закрыт - отвечать некому
}

// Disconnect сообщает каналу об обрыве сокета. Не ждёт ответа.
// Идемпотентен: повторный вызов (или вызов от устаревшей ручки) ничего
// не делает.
func (conn *Connection) Disconnect() {
	conn.ch.tell(disconnectEvent{conn: conn})
}

// Leave сообщает о штатном закрытии сокета игроком.
func (conn *Connection) Leave() {
	conn.ch.tell(disconnectEvent{conn: conn, graceful: true})
}
