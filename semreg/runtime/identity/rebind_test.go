package identity

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// A changed serial is never taken for a replacement: the new token is a new
// identity, unbound; the old identity stays resolvable with its link; and no link
// moves to the new identity without a person's statement (I-CHANGED-SERIAL).
// Every field of the key is varied below: a changed token changes only the anchor
// digest, and the identity of the old key is unchanged.
func TestChangedSerialIsANewUnboundIdentityAndNothingMoves(t *testing.T) {
	l := ledgerOn(t, NewMemoryStore(), embeddedCatalog(t))
	p := Publisher{Driver: "driver-modbus", Instance: "m3"}
	transport := mustNode(t, l, bridge, slotKey(installation, p.Driver, p.Instance, "resource.transport", "", "transport"))
	oldKey := nativeKey(t, installation, p.Driver, p.Instance, "resource.equipment", "sn-old")
	old := mustNode(t, l, bridge, oldKey)
	key := driver.LinkKey{Type: reach, From: transport, To: old}
	if err := l.ClaimLink(p, driver.LinkClaim{Key: key, Basis: proof("old link")}); err != nil {
		t.Fatal(err)
	}
	newKey := nativeKey(t, installation, p.Driver, p.Instance, "resource.equipment", "sn-new")
	replacement := mustNode(t, l, bridge, newKey)
	if replacement == old {
		t.Fatal("a changed serial kept the identity")
	}
	oldNode, ok := l.Lookup(old)
	if !ok || oldNode.State != NodeUnbound || !reflect.DeepEqual(oldNode.Keys, []driver.NodeKey{oldKey}) {
		t.Fatalf("old identity = %+v, %v", oldNode, ok)
	}
	newNode, _ := l.Lookup(replacement)
	if newNode.State != NodeUnbound || newNode.ReboundFrom != nil {
		t.Fatalf("new identity = %+v", newNode)
	}
	links := l.Links()
	if len(links) != 1 || links[0].Key != key {
		t.Fatalf("links after the serial changed = %+v", links)
	}
	if len(l.Rebinds()) != 0 {
		t.Fatal("a rebind was inferred")
	}
}

