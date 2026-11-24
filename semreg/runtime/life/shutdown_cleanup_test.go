package life

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contract "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func fileRig(t *testing.T, retry contract.RetryPolicy, script *startScript, keys ...string) (*rig, *FileStore) {
	t.Helper()
	store, err := OpenFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Release)
	r := newRig(t, store, func(f *testFactory) []InstanceConfig {
		f.retry = retry
		for _, key := range keys {
			f.script[contract.InstanceKey(key)] = script
		}
		return keysOf(f, true, keys...)
	})
	return r, store
}

// An instance the factory constructed is stopped when its publication session
// cannot be opened, and that stop's result is the close evidence: a confirmed
// close releases the store lock at shutdown, an unconfirmed one quarantines the
// instance and keeps the lock.
func TestSessionOpenFailureStopsTheConstructedInstance(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		name := "unconfirmed"
		var script *startScript
		if confirmed {
			name = "confirmed"
		} else {
			script = &startScript{stopReport: &contract.CloseReport{Confirmed: false, Err: errors.New("not proven")}}
		}
		t.Run(name, func(t *testing.T) {
			r, store := fileRig(t, contract.RetryPolicy{}, script, "bus.one")
			r.pub.openErr = errors.New("session refused")
			r.build("inc1", false)
			r.start()
			st := r.waitState("bus.one", ObservedFailed)
			inst := r.instancesOf("bus.one")
			if len(inst) != 1 || inst[0].stopCount() != 1 {
				t.Fatalf("the constructed instance was not stopped: %d instances", len(inst))
			}
			if st.SafetyQuarantined == confirmed {
				t.Fatalf("quarantine = %v for a confirmed=%v close: %+v", st.SafetyQuarantined, confirmed, st)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = r.sup.Close(ctx)
			if store.Exclusive() == confirmed {
				t.Fatalf("store lock held = %v after a confirmed=%v close", store.Exclusive(), confirmed)
			}
		})
	}
}

// A start that fails after the instance began to publish withdraws what its
// generation registered before the failure is returned. A withdrawal that fails
// stays pending: the retry settles it before it constructs anything.
func TestFailedStartWithdrawsItsPublications(t *testing.T) {
	t.Run("withdrawn", func(t *testing.T) {
		r := single(t, contract.RetryPolicy{}, &startScript{startErrs: []error{errors.New("terminal after opening")}}, "bus.one")
		defer r.close()
		r.build("inc1", false)
		r.start()
		r.waitState("bus.one", ObservedFailed)
		opened, withdrawn := r.pub.openedSessions(), r.pub.withdrawals()
		if len(opened) != 1 || len(withdrawn) == 0 || withdrawn[0] != opened[0] {
			t.Fatalf("opened %+v, withdrawn %+v", opened, withdrawn)
		}
	})
	t.Run("pending withdrawal blocks the retry", func(t *testing.T) {
		retry := contract.RetryPolicy{Budget: 1000000, InitialDelay: 20 * time.Millisecond, MaxDelay: 20 * time.Millisecond, JitterRatio: -1}
		r := single(t, retry, &startScript{startErrs: []error{&retryableError{"retryable after opening"}}}, "bus.one")
		defer r.close()
		r.pub.withdrawErr = errors.New("withdrawal refused")
		r.build("inc1", false)
		if err := r.sup.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "a refused retry", func() bool { return len(r.pub.withdrawals()) >= 3 })
		if got := r.factory.constructed(); got != 1 {
			t.Fatalf("constructed %d instances over a pending withdrawal", got)
		}
		r.pub.mu.Lock()
		r.pub.withdrawErr = nil
		r.pub.mu.Unlock()
		r.waitState("bus.one", ObservedRunning)
		calls := r.log.snapshot()
		constructs := 0
		for i, call := range calls {
			if call != "construct" {
				continue
			}
			if constructs++; constructs == 2 && (i == 0 || calls[i-1] != "withdraw") {
				t.Fatalf("the replacement was constructed before a withdrawal settled: %v", calls)
			}
		}
		if constructs < 2 {
			t.Fatalf("no replacement was constructed: %v", calls)
		}
	})
}

