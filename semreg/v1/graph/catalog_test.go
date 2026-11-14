package graph_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

const fixturePath = "../../../fixtures/v1/element-catalog-v1.json"

var (
	evsePack    = semreg.PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}
	pvPack      = semreg.PackRef{ID: "helianthus.pack.pv", Version: "1.0.0"}
	storagePack = semreg.PackRef{ID: "helianthus.pack.storage", Version: "1.1.0"}
)

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read catalog fixture: %v", err)
	}
	return raw
}

func committedCatalog(t *testing.T) *graph.Catalog {
	t.Helper()
	catalog, err := graph.Load(fixtureBytes(t))
	if err != nil {
		t.Fatalf("Load(committed catalog) = %v", err)
	}
	return catalog
}

func evseRef(id string) semreg.DefinitionRef {
	return semreg.DefinitionRef{Pack: evsePack, ID: semreg.DefinitionID(id), Version: "1.0.0"}
}

// The pin is recomputed from the fixture bytes; the test never repeats it.
func TestPinnedDigestIsTheFixtureDigest(t *testing.T) {
	raw := fixtureBytes(t)
	sum := sha256.Sum256(raw)
	computed := semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
	if graph.PinnedDigest != computed {
		t.Fatalf("PinnedDigest = %s, fixture bytes hash to %s", graph.PinnedDigest, computed)
	}
	catalog := committedCatalog(t)
	if catalog.Digest() != computed {
		t.Fatalf("Digest() = %s, want %s", catalog.Digest(), computed)
	}
	if !bytes.Equal(catalog.Bytes(), raw) {
		t.Fatal("Bytes() differs from the fixture bytes")
	}
}

func TestLoadRejectsBytesOtherThanThePinnedFixture(t *testing.T) {
	raw := append(append([]byte(nil), fixtureBytes(t)...), '\n')
	_, err := graph.Load(raw)
	if got := semreg.ErrorIdentifier(err); err == nil || got != semreg.DigestMismatch {
		t.Fatalf("Load(fixture + newline) = %v (%s), want digest_mismatch", err, got)
	}
}

func TestCommittedCatalogIsAccepted(t *testing.T) {
	catalog := committedCatalog(t)
	findings, err := graph.Check(catalog, graph.AcceptedPackMetadata())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, finding := range findings {
		t.Errorf("unexpected finding: %s", finding)
	}
	if err := graph.Validate(catalog, graph.AcceptedPackMetadata()); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	counts := map[graph.ElementKind]int{}
	for _, element := range catalog.Elements() {
		counts[element.ElementKind]++
	}
	want := map[graph.ElementKind]int{
		graph.KindResourceKind: 28, graph.KindRelation: 22, graph.KindDimension: 34, graph.KindService: 29,
		graph.KindFact: 88, graph.KindCapability: 32, graph.KindOperation: 7, graph.KindEffectRule: 7,
		graph.KindEvidenceState: 12, graph.KindLifecycleState: 13, graph.KindPresentation: 35,
	}
	for kind, n := range want {
		if counts[kind] != n {
			t.Errorf("%s entries = %d, want %d", kind, counts[kind], n)
		}
	}
	if n := len(catalog.Elements()); n != 307 {
		t.Errorf("entries = %d, want 307", n)
	}
	if n := len(catalog.Planes()); n != 6 {
		t.Errorf("planes = %d, want 6", n)
	}
	if n := len(catalog.CoreRelationTypes()); n != 7 {
		t.Errorf("core relation types = %d, want 7", n)
	}
	if n := len(catalog.IdentificationSchemes()); n != 0 {
		t.Errorf("identification schemes = %d, want the empty seed", n)
	}
	if n := len(catalog.Lenses()); n != 0 {
		t.Errorf("lenses = %d, want the empty seed", n)
	}
}

func TestLookups(t *testing.T) {
	catalog := committedCatalog(t)

	relationRef := evseRef("evse.relation.asset_evse")
	entry, ok := catalog.PackEntry(relationRef)
	if !ok || entry.ElementKind != graph.KindRelation || entry.Disposition != graph.DispositionMember {
		t.Fatalf("PackEntry(asset_evse) = %+v, %v", entry, ok)
	}
	relation, ok := catalog.Relation(graph.PackElement(relationRef))
	if !ok {
		t.Fatal("Relation(asset_evse) not found")
	}
	if relation.CoreType != graph.CoreContains || relation.Orientation != graph.OrientationAsCore ||
		relation.Cardinality != graph.CardinalityOneToMany || relation.OverTime || relation.Medium != "" {
		t.Fatalf("Relation(asset_evse) = %+v", relation)
	}
	if got := relation.BasisClasses; len(got) != 1 || got[0] != semreg.RelationConfiguration {
		t.Fatalf("basis classes = %v", got)
	}
	from, to, ok := catalog.RelationEndpointKinds(graph.PackElement(relationRef))
	if !ok || len(from) != 1 || from[0] != "resource.charging_station" || len(to) != 1 || to[0] != "resource.evse" {
		t.Fatalf("RelationEndpointKinds = %v %v %v", from, to, ok)
	}
	planes, ok := catalog.PlanesOf(graph.PackElement(relationRef))
	if !ok || strings.Join(planeStrings(planes), ",") != "physical,energy,diagnostic" {
		t.Fatalf("PlanesOf(asset_evse) = %v %v", planes, ok)
	}

	supplies, ok := catalog.Relation(graph.PackElement(semreg.DefinitionRef{
		Pack: semreg.PackRef{ID: "helianthus.pack.infrastructure", Version: "1.0.0"},
		ID:   "infrastructure.relation.feeder_circuit", Version: "1.0.0",
	}))
	if !ok || supplies.CoreType != graph.CoreSupplies || supplies.Medium != graph.MediumElectrical {
		t.Fatalf("Relation(feeder_circuit) = %+v %v", supplies, ok)
	}

	if kind, ok := catalog.DimensionKind(evseRef("evse.dimension.asset")); !ok || kind != "resource.charging_station" {
		t.Fatalf("DimensionKind(evse asset) = %q %v", kind, ok)
	}
	if kind, ok := catalog.DimensionKind(semreg.DefinitionRef{
		Pack: semreg.PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}, ID: "thermal.dimension.power", Version: "1.0.0",
	}); ok {
		t.Fatalf("a qualifier dimension has no kind, got %q", kind)
	}
	if kind, ok := catalog.ServiceSubjectKind(evseRef("evse.service.connector")); !ok || kind != "resource.connector" {
		t.Fatalf("ServiceSubjectKind(connector) = %q %v", kind, ok)
	}

	core, ok := catalog.CoreEntry("relation.relays")
	if !ok || core.Ref.Owner.Kind != graph.OwnerCore || core.ElementKind != graph.KindRelation {
		t.Fatalf("CoreEntry(relation.relays) = %+v %v", core, ok)
	}
	if disposition, ok := catalog.DispositionOf(graph.CoreElement("resource.domestic_hot_water", "1.0.0")); !ok ||
		disposition != graph.DispositionUnknown {
		t.Fatalf("DispositionOf(domestic_hot_water) = %q %v", disposition, ok)
	}
	if disposition, ok := catalog.DispositionOf(graph.CoreElement("kernel.item_kind", "1.0.0")); !ok ||
		disposition != graph.DispositionUnsupported {
		t.Fatalf("DispositionOf(kernel.item_kind) = %q %v", disposition, ok)
	}
	if _, ok := catalog.CoreEntry("evse.relation.asset_evse"); ok {
		t.Fatal("CoreEntry must not return a pack-owned entry")
	}
}

