package operation_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

type fieldProbePack struct {
	*testOperationPack
	fieldCalls int
	fieldRef   semreg.DefinitionRef
	reject     bool
}

type unrelatedFieldProbePack struct {
	*distinctOperationPack
	fieldCalls int
}

type indexedFieldProbePack struct {
	*fieldProbePack
	fields []semreg.DefinitionRef
}

func (p *indexedFieldProbePack) Definitions() semreg.DefinitionIndex {
	index := p.testOperationPack.Definitions()
	index.Fields = append([]semreg.DefinitionRef(nil), p.fields...)
	return index
}

func (p *unrelatedFieldProbePack) ValidateField(semreg.DefinitionRef, semreg.TypedField) error {
	p.fieldCalls++
	return &semreg.Error{ID: semreg.InvalidValue, Detail: "unrelated field owner called"}
}

func (p *fieldProbePack) ValidateField(ref semreg.DefinitionRef, field semreg.TypedField) error {
	p.fieldCalls++
	p.fieldRef = ref
	if field.Value.Boolean != nil {
		*field.Value.Boolean = false
	}
	if p.reject {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "field disallows value"}
	}
	return nil
}

func TestCorrectionIdentityRouteMatrix(t *testing.T) {
	for _, state := range []semreg.LinkState{
		semreg.LinkCandidate,
		semreg.LinkRejected,
		semreg.LinkConflict,
		semreg.LinkWithdrawn,
	} {
		t.Run(string(state), func(t *testing.T) {
			fixture := newOperationFixtureWith(t, func(batch *semreg.PublicationBatch) {
				batch.IdentityLinkUpserts[0].State = state
			})
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			errorID(t, err, semreg.IdentityNotQualified)
		})
	}

	t.Run("missing", func(t *testing.T) {
		fixture := newOperationFixture(t)
		fixture.snapshot.IdentityLinks = []semreg.IdentityLink{}
		sealCorrectionSnapshot(t, &fixture.snapshot)
		_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
		errorID(t, err, semreg.IdentityNotQualified)
	})
}

func TestCorrectionExactFieldHookMatrix(t *testing.T) {
	fixture := newOperationFixture(t)
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject_%t", reject), func(t *testing.T) {
			pack := &fieldProbePack{testOperationPack: &testOperationPack{}, reject: reject}
			unrelated := &unrelatedFieldProbePack{distinctOperationPack: &distinctOperationPack{testOperationPack: &testOperationPack{}}}
			kernel, err := operation.NewKernel(pack, unrelated)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := semreg.CanonicalJSON(fixture.intent)
			admission, err := kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			if reject {
				errorID(t, err, semreg.InvalidValue)
				if admission != nil || pack.intentCalls != 0 {
					t.Fatalf("rejecting field reached admission/intent hook: admission=%v intent_calls=%d", admission != nil, pack.intentCalls)
				}
			} else if err != nil || admission == nil || pack.intentCalls != 1 {
				t.Fatalf("valid field dispatch: admission=%v intent_calls=%d err=%v", admission != nil, pack.intentCalls, err)
			}
			if pack.fieldCalls != 1 || pack.fieldRef != testField {
				t.Fatalf("field hook dispatch: calls=%d ref=%+v", pack.fieldCalls, pack.fieldRef)
			}
			if unrelated.fieldCalls != 0 || unrelated.intentCalls != 0 {
				t.Fatalf("unrelated hooks called: field=%d intent=%d", unrelated.fieldCalls, unrelated.intentCalls)
			}
			after, _ := semreg.CanonicalJSON(fixture.intent)
			if !reflect.DeepEqual(before, after) || fixture.intent.Arguments[0].Value.Boolean == nil || !*fixture.intent.Arguments[0].Value.Boolean {
				t.Fatal("field hook mutated caller input")
			}
		})
	}

	t.Run("preserves_independent_field_version", func(t *testing.T) {
		field := testField
		field.Version = "1.1.0"
		pack := &indexedFieldProbePack{fieldProbePack: &fieldProbePack{testOperationPack: &testOperationPack{}}, fields: []semreg.DefinitionRef{field}}
		kernel, err := operation.NewKernel(pack)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize)); err != nil {
			t.Fatal(err)
		}
		if pack.fieldCalls != 1 || pack.fieldRef != field {
			t.Fatalf("independent field version dispatch: calls=%d ref=%+v", pack.fieldCalls, pack.fieldRef)
		}
	})

	t.Run("ambiguous_field_versions_fail_closed", func(t *testing.T) {
		other := testField
		other.Version = "1.1.0"
		pack := &indexedFieldProbePack{fieldProbePack: &fieldProbePack{testOperationPack: &testOperationPack{}}, fields: []semreg.DefinitionRef{testField, other}}
		kernel, err := operation.NewKernel(pack)
		if err != nil {
			t.Fatal(err)
		}
		_, err = kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
		errorID(t, err, semreg.DefinitionOwnerConflict)
		if pack.fieldCalls != 0 || pack.intentCalls != 0 {
			t.Fatalf("ambiguous field versions reached hooks: field=%d intent=%d", pack.fieldCalls, pack.intentCalls)
		}
	})
}

