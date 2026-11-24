package sources

import (
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func zoneElement(ordinal string) driver.Element {
	return driver.Element{Subject: driver.NodeRef{Bridge: "bridge:lab", ID: v1.ResourceID("resource:zone:" + ordinal)}, Key: dhwKey()}
}

func TestUnresolvedValueStaysASeparateSecondViewAndIsNotJoinedByZoneOrdinal(t *testing.T) {
	l := newLab(t)
	step := quantity("5", -1)
	l.el = ptr(zoneElement("1"))
	boiler := l.addDriver("boiler", "asset:boiler", 30, &step)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))

	remote := driver.Source{For: zoneElement("1"), Producer: driver.NodeRef{Bridge: "bridge:lab", ID: "producer:remote"}, PathKey: "path:zone:1", Priority: 10, Step: &step}
	value := v1.Value{Kind: v1.ValueQuantity, Quantity: &v1.Quantity{Number: v1.Decimal{Coefficient: "32265", Exponent10: -2}, Unit: kelvin}}
	if err := l.table.Hold(Unresolved{Producer: remote.Producer, PathKey: remote.PathKey, Key: dhwKey(), Value: value, Evidence: []v1.EvidenceRef{evidence("remote")}}); err != nil {
		t.Fatal(err)
	}
	if got := l.table.Unresolved(); len(got) != 1 || got[0].PathKey != "path:zone:1" || got[0].Value.Quantity.Number.Coefficient != "32265" {
		t.Fatalf("the unresolved value is a visible second view: %s", mustJSON(t, got))
	}
	e := l.table.Element(zoneElement("1"), l.evals())
	if len(e.Resolution.Sources) != 1 || !isWinner(e, boiler) || hasCoefficient(t, e, "32265") {
		t.Fatalf("an unresolved value is never attached by guess: %s", mustJSON(t, e))
	}
	for _, other := range []driver.Element{zoneElement("2"), dhwElement()} {
		if r := l.table.Element(other, l.evals()); r.Resolution.Winner != nil || len(r.Resolution.Sources) != 0 {
			t.Fatalf("an element the evidence did not name was created or joined: %s", mustJSON(t, r))
		}
	}
	if len(l.table.entries) != 1 {
		t.Fatalf("an unresolved value is not stored as an element: %d entries", len(l.table.entries))
	}
	snapshot := l.view(boiler.asset).Member
	if err := l.table.Admit(driver.Sample{From: remote, Candidate: boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1")}, snapshot); err != ErrUndeclared {
		t.Fatalf("no sample attaches to an undeclared source: %v", err)
	}

	if err := l.table.Declare(remote); err != nil {
		t.Fatal(err)
	}
	if len(l.table.Unresolved()) != 0 {
		t.Fatal("a resolved mapping leaves the second view")
	}
	if e := l.table.Element(zoneElement("1"), l.evals()); len(e.Resolution.Sources) != 2 {
		t.Fatalf("once resolved it is a source of that element: %s", mustJSON(t, e.Resolution))
	}
}

func ptr[T any](v T) *T { return &v }
