// Package storage implements the accepted storage/BMS capability pack.
package storage

import (
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

const (
	packID      semreg.DefinitionID    = "helianthus.pack.storage"
	packVersion semreg.SemanticVersion = "1.1.0"
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
	{"storage.state.soc", semreg.ValueQuantity, "unit.percent", "storage.dimension.pack", bound("0", 0), bound("1", 2), nil},
	{"storage.state.soh", semreg.ValueQuantity, "unit.percent", "storage.dimension.pack", bound("0", 0), bound("1", 2), nil},
	{"storage.pack.voltage", semreg.ValueQuantity, "unit.volt", "storage.dimension.pack", bound("0", 0), nil, nil},
	{"storage.pack.current", semreg.ValueQuantity, "unit.ampere", "storage.dimension.pack", nil, nil, nil},
	{"storage.pack.power", semreg.ValueQuantity, "unit.watt", "storage.dimension.pack", nil, nil, nil},
	{"storage.energy.charged", semreg.ValueQuantity, "unit.kilowatt_hour", "storage.dimension.pack", bound("0", 0), nil, nil},
	{"storage.energy.discharged", semreg.ValueQuantity, "unit.kilowatt_hour", "storage.dimension.pack", bound("0", 0), nil, nil},
	{"storage.capacity.charge", semreg.ValueQuantity, "unit.ampere_hour", "storage.dimension.pack", bound("0", 0), nil, nil},
	{"storage.capacity.discharge", semreg.ValueQuantity, "unit.ampere_hour", "storage.dimension.pack", bound("0", 0), nil, nil},
	{"storage.temperature.pack", semreg.ValueQuantity, "unit.celsius", "storage.dimension.pack", nil, nil, nil},
	{"storage.temperature.cell", semreg.ValueQuantity, "unit.celsius", "storage.dimension.cell", nil, nil, nil},
	{"storage.cell.voltage_minimum", semreg.ValueQuantity, "unit.volt", "storage.dimension.cell", bound("0", 0), nil, nil},
	{"storage.cell.voltage_maximum", semreg.ValueQuantity, "unit.volt", "storage.dimension.cell", bound("0", 0), nil, nil},
	{"storage.limit.charge_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", bound("0", 0), nil, nil},
	{"storage.limit.discharge_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", bound("0", 0), nil, nil},
	{"storage.status.operating", semreg.ValueSymbol, "", "storage.dimension.pack", nil, nil, []string{"storage.status.operating.active", "storage.status.operating.idle", "storage.status.operating.standby"}},
	{"storage.status.alarm", semreg.ValueSymbol, "", "storage.dimension.pack", nil, nil, []string{"storage.status.alarm.clear", "storage.status.alarm.present"}},
	{"storage.status.protection", semreg.ValueSymbol, "", "storage.dimension.pack", nil, nil, []string{"storage.status.protection.active", "storage.status.protection.clear"}},
	{"storage.status.warning", semreg.ValueSymbol, "", "storage.dimension.pack", nil, nil, []string{"storage.status.warning.clear", "storage.status.warning.present"}},
	{"storage.status.interlock", semreg.ValueSymbol, "", "storage.dimension.interface", nil, nil, []string{"storage.status.interlock.active", "storage.status.interlock.clear"}},
}

var services = map[semreg.DefinitionID]semreg.DefinitionID{
	"storage.service.system": "storage.dimension.system", "storage.service.battery": "storage.dimension.battery", "storage.service.pack": "storage.dimension.pack", "storage.service.module": "storage.dimension.module", "storage.service.cell": "storage.dimension.cell", "storage.service.string": "storage.dimension.string", "storage.service.interface": "storage.dimension.interface",
}

type capabilitySpec struct {
	service     semreg.DefinitionID
	constraints []semreg.DefinitionID
}

var capabilities = map[semreg.DefinitionID]capabilitySpec{
	"storage.capability.read.pack": {"storage.service.pack", nil}, "storage.capability.read.cell": {"storage.service.cell", nil}, "storage.capability.read.interface": {"storage.service.interface", nil},
	"storage.capability.set_charge_limit": {"storage.service.interface", []semreg.DefinitionID{"storage.limit.charge_power"}}, "storage.capability.set_discharge_limit": {"storage.service.interface", []semreg.DefinitionID{"storage.limit.discharge_power"}},
}

type operationSpec struct{ capability, argument, effect semreg.DefinitionID }

var operations = map[semreg.DefinitionID]operationSpec{
	"storage.operation.set_charge_limit":    {"storage.capability.set_charge_limit", "storage.limit.charge_power", "storage.effect.set_charge_limit"},
	"storage.operation.set_discharge_limit": {"storage.capability.set_discharge_limit", "storage.limit.discharge_power", "storage.effect.set_discharge_limit"},
}

func definition(id semreg.DefinitionID) semreg.DefinitionRef {
	return semreg.DefinitionRef{Pack: pack, ID: id, Version: packVersion}
}
func index() semreg.DefinitionIndex {
	r := semreg.DefinitionIndex{Pack: pack, Fields: make([]semreg.DefinitionRef, 0, len(fields)), Services: make([]semreg.DefinitionRef, 0, len(services)), Capabilities: make([]semreg.DefinitionRef, 0, len(capabilities)), Operations: []semreg.DefinitionRef{definition("storage.operation.set_charge_limit"), definition("storage.operation.set_discharge_limit")}, EffectRules: []semreg.DefinitionRef{definition("storage.effect.set_charge_limit"), definition("storage.effect.set_discharge_limit")}}
	for _, f := range fields {
		r.Fields = append(r.Fields, definition(f.id))
	}
	sort.Slice(r.Fields, func(i, j int) bool { return r.Fields[i].ID < r.Fields[j].ID })
	for _, id := range []semreg.DefinitionID{"storage.service.battery", "storage.service.cell", "storage.service.interface", "storage.service.module", "storage.service.pack", "storage.service.string", "storage.service.system"} {
		r.Services = append(r.Services, definition(id))
	}
	for _, id := range []semreg.DefinitionID{"storage.capability.read.cell", "storage.capability.read.interface", "storage.capability.read.pack", "storage.capability.set_charge_limit", "storage.capability.set_discharge_limit"} {
		r.Capabilities = append(r.Capabilities, definition(id))
	}
	return r
}

// Metadata returns fresh metadata derived from this validator's private tables.
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
		metadata.Operations = append(metadata.Operations, semreg.OperationMetadata{Ref: definition(id), Capability: definition(spec.capability), Service: definition(capabilities[spec.capability].service), Argument: definition(spec.argument), Effect: definition(spec.effect)})
	}
	for id := range units {
		metadata.Units = append(metadata.Units, definition(id))
	}
	sort.Slice(metadata.Units, func(i, j int) bool { return metadata.Units[i].ID < metadata.Units[j].ID })
	sort.Slice(metadata.Fields, func(i, j int) bool { return metadata.Fields[i].Ref.ID < metadata.Fields[j].Ref.ID })
	sort.Slice(metadata.Services, func(i, j int) bool { return metadata.Services[i].Ref.ID < metadata.Services[j].Ref.ID })
	sort.Slice(metadata.Capabilities, func(i, j int) bool { return metadata.Capabilities[i].Ref.ID < metadata.Capabilities[j].Ref.ID })
	sort.Slice(metadata.Operations, func(i, j int) bool { return metadata.Operations[i].Ref.ID < metadata.Operations[j].Ref.ID })
	return metadata
}
