package thermal

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Approved thermal v1 fixture preimage: fixtures/v1/thermal-hvac-pack-vectors.json.
// Freeze the accepted rename only; compute both digests rather than embed a digest.
const approvedThermalFixture = `{
  "contract":"helianthus.semantic.pack.thermal.acceptance/v1","pack_contract":"helianthus.pack.thermal/v1",
  "catalog":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"kernel_contract":"helianthus.semantic.kernel/v1","status":"accepted",
    "domain_catalog":[{"id":"thermal_hvac","pack":"helianthus.pack.thermal","state":"accepted"},{"id":"pv_inverter","pack":"helianthus.pack.pv","state":"follow_on"},{"id":"storage_bms","pack":"helianthus.pack.storage","state":"follow_on"},{"id":"evse","pack":"helianthus.pack.evse","state":"follow_on"},{"id":"infrastructure","pack":"helianthus.pack.infrastructure","state":"follow_on"}],
    "inputs":[{"id":"semantic_kernel","revision":"da5ab4415d3bec73f9572aec1c495a6cdcbcba47"},{"id":"ebus","revision":"ef076cb03e6cd5612f5dcbe7839e00ab4ee666c9"},{"id":"gree_can","revision":"665a5f22f78c349b5e3063bda158af250a3f44b6"},{"id":"eebus_ledger","revision":"81cd647c834e88c88a3c82ef9fbc5a0194f6b0f1"},{"id":"eebus_m625_donor","revision":"cedf238e34f879815ba773e9cd76b2b31c2822a3"},{"id":"matter_draft","revision":"29b4768a513cf566011ab8cd60df1bc495204953"}],
    "lifecycle_axes":["source_epoch","driver_generation","semantic_revision","capability_qualification","activation","generation_fencing","partial_updates","stale_unknown_evidence","last_known_good_retention"],"publication_withdrawal_policy":{"admission":["exact_source_epoch","current_driver_generation","exact_semantic_revision","qualified_active_capability"],"stale_generation":"reject","partial_publication":"supplied_valid_only_preserve_permitted_last_known_good","withdrawal":"explicit_generation_fenced","evidence":"stale_unknown_never_promote_or_authorize","tombstone":"retained_non_actionable"},"loss_dispositions":["native_enum","precision_range","multiple_zones_faces","schedules","vendor_extensions","unavailable_fields","conflicting_sources","unsupported_operations"],
    "definitions":{"fields":[
        {"id":"thermal.measurement.air_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.air_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.zone","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":10},
        {"id":"thermal.measurement.humidity","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.humidity","version":"1.0.0"},"kind":"quantity","unit":"unit.ratio","dimension":"thermal.dimension.zone","bounds":{"minimum":{"coefficient":"0","exponent10":0},"maximum":{"coefficient":"1","exponent10":0}},"optional":true,"order":20},
        {"id":"thermal.setpoint.temperature_setpoint","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.setpoint.temperature_setpoint","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.zone","bounds":{"minimum":{"coefficient":"22315","exponent10":-2},"maximum":{"coefficient":"42315","exponent10":-2}},"optional":false,"order":30},
        {"id":"thermal.mode.zone","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.mode.zone","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.zone","symbols":["thermal.mode.zone.auto","thermal.mode.zone.comfort","thermal.mode.zone.eco","thermal.mode.zone.off"],"optional":true,"order":40},
        {"id":"thermal.measurement.circuit_flow_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.circuit_flow_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.circuit","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":50},
        {"id":"thermal.measurement.flow_rate","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.flow_rate","version":"1.0.0"},"kind":"quantity","unit":"unit.cubic_metre_per_second","dimension":"thermal.dimension.circuit","bounds":{"minimum":{"coefficient":"0","exponent10":0},"maximum":{"coefficient":"2","exponent10":-1}},"optional":true,"order":60},
        {"id":"thermal.measurement.dhw_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.dhw_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.dhw","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":70},
        {"id":"thermal.setpoint.dhw_temperature_setpoint","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.setpoint.dhw_temperature_setpoint","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.dhw","bounds":{"minimum":{"coefficient":"22315","exponent10":-2},"maximum":{"coefficient":"42315","exponent10":-2}},"optional":true,"order":80},
        {"id":"thermal.demand.level","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.demand.level","version":"1.0.0"},"kind":"quantity","unit":"unit.ratio","dimension":"thermal.dimension.ventilation","bounds":{"minimum":{"coefficient":"0","exponent10":0},"maximum":{"coefficient":"1","exponent10":0}},"optional":true,"order":90},
        {"id":"thermal.measurement.outdoor_air_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.outdoor_air_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.system","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":100},
        {"id":"thermal.measurement.system_flow_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.system_flow_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.system","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":110},
        {"id":"thermal.mode.system","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.mode.system","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.system","symbols":["thermal.mode.system.auto","thermal.mode.system.cool","thermal.mode.system.heat","thermal.mode.system.off"],"optional":true,"order":120},
        {"id":"thermal.action.state","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.action.state","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.system","symbols":["thermal.action.state.cooling","thermal.action.state.dhw","thermal.action.state.heating","thermal.action.state.idle","thermal.action.state.ventilating"],"optional":true,"order":130},
        {"id":"thermal.status.operation","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.status.operation","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.system","symbols":["thermal.status.operation.active","thermal.status.operation.idle"],"optional":true,"order":140},
        {"id":"thermal.status.fault","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.status.fault","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.system","symbols":["thermal.status.fault.clear","thermal.status.fault.present"],"optional":true,"order":150},
        {"id":"thermal.status.maintenance","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.status.maintenance","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.system","symbols":["thermal.status.maintenance.clear","thermal.status.maintenance.due"],"optional":true,"order":160},
        {"id":"thermal.measurement.heat_generator_flow_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.heat_generator_flow_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.heat_generator","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":170},
        {"id":"thermal.measurement.heat_generator_return_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.heat_generator_return_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.heat_generator","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":180},
        {"id":"thermal.measurement.thermal_power","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.thermal_power","version":"1.0.0"},"kind":"quantity","unit":"unit.watt","dimension":"thermal.dimension.heat_generator","bounds":{"minimum":{"coefficient":"-1","exponent10":6},"maximum":{"coefficient":"1","exponent10":6}},"optional":true,"order":190},
        {"id":"thermal.energy.heat_output","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.energy.heat_output","version":"1.0.0"},"kind":"quantity","unit":"unit.joule","dimension":"thermal.dimension.heat_generator","bounds":{"minimum":{"coefficient":"0","exponent10":0},"maximum":{"coefficient":"36","exponent10":17}},"optional":true,"order":200},
        {"id":"thermal.measurement.water_pressure","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.water_pressure","version":"1.0.0"},"kind":"quantity","unit":"unit.pascal","dimension":"thermal.dimension.heat_generator","bounds":{"minimum":{"coefficient":"-101325","exponent10":0},"maximum":{"coefficient":"1","exponent10":7}},"optional":true,"order":210},
        {"id":"thermal.status.heat_generator_fault","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.status.heat_generator_fault","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.heat_generator","symbols":["thermal.status.heat_generator_fault.clear","thermal.status.heat_generator_fault.present"],"optional":true,"order":220},
        {"id":"thermal.status.heat_generator_operation","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.status.heat_generator_operation","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.heat_generator","symbols":["thermal.status.heat_generator_operation.active","thermal.status.heat_generator_operation.idle"],"optional":true,"order":230},
        {"id":"thermal.measurement.store_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.store_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.thermal_store","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":240},
        {"id":"thermal.mode.terminal_unit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.mode.terminal_unit","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.terminal_unit","symbols":["thermal.mode.terminal_unit.auto","thermal.mode.terminal_unit.cool","thermal.mode.terminal_unit.dry","thermal.mode.terminal_unit.fan_only","thermal.mode.terminal_unit.heat","thermal.mode.terminal_unit.off"],"optional":true,"order":250},
        {"id":"thermal.status.terminal_unit_fault","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.status.terminal_unit_fault","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.terminal_unit","symbols":["thermal.status.terminal_unit_fault.clear","thermal.status.terminal_unit_fault.present"],"optional":true,"order":260},
        {"id":"thermal.status.terminal_unit_operation","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.status.terminal_unit_operation","version":"1.0.0"},"kind":"symbol","dimension":"thermal.dimension.terminal_unit","symbols":["thermal.status.terminal_unit_operation.active","thermal.status.terminal_unit_operation.idle"],"optional":true,"order":270},
        {"id":"thermal.measurement.refrigerant_high_side_pressure","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.refrigerant_high_side_pressure","version":"1.0.0"},"kind":"quantity","unit":"unit.pascal","dimension":"thermal.dimension.refrigerant_circuit","bounds":{"minimum":{"coefficient":"0","exponent10":0},"maximum":{"coefficient":"1","exponent10":8}},"optional":true,"order":280},
        {"id":"thermal.measurement.refrigerant_low_side_pressure","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.refrigerant_low_side_pressure","version":"1.0.0"},"kind":"quantity","unit":"unit.pascal","dimension":"thermal.dimension.refrigerant_circuit","bounds":{"minimum":{"coefficient":"0","exponent10":0},"maximum":{"coefficient":"1","exponent10":8}},"optional":true,"order":290},
        {"id":"thermal.measurement.refrigerant_condensing_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.refrigerant_condensing_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.refrigerant_circuit","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":300},
        {"id":"thermal.measurement.refrigerant_evaporating_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.refrigerant_evaporating_temperature","version":"1.0.0"},"kind":"quantity","unit":"unit.kelvin","dimension":"thermal.dimension.refrigerant_circuit","bounds":{"minimum":{"coefficient":"17315","exponent10":-2},"maximum":{"coefficient":"47315","exponent10":-2}},"optional":true,"order":310},
        {"id":"thermal.measurement.compressor_speed","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.measurement.compressor_speed","version":"1.0.0"},"kind":"quantity","unit":"unit.reciprocal_second","dimension":"thermal.dimension.compressor","bounds":{"minimum":{"coefficient":"0","exponent10":0},"maximum":{"coefficient":"1","exponent10":3}},"optional":true,"order":320}],
      "fact_dimensions":[
        {"id":"thermal.dimension.system","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.system","version":"1.0.0"},"value_kind":"text","order":10},
        {"id":"thermal.dimension.zone","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.zone","version":"1.0.0"},"value_kind":"text","order":20},
        {"id":"thermal.dimension.circuit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.circuit","version":"1.0.0"},"value_kind":"text","order":30},
        {"id":"thermal.dimension.dhw","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.dhw","version":"1.0.0"},"value_kind":"text","order":40},
        {"id":"thermal.dimension.ventilation","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.ventilation","version":"1.0.0"},"value_kind":"text","order":50},
        {"id":"thermal.dimension.heat_generator","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.heat_generator","version":"1.0.0"},"value_kind":"text","order":60},
        {"id":"thermal.dimension.thermal_store","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.thermal_store","version":"1.0.0"},"value_kind":"text","order":70},
        {"id":"thermal.dimension.terminal_unit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.terminal_unit","version":"1.0.0"},"value_kind":"text","order":80},
        {"id":"thermal.dimension.refrigerant_circuit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.refrigerant_circuit","version":"1.0.0"},"value_kind":"text","order":90},
        {"id":"thermal.dimension.compressor","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.dimension.compressor","version":"1.0.0"},"value_kind":"text","order":100}],
      "services":[
        {"id":"thermal.service.system","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.system","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.system","order":10},
        {"id":"thermal.service.zone","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.zone","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.zone","order":20},
        {"id":"thermal.service.circuit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.circuit","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.circuit","order":30},
        {"id":"thermal.service.dhw","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.dhw","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.dhw","order":40},
        {"id":"thermal.service.ventilation","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.ventilation","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.ventilation","order":50},
        {"id":"thermal.service.heat_generator","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.heat_generator","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.heat_generator","order":60},
        {"id":"thermal.service.thermal_store","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.thermal_store","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.thermal_store","order":70},
        {"id":"thermal.service.terminal_unit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.terminal_unit","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.terminal_unit","order":80},
        {"id":"thermal.service.refrigerant_circuit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.refrigerant_circuit","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.refrigerant_circuit","order":90},
        {"id":"thermal.service.compressor","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.service.compressor","version":"1.0.0"},"fact_key_dimension":"thermal.dimension.compressor","order":100}],
      "relations":[
        {"id":"thermal.relation.system_member","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.relation.system_member","version":"1.0.0"},"from":["thermal.dimension.system"],"to":["thermal.dimension.zone","thermal.dimension.circuit","thermal.dimension.dhw","thermal.dimension.ventilation","thermal.dimension.heat_generator","thermal.dimension.thermal_store","thermal.dimension.terminal_unit","thermal.dimension.refrigerant_circuit"],"cardinality":"one_to_many","order":10},
        {"id":"thermal.relation.fluid_supply","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.relation.fluid_supply","version":"1.0.0"},"from":["thermal.dimension.heat_generator","thermal.dimension.thermal_store","thermal.dimension.circuit"],"to":["thermal.dimension.circuit","thermal.dimension.thermal_store","thermal.dimension.zone","thermal.dimension.dhw","thermal.dimension.terminal_unit"],"cardinality":"many_to_many","order":20},
        {"id":"thermal.relation.air_supply","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.relation.air_supply","version":"1.0.0"},"from":["thermal.dimension.terminal_unit","thermal.dimension.ventilation"],"to":["thermal.dimension.zone"],"cardinality":"many_to_many","order":30},
        {"id":"thermal.relation.refrigerant_circuit_member","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.relation.refrigerant_circuit_member","version":"1.0.0"},"from":["thermal.dimension.refrigerant_circuit"],"to":["thermal.dimension.heat_generator","thermal.dimension.terminal_unit"],"cardinality":"one_to_many","order":40},
        {"id":"thermal.relation.generator_compressor","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.relation.generator_compressor","version":"1.0.0"},"from":["thermal.dimension.heat_generator"],"to":["thermal.dimension.compressor"],"cardinality":"one_to_many","order":50}],
      "capabilities":[
        {"id":"thermal.capability.read.system","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.system","version":"1.0.0"},"service":"thermal.service.system","activation":"qualified_current_binding","order":10},
        {"id":"thermal.capability.read.zone","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.zone","version":"1.0.0"},"service":"thermal.service.zone","activation":"qualified_current_binding","order":20},
        {"id":"thermal.capability.read.circuit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.circuit","version":"1.0.0"},"service":"thermal.service.circuit","activation":"qualified_current_binding","order":30},
        {"id":"thermal.capability.read.dhw","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.dhw","version":"1.0.0"},"service":"thermal.service.dhw","activation":"qualified_current_binding","order":40},
        {"id":"thermal.capability.read.ventilation","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.ventilation","version":"1.0.0"},"service":"thermal.service.ventilation","activation":"qualified_current_binding","order":50},
        {"id":"thermal.capability.set_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.set_temperature","version":"1.0.0"},"service":"thermal.service.zone","activation":"qualified_current_binding","constraints":["thermal.setpoint.temperature_setpoint"],"order":60},
        {"id":"thermal.capability.set_mode","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.set_mode","version":"1.0.0"},"service":"thermal.service.system","activation":"qualified_current_binding","constraints":["thermal.mode.system"],"order":70},
        {"id":"thermal.capability.read.heat_generator","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.heat_generator","version":"1.0.0"},"service":"thermal.service.heat_generator","activation":"qualified_current_binding","order":80},
        {"id":"thermal.capability.read.thermal_store","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.thermal_store","version":"1.0.0"},"service":"thermal.service.thermal_store","activation":"qualified_current_binding","order":90},
        {"id":"thermal.capability.read.terminal_unit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.terminal_unit","version":"1.0.0"},"service":"thermal.service.terminal_unit","activation":"qualified_current_binding","order":100},
        {"id":"thermal.capability.read.refrigerant_circuit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.refrigerant_circuit","version":"1.0.0"},"service":"thermal.service.refrigerant_circuit","activation":"qualified_current_binding","order":110},
        {"id":"thermal.capability.read.compressor","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.capability.read.compressor","version":"1.0.0"},"service":"thermal.service.compressor","activation":"qualified_current_binding","order":120}],
      "operations":[{"id":"thermal.operation.set_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.operation.set_temperature","version":"1.0.0"},"capability":"thermal.capability.set_temperature","arguments":["thermal.setpoint.temperature_setpoint"],"argument_constraints":["thermal.setpoint.temperature_setpoint"],"effect_rule":"thermal.effect.set_temperature","preconditions":["current_generation","qualified_active_capability","exact_route","authority_admitted"],"stages":["admission","dispatch","acknowledgement","readback","terminal_outcome"],"terminal_outcomes":["rejected","failed_no_contact","acknowledged_unverified","applied","no_effect","conflict","indeterminate"],"readback":"current_generation_exact_effect","retry":"forbidden_after_indeterminate","order":10},{"id":"thermal.operation.set_mode","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.operation.set_mode","version":"1.0.0"},"capability":"thermal.capability.set_mode","arguments":["thermal.mode.system"],"argument_constraints":["thermal.mode.system"],"effect_rule":"thermal.effect.set_mode","preconditions":["current_generation","qualified_active_capability","exact_route","authority_admitted"],"stages":["admission","dispatch","acknowledgement","readback","terminal_outcome"],"terminal_outcomes":["rejected","failed_no_contact","acknowledged_unverified","applied","no_effect","conflict","indeterminate"],"readback":"current_generation_exact_effect","retry":"forbidden_after_indeterminate","order":20}],
      "effect_rules":[{"id":"thermal.effect.set_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.effect.set_temperature","version":"1.0.0"},"operation":"thermal.operation.set_temperature","fact":"thermal.setpoint.temperature_setpoint","operator":"equal","order":10},{"id":"thermal.effect.set_mode","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.effect.set_mode","version":"1.0.0"},"operation":"thermal.operation.set_mode","fact":"thermal.mode.system","operator":"equal","order":20}],
      "portal_contributions":[
        {"id":"thermal.portal.read.system","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.system","version":"1.0.0"},"kind":"read","service":"thermal.service.system","fields":["thermal.action.state","thermal.mode.system","thermal.status.operation","thermal.status.fault","thermal.measurement.outdoor_air_temperature","thermal.measurement.system_flow_temperature","thermal.status.maintenance"],"order":10},
        {"id":"thermal.portal.read.zone","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.zone","version":"1.0.0"},"kind":"read","service":"thermal.service.zone","fields":["thermal.measurement.air_temperature","thermal.measurement.humidity","thermal.setpoint.temperature_setpoint","thermal.mode.zone"],"order":20},
        {"id":"thermal.portal.read.circuit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.circuit","version":"1.0.0"},"kind":"read","service":"thermal.service.circuit","fields":["thermal.measurement.flow_rate","thermal.measurement.circuit_flow_temperature"],"order":30},
        {"id":"thermal.portal.read.dhw","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.dhw","version":"1.0.0"},"kind":"read","service":"thermal.service.dhw","fields":["thermal.measurement.dhw_temperature","thermal.setpoint.dhw_temperature_setpoint"],"order":40},
        {"id":"thermal.portal.read.ventilation","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.ventilation","version":"1.0.0"},"kind":"read","service":"thermal.service.ventilation","fields":["thermal.demand.level"],"order":50},
        {"id":"thermal.portal.operation.set_temperature","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.operation.set_temperature","version":"1.0.0"},"kind":"operation","operation":"thermal.operation.set_temperature","order":60},
        {"id":"thermal.portal.operation.set_mode","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.operation.set_mode","version":"1.0.0"},"kind":"operation","operation":"thermal.operation.set_mode","order":70},
        {"id":"thermal.portal.read.heat_generator","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.heat_generator","version":"1.0.0"},"kind":"read","service":"thermal.service.heat_generator","fields":["thermal.measurement.heat_generator_flow_temperature","thermal.measurement.heat_generator_return_temperature","thermal.measurement.thermal_power","thermal.energy.heat_output","thermal.measurement.water_pressure","thermal.status.heat_generator_fault","thermal.status.heat_generator_operation"],"order":80},
        {"id":"thermal.portal.read.thermal_store","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.thermal_store","version":"1.0.0"},"kind":"read","service":"thermal.service.thermal_store","fields":["thermal.measurement.store_temperature"],"order":90},
        {"id":"thermal.portal.read.terminal_unit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.terminal_unit","version":"1.0.0"},"kind":"read","service":"thermal.service.terminal_unit","fields":["thermal.mode.terminal_unit","thermal.status.terminal_unit_fault","thermal.status.terminal_unit_operation"],"order":100},
        {"id":"thermal.portal.read.refrigerant_circuit","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.refrigerant_circuit","version":"1.0.0"},"kind":"read","service":"thermal.service.refrigerant_circuit","fields":["thermal.measurement.refrigerant_high_side_pressure","thermal.measurement.refrigerant_low_side_pressure","thermal.measurement.refrigerant_condensing_temperature","thermal.measurement.refrigerant_evaporating_temperature"],"order":110},
        {"id":"thermal.portal.read.compressor","ref":{"pack":{"id":"helianthus.pack.thermal","version":"1.0.0"},"id":"thermal.portal.read.compressor","version":"1.0.0"},"kind":"read","service":"thermal.service.compressor","fields":["thermal.measurement.compressor_speed"],"order":120}]},
    "candidate_mappings":[{"id":"thermal.mapping.ebus.companion","native_owner":"helianthus-docs-ebus","state":"candidate","qualification":"native_owner_required","loss":"unknown"},{"id":"thermal.mapping.gree.vrf","native_owner":"helianthus-docs-canbus","state":"candidate","qualification":"native_owner_required","loss":"unknown"},{"id":"thermal.mapping.eebus","native_owner":"helianthus-docs-eebus","state":"unknown_pending_normative_source","qualification":"unresolved","loss":"unknown"}]},
  "vectors":[
    {"id":"TH-POS-001","polarity":"positive","input":{}},{"id":"TH-NEG-001","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.fields.0.kind","value":"decimal"}]},"expect":{"error":"non-kernel ValueKind"}},{"id":"TH-NEG-002","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.fields.0.unit","value":"degC"}]},"expect":{"error":"invalid unit ID"}},{"id":"TH-NEG-003","polarity":"negative","input":{"mutations":[{"op":"delete","path":"definitions.fields.0.ref"}]},"expect":{"error":"DefinitionRef owner/version"}},{"id":"TH-NEG-004","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.fields.13.symbols.1","value":"thermal.status.operation.unknown"}]},"expect":{"error":"invalid semantic symbols"}},{"id":"TH-NEG-005","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.fields.12.symbols.0","value":"thermal.action.state.accepted"}]},"expect":{"error":"field has invalid semantic symbols"}},{"id":"TH-NEG-006","polarity":"negative","input":{"mutations":[{"op":"delete","path":"definitions.services.0.fact_key_dimension"}]},"expect":{"error":"dimension contract"}},{"id":"TH-NEG-007","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.portal_contributions.0.service","value":"thermal.service.missing"}]},"expect":{"error":"Portal dangling reference"}},{"id":"TH-NEG-008","polarity":"negative","input":{"mutations":[{"op":"append_copy","path":"definitions.fields","index":0}]},"expect":{"error":"duplicate definition ID"}},{"id":"TH-NEG-009","polarity":"negative","input":{"mutations":[{"op":"remove_id","path":"domain_catalog","id":"infrastructure"}]},"expect":{"error":"five-domain catalog"}},{"id":"TH-NEG-010","polarity":"negative","input":{"mutations":[{"op":"set","path":"candidate_mappings.2.state","value":"qualified"}]},"expect":{"error":"eeBUS mapping"}},{"id":"TH-NEG-011","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.operations.0.terminal_outcomes.1","value":"dispatched_unacknowledged"}]},"expect":{"error":"operation stages/outcomes are incomplete"}},{"id":"TH-NEG-012","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.fields.0.dimension","value":"thermal.dimension.quantity"}]},"expect":{"error":"field has no resource dimension"}},{"id":"TH-NEG-013","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.fields.1.unit","value":"unit.kilowatt"}]},"expect":{"error":"outside the canonical SI unit set"}},{"id":"TH-NEG-014","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.fields.18.dimension","value":"thermal.dimension.system"}]},"expect":{"error":"field subject differs"}},{"id":"TH-NEG-015","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.relations.1.to.0","value":"thermal.dimension.missing"}]},"expect":{"error":"relation dangling dimension"}},{"id":"TH-NEG-016","polarity":"negative","input":{"mutations":[{"op":"set","path":"definitions.relations.0.cardinality","value":"many_to_many"}]},"expect":{"error":"relation endpoints or cardinality differ"}}]
}
`

