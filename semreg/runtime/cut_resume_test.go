package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func TestResumeOrderedReplayKeepsEveryRevision(t *testing.T) {
	clock := newTestClock()
	svc, session, device := factService(t, clock)
	first := publishAndCapture(t, svc, session, device, "1", "11111", clock.Now())
	publishRevision(t, session, device, "2", "22222", clock.Now())
	publishRevision(t, session, device, "3", "11111", clock.Now())
	changes, err := svc.Resume(context.Background(), first.Cursor, driver.Filter{})
	if err != nil || changes.Resync || len(changes.Commits) != 2 {
		t.Fatalf("replay %+v %v", changes, err)
	}
	if commitCoefficient(changes.Commits[0], "candidate:dhw.one") != "22222" || commitCoefficient(changes.Commits[1], "candidate:dhw.one") != "11111" {
		t.Fatalf("coefficients %s %s", commitCoefficient(changes.Commits[0], "candidate:dhw.one"), commitCoefficient(changes.Commits[1], "candidate:dhw.one"))
	}
	if changes.Commits[0].Ref.CutID == changes.Commits[1].Ref.CutID && changes.Cursor.Position == first.Cursor.Position {
		t.Fatal("replay collapsed onto one cut id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	again, err := svc.Resume(ctx, changes.Cursor, driver.Filter{})
	if !errors.Is(err, context.DeadlineExceeded) || len(again.Commits) != 0 {
		t.Fatalf("caught-up resume returned %+v %v", again, err)
	}
}

func TestResumeLongPollHandoff(t *testing.T) {
	clock := newTestClock()
	svc, session, device := factService(t, clock)
	view := publishAndCapture(t, svc, session, device, "1", "32165", clock.Now())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type result struct {
		changes driver.Changes
		err     error
	}
	done := make(chan result, 1)
	go func() {
		changes, err := svc.Resume(ctx, view.Cursor, driver.Filter{})
		done <- result{changes, err}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		svc.mu.Lock()
		waiting := len(svc.waiters) > 0
		svc.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resume did not wait")
		}
		time.Sleep(time.Millisecond)
	}
	publishRevision(t, session, device, "2", "32265", clock.Now())
	got := <-done
	if got.err != nil || got.changes.Resync || len(got.changes.Commits) != 1 {
		t.Fatalf("handoff %+v %v", got.changes, got.err)
	}
	if commitCoefficient(got.changes.Commits[0], "candidate:dhw.one") != "32265" {
		t.Fatal(commitCoefficient(got.changes.Commits[0], "candidate:dhw.one"))
	}
	restCtx, restCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer restCancel()
	rest, err := svc.Resume(restCtx, got.changes.Cursor, driver.Filter{})
	if !errors.Is(err, context.DeadlineExceeded) || len(rest.Commits) != 0 {
		t.Fatalf("the handoff commit was delivered twice: %+v %v", rest, err)
	}
}

func TestResumeFilterIsolation(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	leftNode := memberNode(t, svc, "unit.left", "slot.left")
	rightNode := memberNode(t, svc, "unit.right", "slot.right")
	left := openPlain(t, svc, "unit.left", "asset:left", "source:left", "epoch:left", "1")
	right := openPlain(t, svc, "unit.right", "asset:right", "source:right", "epoch:right", "1")
	publishNamed(t, left, leftNode, "binding:left", "path.left", "candidate:left", "1", "11111")
	start, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	publishNamed(t, left, leftNode, "binding:left", "path.left", "candidate:left", "2", "22222")
	publishNamed(t, right, rightNode, "binding:right", "path.right", "candidate:right", "1", "33333")
	publishNamed(t, left, leftNode, "binding:left", "path.left", "candidate:left", "3", "44444")
	filter := driver.Filter{Assets: []v1.AssetID{"asset:left"}}
	changes, err := svc.Resume(context.Background(), start.Cursor, filter)
	if err != nil || changes.Resync || len(changes.Commits) != 2 {
		t.Fatalf("filter %+v %v", changes, err)
	}
	for _, commit := range changes.Commits {
		for _, snap := range commit.Snapshots {
			if snap.AssetID == "asset:right" {
				t.Fatal("a filtered resume returned the other asset")
			}
		}
	}
	if commitCoefficient(changes.Commits[0], "candidate:left") != "22222" || commitCoefficient(changes.Commits[1], "candidate:left") != "44444" {
		t.Fatal("the matching revisions were skipped or reordered")
	}
}

func TestResumeResyncAndCancel(t *testing.T) {
	clock := newTestClock()
	svc, session, device := factService(t, clock)
	view := publishAndCapture(t, svc, session, device, "1", "32165", clock.Now())
	publishRevision(t, session, device, "2", "32265", clock.Now())
	foreign := view.Cursor
	foreign.Incarnation = "incarnation.other"
	changes, err := svc.Resume(context.Background(), foreign, driver.Filter{})
	if err != nil || !changes.Resync || len(changes.Commits) != 0 {
		t.Fatalf("incarnation %+v %v", changes, err)
	}
	ahead := view.Cursor
	ahead.Position = "99"
	changes, err = svc.Resume(context.Background(), ahead, driver.Filter{})
	if err != nil || !changes.Resync || len(changes.Commits) != 0 {
		t.Fatalf("ahead %+v %v", changes, err)
	}
	kept, err := svc.Resume(context.Background(), view.Cursor, driver.Filter{})
	if err != nil || kept.Resync || len(kept.Commits) != 1 || commitCoefficient(kept.Commits[0], "candidate:dhw.one") != "32265" {
		t.Fatalf("history after resync %+v %v", kept, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.Resume(ctx, kept.Cursor, driver.Filter{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel %v", err)
	}
	waitCtx, waitCancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := svc.Resume(waitCtx, kept.Cursor, driver.Filter{})
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		svc.mu.Lock()
		waiting := len(svc.waiters) > 0
		svc.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resume did not wait for cancellation")
		}
		time.Sleep(time.Millisecond)
	}
	waitCancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("wait cancel %v", err)
	}
}

func TestResumeExpiredCutKeepsFactValue(t *testing.T) {
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
	if err.Error() == "" {
		t.Fatal(err)
	}
	changes, err := svc.Resume(context.Background(), first.Cursor, driver.Filter{})
	if err != nil || changes.Resync || len(changes.Commits) != 1 {
		t.Fatalf("expired cut replay %+v %v", changes, err)
	}
	if commitCoefficient(changes.Commits[0], "candidate:dhw.one") != "32265" {
		t.Fatal(commitCoefficient(changes.Commits[0], "candidate:dhw.one"))
	}
	if expiry.IsZero() {
		t.Fatal("expiry")
	}
}

func commitCoefficient(cut driver.SemanticCut, id v1.CandidateID) string {
	for _, snap := range cut.Snapshots {
		if coefficient := coefficientOf(snap, id); coefficient != "" {
			return coefficient
		}
	}
	return ""
}

func publishNamed(t *testing.T, session *Session, device driver.NodeRef, binding, path, candidate, revision, coefficient string) {
	t.Helper()
	fact := keepFresh(observedFact(session.spec, candidate, binding, revision, coefficient, time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), 100))
	source := declareFact(device, device, path, 30, fact)
	if revision == "1" {
		publishFact(t, session, binding, source, fact)
		return
	}
	commit(t, session, func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{fact} }, func(tx *transaction) {
		if err := tx.StageSample(driver.Sample{From: source, Candidate: fact}); err != nil {
			t.Fatal(err)
		}
	})
}
