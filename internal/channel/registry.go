package channel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

const (
	maxCodeAttempts = 10 // попыток подобрать свободный код
	channelIDBytes  = 16
	defaultTick     = 50 * time.Millisecond // GAME_TICK по умолчанию
)

var errEmptyCode = errors.New("code generator returned empty code")

// Registry - справочник открытых каналов. Один на процесс, игровой логики
// в нем нет: рассадкой и токенами занимается сам канал.
//
// Что он делает:
//   - Выдает коды, уникальные среди живых каналов, с повтором при коллизии.
//   - Не держит больше GAME_MAX_CHANNELS каналов: все живет в памяти.
//   - Находит каналы по коду (для входа, одиночные скрыты) и по id (для сокета).
//   - Удаляет каналы: хозяин ушел, простой, SIGTERM. Код снова свободен.
//   - Безопасен для вызова из многих горутин.
type Registry struct {
	maxChannels int
	codeGen     func() (Code, error)
	tick        time.Duration
	logger      *slog.Logger // базовый логгер каналов, без request_id

	mu     sync.Mutex
	byID   map[string]*Channel // все каналы, включая одиночные
	byCode map[Code]*Channel   // тоже все: одиночный канал занимает код
	closed bool                // после Close: Create отказывает, иначе канал не остановит никто
}

// Option настраивает Registry при создании.
type Option func(*Registry)

// WithCodeGenerator подменяет генератор кодов (EFR-1.b). Нужен тестам,
// чтобы подстроить коллизии.
func WithCodeGenerator(gen func() (Code, error)) Option {
	return func(r *Registry) { r.codeGen = gen }
}

// WithTick задает шаг времени каналов (GAME_TICK).
func WithTick(d time.Duration) Option {
	return func(r *Registry) { r.tick = d }
}

// WithLogger задает базовый логгер каналов. Без него каналы молчат.
func WithLogger(l *slog.Logger) Option {
	return func(r *Registry) { r.logger = l }
}

// NewRegistry создает пустой реестр на maxChannels каналов (GAME_MAX_CHANNELS).
func NewRegistry(maxChannels int, opts ...Option) *Registry {
	r := &Registry{
		maxChannels: maxChannels,
		codeGen:     newCode,
		tick:        defaultTick,
		logger:      logging.Discard(),
		byID:        make(map[string]*Channel),
		byCode:      make(map[Code]*Channel),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Create открывает канал в режиме mode. Возвращает канал и токен хозяина,
// который уже сидит на west. Событие создания пишется логгером из ctx
// (с request_id создателя) и channel_id: по нему находится вся история канала.
//
// Проверка лимита, выбор кода и вставка - под одним r.mu: иначе два вызова
// получат один код или вместе превысят лимит.
//
// Ошибки:
//   - ErrRegistryClosed - реестр закрыт (сервер останавливается).
//   - ErrTooManyChannels - каналов уже maxChannels; генератор кодов
//     при этом не вызывается.
//   - ErrCodeCollision - maxCodeAttempts кодов подряд заняты.
//   - ошибка генератора кода или токена, обернутая.
func (r *Registry) Create(ctx context.Context, mode GameMode) (*Channel, Token, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, Token{}, ErrRegistryClosed
	}
	if len(r.byID) >= r.maxChannels {
		return nil, Token{}, ErrTooManyChannels
	}
	code, err := r.freeCode()
	if err != nil {
		return nil, Token{}, fmt.Errorf("registry: create channel: %w", err)
	}
	id, err := newChannelID()
	if err != nil {
		return nil, Token{}, fmt.Errorf("registry: create channel: %w", err)
	}
	ch, tok, err := startChannel(channelConfig{id: id, code: code, mode: mode, tick: r.tick, log: r.logger})
	if err != nil {
		return nil, Token{}, fmt.Errorf("registry: create channel: %w", err)
	}
	r.byID[id] = ch
	r.byCode[code] = ch
	logging.FromCtx(ctx).Info("channel created",
		logging.ChannelID(id),
		logging.ChannelCode(code.String()),
		slog.String("mode", string(mode)),
	)
	return ch, tok, nil
}

// Close останавливает все каналы причиной CloseReasonShutdown (клиенты
// получат 1001) и ждет, пока их горутины выйдут. Реестр после этого пуст,
// а Create отвечает ErrRegistryClosed. Повторный вызов ничего не делает.
func (r *Registry) Close() {
	r.mu.Lock()
	r.closed = true
	chans := make([]*Channel, 0, len(r.byID))
	for _, ch := range r.byID {
		chans = append(chans, ch)
	}
	r.byID = make(map[string]*Channel)
	r.byCode = make(map[Code]*Channel)
	r.mu.Unlock()

	for _, ch := range chans {
		ch.Stop(CloseReasonShutdown)
	}
	for _, ch := range chans {
		ch.Wait()
	}
}

// ByCode ищет канал для входа гостя; дальше гость входит через ch.Join(ctx).
//
// Одиночный канал отдает ErrNotFound, как будто его нет: незачем
// подтверждать, что такой код существует.
func (r *Registry) ByCode(code Code) (*Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.byCode[code]
	if !ok || ch.mode == GameModeSolo {
		return nil, ErrNotFound
	}
	return ch, nil
}

// ByID ищет канал для подключения сокета. Находит любой канал,
// в том числе одиночный: хозяину надо в него подключиться.
func (r *Registry) ByID(id string) (*Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return ch, nil
}

// Remove удаляет канал и освобождает его код. Неизвестный или уже удаленный
// id - не ошибка: закрыть канал могут сразу несколько причин.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.byID[id]
	if !ok {
		return
	}
	delete(r.byID, id)
	delete(r.byCode, ch.code)
	ch.Stop(CloseReasonChannelClosed) // не блокирует; клиенты получат 1000
}

// Len - число открытых каналов, включая одиночные.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

// freeCode подбирает код, не занятый живым каналом. Вызывать под r.mu.
func (r *Registry) freeCode() (Code, error) {
	for range maxCodeAttempts {
		c, err := r.codeGen()
		if err != nil {
			return Code{}, err
		}
		if c.IsEmpty() {
			return Code{}, errEmptyCode
		}
		if _, taken := r.byCode[c]; !taken {
			return c, nil
		}
	}
	return Code{}, ErrCodeCollision
}

// newChannelID генерирует id вида "c_" + base64url(16 байт crypto/rand).
// С кодом не связан и не угадывается. Уникальность не проверяем:
// коллизия 128-битных случайных значений практически невозможна.
func newChannelID() (string, error) {
	var b [channelIDBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate channel id: %w", err)
	}
	return "c_" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}
