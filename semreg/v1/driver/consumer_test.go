package driver_test

import (
	"context"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func cutRef(id string) graph.CutRef {
	return graph.CutRef{CutID: digestOf(id), Completeness: graph.CutComplete, Members: []graph.CutMember{{AssetID: "asset:one", SnapshotID: "snapshot:one"}}}
}

func TestSemanticInputCaptureLensAndSumAreTyped(t *testing.T) {
	input := &syntheticInput{
		cut:  driver.SemanticCut{Ref: cutRef("cut-one"), Snapshots: []v1.Snapshot{}, SettingRoutes: []driver.SettingRoute{settingRoute("a")}},
		lens: graph.LensResult{Contract: "contract.test", Lens: "lens.test"},
		sum:  graph.MemberSum{Value: quantity("5", 0, "unit.watt")},
	}
	var in driver.SemanticInput = input
	ctx := context.Background()
	cut, err := in.Capture(ctx, driver.Filter{Assets: []v1.AssetID{"asset:one"}})
	if err != nil {
		t.Fatal(err)
	}
	wantValid(t, "captured cut", cut.Validate())
	if len(cut.SettingRoutes) != 1 || cut.Ref.CutID != digestOf("cut-one") {
		t.Errorf("cut = %+v", cut)
	}
	lens, err := in.RetrieveLens(ctx, cut.Ref, graph.LensRequest{Lens: "lens.test", Direction: graph.DirectionOutgoing, Depth: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if lens.Cut.CutID != cut.Ref.CutID {
		t.Errorf("lens read from cut %q, captured %q", lens.Cut.CutID, cut.Ref.CutID)
	}
	sum, err := in.SumMembers(ctx, cut.Ref, graph.LensRequest{Lens: "lens.test"}, defRef("fact.power"), "resource:scope")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Value == nil || sum.Value.Unit != "unit.watt" {
		t.Errorf("sum = %+v", sum)
	}
	changes, err := in.Resume(ctx, driver.Cursor{Incarnation: "incarnation.one", Position: "4"}, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	wantValid(t, "resume with no change", driver.Changes{Cursor: driver.Cursor{Incarnation: "incarnation.one", Position: "4"}}.Validate())
	_ = changes
}

func TestSemanticCutRules(t *testing.T) {
	wantValid(t, "cut", driver.SemanticCut{Ref: cutRef("a"), Snapshots: []v1.Snapshot{}, SettingRoutes: []driver.SettingRoute{settingRoute("a")}}.Validate())
	wantInvalid(t, "cut without an id", driver.SemanticCut{Ref: graph.CutRef{}}.Validate())
	wantInvalid(t, "cut with an invalid snapshot", driver.SemanticCut{Ref: cutRef("a"), Snapshots: []v1.Snapshot{{}}}.Validate())
	wantInvalid(t, "cut with an invalid route", driver.SemanticCut{Ref: cutRef("a"), SettingRoutes: []driver.SettingRoute{{}}}.Validate())
}

func TestFilterCursorAndChangesRules(t *testing.T) {
	wantValid(t, "filter", driver.Filter{Assets: []v1.AssetID{"asset:one"}}.Validate())
	wantValid(t, "empty filter", driver.Filter{}.Validate())
	wantInvalid(t, "filter with an invalid asset", driver.Filter{Assets: []v1.AssetID{"!"}}.Validate())

	cursor := driver.Cursor{Incarnation: "incarnation.one", Position: "9"}
	wantValid(t, "cursor", cursor.Validate())
	wantInvalid(t, "cursor without an incarnation", driver.Cursor{Position: "9"}.Validate())
	wantInvalid(t, "cursor with an invalid position", driver.Cursor{Incarnation: "incarnation.one", Position: "-1"}.Validate())

	commit := driver.SemanticCut{Ref: cutRef("a")}
	wantValid(t, "changes with two transitions", driver.Changes{Cursor: cursor, Commits: []driver.SemanticCut{commit, commit}}.Validate())
	wantValid(t, "resync", driver.Changes{Cursor: cursor, Resync: true}.Validate())
	wantInvalid(t, "resync carrying commits", driver.Changes{Cursor: cursor, Resync: true, Commits: []driver.SemanticCut{commit}}.Validate())
	wantInvalid(t, "changes with an invalid cursor", driver.Changes{}.Validate())
	wantInvalid(t, "changes with an invalid commit", driver.Changes{Cursor: cursor, Commits: []driver.SemanticCut{{}}}.Validate())
}

func TestInputRefOriginAndFormula(t *testing.T) {
	ref := driver.InputRef{At: cutRef("a"), Subject: node("meter"), Candidate: "candidate:power", Revision: "4"}
	wantValid(t, "input ref", ref.Validate())
	for name, mutate := range map[string]func(*driver.InputRef){
		"cut":       func(r *driver.InputRef) { r.At = graph.CutRef{} },
		"subject":   func(r *driver.InputRef) { r.Subject = driver.NodeRef{} },
		"candidate": func(r *driver.InputRef) { r.Candidate = "" },
		"revision":  func(r *driver.InputRef) { r.Revision = "" },
	} {
		bad := ref
		mutate(&bad)
		wantInvalid(t, "input ref with invalid "+name, bad.Validate())
	}

	for _, kind := range []driver.OriginKind{driver.OriginMeasured, driver.OriginDeclared, driver.OriginDerived} {
		wantValid(t, string(kind), driver.Origin{Kind: kind, Evidence: evidenceSet(string(kind))}.Validate())
	}
	if driver.OriginMeasured != "measured" || driver.OriginDeclared != "declared" || driver.OriginDerived != "derived" {
		t.Fatal("origin kind wire values")
	}
	wantInvalid(t, "origin of another kind", driver.Origin{Kind: "estimated"}.Validate())
	wantInvalid(t, "origin without a kind", driver.Origin{Evidence: evidenceSet("x")}.Validate())
	wantInvalid(t, "origin with invalid evidence", driver.Origin{Kind: driver.OriginDeclared, Evidence: []v1.EvidenceRef{{}}}.Validate())

	formula := driver.Formula{Algorithm: defRef("algorithm.sum"), Inputs: []driver.InputRef{ref}}
	wantValid(t, "formula", formula.Validate())
	wantInvalid(t, "formula without an algorithm", driver.Formula{Inputs: []driver.InputRef{ref}}.Validate())
	wantInvalid(t, "formula without inputs", driver.Formula{Algorithm: defRef("algorithm.sum")}.Validate())
	wantInvalid(t, "formula with an invalid input", driver.Formula{Algorithm: defRef("algorithm.sum"), Inputs: []driver.InputRef{{}}}.Validate())
}

// syntheticOutput is the northbound side of a two-way driver: it takes its data
// from SemReg only and delivers every candidate the cut holds.
type syntheticOutput struct{ delivered []v1.FactCandidate }

func (o *syntheticOutput) StartOutput(ctx context.Context, in driver.SemanticInput, _ driver.ScopedOperations) error {
	cut, err := in.Capture(ctx, driver.Filter{})
	if err != nil {
		return err
	}
	for _, snapshot := range cut.Snapshots {
		for _, envelope := range snapshot.Facts {
			o.delivered = append(o.delivered, envelope.Candidates...)
		}
	}
	return nil
}

var _ driver.SemanticOutput = (*syntheticOutput)(nil)

// Qualification alone never prevents StartOutput from receiving otherwise
// eligible input: a candidate that is not qualified is delivered, and the
// contract has no guard that yields no payload for unqualified input.
func TestStartOutputReceivesUnqualifiedInput(t *testing.T) {
	for _, qualification := range []v1.Qualification{v1.QualificationCandidate, v1.QualificationUnknown, v1.QualificationUnsupported, v1.QualificationRejected, v1.QualificationQualified} {
		c := candidate("a")
		c.Quality.Qualification = qualification
		if qualification == v1.QualificationUnsupported || qualification == v1.QualificationRejected {
			c.Value = nil
		}
		input := &syntheticInput{cut: driver.SemanticCut{Ref: cutRef("a"), Snapshots: []v1.Snapshot{{Facts: []v1.FactEnvelope{{Candidates: []v1.FactCandidate{c}}}}}}}
		output := &syntheticOutput{}
		if err := output.StartOutput(context.Background(), input, &syntheticOperations{}); err != nil {
			t.Fatalf("%s: %v", qualification, err)
		}
		if len(output.delivered) != 1 || output.delivered[0].Quality.Qualification != qualification {
			t.Errorf("%s: delivered %d candidates", qualification, len(output.delivered))
		}
	}
}
