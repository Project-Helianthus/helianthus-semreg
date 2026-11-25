package call

import (
	"context"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestM56BuiltinKernelComposes(t *testing.T) {
	if RetainedOperations != 8192 {
		t.Fatalf("retention %d", RetainedOperations)
	}
	kernel, err := NewBuiltinKernel()
	if err != nil {
		t.Fatal(err)
	}
	rig := newRig(t, thermalSpec())
	request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
	selection, refusal := selectCase(request)
	if refusal != nil {
		t.Fatal(refusal.reason)
	}
	intent, err := rig.owner.Complete(context.Background(), Draft{Request: request, Selection: selection, Resource: rig.plant.resource, Now: rig.now})
	if err != nil {
		t.Fatal(err)
	}
	current := semreg.EvaluationContext{EvaluatedAt: timePoint(rig.now), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:call", Nanoseconds: "1"}}
	if _, err := kernel.Admit(rig.plant.snapshot, current, intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil })); err != nil {
		t.Fatal(err)
	}
}

func TestM56RouterBoundsTheWait(t *testing.T) {
	rig := newRig(t, thermalSpec())
	deadline := timePoint(rig.now.Add(5 * time.Second))
	ctx := WithCaller(context.Background(), Caller{GraphQL: true, Deadline: &deadline})
	result := rig.invoke(ctx, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 30*time.Second))
	if result.Status != StatusProtocolAck {
		t.Fatal(result.Reason)
	}
	intent := rig.owner.intent()
	if intent.Deadline != deadline || intent.Causal.ExpiresAt != deadline {
		t.Fatalf("deadline %#v expires %#v", intent.Deadline, intent.Causal.ExpiresAt)
	}
}

func TestM56RouterReplayNeverRedispatches(t *testing.T) {
	t.Run("cached result", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
		first := rig.invoke(nil, request)
		replay := rig.invoke(WithDispatchMode(context.Background(), ModeReplay), request)
		calls, _ := rig.exec.counts()
		if calls != 1 || replay.Status != first.Status || replay.KernelOutcome != first.KernelOutcome {
			t.Fatalf("calls %d first %#v replay %#v", calls, first, replay)
		}
	})
	t.Run("in-flight admission is not dispatched twice", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		entered := make(chan struct{})
		release := make(chan struct{})
		rig.exec.entered = entered
		rig.exec.release = release
		request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
		firstDone := make(chan driver.OperationResult, 1)
		go func() { firstDone <- rig.invoke(nil, request) }()
		<-entered
		replayDone := make(chan driver.OperationResult, 1)
		go func() {
			replayDone <- rig.invoke(WithDispatchMode(context.Background(), ModeReplay), request)
		}()
		select {
		case result := <-replayDone:
			t.Fatalf("replay returned during the flight: %#v", result)
		case <-time.After(30 * time.Millisecond):
		}
		close(release)
		first := <-firstDone
		replay := <-replayDone
		calls, _ := rig.exec.counts()
		if calls != 1 || first.Status != StatusProtocolAck || replay.Status != first.Status {
			t.Fatalf("calls %d first %#v replay %#v", calls, first, replay)
		}
	})
}

