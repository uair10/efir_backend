package game

// Post - пост в канале. Запад у того, кто открыл канал;
// восток у гостя или у бота в одиночной игре.
type Post string

// Посты канала.
const (
	PostWest Post = "west"
	PostEast Post = "east"
)

// Host - тот, кто открыл канал: он на Западе и только он начинает ночь.
const Host = PostWest

// IsValid - p один из постов канала.
func (p Post) IsValid() bool {
	switch p {
	case PostWest, PostEast:
		return true
	}
	return false
}

// Mode - режим игры.
type Mode string

// Режимы игры.
const (
	ModeSolo Mode = "solo"
	ModeCoop Mode = "coop"
)

// IsValid - m один из режимов игры.
func (m Mode) IsValid() bool {
	switch m {
	case ModeSolo, ModeCoop:
		return true
	}
	return false
}

// Phase обозначает этап смены от сбора игроков до завершения игры.
type Phase string

// Фазы игровой смены.
const (
	PhaseLobby    Phase = "lobby"
	PhaseStarting Phase = "starting"
	PhaseBriefing Phase = "briefing"
	PhaseDay      Phase = "day"
	PhaseNight    Phase = "night"
	PhaseReport   Phase = "report"
	PhaseEnd      Phase = "end"
)

// LogLevel - уровень записи в журнале.
type LogLevel string

// Категории сообщений журнала.
const (
	LogLevelInfo  LogLevel = "info"
	LogLevelMinor LogLevel = "minor"
	LogLevelRadio LogLevel = "radio"
	LogLevelAlert LogLevel = "alert"
	LogLevelHead  LogLevel = "head"
	LogLevelSum   LogLevel = "sum"
)