// Each field of the lookup key is varied alone; every variation must miss.
//   - pack id: a lookup that ignored the owning pack would still hit;
//   - pack version: a lookup that ignored the pack version would still hit;
//   - id: a lookup that matched on the pack only would still hit;
//   - version: a lookup that ignored the element version would still hit;
//   - owner kind: a lookup that matched a core entry by its bare id would hit.
func TestLookupKeyVariesEveryField(t *testing.T) {
	catalog := committedCatalog(t)
	base := evseRef("evse.service.connector")
	if _, ok := catalog.PackEntry(base); !ok {
		t.Fatal("base reference must resolve")
	}
	variants := map[string]semreg.DefinitionRef{
		"pack id":      {Pack: semreg.PackRef{ID: "helianthus.pack.pv", Version: base.Pack.Version}, ID: base.ID, Version: base.Version},
		"pack version": {Pack: semreg.PackRef{ID: base.Pack.ID, Version: "1.1.0"}, ID: base.ID, Version: base.Version},
		"id":           {Pack: base.Pack, ID: "evse.service.session", Version: base.Version},
		"version":      {Pack: base.Pack, ID: base.ID, Version: "1.0.1"},
	}
	for name, ref := range variants {
		if name == "id" {
			// A different existing id resolves to a different entry, never to the base.
			entry, ok := catalog.PackEntry(ref)
			if ok && entry.Ref.ID == base.ID {
				t.Errorf("%s variant resolved to the base entry", name)
			}
			continue
		}
		if entry, ok := catalog.PackEntry(ref); ok {
			t.Errorf("%s variant resolved to %+v", name, entry.Ref)
		}
	}
	if _, ok := catalog.Entry(graph.CoreElement(base.ID, base.Version)); ok {
		t.Error("owner kind variant resolved a pack entry as core")
	}
	if _, ok := catalog.Entry(graph.CoreElement("relation.relays", "1.0.0")); !ok {
		t.Error("core entry by exact core reference must resolve")
	}
	if _, ok := catalog.Entry(graph.CoreElement("relation.relays", "1.0.1")); ok {
		t.Error("core entry with another version must miss")
	}
}

func TestLookupsReturnDetachedCopies(t *testing.T) {
	catalog := committedCatalog(t)
	ref := graph.PackElement(evseRef("evse.relation.asset_evse"))
	relation, _ := catalog.Relation(ref)
	relation.FromKinds[0] = "resource.mutated"
	planes, _ := catalog.PlanesOf(ref)
	planes[0] = "mutated"
	entry, _ := catalog.Entry(ref)
	entry.Relation.ToKinds[0] = "resource.mutated"
	entry.Admission.Evidence[0] = "mutated"
	elements := catalog.Elements()
	elements[0].Planes = nil
	fresh, _ := catalog.Relation(ref)
	if fresh.FromKinds[0] != "resource.charging_station" || fresh.ToKinds[0] != "resource.evse" {
		t.Fatalf("relation mutated through a lookup: %+v", fresh)
	}
	if again, _ := catalog.PlanesOf(ref); again[0] != graph.PlanePhysical {
		t.Fatalf("planes mutated through a lookup: %v", again)
	}
	if again, _ := catalog.Entry(ref); again.Admission.Evidence[0] == "mutated" {
		t.Fatal("admission evidence mutated through a lookup")
	}
	if len(catalog.Elements()[0].Planes) == 0 {
		t.Fatal("element planes mutated through Elements")
	}
	if findings, err := graph.Check(catalog, graph.AcceptedPackMetadata()); err != nil || len(findings) != 0 {
		t.Fatalf("catalog changed after lookups: %v %v", findings, err)
	}
}

func planeStrings(planes []graph.PlaneID) []string {
	out := make([]string, len(planes))
	for i, plane := range planes {
		out[i] = string(plane)
	}
	return out
}

// ---------------------------------------------------------------- mutations

type document = map[string]any

func decodeDocument(t *testing.T) document {
	t.Helper()
	var doc document
	if err := json.Unmarshal(fixtureBytes(t), &doc); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return doc
}

func elements(doc document) []any { return doc["elements"].([]any) }

func element(t *testing.T, doc document, id string) document {
	t.Helper()
	for _, item := range elements(doc) {
		entry := item.(document)
		if entry["ref"].(document)["id"] == id {
			return entry
		}
	}
	t.Fatalf("no element %s", id)
	return nil
}

