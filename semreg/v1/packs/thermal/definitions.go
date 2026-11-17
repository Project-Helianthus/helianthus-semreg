// Package thermal implements the accepted thermal/HVAC capability pack.
package thermal

import (
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

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

// fieldSpec is one thermal field. dimension is the field's only fact
// dimension: the resource dimension of its subject.
type fieldSpec struct {
	id               semreg.DefinitionID
	kind             semreg.ValueKind
	unit, dimension  semreg.DefinitionID
	minimum, maximum *decimalBound
	symbols          []string
}

// The canonical units are SI: kelvin for temperatures, a 0 to 1 ratio for
// dimensionless quantities, cubic metre per second, pascal, reciprocal second,
// watt and joule.
const (
	kelvin               semreg.DefinitionID = "unit.kelvin"
	ratio                semreg.DefinitionID = "unit.ratio"
	cubicMetrePerSecond  semreg.DefinitionID = "unit.cubic_metre_per_second"
	pascal               semreg.DefinitionID = "unit.pascal"
	reciprocalSecond     semreg.DefinitionID = "unit.reciprocal_second"
	watt                 semreg.DefinitionID = "unit.watt"
	joule                semreg.DefinitionID = "unit.joule"
	systemDimension      semreg.DefinitionID = "thermal.dimension.system"
	zoneDimension        semreg.DefinitionID = "thermal.dimension.zone"
	circuitDimension     semreg.DefinitionID = "thermal.dimension.circuit"
	dhwDimension         semreg.DefinitionID = "thermal.dimension.dhw"
	ventilationDimension semreg.DefinitionID = "thermal.dimension.ventilation"
	generatorDimension   semreg.DefinitionID = "thermal.dimension.heat_generator"
	storeDimension       semreg.DefinitionID = "thermal.dimension.thermal_store"
	terminalDimension    semreg.DefinitionID = "thermal.dimension.terminal_unit"
	refrigerantDimension semreg.DefinitionID = "thermal.dimension.refrigerant_circuit"
	compressorDimension  semreg.DefinitionID = "thermal.dimension.compressor"
)

// Temperature bounds in kelvin: 173.15 to 473.15 for a measurement and 223.15
// to 423.15 for a setpoint.
var (
	temperatureMinimum, temperatureMaximum = bound("17315", -2), bound("47315", -2)
	setpointMinimum, setpointMaximum       = bound("22315", -2), bound("42315", -2)
)

var fields = []fieldSpec{
	{"thermal.action.state", semreg.ValueSymbol, "", systemDimension, nil, nil, []string{"thermal.action.state.cooling", "thermal.action.state.dhw", "thermal.action.state.heating", "thermal.action.state.idle", "thermal.action.state.ventilating"}},
	{"thermal.demand.level", semreg.ValueQuantity, ratio, ventilationDimension, bound("0", 0), bound("1", 0), nil},
	{"thermal.energy.heat_output", semreg.ValueQuantity, joule, generatorDimension, bound("0", 0), bound("36", 17), nil},
	{"thermal.measurement.air_temperature", semreg.ValueQuantity, kelvin, zoneDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.circuit_flow_temperature", semreg.ValueQuantity, kelvin, circuitDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.compressor_speed", semreg.ValueQuantity, reciprocalSecond, compressorDimension, bound("0", 0), bound("1", 3), nil},
	{"thermal.measurement.dhw_temperature", semreg.ValueQuantity, kelvin, dhwDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.flow_rate", semreg.ValueQuantity, cubicMetrePerSecond, circuitDimension, bound("0", 0), bound("2", -1), nil},
	{"thermal.measurement.heat_generator_flow_temperature", semreg.ValueQuantity, kelvin, generatorDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.heat_generator_return_temperature", semreg.ValueQuantity, kelvin, generatorDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.humidity", semreg.ValueQuantity, ratio, zoneDimension, bound("0", 0), bound("1", 0), nil},
	{"thermal.measurement.outdoor_air_temperature", semreg.ValueQuantity, kelvin, systemDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.refrigerant_condensing_temperature", semreg.ValueQuantity, kelvin, refrigerantDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.refrigerant_evaporating_temperature", semreg.ValueQuantity, kelvin, refrigerantDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.refrigerant_high_side_pressure", semreg.ValueQuantity, pascal, refrigerantDimension, bound("0", 0), bound("1", 8), nil},
	{"thermal.measurement.refrigerant_low_side_pressure", semreg.ValueQuantity, pascal, refrigerantDimension, bound("0", 0), bound("1", 8), nil},
	{"thermal.measurement.store_temperature", semreg.ValueQuantity, kelvin, storeDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.system_flow_temperature", semreg.ValueQuantity, kelvin, systemDimension, temperatureMinimum, temperatureMaximum, nil},
	{"thermal.measurement.thermal_power", semreg.ValueQuantity, watt, generatorDimension, bound("-1", 6), bound("1", 6), nil},
	{"thermal.measurement.water_pressure", semreg.ValueQuantity, pascal, generatorDimension, bound("-101325", 0), bound("1", 7), nil},
	{"thermal.mode.system", semreg.ValueSymbol, "", systemDimension, nil, nil, []string{"thermal.mode.system.auto", "thermal.mode.system.cool", "thermal.mode.system.heat", "thermal.mode.system.off"}},
	{"thermal.mode.terminal_unit", semreg.ValueSymbol, "", terminalDimension, nil, nil, []string{"thermal.mode.terminal_unit.auto", "thermal.mode.terminal_unit.cool", "thermal.mode.terminal_unit.dry", "thermal.mode.terminal_unit.fan_only", "thermal.mode.terminal_unit.heat", "thermal.mode.terminal_unit.off"}},
	{"thermal.mode.zone", semreg.ValueSymbol, "", zoneDimension, nil, nil, []string{"thermal.mode.zone.auto", "thermal.mode.zone.comfort", "thermal.mode.zone.eco", "thermal.mode.zone.off"}},
	{"thermal.setpoint.dhw_temperature_setpoint", semreg.ValueQuantity, kelvin, dhwDimension, setpointMinimum, setpointMaximum, nil},
	{"thermal.setpoint.temperature_setpoint", semreg.ValueQuantity, kelvin, zoneDimension, setpointMinimum, setpointMaximum, nil},
	{"thermal.status.fault", semreg.ValueSymbol, "", systemDimension, nil, nil, []string{"thermal.status.fault.clear", "thermal.status.fault.present"}},
	{"thermal.status.heat_generator_fault", semreg.ValueSymbol, "", generatorDimension, nil, nil, []string{"thermal.status.heat_generator_fault.clear", "thermal.status.heat_generator_fault.present"}},
	{"thermal.status.heat_generator_operation", semreg.ValueSymbol, "", generatorDimension, nil, nil, []string{"thermal.status.heat_generator_operation.active", "thermal.status.heat_generator_operation.idle"}},
	{"thermal.status.maintenance", semreg.ValueSymbol, "", systemDimension, nil, nil, []string{"thermal.status.maintenance.clear", "thermal.status.maintenance.due"}},
	{"thermal.status.operation", semreg.ValueSymbol, "", systemDimension, nil, nil, []string{"thermal.status.operation.active", "thermal.status.operation.idle"}},
	{"thermal.status.terminal_unit_fault", semreg.ValueSymbol, "", terminalDimension, nil, nil, []string{"thermal.status.terminal_unit_fault.clear", "thermal.status.terminal_unit_fault.present"}},
	{"thermal.status.terminal_unit_operation", semreg.ValueSymbol, "", terminalDimension, nil, nil, []string{"thermal.status.terminal_unit_operation.active", "thermal.status.terminal_unit_operation.idle"}},
}

// dimensions are the ten resource dimensions; each value names one resource.
var dimensions = map[semreg.DefinitionID]semreg.ValueKind{
	systemDimension: semreg.ValueText, zoneDimension: semreg.ValueText,
	circuitDimension: semreg.ValueText, dhwDimension: semreg.ValueText,
	ventilationDimension: semreg.ValueText, generatorDimension: semreg.ValueText,
	storeDimension: semreg.ValueText, terminalDimension: semreg.ValueText,
	refrigerantDimension: semreg.ValueText, compressorDimension: semreg.ValueText,
}
var services = map[semreg.DefinitionID]semreg.DefinitionID{
	"thermal.service.system": systemDimension, "thermal.service.zone": zoneDimension,
	"thermal.service.circuit": circuitDimension, "thermal.service.dhw": dhwDimension,
	"thermal.service.ventilation": ventilationDimension, "thermal.service.heat_generator": generatorDimension,
	"thermal.service.thermal_store": storeDimension, "thermal.service.terminal_unit": terminalDimension,
	"thermal.service.refrigerant_circuit": refrigerantDimension, "thermal.service.compressor": compressorDimension,
}

type capabilitySpec struct {
	service     semreg.DefinitionID
	constraints []semreg.DefinitionID
}

var capabilities = map[semreg.DefinitionID]capabilitySpec{
	"thermal.capability.read.system": {"thermal.service.system", nil}, "thermal.capability.read.zone": {"thermal.service.zone", nil},
	"thermal.capability.read.circuit": {"thermal.service.circuit", nil}, "thermal.capability.read.dhw": {"thermal.service.dhw", nil},
	"thermal.capability.read.ventilation":         {"thermal.service.ventilation", nil},
	"thermal.capability.read.heat_generator":      {"thermal.service.heat_generator", nil},
	"thermal.capability.read.thermal_store":       {"thermal.service.thermal_store", nil},
	"thermal.capability.read.terminal_unit":       {"thermal.service.terminal_unit", nil},
	"thermal.capability.read.refrigerant_circuit": {"thermal.service.refrigerant_circuit", nil},
	"thermal.capability.read.compressor":          {"thermal.service.compressor", nil},
	"thermal.capability.set_temperature":          {"thermal.service.zone", []semreg.DefinitionID{"thermal.setpoint.temperature_setpoint"}},
	"thermal.capability.set_mode":                 {"thermal.service.system", []semreg.DefinitionID{"thermal.mode.system"}},
}

type operationSpec struct{ capability, argument, effect semreg.DefinitionID }

var operations = map[semreg.DefinitionID]operationSpec{
	"thermal.operation.set_temperature": {"thermal.capability.set_temperature", "thermal.setpoint.temperature_setpoint", "thermal.effect.set_temperature"},
	"thermal.operation.set_mode":        {"thermal.capability.set_mode", "thermal.mode.system", "thermal.effect.set_mode"},
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
	result := semreg.DefinitionIndex{Pack: pack, Fields: make([]semreg.DefinitionRef, 0, len(fields)), Services: sortedRefs(services), Capabilities: sortedRefs(capabilities), Operations: sortedRefs(operations), EffectRules: []semreg.DefinitionRef{definition("thermal.effect.set_mode"), definition("thermal.effect.set_temperature")}}
	for _, spec := range fields {
		result.Fields = append(result.Fields, definition(spec.id))
	}
	return result
}

// Metadata returns a fresh description derived from the validator's private
// field, service, capability, and operation tables.
func Metadata() semreg.PackMetadata {
	metadata := semreg.PackMetadata{Pack: pack}
	units := map[semreg.DefinitionID]struct{}{}
	for _, spec := range fields {
		field := semreg.FieldMetadata{Ref: definition(spec.id), Dimension: definition(spec.dimension)}
		if spec.unit != "" {
			unit := definition(spec.unit)
			field.CanonicalUnit, units[spec.unit] = &unit, struct{}{}
		}
		metadata.Fields = append(metadata.Fields, field)
	}
	for id, dimension := range services {
		metadata.Services = append(metadata.Services, semreg.ServiceMetadata{Ref: definition(id), FactKeyDimension: definition(dimension)})
	}
	for id, spec := range capabilities {
		metadata.Capabilities = append(metadata.Capabilities, semreg.CapabilityMetadata{Ref: definition(id), Service: definition(spec.service)})
	}
	for id, spec := range operations {
		service := capabilities[spec.capability].service
		metadata.Operations = append(metadata.Operations, semreg.OperationMetadata{Ref: definition(id), Capability: definition(spec.capability), Service: definition(service), Argument: definition(spec.argument), Effect: definition(spec.effect)})
	}
	for id := range units {
		metadata.Units = append(metadata.Units, definition(id))
	}
	sortMetadata(&metadata)
	return metadata
}

func sortMetadata(metadata *semreg.PackMetadata) {
	sort.Slice(metadata.Units, func(i, j int) bool { return metadata.Units[i].ID < metadata.Units[j].ID })
	sort.Slice(metadata.Fields, func(i, j int) bool { return metadata.Fields[i].Ref.ID < metadata.Fields[j].Ref.ID })
	sort.Slice(metadata.Services, func(i, j int) bool { return metadata.Services[i].Ref.ID < metadata.Services[j].Ref.ID })
	sort.Slice(metadata.Capabilities, func(i, j int) bool { return metadata.Capabilities[i].Ref.ID < metadata.Capabilities[j].Ref.ID })
	sort.Slice(metadata.Operations, func(i, j int) bool { return metadata.Operations[i].Ref.ID < metadata.Operations[j].Ref.ID })
}
