package channel

import "slices"

// Callsign - позывной игрока.
type Callsign string

// Позывные по порядку списка.
const (
	CallsignFirst  Callsign = "Первый"
	CallsignSecond Callsign = "Второй"
)

// callsigns - все допустимые позывные в порядке выдачи.
//
//nolint:gochecknoglobals // справочник только для чтения
var callsigns = [...]Callsign{
	CallsignFirst,
	CallsignSecond,
	"Заря",
	"Гранит",
	"Сокол",
	"Факел",
	"Гроза",
	"Тайга",
}

func (c Callsign) String() string { return string(c) }

// ParseCallsign проверяет, что s - один из допустимых позывных.
// При ok == false возвращает пустой позывной.
func ParseCallsign(s string) (Callsign, bool) {
	for _, c := range callsigns {
		if string(c) == s {
			return c, true
		}
	}
	return "", false
}

// firstFreeCallsign возвращает первый позывной из списка, которого нет в taken.
// ok == false - свободных не осталось.
func firstFreeCallsign(taken ...Callsign) (Callsign, bool) {
	for _, c := range callsigns {
		if !slices.Contains(taken, c) {
			return c, true
		}
	}
	return "", false
}
