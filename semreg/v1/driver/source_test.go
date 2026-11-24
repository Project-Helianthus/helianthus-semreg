package driver_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestElementKeepsTheWholeFactKey(t *testing.T) {
	wantValid(t, "element", element("boiler", "flow").Validate())
	wantInvalid(t, "element without subject", driver.Element{Key: factKey("flow")}.Validate())
	wantInvalid(t, "element without key", driver.Element{Subject: node("boiler")}.Validate())
	// Two slots of one definition on one subject are two elements.
	slotA := driver.Element{Subject: node("boiler"), Key: factKey("flow")}
	slotA.Key.Dimensions = []v1.Dimension{{ID: "dimension.slot", Value: v1.Value{Kind: v1.ValueText, Text: ptr("one")}}}
	slotB := slotA
	slotB.Key.Dimensions = []v1.Dimension{{ID: "dimension.slot", Value: v1.Value{Kind: v1.ValueText, Text: ptr("two")}}}
	wantValid(t, "slot one", slotA.Validate())
	wantValid(t, "slot two", slotB.Validate())
	if reflect.DeepEqual(slotA, slotB) {
		t.Fatal("two slots are one element")
	}
}

func TestSourceRules(t *testing.T) {
	wantValid(t, "source", source("a", 3).Validate())
	unknownStep := source("a", 3)
	unknownStep.Step = nil
	wantValid(t, "source with an unknown step", unknownStep.Validate())
	relayed := source("a", 3)
	relayed.Via = []driver.NodeRef{node("relay-1"), node("relay-2")}
	wantValid(t, "source with relays", relayed.Validate())
	// A priority is the driver's: zero and negative scores are as valid as any.
	wantValid(t, "priority zero", source("a", 0).Validate())
	wantValid(t, "negative priority", source("a", -4).Validate())

	for name, mutate := range map[string]func(*driver.Source){
		"element":       func(s *driver.Source) { s.For = driver.Element{} },
		"producer":      func(s *driver.Source) { s.Producer = driver.NodeRef{} },
		"path key":      func(s *driver.Source) { s.PathKey = "" },
		"zero step":     func(s *driver.Source) { s.Step = quantity("0", 0, "unit.kelvin") },
		"negative step": func(s *driver.Source) { s.Step = quantity("-5", -1, "unit.kelvin") },
		"invalid step":  func(s *driver.Source) { s.Step = quantity("5", -1, "!") },
		"relay":         func(s *driver.Source) { s.Via = []driver.NodeRef{{}} },
	} {
		s := source("a", 3)
		mutate(&s)
		wantInvalid(t, "source with invalid "+name, s.Validate())
	}
}

func TestSourceRefIdentifiesADeclaration(t *testing.T) {
	wantValid(t, "ref", sourceRef("a").Validate())
	if reflect.DeepEqual(sourceRef("a"), sourceRef("b")) {
		t.Fatal("two path keys are one declaration")
	}
	for name, mutate := range map[string]func(*driver.SourceRef){
		"element":  func(r *driver.SourceRef) { r.For = driver.Element{} },
		"producer": func(r *driver.SourceRef) { r.Producer = driver.NodeRef{} },
		"path key": func(r *driver.SourceRef) { r.PathKey = "" },
	} {
		r := sourceRef("a")
		mutate(&r)
		wantInvalid(t, "ref with invalid "+name, r.Validate())
	}
}

func TestSampleCarriesTheRuntimeReceipt(t *testing.T) {
	s := driver.Sample{From: source("a", 1), Candidate: candidate("a"), Receipt: driver.ReceiptStamp{At: monotonic("7")}}
	wantValid(t, "sample", s.Validate())
	noReceipt := s
	noReceipt.Receipt = driver.ReceiptStamp{}
	wantInvalid(t, "sample without the runtime's receipt", noReceipt.Validate())
	badCandidate := s
	badCandidate.Candidate.CandidateID = ""
	wantInvalid(t, "sample with an invalid candidate", badCandidate.Validate())
	badSource := s
	badSource.From.PathKey = ""
	wantInvalid(t, "sample with an invalid source", badSource.Validate())
}

func TestSettingRouteNamesTheOwner(t *testing.T) {
	wantValid(t, "route", settingRoute("a").Validate())
	for name, mutate := range map[string]func(*driver.SettingRoute){
		"element":   func(r *driver.SettingRoute) { r.For = driver.Element{} },
		"operation": func(r *driver.SettingRoute) { r.Operation = v1.DefinitionRef{} },
		"owner":     func(r *driver.SettingRoute) { r.Owner = driver.SourceRef{} },
		"route":     func(r *driver.SettingRoute) { r.Route.BindingID = "" },
	} {
		r := settingRoute("a")
		mutate(&r)
		wantInvalid(t, "route with invalid "+name, r.Validate())
	}
	foreign := settingRoute("a")
	foreign.Owner.For = element("another-boiler", "flow")
	wantInvalid(t, "route whose owner is a source of another element", foreign.Validate())
	otherKey := settingRoute("a")
	otherKey.Owner.For = element("boiler", "return")
	wantInvalid(t, "route whose owner is a source of another key", otherKey.Validate())
}

