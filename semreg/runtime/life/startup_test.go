package life

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

const farFuture = "2099-01-01T00:00:00Z"

// seedControl writes a control file as an earlier process left it. Records are
// keyed by the canonical instance id of their reference.
func seedControl(t *testing.T, dir string, mark, catalog uint64, records map[string]string, recovery string) {
	t.Helper()
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		id, _ := json.Marshal(string(ref(key).ID()))
		parts = append(parts, fmt.Sprintf("%s:%s", id, records[key]))
	}
	rec := ""
	if recovery != "" {
		rec = `,"recovery":` + recovery
	}
	body := fmt.Sprintf(`{"schema_version":1,"issued_revision_mark":"%d","catalog_revision":"%d","instances":{%s}%s}`,
		mark, catalog, strings.Join(parts, ","), rec)
	if err := os.WriteFile(filepath.Join(dir, controlFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func oldEpoch(key string) SourceEpoch { return makeEpoch("old", ref(key).ID(), 1) }

func activeTransition(kind TransitionKind, rev, seq uint64, key string) string {
	return transitionJSON(kind, rev, seq, PhaseActive, string(oldEpoch(key)), 1, true, true, 1, farFuture)
}

// durableRig opens the durable store of dir and plans the named instances, all
// enabled unless disabled lists them.
func durableRig(t *testing.T, dir string, instances func(*testFactory) []InstanceConfig) *rig {
	t.Helper()
	store, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Release() })
	return newRig(t, store, instances)
}

func keysOf(f *testFactory, enabled bool, keys ...string) []InstanceConfig {
	return instanceConfigs(f, enabled, nil, keys...)
}

func settle(t *testing.T, r *rig, key string, state ObservedState) {
	t.Helper()
	waitFor(t, key+" "+string(state), func() bool {
		st := r.status(key)
		rec, _ := r.store.Driver(ref(key).ID())
		return st.ObservedState == state && rec.Current == nil
	})
}

// The first start of an empty store seeds it once: an enabled instance is
// constructed and started, a disabled one is recorded stopped and never
// constructed, and the committed records survive reopening.
func TestStartSeedsAnEmptyStoreOnce(t *testing.T) {
	dir := t.TempDir()
	r := durableRig(t, dir, func(f *testFactory) []InstanceConfig {
		return append(keysOf(f, true, "bus.on"), keysOf(f, false, "bus.off")...)
	})
	r.build("inc1", true)
	r.start()
	if got := r.status("bus.on"); got.ObservedState != ObservedRunning || got.DesiredState != DesiredRunning {
		t.Fatalf("enabled = %+v", got)
	}
	off := r.status("bus.off")
	if off.ObservedState != ObservedDisabled || off.DesiredState != DesiredStopped || off.Enabled {
		t.Fatalf("disabled = %+v", off)
	}
	if r.factory.constructed() != 1 || len(r.instancesOf("bus.off")) != 0 {
		t.Fatal("construction does not follow enablement")
	}
	if r.log.count("restore-empty") != 1 || r.log.count("restore") != 0 {
		t.Fatalf("an empty store restores through RestoreEmpty: %v", r.log.snapshot())
	}
	onRev, offRev := r.status("bus.on").DesiredRevision, off.DesiredRevision
	r.close()
	r.store.Release()

	again := durableRig(t, dir, func(f *testFactory) []InstanceConfig {
		return append(keysOf(f, true, "bus.on"), keysOf(f, false, "bus.off")...)
	})
	defer again.close()
	again.build("inc2", false)
	again.start()
	if again.status("bus.on").DesiredRevision != onRev || again.status("bus.off").DesiredRevision != offRev {
		t.Fatal("reopening re-seeded the store")
	}
	if again.status("bus.off").DesiredState != DesiredStopped {
		t.Fatal("the disabled instance's recorded stop did not survive")
	}
}

