// Package evse implements the accepted EVSE capability pack.
package evse

import (
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

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
	{"evse.energy.lifetime", semreg.ValueQuantity, "unit.joule", "evse.dimension.meter", bound("0", 0), bound("36", 17), nil},
	{"evse.energy.session", semreg.ValueQuantity, "unit.joule", "evse.dimension.session", bound("0", 0), bound("36", 17), nil},
	{"evse.limit.actual_current", semreg.ValueQuantity, "unit.ampere", "evse.dimension.connector", bound("0", 0), bound("1", 3), nil},
	{"evse.limit.advertised_current", semreg.ValueQuantity, "unit.ampere", "evse.dimension.connector", bound("0", 0), bound("1", 3), nil},
	{"evse.limit.allocated_current", semreg.ValueQuantity, "unit.ampere", "evse.dimension.connector", bound("0", 0), bound("1", 3), nil},
	{"evse.limit.allocated_current_phase", semreg.ValueQuantity, "unit.ampere", "evse.dimension.phase", bound("0", 0), bound("1", 3), nil},
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

type operationSpec struct{ capability, argument, effect semreg.DefinitionID }

const limitReleaseOperation semreg.DefinitionID = "evse.operation.set_allocated_current"

var operations = map[semreg.DefinitionID]operationSpec{
	limitReleaseOperation: {"evse.capability.set_allocated_current", "evse.limit.allocated_current", "evse.effect.set_allocated_current"},
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
	r := semreg.DefinitionIndex{Pack: pack, Services: sortedRefs(services), Capabilities: sortedRefs(capabilities), Operations: sortedRefs(operations)}
	ids := make([]semreg.DefinitionID, 0, len(fields))
	for _, f := range fields {
		ids = append(ids, f.id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		r.Fields = append(r.Fields, definition(id))
	}
	effects := map[semreg.DefinitionID]struct{}{}
	for _, spec := range operations {
		effects[spec.effect] = struct{}{}
	}
	r.EffectRules = sortedRefs(effects)
	if len(unionEvents) > 0 {
		events := append([]semreg.DefinitionID(nil), unionEvents...)
		sort.Slice(events, func(i, j int) bool { return events[i] < events[j] })
		for _, id := range events {
			r.Events = append(r.Events, definition(id))
		}
	}
	return r
}

func releaseMetadata(id semreg.DefinitionID) *semreg.ReleaseMetadata {
	if id != limitReleaseOperation {
		return nil
	}
	cleared := false
	return &semreg.ReleaseMetadata{Argument: semreg.Value{Kind: semreg.ValueBoolean, Boolean: &cleared}}
}

// Metadata returns fresh metadata derived from this validator's private tables.
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
		item := semreg.OperationMetadata{Ref: definition(id), Capability: definition(spec.capability), Service: definition(capabilities[spec.capability].service), Argument: definition(spec.argument), Effect: definition(spec.effect), Release: releaseMetadata(id)}
		for _, parameter := range operationParameters[id] {
			item.Parameters = append(item.Parameters, definition(parameter))
		}
		metadata.Operations = append(metadata.Operations, item)
	}
	if len(unionEvents) > 0 {
		for _, id := range unionEvents {
			metadata.Events = append(metadata.Events, definition(id))
		}
	}
	for id := range units {
		metadata.Units = append(metadata.Units, definition(id))
	}
	sort.Slice(metadata.Units, func(i, j int) bool { return metadata.Units[i].ID < metadata.Units[j].ID })
	sort.Slice(metadata.Fields, func(i, j int) bool { return metadata.Fields[i].Ref.ID < metadata.Fields[j].Ref.ID })
	sort.Slice(metadata.Services, func(i, j int) bool { return metadata.Services[i].Ref.ID < metadata.Services[j].Ref.ID })
	sort.Slice(metadata.Capabilities, func(i, j int) bool { return metadata.Capabilities[i].Ref.ID < metadata.Capabilities[j].Ref.ID })
	sort.Slice(metadata.Operations, func(i, j int) bool { return metadata.Operations[i].Ref.ID < metadata.Operations[j].Ref.ID })
	sort.Slice(metadata.Events, func(i, j int) bool { return metadata.Events[i].ID < metadata.Events[j].ID })
	return metadata
}
