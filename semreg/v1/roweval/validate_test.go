package roweval

import (
	"errors"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

func tableReason(err error) Reason {
	var te *TableError
	if errors.As(err, &te) {
		return te.Reason
	}
	return ""
}

// The tables of the three geometries validate against the descriptors and roles
// their drivers supply: the identifiers they name are registered.
func TestValidateTableAcceptsTheThreeGeometries(t *testing.T) {
	if err := ValidateTable(cmdTable(), testDescriptors(cmdExtent), testRoles("dhw")); err != nil {
		t.Errorf("command geometry: %v", err)
	}
	if err := ValidateTable(blkTable(), testDescriptors(blkExtent), testRoles("inverter")); err != nil {
		t.Errorf("block geometry: %v", err)
	}
	if err := ValidateTable(frameTable(), testDescriptors(frmExtent), testRoles("pack")); err != nil {
		t.Errorf("frame geometry: %v", err)
	}
	if err := ValidateTable(inverseTable(), testDescriptors(fixtureExtent), testRoles("zone", "readonly")); err != nil {
		t.Errorf("inverse fixture: %v", err)
	}
}

// An identifier that the driver's descriptors do not know fails validation, and
// the package holds no set of its own to fall back to.
func TestValidateTableRejectsUnknownIdentifiers(t *testing.T) {
	for name, mutate := range map[string]func(*Layout){
		"codec":      func(l *Layout) { l.Codec = "f64" },
		"byte order": func(l *Layout) { l.ByteOrder = "middle" },
		"word order": func(l *Layout) { l.WordOrder = "" },
		"bit order":  func(l *Layout) { l.BitOrder = "lsb0" },
	} {
		tab := cmdTable()
		mutate(&tab.Native[0].Wire)
		if got := tableReason(ValidateTable(tab, testDescriptors(cmdExtent), testRoles("dhw"))); got != ReasonUnknownID {
			t.Errorf("%s: %q", name, got)
		}
	}
	// the identifiers are those the driver registers, not ones this package knows
	empty := Descriptors[cmdSel]{}
	if got := tableReason(ValidateTable(cmdTable(), empty, testRoles("dhw"))); got != ReasonUnknownID {
		t.Errorf("no descriptors: %q", got)
	}
	// and evaluation does not use an identifier that is unknown
	a := cmdAcquired(loadRequested(t, "0004-value-50"), observedTemplate("policy.synthetic", ev(t, "x")), testRoles("dhw"))
	a.Descriptors.Codecs = nil
	if got := mustEvaluate(t, cmdTable(), a); got.Reason != ReasonUnknownID || got.Candidate != nil {
		t.Errorf("%+v", got)
	}
}

func TestValidateTableAmbiguousMatch(t *testing.T) {
	row := func(id string, w When) NativeRow[cmdSel] {
		return NativeRow[cmdSel]{ID: id, Unit: "u", At: reqSel, When: w, Wire: layout(0, 32, "f32", "le")}
	}
	ver := func(min, before uint16) When {
		return When{Versions: []VersionRange{{Axis: "api", Min: []uint16{min}, Before: []uint16{before}}}}
	}
	cases := []struct {
		name    string
		a, b    When
		ambig   bool
		sameID  bool
		otherAt bool
	}{
		{"no conditions", When{}, When{}, true, false, false},
		{"equal differs", When{Equal: map[string]string{"k": "1"}}, When{Equal: map[string]string{"k": "2"}}, false, false, false},
		{"equal on different keys", When{Equal: map[string]string{"k": "1"}}, When{Equal: map[string]string{"j": "2"}}, true, false, false},
		{"versions disjoint", ver(1, 3), ver(3, 5), false, false, false},
		{"versions overlap", ver(1, 4), ver(3, 5), true, false, false},
		{"versions on different axes", ver(1, 3), When{Versions: []VersionRange{{Axis: "hw", Min: []uint16{1}, Before: []uint16{3}}}}, true, false, false},
		{"requires against forbids", When{Requires: []string{"f"}}, When{Forbids: []string{"f"}}, false, false, false},
		{"forbids against requires", When{Forbids: []string{"f"}}, When{Requires: []string{"f"}}, false, false, false},
		{"requires both", When{Requires: []string{"f"}}, When{Requires: []string{"f"}}, true, false, false},
		{"requires different", When{Requires: []string{"f"}}, When{Requires: []string{"g"}}, true, false, false},
		{"same id elsewhere, exclusive", When{Equal: map[string]string{"k": "1"}}, When{Equal: map[string]string{"k": "2"}}, false, true, true},
		{"same id elsewhere, overlapping", When{}, When{}, true, true, true},
		{"other id elsewhere", When{}, When{}, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := row("first", c.a), row("second", c.b)
			if c.sameID {
				b.ID = "first"
			}
			if c.otherAt {
				b.At.Register = 9
			}
			tab := Table[cmdSel]{Native: []NativeRow[cmdSel]{a, b}}
			got := tableReason(ValidateTable(tab, testDescriptors(cmdExtent), Roles{}))
			if c.ambig != (got == ReasonAmbiguous) {
				t.Errorf("reason %q, ambiguous %v", got, c.ambig)
			}
		})
	}
	// and at evaluation, two matching rows give no candidate and the reason
	tab := Table[cmdSel]{Native: []NativeRow[cmdSel]{row("first", When{}), row("second", When{})}}
	a := cmdAcquired(loadRequested(t, "0004-value-50"), observedTemplate("policy.synthetic", ev(t, "x")), testRoles("dhw"))
	if got := mustEvaluate(t, tab, a); got.Reason != ReasonAmbiguous || got.Candidate != nil || got.Native != nil {
		t.Errorf("%+v", got)
	}
}

