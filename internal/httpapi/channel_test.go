package httpapi

import (
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/channel"
)

const jsonContentType = "application/json"

// channelResp - тело ответа на создание канала и вход по коду.
type channelResp struct {
	ChannelID string `json:"channelId"` //nolint:tagliatelle // имя поля из контракта с фронтендом
	Code      string `json:"code"`
	Token     string `json:"token"`
}

// newChannelsHandler - обработчик с реестром на maxChannels каналов.
// Реестр возвращается, чтобы тест мог подготовить каналы напрямую.
func newChannelsHandler(t *testing.T, maxChannels int) (http.Handler, *channel.Registry) {
	t.Helper()
	reg := channel.NewRegistry(maxChannels)
	return newHandler(slog.New(slog.DiscardHandler), testConfig(), reg), reg
}

// createChannel - POST /api/channels.
func createChannel(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, h, http.MethodPost, "/api/channels", jsonContentType, body)
}

// joinChannel - POST /api/channels/{code}/players с телом {}.
func joinChannel(t *testing.T, h http.Handler, code string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, h, http.MethodPost, "/api/channels/"+code+"/players", jsonContentType, `{}`)
}

// decodeChannel проверяет статус и разбирает успешный ответ.
func decodeChannel(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) channelResp {
	t.Helper()
	require.Equal(t, wantStatus, rec.Code, "HTTP status, body: %s", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), jsonContentType)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw), "body: %s", rec.Body.String())
	assert.ElementsMatch(t, []string{"channelId", "code", "token"}, slices.Collect(maps.Keys(raw)), "лишние или недостающие поля")

	var resp channelResp
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

// mustCreate открывает канал через HTTP и возвращает ответ.
func mustCreate(t *testing.T, h http.Handler, mode channel.GameMode) channelResp {
	t.Helper()
	return decodeChannel(t, createChannel(t, h, `{"mode":"`+string(mode)+`"}`), http.StatusCreated)
}

// BR-2.1, BR-2.4: создание канала в обоих режимах.
func TestCreateChannel_OK(t *testing.T) {
	t.Parallel()

	for _, mode := range []channel.GameMode{channel.GameModeSolo, channel.GameModeCoop} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			h, reg := newChannelsHandler(t, 10)
			resp := mustCreate(t, h, mode)

			assert.Regexp(t, `^[1-9][0-9]{5}$`, resp.Code, "BR-1.1: 6 цифр, первая не 0")
			assert.Regexp(t, `^c_[A-Za-z0-9_-]+$`, resp.ChannelID)
			assert.NotContains(t, resp.ChannelID, resp.Code, "BR-2.4: id не выводится из кода")
			assert.NotEmpty(t, resp.Token)

			// Канал реально лежит в общем реестре, а не в одноразовом.
			assert.Equal(t, 1, reg.Len())
			ch, err := reg.ByID(resp.ChannelID)
			require.NoError(t, err)
			assert.Equal(t, resp.Code, ch.Code().String())
			assert.Equal(t, mode, ch.Mode())

			// Токен из ответа пускает хозяина на west.
			tok, err := channel.ParseToken(resp.Token)
			require.NoError(t, err)
			post, ok := ch.PostOf(tok)
			require.True(t, ok, "токен из ответа не опознан каналом")
			assert.Equal(t, channel.PostWest, post)
		})
	}
}

// BR-2.4: два канала - разные id, коды и токены.
func TestCreateChannel_Unique(t *testing.T) {
	t.Parallel()

	h, reg := newChannelsHandler(t, 10)
	a := mustCreate(t, h, channel.GameModeCoop)
	b := mustCreate(t, h, channel.GameModeCoop)

	assert.NotEqual(t, a.ChannelID, b.ChannelID)
	assert.NotEqual(t, a.Code, b.Code)
	assert.NotEqual(t, a.Token, b.Token)
	assert.Equal(t, 2, reg.Len())
}

