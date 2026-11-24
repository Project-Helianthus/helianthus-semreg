package tree_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestNativeReceiptKeepsItsOwnResultBuffer(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	w.life.set(tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}, "running", 1)
	b := w.bind("ns_a", "inst.1", 1)
	buffer := []byte("paired")
	if err := b.Method(b.Root(), methodSpec("pairing"), func(context.Context, []byte) ([]byte, error) {
		return buffer, nil
	}); err != nil {
		t.Fatal(err)
	}
	cut := w.capture()
	method := nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordMethods, "pairing")
	prepared, err := w.tree.Prepare(ctx, cut.ID(), method, tree.Input{})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := w.tree.Call(ctx, prepared, tree.CallOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// The method reuses its buffer after the call returned.
	copy(buffer, "XXXXXX")
	outcome, err := w.tree.Outcome(ctx, receipt.ID)
	if err != nil || string(outcome.Native) != "paired" {
		t.Fatalf("outcome after the method reused its buffer = %q, %v", outcome.Native, err)
	}
	// A caller writes into the slice it was handed.
	outcome.Native[0] = 'Z'
	again, err := w.tree.Outcome(ctx, receipt.ID)
	if err != nil || string(again.Native) != "paired" {
		t.Fatalf("outcome after the caller edited its copy = %q, %v", again.Native, err)
	}
}

func TestCaptureKeepsTheSnapshotsTheSourceHandedOver(t *testing.T) {
	w := scopedWorld(t)
	cut := w.capture()
	index := tree.Ref(cutIndexRef(t, cut, "resources"))
	before, err := cut.Read(ctx, tree.AttrRef(index), tree.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	lensBefore, err := cut.RetrieveLens(ctx, lensRequest(5))
	if err != nil {
		t.Fatal(err)
	}

	// The source edits, in place, the snapshots it already handed to Capture.
	edited := 0
	for _, view := range w.cuts.views {
		for i := range view.Members {
			for j := range view.Members[i].Resources {
				view.Members[i].Resources[j].State = semreg.ResourceRetired
				edited++
			}
		}
	}
	if edited == 0 {
		t.Fatal("the test edited nothing")
	}
	after, err := cut.Read(ctx, tree.AttrRef(index), tree.ReadOptions{})
	if err != nil || string(after.Encoded) != string(before.Encoded) {
		t.Fatalf("a semantic read followed the source's edit: %s vs %s, %v", after.Encoded, before.Encoded, err)
	}
	lensAfter, err := cut.RetrieveLens(ctx, lensRequest(5))
	if err != nil || !reflect.DeepEqual(lensAfter, lensBefore) {
		t.Fatalf("a lens read followed the source's edit: %v", err)
	}
}

func cutIndexRef(t *testing.T, cut *tree.TreeCut, category string) string {
	t.Helper()
	segment, err := tree.EncodeSegment(tree.IndexPart, category)
	if err != nil {
		t.Fatal(err)
	}
	return tree.Key{Node: tree.NodePath{Installation: installation}, Facet: tree.KeywordAttributes, Domain: tree.KeywordSem, Segment: segment}.String()
}

func limits(label string) *driver.ValueLimits {
	one := semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.test"}
	nine := semreg.Quantity{Number: semreg.Decimal{Coefficient: "9"}, Unit: "unit.test"}
	return &driver.ValueLimits{Min: &one, Max: &nine, Origin: driver.LimitSpecification, Evidence: []semreg.EvidenceRef{evidence(label)}}
}

// describedPair registers one node with a subject and one member with limits and
// returns the registered input specs, the tree and both references.
func describedPair(t *testing.T) (*world, driver.NodeSpec, driver.MemberSpec, *tree.TreeCut, tree.Ref, tree.Ref) {
	t.Helper()
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	w.life.set(tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}, "running", 1)
	b := w.bind("ns_a", "inst.1", 1)
	subject := nodeRefOf(instancePath("kind_a", "inst.1"))
	node := nodeSpec("device.1", nil)
	node.Subject = &subject
	device, err := b.Node(node)
	if err != nil {
		t.Fatal(err)
	}
	member := memberSpec("member.1")
	member.Bounds = limits("registered")
	if err := b.ObserveMember(device, member, func(context.Context) (tree.Observation, error) {
		return tree.Observation{Encoded: []byte("v")}, nil
	}); err != nil {
		t.Fatal(err)
	}
	devicePath := mustPath(t, device)
	return w, node, member, w.capture(), tree.Ref(devicePath.String()), nativeRef(t, "ns_a", devicePath, tree.KeywordAttributes, "member.1")
}