func removeElement(t *testing.T, doc document, id string) {
	t.Helper()
	kept := []any{}
	found := false
	for _, item := range elements(doc) {
		if item.(document)["ref"].(document)["id"] == id {
			found = true
			continue
		}
		kept = append(kept, item)
	}
	if !found {
		t.Fatalf("no element %s", id)
	}
	doc["elements"] = kept
}

func clone(t *testing.T, value any) document {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out document
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func orderKey(item any) string {
	ref := item.(document)["ref"].(document)
	owner := ref["owner"].(document)
	packID, packVersion := "", ""
	if pack, ok := owner["pack"].(document); ok {
		packID, packVersion = pack["id"].(string), pack["version"].(string)
	}
	return strings.Join([]string{owner["kind"].(string), packID, packVersion, ref["id"].(string), ref["version"].(string)}, "\x00")
}

func sortElements(doc document) {
	items := elements(doc)
	sort.SliceStable(items, func(i, j int) bool { return orderKey(items[i]) < orderKey(items[j]) })
}

func elementRef(owner document, id string) document {
	return document{"owner": owner, "id": id, "version": "1.0.0"}
}

func packOwner(pack semreg.PackRef) document {
	return document{"kind": "pack", "pack": document{"id": string(pack.ID), "version": string(pack.Version)}}
}

func encode(t *testing.T, doc document) []byte {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func findingsOf(t *testing.T, doc document) []graph.Finding {
	t.Helper()
	catalog, err := graph.Decode(encode(t, doc))
	if err != nil {
		t.Fatalf("Decode(mutated) = %v", err)
	}
	findings, err := graph.Check(catalog, graph.AcceptedPackMetadata())
	if err != nil {
		t.Fatalf("Check(mutated) = %v", err)
	}
	return findings
}

func requireFinding(t *testing.T, findings []graph.Finding, rule, clause string, id semreg.ErrorID, entry string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Rule == rule && finding.Clause == clause && finding.ID == id && strings.Contains(finding.Entry, entry) {
			return
		}
	}
	t.Fatalf("no %s %s %s finding for %q among:\n%s", rule, clause, id, entry, joinFindings(findings))
}

func requireOnlyRule(t *testing.T, findings []graph.Finding, rule string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Rule != rule {
			t.Fatalf("unexpected %s finding beside %s:\n%s", finding.Rule, rule, joinFindings(findings))
		}
	}
}

func joinFindings(findings []graph.Finding) string {
	lines := make([]string, len(findings))
	for i, finding := range findings {
		lines[i] = finding.String()
	}
	return strings.Join(lines, "\n")
}

func TestValidateReportsTheFirstFindingAsStableError(t *testing.T) {
	doc := decodeDocument(t)
	removeElement(t, doc, "evse.ac.voltage")
	catalog, err := graph.Decode(encode(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	err = graph.Validate(catalog, graph.AcceptedPackMetadata())
	if got := semreg.ErrorIdentifier(err); err == nil || got != semreg.MembershipMissing {
		t.Fatalf("Validate = %v (%s), want membership_missing", err, got)
	}
	if _, err := graph.Load(encode(t, doc)); semreg.ErrorIdentifier(err) != semreg.DigestMismatch {
		t.Fatalf("Load(mutated) = %v, want digest_mismatch before any rule", err)
	}
}

func TestCAT1Completeness(t *testing.T) {
	t.Run("pack field missing", func(t *testing.T) {
		doc := decodeDocument(t)
		removeElement(t, doc, "evse.ac.voltage")
		requireFinding(t, findingsOf(t, doc), "CAT-1", "", semreg.MembershipMissing, "evse.ac.voltage@1.0.0")
	})
	t.Run("extra pack reference", func(t *testing.T) {
		doc := decodeDocument(t)
		extra := clone(t, element(t, doc, "evse.ac.voltage"))
		extra["ref"].(document)["id"] = "evse.ac.extra_voltage"
		doc["elements"] = append(elements(doc), extra)
		sortElements(doc)
		requireFinding(t, findingsOf(t, doc), "CAT-1", "", semreg.DefinitionOwnerMissing, "evse.ac.extra_voltage")
	})
	t.Run("entry of a pack that is not accepted", func(t *testing.T) {
		doc := decodeDocument(t)
		extra := clone(t, element(t, doc, "evse.portal.read.evse"))
		extra["ref"].(document)["owner"] = packOwner(semreg.PackRef{ID: "helianthus.pack.synthetic", Version: "1.0.0"})
		extra["ref"].(document)["id"] = "synthetic.portal.read"
		doc["elements"] = append(elements(doc), extra)
		sortElements(doc)
		requireFinding(t, findingsOf(t, doc), "CAT-1", "", semreg.DefinitionOwnerMissing, "synthetic.portal.read")
	})
	t.Run("kernel state type missing", func(t *testing.T) {
		doc := decodeDocument(t)
		removeElement(t, doc, "kernel.ack_state")
		requireFinding(t, findingsOf(t, doc), "CAT-1", "", semreg.MembershipMissing, "kernel.ack_state")
	})
	t.Run("kernel state type that the contract does not list", func(t *testing.T) {
		doc := decodeDocument(t)
		extra := clone(t, element(t, doc, "kernel.ack_state"))
		extra["ref"].(document)["id"] = "kernel.synthetic_state"
		extra["attributes"].(document)["kernel_type"] = "SyntheticState"
		doc["elements"] = append(elements(doc), extra)
		sortElements(doc)
		requireFinding(t, findingsOf(t, doc), "CAT-1", "", semreg.DefinitionOwnerMissing, "kernel.synthetic_state")
	})
	t.Run("duplicate entry", func(t *testing.T) {
		doc := decodeDocument(t)
		doc["elements"] = append(elements(doc), clone(t, element(t, doc, "evse.ac.voltage")))
		sortElements(doc)
		requireFinding(t, findingsOf(t, doc), "CAT-1", "", semreg.DuplicateIdentity, "evse.ac.voltage")
	})
	t.Run("wrong element kind for a pack reference", func(t *testing.T) {
		doc := decodeDocument(t)
		removeElement(t, doc, "evse.ac.voltage")
		element(t, doc, "evse.portal.read.evse")["ref"].(document)["id"] = "evse.ac.voltage"
		sortElements(doc)
		requireFinding(t, findingsOf(t, doc), "CAT-1", "", semreg.InvalidValue, "evse.ac.voltage")
	})
}

func TestCAT2Agreement(t *testing.T) {
	t.Run("service fact-key dimension", func(t *testing.T) {
		doc := decodeDocument(t)
		attributes := element(t, doc, "evse.service.connector")["attributes"].(document)
		attributes["fact_key_dimension"] = elementRef(packOwner(evsePack), "evse.dimension.phase")
		attributes["subject_kind"] = "resource.phase"
		requireFinding(t, findingsOf(t, doc), "CAT-2", "", semreg.InvalidValue, "evse.service.connector")
	})
	t.Run("capability service", func(t *testing.T) {
		doc := decodeDocument(t)
		attributes := element(t, doc, "evse.capability.read.connector")["attributes"].(document)
		attributes["service"] = elementRef(packOwner(evsePack), "evse.service.evse")
		requireFinding(t, findingsOf(t, doc), "CAT-2", "", semreg.InvalidValue, "evse.capability.read.connector")
	})
	t.Run("value kind against the canonical unit", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.status.fault")["attributes"].(document)["value_kind"] = "quantity"
		requireFinding(t, findingsOf(t, doc), "CAT-2", "", semreg.InvalidValue, "evse.status.fault")
	})
	t.Run("fact subject dimension", func(t *testing.T) {
		doc := decodeDocument(t)
		subject := element(t, doc, "evse.ac.voltage")["attributes"].(document)["subject"].(document)
		subject["dimension"] = elementRef(packOwner(evsePack), "evse.dimension.evse")
		subject["kind"] = "resource.evse"
		requireFinding(t, findingsOf(t, doc), "CAT-2", "", semreg.InvalidValue, "evse.ac.voltage")
	})
	t.Run("subject kind against the dimension kind", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.service.connector")["attributes"].(document)["subject_kind"] = "resource.evse"
		requireFinding(t, findingsOf(t, doc), "CAT-2", "", semreg.InvalidValue, "evse.service.connector")
	})
	t.Run("dimension role", func(t *testing.T) {
		doc := decodeDocument(t)
		attributes := element(t, doc, "evse.dimension.connector")["attributes"].(document)
		attributes["role"] = "qualifier"
		delete(attributes, "resource_kind")
		requireFinding(t, findingsOf(t, doc), "CAT-2", "", semreg.InvalidValue, "evse.dimension.connector")
	})
	t.Run("operation effect rule", func(t *testing.T) {
		doc := decodeDocument(t)
		attributes := element(t, doc, "evse.effect.set_allocated_current")["attributes"].(document)
		attributes["fact"] = elementRef(packOwner(evsePack), "evse.ac.voltage")
		requireFinding(t, findingsOf(t, doc), "CAT-2", "", semreg.InvalidValue, "evse.effect.set_allocated_current")
	})
}

