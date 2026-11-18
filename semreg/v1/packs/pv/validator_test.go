package pv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestPVFieldsIndexAndRace(t *testing.T) {
	v := New()
	x := v.Definitions()
	if err := x.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(x.Fields) != 30 || len(x.Services) != 6 || len(x.Capabilities) != 8 || len(x.Operations) != 2 || len(x.EffectRules) != 2 {
		t.Fatal(x)
	}
	for _, s := range fields {
		name := "id"
		k := semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: s.id, Dimensions: []semreg.Dimension{{ID: s.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}}
		z := semreg.Decimal{Coefficient: "0"}
		if s.minimum != nil {
			z = s.minimum.decimal()
		}
		q := semreg.Value{Kind: s.kind}
		if s.kind == semreg.ValueQuantity {
			q.Quantity = &semreg.Quantity{Number: z, Unit: s.unit}
		} else {
			q.Symbol = &semreg.Symbol{Namespace: s.id, Token: strings.TrimPrefix(s.symbols[0], string(s.id)+"."), Known: true}
		}
		if err := v.ValidateFact(k, &q); err != nil {
			t.Fatal(s.id, err)
		}
		if s.minimum != nil {
			b := q
			b.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "-99999999"}, Unit: s.unit}
			if semreg.ErrorIdentifier(v.ValidateFact(k, &b)) != semreg.BoundsExceeded && s.minimum.decimal().Coefficient != "-1" {
				t.Fatal(s.id)
			}
		}
	}
	var wg sync.WaitGroup
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 64; i++ {
				if len(v.Definitions().Fields) != 30 {
					t.Error("index")
				}
			}
		}()
	}
	wg.Wait()
}
func TestPVMachineVectors(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	raw, e := os.ReadFile(filepath.Join(filepath.Dir(f), "..", "..", "..", "..", "fixtures", "v1", "pv-inverter-pack-vectors.json"))
	if e != nil {
		t.Fatal(e)
	}
	if s := sha256.Sum256(raw); s != pvFixtureSHA256 {
		t.Fatalf("fixture SHA-256 = %s", hex.EncodeToString(s[:]))
	}
	var d struct {
		Vectors []struct {
			ID     string `json:"id"`
			Expect *struct {
				Error string `json:"error"`
			} `json:"expect"`
		} `json:"vectors"`
	}
	if e = json.Unmarshal(raw, &d); e != nil {
		t.Fatal(e)
	}
	if len(d.Vectors) != 12 {
		t.Fatal(len(d.Vectors))
	}
	for _, v := range d.Vectors {
		t.Run(v.ID, func(t *testing.T) {
			if v.ID == "PV-POS-001" && v.Expect != nil {
				t.Fatal(v)
			}
			if v.ID != "PV-POS-001" && (v.Expect == nil || v.Expect.Error == "") {
				t.Fatal(v)
			}
		})
	}
}

// pvFixtureSHA256 is the SHA-256 of fixtures/v1/pv-inverter-pack-vectors.json.
var pvFixtureSHA256 = [sha256.Size]byte{
	0xec, 0xe4, 0xc9, 0xd5, 0x72, 0x0e, 0xfc, 0xeb,
	0x54, 0x42, 0x85, 0xf3, 0xcd, 0xb9, 0x0a, 0x1d,
	0x85, 0xd9, 0x01, 0x53, 0x0b, 0xff, 0xe0, 0xe8,
	0x9a, 0x89, 0x3f, 0xb5, 0x17, 0xf4, 0x8f, 0xac,
}

// pvRelations lists the accepted PV relations in declared order; every one
// declares cardinality one_to_many.
var pvRelations = []struct{ id, from, to string }{
	{"pv.relation.system_inverter", "pv.dimension.system", "pv.dimension.inverter"},
	{"pv.relation.inverter_array", "pv.dimension.inverter", "pv.dimension.array"},
	{"pv.relation.array_string", "pv.dimension.array", "pv.dimension.string"},
	{"pv.relation.inverter_input", "pv.dimension.inverter", "pv.dimension.input"},
	{"pv.relation.inverter_phase", "pv.dimension.inverter", "pv.dimension.phase"},
}

// pvRelationsExact reports whether the catalog relations equal pvRelations,
// including identity, endpoints, declared cardinality and order.
func pvRelationsExact(t *testing.T, relations []any) bool {
	t.Helper()
	if len(relations) != len(pvRelations) {
		return false
	}
	for index, want := range pvRelations {
		relation := pvObj(t, relations[index])
		ref := pvObj(t, relation["ref"])
		refPack := pvObj(t, ref["pack"])
		if relation["id"] != want.id || ref["id"] != want.id || ref["version"] != string(pack.Version) || refPack["id"] != string(pack.ID) || refPack["version"] != string(pack.Version) || relation["from"] != want.from || relation["to"] != want.to || relation["cardinality"] != "one_to_many" || relation["order"] != float64(10*(index+1)) {
			return false
		}
	}
	return true
}

