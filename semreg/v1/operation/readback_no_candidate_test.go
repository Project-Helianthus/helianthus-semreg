package operation_test

import (
	"bytes"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func noCandidateReadback(snapshot semreg.Snapshot, relation operation.ReadbackRelation) *operation.Readback {
	value := readback(snapshot, relation)
	value.CandidateID, value.CandidateRevision = "", ""
	return value
}

func TestContradictingReadbackWithoutCandidateIsRecordedAsConflict(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	later := applyReadback(t, fixture, true, "220", "220")
	record := admittedRecord(admission)
	record.Outcome, record.Dispatch, record.Readback = operation.OutcomeConflict, dispatch(operation.DeliverySent, true), noCandidateReadback(later, operation.ReadbackContradicts)
	stored, err := fixture.kernel.Record(admission, record, &later)
	if err != nil || stored.Outcome != operation.OutcomeConflict || stored.Readback == nil || !stored.Readback.NoCandidate() {
		t.Fatalf("candidate-less conflict: %+v %v", stored, err)
	}
	if fixture.pack.readbackCalls != 0 {
		t.Fatalf("the pack hook ran for a readback without a candidate: %d", fixture.pack.readbackCalls)
	}
	raw, err := semreg.CanonicalJSON(stored.Readback)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("candidate_id")) || bytes.Contains(raw, []byte("candidate_revision")) {
		t.Fatalf("candidate members serialized for the no-candidate form: %s", raw)
	}
	again, err := fixture.kernel.Record(admission, record, &later)
	if err != nil || again.Outcome != operation.OutcomeConflict {
		t.Fatalf("identical record: %+v %v", again, err)
	}
}

func TestReadbackWithoutCandidateRules(t *testing.T) {
	fixture := newOperationFixture(t)
	later := applyReadback(t, fixture, true, "220", "220")
	base := func() operation.ExecutionRecord {
		record := admittedRecord(mustAdmit(t, newOperationFixture(t)))
		record.Outcome, record.Dispatch, record.Readback = operation.OutcomeConflict, dispatch(operation.DeliverySent, true), noCandidateReadback(later, operation.ReadbackContradicts)
		return record
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("valid form refused: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*operation.ExecutionRecord)
		want   semreg.ErrorID
	}{
		{"confirms without candidate", func(r *operation.ExecutionRecord) {
			r.Outcome, r.Readback = operation.OutcomeApplied, noCandidateReadback(later, operation.ReadbackConfirms)
		}, semreg.DanglingReference},
		{"inconclusive without candidate", func(r *operation.ExecutionRecord) {
			r.Readback = noCandidateReadback(later, operation.ReadbackInconclusive)
		}, semreg.DanglingReference},
		{"candidate id without revision", func(r *operation.ExecutionRecord) { r.Readback.CandidateID = "candidate:level" }, semreg.InvalidIdentifier},
		{"candidate revision without id", func(r *operation.ExecutionRecord) { r.Readback.CandidateRevision = "1" }, semreg.InvalidIdentifier},
		{"not sent", func(r *operation.ExecutionRecord) { r.Dispatch = dispatch(operation.DeliveryNotSent, false) }, semreg.InvalidOutcome},
		{"unknown delivery", func(r *operation.ExecutionRecord) { r.Dispatch = dispatch(operation.DeliveryUnknown, true) }, semreg.InvalidOutcome},
		{"no_effect outcome", func(r *operation.ExecutionRecord) { r.Outcome = operation.OutcomeNoEffect }, semreg.InvalidOutcome},
		{"indeterminate outcome", func(r *operation.ExecutionRecord) { r.Outcome = operation.OutcomeIndeterminate }, semreg.InvalidOutcome},
		{"missing evidence", func(r *operation.ExecutionRecord) { r.Readback.Evidence = []semreg.EvidenceRef{} }, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := base()
			test.mutate(&record)
			err := record.Validate()
			if err == nil {
				t.Fatal("accepted")
			}
			if test.want != "" {
				errorID(t, err, test.want)
			}
		})
	}
}

