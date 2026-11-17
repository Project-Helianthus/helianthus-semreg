package pv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestPVFixtureFieldsMatchDefinitions compares every field of the copied
// contract with the validator table: identifier, kind, unit, dimension,
// minimum, maximum and symbols.
func TestPVFixtureFieldsMatchDefinitions(t *testing.T) {
	pvCheckFields(t, "pv-inverter-pack-vectors.json", true, "catalog", "definitions", "fields")
}

// TestPVFieldComparisonCatchesEveryProperty changes one property of a
// table entry at a time and requires the comparison to name it.
func TestPVFieldComparisonCatchesEveryProperty(t *testing.T) {
	entries := pvFixtureFields(t, "pv-inverter-pack-vectors.json", "catalog", "definitions", "fields")
	var spec fieldSpec
	for _, candidate := range fields {
		if candidate.id == "pv.temperature.inverter" {
			spec = candidate
		}
	}
	if spec.minimum == nil || spec.maximum == nil {
		t.Fatalf("probe %s needs both bounds", spec.id)
	}
	entry := pvFieldEntry(t, entries, string(spec.id))
	if got := pvFieldMismatch(entry, spec, true); got != "" {
		t.Fatalf("baseline %s differs in %s", spec.id, got)
	}
	copyBound := func(b *decimalBound) *decimalBound {
		if b == nil {
			return nil
		}
		c := *b
		return &c
	}
	for _, change := range []struct {
		property string
		mutate   func(*fieldSpec)
	}{
		{"id", func(s *fieldSpec) { s.id += "_other" }},
		{"kind", func(s *fieldSpec) { s.kind = "symbol" }},
		{"unit", func(s *fieldSpec) { s.unit = "unit.other" }},
		{"dimension", func(s *fieldSpec) { s.dimension += "_other" }},
		{"minimum", func(s *fieldSpec) { s.minimum.coefficient += "1" }},
		{"minimum", func(s *fieldSpec) { s.minimum.exponent10++ }},
		{"minimum", func(s *fieldSpec) { s.minimum = nil }},
		{"maximum", func(s *fieldSpec) { s.maximum.coefficient += "1" }},
		{"maximum", func(s *fieldSpec) { s.maximum.exponent10++ }},
		{"maximum", func(s *fieldSpec) { s.maximum = nil }},
		{"symbols", func(s *fieldSpec) { s.symbols = []string{string(s.id) + ".other"} }},
	} {
		changed := spec
		changed.minimum, changed.maximum = copyBound(spec.minimum), copyBound(spec.maximum)
		change.mutate(&changed)
		if got := pvFieldMismatch(entry, changed, true); got != change.property {
			t.Errorf("changed %s reported as %q", change.property, got)
		}
	}
}

// pvFieldMismatch names the first property in which a fixture field entry
// differs from spec, or returns "" when they agree. Bounds are compared only
// when withBounds is set.
func pvFieldMismatch(entry map[string]any, spec fieldSpec, withBounds bool) string {
	var unit any
	if spec.unit != "" {
		unit = string(spec.unit)
	}
	bounds, _ := entry["bounds"].(map[string]any)
	switch {
	case entry["id"] != string(spec.id):
		return "id"
	case entry["kind"] != string(spec.kind):
		return "kind"
	case entry["unit"] != unit:
		return "unit"
	case entry["dimension"] != string(spec.dimension):
		return "dimension"
	case withBounds && !pvSameBound(bounds["minimum"], spec.minimum):
		return "minimum"
	case withBounds && !pvSameBound(bounds["maximum"], spec.maximum):
		return "maximum"
	case !pvSameSymbols(entry["symbols"], spec.symbols):
		return "symbols"
	}
	return ""
}

func pvSameBound(value any, want *decimalBound) bool {
	if want == nil {
		return value == nil
	}
	got, ok := value.(map[string]any)
	return ok && len(got) == 2 && got["coefficient"] == want.coefficient && got["exponent10"] == float64(want.exponent10)
}

func pvSameSymbols(value any, want []string) bool {
	if want == nil {
		return value == nil
	}
	got, ok := value.([]any)
	if !ok || len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func pvFieldEntry(t *testing.T, entries []map[string]any, id string) map[string]any {
	t.Helper()
	for _, entry := range entries {
		if entry["id"] == id {
			return entry
		}
	}
	t.Fatalf("fixture lacks field %s", id)
	return nil
}

// pvCheckFields requires the field list at path in the named fixture to
// hold exactly the validator table.
func pvCheckFields(t *testing.T, name string, withBounds bool, path ...string) {
	t.Helper()
	entries := pvFixtureFields(t, name, path...)
	if len(entries) != len(fields) {
		t.Fatalf("%s: %d fields, want %d", name, len(entries), len(fields))
	}
	for _, spec := range fields {
		if got := pvFieldMismatch(pvFieldEntry(t, entries, string(spec.id)), spec, withBounds); got != "" {
			t.Errorf("%s: %s differs in %s", name, spec.id, got)
		}
	}
}

// pvFixtureFields returns the field entries at path in the named fixture.
func pvFixtureFields(t *testing.T, name string, path ...string) []map[string]any {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture path")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	var current any
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("%s: %s is not inside an object", name, key)
		}
		current = object[key]
	}
	list, ok := current.([]any)
	if !ok {
		t.Fatalf("%s: fields are not a list", name)
	}
	entries := make([]map[string]any, 0, len(list))
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s: field entry %#v", name, item)
		}
		entries = append(entries, entry)
	}
	return entries
}
