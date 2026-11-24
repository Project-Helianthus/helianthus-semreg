package semreg

import (
	"fmt"
	"testing"
)

var (
	allQualifications = []Qualification{QualificationCandidate, QualificationQualified, QualificationUnsupported, QualificationUnknown, QualificationRejected}
	allValidities     = []Validity{ValidityGood, ValiditySuspect, ValidityBad, ValidityUnknown}
	allAvailabilities = []Availability{AvailabilityAvailable, AvailabilityDegraded, AvailabilityUnavailable, AvailabilityWithdrawn}
)

// A promoted Quality keeps its recorded qualification, validity and
// availability: no combination of the three is by itself a veto of promotion.
func TestQualityPromotionHoldsNoCrossAxisPredicate(t *testing.T) {
	for _, qualification := range allQualifications {
		for _, validity := range allValidities {
			for _, availability := range allAvailabilities {
				t.Run(fmt.Sprintf("%s/%s/%s", qualification, validity, availability), func(t *testing.T) {
					q := validQuality()
					q.Qualification, q.Validity, q.Availability = qualification, validity, availability
					q.Promotion = PromotionPromoted
					if err := q.Validate(); err != nil {
						t.Fatalf("promoted quality rejected: %v", err)
					}
					q.Promotion = PromotionUnpromoted
					if err := q.Validate(); err != nil {
						t.Fatalf("unpromoted quality rejected: %v", err)
					}
				})
			}
		}
	}
}

