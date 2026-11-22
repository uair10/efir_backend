package game

import (
	"fmt"
	"slices"
	"time"
)

// Snapshot возвращает снимок начавшейся смены для данного поста.
func (shift *Shift) Snapshot(viewer Post) Snapshot {
	view := BriefingSnapshot(viewer, shift.mode, shift.paused)
	view.Phase = shift.phase
	view.DaySecondsRemaining = shift.dayRemaining.Seconds()
	view.Energy = shift.energy[viewer]
	view.MateEnergy = shift.energy[OtherPost(viewer)]
	view.Ready = slices.Clone(shift.briefed)
	if shift.phase == PhaseDay {
		view.EarlyBonus = shift.earlyBonus()
	}
	return view
}

// completeBriefing отмечает инструктаж на посту и открывает день после подтверждения всеми игроками.
func (shift *Shift) completeBriefing(from Post) (ShiftUpdate, error) {
	if shift.phase != PhaseBriefing || shift.paused {
		return ShiftUpdate{}, ErrWrongPhase
	}
	if slices.Contains(shift.briefed, from) {
		return ShiftUpdate{}, nil
	}
	shift.briefed = append(shift.briefed, from)
	// В одиночной смене достаточно хоста. В кооперативе ждём обоих людей,
	// включая напарника, который временно потерял связь.
	update := ShiftUpdate{SnapshotChanged: true}
	if len(shift.briefed) == 2 || shift.mode == ModeSolo {
		shift.phase = PhaseDay
		shift.briefed = nil
		update.Events = []Event{RadioEvent{From: RadioSystem, Text: "Терминал ПТ-7 запущен. Связь на канале установлена."}}
	}
	return update, nil
}

// earlyBonus возвращает бонус энергии за полные интервалы оставшегося дневного времени.
func (shift *Shift) earlyBonus() int {
	return int(shift.dayRemaining / (EarlyRate * time.Second))
}

// startNight обслуживает оба способа перехода в ночь. Бонус вычисляется
// до сброса таймера и начисляется только игрокам-людям.
func (shift *Shift) startNight(automatic bool) ShiftUpdate {
	var events []Event
	if automatic {
		events = []Event{
			SoundEvent{Name: "countFinal"},
			RadioEvent{From: RadioSystem, Text: "Время вышло. Бункер переведён в ночной режим."},
		}
	} else if bonus := shift.earlyBonus(); bonus > 0 {
		for post, p := range shift.participants {
			if !p.Bot {
				shift.energy[post] += bonus
			}
		}
		events = []Event{RadioEvent{From: RadioSystem, Text: fmt.Sprintf(
			"Ночь раньше срока: некритичные системы бункера отключены досрочно, в ядро ушло +%d ⚡.", bonus,
		)}}
	}
	shift.phase = PhaseNight
	shift.dayRemaining = 0
	return ShiftUpdate{SnapshotChanged: true, Events: events}
}