func TestCAT3Dispositions(t *testing.T) {
	cases := map[string]struct {
		id     string
		mutate func(document)
	}{
		"member without planes": {"evse.ac.voltage", func(entry document) { entry["planes"] = []any{} }},
		"member with a reason": {"evse.ac.voltage", func(entry document) {
			entry["reason"] = "catalog.reason.synthetic"
		}},
		"unknown without tracking": {"resource.domestic_hot_water", func(entry document) { delete(entry, "tracking") }},
		"unknown in a plane":       {"resource.domestic_hot_water", func(entry document) { entry["planes"] = []any{"physical"} }},
		"unsupported with tracking": {"kernel.item_kind", func(entry document) {
			entry["tracking"] = "https://github.com/Project-Helianthus/helianthus-docs-semantic/issues/68"
		}},
		"unsupported without reason": {"kernel.item_kind", func(entry document) { delete(entry, "reason") }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			doc := decodeDocument(t)
			c.mutate(element(t, doc, c.id))
			requireFinding(t, findingsOf(t, doc), "CAT-3", "", semreg.MembershipMissing, c.id)
		})
	}
}

func TestCAT4Closure(t *testing.T) {
	t.Run("PLN-5 fact outside its subject kind planes", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.ac.voltage")["planes"] = []any{"energy", "hydraulic"}
		requireFinding(t, findingsOf(t, doc), "CAT-4", "PLN-5", semreg.MembershipMissing, "evse.ac.voltage")
	})
	t.Run("PLN-5 resource dimension planes differ from its kind", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.dimension.connector")["planes"] = []any{"physical"}
		requireFinding(t, findingsOf(t, doc), "CAT-4", "PLN-5", semreg.MembershipMissing, "evse.dimension.connector")
	})
	t.Run("PLN-5 effect rule planes differ from its operation", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.effect.set_allocated_current")["planes"] = []any{"control"}
		requireFinding(t, findingsOf(t, doc), "CAT-4", "PLN-5", semreg.MembershipMissing, "evse.effect.set_allocated_current")
	})
	t.Run("PLN-6 relation without the required plane", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.relation.asset_evse")["planes"] = []any{"energy", "diagnostic"}
		requireFinding(t, findingsOf(t, doc), "CAT-4", "PLN-6", semreg.MembershipMissing, "evse.relation.asset_evse")
	})
	t.Run("PLN-6 relation outside its core type planes", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.relation.asset_evse")["planes"] = []any{"physical", "energy", "control"}
		requireFinding(t, findingsOf(t, doc), "CAT-4", "PLN-6", semreg.MembershipMissing, "evse.relation.asset_evse")
	})
	t.Run("PLN-6 planes not given by the medium", func(t *testing.T) {
		doc := decodeDocument(t)
		entry := element(t, doc, "infrastructure.relation.feeder_circuit")
		entry["planes"] = append(entry["planes"].([]any), "hydraulic")
		requireFinding(t, findingsOf(t, doc), "CAT-4", "PLN-6", semreg.MembershipMissing, "infrastructure.relation.feeder_circuit")
	})
	t.Run("LNS-1 root kind with disposition unknown", func(t *testing.T) {
		doc := decodeDocument(t)
		doc["lenses"] = []any{document{
			"id": "lens.synthetic", "question": "What does the synthetic lens show?", "planes": []any{"physical"},
			"root_kinds": []any{"resource.domestic_hot_water"}, "max_depth": 2,
		}}
		requireFinding(t, findingsOf(t, doc), "CAT-4", "LNS-1", semreg.MembershipMissing, "lens.synthetic")
	})
	t.Run("LNS-1 relation outside the lens planes", func(t *testing.T) {
		doc := decodeDocument(t)
		doc["lenses"] = []any{document{
			"id": "lens.synthetic", "question": "What does the synthetic lens show?", "planes": []any{"control"},
			"root_kinds": []any{"resource.evse"}, "max_depth": 2,
			"relations": []any{elementRef(packOwner(evsePack), "evse.relation.asset_evse")},
		}}
		requireFinding(t, findingsOf(t, doc), "CAT-4", "LNS-1", semreg.MembershipMissing, "lens.synthetic")
	})
	t.Run("LNS-1 holds for a closed lens", func(t *testing.T) {
		doc := decodeDocument(t)
		doc["lenses"] = []any{document{
			"id": "lens.synthetic", "question": "What does the synthetic lens show?", "planes": []any{"physical"},
			"root_kinds": []any{"resource.charging_station"}, "max_depth": 2,
			"relations": []any{elementRef(packOwner(evsePack), "evse.relation.asset_evse")},
		}}
		if findings := findingsOf(t, doc); len(findings) != 0 {
			t.Fatalf("closed lens rejected:\n%s", joinFindings(findings))
		}
	})
	t.Run("duplicate plane identifier", func(t *testing.T) {
		doc := decodeDocument(t)
		doc["planes"] = append(doc["planes"].([]any), clone(t, doc["planes"].([]any)[0]))
		requireFinding(t, findingsOf(t, doc), "CAT-4", "", semreg.DuplicateIdentity, "plane physical")
	})
	t.Run("duplicate core type identifier", func(t *testing.T) {
		doc := decodeDocument(t)
		types := doc["core_relation_types"].([]any)
		types[6] = clone(t, types[0])
		findings := findingsOf(t, doc)
		requireFinding(t, findings, "CAT-4", "", semreg.DuplicateIdentity, "core type contains")
		requireFinding(t, findings, "CAT-4", "", semreg.MembershipMissing, "core type relays")
	})
	t.Run("core type planes differ from the contract", func(t *testing.T) {
		doc := decodeDocument(t)
		doc["core_relation_types"].([]any)[0].(document)["required_planes"] = []any{}
		requireFinding(t, findingsOf(t, doc), "CAT-4", "", semreg.InvalidValue, "core type contains")
	})
	t.Run("reference to an absent entry", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.capability.read.connector")["attributes"].(document)["service"] =
			elementRef(packOwner(evsePack), "evse.service.synthetic")
		requireFinding(t, findingsOf(t, doc), "CAT-4", "", semreg.MembershipMissing, "evse.capability.read.connector")
	})
	t.Run("kind that is not a resource kind", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.relation.asset_evse")["attributes"].(document)["to_kinds"] = []any{"resource.synthetic"}
		requireFinding(t, findingsOf(t, doc), "CAT-4", "", semreg.MembershipMissing, "evse.relation.asset_evse")
	})
}

