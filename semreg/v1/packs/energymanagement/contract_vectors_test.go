package energymanagement

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

func TestCopiedVectorDefinitionsAreInThePack(t *testing.T) {
	var document map[string]any
	bytes, err := os.ReadFile(unionFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bytes, &document); err != nil {
		t.Fatal(err)
	}
	definitions := document["catalog"].(map[string]any)["definitions"].(map[string]any)
	for _, entry := range definitions["fields"].([]any) {
		field := entry.(map[string]any)
		id := semreg.DefinitionID(field["id"].(string))
		spec, ok := findField(id)
		if !ok {
			t.Fatalf("missing field %s", id)
		}
		if field["kind"] != string(spec.kind) || field["dimension"] != string(spec.dimension) {
			t.Fatalf("field %s shape", id)
		}
		unit, _ := field["unit"].(string)
		if spec.kind == semreg.ValueQuantity && unit != string(spec.unit) {
			t.Fatalf("field %s unit", id)
		}
	}
	index := index()
	if err := index.Validate(); err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(bytes) != energyManagementFixtureSHA256 {
		t.Fatal("energy management fixture digest")
	}
}

// energyManagementFixtureSHA256 is the SHA-256 of fixtures/v1/energy-management-pack-vectors.json.
var energyManagementFixtureSHA256 = [sha256.Size]byte{
	0x59, 0xea, 0xbc, 0xf0, 0xdb, 0xae, 0x3d, 0x78,
	0x81, 0xfa, 0x46, 0xf1, 0x5b, 0xb1, 0x6e, 0x89,
	0xb5, 0xb0, 0xea, 0xac, 0xc8, 0x1d, 0xb8, 0x27,
	0x10, 0x58, 0x38, 0x63, 0x60, 0x27, 0xbd, 0x07,
}

func unionFixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1", "energy-management-pack-vectors.json")
}
