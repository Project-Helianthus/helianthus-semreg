package life

import (
	"context"
	"sync"
)

// ControlStore is the control store a Supervisor drives: the Store seam of the
// service plus registration of the configured instances and release of the
// store's hold.
type ControlStore interface {
	Store
	// Register registers the configured instances and returns the startup plan
	// derived from durable desired state. The first registration of an empty
	// store seeds it once.
	Register(ctx context.Context, instances []ControlInstance) ([]ControlStartup, error)
	// Exclusive reports that this process holds the store exclusively: no other
	// process can own native resources recorded in it.
	Exclusive() bool
	// Close ends the store's use; Release relinquishes the hold it keeps.
	Close()
	Release()
}

// MemoryStore is the in-memory control store. Its committed state goes through
// the same record codec as the durable store and lives as long as the value.
type MemoryStore struct {
	*controlStore
	mem *memoryPersister
}

type memoryPersister struct {
	mu   sync.Mutex
	data []byte
}

func (m *memoryPersister) read() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return nil, nil
	}
	return append([]byte(nil), m.data...), nil
}

func (m *memoryPersister) commit(data []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = append([]byte(nil), data...)
	return true, nil
}

func (m *memoryPersister) syncDir() error { return nil }

// NewMemoryStore returns an empty in-memory control store.
func NewMemoryStore() *MemoryStore {
	p := &memoryPersister{}
	c := newControlStore(p)
	c.load()
	return &MemoryStore{controlStore: c, mem: p}
}

// Exclusive is false: a memory store cannot prove that no earlier process owned
// native resources.
func (*MemoryStore) Exclusive() bool { return false }

// Close ends the store's use.
func (m *MemoryStore) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
}

// Release has nothing to relinquish.
func (*MemoryStore) Release() {}
