package evse

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

func TestEVSEDefinitionAndFields(t *testing.T) {
	v := New()
	if _, ok := v.(operation.OperationPackValidator); !ok {
		t.Fatal("operation hook")
	}
	index := v.Definitions()
	if err := index.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(index.Fields) != 308 || len(index.Services) != 6 || len(index.Capabilities) != 18 || len(index.Operations) != 12 || len(index.EffectRules) != 12 {
		t.Fatalf("definition index: %+v", index)
	}
	for _, spec := range fields {
		t.Run(string(spec.id), func(t *testing.T) {
			key, current := testKey(spec), testValue(spec)
			if spec.kind == semreg.ValueQuantity && spec.unit == "" {
				invented := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.one"}}
				require(t, v.ValidateFact(key, &invented), semreg.InvalidValue)
				return
			}
			if err := v.ValidateFact(key, &current); err != nil {
				t.Fatal(err)
			}
			if spec.kind == semreg.ValueSymbol || spec.kind == semreg.ValueSymbols {
				bad := current
				bad.Symbol = &semreg.Symbol{Namespace: spec.id, Token: "unknown", Known: true}
				bad.Symbols = nil
				require(t, v.ValidateFact(key, &bad), semreg.InvalidValue)
				return
			}
			if spec.kind != semreg.ValueQuantity {
				bad := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.one"}}
				require(t, v.ValidateFact(key, &bad), semreg.InvalidValue)
				return
			}
			if spec.minimum != nil {
				exact := current
				exact.Quantity = &semreg.Quantity{Number: spec.minimum.decimal(), Unit: spec.unit}
				if err := v.ValidateFact(key, &exact); err != nil {
					t.Fatal(err)
				}
				under := current
				under.Quantity = &semreg.Quantity{Number: below(spec.minimum.decimal()), Unit: spec.unit}
				require(t, v.ValidateFact(key, &under), semreg.BoundsExceeded)
			}
			if spec.maximum != nil {
				exact := current
				exact.Quantity = &semreg.Quantity{Number: spec.maximum.decimal(), Unit: spec.unit}
				if err := v.ValidateFact(key, &exact); err != nil {
					t.Fatal(err)
				}
				over := current
				over.Quantity = &semreg.Quantity{Number: above(spec.maximum.decimal()), Unit: spec.unit}
				require(t, v.ValidateFact(key, &over), semreg.BoundsExceeded)
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
	dims := []semreg.Dimension{{ID: spec.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}
	for _, id := range fieldQualifiers[spec.id] {
		dims = append(dims, evseQualifier(id))
	}
	sort.Slice(dims, func(i, j int) bool { return dims[i].ID < dims[j].ID })
	return semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: spec.id, Dimensions: dims}
}
func evseQualifier(id semreg.DefinitionID) semreg.Dimension {
	if qualifierKinds[id] == semreg.ValueQuantity {
		return semreg.Dimension{ID: id, Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.one"}}}
	}
	token := qualifierSymbols[id][0]
	token = strings.TrimPrefix(token, string(id)+".")
	return semreg.Dimension{ID: id, Value: semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: id, Token: token, Known: true}}}
}
func testValue(spec fieldSpec) semreg.Value {
	switch spec.kind {
	case semreg.ValueQuantity:
		number := semreg.Decimal{Coefficient: "0"}
		if spec.minimum != nil {
			number = spec.minimum.decimal()
		}
		return semreg.Value{Kind: spec.kind, Quantity: &semreg.Quantity{Number: number, Unit: spec.unit}}
	case semreg.ValueBoolean:
		flag := false
		return semreg.Value{Kind: spec.kind, Boolean: &flag}
	case semreg.ValueText:
		text := "text"
		return semreg.Value{Kind: spec.kind, Text: &text}
	case semreg.ValueTime:
		return semreg.Value{Kind: spec.kind, Time: &semreg.TimePoint{UnixNanoseconds: "0", ClockID: "clock.utc", UncertaintyNS: "0"}}
	case semreg.ValueSymbols:
		token := strings.TrimPrefix(spec.symbols[0], string(spec.id)+".")
		return semreg.Value{Kind: spec.kind, Symbols: []semreg.Symbol{{Namespace: spec.id, Token: token, Known: true}}}
	default:
		token := strings.TrimPrefix(spec.symbols[0], string(spec.id)+".")
		return semreg.Value{Kind: spec.kind, Symbol: &semreg.Symbol{Namespace: spec.id, Token: token, Known: true}}
	}
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
func above(value semreg.Decimal) semreg.Decimal { return stepDecimal(value, 1) }
func below(value semreg.Decimal) semreg.Decimal { return stepDecimal(value, -1) }
func stepDecimal(value semreg.Decimal, delta int64) semreg.Decimal {
	var n int64
	fmt.Sscan(value.Coefficient, &n)
	n += delta
	exponent := value.Exponent10
	if n == 0 {
		return semreg.Decimal{Coefficient: "0"}
	}
	for n%10 == 0 {
		n /= 10
		exponent++
	}
	return semreg.Decimal{Coefficient: fmt.Sprint(n), Exponent10: exponent}
}
func require(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if got := semreg.ErrorIdentifier(err); got != want {
		t.Fatalf("error %v, want %s", err, want)
	}
}

// Under graph enforcement the field's dimension is the dimension the catalog
// declares: as a resource dimension it carries an opaque resource identifier,
// as a qualifier dimension it keeps its shape, and no undeclared dimension is
// accepted. Without the catalog ValidateFact is unchanged.
func TestValidateGraphFactUsesTheDeclaredDimensions(t *testing.T) {
	var graphValidator semreg.GraphFactValidator = New().(semreg.GraphFactValidator)
	for _, spec := range fields {
		if len(fieldQualifiers[spec.id]) > 0 || spec.kind == semreg.ValueQuantity && spec.unit == "" {
			continue
		}
		key, value := testKey(spec), testValue(spec)
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

// TestAllocatedCurrentPhaseStaysOffConnector keeps a per-phase allocation
// apart from the connector allocation. evse.limit.allocated_current stays in
// ampere on the connector dimension and remains the only constraint of
// evse.capability.set_allocated_current and the only argument of
// evse.operation.set_allocated_current. evse.limit.allocated_current_phase is
// in ampere on the phase dimension with the connector allocation's bounds,
// and neither allocation is accepted on the other's dimension.
func TestAllocatedCurrentPhaseStaysOffConnector(t *testing.T) {
	v := New().(validator)
	sameBound := func(got *decimalBound, want decimalBound) bool { return got != nil && *got == want }
	connector, ok := findField("evse.limit.allocated_current")
	if !ok {
		t.Fatal("evse.limit.allocated_current is absent")
	}
	if connector.kind != semreg.ValueQuantity || connector.unit != "unit.ampere" || connector.dimension != "evse.dimension.connector" || !sameBound(connector.minimum, decimalBound{"0", 0}) || !sameBound(connector.maximum, decimalBound{"1", 3}) || connector.symbols != nil {
		t.Fatalf("evse.limit.allocated_current changed: %+v", connector)
	}
	phase, ok := findField("evse.limit.allocated_current_phase")
	if !ok {
		t.Fatal("evse.limit.allocated_current_phase is absent")
	}
	if phase.kind != semreg.ValueQuantity || phase.unit != "unit.ampere" || phase.dimension != "evse.dimension.phase" || !sameBound(phase.minimum, *connector.minimum) || !sameBound(phase.maximum, *connector.maximum) || phase.symbols != nil {
		t.Fatalf("evse.limit.allocated_current_phase: %+v", phase)
	}
	if got := capabilities["evse.capability.set_allocated_current"]; got.service != "evse.service.connector" || !reflect.DeepEqual(got.constraints, []semreg.DefinitionID{connector.id}) {
		t.Fatalf("evse.capability.set_allocated_current: %+v", got)
	}
	if got := operations["evse.operation.set_allocated_current"]; got.argument != connector.id || got.capability != "evse.capability.set_allocated_current" {
		t.Fatalf("evse.operation.set_allocated_current: %+v", got)
	}
	phaseCapability := capabilities["evse.capability.set_allocated_current_phase"]
	if phaseCapability.service != "evse.service.phase" || !reflect.DeepEqual(phaseCapability.constraints, []semreg.DefinitionID{phase.id}) {
		t.Fatalf("evse.capability.set_allocated_current_phase: %+v", phaseCapability)
	}
	for id, capability := range capabilities {
		for _, constraint := range capability.constraints {
			if constraint == phase.id && id != "evse.capability.set_allocated_current_phase" {
				t.Fatalf("%s constrains %s", id, phase.id)
			}
		}
	}
	for id, op := range operations {
		if op.argument == phase.id && id != "evse.operation.set_allocated_current_phase" {
			t.Fatalf("%s takes %s", id, phase.id)
		}
	}
	listed := false
	for _, ref := range v.Definitions().Fields {
		listed = listed || ref == definition(phase.id)
	}
	if !listed {
		t.Fatalf("%s is not in the definition index", phase.id)
	}
	metadataOK := false
	for _, field := range Metadata().Fields {
		if field.Ref.ID == phase.id {
			metadataOK = field.CanonicalUnit != nil && *field.CanonicalUnit == definition("unit.ampere") && field.Dimension == definition("evse.dimension.phase")
		}
	}
	if !metadataOK {
		t.Fatalf("%s metadata: %+v", phase.id, Metadata().Fields)
	}

	quantity := func(number semreg.Decimal, unit semreg.DefinitionID) *semreg.Value {
		return &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: number, Unit: unit}}
	}
	key := func(id, dimension semreg.DefinitionID) semreg.FactKey {
		name := "phase:1"
		return semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: id, Dimensions: []semreg.Dimension{{ID: dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}}
	}
	own := key(phase.id, phase.dimension)
	for _, number := range []semreg.Decimal{phase.minimum.decimal(), phase.maximum.decimal()} {
		if err := v.ValidateFact(own, quantity(number, "unit.ampere")); err != nil {
			t.Fatalf("per-phase allocation %v: %v", number, err)
		}
	}
	require(t, v.ValidateFact(own, quantity(above(phase.maximum.decimal()), "unit.ampere")), semreg.BoundsExceeded)
	require(t, v.ValidateFact(own, quantity(below(phase.minimum.decimal()), "unit.ampere")), semreg.BoundsExceeded)
	require(t, v.ValidateFact(key(phase.id, "evse.dimension.connector"), quantity(semreg.Decimal{Coefficient: "16"}, "unit.ampere")), semreg.InvalidValue)
	require(t, v.ValidateFact(key(connector.id, "evse.dimension.phase"), quantity(semreg.Decimal{Coefficient: "16"}, "unit.ampere")), semreg.InvalidValue)
	if err := v.ValidateFact(key(connector.id, "evse.dimension.connector"), quantity(semreg.Decimal{Coefficient: "16"}, "unit.ampere")); err != nil {
		t.Fatalf("connector allocation on the connector dimension: %v", err)
	}
	for dimension := range dimensions {
		if dimension == phase.dimension {
			continue
		}
		require(t, v.ValidateFact(key(phase.id, dimension), quantity(semreg.Decimal{Coefficient: "1"}, "unit.ampere")), semreg.InvalidValue)
	}
	for _, spec := range fields {
		if spec.unit == "" || spec.unit == "unit.ampere" {
			continue
		}
		require(t, v.ValidateFact(own, quantity(semreg.Decimal{Coefficient: "1"}, spec.unit)), semreg.InvalidValue)
	}
	symbol := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: phase.id, Token: "x", Known: true}}
	require(t, v.ValidateFact(own, &symbol), semreg.InvalidValue)
	text := "connector:1"
	for name, change := range map[string]func(*semreg.FactKey){
		"pack id":      func(k *semreg.FactKey) { k.PackID = "helianthus.pack.infrastructure" },
		"pack version": func(k *semreg.FactKey) { k.PackVersion = "1.0.1" },
		"fact id":      func(k *semreg.FactKey) { k.FactID = "evse.limit.allocated_current_line" },
		"no dimension": func(k *semreg.FactKey) { k.Dimensions = nil },
		"dimension id": func(k *semreg.FactKey) {
			k.Dimensions = []semreg.Dimension{{ID: "evse.dimension.session", Value: k.Dimensions[0].Value}}
		},
		"dimension value kind": func(k *semreg.FactKey) {
			k.Dimensions = []semreg.Dimension{{ID: phase.dimension, Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.ratio"}}}}
		},
		"extra dimension": func(k *semreg.FactKey) {
			k.Dimensions = append(append([]semreg.Dimension(nil), k.Dimensions...), semreg.Dimension{ID: "evse.dimension.connector", Value: semreg.Value{Kind: semreg.ValueText, Text: &text}})
		},
	} {
		changed := key(phase.id, phase.dimension)
		change(&changed)
		if v.ValidateFact(changed, quantity(semreg.Decimal{Coefficient: "1"}, "unit.ampere")) == nil {
			t.Fatalf("per-phase allocation with another %s accepted", name)
		}
	}
	capability := testCapability("evse.capability.set_allocated_current")
	if err := v.MatchConstraints(capability, []semreg.TypedField{{ID: connector.id, Value: testValue(connector)}}); err != nil {
		t.Fatalf("connector argument: %v", err)
	}
	require(t, v.MatchConstraints(capability, []semreg.TypedField{{ID: phase.id, Value: testValue(phase)}}), semreg.InvalidValue)
}
