package tree_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func viewMember(namespace, instance string, node driver.NodeRef, local string) driver.MemberKey {
	return driver.MemberKey{Namespace: driver.NamespaceID(namespace), Instance: driver.InstanceKey(instance), Node: driver.NodeID(node.ID), Local: driver.MemberID(local)}
}

func simpleView(name string, key driver.MemberKey) driver.ViewSpec {
	return driver.ViewSpec{Root: name, Records: []driver.ViewRecord{{Name: name, Fields: []driver.ViewField{
		{Name: "reading", Type: driver.ViewType{Name: "Amount", Nullable: true}, Unit: "test units", Member: key},
	}}}}
}

func listViews(t *testing.T, cut *tree.TreeCut, node driver.NodeRef) []tree.Descriptor {
	t.Helper()
	list, err := cut.List(ctx, node, tree.Page{Size: 10, Filter: tree.Filter{Kind: tree.EntryView}})
	if err != nil || list.Cut != cut.ID() || list.Next != "" {
		t.Fatalf("listing views = %+v, %v", list, err)
	}
	return list.Entries
}

func TestViewDeclarationListedDescribedAndReadThroughMember(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	b := w.bind("ns_a", "inst.1", 1)
	var native driver.NativeRegistry = b
	transport := &transportStub{t: t}
	node, err := native.Node(nodeSpec("readings", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := native.Method(node, methodSpec("poll"), func(context.Context, []byte) ([]byte, error) {
		transport.send()
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	for id, value := range map[string]string{"samples": `[{"reading":17}]`, "amount": `17`} {
		if err := native.ReadMember(node, memberSpec(id), func(context.Context) ([]byte, error) {
			return []byte(value), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	before := w.capture()
	spec := simpleView("Sample", viewMember("ns_a", "inst.1", node, "amount"))
	spec.Root = "Report"
	spec.Records = append([]driver.ViewRecord{{Name: "Report", Fields: []driver.ViewField{
		{Name: "samples", Type: driver.ViewType{Name: "Sample", Record: true, List: true}, Member: viewMember("ns_a", "inst.1", node, "samples")},
	}}}, spec.Records...)
	if err := native.View(spec); err != nil {
		t.Fatal(err)
	}
	cut := w.capture()
	if got := listViews(t, before, b.Root()); len(got) != 0 {
		t.Fatalf("earlier cut listed a later declaration: %+v", got)
	}
	listed := listViews(t, cut, b.Root())
	if len(listed) != 1 {
		t.Fatalf("listed %d views; want 1", len(listed))
	}
	ref := listed[0].Ref
	if _, err := before.Describe(ctx, ref); err == nil {
		t.Fatal("earlier cut described a later declaration")
	}
	changes, err := w.tree.Resume(ctx, before.Cursor(), driver.Filter{})
	if err != nil || changes.Resync || len(changes.Native) != 1 || changes.Native[0].Ref != ref || changes.Native[0].Kind != tree.ChangeView {
		t.Fatalf("view change = %+v, %v", changes, err)
	}
	wantSample := tree.ViewField{Name: "samples", Type: spec.Records[0].Fields[0].Type, Member: nativeRef(t, "ns_a", mustPath(t, node), tree.KeywordAttributes, "samples")}
	wantReading := tree.ViewField{Name: "reading", Type: spec.Records[1].Fields[0].Type, Unit: "test units", Member: nativeRef(t, "ns_a", mustPath(t, node), tree.KeywordAttributes, "amount")}
	wantView := &tree.ViewDescription{Root: "Report", Records: []tree.ViewRecord{
		{Name: "Report", Fields: []tree.ViewField{wantSample}},
		{Name: "Sample", Fields: []tree.ViewField{wantReading}},
	}}
	spec.Records[1].Name = "Overwritten"
	spec.Records[0].Fields[0].Type.Name = "Overwritten"
	listed[0].View.Records[1].Fields[0].Member = "overwritten"
	d, err := cut.Describe(ctx, ref)
	if err != nil || d.Kind != tree.EntryView || d.Domain != tree.KeywordNative || d.Namespace != "ns_a" || d.Local != "Report" || !reflect.DeepEqual(d.View, wantView) {
		t.Fatalf("described view = %+v (%+v), %v", d, d.View, err)
	}
	for _, tc := range []struct {
		field tree.ViewField
		value string
	}{{d.View.Records[0].Fields[0], `[{"reading":17}]`}, {d.View.Records[1].Fields[0], `17`}} {
		value, err := cut.Read(ctx, tc.field.Member, tree.ReadOptions{})
		if err != nil || value.Ref != tc.field.Member || value.Cut != cut.ID() || value.Status != tree.StatusLiveOnly || string(value.Encoded) != tc.value || value.Unit != "unit.test" {
			t.Fatalf("read %s = %+v, %v", tc.field.Name, value, err)
		}
	}
	for _, address := range []tree.Ref{ref, wantSample.Member, wantReading.Member} {
		names, err := tree.DriverNames("ns_a", tree.VerbDescribe, string(address))
		if err != nil || names != (tree.DriverName{GraphQL: "ns_a_describe", MCP: "ns_a_v1_describe", Key: string(address)}) {
			t.Fatalf("surface names = %+v, %v", names, err)
		}
	}
	if _, err := tree.DriverNames("ns_a", tree.VerbRead, string(ref)); !errors.Is(err, tree.ErrNoName) {
		t.Fatalf("naming a direct view read = %v", err)
	}
	if _, err := tree.DriverNames("ns_other", tree.VerbDescribe, string(ref)); !errors.Is(err, tree.ErrNoName) {
		t.Fatalf("naming a view in another namespace = %v", err)
	}
	if _, err := tree.DriverNames("ns_a", tree.VerbDescribe, string(ref)+"="); !errors.Is(err, tree.ErrKey) {
		t.Fatalf("naming a non-canonical view key = %v", err)
	}
	if transport.touched != 0 {
		t.Fatal("view read touched the transport")
	}
}

func TestViewRegistrationRejectsUnresolvedOrDuplicateDeclarations(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1", "inst.2")
	w.reserve("kind_a", "ns_other", "inst.1")
	b := w.bind("ns_a", "inst.1", 1)
	sibling := w.bind("ns_a", "inst.2", 1)
	otherNamespace := w.bind("ns_other", "inst.1", 1)
	read := func(context.Context) ([]byte, error) { return nil, nil }
	for _, registry := range []*tree.InstanceRegistry{b, sibling, otherNamespace} {
		if err := registry.ReadMember(registry.Root(), memberSpec("amount"), read); err != nil {
			t.Fatal(err)
		}
	}
	key := viewMember("ns_a", "inst.1", b.Root(), "amount")
	for _, tc := range []struct {
		name string
		edit func(*driver.ViewSpec)
		want error
	}{
		{"namespace", func(s *driver.ViewSpec) { s.Records[0].Fields[0].Member.Namespace = "ns_other" }, tree.ErrUnknownViewMember},
		{"instance", func(s *driver.ViewSpec) { s.Records[0].Fields[0].Member.Instance = "inst.2" }, tree.ErrUnknownViewMember},
		{"node", func(s *driver.ViewSpec) { s.Records[0].Fields[0].Member.Node = driver.NodeID(sibling.Root().ID) }, tree.ErrUnknownViewMember},
		{"local", func(s *driver.ViewSpec) { s.Records[0].Fields[0].Member.Local = "missing" }, tree.ErrUnknownViewMember},
		{"undeclared record", func(s *driver.ViewSpec) { s.Records[0].Fields[0].Type.Record = true }, tree.ErrUnknownViewRecord},
		{"duplicate record", func(s *driver.ViewSpec) { s.Records = append(s.Records, s.Records[0]) }, tree.ErrDuplicate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := simpleView("Sample", key)
			tc.edit(&spec)
			if err := b.View(spec); !errors.Is(err, tc.want) {
				t.Fatalf("registration = %v; want %v", err, tc.want)
			}
		})
	}
	if got := listViews(t, w.capture(), b.Root()); len(got) != 0 {
		t.Fatalf("rejected registrations became visible: %+v", got)
	}
	for _, scope := range []struct {
		b                   *tree.InstanceRegistry
		namespace, instance string
	}{{b, "ns_a", "inst.1"}, {sibling, "ns_a", "inst.2"}, {otherNamespace, "ns_other", "inst.1"}} {
		spec := simpleView("Sample", viewMember(scope.namespace, scope.instance, scope.b.Root(), "amount"))
		if err := scope.b.View(spec); err != nil {
			t.Fatal(err)
		}
	}
	duplicate := simpleView("Other", key)
	duplicate.Records = append(duplicate.Records, driver.ViewRecord{Name: "Sample"})
	if err := b.View(duplicate); !errors.Is(err, tree.ErrDuplicate) {
		t.Fatalf("duplicate record in another view = %v", err)
	}
	if err := b.View(simpleView("Other", key)); err != nil {
		t.Fatalf("different record in same namespace = %v", err)
	}
	cut := w.capture()
	for _, scope := range []struct {
		b    *tree.InstanceRegistry
		want int
	}{{b, 3}, {sibling, 1}} {
		if got := listViews(t, cut, scope.b.Root()); len(got) != scope.want {
			t.Fatalf("view count on %v = %d; want %d", scope.b.Root(), len(got), scope.want)
		}
	}
}

func TestViewKeyScope(t *testing.T) {
	type identity struct{ installation, owner, instance, namespace, root string }
	base := identity{installation, "kind_a", "inst.1", "ns_a", "Sample"}
	register := func(t *testing.T, id identity) tree.Ref {
		t.Helper()
		w := &world{t: t, cuts: newFakeCuts(t), life: newFakeLife(), ops: &fakeOps{}}
		tr, err := tree.New(tree.Config{
			Registry: tree.RegistryConfig{Installation: id.installation, Bridge: bridge, Incarnation: incarnation, Lifecycle: w.life},
			Cuts:     w.cuts, Operations: w.ops,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(tr.Registry.Close)
		w.tree = tr
		w.reserve(id.owner, id.namespace, id.instance)
		b := w.bind(id.namespace, id.instance, 1)
		if err := b.ReadMember(b.Root(), memberSpec(id.root), func(context.Context) ([]byte, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		if err := b.View(simpleView(id.root, viewMember(id.namespace, id.instance, b.Root(), id.root))); err != nil {
			t.Fatal(err)
		}
		entries := listViews(t, w.capture(), b.Root())
		if len(entries) != 1 {
			t.Fatalf("listed views = %+v", entries)
		}
		segment, err := tree.EncodeSegment(id.namespace, id.root)
		if err != nil {
			t.Fatal(err)
		}
		want := tree.Ref("/" + string(b.Root().ID) + "/views/native/" + segment)
		if entries[0].Ref != want {
			t.Fatalf("view ref = %q; want %q", entries[0].Ref, want)
		}
		return entries[0].Ref
	}
	ref := register(t, base)
	for field, change := range map[string]func(*identity){
		"installation": func(id *identity) { id.installation = "installation.other" },
		"driver kind":  func(id *identity) { id.owner = "kind_b" },
		"instance":     func(id *identity) { id.instance = "inst.2" },
		"namespace":    func(id *identity) { id.namespace = "ns_b" },
		"root record":  func(id *identity) { id.root = "Other" },
	} {
		t.Run(field, func(t *testing.T) {
			id := base
			change(&id)
			if register(t, id) == ref {
				t.Fatalf("changing %s did not change the view key", field)
			}
		})
	}
}

func TestViewRegistrationFollowsGeneration(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	instance := tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}
	w.life.set(instance, "running", 1)
	first := w.bind("ns_a", "inst.1", 1)
	read := func(context.Context) ([]byte, error) { return []byte("17"), nil }
	if err := first.ReadMember(first.Root(), memberSpec("amount"), read); err != nil {
		t.Fatal(err)
	}
	spec := simpleView("Sample", viewMember("ns_a", "inst.1", first.Root(), "amount"))
	if err := first.View(spec); err != nil {
		t.Fatal(err)
	}
	if err := w.bind("ns_a", "inst.1", 2).View(simpleView("Other", spec.Records[0].Fields[0].Member)); !errors.Is(err, tree.ErrUnknownViewMember) {
		t.Fatalf("view bound to a member from another generation = %v", err)
	}
	old := w.capture()
	listed := listViews(t, old, first.Root())
	if len(listed) != 1 {
		t.Fatalf("listed views = %+v", listed)
	}
	ref := listed[0].Ref
	w.life.fence(instance, 1, "stopped")
	if err := first.View(spec); !errors.Is(err, tree.ErrFenced) {
		t.Fatalf("fenced registration = %v", err)
	}
	stopped, err := old.Describe(ctx, ref)
	if err != nil || !stopped.Withdrawn || stopped.State == nil || stopped.State.Name != "stopped" {
		t.Fatalf("stopped view = %+v, %v", stopped, err)
	}
	w.life.set(instance, "running", 2)
	second := w.bind("ns_a", "inst.1", 2)
	if err := second.View(spec); !errors.Is(err, tree.ErrUnknownViewMember) {
		t.Fatalf("view bound to withdrawn member = %v", err)
	}
	if err := second.ReadMember(second.Root(), memberSpec("amount"), read); err != nil {
		t.Fatal(err)
	}
	if err := second.View(spec); err != nil {
		t.Fatal(err)
	}
	fresh := w.capture()
	for cut, withdrawn := range map[*tree.TreeCut]bool{old: true, fresh: false} {
		d, err := cut.Describe(ctx, ref)
		if err != nil || d.Withdrawn != withdrawn {
			t.Fatalf("replacement view in cut %s = %+v, %v; withdrawn want %t", cut.ID(), d, err, withdrawn)
		}
		value, err := cut.Read(ctx, d.View.Records[0].Fields[0].Member, tree.ReadOptions{})
		if err != nil || (value.Status == tree.StatusUnavailable) != withdrawn {
			t.Fatalf("replacement member in cut %s = %+v, %v", cut.ID(), value, err)
		}
	}
}
