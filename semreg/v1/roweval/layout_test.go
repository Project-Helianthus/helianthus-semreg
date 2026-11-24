package roweval

import (
	"bytes"
	"reflect"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// The raw field of a layout: its bits are read from the payload with the first
// bit least significant, then the bit, byte and word orders the driver names are
// applied in that order. The expected raw values below follow that statement.
func TestLayoutOrders(t *testing.T) {
	d := testDescriptors[cmdSel](nil)
	payload := []byte{0x11, 0x22, 0x33, 0x44, 0x80}
	cases := []struct {
		name string
		l    Layout
		raw  uint64
	}{
		{"32 bits little endian", Layout{BitOffset: 0, Width: 32, Codec: "u", ByteOrder: "le", WordOrder: "none", BitOrder: "lsb"}, 0x44332211},
		{"32 bits big endian", Layout{BitOffset: 0, Width: 32, Codec: "u", ByteOrder: "be", WordOrder: "none", BitOrder: "lsb"}, 0x11223344},
		{"32 bits big endian, word pairs swapped", Layout{BitOffset: 0, Width: 32, Codec: "u", ByteOrder: "be", WordOrder: "pairs", BitOrder: "lsb"}, 0x33441122},
		{"32 bits little endian, word pairs swapped", Layout{BitOffset: 0, Width: 32, Codec: "u", ByteOrder: "le", WordOrder: "pairs", BitOrder: "lsb"}, 0x22114433},
		{"bits reversed", Layout{BitOffset: 32, Width: 8, Codec: "u", ByteOrder: "le", WordOrder: "none", BitOrder: "msb"}, 0x01},
		{"not byte aligned", Layout{BitOffset: 4, Width: 8, Codec: "u", ByteOrder: "le", WordOrder: "none", BitOrder: "lsb"}, 0x21},
		{"top bit of the payload", Layout{BitOffset: 39, Width: 1, Codec: "u", ByteOrder: "le", WordOrder: "none", BitOrder: "lsb"}, 1},
	}
	for _, c := range cases {
		raw, _, reason, detail := extract(d, c.l, payload)
		if reason != "" || raw != c.raw {
			t.Errorf("%s: raw %#x reason %q %s, want %#x", c.name, raw, reason, detail, c.raw)
		}
		// and back: placing the decoded number gives the bytes that were read
		n, _ := testCodecs["u"].Decode(raw, c.l.Width)
		back, reason, detail := place(d, c.l, n, len(payload))
		if reason != "" {
			t.Fatalf("%s: %s %s", c.name, reason, detail)
		}
		mask := bytes.Repeat([]byte{0}, len(payload))
		for i := uint16(0); i < c.l.Width; i++ {
			p := c.l.BitOffset + i
			mask[p/8] |= 1 << (p % 8)
		}
		for i := range payload {
			if back[i] != payload[i]&mask[i] {
				t.Errorf("%s: byte %d is %#x, want %#x", c.name, i, back[i], payload[i]&mask[i])
			}
		}
	}
}

func TestLayoutRejectsWhatDoesNotFit(t *testing.T) {
	d := testDescriptors[cmdSel](nil)
	l := layout(0, 32, "u", "le")
	if _, _, reason, _ := extract(d, l, []byte{1, 2, 3}); reason != ReasonShortPayload {
		t.Errorf("short payload: %q", reason)
	}
	if _, reason, _ := place(d, l, dec("1"), 3); reason != ReasonExtent {
		t.Errorf("extent: %q", reason)
	}
	for _, w := range []uint16{0, 65} {
		l.Width = w
		if _, _, reason, _ := extract(d, l, make([]byte, 16)); reason != ReasonExtent {
			t.Errorf("width %d: %q", w, reason)
		}
	}
	l = layout(0, 8, "nope", "le")
	if _, _, reason, _ := extract(d, l, []byte{1}); reason != ReasonUnknownID {
		t.Errorf("unknown codec: %q", reason)
	}
}

// A conversion of a value that is not exact is an error unless a rounding is
// named, and each rounding has the result its name states.
func TestAffineRounding(t *testing.T) {
	third := Affine{Num: 1, Den: 3}
	if _, err := affineOf(third, dec("1"), 0, ""); err == nil {
		t.Error("an inexact result with no rounding was accepted")
	}
	for _, c := range []struct {
		x, mode, want string
		a             Affine
	}{
		{"1", RoundTowardZero, "0.333333333333333333", third},
		{"2", RoundTowardZero, "0.666666666666666666", third},
		{"2", RoundHalfUp, "0.666666666666666667", third},
		{"-2", RoundHalfUp, "-0.666666666666666667", third},
		{"-2", RoundTowardZero, "-0.666666666666666666", third},
		{"5", RoundHalfUp, "0.000000000000000003", Affine{Num: 1, Den: 2, ExponentRow: ""}},
		{"1", RoundHalfEven, "0.333333333333333333", third},
	} {
		x := dec(c.x)
		a := c.a
		var exp int64
		if c.x == "5" {
			exp = -18
		}
		got, err := affineOf(a, x, exp, c.mode)
		if err != nil || !sameNumber(got, c.want) {
			t.Errorf("%s %s: %+v %v, want %s", c.x, c.mode, got, err, c.want)
		}
	}
	// ties: 2.5e-18 rounds to 2e-18 half-even and 3e-18 half-up
	tie := Affine{Num: 1, Den: 2}
	if got, _ := affineOf(tie, dec("5"), -18, RoundHalfEven); !sameNumber(got, "0.000000000000000002") {
		t.Errorf("half even: %+v", got)
	}
	if got, _ := affineOf(tie, dec("5"), -18, RoundHalfUp); !sameNumber(got, "0.000000000000000003") {
		t.Errorf("half up: %+v", got)
	}
	// a result outside the decimal range is an error, not a clamp
	if _, err := affineOf(Affine{Num: 1, Den: 1}, dec("1"), 19, ""); err == nil {
		t.Error("1e19 fits no decimal but was accepted")
	}
	if _, err := affineOf(Affine{Num: 1, Den: 0}, dec("1"), 0, ""); err == nil {
		t.Error("a zero denominator was accepted")
	}
}

// The value of a conversion that carries symbols.
func TestSymbolConversion(t *testing.T) {
	tab := Table[cmdSel]{}
	env := &convEnv[cmdSel]{table: tab}
	c := Convert{Symbols: map[string]string{"0": "off", "4": "on"}}
	got, reason, _ := env.apply(c, *quantity(dec("4"), "native.state"), "mode.namespace")
	want := v1.Value{Kind: v1.ValueSymbol, Symbol: &v1.Symbol{Namespace: "mode.namespace", Token: "on", Known: true}}
	if reason != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v %q", got, reason)
	}
	if _, reason, _ := env.apply(c, *quantity(dec("7"), "native.state"), "mode.namespace"); reason != ReasonConversion {
		t.Errorf("an unknown native value is %q, not a symbol", reason)
	}
	back, reason, _ := env.apply(c, want, "native.state")
	if reason != "" || !sameNumber(back.Quantity.Number, "4") {
		t.Fatalf("%+v %q", back, reason)
	}
	dup := Convert{Symbols: map[string]string{"1": "on", "2": "on"}}
	if _, reason, _ := env.apply(dup, want, "native.state"); reason != ReasonAmbiguous {
		t.Errorf("two native values for one symbol: %q", reason)
	}
}