func TestCAT5Ownership(t *testing.T) {
	t.Run("pack relation crossing packs", func(t *testing.T) {
		doc := decodeDocument(t)
		attributes := element(t, doc, "evse.relation.asset_evse")["attributes"].(document)
		attributes["from_kinds"] = []any{"resource.inverter"}
		attributes["endpoint_dimensions"].(document)["from"] = elementRef(packOwner(pvPack), "pv.dimension.inverter")
		requireFinding(t, findingsOf(t, doc), "CAT-5", "", semreg.DefinitionOwnerConflict, "evse.relation.asset_evse")
	})
	t.Run("pack relation to the core equipment kind", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.relation.asset_evse")["attributes"].(document)["from_kinds"] = []any{"resource.equipment"}
		requireFinding(t, findingsOf(t, doc), "CAT-5", "", semreg.DefinitionOwnerConflict, "evse.relation.asset_evse")
	})
	t.Run("core-owned fact", func(t *testing.T) {
		doc := decodeDocument(t)
		entry := clone(t, element(t, doc, "evse.status.fault"))
		entry["ref"] = document{"owner": document{"kind": "core", "contract": "helianthus.semantic.kernel/v1"}, "id": "fault.synthetic", "version": "1.0.0"}
		doc["elements"] = append(elements(doc), entry)
		sortElements(doc)
		requireFinding(t, findingsOf(t, doc), "CAT-5", "", semreg.DefinitionOwnerConflict, "fault.synthetic")
	})
	t.Run("native namespace outside a native owner", func(t *testing.T) {
		doc := decodeDocument(t)
		entry := clone(t, element(t, doc, "resource.domestic_hot_water"))
		entry["ref"].(document)["id"] = "native.synthetic_kind"
		doc["elements"] = append(elements(doc), entry)
		sortElements(doc)
		requireFinding(t, findingsOf(t, doc), "CAT-5", "", semreg.DefinitionOwnerConflict, "native.synthetic_kind")
	})
	t.Run("native extension entry that is a member", func(t *testing.T) {
		doc := decodeDocument(t)
		entry := clone(t, element(t, doc, "resource.equipment"))
		entry["ref"] = document{
			"owner": document{"kind": "native", "repository": "https://github.com/Project-Helianthus/helianthus-docs-synthetic"},
			"id":    "native.synthetic_kind", "version": "1.0.0",
		}
		doc["elements"] = append(elements(doc), entry)
		sortElements(doc)
		requireFinding(t, findingsOf(t, doc), "CAT-5", "", semreg.DefinitionOwnerConflict, "native.synthetic_kind")
	})
}

