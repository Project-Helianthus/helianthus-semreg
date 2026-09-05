package semreg

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var incompatibleStringTokens = []string{`23`, `{}`, `[]`, `false`}

func mutateWire(t *testing.T, raw []byte, path string, token string) []byte {
	t.Helper()
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(path, ".", 2)
	if len(parts) == 2 {
		members[parts[0]] = mutateWire(t, members[parts[0]], parts[1], token)
	} else if token == "" {
		delete(members, parts[0])
	} else {
		members[parts[0]] = json.RawMessage(token)
	}
	out, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func checkCandidateWireTargets(t *testing.T, raw []byte, want ErrorID) {
	t.Helper()
	for _, input := range [][]byte{raw, reverseMembers(t, raw)} {
		_, err := Decode[FactCandidate](input)
		checkMatrixError(t, err, want)
		_, err = Decode[*FactCandidate](input)
		checkMatrixError(t, err, want)
	}
	key, _ := json.Marshal(validKey())
	envelope := []byte(`{"asset_id":"asset:a","key":` + string(key) + `,"candidates":[` + string(raw) + `],"conflicts":[],"revision":"1"}`)
	for _, input := range [][]byte{envelope, reverseMembers(t, envelope)} {
		_, err := Decode[FactEnvelope](input)
		checkMatrixError(t, err, want)
		_, err = Decode[*FactEnvelope](input)
		checkMatrixError(t, err, want)
	}
}

func TestOptionalCandidateWireMatrix(t *testing.T) {
	for _, assertion := range []AssertionKind{AssertionObserved, AssertionInferred} {
		for _, field := range []string{"driver_generation", "binding_id", "source_epoch_id"} {
			values := []string{`"!"`}
			if field == "driver_generation" {
				values = []string{`"1"`, `"0"`, `"bad"`, `"18446744073709551616"`, `"!"`, `null`, ``}
			}
			for _, scalar := range values {
				for _, sibling := range []string{"revision", "quality.assertion", "key.fact_id"} {
					for _, token := range incompatibleStringTokens {
						t.Run(fmt.Sprintf("%s/%s/%s/%s/%s", assertion, field, scalar, sibling, token), func(t *testing.T) {
							c := validCandidate("a", true)
							if assertion == AssertionInferred {
								c = validInferredCandidate()
							}
							raw, _ := json.Marshal(c)
							raw = mutateWire(t, raw, field, scalar)
							want := InvalidIdentifier
							if scalar == `"1"` {
								want = InvalidValue
							}
							if scalar == `null` || scalar == "" && assertion == AssertionObserved {
								want = MissingMember
							}
							if scalar == "" && assertion == AssertionInferred {
								want = InvalidValue
							}
							checkCandidateWireTargets(t, mutateWire(t, raw, sibling, token), want)
						})
					}
				}
			}
		}
	}
}

// These rows cover every optional scalar pointer outside FactCandidate and
// every currently implemented enclosing scalar-domain override. Mutations are
// applied to JSON after marshaling, so no Go value can silently coerce tokens.
func wireScalarRow[T Record](t *testing.T, name string, base T, field, invalid, sibling string, domain, combined ErrorID) {
	t.Helper()
	raw, _ := json.Marshal(base)
	raw = mutateWire(t, raw, field, invalid)
	t.Run(name+"/control", func(t *testing.T) {
		for _, input := range [][]byte{raw, reverseMembers(t, raw)} {
			_, err := Decode[T](input)
			checkMatrixError(t, err, domain)
		}
	})
	for _, token := range incompatibleStringTokens {
		t.Run(name+"/"+token, func(t *testing.T) {
			b := mutateWire(t, raw, sibling, token)
			for _, input := range [][]byte{b, reverseMembers(t, b)} {
				_, err := Decode[T](input)
				checkMatrixError(t, err, combined)
			}
		})
	}
}

func TestContextualScalarWireInventory(t *testing.T) {
	for _, field := range []string{"source_id", "source_epoch_id", "binding_id"} {
		wireScalarRow(t, "OriginRef."+field, validOrigin("a"), field, `"!"`, "origin_id", InvalidIdentifier, InvalidIdentifier)
	}
	wireScalarRow(t, "CausalContext.parent_correlation_id", validCausal(), "parent_correlation_id", `"!"`, "correlation_id", InvalidIdentifier, InvalidIdentifier)
	wireScalarRow(t, "Value.text", Value{Kind: ValueText, Text: conditionalPtr("ok")}, "text", `"e\u0301"`, "kind", InvalidValue, InvalidValue)
	// bool has no invalid non-null scalar value: null tests pointer presence.
	wireScalarRow(t, "Value.boolean", Value{Kind: ValueBoolean, Boolean: boolPtr(true)}, "boolean", `null`, "kind", MissingMember, MissingMember)
	for _, field := range []string{"owner", "kind", "contract", "digest"} {
		wireScalarRow(t, "EvidenceRef."+field, validOrigin("a").Evidence[0], field, `"!"`, "access", InvalidEvidence, InvalidValue)
	}
	for _, field := range []string{"unix_nanoseconds", "uncertainty_ns"} {
		wireScalarRow(t, "TimePoint."+field, validTimes().ReceivedAt, field, `"bad"`, "clock_id", InvalidTime, InvalidValue)
	}
	wireScalarRow(t, "MonotonicPoint.nanoseconds", validTimes().ReceiptMonotonic, "nanoseconds", `"bad"`, "clock_epoch_id", InvalidTime, InvalidValue)
	for _, field := range []string{"fresh_for_ns", "retain_for_ns", "max_wall_uncertainty_ns"} {
		wireScalarRow(t, "FreshnessPolicy."+field, validPolicy(), field, `"bad"`, "policy_id", InvalidTime, InvalidValue)
	}
	// Decimal's other field is numeric, so Quantity.unit provides the unrelated
	// incompatible string token while coefficient/exponent preserve their domain.
	q := Quantity{Number: Decimal{Coefficient: "1"}, Unit: "unit.volt"}
	wireScalarRow(t, "Decimal.coefficient", q, "number.coefficient", `"10"`, "unit", InvalidDecimal, InvalidDecimal)
	wireScalarRow(t, "Decimal.exponent10", q, "number.exponent10", `19`, "unit", InvalidDecimal, InvalidDecimal)
	wireScalarRow(t, "Decimal.exponent10-partial", Decimal{Coefficient: "1"}, "exponent10", `19`, "coefficient", InvalidDecimal, InvalidDecimal)
	wireScalarRow(t, "SourceDescriptor.revision", validSource(), "revision", `"0"`, "source_id", InvalidIdentifier, InvalidIdentifier)
	for _, field := range []string{"driver_generation", "revision"} {
		wireScalarRow(t, "NativeBinding."+field, validBinding(), field, `"0"`, "binding_id", InvalidIdentifier, InvalidIdentifier)
	}
	link := IdentityLink{AssetID: "asset:a", BindingID: "binding:a", State: LinkQualified, Basis: validOrigin("a").Evidence, Revision: "1"}
	wireScalarRow(t, "IdentityLink.revision", link, "revision", `"0"`, "asset_id", InvalidIdentifier, InvalidIdentifier)
	wireScalarRow(t, "SourcePathRef.driver_generation", validPath("a", "1"), "driver_generation", `"0"`, "source_id", InvalidIdentifier, InvalidIdentifier)
	wireScalarRow(t, "DerivationInput.candidate_revision", validInferredCandidate().Derivation.Inputs[0], "candidate_revision", `"0"`, "candidate_id", InvalidIdentifier, InvalidIdentifier)
	for _, field := range []string{"driver_generation", "revision"} {
		wireScalarRow(t, "FactCandidate."+field, validCandidate("a", true), field, `"0"`, "candidate_id", InvalidIdentifier, InvalidIdentifier)
	}
	wireScalarRow(t, "FactEnvelope.revision", conflictingEnvelope(t), "revision", `"0"`, "asset_id", InvalidIdentifier, InvalidIdentifier)
	for _, field := range []string{"driver_generation", "revision"} {
		wireScalarRow(t, "ServiceInstance."+field, validService(), field, `"0"`, "instance_id", InvalidIdentifier, InvalidIdentifier)
		wireScalarRow(t, "CapabilityInstance."+field, validCapability(), field, `"0"`, "instance_id", InvalidIdentifier, InvalidIdentifier)
	}
	for _, field := range []string{"hop_count", "max_hops"} {
		wireScalarRow(t, "CausalContext."+field, validCausal(), field, `65536`, "correlation_id", CausalBudgetExceeded, InvalidValue)
	}
}
