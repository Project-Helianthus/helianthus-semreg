package call

import (
	"context"
	"strings"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

func zoneTemperature() semreg.Value { return qtyExp("29315", -2, "unit.kelvin") }

func thermalSpec() publishSpec {
	return publishSpec{
		name: "thermal", validator: thermal.New(), operation: "thermal.operation.set_temperature",
		resource: "resource:zone:01", asset: "asset:thermal", value: zoneTemperature(),
	}
}

func ampere(coefficient string) semreg.Value { return qty(coefficient, "unit.ampere") }

func evseSpec(value semreg.Value) publishSpec {
	return publishSpec{
		name: "evse", validator: evse.New(), operation: "evse.operation.set_allocated_current",
		resource: "resource:connector:01", asset: "asset:evse", value: value,
	}
}

func assertIdle(t *testing.T, exec *scriptExec) {
	t.Helper()
	calls, sends := exec.counts()
	if calls != 0 || sends != 0 {
		t.Fatalf("executor calls %d sends %d", calls, sends)
	}
}

func assertRefusal(t *testing.T, result driver.OperationResult, status driver.OperationStatus, outcome operation.Outcome, delivery operation.DeliveryState, reason string) {
	t.Helper()
	if result.Status != status || result.KernelOutcome != outcome || result.Delivery != delivery || !strings.Contains(result.Reason, reason) {
		t.Fatalf("result %#v, want status %s outcome %s delivery %s reason %s", result, status, outcome, delivery, reason)
	}
}

func TestEvseAllocatedCurrentAndThermalSetTemperatureEffects(t *testing.T) {
	t.Run("thermal zone", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
		calls, sends := rig.exec.counts()
		if calls != 1 || sends != 1 {
			t.Fatalf("calls %d sends %d", calls, sends)
		}
		if result.Status != StatusProtocolAck || result.KernelOutcome != operation.OutcomeAcknowledgedUnverified || result.Delivery != operation.DeliverySent {
			t.Fatalf("result %#v", result)
		}
		effect := rig.exec.last.ExpectedEffect
		if effect.Rule.ID != "thermal.effect.set_temperature" || effect.Operator != semreg.PredicateEqual || effect.Fact.Dimensions[0].ID != "thermal.dimension.zone" || effect.Fact.Dimensions[0].Value.Text == nil || *effect.Fact.Dimensions[0].Value.Text != "resource:zone:01" {
			t.Fatalf("effect %#v", effect)
		}
		if effect.Fact.PackID != rig.plant.operation.Ref.Pack.ID || effect.Fact.PackVersion != rig.plant.operation.Ref.Pack.Version || effect.Fact.FactID != "thermal.setpoint.temperature_setpoint" {
			t.Fatalf("effect fact %#v", effect.Fact)
		}
	})
	t.Run("evse connector", func(t *testing.T) {
		value := ampere("16")
		rig := newRig(t, evseSpec(value))
		result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: "evse.limit.allocated_current", Value: value}}, 20*time.Second))
		calls, sends := rig.exec.counts()
		if calls != 1 || sends != 1 || result.Status != StatusProtocolAck {
			t.Fatalf("calls %d sends %d result %#v", calls, sends, result)
		}
		effect := rig.exec.last.ExpectedEffect
		if effect.Rule.ID != "evse.effect.set_allocated_current" || effect.Operator != semreg.PredicateEqual || effect.Fact.Dimensions[0].ID != "evse.dimension.connector" || effect.Fact.FactID != "evse.limit.allocated_current" {
			t.Fatalf("effect %#v", effect)
		}
	})
}

func TestServiceWithoutResourceSendsNothing(t *testing.T) {
	spec := thermalSpec()
	spec.omitResource = true
	rig := newRig(t, spec)
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	assertIdle(t, rig.exec)
	assertRefusal(t, result, StatusUnavailable, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonResourceMissing)
}

