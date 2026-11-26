package life

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

var threeBuses = []string{"bus.one", "bus.two", "bus.three"}

func threeBusRig(t *testing.T, store ControlStore) *rig {
	t.Helper()
	return newRig(t, store, func(f *testFactory) []InstanceConfig {
		var out []InstanceConfig
		for i, key := range threeBuses {
			cfg := InstanceConfig{Driver: f.kind, Instance: driver.InstanceKey(key), Enabled: true}
			cfg.Config.Transport = transportJSON(claimSpec{Provider: "provider.a", Config: "config." + key, Domain: "domain.one", Exclusive: true})
			_ = i
			out = append(out, cfg)
		}
		return out
	})
}

func (r *rig) decide(key string, method Method) DesiredView {
	r.t.Helper()
	// A decision made while the previous decision's outcome is still being
	// resolved is refused as busy; it is made again once that settles.
	deadline := time.Now().Add(5 * time.Second)
	for {
		view, err := r.sup.SetDesired(context.Background(), ref(key), method, r.status(key).DesiredRevision)
		var refusal *Refusal
		if errors.As(err, &refusal) && refusal.Code == RefusalTransitionBusy && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
			continue
		}
		if err != nil {
			r.t.Fatalf("%s %s: %v", method, key, err)
		}
		return view
	}
}

func (r *rig) instancesOf(key string) []*testInstance {
	r.factory.mu.Lock()
	defer r.factory.mu.Unlock()
	var out []*testInstance
	for _, inst := range r.factory.made {
		if inst.plan.Instance == driver.InstanceKey(key) {
			out = append(out, inst)
		}
	}
	return out
}

// Three instances of one driver kind, each with its own transport claim, start,
// stop and restart independently; the epoch and generation of each are reported,
// and a restart takes a fresh source epoch whose first attempt is generation 1.
func TestThreeInstancesOfOneKindRunIndependently(t *testing.T) {
	t.Parallel()
	r := threeBusRig(t, NewMemoryStore())
	defer r.close()
	r.build("inc1", false)
	r.start()

	epochs := map[string]SourceEpoch{}
	for _, key := range threeBuses {
		st := r.status(key)
		if st.ObservedState != ObservedRunning || st.Generation != 1 || st.Epoch == "" {
			t.Fatalf("%s = %+v", key, st)
		}
		for other, e := range epochs {
			if e == st.Epoch {
				t.Fatalf("%s and %s share the epoch %s", key, other, e)
			}
		}
		epochs[key] = st.Epoch
	}
	if got := r.factory.constructed(); got != 3 {
		t.Fatalf("constructed %d instances", got)
	}

	// Stopping one leaves the others running.
	r.decide("bus.two", MethodStop)
	r.waitState("bus.two", ObservedStopped)
	if r.status("bus.one").ObservedState != ObservedRunning || r.status("bus.three").ObservedState != ObservedRunning {
		t.Fatal("stopping one instance disturbed another")
	}
	if got := r.instancesOf("bus.two")[0].stopCount(); got != 1 {
		t.Fatalf("stop count = %d", got)
	}
	if r.instancesOf("bus.one")[0].stopCount() != 0 || r.instancesOf("bus.three")[0].stopCount() != 0 {
		t.Fatal("a sibling was stopped")
	}

	// Starting it again opens a new epoch at generation 1.
	r.decide("bus.two", MethodStart)
	waitFor(t, "bus.two running again", func() bool { return r.status("bus.two").ObservedState == ObservedRunning })
	again := r.status("bus.two")
	if again.Generation != 1 || again.Epoch == epochs["bus.two"] {
		t.Fatalf("start after stop = %+v", again)
	}

	// Restart: the predecessor is closed and withdrawn under its own
	// correlation, then the replacement runs in a fresh epoch at generation 1.
	predecessor := again.Epoch
	r.decide("bus.one", MethodRestart)
	waitFor(t, "bus.one restarted", func() bool {
		st := r.status("bus.one")
		return st.ObservedState == ObservedRunning && st.Epoch != epochs["bus.one"]
	})
	restarted := r.status("bus.one")
	if restarted.Generation != 1 {
		t.Fatalf("a restart's first attempt is generation %d", restarted.Generation)
	}
	one := r.instancesOf("bus.one")
	if len(one) != 2 || one[0].stopCount() != 1 || one[1].stopCount() != 0 {
		t.Fatalf("restart instances: %d, stops %d", len(one), one[0].stopCount())
	}
	withdrawn := r.pub.withdrawals()
	foundOld := false
	for _, w := range withdrawn {
		foundOld = foundOld || (w.Epoch == epochs["bus.one"] && w.Generation == 1)
		if w.Epoch == restarted.Epoch {
			t.Fatalf("the successor was withdrawn: %+v", w)
		}
	}
	if !foundOld {
		t.Fatalf("the predecessor was not withdrawn: %+v", withdrawn)
	}
	if r.status("bus.three").Epoch != epochs["bus.three"] || r.status("bus.two").Epoch != predecessor {
		t.Fatal("a sibling's epoch moved")
	}
}

