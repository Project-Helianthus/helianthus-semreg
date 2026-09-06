package infrastructure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

const infrastructureFixtureSHA256 = "5ccf84e4727ec27f7234d1cd4a6a72bc1c5fc60dd5aa755f42f3a9a0e5cf5b6e"

type machineVector struct {
	ID    string `json:"id"`
	Input struct {
		Mutations []machineMutation `json:"mutations"`
	} `json:"input"`
	Expect struct {
		Error string `json:"error"`
	} `json:"expect"`
}

type machineMutation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

func TestInfrastructureMachineContractVectors(t *testing.T) {
	bytes, err := os.ReadFile(infrastructureFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	actual := sha256.Sum256(bytes)
	if got := hex.EncodeToString(actual[:]); got != infrastructureFixtureSHA256 {
		t.Fatalf("fixture SHA-256 = %s, want %s", got, infrastructureFixtureSHA256)
	}
	var document map[string]any
	if err := json.Unmarshal(bytes, &document); err != nil {
		t.Fatal(err)
	}
	if err := validateMachineCatalog(document); err != nil {
		t.Fatalf("baseline catalog: %v", err)
	}
	var header struct {
		Contract     string          `json:"contract"`
		PackContract string          `json:"pack_contract"`
		Vectors      []machineVector `json:"vectors"`
	}
	if err := json.Unmarshal(bytes, &header); err != nil {
		t.Fatal(err)
	}
	if header.Contract != "helianthus.semantic.pack.infrastructure.acceptance/v1" || header.PackContract != "helianthus.pack.infrastructure/v1" || len(header.Vectors) != 10 {
		t.Fatalf("unexpected machine fixture header: %+v", header)
	}
	for _, vector := range header.Vectors {
		t.Run(vector.ID, func(t *testing.T) {
			candidate := cloneDocument(t, document)
			for _, mutation := range vector.Input.Mutations {
				if mutation.Op != "set" {
					t.Fatalf("unsupported fixture operation %q", mutation.Op)
				}
				if err := setPath(candidate, "catalog."+mutation.Path, mutation.Value); err != nil {
					t.Fatal(err)
				}
			}
			err := validateMachineCatalog(candidate)
			if vector.Expect.Error == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || err.Error() != vector.Expect.Error {
				t.Fatalf("validation error = %v, want %q", err, vector.Expect.Error)
			}
		})
	}
}

func TestStaticRelationshipAndPortalMetadata(t *testing.T) {
	document := loadMachineDocument(t)
	catalog := mapValue(t, document["catalog"])
	definitions := mapValue(t, catalog["definitions"])
	relationships := listValue(t, definitions["relationships"])
	if len(relationships) != 5 {
		t.Fatalf("relationship count = %d", len(relationships))
	}
	wantRelationships := map[string][3]string{
		"infrastructure.relationship.site_grid_connection":   {"infrastructure.dimension.site", "infrastructure.dimension.grid_connection", "one_to_many"},
		"infrastructure.relationship.grid_connection_feeder": {"infrastructure.dimension.grid_connection", "infrastructure.dimension.feeder", "one_to_many"},
		"infrastructure.relationship.feeder_circuit":         {"infrastructure.dimension.feeder", "infrastructure.dimension.circuit", "one_to_many"},
		"infrastructure.relationship.circuit_phase":          {"infrastructure.dimension.circuit", "infrastructure.dimension.phase", "one_to_many"},
		"infrastructure.relationship.grid_connection_meter":  {"infrastructure.dimension.grid_connection", "infrastructure.dimension.meter", "one_to_many"},
	}
	for _, item := range relationships {
		relationship := mapValue(t, item)
		id := stringValue(t, relationship["id"])
		want, ok := wantRelationships[id]
		if !ok || stringValue(t, relationship["from"]) != want[0] || stringValue(t, relationship["to"]) != want[1] || stringValue(t, relationship["cardinality"]) != want[2] {
			t.Fatalf("unexpected relationship: %#v", relationship)
		}
		delete(wantRelationships, id)
	}
	if len(wantRelationships) != 0 {
		t.Fatalf("missing relationships: %#v", wantRelationships)
	}
	portal := listValue(t, definitions["portal_contributions"])
	if len(portal) != 6 {
		t.Fatalf("portal read descriptor count = %d", len(portal))
	}
	wantPortal := map[string]struct {
		service string
		fields  []string
	}{
		"infrastructure.portal.read.site":            {"infrastructure.service.site", []string{"infrastructure.status.fault"}},
		"infrastructure.portal.read.grid_connection": {"infrastructure.service.grid_connection", []string{"infrastructure.ac.frequency", "infrastructure.ac.power_factor", "infrastructure.power.apparent", "infrastructure.power.export_active", "infrastructure.power.export_reactive", "infrastructure.power.import_active", "infrastructure.power.import_reactive", "infrastructure.status.connection", "infrastructure.status.readiness"}},
		"infrastructure.portal.read.feeder":          {"infrastructure.service.feeder", []string{}},
		"infrastructure.portal.read.circuit":         {"infrastructure.service.circuit", []string{"infrastructure.status.breaker", "infrastructure.status.interlock"}},
		"infrastructure.portal.read.phase":           {"infrastructure.service.phase", []string{"infrastructure.ac.active_power", "infrastructure.ac.apparent_power", "infrastructure.ac.current", "infrastructure.ac.reactive_power", "infrastructure.ac.voltage"}},
		"infrastructure.portal.read.meter":           {"infrastructure.service.meter", []string{"infrastructure.energy.export", "infrastructure.energy.import"}},
	}
	for _, item := range portal {
		descriptor := mapValue(t, item)
		id, service := stringValue(t, descriptor["id"]), stringValue(t, descriptor["service"])
		want, ok := wantPortal[id]
		if !ok || stringValue(t, descriptor["kind"]) != "read" || service != want.service || !sameStrings(descriptor["fields"], want.fields) {
			t.Fatalf("unexpected Portal read descriptor: %#v", descriptor)
		}
		delete(wantPortal, id)
	}
	if len(wantPortal) != 0 {
		t.Fatalf("missing Portal read descriptors: %#v", wantPortal)
	}
}

func validateMachineCatalog(document map[string]any) error {
	catalog, ok := document["catalog"].(map[string]any)
	if !ok || document["contract"] != "helianthus.semantic.pack.infrastructure.acceptance/v1" || document["pack_contract"] != "helianthus.pack.infrastructure/v1" {
		return fmt.Errorf("exact PackRef")
	}
	pack, ok := catalog["pack"].(map[string]any)
	if !ok || pack["id"] != string(packID) || pack["version"] != string(packVersion) {
		return fmt.Errorf("exact PackRef")
	}
	domains, ok := catalog["domain_catalog"].([]any)
	if !ok || len(domains) != 5 {
		return fmt.Errorf("five-domain catalog")
	}
	for _, item := range domains {
		domain, ok := item.(map[string]any)
		if !ok || domain["state"] != "accepted" {
			return fmt.Errorf("five-domain catalog")
		}
	}
	definitions, ok := catalog["definitions"].(map[string]any)
	if !ok || !exactFields(definitions) || !exactDimensions(definitions) || !exactServicesAndCapabilities(definitions) {
		return fmt.Errorf("quantity contract")
	}
	if !exactRelationships(definitions) {
		return fmt.Errorf("relationship association")
	}
	boundary, ok := catalog["read_only_operation_boundary"].(map[string]any)
	if !ok || boundary["breaker_actuation"] != "not_published" || !emptyList(definitions["operations"]) || !emptyList(definitions["effect_rules"]) {
		return fmt.Errorf("read-only operation boundary")
	}
	mappings, ok := catalog["candidate_mappings"].([]any)
	if !ok || len(mappings) != 4 || mapValueNoPanic(mappings[0])["live_authority"] != false || mapValueNoPanic(mappings[2])["state"] != "unknown_pending_std_01" || mapValueNoPanic(mappings[3])["qualification"] != "not_conformance" {
		return fmt.Errorf("native mapping boundary")
	}
	counter, ok := catalog["counter_policy"].(map[string]any)
	if !ok || counter["decrease"] != "never_infer_reset_or_wrap" {
		return fmt.Errorf("counter policy")
	}
	return nil
}

func exactFields(definitions map[string]any) bool {
	entries, ok := definitions["fields"].([]any)
	if !ok || len(entries) != len(fields) {
		return false
	}
	byID := make(map[string]map[string]any, len(entries))
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			return false
		}
		byID[fmt.Sprint(item["id"])] = item
	}
	for _, spec := range fields {
		item, ok := byID[string(spec.id)]
		if !ok || item["kind"] != string(spec.kind) || item["dimension"] != string(spec.dimension) {
			return false
		}
		if spec.kind == "quantity" {
			bounds, ok := item["bounds"].(map[string]any)
			if !ok || item["unit"] != string(spec.unit) || !sameDecimal(bounds["minimum"], spec.minimum.decimal()) || !sameDecimal(bounds["maximum"], spec.maximum.decimal()) {
				return false
			}
		} else if !sameStrings(item["symbols"], spec.symbols) {
			return false
		}
	}
	return true
}