func TestPVRelationsDeclareCardinality(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(f), "..", "..", "..", "..", "fixtures", "v1", "pv-inverter-pack-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	defs := pvObj(t, pvObj(t, document["catalog"])["definitions"])
	if !pvRelationsExact(t, defs["relations"].([]any)) {
		t.Fatalf("relations: %#v", defs["relations"])
	}
	for _, mutation := range []struct {
		name  string
		apply func(t *testing.T, relation map[string]any)
	}{
		{"id", func(t *testing.T, r map[string]any) { r["id"] = "pv.relation.other" }},
		{"ref id", func(t *testing.T, r map[string]any) { pvObj(t, r["ref"])["id"] = "pv.relation.other" }},
		{"ref version", func(t *testing.T, r map[string]any) { pvObj(t, r["ref"])["version"] = "1.0.1" }},
		{"ref pack id", func(t *testing.T, r map[string]any) {
			pvObj(t, pvObj(t, r["ref"])["pack"])["id"] = "helianthus.pack.other"
		}},
		{"ref pack version", func(t *testing.T, r map[string]any) { pvObj(t, pvObj(t, r["ref"])["pack"])["version"] = "1.0.1" }},
		{"from", func(t *testing.T, r map[string]any) { r["from"] = "pv.dimension.other" }},
		{"to", func(t *testing.T, r map[string]any) { r["to"] = "pv.dimension.other" }},
		{"cardinality", func(t *testing.T, r map[string]any) { r["cardinality"] = "one_to_one" }},
		{"cardinality absent", func(t *testing.T, r map[string]any) { delete(r, "cardinality") }},
		{"order", func(t *testing.T, r map[string]any) { r["order"] = float64(11) }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			var copied map[string]any
			if err := json.Unmarshal(raw, &copied); err != nil {
				t.Fatal(err)
			}
			relations := pvObj(t, pvObj(t, copied["catalog"])["definitions"])["relations"].([]any)
			mutation.apply(t, pvObj(t, relations[0]))
			if pvRelationsExact(t, relations) {
				t.Fatalf("%s mutation accepted", mutation.name)
			}
		})
	}
}

type pvMutation struct {
	Op, Path, ID string
	Value        any
}

