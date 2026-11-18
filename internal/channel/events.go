package channel

import (
	"log/slog"
)

// event - сообщение горутине канала (ADR-002).
//
// Закрытый тип-сумма: все события перечислены ниже, room.handle разбирает
// их одним switch. Линтер gochecksumtype проверяет, что ни одно не забыто.
// Это и есть полный интерфейс актора: чего нет в этом списке, канал не умеет.
//
//sumtype:decl
type event interface {
	isEvent()
	// abort вызывается, если обработка события запаниковала: ответить
	// ждущему или закрыть сокет, чтобы никто не висел вечно.
	abort(err error)
}

// joinEvent - гость садится на свободный пост (Channel.Join).
type joinEvent struct {
	request[Token]

	log *slog.Logger // логгер запроса входа, с channel_id
}

// connectEvent - сокет игрока привязывается к посту (Channel.Connect).
type connectEvent struct {
	request[*Connection]

	ch   *Channel // ручка канала для Connection: через неё сессия шлёт события
	tok  Token
	sock Socket
	log  *slog.Logger // логгер сессии: request_id апгрейда, channel_id
}

// postOfEvent - на каком посту владелец токена (Channel.PostOf).
type postOfEvent struct {
	request[Post]

	tok Token
}

// playerEvent - копия игрока на посту (Channel.Player).
type playerEvent struct {
	request[Player]

	post Post
}

// commandEvent - команда от клиента. Без ответа: отказ room шлёт в сокет сам.
type commandEvent struct {
	conn *Connection
	cmd  command
}

// disconnectEvent - сокет закрылся. Без ответа.
type disconnectEvent struct {
	conn *Connection
}

func (joinEvent) isEvent()       {}
func (connectEvent) isEvent()    {}
func (postOfEvent) isEvent()     {}
func (playerEvent) isEvent()     {}
func (commandEvent) isEvent()    {}
func (disconnectEvent) isEvent() {}

// abort: команда упала - закрываем сокет отправителя кодом 1011 (раздел 5).
func (e commandEvent) abort(error) { e.conn.sock.Close(CloseReasonInternal) }

// abort: ждать некому.
func (disconnectEvent) abort(error) {}

// result - ответ горутины канала на запрос.
type result[T any] struct {
	val T
	err error
}

// request - половина события, которая ждёт ответа. Встраивается в событие.
type request[T any] struct {
	reply chan result[T] // буфер 1: горутина канала никогда не ждёт на ответе
}

func newRequest[T any]() request[T] {
	return request[T]{reply: make(chan result[T], 1)}
}

// respond отдаёт ответ. Вызывается ровно один раз.
func (r request[T]) respond(val T, err error) {
	r.reply <- result[T]{val: val, err: err}
}

// abort отвечает ошибкой, если ответа ещё не было.
func (r request[T]) abort(err error) {
	select {
	case r.reply <- result[T]{err: err}:
	default: // respond уже успел ответить
	}
}

// ask отправляет запрос горутине канала и ждёт ответ.
// ErrChannelClosed - канал остановлен до ответа.
func ask[T any](c *Channel, req request[T], ev event) (T, error) {
	var zero T
	if !c.tell(ev) {
		return zero, ErrChannelClosed
	}
	select {
	case res := <-req.reply:
		return res.val, res.err
	case <-c.done:
		// Канал мог ответить и сразу выйти: ответ важнее закрытия.
		select {
		case res := <-req.reply:
			return res.val, res.err
		default:
			return zero, ErrChannelClosed
		}
	}
}

// tell отправляет событие без ожидания ответа. false - канал остановлен.
func (c *Channel) tell(ev event) bool {
	select {
	case c.inbox <- ev:
		return true
	case <-c.done:
		return false
	}
}
