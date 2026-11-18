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
	if first.Pack != pack || len(first.Fields) != 48 || len(first.Services) != 11 || len(first.Capabilities) != 12 || len(first.Operations) != 2 || len(first.EffectRules) != 2 {
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
					unit = "unit.kilowatt"
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
		capability.Constraints[0].Value.Quantity.Number = above(mustField("thermal.setpoint.temperature_setpoint").maximum.decimal())
		requireID(t, validator.ValidateCapability(capability), semreg.BoundsExceeded)
	})
	t.Run("operation argument must admit published constraint", func(t *testing.T) {
		capability := validCapability("thermal.capability.set_temperature")
		argument := copyFields(capability.Constraints)
		// A fresh quantity: the copied constraint shares the capability's one.
		argument[0].Value.Quantity = &semreg.Quantity{Number: mustField("thermal.setpoint.temperature_setpoint").maximum.decimal(), Unit: argument[0].Value.Quantity.Unit}
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

// requireID requires an error with the stable identifier want. A nil error
// fails: ErrorIdentifier reports invalid_value for any error that is not a
// semreg error, including nil.
func requireID(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %q", want)
	}
	if actual := semreg.ErrorIdentifier(err); actual != want {
		t.Fatalf("error = %q, want %q (%v)", actual, want, err)
	}
}

// Under graph enforcement the field's one dimension is its resource
// dimension: the catalog declares it as the resolved subject of the field with
// no qualifier dimension, and its value is an opaque resource identifier. A
// subject that names another dimension, an unresolved subject and a declared
// qualifier dimension are refused. Without the catalog ValidateFact is
// unchanged.
func TestValidateGraphFactUsesTheResourceDimension(t *testing.T) {
	var graphValidator semreg.GraphFactValidator = New().(semreg.GraphFactValidator)
	for _, spec := range fields {
		key, value := validKey(spec), validValue(spec)
		withText := func(text string) semreg.FactKey {
			changed := key
			changed.Dimensions = []semreg.Dimension{{ID: key.Dimensions[0].ID, Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}}
			return changed
		}
		resource := semreg.GraphFactSubject{Disposition: semreg.GraphMember, Resolved: true, Dimension: spec.dimension, Kind: "resource.equipment"}
		if err := graphValidator.ValidateGraphFact(withText("resource:unit:01"), &value, resource); err != nil {
			t.Fatalf("%s: resource identifier refused: %v", spec.id, err)
		}
		requireID(t, graphValidator.ValidateGraphFact(withText("unit 01"), &value, resource), semreg.InvalidValue)
		if err := New().ValidateFact(withText("unit 01"), &value); err != nil {
			t.Fatalf("%s: text dimension refused without the catalog: %v", spec.id, err)
		}
		extra := withText("resource:unit:01")
		extra.Dimensions = append(extra.Dimensions, semreg.Dimension{ID: "zz.dimension.role", Value: extra.Dimensions[0].Value})
		requireID(t, graphValidator.ValidateGraphFact(extra, &value, resource), semreg.InvalidValue)
		for subjectName, subject := range map[string]semreg.GraphFactSubject{
			"another resource dimension":         {Disposition: semreg.GraphMember, Resolved: true, Dimension: "zz.dimension.other", Kind: "resource.equipment"},
			"a declared qualifier":               {Disposition: semreg.GraphMember, Resolved: true, Dimension: spec.dimension, Kind: "resource.equipment", Qualifiers: []semreg.DefinitionID{"zz.dimension.role"}},
			"the field dimension as a qualifier": {Disposition: semreg.GraphUnknown, Qualifiers: []semreg.DefinitionID{spec.dimension}},
			"no declared dimension":              {Disposition: semreg.GraphUnknown},
			"an unresolved subject":              {Disposition: semreg.GraphUnknown, Dimension: spec.dimension, Kind: "resource.equipment"},
		} {
			if err := graphValidator.ValidateGraphFact(withText("resource:unit:01"), &value, subject); err == nil || semreg.ErrorIdentifier(err) != semreg.InvalidValue {
				t.Fatalf("%s with %s: %v", spec.id, subjectName, err)
			}
		}
	}
}

// overrunTypes are the ten public overrun types. Each is a symbol field on
// the system dimension with the statuses active, finished, inactive and
// running.
var overrunTypes = []string{"one_time_dhw", "party", "sg_ready_condition_1", "sg_ready_condition_3", "sg_ready_condition_4", "one_day_away", "one_day_at_home", "one_time_ventilation", "hvac_system_off", "valve_kick"}

// TestOverrunStatusesOnTheSystemDimension requires the ten overrun statuses
// on thermal.dimension.system with exactly four statuses each, read by the
// system descriptor only, and used by no capability or operation.
func TestOverrunStatusesOnTheSystemDimension(t *testing.T) {
	for _, name := range overrunTypes {
		id := semreg.DefinitionID("thermal.overrun." + name)
		prefix := string(id) + "."
		requireThermalField(t, fieldSpec{id: id, kind: semreg.ValueSymbol, dimension: systemDimension, symbols: []string{prefix + "active", prefix + "finished", prefix + "inactive", prefix + "running"}})
	}
	if len(overrunTypes) != 10 || len(capabilities) != 12 || len(operations) != 2 {
		t.Fatalf("overrun types %d, capabilities %d, operations %d", len(overrunTypes), len(capabilities), len(operations))
	}
}

// requireThermalField requires want to be in the field table exactly (kind,
// unit, dimension, bounds and symbols), in the definition index and in the
// metadata with its dimension and unit; to be accepted on its own dimension
// at each bound or with each symbol; and to be refused, with the production
// check each refusal catches:
//   - pack id or pack version changed: the pack owner check;
//   - fact id changed: the field lookup;
//   - dimension id changed to every other pack dimension, no dimension, an
//     extra dimension or a non-text dimension value: the one-dimension check;
//   - a value of the other kind: the value kind check;
//   - for a quantity, every other pack unit and each value just outside a
//     bound: the unit and bounds checks;
//   - for a symbol, another namespace, an unknown token and an unknown
//     symbol: the symbol check.
//
// It also requires that no capability constraint, operation argument or
// effect fact names the field.
func requireThermalField(t *testing.T, want fieldSpec) {
	t.Helper()
	validator := New()
	spec, ok := findField(want.id)
	if !ok {
		t.Fatalf("%s is absent", want.id)
	}
	if !sameThermalSpec(spec, want) {
		t.Fatalf("%s: got %+v, want %+v", want.id, spec, want)
	}
	indexed := false
	for _, ref := range validator.Definitions().Fields {
		indexed = indexed || ref == definition(want.id)
	}
	if !indexed {
		t.Fatalf("%s is not in the definition index", want.id)
	}
	found := false
	for _, field := range Metadata().Fields {
		if field.Ref.ID != want.id {
			continue
		}
		unitOK := want.unit == "" && field.CanonicalUnit == nil || want.unit != "" && field.CanonicalUnit != nil && *field.CanonicalUnit == definition(want.unit)
		found = unitOK && field.Dimension == definition(want.dimension)
	}
	if !found {
		t.Fatalf("%s metadata differs", want.id)
	}
	key := validKey(want)
	var accepted []semreg.Value
	if want.kind == semreg.ValueQuantity {
		for _, number := range []semreg.Decimal{want.minimum.decimal(), want.maximum.decimal()} {
			accepted = append(accepted, semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: number, Unit: want.unit}})
		}
	} else {
		for _, symbol := range want.symbols {
			accepted = append(accepted, semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: want.id, Token: strings.TrimPrefix(symbol, string(want.id)+"."), Known: true}})
		}
	}
	for _, value := range accepted {
		value := value
		if err := validator.ValidateFact(key, &value); err != nil {
			t.Fatalf("%s %+v: %v", want.id, value, err)
		}
	}
	value := accepted[0]
	text := "other"
	for name, change := range map[string]func(*semreg.FactKey){
		"pack id":      func(k *semreg.FactKey) { k.PackID = "helianthus.pack.other" },
		"pack version": func(k *semreg.FactKey) { k.PackVersion = "1.0.1" },
		"fact id":      func(k *semreg.FactKey) { k.FactID = want.id + "_other" },
		"no dimension": func(k *semreg.FactKey) { k.Dimensions = nil },
		"dimension value kind": func(k *semreg.FactKey) {
			b := true
			k.Dimensions[0].Value = semreg.Value{Kind: semreg.ValueBoolean, Boolean: &b}
		},
		"extra dimension": func(k *semreg.FactKey) {
			k.Dimensions = append(k.Dimensions, semreg.Dimension{ID: want.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &text}})
		},
	} {
		changed := validKey(want)
		change(&changed)
		if validator.ValidateFact(changed, &value) == nil {
			t.Fatalf("%s with another %s accepted", want.id, name)
		}
	}
	for dimension := range dimensions {
		if dimension == want.dimension {
			continue
		}
		changed := validKey(want)
		changed.Dimensions[0].ID = dimension
		requireID(t, validator.ValidateFact(changed, &value), semreg.InvalidValue)
	}
	if want.kind == semreg.ValueQuantity {
		units := map[semreg.DefinitionID]bool{"unit.other": true}
		for _, other := range fields {
			if other.unit != "" {
				units[other.unit] = true
			}
		}
		for unit := range units {
			if unit == want.unit {
				continue
			}
			wrong := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: want.minimum.decimal(), Unit: unit}}
			requireID(t, validator.ValidateFact(key, &wrong), semreg.InvalidValue)
		}
		for _, number := range []semreg.Decimal{below(want.minimum.decimal()), above(want.maximum.decimal())} {
			outside := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: number, Unit: want.unit}}
			requireID(t, validator.ValidateFact(key, &outside), semreg.BoundsExceeded)
		}
		symbol := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: want.id, Token: "active", Known: true}}
		requireID(t, validator.ValidateFact(key, &symbol), semreg.InvalidValue)
	} else {
		token := accepted[0].Symbol.Token
		for name, symbol := range map[string]semreg.Symbol{
			"namespace":      {Namespace: "thermal.mode.system", Token: token, Known: true},
			"token":          {Namespace: want.id, Token: "unknown_status", Known: true},
			"unknown symbol": {Namespace: want.id, Token: token, Known: false},
		} {
			wrong := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &symbol}
			if validator.ValidateFact(key, &wrong) == nil {
				t.Fatalf("%s with another %s accepted", want.id, name)
			}
		}
		quantity := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: ratio}}
		requireID(t, validator.ValidateFact(key, &quantity), semreg.InvalidValue)
	}
	for id, capability := range capabilities {
		for _, constraint := range capability.constraints {
			if constraint == want.id {
				t.Fatalf("%s constrains %s", want.id, id)
			}
		}
	}
	for id, op := range operations {
		if op.argument == want.id {
			t.Fatalf("%s is the argument of %s", want.id, id)
		}
	}
}

