package handlers

import (
	"net/http"

	"github.com/week-book/art-api/internal/store"
)

// Artists держит зависимости, нужные эндпоинту /artists.
type Artists struct {
	store *store.Store
}

func NewArtists(s *store.Store) *Artists {
	return &Artists{store: s}
}

// List — GET /artists
// Возвращает список художников с количеством работ у каждого.
func (a *Artists) List(w http.ResponseWriter, r *http.Request) {
	artists := a.store.Artists()

	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(artists),
		"artists": artists,
	})
}
