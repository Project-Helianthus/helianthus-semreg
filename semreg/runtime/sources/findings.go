package sources

import (
	"math/big"
	"sort"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func rat(d v1.Decimal) *big.Rat {
	n, _ := new(big.Int).SetString(d.Coefficient, 10)
	r := new(big.Rat).SetInt(n)
	exp := d.Exponent10
	if exp < 0 {
		exp = -exp
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exp)), nil)
	if d.Exponent10 < 0 {
		return r.Quo(r, new(big.Rat).SetInt(scale))
	}
	return r.Mul(r, new(big.Rat).SetInt(scale))
}

func ordered(a, b SourceState) (SourceState, SourceState) {
	if compareRef(refOf(a.Source), refOf(b.Source)) > 0 {
		return b, a
	}
	return a, b
}

func quantityOf(s SourceState) *v1.Quantity {
	if s.Latest == nil || s.Latest.Candidate.Value == nil || s.Latest.Candidate.Value.Quantity == nil {
		return nil
	}
	q := clone(*s.Latest.Candidate.Value.Quantity)
	return &q
}

func hasValue(s SourceState) bool { return s.Latest != nil && s.Latest.Candidate.Value != nil }

// findings returns the findings of an element in ascending order, one per pair,
// and the sample comparisons of pairs that also declare equal priorities.
func findings(states []SourceState) ([]driver.Finding, []driver.Finding) {
	var out []driver.Finding
	var tiedSamples []driver.Finding
	for i := 0; i < len(states); i++ {
		for j := i + 1; j < len(states); j++ {
			a, b := ordered(states[i], states[j])
			sample := compare(a, b)
			if a.Source.Priority == b.Source.Priority {
				p := a.Source.Priority
				out = append(out, driver.Finding{First: refOf(a.Source), Second: refOf(b.Source), PriorityTie: &p})
				if sample != nil {
					tiedSamples = append(tiedSamples, *sample)
				}
				continue
			}
			if sample != nil {
				out = append(out, *sample)
			}
		}
	}
	less := func(list []driver.Finding) func(i, j int) bool {
		return func(i, j int) bool {
			if c := compareRef(list[i].First, list[j].First); c != 0 {
				return c < 0
			}
			return compareRef(list[i].Second, list[j].Second) < 0
		}
	}
	sort.Slice(out, less(out))
	sort.Slice(tiedSamples, less(tiedSamples))
	return out, tiedSamples
}

// compare reports a sample finding for the ordered pair a, b: a disagreement
// when commensurate, contemporaneous values with known steps differ by more than
// the larger step, and an incomparable pair otherwise. It reports nothing when a
// side has no sample or no quantity to compare, or when the pair agrees. A pair
// with an expired side reports nothing either: the driver finding needs both SI
// values and the expired value is served nowhere, so that side's state stays
// visible in the source list and the element state instead.
func compare(a, b SourceState) *driver.Finding {
	if !hasValue(a) || !hasValue(b) {
		return nil
	}
	f := driver.Finding{First: refOf(a.Source), Second: refOf(b.Source)}
	if a.Source.Step != nil {
		step := clone(*a.Source.Step)
		f.FirstStep = &step
	}
	if b.Source.Step != nil {
		step := clone(*b.Source.Step)
		f.SecondStep = &step
	}
	f.FirstValue, f.SecondValue = quantityOf(a), quantityOf(b)
	if f.FirstValue == nil || f.SecondValue == nil {
		return nil
	}
	unit := f.FirstValue.Unit
	commensurate := unit == f.SecondValue.Unit
	contemporaneous := a.Freshness == v1.FreshnessFresh && b.Freshness == v1.FreshnessFresh
	stepsKnown := f.FirstStep != nil && f.SecondStep != nil && f.FirstStep.Unit == f.FirstValue.Unit && f.SecondStep.Unit == f.SecondValue.Unit
	if !commensurate || !contemporaneous || !stepsKnown {
		f.Incomparable = true
		return &f
	}
	diff := new(big.Rat).Sub(rat(f.FirstValue.Number), rat(f.SecondValue.Number))
	diff.Abs(diff)
	limit := rat(f.FirstStep.Number)
	if other := rat(f.SecondStep.Number); other.Cmp(limit) > 0 {
		limit = other
	}
	if diff.Cmp(limit) > 0 {
		return &f
	}
	return nil
}
