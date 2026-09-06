package infrastructure

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestDefinitionIndexIsExactAndReadOnly(t *testing.T) {
	validator := New()
	first := validator.Definitions()
	if first.Pack != pack || len(first.Fields) != 19 || len(first.Services) != 6 || len(first.Capabilities) != 6 || len(first.Operations) != 0 || len(first.EffectRules) != 0 {
		t.Fatalf("unexpected definition index: %+v", first)
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
	first.Fields[0].ID = "infrastructure.mutated.field"
	second := validator.Definitions()
	if second.Fields[0].ID != "infrastructure.ac.active_power" {
		t.Fatalf("definitions leaked caller mutation: %+v", second.Fields[0])
	}
	if _, ok := validator.(operation.OperationPackValidator); ok {
		t.Fatal("read-only infrastructure validator must not publish an operation hook")
	}
	if _, err := operation.NewKernel(validator); err != nil {
		t.Fatalf("read-only pack must register without operation routes: %v", err)
	}
	registry, err := semreg.NewRegistry(validator)
	if err != nil {
		t.Fatal(err)
	}
	fromRegistry, err := registry.Validator(pack)
	if err != nil {
		t.Fatal(err)
	}
	registryIndex := fromRegistry.Definitions()
	registryIndex.Fields[0].ID = "infrastructure.registry.mutation"
	if current := fromRegistry.Definitions().Fields[0].ID; current != "infrastructure.ac.active_power" {
		t.Fatalf("registry leaked index mutation: %s", current)
	}
}

func TestFieldsAcceptTheirExactContract(t *testing.T) {
	validator := New()
	for _, spec := range fields {
		t.Run(string(spec.id), func(t *testing.T) {
			value := validFieldValue(spec)
			key := validKey(spec)
			if err := validator.ValidateFact(key, &value); err != nil {
				t.Fatalf("valid fact: %v", err)
			}
			if err := validator.ValidateField(definition(spec.id), semreg.TypedField{ID: spec.id, Value: value}); err != nil {
				t.Fatalf("valid typed field: %v", err)
			}
			badDimension := key
			badDimension.Dimensions = append([]semreg.Dimension(nil), key.Dimensions...)
			badDimension.Dimensions[0].ID = "infrastructure.dimension.site"
			requireErrorID(t, validator.ValidateFact(badDimension, &value), semreg.InvalidValue)
			if spec.kind == semreg.ValueQuantity {
				wrongUnit := value
				wrongUnit.Quantity = &semreg.Quantity{Number: value.Quantity.Number, Unit: "unit.ampere"}
				if spec.unit == "unit.ampere" {
					wrongUnit.Quantity.Unit = "unit.volt"
				}
				requireErrorID(t, validator.ValidateFact(key, &wrongUnit), semreg.InvalidValue)
				outside := value
				outside.Quantity = &semreg.Quantity{Number: beyond(spec.maximum.decimal()), Unit: spec.unit}
				requireErrorID(t, validator.ValidateFact(key, &outside), semreg.BoundsExceeded)
			} else {
				wrongSymbol := value
				wrongSymbol.Symbol = &semreg.Symbol{Namespace: spec.id, Token: "unknown", Known: true}
				requireErrorID(t, validator.ValidateFact(key, &wrongSymbol), semreg.InvalidValue)
			}
		})
	}
	wrongPack := validKey(fields[0])
	wrongPack.PackID = "helianthus.pack.evse"
	value := validFieldValue(fields[0])
	requireErrorID(t, validator.ValidateFact(wrongPack, &value), semreg.DefinitionOwnerMissing)
	unknown := validKey(fields[0])
	unknown.FactID = "infrastructure.unknown.field"
	requireErrorID(t, validator.ValidateFact(unknown, &value), semreg.DefinitionOwnerMissing)
}

func TestServicesAndCapabilitiesRequireDeclaredReadOnlyDefinitions(t *testing.T) {
	validator := New()
	for capabilityID, serviceID := range capabilities {
		t.Run(string(capabilityID), func(t *testing.T) {
			if _, ok := services[serviceID]; !ok {
				t.Fatalf("capability %s refers to undeclared service %s", capabilityID, serviceID)
			}
			service := validService(serviceID)
			if err := validator.ValidateService(service); err != nil {
				t.Fatalf("valid service: %v", err)
			}
			capability := validCapability(capabilityID)
			if err := validator.ValidateCapability(capability); err != nil {
				t.Fatalf("valid capability: %v", err)
			}
			if err := validator.MatchConstraints(capability, []semreg.TypedField{}); err != nil {
				t.Fatalf("empty read constraints: %v", err)
			}
			unqualified := capability
			unqualified.Qualification = semreg.QualificationCandidate
			requireErrorID(t, validator.ValidateCapability(unqualified), semreg.CapabilityNotQualified)
			unavailable := capability
			unavailable.Availability = semreg.AvailabilityUnavailable
			requireErrorID(t, validator.ValidateCapability(unavailable), semreg.CapabilityUnavailable)
			capability.Constraints = []semreg.TypedField{{ID: fields[0].id, Value: validFieldValue(fields[0])}}
			requireErrorID(t, validator.ValidateCapability(capability), semreg.InvalidValue)
		})
	}
	bad := validService("infrastructure.service.site")
	bad.Definition.ID = "infrastructure.service.unknown"
	requireErrorID(t, validator.ValidateService(bad), semreg.DefinitionOwnerMissing)
}

func TestPredicateIsPureAndQuantityAware(t *testing.T) {
	validator := New()
	spec := fields[0]
	value := validFieldValue(spec)
	value.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: spec.unit}
	candidate := validCandidate(spec, value)
	equal, err := validator.EvaluatePredicate(candidate, semreg.PredicateEqual, value)
	if err != nil || !equal {
		t.Fatalf("equal predicate = %t, %v", equal, err)
	}
	greater := value
	greater.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: value.Quantity.Unit}
	less, err := validator.EvaluatePredicate(candidate, semreg.PredicateLess, greater)
	if err != nil || !less {
		t.Fatalf("less predicate = %t, %v", less, err)
	}
	status := validFieldValue(fields[14])
	statusCandidate := validCandidate(fields[14], status)
	_, err = validator.EvaluatePredicate(statusCandidate, semreg.PredicateLess, status)
	requireErrorID(t, err, semreg.InvalidValue)
}

