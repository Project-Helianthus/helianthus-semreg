package thermal

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestDefinitionIndexRegistryAndImmutability(t *testing.T) {
	validator := New()
	if _, ok := validator.(operation.OperationPackValidator); !ok {
		t.Fatal("thermal validator must own operation hook")
	}
	first := validator.Definitions()
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
	if first.Pack != pack || len(first.Fields) != 13 || len(first.Services) != 5 || len(first.Capabilities) != 7 || len(first.Operations) != 2 || len(first.EffectRules) != 2 {
		t.Fatalf("index: %+v", first)
	}
	first.Fields[0].ID = "thermal.mutated.field"
	if next := validator.Definitions().Fields[0].ID; next != "thermal.action.state" {
		t.Fatalf("definition mutation leaked: %s", next)
	}
	registry, err := semreg.NewRegistry(validator)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Definition(semreg.DefinitionOperation, definition("thermal.operation.set_mode")); err != nil {
		t.Fatal(err)
	}
	if _, err := operation.NewKernel(validator); err != nil {
		t.Fatalf("operation kernel registration: %v", err)
	}
}

func TestAllFieldContracts(t *testing.T) {
	validator := New()
	for _, spec := range fields {
		t.Run(string(spec.id), func(t *testing.T) {
			key, value := validKey(spec), validValue(spec)
			if err := validator.ValidateFact(key, &value); err != nil {
				t.Fatalf("valid fact: %v", err)
			}
			if err := validator.ValidateField(definition(spec.id), semreg.TypedField{ID: spec.id, Value: value}); err != nil {
				t.Fatalf("valid field: %v", err)
			}
			wrong := key
			wrong.Dimensions = append([]semreg.Dimension(nil), key.Dimensions...)
			wrong.Dimensions[0].ID = "thermal.dimension.system"
			if spec.dimension == "thermal.dimension.system" {
				wrong.Dimensions[0].ID = "thermal.dimension.zone"
			}
			requireID(t, validator.ValidateFact(wrong, &value), semreg.InvalidValue)
			if spec.kind == semreg.ValueQuantity {
				minimum := value
				minimum.Quantity = &semreg.Quantity{Number: spec.minimum.decimal(), Unit: spec.unit}
				if err := validator.ValidateFact(key, &minimum); err != nil {
					t.Fatalf("exact minimum: %v", err)
				}
				maximum := value
				maximum.Quantity = &semreg.Quantity{Number: spec.maximum.decimal(), Unit: spec.unit}
				if err := validator.ValidateFact(key, &maximum); err != nil {
					t.Fatalf("exact maximum: %v", err)
				}
				bad := value
				unit := semreg.DefinitionID("unit.watt")
				if unit == spec.unit {
					unit = "unit.celsius"
				}
				bad.Quantity = &semreg.Quantity{Number: value.Quantity.Number, Unit: unit}
				requireID(t, validator.ValidateFact(key, &bad), semreg.InvalidValue)
				outside := value
				outside.Quantity = &semreg.Quantity{Number: above(spec.maximum.decimal()), Unit: spec.unit}
				requireID(t, validator.ValidateFact(key, &outside), semreg.BoundsExceeded)
				belowMinimum := value
				belowMinimum.Quantity = &semreg.Quantity{Number: below(spec.minimum.decimal()), Unit: spec.unit}
				requireID(t, validator.ValidateFact(key, &belowMinimum), semreg.BoundsExceeded)
			} else {
				bad := value
				bad.Symbol = &semreg.Symbol{Namespace: spec.id, Token: "outcome", Known: true}
				requireID(t, validator.ValidateFact(key, &bad), semreg.InvalidValue)
			}
		})
	}
	key, value := validKey(fields[0]), validValue(fields[0])
	key.PackID = "helianthus.pack.evse"
	requireID(t, validator.ValidateFact(key, &value), semreg.DefinitionOwnerMissing)
}

