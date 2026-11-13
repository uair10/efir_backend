package channel

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
)

const (
	maxCodeAttempts = 10 // попыток подобрать свободный код
	channelIDBytes  = 16
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

	mu     sync.Mutex
	byID   map[string]*Channel // все каналы, включая одиночные
	byCode map[Code]*Channel   // тоже все: одиночный канал занимает код
}

// Option настраивает Registry при создании.
type Option func(*Registry)

// WithCodeGenerator подменяет генератор кодов (EFR-1.b). Нужен тестам,
// чтобы подстроить коллизии.
func WithCodeGenerator(gen func() (Code, error)) Option {
	return func(r *Registry) { r.codeGen = gen }
}

// NewRegistry создает пустой реестр на maxChannels каналов (GAME_MAX_CHANNELS).
func NewRegistry(maxChannels int, opts ...Option) *Registry {
	r := &Registry{
		maxChannels: maxChannels,
		codeGen:     newCode,
		byID:        make(map[string]*Channel),
		byCode:      make(map[Code]*Channel),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Create открывает канал в режиме mode. Возвращает канал и токен хозяина,
// который уже сидит на west.
//
// Проверка лимита, выбор кода и вставка - под одним r.mu: иначе два вызова
// получат один код или вместе превысят лимит.
//
// Ошибки:
//   - ErrTooManyChannels - каналов уже maxChannels; генератор кодов
//     при этом не вызывается.
//   - ErrCodeCollision - maxCodeAttempts кодов подряд заняты.
//   - ошибка генератора кода или токена, обернутая.
func (r *Registry) Create(mode GameMode) (*Channel, Token, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	ch, tok, err := newChannel(id, code, mode)
	if err != nil {
		return nil, Token{}, fmt.Errorf("registry: create channel: %w", err)
	}
	r.byID[id] = ch
	r.byCode[code] = ch
	return ch, tok, nil
}

// ByCode ищет канал для входа гостя; дальше гость входит через ch.Join().
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
