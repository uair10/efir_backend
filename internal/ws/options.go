package ws

import (
	"log/slog"
	"time"
)

// Options - настройки одного Socket. Значения по умолчанию - в DefaultOptions.
type Options struct {
	MaxMessageBytes int64         // GAME_WS_MAX_MESSAGE; больше - закрытие 1009; 0 - лимит библиотеки (32 KiB)
	PingInterval    time.Duration // как часто слать ping
	PongTimeout     time.Duration // сколько ждать pong, прежде чем оборвать
	OutboxSize      int           // сколько исходящих держать для медленного клиента
	Logger          *slog.Logger
}

// Значения по умолчанию.
const (
	defaultMaxMessageBytes = 4096
	defaultPingInterval    = 20 * time.Second
	defaultPongTimeout     = 10 * time.Second
	defaultOutboxSize      = 32
)

// DefaultOptions - значения по умолчанию. MaxMessageBytes сервер
// перекрывает значением из конфига (GAME_WS_MAX_MESSAGE).
func DefaultOptions(logger *slog.Logger) Options {
	return Options{
		MaxMessageBytes: defaultMaxMessageBytes,
		PingInterval:    defaultPingInterval,
		PongTimeout:     defaultPongTimeout,
		OutboxSize:      defaultOutboxSize,
		Logger:          logger,
	}
}

// normalize заполняет нулевые поля значениями по умолчанию, чтобы Socket
// работал и с частично заданными Options. MaxMessageBytes не трогает:
// 0 там осмыслен.
func (o Options) normalize() Options {
	if o.OutboxSize <= 0 {
		o.OutboxSize = defaultOutboxSize
	}
	if o.PingInterval <= 0 {
		o.PingInterval = defaultPingInterval
	}
	if o.PongTimeout <= 0 {
		o.PongTimeout = defaultPongTimeout
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}
