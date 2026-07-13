package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/week-book/apikeys"

	"github.com/week-book/art-api/internal/metrics"
	"github.com/week-book/art-api/internal/router"
	"github.com/week-book/art-api/internal/store"
)

const keyPrefix = "wa_live_"

func main() {
	addr := flag.String("addr", ":8080", "адрес для прослушивания, например :8080")
	dataPath := flag.String("data", "data/artworks.json", "путь к artworks.json")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set — required for API key verification (see .env.example)")
	}

	ctx := context.Background()
	authStore, err := apikeys.NewPostgresStore(ctx, dsn)
	if err != nil {
		log.Fatalf("connect to auth database: %v", err)
	}
	defer authStore.Close()
	authMW := apikeys.NewMiddleware(authStore, "art-api")

	s := store.New()
	if err := s.Load(*dataPath); err != nil {
		// Не фатально — /readyz отдаст 503 и это будет видно сразу.
		// Сервис всё равно поднимается, чтобы /healthz отвечал и проблему
		// можно было продиагностировать через readyz/логи, а не по
		// отсутствию ответа вовсе.
		log.Printf("warning: failed to load artworks data: %v", err)
	} else {
		log.Printf("loaded %d artworks from %s", s.Count(), *dataPath)
	}

	reg := prometheus.NewRegistry()
	// promhttp.Handler() по умолчанию использует глобальный реестр и
	// бесплатно добавляет go_*/process_* метрики. Раз мы используем
	// собственный реестр (чтобы Handler() видел наши кастомные метрики —
	// см. internal/metrics), эти стандартные коллекторы нужно подключить
	// явно, иначе /metrics показывал бы только domain-метрики без runtime-обвязки.
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := metrics.New(reg)

	mux := router.New(s, m, authMW)

	log.Printf("art-api listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
