package identity

import (
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

var systemBattery = semreg.DefinitionRef{Pack: semreg.PackRef{ID: "helianthus.pack.storage", Version: "1.1.0"}, ID: "storage.relation.system_battery", Version: "1.1.0"}

// Two publishers attest one directed tuple with different bases: both claims are
// kept, one publisher's basis never replaces the other's, and withdrawing one
// leaves the edge and the other claim. The same holds with To absent and after it
// returns (I-TWO-PUBLISHERS, I-ABSENT-TARGET).
func TestLinkClaimsOfTwoPublishersAreKeptApart(t *testing.T) {
	ledger := ledgerOn(t, NewMemoryStore(), embeddedCatalog(t))
	from, to := ref("resource:system-1"), ref("resource:battery-1")
	key := driver.LinkKey{Type: systemBattery, From: from, To: to}
	first, second := Publisher{Driver: "driver-a", Instance: "a1"}, Publisher{Driver: "driver-b", Instance: "b1"}
	firstBasis, secondBasis := proof("first"), proof("second")
	for _, c := range []struct {
		p     Publisher
		basis []semreg.EvidenceRef
	}{{first, firstBasis}, {second, secondBasis}} {
		if err := ledger.ClaimLink(c.p, driver.LinkClaim{Key: key, Basis: c.basis}); err != nil {
			t.Fatal(err)
		}
	}
	claims := ledger.Claims(key)
	if len(claims) != 2 || claims[0].Publisher != first || claims[1].Publisher != second ||
		!reflect.DeepEqual(claims[0].Basis, firstBasis) || !reflect.DeepEqual(claims[1].Basis, secondBasis) {
		t.Fatalf("claims with the target absent = %+v", claims)
	}
	// A later claim by the first publisher replaces only its own basis.
	replacement := proof("first again")
	if err := ledger.ClaimLink(first, driver.LinkClaim{Key: key, Basis: replacement}); err != nil {
		t.Fatal(err)
	}
	claims = ledger.Claims(key)
	if len(claims) != 2 || !reflect.DeepEqual(claims[0].Basis, replacement) || !reflect.DeepEqual(claims[1].Basis, secondBasis) {
		t.Fatalf("claims after a replacement = %+v", claims)
	}
	// The target returns under the same key: the same link key resolves with both claims.
	mustNode(t, ledger, bridge, slotKey(installation, "driver-b", "b1", "resource.battery", "", "battery-1"))
	if len(ledger.Claims(key)) != 2 || len(ledger.Links()) != 1 || ledger.Links()[0].Key != key {
		t.Fatalf("links after the target returned: %+v", ledger.Links())
	}
	// Withdrawing one publisher's claim leaves the other's.
	if err := ledger.WithdrawLink(first, key); err != nil {
		t.Fatal(err)
	}
	claims = ledger.Claims(key)
	if len(claims) != 1 || claims[0].Publisher != second || !reflect.DeepEqual(claims[0].Basis, secondBasis) || len(ledger.Links()) != 1 {
		t.Fatalf("claims after the first was withdrawn = %+v", claims)
	}
	// With the target absent again the second claim still stays under the same To.
	if err := ledger.WithdrawLink(first, key); err != nil || len(ledger.Claims(key)) != 1 {
		t.Fatalf("withdrawing twice: %v, %+v", err, ledger.Claims(key))
	}
	if err := ledger.WithdrawLink(second, key); err != nil || len(ledger.Links()) != 0 {
		t.Fatalf("withdrawing the last claim: %v, %+v", err, ledger.Links())
	}
}

// The cut reports a declaration to an absent target by its own rules: dangling on
// a complete cut, outside the cut on a partial one, stale when the target is only
// withdrawn or retired, excluded when an excluded member holds it, and coherent on
// return, always under the same key (I-ABSENT-TARGET; design 3.1).
func TestLinkStatesInTheCut(t *testing.T) {
	catalog := embeddedCatalog(t)
	ledger := ledgerOn(t, NewMemoryStore(), catalog)
	key := driver.LinkKey{Type: systemBattery, From: ref("resource:system-1"), To: ref("resource:battery-1")}
	if err := ledger.ClaimLink(Publisher{Driver: "driver-a", Instance: "a1"}, driver.LinkClaim{Key: key, Basis: proof("basis")}); err != nil {
		t.Fatal(err)
	}
	holder := asset{id: "asset:a", resources: []semreg.Resource{resourceOf("resource:system-1", "resource.storage_system", semreg.AnchorFunction, "system", semreg.ResourceCurrent)},
		relations: []semreg.RelationInstance{relationOf(systemBattery, "resource:system-1", "resource:battery-1")}}
	battery := func(state semreg.ResourceState) []semreg.Resource {
		return []semreg.Resource{resourceOf("resource:battery-1", "resource.battery", semreg.AnchorEquipment, "battery", state)}
	}
	for name, tc := range map[string]struct {
		completeness graph.Completeness
		others       []asset
		want         graph.RelationCoherence
	}{
		"absent, complete cut": {graph.CutComplete, nil, graph.CoherenceDangling},
		"absent, partial cut":  {graph.CutPartial, nil, graph.CoherenceOutsideCut},
		"withdrawn target":     {graph.CutComplete, []asset{{id: "asset:b", resources: battery(semreg.ResourceWithdrawn)}}, graph.CoherenceStale},
		"retired target":       {graph.CutComplete, []asset{{id: "asset:b", resources: battery(semreg.ResourceRetired)}}, graph.CoherenceStale},
		"held by excluded":     {graph.CutComplete, []asset{{id: "asset:b", resources: battery(semreg.ResourceCurrent)}, {id: "asset:c", resources: battery(semreg.ResourceCurrent)}}, graph.CoherenceExcluded},
		"target has returned":  {graph.CutComplete, []asset{{id: "asset:b", resources: battery(semreg.ResourceCurrent)}}, graph.CoherenceCoherent},
	} {
		cut := cutOf(t, catalog, tc.completeness, append([]asset{holder}, tc.others...)...)
		got, ok := ledger.LinkCoherence(cut, key)
		if !ok || got != tc.want {
			t.Errorf("%s: coherence = %q (held %v), want %q", name, got, ok, tc.want)
		}
		if len(ledger.Claims(key)) != 1 || ledger.Links()[0].Key != key {
			t.Errorf("%s: the claim did not keep its key", name)
		}
	}
	// No member holds a relation instance: the declaration is retained, no edge is invented.
	bare := cutOf(t, catalog, graph.CutComplete, asset{id: "asset:a", resources: holder.resources})
	if got, ok := ledger.LinkCoherence(bare, key); ok {
		t.Fatalf("a link no member holds is %q in the cut", got)
	}
}
