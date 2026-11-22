package game

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlowFieldOnEmptyMap(t *testing.T) {
	t.Parallel()
	field := ComputeFlowField(nil, nil)
	for y := range Rows {
		for x := range Cols {
			dx := max(CoreX-1-x, x-CoreX, 0)
			dy := max(CoreY-1-y, y-CoreY, 0)
			require.Equalf(t, dx+dy, field[CellIndex(x, y)], "cell (%d, %d)", x, y)
		}
	}
}

func TestFlowFieldTargets(t *testing.T) {
	t.Parallel()
	goal := Cell{X: 3, Y: 4}
	field := ComputeFlowField(func(int) bool { return true }, []Cell{goal, goal, {X: -1, Y: 1}})
	assert.Zero(t, field[CellIndex(goal.X, goal.Y)])
	assert.Equal(t, 15, field[CellIndex(goal.X+1, goal.Y)])

	for _, targets := range [][]Cell{{}, {{X: -1, Y: 1}}} {
		field = ComputeFlowField(nil, targets)
		for _, cost := range field {
			require.Equal(t, unreachableCost, cost)
		}
	}
}

func TestNextStepAroundBuildingAndThroughWall(t *testing.T) {
	t.Parallel()
	var paths Pathfinding
	from := Cell{X: CoreX - 3, Y: CoreY}
	barrier := Cell{X: from.X + 1, Y: from.Y}
	random := func() float64 { return 0 }

	assert.Equal(t, barrier, paths.NextStep(from, nil, random))
	paths.SetBuilding(barrier, true)
	assert.Equal(t, Cell{X: from.X, Y: from.Y - 1}, paths.NextStep(from, nil, random))

	for y := range Rows {
		paths.SetBuilding(Cell{X: barrier.X, Y: y}, true)
	}
	assert.Equal(t, barrier, paths.NextStep(from, nil, random))
}

func TestPathfindingCachesUntilOccupancyChanges(t *testing.T) {
	t.Parallel()
	var paths Pathfinding
	goal := Cell{X: 3, Y: 4}
	cell := Cell{X: 4, Y: 4}
	index := CellIndex(cell.X, cell.Y)
	core, target := paths.field(nil), paths.field(&goal)

	paths.SetBuilding(cell, false)
	assert.Same(t, core, paths.field(nil))
	assert.Same(t, target, paths.field(&goal))

	paths.SetBuilding(cell, true)
	assert.NotSame(t, core, paths.field(nil))
	assert.NotSame(t, target, paths.field(&goal))
	assert.Equal(t, 15, paths.field(&goal)[index])

	paths.SetBuilding(cell, false)
	assert.Equal(t, 1, paths.field(&goal)[index])
}

func TestNextStepJitterBreaksEqualCosts(t *testing.T) {
	t.Parallel()
	var paths Pathfinding
	from := Cell{X: CoreX - 2, Y: CoreY - 2}
	samples := []float64{0.9, 0, 0.1, 0}
	index := 0
	random := func() float64 {
		sample := samples[index]
		index++
		return sample
	}
	assert.Equal(t, Cell{X: from.X, Y: from.Y + 1}, paths.NextStep(from, nil, random))
	assert.Equal(t, len(samples), index)
}

func BenchmarkComputeFlowField(b *testing.B) {
	b.ReportAllocs()
	var buildings [Cols * Rows]bool
	for y := range Rows {
		buildings[CellIndex(CoreX-3, y)] = true
	}
	for b.Loop() {
		_ = ComputeFlowField(func(index int) bool { return buildings[index] }, nil)
	}
}
