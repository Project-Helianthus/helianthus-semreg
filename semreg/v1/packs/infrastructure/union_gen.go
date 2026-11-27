package infrastructure

import (
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

func init() {
	mergeUnion()
}

func mergeUnion() {
	known := map[semreg.DefinitionID]struct{}{}
	for _, field := range fields {
		known[field.id] = struct{}{}
	}
	for _, field := range unionFields {
		if _, exists := known[field.id]; exists {
			continue
		}
		fields = append(fields, field)
		known[field.id] = struct{}{}
	}
	for id, kind := range unionDimensions {
		dimensions[id] = kind
	}
	for id, dimension := range unionServices {
		services[id] = dimension
	}
	for id, service := range unionCapabilities {
		capabilities[id] = service
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
}

var unionFields = []fieldSpec{
	{"infrastructure.ac.active_power", semreg.ValueQuantity, "unit.watt", "infrastructure.dimension.phase", bound("-1", 7), bound("1", 7), nil},
	{"infrastructure.ac.apparent_current", semreg.ValueQuantity, "unit.ampere", "infrastructure.dimension.phase", nil, nil, nil},
	{"infrastructure.ac.apparent_power", semreg.ValueQuantity, "unit.volt_ampere", "infrastructure.dimension.phase", bound("0", 0), bound("1", 7), nil},
	{"infrastructure.ac.current", semreg.ValueQuantity, "unit.ampere", "infrastructure.dimension.phase", bound("0", 0), bound("1", 3), nil},
	{"infrastructure.ac.frequency", semreg.ValueQuantity, "unit.hertz", "infrastructure.dimension.grid_connection", bound("0", 0), bound("1", 3), nil},
	{"infrastructure.ac.harmonic_current", semreg.ValueQuantity, "", "infrastructure.dimension.phase", nil, nil, nil},
	{"infrastructure.ac.harmonic_phase", semreg.ValueQuantity, "", "infrastructure.dimension.phase", nil, nil, nil},
	{"infrastructure.ac.neutral_current", semreg.ValueQuantity, "unit.ampere", "infrastructure.dimension.circuit", nil, nil, nil},
	{"infrastructure.ac.phase_power_factor", semreg.ValueQuantity, "unit.ratio", "infrastructure.dimension.phase", nil, nil, nil},
	{"infrastructure.ac.power_factor", semreg.ValueQuantity, "unit.ratio", "infrastructure.dimension.grid_connection", bound("-1", 0), bound("1", 0), nil},
	{"infrastructure.ac.reactive_current", semreg.ValueQuantity, "unit.ampere", "infrastructure.dimension.phase", nil, nil, nil},
	{"infrastructure.ac.reactive_power", semreg.ValueQuantity, "unit.volt_ampere_reactive", "infrastructure.dimension.phase", bound("-1", 7), bound("1", 7), nil},
	{"infrastructure.ac.voltage", semreg.ValueQuantity, "unit.volt", "infrastructure.dimension.phase", bound("0", 0), bound("1", 3), nil},
	{"infrastructure.ac.voltage_line_line", semreg.ValueQuantity, "unit.volt", "infrastructure.dimension.line_pair", bound("0", 0), bound("1", 3), nil},
	{"infrastructure.accuracy.measured", semreg.ValueBoolean, "", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.accuracy.ratio_maximum", semreg.ValueQuantity, "unit.ratio", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.energy.apparent_export", semreg.ValueQuantity, "unit.volt_ampere_second", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.energy.apparent_import", semreg.ValueQuantity, "unit.volt_ampere_second", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.energy.export", semreg.ValueQuantity, "unit.joule", "infrastructure.dimension.meter", bound("0", 0), bound("36", 17), nil},
	{"infrastructure.energy.import", semreg.ValueQuantity, "unit.joule", "infrastructure.dimension.meter", bound("0", 0), bound("36", 17), nil},
	{"infrastructure.energy.phase_export", semreg.ValueQuantity, "unit.joule", "infrastructure.dimension.phase", nil, nil, nil},
	{"infrastructure.energy.phase_import", semreg.ValueQuantity, "unit.joule", "infrastructure.dimension.phase", nil, nil, nil},
	{"infrastructure.energy.reactive_export", semreg.ValueQuantity, "unit.volt_ampere_reactive_second", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.energy.reactive_import", semreg.ValueQuantity, "unit.volt_ampere_reactive_second", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.energy.self_consumption", semreg.ValueQuantity, "unit.joule", "infrastructure.dimension.meter", bound("0", 0), bound("36", 17), nil},
	{"infrastructure.limit.fallback_power", semreg.ValueQuantity, "unit.watt", "infrastructure.dimension.grid_connection", bound("0", 0), bound("1", 7), nil},
	{"infrastructure.limit.pv_curtailment_factor", semreg.ValueQuantity, "unit.ratio", "infrastructure.dimension.grid_connection", bound("0", 0), bound("1", 0), nil},
	{"infrastructure.load.active_power", semreg.ValueQuantity, "unit.watt", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.load.energy_consumed", semreg.ValueQuantity, "unit.joule", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.load.energy_produced", semreg.ValueQuantity, "unit.joule", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.load.phase_active_power", semreg.ValueQuantity, "unit.watt", "infrastructure.dimension.phase", nil, nil, nil},
	{"infrastructure.meter.apparent_power_threshold", semreg.ValueQuantity, "unit.volt_ampere", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.meter.kind", semreg.ValueSymbol, "", "infrastructure.dimension.meter", nil, nil, []string{"utility", "private", "generic"}},
	{"infrastructure.meter.point_of_delivery", semreg.ValueText, "", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.meter.power_threshold", semreg.ValueQuantity, "unit.watt", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.meter.power_threshold_source", semreg.ValueSymbol, "", "infrastructure.dimension.meter", nil, nil, []string{"contract", "regulator", "equipment"}},
	{"infrastructure.meter.protocol_version", semreg.ValueText, "", "infrastructure.dimension.meter", nil, nil, nil},
	{"infrastructure.mgcp.ac_current", semreg.ValueText, "", "infrastructure.dimension.grid_connection", nil, nil, nil},
	{"infrastructure.mgcp.actor", semreg.ValueText, "", "infrastructure.dimension.grid_connection", nil, nil, nil},
	{"infrastructure.mgcp.committed_power_plan", semreg.ValueText, "", "infrastructure.dimension.grid_connection", nil, nil, nil},
	{"infrastructure.mgcp.per", semreg.ValueText, "", "infrastructure.dimension.grid_connection", nil, nil, nil},
	{"infrastructure.mgcp.slots", semreg.ValueText, "", "infrastructure.dimension.grid_connection", nil, nil, nil},
	{"infrastructure.mgcp.use_case_name", semreg.ValueText, "", "infrastructure.dimension.grid_connection", nil, nil, nil},
	{"infrastructure.mgcp.use_case_version", semreg.ValueText, "", "infrastructure.dimension.grid_connection", nil, nil, nil},
	{"infrastructure.mgcp.value_range_min", semreg.ValueText, "", "infrastructure.dimension.grid_connection", nil, nil, nil},
	{"infrastructure.mgcp.value_source", semreg.ValueText, "", "infrastructure.dimension.grid_connection", nil, nil, nil},
	{"infrastructure.mpc.ac_power", semreg.ValueQuantity, "unit.watt", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.mpc.ac_voltage", semreg.ValueText, "", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.mpc.actor", semreg.ValueText, "", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.mpc.one", semreg.ValueText, "", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.mpc.per", semreg.ValueText, "", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.mpc.slots", semreg.ValueText, "", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.mpc.use_case_name", semreg.ValueText, "", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.mpc.use_case_version", semreg.ValueText, "", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.mpc.value_range_min", semreg.ValueText, "", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.mpc.value_state", semreg.ValueText, "", "infrastructure.dimension.load", nil, nil, nil},
	{"infrastructure.power.apparent", semreg.ValueQuantity, "unit.volt_ampere", "infrastructure.dimension.grid_connection", bound("0", 0), bound("1", 7), nil},
	{"infrastructure.power.export_active", semreg.ValueQuantity, "unit.watt", "infrastructure.dimension.grid_connection", bound("0", 0), bound("1", 7), nil},
	{"infrastructure.power.export_reactive", semreg.ValueQuantity, "unit.volt_ampere_reactive", "infrastructure.dimension.grid_connection", bound("0", 0), bound("1", 7), nil},
	{"infrastructure.power.import_active", semreg.ValueQuantity, "unit.watt", "infrastructure.dimension.grid_connection", bound("0", 0), bound("1", 7), nil},
	{"infrastructure.power.import_reactive", semreg.ValueQuantity, "unit.volt_ampere_reactive", "infrastructure.dimension.grid_connection", bound("0", 0), bound("1", 7), nil},
	{"infrastructure.power.pcc", semreg.ValueQuantity, "unit.watt", "infrastructure.dimension.grid_connection", bound("-1", 7), bound("1", 7), nil},
	{"infrastructure.status.breaker", semreg.ValueSymbol, "", "infrastructure.dimension.circuit", nil, nil, []string{"infrastructure.status.breaker.closed", "infrastructure.status.breaker.open"}},
	{"infrastructure.status.connection", semreg.ValueSymbol, "", "infrastructure.dimension.grid_connection", nil, nil, []string{"infrastructure.status.connection.connected", "infrastructure.status.connection.disconnected"}},
	{"infrastructure.status.fault", semreg.ValueSymbol, "", "infrastructure.dimension.site", nil, nil, []string{"infrastructure.status.fault.clear", "infrastructure.status.fault.present"}},
	{"infrastructure.status.interlock", semreg.ValueSymbol, "", "infrastructure.dimension.circuit", nil, nil, []string{"infrastructure.status.interlock.active", "infrastructure.status.interlock.clear"}},
	{"infrastructure.status.readiness", semreg.ValueSymbol, "", "infrastructure.dimension.grid_connection", nil, nil, []string{"infrastructure.status.readiness.not_ready", "infrastructure.status.readiness.ready"}},
	{"infrastructure.status.supply_condition", semreg.ValueSymbol, "", "infrastructure.dimension.grid_connection", nil, nil, []string{"normal", "over_voltage", "under_voltage", "over_frequency", "under_frequency", "outage"}},
}

var unionDimensions = map[semreg.DefinitionID]semreg.ValueKind{
	"infrastructure.dimension.circuit":         semreg.ValueText,
	"infrastructure.dimension.feeder":          semreg.ValueText,
	"infrastructure.dimension.grid_connection": semreg.ValueText,
	"infrastructure.dimension.line_pair":       semreg.ValueText,
	"infrastructure.dimension.load":            semreg.ValueText,
	"infrastructure.dimension.meter":           semreg.ValueText,
	"infrastructure.dimension.phase":           semreg.ValueText,
	"infrastructure.dimension.site":            semreg.ValueText,
}

var unionServices = map[semreg.DefinitionID]semreg.DefinitionID{
	"infrastructure.service.circuit":         "infrastructure.dimension.circuit",
	"infrastructure.service.feeder":          "infrastructure.dimension.feeder",
	"infrastructure.service.grid_connection": "infrastructure.dimension.grid_connection",
	"infrastructure.service.line_pair":       "infrastructure.dimension.line_pair",
	"infrastructure.service.load":            "infrastructure.dimension.load",
	"infrastructure.service.meter":           "infrastructure.dimension.meter",
	"infrastructure.service.phase":           "infrastructure.dimension.phase",
	"infrastructure.service.site":            "infrastructure.dimension.site",
}

var unionCapabilities = map[semreg.DefinitionID]semreg.DefinitionID{
	"infrastructure.capability.read.circuit":         "infrastructure.service.circuit",
	"infrastructure.capability.read.feeder":          "infrastructure.service.feeder",
	"infrastructure.capability.read.grid_connection": "infrastructure.service.grid_connection",
	"infrastructure.capability.read.meter":           "infrastructure.service.meter",
	"infrastructure.capability.read.phase":           "infrastructure.service.phase",
	"infrastructure.capability.read.site":            "infrastructure.service.site",
}

var unionFieldQualifiers = map[semreg.DefinitionID][]semreg.DefinitionID{
	"infrastructure.ac.harmonic_current":                      []semreg.DefinitionID{"infrastructure.dimension.harmonic_order"},
	"infrastructure.ac.harmonic_phase":                        []semreg.DefinitionID{"infrastructure.dimension.harmonic_order"},
	"infrastructure.accuracy.measured":                        []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.ratio_maximum":                   []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.measured_minimum_active_current": []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.measured_minimum_active_power":   []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.measured_maximum_active_current": []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.measured_maximum_active_power":   []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.range_minimum_active_current":    []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.range_minimum_active_power":      []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.range_maximum_active_current":    []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
	"infrastructure.accuracy.range_maximum_active_power":      []semreg.DefinitionID{"infrastructure.dimension.measurement_type"},
}

var unionQualifierSymbols = map[semreg.DefinitionID][]string{
	"infrastructure.dimension.measurement_type": []string{"active_current", "active_power", "apparent_current", "apparent_energy", "apparent_power", "electrical_energy", "frequency", "neutral_current", "power_factor", "reactive_current", "reactive_energy", "reactive_power", "rms_current", "rms_power", "rms_voltage", "unspecified", "voltage"},
}

var unionQualifierKinds = map[semreg.DefinitionID]semreg.ValueKind{
	"infrastructure.dimension.harmonic_order":   semreg.ValueQuantity,
	"infrastructure.dimension.measurement_type": semreg.ValueSymbol,
}

var operationParameters = map[semreg.DefinitionID][]semreg.DefinitionID{}

var unionEvents = []semreg.DefinitionID{"infrastructure.event.energy_measured", "infrastructure.event.supply_condition_changed"}

var qualifierKinds = map[semreg.DefinitionID]semreg.ValueKind{}
