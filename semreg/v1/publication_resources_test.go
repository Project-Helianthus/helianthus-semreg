package semreg

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

// resourceTestCatalog is a synthetic element catalog with the resource
// lookups. It covers the test pack and answers like the accepted catalog: a
// resource kind has one anchor class and a disposition, a fact has one
// subject, a dimension is a resource or a qualifier dimension and a service
// has one subject kind. Every pack entry is a member unless overridden.
type resourceTestCatalog struct {
	digest         Digest
	index          DefinitionIndex
	kinds          map[DefinitionID]resourceTestKind
	facts          map[DefinitionID]GraphFactSubject
	dimensions     map[DefinitionID]GraphDimension
	services       map[DefinitionID]DefinitionID
	serviceClasses map[DefinitionID][]AnchorClass
	members        map[DefinitionRef]GraphDisposition
	schemes        map[DefinitionID]bool
}

type resourceTestKind struct {
	class       AnchorClass
	disposition GraphDisposition
}

var resourceTestPack = PackRef{ID: "pack.test", Version: "1.0.0"}

func resourceTestRef(id DefinitionID) DefinitionRef {
	return DefinitionRef{Pack: resourceTestPack, ID: id, Version: "1.0.0"}
}

func resourceTestIndex() DefinitionIndex {
	return DefinitionIndex{
		Pack: resourceTestPack,
		Fields: []DefinitionRef{
			resourceTestRef("fact.input.power"), resourceTestRef("fact.inverter.frequency"), resourceTestRef("fact.test"),
			resourceTestRef("fact.unknown.temperature"), resourceTestRef("fact.unsupported"),
		},
		Services:     []DefinitionRef{resourceTestRef("service.test")},
		Capabilities: []DefinitionRef{resourceTestRef("capability.test")},
		Operations:   []DefinitionRef{},
		EffectRules:  []DefinitionRef{},
	}
}

func newResourceTestCatalog(t *testing.T) *resourceTestCatalog {
	t.Helper()
	return &resourceTestCatalog{
		digest: pinnedFixtureDigest(t),
		index:  resourceTestIndex(),
		kinds: map[DefinitionID]resourceTestKind{
			"resource.inverter":         {AnchorEquipment, GraphMember},
			"resource.meter":            {AnchorEquipment, GraphMember},
			"resource.dc_input":         {AnchorComponent, GraphMember},
			"resource.charging_session": {AnchorEpisode, GraphMember},
			"resource.pv_system":        {AnchorFunction, GraphMember},
			"resource.thermal_circuit":  {AnchorFunction, GraphUnknown},
		},
		facts: map[DefinitionID]GraphFactSubject{
			"fact.input.power":         {Disposition: GraphMember, Resolved: true, Dimension: "dimension.input", Kind: "resource.dc_input"},
			"fact.inverter.frequency":  {Disposition: GraphMember, Resolved: true, Dimension: "dimension.inverter", Kind: "resource.inverter"},
			"fact.test":                {Disposition: GraphUnknown},
			"fact.unknown.temperature": {Disposition: GraphUnknown, Qualifiers: []DefinitionID{"dimension.temperature"}},
			"fact.unsupported":         {Disposition: GraphUnsupported, Resolved: true, Dimension: "dimension.inverter", Kind: "resource.inverter"},
		},
		dimensions: map[DefinitionID]GraphDimension{
			"dimension.input":       {Resource: true, Kind: "resource.dc_input"},
			"dimension.inverter":    {Resource: true, Kind: "resource.inverter"},
			"dimension.temperature": {},
		},
		services: map[DefinitionID]DefinitionID{"service.test": "resource.inverter"},
		members:  map[DefinitionRef]GraphDisposition{},
		schemes:  map[DefinitionID]bool{"synthetic.serial": true},
	}
}

func (c *resourceTestCatalog) Digest() Digest { return c.digest }

func (c *resourceTestCatalog) Covers(index DefinitionIndex) error {
	if !reflect.DeepEqual(index, c.index) {
		return &Error{ID: MembershipMissing, Detail: "resource test catalog does not cover the pack"}
	}
	return nil
}

func (c *resourceTestCatalog) Membership(ref DefinitionRef) (GraphMembership, bool) {
	if ref.Pack != resourceTestPack {
		return GraphMembership{}, false
	}
	if disposition, ok := c.members[ref]; ok {
		return GraphMembership{Disposition: disposition, Planes: []GraphPlane{}}, true
	}
	return GraphMembership{Disposition: GraphMember, Planes: []GraphPlane{"physical"}}, true
}

func (c *resourceTestCatalog) ResourceKindMembership(kind DefinitionID) (GraphMembership, bool) {
	entry, ok := c.kinds[kind]
	if !ok {
		return GraphMembership{}, false
	}
	return GraphMembership{Disposition: entry.disposition, Planes: []GraphPlane{"physical"}}, true
}

func (c *resourceTestCatalog) DimensionKind(ref DefinitionRef) (DefinitionID, bool) {
	dimension, ok := c.dimensions[ref.ID]
	if !ok || ref.Pack != resourceTestPack || !dimension.Resource {
		return "", false
	}
	return dimension.Kind, true
}

func (c *resourceTestCatalog) ServiceSubjectKind(ref DefinitionRef) (DefinitionID, bool) {
	kind, ok := c.services[ref.ID]
	return kind, ok && ref.Pack == resourceTestPack && kind != ""
}

func (c *resourceTestCatalog) ServiceSubject(ref DefinitionRef) (DefinitionID, []AnchorClass, bool) {
	if ref.Pack != resourceTestPack {
		return "", nil, false
	}
	if classes := c.serviceClasses[ref.ID]; len(classes) > 0 {
		return "", append([]AnchorClass(nil), classes...), true
	}
	kind, ok := c.services[ref.ID]
	if !ok || kind == "" {
		return "", nil, false
	}
	return kind, nil, true
}

func (c *resourceTestCatalog) RelationDefinition(DefinitionRef) (GraphRelationDefinition, bool) {
	return GraphRelationDefinition{}, false
}

func (c *resourceTestCatalog) SchemeListed(id DefinitionID) bool { return c.schemes[id] }

func (c *resourceTestCatalog) ResourceKindClass(kind DefinitionID) (AnchorClass, bool) {
	entry, ok := c.kinds[kind]
	return entry.class, ok
}

func (c *resourceTestCatalog) FactSubject(pack PackRef, fact DefinitionID) (GraphFactSubject, bool) {
	subject, ok := c.facts[fact]
	if !ok || pack != resourceTestPack {
		return GraphFactSubject{}, false
	}
	subject.Qualifiers = append([]DefinitionID(nil), subject.Qualifiers...)
	return subject, true
}

func (c *resourceTestCatalog) PackDimension(pack PackRef, id DefinitionID) (GraphDimension, bool) {
	dimension, ok := c.dimensions[id]
	return dimension, ok && pack == resourceTestPack
}

// graphShapeValidator records the subject a graph-enforced registry passes.
type graphShapeValidator struct {
	countingValidator
	subjects []GraphFactSubject
}

func (v *graphShapeValidator) ValidateGraphFact(_ FactKey, _ *Value, subject GraphFactSubject) error {
	v.subjects = append(v.subjects, subject)
	return v.reject
}

func newResourceKernel(t *testing.T, catalog *resourceTestCatalog) *PublicationKernel {
	t.Helper()
	if catalog == nil {
		catalog = newResourceTestCatalog(t)
	}
	kernel, err := NewGraphPublicationKernel("asset:pv", catalog, &countingValidator{pack: resourceTestPack, index: resourceTestIndex()})
	if err != nil {
		t.Fatal(err)
	}
	return kernel
}