func TestPVFixtureMutations(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(f), "..", "..", "..", "..", "fixtures", "v1", "pv-inverter-pack-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if err = json.Unmarshal(raw, &base); err != nil {
		t.Fatal(err)
	}
	var h struct {
		Vectors []struct {
			ID     string `json:"id"`
			Expect *struct {
				Error string `json:"error"`
			} `json:"expect"`
			Input struct {
				Mutations []pvMutation `json:"mutations"`
			} `json:"input"`
		} `json:"vectors"`
	}
	if err = json.Unmarshal(raw, &h); err != nil {
		t.Fatal(err)
	}
	for _, v := range h.Vectors {
		t.Run(v.ID, func(t *testing.T) {
			b, _ := json.Marshal(base)
			var c map[string]any
			_ = json.Unmarshal(b, &c)
			for _, m := range v.Input.Mutations {
				pvApply(t, c, "catalog."+m.Path, m)
			}
			if v.Expect == nil {
				if pvObj(t, c["catalog"])["pack"].(map[string]any)["id"] != string(pack.ID) {
					t.Fatal("baseline")
				}
				return
			}
			if got := pvVectorFailure(t, v.ID, c); got != v.Expect.Error {
				t.Fatalf("%s want %s", got, v.Expect.Error)
			}
		})
	}
}
func pvApply(t *testing.T, d map[string]any, path string, m pvMutation) {
	p := strings.Split(path, ".")
	var cur any = d
	for _, x := range p[:len(p)-1] {
		switch z := cur.(type) {
		case map[string]any:
			cur = z[x]
		case []any:
			var i int
			if _, e := fmt.Sscan(x, &i); e != nil {
				t.Fatal(e)
			}
			cur = z[i]
		}
	}
	last := p[len(p)-1]
	if z, ok := cur.(map[string]any); ok && m.Op == "remove_id" {
		a := z[last].([]any)
		out := make([]any, 0, len(a))
		for _, x := range a {
			if text, ok := x.(string); ok {
				if text != m.ID {
					out = append(out, x)
				}
				continue
			}
			if pvObj(t, x)["id"] != m.ID {
				out = append(out, x)
			}
		}
		z[last] = out
		return
	}
	switch z := cur.(type) {
	case map[string]any:
		if m.Op == "delete" {
			delete(z, last)
		} else {
			z[last] = m.Value
		}
	case []any:
		var i int
		if _, e := fmt.Sscan(last, &i); e != nil {
			t.Fatal(e)
		}
		z[i] = m.Value
	}
}
func pvObj(t *testing.T, x any) map[string]any {
	t.Helper()
	v, ok := x.(map[string]any)
	if !ok {
		t.Fatal("object")
	}
	return v
}
func pvVectorFailure(t *testing.T, id string, d map[string]any) string {
	c := pvObj(t, d["catalog"])
	defs := pvObj(t, c["definitions"])
	switch id {
	case "PV-NEG-001":
		if pvObj(t, c["pack"])["id"] != string(pack.ID) {
			k := testKey(fields[0])
			k.PackID = semreg.DefinitionID(pvObj(t, c["pack"])["id"].(string))
			x := testValue(fields[0])
			if semreg.ErrorIdentifier(New().ValidateFact(k, &x)) == semreg.DefinitionOwnerMissing {
				return "exact PackRef"
			}
		}
	case "PV-NEG-002":
		if len(c["domain_catalog"].([]any)) != 5 {
			return "five-domain catalog"
		}
	case "PV-NEG-003":
		if _, ok := pvObj(t, defs["fields"].([]any)[0])["ref"]; !ok {
			return "DefinitionRef owner/version"
		}
	case "PV-NEG-004":
		if unit := pvObj(t, defs["fields"].([]any)[0])["unit"]; unit != "unit.volt" {
			k := testKey(fields[0])
			x := testValue(fields[0])
			x.Quantity.Unit = semreg.DefinitionID(unit.(string))
			if New().ValidateFact(k, &x) != nil {
				return "invalid unit ID"
			}
		}
	case "PV-NEG-005":
		if !pvRelationsExact(t, defs["relations"].([]any)) {
			return "relations catalog"
		}
	case "PV-NEG-006":
		if pvObj(t, c["candidate_mappings"].([]any)[0])["state"] != "candidate" {
			return "native mapping boundary"
		}
	case "PV-NEG-007":
		if pvObj(t, c["candidate_mappings"].([]any)[2])["state"] != "unknown_pending_normative_source" {
			return "native mapping boundary"
		}
	case "PV-NEG-008":
		if pvObj(t, defs["operations"].([]any)[0])["preconditions"].([]any)[3] != "authority_admitted" {
			return "operation lacks admission/readback"
		}
	case "PV-NEG-009":
		if id := pvObj(t, defs["operations"].([]any)[0])["id"]; id != "pv.operation.set_active_power_limit" {
			i := intent()
			i.Kind.ID = semreg.DefinitionID(id.(string))
			if semreg.ErrorIdentifier(New().(operation.OperationPackValidator).ValidateIntent(i)) == semreg.DefinitionOwnerMissing {
				return "operations catalog"
			}
		}
	case "PV-NEG-010":
		if _, ok := c["counter_policy"]; !ok {
			return "counter policy"
		}
	case "PV-NEG-011":
		if len(pvObj(t, defs["portal_contributions"].([]any)[1])["fields"].([]any)) != 13 {
			return "Portal read association"
		}
	}
	t.Fatal(id)
	return ""
}

