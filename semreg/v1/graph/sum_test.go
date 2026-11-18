package graph_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func sumCutID() semreg.Digest {
	sum := sha256.Sum256([]byte("sum-cut"))
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func sumRef(id string) semreg.DefinitionRef {
	return semreg.DefinitionRef{Pack: semreg.PackRef{ID: "pack.sum", Version: "1.0.0"}, ID: semreg.DefinitionID(id), Version: "1.0.0"}
}

func sumQuantity(coefficient string, exponent int32) *semreg.Value {
	return &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{
		Number: semreg.Decimal{Coefficient: coefficient, Exponent10: exponent}, Unit: "unit.watt",
	}}
}

func sumFact(subject, coefficient string, exponent int32, aggregation graph.Aggregation) graph.LensFact {
	value := sumQuantity(coefficient, exponent)
	return graph.LensFact{
		AssetID: semreg.AssetID("asset:" + subject), Definition: sumRef("sum.fact.power"),
		Key: semreg.FactKey{
			PackID: "pack.sum", PackVersion: "1.0.0", FactID: "sum.fact.power",
			Dimensions: []semreg.Dimension{{ID: "sum.dimension.device", Value: semreg.Value{Kind: semreg.ValueText, Text: &subject}}},
		},
		Subject: semreg.ResourceID(subject), Candidates: []semreg.FactCandidate{{
			CandidateID: semreg.CandidateID("candidate:" + subject), Value: value, Revision: "1",
		}},
		Conflicts: []semreg.Conflict{}, Revision: "1",
		Evaluation: []semreg.EvaluatedFact{{
			CandidateID: semreg.CandidateID("candidate:" + subject), CandidateRevision: "1",
			Freshness: semreg.FreshnessFresh, EffectiveAvailability: semreg.AvailabilityAvailable,
		}},
		Aggregation: aggregation, Direction: graph.DirectionNonNegative,
	}
}

func sumContains(from, to string, core graph.CoreType, orientation graph.Orientation) graph.LensRelation {
	return graph.LensRelation{
		AssetID: "asset:plant", Definition: sumRef("sum.relation.contains"),
		From: semreg.ResourceID(from), To: semreg.ResourceID(to),
		State: semreg.LinkQualified, Coherence: graph.CoherenceCoherent, InPlane: true,
		CoreType: core, Orientation: orientation,
	}
}

func threeMemberResult() graph.LensResult {
	return graph.LensResult{
		Contract: graph.LensResultContract, Lens: "lens.sum",
		Cut: graph.CutRef{CutID: sumCutID(), Completeness: graph.CutComplete, Members: []graph.CutMember{}},
		Resources: []graph.LensResource{
			{Depth: 0, AssetID: "asset:system"},
			{Depth: 1, AssetID: "asset:a"},
			{Depth: 1, AssetID: "asset:b"},
			{Depth: 1, AssetID: "asset:c"},
		},
		Relations: []graph.LensRelation{
			sumContains("resource:system", "resource:a", graph.CoreContains, graph.OrientationAsCore),
			sumContains("resource:system", "resource:b", graph.CoreContains, graph.OrientationAsCore),
			sumContains("resource:system", "resource:c", graph.CoreContains, graph.OrientationAsCore),
		},
		Facts: []graph.LensFact{
			sumFact("resource:a", "2", 0, graph.AggregationMember),
			sumFact("resource:b", "30", -1, graph.AggregationMember),
			sumFact("resource:c", "4", 0, graph.AggregationMember),
			sumFact("resource:a", "100", 0, graph.AggregationAggregate),
			sumFact("resource:system", "20", 0, graph.AggregationAggregate),
		},
	}
}

func nestedResource(id, kind string, depth int) graph.LensResource {
	return graph.LensResource{
		Resource: semreg.Resource{ResourceID: semreg.ResourceID(id), Kind: semreg.DefinitionID(kind)},
		Depth:    depth, AssetID: semreg.AssetID("asset:" + id),
	}
}

func nestedFact(definition, subject, coefficient string) graph.LensFact {
	fact := sumFact(subject, coefficient, 0, graph.AggregationMember)
	fact.Definition = sumRef(definition)
	fact.Key.FactID = semreg.DefinitionID(definition)
	return fact
}