// An enabled instance stopped through SetDesired and found stopped after the
// durable store is opened again, with no accepted transition outstanding, is
// neither constructed nor started by Start.
func TestStartedThenStoppedInstanceStaysStoppedAfterReopening(t *testing.T) {
	dir := t.TempDir()
	build := func(f *testFactory) []InstanceConfig { return keysOf(f, true, "bus.one", "bus.two") }
	r := durableRig(t, dir, build)
	r.build("inc1", false)
	r.start()
	r.decide("bus.two", MethodStop)
	settle(t, r, "bus.two", ObservedStopped)
	r.close()
	r.store.Release()

	again := durableRig(t, dir, build)
	defer again.close()
	again.build("inc2", false)
	again.start()
	if got := again.status("bus.two"); got.ObservedState != ObservedStopped || got.DesiredState != DesiredStopped {
		t.Fatalf("bus.two = %+v", got)
	}
	if len(again.instancesOf("bus.two")) != 0 || again.status("bus.one").ObservedState != ObservedRunning {
		t.Fatal("a stopped instance was constructed, or its sibling was not started")
	}
	for _, c := range again.pub.openedSessions() {
		if c.InstanceID == ref("bus.two").ID() {
			t.Fatal("a stopped instance was admitted")
		}
	}
}

// An accepted RESTART interrupted by process loss, whose desired state is
// already start, first settles the predecessor's persisted close and withdrawal
// under the old correlation, then performs the replacement under a new
// correlation with the same desired revision; the instance is not started fresh
// first, and the cleanup charge is committed before the old correlation is
// settled.
func TestRestartRecoveredAfterProcessLoss(t *testing.T) {
	dir := t.TempDir()
	seedControl(t, dir, 1, 2, map[string]string{
		"bus.one": recordJSON(DesiredRunning, 1, 1, activeTransition(KindRestart, 1, 1, "bus.one")),
	}, recoveryJSON([]string{string(ref("bus.one").ID())}, 1, []byte("pre-loss-publication")))
	r := durableRig(t, dir, func(f *testFactory) []InstanceConfig { return keysOf(f, true, "bus.one") })
	defer r.close()
	var chargedAtWithdrawal int
	var constructsAtWithdrawal = -1
	r.pub.onWithdraw = func(corr LifecycleCorrelation) {
		if corr.Epoch == oldEpoch("bus.one") {
			rec, _ := r.store.Driver(corr.InstanceID)
			if rec.Current != nil {
				chargedAtWithdrawal = rec.Current.AttemptsUsed
			}
			constructsAtWithdrawal = r.factory.constructed()
		}
	}
	r.build("inc2", false)
	r.start()
	if chargedAtWithdrawal != 2 || constructsAtWithdrawal != 0 {
		t.Fatalf("at the old withdrawal: attempts charged %d (want 2), constructs %d (want 0)", chargedAtWithdrawal, constructsAtWithdrawal)
	}
	log := r.log.snapshot()
	index := func(call string) int {
		for i, c := range log {
			if c == call {
				return i
			}
		}
		return -1
	}
	if !(index("restore") == 0 && index("restore") < index("withdraw") && index("withdraw") < index("construct") && index("construct") < index("open")) {
		t.Fatalf("order = %v", log)
	}
	if r.factory.constructed() != 1 {
		t.Fatalf("constructed %d instances; only the replacement may be constructed", r.factory.constructed())
	}
	got := r.status("bus.one")
	if got.ObservedState != ObservedRunning || got.Generation != 1 || got.DesiredRevision != 1 || !strings.HasPrefix(string(got.Epoch), "inc2.") {
		t.Fatalf("replacement = %+v", got)
	}
	opened := r.pub.openedSessions()
	if len(opened) != 1 || opened[0].Epoch == oldEpoch("bus.one") {
		t.Fatalf("sessions = %+v", opened)
	}
	if w := r.pub.withdrawals(); len(w) != 1 || w[0].Epoch != oldEpoch("bus.one") || w[0].Generation != 1 {
		t.Fatalf("withdrawals = %+v", w)
	}
}