func listed(t *testing.T, cut *tree.TreeCut, node driver.NodeRef) []tree.Descriptor {
	t.Helper()
	entries, err := cut.List(ctx, node, tree.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	return entries.Entries
}

func TestDescribeAndListHandOutNoStoredPointer(t *testing.T) {
	w, _, _, cut, nodeRef, memberRef := describedPair(t)
	wantBounds := limits("registered")
	wantSubject := nodeRefOf(instancePath("kind_a", "inst.1"))

	corrupt := func(d tree.Descriptor) {
		if d.Subject != nil {
			d.Subject.ID = "res:edited"
		}
		if d.Bounds != nil {
			d.Bounds.Min.Number.Coefficient = "-5"
			d.Bounds.Evidence[0].Owner = "owner.edited"
			d.Bounds.Origin = "edited"
		}
	}
	node, err := cut.Describe(ctx, nodeRef)
	if err != nil || node.Subject == nil {
		t.Fatalf("describe node = %+v, %v", node, err)
	}
	member, err := cut.Describe(ctx, memberRef)
	if err != nil || member.Bounds == nil {
		t.Fatalf("describe member = %+v, %v", member, err)
	}
	corrupt(node)
	corrupt(member)
	for _, d := range listed(t, cut, nodeRefOf(instancePath("kind_a", "inst.1"))) {
		corrupt(d)
	}
	devicePath, _ := tree.ParseNodePath(string(nodeRef))
	for _, d := range listed(t, cut, nodeRefOf(devicePath)) {
		corrupt(d)
	}

	for _, d := range []struct {
		ref  tree.Ref
		want func(tree.Descriptor) bool
	}{
		{nodeRef, func(d tree.Descriptor) bool { return d.Subject != nil && *d.Subject == wantSubject }},
		{memberRef, func(d tree.Descriptor) bool { return reflect.DeepEqual(d.Bounds, wantBounds) }},
	} {
		for name, read := range map[string]*tree.TreeCut{"the cut taken before": cut, "a later cut": w.capture()} {
			got, err := read.Describe(ctx, d.ref)
			if err != nil || !d.want(got) {
				t.Fatalf("describe %s after a consumer edited returned descriptors (%s) = %+v, %v", d.ref, name, got, err)
			}
		}
	}
}

func TestRegistrationKeepsNoPointerOfTheDriversSpec(t *testing.T) {
	w, node, member, cut, nodeRef, memberRef := describedPair(t)
	wantBounds := limits("registered")
	wantSubject := nodeRefOf(instancePath("kind_a", "inst.1"))

	// The driver edits the specs it registered, through the pointers it kept.
	node.Subject.ID = "res:edited"
	member.Bounds.Max.Number.Coefficient = "99"
	member.Bounds.Evidence[0].Owner = "owner.edited"

	for _, read := range []*tree.TreeCut{cut, w.capture()} {
		gotNode, err := read.Describe(ctx, nodeRef)
		if err != nil || gotNode.Subject == nil || *gotNode.Subject != wantSubject {
			t.Fatalf("node subject after the driver edited its spec = %+v, %v", gotNode.Subject, err)
		}
		gotMember, err := read.Describe(ctx, memberRef)
		if err != nil || !reflect.DeepEqual(gotMember.Bounds, wantBounds) {
			t.Fatalf("member bounds after the driver edited its spec = %+v, %v", gotMember.Bounds, err)
		}
	}
}

// editingCuts edits the filter slice it is handed, as a source that keeps or
// reuses its argument would.
type editingCuts struct{ *fakeCuts }

func (e editingCuts) Capture(c context.Context, filter driver.Filter) (tree.CutView, error) {
	view, err := e.fakeCuts.Capture(c, filter)
	for i := range filter.Assets {
		filter.Assets[i] = "asset.b"
	}
	return view, err
}

func TestScopedInputKeepsItsOwnScope(t *testing.T) {
	w := scopedWorld(t)

	// The caller edits the slice it passed after creating the session.
	scope := []semreg.AssetID{"asset.a"}
	input := w.tree.SemanticInput(driver.Filter{Assets: scope})
	scope[0] = "asset.b"
	cut, err := input.Capture(ctx, driver.Filter{})
	if err != nil || len(cut.Snapshots) != 1 || cut.Snapshots[0].AssetID != "asset.a" {
		t.Fatalf("the session scope followed the caller's edit: %+v, %v", cut.Ref, err)
	}
}

func TestScopedInputHandsTheSourceACopyOfItsScope(t *testing.T) {
	w := scopedWorld(t)
	// The source edits the filter it was handed; the session scope stays.
	tr, err := tree.New(tree.Config{
		Registry:   tree.RegistryConfig{Installation: installation, Bridge: bridge, Incarnation: incarnation, Lifecycle: w.life},
		Cuts:       editingCuts{w.cuts},
		Operations: w.ops,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := tr.SemanticInput(driver.Filter{Assets: []semreg.AssetID{"asset.a"}})
	for i := 0; i < 2; i++ {
		got, err := session.Capture(ctx, driver.Filter{})
		if err != nil || len(got.Snapshots) != 1 || got.Snapshots[0].AssetID != "asset.a" {
			t.Fatalf("capture %d after the source edited its filter: %+v, %v", i, got.Ref, err)
		}
	}
}
