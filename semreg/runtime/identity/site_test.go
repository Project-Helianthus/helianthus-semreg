package identity

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// siteSystemRow is the approved relation.site_system row of the infrastructure
// family: groups, from resource.site to the system kinds and generic equipment,
// one to many, basis configuration and gate_rule, plane physical. The frozen
// catalog does not publish it yet, so the test adds it.
func siteSystemRow(doc map[string]any) {
	pack := map[string]any{"kind": "pack", "pack": map[string]any{"id": "helianthus.pack.infrastructure", "version": "1.0.0"}}
	elements := doc["elements"].([]any)
	doc["elements"] = append(elements, map[string]any{
		"ref":          map[string]any{"owner": pack, "id": "relation.site_system", "version": "1.0.0"},
		"element_kind": "relation", "disposition": "member", "planes": []any{"physical"},
		"admission": map[string]any{"arm": "accepted_pack", "evidence": []any{"https://github.com/Project-Helianthus/helianthus-docs-semantic/issues/67"}},
		"attributes": map[string]any{
			"core_type": "groups", "orientation": "as_core",
			"from_kinds":    []any{"resource.site"},
			"to_kinds":      []any{"resource.pv_system", "resource.storage_system", "resource.thermal_system", "resource.charging_station", "resource.equipment"},
			"cardinality":   "one_to_many",
			"over_time":     false,
			"basis_classes": []any{"configuration", "gate_rule"},
		},
	})
}

func current(id, kind string, state semreg.ResourceState) semreg.Resource {
	return resourceOf(id, kind, semreg.AnchorFunction, id, state)
}

// The same site id on every start; an operator-declared site is reused and no
// second site appears.
func TestSiteIsTheSameOnEveryStartAndTheDeclaredSiteIsReused(t *testing.T) {
	catalog := embeddedCatalog(t)
	var sites [2]driver.NodeRef
	for i := range sites {
		l := ledgerOn(t, NewMemoryStore(), catalog)
		mustNode(t, l, bridge, slotKey(installation, "driver-x", "x1", "resource.instance", "", "instance"))
		site, err := l.Site(bridge, installation, nil)
		if err != nil {
			t.Fatal(err)
		}
		sites[i] = site
	}
	if sites[0] != sites[1] || sites[0] != ref(idSite) {
		t.Fatalf("sites = %+v, want %s", sites, idSite)
	}
	other := ledgerOn(t, NewMemoryStore(), catalog)
	if site, _ := other.Site(bridge, "inst-b", nil); site == ref(idSite) {
		t.Fatal("two installations share a site")
	}

	// The operator's records declare a site: it is the installation root and no default one is created.
	l := ledgerOn(t, NewMemoryStore(), catalog)
	declared := semreg.ResourceID("resource:declared-site")
	site, err := l.Site(bridge, installation, &declared)
	if err != nil || site != (driver.NodeRef{Bridge: bridge, ID: declared}) {
		t.Fatalf("declared site = %+v, %v", site, err)
	}
	if again, err := l.Site(bridge, installation, &declared); err != nil || again != site {
		t.Fatalf("declared site again = %+v, %v", again, err)
	}
	if _, err := l.Site(bridge, installation, nil); !errors.Is(err, ErrSecondSite) {
		t.Fatalf("a second site appeared: %v", err)
	}
	if _, ok := l.Lookup(ref(idSite)); ok {
		t.Fatal("the default site was created beside the declared one")
	}
}

type claimed struct {
	relation semreg.DefinitionID
	to       semreg.ResourceID
}

func claimsOf(claims []driver.LinkClaim) []claimed {
	var out []claimed
	for _, c := range claims {
		out = append(out, claimed{c.Key.Type.ID, c.Key.To.ID})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].relation != out[j].relation {
			return out[i].relation < out[j].relation
		}
		return out[i].to < out[j].to
	})
	return out
}

