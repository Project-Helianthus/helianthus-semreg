package roweval

import (
	"reflect"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Geometry wordblk: function, table, block or model occurrence, word offset
// and count.

var (
	blkW  = blkSel{Function: 3, Table: "model", Block: 103, Offset: 14, Count: 1}
	blkSF = blkSel{Function: 3, Table: "model", Block: 103, Offset: 15, Count: 1}
)

// blkExtent is the payload of a block: two header words and its 50 words.
func blkExtent(s blkSel) (int, bool) {
	if s.Table == "model" && s.Block == 103 {
		return (2 + 50) * 2, true
	}
	return 0, false
}

func blkTable() Table[blkSel] {
	return Table[blkSel]{
		Native: []NativeRow[blkSel]{
			{ID: "wordblk.value", Unit: "W", SpecRef: "point-clause", At: blkW,
				Wire:   Layout{BitOffset: 14 * 16, Width: 16, Codec: "s", ByteOrder: "be", WordOrder: "none", BitOrder: "lsb", Sentinels: map[uint64]string{0x8000: "not_implemented"}},
				Decode: Convert{Numeric: &Affine{Num: 1, Den: 1, ExponentRow: "wordblk.scale"}}},
			{ID: "wordblk.scale", Unit: "exponent", SpecRef: "scale-clause", At: blkSF,
				Wire:   layout(15*16, 16, "s", "be"),
				Domain: Bounds{Min: ptr(dec("-10")), Max: ptr(dec("10"))}},
		},
		Bindings: []BindingRow{{
			NativeID: "wordblk.value", SubjectRole: "inverter",
			Service: ref(pvPack, "pv.service.inverter"),
			Read:    &ReadRow{Fact: ref(pvPack, "pv.ac.aggregate_active_power"), Dimensions: map[v1.DefinitionID]string{}, Unit: "unit.watt", Policy: "pv.telemetry.fast.v1"},
		}},
	}
}

// blkPayload is a block of the given length with the words set, big endian.
func blkPayload(model, length int, words map[string]any) []byte {
	p := make([]byte, (2+length)*2)
	put := func(i, v int) { p[2*i], p[2*i+1] = byte(v>>8), byte(v) }
	put(0, model)
	put(1, length)
	for k, v := range words {
		put(asInt(float64(parseInt(k))), asInt(v))
	}
	return p
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func blkAcquired(sel blkSel, payload []byte, tpl v1.FactCandidate, roles Roles) Acquired[blkSel] {
	return Acquired[blkSel]{Selector: sel, Payload: payload, Template: tpl, Roles: roles, Descriptors: testDescriptors(blkExtent)}
}

func blkTemplate() v1.FactCandidate {
	return observedTemplate("pv.telemetry.fast.v1", ev(nil, "native-value"))
}

var blkPoint = v1.FactKey{PackID: "helianthus.pack.pv", PackVersion: "1.0.0", FactID: "pv.ac.aggregate_active_power", Dimensions: []v1.Dimension{}}

// The recorded old behaviour of each integer inverter case, and the independent
// new-binding record of the same case.
func TestWordBlkAgainstRecords(t *testing.T) {
	cases := []struct {
		name   string
		reason Reason // for a case with no value
		note   string
	}{
		{name: "w-1500-sf0"},
		{name: "w-1234-sf-minus1"},
		{name: "w-minus50-sf1"},
		{name: "w-sentinel-8000", reason: ReasonSentinel},
		{name: "sf-plus11", reason: ReasonOutsideDomain},
		{name: "sf-minus11", reason: ReasonOutsideDomain},
	}
	units := map[string]string{"W": "unit.watt"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			operand := readJSON(t, "wordblk-103-"+c.name+".operand.json")
			input := readJSON(t, "wordblk-103-"+c.name+".input.json")
			binding := readJSON(t, "wordblk-"+c.name+".binding.json")
			if asInt(at(input, "model")) != 103 || asInt(at(input, "length")) != 50 {
				t.Fatalf("input %v", input)
			}
			words := at(input, "words").(map[string]any)
			payload := blkPayload(103, 50, words)
			// the new-binding record states the same words, signed
			for w, key := range map[string]string{"14": "word14", "15": "word15"} {
				if s := signed(uint64(asInt(words[w])), 16); s != int64(asInt(at(binding, "input", key))) {
					t.Fatalf("word %s: input %d, new-binding record %v", w, s, at(binding, "input", key))
				}
			}
			tpl := blkTemplate()
			got := mustEvaluate(t, blkTable(), blkAcquired(blkW, payload, tpl, testRoles("inverter")))

			admitted := at(operand, "disposition", "admitted") == true
			if admitted != (c.reason == "") {
				t.Fatalf("record admitted %v, table reason %q", admitted, c.reason)
			}
			expected := at(binding, "expected", "value")
			if c.reason != "" {
				if expected != "unknown" {
					t.Fatalf("new-binding record %v", expected)
				}
				if got.Reason != c.reason || got.Candidate != nil || got.Subject != nil {
					t.Fatalf("reason %q (%s) candidate %v", got.Reason, got.Detail, got.Candidate)
				}
				if got.Native == nil || got.Native.Value != nil {
					t.Fatalf("a value was made for an unknown: %+v", got.Native)
				}
				if c.reason == ReasonSentinel && got.Native.Disposition != "not_implemented" {
					t.Errorf("disposition %q", got.Native.Disposition)
				}
				return
			}
			native := at(operand, "native")
			if got.Native == nil || got.Native.Value == nil || !sameNumber(got.Native.Value.Quantity.Number, asString(at(native, "number"))) ||
				string(got.Native.Value.Quantity.Unit) != asString(at(native, "unit")) {
				t.Fatalf("native %+v, record %v", got.Native, native)
			}
			want := wantCandidate(tpl, blkPoint, quantity(dec(asString(at(expected, "number"))), units[asString(at(expected, "unit"))]))
			if got.Candidate == nil || !reflect.DeepEqual(*got.Candidate, want) {
				t.Fatalf("candidate\n got %+v\nwant %+v", got.Candidate, want)
			}
			if got.Subject == nil || got.Subject.Role != "inverter" {
				t.Errorf("subject %+v", got.Subject)
			}
		})
	}
}

