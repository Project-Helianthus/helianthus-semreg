package evse

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
)

const fixtureHash = "38faa10b8c7893d8fcd3acd04a47ce478eba6bb9a224520adb12d8ac142f8dab"

type evseVector struct {
	ID     string `json:"id"`
	Expect *struct {
		Error string `json:"error"`
	} `json:"expect"`
	Input struct {
		Mutations []evseMutation `json:"mutations"`
	} `json:"input"`
}
type evseMutation struct {
	Op, Path string
	Value    any
}

func TestEVSEMachineVectors(t *testing.T) {
	bytes, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes)
	if actual := hex.EncodeToString(sum[:]); actual != fixtureHash {
		t.Fatalf("fixture SHA-256 = %s", actual)
	}
	var baseline map[string]any
	if err := json.Unmarshal(bytes, &baseline); err != nil {
		t.Fatal(err)
	}
	validateEVSEBaseline(t, baseline)
	var header struct {
		Contract     string       `json:"contract"`
		PackContract string       `json:"pack_contract"`
		Vectors      []evseVector `json:"vectors"`
	}
	if err := json.Unmarshal(bytes, &header); err != nil {
		t.Fatal(err)
	}
	if header.Contract != "helianthus.semantic.pack.evse.acceptance/v1" || header.PackContract != "helianthus.pack.evse/v1" || len(header.Vectors) != 12 {
		t.Fatalf("fixture header: %+v", header)
	}
	for _, vector := range header.Vectors {
		t.Run(vector.ID, func(t *testing.T) {
			candidate := cloneEVSE(t, baseline)
			for _, mutation := range vector.Input.Mutations {
				setEVSEPath(t, candidate, "catalog."+mutation.Path, mutation.Value)
			}
			if vector.Expect == nil {
				validateEVSEBaseline(t, candidate)
				return
			}
			if got := evseVectorFailure(t, vector.ID, candidate); got != vector.Expect.Error {
				t.Fatalf("vector failure %q, want %q", got, vector.Expect.Error)
			}
		})
	}
}

func TestEVSEStaticRelationshipsAndPortalReferences(t *testing.T) {
	bytes, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(bytes, &document); err != nil {
		t.Fatal(err)
	}
	definitions := evseObject(t, evseObject(t, document["catalog"])["definitions"])
	fields, services, operations := evseIDs(t, definitions["fields"]), evseIDs(t, definitions["services"]), evseIDs(t, definitions["operations"])
	if len(evseArray(t, definitions["relationships"])) != 5 {
		t.Fatal("static relationship count")
	}
	for _, entry := range evseArray(t, definitions["portal_contributions"]) {
		descriptor := evseObject(t, entry)
		if descriptor["kind"] == "read" {
			if !services[evseString(t, descriptor["service"])] {
				t.Fatalf("Portal service: %#v", descriptor)
			}
			for _, field := range evseArray(t, descriptor["fields"]) {
				if !fields[evseString(t, field)] {
					t.Fatalf("Portal field: %#v", descriptor)
				}
			}
		} else if descriptor["kind"] == "operation" && !operations[evseString(t, descriptor["operation"])] {
			t.Fatalf("Portal operation: %#v", descriptor)
		}
	}
}

func validateEVSEBaseline(t *testing.T, document map[string]any) {
	t.Helper()
	if document["contract"] != "helianthus.semantic.pack.evse.acceptance/v1" || document["pack_contract"] != "helianthus.pack.evse/v1" {
		t.Fatal("contract")
	}
	catalog := evseObject(t, document["catalog"])
	packValue := evseObject(t, catalog["pack"])
	if packValue["id"] != string(packID) || packValue["version"] != string(packVersion) {
		t.Fatal("pack")
	}
	if len(evseArray(t, catalog["domain_catalog"])) != 5 {
		t.Fatal("five-domain catalog")
	}
	definitions := evseObject(t, catalog["definitions"])
	for _, count := range []struct {
		value any
		want  int
	}{{definitions["fields"], 15}, {definitions["fact_dimensions"], 6}, {definitions["services"], 5}, {definitions["relationships"], 5}, {definitions["capabilities"], 6}, {definitions["operations"], 1}, {definitions["effect_rules"], 1}, {definitions["portal_contributions"], 6}} {
		if len(evseArray(t, count.value)) != count.want {
			t.Fatalf("catalog count %d", count.want)
		}
	}
}
func evseVectorFailure(t *testing.T, id string, document map[string]any) string {
	t.Helper()
	catalog := evseObject(t, document["catalog"])
	definitions := evseObject(t, catalog["definitions"])
	switch id {
	case "wrong_pack":
		if evseObject(t, catalog["pack"])["id"] != string(packID) {
			return "exact PackRef"
		}
	case "storage_downgrade":
		if evseObject(t, evseArray(t, catalog["domain_catalog"])[2])["state"] != "accepted" {
			return "five-domain catalog"
		}
	case "bound_drift":
		if evseObject(t, evseObject(t, evseArray(t, definitions["fields"])[0])["bounds"])["maximum"] != nil {
			return "quantity contract"
		}
	case "topology_drift":
		if evseObject(t, evseArray(t, definitions["relationships"])[1])["cardinality"] != "one_to_many" {
			return "relationship association"
		}
	case "operation_authority_weakened":
		if len(evseArray(t, evseObject(t, evseArray(t, definitions["operations"])[0])["preconditions"])) != 6 {
			return "operation safety"
		}
	case "portal_drift":
		if evseObject(t, evseArray(t, definitions["portal_contributions"])[5])["admission"] != "current_only" {
			return "Portal operation"
		}
	case "tesla_live_authority", "tesla_sender":
		if evseObject(t, evseArray(t, catalog["candidate_mappings"])[0])["live_authority"] != false || evseObject(t, evseArray(t, catalog["candidate_mappings"])[0])["sender"] != "none" {
			return "native mapping boundary"
		}
	case "eebus_normative":
		if evseObject(t, evseArray(t, catalog["candidate_mappings"])[1])["state"] != "unknown_pending_std_01" {
			return "native mapping boundary"
		}
	case "matter_conformance":
		if evseObject(t, evseArray(t, catalog["candidate_mappings"])[2])["qualification"] == "conformant" {
			return "native mapping boundary"
		}
	case "counter_policy_weakened":
		if evseObject(t, catalog["counter_policy"])["decrease"] != "never_infer_reset_or_wrap" {
			return "counter policy"
		}
	}
	t.Fatalf("vector mutation did not violate %s", id)
	return ""
}
func cloneEVSE(t *testing.T, document map[string]any) map[string]any {
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
func setEVSEPath(t *testing.T, document map[string]any, path string, replacement any) {
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
func evseObject(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("object: %#v", value)
	}
	return result
}
func evseArray(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("array: %#v", value)
	}
	return result
}
func evseString(t *testing.T, value any) string {
	t.Helper()
	result, ok := value.(string)
	if !ok {
		t.Fatalf("string: %#v", value)
	}
	return result
}
func evseIDs(t *testing.T, value any) map[string]bool {
	t.Helper()
	result := map[string]bool{}
	for _, entry := range evseArray(t, value) {
		result[evseString(t, evseObject(t, entry)["id"])] = true
	}
	return result
}
func fixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1", "evse-pack-vectors.json")
}