// A late report carrying a fenced correlation is discarded: the predecessor
// epoch's report at the same numeric generation as the successor changes neither
// the successor's observation nor its publication, and the fresh epoch's own
// report is accepted.
func TestLateReportsOfAFencedCorrelationAreDiscarded(t *testing.T) {
	t.Parallel()
	r := threeBusRig(t, NewMemoryStore())
	defer r.close()
	r.build("inc1", false)
	r.start()
	old := r.status("bus.one")
	oldCorr := LifecycleCorrelation{InstanceID: ref("bus.one").ID(), Epoch: old.Epoch, Generation: old.Generation}

	r.decide("bus.one", MethodRestart)
	waitFor(t, "bus.one restarted", func() bool {
		st := r.status("bus.one")
		return st.ObservedState == ObservedRunning && st.Epoch != old.Epoch
	})
	fresh := r.status("bus.one")
	if fresh.Generation != old.Generation {
		t.Fatalf("the successor generation %d differs from the predecessor's %d", fresh.Generation, old.Generation)
	}
	opened := len(r.pub.openedSessions())

	// The predecessor, now fenced, reports a failure at the same numeric
	// generation: the service queues nothing for it and the view is unchanged.
	r.sup.service.ObserveNative(oldCorr, NativeObservation{State: ObservedFailed, Reason: ReasonInternalError})
	time.Sleep(50 * time.Millisecond)
	if got := r.status("bus.one"); got.ObservedState != ObservedRunning || got.Epoch != fresh.Epoch || got.Reason != ReasonNone {
		t.Fatalf("a fenced report changed the successor: %+v", got)
	}
	if len(r.pub.openedSessions()) != opened {
		t.Fatal("a fenced report changed the publication")
	}

	// The fresh epoch's own report is accepted.
	freshCorr := LifecycleCorrelation{InstanceID: ref("bus.one").ID(), Epoch: fresh.Epoch, Generation: fresh.Generation}
	r.sup.service.ObserveNative(freshCorr, NativeObservation{State: ObservedDegraded, Reason: ReasonCapabilityDegraded})
	waitFor(t, "the fresh report", func() bool { return r.status("bus.one").ObservedState == ObservedDegraded })
}

// A disabled instance is never constructed and its configured link node stays
// declared, whatever state the instance is in: disabled, running, stopped,
// failed or quarantined.
func TestDisabledInstanceNeverConstructsAndLinkNodesPersist(t *testing.T) {
	t.Parallel()
	spec := func(key string) claimSpec {
		return claimSpec{Provider: "provider.a", Config: "config." + key, Domain: "domain.one", Exclusive: true}
	}
	r := newRig(t, NewMemoryStore(), func(f *testFactory) []InstanceConfig {
		off := withClaims(f, "bus.off", false, spec("off"))
		on := withClaims(f, "bus.on", true, spec("on"))
		bad := withClaims(f, "bus.bad", true, spec("bad"))
		quar := withClaims(f, "bus.quar", true, spec("quar"))
		f.script["bus.bad"] = &startScript{startErrs: []error{errors.New("terminal")}}
		f.script["bus.quar"] = &startScript{stopReport: &driver.CloseReport{Confirmed: false, Err: errors.New("close not proven")}}
		return []InstanceConfig{off, on, bad, quar}
	})
	defer r.close()
	declared := nodeTexts(r.set.Nodes)
	if len(declared) != 5 || len(r.set.Edges) != 4 {
		t.Fatalf("declared nodes %d edges %d", len(declared), len(r.set.Edges))
	}
	r.build("inc1", false)
	r.start()
	if got := r.status("bus.off"); got.ObservedState != ObservedDisabled || got.Enabled {
		t.Fatalf("disabled = %+v", got)
	}
	if len(r.instancesOf("bus.off")) != 0 {
		t.Fatal("a disabled instance was constructed")
	}
	r.waitState("bus.bad", ObservedFailed)
	r.decide("bus.on", MethodStop)
	r.waitState("bus.on", ObservedStopped)
	r.decide("bus.quar", MethodStop)
	waitFor(t, "quarantine", func() bool { return r.status("bus.quar").SafetyQuarantined })
	for state, key := range map[ObservedState]string{ObservedDisabled: "bus.off", ObservedStopped: "bus.on", ObservedFailed: "bus.bad"} {
		if r.status(key).ObservedState != state {
			t.Fatalf("%s = %+v, want %s", key, r.status(key), state)
		}
	}
	if !reflect.DeepEqual(nodeTexts(r.sup.set.Nodes), declared) {
		t.Fatal("a lifecycle state removed a configured node from the plan")
	}
	if _, err := r.sup.SetDesired(context.Background(), ref("bus.off"), MethodStart, r.status("bus.off").DesiredRevision); err == nil {
		t.Fatal("a disabled instance accepted a decision")
	}
}

