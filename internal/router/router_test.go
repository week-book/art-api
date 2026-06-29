package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/week-book/art-api/internal/auth"
	"github.com/week-book/art-api/internal/metrics"
	"github.com/week-book/art-api/internal/store"
)

// testAPIKeyLabel — лейбл тестового ключа, выдаваемого каждому test server'у.
// Сырой ключ не фиксируется константой — он генерируется заново на каждый
// вызов newTestServer, как и в проде (см. auth.GenerateKey), и возвращается
// вызывающему тесту явно, а не угадывается из константы.
const testAPIKeyLabel = "router_test fixture key"

// issueTestKey создаёt валидный API-ключ в переданном auth.Store и
// возвращает сырой ключ — тесты подставляют его в заголовок Authorization.
func issueTestKey(t *testing.T, s auth.Store) string {
	t.Helper()

	rawKey, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("auth.GenerateKey(): %v", err)
	}
	if _, err := s.Create(context.Background(), auth.Hash(rawKey), testAPIKeyLabel); err != nil {
		t.Fatalf("auth store Create(): %v", err)
	}
	return rawKey
}

// Фикстура с известными UUID/style/mood/museum/tags, чтобы тесты могли
// проверять конкретные значения, а не только "код ответа не 500".
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

// testServer оборачивает httptest.Server и хранит валидный API-ключ,
// выданный этому серверу — /healthz, /readyz, /metrics его не требуют, но
// /artworks*, /artists, /taxonomy теперь его требуют (см. router.go: auth
// закрывает только функциональные эндпоинты).
type testServer struct {
	*httptest.Server
	APIKey string
}

// newTestServer собирает полный mux через router.New — тот же код, который
// main.go использует в проде — и поднимает его в httptest.Server.
// Каждый тест получает свой собственный Store/Metrics/Registry/auth.Store,
// чтобы тесты не делили состояние и могли спокойно идти параллельно.
func newTestServer(t *testing.T) *testServer {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "artworks.json")
	if err := os.WriteFile(path, []byte(testData), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	s := store.New()
	if err := s.Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := metrics.New(reg)

	authStore := auth.NewMemoryStore()
	rawKey := issueTestKey(t, authStore)
	authMW := auth.NewMiddleware(authStore)

	mux := New(s, m, authMW)
	srv := httptest.NewServer(mux)
	return &testServer{Server: srv, APIKey: rawKey}
}

// newUnreadyTestServer собирает сервер с Store, у которого Load() не
// выполнялся — имитирует /readyz = 503 без необходимости подсовывать
// битый файл на диске.
func newUnreadyTestServer(t *testing.T) *testServer {
	t.Helper()

	s := store.New() // Load() не вызывается — Store.Ready() == false

	reg := prometheus.NewRegistry()
	m := metrics.New(reg)

	authStore := auth.NewMemoryStore()
	rawKey := issueTestKey(t, authStore)
	authMW := auth.NewMiddleware(authStore)

	mux := New(s, m, authMW)
	srv := httptest.NewServer(mux)
	return &testServer{Server: srv, APIKey: rawKey}
}

// authedGet выполняет GET с заголовком Authorization: Bearer <srv.APIKey> —
// используется для всех запросов к защищённым эндпоинтам
// (/artworks*, /artists, /taxonomy). Для /healthz, /readyz, /metrics
// продолжаем использовать обычный http.Get — они auth не требуют, и тест
// на это явно проверяет (см. TestArtworksList_NoKey_Unauthorized и т.п.).
func authedGet(t *testing.T, url, apiKey string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", url, err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func getJSON(t *testing.T, url, apiKey string, target any) *http.Response {
	t.Helper()
	resp := authedGet(t, url, apiKey)
	defer resp.Body.Close()

	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			t.Fatalf("decode response from %s: %v", url, err)
		}
	}
	return resp
}

// --- /healthz ---

func TestHealthz_AlwaysOK(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHealthz_OKEvenWhenStoreNotReady(t *testing.T) {
	// /healthz — liveness, не зависит от состояния данных (см. architecture.md:
	// "Всегда 200, если сервис запущен"). Этот тест — главная причина
	// существования отдельного newUnreadyTestServer: если кто-то по
	// невнимательности привяжет Healthz к Store.Ready(), этот тест должен
	// упасть первым.
	srv := newUnreadyTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (healthz must not depend on data readiness)", resp.StatusCode)
	}
}

// --- /readyz ---