const (
	resourceConfigSource = SourceID("source:configuration")
	resourceConfigEpoch  = SourceEpochID("epoch:configuration")
	resourceDriverSource = SourceID("source:pv")
	resourceDriverEpoch  = SourceEpochID("epoch:pv:01")
	resourceDriverBind   = NativeBindingID("binding:pv:01")
)

func resourceConfigBatch(sequence, expected Uint64) PublicationBatch {
	batch := publicationBatch("asset:pv", resourceConfigSource, resourceConfigEpoch, "1", sequence, expected)
	if sequence == "1" {
		batch.SourceUpserts = []SourceDescriptor{publicationSource(resourceConfigSource, resourceConfigEpoch)}
	}
	return batch
}

func resourceDriverBatch(sequence, expected Uint64) PublicationBatch {
	batch := publicationBatch("asset:pv", resourceDriverSource, resourceDriverEpoch, "1", sequence, expected)
	if sequence == "1" {
		batch.SourceUpserts = []SourceDescriptor{publicationSource(resourceDriverSource, resourceDriverEpoch)}
		batch.BindingUpserts = []NativeBinding{publicationBinding("asset:pv", resourceDriverSource, resourceDriverEpoch, resourceDriverBind, "1")}
		batch.IdentityLinkUpserts = []IdentityLink{resourceLink(resourceDriverBind, "resource:inverter:01")}
	}
	return batch
}

func resourceLink(binding NativeBindingID, resource ResourceID) IdentityLink {
	link := publicationLink("asset:pv", binding)
	if resource != "" {
		link.Resource = &resource
	}
	return link
}

func operatorOrigin(slot string) OriginRef {
	return OriginRef{OriginID: OriginID("origin:operator:" + slot), Kind: OriginOperator, Evidence: []EvidenceRef{publicEvidence("6")}}
}

func nativeOrigin(binding NativeBindingID, source SourceID, epoch SourceEpochID) OriginRef {
	return OriginRef{OriginID: OriginID("origin:" + string(binding)), Kind: OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []EvidenceRef{publicEvidence("4")}}
}

func driverOrigin() OriginRef {
	return nativeOrigin(resourceDriverBind, resourceDriverSource, resourceDriverEpoch)
}

func operatorResource(id ResourceID, kind DefinitionID, class AnchorClass, slot string, parent *ResourceID) Resource {
	return Resource{
		ResourceID: id, Kind: kind,
		Anchor: ResourceAnchor{Class: class, Basis: AnchorOperatorSlot, Parent: parent, Slot: &slot, Evidence: []EvidenceRef{publicEvidence("6")}},
		State:  ResourceCurrent, Origin: operatorOrigin(slot), Revision: "1",
	}
}

func nativeSlotResource(id ResourceID, kind DefinitionID, class AnchorClass, parent ResourceID, slot string) Resource {
	return Resource{
		ResourceID: id, Kind: kind,
		Anchor: ResourceAnchor{Class: class, Basis: AnchorNativeSlot, Parent: &parent, Slot: &slot, Evidence: []EvidenceRef{publicEvidence("7")}},
		State:  ResourceCurrent, Origin: driverOrigin(), Revision: "1",
	}
}

func nativeIdentityResource(t *testing.T, id ResourceID, kind DefinitionID, issuer, token string) Resource {
	t.Helper()
	scheme := "synthetic.serial"
	key := graphIdentityKey(t, scheme, issuer, token)
	return Resource{
		ResourceID: id, Kind: kind,
		Anchor: ResourceAnchor{Class: AnchorEquipment, Basis: AnchorNativeIdentity, Scheme: &scheme, Key: &key, Evidence: []EvidenceRef{publicEvidence("8")}},
		State:  ResourceCurrent, Origin: driverOrigin(), Revision: "1",
	}
}

func resourceDimension(id DefinitionID, text string) Dimension {
	return Dimension{ID: id, Value: Value{Kind: ValueText, Text: &text}}
}

func resourceFact(id CandidateID, fact DefinitionID, binding NativeBindingID, dimensions ...Dimension) FactCandidate {
	candidate := publicationCandidate(id, fact, true, resourceDriverSource, resourceDriverEpoch, binding, "1")
	candidate.Key.Dimensions = append([]Dimension{}, dimensions...)
	return candidate
}

func resourceService(id ServiceInstanceID, resource ResourceID) ServiceInstance {
	service := publicationService("asset:pv", id, resourceDriverBind, resourceDriverEpoch, "1")
	if resource != "" {
		service.Resource = &resource
	}
	return service
}

