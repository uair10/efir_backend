package httpapi

import (
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/channel"
	"github.com/uair10/efir_backend/internal/httpapi/requestid"
	"github.com/uair10/efir_backend/internal/infrastructure/config"
)

// testMaxBodyBytes - лимит тела в тестах: маленький, чтобы 413 получить коротким телом.
const testMaxBodyBytes = 1024

// testConfig - конфиг сервера для тестов: короткие таймауты, маленький лимит тела, документация включена.
func testConfig() config.Server {
	return config.Server{
		ShutdownTimeout:   time.Second,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		MaxBodyBytes:      testMaxBodyBytes,
		DocsEnabled:       true,
	}
}

// newTestAPIHandler собирает обработчик так же, как newHandler, но позволяет
// зарегистрировать в том же API тестовые операции.
func newTestAPIHandler(logger *slog.Logger, cfg config.Server, register func(api huma.API)) http.Handler {
	mux := http.NewServeMux()
	api := newAPI(mux, cfg)
	registerRoutes(api, testRegistry())
	if register != nil {
		register(api)
	}
	return withMiddleware(mux, logger, cfg.MaxBodyBytes)
}

// newRequest собирает запрос с контекстом теста.
func newRequest(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
}

// doRequest прогоняет запрос через handler и возвращает записанный ответ.
func doRequest(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// serve выполняет запрос к handler. contentType пустой - заголовок не ставится.
func serve(t *testing.T, handler http.Handler, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return doRequest(handler, req)
}

// decodeProblem проверяет Content-Type и разбирает тело как problem+json.
func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json",
		"Content-Type of error response, body: %s", rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body is not JSON: %s", rec.Body.String())
	return body
}

// errorKeys - поля huma.ErrorModel. Других (например, $schema) быть не должно.
var errorKeys = []string{"type", "title", "status", "detail", "instance", "errors"} //nolint:gochecknoglobals // неизменяемый список для тестов

// checkError проверяет ответ-ошибку на модель huma (ADR-006) и возвращает разобранное тело.
func checkError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) map[string]any {
	t.Helper()
	assert.Equal(t, wantStatus, rec.Code, "HTTP status, body: %s", rec.Body.String())
	body := decodeProblem(t, rec)

	assert.Subset(t, errorKeys, slices.Collect(maps.Keys(body)), "fields of error body outside huma.ErrorModel")
	assert.EqualValues(t, wantStatus, body["status"], "field status")
	assert.NotEmpty(t, body["title"], "field title")
	assert.NotEmpty(t, rec.Header().Get(requestid.Header), "header %s", requestid.Header)
	return body
}

// errorLocations проверяет формат errors[] (message и location, без value)
// и возвращает отсортированный список location.
func errorLocations(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["errors"].([]any)
	require.True(t, ok, "errors = %#v, want array", body["errors"])

	locations := make([]string, 0, len(raw))
	for i, item := range raw {
		e, ok := item.(map[string]any)
		require.True(t, ok, "errors[%d] = %#v, want object", i, item)
		assert.NotContains(t, e, "value", "errors[%d]: request value leaked", i)
		assert.NotEmpty(t, e["message"], "errors[%d].message", i)
		location, _ := e["location"].(string)
		locations = append(locations, location)
	}
	slices.Sort(locations)
	return locations
}

// testRegistry - реестр для тестов, которым каналы не важны.
func testRegistry() *channel.Registry { return channel.NewRegistry(10) }
