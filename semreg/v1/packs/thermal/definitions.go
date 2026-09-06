// Package thermal implements the accepted thermal/HVAC capability pack.
package thermal

import semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"

const (
	packID      semreg.DefinitionID    = "helianthus.pack.thermal"
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
func bound(coefficient string, exponent10 int32) *decimalBound {
	return &decimalBound{coefficient, exponent10}
}

type fieldSpec struct {
	id               semreg.DefinitionID
	kind             semreg.ValueKind
	unit, dimension  semreg.DefinitionID
	minimum, maximum *decimalBound
	symbols          []string
}

var fields = []fieldSpec{
	{"thermal.action.state", semreg.ValueSymbol, "", "thermal.dimension.system", nil, nil, []string{"thermal.action.state.cooling", "thermal.action.state.dhw", "thermal.action.state.heating", "thermal.action.state.idle", "thermal.action.state.ventilating"}},
	{"thermal.demand.level", semreg.ValueQuantity, "unit.percent", "thermal.dimension.ratio", bound("0", 0), bound("1", 2), nil},
	{"thermal.measurement.air_temperature", semreg.ValueQuantity, "unit.celsius", "thermal.dimension.temperature", bound("-1", 2), bound("2", 2), nil},
	{"thermal.measurement.flow_rate", semreg.ValueQuantity, "unit.litre_per_minute", "thermal.dimension.volumetric_flow", bound("0", 0), bound("1", 4), nil},
	{"thermal.measurement.humidity_relative", semreg.ValueQuantity, "unit.percent", "thermal.dimension.ratio", bound("0", 0), bound("1", 2), nil},
	{"thermal.measurement.power", semreg.ValueQuantity, "unit.watt", "thermal.dimension.power", bound("-1", 6), bound("1", 6), nil},
	{"thermal.measurement.water_temperature", semreg.ValueQuantity, "unit.celsius", "thermal.dimension.temperature", bound("-1", 2), bound("2", 2), nil},
	{"thermal.mode.system", semreg.ValueSymbol, "", "thermal.dimension.system", nil, nil, []string{"thermal.mode.system.auto", "thermal.mode.system.cool", "thermal.mode.system.heat", "thermal.mode.system.off"}},
	{"thermal.mode.zone", semreg.ValueSymbol, "", "thermal.dimension.zone", nil, nil, []string{"thermal.mode.zone.auto", "thermal.mode.zone.comfort", "thermal.mode.zone.eco", "thermal.mode.zone.off"}},
	{"thermal.setpoint.dhw_temperature", semreg.ValueQuantity, "unit.celsius", "thermal.dimension.temperature", bound("-5", 1), bound("15", 1), nil},
	{"thermal.setpoint.temperature", semreg.ValueQuantity, "unit.celsius", "thermal.dimension.temperature", bound("-5", 1), bound("15", 1), nil},
	{"thermal.status.fault", semreg.ValueSymbol, "", "thermal.dimension.system", nil, nil, []string{"thermal.status.fault.clear", "thermal.status.fault.present"}},
	{"thermal.status.operation", semreg.ValueSymbol, "", "thermal.dimension.system", nil, nil, []string{"thermal.status.operation.active", "thermal.status.operation.idle"}},
}

var dimensions = map[semreg.DefinitionID]semreg.ValueKind{
	"thermal.dimension.system": semreg.ValueText, "thermal.dimension.zone": semreg.ValueText,
	"thermal.dimension.circuit": semreg.ValueText, "thermal.dimension.dhw": semreg.ValueText,
	"thermal.dimension.ventilation": semreg.ValueText,
}
var services = map[semreg.DefinitionID]semreg.DefinitionID{
	"thermal.service.system": "thermal.dimension.system", "thermal.service.zone": "thermal.dimension.zone",
	"thermal.service.circuit": "thermal.dimension.circuit", "thermal.service.dhw": "thermal.dimension.dhw",
	"thermal.service.ventilation": "thermal.dimension.ventilation",
}

type capabilitySpec struct {
	service     semreg.DefinitionID
	constraints []semreg.DefinitionID
}

var capabilities = map[semreg.DefinitionID]capabilitySpec{
	"thermal.capability.read.system": {"thermal.service.system", nil}, "thermal.capability.read.zone": {"thermal.service.zone", nil},
	"thermal.capability.read.circuit": {"thermal.service.circuit", nil}, "thermal.capability.read.dhw": {"thermal.service.dhw", nil},
	"thermal.capability.read.ventilation": {"thermal.service.ventilation", nil},
	"thermal.capability.set_temperature":  {"thermal.service.zone", []semreg.DefinitionID{"thermal.setpoint.temperature"}},
	"thermal.capability.set_mode":         {"thermal.service.system", []semreg.DefinitionID{"thermal.mode.system"}},
}

type operationSpec struct{ capability, argument, effect semreg.DefinitionID }

var operations = map[semreg.DefinitionID]operationSpec{
	"thermal.operation.set_temperature": {"thermal.capability.set_temperature", "thermal.setpoint.temperature", "thermal.effect.set_temperature"},
	"thermal.operation.set_mode":        {"thermal.capability.set_mode", "thermal.mode.system", "thermal.effect.set_mode"},
}

func definition(id semreg.DefinitionID) semreg.DefinitionRef {
	return semreg.DefinitionRef{Pack: pack, ID: id, Version: packVersion}
}
func index() semreg.DefinitionIndex {
	result := semreg.DefinitionIndex{Pack: pack, Fields: make([]semreg.DefinitionRef, 0, len(fields)), Services: make([]semreg.DefinitionRef, 0, len(services)), Capabilities: make([]semreg.DefinitionRef, 0, len(capabilities)), Operations: make([]semreg.DefinitionRef, 0, len(operations)), EffectRules: []semreg.DefinitionRef{definition("thermal.effect.set_mode"), definition("thermal.effect.set_temperature")}}
	for _, spec := range fields {
		result.Fields = append(result.Fields, definition(spec.id))
	}
	for _, id := range []semreg.DefinitionID{"thermal.service.circuit", "thermal.service.dhw", "thermal.service.system", "thermal.service.ventilation", "thermal.service.zone"} {
		result.Services = append(result.Services, definition(id))
	}
	for _, id := range []semreg.DefinitionID{"thermal.capability.read.circuit", "thermal.capability.read.dhw", "thermal.capability.read.system", "thermal.capability.read.ventilation", "thermal.capability.read.zone", "thermal.capability.set_mode", "thermal.capability.set_temperature"} {
		result.Capabilities = append(result.Capabilities, definition(id))
	}
	for _, id := range []semreg.DefinitionID{"thermal.operation.set_mode", "thermal.operation.set_temperature"} {
		result.Operations = append(result.Operations, definition(id))
	}
	return result
}