func TestThreeSourcesReachOnlyTheConfigurationOwner(t *testing.T) {
	rig := newRig(t, thermalSpec())
	ownerRoute := rig.plant.settingRoute(nil)
	producer := &scriptExec{}
	mirror := &scriptExec{}
	producerRoute := ownerRoute
	producerRoute.Route.BindingID = "binding:producer"
	producerRoute.Owner.PathKey = "producer"
	mirrorRoute := ownerRoute
	mirrorRoute.Route.BindingID = "binding:mirror"
	mirrorRoute.Owner.PathKey = "mirror"
	if _, err := rig.router.ScopeOf(rig.scope).RegisterExecutor(producerRoute, producer); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.router.ScopeOf(rig.scope).RegisterExecutor(mirrorRoute, mirror); err != nil {
		t.Fatal(err)
	}
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	calls, sends := rig.exec.counts()
	if calls != 1 || sends != 1 || result.Status != StatusProtocolAck {
		t.Fatalf("owner calls %d sends %d result %#v", calls, sends, result)
	}
	assertIdle(t, producer)
	assertIdle(t, mirror)
	if rig.exec.last.Route.BindingID != ownerRoute.Route.BindingID {
		t.Fatalf("dispatch binding %s", rig.exec.last.Route.BindingID)
	}
}

func TestRouteRefusalsSendNothing(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		mutate func(rig *rig)
	}{
		{name: "zero routes", reason: ReasonNoRoute, mutate: func(rig *rig) { rig.use(nil) }},
		{name: "two routes", reason: ReasonAmbiguousRoute, mutate: func(rig *rig) {
			route := rig.plant.settingRoute(nil)
			other := route
			other.Owner.PathKey = "mirror"
			rig.use([]driver.SettingRoute{route, other})
		}},
		{name: "withdrawn route", reason: ReasonWithdrawnRoute, mutate: func(rig *rig) {
			snap := rig.plant.snapshot
			sources := append([]semreg.SourceDescriptor(nil), snap.Sources...)
			sources[0].State = semreg.SourceRetired
			snap.Sources = sources
			rig.use([]driver.SettingRoute{rig.plant.settingRoute(nil)}, snap)
		}},
		{name: "stale owner", reason: ReasonStaleOwner, mutate: func(rig *rig) {
			snap := rig.plant.snapshot
			sources := append([]semreg.SourceDescriptor(nil), snap.Sources...)
			sources[0].SourceEpochID = "epoch:other"
			snap.Sources = sources
			rig.use([]driver.SettingRoute{rig.plant.settingRoute(nil)}, snap)
		}},
		{name: "unknown owner", reason: ReasonUnknownOwner, mutate: func(rig *rig) {
			route := rig.plant.settingRoute(nil)
			route.Route.SourceID = "source:missing"
			rig.use([]driver.SettingRoute{route})
		}},
		{name: "no active binding", reason: ReasonNoContact, mutate: func(rig *rig) {
			snap := rig.plant.snapshot
			bindings := append([]semreg.NativeBinding(nil), snap.Bindings...)
			bindings[0].State = semreg.BindingFenced
			snap.Bindings = bindings
			rig.use([]driver.SettingRoute{rig.plant.settingRoute(nil)}, snap)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newRig(t, thermalSpec())
			tc.mutate(rig)
			result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
			assertIdle(t, rig.exec)
			outcome := operation.OutcomeRejected
			if tc.reason == ReasonNoContact {
				outcome = operation.OutcomeFailedNoContact
			}
			assertRefusal(t, result, StatusFailed, outcome, operation.DeliveryNotSent, tc.reason)
		})
	}
}

func TestNoRegisteredExecutorAfterCapture(t *testing.T) {
	spec := thermalSpec()
	spec.skipRegister = true
	rig := newRig(t, spec)
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	if rig.capture.calls() < 1 {
		t.Fatal("executor miss did not capture")
	}
	assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonExecutorMissing)
}

