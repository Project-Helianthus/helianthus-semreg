package roweval

import (
	"reflect"
	"strings"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Geometry cmdreg: command, operation, group, instance, register and direction.

var (
	reqSel  = cmdSel{PB: 181, SB: 36, Op: 2, Group: 1, Instance: 0, Register: 4, Direction: "controller_to_initiator"}
	measSel = cmdSel{PB: 181, SB: 36, Op: 2, Group: 1, Instance: 0, Register: 5, Direction: "target_to_initiator"}
)

func cmdExtent(s cmdSel) (int, bool) {
	switch s.Register {
	case 4:
		return 4, true
	case 5:
		return 7, true
	}
	return 0, false
}

func cmdTable() Table[cmdSel] {
	requested := func(presentation string) NativeRow[cmdSel] {
		return NativeRow[cmdSel]{ID: "cmdreg.requested", Unit: "native.celsius", SpecRef: "range-clause", At: reqSel,
			When:   When{Equal: map[string]string{"presentation": presentation}},
			Wire:   layout(0, 32, "f32", "le"),
			Domain: Bounds{Min: ptr(dec("35")), Max: ptr(dec("70"))}}
	}
	return Table[cmdSel]{
		Native: []NativeRow[cmdSel]{
			requested("controller"), requested("controller_mirror"),
			{ID: "cmdreg.measured", Unit: "native.celsius", At: measSel, Wire: layout(24, 32, "f32", "le")},
		},
		Bindings: []BindingRow{{
			NativeID: "cmdreg.requested", SubjectRole: "dhw",
			Service:    ref(thermalPack, "thermal.service.dhw"),
			Capability: ref(thermalPack, "thermal.capability.dhw_setpoint"),
			Read: &ReadRow{
				Fact:       ref(thermalPack, "thermal.setpoint.dhw_temperature_setpoint"),
				Dimensions: map[v1.DefinitionID]string{"thermal.dimension.dhw": "dhw"},
				Unit:       "unit.kelvin", Policy: "policy.synthetic",
				ToRegistry: Convert{Numeric: &Affine{Num: 1, Den: 1, Add: dec("273.15")}},
			},
		}},
	}
}

type cmdDonor struct {
	name         string
	sel          cmdSel
	presentation string
	raw          []byte
	evidence     v1.EvidenceRef
	native       *v1.Quantity // the donor's native value, when it has one
	eligible     bool
	lossKind     string
	out          *v1.Quantity // the donor's converted value, when it has one
}

func selectorOf(m map[string]any) cmdSel {
	return cmdSel{PB: byte(asInt(m["PB"])), SB: byte(asInt(m["SB"])), Op: byte(asInt(m["Opcode"])), Group: byte(asInt(m["Group"])),
		Instance: byte(asInt(m["Instance"])), Register: uint16(asInt(m["Register"])), Direction: asString(m["Direction"])}
}

func quantityOfJSON(v any) *v1.Quantity {
	if v == nil {
		return nil
	}
	n := at(v, "number").(map[string]any)
	return &v1.Quantity{Number: v1.Decimal{Coefficient: asString(n["coefficient"]), Exponent10: int32(asInt(n["exponent10"]))}, Unit: v1.DefinitionID(asString(at(v, "unit")))}
}

func loadRequested(t testing.TB, name string) cmdDonor {
	t.Helper()
	rec := readJSON(t, "cmdreg-"+name+".donor.json")
	obs := at(rec, "input", "observations").([]any)[0].(map[string]any)
	d := cmdDonor{name: name, sel: selectorOf(at(obs, "Identity", "Selector").(map[string]any)), presentation: asString(obs["Presentation"]),
		raw: b64(t, obs["RawReply"]), evidence: evidenceOf(t, obs["Evidence"].([]any)[0]), native: quantityOfJSON(at(obs, "Value", "quantity"))}
	src := at(rec, "output", "Sources").([]any)[0].(map[string]any)
	d.eligible, _ = src["Eligible"].(bool)
	if loss, _ := src["Loss"].([]any); len(loss) > 0 {
		d.lossKind = asString(loss[0].(map[string]any)["kind"])
	}
	d.out = quantityOfJSON(at(src, "Value", "quantity"))
	return d
}

func cmdAcquired(d cmdDonor, tpl v1.FactCandidate, roles Roles) Acquired[cmdSel] {
	return Acquired[cmdSel]{
		Selector: d.sel, Payload: d.raw, Qualifiers: Qualifiers{Values: map[string]string{"presentation": d.presentation}},
		Receipt:  Receipt{ReceivedAt: tpl.Times.ReceivedAt, Monotonic: tpl.Times.ReceiptMonotonic},
		Template: tpl, Roles: roles, Descriptors: testDescriptors(cmdExtent),
	}
}

func dhwKey() v1.FactKey {
	return v1.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.0.0", FactID: "thermal.setpoint.dhw_temperature_setpoint",
		Dimensions: []v1.Dimension{textDimension("thermal.dimension.dhw", "dhw")}}
}

