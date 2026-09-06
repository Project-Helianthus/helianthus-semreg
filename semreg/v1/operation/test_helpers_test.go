package operation_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

var (
	testPackRef = semreg.PackRef{ID: "pack.test", Version: "1.0.0"}
	testField   = semreg.DefinitionRef{Pack: testPackRef, ID: "field.test.level", Version: "1.0.0"}
	testService = semreg.DefinitionRef{Pack: testPackRef, ID: "service.test.control", Version: "1.0.0"}
	testCap     = semreg.DefinitionRef{Pack: testPackRef, ID: "capability.test.level", Version: "1.0.0"}
	testOp      = semreg.DefinitionRef{Pack: testPackRef, ID: "operation.test.set_level", Version: "1.0.0"}
	testEffect  = semreg.DefinitionRef{Pack: testPackRef, ID: "effect.test.level", Version: "1.0.0"}
	preKey      = semreg.FactKey{PackID: testPackRef.ID, PackVersion: testPackRef.Version, FactID: "fact.test.interlock", Dimensions: []semreg.Dimension{}}
	readbackKey = semreg.FactKey{PackID: testPackRef.ID, PackVersion: testPackRef.Version, FactID: "fact.test.level", Dimensions: []semreg.Dimension{}}
)

type testOperationPack struct {
	intentCalls   int
	readbackCalls int
	mutateHooks   bool
}

func (p *testOperationPack) Pack() semreg.PackRef { return testPackRef }
func (p *testOperationPack) Definitions() semreg.DefinitionIndex {
	return semreg.DefinitionIndex{
		Pack: testPackRef, Fields: []semreg.DefinitionRef{testField}, Services: []semreg.DefinitionRef{testService},
		Capabilities: []semreg.DefinitionRef{testCap}, Operations: []semreg.DefinitionRef{testOp}, EffectRules: []semreg.DefinitionRef{testEffect},
	}
}
func (p *testOperationPack) ValidateFact(key semreg.FactKey, value *semreg.Value) error {
	if key.FactID != preKey.FactID && key.FactID != readbackKey.FactID && !strings.HasPrefix(string(key.FactID), "fact.test.padding.") {
		return errors.New("unknown fact")
	}
	if value == nil || value.Kind != semreg.ValueBoolean || value.Boolean == nil {
		return errors.New("boolean required")
	}
	return nil
}
func (p *testOperationPack) ValidateService(semreg.ServiceInstance) error       { return nil }
func (p *testOperationPack) ValidateCapability(semreg.CapabilityInstance) error { return nil }
func (p *testOperationPack) ValidateField(ref semreg.DefinitionRef, field semreg.TypedField) error {
	if ref != testField || field.ID != testField.ID || field.Value.Boolean == nil {
		return errors.New("invalid level")
	}
	return nil
}
func (p *testOperationPack) MatchConstraints(_ semreg.CapabilityInstance, fields []semreg.TypedField) error {
	if len(fields) != 1 || fields[0].Value.Boolean == nil || !*fields[0].Value.Boolean {
		return errors.New("constraint mismatch")
	}
	return nil
}
func (p *testOperationPack) EvaluatePredicate(candidate semreg.FactCandidate, operator semreg.PredicateOp, expected semreg.Value) (bool, error) {
	if operator != semreg.PredicateEqual || candidate.Value == nil || candidate.Value.Boolean == nil || expected.Boolean == nil {
		return false, errors.New("unsupported predicate")
	}
	return *candidate.Value.Boolean == *expected.Boolean, nil
}
func (p *testOperationPack) ValidateIntent(intent operation.Intent) error {
	p.intentCalls++
	want := expectedEffect(true)
	if !reflect.DeepEqual(intent.ExpectedEffect, want) || len(intent.Arguments) != 1 || intent.Arguments[0].ID != testField.ID || intent.Arguments[0].Value.Boolean == nil {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "derived effect mismatch"}
	}
	if p.mutateHooks {
		intent.Arguments[0].ID = "field.test.mutated"
		intent.Preconditions[0].CandidateID = "candidate:mutated"
	}
	return nil
}
func (p *testOperationPack) EvaluateReadback(intent operation.Intent, candidate semreg.FactCandidate) (operation.ReadbackRelation, error) {
	p.readbackCalls++
	if !reflect.DeepEqual(candidate.Key, intent.ExpectedEffect.Fact) || candidate.Value == nil || candidate.Value.Boolean == nil || intent.ExpectedEffect.Expected.Boolean == nil {
		return "", errors.New("readback fact")
	}
	if p.mutateHooks {
		intent.Arguments[0].ID = "field.test.mutated"
		candidate.Key.FactID = "fact.test.mutated"
	}
	if *candidate.Value.Boolean == *intent.ExpectedEffect.Expected.Boolean {
		return operation.ReadbackConfirms, nil
	}
	return operation.ReadbackContradicts, nil
}