func TestM56RouterValidatesRequestBounds(t *testing.T) {
	rig := newRig(t, thermalSpec())
	base := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
	occupied := qtyExp("29315", -2, "unit.kelvin")
	cases := []struct {
		name   string
		mutate func(*driver.OperationRequest)
		reason string
	}{
		{name: "revision form", mutate: func(r *driver.OperationRequest) { r.ExpectedSemanticRevision = "01" }, reason: ReasonInvalidArgument},
		{name: "missing arguments", mutate: func(r *driver.OperationRequest) { r.Arguments = nil }, reason: ReasonInvalidArgument},
		{name: "unordered arguments", mutate: func(r *driver.OperationRequest) {
			r.Arguments = []semreg.TypedField{{ID: "thermal.setpoint.temperature_setpoint", Value: occupied}, {ID: "thermal.setpoint.occupied_heating", Value: occupied}}
		}, reason: ReasonNoncanonicalArguments},
		{name: "duplicate arguments", mutate: func(r *driver.OperationRequest) {
			r.Arguments = []semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: occupied}, {ID: rig.plant.operation.Argument.ID, Value: occupied}}
		}, reason: ReasonDuplicateArgument},
		{name: "unordered preconditions", mutate: func(r *driver.OperationRequest) { r.Preconditions = reversedPreconditions(t) }, reason: ReasonNoncanonicalPre},
		{name: "duplicate preconditions", mutate: func(r *driver.OperationRequest) {
			one := reversedPreconditions(t)[:1]
			r.Preconditions = []operation.Precondition{one[0], one[0]}
		}, reason: ReasonDuplicatePrecondition},
		{name: "value arm", mutate: func(r *driver.OperationRequest) {
			flag := true
			value := occupied
			value.Boolean = &flag
			r.Arguments = []semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: value}}
		}, reason: ReasonInvalidArgument},
		{name: "malformed identity", mutate: func(r *driver.OperationRequest) { r.AssetID = "asset bad" }, reason: ReasonInvalidArgument},
		{name: "maximum unsigned revision", mutate: func(r *driver.OperationRequest) { r.ExpectedSemanticRevision = "18446744073709551615" }, reason: ""},
		{name: "revision above maximum", mutate: func(r *driver.OperationRequest) { r.ExpectedSemanticRevision = "18446744073709551616" }, reason: ReasonInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			request.Arguments = append([]semreg.TypedField{}, base.Arguments...)
			request.Preconditions = append([]operation.Precondition{}, base.Preconditions...)
			tc.mutate(&request)
			result := rig.invoke(nil, request)
			if tc.reason == "" {
				if result.Reason == ReasonInvalidArgument {
					t.Fatalf("maximum revision rejected: %#v", result)
				}
				return
			}
			calls, sends := rig.exec.counts()
			if calls != 0 || sends != 0 {
				t.Fatalf("calls %d sends %d", calls, sends)
			}
			assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, tc.reason)
		})
	}
}

func reversedPreconditions(t *testing.T) []operation.Precondition {
	t.Helper()
	text := "resource:zone:01"
	low := semreg.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.0.0", FactID: "thermal.setpoint.occupied_heating", Dimensions: []semreg.Dimension{{ID: "thermal.dimension.zone", Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}}}
	high := semreg.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.0.0", FactID: "thermal.setpoint.temperature_setpoint", Dimensions: []semreg.Dimension{{ID: "thermal.dimension.zone", Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}}}
	value := zoneTemperature()
	left := operation.Precondition{Fact: low, CandidateID: "candidate:low", CandidateRevision: "1", Operator: semreg.PredicateEqual, Expected: value}
	right := operation.Precondition{Fact: high, CandidateID: "candidate:high", CandidateRevision: "1", Operator: semreg.PredicateEqual, Expected: value}
	leftBytes, err := semreg.CanonicalJSON(left.Fact)
	if err != nil {
		t.Fatal(err)
	}
	rightBytes, err := semreg.CanonicalJSON(right.Fact)
	if err != nil {
		t.Fatal(err)
	}
	if string(leftBytes) > string(rightBytes) {
		return []operation.Precondition{left, right}
	}
	return []operation.Precondition{right, left}
}

func TestRetentionReplacementAtMostOnce(t *testing.T) {
	rig := newRig(t, thermalSpec())
	request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
	selection, refusal := selectCase(request)
	if refusal != nil {
		t.Fatal(refusal.reason)
	}
	intent, err := rig.owner.Complete(context.Background(), Draft{Request: request, Selection: selection, Resource: rig.plant.resource, Now: rig.now})
	if err != nil {
		t.Fatal(err)
	}
	current := semreg.EvaluationContext{EvaluatedAt: timePoint(rig.now), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:call", Nanoseconds: "1"}}
	auth := operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil })
	for i := 0; i < RetainedOperations; i++ {
		filled := intent
		filled.IntentID = semreg.IntentID("intent:fill:" + itoa(i+1))
		filled.IdempotencyKey = semreg.IdempotencyKey("idem:fill:" + itoa(i+1))
		if _, err := rig.router.kernel.Admit(rig.plant.snapshot, current, filled, auth); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	before := rig.router.kernel
	result := rig.invoke(nil, request)
	if result.Status != StatusProtocolAck {
		t.Fatalf("replacement invoke %#v", result)
	}
	replaced := rig.router.kernel
	if replaced == before {
		t.Fatal("full kernel was not replaced")
	}
	again := rig.invoke(nil, request)
	if again.Status != StatusProtocolAck || rig.router.kernel != replaced {
		t.Fatalf("second replacement kernel changed: %#v", again)
	}
}