func samePlain(q *v1.Quantity, d v1.Decimal, unit string) bool {
	return q != nil && string(q.Unit) == unit && ratOfDecimal(q.Number).Cmp(ratOfDecimal(d)) == 0
}

// The recorded old behaviour of every requested-target record. The loss kinds
// and eligibility are read from the record; the table pairs each record with
// the reason the evaluator states for the same input.
func TestCmdRegRequestedAgainstRecords(t *testing.T) {
	cases := []struct {
		name     string
		reason   Reason // "" when a candidate follows
		lossKind string // the record's own loss kind
		payload  []byte // a payload other than the record's own, with the reason
	}{
		{name: "0004-value-50"},
		{name: "0004-boundary-35"},
		{name: "0004-boundary-70"},
		{name: "0004-mirror-presentation"},
		{name: "0004-below-bound-34", reason: ReasonOutsideDomain, lossKind: "range"},
		{name: "0004-above-bound-71", reason: ReasonOutsideDomain, lossKind: "range"},
		{name: "0004-boiler-direct-presentation", reason: ReasonNoMatch, lossKind: "provenance"},
		{name: "0004-neighbouring-group-02", reason: ReasonNoMatch, lossKind: "provenance"},
		{name: "0004-neighbouring-register-0005", reason: ReasonNoMatch, lossKind: "provenance"},
		{name: "0004-neighbouring-sb-23", reason: ReasonNoMatch, lossKind: "provenance"},
		{name: "0004-short-empty-raw-reply", reason: ReasonShortPayload, lossKind: "provenance"},
		// The record's raw reply is a finite number and its absent value is a
		// recorded input member, so the not-a-number pattern stands for it.
		{name: "0004-sentinel-no-value", reason: ReasonDecode, lossKind: "unit", payload: []byte{0, 0, 0xc0, 0x7f}},
	}
	tab := cmdTable()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := loadRequested(t, c.name)
			if d.lossKind != c.lossKind {
				t.Fatalf("record loss kind %q, table %q", d.lossKind, c.lossKind)
			}
			if d.eligible != (c.reason == "") {
				t.Fatalf("record eligible %v, table reason %q", d.eligible, c.reason)
			}
			a := cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw"))
			if c.payload != nil {
				a.Payload = c.payload
			}
			got := mustEvaluate(t, tab, a)
			if got.Reason != c.reason {
				t.Fatalf("reason %q (%s), want %q", got.Reason, got.Detail, c.reason)
			}
			if c.reason != "" {
				if got.Candidate != nil || got.Subject != nil {
					t.Fatalf("candidate %v subject %v with reason %q", got.Candidate, got.Subject, c.reason)
				}
			} else {
				want := wantCandidate(a.Template, dhwKey(), &v1.Value{Kind: v1.ValueQuantity, Quantity: &v1.Quantity{Number: d.out.Number, Unit: d.out.Unit}})
				if !reflect.DeepEqual(*got.Candidate, want) {
					t.Fatalf("candidate\n got %+v\nwant %+v", *got.Candidate, want)
				}
				if got.Subject == nil || got.Subject.Role != "dhw" {
					t.Fatalf("subject %+v", got.Subject)
				}
			}
			// The donor's native value and evidence, on the fields it emits.
			if got.Native != nil && d.native != nil && got.Native.Value != nil {
				if !samePlain(d.native, got.Native.Value.Quantity.Number, string(got.Native.Value.Quantity.Unit)) {
					t.Errorf("native value %+v, donor %+v", got.Native.Value.Quantity, d.native)
				}
				if !reflect.DeepEqual(got.Native.Evidence, []v1.EvidenceRef{d.evidence}) {
					t.Errorf("native evidence %+v, donor %+v", got.Native.Evidence, d.evidence)
				}
			}
		})
	}
}