func boolValue(value bool) semreg.Value {
	copy := value
	return semreg.Value{Kind: semreg.ValueBoolean, Boolean: &copy}
}

func evidence(digit byte) semreg.EvidenceRef {
	return semreg.EvidenceRef{
		Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest("sha256:" + repeatHex(digit)),
		Contract: "test.evidence/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone,
	}
}

func repeatHex(digit byte) string {
	const hex = "0123456789abcdef"
	result := make([]byte, 64)
	for index := range result {
		result[index] = hex[digit%16]
	}
	return string(result)
}

func timePoint(ns string) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(ns), ClockID: "clock.utc", UncertaintyNS: "0"}
}

func context(ns, ticks string) semreg.EvaluationContext {
	return semreg.EvaluationContext{EvaluatedAt: timePoint(ns), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: semreg.Uint64(ticks)}}
}

func expectedEffect(value bool) operation.ExpectedEffect {
	return operation.ExpectedEffect{Rule: testEffect, Fact: readbackKey, Operator: semreg.PredicateEqual, Expected: boolValue(value)}
}

func validIntent() operation.Intent {
	return operation.Intent{
		Contract: operation.ContractOperationV1, IntentID: "intent:test:1", Kind: testOp, ExpectedEffect: expectedEffect(true), AssetID: "asset:test",
		Arguments:          []semreg.TypedField{{ID: testField.ID, Value: boolValue(true)}},
		RequiredCapability: operation.CapabilityRequirement{Pack: testPackRef, DefinitionID: testCap.ID, Versions: semreg.VersionRange{Minimum: "1.0.0", MaximumExclusive: "2.0.0"}},
		Authority:          evidence(1),
		Causal: semreg.CausalContext{
			Origin:        semreg.OriginRef{OriginID: "origin:operator", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{evidence(1)}},
			CorrelationID: "correlation:test:1", HopCount: 0, MaxHops: 8, FirstSeenAt: timePoint("100"), ExpiresAt: timePoint("400"), Path: []semreg.TargetID{},
		},
		ExpectedSemanticRevision: "1", ExpectedCapabilityRevision: "1", ExpectedCapabilityInstanceRevision: "1",
		ExpectedSourceEpochID: "epoch:test:1", ExpectedDriverGeneration: "1",
		Preconditions:  []operation.Precondition{{Fact: preKey, CandidateID: "candidate:interlock", CandidateRevision: "1", Operator: semreg.PredicateEqual, Expected: boolValue(true)}},
		IdempotencyKey: "idempotency:test:1", Deadline: timePoint("300"),
	}
}

type operationFixture struct {
	pack        *testOperationPack
	publication *semreg.PublicationKernel
	kernel      *operation.Kernel
	snapshot    semreg.Snapshot
	intent      operation.Intent
	current     semreg.EvaluationContext
}

func newOperationFixture(t *testing.T) operationFixture {
	return newOperationFixtureWith(t, nil)
}