// An accepted STOP interrupted by process loss before its close and withdrawal
// were cleared, whose desired state is stop, settles the persisted obligations
// under the original correlation, reports stopped only after both are cleared,
// and constructs and starts nothing.
func TestStopRecoveredAfterProcessLossStartsNothing(t *testing.T) {
	dir := t.TempDir()
	seedControl(t, dir, 2, 3, map[string]string{
		"bus.one": recordJSON(DesiredStopped, 2, 2, activeTransition(KindStop, 2, 2, "bus.one")),
		// The terminal case: an already-cleanup slot with both obligations cleared
		// runs no executor call and is only pruned.
		"bus.two": recordJSON(DesiredStopped, 1, 1, transitionJSON(KindStop, 1, 1, PhaseCleanup, string(oldEpoch("bus.two")), 1, false, false, 0, farFuture)),
	}, "")
	r := durableRig(t, dir, func(f *testFactory) []InstanceConfig { return keysOf(f, true, "bus.one", "bus.two") })
	defer r.close()
	var observedAtWithdrawal ObservedState
	r.pub.onWithdraw = func(LifecycleCorrelation) { observedAtWithdrawal = r.status("bus.one").ObservedState }
	r.build("inc2", true)
	r.start()
	settle(t, r, "bus.one", ObservedStopped)
	settle(t, r, "bus.two", ObservedStopped)
	if observedAtWithdrawal == ObservedStopped || observedAtWithdrawal == "" {
		t.Fatalf("stopped was reported before the obligations cleared: %q", observedAtWithdrawal)
	}
	if w := r.pub.withdrawals(); len(w) != 1 || w[0].Epoch != oldEpoch("bus.one") || w[0].Generation != 1 || w[0].InstanceID != ref("bus.one").ID() {
		t.Fatalf("withdrawals = %+v (the terminal slot runs none)", w)
	}
	if r.factory.constructed() != 0 || r.log.count("instance-start") != 0 || r.log.count("open") != 0 {
		t.Fatalf("a recovered STOP started something: %v", r.log.snapshot())
	}
	if secured := r.recovery.securedTransitions(); len(secured) != 2 {
		t.Fatalf("secured %d terminal slots, want 2", len(secured))
	}
}

// recordingStore wraps a control store and records the order of its writes.
type recordingStore struct {
	ControlStore
	log           *callLog
	failRecovery  error
	recoveryWrote *RecoveryMaterial
}

func (s *recordingStore) CommitRecovery(ctx context.Context, m *RecoveryMaterial) error {
	s.log.add("commit-recovery")
	if s.failRecovery != nil {
		return s.failRecovery
	}
	s.recoveryWrote = m
	return s.ControlStore.CommitRecovery(ctx, m)
}

func (s *recordingStore) CommitWork(ctx context.Context, id InstanceID, current, reserved *Transition) error {
	if current == nil && reserved == nil {
		s.log.add("prune")
	}
	return s.ControlStore.CommitWork(ctx, id, current, reserved)
}

func recoveryRig(t *testing.T, material string) (*rig, *recordingStore, string) {
	t.Helper()
	dir := t.TempDir()
	terminal := transitionJSON(KindStop, 2, 2, PhaseCleanup, string(oldEpoch("bus.one")), 1, false, false, 0, farFuture)
	seedControl(t, dir, 2, 3, map[string]string{"bus.one": recordJSON(DesiredStopped, 2, 2, terminal)}, material)
	r := durableRig(t, dir, func(f *testFactory) []InstanceConfig { return keysOf(f, true, "bus.one") })
	rs := &recordingStore{ControlStore: r.store, log: r.log}
	r.store = rs
	return r, rs, dir
}

