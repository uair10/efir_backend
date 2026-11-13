package channel

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BR-1.2: токен - 32 байта из crypto/rand в base64url без паддинга.
func TestNewToken_Format(t *testing.T) {
	t.Parallel()

	tok, err := newToken()
	require.NoError(t, err)
	require.False(t, tok.IsEmpty())

	s := tok.raw
	assert.Len(t, s, 43, "32 байта в base64 без паддинга - 43 символа")
	assert.NotContains(t, s, "=")
	assert.NotContains(t, s, "+")
	assert.NotContains(t, s, "/")

	raw, err := base64.RawURLEncoding.DecodeString(s)
	require.NoError(t, err)
	assert.Len(t, raw, tokenBytes)
}

// BR-1.2: токены случайные, повторов нет.
func TestNewToken_Unique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		tok, err := newToken()
		require.NoError(t, err)
		_, dup := seen[tok.raw]
		require.False(t, dup, "повтор токена")
		seen[tok.raw] = struct{}{}
	}
}

// BR-1.2, EFR-1.b: источник случайности подменяется в тестах.
func TestNewTokenFrom(t *testing.T) {
	t.Parallel()

	src := make([]byte, tokenBytes)
	for i := range src {
		src[i] = byte(i)
	}

	tok, err := newTokenFrom(bytes.NewReader(src))
	require.NoError(t, err)
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(src), tok.raw)
}

// BR-1.2: ошибка источника или нехватка байт - ошибка, а не короткий токен.
func TestNewTokenFrom_Errors(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	tests := []struct {
		name string
		src  *bytes.Reader
	}{
		{name: "мало байт", src: bytes.NewReader(make([]byte, tokenBytes-1))},
		{name: "пусто", src: bytes.NewReader(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tok, err := newTokenFrom(tt.src)
			require.Error(t, err)
			assert.True(t, tok.IsEmpty())
		})
	}

	t.Run("ошибка чтения", func(t *testing.T) {
		t.Parallel()

		tok, err := newTokenFrom(iotest.ErrReader(errBoom))
		require.ErrorIs(t, err, errBoom)
		assert.True(t, tok.IsEmpty())
	})
}

// BR-1.2: токен из query сокета разбирается обратно, мусор отвергается.
func TestParseToken(t *testing.T) {
	t.Parallel()

	good, err := newToken()
	require.NoError(t, err)

	parsed, err := ParseToken(good.raw)
	require.NoError(t, err)
	assert.True(t, parsed.Equal(good))

	bad := []struct {
		name string
		in   string
	}{
		{"пусто", ""},
		{"короткий", good.raw[:42]},
		{"длинный", good.raw + "A"},
		{"с паддингом", base64.URLEncoding.EncodeToString(make([]byte, tokenBytes))},
		{"обычный base64", strings.Repeat("+", 43)},
		{"не base64", strings.Repeat("!", 43)},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tok, err := ParseToken(tt.in)
			require.ErrorIs(t, err, ErrInvalidToken)
			assert.True(t, tok.IsEmpty())
		})
	}
}

// BR-1.2: сравнение токенов.
func TestToken_Equal(t *testing.T) {
	t.Parallel()

	a, err := newToken()
	require.NoError(t, err)
	b, err := newToken()
	require.NoError(t, err)

	aCopy, err := ParseToken(a.raw)
	require.NoError(t, err)

	assert.True(t, a.Equal(a)) //nolint:gocritic // проверяем рефлексивность
	assert.True(t, a.Equal(aCopy))
	assert.False(t, a.Equal(b))
	assert.False(t, a.Equal(Token{}))
	assert.False(t, Token{}.Equal(a))
}

// NFR-3: токен не попадает в логи целиком.
func TestToken_NotInSlog(t *testing.T) {
	t.Parallel()

	tok, err := newToken()
	require.NoError(t, err)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Info("test", "token", tok)

	assert.NotContains(t, buf.String(), tok.raw)
}

func TestToken_NotInSlogStruct(t *testing.T) {
	t.Parallel()

	type payload struct {
		Token Token
		User  string
	}
	tok, err := newToken()
	require.NoError(t, err)
	// Настоящий секрет достаем через raw напрямую - в тесте пакета можно.
	secret := tok.raw

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("test", "payload", payload{Token: tok, User: "u1"})

	assert.NotContains(t, buf.String(), secret, "slog не должен раскрывать raw при логировании структуры")
}

// EFR-2: клиенту отдаётся сырой токен, и только через Reveal.
func TestToken_Reveal(t *testing.T) {
	t.Parallel()

	tok, err := newToken()
	require.NoError(t, err)

	assert.Equal(t, tok.raw, tok.Reveal())

	back, err := ParseToken(tok.Reveal())
	require.NoError(t, err)
	assert.True(t, back.Equal(tok))

	assert.Empty(t, Token{}.Reveal())
}

// NFR-3: маска не раскрывает ни одного символа токена.
func TestToken_Masked(t *testing.T) {
	t.Parallel()

	tok, err := newToken()
	require.NoError(t, err)

	b, err := json.Marshal(tok)
	require.NoError(t, err)

	for _, s := range []string{tok.String(), fmt.Sprintf("%+v", struct{ T Token }{tok}), string(b)} {
		assert.Contains(t, s, "Token(***)")
		assert.NotContains(t, s, tok.raw[:4])
	}
	assert.Equal(t, "Token(empty)", Token{}.String())
}
