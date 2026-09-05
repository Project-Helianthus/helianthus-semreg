package semreg

import (
	"bytes"
	"testing"
)

func TestFoundationVectors(t *testing.T) {
	t.Run("K-POS-002", func(t *testing.T) {
		binding := NativeBinding{BindingID: "binding:01", AssetID: "asset:01", SourceID: "source:01", SourceEpochID: "epoch:01", DriverGeneration: "1", NativeResource: publicEvidence("1"), State: BindingCurrent, Revision: "1"}
		if err := binding.Validate(); err != nil {
			t.Fatalf("native binding: %v", err)
		}
	})
	t.Run("K-POS-001", func(t *testing.T) {
		if err := (Decimal{Coefficient: "23", Exponent10: 1}).Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("K-POS-004", func(t *testing.T) {
		if err := validTimes().Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("K-POS-005", func(t *testing.T) {
		if err := validPolicy().Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("K-POS-006", func(t *testing.T) {
		if err := (Symbol{Namespace: "native.modbusreg.growatt", Token: "0x9f", Known: false}).Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("K-POS-003", func(t *testing.T) {
		d := Derivation{Algorithm: "helianthus.sum.phases", Version: "1.0.0", Inputs: []DerivationInput{{CandidateID: "candidate:a", CandidateRevision: "1", SourcePaths: []SourcePathRef{{BindingID: "binding:a", SourceID: "source:a", SourceEpochID: "epoch:a", DriverGeneration: "1"}}}}, Evidence: []EvidenceRef{}}
		if err := d.Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("K-POS-008", func(t *testing.T) {
		c := CapabilityInstance{InstanceID: "capability:01", AssetID: "asset:01", ServiceInstance: "service:01", Definition: DefinitionRef{Pack: PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}, ID: "helianthus.evse.current_limit", Version: "1.0.0"}, BindingID: "binding:01", SourceEpochID: "epoch:01", DriverGeneration: "1", Qualification: QualificationQualified, Availability: AvailabilityAvailable, Constraints: []TypedField{}, ActivationEvidence: []EvidenceRef{publicEvidence("3")}, Revision: "1"}
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("K-POS-022", func(t *testing.T) {
		evse := testValidator{pack: PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}, index: DefinitionIndex{Pack: PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}, Fields: []DefinitionRef{}, Services: []DefinitionRef{}, Capabilities: []DefinitionRef{}, Operations: []DefinitionRef{{Pack: PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}, ID: "helianthus.evse.set_current_limit", Version: "1.0.0"}}, EffectRules: []DefinitionRef{}}}
		thermal := testValidator{pack: PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}, index: DefinitionIndex{Pack: PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}, Fields: []DefinitionRef{}, Services: []DefinitionRef{}, Capabilities: []DefinitionRef{}, Operations: []DefinitionRef{}, EffectRules: []DefinitionRef{}}}
		r, err := NewRegistry(thermal, evse)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Validator(evse.pack); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("K-NEG-003", func(t *testing.T) {
		requireID(t, (Decimal{Coefficient: "230", Exponent10: 0}).Validate(), InvalidDecimal)
	})
	t.Run("K-NEG-002", func(t *testing.T) {
		b := NativeBinding{BindingID: "binding:01", AssetID: " bus address 03 ", SourceID: "source:01", SourceEpochID: "epoch:01", DriverGeneration: "1", NativeResource: publicEvidence("1"), State: BindingCurrent, Revision: "1"}
		requireID(t, b.Validate(), InvalidIdentifier)
	})
	t.Run("K-NEG-004", func(t *testing.T) {
		b := true
		requireID(t, (Value{Kind: ValueBoolean, Boolean: &b, Text: pointer("x")}).Validate(), InvalidValue)
	})
	t.Run("K-NEG-005", func(t *testing.T) {
		p := validPolicy()
		p.RetainForNS = p.FreshForNS
		requireID(t, p.Validate(), InvalidTime)
	})
	t.Run("K-NEG-006", func(t *testing.T) {
		x := validTimes()
		x.EvaluateMonotonic.ClockEpochID = "epoch:other"
		requireID(t, x.Validate(), IncomparableClockEpoch)
	})
	t.Run("K-NEG-007", func(t *testing.T) {
		e := publicEvidence("a")
		e.Access = EvidenceAccessRestricted
		e.Redaction = RedactionNone
		e.Digest = "sha256:ABC"
		requireID(t, e.Validate(), InvalidEvidence)
	})
	t.Run("K-NEG-009", func(t *testing.T) {
		requireID(t, (IdentityLink{AssetID: "asset:01", BindingID: "binding:01", State: LinkQualified, Basis: []EvidenceRef{}, Revision: "1"}).Validate(), IdentityNotQualified)
	})
	t.Run("K-NEG-027", func(t *testing.T) {
		_, err := DecodeDecimal([]byte(`{"coefficient":"23","coefficient":"24","exponent10":1}`))
		requireID(t, err, DuplicateKey)
	})
	t.Run("K-NEG-028", func(t *testing.T) {
		_, err := DecodeDecimal([]byte(`{"coefficient":"23","exponent10":1,"display_precision":1}`))
		requireID(t, err, UnknownMember)
	})
	t.Run("K-NEG-029", func(t *testing.T) {
		_, err := DecodeDecimal([]byte(`{"coefficient":"23","exponent10":1} trailing`))
		requireID(t, err, InvalidJSON)
	})
	t.Run("K-NEG-030", func(t *testing.T) {
		_, err := DecodeDecimal([]byte(`{"coefficient":"23"}`))
		requireID(t, err, MissingMember)
	})
	t.Run("K-NEG-031", func(t *testing.T) {
		q := validQuality()
		q.Assertion = "measured"
		requireID(t, q.Validate(), InvalidEnum)
	})
	t.Run("K-NEG-032", func(t *testing.T) {
		f := FactEnvelope{AssetID: "asset:01", Key: FactKey{PackID: "helianthus.pack.test", PackVersion: "1.0.0", FactID: "helianthus.test.value", Dimensions: []Dimension{}}, Candidates: make([]FactCandidate, 33), Conflicts: []Conflict{}, Revision: "1"}
		requireID(t, f.Validate(), BoundsExceeded)
	})
	t.Run("K-NEG-033", func(t *testing.T) {
		d := Derivation{Algorithm: "helianthus.sum.phases", Version: "1.0.0", Inputs: []DerivationInput{{CandidateID: "candidate:b", CandidateRevision: "1", SourcePaths: []SourcePathRef{{BindingID: "binding:b", SourceID: "source:b", SourceEpochID: "epoch:b", DriverGeneration: "1"}}}, {CandidateID: "candidate:a", CandidateRevision: "1", SourcePaths: []SourcePathRef{{BindingID: "binding:a", SourceID: "source:a", SourceEpochID: "epoch:a", DriverGeneration: "1"}}}}, Evidence: []EvidenceRef{}}
		requireID(t, d.Validate(), NoncanonicalOrder)
	})
	t.Run("K-NEG-048", func(t *testing.T) {
		c := CapabilityInstance{InstanceID: "capability:01", AssetID: "asset:01", ServiceInstance: "service:01", Definition: DefinitionRef{Pack: PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}, ID: "helianthus.evse.current_limit", Version: "1.0.0"}, BindingID: "binding:01", SourceEpochID: "epoch:01", DriverGeneration: "1", Qualification: QualificationQualified, Availability: AvailabilityAvailable, Constraints: []TypedField{}, ActivationEvidence: []EvidenceRef{}, Revision: "1"}
		requireID(t, c.Validate(), CapabilityNotQualified)
	})
	t.Run("K-NEG-043", func(t *testing.T) {
		p := PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}
		d := DefinitionRef{Pack: p, ID: "helianthus.evse.set_current", Version: "1.0.0"}
		_, err := NewRegistry(testValidator{pack: p, index: DefinitionIndex{Pack: p, Fields: []DefinitionRef{}, Services: []DefinitionRef{}, Capabilities: []DefinitionRef{}, Operations: []DefinitionRef{d}, EffectRules: []DefinitionRef{}}}, testValidator{pack: PackRef{ID: "helianthus.pack.other", Version: "1.0.0"}, index: DefinitionIndex{Pack: PackRef{ID: "helianthus.pack.other", Version: "1.0.0"}, Fields: []DefinitionRef{}, Services: []DefinitionRef{}, Capabilities: []DefinitionRef{}, Operations: []DefinitionRef{{Pack: PackRef{ID: "helianthus.pack.other", Version: "1.0.0"}, ID: d.ID, Version: d.Version}}, EffectRules: []DefinitionRef{}}})
		requireID(t, err, DefinitionOwnerConflict)
	})
	t.Run("canonical_bytes_are_stable", func(t *testing.T) {
		d := Decimal{Coefficient: "23", Exponent10: 1}
		a, err := CanonicalJSON(d)
		if err != nil {
			t.Fatal(err)
		}
		b, err := CanonicalJSON(d)
		if err != nil || !bytes.Equal(a, b) {
			t.Fatalf("canonical bytes changed: %q %q", a, b)
		}
		if string(a) != "{\"coefficient\":\"23\",\"exponent10\":1}" {
			t.Fatalf("unexpected canonical bytes: %s", a)
		}
	})
}

func requireID(t *testing.T, err error, want ErrorID) {
	t.Helper()
	if err == nil || ErrorIdentifier(err) != want {
		t.Fatalf("got %v (%s), want %s", err, ErrorIdentifier(err), want)
	}
}
func pointer(s string) *string { return &s }
func validPolicy() FreshnessPolicy {
	return FreshnessPolicy{PolicyID: "policy:telemetry", Version: "1.0.0", FreshForNS: "30", RetainForNS: "300", MaxWallUncertaintyNS: "1"}
}
func validTimes() Times {
	return Times{ReceivedAt: TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "1"}, ReceiptMonotonic: MonotonicPoint{ClockEpochID: "epoch:1", Nanoseconds: "5"}, EvaluatedAt: TimePoint{UnixNanoseconds: "101", ClockID: "clock.utc", UncertaintyNS: "1"}, EvaluateMonotonic: MonotonicPoint{ClockEpochID: "epoch:1", Nanoseconds: "6"}}
}
func validQuality() Quality {
	return Quality{Assertion: AssertionObserved, Qualification: QualificationQualified, Promotion: PromotionPromoted, Validity: ValidityGood, Availability: AvailabilityAvailable, Freshness: FreshnessFresh, Reasons: []DefinitionID{}}
}

type testValidator struct {
	pack  PackRef
	index DefinitionIndex
}

func (v testValidator) Pack() PackRef                                         { return v.pack }
func (v testValidator) Definitions() DefinitionIndex                          { return v.index }
func (testValidator) ValidateFact(FactKey, *Value) error                      { return nil }
func (testValidator) ValidateService(ServiceInstance) error                   { return nil }
func (testValidator) ValidateCapability(CapabilityInstance) error             { return nil }
func (testValidator) ValidateField(DefinitionRef, TypedField) error           { return nil }
func (testValidator) MatchConstraints(CapabilityInstance, []TypedField) error { return nil }
func (testValidator) EvaluatePredicate(FactCandidate, PredicateOp, Value) (bool, error) {
	return true, nil
}

func publicEvidence(hex string) EvidenceRef {
	return EvidenceRef{Owner: "helianthus.modbusreg", Kind: "profile.observation", Digest: Digest("sha256:" + repeat(hex)), Contract: "helianthus.modbus.profile.v1", Access: EvidenceAccessPublic, Redaction: RedactionNone}
}

func repeat(s string) string {
	result := ""
	for len(result) < 64 {
		result += s
	}
	return result[:64]
}