// Shutdown withdraws the generations the instances published once their native
// owners stopped, once each, and reports a withdrawal that failed.
func TestShutdownWithdrawsAdmittedGenerations(t *testing.T) {
	r := threeBusRig(t, NewMemoryStore())
	r.build("inc1", false)
	r.start()
	byKey := map[string]LifecycleCorrelation{}
	for _, key := range threeBuses {
		st := r.status(key)
		byKey[key] = LifecycleCorrelation{InstanceID: ref(key).ID(), Epoch: st.Epoch, Generation: st.Generation}
	}
	r.decide("bus.two", MethodStop)
	r.waitState("bus.two", ObservedStopped)
	stoppedWithdrawals := len(r.pub.withdrawals())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.sup.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	count := map[LifecycleCorrelation]int{}
	for _, w := range r.pub.withdrawals() {
		count[w]++
	}
	for key, corr := range byKey {
		if count[corr] != 1 {
			t.Fatalf("%s: %d withdrawals of %+v after shutdown (%d before it)", key, count[corr], corr, stoppedWithdrawals)
		}
	}

	failing := threeBusRig(t, NewMemoryStore())
	failing.build("inc1", false)
	failing.start()
	failing.pub.mu.Lock()
	failing.pub.withdrawErr = errors.New("withdrawal refused")
	failing.pub.mu.Unlock()
	if err := failing.sup.Close(ctx); err == nil || !strings.Contains(err.Error(), "withdrawal refused") {
		t.Fatalf("close = %v; want the failed withdrawal reported", err)
	}
}

// Closing the durable store waits for the write in flight: the lock is never
// releasable while a write of this owner can still land.
func TestFileStoreCloseDrainsTheWriteInFlight(t *testing.T) {
	hooks := &blockHooks{}
	s, _ := seededStore(t, hooks)
	entered, release := hooks.arm()
	decided := make(chan error, 1)
	go func() {
		_, err := s.Decide(storeCtx(t), MethodStop, Request{InstanceID: "bus.primary", ExpectedRevision: 2}, startPlanner)
		decided <- err
	}()
	<-entered
	closed := make(chan struct{})
	go func() {
		s.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while a write was in flight")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return after the write finished")
	}
	if err := <-decided; err != nil {
		t.Fatalf("the in-flight decision: %v", err)
	}
	if _, err := s.Decide(storeCtx(t), MethodStart, Request{InstanceID: "bus.primary", ExpectedRevision: 3}, startPlanner); err == nil {
		t.Fatal("a write was admitted after Close")
	}
}

// A stop that interrupts a start in flight proves nothing while the canceled
// construction can still admit late; the retirement proves it afterwards.
func TestStopDuringStartingIsNotProvenWhileConstructionIsOutstanding(t *testing.T) {
	runtime := &lateAdmissionRuntime{startEntered: make(chan struct{}), startRelease: make(chan struct{})}
	manager, err := NewManager(ManagerConfig{Drivers: []ManagedConfig{{
		ID: "bus.primary", Enabled: true, Runtime: runtime, Capabilities: []Capability{CapabilityRead},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartAsync(context.Background(), "bus.primary"); err != nil {
		t.Fatal(err)
	}
	<-runtime.startEntered
	report, err := manager.StopWithReport(context.Background(), "bus.primary")
	if err != nil {
		t.Fatal(err)
	}
	if report.Class.Proven() {
		t.Fatalf("close class %v is proven while the construction is outstanding", report.Class)
	}
	close(runtime.startRelease)
	waitFor(t, "late retirement", func() bool {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		return runtime.stopCalls >= 2 && !runtime.running
	})
	waitFor(t, "a proven close", func() bool {
		again, err := manager.StopWithReport(context.Background(), "bus.primary")
		return err == nil && again.Class.Proven()
	})
	_ = manager.Shutdown(context.Background())
}

// A stop that supersedes a start in flight is settled once the late instance is
// retired, and the retirement's proof lets shutdown release the store lock.
func TestStopSupersedingAStartSettlesAndReleasesTheStore(t *testing.T) {
	entered, block := make(chan struct{}), make(chan struct{})
	r, store := fileRig(t, contract.RetryPolicy{}, &startScript{entered: entered, block: block}, "bus.one")
	r.build("inc1", false)
	if err := r.sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	r.decide("bus.one", MethodStop)
	close(block)
	r.waitState("bus.one", ObservedStopped)
	waitFor(t, "the late instance retired", func() bool {
		inst := r.instancesOf("bus.one")
		return len(inst) == 1 && inst[0].stopCount() >= 1
	})
	waitFor(t, "the stop settled", func() bool {
		rec, err := store.Driver(ref("bus.one").ID())
		return err == nil && rec.Current == nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = r.sup.Close(ctx)
	if store.Exclusive() {
		t.Fatal("a proven retirement left the store lock held")
	}
}
