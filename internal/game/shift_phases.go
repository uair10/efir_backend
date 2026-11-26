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
	view.Night = shift.nightsPassed
	view.NightSecondsElapsed = shift.nightElapsed.Seconds()
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
func (shift *Shift) completeBriefing(from Post) ShiftUpdate {
	if shift.phase != PhaseBriefing || shift.paused {
		return ShiftUpdate{}
	}
	if slices.Contains(shift.briefed, from) {
		return ShiftUpdate{}
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
	return update
}

// requestNight проверяет, что досрочный отбой объявляет хост в дневной фазе.
func (shift *Shift) requestNight(from Post) ShiftUpdate {
	if shift.phase != PhaseDay || shift.paused {
		return ShiftUpdate{}
	}
	if from != PostWest {
		return ShiftUpdate{Reply: []Event{FlashEvent{Text: fmt.Sprintf(
			"Ночь начинает хост, «%s». Скажите ему голосом (V).", shift.participants[PostWest].Callsign,
		)}}}
	}
	return shift.startNight(false)
}

// startNextDay открывает следующий день после сводки по команде хоста.
func (shift *Shift) startNextDay(from Post) ShiftUpdate {
	if shift.phase != PhaseReport || shift.paused {
		return ShiftUpdate{}
	}
	if from != PostWest {
		return ShiftUpdate{Reply: []Event{FlashEvent{Text: fmt.Sprintf(
			"Следующий день начинает хост, «%s».", shift.participants[PostWest].Callsign,
		)}}}
	}
	shift.nightsPassed++
	shift.phase = PhaseDay
	shift.dayRemaining = DayLen * time.Second
	shift.nightElapsed = 0
	return ShiftUpdate{SnapshotChanged: true}
}

// setPaused меняет состояние часов только ночью в одиночной смене.
func (shift *Shift) setPaused(paused bool) ShiftUpdate {
	if shift.phase != PhaseNight {
		return ShiftUpdate{}
	}
	if shift.mode != ModeSolo {
		return ShiftUpdate{Reply: []Event{FlashEvent{Text: "В смене с напарником пауза недоступна."}}}
	}
	if shift.paused == paused {
		return ShiftUpdate{}
	}
	shift.paused = paused
	return ShiftUpdate{SnapshotChanged: true}
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
	} else {
		bonus := shift.earlyBonus()
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
	shift.nightElapsed = 0
	return ShiftUpdate{SnapshotChanged: true, Events: events}
}

// finishNight начисляет энергию обоим постам до формирования сводки.
// Седьмая ночь сразу завершает смену и выдаёт только финальную сводку.
func (shift *Shift) finishNight() ShiftUpdate {
	for post := range shift.energy {
		shift.energy[post] += DawnGain
	}
	shift.phase = PhaseReport
	if shift.nightsPassed == Nights {
		shift.phase = PhaseEnd
	}

	update := ShiftUpdate{SnapshotChanged: true, PostEvents: make(map[Post][]Event)}
	for post := range shift.participants {
		if report, ok := shift.Report(post); ok {
			update.PostEvents[post] = []Event{report}
		}
	}
	return update
}

// Report возвращает сводку для данного игрока в фазе сводки или в конце смены.
// Канал также использует её при восстановлении соединения.
func (shift *Shift) Report(viewer Post) (ReportEvent, bool) {
	if p, ok := shift.participants[viewer]; !ok || p.Bot {
		return ReportEvent{}, false
	}
	switch shift.phase {
	case PhaseReport:
		next := "[ К КАРТЕ ]"
		if viewer == PostWest {
			next = fmt.Sprintf("[ ДЕНЬ %d ]", shift.nightsPassed+1)
		}
		return ReportEvent{Title: fmt.Sprintf("СВОДКА · НОЧЬ %d", shift.nightsPassed), Next: next}, true
	case PhaseEnd:
		return ReportEvent{
			Title: "КОЛОННА ВЫШЛА НА МАЯК",
			Text:  fmt.Sprintf("Семь ночей позади. Колонна видит маяк, эвакуация началась. Ядро: %d%%.", InitialCoreHP),
			Next:  "[ НОВАЯ СМЕНА ]",
			Final: true,
		}, true
	default:
		return ReportEvent{}, false
	}
}
