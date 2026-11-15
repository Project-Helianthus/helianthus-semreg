package graph_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// ------------------------------------------------------------ test catalog

// The cut tests run against a small synthetic catalog decoded from the
// committed document's planes and core relation types, so every endpoint
// kind, plane and core type a vector needs is declared once below.

var cutPack = semreg.PackRef{ID: "pack.cut", Version: "1.0.0"}

func cutRef(id string) semreg.DefinitionRef {
	return semreg.DefinitionRef{Pack: cutPack, ID: semreg.DefinitionID(id), Version: "1.0.0"}
}

const (
	kindDevice  = "resource.cut_device"
	kindPart    = "resource.cut_part"
	kindZone    = "resource.cut_zone"
	kindPending = "resource.cut_pending"

	factPower = "cut.fact.device_power"
	factFault = "cut.fact.device_fault"
	factLabel = "cut.fact.label_text"

	dimensionDevice = "cut.dimension.device"
	dimensionLabel  = "cut.dimension.label"

	cutScheme = "scheme.cut_serial"
)

var (
	relDevicePart    = cutRef("cut.relation.device_part")
	relDevicePartAlt = cutRef("cut.relation.device_part_alt")
	relRelays        = cutRef("cut.relation.device_relays")
	relSupplies      = cutRef("cut.relation.device_supplies")
	relControls      = cutRef("cut.relation.device_zone")
	relFeeds         = cutRef("cut.relation.device_feeds")
	serviceDevice    = cutRef("cut.service.device")
	capabilityDevice = cutRef("cut.capability.device")
)

func cutCatalog(t *testing.T) *graph.Catalog {
	t.Helper()
	doc := decodeDocument(t)
	core := document{"kind": "core", "contract": string(graph.KernelContract)}
	pack := packOwner(cutPack)
	planes := func(ids ...string) []any {
		out := []any{}
		for _, id := range ids {
			out = append(out, id)
		}
		return out
	}
	entry := func(owner document, id, kind, disposition string, planeList []any, attributes document) document {
		e := document{
			"ref": elementRef(owner, id), "element_kind": kind, "disposition": disposition, "planes": planeList,
			"admission":  document{"arm": "accepted_pack", "evidence": []any{"https://github.com/Project-Helianthus/helianthus-docs-semantic/issues/67"}},
			"attributes": attributes,
		}
		if disposition == "unknown" {
			e["reason"] = "reason.cut_pending"
			e["tracking"] = "https://github.com/Project-Helianthus/helianthus-semreg/issues/90"
		}
		return e
	}
	kind := func(id, class, disposition string, planeList []any) document {
		return entry(core, id, "resource_kind", disposition, planeList, document{"anchor_class": class})
	}
	relation := func(ref semreg.DefinitionRef, coreType, from, to, cardinality, medium string, planeList []any) document {
		attributes := document{
			"core_type": coreType, "orientation": "as_core", "from_kinds": planes(from), "to_kinds": planes(to),
			"cardinality": cardinality, "over_time": false, "basis_classes": planes("configuration"),
		}
		if medium != "" {
			attributes["medium"] = medium
		}
		return entry(pack, string(ref.ID), "relation", "member", planeList, attributes)
	}
	deviceDimension := elementRef(pack, dimensionDevice)
	labelDimension := elementRef(pack, dimensionLabel)
	resolved := document{"status": "resolved", "dimension": deviceDimension, "kind": kindDevice}
	fact := func(id, disposition string, planeList []any, subject document) document {
		return entry(pack, id, "fact", disposition, planeList, document{
			"value_kind": "boolean", "subject": subject, "aggregation": "not_applicable", "direction": "not_applicable",
		})
	}
	devicePlanes := planes("physical", "energy", "control", "provenance")
	doc["identification_schemes"] = []any{}
	doc["lenses"] = []any{}
	doc["elements"] = []any{
		kind(kindDevice, "equipment", "member", devicePlanes),
		kind(kindPart, "component", "member", planes("physical", "energy")),
		kind(kindZone, "function", "member", planes("control")),
		// An unknown kind is in no plane even where an entry lists one.
		kind(kindPending, "equipment", "unknown", planes("physical")),
		relation(relDevicePart, "contains", kindDevice, kindPart, "one_to_many", "", planes("physical", "energy")),
		relation(relDevicePartAlt, "contains", kindDevice, kindPart, "one_to_many", "", planes("physical")),
		relation(relRelays, "relays", kindDevice, kindDevice, "many_to_many", "", planes("provenance")),
		relation(relSupplies, "supplies", kindDevice, kindDevice, "many_to_many", "electrical", planes("energy")),
		relation(relControls, "controls", kindDevice, kindZone, "one_to_one", "", planes("control")),
		relation(relFeeds, "supplies", kindDevice, kindDevice, "one_to_many", "electrical", planes("energy")),
		entry(pack, dimensionDevice, "dimension", "member", devicePlanes, document{"role": "resource", "resource_kind": kindDevice}),
		entry(pack, dimensionLabel, "dimension", "unknown", planes(), document{"role": "qualifier"}),
		fact(factPower, "member", planes("energy"), resolved),
		fact(factFault, "member", planes("diagnostic"), resolved),
		fact(factLabel, "unknown", planes(), document{"status": "unresolved", "qualifier_dimension": labelDimension}),
		entry(pack, string(serviceDevice.ID), "service", "member", planes("energy"), document{"fact_key_dimension": deviceDimension, "subject_kind": kindDevice}),
		entry(pack, string(capabilityDevice.ID), "capability", "member", planes("control"), document{"service": elementRef(pack, string(serviceDevice.ID))}),
	}
	sortElements(doc)
	catalog, err := graph.Decode(encode(t, doc))
	if err != nil {
		t.Fatalf("Decode(cut test catalog) = %v", err)
	}
	return catalog
}

// ---------------------------------------------------------- record builders

func cutEvidence(label string) semreg.EvidenceRef {
	sum := sha256.Sum256([]byte("cut evidence " + label))
	return semreg.EvidenceRef{
		Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Contract: "test.contract/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone,
	}
}

func operatorOrigin(label string) semreg.OriginRef {
	return semreg.OriginRef{OriginID: semreg.OriginID("origin:operator:" + label), Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{cutEvidence("operator")}}
}

func sourceOf(asset semreg.AssetID) (semreg.SourceID, semreg.SourceEpochID, semreg.NativeBindingID) {
	return semreg.SourceID("source:" + asset), semreg.SourceEpochID("epoch:" + asset), semreg.NativeBindingID("binding:" + asset)
}

func nativeOrigin(asset semreg.AssetID, label string) semreg.OriginRef {
	source, epoch, binding := sourceOf(asset)
	return semreg.OriginRef{OriginID: semreg.OriginID("origin:native:" + label), Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []semreg.EvidenceRef{cutEvidence("native")}}
}

