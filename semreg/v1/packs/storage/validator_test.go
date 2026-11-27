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
	if len(index.Fields) != 106 || len(index.Services) != 7 || len(index.Capabilities) != 20 || len(index.Operations) != 17 || len(index.EffectRules) != 17 {
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
	switch spec.kind {
	case semreg.ValueQuantity:
		n := semreg.Decimal{Coefficient: "0", Exponent10: 0}
		if spec.minimum != nil {
			n = spec.minimum.decimal()
		}
		return semreg.Value{Kind: spec.kind, Quantity: &semreg.Quantity{Number: n, Unit: spec.unit}}
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

// TestStorageEnergyCapacitiesStayJoule requires the three pack energy
// capacities in joule and keeps the two ampere-hour capacities in ampere-hour.
// Removing a capacity from the field table, or changing its kind, unit,
// dimension, minimum, maximum or symbols, fails the lookup for that identifier.
func TestStorageEnergyCapacitiesStayJoule(t *testing.T) {
	for _, id := range []semreg.DefinitionID{
		"storage.energy.remaining",
		"storage.energy.usable_capacity",
		"storage.energy.nominal_capacity",
	} {
		spec, ok := findField(id)
		if !ok {
			t.Errorf("%s is absent", id)
			continue
		}
		if spec.kind != semreg.ValueQuantity {
			t.Errorf("%s kind %s", id, spec.kind)
		}
		if spec.unit != "unit.joule" {
			t.Errorf("%s unit %s", id, spec.unit)
		}
		if spec.dimension != "storage.dimension.pack" {
			t.Errorf("%s dimension %s", id, spec.dimension)
		}
		if spec.minimum == nil || spec.minimum.coefficient != "0" || spec.minimum.exponent10 != 0 {
			t.Errorf("%s minimum %+v", id, spec.minimum)
		}
		if spec.maximum != nil {
			t.Errorf("%s maximum %+v", id, spec.maximum)
		}
		if len(spec.symbols) != 0 {
			t.Errorf("%s symbols %v", id, spec.symbols)
		}
	}
	for _, id := range []semreg.DefinitionID{"storage.capacity.charge", "storage.capacity.discharge"} {
		spec, ok := findField(id)
		if !ok || spec.kind != semreg.ValueQuantity || spec.unit != "unit.ampere_hour" || spec.dimension != "storage.dimension.pack" {
			t.Errorf("%s = %+v present %v", id, spec, ok)
		}
	}
}

// Under graph enforcement the field's dimension is the dimension the catalog
// declares: as a resource dimension it carries an opaque resource identifier,
// as a qualifier dimension it keeps its shape, and no undeclared dimension is
// accepted. Without the catalog ValidateFact is unchanged.
func TestValidateGraphFactUsesTheDeclaredDimensions(t *testing.T) {
	var graphValidator semreg.GraphFactValidator = New().(semreg.GraphFactValidator)
	for _, spec := range fields {
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

// TestPilotChargeAndDischargeCurrent requires the two pack currents in
// ampere with both bounds empty. Coefficients 1 and -1 are accepted, so a
// lower bound of zero refuses them. Each refusal names the production check
// it catches: pack id and pack version (the pack owner check), fact id (the
// field lookup), dimension id, a symbol on the text dimension, an extra
// dimension and an empty dimension list (the dimension check), another unit
// and a boolean value (the value check).
func TestPilotChargeAndDischargeCurrent(t *testing.T) {
	v := New()
	graphValidator := New().(semreg.GraphFactValidator)
	for _, id := range []semreg.DefinitionID{"storage.limit.charge_current", "storage.limit.discharge_current"} {
		spec, ok := findField(id)
		if !ok || spec.kind != semreg.ValueQuantity || spec.unit != "unit.ampere" || spec.dimension != "storage.dimension.pack" || spec.minimum != nil || spec.maximum != nil {
			t.Fatalf("%s = %+v", id, spec)
		}
		key, value := testKey(spec), testValue(spec)
		if err := v.ValidateFact(key, &value); err != nil {
			t.Fatal(id, err)
		}
		for _, coefficient := range []string{"1", "-1"} {
			accepted := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: coefficient}, Unit: spec.unit}}
			if err := v.ValidateFact(key, &accepted); err != nil {
				t.Fatalf("%s coefficient %s: %v", id, coefficient, err)
			}
		}
		wrongUnit := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.watt"}}
		require(t, v.ValidateFact(key, &wrongUnit), semreg.InvalidValue)
		bit := true
		wrongKind := semreg.Value{Kind: semreg.ValueBoolean, Boolean: &bit}
		require(t, v.ValidateFact(key, &wrongKind), semreg.InvalidValue)
		otherPack := key
		otherPack.PackID = "helianthus.pack.other"
		if v.ValidateFact(otherPack, &value) == nil {
			t.Fatalf("%s with another pack id accepted", id)
		}
		otherVersion := key
		otherVersion.PackVersion = "9.0.0"
		if got := semreg.ErrorIdentifier(v.ValidateFact(otherVersion, &value)); got != semreg.DefinitionOwnerMissing {
			t.Fatalf("%s pack version: %s", id, got)
		}
		otherFact := key
		otherFact.FactID = id + ".other"
		if got := semreg.ErrorIdentifier(v.ValidateFact(otherFact, &value)); got != semreg.DefinitionOwnerMissing {
			t.Fatalf("%s fact id: %s", id, got)
		}
		otherDimension := key
		otherDimension.Dimensions = []semreg.Dimension{{ID: "zz.dimension.subject", Value: key.Dimensions[0].Value}}
		require(t, v.ValidateFact(otherDimension, &value), semreg.InvalidValue)
		symbol := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: spec.dimension, Token: "active_current", Known: true}}
		symbolKey := key
		symbolKey.Dimensions = []semreg.Dimension{{ID: spec.dimension, Value: symbol}}
		require(t, v.ValidateFact(symbolKey, &value), semreg.InvalidValue)
		text := "extra"
		extra := key
		extra.Dimensions = []semreg.Dimension{
			key.Dimensions[0],
			{ID: "zz.dimension.extra", Value: semreg.Value{Kind: semreg.ValueText, Text: &text}},
		}
		require(t, v.ValidateFact(extra, &value), semreg.InvalidValue)
		empty := key
		empty.Dimensions = []semreg.Dimension{}
		require(t, v.ValidateFact(empty, &value), semreg.InvalidValue)
		identifier := "resource:pack:01"
		resourceKey := testKey(spec)
		resourceKey.Dimensions = []semreg.Dimension{{ID: spec.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &identifier}}}
		subject := semreg.GraphFactSubject{Disposition: semreg.GraphMember, Resolved: true, Dimension: spec.dimension, Kind: "resource.equipment"}
		if err := graphValidator.ValidateGraphFact(resourceKey, &value, subject); err != nil {
			t.Fatalf("%s graph: %v", id, err)
		}
		plain := "pack 01"
		plainKey := testKey(spec)
		plainKey.Dimensions = []semreg.Dimension{{ID: spec.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &plain}}}
		require(t, graphValidator.ValidateGraphFact(plainKey, &value, subject), semreg.InvalidValue)
		if err := v.ValidateFact(plainKey, &value); err != nil {
			t.Fatalf("%s without the catalog: %v", id, err)
		}
	}
}