// A native zero is outside the 35 to 70 domain and has the disposition the
// records give to a value outside it.
func TestCmdRegZeroIsOutsideDomain(t *testing.T) {
	below := loadRequested(t, "0004-below-bound-34")
	if below.eligible || below.lossKind != "range" {
		t.Fatalf("record: eligible %v loss %q", below.eligible, below.lossKind)
	}
	a := cmdAcquired(below, observedTemplate("policy.synthetic", below.evidence), testRoles("dhw"))
	a.Payload = []byte{0, 0, 0, 0}
	got := mustEvaluate(t, cmdTable(), a)
	if got.Reason != ReasonOutsideDomain || got.Candidate != nil || got.Native == nil || got.Native.Value == nil {
		t.Fatalf("%+v", got)
	}
	if !sameNumber(got.Native.Value.Quantity.Number, "0") {
		t.Errorf("zero was not kept as read: %+v", got.Native.Value)
	}
}

// The independent new-binding record: 50 °C on the matching read becomes
// 323.15 K under the fact and dimension the record names, including when the
// supplied quality is unqualified or unavailable.
func TestCmdRegNewBinding(t *testing.T) {
	rec := readJSON(t, "cmdreg-0004.binding.json")
	d := loadRequested(t, "0004-value-50")
	units := map[string]string{"K": "unit.kelvin"}
	wantNumber := asString(at(rec, "expected", "value", "number"))
	wantUnit := units[asString(at(rec, "expected", "value", "unit"))]
	if wantNumber != "323.15" || wantUnit == "" {
		t.Fatalf("record %v", rec["expected"])
	}
	if got := asString(at(rec, "input", "native_value", "number")); got != "50" {
		t.Fatalf("record native value %q", got)
	}
	key := v1.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.0.0", FactID: v1.DefinitionID(asString(at(rec, "expected", "fact"))),
		Dimensions: []v1.Dimension{textDimension(asString(at(rec, "expected", "dimension")), "dhw")}}
	value := quantity(dec(wantNumber), wantUnit)

	variants := []struct {
		name  string
		apply func(*v1.Quality)
		value bool
	}{
		{"supplied quality", func(*v1.Quality) {}, true},
		{"unqualified", func(q *v1.Quality) { q.Qualification = v1.QualificationCandidate }, true},
		{"unknown qualification", func(q *v1.Quality) { q.Qualification = v1.QualificationUnknown }, true},
		{"unavailable", func(q *v1.Quality) { q.Availability = v1.AvailabilityUnavailable }, true},
		{"degraded and suspect", func(q *v1.Quality) { q.Availability = v1.AvailabilityDegraded; q.Validity = v1.ValiditySuspect }, true},
		{"unsupported", func(q *v1.Quality) { q.Qualification = v1.QualificationUnsupported }, false},
		{"rejected", func(q *v1.Quality) { q.Qualification = v1.QualificationRejected }, false},
		{"withdrawn", func(q *v1.Quality) { q.Availability = v1.AvailabilityWithdrawn }, false},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			tpl := observedTemplate("policy.synthetic", d.evidence)
			v.apply(&tpl.Quality)
			got := mustEvaluate(t, cmdTable(), cmdAcquired(d, tpl, testRoles("dhw")))
			if got.Candidate == nil {
				t.Fatalf("no candidate: %q %s", got.Reason, got.Detail)
			}
			var want v1.FactCandidate
			if v.value {
				want = wantCandidate(tpl, key, value)
			} else {
				want = wantCandidate(tpl, key, nil)
			}
			if !reflect.DeepEqual(*got.Candidate, want) {
				t.Fatalf("candidate\n got %+v\nwant %+v", *got.Candidate, want)
			}
			if err := got.Candidate.Validate(); err != nil {
				t.Fatal(err)
			}
			if !v.value && got.Native.Value == nil {
				t.Error("the native reading was not kept")
			}
			if got.Subject == nil || got.Subject.Role != "dhw" || got.Subject.ServiceInstance != "service.dhw.1" {
				t.Errorf("subject %+v", got.Subject)
			}
		})
	}
}

