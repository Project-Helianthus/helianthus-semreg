package semreg

import (
	"fmt"
	"testing"
)

func admissionSet(count int, generation, sequence, expected Uint64) PublicationBatch {
	b := publicationBatch("asset:capacity", "source:capacity", "epoch:capacity", generation, sequence, expected)
	b.ObservedAt = TimePoint{UnixNanoseconds: "1000", ClockID: "clock.utc", UncertaintyNS: "0"}
	binding := NativeBindingID("binding:capacity:" + string(generation))
	b.SourceUpserts = []SourceDescriptor{publicationSource(b.SourceID, b.SourceEpochID)}
	b.BindingUpserts = []NativeBinding{publicationBinding(b.AssetID, b.SourceID, b.SourceEpochID, binding, generation)}
	for i := 0; i < count; i++ {
		c := publicationCandidate(CandidateID(fmt.Sprintf("candidate:capacity:%s:%03d", generation, i)), DefinitionID(fmt.Sprintf("fact.field%03d", i)), true, b.SourceID, b.SourceEpochID, binding, generation)
		c.Times = Times{ReceivedAt: b.ObservedAt, EvaluatedAt: b.ObservedAt, ReceiptMonotonic: publicationMonotonic, EvaluateMonotonic: publicationMonotonic}
		c.FreshnessPolicy = FreshnessPolicy{PolicyID: "policy:capacity", Version: "1.0.0", FreshForNS: "30", RetainForNS: "120", MaxWallUncertaintyNS: "0"}
		b.FactUpserts = append(b.FactUpserts, c)
	}
	return b
}