func applyResourceBatch(t *testing.T, kernel *PublicationKernel, batch PublicationBatch) Snapshot {
	t.Helper()
	sealPublicationBatch(t, &batch)
	snapshot, _, err := kernel.Apply(batch, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// rejectResourceBatch seals the batch's exact bytes even when a value shape
// is invalid, so the rejection comes from Apply and not from sealing.
func rejectResourceBatch(t *testing.T, kernel *PublicationKernel, batch PublicationBatch, want ErrorID) {
	t.Helper()
	digest, err := batch.computedDigestUnchecked()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	assertRejectedUnchanged(t, kernel, batch, want)
}

// pvBaseKernel publishes the configured PV system and inverter, then the
// driver's binding, its identity link naming the inverter and the two native
// inputs of the inverter. The current semantic revision is 2.
func pvBaseKernel(t *testing.T, catalog *resourceTestCatalog) *PublicationKernel {
	t.Helper()
	kernel := newResourceKernel(t, catalog)
	config := resourceConfigBatch("1", "0")
	config.ResourceUpserts = []Resource{
		operatorResource("resource:inverter:01", "resource.inverter", AnchorEquipment, "inverter-1", nil),
		operatorResource("resource:pv-system:01", "resource.pv_system", AnchorFunction, "pv-system-1", nil),
	}
	applyResourceBatch(t, kernel, config)
	driver := resourceDriverBatch("1", "1")
	driver.ResourceUpserts = []Resource{
		nativeSlotResource("resource:dc-input:01", "resource.dc_input", AnchorComponent, "resource:inverter:01", "1"),
		nativeSlotResource("resource:dc-input:02", "resource.dc_input", AnchorComponent, "resource:inverter:01", "2"),
	}
	applyResourceBatch(t, kernel, driver)
	return kernel
}

func currentResource(t *testing.T, kernel *PublicationKernel, id ResourceID) Resource {
	t.Helper()
	snapshot, _, ok := kernel.Current()
	if !ok {
		t.Fatal("no current snapshot")
	}
	for _, resource := range snapshot.Resources {
		if resource.ResourceID == id {
			return resource
		}
	}
	t.Fatalf("resource %s is not in the snapshot", id)
	return Resource{}
}

// RES-1, G-NEG-RES-1: a resource kind must be a catalog resource kind with
// disposition member.
func TestResourcePublicationRES1KindMembership(t *testing.T) {
	for name, kind := range map[string]DefinitionID{"unknown disposition": "resource.thermal_circuit", "no catalog entry": "resource.boiler"} {
		t.Run(name, func(t *testing.T) {
			kernel := pvBaseKernel(t, nil)
			batch := resourceConfigBatch("2", "2")
			parent := ResourceID("resource:pv-system:01")
			batch.ResourceUpserts = []Resource{operatorResource("resource:circuit:01", kind, AnchorFunction, "circuit-1", &parent)}
			rejectResourceBatch(t, kernel, batch, MembershipMissing)
		})
	}
}

// RES-2, G-NEG-ID-8, G-NEG-ID-9: the anchor class, its members, the basis
// against the origin kind and the identification scheme are checked against
// the catalog.
func TestResourcePublicationRES2Anchor(t *testing.T) {
	inverter := ResourceID("resource:inverter:01")
	text := func(s string) *string { return &s }
	cases := map[string]struct {
		config bool
		build  func(t *testing.T) Resource
	}{
		"class differs from the kind's class": {true, func(*testing.T) Resource {
			return operatorResource("resource:inverter:02", "resource.inverter", AnchorFunction, "inverter-2", nil)
		}},
		"component without parent": {false, func(*testing.T) Resource {
			r := nativeSlotResource("resource:dc-input:03", "resource.dc_input", AnchorComponent, inverter, "3")
			r.Anchor.Parent = nil
			return r
		}},
		"component without slot": {false, func(*testing.T) Resource {
			r := nativeSlotResource("resource:dc-input:03", "resource.dc_input", AnchorComponent, inverter, "3")
			r.Anchor.Slot = nil
			return r
		}},
		"episode without parent": {false, func(*testing.T) Resource {
			r := nativeSlotResource("resource:session:01", "resource.charging_session", AnchorEpisode, inverter, "session-1")
			r.Anchor.Parent = nil
			return r
		}},
		"function without slot": {true, func(*testing.T) Resource {
			r := operatorResource("resource:pv-system:02", "resource.pv_system", AnchorFunction, "pv-system-2", nil)
			r.Anchor.Slot = nil
			return r
		}},
		"equipment on operator slot without slot": {true, func(*testing.T) Resource {
			r := operatorResource("resource:inverter:02", "resource.inverter", AnchorEquipment, "inverter-2", nil)
			r.Anchor.Slot = nil
			return r
		}},
		"native slot with an operator origin": {true, func(*testing.T) Resource {
			r := nativeSlotResource("resource:dc-input:03", "resource.dc_input", AnchorComponent, inverter, "3")
			r.Origin = operatorOrigin("3")
			return r
		}},
		"native identity with an operator origin": {true, func(t *testing.T) Resource {
			r := nativeIdentityResource(t, "resource:inverter:02", "resource.inverter", "0001", "TOKEN-0002")
			r.Origin = operatorOrigin("inverter-2")
			return r
		}},
		"operator slot with a native origin": {false, func(*testing.T) Resource {
			r := operatorResource("resource:inverter:02", "resource.inverter", AnchorEquipment, "inverter-2", nil)
			r.Origin = driverOrigin()
			return r
		}},
		"G-NEG-ID-8 native identity with an unlisted scheme": {false, func(t *testing.T) Resource {
			r := nativeIdentityResource(t, "resource:inverter:03", "resource.inverter", "0001", "TOKEN-0001")
			r.Anchor.Scheme = text("synthetic.unlisted")
			return r
		}},
		"native identity without scheme": {false, func(t *testing.T) Resource {
			r := nativeIdentityResource(t, "resource:inverter:03", "resource.inverter", "0001", "TOKEN-0001")
			r.Anchor.Scheme = nil
			return r
		}},
		"G-NEG-ID-9 operator slot with a scheme": {true, func(*testing.T) Resource {
			r := operatorResource("resource:inverter:03", "resource.inverter", AnchorEquipment, "inverter-3", nil)
			r.Anchor.Scheme = text("synthetic.serial")
			return r
		}},
		"native slot with a scheme": {false, func(*testing.T) Resource {
			r := nativeSlotResource("resource:dc-input:03", "resource.dc_input", AnchorComponent, inverter, "3")
			r.Anchor.Scheme = text("synthetic.serial")
			return r
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := pvBaseKernel(t, nil)
			batch := resourceDriverBatch("2", "2")
			if tc.config {
				batch = resourceConfigBatch("2", "2")
			}
			batch.ResourceUpserts = []Resource{tc.build(t)}
			rejectResourceBatch(t, kernel, batch, InvalidValue)
		})
	}

	t.Run("accepted shapes", func(t *testing.T) {
		kernel := pvBaseKernel(t, nil)
		batch := resourceDriverBatch("2", "2")
		batch.ResourceUpserts = []Resource{
			nativeSlotResource("resource:dc-input:03", "resource.dc_input", AnchorComponent, inverter, "3"),
			nativeIdentityResource(t, "resource:inverter:03", "resource.inverter", "0001", "TOKEN-0001"),
			nativeSlotResource("resource:session:01", "resource.charging_session", AnchorEpisode, inverter, "session-1"),
		}
		snapshot := applyResourceBatch(t, kernel, batch)
		if len(snapshot.Resources) != 7 {
			t.Fatalf("got %d resources, want 7", len(snapshot.Resources))
		}
		// A function without a parent is a root of the installation.
		root := resourceConfigBatch("2", "3")
		root.ResourceUpserts = []Resource{operatorResource("resource:pv-system:02", "resource.pv_system", AnchorFunction, "pv-system-2", nil)}
		applyResourceBatch(t, kernel, root)
	})
}

// RES-3 and RES-9: the kind never changes and the anchor key changes only by
// an allowed supersession naming the exact previous anchor.
func TestResourcePublicationRES3AndRES9Supersession(t *testing.T) {
	upgraded := func(t *testing.T, kernel *PublicationKernel) Resource {
		t.Helper()
		next := nativeIdentityResource(t, "resource:inverter:01", "resource.inverter", "0001", "TOKEN-0001")
		next.Supersedes = &AnchorSupersession{Previous: currentResource(t, kernel, "resource:inverter:01").Anchor, Evidence: []EvidenceRef{publicEvidence("9")}}
		next.Revision = "2"
		return next
	}
	cases := map[string]struct {
		config bool
		want   ErrorID
		build  func(t *testing.T, kernel *PublicationKernel) Resource
	}{
		"G-NEG-ID-2 kind changes": {true, IdentityDrift, func(t *testing.T, kernel *PublicationKernel) Resource {
			r := currentResource(t, kernel, "resource:inverter:01")
			r.Kind, r.Revision = "resource.meter", "2"
			return r
		}},
		"G-NEG-ID-6 anchor change without supersedes": {false, IdentityDrift, func(t *testing.T, kernel *PublicationKernel) Resource {
			r := upgraded(t, kernel)
			r.Supersedes = nil
			return r
		}},
		"G-NEG-ID-6 previous differs from the prior anchor": {false, IdentityDrift, func(t *testing.T, kernel *PublicationKernel) Resource {
			r := upgraded(t, kernel)
			slot := "inverter-9"
			r.Supersedes.Previous.Slot = &slot
			return r
		}},
		"previous evidence differs from the prior anchor": {false, IdentityDrift, func(t *testing.T, kernel *PublicationKernel) Resource {
			r := upgraded(t, kernel)
			r.Supersedes.Previous.Evidence = []EvidenceRef{publicEvidence("5")}
			return r
		}},
		"slot change on the same basis": {true, IdentityDrift, func(t *testing.T, kernel *PublicationKernel) Resource {
			r := currentResource(t, kernel, "resource:inverter:01")
			slot := "inverter-9"
			r.Supersedes = &AnchorSupersession{Previous: r.Anchor, Evidence: []EvidenceRef{publicEvidence("9")}}
			r.Anchor.Slot, r.Origin.OriginID, r.Revision = &slot, "origin:operator:inverter-9", "2"
			return r
		}},
		"supersedes on a new resource": {false, IdentityDrift, func(t *testing.T, kernel *PublicationKernel) Resource {
			r := nativeIdentityResource(t, "resource:inverter:05", "resource.inverter", "0001", "TOKEN-0005")
			r.Supersedes = &AnchorSupersession{Previous: currentResource(t, kernel, "resource:inverter:01").Anchor, Evidence: []EvidenceRef{publicEvidence("9")}}
			return r
		}},
		"supersedes changes without an anchor change": {true, IdentityDrift, func(t *testing.T, kernel *PublicationKernel) Resource {
			r := currentResource(t, kernel, "resource:inverter:01")
			r.Supersedes = &AnchorSupersession{Previous: r.Anchor, Evidence: []EvidenceRef{publicEvidence("9")}}
			r.Revision = "2"
			return r
		}},
		"supersession with an empty evidence list": {false, IdentityDrift, func(t *testing.T, kernel *PublicationKernel) Resource {
			r := upgraded(t, kernel)
			r.Supersedes.Evidence = []EvidenceRef{}
			return r
		}},
		"new key already held by another current resource": {false, DuplicateIdentity, func(t *testing.T, kernel *PublicationKernel) Resource {
			return upgraded(t, kernel)
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := pvBaseKernel(t, nil)
			expected := Uint64("2")
			if tc.want == DuplicateIdentity {
				holder := resourceDriverBatch("2", "2")
				holder.ResourceUpserts = []Resource{nativeIdentityResource(t, "resource:inverter:04", "resource.inverter", "0001", "TOKEN-0001")}
				applyResourceBatch(t, kernel, holder)
				expected = "3"
			}
			batch := resourceDriverBatch(Uint64(map[bool]string{true: "3", false: "2"}[expected == "3"]), expected)
			if tc.config {
				batch = resourceConfigBatch("2", expected)
			}
			batch.ResourceUpserts = []Resource{tc.build(t, kernel)}
			rejectResourceBatch(t, kernel, batch, tc.want)
		})
	}

	t.Run("G-NEG-ID-7 downgrade from native identity to operator slot", func(t *testing.T) {
		kernel := pvBaseKernel(t, nil)
		native := resourceDriverBatch("2", "2")
		native.ResourceUpserts = []Resource{nativeIdentityResource(t, "resource:inverter:03", "resource.inverter", "0001", "TOKEN-0001")}
		applyResourceBatch(t, kernel, native)
		previous := currentResource(t, kernel, "resource:inverter:03")
		down := operatorResource("resource:inverter:03", "resource.inverter", AnchorEquipment, "inverter-3", nil)
		down.Supersedes = &AnchorSupersession{Previous: previous.Anchor, Evidence: []EvidenceRef{publicEvidence("9")}}
		down.Revision = "2"
		batch := resourceConfigBatch("2", "3")
		batch.ResourceUpserts = []Resource{down}
		rejectResourceBatch(t, kernel, batch, IdentityDrift)
	})

	t.Run("native identity to native identity under another scheme", func(t *testing.T) {
		catalog := newResourceTestCatalog(t)
		catalog.schemes["synthetic.other"] = true
		kernel := pvBaseKernel(t, catalog)
		native := resourceDriverBatch("2", "2")
		native.ResourceUpserts = []Resource{nativeIdentityResource(t, "resource:inverter:03", "resource.inverter", "0001", "TOKEN-0001")}
		applyResourceBatch(t, kernel, native)
		previous := currentResource(t, kernel, "resource:inverter:03")
		other := "synthetic.other"
		key := graphIdentityKey(t, other, "0001", "TOKEN-0001")
		next := previous
		next.Anchor.Scheme, next.Anchor.Key = &other, &key
		next.Supersedes = &AnchorSupersession{Previous: previous.Anchor, Evidence: []EvidenceRef{publicEvidence("9")}}
		next.Revision = "2"
		batch := resourceDriverBatch("3", "3")
		batch.ResourceUpserts = []Resource{next}
		rejectResourceBatch(t, kernel, batch, IdentityDrift)

		// The native owner revises the canonical token form: same scheme.
		revised := graphIdentityKey(t, "synthetic.serial", "0001", "token-0001")
		next = previous
		next.Anchor.Key = &revised
		next.Supersedes = &AnchorSupersession{Previous: previous.Anchor, Evidence: []EvidenceRef{publicEvidence("9")}}
		next.Revision = "2"
		batch.ResourceUpserts = []Resource{next}
		snapshot := applyResourceBatch(t, kernel, batch)
		if got := currentResource(t, kernel, "resource:inverter:03"); got.Revision != "2" || *got.Anchor.Key != revised || snapshot.Revisions.Structure != "4" {
			t.Fatalf("revised token form not recorded: %+v structure %s", got, snapshot.Revisions.Structure)
		}
	})

	t.Run("G-POS-ID-2 operator slot upgraded to native identity", func(t *testing.T) {
		kernel := pvBaseKernel(t, nil)
		previous := currentResource(t, kernel, "resource:inverter:01")
		batch := resourceDriverBatch("2", "2")
		next := upgraded(t, kernel)
		batch.ResourceUpserts = []Resource{next}
		applyResourceBatch(t, kernel, batch)
		got := currentResource(t, kernel, "resource:inverter:01")
		if got.Revision != "2" || got.Anchor.Basis != AnchorNativeIdentity || got.Supersedes == nil || !reflect.DeepEqual(got.Supersedes.Previous, previous.Anchor) {
			t.Fatalf("supersession not recorded: %+v", got)
		}
		// The supersession stays in the record until the next anchor change.
		kept := got
		kept.Anchor.Evidence = []EvidenceRef{publicEvidence("8"), publicEvidence("9")}
		kept.Revision = "3"
		again := resourceDriverBatch("3", "3")
		again.ResourceUpserts = []Resource{kept}
		applyResourceBatch(t, kernel, again)
		if got := currentResource(t, kernel, "resource:inverter:01"); got.Supersedes == nil || got.Revision != "3" {
			t.Fatalf("supersession dropped on an evidence-only change: %+v", got)
		}
		// A native slot is upgraded to a native identity as well.
		input := currentResource(t, kernel, "resource:dc-input:01")
		lifted := input
		scheme := "synthetic.serial"
		key := graphIdentityKey(t, scheme, "0001", "INPUT-0001")
		lifted.Anchor = ResourceAnchor{Class: AnchorComponent, Basis: AnchorNativeIdentity, Scheme: &scheme, Key: &key, Parent: input.Anchor.Parent, Slot: input.Anchor.Slot, Evidence: []EvidenceRef{publicEvidence("8")}}
		lifted.Supersedes = &AnchorSupersession{Previous: input.Anchor, Evidence: []EvidenceRef{publicEvidence("9")}}
		lifted.Revision = "2"
		third := resourceDriverBatch("4", "4")
		third.ResourceUpserts = []Resource{lifted}
		applyResourceBatch(t, kernel, third)
	})

	t.Run("operator slot upgraded to native slot", func(t *testing.T) {
		kernel := pvBaseKernel(t, nil)
		parent := ResourceID("resource:pv-system:01")
		configured := resourceConfigBatch("2", "2")
		configured.ResourceUpserts = []Resource{operatorResource("resource:dc-input:09", "resource.dc_input", AnchorComponent, "9", &parent)}
		applyResourceBatch(t, kernel, configured)
		previous := currentResource(t, kernel, "resource:dc-input:09")
		next := nativeSlotResource("resource:dc-input:09", "resource.dc_input", AnchorComponent, parent, "9")
		next.Supersedes = &AnchorSupersession{Previous: previous.Anchor, Evidence: []EvidenceRef{publicEvidence("9")}}
		next.Revision = "2"
		batch := resourceDriverBatch("2", "3")
		batch.ResourceUpserts = []Resource{next}
		applyResourceBatch(t, kernel, batch)
	})
}

// RES-4: no two current resources share an anchor key and a retired
// ResourceID is never current again. The anchor key is (kind, key) for a
// native identity and (kind, parent, slot) otherwise; each member is varied
// alone.
func TestResourcePublicationRES4DuplicateIdentity(t *testing.T) {
	inverter := ResourceID("resource:inverter:01")
	other := ResourceID("resource:inverter:02")
	cases := map[string]struct {
		config bool
		build  func(t *testing.T) Resource
		want   ErrorID
	}{
		"G-NEG-ID-1 same kind and operator slot": {true, func(*testing.T) Resource {
			return operatorResource("resource:inverter:09", "resource.inverter", AnchorEquipment, "inverter-1", nil)
		}, DuplicateIdentity},
		"slot key: another kind is another key": {true, func(*testing.T) Resource {
			return operatorResource("resource:meter:09", "resource.meter", AnchorEquipment, "inverter-1", nil)
		}, ""},
		"slot key: another slot is another key": {true, func(*testing.T) Resource {
			return operatorResource("resource:inverter:09", "resource.inverter", AnchorEquipment, "inverter-9", nil)
		}, ""},
		"slot key: same parent and slot": {false, func(*testing.T) Resource {
			return nativeSlotResource("resource:dc-input:09", "resource.dc_input", AnchorComponent, inverter, "1")
		}, DuplicateIdentity},
		"slot key: another parent is another key": {false, func(*testing.T) Resource {
			return nativeSlotResource("resource:dc-input:09", "resource.dc_input", AnchorComponent, other, "1")
		}, ""},
		"native key: same kind and key": {false, func(t *testing.T) Resource {
			return nativeIdentityResource(t, "resource:inverter:09", "resource.inverter", "0001", "TOKEN-0001")
		}, DuplicateIdentity},
		"G-POS-ID-3 native key: equal token under another issuer": {false, func(t *testing.T) Resource {
			return nativeIdentityResource(t, "resource:inverter:09", "resource.inverter", "0002", "TOKEN-0001")
		}, ""},
		"native key: another token": {false, func(t *testing.T) Resource {
			return nativeIdentityResource(t, "resource:inverter:09", "resource.inverter", "0001", "TOKEN-0009")
		}, ""},
		"native key: another kind": {false, func(t *testing.T) Resource {
			return nativeIdentityResource(t, "resource:meter:09", "resource.meter", "0001", "TOKEN-0001")
		}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := pvBaseKernel(t, nil)
			holder := resourceDriverBatch("2", "2")
			holder.ResourceUpserts = []Resource{nativeIdentityResource(t, "resource:inverter:03", "resource.inverter", "0001", "TOKEN-0001")}
			applyResourceBatch(t, kernel, holder)
			batch := resourceDriverBatch("3", "3")
			if tc.config {
				batch = resourceConfigBatch("2", "3")
			}
			batch.ResourceUpserts = []Resource{tc.build(t)}
			if tc.want != "" {
				rejectResourceBatch(t, kernel, batch, tc.want)
				return
			}
			applyResourceBatch(t, kernel, batch)
		})
	}

	t.Run("a withdrawn holder frees its key", func(t *testing.T) {
		kernel := pvBaseKernel(t, nil)
		withdrawn := currentResource(t, kernel, "resource:pv-system:01")
		withdrawn.State, withdrawn.Revision = ResourceWithdrawn, "2"
		batch := resourceConfigBatch("2", "2")
		batch.ResourceUpserts = []Resource{withdrawn, operatorResource("resource:pv-system:09", "resource.pv_system", AnchorFunction, "pv-system-1", nil)}
		applyResourceBatch(t, kernel, batch)
	})

	t.Run("G-NEG-ID-3 retired ResourceID made current", func(t *testing.T) {
		kernel := pvBaseKernel(t, nil)
		retired := currentResource(t, kernel, "resource:pv-system:01")
		retired.State, retired.Revision = ResourceRetired, "2"
		batch := resourceConfigBatch("2", "2")
		batch.ResourceUpserts = []Resource{retired}
		applyResourceBatch(t, kernel, batch)
		for _, state := range []ResourceState{ResourceCurrent, ResourceWithdrawn} {
			again := retired
			again.State, again.Revision = state, "3"
			next := resourceConfigBatch("3", "3")
			next.ResourceUpserts = []Resource{again}
			rejectResourceBatch(t, kernel, next, DuplicateIdentity)
		}
	})
}

// RES-5, G-NEG-RES-2 and PLN-7: a resource dimension value and a service's
// resource resolve to a current resource of the declared kind; a fact of an
// unknown definition names no resource and stays publishable.
func TestResourcePublicationRES5Subjects(t *testing.T) {
	cases := map[string]struct {
		fact    *FactCandidate
		service *ServiceInstance
		want    ErrorID
	}{
		"G-NEG-RES-2 dimension names no resource":         {fact: pointerTo(resourceFact("candidate:dc-power:99", "fact.input.power", resourceDriverBind, resourceDimension("dimension.input", "resource:dc-input:99"))), want: DanglingReference},
		"dimension names a resource of another kind":      {fact: pointerTo(resourceFact("candidate:dc-power:01", "fact.input.power", resourceDriverBind, resourceDimension("dimension.input", "resource:inverter:01"))), want: IdentityDrift},
		"dimension value is not a resource identifier":    {fact: pointerTo(resourceFact("candidate:dc-power:01", "fact.input.power", resourceDriverBind, resourceDimension("dimension.input", "input 1"))), want: InvalidValue},
		"unsupported fact definition":                     {fact: pointerTo(resourceFact("candidate:unsupported:01", "fact.unsupported", resourceDriverBind, resourceDimension("dimension.inverter", "resource:inverter:01"))), want: MembershipMissing},
		"fact definition without a catalog entry":         {fact: pointerTo(resourceFact("candidate:other:01", "fact.other", resourceDriverBind)), want: MembershipMissing},
		"service without resource":                        {service: pointerTo(resourceService("service:inverter:01", "")), want: DanglingReference},
		"service names no resource":                       {service: pointerTo(resourceService("service:inverter:01", "resource:inverter:99")), want: DanglingReference},
		"service names a resource of another kind":        {service: pointerTo(resourceService("service:inverter:01", "resource:dc-input:01")), want: IdentityDrift},
		"fact names its resource":                         {fact: pointerTo(resourceFact("candidate:dc-power:01", "fact.input.power", resourceDriverBind, resourceDimension("dimension.input", "resource:dc-input:01")))},
		"service names its resource":                      {service: pointerTo(resourceService("service:inverter:01", "resource:inverter:01"))},
		"PLN-7 unknown fact with its qualifier dimension": {fact: pointerTo(resourceFact("candidate:temperature:01", "fact.unknown.temperature", resourceDriverBind, resourceDimension("dimension.temperature", "flow")))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := pvBaseKernel(t, nil)
			batch := resourceDriverBatch("2", "2")
			if tc.fact != nil {
				batch.FactUpserts = []FactCandidate{*tc.fact}
			}
			if tc.service != nil {
				batch.ServiceUpserts = []ServiceInstance{*tc.service}
			}
			if tc.want != "" {
				rejectResourceBatch(t, kernel, batch, tc.want)
				return
			}
			applyResourceBatch(t, kernel, batch)
		})
	}

	t.Run("dimension names a withdrawn resource", func(t *testing.T) {
		kernel := pvBaseKernel(t, nil)
		withdrawn := currentResource(t, kernel, "resource:dc-input:02")
		withdrawn.State, withdrawn.Revision = ResourceWithdrawn, "2"
		first := resourceDriverBatch("2", "2")
		first.ResourceUpserts = []Resource{withdrawn}
		applyResourceBatch(t, kernel, first)
		batch := resourceDriverBatch("3", "3")
		batch.FactUpserts = []FactCandidate{resourceFact("candidate:dc-power:02", "fact.input.power", resourceDriverBind, resourceDimension("dimension.input", "resource:dc-input:02"))}
		rejectResourceBatch(t, kernel, batch, DanglingReference)
	})

	t.Run("an unknown or unsupported service or capability is not instantiated", func(t *testing.T) {
		for _, ref := range []DefinitionRef{resourceTestRef("service.test"), resourceTestRef("capability.test")} {
			for _, disposition := range []GraphDisposition{GraphUnknown, GraphUnsupported} {
				catalog := newResourceTestCatalog(t)
				catalog.members[ref] = disposition
				kernel := pvBaseKernel(t, catalog)
				batch := resourceDriverBatch("2", "2")
				batch.ServiceUpserts = []ServiceInstance{resourceService("service:inverter:01", "resource:inverter:01")}
				batch.CapabilityUpserts = []CapabilityInstance{publicationCapability("asset:pv", "capability:inverter:01", "service:inverter:01", resourceDriverBind, resourceDriverEpoch, "1")}
				rejectResourceBatch(t, kernel, batch, MembershipMissing)
			}
		}
	})
}

