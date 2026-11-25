package embedded_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func TestEmbeddedUnionStructure(t *testing.T) {
	catalog, err := graph.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Validate(catalog, graph.AcceptedPackMetadata()); err != nil {
		t.Fatal(err)
	}
	for _, pack := range []semreg.PackRef{
		{ID: "helianthus.pack.equipment", Version: "1.0.0"},
		{ID: "helianthus.pack.energy_management", Version: "1.0.0"},
	} {
		found := false
		for _, source := range graph.AcceptedPackMetadata() {
			if source.Metadata.Pack == pack {
				found = true
			}
		}
		if !found {
			t.Fatalf("accepted metadata omits %s", pack.ID)
		}
	}
	site, ok := catalog.CoreEntry("relation.site_system")
	if !ok || site.Relation == nil || site.Relation.CoreType != graph.CoreGroups {
		t.Fatalf("relation.site_system = %+v", site.Relation)
	}
	planes, ok := catalog.PlanesOf(site.Ref)
	if !ok || !planeListed(planes, graph.PlanePhysical) {
		t.Fatalf("relation.site_system planes = %v", planes)
	}
	if _, ok := catalog.CoreEntry("relation.electrical_supply"); !ok {
		t.Fatal("relation.electrical_supply is absent")
	}
	relays, ok := catalog.CoreEntry("relation.relays")
	if !ok || relays.Relation == nil || !kindListed(relays.Relation.FromKinds, "resource.heat_generator") || !basisListed(relays.Relation.BasisClasses, semreg.RelationGateRule) {
		t.Fatalf("relation.relays = %+v", relays.Relation)
	}
	hydraulic := false
	for _, lens := range catalog.Lenses() {
		if lens.ID == "lens.hydraulic" {
			hydraulic = true
		}
	}
	if !hydraulic {
		t.Fatal("lens.hydraulic is absent")
	}
	event, ok := catalog.PackEntry(semreg.DefinitionRef{Pack: semreg.PackRef{ID: "helianthus.pack.equipment", Version: "1.0.0"}, ID: "equipment.event.reachable_changed", Version: "1.0.0"})
	if !ok || event.ElementKind != graph.KindEvent || event.Event == nil || event.Event.Trigger.ID != "equipment.status.reachable" {
		t.Fatalf("equipment.event.reachable_changed = %+v", event.Event)
	}
}

func TestHydraulicLensReturnsOneFluidSupply(t *testing.T) {
	catalog, err := graph.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	thermal := semreg.PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}
	fluid := semreg.DefinitionRef{Pack: thermal, ID: "thermal.relation.fluid_supply", Version: "1.0.0"}
	generator := unionResource("resource:heat-generator:01", "resource.heat_generator", semreg.AnchorEquipment, "generator")
	circuit := unionResource("resource:thermal-circuit:01", "resource.thermal_circuit", semreg.AnchorFunction, "circuit")
	snap := unionSnapshot(t, "asset:hydraulic", []semreg.Resource{generator, circuit}, []semreg.RelationInstance{unionRelation(fluid, "resource:heat-generator:01", "resource:thermal-circuit:01")})
	cut, err := graph.AssembleCut([]semreg.Snapshot{snap}, catalog, graph.CutComplete)
	if err != nil {
		t.Fatal(err)
	}
	store := graph.NewCutStore(func() time.Time { return time.Unix(1_700_000_000, 0).UTC() })
	if err := store.Retain(cut, []semreg.Snapshot{snap}, catalog); err != nil {
		t.Fatal(err)
	}
	result, err := graph.RetrieveLens(store, catalog, graph.LensRequest{
		Lens: "lens.hydraulic", Entry: graph.EntryPoint{Kind: graph.EntryLensRoots},
		Direction: graph.DirectionBoth, Depth: 8, PageSize: 16, Members: []semreg.Snapshot{snap},
	}, semreg.EvaluationContext{
		EvaluatedAt:       semreg.TimePoint{UnixNanoseconds: "130", ClockID: "clock.utc", UncertaintyNS: "0"},
		EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:cut", Nanoseconds: "20"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Lens != "lens.hydraulic" {
		t.Fatalf("lens = %s", result.Lens)
	}
	supplies := 0
	for _, relation := range result.Relations {
		if relation.Definition == fluid && relation.InPlane {
			supplies++
		}
	}
	if supplies != 1 {
		t.Fatalf("fluid-supply instances = %d among %+v", supplies, result.Relations)
	}
}