func TestSourceViewAvailability(t *testing.T) {
	wantValid(t, "view", driver.SourceView{Source: source("a", 1), Availability: v1.AvailabilityDegraded}.Validate())
	wantInvalid(t, "view without availability", driver.SourceView{Source: source("a", 1)}.Validate())
	wantInvalid(t, "view with invalid source", driver.SourceView{Availability: v1.AvailabilityAvailable}.Validate())
}

func tie(score int32) *int32 { return &score }

func sampleFinding() driver.Finding {
	return driver.Finding{
		First: sourceRef("a"), Second: sourceRef("b"),
		FirstValue: quantity("2", 1, "unit.kelvin"), SecondValue: quantity("3", 1, "unit.kelvin"),
		FirstStep: quantity("5", -1, "unit.kelvin"), SecondStep: quantity("1", 0, "unit.kelvin"),
	}
}

func tieFinding(score int32) driver.Finding {
	return driver.Finding{First: sourceRef("a"), Second: sourceRef("b"), PriorityTie: tie(score)}
}

func TestFindingTypedConstruction(t *testing.T) {
	wantValid(t, "sample disagreement", sampleFinding().Validate())
	incomparable := sampleFinding()
	incomparable.FirstStep = nil
	incomparable.Incomparable = true
	wantValid(t, "incomparable sample with an unknown step", incomparable.Validate())
	wantValid(t, "priority tie", tieFinding(7).Validate())
	wantValid(t, "priority tie of the declared score zero", tieFinding(0).Validate())
	wantValid(t, "priority tie of a negative score", tieFinding(-2).Validate())
}

func TestFindingInvalidMixedPayload(t *testing.T) {
	for name, mutate := range map[string]func(*driver.Finding){
		"first value":  func(f *driver.Finding) { f.FirstValue = quantity("2", 1, "unit.kelvin") },
		"second value": func(f *driver.Finding) { f.SecondValue = quantity("2", 1, "unit.kelvin") },
		"first step":   func(f *driver.Finding) { f.FirstStep = quantity("5", -1, "unit.kelvin") },
		"second step":  func(f *driver.Finding) { f.SecondStep = quantity("5", -1, "unit.kelvin") },
		"incomparable": func(f *driver.Finding) { f.Incomparable = true },
	} {
		f := tieFinding(0)
		mutate(&f)
		wantInvalid(t, "priority tie carrying a "+name, f.Validate())
	}
	for name, mutate := range map[string]func(*driver.Finding){
		"first value":      func(f *driver.Finding) { f.FirstValue = nil },
		"second value":     func(f *driver.Finding) { f.SecondValue = nil },
		"invalid value":    func(f *driver.Finding) { f.FirstValue = quantity("x", 0, "unit.kelvin") },
		"nonpositive step": func(f *driver.Finding) { f.FirstStep = quantity("0", 0, "unit.kelvin") },
		"unknown step without the incomparable marker": func(f *driver.Finding) { f.SecondStep = nil },
	} {
		f := sampleFinding()
		mutate(&f)
		wantInvalid(t, "sample finding with "+name, f.Validate())
	}
	bare := driver.Finding{First: sourceRef("a"), Second: sourceRef("b")}
	wantInvalid(t, "finding with neither a tie nor a sample payload", bare.Validate())
}

func TestFindingNeedsTwoDistinctOrderedSources(t *testing.T) {
	same := tieFinding(1)
	same.Second = same.First
	wantInvalid(t, "tie of one source with itself", same.Validate())
	reversed := tieFinding(1)
	reversed.First, reversed.Second = reversed.Second, reversed.First
	wantInvalid(t, "descending references", reversed.Validate())
	invalid := tieFinding(1)
	invalid.First = driver.SourceRef{}
	wantInvalid(t, "invalid reference", invalid.Validate())
	// Two producers of one path key are two distinct sources.
	a, b := sourceRef("same"), sourceRef("same")
	b.Producer = node("producer-b")
	f := driver.Finding{First: a, Second: b, PriorityTie: tie(1)}
	if (a.Producer.ID < b.Producer.ID) != (f.Validate() == nil) {
		t.Errorf("producers a=%v b=%v: validation does not follow ascending order", a.Producer, b.Producer)
	}
}