func exactDimensions(definitions map[string]any) bool {
	entries, ok := definitions["fact_dimensions"].([]any)
	if !ok || len(entries) != len(dimensions) {
		return false
	}
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok || dimensions[semregDefinition(item["id"])] != semregValueKind(item["value_kind"]) {
			return false
		}
	}
	return true
}

func exactServicesAndCapabilities(definitions map[string]any) bool {
	servicesEntries, servicesOK := definitions["services"].([]any)
	capabilityEntries, capabilitiesOK := definitions["capabilities"].([]any)
	if !servicesOK || !capabilitiesOK || len(servicesEntries) != len(services) || len(capabilityEntries) != len(capabilities) {
		return false
	}
	for _, entry := range servicesEntries {
		item, ok := entry.(map[string]any)
		if !ok || services[semregDefinition(item["id"])] != semregDefinition(item["fact_key_dimension"]) {
			return false
		}
	}
	for _, entry := range capabilityEntries {
		item, ok := entry.(map[string]any)
		if !ok || capabilities[semregDefinition(item["id"])] != semregDefinition(item["service"]) || item["activation"] != "qualified_current_binding" {
			return false
		}
	}
	return true
}

func exactRelationships(definitions map[string]any) bool {
	entries, ok := definitions["relationships"].([]any)
	return ok && len(entries) == 5 && mapValueNoPanic(entries[2])["cardinality"] == "one_to_many"
}

