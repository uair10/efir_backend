package game

// EnemyType - тип мутанта.
type EnemyType string

// Типы мутантов.
const (
	EnemyShakal EnemyType = "shakal"
	EnemyTusha  EnemyType = "tusha"
	EnemyNaryv  EnemyType = "naryv"
	EnemyRevun  EnemyType = "revun"
)

// EnemySpec - описание мутанта.
type EnemySpec struct {
	Name          string
	PluralName    string // множественное число: "шакалы"
	DestroyedText string // "уничтожен" или "уничтожена"
	HP            float64
	// Speed - клеток в секунду.
	Speed float64
	// DPS - урон постройкам в секунду.
	DPS float64
	// CoreDPS - урон ядру в секунду, пока грызёт.
	CoreDPS float64
	Desc    string
}

const destroyedMasculine = "уничтожен"

// Enemies - все мутанты по типу. Только для чтения.
//
//nolint:gochecknoglobals,mnd // таблица баланса с числами из эталона
var Enemies = map[EnemyType]EnemySpec{
	EnemyShakal: {
		Name:          "Шакал",
		PluralName:    "шакалы",
		DestroyedText: destroyedMasculine,
		HP:            24,
		Speed:         5,
		DPS:           10,
		CoreDPS:       2.5,
		Desc:          "быстрый, приходит стаями",
	},
	EnemyTusha: {
		Name:          "Туша",
		PluralName:    "туши",
		DestroyedText: "уничтожена",
		HP:            120,
		Speed:         1.5,
		DPS:           24,
		CoreDPS:       6,
		Desc:          "медленная, держит много урона",
	},
	EnemyNaryv: {
		Name:          "Нарыв",
		PluralName:    "нарывы",
		DestroyedText: destroyedMasculine,
		HP:            44,
		Speed:         2.5,
		DPS:           0,
		CoreDPS:       0,
		Desc:          "лопается у построек: 45 урона и по 15 соседним",
	},
	EnemyRevun: {
		Name:          "Ревун",
		PluralName:    "ревуны",
		DestroyedText: destroyedMasculine,
		HP:            68,
		Speed:         1.7,
		DPS:           0,
		CoreDPS:       0,
		Desc:          "охотится на ретрансляторы, пока жив — глушит эфир, слепит радары в радиусе 4,5",
	},
}

const (
	// NaryvCore - нарыв у ядра снимает столько процентов сразу.
	NaryvCore = 10
	// NaryvBurst - урон от взрыва нарыва.
	NaryvBurst = 45
	// NaryvSplash - урон соседним постройкам.
	NaryvSplash = 15
)