func TestServicesCapabilitiesAndConcurrentAccess(t *testing.T) {
	validator := New()
	for id, spec := range capabilities {
		if _, ok := services[spec.service]; !ok {
			t.Fatalf("%s has unknown service", id)
		}
		capability := validCapability(id)
		if err := validator.ValidateCapability(capability); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if err := validator.MatchConstraints(capability, copyFields(capability.Constraints)); err != nil {
			t.Fatalf("constraints %s: %v", id, err)
		}
		capability.Qualification = semreg.QualificationCandidate
		requireID(t, validator.ValidateCapability(capability), semreg.CapabilityNotQualified)
	}
	t.Run("stored operation constraint validates full value", func(t *testing.T) {
		capability := validCapability("thermal.capability.set_temperature")
		capability.Constraints[0].Value.Quantity.Number = above(mustField("thermal.setpoint.temperature").maximum.decimal())
		requireID(t, validator.ValidateCapability(capability), semreg.BoundsExceeded)
	})
	t.Run("operation argument must admit published constraint", func(t *testing.T) {
		capability := validCapability("thermal.capability.set_temperature")
		argument := copyFields(capability.Constraints)
		argument[0].Value.Quantity.Number = semreg.Decimal{Coefficient: "0"}
		requireID(t, validator.MatchConstraints(capability, argument), semreg.InvalidValue)
	})
	var group sync.WaitGroup
	failures := make(chan error, 64)
	key, value := validKey(fields[0]), validValue(fields[0])
	want := validator.Definitions()
	for worker := 0; worker < 24; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for n := 0; n < 64; n++ {
				if err := validator.ValidateFact(key, &value); err != nil {
					failures <- err
					return
				}
				if !reflect.DeepEqual(want, validator.Definitions()) {
					failures <- fmt.Errorf("non-deterministic index")
					return
				}
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func validKey(spec fieldSpec) semreg.FactKey {
	name := "identity"
	return semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: spec.id, Dimensions: []semreg.Dimension{{ID: spec.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}}
}
func validValue(spec fieldSpec) semreg.Value {
	if spec.kind == semreg.ValueQuantity {
		return semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: spec.minimum.decimal(), Unit: spec.unit}}
	}
	token := strings.TrimPrefix(spec.symbols[0], string(spec.id)+".")
	return semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: spec.id, Token: token, Known: true}}
}
func above(value semreg.Decimal) semreg.Decimal {
	var coefficient int64
	if _, err := fmt.Sscan(value.Coefficient, &coefficient); err != nil {
		panic(err)
	}
	return semreg.Decimal{Coefficient: fmt.Sprintf("%d", coefficient+1), Exponent10: value.Exponent10}
}
func below(value semreg.Decimal) semreg.Decimal {
	var coefficient int64
	if _, err := fmt.Sscan(value.Coefficient, &coefficient); err != nil {
		panic(err)
	}
	return semreg.Decimal{Coefficient: fmt.Sprintf("%d", coefficient-1), Exponent10: value.Exponent10}
}
func validCapability(id semreg.DefinitionID) semreg.CapabilityInstance {
	spec := capabilities[id]
	constraints := make([]semreg.TypedField, len(spec.constraints))
	for i, field := range spec.constraints {
		detail, _ := findField(field)
		constraints[i] = semreg.TypedField{ID: field, Value: validValue(detail)}
	}
	return semreg.CapabilityInstance{InstanceID: "capability:1", AssetID: "asset:1", ServiceInstance: "service:1", Definition: definition(id), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Constraints: constraints, ActivationEvidence: []semreg.EvidenceRef{evidence()}, Revision: "1"}
}
func evidence() semreg.EvidenceRef {
	return semreg.EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Contract: "test.evidence/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone}
}
func copyFields(fields []semreg.TypedField) []semreg.TypedField {
	return append([]semreg.TypedField{}, fields...)
}
func requireID(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if actual := semreg.ErrorIdentifier(err); actual != want {
		t.Fatalf("error = %q, want %q (%v)", actual, want, err)
	}
}
