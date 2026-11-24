package sources

import (
	"sort"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// SourceKey identifies a source in the map of member evaluations. A
// driver.SourceRef carries a fact key with slices, so it cannot be a Go map key;
// KeyOf derives the key from its whole identity.
type SourceKey string

// KeyOf returns the map key of a source reference.
func KeyOf(ref driver.SourceRef) SourceKey { return SourceKey(refKey(ref)) }

// MemberEvaluation is the scoped basis of one source: the immutable owning
// member and its evaluation at the selected captured context. The capture
// builds the map from the ownership established at the publication commit.
type MemberEvaluation struct {
	Member v1.Snapshot
	View   v1.EvaluationView
}

// Disposition says why a source shows the state it shows.
type Disposition string

// Disposition values.
const (
	DispositionCurrent        Disposition = "current"
	DispositionStopped        Disposition = "stopped"
	DispositionNoSample       Disposition = "no_sample"
	DispositionNoBasis        Disposition = "no_basis"
	DispositionExpired        Disposition = "expired"
	DispositionWithdrawn      Disposition = "withdrawn"
	DispositionDependencyLost Disposition = "dependency_lost"
)

// SourceState is the evaluated state of one source of an element. Latest is the
// sample served as current: it is nil when no sample exists, when its value
// expired, or when it was removed.
type SourceState struct {
	Source       driver.Source
	Disposition  Disposition
	Availability v1.Availability
	Freshness    v1.Freshness
	Latest       *driver.Sample
	Expired      bool
}

// ElementState is the resolution of one element with the evaluated state the
// driver.Resolution has no field for: the winner's value, availability and
// freshness, every source state, and the sample comparisons of tied pairs.
// The Resolution carries one finding per pair, so a tied pair carries its
// priority tie there and its sample comparison in TiedComparisons.
type ElementState struct {
	Resolution      driver.Resolution
	States          []SourceState
	Value           *v1.Value
	Availability    v1.Availability
	Freshness       v1.Freshness
	UnmappedNative  *v1.Value
	TiedComparisons []driver.Finding
}

// Resolve resolves one element. The evals map holds, per source, the member and
// evaluation the capture scoped to it.
func (t *Table) Resolve(el driver.Element, evals map[SourceKey]MemberEvaluation) driver.Resolution {
	return t.Element(el, evals).Resolution
}

// Element resolves one element and returns its full evaluated state.
func (t *Table) Element(el driver.Element, evals map[SourceKey]MemberEvaluation) ElementState {
	t.mu.Lock()
	defer t.mu.Unlock()
	group := make([]*entry, 0)
	want := elementKey(el)
	for _, e := range t.entries {
		if elementKey(e.source.For) == want {
			group = append(group, e)
		}
	}
	result := ElementState{Availability: v1.AvailabilityUnavailable, Freshness: v1.FreshnessUnknown}
	if len(group) == 0 {
		result.Resolution = driver.Resolution{Sources: []driver.SourceView{}}
		return result
	}
	best := leader(group)
	sort.Slice(group, func(i, j int) bool {
		if group[i] == best || group[j] == best {
			return group[i] == best
		}
		if group[i].source.Priority != group[j].source.Priority {
			return group[i].source.Priority > group[j].source.Priority
		}
		return compareRef(refOf(group[i].source), refOf(group[j].source)) < 0
	})
	winner := refOf(best.source)
	result.Resolution.Winner = &winner
	result.Resolution.Sources = make([]driver.SourceView, 0, len(group))
	for _, e := range group {
		state := evaluate(e, evals)
		result.States = append(result.States, state)
		result.Resolution.Sources = append(result.Resolution.Sources, driver.SourceView{Source: clone(e.source), Availability: state.Availability})
	}
	top := result.States[0]
	result.Availability, result.Freshness = top.Availability, top.Freshness
	if top.Latest != nil && top.Latest.Candidate.Value != nil {
		value := clone(*top.Latest.Candidate.Value)
		if unmapped(value) {
			result.UnmappedNative = &value
		} else {
			result.Value = &value
		}
	}
	result.Resolution.Findings, result.TiedComparisons = findings(result.States)
	return result
}

func unmapped(v v1.Value) bool {
	if v.Symbol != nil && !v.Symbol.Known {
		return true
	}
	for _, s := range v.Symbols {
		if !s.Known {
			return true
		}
	}
	return false
}

// leader selects the authority among the declarations of one element: the
// highest declared priority; among the tied leaders the latest runtime receipt;
// among indistinguishable receipts the lower source key. Receipts in different
// monotonic epochs are never ordered against each other, so the receipt orders
// the tied leaders only when every one of them holds a receipt of one epoch;
// a pairwise switch between receipt and key order would not be transitive. A
// leader with no receipt ranks below one with a receipt.
func leader(group []*entry) *entry {
	top := group[0].source.Priority
	for _, e := range group[1:] {
		if e.source.Priority > top {
			top = e.source.Priority
		}
	}
	leaders := make([]*entry, 0, len(group))
	for _, e := range group {
		if e.source.Priority == top {
			leaders = append(leaders, e)
		}
	}
	withReceipt := make([]*entry, 0, len(leaders))
	for _, e := range leaders {
		if e.acq != nil {
			withReceipt = append(withReceipt, e)
		}
	}
	if len(withReceipt) > 0 {
		leaders = withReceipt
	}
	byTime := len(withReceipt) > 1
	for _, e := range withReceipt {
		if e.acq.stamp.At.ClockEpochID != withReceipt[0].acq.stamp.At.ClockEpochID {
			byTime = false
		}
	}
	best := leaders[0]
	for _, e := range leaders[1:] {
		if byTime {
			x, xok := bigOf(string(e.acq.stamp.At.Nanoseconds))
			y, yok := bigOf(string(best.acq.stamp.At.Nanoseconds))
			if xok && yok && x.Cmp(y) != 0 {
				if x.Cmp(y) > 0 {
					best = e
				}
				continue
			}
		}
		if compareRef(refOf(e.source), refOf(best.source)) < 0 {
			best = e
		}
	}
	return best
}

func effective(stored v1.Availability, freshness v1.Freshness) v1.Availability {
	if stored == v1.AvailabilityWithdrawn || freshness == v1.FreshnessFresh {
		return stored
	}
	if freshness == v1.FreshnessExpired {
		return v1.AvailabilityUnavailable
	}
	if stored == v1.AvailabilityAvailable && (freshness == v1.FreshnessStale || freshness == v1.FreshnessUnknown) {
		return v1.AvailabilityDegraded
	}
	return stored
}

func evaluate(e *entry, evals map[SourceKey]MemberEvaluation) SourceState {
	state := SourceState{Source: clone(e.source), Availability: v1.AvailabilityUnavailable, Freshness: v1.FreshnessUnknown, Disposition: DispositionNoSample}
	if e.sample == nil {
		switch e.removal {
		case ExplicitWithdrawal:
			state.Disposition, state.Availability = DispositionWithdrawn, v1.AvailabilityWithdrawn
		case DependencyClosure:
			state.Disposition = DispositionDependencyLost
		}
		return state
	}
	c := e.sample.candidate
	basis, ok := evals[KeyOf(refOf(e.source))]
	state.Disposition = DispositionNoBasis
	if !ok || basis.Member.AssetID != e.asset || basis.View.SnapshotID != basis.Member.SnapshotID || basis.View.Revisions != basis.Member.Revisions {
		return state
	}
	var fact *v1.EvaluatedFact
	for i := range basis.View.Facts {
		if basis.View.Facts[i].CandidateID == c.CandidateID && basis.View.Facts[i].CandidateRevision == c.Revision {
			fact = &basis.View.Facts[i]
		}
	}
	serve := func() {
		state.Latest = &driver.Sample{From: clone(e.source), Candidate: clone(c), Receipt: e.acq.stamp}
	}
	if e.stopped {
		switch {
		case fact != nil && fact.Freshness == v1.FreshnessExpired:
			state.Disposition, state.Freshness, state.Availability, state.Expired = DispositionExpired, fact.Freshness, fact.EffectiveAvailability, true
		case fact != nil || retained(basis.View, c):
			state.Disposition, state.Freshness = DispositionStopped, v1.FreshnessStale
			state.Availability = effective(c.Quality.Availability, v1.FreshnessStale)
			serve()
		default:
			state.Disposition, state.Freshness, state.Expired = DispositionExpired, v1.FreshnessExpired, true
			state.Availability = effective(c.Quality.Availability, v1.FreshnessExpired)
		}
		return state
	}
	if fact == nil || !carries(basis.Member, c.Key, c.CandidateID, c.Revision) {
		return state
	}
	state.Freshness, state.Availability = fact.Freshness, fact.EffectiveAvailability
	if fact.Freshness == v1.FreshnessExpired {
		state.Disposition, state.Expired = DispositionExpired, true
		return state
	}
	state.Disposition = DispositionCurrent
	serve()
	return state
}

func retained(view v1.EvaluationView, c v1.FactCandidate) bool {
	for _, r := range view.Retained {
		if r.Observation.Candidate.CandidateID == c.CandidateID && r.Observation.Candidate.Revision == c.Revision && r.Freshness != v1.FreshnessExpired {
			return true
		}
	}
	return false
}