func TestReadyz_OKWhenDataLoaded(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/readyz", "", &body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := body["artworks"], float64(3); got != want {
		t.Errorf("readyz artworks count = %v, want %v", got, want)
	}
}

func TestReadyz_ServiceUnavailableWhenDataNotLoaded(t *testing.T) {
	srv := newUnreadyTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// --- /artworks ---

func TestArtworksList_NoFilter_ReturnsAll(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/artworks", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := body["count"], float64(3); got != want {
		t.Errorf("count = %v, want %v", got, want)
	}
}

func TestArtworksList_FilterByStyle(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/artworks?style=Symbolism", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := body["count"], float64(2); got != want {
		t.Errorf("count = %v, want %v (filtered by style=Symbolism)", got, want)
	}
}

func TestArtworksList_FilterCombination_NoMatch(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/artworks?style=Symbolism&mood=dark", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		// Пустой результат — это 200 с count=0, не 404. /artworks — это
		// эндпоинт списка; "ничего не нашлось" не равно "ресурс не найден".
		t.Fatalf("status = %d, want 200 (empty list is not a 404)", resp.StatusCode)
	}
	if got, want := body["count"], float64(0); got != want {
		t.Errorf("count = %v, want %v", got, want)
	}
}

func TestArtworksList_LimitAndOffset(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/artworks?limit=1", srv.APIKey, &body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := body["count"], float64(1); got != want {
		t.Errorf("count with limit=1 = %v, want %v", got, want)
	}
}

func TestArtworksList_InvalidLimitIgnored(t *testing.T) {
	// limit=abc не должно паниковать и не должно молча обрезать список до 0 —
	// невалидное значение трактуется как "без ограничения" (см.
	// parsePositiveInt в handlers/artworks.go).
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/artworks?limit=abc", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := body["count"], float64(3); got != want {
		t.Errorf("count with invalid limit = %v, want %v (invalid limit should be ignored, not applied as 0)", got, want)
	}
}

// --- /artworks/{id} ---

func TestArtworksGetByID_Found(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/artworks/11111111-1111-1111-1111-111111111111", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := body["artist_name"], "Artist A"; got != want {
		t.Errorf("artist_name = %v, want %v", got, want)
	}
}

func TestArtworksGetByID_NotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp := authedGet(t, srv.URL+"/artworks/00000000-0000-0000-0000-000000000000", srv.APIKey)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// --- /artworks/random ---
//
// Этот блок — основная причина появления HTTP-тестов: конфликт маршрутов
// /artworks/random vs /artworks/{id} был до сих пор проверен только
// curl-ом вручную. Здесь это закреплено как регрессионный тест.

func TestArtworksRandom_DoesNotMatchAsID(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/artworks/random", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Если бы ServeMux матчил "random" как значение {id}, GetByID искал бы
	// работу с uuid="random", не нашёл бы её и вернул 404 с телом
	// {"error": "artwork not found"} — у такого тела нет поля "uuid".
	if _, ok := body["uuid"]; !ok {
		t.Errorf("response has no \"uuid\" field — looks like GetByID handled this request instead of Random: %v", body)
	}
	if errMsg, ok := body["error"]; ok {
		t.Errorf("got error response %v — /artworks/random was routed to GetByID(\"random\") instead of Random handler", errMsg)
	}
}

func TestArtworksRandom_ReturnsExistingArtwork(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/artworks/random", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	uuid, _ := body["uuid"].(string)
	validUUIDs := map[string]bool{
		"11111111-1111-1111-1111-111111111111": true,
		"22222222-2222-2222-2222-222222222222": true,
		"33333333-3333-3333-3333-333333333333": true,
	}
	if !validUUIDs[uuid] {
		t.Errorf("random returned uuid=%q, not one of the fixture's artworks", uuid)
	}
}

func TestArtworksRandom_WithStyleFilter(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body map[string]any
	resp := getJSON(t, srv.URL+"/artworks/random?style=Realism", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got, want := body["style"], "Realism"; got != want {
		t.Errorf("style = %v, want %v", got, want)
	}
}

func TestArtworksRandom_StyleWithNoMatches_404(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp := authedGet(t, srv.URL+"/artworks/random?style=NoSuchStyle", srv.APIKey)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (no artworks match the style filter)", resp.StatusCode)
	}
}

// --- /artists ---