// A terminal slot is pruned only after a successful Secure. Nonnil material is
// validated and committed before the prune; nil material preserves the committed
// envelope without a write and lets pruning proceed; a Secure error or a failed
// required commit keeps the slot.
func TestTerminalSlotIsPrunedOnlyAfterSecure(t *testing.T) {
	material := recoveryJSON([]string{string(ref("bus.one").ID())}, 1, []byte("committed-before"))
	committed := func(r *rig) *RecoveryMaterial { return r.store.Recovery() }

	t.Run("nonnil material commits before the prune", func(t *testing.T) {
		r, rs, _ := recoveryRig(t, material)
		defer r.close()
		fresh := &RecoveryMaterial{DriverIDs: []InstanceID{ref("bus.one").ID()}, MaxDesiredRevision: 2, Payload: []byte("secured-material")}
		r.recovery.secureReturn = func(InstanceID, Transition) *RecoveryMaterial { return fresh }
		r.build("inc2", false)
		r.start()
		settle(t, r, "bus.one", ObservedStopped)
		log := r.log.snapshot()
		commit, prune := -1, -1
		for i, c := range log {
			if c == "commit-recovery" && commit < 0 {
				commit = i
			}
			if c == "prune" {
				prune = i
			}
		}
		if commit < 0 || prune < 0 || commit > prune {
			t.Fatalf("the envelope was not committed before the prune: %v", log)
		}
		if got := committed(r); got == nil || string(got.Payload) != "secured-material" {
			t.Fatalf("committed = %+v", got)
		}
		_ = rs
	})
	t.Run("nil material keeps the envelope without a write", func(t *testing.T) {
		r, _, dir := recoveryRig(t, material)
		defer r.close()
		before, _ := os.ReadFile(filepath.Join(dir, controlFileName))
		var beforeEnvelope = decodeNamespace(before).recovery
		r.build("inc2", false)
		r.start()
		settle(t, r, "bus.one", ObservedStopped)
		if r.log.count("commit-recovery") != 0 {
			t.Fatal("nil material was written")
		}
		got := committed(r)
		if got == nil || string(got.Payload) != string(beforeEnvelope.Payload) {
			t.Fatalf("the committed envelope changed: %+v", got)
		}
	})
	t.Run("a Secure error keeps the slot", func(t *testing.T) {
		r, _, _ := recoveryRig(t, material)
		defer r.close()
		r.recovery.secureErr = errors.New("synthetic secure rejection")
		r.build("inc2", false)
		r.start()
		waitFor(t, "secure attempted", func() bool { return r.log.count("secure") >= 2 })
		rec, _ := r.store.Driver(ref("bus.one").ID())
		if rec.Current == nil || r.log.count("prune") != 0 {
			t.Fatalf("a failed Secure pruned the slot: %+v", rec.Current)
		}
	})
	t.Run("a failed required commit keeps the slot", func(t *testing.T) {
		r, rs, _ := recoveryRig(t, material)
		defer r.close()
		rs.failRecovery = errors.New("synthetic commit failure")
		r.recovery.secureReturn = func(InstanceID, Transition) *RecoveryMaterial {
			return &RecoveryMaterial{DriverIDs: []InstanceID{ref("bus.one").ID()}, MaxDesiredRevision: 2, Payload: []byte("x")}
		}
		r.build("inc2", false)
		r.start()
		waitFor(t, "commit attempted", func() bool { return r.log.count("commit-recovery") >= 2 })
		rec, _ := r.store.Driver(ref("bus.one").ID())
		if rec.Current == nil || r.log.count("prune") != 0 {
			t.Fatalf("a failed commit pruned the slot: %+v", rec.Current)
		}
	})
}

