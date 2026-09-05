package semreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"testing"
)

func invalidCandidateFixture(t *testing.T) (Snapshot, EvaluationView, Selection) {
	t.Helper()
	power := evaluationCandidate("candidate:invalid-id:power", "fact.power", "1000", "100")
	voltage := evaluationCandidate("candidate:invalid-id:voltage", "fact.voltage", "1000", "100")
	snapshot := evaluationSnapshot(t, power, voltage)
	view, err := EvaluateSnapshot(snapshot, evaluationContext("1030", "clock-epoch:evaluation", "130", "0"))
	if err != nil {
		t.Fatal(err)
	}
	selection := Selection{
		Contract:          ContractSelectionV1,
		SnapshotID:        snapshot.SnapshotID,
		Revisions:         snapshot.Revisions,
		EvaluationDigest:  view.EvaluationDigest,
		Context:           view.Context,
		Key:               voltage.Key,
		PolicyID:          "policy:invalid-candidate",
		PolicyVersion:     "1.0.0",
		SelectedCandidate: voltage.CandidateID,
		CandidateRevision: voltage.Revision,
		PresentationOnly:  true,
	}
	if err := ValidateSelection(snapshot, view, selection); err != nil {
		t.Fatalf("invalid test fixture: %v", err)
	}
	return snapshot, view, selection
}

func sealStructurallyInvalidView(t *testing.T, view *EvaluationView, selection *Selection) {
	t.Helper()
	digest, err := view.computedDigestUnchecked()
	if err != nil {
		t.Fatal(err)
	}
	view.EvaluationDigest = digest
	selection.EvaluationDigest = digest
}

func testJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertInvalidSelectionEntryPoints(t *testing.T, snapshot Snapshot, view EvaluationView, selection Selection, want ErrorID) {
	t.Helper()
	policy := &recordingSelectionPolicy{id: selection.PolicyID, version: selection.PolicyVersion, chosen: selection.SelectedCandidate}
	kernel, err := NewSelectionKernel(policy)
	if err != nil {
		t.Fatal(err)
	}
	before := testJSON(t, []any{snapshot, view, selection})
	raw := testJSON(t, view)
	rawBefore := append([]byte(nil), raw...)

	typedErr := view.Validate()
	decoded, wireErr := DecodeEvaluationView(raw)
	result, dispatchErr := kernel.SelectPresentation(snapshot, view, selection.Key, selection.PolicyID, selection.PolicyVersion)
	resultErr := ValidateSelection(snapshot, view, selection)
	for name, got := range map[string]error{
		"typed":    typedErr,
		"wire":     wireErr,
		"dispatch": dispatchErr,
		"result":   resultErr,
	} {
		t.Run(name, func(t *testing.T) {
			requireID(t, got, want)
		})
	}
	if policy.Calls() != 0 {
		t.Fatalf("invalid request invoked policy %d times", policy.Calls())
	}
	if !reflect.DeepEqual(decoded, EvaluationView{}) || !reflect.DeepEqual(result, Selection{}) {
		t.Fatalf("rejection returned partial values: decoded=%+v result=%+v", decoded, result)
	}
	if !bytes.Equal(raw, rawBefore) || !bytes.Equal(before, testJSON(t, []any{snapshot, view, selection})) {
		t.Fatal("rejection mutated caller input")
	}
}

func TestEvaluationViewRepeatedInvalidCandidateClassification(t *testing.T) {
	for _, id := range []CandidateID{"", "!"} {
		for _, wrongContract := range []bool{false, true} {
			t.Run(fmt.Sprintf("id_%q/wrong_contract_%t", id, wrongContract), func(t *testing.T) {
				snapshot, view, selection := invalidCandidateFixture(t)
				for index := range view.Facts {
					view.Facts[index].CandidateID = id
				}
				if wrongContract {
					view.Contract = "wrong"
				}
				sealStructurallyInvalidView(t, &view, &selection)
				want := InvalidIdentifier
				if wrongContract {
					want = InvalidContract
				}
				assertInvalidSelectionEntryPoints(t, snapshot, view, selection, want)
			})
		}
	}
}

func TestEvaluationViewValidKeyCollectionWithMalformedSiblings(t *testing.T) {
	t.Run("typed dispatch and result", func(t *testing.T) {
		snapshot, view, selection := invalidCandidateFixture(t)
		duplicate := view.Facts[0]
		duplicate.CandidateRevision = "0"
		duplicate.Freshness = "wrong"
		view.Facts = append(view.Facts, duplicate)
		sealStructurallyInvalidView(t, &view, &selection)
		assertInvalidSelectionEntryPoints(t, snapshot, view, selection, DuplicateKey)
	})

	_, view, _ := invalidCandidateFixture(t)
	for _, sibling := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing", func(fact map[string]any) { delete(fact, "candidate_revision") }},
		{"null", func(fact map[string]any) { fact["candidate_revision"] = nil }},
		{"wrong token", func(fact map[string]any) { fact["candidate_revision"] = 1 }},
	} {
		t.Run("wire/"+sibling.name, func(t *testing.T) {
			var root map[string]any
			if err := json.Unmarshal(testJSON(t, view), &root); err != nil {
				t.Fatal(err)
			}
			facts := root["facts"].([]any)
			original := facts[0].(map[string]any)
			duplicate := map[string]any{
				"candidate_id":           original["candidate_id"],
				"candidate_revision":     original["candidate_revision"],
				"freshness":              original["freshness"],
				"effective_availability": original["effective_availability"],
			}
			sibling.mutate(duplicate)
			root["facts"] = append(facts, duplicate)
			raw := testJSON(t, root)
			before := append([]byte(nil), raw...)
			decoded, err := DecodeEvaluationView(raw)
			requireID(t, err, DuplicateKey)
			if !reflect.DeepEqual(decoded, EvaluationView{}) || !bytes.Equal(raw, before) {
				t.Fatal("wire rejection returned a partial view or mutated input")
			}
		})
	}
}

func TestEvaluationViewValidKeyOrderingAcrossEntryPoints(t *testing.T) {
	snapshot, view, selection := invalidCandidateFixture(t)
	view.Facts[0], view.Facts[1] = view.Facts[1], view.Facts[0]
	sealStructurallyInvalidView(t, &view, &selection)
	assertInvalidSelectionEntryPoints(t, snapshot, view, selection, NoncanonicalOrder)
}

func TestEvaluationViewRepeatedInvalidCandidateAllocationScaling(t *testing.T) {
	_, base, _ := invalidCandidateFixture(t)
	var previous uint64
	for _, count := range []int{128, 256, 512, 1024, 2048} {
		facts := make([]EvaluatedFact, count)
		for index := range facts {
			facts[index] = EvaluatedFact{
				CandidateID:           "",
				CandidateRevision:     "1",
				Freshness:             FreshnessFresh,
				EffectiveAvailability: AvailabilityAvailable,
			}
		}
		view := base
		view.Facts = facts
		digest, err := view.computedDigestUnchecked()
		if err != nil {
			t.Fatal(err)
		}
		view.EvaluationDigest = digest

		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		err = view.Validate()
		runtime.ReadMemStats(&after)
		requireID(t, err, InvalidIdentifier)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("invalid_candidate_ids=%d allocated=%d", count, allocated)
		if allocated > 32<<20 {
			t.Errorf("bounded typed view allocated %d bytes, limit 32 MiB", allocated)
		}
		if previous != 0 && allocated > 3*previous+(512<<10) {
			t.Errorf("doubling input grew allocation from %d to %d", previous, allocated)
		}
		previous = allocated
	}
}
