package life

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
)

type stopScript struct {
	outs []StopOutcome
}

// next returns the scripted outcome of the nth Stop call; the last repeats.
func (s stopScript) next(n int) StopOutcome {
	if n >= len(s.outs) {
		n = len(s.outs) - 1
	}
	return s.outs[n]
}

// A terminal start failure of the initial attempt is retired through the
// executor under the attempt's own correlation, so the instance can be
// restarted afterwards.
func TestUntypedTerminalStartFailureRetiresEpoch(t *testing.T) {
	f := newFixture(t, svcOpts{drivers: enabled(string(nativeDriver)),
		setup: func(f *svcFixture) {
			f.execs[nativeDriver].setStart(func(context.Context, LifecycleCorrelation) error {
				return errors.New("start refused")
			})
		}})
	f.waitObserved(nativeDriver, ObservedFailed)
	calls := f.execs[nativeDriver].snapshot()
	var start, stop *LifecycleCorrelation
	for i := range calls {
		switch calls[i].Op {
		case "start":
			start = &calls[i].Corr
		case "stop":
			stop = &calls[i].Corr
		}
	}
	if start == nil || stop == nil || start.Epoch != stop.Epoch || start.Generation != stop.Generation {
		t.Fatalf("calls = %+v; want one Stop of the failed attempt's correlation", calls)
	}
	if status := f.status(nativeDriver); status.SafetyQuarantined {
		t.Fatalf("status = %+v; a proven retirement must not quarantine", status)
	}
	f.execs[nativeDriver].setStart(nil)
	waitFor(t, "RESTART after retired failure", func() bool {
		_, err := f.decide(MethodRestart, nativeDriver)
		return err == nil
	})
	f.waitSettled(nativeDriver, ObservedRunning)
}

// The same failure with the real executor: the failed epoch is fenced and
// closed, so the next epoch binds instead of being refused for its
// predecessor.
func TestUntypedTerminalStartFailureAllowsRestartWithExecutor(t *testing.T) {
	r := newRig(t, NewMemoryStore(), func(f *testFactory) []InstanceConfig {
		f.script["bus.one"] = &startScript{startErrs: []error{errors.New("start refused")}}
		return instanceConfigs(f, true, nil, "bus.one")
	})
	defer r.close()
	r.build("inc1", false)
	r.start()
	failed := r.waitState("bus.one", ObservedFailed)
	waitFor(t, "RESTART after the failed initial start", func() bool {
		_, err := r.sup.SetDesired(context.Background(), ref("bus.one"), MethodRestart, r.status("bus.one").DesiredRevision)
		return err == nil
	})
	running := r.waitState("bus.one", ObservedRunning)
	if running.Epoch == failed.Epoch {
		t.Fatalf("epoch %s reused after the failure", running.Epoch)
	}
}

