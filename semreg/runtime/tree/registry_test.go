package tree_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestNamespacePlanGivesOneMemberTheSameNameInBothSurfaces(t *testing.T) {
	node := instancePath("kind_a", "inst.1")
	key := nativeRef(t, "ns_a", node, tree.KeywordAttributes, "member.1")
	names, err := tree.DriverNames("ns_a", tree.VerbRead, string(key))
	if err != nil {
		t.Fatal(err)
	}
	if names.GraphQL != "ns_a_read" || names.MCP != "ns_a_v1_read" || names.Key != string(key) {
		t.Fatalf("names = %+v", names)
	}
	again, err := tree.DriverNames("ns_a", tree.VerbRead, string(key))
	if err != nil || again != names {
		t.Fatalf("the rule is not a function of namespace, verb and key: %+v %v", again, err)
	}
	method := nativeRef(t, "ns_a", node, tree.KeywordMethods, "pair")
	for _, verb := range []tree.Verb{tree.VerbDescribe, tree.VerbPrepare, tree.VerbCall} {
		if _, err := tree.DriverNames("ns_a", verb, string(method)); err != nil {
			t.Errorf("method verb %s: %v", verb, err)
		}
	}
	for name, call := range map[string]func() error{
		"read on a method":        func() error { _, err := tree.DriverNames("ns_a", tree.VerbRead, string(method)); return err },
		"call on an attribute":    func() error { _, err := tree.DriverNames("ns_a", tree.VerbCall, string(key)); return err },
		"namespace of another":    func() error { _, err := tree.DriverNames("ns_b", tree.VerbRead, string(key)); return err },
		"namespace not a name":    func() error { _, err := tree.DriverNames("ns-a", tree.VerbRead, string(key)); return err },
		"semantic key":            func() error { _, err := tree.DriverNames("ns_a", tree.VerbRead, string(semanticRef(t))); return err },
		"key that does not parse": func() error { _, err := tree.DriverNames("ns_a", tree.VerbRead, "ns_a/member"); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: a name was given", name)
		}
	}
	if err := func() error { _, err := tree.DriverNames("ns-a", tree.VerbRead, string(key)); return err }(); !errors.Is(err, tree.ErrNoName) {
		t.Errorf("an unnameable namespace = %v, want ErrNoName", err)
	}
	for _, read := range tree.SemanticReads {
		if read.GraphQL == "" || read.MCP == "" {
			t.Errorf("semantic read %+v has no name", read)
		}
	}
	for _, verb := range tree.TreeVerbs {
		graphql, err1 := tree.TreeName(tree.SurfaceGraphQL, verb)
		mcp, err2 := tree.TreeName(tree.SurfaceMCP, verb)
		if err1 != nil || err2 != nil || graphql != "tree_"+string(verb) || mcp != "tree_v1_"+string(verb) {
			t.Errorf("tree verb %s = %q %q %v %v", verb, graphql, mcp, err1, err2)
		}
	}
	if _, err := tree.TreeName(tree.SurfaceMCP, "wipe"); !errors.Is(err, tree.ErrNoName) {
		t.Errorf("an unknown verb = %v", err)
	}
}

func semanticRef(t *testing.T) tree.Ref {
	t.Helper()
	segment, err := tree.FactSegment(factKey("fact.power", "device.one"))
	if err != nil {
		t.Fatal(err)
	}
	return tree.Ref(tree.Key{Node: tree.NodePath{Installation: installation}, Facet: tree.KeywordAttributes, Domain: tree.KeywordSem, Segment: segment}.String())
}

