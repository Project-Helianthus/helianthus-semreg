package thermal

import (
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestCanonicalThermalNamesAndNoAliases(t *testing.T) {
	registry, err := semreg.NewRegistry(New())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		old, canonical, unit, dimension semreg.DefinitionID
		min, max                        semreg.Decimal
	}{
		{"thermal.measurement.humidity_relative", "thermal.measurement.humidity", "unit.percent", "thermal.dimension.ratio", semreg.Decimal{Coefficient: "0"}, semreg.Decimal{Coefficient: "1", Exponent10: 2}},
		{"thermal.setpoint.temperature", "thermal.setpoint.temperature_setpoint", "unit.celsius", "thermal.dimension.temperature", semreg.Decimal{Coefficient: "-5", Exponent10: 1}, semreg.Decimal{Coefficient: "15", Exponent10: 1}},
		{"thermal.setpoint.dhw_temperature", "thermal.setpoint.dhw_temperature_setpoint", "unit.celsius", "thermal.dimension.temperature", semreg.Decimal{Coefficient: "-5", Exponent10: 1}, semreg.Decimal{Coefficient: "15", Exponent10: 1}},
	} {
		t.Run(string(tc.canonical), func(t *testing.T) {
			spec, ok := findField(tc.canonical)
			if !ok || spec.kind != semreg.ValueQuantity || spec.unit != tc.unit || spec.dimension != tc.dimension || spec.minimum.decimal() != tc.min || spec.maximum.decimal() != tc.max {
				t.Fatalf("canonical field contract: %+v", spec)
			}
			if _, ok := findField(tc.old); ok {
				t.Fatal("old alias found")
			}
			if _, err := registry.Definition(semreg.DefinitionField, definition(tc.canonical)); err != nil {
				t.Fatal(err)
			}
			if _, err := registry.Definition(semreg.DefinitionField, definition(tc.old)); err == nil {
				t.Fatal("registry resolved old alias")
			}
			key, value := validKey(spec), validValue(spec)
			key.FactID = tc.old
			requireID(t, New().ValidateFact(key, &value), semreg.DefinitionOwnerMissing)
			requireID(t, New().ValidateField(definition(tc.old), semreg.TypedField{ID: tc.old, Value: value}), semreg.DefinitionOwnerMissing)
			for _, mutate := range []struct {
				name  string
				apply func(*semreg.DefinitionRef)
			}{
				{"pack_id", func(r *semreg.DefinitionRef) { r.Pack.ID = "other.pack" }},
				{"pack_version", func(r *semreg.DefinitionRef) { r.Pack.Version = "1.0.1" }},
				{"definition_id", func(r *semreg.DefinitionRef) { r.ID = tc.old }},
				{"definition_version", func(r *semreg.DefinitionRef) { r.Version = "1.0.1" }},
			} {
				t.Run(mutate.name, func(t *testing.T) {
					ref := definition(tc.canonical)
					mutate.apply(&ref)
					if _, err := registry.Definition(semreg.DefinitionField, ref); err == nil {
						t.Fatal("registry ignored definition identity axis")
					}
					if err := New().ValidateField(ref, semreg.TypedField{ID: tc.canonical, Value: value}); err == nil {
						t.Fatal("validator ignored definition identity axis")
					}
				})
			}
		})
	}
	seen := map[semreg.DefinitionID]bool{}
	for _, ref := range New().Definitions().Fields {
		if seen[ref.ID] {
			t.Fatal("colliding field identity")
		}
		seen[ref.ID] = true
	}
}

// Each mutation catches removal of that axis from EvaluateReadback's exact FactKey comparison.
func TestCanonicalSetpointReadbackIdentityAxes(t *testing.T) {
	intent := validIntent("thermal.operation.set_temperature")
	if intent.Arguments[0].ID != "thermal.setpoint.temperature_setpoint" || intent.ExpectedEffect.Fact.FactID != "thermal.setpoint.temperature_setpoint" {
		t.Fatal("operation references did not adopt canonical name")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*semreg.FactKey)
	}{
		{"pack_id", func(k *semreg.FactKey) { k.PackID = "other.pack" }},
		{"pack_version", func(k *semreg.FactKey) { k.PackVersion = "1.0.1" }},
		{"fact_id", func(k *semreg.FactKey) { k.FactID = "thermal.setpoint.dhw_temperature_setpoint" }},
		{"dimension_id", func(k *semreg.FactKey) { k.Dimensions[0].ID = "thermal.dimension.zone" }},
		{"dimension_value", func(k *semreg.FactKey) { other := "other"; k.Dimensions[0].Value.Text = &other }},
		{"dimension_kind", func(k *semreg.FactKey) {
			v := true
			k.Dimensions[0].Value = semreg.Value{Kind: semreg.ValueBoolean, Boolean: &v}
		}},
		{"dimension_cardinality", func(k *semreg.FactKey) { k.Dimensions = append(k.Dimensions, k.Dimensions[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := intent.ExpectedEffect.Fact
			key.Dimensions = append([]semreg.Dimension(nil), key.Dimensions...)
			candidate := validCandidate(key, intent.ExpectedEffect.Expected)
			if got, err := New().(operation.OperationPackValidator).EvaluateReadback(intent, candidate); err != nil || got != operation.ReadbackConfirms {
				t.Fatalf("baseline: %s %v", got, err)
			}
			tc.mutate(&candidate.Key)
			if got, err := New().(operation.OperationPackValidator).EvaluateReadback(intent, candidate); err != nil || got != operation.ReadbackInconclusive {
				t.Fatalf("cross-identity readback: %s %v", got, err)
			}
		})
	}
	old := intent
	old.Arguments = append([]semreg.TypedField(nil), intent.Arguments...)
	old.Arguments[0].ID = "thermal.setpoint.temperature"
	if err := New().(operation.OperationPackValidator).ValidateIntent(old); err == nil {
		t.Fatal("old operation argument admitted")
	}
	old = intent
	old.ExpectedEffect.Fact.FactID = "thermal.setpoint.temperature"
	if err := New().(operation.OperationPackValidator).ValidateIntent(old); err == nil {
		t.Fatal("old effect key admitted")
	}
}

// Registration must reject a duplicated canonical field, rather than silently
// shadow it when an upstream table accidentally collides during the rename.
type namingCollisionValidator struct{ semreg.PackValidator }

func (v namingCollisionValidator) Definitions() semreg.DefinitionIndex {
	index := v.PackValidator.Definitions()
	index.Fields = append(index.Fields, definition("thermal.measurement.humidity"))
	sort.Slice(index.Fields, func(i, j int) bool { return index.Fields[i].ID < index.Fields[j].ID })
	return index
}

func TestCanonicalThermalRegistrationRejectsCollision(t *testing.T) {
	_, err := semreg.NewRegistry(namingCollisionValidator{New()})
	requireID(t, err, semreg.DefinitionOwnerConflict)
}