type machineVector struct {
	ID     string `json:"id"`
	Expect *struct {
		Error string `json:"error"`
	} `json:"expect"`
	Input struct {
		Mutations []mutation `json:"mutations"`
	} `json:"input"`
}
type mutation struct {
	Op, Path string
	Value    any    `json:"value"`
	Index    int    `json:"index"`
	ID       string `json:"id"`
}

func TestThermalMachineFixtureVectors(t *testing.T) {
	bytes, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes)
	if sum != sha256.Sum256([]byte(approvedThermalFixture)) {
		t.Fatal("thermal fixture differs from the approved naming contract preimage")
	}
	var baseline map[string]any
	if err := json.Unmarshal(bytes, &baseline); err != nil {
		t.Fatal(err)
	}
	validateBaseline(t, baseline)
	var header struct {
		Contract     string          `json:"contract"`
		PackContract string          `json:"pack_contract"`
		Vectors      []machineVector `json:"vectors"`
	}
	if err := json.Unmarshal(bytes, &header); err != nil {
		t.Fatal(err)
	}
	if header.Contract != "helianthus.semantic.pack.thermal.acceptance/v1" || header.PackContract != "helianthus.pack.thermal/v1" || len(header.Vectors) != 17 {
		t.Fatalf("fixture header: %+v", header)
	}
	for _, vector := range header.Vectors {
		t.Run(vector.ID, func(t *testing.T) {
			candidate := cloneDocument(t, baseline)
			for _, change := range vector.Input.Mutations {
				applyMutation(t, candidate, change)
			}
			if vector.Expect == nil {
				validateBaseline(t, candidate)
				return
			}
			if got := vectorFailure(t, vector.ID, candidate); got != vector.Expect.Error {
				t.Fatalf("vector failure = %q, want %q", got, vector.Expect.Error)
			}
		})
	}
}