func TestValidateTableTwoReadBindingsOfOneRow(t *testing.T) {
	tab := cmdTable()
	tab.Bindings = append(tab.Bindings, tab.Bindings[0])
	if got := tableReason(ValidateTable(tab, testDescriptors(cmdExtent), testRoles("dhw"))); got != ReasonAmbiguous {
		t.Errorf("%q", got)
	}
	tab.Bindings[1].When = When{Requires: []string{"x"}}
	tab.Bindings[0].When = When{Forbids: []string{"x"}}
	if err := ValidateTable(tab, testDescriptors(cmdExtent), testRoles("dhw")); err != nil {
		t.Errorf("exclusive bindings: %v", err)
	}
	// and at evaluation
	tab.Bindings[0].When, tab.Bindings[1].When = When{}, When{}
	a := cmdAcquired(loadRequested(t, "0004-value-50"), observedTemplate("policy.synthetic", ev(t, "x")), testRoles("dhw"))
	if got := mustEvaluate(t, tab, a); got.Reason != ReasonAmbiguous || got.Candidate != nil || got.Native == nil {
		t.Errorf("%+v", got)
	}
}

func TestValidateTableExtentOutsideThePayload(t *testing.T) {
	for name, l := range map[string]Layout{
		"starts after the end": layout(32, 8, "u", "le"),
		"ends after the end":   layout(8, 32, "f32", "le"),
		"width zero":           layout(0, 0, "u", "le"),
		"width above 64":       layout(0, 65, "u", "le"),
	} {
		tab := cmdTable()
		tab.Native[0].Wire = l
		if got := tableReason(ValidateTable(tab, testDescriptors(cmdExtent), testRoles("dhw"))); got != ReasonExtent {
			t.Errorf("%s: %q", name, got)
		}
	}
	// the last bit of the last byte fits; a selector that fixes no length is
	// checked against the payload when it is evaluated
	tab := cmdTable()
	tab.Native[0].Wire = layout(31, 1, "u", "le")
	if err := ValidateTable(tab, testDescriptors(cmdExtent), testRoles("dhw")); err != nil {
		t.Error(err)
	}
	tab.Native[0].Wire = layout(900, 8, "u", "le")
	if err := ValidateTable(tab, testDescriptors[cmdSel](func(cmdSel) (int, bool) { return 0, false }), testRoles("dhw")); err != nil {
		t.Errorf("unknown length: %v", err)
	}
}

func TestValidateTableCyclicExponentRows(t *testing.T) {
	row := func(id, exponent string) NativeRow[cmdSel] {
		r := NativeRow[cmdSel]{ID: id, Unit: "u", At: cmdSel{Register: uint16(len(id))}, Wire: layout(0, 8, "s", "le")}
		if exponent != "" {
			r.Decode.Numeric = &Affine{Num: 1, Den: 1, ExponentRow: exponent}
		}
		return r
	}
	d := testDescriptors(func(cmdSel) (int, bool) { return 8, true })
	for name, rows := range map[string][]NativeRow[cmdSel]{
		"itself":    {row("a", "a")},
		"two rows":  {row("a", "bb"), row("bb", "a")},
		"three":     {row("a", "bb"), row("bb", "ccc"), row("ccc", "a")},
		"not first": {row("a", "bb"), row("bb", "ccc"), row("ccc", "bb")},
	} {
		if got := tableReason(ValidateTable(Table[cmdSel]{Native: rows}, d, Roles{})); got != ReasonCyclicExponent {
			t.Errorf("%s: %q", name, got)
		}
	}
	if err := ValidateTable(Table[cmdSel]{Native: []NativeRow[cmdSel]{row("a", "bb"), row("bb", "ccc"), row("ccc", "")}}, d, Roles{}); err != nil {
		t.Errorf("a chain that ends: %v", err)
	}
	if got := tableReason(ValidateTable(Table[cmdSel]{Native: []NativeRow[cmdSel]{row("a", "missing")}}, d, Roles{})); got != ReasonUnresolvedDef {
		t.Errorf("a row that is not there: %q", got)
	}
}

