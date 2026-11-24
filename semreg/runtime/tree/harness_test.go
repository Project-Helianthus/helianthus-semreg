package tree_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

const (
	installation = "installation.test"
	bridge       = driver.BridgeID("bridge.test")
	incarnation  = "incarnation.one"

	kindSite      = "resource.site"
	kindGrid      = "resource.grid_connection"
	kindEquipment = "resource.equipment"
	lensHierarchy = "lens.installation_hierarchy"
)

var relSiteGrid = semreg.DefinitionRef{
	Pack: semreg.PackRef{ID: "helianthus.pack.infrastructure", Version: "1.0.0"}, ID: "infrastructure.relation.site_grid_connection", Version: "1.0.0",
}

func evidence(label string) semreg.EvidenceRef {
	sum := sha256.Sum256([]byte("tree evidence " + label))
	return semreg.EvidenceRef{
		Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Contract: "test.contract/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone,
	}
}

func operatorOrigin(label string) semreg.OriginRef {
	return semreg.OriginRef{OriginID: semreg.OriginID("origin:operator:" + label), Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{evidence("operator")}}
}

func nativeOrigin(asset semreg.AssetID, label string) semreg.OriginRef {
	source, epoch, binding := sourceOf(asset)
	return semreg.OriginRef{OriginID: semreg.OriginID("origin:native:" + label), Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []semreg.EvidenceRef{evidence("native")}}
}

func sourceOf(asset semreg.AssetID) (semreg.SourceID, semreg.SourceEpochID, semreg.NativeBindingID) {
	return semreg.SourceID("source:" + asset), semreg.SourceEpochID("epoch:" + asset), semreg.NativeBindingID("binding:" + asset)
}

func text(s string) *string { return &s }

func slotResource(id, kind string, class semreg.AnchorClass, slot string) semreg.Resource {
	anchor := semreg.ResourceAnchor{Class: class, Basis: semreg.AnchorOperatorSlot, Slot: text(slot), Evidence: []semreg.EvidenceRef{evidence("anchor")}}
	return semreg.Resource{ResourceID: semreg.ResourceID(id), Kind: semreg.DefinitionID(kind), Anchor: anchor, State: semreg.ResourceCurrent, Origin: operatorOrigin(id), Revision: "1"}
}

func qualified(from, to string) semreg.RelationInstance {
	return semreg.RelationInstance{
		Definition: relSiteGrid, From: semreg.ResourceID(from), To: semreg.ResourceID(to), State: semreg.LinkQualified,
		BasisClass: semreg.RelationConfiguration, Basis: []semreg.EvidenceRef{evidence("relation")},
		Origin: operatorOrigin(from + "-" + to), Revision: "1",
	}
}

func factKey(fact, device string) semreg.FactKey {
	return semreg.FactKey{
		PackID: "pack.test", PackVersion: "1.0.0", FactID: semreg.DefinitionID(fact),
		Dimensions: []semreg.Dimension{{ID: "dimension.device", Value: semreg.Value{Kind: semreg.ValueText, Text: text(device)}}},
	}
}

type assetSpec struct {
	id        semreg.AssetID
	resources []semreg.Resource
	relations []semreg.RelationInstance
	facts     []semreg.FactKey
}

type jsonObject map[string]any

func (jsonObject) Validate() error { return nil }

