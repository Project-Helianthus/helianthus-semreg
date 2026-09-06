package thermal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const fixtureSHA256 = "6fc20eaa6965e2073c38fba943b401e0b10673c16cef97286575dca083431576"

type machineVector struct {
	ID     string `json:"id"`
	Expect *struct {
		Error string `json:"error"`
	} `json:"expect"`
	Input struct {
		Mutations []mutation `json:"mutations"`
	} `json:"input"`
}
type mutation struct {
	Op, Path string
	Value    any    `json:"value"`
	Index    int    `json:"index"`
	ID       string `json:"id"`
}

func TestThermalMachineFixtureVectors(t *testing.T) {
	bytes, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes)
	if actual := hex.EncodeToString(sum[:]); actual != fixtureSHA256 {
		t.Fatalf("fixture SHA-256 = %s", actual)
	}
	var baseline map[string]any
	if err := json.Unmarshal(bytes, &baseline); err != nil {
		t.Fatal(err)
	}
	validateBaseline(t, baseline)
	var header struct {
		Contract     string          `json:"contract"`
		PackContract string          `json:"pack_contract"`
		Vectors      []machineVector `json:"vectors"`
	}
	if err := json.Unmarshal(bytes, &header); err != nil {
		t.Fatal(err)
	}
	if header.Contract != "helianthus.semantic.pack.thermal.acceptance/v1" || header.PackContract != "helianthus.pack.thermal/v1" || len(header.Vectors) != 12 {
		t.Fatalf("fixture header: %+v", header)
	}
	for _, vector := range header.Vectors {
		t.Run(vector.ID, func(t *testing.T) {
			candidate := cloneDocument(t, baseline)
			for _, change := range vector.Input.Mutations {
				applyMutation(t, candidate, change)
			}
			if vector.Expect == nil {
				validateBaseline(t, candidate)
				return
			}
			if got := vectorFailure(t, vector.ID, candidate); got != vector.Expect.Error {
				t.Fatalf("vector failure = %q, want %q", got, vector.Expect.Error)
			}
		})
	}
}

func TestStaticThermalPortalReferences(t *testing.T) {
	var document map[string]any
	bytes, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bytes, &document); err != nil {
		t.Fatal(err)
	}
	definitions := object(t, object(t, document["catalog"])["definitions"])
	fields, services, operations := identifiers(t, definitions["fields"]), identifiers(t, definitions["services"]), identifiers(t, definitions["operations"])
	for _, entry := range array(t, definitions["portal_contributions"]) {
		descriptor := object(t, entry)
		if descriptor["kind"] == "read" {
			if !services[stringValue(t, descriptor["service"])] {
				t.Fatalf("Portal service: %#v", descriptor)
			}
			for _, field := range array(t, descriptor["fields"]) {
				if !fields[stringValue(t, field)] {
					t.Fatalf("Portal field: %#v", descriptor)
				}
			}
		} else if descriptor["kind"] == "operation" && !operations[stringValue(t, descriptor["operation"])] {
			t.Fatalf("Portal operation: %#v", descriptor)
		}
	}
}

func validateBaseline(t *testing.T, document map[string]any) {
	t.Helper()
	if document["contract"] != "helianthus.semantic.pack.thermal.acceptance/v1" || document["pack_contract"] != "helianthus.pack.thermal/v1" {
		t.Fatal("contract")
	}
	catalog := object(t, document["catalog"])
	packValue := object(t, catalog["pack"])
	if packValue["id"] != string(packID) || packValue["version"] != string(packVersion) {
		t.Fatal("pack")
	}
	if len(array(t, catalog["domain_catalog"])) != 5 {
		t.Fatal("five-domain catalog")
	}
	definitions := object(t, catalog["definitions"])
	for _, count := range []struct {
		value any
		want  int
	}{{definitions["fields"], 13}, {definitions["fact_dimensions"], 5}, {definitions["services"], 5}, {definitions["capabilities"], 7}, {definitions["operations"], 2}, {definitions["effect_rules"], 2}, {definitions["portal_contributions"], 7}} {
		if len(array(t, count.value)) != count.want {
			t.Fatalf("catalog count %d", count.want)
		}
	}
	if relationships, present := definitions["relationships"]; present && len(array(t, relationships)) != 0 {
		t.Fatal("relationships")
	}
}

