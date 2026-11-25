// Package infrastructure implements the accepted read-only electrical
// infrastructure capability pack. It owns no native mapping, route, operation,
// or consumer API.
package infrastructure

import (
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

const (
	packID      semreg.DefinitionID    = "helianthus.pack.infrastructure"
	packVersion semreg.SemanticVersion = "1.0.0"
)

var pack = semreg.PackRef{ID: packID, Version: packVersion}

type decimalBound struct {
	coefficient string
	exponent10  int32
}

func (b decimalBound) decimal() semreg.Decimal {
	return semreg.Decimal{Coefficient: b.coefficient, Exponent10: b.exponent10}
}

type fieldSpec struct {
	id        semreg.DefinitionID
	kind      semreg.ValueKind
	unit      semreg.DefinitionID
	dimension semreg.DefinitionID
	minimum   *decimalBound
	maximum   *decimalBound
	symbols   []string
}

func bound(coefficient string, exponent10 int32) *decimalBound {
	return &decimalBound{coefficient: coefficient, exponent10: exponent10}
}

var fields = []fieldSpec{
	{id: "infrastructure.ac.active_power", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.phase", minimum: bound("-1", 7), maximum: bound("1", 7)},
	{id: "infrastructure.ac.apparent_power", kind: semreg.ValueQuantity, unit: "unit.volt_ampere", dimension: "infrastructure.dimension.phase", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.ac.current", kind: semreg.ValueQuantity, unit: "unit.ampere", dimension: "infrastructure.dimension.phase", minimum: bound("0", 0), maximum: bound("1", 3)},
	{id: "infrastructure.ac.frequency", kind: semreg.ValueQuantity, unit: "unit.hertz", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 3)},
	{id: "infrastructure.ac.power_factor", kind: semreg.ValueQuantity, unit: "unit.ratio", dimension: "infrastructure.dimension.grid_connection", minimum: bound("-1", 0), maximum: bound("1", 0)},
	{id: "infrastructure.ac.reactive_power", kind: semreg.ValueQuantity, unit: "unit.volt_ampere_reactive", dimension: "infrastructure.dimension.phase", minimum: bound("-1", 7), maximum: bound("1", 7)},
	{id: "infrastructure.ac.voltage", kind: semreg.ValueQuantity, unit: "unit.volt", dimension: "infrastructure.dimension.phase", minimum: bound("0", 0), maximum: bound("1", 3)},
	{id: "infrastructure.ac.voltage_line_line", kind: semreg.ValueQuantity, unit: "unit.volt", dimension: "infrastructure.dimension.line_pair", minimum: bound("0", 0), maximum: bound("1", 3)},
	{id: "infrastructure.accuracy.measured", kind: semreg.ValueBoolean, dimension: "infrastructure.dimension.meter"},
	{id: "infrastructure.accuracy.ratio_maximum", kind: semreg.ValueQuantity, unit: "unit.ratio", dimension: "infrastructure.dimension.meter"},
	{id: "infrastructure.energy.export", kind: semreg.ValueQuantity, unit: "unit.joule", dimension: "infrastructure.dimension.meter", minimum: bound("0", 0), maximum: bound("36", 17)},
	{id: "infrastructure.energy.import", kind: semreg.ValueQuantity, unit: "unit.joule", dimension: "infrastructure.dimension.meter", minimum: bound("0", 0), maximum: bound("36", 17)},
	{id: "infrastructure.energy.self_consumption", kind: semreg.ValueQuantity, unit: "unit.joule", dimension: "infrastructure.dimension.meter", minimum: bound("0", 0), maximum: bound("36", 17)},
	{id: "infrastructure.limit.fallback_power", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.apparent", kind: semreg.ValueQuantity, unit: "unit.volt_ampere", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.export_active", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.export_reactive", kind: semreg.ValueQuantity, unit: "unit.volt_ampere_reactive", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.import_active", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.import_reactive", kind: semreg.ValueQuantity, unit: "unit.volt_ampere_reactive", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.pcc", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("-1", 7), maximum: bound("1", 7)},
	{id: "infrastructure.status.breaker", kind: semreg.ValueSymbol, dimension: "infrastructure.dimension.circuit", symbols: []string{"infrastructure.status.breaker.closed", "infrastructure.status.breaker.open"}},
	{id: "infrastructure.status.connection", kind: semreg.ValueSymbol, dimension: "infrastructure.dimension.grid_connection", symbols: []string{"infrastructure.status.connection.connected", "infrastructure.status.connection.disconnected"}},
	{id: "infrastructure.status.fault", kind: semreg.ValueSymbol, dimension: "infrastructure.dimension.site", symbols: []string{"infrastructure.status.fault.clear", "infrastructure.status.fault.present"}},
	{id: "infrastructure.status.interlock", kind: semreg.ValueSymbol, dimension: "infrastructure.dimension.circuit", symbols: []string{"infrastructure.status.interlock.active", "infrastructure.status.interlock.clear"}},
	{id: "infrastructure.status.readiness", kind: semreg.ValueSymbol, dimension: "infrastructure.dimension.grid_connection", symbols: []string{"infrastructure.status.readiness.not_ready", "infrastructure.status.readiness.ready"}},
}

var dimensions = map[semreg.DefinitionID]semreg.ValueKind{
	"infrastructure.dimension.site":            semreg.ValueText,
	"infrastructure.dimension.grid_connection": semreg.ValueText,
	"infrastructure.dimension.feeder":          semreg.ValueText,
	"infrastructure.dimension.circuit":         semreg.ValueText,
	"infrastructure.dimension.phase":           semreg.ValueText,
	"infrastructure.dimension.meter":           semreg.ValueText,
	"infrastructure.dimension.line_pair":       semreg.ValueText,
}

var measurementSymbols = []string{
	"active_current", "active_power", "apparent_current", "apparent_energy", "apparent_power",
	"electrical_energy", "frequency", "neutral_current", "power_factor", "reactive_current",
	"reactive_energy", "reactive_power", "rms_current", "rms_power", "rms_voltage", "unspecified", "voltage",
}

var fieldQualifiers = map[semreg.DefinitionID][]semreg.DefinitionID{
	"infrastructure.accuracy.measured":      {"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.ratio_maximum": {"infrastructure.dimension.measurement_type"},
}

var qualifierSymbols = map[semreg.DefinitionID][]string{
	"infrastructure.dimension.measurement_type": measurementSymbols,
}

var services = map[semreg.DefinitionID]semreg.DefinitionID{
	"infrastructure.service.site":            "infrastructure.dimension.site",
	"infrastructure.service.grid_connection": "infrastructure.dimension.grid_connection",
	"infrastructure.service.feeder":          "infrastructure.dimension.feeder",
	"infrastructure.service.circuit":         "infrastructure.dimension.circuit",
	"infrastructure.service.phase":           "infrastructure.dimension.phase",
	"infrastructure.service.meter":           "infrastructure.dimension.meter",
	"infrastructure.service.line_pair":       "infrastructure.dimension.line_pair",
}

var capabilities = map[semreg.DefinitionID]semreg.DefinitionID{
	"infrastructure.capability.read.site":            "infrastructure.service.site",
	"infrastructure.capability.read.grid_connection": "infrastructure.service.grid_connection",
	"infrastructure.capability.read.feeder":          "infrastructure.service.feeder",
	"infrastructure.capability.read.circuit":         "infrastructure.service.circuit",
	"infrastructure.capability.read.phase":           "infrastructure.service.phase",
	"infrastructure.capability.read.meter":           "infrastructure.service.meter",
}

func definition(id semreg.DefinitionID) semreg.DefinitionRef {
	return semreg.DefinitionRef{Pack: pack, ID: id, Version: packVersion}
}

func sortedRefs[V any](table map[semreg.DefinitionID]V) []semreg.DefinitionRef {
	ids := make([]semreg.DefinitionID, 0, len(table))
	for id := range table {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	refs := make([]semreg.DefinitionRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, definition(id))
	}
	return refs
}

func index() semreg.DefinitionIndex {
	result := semreg.DefinitionIndex{
		Pack: pack, Services: sortedRefs(services), Capabilities: sortedRefs(capabilities),
		Operations: []semreg.DefinitionRef{}, EffectRules: []semreg.DefinitionRef{},
	}
	ids := make([]semreg.DefinitionID, 0, len(fields))
	for _, field := range fields {
		ids = append(ids, field.id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		result.Fields = append(result.Fields, definition(id))
	}
	if len(unionEvents) > 0 {
		events := append([]semreg.DefinitionID(nil), unionEvents...)
		sort.Slice(events, func(i, j int) bool { return events[i] < events[j] })
		for _, id := range events {
			result.Events = append(result.Events, definition(id))
		}
	}
	return result
}

// Metadata returns fresh metadata derived from this validator's private tables.
func Metadata() semreg.PackMetadata {
	metadata := semreg.PackMetadata{Pack: pack}
	units := map[semreg.DefinitionID]struct{}{}
	for _, spec := range fields {
		field := semreg.FieldMetadata{Ref: definition(spec.id), Dimension: definition(spec.dimension), ValueKind: spec.kind}
		if spec.unit != "" {
			unit := definition(spec.unit)
			field.CanonicalUnit, units[spec.unit] = &unit, struct{}{}
		}
		metadata.Fields = append(metadata.Fields, field)
	}
	for id, dimension := range services {
		metadata.Services = append(metadata.Services, semreg.ServiceMetadata{Ref: definition(id), FactKeyDimension: definition(dimension)})
	}
	for id, service := range capabilities {
		metadata.Capabilities = append(metadata.Capabilities, semreg.CapabilityMetadata{Ref: definition(id), Service: definition(service)})
	}
	for id := range units {
		metadata.Units = append(metadata.Units, definition(id))
	}
	sort.Slice(metadata.Units, func(i, j int) bool { return metadata.Units[i].ID < metadata.Units[j].ID })
	sort.Slice(metadata.Fields, func(i, j int) bool { return metadata.Fields[i].Ref.ID < metadata.Fields[j].Ref.ID })
	sort.Slice(metadata.Services, func(i, j int) bool { return metadata.Services[i].Ref.ID < metadata.Services[j].Ref.ID })
	if len(unionEvents) > 0 {
		for _, id := range unionEvents {
			metadata.Events = append(metadata.Events, definition(id))
		}
	}
	sort.Slice(metadata.Capabilities, func(i, j int) bool { return metadata.Capabilities[i].Ref.ID < metadata.Capabilities[j].Ref.ID })
	sort.Slice(metadata.Events, func(i, j int) bool { return metadata.Events[i].ID < metadata.Events[j].ID })
	return metadata
}