// With a terminal slot ahead of a reserved one, successful nil material lets the
// prune promote the reserved slot, which then runs.
func TestNilSecuredMaterialPromotesTheReservedSlot(t *testing.T) {
	dir := t.TempDir()
	stop := transitionJSON(KindStop, 2, 2, PhaseCleanup, string(oldEpoch("bus.one")), 1, false, false, 0, farFuture)
	start := transitionJSON(KindStart, 3, 3, PhasePending, "", 0, false, false, 0, "")
	record := fmt.Sprintf(`{"desired_state":"RUNNING","desired_revision":"3","lifecycle_sequence":"3","current":%s,"reserved":%s}`, stop, start)
	seedControl(t, dir, 3, 4, map[string]string{"bus.one": record}, recoveryJSON([]string{string(ref("bus.one").ID())}, 2, []byte("kept")))
	r := durableRig(t, dir, func(f *testFactory) []InstanceConfig { return keysOf(f, true, "bus.one") })
	defer r.close()
	r.build("inc2", false)
	r.start()
	settle(t, r, "bus.one", ObservedRunning)
	if r.log.count("commit-recovery") != 0 || string(r.store.Recovery().Payload) != "kept" {
		t.Fatal("nil material changed the envelope")
	}
	if got := r.recovery.securedTransitions(); len(got) < 1 || got[0].Kind != KindStop {
		t.Fatalf("secured = %+v", got)
	}
	if r.factory.constructed() != 1 {
		t.Fatalf("the promoted START constructed %d instances", r.factory.constructed())
	}
}

// A failed restoration blocks only the instances it names: a typed scope first,
// otherwise the committed envelope's ids, otherwise every configured instance.
// A blocked instance reaches no executor and no admission; the others start.
func TestRestoreFailureScope(t *testing.T) {
	ids := func(keys ...string) []InstanceID {
		var out []InstanceID
		for _, k := range keys {
			out = append(out, ref(k).ID())
		}
		return out
	}
	envelope := func(keys ...string) string {
		var text []string
		for _, id := range ids(keys...) {
			text = append(text, string(id))
		}
		sort.Strings(text)
		return recoveryJSON(text, 0, []byte("committed"))
	}
	cases := []struct {
		name    string
		seed    string
		err     error
		empty   bool
		blocked []string
	}{
		{"plain error with a known envelope blocks its drivers", envelope("bus.two"), errors.New("restore failed"), false, []string{"bus.two"}},
		{"a typed scope takes precedence over the envelope", envelope("bus.two"), &RestoreError{Drivers: ids("bus.three"), Err: errors.New("scoped")}, false, []string{"bus.three"}},
		{"a plain empty-restore error with no envelope blocks every instance", "", errors.New("empty restore failed"), true, []string{"bus.one", "bus.two", "bus.three"}},
		{"an empty typed scope with no envelope blocks every instance", "", &RestoreError{Err: errors.New("unscoped")}, true, []string{"bus.one", "bus.two", "bus.three"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.seed != "" {
				mark := map[string]string{}
				for i, k := range threeBuses {
					mark[k] = recordJSON(DesiredRunning, uint64(i+1), 0, "")
				}
				seedControl(t, dir, 3, 4, mark, tc.seed)
			}
			r := durableRig(t, dir, func(f *testFactory) []InstanceConfig { return keysOf(f, true, threeBuses...) })
			if tc.empty {
				r.recovery.emptyErr = tc.err
			} else {
				r.recovery.restoreErr = tc.err
			}
			r.build("inc1", tc.empty)
			err := r.sup.Start(context.Background())
			if err == nil {
				t.Fatal("a failed restoration reported no error")
			}
			defer r.close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.sup.WaitInitial(ctx); err != nil {
				t.Fatal(err)
			}
			blocked := map[string]bool{}
			for _, k := range tc.blocked {
				blocked[k] = true
			}
			for _, key := range threeBuses {
				st := r.status(key)
				if blocked[key] {
					if st.ObservedState != ObservedFailed || len(r.instancesOf(key)) != 0 {
						t.Fatalf("%s should be withheld: %+v", key, st)
					}
					for _, c := range r.pub.openedSessions() {
						if c.InstanceID == ref(key).ID() {
							t.Fatalf("%s reached admission", key)
						}
					}
				} else if st.ObservedState != ObservedRunning {
					t.Fatalf("%s should start normally: %+v", key, st)
				}
			}
			log := r.log.snapshot()
			if len(log) == 0 || (log[0] != "restore" && log[0] != "restore-empty") {
				t.Fatalf("restore is not the first call: %v", log)
			}
		})
	}
}

