package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics держит все Prometheus-коллекторы проекта.
// Мидлвара регистрируется один раз в main.go и оборачивает роутер целиком
// (см. architecture.md "Метрики (Prometheus)").
type Metrics struct {
	registry         *prometheus.Registry
	requestsTotal    *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	artworksServed   *prometheus.CounterVec
	artworksNotFound prometheus.Counter
}

// New регистрирует все коллекторы в переданном реестре и возвращает Metrics.
//
// ВАЖНО: Handler() отдаёт метрики из этого же reg, а не из глобального
// prometheus.DefaultRegisterer — иначе promhttp.Handler() показывал бы
// только встроенные go_*/process_* метрики и не видел бы наши кастомные
// counters/histograms, зарегистрированные здесь.
func New(reg *prometheus.Registry) *Metrics {
	factory := promauto.With(reg)

	return &Metrics{
		registry: reg,
		requestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests.",
			},
			[]string{"path", "method", "status"},
		),
		requestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "Latency of HTTP requests.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"path", "method"},
		),
		artworksServed: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "artworks_served_total",
				Help: "Number of artworks served, broken down by style.",
			},
			[]string{"style"},
		),
		artworksNotFound: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "artworks_not_found_total",
				Help: "Number of 404s on /artworks/{id}.",
			},
		),
	}
}

// Handler возвращает HTTP-хендлер для /metrics, отдающий метрики из реестра
// переданного в New (не глобальный prometheus.DefaultGatherer).
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// ObserveArtworkServed увеличивает счётчик отданных работ для конкретного style.
// Пустой style (работа без указанного стиля) учитывается как отдельная метка "".
func (m *Metrics) ObserveArtworkServed(style string) {
	m.artworksServed.WithLabelValues(style).Inc()
}

// ObserveArtworkNotFound увеличивает счётчик 404 на /artworks/{id}.
func (m *Metrics) ObserveArtworkNotFound() {
	m.artworksNotFound.Inc()
}

// Middleware оборачивает хендлер базовыми HTTP-метриками
// (http_requests_total, http_request_duration_seconds).
//
// pathLabel передаётся явно (а не берётся из r.URL.Path) чтобы избежать
// взрыва кардинальности на путях с переменной частью, например
// /artworks/{id} — там подставляется шаблон маршрута, не конкретный UUID.
func (m *Metrics) Middleware(pathLabel string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next(rec, r)

		duration := time.Since(start).Seconds()
		m.requestsTotal.WithLabelValues(pathLabel, r.Method, strconv.Itoa(rec.status)).Inc()
		m.requestDuration.WithLabelValues(pathLabel, r.Method).Observe(duration)
	}
}

// statusRecorder перехватывает статус-код ответа для метрик.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
