package roweval

import (
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Geometry frame: identifier, format, data length code, multiplexor and signal
// slice.

func frmExtent(s frmSel) (int, bool) { return int(s.DLC), true }

func fsel(id uint32, slice string) frmSel {
	return frmSel{Identifier: id, Format: "standard", DLC: 8, Slice: slice}
}

func frameRow(id uint32, slice, unit string, l Layout, d Bounds) NativeRow[frmSel] {
	return NativeRow[frmSel]{ID: "frame." + slice, Unit: unit, SpecRef: "frame-clause", At: fsel(id, slice), Wire: l, Domain: d}
}

func frameTable() Table[frmSel] {
	counts := Bounds{Min: ptr(dec("1")), Max: ptr(dec("254"))}
	soc := layout(48, 8, "u", "le")
	soc.Sentinels = map[uint64]string{0xff: "not_available"}
	return Table[frmSel]{
		Native: []NativeRow[frmSel]{
			frameRow(0x311, "charge_voltage", "dV", layout(0, 16, "u", "be"), Bounds{}),
			frameRow(0x311, "charge_current", "dA", layout(16, 16, "u", "be"), Bounds{}),
			frameRow(0x311, "discharge_current", "dA", layout(32, 16, "u", "be"), Bounds{}),
			frameRow(0x311, "raw_status", "raw", layout(56, 8, "u", "le"), Bounds{}),
			frameRow(0x312, "pack_count", "count", layout(32, 8, "u", "le"), counts),
			frameRow(0x312, "total_cells", "count", layout(56, 8, "u", "le"), counts),
			frameRow(0x313, "voltage", "cV", layout(0, 16, "u", "be"), Bounds{}),
			frameRow(0x313, "current", "dA", layout(16, 16, "s", "be"), Bounds{}),
			frameRow(0x313, "max_temp", "dC", layout(32, 16, "s", "be"), Bounds{}),
			frameRow(0x313, "soc", "percent", soc, Bounds{Min: ptr(dec("0")), Max: ptr(dec("100"))}),
			frameRow(0x313, "soh_value", "percent", layout(56, 7, "u", "le"), Bounds{}),
			frameRow(0x313, "soh_valid", "flag", layout(63, 1, "u", "le"), Bounds{}),
		},
		Bindings: []BindingRow{{
			NativeID: "frame.current", SubjectRole: "pack",
			Service: ref(storagePack, "storage.service.pack"),
			Read: &ReadRow{Fact: ref(storagePack, "storage.pack.current"), Dimensions: map[v1.DefinitionID]string{"storage.dimension.pack": "pack"},
				Unit: "unit.ampere", Policy: "policy.synthetic", ToRegistry: Convert{Numeric: &Affine{Num: 1, Den: 10}}},
		}},
	}
}

func slicesOf(tab Table[frmSel], id uint32) []string {
	var out []string
	for _, r := range tab.Native {
		if r.At.Identifier == id {
			out = append(out, r.At.Slice)
		}
	}
	return out
}

func frmAcquired(sel frmSel, payload []byte, tpl v1.FactCandidate) Acquired[frmSel] {
	return Acquired[frmSel]{Selector: sel, Payload: payload, Template: tpl, Roles: testRoles("pack"), Descriptors: testDescriptors(frmExtent)}
}

func frameTemplate() v1.FactCandidate {
	return inferredTemplate("policy.synthetic", ev(nil, "frame-value"))
}

type verdict struct {
	listed   bool // the table has rows for this identifier
	accepted bool
	natives  map[string]*Native
}

// judge evaluates every row of the frame; the frame is accepted when each
// evaluates without a reason.
func judge(t testing.TB, tab Table[frmSel], id uint32, extended bool, data []byte) verdict {
	t.Helper()
	format := "standard"
	if extended {
		format = "extended"
	}
	v := verdict{accepted: true, natives: map[string]*Native{}}
	slices := slicesOf(tab, id)
	v.listed = len(slices) > 0
	if !v.listed {
		slices = []string{""}
	}
	for _, s := range slices {
		got := mustEvaluate(t, tab, frmAcquired(frmSel{Identifier: id, Format: format, DLC: byte(len(data)), Slice: s}, data, frameTemplate()))
		if got.Native == nil || got.Native.Reason != "" {
			v.accepted = false
		}
		v.natives[s] = got.Native
	}
	return v
}

type frameStep struct {
	id       uint32
	extended bool
	data     []byte
	valid    bool
}

func loadSteps(t testing.TB, name string) ([]frameStep, map[string]any) {
	t.Helper()
	rec := readJSON(t, name)
	var out []frameStep
	for _, s := range rec["steps"].([]any) {
		f := at(s, "frame")
		data, err := hex.DecodeString(asString(at(f, "data_hex")))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, frameStep{id: uint32(asInt(at(f, "identifier"))), extended: at(f, "extended") == true, data: data, valid: at(s, "valid") == true})
	}
	return out, rec
}