// An initial start whose native close is not proven stays the instance's
// current work: STOP settles the same correlation and reports STOPPED only
// after the close and the withdrawal are proven, and a replacement is refused
// while the close is unproven.
func TestUntypedUnprovenCloseIsSettledByStop(t *testing.T) {
	unproven := StopOutcome{NativeClosed: false, SemanticCleaned: true}
	proven := StopOutcome{NativeClosed: true, SemanticCleaned: true}
	for _, path := range []string{"withdrawn-late", "close-unconfirmed"} {
		for _, stopProves := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/stop-proves=%t", path, stopProves), func(t *testing.T) {
				// The late withdrawal runs its own Stop first (unproven); the
				// unconfirmed-close path has no Stop before the operator's.
				script := stopScript{outs: []StopOutcome{unproven, proven}}
				switch {
				case path == "close-unconfirmed" && stopProves:
					script = stopScript{outs: []StopOutcome{proven}}
				case !stopProves:
					script = stopScript{outs: []StopOutcome{unproven}}
				}
				stops := 0
				f := newFixture(t, svcOpts{drivers: enabled(string(nativeDriver)),
					setup: func(f *svcFixture) {
						e := f.execs[nativeDriver]
						e.setStart(func(context.Context, LifecycleCorrelation) error {
							if path == "close-unconfirmed" {
								return fmt.Errorf("start: %w", issueCloseError{})
							}
							return errors.New("start refused")
						})
						e.setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
							stops++
							return script.next(stops - 1)
						})
					}})
				f.waitObserved(nativeDriver, ObservedFailed)
				waitFor(t, "quarantine", func() bool { return f.status(nativeDriver).SafetyQuarantined })
				issueRequireQuarantinedRestart(t, f)
				before := f.execs[nativeDriver].snapshot()

				f.mustDecide(MethodStop, nativeDriver)
				waitFor(t, "STOP settlement Stop", func() bool {
					return f.execs[nativeDriver].count("stop") > countOp(before, "stop")
				})
				after := f.execs[nativeDriver].snapshot()
				var origin LifecycleCorrelation
				for _, c := range before {
					if c.Op == "start" {
						origin = c.Corr
					}
				}
				last := after[len(after)-1]
				if last.Op != "stop" || last.Corr.Epoch != origin.Epoch || last.Corr.Generation != origin.Generation {
					t.Fatalf("settlement call = %+v; want a Stop of the failed start's correlation %+v", last, origin)
				}
				if stopProves {
					f.waitObserved(nativeDriver, ObservedStopped)
					return
				}
				if st := f.status(nativeDriver); st.ObservedState == ObservedStopped || !st.SafetyQuarantined {
					t.Fatalf("status = %+v; STOPPED must wait for a proven close", st)
				}
			})
		}
	}
}

func countOp(calls []execCall, op string) int {
	n := 0
	for _, c := range calls {
		if c.Op == op {
			n++
		}
	}
	return n
}

// An initial start that failed terminally and whose semantic withdrawal never
// succeeds keeps its correlation: a STOP accepted meanwhile persists the
// failed start's epoch, generation and pending withdrawal, so a reopened
// store settles it under that correlation and reports STOPPED only after the
// withdrawal is proven.
func TestUntypedUnwithdrawnStartKeepsCorrelationForStop(t *testing.T) {
	var origin LifecycleCorrelation
	f := newFixture(t, svcOpts{drivers: enabled(string(nativeDriver)),
		setup: func(f *svcFixture) {
			e := f.execs[nativeDriver]
			e.setStart(func(_ context.Context, c LifecycleCorrelation) error {
				origin = c
				return errors.New("start refused")
			})
			e.setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
				return StopOutcome{NativeClosed: true, SemanticCleaned: false}
			})
		}})
	waitFor(t, "cleanup stalled", func() bool {
		return f.status(nativeDriver).ObservedState == ObservedFailed &&
			hasDiagnostic(f.svc.Diagnostics(nativeDriver), "CLEANUP_STALLED")
	})
	f.mustDecide(MethodStop, nativeDriver)
	cur := f.record(nativeDriver).Current
	if cur == nil || cur.Epoch != origin.Epoch || cur.Generation != origin.Generation || !cur.SemanticCleanupPending {
		t.Fatalf("slot after STOP = %+v; want the failed start's correlation %+v with the withdrawal pending", cur, origin)
	}
	if st := f.status(nativeDriver); st.ObservedState == ObservedStopped {
		t.Fatalf("status = %+v; STOPPED before the withdrawal was proven", st)
	}

	data := f.fileBytes()
	g := newFixture(t, svcOpts{drivers: enabled(string(nativeDriver)), incarnation: "inc2",
		state: func(t *testing.T, path string) {
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}})
	g.waitObserved(nativeDriver, ObservedStopped)
	var settled bool
	for _, c := range g.execs[nativeDriver].snapshot() {
		if c.Op == "stop" && c.Corr.Epoch == origin.Epoch && c.Corr.Generation == origin.Generation {
			settled = true
		}
		if c.Op == "start" {
			t.Fatalf("calls = %+v; a stopped instance must not start", g.execs[nativeDriver].snapshot())
		}
	}
	if !settled {
		t.Fatalf("calls = %+v; want a Stop of the failed start's correlation", g.execs[nativeDriver].snapshot())
	}
}
