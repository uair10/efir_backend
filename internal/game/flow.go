package game

const (
	buildingPenalty = 14
	heapArity       = 2
	// unreachableCost больше стоимости любого маршрута без повторных посещений, даже через постройки.
	unreachableCost = Cols*Rows*(1+buildingPenalty) + 1
)

// cardinalOffsets задаёт четыре направления в порядке эталонной симуляции.
//
//nolint:gochecknoglobals // внутренняя таблица направлений движения
var cardinalOffsets = [...]Cell{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

// FlowField хранит стоимость маршрута для каждой клетки по индексу CellIndex.
type FlowField [Cols * Rows]int

type flowItem struct {
	cost  int
	index int
}

// flowHeap - куча, упорядоченная по стоимости маршрута.
type flowHeap []flowItem

func (h *flowHeap) push(item flowItem) {
	*h = append(*h, item)
	// Поднимаем новый лист к корню, пока не восстановится порядок кучи.
	for child := len(*h) - 1; child > 0; {
		parent := (child - 1) / heapArity
		if (*h)[parent].cost <= (*h)[child].cost {
			break
		}
		(*h)[parent], (*h)[child] = (*h)[child], (*h)[parent]
		child = parent
	}
}

// pop извлекает элемент с наименьшей стоимостью из непустой кучи.
func (h *flowHeap) pop() flowItem {
	top := (*h)[0]
	last := len(*h) - 1
	(*h)[0] = (*h)[last]
	*h = (*h)[:last]

	// Последний лист занял место корня. Опускаем его через потомка с меньшей
	// стоимостью, пока стоимость родителя не станет меньше или равна стоимостям потомков.
	for parent := 0; ; {
		child := heapArity*parent + 1
		if child >= len(*h) {
			break
		}
		right := child + 1
		if right < len(*h) && (*h)[right].cost < (*h)[child].cost {
			child = right
		}
		if (*h)[parent].cost <= (*h)[child].cost {
			break
		}
		(*h)[parent], (*h)[child] = (*h)[child], (*h)[parent]
		parent = child
	}
	return top
}

func coreFlowTargets() [4]Cell {
	return [4]Cell{
		{CoreX - 1, CoreY - 1},
		{CoreX, CoreY - 1},
		{CoreX - 1, CoreY},
		{CoreX, CoreY},
	}
}

// ComputeFlowField возвращает для каждой клетки стоимость самого дешёвого маршрута
// через неё к одной из целей. При targets == nil целями служат клетки ядра.
// Пустой слайс оставляет все клетки со значением unreachableCost. Цели за границами
// карты пропускаются. hasBuilding проверяет наличие постройки по индексу клетки.
// При hasBuilding == nil карта считается пустой.
func ComputeFlowField(hasBuilding func(index int) bool, targets []Cell) FlowField {
	if targets == nil {
		core := coreFlowTargets()
		targets = core[:]
	}

	var field FlowField
	for index := range field {
		field[index] = unreachableCost
	}

	queue := seedFlowQueue(&field, targets)

	for len(queue) > 0 {
		current := queue.pop()
		// Если нашли более дешёвый маршрут, очередь уже получила новую запись
		// для этой клетки. Обходим соседей только по записи с актуальной стоимостью.
		if current.cost != field[current.index] {
			continue
		}
		x, y := current.index%Cols, current.index/Cols
		for _, offset := range cardinalOffsets {
			nx, ny := x+offset.X, y+offset.Y
			if !InBounds(nx, ny) {
				continue
			}

			// Поиск идёт от целей. При обновлении соседа учитываем стоимость входа
			// в его клетку, поскольку при движении к цели мутант сначала войдёт
			// в неё. Через постройку можно пройти с дополнительным штрафом.
			next := CellIndex(nx, ny)
			cost := current.cost + 1
			if hasBuilding != nil && hasBuilding(next) {
				cost += buildingPenalty
			}
			if cost < field[next] {
				field[next] = cost
				queue.push(flowItem{cost: cost, index: next})
			}
		}
	}
	return field
}

func seedFlowQueue(field *FlowField, targets []Cell) flowHeap {
	// Запускаем алгоритм Дейкстры одновременно от всех целей. Так каждая клетка
	// получит стоимость пути к самой дешёвой для неё цели. Стоимость в клетке
	// цели равна нулю, даже если там есть постройка.
	queue := make(flowHeap, 0, len(field))
	for _, target := range targets {
		if !InBounds(target.X, target.Y) {
			continue
		}
		index := CellIndex(target.X, target.Y)
		if field[index] != 0 {
			field[index] = 0
			queue.push(flowItem{cost: 0, index: index})
		}
	}

	return queue
}
