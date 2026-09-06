// Package pv implements the accepted PV/inverter capability pack.
package pv

import (
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"sort"
)

const (
	packID      semreg.DefinitionID    = "helianthus.pack.pv"
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
func bound(c string, e int32) *decimalBound { return &decimalBound{c, e} }

type fieldSpec struct {
	id               semreg.DefinitionID
	kind             semreg.ValueKind
	unit, dimension  semreg.DefinitionID
	minimum, maximum *decimalBound
	symbols          []string
}

var fields = []fieldSpec{
	{"pv.dc.voltage", semreg.ValueQuantity, "unit.volt", "pv.dimension.input", bound("0", 0), bound("2", 3), nil}, {"pv.dc.current", semreg.ValueQuantity, "unit.ampere", "pv.dimension.input", bound("0", 0), bound("2", 3), nil}, {"pv.dc.power", semreg.ValueQuantity, "unit.watt", "pv.dimension.input", bound("0", 0), bound("1", 7), nil},
	{"pv.ac.voltage", semreg.ValueQuantity, "unit.volt", "pv.dimension.phase", bound("0", 0), bound("1", 3), nil}, {"pv.ac.current", semreg.ValueQuantity, "unit.ampere", "pv.dimension.phase", bound("0", 0), bound("2", 3), nil}, {"pv.ac.active_power", semreg.ValueQuantity, "unit.watt", "pv.dimension.phase", bound("-1", 7), bound("1", 7), nil},
	{"pv.ac.frequency", semreg.ValueQuantity, "unit.hertz", "pv.dimension.inverter", bound("0", 0), bound("1", 3), nil}, {"pv.ac.power_factor", semreg.ValueQuantity, "unit.ratio", "pv.dimension.inverter", bound("-1", 0), bound("1", 0), nil}, {"pv.energy.generated", semreg.ValueQuantity, "unit.kilowatt_hour", "pv.dimension.system", bound("0", 0), bound("1", 12), nil}, {"pv.temperature.inverter", semreg.ValueQuantity, "unit.celsius", "pv.dimension.inverter", bound("-5", 1), bound("2", 2), nil},
	{"pv.limit.active_power", semreg.ValueQuantity, "unit.watt", "pv.dimension.inverter", bound("0", 0), bound("1", 7), nil}, {"pv.limit.export_power", semreg.ValueQuantity, "unit.watt", "pv.dimension.system", bound("0", 0), bound("1", 7), nil},
	{"pv.status.operating", semreg.ValueSymbol, "", "pv.dimension.inverter", nil, nil, []string{"pv.status.operating.generating", "pv.status.operating.idle", "pv.status.operating.standby"}}, {"pv.status.derating", semreg.ValueSymbol, "", "pv.dimension.inverter", nil, nil, []string{"pv.status.derating.active", "pv.status.derating.clear"}}, {"pv.status.fault", semreg.ValueSymbol, "", "pv.dimension.inverter", nil, nil, []string{"pv.status.fault.clear", "pv.status.fault.present"}},
	{"pv.dc.aggregate_voltage", semreg.ValueQuantity, "unit.volt", "pv.dimension.inverter", bound("0", 0), bound("2", 3), nil}, {"pv.dc.aggregate_current", semreg.ValueQuantity, "unit.ampere", "pv.dimension.inverter", bound("0", 0), bound("2", 3), nil}, {"pv.dc.aggregate_power", semreg.ValueQuantity, "unit.watt", "pv.dimension.inverter", bound("0", 0), bound("1", 7), nil}, {"pv.ac.aggregate_voltage", semreg.ValueQuantity, "unit.volt", "pv.dimension.inverter", bound("0", 0), bound("1", 3), nil}, {"pv.ac.aggregate_current", semreg.ValueQuantity, "unit.ampere", "pv.dimension.inverter", bound("0", 0), bound("2", 3), nil}, {"pv.ac.aggregate_active_power", semreg.ValueQuantity, "unit.watt", "pv.dimension.inverter", bound("-1", 7), bound("1", 7), nil},
}
var dimensions = map[semreg.DefinitionID]semreg.ValueKind{"pv.dimension.system": semreg.ValueText, "pv.dimension.inverter": semreg.ValueText, "pv.dimension.array": semreg.ValueText, "pv.dimension.string": semreg.ValueText, "pv.dimension.input": semreg.ValueText, "pv.dimension.phase": semreg.ValueText}
var services = map[semreg.DefinitionID]semreg.DefinitionID{"pv.service.system": "pv.dimension.system", "pv.service.inverter": "pv.dimension.inverter", "pv.service.array": "pv.dimension.array", "pv.service.string": "pv.dimension.string", "pv.service.input": "pv.dimension.input", "pv.service.phase": "pv.dimension.phase"}

type capabilitySpec struct {
	service     semreg.DefinitionID
	constraints []semreg.DefinitionID
}

var capabilities = map[semreg.DefinitionID]capabilitySpec{"pv.capability.read.system": {"pv.service.system", nil}, "pv.capability.read.inverter": {"pv.service.inverter", nil}, "pv.capability.read.array": {"pv.service.array", nil}, "pv.capability.read.string": {"pv.service.string", nil}, "pv.capability.read.input": {"pv.service.input", nil}, "pv.capability.read.phase": {"pv.service.phase", nil}, "pv.capability.set_active_power_limit": {"pv.service.inverter", []semreg.DefinitionID{"pv.limit.active_power"}}, "pv.capability.set_export_limit": {"pv.service.system", []semreg.DefinitionID{"pv.limit.export_power"}}}

func definition(id semreg.DefinitionID) semreg.DefinitionRef {
	return semreg.DefinitionRef{Pack: pack, ID: id, Version: packVersion}
}
func index() semreg.DefinitionIndex {
	r := semreg.DefinitionIndex{Pack: pack, Fields: make([]semreg.DefinitionRef, 0, len(fields)), Services: make([]semreg.DefinitionRef, 0, len(services)), Capabilities: make([]semreg.DefinitionRef, 0, len(capabilities)), Operations: []semreg.DefinitionRef{definition("pv.operation.set_active_power_limit"), definition("pv.operation.set_export_limit")}, EffectRules: []semreg.DefinitionRef{definition("pv.effect.set_active_power_limit"), definition("pv.effect.set_export_limit")}}
	for _, f := range fields {
		r.Fields = append(r.Fields, definition(f.id))
	}
	sort.Slice(r.Fields, func(i, j int) bool { return r.Fields[i].ID < r.Fields[j].ID })
	for _, id := range []semreg.DefinitionID{"pv.service.array", "pv.service.input", "pv.service.inverter", "pv.service.phase", "pv.service.string", "pv.service.system"} {
		r.Services = append(r.Services, definition(id))
	}
	for _, id := range []semreg.DefinitionID{"pv.capability.read.array", "pv.capability.read.input", "pv.capability.read.inverter", "pv.capability.read.phase", "pv.capability.read.string", "pv.capability.read.system", "pv.capability.set_active_power_limit", "pv.capability.set_export_limit"} {
		r.Capabilities = append(r.Capabilities, definition(id))
	}
	return r
}
