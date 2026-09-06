// Package evse implements the accepted EVSE capability pack.
package evse

import semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"

const (
	packID      semreg.DefinitionID    = "helianthus.pack.evse"
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
	{"evse.ac.active_power", semreg.ValueQuantity, "unit.watt", "evse.dimension.phase", bound("-1", 7), bound("1", 7), nil},
	{"evse.ac.current", semreg.ValueQuantity, "unit.ampere", "evse.dimension.phase", bound("0", 0), bound("1", 3), nil},
	{"evse.ac.frequency", semreg.ValueQuantity, "unit.hertz", "evse.dimension.evse", bound("0", 0), bound("1", 3), nil},
	{"evse.ac.voltage", semreg.ValueQuantity, "unit.volt", "evse.dimension.phase", bound("0", 0), bound("1", 3), nil},
	{"evse.energy.lifetime", semreg.ValueQuantity, "unit.kilowatt_hour", "evse.dimension.meter", bound("0", 0), bound("1", 12), nil},
	{"evse.energy.session", semreg.ValueQuantity, "unit.kilowatt_hour", "evse.dimension.session", bound("0", 0), bound("1", 12), nil},
	{"evse.limit.actual_current", semreg.ValueQuantity, "unit.ampere", "evse.dimension.connector", bound("0", 0), bound("1", 3), nil},
	{"evse.limit.advertised_current", semreg.ValueQuantity, "unit.ampere", "evse.dimension.connector", bound("0", 0), bound("1", 3), nil},
	{"evse.limit.allocated_current", semreg.ValueQuantity, "unit.ampere", "evse.dimension.connector", bound("0", 0), bound("1", 3), nil},
	{"evse.limit.configured_current", semreg.ValueQuantity, "unit.ampere", "evse.dimension.evse", bound("0", 0), bound("1", 3), nil},
	{"evse.status.charging", semreg.ValueSymbol, "", "evse.dimension.connector", nil, nil, []string{"evse.status.charging.charging", "evse.status.charging.complete", "evse.status.charging.idle"}},
	{"evse.status.connection", semreg.ValueSymbol, "", "evse.dimension.connector", nil, nil, []string{"evse.status.connection.connected", "evse.status.connection.disconnected"}},
	{"evse.status.fault", semreg.ValueSymbol, "", "evse.dimension.evse", nil, nil, []string{"evse.status.fault.clear", "evse.status.fault.present"}},
	{"evse.status.interlock", semreg.ValueSymbol, "", "evse.dimension.connector", nil, nil, []string{"evse.status.interlock.active", "evse.status.interlock.clear"}},
	{"evse.status.readiness", semreg.ValueSymbol, "", "evse.dimension.evse", nil, nil, []string{"evse.status.readiness.not_ready", "evse.status.readiness.ready"}},
}
var dimensions = map[semreg.DefinitionID]semreg.ValueKind{"evse.dimension.asset": semreg.ValueText, "evse.dimension.evse": semreg.ValueText, "evse.dimension.connector": semreg.ValueText, "evse.dimension.phase": semreg.ValueText, "evse.dimension.session": semreg.ValueText, "evse.dimension.meter": semreg.ValueText}
var services = map[semreg.DefinitionID]semreg.DefinitionID{"evse.service.evse": "evse.dimension.evse", "evse.service.connector": "evse.dimension.connector", "evse.service.phase": "evse.dimension.phase", "evse.service.session": "evse.dimension.session", "evse.service.meter": "evse.dimension.meter"}

type capabilitySpec struct {
	service     semreg.DefinitionID
	constraints []semreg.DefinitionID
}

var capabilities = map[semreg.DefinitionID]capabilitySpec{"evse.capability.read.evse": {"evse.service.evse", nil}, "evse.capability.read.connector": {"evse.service.connector", nil}, "evse.capability.read.phase": {"evse.service.phase", nil}, "evse.capability.read.session": {"evse.service.session", nil}, "evse.capability.read.meter": {"evse.service.meter", nil}, "evse.capability.set_allocated_current": {"evse.service.connector", []semreg.DefinitionID{"evse.limit.allocated_current"}}}

func definition(id semreg.DefinitionID) semreg.DefinitionRef {
	return semreg.DefinitionRef{Pack: pack, ID: id, Version: packVersion}
}
func index() semreg.DefinitionIndex {
	r := semreg.DefinitionIndex{Pack: pack, Fields: make([]semreg.DefinitionRef, 0, len(fields)), Services: make([]semreg.DefinitionRef, 0, len(services)), Capabilities: make([]semreg.DefinitionRef, 0, len(capabilities)), Operations: []semreg.DefinitionRef{definition("evse.operation.set_allocated_current")}, EffectRules: []semreg.DefinitionRef{definition("evse.effect.set_allocated_current")}}
	for _, f := range fields {
		r.Fields = append(r.Fields, definition(f.id))
	}
	for _, id := range []semreg.DefinitionID{"evse.service.connector", "evse.service.evse", "evse.service.meter", "evse.service.phase", "evse.service.session"} {
		r.Services = append(r.Services, definition(id))
	}
	for _, id := range []semreg.DefinitionID{"evse.capability.read.connector", "evse.capability.read.evse", "evse.capability.read.meter", "evse.capability.read.phase", "evse.capability.read.session", "evse.capability.set_allocated_current"} {
		r.Capabilities = append(r.Capabilities, definition(id))
	}
	return r
}
