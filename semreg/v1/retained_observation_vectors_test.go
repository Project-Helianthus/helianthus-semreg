package semreg

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRetainedObservationAcceptedVectors pins the merged docs-semantic #49 fixture.
// Typed admission and lifecycle tests exercise the executable scenarios.
func TestRetainedObservationAcceptedVectors(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "v1", "retained-observation-acceptance.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	wantDigest := [sha256.Size]byte{9, 5, 56, 191, 192, 1, 179, 153, 106, 186, 24, 112, 168, 146, 189, 94, 87, 178, 16, 29, 156, 235, 49, 243, 4, 29, 189, 37, 128, 201, 255, 225}
	if sum != wantDigest {
		t.Fatal("retained fixture integrity mismatch")
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
	if fixture.Contract != "helianthus.semantic.retained-observation.acceptance/v1" || fixture.Kernel != string(ContractKernelV1) || fixture.Retained != string(ContractRetainedObservationV1) || fixture.Counts.Positive != 21 || fixture.Counts.Negative != 20 || fixture.Counts.Total != 41 || len(fixture.Scenarios) != 41 {
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
		{"RO-CAP-boundary", "positive", "bounded_admission"},
		{"RO-CAP-excess", "negative", "bounded_admission"},
		{"RO-CAP-first_twenty", "positive", "bounded_admission"},
		{"RO-CAP-second_twenty", "positive", "bounded_admission"},
		{"RO-CAP-second_fence", "positive", "bounded_admission"},
		{"RO-CAP-third_twenty", "negative", "bounded_admission"},
		{"RO-CAP-refresh", "positive", "bounded_admission"},
		{"RO-CAP-inferred", "positive", "bounded_admission"},
		{"RO-CAP-mixed_paths", "positive", "bounded_admission"},
		{"RO-CAP-expiry", "positive", "bounded_admission"},
		{"RO-CAP-unknown_clock", "negative", "bounded_admission"},
		{"RO-CAP-expired_current_reserved", "negative", "bounded_admission"},
		{"RO-CAP-legacy_import", "positive", "bounded_admission"},
		{"RO-CAP-legacy_refusal", "negative", "bounded_admission"},
		{"RO-CAP-legacy_expiry_recovery", "positive", "bounded_admission"},
		{"RO-CAP-replay", "positive", "bounded_admission"},
		{"RO-CAP-precedence", "negative", "bounded_admission"},
		{"RO-CAP-fence_boundary", "positive", "bounded_admission"},
		{"RO-CAP-fence_excess", "negative", "bounded_admission"},
		{"RO-CAP-invalid_fence_restore", "negative", "structural_restore"},
		{"RO-CAP-invalid_empty_fence_restore", "negative", "structural_restore"},
		{"RO-CAP-empty_generation_fence_reservation", "negative", "bounded_admission"},
	}
	for i, scenario := range fixture.Scenarios {
		if scenario.ID != want[i].id || scenario.Polarity != want[i].polarity || scenario.Operation != want[i].operation {
			t.Fatalf("scenario %d: got %+v, want %+v", i, scenario, want[i])
		}
	}
}
