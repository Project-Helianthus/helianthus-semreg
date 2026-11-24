package catalog

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

const (
	packEVSE           = "helianthus.pack.evse@1.0.0"
	packInfrastructure = "helianthus.pack.infrastructure@1.0.0"
	packPV             = "helianthus.pack.pv@1.0.0"
	packStorage        = "helianthus.pack.storage@1.1.0"
	packThermal        = "helianthus.pack.thermal@1.0.0"
)

// packDocument returns the catalog document text holding the required
// infrastructure family and one test-local family whose semantic_pack is pack.
// The test-local row comes first, so the anchor of withFurther always finds it.
// Both rows are independent of any driver family.
func packDocument(t *testing.T, pack string) string {
	t.Helper()
	c, _ := load(t)
	row := Family{
		FamilyID: "pack-test-family", Name: "Pack test family", SemanticPack: pack,
		State: semreg.QualificationUnknown, Composition: CompositionNotComposed, Reason: ReasonNoProducer,
		Evidence:     []Evidence{{Owner: "Project-Helianthus/example-reg@v1.0.0", Kind: "semantic_contract", Locator: "docs/packs.md", Statement: "a test-local row"}},
		Verification: VerificationNotTestedOnHardware,
	}
	c.Families = append([]Family{row}, c.Families...)
	return string(encodeIndented(t, c, ""))
}

// withFurther inserts a further_semantic_packs member after the test-local row
// whose semantic_pack equals pack.
func withFurther(t *testing.T, pack, value string) string {
	t.Helper()
	text := packDocument(t, pack)
	anchor := `"semantic_pack": "` + pack + `",`
	if !strings.Contains(text, anchor) {
		t.Fatalf("no row with semantic_pack %q", pack)
	}
	return strings.Replace(text, anchor, anchor+"\n      \"further_semantic_packs\": "+value+",", 1)
}

func quoted(packs ...string) string {
	b, _ := json.Marshal(packs)
	return string(b)
}

func familyByID(t *testing.T, c Catalog, id string) Family {
	t.Helper()
	for _, f := range c.Families {
		if f.FamilyID == id {
			return f
		}
	}
	t.Fatalf("family %s missing", id)
	return Family{}
}

func TestFurtherSemanticPacksDecode(t *testing.T) {
	cases := []struct {
		name, pack string
		further    []string
	}{
		{"two further packs", packInfrastructure, []string{packPV, packStorage}},
		{"one further pack", packEVSE, []string{packInfrastructure}},
		{"four further packs", packEVSE, []string{packInfrastructure, packPV, packStorage, packThermal}},
		{"five further packs, six accepted packs", packEVSE, []string{packInfrastructure, packPV, packStorage, packThermal, "helianthus.pack.zz@1.0.0"}},
	}
	for _, tc := range cases {
		doc := withFurther(t, tc.pack, quoted(tc.further...))
		c, err := Decode([]byte(doc))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		f := familyByID(t, c, "pack-test-family")
		if err := f.Validate(); err != nil {
			t.Errorf("%s: Family.Validate: %v", tc.name, err)
		}
		if !reflect.DeepEqual(f.FurtherSemanticPacks, tc.further) {
			t.Errorf("%s: further = %v", tc.name, f.FurtherSemanticPacks)
		}
		wantPacks := append([]string{tc.pack}, tc.further...)
		if got := f.Packs(); !reflect.DeepEqual(got, wantPacks) {
			t.Errorf("%s: Packs() = %v, want %v", tc.name, got, wantPacks)
		}
		encoded, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(encoded, []byte(`"further_semantic_packs":`+quoted(tc.further...))) {
			t.Errorf("%s: re-encoding drops the member: %s", tc.name, encoded)
		}
		built, _ := load(t)
		row := f
		row.FurtherSemanticPacks = append([]string(nil), tc.further...)
		built.Families = append([]Family{row}, built.Families...)
		want := encodeIndented(t, built, "")
		decoded, err := Decode(want)
		if err != nil {
			t.Fatalf("%s: canonical document: %v", tc.name, err)
		}
		if got := encodeIndented(t, decoded, ""); !bytes.Equal(got, want) {
			t.Errorf("%s: document does not re-encode to the same bytes", tc.name)
		}
	}
}