// Recovery ordering: the adapter's Restore is the first call, before any
// executor call or admission.
func TestRestoreIsTheFirstCall(t *testing.T) {
	dir := t.TempDir()
	seedControl(t, dir, 1, 2, map[string]string{"bus.one": recordJSON(DesiredRunning, 1, 0, "")},
		recoveryJSON([]string{string(ref("bus.one").ID())}, 1, []byte("pre-loss")))
	r := durableRig(t, dir, func(f *testFactory) []InstanceConfig { return keysOf(f, true, "bus.one") })
	defer r.close()
	r.build("inc2", true)
	r.start()
	if first := r.log.snapshot()[0]; first != "restore" {
		t.Fatalf("first call = %s", first)
	}
	r.recovery.mu.Lock()
	got := r.recovery.restored
	r.recovery.mu.Unlock()
	if len(got) != 1 || got[0] == nil || string(got[0].Payload) != "pre-loss" {
		t.Fatalf("restored = %+v", got)
	}
}

// A second host that opens while the owner holds the store lock preserves the
// last committed read-only view and stops before Restore, executor calls and
// admission: it makes no charge and no control write.
func TestSecondHostIsReadOnlyAndTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	seedControl(t, dir, 1, 2, map[string]string{
		"bus.one": recordJSON(DesiredRunning, 1, 1, activeTransition(KindRestart, 1, 1, "bus.one")),
	}, recoveryJSON([]string{string(ref("bus.one").ID())}, 1, []byte("owner-material")))
	owner, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Release()
	before, _ := os.ReadFile(filepath.Join(dir, controlFileName))

	r := newRig(t, nil, func(f *testFactory) []InstanceConfig { return keysOf(f, true, "bus.one") })
	second, err := OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	r.store = second
	r.build("inc2", true)
	err = r.sup.Start(context.Background())
	refusalOf(t, err, RefusalFailedPrecondition, 3)
	defer r.close()
	if second.Exclusive() {
		t.Fatal("the second host holds the lock")
	}
	if len(r.log.snapshot()) != 0 {
		t.Fatalf("the second host made calls: %v", r.log.snapshot())
	}
	if r.factory.constructed() != 0 || len(r.pub.openedSessions()) != 0 || len(r.pub.withdrawals()) != 0 {
		t.Fatal("the second host constructed or admitted something")
	}
	after, _ := os.ReadFile(filepath.Join(dir, controlFileName))
	if string(after) != string(before) {
		t.Fatal("the second host wrote the control state")
	}
	rec, err := second.Driver(ref("bus.one").ID())
	if err != nil || !rec.ReadOnly || rec.DesiredRevision != 1 || rec.Current == nil || rec.Current.Kind != KindRestart || rec.Current.AttemptsUsed != 1 {
		t.Fatalf("read-only view = %+v %v", rec, err)
	}
	st := r.status("bus.one")
	if st.ObservedState != ObservedFailed || st.DesiredState != DesiredRunning || st.DesiredRevision != 1 {
		t.Fatalf("withheld view = %+v", st)
	}
	_ = driver.Config{}
}