func frameCases(t testing.TB) []string {
	files, _ := filepath.Glob("testdata/frame-*.classifier.json")
	var out []string
	for _, f := range files {
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "frame-"), ".classifier.json"))
	}
	if len(out) != 13 {
		t.Fatalf("got %d frame cases, want 13", len(out))
	}
	return out
}

// The old behaviour of every recorded frame sequence: the frame verdicts of the
// registry classifier and of the gateway receive path. The two records of a
// count byte 255 differ from each other and are never substituted; the
// specification gives the answer there.
func TestFrameVerdictsAgainstRecords(t *testing.T) {
	tab := frameTable()
	// Frames that carry a count of 255 are rejected by the classifier and
	// accepted by the receive path.
	divergence := map[string]bool{"312-count-255-cells-16": true, "312-count4-eq-1-byte7-eq-255": true}
	for _, name := range frameCases(t) {
		t.Run(name, func(t *testing.T) {
			classifier, _ := loadSteps(t, "frame-"+name+".classifier.json")
			recv, _ := loadSteps(t, "frame-"+name+".recv.json")
			if len(classifier) != len(recv) {
				t.Fatalf("%d steps, %d steps", len(classifier), len(recv))
			}
			for i, c := range classifier {
				r := recv[i]
				if c.id != r.id || !reflect.DeepEqual(c.data, r.data) {
					t.Fatalf("step %d: the two records carry different frames", i)
				}
				v := judge(t, tab, c.id, c.extended, c.data)
				if !v.listed {
					for _, n := range v.natives {
						if n != nil {
							t.Errorf("step %d: identifier %#x has no row but %+v", i, c.id, n)
						}
					}
					continue
				}
				if v.accepted != c.valid {
					t.Errorf("step %d (%#x): evaluator accepts %v, classifier record %v", i, c.id, v.accepted, c.valid)
				}
				if v.accepted != r.valid {
					if !(divergence[name] && c.id == 0x312 && r.valid && !c.valid) {
						t.Errorf("step %d (%#x): evaluator accepts %v, receive-path record %v", i, c.id, v.accepted, r.valid)
					}
				}
			}
		})
	}
}

// A frame the table has no row for matches nothing.
func TestFrameUnlistedIdentifiers(t *testing.T) {
	tab := frameTable()
	for _, name := range []string{"neighbour-310", "neighbour-314"} {
		steps, _ := loadSteps(t, "frame-"+name+".classifier.json")
		found := false
		for _, s := range steps {
			if s.id == 0x310 || s.id == 0x314 {
				found = true
				got := mustEvaluate(t, tab, frmAcquired(fsel(s.id, "charge_voltage"), s.data, frameTemplate()))
				if got.Reason != ReasonNoMatch || got.Candidate != nil || got.Native != nil {
					t.Errorf("%s: %+v", name, got)
				}
			}
		}
		if !found {
			t.Errorf("%s: no neighbouring frame", name)
		}
	}
}

// The native values of the receive path's snapshot, on the fields it emits.
func TestFrameNativeValuesAgainstSnapshot(t *testing.T) {
	tab := frameTable()
	groups := map[string]map[string]string{
		"measurements": {"current_deciamps": "current", "maximum_cell_temperature_decic": "max_temp", "soc_percent": "soc",
			"soh_value": "soh_value", "voltage_centivolts": "voltage"},
		"limits": {"charge_current_deciamps": "charge_current", "charge_voltage_decivolts": "charge_voltage",
			"discharge_current_deciamps": "discharge_current", "raw_status": "raw_status"},
		"status": {"pack_count": "pack_count", "total_cell_count": "total_cells"},
	}
	frames := map[string]uint32{"limits": 0x311, "status": 0x312, "measurements": 0x313}
	compared := 0
	for _, name := range frameCases(t) {
		steps, rec := loadSteps(t, "frame-"+name+".recv.json")
		snap, ok := at(rec, "result", "snapshot").(map[string]any)
		if !ok {
			continue
		}
		byID := map[uint32]frameStep{}
		for _, s := range steps {
			byID[s.id] = s
		}
		for group, fields := range groups {
			step := byID[frames[group]]
			v := judge(t, tab, step.id, step.extended, step.data)
			for field, slice := range fields {
				want, present := at(snap, group, field).(float64)
				if !present {
					t.Fatalf("%s: no %s.%s", name, group, field)
				}
				n := v.natives[slice]
				if n == nil || n.Value == nil || !sameNumber(n.Value.Quantity.Number, formatInt(want)) {
					t.Errorf("%s: %s: native %+v, record %v", name, slice, n, want)
				}
				compared++
			}
		}
		// the valid bit and the value share the last byte of the 313 frame
		step := byID[0x313]
		v := judge(t, tab, step.id, step.extended, step.data)
		valid := at(snap, "measurements", "soh_valid") == true
		if n := v.natives["soh_valid"]; n == nil || sameNumber(n.Value.Quantity.Number, "1") != valid {
			t.Errorf("%s: soh_valid %+v, record %v", name, n, valid)
		}
	}
	if compared == 0 {
		t.Fatal("no snapshot was compared")
	}
}

