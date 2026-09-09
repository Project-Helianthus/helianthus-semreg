package semreg

import (
	"bytes"
	"reflect"
	"testing"
)

func retainedResult(t *testing.T, view EvaluationView, id CandidateID) EvaluatedRetainedObservation {
	t.Helper()
	for _, record := range view.Retained {
		if record.Observation.Candidate.CandidateID == id {
			return record
		}
	}
	t.Fatalf("missing retained observation %s", id)
	return EvaluatedRetainedObservation{}
}

func TestRetainedObservationGenerationFenceLifecycle(t *testing.T) {
	kernel := newTestPublicationKernel(t, "asset:retained")
	initial := completePublicationBatch("asset:retained", "source:retained", "epoch:retained", "binding:old", "1", "1", "0")
	power := initial.FactUpserts[0]
	power.CandidateID, power.Key.FactID = "candidate:power:old", "fact.power"
	power.Times = Times{ReceivedAt: TimePoint{UnixNanoseconds: "1000", ClockID: "clock.utc", UncertaintyNS: "0"}, ReceiptMonotonic: MonotonicPoint{ClockEpochID: "clock-epoch:retained", Nanoseconds: "100"}, EvaluatedAt: TimePoint{UnixNanoseconds: "1000", ClockID: "clock.utc", UncertaintyNS: "0"}, EvaluateMonotonic: MonotonicPoint{ClockEpochID: "clock-epoch:retained", Nanoseconds: "100"}}
	power.FreshnessPolicy = FreshnessPolicy{PolicyID: "policy:retained", Version: "1.0.0", FreshForNS: "30", RetainForNS: "120", MaxWallUncertaintyNS: "0"}
	initial.FactUpserts = []FactCandidate{power}
	sealPublicationBatch(t, &initial)
	if _, _, err := kernel.Apply(initial, MonotonicPoint{ClockEpochID: "clock-epoch:retained", Nanoseconds: "100"}); err != nil {
		t.Fatal(err)
	}

	transition := publicationBatch("asset:retained", "source:retained", "epoch:retained", "2", "1", "1")
	transition.BindingUpserts = []NativeBinding{publicationBinding("asset:retained", "source:retained", "epoch:retained", "binding:new", "2")}
	transition.GenerationFences = []GenerationFence{publicationFence("source:retained", "epoch:retained", "1", publicEvidence("9"))}
	// A current independent field advances on the replacement path while the
	// failed power field is never rebound to that path.
	voltage := publicationCandidate("candidate:voltage:new", "fact.voltage", true, "source:retained", "epoch:retained", "binding:new", "2")
	transition.FactUpserts = []FactCandidate{voltage}
	sealPublicationBatch(t, &transition)
	result, raw, err := kernel.Apply(transition, MonotonicPoint{ClockEpochID: "clock-epoch:retained", Nanoseconds: "110"})
	if err != nil {
		t.Fatal(err)
	}
	if hasCandidate(result, power.CandidateID) || len(result.Retained) != 1 {
		t.Fatalf("fenced observation was not represented only as retained state: %+v", result)
	}
	retained := result.Retained[0]
	if retained.Contract != ContractRetainedObservationV1 || retained.Removal != RetainedRemovalGenerationFence || !reflect.DeepEqual(retained.Candidate, power) || retained.Candidate.BindingID == voltage.BindingID {
		t.Fatalf("retained observation changed its original binding/value/times/evidence: %+v", retained)
	}
	if !hasCandidate(result, voltage.CandidateID) || result.Revisions.Facts != "2" {
		t.Fatalf("independent replacement observation did not advance: %+v", result)
	}
	canonical, err := CanonicalJSON(result)
	if err != nil || !bytes.Equal(raw, canonical) || result.Validate() != nil {
		t.Fatalf("retained snapshot is not canonical/valid: %v", err)
	}
	view, err := EvaluateSnapshot(result, EvaluationContext{EvaluatedAt: TimePoint{UnixNanoseconds: "1030", ClockID: "clock.utc", UncertaintyNS: "0"}, EvaluateMonotonic: MonotonicPoint{ClockEpochID: "clock-epoch:retained", Nanoseconds: "130"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := retainedResult(t, view, power.CandidateID); got.Observation.Removal != RetainedRemovalGenerationFence || got.Freshness != FreshnessStale {
		t.Fatalf("retained evaluation: %+v", got)
	}
	if hasCandidate(result, power.CandidateID) {
		t.Fatal("retained observation became current operation/precondition input")
	}

	bad := publicationBatch("asset:retained", "source:retained", "epoch:retained", "2", "2", "2")
	bad.FactUpserts = []FactCandidate{publicationCandidate("candidate:bad", "fact.power", true, "source:retained", "epoch:retained", "binding:old", "1")}
	sealPublicationBatch(t, &bad)
	assertRejectedUnchanged(t, kernel, bad, InvalidValue)

	before, beforeBytes, _ := kernel.Current()
	expired, view, raw, _, err := kernel.CurrentAt(EvaluationContext{EvaluatedAt: TimePoint{UnixNanoseconds: "1120", ClockID: "clock.utc", UncertaintyNS: "0"}, EvaluateMonotonic: MonotonicPoint{ClockEpochID: "clock-epoch:retained", Nanoseconds: "220"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Retained) != 0 || !reflect.DeepEqual(expired, before) || !bytes.Equal(raw, beforeBytes) {
		t.Fatalf("current/readback expiry mutated snapshot or exposed retained state: %+v", view)
	}
}

func TestRetainedObservationSourceEpochRetirement(t *testing.T) {
	kernel := newTestPublicationKernel(t, "asset:retained-epoch")
	initial := completePublicationBatch("asset:retained-epoch", "source:retained-epoch", "epoch:old", "binding:old", "1", "1", "0")
	sealPublicationBatch(t, &initial)
	if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	restart := publicationBatch("asset:retained-epoch", "source:retained-epoch", "epoch:new", "1", "1", "1")
	restart.SourceUpserts = []SourceDescriptor{publicationSource("source:retained-epoch", "epoch:new")}
	restart.SourceRetirements = []SourceEpochID{"epoch:old"}
	restart.BindingUpserts = []NativeBinding{publicationBinding("asset:retained-epoch", "source:retained-epoch", "epoch:new", "binding:new", "1")}
	restart.FactUpserts = []FactCandidate{publicationCandidate("candidate:new", "fact.voltage", true, "source:retained-epoch", "epoch:new", "binding:new", "1")}
	sealPublicationBatch(t, &restart)
	result, _, err := kernel.Apply(restart, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Retained) != 1 || result.Retained[0].Candidate.SourceEpochID == nil || *result.Retained[0].Candidate.SourceEpochID != "epoch:old" || result.Retained[0].Removal != RetainedRemovalSourceRetirement || !hasCandidate(result, "candidate:new") {
		t.Fatalf("epoch retirement did not preserve original retained path and independent current fact: %+v", result)
	}
}

func TestRetainedObservationStableCandidateIDHistoryAndOrdering(t *testing.T) {
	kernel := newTestPublicationKernel(t, "asset:retained-history")
	initial := completePublicationBatch("asset:retained-history", "source:retained-history", "epoch:retained-history", "binding:one", "1", "1", "0")
	stable := initial.FactUpserts[0]
	stable.CandidateID = "candidate:stable"
	initial.FactUpserts = []FactCandidate{stable}
	sealPublicationBatch(t, &initial)
	if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
		t.Fatal(err)
	}

	transition := func(generation, expected Uint64, oldBinding, newBinding NativeBindingID, revision Uint64) Snapshot {
		batch := publicationBatch("asset:retained-history", "source:retained-history", "epoch:retained-history", generation, "1", expected)
		batch.BindingUpserts = []NativeBinding{publicationBinding("asset:retained-history", "source:retained-history", "epoch:retained-history", newBinding, generation)}
		previous := Uint64("1")
		if generation == "3" {
			previous = "2"
		}
		batch.GenerationFences = []GenerationFence{publicationFence("source:retained-history", "epoch:retained-history", previous, publicEvidence("9"))}
		replacement := publicationCandidate("candidate:stable", "fact.power", true, "source:retained-history", "epoch:retained-history", newBinding, generation)
		replacement.Revision = revision
		batch.FactUpserts = []FactCandidate{replacement}
		sealPublicationBatch(t, &batch)
		if err := batch.Validate(); err != nil {
			t.Fatalf("replacement batch invalid: %v", err)
		}
		result, _, err := kernel.Apply(batch, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	second := transition("2", "1", "binding:one", "binding:two", "2")
	if len(second.Retained) != 1 || !hasCandidate(second, "candidate:stable") || second.Retained[0].Candidate.BindingID == nil || *second.Retained[0].Candidate.BindingID != "binding:one" {
		t.Fatalf("same candidate ID did not retain immutable old observation beside current replacement: %+v", second)
	}
	view, err := EvaluateSnapshot(second, EvaluationContext{EvaluatedAt: TimePoint{UnixNanoseconds: "200", ClockID: "clock.utc", UncertaintyNS: "0"}, EvaluateMonotonic: publicationMonotonic})
	if err != nil || view.Validate() != nil || len(view.Retained) != 1 || view.Retained[0].Observation.Candidate.CandidateID != "candidate:stable" || !reflect.DeepEqual(view.Retained[0].Observation, second.Retained[0]) {
		t.Fatalf("same-ID retained evaluation/digest binding: view=%+v err=%v", view, err)
	}
	third := transition("3", "2", "binding:two", "binding:three", "3")
	if len(third.Retained) != 2 || !hasCandidate(third, "candidate:stable") || retainedObservationOrderError(third.Retained) != nil {
		t.Fatalf("repeated stable-ID replacements lost historical observations: %+v", third)
	}
	unordered := cloneSnapshot(third)
	unordered.Retained[0], unordered.Retained[1] = unordered.Retained[1], unordered.Retained[0]
	recomputeSnapshotID(t, &unordered)
	requireID(t, unordered.Validate(), NoncanonicalOrder)
	duplicate := cloneSnapshot(third)
	duplicate.Retained = append(duplicate.Retained, duplicate.Retained[0])
	recomputeSnapshotID(t, &duplicate)
	requireID(t, duplicate.Validate(), DuplicateKey)
}

func TestRetainedObservationExplicitWithdrawalRemovesHistoricalRecords(t *testing.T) {
	kernel := newTestPublicationKernel(t, "asset:retained-withdrawal")
	initial := completePublicationBatch("asset:retained-withdrawal", "source:retained-withdrawal", "epoch:retained-withdrawal", "binding:old", "1", "1", "0")
	old := initial.FactUpserts[0]
	sealPublicationBatch(t, &initial)
	if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	fence := publicationBatch("asset:retained-withdrawal", "source:retained-withdrawal", "epoch:retained-withdrawal", "2", "1", "1")
	fence.GenerationFences = []GenerationFence{publicationFence("source:retained-withdrawal", "epoch:retained-withdrawal", "1", publicEvidence("9"))}
	sealPublicationBatch(t, &fence)
	retained, _, err := kernel.Apply(fence, publicationMonotonic)
	if err != nil || len(retained.Retained) != 1 {
		t.Fatalf("fence retained fixture: %+v %v", retained, err)
	}
	withdraw := publicationBatch("asset:retained-withdrawal", "source:retained-withdrawal", "epoch:retained-withdrawal", "2", "2", "2")
	withdraw.FactWithdrawals = []CandidateID{old.CandidateID}
	sealPublicationBatch(t, &withdraw)
	result, _, err := kernel.Apply(withdraw, publicationMonotonic)
	if err != nil || len(result.Retained) != 0 || len(result.Facts) != 0 || !reflect.DeepEqual(result.Sources, retained.Sources) || !reflect.DeepEqual(result.Bindings, retained.Bindings) {
		t.Fatalf("retained explicit withdrawal changed unrelated state: result=%+v err=%v", result, err)
	}
}
