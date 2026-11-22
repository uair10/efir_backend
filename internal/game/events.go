package game

// RadioFrom - от кого пришла радиограмма.
type RadioFrom string

// RadioSystem is the system sender for radio messages.
const RadioSystem RadioFrom = "sys"

// SoundName - имя звука для клиента.
type SoundName string

// ReportRow - строка сводки.
type ReportRow struct {
	Label string
	Value string
	Bad   bool
}

// Event - событие смены. Закрытый тип-сумма.
//
//sumtype:decl
type Event interface {
	isEvent()
}

// RadioEvent - строка в рации.
type RadioEvent struct {
	From    RadioFrom
	Text    string
	Level   LogLevel
	Garbled bool
}

func (RadioEvent) isEvent() {}

// VoiceEvent - голосовое сообщение напарника.
type VoiceEvent struct {
	From    Post
	Seconds int
	Garbled bool
}

func (VoiceEvent) isEvent() {}

// LogEvent - запись в журнал.
type LogEvent struct {
	Night int
	Time  string
	Text  string
	Level LogLevel
}

func (LogEvent) isEvent() {}

// SoundEvent - звук; X - клетка источника (для стерео).
type SoundEvent struct {
	Name SoundName
	X    *int
	Mine bool
}

func (SoundEvent) isEvent() {}

// ShotEvent - выстрел турели.
type ShotEvent struct {
	X1   float64
	Y1   float64
	X2   float64
	Y2   float64
	Post Post
}

func (ShotEvent) isEvent() {}

// FXEvent - вспышка/взрыв.
type FXEvent struct {
	FX string // "burst" | "boom"
	X  float64
	Y  float64
}

func (FXEvent) isEvent() {}

// CoreHitEvent - попадание по ядру.
type CoreHitEvent struct{}

func (CoreHitEvent) isEvent() {}

// GateFlashEvent - вспышка на воротах.
type GateFlashEvent struct {
	Gate GateID
}

func (GateFlashEvent) isEvent() {}

// FlashEvent - ответ только этому игроку (почему команда не прошла).
type FlashEvent struct {
	Text string
}

func (FlashEvent) isEvent() {}

// ReportEvent - сводка после ночи.
type ReportEvent struct {
	Title string
	Text  string
	Rows  []ReportRow
	Next  string
	Final bool
}

func (ReportEvent) isEvent() {}
