// Package auth реализует статические API-ключи для art-api.
//
// Архитектурное решение (см. architecture.md "Auth"): статические ключи,
// не OAuth/sessions — соответствует масштабу (read-only данные, нет
// пользовательских аккаунтов, выдача только вручную через cmd/artkeys, без
// публичного self-service эндпоинта — это решение от 29.06.2026, отличается
// от более раннего плана "свободная самовыдача без подтверждения").
//
// Хранится только SHA-256 хеш ключа, никогда сам ключ — см. Hash.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// KeyPrefix — префикс всех выданных ключей. Видимый префикс в логах/конфигах
// сразу говорит "это API-ключ art-api", не требуя расшифровки содержимого
// (тот же приём что у Stripe sk_live_, GitHub ghp_ и т.д.).
const KeyPrefix = "wa_live_"

// keyRandomBytes — количество случайных байт энтропии в самом ключе
// (после префикса). 32 байта = 256 бит — избыточно для офлайн-брутфорса,
// выбрано с запасом, а не подогнано под минимум.
const keyRandomBytes = 32

// GenerateKey создаёт новый случайный API-ключ вида wa_live_<64 hex chars>.
// Возвращает сырой ключ — единственный момент, когда он существует в открытом
// виде в памяти процесса. Вызывающий код (cmd/artkeys) должен напечатать его
// один раз и не хранить в переменных дольше необходимого.
func GenerateKey() (string, error) {
	buf := make([]byte, keyRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return KeyPrefix + hex.EncodeToString(buf), nil
}

// Hash возвращает SHA-256 хеш ключа в виде hex-строки — то, что реально
// хранится в столбце api_keys.key_hash. Чистая функция без побочных
// эффектов: используется и при создании ключа (artkeys create), и при
// каждой проверке входящего запроса (middleware), чтобы гарантировать что
// обе стороны хешируют одинаково.
func Hash(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}
