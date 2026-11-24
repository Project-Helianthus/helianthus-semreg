package tree_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func threeResourceWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.cuts.add(siteAsset("asset.a", "res:site", "res:grid1", "res:grid2"))
	return w
}

func lensRequest(pageSize int) graph.LensRequest {
	return graph.LensRequest{
		Lens: lensHierarchy, Entry: graph.EntryPoint{Kind: graph.EntryLensRoots},
		Direction: graph.DirectionBoth, Depth: 4, PageSize: pageSize,
	}
}

func idsOf(page graph.ResourcePage) []string {
	out := []string{}
	for _, r := range page.Resources {
		out = append(out, string(r.ResourceID))
	}
	return out
}

func TestGraphReadsCallTheFrozenFunctionsUnchanged(t *testing.T) {
	w := threeResourceWorld(t)
	cut := w.capture()
	view := w.cuts.views[mustDigest(t, cut)]

	listed, err := cut.ListResources(ctx, graph.ListRequest{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	want, err := graph.ListResources(view.Cut, view.Members, view.Catalog, graph.ListRequest{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(listed.Page, want) {
		t.Fatalf("listing differs from graph.ListResources:\n%+v\n%+v", listed.Page, want)
	}
	if got := idsOf(listed.Page); !reflect.DeepEqual(got, []string{"res:grid1", "res:grid2", "res:site"}) {
		t.Fatalf("listing = %v", got)
	}

	request := graph.TraversalRequest{Entry: graph.EntryPoint{Kind: graph.EntrySites}, Lens: lensHierarchy, Direction: graph.DirectionBoth, Depth: 4, PageSize: 10}
	traversed, err := cut.Traverse(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	wantTraversal, err := graph.Traverse(view.Cut, view.Members, view.Catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(traversed.Page, wantTraversal) {
		t.Fatalf("traversal differs from graph.Traverse")
	}

	lens, err := cut.RetrieveLens(ctx, lensRequest(10))
	if err != nil {
		t.Fatal(err)
	}
	direct := lensRequest(10)
	direct.Members = view.Members
	wantLens, err := graph.RetrieveLens(view.Store, view.Catalog, direct, view.Evaluation)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lens, wantLens) {
		t.Fatal("lens differs from graph.RetrieveLens")
	}

	catalog, err := cut.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Digest != view.Catalog.Digest() || !reflect.DeepEqual(catalog.Planes, view.Catalog.Planes()) || !reflect.DeepEqual(catalog.Lenses, view.Catalog.Lenses()) || len(catalog.Entries) != len(view.Catalog.Elements()) {
		t.Fatal("catalog differs from the frozen catalog")
	}
	unpinned, err := w.tree.ListResources(ctx, graph.ListRequest{PageSize: 10})
	if err != nil || !reflect.DeepEqual(unpinned.Page.Resources, listed.Page.Resources) {
		t.Fatalf("an unpinned read uses the newest cut: %v", err)
	}
	if _, err := cut.ListResources(ctx, graph.ListRequest{PageSize: graph.MaxPageSize + 1}); semreg.ErrorIdentifier(err) != semreg.BoundsExceeded {
		t.Fatalf("a bound of the frozen read = %v, want bounds_exceeded", err)
	}
}

func mustDigest(t *testing.T, cut *tree.TreeCut) semreg.Digest {
	t.Helper()
	id := string(cut.ID())
	return semreg.Digest(id[:strings.LastIndex(id, ":")])
}

func TestPinnedReadNamesTheRevisionWhenThePinnedCutIsGone(t *testing.T) {
	w := threeResourceWorld(t)
	missing := semreg.Digest("sha256:" + strings.Repeat("0", 64))
	_, err := w.tree.ListResources(ctx, graph.ListRequest{CutID: &missing, PageSize: 5})
	coded := mustCode(t, err, semreg.RevisionConflict)
	if !strings.Contains(coded.Detail, string(missing)) {
		t.Fatalf("revision_conflict does not name the pinned revision: %q", coded.Detail)
	}
	cut := w.capture()
	if _, err := cut.Traverse(ctx, graph.TraversalRequest{CutID: &missing, Entry: graph.EntryPoint{Kind: graph.EntrySites}, Lens: lensHierarchy, Direction: graph.DirectionBoth, Depth: 1, PageSize: 5}); semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
		t.Fatalf("pinned traversal = %v", err)
	}
	lens := lensRequest(5)
	lens.CutID = &missing
	if _, err := cut.RetrieveLens(ctx, lens); semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
		t.Fatalf("pinned lens = %v", err)
	}
	pinnedOther, err := w.tree.ListResources(ctx, graph.ListRequest{CutID: idPtr(cut), PageSize: 5})
	if err != nil || pinnedOther.Page.CutID != mustDigest(t, cut) {
		t.Fatalf("a pin that is retained is served: %v", err)
	}
}

func idPtr(cut *tree.TreeCut) *semreg.Digest {
	id := string(cut.ID())
	d := semreg.Digest(id[:strings.LastIndex(id, ":")])
	return &d
}

func TestExpiredPinnedReadThroughTheTreeEntryPoint(t *testing.T) {
	w := threeResourceWorld(t)
	cut := w.capture()
	first := mustDigest(t, cut)
	page, err := cut.ListResources(ctx, graph.ListRequest{PageSize: 1})
	if err != nil || page.Page.NextPage == "" {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	w.cuts.add(equipmentAsset("asset.b", "res:eq-b"))
	w.capture()
	w.cuts.advance(graph.CutRetention + time.Second)
	w.capture()

	_, err = cut.ListResources(ctx, graph.ListRequest{PageSize: 1, PageToken: page.Page.NextPage})
	coded := mustCode(t, err, semreg.RevisionConflict)
	if !strings.Contains(coded.Detail, string(first)) {
		t.Fatalf("expired pin does not name the revision: %q", coded.Detail)
	}
	expiredAt := w.cuts.now
	w.cuts.expired[first] = expiredAt
	other := w.capture()
	pin := first
	_, err = other.ListResources(ctx, graph.ListRequest{CutID: &pin, PageSize: 1})
	coded = mustCode(t, err, semreg.RevisionConflict)
	if !strings.Contains(coded.Detail, string(first)) || !strings.Contains(coded.Detail, "expired at "+expiredAt.UTC().Format(time.RFC3339Nano)) {
		t.Fatalf("an expired pin names its revision and expiry: %q", coded.Detail)
	}
	_, err = w.tree.RetrieveLens(ctx, graph.LensRequest{CutID: &pin, Lens: lensHierarchy, Entry: graph.EntryPoint{Kind: graph.EntryLensRoots}, Direction: graph.DirectionBoth, Depth: 1, PageSize: 1})
	mustCode(t, err, semreg.RevisionConflict)
}

func TestPageContinuationThroughAnInterveningCommit(t *testing.T) {
	w := threeResourceWorld(t)
	cut := w.capture()
	first, err := cut.ListResources(ctx, graph.ListRequest{PageSize: 2})
	if err != nil || first.Page.NextPage == "" {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	w.cuts.add(equipmentAsset("asset.b", "res:eq-b"))
	newest := w.capture()
	if mustDigest(t, newest) == mustDigest(t, cut) {
		t.Fatal("the commit did not change the cut")
	}
	second, err := cut.ListResources(ctx, graph.ListRequest{PageSize: 2, PageToken: first.Page.NextPage})
	if err != nil {
		t.Fatalf("page two after a commit: %v", err)
	}
	if second.Page.CutID != first.Page.CutID || second.Page.Catalog != first.Page.Catalog {
		t.Fatalf("page two comes from another cut or catalog: %+v %+v", second.Page.CutID, first.Page.CutID)
	}
	if got := append(idsOf(first.Page), idsOf(second.Page)...); !reflect.DeepEqual(got, []string{"res:grid1", "res:grid2", "res:site"}) {
		t.Fatalf("pages = %v", got)
	}
	fresh, err := w.tree.ListResources(ctx, graph.ListRequest{PageSize: 10})
	if err != nil || fresh.Page.CutID == first.Page.CutID {
		t.Fatalf("an unpinned read serves the newest cut: %v", err)
	}
	moved := mustDigest(t, newest)
	if _, err := cut.ListResources(ctx, graph.ListRequest{CutID: &moved, PageSize: 2, PageToken: first.Page.NextPage}); semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
		t.Fatalf("a token used against another cut = %v", err)
	}
	if _, err := cut.Traverse(ctx, graph.TraversalRequest{Entry: graph.EntryPoint{Kind: graph.EntrySites}, Lens: lensHierarchy, Direction: graph.DirectionBoth, Depth: 4, PageSize: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestLensPageTwoAfterClockAdvanceAndAfterPublication(t *testing.T) {
	w := threeResourceWorld(t)
	cut := w.capture()
	first, err := cut.RetrieveLens(ctx, lensRequest(1))
	if err != nil || first.NextPage == nil {
		t.Fatalf("lens page one = %+v, %v", first.Resources, err)
	}
	w.cuts.advanceEvaluation()
	w.cuts.add(equipmentAsset("asset.b", "res:eq-b"))
	w.capture()
	w.cuts.advanceEvaluation()

	request := lensRequest(1)
	request.PageToken = *first.NextPage
	second, err := cut.RetrieveLens(ctx, request)
	if err != nil {
		t.Fatalf("lens page two after the clock advanced and a publication: %v", err)
	}
	if second.Cut.CutID != first.Cut.CutID || second.Catalog != first.Catalog {
		t.Fatal("page two was not answered from the first page's cut and catalog")
	}
	if len(second.Resources) != 1 || second.Resources[0].Resource.ResourceID == first.Resources[0].Resource.ResourceID {
		t.Fatalf("page two = %+v", second.Resources)
	}
}

func TestPartialCutNamesTheOmittedAssetAndItsReason(t *testing.T) {
	w := threeResourceWorld(t)
	w.cuts.add(equipmentAsset("asset.stopped", "res:eq-stopped"))
	w.cuts.stopped["asset.stopped"] = true
	cut := w.capture()

	want := []graph.NotIncludedAsset{{AssetID: "asset.stopped", Reason: graph.NotIncludedSourceUnavailable}}
	listed, err := cut.ListResources(ctx, graph.ListRequest{PageSize: 10})
	if err != nil || !reflect.DeepEqual(listed.NotIncluded, want) || listed.Page.Completeness != graph.CutPartial {
		t.Fatalf("list = %+v %v", listed.NotIncluded, err)
	}
	traversed, err := cut.Traverse(ctx, graph.TraversalRequest{Entry: graph.EntryPoint{Kind: graph.EntrySites}, Lens: lensHierarchy, Direction: graph.DirectionBoth, Depth: 4, PageSize: 10})
	if err != nil || !reflect.DeepEqual(traversed.NotIncluded, want) {
		t.Fatalf("traverse = %+v %v", traversed.NotIncluded, err)
	}
	lens, err := cut.RetrieveLens(ctx, lensRequest(10))
	if err != nil || !reflect.DeepEqual(lens.Cut.NotIncluded, want) || lens.Cut.Completeness != graph.CutPartial {
		t.Fatalf("lens = %+v %v", lens.Cut, err)
	}
	for _, id := range idsOf(listed.Page) {
		if id == "res:eq-stopped" {
			t.Fatal("the stopped asset's resource is listed")
		}
	}
	complete := threeResourceWorld(t).capture()
	again, err := complete.ListResources(ctx, graph.ListRequest{PageSize: 10})
	if err != nil || len(again.NotIncluded) != 0 || again.Page.Completeness != graph.CutComplete {
		t.Fatalf("a complete cut has no omissions: %+v %v", again.NotIncluded, err)
	}
}