// sameThermalSpec reports whether two table entries agree in every property.
func sameThermalSpec(a, b fieldSpec) bool {
	sameBound := func(x, y *decimalBound) bool { return x == nil && y == nil || x != nil && y != nil && *x == *y }
	return a.id == b.id && a.kind == b.kind && a.unit == b.unit && a.dimension == b.dimension && sameBound(a.minimum, b.minimum) && sameBound(a.maximum, b.maximum) && reflect.DeepEqual(a.symbols, b.symbols)
}

// TestZoneSetpointsAndSystemModes requires the occupied and unoccupied
// heating and cooling setpoints in kelvin on the zone dimension with the
// setpoint bounds, and the nine system-mode symbols. The zone setpoint keeps
// its contract and stays the only constraint of the set-temperature
// capability and the only argument and effect fact of its operation.
func TestZoneSetpointsAndSystemModes(t *testing.T) {
	for _, id := range []semreg.DefinitionID{"thermal.setpoint.occupied_cooling", "thermal.setpoint.occupied_heating", "thermal.setpoint.unoccupied_cooling", "thermal.setpoint.unoccupied_heating"} {
		requireThermalField(t, fieldSpec{id: id, kind: semreg.ValueQuantity, unit: kelvin, dimension: zoneDimension, minimum: bound("22315", -2), maximum: bound("42315", -2)})
	}
	mode, ok := findField("thermal.mode.system")
	wantModes := []string{"thermal.mode.system.auto", "thermal.mode.system.cool", "thermal.mode.system.dry", "thermal.mode.system.emergency_heat", "thermal.mode.system.fan_only", "thermal.mode.system.heat", "thermal.mode.system.off", "thermal.mode.system.precooling", "thermal.mode.system.sleep"}
	if !ok || !sameThermalSpec(mode, fieldSpec{id: "thermal.mode.system", kind: semreg.ValueSymbol, dimension: systemDimension, symbols: wantModes}) {
		t.Fatalf("thermal.mode.system: %+v", mode)
	}
	setpoint, ok := findField("thermal.setpoint.temperature_setpoint")
	if !ok || !sameThermalSpec(setpoint, fieldSpec{id: "thermal.setpoint.temperature_setpoint", kind: semreg.ValueQuantity, unit: kelvin, dimension: zoneDimension, minimum: bound("22315", -2), maximum: bound("42315", -2)}) {
		t.Fatalf("thermal.setpoint.temperature_setpoint: %+v", setpoint)
	}
	capability := capabilities["thermal.capability.set_temperature"]
	if capability.service != "thermal.service.zone" || !reflect.DeepEqual(capability.constraints, []semreg.DefinitionID{"thermal.setpoint.temperature_setpoint"}) {
		t.Fatalf("set-temperature capability: %+v", capability)
	}
	if op := operations["thermal.operation.set_temperature"]; op != (operationSpec{"thermal.capability.set_temperature", "thermal.setpoint.temperature_setpoint", "thermal.effect.set_temperature"}) {
		t.Fatalf("set-temperature operation: %+v", op)
	}
	if op := operations["thermal.operation.set_mode"]; op != (operationSpec{"thermal.capability.set_mode", "thermal.mode.system", "thermal.effect.set_mode"}) {
		t.Fatalf("set-mode operation: %+v", op)
	}
	validator := New()
	for _, symbol := range wantModes {
		token := strings.TrimPrefix(symbol, "thermal.mode.system.")
		value := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: "thermal.mode.system", Token: token, Known: true}}
		if err := validator.ValidateFact(validKey(mode), &value); err != nil {
			t.Fatalf("%s: %v", symbol, err)
		}
		if err := validator.ValidateField(definition("thermal.mode.system"), semreg.TypedField{ID: "thermal.mode.system", Value: value}); err != nil {
			t.Fatalf("%s as the set-mode argument: %v", symbol, err)
		}
	}
	for _, token := range []string{"cooling", "heating", "unknown_mode"} {
		value := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: "thermal.mode.system", Token: token, Known: true}}
		requireID(t, validator.ValidateFact(validKey(mode), &value), semreg.InvalidValue)
	}
}

