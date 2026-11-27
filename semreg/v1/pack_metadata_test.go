package semreg_test

import (
	"reflect"
	"sync"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/energymanagement"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/equipment"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

func builtinSources() []semreg.PackMetadataSource {
	return []semreg.PackMetadataSource{
		{Validator: thermal.New(), Metadata: thermal.Metadata()},
		{Validator: pv.New(), Metadata: pv.Metadata()},
		{Validator: storage.New(), Metadata: storage.Metadata()},
		{Validator: energymanagement.New(), Metadata: energymanagement.Metadata()},
		{Validator: equipment.New(), Metadata: equipment.Metadata()},
		{Validator: evse.New(), Metadata: evse.Metadata()},
		{Validator: infrastructure.New(), Metadata: infrastructure.Metadata()},
	}
}

type driftingMetadataProvider struct {
	semreg.PackValidator
	metadata semreg.PackMetadata
}

func (p driftingMetadataProvider) Metadata() semreg.PackMetadata { return p.metadata }

func demandLevel(metadata *semreg.PackMetadata) *semreg.FieldMetadata {
	for i := range metadata.Fields {
		if metadata.Fields[i].Ref.ID == "thermal.demand.level" {
			return &metadata.Fields[i]
		}
	}
	return nil
}

func TestPackMetadataBuiltinQueries(t *testing.T) {
	registry, err := packs.NewMetadataRegistry()
	if err != nil {
		t.Fatal(err)
	}
	wantPacks := []semreg.PackRef{
		{ID: "helianthus.pack.energy_management", Version: "1.0.0"},
		{ID: "helianthus.pack.equipment", Version: "1.0.0"},
		{ID: "helianthus.pack.evse", Version: "1.0.0"},
		{ID: "helianthus.pack.infrastructure", Version: "1.0.0"},
		{ID: "helianthus.pack.pv", Version: "1.0.0"},
		{ID: "helianthus.pack.storage", Version: "1.1.0"},
		{ID: "helianthus.pack.thermal", Version: "1.0.0"},
	}
	if got := registry.Packs(); !reflect.DeepEqual(got, wantPacks) {
		t.Fatalf("packs = %#v, want %#v", got, wantPacks)
	}
	for _, source := range builtinSources() {
		metadata := source.Metadata
		index := source.Validator.Definitions()
		want := map[semreg.DefinitionID]struct{ units, fields, services, capabilities, operations, events int }{
			"helianthus.pack.energy_management": {8, 261, 2, 21, 19, 2},
			"helianthus.pack.equipment":         {7, 59, 2, 3, 1, 5},
			"helianthus.pack.evse":              {10, 308, 6, 18, 12, 2},
			"helianthus.pack.infrastructure":    {10, 76, 8, 6, 0, 2},
			"helianthus.pack.pv":                {13, 103, 6, 8, 2, 0},
			"helianthus.pack.storage":           {9, 106, 7, 20, 17, 2},
			"helianthus.pack.thermal":           {12, 269, 21, 87, 74, 15},
		}[metadata.Pack.ID]
		if got := struct{ units, fields, services, capabilities, operations, events int }{len(metadata.Units), len(metadata.Fields), len(metadata.Services), len(metadata.Capabilities), len(metadata.Operations), len(metadata.Events)}; got != want {
			t.Fatalf("metadata shape for %s = %+v, want %+v", metadata.Pack.ID, got, want)
		}
		for _, unit := range metadata.Units {
			if !registry.HasDefinition(unit) {
				t.Fatalf("missing unit %v", unit)
			}
		}
		for _, refs := range [][]semreg.DefinitionRef{index.Fields, index.Services, index.Capabilities, index.Operations, index.EffectRules, index.Events} {
			for _, ref := range refs {
				if !registry.HasDefinition(ref) {
					t.Fatalf("missing indexed definition %v", ref)
				}
			}
		}
		for _, field := range metadata.Fields {
			got, ok := registry.CanonicalUnit(field.Ref)
			if field.CanonicalUnit == nil {
				if ok {
					t.Fatalf("non-quantity %v returned unit %v", field.Ref, got)
				}
			} else if !ok || got != *field.CanonicalUnit {
				t.Fatalf("unit for %v = %v, %t; want %v", field.Ref, got, ok, *field.CanonicalUnit)
			}
		}
		for _, capability := range metadata.Capabilities {
			if !registry.ServiceOwnsCapability(capability.Service, capability.Ref) {
				t.Fatalf("owner relation missing for %v", capability.Ref)
			}
		}
		for _, field := range metadata.Fields {
			for _, capability := range metadata.Capabilities {
				for _, service := range metadata.Services {
					want := field.Dimension == service.FactKeyDimension && capability.Service == service.Ref
					if got := registry.FieldMatches(field.Ref, service.Ref, capability.Ref); got != want {
						t.Fatalf("field match %v / %v / %v = %t, want %t", field.Ref, service.Ref, capability.Ref, got, want)
					}
				}
			}
		}
		for _, operation := range metadata.Operations {
			if !registry.OperationMatches(operation.Ref, operation.Capability, operation.Service, operation.Argument, operation.Effect) {
				t.Fatalf("operation shape missing for %v", operation.Ref)
			}
			wrong := semreg.DefinitionRef{Pack: operation.Ref.Pack, ID: "metadata.unknown", Version: operation.Ref.Version}
			if registry.OperationMatches(wrong, operation.Capability, operation.Service, operation.Argument, operation.Effect) ||
				registry.OperationMatches(operation.Ref, wrong, operation.Service, operation.Argument, operation.Effect) ||
				registry.OperationMatches(operation.Ref, operation.Capability, wrong, operation.Argument, operation.Effect) ||
				registry.OperationMatches(operation.Ref, operation.Capability, operation.Service, wrong, operation.Effect) ||
				registry.OperationMatches(operation.Ref, operation.Capability, operation.Service, operation.Argument, wrong) {
				t.Fatalf("wrong operation tuple member accepted for %v", operation.Ref)
			}
		}
	}

	storagePack := semreg.PackRef{ID: "helianthus.pack.storage", Version: "1.1.0"}
	soc := semreg.DefinitionRef{Pack: storagePack, ID: "storage.state.soc", Version: "1.1.0"}
	ratio := semreg.DefinitionRef{Pack: storagePack, ID: "unit.ratio", Version: "1.1.0"}
	if got, ok := registry.CanonicalUnit(soc); !ok || got != ratio {
		t.Fatalf("storage SOC unit = %v, %t", got, ok)
	}
	for _, stale := range []semreg.DefinitionRef{{Pack: storagePack, ID: "storage.state_of_charge", Version: "1.1.0"}, {Pack: storagePack, ID: "storage.unit.ratio", Version: "1.1.0"}, {Pack: storagePack, ID: "storage.unit.percent", Version: "1.1.0"}, {Pack: storagePack, ID: "unit.percent", Version: "1.1.0"}} {
		if registry.HasDefinition(stale) {
			t.Fatalf("stale storage alias exported: %v", stale)
		}
	}
}

func TestPackMetadataFailsClosedAndCopies(t *testing.T) {
	sources := builtinSources()
	registry, err := semreg.NewPackMetadataRegistry(sources...)
	if err != nil {
		t.Fatal(err)
	}
	thermalPack := semreg.PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}
	evsePack := semreg.PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}
	field := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.mode.zone", Version: "1.0.0"}
	service := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.service.zone", Version: "1.0.0"}
	capability := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.capability.read.zone", Version: "1.0.0"}
	if !registry.FieldMatches(field, service, capability) {
		t.Fatal("exact thermal field match rejected")
	}
	wrong := field
	wrong.Version = "1.0.1"
	if registry.HasDefinition(wrong) || registry.FieldMatches(wrong, service, capability) {
		t.Fatal("wrong version accepted")
	}
	cross := capability
	cross.Pack = evsePack
	cross.Version = evsePack.Version
	if registry.ServiceOwnsCapability(service, cross) || registry.FieldMatches(field, service, cross) {
		t.Fatal("cross-pack relation accepted")
	}
	wrongOperation := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.operation.set_temperature", Version: "1.0.0"}
	wrongEffect := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.effect.set_mode", Version: "1.0.0"}
	operationCapability := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.capability.set_temperature", Version: "1.0.0"}
	operationArgument := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.setpoint.temperature_setpoint", Version: "1.0.0"}
	if registry.OperationMatches(wrongOperation, operationCapability, service, operationArgument, wrongEffect) {
		t.Fatal("wrong operation effect accepted")
	}

	*demandLevel(&sources[0].Metadata).CanonicalUnit = semreg.DefinitionRef{}
	thermalDemand := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.demand.level", Version: "1.0.0"}
	if got, ok := registry.CanonicalUnit(thermalDemand); !ok || got.ID != "unit.ratio" {
		t.Fatalf("input mutation changed accepted registry: %v, %t", got, ok)
	}
	returned := registry.Packs()
	returned[0] = thermalPack
	if got := registry.Packs()[0]; got != (semreg.PackRef{ID: "helianthus.pack.energy_management", Version: "1.0.0"}) {
		t.Fatalf("returned pack mutation leaked: %v", got)
	}
}