func pointerTo[T any](value T) *T { return &value }

// RES-6: an observed fact about R comes from a binding whose non-withdrawn
// identity link names R or R's anchor parent chain up to an equipment or
// function resource; a promoted fact needs that link qualified.
func TestResourcePublicationRES6IdentityQualification(t *testing.T) {
	unpromoted := func(c FactCandidate) FactCandidate {
		c.Quality.Promotion = PromotionUnpromoted
		return c
	}
	frequency := func(binding NativeBindingID) FactCandidate {
		return resourceFact("candidate:frequency:"+CandidateID(binding), "fact.inverter.frequency", binding, resourceDimension("dimension.inverter", "resource:inverter:01"))
	}
	cases := map[string]struct {
		link IdentityLink
		fact FactCandidate
		want ErrorID
	}{
		"link names no resource":            {resourceLink("binding:pv:02", ""), frequency("binding:pv:02"), IdentityNotQualified},
		"link names another resource":       {resourceLink("binding:pv:02", "resource:pv-system:01"), frequency("binding:pv:02"), IdentityNotQualified},
		"link names a child of the subject": {resourceLink("binding:pv:02", "resource:dc-input:01"), frequency("binding:pv:02"), IdentityNotQualified},
		"candidate link for a promoted fact": {func() IdentityLink {
			l := resourceLink("binding:pv:02", "resource:inverter:01")
			l.State = LinkCandidate
			return l
		}(), frequency("binding:pv:02"), IdentityNotQualified},
		"candidate link for an unpromoted fact": {func() IdentityLink {
			l := resourceLink("binding:pv:02", "resource:inverter:01")
			l.State = LinkCandidate
			return l
		}(), unpromoted(frequency("binding:pv:02")), ""},
		"link names the subject":                      {resourceLink("binding:pv:02", "resource:inverter:01"), frequency("binding:pv:02"), ""},
		"link names the anchor parent of the subject": {resourceLink("binding:pv:02", "resource:inverter:01"), resourceFact("candidate:dc-power:01", "fact.input.power", "binding:pv:02", resourceDimension("dimension.input", "resource:dc-input:01")), ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := pvBaseKernel(t, nil)
			batch := resourceDriverBatch("2", "2")
			batch.BindingUpserts = []NativeBinding{publicationBinding("asset:pv", resourceDriverSource, resourceDriverEpoch, "binding:pv:02", "1")}
			batch.IdentityLinkUpserts = []IdentityLink{tc.link}
			batch.FactUpserts = []FactCandidate{tc.fact}
			if tc.want != "" {
				rejectResourceBatch(t, kernel, batch, tc.want)
				return
			}
			applyResourceBatch(t, kernel, batch)
		})
	}

	t.Run("a link change that strands a current fact", func(t *testing.T) {
		kernel := pvBaseKernel(t, nil)
		first := resourceDriverBatch("2", "2")
		first.FactUpserts = []FactCandidate{frequency(resourceDriverBind)}
		applyResourceBatch(t, kernel, first)
		moved := resourceLink(resourceDriverBind, "resource:pv-system:01")
		moved.Revision = "2"
		batch := resourceDriverBatch("3", "3")
		batch.IdentityLinkUpserts = []IdentityLink{moved}
		rejectResourceBatch(t, kernel, batch, IdentityNotQualified)
	})
}