func TestStaticThermalPortalReferences(t *testing.T) {
	var document map[string]any
	bytes, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bytes, &document); err != nil {
		t.Fatal(err)
	}
	definitions := object(t, object(t, document["catalog"])["definitions"])
	fieldIDs, serviceIDs, operations := identifiers(t, definitions["fields"]), identifiers(t, definitions["services"]), identifiers(t, definitions["operations"])
	read := map[string]int{}
	for _, entry := range array(t, definitions["portal_contributions"]) {
		descriptor := object(t, entry)
		if descriptor["kind"] == "read" {
			service := stringValue(t, descriptor["service"])
			if !serviceIDs[service] {
				t.Fatalf("Portal service: %#v", descriptor)
			}
			for _, field := range array(t, descriptor["fields"]) {
				id := stringValue(t, field)
				spec, ok := findField(semreg.DefinitionID(id))
				if !fieldIDs[id] || !ok || string(spec.dimension) != servicesTable()[service] {
					t.Fatalf("Portal field %s outside the subject of %s", id, service)
				}
				read[id]++
			}
		} else if descriptor["kind"] == "operation" && !operations[stringValue(t, descriptor["operation"])] {
			t.Fatalf("Portal operation: %#v", descriptor)
		}
	}
	for _, spec := range fields {
		if read[string(spec.id)] != 1 {
			t.Fatalf("field %s is read by %d descriptors", spec.id, read[string(spec.id)])
		}
	}
}

