package sources

import (
	"reflect"
	"strings"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func coefficient(v *v1.Value) string {
	if v == nil || v.Quantity == nil {
		return ""
	}
	return v.Quantity.Number.Coefficient
}

func hasCoefficient(t *testing.T, v any, c string) bool {
	t.Helper()
	return strings.Contains(mustJSON(t, v), `"coefficient":"`+c+`"`)
}

func isWinner(e ElementState, d *drv) bool {
	return e.Resolution.Winner != nil && refKey(*e.Resolution.Winner) == refKey(d.ref())
}

func factFor(t *testing.T, m MemberEvaluation, id v1.CandidateID) v1.EvaluatedFact {
	t.Helper()
	for _, f := range m.View.Facts {
		if f.CandidateID == id {
			return f
		}
	}
	t.Fatalf("no evaluated fact for %s", id)
	return v1.EvaluatedFact{}
}

func threeSources(t *testing.T) (*lab, *drv, *drv, *drv) {
	l := newLab(t)
	step := quantity("5", -1)
	boiler := l.addDriver("boiler", "asset:boiler", 30, &step)
	regulator := l.addDriver("regulator", "asset:regulator", 20, &step)
	mirror := l.addDriver("mirror", "asset:mirror", 10, &step)
	return l, boiler, regulator, mirror
}

func TestThreeSourcesOnThreePathsWinnerSuppliesValue(t *testing.T) {
	l, boiler, regulator, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	regulator.publish(regulator.obs("cand:regulator", quantity("32165", -2), "1", 100, "r1"))
	mirror.publish(mirror.obs("cand:mirror", quantity("32165", -2), "1", 100, "m1"))
	l.now = 110
	e := l.element()
	if err := e.Resolution.Validate(); err != nil {
		t.Fatal(err)
	}
	if !isWinner(e, boiler) || coefficient(e.Value) != "32165" || e.Freshness != v1.FreshnessFresh || e.Availability != v1.AvailabilityAvailable {
		t.Fatalf("winner/value/state: %s", mustJSON(t, e))
	}
	names := sortedNames(e)
	if !reflect.DeepEqual(names, []string{"producer:boiler", "producer:regulator", "producer:mirror"}) {
		t.Fatalf("every source is a link, ordered by authority: %v", names)
	}
	priorities := []int32{30, 20, 10}
	paths := []string{"path:boiler", "path:regulator", "path:mirror"}
	for i, view := range e.Resolution.Sources {
		if view.Source.Priority != priorities[i] || view.Source.PathKey != paths[i] || view.Source.Step == nil || view.Availability != v1.AvailabilityAvailable {
			t.Fatalf("source %d link: %+v", i, view)
		}
		if e.States[i].Latest == nil || e.States[i].Latest.Candidate.Value == nil {
			t.Fatalf("source %d: latest sample missing", i)
		}
	}
	if len(e.Resolution.Findings) != 0 || len(e.TiedComparisons) != 0 {
		t.Fatalf("agreeing sources produce no finding: %s", mustJSON(t, e.Resolution.Findings))
	}
}

func TestTopSourceMissingRefreshesIsStaleLowerListedUnused(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "1", 100, "m1"))
	l.now = 140
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "2", 140, "m2"))
	evals := l.evals()
	if got := factFor(t, evals[KeyOf(boiler.ref())], "cand:boiler").Freshness; got != v1.FreshnessStale {
		t.Fatalf("evaluator freshness handed to Resolve: %s", got)
	}
	e := l.element()
	if !isWinner(e, boiler) || e.Freshness != v1.FreshnessStale || e.Availability != v1.AvailabilityDegraded {
		t.Fatalf("the stale winner stays authoritative: %s", mustJSON(t, e))
	}
	if coefficient(e.Value) != "32165" {
		t.Fatalf("stale winner keeps its own value: %v", e.Value)
	}
	if st := e.state(mirror); st.Freshness != v1.FreshnessFresh || coefficient(&v1.Value{Quantity: quantityOf(st)}) != "32265" {
		t.Fatalf("the lower source is listed with its own state: %+v", st)
	}
	if coefficient(e.Value) == "32265" {
		t.Fatal("the lower value was used")
	}
}

