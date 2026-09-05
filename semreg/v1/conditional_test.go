package semreg

import (
	"encoding/json"
	"fmt"
	"testing"
)

func conditionalPtr[T any](v T) *T { return &v }

func checkConditionalForms[T Record](t *testing.T, v T, want ErrorID) {
	t.Helper()
	checkMatrixError(t, v.Validate(), want)
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]byte{raw, reverseMembers(t, raw)} {
		_, err := Decode[T](input)
		checkMatrixError(t, err, want)
	}
}

func TestFactCandidatePresentMembers(t *testing.T) {
	for _, mutation := range []string{"derivation-duplicate", "derivation-algorithm", "binding", "epoch", "generation"} {
		t.Run(mutation, func(t *testing.T) {
			c := validInferredCandidate()
			want := InvalidIdentifier
			switch mutation {
			case "derivation-duplicate":
				c = validCandidate("a", true)
				c.Derivation = validInferredCandidate().Derivation
				e := validOrigin("a").Evidence[0]
				c.Derivation.Evidence = []EvidenceRef{e, e}
				want = DuplicateKey
			case "derivation-algorithm":
				c = validCandidate("a", true)
				c.Derivation = validInferredCandidate().Derivation
				c.Derivation.Algorithm = "!"
			case "binding":
				c.BindingID = conditionalPtr(NativeBindingID("!"))
			case "epoch":
				c.SourceEpochID = conditionalPtr(SourceEpochID("!"))
			case "generation":
				c.DriverGeneration = conditionalPtr(Uint64("bad"))
			}
			checkConditionalForms(t, c, want)
		})
	}
}

func TestFactCandidateSelectionMatrix(t *testing.T) {
	for _, assertion := range []AssertionKind{AssertionObserved, AssertionInferred, "invalid"} {
		for _, derivation := range []string{"absent", "valid", "bad-id", "duplicate"} {
			for _, path := range []string{"absent", "valid", "bad-binding", "bad-epoch", "bad-generation"} {
				t.Run(fmt.Sprintf("%s/%s/%s", assertion, derivation, path), func(t *testing.T) {
					c := validCandidate("a", true)
					c.Quality.Assertion = assertion
					c.Origin = OriginRef{OriginID: "origin:a", Kind: OriginOperator, Evidence: validOrigin("a").Evidence}
					if path == "absent" {
						c.BindingID = nil
						c.SourceEpochID = nil
						c.DriverGeneration = nil
					}
					if path == "bad-binding" {
						c.BindingID = conditionalPtr(NativeBindingID("!"))
					}
					if path == "bad-epoch" {
						c.SourceEpochID = conditionalPtr(SourceEpochID("!"))
					}
					if path == "bad-generation" {
						c.DriverGeneration = conditionalPtr(Uint64("bad"))
					}
					if derivation != "absent" {
						c.Derivation = validInferredCandidate().Derivation
					}
					if derivation == "bad-id" {
						c.Derivation.Algorithm = "!"
					}
					if derivation == "duplicate" {
						e := validOrigin("a").Evidence[0]
						c.Derivation.Evidence = []EvidenceRef{e, e}
					}
					want := ErrorID("")
					if assertion == "invalid" {
						want = InvalidEnum
					}
					if assertion == AssertionObserved && derivation != "absent" || assertion == AssertionInferred && path != "absent" {
						want = InvalidValue
					}
					if derivation == "bad-id" || path == "bad-binding" || path == "bad-epoch" || path == "bad-generation" {
						want = InvalidIdentifier
					}
					if assertion == AssertionObserved && path == "absent" || assertion == AssertionInferred && derivation == "absent" {
						want = MissingMember
					}
					if derivation == "duplicate" {
						want = DuplicateKey
					}
					checkConditionalForms(t, c, want)
				})
			}
		}
	}
}

func TestFactCandidateOriginMatrix(t *testing.T) {
	for _, kind := range []OriginKind{OriginNativeObservation, OriginDerived, OriginOperator, OriginAutomation, OriginProjection} {
		for _, present := range []bool{false, true} {
			for _, badID := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%t/%t", kind, present, badID), func(t *testing.T) {
					c := validCandidate("a", true)
					if kind != OriginNativeObservation {
						c.Origin = OriginRef{OriginID: "origin:a", Kind: kind, Evidence: validOrigin("a").Evidence}
					}
					switch kind {
					case OriginNativeObservation:
						if !present {
							c.BindingID = nil
						}
					case OriginDerived:
						c.Quality.Assertion = AssertionInferred
						c.BindingID = nil
						c.SourceEpochID = nil
						c.DriverGeneration = nil
						if present {
							c.Derivation = validInferredCandidate().Derivation
						}
					case OriginProjection:
						if present {
							c.Causal = conditionalPtr(validCausal())
						}
					default:
						if !present {
							c.Value = nil
						}
					}
					want := ErrorID("")
					if badID {
						c.Origin.OriginID = "!"
						want = InvalidIdentifier
					}
					if !present {
						want = MissingMember
					}
					checkConditionalForms(t, c, want)
				})
			}
		}
	}
}

