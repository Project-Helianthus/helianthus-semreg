package semreg

import (
	"bytes"
	"reflect"
	"testing"
)

func retainedResult(t *testing.T, view EvaluationView, id CandidateID) EvaluatedRetainedObservation {
	t.Helper()
	for _, record := range view.Retained {
		if record.CandidateID == id {
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
	if retained.State != RetainedObservation || !reflect.DeepEqual(retained.Observation, power) || retained.Observation.BindingID == voltage.BindingID {
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
	if got := retainedResult(t, view, power.CandidateID); got.State != RetainedObservation || got.Freshness != FreshnessStale {
		t.Fatalf("retained evaluation: %+v", got)
	}
	if hasCandidate(result, power.CandidateID) {
		t.Fatal("retained observation became current operation/precondition input")
	}

	bad := publicationBatch("asset:retained", "source:retained", "epoch:retained", "2", "2", "2")
	bad.FactUpserts = []FactCandidate{publicationCandidate("candidate:bad", "fact.power", true, "source:retained", "epoch:retained", "binding:old", "1")}
	sealPublicationBatch(t, &bad)
	assertRejectedUnchanged(t, kernel, bad, InvalidValue)

	expire := publicationBatch("asset:retained", "source:retained", "epoch:retained", "2", "2", "2")
	expire.ObservedAt.UnixNanoseconds = "1300"
	sealPublicationBatch(t, &expire)
	expired, _, err := kernel.Apply(expire, MonotonicPoint{ClockEpochID: "clock-epoch:retained", Nanoseconds: "220"})
	if err != nil {
		t.Fatal(err)
	}
	if len(expired.Retained) != 0 {
		t.Fatalf("retained observation survived its original retention deadline: %+v", expired.Retained)
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
	if len(result.Retained) != 1 || result.Retained[0].Observation.SourceEpochID == nil || *result.Retained[0].Observation.SourceEpochID != "epoch:old" || !hasCandidate(result, "candidate:new") {
		t.Fatalf("epoch retirement did not preserve original retained path and independent current fact: %+v", result)
	}
}
