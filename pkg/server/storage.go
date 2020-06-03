package server

import (
	"encoding/gob"
	"os"
	"sync"
)

/**
Generic interface for storage
*/
type Storage interface {
	Set(key string, value []byte)
	Get(key string) ([]byte, bool)
	HasKey() bool
	HasData() bool
}

type FileStorage struct {
	mutex    sync.Mutex
	storage  map[string][]byte
	fileName string
}

/**

 */
func (vs *FileStorage) Serialize() (bool, error) {
	file, _ := os.Create(vs.fileName)
	defer file.Close()

	e := gob.NewEncoder(file)
	err := e.Encode(vs.storage)
	if err != nil {
		return false, err
	}

	return true, nil
}

/**

 */
func (vs *FileStorage) Get(key string) ([]byte, bool) {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()
	v, found := vs.storage[key]
	return v, found
}

/**

 */
func (vs *FileStorage) Set(key string, value []byte) {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()
	vs.storage[key] = value
}

/**

 */
func (vs *FileStorage) HasKey(key string) bool {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()
	if _, ok := vs.storage[key]; ok {
		return true
	}
	return false
}

/**

 */
func (vs *FileStorage) HasData() bool {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()
	return len(vs.storage) > 0
}

/**
  In memory storage
*/
type VolatileStorage struct {
	mutex   sync.Mutex
	storage map[string][]byte
}

func NeInMemory() *VolatileStorage {
	m := make(map[string][]byte)
	return &VolatileStorage{storage: m}
}

func (vs *VolatileStorage) Get(key string) ([]byte, bool) {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()
	v, found := vs.storage[key]
	return v, found
}

func (vs *VolatileStorage) Set(key string, value []byte) {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()
	vs.storage[key] = value
}

func (vs *VolatileStorage) HasKey(key string) bool {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()
	if _, ok := vs.storage[key]; ok {
		return true
	}
	return false
}

func (vs *VolatileStorage) HasData() bool {
	vs.mutex.Lock()
	defer vs.mutex.Unlock()
	return len(vs.storage) > 0
}

// Encoding the m
