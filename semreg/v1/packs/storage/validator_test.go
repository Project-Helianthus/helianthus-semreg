package storage

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestStorageDefinitionAndFields(t *testing.T) {
	v := New()
	if _, ok := v.(operation.OperationPackValidator); !ok {
		t.Fatal("operation hook")
	}
	index := v.Definitions()
	if err := index.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(index.Fields) != 20 || len(index.Services) != 7 || len(index.Capabilities) != 5 || len(index.Operations) != 2 || len(index.EffectRules) != 2 {
		t.Fatalf("definition index: %+v", index)
	}
	for _, spec := range fields {
		t.Run(string(spec.id), func(t *testing.T) {
			key, current := testKey(spec), testValue(spec)
			if err := v.ValidateFact(key, &current); err != nil {
				t.Fatal(err)
			}
			if spec.kind == semreg.ValueSymbol {
				bad := current
				bad.Symbol = &semreg.Symbol{Namespace: spec.id, Token: "unknown", Known: true}
				require(t, v.ValidateFact(key, &bad), semreg.InvalidValue)
				return
			}
			if spec.minimum != nil {
				bad := current
				bad.Quantity = &semreg.Quantity{Number: below(spec.minimum.decimal()), Unit: spec.unit}
				require(t, v.ValidateFact(key, &bad), semreg.BoundsExceeded)
			}
			if spec.maximum != nil {
				bad := current
				bad.Quantity = &semreg.Quantity{Number: above(spec.maximum.decimal()), Unit: spec.unit}
				require(t, v.ValidateFact(key, &bad), semreg.BoundsExceeded)
			}
		})
	}
}
func TestStorageCapabilitiesAndRace(t *testing.T) {
	v := New().(validator)
	for _, id := range []semreg.DefinitionID{"storage.capability.read.pack", "storage.capability.read.cell", "storage.capability.read.interface", "storage.capability.set_charge_limit", "storage.capability.set_discharge_limit"} {
		if err := v.ValidateCapability(testCapability(id)); err != nil {
			t.Fatal(id, err)
		}
	}
	cap := testCapability("storage.capability.set_charge_limit")
	bad := cap
	bad.Constraints[0].Value.Quantity.Number = above(must("storage.limit.charge_power").minimum.decimal())
	require(t, v.MatchConstraints(cap, bad.Constraints), semreg.InvalidValue)
	want := v.Definitions()
	key, value := testKey(fields[0]), testValue(fields[0])
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
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
	wg.Wait()
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
		n := semreg.Decimal{Coefficient: "0", Exponent10: 0}
		if spec.minimum != nil {
			n = spec.minimum.decimal()
		}
		return semreg.Value{Kind: spec.kind, Quantity: &semreg.Quantity{Number: n, Unit: spec.unit}}
	}
	return semreg.Value{Kind: spec.kind, Symbol: &semreg.Symbol{Namespace: spec.id, Token: strings.TrimPrefix(spec.symbols[0], string(spec.id)+"."), Known: true}}
}
func must(id semreg.DefinitionID) fieldSpec {
	s, ok := findField(id)
	if !ok {
		panic(id)
	}
	return s
}
func testCapability(id semreg.DefinitionID) semreg.CapabilityInstance {
	s := capabilities[id]
	cs := make([]semreg.TypedField, len(s.constraints))
	for i, f := range s.constraints {
		cs[i] = semreg.TypedField{ID: f, Value: testValue(must(f))}
	}
	return semreg.CapabilityInstance{InstanceID: "cap:1", AssetID: "asset:1", ServiceInstance: "service:1", Definition: definition(id), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Constraints: cs, ActivationEvidence: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}
}
func testEvidence() semreg.EvidenceRef {
	return semreg.EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Contract: "test.evidence/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone}
}
func above(v semreg.Decimal) semreg.Decimal {
	var n int64
	fmt.Sscan(v.Coefficient, &n)
	return semreg.Decimal{Coefficient: fmt.Sprint(n + 1), Exponent10: v.Exponent10}
}
func below(v semreg.Decimal) semreg.Decimal {
	var n int64
	fmt.Sscan(v.Coefficient, &n)
	return semreg.Decimal{Coefficient: fmt.Sprint(n - 1), Exponent10: v.Exponent10}
}
func require(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if got := semreg.ErrorIdentifier(err); got != want {
		t.Fatalf("error %v, want %s", err, want)
	}
}
