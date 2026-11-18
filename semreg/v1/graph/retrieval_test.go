package graph_test

import (
	"bytes"
	"sync"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func readCatalog(t *testing.T, spans int) *graph.Catalog {
	t.Helper()
	doc := decodeDocument(t)
	core := document{"kind": "core", "contract": string(graph.KernelContract)}
	pack := packOwner(cutPack)
	planes := func(ids ...string) []any {
		out := []any{}
		for _, id := range ids {
			out = append(out, id)
		}
		return out
	}
	entry := func(owner document, id, kind, disposition string, planeList []any, attributes document) document {
		return document{
			"ref": elementRef(owner, id), "element_kind": kind, "disposition": disposition, "planes": planeList,
			"admission":  document{"arm": "accepted_pack", "evidence": []any{"https://github.com/Project-Helianthus/helianthus-docs-semantic/issues/67"}},
			"attributes": attributes,
		}
	}
	kind := func(id, class string, planeList []any) document {
		return entry(core, id, "resource_kind", "member", planeList, document{"anchor_class": class})
	}
	relation := func(ref semreg.DefinitionRef, coreType, orientation, from, to, cardinality, medium string, planeList []any) document {
		attributes := document{
			"core_type": coreType, "orientation": orientation, "from_kinds": planes(from), "to_kinds": planes(to),
			"cardinality": cardinality, "over_time": false, "basis_classes": planes("configuration"),
		}
		if medium != "" {
			attributes["medium"] = medium
		}
		return entry(pack, string(ref.ID), "relation", "member", planeList, attributes)
	}
	devicePlanes := planes("physical", "energy", "control", "provenance")
	doc["identification_schemes"] = []any{}
	doc["lenses"] = []any{document{
		"id": "lens.cut_hierarchy", "question": "What contains what?",
		"planes": planes("physical"), "root_kinds": planes(kindDevice), "max_depth": 4,
	}}
	elements := []any{
		kind(kindDevice, "equipment", devicePlanes),
		kind(kindPart, "component", planes("physical", "energy")),
		kind(kindZone, "function", planes("control")),
		kind("resource.site", "function", planes("physical", "energy")),
		relation(relDevicePart, "contains", "as_core", kindDevice, kindPart, "one_to_many", "", planes("physical", "energy")),
		relation(relSupplies, "supplies", "as_core", kindDevice, kindDevice, "many_to_many", "electrical", planes("energy")),
		relation(relInverse, "contains", "inverse", kindPart, kindDevice, "one_to_many", "", planes("physical")),
	}
	for i := 0; i < spans; i++ {
		ref := cutRef(spanDefinition(i))
		elements = append(elements, relation(ref, "supplies", "as_core", kindDevice, kindDevice, "many_to_many", "electrical", planes("energy")))
	}
	doc["elements"] = elements
	sortElements(doc)
	catalog, err := graph.Decode(encode(t, doc))
	if err != nil {
		t.Fatalf("Decode(read catalog) = %v", err)
	}
	return catalog
}

func spanDefinition(i int) string {
	return "cut.relation.span_" + twoDigits(i)
}

func twoDigits(i int) string {
	return string([]byte{byte('0' + i/10), byte('0' + i%10)})
}

var relInverse = cutRef("cut.relation.part_device")

func mustCut(t *testing.T, catalog *graph.Catalog, completeness graph.Completeness, assets ...cutAsset) (*graph.Cut, []semreg.Snapshot) {
	t.Helper()
	snaps := snapshots(t, assets...)
	cut, err := graph.AssembleCut(snaps, catalog, completeness)
	if err != nil {
		t.Fatalf("AssembleCut = %v", err)
	}
	return cut, snaps
}

func requireReadError(t *testing.T, err error, id semreg.ErrorID) {
	t.Helper()
	if semreg.ErrorIdentifier(err) != id {
		t.Fatalf("error = %v, want %s", err, id)
	}
}

func resourceIDs(entries []graph.ResourceEntry) []string {
	out := make([]string, len(entries))
	for i, entry := range entries {
		out[i] = string(entry.ResourceID)
	}
	return out
}

func TestListResources(t *testing.T) {
	catalog := readCatalog(t, 0)
	one := cutAsset{id: "asset:one", resources: []semreg.Resource{
		device("resource:m", "m"),
		device("resource:a", "a"),
		withState(device("resource:gone", "gone"), semreg.ResourceWithdrawn),
		part("resource:p", "resource:a", "p"),
	}, relations: []semreg.RelationInstance{qualified(relSupplies, "resource:a", "resource:m")}}
	two := cutAsset{id: "asset:two", resources: []semreg.Resource{device("resource:b", "b")}}
	cut, snaps := mustCut(t, catalog, graph.CutComplete, two, one)
	base := graph.ListRequest{PageSize: 10}
	page, err := graph.ListResources(cut, snaps, catalog, base)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resourceIDs(page.Resources), []string{"resource:a", "resource:b", "resource:m", "resource:p"}; !sameStrings(got, want) {
		t.Fatalf("resources = %v, want %v", got, want)
	}
	if page.Resources[0].AssetID != "asset:one" || page.Resources[1].AssetID != "asset:two" {
		t.Fatalf("homes = %s %s", page.Resources[0].AssetID, page.Resources[1].AssetID)
	}
	if page.CutID != cut.ID() || page.Catalog != catalog.Digest() || page.Completeness != graph.CutComplete {
		t.Fatalf("page cut = %s %s %s", page.CutID, page.Catalog, page.Completeness)
	}
	if page.NextPage != "" {
		t.Fatal("unexpected next page")
	}

	t.Run("kind and plane", func(t *testing.T) {
		kind := semreg.DefinitionID(kindPart)
		filtered, err := graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 10, Kind: &kind})
		if err != nil {
			t.Fatal(err)
		}
		if got := resourceIDs(filtered.Resources); !sameStrings(got, []string{"resource:p"}) {
			t.Fatalf("kind filter = %v", got)
		}
		plane := graph.PlaneEnergy
		energy, err := graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 10, Plane: &plane})
		if err != nil {
			t.Fatal(err)
		}
		if got := resourceIDs(energy.Resources); !sameStrings(got, []string{"resource:a", "resource:m"}) {
			t.Fatalf("plane filter = %v, want the energy endpoints", got)
		}
	})

	t.Run("pages carry one cut and union to the list", func(t *testing.T) {
		first, err := graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 1})
		if err != nil {
			t.Fatal(err)
		}
		if first.NextPage == "" || len(first.Resources) != 1 {
			t.Fatalf("first page = %d resources, next %q", len(first.Resources), first.NextPage)
		}
		var got []string
		token := ""
		seen := map[string]bool{}
		for pageNo := 0; pageNo < 6; pageNo++ {
			next, err := graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 1, PageToken: token})
			if err != nil {
				t.Fatal(err)
			}
			if next.CutID != cut.ID() || len(next.Members) != len(cut.Members()) {
				t.Fatalf("page %d cut = %s members %d", pageNo, next.CutID, len(next.Members))
			}
			for _, entry := range next.Resources {
				if seen[string(entry.ResourceID)] {
					t.Fatalf("duplicate %s", entry.ResourceID)
				}
				seen[string(entry.ResourceID)] = true
				got = append(got, string(entry.ResourceID))
			}
			token = next.NextPage
			if token == "" {
				break
			}
		}
		if !sameStrings(got, []string{"resource:a", "resource:b", "resource:m", "resource:p"}) {
			t.Fatalf("union = %v", got)
		}
	})

	t.Run("token of another request or cut", func(t *testing.T) {
		first, err := graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 1})
		if err != nil {
			t.Fatal(err)
		}
		// Fields of the listing identity: kind, plane, page_size. Each mutation
		// changes the request digest the token is bound to.
		kind := semreg.DefinitionID(kindDevice)
		plane := graph.PlanePhysical
		otherCut, otherSnaps := mustCut(t, catalog, graph.CutComplete, cutAsset{id: "asset:other", resources: []semreg.Resource{device("resource:z", "z")}})
		cases := []struct {
			name string
			req  graph.ListRequest
			snap []semreg.Snapshot
			cut  *graph.Cut
			want semreg.ErrorID
		}{
			{"kind", graph.ListRequest{PageSize: 1, PageToken: first.NextPage, Kind: &kind}, snaps, cut, semreg.RevisionConflict},
			{"plane", graph.ListRequest{PageSize: 1, PageToken: first.NextPage, Plane: &plane}, snaps, cut, semreg.RevisionConflict},
			{"page size", graph.ListRequest{PageSize: 2, PageToken: first.NextPage}, snaps, cut, semreg.RevisionConflict},
			{"cut", graph.ListRequest{PageSize: 1, PageToken: first.NextPage, CutID: idPtr(otherCut.ID())}, otherSnaps, otherCut, semreg.RevisionConflict},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := graph.ListResources(tc.cut, tc.snap, catalog, tc.req)
				requireReadError(t, err, tc.want)
			})
		}
		_, err = graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 1, PageToken: "not-a-token"})
		requireReadError(t, err, semreg.InvalidValue)
		_, err = graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 4097})
		requireReadError(t, err, semreg.BoundsExceeded)
		_, err = graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 0})
		requireReadError(t, err, semreg.InvalidValue)
	})

	t.Run("identical bytes", func(t *testing.T) {
		kind := semreg.DefinitionID(kindDevice)
		left, err := graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 2, Kind: &kind})
		if err != nil {
			t.Fatal(err)
		}
		right, err := graph.ListResources(cut, snaps, catalog, graph.ListRequest{PageSize: 2, Kind: &kind})
		if err != nil {
			t.Fatal(err)
		}
		a, err := left.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		b, err := right.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("bytes differ\n%s\n%s", a, b)
		}
	})

	t.Run("partial cut stays partial", func(t *testing.T) {
		partial, partialSnaps := mustCut(t, catalog, graph.CutPartial, one)
		page, err := graph.ListResources(partial, partialSnaps, catalog, graph.ListRequest{PageSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		if page.Completeness != graph.CutPartial || page.CutID != partial.ID() {
			t.Fatalf("completeness = %s", page.Completeness)
		}
	})
}

func TestCutRetention(t *testing.T) {
	if graph.MaxRetainedCuts != 8 || graph.CutRetention != 120*time.Second {
		t.Fatalf("retention bounds = %d %s", graph.MaxRetainedCuts, graph.CutRetention)
	}
	catalog := readCatalog(t, 0)
	var now time.Time
	now = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	store := graph.NewCutStore(func() time.Time { return now })
	_, err := store.ListResources(graph.ListRequest{PageSize: 1})
	requireReadError(t, err, semreg.RevisionConflict)

	keep := func(asset string, resources ...semreg.Resource) (*graph.Cut, []semreg.Snapshot) {
		t.Helper()
		if len(resources) == 0 {
			resources = []semreg.Resource{device("resource:d", "d")}
		}
		return mustCut(t, catalog, graph.CutComplete, cutAsset{id: semreg.AssetID(asset), resources: resources})
	}
	first, firstSnaps := keep("asset:keep-a", device("resource:a1", "a1"), device("resource:a2", "a2"))
	if err := store.Retain(first, []semreg.Snapshot{}, catalog); err == nil {
		t.Fatal("mismatched snapshots retained")
	}
	if err := store.Retain(nil, firstSnaps, catalog); semreg.ErrorIdentifier(err) != semreg.InvalidValue {
		t.Fatalf("nil cut = %v", err)
	}
	if err := store.Retain(first, firstSnaps, catalog); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListResources(graph.ListRequest{PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.CutID != first.ID() || page.NextPage == "" {
		t.Fatalf("newest page = %s next %q", page.CutID, page.NextPage)
	}
	direct, err := graph.ListResources(first, firstSnaps, catalog, graph.ListRequest{PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	left, _ := page.CanonicalJSON()
	right, _ := direct.CanonicalJSON()
	if !bytes.Equal(left, right) {
		t.Fatal("store listing bytes differ from the pure function")
	}

	t.Run("pinned cut that is not retained", func(t *testing.T) {
		missing := first.ID()
		missing = missing[:len(missing)-1] + semreg.Digest(otherHex(missing[len(missing)-1]))
		_, err := store.ListResources(graph.ListRequest{PageSize: 1, CutID: &missing})
		requireReadError(t, err, semreg.RevisionConflict)
	})

	t.Run("newest and a token of another cut", func(t *testing.T) {
		second, secondSnaps := keep("asset:keep-b")
		now = now.Add(time.Second)
		if err := store.Retain(second, secondSnaps, catalog); err != nil {
			t.Fatal(err)
		}
		newest, err := store.ListResources(graph.ListRequest{PageSize: 1})
		if err != nil {
			t.Fatal(err)
		}
		if newest.CutID != second.ID() {
			t.Fatalf("unpinned = %s, want newest", newest.CutID)
		}
		pinned, err := store.ListResources(graph.ListRequest{PageSize: 1, CutID: idPtr(first.ID())})
		if err != nil {
			t.Fatal(err)
		}
		if pinned.CutID != first.ID() {
			t.Fatalf("pin substituted %s", pinned.CutID)
		}
		_, err = store.ListResources(graph.ListRequest{PageSize: 1, CutID: idPtr(second.ID()), PageToken: page.NextPage})
		requireReadError(t, err, semreg.RevisionConflict)
	})

	t.Run("demoted cut expires at 120 seconds", func(t *testing.T) {
		demotedAt := now
		now = demotedAt.Add(graph.CutRetention - time.Nanosecond)
		still, err := store.ListResources(graph.ListRequest{PageSize: 1, CutID: idPtr(first.ID())})
		if err != nil || still.CutID != first.ID() {
			t.Fatalf("within retention: %v %s", err, still.CutID)
		}
		now = demotedAt.Add(graph.CutRetention)
		_, err = store.ListResources(graph.ListRequest{PageSize: 1, CutID: idPtr(first.ID())})
		requireReadError(t, err, semreg.RevisionConflict)
		newest, err := store.ListResources(graph.ListRequest{PageSize: 1})
		if err != nil || newest.CutID == "" {
			t.Fatalf("newest after expiry: %v", err)
		}
	})

	t.Run("at most 8 and a token keeps its cut", func(t *testing.T) {
		var clock time.Time
		clock = time.Date(2000, 6, 1, 0, 0, 0, 0, time.UTC)
		limited := graph.NewCutStore(func() time.Time { return clock })
		type held struct {
			cut   *graph.Cut
			snaps []semreg.Snapshot
		}
		cuts := make([]held, 0, 9)
		for i := 0; i < 9; i++ {
			asset := "asset:cap-" + twoDigits(i)
			resources := []semreg.Resource{device("resource:cap", "cap")}
			if i == 0 {
				resources = []semreg.Resource{device("resource:c0", "c0"), device("resource:c1", "c1")}
			}
			cut, snaps := keep(asset, resources...)
			cuts = append(cuts, held{cut, snaps})
			if err := limited.Retain(cut, snaps, catalog); err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				listed, err := limited.ListResources(graph.ListRequest{PageSize: 1})
				if err != nil || listed.NextPage == "" {
					t.Fatalf("token issue: %v %q", err, listed.NextPage)
				}
			}
			clock = clock.Add(time.Second)
		}
		_, err := limited.ListResources(graph.ListRequest{PageSize: 1, CutID: idPtr(cuts[1].cut.ID())})
		requireReadError(t, err, semreg.RevisionConflict)
		kept, err := limited.ListResources(graph.ListRequest{PageSize: 1, CutID: idPtr(cuts[0].cut.ID())})
		if err != nil || kept.CutID != cuts[0].cut.ID() {
			t.Fatalf("token-named cut: %v", err)
		}
		newest, err := limited.ListResources(graph.ListRequest{PageSize: 1})
		if err != nil || newest.CutID != cuts[8].cut.ID() {
			t.Fatalf("newest after cap = %s", newest.CutID)
		}
		clock = clock.Add(graph.CutRetention)
		_, err = limited.ListResources(graph.ListRequest{PageSize: 1, CutID: idPtr(cuts[0].cut.ID())})
		requireReadError(t, err, semreg.RevisionConflict)
		stillNewest, err := limited.ListResources(graph.ListRequest{PageSize: 1, CutID: idPtr(cuts[8].cut.ID())})
		if err != nil || stillNewest.CutID != cuts[8].cut.ID() {
			t.Fatalf("newest expired: %v", err)
		}
	})

	t.Run("concurrent retain and list", func(t *testing.T) {
		shared := graph.NewCutStore(nil)
		var group sync.WaitGroup
		for i := 0; i < 4; i++ {
			group.Add(1)
			asset := "asset:race-" + twoDigits(i)
			cut, snaps := keep(asset)
			go func() {
				defer group.Done()
				if err := shared.Retain(cut, snaps, catalog); err != nil {
					t.Errorf("retain: %v", err)
					return
				}
				if _, err := shared.ListResources(graph.ListRequest{PageSize: 4, CutID: idPtr(cut.ID())}); err != nil {
					t.Errorf("list: %v", err)
				}
			}()
		}
		group.Wait()
	})
}

func idPtr(id semreg.Digest) *semreg.Digest { return &id }

func otherHex(b byte) string {
	if b == '0' {
		return "1"
	}
	return "0"
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