func (a assetSpec) snapshot(t *testing.T) semreg.Snapshot {
	t.Helper()
	source, epoch, binding := sourceOf(a.id)
	generation := semreg.Uint64("1")
	received := semreg.TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"}
	evaluated := semreg.TimePoint{UnixNanoseconds: "101", ClockID: "clock.utc", UncertaintyNS: "0"}
	s := semreg.Snapshot{
		Contract: semreg.ContractKernelV1, AssetID: a.id,
		Revisions:         semreg.RevisionVector{Semantic: "1", Identity: "1", Facts: "1", Services: "1", Capabilities: "1", Structure: "1"},
		EvaluatedAt:       evaluated,
		EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:tree", Nanoseconds: "6"},
		Sources: []semreg.SourceDescriptor{{
			SourceID: source, SourceEpochID: epoch, ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "release-1",
			RegistryEvidence: evidence("registry"), StartedAt: received, State: semreg.SourceCurrent, Revision: "1",
		}},
		Bindings:      []semreg.NativeBinding{{BindingID: binding, AssetID: a.id, SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, NativeResource: evidence("native resource"), State: semreg.BindingCurrent, Revision: "1"}},
		IdentityLinks: []semreg.IdentityLink{}, Facts: []semreg.FactEnvelope{}, Services: []semreg.ServiceInstance{}, Capabilities: []semreg.CapabilityInstance{}, Fences: []semreg.GenerationFence{},
		Cursors: []semreg.PublicationCursor{{SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, LastSequence: "1", LastBatchDigest: evidence("batch").Digest}},
	}
	for i, key := range a.facts {
		candidate := semreg.FactCandidate{
			CandidateID: semreg.CandidateID(fmt.Sprintf("candidate:%s:%02d", a.id, i)), Key: key, Value: &semreg.Value{Kind: semreg.ValueBoolean, Boolean: new(bool)},
			Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}},
			Times: semreg.Times{
				ReceivedAt: received, ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:tree", Nanoseconds: "5"},
				EvaluatedAt: evaluated, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:tree", Nanoseconds: "6"},
			},
			FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:tree", Version: "1.0.0", FreshForNS: "30", RetainForNS: "300", MaxWallUncertaintyNS: "1"},
			BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
			Origin: nativeOrigin(a.id, fmt.Sprintf("fact-%02d", i)), Evidence: []semreg.EvidenceRef{evidence("fact")}, Revision: "1",
		}
		s.Facts = append(s.Facts, semreg.FactEnvelope{AssetID: a.id, Key: key, Candidates: []semreg.FactCandidate{candidate}, Conflicts: []semreg.Conflict{}, Revision: "1"})
	}
	sort.Slice(s.Facts, func(i, j int) bool {
		left, _ := semreg.CanonicalJSON(s.Facts[i].Key)
		right, _ := semreg.CanonicalJSON(s.Facts[j].Key)
		return string(left) < string(right)
	})
	s.Resources = append([]semreg.Resource(nil), a.resources...)
	sort.Slice(s.Resources, func(i, j int) bool { return s.Resources[i].ResourceID < s.Resources[j].ResourceID })
	s.Relations = append([]semreg.RelationInstance(nil), a.relations...)
	sort.Slice(s.Relations, func(i, j int) bool {
		return string(s.Relations[i].From)+"\x00"+string(s.Relations[i].To) < string(s.Relations[j].From)+"\x00"+string(s.Relations[j].To)
	})
	if len(s.Relations) == 0 {
		s.Relations = nil
	}
	raw, err := json.Marshal(&s)
	if err != nil {
		t.Fatal(err)
	}
	var object jsonObject
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
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
	if err := s.Validate(); err != nil {
		t.Fatalf("snapshot of %s is invalid: %v", a.id, err)
	}
	return s
}

// siteAsset is one asset with a site and n grid connections under it.
func siteAsset(id semreg.AssetID, site string, grids ...string) assetSpec {
	a := assetSpec{id: id, resources: []semreg.Resource{slotResource(site, kindSite, semreg.AnchorFunction, "slot-"+site)}}
	for _, g := range grids {
		a.resources = append(a.resources, slotResource(g, kindGrid, semreg.AnchorFunction, "slot-"+g))
		a.relations = append(a.relations, qualified(site, g))
	}
	return a
}

func equipmentAsset(id semreg.AssetID, resource string) assetSpec {
	return assetSpec{id: id, resources: []semreg.Resource{slotResource(resource, kindEquipment, semreg.AnchorEquipment, "slot-"+resource)}}
}

