package channel

import "github.com/uair10/efir_backend/internal/game"

// Callsign - позывной игрока в представлении канала.
type Callsign = game.Callsign

// Позывные, которые канал показывает при первоначальном распределении постов.
const (
	CallsignFirst  = game.CallsignFirst
	CallsignSecond = game.CallsignSecond
)