// nestedMemberResult is a system that groups two inverters, each of which
// contains phases. Phase power is 2, 3 and 4. Inverter power is 11 and 22.
func nestedMemberResult() graph.LensResult {
	return graph.LensResult{
		Contract: graph.LensResultContract, Lens: "lens.sum",
		Cut: graph.CutRef{CutID: sumCutID(), Completeness: graph.CutComplete, Members: []graph.CutMember{}},
		Resources: []graph.LensResource{
			nestedResource("resource:system", "resource.system", 0),
			nestedResource("resource:inv-a", "resource.inverter", 1),
			nestedResource("resource:inv-b", "resource.inverter", 1),
			nestedResource("resource:phase-a1", "resource.phase", 2),
			nestedResource("resource:phase-a2", "resource.phase", 2),
			nestedResource("resource:phase-b1", "resource.phase", 2),
		},
		Relations: []graph.LensRelation{
			sumContains("resource:system", "resource:inv-a", graph.CoreGroups, graph.OrientationAsCore),
			sumContains("resource:system", "resource:inv-b", graph.CoreGroups, graph.OrientationAsCore),
			sumContains("resource:inv-a", "resource:phase-a1", graph.CoreContains, graph.OrientationAsCore),
			sumContains("resource:inv-a", "resource:phase-a2", graph.CoreContains, graph.OrientationAsCore),
			sumContains("resource:inv-b", "resource:phase-b1", graph.CoreContains, graph.OrientationAsCore),
		},
		Facts: []graph.LensFact{
			nestedFact("sum.fact.power", "resource:phase-a1", "2"),
			nestedFact("sum.fact.power", "resource:phase-a2", "3"),
			nestedFact("sum.fact.power", "resource:phase-b1", "4"),
			nestedFact("sum.fact.inverter_power", "resource:inv-a", "11"),
			nestedFact("sum.fact.inverter_power", "resource:inv-b", "22"),
		},
	}
}

func requireSum(t *testing.T, got graph.MemberSum, coefficient string, exponent int32) {
	t.Helper()
	if got.Withheld || got.Reason != "" || got.Value == nil {
		t.Fatalf("sum = %+v, want value %s e%d", got, coefficient, exponent)
	}
	if got.Value.Unit != "unit.watt" || got.Value.Number.Coefficient != coefficient || got.Value.Number.Exponent10 != exponent {
		t.Fatalf("value = %+v, want %s e%d unit.watt", got.Value, coefficient, exponent)
	}
}

func requireWithheld(t *testing.T, got graph.MemberSum, reason graph.SumReason) {
	t.Helper()
	if !got.Withheld || got.Reason != reason || got.Value != nil {
		t.Fatalf("sum = %+v, want withheld %s", got, reason)
	}
}

func observedValue(facts []graph.LensFact, subject string) (string, bool) {
	for _, fact := range facts {
		if string(fact.Subject) != subject || fact.Aggregation != graph.AggregationAggregate || len(fact.Candidates) == 0 || fact.Candidates[0].Value == nil || fact.Candidates[0].Value.Quantity == nil {
			continue
		}
		return fact.Candidates[0].Value.Quantity.Number.Coefficient, true
	}
	return "", false
}

