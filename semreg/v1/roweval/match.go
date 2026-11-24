package roweval

import (
	"fmt"
	"reflect"
	"sort"
)

// The reasons Evaluate, Invert and ValidateTable state. A hook may return any
// other Reason of its own.
const (
	ReasonNoMatch        Reason = "no_match"
	ReasonAmbiguous      Reason = "ambiguous_match"
	ReasonShortPayload   Reason = "short_payload"
	ReasonSentinel       Reason = "sentinel"
	ReasonDecode         Reason = "decode_error"
	ReasonOutsideDomain  Reason = "outside_domain"
	ReasonOffStep        Reason = "off_step"
	ReasonConversion     Reason = "conversion_error"
	ReasonRegistry       Reason = "registry_representation"
	ReasonNoBinding      Reason = "no_binding"
	ReasonNoWrite        Reason = "no_write"
	ReasonUnknownID      Reason = "unknown_identifier"
	ReasonUnknownHook    Reason = "unknown_hook"
	ReasonExtent         Reason = "extent_outside_payload"
	ReasonUnit           Reason = "unit_mismatch"
	ReasonArgument       Reason = "argument_kind"
	ReasonEncode         Reason = "encode_error"
	ReasonCyclicExponent Reason = "cyclic_exponent_row"
	ReasonUnresolvedRole Reason = "unresolved_role"
	ReasonUnresolvedDef  Reason = "unresolved_definition"
	ReasonBadAffine      Reason = "invalid_affine"
	ReasonBadRound       Reason = "unknown_rounding"
)

// whenResult is the outcome of checking a When against what the driver supplied.
type whenResult struct {
	ok      bool
	missing []string
}

func (w When) check(q Qualifiers) whenResult {
	var res whenResult
	res.ok = true
	fail := func(missing string) {
		res.ok = false
		if missing != "" {
			res.missing = append(res.missing, missing)
		}
	}
	keys := make([]string, 0, len(w.Equal))
	for k := range w.Equal {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, present := q.Values[k]
		if !present {
			fail("value " + k)
		} else if v != w.Equal[k] {
			fail("")
		}
	}
	for _, r := range w.Versions {
		v, present := q.Revisions[r.Axis]
		if !present {
			fail("revision " + r.Axis)
		} else if compareVersion(v, r.Min) < 0 || compareVersion(v, r.Before) >= 0 {
			fail("")
		}
	}
	for _, f := range w.Requires {
		on, present := q.Features[f]
		if !present {
			fail("feature " + f)
		} else if !on {
			fail("")
		}
	}
	for _, f := range w.Forbids {
		on, present := q.Features[f]
		if !present {
			fail("feature " + f)
		} else if on {
			fail("")
		}
	}
	return res
}

func compareVersion(a, b []uint16) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var x, y uint16
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// canOverlap reports whether some observation could satisfy both conditions.
func canOverlap(a, b When) bool {
	for k, v := range a.Equal {
		if w, ok := b.Equal[k]; ok && w != v {
			return false
		}
	}
	for _, f := range a.Requires {
		for _, g := range b.Forbids {
			if f == g {
				return false
			}
		}
	}
	for _, f := range b.Requires {
		for _, g := range a.Forbids {
			if f == g {
				return false
			}
		}
	}
	for _, x := range a.Versions {
		for _, y := range b.Versions {
			if x.Axis != y.Axis {
				continue
			}
			if compareVersion(x.Min, y.Before) >= 0 || compareVersion(y.Min, x.Before) >= 0 {
				return false
			}
		}
	}
	return true
}

func sameAt[S any](a, b S) bool { return reflect.DeepEqual(a, b) }

// matchNative returns the rows of the table at the selector whose conditions are
// met, and the qualifiers that stopped the others.
func matchNative[S any](t Table[S], at S, q Qualifiers) (rows []NativeRow[S], missing []string) {
	for _, r := range t.Native {
		if !sameAt(r.At, at) {
			continue
		}
		res := r.When.check(q)
		if res.ok {
			rows = append(rows, r)
		} else {
			missing = append(missing, res.missing...)
		}
	}
	return rows, missing
}

// rowByID returns the one row with the ID whose conditions are met.
func rowByID[S any](t Table[S], id string, q Qualifiers) (NativeRow[S], Reason, string) {
	var found []NativeRow[S]
	var missing []string
	for _, r := range t.Native {
		if r.ID != id {
			continue
		}
		res := r.When.check(q)
		if res.ok {
			found = append(found, r)
		} else {
			missing = append(missing, res.missing...)
		}
	}
	switch len(found) {
	case 0:
		return NativeRow[S]{}, ReasonNoMatch, fmt.Sprintf("no row %q applies%s", id, missingText(missing))
	case 1:
		return found[0], "", ""
	}
	return NativeRow[S]{}, ReasonAmbiguous, fmt.Sprintf("%d rows %q apply", len(found), id)
}

func missingText(missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	return "; not supplied: " + fmt.Sprint(missing)
}
