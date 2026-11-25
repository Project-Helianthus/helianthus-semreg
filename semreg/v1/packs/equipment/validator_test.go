package equipment

import (
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

func TestUnionFieldsAcceptTheirUnitAndDimension(t *testing.T) {
	validator := New()
	for _, spec := range fields {
		key, value := unionKey(spec), unionValue(spec)
		if spec.kind == semreg.ValueQuantity && spec.unit == "" {
			invented := value
			invented.Quantity = &semreg.Quantity{Number: value.Quantity.Number, Unit: "unit.one"}
			if semreg.ErrorIdentifier(validator.ValidateFact(key, &invented)) != semreg.InvalidValue {
				t.Fatalf("%s invented unit accepted", spec.id)
			}
			continue
		}
		if err := validator.ValidateFact(key, &value); err != nil {
			t.Fatalf("%s valid: %v", spec.id, err)
		}
		other := key
		other.Dimensions = append([]semreg.Dimension{}, key.Dimensions...)
		for i := range other.Dimensions {
			if other.Dimensions[i].ID != spec.dimension {
				continue
			}
			other.Dimensions[i].ID = "zz.dimension.other"
		}
		if semreg.ErrorIdentifier(validator.ValidateFact(other, &value)) != semreg.InvalidValue {
			t.Fatalf("%s other dimension accepted", spec.id)
		}
		if spec.kind != semreg.ValueQuantity {
			continue
		}
		bad := value
		unit := semreg.DefinitionID("unit.watt")
		if spec.unit == unit {
			unit = "unit.volt"
		}
		bad.Quantity = &semreg.Quantity{Number: value.Quantity.Number, Unit: unit}
		if semreg.ErrorIdentifier(validator.ValidateFact(key, &bad)) != semreg.InvalidValue {
			t.Fatalf("%s other unit accepted", spec.id)
		}
	}
}

func unionKey(spec fieldSpec) semreg.FactKey {
	name := "resource-1"
	values := []semreg.Dimension{{ID: spec.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}
	for _, id := range fieldQualifiers[spec.id] {
		values = append(values, unionQualifier(id))
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: spec.id, Dimensions: values}
}

func unionQualifier(id semreg.DefinitionID) semreg.Dimension {
	kind := qualifierKinds[id]
	switch kind {
	case semreg.ValueSymbol:
		token := qualifierSymbols[id][0]
		if len(token) > len(id)+1 && token[:len(id)+1] == string(id)+"." {
			token = token[len(id)+1:]
		}
		return semreg.Dimension{ID: id, Value: semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: id, Token: token, Known: true}}}
	case semreg.ValueQuantity:
		return semreg.Dimension{ID: id, Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1", Exponent10: 0}, Unit: "unit.one"}}}
	default:
		text := "qualifier"
		return semreg.Dimension{ID: id, Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}
	}
}

func unionValue(spec fieldSpec) semreg.Value {
	switch spec.kind {
	case semreg.ValueQuantity:
		number := semreg.Decimal{Coefficient: "1", Exponent10: 0}
		if spec.minimum != nil {
			number = spec.minimum.decimal()
		}
		return semreg.Value{Kind: spec.kind, Quantity: &semreg.Quantity{Number: number, Unit: spec.unit}}
	case semreg.ValueBoolean:
		value := true
		return semreg.Value{Kind: spec.kind, Boolean: &value}
	case semreg.ValueText:
		value := "text"
		return semreg.Value{Kind: spec.kind, Text: &value}
	case semreg.ValueTime:
		return semreg.Value{Kind: spec.kind, Time: &semreg.TimePoint{UnixNanoseconds: "0", ClockID: "clock.utc", UncertaintyNS: "0"}}
	case semreg.ValueSymbols:
		token := spec.symbols[0]
		if len(token) > len(spec.id)+1 && token[:len(spec.id)+1] == string(spec.id)+"." {
			token = token[len(spec.id)+1:]
		}
		return semreg.Value{Kind: spec.kind, Symbols: []semreg.Symbol{{Namespace: spec.id, Token: token, Known: true}}}
	default:
		token := spec.symbols[0]
		if len(token) > len(spec.id)+1 && token[:len(spec.id)+1] == string(spec.id)+"." {
			token = token[len(spec.id)+1:]
		}
		return semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: spec.id, Token: token, Known: true}}
	}
}

func TestClassSubjectAcceptsDeclaredClasses(t *testing.T) {
	spec, ok := findField("equipment.diagnosis.operating_state")
	if !ok || spec.dimension != "equipment.dimension.unit" {
		t.Fatalf("operating state: %+v", spec)
	}
	text := "resource:unit:01"
	key := semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: spec.id, Dimensions: []semreg.Dimension{{ID: spec.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}}}
	value := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: spec.id, Token: "normal_operation", Known: true}}
	subject := semreg.GraphFactSubject{Disposition: semreg.GraphMember, Resolved: true, Dimension: spec.dimension, Classes: []semreg.AnchorClass{semreg.AnchorEquipment, semreg.AnchorComponent}}
	graphValidator := New().(semreg.GraphFactValidator)
	if err := graphValidator.ValidateGraphFact(key, &value, subject); err != nil {
		t.Fatal(err)
	}
	subject.Classes = nil
	if semreg.ErrorIdentifier(graphValidator.ValidateGraphFact(key, &value, subject)) != semreg.InvalidValue {
		t.Fatal("subject without a kind or class accepted")
	}
	subject.Kind = "resource.equipment"
	if err := graphValidator.ValidateGraphFact(key, &value, subject); err != nil {
		t.Fatal(err)
	}
}
