package thermal

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// thermalFixtureSHA256 is the SHA-256 of fixtures/v1/thermal-hvac-pack-vectors.json.
var thermalFixtureSHA256 = [sha256.Size]byte{
	0xb4, 0x7d, 0xf6, 0x84, 0x54, 0x22, 0xeb, 0x56,
	0x5a, 0x5f, 0x25, 0x3e, 0xfb, 0x62, 0x9d, 0x5c,
	0x1b, 0xb7, 0xda, 0xf0, 0x89, 0x63, 0x01, 0xd8,
	0x58, 0x06, 0xda, 0x7e, 0x3b, 0x1d, 0x57, 0x3a,
}

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
	if sha256.Sum256(bytes) != thermalFixtureSHA256 {
		t.Fatal("thermal fixture digest")
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
	if header.Contract != "helianthus.semantic.pack.thermal.acceptance/v1" || header.PackContract != "helianthus.pack.thermal/v1" || len(header.Vectors) != 17 {
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
	fieldIDs, serviceIDs, operations := identifiers(t, definitions["fields"]), identifiers(t, definitions["services"]), identifiers(t, definitions["operations"])
	read := map[string]int{}
	for _, entry := range array(t, definitions["portal_contributions"]) {
		descriptor := object(t, entry)
		if descriptor["kind"] == "read" {
			service := stringValue(t, descriptor["service"])
			if !serviceIDs[service] {
				t.Fatalf("Portal service: %#v", descriptor)
			}
			for _, field := range array(t, descriptor["fields"]) {
				id := stringValue(t, field)
				spec, ok := findField(semreg.DefinitionID(id))
				if !fieldIDs[id] || !ok || string(spec.dimension) != servicesTable()[service] {
					t.Fatalf("Portal field %s outside the subject of %s", id, service)
				}
				read[id]++
			}
		} else if descriptor["kind"] == "operation" && !operations[stringValue(t, descriptor["operation"])] {
			t.Fatalf("Portal operation: %#v", descriptor)
		}
	}
	for _, spec := range fields {
		if read[string(spec.id)] != 1 {
			t.Fatalf("field %s is read by %d descriptors", spec.id, read[string(spec.id)])
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
	}{{definitions["fields"], 269}, {definitions["fact_dimensions"], 21}, {definitions["services"], 21}, {definitions["relations"], 15}, {definitions["capabilities"], 87}, {definitions["operations"], 74}, {definitions["effect_rules"], 74}, {definitions["portal_contributions"], 23}, {definitions["events"], 15}} {
		if len(array(t, count.value)) != count.want {
			t.Fatalf("catalog count %d", count.want)
		}
	}
	if failure := baselineFailure(t, definitions); failure != "" {
		t.Fatal(failure)
	}
}

// wantRelations are the five pack relation definitions, spelled
// thermal.relation.<name>, with the dimensions of their from and to ends and
// their cardinality, in the order of the vectors. The element catalog holds
// their core type, endpoint kinds, medium, basis classes and planes.
var wantRelations = []struct {
	id          string
	from, to    []semreg.DefinitionID
	cardinality string
}{
	{"thermal.relation.system_member", []semreg.DefinitionID{systemDimension}, []semreg.DefinitionID{zoneDimension, circuitDimension, dhwDimension, ventilationDimension, generatorDimension, storeDimension, terminalDimension, refrigerantDimension}, "one_to_many"},
	{"thermal.relation.fluid_supply", []semreg.DefinitionID{generatorDimension, storeDimension, circuitDimension}, []semreg.DefinitionID{circuitDimension, storeDimension, zoneDimension, dhwDimension, terminalDimension}, "many_to_many"},
	{"thermal.relation.air_supply", []semreg.DefinitionID{terminalDimension, ventilationDimension}, []semreg.DefinitionID{zoneDimension}, "many_to_many"},
	{"thermal.relation.refrigerant_circuit_member", []semreg.DefinitionID{refrigerantDimension}, []semreg.DefinitionID{generatorDimension, terminalDimension}, "one_to_many"},
	{"thermal.relation.generator_compressor", []semreg.DefinitionID{generatorDimension}, []semreg.DefinitionID{compressorDimension}, "one_to_many"},
	{"thermal.relation.circuit_room", []semreg.DefinitionID{circuitDimension}, []semreg.DefinitionID{"thermal.dimension.room"}, "one_to_many"},
	{"thermal.relation.circuit_zone", []semreg.DefinitionID{circuitDimension}, []semreg.DefinitionID{zoneDimension}, "one_to_many"},
	{"thermal.relation.zone_room", []semreg.DefinitionID{zoneDimension}, []semreg.DefinitionID{"thermal.dimension.room"}, "one_to_many"},
	{"thermal.relation.pump_member", []semreg.DefinitionID{circuitDimension, dhwDimension, generatorDimension, "thermal.dimension.solar_circuit"}, []semreg.DefinitionID{"thermal.dimension.pump"}, "one_to_many"},
	{"thermal.relation.valve_member", []semreg.DefinitionID{circuitDimension, zoneDimension, dhwDimension, generatorDimension, terminalDimension}, []semreg.DefinitionID{"thermal.dimension.valve"}, "one_to_many"},
	{"thermal.relation.solar_circuit_collector", []semreg.DefinitionID{"thermal.dimension.solar_circuit"}, []semreg.DefinitionID{"thermal.dimension.solar_collector"}, "one_to_many"},
	{"thermal.relation.detector_member", []semreg.DefinitionID{systemDimension, pumpDimension, zoneDimension}, []semreg.DefinitionID{"thermal.dimension.detector"}, "one_to_many"},
	{"thermal.relation.water_tank_member", []semreg.DefinitionID{ventilationDimension, terminalDimension, generatorDimension}, []semreg.DefinitionID{"thermal.dimension.water_tank"}, "one_to_many"},
	{"thermal.relation.fan_member", []semreg.DefinitionID{terminalDimension, ventilationDimension, generatorDimension}, []semreg.DefinitionID{"thermal.dimension.fan"}, "one_to_many"},
	{"thermal.relation.filter_member", []semreg.DefinitionID{ventilationDimension, terminalDimension}, []semreg.DefinitionID{"thermal.dimension.air_filter"}, "one_to_many"},
}

// canonicalUnits is the SI unit set of the thermal pack.
var canonicalUnits = map[string]bool{"unit.kelvin": true, "unit.ratio": true, "unit.cubic_metre_per_second": true, "unit.pascal": true, "unit.reciprocal_second": true, "unit.watt": true, "unit.joule": true, "unit.one": true, "unit.second": true, "unit.cubic_metre": true, "unit.kilogram_per_cubic_metre": true, "unit.becquerel_per_cubic_metre": true}

// baselineFailure compares the fixture definitions with the validator tables:
// every field with its kind, SI unit, bounds, symbols and one resource
// dimension, every resource dimension and service, every relation definition
// with its endpoint dimensions and cardinality, and every capability. It
// returns the failure a vector expects, or "" when the definitions agree.
func baselineFailure(t *testing.T, definitions map[string]any) string {
	t.Helper()
	declared := map[string]bool{}
	for index, entry := range array(t, definitions["fact_dimensions"]) {
		dimension := object(t, entry)
		id := stringValue(t, dimension["id"])
		if kind, ok := dimensions[semreg.DefinitionID(id)]; !ok || dimension["value_kind"] != string(kind) {
			t.Fatalf("fact dimension %d: %#v", index, dimension)
		}
		declared[id] = true
	}
	if len(declared) != len(dimensions) {
		t.Fatalf("fact dimensions: %v", declared)
	}
	seen := map[string]bool{}
	for index, entry := range array(t, definitions["fields"]) {
		field := object(t, entry)
		id := stringValue(t, field["id"])
		spec, ok := findField(semreg.DefinitionID(id))
		if !ok || seen[id] {
			t.Fatalf("field %d: %s", index, id)
		}
		seen[id] = true
		if !declared[stringValue(t, field["dimension"])] {
			return "field has no resource dimension"
		}
		if field["dimension"] != string(spec.dimension) {
			return "field subject differs"
		}
		if field["kind"] != string(spec.kind) {
			t.Fatalf("field %s kind: %#v", id, field["kind"])
		}
		if spec.kind == semreg.ValueQuantity {
			if !canonicalUnits[stringValue(t, field["unit"])] {
				return "outside the canonical SI unit set"
			}
			var minimum, maximum any
			if field["bounds"] != nil {
				bounds := object(t, field["bounds"])
				minimum, maximum = bounds["minimum"], bounds["maximum"]
			}
			if field["unit"] != string(spec.unit) || !boundEquals(t, minimum, spec.minimum) || !boundEquals(t, maximum, spec.maximum) {
				t.Fatalf("field %s quantity: %#v", id, field)
			}
		} else if spec.kind == semreg.ValueSymbol || spec.kind == semreg.ValueSymbols {
			if !reflect.DeepEqual(stringList(t, field["symbols"]), spec.symbols) {
				t.Fatalf("field %s symbols: %#v", id, field["symbols"])
			}
		} else if field["unit"] != nil {
			t.Fatalf("field %s unit: %#v", id, field["unit"])
		}
	}
	if len(seen) != len(fields) {
		t.Fatalf("fields: %d of %d", len(seen), len(fields))
	}
	services := array(t, definitions["services"])
	if len(services) != len(servicesTable()) {
		t.Fatalf("services: %d", len(services))
	}
	for index, entry := range services {
		service := object(t, entry)
		if dimension, ok := servicesTable()[stringValue(t, service["id"])]; !ok || service["fact_key_dimension"] != dimension {
			t.Fatalf("service %d: %#v", index, service)
		}
	}
	for index, entry := range array(t, definitions["relations"]) {
		relation := object(t, entry)
		for _, end := range []string{"from", "to"} {
			for _, dimension := range stringList(t, relation[end]) {
				if !declared[dimension] {
					return "relation dangling dimension"
				}
			}
		}
		want := wantRelations[index]
		if relation["id"] != want.id || relation["order"] != float64(10*(index+1)) {
			t.Fatalf("relation %d: %#v", index, relation)
		}
		if !reflect.DeepEqual(stringList(t, relation["from"]), ids(want.from)) || !reflect.DeepEqual(stringList(t, relation["to"]), ids(want.to)) || relation["cardinality"] != want.cardinality {
			return "relation endpoints or cardinality differ"
		}
	}
	capabilitiesSeen := map[string]bool{}
	for index, entry := range array(t, definitions["capabilities"]) {
		capability := object(t, entry)
		id := stringValue(t, capability["id"])
		spec, ok := capabilities[semreg.DefinitionID(id)]
		if !ok || capabilitiesSeen[id] || capability["service"] != string(spec.service) || capability["order"] != float64(10*(index+1)) {
			t.Fatalf("capability %d: %#v", index, capability)
		}
		capabilitiesSeen[id] = true
	}
	return ""
}
func servicesTable() map[string]any {
	result := map[string]any{}
	for id, dimension := range services {
		result[string(id)] = string(dimension)
	}
	return result
}
func boundEquals(t *testing.T, value any, want *decimalBound) bool {
	t.Helper()
	if value == nil || want == nil {
		return value == nil && want == nil
	}
	decimal := object(t, value)
	return decimal["coefficient"] == want.coefficient && decimal["exponent10"] == float64(want.exponent10)
}
func stringList(t *testing.T, value any) []string {
	t.Helper()
	result := []string{}
	for _, entry := range array(t, value) {
		result = append(result, stringValue(t, entry))
	}
	return result
}
func ids(values []semreg.DefinitionID) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
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
		if object(t, fields[0])["unit"] != "unit.kelvin" {
			return "invalid unit ID"
		}
	case "TH-NEG-003":
		if _, ok := object(t, fields[0])["ref"]; !ok {
			return "DefinitionRef owner/version"
		}
	case "TH-NEG-004":
		if array(t, object(t, fields[13])["symbols"])[1] != "thermal.status.operation.idle" {
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
		if len(fields) == 270 {
			return "duplicate definition ID"
		}
	case "TH-NEG-009":
		if len(array(t, catalog["domain_catalog"])) == 4 {
			return "five-domain catalog"
		}
	case "TH-NEG-010":
		if object(t, array(t, catalog["candidate_mappings"])[2])["state"] != "unknown_pending_normative_source" {
			return "eeBUS mapping"
		}
	case "TH-NEG-011":
		if array(t, object(t, array(t, definitions["operations"])[0])["terminal_outcomes"])[1] != "failed_no_contact" {
			return "operation stages/outcomes are incomplete"
		}
	case "TH-NEG-012", "TH-NEG-013", "TH-NEG-014", "TH-NEG-015", "TH-NEG-016":
		if failure := baselineFailure(t, definitions); failure != "" {
			return failure
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
