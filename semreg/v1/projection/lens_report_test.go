package projection_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

const (
	lensComposition = semreg.DefinitionID("lens.equipment_composition")
	lensEnergy      = semreg.DefinitionID("lens.energy_flow")
	relationArray   = semreg.DefinitionID("pv.relation.array_string")
)

func lensDigest(label string) semreg.Digest {
	sum := sha256.Sum256([]byte("lens-report " + label))
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func lensEvidence(label string) semreg.EvidenceRef {
	sum := sha256.Sum256([]byte("lens-report evidence " + label))
	return evidence(hex.EncodeToString(sum[:]))
}

func lensOrigin(id string) semreg.OriginRef {
	return semreg.OriginRef{
		OriginID: semreg.OriginID("origin:operator:" + id), Kind: semreg.OriginOperator,
		Evidence: []semreg.EvidenceRef{lensEvidence(id)},
	}
}

func lensResource(id, slot string) semreg.Resource {
	slotText := slot
	return semreg.Resource{
		ResourceID: semreg.ResourceID(id), Kind: "resource.equipment",
		Anchor: semreg.ResourceAnchor{
			Class: semreg.AnchorEquipment, Basis: semreg.AnchorOperatorSlot, Slot: &slotText,
			Evidence: []semreg.EvidenceRef{lensEvidence("anchor-" + id)},
		},
		State: semreg.ResourceCurrent, Origin: lensOrigin(id), Revision: "1",
	}
}

func arrayStringRef() semreg.DefinitionRef {
	return semreg.DefinitionRef{
		Pack: semreg.PackRef{ID: "helianthus.pack.pv", Version: "1.0.0"}, ID: relationArray, Version: "1.0.0",
	}
}

func coherentKey() semreg.RelationKey {
	return semreg.RelationKey{Definition: arrayStringRef(), From: "resource:array", To: "resource:string"}
}

func lensRelation(key semreg.RelationKey, asset semreg.AssetID, coherence graph.RelationCoherence) graph.LensRelation {
	return graph.LensRelation{
		AssetID: asset, Definition: key.Definition, From: key.From, To: key.To,
		State: semreg.LinkQualified, Coherence: coherence, InPlane: true,
	}
}

func reportLens(t *testing.T, snapshot semreg.Snapshot, relations []graph.LensRelation) graph.LensResult {
	t.Helper()
	catalog, err := graph.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	return graph.LensResult{
		Contract: graph.LensResultContract, Lens: lensComposition, Catalog: catalog.Digest(),
		Cut: graph.CutRef{
			CutID: lensDigest("cut"), Completeness: graph.CutComplete,
			Members: []graph.CutMember{{AssetID: snapshot.AssetID, SnapshotID: snapshot.SnapshotID}},
		},
		Entry: graph.EntryPoint{Kind: graph.EntryLensRoots, Lens: lensComposition},
		Resources: []graph.LensResource{
			{Resource: lensResource("resource:array", "array"), Depth: 0, AssetID: snapshot.AssetID},
			{Resource: lensResource("resource:string", "string"), Depth: 1, AssetID: snapshot.AssetID},
		},
		Relations: relations,
	}
}

func lensManifest(id semreg.DefinitionID) projection.ProjectionManifest {
	manifest := manifest()
	manifest.Lens = definition(string(id))
	return manifest
}

func citedDisposition(resources []semreg.ResourceID, relations []semreg.RelationKey) projection.ProjectionDisposition {
	disposition := exact(projection.ItemRelation, relationArray)
	disposition.Resources = resources
	disposition.Relations = relations
	return disposition
}

func relationRequest() []projection.RequestedItem {
	return []projection.RequestedItem{{Kind: projection.ItemRelation, ItemID: relationArray}}
}

func projectLens(t *testing.T, snapshot semreg.Snapshot, manifest projection.ProjectionManifest, disposition projection.ProjectionDisposition, result *graph.LensResult) (projection.ProjectionReport, error) {
	t.Helper()
	if result == nil {
		return projection.Project(snapshot, manifest, relationRequest(), []projection.ProjectionDisposition{disposition}, nil)
	}
	return projection.Project(snapshot, manifest, relationRequest(), []projection.ProjectionDisposition{disposition}, nil, result)
}

func TestLensReportExistingBytes(t *testing.T) {
	snapshot := projectionSnapshot(t)
	requested := []projection.RequestedItem{{Kind: projection.ItemFact, ItemID: "helianthus.pv.active_power"}}
	dispositions := []projection.ProjectionDisposition{exact(projection.ItemFact, "helianthus.pv.active_power")}
	report, err := projection.Project(snapshot, manifest(), requested, dispositions, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := semreg.CanonicalJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := semreg.Decode[projection.ProjectionReport](raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := semreg.CanonicalJSON(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatalf("re-encode changed bytes\n%s\n%s", raw, again)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	var encodedManifest map[string]json.RawMessage
	if err := json.Unmarshal(body["manifest"], &encodedManifest); err != nil {
		t.Fatal(err)
	}
	if _, ok := encodedManifest["lens"]; ok {
		t.Fatalf("manifest lens was not omitted: %s", raw)
	}
	var encodedDispositions []map[string]json.RawMessage
	if err := json.Unmarshal(body["dispositions"], &encodedDispositions); err != nil {
		t.Fatal(err)
	}
	for _, disposition := range encodedDispositions {
		if _, ok := disposition["resources"]; ok {
			t.Fatalf("empty resources were not omitted: %s", raw)
		}
		if _, ok := disposition["relations"]; ok {
			t.Fatalf("empty relations were not omitted: %s", raw)
		}
	}
}

func TestLensReportOverLens(t *testing.T) {
	snapshot := projectionSnapshot(t)
	key := coherentKey()
	result := reportLens(t, snapshot, []graph.LensRelation{lensRelation(key, snapshot.AssetID, graph.CoherenceCoherent)})
	disposition := citedDisposition([]semreg.ResourceID{"resource:array", "resource:string"}, []semreg.RelationKey{key})
	report, err := projectLens(t, snapshot, lensManifest(lensComposition), disposition, &result)
	if err != nil {
		t.Fatal(err)
	}
	if report.Manifest.Lens == nil || *report.Manifest.Lens != lensComposition {
		t.Fatalf("lens = %v", report.Manifest.Lens)
	}
	if len(report.Dispositions) != 1 || len(report.Dispositions[0].Resources) != 2 || len(report.Dispositions[0].Relations) != 1 {
		t.Fatalf("citations = %+v", report.Dispositions)
	}
	raw, err := semreg.CanonicalJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := semreg.Decode[projection.ProjectionReport](raw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := semreg.CanonicalJSON(decoded)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatalf("lens report re-encode err=%v", err)
	}
	if _, err := semreg.Decode[projection.ProjectionReport](append(bytes.TrimSuffix(raw, []byte("}")), []byte(`,"future":true}`)...)); err == nil {
		t.Fatal("strict decode accepted an unknown member")
	}
}

func TestLensReportRelationNotCoherent(t *testing.T) {
	snapshot := projectionSnapshot(t)
	key := coherentKey()
	cited := key
	cited.To = "resource:other-string"
	for _, coherence := range []graph.RelationCoherence{
		graph.CoherenceStale, graph.CoherenceDangling, graph.CoherenceOutsideCut, graph.CoherenceExcluded, graph.CoherenceConflict,
	} {
		t.Run(string(coherence), func(t *testing.T) {
			result := reportLens(t, snapshot, []graph.LensRelation{
				lensRelation(key, snapshot.AssetID, graph.CoherenceCoherent),
				lensRelation(cited, snapshot.AssetID, coherence),
			})
			disposition := citedDisposition([]semreg.ResourceID{"resource:array", "resource:string"}, []semreg.RelationKey{cited})
			_, err := projectLens(t, snapshot, lensManifest(lensComposition), disposition, &result)
			if semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
				t.Fatalf("%s err=%v", coherence, err)
			}
		})
	}
}

func TestLensReportRelationDefinition(t *testing.T) {
	snapshot := projectionSnapshot(t)
	accept := func(kind projection.ItemKind, id semreg.DefinitionID) {
		t.Helper()
		requested := []projection.RequestedItem{{Kind: kind, ItemID: id}}
		disposition := exact(kind, id)
		if _, err := projection.Project(snapshot, manifest(), requested, []projection.ProjectionDisposition{disposition}, nil); err != nil {
			t.Fatalf("%s %s: %v", kind, id, err)
		}
	}
	refuse := func(id semreg.DefinitionID) {
		t.Helper()
		requested := []projection.RequestedItem{{Kind: projection.ItemRelation, ItemID: id}}
		disposition := exact(projection.ItemRelation, id)
		_, err := projection.Project(snapshot, manifest(), requested, []projection.ProjectionDisposition{disposition}, nil)
		if semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
			t.Fatalf("%s err=%v", id, err)
		}
	}
	// kind selects the catalog check: the same identifier is a fact request
	// and a relation request. item_id selects the definition: a member
	// relation is accepted and any other identifier is membership_missing.
	accept(projection.ItemFact, "relation.not_catalogued")
	accept(projection.ItemRelation, relationArray)
	accept(projection.ItemRelation, "relation.relays")
	refuse("relation.not_catalogued")
	refuse(lensComposition)
}

// TestLensReportCitationKey varies every field of the cited relation key and
// the cited resource id. The result holds one coherent key. Each mutation
// below is the production miss the test catches: a lookup that ignored that
// field would still find the coherent key and accept the report.
func TestLensReportCitationKey(t *testing.T) {
	snapshot := projectionSnapshot(t)
	key := coherentKey()
	result := reportLens(t, snapshot, []graph.LensRelation{lensRelation(key, snapshot.AssetID, graph.CoherenceCoherent)})
	mutate := []struct {
		name string
		key  semreg.RelationKey
	}{
		{"definition.pack.id", func() semreg.RelationKey {
			next := key
			next.Definition.Pack.ID = "helianthus.pack.storage"
			return next
		}()},
		{"definition.pack.version", func() semreg.RelationKey {
			next := key
			next.Definition.Pack.Version = "1.0.1"
			return next
		}()},
		{"definition.id", func() semreg.RelationKey {
			next := key
			next.Definition.ID = "pv.relation.inverter_array"
			return next
		}()},
		{"definition.version", func() semreg.RelationKey {
			next := key
			next.Definition.Version = "1.0.1"
			return next
		}()},
		{"from", func() semreg.RelationKey {
			next := key
			next.From = "resource:other-array"
			return next
		}()},
		{"to", func() semreg.RelationKey {
			next := key
			next.To = "resource:other-string"
			return next
		}()},
	}
	for _, test := range mutate {
		t.Run(test.name, func(t *testing.T) {
			disposition := citedDisposition([]semreg.ResourceID{"resource:array", "resource:string"}, []semreg.RelationKey{test.key})
			_, err := projectLens(t, snapshot, lensManifest(lensComposition), disposition, &result)
			if semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
				t.Fatalf("%s err=%v", test.name, err)
			}
		})
	}
	t.Run("resource_id", func(t *testing.T) {
		disposition := citedDisposition([]semreg.ResourceID{"resource:array", "resource:missing"}, []semreg.RelationKey{key})
		_, err := projectLens(t, snapshot, lensManifest(lensComposition), disposition, &result)
		if semreg.ErrorIdentifier(err) != semreg.DanglingReference {
			t.Fatalf("resource_id err=%v", err)
		}
	})
}

// TestLensReportSourceIdentity varies the lens identifier, the catalog digest
// and both fields of the cut member. A comparison that ignored one of those
// fields would accept the report against a different lens, catalog or snapshot.
func TestLensReportSourceIdentity(t *testing.T) {
	snapshot := projectionSnapshot(t)
	key := coherentKey()
	result := reportLens(t, snapshot, []graph.LensRelation{lensRelation(key, snapshot.AssetID, graph.CoherenceCoherent)})
	disposition := citedDisposition([]semreg.ResourceID{"resource:array", "resource:string"}, []semreg.RelationKey{key})
	t.Run("lens", func(t *testing.T) {
		_, err := projectLens(t, snapshot, lensManifest(lensEnergy), disposition, &result)
		if semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
			t.Fatalf("lens err=%v", err)
		}
	})
	t.Run("catalog", func(t *testing.T) {
		changed := result
		changed.Catalog = lensDigest("other-catalog")
		_, err := projectLens(t, snapshot, lensManifest(lensComposition), disposition, &changed)
		if semreg.ErrorIdentifier(err) != semreg.DigestMismatch {
			t.Fatalf("catalog err=%v", err)
		}
	})
	t.Run("asset_id", func(t *testing.T) {
		changed := result
		changed.Cut.Members = []graph.CutMember{{AssetID: "asset:other", SnapshotID: snapshot.SnapshotID}}
		_, err := projectLens(t, snapshot, lensManifest(lensComposition), disposition, &changed)
		if semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
			t.Fatalf("asset_id err=%v", err)
		}
	})
	t.Run("snapshot_id", func(t *testing.T) {
		changed := result
		changed.Cut.Members = []graph.CutMember{{AssetID: snapshot.AssetID, SnapshotID: semreg.SnapshotID(lensDigest("other-snapshot"))}}
		_, err := projectLens(t, snapshot, lensManifest(lensComposition), disposition, &changed)
		if semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
			t.Fatalf("snapshot_id err=%v", err)
		}
	})
	t.Run("absent lens result", func(t *testing.T) {
		_, err := projectLens(t, snapshot, lensManifest(lensComposition), disposition, nil)
		if semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
			t.Fatalf("absent err=%v", err)
		}
	})
	t.Run("unknown lens", func(t *testing.T) {
		_, err := projectLens(t, snapshot, lensManifest("lens.not_catalogued"), disposition, nil)
		if semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
			t.Fatalf("unknown lens err=%v", err)
		}
	})
}