func TestFurtherSemanticPacksRejected(t *testing.T) {
	text := packDocument(t, packInfrastructure)
	cases := []struct {
		name, doc, want string
	}{
		{"present empty list", withFurther(t, packInfrastructure, `[]`), "non-canonical or missing member name"},
		{"null", withFurther(t, packInfrastructure, `null`), "non-canonical or missing member name"},
		{"duplicate entry", withFurther(t, packInfrastructure, quoted(packPV, packPV)), "repeats"},
		{"entry equal to semantic_pack", withFurther(t, packInfrastructure, quoted(packInfrastructure)), "equals semantic_pack"},
		{"unsorted list", withFurther(t, packInfrastructure, quoted(packStorage, packPV)), "canonical order"},
		{"entry sorting before semantic_pack", withFurther(t, packPV, quoted(packInfrastructure)), "canonical order"},
		{"empty semantic_pack", withFurther(t, "", quoted(packPV)), "empty semantic_pack"},
		{"entry without version", withFurther(t, packInfrastructure, quoted("helianthus.pack.pv")), "PackRef"},
		{"entry with bad version", withFurther(t, packInfrastructure, quoted("helianthus.pack.pv@one")), "PackRef"},
		{"empty entry", withFurther(t, packInfrastructure, quoted("")), "PackRef"},
		{"non-string entry", withFurther(t, packInfrastructure, `[1]`), "cannot unmarshal"},
		{"case-variant member", strings.Replace(withFurther(t, packInfrastructure, quoted(packPV)), `"further_semantic_packs"`, `"Further_Semantic_Packs"`, 1), "non-canonical or missing member name"},
		{"repeated member", strings.Replace(withFurther(t, packInfrastructure, quoted(packPV)), `"further_semantic_packs": ["helianthus.pack.pv@1.0.0"],`, `"further_semantic_packs": ["helianthus.pack.pv@1.0.0"], "further_semantic_packs": ["helianthus.pack.storage@1.1.0"],`, 1), "duplicate member"},
	}
	for _, tc := range cases {
		if tc.doc == text {
			t.Fatalf("%s: mutation did not apply", tc.name)
		}
		_, err := Decode([]byte(tc.doc))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestFurtherSemanticPacksValidateStructRules(t *testing.T) {
	c, _ := load(t)
	i := -1
	for j, f := range c.Families {
		if f.FamilyID == "infrastructure" {
			i = j
		}
	}
	if i < 0 {
		t.Fatal("infrastructure row missing")
	}
	for name, tc := range map[string]struct {
		semantic string
		further  []string
		want     string
	}{
		"present empty list":  {packInfrastructure, []string{}, "present but empty"},
		"empty semantic_pack": {"", []string{packPV}, "empty semantic_pack"},
		"equal to semantic":   {packInfrastructure, []string{packInfrastructure}, "equals semantic_pack"},
		"duplicate":           {packInfrastructure, []string{packPV, packStorage, packPV}, "repeats"},
		"unsorted":            {packInfrastructure, []string{packStorage, packPV}, "canonical order"},
		"before semantic":     {packPV, []string{packInfrastructure}, "canonical order"},
	} {
		clone := c
		clone.Families = append([]Family(nil), c.Families...)
		clone.Families[i].SemanticPack = tc.semantic
		clone.Families[i].FurtherSemanticPacks = tc.further
		if err := clone.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestPacksAccessor(t *testing.T) {
	if got := (Family{}).Packs(); len(got) != 0 {
		t.Errorf("row without packs: %v", got)
	}
	if got := (Family{SemanticPack: packPV}).Packs(); !reflect.DeepEqual(got, []string{packPV}) {
		t.Errorf("single pack: %v", got)
	}
	f := Family{SemanticPack: packInfrastructure, FurtherSemanticPacks: []string{packPV, packStorage}}
	got := f.Packs()
	got[1] = "mutated"
	if f.FurtherSemanticPacks[0] != packPV {
		t.Error("Packs aliases the row's further packs")
	}
}

// TestExistingRowsKeepTheirBytes re-encodes the decoded fixture with the
// fixture's own indentation and requires every row, and the whole document,
// to reproduce the committed bytes.
func TestExistingRowsKeepTheirBytes(t *testing.T) {
	c, raw := load(t)
	encode := func(v any, prefix string) []byte {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent(prefix, "  ")
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	for _, f := range c.Families {
		if f.FurtherSemanticPacks != nil {
			continue
		}
		row := append([]byte("    "), bytes.TrimSuffix(encode(f, "    "), []byte("\n"))...)
		if !bytes.Contains(raw, row) {
			t.Errorf("%s: row does not re-encode to its committed bytes", f.FamilyID)
		}
	}
	if !bytes.Equal(encode(c, ""), raw) {
		t.Error("catalog does not re-encode to its committed bytes")
	}
}