// The supplied template of every new-binding case agrees with the metadata
// operand on the members the operand names.
func TestMetadataOperandAgreesWithTemplate(t *testing.T) {
	op := readJSON(t, "metadata-operand.json")
	vals := at(op, "synthetic_values")
	tpl := observedTemplate("policy.synthetic")
	checks := []struct{ name, got, want string }{
		{"origin", string(tpl.Origin.OriginID), asString(at(vals, "origin"))},
		{"identity", string(tpl.CandidateID), asString(at(vals, "identity"))},
		{"assertion", string(tpl.Quality.Assertion), asString(at(vals, "quality", "assertion"))},
		{"validity", string(tpl.Quality.Validity), asString(at(vals, "quality", "validity"))},
		{"freshness", string(tpl.Quality.Freshness), asString(at(vals, "quality", "freshness"))},
		{"received", string(tpl.Times.ReceivedAt.UnixNanoseconds), asString(at(vals, "time", "received_at_ns"))},
		{"clock", string(tpl.Times.ReceivedAt.ClockID), asString(at(vals, "time", "clock_id"))},
		{"policy", string(tpl.FreshnessPolicy.PolicyID), asString(at(vals, "freshness_policy"))},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: template %q, operand %q", c.name, c.got, c.want)
		}
	}
}

// A valid 0004 read with no resolved hot-water subject is the typed
// unresolved-role input error and no candidate.
func TestCmdRegUnresolvedSubject(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	for name, roles := range map[string]Roles{
		"no roles":        {Catalog: testCatalog()},
		"another role":    testRoles("zone"),
		"no catalog":      {Subjects: testRoles("dhw").Subjects},
		"empty service":   {Subjects: map[string]RoleBinding{"dhw": {}}, Catalog: testCatalog()},
		"service missing": {Subjects: testRoles("dhw").Subjects, Catalog: testCatalog()[1:]},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Evaluate(cmdTable(), cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), roles))
			if inputKind(err) != InputUnresolvedRole {
				t.Fatalf("error %v", err)
			}
			if got.Candidate != nil || got.Subject != nil {
				t.Fatalf("candidate %v subject %v", got.Candidate, got.Subject)
			}
			if got.Native == nil || got.Native.Value == nil {
				t.Error("the native evaluation was not kept")
			}
		})
	}
}

