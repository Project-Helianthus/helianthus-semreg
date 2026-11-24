package identity

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// view is what one lifecycle publishes and a consumer reads: the resource ids,
// the relation keys of the cut and the consumer value keys (the canonical key of
// each element, its subject and its whole fact key).
type view struct {
	Resources []string
	Relations []string
	Values    []string
}

// TestRestartCompositionPairedThermalSiteLateLink composes two sources on one
// thermal asset, the configured site and an operator identity link declared after
// the first batch. A cut is captured, the composition is stopped and started on
// the same allocator store, the same things are republished and a cut is captured
// again: the resource ids, the relation keys and the consumer value keys are equal,
// the checkpoint export of the shared asset succeeds, and the late link is carried
// after the republish.
func TestRestartCompositionPairedThermalSiteLateLink(t *testing.T) {
	catalog := catalogWith(t, siteSystemRow)
	dir := t.TempDir()
	declared := []OperatorLink(nil) // the operator's declarations outlive the composition

	run := func(generation semreg.Uint64) (view, []semreg.IdentityLink, semreg.ResourceID) {
		store, err := OpenStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		l := ledgerOn(t, store, catalog)
		site, err := l.Site(bridge, installation, nil)
		if err != nil {
			t.Fatal(err)
		}
		thermal := mustNode(t, l, bridge, slotKey(installation, "driver-ebus", "e1", "resource.thermal_system", "", "thermal-1"))
		boiler := mustNode(t, l, bridge, nativeKey(t, installation, "driver-ebus", "e1", "resource.equipment", "boiler-a"))
		remote := mustNode(t, l, bridge, nativeKey(t, installation, "driver-eebus", "r1", "resource.equipment", "boiler-b"))
		for _, source := range []driver.Source{
			{For: flowElement(thermal), Producer: boiler, PathKey: "native", Priority: 10},
			{For: flowElement(thermal), Producer: remote, PathKey: "remote", Priority: 20},
		} {
			if err := l.RecordSource(source); err != nil {
				t.Fatal(err)
			}
		}
		resources := []semreg.Resource{
			resourceOf(string(site.ID), "resource.site", semreg.AnchorFunction, SiteSlot, semreg.ResourceCurrent),
			resourceOf(string(thermal.ID), "resource.thermal_system", semreg.AnchorFunction, "thermal-1", semreg.ResourceCurrent),
		}
		claims := SiteRelations(catalog, site, resources[1:])
		publisher := Publisher{Driver: SiteDriver, Instance: SiteInstance}
		var relations []semreg.RelationInstance
		for _, claim := range claims {
			if err := l.ClaimLink(publisher, claim); err != nil {
				t.Fatal(err)
			}
			relations = append(relations, relationOf(claim.Key.Type, string(claim.Key.From.ID), string(claim.Key.To.ID)))
		}
		cut := cutOf(t, catalog, graph.CutComplete, asset{id: "asset:thermal", resources: resources, relations: relations})
		if _, err := l.Export(); err != nil {
			t.Fatalf("the checkpoint export of the shared asset failed: %v", err)
		}

		var v view
		for _, resource := range resources {
			v.Resources = append(v.Resources, string(resource.ResourceID))
		}
		for _, relation := range cut.Relations() {
			v.Relations = append(v.Relations, fmt.Sprintf("%s %s %s %s", relation.Definition.ID, relation.From, relation.To, relation.Coherence))
		}
		values := map[string]bool{}
		for _, source := range l.Sources() {
			key, err := semreg.CanonicalJSON(source.For)
			if err != nil {
				t.Fatal(err)
			}
			if !values[string(key)] {
				values[string(key)] = true
				v.Values = append(v.Values, string(key))
			}
		}
		sort.Strings(v.Resources)
		sort.Strings(v.Values)

		// The owner's batch of this generation: its binding and its own identity
		// link, with the declarations carried.
		binding := bindingOf(bindingOne, "thermal-native", generation)
		k := kernel{}
		k.commit([]semreg.NativeBinding{binding}, nil)
		built := []semreg.IdentityLink{publisherLink(bindingOne, "own")}
		carried := CarryOperatorLinks(built, PendingOperatorLinks(declared, k.snapshot(), true, linkAsset, linkSource, linkEpoch, generation, []semreg.NativeBinding{binding}))
		return v, carried, thermal.ID
	}

	first, firstLinks, thermal := run("1")
	if len(firstLinks) != 1 || firstLinks[0].Resource != nil {
		t.Fatalf("the first batch carried a link nobody declared: %+v", firstLinks)
	}
	// The operator declares the association after the first batch; the composition restarts.
	declared = []OperatorLink{declare("thermal-native", thermal)}
	second, secondLinks, again := run("2")
	if again != thermal {
		t.Fatalf("thermal id after the restart = %s, want %s", again, thermal)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("the restart changed what consumers read:\n first %+v\nsecond %+v", first, second)
	}
	// Two sources of one element are one consumer value key.
	if len(second.Relations) != 1 || len(second.Values) != 1 || len(second.Resources) != 2 {
		t.Fatalf("view = %+v", second)
	}
	if len(secondLinks) != 1 || secondLinks[0].Resource == nil || *secondLinks[0].Resource != thermal || secondLinks[0].State != semreg.LinkQualified {
		t.Fatalf("the late link was not carried after the republish: %+v", secondLinks)
	}
}
