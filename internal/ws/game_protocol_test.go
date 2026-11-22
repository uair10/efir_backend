package ws

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/game"
)

func TestMarshalShiftSnapshot(t *testing.T) {
	t.Parallel()
	snapshot := game.BriefingSnapshot(game.PostEast, game.ModeCoop, false)
	snapshot.Buildings = []game.BuildingView{
		{Type: game.BuildingTurret, X: 5, Y: 6, Post: game.PostEast, HP: 9.5, MaxHP: 10},
	}
	snapshot.GateIntel = map[game.GateID]game.GateIntel{
		game.GateE: {Lost: true},
		game.GateW: {},
	}
	assertGameFixture(t, "shift_snapshot.json", snapshot)

	data, err := marshalPayload(game.Snapshot{Card: &game.CardView{Vote: new(false)}})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"card":{"from":"","title":"","text":"","price":null,"vote":false,"mateVote":null,"result":null,"note":null}`)
}

func TestMarshalGameEvents(t *testing.T) {
	t.Parallel()
	assertGameFixture(t, "shift_events.json", []game.Event{
		game.RadioEvent{From: game.RadioSystem, Text: "Напарник пропал из эфира"},
		game.VoiceEvent{From: game.PostWest, Seconds: 2},
		game.LogEvent{Night: 1, Time: "22:00", Text: "Ночь", Level: "info"},
		game.SoundEvent{Name: "shot", X: new(0)},
		game.ShotEvent{X1: 1, Y1: 2, X2: 3, Y2: 4, Post: game.PostEast},
		game.FXEvent{FX: "burst", X: 1, Y: 2},
		game.CoreHitEvent{},
		game.GateFlashEvent{Gate: game.GateW},
		game.FlashEvent{Text: "Недоступно"},
		game.ReportEvent{Title: "Сводка", Rows: []game.ReportRow{{Label: "Ядро", Value: "100%"}}, Next: "[ К КАРТЕ ]"},
	})
}

func assertGameFixture(t *testing.T, name string, payload any) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	got, err := marshalPayload(payload)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

func TestDecodeGameCommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		body string
		want game.Command
	}{
		{`{"kind":"complete_briefing"}`, game.CompleteBriefing{}},
		{`{"kind":"build","building":"turret","x":0,"y":2}`, game.Build{Building: game.BuildingTurret, Y: 2}},
		{`{"kind":"demolish","x":1,"y":2}`, game.Demolish{X: 1, Y: 2}},
		{`{"kind":"place_barricade","gate":"E"}`, game.PlaceBarricade{Gate: game.GateE}},
		{`{"kind":"remove_barricade","gate":"E"}`, game.RemoveBarricade{Gate: game.GateE}},
		{`{"kind":"scout_gate","gate":"E"}`, game.ScoutGate{Gate: game.GateE}},
		{`{"kind":"place_map_marker","x":1.5,"y":2.5}`, game.PlaceMapMarker{X: 1.5, Y: 2.5}},
		{`{"kind":"request_support","square":"Б2"}`, game.RequestSupport{Square: "Б2"}},
		{`{"kind":"fulfill_energy_request","requestId":3}`, game.FulfillEnergyRequest{RequestID: 3}},
		{`{"kind":"refuel_diesel"}`, game.RefuelDiesel{}},
		{`{"kind":"vote_on_request","yes":true}`, game.VoteOnRequest{Yes: true}},
		{`{"kind":"start_night"}`, game.StartNight{}},
		{`{"kind":"set_paused","paused":false}`, game.SetPaused{}},
		{`{"kind":"start_next_day"}`, game.StartNextDay{}},
		{`{"kind":"voice","seconds":2}`, game.Voice{Seconds: 2}},
	} {
		got, err := decodeGameCommand([]byte(tc.body))
		require.NoError(t, err, tc.body)
		assert.Equal(t, tc.want, got, tc.body)
	}
	for _, body := range []string{`null`, `{"kind":"briefing_done"}`, `{"kind":"set_paused","paused":"false"}`} {
		_, err := decodeGameCommand([]byte(body))
		assert.Error(t, err, body)
	}
}
