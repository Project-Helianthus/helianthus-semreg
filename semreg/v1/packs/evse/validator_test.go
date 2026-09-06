package evse

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestEVSEDefinitionAndFields(t *testing.T) {
	v := New()
	if _, ok := v.(operation.OperationPackValidator); !ok {
		t.Fatal("operation hook")
	}
	index := v.Definitions()
	if err := index.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(index.Fields) != 15 || len(index.Services) != 5 || len(index.Capabilities) != 6 || len(index.Operations) != 1 || len(index.EffectRules) != 1 {
		t.Fatalf("definition index: %+v", index)
	}
	for _, spec := range fields {
		t.Run(string(spec.id), func(t *testing.T) {
			key, current := testKey(spec), testValue(spec)
			if err := v.ValidateFact(key, &current); err != nil {
				t.Fatal(err)
			}
			if spec.kind != semreg.ValueQuantity {
				bad := current
				bad.Symbol = &semreg.Symbol{Namespace: spec.id, Token: "unknown", Known: true}
				require(t, v.ValidateFact(key, &bad), semreg.InvalidValue)
				return
			}
			for _, tc := range []struct {
				name  string
				value semreg.Decimal
				want  semreg.ErrorID
			}{
				{"minimum", spec.minimum.decimal(), ""}, {"maximum", spec.maximum.decimal(), ""},
				{"below minimum", below(spec.minimum.decimal()), semreg.BoundsExceeded}, {"above maximum", above(spec.maximum.decimal()), semreg.BoundsExceeded},
			} {
				t.Run(tc.name, func(t *testing.T) {
					candidate := current
					candidate.Quantity = &semreg.Quantity{Number: tc.value, Unit: spec.unit}
					if tc.want == "" {
						if err := v.ValidateFact(key, &candidate); err != nil {
							t.Fatal(err)
						}
					} else {
						require(t, v.ValidateFact(key, &candidate), tc.want)
					}
				})
			}
		})
	}
}

func TestEVSECapabilityConstraintsAndRace(t *testing.T) {
	v := New().(validator)
	cap := testCapability("evse.capability.set_allocated_current")
	if err := v.ValidateCapability(cap); err != nil {
		t.Fatal(err)
	}
	invalid := cap
	invalid.Constraints = append([]semreg.TypedField(nil), cap.Constraints...)
	invalid.Constraints[0].Value.Quantity = &semreg.Quantity{Number: above(must("evse.limit.allocated_current").maximum.decimal()), Unit: "unit.ampere"}
	require(t, v.ValidateCapability(invalid), semreg.BoundsExceeded)
	arguments := append([]semreg.TypedField(nil), cap.Constraints...)
	arguments[0].Value.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.ampere"}
	require(t, v.MatchConstraints(cap, arguments), semreg.InvalidValue)

	want := v.Definitions()
	key, value := testKey(fields[0]), testValue(fields[0])
	errs := make(chan error, 16)
	var group sync.WaitGroup
	for n := 0; n < 16; n++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < 64; i++ {
				if err := v.ValidateFact(key, &value); err != nil {
					errs <- err
					return
				}
				if !reflect.DeepEqual(want, v.Definitions()) {
					errs <- fmt.Errorf("non-deterministic definitions")
					return
				}
			}
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func testKey(spec fieldSpec) semreg.FactKey {
	name := "id"
	return semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: spec.id, Dimensions: []semreg.Dimension{{ID: spec.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}}
}
func testValue(spec fieldSpec) semreg.Value {
	if spec.kind == semreg.ValueQuantity {
		return semreg.Value{Kind: spec.kind, Quantity: &semreg.Quantity{Number: spec.minimum.decimal(), Unit: spec.unit}}
	}
	return semreg.Value{Kind: spec.kind, Symbol: &semreg.Symbol{Namespace: spec.id, Token: strings.TrimPrefix(spec.symbols[0], string(spec.id)+"."), Known: true}}
}
func must(id semreg.DefinitionID) fieldSpec {
	spec, ok := findField(id)
	if !ok {
		panic(id)
	}
	return spec
}
func testCapability(id semreg.DefinitionID) semreg.CapabilityInstance {
	spec := capabilities[id]
	constraints := make([]semreg.TypedField, len(spec.constraints))
	for i, field := range spec.constraints {
		constraints[i] = semreg.TypedField{ID: field, Value: testValue(must(field))}
	}
	return semreg.CapabilityInstance{InstanceID: "cap:1", AssetID: "asset:1", ServiceInstance: "service:1", Definition: definition(id), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Constraints: constraints, ActivationEvidence: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}
}
func testEvidence() semreg.EvidenceRef {
	return semreg.EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Contract: "test.evidence/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone}
}
func above(value semreg.Decimal) semreg.Decimal {
	var n int64
	fmt.Sscan(value.Coefficient, &n)
	return semreg.Decimal{Coefficient: fmt.Sprint(n + 1), Exponent10: value.Exponent10}
}
func below(value semreg.Decimal) semreg.Decimal {
	var n int64
	fmt.Sscan(value.Coefficient, &n)
	return semreg.Decimal{Coefficient: fmt.Sprint(n - 1), Exponent10: value.Exponent10}
}
func require(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if got := semreg.ErrorIdentifier(err); got != want {
		t.Fatalf("error %v, want %s", err, want)
	}
}
