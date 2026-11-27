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

// infrastructureFixtureSHA256 is the SHA-256 of
// fixtures/v1/infrastructure-pack-vectors.json.
var infrastructureFixtureSHA256 = [sha256.Size]byte{
	0x94, 0x8d, 0x6a, 0x74, 0x73, 0x99, 0xdb, 0x19,
	0xcf, 0xdf, 0xf8, 0x26, 0x5a, 0x8f, 0x7c, 0xca,
	0x49, 0x31, 0x88, 0x8e, 0x03, 0x16, 0xde, 0x2f,
	0x98, 0xf6, 0xf9, 0x4c, 0x90, 0x89, 0xb3, 0x3a,
}

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
	if actual != infrastructureFixtureSHA256 {
		t.Fatalf("fixture SHA-256 = %s, want %s", hex.EncodeToString(actual[:]), hex.EncodeToString(infrastructureFixtureSHA256[:]))
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

func TestStaticRelationAndPortalMetadata(t *testing.T) {
	document := loadMachineDocument(t)
	catalog := mapValue(t, document["catalog"])
	definitions := mapValue(t, catalog["definitions"])
	relations := listValue(t, definitions["relations"])
	if len(relations) != 7 {
		t.Fatalf("relation count = %d", len(relations))
	}
	wantRelations := map[string]struct {
		from, to, cardinality string
		order                 float64
	}{
		"infrastructure.relation.site_grid_connection":   {"infrastructure.dimension.site", "infrastructure.dimension.grid_connection", "one_to_many", 10},
		"infrastructure.relation.grid_connection_feeder": {"infrastructure.dimension.grid_connection", "infrastructure.dimension.feeder", "one_to_many", 20},
		"infrastructure.relation.feeder_circuit":         {"infrastructure.dimension.feeder", "infrastructure.dimension.circuit", "one_to_many", 30},
		"infrastructure.relation.circuit_phase":          {"infrastructure.dimension.circuit", "infrastructure.dimension.phase", "one_to_many", 40},
		"infrastructure.relation.grid_connection_meter":  {"infrastructure.dimension.grid_connection", "infrastructure.dimension.meter", "one_to_many", 50},
		"infrastructure.relation.site_load":              {"infrastructure.dimension.site", "infrastructure.dimension.load", "one_to_many", 60},
		"infrastructure.relation.load_meter":             {"infrastructure.dimension.meter", "infrastructure.dimension.load", "one_to_many", 70},
	}
	for _, item := range relations {
		relation := mapValue(t, item)
		id := stringValue(t, relation["id"])
		want, ok := wantRelations[id]
		if !ok || stringValue(t, relation["from"]) != want.from || stringValue(t, relation["to"]) != want.to || stringValue(t, relation["cardinality"]) != want.cardinality || relation["order"] != want.order {
			t.Fatalf("unexpected relation: %#v", relation)
		}
		delete(wantRelations, id)
	}
	if len(wantRelations) != 0 {
		t.Fatalf("missing relations: %#v", wantRelations)
	}
	portal := listValue(t, definitions["portal_contributions"])
	if len(portal) != 8 {
		t.Fatalf("portal read descriptor count = %d", len(portal))
	}
	wantPortal := map[string]struct {
		service string
		fields  []string
	}{
		"infrastructure.portal.read.site":            {"infrastructure.service.site", []string{"infrastructure.status.fault"}},
		"infrastructure.portal.read.grid_connection": {"infrastructure.service.grid_connection", []string{"infrastructure.ac.frequency", "infrastructure.ac.power_factor", "infrastructure.limit.fallback_power", "infrastructure.limit.pv_curtailment_factor", "infrastructure.mgcp.ac_current", "infrastructure.mgcp.actor", "infrastructure.mgcp.committed_power_plan", "infrastructure.mgcp.per", "infrastructure.mgcp.slots", "infrastructure.mgcp.use_case_name", "infrastructure.mgcp.use_case_version", "infrastructure.mgcp.value_range_min", "infrastructure.mgcp.value_source", "infrastructure.power.apparent", "infrastructure.power.export_active", "infrastructure.power.export_reactive", "infrastructure.power.import_active", "infrastructure.power.import_reactive", "infrastructure.power.pcc", "infrastructure.status.connection", "infrastructure.status.readiness", "infrastructure.status.supply_condition"}},
		"infrastructure.portal.read.feeder":          {"infrastructure.service.feeder", []string{}},
		"infrastructure.portal.read.circuit":         {"infrastructure.service.circuit", []string{"infrastructure.ac.neutral_current", "infrastructure.status.breaker", "infrastructure.status.interlock"}},
		"infrastructure.portal.read.phase":           {"infrastructure.service.phase", []string{"infrastructure.ac.active_power", "infrastructure.ac.apparent_current", "infrastructure.ac.apparent_power", "infrastructure.ac.current", "infrastructure.ac.harmonic_current", "infrastructure.ac.harmonic_phase", "infrastructure.ac.phase_power_factor", "infrastructure.ac.reactive_current", "infrastructure.ac.reactive_power", "infrastructure.ac.voltage", "infrastructure.energy.phase_export", "infrastructure.energy.phase_import", "infrastructure.load.phase_active_power"}},
		"infrastructure.portal.read.meter":           {"infrastructure.service.meter", []string{"infrastructure.accuracy.measured", "infrastructure.accuracy.measured_maximum_active_current", "infrastructure.accuracy.measured_maximum_active_power", "infrastructure.accuracy.measured_minimum_active_current", "infrastructure.accuracy.measured_minimum_active_power", "infrastructure.accuracy.range_maximum_active_current", "infrastructure.accuracy.range_maximum_active_power", "infrastructure.accuracy.range_minimum_active_current", "infrastructure.accuracy.range_minimum_active_power", "infrastructure.accuracy.ratio_maximum", "infrastructure.energy.apparent_export", "infrastructure.energy.apparent_import", "infrastructure.energy.export", "infrastructure.energy.import", "infrastructure.energy.reactive_export", "infrastructure.energy.reactive_import", "infrastructure.energy.self_consumption", "infrastructure.meter.apparent_power_threshold", "infrastructure.meter.kind", "infrastructure.meter.point_of_delivery", "infrastructure.meter.power_threshold", "infrastructure.meter.power_threshold_source", "infrastructure.meter.protocol_version"}},
		"infrastructure.portal.read.line_pair":       {"infrastructure.service.line_pair", []string{"infrastructure.ac.voltage_line_line"}},
		"infrastructure.portal.read.load":            {"infrastructure.service.load", []string{"infrastructure.load.active_power", "infrastructure.load.energy_consumed", "infrastructure.load.energy_produced", "infrastructure.mpc.ac_power", "infrastructure.mpc.ac_voltage", "infrastructure.mpc.actor", "infrastructure.mpc.one", "infrastructure.mpc.per", "infrastructure.mpc.slots", "infrastructure.mpc.use_case_name", "infrastructure.mpc.use_case_version", "infrastructure.mpc.value_range_min", "infrastructure.mpc.value_state"}},
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
	if !ok || !exactFields(definitions) || !exactDimensions(definitions) || !exactQualifierDimensions(definitions) || !exactServicesAndCapabilities(definitions) {
		return fmt.Errorf("quantity contract")
	}
	if !exactRelations(definitions) {
		return fmt.Errorf("relation association")
	}
	boundary, ok := catalog["read_only_operation_boundary"].(map[string]any)
	if !ok || boundary["breaker_actuation"] != "not_published" || !emptyList(definitions["operations"]) || !emptyList(definitions["effect_rules"]) {
		return fmt.Errorf("read-only operation boundary")
	}
	mappings, ok := catalog["candidate_mappings"].([]any)
	if !ok || len(mappings) != 4 || mapValueNoPanic(mappings[0])["live_authority"] != false || mapValueNoPanic(mappings[2])["state"] != "unknown_pending_normative_source" || mapValueNoPanic(mappings[3])["qualification"] != "not_conformance" {
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
		if !ok || item["kind"] != string(spec.kind) || item["dimension"] != string(spec.dimension) || !sameIDs(item["qualifiers"], fieldQualifiers[spec.id]) {
			return false
		}
		switch spec.kind {
		case semreg.ValueQuantity:
			bounds, ok := item["bounds"].(map[string]any)
			unit, _ := item["unit"].(string)
			if !ok || unit != string(spec.unit) || !sameOptionalDecimal(bounds["minimum"], spec.minimum) || !sameOptionalDecimal(bounds["maximum"], spec.maximum) {
				return false
			}
		case semreg.ValueBoolean:
			if item["unit"] != nil || item["bounds"] != nil || item["symbols"] != nil {
				return false
			}
		case semreg.ValueSymbol, semreg.ValueSymbols:
			if !sameStrings(item["symbols"], spec.symbols) {
				return false
			}
		default:
			if item["unit"] != nil || item["symbols"] != nil {
				return false
			}
		}
	}
	return true
}

func sameOptionalDecimal(value any, want *decimalBound) bool {
	if want == nil {
		return value == nil
	}
	return sameDecimal(value, want.decimal())
}

func sameIDs(value any, want []semreg.DefinitionID) bool {
	if want == nil {
		return value == nil
	}
	got, ok := value.([]any)
	if !ok || len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != string(want[i]) {
			return false
		}
	}
	return true
}

func exactQualifierDimensions(definitions map[string]any) bool {
	entries, ok := definitions["qualifier_dimensions"].([]any)
	if !ok || len(entries) != len(qualifierKinds) {
		return false
	}
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			return false
		}
		id := semreg.DefinitionID(fmt.Sprint(item["id"]))
		kind, known := qualifierKinds[id]
		if !known || item["value_kind"] != string(kind) {
			return false
		}
		if kind == semreg.ValueSymbol && !sameStrings(item["symbols"], qualifierSymbols[id]) {
			return false
		}
		if kind == semreg.ValueQuantity {
			unit, _ := item["unit"].(string)
			if unit != "unit.one" {
				return false
			}
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

func exactRelations(definitions map[string]any) bool {
	entries, ok := definitions["relations"].([]any)
	return ok && len(entries) == 7 && mapValueNoPanic(entries[2])["cardinality"] == "one_to_many"
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
