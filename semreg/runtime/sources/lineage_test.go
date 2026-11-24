package sources

import (
	"fmt"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func inputRef(subject, candidate, revision string) driver.InputRef {
	return driver.InputRef{At: graph.CutRef{CutID: digestOf("cut:one")}, Subject: driver.NodeRef{Bridge: "bridge:lab", ID: v1.ResourceID(subject)}, Candidate: v1.CandidateID(candidate), Revision: v1.Uint64(revision)}
}

func formulaOf(refs ...driver.InputRef) *driver.Formula {
	return &driver.Formula{Algorithm: v1.DefinitionRef{Pack: v1.PackRef{ID: "pack.test", Version: "1.0.0"}, ID: "algorithm.test", Version: "1.0.0"}, Inputs: refs}
}

func heldInput(label string, freshness v1.Freshness, availability v1.Availability) Input {
	value := quantity("29315", -2)
	return Input{
		Candidate: v1.FactCandidate{
			CandidateID: v1.CandidateID("cand:" + label), Key: dhwKey(), Value: &v1.Value{Kind: v1.ValueQuantity, Quantity: &value},
			Quality:  v1.Quality{Assertion: v1.AssertionObserved, Qualification: v1.QualificationQualified, Promotion: v1.PromotionPromoted, Validity: v1.ValidityGood, Availability: availability, Freshness: v1.FreshnessFresh, Reasons: []v1.DefinitionID{}},
			Times:    v1.Times{ReceivedAt: at(7), ReceiptMonotonic: mono(7), EvaluatedAt: at(8), EvaluateMonotonic: mono(8)},
			Revision: "1",
		},
		Evaluation: v1.EvaluatedFact{CandidateID: v1.CandidateID("cand:" + label), CandidateRevision: "1", Freshness: freshness, EffectiveAvailability: availability},
	}
}

func resolverOver(inputs map[string]Input, seen *[]driver.InputRef) InputResolver {
	return func(ref driver.InputRef) (Input, bool) {
		if seen != nil {
			*seen = append(*seen, ref)
		}
		in, ok := inputs[fmt.Sprintf("%s/%s/%s", ref.Subject.ID, ref.Candidate, ref.Revision)]
		return in, ok
	}
}

var derivedOrigin = driver.Origin{Kind: driver.OriginDerived, Evidence: []v1.EvidenceRef{}}

func TestCrossAssetDerivedValueKeepsEachInputAsHeld(t *testing.T) {
	a, b := inputRef("resource:asset-a", "cand:a", "1"), inputRef("resource:asset-b", "cand:b", "3")
	inputs := map[string]Input{
		"resource:asset-a/cand:a/1": heldInput("a", v1.FreshnessFresh, v1.AvailabilityAvailable),
		"resource:asset-b/cand:b/3": heldInput("b", v1.FreshnessFresh, v1.AvailabilityDegraded),
	}
	var seen []driver.InputRef
	got, err := CheckLineage(inputRef("resource:asset-c", "cand:derived", "1"), derivedOrigin, formulaOf(a, b), resolverOver(inputs, &seen))
	if err != nil {
		t.Fatal(err)
	}
	if got.Freshness != v1.FreshnessFresh || len(got.Named) != 0 || got.Origin.Kind != driver.OriginDerived {
		t.Fatalf("lineage: %s", mustJSON(t, got))
	}
	if len(seen) != 2 || mustJSON(t, seen[0]) != mustJSON(t, a) || mustJSON(t, seen[1]) != mustJSON(t, b) {
		t.Fatalf("each InputRef is resolved exactly as pinned: %v", seen)
	}
	for i, ref := range []string{"resource:asset-a/cand:a/1", "resource:asset-b/cand:b/3"} {
		if !got.Inputs[i].Found || mustJSON(t, got.Inputs[i].Input) != mustJSON(t, inputs[ref]) {
			t.Fatalf("input %d: unit, quality and times are kept as held", i)
		}
	}
}

func TestMissingStaleAndWithdrawnInputsPropagateAndAreNamed(t *testing.T) {
	a, b, c := inputRef("resource:asset-a", "cand:a", "1"), inputRef("resource:asset-b", "cand:b", "1"), inputRef("resource:asset-c", "cand:c", "1")
	self := inputRef("resource:asset-d", "cand:derived", "1")
	t.Run("missing_input_is_unknown", func(t *testing.T) {
		got, err := CheckLineage(self, derivedOrigin, formulaOf(a, b), resolverOver(map[string]Input{"resource:asset-a/cand:a/1": heldInput("a", v1.FreshnessFresh, v1.AvailabilityAvailable)}, nil))
		if err != nil || got.Freshness != v1.FreshnessUnknown || len(got.Named) != 1 || mustJSON(t, got.Named[0]) != mustJSON(t, b) || got.Inputs[1].Found {
			t.Fatalf("%v %s", err, mustJSON(t, got))
		}
	})
	t.Run("newer_revision_does_not_substitute", func(t *testing.T) {
		got, err := CheckLineage(self, derivedOrigin, formulaOf(inputRef("resource:asset-a", "cand:a", "1")), resolverOver(map[string]Input{"resource:asset-a/cand:a/2": heldInput("a", v1.FreshnessFresh, v1.AvailabilityAvailable)}, nil))
		if err != nil || got.Freshness != v1.FreshnessUnknown || len(got.Named) != 1 {
			t.Fatalf("%v %s", err, mustJSON(t, got))
		}
	})
	t.Run("stale_input_is_stale", func(t *testing.T) {
		got, err := CheckLineage(self, derivedOrigin, formulaOf(a, b), resolverOver(map[string]Input{
			"resource:asset-a/cand:a/1": heldInput("a", v1.FreshnessFresh, v1.AvailabilityAvailable),
			"resource:asset-b/cand:b/1": heldInput("b", v1.FreshnessStale, v1.AvailabilityDegraded),
		}, nil))
		if err != nil || got.Freshness != v1.FreshnessStale || len(got.Named) != 1 || mustJSON(t, got.Named[0]) != mustJSON(t, b) {
			t.Fatalf("%v %s", err, mustJSON(t, got))
		}
	})
	t.Run("withdrawn_input_is_unknown", func(t *testing.T) {
		got, err := CheckLineage(self, derivedOrigin, formulaOf(a), resolverOver(map[string]Input{"resource:asset-a/cand:a/1": heldInput("a", v1.FreshnessFresh, v1.AvailabilityWithdrawn)}, nil))
		if err != nil || got.Freshness != v1.FreshnessUnknown || len(got.Named) != 1 || mustJSON(t, got.Named[0]) != mustJSON(t, a) {
			t.Fatalf("%v %s", err, mustJSON(t, got))
		}
	})
	t.Run("nested_missing_input_propagates", func(t *testing.T) {
		mid := heldInput("b", v1.FreshnessFresh, v1.AvailabilityAvailable)
		mid.Formula = formulaOf(c)
		got, err := CheckLineage(self, derivedOrigin, formulaOf(b), resolverOver(map[string]Input{"resource:asset-b/cand:b/1": mid}, nil))
		if err != nil || got.Freshness != v1.FreshnessUnknown || len(got.Named) != 1 || mustJSON(t, got.Named[0]) != mustJSON(t, c) {
			t.Fatalf("%v %s", err, mustJSON(t, got))
		}
	})
}

func TestDerivedLineageCycleGivesDerivationCycle(t *testing.T) {
	self := inputRef("resource:asset-a", "cand:self", "1")
	b, c := inputRef("resource:asset-b", "cand:b", "1"), inputRef("resource:asset-c", "cand:c", "1")
	t.Run("back_to_the_derived_value", func(t *testing.T) {
		in := heldInput("b", v1.FreshnessFresh, v1.AvailabilityAvailable)
		in.Formula = formulaOf(self)
		selfInput := heldInput("self", v1.FreshnessFresh, v1.AvailabilityAvailable)
		_, err := CheckLineage(self, derivedOrigin, formulaOf(b), resolverOver(map[string]Input{"resource:asset-b/cand:b/1": in, "resource:asset-a/cand:self/1": selfInput}, nil))
		if v1.ErrorIdentifier(err) != v1.DerivationCycle {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("self_reference_with_an_unresolved_input", func(t *testing.T) {
		_, err := CheckLineage(self, derivedOrigin, formulaOf(self), resolverOver(nil, nil))
		if v1.ErrorIdentifier(err) != v1.DerivationCycle {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("repeated_ancestor_that_does_not_resolve", func(t *testing.T) {
		inB := heldInput("b", v1.FreshnessFresh, v1.AvailabilityAvailable)
		inB.Formula = formulaOf(b)
		_, err := CheckLineage(self, derivedOrigin, formulaOf(b), resolverOver(map[string]Input{"resource:asset-b/cand:b/1": inB}, nil))
		if v1.ErrorIdentifier(err) != v1.DerivationCycle {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("between_foreign_inputs", func(t *testing.T) {
		inB, inC := heldInput("b", v1.FreshnessFresh, v1.AvailabilityAvailable), heldInput("c", v1.FreshnessFresh, v1.AvailabilityAvailable)
		inB.Formula, inC.Formula = formulaOf(c), formulaOf(b)
		_, err := CheckLineage(self, derivedOrigin, formulaOf(b), resolverOver(map[string]Input{"resource:asset-b/cand:b/1": inB, "resource:asset-c/cand:c/1": inC}, nil))
		if v1.ErrorIdentifier(err) != v1.DerivationCycle {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a_shared_input_is_not_a_cycle", func(t *testing.T) {
		shared := heldInput("c", v1.FreshnessFresh, v1.AvailabilityAvailable)
		inB := heldInput("b", v1.FreshnessFresh, v1.AvailabilityAvailable)
		inB.Formula = formulaOf(c)
		got, err := CheckLineage(self, derivedOrigin, formulaOf(b, c), resolverOver(map[string]Input{"resource:asset-b/cand:b/1": inB, "resource:asset-c/cand:c/1": shared}, nil))
		if err != nil || got.Freshness != v1.FreshnessFresh {
			t.Fatalf("%v %s", err, mustJSON(t, got))
		}
	})
}

func TestCrossAssetFormulaIsNotBoundedBySnapshotLocalInputCount(t *testing.T) {
	inputs := map[string]Input{}
	var refs []driver.InputRef
	for i := 0; i < 40; i++ {
		ref := inputRef(fmt.Sprintf("resource:asset-%d", i), "cand:x", "1")
		refs = append(refs, ref)
		inputs[fmt.Sprintf("resource:asset-%d/cand:x/1", i)] = heldInput("x", v1.FreshnessFresh, v1.AvailabilityAvailable)
	}
	got, err := CheckLineage(inputRef("resource:asset-z", "cand:derived", "1"), derivedOrigin, formulaOf(refs...), resolverOver(inputs, nil))
	if err != nil || len(got.Inputs) != 40 {
		t.Fatalf("%v", err)
	}
}

func TestMeasuredAndDeclaredValuesCarryNoFormulaAndDerivedNeedsOne(t *testing.T) {
	self := inputRef("resource:asset-a", "cand:self", "1")
	for _, kind := range []driver.OriginKind{driver.OriginMeasured, driver.OriginDeclared} {
		origin := driver.Origin{Kind: kind, Evidence: []v1.EvidenceRef{}}
		got, err := CheckLineage(self, origin, nil, nil)
		if err != nil || got.Origin.Kind != kind || len(got.Inputs) != 0 {
			t.Fatalf("%s: %v", kind, err)
		}
		if _, err := CheckLineage(self, origin, formulaOf(inputRef("resource:asset-b", "cand:b", "1")), nil); err == nil {
			t.Fatalf("%s value with a formula was presented as lineage", kind)
		}
	}
	if _, err := CheckLineage(self, derivedOrigin, nil, nil); err == nil {
		t.Fatal("a derived value without a formula was accepted")
	}
}

func TestSharedLineageIsWalkedOncePerInput(t *testing.T) {
	self := inputRef("resource:asset-a", "cand:self", "1")
	const levels = 40
	ref := func(level, side int) driver.InputRef {
		return inputRef("resource:asset-b", fmt.Sprintf("cand:l%d-%d", level, side), "1")
	}
	inputs := map[string]Input{}
	for level := 0; level < levels; level++ {
		for side := 0; side < 2; side++ {
			in := heldInput("n", v1.FreshnessStale, v1.AvailabilityAvailable)
			if level+1 < levels {
				in.Formula = formulaOf(ref(level+1, 0), ref(level+1, 1))
			}
			inputs[fmt.Sprintf("resource:asset-b/cand:l%d-%d/1", level, side)] = in
		}
	}
	var seen []driver.InputRef
	got, err := CheckLineage(self, derivedOrigin, formulaOf(ref(0, 0), ref(0, 1)), resolverOver(inputs, &seen))
	if err != nil || got.Freshness != v1.FreshnessStale {
		t.Fatalf("%v %s", err, mustJSON(t, got))
	}
	if len(seen) > 4*levels || len(got.Named) != 2*levels {
		t.Fatalf("a shared input must be resolved and named once: %d resolver calls, %d named", len(seen), len(got.Named))
	}
}
