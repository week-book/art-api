package auth

import "testing"

func TestGenerateKey_HasExpectedPrefix(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	if len(key) <= len(KeyPrefix) {
		t.Fatalf("key %q is not longer than its own prefix", key)
	}
	if got := key[:len(KeyPrefix)]; got != KeyPrefix {
		t.Errorf("key prefix = %q, want %q", got, KeyPrefix)
	}
}

func TestGenerateKey_IsRandomAcrossCalls(t *testing.T) {
	a, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	b, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	if a == b {
		t.Errorf("two calls to GenerateKey() produced the same key — randomness source broken")
	}
}

func TestHash_IsDeterministic(t *testing.T) {
	// Тот же ключ должен всегда хешироваться в то же значение — это
	// фундаментальное требование для Verify(): middleware хеширует входящий
	// ключ и ищет совпадение с тем, что записал Create() при выдаче.
	key := "wa_live_example"
	if Hash(key) != Hash(key) {
		t.Error("Hash() is not deterministic for the same input")
	}
}

func TestHash_DiffersForDifferentKeys(t *testing.T) {
	if Hash("wa_live_aaa") == Hash("wa_live_bbb") {
		t.Error("Hash() produced the same output for different inputs — collision or bug")
	}
}

func TestHash_NeverEqualsRawKey(t *testing.T) {
	// Регрессионный тест на самую опасную возможную ошибку в этом пакете:
	// если Hash когда-нибудь начнёт возвращать вход как есть (no-op), это
	// означало бы хранение ключей в БД в открытом виде — прямое нарушение
	// принципа из architecture.md "только SHA-256 хеш, никогда сам ключ".
	key := "wa_live_should_not_be_stored_as_is"
	if Hash(key) == key {
		t.Fatal("Hash(key) == key — hashing is not happening, raw key would be stored")
	}
}