func TestPVExactBoundsSymbolsServicesCapabilitiesAndDetachment(t *testing.T) {
	v := New()
	for _, s := range fields {
		if s.kind == semreg.ValueQuantity {
			for _, n := range []semreg.Decimal{s.minimum.decimal(), s.maximum.decimal()} {
				k := testKey(s)
				x := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: n, Unit: s.unit}}
				if err := v.ValidateFact(k, &x); err != nil {
					t.Fatal(s.id, n, err)
				}
			}
			for _, n := range []semreg.Decimal{pvOutside(s.minimum.decimal(), -1), pvOutside(s.maximum.decimal(), 1)} {
				k := testKey(s)
				x := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: n, Unit: s.unit}}
				require(t, v.ValidateFact(k, &x), semreg.BoundsExceeded)
			}
		} else {
			for _, full := range s.symbols {
				k := testKey(s)
				x := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: s.id, Token: strings.TrimPrefix(full, string(s.id)+"."), Known: true}}
				if err := v.ValidateFact(k, &x); err != nil {
					t.Fatal(full, err)
				}
			}
			k := testKey(s)
			bad := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: "pv.other", Token: "x", Known: true}}
			require(t, v.ValidateFact(k, &bad), semreg.InvalidValue)
		}
	}
	for id := range services {
		s := semreg.ServiceInstance{InstanceID: "service:1", AssetID: "asset:1", Definition: definition(id), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}
		if err := v.ValidateService(s); err != nil {
			t.Fatal(id, err)
		}
		s.Definition.Pack.ID = "helianthus.pack.other"
		require(t, v.ValidateService(s), semreg.DefinitionOwnerMissing)
	}
	for id := range capabilities {
		c := testCapability(id)
		if err := v.ValidateCapability(c); err != nil {
			t.Fatal(id, err)
		}
		c.Qualification = semreg.QualificationCandidate
		require(t, v.ValidateCapability(c), semreg.CapabilityNotQualified)
		c = testCapability(id)
		c.Availability = semreg.AvailabilityUnavailable
		require(t, v.ValidateCapability(c), semreg.CapabilityUnavailable)
		if len(c.Constraints) > 0 {
			c = testCapability(id)
			args := append([]semreg.TypedField(nil), c.Constraints...)
			if err := v.(interface {
				MatchConstraints(semreg.CapabilityInstance, []semreg.TypedField) error
			}).MatchConstraints(c, args); err != nil {
				t.Fatal(id, err)
			}
			args[0].Value.Quantity.Number = pvOutside(args[0].Value.Quantity.Number, 1)
			require(t, v.(interface {
				MatchConstraints(semreg.CapabilityInstance, []semreg.TypedField) error
			}).MatchConstraints(c, args), semreg.InvalidValue)
		}
	}
	f := newPVFixture(t, nil)
	before, _ := semreg.CanonicalJSON(f.snapshot)
	bad := f.intent
	bad.ExpectedDriverGeneration = "2"
	_, err := f.kernel.Admit(f.snapshot, f.current, bad, operation.AuthorityResolverFunc(func(i operation.Intent, _ operation.Route, _ semreg.EvaluationContext) error {
		i.Arguments[0].ID = "pv.changed"
		return nil
	}))
	require(t, err, semreg.StaleDriverGeneration)
	after, _ := semreg.CanonicalJSON(f.snapshot)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected admission mutated snapshot")
	}
	good := newPVFixture(t, nil)
	intentBefore, _ := semreg.CanonicalJSON(good.intent)
	if _, e := good.kernel.Admit(good.snapshot, good.current, good.intent, operation.AuthorityResolverFunc(func(i operation.Intent, _ operation.Route, _ semreg.EvaluationContext) error {
		i.Arguments[0].ID = "pv.changed"
		return nil
	})); e != nil {
		t.Fatal(e)
	}
	intentAfter, _ := semreg.CanonicalJSON(good.intent)
	if !bytes.Equal(intentBefore, intentAfter) {
		t.Fatal("authority hook mutated intent")
	}
}
func pvOutside(n semreg.Decimal, d int64) semreg.Decimal {
	x, _ := strconv.ParseInt(n.Coefficient, 10, 64)
	return semreg.Decimal{Coefficient: strconv.FormatInt(x+d, 10), Exponent10: n.Exponent10}
}

func pvGraphTestFact(s fieldSpec) (semreg.FactKey, semreg.Value) {
	name := "id"
	k := semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: s.id, Dimensions: []semreg.Dimension{{ID: s.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}}
	z := semreg.Decimal{Coefficient: "0"}
	if s.minimum != nil {
		z = s.minimum.decimal()
	}
	q := semreg.Value{Kind: s.kind}
	if s.kind == semreg.ValueQuantity {
		q.Quantity = &semreg.Quantity{Number: z, Unit: s.unit}
	} else {
		q.Symbol = &semreg.Symbol{Namespace: s.id, Token: strings.TrimPrefix(s.symbols[0], string(s.id)+"."), Known: true}
	}
	return k, q
}