func sameDecimal(value any, expected semreg.Decimal) bool {
	actual, ok := value.(map[string]any)
	return ok && actual["coefficient"] == expected.Coefficient && actual["exponent10"] == float64(expected.Exponent10)
}

func sameStrings(value any, expected []string) bool {
	actual, ok := value.([]any)
	if !ok || len(actual) != len(expected) {
		return false
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func emptyList(value any) bool { list, ok := value.([]any); return ok && len(list) == 0 }

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

func setPath(document map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	var current any = document
	for _, part := range parts[:len(parts)-1] {
		switch container := current.(type) {
		case map[string]any:
			current = container[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(container) {
				return fmt.Errorf("invalid fixture path %q", path)
			}
			current = container[index]
		default:
			return fmt.Errorf("invalid fixture path %q", path)
		}
	}
	last := parts[len(parts)-1]
	switch container := current.(type) {
	case map[string]any:
		container[last] = value
	case []any:
		index, err := strconv.Atoi(last)
		if err != nil || index < 0 || index >= len(container) {
			return fmt.Errorf("invalid fixture path %q", path)
		}
		container[index] = value
	default:
		return fmt.Errorf("invalid fixture path %q", path)
	}
	return nil
}

func loadMachineDocument(t *testing.T) map[string]any {
	t.Helper()
	bytes, err := os.ReadFile(infrastructureFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(bytes, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func infrastructureFixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve fixture path")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "v1", "infrastructure-pack-vectors.json")
}

func mapValue(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("map value: %#v", value)
	}
	return result
}

func mapValueNoPanic(value any) map[string]any { result, _ := value.(map[string]any); return result }

func listValue(t *testing.T, value any) []any {
	t.Helper()
	result, ok := value.([]any)
	if !ok {
		t.Fatalf("list value: %#v", value)
	}
	return result
}

func stringValue(t *testing.T, value any) string {
	t.Helper()
	result, ok := value.(string)
	if !ok {
		t.Fatalf("string value: %#v", value)
	}
	return result
}

func semregDefinition(value any) semreg.DefinitionID { return semreg.DefinitionID(fmt.Sprint(value)) }
func semregValueKind(value any) semreg.ValueKind     { return semreg.ValueKind(fmt.Sprint(value)) }
