package semreg

import (
	"strconv"
	"testing"
)

func TestCrossEpochRetentionEdges(t *testing.T) {
	cases := []struct {
		name                  string
		delta                 int64
		evaluationUncertainty int64
		lower, upper          int64
		freshness             Freshness
		availability          Availability
	}{
		{"upper_before_retain", 117, 1, 115, 119, FreshnessStale, AvailabilityDegraded},
		{"upper_equals_retain", 118, 1, 116, 120, FreshnessUnknown, AvailabilityDegraded},
		{"interval_straddles_retain", 120, 1, 118, 122, FreshnessUnknown, AvailabilityDegraded},
		{"lower_equals_retain", 122, 1, 120, 124, FreshnessExpired, AvailabilityUnavailable},
		{"summed_uncertainty_equals_maximum", 116, 1, 114, 118, FreshnessStale, AvailabilityDegraded},
		{"summed_uncertainty_exceeds_maximum", 116, 2, 113, 119, FreshnessUnknown, AvailabilityDegraded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := evaluationCandidate("candidate:retention:edge", "fact.power", "1000", "100")
			candidate.Times.ReceivedAt.UncertaintyNS = "1"
			candidate.FreshnessPolicy = FreshnessPolicy{
				PolicyID: "policy:retention:edge", Version: "1.0.0",
				FreshForNS: "30", RetainForNS: "120", MaxWallUncertaintyNS: "2",
			}
			snapshot := evaluationSnapshot(t, candidate)
			context := evaluationContext(
				Int64(strconv.FormatInt(1000+tc.delta, 10)),
				"clock-epoch:retention:restart", "1",
				Uint64(strconv.FormatInt(tc.evaluationUncertainty, 10)),
			)
			if candidate.Times.ReceiptMonotonic.ClockEpochID == context.EvaluateMonotonic.ClockEpochID {
				t.Fatal("fixture must cross clock epochs")
			}
			// Literal interval endpoints pin the synthetic fixture independently of evaluation.
			if tc.delta-1-tc.evaluationUncertainty != tc.lower || tc.delta+1+tc.evaluationUncertainty != tc.upper {
				t.Fatalf("invalid fixture interval [%d, %d]", tc.lower, tc.upper)
			}
			if err := snapshot.Validate(); err != nil {
				t.Fatalf("invalid snapshot fixture: %v", err)
			}
			view, err := EvaluateSnapshot(snapshot, context)
			if err != nil {
				t.Fatal(err)
			}
			fact := factResult(t, view, candidate.CandidateID)
			if fact.Freshness != tc.freshness || fact.EffectiveAvailability != tc.availability {
				t.Fatalf("interval [%d, %d]: freshness=%s availability=%s; want %s, %s",
					tc.lower, tc.upper, fact.Freshness, fact.EffectiveAvailability, tc.freshness, tc.availability)
			}
		})
	}
}
