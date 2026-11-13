package channel

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

const (
	tokenBytes  = 32
	tokenMasked = "Token(***)"
)

var errUnmarshalUnsupported = errors.New("token: unmarshal not supported; use ParseToken")

// Token - токен игрока: 32 случайных байта в base64url без паддинга.
// Аккаунтов нет, поэтому токен - единственное удостоверение игрока.
//
// Поле приватное: снаружи пакета нельзя собрать невалидный токен.
// String, LogValue и MarshalJSON отдают маску без единого символа токена
// (NFR-3). Сырое значение - только через Reveal.
type Token struct {
	raw string // base64url(RawURLEncoding) от ровно tokenBytes байт
}

// newTokenFrom читает tokenBytes байт из r. Тесты подменяют источник (EFR-1.b).
func newTokenFrom(r io.Reader) (Token, error) {
	var b [tokenBytes]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return Token{}, fmt.Errorf("generate player token: %w", err)
	}
	return Token{raw: base64.RawURLEncoding.EncodeToString(b[:])}, nil
}

func newToken() (Token, error) {
	return newTokenFrom(rand.Reader)
}

// ParseToken разбирает токен из запроса: ровно 43 символа base64url, без '='.
func ParseToken(s string) (Token, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Token{}, fmt.Errorf("%w: not base64url: %w", ErrInvalidToken, err)
	}
	if len(b) != tokenBytes {
		return Token{}, fmt.Errorf("%w: length %d, want %d", ErrInvalidToken, len(b), tokenBytes)
	}
	return Token{raw: s}, nil
}

// IsEmpty - токен нулевой (не выдан).
func (t Token) IsEmpty() bool { return t.raw == "" }

// Reveal отдает сырой токен. Только для ответа клиенту, которому токен
// выдан (создание канала, вход). Никогда не логировать.
func (t Token) Reveal() string { return t.raw }

// Equal сравнивает токены за постоянное время (BR-1.2). Пустой токен не равен
// ничему: subtle.ConstantTimeCompare("", "") == 1, поэтому проверка обязательна.
func (t Token) Equal(other Token) bool {
	if t.IsEmpty() || other.IsEmpty() {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(t.raw), []byte(other.raw)) == 1
}

// String отдает маску.
func (t Token) String() string { return t.masked() }

// LogValue - маска для slog.
func (t Token) LogValue() slog.Value { return slog.StringValue(t.masked()) }

// MarshalJSON отдает маску, чтобы случайный json.Marshal не выдал секрет.
// В DTO ответа кладется строка из Reveal, а не Token.
func (t Token) MarshalJSON() ([]byte, error) {
	if t.IsEmpty() {
		return []byte("null"), nil
	}
	b, err := json.Marshal(t.masked())
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	return b, nil
}

// UnmarshalJSON запрещен: токен из запроса разбирается только через ParseToken.
func (*Token) UnmarshalJSON(_ []byte) error {
	return errUnmarshalUnsupported
}

func (t Token) masked() string {
	if t.IsEmpty() {
		return "Token(empty)"
	}
	return tokenMasked
}