func TestCorrectionAdmissionChronology(t *testing.T) {
	for _, epochsMatch := range []bool{true, false} {
		t.Run(fmt.Sprintf("same_epoch_%t", epochsMatch), func(t *testing.T) {
			fixture := newOperationFixture(t)
			admission := mustAdmit(t, fixture)
			record := admittedRecord(admission)
			record.Outcome = operation.OutcomeIndeterminate
			record.Dispatch = dispatch(operation.DeliveryUnknown, true)
			record.Dispatch.Started = context("110", "110")
			completed := context("120", "120")
			if !epochsMatch {
				record.Dispatch.Started.EvaluateMonotonic.ClockEpochID = "clock-epoch:other"
				completed.EvaluateMonotonic.ClockEpochID = "clock-epoch:other"
			}
			record.Dispatch.Completed = &completed
			_, err := fixture.kernel.Record(admission, record, nil)
			errorID(t, err, semreg.InvalidOutcome)
			if _, recorded := admission.Recorded(); recorded {
				t.Fatal("pre-admission dispatch was stored")
			}
		})
	}
}

func TestCorrectionReadbackAssetBinding(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	snapshot := applyReadback(t, fixture, true, "220", "220")
	snapshot.AssetID = "asset:other"
	for index := range snapshot.Bindings {
		snapshot.Bindings[index].AssetID = snapshot.AssetID
	}
	for index := range snapshot.IdentityLinks {
		snapshot.IdentityLinks[index].AssetID = snapshot.AssetID
	}
	for index := range snapshot.Services {
		snapshot.Services[index].AssetID = snapshot.AssetID
	}
	for index := range snapshot.Capabilities {
		snapshot.Capabilities[index].AssetID = snapshot.AssetID
	}
	for index := range snapshot.Facts {
		snapshot.Facts[index].AssetID = snapshot.AssetID
	}
	sealCorrectionSnapshot(t, &snapshot)
	record := admittedRecord(admission)
	record.Outcome = operation.OutcomeApplied
	record.Dispatch = dispatch(operation.DeliverySent, true)
	record.Readback = readback(snapshot, operation.ReadbackConfirms)
	_, err := fixture.kernel.Record(admission, record, &snapshot)
	errorID(t, err, semreg.InvalidOutcome)
}

func TestCorrectionOutcomePartitions(t *testing.T) {
	valid := []struct {
		name     string
		outcome  operation.Outcome
		delivery operation.DeliveryState
		possible bool
		ack      *operation.Acknowledgement
		relation operation.ReadbackRelation
	}{
		{"failed_no_contact", operation.OutcomeFailedNoContact, operation.DeliveryNotSent, false, nil, ""},
		{"acknowledged_unverified", operation.OutcomeAcknowledgedUnverified, operation.DeliverySent, true, &operation.Acknowledgement{State: operation.AckAccepted, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}, ""},
		{"applied", operation.OutcomeApplied, operation.DeliverySent, true, nil, operation.ReadbackConfirms},
		{"no_effect_possible", operation.OutcomeNoEffect, operation.DeliverySent, true, nil, ""},
		{"no_effect_proved", operation.OutcomeNoEffect, operation.DeliverySent, false, &operation.Acknowledgement{State: operation.AckRejected, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}, ""},
		{"conflict", operation.OutcomeConflict, operation.DeliverySent, true, nil, operation.ReadbackContradicts},
		{"indeterminate", operation.OutcomeIndeterminate, operation.DeliveryUnknown, true, nil, ""},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			record := admittedRecord(mustAdmit(t, fixture))
			record.Outcome = test.outcome
			record.Dispatch = dispatch(test.delivery, test.possible)
			record.Acknowledgement = test.ack
			if test.relation != "" {
				record.Readback = readback(fixture.snapshot, test.relation)
			}
			if err := record.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}

	fixture := newOperationFixture(t)
	record := admittedRecord(mustAdmit(t, fixture))
	record.Outcome = operation.OutcomeApplied
	record.Dispatch = dispatch(operation.DeliverySent, false)
	record.Readback = readback(fixture.snapshot, operation.ReadbackConfirms)
	errorID(t, record.Validate(), semreg.InvalidOutcome)
}

