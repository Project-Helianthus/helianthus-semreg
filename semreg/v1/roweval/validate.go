package roweval

import (
	"fmt"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// TableError is the stated reason a table is rejected.
type TableError struct {
	Reason Reason
	Row    string
	Detail string
}

func (e *TableError) Error() string {
	return fmt.Sprintf("roweval: table rejected: %s: row %q: %s", e.Reason, e.Row, e.Detail)
}

func reject(r Reason, row, format string, args ...any) error {
	return &TableError{Reason: r, Row: row, Detail: fmt.Sprintf(format, args...)}
}

// ValidateTable rejects a table with a stated reason when it holds an ambiguous
// match (two rows that one observation can match), an extent outside the
// payload, a cyclic ExponentRow chain, an identifier that the driver's
// descriptors do not know, a hook that is not registered, or a binding whose
// role or catalog definitions do not resolve.
func ValidateTable[S any](t Table[S], d Descriptors[S], r Roles) error {
	for i, row := range t.Native {
		if err := validateNative(t, d, row); err != nil {
			return err
		}
		for _, other := range t.Native[:i] {
			if canOverlap(row.When, other.When) && (sameAt(row.At, other.At) || row.ID == other.ID) {
				return reject(ReasonAmbiguous, row.ID, "one observation can match this row and row %q", other.ID)
			}
		}
	}
	for i, b := range t.Bindings {
		if _, err := resolve(r, b); err != nil {
			return reject(ReasonUnresolvedRole, b.NativeID, "%v", err)
		}
		for _, c := range convertsOf(b) {
			if err := checkConvert(t, b.NativeID, c); err != nil {
				return err
			}
		}
		for _, other := range t.Bindings[:i] {
			if b.Read != nil && other.Read != nil && b.NativeID == other.NativeID && canOverlap(b.When, other.When) {
				return reject(ReasonAmbiguous, b.NativeID, "one observation can match two read bindings of this row")
			}
		}
	}
	return nil
}

func convertsOf(b BindingRow) []Convert {
	var out []Convert
	if b.Read != nil {
		out = append(out, b.Read.ToRegistry)
	}
	if b.Write != nil {
		out = append(out, b.Write.Inverse)
	}
	return out
}

func checkConvert[S any](t Table[S], id string, c Convert) error {
	if c.Numeric != nil {
		if c.Numeric.Den == 0 {
			return reject(ReasonBadAffine, id, "denominator is zero")
		}
		if c.Numeric.Add != (v1.Decimal{}) {
			if err := c.Numeric.Add.Validate(); err != nil {
				return reject(ReasonBadAffine, id, "add: %v", err)
			}
		}
		if c.Numeric.ExponentRow != "" && !hasRow(t, c.Numeric.ExponentRow) {
			return reject(ReasonUnresolvedDef, id, "exponent row %q does not exist", c.Numeric.ExponentRow)
		}
	}
	switch c.Round {
	case "", RoundTowardZero, RoundHalfUp, RoundHalfEven:
	default:
		return reject(ReasonBadRound, id, "rounding %q", c.Round)
	}
	if c.Hook != "" && t.Hooks[c.Hook] == nil {
		return reject(ReasonUnknownHook, id, "hook %q is not registered", c.Hook)
	}
	return nil
}

func hasRow[S any](t Table[S], id string) bool {
	for _, r := range t.Native {
		if r.ID == id {
			return true
		}
	}
	return false
}

func validateNative[S any](t Table[S], d Descriptors[S], row NativeRow[S]) error {
	if _, _, _, _, missing := identifiers(d, row.Wire); missing != "" {
		return reject(ReasonUnknownID, row.ID, "unknown %s", missing)
	}
	if row.Wire.Width < 1 || row.Wire.Width > 64 {
		return reject(ReasonExtent, row.ID, "width %d", row.Wire.Width)
	}
	if d.Extent != nil {
		if length, ok := d.Extent(row.At); ok && fieldBytes(row.Wire) > length {
			return reject(ReasonExtent, row.ID, "field ends at byte %d, the payload has %d", fieldBytes(row.Wire), length)
		}
	}
	if err := checkConvert(t, row.ID, row.Decode); err != nil {
		return err
	}
	if back := exponentCycle(t, row, []When{row.When}, map[string]bool{row.ID: true}); back != "" {
		return reject(ReasonCyclicExponent, row.ID, "exponent row chain returns to %q", back)
	}
	return nil
}

// exponentCycle follows the exponent rows of row through every variant of the
// named ID whose conditions can hold together with those of the chain so far,
// and returns the ID the chain returns to, or "" when no such chain cycles.
func exponentCycle[S any](t Table[S], row NativeRow[S], chain []When, seen map[string]bool) string {
	next := exponentOf(row)
	if next == "" {
		return ""
	}
	if seen[next] {
		return next
	}
	seen[next] = true
	defer delete(seen, next)
	for _, step := range t.Native {
		if step.ID != next {
			continue
		}
		compatible := true
		for _, w := range chain {
			if !canOverlap(w, step.When) {
				compatible = false
				break
			}
		}
		if !compatible {
			continue
		}
		if back := exponentCycle(t, step, append(chain[:len(chain):len(chain)], step.When), seen); back != "" {
			return back
		}
	}
	return ""
}

func exponentOf[S any](r NativeRow[S]) string {
	if r.Decode.Numeric == nil {
		return ""
	}
	return r.Decode.Numeric.ExponentRow
}
