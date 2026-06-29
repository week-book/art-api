# art-api

Go API для personal art archive. Read-only, метаданные читаются из
`artworks.json` в памяти (см. `architecture.md` в проекте Obsidian для
полной архитектуры). Функциональные эндпоинты защищены статическими
API-ключами; инфраструктурные — открыты.

Текущая версия: **v0.2.0** (Auth + Docker, ART-SPRINT-07). См. `CHANGELOG.md`
для полной истории изменений.

## Быстрый старт

Самый простой способ — через Docker Compose, поднимает Postgres,
прогоняет миграции и стартует сервис:

```bash
cp .env.example .env   # при желании поменять дефолты
docker compose up --build
```

Выдать первый API-ключ (без него `/artworks`, `/artists`, `/taxonomy`
вернут 401):

```bash
docker compose run --rm artkeys create --label "локальный тест"
# → Ключ создан (id=..., label="локальный тест").
#   Сохрани его — он больше никогда не будет показан:
#   wa_live_...
```

Проверить:

```bash
curl http://localhost:8080/healthz
curl -H "Authorization: Bearer wa_live_..." http://localhost:8080/artworks
```

### Без Docker

Требует доступный Postgres и применённые миграции (`migrate -path migrations
-database "$DATABASE_URL" up`, см. [golang-migrate](https://github.com/golang-migrate/migrate)).

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/art_api?sslmode=disable"
go run ./cmd/art-api -addr=:8080 -data=data/artworks.json
go run ./cmd/artkeys create --label "локальный тест"
```

Флаги `art-api`:
- `-addr` — адрес для прослушивания (по умолчанию `:8080`)
- `-data` — путь к `artworks.json` (по умолчанию `data/artworks.json`)

`DATABASE_URL` — обязательная переменная окружения, не флаг (credentials не
должны попадать в аргументы процесса, видные в `ps`/логах).

## Auth

Статические API-ключи, не OAuth/sessions — соответствует масштабу
(read-only данные, нет пользовательских аккаунтов). Подробное архитектурное
решение и почему отклонён вариант с Telegram-логином — в `CHANGELOG.md`
(v0.2.0) и `architecture.md`.

- **Формат ключа:** `wa_live_<64 hex chars>`. Хранится только SHA-256 хеш —
  сам ключ показывается один раз при создании и не восстановим.
- **Передача:** `Authorization: Bearer <key>` или `X-API-Key: <key>`.
- **Выдача:** только вручную, через `cmd/artkeys` (`create`/`list`/`revoke`).
  Нет публичного `POST /keys` эндпоинта — кому давать доступ решается
  человеком, не самообслуживанием.
- **Закрытые эндпоинты:** `/artworks`, `/artworks/{id}`, `/artworks/random`,
  `/artists`, `/taxonomy`.
- **Открытые эндпоинты:** `/healthz`, `/readyz`, `/metrics` — не должны
  зависеть от ключа (k8s-пробы, Prometheus scrape).

### Управление ключами (`artkeys`)

```bash
artkeys create --label "MCP server"   # выдать новый ключ
artkeys list                          # все ключи, включая отозванные
artkeys revoke <id>                   # отозвать по id
```

Требует `DATABASE_URL` в окружении — подключается к той же базе, что и сам
сервис.

## Тесты

```bash
go test ./...          # все тесты
go test ./... -race    # с детектором гонок (store использует мьютексы)
```

45 тестов в трёх пакетах:

- **`internal/store`** (11) — фильтры, пагинация, random с пустым
  результатом, группировка по artists, taxonomy с пустыми значениями.
  Тесты дёргают `Store` напрямую, без HTTP.
- **`internal/auth`** (12) — генерация и детерминированность SHA-256 хеша
  (включая регрессионный тест на то, что хеш никогда не равен исходному
  ключу), полный контракт `Store` через `MemoryStore`: create→verify,
  unknown hash → `ErrKeyNotFound`, revoked key → та же ошибка что unknown
  (middleware не должен различать эти случаи для клиента), идемпотентность
  `Revoke`, `Touch` обновляет `last_used_at`, `List` включает отозванные
  ключи.
- **`internal/router`** (22) — HTTP-тесты через `httptest.Server`,
  поднимающие **тот же** `router.New(...)`, что и `main.go` в проде:
  - `/healthz` всегда 200, даже если данные не загружены
  - `/readyz` 200 при загруженных данных, 503 если `Store.Load()` не
    выполнялся
  - `/artworks`: фильтрация, `limit`/`offset`, невалидный `limit`
    игнорируется (не схлопывает список до нуля)
  - `/artworks/{id}`: найдено / 404
  - `/artworks/random`: **регрессионный тест на конфликт маршрутов** — `random`
    не попадает в `GetByID` как значение `{id}`
  - `/artists`, `/taxonomy`: группировка, дедупликация, пустые значения
    исключены из словаря
  - `/metrics`: кастомные счётчики и стандартные go-метрики из одного реестра
  - **Auth (4):** 401 без ключа, 401 с синтаксически верным но не выданным
    ключом, `X-API-Key` принимается как альтернатива `Authorization`,
    инфраструктурные эндпоинты не требуют ключ ни при каких условиях

Ручная проверка через `curl`/Postman для этих сценариев не обязательна —
весь HTTP-слой, включая auth, проверяется `go test ./...`. Postman-коллекция
(`postman_collection.json` в корне репозитория, либо в `architecture.md`)
остаётся полезной для исследовательского тестирования и smoke-проверки
после деплоя.

## Эндпоинты

### Инфраструктурные — без auth

| Метод | Путь | Описание |
|---|---|---|
| GET | `/healthz` | Всегда 200, если процесс жив |
| GET | `/readyz` | 200 если `artworks.json` загружен, иначе 503 |
| GET | `/metrics` | Prometheus-формат |

### Функциональные — требуют API-ключ

| Метод | Путь | Query-параметры |
|---|---|---|
| GET | `/artworks` | `style`, `mood`, `museum`, `artist_name`, `limit`, `offset` |
| GET | `/artworks/{id}` | — (UUID в пути) |
| GET | `/artworks/random` | `style` (опционально) |
| GET | `/artists` | — |
| GET | `/taxonomy` | — |

## Важные расхождения с `conventions.md` (намеренные)

Реальный `artworks.json` (выход `sheets-export`) отличается от
идеализированной схемы из `conventions.md`. Код написан под то, что
**фактически приходит на вход**, а не под документ:

- Первичный ключ в JSON — `uuid`, не `id`. API использует `uuid` везде
  (включая путь `/artworks/{id}` — туда передаётся значение `uuid`).
- `tags` — это `[]string` в JSON, не строка через запятую.
- `year` и `dimensions` остаются `string` — нормализация в числа/структуры
  отложена до Фазы 5 (Database), как и зафиксировано в `roadmap.md`.
- Пустые `museum`/`dimensions`/`notes` — нормальное явление; `store` и
  handlers их не валидируют как required, просто не включают в `Taxonomy()`.

## `/taxonomy` — агрегация по фактическим данным, не по листу Sheets

`/taxonomy` строит словарь из **фактически встречающихся** значений
`style`/`mood`/`museum`/`tags` в `artworks.json`, а не из отдельного
экспорта листа `taxonomy` в Sheets (такого экспорта не существует в
пайплайне). Если в `taxonomy.md`/Sheets заведено новое значение, ещё не
присвоенное ни одной работе — оно не появится в ответе API, пока хотя бы
одна работа его не получит.

Сознательный компромисс ради простоты — если станет мешать (например,
полный список значений нужен для UI-дропдауна на Фазе 7/Frontend),
естественное решение — добавить экспорт листа `taxonomy` отдельным
`taxonomy.json` и переключить `store.Taxonomy()` читать оттуда.

## Маршрутизация `/artworks/random` vs `/artworks/{id}`

Go 1.22 `http.ServeMux` корректно отдаёт приоритет литералу `/artworks/random`
над wildcard-паттерном `/artworks/{id}`. Закреплено тестом
`TestArtworksRandom_DoesNotMatchAsID`. Регистрация в `router.go` всё равно
держит `random` отдельной строкой до `{id}` для читаемости, а не из
необходимости порядка.

## Структура

```
art-api/
├── cmd/
│   ├── art-api/main.go         # роутинг, запуск сервера, DATABASE_URL
│   └── artkeys/main.go         # CLI: create / list / revoke API-ключей
├── internal/
│   ├── auth/                   # apikey.go, store.go (Postgres), memory_store.go,
│   │                           # middleware.go + тесты
│   ├── handlers/                # artworks.go, artists.go, taxonomy.go, health.go
│   ├── router/                  # router.go (сборка mux) + router_test.go
│   ├── metrics/                  # prometheus-коллекторы + мидлвара
│   ├── store/                    # store.go (artworks.json) + store_test.go
│   └── models/                   # artwork.go
├── migrations/                   # 0001_create_api_keys.{up,down}.sql
├── data/artworks.json             # снимок данных (положить свой перед запуском)
├── Dockerfile                     # multi-stage, distroless, art-api + artkeys
├── docker-compose.yml              # postgres → migrate → art-api; artkeys через --profile tools
├── .env.example
├── postman_collection.json
├── CHANGELOG.md
├── go.mod / go.sum
└── README.md
```

## Зависимости и сеть

`go.mod`:
- `github.com/prometheus/client_golang v1.19.1` — совместима с Go 1.22
- `github.com/jackc/pgx/v5 v5.6.0` — без ORM, совместима с Go 1.22
  (более новые минорные версии `pgx/v5` требуют Go 1.25+)

### Про `replace`-директивы в `go.mod`

Несколько `replace` указывают на GitHub-зеркала вместо `golang.org`/
`gopkg.in`-путей:

```
replace golang.org/x/sys => github.com/golang/sys v0.17.0
replace google.golang.org/protobuf => github.com/protocolbuffers/protobuf-go v1.33.0
replace gopkg.in/yaml.v3 => github.com/go-yaml/yaml v3.0.1+incompatible
replace golang.org/x/crypto => github.com/golang/crypto v0.17.0
replace golang.org/x/text => github.com/golang/text v0.14.0
replace gopkg.in/check.v1 => github.com/go-check/check v0.0.0-20180628173108-788fd7840127
replace golang.org/x/sync => github.com/golang/sync v0.3.0
```

Они были нужны только в среде разработки, где не было прямого доступа к
`proxy.golang.org`/`golang.org`/`gopkg.in` (отдельная сетевая песочница),
поэтому пришлось резолвить модули через их фактические зеркала на GitHub.
Если у тебя есть штатный доступ к Go module proxy — все строки `replace`
можно удалить и пересобрать `go.sum`:

```bash
# убери все replace-строки из go.mod, затем:
go mod tidy
```

Семантически это не меняет ничего — те же версии тех же модулей, просто
получены через стандартный путь, а не через зеркало.

## Деплой

Не реализован в этом релизе. См. `architecture.md`, раздел "Планы" → Деплой
в кластер: k3s + ArgoCD, тот же паттерн что у остальной инфраструктуры
(`s3.week-book.ru`), публичный ingress через ingress-nginx + cert-manager.
**Порядок принципиален:** auth (этот релиз) должен быть в проде раньше, чем
появится публичный ingress — см. `roadmap.md`, "не открывать API публично
без auth — даже read-only".
