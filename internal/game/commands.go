package game

// Command - команда клиента во время смены.
// Закрытый тип-сумма: каждая реализация - конкретная команда.
//
//sumtype:decl
type Command interface {
	isCommand()
}

// CompleteBriefing - игрок подтвердил инструктаж.
type CompleteBriefing struct{}

func (CompleteBriefing) isCommand() {}

// Build - построить здание.
type Build struct {
	Building BuildingType
	X        int
	Y        int
}

func (Build) isCommand() {}

// Demolish - снести постройку.
type Demolish struct {
	X int
	Y int
}

func (Demolish) isCommand() {}

// PlaceBarricade - поставить баррикаду на воротах.
type PlaceBarricade struct {
	Gate GateID
}

func (PlaceBarricade) isCommand() {}

// RemoveBarricade - снять баррикаду.
type RemoveBarricade struct {
	Gate GateID
}

func (RemoveBarricade) isCommand() {}

// ScoutGate - запустить дрон к воротам.
type ScoutGate struct {
	Gate GateID
}

func (ScoutGate) isCommand() {}

// PlaceMapMarker - метка на клетке.
type PlaceMapMarker struct {
	X float64
	Y float64
}

func (PlaceMapMarker) isCommand() {}

// RequestSupport - просьба о помощи по квадрату.
type RequestSupport struct {
	Square string
}

func (RequestSupport) isCommand() {}

// FulfillEnergyRequest - ответ на просьбу бота по идентификатору запроса.
type FulfillEnergyRequest struct {
	RequestID int
}

func (FulfillEnergyRequest) isCommand() {}

// RefuelDiesel - залить канистру со склада в свой дизель (день).
type RefuelDiesel struct{}

func (RefuelDiesel) isCommand() {}

// VoteOnRequest - голос по просьбе бункера (день); переголосовать нельзя.
type VoteOnRequest struct {
	Yes bool
}

func (VoteOnRequest) isCommand() {}

// StartNight - хозяин досрочно начинает ночь.
type StartNight struct{}

func (StartNight) isCommand() {}

// SetPaused - пауза (только solo).
type SetPaused struct {
	Paused bool
}

func (SetPaused) isCommand() {}

// StartNextDay - хозяин начинает следующий день после сводки.
type StartNextDay struct{}

func (StartNextDay) isCommand() {}

// Voice - голосовое сообщение ушло в эфир (секунды).
type Voice struct {
	Seconds int
}

func (Voice) isCommand() {}