// WaitInitial returns only once every started instance has committed its
// initial publication or reported its outcome.
func TestWaitInitialWaitsForTheInitialPublicationOfEveryStartedInstance(t *testing.T) {
	t.Parallel()
	r := threeBusRig(t, NewMemoryStore())
	defer r.close()
	release := make(chan struct{})
	r.factory.script["bus.two"] = &startScript{stage: release}
	r.build("inc1", false)
	if err := r.sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		done <- r.sup.WaitInitial(ctx)
	}()
	waitFor(t, "the unstaged instances running", func() bool {
		return r.status("bus.one").ObservedState == ObservedRunning && r.status("bus.three").ObservedState == ObservedRunning
	})
	select {
	case err := <-done:
		t.Fatalf("WaitInitial returned before bus.two committed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if got := r.status("bus.two").ObservedState; got != ObservedStarting {
		t.Fatalf("bus.two announced %s before its publication committed", got)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("WaitInitial did not return after the commit")
	}
	if got := r.status("bus.two").ObservedState; got != ObservedRunning {
		t.Fatalf("bus.two = %s", got)
	}
}

// An instance that reports a failed outcome also releases WaitInitial.
func TestWaitInitialReturnsOnAFailedOutcome(t *testing.T) {
	t.Parallel()
	r := newRig(t, NewMemoryStore(), func(f *testFactory) []InstanceConfig {
		f.script["bus.bad"] = &startScript{startErrs: []error{errors.New("terminal")}}
		return instanceConfigs(f, true, nil, "bus.bad", "bus.good")
	})
	defer r.close()
	r.build("inc1", false)
	r.start()
	if got := r.status("bus.bad"); got.ObservedState != ObservedFailed {
		t.Fatalf("bus.bad = %+v", got)
	}
	if got := r.status("bus.good"); got.ObservedState != ObservedRunning {
		t.Fatalf("bus.good = %+v", got)
	}
}

// A rejected plan set installs nothing and does not stop what runs: the plan
// error leaves the running supervisor and its instances untouched.
func TestRejectedPlanSetLeavesRunningInstancesAlone(t *testing.T) {
	t.Parallel()
	r := threeBusRig(t, NewMemoryStore())
	defer r.close()
	r.build("inc1", false)
	r.start()
	shared := claimSpec{Provider: "provider.a", Config: "config.shared", Domain: "domain.one", Exclusive: true}
	other := newTestFactory("driver.other")
	_, err := Plan(planInstallation(other, withClaims(other, "x.one", true, shared), withClaims(other, "x.two", true, shared)), other)
	planError(t, err)
	for _, key := range threeBuses {
		if r.status(key).ObservedState != ObservedRunning || r.instancesOf(key)[0].stopCount() != 0 {
			t.Fatalf("%s was disturbed by a rejected plan", key)
		}
	}
	if other.constructed() != 0 {
		t.Fatal("a rejected set constructed an instance")
	}
}