func validateBaseline(t *testing.T, document map[string]any) {
	t.Helper()
	if document["contract"] != "helianthus.semantic.pack.thermal.acceptance/v1" || document["pack_contract"] != "helianthus.pack.thermal/v1" {
		t.Fatal("contract")
	}
	catalog := object(t, document["catalog"])
	packValue := object(t, catalog["pack"])
	if packValue["id"] != string(packID) || packValue["version"] != string(packVersion) {
		t.Fatal("pack")
	}
	if len(array(t, catalog["domain_catalog"])) != 5 {
		t.Fatal("five-domain catalog")
	}
	definitions := object(t, catalog["definitions"])
	for _, count := range []struct {
		value any
		want  int
	}{{definitions["fields"], 32}, {definitions["fact_dimensions"], 10}, {definitions["services"], 10}, {definitions["relations"], 5}, {definitions["capabilities"], 12}, {definitions["operations"], 2}, {definitions["effect_rules"], 2}, {definitions["portal_contributions"], 12}} {
		if len(array(t, count.value)) != count.want {
			t.Fatalf("catalog count %d", count.want)
		}
	}
	if failure := baselineFailure(t, definitions); failure != "" {
		t.Fatal(failure)
	}
}

// wantRelations are the five pack relation definitions, spelled
// thermal.relation.<name>, with the dimensions of their from and to ends and
// their cardinality, in the order of the vectors. The element catalog holds
// their core type, endpoint kinds, medium, basis classes and planes.
var wantRelations = []struct {
	id          string
	from, to    []semreg.DefinitionID
	cardinality string
}{
	{"thermal.relation.system_member", []semreg.DefinitionID{systemDimension}, []semreg.DefinitionID{zoneDimension, circuitDimension, dhwDimension, ventilationDimension, generatorDimension, storeDimension, terminalDimension, refrigerantDimension}, "one_to_many"},
	{"thermal.relation.fluid_supply", []semreg.DefinitionID{generatorDimension, storeDimension, circuitDimension}, []semreg.DefinitionID{circuitDimension, storeDimension, zoneDimension, dhwDimension, terminalDimension}, "many_to_many"},
	{"thermal.relation.air_supply", []semreg.DefinitionID{terminalDimension, ventilationDimension}, []semreg.DefinitionID{zoneDimension}, "many_to_many"},
	{"thermal.relation.refrigerant_circuit_member", []semreg.DefinitionID{refrigerantDimension}, []semreg.DefinitionID{generatorDimension, terminalDimension}, "one_to_many"},
	{"thermal.relation.generator_compressor", []semreg.DefinitionID{generatorDimension}, []semreg.DefinitionID{compressorDimension}, "one_to_many"},
}

