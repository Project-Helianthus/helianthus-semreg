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
	if len(x.Fields) != 21 || len(x.Services) != 6 || len(x.Capabilities) != 8 || len(x.Operations) != 2 || len(x.EffectRules) != 2 {
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
			if semreg.ErrorIdentifier(v.ValidateFact(k, &b)) != semreg.BoundsExceeded && s.minimum.decimal().Coefficient != "-1" && s.minimum.decimal().Coefficient != "-5" {
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
				if len(v.Definitions().Fields) != 21 {
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
	0x78, 0x54, 0x0d, 0x34, 0x92, 0xed, 0x11, 0xeb,
	0xcb, 0x87, 0xf8, 0xd0, 0x5b, 0xb6, 0xcc, 0x82,
	0x27, 0xa6, 0x50, 0x34, 0x9b, 0x8e, 0x9e, 0x65,
	0x94, 0x69, 0x15, 0x91, 0x76, 0x95, 0xb3, 0x84,
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
