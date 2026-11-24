package driver_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/catalog"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func digestOf(s string) v1.Digest {
	sum := sha256.Sum256([]byte(s))
	return v1.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func evidence(s string) v1.EvidenceRef {
	return v1.EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: digestOf(s), Contract: "test.contract/v1", Access: v1.EvidenceAccessPublic, Redaction: v1.RedactionNone}
}

func evidenceSet(s string) []v1.EvidenceRef { return []v1.EvidenceRef{evidence(s)} }

func packRef() v1.PackRef { return v1.PackRef{ID: "pack.test", Version: "1.0.0"} }

func defRef(id string) v1.DefinitionRef {
	return v1.DefinitionRef{Pack: packRef(), ID: v1.DefinitionID(id), Version: "1.0.0"}
}

func node(id string) driver.NodeRef {
	return driver.NodeRef{Bridge: "bridge.local", ID: v1.ResourceID("resource:" + id)}
}

func factKey(id string) v1.FactKey {
	return v1.FactKey{PackID: "pack.test", PackVersion: "1.0.0", FactID: v1.DefinitionID("fact." + id), Dimensions: []v1.Dimension{}}
}

func element(subject, fact string) driver.Element {
	return driver.Element{Subject: node(subject), Key: factKey(fact)}
}

func quantity(coefficient string, exponent int32, unit string) *v1.Quantity {
	return &v1.Quantity{Number: v1.Decimal{Coefficient: coefficient, Exponent10: exponent}, Unit: v1.DefinitionID(unit)}
}

func source(path string, priority int32) driver.Source {
	return driver.Source{For: element("boiler", "flow"), Producer: node("producer-" + path), PathKey: path, Priority: priority, Step: quantity("5", -1, "unit.kelvin")}
}

func sourceRef(path string) driver.SourceRef {
	s := source(path, 0)
	return driver.SourceRef{For: s.For, Producer: s.Producer, PathKey: s.PathKey}
}

func route() operation.Route {
	return operation.Route{CapabilityInstance: "capability:one", ServiceInstance: "service:one", BindingID: "binding:one", SourceID: "source:one", SourceEpochID: "epoch:one", DriverGeneration: "1"}
}

func settingRoute(path string) driver.SettingRoute {
	return driver.SettingRoute{For: element("boiler", "flow"), Operation: defRef("operation.set"), Owner: sourceRef(path), Route: route()}
}

func timePoint() v1.TimePoint {
	return v1.TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"}
}

func monotonic(n string) v1.MonotonicPoint {
	return v1.MonotonicPoint{ClockEpochID: "clock:test", Nanoseconds: v1.Uint64(n)}
}

func candidate(id string) v1.FactCandidate {
	binding, epoch, generation := v1.NativeBindingID("binding:"+id), v1.SourceEpochID("epoch:"+id), v1.Uint64("1")
	value := true
	origin := v1.OriginRef{OriginID: v1.OriginID("origin:" + id), Kind: v1.OriginNativeObservation, SourceID: ptr(v1.SourceID("source:" + id)), SourceEpochID: &epoch, BindingID: &binding, Evidence: evidenceSet(id)}
	return v1.FactCandidate{
		CandidateID: v1.CandidateID("candidate:" + id), Key: factKey("flow"),
		Value:           &v1.Value{Kind: v1.ValueBoolean, Boolean: &value},
		Quality:         v1.Quality{Assertion: v1.AssertionObserved, Qualification: v1.QualificationQualified, Promotion: v1.PromotionPromoted, Validity: v1.ValidityGood, Availability: v1.AvailabilityAvailable, Freshness: v1.FreshnessFresh, Reasons: []v1.DefinitionID{}},
		Times:           v1.Times{ReceivedAt: timePoint(), ReceiptMonotonic: monotonic("5"), EvaluatedAt: v1.TimePoint{UnixNanoseconds: "101", ClockID: "clock.utc", UncertaintyNS: "0"}, EvaluateMonotonic: monotonic("6")},
		FreshnessPolicy: v1.FreshnessPolicy{PolicyID: "policy:test", Version: "1.0.0", FreshForNS: "30", RetainForNS: "300", MaxWallUncertaintyNS: "1"},
		BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation, Origin: origin, Evidence: evidenceSet(id), Revision: "1",
	}
}

func ptr[T any](v T) *T { return &v }

func revisionVector() v1.RevisionVector {
	return v1.RevisionVector{Semantic: "1", Identity: "1", Facts: "1", Services: "1", Capabilities: "1"}
}

func expectedEffect() operation.ExpectedEffect {
	value := true
	return operation.ExpectedEffect{Rule: defRef("rule.set"), Fact: factKey("flow"), Operator: v1.PredicateEqual, Expected: v1.Value{Kind: v1.ValueBoolean, Boolean: &value}}
}

func operationRequest() driver.OperationRequest {
	value := true
	return driver.OperationRequest{
		AssetID: "asset:one", BindingID: "binding:one", CapabilityInstanceID: "capability:one", Operation: defRef("operation.set"),
		Arguments:                v1ArgumentSet(value),
		ExpectedSemanticRevision: "1", ExpectedCapabilityRevision: "1", ExpectedCapabilityInstanceRevision: "1",
		ExpectedDriverGeneration: "1", ExpectedSourceEpochID: "epoch:one", Preconditions: []operation.Precondition{},
	}
}

func v1ArgumentSet(value bool) []v1.TypedField {
	return []v1.TypedField{{ID: "field.enabled", Value: v1.Value{Kind: v1.ValueBoolean, Boolean: &value}}}
}

// sourceBackedFamily is a family a driver declares: qualified from its
// specification, with a recorded hardware test and a claimed composition.
func sourceBackedFamily() catalog.Family {
	return catalog.Family{
		FamilyID: "example-family", Name: "Example family", NativeOwner: "Project-Helianthus/example-reg",
		SemanticPack: "helianthus.pack.pv@1.0.0", State: v1.QualificationQualified, Composition: catalog.CompositionComposed,
		Reason: catalog.ReasonDeclaredBySpecification,
		Evidence: []catalog.Evidence{
			{Owner: "Project-Helianthus/example-reg@v1.0.0", Kind: catalog.EvidenceKindSpecification, Locator: "docs/specification.md", Statement: "the public specification declares the mapping"},
			{Owner: "Project-Helianthus/example-reg@v1.0.0", Kind: catalog.EvidenceKindHardwareTestRecord, Locator: "docs/hardware-test.md", Statement: "a public hardware test record exists"},
		},
		CompositionVerified: true, Verification: catalog.VerificationTestedOnHardware,
		Module: "github.com/Project-Helianthus/example-reg", Version: "v1.0.0",
	}
}

func wantInvalid(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: accepted", name)
	}
}

func wantValid(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", name, err)
	}
}