// SetDesired takes the desired revision the caller last saw as its precondition:
// a stale value is refused, and the observed generation is not that check.
func TestSetDesiredPreconditionIsTheDesiredRevision(t *testing.T) {
	t.Parallel()
	r := threeBusRig(t, NewMemoryStore())
	defer r.close()
	r.build("inc1", false)
	r.start()
	st := r.status("bus.two")
	if st.DesiredRevision == DesiredRevision(st.Generation) {
		t.Fatalf("the fixture does not tell the desired revision %d from the generation %d", st.DesiredRevision, st.Generation)
	}
	_, err := r.sup.SetDesired(context.Background(), ref("bus.two"), MethodStop, DesiredRevision(st.Generation))
	refusalOf(t, err, RefusalRevisionConflict, 6)
	_, err = r.sup.SetDesired(context.Background(), ref("bus.two"), MethodStop, st.DesiredRevision+7)
	refusalOf(t, err, RefusalRevisionConflict, 6)
	var view DesiredView
	waitFor(t, "stop decision after the previous decision settled", func() bool {
		var refusal *Refusal
		view, err = r.sup.SetDesired(context.Background(), ref("bus.two"), MethodStop, st.DesiredRevision)
		return !(errors.As(err, &refusal) && refusal.Code == RefusalTransitionBusy)
	})
	if err != nil || view.DesiredState != DesiredStopped || view.DesiredRevision <= st.DesiredRevision {
		t.Fatalf("stop = %+v %v", view, err)
	}
	r.waitState("bus.two", ObservedStopped)
	_, err = r.sup.SetDesired(context.Background(), Ref{Driver: testKind, Instance: "bus.unknown"}, MethodStop, 1)
	refusalOf(t, err, RefusalNotFound, 2)
}

// Watch returns the state and a channel closed at the instance's next change.
func TestWatchReportsTheNextChange(t *testing.T) {
	t.Parallel()
	r := threeBusRig(t, NewMemoryStore())
	defer r.close()
	r.build("inc1", false)
	r.start()
	status, changed, err := r.sup.Watch(ref("bus.one"))
	if err != nil || status.ObservedState != ObservedRunning {
		t.Fatalf("watch = %+v %v", status, err)
	}
	select {
	case <-changed:
		t.Fatal("the channel closed without a change")
	case <-time.After(50 * time.Millisecond):
	}
	r.decide("bus.one", MethodStop)
	select {
	case <-changed:
	case <-time.After(10 * time.Second):
		t.Fatal("the channel did not close at the change")
	}
	if _, _, err := r.sup.Watch(Ref{Driver: testKind, Instance: "bus.unknown"}); err == nil {
		t.Fatal("an unknown instance was watched")
	}
}

// Close stops every instance and relinquishes the store only when every close
// was proven.
func TestCloseStopsInstancesAndRelinquishesTheStoreOnlyWhenProven(t *testing.T) {
	t.Parallel()
	for _, proven := range []bool{true, false} {
		dir := t.TempDir()
		store, err := OpenFileStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		r := newRig(t, store, func(f *testFactory) []InstanceConfig {
			if !proven {
				f.script["bus.one"] = &startScript{stopReport: &driver.CloseReport{Confirmed: false, Err: errors.New("not proven")}}
			}
			return instanceConfigs(f, true, nil, "bus.one", "bus.two")
		})
		r.build("inc1", false)
		r.start()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		closeErr := r.sup.Close(ctx)
		cancel()
		if proven && closeErr != nil {
			t.Fatalf("proven close returned %v", closeErr)
		}
		if !proven && !errors.Is(closeErr, errNativeCloseUnproven) {
			t.Fatalf("an unproven native close returned %v, want %v", closeErr, errNativeCloseUnproven)
		}
		for _, key := range []string{"bus.one", "bus.two"} {
			if got := r.instancesOf(key)[0].stopCount(); got != 1 {
				t.Fatalf("proven=%v: %s stop count %d", proven, key, got)
			}
		}
		if got := store.Exclusive(); got == proven {
			t.Fatalf("proven=%v: the store lock is held=%v", proven, got)
		}
		store.Release()
	}
}

func TestNewSupervisorRequiresItsInputs(t *testing.T) {
	t.Parallel()
	r := threeBusRig(t, NewMemoryStore())
	good := Options{Store: r.store, Recovery: r.recovery, Publication: r.pub, Incarnation: "inc1"}
	for name, mutate := range map[string]func(*Options){
		"store":       func(o *Options) { o.Store = nil },
		"recovery":    func(o *Options) { o.Recovery = nil },
		"publication": func(o *Options) { o.Publication = nil },
		"incarnation": func(o *Options) { o.Incarnation = "" },
	} {
		opts := good
		mutate(&opts)
		if _, err := NewSupervisor(r.set, opts); err == nil {
			t.Errorf("a missing %s was accepted", name)
		}
	}
	noClock := r.set
	noClock.Clock = nil
	if _, err := NewSupervisor(noClock, good); err == nil {
		t.Error("a missing clock was accepted")
	}
}

// advanceFailStore is a control store whose catalog advance can be made to fail.
type advanceFailStore struct {
	ControlStore
	fail atomic.Bool
}

