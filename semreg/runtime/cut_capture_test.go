package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/sources"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func TestCaptureInstallationCutTwiceMatchesFixture(t *testing.T) {
	first := installationTopology(t)
	second := installationTopology(t)
	if first.text != second.text {
		t.Fatalf("two lifecycles differ\n%s\n---\n%s", first.text, second.text)
	}
	raw, err := os.ReadFile("testdata/installation-cut.txt")
	if err != nil {
		t.Fatalf("fixture: %v\n%s", err, first.text)
	}
	if string(raw) != first.text {
		t.Fatalf("fixture differs\n%s", first.text)
	}
	if first.view.Ref.Completeness != graph.CutComplete || len(first.view.Omissions) != 0 {
		t.Fatalf("completeness %s omissions %v", first.view.Ref.Completeness, first.view.Omissions)
	}
	stopped := false
	for _, node := range first.view.Nodes {
		if node.Key.Instance == "unit.three" && node.Kind == "resource.equipment" {
			stopped = true
		}
		if node.Kind != "resource.site" && node.Kind != "resource.equipment" {
			t.Fatalf("unexpected kind %s", node.Kind)
		}
	}
	if !stopped {
		t.Fatal("the stopped instance has no link node")
	}
	if len(first.view.Overlay) != 3 {
		t.Fatalf("relays = %d", len(first.view.Overlay))
	}
	snap, ok := snapOf(first.view.Members, "asset:unit.one")
	if !ok || coefficientOf(snap, "candidate:dhw.one") != "32165" {
		t.Fatalf("published fact = %s present %v", coefficientOf(snap, "candidate:dhw.one"), ok)
	}
	for _, omission := range first.view.Omissions {
		if omission.Reason != graph.NotIncludedSourceUnavailable && omission.Reason != graph.NotIncludedUngraphed && omission.Reason != graph.NotIncludedExcluded {
			t.Fatalf("reason %s", omission.Reason)
		}
	}
}

type topologyResult struct {
	text string
	view View
}

func installationTopology(t *testing.T) topologyResult {
	t.Helper()
	clock := newTestClock()
	svc := openService(t, clock)
	if err := svc.Install(context.Background(), installationPlan(t)); err != nil {
		t.Fatal(err)
	}
	device := nodeOf(svc, "unit.one")
	if device.ID == "" {
		t.Fatal("unit.one has no node")
	}
	session := openPlain(t, svc, "unit.one", "asset:unit.one", "source:unit.one", "epoch:unit.one", "1")
	fact := observedFact(session.spec, "candidate:dhw.one", "binding:unit.one:g1", "1", "32165", clock.Now(), 10)
	publishFact(t, session, "binding:unit.one:g1", declareFact(device, device, "path.dhw", 30, fact), fact)
	view, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return topologyResult{text: topologyText(view), view: view}
}

