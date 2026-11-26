package game

import "strings"

// Snapshot - состояние смены глазами одного игрока.
// Содержит данные, доступные этому посту.
type Snapshot struct {
	Phase               Phase      // Текущий этап смены.
	Night               int        // Номер текущей ночи, начиная с 1.
	Paused              bool       // Приостановлена ли смена.
	CanPause            bool       // Можно ли ставить паузу (только solo).
	DaySecondsRemaining float64    // Секунд до ночи.
	EarlyBonus          int        // ⚡ за ранний старт прямо сейчас.
	NightSecondsElapsed float64    // Секунд ночи прошло, 0..NightSeconds.
	NightAccelerated    bool       // Время ночи сейчас ускорено.
	Energy              int        // Энергия получателя.
	MateEnergy          int        // Энергия напарника.
	FuelStock           int        // Канистры на складе бункера (общий запас).
	Tank                float64    // Канистры в баке своего дизеля (дробно).
	MateTank            float64    // Канистры в баке дизеля напарника.
	CoreHP              float64    // Прочность ядра.
	Card                *CardView  // Просьба бункера (день); nil - нет.
	Shaft               *ShaftView // Шахта лаборатории; nil - нет.
	OpenGate            *GateID    // Ворота для беженцев до утра; nil - нет.
	Noise               bool       // Помехи Фона в рации.
	Jam                 bool       // Канал данных заглушён.
	CoreJammed          bool       // Радар ядра ослеплён ревуном.
	Ready               []Post     // Посты, подтвердившие инструктаж.
	Vision              string     // Cols*Rows клеток по строкам: 0 — скрыта, 1 — свой радар, 2 — радар напарника.
	Buildings           []BuildingView
	Enemies             []EnemyView // Только видимые этому игроку.
	Barricades          []BarricadeView
	GateIntel           map[GateID]GateIntel // Только разведанные ворота.
	Drones              []DroneView
	Pings               []PingView
	SquarePings         []SquarePingView
	EnergyRequests      []EnergyRequest // Запросы бота этому игроку.
	ScoutedToday        bool
}

// BuildingView - постройка на карте, как её видит игрок.
type BuildingView struct {
	Type   BuildingType
	X      int
	Y      int
	Post   Post
	HP     float64
	MaxHP  float64
	Angle  float64 // Куда смотрит ствол турели, радианы.
	Jammed bool    // Ретранслятор/радар заглушён ревуном.
	Hit    bool    // По постройке только что попали - мигает.
	Kills  int     // Сколько убила турель.
}

// EnemyView - мутант в обзоре игрока.
type EnemyView struct {
	ID    int
	Type  EnemyType
	X     float64
	Y     float64
	HP    float64
	MaxHP float64
	Dir   float64 // Направление движения, радианы.
	Hit   bool    // Только что попали - мигает.
	Gate  GateID  // Из каких ворот пришёл.
}

// BarricadeView - баррикада у ворот.
type BarricadeView struct {
	Gate  GateID
	Owner Post // Кто поставил; ворота могут быть на чужой стороне.
	HP    float64
	MaxHP float64
	Hit   bool
}

// GateIntel - что известно о воротах после разведки.
// При потере дрона оценка и направление удара неизвестны.
type GateIntel struct {
	Lost     bool
	Estimate int  // Оценка числа отметок с погрешностью дрона.
	Main     bool // Основной удар идёт через эти ворота.
}

// DroneView - разведывательный дрон на карте.
type DroneView struct {
	Gate     GateID
	By       Post
	Progress float64 // 0..1: достигает ворот на 0.5, возвращается на 1.
}

// PingView - метка на клетке карты.
type PingView struct {
	X    float64
	Y    float64
	From Post
	Left float64 // 0..1: сколько осталось жить метке.
}

// SquarePingView - метка на квадрате карты.
type SquarePingView struct {
	Square string
	From   Post
}

// ShaftView - шахта лаборатории: клетка, которую просят прикрыть.
type ShaftView struct {
	X        int
	Y        int
	Pending  bool // Ещё голосуют.
	Breached bool // Пробита.
}

// CardView - просьба бункера на сегодня.
type CardView struct {
	From     string // «Лаборатория», «Совет бункера»…
	Title    string
	Text     string  // Лор и намёк на последствия.
	Price    *string // Цена словами; nil - платить нечем, но последствия есть.
	Vote     *bool   // Свой голос; nil - не голосовал.
	MateVote *bool   // Голос напарника; nil - не голосовал.
	Result   *string // "yes"/"no"/nil - ещё голосуют.
	Note     *string // Почему не принято, хотя оба «за».
}

// EnergyRequest - просьба бота-напарника подкинуть энергии.
type EnergyRequest struct {
	ID       int
	Building BuildingType
	Cost     int
}

// BriefingSnapshot возвращает снапшот игры на этапе брифинга.
func BriefingSnapshot(viewer Post, mode Mode, paused bool) Snapshot {
	return Snapshot{
		Phase:               PhaseBriefing,
		Night:               1,
		Paused:              paused,
		CanPause:            mode == ModeSolo,
		DaySecondsRemaining: FirstDaySeconds,
		Energy:              StartEnergy[viewer],
		MateEnergy:          StartEnergy[OtherPost(viewer)],
		FuelStock:           FuelStock,
		CoreHP:              InitialCoreHP,
		Vision:              strings.Repeat("0", Cols*Rows),
	}
}