func TestTopSourceExpiredOmitsItsValueEverywhere(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "1", 100, "m1"))
	l.now = 440
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "2", 440, "m2"))
	l.now = 450
	evals := l.evals()
	fact := factFor(t, evals[KeyOf(boiler.ref())], "cand:boiler")
	if fact.Freshness != v1.FreshnessExpired {
		t.Fatalf("expected expired from the evaluator, got %s", fact.Freshness)
	}
	e := l.element()
	if !isWinner(e, boiler) || e.Freshness != v1.FreshnessExpired || e.Availability != fact.EffectiveAvailability {
		t.Fatalf("element must carry the evaluator's state: %s", mustJSON(t, e))
	}
	if e.Value != nil || e.UnmappedNative != nil {
		t.Fatalf("the mirror must not supply a value: %v", e.Value)
	}
	if len(e.Resolution.Sources) != 3 {
		t.Fatalf("both links are kept: %v", sortedNames(e))
	}
	st := e.state(boiler)
	if st.Latest != nil || !st.Expired || st.Freshness != v1.FreshnessExpired || st.Disposition != DispositionExpired {
		t.Fatalf("the expired winner's state stays visible without its sample: %+v", st)
	}
	if hasCoefficient(t, e, "32165") {
		t.Fatalf("the expired value is served: %s", mustJSON(t, e))
	}
	if e.state(mirror).Latest == nil || !hasCoefficient(t, e.state(mirror), "32265") {
		t.Fatal("the mirror stays listed with its own sample")
	}
	if err := e.Resolution.Validate(); err != nil {
		t.Fatalf("the resolution must satisfy the driver contract: %v", err)
	}
	for _, f := range e.Resolution.Findings {
		if f.PriorityTie == nil && (refKey(f.First) == refKey(boiler.ref()) || refKey(f.Second) == refKey(boiler.ref())) {
			t.Fatalf("no sample finding carries the expired side: %s", mustJSON(t, e.Resolution.Findings))
		}
	}
}

func TestStoppedGenerationWithinRetentionShowsLastValueStale(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "1", 100, "m1"))
	l.now = 110
	boiler.stop()
	l.now = 140
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "2", 140, "m2"))
	evals := l.evals()
	boilerView := evals[KeyOf(boiler.ref())].View
	for _, f := range boilerView.Facts {
		if f.CandidateID == "cand:boiler" {
			t.Fatal("the stopped candidate must be absent from the evaluation facts")
		}
	}
	if len(boilerView.Retained) != 1 {
		t.Fatalf("the stop retains the observation: %d", len(boilerView.Retained))
	}
	e := l.element()
	st := e.state(boiler)
	if !isWinner(e, boiler) || coefficient(e.Value) != "32165" || e.Freshness != v1.FreshnessStale || st.Disposition != DispositionStopped || st.Latest == nil {
		t.Fatalf("the stopped winner shows its last value stale: %s", mustJSON(t, e))
	}
	if e.state(mirror).Freshness != v1.FreshnessFresh || e.state(mirror).Latest == nil {
		t.Fatal("the lower source is listed")
	}
	if coefficient(e.Value) == "32265" {
		t.Fatal("the lower source was used")
	}
}

func TestValuePastRetentionWhileActiveThenStoppedStaysOmitted(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "1", 100, "m1"))
	l.now = 450
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "2", 450, "m2"))
	if st := l.element().state(boiler); !st.Expired {
		t.Fatalf("expired while active: %+v", st)
	}
	boiler.stop()
	l.now = 460
	evals := l.evals()
	view := evals[KeyOf(boiler.ref())].View
	if len(view.Facts) != 0 || len(view.Retained) != 0 {
		t.Fatalf("the live fact is absent and nothing is retained: %+v", view)
	}
	e := l.element()
	if !isWinner(e, boiler) || e.Value != nil || e.Freshness != v1.FreshnessExpired || e.state(boiler).Latest != nil || hasCoefficient(t, e, "32165") {
		t.Fatalf("the expired value stays omitted after the stop: %s", mustJSON(t, e))
	}
	if e.state(mirror).Latest == nil {
		t.Fatal("the mirror keeps publishing, unused")
	}
}

