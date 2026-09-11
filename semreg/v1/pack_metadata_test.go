package semreg_test

import (
	"reflect"
	"sync"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs"
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
		{Validator: evse.New(), Metadata: evse.Metadata()},
		{Validator: infrastructure.New(), Metadata: infrastructure.Metadata()},
	}
}

func TestPackMetadataBuiltinQueries(t *testing.T) {
	registry, err := packs.NewMetadataRegistry()
	if err != nil {
		t.Fatal(err)
	}
	wantPacks := []semreg.PackRef{
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
		want := map[semreg.DefinitionID]struct{ units, fields, services, capabilities, operations int }{
			"helianthus.pack.thermal":        {4, 13, 5, 7, 2},
			"helianthus.pack.pv":             {7, 21, 6, 8, 2},
			"helianthus.pack.storage":        {7, 20, 7, 5, 2},
			"helianthus.pack.evse":           {5, 15, 5, 6, 1},
			"helianthus.pack.infrastructure": {8, 19, 6, 6, 0},
		}[metadata.Pack.ID]
		if got := struct{ units, fields, services, capabilities, operations int }{len(metadata.Units), len(metadata.Fields), len(metadata.Services), len(metadata.Capabilities), len(metadata.Operations)}; got != want {
			t.Fatalf("metadata shape for %s = %+v, want %+v", metadata.Pack.ID, got, want)
		}
		for _, unit := range metadata.Units {
			if !registry.HasDefinition(unit) {
				t.Fatalf("missing unit %v", unit)
			}
		}
		for _, refs := range [][]semreg.DefinitionRef{index.Fields, index.Services, index.Capabilities, index.Operations, index.EffectRules} {
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
	percent := semreg.DefinitionRef{Pack: storagePack, ID: "unit.percent", Version: "1.1.0"}
	if got, ok := registry.CanonicalUnit(soc); !ok || got != percent {
		t.Fatalf("storage SOC unit = %v, %t", got, ok)
	}
	for _, stale := range []semreg.DefinitionRef{{Pack: storagePack, ID: "storage.state_of_charge", Version: "1.1.0"}, {Pack: storagePack, ID: "storage.unit.percent", Version: "1.1.0"}} {
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
	operationArgument := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.setpoint.temperature", Version: "1.0.0"}
	if registry.OperationMatches(wrongOperation, operationCapability, service, operationArgument, wrongEffect) {
		t.Fatal("wrong operation effect accepted")
	}

	*sources[0].Metadata.Fields[1].CanonicalUnit = semreg.DefinitionRef{}
	thermalDemand := semreg.DefinitionRef{Pack: thermalPack, ID: "thermal.demand.level", Version: "1.0.0"}
	if got, ok := registry.CanonicalUnit(thermalDemand); !ok || got.ID != "unit.percent" {
		t.Fatalf("input mutation changed accepted registry: %v, %t", got, ok)
	}
	returned := registry.Packs()
	returned[0] = thermalPack
	if got := registry.Packs()[0]; got != (semreg.PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}) {
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
			s[0].Metadata.Fields[1].CanonicalUnit = &u
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
