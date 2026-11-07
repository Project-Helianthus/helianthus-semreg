package semreg

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// capacityLog constructs a complete synthetic history, not a final-state import.
// The optional short prefix expires only when a later trusted context proves it.
func capacityLog(t *testing.T, history, current, short int) []AcceptedPublication {
	t.Helper()
	var log []AcceptedPublication
	add := func(b PublicationBatch) {
		sealPublicationBatch(t, &b)
		log = append(log, AcceptedPublication{b, publicationMonotonic})
	}
	if history > 0 {
		b := admissionSet(history, "1", "1", "0")
		for i := range b.FactUpserts {
			if i >= short {
				b.FactUpserts[i].FreshnessPolicy.RetainForNS = "10000"
			}
		}
		add(b)
		f := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "1", "2", "1")
		f.ObservedAt = b.ObservedAt
		f.GenerationFences = []GenerationFence{publicationFence(b.SourceID, b.SourceEpochID, "1", publicEvidence("9"))}
		add(f)
	}
	generation, revision := Uint64("1"), Uint64("0")
	if history > 0 {
		generation, revision = "2", "2"
	}
	add(admissionSet(current, generation, "1", revision))
	return log
}

func restoredCapacity(t *testing.T, history, current, short int) (*PublicationKernel, []AcceptedPublication) {
	t.Helper()
	k := newTestPublicationKernel(t, "asset:capacity")
	log := capacityLog(t, history, current, short)
	if err := k.RestoreAccepted(log); err != nil {
		t.Fatal(err)
	}
	return k, log
}

func capacityPatch(k *PublicationKernel) PublicationBatch {
	s, _, _ := k.Current()
	c := s.Cursors[len(s.Cursors)-1]
	b := publicationBatch(s.AssetID, c.SourceID, c.SourceEpochID, c.DriverGeneration, increment(c.LastSequence), s.Revisions.Semantic)
	b.ObservedAt = TimePoint{UnixNanoseconds: "1000", ClockID: "clock.utc", UncertaintyNS: "0"}
	return b
}

func expectCapacity(t *testing.T, k *PublicationKernel, h, c int, blocked bool) {
	t.Helper()
	d, err := k.RetainedCapacity()
	if err != nil || d.Retained != h || d.CurrentObserved != c || d.RecoveryBlocked != blocked {
		t.Fatalf("capacity %+v, %v; want H=%d C=%d blocked=%v", d, err, h, c, blocked)
	}
}

func TestRetainedRestoreLegacyDebtReplayAndRecovery(t *testing.T) {
	k, log := restoredCapacity(t, 30, 20, 10)
	expectCapacity(t, k, 30, 20, true)
	before := mustCurrentBytes(t, k)
	historical, _, err := k.Apply(log[1].Batch, log[1].PublicationMonotonic)
	if err != nil || len(historical.Retained) != 30 || len(historical.Facts) != 0 || !bytes.Equal(before, mustCurrentBytes(t, k)) {
		t.Fatalf("historical tuple replay: %v", err)
	}
	historical.Retained[0].Candidate.Evidence[0].Owner = "mutated"
	historical, _, err = k.Apply(log[1].Batch, log[1].PublicationMonotonic)
	if err != nil || historical.Retained[0].Candidate.Evidence[0].Owner == "mutated" {
		t.Fatal("historical replay alias")
	}
	last := log[len(log)-1]
	replay, raw, err := k.Apply(last.Batch, last.PublicationMonotonic)
	if err != nil || !bytes.Equal(raw, before) {
		t.Fatalf("legacy accepted replay: %v", err)
	}
	replay.Retained[0].Candidate.Evidence[0].Owner = "mutated"
	raw[0] ^= 1
	if !bytes.Equal(before, mustCurrentBytes(t, k)) {
		t.Fatal("replay alias")
	}
	b := capacityPatch(k)
	sealPublicationBatch(t, &b)
	assertRejectedUnchanged(t, k, b, BoundsExceeded)
	// A STOP/fence cannot promise recovery of legacy overcommit.
	b.GenerationFences = []GenerationFence{publicationFence(b.SourceID, b.SourceEpochID, b.DriverGeneration, publicEvidence("9"))}
	sealPublicationBatch(t, &b)
	assertRejectedUnchanged(t, k, b, BoundsExceeded)
	// The rejected sequence is reusable after truthful expiry proves ten old
	// observations expired; no value, lineage or surviving deadline is rewritten.
	b.GenerationFences = []GenerationFence{}
	b.ObservedAt.UnixNanoseconds = "1120"
	sealPublicationBatch(t, &b)
	s, _, err := k.Apply(b, MonotonicPoint{ClockEpochID: publicationMonotonic.ClockEpochID, Nanoseconds: "220"})
	if err != nil {
		t.Fatal(err)
	}
	expectCapacity(t, k, 20, 20, false)
	previous := replay.Retained
	for _, r := range s.Retained {
		found := false
		for _, old := range previous {
			if reflect.DeepEqual(r, old) {
				found = true
			}
		}
		if !found {
			t.Fatal("recovery rewrote retained history")
		}
	}
}

