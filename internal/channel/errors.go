package channel

import "errors"

// Доменные ошибки. HTTP-слой отображает их на статусы.
var (
	// ErrNotFound - канала нет или он одиночный.
	ErrNotFound = errors.New("channel not found")
	// ErrFull - в канале нет свободного поста.
	ErrFull = errors.New("channel is full")
	// ErrTooManyChannels - открыто GAME_MAX_CHANNELS каналов.
	ErrTooManyChannels = errors.New("too many channels")
	// ErrCodeCollision - maxCodeAttempts кодов подряд заняты.
	ErrCodeCollision = errors.New("code collision")
	// ErrInvalidCode - строка не похожа на код канала (ParseCode).
	ErrInvalidCode = errors.New("invalid channel code")
	// ErrInvalidToken - строка не похожа на токен игрока (ParseToken).
	ErrInvalidToken = errors.New("invalid player token")
)