// RES-7, G-NEG-ID-4 and G-NEG-ID-5: an identity link naming a natively
// anchored resource carries that resource's anchor key, and a qualified link
// naming a resource has a basis.
func TestResourcePublicationRES7IdentityLinkKey(t *testing.T) {
	setup := func(t *testing.T) *PublicationKernel {
		kernel := pvBaseKernel(t, nil)
		native := resourceDriverBatch("2", "2")
		native.ResourceUpserts = []Resource{nativeIdentityResource(t, "resource:inverter:03", "resource.inverter", "0001", "TOKEN-0001")}
		applyResourceBatch(t, kernel, native)
		return kernel
	}
	link := func(t *testing.T, token string) IdentityLink {
		l := resourceLink("binding:pv:02", "resource:inverter:03")
		if token != "" {
			key := graphIdentityKey(t, "synthetic.serial", "0001", token)
			l.Key = &key
		}
		return l
	}
	cases := map[string]struct {
		link IdentityLink
		want ErrorID
	}{
		"G-NEG-ID-4 key differs from the anchor key":    {link(t, "TOKEN-0002"), IdentityNotQualified},
		"G-NEG-ID-5 qualified link with an empty basis": {func() IdentityLink { l := link(t, ""); l.Basis = []EvidenceRef{}; return l }(), IdentityNotQualified},
		"link names no resource": {func() IdentityLink {
			l := link(t, "")
			missing := ResourceID("resource:inverter:99")
			l.Resource = &missing
			return l
		}(), DanglingReference},
		"key equals the anchor key": {link(t, "TOKEN-0001"), ""},
		"link without key":          {link(t, ""), ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := setup(t)
			batch := resourceDriverBatch("3", "3")
			batch.BindingUpserts = []NativeBinding{publicationBinding("asset:pv", resourceDriverSource, resourceDriverEpoch, "binding:pv:02", "1")}
			batch.IdentityLinkUpserts = []IdentityLink{tc.link}
			if tc.want != "" {
				rejectResourceBatch(t, kernel, batch, tc.want)
				return
			}
			applyResourceBatch(t, kernel, batch)
		})
	}
}

