package game

// BuildingType - тип постройки.
type BuildingType string

// Типы построек.
const (
	BuildingTurret BuildingType = "turret"
	BuildingRadar  BuildingType = "radar"
	BuildingRelay  BuildingType = "relay"
	BuildingDiesel BuildingType = "diesel"
)

// BuildingSpec - описание постройки.
type BuildingSpec struct {
	Name  string
	Cost  int
	HP    int
	Key   string // клавиша на панели
	Desc  string
	Fixed bool // стоит с начала смены: не строится и не разбирается
}

// Buildings - все постройки по типу. Только для чтения.
//
//nolint:gochecknoglobals,mnd // таблица баланса с числами из эталона
var Buildings = map[BuildingType]BuildingSpec{
	BuildingTurret: {
		Name: "Турель",
		Cost: 20,
		HP:   40,
		Key:  "2",
		Desc: "Радиус 3,5. Стреляет только по тем, кого видит радар.",
	},
	BuildingRadar: {
		Name: "Радар",
		Cost: 15,
		HP:   25,
		Key:  "3",
		Desc: "Обзор радиусом 5. Рядом с ревуном слепнет.",
	},
	BuildingRelay: {
		Name: "Ретранслятор",
		Cost: 25,
		HP:   30,
		Key:  "5",
		Desc: "Держит рацию и канал данных, пока рядом нет ревуна. Ревуны идут прямо на него.",
	},
	BuildingDiesel: {
		Name:  "Дизель",
		Cost:  0,
		HP:    60,
		Key:   "",
		Desc:  "Стоит у ядра. Канистра (4) горит два часа ночи и даёт 20 ⚡. Работает — шумит: мутанты рядом идут на него. Сломают — топливо в баке пропадёт, утром бригада починит.",
		Fixed: true,
	},
}

// BuildingTypes - все типы построек.
//
//nolint:gochecknoglobals // таблица правил смены
var BuildingTypes = []BuildingType{
	BuildingTurret, BuildingRadar, BuildingRelay, BuildingDiesel,
}

// Buildable - то, что игрок строит сам.
//
//nolint:gochecknoglobals // таблица правил смены
var Buildable = []BuildingType{BuildingTurret, BuildingRadar, BuildingRelay}

// Параметры турели и радара.
const (
	TurretRange = 3.5
	TurretDmg   = 6
	TurretRate  = 0.55
	// RadarVision - радиус обзора радара.
	RadarVision = 5
)
