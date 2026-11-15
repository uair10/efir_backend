package channel

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// errJobPanic - задание в горутине канала запаниковало.
var errJobPanic = errors.New("channel: job panic")

// postsPerChannel - постов в канале: west и east.
const postsPerChannel = 2

// Channel - комната на одну смену. Через неё два человека (или человек и бот)
// находят друг друга и вместе играют.
//
// Модель - актор (ADR-002): у канала одна горутина loop, и только она трогает
// conns. Остальные кладут задания в inbox и ждут ответ через do. Сколько бы
// сокетов ни подключалось и ни отключалось одновременно, изменения идут по
// одному.
//
// west/east - отдельная история: их пишет Join (снаружи, под c.mu), а читает
// и loop, и Player/PostOf. Поэтому они защищены c.mu, а не актором.
type Channel struct {
	id   string
	code Code
	mode GameMode

	mu   sync.Mutex // защищает west и east; conns - только loop
	west *Player
	east *Player // nil - пост свободен

	inbox  chan func()          // задания для горутины канала (без буфера: do ждёт приёма)
	stop   chan CloseReason     // буфер 1: Stop не блокируется и идемпотентен
	done   chan struct{}        // закрыт горутиной при выходе
	conns  map[Post]*Connection // подключенные сокеты; трогает только loop
	logger *slog.Logger
}

// newChannel собирает канал и сажает хоста на west с позывным "Первый";
// в одиночной смене east сразу занимает бот. id и code выдаёт реестр.
//
// Канал ещё не запущен: Start() заводит горутину loop.
// Возвращает токен хоста. Наружу канал его больше не отдаёт.
func newChannel(id string, code Code, mode GameMode, logger *slog.Logger) (*Channel, Token, error) {
	tok, err := newToken()
	if err != nil {
		return nil, Token{}, err
	}
	ch := &Channel{
		id:     id,
		code:   code,
		mode:   mode,
		west:   newPlayer(CallsignFirst, tok),
		inbox:  make(chan func()),
		stop:   make(chan CloseReason, 1),
		done:   make(chan struct{}),
		conns:  make(map[Post]*Connection, postsPerChannel),
		logger: logger.With(slog.String("channel_id", id), slog.String("code", code.String())),
	}
	if mode == GameModeSolo {
		ch.east = newBot()
	}
	return ch, tok, nil
}

// Start запускает горутину канала. Вызывать один раз, после регистрации
// канала в реестре (чтобы ByID уже находил его).
func (c *Channel) Start() { go c.loop() }

// Stop просит канал остановиться: закрыть сокеты причиной reason и выйти.
// Идемпотентен: повторный вызов (или вызов после выхода loop) ничего не делает.
func (c *Channel) Stop(reason CloseReason) {
	select {
	case c.stop <- reason:
	default: // уже остановлен или stop уже отправлен
	}
}

// Wait блокируется, пока loop не выйдет.
func (c *Channel) Wait() { <-c.done }

// ID - внутренний идентификатор "c_…" для подключения сокета.
func (c *Channel) ID() string { return c.id }

// Code - публичный 6-значный код для входа по нему.
func (c *Channel) Code() Code { return c.code }

// Mode - режим смены, solo или coop.
func (c *Channel) Mode() GameMode { return c.mode }

// Join сажает гостя на east с первым свободным позывным, рассылает лобби
// подключённым и возвращает токен гостя. Канал гость нашел через
// Registry.ByCode. Канал должен быть запущен (Start), иначе Join зависнет.
//
// Проверка места и посадка идут под одним c.mu, иначе два гостя одновременно
// увидят свободный east и оба сядут.
//
// Ошибки:
//   - ErrFull - east занят (гостем или ботом). Сидящий на east не вытеснен.
//   - ошибка генерации токена.
func (c *Channel) Join() (Token, error) {
	tok, err := c.seatGuest()
	if err != nil {
		return Token{}, err
	}
	// BR-4.1, контракт 3.1: лобби - после каждого изменения. Хост сразу видит
	// гостя (connected: false), не дожидаясь, пока тот откроет сокет.
	// ErrChannelClosed не ошибка: канал удалили, рассылать некому.
	err = c.do(func() error {
		c.broadcastLobby()
		return nil
	})
	if err != nil && !errors.Is(err, ErrChannelClosed) {
		c.logger.Error("join: broadcast lobby", slog.Any("err", err))
	}
	return tok, nil
}

// Player возвращает копию игрока на посту. ok == false - пост свободен.
func (c *Channel) Player(post Post) (Player, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.playerAt(post)
	if p == nil {
		return Player{}, false
	}
	return *p, true
}

// PostOf опознает игрока по токену: на каком посту сидит владелец tok.
// ok == false - токен не из этого канала. Бот не опознается ничем:
// у него пустой токен, а Token.Equal с пустым всегда false.
func (c *Channel) PostOf(tok Token) (Post, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, post := range [...]Post{PostWest, PostEast} {
		if p := c.playerAt(post); p != nil && p.token.Equal(tok) {
			return post, true
		}
	}
	return "", false
}

