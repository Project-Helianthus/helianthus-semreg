package infrastructure

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestDefinitionIndexIsExactAndReadOnly(t *testing.T) {
	validator := New()
	first := validator.Definitions()
	if first.Pack != pack || len(first.Fields) != 68 || len(first.Services) != 8 || len(first.Capabilities) != 6 || len(first.Operations) != 0 || len(first.EffectRules) != 0 {
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
			if spec.kind == semreg.ValueQuantity && spec.unit == "" {
				invented := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.one"}}
				requireErrorID(t, validator.ValidateFact(key, &invented), semreg.InvalidValue)
				return
			}
			if err := validator.ValidateFact(key, &value); err != nil {
				t.Fatalf("valid fact: %v", err)
			}
			if err := validator.ValidateField(definition(spec.id), semreg.TypedField{ID: spec.id, Value: value}); err != nil {
				t.Fatalf("valid typed field: %v", err)
			}
			if len(key.Dimensions) == 1 {
				badDimension := key
				badDimension.Dimensions = append([]semreg.Dimension(nil), key.Dimensions...)
				badDimension.Dimensions[0].ID = "infrastructure.dimension.site"
				requireErrorID(t, validator.ValidateFact(badDimension, &value), semreg.InvalidValue)
			}
			switch spec.kind {
			case semreg.ValueQuantity:
				wrongUnit := value
				wrongUnit.Quantity = &semreg.Quantity{Number: value.Quantity.Number, Unit: "unit.ampere"}
				if spec.unit == "unit.ampere" {
					wrongUnit.Quantity.Unit = "unit.volt"
				}
				requireErrorID(t, validator.ValidateFact(key, &wrongUnit), semreg.InvalidValue)
				if spec.maximum != nil {
					outside := value
					outside.Quantity = &semreg.Quantity{Number: beyond(spec.maximum.decimal()), Unit: spec.unit}
					requireErrorID(t, validator.ValidateFact(key, &outside), semreg.BoundsExceeded)
				}
			case semreg.ValueSymbol:
				wrongSymbol := value
				wrongSymbol.Symbol = &semreg.Symbol{Namespace: spec.id, Token: "unknown", Known: true}
				requireErrorID(t, validator.ValidateFact(key, &wrongSymbol), semreg.InvalidValue)
			default:
				wrongKind := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: "unit.ratio"}}
				requireErrorID(t, validator.ValidateFact(key, &wrongKind), semreg.InvalidValue)
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
	return infrastructureFactKey(spec, "identity")
}

func infrastructureFactKey(spec fieldSpec, text string) semreg.FactKey {
	dims := make([]semreg.Dimension, 0, 1+len(fieldQualifiers[spec.id]))
	for _, id := range fieldQualifiers[spec.id] {
		dims = append(dims, infrastructureQualifier(id))
	}
	dims = append(dims, semreg.Dimension{ID: spec.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &text}})
	sort.Slice(dims, func(i, j int) bool { return dims[i].ID < dims[j].ID })
	return semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: spec.id, Dimensions: dims}
}

func infrastructureQualifier(id semreg.DefinitionID) semreg.Dimension {
	switch qualifierKinds[id] {
	case semreg.ValueQuantity:
		return semreg.Dimension{ID: id, Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.one"}}}
	case semreg.ValueText:
		text := "qualifier"
		return semreg.Dimension{ID: id, Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}
	default:
		token := qualifierSymbols[id][0]
		if len(token) > len(id)+1 && token[:len(id)+1] == string(id)+"." {
			token = token[len(id)+1:]
		}
		return semreg.Dimension{ID: id, Value: semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: id, Token: token, Known: true}}}
	}
}