func TestCorrectionRecordActualHookRejection(t *testing.T) {
	fixture := newOperationFixture(t)
	fixture.intent.ExpectedEffect.Expected = boolValue(false)
	_, admitErr := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
	errorID(t, admitErr, semreg.InvalidValue)
	if fixture.pack.intentCalls != 1 {
		t.Fatalf("initial hook calls: %d", fixture.pack.intentCalls)
	}
	first, err := fixture.kernel.RecordRejection(fixture.intent, semreg.InvalidValue, []semreg.EvidenceRef{})
	if err != nil || first.Outcome != operation.OutcomeRejected || fixture.pack.intentCalls != 1 {
		t.Fatalf("record hook rejection: %+v calls=%d err=%v", first, fixture.pack.intentCalls, err)
	}
	second, err := fixture.kernel.RecordRejection(fixture.intent, semreg.InvalidValue, []semreg.EvidenceRef{})
	if err != nil || !reflect.DeepEqual(first, second) || fixture.pack.intentCalls != 1 {
		t.Fatalf("deduplicate hook rejection: %+v calls=%d err=%v", second, fixture.pack.intentCalls, err)
	}
	changed := fixture.intent
	changed.IntentID = "intent:changed"
	_, err = fixture.kernel.RecordRejection(changed, semreg.InvalidValue, []semreg.EvidenceRef{})
	errorID(t, err, semreg.SequenceConflict)
}

func TestCorrectionCollectedErrorPrecedence(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*operationFixture)
		authority operation.AuthorityResolver
		want      semreg.ErrorID
	}{
		{"authority_deadline", func(f *operationFixture) { f.intent.Deadline = timePoint("150") }, nil, semreg.AuthorityMissing},
		{"authority_precondition", func(f *operationFixture) { f.intent.Preconditions[0].CandidateID = "candidate:absent" }, nil, semreg.AuthorityMissing},
		{"epoch_revision", func(f *operationFixture) {
			f.intent.ExpectedSourceEpochID = "epoch:old"
			f.intent.ExpectedCapabilityInstanceRevision = "2"
		}, operation.AuthorityResolverFunc(authorize), semreg.StaleSourceEpoch},
		{"generation_revision", func(f *operationFixture) {
			f.intent.ExpectedDriverGeneration = "2"
			f.intent.ExpectedCapabilityInstanceRevision = "2"
		}, operation.AuthorityResolverFunc(authorize), semreg.StaleDriverGeneration},
		{"generation_causal", func(f *operationFixture) { f.intent.ExpectedDriverGeneration = "2"; f.intent.Causal.MaxHops = 0 }, operation.AuthorityResolverFunc(authorize), semreg.StaleDriverGeneration},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			test.mutate(&fixture)
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, test.authority)
			errorID(t, err, test.want)
		})
	}

	dispatch := dispatch(operation.DeliverySent, true)
	dispatch.Evidence = []semreg.EvidenceRef{evidence(1), evidence(2), evidence(1)}
	errorID(t, dispatch.Validate(), semreg.DuplicateKey)
	intent := validIntent()
	intent.Arguments = append(intent.Arguments, semreg.TypedField{ID: "field.test.zzz", Value: boolValue(true)}, intent.Arguments[0])
	errorID(t, intent.Validate(), semreg.DuplicateKey)
	intent = validIntent()
	intent.Preconditions[0].CandidateRevision = "2"
	second := intent.Preconditions[0]
	second.CandidateRevision = "10"
	intent.Preconditions = append(intent.Preconditions, second)
	if err := intent.Validate(); err != nil {
		t.Fatal("numeric precondition order", err)
	}
}

func TestCorrectionCompleteCandidateRouteEligibility(t *testing.T) {
	fixture := newOperationFixtureWith(t, func(batch *semreg.PublicationBatch) {
		second := batch.CapabilityUpserts[0]
		second.InstanceID = "capability:test:z"
		second.Revision = "2"
		batch.CapabilityUpserts = append(batch.CapabilityUpserts, second)
	})
	admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
	if err != nil {
		t.Fatal(err)
	}
	route, ok := admission.Route()
	if !ok || route.CapabilityInstance != "capability:test" {
		t.Fatalf("wrong sole eligible route: %+v", route)
	}
}