// The recovered executor proves the native close of an earlier process only for
// the handles that process owned, and only while this process holds the store
// exclusively; an unconfirmed or unknown owner never yields a successful native
// close.
func TestRecoveredExecutorProvesCloseOnlyForTheFormerProcess(t *testing.T) {
	build := func(exclusive bool) (*instanceExecutor, *testPublication, *testFactory) {
		r := newRig(t, NewMemoryStore(), func(f *testFactory) []InstanceConfig { return keysOf(f, true, "bus.one") })
		r.build("inc2", false)
		sv := r.sup.byID[ref("bus.one").ID()]
		sv.executor.exclusive = func() bool { return exclusive }
		t.Cleanup(r.close)
		return sv.executor, r.pub, r.factory
	}
	former := LifecycleCorrelation{InstanceID: ref("bus.one").ID(), Epoch: oldEpoch("bus.one"), Generation: 1, Sequence: 1}

	exec, pub, factory := build(true)
	out := exec.Stop(context.Background(), former)
	if !out.NativeClosed || !out.SemanticCleaned || factory.constructed() != 0 || len(pub.withdrawals()) != 1 {
		t.Fatalf("former process, lock held: %+v", out)
	}
	exec, pub, _ = build(false)
	out = exec.Stop(context.Background(), former)
	if out.NativeClosed || !out.SemanticCleaned || len(pub.withdrawals()) != 1 {
		t.Fatalf("former process, lock not held: %+v", out)
	}
	exec, pub, _ = build(true)
	pub.withdrawErr = errors.New("withdrawal pending")
	out = exec.Stop(context.Background(), former)
	if !out.NativeClosed || out.SemanticCleaned {
		t.Fatalf("native settled, semantic pending: %+v", out)
	}
	foreign := former
	foreign.InstanceID = ref("bus.other").ID()
	if out := exec.Stop(context.Background(), foreign); out.NativeClosed || out.SemanticCleaned {
		t.Fatalf("a foreign instance was settled: %+v", out)
	}
}

// A native close that stays unconfirmed while the semantic cleanup settled
// quarantines the instance and blocks every successor construct; a settled
// native close with a pending semantic result leaves the accepted correlation
// pending.
func TestUnsettledCloseKeepsTheAcceptedCorrelationPending(t *testing.T) {
	t.Run("native close unconfirmed", func(t *testing.T) {
		r := newRig(t, NewMemoryStore(), func(f *testFactory) []InstanceConfig {
			f.script["bus.one"] = &startScript{stopReport: &driver.CloseReport{Confirmed: false, Err: errors.New("not proven")}}
			return keysOf(f, true, "bus.one")
		})
		defer r.close()
		r.build("inc1", false)
		r.start()
		r.decide("bus.one", MethodRestart)
		waitFor(t, "quarantine", func() bool { return r.status("bus.one").SafetyQuarantined })
		built := r.factory.constructed()
		st := r.status("bus.one")
		_, err := r.sup.SetDesired(context.Background(), ref("bus.one"), MethodRestart, st.DesiredRevision)
		refusalOf(t, err, RefusalFailedPrecondition, 7)
		time.Sleep(100 * time.Millisecond)
		if r.factory.constructed() != built || st.ObservedState != ObservedFailed || st.Reason != ReasonCloseUnconfirmed {
			t.Fatalf("a successor was constructed or the state is wrong: %+v (constructs %d -> %d)", st, built, r.factory.constructed())
		}
		rec, _ := r.store.Driver(ref("bus.one").ID())
		if rec.Current == nil || !rec.Current.NativeClosePending {
			t.Fatalf("the unconfirmed close was dropped: %+v", rec.Current)
		}
	})
	t.Run("semantic cleanup pending", func(t *testing.T) {
		r := newRig(t, NewMemoryStore(), func(f *testFactory) []InstanceConfig { return keysOf(f, true, "bus.one") })
		defer r.close()
		r.build("inc1", false)
		r.start()
		r.pub.mu.Lock()
		r.pub.withdrawErr = errors.New("withdrawal pending")
		r.pub.mu.Unlock()
		r.decide("bus.one", MethodStop)
		waitFor(t, "semantic cleanup attempted", func() bool { return len(r.pub.withdrawals()) >= 1 })
		waitFor(t, "slot persisted with the pending obligation", func() bool {
			rec, _ := r.store.Driver(ref("bus.one").ID())
			return rec.Current != nil && rec.Current.Kind == KindStop && !rec.Current.NativeClosePending && rec.Current.SemanticCleanupPending
		})
		if got := r.status("bus.one").ObservedState; got == ObservedStopped {
			t.Fatal("stopped was reported with the semantic cleanup pending")
		}
	})
}
