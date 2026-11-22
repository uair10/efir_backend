package game

const pathTieBreak = 0.3

// Pathfinding хранит занятость клеток и кэш полей путей для одной симуляции.
// Нулевое значение соответствует пустой карте.
type Pathfinding struct {
	buildings [Cols * Rows]bool
	core      *FlowField
	targets   map[Cell]*FlowField
}

// SetBuilding отмечает наличие постройки и сбрасывает кэш полей при изменении
// занятости клетки. Клетки за пределами карты пропускаются.
func (p *Pathfinding) SetBuilding(cell Cell, present bool) {
	if !InBounds(cell.X, cell.Y) {
		return
	}
	index := CellIndex(cell.X, cell.Y)
	if p.buildings[index] == present {
		return
	}
	p.buildings[index] = present
	p.core = nil
	clear(p.targets)
}

// NextStep выбирает соседа с наименьшей суммой стоимости из поля и случайной
// добавки из [0, 0.3). Функция random возвращает число из [0, 1) от генератора
// симуляции. При target == nil цель - ядро. Мутант остаётся на месте, если
// он уже у цели либо его клетка или цель находятся за пределами карты.
func (p *Pathfinding) NextStep(from Cell, target *Cell, random func() float64) Cell {
	if !InBounds(from.X, from.Y) || (target != nil && !InBounds(target.X, target.Y)) {
		return from
	}
	field := p.field(target)
	if field[CellIndex(from.X, from.Y)] == 0 {
		return from
	}

	// Стоимости целые, а добавка меньше единицы, поэтому она влияет только
	// на выбор между соседями с одинаковой стоимостью.
	best := from
	bestScore := float64(unreachableCost)
	for _, offset := range cardinalOffsets {
		next := Cell{X: from.X + offset.X, Y: from.Y + offset.Y}
		if !InBounds(next.X, next.Y) {
			continue
		}
		score := float64(field[CellIndex(next.X, next.Y)]) + random()*pathTieBreak
		if score < bestScore {
			best, bestScore = next, score
		}
	}
	return best
}

func (p *Pathfinding) field(target *Cell) *FlowField {
	hasBuilding := func(index int) bool { return p.buildings[index] }
	if target == nil {
		if p.core == nil {
			field := ComputeFlowField(hasBuilding, nil)
			p.core = &field
		}
		return p.core
	}
	if field := p.targets[*target]; field != nil {
		return field
	}
	if p.targets == nil {
		p.targets = make(map[Cell]*FlowField)
	}
	field := ComputeFlowField(hasBuilding, []Cell{*target})
	p.targets[*target] = &field
	return &field
}