func TestQualityKeepsItsOtherChecks(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*Quality)
		want   ErrorID
	}{
		"promotion value":        {func(q *Quality) { q.Promotion = "maybe" }, InvalidEnum},
		"qualification value":    {func(q *Quality) { q.Qualification = "tested" }, InvalidEnum},
		"validity value":         {func(q *Quality) { q.Validity = "fine" }, InvalidEnum},
		"availability value":     {func(q *Quality) { q.Availability = "gone" }, InvalidEnum},
		"freshness value":        {func(q *Quality) { q.Freshness = "old" }, InvalidEnum},
		"assertion value":        {func(q *Quality) { q.Assertion = "guessed" }, InvalidEnum},
		"missing reasons":        {func(q *Quality) { q.Reasons = nil }, MissingMember},
		"duplicate reasons":      {func(q *Quality) { q.Reasons = []DefinitionID{"reason.a", "reason.a"} }, DuplicateKey},
		"unordered reasons":      {func(q *Quality) { q.Reasons = []DefinitionID{"reason.b", "reason.a"} }, NoncanonicalOrder},
		"more than sixteen":      {func(q *Quality) { q.Reasons = sixteenPlusOneReasons() }, BoundsExceeded},
		"promoted enum survives": {func(q *Quality) { q.Promotion = PromotionPromoted; q.Freshness = "old" }, InvalidEnum},
	} {
		q := validQuality()
		tc.mutate(&q)
		if got := ErrorIdentifier(q.Validate()); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func sixteenPlusOneReasons() []DefinitionID {
	reasons := make([]DefinitionID, 17)
	for i := range reasons {
		reasons[i] = DefinitionID(fmt.Sprintf("reason.%02d", i))
	}
	return reasons
}

// FactCandidate.Validate uses the corrected Quality check and keeps the
// value-presence rules for unsupported, rejected and withdrawn candidates.
func TestFactCandidatePromotedKeepsOriginalQualityStates(t *testing.T) {
	for _, qualification := range allQualifications {
		for _, availability := range allAvailabilities {
			t.Run(fmt.Sprintf("%s/%s", qualification, availability), func(t *testing.T) {
				forbidden := qualification == QualificationUnsupported || qualification == QualificationRejected || availability == AvailabilityWithdrawn
				c := validCandidate("a", true)
				c.Quality.Qualification, c.Quality.Availability = qualification, availability
				c.Quality.Promotion = PromotionPromoted
				if forbidden {
					if got := ErrorIdentifier(c.Validate()); got != InvalidValue {
						t.Fatalf("promoted %s/%s with a value: %q", qualification, availability, got)
					}
					c.Value = nil
				}
				if err := c.Validate(); err != nil {
					t.Fatalf("promoted candidate rejected: %v", err)
				}
				if !forbidden {
					c.Value = nil
					if got := ErrorIdentifier(c.Validate()); got != MissingMember {
						t.Fatalf("promoted candidate without a value: %q", got)
					}
				}
			})
		}
	}
}

func TestFactCandidatePromotedKeepsAssertionRules(t *testing.T) {
	unqualified := func(c *FactCandidate) {
		c.Quality.Qualification = QualificationCandidate
		c.Quality.Validity = ValidityBad
		c.Quality.Availability = AvailabilityUnavailable
		c.Quality.Promotion = PromotionPromoted
	}
	observed := validCandidate("a", true)
	unqualified(&observed)
	if err := observed.Validate(); err != nil {
		t.Fatal(err)
	}
	inferred := validInferredCandidate()
	unqualified(&inferred)
	if err := inferred.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		candidate FactCandidate
		mutate    func(*FactCandidate)
		want      ErrorID
	}{
		"observed without a source path": {observed, func(c *FactCandidate) { c.BindingID = nil }, MissingMember},
		"observed with a derivation":     {observed, func(c *FactCandidate) { c.Derivation = inferred.Derivation }, InvalidValue},
		"inferred without a derivation":  {inferred, func(c *FactCandidate) { c.Derivation = nil }, MissingMember},
		"inferred with a source path":    {inferred, func(c *FactCandidate) { b := NativeBindingID("binding:x"); c.BindingID = &b }, InvalidValue},
		"derived origin without a derivation": {inferred, func(c *FactCandidate) {
			c.Derivation = nil
			c.Quality.Assertion = AssertionObserved
			c.BindingID = observed.BindingID
			c.SourceEpochID = observed.SourceEpochID
			c.DriverGeneration = observed.DriverGeneration
		}, MissingMember},
		"candidate revision not positive":       {observed, func(c *FactCandidate) { c.Revision = "0" }, InvalidIdentifier},
		"promoted enum survives in a candidate": {observed, func(c *FactCandidate) { c.Quality.Promotion = "maybe" }, InvalidEnum},
	} {
		c := tc.candidate
		tc.mutate(&c)
		if got := ErrorIdentifier(c.Validate()); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// Conflict derivation is unchanged: only qualified, promoted candidates with a
// value take part, so a promoted candidate of another qualification neither
// opens nor joins a conflict.
func TestConflictDerivationUnchangedByPromotionCorrection(t *testing.T) {
	qualifiedA := validCandidate("a", true)
	qualifiedB := validCandidate("b", false)
	promotedCandidate := validCandidate("c", false)
	promotedCandidate.Quality.Qualification = QualificationCandidate
	promotedCandidate.Quality.Promotion = PromotionPromoted
	promotedUnknown := validCandidate("d", false)
	promotedUnknown.Quality.Qualification = QualificationUnknown
	promotedUnknown.Quality.Promotion = PromotionPromoted
	for _, c := range []FactCandidate{qualifiedA, qualifiedB, promotedCandidate, promotedUnknown} {
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", c.CandidateID, err)
		}
	}
	conflicts, err := deriveConflicts("asset:a", validKey(), []FactCandidate{qualifiedA, qualifiedB, promotedCandidate, promotedUnknown})
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || len(conflicts[0].Candidates) != 2 || conflicts[0].Candidates[0] != "candidate:a" || conflicts[0].Candidates[1] != "candidate:b" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	none, err := deriveConflicts("asset:a", validKey(), []FactCandidate{qualifiedA, promotedCandidate, promotedUnknown})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("a promoted candidate of another qualification opened a conflict: %+v", none)
	}
}
