package ws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"

	"github.com/uair10/efir_backend/internal/channel"
)

// writeLoop разбирает outbox и пишет кадры. После Close - close-рукопожатие.
//
// Единственный писатель в conn: coder/websocket не разрешает конкурентные
// Write, поэтому весь вывод идёт через эту горутину.
func (s *Socket) writeLoop(ctx context.Context) error {
	for {
		s.mu.Lock()
		queue := s.queue
		s.queue = nil
		closing := s.closing
		s.mu.Unlock()

		if len(queue) > 0 {
			if err := s.writeAll(ctx, queue); err != nil {
				return err
			}
			continue // после разбора очереди проверим closing ещё раз
		}

		if closing != nil {
			return s.closeHandshake(*closing)
		}

		// Ждём сигнала или отмены контекста.
		select {
		case <-s.wake:
		case <-ctx.Done():
			// Контекст отменен: рукопожатие не ждем, рвем транспорт.
			_ = s.conn.CloseNow()
			return fmt.Errorf("ws: write loop: %w", ctx.Err())
		}
	}
}

// writeAll пишет сообщения по порядку.
func (s *Socket) writeAll(ctx context.Context, queue []channel.Message) error {
	for _, m := range queue {
		data, err := json.Marshal(m.Payload)
		if err != nil {
			// Payload не сериализуется - это баг вызывающего, не клиента.
			// Логируем и пропускаем, чтобы одно сообщение не рвало соединение.
			s.opts.Logger.Error("ws: marshal payload", "err", err)
			continue
		}
		// Write ждёт, пока TCP-буфер разгрузится. Блокируется только
		// эта горутина, канал и другие клиенты не ждут.
		if err := s.conn.Write(ctx, websocket.MessageText, data); err != nil {
			return fmt.Errorf("ws: write: %w", err)
		}
	}
	return nil
}

// closeHandshake отправляет close-кадр и ждёт ответ клиента.
func (s *Socket) closeHandshake(spec closeSpec) error {
	err := s.conn.Close(spec.code, spec.reason)
	// Close возвращает ошибку, если соединение уже закрыто;
	// для нас это не ошибка - код всё равно тот, что мы послали.
	if err != nil && websocket.CloseStatus(err) == -1 {
		return fmt.Errorf("ws: close: %w", err)
	}
	return nil
}