func vectorFailure(t *testing.T, id string, document map[string]any) string {
	t.Helper()
	catalog := object(t, document["catalog"])
	definitions := object(t, catalog["definitions"])
	fields := array(t, definitions["fields"])
	switch id {
	case "TH-NEG-001":
		if object(t, fields[0])["kind"] != "quantity" {
			return "non-kernel ValueKind"
		}
	case "TH-NEG-002":
		if object(t, fields[0])["unit"] != "unit.celsius" {
			return "invalid unit ID"
		}
	case "TH-NEG-003":
		if _, ok := object(t, fields[0])["ref"]; !ok {
			return "DefinitionRef owner/version"
		}
	case "TH-NEG-004":
		if array(t, object(t, fields[10])["symbols"])[1] != "thermal.status.operation.idle" {
			return "invalid semantic symbols"
		}
	case "TH-NEG-005":
		if array(t, object(t, fields[12])["symbols"])[0] != "thermal.action.state.cooling" {
			return "field has invalid semantic symbols"
		}
	case "TH-NEG-006":
		if _, ok := object(t, array(t, definitions["services"])[0])["fact_key_dimension"]; !ok {
			return "dimension contract"
		}
	case "TH-NEG-007":
		if object(t, array(t, definitions["portal_contributions"])[0])["service"] != "thermal.service.system" {
			return "Portal dangling reference"
		}
	case "TH-NEG-008":
		if len(fields) == 14 {
			return "duplicate definition ID"
		}
	case "TH-NEG-009":
		if len(array(t, catalog["domain_catalog"])) == 4 {
			return "five-domain catalog"
		}
	case "TH-NEG-010":
		if object(t, array(t, catalog["candidate_mappings"])[2])["state"] != "unknown_pending_std_01" {
			return "eeBUS mapping"
		}
	case "TH-NEG-011":
		if array(t, object(t, array(t, definitions["operations"])[0])["terminal_outcomes"])[1] != "failed_no_contact" {
			return "operation stages/outcomes are incomplete"
		}
	}
	t.Fatalf("vector mutation did not violate %s", id)
	return ""
}

func applyMutation(t *testing.T, document map[string]any, change mutation) {
	t.Helper()
	path := "catalog." + change.Path
	switch change.Op {
	case "set":
		setPath(t, document, path, change.Value)
	case "delete":
		deletePath(t, document, path)
	case "append_copy":
		values := arrayAt(t, document, path)
		values = append(values, values[change.Index])
		setPath(t, document, path, values)
	case "remove_id":
		values := arrayAt(t, document, path)
		kept := make([]any, 0, len(values))
		for _, value := range values {
			if object(t, value)["id"] != change.ID {
				kept = append(kept, value)
			}
		}
		setPath(t, document, path, kept)
	default:
		t.Fatalf("mutation %s", change.Op)
	}
}
func cloneDocument(t *testing.T, document map[string]any) map[string]any {
	t.Helper()
	bytes, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(bytes, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}
func setPath(t *testing.T, document map[string]any, path string, value any) {
	t.Helper()
	parent, last := pathParent(t, document, path)
	switch values := parent.(type) {
	case map[string]any:
		values[last] = value
	case []any:
		values[parseIndex(t, last)] = value
	default:
		t.Fatal(path)
	}
}
func deletePath(t *testing.T, document map[string]any, path string) {
	t.Helper()
	parent, last := pathParent(t, document, path)
	values, ok := parent.(map[string]any)
	if !ok {
		t.Fatal(path)
	}
	delete(values, last)
}
func pathParent(t *testing.T, document map[string]any, path string) (any, string) {
	t.Helper()
	parts := split(path)
	var current any = document
	for _, part := range parts[:len(parts)-1] {
		switch values := current.(type) {
		case map[string]any:
			current = values[part]
		case []any:
			current = values[parseIndex(t, part)]
		default:
			t.Fatal(path)
		}
	}
	return current, parts[len(parts)-1]
}
func split(path string) []string {
	result := []string{}
	current := ""
	for _, r := range path {
		if r == '.' {
			result = append(result, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	return append(result, current)
}
func parseIndex(t *testing.T, value string) int {
	t.Helper()
	var result int
	if _, err := fmtSscan(value, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func fmtSscan(value string, result *int) (int, error) {
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, os.ErrInvalid
		}
		*result = *result*10 + int(r-'0')
	}
	return 1, nil
}
func arrayAt(t *testing.T, document map[string]any, path string) []any {
	t.Helper()
	parent, last := pathParent(t, document, path)
	return array(t, object(t, parent)[last])
}
func object(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("object: %#v", value)
	}
	return result
}
func array(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("array: %#v", value)
	}
	return result
}
func stringValue(t *testing.T, value any) string {
	t.Helper()
	result, ok := value.(string)
	if !ok {
		t.Fatalf("string: %#v", value)
	}
	return result
}
func identifiers(t *testing.T, value any) map[string]bool {
	t.Helper()
	result := map[string]bool{}
	for _, entry := range array(t, value) {
		result[stringValue(t, object(t, entry)["id"])] = true
	}
	return result
}
func fixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1", "thermal-hvac-pack-vectors.json")
}
