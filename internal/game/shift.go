package game

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

const lobbyCountdown = 3 * time.Second

// Ошибки, связанные со сменой.
var (
	ErrPostTaken          = errors.New("channel is full")
	ErrCallsignTaken      = errors.New("callsign taken")
	ErrCallsignLocked     = errors.New("callsign locked")
	ErrPartnerMissing     = errors.New("partner missing")
	ErrInvalidCallsign    = errors.New("invalid callsign")
	ErrWrongPhase         = errors.New("command not allowed in this phase")
	ErrInvalidPlayer      = errors.New("post has no human player")
	ErrUnsupportedCommand = errors.New("command is not implemented")
)

// Participant - состояние игрока на посту. Shift возвращает его копию.
type Participant struct {
	Callsign  Callsign
	Ready     bool
	Bot       bool
	Connected bool
}

// ShiftUpdate описывает изменения, которые нужно показать игрокам.
// Reply предназначен отправителю команды, Events - всем игрокам,
// PostEvents - только указанным постам.
type ShiftUpdate struct {
	LobbyChanged    bool
	SnapshotChanged bool
	Events          []Event
	Reply           []Event
	PostEvents      map[Post][]Event
}

// Shift хранит правила и состояние одного экземпляра игры с момента создания лобби.
// Владелец вызывает методы последовательно и передаёт прошедшее время в Tick.
type Shift struct {
	mode         Mode
	participants map[Post]Participant

	phase        Phase
	countdown    time.Duration
	briefed      []Post
	dayRemaining time.Duration
	nightElapsed time.Duration
	paused       bool
	energy       map[Post]int
	nightsPassed int
}

// NewShift занимает западный пост хостом, а в одиночной смене восточный пост ботом.
func NewShift(mode Mode) *Shift {
	s := &Shift{
		mode:         mode,
		phase:        PhaseLobby,
		participants: map[Post]Participant{PostWest: {Callsign: CallsignFirst}},
	}
	if mode == ModeSolo {
		s.participants[PostEast] = Participant{Callsign: CallsignSecond, Bot: true, Ready: true, Connected: true}
	}
	return s
}

// Mode возвращает режим смены.
func (shift *Shift) Mode() Mode { return shift.mode }

// Phase возвращает текущий этап смены.
func (shift *Shift) Phase() Phase { return shift.phase }

// Started сообщает, что смена дошла до инструктажа или более поздней фазы.
func (shift *Shift) Started() bool {
	switch shift.phase {
	case PhaseBriefing, PhaseDay, PhaseNight, PhaseReport, PhaseEnd:
		return true
	default:
		return false
	}
}

// Participant возвращает состояние поста и признак того, что он занят.
func (shift *Shift) Participant(post Post) (Participant, bool) {
	p, ok := shift.participants[post]
	return p, ok
}

// JoinGuest занимает восточный пост со свободным позывным.
func (shift *Shift) JoinGuest() (Callsign, error) {
	if _, occupied := shift.participants[PostEast]; occupied {
		return "", ErrPostTaken
	}
	cs, ok := firstFreeCallsign(shift.participants[PostWest].Callsign)
	if !ok {
		return "", ErrPostTaken
	}
	shift.participants[PostEast] = Participant{Callsign: cs}
	return cs, nil
}

// SetConnected обновляет доступность поста для отсчёта перед стартом.
func (shift *Shift) SetConnected(post Post, connected bool) {
	if p, ok := shift.participants[post]; ok && !p.Bot {
		p.Connected = connected
		shift.participants[post] = p
		shift.reconcileLobby()
	}
}

// Leave освобождает пост при штатном уходе игрока.
func (shift *Shift) Leave(post Post) {
	if p, ok := shift.participants[post]; ok && !p.Bot {
		delete(shift.participants, post)
		shift.briefed = slices.DeleteFunc(shift.briefed, func(p Post) bool { return p == post })
		shift.reconcileLobby()
	}
}

// ReconnectExpired освобождает пост в лобби или ставит начавшуюся смену на паузу.
func (shift *Shift) ReconnectExpired(post Post) ShiftUpdate {
	if !shift.Started() {
		shift.Leave(post)
		return ShiftUpdate{LobbyChanged: true}
	}
	shift.paused = true
	return ShiftUpdate{
		SnapshotChanged: true,
		Events:          []Event{RadioEvent{From: RadioSystem, Text: "Напарник пропал из эфира"}},
	}
}