func newOperationFixtureWith(t *testing.T, mutate func(*semreg.PublicationBatch)) operationFixture {
	t.Helper()
	pack := &testOperationPack{}
	publication, err := semreg.NewPublicationKernel("asset:test", pack)
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := operation.NewKernel(pack)
	if err != nil {
		t.Fatal(err)
	}
	batch := semreg.PublicationBatch{
		Contract: semreg.ContractKernelV1, BatchID: "batch:test:1", AssetID: "asset:test", SourceID: "source:test", SourceEpochID: "epoch:test:1",
		DriverGeneration: "1", Sequence: "1", ExpectedSemanticRevision: "0", ObservedAt: timePoint("100"),
		SourceUpserts:       []semreg.SourceDescriptor{{SourceID: "source:test", SourceEpochID: "epoch:test:1", ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1", RegistryEvidence: evidence(1), StartedAt: timePoint("90"), State: semreg.SourceCurrent, Revision: "1"}},
		SourceRetirements:   []semreg.SourceEpochID{},
		BindingUpserts:      []semreg.NativeBinding{{BindingID: "binding:test", AssetID: "asset:test", SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1", NativeResource: evidence(2), State: semreg.BindingCurrent, Revision: "1"}},
		IdentityLinkUpserts: []semreg.IdentityLink{{AssetID: "asset:test", BindingID: "binding:test", State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{evidence(3)}, Revision: "1"}},
		FactUpserts:         []semreg.FactCandidate{factCandidate("candidate:interlock", preKey, true, "100", "100", "1")}, FactWithdrawals: []semreg.CandidateID{},
		ServiceUpserts:        []semreg.ServiceInstance{{InstanceID: "service:test", AssetID: "asset:test", Definition: testService, BindingID: "binding:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}},
		ServiceWithdrawals:    []semreg.ServiceInstanceID{},
		CapabilityUpserts:     []semreg.CapabilityInstance{{InstanceID: "capability:test", AssetID: "asset:test", ServiceInstance: "service:test", Definition: testCap, BindingID: "binding:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Constraints: []semreg.TypedField{}, ActivationEvidence: []semreg.EvidenceRef{evidence(4)}, Revision: "1"}},
		CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{},
	}
	if mutate != nil {
		mutate(&batch)
	}
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: "100"})
	if err != nil {
		t.Fatal(err)
	}
	return operationFixture{pack: pack, publication: publication, kernel: kernel, snapshot: snapshot, intent: validIntent(), current: context("150", "150")}
}

func factCandidate(id semreg.CandidateID, key semreg.FactKey, value bool, wall, ticks string, revision semreg.Uint64) semreg.FactCandidate {
	binding := semreg.NativeBindingID("binding:test")
	epoch := semreg.SourceEpochID("epoch:test:1")
	generation := semreg.Uint64("1")
	return semreg.FactCandidate{
		CandidateID: id, Key: key, Value: func() *semreg.Value { value := boolValue(value); return &value }(),
		Quality:         semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}},
		Times:           semreg.Times{ReceivedAt: timePoint(wall), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: semreg.Uint64(ticks)}, EvaluatedAt: timePoint(wall), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: semreg.Uint64(ticks)}},
		FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy.test", Version: "1.0.0", FreshForNS: "1000", RetainForNS: "2000", MaxWallUncertaintyNS: "10"},
		BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
		Origin:   semreg.OriginRef{OriginID: semreg.OriginID("origin:" + string(id)), Kind: semreg.OriginNativeObservation, SourceID: func() *semreg.SourceID { value := semreg.SourceID("source:test"); return &value }(), SourceEpochID: &epoch, BindingID: &binding, Evidence: []semreg.EvidenceRef{evidence(5)}},
		Evidence: []semreg.EvidenceRef{evidence(5)}, Revision: revision,
	}
}

func authorize(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil }

func mustAdmit(t *testing.T, fixture operationFixture) *operation.Admission {
	t.Helper()
	admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

func errorID(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if got := semreg.ErrorIdentifier(err); got != want {
		t.Fatalf("error id: got %q want %q (%v)", got, want, err)
	}
}