func TestPackMetadataConstructorRejectsHostileRelations(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func([]semreg.PackMetadataSource)
	}{
		{"duplicate-pack", func(s []semreg.PackMetadataSource) { s[1].Metadata.Pack = s[0].Metadata.Pack }},
		{"cross-pack-dimension", func(s []semreg.PackMetadataSource) {
			s[0].Metadata.Fields[0].Dimension = s[1].Metadata.Fields[0].Dimension
		}},
		{"unknown-unit", func(s []semreg.PackMetadataSource) {
			u := s[0].Metadata.Units[0]
			u.ID = "unit.unknown"
			demandLevel(&s[0].Metadata).CanonicalUnit = &u
		}},
		{"forged-unit-added-to-list", func(s []semreg.PackMetadataSource) {
			forged := semreg.DefinitionRef{Pack: s[0].Metadata.Pack, ID: "unit.unaccepted", Version: s[0].Metadata.Pack.Version}
			s[0].Metadata.Units = append(s[0].Metadata.Units, forged)
			demandLevel(&s[0].Metadata).CanonicalUnit = &forged
		}},
		{"forged-matching-dimension", func(s []semreg.PackMetadataSource) {
			forged := semreg.DefinitionRef{Pack: s[0].Metadata.Pack, ID: "thermal.dimension.unaccepted", Version: s[0].Metadata.Pack.Version}
			s[0].Metadata.Fields[0].Dimension = forged
			for i := range s[0].Metadata.Services {
				if s[0].Metadata.Services[i].Ref.ID == "thermal.service.system" {
					s[0].Metadata.Services[i].FactKeyDimension = forged
				}
			}
		}},
		{"unit-on-non-quantity-field", func(s []semreg.PackMetadataSource) {
			unit := s[0].Metadata.Units[0]
			s[0].Metadata.Fields[0].CanonicalUnit = &unit
		}},
		{"missing-service", func(s []semreg.PackMetadataSource) {
			s[0].Metadata.Capabilities[0].Service = s[0].Metadata.Services[0].Ref
			s[0].Metadata.Services = s[0].Metadata.Services[1:]
		}},
		{"duplicate-operation", func(s []semreg.PackMetadataSource) {
			s[0].Metadata.Operations = append(s[0].Metadata.Operations, s[0].Metadata.Operations[0])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := builtinSources()
			test.mutate(sources)
			if _, err := semreg.NewPackMetadataRegistry(sources...); err == nil {
				t.Fatal("hostile metadata accepted")
			}
		})
	}

	t.Run("authoritative-provider-drift", func(t *testing.T) {
		sources := builtinSources()
		drifted := thermal.Metadata()
		forged := semreg.DefinitionRef{Pack: drifted.Pack, ID: "unit.drifted", Version: drifted.Pack.Version}
		drifted.Units = append(drifted.Units, forged)
		demandLevel(&drifted).CanonicalUnit = &forged
		sources[0].Validator = driftingMetadataProvider{PackValidator: thermal.New(), metadata: drifted}
		if _, err := semreg.NewPackMetadataRegistry(sources...); semreg.ErrorIdentifier(err) != semreg.DefinitionOwnerConflict {
			t.Fatalf("provider drift error = %v, want definition ownership conflict", err)
		}
	})

	first, err := semreg.NewPackMetadataRegistry(builtinSources()...)
	if err != nil {
		t.Fatal(err)
	}
	sources := builtinSources()
	for i, j := 0, len(sources)-1; i < j; i, j = i+1, j-1 {
		sources[i], sources[j] = sources[j], sources[i]
	}
	second, err := semreg.NewPackMetadataRegistry(sources...)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Packs(), second.Packs()) {
		t.Fatal("equivalent reordered inputs changed canonical enumeration")
	}
}