func formatInt(f float64) string { return strconv.FormatInt(int64(f), 10) }

// The independent new-binding record: the 313 current of the complete tuple is
// -10 deciamperes, which is -1 A under the pack current fact.
func TestFrameCurrentNewBinding(t *testing.T) {
	rec := readJSON(t, "frame-313-current.binding.json")
	data, err := hex.DecodeString(asString(at(rec, "input", "frame_313_hex")))
	if err != nil {
		t.Fatal(err)
	}
	if asInt(at(rec, "input", "current_raw_signed")) != -10 || asString(at(rec, "expected", "unit")) != "A" || asString(at(rec, "expected", "fact")) != "storage.pack.current" {
		t.Fatalf("record %v", rec)
	}
	key := v1.FactKey{PackID: "helianthus.pack.storage", PackVersion: "1.0.0", FactID: "storage.pack.current", Dimensions: []v1.Dimension{textDimension("storage.dimension.pack", "pack")}}
	value := quantity(dec(asString(at(rec, "expected", "value", "number"))), "unit.ampere")

	for name, mutate := range map[string]func(*v1.Quality){
		"supplied quality": func(*v1.Quality) {},
		"unqualified":      func(q *v1.Quality) { q.Qualification = v1.QualificationCandidate },
		"unavailable":      func(q *v1.Quality) { q.Availability = v1.AvailabilityUnavailable },
	} {
		t.Run(name, func(t *testing.T) {
			tpl := frameTemplate()
			mutate(&tpl.Quality)
			got := mustEvaluate(t, frameTable(), frmAcquired(fsel(0x313, "current"), data, tpl))
			if got.Candidate == nil {
				t.Fatalf("%q %s", got.Reason, got.Detail)
			}
			if !reflect.DeepEqual(*got.Candidate, wantCandidate(tpl, key, value)) {
				t.Fatalf("candidate\n got %+v\nwant %+v", *got.Candidate, wantCandidate(tpl, key, value))
			}
			c := got.Candidate
			if c.Derivation == nil || c.BindingID != nil || c.SourceEpochID != nil || c.DriverGeneration != nil || c.Origin.SourceID != nil {
				t.Errorf("an inferred candidate keeps its derivation and no native path: %+v", c)
			}
		})
	}

	// A complete inferred template with no derivation is the typed input error.
	tpl := frameTemplate()
	tpl.Derivation = nil
	got, err := Evaluate(frameTable(), frmAcquired(fsel(0x313, "current"), data, tpl))
	if inputKind(err) != InputMissingMetadata || got.Candidate != nil {
		t.Fatalf("error %v candidate %v", err, got.Candidate)
	}
	// An unresolved pack role is a different typed error.
	a := frmAcquired(fsel(0x313, "current"), data, frameTemplate())
	a.Roles = testRoles("other")
	if got, err = Evaluate(frameTable(), a); inputKind(err) != InputUnresolvedRole || got.Candidate != nil {
		t.Fatalf("error %v candidate %v", err, got.Candidate)
	}
}

// Counts of 1 to 254 are specified; 255 is outside the range and is rejected
// whatever the other count is. The classifier record agrees; the receive-path
// record accepts it, which is the expected historical divergence.
func TestFrameCountByte255(t *testing.T) {
	spec := readJSON(t, "frame-312-count.specification.json")
	if at(spec, "expected", "verdict") != "reject" || at(spec, "donor_observed", "recv") != "accepts" || at(spec, "donor_observed", "classifier") != "rejects" {
		t.Fatalf("record %v", spec)
	}
	data, _ := hex.DecodeString(asString(at(spec, "input", "frame_312_hex")))
	if data[4] != byte(asInt(at(spec, "input", "byte4_pack_count"))) || data[7] != byte(asInt(at(spec, "input", "byte7_total_cell_count"))) {
		t.Fatal("record input")
	}
	tab := frameTable()
	v := judge(t, tab, 0x312, false, data)
	if v.accepted {
		t.Fatal("accepted")
	}
	if n := v.natives["pack_count"]; n.Reason != "" || !sameNumber(n.Value.Quantity.Number, "1") {
		t.Errorf("pack count %+v", n)
	}
	if n := v.natives["total_cells"]; n.Reason != ReasonOutsideDomain {
		t.Errorf("total cells %+v", n)
	}

	classifier, _ := loadSteps(t, "frame-312-count4-eq-1-byte7-eq-255.classifier.json")
	recv, _ := loadSteps(t, "frame-312-count4-eq-1-byte7-eq-255.recv.json")
	if !reflect.DeepEqual(classifier[1].data, data) || !reflect.DeepEqual(recv[1].data, data) {
		t.Fatal("the donor records carry another frame")
	}
	if classifier[1].valid || !recv[1].valid {
		t.Fatalf("donor records: classifier %v receive path %v", classifier[1].valid, recv[1].valid)
	}
	if judge(t, tab, 0x312, false, data).accepted != classifier[1].valid {
		t.Error("the evaluator does not agree with the classifier record")
	}
}

