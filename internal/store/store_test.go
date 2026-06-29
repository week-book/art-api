package store

import (
	"os"
	"path/filepath"
	"testing"
)

const testData = `[
  {
    "uuid": "11111111-1111-1111-1111-111111111111",
    "filename": "a.jpg",
    "artist_name": "Artist A",
    "year": "1900",
    "style": "Symbolism",
    "mood": "calm",
    "museum": "State Tretyakov Gallery",
    "tags": ["forest", "river"],
    "added_date": "2026-06-01"
  },
  {
    "uuid": "22222222-2222-2222-2222-222222222222",
    "filename": "b.jpg",
    "artist_name": "Artist B",
    "year": "1910",
    "style": "Realism",
    "mood": "dark",
    "museum": "",
    "tags": ["portrait"],
    "added_date": "2026-06-02"
  },
  {
    "uuid": "33333333-3333-3333-3333-333333333333",
    "filename": "c.jpg",
    "artist_name": "Artist A",
    "year": "1920",
    "style": "Symbolism",
    "mood": "joyful",
    "museum": "",
    "tags": ["river"],
    "added_date": "2026-06-03"
  }
]`

func newLoadedStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "artworks.json")
	if err := os.WriteFile(path, []byte(testData), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	s := New()
	if err := s.Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return s
}

func TestLoad_Ready(t *testing.T) {
	s := newLoadedStore(t)
	if !s.Ready() {
		t.Error("expected Ready() = true after successful Load")
	}
	if got := s.Count(); got != 3 {
		t.Errorf("Count() = %d, want 3", got)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	s := New()
	err := s.Load("/nonexistent/path/artworks.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if s.Ready() {
		t.Error("expected Ready() = false after failed Load")
	}
	if s.LoadError() == nil {
		t.Error("expected LoadError() to be non-nil after failed Load")
	}
}

func TestList_NoFilter(t *testing.T) {
	s := newLoadedStore(t)
	got := s.List(ListFilter{})
	if len(got) != 3 {
		t.Errorf("List(no filter) returned %d items, want 3", len(got))
	}
	// порядок должен быть стабильным (по added_date)
	if got[0].UUID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("List() first item = %s, want stable order by added_date", got[0].UUID)
	}
}

func TestList_FilterByStyle(t *testing.T) {
	s := newLoadedStore(t)
	got := s.List(ListFilter{Style: "Symbolism"})
	if len(got) != 2 {
		t.Errorf("List(style=Symbolism) returned %d items, want 2", len(got))
	}
}

func TestList_FilterByArtistAndMuseum_NoMatch(t *testing.T) {
	s := newLoadedStore(t)
	got := s.List(ListFilter{Museum: "Garage Museum of Contemporary Art"})
	if len(got) != 0 {
		t.Errorf("List(museum=no-match) returned %d items, want 0", len(got))
	}
}

func TestList_Pagination(t *testing.T) {
	s := newLoadedStore(t)

	got := s.List(ListFilter{Limit: 1})
	if len(got) != 1 {
		t.Fatalf("List(limit=1) returned %d items, want 1", len(got))
	}

	got = s.List(ListFilter{Offset: 2})
	if len(got) != 1 {
		t.Fatalf("List(offset=2) returned %d items, want 1", len(got))
	}

	got = s.List(ListFilter{Offset: 10})
	if len(got) != 0 {
		t.Fatalf("List(offset=10) returned %d items, want 0 (offset beyond range)", len(got))
	}
}

func TestGetByUUID(t *testing.T) {
	s := newLoadedStore(t)

	a, ok := s.GetByUUID("11111111-1111-1111-1111-111111111111")
	if !ok {
		t.Fatal("GetByUUID() ok = false for existing UUID")
	}
	if a.ArtistName != "Artist A" {
		t.Errorf("GetByUUID() ArtistName = %q, want Artist A", a.ArtistName)
	}

	_, ok = s.GetByUUID("does-not-exist")
	if ok {
		t.Error("GetByUUID() ok = true for nonexistent UUID, want false")
	}
}

func TestRandom_EmptyFilterNoMatch(t *testing.T) {
	s := newLoadedStore(t)

	_, ok := s.Random("NoSuchStyle")
	if ok {
		t.Error("Random(style with no matches) ok = true, want false")
	}
}

func TestRandom_NoStyleAlwaysReturns(t *testing.T) {
	s := newLoadedStore(t)

	for i := 0; i < 10; i++ {
		_, ok := s.Random("")
		if !ok {
			t.Fatal("Random(\"\") ok = false, want true (archive is non-empty)")
		}
	}
}

func TestArtists_GroupsAndCounts(t *testing.T) {
	s := newLoadedStore(t)

	artists := s.Artists()
	if len(artists) != 2 {
		t.Fatalf("Artists() returned %d artists, want 2", len(artists))
	}

	counts := make(map[string]int)
	for _, a := range artists {
		counts[a.Name] = a.Count
	}
	if counts["Artist A"] != 2 {
		t.Errorf("Artist A count = %d, want 2", counts["Artist A"])
	}
	if counts["Artist B"] != 1 {
		t.Errorf("Artist B count = %d, want 1", counts["Artist B"])
	}
}

func TestTaxonomy_EmptyValuesExcluded(t *testing.T) {
	s := newLoadedStore(t)

	tax := s.Taxonomy()

	if len(tax.Style) != 2 {
		t.Errorf("Taxonomy().Style = %v, want 2 unique styles", tax.Style)
	}
	if len(tax.Museum) != 1 {
		t.Errorf("Taxonomy().Museum = %v, want 1 (empty museum strings excluded)", tax.Museum)
	}
	if len(tax.Tags) != 3 {
		t.Errorf("Taxonomy().Tags = %v, want 3 unique tags (forest, river, portrait)", tax.Tags)
	}
}