// Connect привязывает сокет к посту владельца tok.
//
// Старое подключение того же игрока закрывается с CloseReasonReplaced. Всем
// подключённым уходит свежее лобби.
//
// Ошибки:
//   - ErrUnauthorized - токен не принадлежит ни одному посту канала.
//     Сокет остаётся за вызывающим: закрывать его - решение ws-слоя.
//   - ErrChannelClosed - канал остановлен. Сокет уже закрыт причиной
//     CloseReasonChannelClosed.
func (c *Channel) Connect(tok Token, sock Socket) (*Connection, error) {
	var conn *Connection
	err := c.do(func() error {
		post, found := c.PostOf(tok) // west/east - под c.mu
		if !found {
			return ErrUnauthorized
		}
		old := c.conns[post]
		conn = &Connection{ch: c, post: post, sock: sock}
		c.conns[post] = conn
		if old != nil {
			old.sock.Close(CloseReasonReplaced)
		}
		c.broadcastLobby()
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrChannelClosed) {
			sock.Close(CloseReasonChannelClosed)
		}
		return nil, err
	}
	return conn, nil
}

// seatGuest сажает гостя на east. Проверка места и посадка - под одним c.mu.
func (c *Channel) seatGuest() (Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.east != nil {
		return Token{}, ErrFull
	}
	// Занят только позывной хозяина, свободный есть всегда. Проверка - на
	// случай, если постов или позывных станет иначе.
	cs, ok := firstFreeCallsign(c.west.Callsign)
	if !ok {
		return Token{}, ErrFull
	}
	tok, err := newToken()
	if err != nil {
		return Token{}, fmt.Errorf("channel: join: %w", err)
	}
	c.east = newPlayer(cs, tok)
	return tok, nil
}

// playerAt - игрок на посту или nil. Вызывать под c.mu.
func (c *Channel) playerAt(post Post) *Player {
	switch post {
	case PostWest:
		return c.west
	case PostEast:
		return c.east
	default:
		return nil
	}
}

// loop - горутина канала. Единственный, кто трогает conns. Выполняет
// задания из inbox по одному, пока не придёт Stop.
func (c *Channel) loop() {
	defer close(c.done)
	for {
		select {
		case job := <-c.inbox:
			job()
		case reason := <-c.stop:
			c.closeAll(reason)
			return
		}
	}
}

// closeAll закрывает все сокеты причиной reason и очищает conns.
// Вызывать только из loop.
func (c *Channel) closeAll(reason CloseReason) {
	for _, conn := range c.conns {
		conn.sock.Close(reason)
	}
	clear(c.conns)
}

// do выполняет job в горутине канала и ждёт результата.
//
// Возвращает:
//   - nil - job выполнен успешно;
//   - ErrChannelClosed - канал уже остановлен (job не выполнится);
//   - ошибку job - job вернул её.
//
// Паника в job превращается в ошибку: один плохой обработчик не должен
// убивать горутину канала.
func (c *Channel) do(job func() error) error {
	reply := make(chan error, 1)
	wrapped := func() {
		defer func() {
			if r := recover(); r != nil {
				reply <- fmt.Errorf("%w: %v", errJobPanic, r)
			}
		}()
		reply <- job()
	}
	select {
	case c.inbox <- wrapped:
	case <-c.done:
		return ErrChannelClosed
	}
	select {
	case err := <-reply:
		return err
	case <-c.done:
		return ErrChannelClosed
	}
}

// broadcastLobby рассылает свежее лобби всем подключённым.
// Вызывать только из горутины канала.
func (c *Channel) broadcastLobby() {
	for post, conn := range c.conns {
		conn.sock.Send(Message{Payload: c.lobbyFor(post)}) // лобби не выбрасывать
	}
}

// lobbyFor собирает состояние лобби для поста you.
// Вызывать только из горутины канала.
//
// Игроки читаются целиком под c.mu: их пишет Join снаружи, а позже и
// set_ready. Копировать только указатели мало - поля читались бы без лока.
// conns трогает только loop, поэтому без лока.
func (c *Channel) lobbyFor(you Post) LobbyMessage {
	players := make(map[Post]*LobbyPlayer, postsPerChannel)
	c.mu.Lock()
	for _, post := range [...]Post{PostWest, PostEast} {
		p := c.playerAt(post)
		if p == nil {
			players[post] = nil // пост свободен: в JSON будет null
			continue
		}
		players[post] = &LobbyPlayer{
			Callsign: p.Callsign,
			Ready:    p.Ready,
			Bot:      p.Bot,
		}
	}
	c.mu.Unlock()

	for post, lp := range players {
		if lp != nil {
			_, connected := c.conns[post]
			lp.Connected = lp.Bot || connected // бот всегда «на связи»
		}
	}
	return LobbyMessage{Type: "lobby", Lobby: LobbyState{
		Code:      c.code.String(),
		Mode:      c.mode,
		Phase:     LobbyPhaseLobby,
		Countdown: nil,
		You:       you,
		Players:   players,
	}}
}