func TestStoppedThenPastRetentionDeadlineIsSuppressed(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "1", 100, "m1"))
	l.now = 110
	boiler.stop()
	l.now = 150
	if e := l.element(); coefficient(e.Value) != "32165" || e.Freshness != v1.FreshnessStale {
		t.Fatalf("within the deadline the stopped value is stale: %s", mustJSON(t, e))
	}
	l.now = 450
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "2", 450, "m2"))
	e := l.element()
	if e.Value != nil || e.Freshness != v1.FreshnessExpired || e.state(boiler).Latest != nil || hasCoefficient(t, e, "32165") || !isWinner(e, boiler) {
		t.Fatalf("past the deadline the value is suppressed: %s", mustJSON(t, e))
	}
}

func TestFencedStaleThenGenerationTwoWithdrawalClearsSampleAndBasis(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:shared", quantity("32165", -2), "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:shared", quantity("32265", -2), "1", 100, "m1"))
	l.now = 110
	fenced := boiler.supersede()
	if len(fenced.Retained) != 1 {
		t.Fatalf("post-fence snapshot retains the observation: %d", len(fenced.Retained))
	}
	l.now = 130
	mirror.publish(mirror.obs("cand:shared", quantity("32265", -2), "2", 130, "m2"))
	if e := l.element(); coefficient(e.Value) != "32165" || e.state(boiler).Disposition != DispositionStopped {
		t.Fatalf("fenced and kept stale: %s", mustJSON(t, e))
	}
	withdrawn := boiler.withdraw("cand:shared")
	if len(withdrawn.Retained) != 0 {
		t.Fatalf("post-withdrawal snapshot holds no retained sample: %d", len(withdrawn.Retained))
	}
	entry := l.table.entries[refKey(boiler.ref())]
	if entry.sample != nil || entry.removal != ExplicitWithdrawal || entry.acq == nil {
		t.Fatalf("saved sample and member basis must be cleared with the comparison fields kept: %+v", entry)
	}
	if !l.table.GenerationStopped(boiler.ref()) {
		t.Fatal("source not declared")
	}
	e := l.element()
	st := e.state(boiler)
	if !isWinner(e, boiler) || e.Value != nil || st.Latest != nil || st.Disposition != DispositionWithdrawn || st.Availability != v1.AvailabilityWithdrawn || hasCoefficient(t, e, "32165") {
		t.Fatalf("withdrawn without a sample, a stop cannot recreate it: %s", mustJSON(t, e))
	}
	if e.state(mirror).Latest == nil || coefficient(e.Value) == "32265" {
		t.Fatal("the mirror is listed and unused")
	}
	if l.table.WithdrawFact(mirror.ref(), boiler.asset, "cand:shared", "2", ExplicitWithdrawal) {
		t.Fatal("a withdrawal of another asset removed a source")
	}
	if l.table.WithdrawFact(mirror.ref(), mirror.asset, "cand:shared", "1", ExplicitWithdrawal) {
		t.Fatal("a withdrawal of another revision removed a source")
	}
	if e.state(mirror).Latest == nil || l.element().state(mirror).Latest == nil {
		t.Fatal("the unaffected asset with the same candidate id lost its sample")
	}
}

func derivedWinner(d *drv, id string, input v1.FactCandidate, receipt uint64) v1.FactCandidate {
	value := quantity("32165", -2)
	evidenceRef := evidence("derived:" + id)
	binding, epoch, generation := *input.BindingID, *input.SourceEpochID, *input.DriverGeneration
	return v1.FactCandidate{
		CandidateID: v1.CandidateID(id), Key: d.source.For.Key, Value: &v1.Value{Kind: v1.ValueQuantity, Quantity: &value},
		Quality:         v1.Quality{Assertion: v1.AssertionInferred, Qualification: v1.QualificationQualified, Promotion: v1.PromotionPromoted, Validity: v1.ValidityGood, Availability: v1.AvailabilityAvailable, Freshness: v1.FreshnessFresh, Reasons: []v1.DefinitionID{}},
		Times:           d.times(receipt),
		FreshnessPolicy: input.FreshnessPolicy,
		Origin:          v1.OriginRef{OriginID: v1.OriginID("origin:" + id), Kind: v1.OriginDerived, Evidence: []v1.EvidenceRef{evidenceRef}},
		Evidence:        []v1.EvidenceRef{evidenceRef},
		Derivation: &v1.Derivation{Algorithm: "algorithm.test", Version: "1.0.0", Evidence: []v1.EvidenceRef{}, Inputs: []v1.DerivationInput{{
			CandidateID: input.CandidateID, CandidateRevision: input.Revision,
			SourcePaths: []v1.SourcePathRef{{BindingID: binding, SourceID: *input.Origin.SourceID, SourceEpochID: epoch, DriverGeneration: generation}},
		}}},
		Revision: "1",
	}
}