func TestCapturePagesKeepCutIDWhilePublishing(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	if err := svc.Install(context.Background(), installationPlan(t)); err != nil {
		t.Fatal(err)
	}
	view, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	request := graph.LensRequest{
		Lens: "lens.equipment_composition", Entry: graph.EntryPoint{Kind: graph.EntryLensRoots},
		Direction: graph.DirectionOutgoing, Depth: 4, PageSize: 1,
	}
	first, err := svc.RetrieveLens(context.Background(), view.Ref, request)
	if err != nil {
		t.Fatal(err)
	}
	session := openPlain(t, svc, "unit.one", "asset:unit.one", "source:unit.one", "epoch:unit.one", "1")
	fact := observedFact(session.spec, "candidate:dhw.one", "binding:unit.one:g1", "1", "32165", clock.Now(), 10)
	device := nodeOf(svc, "unit.one")
	publishFact(t, session, "binding:unit.one:g1", declareFact(device, device, "path.dhw", 30, fact), fact)
	if first.NextPage == nil {
		t.Fatal("the first lens page has no continuation")
	}
	request.PageToken = *first.NextPage
	second, err := svc.RetrieveLens(context.Background(), view.Ref, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cut.CutID != view.Ref.CutID || second.Cut.CutID != view.Ref.CutID {
		t.Fatalf("pages %s %s want %s", first.Cut.CutID, second.Cut.CutID, view.Ref.CutID)
	}
	later, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if later.Ref.CutID == view.Ref.CutID {
		t.Fatal("the later commit did not change the newest cut")
	}
}

func TestCapturePinnedReadNamesRevision(t *testing.T) {
	t.Run("count cap", func(t *testing.T) {
		clock := newTestClock()
		svc, session, device := factService(t, clock)
		first := publishAndCapture(t, svc, session, device, "1", "32165", clock.Now())
		cursor := first.Cursor
		for i := 2; i <= 9; i++ {
			clock.advance(time.Nanosecond)
			publishRevision(t, session, device, strconv.Itoa(i), strconv.Itoa(32000+i), clock.Now())
			if _, err := svc.CaptureView(context.Background(), driver.Filter{}); err != nil {
				t.Fatal(err)
			}
		}
		_, err := svc.Lookup(context.Background(), first.Ref)
		requireIdent(t, err, v1.RevisionConflict)
		if strings.Contains(err.Error(), "expired at") || !strings.Contains(err.Error(), string(first.Ref.CutID)) {
			t.Fatal(err)
		}
		changes, err := svc.Resume(context.Background(), cursor, driver.Filter{})
		if err != nil || changes.Resync || len(changes.Commits) == 0 {
			t.Fatalf("history %+v %v", changes, err)
		}
	})
	t.Run("unknown pin", func(t *testing.T) {
		clock := newTestClock()
		svc, session, device := factService(t, clock)
		view := publishAndCapture(t, svc, session, device, "1", "32165", clock.Now())
		sum := sha256.Sum256([]byte("pin.unknown"))
		view.Ref.CutID = v1.Digest("sha256:" + hex.EncodeToString(sum[:]))
		_, err := svc.Lookup(context.Background(), view.Ref)
		requireIdent(t, err, v1.RevisionConflict)
		if strings.Contains(err.Error(), "expired at") || !strings.Contains(err.Error(), string(view.Ref.CutID)) {
			t.Fatal(err)
		}
		publishRevision(t, session, device, "2", "32265", clock.Now())
		changes, err := svc.Resume(context.Background(), view.Cursor, driver.Filter{})
		if err != nil || changes.Resync || len(changes.Commits) != 1 {
			t.Fatalf("history %+v %v", changes, err)
		}
	})
	t.Run("time expiry", func(t *testing.T) {
		clock := newTestClock()
		svc, session, device := factService(t, clock)
		first := publishAndCapture(t, svc, session, device, "1", "32165", clock.Now())
		demoted := clock.Now()
		publishRevision(t, session, device, "2", "32265", clock.Now())
		if _, err := svc.CaptureView(context.Background(), driver.Filter{}); err != nil {
			t.Fatal(err)
		}
		expiry := demoted.Add(graph.CutRetention)
		clock.advance(graph.CutRetention)
		_, err := svc.Lookup(context.Background(), first.Ref)
		requireIdent(t, err, v1.RevisionConflict)
		if !strings.Contains(err.Error(), string(first.Ref.CutID)) || !strings.Contains(err.Error(), "expired at "+expiry.UTC().Format(time.RFC3339Nano)) {
			t.Fatal(err)
		}
		changes, err := svc.Resume(context.Background(), first.Cursor, driver.Filter{})
		if err != nil || changes.Resync || len(changes.Commits) == 0 {
			t.Fatalf("history %+v %v", changes, err)
		}
	})
}

func TestCaptureCommitLandsOnce(t *testing.T) {
	clock := newTestClock()
	svc, session, device := factService(t, clock)
	fact := observedFact(session.spec, "candidate:dhw.once", "binding:once:g1", "1", "32165", clock.Now(), 10)
	source := declareFact(device, device, "path.once", 30, fact)
	var ready sync.WaitGroup
	ready.Add(1)
	go func() {
		ready.Wait()
		commit(t, session, func(b *v1.PublicationBatch) {
			b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(session.spec, "registry:once")}
			b.BindingUpserts = []v1.NativeBinding{bindingOf(session.spec, "binding:once:g1", "native:once")}
			b.IdentityLinkUpserts = []v1.IdentityLink{linkOf(session.spec, "binding:once:g1", "basis:once", nil)}
			b.FactUpserts = []v1.FactCandidate{fact}
		}, func(tx *transaction) {
			if err := tx.DeclareSource(source); err != nil {
				t.Error(err)
			}
			if err := tx.StageSample(driver.Sample{From: source, Candidate: fact}); err != nil {
				t.Error(err)
			}
		})
	}()
	ready.Done()
	view, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	changes, err := svc.Resume(ctx, view.Cursor, driver.Filter{})
	inCut := factIn(view.Members, "candidate:dhw.once")
	inReplay := false
	if err == nil {
		for _, commit := range changes.Commits {
			if factIn(commit.Snapshots, "candidate:dhw.once") {
				inReplay = true
			}
		}
	} else if ctx.Err() == nil {
		t.Fatal(err)
	}
	if inCut == inReplay {
		t.Fatalf("in cut %v in replay %v err %v", inCut, inReplay, err)
	}
}

