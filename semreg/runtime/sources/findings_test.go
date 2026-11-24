package sources

import (
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func pairLab(t *testing.T, topStep, lowStep *v1.Quantity) (*lab, *drv, *drv) {
	l := newLab(t)
	boiler := l.addDriver("boiler", "asset:boiler", 30, topStep)
	mirror := l.addDriver("mirror", "asset:mirror", 10, lowStep)
	return l, boiler, mirror
}

func publishPair(l *lab, boiler, mirror *drv, top, low v1.Quantity) ElementState {
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", top, "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:mirror", low, "1", 100, "m1"))
	l.now = 110
	return l.element()
}

func TestValuesWithinAndOverTheStepGiveNoFindingAndAFindingRespectively(t *testing.T) {
	half := quantity("5", -1)
	whole := quantity("1", 0)
	cases := []struct {
		name      string
		topStep   *v1.Quantity
		lowStep   *v1.Quantity
		low       v1.Quantity
		wantFound bool
	}{
		{"exactly_the_step_is_within", &half, &half, quantity("32215", -2), false},
		{"over_the_step_disagrees", &half, &half, quantity("32216", -2), true},
		{"the_larger_step_governs_within", &half, &whole, quantity("32245", -2), false},
		{"the_larger_step_governs_over", &half, &whole, quantity("32266", -2), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, boiler, mirror := pairLab(t, tc.topStep, tc.lowStep)
			e := publishPair(l, boiler, mirror, quantity("32165", -2), tc.low)
			if err := e.Resolution.Validate(); err != nil {
				t.Fatal(err)
			}
			if !isWinner(e, boiler) || coefficient(e.Value) != "32165" {
				t.Fatalf("a finding never changes the winner: %s", mustJSON(t, e))
			}
			if !tc.wantFound {
				if len(e.Resolution.Findings) != 0 {
					t.Fatalf("unexpected finding: %s", mustJSON(t, e.Resolution.Findings))
				}
				return
			}
			if len(e.Resolution.Findings) != 1 {
				t.Fatalf("findings: %s", mustJSON(t, e.Resolution.Findings))
			}
			f := e.Resolution.Findings[0]
			if f.Incomparable || refKey(f.First) != refKey(boiler.ref()) || refKey(f.Second) != refKey(mirror.ref()) ||
				f.FirstValue == nil || f.FirstValue.Number.Coefficient != "32165" || f.SecondValue == nil || f.SecondValue.Number.Coefficient != tc.low.Number.Coefficient ||
				f.FirstStep == nil || f.SecondStep == nil || *f.FirstStep != *tc.topStep || *f.SecondStep != *tc.lowStep {
				t.Fatalf("the finding names both sources, values and steps: %s", mustJSON(t, f))
			}
		})
	}
}

func TestUnknownStepAndFloat32EncodingAreReportedIncomparable(t *testing.T) {
	half := quantity("5", -1)
	cases := []struct {
		name string
		top  v1.Quantity
		low  v1.Quantity
	}{
		{"unknown_step_far_apart", quantity("32165", -2), quantity("32665", -2)},
		{"float32_encoding_with_no_stated_step", quantity("321649994", -6), quantity("321650009", -6)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, boiler, mirror := pairLab(t, &half, nil)
			e := publishPair(l, boiler, mirror, tc.top, tc.low)
			if err := e.Resolution.Validate(); err != nil {
				t.Fatal(err)
			}
			if len(e.Resolution.Findings) != 1 {
				t.Fatalf("findings: %s", mustJSON(t, e.Resolution.Findings))
			}
			f := e.Resolution.Findings[0]
			if !f.Incomparable || f.FirstValue == nil || f.SecondValue == nil || f.FirstStep == nil || f.SecondStep != nil {
				t.Fatalf("an unknown step is incomparable, never a disagreement and never skipped: %s", mustJSON(t, f))
			}
			if !isWinner(e, boiler) {
				t.Fatal("the winner changed")
			}
		})
	}
}

