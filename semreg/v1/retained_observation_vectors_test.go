package semreg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRetainedObservationAcceptedVectors pins the public docs-semantic #24
// sequential-lifecycle fixture. The focused lifecycle and operation tests
// execute its positive/negative semantic postconditions through typed entry
// points; this loader fails if the accepted inventory or ordering drifts.
func TestRetainedObservationAcceptedVectors(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "v1", "retained-observation-acceptance.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != "f98d57912a3a2f08291a80e6a65d8ecdd66a1037b7a580d3ec43b3ef1172ef73" {
		t.Fatalf("fixture digest %s", got)
	}
	var fixture struct {
		Contract string `json:"contract"`
		Kernel   string `json:"kernel_contract"`
		Retained string `json:"retained_contract"`
		Counts   struct {
			Positive int `json:"positive"`
			Negative int `json:"negative"`
			Total    int `json:"total"`
		} `json:"scenario_counts"`
		Scenarios []struct {
			ID        string `json:"id"`
			Polarity  string `json:"polarity"`
			Operation string `json:"operation"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Contract != "helianthus.semantic.retained-observation.acceptance/v1" || fixture.Kernel != string(ContractKernelV1) || fixture.Retained != string(ContractRetainedObservationV1) || fixture.Counts.Positive != 9 || fixture.Counts.Negative != 10 || fixture.Counts.Total != 19 || len(fixture.Scenarios) != 19 {
		t.Fatalf("fixture contract: %+v", fixture)
	}
	want := []struct {
		id, polarity, operation string
	}{
		{"RO-POS-001", "positive", "generation_fence"},
		{"RO-POS-002", "positive", "source_retirement"},
		{"RO-POS-003", "positive", "same_id_replacement"},
		{"RO-POS-004", "positive", "withdraw_retained_id"},
		{"RO-POS-005", "positive", "evaluate_after_original_deadline"},
		{"RO-POS-006", "positive", "successive_fences"},
		{"RO-POS-007", "positive", "fence_then_source_retirement"},
		{"RO-POS-008", "positive", "multiple_fences_then_source_retirement"},
		{"RO-POS-009", "positive", "replay_source_retirement"},
		{"RO-NEG-001", "negative", "validate_retained_copy"},
		{"RO-NEG-002", "negative", "select_retained"},
		{"RO-NEG-003", "negative", "admit_or_confirm_from_retained"},
		{"RO-NEG-004", "negative", "incomplete_fence_transition"},
		{"RO-NEG-005", "negative", "validate_retained_tombstone_path"},
		{"RO-NEG-006", "negative", "validate_retained_tombstone_path"},
		{"RO-NEG-007", "negative", "retire_foreign_source"},
		{"RO-NEG-008", "negative", "retire_foreign_epoch"},
		{"RO-NEG-009", "negative", "retire_foreign_generation"},
		{"RO-NEG-010", "negative", "retire_already_retired_epoch"},
	}
	for i, scenario := range fixture.Scenarios {
		if scenario.ID != want[i].id || scenario.Polarity != want[i].polarity || scenario.Operation != want[i].operation {
			t.Fatalf("scenario %d: got %+v, want %+v", i, scenario, want[i])
		}
	}
}
