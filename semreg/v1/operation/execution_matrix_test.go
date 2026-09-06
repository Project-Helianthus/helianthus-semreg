package operation_test

import (
	"bytes"
	"reflect"
	"sync"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func dispatch(delivery operation.DeliveryState, possible bool) *operation.DispatchEvidence {
	return &operation.DispatchEvidence{
		AttemptID: "attempt:test:1", Started: context("180", "180"), Completed: func() *semreg.EvaluationContext { value := context("200", "200"); return &value }(),
		Delivery: delivery, PossibleSideEffect: possible, Evidence: []semreg.EvidenceRef{evidence(7)},
	}
}

func admittedRecord(admission *operation.Admission) operation.ExecutionRecord {
	intent := admission.Intent()
	route, _ := admission.Route()
	at, revisions, _ := admission.AdmittedAt()
	return operation.ExecutionRecord{
		Contract: operation.ContractOperationV1, Intent: intent, AdmittedAt: &at, AdmittedRevision: &revisions, Route: &route,
		OutcomeEvidence: []semreg.EvidenceRef{evidence(8)},
	}
}

func applyReadback(t *testing.T, fixture operationFixture, value bool, wall, ticks string) semreg.Snapshot {
	t.Helper()
	batch := semreg.PublicationBatch{
		Contract: semreg.ContractKernelV1, BatchID: "batch:test:readback", AssetID: "asset:test", SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1", Sequence: "2",
		ExpectedSemanticRevision: fixture.snapshot.Revisions.Semantic, ObservedAt: timePoint(wall), SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{},
		BindingUpserts: []semreg.NativeBinding{}, IdentityLinkUpserts: []semreg.IdentityLink{}, FactUpserts: []semreg.FactCandidate{factCandidate("candidate:level", readbackKey, value, wall, ticks, "1")}, FactWithdrawals: []semreg.CandidateID{},
		ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{},
	}
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := fixture.publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: semreg.Uint64(ticks)})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func readback(snapshot semreg.Snapshot, relation operation.ReadbackRelation) *operation.Readback {
	return &operation.Readback{
		SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions, CandidateID: "candidate:level", CandidateRevision: "1", BindingID: "binding:test", SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1",
		Relation: relation, Evaluation: context("240", "240"), Evidence: []semreg.EvidenceRef{evidence(9)},
	}
}

func TestEveryTerminalOutcomeMatrix(t *testing.T) {
	t.Run("rejected", func(t *testing.T) {
		fixture := newOperationFixture(t)
		first, err := fixture.kernel.RecordRejection(fixture.intent, semreg.AuthorityMissing, []semreg.EvidenceRef{})
		if err != nil || first.Outcome != operation.OutcomeRejected || first.ErrorID == nil || *first.ErrorID != semreg.AuthorityMissing {
			t.Fatalf("rejected record: %+v %v", first, err)
		}
		second, err := fixture.kernel.RecordRejection(fixture.intent, semreg.AuthorityMissing, []semreg.EvidenceRef{})
		if err != nil || !reflect.DeepEqual(first, second) {
			t.Fatal("identical rejected outcome did not deduplicate")
		}
	})

	tests := []struct {
		name      string
		outcome   operation.Outcome
		dispatch  *operation.DispatchEvidence
		ack       *operation.Acknowledgement
		withLater bool
		value     bool
		relation  operation.ReadbackRelation
	}{
		{"failed_no_contact", operation.OutcomeFailedNoContact, dispatch(operation.DeliveryNotSent, false), nil, false, false, ""},
		{"acknowledged_unverified", operation.OutcomeAcknowledgedUnverified, dispatch(operation.DeliverySent, true), &operation.Acknowledgement{State: operation.AckAccepted, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}, false, false, ""},
		{"applied", operation.OutcomeApplied, dispatch(operation.DeliverySent, true), &operation.Acknowledgement{State: operation.AckAccepted, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}, true, true, operation.ReadbackConfirms},
		{"no_effect", operation.OutcomeNoEffect, dispatch(operation.DeliverySent, true), &operation.Acknowledgement{State: operation.AckRejected, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}, false, false, ""},
		{"conflict", operation.OutcomeConflict, dispatch(operation.DeliverySent, true), nil, true, false, operation.ReadbackContradicts},
		{"indeterminate", operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true), nil, false, false, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			if test.outcome == operation.OutcomeApplied {
				fixture.pack.mutateHooks = true
			}
			admission := mustAdmit(t, fixture)
			record := admittedRecord(admission)
			record.Outcome, record.Dispatch, record.Acknowledgement = test.outcome, test.dispatch, test.ack
			var later *semreg.Snapshot
			if test.withLater {
				snapshot := applyReadback(t, fixture, test.value, "220", "220")
				beforeSnapshot, _ := semreg.CanonicalJSON(snapshot)
				record.Readback = readback(snapshot, test.relation)
				later = &snapshot
				defer func() {
					afterSnapshot, _ := semreg.CanonicalJSON(snapshot)
					if !bytes.Equal(beforeSnapshot, afterSnapshot) {
						t.Error("readback hook mutated snapshot input")
					}
				}()
			}
			stored, err := fixture.kernel.Record(admission, record, later)
			if err != nil || stored.Outcome != test.outcome {
				t.Fatalf("terminal %s: %+v %v", test.outcome, stored, err)
			}
			before, _ := semreg.CanonicalJSON(stored)
			stored.Intent.Arguments[0].ID = "field.test.changed"
			stored.OutcomeEvidence[0] = evidence(15)
			replayed, err := fixture.kernel.Record(admission, record, later)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := semreg.CanonicalJSON(replayed)
			if !bytes.Equal(before, after) {
				t.Fatal("stored record was mutable")
			}
			if test.outcome == operation.OutcomeApplied && fixture.pack.readbackCalls != 1 {
				t.Fatalf("readback hook calls: %d", fixture.pack.readbackCalls)
			}
		})
	}
}

