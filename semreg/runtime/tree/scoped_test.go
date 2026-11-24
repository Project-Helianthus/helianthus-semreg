package tree_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

var powerFact = semreg.DefinitionRef{Pack: semreg.PackRef{ID: "pack.test", Version: "1.0.0"}, ID: "fact.power", Version: "1.0.0"}

func scopedWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.cuts.add(siteAsset("asset.a", "res:site", "res:grid1", "res:grid2"))
	w.cuts.add(equipmentAsset("asset.b", "res:eq-b"))
	return w
}

func TestScopedPartialCutLensKeepsItsMembersAndSumIsPartialCut(t *testing.T) {
	w := scopedWorld(t)
	w.cuts.add(equipmentAsset("asset.stopped", "res:eq-stopped"))
	w.cuts.stopped["asset.stopped"] = true
	input := w.tree.SemanticInput(driver.Filter{Assets: []semreg.AssetID{"asset.a", "asset.stopped"}})
	cut, err := input.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if cut.Ref.Completeness != graph.CutPartial || len(cut.Snapshots) != 1 || cut.Snapshots[0].AssetID != "asset.a" {
		t.Fatalf("scoped cut = %+v", cut.Ref)
	}

	lens, err := input.RetrieveLens(ctx, cut.Ref, lensRequest(1))
	if err != nil {
		t.Fatalf("a scoped partial cut reads: %v", err)
	}
	want := []graph.NotIncludedAsset{{AssetID: "asset.stopped", Reason: graph.NotIncludedSourceUnavailable}}
	if len(lens.Resources) != 1 || lens.NextPage == nil || !reflect.DeepEqual(lens.Cut.NotIncluded, want) || lens.Cut.Completeness != graph.CutPartial {
		t.Fatalf("lens = %d resources, next=%v, cut=%+v", len(lens.Resources), lens.NextPage, lens.Cut)
	}
	next := lensRequest(1)
	next.PageToken = *lens.NextPage
	page2, err := input.RetrieveLens(ctx, cut.Ref, next)
	if err != nil || len(page2.Resources) != 1 || page2.Cut.CutID != lens.Cut.CutID {
		t.Fatalf("page two = %+v, %v", page2.Resources, err)
	}

	sum, err := input.SumMembers(ctx, cut.Ref, lensRequest(1), powerFact, "res:site")
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Withheld || sum.Reason != graph.SumPartialCut || sum.Value != nil {
		t.Fatalf("sum on a partial cut = %+v, want partial_cut", sum)
	}
}

func TestScopedLensEntryOutsideTheCapturedFilterIsDanglingReference(t *testing.T) {
	w := scopedWorld(t)
	request := graph.LensRequest{
		Lens: lensHierarchy, Entry: graph.EntryPoint{Kind: graph.EntryResource, Resource: "res:eq-b"},
		Direction: graph.DirectionBoth, Depth: 2, PageSize: 5,
	}
	wide := w.tree.SemanticInput(driver.Filter{})
	whole, err := wide.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wide.RetrieveLens(ctx, whole.Ref, request); err != nil {
		t.Fatalf("the same request is valid in the wider capture: %v", err)
	}
	narrow := w.tree.SemanticInput(driver.Filter{Assets: []semreg.AssetID{"asset.a"}})
	cut, err := narrow.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := narrow.RetrieveLens(ctx, cut.Ref, request)
	mustCode(t, err, semreg.DanglingReference)
	if len(result.Resources) != 0 {
		t.Fatal("a lens result was returned for an entry outside the captured filter")
	}
	if _, err := narrow.Capture(ctx, driver.Filter{Assets: []semreg.AssetID{"asset.b"}}); semreg.ErrorIdentifier(err) != semreg.DanglingReference {
		t.Fatalf("a capture cannot widen the scope: %v", err)
	}
}

func TestScopedSumScopeOutsideTheCapturedFilterIsDanglingReference(t *testing.T) {
	w := scopedWorld(t)
	narrow := w.tree.SemanticInput(driver.Filter{Assets: []semreg.AssetID{"asset.a"}})
	cut, err := narrow.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := narrow.RetrieveLens(ctx, cut.Ref, lensRequest(5)); err != nil {
		t.Fatalf("the lens entry is valid in scope: %v", err)
	}
	sum, err := narrow.SumMembers(ctx, cut.Ref, lensRequest(5), powerFact, "res:eq-b")
	mustCode(t, err, semreg.DanglingReference)
	if !reflect.DeepEqual(sum, graph.MemberSum{}) {
		t.Fatalf("a MemberSum was returned for a scope outside the captured filter: %+v", sum)
	}
	wide := w.tree.SemanticInput(driver.Filter{})
	whole, err := wide.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wide.SumMembers(ctx, whole.Ref, lensRequest(5), powerFact, "res:eq-b"); err != nil {
		t.Fatalf("the same scope is a resource of the wider capture: %v", err)
	}
	inScope, err := narrow.SumMembers(ctx, cut.Ref, lensRequest(5), powerFact, "res:site")
	if err != nil || (inScope.Withheld && inScope.Reason == graph.SumPartialCut) {
		t.Fatalf("a phase-only complete cut is not partial: %+v, %v", inScope, err)
	}
}

