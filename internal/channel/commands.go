package channel

import (
	"encoding/json"
	"errors"
	"fmt"
)

// errInvalidCommand - сообщение не разобралось как команда или не имеет смысла
// (неизвестный type, неизвестный позывной). SPEC, раздел 5: такое игнорируется
// с записью в debug, клиенту ничего не уходит, соединение остаётся.
var errInvalidCommand = errors.New("invalid command")

// errWrongPhase - команда не в своей фазе (лобби-команда после shift).
// Игнорируется, как и в смене.
var errWrongPhase = errors.New("command not allowed in this phase")

// Типы входящих сообщений лобби (раздел 3.1 контракта).
const (
	typeSetCallsign = "set_callsign"
	typeSetReady    = "set_ready"
)

// envelope - общее у всех входящих сообщений. Поля команды лежат рядом с type,
// без вложенного объекта: {"type":"set_callsign","callsign":"Сокол"}.
type envelope struct {
	Type string `json:"type"`
}

// command - разобранная команда клиента. Только данные: выполняет её room
// одним switch. Закрытый тип-сумма, как event.
//
//sumtype:decl
type command interface {
	// kind - type сообщения, для логов.
	kind() string
}

// setCallsign - клиент меняет свой позывной.
type setCallsign struct {
	Callsign Callsign `json:"callsign"`
}

// setReady - клиент меняет готовность.
type setReady struct {
	Ready bool `json:"ready"`
}

func (setCallsign) kind() string { return typeSetCallsign }
func (setReady) kind() string    { return typeSetReady }

// parseCommand разбирает JSON в конкретную команду.
// Не JSON или неизвестный type - errInvalidCommand.
//
//nolint:ireturn // command - закрытый тип-сумма, вызывающий разбирает его switch
func parseCommand(data []byte) (command, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidCommand, err)
	}
	switch env.Type {
	case typeSetCallsign:
		return decode[setCallsign](data)
	case typeSetReady:
		return decode[setReady](data)
	default:
		return nil, fmt.Errorf("%w: unknown type %q", errInvalidCommand, env.Type)
	}
}

// decode разбирает поля команды C.
//
//nolint:ireturn // см. parseCommand
func decode[C command](data []byte) (command, error) {
	var c C
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", errInvalidCommand, c.kind(), err)
	}
	return c, nil
}
