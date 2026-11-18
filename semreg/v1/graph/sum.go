package graph

import (
	"math/big"
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// SumReason is why a member sum is withheld (SUM-2, SUM-3, SUM-4, SUM-5).
type SumReason string

// SumReason values. A withheld sum is not zero and not a partial total.
const (
	SumNoCoverage        SumReason = "no_coverage"
	SumSameUnit          SumReason = "same_unit"
	SumMemberUnavailable SumReason = "member_unavailable"
	SumMemberConflict    SumReason = "member_conflict"
	SumPartialCut        SumReason = "partial_cut"
	SumMixedSign         SumReason = "mixed_sign_reference"
	SumNativeConvention  SumReason = "native_convention"
)

// MemberSum is a sum of member facts or a withheld reason. Observed carries
// a source-reported aggregate of the scope and is never merged into Value
// (SUM-6).
type MemberSum struct {
	Withheld bool             `json:"withheld"`
	Reason   SumReason        `json:"reason,omitempty"`
	Value    *semreg.Quantity `json:"value,omitempty"`
	Observed []LensFact       `json:"observed,omitempty"`
}

// SumMembers adds the member facts of one definition on the resources reached
// from scope through coherent contains or groups instances (SUM-1, SUM-2).
// When those facts name a subject kind, only reached resources of that kind
// are summands, so a container of another kind does not withhold the sum.
// An aggregate is never added. The scope's own aggregate is returned beside
// the sum.
func SumMembers(result LensResult, fact semreg.DefinitionRef, scope semreg.ResourceID) (MemberSum, error) {
	if err := fact.Validate(); err != nil {
		return MemberSum{}, err
	}
	if err := scope.Validate(); err != nil {
		return MemberSum{}, err
	}
	observed := observedAggregates(result, scope)
	if result.Cut.Completeness == CutPartial && len(result.Cut.NotIncluded) > 0 {
		return withheldSum(SumPartialCut, observed), nil
	}
	members := scopeMembers(result, scope)
	if len(members) == 0 {
		return withheldSum(SumNoCoverage, observed), nil
	}
	if sameUnitMembers(result, members) {
		return withheldSum(SumSameUnit, observed), nil
	}
	subjects := sumSubjects(result, fact, members)
	quantities := make([]semreg.Quantity, 0, len(subjects))
	chosen := make([]LensFact, 0, len(subjects))
	conflict := false
	unavailable := false
	for _, id := range subjects {
		facts := memberFacts(result, fact, id)
		quantity, reason, err := classifyMember(facts)
		if err != nil {
			return MemberSum{}, err
		}
		switch reason {
		case SumMemberConflict:
			conflict = true
		case SumMemberUnavailable:
			unavailable = true
		default:
			quantities = append(quantities, quantity)
			chosen = append(chosen, facts[0])
		}
	}
	if conflict {
		return withheldSum(SumMemberConflict, observed), nil
	}
	if unavailable {
		return withheldSum(SumMemberUnavailable, observed), nil
	}
	if reason, err := signReason(chosen); err != nil || reason != "" {
		if err != nil {
			return MemberSum{}, err
		}
		return withheldSum(reason, observed), nil
	}
	value, err := canonicalSum(quantities)
	if err != nil {
		return MemberSum{}, err
	}
	return MemberSum{Value: &value, Observed: observed}, nil
}

func withheldSum(reason SumReason, observed []LensFact) MemberSum {
	return MemberSum{Withheld: true, Reason: reason, Observed: observed}
}

func observedAggregates(result LensResult, scope semreg.ResourceID) []LensFact {
	var out []LensFact
	for _, fact := range result.Facts {
		if fact.Subject == scope && fact.Aggregation == AggregationAggregate {
			out = append(out, fact)
		}
	}
	return out
}

// sumSubjects returns the reached members that are summands of fact.
// With no member fact of that definition, or with a member whose kind is
// unnamed, every reached member stays a summand. Otherwise the facts name
// the subject kinds, and a reached resource of another kind is left out.
func sumSubjects(result LensResult, fact semreg.DefinitionRef, members []semreg.ResourceID) []semreg.ResourceID {
	kindOf := make(map[semreg.ResourceID]semreg.DefinitionID, len(result.Resources))
	for _, resource := range result.Resources {
		kindOf[resource.Resource.ResourceID] = resource.Resource.Kind
	}
	for _, id := range members {
		if kindOf[id] == "" {
			return members
		}
	}
	memberSet := make(map[semreg.ResourceID]struct{}, len(members))
	for _, id := range members {
		memberSet[id] = struct{}{}
	}
	kinds := map[semreg.DefinitionID]struct{}{}
	for _, item := range result.Facts {
		if item.Aggregation != AggregationMember || item.Definition != fact {
			continue
		}
		if _, ok := memberSet[item.Subject]; !ok {
			continue
		}
		if kind := kindOf[item.Subject]; kind != "" {
			kinds[kind] = struct{}{}
		}
	}
	if len(kinds) == 0 {
		return members
	}
	subjects := make([]semreg.ResourceID, 0, len(members))
	for _, id := range members {
		if _, ok := kinds[kindOf[id]]; ok {
			subjects = append(subjects, id)
		}
	}
	if len(subjects) == 0 {
		return members
	}
	return subjects
}

func scopeMembers(result LensResult, scope semreg.ResourceID) []semreg.ResourceID {
	type edge struct {
		whole semreg.ResourceID
		part  semreg.ResourceID
	}
	var edges []edge
	for _, relation := range result.Relations {
		if relation.State != semreg.LinkQualified || relation.Coherence != CoherenceCoherent {
			continue
		}
		if relation.CoreType != CoreContains && relation.CoreType != CoreGroups {
			continue
		}
		whole, part := relation.From, relation.To
		if relation.Orientation == OrientationInverse {
			whole, part = part, whole
		}
		edges = append(edges, edge{whole: whole, part: part})
	}
	seen := map[semreg.ResourceID]struct{}{scope: {}}
	current := []semreg.ResourceID{scope}
	var members []semreg.ResourceID
	for len(current) > 0 {
		var next []semreg.ResourceID
		for _, id := range current {
			for _, item := range edges {
				if item.whole != id {
					continue
				}
				if _, ok := seen[item.part]; ok {
					continue
				}
				seen[item.part] = struct{}{}
				members = append(members, item.part)
				next = append(next, item.part)
			}
		}
		current = next
	}
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	return members
}

func sameUnitMembers(result LensResult, members []semreg.ResourceID) bool {
	set := map[semreg.ResourceID]struct{}{}
	for _, id := range members {
		set[id] = struct{}{}
	}
	for _, finding := range result.Findings {
		if finding.Kind != CutSameUnit {
			continue
		}
		count := 0
		for _, resource := range finding.Resources {
			if _, ok := set[resource.ResourceID]; ok {
				count++
			}
		}
		if count >= 2 {
			return true
		}
	}
	return false
}

func memberFacts(result LensResult, fact semreg.DefinitionRef, subject semreg.ResourceID) []LensFact {
	var out []LensFact
	for _, item := range result.Facts {
		if item.Subject == subject && item.Aggregation == AggregationMember && item.Definition == fact {
			out = append(out, item)
		}
	}
	return out
}

func classifyMember(facts []LensFact) (semreg.Quantity, SumReason, error) {
	if len(facts) == 0 {
		return semreg.Quantity{}, SumMemberUnavailable, nil
	}
	if len(facts) > 1 {
		return semreg.Quantity{}, SumMemberConflict, nil
	}
	fact := facts[0]
	if len(fact.Conflicts) > 0 {
		return semreg.Quantity{}, SumMemberConflict, nil
	}
	if len(fact.Candidates) == 0 {
		return semreg.Quantity{}, SumMemberUnavailable, nil
	}
	rows := map[semreg.CandidateID]semreg.EvaluatedFact{}
	for _, row := range fact.Evaluation {
		rows[row.CandidateID] = row
	}
	var quantity *semreg.Quantity
	unequal := false
	unusable := false
	for _, candidate := range fact.Candidates {
		row, ok := rows[candidate.CandidateID]
		if !ok || !memberUsable(row) {
			unusable = true
		}
		if candidate.Value == nil {
			unusable = true
			continue
		}
		if candidate.Value.Kind != semreg.ValueQuantity || candidate.Value.Quantity == nil {
			return semreg.Quantity{}, "", sumError(semreg.InvalidValue, "member fact is not a quantity")
		}
		if quantity == nil {
			copied := *candidate.Value.Quantity
			quantity = &copied
			continue
		}
		if quantity.Unit != candidate.Value.Quantity.Unit || quantity.Number != candidate.Value.Quantity.Number {
			unequal = true
		}
	}
	if unequal {
		return semreg.Quantity{}, SumMemberConflict, nil
	}
	if unusable || quantity == nil {
		return semreg.Quantity{}, SumMemberUnavailable, nil
	}
	return *quantity, "", nil
}

func memberUsable(row semreg.EvaluatedFact) bool {
	if row.Freshness != semreg.FreshnessFresh {
		return false
	}
	return row.EffectiveAvailability == semreg.AvailabilityAvailable || row.EffectiveAvailability == semreg.AvailabilityDegraded
}

func signReason(facts []LensFact) (SumReason, error) {
	signed := 0
	for _, fact := range facts {
		if fact.Direction == DirectionSigned {
			signed++
		}
	}
	if signed == 0 {
		return "", nil
	}
	if signed != len(facts) {
		return SumMixedSign, nil
	}
	var relation *semreg.DefinitionRef
	native := 0
	assets := map[semreg.AssetID]struct{}{}
	for _, fact := range facts {
		assets[fact.AssetID] = struct{}{}
		if fact.Sign == nil || fact.Sign.NativeConvention == (fact.Sign.Relation != nil) {
			return SumMixedSign, nil
		}
		if fact.Sign.NativeConvention {
			native++
			continue
		}
		if relation == nil {
			copied := *fact.Sign.Relation
			relation = &copied
			continue
		}
		if *relation != *fact.Sign.Relation {
			return SumMixedSign, nil
		}
	}
	if native == len(facts) {
		if len(assets) > 1 {
			return SumNativeConvention, nil
		}
		return "", nil
	}
	if native != 0 || relation == nil {
		return SumMixedSign, nil
	}
	return "", nil
}

func canonicalSum(values []semreg.Quantity) (semreg.Quantity, error) {
	if len(values) == 0 {
		return semreg.Quantity{}, sumError(semreg.InvalidValue, "empty sum")
	}
	unit := values[0].Unit
	if err := unit.Validate(); err != nil {
		return semreg.Quantity{}, err
	}
	sum := new(big.Int)
	ten := big.NewInt(10)
	for _, value := range values {
		if value.Unit != unit {
			return semreg.Quantity{}, sumError(semreg.InvalidValue, "member units differ")
		}
		if value.Number.Exponent10 < -18 || value.Number.Exponent10 > 18 {
			return semreg.Quantity{}, sumError(semreg.InvalidDecimal, "member decimal")
		}
		coefficient := new(big.Int)
		if _, ok := coefficient.SetString(value.Number.Coefficient, 10); !ok {
			return semreg.Quantity{}, sumError(semreg.InvalidDecimal, "member decimal")
		}
		shift := int64(value.Number.Exponent10) + 18
		scale := new(big.Int).Exp(ten, big.NewInt(shift), nil)
		sum.Add(sum, new(big.Int).Mul(coefficient, scale))
	}
	exponent := int32(-18)
	for sum.Sign() != 0 {
		remainder := new(big.Int)
		quotient, _ := new(big.Int).QuoRem(sum, ten, remainder)
		if remainder.Sign() != 0 {
			break
		}
		if exponent == 18 {
			return semreg.Quantity{}, sumError(semreg.InvalidDecimal, "sum exponent")
		}
		sum = quotient
		exponent++
	}
	if sum.Sign() == 0 {
		return semreg.Quantity{Number: semreg.Decimal{Coefficient: "0", Exponent10: 0}, Unit: unit}, nil
	}
	return semreg.Quantity{Number: semreg.Decimal{Coefficient: sum.String(), Exponent10: exponent}, Unit: unit}, nil
}

func sumError(id semreg.ErrorID, detail string) error {
	return &semreg.Error{ID: id, Detail: "graph sum: " + detail}
}