// fakeCuts is a synthetic cut source over the real embedded catalog and a real
// CutStore, so the frozen graph functions run unchanged.
type fakeCuts struct {
	t        *testing.T
	mu       sync.Mutex
	catalog  *graph.Catalog
	store    *graph.CutStore
	now      time.Time
	assets   map[semreg.AssetID]assetSpec
	stopped  map[semreg.AssetID]bool
	declared map[semreg.AssetID]graph.NotIncludedReason
	eval     semreg.EvaluationContext
	views    map[semreg.Digest]tree.CutView
	expired  map[semreg.Digest]time.Time
	// demoted is when a cut stopped being the newest; the source stops
	// retaining it graph.CutRetention later, as the retained store does.
	demoted  map[semreg.Digest]time.Time
	history  []driver.SemanticCut
	resync   bool
	captures int
}

func newFakeCuts(t *testing.T) *fakeCuts {
	t.Helper()
	catalog, err := graph.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeCuts{
		t: t, catalog: catalog, now: time.Unix(1_700_000_000, 0).UTC(),
		assets: map[semreg.AssetID]assetSpec{}, stopped: map[semreg.AssetID]bool{}, declared: map[semreg.AssetID]graph.NotIncludedReason{},
		views: map[semreg.Digest]tree.CutView{}, expired: map[semreg.Digest]time.Time{}, demoted: map[semreg.Digest]time.Time{},
		eval: semreg.EvaluationContext{
			EvaluatedAt:       semreg.TimePoint{UnixNanoseconds: "130", ClockID: "clock.utc", UncertaintyNS: "0"},
			EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:tree", Nanoseconds: "20"},
		},
	}
	f.store = graph.NewCutStore(func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now })
	return f
}

func (f *fakeCuts) add(a assetSpec) { f.mu.Lock(); f.assets[a.id] = a; f.mu.Unlock() }

func (f *fakeCuts) advance(d time.Duration) { f.mu.Lock(); f.now = f.now.Add(d); f.mu.Unlock() }

func (f *fakeCuts) advanceEvaluation() {
	f.mu.Lock()
	defer f.mu.Unlock()
	next, _ := strconv.Atoi(string(f.eval.EvaluateMonotonic.Nanoseconds))
	f.eval.EvaluateMonotonic.Nanoseconds = semreg.Uint64(strconv.Itoa(next + 10))
}

func (f *fakeCuts) build(filter driver.Filter) (tree.CutView, error) {
	f.mu.Lock()
	ids := map[semreg.AssetID]struct{}{}
	if len(filter.Assets) == 0 {
		for id := range f.assets {
			ids[id] = struct{}{}
		}
		for id := range f.declared {
			ids[id] = struct{}{}
		}
	} else {
		for _, id := range filter.Assets {
			ids[id] = struct{}{}
		}
	}
	var snaps []semreg.Snapshot
	var omitted []graph.NotIncludedAsset
	for _, id := range sortedAssets(ids) {
		spec, known := f.assets[id]
		switch {
		case known && !f.stopped[id]:
			snaps = append(snaps, spec.snapshot(f.t))
		case f.stopped[id]:
			omitted = append(omitted, graph.NotIncludedAsset{AssetID: id, Reason: graph.NotIncludedSourceUnavailable})
		case f.declared[id] != "":
			omitted = append(omitted, graph.NotIncludedAsset{AssetID: id, Reason: f.declared[id]})
		}
	}
	completeness := graph.CutComplete
	if len(omitted) > 0 {
		completeness = graph.CutPartial
	}
	eval := f.eval
	f.mu.Unlock()
	cut, err := graph.AssembleCut(snaps, f.catalog, completeness)
	if err != nil {
		return tree.CutView{}, err
	}
	if err := f.store.Retain(cut, snaps, f.catalog); err != nil {
		return tree.CutView{}, err
	}
	f.mu.Lock()
	f.captures++
	position := strconv.Itoa(f.captures)
	f.mu.Unlock()
	ref := graph.CutRef{CutID: cut.ID(), Completeness: cut.Completeness(), Members: cut.Members(), NotIncluded: omitted}
	view := tree.CutView{
		Ref: ref, Cut: cut, Members: snaps, Store: f.store, Catalog: f.catalog, Evaluation: eval,
		Cursor: driver.Cursor{Incarnation: "semantic.one", Position: semreg.Uint64(position)},
	}
	f.mu.Lock()
	for other := range f.views {
		if _, ok := f.demoted[other]; !ok && other != cut.ID() {
			f.demoted[other] = f.now
		}
	}
	delete(f.demoted, cut.ID())
	f.views[cut.ID()] = view
	f.mu.Unlock()
	return view, nil
}