func TestDerivedWinnerLosesInputByKernelCascadeKeepsDeclarationAndDisposition(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	input := boiler.obs("cand:input", quantity("29315", -2), "1", 100, "in1")
	input.Key = v1.FactKey{PackID: labPack.ID, PackVersion: labPack.Version, FactID: "thermal.measurement.input", Dimensions: []v1.Dimension{}}
	derived := derivedWinner(boiler, "cand:derived", input, 100)
	boiler.apply(boiler.batch(func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{derived, input} }))
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "1", 100, "m1"))
	l.now = 110
	before := l.element()
	if coefficient(before.Value) != "32165" || before.state(boiler).Latest == nil {
		t.Fatalf("the derived winner is served before the input changes: %s", mustJSON(t, before))
	}
	capture := mustJSON(t, before)

	changed := input
	changed.Revision = "2"
	changed.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: &v1.Quantity{Number: v1.Decimal{Coefficient: "29415", Exponent10: -2}, Unit: kelvin}}
	batch := boiler.batch(func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{changed} })
	if len(batch.FactWithdrawals) != 0 || len(batch.GenerationFences) != 0 {
		t.Fatal("the input-only commit names no withdrawal and no stop")
	}
	snapshot, err := boiler.apply(batch)
	if err != nil {
		t.Fatal(err)
	}
	if candidate(snapshot, "cand:derived") != nil {
		t.Fatal("the kernel must drop the derived candidate on the input change")
	}
	e := l.element()
	st := e.state(boiler)
	if !isWinner(e, boiler) || len(e.Resolution.Sources) != 3 || st.Disposition != DispositionDependencyLost || st.Availability != v1.AvailabilityUnavailable || st.Freshness != v1.FreshnessUnknown || st.Latest != nil {
		t.Fatalf("declaration, links and dependency-loss disposition: %s", mustJSON(t, e))
	}
	if e.Value != nil || e.Availability != v1.AvailabilityUnavailable || e.Freshness != v1.FreshnessUnknown || hasCoefficient(t, e, "32165") {
		t.Fatalf("the former value is served: %s", mustJSON(t, e))
	}
	if e.state(mirror).Latest == nil || mustJSON(t, before) != capture {
		t.Fatal("the mirror is listed unused and the earlier capture is unchanged")
	}
	boiler.stop()
	if after := l.element(); after.state(boiler).Disposition != DispositionDependencyLost || after.state(boiler).Latest != nil {
		t.Fatalf("a later stop cannot recreate the basis: %s", mustJSON(t, after))
	}
}

func TestDeclaredWinnerWithoutSampleKeepsElementUnknown(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "1", 100, "m1"))
	e := l.element()
	st := e.state(boiler)
	if !isWinner(e, boiler) || e.Value != nil || e.Freshness != v1.FreshnessUnknown || e.Availability != v1.AvailabilityUnavailable || st.Disposition != DispositionNoSample || st.Latest != nil {
		t.Fatalf("a winner that has not published is unknown: %s", mustJSON(t, e))
	}
	if e.state(mirror).Latest == nil || len(e.Resolution.Sources) != 3 {
		t.Fatal("lower sources are listed, none supplies a value")
	}
}

func TestMirrorOnlyCommitOnTheSameAssetKeepsWinnerCandidateAndReceipt(t *testing.T) {
	l := newLab(t)
	step := quantity("5", -1)
	boiler := l.addDriver("boiler", "asset:site", 30, &step)
	mirror := l.addDriver("mirror", "asset:site", 10, &step)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	l.now = 105
	first := l.element()
	firstMember := l.evals()[KeyOf(boiler.ref())].Member
	mirror.publish(mirror.obs("cand:mirror", quantity("32165", -2), "1", 105, "m1"))
	second := l.element()
	secondMember := l.evals()[KeyOf(boiler.ref())].Member
	if firstMember.SnapshotID == secondMember.SnapshotID || firstMember.Revisions == secondMember.Revisions {
		t.Fatal("the mirror-only commit must advance the member snapshot and revision vector")
	}
	a, b := first.state(boiler).Latest, second.state(boiler).Latest
	if a == nil || b == nil || a.Candidate.CandidateID != b.Candidate.CandidateID || a.Candidate.Revision != b.Candidate.Revision || a.Receipt != b.Receipt {
		t.Fatalf("candidate id, revision and receipt must be preserved: %s %s", mustJSON(t, a), mustJSON(t, b))
	}
	l.now = 140
	third := l.element()
	if third.state(boiler).Freshness != v1.FreshnessStale || third.state(boiler).Latest.Receipt != a.Receipt {
		t.Fatalf("freshness comes from the new member evaluation with no receipt refresh: %s", mustJSON(t, third.state(boiler)))
	}
}