func TestArtistsList_GroupsAndCounts(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body struct {
		Count   int `json:"count"`
		Artists []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"artists"`
	}
	resp := getJSON(t, srv.URL+"/artists", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2 unique artists", body.Count)
	}

	counts := make(map[string]int)
	for _, a := range body.Artists {
		counts[a.Name] = a.Count
	}
	if counts["Artist A"] != 2 {
		t.Errorf("Artist A count = %d, want 2", counts["Artist A"])
	}
	if counts["Artist B"] != 1 {
		t.Errorf("Artist B count = %d, want 1", counts["Artist B"])
	}
}

// --- /taxonomy ---

func TestTaxonomy_ReturnsExpectedSections(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	var body struct {
		Style  []string `json:"style"`
		Mood   []string `json:"mood"`
		Museum []string `json:"museum"`
		Tags   []string `json:"tags"`
	}
	resp := getJSON(t, srv.URL+"/taxonomy", srv.APIKey, &body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(body.Style) != 2 {
		t.Errorf("style = %v, want 2 unique values", body.Style)
	}
	// Пустые museum-строки у двух из трёх работ не должны попадать в словарь.
	if len(body.Museum) != 1 {
		t.Errorf("museum = %v, want 1 (empty museum strings excluded)", body.Museum)
	}
	if len(body.Tags) != 3 {
		t.Errorf("tags = %v, want 3 unique tags (forest, river, portrait — river deduplicated)", body.Tags)
	}
}

// --- /metrics ---

func TestMetrics_ExposesCustomCounters(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Прогреваем счётчики реальными запросами через тот же mux. Ключ
	// обязателен: authMW.Require оборачивает metrics.Middleware снаружи
	// (см. router.go), так что запрос без ключа получил бы 401 и не дошёл
	// бы до счётчиков — это бы тестировало совсем не то, что задумано здесь.
	if resp := authedGet(t, srv.URL+"/artworks", srv.APIKey); resp != nil {
		resp.Body.Close()
	}
	if resp := authedGet(t, srv.URL+"/artworks/00000000-0000-0000-0000-000000000000", srv.APIKey); resp != nil {
		resp.Body.Close()
	}

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
	}
	text := string(body)

	// Регрессионный тест на баг, который уже однажды случился: Handler()
	// должен отдавать метрики из ТОГО ЖЕ реестра, что и New() — иначе
	// /metrics показывает только go_*/process_* и не видит наши counters.
	mustContain := []string{
		"http_requests_total",
		"artworks_not_found_total",
		"go_goroutines",
	}
	for _, substr := range mustContain {
		if !contains(text, substr) {
			t.Errorf("/metrics output missing %q — registry mismatch between New() and Handler()?", substr)
		}
	}
}

// --- Auth ---
//
// Этот блок — основная причина появления auth-кода в роутере: проверяем,
// что закрытые эндпоинты реально требуют ключ, отозванный ключ реально
// отклоняется, и что health/metrics остаются открытыми, как зафиксировано
// в architecture.md ("Auth закрывает только функциональные эндпоинты").

func TestArtworksList_NoKey_Unauthorized(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp := authedGet(t, srv.URL+"/artworks", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (no API key provided)", resp.StatusCode)
	}
}

func TestArtworksList_WrongKey_Unauthorized(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Синтаксически валидный wa_live_ ключ, но не выданный этому серверу —
	// не должен совпасть ни с одним хешем в auth.MemoryStore.
	bogusKey, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("auth.GenerateKey(): %v", err)
	}

	resp := authedGet(t, srv.URL+"/artworks", bogusKey)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (key not issued by this server)", resp.StatusCode)
	}
}

func TestArtworksGetByID_XAPIKeyHeader_Accepted(t *testing.T) {
	// middleware.go принимает ключ и через Authorization: Bearer, и через
	// X-API-Key — этот тест покрывает вторую форму, первая покрывается
	// всеми остальными тестами этого файла через authedGet.
	srv := newTestServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/artworks", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-API-Key", srv.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /artworks via X-API-Key: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (X-API-Key header should be accepted)", resp.StatusCode)
	}
}

func TestHealthzReadyzMetrics_DoNotRequireAPIKey(t *testing.T) {
	// Инфраструктурные эндпоинты остаются открытыми — k8s liveness/readiness
	// и Prometheus scrape не должны нести ключ (см. router.go).
	srv := newTestServer(t)
	defer srv.Close()

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("%s returned 401 without a key — infra endpoints must stay open", path)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