func TestCorrectionReplaySafeRetentionCapacity(t *testing.T) {
	fixture := newOperationFixture(t)
	kernel, err := operation.NewKernelWithOptions(operation.KernelOptions{MaxRetainedOperations: 2}, fixture.pack)
	if err != nil {
		t.Fatal(err)
	}
	var first *operation.Admission
	for index := 0; index < 2; index++ {
		intent := fixture.intent
		intent.IntentID = semreg.IntentID(fmt.Sprintf("intent:capacity:%d", index))
		intent.IdempotencyKey = semreg.IdempotencyKey(fmt.Sprintf("key:capacity:%d", index))
		admission, err := kernel.Admit(fixture.snapshot, fixture.current, intent, operation.AuthorityResolverFunc(authorize))
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = admission
		}
	}
	replay := fixture.intent
	replay.IntentID = "intent:capacity:0"
	replay.IdempotencyKey = "key:capacity:0"
	got, err := kernel.Admit(fixture.snapshot, fixture.current, replay, operation.AuthorityResolverFunc(authorize))
	if err != nil || got != first {
		t.Fatalf("retained replay at capacity: same=%v err=%v", got == first, err)
	}
	changed := replay
	changed.IntentID = "intent:capacity:changed"
	_, err = kernel.Admit(fixture.snapshot, fixture.current, changed, operation.AuthorityResolverFunc(authorize))
	errorID(t, err, semreg.SequenceConflict)
	third := fixture.intent
	third.IntentID = "intent:capacity:2"
	third.IdempotencyKey = "key:capacity:2"
	_, err = kernel.Admit(fixture.snapshot, fixture.current, third, operation.AuthorityResolverFunc(authorize))
	errorID(t, err, semreg.BoundsExceeded)

	_, err = operation.NewKernelWithOptions(operation.KernelOptions{}, fixture.pack)
	errorID(t, err, semreg.BoundsExceeded)
	_, err = operation.NewKernelWithOptions(operation.KernelOptions{MaxRetainedOperations: operation.MaxRetainedOperations + 1}, fixture.pack)
	errorID(t, err, semreg.BoundsExceeded)

	rejections, err := operation.NewKernelWithOptions(operation.KernelOptions{MaxRetainedOperations: 1}, fixture.pack)
	if err != nil {
		t.Fatal(err)
	}
	firstRejection, err := rejections.RecordRejection(fixture.intent, semreg.AuthorityMissing, []semreg.EvidenceRef{})
	if err != nil {
		t.Fatal(err)
	}
	replayedRejection, err := rejections.RecordRejection(fixture.intent, semreg.AuthorityMissing, []semreg.EvidenceRef{})
	if err != nil || !reflect.DeepEqual(firstRejection, replayedRejection) {
		t.Fatalf("retained rejection replay at capacity: err=%v", err)
	}
	changedRejection := fixture.intent
	changedRejection.IntentID = "intent:capacity:rejection-changed"
	_, err = rejections.RecordRejection(changedRejection, semreg.AuthorityMissing, []semreg.EvidenceRef{})
	errorID(t, err, semreg.SequenceConflict)
	newRejection := fixture.intent
	newRejection.IntentID, newRejection.IdempotencyKey = "intent:capacity:rejection-new", "key:capacity:rejection-new"
	_, err = rejections.RecordRejection(newRejection, semreg.AuthorityMissing, []semreg.EvidenceRef{})
	errorID(t, err, semreg.BoundsExceeded)
}

func TestCorrectionExplicitForbiddenRouteBoundaries(t *testing.T) {
	fixture := newOperationFixture(t)
	selection := semreg.Selection{
		Contract: semreg.ContractSelectionV1, SnapshotID: fixture.snapshot.SnapshotID, Revisions: fixture.snapshot.Revisions,
		EvaluationDigest: semreg.Digest("sha256:" + repeatHex(10)), Context: fixture.current, Key: preKey,
		PolicyID: "policy.test", PolicyVersion: "1.0.0", SelectedCandidate: "candidate:interlock", CandidateRevision: "1", PresentationOnly: true,
	}
	_, err := fixture.kernel.AdmitSelectionRoute(selection)
	errorID(t, err, semreg.RouteSelectionForbidden)
	_, err = fixture.kernel.AdmitAliasRoute("Service:/HeatGenerator:1")
	errorID(t, err, semreg.AliasNotRoutable)
}

func sealCorrectionSnapshot(t *testing.T, snapshot *semreg.Snapshot) {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	delete(value, "snapshot_id")
	raw, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	snapshot.SnapshotID = semreg.SnapshotID(fmt.Sprintf("sha256:%x", sum))
	if err := snapshot.Validate(); err != nil {
		t.Fatal("invalid correction snapshot", err)
	}
}