// Raw zero is a number; the not-implemented pattern is not zero.
func TestWordBlkZeroIsNotUnknown(t *testing.T) {
	got := mustEvaluate(t, blkTable(), blkAcquired(blkW, blkPayload(103, 50, map[string]any{"14": 0.0, "15": 0.0}), blkTemplate(), testRoles("inverter")))
	if got.Candidate == nil || !sameNumber(got.Candidate.Value.Quantity.Number, "0") {
		t.Fatalf("%+v", got)
	}
}

// The scale factor bounds -10 and 10 are valid and the values beside them are not.
func TestWordBlkScaleBounds(t *testing.T) {
	for _, c := range []struct {
		sf     int
		reason Reason
		want   string
	}{
		{-11, ReasonOutsideDomain, ""}, {-10, "", "0.00000015"}, {10, "", "15000000000000"}, {11, ReasonOutsideDomain, ""},
	} {
		words := map[string]any{"14": 1500.0, "15": float64(uint16(int16(c.sf)))}
		got := mustEvaluate(t, blkTable(), blkAcquired(blkW, blkPayload(103, 50, words), blkTemplate(), testRoles("inverter")))
		if got.Reason != c.reason {
			t.Fatalf("scale %d: reason %q (%s)", c.sf, got.Reason, got.Detail)
		}
		if c.reason == "" && !sameNumber(got.Candidate.Value.Quantity.Number, c.want) {
			t.Errorf("scale %d: %+v, want %s", c.sf, got.Candidate.Value.Quantity.Number, c.want)
		}
	}
}

