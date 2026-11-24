package identity

import (
	"encoding/json"
	"testing"
	"time"
)

// pausingStore holds the caller of Replace or Table, after the store did its
// part, until it is released, so a node can be recorded while a restore or an
// export is half done.
type pausingStore struct {
	Store
	pauseReplace, pauseTable bool
	paused                   chan struct{}
	release                  chan struct{}
}

func newPausingStore() *pausingStore {
	return &pausingStore{Store: NewMemoryStore(), paused: make(chan struct{}, 1), release: make(chan struct{})}
}

func (s *pausingStore) Replace(table Table) error {
	if err := s.Store.Replace(table); err != nil {
		return err
	}
	if s.pauseReplace {
		s.paused <- struct{}{}
		<-s.release
	}
	return nil
}

func (s *pausingStore) Table() (Table, error) {
	table, err := s.Store.Table()
	if err == nil && s.pauseTable {
		s.paused <- struct{}{}
		<-s.release
	}
	return table, err
}

// nodeDuring records a node on another goroutine while the store is paused and
// returns once that node was recorded or the window expired, then releases the
// store.
func nodeDuring(t *testing.T, s *pausingStore, l *Ledger, finish func()) {
	t.Helper()
	<-s.paused
	recorded := make(chan struct{})
	go func() {
		defer close(recorded)
		if _, err := l.Node(bridge, nativeKey(t, installation, "driver-a", "a1", "resource.equipment", "racing-device")); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-recorded:
	case <-time.After(200 * time.Millisecond):
	}
	close(s.release)
	<-recorded
	finish()
}

func TestRestoreDoesNotSplitANodeFromItsAnchor(t *testing.T) {
	store := newPausingStore()
	l := ledgerOn(t, store, embeddedCatalog(t))
	empty, err := l.Export()
	if err != nil {
		t.Fatal(err)
	}
	store.pauseReplace = true
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := l.Restore(empty); err != nil {
			t.Error(err)
		}
	}()
	nodeDuring(t, store, l, func() { <-done })
	doc := exportedCheckpoint(t, l)
	if anchored, recorded := len(doc.Anchors.Anchors) > 0, len(doc.Nodes) > 0; anchored != recorded {
		t.Fatalf("after the restore the anchor is held = %v and the node is recorded = %v", anchored, recorded)
	}
}

// An allocator operation that already began on the allocator never commits into
// the table a restore is installing: it waits for the restore and runs after it.
func TestRestoreWaitsForAnAllocatorOperationAndRunsItAfter(t *testing.T) {
	store := newPausingStore()
	l := ledgerOn(t, store, embeddedCatalog(t))
	empty, err := l.Export()
	if err != nil {
		t.Fatal(err)
	}
	store.pauseReplace = true
	restored := make(chan struct{})
	go func() {
		defer close(restored)
		if err := l.Restore(empty); err != nil {
			t.Error(err)
		}
	}()
	<-store.paused
	resolved := make(chan struct{})
	go func() {
		defer close(resolved)
		if _, _, err := l.allocator.Resolve(kindEquipment, tokenAnchor("token-a", "x")); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-resolved:
		t.Error("an allocator operation committed while the restore was replacing the table")
	case <-time.After(200 * time.Millisecond):
	}
	close(store.release)
	<-restored
	<-resolved
	if table, err := store.Table(); err != nil || len(table.Anchors) == 0 {
		t.Fatalf("the operation that ran after the restore left no anchor: %v %v", table, err)
	}
}

func TestExportDoesNotPairAnchorsFromBeforeWithNodesFromAfter(t *testing.T) {
	store := newPausingStore()
	l := ledgerOn(t, store, embeddedCatalog(t))
	store.pauseTable = true
	exported := make(chan checkpoint, 1)
	go func() {
		data, err := l.Export()
		if err != nil {
			t.Error(err)
		}
		var doc checkpoint
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Error(err)
		}
		exported <- doc
	}()
	nodeDuring(t, store, l, func() {})
	doc := <-exported
	if anchored, recorded := len(doc.Anchors.Anchors) > 0, len(doc.Nodes) > 0; anchored != recorded {
		t.Fatalf("the export holds the anchor = %v and the node = %v", anchored, recorded)
	}
}

func exportedCheckpoint(t *testing.T, l *Ledger) checkpoint {
	t.Helper()
	data, err := l.Export()
	if err != nil {
		t.Fatal(err)
	}
	var doc checkpoint
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
