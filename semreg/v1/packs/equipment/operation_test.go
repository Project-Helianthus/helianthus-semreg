package equipment

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestUnionOperationArguments(t *testing.T) {
	validator := New().(operation.OperationPackValidator)
	for id, spec := range operations {
		intent := unionIntent(id, true)
		if err := validator.ValidateIntent(intent); err != nil {
			t.Fatalf("%s complete: %v", id, err)
		}
		omitted := unionIntent(id, false)
		if err := validator.ValidateIntent(omitted); err != nil {
			t.Fatalf("%s omitted parameter: %v", id, err)
		}
		missing := omitted
		missing.Arguments = []semreg.TypedField{}
		if semreg.ErrorIdentifier(validator.ValidateIntent(missing)) != semreg.InvalidValue {
			t.Fatalf("%s missing argument accepted", id)
		}
		unknown := omitted
		unknown.Arguments = append([]semreg.TypedField{}, omitted.Arguments...)
		unknown.Arguments = append(unknown.Arguments, semreg.TypedField{ID: "zz.field.other", Value: unionValue(mustUnionField(spec.argument))})
		if semreg.ErrorIdentifier(validator.ValidateIntent(unknown)) != semreg.InvalidValue {
			t.Fatalf("%s undeclared field accepted", id)
		}
		if len(operationParameters[id]) == 0 {
			continue
		}
		repeated := unionIntent(id, true)
		repeated.Arguments = append(repeated.Arguments, repeated.Arguments[len(repeated.Arguments)-1])
		if semreg.ErrorIdentifier(validator.ValidateIntent(repeated)) != semreg.DuplicateKey {
			t.Fatalf("%s repeated parameter accepted", id)
		}
		readback, err := validator.EvaluateReadback(omitted, unionCandidate(omitted.ExpectedEffect.Fact, omitted.ExpectedEffect.Expected))
		if err != nil || readback != operation.ReadbackConfirms {
			t.Fatalf("%s readback %s %v", id, readback, err)
		}
	}
}

func unionIntent(id semreg.DefinitionID, withParameters bool) operation.Intent {
	spec := operations[id]
	field := mustUnionField(spec.argument)
	value := unionValue(field)
	arguments := []semreg.TypedField{{ID: spec.argument, Value: value}}
	if withParameters {
		for _, parameter := range operationParameters[id] {
			parameterField := mustUnionField(parameter)
			arguments = append(arguments, semreg.TypedField{ID: parameter, Value: unionValue(parameterField)})
		}
	}
	sort.Slice(arguments, func(i, j int) bool { return arguments[i].ID < arguments[j].ID })
	return operation.Intent{Contract: operation.ContractOperationV1, IntentID: "intent:1", Kind: definition(id), ExpectedEffect: operation.ExpectedEffect{Rule: definition(spec.effect), Fact: unionKey(field), Operator: semreg.PredicateEqual, Expected: value}, AssetID: "asset:1", Arguments: arguments, RequiredCapability: operation.CapabilityRequirement{Pack: pack, DefinitionID: spec.capability, Versions: semreg.VersionRange{Minimum: packVersion, MaximumExclusive: "2.0.0"}}, Authority: unionEvidence(), Causal: semreg.CausalContext{Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{unionEvidence()}}, CorrelationID: "correlation:1", MaxHops: 1, FirstSeenAt: unionTime("1"), ExpiresAt: unionTime("2"), Path: []semreg.TargetID{}}, ExpectedSemanticRevision: "1", ExpectedCapabilityRevision: "1", ExpectedCapabilityInstanceRevision: "1", ExpectedSourceEpochID: "epoch:1", ExpectedDriverGeneration: "1", Preconditions: []operation.Precondition{}, IdempotencyKey: "idempotency:1", Deadline: unionTime("3")}
}

func mustUnionField(id semreg.DefinitionID) fieldSpec {
	field, ok := findField(id)
	if !ok {
		panic(id)
	}
	return field
}

func unionEvidence() semreg.EvidenceRef {
	sum := sha256.Sum256([]byte("equipment operation evidence"))
	return semreg.EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest("sha256:" + hex.EncodeToString(sum[:])), Contract: "test.evidence/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone}
}

func unionTime(value string) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(value), ClockID: "clock.utc", UncertaintyNS: "0"}
}

func unionCandidate(key semreg.FactKey, value semreg.Value) semreg.FactCandidate {
	binding, epoch, generation := semreg.NativeBindingID("binding:1"), semreg.SourceEpochID("epoch:1"), semreg.Uint64("1")
	source := semreg.SourceID("source:1")
	return semreg.FactCandidate{CandidateID: "candidate:1", Key: key, Value: &value, Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}}, Times: semreg.Times{ReceivedAt: unionTime("1"), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "1"}, EvaluatedAt: unionTime("1"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "1"}}, FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1", RetainForNS: "2", MaxWallUncertaintyNS: "0"}, BindingID: &binding, SourceEpochID: &epoch, DriverGeneration: &generation, Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []semreg.EvidenceRef{unionEvidence()}}, Evidence: []semreg.EvidenceRef{unionEvidence()}, Revision: "1"}
}