// Values on each side of every bound of the 312 and 313 rows.
func TestFrameBounds(t *testing.T) {
	tab := frameTable()
	frame312 := func(count, cells byte) []byte { return []byte{0x00, 0x80, 0x00, 0x00, count, 0xaa, 0xbb, cells} }
	frame313 := func(soc byte) []byte { return []byte{0x14, 0x00, 0xff, 0xf6, 0x01, 0x2c, soc, 0xff} }
	for _, c := range []struct {
		name   string
		data   []byte
		id     uint32
		accept bool
	}{
		{"count 0", frame312(0, 16), 0x312, false}, {"count 1", frame312(1, 16), 0x312, true},
		{"count 254", frame312(254, 16), 0x312, true}, {"count 255", frame312(255, 16), 0x312, false},
		{"cells 0", frame312(2, 0), 0x312, false}, {"cells 1", frame312(2, 1), 0x312, true},
		{"cells 254", frame312(2, 254), 0x312, true}, {"cells 255", frame312(2, 255), 0x312, false},
		{"soc 0", frame313(0), 0x313, true}, {"soc 100", frame313(100), 0x313, true},
		{"soc 101", frame313(101), 0x313, false}, {"soc sentinel", frame313(0xff), 0x313, false},
	} {
		if got := judge(t, tab, c.id, false, c.data).accepted; got != c.accept {
			t.Errorf("%s: accepted %v, want %v", c.name, got, c.accept)
		}
	}
	// The sentinel is a reason of its own, not a number.
	got := mustEvaluate(t, tab, frmAcquired(fsel(0x313, "soc"), frame313(0xff), frameTemplate()))
	if got.Reason != ReasonSentinel || got.Native.Disposition != "not_available" || got.Native.Value != nil {
		t.Errorf("%+v", got)
	}
}

// The last byte of 313 holds a 7-bit value and a valid bit.
func TestFrameValueAndValidBitShareAByte(t *testing.T) {
	tab := frameTable()
	for _, c := range []struct {
		last        byte
		value, flag string
	}{{0x7f, "127", "0"}, {0x80, "0", "1"}, {0xff, "127", "1"}, {0x01, "1", "0"}} {
		v := judge(t, tab, 0x313, false, []byte{0x14, 0x00, 0xff, 0xf6, 0x01, 0x2c, 0x50, c.last})
		if !sameNumber(v.natives["soh_value"].Value.Quantity.Number, c.value) || !sameNumber(v.natives["soh_valid"].Value.Quantity.Number, c.flag) {
			t.Errorf("byte %#x: value %+v flag %+v", c.last, v.natives["soh_value"].Value, v.natives["soh_valid"].Value)
		}
	}
}

// Each field of the frame selector is varied on its own.
func TestFrameNeighbouringSelector(t *testing.T) {
	data := []byte{0x14, 0x00, 0xff, 0xf6, 0x01, 0x2c, 0x50, 0xff}
	for name, mutate := range map[string]func(*frmSel){
		"identifier": func(s *frmSel) { s.Identifier = 0x314 }, "format": func(s *frmSel) { s.Format = "extended" },
		"data length code short": func(s *frmSel) { s.DLC = 7 }, "data length code long": func(s *frmSel) { s.DLC = 9 },
		"multiplexor": func(s *frmSel) { s.Mux = "1" }, "signal slice": func(s *frmSel) { s.Slice = "other" },
	} {
		sel := fsel(0x313, "current")
		mutate(&sel)
		got := mustEvaluate(t, frameTable(), frmAcquired(sel, data, frameTemplate()))
		if got.Reason != ReasonNoMatch || got.Candidate != nil || got.Native != nil {
			t.Errorf("%s: %+v", name, got)
		}
	}
	// A frame that is shorter than its data length code states is short.
	got := mustEvaluate(t, frameTable(), frmAcquired(fsel(0x313, "current"), data[:7], frameTemplate()))
	if got.Reason != ReasonShortPayload {
		t.Errorf("%+v", got)
	}
}
