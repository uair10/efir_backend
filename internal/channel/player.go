package channel

import "fmt"

// Post - пост в канале. Запад у того, кто открыл канал;
// восток у гостя или у бота в одиночной игре.
type Post string

// Посты канала.
const (
	PostWest Post = "west"
	PostEast Post = "east"
)

// IsValid - p один из постов канала.
func (p Post) IsValid() bool {
	switch p {
	case PostWest, PostEast:
		return true
	}
	return false
}

// Player - игрок на посту. Наружу канал отдает только копию (Channel.Player),
// поэтому открытые поля не дают менять состояние канала в обход его замка.
// Пост игрока не хранится: его задает слот канала, в котором он сидит.
type Player struct {
	Callsign  Callsign
	Ready     bool
	Bot       bool
	Connected bool

	token Token // у бота пустой: пустой токен не совпадает ни с каким
}

// newPlayer - человек с токеном, еще не готов (хозяин или гость).
func newPlayer(callsign Callsign, token Token) *Player {
	return &Player{Callsign: callsign, token: token}
}

// newBot - бот одиночной смены: без токена, сразу готов (BR-1.3).
func newBot() *Player {
	return &Player{Callsign: CallsignSecond, Ready: true, Bot: true}
}

func (p Player) String() string {
	return fmt.Sprintf("Player(%s)", p.Callsign)
}
