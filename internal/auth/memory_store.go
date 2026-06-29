package auth

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// MemoryStore — in-memory реализация Store для тестов (router_test.go,
// будущие тесты самого пакета auth). Не предназначена для прода — нет
// персистентности между перезапусками процесса. Существует ровно потому,
// что router_test.go и подобные HTTP-тесты не должны поднимать настоящий
// Postgres ради проверки роутинга и middleware (см. architecture.md
// "store — единственное место, которое знает про формат хранения": тот же
// принцип, что у store.Store для artworks, применён здесь к auth.Store).
type MemoryStore struct {
	mu     sync.Mutex
	keys   map[string]*Key   // id -> Key
	byHash map[string]string // key_hash -> id
	seq    int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		keys:   make(map[string]*Key),
		byHash: make(map[string]string),
	}
}

func (m *MemoryStore) Create(_ context.Context, keyHash, label string) (Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.seq++
	id := "test-key-" + strconv.Itoa(m.seq)
	k := &Key{ID: id, Label: label, CreatedAt: time.Now()}
	m.keys[id] = k
	m.byHash[keyHash] = id
	return *k, nil
}

func (m *MemoryStore) Verify(_ context.Context, keyHash string) (Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, ok := m.byHash[keyHash]
	if !ok {
		return Key{}, ErrKeyNotFound
	}
	k := m.keys[id]
	if k.RevokedAt != nil {
		return Key{}, ErrKeyNotFound
	}
	return *k, nil
}

func (m *MemoryStore) Touch(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k, ok := m.keys[id]
	if !ok {
		return ErrKeyNotFound
	}
	now := time.Now()
	k.LastUsedAt = &now
	return nil
}

func (m *MemoryStore) List(_ context.Context) ([]Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keys := make([]Key, 0, len(m.keys))
	for _, k := range m.keys {
		keys = append(keys, *k)
	}
	return keys, nil
}

func (m *MemoryStore) Revoke(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k, ok := m.keys[id]
	if !ok {
		return ErrKeyNotFound
	}
	if k.RevokedAt == nil {
		now := time.Now()
		k.RevokedAt = &now
	}
	return nil
}

func (m *MemoryStore) Close() {}