func TestScopedContinuationAfterSecondCaptureWithNewOmissionsAndLaterClock(t *testing.T) {
	w := scopedWorld(t)
	input := w.tree.SemanticInput(driver.Filter{})
	first, err := input.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	page1, err := input.RetrieveLens(ctx, first.Ref, lensRequest(1))
	if err != nil || page1.NextPage == nil {
		t.Fatalf("page one = %+v, %v", page1.Resources, err)
	}
	w.cuts.advanceEvaluation()
	w.cuts.declared["asset.pending"] = graph.NotIncludedSourceUnavailable
	second, err := input.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Ref.CutID != first.Ref.CutID {
		t.Fatal("the second capture was meant to share the first capture's digest")
	}
	if reflect.DeepEqual(second.Ref.NotIncluded, first.Ref.NotIncluded) {
		t.Fatal("the second capture was meant to carry new omissions")
	}
	next := lensRequest(1)
	next.PageToken = *page1.NextPage
	page2, err := input.RetrieveLens(ctx, first.Ref, next)
	if err != nil {
		t.Fatalf("page two of the first capture after a second capture: %v", err)
	}
	if page2.Cut.CutID != page1.Cut.CutID || len(page2.Cut.NotIncluded) != 0 || len(page2.Resources) != 1 ||
		page2.Cut.Completeness != page1.Cut.Completeness || page2.Cut.Completeness != graph.CutComplete {
		t.Fatalf("page two is not the first view's: %+v", page2.Cut)
	}
	if _, err := input.RetrieveLens(ctx, second.Ref, next); semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
		t.Fatalf("the first view's token against the second view = %v, want revision_conflict", err)
	}
	other, err := input.RetrieveLens(ctx, second.Ref, lensRequest(1))
	if err != nil || len(other.Cut.NotIncluded) != 1 {
		t.Fatalf("the second capture serves its own omissions: %+v, %v", other.Cut, err)
	}
}

func TestScopedReadAfterPendingPublicationBetweenCalls(t *testing.T) {
	w := scopedWorld(t)
	input := w.tree.SemanticInput(driver.Filter{Assets: []semreg.AssetID{"asset.a"}})
	cut, err := input.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	page1, err := input.RetrieveLens(ctx, cut.Ref, lensRequest(1))
	if err != nil || page1.NextPage == nil {
		t.Fatalf("page one: %v", err)
	}
	w.cuts.add(siteAsset("asset.c", "res:site-c", "res:grid-c"))
	if _, err := w.tree.Capture(ctx, tree.CutOptions{}); err != nil {
		t.Fatal(err)
	}
	next := lensRequest(1)
	next.PageToken = *page1.NextPage
	page2, err := input.RetrieveLens(ctx, cut.Ref, next)
	if err != nil || page2.Cut.CutID != cut.Ref.CutID {
		t.Fatalf("page two after a pending publication: %+v, %v", page2.Cut, err)
	}
	if _, err := input.RetrieveLens(ctx, graph.CutRef{CutID: cut.Ref.CutID}, next); semreg.ErrorIdentifier(err) != semreg.DanglingReference {
		t.Fatalf("a CutRef this session never captured = %v", err)
	}
}

func TestScopedContinuationAfterSecondCaptureWithOnlyALaterClock(t *testing.T) {
	w := scopedWorld(t)
	input := w.tree.SemanticInput(driver.Filter{})
	first, err := input.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	page1, err := input.RetrieveLens(ctx, first.Ref, lensRequest(1))
	if err != nil || page1.NextPage == nil {
		t.Fatalf("page one = %+v, %v", page1.Resources, err)
	}
	w.cuts.advanceEvaluation()
	second, err := input.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.Ref, first.Ref) {
		t.Fatal("the second capture was meant to yield the same CutRef")
	}
	next := lensRequest(1)
	next.PageToken = *page1.NextPage
	page2, err := input.RetrieveLens(ctx, first.Ref, next)
	if err != nil {
		t.Fatalf("page two after a later capture of the same CutRef: %v", err)
	}
	if page2.Cut.CutID != page1.Cut.CutID || len(page2.Resources) != 1 {
		t.Fatalf("page two = %+v", page2.Cut)
	}
	if _, err := input.RetrieveLens(ctx, second.Ref, next); err != nil {
		t.Fatalf("the same CutRef answers the same continuation: %v", err)
	}
}

func TestScopedContinuationOutlivingTheCutRetentionIsRevisionConflict(t *testing.T) {
	w := scopedWorld(t)
	input := w.tree.SemanticInput(driver.Filter{})
	first, err := input.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	page1, err := input.RetrieveLens(ctx, first.Ref, lensRequest(1))
	if err != nil || page1.NextPage == nil {
		t.Fatalf("page one = %+v, %v", page1.Resources, err)
	}
	w.cuts.add(siteAsset("asset.c", "res:site-c", "res:grid-c"))
	if _, err := w.tree.Capture(ctx, tree.CutOptions{}); err != nil {
		t.Fatal(err)
	}
	next := lensRequest(1)
	next.PageToken = *page1.NextPage
	if _, err := input.RetrieveLens(ctx, first.Ref, next); err != nil {
		t.Fatalf("a continuation inside the retention: %v", err)
	}
	w.cuts.advance(graph.CutRetention + time.Second)
	_, err = input.RetrieveLens(ctx, first.Ref, next)
	coded := mustCode(t, err, semreg.RevisionConflict)
	if !strings.Contains(coded.Detail, string(first.Ref.CutID)) {
		t.Fatalf("revision_conflict does not name the revision: %q", coded.Detail)
	}
	if _, err := input.SumMembers(ctx, first.Ref, lensRequest(5), powerFact, "res:site"); semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
		t.Fatalf("a sum on an expired capture = %v", err)
	}
	if _, err := input.Capture(ctx, driver.Filter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := input.RetrieveLens(ctx, first.Ref, next); semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
		t.Fatalf("a continuation after the session dropped the view = %v", err)
	}
}