func TestCAT6Admission(t *testing.T) {
	issue := "https://github.com/Project-Helianthus/helianthus-docs-semantic/issues/68"
	cases := map[string]struct {
		entry  string
		mutate func(document)
	}{
		"plane with arm accepted_pack": {"plane physical", func(doc document) {
			doc["planes"].([]any)[0].(document)["admission"].(document)["arm"] = "accepted_pack"
		}},
		"core type with arm public_registry": {"core type groups", func(doc document) {
			doc["core_relation_types"].([]any)[1].(document)["admission"].(document)["arm"] = "public_registry"
		}},
		"pack entry with arm accepted_kernel": {"evse.ac.voltage", func(doc document) {
			doc["elements"].([]any)[indexOf(doc, "evse.ac.voltage")].(document)["admission"].(document)["arm"] = "accepted_kernel"
		}},
		"core kind not reached by a pack with arm accepted_pack": {"resource.equipment", func(doc document) {
			doc["elements"].([]any)[indexOf(doc, "resource.equipment")].(document)["admission"].(document)["arm"] = "accepted_pack"
		}},
		"kernel state with arm matter_baseline": {"kernel.ack_state", func(doc document) {
			doc["elements"].([]any)[indexOf(doc, "kernel.ack_state")].(document)["admission"].(document)["arm"] = "matter_baseline"
		}},
		"two_vendors without a research report": {"relation.relays", func(doc document) {
			doc["elements"].([]any)[indexOf(doc, "relation.relays")].(document)["admission"].(document)["arm"] = "two_vendors"
		}},
		"research report without two_vendors": {"relation.relays", func(doc document) {
			doc["elements"].([]any)[indexOf(doc, "relation.relays")].(document)["admission"].(document)["research_report"] = issue
		}},
		"owner_decision with evidence that is not an issue": {"relation.relays", func(doc document) {
			doc["elements"].([]any)[indexOf(doc, "relation.relays")].(document)["admission"].(document)["arm"] = "owner_decision"
		}},
		"evidence that is not pinned": {"evse.ac.voltage", func(doc document) {
			doc["elements"].([]any)[indexOf(doc, "evse.ac.voltage")].(document)["admission"].(document)["evidence"] = []any{
				"https://github.com/Project-Helianthus/helianthus-docs-semantic/blob/main/api/v1/packs/evse-v1.md",
			}
		}},
		"repeated evidence": {"evse.ac.voltage", func(doc document) {
			admission := doc["elements"].([]any)[indexOf(doc, "evse.ac.voltage")].(document)["admission"].(document)
			evidence := admission["evidence"].([]any)
			admission["evidence"] = append(evidence, evidence[0])
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			doc := decodeDocument(t)
			c.mutate(doc)
			requireFinding(t, findingsOf(t, doc), "CAT-6", "", semreg.InvalidValue, c.entry)
		})
	}
}

func indexOf(doc document, id string) int {
	for i, item := range elements(doc) {
		if item.(document)["ref"].(document)["id"] == id {
			return i
		}
	}
	return -1
}

func TestCAT7SignReference(t *testing.T) {
	t.Run("sign reference that is not a supplies relation", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.ac.active_power")["attributes"].(document)["sign_reference"] =
			elementRef(packOwner(evsePack), "evse.relation.asset_evse")
		requireFinding(t, findingsOf(t, doc), "CAT-7", "", semreg.InvalidValue, "evse.ac.active_power")
	})
	t.Run("sign reference without an entry", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "evse.ac.active_power")["attributes"].(document)["sign_reference"] =
			elementRef(packOwner(evsePack), "evse.relation.synthetic_supply")
		requireFinding(t, findingsOf(t, doc), "CAT-7", "", semreg.MembershipMissing, "evse.ac.active_power")
	})
	t.Run("supplies relation as sign reference", func(t *testing.T) {
		doc := decodeDocument(t)
		element(t, doc, "pv.ac.active_power")["attributes"].(document)["sign_reference"] =
			elementRef(packOwner(pvPack), "pv.relation.inverter_array")
		if findings := findingsOf(t, doc); len(findings) != 0 {
			t.Fatalf("supplies sign reference rejected:\n%s", joinFindings(findings))
		}
	})
	t.Run("native convention outside the accepted seed", func(t *testing.T) {
		doc := decodeDocument(t)
		entry := element(t, doc, "evse.ac.active_power")
		entry["admission"].(document)["arm"] = "matter_baseline"
		requireFinding(t, findingsOf(t, doc), "CAT-7", "", semreg.InvalidValue, "evse.ac.active_power")
	})
}

func TestCAT8CanonicalOrder(t *testing.T) {
	doc := decodeDocument(t)
	items := elements(doc)
	at := indexOf(doc, "evse.ac.voltage")
	items[at], items[at+1] = items[at+1], items[at]
	findings := findingsOf(t, doc)
	requireFinding(t, findings, "CAT-8", "", semreg.NoncanonicalOrder, "evse.ac.voltage")
	requireOnlyRule(t, findings, "CAT-8")
}

func TestCAT9IdentificationSchemes(t *testing.T) {
	scheme := func(arm string) document {
		return document{
			"id": "scheme.synthetic", "issuer_registry": "none", "token_form": "synthetic token",
			"admission": document{"arm": arm, "evidence": []any{"https://example.org/scheme"}},
		}
	}
	t.Run("listed scheme is accepted", func(t *testing.T) {
		doc := decodeDocument(t)
		doc["identification_schemes"] = []any{scheme("public_registry")}
		if findings := findingsOf(t, doc); len(findings) != 0 {
			t.Fatalf("scheme rejected:\n%s", joinFindings(findings))
		}
		catalog, err := graph.Decode(encode(t, doc))
		if err != nil {
			t.Fatal(err)
		}
		schemes := catalog.IdentificationSchemes()
		if len(schemes) != 1 || schemes[0].ID != "scheme.synthetic" || schemes[0].IssuerRegistry != "none" {
			t.Fatalf("IdentificationSchemes() = %+v", schemes)
		}
	})
	t.Run("duplicate scheme identifier", func(t *testing.T) {
		doc := decodeDocument(t)
		doc["identification_schemes"] = []any{scheme("public_registry"), scheme("public_registry")}
		requireFinding(t, findingsOf(t, doc), "CAT-9", "", semreg.DuplicateIdentity, "scheme.synthetic")
	})
	t.Run("scheme without a public registry admission", func(t *testing.T) {
		doc := decodeDocument(t)
		doc["identification_schemes"] = []any{scheme("matter_baseline")}
		requireFinding(t, findingsOf(t, doc), "CAT-9", "", semreg.InvalidValue, "scheme.synthetic")
	})
}

