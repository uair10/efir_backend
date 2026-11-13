package channel

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
)

const (
	codeLen   = 6
	codeMin   = 100_000 // минимальный код, первая цифра не 0
	codeRange = 900_000 // число кодов в 100000..999999
)

// Code - 6-значный код канала для входа по нему.
type Code struct {
	value string
}

// newCodeFrom генерирует код из диапазона 100000..999999.
func newCodeFrom(r io.Reader) (Code, error) {
	n, err := rand.Int(r, big.NewInt(codeRange)) // [0, codeRange)
	if err != nil {
		return Code{}, fmt.Errorf("generate code: %w", err)
	}
	return Code{value: fmt.Sprintf("%06d", n.Int64()+codeMin)}, nil
}

func newCode() (Code, error) {
	return newCodeFrom(rand.Reader)
}

// ParseCode разбирает код из запроса: ровно 6 цифр ASCII.
//
// Ведущий ноль формат проходит: такой код никогда не выдается,
// поэтому Registry.ByCode ответит ErrNotFound, то есть 404, а не 422.
func ParseCode(s string) (Code, error) {
	if len(s) != codeLen {
		return Code{}, fmt.Errorf("%w: length %d, want %d", ErrInvalidCode, len(s), codeLen)
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return Code{}, fmt.Errorf("%w: must be digits only", ErrInvalidCode)
		}
	}
	return Code{value: s}, nil
}

// IsEmpty - код нулевой (не задан).
func (c Code) IsEmpty() bool { return c.value == "" }

func (c Code) String() string { return c.value }

// UnmarshalText парсит код из json.
func (c *Code) UnmarshalText(text []byte) error {
	parsed, err := ParseCode(string(text))
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}