func planeListed(planes []graph.PlaneID, want graph.PlaneID) bool {
	for _, plane := range planes {
		if plane == want {
			return true
		}
	}
	return false
}

func kindListed(kinds []semreg.DefinitionID, want semreg.DefinitionID) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}

func basisListed(bases []semreg.RelationBasis, want semreg.RelationBasis) bool {
	for _, basis := range bases {
		if basis == want {
			return true
		}
	}
	return false
}

func unionEvidence(label string) semreg.EvidenceRef {
	sum := sha256.Sum256([]byte("union evidence " + label))
	return semreg.EvidenceRef{
		Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Contract: "test.contract/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone,
	}
}

func unionResource(id, kind string, class semreg.AnchorClass, slot string) semreg.Resource {
	return semreg.Resource{
		ResourceID: semreg.ResourceID(id), Kind: semreg.DefinitionID(kind),
		Anchor:   semreg.ResourceAnchor{Class: class, Basis: semreg.AnchorOperatorSlot, Slot: &slot, Evidence: []semreg.EvidenceRef{unionEvidence("anchor")}},
		State:    semreg.ResourceCurrent,
		Origin:   semreg.OriginRef{OriginID: semreg.OriginID("origin:operator:" + id), Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{unionEvidence("operator")}},
		Revision: "1",
	}
}

func unionRelation(definition semreg.DefinitionRef, from, to string) semreg.RelationInstance {
	return semreg.RelationInstance{
		Definition: definition, From: semreg.ResourceID(from), To: semreg.ResourceID(to), State: semreg.LinkQualified,
		BasisClass: semreg.RelationConfiguration, Basis: []semreg.EvidenceRef{unionEvidence("relation")},
		Origin:   semreg.OriginRef{OriginID: semreg.OriginID("origin:operator:" + from), Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{unionEvidence("operator")}},
		Revision: "1",
	}
}

func unionSnapshot(t *testing.T, asset semreg.AssetID, resources []semreg.Resource, relations []semreg.RelationInstance) semreg.Snapshot {
	t.Helper()
	source, epoch, binding := semreg.SourceID("source:"+asset), semreg.SourceEpochID("epoch:"+asset), semreg.NativeBindingID("binding:"+asset)
	received := semreg.TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"}
	evaluated := semreg.TimePoint{UnixNanoseconds: "101", ClockID: "clock.utc", UncertaintyNS: "0"}
	snapshot := semreg.Snapshot{
		Contract: semreg.ContractKernelV1, AssetID: asset,
		Revisions:         semreg.RevisionVector{Semantic: "1", Identity: "1", Structure: "1", Facts: "1", Services: "1", Capabilities: "1"},
		EvaluatedAt:       evaluated,
		EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:cut", Nanoseconds: "6"},
		Sources: []semreg.SourceDescriptor{{
			SourceID: source, SourceEpochID: epoch, ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "release-1",
			RegistryEvidence: unionEvidence("registry"), StartedAt: received, State: semreg.SourceCurrent, Revision: "1",
		}},
		Bindings:         []semreg.NativeBinding{{BindingID: binding, AssetID: asset, SourceID: source, SourceEpochID: epoch, DriverGeneration: "1", NativeResource: unionEvidence("native"), State: semreg.BindingCurrent, Revision: "1"}},
		IdentityLinks:    []semreg.IdentityLink{},
		Facts:            []semreg.FactEnvelope{},
		Services:         []semreg.ServiceInstance{},
		Capabilities:     []semreg.CapabilityInstance{},
		Fences:           []semreg.GenerationFence{},
		Cursors:          []semreg.PublicationCursor{{SourceID: source, SourceEpochID: epoch, DriverGeneration: "1", LastSequence: "1", LastBatchDigest: unionEvidence("batch").Digest}},
		Resources:        append([]semreg.Resource(nil), resources...),
		Relations:        append([]semreg.RelationInstance(nil), relations...),
		RelationAbsences: []semreg.RelationAbsence{},
	}
	if len(snapshot.Resources) > 1 && snapshot.Resources[0].ResourceID > snapshot.Resources[1].ResourceID {
		snapshot.Resources[0], snapshot.Resources[1] = snapshot.Resources[1], snapshot.Resources[0]
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	object := map[string]any{}
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	delete(object, "snapshot_id")
	canonical, err := semreg.CanonicalJSON(unionObject(object))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	snapshot.SnapshotID = semreg.SnapshotID("sha256:" + hex.EncodeToString(sum[:]))
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snapshot
}

type unionObject map[string]any

func (unionObject) Validate() error { return nil }
