package operation_test

import (
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

func TestCallBoundAdmissionKeepsWireExpiry(t *testing.T) {
	fixture := newOperationFixture(t)
	if err := fixture.intent.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.intent.Causal.Validate(); err != nil {
		t.Fatal(err)
	}
	finite := fixture.intent
	finite.Deadline = semreg.TimePoint{}
	if err := finite.Validate(); err == nil {
		t.Fatal("finite intent accepted a missing deadline")
	}
	if _, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, finite, operation.AuthorityResolverFunc(authorize)); err == nil {
		t.Fatal("finite admission accepted a missing deadline")
	}
	callBound := fixture.intent
	callBound.IntentID = "intent:call-bound"
	callBound.IdempotencyKey = "idempotency:call-bound"
	callBound.Deadline = semreg.TimePoint{}
	callBound.Causal.ExpiresAt = semreg.TimePoint{}
	if err := callBound.Validate(); err == nil {
		t.Fatal("wire validate accepted a call-bound intent")
	}
	if err := callBound.ValidateWithoutLifetime(); err != nil {
		t.Fatal(err)
	}
	if err := callBound.Causal.Validate(); err == nil {
		t.Fatal("wire causal validate accepted a missing expiry")
	}
	if err := callBound.Causal.ValidateWithoutExpiry(); err != nil {
		t.Fatal(err)
	}
	broken := callBound.Causal
	broken.HopCount = 2
	if err := broken.ValidateWithoutExpiry(); err == nil {
		t.Fatal("hop budget accepted a path shorter than the hop count")
	}
	admission, err := fixture.kernel.AdmitCallBound(fixture.snapshot, fixture.current, callBound, operation.AuthorityResolverFunc(authorize))
	if err != nil {
		t.Fatal(err)
	}
	if !admission.CallBound() {
		t.Fatal("call-bound admission lost its marker")
	}
	record := operation.ExecutionRecord{Contract: operation.ContractOperationV1, Intent: admission.Intent(), Outcome: operation.OutcomeAcknowledgedUnverified, OutcomeEvidence: []semreg.EvidenceRef{evidence(1)}}
	if err := record.Validate(); err == nil {
		t.Fatal("wire execution record accepted a call-bound intent")
	}
	if _, err := fixture.kernel.Record(admission, record, nil); err == nil {
		t.Fatal("recorded a call-bound admission")
	}
	if _, err := fixture.kernel.Record(admission, record, nil); err == nil {
		t.Fatal("a second record stored the call-bound admission")
	}
	wire, err := operation.EnterCausal(fixture.intent.Causal, "runtime:call", timePoint("150"))
	if err != nil {
		t.Fatal(err)
	}
	if len(wire.Path) != 1 || wire.Path[0] != "runtime:call" {
		t.Fatalf("finite enter %#v", wire.Path)
	}
	if _, err := operation.EnterCausal(callBound.Causal, "runtime:call", timePoint("150")); err == nil {
		t.Fatal("finite enter accepted a context without expiry")
	}
	entered, err := operation.EnterCausalWithoutExpiry(callBound.Causal, "runtime:call", timePoint("150"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entered.Path) != 1 || entered.Path[0] != "runtime:call" || entered.HopCount != 1 || entered.ExpiresAt != (semreg.TimePoint{}) {
		t.Fatalf("call-bound enter %#v", entered)
	}
	echo := callBound.Causal
	echo.Path = []semreg.TargetID{"runtime:call"}
	echo.HopCount = 1
	if _, err := operation.EnterCausalWithoutExpiry(echo, "runtime:call", timePoint("150")); semreg.ErrorIdentifier(err) != semreg.EchoSuppressed {
		t.Fatalf("echo %v", err)
	}
	budget := callBound.Causal
	budget.HopCount = 1
	budget.MaxHops = 1
	budget.Path = []semreg.TargetID{"runtime:upstream"}
	if _, err := operation.EnterCausalWithoutExpiry(budget, "runtime:call", timePoint("150")); semreg.ErrorIdentifier(err) != semreg.CausalBudgetExceeded {
		t.Fatalf("budget %v", err)
	}
}

func TestThermalHookRevalidatesWithoutDurableLifetime(t *testing.T) {
	hook := thermal.New().(operation.OperationPackValidator)
	intent := thermalProcessIntent()
	if err := intent.Validate(); err == nil {
		t.Fatal("thermal wire intent accepted a missing deadline")
	}
	if err := hook.ValidateIntent(intent); err != nil {
		t.Fatal(err)
	}
	intent.ExpectedEffect.Rule.ID = "thermal.effect.set_mode"
	if err := hook.ValidateIntent(intent); err == nil {
		t.Fatal("thermal hook accepted a mismatched effect")
	}
}

func thermalProcessIntent() operation.Intent {
	text := "identity"
	value := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "29315", Exponent10: -2}, Unit: "unit.kelvin"}}
	pack := semreg.PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}
	return operation.Intent{
		Contract: operation.ContractOperationV1, IntentID: "intent:thermal:call", Kind: semreg.DefinitionRef{Pack: pack, ID: "thermal.operation.set_temperature", Version: "1.0.0"},
		ExpectedEffect: operation.ExpectedEffect{
			Rule:     semreg.DefinitionRef{Pack: pack, ID: "thermal.effect.set_temperature", Version: "1.0.0"},
			Fact:     semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: "thermal.setpoint.temperature_setpoint", Dimensions: []semreg.Dimension{{ID: "thermal.dimension.zone", Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}}},
			Operator: semreg.PredicateEqual, Expected: value,
		},
		AssetID: "asset:1", Arguments: []semreg.TypedField{{ID: "thermal.setpoint.temperature_setpoint", Value: value}},
		RequiredCapability: operation.CapabilityRequirement{Pack: pack, DefinitionID: "thermal.capability.set_temperature", Versions: semreg.VersionRange{Minimum: "1.0.0", MaximumExclusive: "2.0.0"}},
		Authority:          evidence(1),
		Causal: semreg.CausalContext{
			Origin:        semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{evidence(1)}},
			CorrelationID: "correlation:thermal", HopCount: 0, MaxHops: 1, FirstSeenAt: timePoint("1"), Path: []semreg.TargetID{},
		},
		ExpectedSemanticRevision: "1", ExpectedCapabilityRevision: "1", ExpectedCapabilityInstanceRevision: "1",
		ExpectedSourceEpochID: "epoch:1", ExpectedDriverGeneration: "1", Preconditions: []operation.Precondition{}, IdempotencyKey: "idempotency:thermal:call",
	}
}
