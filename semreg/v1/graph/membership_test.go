package graph_test

import (
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func planeList(planes ...graph.PlaneID) []graph.PlaneID { return planes }

func requirePlanes(t *testing.T, what string, got, want []graph.PlaneID) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s planes = %v, want %v", what, got, want)
	}
}

// G-POS-PLN-1: an equipment resource that only relays, beside another
// equipment resource with an energy relation, is in physical and provenance
// only, although its kind also admits energy and control.
func TestPlaneMembershipRelayingEquipment(t *testing.T) {
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:relay", "relay")}, relations: []semreg.RelationInstance{qualified(relRelays, "resource:relay", "resource:meter")}}
	b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{device("resource:meter", "meter"), device("resource:feed", "feed")}, relations: []semreg.RelationInstance{qualified(relSupplies, "resource:feed", "resource:meter")}}
	cut := assemble(t, graph.CutComplete, a, b)
	requirePlanes(t, "relaying equipment", cut.PlanesOfResource("resource:relay"), planeList(graph.PlanePhysical, graph.PlaneProvenance))
	requirePlanes(t, "relayed and supplied equipment", cut.PlanesOfResource("resource:meter"), planeList(graph.PlanePhysical, graph.PlaneEnergy, graph.PlaneProvenance))
	requirePlanes(t, "supplying equipment", cut.PlanesOfResource("resource:feed"), planeList(graph.PlanePhysical, graph.PlaneEnergy))
	requirePlanes(t, "relays instance", cut.PlanesOfRelation(relationKey(relRelays, "resource:relay", "resource:meter")), planeList(graph.PlaneProvenance))
	requirePlanes(t, "supplies instance", cut.PlanesOfRelation(relationKey(relSupplies, "resource:feed", "resource:meter")), planeList(graph.PlaneEnergy))
}

// PLN-2: a relation instance is in its definition's planes only when it is
// qualified and coherent; candidate, rejected and conflicting instances stay
// visible with their state and are in no plane. An endpoint of such an
// instance gains no plane from it.
func TestPlaneMembershipRelationInstances(t *testing.T) {
	for _, tc := range []struct {
		name      string
		state     semreg.LinkState
		farEnd    []semreg.Resource
		coherence graph.RelationCoherence
		want      []graph.PlaneID
	}{
		{"qualified coherent", semreg.LinkQualified, []semreg.Resource{zone("resource:z", "z")}, graph.CoherenceCoherent, planeList(graph.PlaneControl)},
		{"candidate", semreg.LinkCandidate, []semreg.Resource{zone("resource:z", "z")}, graph.CoherenceCoherent, nil},
		{"rejected", semreg.LinkRejected, []semreg.Resource{zone("resource:z", "z")}, graph.CoherenceCoherent, nil},
		{"stored conflict", semreg.LinkConflict, []semreg.Resource{zone("resource:z", "z")}, graph.CoherenceConflict, nil},
		{"stale", semreg.LinkQualified, []semreg.Resource{withState(zone("resource:z", "z"), semreg.ResourceWithdrawn)}, graph.CoherenceStale, nil},
		{"dangling", semreg.LinkQualified, nil, graph.CoherenceDangling, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d")}, relations: []semreg.RelationInstance{relation(relControls, "resource:d", "resource:z", tc.state)}}
			b := cutAsset{id: "asset:cut-b", resources: tc.farEnd}
			cut := assemble(t, graph.CutComplete, a, b)
			key := relationKey(relControls, "resource:d", "resource:z")
			got, ok := cut.Relation(key)
			if !ok || got.State != tc.state || got.Coherence != tc.coherence {
				t.Fatalf("Relation = (%+v, %v)", got, ok)
			}
			requirePlanes(t, "relation", cut.PlanesOfRelation(key), tc.want)
			requirePlanes(t, "controlling device", cut.PlanesOfResource("resource:d"), append(planeList(graph.PlanePhysical), tc.want...))
			requirePlanes(t, "controlled zone", cut.PlanesOfResource("resource:z"), tc.want)
		})
	}
}

// PLN-2 and PLN-3 for facts: a fact is in its definition's planes when its
// subject is current in its member; its subject gains only the planes its
// kind admits.
func TestPlaneMembershipFacts(t *testing.T) {
	power, fault := deviceFact(factPower, "resource:d"), deviceFact(factFault, "resource:d")
	label := labelFact("text")
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d"), zone("resource:z", "z")}, facts: []semreg.FactKey{power, fault, label, deviceFact(factPower, "resource:absent")}}
	cut := assemble(t, graph.CutComplete, a)
	requirePlanes(t, "power fact", cut.PlanesOfFact("asset:cut-a", power), planeList(graph.PlaneEnergy))
	requirePlanes(t, "fault fact", cut.PlanesOfFact("asset:cut-a", fault), planeList(graph.PlaneDiagnostic))
	requirePlanes(t, "unknown fact", cut.PlanesOfFact("asset:cut-a", label), nil)
	requirePlanes(t, "fact without a current subject", cut.PlanesOfFact("asset:cut-a", deviceFact(factPower, "resource:absent")), nil)
	// The kind admits energy but not diagnostic.
	requirePlanes(t, "subject", cut.PlanesOfResource("resource:d"), planeList(graph.PlanePhysical, graph.PlaneEnergy))
	requirePlanes(t, "function without instances", cut.PlanesOfResource("resource:z"), nil)

	// A withdrawn subject leaves every plane, and so does its fact.
	a.resources = []semreg.Resource{withState(device("resource:d", "d"), semreg.ResourceWithdrawn)}
	a.facts = []semreg.FactKey{power}
	withdrawn := assemble(t, graph.CutComplete, a)
	requirePlanes(t, "fact of a withdrawn subject", withdrawn.PlanesOfFact("asset:cut-a", power), nil)
	requirePlanes(t, "withdrawn resource", withdrawn.PlanesOfResource("resource:d"), nil)
}