func TestValuelessWithdrawnWinnerEvaluatedExpiredKeepsBothStates(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "1", 100, "m1"))
	l.now = 120
	valueless := boiler.obs("cand:boiler", quantity("32165", -2), "2", 100, "b1")
	valueless.Value = nil
	valueless.Quality.Availability = v1.AvailabilityWithdrawn
	boiler.publish(valueless)
	if e := l.element(); e.Value != nil || e.Availability != v1.AvailabilityWithdrawn || e.state(boiler).Latest == nil || e.state(boiler).Latest.Candidate.Revision != "2" || hasCoefficient(t, e.state(boiler), "32165") {
		t.Fatalf("the valueless revision suppresses the old sample: %s", mustJSON(t, e))
	}
	l.now = 450
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "2", 450, "m2"))
	evals := l.evals()
	fact := factFor(t, evals[KeyOf(boiler.ref())], "cand:boiler")
	if fact.Freshness != v1.FreshnessExpired || fact.EffectiveAvailability != v1.AvailabilityWithdrawn {
		t.Fatalf("evaluator: %+v", fact)
	}
	e := l.element()
	if !isWinner(e, boiler) || e.Freshness != v1.FreshnessExpired || e.Availability != v1.AvailabilityWithdrawn || e.Value != nil || hasCoefficient(t, e, "32165") {
		t.Fatalf("both exact states stay visible without a lower substitute: %s", mustJSON(t, e))
	}
	l.table.GenerationStopped(boiler.ref())
	if later := l.element(); later.Value != nil || hasCoefficient(t, later, "32165") {
		t.Fatalf("stopped retention does not bring the old sample back: %s", mustJSON(t, later))
	}
}

func TestFreshReplacementCandidateUsesItsOwnEvaluation(t *testing.T) {
	l, boiler, _, _ := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	oldCut := l.evals()
	l.now = 200
	boiler.publish(boiler.obs("cand:boiler", quantity("32215", -2), "2", 200, "b2"))
	l.now = 210
	e := l.element()
	st := e.state(boiler)
	if st.Freshness != v1.FreshnessFresh || st.Latest == nil || st.Latest.Candidate.Revision != "2" || coefficient(e.Value) != "32215" {
		t.Fatalf("the replacement uses its own evaluated state: %s", mustJSON(t, st))
	}
	stale := l.table.Element(dhwElement(), oldCut)
	if st := stale.state(boiler); st.Disposition != DispositionNoBasis || st.Latest != nil || stale.Value != nil {
		t.Fatalf("an earlier member that lacks the saved revision cannot serve it: %s", mustJSON(t, st))
	}
}

func TestJoinedMembersWithEqualCandidateIdsKeepTheirOwnEvaluation(t *testing.T) {
	l, boiler, regulator, mirror := threeSources(t)
	l.now = 100
	regulator.publish(regulator.obs("cand:same", quantity("32165", -2), "1", 100, "r1"))
	l.now = 140
	boiler.publish(boiler.obs("cand:same", quantity("32165", -2), "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:same", quantity("32165", -2), "1", 140, "m1"))
	l.now = 150
	forward := l.evals()
	reversed := map[SourceKey]MemberEvaluation{}
	for _, d := range []*drv{mirror, regulator, boiler} {
		reversed[KeyOf(d.ref())] = forward[KeyOf(d.ref())]
	}
	a, b := l.table.Element(dhwElement(), forward), l.table.Element(dhwElement(), reversed)
	if mustJSON(t, a) != mustJSON(t, b) {
		t.Fatal("the order the evaluations are supplied in changes the result")
	}
	if a.state(boiler).Freshness != v1.FreshnessStale || a.state(regulator).Freshness != v1.FreshnessStale || a.state(mirror).Freshness != v1.FreshnessFresh || a.Freshness != v1.FreshnessStale {
		t.Fatalf("each source keeps its own state, the top one stale: %s", mustJSON(t, a.States))
	}
	swapped := map[SourceKey]MemberEvaluation{KeyOf(boiler.ref()): forward[KeyOf(mirror.ref())], KeyOf(regulator.ref()): forward[KeyOf(regulator.ref())], KeyOf(mirror.ref()): forward[KeyOf(boiler.ref())]}
	s := l.table.Element(dhwElement(), swapped)
	if s.state(boiler).Disposition != DispositionNoBasis || s.state(mirror).Disposition != DispositionNoBasis {
		t.Fatal("a member of another asset must not serve as a basis")
	}
}

