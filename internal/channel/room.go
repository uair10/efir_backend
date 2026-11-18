package channel

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

// countdownFrom - длина отсчёта перед сменой.
const countdownFrom = 3 * time.Second

// room - состояние канала: посты, подключения, фаза лобби.
//
// Принадлежит горутине канала. Ссылка на room есть только у Channel.run
// (локальная переменная), в Channel её нет, поэтому снаружи до состояния
// не добраться: граница актора проверяется компилятором, а не комментарием.
//
// Методы room - обычный синхронный код без замков и горутин. Тесты могут
// звать их напрямую, время двигается через tick(dt).
type room struct {
	code Code
	mode GameMode
	log  *slog.Logger // логгер канала: channel_id, channel_code

	west *Player
	east *Player // nil - пост свободен

	conns map[Post]*Connection

	phase     LobbyPhase
	countdown time.Duration // сколько осталось до shift; значим только в starting
}

// newRoom сажает хоста на west с позывным "Первый"; в одиночной смене east
// сразу занимает бот. Возвращает токен хоста.
func newRoom(code Code, mode GameMode, log *slog.Logger) (*room, Token, error) {
	tok, err := newToken()
	if err != nil {
		return nil, Token{}, err
	}
	r := &room{
		code:  code,
		mode:  mode,
		log:   log,
		west:  newPlayer(CallsignFirst, tok),
		conns: make(map[Post]*Connection, postsPerChannel),
		phase: LobbyPhaseLobby,
	}
	if mode == GameModeSolo {
		r.east = newBot()
	}
	return r, tok, nil
}

// handle выполняет одно событие. Единственная точка входа в room снаружи.
func (r *room) handle(ev event) {
	switch ev := ev.(type) {
	case joinEvent:
		ev.respond(r.join(ev.log))
	case connectEvent:
		ev.respond(r.connect(ev))
	case postOfEvent:
		if post, ok := r.postOf(ev.tok); ok {
			ev.respond(post, nil)
		} else {
			ev.respond("", ErrUnauthorized)
		}
	case playerEvent:
		if p := r.playerAt(ev.post); p != nil {
			ev.respond(*p, nil)
		} else {
			ev.respond(Player{}, ErrNotFound)
		}
	case commandEvent:
		r.command(ev.conn, ev.cmd)
	case disconnectEvent:
		r.disconnect(ev.conn)
	}
}

// join сажает гостя на east с первым свободным позывным и рассылает лобби.
func (r *room) join(log *slog.Logger) (Token, error) {
	if r.east != nil {
		return Token{}, ErrFull
	}
	// Занят только позывной хозяина, свободный есть всегда. Проверка - на
	// случай, если постов или позывных станет иначе.
	cs, ok := firstFreeCallsign(r.west.Callsign)
	if !ok {
		return Token{}, ErrFull
	}
	tok, err := newToken()
	if err != nil {
		return Token{}, fmt.Errorf("channel: join: %w", err)
	}
	r.east = newPlayer(cs, tok)
	log.Info("guest joined", logging.Post(PostEast), slog.String("callsign", cs.String()))
	r.recompute()
	r.broadcastLobby()
	return tok, nil
}

// connect привязывает сокет к посту владельца tok. Старое подключение того
// же игрока закрывается с CloseReasonReplaced.
func (r *room) connect(ev connectEvent) (*Connection, error) {
	post, ok := r.postOf(ev.tok)
	if !ok {
		return nil, ErrUnauthorized
	}
	conn := &Connection{ch: ev.ch, post: post, sock: ev.sock, log: ev.log.With(logging.Post(post))}
	if old := r.conns[post]; old != nil {
		old.log.Info("socket replaced")
		old.sock.Close(CloseReasonReplaced)
	}
	r.conns[post] = conn
	conn.log.Info("socket connected")
	r.recompute()
	r.broadcastLobby()
	return conn, nil
}

// disconnect снимает подключение с поста. Пост за игроком остаётся.
// Устаревшая ручка (игрок уже переподключился) ничего не делает.
func (r *room) disconnect(conn *Connection) {
	if r.conns[conn.post] != conn {
		return
	}
	delete(r.conns, conn.post)
	r.recompute()
	r.broadcastLobby()
}

// command выполняет команду клиента. Успех - рассылка лобби,
// отказ - сообщение error только отправителю или тишина.
func (r *room) command(conn *Connection, cmd command) {
	if r.conns[conn.post] != conn {
		return // ручка заменена: старая вкладка ничего не решает
	}
	var err error
	switch cmd := cmd.(type) {
	case setCallsign:
		err = r.setCallsign(conn.post, cmd.Callsign)
	case setReady:
		err = r.setReady(conn.post, cmd.Ready)
	}
	if err != nil {
		conn.reject(cmd, err)
		return
	}
	r.recompute()
	r.broadcastLobby()
}

// reject сообщает клиенту об отказе, если у отказа есть код контракта.
// Остальное (мусор, не та фаза) - только в debug.
// Вызывается из горутины канала.
func (conn *Connection) reject(cmd command, err error) {
	conn.log.Debug("command rejected", slog.String("kind", cmd.kind()), logging.Err(err))
	if code, ok := errorCode(err); ok {
		conn.sock.Send(Message{Payload: newErrorMessage(code, err)})
	}
}