// The register 0005 records: the donor's native value and evidence, a native
// evaluation with no binding, and no setpoint candidate.
func TestCmdRegMeasuredAgainstRecords(t *testing.T) {
	cases := []struct {
		name   string
		reason Reason
	}{
		{"0005-measured-temperature", ReasonNoBinding},
		{"0005-sentinel-nan", ReasonDecode},
		{"0005-short-measurement-reply", ReasonShortPayload},
		{"0005-neighbouring-register-0004", ReasonNoMatch},
		{"0005-version-miss-sb-23", ReasonNoMatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := readJSON(t, "cmdreg-"+c.name+".donor.json")
			attempt := at(rec, "input", "Measurement", "Attempt")
			evidence := evidenceOf(t, at(attempt, "Evidence"))
			// The reply is its three request-echo bytes and the value bytes the record
			// carries; the donor decodes the value bytes.
			reply := append(append([]byte{}, b64(t, at(attempt, "RawReply"))[:3]...), b64(t, at(attempt, "Value"))...)
			d := cmdDonor{sel: selectorOf(at(rec, "input", "Identity", "Selector").(map[string]any)), raw: reply, evidence: evidence}
			a := cmdAcquired(d, observedTemplate("policy.synthetic", evidence), testRoles("dhw"))
			got := mustEvaluate(t, cmdTable(), a)
			if got.Reason != c.reason || got.Candidate != nil {
				t.Fatalf("reason %q (%s) candidate %v", got.Reason, got.Detail, got.Candidate)
			}
			celsius := at(rec, "output", "Celsius")
			if celsius == nil {
				if got.Native != nil && got.Native.Value != nil {
					t.Errorf("the donor has no value, the evaluation has %+v", got.Native.Value)
				}
				return
			}
			if got.Native == nil || got.Native.Value == nil || !sameNumber(got.Native.Value.Quantity.Number, "45") {
				t.Fatalf("native %+v, donor %v", got.Native, celsius)
			}
			if got.Native.Value.Quantity.Unit != "native.celsius" {
				t.Errorf("unit %q", got.Native.Value.Quantity.Unit)
			}
			if !reflect.DeepEqual(got.Native.Evidence, []v1.EvidenceRef{evidence}) {
				t.Errorf("evidence %+v, donor %+v", got.Native.Evidence, evidence)
			}
		})
	}
}

// Each field of the selector, and the presentation qualifier, is varied on its
// own: any neighbour gives no candidate and no match. Operation 06 is not a
// fallback for operation 02.
func TestCmdRegNeighbouringSelector(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	mutations := map[string]func(*cmdSel){
		"command primary byte":   func(s *cmdSel) { s.PB++ },
		"command secondary byte": func(s *cmdSel) { s.SB-- },
		"operation 06":           func(s *cmdSel) { s.Op = 6 },
		"group":                  func(s *cmdSel) { s.Group = 2 },
		"instance":               func(s *cmdSel) { s.Instance = 1 },
		"register":               func(s *cmdSel) { s.Register = 5 },
		"opposite direction":     func(s *cmdSel) { s.Direction = "target_to_initiator" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			a := cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw"))
			mutate(&a.Selector)
			got := mustEvaluate(t, cmdTable(), a)
			if got.Reason != ReasonNoMatch || got.Candidate != nil || got.Native != nil {
				t.Fatalf("%+v", got)
			}
		})
	}
	for _, presentation := range []string{"boiler_direct", "Controller", "controller_mirror2"} {
		a := cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw"))
		a.Qualifiers.Values["presentation"] = presentation
		if got := mustEvaluate(t, cmdTable(), a); got.Reason != ReasonNoMatch || got.Candidate != nil {
			t.Errorf("presentation %q: %+v", presentation, got)
		}
	}
	// A qualifier that was not supplied is not a wildcard.
	a := cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw"))
	a.Qualifiers = Qualifiers{}
	got := mustEvaluate(t, cmdTable(), a)
	if got.Reason != ReasonNoMatch || !strings.Contains(got.Detail, "presentation") {
		t.Errorf("missing presentation: %+v", got)
	}
}

