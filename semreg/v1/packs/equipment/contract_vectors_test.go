package equipment

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
		if spec.kind == semreg.ValueQuantity && field["unit"] != string(spec.unit) {
			t.Fatalf("field %s unit", id)
		}
	}
	index := index()
	if err := index.Validate(); err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(bytes) != equipmentFixtureSHA256 {
		t.Fatal("equipment fixture digest")
	}
}

// equipmentFixtureSHA256 is the SHA-256 of fixtures/v1/equipment-pack-vectors.json.
var equipmentFixtureSHA256 = [sha256.Size]byte{
	0x37, 0x28, 0x62, 0x91, 0x60, 0x0e, 0x4d, 0xd6,
	0x5e, 0x37, 0x4d, 0x41, 0xd7, 0x6b, 0x44, 0x2e,
	0x88, 0x32, 0x2d, 0x8e, 0x45, 0xfa, 0xf8, 0xb5,
	0xe9, 0xc5, 0x7f, 0x5f, 0x0a, 0x0a, 0xd3, 0x54,
}

func unionFixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1", "equipment-pack-vectors.json")
}
