package tree_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"time"
)

func publishGraph(t *testing.T, w *world) []tree.Descriptor {
	t.Helper()
	w.cuts.add(siteAsset("asset.a", "res:site", "res:grid1"))
	w.reserve("kind_a", "ns_a", "inst.1", "inst.2")
	w.reserve("kind_b", "ns_b", "solo")
	for _, ref := range []tree.InstanceRef{{Driver: "kind_a", Instance: "inst.1"}, {Driver: "kind_a", Instance: "inst.2"}, {Driver: "kind_b", Instance: "solo"}} {
		w.life.set(ref, "running", 1)
	}
	for _, instance := range []string{"inst.1", "inst.2"} {
		b := w.bind("ns_a", instance, 1)
		transport, err := b.Node(nodeSpec("transport.1", nil))
		if err != nil {
			t.Fatal(err)
		}
		device, err := b.Node(nodeSpec("device.1", &transport))
		if err != nil {
			t.Fatal(err)
		}
		value := &retained{obs: tree.Observation{Encoded: []byte(instance)}}
		if err := b.ObserveMember(device, memberSpec("member.1"), value.read); err != nil {
			t.Fatal(err)
		}
		if err := b.Method(b.Root(), methodSpec("pairing"), func(context.Context, []byte) ([]byte, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
	}
	solo := w.bind("ns_b", "solo", 1)
	if err := solo.ReadMember(solo.Root(), memberSpec("member.1"), func(context.Context) ([]byte, error) { return []byte("s"), nil }); err != nil {
		t.Fatal(err)
	}
	cut := w.capture()
	var out []tree.Descriptor
	var walk func(ref driver.NodeRef)
	walk = func(ref driver.NodeRef) {
		entries, err := cut.List(ctx, ref, tree.Page{Size: 7})
		for {
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range entries.Entries {
				out = append(out, d)
				if d.Kind == tree.EntryNode {
					walk(driver.NodeRef{Bridge: bridge, ID: nodeIDOf(d.Ref)})
				}
			}
			if entries.Next == "" {
				return
			}
			entries, err = cut.List(ctx, ref, tree.Page{Size: 7, Token: entries.Next})
		}
	}
	walk(nodeRefOf(tree.NodePath{Installation: installation}))
	return out
}

func nodeIDOf(ref tree.Ref) semreg.ResourceID { return semreg.ResourceID(string(ref)[1:]) }

func TestTwoLifecyclesOfOneInstallationPublishTheSameGraph(t *testing.T) {
	first := publishGraph(t, newWorld(t))
	second := publishGraph(t, newWorld(t))
	if len(first) < 20 {
		t.Fatalf("the published graph is too small to compare: %d entries", len(first))
	}
	if !reflect.DeepEqual(first, second) {
		for i := range first {
			if i < len(second) && !reflect.DeepEqual(first[i], second[i]) {
				t.Fatalf("entry %d differs:\n%+v\n%+v", i, first[i], second[i])
			}
		}
		t.Fatalf("graphs differ in length: %d vs %d", len(first), len(second))
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Ref == first[i].Ref {
			t.Fatalf("collision on %s", first[i].Ref)
		}
	}
}

func TestResumeAndExpiredPinAreThreeDistinctOutcomes(t *testing.T) {
	w := threeResourceWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	w.life.set(tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}, "running", 1)
	b := w.bind("ns_a", "inst.1", 1)
	cut := w.capture()
	cursor := cut.Cursor()
	page, err := cut.ListResources(ctx, graph.ListRequest{PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	value := &retained{obs: tree.Observation{Encoded: []byte("v")}}
	if err := b.ObserveMember(b.Root(), memberSpec("later.1"), value.read); err != nil {
		t.Fatal(err)
	}
	if err := b.Method(b.Root(), methodSpec("later.2"), func(context.Context, []byte) ([]byte, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}

	w.cuts.add(equipmentAsset("asset.b", "res:eq-b"))
	w.capture()
	w.cuts.advance(graph.CutRetention + time.Second)
	w.capture()

	_, err = cut.ListResources(ctx, graph.ListRequest{PageSize: 1, PageToken: page.Page.NextPage})
	mustCode(t, err, semreg.RevisionConflict)

	changes, err := w.tree.Resume(ctx, cursor, driver.Filter{})
	if err != nil || changes.Resync {
		t.Fatalf("a cursor with history after its cut expired = %+v, %v", changes, err)
	}
	if len(changes.Native) != 2 || changes.Native[0].Kind != tree.ChangeMember || changes.Native[1].Kind != tree.ChangeMethod || changes.Native[0].Position >= changes.Native[1].Position {
		t.Fatalf("changes are not the ordered registrations: %+v", changes.Native)
	}
	again, err := w.tree.Resume(ctx, changes.Cursor, driver.Filter{})
	if err != nil || again.Resync || len(again.Native) != 0 {
		t.Fatalf("resuming from the new cursor = %+v, %v", again, err)
	}

	foreign := cursor
	foreign.Incarnation = "another.incarnation"
	resync, err := w.tree.Resume(ctx, foreign, driver.Filter{})
	if err != nil || !resync.Resync || len(resync.Native) != 0 || len(resync.Commits) != 0 {
		t.Fatalf("a cursor of another incarnation = %+v, %v", resync, err)
	}
	future := cursor
	future.Native += 1000
	resync, err = w.tree.Resume(ctx, future, driver.Filter{})
	if err != nil || !resync.Resync {
		t.Fatalf("a cursor beyond the history = %+v, %v", resync, err)
	}
	w.cuts.resync = true
	resync, err = w.tree.Resume(ctx, cursor, driver.Filter{})
	if err != nil || !resync.Resync {
		t.Fatalf("semantic history unavailable = %+v, %v", resync, err)
	}
}