func validFieldValue(spec fieldSpec) semreg.Value {
	switch spec.kind {
	case semreg.ValueQuantity:
		number := semreg.Decimal{Coefficient: "0"}
		if spec.minimum != nil {
			number = spec.minimum.decimal()
		}
		return semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: number, Unit: spec.unit}}
	case semreg.ValueBoolean:
		value := true
		return semreg.Value{Kind: semreg.ValueBoolean, Boolean: &value}
	case semreg.ValueText:
		value := "text"
		return semreg.Value{Kind: semreg.ValueText, Text: &value}
	case semreg.ValueTime:
		return semreg.Value{Kind: semreg.ValueTime, Time: &semreg.TimePoint{UnixNanoseconds: "0", ClockID: "clock.utc", UncertaintyNS: "0"}}
	case semreg.ValueSymbols:
		name := strings.TrimPrefix(spec.symbols[0], string(spec.id)+".")
		return semreg.Value{Kind: semreg.ValueSymbols, Symbols: []semreg.Symbol{{Namespace: spec.id, Token: name, Known: true}}}
	default:
		name := strings.TrimPrefix(spec.symbols[0], string(spec.id)+".")
		return semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: spec.id, Token: name, Known: true}}
	}
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

// Under graph enforcement the field's dimension is the dimension the catalog
// declares: as a resource dimension it carries an opaque resource identifier,
// as a qualifier dimension it keeps its shape, and no undeclared dimension is
// accepted. Without the catalog ValidateFact is unchanged.
func TestValidateGraphFactUsesTheDeclaredDimensions(t *testing.T) {
	var graphValidator semreg.GraphFactValidator = New().(semreg.GraphFactValidator)
	for _, spec := range fields {
		if len(fieldQualifiers[spec.id]) > 0 {
			continue
		}
		if spec.kind == semreg.ValueQuantity && spec.unit == "" {
			invented := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.one"}}
			if semreg.ErrorIdentifier(New().ValidateFact(validKey(spec), &invented)) != semreg.InvalidValue {
				t.Fatalf("%s invented unit accepted", spec.id)
			}
			continue
		}
		key, value := validKey(spec), validFieldValue(spec)
		withText := func(text string) semreg.FactKey {
			changed := key
			changed.Dimensions = []semreg.Dimension{{ID: key.Dimensions[0].ID, Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}}
			return changed
		}
		resource := semreg.GraphFactSubject{Disposition: semreg.GraphMember, Resolved: true, Dimension: spec.dimension, Kind: "resource.equipment"}
		qualifier := semreg.GraphFactSubject{Disposition: semreg.GraphUnknown, Qualifiers: []semreg.DefinitionID{spec.dimension}}
		if err := graphValidator.ValidateGraphFact(withText("resource:unit:01"), &value, resource); err != nil {
			t.Fatalf("%s: resource identifier refused: %v", spec.id, err)
		}
		if got := semreg.ErrorIdentifier(graphValidator.ValidateGraphFact(withText("unit 01"), &value, resource)); got != semreg.InvalidValue {
			t.Fatalf("%s: resource dimension text accepted: %s", spec.id, got)
		}
		if err := graphValidator.ValidateGraphFact(withText("unit 01"), &value, qualifier); err != nil {
			t.Fatalf("%s: qualifier dimension refused: %v", spec.id, err)
		}
		if err := New().ValidateFact(withText("unit 01"), &value); err != nil {
			t.Fatalf("%s: text dimension refused without the catalog: %v", spec.id, err)
		}
		for subjectName, subject := range map[string]semreg.GraphFactSubject{
			"another resource dimension":      {Disposition: semreg.GraphMember, Resolved: true, Dimension: "zz.dimension.other", Kind: "resource.equipment"},
			"an undeclared extra qualifier":   {Disposition: semreg.GraphMember, Resolved: true, Dimension: spec.dimension, Kind: "resource.equipment", Qualifiers: []semreg.DefinitionID{"zz.dimension.role"}},
			"no declared dimension":           {Disposition: semreg.GraphUnknown},
			"another declared qualifier only": {Disposition: semreg.GraphUnknown, Qualifiers: []semreg.DefinitionID{"zz.dimension.role"}},
		} {
			if got := semreg.ErrorIdentifier(graphValidator.ValidateGraphFact(withText("resource:unit:01"), &value, subject)); got != semreg.InvalidValue {
				t.Fatalf("%s with %s: %s", spec.id, subjectName, got)
			}
		}
	}
}