// A catalog role that does not resolve is rejected; a resolved one is complete.
func TestValidateTableRoleResolution(t *testing.T) {
	thermalOnly := func(f func(*v1.DefinitionIndex)) []v1.DefinitionIndex {
		c := testCatalog()
		f(&c[0])
		return c
	}
	cases := []struct {
		name  string
		roles Roles
		ok    bool
	}{
		{"complete", testRoles("zone", "readonly"), true},
		{"role not bound", testRoles("readonly"), false},
		{"role bound to no instance", Roles{Subjects: map[string]RoleBinding{"zone": {}, "readonly": {ServiceInstance: "service.r.1"}}, Catalog: testCatalog()}, false},
		{"no catalog", Roles{Subjects: testRoles("zone", "readonly").Subjects}, false},
		{"service not in the catalog", Roles{Subjects: testRoles("zone", "readonly").Subjects, Catalog: thermalOnly(func(i *v1.DefinitionIndex) { i.Services = nil })}, false},
		{"capability not in the catalog", Roles{Subjects: testRoles("zone", "readonly").Subjects, Catalog: thermalOnly(func(i *v1.DefinitionIndex) { i.Capabilities = nil })}, false},
		{"operation not in the catalog", Roles{Subjects: testRoles("zone", "readonly").Subjects, Catalog: thermalOnly(func(i *v1.DefinitionIndex) { i.Operations = nil })}, false},
		{"service of another id", Roles{Subjects: testRoles("zone", "readonly").Subjects, Catalog: thermalOnly(func(i *v1.DefinitionIndex) {
			i.Services = []v1.DefinitionRef{ref(thermalPack, "thermal.service.other")}
		})}, false},
		{"service of another pack version", Roles{Subjects: testRoles("zone", "readonly").Subjects, Catalog: thermalOnly(func(i *v1.DefinitionIndex) {
			i.Services = []v1.DefinitionRef{{Pack: v1.PackRef{ID: thermalPack.ID, Version: "2.0.0"}, ID: "thermal.service.zone", Version: "1.0.0"}}
		})}, false},
		{"service of another pack id", Roles{Subjects: testRoles("zone", "readonly").Subjects, Catalog: thermalOnly(func(i *v1.DefinitionIndex) {
			i.Services = []v1.DefinitionRef{{Pack: v1.PackRef{ID: "helianthus.pack.other", Version: thermalPack.Version}, ID: "thermal.service.zone", Version: "1.0.0"}}
		})}, false},
		{"service of another version", Roles{Subjects: testRoles("zone", "readonly").Subjects, Catalog: thermalOnly(func(i *v1.DefinitionIndex) {
			i.Services = []v1.DefinitionRef{{Pack: thermalPack, ID: "thermal.service.zone", Version: "2.0.0"}}
		})}, false},
	}
	for _, c := range cases {
		err := ValidateTable(inverseTable(), testDescriptors(fixtureExtent), c.roles)
		if c.ok != (err == nil) || (!c.ok && tableReason(err) != ReasonUnresolvedRole) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// a read row's fact has to be in the catalog as well
	c := testCatalog()
	c[0].Fields = nil
	if got := tableReason(ValidateTable(cmdTable(), testDescriptors(cmdExtent), Roles{Subjects: testRoles("dhw").Subjects, Catalog: c})); got != ReasonUnresolvedRole {
		t.Errorf("fact: %q", got)
	}
}

func TestValidateTableConversions(t *testing.T) {
	for name, mutate := range map[string]func(*Table[cmdSel]){
		"zero denominator": func(t *Table[cmdSel]) { t.Bindings[0].Read.ToRegistry.Numeric.Den = 0 },
		"bad addend":       func(t *Table[cmdSel]) { t.Bindings[0].Read.ToRegistry.Numeric.Add = v1.Decimal{Coefficient: "10"} },
		"unknown rounding": func(t *Table[cmdSel]) { t.Bindings[0].Read.ToRegistry.Round = "nearest" },
		"hook not there":   func(t *Table[cmdSel]) { t.Bindings[0].Read.ToRegistry.Hook = "nothing" },
		"decode hook":      func(t *Table[cmdSel]) { t.Native[0].Decode.Hook = "nothing" },
	} {
		tab := cmdTable()
		mutate(&tab)
		if err := ValidateTable(tab, testDescriptors(cmdExtent), testRoles("dhw")); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