func (a *advanceFailStore) AdvanceCatalog(ctx context.Context) (CatalogRevision, error) {
	if a.fail.Load() {
		return 0, errors.New("synthetic advance failure")
	}
	return a.ControlStore.AdvanceCatalog(ctx)
}

// A failed initial view commit leaves every launched instance absent from the
// cached view; WaitInitial stays pending for it until the view shows a
// publication or an outcome, and never counts the absence as a reported failure.
func TestWaitInitialStaysPendingWhenTheInitialViewCommitFails(t *testing.T) {
	t.Parallel()
	store := &advanceFailStore{ControlStore: NewMemoryStore()}
	store.fail.Store(true)
	r := threeBusRig(t, store)
	defer r.close()
	release := make(chan struct{})
	r.factory.script["bus.two"] = &startScript{stage: release}
	r.build("inc1", false)
	if err := r.sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := r.sup.WaitInitial(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitInitial = %v, want it pending while the initial view is uncommitted", err)
	}
	store.fail.Store(false)
	close(release)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if err := r.sup.WaitInitial(ctx2); err != nil {
		t.Fatal(err)
	}
	for _, key := range threeBuses {
		if got := r.status(key).ObservedState; got != ObservedRunning {
			t.Fatalf("%s = %s", key, got)
		}
	}
}

// A failed initial view commit leaves the cached view empty while enabled
// instances are starting. Snapshot and Watch must not turn that absence into
// a desired or observed outcome: a committed failure is an entry of the view,
// with a desired state and an observed failure, and absence is neither.
func TestSnapshotAndWatchDoNotInventAFailureWhileTheViewIsEmpty(t *testing.T) {
	t.Parallel()
	store := &advanceFailStore{ControlStore: NewMemoryStore()}
	store.fail.Store(true)
	r := threeBusRig(t, store)
	defer r.close()
	r.factory.script["bus.one"] = &startScript{startErrs: []error{errors.New("synthetic start failure")}}
	r.build("inc1", false)
	if err := r.sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.sup.service.List().Drivers; len(got) != 0 {
		t.Fatalf("cached view carries %d instances", len(got))
	}
	for _, key := range threeBuses {
		snap, err := r.sup.Snapshot(ref(key))
		if reported, why := outcomeReported(snap, err); reported {
			t.Fatalf("snapshot of %s reported an outcome while the view does not carry it: %s", key, why)
		}
		if !errors.Is(err, ErrInstanceAbsent) {
			t.Fatalf("snapshot of %s error = %v, want %v", key, err, ErrInstanceAbsent)
		}
		watched, _, werr := r.sup.Watch(ref(key))
		if reported, why := outcomeReported(watched, werr); reported {
			t.Fatalf("watch of %s reported an outcome while the view does not carry it: %s", key, why)
		}
		if !errors.Is(werr, ErrInstanceAbsent) {
			t.Fatalf("watch of %s error = %v, want %v", key, werr, ErrInstanceAbsent)
		}
	}

	store.fail.Store(false)
	waitFor(t, "committed failure of bus.one", func() bool {
		st, err := r.sup.Snapshot(ref("bus.one"))
		return err == nil && st.DesiredState == DesiredRunning && st.ObservedState == ObservedFailed && st.Reason == ReasonInternalError
	})
	failed, err := r.sup.Snapshot(ref("bus.one"))
	if err != nil || failed.DesiredState != DesiredRunning || failed.ObservedState != ObservedFailed || failed.Reason != ReasonInternalError {
		t.Fatalf("committed failure = %+v, %v", failed, err)
	}
	watched, _, werr := r.sup.Watch(ref("bus.one"))
	if werr != nil || watched.DesiredState != DesiredRunning || watched.ObservedState != ObservedFailed || watched.Reason != ReasonInternalError {
		t.Fatalf("watch of the committed failure = %+v, %v", watched, werr)
	}
}

// outcomeReported is a desired or observed outcome. Absence is an error with
// neither state filled; a committed failure is a nil error with both filled.
func outcomeReported(st Status, err error) (bool, string) {
	if err == nil {
		return true, fmt.Sprintf("nil error with status %+v", st)
	}
	if st.DesiredState != "" || st.ObservedState != "" {
		return true, fmt.Sprintf("error %v with status %+v", err, st)
	}
	var refusal *Refusal
	if errors.As(err, &refusal) {
		return true, fmt.Sprintf("refusal %+v", refusal)
	}
	return false, ""
}
