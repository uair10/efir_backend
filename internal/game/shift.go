package game

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

const lobbyCountdown = 3 * time.Second

// LobbyPhase определяет этап подготовки игроков к смене.
type LobbyPhase string

// Этапы подготовки до инструктажа и после запуска игры.
const (
	LobbyPhaseLobby    LobbyPhase = "lobby"
	LobbyPhaseStarting LobbyPhase = "starting"
	LobbyPhaseShift    LobbyPhase = "shift"
)

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
// Reply предназначен только отправителю команды, Events доступны обоим постам.
type ShiftUpdate struct {
	LobbyChanged    bool
	SnapshotChanged bool
	Events          []Event
	Reply           []Event
}

// Shift хранит правила и состояние одного экземпляра игры с момента создания лобби.
// Владелец вызывает методы последовательно и передаёт прошедшее время в Tick.
type Shift struct {
	mode         Mode
	participants map[Post]Participant
	lobbyPhase   LobbyPhase
	countdown    time.Duration

	phase        Phase
	briefed      []Post
	dayRemaining time.Duration
	paused       bool
	energy       map[Post]int
}

// NewShift занимает западный пост хостом, а в одиночной смене восточный пост ботом.
func NewShift(mode Mode) *Shift {
	s := &Shift{
		mode:         mode,
		lobbyPhase:   LobbyPhaseLobby,
		participants: map[Post]Participant{PostWest: {Callsign: CallsignFirst}},
	}
	if mode == ModeSolo {
		s.participants[PostEast] = Participant{Callsign: CallsignSecond, Bot: true, Ready: true, Connected: true}
	}
	return s
}

// Mode возвращает режим смены.
func (shift *Shift) Mode() Mode { return shift.mode }

// LobbyPhase возвращает этап подготовки. После запуска сохраняется LobbyPhaseShift.
func (shift *Shift) LobbyPhase() LobbyPhase { return shift.lobbyPhase }

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
	if shift.lobbyPhase != LobbyPhaseShift {
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
	p, ok := shift.participants[from]
	if !ok || p.Bot {
		return ShiftUpdate{}, ErrInvalidPlayer
	}
	switch cmd := cmd.(type) {
	case SetCallsign:
		return shift.setCallsign(from, p, cmd.Callsign)
	case SetReady:
		return shift.setReady(from, p, cmd.Ready)
	case CompleteBriefing:
		return shift.completeBriefing(from)
	case StartNight:
		if shift.phase != PhaseDay || shift.paused {
			return ShiftUpdate{}, ErrWrongPhase
		}
		if from != PostWest {
			return ShiftUpdate{Reply: []Event{FlashEvent{Text: fmt.Sprintf(
				"Ночь начинает хост, «%s». Скажите ему голосом (V).", shift.participants[PostWest].Callsign,
			)}}}, nil
		}
		return shift.startNight(false), nil
	case Build, Demolish, PlaceBarricade, RemoveBarricade, ScoutGate, PlaceMapMarker,
		RequestSupport, FulfillEnergyRequest, RefuelDiesel, VoteOnRequest, SetPaused, StartNextDay, Voice:
		return ShiftUpdate{}, ErrUnsupportedCommand
	}
	return ShiftUpdate{}, ErrUnsupportedCommand
}

// Tick двигает отсчёт лобби и дневной таймер. При паузе игровое время стоит.
func (shift *Shift) Tick(dt time.Duration) ShiftUpdate {
	if dt <= 0 || shift.paused {
		return ShiftUpdate{}
	}
	if shift.lobbyPhase == LobbyPhaseStarting {
		before := shift.CountdownSeconds()
		shift.countdown = max(0, shift.countdown-dt)
		if shift.countdown == 0 {
			shift.beginBriefing()
			return ShiftUpdate{LobbyChanged: true, SnapshotChanged: true}
		}
		return ShiftUpdate{LobbyChanged: shift.CountdownSeconds() != before}
	}
	if shift.phase == PhaseDay {
		shift.dayRemaining = max(0, shift.dayRemaining-dt)
		if shift.dayRemaining == 0 {
			return shift.startNight(true)
		}
		return ShiftUpdate{SnapshotChanged: true}
	}
	return ShiftUpdate{}
}

// CountdownSeconds округляет оставшееся время до старта вверх до целой секунды.
func (shift *Shift) CountdownSeconds() int {
	return int((shift.countdown + time.Second - 1) / time.Second)
}

// setCallsign разрешает выбрать свободный позывной до подтверждения готовности и запуска смены.
func (shift *Shift) setCallsign(from Post, p Participant, callsign Callsign) (ShiftUpdate, error) {
	if shift.lobbyPhase == LobbyPhaseShift {
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
	if shift.lobbyPhase == LobbyPhaseShift {
		return ShiftUpdate{}, ErrWrongPhase
	}
	if _, ok := shift.participants[OtherPost(from)]; ready && !ok {
		return ShiftUpdate{}, ErrPartnerMissing
	}
	p.Ready = ready
	shift.participants[from] = p
	shift.reconcileLobby()
	return ShiftUpdate{LobbyChanged: true, SnapshotChanged: shift.lobbyPhase == LobbyPhaseShift}, nil
}

// reconcileLobby запускает отсчёт при готовности обоих постов и сбрасывает
// его, если кто-то снял готовность или потерял связь. Бот всегда готов и на связи.
func (shift *Shift) reconcileLobby() {
	if shift.lobbyPhase == LobbyPhaseShift {
		return
	}
	west, east := shift.participants[PostWest], shift.participants[PostEast]
	if !west.Ready || !east.Ready || !west.Connected || !east.Connected {
		shift.lobbyPhase = LobbyPhaseLobby
		shift.countdown = 0
		return
	}
	if shift.mode == ModeSolo {
		shift.beginBriefing()
	} else if shift.lobbyPhase == LobbyPhaseLobby {
		shift.lobbyPhase = LobbyPhaseStarting
		shift.countdown = lobbyCountdown
	}
}

// beginBriefing завершает отсчёт и задаёт начальную энергию постов и запас времени на первый день.
func (shift *Shift) beginBriefing() {
	shift.lobbyPhase = LobbyPhaseShift
	shift.countdown = 0
	shift.phase = PhaseBriefing
	shift.dayRemaining = FirstDaySeconds * time.Second
	shift.energy = map[Post]int{PostWest: StartEnergy[PostWest], PostEast: StartEnergy[PostEast]}
}
