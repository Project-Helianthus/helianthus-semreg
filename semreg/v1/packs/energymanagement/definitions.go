package energymanagement

import (
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

const (
	packID      semreg.DefinitionID    = "helianthus.pack.energy_management"
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
	{"energy.direct_control.activity_state", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.direct_control.activity_state.inactive", "energy.direct_control.activity_state.paused", "energy.direct_control.activity_state.running"}},
	{"energy.direct_control.energy", semreg.ValueQuantity, "unit.joule", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.energy_mode", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.energy_unit", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.is_activity_state_changeable", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.is_energy_changeable", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.is_energy_mode_changeable", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.is_power_changeable", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.positive_energy_direction", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.power_unit", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.sequence_id", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.direct_control.timestamp", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.adjustment_cause", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.esa.adjustment_cause.grid_optimization", "energy.esa.adjustment_cause.local_optimization", "energy.esa.adjustment_cause.no_adjustment"}},
	{"energy.esa.adjustment_end_cause", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.esa.adjustment_end_cause.cancelled", "energy.esa.adjustment_end_cause.fault", "energy.esa.adjustment_end_cause.normal_completion", "energy.esa.adjustment_end_cause.offline", "energy.esa.adjustment_end_cause.user_opt_out"}},
	{"energy.esa.adjustment_maximum_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.adjustment_maximum_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.adjustment_minimum_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.adjustment_minimum_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.can_generate", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.maximum_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.minimum_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.mode", semreg.ValueQuantity, "unit.one", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.mode_supported", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.opt_out", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.esa.opt_out.grid_opt_out", "energy.esa.opt_out.local_opt_out", "energy.esa.opt_out.no_opt_out", "energy.esa.opt_out.opt_out"}},
	{"energy.esa.request_cause", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.esa.request_cause.grid_optimization", "energy.esa.request_cause.local_optimization"}},
	{"energy.esa.requested_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.requested_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.esa.state", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.esa.state.fault", "energy.esa.state.offline", "energy.esa.state.online", "energy.esa.state.paused", "energy.esa.state.power_adjust_active"}},
	{"energy.esa.type", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.esa.type.battery_storage", "energy.esa.type.cooking", "energy.esa.type.dishwasher", "energy.esa.type.evse", "energy.esa.type.fridge_freezer", "energy.esa.type.home_water_pump", "energy.esa.type.irrigation_water_pump", "energy.esa.type.other", "energy.esa.type.pool_pump", "energy.esa.type.solar_pv", "energy.esa.type.space_cooling", "energy.esa.type.space_heating", "energy.esa.type.space_heating_cooling", "energy.esa.type.washing_machine", "energy.esa.type.water_heating"}},
	{"energy.grid.carbon_intensity", semreg.ValueQuantity, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.grid.carbon_level", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.grid.carbon_level.high", "energy.grid.carbon_level.low", "energy.grid.carbon_level.medium"}},
	{"energy.grid.end", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.grid.local_carbon_intensity", semreg.ValueQuantity, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.grid.local_carbon_level", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.grid.local_carbon_level.high", "energy.grid.local_carbon_level.low", "energy.grid.local_carbon_level.medium"}},
	{"energy.grid.local_generation_available", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.grid.start", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.heartbeat.status", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.heartbeat.status.ok", "energy.heartbeat.status.timeout", "energy.heartbeat.status.unknown"}},
	{"energy.heartbeat.timeout", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, bound("6", 1), nil},
	{"energy.incentive.boundary_lower", semreg.ValueQuantity, "unit.watt", "energy.dimension.tariff", nil, nil, nil},
	{"energy.incentive.boundary_upper", semreg.ValueQuantity, "unit.watt", "energy.dimension.tariff", nil, nil, nil},
	{"energy.incentive.co2_intensity", semreg.ValueQuantity, "unit.kilogram_per_joule", "energy.dimension.tariff", nil, nil, nil},
	{"energy.incentive.incentive_kind", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.incentive.incentive_kind.absolute_cost", "energy.incentive.incentive_kind.co2_emission", "energy.incentive.incentive_kind.relative_cost", "energy.incentive.incentive_kind.renewable_energy_percentage"}},
	{"energy.incentive.incentive_value", semreg.ValueQuantity, "unit.currency_per_joule", "energy.dimension.tariff", nil, nil, nil},
	{"energy.incentive.maximum_slots", semreg.ValueQuantity, "unit.one", "energy.dimension.tariff", nil, nil, nil},
	{"energy.incentive.maximum_tiers", semreg.ValueQuantity, "unit.one", "energy.dimension.tariff", bound("3", 0), nil, nil},
	{"energy.incentive.renewable_ratio", semreg.ValueQuantity, "unit.ratio", "energy.dimension.tariff", bound("0", 0), bound("1", 0), nil},
	{"energy.incentive.slot_end", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.incentive.slot_start", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.incentive.tier_kind", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.incentive.tier_kind.dynamic_cost", "energy.incentive.tier_kind.fixed_cost"}},
	{"energy.itpcm.actor", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.itpcm.committed_power_plan", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.itpcm.itpcm_031", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.itpcm.per", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.itpcm.simple_committed_incentive_table", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.itpcm.slots", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.itpcm.use_case_name", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.itpcm.use_case_version", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.itpcm.value_range_min", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.limit.consumption_active", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.limit.consumption_active_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", bound("0", 0), nil, nil},
	{"energy.limit.consumption_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.limit.consumption_failsafe_duration_minimum", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", bound("72", 2), bound("864", 2), nil},
	{"energy.limit.consumption_failsafe_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", bound("0", 0), nil, nil},
	{"energy.limit.production_active", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.limit.production_active_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", bound("0", 0), nil, nil},
	{"energy.limit.production_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.limit.production_failsafe_duration_minimum", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", bound("72", 2), bound("864", 2), nil},
	{"energy.limit.production_failsafe_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", bound("0", 0), nil, nil},
	{"energy.limit.state", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.limit.state.failsafe", "energy.limit.state.init", "energy.limit.state.limited", "energy.limit.state.unlimited_autonomous", "energy.limit.state.unlimited_controlled"}},
	{"energy.lpc.actor", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpc.committed_power_plan", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpc.heartbeat_counter", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpc.heartbeat_timestamp", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpc.per", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpc.slots", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpc.use_case_name", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpc.use_case_version", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpc.value_range_min", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpp.actor", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpp.committed_power_plan", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpp.heartbeat_counter", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpp.heartbeat_timestamp", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpp.per", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpp.slots", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpp.use_case_name", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpp.use_case_version", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.lpp.value_range_min", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.manager.state", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.manager.state.failure", "energy.manager.state.normal_operation", "energy.manager.state.not_reachable"}},
	{"energy.metering.maximum_quantities", semreg.ValueQuantity, "unit.one", "energy.dimension.subject", nil, nil, nil},
	{"energy.metering.quantity", semreg.ValueQuantity, "unit.joule", "energy.dimension.subject", nil, nil, nil},
	{"energy.metering.timestamp", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.nameplate.consumption_contractual_maximum", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", bound("0", 0), nil, nil},
	{"energy.nameplate.consumption_nominal_maximum", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", bound("0", 0), nil, nil},
	{"energy.nameplate.production_contractual_maximum", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", bound("0", 0), nil, nil},
	{"energy.nameplate.production_nominal_maximum", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", bound("0", 0), nil, nil},
	{"energy.negotiation.request_timeout", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.negotiation.simulation_concurrent", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.negotiation.simulation_cycles_maximum", semreg.ValueQuantity, "unit.one", "energy.dimension.subject", nil, nil, nil},
	{"energy.negotiation.writeable_wait", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.active_duration_min", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.actor", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.alternatives", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.alternatives_count", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.committed_power_plan", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.default_duration", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.earliest_start_time", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.elapsed_slot_time", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.flexibility", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.is_pausable", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.is_stoppable", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.latest_end_time", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.pause_duration_min", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.per", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.remaining_slot_time", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.sequence_id", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.slots", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.supports_reselection", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.total_sequences_count_max", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.use_case_name", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.use_case_version", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.ohpcf.value_range_min", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.podf.actor", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.podf.committed_power_plan", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.podf.per", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.podf.slots", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.podf.use_case_name", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.podf.use_case_version", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.podf.value_range_min", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.poen.actor", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.poen.committed_power_plan", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.poen.per", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.poen.slots", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.poen.use_case_name", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.poen.use_case_version", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.poen.value_range_min", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.preference.balance_label", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.preference.balance_step", semreg.ValueQuantity, "unit.ratio", "energy.dimension.subject", nil, nil, nil},
	{"energy.preference.current_balance", semreg.ValueQuantity, "unit.one", "energy.dimension.subject", nil, nil, nil},
	{"energy.preference.low_power_sensitivity", semreg.ValueQuantity, "unit.one", "energy.dimension.subject", nil, nil, nil},
	{"energy.preference.priorities", semreg.ValueSymbols, "", "energy.dimension.subject", nil, nil, []string{"energy.preference.priorities.comfort", "energy.preference.priorities.efficiency", "energy.preference.priorities.speed", "energy.preference.priorities.water_consumption"}},
	{"energy.price.current", semreg.ValueQuantity, "unit.currency_per_joule", "energy.dimension.tariff", nil, nil, nil},
	{"energy.price.current_description", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.price.current_end", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.price.current_level", semreg.ValueQuantity, "unit.one", "energy.dimension.tariff", bound("-32768", 0), bound("32767", 0), nil},
	{"energy.price.current_start", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.price.forecast_end", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.price.forecast_level", semreg.ValueQuantity, "unit.one", "energy.dimension.tariff", bound("-32768", 0), bound("32767", 0), nil},
	{"energy.price.forecast_price", semreg.ValueQuantity, "unit.currency_per_joule", "energy.dimension.tariff", nil, nil, nil},
	{"energy.price.forecast_start", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.sequence.active_duration_maximum", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.active_duration_minimum", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.earliest_start", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.latest_end", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.pausable", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.pause_duration_maximum", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.pause_duration_minimum", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.scheduled_start", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.slot_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.slot_power_expected", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.slot_power_maximum", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.slot_power_minimum", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.sequence.state", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.sequence.state.completed", "energy.sequence.state.inactive", "energy.sequence.state.invalid", "energy.sequence.state.paused", "energy.sequence.state.pending", "energy.sequence.state.running", "energy.sequence.state.scheduled", "energy.sequence.state.scheduled_paused"}},
	{"energy.sequence.stoppable", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.constraint_maximum_slots", semreg.ValueQuantity, "unit.one", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.constraint_minimum_slot_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.constraint_slot_value_step", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.earliest_start", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.end", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.identifier", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.latest_end", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.pausable", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_adjustment_maximum_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_adjustment_maximum_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_adjustment_minimum_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_adjustment_minimum_power", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_default_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_elapsed", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_energy_nominal", semreg.ValueQuantity, "unit.joule", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_maximum_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_maximum_pause", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_minimum_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_minimum_pause", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_pausable", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_power_maximum", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_power_minimum", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_power_nominal", semreg.ValueQuantity, "unit.watt", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_remaining", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_start", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.slot_state", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.start", semreg.ValueTime, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.series.update_reason", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.series.update_reason.grid_optimization", "energy.series.update_reason.internal_optimization", "energy.series.update_reason.local_optimization"}},
	{"energy.tariff.block_mode", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.block_mode.combined", "energy.tariff.block_mode.individual", "energy.tariff.block_mode.no_block"}},
	{"energy.tariff.calendar_period_pattern", semreg.ValueBoolean, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.calendar_period_start", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.component_apparent_power_threshold", semreg.ValueQuantity, "unit.volt_ampere", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.component_auxiliary_load", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.component_auxiliary_load.none", "energy.tariff.component_auxiliary_load.off", "energy.tariff.component_auxiliary_load.on"}},
	{"energy.tariff.component_friendly_credit", semreg.ValueBoolean, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.component_label", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.component_peak_period", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.component_peak_period.high", "energy.tariff.component_peak_period.low", "energy.tariff.component_peak_period.medium", "energy.tariff.component_peak_period.unused"}},
	{"energy.tariff.component_power_threshold", semreg.ValueQuantity, "unit.watt", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.component_predicted", semreg.ValueBoolean, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.component_price", semreg.ValueQuantity, "unit.currency_per_joule", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.component_price_kind", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.component_price_kind.critical", "energy.tariff.component_price_kind.incentive", "energy.tariff.component_price_kind.incentive_signal", "energy.tariff.component_price_kind.standard", "energy.tariff.component_price_kind.virtual"}},
	{"energy.tariff.component_threshold", semreg.ValueQuantity, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.currency", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.current_components", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.current_day", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.current_day_entry", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.current_day_entry_date", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.day_entry_duration", semreg.ValueQuantity, "unit.second", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.day_entry_randomization_offset", semreg.ValueQuantity, "unit.second", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.day_entry_start", semreg.ValueQuantity, "unit.second", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.day_pattern_days", semreg.ValueSymbols, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.day_pattern_days.friday", "energy.tariff.day_pattern_days.monday", "energy.tariff.day_pattern_days.saturday", "energy.tariff.day_pattern_days.sunday", "energy.tariff.day_pattern_days.thursday", "energy.tariff.day_pattern_days.tuesday", "energy.tariff.day_pattern_days.wednesday"}},
	{"energy.tariff.day_pattern_entry", semreg.ValueBoolean, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.direction", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.direction.consumption", "energy.tariff.direction.production"}},
	{"energy.tariff.individual_day_date", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.individual_day_entry", semreg.ValueBoolean, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.individual_day_kind", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.individual_day_kind.dynamic", "energy.tariff.individual_day_kind.event", "energy.tariff.individual_day_kind.holiday", "energy.tariff.individual_day_kind.standard"}},
	{"energy.tariff.label", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.next_components", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.next_day", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.next_day_entry", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.next_day_entry_date", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.period_component", semreg.ValueBoolean, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.period_entry", semreg.ValueBoolean, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.period_label", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.price_kind", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.price_kind.energy", "energy.tariff.price_kind.energy_and_transmission", "energy.tariff.price_kind.transmission"}},
	{"energy.tariff.provider", semreg.ValueText, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.randomization_kind", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.randomization_kind.fixed", "energy.tariff.randomization_kind.none", "energy.tariff.randomization_kind.random", "energy.tariff.randomization_kind.random_negative", "energy.tariff.randomization_kind.random_positive"}},
	{"energy.tariff.randomization_offset", semreg.ValueQuantity, "unit.second", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.start", semreg.ValueTime, "", "energy.dimension.tariff", nil, nil, nil},
	{"energy.tariff.status", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.status.committed", "energy.tariff.status.preliminary"}},
	{"energy.tariff.unit_basis", semreg.ValueSymbol, "", "energy.dimension.tariff", nil, nil, []string{"energy.tariff.unit_basis.apparent_energy", "energy.tariff.unit_basis.energy"}},
	{"energy.task.description", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.direct_control_related", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.elapsed_time", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.hvac_related", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.hvac_related_overrun_id", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.job_id", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.job_source", semreg.ValueSymbol, "", "energy.dimension.subject", nil, nil, []string{"energy.task.job_source.external_configuration", "energy.task.job_source.internal_mechanism", "energy.task.job_source.user_interaction"}},
	{"energy.task.job_state", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.jobs_active", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.label", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.load_control_releated", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.load_control_releated_event_id", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.power_sequences_related", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.power_sequences_related_sequence_id", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.remaining_time", semreg.ValueQuantity, "unit.second", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.remote_controllable", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.smart_energy_management_ps_related", semreg.ValueBoolean, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.smart_energy_management_ps_related_sequence_id", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.task.timestamp", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.tout.actor", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.tout.committed_power_plan", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.tout.per", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.tout.slots", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.tout.use_case_name", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.tout.use_case_version", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
	{"energy.tout.value_range_min", semreg.ValueText, "", "energy.dimension.subject", nil, nil, nil},
}

var dimensions = map[semreg.DefinitionID]semreg.ValueKind{
	"energy.dimension.subject": semreg.ValueText,
	"energy.dimension.tariff":  semreg.ValueText,
}

var fieldQualifiers = map[semreg.DefinitionID][]semreg.DefinitionID{
	"energy.direct_control.activity_state":                       []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.direct_control.energy":                               []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.direct_control.energy_mode":                          []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.direct_control.is_activity_state_changeable":         []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.direct_control.is_energy_changeable":                 []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.direct_control.is_energy_mode_changeable":            []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.direct_control.is_power_changeable":                  []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.direct_control.power":                                []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.direct_control.sequence_id":                          []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.direct_control.timestamp":                            []semreg.DefinitionID{"energy.dimension.direct_control_timestamp"},
	"energy.esa.adjustment_maximum_duration":                     []semreg.DefinitionID{"energy.dimension.adjustment_range"},
	"energy.esa.adjustment_maximum_power":                        []semreg.DefinitionID{"energy.dimension.adjustment_range"},
	"energy.esa.adjustment_minimum_duration":                     []semreg.DefinitionID{"energy.dimension.adjustment_range"},
	"energy.esa.adjustment_minimum_power":                        []semreg.DefinitionID{"energy.dimension.adjustment_range"},
	"energy.grid.carbon_intensity":                               []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.grid.carbon_level":                                   []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.grid.end":                                            []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.grid.local_carbon_intensity":                         []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.grid.local_carbon_level":                             []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.grid.start":                                          []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.incentive.boundary_lower":                            []semreg.DefinitionID{"energy.dimension.boundary", "energy.dimension.slot", "energy.dimension.tier"},
	"energy.incentive.boundary_upper":                            []semreg.DefinitionID{"energy.dimension.boundary", "energy.dimension.slot", "energy.dimension.tier"},
	"energy.incentive.co2_intensity":                             []semreg.DefinitionID{"energy.dimension.incentive", "energy.dimension.slot", "energy.dimension.tier"},
	"energy.incentive.incentive_kind":                            []semreg.DefinitionID{"energy.dimension.incentive", "energy.dimension.slot", "energy.dimension.tier"},
	"energy.incentive.incentive_value":                           []semreg.DefinitionID{"energy.dimension.incentive", "energy.dimension.slot", "energy.dimension.tier"},
	"energy.incentive.renewable_ratio":                           []semreg.DefinitionID{"energy.dimension.incentive", "energy.dimension.slot", "energy.dimension.tier"},
	"energy.incentive.slot_end":                                  []semreg.DefinitionID{"energy.dimension.slot"},
	"energy.incentive.slot_start":                                []semreg.DefinitionID{"energy.dimension.slot"},
	"energy.incentive.tier_kind":                                 []semreg.DefinitionID{"energy.dimension.slot", "energy.dimension.tier"},
	"energy.metering.quantity":                                   []semreg.DefinitionID{"energy.dimension.component"},
	"energy.preference.balance_label":                            []semreg.DefinitionID{"energy.dimension.balance"},
	"energy.preference.balance_step":                             []semreg.DefinitionID{"energy.dimension.balance"},
	"energy.price.forecast_end":                                  []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.price.forecast_level":                                []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.price.forecast_price":                                []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.price.forecast_start":                                []semreg.DefinitionID{"energy.dimension.forecast_slot"},
	"energy.sequence.active_duration_maximum":                    []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.sequence.active_duration_minimum":                    []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.sequence.earliest_start":                             []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.sequence.latest_end":                                 []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.sequence.pausable":                                   []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.sequence.pause_duration_maximum":                     []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.sequence.pause_duration_minimum":                     []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.sequence.scheduled_start":                            []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.sequence.slot_duration":                              []semreg.DefinitionID{"energy.dimension.sequence", "energy.dimension.slot"},
	"energy.sequence.slot_power_expected":                        []semreg.DefinitionID{"energy.dimension.sequence", "energy.dimension.slot"},
	"energy.sequence.slot_power_maximum":                         []semreg.DefinitionID{"energy.dimension.sequence", "energy.dimension.slot"},
	"energy.sequence.slot_power_minimum":                         []semreg.DefinitionID{"energy.dimension.sequence", "energy.dimension.slot"},
	"energy.sequence.state":                                      []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.sequence.stoppable":                                  []semreg.DefinitionID{"energy.dimension.sequence"},
	"energy.series.constraint_maximum_slots":                     []semreg.DefinitionID{"energy.dimension.series"},
	"energy.series.constraint_minimum_slot_duration":             []semreg.DefinitionID{"energy.dimension.series"},
	"energy.series.constraint_slot_value_step":                   []semreg.DefinitionID{"energy.dimension.series"},
	"energy.series.earliest_start":                               []semreg.DefinitionID{"energy.dimension.series"},
	"energy.series.end":                                          []semreg.DefinitionID{"energy.dimension.series"},
	"energy.series.identifier":                                   []semreg.DefinitionID{"energy.dimension.series"},
	"energy.series.latest_end":                                   []semreg.DefinitionID{"energy.dimension.series"},
	"energy.series.pausable":                                     []semreg.DefinitionID{"energy.dimension.series"},
	"energy.series.slot_adjustment_maximum_duration":             []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_adjustment_maximum_power":                []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_adjustment_minimum_duration":             []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_adjustment_minimum_power":                []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_default_duration":                        []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_duration":                                []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_elapsed":                                 []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_energy_nominal":                          []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_maximum_duration":                        []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_maximum_pause":                           []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_minimum_duration":                        []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_minimum_pause":                           []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_pausable":                                []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_power_maximum":                           []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_power_minimum":                           []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_power_nominal":                           []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_remaining":                               []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_start":                                   []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.slot_state":                                   []semreg.DefinitionID{"energy.dimension.series", "energy.dimension.slot"},
	"energy.series.start":                                        []semreg.DefinitionID{"energy.dimension.series"},
	"energy.series.update_reason":                                []semreg.DefinitionID{"energy.dimension.series"},
	"energy.tariff.calendar_period_pattern":                      []semreg.DefinitionID{"energy.dimension.calendar_period", "energy.dimension.day_pattern"},
	"energy.tariff.calendar_period_start":                        []semreg.DefinitionID{"energy.dimension.calendar_period"},
	"energy.tariff.component_apparent_power_threshold":           []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.component_auxiliary_load":                     []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.component_friendly_credit":                    []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.component_label":                              []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.component_peak_period":                        []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.component_power_threshold":                    []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.component_predicted":                          []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.component_price":                              []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.component_price_kind":                         []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.component_threshold":                          []semreg.DefinitionID{"energy.dimension.component"},
	"energy.tariff.day_entry_duration":                           []semreg.DefinitionID{"energy.dimension.day_entry"},
	"energy.tariff.day_entry_randomization_offset":               []semreg.DefinitionID{"energy.dimension.day_entry"},
	"energy.tariff.day_entry_start":                              []semreg.DefinitionID{"energy.dimension.day_entry"},
	"energy.tariff.day_pattern_days":                             []semreg.DefinitionID{"energy.dimension.day_pattern"},
	"energy.tariff.day_pattern_entry":                            []semreg.DefinitionID{"energy.dimension.day_entry", "energy.dimension.day_pattern"},
	"energy.tariff.individual_day_date":                          []semreg.DefinitionID{"energy.dimension.individual_day"},
	"energy.tariff.individual_day_entry":                         []semreg.DefinitionID{"energy.dimension.day_entry", "energy.dimension.individual_day"},
	"energy.tariff.individual_day_kind":                          []semreg.DefinitionID{"energy.dimension.individual_day"},
	"energy.tariff.period_component":                             []semreg.DefinitionID{"energy.dimension.component", "energy.dimension.tariff_period"},
	"energy.tariff.period_entry":                                 []semreg.DefinitionID{"energy.dimension.day_entry", "energy.dimension.tariff_period"},
	"energy.tariff.period_label":                                 []semreg.DefinitionID{"energy.dimension.tariff_period"},
	"energy.task.description":                                    []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.direct_control_related":                         []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.elapsed_time":                                   []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.hvac_related":                                   []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.hvac_related_overrun_id":                        []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.job_id":                                         []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.job_source":                                     []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.job_state":                                      []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.label":                                          []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.load_control_releated":                          []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.load_control_releated_event_id":                 []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.power_sequences_related":                        []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.power_sequences_related_sequence_id":            []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.remaining_time":                                 []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.smart_energy_management_ps_related":             []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.smart_energy_management_ps_related_sequence_id": []semreg.DefinitionID{"energy.dimension.job"},
	"energy.task.timestamp":                                      []semreg.DefinitionID{"energy.dimension.job"},
}

var qualifierSymbols = map[semreg.DefinitionID][]string{
	"energy.dimension.series": []string{"charging_plan", "charging_power_limit_curve", "committed_power_plan", "consumption_limit_curve", "fallback_consumption_limit_curve", "fallback_production_limit_curve", "forecast", "power_demand_forecast", "preliminary_power_plan", "production_limit_curve"},
}

var qualifierKinds = map[semreg.DefinitionID]semreg.ValueKind{
	"energy.dimension.adjustment_range":         semreg.ValueQuantity,
	"energy.dimension.balance":                  semreg.ValueQuantity,
	"energy.dimension.boundary":                 semreg.ValueQuantity,
	"energy.dimension.calendar_period":          semreg.ValueQuantity,
	"energy.dimension.component":                semreg.ValueQuantity,
	"energy.dimension.day_entry":                semreg.ValueQuantity,
	"energy.dimension.day_pattern":              semreg.ValueQuantity,
	"energy.dimension.direct_control_timestamp": semreg.ValueText,
	"energy.dimension.forecast_slot":            semreg.ValueQuantity,
	"energy.dimension.incentive":                semreg.ValueQuantity,
	"energy.dimension.individual_day":           semreg.ValueQuantity,
	"energy.dimension.job":                      semreg.ValueText,
	"energy.dimension.sequence":                 semreg.ValueText,
	"energy.dimension.series":                   semreg.ValueSymbol,
	"energy.dimension.slot":                     semreg.ValueQuantity,
	"energy.dimension.tariff_period":            semreg.ValueQuantity,
	"energy.dimension.tier":                     semreg.ValueQuantity,
}

var services = map[semreg.DefinitionID]semreg.DefinitionID{
	"energy.service.subject": "energy.dimension.subject",
	"energy.service.tariff":  "energy.dimension.tariff",
}

var capabilities = map[semreg.DefinitionID]capabilitySpec{
	"energy.capability.adjust_power":                      {"energy.service.subject", nil},
	"energy.capability.adjust_start_time":                 {"energy.service.subject", nil},
	"energy.capability.modify_series_slot":                {"energy.service.subject", nil},
	"energy.capability.read.subject":                      {"energy.service.subject", nil},
	"energy.capability.read.tariff":                       {"energy.service.tariff", nil},
	"energy.capability.schedule_sequence":                 {"energy.service.subject", nil},
	"energy.capability.set_consumption_failsafe":          {"energy.service.subject", nil},
	"energy.capability.set_consumption_limit":             {"energy.service.subject", nil},
	"energy.capability.set_direct_control_activity_state": {"energy.service.subject", nil},
	"energy.capability.set_direct_control_energy":         {"energy.service.subject", nil},
	"energy.capability.set_direct_control_energy_mode":    {"energy.service.subject", nil},
	"energy.capability.set_direct_control_power":          {"energy.service.subject", nil},
	"energy.capability.set_energy_balance":                {"energy.service.subject", nil},
	"energy.capability.set_esa_mode":                      {"energy.service.subject", nil},
	"energy.capability.set_esa_state":                     {"energy.service.subject", nil},
	"energy.capability.set_local_generation_available":    {"energy.service.subject", nil},
	"energy.capability.set_low_power_sensitivity":         {"energy.service.subject", nil},
	"energy.capability.set_production_failsafe":           {"energy.service.subject", nil},
	"energy.capability.set_production_limit":              {"energy.service.subject", nil},
	"energy.capability.set_sequence_state":                {"energy.service.subject", nil},
	"energy.capability.write_incentive_slot":              {"energy.service.tariff", nil},
}

var operations = map[semreg.DefinitionID]operationSpec{
	"energy.operation.adjust_power":                      {"energy.capability.adjust_power", "energy.esa.requested_power", "energy.effect.adjust_power"},
	"energy.operation.adjust_start_time":                 {"energy.capability.adjust_start_time", "energy.series.start", "energy.effect.adjust_start_time"},
	"energy.operation.modify_series_slot":                {"energy.capability.modify_series_slot", "energy.series.slot_power_nominal", "energy.effect.modify_series_slot"},
	"energy.operation.schedule_sequence":                 {"energy.capability.schedule_sequence", "energy.sequence.scheduled_start", "energy.effect.schedule_sequence"},
	"energy.operation.set_consumption_failsafe":          {"energy.capability.set_consumption_failsafe", "energy.limit.consumption_failsafe_power", "energy.effect.set_consumption_failsafe"},
	"energy.operation.set_consumption_limit":             {"energy.capability.set_consumption_limit", "energy.limit.consumption_active_power", "energy.effect.set_consumption_limit"},
	"energy.operation.set_direct_control_activity_state": {"energy.capability.set_direct_control_activity_state", "energy.direct_control.activity_state", "energy.effect.set_direct_control_activity_state"},
	"energy.operation.set_direct_control_energy":         {"energy.capability.set_direct_control_energy", "energy.direct_control.energy", "energy.effect.set_direct_control_energy"},
	"energy.operation.set_direct_control_energy_mode":    {"energy.capability.set_direct_control_energy_mode", "energy.direct_control.energy_mode", "energy.effect.set_direct_control_energy_mode"},
	"energy.operation.set_direct_control_power":          {"energy.capability.set_direct_control_power", "energy.direct_control.power", "energy.effect.set_direct_control_power"},
	"energy.operation.set_energy_balance":                {"energy.capability.set_energy_balance", "energy.preference.current_balance", "energy.effect.set_energy_balance"},
	"energy.operation.set_esa_mode":                      {"energy.capability.set_esa_mode", "energy.esa.mode", "energy.effect.set_esa_mode"},
	"energy.operation.set_esa_state":                     {"energy.capability.set_esa_state", "energy.esa.state", "energy.effect.set_esa_state"},
	"energy.operation.set_local_generation_available":    {"energy.capability.set_local_generation_available", "energy.grid.local_generation_available", "energy.effect.set_local_generation_available"},
	"energy.operation.set_low_power_sensitivity":         {"energy.capability.set_low_power_sensitivity", "energy.preference.low_power_sensitivity", "energy.effect.set_low_power_sensitivity"},
	"energy.operation.set_production_failsafe":           {"energy.capability.set_production_failsafe", "energy.limit.production_failsafe_power", "energy.effect.set_production_failsafe"},
	"energy.operation.set_production_limit":              {"energy.capability.set_production_limit", "energy.limit.production_active_power", "energy.effect.set_production_limit"},
	"energy.operation.set_sequence_state":                {"energy.capability.set_sequence_state", "energy.sequence.state", "energy.effect.set_sequence_state"},
	"energy.operation.write_incentive_slot":              {"energy.capability.write_incentive_slot", "energy.incentive.incentive_value", "energy.effect.write_incentive_slot"},
}

var operationParameters = map[semreg.DefinitionID][]semreg.DefinitionID{
	"energy.operation.adjust_power":             []semreg.DefinitionID{"energy.esa.request_cause", "energy.esa.requested_duration"},
	"energy.operation.adjust_start_time":        []semreg.DefinitionID{"energy.esa.request_cause"},
	"energy.operation.modify_series_slot":       []semreg.DefinitionID{"energy.esa.request_cause", "energy.series.identifier", "energy.series.slot_duration", "energy.series.slot_power_maximum"},
	"energy.operation.set_consumption_failsafe": []semreg.DefinitionID{"energy.limit.consumption_failsafe_duration_minimum"},
	"energy.operation.set_consumption_limit":    []semreg.DefinitionID{"energy.limit.consumption_active", "energy.limit.consumption_duration"},
	"energy.operation.set_esa_state":            []semreg.DefinitionID{"energy.esa.request_cause", "energy.esa.requested_duration"},
	"energy.operation.set_production_failsafe":  []semreg.DefinitionID{"energy.limit.production_failsafe_duration_minimum"},
	"energy.operation.set_production_limit":     []semreg.DefinitionID{"energy.limit.production_active", "energy.limit.production_duration"},
	"energy.operation.write_incentive_slot":     []semreg.DefinitionID{"energy.incentive.boundary_lower", "energy.incentive.boundary_upper", "energy.incentive.incentive_kind", "energy.incentive.slot_end", "energy.incentive.slot_start", "energy.incentive.tier_kind"},
}

var unionEvents = []semreg.DefinitionID{"energy.event.grid_conditions_changed", "energy.event.price_changed"}

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