// PLN-3 for services and capabilities, and the kind bound: a resource enters
// a plane through a current service or capability about it; a withdrawn one
// adds nothing; a kind with disposition unknown is in no plane.
func TestPlaneMembershipServicesCapabilitiesAndKinds(t *testing.T) {
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d"), part("resource:p", "resource:d", "1"), slotResource("resource:pending", kindPending, semreg.AnchorEquipment, "", "pending")}, services: []string{"resource:d"}}
	cut := assemble(t, graph.CutComplete, a)
	// The service adds energy and its capability control.
	requirePlanes(t, "device with a service", cut.PlanesOfResource("resource:d"), planeList(graph.PlanePhysical, graph.PlaneEnergy, graph.PlaneControl))
	requirePlanes(t, "component", cut.PlanesOfResource("resource:p"), planeList(graph.PlanePhysical))
	requirePlanes(t, "unknown kind", cut.PlanesOfResource("resource:pending"), nil)
	requirePlanes(t, "resource in no member", cut.PlanesOfResource("resource:elsewhere"), nil)

	a.withdrawn = true
	tombstoned := assemble(t, graph.CutComplete, a)
	requirePlanes(t, "device with a withdrawn service", tombstoned.PlanesOfResource("resource:d"), planeList(graph.PlanePhysical))
}

// Every member of each lookup key is varied once; a lookup with any member
// changed answers no plane. PlanesOfRelation: definition (pack id, pack
// version, id, version), from, to. PlanesOfFact: asset, pack id, pack
// version, fact id, dimension id, dimension value. PlanesOfResource:
// resource id.
func TestPlaneMembershipLookupKeysVaryEveryField(t *testing.T) {
	power := deviceFact(factPower, "resource:d")
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:d", "d"), zone("resource:z", "z")}, relations: []semreg.RelationInstance{qualified(relControls, "resource:d", "resource:z")}, facts: []semreg.FactKey{power}}
	b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{device("resource:d2", "d2")}, facts: []semreg.FactKey{deviceFact(factPower, "resource:d2")}}
	cut := assemble(t, graph.CutComplete, a, b)

	key := relationKey(relControls, "resource:d", "resource:z")
	requirePlanes(t, "relation", cut.PlanesOfRelation(key), planeList(graph.PlaneControl))
	for name, mutate := range map[string]func(*semreg.RelationKey){
		"pack id":      func(k *semreg.RelationKey) { k.Definition.Pack.ID = "pack.other" },
		"pack version": func(k *semreg.RelationKey) { k.Definition.Pack.Version = "1.0.1" },
		"id":           func(k *semreg.RelationKey) { k.Definition.ID = relDevicePart.ID },
		"version":      func(k *semreg.RelationKey) { k.Definition.Version = "1.0.1" },
		"from":         func(k *semreg.RelationKey) { k.From = "resource:d2" },
		"to":           func(k *semreg.RelationKey) { k.To = "resource:d" },
	} {
		changed := key
		mutate(&changed)
		requirePlanes(t, "relation with another "+name, cut.PlanesOfRelation(changed), nil)
	}

	requirePlanes(t, "fact", cut.PlanesOfFact("asset:cut-a", power), planeList(graph.PlaneEnergy))
	requirePlanes(t, "fact of another asset", cut.PlanesOfFact("asset:cut-b", power), nil)
	for name, mutate := range map[string]func(*semreg.FactKey){
		"pack id":      func(k *semreg.FactKey) { k.PackID = "pack.other" },
		"pack version": func(k *semreg.FactKey) { k.PackVersion = "1.0.1" },
		"fact id":      func(k *semreg.FactKey) { k.FactID = factFault },
		"dimension id": func(k *semreg.FactKey) { k.Dimensions[0].ID = "cut.dimension.other" },
		"dimension value": func(k *semreg.FactKey) {
			k.Dimensions[0].Value = semreg.Value{Kind: semreg.ValueText, Text: text("resource:d2")}
		},
	} {
		changed := power
		changed.Dimensions = append([]semreg.Dimension(nil), power.Dimensions...)
		mutate(&changed)
		requirePlanes(t, "fact with another "+name, cut.PlanesOfFact("asset:cut-a", changed), nil)
	}

	requirePlanes(t, "resource", cut.PlanesOfResource("resource:d"), planeList(graph.PlanePhysical, graph.PlaneEnergy, graph.PlaneControl))
	requirePlanes(t, "another resource", cut.PlanesOfResource("resource:d3"), nil)
}

// An excluded member contributes no fact, relation or resource to any plane.
func TestPlaneMembershipExcludedMembers(t *testing.T) {
	power := deviceFact(factPower, "resource:x")
	a := cutAsset{id: "asset:cut-a", resources: []semreg.Resource{device("resource:x", "x"), zone("resource:z", "z")}, relations: []semreg.RelationInstance{qualified(relControls, "resource:x", "resource:z")}, facts: []semreg.FactKey{power}}
	b := cutAsset{id: "asset:cut-b", resources: []semreg.Resource{device("resource:x", "x")}}
	cut := assemble(t, graph.CutComplete, a, b)
	requirePlanes(t, "fact of an excluded member", cut.PlanesOfFact("asset:cut-a", power), nil)
	requirePlanes(t, "relation of an excluded member", cut.PlanesOfRelation(relationKey(relControls, "resource:x", "resource:z")), nil)
	requirePlanes(t, "resource of an excluded member", cut.PlanesOfResource("resource:z"), nil)
}