func TestExecutionNegativeAndChronologyMatrix(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*operation.ExecutionRecord)
		want   semreg.ErrorID
	}{
		{"failed no contact unknown delivery", func(r *operation.ExecutionRecord) {
			r.Outcome = operation.OutcomeFailedNoContact
			r.Dispatch = dispatch(operation.DeliveryUnknown, true)
		}, semreg.InvalidOutcome},
		{"applied without readback", func(r *operation.ExecutionRecord) {
			r.Outcome = operation.OutcomeApplied
			r.Dispatch = dispatch(operation.DeliverySent, true)
		}, semreg.InvalidOutcome},
		{"rejected with route", func(r *operation.ExecutionRecord) {
			id := semreg.AuthorityMissing
			r.Outcome = operation.OutcomeRejected
			r.ErrorID = &id
		}, semreg.InvalidOutcome},
		{"dispatch chronology", func(r *operation.ExecutionRecord) {
			r.Outcome = operation.OutcomeIndeterminate
			r.Dispatch = dispatch(operation.DeliveryUnknown, true)
			r.Dispatch.Completed = func() *semreg.EvaluationContext { value := context("170", "170"); return &value }()
		}, semreg.InvalidOutcome},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			admission := mustAdmit(t, fixture)
			record := admittedRecord(admission)
			test.mutate(&record)
			_, err := fixture.kernel.Record(admission, record, nil)
			errorID(t, err, test.want)
			if _, ok := admission.Recorded(); ok {
				t.Fatal("invalid record partially appended")
			}
		})
	}
}

func TestAcknowledgedUnverifiedRejectsContradictingReadback(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	record := admittedRecord(admission)
	record.Outcome = operation.OutcomeAcknowledgedUnverified
	record.Dispatch = dispatch(operation.DeliverySent, true)
	record.Acknowledgement = &operation.Acknowledgement{State: operation.AckAccepted, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}
	record.Readback = readback(fixture.snapshot, operation.ReadbackContradicts)
	errorID(t, record.Validate(), semreg.InvalidOutcome)
}

func TestReadbackExactEffectAndPostDispatchProof(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*operation.ExecutionRecord, *semreg.Snapshot)
		want   semreg.ErrorID
	}{
		{"pre-dispatch receipt", func(_ *operation.ExecutionRecord, snapshot *semreg.Snapshot) {}, semreg.InvalidOutcome},
		{"different generation", func(r *operation.ExecutionRecord, _ *semreg.Snapshot) { r.Readback.DriverGeneration = "2" }, semreg.StaleDriverGeneration},
		{"different epoch", func(r *operation.ExecutionRecord, _ *semreg.Snapshot) { r.Readback.SourceEpochID = "epoch:test:2" }, semreg.StaleSourceEpoch},
		{"caller confirms contradicting value", func(_ *operation.ExecutionRecord, _ *semreg.Snapshot) {}, semreg.InvalidOutcome},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			admission := mustAdmit(t, fixture)
			wall, ticks, value := "220", "220", true
			if test.name == "pre-dispatch receipt" {
				wall, ticks = "190", "190"
			}
			if test.name == "caller confirms contradicting value" {
				value = false
			}
			snapshot := applyReadback(t, fixture, value, wall, ticks)
			record := admittedRecord(admission)
			record.Outcome, record.Dispatch, record.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), readback(snapshot, operation.ReadbackConfirms)
			test.mutate(&record, &snapshot)
			_, err := fixture.kernel.Record(admission, record, &snapshot)
			errorID(t, err, test.want)
		})
	}
}

func TestReadbackCrossEpochUncertaintyFailsClosed(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	snapshot := applyReadback(t, fixture, true, "220", "220")
	record := admittedRecord(admission)
	record.Outcome, record.Dispatch, record.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), readback(snapshot, operation.ReadbackConfirms)
	record.Readback.Evaluation = semreg.EvaluationContext{
		EvaluatedAt:       semreg.TimePoint{UnixNanoseconds: "240", ClockID: "clock.utc", UncertaintyNS: "11"},
		EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:restarted", Nanoseconds: "1"},
	}
	_, err := fixture.kernel.Record(admission, record, &snapshot)
	errorID(t, err, semreg.InvalidOutcome)
}

func TestIndeterminateEvidenceCannotBeRewrittenOrRetried(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	record := admittedRecord(admission)
	record.Outcome, record.Dispatch = operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true)
	if _, err := fixture.kernel.Record(admission, record, nil); err != nil {
		t.Fatal(err)
	}
	rewrite := record
	rewrite.Dispatch = dispatch(operation.DeliveryNotSent, false)
	rewrite.Outcome = operation.OutcomeFailedNoContact
	_, err := fixture.kernel.Record(admission, rewrite, nil)
	errorID(t, err, semreg.SequenceConflict)
	errorID(t, operation.ValidateRetry(record, true), semreg.RetryForbidden)
}

func TestConcurrentExternalEvidenceRecordingDeduplicates(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	record := admittedRecord(admission)
	record.Outcome, record.Dispatch = operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true)

	const workers = 32
	results := make(chan operation.ExecutionRecord, workers)
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			stored, err := fixture.kernel.Record(admission, record, nil)
			results <- stored
			errors <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	want, err := semreg.CanonicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	for stored := range results {
		got, err := semreg.CanonicalJSON(stored)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("concurrent external evidence changed: %s %v", got, err)
		}
	}
}
