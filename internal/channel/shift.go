package channel

import "strings"

const (
	briefingDayLength = 150
	briefingFuelStock = 24
	briefingCoreHP    = 100
	briefingMapCols   = 30
	briefingMapRows   = 20
)

// shiftPhase обозначает этап игровой смены в снимке.
type shiftPhase string

const shiftPhaseBriefing shiftPhase = "briefing"

// shiftEventKind обозначает вид события смены.
type shiftEventKind string

const shiftEventRadio shiftEventKind = "radio"

// radioSource обозначает отправителя реплики рации.
type radioSource string

const radioSourceSystem radioSource = "sys"

type shiftSnapshotMessage struct {
	Type     ServerMessageType `json:"type"`
	Snapshot briefingSnapshot  `json:"snapshot"`
}

// briefingSnapshot содержит поля GameSnapshot, которые клиент читает с начала смены.
//
//nolint:tagliatelle // имена полей заданы контрактом GameSnapshot
type briefingSnapshot struct {
	Phase        shiftPhase     `json:"phase"`        // Текущий этап смены.
	Night        int            `json:"night"`        // Номер текущей ночи, начиная с 1.
	Paused       bool           `json:"paused"`       // Приостановлена ли смена.
	CanPause     bool           `json:"canPause"`     // Может ли игрок сам поставить смену на паузу.
	DayLeft      int            `json:"dayLeft"`      // Секунд до наступления ночи.
	EarlyBonus   int            `json:"earlyBonus"`   // Энергия за досрочный запуск ночи.
	NightTime    int            `json:"nightTime"`    // Секунд с начала текущей ночи.
	Fast         bool           `json:"fast"`         // Ускорено ли время ночи после уничтожения мутантов.
	Energy       int            `json:"energy"`       // Энергия получателя снимка.
	MateEnergy   int            `json:"mateEnergy"`   // Энергия напарника.
	Fuel         int            `json:"fuel"`         // Общий запас канистр в бункере.
	Tank         float64        `json:"tank"`         // Канистры в баке дизеля получателя, включая дробный остаток.
	MateTank     float64        `json:"mateTank"`     // Канистры в баке дизеля напарника, включая дробный остаток.
	Core         int            `json:"core"`         // Прочность ядра.
	Card         any            `json:"card"`         // Просьба бункера на текущий день, если она есть.
	Shaft        any            `json:"shaft"`        // Состояние шахты лаборатории и голосования по её защите.
	OpenGate     *string        `json:"openGate"`     // Ворота, открытые для беженцев до утра, если они есть.
	Noise        bool           `json:"noise"`        // Помехи Фона в голосовой связи и рации.
	Jam          bool           `json:"jam"`          // Заглушён ли канал данных напарника.
	CoreJammed   bool           `json:"coreJammed"`   // Заглушён ли радар ядра.
	Ready        []Post         `json:"ready"`        // Посты игроков, подтвердивших готовность к следующему этапу.
	Vision       string         `json:"vision"`       // Обзор клеток по строкам: 0 — скрыта, 1 — свой радар, 2 — радар напарника.
	Buildings    []any          `json:"buildings"`    // Постройки обоих постов.
	Enemies      []any          `json:"enemies"`      // Мутанты, видимые получателю.
	Barricades   []any          `json:"barricades"`   // Баррикады у ворот.
	Intel        map[string]any `json:"intel"`        // Результаты разведки по воротам, доступные получателю.
	Drones       []any          `json:"drones"`       // Разведывательные дроны на карте.
	Pings        []any          `json:"pings"`        // Метки на клетках карты.
	SquarePings  []any          `json:"squarePings"`  // Метки на квадратах карты.
	Requests     []any          `json:"requests"`     // Запросы бота на энергию, адресованные получателю.
	ScoutedToday bool           `json:"scoutedToday"` // Запускал ли получатель разведку сегодня.
}

type shiftEventsMessage struct {
	Type   ServerMessageType `json:"type"`
	Events []shiftRadioEvent `json:"events"`
}

type shiftRadioEvent struct {
	Kind shiftEventKind `json:"kind"`
	From radioSource    `json:"from"`
	Text string         `json:"text"`
}

// sendShiftSnapshot передаёт начальное состояние предыстории в формате
// GameSnapshot. Симуляция, которая будет обновлять снимок, появится в EFR-6–8.
func (r *room) sendShiftSnapshot(conn *Connection) {
	energy, mateEnergy := 70, 40
	if conn.post == PostEast {
		energy, mateEnergy = mateEnergy, energy
	}
	conn.sock.Send(Message{Payload: shiftSnapshotMessage{
		Type: ServerMessageGame,
		Snapshot: briefingSnapshot{
			Phase:       shiftPhaseBriefing,
			Night:       1,
			Paused:      r.shiftPaused,
			CanPause:    r.mode == GameModeSolo,
			DayLeft:     briefingDayLength,
			Energy:      energy,
			MateEnergy:  mateEnergy,
			Fuel:        briefingFuelStock,
			Core:        briefingCoreHP,
			Ready:       []Post{},
			Vision:      strings.Repeat("0", briefingMapCols*briefingMapRows),
			Buildings:   []any{},
			Enemies:     []any{},
			Barricades:  []any{},
			Intel:       map[string]any{},
			Drones:      []any{},
			Pings:       []any{},
			SquarePings: []any{},
			Requests:    []any{},
		},
	}})
}