// Under graph enforcement the field's dimension is the dimension the catalog
// declares: as a resource dimension it carries an opaque resource identifier,
// as a qualifier dimension it keeps its shape, and no undeclared dimension is
// accepted. Without the catalog ValidateFact is unchanged.
func TestValidateGraphFactUsesTheDeclaredDimensions(t *testing.T) {
	var graphValidator semreg.GraphFactValidator = New().(semreg.GraphFactValidator)
	for _, s := range fields {
		key, value := pvGraphTestFact(s)
		withText := func(text string) semreg.FactKey {
			changed := key
			changed.Dimensions = []semreg.Dimension{{ID: key.Dimensions[0].ID, Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}}
			return changed
		}
		resource := semreg.GraphFactSubject{Disposition: semreg.GraphMember, Resolved: true, Dimension: s.dimension, Kind: "resource.equipment"}
		qualifier := semreg.GraphFactSubject{Disposition: semreg.GraphUnknown, Qualifiers: []semreg.DefinitionID{s.dimension}}
		if err := graphValidator.ValidateGraphFact(withText("resource:unit:01"), &value, resource); err != nil {
			t.Fatalf("%s: resource identifier refused: %v", s.id, err)
		}
		if got := semreg.ErrorIdentifier(graphValidator.ValidateGraphFact(withText("unit 01"), &value, resource)); got != semreg.InvalidValue {
			t.Fatalf("%s: resource dimension text accepted: %s", s.id, got)
		}
		if err := graphValidator.ValidateGraphFact(withText("unit 01"), &value, qualifier); err != nil {
			t.Fatalf("%s: qualifier dimension refused: %v", s.id, err)
		}
		if err := New().ValidateFact(withText("unit 01"), &value); err != nil {
			t.Fatalf("%s: text dimension refused without the catalog: %v", s.id, err)
		}
		for subjectName, subject := range map[string]semreg.GraphFactSubject{
			"another resource dimension":      {Disposition: semreg.GraphMember, Resolved: true, Dimension: "zz.dimension.other", Kind: "resource.equipment"},
			"an undeclared extra qualifier":   {Disposition: semreg.GraphMember, Resolved: true, Dimension: s.dimension, Kind: "resource.equipment", Qualifiers: []semreg.DefinitionID{"zz.dimension.role"}},
			"no declared dimension":           {Disposition: semreg.GraphUnknown},
			"another declared qualifier only": {Disposition: semreg.GraphUnknown, Qualifiers: []semreg.DefinitionID{"zz.dimension.role"}},
		} {
			if got := semreg.ErrorIdentifier(graphValidator.ValidateGraphFact(withText("resource:unit:01"), &value, subject)); got != semreg.InvalidValue {
				t.Fatalf("%s with %s: %s", s.id, subjectName, got)
			}
		}
	}
}

// TestPVStringEnergyStaysDistinct keeps per-string cumulative DC energy,
// per-phase apparent power and per-phase reactive power apart from the
// system-level generated energy. Each of the three is accepted only on its own
// dimension, in its own unit and inside its own bounds; string energy is
// refused on the input and system dimensions, generated energy keeps the
// system dimension and its nonnegative lower bound, and no capability or
// operation uses the three.
func TestPVStringEnergyStaysDistinct(t *testing.T) {
	v := New()
	generated, ok := findField("pv.energy.generated")
	if !ok {
		t.Fatal("pv.energy.generated is absent")
	}
	if !pvSameSpec(generated, semreg.ValueQuantity, "unit.joule", "pv.dimension.system", bound("0", 0), bound("36", 17)) {
		t.Fatalf("pv.energy.generated changed: %s", pvDescribe(generated))
	}
	infrastructure := pvFixtureFields(t, "infrastructure-pack-vectors.json", "catalog", "definitions", "fields")
	infrastructureBound := func(id, side string) *decimalBound {
		t.Helper()
		bounds, _ := pvFieldEntry(t, infrastructure, id)["bounds"].(map[string]any)
		value, ok := bounds[side].(map[string]any)
		if !ok {
			t.Fatalf("%s has no %s", id, side)
		}
		coefficient, _ := value["coefficient"].(string)
		exponent, _ := value["exponent10"].(float64)
		return bound(coefficient, int32(exponent))
	}
	pvRequireDistinct(t, v, []pvDistinctCase{
		{"pv.dc.energy", "unit.joule", "pv.dimension.string", bound("0", 0), generated.maximum, false},
		{"pv.ac.apparent_power", "unit.volt_ampere", "pv.dimension.phase", infrastructureBound("infrastructure.ac.apparent_power", "minimum"), infrastructureBound("infrastructure.ac.apparent_power", "maximum"), false},
		{"pv.ac.reactive_power", "unit.volt_ampere_reactive", "pv.dimension.phase", infrastructureBound("infrastructure.ac.reactive_power", "minimum"), infrastructureBound("infrastructure.ac.reactive_power", "maximum"), true},
	})
	quantity := func(n semreg.Decimal, unit semreg.DefinitionID) *semreg.Value {
		return &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: n, Unit: unit}}
	}
	key := func(id, dimension semreg.DefinitionID) semreg.FactKey {
		return testKey(fieldSpec{id: id, dimension: dimension})
	}
	for _, dimension := range []semreg.DefinitionID{"pv.dimension.input", "pv.dimension.system"} {
		if got := pvRefusal(v.ValidateFact(key("pv.dc.energy", dimension), quantity(semreg.Decimal{Coefficient: "1"}, "unit.joule"))); got != semreg.InvalidValue {
			t.Fatalf("pv.dc.energy on %s: %s", dimension, got)
		}
	}
	if got := pvRefusal(v.ValidateFact(key("pv.energy.generated", "pv.dimension.string"), quantity(semreg.Decimal{Coefficient: "1"}, "unit.joule"))); got != semreg.InvalidValue {
		t.Fatalf("pv.energy.generated on pv.dimension.string: %s", got)
	}
	if got := pvRefusal(v.ValidateFact(key("pv.energy.generated", "pv.dimension.system"), quantity(semreg.Decimal{Coefficient: "-1"}, "unit.joule"))); got != semreg.BoundsExceeded {
		t.Fatalf("negative pv.energy.generated: %s", got)
	}
}

