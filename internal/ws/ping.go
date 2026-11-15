package ws

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// pingLoop шлёт ping с интервалом PingInterval и рвёт соединение, если
// pong не пришёл за PongTimeout.
//
// coder/websocket: Ping(ctx) блокируется до pong или до отмены ctx. Ждём
// его с таймаутом PongTimeout. Pong читает reader (Run), поэтому ping
// работает, только пока Run крутит Read.
func (s *Socket) pingLoop(ctx context.Context) error {
	ticker := time.NewTicker(s.opts.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ws: ping loop: %w", ctx.Err())
		case <-ticker.C:
			// Контекст ping'а ограничен PongTimeout: если pong не пришёл,
			// контекст отменяется, соединение рвём.
			pingCtx, cancel := context.WithTimeout(ctx, s.opts.PongTimeout)
			err := s.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return s.pingFailed(ctx, err)
			}
		}
	}
}

// pingFailed разбирает, почему Ping не удался. Pong timeout - только когда
// истёк именно PongTimeout, а сам pinger ещё нужен. Иначе соединение уже
// закрыто (writer сделал close, клиент ушёл) или Run гасит pinger -
// клиент ни при чём, в лог как обрыв не пишем.
func (s *Socket) pingFailed(ctx context.Context, err error) error {
	if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		// Клиент молчит. Рвём транспорт: reader получит ошибку и выйдет,
		// за ним writer.
		s.opts.Logger.Info("ws: pong timeout, dropping client")
		_ = s.conn.CloseNow()
	}
	return fmt.Errorf("ws: ping: %w", err)
}
