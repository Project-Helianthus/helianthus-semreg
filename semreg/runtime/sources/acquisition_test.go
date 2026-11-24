package sources

import (
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

func TestAcquisitionIdentityVariesWithEveryAcquisitionFieldAndOnlyThose(t *testing.T) {
	l := newLab(t)
	d := l.addDriver("boiler", "asset:boiler", 30, nil)
	base := d.obs("cand:a", quantity("32165", -2), "1", 100, "a1")
	base.Times.PhenomenonAt, base.Times.SourceAt = ptr(at(90)), ptr(at(95))
	identity := func(asset v1.AssetID, c v1.FactCandidate) string { return acquisitionOf(asset, c).identity }
	want := identity("asset:boiler", base)

	changes := map[string]func(*v1.FactCandidate){
		"candidate id":      func(c *v1.FactCandidate) { c.CandidateID = "cand:other" },
		"origin":            func(c *v1.FactCandidate) { c.Origin.OriginID = "origin:other" },
		"origin evidence":   func(c *v1.FactCandidate) { c.Origin.Evidence = []v1.EvidenceRef{evidence("other")} },
		"evidence":          func(c *v1.FactCandidate) { c.Evidence = []v1.EvidenceRef{evidence("other")} },
		"phenomenon time":   func(c *v1.FactCandidate) { c.Times.PhenomenonAt = ptr(at(91)) },
		"source time":       func(c *v1.FactCandidate) { c.Times.SourceAt = ptr(at(96)) },
		"received time":     func(c *v1.FactCandidate) { c.Times.ReceivedAt = at(101) },
		"receipt monotonic": func(c *v1.FactCandidate) { c.Times.ReceiptMonotonic = mono(101) },
		"source epoch":      func(c *v1.FactCandidate) { c.SourceEpochID = ptr(v1.SourceEpochID("epoch:other")) },
		"driver generation": func(c *v1.FactCandidate) { c.DriverGeneration = ptr(v1.Uint64("2")) },
	}
	for name, change := range changes {
		c := clone(base)
		change(&c)
		if name == "source epoch" || name == "driver generation" {
			ref, got := acquisitionOf("asset:boiler", base), acquisitionOf("asset:boiler", c)
			if samePtr(got.epoch, ref.epoch) && samePtr(got.generation, ref.generation) {
				t.Fatalf("a changed %s must change the scoped comparison fields", name)
			}
			continue
		}
		if identity("asset:boiler", c) == want {
			t.Fatalf("a changed %s must be a new acquisition", name)
		}
	}
	if identity("asset:other", base) == want {
		t.Fatal("a changed asset must be a new acquisition")
	}

	same := map[string]func(*v1.FactCandidate){
		"value": func(c *v1.FactCandidate) {
			c.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: ptr(quantity("32265", -2))}
		},
		"availability":       func(c *v1.FactCandidate) { c.Quality.Availability = v1.AvailabilityUnavailable },
		"validity":           func(c *v1.FactCandidate) { c.Quality.Validity = v1.ValidityBad },
		"revision":           func(c *v1.FactCandidate) { c.Revision = "7" },
		"evaluated time":     func(c *v1.FactCandidate) { c.Times.EvaluatedAt = at(150) },
		"evaluate monotonic": func(c *v1.FactCandidate) { c.Times.EvaluateMonotonic = mono(150) },
		"freshness policy":   func(c *v1.FactCandidate) { c.FreshnessPolicy.FreshForNS = "31" },
	}
	for name, change := range same {
		c := clone(base)
		change(&c)
		if identity("asset:boiler", c) != want {
			t.Fatalf("a changed %s must not be a new acquisition", name)
		}
	}
}

func TestNewGenerationOfTheSameObservationIsANewAcquisition(t *testing.T) {
	l := newLab(t)
	d := l.addDriver("boiler", "asset:boiler", 30, nil)
	l.now = 100
	d.publish(d.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	entry := l.table.entries[refKey(d.ref())]
	first := entry.acq.stamp
	l.now = 110
	d.supersede()
	if entry.acq.stamp != first {
		t.Fatal("a stop alone must not refresh the receipt")
	}
	l.now = 120
	d.publish(d.obs("cand:a", quantity("32165", -2), "1", 100, "a1"))
	if entry.acq.stamp == first || entry.stopped || entry.sample == nil || *entry.acq.generation != "2" {
		t.Fatalf("the comparison fields do not cross a fenced generation: %+v", entry.acq)
	}
}

func TestOlderWallClockAcrossEpochsNeedsTheGapBeyondBothUncertainties(t *testing.T) {
	point := func(epoch v1.ClockEpochID, unix string, uncertainty string) *acquisition {
		return &acquisition{
			receipt:    v1.MonotonicPoint{ClockEpochID: epoch, Nanoseconds: "1"},
			receivedAt: v1.TimePoint{UnixNanoseconds: v1.Int64(unix), ClockID: "clock.utc", UncertaintyNS: v1.Uint64(uncertainty)},
		}
	}
	held := point("clock:one", "1000", "0")
	if !olderThan(point("clock:two", "900", "50"), held) {
		t.Fatal("a gap of 100 beyond uncertainties of 50 is demonstrably earlier")
	}
	if olderThan(point("clock:two", "900", "60"), point("clock:one", "1000", "50")) {
		t.Fatal("overlapping uncertainty is not demonstrably earlier: the observation follows the new-acquisition path")
	}
}