func TestGenerationChangeBetweenAdmissionAndDispatchSendsNothing(t *testing.T) {
	rig := newRig(t, thermalSpec())
	second := rig.plant.snapshot
	capabilities := append([]semreg.CapabilityInstance(nil), second.Capabilities...)
	capabilities[0].DriverGeneration = "2"
	second.Capabilities = capabilities
	rig.capture.reset([]driver.SemanticCut{
		rig.plant.cut([]driver.SettingRoute{rig.plant.settingRoute(nil)}, rig.plant.snapshot),
		rig.plant.cut([]driver.SettingRoute{rig.plant.settingRoute(nil)}, second),
	})
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	assertIdle(t, rig.exec)
	assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonGuardChanged)
}

func TestExecutorReceivesGuardAndRetirementBeforeSend(t *testing.T) {
	rig := newRig(t, thermalSpec())
	entered := make(chan struct{})
	release := make(chan struct{})
	rig.exec.entered = entered
	rig.exec.release = release
	rig.exec.retire = func() bool { return true }
	done := make(chan driver.OperationResult, 1)
	go func() {
		done <- rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	}()
	<-entered
	if rig.exec.last.Guard.DriverGeneration != rig.plant.generation || rig.exec.last.Guard.BindingID != rig.plant.binding {
		t.Fatalf("guard %#v", rig.exec.last.Guard)
	}
	close(release)
	result := <-done
	calls, sends := rig.exec.counts()
	if calls != 1 || sends != 0 {
		t.Fatalf("calls %d sends %d", calls, sends)
	}
	assertRefusal(t, result, StatusFailed, operation.OutcomeFailedNoContact, operation.DeliveryNotSent, ReasonNoContact)
}

