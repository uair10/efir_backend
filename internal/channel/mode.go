package channel

// GameMode - режим игры канала.
type GameMode string

// Режимы игры.
const (
	GameModeSolo GameMode = "solo"
	GameModeCoop GameMode = "coop"
)

// IsValid - m один из режимов игры.
func (m GameMode) IsValid() bool {
	switch m {
	case GameModeSolo, GameModeCoop:
		return true
	}
	return false
}
