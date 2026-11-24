package life

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startPlanner places a request in the current slot when it is empty,
// otherwise in the empty reserved slot unless an accepted RESTART is still
// executing; anything else is TRANSITION_BUSY.
func startPlanner(in PlanInput) (SlotPlan, *Refusal) {
	next := &Transition{Kind: TransitionKind(in.Method), Phase: PhasePending}
	switch {
	case in.Current == nil:
		return SlotPlan{Current: next}, nil
	case in.Reserved == nil && in.Current.Kind != KindRestart:
		return SlotPlan{Current: in.Current, Reserved: next}, nil
	}
	return SlotPlan{}, refuse(RefusalTransitionBusy, 8, "typed transition slots are occupied")
}

func storeCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

var storeInstances = []ControlInstance{{InstanceID: "alpha.off", Enabled: false}, {InstanceID: "bus.primary", Enabled: true}}

func openStore(t *testing.T, dir string, hooks *blockHooks) *FileStore {
	t.Helper()
	if hooks == nil {
		hooks = &blockHooks{}
	}
	s, err := openFileStore(dir, hooks, acquireStoreLock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

func seededStore(t *testing.T, hooks *blockHooks) (*FileStore, string) {
	t.Helper()
	dir := t.TempDir()
	s := openStore(t, dir, hooks)
	if _, err := s.Register(storeCtx(t), storeInstances); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func mustDriver(t *testing.T, s Store, id InstanceID) ControlRecord {
	t.Helper()
	rec, err := s.Driver(id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// The first registration of an empty store seeds it once: an enabled instance
// is recorded as start and a disabled one as stop; the committed records survive
// reopening, and a stop decision survives it too.
func TestStoreSeedOnceAndStoppedSurvivesReload(t *testing.T) {
	s, dir := seededStore(t, nil)
	off, on := mustDriver(t, s, "alpha.off"), mustDriver(t, s, "bus.primary")
	if off.DesiredState != DesiredStopped || off.DesiredRevision != 1 || on.DesiredState != DesiredRunning || on.DesiredRevision != 2 {
		t.Fatalf("seeded records = %+v %+v", off, on)
	}
	cat, err := s.Catalog()
	if err != nil || cat.IssuedMark != 2 || cat.CatalogRevision != 3 {
		t.Fatalf("seeded catalog = %+v %v", cat, err)
	}
	view, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner)
	if err != nil || view.DesiredState != DesiredStopped || view.DesiredRevision != 3 {
		t.Fatalf("stop = %+v %v", view, err)
	}
	_ = s.Stop(context.Background())

	second := openStore(t, dir, nil)
	plan, err := second.Register(storeCtx(t), storeInstances)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0].Action != StartupDisabled || plan[1].Action != StartupStopped || plan[1].DesiredRevision != 3 {
		t.Fatalf("startup plan after reload = %+v", plan)
	}
	cat, _ = second.Catalog()
	if cat.IssuedMark != 3 {
		t.Fatalf("a registration after the seed changed the mark: %+v", cat)
	}
}

// A decision is committed before Decide returns, for the memory store and for
// the durable store, and the durable store shows it after reopening.
func TestStoreDecisionIsCommittedBeforeItReturns(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		s := NewMemoryStore()
		if _, err := s.Register(storeCtx(t), storeInstances); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner); err != nil {
			t.Fatal(err)
		}
		committed, err := s.mem.read()
		if err != nil {
			t.Fatal(err)
		}
		ns := decodeNamespace(committed)
		if r := ns.records["bus.primary"]; r.Desired != DesiredStopped || r.Revision != 3 || r.Current == nil || r.Current.Kind != KindStop {
			t.Fatalf("committed bytes do not hold the decision: %+v", r)
		}
	})
	t.Run("durable", func(t *testing.T) {
		s, dir := seededStore(t, nil)
		view, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, controlFileName))
		if err != nil {
			t.Fatal(err)
		}
		if r := decodeNamespace(raw).records["bus.primary"]; r.Revision != view.DesiredRevision || r.Desired != DesiredStopped {
			t.Fatalf("file does not hold the returned revision %d: %+v", view.DesiredRevision, r)
		}
		_ = s.Stop(context.Background())
		reopened := openStore(t, dir, nil)
		if _, err := reopened.Register(storeCtx(t), storeInstances); err != nil {
			t.Fatal(err)
		}
		if rec := mustDriver(t, reopened, "bus.primary"); rec.DesiredRevision != view.DesiredRevision || rec.DesiredState != DesiredStopped || rec.Current == nil {
			t.Fatalf("reopened record = %+v", rec)
		}
	})
}