// RES-8: a resource named by a current fact or service is neither withdrawn
// nor retired; once nothing names it, both are accepted, and a withdrawn
// resource becomes current again with a new revision.
func TestResourcePublicationRES8WithdrawalAndRetirement(t *testing.T) {
	for _, state := range []ResourceState{ResourceWithdrawn, ResourceRetired} {
		t.Run(string(state)+" named by a current fact", func(t *testing.T) {
			kernel := pvBaseKernel(t, nil)
			named := resourceDriverBatch("2", "2")
			named.FactUpserts = []FactCandidate{resourceFact("candidate:dc-power:01", "fact.input.power", resourceDriverBind, resourceDimension("dimension.input", "resource:dc-input:01"))}
			applyResourceBatch(t, kernel, named)
			ended := currentResource(t, kernel, "resource:dc-input:01")
			ended.State, ended.Revision = state, "2"
			batch := resourceDriverBatch("3", "3")
			batch.ResourceUpserts = []Resource{ended}
			rejectResourceBatch(t, kernel, batch, DanglingReference)

			withdrawn := resourceDriverBatch("3", "3")
			withdrawn.FactWithdrawals = []CandidateID{"candidate:dc-power:01"}
			withdrawn.ResourceUpserts = []Resource{ended}
			applyResourceBatch(t, kernel, withdrawn)
		})
		t.Run(string(state)+" named by a current service", func(t *testing.T) {
			kernel := pvBaseKernel(t, nil)
			named := resourceDriverBatch("2", "2")
			named.ServiceUpserts = []ServiceInstance{resourceService("service:inverter:01", "resource:inverter:01")}
			applyResourceBatch(t, kernel, named)
			ended := currentResource(t, kernel, "resource:inverter:01")
			ended.State, ended.Revision = state, "2"
			batch := resourceConfigBatch("2", "3")
			batch.ResourceUpserts = []Resource{ended}
			rejectResourceBatch(t, kernel, batch, DanglingReference)
		})
	}

	t.Run("withdrawn back to current", func(t *testing.T) {
		kernel := pvBaseKernel(t, nil)
		withdrawn := currentResource(t, kernel, "resource:pv-system:01")
		withdrawn.State, withdrawn.Revision = ResourceWithdrawn, "2"
		first := resourceConfigBatch("2", "2")
		first.ResourceUpserts = []Resource{withdrawn}
		applyResourceBatch(t, kernel, first)
		current := withdrawn
		current.State, current.Revision = ResourceCurrent, "3"
		second := resourceConfigBatch("3", "3")
		second.ResourceUpserts = []Resource{current}
		applyResourceBatch(t, kernel, second)
		stale := current
		stale.State, stale.Revision = ResourceWithdrawn, "3"
		third := resourceConfigBatch("4", "4")
		third.ResourceUpserts = []Resource{stale}
		rejectResourceBatch(t, kernel, third, RevisionConflict)
	})
}

