package roweval

import (
	"bytes"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// The inverse fixtures are artificial: the 5 to 30 native bounds belong to no
// real target and are not a source for one.

var (
	targetSel = cmdSel{PB: 1, SB: 2, Op: 3, Group: 4, Instance: 0, Register: 9, Direction: "initiator_to_target"}
	setpoint  = ref(thermalPack, "thermal.operation.set_zone_setpoint")
)

const argID = "thermal.argument.setpoint"

func fixtureExtent(cmdSel) (int, bool) { return 4, true }

func inverseTable() Table[cmdSel] {
	return Table[cmdSel]{
		Native: []NativeRow[cmdSel]{
			{ID: "inverse.target", Unit: "native.celsius", At: targetSel, Wire: layout(0, 32, "f32", "le")},
			{ID: "inverse.read", Unit: "native.celsius", At: cmdSel{PB: 1, SB: 2, Op: 3, Group: 4, Register: 8, Direction: "target_to_initiator"}, Wire: layout(0, 32, "f32", "le")},
		},
		Bindings: []BindingRow{
			{NativeID: "inverse.read", SubjectRole: "zone", Service: ref(thermalPack, "thermal.service.zone"), Capability: ref(thermalPack, "thermal.capability.zone_setpoint"),
				Write: &WriteRow{Operation: setpoint, Argument: argID, TargetRow: "inverse.target", ReadbackRow: "inverse.read",
					Inverse:      Convert{Numeric: &Affine{Num: 1, Den: 1, Add: dec("-273.15")}},
					NativeBounds: Bounds{Min: ptr(dec("5")), Max: ptr(dec("30"))}}},
			{NativeID: "inverse.read", SubjectRole: "readonly", Service: ref(thermalPack, "thermal.service.zone")},
		},
	}
}

func writing(kelvin string, limits *driver.ValueLimits) Writing[cmdSel] {
	return Writing[cmdSel]{NativeID: "inverse.read", SubjectRole: "zone", Operation: setpoint,
		Argument:    v1.TypedField{ID: argID, Value: *quantity(dec(kelvin), "unit.kelvin")},
		Limits:      limits,
		Descriptors: testDescriptors(fixtureExtent)}
}

func kelvinLimits(origin driver.LimitOrigin, min, max, step string) *driver.ValueLimits {
	q := func(s string) *v1.Quantity {
		if s == "" {
			return nil
		}
		return &v1.Quantity{Number: dec(s), Unit: "unit.kelvin"}
	}
	return &driver.ValueLimits{Min: q(min), Max: q(max), Step: q(step), Origin: origin, Evidence: []v1.EvidenceRef{ev(nil, "limit")}}
}

// donorPayload is the raw reply of a recorded requested-target observation: the
// bytes the old path read for the temperature the record names.
func donorPayload(t testing.TB, name string) []byte {
	t.Helper()
	return loadRequested(t, name).raw
}

func mustInvert(t testing.TB, tab Table[cmdSel], w Writing[cmdSel]) WriteResult {
	t.Helper()
	r, err := Invert(tab, w)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func wantPayload(t testing.TB, r WriteResult, want []byte) {
	t.Helper()
	if r.Reason != "" || !bytes.Equal(r.Payload, want) {
		t.Fatalf("payload %x reason %q (%s), want %x", r.Payload, r.Reason, r.Detail, want)
	}
	if r.Target != "inverse.target" || r.Readback != "inverse.read" {
		t.Errorf("target %q readback %q", r.Target, r.Readback)
	}
}

func wantNone(t testing.TB, r WriteResult, reason Reason) {
	t.Helper()
	if r.Payload != nil || r.Reason != reason {
		t.Fatalf("payload %x reason %q (%s), want none with %q", r.Payload, r.Reason, r.Detail, reason)
	}
}

// (a) A target with no limit query: the native bounds apply.
func TestInverseNativeBoundsWhenNoLimitQuery(t *testing.T) {
	tab := inverseTable()
	// 293.15 K reaches the encoder as 20 degrees: float32 20 is 0x41a00000.
	r := mustInvert(t, tab, writing("293.15", nil))
	wantPayload(t, r, []byte{0x00, 0x00, 0xa0, 0x41})
	if r.Native == nil || !sameNumber(*r.Native, "20") {
		t.Errorf("native %+v", r.Native)
	}
	// 400 K is 126.85 degrees, outside 5 to 30.
	r = mustInvert(t, tab, writing("400", nil))
	wantNone(t, r, ReasonOutsideDomain)
	// each side of each native bound
	for _, c := range []struct {
		kelvin string
		want   []byte // float32 bytes, little endian; nil when outside
	}{
		{"278.1", nil}, {"278.15", []byte{0, 0, 0xa0, 0x40}}, // 5 degrees is 0x40a00000
		{"303.15", []byte{0, 0, 0xf0, 0x41}}, {"303.2", nil}, // 30 degrees is 0x41f00000
	} {
		r := mustInvert(t, tab, writing(c.kelvin, nil))
		if c.want == nil {
			wantNone(t, r, ReasonOutsideDomain)
		} else {
			wantPayload(t, r, c.want)
		}
	}
	// a step of the native bounds counts from the minimum
	stepped := inverseTable()
	stepped.Bindings[0].Write.NativeBounds.Step = ptr(dec("0.5"))
	wantPayload(t, mustInvert(t, stepped, writing("293.15", nil)), []byte{0, 0, 0xa0, 0x41})
	wantNone(t, mustInvert(t, stepped, writing("293.35", nil)), ReasonOffStep)
}

// (b) A published device_query limit wider than the native bounds is kept.
func TestInversePublishedLimitApplies(t *testing.T) {
	tab := inverseTable()
	limits := kelvinLimits(driver.LimitDeviceQuery, "278.15", "313.15", "")
	// 308.15 K is 35 degrees, which is outside 5 to 30; the bytes are those the
	// record of the 35 degree observation holds.
	r := mustInvert(t, tab, writing("308.15", limits))
	wantPayload(t, r, donorPayload(t, "0004-boundary-35"))
	if r.Native == nil || !sameNumber(*r.Native, "35") {
		t.Errorf("native %+v", r.Native)
	}
	// 313.15 K is 40 degrees (float32 0x42200000) and is the published maximum.
	wantPayload(t, mustInvert(t, tab, writing("313.15", limits)), []byte{0, 0, 0x20, 0x42})
	// beyond the published limits, in kelvin, before any conversion
	wantNone(t, mustInvert(t, tab, writing("313.2", limits)), ReasonOutsideDomain)
	wantNone(t, mustInvert(t, tab, writing("278.1", limits)), ReasonOutsideDomain)
	// the published limit is not the narrower of the two
	narrow := kelvinLimits(driver.LimitDeviceQuery, "", "283.15", "")
	wantNone(t, mustInvert(t, tab, writing("293.15", narrow)), ReasonOutsideDomain)
	// its step counts from its minimum, in its own unit
	stepped := kelvinLimits(driver.LimitDeviceQuery, "278.15", "313.15", "0.5")
	wantPayload(t, mustInvert(t, tab, writing("308.15", stepped)), donorPayload(t, "0004-boundary-35"))
	wantNone(t, mustInvert(t, tab, writing("308.3", stepped)), ReasonOffStep)
	// and from zero when it has no minimum, whatever the origin of the limit
	zero := kelvinLimits(driver.LimitSpecification, "", "313.15", "0.05")
	wantPayload(t, mustInvert(t, tab, writing("308.15", zero)), donorPayload(t, "0004-boundary-35"))
	wantNone(t, mustInvert(t, tab, writing("308.17", zero)), ReasonOffStep)
}

// The admitted arguments of the recorded requested-target observations are
// written as the bytes those records hold.
func TestInverseAgainstRecordedPayloads(t *testing.T) {
	limits := kelvinLimits(driver.LimitDeviceQuery, "308.15", "343.15", "")
	for name, kelvin := range map[string]string{"0004-boundary-35": "308.15", "0004-value-50": "323.15", "0004-boundary-70": "343.15"} {
		wantPayload(t, mustInvert(t, inverseTable(), writing(kelvin, limits)), donorPayload(t, name))
	}
}

func TestInverseWhenOfTheWriteRow(t *testing.T) {
	gated := inverseTable()
	gated.Bindings[0].Write.When = When{Requires: []string{"writable"}, Equal: map[string]string{"role": "controller"}}
	ok := writing("293.15", nil)
	ok.Qualifiers = Qualifiers{Features: map[string]bool{"writable": true}, Values: map[string]string{"role": "controller"}}
	wantPayload(t, mustInvert(t, gated, ok), []byte{0, 0, 0xa0, 0x41})

	for name, q := range map[string]Qualifiers{
		"feature false":     {Features: map[string]bool{"writable": false}, Values: map[string]string{"role": "controller"}},
		"feature unknown":   {Values: map[string]string{"role": "controller"}},
		"qualifier differs": {Features: map[string]bool{"writable": true}, Values: map[string]string{"role": "other"}},
		"qualifier unknown": {Features: map[string]bool{"writable": true}},
		"nothing supplied":  {},
	} {
		t.Run(name, func(t *testing.T) {
			w := writing("293.15", nil)
			w.Qualifiers = q
			wantNone(t, mustInvert(t, gated, w), ReasonNoMatch)
		})
	}
}

// A write whose own conditions pass but whose target row does not apply to the
// revision or features supplied gives no payload.
func TestInverseTargetRowConditions(t *testing.T) {
	tab := inverseTable()
	tab.Native[0].When = When{Versions: []VersionRange{{Axis: "api", Min: []uint16{2}, Before: []uint16{3}}}, Requires: []string{"target"}}
	w := func(rev []uint16, feature *bool) Writing[cmdSel] {
		x := writing("293.15", nil)
		x.Qualifiers = Qualifiers{Revisions: map[string][]uint16{"api": rev}}
		if rev == nil {
			x.Qualifiers.Revisions = nil
		}
		if feature != nil {
			x.Qualifiers.Features = map[string]bool{"target": *feature}
		}
		return x
	}
	wantPayload(t, mustInvert(t, tab, w([]uint16{2}, ptr(true))), []byte{0, 0, 0xa0, 0x41})
	wantPayload(t, mustInvert(t, tab, w([]uint16{2, 9}, ptr(true))), []byte{0, 0, 0xa0, 0x41})
	wantNone(t, mustInvert(t, tab, w([]uint16{1, 65535}, ptr(true))), ReasonNoMatch) // below
	wantNone(t, mustInvert(t, tab, w([]uint16{3}, ptr(true))), ReasonNoMatch)        // the end is excluded
	wantNone(t, mustInvert(t, tab, w([]uint16{2}, ptr(false))), ReasonNoMatch)       // feature known false
	wantNone(t, mustInvert(t, tab, w([]uint16{2}, nil)), ReasonNoMatch)              // feature unknown
	wantNone(t, mustInvert(t, tab, w(nil, ptr(true))), ReasonNoMatch)                // revision unknown

	// two variants of the target, told apart by revision, each with its own layout
	variants := inverseTable()
	variants.Native[0].When = When{Versions: []VersionRange{{Axis: "api", Min: []uint16{0}, Before: []uint16{3}}}}
	variants.Native = append(variants.Native, NativeRow[cmdSel]{ID: "inverse.target", Unit: "native.celsius", At: targetSel,
		When: When{Versions: []VersionRange{{Axis: "api", Min: []uint16{3}, Before: []uint16{65535}}}}, Wire: layout(0, 32, "f32", "be")})
	old, later := writing("293.15", nil), writing("293.15", nil)
	old.Qualifiers = Qualifiers{Revisions: map[string][]uint16{"api": {2}}}
	later.Qualifiers = Qualifiers{Revisions: map[string][]uint16{"api": {3}}}
	wantPayload(t, mustInvert(t, variants, old), []byte{0, 0, 0xa0, 0x41})
	wantPayload(t, mustInvert(t, variants, later), []byte{0x41, 0xa0, 0, 0})

	// two applicable variants are ambiguous and give no payload
	both := inverseTable()
	both.Native = append(both.Native, both.Native[0])
	wantNone(t, mustInvert(t, both, writing("293.15", nil)), ReasonAmbiguous)
}

func TestInverseExtentOfTheTarget(t *testing.T) {
	tab := inverseTable()
	tab.Native[0].Wire = layout(8, 32, "f32", "le") // ends at byte 5 of 4
	wantNone(t, mustInvert(t, tab, writing("293.15", nil)), ReasonExtent)
}

func TestInverseNoWrite(t *testing.T) {
	w := writing("293.15", nil)
	w.SubjectRole = "readonly"
	wantNone(t, mustInvert(t, inverseTable(), w), ReasonNoWrite)
}

func TestInverseNeighbouringWriteSelectors(t *testing.T) {
	for name, mutate := range map[string]func(*Writing[cmdSel]){
		"native row":             func(w *Writing[cmdSel]) { w.NativeID = "inverse.target" },
		"role":                   func(w *Writing[cmdSel]) { w.SubjectRole = "other" },
		"operation":              func(w *Writing[cmdSel]) { w.Operation = ref(thermalPack, "thermal.operation.other") },
		"operation v":            func(w *Writing[cmdSel]) { w.Operation.Version = "1.0.1" },
		"operation pack id":      func(w *Writing[cmdSel]) { w.Operation.Pack.ID = "helianthus.pack.other" },
		"operation pack version": func(w *Writing[cmdSel]) { w.Operation.Pack.Version = "1.0.1" },
		"argument":               func(w *Writing[cmdSel]) { w.Argument.ID = "thermal.argument.other" },
	} {
		t.Run(name, func(t *testing.T) {
			w := writing("293.15", nil)
			mutate(&w)
			wantNone(t, mustInvert(t, inverseTable(), w), ReasonNoMatch)
		})
	}
}

func TestInverseArgumentChecks(t *testing.T) {
	// a published limit is in the unit of the argument
	wrong := kelvinLimits(driver.LimitDeviceQuery, "", "313.15", "")
	wrong.Max.Unit = "unit.celsius"
	wantNone(t, mustInvert(t, inverseTable(), writing("293.15", wrong)), ReasonUnit)
	// a limit applies to a quantity
	w := writing("293.15", kelvinLimits(driver.LimitDeviceQuery, "", "313.15", ""))
	text := "warm"
	w.Argument.Value = v1.Value{Kind: v1.ValueText, Text: &text}
	wantNone(t, mustInvert(t, inverseTable(), w), ReasonArgument)
	// a native value that no codec can encode exactly gives no payload
	tab := inverseTable()
	tab.Native[0].Wire = layout(0, 16, "u", "be")
	tab.Bindings[0].Write.Inverse = Convert{Numeric: &Affine{Num: 10, Den: 1, Add: dec("-2731.5")}}
	tab.Bindings[0].Write.NativeBounds = Bounds{}
	wantPayload(t, mustInvert(t, tab, writing("293.15", nil)), []byte{0x00, 0xc8, 0, 0}) // 200 tenths of a degree, in the 4 bytes the selector fixes
	wantNone(t, mustInvert(t, tab, writing("293.16", nil)), ReasonEncode)                // 200.1 tenths
	wantNone(t, mustInvert(t, tab, writing("260", nil)), ReasonEncode)                   // below zero
}