// Disabled configuration wins: a decision for a disabled instance is refused and
// durable intent is not rewritten.
func TestStoreDisabledConfigurationWinsWithoutRewritingIntent(t *testing.T) {
	s, dir := seededStore(t, nil)
	before, _ := os.ReadFile(filepath.Join(dir, controlFileName))
	_, err := s.Decide(storeCtx(t), MethodStart, Request{InstanceID: "alpha.off", ExpectedRevision: 1}, startPlanner)
	refusalOf(t, err, RefusalFailedPrecondition, 4)
	after, _ := os.ReadFile(filepath.Join(dir, controlFileName))
	if string(before) != string(after) {
		t.Fatal("a refused decision rewrote the control state")
	}
}

// Failures before the commit point discard the staging: nothing is exposed, the
// error is retryable and a later write succeeds.
func TestStorePreCommitFailuresDiscardStaging(t *testing.T) {
	for name, set := range map[string]func(*failHooks){
		"write":  func(h *failHooks) { h.failWrite = errors.New("synthetic write failure") },
		"fsync":  func(h *failHooks) { h.failFsync = errors.New("synthetic fsync failure") },
		"rename": func(h *failHooks) { h.failRenameErr = errors.New("synthetic rename failure") },
	} {
		t.Run(name, func(t *testing.T) {
			hooks := &failHooks{}
			dir := t.TempDir()
			s, err := openFileStore(dir, hooks, acquireStoreLock)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Stop(context.Background()) })
			if _, err := s.Register(storeCtx(t), storeInstances); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(dir, controlFileName))
			set(hooks)
			_, err = s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner)
			var refusal *Refusal
			if !errors.As(err, &refusal) || refusal.Code != RefusalUnavailable || refusal.Row != 0 || !refusal.Retryable() {
				t.Fatalf("pre-commit failure = %v", err)
			}
			if rec := mustDriver(t, s, "bus.primary"); rec.DesiredState != DesiredRunning || rec.DesiredRevision != 2 || rec.Current != nil {
				t.Fatalf("a failed write was exposed: %+v", rec)
			}
			after, _ := os.ReadFile(filepath.Join(dir, controlFileName))
			if string(before) != string(after) {
				t.Fatal("a failed write changed the committed file")
			}
			if !s.AdmissionOpen() {
				t.Fatal("a pre-commit failure closed admission")
			}
			*hooks = failHooks{}
			if _, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner); err != nil {
				t.Fatalf("a later write after the failure: %v", err)
			}
		})
	}
}

type failHooks struct {
	osHooks
	failWrite, failFsync, failRenameErr error
}

func (h *failHooks) WriteFile(path string, data []byte, perm uint32) error {
	if h.failWrite != nil {
		return h.failWrite
	}
	return h.osHooks.WriteFile(path, data, perm)
}

func (h *failHooks) FsyncFile(path string) error {
	if h.failFsync != nil {
		return h.failFsync
	}
	return h.osHooks.FsyncFile(path)
}

func (h *failHooks) Rename(oldpath, newpath string) error {
	if h.failRenameErr != nil {
		return h.failRenameErr
	}
	return h.osHooks.Rename(oldpath, newpath)
}

// A directory fsync failure after the rename is ambiguous persistence: nothing
// is exposed, admission closes and readback reopens it only when the stored
// namespace agrees with the staged or the previous one and directory durability
// is established.
func TestStoreDirectoryFsyncFailureIsAmbiguousPersistence(t *testing.T) {
	for _, persisted := range []bool{true, false} {
		name := "reverted"
		if persisted {
			name = "persisted"
		}
		t.Run(name, func(t *testing.T) {
			hooks := &blockHooks{}
			s, dir := seededStore(t, hooks)
			path := filepath.Join(dir, controlFileName)
			previous, _ := os.ReadFile(path)
			hooks.mu.Lock()
			hooks.failDirSync = 1
			if !persisted {
				hooks.afterRename = func(p string) { _ = os.WriteFile(p, previous, 0o644) }
			}
			hooks.mu.Unlock()
			_, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner)
			refusalOf(t, err, RefusalUnavailable, 3)
			if s.AdmissionOpen() {
				t.Fatal("ambiguous persistence left admission open")
			}
			if rec := mustDriver(t, s, "bus.primary"); rec.DesiredRevision != 2 || rec.DesiredState != DesiredRunning {
				t.Fatalf("an ambiguous write was exposed: %+v", rec)
			}
			if _, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner); err == nil {
				t.Fatal("a decision was admitted before the readback")
			}
			hooks.mu.Lock()
			hooks.afterRename = nil
			hooks.mu.Unlock()
			if err := s.ResolveReadback(storeCtx(t)); err != nil {
				t.Fatalf("readback: %v", err)
			}
			if !s.AdmissionOpen() {
				t.Fatal("a resolved readback left admission closed")
			}
			rec := mustDriver(t, s, "bus.primary")
			if persisted && (rec.DesiredRevision != 3 || rec.DesiredState != DesiredStopped) {
				t.Fatalf("the persisted decision was not adopted: %+v", rec)
			}
			if !persisted && (rec.DesiredRevision != 2 || rec.DesiredState != DesiredRunning) {
				t.Fatalf("the reverted decision was exposed: %+v", rec)
			}
		})
	}
}

