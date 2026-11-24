package roweval

import "testing"

// A scaling exponent outside the decimal exponent range is accepted when the
// exact result is a decimal: ten at 10^-19 is 10^-18. A scaling no operand can
// make representable is still refused without building the power.
func TestAffineScalingByTheResultNotTheExponent(t *testing.T) {
	id := Affine{Num: 1, Den: 1}
	got, err := affineOf(id, dec("10"), -19, "")
	if err != nil || !sameNumber(got, "0.000000000000000001") {
		t.Errorf("10 x 10^-19: %+v %v", got, err)
	}
	got, err = affineOf(Affine{Num: 1, Den: 1, Add: dec("-1000000000000000000")}, dec("1"), 20, "")
	if err != nil || !sameNumber(got, "99000000000000000000") {
		t.Errorf("1 x 10^20 - 10^18: %+v %v", got, err)
	}
	for _, e := range []int64{1 << 40, -(1 << 40), -1 << 63} {
		if _, err := affineOf(id, dec("1"), e, RoundTowardZero); err == nil {
			t.Errorf("exponent %d was accepted", e)
		}
	}
}

// Revision-specific variants of an ID: a chain follows only the variants whose
// conditions can hold together, so two revisions that each end do not cycle.
func TestExponentCycleFollowsCompatibleVariantsOnly(t *testing.T) {
	rev := func(n uint16) When {
		return When{Versions: []VersionRange{{Axis: "api", Min: []uint16{n}, Before: []uint16{n + 1}}}}
	}
	row := func(id, exponent string, at uint16, w When) NativeRow[cmdSel] {
		r := NativeRow[cmdSel]{ID: id, Unit: "u", At: cmdSel{Register: at}, When: w, Wire: layout(0, 8, "s", "le")}
		if exponent != "" {
			r.Decode.Numeric = &Affine{Num: 1, Den: 1, ExponentRow: exponent}
		}
		return r
	}
	d := testDescriptors(func(cmdSel) (int, bool) { return 8, true })
	// revision 1: A -> B, B ends; revision 2: B -> A, A ends. Revision-1 variants come first.
	ends := []NativeRow[cmdSel]{
		row("A", "B", 1, rev(1)), row("B", "", 2, rev(1)),
		row("B", "A", 3, rev(2)), row("A", "", 4, rev(2)),
	}
	if err := ValidateTable(Table[cmdSel]{Native: ends}, d, Roles{}); err != nil {
		t.Errorf("two chains that each end: %v", err)
	}
	// a real cycle inside one revision is still found, behind another variant
	cyc := []NativeRow[cmdSel]{
		row("A", "", 1, rev(1)), row("B", "", 2, rev(1)),
		row("A", "B", 3, rev(2)), row("B", "A", 4, rev(2)),
	}
	if got := tableReason(ValidateTable(Table[cmdSel]{Native: cyc}, d, Roles{})); got != ReasonCyclicExponent {
		t.Errorf("cycle in one revision: %q", got)
	}
}
