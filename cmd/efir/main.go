// Command efir запускает сервер игры «Эфир»: каналы, лобби и смену.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/uair10/efir_backend/internal/channel"
	"github.com/uair10/efir_backend/internal/httpapi"
	"github.com/uair10/efir_backend/internal/infrastructure/config"
	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

func main() {
	if err := run(); err != nil {
		slog.Error("run failed", slog.Any("err", err))
		os.Exit(1)
	}
}

// run собирает зависимости и работает до сигнала или ошибки сервера.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.NewLogger(cfg.Log)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	registry := channel.NewRegistry(cfg.Game.MaxChannels)
	server := httpapi.New(logger, cfg.Server, registry)

	errCh := make(chan error, 1)
	go func() { errCh <- server.Listen(ctx) }()

	select {
	case err := <-errCh: // сервер упал сам, например порт занят
		return err
	case <-ctx.Done(): // пришёл сигнал SIGINT/SIGTERM, идём дальше к остановке
	}
	stop() // теперь второй Ctrl+C сразу убьёт процесс, если остановка зависнет

	logger.Info("shutting down")
	// ctx уже отменён, поэтому таймаут строим от context.WithoutCancel
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("stop server: %w", err)
	}
	logger.Info("server stopped")
	return nil
}