// The load dispositions never collapse: a missing file is seedable, a corrupt or
// unsupported one is preserved byte for byte and fails control closed.
func TestStoreLoadDispositionsNeverCollapse(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    ControlLoadDisposition
	}{
		{"missing", "", ControlLoadMissing},
		{"corrupt", "{not json", ControlLoadCorrupt},
		{"unknown member", `{"schema_version":1,"issued_revision_mark":"0","catalog_revision":"1","instances":{},"extra":1}`, ControlLoadCorrupt},
		{"unsupported", `{"schema_version":9,"issued_revision_mark":"0","catalog_revision":"1","instances":{}}`, ControlLoadUnsupported},
		{"mark not canonical", `{"schema_version":1,"issued_revision_mark":"01","catalog_revision":"1","instances":{}}`, ControlLoadCorrupt},
		{"catalog zero", `{"schema_version":1,"issued_revision_mark":"0","catalog_revision":"0","instances":{}}`, ControlLoadCorrupt},
		{"loaded", `{"schema_version":1,"issued_revision_mark":"0","catalog_revision":"1","instances":{}}`, ControlLoadLoaded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, controlFileName)
			if tc.content != "" {
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			s := openStore(t, dir, nil)
			if got, _ := s.ControlLoad(); got != tc.want {
				t.Fatalf("disposition = %s, want %s", got, tc.want)
			}
			_, _ = s.Register(storeCtx(t), storeInstances)
			after, _ := os.ReadFile(path)
			switch tc.want {
			case ControlLoadCorrupt, ControlLoadUnsupported:
				if string(after) != tc.content {
					t.Fatal("a failed control state was overwritten")
				}
				if plan := s.startupPlan(); len(plan) != 2 || plan[0].Action != StartupFailed {
					t.Fatalf("failed state startup plan = %+v", plan)
				}
			case ControlLoadMissing:
				if len(after) == 0 {
					t.Fatal("a missing state was not seeded")
				}
			}
		})
	}
}

// A second opener that cannot take the store lock reads the last committed
// state read-only and writes nothing: no seed, no charge, no control write.
func TestStoreLockHeldKeepsCommittedViewReadOnly(t *testing.T) {
	owner, dir := seededStore(t, nil)
	path := filepath.Join(dir, controlFileName)
	if _, err := owner.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner); err != nil {
		t.Fatal(err)
	}
	committed, _ := os.ReadFile(path)
	if !owner.Exclusive() {
		t.Fatal("the owner does not hold the lock")
	}

	second := openStore(t, dir, nil)
	if second.Exclusive() {
		t.Fatal("a second opener took the lock")
	}
	if got, _ := second.ControlLoad(); got != ControlLoadLocked {
		t.Fatalf("disposition = %s", got)
	}
	if _, err := second.Register(storeCtx(t), storeInstances); err != nil {
		t.Fatal(err)
	}
	cat, err := second.Catalog()
	if err != nil || !cat.ReadOnly || cat.IssuedMark != 3 {
		t.Fatalf("read-only catalog = %+v %v", cat, err)
	}
	rec := mustDriver(t, second, "bus.primary")
	if !rec.ReadOnly || rec.DesiredState != DesiredStopped || rec.DesiredRevision != 3 || rec.Current == nil || rec.Current.Kind != KindStop {
		t.Fatalf("read-only record = %+v", rec)
	}
	if _, err := second.Decide(storeCtx(t), MethodStart, Request{InstanceID: "bus.primary", ExpectedRevision: 3}, startPlanner); err == nil {
		t.Fatal("a decision was admitted without the lock")
	}
	if err := second.CommitWork(storeCtx(t), "bus.primary", nil, nil); err == nil {
		t.Fatal("work was committed without the lock")
	}
	if _, err := second.AdvanceCatalog(storeCtx(t)); err == nil {
		t.Fatal("the catalog advanced without the lock")
	}
	if err := second.CommitRecovery(storeCtx(t), &RecoveryMaterial{DriverIDs: []InstanceID{"bus.primary"}, Payload: []byte("x")}); err == nil {
		t.Fatal("recovery was committed without the lock")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(committed) {
		t.Fatal("the read-only opener changed the committed bytes")
	}
	// The owner releases the lock: the next opener is exclusive.
	_ = owner.Stop(context.Background())
	third := openStore(t, dir, nil)
	if !third.Exclusive() {
		t.Fatal("the lock was not released")
	}
}

