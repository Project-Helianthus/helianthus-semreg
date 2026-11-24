package sources

import (
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func tiedPair(t *testing.T) (*lab, *drv, *drv) {
	l := newLab(t)
	step := quantity("5", -1)
	a := l.addDriver("boiler", "asset:boiler", 30, &step)
	b := l.addDriver("regulator", "asset:regulator", 30, &step)
	return l, a, b
}

func tieFindings(e ElementState) []driver.Finding {
	var out []driver.Finding
	for _, f := range e.Resolution.Findings {
		if f.PriorityTie != nil {
			out = append(out, f)
		}
	}
	return out
}

func TestEqualPriorityIsFlaggedAndLatestReceiptWins(t *testing.T) {
	l, a, b := tiedPair(t)
	l.now = 100
	a.publish(a.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	b.publish(b.obs("cand:b", quantity("32185", -2), "1", 100, "b1"))
	l.now = 110
	e := l.element()
	if !isWinner(e, b) || coefficient(e.Value) != "32185" {
		t.Fatalf("the most recent receipt takes authority: %s", mustJSON(t, e))
	}
	ties := tieFindings(e)
	if len(ties) != 1 || *ties[0].PriorityTie != 30 || refKey(ties[0].First) != refKey(a.ref()) || refKey(ties[0].Second) != refKey(b.ref()) {
		t.Fatalf("a driver bug is flagged with the ordered references and the common priority: %s", mustJSON(t, e.Resolution.Findings))
	}
	if err := e.Resolution.Validate(); err != nil {
		t.Fatal(err)
	}
	l.now = 120
	a.publish(a.obs("cand:a", quantity("32165", -2), "2", 120, "a2"))
	if e := l.element(); !isWinner(e, a) {
		t.Fatalf("the later receipt takes authority: %s", mustJSON(t, e))
	}
}

func TestEqualPriorityWithIndistinguishableReceiptsOrdersBySourceKey(t *testing.T) {
	l, a, b := tiedPair(t)
	l.frozen = true
	l.tick = 7
	l.now = 100
	b.publish(b.obs("cand:b", quantity("32185", -2), "1", 100, "b1"))
	a.publish(a.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	l.now = 110
	if e := l.element(); !isWinner(e, a) {
		t.Fatalf("equal receipts order by source key bytes: %s", mustJSON(t, e))
	}
}

func TestEqualPriorityBelowTheWinnerIsFlaggedAndNeverChangesIt(t *testing.T) {
	l := newLab(t)
	step := quantity("5", -1)
	top := l.addDriver("boiler", "asset:boiler", 30, &step)
	x := l.addDriver("mirror", "asset:mirror", 10, &step)
	y := l.addDriver("remote", "asset:remote", 10, &step)
	l.now = 100
	top.publish(top.obs("cand:top", quantity("32165", -2), "1", 100, "t1"))
	x.publish(x.obs("cand:x", quantity("32165", -2), "1", 100, "x1"))
	y.publish(y.obs("cand:y", quantity("32165", -2), "1", 100, "y1"))
	l.now = 110
	e := l.element()
	ties := tieFindings(e)
	if !isWinner(e, top) || len(ties) != 1 || *ties[0].PriorityTie != 10 || refKey(ties[0].First) != refKey(x.ref()) || refKey(ties[0].Second) != refKey(y.ref()) {
		t.Fatalf("a lower-ranked tie is flagged and never changes the winner: %s", mustJSON(t, e.Resolution))
	}
}

func TestPrioritiesThreeZeroZeroTieHasNoSamplePayload(t *testing.T) {
	l := newLab(t)
	step := quantity("5", -1)
	top := l.addDriver("boiler", "asset:boiler", 3, &step)
	x := l.addDriver("mirror", "asset:mirror", 0, &step)
	y := l.addDriver("remote", "asset:remote", 0, &step)
	e := l.element()
	if !isWinner(e, top) || len(e.Resolution.Findings) != 1 {
		t.Fatalf("one tie finding, the priority-3 source stays the winner: %s", mustJSON(t, e.Resolution))
	}
	f := e.Resolution.Findings[0]
	if f.PriorityTie == nil || *f.PriorityTie != 0 || refKey(f.First) != refKey(x.ref()) || refKey(f.Second) != refKey(y.ref()) ||
		f.FirstValue != nil || f.SecondValue != nil || f.FirstStep != nil || f.SecondStep != nil || f.Incomparable {
		t.Fatalf("score 0 tie with no sample payload: %s", mustJSON(t, f))
	}
	if err := e.Resolution.Validate(); err != nil {
		t.Fatal(err)
	}

	l.now = 100
	top.publish(top.obs("cand:top", quantity("32165", -2), "1", 100, "t1"))
	x.publish(x.obs("cand:x", quantity("32565", -2), "1", 100, "x1"))
	y.publish(y.obs("cand:y", quantity("32965", -2), "1", 100, "y1"))
	l.now = 110
	e = l.element()
	if err := e.Resolution.Validate(); err != nil {
		t.Fatal(err)
	}
	var tie, sample int
	for _, f := range e.Resolution.Findings {
		if f.PriorityTie != nil {
			tie++
			if f.FirstValue != nil || f.SecondValue != nil {
				t.Fatalf("the tie finding carries no sample: %s", mustJSON(t, f))
			}
		} else if f.FirstValue != nil && f.SecondValue != nil && !f.Incomparable {
			sample++
		}
	}
	if tie != 1 || sample != 2 {
		t.Fatalf("the tie and the separate sample disagreements keep their own payloads: %s", mustJSON(t, e.Resolution.Findings))
	}
	if len(e.TiedComparisons) != 1 || e.TiedComparisons[0].FirstValue == nil || e.TiedComparisons[0].FirstStep == nil {
		t.Fatalf("the tied pair's own sample comparison is kept beside it: %s", mustJSON(t, e.TiedComparisons))
	}
}

func TestReplayingAnOldObservationLeavesReceiptAndBasisUnchanged(t *testing.T) {
	l, a, _ := tiedPair(t)
	l.now = 100
	snapshot := a.publish(a.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	held := l.table.entries[refKey(a.ref())]
	stamp, basis := held.acq.stamp, held.sample.member.SnapshotID

	if err := l.table.Admit(driver.Sample{From: a.source, Candidate: a.obs("cand:a", quantity("32165", -2), "1", 100, "a1")}, snapshot); err != nil {
		t.Fatal(err)
	}
	if held.acq.stamp != stamp || held.sample.member.SnapshotID != basis {
		t.Fatal("an exact replay refreshed the receipt or the basis")
	}
	l.now = 110
	a.publish(a.obs("cand:a", quantity("32105", -2), "2", 90, "older"))
	if held.acq.stamp != stamp || held.sample.member.SnapshotID != basis || held.sample.candidate.Revision != "1" {
		t.Fatalf("an older observation refreshed the receipt or the basis: %+v", held.acq)
	}
}

func TestHigherSequenceDuplicateAfterNewerPeerKeepsPeerAuthoritative(t *testing.T) {
	l, a, b := tiedPair(t)
	l.now = 100
	first := a.publish(a.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	b.publish(b.obs("cand:b", quantity("32185", -2), "1", 100, "b1"))
	held := l.table.entries[refKey(a.ref())]
	stamp := held.acq.stamp
	l.now = 110
	duplicate := a.publish(a.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	if duplicate.SnapshotID == first.SnapshotID {
		t.Fatal("the carrier must commit at a higher publication sequence as a new snapshot")
	}
	if held.acq.stamp != stamp || held.sample.member.SnapshotID != first.SnapshotID {
		t.Fatal("the duplicate refreshed the receipt or the saved basis")
	}
	if e := l.element(); !isWinner(e, b) {
		t.Fatalf("the peer stays authoritative: %s", mustJSON(t, e))
	}
}

func TestWithdrawnTiedSourceReplayDoesNotRefreshThenNewAcquisitionTakesAuthority(t *testing.T) {
	l, a, b := tiedPair(t)
	l.now = 100
	original := a.obs("cand:a", quantity("32165", -2), "1", 100, "a1")
	a.publish(original)
	b.publish(b.obs("cand:b", quantity("32185", -2), "1", 100, "b1"))
	entry := l.table.entries[refKey(a.ref())]
	stamp, acq := entry.acq.stamp, *entry.acq
	l.now = 110
	prior := l.element()
	priorJSON := mustJSON(t, prior)
	a.withdraw("cand:a")
	l.now = 120
	a.publish(original)
	if entry.sample != nil || entry.acq.stamp != stamp || entry.removal != ExplicitWithdrawal {
		t.Fatalf("the original acquisition refreshed the receipt or restored the basis: %+v", entry)
	}
	if entry.acq.asset != acq.asset || entry.acq.candidate != acq.candidate || !samePtr(entry.acq.epoch, acq.epoch) || !samePtr(entry.acq.generation, acq.generation) || entry.acq.identity != acq.identity {
		t.Fatal("the scoped source/epoch/generation correlation was lost through the removal")
	}
	e := l.element()
	st := e.state(a)
	if !isWinner(e, b) || st.Latest != nil || st.Availability != v1.AvailabilityWithdrawn || hasCoefficient(t, e.state(a), "32165") || coefficient(e.Value) != "32185" {
		t.Fatalf("withdrawn without value, basis or served evidence, the peer remains authoritative: %s", mustJSON(t, e))
	}
	if mustJSON(t, prior) != priorJSON || !hasCoefficient(t, prior, "32165") {
		t.Fatal("the previously captured cut changed")
	}
	fresh := a.obs("cand:a", quantity("32165", -2), "2", 125, "a2")
	l.now = 125
	a.publish(fresh)
	if e := l.element(); !isWinner(e, a) || coefficient(e.Value) != "32165" {
		t.Fatalf("a genuinely acquired observation takes authority: %s", mustJSON(t, e))
	}
}

func TestSameAcquisitionAvailabilityRevisionKeepsReceiptAndPeerAuthority(t *testing.T) {
	l, a, b := tiedPair(t)
	l.now = 100
	a.publish(a.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	b.publish(b.obs("cand:b", quantity("32185", -2), "1", 100, "b1"))
	stamp := l.table.entries[refKey(a.ref())].acq.stamp
	l.now = 110
	changed := a.obs("cand:a", quantity("32165", -2), "2", 100, "a1")
	changed.Quality.Availability = v1.AvailabilityUnavailable
	a.publish(changed)
	e := l.element()
	st := e.state(a)
	if !isWinner(e, b) || l.table.entries[refKey(a.ref())].acq.stamp != stamp {
		t.Fatalf("the peer stays authoritative and the receipt is preserved: %s", mustJSON(t, e))
	}
	if st.Latest == nil || st.Latest.Candidate.Revision != "2" || st.Availability != v1.AvailabilityUnavailable || st.Latest.Receipt != stamp {
		t.Fatalf("Sources exposes the new revision and its exact evaluated availability: %s", mustJSON(t, st))
	}
}

func TestReevaluationAndWithdrawalRevisionAreNotNewAcquisitions(t *testing.T) {
	l, a, b := tiedPair(t)
	l.now = 100
	a.publish(a.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	b.publish(b.obs("cand:b", quantity("32185", -2), "1", 100, "b1"))
	stamp := l.table.entries[refKey(a.ref())].acq.stamp

	l.now = 110
	reevaluated := a.obs("cand:a", quantity("32165", -2), "2", 100, "a1")
	reevaluated.Times.EvaluatedAt, reevaluated.Times.EvaluateMonotonic = at(110), mono(110)
	a.publish(reevaluated)
	e := l.element()
	if !isWinner(e, b) || e.state(a).Latest.Candidate.Revision != "2" || l.table.entries[refKey(a.ref())].acq.stamp != stamp {
		t.Fatalf("a re-evaluation is not a new acquisition: %s", mustJSON(t, e))
	}

	l.now = 115
	withdrawn := a.obs("cand:a", quantity("32165", -2), "3", 100, "a1")
	withdrawn.Value = nil
	withdrawn.Quality.Availability = v1.AvailabilityWithdrawn
	a.publish(withdrawn)
	e = l.element()
	st := e.state(a)
	if !isWinner(e, b) || st.Latest == nil || st.Latest.Candidate.Revision != "3" || st.Latest.Candidate.Value != nil || st.Availability != v1.AvailabilityWithdrawn || st.Latest.Receipt != stamp || hasCoefficient(t, e, "32165") {
		t.Fatalf("the withdrawal revision omits the value, keeps the link and the older receipt: %s", mustJSON(t, e))
	}
	a.stop()
	if later := l.element(); hasCoefficient(t, later, "32165") || later.state(a).Latest != nil && later.state(a).Latest.Candidate.Value != nil {
		t.Fatalf("stopped retention brought the old sample back: %s", mustJSON(t, later))
	}
}

func TestNewObservationWithTheSameScalarRefreshesReceiptAndTakesAuthority(t *testing.T) {
	l, a, b := tiedPair(t)
	l.now = 100
	a.publish(a.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	b.publish(b.obs("cand:b", quantity("32185", -2), "1", 100, "b1"))
	if !isWinner(l.element(), b) {
		t.Fatal("setup: the peer holds authority")
	}
	before := l.table.entries[refKey(a.ref())].acq.stamp
	l.now = 120
	a.publish(a.obs("cand:a", quantity("32165", -2), "2", 120, "a2"))
	after := l.table.entries[refKey(a.ref())].acq.stamp
	if after == before {
		t.Fatal("a new acquisition must be stamped")
	}
	if e := l.element(); !isWinner(e, a) {
		t.Fatalf("the refreshed receipt takes authority: %s", mustJSON(t, e))
	}
}
