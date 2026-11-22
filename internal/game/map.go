package game

import (
	"fmt"
	"math"
	"slices"
	"strconv"
)

// Размеры карты и зоны влияния в клетках.
const (
	Cols = 30
	Rows = 20
	// Mid - граница Запада и Востока по x.
	Mid = 15
	// CoreX и CoreY задают центр ядра.
	CoreX = 15
	CoreY = 10
	// InitialCoreHP is the core integrity at the beginning of a shift.
	InitialCoreHP = 100
	// CoreVision - собственный радар ядра; ревун рядом его слепит.
	CoreVision = 4.5
	// JamR - ревун слепит радары и ретрансляторы в этом радиусе.
	JamR = 4.5
	// FonAfter - фон держится в эфире после гибели последнего ревуна,
	// с времени симуляции (≈ час игровой ночи). Рацию и канал данных в это
	// время держит только работающий ретранслятор.
	FonAfter = 20
	// RevunHoverRelay - ревун встаёт на таком расстоянии от ретранслятора:
	// тот попадает в радиус глушения.
	RevunHoverRelay = 3
	// RevunHoverCore - ревун без ретрансляторов висит у ядра на таком расстоянии.
	RevunHoverCore = 3.8
	// SquareSize - сторона квадрата А1–Е4 в клетках.
	SquareSize = 5
)

// Letters - буквы квадратов А1–Е4. Руны, не байты: кириллица в UTF-8 - 2 байта.
//
//nolint:gochecknoglobals // таблица правил смены
var Letters = []rune("АБВГДЕ")

// GateID - идентификатор ворот.
type GateID string

// Идентификаторы ворот по сторонам карты.
const (
	GateNW GateID = "NW"
	GateW  GateID = "W"
	GateSW GateID = "SW"
	GateNE GateID = "NE"
	GateE  GateID = "E"
	GateSE GateID = "SE"
)

// GateSpec - описание ворот.
type GateSpec struct {
	Name string
	Post Post
	From string // "с запада"
	To   string // "на запад"
	Over string // "над западом"
}

// Gates - все ворота. Только для чтения.
//
//nolint:gochecknoglobals // таблица правил смены
var Gates = map[GateID]GateSpec{
	GateNW: {Name: "СЗ", Post: PostWest, From: "с северо-запада", To: "на северо-запад", Over: "над северо-западом"},
	GateW:  {Name: "З", Post: PostWest, From: "с запада", To: "на запад", Over: "над западом"},
	GateSW: {Name: "ЮЗ", Post: PostWest, From: "с юго-запада", To: "на юго-запад", Over: "над юго-западом"},
	GateNE: {Name: "СВ", Post: PostEast, From: "с северо-востока", To: "на северо-восток", Over: "над северо-востоком"},
	GateE:  {Name: "В", Post: PostEast, From: "с востока", To: "на восток", Over: "над востоком"},
	GateSE: {Name: "ЮВ", Post: PostEast, From: "с юго-востока", To: "на юго-восток", Over: "над юго-востоком"},
}

// GateIDs - все ворота в порядке.
//
//nolint:gochecknoglobals // таблица правил смены
var GateIDs = []GateID{GateNW, GateW, GateSW, GateNE, GateE, GateSE}

// NeighboringGates - соседние ворота той же стороны: туда уходит волна, когда ворота
// закрыты баррикадой.
//
//nolint:gochecknoglobals // таблица правил смены
var NeighboringGates = map[GateID][]GateID{
	GateNW: {GateW},
	GateW:  {GateNW, GateSW},
	GateSW: {GateW},
	GateNE: {GateE},
	GateE:  {GateNE, GateSE},
	GateSE: {GateE},
}

// GatePos - середина ворот на краю карты, в клетках.
//
//nolint:gochecknoglobals // таблица правил смены
var GatePos = map[GateID]Vec2{
	GateNW: {7.5, 0},
	GateW:  {0, 10},
	GateSW: {7.5, 20},
	GateNE: {22.5, 0},
	GateE:  {30, 10},
	GateSE: {22.5, 20},
}

// GateCells - клетки края карты, из которых выходят мутанты.
//
//nolint:gochecknoglobals // таблица правил смены
var GateCells = func() map[GateID][]Cell {
	c := map[GateID][]Cell{
		GateNW: {}, GateW: {}, GateSW: {}, GateNE: {}, GateE: {}, GateSE: {},
	}
	for x := 1; x <= 13; x++ {
		c[GateNW] = append(c[GateNW], Cell{x, 0})
		c[GateSW] = append(c[GateSW], Cell{x, Rows - 1})
	}
	for x := 16; x <= 28; x++ {
		c[GateNE] = append(c[GateNE], Cell{x, 0})
		c[GateSE] = append(c[GateSE], Cell{x, Rows - 1})
	}
	for y := 3; y <= 16; y++ {
		c[GateW] = append(c[GateW], Cell{0, y})
		c[GateE] = append(c[GateE], Cell{Cols - 1, y})
	}
	return c
}()

