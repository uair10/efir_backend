package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/uair10/efir_backend/internal/game"
)

//nolint:tagliatelle // the WebSocket contract uses camelCase fields
type gameSnapshotJSON struct {
	Phase               game.Phase                    `json:"phase"`
	Night               int                           `json:"night"`
	Paused              bool                          `json:"paused"`
	CanPause            bool                          `json:"canPause"`
	DaySecondsRemaining float64                       `json:"daySecondsRemaining"`
	EarlyBonus          int                           `json:"earlyBonus"`
	NightSecondsElapsed float64                       `json:"nightSecondsElapsed"`
	NightAccelerated    bool                          `json:"nightAccelerated"`
	Energy              int                           `json:"energy"`
	MateEnergy          int                           `json:"mateEnergy"`
	FuelStock           int                           `json:"fuelStock"`
	Tank                float64                       `json:"tank"`
	MateTank            float64                       `json:"mateTank"`
	CoreHP              float64                       `json:"coreHp"`
	Card                *cardViewJSON                 `json:"card"`
	Shaft               *shaftViewJSON                `json:"shaft"`
	OpenGate            *game.GateID                  `json:"openGate"`
	Noise               bool                          `json:"noise"`
	Jam                 bool                          `json:"jam"`
	CoreJammed          bool                          `json:"coreJammed"`
	Ready               []game.Post                   `json:"ready"`
	Vision              string                        `json:"vision"`
	Buildings           []buildingViewJSON            `json:"buildings"`
	Enemies             []enemyViewJSON               `json:"enemies"`
	Barricades          []barricadeViewJSON           `json:"barricades"`
	GateIntel           map[game.GateID]gateIntelJSON `json:"gateIntel"`
	Drones              []droneViewJSON               `json:"drones"`
	Pings               []pingViewJSON                `json:"pings"`
	SquarePings         []squarePingViewJSON          `json:"squarePings"`
	EnergyRequests      []energyRequestJSON           `json:"energyRequests"`
	ScoutedToday        bool                          `json:"scoutedToday"`
}

//nolint:tagliatelle // the WebSocket contract uses camelCase fields
type buildingViewJSON struct {
	Type   game.BuildingType `json:"type"`
	X      int               `json:"x"`
	Y      int               `json:"y"`
	Post   game.Post         `json:"post"`
	HP     float64           `json:"hp"`
	MaxHP  float64           `json:"maxHp"`
	Angle  float64           `json:"angle"`
	Jammed bool              `json:"jammed"`
	Hit    bool              `json:"hit"`
	Kills  int               `json:"kills"`
}

//nolint:tagliatelle // the WebSocket contract uses camelCase fields
type enemyViewJSON struct {
	ID    int            `json:"id"`
	Type  game.EnemyType `json:"type"`
	X     float64        `json:"x"`
	Y     float64        `json:"y"`
	HP    float64        `json:"hp"`
	MaxHP float64        `json:"maxHp"`
	Dir   float64        `json:"dir"`
	Hit   bool           `json:"hit"`
	Gate  game.GateID    `json:"gate"`
}

//nolint:tagliatelle // the WebSocket contract uses camelCase fields
type barricadeViewJSON struct {
	Gate  game.GateID `json:"gate"`
	Owner game.Post   `json:"owner"`
	HP    float64     `json:"hp"`
	MaxHP float64     `json:"maxHp"`
	Hit   bool        `json:"hit"`
}

type gateIntelJSON struct {
	Lost     bool  `json:"lost"`
	Estimate *int  `json:"estimate,omitempty"`
	Main     *bool `json:"main,omitempty"`
}

type droneViewJSON struct {
	Gate     game.GateID `json:"gate"`
	By       game.Post   `json:"by"`
	Progress float64     `json:"progress"`
}

type pingViewJSON struct {
	X    float64   `json:"x"`
	Y    float64   `json:"y"`
	From game.Post `json:"from"`
	Left float64   `json:"left"`
}

type squarePingViewJSON struct {
	Square string    `json:"square"`
	From   game.Post `json:"from"`
}

type shaftViewJSON struct {
	X        int  `json:"x"`
	Y        int  `json:"y"`
	Pending  bool `json:"pending"`
	Breached bool `json:"breached"`
}