// The fact key is built from every member of the read row's fact definition and
// each declared dimension; each member is varied on its own.
func TestCmdRegFactKeyMembers(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	otherPack := v1.PackRef{ID: "helianthus.pack.other", Version: "1.0.0"}
	laterPack := v1.PackRef{ID: "helianthus.pack.thermal", Version: "1.1.0"}
	fact := ref(thermalPack, "thermal.setpoint.dhw_temperature_setpoint")
	cases := []struct {
		name string
		fact v1.DefinitionRef
		dims map[v1.DefinitionID]string
		want v1.FactKey
	}{
		{"pack id", v1.DefinitionRef{Pack: otherPack, ID: fact.ID, Version: "1.0.0"}, map[v1.DefinitionID]string{"thermal.dimension.dhw": "dhw"},
			v1.FactKey{PackID: "helianthus.pack.other", PackVersion: "1.0.0", FactID: fact.ID, Dimensions: []v1.Dimension{textDimension("thermal.dimension.dhw", "dhw")}}},
		{"pack version", v1.DefinitionRef{Pack: laterPack, ID: fact.ID, Version: "1.0.0"}, map[v1.DefinitionID]string{"thermal.dimension.dhw": "dhw"},
			v1.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.1.0", FactID: fact.ID, Dimensions: []v1.Dimension{textDimension("thermal.dimension.dhw", "dhw")}}},
		{"fact id", v1.DefinitionRef{Pack: thermalPack, ID: "thermal.setpoint.other", Version: "1.0.0"}, map[v1.DefinitionID]string{"thermal.dimension.dhw": "dhw"},
			v1.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.0.0", FactID: "thermal.setpoint.other", Dimensions: []v1.Dimension{textDimension("thermal.dimension.dhw", "dhw")}}},
		{"dimension id", fact, map[v1.DefinitionID]string{"thermal.dimension.zone": "dhw"},
			v1.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.0.0", FactID: fact.ID, Dimensions: []v1.Dimension{textDimension("thermal.dimension.zone", "dhw")}}},
		{"dimension role", fact, map[v1.DefinitionID]string{"thermal.dimension.dhw": "zone"},
			v1.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.0.0", FactID: fact.ID, Dimensions: []v1.Dimension{textDimension("thermal.dimension.dhw", "zone")}}},
		{"two dimensions sorted by id", fact, map[v1.DefinitionID]string{"thermal.dimension.zone": "zone", "thermal.dimension.dhw": "dhw"},
			v1.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.0.0", FactID: fact.ID,
				Dimensions: []v1.Dimension{textDimension("thermal.dimension.dhw", "dhw"), textDimension("thermal.dimension.zone", "zone")}}},
		{"no dimension", fact, nil,
			v1.FactKey{PackID: "helianthus.pack.thermal", PackVersion: "1.0.0", FactID: fact.ID, Dimensions: []v1.Dimension{}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tab := cmdTable()
			tab.Bindings[0].Read.Fact, tab.Bindings[0].Read.Dimensions = c.fact, c.dims
			roles := testRoles("dhw", "zone")
			roles.Catalog = append(roles.Catalog, v1.DefinitionIndex{Pack: c.fact.Pack, Fields: []v1.DefinitionRef{c.fact}})
			got := mustEvaluate(t, tab, cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), roles))
			if got.Candidate == nil {
				t.Fatalf("%q %s", got.Reason, got.Detail)
			}
			if !reflect.DeepEqual(got.Candidate.Key, c.want) {
				t.Fatalf("key\n got %+v\nwant %+v", got.Candidate.Key, c.want)
			}
		})
	}
	t.Run("slot", func(t *testing.T) {
		tab := cmdTable()
		tab.Bindings[0].Read.Dimensions = map[v1.DefinitionID]string{"thermal.dimension.dhw": "circuit"}
		a := cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw"))
		a.Coordinates = map[string]string{"circuit": "2"}
		got := mustEvaluate(t, tab, a)
		if got.Candidate == nil || !reflect.DeepEqual(got.Candidate.Key.Dimensions, []v1.Dimension{textDimension("thermal.dimension.dhw", "2")}) {
			t.Fatalf("%+v", got)
		}
		a.Coordinates = nil
		if _, err := Evaluate(tab, a); inputKind(err) != InputUnresolvedSlot {
			t.Errorf("unbound slot: %v", err)
		}
	})
}