func TestScopedOperationsReachTheSameOwner(t *testing.T) {
	rig := newRig(t, thermalSpec())
	var ops driver.ScopedOperations = rig.router.ScopeOf(rig.scope)
	request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
	direct := rig.invoke(nil, request)
	scoped, err := ops.Invoke(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if direct.Status != StatusProtocolAck || scoped.Status != direct.Status || scoped.KernelOutcome != direct.KernelOutcome || scoped.Delivery != direct.Delivery {
		t.Fatalf("direct %#v scoped %#v", direct, scoped)
	}
	calls, _ := rig.exec.counts()
	if calls != 2 {
		t.Fatalf("calls %d", calls)
	}
}

func TestRegistrationFence(t *testing.T) {
	rig := newRig(t, thermalSpec())
	route := rig.plant.settingRoute(nil)
	request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
	rig.use(nil)
	before := rig.invoke(nil, request)
	assertIdle(t, rig.exec)
	assertRefusal(t, before, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonNoRoute)
	rig.use([]driver.SettingRoute{route})
	after := rig.invoke(nil, request)
	calls, _ := rig.exec.counts()
	if calls != 1 || after.Status != StatusProtocolAck || rig.exec.last.Route.CapabilityInstance != route.Route.CapabilityInstance {
		t.Fatalf("calls %d result %#v capability %s", calls, after, rig.exec.last.Route.CapabilityInstance)
	}
	foreign := rig.scope
	foreign.Producer.ID = "resource:other"
	if _, err := rig.router.ScopeOf(foreign).RegisterExecutor(route, &scriptExec{}); err == nil {
		t.Fatal("foreign binding registered")
	}
	if _, err := rig.router.ScopeOf(rig.scope).RegisterExecutor(route, &scriptExec{}); err == nil {
		t.Fatal("duplicate owner registered")
	}
	rig.router.ScopeOf(rig.scope).Retire()
	if _, err := rig.router.ScopeOf(rig.scope).RegisterExecutor(route, &scriptExec{}); err == nil {
		t.Fatal("retired generation registered")
	}
	replacement := &scriptExec{}
	next := Scope{Instance: "session:2", Generation: 2, Producer: rig.plant.node()}
	if _, err := rig.router.ScopeOf(next).RegisterExecutor(route, replacement); err != nil {
		t.Fatal(err)
	}
	rig.unregister()
	rig.use([]driver.SettingRoute{route})
	kept := rig.invoke(nil, request)
	if kept.Status != StatusProtocolAck {
		t.Fatalf("replacement %#v", kept)
	}
	replacedCalls, _ := replacement.counts()
	if replacedCalls != 1 {
		t.Fatalf("replacement calls %d", replacedCalls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bound := request
	bound.Timeout = 0
	boundResult, err := rig.router.ScopeOf(next).InvokeCallBound(ctx, bound)
	if err != nil || boundResult.Status != StatusProtocolAck {
		t.Fatalf("call-bound %#v %v", boundResult, err)
	}
	replacedCalls, _ = replacement.counts()
	if replacedCalls != 2 {
		t.Fatalf("finite and call-bound calls %d", replacedCalls)
	}
}

func TestUnspecifiedCallerAndQualification(t *testing.T) {
	rig := newRig(t, thermalSpec())
	request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
	result := rig.invoke(nil, request)
	if result.Status != StatusProtocolAck {
		t.Fatal(result.Reason)
	}
	intent := rig.owner.intent()
	if intent.Authority.Kind != "semantic.caller.unspecified" {
		t.Fatalf("authority %#v", intent.Authority)
	}
	if intent.Causal.Path == nil || len(intent.Causal.Path) != 1 || intent.Causal.Path[0] != "runtime:call" || intent.Causal.HopCount != 1 || intent.Causal.MaxHops != 8 {
		t.Fatalf("causal %#v", intent.Causal)
	}
	if intent.Deadline == (semreg.TimePoint{}) || intent.Causal.ExpiresAt == (semreg.TimePoint{}) {
		t.Fatal("finite request lost its deadline")
	}
	noted, ok := rig.router.RecordedQualification(intent.IntentID)
	if !ok || noted != "" {
		t.Fatalf("qualification %q %v", noted, ok)
	}
	ctx := WithCaller(context.Background(), Caller{GraphQL: true, Qualification: "observed"})
	rig.capture.reset([]driver.SemanticCut{rig.plant.cut([]driver.SettingRoute{rig.plant.settingRoute(nil)}, rig.plant.snapshot)})
	qualified := rig.invoke(ctx, request)
	if qualified.Status != StatusProtocolAck {
		t.Fatal(qualified.Reason)
	}
	again := rig.owner.intent()
	got, ok := rig.router.RecordedQualification(again.IntentID)
	if !ok || got != "observed" || again.Authority.Kind != "semantic.caller.graphql" {
		t.Fatalf("qualification %q authority %s", got, again.Authority.Kind)
	}
}

func TestCallerDeadlineIsTheEarlierBound(t *testing.T) {
	rig := newRig(t, thermalSpec())
	deadline := timePoint(rig.now.Add(5 * time.Second))
	ctx := WithCaller(context.Background(), Caller{GraphQL: true, Deadline: &deadline})
	result := rig.invoke(ctx, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	if result.Status != StatusProtocolAck {
		t.Fatal(result.Reason)
	}
	intent := rig.owner.intent()
	if intent.Deadline != deadline || intent.Causal.ExpiresAt != deadline {
		t.Fatalf("deadline %#v expires %#v", intent.Deadline, intent.Causal.ExpiresAt)
	}
}

func TestTwoRuntimesKeepTheirComposedTargets(t *testing.T) {
	for _, target := range []semreg.TargetID{"runtime:one", "runtime:two"} {
		owner := NewIntentOwner(target)
		rig := newRig(t, thermalSpec())
		request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
		selection, refusal := selectCase(request)
		if refusal != nil {
			t.Fatal(refusal.reason)
		}
		intent, err := owner.Complete(context.Background(), Draft{Request: request, Selection: selection, Resource: rig.plant.resource, Now: rig.now})
		if err != nil {
			t.Fatal(err)
		}
		if len(intent.Causal.Path) != 1 || intent.Causal.Path[0] != target || intent.Causal.HopCount != 1 || intent.Causal.MaxHops != 8 {
			t.Fatalf("%s causal %#v", target, intent.Causal)
		}
	}
}

func TestInboundEchoAndHopBudgetSendNothing(t *testing.T) {
	base := func(t *testing.T) (*rig, driver.OperationRequest, semreg.CausalContext) {
		t.Helper()
		rig := newRig(t, thermalSpec())
		request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
		causal := semreg.CausalContext{
			Origin:        semreg.OriginRef{OriginID: "origin:graphql", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{pubEvidence("inbound")}},
			CorrelationID: "correlation:inbound", HopCount: 1, MaxHops: 8, FirstSeenAt: timePoint(rig.now), ExpiresAt: timePoint(rig.now.Add(20 * time.Second)),
			Path: []semreg.TargetID{"runtime:upstream"},
		}
		return rig, request, causal
	}
	t.Run("echo", func(t *testing.T) {
		rig, request, causal := base(t)
		causal.Path = []semreg.TargetID{"runtime:call"}
		ctx := WithCaller(context.Background(), Caller{GraphQL: true, Inbound: &causal})
		result := rig.invoke(ctx, request)
		assertIdle(t, rig.exec)
		assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, "echo_suppressed")
	})
	t.Run("hop budget", func(t *testing.T) {
		rig, request, causal := base(t)
		causal.HopCount = 8
		causal.MaxHops = 8
		causal.Path = []semreg.TargetID{"runtime:a", "runtime:b", "runtime:c", "runtime:d", "runtime:e", "runtime:f", "runtime:g", "runtime:h"}
		ctx := WithCaller(context.Background(), Caller{GraphQL: true, Inbound: &causal})
		result := rig.invoke(ctx, request)
		assertIdle(t, rig.exec)
		assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, "causal_budget_exceeded")
	})
}

func TestOutputUsesItsKeyAndOrigin(t *testing.T) {
	rig := newRig(t, thermalSpec())
	ctx := WithCaller(context.Background(), Caller{Output: &OutputIdentity{DriverID: "driver:output", Key: "key:one"}})
	result := rig.invoke(ctx, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	if result.Status != StatusProtocolAck {
		t.Fatal(result.Reason)
	}
	intent := rig.owner.intent()
	if intent.IdempotencyKey != "driver:output:key:one" || intent.Causal.Origin.Kind != semreg.OriginAutomation || intent.Causal.Origin.OriginID != "driver:output" || intent.Authority.Kind != "semantic.caller.output" {
		t.Fatalf("intent origin %#v key %s authority %s", intent.Causal.Origin, intent.IdempotencyKey, intent.Authority.Kind)
	}
}

func TestNilOwnerSendsNothing(t *testing.T) {
	capture := &captureScript{}
	router, err := NewRouter(Config{Capture: capture, Now: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }})
	if err != nil {
		t.Fatal(err)
	}
	result, err := router.Invoke(context.Background(), driver.OperationRequest{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if capture.calls() != 0 {
		t.Fatal("nil owner captured")
	}
	assertRefusal(t, result, StatusUnavailable, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonUnavailable)
}

func TestOwnerAlterationSendsNothing(t *testing.T) {
	rig := newRig(t, thermalSpec())
	rig.router.owner = alteringOwner{inner: rig.owner}
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	assertIdle(t, rig.exec)
	assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonOwnerAltered)
}

type alteringOwner struct{ inner IntentOwner }

func (o alteringOwner) Complete(ctx context.Context, draft Draft) (operation.Intent, error) {
	intent, err := o.inner.Complete(ctx, draft)
	intent.AssetID = "asset:other"
	return intent, err
}

func TestStaleElementStillReachesTheOwner(t *testing.T) {
	spec := thermalSpec()
	spec.freshFor = "1"
	rig := newRig(t, spec)
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	calls, sends := rig.exec.counts()
	if calls != 1 || sends != 1 || result.Status != StatusProtocolAck {
		t.Fatalf("calls %d sends %d result %#v", calls, sends, result)
	}
}

func TestValueOutsidePublishedLimitsNamesTheLimit(t *testing.T) {
	value := qtyExp("2", 1, "unit.ampere")
	rig := newRig(t, evseSpec(value))
	max := semreg.Quantity{Number: semreg.Decimal{Coefficient: "1", Exponent10: 1}, Unit: "unit.ampere"}
	limits := &driver.ValueLimits{Max: &max, Origin: driver.LimitDeviceQuery}
	rig.use([]driver.SettingRoute{rig.plant.settingRoute(limits)})
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: "evse.limit.allocated_current", Value: value}}, 20*time.Second))
	assertIdle(t, rig.exec)
	assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, "max=1e1 unit.ampere origin=device_query")
}