func TestDecodeIsStrict(t *testing.T) {
	raw := fixtureBytes(t)
	statusLine := []byte(`"status": "accepted",`)
	if !bytes.Contains(raw, statusLine) {
		t.Fatal("fixture layout changed: status line not found")
	}
	reject := func(name string, input []byte, want semreg.ErrorID) {
		t.Run(name, func(t *testing.T) {
			if _, err := graph.Decode(input); semreg.ErrorIdentifier(err) != want || err == nil {
				t.Fatalf("Decode = %v, want %s", err, want)
			}
		})
	}
	reject("repeated JSON key", bytes.Replace(raw, statusLine, append(append([]byte(nil), statusLine...), statusLine...), 1), semreg.DuplicateKey)
	reject("repeated JSON key in another case", bytes.Replace(raw, statusLine, []byte(`"status": "accepted", "Status": "accepted",`), 1), semreg.DuplicateKey)
	reject("member in another case", bytes.Replace(raw, statusLine, []byte(`"Status": "accepted",`), 1), semreg.UnknownMember)
	reject("unknown top-level member", bytes.Replace(raw, statusLine, []byte(`"status": "accepted", "planes_extra": [],`), 1), semreg.UnknownMember)
	reject("trailing data", append(append([]byte(nil), raw...), []byte(" {}")...), semreg.InvalidJSON)
	reject("invalid UTF-8", bytes.Replace(raw, []byte(`"accepted"`), []byte("\"accepted\xff\""), 1), semreg.InvalidJSON)
	reject("other contract", bytes.Replace(raw, []byte(`"helianthus.semantic.element-catalog/v1"`), []byte(`"helianthus.semantic.element-catalog/v2"`), 1), semreg.InvalidContract)

	mutated := func(name string, want semreg.ErrorID, mutate func(document)) {
		t.Run(name, func(t *testing.T) {
			doc := decodeDocument(t)
			mutate(doc)
			if _, err := graph.Decode(encode(t, doc)); semreg.ErrorIdentifier(err) != want || err == nil {
				t.Fatalf("Decode = %v, want %s", err, want)
			}
		})
	}
	mutated("unknown member of an element", semreg.UnknownMember, func(doc document) {
		element(t, doc, "evse.ac.voltage")["note"] = "synthetic"
	})
	mutated("attribute of another element kind", semreg.UnknownMember, func(doc document) {
		element(t, doc, "evse.ac.voltage")["attributes"].(document)["role"] = "resource"
	})
	mutated("unknown member of an element reference", semreg.UnknownMember, func(doc document) {
		element(t, doc, "evse.service.connector")["attributes"].(document)["fact_key_dimension"].(document)["pack"] = "synthetic"
	})
	mutated("missing member", semreg.MissingMember, func(doc document) { delete(doc, "lenses") })
	mutated("missing attribute", semreg.MissingMember, func(doc document) {
		delete(element(t, doc, "evse.relation.asset_evse")["attributes"].(document), "over_time")
	})
	mutated("missing admission", semreg.MissingMember, func(doc document) {
		delete(element(t, doc, "evse.ac.voltage"), "admission")
	})
	mutated("unknown element kind", semreg.InvalidEnum, func(doc document) {
		element(t, doc, "evse.ac.voltage")["element_kind"] = "measurement"
	})
	mutated("unknown plane", semreg.InvalidEnum, func(doc document) {
		element(t, doc, "evse.ac.voltage")["planes"] = []any{"spatial"}
	})
	mutated("repeated plane", semreg.InvalidValue, func(doc document) {
		element(t, doc, "evse.ac.voltage")["planes"] = []any{"energy", "energy"}
	})
	mutated("medium on a relation that is not supplies", semreg.InvalidValue, func(doc document) {
		element(t, doc, "evse.relation.asset_evse")["attributes"].(document)["medium"] = "electrical"
	})
	mutated("supplies relation without medium", semreg.InvalidValue, func(doc document) {
		delete(element(t, doc, "infrastructure.relation.feeder_circuit")["attributes"].(document), "medium")
	})
	mutated("signed fact without sign reference", semreg.InvalidValue, func(doc document) {
		delete(element(t, doc, "evse.ac.active_power")["attributes"].(document), "sign_reference")
	})
	mutated("sign reference on an unsigned fact", semreg.InvalidValue, func(doc document) {
		element(t, doc, "evse.ac.voltage")["attributes"].(document)["sign_reference"] = "native_convention"
	})
	mutated("direction on a fact that is not a quantity", semreg.InvalidValue, func(doc document) {
		element(t, doc, "evse.status.fault")["attributes"].(document)["direction"] = "non_negative"
	})
	mutated("resource kind on a qualifier dimension", semreg.InvalidValue, func(doc document) {
		attributes := element(t, doc, "evse.dimension.connector")["attributes"].(document)
		attributes["role"] = "qualifier"
	})
	mutated("identifier outside the syntax", semreg.InvalidIdentifier, func(doc document) {
		element(t, doc, "evse.ac.voltage")["ref"].(document)["id"] = "Evse.Voltage"
	})
	mutated("lens depth out of range", semreg.InvalidValue, func(doc document) {
		doc["lenses"] = []any{document{
			"id": "lens.synthetic", "question": "What does the synthetic lens show?", "planes": []any{"physical"},
			"root_kinds": []any{"resource.evse"}, "max_depth": 33,
		}}
	})
}

