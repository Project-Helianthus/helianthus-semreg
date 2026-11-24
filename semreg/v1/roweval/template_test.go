package roweval

import (
	"reflect"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Everything the caller supplied is kept: the evaluator writes the fact key and
// the value and nothing else, and shares nothing with the template.
func TestTemplateIsPreservedAndNotShared(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	phenomenon := v1.TimePoint{UnixNanoseconds: "50", ClockID: "clock.synthetic", UncertaintyNS: "1"}
	tpl := observedTemplate("policy.synthetic", d.evidence)
	tpl.Times.PhenomenonAt = &phenomenon
	tpl.Quality.Reasons = []v1.DefinitionID{"reason.a", "reason.b"}
	tpl.Revision = "41"
	want := cloneCandidate(tpl)

	got := mustEvaluate(t, cmdTable(), cmdAcquired(d, tpl, testRoles("dhw")))
	if got.Candidate == nil {
		t.Fatalf("%+v", got)
	}
	exp := wantCandidate(want, dhwKey(), quantity(dec("323.15"), "unit.kelvin"))
	if !reflect.DeepEqual(*got.Candidate, exp) {
		t.Fatalf("candidate\n got %+v\nwant %+v", *got.Candidate, exp)
	}
	// the template is as it was
	if !reflect.DeepEqual(tpl, want) {
		t.Errorf("the template changed")
	}
	// a time that was absent stays absent; the evaluator has no clock
	noSource := observedTemplate("policy.synthetic", d.evidence)
	noSource.Times.SourceAt = nil
	got = mustEvaluate(t, cmdTable(), cmdAcquired(d, noSource, testRoles("dhw")))
	if got.Candidate.Times.SourceAt != nil || got.Candidate.Times.PhenomenonAt != nil {
		t.Errorf("times %+v", got.Candidate.Times)
	}
	// nothing is shared: changing the result leaves the template alone
	c := got.Candidate
	c.Evidence[0].Owner = "test.changed"
	c.Origin.Evidence[0].Owner = "test.changed"
	*c.BindingID = "binding.changed"
	*c.Origin.SourceID = "source.changed"
	if noSource.Evidence[0].Owner == "test.changed" || *noSource.BindingID == "binding.changed" || *noSource.Origin.SourceID == "source.changed" {
		t.Error("the candidate shares memory with the template")
	}
	// the template's own key and value are not used
	tpl = observedTemplate("policy.synthetic", d.evidence)
	tpl.Value = quantity(dec("1"), "unit.kelvin")
	got = mustEvaluate(t, cmdTable(), cmdAcquired(d, tpl, testRoles("dhw")))
	if !sameNumber(got.Candidate.Value.Quantity.Number, "323.15") || got.Candidate.Key.FactID == "template.fact" {
		t.Errorf("%+v", got.Candidate)
	}
}

// A template that is not complete is the typed input error and no candidate.
func TestIncompleteTemplateIsATypedInputError(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	cases := map[string]func(*v1.FactCandidate){
		"no candidate id":           func(c *v1.FactCandidate) { c.CandidateID = "" },
		"no revision":               func(c *v1.FactCandidate) { c.Revision = "" },
		"no evidence":               func(c *v1.FactCandidate) { c.Evidence = nil },
		"no reasons list":           func(c *v1.FactCandidate) { c.Quality.Reasons = nil },
		"no quality":                func(c *v1.FactCandidate) { c.Quality = v1.Quality{} },
		"no receipt time":           func(c *v1.FactCandidate) { c.Times.ReceivedAt = v1.TimePoint{} },
		"no evaluation time":        func(c *v1.FactCandidate) { c.Times.EvaluatedAt = v1.TimePoint{} },
		"no freshness policy":       func(c *v1.FactCandidate) { c.FreshnessPolicy.FreshForNS = "" },
		"observed without binding":  func(c *v1.FactCandidate) { c.BindingID = nil },
		"observed without epoch":    func(c *v1.FactCandidate) { c.SourceEpochID = nil },
		"observed without driver":   func(c *v1.FactCandidate) { c.DriverGeneration = nil },
		"native origin without src": func(c *v1.FactCandidate) { c.Origin.SourceID = nil },
		"observed with derivation": func(c *v1.FactCandidate) {
			c.Derivation = inferredTemplate("policy.synthetic", c.Evidence...).Derivation
		},
		"no origin": func(c *v1.FactCandidate) { c.Origin = v1.OriginRef{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			tpl := observedTemplate("policy.synthetic", d.evidence)
			mutate(&tpl)
			got, err := Evaluate(cmdTable(), cmdAcquired(d, tpl, testRoles("dhw")))
			if inputKind(err) != InputMissingMetadata || got.Candidate != nil || got.Subject != nil {
				t.Fatalf("error %v candidate %v", err, got.Candidate)
			}
			if got.Native == nil || got.Native.Value == nil {
				t.Error("the native evaluation was not kept")
			}
		})
	}
	// an inferred template needs its derivation and no native path
	for name, mutate := range map[string]func(*v1.FactCandidate){
		"no derivation":    func(c *v1.FactCandidate) { c.Derivation = nil },
		"a native path":    func(c *v1.FactCandidate) { b := v1.NativeBindingID("binding.synthetic"); c.BindingID = &b },
		"a derived origin": func(c *v1.FactCandidate) { c.Origin.Kind = v1.OriginNativeObservation },
	} {
		tpl := inferredTemplate("policy.synthetic", d.evidence)
		mutate(&tpl)
		got, err := Evaluate(cmdTable(), cmdAcquired(d, tpl, testRoles("dhw")))
		if inputKind(err) != InputMissingMetadata || got.Candidate != nil {
			t.Errorf("inferred, %s: error %v", name, err)
		}
	}
	// the freshness policy has to be the read row's
	tpl := observedTemplate("policy.other", d.evidence)
	got, err := Evaluate(cmdTable(), cmdAcquired(d, tpl, testRoles("dhw")))
	if inputKind(err) != InputPolicyMismatch || got.Candidate != nil {
		t.Errorf("policy: %v", err)
	}
}

// A complete inferred template keeps its derivation, its absent native path and
// its derived origin.
func TestInferredTemplateIsPreserved(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	tpl := inferredTemplate("policy.synthetic", d.evidence)
	got := mustEvaluate(t, cmdTable(), cmdAcquired(d, tpl, testRoles("dhw")))
	if got.Candidate == nil || !reflect.DeepEqual(*got.Candidate, wantCandidate(tpl, dhwKey(), quantity(dec("323.15"), "unit.kelvin"))) {
		t.Fatalf("%+v", got)
	}
	if err := got.Candidate.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A value the registry cannot represent is reported, not clamped.
func TestRegistryRepresentationIsCheckedSeparately(t *testing.T) {
	d := loadRequested(t, "0004-value-50")
	tab := cmdTable()
	// 50 × 9e18 = 45e19 needs an exponent of 19; a decimal holds 18 at most
	tab.Bindings[0].Read.ToRegistry.Numeric = &Affine{Num: 9_000_000_000_000_000_000, Den: 1}
	got := mustEvaluate(t, tab, cmdAcquired(d, observedTemplate("policy.synthetic", d.evidence), testRoles("dhw")))
	if got.Candidate != nil || got.Reason != ReasonConversion {
		t.Errorf("%+v", got)
	}
	// the native value is still there
	if got.Native == nil || got.Native.Value == nil || !sameNumber(got.Native.Value.Quantity.Number, "50") {
		t.Errorf("%+v", got.Native)
	}
}