//nolint:tagliatelle // the WebSocket contract uses camelCase fields
type cardViewJSON struct {
	From     string  `json:"from"`
	Title    string  `json:"title"`
	Text     string  `json:"text"`
	Price    *string `json:"price"`
	Vote     *bool   `json:"vote"`
	MateVote *bool   `json:"mateVote"`
	Result   *string `json:"result"`
	Note     *string `json:"note"`
}

type energyRequestJSON struct {
	ID       int               `json:"id"`
	Building game.BuildingType `json:"building"`
	Cost     int               `json:"cost"`
}

type reportRowJSON struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Bad   bool   `json:"bad,omitempty"`
}

type radioEventJSON struct {
	Kind    string         `json:"kind"`
	From    game.RadioFrom `json:"from"`
	Text    string         `json:"text"`
	Level   game.LogLevel  `json:"level,omitempty"`
	Garbled bool           `json:"garbled,omitempty"`
}

type voiceEventJSON struct {
	Kind    string    `json:"kind"`
	From    game.Post `json:"from"`
	Seconds int       `json:"seconds"`
	Garbled bool      `json:"garbled"`
}

type logEventJSON struct {
	Kind  string        `json:"kind"`
	Night int           `json:"night"`
	Time  string        `json:"time"`
	Text  string        `json:"text"`
	Level game.LogLevel `json:"level"`
}

type soundEventJSON struct {
	Kind string         `json:"kind"`
	Name game.SoundName `json:"name"`
	X    *int           `json:"x,omitempty"`
	Mine bool           `json:"mine,omitempty"`
}

type shotEventJSON struct {
	Kind string    `json:"kind"`
	X1   float64   `json:"x1"`
	Y1   float64   `json:"y1"`
	X2   float64   `json:"x2"`
	Y2   float64   `json:"y2"`
	Post game.Post `json:"post"`
}

