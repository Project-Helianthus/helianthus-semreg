package graph_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func TestTraverse(t *testing.T) {
	if graph.MaxPageSize != 4096 || graph.MaxTraversalDepth != 32 || graph.MaxFanout != 256 ||
		graph.MaxTraversalResources != 4096 || graph.MaxTraversalRelations != 8192 || graph.MaxFrontierEntries != 4096 {
		t.Fatal("traversal bounds are not the contract bounds")
	}
	catalog := readCatalog(t, 0)
	diamond := cutAsset{id: "asset:diamond", resources: []semreg.Resource{
		device("resource:a", "a"), device("resource:b", "b"), device("resource:c", "c"), device("resource:d", "d"),
		device("resource:side", "side"),
		withState(device("resource:old", "old"), semreg.ResourceWithdrawn),
		withState(device("resource:ended", "ended"), semreg.ResourceRetired),
	}, relations: []semreg.RelationInstance{
		qualified(relSupplies, "resource:a", "resource:b"),
		qualified(relSupplies, "resource:a", "resource:c"),
		qualified(relSupplies, "resource:b", "resource:d"),
		qualified(relSupplies, "resource:c", "resource:d"),
		relation(relSupplies, "resource:a", "resource:side", semreg.LinkCandidate),
	}}
	cut, snaps := mustCut(t, catalog, graph.CutComplete, diamond)
	energy := graph.TraversalRequest{
		Entry:  graph.EntryPoint{Kind: graph.EntryResource, Resource: "resource:a"},
		Planes: []graph.PlaneID{graph.PlaneEnergy}, Definitions: []semreg.DefinitionRef{relSupplies},
		Direction: graph.DirectionOutgoing, Depth: 4, PageSize: 10,
	}
	page, err := graph.Traverse(cut, snaps, catalog, energy)
	if err != nil {
		t.Fatal(err)
	}
	if got := reachedDepths(page.Resources); !samePairs(got, []pair{{"resource:a", 0}, {"resource:b", 1}, {"resource:c", 1}, {"resource:d", 2}}) {
		t.Fatalf("diamond = %v", got)
	}
	if len(page.Relations) != 4 {
		t.Fatalf("relations = %d", len(page.Relations))
	}
	raw, err := page.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["path"]; ok {
		t.Fatal("traversal result has a path member")
	}

	t.Run("planes without definitions", func(t *testing.T) {
		planesOnly := energy
		planesOnly.Definitions = nil
		page, err := graph.Traverse(cut, snaps, catalog, planesOnly)
		if err != nil {
			t.Fatal(err)
		}
		if got := reachedDepths(page.Resources); !samePairs(got, []pair{{"resource:a", 0}, {"resource:b", 1}, {"resource:c", 1}, {"resource:d", 2}}) {
			t.Fatalf("energy planes = %v", got)
		}
		if len(page.Relations) != 4 {
			t.Fatalf("relations = %d", len(page.Relations))
		}
		physical := planesOnly
		physical.Planes = []graph.PlaneID{graph.PlanePhysical}
		page, err = graph.Traverse(cut, snaps, catalog, physical)
		if err != nil {
			t.Fatal(err)
		}
		if got := reachedDepths(page.Resources); !samePairs(got, []pair{{"resource:a", 0}}) {
			t.Fatalf("physical planes = %v", got)
		}
	})

	t.Run("smallest depth", func(t *testing.T) {
		shortcut := diamond
		shortcut.relations = append(append([]semreg.RelationInstance{}, diamond.relations...), qualified(relSupplies, "resource:a", "resource:d"))
		shortCut, shortSnaps := mustCut(t, catalog, graph.CutComplete, shortcut)
		page, err := graph.Traverse(shortCut, shortSnaps, catalog, energy)
		if err != nil {
			t.Fatal(err)
		}
		if got := reachedDepths(page.Resources); !samePairs(got, []pair{{"resource:a", 0}, {"resource:b", 1}, {"resource:c", 1}, {"resource:d", 1}}) {
			t.Fatalf("shortcut = %v", got)
		}
	})

	t.Run("depth frontier", func(t *testing.T) {
		chain := cutAsset{id: "asset:chain", resources: []semreg.Resource{
			device("resource:a", "a"), device("resource:b", "b"), device("resource:c", "c"),
		}, relations: []semreg.RelationInstance{
			qualified(relSupplies, "resource:a", "resource:b"),
			qualified(relSupplies, "resource:b", "resource:c"),
		}}
		chainCut, chainSnaps := mustCut(t, catalog, graph.CutComplete, chain)
		request := energy
		request.Depth = 1
		page, err := graph.Traverse(chainCut, chainSnaps, catalog, request)
		if err != nil {
			t.Fatal(err)
		}
		if got := reachedDepths(page.Resources); !samePairs(got, []pair{{"resource:a", 0}, {"resource:b", 1}}) {
			t.Fatalf("chain = %v", got)
		}
		if len(page.Frontier) != 1 || page.Frontier[0].ResourceID != "resource:b" || page.Frontier[0].Reason != graph.FrontierDepthLimit || page.Frontier[0].NotFollowed != 1 {
			t.Fatalf("frontier = %+v", page.Frontier)
		}
	})

	t.Run("fan-out frontier", func(t *testing.T) {
		resources := []semreg.Resource{device("resource:hub", "hub")}
		var relations []semreg.RelationInstance
		for i := 0; i < graph.MaxFanout+1; i++ {
			id := fmt.Sprintf("resource:t%03d", i)
			resources = append(resources, device(id, id))
			relations = append(relations, qualified(relSupplies, "resource:hub", id))
		}
		fanCut, fanSnaps := mustCut(t, catalog, graph.CutComplete, cutAsset{id: "asset:fan", resources: resources, relations: relations})
		request := energy
		request.Entry.Resource = "resource:hub"
		request.Depth = 1
		request.PageSize = graph.MaxFanout + 1
		page, err := graph.Traverse(fanCut, fanSnaps, catalog, request)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Resources) != graph.MaxFanout+1 {
			t.Fatalf("reached %d, want hub plus %d", len(page.Resources), graph.MaxFanout)
		}
		last := fmt.Sprintf("resource:t%03d", graph.MaxFanout)
		for _, resource := range page.Resources {
			if resource.ResourceID == semreg.ResourceID(last) {
				t.Fatalf("followed %s past the fan-out", last)
			}
		}
		if len(page.Frontier) != 1 || page.Frontier[0].ResourceID != "resource:hub" || page.Frontier[0].Reason != graph.FrontierFanoutLimit || page.Frontier[0].NotFollowed != 1 {
			t.Fatalf("frontier = %+v", page.Frontier)
		}
	})

	t.Run("frontier above its bound", func(t *testing.T) {
		const spans = 17
		const children = graph.MaxFrontierEntries + 1
		wide := readCatalog(t, spans)
		var defs []semreg.DefinitionRef
		for i := 0; i < spans; i++ {
			defs = append(defs, cutRef(spanDefinition(i)))
		}
		root := cutAsset{id: "asset:span-root", resources: []semreg.Resource{device("resource:root", "root")}}
		left := cutAsset{id: "asset:span-a"}
		right := cutAsset{id: "asset:span-b"}
		for i := 0; i < children; i++ {
			id := fmt.Sprintf("resource:c%04d", i)
			child := device(id, id)
			definition := defs[i/graph.MaxFanout]
			root.relations = append(root.relations, qualified(definition, "resource:root", id))
			back := qualified(defs[0], id, "resource:root")
			if i < graph.MaxPageSize {
				left.resources = append(left.resources, child)
				left.relations = append(left.relations, back)
			} else {
				right.resources = append(right.resources, child)
				right.relations = append(right.relations, back)
			}
		}
		wideCut, wideSnaps := mustCut(t, wide, graph.CutComplete, root, left, right)
		_, err := graph.Traverse(wideCut, wideSnaps, wide, graph.TraversalRequest{
			Entry:  graph.EntryPoint{Kind: graph.EntryResource, Resource: "resource:root"},
			Planes: []graph.PlaneID{graph.PlaneEnergy}, Definitions: defs,
			Direction: graph.DirectionOutgoing, Depth: 1, PageSize: 10,
		})
		if err == nil || semreg.ErrorIdentifier(err) != semreg.BoundsExceeded || !strings.Contains(err.Error(), "frontier") {
			t.Fatalf("frontier bound = %v", err)
		}
	})

	t.Run("non-current entry", func(t *testing.T) {
		for _, id := range []semreg.ResourceID{"resource:old", "resource:ended", "resource:missing"} {
			request := energy
			request.Entry.Resource = id
			_, err := graph.Traverse(cut, snaps, catalog, request)
			requireReadError(t, err, semreg.DanglingReference)
		}
	})

	t.Run("definition outside the requested planes", func(t *testing.T) {
		request := energy
		request.Planes = []graph.PlaneID{graph.PlanePhysical}
		_, err := graph.Traverse(cut, snaps, catalog, request)
		requireReadError(t, err, semreg.MembershipMissing)
		request.Definitions = []semreg.DefinitionRef{cutRef("cut.relation.missing")}
		request.Planes = []graph.PlaneID{graph.PlaneEnergy}
		_, err = graph.Traverse(cut, snaps, catalog, request)
		requireReadError(t, err, semreg.MembershipMissing)
	})

	t.Run("direction uses the definition from and to", func(t *testing.T) {
		attributes, ok := catalog.Relation(graph.PackElement(relInverse))
		if !ok || attributes.Orientation != graph.OrientationInverse {
			t.Fatal("inverse fixture is not inverse")
		}
		owned := cutAsset{id: "asset:inverse", resources: []semreg.Resource{
			device("resource:d", "d"), part("resource:p", "resource:d", "p"),
		}, relations: []semreg.RelationInstance{qualified(relInverse, "resource:p", "resource:d")}}
		ownedCut, ownedSnaps := mustCut(t, catalog, graph.CutComplete, owned)
		out := graph.TraversalRequest{
			Entry:  graph.EntryPoint{Kind: graph.EntryResource, Resource: "resource:p"},
			Planes: []graph.PlaneID{graph.PlanePhysical}, Definitions: []semreg.DefinitionRef{relInverse},
			Direction: graph.DirectionOutgoing, Depth: 2, PageSize: 10,
		}
		page, err := graph.Traverse(ownedCut, ownedSnaps, catalog, out)
		if err != nil {
			t.Fatal(err)
		}
		if got := reachedDepths(page.Resources); !samePairs(got, []pair{{"resource:p", 0}, {"resource:d", 1}}) {
			t.Fatalf("outgoing inverse = %v", got)
		}
		out.Entry.Resource = "resource:d"
		page, err = graph.Traverse(ownedCut, ownedSnaps, catalog, out)
		if err != nil {
			t.Fatal(err)
		}
		if got := reachedDepths(page.Resources); !samePairs(got, []pair{{"resource:d", 0}}) {
			t.Fatalf("outgoing from the to end = %v", got)
		}
		out.Entry.Resource = "resource:d"
		out.Direction = graph.DirectionIncoming
		page, err = graph.Traverse(ownedCut, ownedSnaps, catalog, out)
		if err != nil {
			t.Fatal(err)
		}
		if got := reachedDepths(page.Resources); !samePairs(got, []pair{{"resource:d", 0}, {"resource:p", 1}}) {
			t.Fatalf("incoming = %v", got)
		}
	})

	t.Run("lens roots and sites", func(t *testing.T) {
		plant := cutAsset{id: "asset:lens", resources: []semreg.Resource{
			device("resource:dev", "dev"), part("resource:bit", "resource:dev", "bit"),
			slotResource("resource:site", "resource.site", semreg.AnchorFunction, "", "site"),
		}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:dev", "resource:bit")}}
		lensCut, lensSnaps := mustCut(t, catalog, graph.CutComplete, plant)
		page, err := graph.Traverse(lensCut, lensSnaps, catalog, graph.TraversalRequest{
			Entry: graph.EntryPoint{Kind: graph.EntryLensRoots}, Lens: "lens.cut_hierarchy",
			Direction: graph.DirectionOutgoing, Depth: 2, PageSize: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := reachedDepths(page.Resources); !samePairs(got, []pair{{"resource:dev", 0}, {"resource:bit", 1}}) {
			t.Fatalf("lens roots = %v", got)
		}
		_, err = graph.Traverse(lensCut, lensSnaps, catalog, graph.TraversalRequest{
			Entry: graph.EntryPoint{Kind: graph.EntryLensRoots}, Lens: "lens.missing",
			Direction: graph.DirectionOutgoing, Depth: 1, PageSize: 10,
		})
		requireReadError(t, err, semreg.MembershipMissing)
		page, err = graph.Traverse(lensCut, lensSnaps, catalog, graph.TraversalRequest{
			Entry:  graph.EntryPoint{Kind: graph.EntrySites},
			Planes: []graph.PlaneID{graph.PlanePhysical}, Definitions: []semreg.DefinitionRef{relDevicePart},
			Direction: graph.DirectionOutgoing, Depth: 1, PageSize: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if page.EntryStatus != graph.EntryStatusNoSite && len(page.Resources) == 1 && page.Resources[0].ResourceID == "resource:site" {
			if page.Entry.Kind != graph.EntrySites {
				t.Fatalf("entry = %+v", page.Entry)
			}
		} else if len(page.Resources) != 1 || page.Resources[0].ResourceID != "resource:site" {
			t.Fatalf("sites = %+v status %s", page.Resources, page.EntryStatus)
		}
		empty, emptySnaps := mustCut(t, catalog, graph.CutComplete, cutAsset{id: "asset:empty", resources: []semreg.Resource{device("resource:only", "only")}})
		page, err = graph.Traverse(empty, emptySnaps, catalog, graph.TraversalRequest{
			Entry:  graph.EntryPoint{Kind: graph.EntrySites},
			Planes: []graph.PlaneID{graph.PlanePhysical}, Definitions: []semreg.DefinitionRef{relDevicePart},
			Direction: graph.DirectionOutgoing, Depth: 1, PageSize: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if page.EntryStatus != graph.EntryStatusNoSite || len(page.Resources) != 0 {
			t.Fatalf("absent site = %+v %s", page.Resources, page.EntryStatus)
		}
		_, err = graph.Traverse(lensCut, lensSnaps, catalog, graph.TraversalRequest{
			Entry: graph.EntryPoint{Kind: graph.EntryResource, Resource: "resource:dev"}, Lens: "lens.cut_hierarchy",
			Direction: graph.DirectionOutgoing, Depth: 5, PageSize: 10,
		})
		requireReadError(t, err, semreg.BoundsExceeded)
	})

	t.Run("request bounds and malformed token", func(t *testing.T) {
		tooDeep := energy
		tooDeep.Depth = graph.MaxTraversalDepth + 1
		_, err := graph.Traverse(cut, snaps, catalog, tooDeep)
		requireReadError(t, err, semreg.BoundsExceeded)
		tooWide := energy
		tooWide.PageSize = graph.MaxPageSize + 1
		_, err = graph.Traverse(cut, snaps, catalog, tooWide)
		requireReadError(t, err, semreg.BoundsExceeded)
		_, err = graph.Traverse(cut, snaps, catalog, graph.TraversalRequest{})
		requireReadError(t, err, semreg.InvalidEnum)
		bad := energy
		bad.PageToken = "pt1.%%%%"
		_, err = graph.Traverse(cut, snaps, catalog, bad)
		requireReadError(t, err, semreg.InvalidValue)
	})

	t.Run("token of another request", func(t *testing.T) {
		first := energy
		first.PageSize = 1
		page, err := graph.Traverse(cut, snaps, catalog, first)
		if err != nil || page.NextPage == "" {
			t.Fatalf("first page: %v %q", err, page.NextPage)
		}
		// Fields of the traversal identity, and the production check a mismatch hits.
		// entry resource, planes, definitions, direction, depth and page size each
		// change traverseIdentity, so boundCursor rejects the old token as
		// revision_conflict. The served cut is compared with the token cut id.
		cases := []graph.TraversalRequest{
			withToken(first, func(r *graph.TraversalRequest) { r.Entry.Resource = "resource:b" }),
			withToken(first, func(r *graph.TraversalRequest) { r.Planes = []graph.PlaneID{graph.PlanePhysical, graph.PlaneEnergy} }),
			withToken(first, func(r *graph.TraversalRequest) { r.Definitions = []semreg.DefinitionRef{relDevicePart} }),
			withToken(first, func(r *graph.TraversalRequest) { r.Direction = graph.DirectionIncoming }),
			withToken(first, func(r *graph.TraversalRequest) { r.Depth = 3 }),
			withToken(first, func(r *graph.TraversalRequest) { r.PageSize = 2 }),
		}
		for i, request := range cases {
			request.PageToken = page.NextPage
			_, err := graph.Traverse(cut, snaps, catalog, request)
			if semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
				t.Fatalf("mutation %d = %v", i, err)
			}
		}
		other, otherSnaps := mustCut(t, catalog, graph.CutComplete, cutAsset{id: "asset:else", resources: []semreg.Resource{device("resource:a", "a")}})
		moved := first
		moved.PageToken = page.NextPage
		moved.CutID = idPtr(other.ID())
		_, err = graph.Traverse(other, otherSnaps, catalog, moved)
		requireReadError(t, err, semreg.RevisionConflict)
		lensReq := graph.TraversalRequest{
			Entry: graph.EntryPoint{Kind: graph.EntryLensRoots}, Lens: "lens.cut_hierarchy",
			Direction: graph.DirectionOutgoing, Depth: 1, PageSize: 1,
		}
		lensCut, lensSnaps := mustCut(t, catalog, graph.CutComplete, cutAsset{id: "asset:lens-page", resources: []semreg.Resource{
			device("resource:dev", "dev"), device("resource:other", "other"),
		}, relations: []semreg.RelationInstance{qualified(relDevicePart, "resource:dev", "resource:bit")}})
		// A lens with no edge still pages the two roots when both are root kinds.
		lensCut, lensSnaps = mustCut(t, catalog, graph.CutComplete, cutAsset{id: "asset:lens-page", resources: []semreg.Resource{
			device("resource:dev", "dev"), device("resource:other", "other"),
		}})
		lensPage, err := graph.Traverse(lensCut, lensSnaps, catalog, lensReq)
		if err != nil || lensPage.NextPage == "" {
			t.Fatalf("lens page: %v %q resources %d", err, lensPage.NextPage, len(lensPage.Resources))
		}
		changed := lensReq
		changed.PageToken = lensPage.NextPage
		changed.Lens = "lens.other"
		_, err = graph.Traverse(lensCut, lensSnaps, catalog, changed)
		requireReadError(t, err, semreg.RevisionConflict)
		// entry kind is its own identity field. Sites with the same lens is a
		// valid request whose digest differs, so the lens-root token conflicts.
		sites := changed
		sites.Lens = lensReq.Lens
		sites.Entry = graph.EntryPoint{Kind: graph.EntrySites}
		_, err = graph.Traverse(lensCut, lensSnaps, catalog, sites)
		requireReadError(t, err, semreg.RevisionConflict)
	})
}

func TestTraverseDeterministic(t *testing.T) {
	catalog := readCatalog(t, 0)
	asset := cutAsset{id: "asset:stable", resources: []semreg.Resource{
		device("resource:a", "a"), device("resource:b", "b"), device("resource:c", "c"), device("resource:d", "d"),
	}, relations: []semreg.RelationInstance{
		qualified(relSupplies, "resource:a", "resource:c"),
		qualified(relSupplies, "resource:a", "resource:b"),
		qualified(relSupplies, "resource:c", "resource:d"),
		qualified(relSupplies, "resource:b", "resource:d"),
	}}
	cut, snaps := mustCut(t, catalog, graph.CutComplete, asset)
	request := graph.TraversalRequest{
		Entry:       graph.EntryPoint{Kind: graph.EntryResource, Resource: "resource:a"},
		Planes:      []graph.PlaneID{graph.PlaneEnergy, graph.PlanePhysical},
		Definitions: []semreg.DefinitionRef{relSupplies, relDevicePart},
		Direction:   graph.DirectionBoth, Depth: 3, PageSize: 2,
	}
	first, err := graph.Traverse(cut, snaps, catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	left, err := first.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	swapped := request
	swapped.Planes = []graph.PlaneID{graph.PlanePhysical, graph.PlaneEnergy}
	swapped.Definitions = []semreg.DefinitionRef{relDevicePart, relSupplies}
	second, err := graph.Traverse(cut, snaps, catalog, swapped)
	if err != nil {
		t.Fatal(err)
	}
	right, err := second.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatalf("bytes differ\n%s\n%s", left, right)
	}
	again, err := graph.Traverse(cut, snaps, catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	third, err := again.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, third) {
		t.Fatal("repeat differed")
	}
}

type pair struct {
	id    string
	depth int
}

func reachedDepths(resources []graph.ReachedResource) []pair {
	out := make([]pair, len(resources))
	for i, resource := range resources {
		out[i] = pair{string(resource.ResourceID), resource.Depth}
	}
	return out
}

func samePairs(got, want []pair) bool {
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

func withToken(base graph.TraversalRequest, edit func(*graph.TraversalRequest)) graph.TraversalRequest {
	edit(&base)
	return base
}
