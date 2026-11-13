package channel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BR-1.1: код - 6 цифр, первая не 0.
func TestNewCode_Format(t *testing.T) {
	t.Parallel()

	for range 10_000 {
		c, err := newCode()
		require.NoError(t, err)
		require.False(t, c.IsEmpty())

		s := c.String()
		require.Len(t, s, 6, s)
		require.NotEqual(t, byte('0'), s[0], s)
		for _, r := range s {
			require.True(t, r >= '0' && r <= '9', s)
		}
	}
}

// BR-1.1, BR-2.2: формат кода из запроса - ровно 6 цифр.
// Ведущий ноль формат проходит: такого канала просто не бывает (404).
func TestParseCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		wantErr bool
	}{
		{"482117", false},
		{"100000", false},
		{"999999", false},
		{"012345", false},
		{"", true},
		{"48211", true},
		{"4821170", true},
		{"48211a", true},
		{" 482117", true},
		{"482 17", true},
		{"-48211", true},
		{"0abcde", true}, // ошибка формата (422), а не «ведущий ноль»
		{"４８２１１７", true}, // полноширинные цифры
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			c, err := ParseCode(tt.in)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidCode)
				assert.True(t, c.IsEmpty())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.in, c.String())
		})
	}
}

func TestChannelCode_Zero(t *testing.T) {
	t.Parallel()

	var c Code
	assert.True(t, c.IsEmpty())
	assert.Empty(t, c.String())
}
