package tree_test

import (
	"reflect"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestRosterListsNamespacesOfOneOwnerOverTwoInstances(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_b", "inst.2", "inst.1")
	w.reserve("kind_a", "ns_a", "inst.1", "inst.2")
	w.reserve("kind_a", "ns_c", "inst.1")
	cut := w.capture()
	want := []tree.RosterEntry{
		{Owner: "kind_a", Key: "ns_a", Instances: []driver.InstanceKey{"inst.1", "inst.2"}},
		{Owner: "kind_a", Key: "ns_b", Instances: []driver.InstanceKey{"inst.1", "inst.2"}},
		{Owner: "kind_a", Key: "ns_c", Instances: []driver.InstanceKey{"inst.1"}},
	}
	if got := cut.Roster(); !reflect.DeepEqual(got, want) {
		t.Fatalf("roster = %+v, want %+v", got, want)
	}
}

func TestRosterShowsSecondNamespaceReservedOnExistingInstanceAndEmptyInstance(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	w.reserve("kind_a", "ns_b", "inst.1")
	w.reserve("kind_b", "ns_empty")
	cut := w.capture()
	got := cut.Roster()
	if len(got) != 3 || !reflect.DeepEqual(got[0].Instances, []driver.InstanceKey{"inst.1"}) || !reflect.DeepEqual(got[1].Instances, []driver.InstanceKey{"inst.1"}) {
		t.Fatalf("roster = %+v", got)
	}
	if got[2].Key != "ns_empty" || got[2].Owner != "kind_b" || len(got[2].Instances) != 0 {
		t.Fatalf("namespace without instances = %+v", got[2])
	}
	entries, err := cut.List(ctx, cut.InstallationRoot(), tree.Page{Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries.Entries) == 0 {
		t.Fatal("instance with no members is not listed under the installation")
	}
}

func TestRosterIsCopiedAtCaptureAndCallerCannotMoveIt(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	cut := w.capture()
	w.reserve("kind_a", "ns_a", "inst.2")
	w.reserve("kind_a", "ns_z", "inst.9")
	cut.Roster()[0].Instances[0] = "mutated"
	want := []tree.RosterEntry{{Owner: "kind_a", Key: "ns_a", Instances: []driver.InstanceKey{"inst.1"}}}
	if got := cut.Roster(); !reflect.DeepEqual(got, want) {
		t.Fatalf("roster moved under the cut: %+v", got)
	}
	if got := w.capture().Roster(); len(got) != 2 || len(got[0].Instances) != 2 {
		t.Fatalf("later capture = %+v", got)
	}
}

func TestInstallationRootIsPresentBeforeAnyPublication(t *testing.T) {
	w := newWorld(t)
	cut := w.capture()
	want := tree.NodeRef{Bridge: bridge, ID: tree.NodePath{Installation: installation}.NodeID()}
	if got := cut.InstallationRoot(); got != want {
		t.Fatalf("root = %+v, want %+v", got, want)
	}
	if _, err := cut.List(ctx, cut.InstallationRoot(), tree.Page{Size: 5}); err != nil {
		t.Fatal(err)
	}
}

func TestCutByIDContinuesAListingReleasesAndReportsDangling(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1", "inst.2", "inst.3")
	cut := w.capture()
	first, err := cut.List(ctx, cut.InstallationRoot(), tree.Page{Size: 1})
	if err != nil || first.Next == "" {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	again, err := w.tree.CutByID(cut.ID())
	if err != nil || again != cut {
		t.Fatalf("lookup = %p %v, want %p", again, err, cut)
	}
	second, err := again.List(ctx, again.InstallationRoot(), tree.Page{Size: 1, Token: first.Next})
	if err != nil || len(second.Entries) != 1 || second.Entries[0].Ref == first.Entries[0].Ref {
		t.Fatalf("second page = %+v, %v", second, err)
	}

	other := w.capture()
	_, err = other.List(ctx, other.InstallationRoot(), tree.Page{Size: 1, Token: first.Next})
	mustCode(t, err, semreg.RevisionConflict)

	if err := w.tree.Release(cut.ID()); err != nil {
		t.Fatal(err)
	}
	_, err = w.tree.CutByID(cut.ID())
	mustCode(t, err, semreg.DanglingReference)
	_, err = w.tree.CutByID("unknown:1")
	mustCode(t, err, semreg.DanglingReference)
	mustCode(t, w.tree.Release(cut.ID()), semreg.DanglingReference)
}