func TestPackMetadataProviderReturnsFreshAuthoritativeSnapshot(t *testing.T) {
	provider, ok := thermal.New().(semreg.PackMetadataProvider)
	if !ok {
		t.Fatal("thermal validator does not expose metadata provider")
	}
	first := provider.Metadata()
	*demandLevel(&first).CanonicalUnit = semreg.DefinitionRef{}
	second := provider.Metadata()
	unit := demandLevel(&second).CanonicalUnit
	if unit == nil || unit.ID != "unit.ratio" {
		t.Fatalf("provider snapshot mutation leaked: %+v", demandLevel(&second))
	}
}

type callbackMetadataProvider struct {
	semreg.PackValidator
	metadata func() semreg.PackMetadata
}

func (p callbackMetadataProvider) Metadata() semreg.PackMetadata { return p.metadata() }

func TestPackMetadataRelease(t *testing.T) {
	source := func(t *testing.T) semreg.PackMetadataSource {
		t.Helper()
		provider := thermal.New()
		metadata := thermal.Metadata()
		literal := false
		metadata.Operations[0].Release = &semreg.ReleaseMetadata{Argument: semreg.Value{Kind: semreg.ValueBoolean, Boolean: &literal}}
		return semreg.PackMetadataSource{Validator: driftingMetadataProvider{provider, metadata}, Metadata: metadata}
	}
	t.Run("valid literal keeps the primary operation lookup", func(t *testing.T) {
		s := source(t)
		registry, err := semreg.NewPackMetadataRegistry(s)
		if err != nil {
			t.Fatal(err)
		}
		op := s.Metadata.Operations[0]
		if !registry.OperationMatches(op.Ref, op.Capability, op.Service, op.Argument, op.Effect) {
			t.Fatal("release changed the operation's primary lookup")
		}
	})
	t.Run("matching provider cannot supply a malformed literal", func(t *testing.T) {
		s := source(t)
		s.Metadata.Operations[0].Release.Argument.Boolean = nil
		if _, err := semreg.NewPackMetadataRegistry(s); semreg.ErrorIdentifier(err) != semreg.InvalidValue {
			t.Fatalf("malformed literal: %v", err)
		}
	})
	t.Run("release belongs to the operation owner", func(t *testing.T) {
		s := source(t)
		s.Validator = thermal.New()
		if _, err := semreg.NewPackMetadataRegistry(s); semreg.ErrorIdentifier(err) != semreg.DefinitionOwnerConflict {
			t.Fatalf("undeclared release: %v", err)
		}
	})
	t.Run("copies the release and its literal payload", func(t *testing.T) {
		s := source(t)
		original := s.Metadata.Operations[0].Release
		owner := thermal.Metadata()
		literal := false
		owner.Operations[0].Release = &semreg.ReleaseMetadata{Argument: semreg.Value{Kind: semreg.ValueBoolean, Boolean: &literal}}
		s.Validator = callbackMetadataProvider{s.Validator, func() semreg.PackMetadata {
			*original.Argument.Boolean = true
			return owner
		}}
		if _, err := semreg.NewPackMetadataRegistry(s); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPackMetadataConcurrentQueries(t *testing.T) {
	registry, err := packs.NewMetadataRegistry()
	if err != nil {
		t.Fatal(err)
	}
	pack := semreg.PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}
	field := semreg.DefinitionRef{Pack: pack, ID: "evse.limit.allocated_current", Version: "1.0.0"}
	service := semreg.DefinitionRef{Pack: pack, ID: "evse.service.connector", Version: "1.0.0"}
	capability := semreg.DefinitionRef{Pack: pack, ID: "evse.capability.set_allocated_current", Version: "1.0.0"}
	operation := semreg.DefinitionRef{Pack: pack, ID: "evse.operation.set_allocated_current", Version: "1.0.0"}
	effect := semreg.DefinitionRef{Pack: pack, ID: "evse.effect.set_allocated_current", Version: "1.0.0"}
	var group sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for n := 0; n < 128; n++ {
				if !registry.HasDefinition(field) || !registry.ServiceOwnsCapability(service, capability) || !registry.FieldMatches(field, service, capability) || !registry.OperationMatches(operation, capability, service, field, effect) {
					t.Error("accepted query failed")
				}
				if got, ok := registry.CanonicalUnit(field); !ok || got.ID != "unit.ampere" {
					t.Error("canonical unit changed")
				}
				copy := registry.Packs()
				copy[0].ID = "mutated"
			}
		}()
	}
	group.Wait()
}
