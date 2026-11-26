package pv

import (
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

func init() {
	mergeUnion()
}

func mergeUnion() {
	fields = append(fields, unionFields...)
	for id, kind := range unionDimensions {
		dimensions[id] = kind
	}
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
	{"pv.ac.phase_energy_exported", semreg.ValueQuantity, "unit.joule", "pv.dimension.phase", nil, nil, nil},
	{"pv.ac.phase_energy_imported", semreg.ValueQuantity, "unit.joule", "pv.dimension.phase", nil, nil, nil},
	{"pv.ac.phase_power_factor", semreg.ValueQuantity, "unit.ratio", "pv.dimension.phase", bound("-1", 0), bound("1", 0), nil},
	{"pv.configuration.grid_code", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.dc.aggregate_energy", semreg.ValueQuantity, "unit.joule", "pv.dimension.inverter", nil, nil, nil},
	{"pv.energy.apparent_exported", semreg.ValueQuantity, "unit.volt_ampere_second", "pv.dimension.inverter", nil, nil, nil},
	{"pv.energy.apparent_imported", semreg.ValueQuantity, "unit.volt_ampere_second", "pv.dimension.inverter", nil, nil, nil},
	{"pv.energy.consumed", semreg.ValueQuantity, "unit.joule", "pv.dimension.inverter", nil, nil, nil},
	{"pv.energy.reactive_exported", semreg.ValueQuantity, "unit.volt_ampere_reactive_second", "pv.dimension.inverter", nil, nil, nil},
	{"pv.energy.reactive_imported", semreg.ValueQuantity, "unit.volt_ampere_reactive_second", "pv.dimension.inverter", nil, nil, nil},
	{"pv.energy.yield_history", semreg.ValueQuantity, "unit.joule", "pv.dimension.system", nil, nil, nil},
	{"pv.moi.ac_yield_day", semreg.ValueQuantity, "unit.joule", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.ac_yield_day_historical", semreg.ValueQuantity, "unit.joule", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.ac_yield_month", semreg.ValueQuantity, "unit.joule", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.ac_yield_month_historical", semreg.ValueQuantity, "unit.joule", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.ac_yield_total", semreg.ValueQuantity, "unit.joule", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.ac_yield_year", semreg.ValueQuantity, "unit.joule", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.ac_yield_year_historical", semreg.ValueQuantity, "unit.joule", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.actor", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.device_code", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.device_name", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.ec1", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.hardware_revision", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.manufacturer_label", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.moi_016b", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.moi_018", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.moi_022", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.serial_number", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.software_revision", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.timestamp", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.use_case_name", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.use_case_version", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.vendor_code", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.moi.vendor_name", semreg.ValueText, "", "pv.dimension.inverter", nil, nil, nil},
	{"pv.mps.actor", semreg.ValueText, "", "pv.dimension.string", nil, nil, nil},
	{"pv.mps.ec1", semreg.ValueText, "", "pv.dimension.string", nil, nil, nil},
	{"pv.mps.use_case_name", semreg.ValueText, "", "pv.dimension.string", nil, nil, nil},
	{"pv.mps.use_case_version", semreg.ValueText, "", "pv.dimension.string", nil, nil, nil},
	{"pv.nameplate.apparent_consumption_nominal_maximum", semreg.ValueQuantity, "unit.volt_ampere", "pv.dimension.inverter", nil, nil, nil},
	{"pv.nameplate.apparent_production_nominal_maximum", semreg.ValueQuantity, "unit.volt_ampere", "pv.dimension.inverter", nil, nil, nil},
	{"pv.nameplate.consumption_nominal_maximum", semreg.ValueQuantity, "unit.watt", "pv.dimension.inverter", nil, nil, nil},
	{"pv.nameplate.peak_power", semreg.ValueQuantity, "unit.watt", "pv.dimension.system", nil, nil, nil},
	{"pv.nameplate.production_nominal_maximum", semreg.ValueQuantity, "unit.watt", "pv.dimension.inverter", nil, nil, nil},
	{"pv.status.battery_availability", semreg.ValueSymbol, "", "pv.dimension.inverter", nil, nil, []string{"available", "not_available"}},
	{"pv.status.failure_states", semreg.ValueSymbols, "", "pv.dimension.inverter", nil, nil, []string{"failure_1", "failure_2", "failure_3", "failure_4", "failure_5", "failure_6", "failure_7", "failure_8", "failure_9", "failure_10", "failure_11", "failure_12", "failure_13", "failure_14", "failure_15", "failure_16", "failure_17", "failure_18"}},
	{"pv.status.functional_states", semreg.ValueSymbols, "", "pv.dimension.inverter", nil, nil, []string{"functional_1", "functional_2", "functional_3", "functional_4", "functional_5", "functional_6", "functional_7", "functional_8", "functional_9", "functional_10", "functional_11", "functional_12", "functional_13"}},
	{"pv.status.grid_connection", semreg.ValueSymbol, "", "pv.dimension.inverter", nil, nil, []string{"connected", "not_connected", "unknown"}},
	{"pv.status.string_availability", semreg.ValueSymbol, "", "pv.dimension.string", nil, nil, []string{"available", "not_available"}},
	{"pv.string.azimuth", semreg.ValueQuantity, "unit.radian", "pv.dimension.string", nil, nil, nil},
	{"pv.string.current", semreg.ValueQuantity, "unit.ampere", "pv.dimension.string", nil, nil, nil},
	{"pv.string.insulation_resistance", semreg.ValueQuantity, "unit.ohm", "pv.dimension.string", nil, nil, nil},
	{"pv.string.power", semreg.ValueQuantity, "unit.watt", "pv.dimension.string", nil, nil, nil},
	{"pv.string.temperature", semreg.ValueQuantity, "unit.kelvin", "pv.dimension.string", nil, nil, nil},
	{"pv.string.tilt", semreg.ValueQuantity, "unit.radian", "pv.dimension.string", nil, nil, nil},
	{"pv.string.voltage", semreg.ValueQuantity, "unit.volt", "pv.dimension.string", nil, nil, nil},
	{"pv.system.active_power", semreg.ValueQuantity, "unit.watt", "pv.dimension.system", nil, nil, nil},
	{"pv.vapd.ac_power_total", semreg.ValueQuantity, "unit.watt", "pv.dimension.system", nil, nil, nil},
	{"pv.vapd.ac_yield_total", semreg.ValueQuantity, "unit.joule", "pv.dimension.system", nil, nil, nil},
	{"pv.vapd.actor", semreg.ValueText, "", "pv.dimension.system", nil, nil, nil},
	{"pv.vapd.ec1", semreg.ValueText, "", "pv.dimension.system", nil, nil, nil},
	{"pv.vapd.use_case_name", semreg.ValueText, "", "pv.dimension.system", nil, nil, nil},
	{"pv.vapd.use_case_version", semreg.ValueText, "", "pv.dimension.system", nil, nil, nil},
	{"pv.vapd.vapd_004", semreg.ValueText, "", "pv.dimension.system", nil, nil, nil},
}

var unionDimensions = map[semreg.DefinitionID]semreg.ValueKind{
	"pv.dimension.array":            semreg.ValueText,
	"pv.dimension.input":            semreg.ValueText,
	"pv.dimension.inverter":         semreg.ValueText,
	"pv.dimension.measurement_type": semreg.ValueSymbol,
	"pv.dimension.period":           semreg.ValueSymbol,
	"pv.dimension.period_offset":    semreg.ValueQuantity,
	"pv.dimension.phase":            semreg.ValueText,
	"pv.dimension.string":           semreg.ValueText,
	"pv.dimension.system":           semreg.ValueText,
}

var unionServices = map[semreg.DefinitionID]semreg.DefinitionID{
	"pv.service.array":    "pv.dimension.array",
	"pv.service.input":    "pv.dimension.input",
	"pv.service.inverter": "pv.dimension.inverter",
	"pv.service.phase":    "pv.dimension.phase",
	"pv.service.string":   "pv.dimension.string",
	"pv.service.system":   "pv.dimension.system",
}

var unionCapabilities = map[semreg.DefinitionID]capabilitySpec{
	"pv.capability.read.array":             {"pv.service.array", nil},
	"pv.capability.read.input":             {"pv.service.input", nil},
	"pv.capability.read.inverter":          {"pv.service.inverter", nil},
	"pv.capability.read.phase":             {"pv.service.phase", nil},
	"pv.capability.read.string":            {"pv.service.string", nil},
	"pv.capability.read.system":            {"pv.service.system", nil},
	"pv.capability.set_active_power_limit": {"pv.service.inverter", nil},
	"pv.capability.set_export_limit":       {"pv.service.system", nil},
}

var unionOperations = map[semreg.DefinitionID]operationSpec{
	"pv.operation.set_active_power_limit": {"pv.capability.set_active_power_limit", "pv.limit.active_power", "pv.effect.set_active_power_limit"},
	"pv.operation.set_export_limit":       {"pv.capability.set_export_limit", "pv.limit.export_power", "pv.effect.set_export_limit"},
}

var unionFieldQualifiers = map[semreg.DefinitionID][]semreg.DefinitionID{
	"pv.accuracy.measured":                        []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.accuracy.ratio_maximum":                   []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.accuracy.measured_minimum_active_current": []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.accuracy.measured_minimum_active_power":   []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.accuracy.measured_maximum_active_current": []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.accuracy.measured_maximum_active_power":   []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.accuracy.range_minimum_active_current":    []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.accuracy.range_minimum_active_power":      []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.accuracy.range_maximum_active_current":    []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.accuracy.range_maximum_active_power":      []semreg.DefinitionID{"pv.dimension.measurement_type"},
	"pv.energy.yield_history":                     []semreg.DefinitionID{"pv.dimension.period", "pv.dimension.period_offset"},
	"pv.moi.ac_yield_day":                         []semreg.DefinitionID{"pv.dimension.period"},
	"pv.moi.ac_yield_day_historical":              []semreg.DefinitionID{"pv.dimension.period", "pv.dimension.period_offset"},
	"pv.moi.ac_yield_month":                       []semreg.DefinitionID{"pv.dimension.period"},
	"pv.moi.ac_yield_month_historical":            []semreg.DefinitionID{"pv.dimension.period", "pv.dimension.period_offset"},
	"pv.moi.ac_yield_year":                        []semreg.DefinitionID{"pv.dimension.period"},
	"pv.moi.ac_yield_year_historical":             []semreg.DefinitionID{"pv.dimension.period", "pv.dimension.period_offset"},
}

var unionQualifierSymbols = map[semreg.DefinitionID][]string{
	"pv.dimension.measurement_type": []string{"active_current", "active_power", "apparent_current", "apparent_energy", "apparent_power", "electrical_energy", "frequency", "neutral_current", "power_factor", "reactive_current", "reactive_energy", "reactive_power", "rms_current", "rms_power", "rms_voltage", "unspecified", "voltage"},
	"pv.dimension.period":           []string{"day", "month", "year"},
}

var unionQualifierKinds = map[semreg.DefinitionID]semreg.ValueKind{
	"pv.dimension.measurement_type": semreg.ValueSymbol,
	"pv.dimension.period":           semreg.ValueSymbol,
	"pv.dimension.period_offset":    semreg.ValueQuantity,
}

var operationParameters = map[semreg.DefinitionID][]semreg.DefinitionID{}

var unionEvents = []semreg.DefinitionID{}

var qualifierKinds = map[semreg.DefinitionID]semreg.ValueKind{}