func sortedAssets(m map[semreg.AssetID]struct{}) []semreg.AssetID {
	out := make([]semreg.AssetID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (f *fakeCuts) Capture(_ context.Context, filter driver.Filter) (tree.CutView, error) {
	return f.build(filter)
}

func (f *fakeCuts) Lookup(_ context.Context, ref graph.CutRef) (tree.CutView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if at, ok := f.expired[ref.CutID]; ok {
		return tree.CutView{}, &tree.ExpiredError{CutID: ref.CutID, ExpiredAt: at}
	}
	if at, ok := f.demoted[ref.CutID]; ok && !f.now.Before(at.Add(graph.CutRetention)) {
		return tree.CutView{}, &tree.ExpiredError{CutID: ref.CutID, ExpiredAt: at.Add(graph.CutRetention)}
	}
	view, ok := f.views[ref.CutID]
	if !ok {
		return tree.CutView{}, &semreg.Error{ID: semreg.RevisionConflict, Detail: "cut is not retained"}
	}
	return view, nil
}

func (f *fakeCuts) Resume(_ context.Context, cursor driver.Cursor, _ driver.Filter) (driver.Changes, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := driver.Cursor{Incarnation: "semantic.one", Position: semreg.Uint64(strconv.Itoa(f.captures))}
	if f.resync {
		return driver.Changes{Cursor: next, Resync: true}, nil
	}
	return driver.Changes{Cursor: next}, nil
}

// fakeLife is a lifecycle source the tests drive.
type fakeLife struct {
	mu     sync.Mutex
	states map[tree.InstanceRef]tree.InstanceState
	fences []func(tree.Fence)
}

func newFakeLife() *fakeLife { return &fakeLife{states: map[tree.InstanceRef]tree.InstanceState{}} }

func (l *fakeLife) State(ref tree.InstanceRef) (tree.InstanceState, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.states[ref]
	if !ok {
		return tree.InstanceState{Name: "unknown"}, nil
	}
	return state, nil
}

func (l *fakeLife) OnFence(fn func(tree.Fence)) func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fences = append(l.fences, fn)
	return func() {}
}

func (l *fakeLife) set(ref tree.InstanceRef, name string, generation uint64) {
	l.mu.Lock()
	l.states[ref] = tree.InstanceState{Name: name, Generation: generation}
	l.mu.Unlock()
}

func (l *fakeLife) fence(ref tree.InstanceRef, generation uint64, name string) {
	l.set(ref, name, generation)
	l.mu.Lock()
	fns := append([]func(tree.Fence){}, l.fences...)
	l.mu.Unlock()
	for _, fn := range fns {
		fn(tree.Fence{Instance: ref, Generation: generation})
	}
}

// fakeOps is the dispatcher port: it counts what reaches it.
type fakeOps struct {
	mu       sync.Mutex
	prepares int
	calls    int
	outcomes int
	result   driver.OperationResult
}

func (o *fakeOps) Prepare(_ context.Context, _ graph.CutRef, _ semreg.DefinitionRef, input tree.Input) (tree.PortPrepared, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.prepares++
	return tree.PortPrepared{ID: tree.PreparedID("port-prepared-" + strconv.Itoa(o.prepares)), Request: *input.Request}, nil
}

func (o *fakeOps) Call(_ context.Context, prepared tree.PortPrepared, _ tree.CallOptions) (tree.PortReceipt, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	result := o.result
	return tree.PortReceipt{ID: tree.ReceiptID("port-receipt-" + strconv.Itoa(o.calls)), Result: &result}, nil
}

func (o *fakeOps) Outcome(_ context.Context, id tree.ReceiptID) (driver.OperationResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.outcomes++
	return o.result, nil
}

// world is one tree over the fakes.
type world struct {
	t    *testing.T
	cuts *fakeCuts
	life *fakeLife
	ops  *fakeOps
	tree *tree.Tree
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, cuts: newFakeCuts(t), life: newFakeLife(), ops: &fakeOps{}}
	tr, err := tree.New(tree.Config{
		Registry:   tree.RegistryConfig{Installation: installation, Bridge: bridge, Incarnation: incarnation, Lifecycle: w.life},
		Cuts:       w.cuts,
		Operations: w.ops,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.tree = tr
	return w
}

func (w *world) reserve(owner, namespace string, instances ...string) {
	w.t.Helper()
	keys := make([]driver.InstanceKey, len(instances))
	for i, instance := range instances {
		keys[i] = driver.InstanceKey(instance)
	}
	if err := w.tree.Registry.Reserve(driver.Namespace{Owner: driver.DriverKind(owner), Key: driver.NamespaceID(namespace)}, keys); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) bind(namespace, instance string, generation uint64) *tree.InstanceRegistry {
	w.t.Helper()
	b, err := w.tree.Registry.Bind(driver.NamespaceID(namespace), driver.InstanceKey(instance), generation)
	if err != nil {
		w.t.Fatal(err)
	}
	return b
}

func (w *world) capture() *tree.TreeCut {
	w.t.Helper()
	cut, err := w.tree.Capture(context.Background(), tree.CutOptions{})
	if err != nil {
		w.t.Fatal(err)
	}
	return cut
}

func memberSpec(local string) driver.MemberSpec {
	return driver.MemberSpec{Local: driver.MemberID(local), Schema: "schema.test", Units: "unit.test", Mode: "retained", Scope: "scope.test"}
}

func methodSpec(local string) driver.MethodSpec {
	return driver.MethodSpec{Local: driver.MemberID(local), Schema: "schema.test", Scope: "scope.test"}
}

func nodeSpec(local string, parent *driver.NodeRef) driver.NodeSpec {
	return driver.NodeSpec{Parent: parent, Kind: "node.test", Local: driver.NodeID(local), Contract: "contract.test"}
}

func nativeRef(t *testing.T, ns driver.NamespaceID, node tree.NodePath, facet tree.Facet, local string) tree.Ref {
	t.Helper()
	segment, err := tree.NativeSegment(ns, driver.MemberID(local))
	if err != nil {
		t.Fatal(err)
	}
	return tree.Ref(tree.Key{Node: node, Facet: facet, Domain: tree.KeywordNative, Segment: segment}.String())
}

func instancePath(owner, instance string) tree.NodePath {
	return tree.NodePath{Installation: installation, Driver: driver.DriverKind(owner), Instance: driver.InstanceKey(instance)}
}

func nodeRefOf(path tree.NodePath) driver.NodeRef {
	return driver.NodeRef{Bridge: bridge, ID: path.NodeID()}
}

func mustCode(t *testing.T, err error, id semreg.ErrorID) *semreg.Error {
	t.Helper()
	var coded *semreg.Error
	if err == nil {
		t.Fatalf("error is nil, want %s", id)
	}
	coded, ok := err.(*semreg.Error)
	if !ok || coded.ID != id {
		t.Fatalf("error = %v, want %s", err, id)
	}
	return coded
}

func quoted(s string) string { return strconv.Quote(s) }

var ctx = context.Background()