// With the relation.site_system row, the site links each current system resource
// and each configured generic equipment root the catalog admits, a published grid
// connection by the site-grid-connection relation, and nothing else; a withdrawn
// resource has no link; the basis is configuration owned by the operator.
func TestSiteRelationsWithTheSiteSystemRow(t *testing.T) {
	site := ref(idSite)
	configured := resourceOf("resource:equipment-root", "resource.equipment", semreg.AnchorEquipment, "root", semreg.ResourceCurrent)
	native := configured
	native.ResourceID = "resource:equipment-native"
	native.Anchor.Basis = semreg.AnchorNativeIdentity
	child := configured
	child.ResourceID = "resource:equipment-child"
	parent := semreg.ResourceID("resource:equipment-root")
	child.Anchor.Parent = &parent
	resources := []semreg.Resource{
		current("resource:pv", "resource.pv_system", semreg.ResourceCurrent),
		current("resource:storage", "resource.storage_system", semreg.ResourceCurrent),
		current("resource:thermal", "resource.thermal_system", semreg.ResourceCurrent),
		resourceOf("resource:station", "resource.charging_station", semreg.AnchorEquipment, "station", semreg.ResourceCurrent),
		configured, native, child,
		current("resource:grid", "resource.grid_connection", semreg.ResourceCurrent),
		current("resource:grid-withdrawn", "resource.grid_connection", semreg.ResourceWithdrawn),
		current("resource:pv-withdrawn", "resource.pv_system", semreg.ResourceWithdrawn),
		resourceOf("resource:battery", "resource.battery", semreg.AnchorEquipment, "battery", semreg.ResourceCurrent),
	}
	for name, catalog := range map[string]*graph.Catalog{"the catalog with the row": catalogWith(t, siteSystemRow)} {
		claims := SiteRelations(catalog, site, resources)
		got := claimsOf(claims)
		expected := []claimed{
			{"infrastructure.relation.site_grid_connection", "resource:grid"},
			{"relation.site_system", "resource:equipment-root"},
			{"relation.site_system", "resource:pv"},
			{"relation.site_system", "resource:station"},
			{"relation.site_system", "resource:storage"},
			{"relation.site_system", "resource:thermal"},
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("%s: claims\n got %+v\nwant %+v", name, got, expected)
		}
		for _, claim := range claims {
			if err := claim.Validate(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if claim.Key.From != site || claim.Key.To.Bridge != bridge || len(claim.Basis) != 1 || claim.Basis[0].Owner != OperatorEvidenceOwner {
				t.Fatalf("%s: claim %+v", name, claim)
			}
			if relation, ok := catalog.Relation(graph.PackElement(claim.Key.Type)); !ok || relation.BasisClasses[0] != "configuration" {
				t.Fatalf("%s: relation %v is not a configuration relation of the catalog", name, claim.Key.Type)
			}
		}
		// A resource that is withdrawn has no claim, so its link is withdrawn with it.
		l := ledgerOn(t, NewMemoryStore(), catalog)
		publisher := Publisher{Driver: "semreg.runtime", Instance: "site"}
		for _, claim := range claims {
			if err := l.ClaimLink(publisher, claim); err != nil {
				t.Fatal(err)
			}
		}
		withdrawn := append([]semreg.Resource(nil), resources...)
		withdrawn[0] = current("resource:pv", "resource.pv_system", semreg.ResourceWithdrawn)
		remaining := claimsOf(SiteRelations(catalog, site, withdrawn))
		for _, c := range claims {
			stay := false
			for _, r := range remaining {
				stay = stay || (r.relation == c.Key.Type.ID && r.to == c.Key.To.ID)
			}
			if !stay {
				if err := l.WithdrawLink(publisher, c.Key); err != nil {
					t.Fatal(err)
				}
			}
		}
		if len(l.Links()) != len(expected)-1 {
			t.Fatalf("%s: links after the withdrawal = %d", name, len(l.Links()))
		}
	}
	// The frozen catalog publishes no relation.site_system: no claim of it is made.
	frozen := claimsOf(SiteRelations(embeddedCatalog(t), site, resources))
	if !reflect.DeepEqual(frozen, []claimed{{"infrastructure.relation.site_grid_connection", "resource:grid"}}) {
		t.Fatalf("frozen catalog claims = %+v", frozen)
	}
}

// The configured storage grouping keeps its identifiers across two lifecycles:
// each identity comes from its operator slot, whatever the order they are
// configured in.
func TestStorageGroupingKeepsItsIdentitiesAcrossLifecycles(t *testing.T) {
	catalog := embeddedCatalog(t)
	if _, ok := catalog.Relation(graph.PackElement(systemBatteryRelation)); !ok {
		t.Fatal("the catalog has no system_battery relation under the grouping's reference")
	}
	const system, battery1, battery2 = "resource:23247c677e5051157248880df333d5c1", "resource:b1f2bc7237c350213d6e797775fd5323", "resource:4d651940b169a1b3fd738dbce269674e"
	var runs [2][]claimed
	for i, reversed := range []bool{false, true} {
		l := ledgerOn(t, NewMemoryStore(), catalog)
		slots := []string{"battery-1", "battery-2"}
		if reversed {
			slots = []string{"battery-2", "battery-1"}
		}
		var batteries []driver.NodeRef
		for _, slot := range slots {
			batteries = append(batteries, mustNode(t, l, bridge, slotKey(installation, "driver-x", "x1", "resource.battery", "", slot)))
		}
		sys := mustNode(t, l, bridge, slotKey(installation, "driver-x", "x1", "resource.storage_system", "", "storage-1"))
		claims := StorageGrouping(sys, batteries)
		for _, c := range claims {
			if err := c.Validate(); err != nil || c.Key.From.ID != system || c.Basis[0].Owner != OperatorEvidenceOwner {
				t.Fatalf("claim %+v: %v", c, err)
			}
		}
		runs[i] = claimsOf(claims)
	}
	want := []claimed{{"storage.relation.system_battery", battery2}, {"storage.relation.system_battery", battery1}}
	sort.Slice(want, func(i, j int) bool { return want[i].to < want[j].to })
	if !reflect.DeepEqual(runs[0], want) || !reflect.DeepEqual(runs[1], want) {
		t.Fatalf("runs = %+v, want %+v", runs, want)
	}
}