func TestReadbackWithoutCandidateBindsToTheAdmittedRoute(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*operation.ExecutionRecord)
		want   semreg.ErrorID
	}{
		{"source epoch", func(r *operation.ExecutionRecord) { r.Readback.SourceEpochID = "epoch:test:2" }, semreg.StaleSourceEpoch},
		{"driver generation", func(r *operation.ExecutionRecord) { r.Readback.DriverGeneration = "2" }, semreg.StaleDriverGeneration},
		{"binding", func(r *operation.ExecutionRecord) { r.Readback.BindingID = "binding:other" }, semreg.InvalidOutcome},
		{"source", func(r *operation.ExecutionRecord) { r.Readback.SourceID = "source:other" }, semreg.InvalidOutcome},
		{"snapshot", func(r *operation.ExecutionRecord) { r.Readback.SnapshotID = "snapshot:other" }, semreg.DanglingReference},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			admission := mustAdmit(t, fixture)
			later := applyReadback(t, fixture, true, "220", "220")
			record := admittedRecord(admission)
			record.Outcome, record.Dispatch, record.Readback = operation.OutcomeConflict, dispatch(operation.DeliverySent, true), noCandidateReadback(later, operation.ReadbackContradicts)
			test.mutate(&record)
			_, err := fixture.kernel.Record(admission, record, &later)
			errorID(t, err, test.want)
		})
	}
	t.Run("missing later snapshot", func(t *testing.T) {
		fixture := newOperationFixture(t)
		admission := mustAdmit(t, fixture)
		later := applyReadback(t, fixture, true, "220", "220")
		record := admittedRecord(admission)
		record.Outcome, record.Dispatch, record.Readback = operation.OutcomeConflict, dispatch(operation.DeliverySent, true), noCandidateReadback(later, operation.ReadbackContradicts)
		_, err := fixture.kernel.Record(admission, record, nil)
		errorID(t, err, semreg.DanglingReference)
	})
}

func TestReadbackConfirmsWithoutCandidateIsInvalidAsBefore(t *testing.T) {
	fixture := newOperationFixture(t)
	fixture.pack.mutateHooks = false
	admission := mustAdmit(t, fixture)
	later := applyReadback(t, fixture, true, "220", "220")
	record := admittedRecord(admission)
	record.Outcome, record.Dispatch, record.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), noCandidateReadback(later, operation.ReadbackConfirms)
	_, err := fixture.kernel.Record(admission, record, &later)
	errorID(t, err, semreg.DanglingReference)
}

func TestReadbackCandidateOfAnotherFactIsInvalidAsBefore(t *testing.T) {
	for _, relation := range []operation.ReadbackRelation{operation.ReadbackConfirms, operation.ReadbackContradicts} {
		t.Run(string(relation), func(t *testing.T) {
			fixture := newOperationFixture(t)
			admission := mustAdmit(t, fixture)
			later := applyReadback(t, fixture, true, "220", "220")
			record := admittedRecord(admission)
			outcome := operation.OutcomeApplied
			if relation == operation.ReadbackContradicts {
				outcome = operation.OutcomeConflict
			}
			record.Outcome, record.Dispatch, record.Readback = outcome, dispatch(operation.DeliverySent, true), readback(later, relation)
			record.Readback.CandidateID, record.Readback.CandidateRevision = "candidate:flag", "1"
			_, err := fixture.kernel.Record(admission, record, &later)
			errorID(t, err, semreg.InvalidOutcome)
		})
	}
}

func TestNotSentCompletionWithNoSideEffectIsFailedNoContact(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	record := admittedRecord(admission)
	record.Outcome, record.Dispatch = operation.OutcomeFailedNoContact, dispatch(operation.DeliveryNotSent, false)
	stored, err := fixture.kernel.Record(admission, record, nil)
	if err != nil || stored.Outcome != operation.OutcomeFailedNoContact {
		t.Fatalf("failed_no_contact: %+v %v", stored, err)
	}
}
