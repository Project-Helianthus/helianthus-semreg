package identity

import (
	"bytes"
	"errors"
	"reflect"
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func flowElement(subject driver.NodeRef) driver.Element {
	return driver.Element{Subject: subject, Key: semreg.FactKey{PackID: "pack.test", PackVersion: "1.0.0", FactID: "fact.flow", Dimensions: []semreg.Dimension{}}}
}

// mixedLedger holds, in one checkpoint, a single-source asset and an asset with
// two sources, their links, a superseded anchor, a join the operator certified and
// then revoked, a certified pair that stays applied, a rebind and the site.
func mixedLedger(t *testing.T, store Store) (l *Ledger, revoked driver.CertifiedJoin) {
	t.Helper()
	l = ledgerOn(t, store, embeddedCatalog(t))
	allocator := l.allocator
	labelled := mustResolve(t, allocator, kindEquipment, operatorEquipment("unit-b", "first"))
	if _, _, err := allocator.Supersede(Claim{kindEquipment, operatorEquipment("unit-b", "first")}, Claim{kindEquipment, tokenAnchor("token-b", "first")}, continuity(ContinuityBinding, "both")); err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Home(labelled, "asset:single"); err != nil {
		t.Fatal(err)
	}
	single := mustNode(t, l, bridge, slotKey(installation, "driver-a", "a1", "resource.instance", "", "instance"))
	shared := mustNode(t, l, bridge, nativeKey(t, installation, "driver-a", "a1", "resource.equipment", "shared-device"))
	second := mustNode(t, l, bridge, nativeKey(t, installation, "driver-b", "b1", "resource.equipment", "shared-device-other"))
	third := mustNode(t, l, bridge, nativeKey(t, installation, "driver-c", "c1", "resource.equipment", "third-device"))
	old := mustNode(t, l, bridge, nativeKey(t, installation, "driver-a", "a1", "resource.equipment", "sn-old"))
	next := mustNode(t, l, bridge, nativeKey(t, installation, "driver-a", "a1", "resource.equipment", "sn-new"))
	for _, source := range []driver.Source{
		{For: flowElement(single), Producer: single, PathKey: "only", Priority: 5},
		{For: flowElement(shared), Producer: shared, PathKey: "first", Priority: 30},
		{For: flowElement(shared), Producer: second, PathKey: "second", Priority: 20},
	} {
		if err := l.RecordSource(source); err != nil {
			t.Fatal(err)
		}
	}
	a, b := Publisher{Driver: "driver-a", Instance: "a1"}, Publisher{Driver: "driver-b", Instance: "b1"}
	key := driver.LinkKey{Type: reach, From: single, To: shared}
	for _, p := range []Publisher{a, b} {
		if err := l.ClaimLink(p, driver.LinkClaim{Key: key, Basis: proof("link " + string(p.Driver))}); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.ClaimLink(a, driver.LinkClaim{Key: driver.LinkKey{Type: reach, From: single, To: old}, Basis: proof("old")}); err != nil {
		t.Fatal(err)
	}
	if err := l.AdmitManufacturerSerial(b, serialClaim(second, "Synthetic Maker", "SYN-7", "serial")); err != nil {
		t.Fatal(err)
	}
	revoked = driver.CertifiedJoin{A: shared, B: second, Survivor: second, Statement: statement("1", "revoked")}
	kept := driver.CertifiedJoin{A: shared, B: third, Survivor: shared, Statement: statement("2", "kept")}
	for _, join := range []driver.CertifiedJoin{revoked, kept} {
		if err := l.Certify(join); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Revoke(revoked); err != nil {
		t.Fatal(err)
	}
	if err := l.Rebind(old, next, driver.CertifiedJoin{A: old, B: next, Survivor: next, Statement: statement("3", "rebind")}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Site(bridge, installation, nil); err != nil {
		t.Fatal(err)
	}
	return l, revoked
}

// One checkpoint holds a single-source asset and an asset with two sources.
// Export succeeds with every source and its priority, and Restore into a fresh
// store gives the same member identities, ids, anchors, links, source links and
// priorities; the revoked join stays revoked and unapplied, both references stay
// resolvable and the original statement is not reapplied.
func TestCheckpointRoundTripKeepsEverythingIncludingRevocations(t *testing.T) {
	source, revoked := mixedLedger(t, NewMemoryStore())
	data, err := source.Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var priorities []int32
	for _, s := range source.Sources() {
		priorities = append(priorities, s.Priority)
	}
	sort.Slice(priorities, func(i, j int) bool { return priorities[i] < priorities[j] })
	if !reflect.DeepEqual(priorities, []int32{5, 20, 30}) {
		t.Fatalf("the sources of both assets carry priorities %v", priorities)
	}

	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	restored := ledgerOn(t, store, embeddedCatalog(t))
	if err := restored.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	again, err := restored.Export()
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("a restored checkpoint exports other bytes (err=%v)", err)
	}
	if !reflect.DeepEqual(readGraph(t, source), readGraph(t, restored)) {
		t.Fatal("the graph differs after the restore")
	}
	if got, want := restored.Sources(), source.Sources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("sources after the restore = %+v, want %+v", got, want)
	}
	// The anchors and the supersession come back with the file store, which a new process opens.
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := reopened.Table()
	original, _ := source.allocator.store.Table()
	if !reflect.DeepEqual(stored, original) {
		t.Fatalf("anchors after the restore differ:\n got %+v\nwant %+v", stored, original)
	}
	after := ledgerOn(t, reopened, embeddedCatalog(t))
	if _, _, err := after.allocator.Resolve(kindEquipment, operatorEquipment("unit-b", "again")); !errors.Is(err, ErrAnchorSuperseded) {
		t.Fatalf("the superseded anchor after the restore: %v", err)
	}
	if _, _, err := after.allocator.Resolve(kindEquipment, tokenAnchor("token-b", "again")); err != nil {
		t.Fatalf("the upgraded anchor after the restore: %v", err)
	}

	// The revocation survived: the revoked pair stays two resources, both references resolve, the kept pair is joined.
	cut := cutOf(t, embeddedCatalog(t), graph.CutComplete, asset{id: "asset:cut"})
	out, err := restored.Resolve(cut)
	if err != nil {
		t.Fatal(err)
	}
	if len(classOf(out, revoked.A)) != 2 || containsRef(classOf(out, revoked.A), revoked.B) {
		t.Fatalf("the revoked join is applied or the kept one is not: %+v", classOf(out, revoked.A))
	}
	for _, member := range []driver.NodeRef{revoked.A, revoked.B} {
		if _, ok := restored.Lookup(member); !ok {
			t.Fatalf("%v is not resolvable after the restore", member)
		}
	}
	if err := restored.Certify(revoked); err != nil {
		t.Fatal(err)
	}
	out, _ = restored.Resolve(cut)
	if containsRef(classOf(out, revoked.A), revoked.B) {
		t.Fatal("the original statement was applied again after the restore")
	}
	// The rebind and the serial claim came back.
	if rebinds := restored.Rebinds(); len(rebinds) != 1 || rebinds[0].Revoked {
		t.Fatalf("rebinds after the restore = %+v", rebinds)
	}
}

func containsRef(refs []driver.NodeRef, ref driver.NodeRef) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}

// Another format or a damaged document is refused and leaves the state unchanged.
func TestRestoreRefusesForeignAndDamagedCheckpoints(t *testing.T) {
	l, _ := mixedLedger(t, NewMemoryStore())
	before, _ := l.Export()
	for name, tc := range map[string]struct {
		data []byte
		want error
	}{
		"another format":  {[]byte(`{"format":"other","version":1}`), ErrCheckpointForeign},
		"another version": {[]byte(`{"format":"helianthus.semreg.identity-checkpoint","version":2}`), ErrCheckpointForeign},
		"not json":        {[]byte(`nope`), ErrCheckpointDamaged},
		"unknown member":  {[]byte(`{"format":"helianthus.semreg.identity-checkpoint","version":1,"extra":1}`), ErrCheckpointDamaged},
		"invalid node":    {[]byte(`{"format":"helianthus.semreg.identity-checkpoint","version":1,"anchors":{"anchors":[],"homes":[]},"nodes":[{"ref":{"bridge":"","id":""},"keys":[]}]}`), ErrCheckpointDamaged},
	} {
		if err := l.Restore(tc.data); !errors.Is(err, tc.want) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if after, _ := l.Export(); !bytes.Equal(before, after) {
		t.Fatal("a refused checkpoint changed the state")
	}
}

// A restore drops what the allocator held in memory: Home, Resolve and the
// parent check follow only the restored store. A resource the checkpoint does
// not hold stays unknown, and a supersession the checkpoint does not hold stays
// rolled back.
func TestRestoreDropsTheAllocatorsInMemoryState(t *testing.T) {
	l := ledgerOn(t, NewMemoryStore(), embeddedCatalog(t))
	allocator := l.allocator
	anchored := mustResolve(t, allocator, kindEquipment, operatorEquipment("unit-r", "first"))
	data, err := l.Export()
	if err != nil {
		t.Fatal(err)
	}
	later := mustResolve(t, allocator, kindEquipment, operatorEquipment("unit-later", "x"))
	if _, _, err := allocator.Supersede(Claim{kindEquipment, operatorEquipment("unit-r", "first")}, Claim{kindEquipment, tokenAnchor("token-r", "first")}, continuity(ContinuityBinding, "both")); err != nil {
		t.Fatal(err)
	}
	if err := l.Restore(data); err != nil {
		t.Fatal(err)
	}
	// The resource the checkpoint does not hold is not known: no home is recorded for it.
	if _, err := allocator.Home(later, "asset:later"); !errors.Is(err, ErrUnknownResource) {
		t.Fatalf("Home of a resource the checkpoint does not hold: %v", err)
	}
	if home, err := allocator.store.RecordHome(later, "asset:probe"); err != nil || home != "asset:probe" {
		t.Fatalf("a home was recorded for the dropped resource: %q %v", home, err)
	}
	// The rolled-back supersession stays rolled back: the new token is not current on the old resource.
	next, _, err := allocator.Resolve(kindEquipment, tokenAnchor("token-r", "first"))
	if err != nil {
		t.Fatal(err)
	}
	if next == anchored {
		t.Fatalf("the rolled-back supersession was resolved again onto %s", anchored)
	}
	if again, _, err := allocator.Resolve(kindEquipment, operatorEquipment("unit-r", "first")); err != nil || again != anchored {
		t.Fatalf("the earlier anchor after the restore = %s, %v; want %s", again, err, anchored)
	}
}