func text(value string) *string { return &value }

func resourceID(id string) *semreg.ResourceID {
	value := semreg.ResourceID(id)
	return &value
}

func slotResource(id, kind string, class semreg.AnchorClass, parent, slot string) semreg.Resource {
	anchor := semreg.ResourceAnchor{Class: class, Basis: semreg.AnchorOperatorSlot, Slot: text(slot), Evidence: []semreg.EvidenceRef{cutEvidence("anchor")}}
	if parent != "" {
		anchor.Parent = resourceID(parent)
	}
	return semreg.Resource{ResourceID: semreg.ResourceID(id), Kind: semreg.DefinitionID(kind), Anchor: anchor, State: semreg.ResourceCurrent, Origin: operatorOrigin(id), Revision: "1"}
}

// device is equipment anchored on an operator slot.
func device(id, slot string) semreg.Resource {
	return slotResource(id, kindDevice, semreg.AnchorEquipment, "", slot)
}

// part is a component of its anchor parent.
func part(id, parent, slot string) semreg.Resource {
	return slotResource(id, kindPart, semreg.AnchorComponent, parent, slot)
}

// zone is a root function of the installation.
func zone(id, slot string) semreg.Resource {
	return slotResource(id, kindZone, semreg.AnchorFunction, "", slot)
}

// identityKey is the anchor key of a native token: sha256 over the JCS bytes
// of the identity object {scheme, issuer, token}.
func identityKey(t *testing.T, scheme, issuer, token string) semreg.Digest {
	t.Helper()
	canonical, err := semreg.CanonicalJSON(jsonObject{"scheme": scheme, "issuer": issuer, "token": token})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func nativeDevice(asset semreg.AssetID, id string, key semreg.Digest) semreg.Resource {
	scheme := cutScheme
	return semreg.Resource{
		ResourceID: semreg.ResourceID(id), Kind: kindDevice,
		Anchor: semreg.ResourceAnchor{Class: semreg.AnchorEquipment, Basis: semreg.AnchorNativeIdentity, Scheme: &scheme, Key: &key, Evidence: []semreg.EvidenceRef{cutEvidence("anchor")}},
		State:  semreg.ResourceCurrent, Origin: nativeOrigin(asset, id), Revision: "1",
	}
}

func withState(resource semreg.Resource, state semreg.ResourceState) semreg.Resource {
	resource.State = state
	return resource
}

func relation(definition semreg.DefinitionRef, from, to string, state semreg.LinkState) semreg.RelationInstance {
	return semreg.RelationInstance{
		Definition: definition, From: semreg.ResourceID(from), To: semreg.ResourceID(to), State: state,
		BasisClass: semreg.RelationConfiguration, Basis: []semreg.EvidenceRef{cutEvidence("relation")},
		Origin: operatorOrigin(from + "-" + to), Revision: "1",
	}
}

func qualified(definition semreg.DefinitionRef, from, to string) semreg.RelationInstance {
	return relation(definition, from, to, semreg.LinkQualified)
}

func absence(definition semreg.DefinitionRef, resource string, side semreg.AbsenceSide) semreg.RelationAbsence {
	return semreg.RelationAbsence{
		Definition: definition, Resource: semreg.ResourceID(resource), Side: side, State: semreg.LinkQualified,
		BasisClass: semreg.RelationConfiguration, Basis: []semreg.EvidenceRef{cutEvidence("absence")},
		Origin: operatorOrigin("absence-" + resource), Revision: "1",
	}
}

func factKey(fact, dimension, value string) semreg.FactKey {
	return semreg.FactKey{
		PackID: cutPack.ID, PackVersion: cutPack.Version, FactID: semreg.DefinitionID(fact),
		Dimensions: []semreg.Dimension{{ID: semreg.DefinitionID(dimension), Value: semreg.Value{Kind: semreg.ValueText, Text: text(value)}}},
	}
}

func deviceFact(fact, subject string) semreg.FactKey { return factKey(fact, dimensionDevice, subject) }

func labelFact(label string) semreg.FactKey { return factKey(factLabel, dimensionLabel, label) }

// cutAsset is the published content of one member: the kernel records of
// one asset with a single current source, binding and generation.
type cutAsset struct {
	id        semreg.AssetID
	protocol  semreg.DefinitionID
	resources []semreg.Resource
	relations []semreg.RelationInstance
	absences  []semreg.RelationAbsence
	facts     []semreg.FactKey
	// services publishes one service and one capability about each named
	// resource; withdrawn publishes both as withdrawn tombstones.
	services  []string
	withdrawn bool
}

type jsonObject map[string]any

func (jsonObject) Validate() error { return nil }

func canonicalDefinition(t *testing.T, ref semreg.DefinitionRef) string {
	t.Helper()
	canonical, err := semreg.CanonicalJSON(ref)
	if err != nil {
		t.Fatal(err)
	}
	return string(canonical)
}

func (a cutAsset) snapshot(t *testing.T) semreg.Snapshot {
	t.Helper()
	source, epoch, binding := sourceOf(a.id)
	generation := semreg.Uint64("1")
	protocol := a.protocol
	if protocol == "" {
		protocol = "protocol.test"
	}
	received := semreg.TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"}
	evaluated := semreg.TimePoint{UnixNanoseconds: "101", ClockID: "clock.utc", UncertaintyNS: "0"}
	s := semreg.Snapshot{
		Contract: semreg.ContractKernelV1, AssetID: a.id,
		Revisions:         semreg.RevisionVector{Semantic: "1", Identity: "1", Facts: "1", Services: "1", Capabilities: "1"},
		EvaluatedAt:       evaluated,
		EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:cut", Nanoseconds: "6"},
		Sources: []semreg.SourceDescriptor{{
			SourceID: source, SourceEpochID: epoch, ProtocolID: protocol, ProfileID: "profile.test", ProfileVersion: "release-1",
			RegistryEvidence: cutEvidence("registry"), StartedAt: received, State: semreg.SourceCurrent, Revision: "1",
		}},
		Bindings:      []semreg.NativeBinding{{BindingID: binding, AssetID: a.id, SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, NativeResource: cutEvidence("native resource"), State: semreg.BindingCurrent, Revision: "1"}},
		IdentityLinks: []semreg.IdentityLink{},
		Facts:         []semreg.FactEnvelope{},
		Services:      []semreg.ServiceInstance{},
		Capabilities:  []semreg.CapabilityInstance{},
		Fences:        []semreg.GenerationFence{},
		Cursors:       []semreg.PublicationCursor{{SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, LastSequence: "1", LastBatchDigest: cutEvidence("batch").Digest}},
	}
	for i, key := range a.facts {
		candidate := semreg.FactCandidate{
			CandidateID: semreg.CandidateID(fmt.Sprintf("candidate:%s:%02d", a.id, i)), Key: key, Value: &semreg.Value{Kind: semreg.ValueBoolean, Boolean: new(bool)},
			Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}},
			Times: semreg.Times{
				ReceivedAt: received, ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:cut", Nanoseconds: "5"},
				EvaluatedAt: evaluated, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:cut", Nanoseconds: "6"},
			},
			FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:cut", Version: "1.0.0", FreshForNS: "30", RetainForNS: "300", MaxWallUncertaintyNS: "1"},
			BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
			Origin: nativeOrigin(a.id, fmt.Sprintf("fact-%02d", i)), Evidence: []semreg.EvidenceRef{cutEvidence("fact")}, Revision: "1",
		}
		s.Facts = append(s.Facts, semreg.FactEnvelope{AssetID: a.id, Key: key, Candidates: []semreg.FactCandidate{candidate}, Conflicts: []semreg.Conflict{}, Revision: "1"})
	}
	sort.Slice(s.Facts, func(i, j int) bool {
		left, _ := semreg.CanonicalJSON(s.Facts[i].Key)
		right, _ := semreg.CanonicalJSON(s.Facts[j].Key)
		return string(left) < string(right)
	})
	availability := semreg.AvailabilityAvailable
	if a.withdrawn {
		availability = semreg.AvailabilityWithdrawn
	}
	for _, subject := range a.services {
		service := semreg.ServiceInstanceID("service:" + subject)
		s.Services = append(s.Services, semreg.ServiceInstance{
			InstanceID: service, AssetID: a.id, Definition: serviceDevice, BindingID: binding, SourceEpochID: epoch, DriverGeneration: generation,
			Qualification: semreg.QualificationQualified, Availability: availability, Revision: "1", Resource: resourceID(subject),
		})
		s.Capabilities = append(s.Capabilities, semreg.CapabilityInstance{
			InstanceID: semreg.CapabilityInstanceID("capability:" + subject), AssetID: a.id, ServiceInstance: service, Definition: capabilityDevice,
			BindingID: binding, SourceEpochID: epoch, DriverGeneration: generation, Qualification: semreg.QualificationQualified,
			Availability: availability, Constraints: []semreg.TypedField{}, ActivationEvidence: []semreg.EvidenceRef{cutEvidence("activation")}, Revision: "1",
		})
	}
	if len(a.resources)+len(a.relations)+len(a.absences) != 0 {
		s.Revisions.Structure = "1"
	}
	s.Resources = append([]semreg.Resource(nil), a.resources...)
	sort.Slice(s.Resources, func(i, j int) bool { return s.Resources[i].ResourceID < s.Resources[j].ResourceID })
	s.Relations = append([]semreg.RelationInstance(nil), a.relations...)
	sort.Slice(s.Relations, func(i, j int) bool {
		left, right := s.Relations[i], s.Relations[j]
		return canonicalDefinition(t, left.Definition)+"\x00"+string(left.From)+"\x00"+string(left.To) < canonicalDefinition(t, right.Definition)+"\x00"+string(right.From)+"\x00"+string(right.To)
	})
	s.RelationAbsences = append([]semreg.RelationAbsence(nil), a.absences...)
	sort.Slice(s.RelationAbsences, func(i, j int) bool {
		left, right := s.RelationAbsences[i], s.RelationAbsences[j]
		return canonicalDefinition(t, left.Definition)+"\x00"+string(left.Resource)+"\x00"+string(left.Side) < canonicalDefinition(t, right.Definition)+"\x00"+string(right.Resource)+"\x00"+string(right.Side)
	})
	if len(s.Resources) == 0 {
		s.Resources = nil
	}
	if len(s.Relations) == 0 {
		s.Relations = nil
	}
	if len(s.RelationAbsences) == 0 {
		s.RelationAbsences = nil
	}
	sealSnapshot(t, &s)
	if err := s.Validate(); err != nil {
		t.Fatalf("hand-built snapshot of %s is invalid: %v", a.id, err)
	}
	return s
}

