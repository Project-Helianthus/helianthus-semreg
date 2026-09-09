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

// TestRetainedObservationAcceptedVectors pins the public docs-semantic #23
// fixture. The focused lifecycle tests execute its positive/negative semantic
// postconditions through the typed publication/evaluation entry points.
func TestRetainedObservationAcceptedVectors(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "v1", "retained-observation-acceptance.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != "09d9cd244c9056185892e598e4c86d0e78ced14c5a8f257b0c3fdd1789b9aa6b" {
		t.Fatalf("fixture digest %s", got)
	}
	var fixture struct {
		Contract  string `json:"contract"`
		Kernel    string `json:"kernel_contract"`
		Retained  string `json:"retained_contract"`
		Scenarios []struct {
			ID string `json:"id"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Contract != "helianthus.semantic.retained-observation.acceptance/v1" || fixture.Kernel != string(ContractKernelV1) || fixture.Retained != string(ContractRetainedObservationV1) || len(fixture.Scenarios) != 12 {
		t.Fatalf("fixture contract: %+v", fixture)
	}
	for _, scenario := range fixture.Scenarios {
		if scenario.ID == "" {
			t.Fatal("empty retained vector id")
		}
	}
}
