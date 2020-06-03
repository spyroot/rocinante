package server

import "sync"

/**
	Generic interface for storage
 */
type Storage interface {

	Set(key string, value []byte)
	Get(key string) ([]byte, bool)

	// HasData returns true iff any Sets were made on this Storage.
	HasData() bool
}

/**
  In memory storage
 */
type VolatileStorage struct {
	mu sync.Mutex
	m  map[string][]byte
}

func NeInMemory() *VolatileStorage {
	m := make(map[string][]byte)
	return &VolatileStorage{m: m,}
}

func (ms *VolatileStorage) Get(key string) ([]byte, bool) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	v, found := ms.m[key]
	return v, found
}

func (ms *VolatileStorage) Set(key string, value []byte) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.m[key] = value
}

func (ms *VolatileStorage) HasData() bool {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return len(ms.m) > 0
}