// TestLineToLineVoltageStaysOffPhase keeps a phase-to-phase voltage apart
// from the phase voltage. infrastructure.ac.voltage stays in volt on the
// phase dimension. infrastructure.ac.voltage_line_line is in volt on the
// line-pair dimension, from 0 to the phase voltage's maximum, and the
// line-pair service keys its facts by that dimension. Neither voltage is
// accepted on the other's dimension, and no capability or operation names
// the line-pair service or field.
func TestLineToLineVoltageStaysOffPhase(t *testing.T) {
	v := New()
	sameBound := func(got *decimalBound, want decimalBound) bool { return got != nil && *got == want }
	phase, ok := findField("infrastructure.ac.voltage")
	if !ok {
		t.Fatal("infrastructure.ac.voltage is absent")
	}
	if phase.kind != semreg.ValueQuantity || phase.unit != "unit.volt" || phase.dimension != "infrastructure.dimension.phase" || !sameBound(phase.minimum, decimalBound{"0", 0}) || !sameBound(phase.maximum, decimalBound{"1", 3}) || phase.symbols != nil {
		t.Fatalf("infrastructure.ac.voltage changed: %+v", phase)
	}
	lineLine, ok := findField("infrastructure.ac.voltage_line_line")
	if !ok {
		t.Fatal("infrastructure.ac.voltage_line_line is absent")
	}
	if lineLine.kind != semreg.ValueQuantity || lineLine.unit != "unit.volt" || lineLine.dimension != "infrastructure.dimension.line_pair" || !sameBound(lineLine.minimum, decimalBound{"0", 0}) || !sameBound(lineLine.maximum, *phase.maximum) || lineLine.symbols != nil {
		t.Fatalf("infrastructure.ac.voltage_line_line: %+v", lineLine)
	}
	if kind, ok := dimensions["infrastructure.dimension.line_pair"]; !ok || kind != semreg.ValueText {
		t.Fatalf("infrastructure.dimension.line_pair = %q, %v", kind, ok)
	}
	if dimension, ok := services["infrastructure.service.line_pair"]; !ok || dimension != "infrastructure.dimension.line_pair" {
		t.Fatalf("infrastructure.service.line_pair keys %q, %v", dimension, ok)
	}
	for id, service := range capabilities {
		if service == "infrastructure.service.line_pair" {
			t.Fatalf("%s uses the line-pair service", id)
		}
	}
	index := v.Definitions()
	listed := func(refs []semreg.DefinitionRef, id semreg.DefinitionID) bool {
		for _, ref := range refs {
			if ref == definition(id) {
				return true
			}
		}
		return false
	}
	if !listed(index.Fields, lineLine.id) || !listed(index.Services, "infrastructure.service.line_pair") || len(index.Operations) != 0 || len(index.EffectRules) != 0 {
		t.Fatalf("definition index: %+v", index)
	}
	metadata := Metadata()
	fieldFound, serviceFound := false, false
	for _, field := range metadata.Fields {
		if field.Ref.ID == lineLine.id {
			fieldFound = field.CanonicalUnit != nil && *field.CanonicalUnit == definition("unit.volt") && field.Dimension == definition("infrastructure.dimension.line_pair")
		}
	}
	for _, service := range metadata.Services {
		if service.Ref.ID == "infrastructure.service.line_pair" {
			serviceFound = service.FactKeyDimension == definition("infrastructure.dimension.line_pair")
		}
	}
	if !fieldFound || !serviceFound || len(metadata.Operations) != 0 {
		t.Fatalf("metadata: %+v", metadata)
	}
	if err := v.ValidateService(validService("infrastructure.service.line_pair")); err != nil {
		t.Fatalf("line-pair service: %v", err)
	}
	requireErrorID(t, v.ValidateCapability(validCapability("infrastructure.capability.read.line_pair")), semreg.DefinitionOwnerMissing)

	quantity := func(number semreg.Decimal, unit semreg.DefinitionID) *semreg.Value {
		return &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: number, Unit: unit}}
	}
	key := func(id, dimension semreg.DefinitionID) semreg.FactKey {
		name := "pair:1"
		return semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: id, Dimensions: []semreg.Dimension{{ID: dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}}
	}
	own := key(lineLine.id, lineLine.dimension)
	for _, number := range []semreg.Decimal{lineLine.minimum.decimal(), lineLine.maximum.decimal()} {
		if err := v.ValidateFact(own, quantity(number, "unit.volt")); err != nil {
			t.Fatalf("line-to-line %v: %v", number, err)
		}
	}
	requireErrorID(t, v.ValidateFact(own, quantity(beyond(lineLine.maximum.decimal()), "unit.volt")), semreg.BoundsExceeded)
	requireErrorID(t, v.ValidateFact(own, quantity(semreg.Decimal{Coefficient: "-1"}, "unit.volt")), semreg.BoundsExceeded)
	requireErrorID(t, v.ValidateFact(key(lineLine.id, "infrastructure.dimension.phase"), quantity(semreg.Decimal{Coefficient: "4", Exponent10: 2}, "unit.volt")), semreg.InvalidValue)
	requireErrorID(t, v.ValidateFact(key(phase.id, "infrastructure.dimension.line_pair"), quantity(semreg.Decimal{Coefficient: "23", Exponent10: 1}, "unit.volt")), semreg.InvalidValue)
	if err := v.ValidateFact(key(phase.id, "infrastructure.dimension.phase"), quantity(semreg.Decimal{Coefficient: "23", Exponent10: 1}, "unit.volt")); err != nil {
		t.Fatalf("phase voltage on the phase dimension: %v", err)
	}
	for dimension := range dimensions {
		if dimension == lineLine.dimension {
			continue
		}
		requireErrorID(t, v.ValidateFact(key(lineLine.id, dimension), quantity(semreg.Decimal{Coefficient: "1"}, "unit.volt")), semreg.InvalidValue)
	}
	for _, spec := range fields {
		if spec.unit == "" || spec.unit == "unit.volt" {
			continue
		}
		requireErrorID(t, v.ValidateFact(own, quantity(semreg.Decimal{Coefficient: "1"}, spec.unit)), semreg.InvalidValue)
	}
	symbol := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: lineLine.id, Token: "x", Known: true}}
	requireErrorID(t, v.ValidateFact(own, &symbol), semreg.InvalidValue)
	text := "pair:2"
	for name, change := range map[string]func(*semreg.FactKey){
		"pack id":      func(k *semreg.FactKey) { k.PackID = "helianthus.pack.evse" },
		"pack version": func(k *semreg.FactKey) { k.PackVersion = "1.0.1" },
		"fact id":      func(k *semreg.FactKey) { k.FactID = "infrastructure.ac.voltage_line_neutral" },
		"no dimension": func(k *semreg.FactKey) { k.Dimensions = nil },
		"dimension id": func(k *semreg.FactKey) {
			k.Dimensions = []semreg.Dimension{{ID: "infrastructure.dimension.meter", Value: k.Dimensions[0].Value}}
		},
		"dimension value kind": func(k *semreg.FactKey) {
			k.Dimensions = []semreg.Dimension{{ID: lineLine.dimension, Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.ratio"}}}}
		},
		"extra dimension": func(k *semreg.FactKey) {
			k.Dimensions = append(append([]semreg.Dimension(nil), k.Dimensions...), semreg.Dimension{ID: "infrastructure.dimension.phase", Value: semreg.Value{Kind: semreg.ValueText, Text: &text}})
		},
	} {
		changed := key(lineLine.id, lineLine.dimension)
		change(&changed)
		if v.ValidateFact(changed, quantity(semreg.Decimal{Coefficient: "1"}, "unit.volt")) == nil {
			t.Fatalf("line-to-line voltage with another %s accepted", name)
		}
	}
}

// TestSelfConsumptionCouplingPowerAndFallbackLimit requires self-consumption
// energy in joule on the meter dimension from 0 to 3.6 × 10^18, the signed
// point-of-coupling power in watt on the grid-connection dimension from
// -10^7 to 10^7, and the fallback grid limit in watt on the grid-connection
// dimension from 0 to 10^7. Each is refused, with the production check it
// catches: under another pack id or pack version (the pack owner check),
// another fact id (the field lookup), every other pack dimension, no
// dimension, an extra dimension or a non-text dimension value (the dimension
// check), every other pack unit and a symbol value (the value check), and
// just outside each bound (the bounds check). Phase voltage stays on the
// phase dimension, line-to-line voltage stays on the line-pair dimension,
// the import and export quantities keep their contracts, and no capability,
// operation or effect rule is added.
func TestSelfConsumptionCouplingPowerAndFallbackLimit(t *testing.T) {
	v := New()
	sameBound := func(got *decimalBound, want decimalBound) bool { return got != nil && *got == want }
	for _, want := range []fieldSpec{
		{id: "infrastructure.ac.voltage", kind: semreg.ValueQuantity, unit: "unit.volt", dimension: "infrastructure.dimension.phase", minimum: bound("0", 0), maximum: bound("1", 3)},
		{id: "infrastructure.ac.voltage_line_line", kind: semreg.ValueQuantity, unit: "unit.volt", dimension: "infrastructure.dimension.line_pair", minimum: bound("0", 0), maximum: bound("1", 3)},
		{id: "infrastructure.energy.import", kind: semreg.ValueQuantity, unit: "unit.joule", dimension: "infrastructure.dimension.meter", minimum: bound("0", 0), maximum: bound("36", 17)},
		{id: "infrastructure.energy.export", kind: semreg.ValueQuantity, unit: "unit.joule", dimension: "infrastructure.dimension.meter", minimum: bound("0", 0), maximum: bound("36", 17)},
		{id: "infrastructure.power.import_active", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
		{id: "infrastructure.power.export_active", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
		{id: "infrastructure.power.import_reactive", kind: semreg.ValueQuantity, unit: "unit.volt_ampere_reactive", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
		{id: "infrastructure.power.export_reactive", kind: semreg.ValueQuantity, unit: "unit.volt_ampere_reactive", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	} {
		spec, ok := findField(want.id)
		if !ok || spec.kind != want.kind || spec.unit != want.unit || spec.dimension != want.dimension || !sameBound(spec.minimum, *want.minimum) || !sameBound(spec.maximum, *want.maximum) || spec.symbols != nil {
			t.Fatalf("%s changed: %+v", want.id, spec)
		}
	}
	units := map[semreg.DefinitionID]bool{"unit.other": true}
	for _, spec := range fields {
		if spec.unit != "" {
			units[spec.unit] = true
		}
	}
	indexed := map[semreg.DefinitionRef]bool{}
	for _, ref := range v.Definitions().Fields {
		indexed[ref] = true
	}
	metadata := map[semreg.DefinitionID]semreg.FieldMetadata{}
	for _, field := range Metadata().Fields {
		metadata[field.Ref.ID] = field
	}
	quantity := func(number semreg.Decimal, unit semreg.DefinitionID) *semreg.Value {
		return &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: number, Unit: unit}}
	}
	for _, want := range []struct {
		fieldSpec
		signed bool
	}{
		{fieldSpec{id: "infrastructure.energy.self_consumption", kind: semreg.ValueQuantity, unit: "unit.joule", dimension: "infrastructure.dimension.meter", minimum: bound("0", 0), maximum: bound("36", 17)}, false},
		{fieldSpec{id: "infrastructure.power.pcc", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("-1", 7), maximum: bound("1", 7)}, true},
		{fieldSpec{id: "infrastructure.limit.fallback_power", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)}, false},
	} {
		spec, ok := findField(want.id)
		if !ok {
			t.Fatalf("%s is absent", want.id)
		}
		if spec.kind != want.kind || spec.unit != want.unit || spec.dimension != want.dimension || !sameBound(spec.minimum, *want.minimum) || !sameBound(spec.maximum, *want.maximum) || spec.symbols != nil {
			t.Fatalf("%s: %+v", want.id, spec)
		}
		if !indexed[definition(want.id)] {
			t.Fatalf("%s is not in the definition index", want.id)
		}
		if m, ok := metadata[want.id]; !ok || m.CanonicalUnit == nil || *m.CanonicalUnit != definition(want.unit) || m.Dimension != definition(want.dimension) {
			t.Fatalf("%s metadata: %+v", want.id, m)
		}
		own := validKey(want.fieldSpec)
		for _, number := range []semreg.Decimal{want.minimum.decimal(), want.maximum.decimal()} {
			if err := v.ValidateFact(own, quantity(number, want.unit)); err != nil {
				t.Fatalf("%s %v: %v", want.id, number, err)
			}
		}
		requireErrorID(t, v.ValidateFact(own, quantity(beyond(want.maximum.decimal()), want.unit)), semreg.BoundsExceeded)
		below := semreg.Decimal{Coefficient: fmt.Sprintf("%d", decimalInteger(want.minimum.decimal())-1), Exponent10: want.minimum.exponent10}
		requireErrorID(t, v.ValidateFact(own, quantity(below, want.unit)), semreg.BoundsExceeded)
		negative := v.ValidateFact(own, quantity(semreg.Decimal{Coefficient: "-1"}, want.unit))
		if want.signed && negative != nil || !want.signed && semreg.ErrorIdentifier(negative) != semreg.BoundsExceeded {
			t.Fatalf("%s negative value: %v", want.id, negative)
		}
		for dimension := range dimensions {
			if dimension == want.dimension {
				continue
			}
			changed := validKey(want.fieldSpec)
			changed.Dimensions[0].ID = dimension
			requireErrorID(t, v.ValidateFact(changed, quantity(want.minimum.decimal(), want.unit)), semreg.InvalidValue)
		}
		for unit := range units {
			if unit != want.unit {
				requireErrorID(t, v.ValidateFact(own, quantity(want.minimum.decimal(), unit)), semreg.InvalidValue)
			}
		}
		symbol := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: want.id, Token: "x", Known: true}}
		requireErrorID(t, v.ValidateFact(own, &symbol), semreg.InvalidValue)
		text := "other"
		for name, change := range map[string]func(*semreg.FactKey){
			"pack id":      func(k *semreg.FactKey) { k.PackID = "helianthus.pack.pv" },
			"pack version": func(k *semreg.FactKey) { k.PackVersion = "1.0.1" },
			"fact id":      func(k *semreg.FactKey) { k.FactID = want.id + "_other" },
			"no dimension": func(k *semreg.FactKey) { k.Dimensions = nil },
			"dimension value kind": func(k *semreg.FactKey) {
				k.Dimensions = []semreg.Dimension{{ID: want.dimension, Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.ratio"}}}}
			},
			"extra dimension": func(k *semreg.FactKey) {
				k.Dimensions = append(k.Dimensions, semreg.Dimension{ID: "infrastructure.dimension.site", Value: semreg.Value{Kind: semreg.ValueText, Text: &text}})
			},
		} {
			changed := validKey(want.fieldSpec)
			change(&changed)
			if v.ValidateFact(changed, quantity(want.minimum.decimal(), want.unit)) == nil {
				t.Fatalf("%s with another %s accepted", want.id, name)
			}
		}
	}
	index := v.Definitions()
	if len(index.Capabilities) != 6 || len(index.Operations) != 0 || len(index.EffectRules) != 0 || len(capabilities) != 6 {
		t.Fatalf("definition index: %+v", index)
	}
}

// TestPilotAccuracyFacts requires the measured boolean and the ratio maximum
// on the meter dimension, qualified by the measurement-type symbol dimension.
// Each refusal names the production check it catches. Coefficients 1 and -1
// of the ratio are accepted, so a lower bound of zero refuses them.
func TestPilotAccuracyFacts(t *testing.T) {
	v := New()
	graphValidator := New().(semreg.GraphFactValidator)
	for _, id := range []semreg.DefinitionID{"infrastructure.accuracy.measured", "infrastructure.accuracy.ratio_maximum"} {
		spec, ok := findField(id)
		if !ok || spec.dimension != "infrastructure.dimension.meter" || !sameIDsOf(fieldQualifiers[id], []semreg.DefinitionID{"infrastructure.dimension.measurement_type"}) {
			t.Fatalf("%s = %+v qualifiers %v", id, spec, fieldQualifiers[id])
		}
		key := infrastructureFactKey(spec, "meter:1")
		value := validFieldValue(spec)
		if err := v.ValidateFact(key, &value); err != nil {
			t.Fatal(id, err)
		}
		if spec.kind == semreg.ValueBoolean {
			for _, bit := range []bool{true, false} {
				bit := bit
				accepted := semreg.Value{Kind: semreg.ValueBoolean, Boolean: &bit}
				if err := v.ValidateFact(key, &accepted); err != nil {
					t.Fatalf("%s %v: %v", id, bit, err)
				}
			}
			quantity := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.ratio"}}
			requireErrorID(t, v.ValidateFact(key, &quantity), semreg.InvalidValue)
		} else {
			for _, coefficient := range []string{"1", "-1"} {
				accepted := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: coefficient}, Unit: spec.unit}}
				if err := v.ValidateFact(key, &accepted); err != nil {
					t.Fatalf("%s coefficient %s: %v", id, coefficient, err)
				}
			}
			wrongUnit := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.watt"}}
			requireErrorID(t, v.ValidateFact(key, &wrongUnit), semreg.InvalidValue)
			bit := true
			wrongKind := semreg.Value{Kind: semreg.ValueBoolean, Boolean: &bit}
			requireErrorID(t, v.ValidateFact(key, &wrongKind), semreg.InvalidValue)
		}
		refuse := func(name string, changed semreg.FactKey) {
			t.Helper()
			if v.ValidateFact(changed, &value) == nil {
				t.Fatalf("%s with another %s accepted", id, name)
			}
		}
		otherPack := key
		otherPack.PackID = "helianthus.pack.other"
		refuse("pack id", otherPack)
		otherVersion := key
		otherVersion.PackVersion = "9.0.0"
		refuse("pack version", otherVersion)
		otherFact := key
		otherFact.FactID = id + ".other"
		refuse("fact id", otherFact)
		requireErrorID(t, v.ValidateFact(pilotDropDimension(key, "infrastructure.dimension.measurement_type"), &value), semreg.InvalidValue)
		requireErrorID(t, v.ValidateFact(pilotReplaceDimensionID(key, "infrastructure.dimension.measurement_type", "zz.dimension.type"), &value), semreg.InvalidValue)
		requireErrorID(t, v.ValidateFact(pilotReplaceDimensionID(key, spec.dimension, "zz.dimension.subject"), &value), semreg.InvalidValue)
		requireErrorID(t, v.ValidateFact(pilotQualifierText(key, "infrastructure.dimension.measurement_type"), &value), semreg.InvalidValue)
		requireErrorID(t, v.ValidateFact(pilotQualifierToken(key, "infrastructure.dimension.measurement_type", "other"), &value), semreg.InvalidValue)
		requireErrorID(t, v.ValidateFact(pilotQualifierNamespace(key, "infrastructure.dimension.measurement_type", "zz.dimension.type"), &value), semreg.InvalidValue)
		requireErrorID(t, v.ValidateFact(pilotSubjectSymbol(key, spec.dimension), &value), semreg.InvalidValue)
		extra := pilotCopyKey(key)
		text := "extra"
		extra.Dimensions = append(extra.Dimensions, semreg.Dimension{ID: "zz.dimension.extra", Value: semreg.Value{Kind: semreg.ValueText, Text: &text}})
		sort.Slice(extra.Dimensions, func(i, j int) bool { return extra.Dimensions[i].ID < extra.Dimensions[j].ID })
		requireErrorID(t, v.ValidateFact(extra, &value), semreg.InvalidValue)
		empty := key
		empty.Dimensions = []semreg.Dimension{}
		requireErrorID(t, v.ValidateFact(empty, &value), semreg.InvalidValue)
		resourceKey := infrastructureFactKey(spec, "resource:meter:01")
		subject := semreg.GraphFactSubject{Disposition: semreg.GraphMember, Resolved: true, Dimension: spec.dimension, Kind: "resource.meter", Qualifiers: append([]semreg.DefinitionID(nil), fieldQualifiers[id]...)}
		if err := graphValidator.ValidateGraphFact(resourceKey, &value, subject); err != nil {
			t.Fatalf("%s graph: %v", id, err)
		}
		plain := infrastructureFactKey(spec, "meter 01")
		requireErrorID(t, graphValidator.ValidateGraphFact(plain, &value, subject), semreg.InvalidValue)
		if err := v.ValidateFact(plain, &value); err != nil {
			t.Fatalf("%s without the catalog: %v", id, err)
		}
		bare := subject
		bare.Qualifiers = nil
		requireErrorID(t, graphValidator.ValidateGraphFact(resourceKey, &value, bare), semreg.InvalidValue)
	}
}