// canonicalUnits is the SI unit set of the thermal pack.
var canonicalUnits = map[string]bool{"unit.kelvin": true, "unit.ratio": true, "unit.cubic_metre_per_second": true, "unit.pascal": true, "unit.reciprocal_second": true, "unit.watt": true, "unit.joule": true}

// baselineFailure compares the fixture definitions with the validator tables:
// every field with its kind, SI unit, bounds, symbols and one resource
// dimension, every resource dimension and service, every relation definition
// with its endpoint dimensions and cardinality, and every capability. It
// returns the failure a vector expects, or "" when the definitions agree.
func baselineFailure(t *testing.T, definitions map[string]any) string {
	t.Helper()
	declared := map[string]bool{}
	for index, entry := range array(t, definitions["fact_dimensions"]) {
		dimension := object(t, entry)
		id := stringValue(t, dimension["id"])
		if _, ok := dimensions[semreg.DefinitionID(id)]; !ok || dimension["value_kind"] != "text" || dimension["order"] != float64(10*(index+1)) {
			t.Fatalf("fact dimension %d: %#v", index, dimension)
		}
		declared[id] = true
	}
	if len(declared) != len(dimensions) {
		t.Fatalf("fact dimensions: %v", declared)
	}
	seen := map[string]bool{}
	for index, entry := range array(t, definitions["fields"]) {
		field := object(t, entry)
		id := stringValue(t, field["id"])
		spec, ok := findField(semreg.DefinitionID(id))
		if !ok || seen[id] {
			t.Fatalf("field %d: %s", index, id)
		}
		seen[id] = true
		if !declared[stringValue(t, field["dimension"])] {
			return "field has no resource dimension"
		}
		if field["dimension"] != string(spec.dimension) {
			return "field subject differs"
		}
		if field["kind"] != string(spec.kind) {
			t.Fatalf("field %s kind: %#v", id, field["kind"])
		}
		if spec.kind == semreg.ValueQuantity {
			if !canonicalUnits[stringValue(t, field["unit"])] {
				return "outside the canonical SI unit set"
			}
			bounds := object(t, field["bounds"])
			if field["unit"] != string(spec.unit) || !boundEquals(t, bounds["minimum"], spec.minimum) || !boundEquals(t, bounds["maximum"], spec.maximum) {
				t.Fatalf("field %s quantity: %#v", id, field)
			}
		} else if !reflect.DeepEqual(stringList(t, field["symbols"]), spec.symbols) {
			t.Fatalf("field %s symbols: %#v", id, field["symbols"])
		}
	}
	if len(seen) != len(fields) {
		t.Fatalf("fields: %d of %d", len(seen), len(fields))
	}
	services := array(t, definitions["services"])
	if len(services) != len(servicesTable()) {
		t.Fatalf("services: %d", len(services))
	}
	for index, entry := range services {
		service := object(t, entry)
		if dimension, ok := servicesTable()[stringValue(t, service["id"])]; !ok || service["fact_key_dimension"] != dimension || service["order"] != float64(10*(index+1)) {
			t.Fatalf("service %d: %#v", index, service)
		}
	}
	for index, entry := range array(t, definitions["relations"]) {
		relation := object(t, entry)
		for _, end := range []string{"from", "to"} {
			for _, dimension := range stringList(t, relation[end]) {
				if !declared[dimension] {
					return "relation dangling dimension"
				}
			}
		}
		want := wantRelations[index]
		if relation["id"] != want.id || relation["order"] != float64(10*(index+1)) {
			t.Fatalf("relation %d: %#v", index, relation)
		}
		if !reflect.DeepEqual(stringList(t, relation["from"]), ids(want.from)) || !reflect.DeepEqual(stringList(t, relation["to"]), ids(want.to)) || relation["cardinality"] != want.cardinality {
			return "relation endpoints or cardinality differ"
		}
	}
	capabilitiesSeen := map[string]bool{}
	for index, entry := range array(t, definitions["capabilities"]) {
		capability := object(t, entry)
		id := stringValue(t, capability["id"])
		spec, ok := capabilities[semreg.DefinitionID(id)]
		if !ok || capabilitiesSeen[id] || capability["service"] != string(spec.service) || capability["order"] != float64(10*(index+1)) {
			t.Fatalf("capability %d: %#v", index, capability)
		}
		capabilitiesSeen[id] = true
	}
	return ""
}
func servicesTable() map[string]any {
	result := map[string]any{}
	for id, dimension := range services {
		result[string(id)] = string(dimension)
	}
	return result
}
func boundEquals(t *testing.T, value any, want *decimalBound) bool {
	t.Helper()
	decimal := object(t, value)
	return want != nil && decimal["coefficient"] == want.coefficient && decimal["exponent10"] == float64(want.exponent10)
}
func stringList(t *testing.T, value any) []string {
	t.Helper()
	result := []string{}
	for _, entry := range array(t, value) {
		result = append(result, stringValue(t, entry))
	}
	return result
}
func ids(values []semreg.DefinitionID) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func vectorFailure(t *testing.T, id string, document map[string]any) string {
	t.Helper()
	catalog := object(t, document["catalog"])
	definitions := object(t, catalog["definitions"])
	fields := array(t, definitions["fields"])
	switch id {
	case "TH-NEG-001":
		if object(t, fields[0])["kind"] != "quantity" {
			return "non-kernel ValueKind"
		}
	case "TH-NEG-002":
		if object(t, fields[0])["unit"] != "unit.kelvin" {
			return "invalid unit ID"
		}
	case "TH-NEG-003":
		if _, ok := object(t, fields[0])["ref"]; !ok {
			return "DefinitionRef owner/version"
		}
	case "TH-NEG-004":
		if array(t, object(t, fields[13])["symbols"])[1] != "thermal.status.operation.idle" {
			return "invalid semantic symbols"
		}
	case "TH-NEG-005":
		if array(t, object(t, fields[12])["symbols"])[0] != "thermal.action.state.cooling" {
			return "field has invalid semantic symbols"
		}
	case "TH-NEG-006":
		if _, ok := object(t, array(t, definitions["services"])[0])["fact_key_dimension"]; !ok {
			return "dimension contract"
		}
	case "TH-NEG-007":
		if object(t, array(t, definitions["portal_contributions"])[0])["service"] != "thermal.service.system" {
			return "Portal dangling reference"
		}
	case "TH-NEG-008":
		if len(fields) == 33 {
			return "duplicate definition ID"
		}
	case "TH-NEG-009":
		if len(array(t, catalog["domain_catalog"])) == 4 {
			return "five-domain catalog"
		}
	case "TH-NEG-010":
		if object(t, array(t, catalog["candidate_mappings"])[2])["state"] != "unknown_pending_normative_source" {
			return "eeBUS mapping"
		}
	case "TH-NEG-011":
		if array(t, object(t, array(t, definitions["operations"])[0])["terminal_outcomes"])[1] != "failed_no_contact" {
			return "operation stages/outcomes are incomplete"
		}
	case "TH-NEG-012", "TH-NEG-013", "TH-NEG-014", "TH-NEG-015", "TH-NEG-016":
		if failure := baselineFailure(t, definitions); failure != "" {
			return failure
		}
	}
	t.Fatalf("vector mutation did not violate %s", id)
	return ""
}

