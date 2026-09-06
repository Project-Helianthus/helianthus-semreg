package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStorageFixturesAreAcceptedByteExactContracts(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture path")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1")
	for _, tc := range []struct{ name, want string }{{"storage-bms-pack-vectors.json", "f78906f336973c5fbe44bbf40d9ad9715c043b3c6421cf2d7ec0a5ce68f79146"}, {"storage-bms-contract-tables.json", "3bde31cbc59ff3e2ea19408f3bfb09210837e7df20a692593e5c261bc48b032f"}} {
		raw, err := os.ReadFile(filepath.Join(root, tc.name))
		if err != nil {
			t.Fatal(err)
		}
		got := sha256.Sum256(raw)
		if hex.EncodeToString(got[:]) != tc.want {
			t.Fatalf("%s digest %x", tc.name, got)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "storage-bms-contract-tables.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tables map[string]any
	if err := json.Unmarshal(raw, &tables); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int{"field_specs": 20, "services": 7, "capabilities": 5, "relationships": 6, "operations": 2, "effects": 2, "portal_contributions": 5} {
		if got := len(tables[key].([]any)); got != want {
			t.Fatalf("%s=%d", key, got)
		}
	}
}