// TestPVInverterTotalApparentAndReactivePower keeps the inverter totals of
// apparent and reactive power apart from the per-phase values.
// pv.ac.aggregate_apparent_power is nonnegative in volt-ampere and
// pv.ac.aggregate_reactive_power is signed in volt-ampere reactive, both on
// the inverter dimension and both bounded by the magnitude of
// pv.ac.aggregate_active_power. The per-phase apparent and reactive power
// keep the phase dimension, neither total is accepted on the phase
// dimension, and no capability or operation uses either total.
func TestPVInverterTotalApparentAndReactivePower(t *testing.T) {
	v := New()
	active, ok := findField("pv.ac.aggregate_active_power")
	if !ok {
		t.Fatal("pv.ac.aggregate_active_power is absent")
	}
	if !pvSameSpec(active, semreg.ValueQuantity, "unit.watt", "pv.dimension.inverter", bound("-1", 7), bound("1", 7)) {
		t.Fatalf("pv.ac.aggregate_active_power changed: %s", pvDescribe(active))
	}
	for _, phase := range []struct {
		id, unit         semreg.DefinitionID
		minimum, maximum *decimalBound
	}{
		{"pv.ac.apparent_power", "unit.volt_ampere", bound("0", 0), bound("1", 7)},
		{"pv.ac.reactive_power", "unit.volt_ampere_reactive", bound("-1", 7), bound("1", 7)},
	} {
		spec, ok := findField(phase.id)
		if !ok || !pvSameSpec(spec, semreg.ValueQuantity, phase.unit, "pv.dimension.phase", phase.minimum, phase.maximum) {
			t.Fatalf("%s changed: %s", phase.id, pvDescribe(spec))
		}
	}
	pvRequireDistinct(t, v, []pvDistinctCase{
		{"pv.ac.aggregate_apparent_power", "unit.volt_ampere", "pv.dimension.inverter", bound("0", 0), active.maximum, false},
		{"pv.ac.aggregate_reactive_power", "unit.volt_ampere_reactive", "pv.dimension.inverter", active.minimum, active.maximum, true},
	})
	quantity := func(n semreg.Decimal, unit semreg.DefinitionID) *semreg.Value {
		return &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: n, Unit: unit}}
	}
	for total, perPhase := range map[semreg.DefinitionID]semreg.DefinitionID{
		"pv.ac.aggregate_apparent_power": "pv.ac.apparent_power",
		"pv.ac.aggregate_reactive_power": "pv.ac.reactive_power",
	} {
		spec, _ := findField(total)
		one := quantity(semreg.Decimal{Coefficient: "1"}, spec.unit)
		if got := pvRefusal(v.ValidateFact(testKey(fieldSpec{id: total, dimension: "pv.dimension.phase"}), one)); got != semreg.InvalidValue {
			t.Fatalf("%s on pv.dimension.phase: %s", total, got)
		}
		if got := pvRefusal(v.ValidateFact(testKey(fieldSpec{id: perPhase, dimension: "pv.dimension.inverter"}), one)); got != semreg.InvalidValue {
			t.Fatalf("%s on pv.dimension.inverter: %s", perPhase, got)
		}
	}
}

// pvDistinctCase is the exact contract of one field that must stay apart
// from the fields it could be confused with.
type pvDistinctCase struct {
	id, unit, dimension semreg.DefinitionID
	minimum, maximum    *decimalBound
	signed              bool
}

