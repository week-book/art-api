package handlers

import (
	"net/http"
	"strconv"

	"github.com/week-book/art-api/internal/metrics"
	"github.com/week-book/art-api/internal/store"
)

// Artworks держит зависимости, нужные функциональным эндпоинтам артворков.
type Artworks struct {
	store   *store.Store
	metrics *metrics.Metrics
}

func NewArtworks(s *store.Store, m *metrics.Metrics) *Artworks {
	return &Artworks{store: s, metrics: m}
}

// List — GET /artworks
// Query-параметры: style, mood, museum, artist_name, limit, offset.
// Все фильтры опциональны и комбинируются через AND.
func (a *Artworks) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := store.ListFilter{
		Style:      q.Get("style"),
		Mood:       q.Get("mood"),
		Museum:     q.Get("museum"),
		ArtistName: q.Get("artist_name"),
	}

	if limit, err := parsePositiveInt(q.Get("limit")); err == nil {
		filter.Limit = limit
	}
	if offset, err := parsePositiveInt(q.Get("offset")); err == nil {
		filter.Offset = offset
	}

	results := a.store.List(filter)

	for _, art := range results {
		a.metrics.ObserveArtworkServed(art.Style)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":    len(results),
		"artworks": results,
	})
}

// GetByID — GET /artworks/{id}
func (a *Artworks) GetByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	art, ok := a.store.GetByUUID(id)
	if !ok {
		a.metrics.ObserveArtworkNotFound()
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "artwork not found",
		})
		return
	}

	a.metrics.ObserveArtworkServed(art.Style)
	writeJSON(w, http.StatusOK, art)
}

// Random — GET /artworks/random
// Опциональный query-параметр ?style= ограничивает выбор случайной работы
// внутри указанного style.
func (a *Artworks) Random(w http.ResponseWriter, r *http.Request) {
	style := r.URL.Query().Get("style")

	art, ok := a.store.Random(style)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "no artworks match the given filter",
		})
		return
	}

	a.metrics.ObserveArtworkServed(art.Style)
	writeJSON(w, http.StatusOK, art)
}

// parsePositiveInt парсит query-параметр в неотрицательное int.
// Возвращает ошибку для пустой строки или отрицательного числа — в этом
// случае хендлер должен оставить filter.Limit/Offset равным 0 (значение
// "без ограничения"/"без смещения" по умолчанию).
func parsePositiveInt(raw string) (int, error) {
	if raw == "" {
		return 0, strconv.ErrSyntax
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, strconv.ErrSyntax
	}
	return n, nil
}
