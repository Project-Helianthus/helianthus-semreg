package equipment

import (
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

const (
	packID      semreg.DefinitionID    = "helianthus.pack.equipment"
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

type capabilitySpec struct {
	service     semreg.DefinitionID
	constraints []semreg.DefinitionID
}

type operationSpec struct{ capability, argument, effect semreg.DefinitionID }

var fields = []fieldSpec{
	{"equipment.diagnosis.last_error_code", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.diagnosis.operating_state", semreg.ValueSymbol, "", "equipment.dimension.unit", nil, nil, []string{"normal_operation", "standby", "failure", "service_needed", "override_detected", "in_alarm", "not_reachable", "finished", "temporarily_not_ready", "off"}},
	{"equipment.diagnosis.operating_time", semreg.ValueQuantity, "unit.second", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.diagnosis.vendor_state_code", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.brand_name", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.hardware_revision", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.hardware_version", semreg.ValueQuantity, "unit.one", "equipment.dimension.unit", bound("0", 0), bound("65535", 0), nil},
	{"equipment.identification.manufacturing_date", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.part_number", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.power_source_kind", semreg.ValueSymbol, "", "equipment.dimension.unit", nil, nil, []string{"unknown", "mains_single_phase", "mains_three_phase", "battery", "dc"}},
	{"equipment.identification.product_code", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.product_description", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.product_label", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.product_name", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.product_url", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.serial_number", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.software_revision", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.software_version", semreg.ValueQuantity, "unit.one", "equipment.dimension.unit", bound("0", 0), bound("4294967295", 0), nil},
	{"equipment.identification.vendor_code", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.identification.vendor_name", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.label.description", semreg.ValueText, "", "equipment.dimension.resource", nil, nil, nil},
	{"equipment.label.name", semreg.ValueText, "", "equipment.dimension.resource", nil, nil, nil},
	{"equipment.message.kind", semreg.ValueSymbol, "", "equipment.dimension.unit", nil, nil, []string{"logging", "information", "warning", "alarm", "emergency", "obsolete"}},
	{"equipment.message.text", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.message.time", semreg.ValueTime, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_ansi_designation", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_capacity", semreg.ValueQuantity, "unit.ampere_hour", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_charge_faults", semreg.ValueSymbols, "", "equipment.dimension.unit", nil, nil, []string{"unspecified", "ambient_too_hot", "ambient_too_cold", "battery_too_hot", "battery_too_cold", "battery_absent", "battery_over_voltage", "battery_under_voltage", "charger_over_voltage", "charger_under_voltage", "safety_timeout", "no_fault"}},
	{"equipment.power.battery_charge_level", semreg.ValueSymbol, "", "equipment.dimension.unit", nil, nil, []string{"ok", "warning", "critical"}},
	{"equipment.power.battery_charge_ratio", semreg.ValueQuantity, "unit.ratio", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_charge_state", semreg.ValueSymbol, "", "equipment.dimension.unit", nil, nil, []string{"unknown", "charging", "full", "not_charging"}},
	{"equipment.power.battery_charging_current", semreg.ValueQuantity, "unit.ampere", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_chemistry", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_common_designation", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_faults", semreg.ValueSymbols, "", "equipment.dimension.unit", nil, nil, []string{"unspecified", "over_temperature", "under_temperature", "no_fault"}},
	{"equipment.power.battery_functional_while_charging", semreg.ValueBoolean, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_iec_designation", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_present", semreg.ValueBoolean, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_quantity", semreg.ValueQuantity, "unit.one", "equipment.dimension.unit", bound("0", 0), bound("255", 0), nil},
	{"equipment.power.battery_replaceability", semreg.ValueSymbol, "", "equipment.dimension.unit", nil, nil, []string{"unspecified", "not_replaceable", "user_replaceable", "factory_replaceable"}},
	{"equipment.power.battery_replacement_description", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_replacement_needed", semreg.ValueBoolean, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_time_remaining", semreg.ValueQuantity, "unit.second", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_time_to_full", semreg.ValueQuantity, "unit.second", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.battery_voltage", semreg.ValueQuantity, "unit.volt", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.source_description", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.source_order", semreg.ValueQuantity, "unit.one", "equipment.dimension.unit", bound("0", 0), bound("255", 0), nil},
	{"equipment.power.source_status", semreg.ValueSymbol, "", "equipment.dimension.unit", nil, nil, []string{"unspecified", "active", "standby", "unavailable"}},
	{"equipment.power.wired_current", semreg.ValueQuantity, "unit.ampere", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.wired_current_type", semreg.ValueSymbol, "", "equipment.dimension.unit", nil, nil, []string{"ac", "dc"}},
	{"equipment.power.wired_faults", semreg.ValueSymbols, "", "equipment.dimension.unit", nil, nil, []string{"unspecified", "over_voltage", "under_voltage", "no_fault"}},
	{"equipment.power.wired_input_frequency", semreg.ValueQuantity, "unit.hertz", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.wired_input_voltage", semreg.ValueQuantity, "unit.volt", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.wired_maximum_current", semreg.ValueQuantity, "unit.ampere", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.wired_nominal_voltage", semreg.ValueQuantity, "unit.volt", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.power.wired_present", semreg.ValueBoolean, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.status.reachable", semreg.ValueBoolean, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.time.current", semreg.ValueTime, "", "equipment.dimension.unit", nil, nil, nil},
	{"equipment.time.timezone", semreg.ValueText, "", "equipment.dimension.unit", nil, nil, nil},
}

var dimensions = map[semreg.DefinitionID]semreg.ValueKind{
	"equipment.dimension.resource": semreg.ValueText,
	"equipment.dimension.unit":     semreg.ValueText,
}

var fieldQualifiers = map[semreg.DefinitionID][]semreg.DefinitionID{
	"equipment.message.kind": []semreg.DefinitionID{"equipment.dimension.message"},
	"equipment.message.text": []semreg.DefinitionID{"equipment.dimension.message"},
	"equipment.message.time": []semreg.DefinitionID{"equipment.dimension.message"},
}

var qualifierSymbols = map[semreg.DefinitionID][]string{}

var qualifierKinds = map[semreg.DefinitionID]semreg.ValueKind{
	"equipment.dimension.message": semreg.ValueQuantity,
}

var services = map[semreg.DefinitionID]semreg.DefinitionID{
	"equipment.service.resource": "equipment.dimension.resource",
	"equipment.service.unit":     "equipment.dimension.unit",
}

var capabilities = map[semreg.DefinitionID]capabilitySpec{
	"equipment.capability.read.resource": {"equipment.service.resource", nil},
	"equipment.capability.read.unit":     {"equipment.service.unit", nil},
	"equipment.capability.set_label":     {"equipment.service.resource", nil},
}

var operations = map[semreg.DefinitionID]operationSpec{
	"equipment.operation.set_label": {"equipment.capability.set_label", "equipment.label.name", "equipment.effect.set_label"},
}

var operationParameters = map[semreg.DefinitionID][]semreg.DefinitionID{
	"equipment.operation.set_label": []semreg.DefinitionID{"equipment.label.description"},
}

var unionEvents = []semreg.DefinitionID{"equipment.event.battery_charge_fault_change", "equipment.event.battery_fault_change", "equipment.event.message_queued", "equipment.event.reachable_changed", "equipment.event.wired_fault_change"}

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
	result := semreg.DefinitionIndex{Pack: pack, Services: sortedRefs(services), Capabilities: sortedRefs(capabilities), Operations: sortedRefs(operations)}
	for _, spec := range fields {
		result.Fields = append(result.Fields, definition(spec.id))
	}
	sort.Slice(result.Fields, func(i, j int) bool { return result.Fields[i].ID < result.Fields[j].ID })
	effects := map[semreg.DefinitionID]struct{}{}
	for _, spec := range operations {
		effects[spec.effect] = struct{}{}
	}
	result.EffectRules = sortedRefs(effects)
	if len(unionEvents) > 0 {
		for _, id := range unionEvents {
			result.Events = append(result.Events, definition(id))
		}
		sort.Slice(result.Events, func(i, j int) bool { return result.Events[i].ID < result.Events[j].ID })
	}
	return result
}

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
	for id, spec := range capabilities {
		metadata.Capabilities = append(metadata.Capabilities, semreg.CapabilityMetadata{Ref: definition(id), Service: definition(spec.service)})
	}
	for id, spec := range operations {
		item := semreg.OperationMetadata{Ref: definition(id), Capability: definition(spec.capability), Service: definition(capabilities[spec.capability].service), Argument: definition(spec.argument), Effect: definition(spec.effect)}
		for _, parameter := range operationParameters[id] {
			item.Parameters = append(item.Parameters, definition(parameter))
		}
		metadata.Operations = append(metadata.Operations, item)
	}
	for id := range units {
		metadata.Units = append(metadata.Units, definition(id))
	}
	for _, id := range unionEvents {
		metadata.Events = append(metadata.Events, definition(id))
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
	sort.Slice(metadata.Events, func(i, j int) bool { return metadata.Events[i].ID < metadata.Events[j].ID })
}