// sealSnapshot sets the content-derived identifier: sha256 over the
// canonical JSON of the snapshot without snapshot_id.
func sealSnapshot(t *testing.T, s *semreg.Snapshot) {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object jsonObject
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	delete(object, "snapshot_id")
	canonical, err := semreg.CanonicalJSON(object)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	s.SnapshotID = semreg.SnapshotID("sha256:" + hex.EncodeToString(sum[:]))
}

func snapshots(t *testing.T, assets ...cutAsset) []semreg.Snapshot {
	t.Helper()
	out := make([]semreg.Snapshot, len(assets))
	for i, asset := range assets {
		out[i] = asset.snapshot(t)
	}
	return out
}

func assemble(t *testing.T, completeness graph.Completeness, assets ...cutAsset) *graph.Cut {
	t.Helper()
	cut, err := graph.AssembleCut(snapshots(t, assets...), cutCatalog(t), completeness)
	if err != nil {
		t.Fatalf("AssembleCut = %v", err)
	}
	return cut
}

func relationKey(definition semreg.DefinitionRef, from, to string) semreg.RelationKey {
	return semreg.RelationKey{Definition: definition, From: semreg.ResourceID(from), To: semreg.ResourceID(to)}
}

func requireRelation(t *testing.T, cut *graph.Cut, key semreg.RelationKey, state semreg.LinkState, coherence graph.RelationCoherence) {
	t.Helper()
	got, ok := cut.Relation(key)
	if !ok {
		t.Fatalf("relation %s %s -> %s is not in the cut", key.Definition.ID, key.From, key.To)
	}
	if got.State != state || got.Coherence != coherence {
		t.Fatalf("relation %s %s -> %s = (%s, %s), want (%s, %s)", key.Definition.ID, key.From, key.To, got.State, got.Coherence, state, coherence)
	}
}

func findingsOfKind(cut *graph.Cut, kind graph.CutFindingKind) []graph.CutFinding {
	var out []graph.CutFinding
	for _, finding := range cut.Findings() {
		if finding.Kind == kind {
			out = append(out, finding)
		}
	}
	return out
}

func ref(asset, resource string) graph.CutResourceRef {
	return graph.CutResourceRef{AssetID: semreg.AssetID(asset), ResourceID: semreg.ResourceID(resource)}
}

func requireFindings(t *testing.T, cut *graph.Cut, kind graph.CutFindingKind, want ...graph.CutFinding) {
	t.Helper()
	got := findingsOfKind(cut, kind)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s findings = %+v, want %+v", kind, got, want)
	}
}

// ------------------------------------------------------------ cut identity