func TestCapturePartialLookupNamesOmission(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	if err := svc.Install(context.Background(), installationPlan(t)); err != nil {
		t.Fatal(err)
	}
	openPlain(t, svc, "unit.held", "asset:held", "source:held", "epoch:held", "1")
	view, err := svc.CaptureView(context.Background(), driver.Filter{Assets: []v1.AssetID{installationAsset, "asset:held"}})
	if err != nil {
		t.Fatal(err)
	}
	if view.Ref.Completeness != graph.CutPartial || len(view.Omissions) != 1 || view.Omissions[0].AssetID != "asset:held" || view.Omissions[0].Reason != graph.NotIncludedSourceUnavailable {
		t.Fatalf("omission %+v %s", view.Omissions, view.Ref.Completeness)
	}
	session := openPlain(t, svc, "unit.one", "asset:unit.one", "source:unit.one", "epoch:unit.one", "1")
	device := nodeOf(svc, "unit.one")
	fact := observedFact(session.spec, "candidate:dhw.one", "binding:unit.one:g1", "1", "32165", clock.Now(), 10)
	publishFact(t, session, "binding:unit.one:g1", declareFact(device, device, "path.dhw", 30, fact), fact)
	held, err := svc.Lookup(context.Background(), view.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(held.Omissions) != 1 || held.Omissions[0].AssetID != "asset:held" || held.Omissions[0].Reason != graph.NotIncludedSourceUnavailable || held.Ref.CutID != view.Ref.CutID {
		t.Fatalf("lookup %+v", held.Omissions)
	}
}

func TestCaptureSameDigestKeepsScopedLens(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	if err := svc.Install(context.Background(), installationPlan(t)); err != nil {
		t.Fatal(err)
	}
	openPlain(t, svc, "unit.held", "asset:held", "source:held", "epoch:held", "1")
	narrow := driver.Filter{Assets: []v1.AssetID{installationAsset}}
	wide := driver.Filter{Assets: []v1.AssetID{installationAsset, "asset:held"}}
	plain, err := svc.CaptureView(context.Background(), narrow)
	if err != nil {
		t.Fatal(err)
	}
	omitted, err := svc.CaptureView(context.Background(), wide)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Ref.CutID != omitted.Ref.CutID {
		t.Fatalf("digest %s %s", plain.Ref.CutID, omitted.Ref.CutID)
	}
	if len(plain.Omissions) != 0 || len(omitted.Omissions) != 1 || omitted.Omissions[0].AssetID != "asset:held" {
		t.Fatalf("omissions %v %v", plain.Omissions, omitted.Omissions)
	}
	request := graph.LensRequest{
		Lens: "lens.equipment_composition", Entry: graph.EntryPoint{Kind: graph.EntryLensRoots},
		Direction: graph.DirectionOutgoing, Depth: 2, PageSize: 1,
	}
	left, err := svc.RetrieveLens(context.Background(), plain.Ref, request)
	if err != nil {
		t.Fatal(err)
	}
	right, err := svc.RetrieveLens(context.Background(), omitted.Ref, request)
	if err != nil {
		t.Fatal(err)
	}
	if left.Cut.CutID != plain.Ref.CutID || len(left.Cut.NotIncluded) != 0 {
		t.Fatalf("plain lens %+v", left.Cut)
	}
	if right.Cut.CutID != omitted.Ref.CutID || len(right.Cut.NotIncluded) != 1 || right.Cut.NotIncluded[0].AssetID != "asset:held" {
		t.Fatalf("omitted lens %+v", right.Cut)
	}
	if left.NextPage == nil {
		t.Fatal("no lens continuation")
	}
	clock.advance(time.Second)
	openPlain(t, svc, "unit.later", "asset:later", "source:later", "epoch:later", "1")
	request.PageToken = *left.NextPage
	continued, err := svc.RetrieveLens(context.Background(), plain.Ref, request)
	if err != nil {
		t.Fatal(err)
	}
	if continued.Cut.CutID != plain.Ref.CutID || len(continued.Cut.NotIncluded) != 0 {
		t.Fatalf("continuation %+v", continued.Cut)
	}
	again, err := svc.CaptureView(context.Background(), driver.Filter{Assets: []v1.AssetID{installationAsset, "asset:held", "asset:later"}})
	if err != nil {
		t.Fatal(err)
	}
	if again.Ref.CutID != plain.Ref.CutID || len(again.Omissions) != 2 {
		t.Fatalf("same digest omissions %+v %s", again.Omissions, again.Ref.CutID)
	}
	third, err := svc.RetrieveLens(context.Background(), again.Ref, graph.LensRequest{
		Lens: "lens.equipment_composition", Entry: graph.EntryPoint{Kind: graph.EntryLensRoots},
		Direction: graph.DirectionOutgoing, Depth: 2, PageSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Cut.NotIncluded) != 2 {
		t.Fatalf("new scope %+v", third.Cut.NotIncluded)
	}
}

func TestCaptureEvaluationFollowsTheClock(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	node := memberNode(t, svc, "unit.one", "slot.one")
	session := openPlain(t, svc, "unit.one", "asset:one", "source:one", "epoch:one", "1")
	fact := keepFresh(observedFact(session.spec, "candidate:one", "binding:one", "1", "32165", clock.Now(), 1000))
	fact.Times.ReceiptMonotonic = monoNS(1000)
	fact.Times.EvaluateMonotonic = monoNS(1000)
	fact.FreshnessPolicy.FreshForNS = "1000"
	fact.FreshnessPolicy.RetainForNS = "5000"
	source := declareFact(node, node, "path.one", 30, fact)
	publishFact(t, session, "binding:one", source, fact)
	if assetSnap(t, svc, "asset:one").EvaluateMonotonic.Nanoseconds != "1000" {
		t.Fatalf("publication point %s", assetSnap(t, svc, "asset:one").EvaluateMonotonic.Nanoseconds)
	}
	first, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Evaluation.EvaluateMonotonic.Nanoseconds != "1000" || svc.Element(source.For, first).Freshness != v1.FreshnessFresh {
		t.Fatalf("first view %s %s", first.Evaluation.EvaluateMonotonic.Nanoseconds, svc.Element(source.For, first).Freshness)
	}
	clock.advance(2000 * time.Nanosecond)
	stale, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if stale.Evaluation.EvaluateMonotonic.Nanoseconds != "3000" || svc.Element(source.For, stale).Freshness != v1.FreshnessStale {
		t.Fatalf("stale view %s %s", stale.Evaluation.EvaluateMonotonic.Nanoseconds, svc.Element(source.For, stale).Freshness)
	}
	if first.Evaluation.EvaluateMonotonic.Nanoseconds != "1000" || svc.Element(source.For, first).Freshness != v1.FreshnessFresh {
		t.Fatal("the earlier view changed with the clock")
	}
	clock.advance(4000 * time.Nanosecond)
	expired, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	gone := svc.Element(source.For, expired)
	if gone.Freshness != v1.FreshnessExpired || gone.Value != nil || sourceState(gone, "path.one").Disposition != sources.DispositionExpired {
		t.Fatalf("expired %+v %s", gone.Freshness, valueCoefficient(gone.Value))
	}
	if svc.Element(source.For, first).Freshness != v1.FreshnessFresh {
		t.Fatal("the retained view was reevaluated")
	}
	later := retitle(fact, "thermal.measurement.later.heat", "candidate:later")
	later.Times.ReceiptMonotonic = monoNS(6500)
	later.Times.EvaluateMonotonic = monoNS(6500)
	later.FreshnessPolicy.FreshForNS = "1000"
	later.FreshnessPolicy.RetainForNS = "5000"
	laterSource := declareFact(node, node, "path.later", 30, later)
	commit(t, session, func(b *v1.PublicationBatch) {
		b.FactUpserts = []v1.FactCandidate{later}
	}, func(tx *transaction) {
		if err := tx.DeclareSource(laterSource); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageSample(driver.Sample{From: laterSource, Candidate: later}); err != nil {
			t.Fatal(err)
		}
	})
	if assetSnap(t, svc, "asset:one").EvaluateMonotonic.Nanoseconds != "7000" {
		t.Fatalf("later publication point %s", assetSnap(t, svc, "asset:one").EvaluateMonotonic.Nanoseconds)
	}
	view, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if sourceState(svc.Element(laterSource.For, view), "path.later").Freshness != v1.FreshnessFresh {
		t.Fatalf("later receipt %s", sourceState(svc.Element(laterSource.For, view), "path.later").Freshness)
	}
}

func TestCaptureBoundsExceeded(t *testing.T) {
	if _, err := publishMembers(t, graph.MaxCutMembers+1); v1.ErrorIdentifier(err) != v1.BoundsExceeded {
		t.Fatalf("257 members: %v", err)
	}
	view, err := publishMembers(t, graph.MaxCutMembers)
	if err != nil || len(view.Members) != graph.MaxCutMembers {
		t.Fatalf("256 members: %v len %d", err, len(view.Members))
	}
}

func TestCaptureAbsentTargetCoherence(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	if err := svc.Install(context.Background(), installationPlan(t)); err != nil {
		t.Fatal(err)
	}
	session := installSession(svc)
	site := svc.nodes[0].Ref
	target := v1.ResourceID("resource:grid.absent")
	relation := v1.RelationInstance{
		Definition: v1.DefinitionRef{Pack: v1.PackRef{ID: "helianthus.pack.infrastructure", Version: "1.0.0"}, ID: "infrastructure.relation.site_grid_connection", Version: "1.0.0"},
		From:       site.ID, To: target, State: v1.LinkQualified, BasisClass: v1.RelationConfiguration,
		Basis:    []v1.EvidenceRef{testEvidence("basis:grid")},
		Origin:   v1.OriginRef{OriginID: "origin:operator:grid", Kind: v1.OriginOperator, Evidence: []v1.EvidenceRef{testEvidence("origin:grid")}},
		Revision: "1",
	}
	commit(t, session, func(b *v1.PublicationBatch) { b.RelationUpserts = []v1.RelationInstance{relation} }, nil)
	complete, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	key := driver.LinkKey{Type: relation.Definition, From: site, To: driver.NodeRef{Bridge: site.Bridge, ID: target}}
	coherence, ok := complete.Coherence(key)
	if !ok || coherence != graph.CoherenceDangling || complete.Ref.Completeness != graph.CutComplete {
		t.Fatalf("complete %s %v %s", coherence, ok, complete.Ref.Completeness)
	}
	openPlain(t, svc, "unit.held", "asset:held", "source:held", "epoch:held", "1")
	partial, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	coherence, ok = partial.Coherence(key)
	if !ok || coherence != graph.CoherenceOutsideCut || partial.Ref.Completeness != graph.CutPartial {
		t.Fatalf("partial %s %v %s", coherence, ok, partial.Ref.Completeness)
	}
	class, classOK := svc.catalog.ResourceKindClass("resource.grid_connection")
	if !classOK {
		t.Fatal("grid connection has no class")
	}
	resource, err := svc.operatorResource(target, "resource.grid_connection", "grid.absent")
	if err != nil {
		t.Fatal(err)
	}
	if resource.Anchor.Class != class {
		t.Fatal(resource.Anchor.Class)
	}
	commit(t, session, func(b *v1.PublicationBatch) { b.ResourceUpserts = []v1.Resource{resource} }, nil)
	returned, err := svc.CaptureView(context.Background(), driver.Filter{Assets: []v1.AssetID{installationAsset}})
	if err != nil {
		t.Fatal(err)
	}
	coherence, ok = returned.Coherence(key)
	rel, relOK := returned.Cut.Relation(v1.RelationKey{Definition: relation.Definition, From: site.ID, To: target})
	if !ok || !relOK || coherence != graph.CoherenceCoherent || rel.Coherence != graph.CoherenceCoherent {
		t.Fatalf("returned %s cut %s", coherence, rel.Coherence)
	}
	missing := complete
	missing.Overlay = append([]OverlayLink(nil), complete.Overlay...)
	missing.Overlay = append(missing.Overlay, OverlayLink{Relation: "relation.relays", From: site, To: driver.NodeRef{Bridge: site.Bridge, ID: "resource:overlay.absent"}})
	overlay, ok := missing.OverlayCoherence("relation.relays", site.ID, "resource:overlay.absent")
	if !ok || overlay != graph.CoherenceDangling {
		t.Fatalf("overlay complete %s", overlay)
	}
	missing.Cut = partial.Cut
	overlay, ok = missing.OverlayCoherence("relation.relays", site.ID, "resource:overlay.absent")
	if !ok || overlay != graph.CoherenceOutsideCut {
		t.Fatalf("overlay partial %s", overlay)
	}
}

func TestCaptureSumMembers(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	if err := svc.Install(context.Background(), installationPlan(t)); err != nil {
		t.Fatal(err)
	}
	view, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	site := view.Nodes[0].Ref.ID
	request := graph.LensRequest{
		Lens: "lens.equipment_composition", Entry: graph.EntryPoint{Kind: graph.EntryLensRoots},
		Direction: graph.DirectionOutgoing, Depth: 1, PageSize: 10,
	}
	sum, err := svc.InputOf(view).SumMembers(context.Background(), view.Ref, request, v1.DefinitionRef{Pack: labPack, ID: "thermal.measurement.dhw_temperature", Version: "1.0.0"}, site)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Withheld {
		t.Fatalf("sum %+v", sum)
	}
	_, err = svc.InputOf(view).SumMembers(context.Background(), view.Ref, request, v1.DefinitionRef{Pack: labPack, ID: "thermal.measurement.dhw_temperature", Version: "1.0.0"}, "resource:outside.cut")
	requireIdent(t, err, v1.DanglingReference)
}

func factService(t *testing.T, clock *testClock) (*Service, *Session, driver.NodeRef) {
	t.Helper()
	svc := openService(t, clock)
	if err := svc.Install(context.Background(), installationPlan(t)); err != nil {
		t.Fatal(err)
	}
	session := openPlain(t, svc, "unit.one", "asset:unit.one", "source:unit.one", "epoch:unit.one", "1")
	return svc, session, nodeOf(svc, "unit.one")
}

func publishAndCapture(t *testing.T, svc *Service, session *Session, device driver.NodeRef, revision, coefficient string, at time.Time) View {
	t.Helper()
	publishRevision(t, session, device, revision, coefficient, at)
	view, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func publishRevision(t *testing.T, session *Session, device driver.NodeRef, revision, coefficient string, at time.Time) {
	t.Helper()
	fact := observedFact(session.spec, "candidate:dhw.one", "binding:unit.one:g1", revision, coefficient, at, 10)
	source := declareFact(device, device, "path.dhw", 30, fact)
	if revision == "1" {
		publishFact(t, session, "binding:unit.one:g1", source, fact)
		return
	}
	commit(t, session, func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{fact} }, func(tx *transaction) {
		if err := tx.StageSample(driver.Sample{From: source, Candidate: fact}); err != nil {
			t.Fatal(err)
		}
	})
}

func publishMembers(t *testing.T, n int) (View, error) {
	t.Helper()
	svc := openService(t, newTestClock())
	for i := 0; i < n; i++ {
		name := strconv.Itoa(i)
		session := openPlain(t, svc, "unit."+name, "asset:member:"+name, "source:member:"+name, "epoch:member:"+name, "1")
		commit(t, session, func(b *v1.PublicationBatch) {
			b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(session.spec, "registry:"+name)}
		}, nil)
	}
	return svc.CaptureView(context.Background(), driver.Filter{})
}

func factIn(snaps []v1.Snapshot, id v1.CandidateID) bool {
	for _, snap := range snaps {
		if coefficientOf(snap, id) != "" {
			return true
		}
	}
	return false
}

func (s *Service) InputOf(view View) driver.SemanticInput {
	session := &Session{svc: s, scope: view.Filter}
	session.Pin(view.Ref)
	return session.Input()
}
