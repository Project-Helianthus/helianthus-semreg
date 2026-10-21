package semreg

import "testing"

func TestAvailabilityFreshnessProductPolicy(t *testing.T) {
	cases := []struct {
		stored    Availability
		freshness Freshness
		want      Availability
	}{
		{AvailabilityAvailable, FreshnessFresh, AvailabilityAvailable},
		{AvailabilityAvailable, FreshnessStale, AvailabilityDegraded},
		{AvailabilityAvailable, FreshnessUnknown, AvailabilityDegraded},
		{AvailabilityAvailable, FreshnessExpired, AvailabilityUnavailable},
		{AvailabilityDegraded, FreshnessFresh, AvailabilityDegraded},
		{AvailabilityDegraded, FreshnessStale, AvailabilityDegraded},
		{AvailabilityDegraded, FreshnessUnknown, AvailabilityDegraded},
		{AvailabilityDegraded, FreshnessExpired, AvailabilityUnavailable},
		{AvailabilityUnavailable, FreshnessFresh, AvailabilityUnavailable},
		{AvailabilityUnavailable, FreshnessStale, AvailabilityUnavailable},
		{AvailabilityUnavailable, FreshnessUnknown, AvailabilityUnavailable},
		{AvailabilityUnavailable, FreshnessExpired, AvailabilityUnavailable},
		{AvailabilityWithdrawn, FreshnessFresh, AvailabilityWithdrawn},
		{AvailabilityWithdrawn, FreshnessStale, AvailabilityWithdrawn},
		{AvailabilityWithdrawn, FreshnessUnknown, AvailabilityWithdrawn},
		{AvailabilityWithdrawn, FreshnessExpired, AvailabilityWithdrawn},
	}
	for _, tc := range cases {
		t.Run(string(tc.stored)+"/"+string(tc.freshness), func(t *testing.T) {
			if got := effectiveAvailability(tc.stored, tc.freshness); got != tc.want {
				t.Fatalf("effectiveAvailability(%s, %s) = %s, want %s", tc.stored, tc.freshness, got, tc.want)
			}
		})
	}
}

func TestAvailabilityFreshnessProductSnapshot(t *testing.T) {
	cases := []struct {
		stored    Availability
		freshness Freshness
		want      Availability
	}{
		{AvailabilityAvailable, FreshnessFresh, AvailabilityAvailable},
		{AvailabilityAvailable, FreshnessStale, AvailabilityDegraded},
		{AvailabilityAvailable, FreshnessUnknown, AvailabilityDegraded},
		{AvailabilityAvailable, FreshnessExpired, AvailabilityUnavailable},
		{AvailabilityDegraded, FreshnessFresh, AvailabilityDegraded},
		{AvailabilityDegraded, FreshnessStale, AvailabilityDegraded},
		{AvailabilityDegraded, FreshnessUnknown, AvailabilityDegraded},
		{AvailabilityDegraded, FreshnessExpired, AvailabilityUnavailable},
		{AvailabilityUnavailable, FreshnessFresh, AvailabilityUnavailable},
		{AvailabilityUnavailable, FreshnessStale, AvailabilityUnavailable},
		{AvailabilityUnavailable, FreshnessUnknown, AvailabilityUnavailable},
		{AvailabilityUnavailable, FreshnessExpired, AvailabilityUnavailable},
	}
	for _, tc := range cases {
		t.Run(string(tc.stored)+"/"+string(tc.freshness), func(t *testing.T) {
			candidate := evaluationCandidate("candidate:evaluation:availability", "fact.power", "1000", "100")
			candidate.Quality.Availability = tc.stored
			if tc.stored == AvailabilityUnavailable {
				// Unavailable candidates cannot be promoted under the quality contract.
				candidate.Quality.Promotion = PromotionUnpromoted
			}
			if err := candidate.Validate(); err != nil {
				t.Fatalf("invalid candidate fixture: %v", err)
			}
			snapshot := evaluationSnapshot(t, candidate)
			if err := snapshot.Validate(); err != nil {
				t.Fatalf("invalid snapshot fixture: %v", err)
			}
			var context EvaluationContext
			switch tc.freshness {
			case FreshnessFresh:
				context = evaluationContext("1029", "clock-epoch:evaluation", "129", "0")
			case FreshnessStale:
				context = evaluationContext("1030", "clock-epoch:evaluation", "130", "0")
			case FreshnessExpired:
				context = evaluationContext("1120", "clock-epoch:evaluation", "220", "0")
			case FreshnessUnknown:
				context = evaluationContext("1", "clock-epoch:incomparable", "1", "0")
				context.EvaluatedAt.ClockID = "clock.other"
			default:
				t.Fatalf("missing context for %s", tc.freshness)
			}
			view, err := EvaluateSnapshot(snapshot, context)
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Facts) != 1 {
				t.Fatalf("evaluated %d facts, want 1", len(view.Facts))
			}
			got := view.Facts[0]
			if got.Freshness != tc.freshness || got.EffectiveAvailability != tc.want {
				t.Fatalf("got freshness=%s availability=%s, want freshness=%s availability=%s", got.Freshness, got.EffectiveAvailability, tc.freshness, tc.want)
			}
		})
	}
}