func handCutID(members ...[2]string) semreg.Digest {
	var b strings.Builder
	b.WriteString(`{"contract":"helianthus.semantic.graph-cut/v1","members":[`)
	for i, member := range members {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"asset_id":%q,"snapshot_id":%q}`, member[0], member[1])
	}
	b.WriteString("]}")
	sum := sha256.Sum256([]byte(b.String()))
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// The identifier covers the contract and every member's asset_id and
// snapshot_id, sorted by asset; it covers neither input order nor the
// asserted completeness. Each member field is varied once.
func TestAssembleCutIdentifierIsTheSortedMemberVector(t *testing.T) {
	catalog := cutCatalog(t)
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:a1", "a1")}}
	b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{device("resource:b1", "b1")}}
	sa, sb := a.snapshot(t), b.snapshot(t)
	cut, err := graph.AssembleCut([]semreg.Snapshot{sb, sa}, catalog, graph.CutComplete)
	if err != nil {
		t.Fatal(err)
	}
	want := handCutID([2]string{string(sa.AssetID), string(sa.SnapshotID)}, [2]string{string(sb.AssetID), string(sb.SnapshotID)})
	if cut.ID() != want {
		t.Fatalf("ID = %s, want %s", cut.ID(), want)
	}
	if got := cut.Members(); !reflect.DeepEqual(got, []graph.CutMember{{AssetID: sa.AssetID, SnapshotID: sa.SnapshotID}, {AssetID: sb.AssetID, SnapshotID: sb.SnapshotID}}) {
		t.Fatalf("Members = %+v", got)
	}
	if cut.Completeness() != graph.CutComplete {
		t.Fatalf("Completeness = %s", cut.Completeness())
	}
	partial, err := graph.AssembleCut([]semreg.Snapshot{sa, sb}, catalog, graph.CutPartial)
	if err != nil {
		t.Fatal(err)
	}
	if partial.ID() != want || partial.Completeness() != graph.CutPartial {
		t.Fatalf("partial cut = (%s, %s), want (%s, partial)", partial.ID(), partial.Completeness(), want)
	}

	// asset_id: the same content under another asset is another member.
	renamed := b
	renamed.id = "asset:cut-c"
	sc := renamed.snapshot(t)
	other, err := graph.AssembleCut([]semreg.Snapshot{sa, sc}, catalog, graph.CutComplete)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID() == want || other.ID() != handCutID([2]string{string(sa.AssetID), string(sa.SnapshotID)}, [2]string{string(sc.AssetID), string(sc.SnapshotID)}) {
		t.Fatalf("renamed asset ID = %s", other.ID())
	}

	// snapshot_id: another snapshot of the same asset is another revision.
	changed := b
	changed.resources = []semreg.Resource{device("resource:b1", "b1"), device("resource:b2", "b2")}
	sd := changed.snapshot(t)
	if sd.SnapshotID == sb.SnapshotID {
		t.Fatal("changed content kept its snapshot identifier")
	}
	revised, err := graph.AssembleCut([]semreg.Snapshot{sa, sd}, catalog, graph.CutComplete)
	if err != nil {
		t.Fatal(err)
	}
	if revised.ID() == want || revised.ID() != handCutID([2]string{string(sa.AssetID), string(sa.SnapshotID)}, [2]string{string(sd.AssetID), string(sd.SnapshotID)}) {
		t.Fatalf("revised snapshot ID = %s", revised.ID())
	}

	empty, err := graph.AssembleCut(nil, catalog, graph.CutPartial)
	if err != nil {
		t.Fatal(err)
	}
	if empty.ID() != handCutID() || len(empty.Members()) != 0 {
		t.Fatalf("empty cut = (%s, %+v)", empty.ID(), empty.Members())
	}
}

// ------------------------------------------------------ section 19 vectors

// G-POS-CUT-1: one ResourceID current in two members, or retired in one and
// current in another, excludes both members and makes the cut partial.
func TestAssembleCutDuplicateIdentityExcludesBothMembers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state semreg.ResourceState
	}{{"current in both", semreg.ResourceCurrent}, {"retired in one", semreg.ResourceRetired}} {
		t.Run(tc.name, func(t *testing.T) {
			a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:x", "x"), part("resource:a-part", "resource:x", "1")}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:x", "resource:a-part")}}
			b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{withState(device("resource:x", "x"), tc.state)}}
			c := cutAsset{id: "asset:cut-c", resources: []semreg.Resource{device("resource:c1", "c1")}}
			cut := assemble(t, graph.CutComplete, a, b, c)
			requireFindings(t, cut, graph.CutDuplicateIdentity, graph.CutFinding{Kind: graph.CutDuplicateIdentity, Resources: []graph.CutResourceRef{ref("asset:cut-a", "resource:x"), ref("asset:cut-b", "resource:x")}})
			if got := cut.Excluded(); !reflect.DeepEqual(got, []semreg.AssetID{"asset:cut-a", "asset:cut-b"}) {
				t.Fatalf("Excluded = %v", got)
			}
			if cut.Completeness() != graph.CutPartial {
				t.Fatalf("Completeness = %s, want partial", cut.Completeness())
			}
			if len(cut.Members()) != 3 {
				t.Fatalf("Members = %+v; an excluded member stays in the read vector", cut.Members())
			}
			if _, ok := cut.Relation(relationKey(relDevicePart, "resource:x", "resource:a-part")); ok {
				t.Fatal("a relation of an excluded member is in the cut")
			}
			if planes := cut.PlanesOfResource("resource:x"); len(planes) != 0 {
				t.Fatalf("PlanesOfResource(excluded) = %v", planes)
			}
			if planes := cut.PlanesOfResource("resource:c1"); !reflect.DeepEqual(planes, []graph.PlaneID{graph.PlanePhysical}) {
				t.Fatalf("PlanesOfResource(kept member) = %v", planes)
			}
		})
	}
	t.Run("withdrawn in one is no duplicate", func(t *testing.T) {
		a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:x", "x")}}
		b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{withState(device("resource:x", "x"), semreg.ResourceWithdrawn)}}
		cut := assemble(t, graph.CutComplete, a, b)
		requireFindings(t, cut, graph.CutDuplicateIdentity)
		if len(cut.Excluded()) != 0 || cut.Completeness() != graph.CutComplete {
			t.Fatalf("Excluded = %v, Completeness = %s", cut.Excluded(), cut.Completeness())
		}
	})
}

// G-POS-CUT-2: a far end of a kind the definition does not admit is conflict.
func TestAssembleCutFarEndOfNonAdmittedKindIsConflict(t *testing.T) {
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d")}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:z")}}
	b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{zone("resource:z", "z")}}
	cut := assemble(t, graph.CutComplete, a, b)
	key := relationKey(relDevicePart, "resource:d", "resource:z")
	requireRelation(t, cut, key, semreg.LinkConflict, graph.CoherenceConflict)
	if planes := cut.PlanesOfRelation(key); len(planes) != 0 {
		t.Fatalf("PlanesOfRelation(conflict) = %v", planes)
	}
}

// G-POS-CUT-3: a far end withdrawn or retired in its home is stale and in
// no plane.
func TestAssembleCutFarEndRetiredInItsHomeIsStale(t *testing.T) {
	for _, state := range []semreg.ResourceState{semreg.ResourceRetired, semreg.ResourceWithdrawn} {
		t.Run(string(state), func(t *testing.T) {
			a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d")}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:p")}}
			b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{withState(part("resource:p", "resource:d", "1"), state)}}
			cut := assemble(t, graph.CutComplete, a, b)
			key := relationKey(relDevicePart, "resource:d", "resource:p")
			requireRelation(t, cut, key, semreg.LinkQualified, graph.CoherenceStale)
			if planes := cut.PlanesOfRelation(key); len(planes) != 0 {
				t.Fatalf("PlanesOfRelation(stale) = %v", planes)
			}
		})
	}
}

// G-POS-CUT-4: two current resources of one kind with one anchor key in two
// members are flagged same_unit, once by native key and once by parent and
// slot; both stay in the cut.
func TestAssembleCutSameUnitByNativeKeyAndBySlot(t *testing.T) {
	t.Run("native key", func(t *testing.T) {
		key := identityKey(t, cutScheme, "issuer.one", "unit-0001")
		a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{nativeDevice("asset:cut-a", "resource:a1", key)}}
		b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{nativeDevice("asset:cut-b", "resource:b1", key)}}
		cut := assemble(t, graph.CutComplete, a, b)
		requireFindings(t, cut, graph.CutSameUnit, graph.CutFinding{Kind: graph.CutSameUnit, Resources: []graph.CutResourceRef{ref("asset:cut-a", "resource:a1"), ref("asset:cut-b", "resource:b1")}})
		if len(cut.Excluded()) != 0 {
			t.Fatalf("Excluded = %v; same_unit keeps both", cut.Excluded())
		}
	})
	t.Run("parent and slot", func(t *testing.T) {
		a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d"), part("resource:a-part", "resource:d", "1")}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:a-part")}}
		b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{part("resource:b-part", "resource:d", "1")}}
		cut := assemble(t, graph.CutComplete, a, b)
		requireFindings(t, cut, graph.CutSameUnit, graph.CutFinding{Kind: graph.CutSameUnit, Resources: []graph.CutResourceRef{ref("asset:cut-a", "resource:a-part"), ref("asset:cut-b", "resource:b-part")}})
	})
	t.Run("operator slot of equipment", func(t *testing.T) {
		a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:a1", "declared-unit")}}
		b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{device("resource:b1", "declared-unit")}}
		cut := assemble(t, graph.CutComplete, a, b)
		requireFindings(t, cut, graph.CutSameUnit, graph.CutFinding{Kind: graph.CutSameUnit, Resources: []graph.CutResourceRef{ref("asset:cut-a", "resource:a1"), ref("asset:cut-b", "resource:b1")}})
	})
	// Each member of the anchor key is varied: kind, parent, slot, the
	// native key, and the current state the rule requires.
	for _, tc := range []struct {
		name string
		b    semreg.Resource
	}{
		{"other kind", slotResource("resource:b1", kindZone, semreg.AnchorFunction, "", "declared-unit")},
		{"other slot", device("resource:b1", "other-unit")},
		{"other parent", slotResource("resource:b1", kindDevice, semreg.AnchorEquipment, "resource:elsewhere", "declared-unit")},
		{"withdrawn", withState(device("resource:b1", "declared-unit"), semreg.ResourceWithdrawn)},
	} {
		t.Run("no finding for "+tc.name, func(t *testing.T) {
			a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:a1", "declared-unit")}}
			b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{tc.b}}
			requireFindings(t, assemble(t, graph.CutComplete, a, b), graph.CutSameUnit)
		})
	}
	t.Run("no finding for another native key", func(t *testing.T) {
		a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{nativeDevice("asset:cut-a", "resource:a1", identityKey(t, cutScheme, "issuer.one", "unit-0001"))}}
		b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{nativeDevice("asset:cut-b", "resource:b1", identityKey(t, cutScheme, "issuer.one", "unit-0002"))}}
		requireFindings(t, assemble(t, graph.CutComplete, a, b), graph.CutSameUnit)
	})
}

// G-POS-CUT-5: a member with facts of member definitions and no graph record
// is ungraphed and contributes to no plane.
func TestAssembleCutUngraphedMember(t *testing.T) {
	power := deviceFact(factPower, "inverter-1")
	u := cutAsset{id: "asset:cut-u", facts: []semreg.FactKey{power, labelFact("text")}}
	g := cutAsset{id: "asset:cut-g", resources: []semreg.Resource{device("resource:g", "g")}, facts: []semreg.FactKey{deviceFact(factPower, "resource:g")}}
	cut := assemble(t, graph.CutComplete, u, g)
	requireFindings(t, cut, graph.CutUngraphed, graph.CutFinding{Kind: graph.CutUngraphed, Assets: []semreg.AssetID{"asset:cut-u"}})
	if planes := cut.PlanesOfFact("asset:cut-u", power); len(planes) != 0 {
		t.Fatalf("PlanesOfFact(ungraphed) = %v", planes)
	}
	if len(cut.Excluded()) != 0 {
		t.Fatalf("Excluded = %v; an ungraphed member is not excluded", cut.Excluded())
	}
}

// G-POS-CUT-6: with a complete cut, a far end in no member is dangling; with
// a partial cut it is outside_cut.
func TestAssembleCutFarEndInNoMember(t *testing.T) {
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d")}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:p")}}
	key := relationKey(relDevicePart, "resource:d", "resource:p")
	complete := assemble(t, graph.CutComplete, a)
	requireRelation(t, complete, key, semreg.LinkQualified, graph.CoherenceDangling)
	partial := assemble(t, graph.CutPartial, a)
	requireRelation(t, partial, key, semreg.LinkQualified, graph.CoherenceOutsideCut)
	for _, cut := range []*graph.Cut{complete, partial} {
		if planes := cut.PlanesOfRelation(key); len(planes) != 0 {
			t.Fatalf("PlanesOfRelation(unresolved) = %v", planes)
		}
	}
}

// G-POS-CUT-7: member B's absence statement denies a qualified instance held
// by member A: the instance becomes conflict and the statement is reported.
func TestAssembleCutContradictedAbsence(t *testing.T) {
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d")}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:p")}}
	b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{part("resource:p", "resource:d", "1")}, absences: []semreg.RelationAbsence{absence(relDevicePart, "resource:p", semreg.AbsenceTo)}}
	cut := assemble(t, graph.CutComplete, a, b)
	key := relationKey(relDevicePart, "resource:d", "resource:p")
	requireRelation(t, cut, key, semreg.LinkConflict, graph.CoherenceConflict)
	requireFindings(t, cut, graph.CutContradictedAbsence, graph.CutFinding{
		Kind:     graph.CutContradictedAbsence,
		Relation: &graph.CutRelationRef{AssetID: "asset:cut-a", Definition: relDevicePart, From: "resource:d", To: "resource:p"},
		Absence:  &graph.CutAbsenceRef{AssetID: "asset:cut-b", Definition: relDevicePart, Resource: "resource:p", Side: semreg.AbsenceTo},
	})
	if planes := cut.PlanesOfRelation(key); len(planes) != 0 {
		t.Fatalf("PlanesOfRelation(contradicted) = %v", planes)
	}

	// A candidate instance is not qualified, so nothing contradicts it.
	a.relations = []semreg.RelationInstance{relation(relDevicePart, "resource:d", "resource:p", semreg.LinkCandidate)}
	candidate := assemble(t, graph.CutComplete, a, b)
	requireRelation(t, candidate, key, semreg.LinkCandidate, graph.CoherenceCoherent)
	requireFindings(t, candidate, graph.CutContradictedAbsence)

	// An absence about another definition or another resource does not deny it.
	a.relations = []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:p")}
	for _, other := range []semreg.RelationAbsence{absence(relDevicePartAlt, "resource:p", semreg.AbsenceTo), absence(relDevicePart, "resource:p2", semreg.AbsenceTo)} {
		b.absences = []semreg.RelationAbsence{other}
		b.resources = []semreg.Resource{part("resource:p", "resource:d", "1"), part("resource:p2", "resource:d", "2")}
		cut := assemble(t, graph.CutComplete, a, b)
		requireRelation(t, cut, key, semreg.LinkQualified, graph.CoherenceCoherent)
		requireFindings(t, cut, graph.CutContradictedAbsence)
	}
}

// G-POS-CUT-8: one unit seen through two protocols whose native owners map
// its token to the same scheme, issuer and token gives one anchor key.
func TestAssembleCutSameUnitThroughTwoProtocols(t *testing.T) {
	a := cutAsset{id: "asset:cut-a", protocol: "protocol.first", resources: []semreg.Resource{nativeDevice("asset:cut-a", "resource:a1", identityKey(t, cutScheme, "issuer.one", "unit-0001"))}}
	b := cutAsset{id: "asset:cut-b", protocol: "protocol.second", resources: []semreg.Resource{nativeDevice("asset:cut-b", "resource:b1", identityKey(t, cutScheme, "issuer.one", "unit-0001"))}}
	cut := assemble(t, graph.CutComplete, a, b)
	requireFindings(t, cut, graph.CutSameUnit, graph.CutFinding{Kind: graph.CutSameUnit, Resources: []graph.CutResourceRef{ref("asset:cut-a", "resource:a1"), ref("asset:cut-b", "resource:b1")}})

	// Equal token text under two issuers is two units.
	b.resources = []semreg.Resource{nativeDevice("asset:cut-b", "resource:b1", identityKey(t, cutScheme, "issuer.two", "unit-0001"))}
	requireFindings(t, assemble(t, graph.CutComplete, a, b), graph.CutSameUnit)
}

// G-POS-CUT-9: a far end held by a member this cut excluded is excluded.
func TestAssembleCutFarEndHeldByExcludedMember(t *testing.T) {
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:x", "x"), part("resource:p", "resource:x", "1")}}
	b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{device("resource:x", "x")}}
	c := cutAsset{id: "asset:cut-c", resources: []semreg.Resource{device("resource:d", "d")}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:p")}}
	cut := assemble(t, graph.CutComplete, a, b, c)
	key := relationKey(relDevicePart, "resource:d", "resource:p")
	requireRelation(t, cut, key, semreg.LinkQualified, graph.CoherenceExcluded)
	if planes := cut.PlanesOfRelation(key); len(planes) != 0 {
		t.Fatalf("PlanesOfRelation(excluded) = %v", planes)
	}

	// A kept member that holds the far end only withdrawn does not make it
	// stale: the excluded member's record is hidden, not absent.
	d := cutAsset{id: "asset:cut-d", resources: []semreg.Resource{withState(part("resource:p", "resource:x", "1"), semreg.ResourceWithdrawn)}}
	requireRelation(t, assemble(t, graph.CutComplete, a, b, c, d), key, semreg.LinkQualified, graph.CoherenceExcluded)
}

// G-POS-CUT-10: an enforced member that publishes only facts of unknown
// definitions is not ungraphed, nor is a member without a graph record that
// publishes only such facts.
func TestAssembleCutUnknownFactsAreNotUngraphed(t *testing.T) {
	enforced := cutAsset{id: "asset:cut-e", resources: []semreg.Resource{zone("resource:z", "z")}, facts: []semreg.FactKey{labelFact("one")}}
	plain := cutAsset{id: "asset:cut-f", facts: []semreg.FactKey{labelFact("two")}}
	empty := cutAsset{id: "asset:cut-n"}
	cut := assemble(t, graph.CutComplete, enforced, plain, empty)
	requireFindings(t, cut, graph.CutUngraphed)
}

// G-POS-CUT-11: two resources of one unit whose anchors share no key are not
// detectable, so no finding is reported.
func TestAssembleCutUndetectablePairHasNoFinding(t *testing.T) {
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{nativeDevice("asset:cut-a", "resource:a1", identityKey(t, cutScheme, "issuer.one", "unit-0001"))}}
	b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{device("resource:b1", "unit-0001")}}
	cut := assemble(t, graph.CutComplete, a, b)
	if findings := cut.Findings(); len(findings) != 0 {
		t.Fatalf("Findings = %+v, want none", findings)
	}
}

// A cross-asset relation whose far end is current in another member and of
// an admitted kind is coherent and in its definition's planes.
func TestAssembleCutCrossAssetRelationIsCoherent(t *testing.T) {
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d")}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:p")}}
	b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{part("resource:p", "resource:d", "1")}}
	cut := assemble(t, graph.CutComplete, a, b)
	key := relationKey(relDevicePart, "resource:d", "resource:p")
	requireRelation(t, cut, key, semreg.LinkQualified, graph.CoherenceCoherent)
	if planes := cut.PlanesOfRelation(key); !reflect.DeepEqual(planes, []graph.PlaneID{graph.PlanePhysical, graph.PlaneEnergy}) {
		t.Fatalf("PlanesOfRelation = %v", planes)
	}
	if findings := cut.Findings(); len(findings) != 0 {
		t.Fatalf("Findings = %+v, want none", findings)
	}
}

// Qualified instances held by different members together violate
// cardinality, the forest rule or acyclicity: every participant is conflict.
// A stored conflict stays conflict, and a rejected instance takes no part.
func TestAssembleCutCrossMemberConflicts(t *testing.T) {
	holder := cutAsset{id: "asset:cut-h", resources: []semreg.Resource{part("resource:p", "resource:d1", "1"), zone("resource:z", "z")}}
	for _, tc := range []struct {
		name     string
		a, b     semreg.RelationInstance
		conflict bool
	}{
		{"cardinality of one definition", qualified(relFeeds, "resource:d1", "resource:d3"), qualified(relFeeds, "resource:d2", "resource:d3"), true},
		{"cardinality from one starting end is many", qualified(relFeeds, "resource:d1", "resource:d3"), qualified(relFeeds, "resource:d2", "resource:d1"), false},
		{"forest of one definition", qualified(relDevicePart, "resource:d1", "resource:p"), qualified(relDevicePart, "resource:d2", "resource:p"), true},
		{"forest across definitions", qualified(relDevicePart, "resource:d1", "resource:p"), qualified(relDevicePartAlt, "resource:d2", "resource:p"), true},
		{"one to one far end", qualified(relControls, "resource:d1", "resource:z"), qualified(relControls, "resource:d2", "resource:z"), true},
		{"relays cycle", qualified(relRelays, "resource:d1", "resource:d2"), qualified(relRelays, "resource:d2", "resource:d1"), true},
		{"relays chain", qualified(relRelays, "resource:d1", "resource:d2"), qualified(relRelays, "resource:d2", "resource:d3"), false},
		{"supplies loop is a graph", qualified(relSupplies, "resource:d1", "resource:d2"), qualified(relSupplies, "resource:d2", "resource:d1"), false},
		{"rejected takes no part", qualified(relDevicePart, "resource:d1", "resource:p"), relation(relDevicePart, "resource:d2", "resource:p", semreg.LinkRejected), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d1", "d1")}, relations: []semreg.RelationInstance{tc.a}}
			b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{device("resource:d2", "d2")}, relations: []semreg.RelationInstance{tc.b}}
			c := cutAsset{id: "asset:cut-c", resources: []semreg.Resource{device("resource:d3", "d3")}}
			cut := assemble(t, graph.CutComplete, a, b, c, holder)
			for _, instance := range []semreg.RelationInstance{tc.a, tc.b} {
				state, coherence := instance.State, graph.CoherenceCoherent
				if tc.conflict {
					state, coherence = semreg.LinkConflict, graph.CoherenceConflict
				}
				requireRelation(t, cut, relationKey(instance.Definition, string(instance.From), string(instance.To)), state, coherence)
			}
		})
	}
	t.Run("stored conflict", func(t *testing.T) {
		a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d1", "d1")}, relations: []semreg.RelationInstance{relation(relDevicePart, "resource:d1", "resource:p", semreg.LinkConflict)}}
		cut := assemble(t, graph.CutComplete, a, holder)
		requireRelation(t, cut, relationKey(relDevicePart, "resource:d1", "resource:p"), semreg.LinkConflict, graph.CoherenceConflict)
	})
}

// A resource whose anchor parent is not the starting end of a qualified,
// coherent contains, groups or hosts instance to it is reported, wherever
// that instance is held.
func TestAssembleCutAnchorParentUnresolved(t *testing.T) {
	unresolved := graph.CutFinding{Kind: graph.CutAnchorParentUnresolved, Resources: []graph.CutResourceRef{ref("asset:cut-b", "resource:p")}}
	for _, tc := range []struct {
		name      string
		relations []semreg.RelationInstance
		want      []graph.CutFinding
	}{
		{"no instance", nil, []graph.CutFinding{unresolved}},
		{"coherent contains", []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:p")}, nil},
		{"candidate contains", []semreg.RelationInstance{relation(relDevicePart, "resource:d", "resource:p", semreg.LinkCandidate)}, []graph.CutFinding{unresolved}},
		{"another starting end", []semreg.RelationInstance{qualified(relDevicePart, "resource:d2", "resource:p")}, []graph.CutFinding{unresolved}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d"), device("resource:d2", "d2")}, relations: tc.relations}
			b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{part("resource:p", "resource:d", "1")}}
			requireFindings(t, assemble(t, graph.CutComplete, a, b), graph.CutAnchorParentUnresolved, tc.want...)
		})
	}
	t.Run("conflicting contains", func(t *testing.T) {
		a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d")}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:d", "resource:p")}}
		b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{part("resource:p", "resource:d", "1")}, absences: []semreg.RelationAbsence{absence(relDevicePart, "resource:p", semreg.AbsenceTo)}}
		requireFindings(t, assemble(t, graph.CutComplete, a, b), graph.CutAnchorParentUnresolved, unresolved)
	})
}

// --------------------------------------------------------- invalid input

func TestAssembleCutRejectsOnlyInvalidInput(t *testing.T) {
	catalog := cutCatalog(t)
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:a", "a")}}
	sa := a.snapshot(t)
	t.Run("duplicate_key for two snapshots of one asset", func(t *testing.T) {
		changed := a
		changed.resources = []semreg.Resource{device("resource:a", "a"), device("resource:a2", "a2")}
		for _, pair := range [][]semreg.Snapshot{{sa, changed.snapshot(t)}, {sa, sa}} {
			if _, err := graph.AssembleCut(pair, catalog, graph.CutComplete); semreg.ErrorIdentifier(err) != semreg.DuplicateKey {
				t.Fatalf("AssembleCut(two snapshots of one asset) = %v, want duplicate_key", err)
			}
		}
	})
	t.Run("bounds_exceeded for 257 members", func(t *testing.T) {
		members := make([]semreg.Snapshot, 0, graph.MaxCutMembers+1)
		for i := 0; i <= graph.MaxCutMembers; i++ {
			members = append(members, cutAsset{id: semreg.AssetID(fmt.Sprintf("asset:cut-%03d", i))}.snapshot(t))
		}
		if _, err := graph.AssembleCut(members, catalog, graph.CutComplete); semreg.ErrorIdentifier(err) != semreg.BoundsExceeded {
			t.Fatalf("AssembleCut(257 members) = %v, want bounds_exceeded", err)
		}
		if cut, err := graph.AssembleCut(members[:graph.MaxCutMembers], catalog, graph.CutComplete); err != nil || len(cut.Members()) != 256 {
			t.Fatalf("AssembleCut(256 members) = %v", err)
		}
	})
	t.Run("malformed snapshot", func(t *testing.T) {
		tampered := sa
		tampered.Resources = []semreg.Resource{device("resource:a", "renamed")}
		if _, err := graph.AssembleCut([]semreg.Snapshot{tampered}, catalog, graph.CutComplete); semreg.ErrorIdentifier(err) != semreg.DigestMismatch {
			t.Fatalf("AssembleCut(tampered snapshot) = %v, want digest_mismatch", err)
		}
	})
	t.Run("completeness outside its vocabulary", func(t *testing.T) {
		if _, err := graph.AssembleCut([]semreg.Snapshot{sa}, catalog, graph.Completeness("total")); semreg.ErrorIdentifier(err) != semreg.InvalidEnum {
			t.Fatalf("AssembleCut(completeness total) = %v, want invalid_enum", err)
		}
	})
	t.Run("no catalog", func(t *testing.T) {
		if _, err := graph.AssembleCut([]semreg.Snapshot{sa}, nil, graph.CutComplete); semreg.ErrorIdentifier(err) != semreg.InvalidValue {
			t.Fatalf("AssembleCut(nil catalog) = %v, want invalid_value", err)
		}
	})
}

// ----------------------------------------------------- canonical output

// richCut holds at least one of every finding kind and every coherence.
func richAssets(t *testing.T) []cutAsset {
	t.Helper()
	key := identityKey(t, cutScheme, "issuer.one", "unit-0001")
	return []cutAsset{
		{id: "asset:cut-a", resources: []semreg.Resource{device("resource:x", "x")}},
		{id: "asset:cut-b", resources: []semreg.Resource{device("resource:x", "x"), part("resource:xp", "resource:x", "1")}},
		{id: "asset:cut-c", resources: []semreg.Resource{nativeDevice("asset:cut-c", "resource:c1", key), device("resource:d", "d")}, relations: []semreg.RelationInstance{
			qualified(relDevicePart, "resource:d", "resource:p"), qualified(relDevicePart, "resource:d", "resource:xp"),
			qualified(relDevicePart, "resource:d", "resource:gone"), qualified(relDevicePartAlt, "resource:d", "resource:nowhere"),
			qualified(relControls, "resource:d", "resource:p2"), relation(relRelays, "resource:d", "resource:c1", semreg.LinkCandidate),
		}},
		{id: "asset:cut-e", resources: []semreg.Resource{nativeDevice("asset:cut-e", "resource:e1", key), part("resource:p", "resource:d", "1"), part("resource:p2", "resource:d", "2"), withState(part("resource:gone", "resource:d", "3"), semreg.ResourceRetired)}, absences: []semreg.RelationAbsence{absence(relDevicePart, "resource:p", semreg.AbsenceTo)}},
		{id: "asset:cut-u", facts: []semreg.FactKey{deviceFact(factPower, "unit"), deviceFact(factFault, "unit")}},
		{id: "asset:cut-v", facts: []semreg.FactKey{labelFact("text")}},
	}
}

func TestAssembleCutCanonicalOutput(t *testing.T) {
	cut := assemble(t, graph.CutPartial, richAssets(t)...)
	canonical, err := cut.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	direct, err := semreg.CanonicalJSON(cut.Result())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, direct) {
		t.Fatal("CanonicalJSON differs from the canonical bytes of Result")
	}
	result := cut.Result()
	if result.Contract != graph.CutContract || result.CutID != cut.ID() || result.Completeness != graph.CutPartial {
		t.Fatalf("Result header = (%s, %s, %s)", result.Contract, result.CutID, result.Completeness)
	}
	kinds := map[graph.CutFindingKind]bool{}
	previous := ""
	for _, finding := range result.Findings {
		kinds[finding.Kind] = true
		subject, err := semreg.CanonicalJSON(finding)
		if err != nil {
			t.Fatal(err)
		}
		current := string(finding.Kind) + "\x00" + string(subject)
		if previous != "" && current <= previous {
			t.Fatalf("findings are not ordered by (kind, canonical subject): %s after %s", current, previous)
		}
		previous = current
	}
	for _, kind := range []graph.CutFindingKind{graph.CutDuplicateIdentity, graph.CutSameUnit, graph.CutContradictedAbsence, graph.CutAnchorParentUnresolved, graph.CutUngraphed} {
		if !kinds[kind] {
			t.Fatalf("rich cut has no %s finding: %s", kind, canonical)
		}
	}
	coherences := map[graph.RelationCoherence]bool{}
	previous = ""
	for _, relation := range result.Relations {
		coherences[relation.Coherence] = true
		current := canonicalDefinition(t, relation.Definition) + "\x00" + string(relation.From) + "\x00" + string(relation.To)
		if previous != "" && current <= previous {
			t.Fatalf("relations are not ordered by (definition, from, to): %q after %q", current, previous)
		}
		previous = current
	}
	for _, coherence := range []graph.RelationCoherence{graph.CoherenceCoherent, graph.CoherenceStale, graph.CoherenceOutsideCut, graph.CoherenceExcluded, graph.CoherenceConflict} {
		if !coherences[coherence] {
			t.Fatalf("rich cut has no %s relation: %s", coherence, canonical)
		}
	}
	for i := 1; i < len(result.Members); i++ {
		if result.Members[i-1].AssetID >= result.Members[i].AssetID {
			t.Fatalf("members are not ordered by asset: %+v", result.Members)
		}
	}
	if !json.Valid(canonical) || bytes.Contains(canonical, []byte("\n")) {
		t.Fatalf("canonical output is not compact JSON: %s", canonical)
	}

	// The accessors return detached copies.
	findings := cut.Findings()
	findings[0].Kind = "changed"
	if cut.Findings()[0].Kind == "changed" {
		t.Fatal("Findings returned the cut's own slice")
	}
	relations := cut.Relations()
	relations[0].Coherence = "changed"
	if cut.Relations()[0].Coherence == "changed" {
		t.Fatal("Relations returned the cut's own slice")
	}
	again, err := cut.CanonicalJSON()
	if err != nil || !bytes.Equal(again, canonical) {
		t.Fatal("canonical output changed after accessor mutation")
	}
}

// Identical inputs give identical bytes whatever the input order, and
// assembly leaves its input unchanged.
func TestAssembleCutDeterministic(t *testing.T) {
	catalog := cutCatalog(t)
	members := snapshots(t, richAssets(t)...)
	before, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := graph.AssembleCut(members, catalog, graph.CutComplete)
	if err != nil {
		t.Fatal(err)
	}
	want, err := baseline.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	seed := time.Now().UnixNano()
	random := rand.New(rand.NewSource(seed))
	for round := 0; round < 32; round++ {
		shuffled := append([]semreg.Snapshot(nil), members...)
		random.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		cut, err := graph.AssembleCut(shuffled, catalog, graph.CutComplete)
		if err != nil {
			t.Fatal(err)
		}
		got, err := cut.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) || cut.ID() != baseline.ID() {
			t.Fatalf("seed %d round %d: canonical output depends on input order", seed, round)
		}
	}
	after, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("AssembleCut changed its input")
	}
}
