package auth

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
)

// Middleware проверяет API-ключ на входящих запросах. Принимает Store как
// интерфейс — не знает и не должен знать, что за ним сейчас Postgres
// (см. store.go); при смене backend (например, для тестов — in-memory
// реализация Store) этот файл не меняется.
type Middleware struct {
	store Store
}

func NewMiddleware(s Store) *Middleware {
	return &Middleware{store: s}
}

// Require оборачивает хендлер проверкой ключа. Принимает ключ из заголовка
// Authorization: Bearer <key> или X-API-Key: <key> — в этом порядке
// проверки; если оба присутствуют, используется Authorization.
//
// При успехе кладёt auth.Key в контекст запроса (см. KeyFromContext) и
// обновляет last_used_at в отдельной горутине — не на горячем пути ответа,
// чтобы задержка БД не добавлялась к латентности каждого запроса. Это
// осознанный trade-off: last_used_at может на доли секунды отставать от
// реального последнего запроса, что не критично для read-only API-ключей,
// используемых только для подсчёта активности, не для биллинга в реальном
// времени.
func (m *Middleware) Require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawKey := extractKey(r)
		if rawKey == "" {
			writeUnauthorized(w, "missing API key")
			return
		}

		key, err := m.store.Verify(r.Context(), Hash(rawKey))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				writeUnauthorized(w, "invalid or revoked API key")
				return
			}
			// Ошибка инфраструктуры (БД недоступна и т.п.) — не путать
			// с "ключ неверный". Клиенту всё равно нужен ответ, но это
			// 500, не 401, чтобы не маскировать реальную проблему сервиса.
			log.Printf("auth: verify key failed: %v", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		go m.touchAsync(key.ID)

		ctx := context.WithValue(r.Context(), contextKeyAuthKey, key)
		next(w, r.WithContext(ctx))
	}
}

// touchAsync обновляет last_used_at в фоне. Использует context.Background(),
// а не контекст исходного запроса — тот закрывается сразу после ответа
// клиенту, что отменило бы обновление до его завершения.
func (m *Middleware) touchAsync(id string) {
	if err := m.store.Touch(context.Background(), id); err != nil {
		log.Printf("auth: touch key %s failed: %v", id, err)
	}
}

// extractKey достаёт сырой ключ из заголовков запроса, или "" если ключа нет.
func extractKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if rest, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="art-api"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

type contextKey string

const contextKeyAuthKey contextKey = "auth.key"

// KeyFromContext извлекает аутентифицированный Key из контекста запроса,
// если хендлеру за Require нужно знать, каким ключом был сделан запрос
// (например, для более детальных метрик в будущем). Сейчас не используется
// ни одним хендлером — добавлено как точка расширения, не как мёртвый код:
// без неё пришлось бы лезть в этот файл при первой реальной надобности.
func KeyFromContext(ctx context.Context) (Key, bool) {
	key, ok := ctx.Value(contextKeyAuthKey).(Key)
	return key, ok
}