func TestLensReportCitationOrder(t *testing.T) {
	swappedResources := citedDisposition([]semreg.ResourceID{"resource:string", "resource:array"}, nil)
	if semreg.ErrorIdentifier(swappedResources.Validate()) != semreg.NoncanonicalOrder {
		t.Fatalf("resource order: %v", swappedResources.Validate())
	}
	duplicateResources := citedDisposition([]semreg.ResourceID{"resource:array", "resource:array"}, nil)
	if semreg.ErrorIdentifier(duplicateResources.Validate()) != semreg.DuplicateKey {
		t.Fatalf("duplicate resources: %v", duplicateResources.Validate())
	}
	first, second := coherentKey(), coherentKey()
	second.To = "resource:other-string"
	swappedRelations := citedDisposition(nil, []semreg.RelationKey{first, second})
	if semreg.ErrorIdentifier(swappedRelations.Validate()) != semreg.NoncanonicalOrder {
		t.Fatalf("relation order: %v", swappedRelations.Validate())
	}
	duplicateRelations := citedDisposition(nil, []semreg.RelationKey{first, first})
	if semreg.ErrorIdentifier(duplicateRelations.Validate()) != semreg.DuplicateKey {
		t.Fatalf("duplicate relations: %v", duplicateRelations.Validate())
	}
	emptyResources := exact(projection.ItemFact, "helianthus.pv.active_power")
	emptyResources.Resources = []semreg.ResourceID{}
	if semreg.ErrorIdentifier(emptyResources.Validate()) != semreg.InvalidValue {
		t.Fatalf("present empty resources: %v", emptyResources.Validate())
	}
	emptyRelations := exact(projection.ItemFact, "helianthus.pv.active_power")
	emptyRelations.Relations = []semreg.RelationKey{}
	if semreg.ErrorIdentifier(emptyRelations.Validate()) != semreg.InvalidValue {
		t.Fatalf("present empty relations: %v", emptyRelations.Validate())
	}
	raw, err := semreg.CanonicalJSON(exact(projection.ItemFact, "helianthus.pv.active_power"))
	if err != nil {
		t.Fatal(err)
	}
	presentEmpty := bytes.Replace(raw, []byte(`"source_keys"`), []byte(`"resources":[],"source_keys"`), 1)
	if _, err := semreg.Decode[projection.ProjectionDisposition](presentEmpty); semreg.ErrorIdentifier(err) != semreg.InvalidValue {
		t.Fatalf("strict decode of present empty resources: %v", err)
	}
}