// An invalid instance record fails only that instance.
func TestStoreInvalidRecordFailsOnlyThatInstance(t *testing.T) {
	dir := t.TempDir()
	good := recordJSON(DesiredRunning, 1, 0, "")
	content := `{"schema_version":1,"issued_revision_mark":"2","catalog_revision":"3","instances":{"fam.good":` + good + `,"fam.bad":{"desired_state":"PAUSED"}}}`
	if err := os.WriteFile(filepath.Join(dir, controlFileName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s := openStore(t, dir, nil)
	if _, err := s.Register(storeCtx(t), enabled("fam.good", "fam.bad")); err != nil {
		t.Fatal(err)
	}
	if rec := mustDriver(t, s, "fam.bad"); !rec.Failed {
		t.Fatalf("the bad record is usable: %+v", rec)
	}
	if rec := mustDriver(t, s, "fam.good"); rec.Failed || rec.DesiredState != DesiredRunning {
		t.Fatalf("the good record failed with its neighbour: %+v", rec)
	}
	_, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "fam.bad", ExpectedRevision: 1}, startPlanner)
	refusalOf(t, err, RefusalFailedPrecondition, 3)
}

// Removing an instance from the configuration and adding it again are mark-only
// writes: its record is deleted when it holds no work or recovery material, and
// a re-added instance reads as stopped at the mark.
func TestStoreRemovalAndReAddAreMarkOnlyWrites(t *testing.T) {
	s, dir := seededStore(t, nil)
	if _, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitWork(storeCtx(t), "bus.primary", nil, nil); err != nil {
		t.Fatalf("clearing the terminal slot: %v", err)
	}
	_ = s.Stop(context.Background())

	removed := openStore(t, dir, nil)
	if _, err := removed.Register(storeCtx(t), []ControlInstance{{InstanceID: "alpha.off"}}); err != nil {
		t.Fatal(err)
	}
	cat, _ := removed.Catalog()
	if cat.IssuedMark != 4 || len(cat.Drivers) != 1 {
		t.Fatalf("after removal = %+v", cat)
	}
	_ = removed.Stop(context.Background())

	readded := openStore(t, dir, nil)
	if _, err := readded.Register(storeCtx(t), storeInstances); err != nil {
		t.Fatal(err)
	}
	rec := mustDriver(t, readded, "bus.primary")
	if rec.HasRecord || rec.DesiredState != DesiredStopped || rec.DesiredRevision != 5 {
		t.Fatalf("re-added record = %+v", rec)
	}
}

// Work updates keep the stored identity, move forward only and never produce
// reversed or duplicate slots.
func TestStoreWorkGuards(t *testing.T) {
	s, _ := seededStore(t, nil)
	if _, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner); err != nil {
		t.Fatal(err)
	}
	cur := mustDriver(t, s, "bus.primary").Current
	obsolete := *cur
	obsolete.Sequence++
	if err := s.CommitWork(storeCtx(t), "bus.primary", &obsolete, nil); !errors.Is(err, ErrControlInvalidWork) {
		t.Fatalf("obsolete identity = %v", err)
	}
	active := *cur
	active.Phase = PhaseActive
	active.Epoch, active.Generation = "e.1", 1
	active.NativeClosePending, active.SemanticCleanupPending = true, true
	active.DeadlineUTC = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.CommitWork(storeCtx(t), "bus.primary", &active, nil); err != nil {
		t.Fatal(err)
	}
	back := active
	back.Phase = PhasePending
	if err := s.CommitWork(storeCtx(t), "bus.primary", &back, nil); !errors.Is(err, ErrInvalidWork) {
		t.Fatalf("phase backwards = %v", err)
	}
	extended := active
	extended.DeadlineUTC = active.DeadlineUTC.Add(time.Hour)
	if err := s.CommitWork(storeCtx(t), "bus.primary", &extended, nil); !errors.Is(err, ErrInvalidWork) {
		t.Fatalf("deadline extended = %v", err)
	}
	renewed := active
	renewed.SemanticCleanupPending = false
	renewed.NativeClosePending = false
	renewed.Phase = PhaseCleanup
	if err := s.CommitWork(storeCtx(t), "bus.primary", &renewed, nil); err != nil {
		t.Fatal(err)
	}
	reactivated := renewed
	reactivated.NativeClosePending = true
	if err := s.CommitWork(storeCtx(t), "bus.primary", &reactivated, nil); !errors.Is(err, ErrInvalidWork) {
		t.Fatalf("cleared obligation re-activated = %v", err)
	}
}