// pvRequireDistinct requires each case to be in the table, the definition
// index and the metadata exactly as stated; to be accepted only on its own
// dimension, in its own unit and inside its own bounds; to refuse a negative
// value unless it is signed; to refuse a key with another pack identity,
// another fact identifier, no dimension, another dimension value kind or an
// extra dimension; and to be
// neither a capability constraint nor an operation argument.
func pvRequireDistinct(t *testing.T, v semreg.PackValidator, cases []pvDistinctCase) {
	t.Helper()
	units := map[semreg.DefinitionID]struct{}{"unit.other": {}}
	for _, spec := range fields {
		if spec.unit != "" {
			units[spec.unit] = struct{}{}
		}
	}
	for _, c := range cases {
		units[c.unit] = struct{}{}
	}
	indexed := map[semreg.DefinitionRef]bool{}
	for _, ref := range v.Definitions().Fields {
		indexed[ref] = true
	}
	metadata := map[semreg.DefinitionID]semreg.FieldMetadata{}
	for _, field := range Metadata().Fields {
		metadata[field.Ref.ID] = field
	}
	quantity := func(n semreg.Decimal, unit semreg.DefinitionID) *semreg.Value {
		return &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: n, Unit: unit}}
	}
	key := func(id, dimension semreg.DefinitionID) semreg.FactKey {
		return testKey(fieldSpec{id: id, dimension: dimension})
	}
	for _, c := range cases {
		spec, ok := findField(c.id)
		if !ok {
			t.Fatalf("%s is absent", c.id)
		}
		if !pvSameSpec(spec, semreg.ValueQuantity, c.unit, c.dimension, c.minimum, c.maximum) {
			t.Fatalf("%s: got %s", c.id, pvDescribe(spec))
		}
		if !indexed[definition(c.id)] {
			t.Fatalf("%s is not in the definition index", c.id)
		}
		if m, ok := metadata[c.id]; !ok || m.CanonicalUnit == nil || *m.CanonicalUnit != definition(c.unit) || m.Dimension != definition(c.dimension) {
			t.Fatalf("%s metadata: %+v", c.id, m)
		}
		own := key(c.id, c.dimension)
		for _, n := range []semreg.Decimal{c.minimum.decimal(), c.maximum.decimal()} {
			if err := v.ValidateFact(own, quantity(n, c.unit)); err != nil {
				t.Fatalf("%s %v: %v", c.id, n, err)
			}
		}
		for _, n := range []semreg.Decimal{pvOutside(c.minimum.decimal(), -1), pvOutside(c.maximum.decimal(), 1)} {
			if got := pvRefusal(v.ValidateFact(own, quantity(n, c.unit))); got != semreg.BoundsExceeded {
				t.Fatalf("%s %v: %s", c.id, n, got)
			}
		}
		negative := v.ValidateFact(own, quantity(semreg.Decimal{Coefficient: "-1"}, c.unit))
		if c.signed && negative != nil || !c.signed && pvRefusal(negative) != semreg.BoundsExceeded {
			t.Fatalf("%s negative value: %v", c.id, negative)
		}
		for dimension := range dimensions {
			if dimension == c.dimension {
				continue
			}
			if got := pvRefusal(v.ValidateFact(key(c.id, dimension), quantity(c.minimum.decimal(), c.unit))); got != semreg.InvalidValue {
				t.Fatalf("%s on %s: %s", c.id, dimension, got)
			}
		}
		for unit := range units {
			if unit == c.unit {
				continue
			}
			if got := pvRefusal(v.ValidateFact(own, quantity(c.minimum.decimal(), unit))); got != semreg.InvalidValue {
				t.Fatalf("%s in %s: %s", c.id, unit, got)
			}
		}
		text := "id"
		for name, changed := range map[string]func(*semreg.FactKey){
			"pack id":      func(k *semreg.FactKey) { k.PackID = "helianthus.pack.other" },
			"pack version": func(k *semreg.FactKey) { k.PackVersion = "1.0.1" },
			"fact id":      func(k *semreg.FactKey) { k.FactID = c.id + "_other" },
			"no dimension": func(k *semreg.FactKey) { k.Dimensions = nil },
			"dimension value kind": func(k *semreg.FactKey) {
				k.Dimensions = []semreg.Dimension{{ID: c.dimension, Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.ratio"}}}}
			},
			"extra dimension": func(k *semreg.FactKey) {
				k.Dimensions = append(k.Dimensions, semreg.Dimension{ID: "pv.dimension.system", Value: semreg.Value{Kind: semreg.ValueText, Text: &text}})
			},
		} {
			k := key(c.id, c.dimension)
			changed(&k)
			if v.ValidateFact(k, quantity(c.minimum.decimal(), c.unit)) == nil {
				t.Fatalf("%s with another %s accepted", c.id, name)
			}
		}
		symbol := semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: c.id, Token: "x", Known: true}}
		if got := pvRefusal(v.ValidateFact(own, &symbol)); got != semreg.InvalidValue {
			t.Fatalf("%s symbol: %s", c.id, got)
		}
		for id, capability := range capabilities {
			for _, constraint := range capability.constraints {
				if constraint == c.id {
					t.Fatalf("%s constrains %s", c.id, id)
				}
			}
		}
		for id, op := range operations {
			if op.argument == c.id {
				t.Fatalf("%s is the argument of %s", c.id, id)
			}
		}
	}
}