func TestFreshSampleAfterSetpointChangeAgainstMirrorPreChangeIsIncomparable(t *testing.T) {
	l := newLab(t)
	half := quantity("5", -1)
	boiler := l.addDriver("boiler", "asset:site", 30, &half)
	mirror := l.addDriver("mirror", "asset:site", 10, &half)
	l.now = 100
	mirror.publish(mirror.obs("cand:mirror", quantity("32165", -2), "1", 100, "m1"))
	l.now = 130
	snapshot := boiler.publish(boiler.obs("cand:boiler", quantity("32665", -2), "1", 130, "b1"))
	l.now = 140
	conflicts := func(s v1.Snapshot) string {
		var all []v1.Conflict
		for _, envelope := range s.Facts {
			all = append(all, envelope.Conflicts...)
		}
		return mustJSON(t, all)
	}
	if conflicts(snapshot) == "null" || conflicts(snapshot) == "[]" {
		t.Fatal("setup: the kernel keeps an exact conflict for the two values")
	}
	member := l.evals()[KeyOf(boiler.ref())].Member
	before := conflicts(member)
	e := l.element()
	if err := e.Resolution.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(e.Resolution.Findings) != 1 {
		t.Fatalf("findings: %s", mustJSON(t, e.Resolution.Findings))
	}
	f := e.Resolution.Findings[0]
	if !f.Incomparable || f.FirstValue == nil || f.SecondValue == nil || f.FirstStep == nil || f.SecondStep == nil {
		t.Fatalf("not contemporaneous, both steps known: incomparable and not a disagreement: %s", mustJSON(t, f))
	}
	if !isWinner(e, boiler) || coefficient(e.Value) != "32665" {
		t.Fatalf("the winner is unchanged: %s", mustJSON(t, e))
	}
	if conflicts(l.evals()[KeyOf(boiler.ref())].Member) != before || before != conflicts(snapshot) {
		t.Fatal("the exact kernel conflict changed")
	}
}

func TestCelsiusNativeDhwIsAdmittedAndResolvedAsKelvinWithoutASecondConversion(t *testing.T) {
	l := newLab(t)
	step := quantity("5", -1)
	boiler := l.addDriver("boiler", "asset:boiler", 30, &step)
	mirror := l.addDriver("mirror", "asset:mirror", 10, &step)
	l.now = 100
	candidate := boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "native-48.5-degC")
	boiler.publish(candidate)
	mirror.publish(mirror.obs("cand:mirror", quantity("32215", -2), "1", 100, "m1"))
	l.now = 110
	e := l.element()
	latest := e.state(boiler).Latest
	if latest == nil || mustJSON(t, latest.Candidate) != mustJSON(t, candidate) {
		t.Fatalf("the admitted candidate is the producer's, unchanged: %s", mustJSON(t, latest))
	}
	q := e.Value.Quantity
	if q.Number != (v1.Decimal{Coefficient: "32165", Exponent10: -2}) || q.Unit != kelvin || latest.Candidate.Key.FactID != "thermal.measurement.dhw_temperature" {
		t.Fatalf("321.65 K on thermal.measurement.dhw_temperature: %s", mustJSON(t, e.Value))
	}
	if latest.Candidate.Evidence[0].Digest != digestOf("native:native-48.5-degC") {
		t.Fatal("the original evidence reference changed")
	}
	step0 := e.Resolution.Sources[0].Source.Step
	if step0 == nil || *step0 != step {
		t.Fatalf("the step is the 0.5 K interval: %s", mustJSON(t, step0))
	}
	if len(e.Resolution.Findings) != 0 {
		t.Fatalf("0.5 K apart is within the 0.5 K step (and no 273.65 K step exists): %s", mustJSON(t, e.Resolution.Findings))
	}
}

func TestNativeModeWithNoCatalogSymbolIsUnknownWithTheNativeValueNamed(t *testing.T) {
	l := newLab(t)
	mode := driver.Element{Subject: driver.NodeRef{Bridge: "bridge:lab", ID: "resource:circuit"}, Key: v1.FactKey{PackID: labPack.ID, PackVersion: labPack.Version, FactID: "thermal.operating_mode", Dimensions: []v1.Dimension{}}}
	l.el = &mode
	boiler := l.addDriver("boiler", "asset:boiler", 30, nil)
	l.now = 100
	native := v1.Value{Kind: v1.ValueSymbol, Symbol: &v1.Symbol{Namespace: "native.mode", Token: "0x2a", Known: false}}
	boiler.publish(boiler.obsValue("cand:mode", &native, "1", 100, "mode1"))
	l.now = 110
	e := l.element()
	if e.Value != nil || e.UnmappedNative == nil || e.UnmappedNative.Symbol.Token != "0x2a" || e.UnmappedNative.Symbol.Known {
		t.Fatalf("unknown with the native value named, never a guessed symbol: %s", mustJSON(t, e))
	}
	if latest := e.state(boiler).Latest; latest == nil || latest.Candidate.Value.Symbol.Token != "0x2a" {
		t.Fatal("the native value stays visible in the source's sample")
	}
}
