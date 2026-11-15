package routes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/coder/websocket"

	"github.com/uair10/efir_backend/internal/channel"
	"github.com/uair10/efir_backend/internal/httpapi/middleware"
	"github.com/uair10/efir_backend/internal/infrastructure/logging"
	"github.com/uair10/efir_backend/internal/ws"
)

// ChannelsWS отдаёт WebSocket для подключения игрока к каналу и считает
// живые соединения, чтобы сервер мог дождаться их при остановке.
//
// GET /api/channels/{channelId}/ws?token=...
//
// Проверки (Origin, токен, канал) - до апгрейда, чтобы отвечать обычным
// problem+json. После апгрейда HTTP-ответ уже не отправить: ошибки уходят
// кодом закрытия WebSocket.
//
// Токен не логируется (NFR-3). Access log пишет только путь, без query.
//
// ReadTimeout и WriteTimeout http.Server сокет не обрывают. Accept
// перехватывает соединение (Hijack), а net/http при Hijack снимает с него
// дедлайны (SetDeadline(time.Time{})). Дальше живость соединения проверяет
// ping/pong пакета ws. Тест: TestWS_ServerTimeoutsDoNotKillSocket.
type ChannelsWS struct {
	registry       *channel.Registry
	socketOpts     ws.Options
	allowedOrigins []string // шаблоны host[:port], как websocket.AcceptOptions.OriginPatterns

	// active считает запущенные обработчики. http.Server.Shutdown их не
	// ждёт: после апгрейда соединение перехвачено (Hijack) и серверу не видно.
	active sync.WaitGroup
}

// NewChannelsWS создаёт обработчик сокетов поверх общего реестра каналов.
func NewChannelsWS(registry *channel.Registry, socketOpts ws.Options, allowedOrigins []string) *ChannelsWS {
	return &ChannelsWS{
		registry:       registry,
		socketOpts:     socketOpts,
		allowedOrigins: allowedOrigins,
	}
}

// Wait ждёт, пока завершатся все обработчики сокетов, или пока не истечёт ctx.
//
// Вызывать после http.Server.Shutdown и Registry.Close: Shutdown гарантирует,
// что новых обработчиков не будет, Close - что живые закроют сокеты (1001).
// Без этого процесс может выйти раньше, чем close-кадр уйдёт клиенту.
func (h *ChannelsWS) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		h.active.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait websockets: %w", ctx.Err())
	}
}

// ServeHTTP проверяет запрос, делает апгрейд и держит сокет до закрытия.
func (h *ChannelsWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add - до апгрейда, пока соединение ещё видно http.Server: Shutdown
	// дождётся этой точки, поэтому Add всегда случится раньше Wait.
	h.active.Add(1)
	defer h.active.Done()

	ctx := r.Context()
	logger := logging.FromCtx(ctx) // с request_id

	// Origin. Без заголовка - можно (не браузер).
	if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, h.allowedOrigins) {
		middleware.WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Токен и канал. На всё - один 401: не подсказываем, что именно не так.
	tok, ch, ok := h.authorize(r)
	if !ok {
		middleware.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Апгрейд. Accept сверяет Origin ещё раз сам и без шаблонов
	// отвечает 403 на любой чужой, поэтому даём ему тот же список.
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  h.allowedOrigins,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		logger.ErrorContext(ctx, "ws upgrade failed", slog.Any("err", err))
		return // Accept уже ответил ошибкой
	}

	// Привязка к каналу. sock реализует channel.Socket. Логи сокета
	// (pong timeout, ошибки записи) - с request_id и channel_id этого запроса.
	logger = logger.With(slog.String("channel_id", ch.ID()))
	socketOpts := h.socketOpts
	socketOpts.Logger = logger
	sock := ws.New(wsConn, socketOpts)
	conn, err := ch.Connect(tok, sock)
	if err != nil {
		closeRejected(ctx, logger, wsConn, err)
		return
	}

	// Run блокируется до закрытия соединения. Входящие - в Receive канала.
	// Disconnect после Run: идемпотентен, снимет пост, если мы всё ещё там.
	runErr := sock.Run(ctx, receiveSafely(ctx, logger, conn.Receive, sock))
	conn.Disconnect()

	code := websocket.CloseStatus(runErr)
	if code == -1 {
		code = websocket.StatusAbnormalClosure // close-кадра не было: обрыв
	}
	logger.InfoContext(ctx, "ws closed",
		slog.String("post", string(conn.Post())),
		slog.Int("code", int(code)),
	)
}

// authorize находит канал и проверяет, что токен из него.
// ok == false - любая из причин 401; какая именно, наружу не говорим.
func (h *ChannelsWS) authorize(r *http.Request) (channel.Token, *channel.Channel, bool) {
	tok, err := channel.ParseToken(r.URL.Query().Get("token"))
	if err != nil {
		return channel.Token{}, nil, false
	}
	ch, err := h.registry.ByID(r.PathValue("channelId"))
	if err != nil {
		return channel.Token{}, nil, false
	}
	if _, ok := ch.PostOf(tok); !ok {
		return channel.Token{}, nil, false
	}
	return tok, ch, true
}

// closeRejected закрывает сокет, который канал не принял. Run ещё не
// запущен, поэтому close-кадр отправляем сами.
func closeRejected(ctx context.Context, logger *slog.Logger, wsConn *websocket.Conn, err error) {
	switch {
	case errors.Is(err, channel.ErrUnauthorized):
		// Гонка: игрок пропал между authorize и Connect.
		_ = wsConn.Close(websocket.StatusPolicyViolation, "unauthorized")
	case errors.Is(err, channel.ErrChannelClosed):
		// Канал удалён между authorize и Connect.
		_ = wsConn.Close(websocket.StatusNormalClosure, "channel closed")
	default:
		logger.ErrorContext(ctx, "ws connect failed", slog.Any("err", err))
		_ = wsConn.Close(websocket.StatusInternalError, "internal error")
	}
}

// receiveSafely оборачивает обработчик входящих: паника при разборе
// сообщения закрывает только этот сокет кодом 1011, а пост снимает обычный
// Disconnect после Run.
//
// Middleware Recover тут не поможет: после апгрейда HTTP-ответ не написать,
// а паника, вылетевшая из Run, пропустила бы Disconnect - в лобби навсегда
// остался бы connected: true.
func receiveSafely(ctx context.Context, logger *slog.Logger, receive func([]byte), sock channel.Socket) func([]byte) {
	return func(data []byte) {
		defer func() {
			if v := recover(); v != nil {
				logger.ErrorContext(ctx, "ws receive panic",
					slog.Any("panic", v),
					slog.String("stack", string(debug.Stack())),
				)
				sock.Close(channel.CloseReasonInternal)
			}
		}()
		receive(data)
	}
}

// originAllowed сравнивает host[:port] из Origin со списком шаблонов.
// Семантика как websocket.AcceptOptions.OriginPatterns: шаблоны path.Match
// ("*" - любой), регистронезависимо, пустой список - запрещаем все (кроме
// отсутствия Origin - это не браузер).
func originAllowed(origin string, patterns []string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Host)
	for _, p := range patterns {
		if ok, _ := path.Match(strings.ToLower(p), host); ok {
			return true
		}
	}
	return false
}
