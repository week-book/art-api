# Changelog

Все заметные изменения в `art-api` фиксируются здесь.

Формат вольно следует [Keep a Changelog](https://keepachangelog.com/), но
без строгого обязательства по [SemVer](https://semver.org/) — это личный
проект одного человека, версии отражают завершённые спринты, а не
контракт обратной совместимости для внешних потребителей (пока нет
внешних потребителей, кроме самого автора и, в будущем, MCP-сервера —
см. `architecture.md`).

---

## [0.2.0] — 2026-06-29 — Auth + Docker (ART-SPRINT-07)

### Добавлено

- **API-ключи** — статическая авторизация по образцу [Art Institute of Chicago API](https://api.artic.edu/docs/):
  - формат ключа `wa_live_<64 hex chars>`, хранится только SHA-256 хеш
    (`internal/auth/apikey.go`)
  - `internal/auth.Store` — интерфейс хранения, backend-агностичный;
    `PostgresStore` — реализация на `pgx/v5`, без ORM
  - `internal/auth.Middleware` — проверка `Authorization: Bearer <key>` или
    `X-API-Key: <key>`, асинхронное обновление `last_used_at`
  - закрыты эндпоинты: `/artworks`, `/artworks/{id}`, `/artworks/random`,
    `/artists`, `/taxonomy`. **Не закрыты**: `/healthz`, `/readyz`, `/metrics` —
    остаются открытыми для k8s-проб и Prometheus scrape
- **`cmd/artkeys`** — CLI для ручной выдачи/просмотра/отзыва ключей
  (`create --label`, `list`, `revoke <id>`). Сознательно нет HTTP-эндпоинта
  для self-service выдачи — кому давать доступ решается вручную
- **Миграция** `migrations/0001_create_api_keys.{up,down}.sql` — таблица
  `api_keys` (`id`, `key_hash`, `label`, `created_at`, `last_used_at`,
  `revoked_at`), `golang-migrate`-совместимая
- **Docker** — `Dockerfile` (multi-stage, `distroless/static-debian12:nonroot`,
  собирает оба бинарника — `art-api` и `artkeys`); `docker-compose.yml`
  (`postgres` → `migrate` (одноразовый) → `art-api`; `artkeys` через
  `--profile tools`, не стартует автоматически)
- **`.env.example`** — документирует переменные окружения локального стенда

### Изменено

- `router.New(...)` теперь принимает четвёртый параметр `authMW *auth.Middleware`
  — точечное расширение сигнатуры, не переписывание роутинга
- `cmd/art-api/main.go` — добавлено обязательное `DATABASE_URL` (через env,
  не флаг — credentials не должны попадать в аргументы процесса)

### Архитектурные решения, зафиксированные в этом релизе

- **Не Telegram-логин.** Рассматривался вариант с привязкой `tg_id ↔ api_key`,
  отклонён: вводит ненужную сущность "пользователь" в read-only каталог без
  аккаунтов, добавляет внешнюю зависимость (Telegram Bot API) в критичный
  путь авторизации
- **Не self-service эндпоинт.** Более ранний план ("свободная самовыдача без
  подтверждения", см. `architecture.md`) уточнён: выдача ключей — это
  решение автора "кому дать доступ", не публичная форма. `POST /keys` не
  существует
- **Свой Postgres, не `week-infra`.** Платформенный Postgres в `week-infra`
  — заготовка без реальных схем (см. `platform-context.md`); auth-трек
  использует собственный локальный/задеплоенный инстанс, чтобы не зависеть
  от подтверждения инфраструктурного проекта на этапе разработки

### Тесты

- `internal/auth` — 12 новых тестов: генерация/детерминированность хеша,
  полный контракт `Store` (create/verify/touch/list/revoke, включая
  идемпотентность revoke и одинаковую ошибку для "не найден" и "отозван")
- `internal/router` — 4 новых теста: 401 без ключа, 401 с чужим ключом,
  `X-API-Key` принимается как альтернатива `Authorization`, инфраструктурные
  эндпоинты не требуют ключ
- Полный прогон: 45/45 теста зелёные (`go test ./...`)

---

## [0.1.0] — 2026-06-24 — MVP API (ART-SPRINT-06)

### Добавлено

- Первая версия Go API по архитектуре из `architecture.md`: `net/http` +
  `http.ServeMux` (паттерны маршрутов Go 1.22), без фреймворка
- **Функциональные эндпоинты** (read-only, источник данных — `artworks.json`
  в памяти, загружается один раз при старте):
  - `GET /artworks` — список с фильтрами `style`, `mood`, `museum`,
    `artist_name`, пагинацией `limit`/`offset`
  - `GET /artworks/{id}` — одна работа по `uuid`
  - `GET /artworks/random` — случайная работа, опционально с фильтром `style`
  - `GET /artists` — список художников с количеством работ
  - `GET /taxonomy` — словарь `style`/`mood`/`museum`/`tags`, агрегированный
    из фактических данных (не из отдельного экспорта листа `taxonomy`)
- **Инфраструктурные эндпоинты**:
  - `GET /healthz` — liveness, всегда 200
  - `GET /readyz` — readiness, 200 если данные загружены, иначе 503
  - `GET /metrics` — Prometheus-формат через собственный `*prometheus.Registry`
- **Метрики**: `http_requests_total`, `http_request_duration_seconds`,
  `artworks_served_total{style}`, `artworks_not_found_total`
- Postman-коллекция (см. `architecture.md`, раздел "JSON для postman")

### Намеренные отклонения от первоначального плана

См. `architecture.md`, раздел "Намеренные отклонения" — кратко:

- первичный ключ в данных — `uuid`, не `id` как в `conventions.md`
- `tags` — `[]string`, не строка через запятую
- `year`/`dimensions` остаются строками, без нормализации (отложено до Фазы 5)

### Тесты

- `internal/store` — 11 тестов: фильтры, edge cases, пагинация
- `internal/router` — HTTP-тесты через `httptest.Server`, включая
  регрессионный тест на конфликт маршрутов `/artworks/random` vs `/artworks/{id}`

---

## Связано

- `roadmap.md` — Фаза 4 (API), переходная стадия Infra Readiness (Auth, Deploy, Database)
- `architecture.md` — источник правды по архитектуре, "Намеренные отклонения", "Планы"
- `platform-context.md` — контракт с инфраструктурной платформой (домены, S3, БД)
