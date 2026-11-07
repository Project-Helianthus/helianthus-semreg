package semreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
)

func TestRetainedCapacityTypedVectors(t *testing.T) {
	// The accepted histories are large; this test and the fence-boundary test
	// run alongside each other so the race-detector gate stays well inside its
	// package budget.
	t.Parallel()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "v1", "retained-observation-acceptance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Scenarios []struct {
			ID        string `json:"id"`
			Operation string `json:"operation"`
			Input     struct {
				History         int    `json:"history"`
				Current         int    `json:"current"`
				Upserts         int    `json:"upserts"`
				Replacements    int    `json:"same_binding_replacements"`
				Inferred        int    `json:"inferred"`
				Expiry          int    `json:"expiry_candidates"`
				Clock           string `json:"clock"`
				Mode            string `json:"mode"`
				Replay          bool   `json:"replay"`
				Earlier         string `json:"earlier_error"`
				ExpiredCurrent  int    `json:"currently_expired_observed"`
				Fences          int    `json:"fences"`
				Future          int    `json:"future_fence_tuples"`
				CurrentUnfenced int    `json:"current_unfenced_tuples"`
				Refreshes       int    `json:"refresh_repetitions"`
			} `json:"input"`
			// Typed admission and structural scenarios carry one expectation
			// object; the remaining scenarios carry an array of named
			// expectations, so decoding is deferred until the operation is known.
			RawExpect json.RawMessage `json:"expect"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	executed := 0
	history := sharedCapacityHistory(t)
	// The max-profile restore proofs replay the shared histories into fresh
	// kernels; they run alongside the fixture scenarios.
	t.Run("max-profile-live-restore", func(t *testing.T) {
		t.Parallel()
		restored := newTestPublicationKernel(t, "asset:capacity")
		if err := restored.RestoreAccepted(history.liveLog); err != nil {
			t.Fatalf("restore 127 fences plus live: %v", err)
		}
		if !bytes.Equal(mustCurrentBytes(t, history.live), mustCurrentBytes(t, restored)) {
			t.Fatal("live restore differs")
		}
		expectCapacity(t, restored, 0, 20, false)
		if d, _ := restored.RetainedCapacity(); d.FenceReservations != 128 {
			t.Fatalf("live reservations %+v", d)
		}
	})
	t.Run("max-profile-fenced-restore", func(t *testing.T) {
		t.Parallel()
		restored := newTestPublicationKernel(t, "asset:capacity")
		if err := restored.RestoreAccepted(history.fencedLog); err != nil {
			t.Fatalf("restore 128 fences: %v", err)
		}
		if !bytes.Equal(mustCurrentBytes(t, history.fenced), mustCurrentBytes(t, restored)) {
			t.Fatal("fenced restore differs")
		}
		if d, _ := restored.RetainedCapacity(); d.FenceReservations != 128 || d.RecoveryBlocked {
			t.Fatalf("fenced reservations %+v", d)
		}
	})
	for _, v := range fixture.Scenarios {
		if v.Operation != "bounded_admission" && v.Operation != "structural_restore" {
			continue
		}
		var expect struct {
			Result  string `json:"result"`
			H       int    `json:"H"`
			C       int    `json:"C"`
			Blocked bool   `json:"recovery_blocked"`
		}
		if err := json.Unmarshal(v.RawExpect, &expect); err != nil {
			t.Fatalf("%s: expect: %v", v.ID, err)
		}
		switch v.ID {
		case "RO-CAP-fence_boundary", "RO-CAP-fence_excess", "RO-CAP-invalid_fence_restore", "RO-CAP-invalid_empty_fence_restore", "RO-CAP-empty_generation_fence_reservation":
			executed++
			t.Run(v.ID, func(t *testing.T) {
				t.Parallel()
				history.run(t, v.ID, v.Operation, v.Input.Fences, v.Input.Current, v.Input.Upserts, v.Input.Future, v.Input.CurrentUnfenced, expect.Result, expect.H, expect.C, expect.Blocked)
			})
			continue
		}
		executed++
		t.Run(v.ID, func(t *testing.T) {
			t.Parallel()
			k, log := restoredCapacity(t, v.Input.History, v.Input.Current, v.Input.Expiry)
			if v.Input.ExpiredCurrent > 0 {
				last := &log[len(log)-1]
				for i := 0; i < v.Input.ExpiredCurrent; i++ {
					last.Batch.FactUpserts[i].FreshnessPolicy.RetainForNS = "40"
				}
				sealPublicationBatch(t, &last.Batch)
				k = newTestPublicationKernel(t, "asset:capacity")
				if err := k.RestoreAccepted(log); err != nil {
					t.Fatal(err)
				}
			}
			before := mustCurrentBytes(t, k)
			if v.Input.Mode == "restore" {
				expectCapacity(t, k, expect.H, expect.C, expect.Blocked)
				return
			}
			if v.Input.Replay {
				last := log[len(log)-1]
				_, got, err := k.Apply(last.Batch, last.PublicationMonotonic)
				if err != nil || !bytes.Equal(got, before) || !bytes.Equal(before, mustCurrentBytes(t, k)) {
					t.Fatalf("accepted replay %v", err)
				}
				return
			}
			repetitions := v.Input.Refreshes
			if repetitions == 0 {
				repetitions = 1
			}
			for n := 0; n < repetitions; n++ {
				b := capacityPatch(k)
				s, _, _ := k.Current()
				if v.ID == "RO-CAP-mixed_paths" {
					b = publicationBatch("asset:capacity", "source:other", "epoch:other", "1", "1", s.Revisions.Semantic)
					b.ObservedAt = s.EvaluatedAt
					b.SourceUpserts = []SourceDescriptor{publicationSource(b.SourceID, b.SourceEpochID)}
					b.BindingUpserts = []NativeBinding{publicationBinding(b.AssetID, b.SourceID, b.SourceEpochID, "binding:other", "1")}
				}
				if v.Input.Replacements > 0 {
					for _, e := range s.Facts {
						for _, c := range e.Candidates {
							c.Revision = increment(c.Revision)
							// A revision bump requires a content change; the
							// accepted candidates hold true, so alternate from false.
							c.Value.Boolean = pointerRecord(n%2 == 1)
							b.FactUpserts = append(b.FactUpserts, c)
						}
					}
				} else {
					for i := 0; i < v.Input.Upserts; i++ {
						binding := s.Bindings[len(s.Bindings)-1].BindingID
						if len(b.BindingUpserts) > 0 {
							binding = b.BindingUpserts[0].BindingID
						}
						b.FactUpserts = append(b.FactUpserts, publicationCandidate(CandidateID(fmt.Sprintf("candidate:new:%03d", i)), DefinitionID(fmt.Sprintf("fact.new%03d", i)), true, b.SourceID, b.SourceEpochID, binding, b.DriverGeneration))
					}
				}
				for i := 0; i < v.Input.Inferred; i++ {
					b.FactUpserts = append(b.FactUpserts, publicationDerivedCandidate(CandidateID(fmt.Sprintf("candidate:derived:%03d", i)), DefinitionID(fmt.Sprintf("fact.derived%03d", i)), []FactCandidate{s.Facts[0].Candidates[0]}))
				}
				mono := publicationMonotonic
				if v.Input.Expiry > 0 || v.Input.ExpiredCurrent > 0 {
					b.ObservedAt.UnixNanoseconds = "1120"
					mono.Nanoseconds = "220"
				}
				if v.Input.Clock == "unknown" {
					b.ObservedAt.UncertaintyNS = "1"
					mono.ClockEpochID = "clock-epoch:unknown"
				}
				sealPublicationBatch(t, &b)
				if v.Input.Earlier != "" {
					b.ObservedAt.UnixNanoseconds = "bad"
				}
				_, _, err := k.Apply(b, mono)
				if expect.Result == "accept" {
					if err != nil {
						t.Fatal(err)
					}
					expectCapacity(t, k, expect.H, expect.C, expect.Blocked)
				} else {
					if string(ErrorIdentifier(err)) != expect.Result {
						t.Fatalf("got %v want %s", err, expect.Result)
					}
					if !bytes.Equal(before, mustCurrentBytes(t, k)) {
						t.Fatal("rejection advanced state")
					}
				}
			}
		})
	}
	if executed != 22 {
		t.Fatalf("typed capacity inventory %d", executed)
	}
}

// capacityHistory is the accepted max-profile history shared by the retained
// proofs: one tuple admitted and fenced through 127 generations, then
// generation 128 either live (two bindings and twenty candidates on the
// unfenced tuple) or fenced (an empty admission and its fence). Building it
// replays hundreds of kernel applications, so the test binary builds it once
// and checks the reservation count after every generation; each proof forks
// its own copy of the accepted state and never mutates the shared logs.
type capacityHistory struct {
	live, fenced                 *PublicationKernel
	liveLog, fencedLog           []AcceptedPublication
	liveRevision, fencedRevision Uint64
	err                          error
}

var (
	capacityHistoryOnce sync.Once
	capacityHistoryData *capacityHistory
)

func sharedCapacityHistory(t *testing.T) *capacityHistory {
	t.Helper()
	capacityHistoryOnce.Do(func() { capacityHistoryData = buildCapacityHistory() })
	if capacityHistoryData.err != nil {
		t.Fatalf("shared capacity history: %v", capacityHistoryData.err)
	}
	return capacityHistoryData
}

func sealBatch(batch *PublicationBatch) error {
	digest, err := batch.ComputedDigest()
	if err != nil {
		return err
	}
	batch.BatchDigest = digest
	return nil
}

func buildCapacityHistory() *capacityHistory {
	h := &capacityHistory{}
	k, err := testPublicationKernel("asset:capacity")
	if err != nil {
		h.err = err
		return h
	}
	apply := func(k *PublicationKernel, b *PublicationBatch, what string) (Snapshot, error) {
		if err := sealBatch(b); err != nil {
			return Snapshot{}, fmt.Errorf("%s: %w", what, err)
		}
		s, _, err := k.Apply(*b, publicationMonotonic)
		if err != nil {
			return Snapshot{}, fmt.Errorf("%s: %w", what, err)
		}
		return s, nil
	}
	observed := TimePoint{UnixNanoseconds: "1000", ClockID: "clock.utc", UncertaintyNS: "0"}
	var log []AcceptedPublication
	revision := Uint64("0")
	for generation := 1; generation <= 127; generation++ {
		g := Uint64(fmt.Sprint(generation))
		b := publicationBatch("asset:capacity", "source:capacity", "epoch:capacity", g, "1", revision)
		b.ObservedAt = observed
		if generation == 1 {
			b.SourceUpserts = []SourceDescriptor{publicationSource(b.SourceID, b.SourceEpochID)}
		}
		s, err := apply(k, &b, "generation "+string(g))
		if err != nil {
			h.err = err
			return h
		}
		log = append(log, AcceptedPublication{b, publicationMonotonic})
		// Every accepted generation reserves exactly one fence cursor.
		if d, _ := k.RetainedCapacity(); d.FenceReservations != generation {
			h.err = fmt.Errorf("generation %d reservations %+v", generation, d)
			return h
		}
		f := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, g, "2", s.Revisions.Semantic)
		f.ObservedAt = observed
		f.GenerationFences = []GenerationFence{publicationFence(b.SourceID, b.SourceEpochID, g, publicEvidence("9"))}
		if s, err = apply(k, &f, "fence "+string(g)); err != nil {
			h.err = err
			return h
		}
		log = append(log, AcceptedPublication{f, publicationMonotonic})
		revision = s.Revisions.Semantic
	}

	live, err := k.Fork()
	if err != nil {
		h.err = err
		return h
	}
	b := admissionSet(20, "128", "1", revision)
	other := publicationBinding(b.AssetID, b.SourceID, b.SourceEpochID, "binding:capacity:other", "128")
	b.BindingUpserts = append(b.BindingUpserts, other)
	for i := 10; i < 20; i++ {
		b.FactUpserts[i].BindingID = pointerRecord(other.BindingID)
		b.FactUpserts[i].Origin.BindingID = pointerRecord(other.BindingID)
	}
	s, err := apply(live, &b, "live generation 128")
	if err != nil {
		h.err = err
		return h
	}
	if d, _ := live.RetainedCapacity(); d.FenceReservations != 128 || d.Retained != 0 || d.CurrentObserved != 20 || d.RecoveryBlocked {
		h.err = fmt.Errorf("live reservations %+v", d)
		return h
	}
	h.live, h.liveRevision = live, s.Revisions.Semantic
	h.liveLog = append(append([]AcceptedPublication(nil), log...), AcceptedPublication{b, publicationMonotonic})

	e := admissionSet(0, "128", "1", revision)
	if s, err = apply(k, &e, "fenced generation 128"); err != nil {
		h.err = err
		return h
	}
	log = append(log, AcceptedPublication{e, publicationMonotonic})
	f := publicationBatch(e.AssetID, e.SourceID, e.SourceEpochID, "128", "2", s.Revisions.Semantic)
	f.ObservedAt = e.ObservedAt
	f.GenerationFences = []GenerationFence{publicationFence(e.SourceID, e.SourceEpochID, "128", publicEvidence("9"))}
	if s, err = apply(k, &f, "fence 128"); err != nil {
		h.err = err
		return h
	}
	log = append(log, AcceptedPublication{f, publicationMonotonic})
	if d, _ := k.RetainedCapacity(); d.FenceReservations != 128 || d.RecoveryBlocked {
		h.err = fmt.Errorf("fenced reservations %+v", d)
		return h
	}
	h.fenced, h.fencedLog, h.fencedRevision = k, log, s.Revisions.Semantic
	return h
}

func (v *capacityHistory) run(t *testing.T, id, operation string, fences, current, upserts, future, unfenced int, result string, h, c int, blocked bool) {
	t.Helper()
	if future != 1 || h != 0 || blocked {
		t.Fatalf("unexpected vector expectation %s", id)
	}
	switch id {
	case "RO-CAP-fence_boundary":
		if operation != "bounded_admission" || fences != 127 || current != 20 || upserts != 0 || unfenced != 1 || result != "accept" || c != 20 {
			t.Fatalf("unexpected vector %s", id)
		}
		k, err := v.live.Fork()
		if err != nil {
			t.Fatal(err)
		}
		b := publicationBatch("asset:capacity", "source:capacity", "epoch:capacity", "128", "2", v.liveRevision)
		b.ObservedAt = TimePoint{UnixNanoseconds: "1000", ClockID: "clock.utc", UncertaintyNS: "0"}
		applyAdmission(t, k, b)
		expectCapacity(t, k, h, c, blocked)
		d, _ := k.RetainedCapacity()
		if d.FenceReservations != 128 {
			t.Fatalf("reservations %+v", d)
		}
	case "RO-CAP-fence_excess", "RO-CAP-empty_generation_fence_reservation":
		wantUpserts := 0
		if id == "RO-CAP-fence_excess" {
			wantUpserts = 20
		}
		if operation != "bounded_admission" || fences != 128 || current != 0 || upserts != wantUpserts || unfenced != 0 || result != "bounds_exceeded" || c != wantUpserts {
			t.Fatalf("unexpected vector %s", id)
		}
		k, err := v.fenced.Fork()
		if err != nil {
			t.Fatal(err)
		}
		before, err := k.Fork()
		if err != nil {
			t.Fatal(err)
		}
		b := admissionSet(upserts, "129", "1", v.fencedRevision)
		sealPublicationBatch(t, &b)
		assertRejectedUnchanged(t, k, b, BoundsExceeded)
		if !reflect.DeepEqual(k.replays, before.replays) {
			t.Fatal("rejection changed replay state")
		}
		expectCapacity(t, k, 0, 0, false)
	case "RO-CAP-invalid_fence_restore", "RO-CAP-invalid_empty_fence_restore":
		wantCurrent := 0
		if id == "RO-CAP-invalid_fence_restore" {
			wantCurrent = 20
		}
		if operation != "structural_restore" || fences != 128 || current != wantCurrent || upserts != 0 || unfenced != 1 || result != "bounds_exceeded" || c != 0 {
			t.Fatalf("unexpected vector %s", id)
		}
		b := admissionSet(wantCurrent, "129", "1", v.fencedRevision)
		sealPublicationBatch(t, &b)
		log := append(append([]AcceptedPublication(nil), v.fencedLog...), AcceptedPublication{b, publicationMonotonic})
		k := newTestPublicationKernel(t, "asset:capacity")
		if err := k.RestoreAccepted(log); ErrorIdentifier(err) != BoundsExceeded {
			t.Fatalf("invalid restore: %v", err)
		}
		if _, _, ok := k.Current(); ok || len(k.canonical) != 0 || len(k.replays) != 0 {
			t.Fatal("invalid restore partially committed")
		}
	}
}