// TestPumpSpeedAndValvePosition requires the pump dimension and the pump
// service keyed by it, pump speed in reciprocal second on the pump dimension
// from 0 to 1000, and valve position as a 0 to 1 ratio on the terminal-unit
// dimension. No capability reads the pump service, and compressor speed,
// flow rate and the pressures keep their contracts.
func TestPumpSpeedAndValvePosition(t *testing.T) {
	if kind, ok := dimensions["thermal.dimension.pump"]; !ok || kind != semreg.ValueText {
		t.Fatalf("thermal.dimension.pump = %q, %v", kind, ok)
	}
	if dimension, ok := services["thermal.service.pump"]; !ok || dimension != "thermal.dimension.pump" {
		t.Fatalf("thermal.service.pump keys %q, %v", dimension, ok)
	}
	requireThermalField(t, fieldSpec{id: "thermal.measurement.pump_speed", kind: semreg.ValueQuantity, unit: reciprocalSecond, dimension: "thermal.dimension.pump", minimum: bound("0", 0), maximum: bound("1", 3)})
	requireThermalField(t, fieldSpec{id: "thermal.measurement.valve_position", kind: semreg.ValueQuantity, unit: ratio, dimension: terminalDimension, minimum: bound("0", 0), maximum: bound("1", 0)})
	for id, capability := range capabilities {
		if capability.service == "thermal.service.pump" {
			t.Fatalf("%s reads the pump service", id)
		}
	}
	validator := New()
	listed := false
	for _, ref := range validator.Definitions().Services {
		listed = listed || ref == definition("thermal.service.pump")
	}
	keyed := false
	for _, service := range Metadata().Services {
		keyed = keyed || service.Ref == definition("thermal.service.pump") && service.FactKeyDimension == definition("thermal.dimension.pump")
	}
	if !listed || !keyed {
		t.Fatalf("pump service index %v, metadata %v", listed, keyed)
	}
	for _, want := range []fieldSpec{
		{id: "thermal.measurement.compressor_speed", kind: semreg.ValueQuantity, unit: reciprocalSecond, dimension: compressorDimension, minimum: bound("0", 0), maximum: bound("1", 3)},
		{id: "thermal.measurement.flow_rate", kind: semreg.ValueQuantity, unit: cubicMetrePerSecond, dimension: circuitDimension, minimum: bound("0", 0), maximum: bound("2", -1)},
		{id: "thermal.measurement.water_pressure", kind: semreg.ValueQuantity, unit: pascal, dimension: generatorDimension, minimum: bound("-101325", 0), maximum: bound("1", 7)},
		{id: "thermal.measurement.refrigerant_high_side_pressure", kind: semreg.ValueQuantity, unit: pascal, dimension: refrigerantDimension, minimum: bound("0", 0), maximum: bound("1", 8)},
		{id: "thermal.measurement.refrigerant_low_side_pressure", kind: semreg.ValueQuantity, unit: pascal, dimension: refrigerantDimension, minimum: bound("0", 0), maximum: bound("1", 8)},
	} {
		if spec, ok := findField(want.id); !ok || !sameThermalSpec(spec, want) {
			t.Fatalf("%s changed: %+v", want.id, spec)
		}
	}
	speed := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "5", Exponent10: 1}, Unit: reciprocalSecond}}
	pumpKey := validKey(fieldSpec{id: "thermal.measurement.pump_speed", dimension: "thermal.dimension.pump"})
	compressorKey := validKey(fieldSpec{id: "thermal.measurement.compressor_speed", dimension: "thermal.dimension.pump"})
	requireID(t, validator.ValidateFact(compressorKey, &speed), semreg.InvalidValue)
	if err := validator.ValidateFact(pumpKey, &speed); err != nil {
		t.Fatalf("pump speed: %v", err)
	}
}