type fxEventJSON struct {
	Kind string  `json:"kind"`
	FX   string  `json:"fx"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

type coreHitEventJSON struct {
	Kind string `json:"kind"`
}

type gateFlashEventJSON struct {
	Kind string      `json:"kind"`
	Gate game.GateID `json:"gate"`
}

type flashEventJSON struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type reportEventJSON struct {
	Kind  string          `json:"kind"`
	Title string          `json:"title"`
	Text  string          `json:"text,omitempty"`
	Rows  []reportRowJSON `json:"rows"`
	Next  string          `json:"next"`
	Final bool            `json:"final"`
}

// marshalPayload maps typed game data to the WebSocket contract.
func marshalPayload(payload any) ([]byte, error) {
	switch value := payload.(type) {
	case game.Snapshot:
		return encodeJSON(struct {
			Type     string           `json:"type"`
			Snapshot gameSnapshotJSON `json:"snapshot"`
		}{"game", snapshotJSON(value)})
	case []game.Event:
		events := make([]any, 0, len(value))
		for _, event := range value {
			encoded, err := eventJSON(event)
			if err != nil {
				return nil, err
			}
			events = append(events, encoded)
		}
		return encodeJSON(struct {
			Type   string `json:"type"`
			Events []any  `json:"events"`
		}{"events", events})
	default:
		return encodeJSON(payload)
	}
}

func snapshotJSON(snapshot game.Snapshot) gameSnapshotJSON {
	intel := make(map[game.GateID]gateIntelJSON, len(snapshot.GateIntel))
	for gate, info := range snapshot.GateIntel {
		entry := gateIntelJSON{Lost: info.Lost}
		if !info.Lost {
			entry.Estimate = new(info.Estimate)
			entry.Main = new(info.Main)
		}
		intel[gate] = entry
	}
	return gameSnapshotJSON{
		Phase:               snapshot.Phase,
		Night:               snapshot.Night,
		Paused:              snapshot.Paused,
		CanPause:            snapshot.CanPause,
		DaySecondsRemaining: snapshot.DaySecondsRemaining,
		EarlyBonus:          snapshot.EarlyBonus,
		NightSecondsElapsed: snapshot.NightSecondsElapsed,
		NightAccelerated:    snapshot.NightAccelerated,
		Energy:              snapshot.Energy,
		MateEnergy:          snapshot.MateEnergy,
		FuelStock:           snapshot.FuelStock,
		Tank:                snapshot.Tank,
		MateTank:            snapshot.MateTank,
		CoreHP:              snapshot.CoreHP,
		Card:                (*cardViewJSON)(snapshot.Card),
		Shaft:               (*shaftViewJSON)(snapshot.Shaft),
		OpenGate:            snapshot.OpenGate,
		Noise:               snapshot.Noise,
		Jam:                 snapshot.Jam,
		CoreJammed:          snapshot.CoreJammed,
		Ready:               append([]game.Post{}, snapshot.Ready...),
		Vision:              snapshot.Vision,
		Buildings:           mapSlice(snapshot.Buildings, func(value game.BuildingView) buildingViewJSON { return buildingViewJSON(value) }),
		Enemies:             mapSlice(snapshot.Enemies, func(value game.EnemyView) enemyViewJSON { return enemyViewJSON(value) }),
		Barricades:          mapSlice(snapshot.Barricades, func(value game.BarricadeView) barricadeViewJSON { return barricadeViewJSON(value) }),
		GateIntel:           intel,
		Drones:              mapSlice(snapshot.Drones, func(value game.DroneView) droneViewJSON { return droneViewJSON(value) }),
		Pings:               mapSlice(snapshot.Pings, func(value game.PingView) pingViewJSON { return pingViewJSON(value) }),
		SquarePings:         mapSlice(snapshot.SquarePings, func(value game.SquarePingView) squarePingViewJSON { return squarePingViewJSON(value) }),
		EnergyRequests:      mapSlice(snapshot.EnergyRequests, func(value game.EnergyRequest) energyRequestJSON { return energyRequestJSON(value) }),
		ScoutedToday:        snapshot.ScoutedToday,
	}
}

// mapSlice preserves the contract's empty arrays even when the domain slice is nil.
func mapSlice[T, U any](values []T, convert func(T) U) []U {
	result := make([]U, len(values))
	for i, value := range values {
		result[i] = convert(value)
	}
	return result
}

func eventJSON(event game.Event) (any, error) {
	switch value := event.(type) {
	case game.RadioEvent:
		return radioEventJSON{Kind: "radio", From: value.From, Text: value.Text, Level: value.Level, Garbled: value.Garbled}, nil
	case game.VoiceEvent:
		return voiceEventJSON{Kind: "voice", From: value.From, Seconds: value.Seconds, Garbled: value.Garbled}, nil
	case game.LogEvent:
		return logEventJSON{Kind: "log", Night: value.Night, Time: value.Time, Text: value.Text, Level: value.Level}, nil
	case game.SoundEvent:
		return soundEventJSON{Kind: "sound", Name: value.Name, X: value.X, Mine: value.Mine}, nil
	case game.ShotEvent:
		return shotEventJSON{Kind: "shot", X1: value.X1, Y1: value.Y1, X2: value.X2, Y2: value.Y2, Post: value.Post}, nil
	case game.FXEvent:
		return fxEventJSON{Kind: "fx", FX: value.FX, X: value.X, Y: value.Y}, nil
	case game.CoreHitEvent:
		return coreHitEventJSON{Kind: "core_hit"}, nil
	case game.GateFlashEvent:
		return gateFlashEventJSON{Kind: "gate_flash", Gate: value.Gate}, nil
	case game.FlashEvent:
		return flashEventJSON{Kind: "flash", Text: value.Text}, nil
	case game.ReportEvent:
		return reportEventJSON{Kind: "report", Title: value.Title, Text: value.Text, Rows: mapSlice(value.Rows, func(row game.ReportRow) reportRowJSON { return reportRowJSON(row) }), Next: value.Next, Final: value.Final}, nil
	}
	return nil, fmt.Errorf("%w: %T", errUnknownGameEvent, event)
}

var (
	errUnknownGameEvent   = errors.New("unknown game event")
	errUnknownGameCommand = errors.New("unknown game command")
)

func encodeJSON(payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode %T: %w", payload, err)
	}
	return data, nil
}

type gameCommandDecoder func(json.RawMessage) (game.Command, error)

// gameCommandDecoders is the wire contract for shift commands.
//
//nolint:gochecknoglobals // fixed dispatch table shared by connections
var gameCommandDecoders = map[string]gameCommandDecoder{
	"complete_briefing": commandWithoutFields(game.CompleteBriefing{}),
	"build":             commandWithFields(func(value buildJSON) game.Build { return game.Build(value) }),
	"demolish":          commandWithFields(func(value demolishJSON) game.Demolish { return game.Demolish(value) }),
	"place_barricade":   commandWithFields(func(value placeBarricadeJSON) game.PlaceBarricade { return game.PlaceBarricade(value) }),
	"remove_barricade":  commandWithFields(func(value removeBarricadeJSON) game.RemoveBarricade { return game.RemoveBarricade(value) }),
	"scout_gate":        commandWithFields(func(value scoutGateJSON) game.ScoutGate { return game.ScoutGate(value) }),
	"place_map_marker":  commandWithFields(func(value placeMapMarkerJSON) game.PlaceMapMarker { return game.PlaceMapMarker(value) }),
	"request_support":   commandWithFields(func(value requestSupportJSON) game.RequestSupport { return game.RequestSupport(value) }),
	"fulfill_energy_request": commandWithFields(func(value fulfillEnergyRequestJSON) game.FulfillEnergyRequest {
		return game.FulfillEnergyRequest(value)
	}),
	"refuel_diesel":   commandWithoutFields(game.RefuelDiesel{}),
	"vote_on_request": commandWithFields(func(value voteOnRequestJSON) game.VoteOnRequest { return game.VoteOnRequest(value) }),
	"start_night":     commandWithoutFields(game.StartNight{}),
	"set_paused":      commandWithFields(func(value setPausedJSON) game.SetPaused { return game.SetPaused(value) }),
	"start_next_day":  commandWithoutFields(game.StartNextDay{}),
	"voice":           commandWithFields(func(value voiceJSON) game.Voice { return game.Voice(value) }),
}

func commandWithFields[T game.Command, U any](convert func(U) T) gameCommandDecoder {
	return func(data json.RawMessage) (game.Command, error) {
		var value U
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("decode %T: %w", value, err)
		}
		return convert(value), nil
	}
}

func commandWithoutFields(cmd game.Command) gameCommandDecoder {
	return func(json.RawMessage) (game.Command, error) { return cmd, nil }
}

// decodeGameCommand reads the inner command in a shift message.
//
//nolint:ireturn // the simulation receives a closed set of command types
func decodeGameCommand(data json.RawMessage) (game.Command, error) {
	var envelope struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode game command kind: %w", err)
	}
	decode, ok := gameCommandDecoders[envelope.Kind]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errUnknownGameCommand, envelope.Kind)
	}
	return decode(data)
}

type buildJSON struct {
	Building game.BuildingType `json:"building"`
	X        int               `json:"x"`
	Y        int               `json:"y"`
}

type demolishJSON struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type placeBarricadeJSON struct {
	Gate game.GateID `json:"gate"`
}

type removeBarricadeJSON struct {
	Gate game.GateID `json:"gate"`
}

type scoutGateJSON struct {
	Gate game.GateID `json:"gate"`
}

type placeMapMarkerJSON struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type requestSupportJSON struct {
	Square string `json:"square"`
}

//nolint:tagliatelle // the WebSocket contract uses camelCase fields
type fulfillEnergyRequestJSON struct {
	RequestID int `json:"requestId"`
}

type voteOnRequestJSON struct {
	Yes bool `json:"yes"`
}

type setPausedJSON struct {
	Paused bool `json:"paused"`
}

type voiceJSON struct {
	Seconds int `json:"seconds"`
}

// HandleGameFrame recognizes shift commands and validates their wire format.
// The shift simulation starts handling commands in EFR-8.
func HandleGameFrame(data []byte, log *slog.Logger) bool {
	var envelope struct {
		Type    string          `json:"type"`
		Command json.RawMessage `json:"command"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type != "command" {
		return false
	}
	if _, err := decodeGameCommand(envelope.Command); err != nil {
		log.Debug("game command ignored", "error", err)
	}
	return true
}
