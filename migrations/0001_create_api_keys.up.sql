-- api_keys хранит только SHA-256 хеш ключа, никогда сам ключ (см. architecture.md "Auth").
-- pgcrypto нужен только для gen_random_uuid() — если в образе postgres его нет,
-- можно заменить на uuid-ossp или генерировать UUID в Go перед INSERT.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE api_keys (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash      TEXT NOT NULL UNIQUE,
    label         TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ
);

-- Поиск по хешу — путь горячего запроса (каждый авторизованный HTTP-запрос).
-- UNIQUE на key_hash уже создаёт btree-индекс, отдельный CREATE INDEX не нужен.

-- Частичный индекс на активные ключи — ускоряет artkeys list, фильтрующий
-- по revoked_at IS NULL, не обязателен на масштабе "пара ключей", но дешёвый
-- и не требует пересмотра при росте числа ключей.
CREATE INDEX idx_api_keys_active ON api_keys (created_at) WHERE revoked_at IS NULL;
