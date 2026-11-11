package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
)

// 404 и 405 от самого mux - тоже в модели huma.
func TestRouteErrorsAsProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantAllow  string
	}{
		{"неизвестный путь", http.MethodGet, "/unknown", http.StatusNotFound, ""},
		{"неверный метод", http.MethodPost, "/healthz", http.StatusMethodNotAllowed, "GET, HEAD"},
	}

	handler := newHandler(slog.New(slog.DiscardHandler), testConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, http.NoBody)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantAllow, rec.Header().Get("Allow"), "header Allow")
			checkError(t, rec, tt.wantStatus)
		})
	}
}

type codeInput struct {
	Code string `path:"code"`
}

// Собственный 404 операции не должен подменяться на "route not found".
func TestHandlerNotFoundNotRewritten(t *testing.T) {
	t.Parallel()

	handler := newTestAPIHandler(slog.New(slog.DiscardHandler), testConfig(), func(api huma.API) {
		huma.Get(api, "/test/channels/{code}", func(_ context.Context, in *codeInput) (*struct{}, error) {
			return nil, huma.Error404NotFound("channel " + in.Code + " not found")
		})
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test/channels/482117", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := checkError(t, rec, http.StatusNotFound)
	assert.Equal(t, "channel 482117 not found", body["detail"], "field detail")
}