func TestConcurrentValidationIsDeterministic(t *testing.T) {
	validator := New()
	spec := fields[0]
	key, value := validKey(spec), validFieldValue(spec)
	want := validator.Definitions()
	var group sync.WaitGroup
	errors := make(chan error, 128)
	for worker := 0; worker < 32; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for iteration := 0; iteration < 64; iteration++ {
				if err := validator.ValidateFact(key, &value); err != nil {
					errors <- err
					return
				}
				if actual := validator.Definitions(); !reflect.DeepEqual(actual, want) {
					errors <- fmt.Errorf("non-deterministic definitions")
					return
				}
			}
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func validKey(spec fieldSpec) semreg.FactKey {
	name := "identity"
	return semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: spec.id, Dimensions: []semreg.Dimension{{ID: spec.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}}
}

func validFieldValue(spec fieldSpec) semreg.Value {
	if spec.kind == semreg.ValueQuantity {
		return semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: spec.minimum.decimal(), Unit: spec.unit}}
	}
	name := strings.TrimPrefix(spec.symbols[0], string(spec.id)+".")
	return semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: spec.id, Token: name, Known: true}}
}

func beyond(value semreg.Decimal) semreg.Decimal {
	if value.Exponent10 == 0 {
		return semreg.Decimal{Coefficient: fmt.Sprintf("%d", decimalInteger(value)+1)}
	}
	return semreg.Decimal{Coefficient: fmt.Sprintf("%d", decimalInteger(value)+1), Exponent10: value.Exponent10}
}

func decimalInteger(value semreg.Decimal) int64 {
	var integer int64
	if _, err := fmt.Sscan(value.Coefficient, &integer); err != nil {
		panic(err)
	}
	return integer
}

func validService(id semreg.DefinitionID) semreg.ServiceInstance {
	return semreg.ServiceInstance{InstanceID: "service:1", AssetID: "asset:1", Definition: definition(id), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}
}

func validCapability(id semreg.DefinitionID) semreg.CapabilityInstance {
	return semreg.CapabilityInstance{InstanceID: "capability:1", AssetID: "asset:1", ServiceInstance: "service:1", Definition: definition(id), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Constraints: []semreg.TypedField{}, ActivationEvidence: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}
}

func validCandidate(spec fieldSpec, value semreg.Value) semreg.FactCandidate {
	binding, epoch, generation := semreg.NativeBindingID("binding:1"), semreg.SourceEpochID("epoch:1"), semreg.Uint64("1")
	return semreg.FactCandidate{CandidateID: "candidate:1", Key: validKey(spec), Value: &value, Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}}, Times: semreg.Times{ReceivedAt: semreg.TimePoint{UnixNanoseconds: "1", ClockID: "clock.utc", UncertaintyNS: "0"}, ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "1"}, EvaluatedAt: semreg.TimePoint{UnixNanoseconds: "1", ClockID: "clock.utc", UncertaintyNS: "0"}, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "1"}}, FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1", RetainForNS: "2", MaxWallUncertaintyNS: "0"}, BindingID: &binding, SourceEpochID: &epoch, DriverGeneration: &generation, Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginNativeObservation, SourceID: ptr(semreg.SourceID("source:1")), SourceEpochID: &epoch, BindingID: &binding, Evidence: []semreg.EvidenceRef{testEvidence()}}, Evidence: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}
}

func ptr[T any](value T) *T { return &value }

func testEvidence() semreg.EvidenceRef {
	return semreg.EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Contract: "test.evidence/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone}
}

func requireErrorID(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if actual := semreg.ErrorIdentifier(err); actual != want {
		t.Fatalf("error id = %q, want %q (error %v)", actual, want, err)
	}
}