// The loader rejects reversed and duplicate transition sequences per record.
func TestStoreLoaderRejectsInvalidRecordInvariants(t *testing.T) {
	far := "2099-01-01T00:00:00Z"
	t1 := transitionJSON(KindStop, 1, 2, PhasePending, "", 0, false, false, 0, "")
	t2 := transitionJSON(KindStart, 1, 1, PhasePending, "", 0, false, false, 0, "")
	_ = far
	cases := map[string]string{
		"reversed":      `{"desired_state":"RUNNING","desired_revision":"1","lifecycle_sequence":"2","current":` + t1 + `,"reserved":` + t2 + `}`,
		"duplicate":     `{"desired_state":"RUNNING","desired_revision":"1","lifecycle_sequence":"2","current":` + t1 + `,"reserved":` + t1 + `}`,
		"reserved only": `{"desired_state":"RUNNING","desired_revision":"1","lifecycle_sequence":"2","current":null,"reserved":` + t1 + `}`,
		"above mark":    `{"desired_state":"RUNNING","desired_revision":"9","lifecycle_sequence":"0","current":null,"reserved":null}`,
		"revision":      `{"desired_state":"RUNNING","desired_revision":"1","lifecycle_sequence":"0","current":` + t1 + `,"reserved":null}`,
	}
	for name, record := range cases {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"schema_version": 1, "issued_revision_mark": "1", "catalog_revision": "2",
				"instances": map[string]json.RawMessage{"fam.a": json.RawMessage(record)}})
			ns := decodeNamespace(raw)
			if ns.status != ControlLoadLoaded {
				t.Fatalf("namespace = %s", ns.status)
			}
			if _, bad := ns.failed["fam.a"]; !bad {
				t.Fatalf("the invalid record was accepted: %+v", ns.records["fam.a"])
			}
		})
	}
}

// The committed recovery envelope is returned byte for byte after reopening and a
// payload whose digest does not match fails control closed.
func TestStoreRecoveryEnvelopeSurvivesReopeningAndIsDigestChecked(t *testing.T) {
	s, dir := seededStore(t, nil)
	if got := s.Recovery(); got != nil {
		t.Fatalf("recovery before any commit = %+v", got)
	}
	material := &RecoveryMaterial{DriverIDs: []InstanceID{"bus.primary"}, MaxDesiredRevision: 2, Payload: []byte("opaque-synthetic-material")}
	if err := s.CommitRecovery(storeCtx(t), material); err != nil {
		t.Fatal(err)
	}
	over := &RecoveryMaterial{DriverIDs: []InstanceID{"bus.primary"}, MaxDesiredRevision: 99, Payload: []byte("x")}
	if err := s.CommitRecovery(storeCtx(t), over); !errors.Is(err, ErrControlInvalidRecovery) {
		t.Fatalf("recovery above the mark = %v", err)
	}
	unsorted := &RecoveryMaterial{DriverIDs: []InstanceID{"b", "a"}, Payload: []byte("x")}
	if err := s.CommitRecovery(storeCtx(t), unsorted); !errors.Is(err, ErrControlInvalidRecovery) {
		t.Fatalf("unsorted ids = %v", err)
	}
	_ = s.Stop(context.Background())
	reopened := openStore(t, dir, nil)
	if _, err := reopened.Register(storeCtx(t), storeInstances); err != nil {
		t.Fatal(err)
	}
	got := reopened.Recovery()
	if got == nil || string(got.Payload) != string(material.Payload) || got.MaxDesiredRevision != 2 || len(got.DriverIDs) != 1 {
		t.Fatalf("recovery after reopening = %+v", got)
	}
	got.Payload[0] ^= 0xff
	if again := reopened.Recovery(); string(again.Payload) != string(material.Payload) {
		t.Fatal("the returned recovery is not a detached copy")
	}
	_ = reopened.Stop(context.Background())

	raw, _ := os.ReadFile(filepath.Join(dir, controlFileName))
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	doc["recovery"].(map[string]any)["payload"] = "AAAA"
	tampered, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(dir, controlFileName), tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	failed := openStore(t, dir, nil)
	if d, _ := failed.ControlLoad(); d != ControlLoadCorrupt {
		t.Fatalf("a tampered recovery payload loaded as %s", d)
	}
}