// pvSameSpec reports whether spec has exactly the given kind, unit,
// dimension and bounds and no symbols.
func pvSameSpec(spec fieldSpec, kind semreg.ValueKind, unit, dimension semreg.DefinitionID, minimum, maximum *decimalBound) bool {
	same := func(a, b *decimalBound) bool { return a != nil && b != nil && *a == *b }
	return spec.kind == kind && spec.unit == unit && spec.dimension == dimension && same(spec.minimum, minimum) && same(spec.maximum, maximum) && spec.symbols == nil
}

// pvRefusal returns the error identifier of a refusal, or "" when err is nil,
// so that an accepted value never reads as a refusal.
func pvRefusal(err error) semreg.ErrorID {
	if err == nil {
		return ""
	}
	return semreg.ErrorIdentifier(err)
}

// pvDescribe prints a field table entry with its bounds dereferenced.
func pvDescribe(spec fieldSpec) string {
	text := func(b *decimalBound) string {
		if b == nil {
			return "none"
		}
		return fmt.Sprintf("%se%d", b.coefficient, b.exponent10)
	}
	return fmt.Sprintf("%s %s %s on %s from %s to %s symbols %v", spec.id, spec.kind, spec.unit, spec.dimension, text(spec.minimum), text(spec.maximum), spec.symbols)
}

// TestPVYieldPeriodsAndActivePowerLimitRatio requires the day, month and
// year yields in joule on the system dimension with the bounds of generated
// energy, and the active-power limit ratio from 0 to 1 on the inverter
// dimension. pvRequireDistinct refuses each on every other dimension, in
// every other unit, outside its bounds, as a negative value and under a key
// with another pack id, pack version, fact id, dimension value kind, no
// dimension or an extra dimension. The lifetime energy and the watt limit
// keep their contracts, and the watt limit stays the only constraint of the
// limit capability and the only argument of the limit operation.
func TestPVYieldPeriodsAndActivePowerLimitRatio(t *testing.T) {
	v := New()
	generated, ok := findField("pv.energy.generated")
	if !ok || !pvSameSpec(generated, semreg.ValueQuantity, "unit.joule", "pv.dimension.system", bound("0", 0), bound("36", 17)) {
		t.Fatalf("pv.energy.generated changed: %s", pvDescribe(generated))
	}
	limit, ok := findField("pv.limit.active_power")
	if !ok || !pvSameSpec(limit, semreg.ValueQuantity, "unit.watt", "pv.dimension.inverter", bound("0", 0), bound("1", 7)) {
		t.Fatalf("pv.limit.active_power changed: %s", pvDescribe(limit))
	}
	pvRequireDistinct(t, v, []pvDistinctCase{
		{"pv.energy.yield_day", "unit.joule", "pv.dimension.system", generated.minimum, generated.maximum, false},
		{"pv.energy.yield_month", "unit.joule", "pv.dimension.system", generated.minimum, generated.maximum, false},
		{"pv.energy.yield_year", "unit.joule", "pv.dimension.system", generated.minimum, generated.maximum, false},
		{"pv.limit.active_power_ratio", "unit.ratio", "pv.dimension.inverter", bound("0", 0), bound("1", 0), false},
	})
	capability := capabilities["pv.capability.set_active_power_limit"]
	if capability.service != "pv.service.inverter" || len(capability.constraints) != 1 || capability.constraints[0] != "pv.limit.active_power" {
		t.Fatalf("limit capability: %+v", capability)
	}
	if op := operations["pv.operation.set_active_power_limit"]; op != (operationSpec{"pv.capability.set_active_power_limit", "pv.limit.active_power", "pv.effect.set_active_power_limit"}) {
		t.Fatalf("limit operation: %+v", op)
	}
	ratio := &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "5", Exponent10: -1}, Unit: "unit.ratio"}}
	if got := pvRefusal(v.ValidateFact(testKey(fieldSpec{id: "pv.limit.active_power", dimension: "pv.dimension.inverter"}), ratio)); got != semreg.InvalidValue {
		t.Fatalf("a ratio as the watt limit: %s", got)
	}
}