// CellIndex - линейный индекс клетки.
func CellIndex(x, y int) int { return y*Cols + x }

// InBounds - клетка в пределах карты.
func InBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < Cols && y < Rows
}

// IsCore - клетка ядра.
func IsCore(x, y int) bool {
	return (x == CoreX-1 || x == CoreX) && (y == CoreY-1 || y == CoreY)
}

// IsDieselCell - клетка, где стоит (или будет стоять после ремонта) дизель.
func IsDieselCell(x, y int) bool {
	return (DieselPos[PostWest].X == float64(x) && DieselPos[PostWest].Y == float64(y)) ||
		(DieselPos[PostEast].X == float64(x) && DieselPos[PostEast].Y == float64(y))
}

// OtherPost - противоположный пост.
func OtherPost(p Post) Post {
	if p == PostWest {
		return PostEast
	}
	return PostWest
}

// PostOfX - половина карты, которую держит пост.
func PostOfX(x int) Post {
	if x < Mid {
		return PostWest
	}
	return PostEast
}

// PostName - человекочитаемое имя поста.
//
//nolint:gochecknoglobals // таблица правил смены
var PostName = map[Post]string{
	PostWest: "Запад",
	PostEast: "Восток",
}

// clamp - ограничивает v диапазоном [lo, hi].
func clamp[T int | float64](v, lo, hi T) T {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// SquareName - "Б2": квадрат по координатам клетки.
func SquareName(x, y int) string {
	col := Letters[clamp(x/SquareSize, 0, Cols/SquareSize-1)]
	return string(col) + strconv.Itoa(clamp(y/SquareSize, 0, Rows/SquareSize-1)+1)
}

// SquareOrigin - левый верхний угол квадрата в клетках.
// Нераспознанная буква или цифра заменяется первой колонкой или строкой.
func SquareOrigin(name string) Cell {
	rs := []rune(name)
	col := 0
	if len(rs) > 0 {
		for i, r := range Letters {
			if r == rs[0] {
				col = i
				break
			}
		}
	}
	row := 0
	if len(rs) > 1 {
		// name[1] - цифра '1'..'4'; преобразуем в 0..3.
		if rs[1] >= '1' && rs[1] <= '4' {
			row = int(rs[1] - '1')
		}
	}
	return Cell{col * SquareSize, row * SquareSize}
}

// IsSquareName - name вида "А1".."Е4".
func IsSquareName(name string) bool {
	rs := []rune(name)
	const squareNameLength = 2
	if len(rs) != squareNameLength {
		return false
	}
	return slices.Contains(Letters, rs[0]) && rs[1] >= '1' && rs[1] <= '4'
}

// plural - выбор формы слова по числу.
//
//nolint:mnd // правила русских числительных зависят от последних двух цифр
func plural(n int, one, few, many string) string {
	d := n % 10
	h := n % 100
	switch {
	case d == 1 && h != 11:
		return one
	case (d == 2 || d == 3 || d == 4) && (h < 12 || h > 14):
		return few
	default:
		return many
	}
}

// Marks - "~3 отметки": число отметок на радаре с правильным окончанием.
func Marks(n int) string {
	if n == 0 {
		return "пусто"
	}
	return fmt.Sprintf("~%d %s", n, plural(n, "отметка", "отметки", "отметок"))
}

// Canisters - "1 канистра", "2 канистры", "5 канистр".
func Canisters(n int) string {
	return fmt.Sprintf("%d %s", n, plural(n, "канистра", "канистры", "канистр"))
}

// NearestGate - ворота, ближайшие по направлению от ядра; post ограничивает
// выбор своей стороной.
func NearestGate(x, y float64, post *Post) GateID {
	a := math.Atan2(y+0.5-CoreY, x+0.5-CoreX)
	best := GateW
	bd := math.Inf(1)
	for _, g := range GateIDs {
		if post != nil && Gates[g].Post != *post {
			continue
		}
		p := GatePos[g]
		ga := math.Atan2(p.Y-CoreY, p.X-CoreX)
		d := math.Abs(math.Atan2(math.Sin(a-ga), math.Cos(a-ga)))
		if d < bd {
			bd = d
			best = g
		}
	}
	return best
}

// IsGateCell - клетка края карты, на которой стоит баррикада ворот g.
func IsGateCell(g GateID, x, y int) bool {
	for _, c := range GateCells[g] {
		if c.X == x && c.Y == y {
			return true
		}
	}
	return false
}

// GateApproach - точка на пути от ядра к воротам на расстоянии dist.
func GateApproach(g GateID, dist float64) Vec2 {
	p := GatePos[g]
	dx := p.X - CoreX
	dy := p.Y - CoreY
	d := math.Hypot(dx, dy)
	return Vec2{CoreX + (dx/d)*dist, CoreY + (dy/d)*dist}
}