func TestFindingEncodesTheDeclaredScoreZero(t *testing.T) {
	raw, err := json.Marshal(tieFinding(0))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"priority_tie":0`) {
		t.Fatalf("score zero is not encoded: %s", raw)
	}
	for _, absent := range []string{"first_value", "second_value", "first_step", "second_step", "incomparable"} {
		if strings.Contains(string(raw), absent) {
			t.Errorf("tie encodes %s: %s", absent, raw)
		}
	}
	var decoded driver.Finding
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PriorityTie == nil || *decoded.PriorityTie != 0 {
		t.Fatalf("score zero decoded as %v", decoded.PriorityTie)
	}
	wantValid(t, "decoded tie", decoded.Validate())
	if !reflect.DeepEqual(decoded, tieFinding(0)) {
		t.Errorf("round trip: %+v", decoded)
	}

	sample, err := json.Marshal(sampleFinding())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sample), "priority_tie") {
		t.Errorf("sample finding encodes an absent tie: %s", sample)
	}
	var again driver.Finding
	if err := json.Unmarshal(sample, &again); err != nil {
		t.Fatal(err)
	}
	if again.PriorityTie != nil || !reflect.DeepEqual(again, sampleFinding()) {
		t.Errorf("sample round trip: %+v", again)
	}
}

func TestResolution(t *testing.T) {
	views := []driver.SourceView{
		{Source: source("a", 5), Availability: v1.AvailabilityAvailable},
		{Source: source("b", 5), Availability: v1.AvailabilityAvailable},
		{Source: source("c", 1), Availability: v1.AvailabilityUnavailable},
	}
	winner := sourceRef("b")
	r := driver.Resolution{Winner: &winner, Sources: views, Findings: []driver.Finding{tieFinding(5)}}
	wantValid(t, "resolution", r.Validate())
	wantValid(t, "resolution with no winner", driver.Resolution{Sources: views}.Validate())

	absent := sourceRef("zz")
	wantInvalid(t, "winner outside the sources", driver.Resolution{Winner: &absent, Sources: views}.Validate())
	invalidWinner := driver.SourceRef{}
	wantInvalid(t, "invalid winner", driver.Resolution{Winner: &invalidWinner, Sources: views}.Validate())
	wantInvalid(t, "invalid source", driver.Resolution{Sources: []driver.SourceView{{}}}.Validate())
	wantInvalid(t, "invalid finding", driver.Resolution{Sources: views, Findings: []driver.Finding{{}}}.Validate())
}

func TestResolutionFindingsAreInDeterministicOrder(t *testing.T) {
	ab := driver.Finding{First: sourceRef("a"), Second: sourceRef("b"), PriorityTie: tie(5)}
	ac := driver.Finding{First: sourceRef("a"), Second: sourceRef("c"), PriorityTie: tie(5)}
	bc := driver.Finding{First: sourceRef("b"), Second: sourceRef("c"), PriorityTie: tie(5)}
	views := []driver.SourceView{
		{Source: source("a", 5), Availability: v1.AvailabilityAvailable},
		{Source: source("b", 5), Availability: v1.AvailabilityAvailable},
		{Source: source("c", 5), Availability: v1.AvailabilityAvailable},
	}
	wantValid(t, "ascending findings", driver.Resolution{Sources: views, Findings: []driver.Finding{ab, ac, bc}}.Validate())
	wantInvalid(t, "descending findings", driver.Resolution{Sources: views, Findings: []driver.Finding{bc, ab}}.Validate())
	wantInvalid(t, "unordered second reference", driver.Resolution{Sources: views, Findings: []driver.Finding{ac, ab}}.Validate())
	wantInvalid(t, "repeated finding", driver.Resolution{Sources: views, Findings: []driver.Finding{ab, ab}}.Validate())
}

// A tie is reported before either source has a sample, so a tie finding needs no
// sample record, and a conflict stays a separate kernel record.
func TestTieIsReportedBeforeAnySampleAndConflictStaysSeparate(t *testing.T) {
	views := []driver.SourceView{
		{Source: source("a", 0), Availability: v1.AvailabilityUnavailable},
		{Source: source("b", 0), Availability: v1.AvailabilityUnavailable},
	}
	wantValid(t, "tie with no sample", driver.Resolution{Sources: views, Findings: []driver.Finding{tieFinding(0)}}.Validate())
	if _, ok := reflect.TypeOf(driver.Resolution{}).FieldByName("Conflicts"); ok {
		t.Error("Resolution carries a conflict; v1.Conflict stays separate")
	}
	if reflect.TypeOf(driver.Finding{}) == reflect.TypeOf(v1.Conflict{}) {
		t.Error("Finding is the kernel conflict")
	}
}
