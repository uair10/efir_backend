package channel

import (
	"fmt"

	"github.com/uair10/efir_backend/internal/game"
)

// Player - игрок на посту. Наружу канал отдает только копию (Channel.Player),
// поэтому открытые поля не дают менять состояние канала в обход его горутины.
// Пост игрока не хранится: его задает слот канала, в котором он сидит.
type Player struct {
	Callsign Callsign
	Ready    bool
	Bot      bool
}

// posts - все посты канала по порядку.
//
//nolint:gochecknoglobals // справочник только для чтения
var posts = [...]game.Post{game.PostWest, game.PostEast}

func (p Player) String() string {
	return fmt.Sprintf("Player(%s)", p.Callsign)
}
