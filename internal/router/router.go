// Package router собирает http.ServeMux со всеми эндпоинтами art-api.
//
// Вынесено из main.go в отдельный пакет с одной целью: чтобы HTTP-тесты
// (internal/router/router_test.go) проверяли тот же самый роутинг, который
// реально поднимается в проде — включая порядок регистрации
// /artworks/random относительно /artworks/{id}. Если тесты собирали бы
// свой mux вручную, регрессия в main.go могла бы пройти незамеченной.
package router

import (
	"net/http"

	"github.com/week-book/apikeys"

	"github.com/week-book/art-api/internal/handlers"
	"github.com/week-book/art-api/internal/metrics"
	"github.com/week-book/art-api/internal/store"
)

// New собирает http.ServeMux со всеми инфраструктурными и функциональными
// эндпоинтами art-api (см. architecture.md).
//
// authMW авторизует функциональные эндпоинты (/artworks*, /artists,
// /taxonomy) — см. "Auth" в architecture.md: статические API-ключи,
// выдаются вручную через cmd/artkeys, без публичного self-service
// эндпоинта. Инфраструктурные эндпоинты (/healthz, /readyz, /metrics)
// сознательно остаются без авторизации — k8s liveness/readiness-пробы и
// Prometheus scrape не должны зависеть от наличия ключа, это усложнило бы
// деплой без реальной пользы для безопасности (эти эндпоинты не отдают
// данные архива).
func New(s *store.Store, m *metrics.Metrics, authMW *apikeys.Middleware) *http.ServeMux {
	health := handlers.NewHealth(s)
	artworks := handlers.NewArtworks(s, m)
	artists := handlers.NewArtists(s)
	taxonomy := handlers.NewTaxonomy(s)

	mux := http.NewServeMux()

	// Инфраструктурные эндпоинты — без метрик-мидлвары и без auth, чтобы не
	// учитывать собственный health-check трафик в http_requests_total, не
	// плодить рекурсию в /metrics, и не блокировать k8s/Prometheus отсутствием
	// ключа.
	mux.HandleFunc("GET /healthz", health.Healthz)
	mux.HandleFunc("GET /readyz", health.Readyz)
	mux.Handle("GET /metrics", m.Handler())

	// Функциональные эндпоинты — через мидлвару метрик и затем auth.
	// Порядок оборачивания: authMW.Require снаружи metrics.Middleware,
	// то есть запрос сначала аутентифицируется, и только успешные (или явно
	// отклонённые с 401, что тоже фиксируется метрикой) доходят до счётчиков
	// — иначе нагрузочный спам без ключа исказил бы http_requests_total так,
	// будто это легитимный трафик к данным.
	//
	// /artworks/random регистрируется до /artworks/{id} для читаемости;
	// Go 1.22 ServeMux отдаёт приоритет литералу над wildcard независимо
	// от порядка регистрации — см. router_test.go, который проверяет это
	// напрямую, а не полагается на комментарий.
	mux.HandleFunc("GET /artworks", authMW.Require(m.Middleware("/artworks", artworks.List)))
	mux.HandleFunc("GET /artworks/random", authMW.Require(m.Middleware("/artworks/random", artworks.Random)))
	mux.HandleFunc("GET /artworks/{id}", authMW.Require(m.Middleware("/artworks/{id}", artworks.GetByID)))
	mux.HandleFunc("GET /artists", authMW.Require(m.Middleware("/artists", artists.List)))
	mux.HandleFunc("GET /taxonomy", authMW.Require(m.Middleware("/taxonomy", taxonomy.Get)))

	return mux
}
