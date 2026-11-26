package runtime

import (
	"strings"
	"testing"
	"time"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestWithdrawalCandidateIDsAreAscendingWhateverTheFactKeyOrder(t *testing.T) {
	envelopes := []v1.FactEnvelope{
		{Candidates: []v1.FactCandidate{{CandidateID: "candidate:z"}}},
		{Candidates: []v1.FactCandidate{{CandidateID: "candidate:b"}, {CandidateID: "candidate:a"}}},
	}
	got := candidateIDsAscending(envelopes)
	want := []v1.CandidateID{"candidate:a", "candidate:b", "candidate:z"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestEditingAReturnedResultDoesNotChangeTheRecordedOne(t *testing.T) {
	recorded := driver.OperationResult{CompoundExecution: &driver.CompoundExecution{Steps: []driver.CompoundStep{{Point: "one"}}}}
	copied := cloneResult(recorded)
	copied.CompoundExecution.Partial = true
	copied.CompoundExecution.Steps[0].Point = "edited"
	if recorded.CompoundExecution.Partial || recorded.CompoundExecution.Steps[0].Point != "one" {
		t.Fatalf("the copy shares the recorded execution: %+v", recorded.CompoundExecution)
	}
}

func TestHostCalendarFallsBackToTheZoneGoResolvedWhateverTZHolds(t *testing.T) {
	for _, tz := range []string{":/etc/localtime", ""} {
		t.Run("TZ="+tz, func(t *testing.T) {
			t.Setenv("TZ", tz)
			calendar, err := newHostCalendar(nil, newTestClock().Now)
			if err != nil {
				t.Fatal(err)
			}
			if calendar.location != time.Local {
				t.Fatalf("location %v, want the zone the process already resolved", calendar.location)
			}
			rt, _ := openBinder(t, binderFactory{})
			if rt == nil {
				t.Fatal("no runtime")
			}
		})
	}
}

func TestHostCalendarInvalidConfiguredNameNamesTheValue(t *testing.T) {
	for _, name := range []string{"/etc/localtime", "Not/AZone", "../UTC"} {
		bad := name
		_, err := newHostCalendar(&bad, newTestClock().Now)
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%q: error %v must name the configured value", name, err)
		}
	}
}