func TestTwoDriverKindsWithThreeInstancesOfOneKind(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1", "inst.2", "inst.3")
	w.reserve("kind_b", "ns_b", "solo")
	for _, instance := range []string{"inst.1", "inst.2", "inst.3"} {
		b := w.bind("ns_a", instance, 1)
		if err := b.ReadMember(b.Root(), memberSpec("member.1"), func(context.Context) ([]byte, error) { return []byte(instance), nil }); err != nil {
			t.Fatal(err)
		}
	}
	solo := w.bind("ns_b", "solo", 1)
	if err := solo.ReadMember(solo.Root(), memberSpec("member.1"), func(context.Context) ([]byte, error) { return []byte("solo"), nil }); err != nil {
		t.Fatal(err)
	}
	cut := w.capture()
	entries, err := cut.List(ctx, nodeRefOf(tree.NodePath{Installation: installation}), tree.Page{Size: 1000, Filter: tree.Filter{Kind: tree.EntryNode}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries.Entries) != 4 {
		t.Fatalf("instance nodes = %d, want 4", len(entries.Entries))
	}
	seen := map[tree.Ref]string{}
	for _, instance := range []string{"inst.1", "inst.2", "inst.3"} {
		ref := nativeRef(t, "ns_a", instancePath("kind_a", instance), tree.KeywordAttributes, "member.1")
		value, err := cut.Read(ctx, ref, tree.ReadOptions{})
		if err != nil || string(value.Encoded) != instance {
			t.Fatalf("instance %s reads %+v, %v", instance, value, err)
		}
		seen[ref] = instance
	}
	if len(seen) != 3 {
		t.Fatal("three instances of one kind share a member key")
	}
	only := driver.InstanceKey("inst.2")
	filtered, err := cut.List(ctx, nodeRefOf(tree.NodePath{Installation: installation}), tree.Page{Size: 10, Filter: tree.Filter{Instance: &only, Kind: tree.EntryNode}})
	if err != nil || len(filtered.Entries) != 1 || filtered.Entries[0].Instance.Instance != "inst.2" {
		t.Fatalf("instance filter = %+v, %v", filtered, err)
	}
}

func TestNamespaceConflictAtPlanning(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_shared", "inst.1")
	err := w.tree.Registry.Reserve(driver.Namespace{Owner: "kind_b", Key: "ns_shared"}, []driver.InstanceKey{"other"})
	if !errors.Is(err, tree.ErrNamespaceConflict) {
		t.Fatalf("second owner = %v, want ErrNamespaceConflict", err)
	}
	if err := w.tree.Registry.Reserve(driver.Namespace{Owner: "kind_a", Key: "ns_shared"}, []driver.InstanceKey{"inst.2"}); err != nil {
		t.Fatalf("the owner adds an instance: %v", err)
	}
	if _, err := w.tree.Registry.Bind("ns_shared", "other", 1); !errors.Is(err, tree.ErrNotReserved) {
		t.Fatalf("an instance of the refused owner = %v", err)
	}
	if _, err := w.tree.Registry.Bind("ns_unreserved", "inst.1", 1); !errors.Is(err, tree.ErrNotReserved) {
		t.Fatalf("an unreserved namespace = %v", err)
	}
}

func TestRegistrationAcceptsOwnSubtreeAndRejectsSiblingInstance(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1", "inst.2")
	one := w.bind("ns_a", "inst.1", 1)
	two := w.bind("ns_a", "inst.2", 1)
	read := func(context.Context) ([]byte, error) { return []byte("v"), nil }
	call := func(context.Context, []byte) ([]byte, error) { return nil, nil }

	transport, err := one.Node(nodeSpec("transport.1", nil))
	if err != nil {
		t.Fatalf("Node under the instance root: %v", err)
	}
	device, err := one.Node(nodeSpec("device.1", &transport))
	if err != nil {
		t.Fatalf("Node under its transport: %v", err)
	}
	if err := one.ReadMember(device, memberSpec("member.1"), read); err != nil {
		t.Fatalf("ReadMember inside the subtree: %v", err)
	}
	if err := one.Method(device, methodSpec("method.1"), call); err != nil {
		t.Fatalf("Method inside the subtree: %v", err)
	}
	siblingTransport, err := two.Node(nodeSpec("transport.1", nil))
	if err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"Node under a sibling's node":    second(one.Node(nodeSpec("device.9", &siblingTransport))),
		"ReadMember on a sibling's node": one.ReadMember(siblingTransport, memberSpec("member.9"), read),
		"Method on a sibling's node":     one.Method(siblingTransport, methodSpec("method.9"), call),
		"Node under the sibling's root":  second(one.Node(nodeSpec("transport.9", ptr(two.Root())))),
	} {
		if !errors.Is(err, tree.ErrOutsideInstance) {
			t.Errorf("%s = %v, want ErrOutsideInstance", name, err)
		}
	}
	unknown := nodeRefOf(instancePath("kind_a", "inst.1"))
	unknown.ID += semreg.ResourceID("/transports/" + mustSegment(t, "never"))
	if err := one.ReadMember(unknown, memberSpec("member.9"), read); !errors.Is(err, tree.ErrUnknownParent) {
		t.Errorf("unknown parent = %v", err)
	}
	if err := one.ReadMember(device, memberSpec("member.1"), read); !errors.Is(err, tree.ErrDuplicate) {
		t.Errorf("duplicate member key = %v", err)
	}
	if err := one.Method(device, methodSpec("method.1"), call); !errors.Is(err, tree.ErrDuplicate) {
		t.Errorf("duplicate method key = %v", err)
	}
	if _, err := one.Node(nodeSpec("device.1", &transport)); !errors.Is(err, tree.ErrDuplicate) {
		t.Errorf("duplicate node = %v", err)
	}
	if err := one.ReadMember(device, memberSpec("member.2"), nil); err == nil {
		t.Error("a member without a reader was accepted")
	}
}

func second[T any](_ T, err error) error { return err }

func ptr[T any](v T) *T { return &v }

func TestProcedureListedOnInstanceNodeBeforeAnyDevice(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	b := w.bind("ns_a", "inst.1", 1)
	if err := b.Method(b.Root(), methodSpec("pairing"), func(context.Context, []byte) ([]byte, error) { return []byte("ok"), nil }); err != nil {
		t.Fatal(err)
	}
	cut := w.capture()
	entries, err := cut.List(ctx, b.Root(), tree.Page{Size: 10, Filter: tree.Filter{Kind: tree.EntryMethod}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries.Entries) != 1 || entries.Entries[0].Local != "pairing" || entries.Entries[0].Level != "" {
		t.Fatalf("procedures = %+v", entries.Entries)
	}
	children, err := cut.List(ctx, b.Root(), tree.Page{Size: 10, Filter: tree.Filter{Kind: tree.EntryNode}})
	if err != nil || len(children.Entries) != 0 {
		t.Fatalf("the instance has devices: %+v %v", children, err)
	}
}
