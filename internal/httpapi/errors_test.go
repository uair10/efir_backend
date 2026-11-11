package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/efir_backend/internal/httpapi/requestid"
)

// Ошибки отдаются в стандартной модели huma (ADR-006).

// secretValue не должен попасть в тело ответа, а в лог - только у 5xx.
const secretValue = "efir_SecretValue123"

// echoInput - тестовая операция с query-параметром и телом с ограничениями формата.
type echoInput struct {
	Limit int `default:"20" maximum:"100" minimum:"1" query:"limit"`
	Body  echoBody
}

type echoBody struct {
	URL   string `json:"url"             maxLength:"64" minLength:"1"`
	Count int    `json:"count,omitempty" minimum:"0"`
}

type echoOutput struct {
	Body echoOutBody
}

type echoOutBody struct {
	URL string `json:"url"`
}

// headerInput - тестовая операция с проверяемым заголовком.
type headerInput struct {
	Key string `header:"X-Test-Key" pattern:"^[a-z]+$" required:"true"`
}

// registerTestOps регистрирует операции, на которых проверяются ошибки.
func registerTestOps(api huma.API) {
	huma.Post(api, "/test/echo", func(_ context.Context, in *echoInput) (*echoOutput, error) {
		return &echoOutput{Body: echoOutBody{URL: in.Body.URL}}, nil
	})
	huma.Get(api, "/test/header", func(context.Context, *headerInput) (*struct{}, error) {
		return &struct{}{}, nil
	})
	huma.Get(api, "/test/conflict", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, huma.Error409Conflict("channel is full")
	})
	huma.Get(api, "/test/wrapped", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, fmt.Errorf("join channel: %w", huma.Error404NotFound("channel not found"))
	})
	huma.Get(api, "/test/unknown", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, errors.New("db: password=" + secretValue)
	})
}

func newErrorsTestHandler(buf *bytes.Buffer) http.Handler {
	return newTestAPIHandler(slog.New(slog.NewJSONHandler(buf, nil)), testConfig(), registerTestOps)
}

// ошибки, которые Huma находит сама, - со своим статусом, при 422 с errors[].
func TestRequestErrors(t *testing.T) {
	t.Parallel()

	const jsonCT = "application/json"
	tooLarge := `{"url":"` + strings.Repeat("a", testMaxBodyBytes) + `"}`

	tests := []struct {
		name          string
		target        string
		contentType   string
		body          string
		wantStatus    int
		wantLocations []string // nil - поле errors не проверяется
	}{
		{"битый JSON", "/test/echo", jsonCT, `{"url":`, 400, nil},
		{"не JSON", "/test/echo", "text/plain", "url=x", 415, nil},
		{"тело больше лимита", "/test/echo", jsonCT, tooLarge, 413, nil},
		{"лишнее поле", "/test/echo", jsonCT, `{"url":"x","extra":1}`, 422, []string{"body.extra"}},
		{"неверный тип", "/test/echo", jsonCT, `{"url":123}`, 422, []string{"body.url"}},
		{"нарушено ограничение", "/test/echo", jsonCT, `{"url":""}`, 422, []string{"body.url"}},
		{
			"все нарушения сразу", "/test/echo", jsonCT, `{"url":"","count":-1,"extra":true}`,
			422,
			[]string{"body.count", "body.extra", "body.url"},
		},
		{"query не число", "/test/echo?limit=abc", jsonCT, `{"url":"x"}`, 422, []string{"query.limit"}},
		{"query вне диапазона", "/test/echo?limit=0", jsonCT, `{"url":"x"}`, 422, []string{"query.limit"}},
	}

	handler := newErrorsTestHandler(&bytes.Buffer{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, handler, http.MethodPost, tt.target, tt.contentType, tt.body)
			body := checkError(t, rec, tt.wantStatus)
			if tt.wantLocations != nil {
				assert.Equal(t, tt.wantLocations, errorLocations(t, body), "locations in errors[]")
			}
		})
	}
}

// Валидный запрос проходит: тестовая операция рабочая, ошибки выше - не случайность.
func TestRequestValid(t *testing.T) {
	t.Parallel()

	rec := serve(t, newErrorsTestHandler(&bytes.Buffer{}), http.MethodPost, "/test/echo?limit=5",
		"application/json", `{"url":"https://go.dev"}`)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.JSONEq(t, `{"url":"https://go.dev"}`, rec.Body.String())
}

// Значения из запроса не возвращаются в ответе об ошибке
// (Huma по умолчанию кладёт их в errors[].value, installErrorPolicy их убирает).
func TestErrorBodyHasNoRequestValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		header string
		body   string
	}{
		{"лишнее поле", "/test/echo", "", `{"url":"x","token":"` + secretValue + `"}`},
		{"нарушено ограничение", "/test/echo", "", `{"url":"` + secretValue + strings.Repeat("a", 64) + `"}`},
		{"битый JSON", "/test/echo", "", `{"url":"` + secretValue + `"`},
		{"заголовок", "/test/header", secretValue, ""},
	}

	handler := newErrorsTestHandler(&bytes.Buffer{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := http.MethodPost
			if tt.header != "" {
				method = http.MethodGet
			}
			req := newRequest(t, method, tt.target, tt.body)
			if tt.header != "" {
				req.Header.Set("X-Test-Key", tt.header)
			}
			rec := doRequest(handler, req)

			require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, "want error status")
			assert.NotContains(t, rec.Body.String(), secretValue, "error body echoes request value")
		})
	}
}

// ошибка, которую вернула операция, - в модели huma (обёрнутая тоже),
// неизвестная ошибка - 500 без текста.
func TestOperationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantDetail string
	}{
		{"ошибка huma как есть", "/test/conflict", 409, "channel is full"},
		{"обёрнутая ошибка huma", "/test/wrapped", 404, "channel not found"},
		{"неизвестная ошибка", "/test/unknown", 500, "Internal Server Error"},
	}

	handler := newErrorsTestHandler(&bytes.Buffer{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, handler, http.MethodGet, tt.target, "", "")
			body := checkError(t, rec, tt.wantStatus)
			assert.Equal(t, tt.wantDetail, body["detail"], "field detail")
			assert.NotContains(t, body, "errors", "no errors[] here")
			assert.NotContains(t, rec.Body.String(), secretValue, "body leaks error text")
		})
	}
}

// текст неизвестной ошибки - в логе, с request ID запроса.
func TestUnknownErrorLogged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	req := newRequest(t, http.MethodGet, "/test/unknown", "")
	req.Header.Set(requestid.Header, "req-500")
	rec := doRequest(newErrorsTestHandler(&buf), req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.True(t, slices.ContainsFunc(slices.Collect(bytes.Lines(buf.Bytes())), func(line []byte) bool {
		return bytes.Contains(line, []byte(secretValue)) && bytes.Contains(line, []byte(`"request_id":"req-500"`))
	}), "no log line with error text and request_id, log:\n%s", buf.String())
}