func TestChargeDirectionIsSeparateFromOperating(t *testing.T) {
	direction, ok := findField("storage.status.charge_direction")
	if !ok || direction.dimension != "storage.dimension.pack" || !reflect.DeepEqual(direction.symbols, []string{"charging", "discharging", "idle", "starting"}) {
		t.Fatalf("charge direction: %+v", direction)
	}
	operating, ok := findField("storage.status.operating")
	if !ok || operating.dimension != "storage.dimension.pack" || !reflect.DeepEqual(operating.symbols, []string{"storage.status.operating.active", "storage.status.operating.idle", "storage.status.operating.standby"}) {
		t.Fatalf("operating: %+v", operating)
	}
	v := New()
	for _, token := range []string{"starting", "charging", "discharging"} {
		value := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: direction.id, Token: token, Known: true}}
		if err := v.ValidateFact(testKey(direction), &value); err != nil {
			t.Fatalf("%s: %v", token, err)
		}
		require(t, v.ValidateFact(testKey(operating), &value), semreg.InvalidValue)
	}
	for _, token := range []string{"active", "idle", "standby"} {
		value := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: operating.id, Token: token, Known: true}}
		if err := v.ValidateFact(testKey(operating), &value); err != nil {
			t.Fatalf("operating %s: %v", token, err)
		}
	}
	for _, token := range []string{"active", "standby"} {
		value := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: direction.id, Token: token, Known: true}}
		require(t, v.ValidateFact(testKey(direction), &value), semreg.InvalidValue)
	}
}
