// Package ws - транспорт WebSocket: JSON-кадры, outbox, ping/pong и коды
// закрытия. О лобби и игроках не знает, канал видит его как channel.Socket.
package ws

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/uair10/efir_backend/internal/channel"
	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

// Socket оборачивает websocket.Conn и реализует channel.Socket.
//
// Три горутины на соединение:
//   - reader: Run (сам вызывающий) читает кадры и отдаёт их onMessage;
//   - writer: разбирает outbox, пишет кадры, в конце делает close-рукопожатие;
//   - pinger: шлёт ping, рвёт соединение, если pong не пришёл.
//
// Send/Close не блокируют: они только кладут в очередь/ставят флаг.
// Политика переполнения outbox (BR-3.5, ADR-002): старые Droppable
// выбрасываются; если выбрасывать нечего - соединение закрывается (1008).
type Socket struct {
	conn *websocket.Conn
	opts Options

	mu      sync.Mutex        // защищает queue и closing
	queue   []channel.Message // outbox: writer забирает его целиком
	closing *closeSpec        // nil - не закрываемся; после установки не меняется
	wake    chan struct{}     // сигнал writer'у: есть работа (буфер 1)
}

// slowConsumerGrace - сколько ждать кадр 1008 медленному клиенту, прежде
// чем оборвать транспорт.
const slowConsumerGrace = 5 * time.Second

// StatusReplaced - сокет закрыт, потому что тот же игрок подключился заново
// (другая вкладка, BR-3.2). Код из диапазона 4000-4999, который RFC 6455
// (7.4.2) отдаёт приложениям: клиент по нему понимает, что переподключаться
// не надо, иначе две вкладки будут выбивать друг друга по кругу.
const StatusReplaced websocket.StatusCode = 4001

// closeSpec - запрошенное закрытие: код и причина.
type closeSpec struct {
	code   websocket.StatusCode
	reason string
}

// closeSpecFor - код WebSocket для channel.CloseReason.
func closeSpecFor(reason channel.CloseReason) closeSpec {
	switch reason {
	case channel.CloseReasonReplaced:
		return closeSpec{StatusReplaced, "replaced"}
	case channel.CloseReasonChannelClosed:
		return closeSpec{websocket.StatusNormalClosure, "channel closed"}
	case channel.CloseReasonShutdown:
		return closeSpec{websocket.StatusGoingAway, "shutdown"}
	case channel.CloseReasonInternal:
		return closeSpec{websocket.StatusInternalError, "internal error"}
	default:
		return closeSpec{websocket.StatusInternalError, "unknown close reason"}
	}
}

// New оборачивает соединение. Лимит входящего сообщения ставится сразу:
// больше него - библиотека закроет сокет с 1009.
func New(conn *websocket.Conn, opts Options) *Socket {
	opts = opts.normalize()
	if opts.MaxMessageBytes > 0 {
		conn.SetReadLimit(opts.MaxMessageBytes)
	}
	return &Socket{
		conn:  conn,
		opts:  opts,
		queue: make([]channel.Message, 0, opts.OutboxSize),
		wake:  make(chan struct{}, 1),
	}
}

// Send кладёт сообщение в outbox, не блокируя. Если outbox полон:
//   - выбрасывается самый старый Droppable, если такой есть;
//   - иначе соединение закрывается кодом 1008 "slow consumer".
//
// После Close (или после выхода Run) Send - no-op.
func (s *Socket) Send(msg channel.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing != nil {
		return
	}
	if len(s.queue) >= s.opts.OutboxSize {
		// Ищем самый старый Droppable - его можно выбросить.
		oldest := -1
		for i, queued := range s.queue {
			if queued.Droppable {
				oldest = i
				break
			}
		}
		if oldest < 0 {
			// Выбрасывать нечего - важные сообщения терять нельзя.
			// Закрываем соединение: клиент безнадёжно отстал. Очередь ему
			// уже не нужна. Writer может висеть в Write на забитом TCP -
			// тогда кадр 1008 не уйдет, и транспорт рвем по таймеру.
			s.queue = nil
			s.closing = &closeSpec{websocket.StatusPolicyViolation, "slow consumer"}
			s.opts.Logger.Warn("ws: slow consumer, closing", logging.CloseCode(int(websocket.StatusPolicyViolation)))
			s.wakeWriter()
			time.AfterFunc(slowConsumerGrace, func() { _ = s.conn.CloseNow() })
			return
		}
		s.queue = append(s.queue[:oldest], s.queue[oldest+1:]...)
	}
	s.queue = append(s.queue, msg)
	s.wakeWriter()
}