// setCallsign меняет позывной.
func (r *room) setCallsign(from Post, cs Callsign) error {
	if r.phase == LobbyPhaseShift {
		return errWrongPhase
	}
	if _, ok := ParseCallsign(string(cs)); !ok {
		return fmt.Errorf("%w: unknown callsign %q", errInvalidCommand, cs)
	}
	p := r.playerAt(from)
	if p.Ready {
		return ErrCallsignLocked
	}
	if mate := r.playerAt(mateOf(from)); mate != nil && mate.Callsign == cs {
		return ErrCallsignTaken
	}
	p.Callsign = cs
	return nil
}

// setReady меняет готовность.
func (r *room) setReady(from Post, ready bool) error {
	if r.phase == LobbyPhaseShift {
		return errWrongPhase
	}
	if ready && r.playerAt(mateOf(from)) == nil {
		return ErrPartnerMissing
	}
	r.playerAt(from).Ready = ready
	return nil
}

// tick - шаг времени. Двигает отсчёт и рассылает лобби, когда меняется
// показываемое число.
func (r *room) tick(dt time.Duration) {
	if r.phase != LobbyPhaseStarting {
		return
	}
	before := r.countdownSeconds()
	r.countdown -= dt
	if r.countdown <= 0 {
		r.startShift()
		r.broadcastLobby()
		return
	}
	if r.countdownSeconds() != before {
		r.broadcastLobby()
	}
}

// recompute приводит фазу в соответствие с игроками.
//
// Инварианты:
//   - starting только когда оба поста заняты, готовы и на связи;
//   - shift окончательный: обратно в лобби смена не возвращается.
func (r *room) recompute() {
	switch r.phase {
	case LobbyPhaseShift:
		return
	case LobbyPhaseLobby:
		if !r.canStart() {
			return
		}
		if r.mode == GameModeSolo {
			r.startShift() // без отсчёта
			return
		}
		r.phase = LobbyPhaseStarting
		r.countdown = countdownFrom
		r.log.Info("countdown started")
	case LobbyPhaseStarting:
		if !r.canStart() {
			r.phase = LobbyPhaseLobby
			r.countdown = 0
			r.log.Info("countdown cancelled")
		}
	}
}

// canStart - оба поста заняты, готовы и на связи. Бот всегда на связи.
// Отсчёт без гостя на связи не идёт: иначе смена начнётся без него.
func (r *room) canStart() bool {
	for _, post := range posts {
		p := r.playerAt(post)
		if p == nil || !p.Ready {
			return false
		}
		if _, online := r.conns[post]; !p.Bot && !online {
			return false
		}
	}
	return true
}

// startShift переводит канал в смену.
func (r *room) startShift() {
	r.phase = LobbyPhaseShift
	r.countdown = 0
	r.log.Info("shift started")
	// EFR-6+: здесь создаётся симуляция смены.
}

// countdownSeconds - число на табло: оставшееся время вверх до секунды.
func (r *room) countdownSeconds() int {
	return int((r.countdown + time.Second - 1) / time.Second)
}

// closeAll закрывает все сокеты причиной reason.
func (r *room) closeAll(reason CloseReason) {
	for _, conn := range r.conns {
		conn.sock.Close(reason)
	}
	clear(r.conns)
}

// broadcastLobby рассылает свежее лобби всем подключённым, каждому своё.
func (r *room) broadcastLobby() {
	for post, conn := range r.conns {
		conn.sock.Send(Message{Payload: r.lobbyFor(post)})
	}
}

// lobbyFor собирает лобби глазами поста you.
func (r *room) lobbyFor(you Post) LobbyMessage {
	players := make(map[Post]*LobbyPlayer, postsPerChannel)
	for _, post := range posts {
		p := r.playerAt(post)
		if p == nil {
			players[post] = nil
			continue
		}
		_, online := r.conns[post]
		players[post] = &LobbyPlayer{
			Callsign:  p.Callsign,
			Ready:     p.Ready,
			Bot:       p.Bot,
			Connected: p.Bot || online,
		}
	}
	var countdown *int
	if r.phase == LobbyPhaseStarting {
		countdown = new(r.countdownSeconds())
	}
	return LobbyMessage{Type: "lobby", Lobby: LobbyState{
		Code:      r.code.String(),
		Mode:      r.mode,
		Phase:     r.phase,
		Countdown: countdown,
		You:       you,
		Players:   players,
	}}
}

// playerAt - игрок на посту или nil.
func (r *room) playerAt(post Post) *Player {
	switch post {
	case PostWest:
		return r.west
	case PostEast:
		return r.east
	default:
		return nil
	}
}

// postOf - пост владельца tok. Бот не опознаётся: пустой токен не равен никакому.
func (r *room) postOf(tok Token) (Post, bool) {
	for _, post := range posts {
		if p := r.playerAt(post); p != nil && p.token.Equal(tok) {
			return post, true
		}
	}
	return "", false
}