// PUB-3: a resource's native origin resolves to a binding of its home asset.
func TestResourcePublicationOriginResolvesInTheHomeSnapshot(t *testing.T) {
	kernel := pvBaseKernel(t, nil)
	batch := resourceDriverBatch("2", "2")
	stranger := nativeSlotResource("resource:dc-input:03", "resource.dc_input", AnchorComponent, "resource:inverter:01", "3")
	stranger.Origin = nativeOrigin("binding:elsewhere", resourceDriverSource, resourceDriverEpoch)
	batch.ResourceUpserts = []Resource{stranger}
	rejectResourceBatch(t, kernel, batch, DanglingReference)

	mismatched := stranger
	mismatched.Origin = nativeOrigin(resourceDriverBind, resourceDriverSource, "epoch:pv:09")
	batch.ResourceUpserts = []Resource{mismatched}
	rejectResourceBatch(t, kernel, batch, DanglingReference)
}

// G-POS-ID-1: a new source epoch after a driver restart never rewrites a
// resource; the resource records keep their identifiers and revisions.
func TestResourcePublicationNewSourceEpochLeavesResourcesUnchanged(t *testing.T) {
	kernel := pvBaseKernel(t, nil)
	before, _, _ := kernel.Current()
	epoch := SourceEpochID("epoch:pv:02")
	binding := NativeBindingID("binding:pv:01:epoch-2")
	batch := publicationBatch("asset:pv", resourceDriverSource, epoch, "1", "1", "2")
	batch.SourceUpserts = []SourceDescriptor{publicationSource(resourceDriverSource, epoch)}
	batch.SourceRetirements = []SourceEpochID{resourceDriverEpoch}
	batch.BindingUpserts = []NativeBinding{publicationBinding("asset:pv", resourceDriverSource, epoch, binding, "1")}
	batch.IdentityLinkUpserts = []IdentityLink{resourceLink(binding, "resource:inverter:01")}
	fact := publicationCandidate("candidate:frequency:01", "fact.inverter.frequency", true, resourceDriverSource, epoch, binding, "1")
	fact.Key.Dimensions = []Dimension{resourceDimension("dimension.inverter", "resource:inverter:01")}
	batch.FactUpserts = []FactCandidate{fact}
	after := applyResourceBatch(t, kernel, batch)
	if !reflect.DeepEqual(after.Resources, before.Resources) {
		t.Fatal("a new source epoch rewrote a resource")
	}
	if after.Revisions.Structure != before.Revisions.Structure {
		t.Fatalf("structure revision moved from %s to %s without a resource change", before.Revisions.Structure, after.Revisions.Structure)
	}
}

// One resource observed through two bindings of its home asset stays one
// resource, and each binding's identity link names it.
func TestResourcePublicationOneResourceThroughTwoBindings(t *testing.T) {
	kernel := pvBaseKernel(t, nil)
	before, _, _ := kernel.Current()
	batch := resourceDriverBatch("2", "2")
	batch.BindingUpserts = []NativeBinding{publicationBinding("asset:pv", resourceDriverSource, resourceDriverEpoch, "binding:pv:03", "1")}
	batch.IdentityLinkUpserts = []IdentityLink{resourceLink("binding:pv:03", "resource:inverter:01")}
	batch.FactUpserts = []FactCandidate{
		resourceFact("candidate:frequency:01", "fact.inverter.frequency", resourceDriverBind, resourceDimension("dimension.inverter", "resource:inverter:01")),
		resourceFact("candidate:frequency:03", "fact.inverter.frequency", "binding:pv:03", resourceDimension("dimension.inverter", "resource:inverter:01")),
	}
	after := applyResourceBatch(t, kernel, batch)
	if !reflect.DeepEqual(after.Resources, before.Resources) {
		t.Fatal("a second binding changed the resources")
	}
	naming := 0
	for _, link := range after.IdentityLinks {
		if link.Resource != nil && *link.Resource == "resource:inverter:01" {
			naming++
		}
	}
	if naming != 2 {
		t.Fatalf("%d identity links name the inverter, want 2", naming)
	}
}

// Section 11.5: identity links that carry a resource are bounded at 1024 per
// asset; the bound of 128 still applies to links without one.
func TestResourcePublicationIdentityLinkBounds(t *testing.T) {
	links := func(withResource, without int) []IdentityLink {
		result := make([]IdentityLink, 0, withResource+without)
		for i := 0; i < withResource+without; i++ {
			resource := ResourceID("")
			if i < withResource {
				resource = "resource:inverter:01"
			}
			result = append(result, resourceLink(NativeBindingID(fmt.Sprintf("binding:%05d", i)), resource))
		}
		return result
	}
	base := graphSnapshot(t, false)
	cases := []struct {
		withResource, without int
		bounded               bool
	}{
		{1024, 0, false}, {1025, 0, true}, {0, 128, false}, {0, 129, true}, {1024, 128, false}, {1, 129, true}, {1025, 1, true},
	}
	for _, tc := range cases {
		snapshot := base
		snapshot.IdentityLinks = links(tc.withResource, tc.without)
		for name, err := range map[string]error{"snapshot": snapshot.Validate(), "post-state": snapshot.publicationContentErrors()} {
			if got := ErrorIdentifier(err) == BoundsExceeded; got != tc.bounded {
				t.Errorf("%s with %d links carrying a resource and %d without: %v", name, tc.withResource, tc.without, err)
			}
		}
	}
}

// Section 11.5: at most 4096 resources per asset; one more is rejected and
// never truncated.
func TestResourcePublicationResourceBound(t *testing.T) {
	resources := func(n int) []Resource {
		result := make([]Resource, n)
		for i := range result {
			result[i] = operatorResource(ResourceID(fmt.Sprintf("resource:%05d", i)), "resource.pv_system", AnchorFunction, fmt.Sprintf("system-%05d", i), nil)
		}
		return result
	}
	kernel := newResourceKernel(t, nil)
	over := resourceConfigBatch("1", "0")
	over.ResourceUpserts = resources(maxSnapshotResources + 1)
	rejectResourceBatch(t, kernel, over, BoundsExceeded)
	at := resourceConfigBatch("1", "0")
	at.ResourceUpserts = resources(maxSnapshotResources)
	if snapshot := applyResourceBatch(t, kernel, at); len(snapshot.Resources) != maxSnapshotResources {
		t.Fatalf("got %d resources", len(snapshot.Resources))
	}
}

