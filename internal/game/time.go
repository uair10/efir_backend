package game

import (
	"fmt"
	"math"
)

// Длительности фаз и параметры игрового времени.
const (
	Nights = 7
	// NightSeconds - ночь идёт по игровым часам 22:00–05:00 за NightSeconds секунд симуляции.
	NightSeconds = 140
	// NightFrom - начало ночи в игровых минутах (22:00).
	NightFrom = 22 * 60
	// NightMinutes - длительность ночи в игровых минутах (7 часов).
	NightMinutes = 7 * 60
	// CalmSpeed - когда мутантов нет, время ночи идёт быстрее.
	CalmSpeed = 4
	// DayLen - длительность последующих дней в секундах.
	DayLen = 75
	// FirstDaySeconds - первый день длиннее, чтобы успеть прочитать правила.
	FirstDaySeconds = 150
	// EarlyRate - ранний старт ночи: +1 ⚡ за каждые EarlyRate секунд до таймера.
	EarlyRate = 5
	// Countdown - последние секунды дня отсчитываются писком.
	Countdown = 5
)

// GameTime - игровое время ночи: "23:40".
//
//nolint:revive // имя помощника задано в EFR-6
func GameTime(nightT float64) string {
	const (
		hoursPerDay    = 24
		minutesPerHour = 60
	)
	m := NightFrom + math.Min(1, nightT/NightSeconds)*NightMinutes
	return fmt.Sprintf("%02d:%02d", int(math.Floor(m/minutesPerHour))%hoursPerDay, int(math.Floor(math.Mod(m, minutesPerHour))))
}