// Close просит соединение закрыться кодом по причине. Writer сначала
// дописывает очередь, потом делает close-рукопожатие. Идемпотентен и
// не блокирует: если соединение уже закрывается, вызов игнорируется.
func (s *Socket) Close(reason channel.CloseReason) {
	s.mu.Lock()
	if s.closing != nil {
		s.mu.Unlock()
		return
	}
	spec := closeSpecFor(reason)
	s.closing = &spec
	s.mu.Unlock()
	s.wakeWriter()
}

// Run читает кадры до закрытия и возвращает ошибку, по которой можно
// узнать код закрытия через websocket.CloseStatus.
//
// Run возвращается, только когда все горутины соединения завершены:
// writer и pinger останавливаются, соединение закрыто.
func (s *Socket) Run(ctx context.Context, onMessage func([]byte)) error {
	// Writer и pinger стартуют сразу: writer нужен, чтобы отправить
	// close-рукопожатие, pinger - чтобы оборвать молчащего клиента.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	writeDone := make(chan error, 1)
	go func() { writeDone <- s.writeLoop(ctx) }()

	pingDone := make(chan error, 1)
	go func() { pingDone <- s.pingLoop(ctx) }()

	// Reader - в текущей горутине.
	readErr := s.readLoop(ctx, onMessage)
	if ctx.Err() != nil {
		// Контекст отменен: рукопожатие не ждем, клиент может не отвечать.
		_ = s.conn.CloseNow()
	}

	// Просим writer закрыть (если ещё не) и остановиться.
	s.mu.Lock()
	if s.closing == nil {
		s.closing = &closeSpec{websocket.StatusNormalClosure, ""}
	}
	s.mu.Unlock()
	s.wakeWriter()

	// Сначала writer: он допишет очередь и сделает close-рукопожатие.
	// Потом отменяем ctx - pinger выйдет сразу, а не на следующем тике.
	writeErr := <-writeDone
	cancel()
	<-pingDone // ошибка pinger не нужна: если он порвал сокет, это видно по readErr

	return pickRunErr(readErr, writeErr)
}

// pickRunErr выбирает, что вернуть из Run. Важнее ошибка с кодом закрытия:
// по ней вызывающий узнает код, который видел клиент. Reader первым: в нем
// close-кадр клиента (или эхо нашего). Writer, закрывая уже закрытое
// соединение, получает ошибку без кода - она вторична.
func pickRunErr(readErr, writeErr error) error {
	switch {
	case websocket.CloseStatus(readErr) != -1:
		return readErr
	case websocket.CloseStatus(writeErr) != -1:
		return writeErr
	case readErr != nil:
		return readErr
	default:
		return writeErr
	}
}

// readLoop читает кадры и отдаёт их onMessage. Возвращает ошибку чтения.
func (s *Socket) readLoop(ctx context.Context, onMessage func([]byte)) error {
	for {
		typ, data, err := s.conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("ws: read: %w", err)
		}
		// Ping, pong и close библиотека обрабатывает сама и сюда не отдаёт.
		// Проверка - на случай новых типов кадров.
		if typ != websocket.MessageText && typ != websocket.MessageBinary {
			continue
		}
		onMessage(data)
	}
}

// wakeWriter будит writer. Буфер 1: повторные сигналы не нужны, writer сам
// разберёт всю очередь.
func (s *Socket) wakeWriter() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