// BR-2.1: mode - только solo или coop; кривое тело - тоже 422.
func TestCreateChannel_Invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "неизвестный mode", body: `{"mode":"duel"}`},
		{name: "mode в другом регистре", body: `{"mode":"COOP"}`},
		{name: "пустой mode", body: `{"mode":""}`},
		{name: "mode не строка", body: `{"mode":1}`},
		{name: "нет mode", body: `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, reg := newChannelsHandler(t, 10)
			rec := createChannel(t, h, tt.body)

			checkError(t, rec, http.StatusUnprocessableEntity)
			assert.Zero(t, reg.Len(), "канал не должен создаться")
		})
	}
}

// BR-2.3, BR-1.7: лимит каналов -> 503.
func TestCreateChannel_TooMany(t *testing.T) {
	t.Parallel()

	h, reg := newChannelsHandler(t, 2)
	mustCreate(t, h, channel.GameModeCoop)
	mustCreate(t, h, channel.GameModeSolo) // одиночный тоже в счёт

	rec := createChannel(t, h, `{"mode":"coop"}`)
	checkError(t, rec, http.StatusServiceUnavailable)
	assert.Equal(t, 2, reg.Len())
}

// Вход гостя: 200, тот же канал, свой токен на east.
func TestJoinChannel_OK(t *testing.T) {
	t.Parallel()

	h, reg := newChannelsHandler(t, 10)
	host := mustCreate(t, h, channel.GameModeCoop)

	guest := decodeChannel(t, joinChannel(t, h, host.Code), http.StatusOK)

	assert.Equal(t, host.ChannelID, guest.ChannelID)
	assert.Equal(t, host.Code, guest.Code)
	assert.NotEmpty(t, guest.Token)
	assert.NotEqual(t, host.Token, guest.Token, "у гостя свой токен")

	ch, err := reg.ByID(host.ChannelID)
	require.NoError(t, err)
	tok, err := channel.ParseToken(guest.Token)
	require.NoError(t, err)
	post, ok := ch.PostOf(tok)
	require.True(t, ok)
	assert.Equal(t, channel.PostEast, post)
}

// BR-2.2, BR-2.3: статусы входа по коду.
func TestJoinChannel_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// prepare готовит канал и возвращает код для входа.
		prepare    func(t *testing.T, h http.Handler) string
		wantStatus int
	}{
		{
			name:       "BR-2.2: 5 цифр",
			prepare:    func(*testing.T, http.Handler) string { return "12345" },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "BR-2.2: 7 цифр",
			prepare:    func(*testing.T, http.Handler) string { return "1234567" },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "BR-2.2: буквы",
			prepare:    func(*testing.T, http.Handler) string { return "12a456" },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "BR-2.2: не ASCII-цифры",
			prepare:    func(*testing.T, http.Handler) string { return "١٢٣٤٥٦" },
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "BR-2.2: ведущий ноль проходит формат, но канала нет",
			prepare:    func(*testing.T, http.Handler) string { return "012345" },
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "BR-2.3: нет канала",
			prepare:    func(*testing.T, http.Handler) string { return "999999" },
			wantStatus: http.StatusNotFound,
		},
		{
			name: "BR-2.3, BR-1.5: одиночный канал выглядит несуществующим",
			prepare: func(t *testing.T, h http.Handler) string {
				t.Helper()
				return mustCreate(t, h, channel.GameModeSolo).Code
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "BR-2.3, BR-1.6: оба поста заняты",
			prepare: func(t *testing.T, h http.Handler) string {
				t.Helper()
				code := mustCreate(t, h, channel.GameModeCoop).Code
				decodeChannel(t, joinChannel(t, h, code), http.StatusOK)
				return code
			},
			wantStatus: http.StatusConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, _ := newChannelsHandler(t, 10)
			code := tt.prepare(t, h)

			body := checkError(t, joinChannel(t, h, code), tt.wantStatus)
			assert.NotContains(t, body, "token")
		})
	}
}

// BR-2.2: 404 на одиночный и несуществующий канал неотличимы снаружи
// (кроме request ID в заголовке).
func TestJoinChannel_SoloIndistinguishable(t *testing.T) {
	t.Parallel()

	h, _ := newChannelsHandler(t, 10)
	solo := mustCreate(t, h, channel.GameModeSolo)

	missing := "999999"
	if solo.Code == missing {
		missing = "999998"
	}

	recSolo := joinChannel(t, h, solo.Code)
	recMissing := joinChannel(t, h, missing)

	require.Equal(t, http.StatusNotFound, recSolo.Code)
	require.Equal(t, http.StatusNotFound, recMissing.Code)
	assert.JSONEq(t, recMissing.Body.String(), recSolo.Body.String())
}

// Ошибка 422 не должна отражать значение из запроса (ADR-005).
func TestJoinChannel_InvalidCodeNotEchoed(t *testing.T) {
	t.Parallel()

	h, _ := newChannelsHandler(t, 10)
	rec := joinChannel(t, h, "12x456")

	body := checkError(t, rec, http.StatusUnprocessableEntity)
	errorLocations(t, body) // проверяет, что в errors[] нет value
}
