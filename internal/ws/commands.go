package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/uair10/efir_backend/internal/channel"
	"github.com/uair10/efir_backend/internal/game"
	"github.com/uair10/efir_backend/internal/infrastructure/logging"
)

// CommandReceiver разбирает сообщения WebSocket перед передачей в очередь канала.
func CommandReceiver(log *slog.Logger, conn *channel.Connection) func([]byte) {
	return func(data []byte) {
		cmd, kind, err := decodeClientCommand(data)
		if err != nil {
			log.Debug("message ignored", slog.Int("size", len(data)), logging.Err(err))
			return
		}
		log.Debug("message received", slog.String("type", kind), slog.Int("size", len(data)))
		conn.Receive(cmd, kind)
	}
}

// decodeClientCommand поддерживает плоские сообщения лобби и вложенные команды смены.
//
//nolint:ireturn // игра принимает закрытый набор типов команд
func decodeClientCommand(data []byte) (game.Command, string, error) {
	var envelope struct {
		Type    string          `json:"type"`
		Command json.RawMessage `json:"command"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, "", fmt.Errorf("decode message type: %w", err)
	}
	switch envelope.Type {
	case "set_callsign":
		cmd, err := decodeCallsign(data)
		return cmd, envelope.Type, err
	case "set_ready":
		cmd, err := decodeReady(data)
		return cmd, envelope.Type, err
	case "command":
		return decodeGameCommandWithKind(envelope.Command)
	default:
		return nil, "", fmt.Errorf("%w: message type %q", errUnknownGameCommand, envelope.Type)
	}
}

// decodeCallsign читает позывной из сообщения лобби. Допустимость позывного проверяет смена.
func decodeCallsign(data []byte) (game.SetCallsign, error) {
	var body struct {
		Callsign game.Callsign `json:"callsign"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return game.SetCallsign{}, fmt.Errorf("decode callsign: %w", err)
	}
	return game.SetCallsign(body), nil
}

// decodeReady читает флаг готовности из сообщения лобби.
func decodeReady(data []byte) (game.SetReady, error) {
	var body struct {
		Ready bool `json:"ready"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return game.SetReady{}, fmt.Errorf("decode ready: %w", err)
	}
	return game.SetReady(body), nil
}

var errUnknownGameCommand = errors.New("unknown game command")

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

// commandWithFields создаёт декодер, который читает поля JSON и собирает доменную команду.
func commandWithFields[T game.Command, U any](convert func(U) T) gameCommandDecoder {
	return func(data json.RawMessage) (game.Command, error) {
		var value U
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("decode %T: %w", value, err)
		}
		return convert(value), nil
	}
}

// commandWithoutFields создаёт декодер для команды, которой достаточно её имени.
func commandWithoutFields(cmd game.Command) gameCommandDecoder {
	return func(json.RawMessage) (game.Command, error) { return cmd, nil }
}

// decodeGameCommandWithKind reads the inner command in a shift message.
//
//nolint:ireturn // the simulation receives a closed set of command types
func decodeGameCommandWithKind(data json.RawMessage) (game.Command, string, error) {
	var envelope struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, "", fmt.Errorf("decode game command kind: %w", err)
	}
	decode, ok := gameCommandDecoders[envelope.Kind]
	if !ok {
		return nil, "", fmt.Errorf("%w: %q", errUnknownGameCommand, envelope.Kind)
	}
	cmd, err := decode(data)
	return cmd, envelope.Kind, err
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
