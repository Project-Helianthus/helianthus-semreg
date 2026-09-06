// Package infrastructure implements the accepted read-only electrical
// infrastructure capability pack. It owns no native mapping, route, operation,
// or consumer API.
package infrastructure

import semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"

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
	{id: "infrastructure.energy.export", kind: semreg.ValueQuantity, unit: "unit.kilowatt_hour", dimension: "infrastructure.dimension.meter", minimum: bound("0", 0), maximum: bound("1", 12)},
	{id: "infrastructure.energy.import", kind: semreg.ValueQuantity, unit: "unit.kilowatt_hour", dimension: "infrastructure.dimension.meter", minimum: bound("0", 0), maximum: bound("1", 12)},
	{id: "infrastructure.power.apparent", kind: semreg.ValueQuantity, unit: "unit.volt_ampere", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.export_active", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.export_reactive", kind: semreg.ValueQuantity, unit: "unit.volt_ampere_reactive", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.import_active", kind: semreg.ValueQuantity, unit: "unit.watt", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
	{id: "infrastructure.power.import_reactive", kind: semreg.ValueQuantity, unit: "unit.volt_ampere_reactive", dimension: "infrastructure.dimension.grid_connection", minimum: bound("0", 0), maximum: bound("1", 7)},
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
}

var services = map[semreg.DefinitionID]semreg.DefinitionID{
	"infrastructure.service.site":            "infrastructure.dimension.site",
	"infrastructure.service.grid_connection": "infrastructure.dimension.grid_connection",
	"infrastructure.service.feeder":          "infrastructure.dimension.feeder",
	"infrastructure.service.circuit":         "infrastructure.dimension.circuit",
	"infrastructure.service.phase":           "infrastructure.dimension.phase",
	"infrastructure.service.meter":           "infrastructure.dimension.meter",
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

func index() semreg.DefinitionIndex {
	result := semreg.DefinitionIndex{
		Pack: pack, Fields: make([]semreg.DefinitionRef, 0, len(fields)),
		Services:     make([]semreg.DefinitionRef, 0, len(services)),
		Capabilities: make([]semreg.DefinitionRef, 0, len(capabilities)),
		Operations:   []semreg.DefinitionRef{}, EffectRules: []semreg.DefinitionRef{},
	}
	for _, field := range fields {
		result.Fields = append(result.Fields, definition(field.id))
	}
	for _, id := range []semreg.DefinitionID{
		"infrastructure.service.circuit", "infrastructure.service.feeder", "infrastructure.service.grid_connection",
		"infrastructure.service.meter", "infrastructure.service.phase", "infrastructure.service.site",
	} {
		result.Services = append(result.Services, definition(id))
	}
	for _, id := range []semreg.DefinitionID{
		"infrastructure.capability.read.circuit", "infrastructure.capability.read.feeder", "infrastructure.capability.read.grid_connection",
		"infrastructure.capability.read.meter", "infrastructure.capability.read.phase", "infrastructure.capability.read.site",
	} {
		result.Capabilities = append(result.Capabilities, definition(id))
	}
	return result
}