func TestRetainedRestoreUnknownClockAndWithdrawal(t *testing.T) {
	k, _ := restoredCapacity(t, 30, 20, 10)
	b := capacityPatch(k)
	b.ObservedAt.UnixNanoseconds = "5000"
	b.ObservedAt.UncertaintyNS = "1"
	sealPublicationBatch(t, &b)
	before := mustCurrentBytes(t, k)
	if _, _, err := k.Apply(b, MonotonicPoint{ClockEpochID: "clock-epoch:unknown", Nanoseconds: "1"}); ErrorIdentifier(err) != BoundsExceeded {
		t.Fatalf("unknown-clock debt: %v", err)
	}
	if !bytes.Equal(before, mustCurrentBytes(t, k)) {
		t.Fatal("unknown expiry changed debt")
	}
	b = capacityPatch(k)
	s, _, _ := k.Current()
	for _, r := range s.Retained[:10] {
		b.FactWithdrawals = append(b.FactWithdrawals, r.Candidate.CandidateID)
	}
	// Retained identity order includes candidate ID first for this homogeneous set.
	sortCandidateIDs(b.FactWithdrawals)
	applyAdmission(t, k, b)
	expectCapacity(t, k, 20, 20, false)
}

func sortCandidateIDs(ids []CandidateID) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

func TestRetainedRestoreAtomicValidationAndCopies(t *testing.T) {
	t.Parallel()
	good := capacityLog(t, 30, 20, 0)
	mutations := []struct {
		name   string
		change func(*PublicationBatch)
		want   ErrorID
	}{
		{"asset", func(b *PublicationBatch) { b.AssetID = "asset:foreign" }, InvalidValue},
		{"source", func(b *PublicationBatch) { b.SourceID = "source:foreign" }, InvalidValue},
		{"epoch", func(b *PublicationBatch) { b.SourceEpochID = "epoch:foreign" }, InvalidValue},
		{"generation", func(b *PublicationBatch) { b.DriverGeneration = "9" }, InvalidValue},
		{"sequence", func(b *PublicationBatch) { b.Sequence = "9" }, SequenceConflict},
		{"revision", func(b *PublicationBatch) { b.ExpectedSemanticRevision = "9" }, RevisionConflict},
		{"missing_binding", func(b *PublicationBatch) { b.BindingUpserts = []NativeBinding{} }, DanglingReference},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			log := append([]AcceptedPublication(nil), good...)
			log[2].Batch = clonePublicationBatch(log[2].Batch)
			tc.change(&log[2].Batch)
			sealPublicationBatch(t, &log[2].Batch)
			k := newTestPublicationKernel(t, "asset:capacity")
			if err := k.RestoreAccepted(log); ErrorIdentifier(err) != tc.want {
				t.Fatalf("restore error %v want %s", err, tc.want)
			}
			if _, _, ok := k.Current(); ok {
				t.Fatal("partial restore committed")
			}
			if err := k.RestoreAccepted(good); err != nil {
				t.Fatal(err)
			}
		})
	}
	k := newTestPublicationKernel(t, "asset:capacity")
	if err := k.RestoreAccepted(good); err != nil {
		t.Fatal(err)
	}
	before := mustCurrentBytes(t, k)
	// Mutate every reachable input slice/pointer, including historical entries.
	for i := range good {
		mutateBoundaryAliases(reflect.ValueOf(&good[i]).Elem())
	}
	if !bytes.Equal(before, mustCurrentBytes(t, k)) {
		t.Fatal("restore input alias")
	}
	fork, err := k.Fork()
	if err != nil {
		t.Fatal(err)
	}
	view, raw, _ := fork.Current()
	view.Retained[0].Candidate.Evidence[0].Owner = "mutated"
	raw[0] ^= 1
	if !bytes.Equal(before, mustCurrentBytes(t, k)) || !bytes.Equal(before, mustCurrentBytes(t, fork)) {
		t.Fatal("restore/fork output alias")
	}
	if err := k.RestoreAccepted(capacityLog(t, 0, 1, 0)); ErrorIdentifier(err) != InvalidValue {
		t.Fatalf("restore reset live kernel: %v", err)
	}
	if !bytes.Equal(before, mustCurrentBytes(t, k)) {
		t.Fatal("restore replaced live kernel")
	}
	var nilKernel *PublicationKernel
	if err := nilKernel.RestoreAccepted(good); ErrorIdentifier(err) != InvalidValue {
		t.Fatal(err)
	}
	if _, err := nilKernel.RetainedCapacity(); ErrorIdentifier(err) != InvalidValue {
		t.Fatal(err)
	}
	empty := newTestPublicationKernel(t, "asset:capacity")
	if err := empty.RestoreAccepted(nil); ErrorIdentifier(err) != MissingMember {
		t.Fatal(err)
	}
}