// The structure revision is set on the first accepted resource change and
// increments on every later one; a snapshot without graph content keeps its
// bytes.
func TestResourcePublicationStructureRevision(t *testing.T) {
	kernel := newResourceKernel(t, nil)
	config := resourceConfigBatch("1", "0")
	config.ResourceUpserts = []Resource{operatorResource("resource:inverter:01", "resource.inverter", AnchorEquipment, "inverter-1", nil)}
	if got := applyResourceBatch(t, kernel, config).Revisions.Structure; got != "1" {
		t.Fatalf("first resource change: structure %q, want 1", got)
	}
	driver := resourceDriverBatch("1", "1")
	if got := applyResourceBatch(t, kernel, driver).Revisions.Structure; got != "1" {
		t.Fatalf("batch without a resource change: structure %q, want 1", got)
	}
	again := resourceConfigBatch("2", "2")
	again.ResourceUpserts = []Resource{operatorResource("resource:inverter:01", "resource.inverter", AnchorEquipment, "inverter-1", nil)}
	if got := applyResourceBatch(t, kernel, again).Revisions.Structure; got != "1" {
		t.Fatalf("unchanged resource upsert: structure %q, want 1", got)
	}
	changed := currentResource(t, kernel, "resource:inverter:01")
	changed.Anchor.Evidence, changed.Revision = []EvidenceRef{publicEvidence("6"), publicEvidence("7")}, "2"
	next := resourceConfigBatch("3", "3")
	next.ResourceUpserts = []Resource{changed}
	if got := applyResourceBatch(t, kernel, next).Revisions.Structure; got != "2" {
		t.Fatalf("second resource change: structure %q, want 2", got)
	}

	// Without graph content an enforced kernel publishes the bytes of a
	// kernel without the catalog.
	plainBatch := func() PublicationBatch {
		batch := publicationBatch("asset:pv", resourceDriverSource, resourceDriverEpoch, "1", "1", "0")
		batch.SourceUpserts = []SourceDescriptor{publicationSource(resourceDriverSource, resourceDriverEpoch)}
		batch.BindingUpserts = []NativeBinding{publicationBinding("asset:pv", resourceDriverSource, resourceDriverEpoch, resourceDriverBind, "1")}
		batch.IdentityLinkUpserts = []IdentityLink{resourceLink(resourceDriverBind, "")}
		batch.FactUpserts = []FactCandidate{resourceFact("candidate:test:01", "fact.test", resourceDriverBind)}
		sealPublicationBatch(t, &batch)
		return batch
	}
	plain, err := NewPublicationKernel("asset:pv", &countingValidator{pack: resourceTestPack, index: resourceTestIndex()})
	if err != nil {
		t.Fatal(err)
	}
	_, want, err := plain.Apply(plainBatch(), publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := newResourceKernel(t, nil).Apply(plainBatch(), publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || bytes.Contains(got, []byte(`"structure"`)) {
		t.Fatalf("snapshot without graph content changed bytes:\n got %s\nwant %s", got, want)
	}
}

// A graph-enforced registry passes the catalog's fact subject to a pack
// validator that validates facts under enforcement; without the catalog the
// same validator is called as before.
func TestResourcePublicationPackValidatorsLearnEnforcement(t *testing.T) {
	key := FactKey{PackID: resourceTestPack.ID, PackVersion: resourceTestPack.Version, FactID: "fact.input.power", Dimensions: []Dimension{resourceDimension("dimension.input", "resource:dc-input:01")}}
	value := booleanValue(true)

	enforced := &graphShapeValidator{countingValidator: countingValidator{pack: resourceTestPack, index: resourceTestIndex()}}
	registry, err := NewGraphRegistry(newResourceTestCatalog(t), enforced)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateFact(key, &value); err != nil {
		t.Fatal(err)
	}
	want := GraphFactSubject{Disposition: GraphMember, Resolved: true, Dimension: "dimension.input", Kind: "resource.dc_input"}
	if len(enforced.subjects) != 1 || !reflect.DeepEqual(enforced.subjects[0], want) || enforced.factCalls != 0 {
		t.Fatalf("enforced registry passed %+v and %d plain calls", enforced.subjects, enforced.factCalls)
	}
	unknown := key
	unknown.FactID = "fact.unknown.temperature"
	if err := registry.ValidateFact(unknown, &value); err != nil {
		t.Fatal(err)
	}
	if got := enforced.subjects[1]; got.Resolved || got.Dimension != "" || !reflect.DeepEqual(got.Qualifiers, []DefinitionID{"dimension.temperature"}) {
		t.Fatalf("unknown fact subject %+v", got)
	}
	missing := key
	missing.FactID = "fact.other"
	requireID(t, registry.ValidateFact(missing, &value), MembershipMissing)

	plain := &graphShapeValidator{countingValidator: countingValidator{pack: resourceTestPack, index: resourceTestIndex()}}
	without, err := NewRegistry(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := without.ValidateFact(missing, &value); err != nil {
		t.Fatal(err)
	}
	if len(plain.subjects) != 0 || plain.factCalls != 1 {
		t.Fatalf("registry without the catalog passed %+v and %d plain calls", plain.subjects, plain.factCalls)
	}

	// A catalog without the resource lookups keeps refusing graph members.
	limited := coveringCatalog(t, nil)
	limited.covered = map[PackRef]DefinitionIndex{resourceTestPack: resourceTestIndex()}
	kernel, err := NewGraphPublicationKernel("asset:pv", limited, &countingValidator{pack: resourceTestPack, index: resourceTestIndex()})
	if err != nil {
		t.Fatal(err)
	}
	batch := resourceConfigBatch("1", "0")
	batch.ResourceUpserts = []Resource{operatorResource("resource:inverter:01", "resource.inverter", AnchorEquipment, "inverter-1", nil)}
	rejectResourceBatch(t, kernel, batch, MembershipMissing)
	if limited.lookups != 0 {
		t.Fatalf("catalog without resource lookups was read %d times", limited.lookups)
	}
}

func TestResourcePublicationClassSubject(t *testing.T) {
	catalog := newResourceTestCatalog(t)
	catalog.serviceClasses = map[DefinitionID][]AnchorClass{"service.unit": {AnchorComponent}}
	index := resourceTestIndex()
	index.Services = append(append([]DefinitionRef{}, index.Services...), resourceTestRef("service.unit"))
	catalog.index = index
	kernel, err := NewGraphPublicationKernel("asset:pv", catalog, &countingValidator{pack: resourceTestPack, index: index})
	if err != nil {
		t.Fatal(err)
	}
	config := resourceConfigBatch("1", "0")
	config.ResourceUpserts = []Resource{
		operatorResource("resource:inverter:01", "resource.inverter", AnchorEquipment, "inverter-1", nil),
		operatorResource("resource:pv-system:01", "resource.pv_system", AnchorFunction, "pv-system-1", nil),
	}
	applyResourceBatch(t, kernel, config)
	driver := resourceDriverBatch("1", "1")
	driver.ResourceUpserts = []Resource{
		nativeSlotResource("resource:dc-input:01", "resource.dc_input", AnchorComponent, "resource:inverter:01", "1"),
		nativeSlotResource("resource:dc-input:02", "resource.dc_input", AnchorComponent, "resource:inverter:01", "2"),
	}
	applyResourceBatch(t, kernel, driver)
	component := resourceService("service:unit:component", "resource:dc-input:01")
	component.Definition.ID = "service.unit"
	accept := resourceDriverBatch("2", "2")
	accept.ServiceUpserts = []ServiceInstance{component}
	applyResourceBatch(t, kernel, accept)
	equipment := component
	equipment.InstanceID = "service:unit:equipment"
	equipment.Revision = "1"
	equipment.Resource = resourceID("resource:inverter:01")
	refuse := resourceDriverBatch("3", "3")
	refuse.ServiceUpserts = []ServiceInstance{equipment}
	rejectResourceBatch(t, kernel, refuse, IdentityDrift)

	kind := resourceService("service:kind:inverter", "resource:inverter:01")
	kindBatch := resourceDriverBatch("3", "3")
	kindBatch.ServiceUpserts = []ServiceInstance{kind}
	applyResourceBatch(t, kernel, kindBatch)
	other := kind
	other.InstanceID = "service:kind:component"
	other.Resource = resourceID("resource:dc-input:01")
	otherBatch := resourceDriverBatch("4", "4")
	otherBatch.ServiceUpserts = []ServiceInstance{other}
	rejectResourceBatch(t, kernel, otherBatch, IdentityDrift)
}

func resourceID(id string) *ResourceID {
	value := ResourceID(id)
	return &value
}