// The offsets on each side of the power word belong to other points and miss.
func TestWordBlkNeighbouringOffsets(t *testing.T) {
	for _, off := range []uint16{13, 16} {
		rec := readJSON(t, "wordblk-neighbour-offset-"+map[uint16]string{13: "13", 16: "16"}[off]+".specification.json")
		if asInt(rec["word"]) != int(off) || at(rec, "expected", "binding") != "miss" || at(rec, "expected", "publication") != "none" {
			t.Fatalf("record %v", rec)
		}
		sel := blkW
		sel.Offset = off
		got := mustEvaluate(t, blkTable(), blkAcquired(sel, blkPayload(103, 50, nil), blkTemplate(), testRoles("inverter")))
		if got.Reason != ReasonNoMatch || got.Candidate != nil || got.Native != nil {
			t.Errorf("offset %d: %+v", off, got)
		}
	}
	// Every other field of the selector is varied on its own.
	for name, mutate := range map[string]func(*blkSel){
		"function": func(s *blkSel) { s.Function = 4 }, "table": func(s *blkSel) { s.Table = "other" },
		"block": func(s *blkSel) { s.Block = 101 }, "count": func(s *blkSel) { s.Count = 2 },
	} {
		sel := blkW
		mutate(&sel)
		got := mustEvaluate(t, blkTable(), blkAcquired(sel, blkPayload(103, 50, nil), blkTemplate(), testRoles("inverter")))
		if got.Reason != ReasonNoMatch || got.Candidate != nil {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

// A block whose length is shorter than the row's block, and a block of another
// model, give nothing.
func TestWordBlkShortAndOtherModel(t *testing.T) {
	short := readJSON(t, "wordblk-103-short-inconsistent-length.input.json")
	opShort := readJSON(t, "wordblk-103-short-inconsistent-length.operand.json")
	if at(opShort, "disposition", "admitted") != false {
		t.Fatal("record admits it")
	}
	payload := blkPayload(103, asInt(short["length"]), map[string]any{"14": 1500.0, "15": 0.0})
	got := mustEvaluate(t, blkTable(), blkAcquired(blkW, payload, blkTemplate(), testRoles("inverter")))
	if got.Reason != ReasonShortPayload || got.Candidate != nil || (got.Native != nil && got.Native.Value != nil) {
		t.Fatalf("%+v", got)
	}

	other := readJSON(t, "wordblk-common-then-101.input.json")
	opOther := readJSON(t, "wordblk-common-then-101.operand.json")
	if at(opOther, "disposition", "admitted") != false || len(other["models"].([]any)) != 2 {
		t.Fatal("record")
	}
	sel := blkW
	sel.Block = uint16(asInt(other["models"].([]any)[1]))
	got = mustEvaluate(t, blkTable(), blkAcquired(sel, blkPayload(101, 50, nil), blkTemplate(), testRoles("inverter")))
	if got.Reason != ReasonNoMatch || got.Candidate != nil {
		t.Fatalf("%+v", got)
	}
}

// The scale row read on its own is a native evaluation with no binding.
func TestWordBlkScaleRowAlone(t *testing.T) {
	got := mustEvaluate(t, blkTable(), blkAcquired(blkSF, blkPayload(103, 50, map[string]any{"15": 65535.0}), blkTemplate(), testRoles("inverter")))
	if got.Reason != ReasonNoBinding || got.Native == nil || !sameNumber(got.Native.Value.Quantity.Number, "-1") {
		t.Fatalf("%+v", got)
	}
}

// A promoted observation with unqualified or unavailable quality keeps it.
func TestWordBlkSuppliedQualityKept(t *testing.T) {
	for _, mutate := range []func(*v1.Quality){
		func(q *v1.Quality) { q.Qualification = v1.QualificationCandidate },
		func(q *v1.Quality) { q.Availability = v1.AvailabilityUnavailable },
	} {
		tpl := blkTemplate()
		mutate(&tpl.Quality)
		got := mustEvaluate(t, blkTable(), blkAcquired(blkW, blkPayload(103, 50, map[string]any{"14": 1500.0}), tpl, testRoles("inverter")))
		if got.Candidate == nil || !reflect.DeepEqual(*got.Candidate, wantCandidate(tpl, blkPoint, quantity(dec("1500"), "unit.watt"))) {
			t.Fatalf("%+v", got)
		}
	}
}
