package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrKeyNotFound — хеш не найден среди активных (не отозванных) ключей.
// Middleware и CLI должны трактовать это как "401 / ключ невалиден",
// не как внутреннюю ошибку сервера.
var ErrKeyNotFound = errors.New("api key not found")

// Key — одна запись api_keys, как она читается из БД. Раздельные поля
// вместо переиспользования сырого ключа: RawKey никогда не появляется
// здесь — Store работает только с хешами и метаданными.
type Key struct {
	ID         string
	Label      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// Store — интерфейс хранения API-ключей. Единственное место, которое
// знает про формат хранения (см. architecture.md / store.go для artworks —
// тот же паттерн: когда заменится backend, меняется только реализация
// интерфейса, middleware и CLI остаются как есть).
//
// Сознательно узкий набор методов — ровно то, что нужно middleware
// (Verify, Touch) и CLI (Create, List, Revoke). Не добавлять Update/Search
// и подобное "на будущее" без реальной задачи под это.
type Store interface {
	// Create сохраняет новый ключ по его хешу и возвращает созданную запись.
	Create(ctx context.Context, keyHash, label string) (Key, error)

	// Verify ищет активный (не отозванный) ключ по хешу.
	// Возвращает ErrKeyNotFound если хеш не найден или ключ отозван —
	// middleware не должен различать эти два случая для клиента (оба 401).
	Verify(ctx context.Context, keyHash string) (Key, error)

	// Touch обновляет last_used_at для ключа. Вызывается middleware на
	// каждый успешно авторизованный запрос — см. примечание в middleware.go
	// про асинхронный вызов, чтобы не задерживать ответ клиенту.
	Touch(ctx context.Context, id string) error

	// List возвращает все ключи (включая отозванные) для cmd/artkeys list,
	// отсортированные по created_at.
	List(ctx context.Context) ([]Key, error)

	// Revoke помечает ключ отозванным (выставляет revoked_at = now()).
	// Идемпотентно: повторный Revoke уже отозванного ключа не ошибка.
	Revoke(ctx context.Context, id string) error

	// Close освобождает ресурсы соединения. Вызывать при остановке процесса.
	Close()
}

// PostgresStore — реализация Store на pgx (без ORM, см. tools.md / память
// проекта: "pgx driver, no ORM, golang-migrate для версионированных
// .sql миграций" — миграции лежат в migrations/, эта реализация
// предполагает что они уже применены).
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore открывает пул соединений к Postgres по DSN
// (например "postgres://user:pass@host:5432/dbname?sslmode=disable").
// Не выполняет миграции — это ответственность отдельного шага деплоя
// (см. docker-compose.yml: сервис migrate запускается перед art-api).
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Create(ctx context.Context, keyHash, label string) (Key, error) {
	const q = `
		INSERT INTO api_keys (key_hash, label)
		VALUES ($1, $2)
		RETURNING id, label, created_at, last_used_at, revoked_at`

	var k Key
	row := s.pool.QueryRow(ctx, q, keyHash, label)
	if err := row.Scan(&k.ID, &k.Label, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
		return Key{}, fmt.Errorf("insert api key: %w", err)
	}
	return k, nil
}

func (s *PostgresStore) Verify(ctx context.Context, keyHash string) (Key, error) {
	const q = `
		SELECT id, label, created_at, last_used_at, revoked_at
		FROM api_keys
		WHERE key_hash = $1 AND revoked_at IS NULL`

	var k Key
	row := s.pool.QueryRow(ctx, q, keyHash)
	if err := row.Scan(&k.ID, &k.Label, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Key{}, ErrKeyNotFound
		}
		return Key{}, fmt.Errorf("verify api key: %w", err)
	}
	return k, nil
}

func (s *PostgresStore) Touch(ctx context.Context, id string) error {
	const q = `UPDATE api_keys SET last_used_at = now() WHERE id = $1`
	if _, err := s.pool.Exec(ctx, q, id); err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}

func (s *PostgresStore) List(ctx context.Context) ([]Key, error) {
	const q = `
		SELECT id, label, created_at, last_used_at, revoked_at
		FROM api_keys
		ORDER BY created_at`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []Key
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.ID, &k.Label, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan api key row: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api key rows: %w", err)
	}
	return keys, nil
}

func (s *PostgresStore) Revoke(ctx context.Context, id string) error {
	// COALESCE — повторный revoke не перезатирает исходный revoked_at,
	// поэтому операция идемпотентна без отдельной проверки "уже отозван".
	const q = `UPDATE api_keys SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrKeyNotFound
	}
	return nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}
