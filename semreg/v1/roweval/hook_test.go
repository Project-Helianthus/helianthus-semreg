package roweval

import (
	"reflect"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

type hookCall struct {
	inputs    DetachedInputs
	row       RowID
	direction Direction
	value     TypedValue
}

// hooked is the command table with a hook declared beside the row that uses it.
func hooked(place string, h RowHook) Table[cmdSel] {
	tab := cmdTable()
	tab.Hooks = map[string]RowHook{"derive": h}
	switch place {
	case "decode":
		tab.Native[0].Decode.Hook, tab.Native[1].Decode.Hook = "derive", "derive"
	case "registry":
		tab.Bindings[0].Read.ToRegistry.Hook = "derive"
	}
	return tab
}

func TestHookIsCalledWithDetachedInputsAndBothResultsAreUsed(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	cited := ev(t, "cited input")
	replies := map[string]string{"decode": "60", "registry": "400"}
	for _, place := range []string{"decode", "registry"} {
		t.Run(place, func(t *testing.T) {
			var calls []hookCall
			tab := hooked(place, func(in DetachedInputs, row RowID, dir Direction, v TypedValue) (DerivedValue, Reason) {
				calls = append(calls, hookCall{in, row, dir, v})
				// the hook answers with a value of its own and cites an input
				return DerivedValue{Value: *quantity(dec(replies[place]), string(v.Quantity.Unit)), Citations: []v1.EvidenceRef{cited}}, ""
			})
			a := cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw"))
			a.Qualifiers.Features = map[string]bool{"x": true}
			a.Coordinates = map[string]string{"slot": "1"}
			a.Inputs = []Input{{Payload: []byte{9, 9}, Receipt: Receipt{ReceivedAt: v1.TimePoint{UnixNanoseconds: "7", ClockID: "clock.synthetic", UncertaintyNS: "0"}}}}
			got := mustEvaluate(t, tab, a)
			if len(calls) != 1 || calls[0].row != "cmdreg.requested" || calls[0].direction != Forward {
				t.Fatalf("calls %+v", calls)
			}
			in := calls[0].inputs
			if !reflect.DeepEqual(in.Inputs()[0].Payload, d.raw) || !reflect.DeepEqual(in.Inputs()[1].Payload, []byte{9, 9}) || in.Inputs()[1].Receipt.ReceivedAt.UnixNanoseconds != "7" {
				t.Errorf("inputs %+v", in.Inputs())
			}
			if in.Qualifiers().Values["presentation"] != "controller" || !in.Qualifiers().Features["x"] || in.Coordinates()["slot"] != "1" {
				t.Errorf("qualifiers %+v coordinates %+v", in.Qualifiers(), in.Coordinates())
			}
			// the value the hook is applied to
			wantIn := "50"
			if place == "registry" {
				wantIn = "323.15"
			}
			if !sameNumber(calls[0].value.Quantity.Number, wantIn) {
				t.Errorf("hook was applied to %+v", calls[0].value)
			}
			// the derived value is used, and the citations are recorded
			if got.Candidate == nil || !reflect.DeepEqual(got.Citations, []v1.EvidenceRef{cited}) {
				t.Fatalf("%+v", got)
			}
			wantOut := map[string]string{"decode": "333.15", "registry": "400"}[place]
			if !sameNumber(got.Candidate.Value.Quantity.Number, wantOut) {
				t.Errorf("value %+v, want %s", got.Candidate.Value.Quantity.Number, wantOut)
			}
		})
	}
}

func TestHookIsGivenACopy(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	tab := hooked("registry", func(in DetachedInputs, _ RowID, _ Direction, v TypedValue) (DerivedValue, Reason) {
		in.Inputs()[0].Payload[0] = 0xff
		in.Qualifiers().Values["presentation"] = "changed"
		in.Coordinates()["slot"] = "changed"
		return DerivedValue{Value: v}, ""
	})
	a := cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw"))
	a.Coordinates = map[string]string{"slot": "1"}
	before := append([]byte(nil), a.Payload...)
	mustEvaluate(t, tab, a)
	if !reflect.DeepEqual(a.Payload, before) || a.Qualifiers.Values["presentation"] != "controller" || a.Coordinates["slot"] != "1" {
		t.Errorf("the hook reached the caller's data: %x %v %v", a.Payload, a.Qualifiers, a.Coordinates)
	}
}

func TestHookReasonIsAStatedRefusal(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	tab := hooked("registry", func(_ DetachedInputs, _ RowID, _ Direction, v TypedValue) (DerivedValue, Reason) {
		return DerivedValue{Value: v, Citations: []v1.EvidenceRef{ev(t, "c")}}, "no_cached_input"
	})
	got := mustEvaluate(t, tab, cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw")))
	if got.Reason != "no_cached_input" || got.Candidate != nil || got.Native == nil || got.Native.Value == nil {
		t.Errorf("%+v", got)
	}
	// a hook that is not registered is not called
	tab = cmdTable()
	tab.Bindings[0].Read.ToRegistry.Hook = "nothing"
	if got := mustEvaluate(t, tab, cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw"))); got.Reason != ReasonUnknownHook || got.Candidate != nil {
		t.Errorf("%+v", got)
	}
}

func TestHookInDirectionInverse(t *testing.T) {
	var call hookCall
	tab := inverseTable()
	tab.Hooks = map[string]RowHook{"derive": func(in DetachedInputs, row RowID, dir Direction, v TypedValue) (DerivedValue, Reason) {
		call = hookCall{in, row, dir, v}
		return DerivedValue{Value: *quantity(dec("20"), "native.celsius"), Citations: []v1.EvidenceRef{ev(t, "c")}}, ""
	}}
	tab.Bindings[0].Write.Inverse.Hook = "derive"
	w := writing("999", nil)
	w.Inputs = []Input{{Payload: []byte{1}}}
	r := mustInvert(t, tab, w)
	wantPayload(t, r, []byte{0, 0, 0xa0, 0x41})
	if call.direction != Inverse || call.row != "inverse.target" || len(call.inputs.Inputs()) != 1 || call.value.Quantity.Unit != "native.celsius" {
		t.Errorf("call %+v", call)
	}
	if len(r.Citations) != 1 {
		t.Errorf("citations %+v", r.Citations)
	}
	tab.Hooks["derive"] = func(DetachedInputs, RowID, Direction, TypedValue) (DerivedValue, Reason) {
		return DerivedValue{}, "no_mode"
	}
	wantNone(t, mustInvert(t, tab, w), "no_mode")
}