func TestExplicitWithdrawalOfWinnerMakesNextDeclaredWinner(t *testing.T) {
	l, boiler, regulator, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	regulator.publish(regulator.obs("cand:regulator", quantity("32185", -2), "1", 100, "r1"))
	mirror.publish(mirror.obs("cand:mirror", quantity("32205", -2), "1", 100, "m1"))
	l.table.Withdraw(boiler.ref())
	e := l.element()
	if !isWinner(e, regulator) || coefficient(e.Value) != "32185" || len(e.Resolution.Sources) != 2 {
		t.Fatalf("the next declaration by priority wins: %s", mustJSON(t, e.Resolution))
	}
	if err := l.table.Admit(driver.Sample{From: boiler.source, Candidate: boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1")}, l.view(boiler.asset).Member); err != ErrUndeclared {
		t.Fatalf("a withdrawn declaration admits nothing: %v", err)
	}
}

func TestAdmissionPreservesUnavailableObservationNeverFallsToLowerSource(t *testing.T) {
	l, boiler, _, mirror := threeSources(t)
	l.now = 100
	boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	mirror.publish(mirror.obs("cand:mirror", quantity("32265", -2), "1", 100, "m1"))
	l.now = 110
	notReady := boiler.obs("cand:boiler", quantity("32165", -2), "2", 100, "b1")
	notReady.Quality.Availability = v1.AvailabilityUnavailable
	notReady.Quality.Validity = v1.ValidityUnknown
	notReady.Quality.Qualification = v1.QualificationCandidate
	notReady.Quality.Promotion = v1.PromotionUnpromoted
	boiler.publish(notReady)
	e := l.element()
	if !isWinner(e, boiler) || e.Availability != v1.AvailabilityUnavailable || coefficient(e.Value) != "32165" || e.state(boiler).Latest.Candidate.Quality.Validity != v1.ValidityUnknown {
		t.Fatalf("an unavailable producer keeps its last accepted value as supplied: %s", mustJSON(t, e))
	}
}

func TestAdmitRejectsWhatItCannotAttach(t *testing.T) {
	l, boiler, _, _ := threeSources(t)
	l.now = 100
	snapshot := boiler.publish(boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1"))
	good := boiler.obs("cand:boiler", quantity("32165", -2), "1", 100, "b1")
	other := boiler.source
	other.PathKey = "path:other"
	if err := l.table.Admit(driver.Sample{From: other, Candidate: good}, snapshot); err != ErrUndeclared {
		t.Fatalf("undeclared source: %v", err)
	}
	wrongKey := good
	wrongKey.Key = v1.FactKey{PackID: labPack.ID, PackVersion: labPack.Version, FactID: "thermal.measurement.other", Dimensions: []v1.Dimension{}}
	if err := l.table.Admit(driver.Sample{From: boiler.source, Candidate: wrongKey}, snapshot); err == nil {
		t.Fatal("a candidate of another fact key was attached")
	}
	missing := good
	missing.CandidateID = "cand:absent"
	if err := l.table.Admit(driver.Sample{From: boiler.source, Candidate: missing}, snapshot); err == nil {
		t.Fatal("a candidate the member does not carry was admitted")
	}
	wrongRevision := good
	wrongRevision.Revision = "9"
	if err := l.table.Admit(driver.Sample{From: boiler.source, Candidate: wrongRevision}, snapshot); err == nil {
		t.Fatal("a revision the member does not carry was admitted")
	}
}
