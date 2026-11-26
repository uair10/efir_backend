package game

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShift_CoopBriefingAndEarlyNight проверяет инструктаж обоих игроков и досрочный старт ночи хостом.
func TestShift_CoopBriefingAndEarlyNight(t *testing.T) {
	t.Parallel()

	s := NewShift(ModeCoop)
	_, err := s.JoinGuest()
	require.NoError(t, err)
	s.SetConnected(PostWest, true)
	s.SetConnected(PostEast, true)
	applyShiftCommand(t, s, PostWest, SetCallsign{Callsign: "Сокол"})
	applyShiftCommand(t, s, PostWest, SetReady{Ready: true})
	applyShiftCommand(t, s, PostEast, SetReady{Ready: true})
	s.Tick(lobbyCountdown)

	applyShiftCommand(t, s, PostWest, CompleteBriefing{})
	assert.Empty(t, applyShiftCommand(t, s, PostWest, CompleteBriefing{}))
	view := s.Snapshot(PostEast)
	assert.Equal(t, PhaseBriefing, view.Phase)
	assert.Equal(t, []Post{PostWest}, view.Ready)

	assert.Empty(t, s.Tick(time.Hour))
	assert.Equal(t, view, s.Snapshot(PostEast))
	applyShiftCommand(t, s, PostEast, CompleteBriefing{})
	s.Tick(12*time.Second + 500*time.Millisecond)
	day := s.Snapshot(PostWest)
	assert.Equal(t, PhaseDay, day.Phase)
	assert.InDelta(t, FirstDaySeconds-12.5, day.DaySecondsRemaining, 0.001)
	assert.Equal(t, 27, day.EarlyBonus)

	guest := applyShiftCommand(t, s, PostEast, StartNight{})
	assert.Empty(t, guest.Events)
	assert.Equal(t, []Event{FlashEvent{Text: "Ночь начинает хост, «Сокол». Скажите ему голосом (V)."}}, guest.Reply)
	assert.Equal(t, day, s.Snapshot(PostWest))

	require.True(t, applyShiftCommand(t, s, PostWest, StartNight{}).SnapshotChanged)
	night := s.Snapshot(PostWest)
	assert.Equal(t, PhaseNight, night.Phase)
	assert.Zero(t, night.DaySecondsRemaining)
	assert.Equal(t, day.Energy+27, night.Energy)
	assert.Equal(t, day.MateEnergy+27, night.MateEnergy)

	assert.Empty(t, applyShiftCommand(t, s, PostWest, StartNight{}))
	assert.Equal(t, night, s.Snapshot(PostWest))
}

// TestShift_SoloManualAndAutomaticNight проверяет ручной старт ночи и переход по таймеру в одиночной смене.
func TestShift_SoloManualAndAutomaticNight(t *testing.T) {
	t.Parallel()

	for name, automatic := range map[string]bool{"manual": false, "automatic": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := NewShift(ModeSolo)
			s.SetConnected(PostWest, true)
			applyShiftCommand(t, s, PostWest, SetReady{Ready: true})
			assert.Empty(t, applyShiftCommand(t, s, PostEast, CompleteBriefing{}))
			require.Equal(t, PhaseBriefing, s.Phase())
			applyShiftCommand(t, s, PostWest, CompleteBriefing{})
			day := s.Snapshot(PostWest)
			require.Equal(t, PhaseDay, day.Phase)

			if automatic {
				s.Tick(FirstDaySeconds*time.Second - time.Millisecond)
				require.Equal(t, PhaseDay, s.Phase())
				events := s.Tick(time.Millisecond).Events
				require.Len(t, events, 2)
				assert.Equal(t, SoundEvent{Name: "countFinal"}, events[0])
				assert.Equal(t, RadioEvent{From: RadioSystem, Text: "Время вышло. Бункер переведён в ночной режим."}, events[1])
			} else {
				applyShiftCommand(t, s, PostWest, StartNight{})
			}
			night := s.Snapshot(PostWest)
			assert.Equal(t, PhaseNight, night.Phase)
			assert.Equal(t, day.MateEnergy, night.MateEnergy)
			if automatic {
				assert.Equal(t, day.Energy, night.Energy)
			} else {
				assert.Equal(t, day.Energy+day.EarlyBonus, night.Energy)
			}
		})
	}
}