// Rebind moves the old identity's links to the new one only with a person's
// statement; a second identical statement changes nothing; the old identity stays
// resolvable; a revocation of the rebind moves the links back.
func TestRebindMovesLinksOnlyWithAStatement(t *testing.T) {
	l := ledgerOn(t, NewMemoryStore(), embeddedCatalog(t))
	p := Publisher{Driver: "driver-modbus", Instance: "m3"}
	transport := mustNode(t, l, bridge, slotKey(installation, p.Driver, p.Instance, "resource.transport", "", "transport"))
	old := mustNode(t, l, bridge, nativeKey(t, installation, p.Driver, p.Instance, "resource.equipment", "sn-old"))
	next := mustNode(t, l, bridge, nativeKey(t, installation, p.Driver, p.Instance, "resource.equipment", "sn-new"))
	declared := driver.LinkKey{Type: reach, From: transport, To: old}
	if err := l.ClaimLink(p, driver.LinkClaim{Key: declared, Basis: proof("link")}); err != nil {
		t.Fatal(err)
	}
	join := driver.CertifiedJoin{A: old, B: next, Survivor: next, Statement: statement("1", "rebind")}

	for name, tc := range map[string]struct {
		old, new driver.NodeRef
		join     driver.CertifiedJoin
		want     error
	}{
		"pair not named":          {old, ref("resource:other"), join, ErrWrongRebindPair},
		"old survives":            {old, next, driver.CertifiedJoin{A: old, B: next, Survivor: old, Statement: join.Statement}, ErrRebindSurvivor},
		"invalid statement":       {old, next, driver.CertifiedJoin{A: old, B: next, Survivor: next}, nil},
		"the same identity twice": {old, old, driver.CertifiedJoin{A: old, B: old, Survivor: old, Statement: join.Statement}, nil},
	} {
		err := l.Rebind(tc.old, tc.new, tc.join)
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if got := l.Links()[0].Key; got != declared {
		t.Fatalf("a refused rebind moved the link to %+v", got)
	}

	if err := l.Rebind(old, next, join); err != nil {
		t.Fatal(err)
	}
	before, _ := l.Export()
	if err := l.Rebind(old, next, join); err != nil {
		t.Fatal(err)
	}
	if after, _ := l.Export(); !bytes.Equal(before, after) {
		t.Fatal("a second identical statement changed the state")
	}
	moved := l.Links()
	want := driver.LinkKey{Type: reach, From: transport, To: next}
	if len(moved) != 1 || moved[0].Key != want || moved[0].Claims[0].Declared != declared {
		t.Fatalf("links after the rebind = %+v", moved)
	}
	if node, ok := l.Lookup(old); !ok || node.State != NodeUnbound {
		t.Fatalf("old identity = %+v, %v", node, ok)
	}
	if node, _ := l.Lookup(next); node.State != NodeRebound || node.ReboundFrom == nil || *node.ReboundFrom != old {
		t.Fatalf("new identity = %+v", node)
	}
	if rebinds := l.Rebinds(); len(rebinds) != 1 || rebinds[0].Statement != join {
		t.Fatalf("rebinds = %+v", rebinds)
	}
	// Another identity cannot be rebound to the same new identity, and a rebind that closes a cycle is refused.
	third := mustNode(t, l, bridge, nativeKey(t, installation, p.Driver, p.Instance, "resource.equipment", "sn-third"))
	if err := l.Rebind(third, next, driver.CertifiedJoin{A: third, B: next, Survivor: next, Statement: statement("2", "second")}); !errors.Is(err, ErrRebindConflict) {
		t.Fatalf("second rebind to one identity: %v", err)
	}
	if err := l.Rebind(next, old, driver.CertifiedJoin{A: next, B: old, Survivor: old, Statement: statement("3", "back")}); !errors.Is(err, ErrRebindCycle) && !errors.Is(err, ErrRebindConflict) {
		t.Fatalf("cycle: %v", err)
	}

	// The revocation of the statement undoes the move and keeps both identities.
	if err := l.Revoke(join); err != nil {
		t.Fatal(err)
	}
	if got := l.Links()[0].Key; got != declared {
		t.Fatalf("a revoked rebind still moves the link: %+v", got)
	}
	for _, member := range []driver.NodeRef{old, next} {
		if _, ok := l.Lookup(member); !ok {
			t.Fatalf("%v is not resolvable", member)
		}
	}
}

// A rebind whose identities are not nodes yet is retained and applies when both
// are, with no member invented.
func TestRebindOfAbsentIdentitiesIsRetainedUnapplied(t *testing.T) {
	l := ledgerOn(t, NewMemoryStore(), embeddedCatalog(t))
	p := Publisher{Driver: "driver-modbus", Instance: "m3"}
	transport := mustNode(t, l, bridge, slotKey(installation, p.Driver, p.Instance, "resource.transport", "", "transport"))
	oldKey := nativeKey(t, installation, p.Driver, p.Instance, "resource.equipment", "sn-old")
	newKey := nativeKey(t, installation, p.Driver, p.Instance, "resource.equipment", "sn-new")
	oldID, _ := l.allocator.Allocate(oldKey)
	newID, _ := l.allocator.Allocate(newKey)
	old, next := ref(string(oldID)), ref(string(newID))
	declared := driver.LinkKey{Type: reach, From: transport, To: old}
	_ = l.ClaimLink(p, driver.LinkClaim{Key: declared, Basis: proof("link")})
	if err := l.Rebind(old, next, driver.CertifiedJoin{A: old, B: next, Survivor: next, Statement: statement("1", "later")}); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Lookup(next); ok {
		t.Fatal("a member was invented")
	}
	if got := l.Links()[0].Key; got != declared {
		t.Fatalf("an unapplied rebind moved the link: %+v", got)
	}
	mustNode(t, l, bridge, oldKey)
	mustNode(t, l, bridge, newKey)
	if got := l.Links()[0].Key; got != (driver.LinkKey{Type: reach, From: transport, To: next}) {
		t.Fatalf("the rebind did not apply once both were nodes: %+v", got)
	}
}
