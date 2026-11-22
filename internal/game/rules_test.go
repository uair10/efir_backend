package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSquareHelpers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cell   Cell
		name   string
		origin Cell
	}{
		{Cell{0, 0}, "А1", Cell{0, 0}},
		{Cell{5, 7}, "Б2", Cell{5, 5}},
		{Cell{29, 19}, "Е4", Cell{25, 15}},
	} {
		assert.Equal(t, tc.name, SquareName(tc.cell.X, tc.cell.Y))
		assert.Equal(t, tc.origin, SquareOrigin(tc.name))
		assert.True(t, IsSquareName(tc.name))
	}
	assert.False(t, IsSquareName("A1"), "латинская A не входит в сетку")
	assert.Equal(t, Cell{0, 5}, SquareOrigin("A2"))
}

func TestTimeAndMarks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		seconds float64
		want    string
	}{{0, "22:00"}, {40, "00:00"}, {140, "05:00"}, {150, "05:00"}} {
		assert.Equal(t, tc.want, GameTime(tc.seconds))
	}
	for _, tc := range []struct {
		count int
		want  string
	}{{0, "пусто"}, {1, "~1 отметка"}, {12, "~12 отметок"}, {22, "~22 отметки"}} {
		assert.Equal(t, tc.want, Marks(tc.count))
	}
}

func TestMapHelpers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		x, y           int
		inBounds, core bool
	}{{-1, 0, false, false}, {29, 19, true, false}, {30, 20, false, false}, {14, 9, true, true}, {15, 10, true, true}, {16, 10, true, false}} {
		assert.Equal(t, tc.inBounds, InBounds(tc.x, tc.y))
		assert.Equal(t, tc.core, IsCore(tc.x, tc.y))
	}
	east := PostEast
	for _, tc := range []struct {
		x, y float64
		post *Post
		want GateID
	}{{3, 10, nil, GateW}, {26, 10, nil, GateE}, {3, 10, &east, GateSE}} {
		assert.Equal(t, tc.want, NearestGate(tc.x, tc.y, tc.post))
	}
	for _, tc := range []struct {
		gate GateID
		want Vec2
	}{{GateW, Vec2{10, 10}}, {GateE, Vec2{20, 10}}} {
		assert.Equal(t, tc.want, GateApproach(tc.gate, 5))
	}
}