func TestDistinctResultsFromExecutorEvidence(t *testing.T) {
	requestOf := func(rig *rig) driver.OperationRequest {
		return rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
	}
	t.Run("accepted", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		rig.exec.complete = func(context.Context, driver.Dispatch) (driver.Completion, error) { return driver.Completion{}, nil }
		result := rig.invoke(nil, requestOf(rig))
		if result.Status != StatusAccepted || result.KernelOutcome != "" {
			t.Fatalf("%#v", result)
		}
	})
	t.Run("protocol_ack", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		result := rig.invoke(nil, requestOf(rig))
		if result.Status != StatusProtocolAck || result.Acknowledgement != operation.AckAccepted || result.ReadbackRelation != "" {
			t.Fatalf("%#v", result)
		}
	})
	t.Run("readback_confirmed", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		request := requestOf(rig)
		later := rig.plant.laterFact(rig.plant.argumentFact, zoneTemperature(), rig.now.Add(3*time.Second), "candidate:confirm", "2")
		rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
			return readbackCompletion(dispatch, later, "candidate:confirm", "2", rig.now.Add(4*time.Second), operation.OutcomeApplied), nil
		}
		result := rig.invoke(nil, request)
		if result.Status != StatusReadbackConfirmed || result.KernelOutcome != operation.OutcomeApplied || result.ReadbackRelation != operation.ReadbackConfirms {
			t.Fatalf("%#v", result)
		}
	})
	t.Run("conflict", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		request := requestOf(rig)
		later := rig.plant.laterFactReplacing(rig.plant.argumentFact, qtyExp("3", 2, "unit.kelvin"), rig.now.Add(3*time.Second), "candidate:other", "2", []semreg.CandidateID{"candidate:primary"})
		rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
			return readbackCompletion(dispatch, later, "candidate:other", "2", rig.now.Add(4*time.Second), operation.OutcomeConflict), nil
		}
		result := rig.invoke(nil, request)
		assertRefusal(t, result, StatusFailed, operation.OutcomeConflict, operation.DeliverySent, "")
	})
	t.Run("no_effect", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
			completion := acknowledged(dispatch)
			completion.Acknowledgement.State = operation.AckRejected
			return completion, nil
		}
		result := rig.invoke(nil, requestOf(rig))
		assertRefusal(t, result, StatusFailed, operation.OutcomeNoEffect, operation.DeliverySent, "")
	})
	t.Run("unknown", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
			completion := acknowledged(dispatch)
			completion.Acknowledgement = nil
			completion.Dispatch.Delivery = operation.DeliveryUnknown
			return completion, nil
		}
		result := rig.invoke(nil, requestOf(rig))
		if result.Status != StatusUnknown || result.KernelOutcome != operation.OutcomeIndeterminate {
			t.Fatalf("%#v", result)
		}
	})
	t.Run("stale readback is not confirmation", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		request := requestOf(rig)
		later := rig.plant.laterFact(rig.plant.argumentFact, zoneTemperature(), rig.now.Add(3*time.Second), "candidate:stale", "2")
		original := rig.plant.asset
		rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
			completion := readbackCompletion(dispatch, later, "candidate:stale", "2", rig.now.Add(4*time.Second), operation.OutcomeApplied)
			completion.Readback.SourceID = "source:other"
			return completion, nil
		}
		result := rig.invoke(nil, request)
		if result.Status == StatusReadbackConfirmed || rig.owner.intent().AssetID != original {
			t.Fatalf("result %#v asset %s", result, rig.owner.intent().AssetID)
		}
		if result.Status != StatusProtocolAck {
			t.Fatalf("%#v", result)
		}
	})
}

