package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/week-book/art-api/internal/store"
)

// Health держит зависимости, нужные для health-эндпоинтов.
type Health struct {
	store *store.Store
}

func NewHealth(s *store.Store) *Health {
	return &Health{store: s}
}

// Healthz — liveness. Всегда 200, если процесс жив и обрабатывает HTTP.
// Не проверяет состояние данных — для этого есть /readyz.
func (h *Health) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz — readiness. 200 если artworks.json загружен и валиден, иначе 503.
func (h *Health) Readyz(w http.ResponseWriter, r *http.Request) {
	if !h.store.Ready() {
		body := map[string]string{"status": "not ready"}
		if err := h.store.LoadError(); err != nil {
			body["error"] = err.Error()
		}
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"artworks": h.store.Count(),
	})
}

// writeJSON — общий хелпер сериализации ответа, используется во всех handlers.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
