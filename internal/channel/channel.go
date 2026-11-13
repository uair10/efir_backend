package channel

import (
	"fmt"
	"sync"
)

// Channel - комната на одну смену. Через нее два человека (или человек и бот)
// находят друг друга и вместе играют.
//
// Что он делает:
//   - Хранит места. У канала два поста, west и east; он решает, кто какой
//     пост занимает.
//   - Опознает игрока по токену и возвращает его на пост после обрыва.
//   - Дальше (EFR-3+): лобби, запуск смены, жизненный цикл.
type Channel struct {
	id   string
	code Code
	mode GameMode

	mu   sync.Mutex // временно, до актора (ADR-002); защищает west и east
	west *Player
	east *Player // nil - пост свободен
}

// newChannel собирает канал и сажает хоста на west с позывным "Первый";
// в одиночной смене east сразу занимает бот. id и code выдает реестр.
//
// Возвращает токен хоста. Наружу канал его больше не отдает: клиент
// получит его один раз в ответе на создание.
func newChannel(id string, code Code, mode GameMode) (*Channel, Token, error) {
	tok, err := newToken()
	if err != nil {
		return nil, Token{}, err
	}
	ch := &Channel{id: id, code: code, mode: mode, west: newPlayer(CallsignFirst, tok)}
	if mode == GameModeSolo {
		ch.east = newBot()
	}
	return ch, tok, nil
}

// ID - внутренний идентификатор "c_…" для подключения сокета.
func (c *Channel) ID() string { return c.id }

// Code - публичный 6-значный код для входа по нему.
func (c *Channel) Code() Code { return c.code }

// Mode - режим смены, solo или coop.
func (c *Channel) Mode() GameMode { return c.mode }

// Join сажает гостя на east с первым свободным позывным и возвращает его
// токен. Канал гость нашел через Registry.ByCode.
//
// Проверка места и посадка идут под одним c.mu, иначе два гостя одновременно
// увидят свободный east и оба сядут.
//
// Ошибки:
//   - ErrFull - east занят (гостем или ботом). Сидящий на east не вытеснен.
//   - ошибка генерации токена.
func (c *Channel) Join() (Token, error) {
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

// Player возвращает копию игрока на посту. ok == false - пост свободен.
func (c *Channel) Player(post Post) (Player, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.slot(post)
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
		if p := c.slot(post); p != nil && p.token.Equal(tok) {
			return post, true
		}
	}
	return "", false
}

// slot - игрок на посту или nil. Вызывать под c.mu.
func (c *Channel) slot(post Post) *Player {
	switch post {
	case PostWest:
		return c.west
	case PostEast:
		return c.east
	default:
		return nil
	}
}
