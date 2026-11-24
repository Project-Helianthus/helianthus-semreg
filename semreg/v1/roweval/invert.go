package roweval

import (
	"fmt"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// WriteResult is the result of Invert. Payload is the exact native payload of
// the target row, nil when Reason says why there is none. It grants no
// permission and claims no success: admission, dispatch and readback belong to
// the runtime.
type WriteResult struct {
	Payload   []byte
	Target    string
	Readback  string
	Native    *v1.Decimal
	Reason    Reason
	Detail    string
	Citations []v1.EvidenceRef
}

func noPayload(r Reason, format string, args ...any) (WriteResult, error) {
	return WriteResult{Reason: r, Detail: fmt.Sprintf(format, args...)}, nil
}

// Invert turns an admitted argument into the exact native payload of the target
// row of a write. The conditions of the write row and of the target row are
// checked against the same qualifiers; the argument is checked against the
// published limits in their own unit when the driver published any, and against
// the row's native bounds in the native domain otherwise.
func Invert[S any](t Table[S], w Writing[S]) (WriteResult, error) {
	var withWrite []BindingRow
	var seen bool
	for _, b := range t.Bindings {
		if b.NativeID != w.NativeID || b.SubjectRole != w.SubjectRole || !b.When.check(w.Qualifiers).ok {
			continue
		}
		seen = true
		if b.Write != nil && b.Write.Operation == w.Operation && b.Write.Argument == w.Argument.ID && b.Write.When.check(w.Qualifiers).ok {
			withWrite = append(withWrite, b)
		}
	}
	if !seen {
		return noPayload(ReasonNoMatch, "no binding for row %q and role %q", w.NativeID, w.SubjectRole)
	}
	switch len(withWrite) {
	case 0:
		for _, b := range t.Bindings {
			if b.NativeID == w.NativeID && b.SubjectRole == w.SubjectRole && b.Write == nil && b.When.check(w.Qualifiers).ok {
				return noPayload(ReasonNoWrite, "the element has no semantic write")
			}
		}
		return noPayload(ReasonNoMatch, "no write row applies")
	case 1:
	default:
		return noPayload(ReasonAmbiguous, "%d write rows apply", len(withWrite))
	}
	wr := withWrite[0].Write

	target, reason, detail := rowByID(t, wr.TargetRow, w.Qualifiers)
	if reason != "" {
		return noPayload(reason, "%s", detail)
	}

	arg := w.Argument.Value
	if w.Limits != nil {
		if reason, detail := checkLimits(*w.Limits, arg); reason != "" {
			return noPayload(reason, "%s", detail)
		}
	}

	unit := v1.DefinitionID(target.Unit)
	env := &convEnv[S]{table: t, desc: w.Descriptors, qualifier: w.Qualifiers, row: RowID(target.ID), dir: Inverse,
		inputs: detach(w.Inputs, w.Qualifiers, w.Coordinates)}
	native, reason, detail := env.apply(wr.Inverse, arg, unit)
	if reason != "" {
		return WriteResult{Reason: reason, Detail: detail, Citations: env.citations}, nil
	}
	if native.Kind != v1.ValueQuantity || native.Quantity == nil {
		return WriteResult{Reason: ReasonArgument, Detail: "the native value is not a quantity", Citations: env.citations}, nil
	}
	n := native.Quantity.Number
	if w.Limits == nil {
		if reason, detail := boundsReason(wr.NativeBounds, n); reason != "" {
			return WriteResult{Reason: reason, Detail: "native bounds: " + detail, Native: &n, Citations: env.citations}, nil
		}
	}

	length := -1
	if w.Descriptors.Extent != nil {
		if l, ok := w.Descriptors.Extent(target.At); ok {
			length = l
		}
	}
	payload, reason, detail := place(w.Descriptors, target.Wire, n, length)
	if reason != "" {
		return WriteResult{Reason: reason, Detail: detail, Native: &n, Citations: env.citations}, nil
	}
	return WriteResult{Payload: payload, Target: target.ID, Readback: wr.ReadbackRow, Native: &n, Citations: env.citations}, nil
}

// checkLimits checks the argument against the published limits in the unit of
// the limits. The step counts from the minimum when there is one and from zero
// otherwise.
func checkLimits(l driver.ValueLimits, arg v1.Value) (Reason, string) {
	if arg.Kind != v1.ValueQuantity || arg.Quantity == nil {
		return ReasonArgument, "limits apply to a quantity"
	}
	for _, m := range []struct {
		name string
		q    *v1.Quantity
	}{{"min", l.Min}, {"max", l.Max}, {"step", l.Step}} {
		if m.q != nil && m.q.Unit != arg.Quantity.Unit {
			return ReasonUnit, fmt.Sprintf("limit %s is in %q, the argument in %q", m.name, m.q.Unit, arg.Quantity.Unit)
		}
	}
	b := Bounds{}
	if l.Min != nil {
		b.Min = &l.Min.Number
	}
	if l.Max != nil {
		b.Max = &l.Max.Number
	}
	if l.Step != nil {
		b.Step = &l.Step.Number
	}
	r, d := boundsReason(b, arg.Quantity.Number)
	if r != "" {
		return r, "published limit: " + d
	}
	return "", ""
}