func sameIDsOf(got, want []semreg.DefinitionID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func pilotCopyKey(key semreg.FactKey) semreg.FactKey {
	key.Dimensions = append([]semreg.Dimension(nil), key.Dimensions...)
	for i := range key.Dimensions {
		if key.Dimensions[i].Value.Text != nil {
			text := *key.Dimensions[i].Value.Text
			key.Dimensions[i].Value.Text = &text
		}
		if key.Dimensions[i].Value.Symbol != nil {
			symbol := *key.Dimensions[i].Value.Symbol
			key.Dimensions[i].Value.Symbol = &symbol
		}
	}
	return key
}

func pilotDropDimension(key semreg.FactKey, id semreg.DefinitionID) semreg.FactKey {
	key = pilotCopyKey(key)
	kept := make([]semreg.Dimension, 0, len(key.Dimensions))
	for _, dimension := range key.Dimensions {
		if dimension.ID != id {
			kept = append(kept, dimension)
		}
	}
	key.Dimensions = kept
	return key
}

func pilotReplaceDimensionID(key semreg.FactKey, from, to semreg.DefinitionID) semreg.FactKey {
	key = pilotCopyKey(key)
	for i := range key.Dimensions {
		if key.Dimensions[i].ID == from {
			key.Dimensions[i].ID = to
		}
	}
	sort.Slice(key.Dimensions, func(i, j int) bool { return key.Dimensions[i].ID < key.Dimensions[j].ID })
	return key
}

func pilotQualifierText(key semreg.FactKey, id semreg.DefinitionID) semreg.FactKey {
	key = pilotCopyKey(key)
	text := "voltage"
	for i := range key.Dimensions {
		if key.Dimensions[i].ID == id {
			key.Dimensions[i].Value = semreg.Value{Kind: semreg.ValueText, Text: &text}
		}
	}
	return key
}

func pilotQualifierToken(key semreg.FactKey, id semreg.DefinitionID, token string) semreg.FactKey {
	key = pilotCopyKey(key)
	for i := range key.Dimensions {
		if key.Dimensions[i].ID == id && key.Dimensions[i].Value.Symbol != nil {
			key.Dimensions[i].Value.Symbol.Token = token
		}
	}
	return key
}

func pilotQualifierNamespace(key semreg.FactKey, id semreg.DefinitionID, namespace semreg.DefinitionID) semreg.FactKey {
	key = pilotCopyKey(key)
	for i := range key.Dimensions {
		if key.Dimensions[i].ID == id && key.Dimensions[i].Value.Symbol != nil {
			key.Dimensions[i].Value.Symbol.Namespace = namespace
		}
	}
	return key
}

func pilotSubjectSymbol(key semreg.FactKey, id semreg.DefinitionID) semreg.FactKey {
	key = pilotCopyKey(key)
	for i := range key.Dimensions {
		if key.Dimensions[i].ID == id {
			key.Dimensions[i].Value = semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: id, Token: "active_current", Known: true}}
		}
	}
	return key
}
