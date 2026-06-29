package handlers

import (
	"net/http"

	"github.com/week-book/art-api/internal/store"
)

// Taxonomy держит зависимости, нужные эндпоинту /taxonomy.
type Taxonomy struct {
	store *store.Store
}

func NewTaxonomy(s *store.Store) *Taxonomy {
	return &Taxonomy{store: s}
}

// Get — GET /taxonomy
// Отдаёт текущий словарь, сгруппированный по секциям (style/mood/museum/tags).
//
// ПРИМЕЧАНИЕ (см. store.Taxonomy и architecture.md "Открытые вопросы"):
// на MVP это словарь фактически встречающихся значений в artworks.json,
// а не полный экспорт листа taxonomy из Sheets.
func (t *Taxonomy) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, t.store.Taxonomy())
}
