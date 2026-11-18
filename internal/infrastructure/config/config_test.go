package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	defaults := Config{
		Server: Server{
			Port:              8080,
			ShutdownTimeout:   10 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxBodyBytes:      1 << 20,
			DocsEnabled:       false,
		},
		Log: Log{Level: slog.LevelInfo},
		Game: Game{
			Tick:              50 * time.Millisecond,
			MaxChannels:       500,
			WSMaxMessageBytes: 4096,
			WSAllowedOrigins:  []string{"localhost:5173"},
		},
	}

	overridden := defaults
	overridden.Server.Port = 9090
	overridden.Server.ShutdownTimeout = 3 * time.Second
	overridden.Server.MaxBodyBytes = 512
	overridden.Log.Level = slog.LevelDebug
	overridden.Game.MaxChannels = 7
	overridden.Game.WSMaxMessageBytes = 8192
	overridden.Game.WSAllowedOrigins = []string{"localhost:5173", "efir.example.com"}

	tests := []struct {
		name     string
		env      map[string]string
		want     Config
		wantErrs []string // все подстроки должны быть в одной ошибке
	}{
		{
			name: "значения по умолчанию",
			env:  map[string]string{},
			want: defaults,
		},
		{
			name: "переопределение через env",
			env: map[string]string{
				"SERVER_PORT":             "9090",
				"SERVER_SHUTDOWN_TIMEOUT": "3s",
				"SERVER_MAX_BODY_BYTES":   "512",
				"LOG_LEVEL":               "debug",
				"GAME_MAX_CHANNELS":       "7",
				"GAME_WS_MAX_MESSAGE":     "8192",
				"GAME_WS_ALLOWED_ORIGINS": "localhost:5173,efir.example.com",
			},
			want: overridden,
		},
		{
			name:     "GAME_WS_MAX_MESSAGE 0",
			env:      map[string]string{"GAME_WS_MAX_MESSAGE": "0"},
			wantErrs: []string{"GAME_WS_MAX_MESSAGE must be positive"},
		},
		{
			// OriginPatterns coder/websocket сравнивает с хостом, схема не совпадет никогда.
			name:     "GAME_WS_ALLOWED_ORIGINS со схемой",
			env:      map[string]string{"GAME_WS_ALLOWED_ORIGINS": "http://localhost:5173"},
			wantErrs: []string{"GAME_WS_ALLOWED_ORIGINS"},
		},
		{
			name:     "GAME_WS_ALLOWED_ORIGINS с битым шаблоном",
			env:      map[string]string{"GAME_WS_ALLOWED_ORIGINS": "localhost:5173,[bad"},
			wantErrs: []string{"GAME_WS_ALLOWED_ORIGINS"},
		},
		{
			// env режет по запятой без trim: " b" не совпал бы ни с одним Origin.
			name:     "GAME_WS_ALLOWED_ORIGINS с пробелом после запятой",
			env:      map[string]string{"GAME_WS_ALLOWED_ORIGINS": "localhost:5173, efir.example.com"},
			wantErrs: []string{"GAME_WS_ALLOWED_ORIGINS"},
		},
		{
			name:     "порт 0",
			env:      map[string]string{"SERVER_PORT": "0"},
			wantErrs: []string{"SERVER_PORT must be in range"},
		},
		{
			name:     "порт 70000",
			env:      map[string]string{"SERVER_PORT": "70000"},
			wantErrs: []string{"SERVER_PORT must be in range"},
		},
		{
			name: "невалидный LOG_LEVEL",
			env:  map[string]string{"LOG_LEVEL": "verbose"},
			// env называет в ошибке разбора поле Go, а не переменную окружения.
			wantErrs: []string{`field "Level"`},
		},
		{
			name: "несколько ошибок валидации сразу",
			env: map[string]string{
				"SERVER_PORT":           "0",
				"SERVER_READ_TIMEOUT":   "-1s",
				"SERVER_MAX_BODY_BYTES": "0",
				"GAME_MAX_CHANNELS":     "0",
			},
			wantErrs: []string{"SERVER_PORT", "SERVER_READ_TIMEOUT", "SERVER_MAX_BODY_BYTES", "GAME_MAX_CHANNELS"},
		},
		{
			name: "несколько ошибок разбора сразу",
			env: map[string]string{
				"SERVER_PORT": "abc",
				"LOG_LEVEL":   "verbose",
			},
			wantErrs: []string{`field "Port"`, `field "Level"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := loadWithOpts(env.Options{Environment: tt.env})

			if len(tt.wantErrs) > 0 {
				// Все подстроки должны быть в одной ошибке: проверяем все, а не до первой.
				require.Error(t, err)
				for _, want := range tt.wantErrs {
					assert.Contains(t, err.Error(), want, "error text")
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, *got)
		})
	}
}
