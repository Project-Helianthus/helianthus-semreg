package sources

import (
	"errors"
	"fmt"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Input is the exact input proposition one InputRef names in its captured cut:
// the stored candidate with its unit, quality and observation times as held,
// the evaluated state, and the formula when the input is itself derived.
type Input struct {
	Candidate  v1.FactCandidate
	Evaluation v1.EvaluatedFact
	Formula    *driver.Formula
}

// InputResolver reads one InputRef from the exact retained, scoped capture it
// names. It reports false when the subject, candidate or revision is not in that
// capture; a newer revision, another member with an equal candidate id or a
// hidden member never substitutes.
type InputResolver func(driver.InputRef) (Input, bool)

// InputState is one input of a derived value as held.
type InputState struct {
	Ref   driver.InputRef
	Found bool
	Input Input
}

// Lineage is the validated lineage of a derived value. Freshness is the
// combination of the inputs' states, unknown when an input is missing or
// withdrawn; Named lists the inputs that make it unknown, stale or expired.
type Lineage struct {
	Origin    driver.Origin
	Inputs    []InputState
	Freshness v1.Freshness
	Named     []driver.InputRef
}

func inputKey(r driver.InputRef) string {
	return fmt.Sprintf("%q|%q|%q|%q|%q", r.At.CutID, r.Subject.Bridge, r.Subject.ID, r.Candidate, r.Revision)
}

func combine(left, right v1.Freshness) v1.Freshness {
	order := map[v1.Freshness]int{v1.FreshnessFresh: 0, v1.FreshnessStale: 1, v1.FreshnessUnknown: 2, v1.FreshnessExpired: 3}
	if order[right] > order[left] {
		return right
	}
	return left
}

// CheckLineage validates the lineage of the derived value self: every input
// resolves in its pinned cut, the input graph has no cycle (derivation_cycle),
// and a missing, stale or withdrawn input propagates to the derived freshness
// with the input named. The unit, quality and times of each input stay as held.
// A measured or declared origin carries no formula.
func CheckLineage(self driver.InputRef, origin driver.Origin, formula *driver.Formula, resolve InputResolver) (Lineage, error) {
	if err := first(self.Validate(), origin.Validate()); err != nil {
		return Lineage{}, err
	}
	result := Lineage{Origin: origin, Freshness: v1.FreshnessFresh}
	if origin.Kind != driver.OriginDerived {
		if formula != nil {
			return Lineage{}, errors.New("only a derived value carries a formula")
		}
		return result, nil
	}
	if formula == nil {
		return Lineage{}, errors.New("a derived value needs a formula")
	}
	if err := formula.Validate(); err != nil {
		return Lineage{}, err
	}
	if resolve == nil {
		return Lineage{}, errors.New("an input resolver is required")
	}
	onPath := map[string]bool{inputKey(self): true}
	var walk func(f driver.Formula, top bool) error
	walk = func(f driver.Formula, top bool) error {
		for _, ref := range f.Inputs {
			input, found := resolve(ref)
			if top {
				result.Inputs = append(result.Inputs, InputState{Ref: ref, Found: found, Input: clone(input)})
			}
			if !found {
				result.Freshness = combine(result.Freshness, v1.FreshnessUnknown)
				result.Named = append(result.Named, ref)
				continue
			}
			key := inputKey(ref)
			if onPath[key] {
				return &v1.Error{ID: v1.DerivationCycle, Detail: "lineage"}
			}
			contribution := input.Evaluation.Freshness
			if input.Evaluation.EffectiveAvailability == v1.AvailabilityWithdrawn {
				contribution = v1.FreshnessUnknown
			}
			if contribution != v1.FreshnessFresh {
				result.Named = append(result.Named, ref)
			}
			result.Freshness = combine(result.Freshness, contribution)
			if input.Formula != nil {
				onPath[key] = true
				err := walk(*input.Formula, false)
				delete(onPath, key)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(*formula, true); err != nil {
		return Lineage{}, err
	}
	return result, nil
}

func first(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