func TestSumMembers(t *testing.T) {
	fact := sumRef("sum.fact.power")
	scope := semreg.ResourceID("resource:system")

	t.Run("three members and the observed aggregate", func(t *testing.T) {
		got, err := graph.SumMembers(threeMemberResult(), fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireSum(t, got, "9", 0)
		coefficient, ok := observedValue(got.Observed, "resource:system")
		if !ok || coefficient != "20" {
			t.Fatalf("observed = %+v", got.Observed)
		}
		for _, item := range got.Observed {
			if string(item.Subject) != "resource:system" {
				t.Fatalf("observed subject %s", item.Subject)
			}
		}
	})

	t.Run("aggregate on a member is not added", func(t *testing.T) {
		got, err := graph.SumMembers(threeMemberResult(), fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireSum(t, got, "9", 0)
	})

	t.Run("inverse contains and groups", func(t *testing.T) {
		result := threeMemberResult()
		result.Relations = []graph.LensRelation{
			sumContains("resource:a", "resource:system", graph.CoreGroups, graph.OrientationInverse),
			sumContains("resource:b", "resource:system", graph.CoreContains, graph.OrientationInverse),
			sumContains("resource:c", "resource:system", graph.CoreGroups, graph.OrientationInverse),
		}
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireSum(t, got, "9", 0)
	})

	t.Run("supplies is not membership", func(t *testing.T) {
		result := threeMemberResult()
		for i := range result.Relations {
			result.Relations[i].CoreType = graph.CoreSupplies
		}
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumNoCoverage)
	})

	t.Run("no members is not zero", func(t *testing.T) {
		result := threeMemberResult()
		result.Relations = nil
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumNoCoverage)
		if coefficient, ok := observedValue(got.Observed, "resource:system"); !ok || coefficient != "20" {
			t.Fatalf("observed beside an empty scope = %+v", got.Observed)
		}
	})

	t.Run("same unit", func(t *testing.T) {
		result := threeMemberResult()
		result.Findings = []graph.CutFinding{{
			Kind: graph.CutSameUnit,
			Resources: []graph.CutResourceRef{
				{AssetID: "asset:a", ResourceID: "resource:a"},
				{AssetID: "asset:b", ResourceID: "resource:b"},
			},
		}}
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumSameUnit)
	})

	t.Run("partial cut", func(t *testing.T) {
		result := threeMemberResult()
		result.Cut.Completeness = graph.CutPartial
		result.Cut.NotIncluded = []graph.NotIncludedAsset{{AssetID: "asset:missing", Reason: graph.NotIncludedSourceUnavailable}}
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumPartialCut)
	})

	t.Run("partial without a not-included asset is summed", func(t *testing.T) {
		result := threeMemberResult()
		result.Cut.Completeness = graph.CutPartial
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireSum(t, got, "9", 0)
	})

	t.Run("unavailable member", func(t *testing.T) {
		result := threeMemberResult()
		result.Facts[1].Evaluation[0].EffectiveAvailability = semreg.AvailabilityUnavailable
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumMemberUnavailable)
	})

	t.Run("stale member", func(t *testing.T) {
		result := threeMemberResult()
		result.Facts[1].Evaluation[0].Freshness = semreg.FreshnessStale
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumMemberUnavailable)
	})

	t.Run("conflict", func(t *testing.T) {
		result := threeMemberResult()
		result.Facts[1].Conflicts = []semreg.Conflict{{ConflictID: "conflict:b", Kind: semreg.ConflictValue, Candidates: []semreg.CandidateID{"candidate:b", "candidate:b2"}, State: semreg.ConflictOpen}}
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumMemberConflict)
	})

	t.Run("mixed sign reference", func(t *testing.T) {
		result := threeMemberResult()
		left := sumRef("sum.relation.supply_left")
		right := sumRef("sum.relation.supply_right")
		for i := range result.Facts {
			if result.Facts[i].Aggregation != graph.AggregationMember {
				continue
			}
			result.Facts[i].Direction = graph.DirectionSigned
			sign := left
			if result.Facts[i].Subject == "resource:c" {
				sign = right
			}
			result.Facts[i].Sign = &graph.LensSign{Relation: &sign}
		}
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumMixedSign)
	})

	t.Run("one sign reference is added", func(t *testing.T) {
		result := threeMemberResult()
		sign := sumRef("sum.relation.supply")
		for i := range result.Facts {
			if result.Facts[i].Aggregation != graph.AggregationMember {
				continue
			}
			result.Facts[i].Direction = graph.DirectionSigned
			result.Facts[i].Sign = &graph.LensSign{Relation: &sign}
		}
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireSum(t, got, "9", 0)
	})

	t.Run("nested subjects skip another kind", func(t *testing.T) {
		result := nestedMemberResult()
		phases, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireSum(t, phases, "9", 0)
		inverters, err := graph.SumMembers(result, sumRef("sum.fact.inverter_power"), scope)
		if err != nil {
			t.Fatal(err)
		}
		requireSum(t, inverters, "33", 0)
	})

	t.Run("missing nested subject withholds", func(t *testing.T) {
		result := nestedMemberResult()
		kept := make([]graph.LensFact, 0, len(result.Facts))
		for _, item := range result.Facts {
			if item.Subject == "resource:phase-b1" {
				continue
			}
			kept = append(kept, item)
		}
		result.Facts = kept
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumMemberUnavailable)
	})

	t.Run("native convention across assets", func(t *testing.T) {
		result := threeMemberResult()
		for i := range result.Facts {
			if result.Facts[i].Aggregation != graph.AggregationMember {
				continue
			}
			result.Facts[i].Direction = graph.DirectionSigned
			result.Facts[i].Sign = &graph.LensSign{NativeConvention: true}
		}
		got, err := graph.SumMembers(result, fact, scope)
		if err != nil {
			t.Fatal(err)
		}
		requireWithheld(t, got, graph.SumNativeConvention)
	})
}

// TestSumMembersDefinitionFields varies every field of the fact definition and
// the scope. pack id, pack version, fact id and fact version each select no
// member fact, so a sum that ignored that field would still return 9.
// scope selects the resources reached from that resource; a sum that ignored
// it would still return 9.
func TestSumMembersDefinitionFields(t *testing.T) {
	base := sumRef("sum.fact.power")
	scope := semreg.ResourceID("resource:system")
	mutations := []struct {
		name string
		edit func(*semreg.DefinitionRef, *semreg.ResourceID)
		want graph.SumReason
	}{
		{"pack id", func(def *semreg.DefinitionRef, _ *semreg.ResourceID) { def.Pack.ID = "pack.other" }, graph.SumMemberUnavailable},
		{"pack version", func(def *semreg.DefinitionRef, _ *semreg.ResourceID) { def.Pack.Version = "1.2.0" }, graph.SumMemberUnavailable},
		{"fact id", func(def *semreg.DefinitionRef, _ *semreg.ResourceID) { def.ID = "sum.fact.other" }, graph.SumMemberUnavailable},
		{"fact version", func(def *semreg.DefinitionRef, _ *semreg.ResourceID) { def.Version = "1.0.1" }, graph.SumMemberUnavailable},
		{"scope", func(_ *semreg.DefinitionRef, scope *semreg.ResourceID) { *scope = "resource:a" }, graph.SumNoCoverage},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			def := base
			id := scope
			mutation.edit(&def, &id)
			got, err := graph.SumMembers(threeMemberResult(), def, id)
			if err != nil {
				t.Fatal(err)
			}
			requireWithheld(t, got, mutation.want)
		})
	}
}
