package logging

import (
	"log/slog"
)

// Ключи корреляции. Одни на весь проект: других имён для тех же
// сущностей нет, иначе grep по логу теряет записи.
const (
	KeyRequestID   = "request_id"   // id HTTP-запроса (X-Request-ID)
	KeyChannelID   = "channel_id"   // внутренний id канала "c_…"
	KeyChannelCode = "channel_code" // публичный 6-значный код канала
	KeyPost        = "post"         // west | east
	KeyCloseCode   = "close_code"   // код закрытия WebSocket
	KeyErr         = "err"          // ошибка
)

// RequestID - атрибут id запроса.
func RequestID(id string) slog.Attr { return slog.String(KeyRequestID, id) }

// ChannelID - атрибут id канала.
func ChannelID(id string) slog.Attr { return slog.String(KeyChannelID, id) }

// ChannelCode - атрибут кода канала.
func ChannelCode(code string) slog.Attr { return slog.String(KeyChannelCode, code) }

// Post - атрибут поста. Принимает строку: logging не знает доменных типов.
func Post[S ~string](post S) slog.Attr { return slog.String(KeyPost, string(post)) }

// CloseCode - атрибут кода закрытия WebSocket.
func CloseCode(code int) slog.Attr { return slog.Int(KeyCloseCode, code) }

// Err - атрибут ошибки.
func Err(err error) slog.Attr { return slog.Any(KeyErr, err) }

// Discard - логгер, который ничего не пишет. Запасной вариант для
// необязательного логгера вместо slog.Default.
func Discard() *slog.Logger { return slog.New(slog.DiscardHandler) }