func TestCompoundPartialSteps(t *testing.T) {
	rig := newRig(t, thermalSpec())
	rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
		completion := acknowledged(dispatch)
		completion.Acknowledgement.State = operation.AckRejected
		completion.CompoundExecution = &driver.CompoundExecution{Partial: true, Steps: []driver.CompoundStep{
			{Point: "first", Attempted: true, Delivery: operation.DeliverySent, Acknowledgement: operation.AckAccepted},
			{Point: "second", Attempted: true, Delivery: operation.DeliverySent, Acknowledgement: operation.AckRejected},
			{Point: "third", Attempted: false},
		}}
		return completion, nil
	}
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	if result.Status == StatusReadbackConfirmed || result.KernelOutcome == operation.OutcomeFailedNoContact || result.CompoundExecution == nil || !result.CompoundExecution.Partial || len(result.CompoundExecution.Steps) != 3 {
		t.Fatalf("%#v", result)
	}
	if result.CompoundExecution.Steps[2].Attempted || result.CompoundExecution.Steps[0].Acknowledgement != operation.AckAccepted {
		t.Fatalf("steps %#v", result.CompoundExecution.Steps)
	}
	lost := newRig(t, thermalSpec())
	lost.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
		completion := acknowledged(dispatch)
		completion.Acknowledgement = nil
		completion.Dispatch.Delivery = operation.DeliveryUnknown
		completion.CompoundExecution = &driver.CompoundExecution{Partial: true, Steps: []driver.CompoundStep{
			{Point: "first", Attempted: true, Delivery: operation.DeliverySent, Acknowledgement: operation.AckAccepted},
			{Point: "second", Attempted: true, Delivery: operation.DeliveryUnknown},
			{Point: "third", Attempted: false},
		}}
		return completion, nil
	}
	lostResult := lost.invoke(nil, lost.plant.request([]semreg.TypedField{{ID: lost.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	if lostResult.Status != StatusUnknown || lostResult.KernelOutcome == operation.OutcomeFailedNoContact || lostResult.Status == StatusReadbackConfirmed {
		t.Fatalf("%#v", lostResult)
	}
}

func TestSingleAcknowledgedCompoundStepIsNotFullSuccess(t *testing.T) {
	rig := newRig(t, thermalSpec())
	request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second)
	later := rig.plant.laterFact(rig.plant.argumentFact, zoneTemperature(), rig.now.Add(3*time.Second), "candidate:one", "2")
	rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
		completion := readbackCompletion(dispatch, later, "candidate:one", "2", rig.now.Add(4*time.Second), operation.OutcomeApplied)
		completion.CompoundExecution = &driver.CompoundExecution{Steps: []driver.CompoundStep{
			{Point: "only", Attempted: true, Delivery: operation.DeliverySent, Acknowledgement: operation.AckAccepted},
		}}
		return completion, nil
	}
	result := rig.invoke(nil, request)
	if result.Status == StatusReadbackConfirmed || result.Status != StatusProtocolAck {
		t.Fatalf("%#v", result)
	}
}

func TestCallBoundMatterCallback(t *testing.T) {
	rig := newRig(t, thermalSpec())
	request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 0)
	later := rig.plant.laterFact(rig.plant.argumentFact, zoneTemperature(), rig.now.Add(3*time.Second), "candidate:matter", "2")
	rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
		if dispatch.Guard.DriverGeneration != rig.plant.generation || dispatch.Guard.BindingID != rig.plant.binding {
			t.Fatalf("guard %#v", dispatch.Guard)
		}
		return readbackCompletion(dispatch, later, "candidate:matter", "2", rig.now.Add(4*time.Second), operation.OutcomeApplied), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = WithCaller(ctx, Caller{GraphQL: true, Qualification: "matter"})
	var ops driver.ScopedOperations = rig.router.ScopeOf(rig.scope)
	result, err := ops.InvokeCallBound(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusReadbackConfirmed {
		t.Fatalf("%#v", result)
	}
	intent := rig.owner.intent()
	if intent.Deadline != (semreg.TimePoint{}) || intent.Causal.ExpiresAt != (semreg.TimePoint{}) || intent.Authority.Kind != "semantic.caller.graphql" {
		t.Fatalf("intent deadline %#v causal %#v authority %s", intent.Deadline, intent.Causal, intent.Authority.Kind)
	}
	if _, err := rig.router.InvokeCallBound(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	idle := &scriptExec{}
	_ = idle
	calls, _ := rig.exec.counts()
	if calls != 1 {
		t.Fatalf("second shape called executor, calls %d", calls)
	}
}

func TestCallBoundCancellation(t *testing.T) {
	t.Run("before send", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 0)
		result, err := rig.router.InvokeCallBound(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		assertIdle(t, rig.exec)
		assertRefusal(t, result, StatusFailed, operation.OutcomeFailedNoContact, operation.DeliveryNotSent, ReasonNoContact)
	})
	t.Run("after entry before send", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		entered := make(chan struct{})
		release := make(chan struct{})
		rig.exec.entered = entered
		rig.exec.release = release
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 0)
		done := make(chan driver.OperationResult, 1)
		go func() {
			result, err := rig.router.InvokeCallBound(ctx, request)
			if err != nil {
				t.Error(err)
			}
			done <- result
		}()
		<-entered
		cancel()
		close(release)
		result := <-done
		calls, sends := rig.exec.counts()
		if calls != 1 || sends != 0 {
			t.Fatalf("calls %d sends %d", calls, sends)
		}
		assertRefusal(t, result, StatusFailed, operation.OutcomeFailedNoContact, operation.DeliveryNotSent, ReasonNoContact)
	})
	t.Run("after possible send", func(t *testing.T) {
		rig := newRig(t, thermalSpec())
		entered := make(chan struct{})
		release := make(chan struct{})
		rig.exec.entered = entered
		rig.exec.release = release
		rig.exec.allowAfterCancel = true
		rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
			completion := acknowledged(dispatch)
			completion.Acknowledgement = nil
			completion.Dispatch.Delivery = operation.DeliveryUnknown
			return completion, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 0)
		done := make(chan driver.OperationResult, 1)
		go func() {
			result, err := rig.router.InvokeCallBound(ctx, request)
			if err != nil {
				t.Error(err)
			}
			done <- result
		}()
		<-entered
		cancel()
		close(release)
		result := <-done
		calls, sends := rig.exec.counts()
		if calls != 1 || sends != 1 || result.Status != StatusUnknown || result.Delivery == operation.DeliveryNotSent {
			t.Fatalf("calls %d sends %d result %#v", calls, sends, result)
		}
	})
}

func TestCallBoundRejectsPersistedAsyncAndReplay(t *testing.T) {
	rig := newRig(t, thermalSpec())
	request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, mode := range []DispatchMode{ModePersisted, ModeAsync, ModeReplay} {
		result, err := rig.router.InvokeCallBound(WithDispatchMode(ctx, mode), request)
		if err != nil {
			t.Fatal(err)
		}
		assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonCallBoundInvalid)
	}
	timed, stop := context.WithTimeout(context.Background(), time.Hour)
	defer stop()
	result, err := rig.router.InvokeCallBound(timed, request)
	if err != nil {
		t.Fatal(err)
	}
	assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonCallBoundInvalid)
	assertIdle(t, rig.exec)
	finite := request
	finite.Timeout = 0
	finiteResult := rig.invoke(nil, finite)
	assertRefusal(t, finiteResult, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonInvalidArgument)
}

func TestSemanticDispatchDoesNotApplyARawWhitelist(t *testing.T) {
	rig := newRig(t, thermalSpec())
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	calls, sends := rig.exec.counts()
	if calls != 1 || sends != 1 || result.Status != StatusProtocolAck {
		t.Fatalf("calls %d sends %d result %#v", calls, sends, result)
	}
}
