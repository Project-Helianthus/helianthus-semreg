package life

import (
	"context"
	"errors"
	"testing"
	"time"

	contract "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func single(t *testing.T, retry contract.RetryPolicy, script *startScript, key string) *rig {
	t.Helper()
	return newRig(t, NewMemoryStore(), func(f *testFactory) []InstanceConfig {
		f.retry = retry
		f.script[contract.InstanceKey(key)] = script
		return keysOf(f, true, key)
	})
}

// The retry policy of an instance plan is normalized exactly: a negative budget
// is zero, a delay of zero or less is one second, a maximum below the initial
// delay is the initial delay, a jitter of zero selects 20%, a negative one none
// and one above half is half; a persistent phase follows the same delay rules
// and is left as given while disabled. No driver name selects a policy.
func TestRetryPolicyNormalization(t *testing.T) {
	s := time.Second
	cases := []struct {
		name string
		in   contract.RetryPolicy
		want contract.RetryPolicy
	}{
		{"zero value", contract.RetryPolicy{}, contract.RetryPolicy{InitialDelay: s, MaxDelay: s, JitterRatio: 0.2}},
		{"negative budget", contract.RetryPolicy{Budget: -3, InitialDelay: s, MaxDelay: 2 * s, JitterRatio: 0.1}, contract.RetryPolicy{InitialDelay: s, MaxDelay: 2 * s, JitterRatio: 0.1}},
		{"negative initial delay", contract.RetryPolicy{Budget: 2, InitialDelay: -s, MaxDelay: 5 * s, JitterRatio: 0.3}, contract.RetryPolicy{Budget: 2, InitialDelay: s, MaxDelay: 5 * s, JitterRatio: 0.3}},
		{"max below initial", contract.RetryPolicy{Budget: 1, InitialDelay: 4 * s, MaxDelay: s, JitterRatio: 0.3}, contract.RetryPolicy{Budget: 1, InitialDelay: 4 * s, MaxDelay: 4 * s, JitterRatio: 0.3}},
		{"zero jitter selects 20%", contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s}, contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: 0.2}},
		{"negative jitter disables", contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: -1}, contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s}},
		{"jitter above half", contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: 0.9}, contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: 0.5}},
		{"persistent enabled", contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: 0.1, Persistent: contract.PersistentRetry{Enabled: true, InitialDelay: 0, MaxDelay: 0}},
			contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: 0.1, Persistent: contract.PersistentRetry{Enabled: true, InitialDelay: s, MaxDelay: s}}},
		{"persistent max below initial", contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: 0.1, Persistent: contract.PersistentRetry{Enabled: true, InitialDelay: 3 * s, MaxDelay: s}},
			contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: 0.1, Persistent: contract.PersistentRetry{Enabled: true, InitialDelay: 3 * s, MaxDelay: 3 * s}}},
		{"persistent disabled is left as given", contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: 0.1, Persistent: contract.PersistentRetry{InitialDelay: -s, MaxDelay: -2 * s}},
			contract.RetryPolicy{Budget: 1, InitialDelay: s, MaxDelay: s, JitterRatio: 0.1, Persistent: contract.PersistentRetry{InitialDelay: -s, MaxDelay: -2 * s}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeRetryPolicy(tc.in); got != tc.want {
				t.Fatalf("normalize(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// Epochs name their incarnation, so a correlation of an earlier process is told
// from this process's.
func TestEpochNamesItsIncarnation(t *testing.T) {
	epoch := makeEpoch("inc.7", ref("bus.one").ID(), 3)
	if got, ok := epochIncarnation(epoch); !ok || got != "inc.7" {
		t.Fatalf("incarnation = %q %v", got, ok)
	}
	if makeEpoch("inc.7", ref("bus.one").ID(), 3) != epoch || makeEpoch("inc.8", ref("bus.one").ID(), 3) == epoch ||
		makeEpoch("inc.7", ref("bus.two").ID(), 3) == epoch || makeEpoch("inc.7", ref("bus.one").ID(), 4) == epoch {
		t.Fatal("an epoch does not vary with each of its members")
	}
	if err := validateTransition(&Transition{Kind: KindStart, Phase: PhaseActive, DesiredRevision: 1, Sequence: 1, Epoch: epoch}); err != nil {
		t.Fatalf("a generated epoch is not storable: %v", err)
	}
	if _, ok := epochIncarnation("nodots"); ok {
		t.Fatal("a malformed epoch named an incarnation")
	}
}

// A start that fails after the instance opened something is a failed partial
// start: the instance is closed, nothing stays open and the instance reports
// failure.
func TestFailedPartialStartLeavesNothingOpen(t *testing.T) {
	r := single(t, contract.RetryPolicy{}, &startScript{startErrs: []error{errors.New("terminal after opening")}}, "bus.one")
	defer r.close()
	r.build("inc1", false)
	r.start()
	st := r.waitState("bus.one", ObservedFailed)
	if st.SafetyQuarantined {
		t.Fatalf("a confirmed close quarantined the instance: %+v", st)
	}
	inst := r.instancesOf("bus.one")
	if len(inst) != 1 || inst[0].stopCount() != 1 {
		t.Fatalf("the partially started instance was not closed: %d instances", len(inst))
	}
	if r.sup.byID[ref("bus.one").ID()].runtime.SafetyQuarantined() {
		t.Fatal("the runtime is quarantined")
	}
}

// A failed start whose close cannot be proven quarantines the instance: it is
// reported, and no replacement is constructed.
func TestFailedStartWithUnprovenCloseQuarantines(t *testing.T) {
	r := single(t, contract.RetryPolicy{Budget: 3}, &startScript{
		startErrs:  []error{&retryableError{"retryable after opening"}},
		stopReport: &contract.CloseReport{Confirmed: false, Err: errors.New("not proven")},
	}, "bus.one")
	defer r.close()
	r.build("inc1", false)
	r.start()
	waitFor(t, "quarantine", func() bool { return r.status("bus.one").SafetyQuarantined })
	time.Sleep(60 * time.Millisecond)
	if r.factory.constructed() != 1 {
		t.Fatalf("a quarantined instance was reconstructed: %d", r.factory.constructed())
	}
	_, err := r.sup.SetDesired(context.Background(), ref("bus.one"), MethodRestart, r.status("bus.one").DesiredRevision)
	if err == nil {
		t.Fatal("a restart was admitted over an unproven close")
	}
}

// A start whose first attempt fails retryably under a persistent policy is
// admitted into the native owner's recovery: the service shows the BACKOFF with
// its schedule and then follows the owner's later transitions to RUNNING; the
// manager is the one retry owner.
func TestRecoveringStartIsAdmittedIntoBackoffAndFollowed(t *testing.T) {
	retry := contract.RetryPolicy{JitterRatio: -1, Persistent: contract.PersistentRetry{Enabled: true, InitialDelay: 300 * time.Millisecond, MaxDelay: 300 * time.Millisecond}}
	r := single(t, retry, &startScript{startErrs: []error{&retryableError{"first attempt"}}}, "bus.one")
	defer r.close()
	r.build("inc1", false)
	r.start()
	waitFor(t, "backoff", func() bool { return r.status("bus.one").ObservedState == ObservedBackoff })
	st := r.status("bus.one")
	if st.NextAttempt != 1 || st.NextAttemptUTC.IsZero() || st.Generation != 1 {
		t.Fatalf("admitted backoff = %+v", st)
	}
	waitFor(t, "the owner's retry", func() bool { return r.status("bus.one").ObservedState == ObservedRunning })
	if r.factory.constructed() != 2 {
		t.Fatalf("constructed %d instances, want the failed attempt and its retry", r.factory.constructed())
	}
	// One owner retried: the service charged exactly one attempt.
	if got := r.status("bus.one").Generation; got != 1 {
		t.Fatalf("the service retried as well: generation %d", got)
	}
}

// A failure after start withdraws the generation and the native owner replaces
// it: the old instance is closed and withdrawn, a new one starts, and the
// service follows the owner back to RUNNING.
func TestFailureAfterStartIsReplacedByTheNativeOwner(t *testing.T) {
	retry := contract.RetryPolicy{Budget: 2, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, JitterRatio: -1}
	r := single(t, retry, &startScript{}, "bus.one")
	defer r.close()
	r.build("inc1", false)
	r.start()
	sv := r.sup.byID[ref("bus.one").ID()]
	first := r.instancesOf("bus.one")[0]
	ok := r.sup.manager.ReportFailure(string(ref("bus.one").ID()), Correlation{Generation: sv.runtime.Generation()},
		Failure{Reason: Reason{Code: ReasonDependencyUnavailable, Retryable: true}})
	if !ok {
		t.Fatal("the failure was not accepted")
	}
	waitFor(t, "replacement", func() bool {
		return r.factory.constructed() == 2 && r.status("bus.one").ObservedState == ObservedRunning
	})
	if first.stopCount() != 1 || !first.fenced {
		t.Fatalf("the failed generation was not fenced and closed: stops %d fenced %v", first.stopCount(), first.fenced)
	}
	if w := r.pub.withdrawals(); len(w) != 1 {
		t.Fatalf("withdrawals = %+v", w)
	}
}

// A stop that arrives while a start is still in flight supersedes it: the late
// admission is retired, never published, and the instance ends stopped with its
// instance closed.
func TestStopSupersedesAStartInFlight(t *testing.T) {
	entered, block := make(chan struct{}), make(chan struct{})
	r := single(t, contract.RetryPolicy{}, &startScript{entered: entered, block: block}, "bus.one")
	defer r.close()
	r.build("inc1", false)
	if err := r.sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	r.decide("bus.one", MethodStop)
	waitFor(t, "stopping", func() bool { return r.status("bus.one").DesiredState == DesiredStopped })
	close(block)
	r.waitState("bus.one", ObservedStopped)
	waitFor(t, "the late instance retired", func() bool {
		inst := r.instancesOf("bus.one")
		return len(inst) == 1 && inst[0].stopCount() >= 1
	})
	if got := r.status("bus.one").ObservedState; got == ObservedRunning {
		t.Fatalf("the superseded admission was published: %s", got)
	}
}
