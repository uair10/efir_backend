package channel

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/uair10/efir_backend/internal/game"
	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

// channelActor последовательно обрабатывает события канала. Им владеет
// горутина Channel.run, которая хранит здесь подключение каждого поста и смену.
type channelActor struct {
	code   Code
	log    *slog.Logger
	shift  *game.Shift
	tokens map[game.Post]Token
	conns  map[game.Post]*Connection

	reconnectGrace time.Duration
	lobbyIdleTTL   time.Duration
	reconnectUntil map[game.Post]time.Time
	idleSince      time.Time
}

// newChannelActor создаёт смену и выдаёт токен хоста.
func newChannelActor(code Code, mode game.Mode, log *slog.Logger) (*channelActor, Token, error) {
	tok, err := newToken()
	if err != nil {
		return nil, Token{}, err
	}
	return &channelActor{
		code:           code,
		log:            log,
		shift:          game.NewShift(mode),
		tokens:         map[game.Post]Token{game.PostWest: tok},
		conns:          make(map[game.Post]*Connection, postsPerChannel),
		reconnectGrace: defaultReconnectGrace,
		lobbyIdleTTL:   defaultLobbyIdleTTL,
		reconnectUntil: make(map[game.Post]time.Time, postsPerChannel),
		idleSince:      time.Now(),
	}, tok, nil
}

// handle выполняет одно событие и возвращает true при штатном уходе хозяина.
func (actor *channelActor) handle(ev event) bool {
	actor.expireDisconnected(time.Now())
	switch ev := ev.(type) {
	case joinEvent:
		ev.respond(actor.join(ev.log))
	case connectEvent:
		ev.respond(actor.connect(ev))
	case postOfEvent:
		if post, ok := actor.postOf(ev.tok); ok {
			ev.respond(post, nil)
		} else {
			ev.respond("", ErrUnauthorized)
		}
	case playerEvent:
		if p := actor.playerAt(ev.post); p != nil {
			ev.respond(*p, nil)
		} else {
			ev.respond(Player{}, ErrNotFound)
		}
	case commandEvent:
		actor.command(ev.conn, ev.cmd, ev.kind)
	case disconnectEvent:
		return actor.disconnect(ev.conn, ev.graceful)
	}
	return false
}

// join сажает гостя на east с первым свободным позывным и рассылает лобби.
func (actor *channelActor) join(log *slog.Logger) (Token, error) {
	tok, err := newToken()
	if err != nil {
		return Token{}, fmt.Errorf("channel: join: %w", err)
	}
	cs, err := actor.shift.JoinGuest()
	if err != nil {
		return Token{}, fmt.Errorf("channel: join: %w", err)
	}
	actor.tokens[game.PostEast] = tok
	log.Info("guest joined", logging.Post(game.PostEast), slog.String("callsign", cs.String()))
	actor.broadcastLobby()
	return tok, nil
}

// connect привязывает сокет к посту владельца tok. Старое подключение того
// же игрока закрывается с CloseReasonReplaced.
func (actor *channelActor) connect(ev connectEvent) (*Connection, error) {
	post, ok := actor.postOf(ev.tok)
	if !ok {
		return nil, ErrUnauthorized
	}
	conn := &Connection{ch: ev.ch, post: post, sock: ev.sock, log: ev.log.With(logging.Post(post))}
	if old := actor.conns[post]; old != nil {
		old.log.Info("socket replaced")
		old.sock.Close(CloseReasonReplaced)
	}
	actor.conns[post] = conn
	delete(actor.reconnectUntil, post)
	actor.idleSince = time.Time{}
	conn.log.Info("socket connected")
	before := actor.shift.Phase()
	actor.shift.SetConnected(post, true)
	actor.logLobbyTransition(before)
	actor.broadcastLobby()
	if actor.shift.Started() {
		actor.sendShiftSnapshot(conn, false)
	}
	return conn, nil
}

// disconnect возвращает true, если уход хозяина закрывает канал.
func (actor *channelActor) disconnect(conn *Connection, graceful bool) bool {
	if actor.conns[conn.post] != conn {
		return false
	}
	delete(actor.conns, conn.post)
	now := time.Now()
	if len(actor.conns) == 0 {
		actor.idleSince = now
	}
	before := actor.shift.Phase()
	actor.shift.SetConnected(conn.post, false)
	if graceful {
		if conn.post == game.PostWest {
			return true
		}
		delete(actor.tokens, conn.post)
		actor.shift.Leave(conn.post)
	} else {
		actor.reconnectUntil[conn.post] = now.Add(actor.reconnectGrace)
	}
	actor.logLobbyTransition(before)
	actor.broadcastLobby()
	return false
}

// expireDisconnected обрабатывает истёкшие сроки возврата игроков.
func (actor *channelActor) expireDisconnected(now time.Time) {
	before := actor.shift.Phase()
	lobbyChanged := false
	for post, deadline := range actor.reconnectUntil {
		if now.Before(deadline) {
			continue
		}
		delete(actor.reconnectUntil, post)
		delete(actor.tokens, post)
		update := actor.shift.ReconnectExpired(post)
		lobbyChanged = lobbyChanged || update.LobbyChanged
		update.LobbyChanged = false
		actor.publish(before, nil, update)
		before = actor.shift.Phase()
	}
	if lobbyChanged {
		actor.broadcastLobby()
	}
}