// TestShift_DawnAndVictory проводит смену через все семь ночей и проверяет
// таймеры, начисление энергии обоим постам и единственную финальную сводку.
func TestShift_DawnAndVictory(t *testing.T) {
	t.Parallel()

	s := NewShift(ModeSolo)
	s.SetConnected(PostWest, true)
	applyShiftCommand(t, s, PostWest, SetReady{Ready: true})
	applyShiftCommand(t, s, PostWest, CompleteBriefing{})

	for nightNumber := 1; nightNumber <= Nights; nightNumber++ {
		day := s.Snapshot(PostWest)
		require.Equal(t, PhaseDay, day.Phase)
		assert.Equal(t, nightNumber, day.Night)
		assert.Zero(t, day.NightSecondsElapsed)
		dayLength := DayLen
		if nightNumber == 1 {
			dayLength = FirstDaySeconds
		}
		assert.InDelta(t, dayLength, day.DaySecondsRemaining, 0.001)

		s.Tick(time.Duration(dayLength) * time.Second)
		night := s.Snapshot(PostWest)
		require.Equal(t, PhaseNight, night.Phase)
		assert.Equal(t, "22:00", GameTime(night.NightSecondsElapsed))
		assert.Zero(t, night.DaySecondsRemaining)

		s.Tick(40 * time.Second)
		assert.Equal(t, "00:00", GameTime(s.Snapshot(PostWest).NightSecondsElapsed))
		update := s.Tick((NightSeconds-40)*time.Second - 250*time.Millisecond)
		assert.True(t, update.SnapshotChanged)
		assert.False(t, update.LobbyChanged)
		assert.Empty(t, update.PostEvents)
		require.Equal(t, PhaseNight, s.Phase())
		assert.InDelta(t, NightSeconds-0.25, s.Snapshot(PostWest).NightSecondsElapsed, 0.001)

		dawn := s.Tick(time.Second)
		require.True(t, dawn.SnapshotChanged)
		assert.Empty(t, dawn.Events)
		report := s.Snapshot(PostWest)
		assert.Equal(t, nightNumber, report.Night)
		assert.Equal(t, "05:00", GameTime(report.NightSecondsElapsed))
		assert.Equal(t, night.Energy+DawnGain, report.Energy)
		assert.Equal(t, night.MateEnergy+DawnGain, report.MateEnergy)
		assert.Empty(t, s.Tick(time.Hour))
		require.Len(t, dawn.PostEvents, 1)
		require.Len(t, dawn.PostEvents[PostWest], 1)
		event, ok := dawn.PostEvents[PostWest][0].(ReportEvent)
		require.True(t, ok)

		if nightNumber == Nights {
			assert.Equal(t, PhaseEnd, report.Phase)
			assert.True(t, event.Final)
			assert.Equal(t, "КОЛОННА ВЫШЛА НА МАЯК", event.Title)
			assert.Equal(t, "[ НОВАЯ СМЕНА ]", event.Next)
			assert.Empty(t, applyShiftCommand(t, s, PostWest, StartNextDay{}))
			assert.Empty(t, applyShiftCommand(t, s, PostWest, SetPaused{Paused: true}))
			assert.Equal(t, report, s.Snapshot(PostWest))
			break
		}
		assert.Equal(t, PhaseReport, report.Phase)
		assert.False(t, event.Final)
		assert.Equal(t, fmt.Sprintf("СВОДКА · НОЧЬ %d", nightNumber), event.Title)
		assert.Equal(t, fmt.Sprintf("[ ДЕНЬ %d ]", nightNumber+1), event.Next)
		assert.Empty(t, event.Rows)
		assert.True(t, applyShiftCommand(t, s, PostWest, StartNextDay{}).SnapshotChanged)
	}
}

// TestShift_SoloPause проверяет, что пауза замораживает часы, а снятие
// паузы продолжает ночь с того же момента.
func TestShift_SoloPause(t *testing.T) {
	t.Parallel()

	s := NewShift(ModeSolo)
	s.SetConnected(PostWest, true)
	applyShiftCommand(t, s, PostWest, SetReady{Ready: true})
	applyShiftCommand(t, s, PostWest, CompleteBriefing{})
	assert.Empty(t, applyShiftCommand(t, s, PostWest, SetPaused{Paused: true}))
	assert.Empty(t, applyShiftCommand(t, s, PostWest, StartNextDay{}))
	applyShiftCommand(t, s, PostWest, StartNight{})

	s.Tick(250 * time.Millisecond)
	assert.True(t, applyShiftCommand(t, s, PostWest, SetPaused{Paused: true}).SnapshotChanged)
	paused := s.Snapshot(PostWest)
	assert.True(t, paused.CanPause)
	assert.True(t, paused.Paused)
	assert.InDelta(t, 0.25, paused.NightSecondsElapsed, 0.001)
	assert.Empty(t, applyShiftCommand(t, s, PostWest, SetPaused{Paused: true}))
	assert.Empty(t, s.Tick(time.Hour))
	assert.Equal(t, paused, s.Snapshot(PostWest))

	assert.True(t, applyShiftCommand(t, s, PostWest, SetPaused{Paused: false}).SnapshotChanged)
	assert.Empty(t, s.Tick(-time.Second))
	s.Tick(250 * time.Millisecond)
	resumed := s.Snapshot(PostWest)
	assert.False(t, resumed.Paused)
	assert.InDelta(t, 0.5, resumed.NightSecondsElapsed, 0.001)
}

// applyShiftCommand применяет команду и прерывает тест при ошибке.
func applyShiftCommand(t *testing.T, s *Shift, from Post, cmd Command) ShiftUpdate {
	t.Helper()
	update, err := s.Apply(from, cmd)
	require.NoError(t, err)
	return update
}
