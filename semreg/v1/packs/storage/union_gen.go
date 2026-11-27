package storage

import (
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

func init() {
	mergeUnion()
}

func mergeUnion() {
	fields = append(fields, unionFields...)
	for id, dimension := range unionServices {
		services[id] = dimension
	}
	for id, spec := range unionCapabilities {
		capabilities[id] = spec
	}
	for id, spec := range unionOperations {
		operations[id] = spec
	}
	for id, qualifiers := range unionFieldQualifiers {
		if _, ok := fieldQualifiers[id]; !ok {
			fieldQualifiers[id] = qualifiers
		}
	}
	for id, symbols := range unionQualifierSymbols {
		if _, ok := qualifierSymbols[id]; !ok {
			qualifierSymbols[id] = symbols
		}
	}
	for id, kind := range unionQualifierKinds {
		qualifierKinds[id] = kind
	}
	for _, spec := range operations {
		current := capabilities[spec.capability]
		if len(current.constraints) == 0 {
			current.constraints = []semreg.DefinitionID{spec.argument}
			capabilities[spec.capability] = current
		}
	}
}

var unionFields = []fieldSpec{
	{"storage.capacity.full_charge", semreg.ValueQuantity, "unit.ampere_hour", "storage.dimension.pack", nil, nil, nil},
	{"storage.capacity.remaining", semreg.ValueQuantity, "unit.ampere_hour", "storage.dimension.pack", nil, nil, nil},
	{"storage.cob.actor", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.cob.ec1", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.cob.heartbeat_counter", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.cob.heartbeat_timeout", semreg.ValueQuantity, "unit.second", "storage.dimension.pack", nil, bound("6", 1), nil},
	{"storage.cob.heartbeat_timestamp", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.cob.use_case_name", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.cob.use_case_version", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.configuration.battery_type", semreg.ValueSymbol, "", "storage.dimension.pack", nil, nil, []string{"lead_acid", "lithium_iron_phosphate", "lithium_nickel_manganese_cobalt", "other"}},
	{"storage.configuration.control_mode", semreg.ValueSymbol, "", "storage.dimension.interface", nil, nil, []string{"auto", "pcc", "power"}},
	{"storage.configuration.default_ac_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.configuration.default_dc_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.configuration.default_pcc_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.configuration.failsafe_ac_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.configuration.failsafe_dc_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.configuration.failsafe_duration_minimum", semreg.ValueQuantity, "unit.second", "storage.dimension.interface", bound("72", 2), bound("864", 2), nil},
	{"storage.configuration.failsafe_pcc_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.configuration.maximum_cycles_per_day", semreg.ValueQuantity, "unit.one", "storage.dimension.pack", nil, nil, nil},
	{"storage.interface.ac_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.limit.ac_charge_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.limit.ac_discharge_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.limit.dc_charge_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.limit.dc_discharge_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.mob.actor", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.device_code", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.device_name", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.ec1", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.hardware_revision", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.manufacturer_label", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.moi_016b", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.moi_018", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.moi_022", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.serial_number", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.software_revision", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.timestamp", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.use_case_name", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.use_case_version", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.vendor_code", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.mob.vendor_name", semreg.ValueText, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.nameplate.ac_charge_power_maximum", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.nameplate.ac_charge_power_nominal_maximum", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.nameplate.ac_charge_power_nominal_minimum", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.nameplate.ac_discharge_power_maximum", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.nameplate.ac_discharge_power_nominal_maximum", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.nameplate.ac_discharge_power_nominal_minimum", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.nameplate.dc_charge_power_maximum", semreg.ValueQuantity, "unit.watt", "storage.dimension.pack", nil, nil, nil},
	{"storage.nameplate.dc_discharge_power_maximum", semreg.ValueQuantity, "unit.watt", "storage.dimension.pack", nil, nil, nil},
	{"storage.nameplate.time_to_ac_charge_power_maximum", semreg.ValueQuantity, "unit.second", "storage.dimension.interface", nil, nil, nil},
	{"storage.nameplate.time_to_ac_discharge_power_maximum", semreg.ValueQuantity, "unit.second", "storage.dimension.interface", nil, nil, nil},
	{"storage.setpoint.ac_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.setpoint.active", semreg.ValueBoolean, "", "storage.dimension.interface", nil, nil, nil},
	{"storage.setpoint.dc_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.setpoint.duration", semreg.ValueQuantity, "unit.second", "storage.dimension.interface", nil, nil, nil},
	{"storage.setpoint.pcc_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.interface", nil, nil, nil},
	{"storage.state.cycle_count", semreg.ValueQuantity, "unit.one", "storage.dimension.pack", nil, nil, nil},
	{"storage.status.charge_direction", semreg.ValueSymbol, "", "storage.dimension.pack", nil, nil, []string{"charging", "discharging", "idle", "starting"}},
	{"storage.status.charge_faults", semreg.ValueSymbols, "", "storage.dimension.pack", nil, nil, []string{"ambient_too_cold", "ambient_too_hot", "battery_absent", "battery_over_voltage", "battery_too_cold", "battery_too_hot", "battery_under_voltage", "charger_over_voltage", "charger_under_voltage", "safety_timeout", "unspecified"}},
	{"storage.status.charge_level", semreg.ValueSymbol, "", "storage.dimension.pack", nil, nil, []string{"critical", "ok", "warning"}},
	{"storage.status.faults", semreg.ValueSymbols, "", "storage.dimension.pack", nil, nil, []string{"over_temperature", "under_temperature", "unspecified"}},
	{"storage.status.replacement_needed", semreg.ValueBoolean, "", "storage.dimension.pack", nil, nil, nil},
	{"storage.status.state", semreg.ValueSymbol, "", "storage.dimension.pack", nil, nil, []string{"failure", "normal", "off", "standby"}},
	{"storage.system.ac_power", semreg.ValueQuantity, "unit.watt", "storage.dimension.system", nil, nil, nil},
	{"storage.system.energy_charged", semreg.ValueQuantity, "unit.joule", "storage.dimension.system", nil, nil, nil},
	{"storage.system.energy_discharged", semreg.ValueQuantity, "unit.joule", "storage.dimension.system", nil, nil, nil},
	{"storage.system.state_of_charge", semreg.ValueQuantity, "unit.ratio", "storage.dimension.system", bound("0", 0), bound("1", 0), nil},
	{"storage.vabd.actor", semreg.ValueText, "", "storage.dimension.system", nil, nil, nil},
	{"storage.vabd.ec1", semreg.ValueText, "", "storage.dimension.system", nil, nil, nil},
	{"storage.vabd.use_case_name", semreg.ValueText, "", "storage.dimension.system", nil, nil, nil},
	{"storage.vabd.use_case_version", semreg.ValueText, "", "storage.dimension.system", nil, nil, nil},
	{"storage.vabd.vabd_005", semreg.ValueText, "", "storage.dimension.system", nil, nil, nil},
}

var unionDimensions = map[semreg.DefinitionID]semreg.ValueKind{
	"storage.dimension.battery":   semreg.ValueText,
	"storage.dimension.cell":      semreg.ValueText,
	"storage.dimension.interface": semreg.ValueText,
	"storage.dimension.module":    semreg.ValueText,
	"storage.dimension.pack":      semreg.ValueText,
	"storage.dimension.string":    semreg.ValueText,
	"storage.dimension.system":    semreg.ValueText,
}

var unionServices = map[semreg.DefinitionID]semreg.DefinitionID{
	"storage.service.battery":   "storage.dimension.battery",
	"storage.service.cell":      "storage.dimension.cell",
	"storage.service.interface": "storage.dimension.interface",
	"storage.service.module":    "storage.dimension.module",
	"storage.service.pack":      "storage.dimension.pack",
	"storage.service.string":    "storage.dimension.string",
	"storage.service.system":    "storage.dimension.system",
}

var unionCapabilities = map[semreg.DefinitionID]capabilitySpec{
	"storage.capability.read.cell":                     {"storage.service.cell", nil},
	"storage.capability.read.interface":                {"storage.service.interface", nil},
	"storage.capability.read.pack":                     {"storage.service.pack", nil},
	"storage.capability.set_ac_charge_power_limit":     {"storage.service.interface", nil},
	"storage.capability.set_ac_discharge_power_limit":  {"storage.service.interface", nil},
	"storage.capability.set_ac_power":                  {"storage.service.interface", nil},
	"storage.capability.set_charge_limit":              {"storage.service.interface", nil},
	"storage.capability.set_control_mode":              {"storage.service.interface", nil},
	"storage.capability.set_dc_charge_power_limit":     {"storage.service.interface", nil},
	"storage.capability.set_dc_discharge_power_limit":  {"storage.service.interface", nil},
	"storage.capability.set_dc_power":                  {"storage.service.interface", nil},
	"storage.capability.set_default_ac_power":          {"storage.service.interface", nil},
	"storage.capability.set_default_dc_power":          {"storage.service.interface", nil},
	"storage.capability.set_default_pcc_power":         {"storage.service.interface", nil},
	"storage.capability.set_discharge_limit":           {"storage.service.interface", nil},
	"storage.capability.set_failsafe_ac_power":         {"storage.service.interface", nil},
	"storage.capability.set_failsafe_dc_power":         {"storage.service.interface", nil},
	"storage.capability.set_failsafe_duration_minimum": {"storage.service.interface", nil},
	"storage.capability.set_failsafe_pcc_power":        {"storage.service.interface", nil},
	"storage.capability.set_pcc_power":                 {"storage.service.interface", nil},
}

var unionOperations = map[semreg.DefinitionID]operationSpec{
	"storage.operation.set_ac_charge_power_limit":     {"storage.capability.set_ac_charge_power_limit", "storage.limit.ac_charge_power", "storage.effect.set_ac_charge_power_limit"},
	"storage.operation.set_ac_discharge_power_limit":  {"storage.capability.set_ac_discharge_power_limit", "storage.limit.ac_discharge_power", "storage.effect.set_ac_discharge_power_limit"},
	"storage.operation.set_ac_power":                  {"storage.capability.set_ac_power", "storage.setpoint.ac_power", "storage.effect.set_ac_power"},
	"storage.operation.set_charge_limit":              {"storage.capability.set_charge_limit", "storage.limit.charge_power", "storage.effect.set_charge_limit"},
	"storage.operation.set_control_mode":              {"storage.capability.set_control_mode", "storage.configuration.control_mode", "storage.effect.set_control_mode"},
	"storage.operation.set_dc_charge_power_limit":     {"storage.capability.set_dc_charge_power_limit", "storage.limit.dc_charge_power", "storage.effect.set_dc_charge_power_limit"},
	"storage.operation.set_dc_discharge_power_limit":  {"storage.capability.set_dc_discharge_power_limit", "storage.limit.dc_discharge_power", "storage.effect.set_dc_discharge_power_limit"},
	"storage.operation.set_dc_power":                  {"storage.capability.set_dc_power", "storage.setpoint.dc_power", "storage.effect.set_dc_power"},
	"storage.operation.set_default_ac_power":          {"storage.capability.set_default_ac_power", "storage.configuration.default_ac_power", "storage.effect.set_default_ac_power"},
	"storage.operation.set_default_dc_power":          {"storage.capability.set_default_dc_power", "storage.configuration.default_dc_power", "storage.effect.set_default_dc_power"},
	"storage.operation.set_default_pcc_power":         {"storage.capability.set_default_pcc_power", "storage.configuration.default_pcc_power", "storage.effect.set_default_pcc_power"},
	"storage.operation.set_discharge_limit":           {"storage.capability.set_discharge_limit", "storage.limit.discharge_power", "storage.effect.set_discharge_limit"},
	"storage.operation.set_failsafe_ac_power":         {"storage.capability.set_failsafe_ac_power", "storage.configuration.failsafe_ac_power", "storage.effect.set_failsafe_ac_power"},
	"storage.operation.set_failsafe_dc_power":         {"storage.capability.set_failsafe_dc_power", "storage.configuration.failsafe_dc_power", "storage.effect.set_failsafe_dc_power"},
	"storage.operation.set_failsafe_duration_minimum": {"storage.capability.set_failsafe_duration_minimum", "storage.configuration.failsafe_duration_minimum", "storage.effect.set_failsafe_duration_minimum"},
	"storage.operation.set_failsafe_pcc_power":        {"storage.capability.set_failsafe_pcc_power", "storage.configuration.failsafe_pcc_power", "storage.effect.set_failsafe_pcc_power"},
	"storage.operation.set_pcc_power":                 {"storage.capability.set_pcc_power", "storage.setpoint.pcc_power", "storage.effect.set_pcc_power"},
}

var unionFieldQualifiers = map[semreg.DefinitionID][]semreg.DefinitionID{}

var unionQualifierSymbols = map[semreg.DefinitionID][]string{}

var unionQualifierKinds = map[semreg.DefinitionID]semreg.ValueKind{}

var operationParameters = map[semreg.DefinitionID][]semreg.DefinitionID{
	"storage.operation.set_ac_power":  []semreg.DefinitionID{"storage.setpoint.active", "storage.setpoint.duration"},
	"storage.operation.set_dc_power":  []semreg.DefinitionID{"storage.setpoint.active", "storage.setpoint.duration"},
	"storage.operation.set_pcc_power": []semreg.DefinitionID{"storage.setpoint.active", "storage.setpoint.duration"},
}

var unionEvents = []semreg.DefinitionID{"storage.event.charge_fault_change", "storage.event.fault_change"}

var fieldQualifiers = map[semreg.DefinitionID][]semreg.DefinitionID{
	"storage.accuracy.measured":                        {"storage.dimension.measurement_type"},
	"storage.accuracy.ratio_maximum":                   {"storage.dimension.measurement_type"},
	"storage.accuracy.measured_minimum_active_current": {"storage.dimension.measurement_type"},
	"storage.accuracy.measured_minimum_active_power":   {"storage.dimension.measurement_type"},
	"storage.accuracy.measured_maximum_active_current": {"storage.dimension.measurement_type"},
	"storage.accuracy.measured_maximum_active_power":   {"storage.dimension.measurement_type"},
	"storage.accuracy.range_minimum_active_current":    {"storage.dimension.measurement_type"},
	"storage.accuracy.range_minimum_active_power":      {"storage.dimension.measurement_type"},
	"storage.accuracy.range_maximum_active_current":    {"storage.dimension.measurement_type"},
	"storage.accuracy.range_maximum_active_power":      {"storage.dimension.measurement_type"},
}
var qualifierSymbols = map[semreg.DefinitionID][]string{
	"storage.dimension.measurement_type": {"active_current", "active_power", "apparent_current", "apparent_energy", "apparent_power", "electrical_energy", "frequency", "neutral_current", "power_factor", "reactive_current", "reactive_energy", "reactive_power", "rms_current", "rms_power", "rms_voltage", "unspecified", "voltage"},
}
var qualifierKinds = map[semreg.DefinitionID]semreg.ValueKind{
	"storage.dimension.measurement_type": semreg.ValueSymbol,
}
