package auth

import (
	"context"
	"errors"
	"testing"
)

// Эти тесты гоняются против MemoryStore, но проверяют контракт интерфейса
// Store как такового — те же сценарии должны вести себя одинаково и для
// PostgresStore (см. store.go). Дублировать их через testcontainers/реальный
// Postgres — отдельная задача, не блокирующая текущий auth-трек; покрытие
// здесь страхует логику middleware и CLI, которые видят только интерфейс.

func TestMemoryStore_CreateThenVerify_Succeeds(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	rawKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	created, err := s.Create(ctx, Hash(rawKey), "test key")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Label != "test key" {
		t.Errorf("created.Label = %q, want %q", created.Label, "test key")
	}

	verified, err := s.Verify(ctx, Hash(rawKey))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified.ID != created.ID {
		t.Errorf("Verify() returned id = %q, want %q", verified.ID, created.ID)
	}
}

func TestMemoryStore_Verify_UnknownHash_ReturnsErrKeyNotFound(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	_, err := s.Verify(ctx, Hash("wa_live_never_created"))
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Verify() error = %v, want ErrKeyNotFound", err)
	}
}

func TestMemoryStore_Verify_RevokedKey_ReturnsErrKeyNotFound(t *testing.T) {
	// Middleware не должен различать "ключ не существует" и "ключ отозван" —
	// оба случая дают клиенту одинаковый 401 (см. middleware.go), поэтому
	// Verify обязан возвращать ту же ошибку для обоих сценариев.
	ctx := context.Background()
	s := NewMemoryStore()

	rawKey, _ := GenerateKey()
	created, err := s.Create(ctx, Hash(rawKey), "to be revoked")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := s.Revoke(ctx, created.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	_, err = s.Verify(ctx, Hash(rawKey))
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Verify() on revoked key error = %v, want ErrKeyNotFound", err)
	}
}

func TestMemoryStore_Revoke_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	rawKey, _ := GenerateKey()
	created, _ := s.Create(ctx, Hash(rawKey), "double revoke")

	if err := s.Revoke(ctx, created.ID); err != nil {
		t.Fatalf("first Revoke() error = %v", err)
	}
	if err := s.Revoke(ctx, created.ID); err != nil {
		t.Errorf("second Revoke() on already-revoked key error = %v, want nil (idempotent)", err)
	}
}

func TestMemoryStore_Revoke_UnknownID_ReturnsErrKeyNotFound(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	err := s.Revoke(ctx, "does-not-exist")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("Revoke() on unknown id error = %v, want ErrKeyNotFound", err)
	}
}

func TestMemoryStore_Touch_UpdatesLastUsedAt(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	rawKey, _ := GenerateKey()
	created, _ := s.Create(ctx, Hash(rawKey), "touch me")

	if created.LastUsedAt != nil {
		t.Fatalf("newly created key already has LastUsedAt = %v, want nil", created.LastUsedAt)
	}

	if err := s.Touch(ctx, created.ID); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}

	keys, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var found bool
	for _, k := range keys {
		if k.ID == created.ID {
			found = true
			if k.LastUsedAt == nil {
				t.Error("LastUsedAt is still nil after Touch()")
			}
		}
	}
	if !found {
		t.Fatalf("created key %s not found in List()", created.ID)
	}
}

func TestMemoryStore_List_IncludesRevokedKeys(t *testing.T) {
	// cmd/artkeys list должен показывать и отозванные ключи (с пометкой) —
	// это инструмент аудита, а не только "что сейчас активно".
	ctx := context.Background()
	s := NewMemoryStore()

	rawKey, _ := GenerateKey()
	created, _ := s.Create(ctx, Hash(rawKey), "will be revoked")
	if err := s.Revoke(ctx, created.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	keys, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("List() returned %d keys, want 1 (revoked keys must still be listed)", len(keys))
	}
	if keys[0].RevokedAt == nil {
		t.Error("listed key has nil RevokedAt despite being revoked")
	}
}