// idleExpired проверяет, истёк ли срок простоя лобби без подключённых игроков.
func (actor *channelActor) idleExpired(now time.Time) bool {
	return actor.shift.Phase() == game.PhaseLobby && !actor.idleSince.IsZero() && now.Sub(actor.idleSince) >= actor.lobbyIdleTTL
}

// command передаёт команду смене, только если подключение игрока всё ещё актуально.
func (actor *channelActor) command(conn *Connection, cmd game.Command, kind string) {
	if actor.conns[conn.post] != conn {
		return // связь перепривязана к другому подключению
	}
	before := actor.shift.Phase()
	update, err := actor.shift.Apply(conn.post, cmd)
	if err != nil {
		conn.reject(kind, err)
		return
	}
	actor.publish(before, conn, update)
}

// reject сообщает клиенту об отказе, если у отказа есть код контракта.
func (conn *Connection) reject(kind string, err error) {
	conn.log.Debug("command rejected", slog.String("kind", kind), logging.Err(err))
	if code, ok := errorCode(err); ok {
		conn.sock.Send(Message{Payload: newErrorMessage(code, err)})
	}
}

// tick передаёт смене прошедшее время и публикует результат.
func (actor *channelActor) tick(dt time.Duration) {
	before := actor.shift.Phase()
	actor.publish(before, nil, actor.shift.Tick(dt))
}

// logLobbyTransition записывает запуск или отмену отсчёта и начало смены.
func (actor *channelActor) logLobbyTransition(before game.Phase) {
	after := actor.shift.Phase()
	if before == after {
		return
	}
	switch after {
	case game.PhaseStarting:
		actor.log.Info("countdown started")
	case game.PhaseLobby:
		actor.log.Info("countdown cancelled")
	case game.PhaseBriefing:
		actor.log.Info("shift started")
	case game.PhaseDay, game.PhaseNight, game.PhaseReport, game.PhaseEnd:
		// Переходы после инструктажа не относятся к журналу лобби.
	}
}

// publish отправляет личный ответ только автору команды, а общие события и снимки — всем подключённым.
func (actor *channelActor) publish(before game.Phase, sender *Connection, update game.ShiftUpdate) {
	actor.logLobbyTransition(before)
	if update.LobbyChanged {
		actor.broadcastLobby()
	}
	if sender != nil && len(update.Reply) > 0 {
		sender.sock.Send(Message{Payload: update.Reply})
	}
	for _, conn := range actor.conns {
		if len(update.Events) > 0 {
			conn.sock.Send(Message{Payload: update.Events})
		}
		if update.SnapshotChanged {
			actor.sendShiftSnapshot(conn, true)
		}
	}
}

// closeAll закрывает все сокеты причиной reason.
func (actor *channelActor) closeAll(reason CloseReason) {
	for _, conn := range actor.conns {
		conn.sock.Close(reason)
	}
	clear(actor.conns)
}

// broadcastLobby рассылает свежее лобби всем подключённым, каждому своё.
func (actor *channelActor) broadcastLobby() {
	for post, conn := range actor.conns {
		conn.sock.Send(Message{Payload: actor.lobbyFor(post)})
	}
}

// lobbyFor собирает лобби глазами поста you.
func (actor *channelActor) lobbyFor(you game.Post) LobbyMessage {
	players := make(map[game.Post]*LobbyPlayer, postsPerChannel)
	for _, post := range posts {
		p, occupied := actor.shift.Participant(post)
		if !occupied {
			players[post] = nil
			continue
		}
		players[post] = &LobbyPlayer{
			Callsign:  p.Callsign,
			Ready:     p.Ready,
			Bot:       p.Bot,
			Connected: p.Connected,
		}
	}
	var countdown *int
	if actor.shift.Phase() == game.PhaseStarting {
		countdown = new(actor.shift.CountdownSeconds())
	}
	return LobbyMessage{Type: ServerMessageLobby, Lobby: LobbyState{
		Code:      actor.code.String(),
		Mode:      actor.shift.Mode(),
		Phase:     actor.shift.Phase(),
		Countdown: countdown,
		You:       you,
		Players:   players,
	}}
}

// playerAt - игрок на посту или nil.
func (actor *channelActor) playerAt(post game.Post) *Player {
	p, ok := actor.shift.Participant(post)
	if !ok {
		return nil
	}
	return &Player{Callsign: p.Callsign, Ready: p.Ready, Bot: p.Bot}
}

// postOf ищет пост по токену. Боту токен не выдаётся.
func (actor *channelActor) postOf(tok Token) (game.Post, bool) {
	for _, post := range posts {
		if actor.tokens[post].Equal(tok) {
			return post, true
		}
	}
	return "", false
}

// sendShiftSnapshot отправляет состояние смены глазами подключённого игрока.
func (actor *channelActor) sendShiftSnapshot(conn *Connection, droppable bool) {
	conn.sock.Send(Message{Payload: actor.shift.Snapshot(conn.post), Droppable: droppable})
}