// The copied pack fixtures are the pack vectors the catalog is generated from.
// This test recomputes CAT-1 for relation definitions and Portal contributions,
// which no Go pack table declares, and CAT-2 for relation endpoints and
// cardinality.
func TestCatalogCoversThePackVectorRelationsAndPortalContributions(t *testing.T) {
	catalog := committedCatalog(t)
	type relationSpec struct {
		ref         semreg.DefinitionRef
		from, to    semreg.DefinitionRef
		cardinality string
	}
	var relations []relationSpec
	var portals []semreg.DefinitionRef
	for _, name := range []string{"pv-inverter-pack-vectors.json", "evse-pack-vectors.json", "infrastructure-pack-vectors.json", "thermal-hvac-pack-vectors.json"} {
		var vectors struct {
			Catalog struct {
				Pack        semreg.PackRef `json:"pack"`
				Definitions struct {
					FactDimensions []struct {
						ID  semreg.DefinitionID  `json:"id"`
						Ref semreg.DefinitionRef `json:"ref"`
					} `json:"fact_dimensions"`
					Relations []struct {
						Ref         semreg.DefinitionRef `json:"ref"`
						From        semreg.DefinitionID  `json:"from"`
						To          semreg.DefinitionID  `json:"to"`
						Cardinality string               `json:"cardinality"`
					} `json:"relations"`
					Portal []struct {
						Ref semreg.DefinitionRef `json:"ref"`
					} `json:"portal_contributions"`
				} `json:"definitions"`
			} `json:"catalog"`
		}
		readJSON(t, filepath.Join("../../../fixtures/v1", name), &vectors)
		pack := vectors.Catalog.Pack
		declared := map[semreg.DefinitionID]semreg.SemanticVersion{}
		for _, item := range vectors.Catalog.Definitions.FactDimensions {
			declared[item.ID] = item.Ref.Version
		}
		dimension := func(id semreg.DefinitionID) semreg.DefinitionRef {
			version, ok := declared[id]
			if !ok {
				version = pack.Version
			}
			return semreg.DefinitionRef{Pack: pack, ID: id, Version: version}
		}
		for _, item := range vectors.Catalog.Definitions.Relations {
			relations = append(relations, relationSpec{item.Ref, dimension(item.From), dimension(item.To), item.Cardinality})
		}
		for _, item := range vectors.Catalog.Definitions.Portal {
			portals = append(portals, item.Ref)
		}
	}
	var tables struct {
		Services []struct {
			ID        semreg.DefinitionID `json:"id"`
			Dimension semreg.DefinitionID `json:"dimension"`
		} `json:"services"`
		Relations []struct {
			ID          semreg.DefinitionID `json:"id"`
			From        semreg.DefinitionID `json:"from"`
			To          semreg.DefinitionID `json:"to"`
			Cardinality string              `json:"cardinality"`
		} `json:"relations"`
		Portal []struct {
			ID semreg.DefinitionID `json:"id"`
		} `json:"portal_contributions"`
	}
	readJSON(t, "../../../fixtures/v1/storage-bms-contract-tables.json", &tables)
	storageRef := func(id semreg.DefinitionID) semreg.DefinitionRef {
		return semreg.DefinitionRef{Pack: storagePack, ID: id, Version: storagePack.Version}
	}
	serviceDimension := map[semreg.DefinitionID]semreg.DefinitionRef{}
	for _, item := range tables.Services {
		serviceDimension[item.ID] = storageRef(item.Dimension)
	}
	for _, item := range tables.Relations {
		relations = append(relations, relationSpec{storageRef(item.ID), serviceDimension[item.From], serviceDimension[item.To], item.Cardinality})
	}
	for _, item := range tables.Portal {
		portals = append(portals, storageRef(item.ID))
	}

	if len(relations) != 21 || len(portals) != 32 {
		t.Fatalf("pack vectors declare %d relations and %d Portal contributions, want 21 and 32", len(relations), len(portals))
	}
	declared := map[semreg.DefinitionRef]bool{}
	for _, spec := range relations {
		declared[spec.ref] = true
		entry, ok := catalog.PackEntry(spec.ref)
		if !ok || entry.ElementKind != graph.KindRelation || entry.Relation == nil || entry.Relation.EndpointDimensions == nil {
			t.Errorf("relation %s has no relation entry", spec.ref.ID)
			continue
		}
		ends := entry.Relation.EndpointDimensions
		if ends.From != graph.PackElement(spec.from) || ends.To != graph.PackElement(spec.to) {
			t.Errorf("relation %s endpoints %+v, pack declares %s -> %s", spec.ref.ID, ends, spec.from.ID, spec.to.ID)
		}
		cardinality := strings.TrimSuffix(spec.cardinality, "_over_time")
		overTime := cardinality != spec.cardinality
		if string(entry.Relation.Cardinality) != cardinality || entry.Relation.OverTime != overTime {
			t.Errorf("relation %s cardinality %s/%v, pack declares %s", spec.ref.ID, entry.Relation.Cardinality, entry.Relation.OverTime, spec.cardinality)
		}
	}
	for _, ref := range portals {
		declared[ref] = true
		entry, ok := catalog.PackEntry(ref)
		if !ok || entry.ElementKind != graph.KindPresentation || entry.Disposition != graph.DispositionUnsupported {
			t.Errorf("Portal contribution %s has no unsupported presentation entry", ref.ID)
		}
	}
	for _, entry := range catalog.Elements() {
		if entry.Ref.Owner.Kind != graph.OwnerPack {
			continue
		}
		if entry.ElementKind == graph.KindRelation || entry.ElementKind == graph.KindPresentation {
			ref, _ := entry.Ref.DefinitionRef()
			if !declared[ref] {
				t.Errorf("pack %s entry %s is not declared by the pack vectors", entry.ElementKind, entry.Ref.ID)
			}
		}
	}
}

func readJSON(t *testing.T, path string, out any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