// Apply проверяет команду от человека на посту и применяет правила текущей фазы.
func (shift *Shift) Apply(from Post, cmd Command) (ShiftUpdate, error) {
	if shift.phase == PhaseEnd {
		return ShiftUpdate{}, nil
	}
	p, ok := shift.participants[from]
	if p.Bot {
		return ShiftUpdate{}, nil
	}
	if !ok {
		return ShiftUpdate{}, ErrInvalidPlayer
	}
	switch cmd := cmd.(type) {
	case SetCallsign:
		return shift.setCallsign(from, p, cmd.Callsign)
	case SetReady:
		return shift.setReady(from, p, cmd.Ready)
	case CompleteBriefing:
		return shift.completeBriefing(from), nil
	case StartNight:
		return shift.requestNight(from), nil
	case StartNextDay:
		return shift.startNextDay(from), nil
	case SetPaused:
		return shift.setPaused(cmd.Paused), nil
	case Build, Demolish, PlaceBarricade, RemoveBarricade, ScoutGate, PlaceMapMarker,
		RequestSupport, FulfillEnergyRequest, RefuelDiesel, VoteOnRequest, Voice:
		return ShiftUpdate{}, ErrUnsupportedCommand
	}
	return ShiftUpdate{}, ErrUnsupportedCommand
}

// Tick продвигает таймер текущей фазы. Переход открывает следующую фазу
// с её собственным таймером. Во время паузы игровое время стоит.
func (shift *Shift) Tick(dt time.Duration) ShiftUpdate {
	if dt <= 0 || shift.paused {
		return ShiftUpdate{}
	}
	switch shift.phase {
	case PhaseStarting:
		before := shift.CountdownSeconds()
		shift.countdown = max(0, shift.countdown-dt)
		if shift.countdown == 0 {
			shift.beginBriefing()
			return ShiftUpdate{LobbyChanged: true, SnapshotChanged: true}
		}
		return ShiftUpdate{LobbyChanged: shift.CountdownSeconds() != before}
	case PhaseDay:
		shift.dayRemaining = max(0, shift.dayRemaining-dt)
		if shift.dayRemaining == 0 {
			return shift.startNight(true)
		}
		return ShiftUpdate{SnapshotChanged: true}
	case PhaseNight:
		const nightDuration = NightSeconds * time.Second
		// Сравниваем с остатком до сложения, чтобы такт любой длины
		// заканчивал ночь ровно в 05:00.
		shift.nightElapsed += min(dt, nightDuration-shift.nightElapsed)
		if shift.nightElapsed == nightDuration {
			return shift.finishNight()
		}
		return ShiftUpdate{SnapshotChanged: true}
	default:
		return ShiftUpdate{}
	}
}

// CountdownSeconds округляет оставшееся время до старта вверх до целой секунды.
func (shift *Shift) CountdownSeconds() int {
	return int((shift.countdown + time.Second - 1) / time.Second)
}

// setCallsign разрешает выбрать свободный позывной до подтверждения готовности и запуска смены.
func (shift *Shift) setCallsign(from Post, p Participant, callsign Callsign) (ShiftUpdate, error) {
	if shift.Started() {
		return ShiftUpdate{}, ErrWrongPhase
	}
	if _, ok := ParseCallsign(callsign.String()); !ok {
		return ShiftUpdate{}, fmt.Errorf("%w: %q", ErrInvalidCallsign, callsign)
	}
	if p.Ready {
		return ShiftUpdate{}, ErrCallsignLocked
	}
	if shift.participants[OtherPost(from)].Callsign == callsign {
		return ShiftUpdate{}, ErrCallsignTaken
	}
	p.Callsign = callsign
	shift.participants[from] = p
	return ShiftUpdate{LobbyChanged: true}, nil
}

// setReady меняет готовность игрока и пересчитывает условия запуска смены.
func (shift *Shift) setReady(from Post, p Participant, ready bool) (ShiftUpdate, error) {
	if shift.Started() {
		return ShiftUpdate{}, ErrWrongPhase
	}
	if _, ok := shift.participants[OtherPost(from)]; ready && !ok {
		return ShiftUpdate{}, ErrPartnerMissing
	}
	p.Ready = ready
	shift.participants[from] = p
	shift.reconcileLobby()
	return ShiftUpdate{LobbyChanged: true, SnapshotChanged: shift.Started()}, nil
}

// reconcileLobby запускает отсчёт при готовности обоих постов и сбрасывает
// его, если кто-то снял готовность или потерял связь. Бот всегда готов и на связи.
func (shift *Shift) reconcileLobby() {
	if shift.Started() {
		return
	}
	west, east := shift.participants[PostWest], shift.participants[PostEast]
	if !west.Ready || !east.Ready || !west.Connected || !east.Connected {
		shift.phase = PhaseLobby
		shift.countdown = 0
		return
	}
	if shift.mode == ModeSolo {
		shift.beginBriefing()
	} else if shift.phase == PhaseLobby {
		shift.phase = PhaseStarting
		shift.countdown = lobbyCountdown
	}
}

// beginBriefing завершает отсчёт и задаёт начальную энергию постов и запас времени на первый день.
func (shift *Shift) beginBriefing() {
	shift.countdown = 0
	shift.phase = PhaseBriefing
	shift.nightsPassed = 1
	shift.dayRemaining = FirstDaySeconds * time.Second
	shift.energy = map[Post]int{PostWest: StartEnergy[PostWest], PostEast: StartEnergy[PostEast]}
}
