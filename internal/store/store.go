package store

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"

	"github.com/week-book/art-api/internal/models"
)

// Store — единственное место, которое знает про формат данных на диске.
// Когда дойдёте до Фазы 5 (PostgreSQL), меняется только этот слой —
// хендлеры и роутинг остаются как есть (см. architecture.md).
type Store struct {
	mu       sync.RWMutex
	artworks []models.Artwork
	byUUID   map[string]*models.Artwork
	loaded   bool
	loadErr  error
}

// New создаёт пустой Store. Данные нужно загрузить через Load.
func New() *Store {
	return &Store{
		byUUID: make(map[string]*models.Artwork),
	}
}

// Load читает artworks.json целиком в память и строит индекс по UUID.
// Вызывается один раз при старте сервиса (см. architecture.md: "Источник
// данных — файл, не база").
func (s *Store) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		s.setLoadErr(err)
		return fmt.Errorf("read %s: %w", path, err)
	}

	var artworks []models.Artwork
	if err := json.Unmarshal(data, &artworks); err != nil {
		s.setLoadErr(err)
		return fmt.Errorf("unmarshal %s: %w", path, err)
	}

	index := make(map[string]*models.Artwork, len(artworks))
	for i := range artworks {
		a := &artworks[i]
		if a.UUID == "" {
			// Не валим весь старт из-за одной кривой записи — но и не
			// делаем вид, что всё в порядке: пропускаем и продолжаем.
			// Видимость в логах достаточна на этом масштабе (без auth,
			// без внешних потребителей кроме самого Данила).
			continue
		}
		index[a.UUID] = a
	}

	s.mu.Lock()
	s.artworks = artworks
	s.byUUID = index
	s.loaded = true
	s.loadErr = nil
	s.mu.Unlock()

	return nil
}

func (s *Store) setLoadErr(err error) {
	s.mu.Lock()
	s.loaded = false
	s.loadErr = err
	s.mu.Unlock()
}

// Ready возвращает true если данные успешно загружены — используется в /readyz.
func (s *Store) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loaded
}

// LoadError возвращает последнюю ошибку загрузки, если она есть.
func (s *Store) LoadError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

// Count возвращает текущее количество работ в архиве.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.artworks)
}

// ListFilter — параметры фильтрации и пагинации для List.
// Пустая строка в Style/Mood/Museum/ArtistName означает "без фильтра по
// этому полю". Limit <= 0 означает "без ограничения" (архив на этом
// масштабе — ~100-1000 записей, см. architecture.md "Открытые вопросы").
type ListFilter struct {
	Style      string
	Mood       string
	Museum     string
	ArtistName string
	Limit      int
	Offset     int
}

// List возвращает работы, прошедшие фильтр, с пагинацией.
// Сортировка — по added_date, затем по UUID, для стабильного порядка
// между запросами (важно для пагинации).
func (s *Store) List(f ListFilter) []models.Artwork {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []models.Artwork
	for _, a := range s.artworks {
		if f.Style != "" && a.Style != f.Style {
			continue
		}
		if f.Mood != "" && a.Mood != f.Mood {
			continue
		}
		if f.Museum != "" && a.Museum != f.Museum {
			continue
		}
		if f.ArtistName != "" && a.ArtistName != f.ArtistName {
			continue
		}
		result = append(result, a)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].AddedDate != result[j].AddedDate {
			return result[i].AddedDate < result[j].AddedDate
		}
		return result[i].UUID < result[j].UUID
	})

	if f.Offset > 0 {
		if f.Offset >= len(result) {
			return []models.Artwork{}
		}
		result = result[f.Offset:]
	}
	if f.Limit > 0 && f.Limit < len(result) {
		result = result[:f.Limit]
	}

	return result
}

// GetByUUID возвращает работу по UUID. Второе значение — найдена ли запись.
func (s *Store) GetByUUID(uuid string) (models.Artwork, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a, ok := s.byUUID[uuid]
	if !ok {
		return models.Artwork{}, false
	}
	return *a, true
}

// Random возвращает случайную работу, опционально отфильтрованную по style.
// Второе значение — false если архив (или фильтр) пуст.
func (s *Store) Random(style string) (models.Artwork, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if style == "" {
		if len(s.artworks) == 0 {
			return models.Artwork{}, false
		}
		return s.artworks[rand.Intn(len(s.artworks))], true
	}

	var filtered []models.Artwork
	for _, a := range s.artworks {
		if a.Style == style {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == 0 {
		return models.Artwork{}, false
	}
	return filtered[rand.Intn(len(filtered))], true
}

// Artists возвращает список художников с количеством работ у каждого,
// отсортированный по имени.
func (s *Store) Artists() []models.Artist {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]int)
	for _, a := range s.artworks {
		if a.ArtistName == "" {
			continue
		}
		counts[a.ArtistName]++
	}

	artists := make([]models.Artist, 0, len(counts))
	for name, count := range counts {
		artists = append(artists, models.Artist{Name: name, Count: count})
	}

	sort.Slice(artists, func(i, j int) bool {
		return artists[i].Name < artists[j].Name
	})

	return artists
}

// Taxonomy строит словарь фактически встречающихся значений style/mood/
// museum/tags из текущих данных.
//
// ПРИМЕЧАНИЕ: architecture.md оставляет открытым вопрос "отдавать как есть
// из листа, или сгруппировать по секциям" — здесь источник правды не лист
// taxonomy (он не экспортируется в artworks.json), а сами артворки. Это
// осознанное упрощение для MVP: словарь покажет то, что реально используется,
// но не то, что потенциально разрешено в taxonomy.md/Sheets, если там есть
// значения, которые ещё не встретились ни у одной работы.
func (s *Store) Taxonomy() models.Taxonomy {
	s.mu.RLock()
	defer s.mu.RUnlock()

	styleSet := make(map[string]struct{})
	moodSet := make(map[string]struct{})
	museumSet := make(map[string]struct{})
	tagSet := make(map[string]struct{})

	for _, a := range s.artworks {
		if a.Style != "" {
			styleSet[a.Style] = struct{}{}
		}
		if a.Mood != "" {
			moodSet[a.Mood] = struct{}{}
		}
		if a.Museum != "" {
			museumSet[a.Museum] = struct{}{}
		}
		for _, t := range a.Tags {
			if t != "" {
				tagSet[t] = struct{}{}
			}
		}
	}

	return models.Taxonomy{
		Style:  sortedKeys(styleSet),
		Mood:   sortedKeys(moodSet),
		Museum: sortedKeys(museumSet),
		Tags:   sortedKeys(tagSet),
	}
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
