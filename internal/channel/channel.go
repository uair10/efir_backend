package channel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

// errEventPanic - обработка события в горутине канала запаниковала.
var errEventPanic = errors.New("channel: event panic")

// postsPerChannel - постов в канале: west и east.
const postsPerChannel = 2

// Channel - комната на одну смену. Через неё два человека (или человек и бот)
// находят друг друга и вместе играют.
//
// Модель - актор (ADR-002). Channel - только ручка: неизменяемые поля и
// почтовый ящик. Состояние (room) живёт в горутине run и наружу не
// попадает. Методы Channel превращают вызов в событие (events.go) и, если
// нужен ответ, ждут его.
type Channel struct {
	id      string
	code    Code
	mode    GameMode
	created time.Time
	tick    time.Duration
	log     *slog.Logger // channel_id, channel_code

	inbox chan event
	stop  chan CloseReason // буфер 1: Stop не блокируется и идемпотентен
	done  chan struct{}    // закрыт run при выходе
}

// channelConfig - всё, что нужно для запуска канала. Выдаёт реестр.
type channelConfig struct {
	id   string
	code Code
	mode GameMode
	tick time.Duration // шаг времени канала (GAME_TICK)
	log  *slog.Logger  // базовый логгер; канал добавит channel_id и channel_code
}

// startChannel собирает канал, сажает хоста и запускает горутину канала.
// Возвращает токен хоста: наружу канал его больше не отдаёт.
func startChannel(cfg channelConfig) (*Channel, Token, error) {
	log := cfg.log.With(logging.ChannelID(cfg.id), logging.ChannelCode(cfg.code.String()))
	r, tok, err := newRoom(cfg.code, cfg.mode, log)
	if err != nil {
		return nil, Token{}, err
	}
	c := &Channel{
		id:      cfg.id,
		code:    cfg.code,
		mode:    cfg.mode,
		created: time.Now(),
		tick:    cfg.tick,
		log:     log,
		inbox:   make(chan event),
		stop:    make(chan CloseReason, 1),
		done:    make(chan struct{}),
	}
	go c.run(r)
	return c, tok, nil
}

// Stop просит канал остановиться: закрыть сокеты причиной reason и выйти.
// Идемпотентен: повторный вызов (или вызов после выхода) ничего не делает.
func (c *Channel) Stop(reason CloseReason) {
	select {
	case c.stop <- reason:
	default: // stop уже отправлен
	}
}

// Wait блокируется, пока горутина канала не выйдет.
func (c *Channel) Wait() { <-c.done }

// ID - внутренний идентификатор "c_…" для подключения сокета.
func (c *Channel) ID() string { return c.id }

// Code - публичный 6-значный код для входа по нему.
func (c *Channel) Code() Code { return c.code }

// Mode - режим смены, solo или coop.
func (c *Channel) Mode() GameMode { return c.mode }

// Join сажает гостя на east с первым свободным позывным, рассылает лобби
// подключённым и возвращает токен гостя. Событие входа пишется в лог
// логгером из ctx (с request_id).
//
// Ошибки:
//   - ErrFull - east занят (гостем или ботом);
//   - ErrChannelClosed - канал остановлен;
//   - ошибка генерации токена.
func (c *Channel) Join(ctx context.Context) (Token, error) {
	req := newRequest[Token]()
	return ask(c, req, joinEvent{request: req, log: c.requestLog(ctx)})
}

// Player возвращает копию игрока на посту. ok == false - пост свободен
// или канал остановлен.
func (c *Channel) Player(post Post) (Player, bool) {
	req := newRequest[Player]()
	p, err := ask(c, req, playerEvent{request: req, post: post})
	return p, err == nil
}

// PostOf опознаёт игрока по токену. ok == false - токен не из этого канала
// или канал остановлен. Бот не опознаётся ничем: у него пустой токен.
func (c *Channel) PostOf(tok Token) (Post, bool) {
	req := newRequest[Post]()
	post, err := ask(c, req, postOfEvent{request: req, tok: tok})
	return post, err == nil
}

// Connect привязывает сокет к посту владельца tok. Старое подключение того
// же игрока закрывается с CloseReasonReplaced, всем подключённым уходит
// свежее лобби. Логгер из ctx (с request_id апгрейда) становится логгером
// сессии.
//
// Ошибки:
//   - ErrUnauthorized - токен не принадлежит ни одному посту канала.
//     Сокет остаётся за вызывающим.
//   - ErrChannelClosed - канал остановлен. Сокет уже закрыт причиной
//     CloseReasonChannelClosed.
func (c *Channel) Connect(ctx context.Context, tok Token, sock Socket) (*Connection, error) {
	req := newRequest[*Connection]()
	conn, err := ask(c, req, connectEvent{request: req, ch: c, tok: tok, sock: sock, log: c.requestLog(ctx)})
	if err != nil {
		if errors.Is(err, ErrChannelClosed) {
			sock.Close(CloseReasonChannelClosed)
		}
		return nil, err
	}
	return conn, nil
}

// requestLog - логгер запроса из ctx с id этого канала.
func (c *Channel) requestLog(ctx context.Context) *slog.Logger {
	return logging.FromCtx(ctx).With(logging.ChannelID(c.id))
}

// run - горутина канала. Единственная, кто видит room.
//
// Тикер работает всё время жизни канала: в смене (EFR-8) он нужен каждый
// такт, а в лобби пустой такт стоит одну проверку фазы.
func (c *Channel) run(r *room) {
	defer close(c.done)
	ticker := time.NewTicker(c.tick)
	defer ticker.Stop()
	for {
		select {
		case ev := <-c.inbox:
			c.handle(r, ev)
		case <-ticker.C:
			c.tickRoom(r)
		case reason := <-c.stop:
			r.closeAll(reason)
			c.log.Info("channel closed",
				slog.String("reason", string(reason)),
				slog.Duration("lifetime", time.Since(c.created)),
			)
			return
		}
	}
}

// handle выполняет событие. Паника - баг сервера: пишем со стеком,
// ждущему отвечаем ошибкой, канал живёт дальше.
func (c *Channel) handle(r *room, ev event) {
	defer func() {
		if v := recover(); v != nil {
			c.logPanic(fmt.Sprintf("%T", ev), v)
			ev.abort(fmt.Errorf("%w: %v", errEventPanic, v))
		}
	}()
	r.handle(ev)
}

// tickRoom - такт с той же защитой от паники, что и handle.
func (c *Channel) tickRoom(r *room) {
	defer func() {
		if v := recover(); v != nil {
			c.logPanic("tick", v)
		}
	}()
	r.tick(c.tick)
}

func (c *Channel) logPanic(where string, v any) {
	c.log.Error("channel panic",
		slog.String("in", where),
		slog.Any("panic", v),
		slog.String("stack", string(debug.Stack())),
	)
}
