package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

type storageVector struct {
	ID     string `json:"id"`
	Expect *struct {
		Error string `json:"error"`
	} `json:"expect"`
	Input struct {
		Mutations []storageMutation `json:"mutations"`
	} `json:"input"`
}

type storageMutation struct {
	Op, Path string
	Value    any
}

func TestStorageFixturesAreAcceptedByteExactContracts(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture path")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1")
	for _, tc := range []struct {
		name string
		want [sha256.Size]byte
	}{{"storage-bms-pack-vectors.json", storageVectorFixtureSHA256}, {"storage-bms-contract-tables.json", storageContractTablesSHA256}} {
		raw, err := os.ReadFile(filepath.Join(root, tc.name))
		if err != nil {
			t.Fatal(err)
		}
		if got := sha256.Sum256(raw); got != tc.want {
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
	for key, want := range map[string]int{"field_specs": 106, "services": 7, "capabilities": 20, "relations": 7, "operations": 17, "effects": 17, "portal_contributions": 5} {
		if got := len(tables[key].([]any)); got != want {
			t.Fatalf("%s=%d", key, got)
		}
	}
	for index, want := range storageRelations {
		relation := storageVectorObject(t, tables["relations"].([]any)[index])
		if len(relation) != 5 || relation["id"] != "storage.relation."+want.name || relation["from"] != want.from || relation["to"] != want.to || relation["cardinality"] != "one_to_many" || relation["order"] != float64(10*(index+1)) {
			t.Fatalf("relation %d: %#v", index, relation)
		}
	}
}

// storageVectorFixtureSHA256 and storageContractTablesSHA256 are the SHA-256
// values of fixtures/v1/storage-bms-pack-vectors.json and
// fixtures/v1/storage-bms-contract-tables.json.
var (
	storageVectorFixtureSHA256 = [sha256.Size]byte{
		0x74, 0x21, 0xd0, 0x2f, 0x6c, 0x50, 0xf3, 0xc6,
		0xfb, 0x7f, 0xbd, 0xaa, 0x35, 0xad, 0xf2, 0x5a,
		0x50, 0xea, 0x73, 0x21, 0x27, 0xa1, 0xc7, 0x35,
		0x86, 0xef, 0x6d, 0x96, 0x69, 0x26, 0x48, 0x96,
	}
	storageContractTablesSHA256 = [sha256.Size]byte{
		0x76, 0x4f, 0xa0, 0xf7, 0x57, 0xf3, 0xae, 0xa4,
		0x57, 0x51, 0xe0, 0x2f, 0xc6, 0xb6, 0xe2, 0x4e,
		0xd8, 0xa6, 0x66, 0x3f, 0x9f, 0x8d, 0xb8, 0xde,
		0x91, 0x4a, 0x39, 0xfe, 0xc7, 0xcb, 0x9c, 0x0e,
	}
)

// storageRelations lists the accepted storage relations in declared order.
var storageRelations = []struct{ name, from, to string }{
	{"system_battery", "storage.service.system", "storage.service.battery"},
	{"battery_pack", "storage.service.battery", "storage.service.pack"},
	{"pack_module", "storage.service.pack", "storage.service.module"},
	{"module_cell", "storage.service.module", "storage.service.cell"},
	{"pack_string", "storage.service.pack", "storage.service.string"},
	{"pack_interface", "storage.service.pack", "storage.service.interface"},
	{"interface_supply", "storage.service.interface", "storage.service.pack"},
}

func TestStorageMachineFixtureVectors(t *testing.T) {
	raw, err := os.ReadFile(storageVectorFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if sum != storageVectorFixtureSHA256 {
		t.Fatalf("fixture SHA-256 = %s", hex.EncodeToString(sum[:]))
	}
	var baseline map[string]any
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	validateStorageVectorBaseline(t, baseline)
	var header struct {
		Contract     string          `json:"contract"`
		PackContract string          `json:"pack_contract"`
		Vectors      []storageVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatal(err)
	}
	if header.Contract != "helianthus.semantic.pack.storage.acceptance/v1" || header.PackContract != "helianthus.pack.storage/v1" || len(header.Vectors) != 5 {
		t.Fatalf("fixture header: %+v", header)
	}
	for _, vector := range header.Vectors {
		t.Run(vector.ID, func(t *testing.T) {
			candidate := cloneStorageVectorDocument(t, baseline)
			for _, mutation := range vector.Input.Mutations {
				if mutation.Op != "set" {
					t.Fatalf("unsupported mutation %q", mutation.Op)
				}
				setStorageVectorPath(t, candidate, "catalog."+mutation.Path, mutation.Value)
			}
			if vector.Expect == nil {
				validateStorageVectorBaseline(t, candidate)
				return
			}
			if got := storageVectorFailure(t, vector.ID, candidate); got != vector.Expect.Error {
				t.Fatalf("vector failure %q, want %q", got, vector.Expect.Error)
			}
		})
	}
}

func validateStorageVectorBaseline(t *testing.T, document map[string]any) {
	t.Helper()
	if document["contract"] != "helianthus.semantic.pack.storage.acceptance/v1" || document["pack_contract"] != "helianthus.pack.storage/v1" {
		t.Fatal("contract")
	}
	catalog := storageVectorObject(t, document["catalog"])
	validator := New()
	if catalogPack := storageVectorObject(t, catalog["pack"]); catalogPack["id"] != string(validator.Pack().ID) || catalogPack["version"] != string(validator.Pack().Version) {
		t.Fatal("pack")
	}
	index := validator.Definitions()
	if err := index.Validate(); err != nil {
		t.Fatal(err)
	}
	definitions := storageVectorObject(t, catalog["definitions"])
	for _, count := range []struct {
		value any
		want  int
	}{{definitions["fields"], 106}, {definitions["dimensions"], 7}, {definitions["relations"], 7}, {catalog["domain_catalog"], 5}, {index.Fields, 106}, {index.Services, 7}, {index.Capabilities, 20}, {index.Operations, 17}, {index.EffectRules, 17}} {
		if storageVectorLen(t, count.value) != count.want {
			t.Fatalf("catalog count = %d, want %d", storageVectorLen(t, count.value), count.want)
		}
	}
	for index, name := range storageVectorArray(t, definitions["relations"]) {
		if name != storageRelations[index].name {
			t.Fatalf("relation %d = %#v", index, name)
		}
	}
	for _, fixtureField := range storageVectorArray(t, definitions["fields"]) {
		field := storageVectorObject(t, fixtureField)
		id := semreg.DefinitionID(storageVectorString(t, field["id"]))
		spec, found := findField(id)
		if !found || field["dimension"] != string(spec.dimension) {
			t.Fatalf("fixture field %q", id)
		}
		key, value := storageVectorFact(spec)
		if err := validator.ValidateFact(key, &value); err != nil {
			t.Fatalf("validator baseline %q: %v", id, err)
		}
	}
}

func storageVectorFailure(t *testing.T, id string, document map[string]any) string {
	t.Helper()
	catalog := storageVectorObject(t, document["catalog"])
	switch id {
	case "ST-NEG-001":
		pack := storageVectorObject(t, catalog["pack"])
		key, value := storageVectorFact(fields[0])
		key.PackID = semreg.DefinitionID(storageVectorString(t, pack["id"]))
		if semreg.ErrorIdentifier(New().ValidateFact(key, &value)) == semreg.DefinitionOwnerMissing {
			return "PackRef"
		}
	case "ST-NEG-002":
		mapping := storageVectorObject(t, storageVectorArray(t, catalog["candidate_mappings"])[0])
		if mapping["physical_qualified"] != false {
			return "mapping"
		}
	case "ST-NEG-003":
		if storageVectorObject(t, catalog["supersession"])["operation_use"] != "superseded_non_implementable" {
			return "supersession"
		}
	case "ST-NEG-004":
		fields := storageVectorArray(t, storageVectorObject(t, catalog["definitions"])["fields"])
		if storageVectorString(t, storageVectorArray(t, storageVectorObject(t, fields[105])["symbols"])[1]) != "storage.status.interlock.clear" {
			return "field catalog"
		}
	default:
		t.Fatalf("unexpected vector %q", id)
	}
	t.Fatalf("vector mutation did not violate %s", id)
	return ""
}

func storageVectorFact(spec fieldSpec) (semreg.FactKey, semreg.Value) {
	name := "vector-interface"
	key := testKey(spec)
	for i := range key.Dimensions {
		if key.Dimensions[i].ID == spec.dimension {
			key.Dimensions[i].Value.Text = &name
		}
	}
	switch spec.kind {
	case semreg.ValueQuantity:
		value := semreg.Decimal{Coefficient: "0"}
		if spec.minimum != nil {
			value = spec.minimum.decimal()
		}
		return key, semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: value, Unit: spec.unit}}
	case semreg.ValueBoolean:
		flag := false
		return key, semreg.Value{Kind: semreg.ValueBoolean, Boolean: &flag}
	case semreg.ValueText:
		text := "text"
		return key, semreg.Value{Kind: semreg.ValueText, Text: &text}
	case semreg.ValueSymbols:
		token := strings.TrimPrefix(spec.symbols[0], string(spec.id)+".")
		return key, semreg.Value{Kind: semreg.ValueSymbols, Symbols: []semreg.Symbol{{Namespace: spec.id, Token: token, Known: true}}}
	default:
		token := strings.TrimPrefix(spec.symbols[0], string(spec.id)+".")
		return key, semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: spec.id, Token: token, Known: true}}
	}
}