// Each inventory row covers the valid control, either defect alone, and their
// combination. Expected combined classes are explicit contract expectations;
// this test does not call the production ranking or collection helpers.
func conditionalPair[T Record](t *testing.T, name string, base func() T, a func(*T), aID ErrorID, b func(*T), bID, combined ErrorID) {
	t.Helper()
	for _, first := range []bool{false, true} {
		for _, second := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t/%t", name, first, second), func(t *testing.T) {
				v := base()
				want := ErrorID("")
				if first {
					a(&v)
					want = aID
				}
				if second {
					b(&v)
					want = bID
				}
				if first && second {
					want = combined
				}
				checkConditionalForms(t, v, want)
			})
		}
	}
}

func TestFoundationConditionalInventory(t *testing.T) {
	evidence := func() EvidenceRef { return validOrigin("a").Evidence[0] }
	boolean := func() Value { return Value{Kind: ValueBoolean, Boolean: boolPtr(true)} }
	quantity := func() Quantity { return Quantity{Number: Decimal{Coefficient: "1"}, Unit: "unit.volt"} }
	pack := func() PackRef { return PackRef{ID: "pack.test", Version: "1.0.0"} }
	ref := func() DefinitionRef { return DefinitionRef{Pack: pack(), ID: "field.a", Version: "1.0.0"} }
	conditionalPair(t, "VersionRange", func() VersionRange { return VersionRange{Minimum: "1.0.0", MaximumExclusive: "2.0.0"} }, func(v *VersionRange) { v.Minimum = "3.0.0" }, InvalidValue, func(v *VersionRange) { v.MaximumExclusive = "bad" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "EvidenceRef", evidence, func(v *EvidenceRef) { v.Access = EvidenceAccessRestricted }, InvalidEvidence, func(v *EvidenceRef) { v.Contract = "!" }, InvalidEvidence, InvalidEvidence)
	conditionalPair(t, "TimePoint", func() TimePoint { return validTimes().ReceivedAt }, func(v *TimePoint) { v.UnixNanoseconds = "bad" }, InvalidTime, func(v *TimePoint) { v.ClockID = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "MonotonicPoint", func() MonotonicPoint { return validTimes().ReceiptMonotonic }, func(v *MonotonicPoint) { v.Nanoseconds = "bad" }, InvalidTime, func(v *MonotonicPoint) { v.ClockEpochID = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "SourceDescriptor", validSource, func(v *SourceDescriptor) { v.State = "invalid" }, InvalidEnum, func(v *SourceDescriptor) { v.Revision = "0" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "OriginRef", func() OriginRef {
		return OriginRef{OriginID: "origin:a", Kind: OriginOperator, Evidence: []EvidenceRef{evidence()}}
	}, func(v *OriginRef) { v.BindingID = conditionalPtr(NativeBindingID("binding:a")) }, InvalidValue, func(v *OriginRef) { v.SourceEpochID = conditionalPtr(SourceEpochID("!")) }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "NativeBinding", validBinding, func(v *NativeBinding) { v.State = "invalid" }, InvalidEnum, func(v *NativeBinding) { v.DriverGeneration = "bad" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "IdentityLink", func() IdentityLink {
		return IdentityLink{AssetID: "asset:a", BindingID: "binding:a", State: LinkQualified, Basis: []EvidenceRef{evidence()}, Revision: "1"}
	}, func(v *IdentityLink) { v.State = "invalid" }, InvalidEnum, func(v *IdentityLink) { v.Basis = append(v.Basis, evidence()) }, DuplicateKey, DuplicateKey)
	conditionalPair(t, "SourcePathRef", func() SourcePathRef { return validPath("a", "1") }, func(v *SourcePathRef) { v.DriverGeneration = "0" }, InvalidIdentifier, func(v *SourcePathRef) { v.SourceID = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "DerivationInput", func() DerivationInput { return validInferredCandidate().Derivation.Inputs[0] }, func(v *DerivationInput) { v.SourcePaths = append(v.SourcePaths, v.SourcePaths[0]) }, DuplicateKey, func(v *DerivationInput) { v.CandidateRevision = "bad" }, InvalidIdentifier, DuplicateKey)
	conditionalPair(t, "Derivation", func() Derivation { return *validInferredCandidate().Derivation }, func(v *Derivation) { v.Inputs = append(v.Inputs, v.Inputs[len(v.Inputs)-1]) }, DerivationCycle, func(v *Derivation) { v.Algorithm = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "Decimal", func() Decimal { return Decimal{Coefficient: "1"} }, func(v *Decimal) { v.Exponent10 = 19 }, InvalidDecimal, func(v *Decimal) { v.Coefficient = "10" }, InvalidDecimal, InvalidDecimal)
	conditionalPair(t, "Symbol", func() Symbol { return Symbol{Namespace: "native.test", Token: "x"} }, func(v *Symbol) { v.Known = true }, InvalidValue, func(v *Symbol) { v.Namespace = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "Quantity", quantity, func(v *Quantity) { v.Number.Coefficient = "10" }, InvalidDecimal, func(v *Quantity) { v.Unit = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "Value", boolean, func(v *Value) { v.Quantity = conditionalPtr(quantity()) }, InvalidValue, func(v *Value) {
		v.Symbols = []Symbol{{Namespace: "native.test", Token: "x"}, {Namespace: "native.test", Token: "x"}}
	}, DuplicateKey, DuplicateKey)
	conditionalPair(t, "Dimension", func() Dimension { return Dimension{ID: "dimension.a", Value: boolean()} }, func(v *Dimension) { v.Value = Value{Kind: ValueTime, Time: conditionalPtr(validTimes().ReceivedAt)} }, InvalidValue, func(v *Dimension) { v.ID = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "FactKey", validKey, func(v *FactKey) {
		v.Dimensions = []Dimension{{ID: "dimension.b", Value: boolean()}, {ID: "dimension.a", Value: boolean()}}
	}, NoncanonicalOrder, func(v *FactKey) { v.PackID = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "Times", validTimes, func(v *Times) { v.EvaluateMonotonic = v.ReceiptMonotonic; v.EvaluateMonotonic.Nanoseconds = "0" }, InvalidTime, func(v *Times) {
		v.SourceAt = conditionalPtr(TimePoint{UnixNanoseconds: "0", ClockID: "!", UncertaintyNS: "0"})
	}, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "FreshnessPolicy", validPolicy, func(v *FreshnessPolicy) { v.RetainForNS = v.FreshForNS }, InvalidTime, func(v *FreshnessPolicy) { v.PolicyID = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "Quality", validQuality, func(v *Quality) { v.Qualification = QualificationCandidate }, InvalidValue, func(v *Quality) { v.Reasons = []DefinitionID{"reason.a", "reason.a"} }, DuplicateKey, DuplicateKey)
	conditionalPair(t, "CausalContext", validCausal, func(v *CausalContext) { v.MaxHops = 0 }, CausalBudgetExceeded, func(v *CausalContext) { v.FirstSeenAt.ClockID = "clock.other" }, InvalidTime, InvalidTime)
	conditionalPair(t, "Conflict", func() Conflict { return conflictingEnvelope(t).Conflicts[0] }, func(v *Conflict) { v.State = "invalid" }, InvalidEnum, func(v *Conflict) { v.Candidates = append(v.Candidates, v.Candidates[0]) }, DuplicateKey, DuplicateKey)
	conditionalPair(t, "FactEnvelope", func() FactEnvelope { return conflictingEnvelope(t) }, func(v *FactEnvelope) { v.Conflicts = []Conflict{} }, InvalidValue, func(v *FactEnvelope) { v.Candidates[0].Key.FactID = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "PackRef", pack, func(v *PackRef) { v.ID = "!" }, InvalidIdentifier, func(v *PackRef) { v.Version = "bad" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "DefinitionRef", ref, func(v *DefinitionRef) { v.Pack.ID = "!" }, InvalidIdentifier, func(v *DefinitionRef) { v.Version = "bad" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "DefinitionIndex", func() DefinitionIndex { return indexWith(pack(), DefinitionField, ref()) }, func(v *DefinitionIndex) { v.Fields[0].Pack.ID = "pack.other" }, DefinitionOwnerConflict, func(v *DefinitionIndex) { v.Fields[0].ID = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "TypedField", func() TypedField { return TypedField{ID: "field.a", Value: boolean()} }, func(v *TypedField) { v.Value.Kind = "invalid" }, InvalidEnum, func(v *TypedField) { v.ID = "!" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "ServiceInstance", validService, func(v *ServiceInstance) { v.Qualification = "invalid" }, InvalidEnum, func(v *ServiceInstance) { v.DriverGeneration = "bad" }, InvalidIdentifier, InvalidIdentifier)
	conditionalPair(t, "CapabilityInstance", validCapability, func(v *CapabilityInstance) { v.ActivationEvidence = []EvidenceRef{} }, CapabilityNotQualified, func(v *CapabilityInstance) { v.InstanceID = "!" }, InvalidIdentifier, InvalidIdentifier)
}
