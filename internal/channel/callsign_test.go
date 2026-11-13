package channel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// BR-1.4: фиксированный список из 8 позывных в этом порядке.
func TestCallsigns_Order(t *testing.T) {
	t.Parallel()

	want := []Callsign{"Первый", "Второй", "Заря", "Гранит", "Сокол", "Факел", "Гроза", "Тайга"}
	assert.Equal(t, want, callsigns[:])
}

func TestParseCallsign(t *testing.T) {
	t.Parallel()

	for _, c := range callsigns {
		got, ok := ParseCallsign(string(c))
		assert.True(t, ok, c)
		assert.Equal(t, c, got)
	}

	for _, s := range []string{"", "первый", "Первый ", "Third", "Ястреб"} {
		got, ok := ParseCallsign(s)
		assert.False(t, ok, s)
		assert.Empty(t, got, s)
	}
}

// BR-1.4: гость получает первый свободный позывной из списка.
func TestFirstFreeCallsign(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		taken []Callsign
		want  Callsign
	}{
		{name: "никто не занял", taken: nil, want: "Первый"},
		{name: "у хозяина Первый", taken: []Callsign{"Первый"}, want: "Второй"},
		{name: "у хозяина Второй", taken: []Callsign{"Второй"}, want: "Первый"},
		{name: "у хозяина Заря", taken: []Callsign{"Заря"}, want: "Первый"},
		{name: "заняты два первых", taken: []Callsign{"Второй", "Первый"}, want: "Заря"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := firstFreeCallsign(tt.taken...)
			assert.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Все позывные заняты - свободного нет. В канале на два поста не случится,
// но функция не должна вернуть пустой позывной как валидный.
func TestFirstFreeCallsign_AllTaken(t *testing.T) {
	t.Parallel()

	got, ok := firstFreeCallsign(callsigns[:]...)
	assert.False(t, ok)
	assert.Empty(t, got)
}