func cloneStorageVectorDocument(t *testing.T, document map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func setStorageVectorPath(t *testing.T, document map[string]any, path string, replacement any) {
	t.Helper()
	parts := strings.Split(path, ".")
	var current any = document
	for _, part := range parts[:len(parts)-1] {
		switch values := current.(type) {
		case map[string]any:
			current = values[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil {
				t.Fatal(err)
			}
			current = values[index]
		default:
			t.Fatal(path)
		}
	}
	last := parts[len(parts)-1]
	switch values := current.(type) {
	case map[string]any:
		values[last] = replacement
	case []any:
		index, err := strconv.Atoi(last)
		if err != nil {
			t.Fatal(err)
		}
		values[index] = replacement
	default:
		t.Fatal(path)
	}
}

func storageVectorObject(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("object: %#v", value)
	}
	return result
}

func storageVectorArray(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("array: %#v", value)
	}
	return result
}

func storageVectorString(t *testing.T, value any) string {
	t.Helper()
	result, ok := value.(string)
	if !ok {
		t.Fatalf("string: %#v", value)
	}
	return result
}

func storageVectorLen(t *testing.T, value any) int {
	t.Helper()
	switch values := value.(type) {
	case []any:
		return len(values)
	case []semreg.DefinitionRef:
		return len(values)
	default:
		t.Fatalf("countable: %#v", value)
		return 0
	}
}

func storageVectorFixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1", "storage-bms-pack-vectors.json")
}
