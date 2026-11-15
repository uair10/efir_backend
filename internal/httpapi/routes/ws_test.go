package routes

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uair10/efir_backend/internal/channel"
)

// BR-3.1: originAllowed сверяет host[:port] из Origin с шаблонами так же,
// как websocket.AcceptOptions.OriginPatterns. Иначе ручная проверка и
// Accept разойдутся: один пропустит, другой ответит 403.
func TestOriginAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		origin   string
		patterns []string
		want     bool
	}{
		{"точное совпадение", "http://localhost:5173", []string{"localhost:5173"}, true},
		{"https", "https://efir.example.com", []string{"efir.example.com"}, true},
		{"регистр не важен", "http://LocalHost:5173", []string{"localhost:5173"}, true},
		{"шаблон в списке", "http://localhost:5173", []string{"localhost:5173"}, true},
		{"второй шаблон", "https://efir.example.com", []string{"localhost:5173", "efir.example.com"}, true},
		{"звездочка - любой", "https://anything.test:8443", []string{"*"}, true},
		{"поддомен по шаблону", "https://app.efir.example.com", []string{"*.efir.example.com"}, true},
		{"шаблон поддомена не ловит сам домен", "https://efir.example.com", []string{"*.efir.example.com"}, false},
		{"другой порт", "http://localhost:5174", []string{"localhost:5173"}, false},
		{"без порта не равно с портом", "http://localhost", []string{"localhost:5173"}, false},
		{"чужой хост", "http://evil.example", []string{"localhost:5173"}, false},
		{"суффикс не обманывает", "http://localhost:5173.evil.example", []string{"localhost:5173"}, false},
		{"пустой список", "http://localhost:5173", nil, false},
		{"не URL", "::::", []string{"*"}, false},
		{"без хоста", "file:///etc/passwd", []string{"*"}, false},
		{"null", "null", []string{"*"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, originAllowed(tt.origin, tt.patterns))
		})
	}
}

// closeRecorder - channel.Socket, который запоминает причину закрытия.
type closeRecorder struct{ reasons []channel.CloseReason }

func (*closeRecorder) Send(channel.Message)           {}
func (r *closeRecorder) Close(rs channel.CloseReason) { r.reasons = append(r.reasons, rs) }

// Паника в Receive не роняет обработчик: сокет закрывается 1011, стек - в лог.
func TestReceiveSafely_Panic(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	sock := &closeRecorder{}

	receive := receiveSafely(context.Background(), logger, func([]byte) { panic("boom") }, sock)
	assert.NotPanics(t, func() { receive([]byte(`{}`)) })

	assert.Equal(t, []channel.CloseReason{channel.CloseReasonInternal}, sock.reasons)
	assert.Contains(t, logs.String(), "ws receive panic")
	assert.Contains(t, logs.String(), "boom")
}

// Без паники обёртка прозрачна: данные доходят, сокет не закрыт.
func TestReceiveSafely_PassThrough(t *testing.T) {
	t.Parallel()

	sock := &closeRecorder{}
	var got []byte
	receive := receiveSafely(context.Background(), slog.New(slog.DiscardHandler),
		func(data []byte) { got = data }, sock)

	receive([]byte(`{"type":"x"}`))

	assert.JSONEq(t, `{"type":"x"}`, string(got))
	assert.Empty(t, sock.reasons)
}
