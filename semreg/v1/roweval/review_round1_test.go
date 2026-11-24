package roweval

import (
	"testing"
	"time"
)

func scaledTable(scale NativeRow[blkSel], extra ...NativeRow[blkSel]) Table[blkSel] {
	tab := blkTable()
	tab.Native[1] = scale
	tab.Native = append(tab.Native, extra...)
	return tab
}

func evalScaled(t *testing.T, tab Table[blkSel], words map[string]any) Evaluation {
	t.Helper()
	return mustEvaluate(t, tab, blkAcquired(blkW, blkPayload(103, 50, words), blkTemplate(), testRoles("inverter")))
}

// An exponent row's own Decode applies before its value is used: raw 0 plus 1
// is exponent 1, so 1500 reads as 15000.
func TestExponentRowAppliesItsDecode(t *testing.T) {
	scale := NativeRow[blkSel]{ID: "wordblk.scale", Unit: "exponent", At: blkSF, Wire: layout(15*16, 16, "s", "be"),
		Decode: Convert{Numeric: &Affine{Num: 1, Den: 1, Add: dec("1")}}}
	got := evalScaled(t, scaledTable(scale), map[string]any{"14": 1500.0, "15": 0.0})
	if got.Candidate == nil || !sameNumber(got.Candidate.Value.Quantity.Number, "15000") {
		t.Fatalf("%+v", got)
	}
}

// The conversion of the exponent row applies to the domain check too.
func TestExponentRowDomainChecksTheDecodedValue(t *testing.T) {
	scale := NativeRow[blkSel]{ID: "wordblk.scale", Unit: "exponent", At: blkSF, Wire: layout(15*16, 16, "s", "be"),
		Decode: Convert{Numeric: &Affine{Num: 1, Den: 1, Add: dec("1")}},
		Domain: Bounds{Max: ptr(dec("0"))}}
	got := evalScaled(t, scaledTable(scale), map[string]any{"14": 1500.0, "15": 0.0})
	if got.Candidate != nil || got.Reason != ReasonOutsideDomain {
		t.Fatalf("%+v", got)
	}
}

// A nested ExponentRow of the exponent row applies: raw 1 scaled by 10^1 is
// exponent 10, so 1500 reads as 15000000000000.
func TestExponentRowNestedDependency(t *testing.T) {
	selSecond := blkSel{Function: 3, Table: "model", Block: 103, Offset: 16, Count: 1}
	scale := NativeRow[blkSel]{ID: "wordblk.scale", Unit: "exponent", At: blkSF, Wire: layout(15*16, 16, "s", "be"),
		Decode: Convert{Numeric: &Affine{Num: 1, Den: 1, ExponentRow: "wordblk.scale2"}}}
	second := NativeRow[blkSel]{ID: "wordblk.scale2", Unit: "exponent", At: selSecond, Wire: layout(16*16, 16, "s", "be")}
	got := evalScaled(t, scaledTable(scale, second), map[string]any{"14": 1500.0, "15": 1.0, "16": 1.0})
	if got.Candidate == nil || !sameNumber(got.Candidate.Value.Quantity.Number, "15000000000000") {
		t.Fatalf("%+v", got)
	}
}

// A cyclic exponent chain that was never validated ends with the stated reason.
func TestExponentRowCycleAtEvaluationEnds(t *testing.T) {
	scale := NativeRow[blkSel]{ID: "wordblk.scale", Unit: "exponent", At: blkSF, Wire: layout(15*16, 16, "s", "be"),
		Decode: Convert{Numeric: &Affine{Num: 1, Den: 1, ExponentRow: "wordblk.scale"}}}
	got := evalScaled(t, scaledTable(scale), map[string]any{"14": 1500.0, "15": 1.0})
	if got.Candidate != nil || got.Reason != ReasonCyclicExponent {
		t.Fatalf("%+v", got)
	}
}

// An exponent that no Decimal can hold is refused before a power of ten is built.
func TestExponentOutsideDecimalRangeIsRefused(t *testing.T) {
	scale := NativeRow[blkSel]{ID: "wordblk.scale", Unit: "exponent", At: blkSF, Wire: layout(15*16, 32, "u", "be")}
	tab := scaledTable(scale)
	done := make(chan Evaluation, 1)
	go func() {
		done <- evalScaled(t, tab, map[string]any{"14": 1500.0, "15": 32767.0, "16": 65535.0})
	}()
	select {
	case got := <-done:
		if got.Candidate != nil || got.Reason != ReasonConversion {
			t.Fatalf("%+v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("evaluation did not finish")
	}
}

// The conditions of the binding apply to the write as they do to the read.
func TestInvertChecksBindingConditions(t *testing.T) {
	tab := inverseTable()
	tab.Bindings[0].When = When{Requires: []string{"writable"}}
	w := writing("293.15", nil)
	wantNone(t, mustInvert(t, tab, w), ReasonNoMatch)
	w.Qualifiers = Qualifiers{Features: map[string]bool{"writable": false}}
	wantNone(t, mustInvert(t, tab, w), ReasonNoMatch)
	w.Qualifiers = Qualifiers{Features: map[string]bool{"writable": true}}
	wantPayload(t, mustInvert(t, tab, w), []byte{0x00, 0x00, 0xa0, 0x41})
}

// Two conditional variants of one write are told apart by the binding conditions.
func TestInvertBindingConditionsSelectOneVariant(t *testing.T) {
	tab := inverseTable()
	variant := tab.Bindings[0]
	tab.Bindings[0].When = When{Requires: []string{"variant_a"}}
	variant.When = When{Forbids: []string{"variant_a"}}
	tab.Bindings = append(tab.Bindings, variant)
	w := writing("293.15", nil)
	w.Qualifiers = Qualifiers{Features: map[string]bool{"variant_a": true}}
	wantPayload(t, mustInvert(t, tab, w), []byte{0x00, 0x00, 0xa0, 0x41})
	w.Qualifiers = Qualifiers{Features: map[string]bool{"variant_a": false}}
	wantPayload(t, mustInvert(t, tab, w), []byte{0x00, 0x00, 0xa0, 0x41})
}