func applyMutation(t *testing.T, document map[string]any, change mutation) {
	t.Helper()
	path := "catalog." + change.Path
	switch change.Op {
	case "set":
		setPath(t, document, path, change.Value)
	case "delete":
		deletePath(t, document, path)
	case "append_copy":
		values := arrayAt(t, document, path)
		values = append(values, values[change.Index])
		setPath(t, document, path, values)
	case "remove_id":
		values := arrayAt(t, document, path)
		kept := make([]any, 0, len(values))
		for _, value := range values {
			if object(t, value)["id"] != change.ID {
				kept = append(kept, value)
			}
		}
		setPath(t, document, path, kept)
	default:
		t.Fatalf("mutation %s", change.Op)
	}
}
func cloneDocument(t *testing.T, document map[string]any) map[string]any {
	t.Helper()
	bytes, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(bytes, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}
func setPath(t *testing.T, document map[string]any, path string, value any) {
	t.Helper()
	parent, last := pathParent(t, document, path)
	switch values := parent.(type) {
	case map[string]any:
		values[last] = value
	case []any:
		values[parseIndex(t, last)] = value
	default:
		t.Fatal(path)
	}
}
func deletePath(t *testing.T, document map[string]any, path string) {
	t.Helper()
	parent, last := pathParent(t, document, path)
	values, ok := parent.(map[string]any)
	if !ok {
		t.Fatal(path)
	}
	delete(values, last)
}
func pathParent(t *testing.T, document map[string]any, path string) (any, string) {
	t.Helper()
	parts := split(path)
	var current any = document
	for _, part := range parts[:len(parts)-1] {
		switch values := current.(type) {
		case map[string]any:
			current = values[part]
		case []any:
			current = values[parseIndex(t, part)]
		default:
			t.Fatal(path)
		}
	}
	return current, parts[len(parts)-1]
}
func split(path string) []string {
	result := []string{}
	current := ""
	for _, r := range path {
		if r == '.' {
			result = append(result, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	return append(result, current)
}
func parseIndex(t *testing.T, value string) int {
	t.Helper()
	var result int
	if _, err := fmtSscan(value, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func fmtSscan(value string, result *int) (int, error) {
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, os.ErrInvalid
		}
		*result = *result*10 + int(r-'0')
	}
	return 1, nil
}
func arrayAt(t *testing.T, document map[string]any, path string) []any {
	t.Helper()
	parent, last := pathParent(t, document, path)
	return array(t, object(t, parent)[last])
}
func object(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("object: %#v", value)
	}
	return result
}
func array(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("array: %#v", value)
	}
	return result
}
func stringValue(t *testing.T, value any) string {
	t.Helper()
	result, ok := value.(string)
	if !ok {
		t.Fatalf("string: %#v", value)
	}
	return result
}
func identifiers(t *testing.T, value any) map[string]bool {
	t.Helper()
	result := map[string]bool{}
	for _, entry := range array(t, value) {
		result[stringValue(t, object(t, entry)["id"])] = true
	}
	return result
}
func fixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1", "thermal-hvac-pack-vectors.json")
}
