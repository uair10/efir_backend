package channel

import (
	"errors"

	"github.com/uair10/efir_backend/internal/game"
)

// Доменные ошибки. HTTP-слой отображает их на статусы.
var (
	// ErrNotFound - канала нет или он одиночный.
	ErrNotFound = errors.New("channel not found")
	// ErrFull - в канале нет свободного поста.
	ErrFull = game.ErrPostTaken
	// ErrTooManyChannels - открыто GAME_MAX_CHANNELS каналов.
	ErrTooManyChannels = errors.New("too many channels")
	// ErrCodeCollision - maxCodeAttempts кодов подряд заняты.
	ErrCodeCollision = errors.New("code collision")
	// ErrInvalidCode - строка не похожа на код канала (ParseCode).
	ErrInvalidCode = errors.New("invalid channel code")
	// ErrInvalidToken - строка не похожа на токен игрока (ParseToken).
	ErrInvalidToken = errors.New("invalid player token")
	// ErrUnauthorized - токен не принадлежит ни одному посту канала (Connect, PostOf).
	ErrUnauthorized = errors.New("unauthorized access")
	// ErrChannelClosed - горутина канала остановлена: Remove или Close реестра.
	ErrChannelClosed = errors.New("channel closed")
	// ErrRegistryClosed - реестр закрыт (сервер останавливается), новых каналов нет.
	ErrRegistryClosed = errors.New("registry closed")
	// ErrCallsignTaken - позывной уже занят.
	ErrCallsignTaken = game.ErrCallsignTaken
	// ErrCallsignLocked - игрок уже готов, позывной не меняется.
	ErrCallsignLocked = game.ErrCallsignLocked
	// ErrPartnerMissing - "Готов", когда напарника в канале нет.
	ErrPartnerMissing = game.ErrPartnerMissing
)
