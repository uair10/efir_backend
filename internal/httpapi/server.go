// Package httpapi содержит HTTP-слой сервиса.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/uair10/efir_backend/internal/channel"
	"github.com/uair10/efir_backend/internal/httpapi/routes"

	"github.com/uair10/efir_backend/internal/httpapi/middleware"
	"github.com/uair10/efir_backend/internal/infrastructure/config"
)

// Server - HTTP-сервер сервиса.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// New собирает http.Server с роутером и таймаутами из конфига.
func New(logger *slog.Logger, cfg config.Server, registry *channel.Registry) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.Addr(),
			Handler:           newHandler(logger, cfg, registry),
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		},
		logger: logger,
	}
}

// newHandler собирает роутер и оборачивает его в middleware.
// Отдельно от New, чтобы роутер можно было тестировать без http.Server.
// registry - общий на все запросы реестр каналов, создаётся один раз в main.
func newHandler(logger *slog.Logger, cfg config.Server, registry *channel.Registry) http.Handler {
	mux := http.NewServeMux()

	api := newAPI(mux, cfg)
	registerRoutes(api, registry)

	// Оборачиваем mux в middleware - они работают и для Huma, и для ручных роутов.
	return withMiddleware(mux, logger, cfg.MaxBodyBytes)
}

// registerRoutes регистрирует эндпоинты в Huma.
func registerRoutes(api huma.API, registry *channel.Registry) {
	channels := routes.NewChannels(registry)

	huma.Register(api, huma.Operation{
		OperationID:   "getHealthz",
		Method:        http.MethodGet,
		Path:          "/healthz",
		Summary:       "Healthcheck",
		Tags:          []string{"system"},
		DefaultStatus: http.StatusOK,
	}, routes.HealthzHumaHandler)
	huma.Register(api, huma.Operation{
		OperationID:   "createChannel",
		Method:        http.MethodPost,
		Path:          "/api/channels",
		Summary:       "Открыть канал",
		Tags:          []string{"channels"},
		DefaultStatus: http.StatusCreated,
		Errors: []int{
			http.StatusUnprocessableEntity,
			http.StatusServiceUnavailable,
		},
	}, channels.Create)

	huma.Register(api, huma.Operation{
		OperationID: "joinChannel",
		Method:      http.MethodPost,
		Path:        "/api/channels/{code}/players",
		Summary:     "Войти по коду",
		Tags:        []string{"channels"},
		Errors: []int{
			http.StatusNotFound,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
		},
	}, channels.Join)
}

// withMiddleware оборачивает роутер в middleware. Тесты передают сюда свой mux.
func withMiddleware(mux *http.ServeMux, logger *slog.Logger, maxBodyBytes int64) http.Handler {
	// middleware оборачивают mux изнутри наружу
	handler := notFoundAsProblem(mux)
	// MaxBytesHandler только ограничивает r.Body. 413 отдаёт хендлер,
	// когда при чтении тела получает *http.MaxBytesError.
	handler = http.MaxBytesHandler(handler, maxBodyBytes)
	// Recover внутри AccessLog: после паники access log пишется со статусом 500.
	handler = middleware.Recover(handler)
	handler = middleware.AccessLog(handler)
	handler = middleware.RequestID(logger)(handler) // самый первый
	return handler
}

// newAPI создаёт huma.API поверх готового mux.
func newAPI(mux *http.ServeMux, cfg config.Server) huma.API {
	installErrorPolicy() // sync.Once: безопасно при параллельных тестах

	humaConfig := huma.DefaultConfig("Эфир", "0.1.0")
	humaConfig.Info.Description = "Сервер игры «Эфир»: каналы на двоих, лобби и смена"
	serverURL := "/"
	if cfg.PublicBaseURL != "" {
		serverURL = cfg.PublicBaseURL
	}
	humaConfig.Servers = []*huma.Server{{URL: serverURL}}

	// Документация: пути и хуки
	if !cfg.DocsEnabled {
		humaConfig.OpenAPIPath = ""
		humaConfig.DocsPath = ""
	}
	humaConfig.SchemasPath = ""  // не отдаём отдельный /schemas
	humaConfig.CreateHooks = nil // убираем $schema из ответов (Huma ставит его по умолчанию)

	return humago.New(mux, humaConfig)
}

// Listen занимает адрес из конфига и обслуживает запросы до остановки.
// ctx ограничивает только захват порта, а не работу сервера.
func (s *Server) Listen(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return s.Serve(ln)
}

// Serve обслуживает запросы на готовом listener и блокируется до остановки.
// После Shutdown возвращает nil. Тесты передают сюда свой listener на :0.
func (s *Server) Serve(ln net.Listener) error {
	s.logger.Info("server started", slog.String("addr", ln.Addr().String()))
	if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// Shutdown перестаёт принимать соединения и ждёт текущие запросы, пока не истечёт ctx.
// Если не дождались, разрывает оставшиеся соединения принудительно.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", errors.Join(err, s.httpServer.Close()))
	}
	return nil
}