func applyAdmission(t *testing.T, k *PublicationKernel, b PublicationBatch) Snapshot {
	t.Helper()
	sealPublicationBatch(t, &b)
	s, _, err := k.Apply(b, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRetainedAdmissionFortyAndFortyOne(t *testing.T) {
	for _, n := range []int{40, 41} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			k := newTestPublicationKernel(t, "asset:capacity")
			b := admissionSet(n, "1", "1", "0")
			sealPublicationBatch(t, &b)
			if n == 41 {
				assertRejectedUnchanged(t, k, b, BoundsExceeded)
			} else {
				if _, _, err := k.Apply(b, publicationMonotonic); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRetainedAdmissionTwoSetsAndThirdRefusal(t *testing.T) {
	k := newTestPublicationKernel(t, "asset:capacity")
	applyAdmission(t, k, admissionSet(20, "1", "1", "0"))
	second := admissionSet(20, "2", "1", "1")
	second.GenerationFences = []GenerationFence{publicationFence(second.SourceID, second.SourceEpochID, "1", publicEvidence("9"))}
	s := applyAdmission(t, k, second)
	if len(s.Retained) != 20 {
		t.Fatal("first history lost")
	}
	fence := publicationBatch(second.AssetID, second.SourceID, second.SourceEpochID, "2", "2", "2")
	fence.ObservedAt = second.ObservedAt
	fence.GenerationFences = []GenerationFence{publicationFence(second.SourceID, second.SourceEpochID, "2", publicEvidence("9"))}
	s = applyAdmission(t, k, fence)
	d, _ := k.RetainedCapacity()
	if !d.DowngradeBlocked {
		t.Fatal("former-bound downgrade must refuse")
	}
	if len(s.Retained) != 40 {
		t.Fatal("second history lost")
	}
	third := admissionSet(20, "3", "1", "3")
	sealPublicationBatch(t, &third)
	assertRejectedUnchanged(t, k, third, BoundsExceeded)
}

func TestRetainedAdmissionFenceBoundaryAndEmptyGeneration(t *testing.T) {
	t.Parallel()
	// The shared history proves that each of the 127 accepted generations
	// reserved exactly one fence cursor. Fencing the live generation 128
	// retains its twenty candidates on the 128th cursor.
	history := sharedCapacityHistory(t)
	k, err := history.live.Fork()
	if err != nil {
		t.Fatal(err)
	}
	last := history.liveLog[len(history.liveLog)-1].Batch
	f := publicationBatch(last.AssetID, last.SourceID, last.SourceEpochID, "128", "2", history.liveRevision)
	f.ObservedAt = last.ObservedAt
	f.GenerationFences = []GenerationFence{publicationFence(last.SourceID, last.SourceEpochID, "128", publicEvidence("9"))}
	sealPublicationBatch(t, &f)
	s := applyAdmission(t, k, f)
	d, _ := k.RetainedCapacity()
	if d.FenceReservations != 128 || d.Retained != 20 || d.DowngradeBlocked {
		t.Fatalf("full fences %+v", d)
	}
	empty := admissionSet(0, "129", "1", s.Revisions.Semantic)
	sealPublicationBatch(t, &empty)
	assertRejectedUnchanged(t, k, empty, BoundsExceeded)
	// Structural import cannot represent 128 fence cursors plus one live cursor.
	// It must reject atomically instead of weakening the independent cursor bound.
	log := append(append([]AcceptedPublication(nil), history.liveLog...), AcceptedPublication{f, publicationMonotonic}, AcceptedPublication{empty, publicationMonotonic})
	restored := newTestPublicationKernel(t, "asset:capacity")
	if err := restored.RestoreAccepted(log); ErrorIdentifier(err) != BoundsExceeded {
		t.Fatalf("cursor-overflow import: %v", err)
	}
	if _, _, ok := restored.Current(); ok {
		t.Fatal("cursor-overflow import partially committed")
	}
}

func TestRetainedAdmissionDistinctTupleAxes(t *testing.T) {
	for _, axis := range []string{"source", "epoch", "generation", "binding"} {
		t.Run(axis, func(t *testing.T) {
			k := newTestPublicationKernel(t, "asset:capacity")
			initial := admissionSet(20, "1", "1", "0")
			applyAdmission(t, k, initial)
			b := admissionSet(20, "1", "1", "1")
			source, epoch, generation, binding := initial.SourceID, initial.SourceEpochID, Uint64("1"), NativeBindingID("binding:other")
			switch axis {
			case "source":
				source = "source:other"
			case "epoch":
				f := publicationBatch(initial.AssetID, source, epoch, "1", "2", "1")
				f.ObservedAt = initial.ObservedAt
				f.GenerationFences = []GenerationFence{publicationFence(source, epoch, "1", publicEvidence("9"))}
				applyAdmission(t, k, f)
				b.ExpectedSemanticRevision = "2"
				epoch = "epoch:other"
				b.SourceRetirements = []SourceEpochID{initial.SourceEpochID}
			case "generation":
				generation = "2"
				b.GenerationFences = []GenerationFence{publicationFence(source, epoch, "1", publicEvidence("9"))}
			case "binding":
				b.Sequence = "2"
			}
			b.SourceID, b.SourceEpochID, b.DriverGeneration = source, epoch, generation
			b.SourceUpserts = []SourceDescriptor{publicationSource(source, epoch)}
			b.BindingUpserts = []NativeBinding{publicationBinding(b.AssetID, source, epoch, binding, generation)}
			for i := range b.FactUpserts {
				c := publicationCandidate(CandidateID(fmt.Sprintf("candidate:other:%03d", i)), initial.FactUpserts[i].Key.FactID, true, source, epoch, binding, generation)
				c.Times = initial.FactUpserts[i].Times
				c.FreshnessPolicy = initial.FactUpserts[i].FreshnessPolicy
				b.FactUpserts[i] = c
			}
			s := applyAdmission(t, k, b)
			d, _ := k.RetainedCapacity()
			if d.Retained+d.CurrentObserved != 40 {
				t.Fatalf("%s collapsed debt %+v", axis, d)
			}
			wantTuples := 1
			if axis == "source" || axis == "epoch" || axis == "generation" {
				wantTuples = 2
			}
			if d.FenceReservations != wantTuples {
				t.Fatalf("%s tuple reservation %+v", axis, d)
			}
			// An extra observation on the exact second path must refuse without consuming
			// its sequence, even when its fact key/value equals an existing observation.
			extra := publicationBatch(b.AssetID, source, epoch, generation, increment(b.Sequence), s.Revisions.Semantic)
			extra.ObservedAt = b.ObservedAt
			c := b.FactUpserts[0]
			c.CandidateID = "candidate:overflow"
			extra.FactUpserts = []FactCandidate{c}
			sealPublicationBatch(t, &extra)
			assertRejectedUnchanged(t, k, extra, BoundsExceeded)
			extra.FactUpserts = []FactCandidate{}
			extra.FactWithdrawals = []CandidateID{b.FactUpserts[0].CandidateID}
			applyAdmission(t, k, extra)
			d, _ = k.RetainedCapacity()
			if d.Retained+d.CurrentObserved != 39 {
				t.Fatal("rejected sequence was not reusable")
			}

		})
	}
}
