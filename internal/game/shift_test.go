package game

import (
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

	applyShiftCommand(t, s, PostEast, CompleteBriefing{})
	s.Tick(12 * time.Second)
	day := s.Snapshot(PostWest)
	assert.Equal(t, PhaseDay, day.Phase)
	assert.InDelta(t, FirstDaySeconds-12, day.DaySecondsRemaining, 0.001)
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

	_, err = s.Apply(PostWest, StartNight{})
	require.ErrorIs(t, err, ErrWrongPhase)
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
			_, err := s.Apply(PostEast, CompleteBriefing{})
			require.ErrorIs(t, err, ErrInvalidPlayer)
			applyShiftCommand(t, s, PostWest, CompleteBriefing{})
			day := s.Snapshot(PostWest)
			require.Equal(t, PhaseDay, day.Phase)

			if automatic {
				events := s.Tick(FirstDaySeconds * time.Second).Events
				require.NotEmpty(t, events)
				assert.Equal(t, SoundEvent{Name: "countFinal"}, events[0])
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

// applyShiftCommand применяет команду и прерывает тест при ошибке.
func applyShiftCommand(t *testing.T, s *Shift, from Post, cmd Command) ShiftUpdate {
	t.Helper()
	update, err := s.Apply(from, cmd)
	require.NoError(t, err)
	return update
}
