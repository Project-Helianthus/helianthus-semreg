package identity

import (
	"sync"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Store is the supersession table and the home asset of each resource.
// Commit is atomic: it records the previous key as superseded (unless it
// equals the next) and the next key as current, and refuses a record that
// contradicts what it holds, including a class, a previous basis, a previous
// parent or a previous slot other than the previous key's recorded one. RecordHome records asset as the home of
// resource when none is recorded and returns the recorded home, so every
// allocator over the store sees one home per resource.
type Store interface {
	Lookup(key AnchorKey) (Entry, bool, error)
	Commit(record Supersession) error
	RecordHome(resource semreg.ResourceID, asset semreg.AssetID) (semreg.AssetID, error)
	// Table returns every recorded anchor and home. Replace makes table the
	// whole content of the store, so a checkpoint restores it.
	Table() (Table, error)
	Replace(Table) error
}

// StoredAnchor and StoredHome are the members of the stored document.
type (
	StoredAnchor = storeAnchor
	StoredHome   = storeHome
)

// Table is the content of a Store in stored order.
type Table struct {
	Anchors []StoredAnchor `json:"anchors"`
	Homes   []StoredHome   `json:"homes"`
}

// MemoryStore is the in-memory Store.
type MemoryStore struct {
	mu      sync.Mutex
	entries map[AnchorKey]Entry
	homes   map[semreg.ResourceID]semreg.AssetID
}

// NewMemoryStore returns an empty in-memory supersession table.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[AnchorKey]Entry), homes: make(map[semreg.ResourceID]semreg.AssetID)}
}

// RecordHome records asset as the home of resource unless one is recorded,
// and returns the recorded home.
func (s *MemoryStore) RecordHome(resource semreg.ResourceID, asset semreg.AssetID) (semreg.AssetID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if home, ok := s.homes[resource]; ok {
		return home, nil
	}
	s.homes[resource] = asset
	return asset, nil
}

// Lookup returns the recorded entry of key.
func (s *MemoryStore) Lookup(key AnchorKey) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	return entry, ok, nil
}

// Commit records one supersession.
func (s *MemoryStore) Commit(record Supersession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return commitTo(s.entries, record)
}

// commitTo applies one supersession to entries, or refuses it and leaves
// entries unchanged.
func commitTo(entries map[AnchorKey]Entry, record Supersession) error {
	if previous, ok := entries[record.Previous]; ok && (previous.Superseded || previous.Resource != record.Resource || previous.Class != record.Class || previous.Basis != record.PreviousBasis || previous.Parent != record.PreviousParent || previous.Slot != record.PreviousSlot) {
		return ErrStoreConflict
	}
	if record.Next != record.Previous {
		if _, ok := entries[record.Next]; ok {
			return ErrStoreConflict
		}
		entries[record.Previous] = Entry{Resource: record.Resource, Class: record.Class, Basis: record.PreviousBasis, Parent: record.PreviousParent, Slot: record.PreviousSlot, Superseded: true}
	}
	entries[record.Next] = Entry{Resource: record.Resource, Class: record.Class, Basis: record.NextBasis, Parent: record.NextParent, Slot: record.NextSlot}
	return nil
}

// Table returns every recorded anchor and home in stored order.
func (s *MemoryStore) Table() (Table, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return tableOf(s.entries, s.homes), nil
}

// Replace makes table the whole content of the store.
func (s *MemoryStore) Replace(table Table) error {
	entries, homes, err := documentMaps(storeDocument{Anchors: table.Anchors, Homes: table.Homes})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries, s.homes = entries, homes
	return nil
}