func TestRetainedAdmissionRefreshInferredExpiredAndPrecedence(t *testing.T) {
	t.Parallel()
	k, _ := restoredCapacity(t, 20, 20, 0)
	for n := 0; n < 3; n++ {
		b := capacityPatch(k)
		s, _, _ := k.Current()
		for _, e := range s.Facts {
			for _, c := range e.Candidates {
				c.Revision = increment(c.Revision)
				// The accepted candidates hold true, and a revision bump needs a
				// content change, so the first replacement flips the value.
				c.Value.Boolean = pointerRecord(n%2 == 1)
				b.FactUpserts = append(b.FactUpserts, c)
			}
		}
		applyAdmission(t, k, b)
		expectCapacity(t, k, 20, 20, false)
	}
	s, _, _ := k.Current()
	b := capacityPatch(k)
	for i := 0; i < 20; i++ {
		b.FactUpserts = append(b.FactUpserts, publicationDerivedCandidate(CandidateID(fmt.Sprintf("candidate:derived:%03d", i)), DefinitionID(fmt.Sprintf("fact.derived%03d", i)), []FactCandidate{s.Facts[0].Candidates[0]}))
	}
	applyAdmission(t, k, b)
	expectCapacity(t, k, 20, 20, false)
	// Current expired observations still reserve debt; retained history has a
	// longer original deadline, so only C is expired at the proposed context.
	_, log := restoredCapacity(t, 21, 20, 0)
	for i := range log[2].Batch.FactUpserts {
		log[2].Batch.FactUpserts[i].FreshnessPolicy.RetainForNS = "40"
	}
	sealPublicationBatch(t, &log[2].Batch)
	k = newTestPublicationKernel(t, "asset:capacity")
	if err := k.RestoreAccepted(log); err != nil {
		t.Fatal(err)
	}
	b = capacityPatch(k)
	b.ObservedAt.UnixNanoseconds = "1120"
	sealPublicationBatch(t, &b)
	before := mustCurrentBytes(t, k)
	if _, _, err := k.Apply(b, MonotonicPoint{ClockEpochID: publicationMonotonic.ClockEpochID, Nanoseconds: "220"}); ErrorIdentifier(err) != BoundsExceeded {
		t.Fatal(err)
	}
	if !bytes.Equal(before, mustCurrentBytes(t, k)) {
		t.Fatal("expired-current rejection changed state")
	}
	b.ObservedAt.UnixNanoseconds = "bad"
	// Malformed time precedes capacity without requiring a valid digest.
	if _, _, err := k.Apply(b, publicationMonotonic); ErrorIdentifier(err) != InvalidTime {
		t.Fatalf("precedence %v", err)
	}
}

func TestRetainedAdmissionConcurrentFork(t *testing.T) {
	k, _ := restoredCapacity(t, 20, 20, 0)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	forks := make(chan *PublicationKernel, 10)
	b := capacityPatch(k)
	sealPublicationBatch(t, &b)
	wg.Add(1)
	go func() { defer wg.Done(); _, _, err := k.Apply(b, publicationMonotonic); errs <- err }()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f, e := k.Fork(); errs <- e; forks <- f }()
	}
	wg.Wait()
	close(errs)
	close(forks)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	before := mustCurrentBytes(t, k)
	for f := range forks {
		expectCapacity(t, f, 20, 20, false)
		patch := capacityPatch(f)
		patch.FactWithdrawals = []CandidateID{"candidate:capacity:2:000"}
		applyAdmission(t, f, patch)
		expectCapacity(t, f, 20, 19, false)
	}
	if !bytes.Equal(before, mustCurrentBytes(t, k)) {
		t.Fatal("fork publication changed source")
	}
}
