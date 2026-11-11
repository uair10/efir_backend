package middleware

import (
	"encoding/json"
	"net/http"
)

// errorBody - поля huma.ErrorModel, которые нужны ответам вне Huma.
type errorBody struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// WriteError пишет ошибку в стандартной модели huma (application/problem+json).
// Нужен для ответов, которые рождаются вне операций Huma: 404 и 405 от самого
// http.ServeMux и 500 после паники. Ошибки операций Huma пишет сама.
func WriteError(w http.ResponseWriter, status int, detail string) {
	b, err := json.Marshal(errorBody{Title: http.StatusText(status), Status: status, Detail: detail})
	if err != nil {
		// Не случится: в errorBody только строки и число. Но и тогда клиент получит статус.
		http.Error(w, http.StatusText(status), status)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Del("Content-Length")
	w.WriteHeader(status)
	// Клиент мог уйти: ошибку записи всё равно некуда сообщить.
	_, _ = w.Write(append(b, '\n'))
}
