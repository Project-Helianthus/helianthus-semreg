package evse

import (
	"bytes"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"reflect"
	"testing"
)

func TestAllocatedCurrentOperationAndReadback(t *testing.T) {
	v := New().(operation.OperationPackValidator)
	i := intent()
	if err := v.ValidateIntent(i); err != nil {
		t.Fatal(err)
	}
	k, err := operation.NewKernel(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.ValidateIntent(i); err != nil {
		t.Fatal(err)
	}
	for _, mut := range []func(*operation.Intent){func(x *operation.Intent) { x.Arguments = []semreg.TypedField{} }, func(x *operation.Intent) { x.RequiredCapability.DefinitionID = "evse.capability.read.connector" }, func(x *operation.Intent) { x.ExpectedEffect.Rule = definition("evse.effect.unknown") }, func(x *operation.Intent) { x.ExpectedEffect.Operator = semreg.PredicateNotEqual }, func(x *operation.Intent) { x.Kind = definition("evse.operation.unknown") }} {
		bad := intent()
		mut(&bad)
		if v.ValidateIntent(bad) == nil {
			t.Fatal("invalid operation admitted")
		}
	}
	c := candidate(i.ExpectedEffect.Fact, i.ExpectedEffect.Expected)
	r, err := v.EvaluateReadback(i, c)
	if err != nil || r != operation.ReadbackConfirms {
		t.Fatal(r, err)
	}
	c.Value = &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.ampere"}}
	r, err = v.EvaluateReadback(i, c)
	if err != nil || r != operation.ReadbackContradicts {
		t.Fatal(r, err)
	}
	c.Key.FactID = "evse.status.fault"
	r, err = v.EvaluateReadback(i, c)
	if err != nil || r != operation.ReadbackInconclusive {
		t.Fatal(r, err)
	}
}
func intent() operation.Intent {
	s := must("evse.limit.allocated_current")
	x := testValue(s)
	return operation.Intent{Contract: operation.ContractOperationV1, IntentID: "intent:1", Kind: definition("evse.operation.set_allocated_current"), ExpectedEffect: operation.ExpectedEffect{Rule: definition("evse.effect.set_allocated_current"), Fact: testKey(s), Operator: semreg.PredicateEqual, Expected: x}, AssetID: "asset:1", Arguments: []semreg.TypedField{{ID: s.id, Value: x}}, RequiredCapability: operation.CapabilityRequirement{Pack: pack, DefinitionID: "evse.capability.set_allocated_current", Versions: semreg.VersionRange{Minimum: "1.0.0", MaximumExclusive: "2.0.0"}}, Authority: testEvidence(), Causal: semreg.CausalContext{Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{testEvidence()}}, CorrelationID: "correlation:1", MaxHops: 1, FirstSeenAt: time("1"), ExpiresAt: time("2"), Path: []semreg.TargetID{}}, ExpectedSemanticRevision: "1", ExpectedCapabilityRevision: "1", ExpectedCapabilityInstanceRevision: "1", ExpectedSourceEpochID: "epoch:1", ExpectedDriverGeneration: "1", Preconditions: []operation.Precondition{}, IdempotencyKey: "key:1", Deadline: time("3")}
}
func candidate(k semreg.FactKey, x semreg.Value) semreg.FactCandidate {
	b, e, g := semreg.NativeBindingID("binding:1"), semreg.SourceEpochID("epoch:1"), semreg.Uint64("1")
	source := semreg.SourceID("source:1")
	return semreg.FactCandidate{CandidateID: "candidate:1", Key: k, Value: &x, Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}}, Times: semreg.Times{ReceivedAt: time("1"), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:1", Nanoseconds: "1"}, EvaluatedAt: time("1"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:1", Nanoseconds: "1"}}, FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1", RetainForNS: "2", MaxWallUncertaintyNS: "0"}, BindingID: &b, SourceEpochID: &e, DriverGeneration: &g, Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &e, BindingID: &b, Evidence: []semreg.EvidenceRef{testEvidence()}}, Evidence: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}
}
func time(n string) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(n), ClockID: "clock.utc", UncertaintyNS: "0"}
}

var _ = reflect.DeepEqual

func TestEVSEOperationRejectionMatrix(t *testing.T) {
	v := New().(operation.OperationPackValidator)
	for _, tc := range []struct {
		name   string
		mutate func(*operation.Intent)
		want   semreg.ErrorID
	}{
		{"wrong kind", func(i *operation.Intent) { i.Kind.ID = "evse.operation.other" }, semreg.DefinitionOwnerMissing},
		{"wrong pack", func(i *operation.Intent) { i.Kind.Pack.ID = "helianthus.pack.thermal" }, semreg.InvalidValue},
		{"wrong version", func(i *operation.Intent) { i.Kind.Version = "1.0.1" }, semreg.DefinitionOwnerMissing},
		{"argument count", func(i *operation.Intent) { i.Arguments = []semreg.TypedField{} }, semreg.InvalidValue},
		{"argument field", func(i *operation.Intent) { i.Arguments[0].ID = "evse.limit.actual_current" }, semreg.InvalidValue},
		{"argument value", func(i *operation.Intent) {
			i.Arguments[0].Value.Quantity.Number = above(must("evse.limit.allocated_current").maximum.decimal())
		}, semreg.BoundsExceeded},
		{"effect rule", func(i *operation.Intent) { i.ExpectedEffect.Rule.ID = "evse.effect.other" }, semreg.InvalidValue},
		{"effect fact", func(i *operation.Intent) { i.ExpectedEffect.Fact.FactID = "evse.limit.actual_current" }, semreg.InvalidValue},
		{"effect operator", func(i *operation.Intent) { i.ExpectedEffect.Operator = semreg.PredicateNotEqual }, semreg.InvalidValue},
		{"effect value", func(i *operation.Intent) {
			i.ExpectedEffect.Expected.Quantity.Number = semreg.Decimal{Coefficient: "1"}
		}, semreg.InvalidValue},
		{"required capability", func(i *operation.Intent) { i.RequiredCapability.DefinitionID = "evse.capability.read.connector" }, semreg.InvalidValue},
		{"unsupported operation", func(i *operation.Intent) { i.Kind = definition("evse.operation.unsupported") }, semreg.DefinitionOwnerMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := intent()
			tc.mutate(&candidate)
			require(t, v.ValidateIntent(candidate), tc.want)
		})
	}
}

func TestEVSETerminalOutcomesAndConstraintGate(t *testing.T) {
	t.Run("matching published constraint admits", func(t *testing.T) {
		fixture := newEVSEFixture(t, nil)
		calls := 0
		_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { calls++; return nil }))
		if err != nil || calls != 1 {
			t.Fatalf("matching admit %v, authority calls %d", err, calls)
		}
	})
	t.Run("non-admitting published constraint rejects before authority", func(t *testing.T) {
		fixture := newEVSEFixture(t, func(capability *semreg.CapabilityInstance) {
			capability.Constraints[0].Value = alternate(capability.Constraints[0].Value)
		})
		calls := 0
		_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { calls++; return nil }))
		require(t, err, semreg.AmbiguousRoute)
		if calls != 0 {
			t.Fatalf("authority called before route rejection: %d", calls)
		}
	})

	for _, tc := range []struct {
		name            string
		outcome         operation.Outcome
		delivery        operation.DeliveryState
		acknowledgement *operation.Acknowledgement
		relation        operation.ReadbackRelation
	}{
		{"rejected", operation.OutcomeRejected, "", nil, ""},
		{"failed_no_contact", operation.OutcomeFailedNoContact, operation.DeliveryNotSent, nil, ""},
		{"acknowledged_unverified", operation.OutcomeAcknowledgedUnverified, operation.DeliverySent, evseAck(operation.AckAccepted), ""},
		{"applied", operation.OutcomeApplied, operation.DeliverySent, evseAck(operation.AckAccepted), operation.ReadbackConfirms},
		{"no_effect", operation.OutcomeNoEffect, operation.DeliverySent, evseAck(operation.AckRejected), ""},
		{"conflict", operation.OutcomeConflict, operation.DeliverySent, nil, operation.ReadbackContradicts},
		{"indeterminate", operation.OutcomeIndeterminate, operation.DeliveryUnknown, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newEVSEFixture(t, nil)
			if tc.outcome == operation.OutcomeRejected {
				record, err := fixture.kernel.RecordRejection(fixture.intent, semreg.AuthorityMissing, []semreg.EvidenceRef{})
				if err != nil || record.Outcome != tc.outcome {
					t.Fatalf("rejection: %+v %v", record, err)
				}
				return
			}
			admission := admitEVSE(t, fixture)
			record := evseRecord(admission)
			record.Outcome, record.Dispatch, record.Acknowledgement = tc.outcome, evseDispatch(tc.delivery), tc.acknowledgement
			var later *semreg.Snapshot
			if tc.relation != "" {
				value := fixture.intent.ExpectedEffect.Expected
				if tc.relation == operation.ReadbackContradicts {
					value = alternate(value)
				}
				snapshot := updateEVSEReadback(t, fixture, value)
				before, _ := semreg.CanonicalJSON(snapshot)
				fixture.hook.mutateReadback = true
				record.Readback, later = evseReadback(snapshot, tc.relation), &snapshot
				defer func() {
					after, _ := semreg.CanonicalJSON(snapshot)
					if !bytes.Equal(before, after) {
						t.Error("readback hook mutated snapshot input")
					}
				}()
			}
			stored, err := fixture.kernel.Record(admission, record, later)
			if err != nil || stored.Outcome != tc.outcome {
				t.Fatalf("record: %+v %v", stored, err)
			}
			if tc.relation != "" && fixture.hook.readbackCalls != 1 {
				t.Fatalf("readback calls %d", fixture.hook.readbackCalls)
			}
			if tc.outcome == operation.OutcomeIndeterminate {
				require(t, operation.ValidateRetry(stored, true), semreg.RetryForbidden)
				rewrite := stored
				rewrite.Outcome, rewrite.Dispatch = operation.OutcomeFailedNoContact, evseDispatch(operation.DeliveryNotSent)
				_, err := fixture.kernel.Record(admission, rewrite, nil)
				require(t, err, semreg.SequenceConflict)
			}
		})
	}
}

type evseRecordHook struct {
	validator
	readbackCalls  int
	mutateReadback bool
}

func (h *evseRecordHook) EvaluateReadback(intent operation.Intent, candidate semreg.FactCandidate) (operation.ReadbackRelation, error) {
	h.readbackCalls++
	relation, err := h.validator.EvaluateReadback(intent, candidate)
	if h.mutateReadback && candidate.Value != nil {
		candidate.Value.Quantity = nil
	}
	return relation, err
}

type evseFixture struct {
	hook        *evseRecordHook
	publication *semreg.PublicationKernel
	kernel      *operation.Kernel
	snapshot    semreg.Snapshot
	intent      operation.Intent
	current     semreg.EvaluationContext
}

func newEVSEFixture(t *testing.T, mutate func(*semreg.CapabilityInstance)) evseFixture {
	t.Helper()
	hook := &evseRecordHook{}
	publication, err := semreg.NewPublicationKernel("asset:1", hook)
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := operation.NewKernel(hook)
	if err != nil {
		t.Fatal(err)
	}
	requested := intent()
	requested.Causal.FirstSeenAt, requested.Causal.ExpiresAt, requested.Deadline = evseTime("100"), evseTime("400"), evseTime("300")
	service := semreg.ServiceInstance{InstanceID: "service:1", AssetID: "asset:1", Definition: definition("evse.service.connector"), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}
	capability := testCapability("evse.capability.set_allocated_current")
	if mutate != nil {
		mutate(&capability)
	}
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:evse:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Sequence: "1", ExpectedSemanticRevision: "0", ObservedAt: evseTime("100"), SourceUpserts: []semreg.SourceDescriptor{{SourceID: "source:1", SourceEpochID: "epoch:1", ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1", RegistryEvidence: testEvidence(), StartedAt: evseTime("90"), State: semreg.SourceCurrent, Revision: "1"}}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{{BindingID: "binding:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", NativeResource: testEvidence(), State: semreg.BindingCurrent, Revision: "1"}}, IdentityLinkUpserts: []semreg.IdentityLink{{AssetID: "asset:1", BindingID: "binding:1", State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}}, FactUpserts: []semreg.FactCandidate{evseFact(requested.ExpectedEffect.Fact, requested.ExpectedEffect.Expected, "100", "100", "1")}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{service}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{capability}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "100"})
	if err != nil {
		t.Fatal(err)
	}
	return evseFixture{hook: hook, publication: publication, kernel: kernel, snapshot: snapshot, intent: requested, current: semreg.EvaluationContext{EvaluatedAt: evseTime("150"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "150"}}}
}
func evseTime(ns string) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(ns), ClockID: "clock.utc", UncertaintyNS: "0"}
}
func evseFact(key semreg.FactKey, value semreg.Value, wall, ticks, revision string) semreg.FactCandidate {
	candidate := candidate(key, value)
	candidate.CandidateID, candidate.Revision = "candidate:evse", semreg.Uint64(revision)
	candidate.Times = semreg.Times{ReceivedAt: evseTime(wall), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: semreg.Uint64(ticks)}, EvaluatedAt: evseTime(wall), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: semreg.Uint64(ticks)}}
	candidate.FreshnessPolicy = semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1000", RetainForNS: "2000", MaxWallUncertaintyNS: "0"}
	return candidate
}
func admitEVSE(t *testing.T, fixture evseFixture) *operation.Admission {
	t.Helper()
	admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	return admission
}
func evseRecord(admission *operation.Admission) operation.ExecutionRecord {
	intent := admission.Intent()
	route, _ := admission.Route()
	at, revisions, _ := admission.AdmittedAt()
	return operation.ExecutionRecord{Contract: operation.ContractOperationV1, Intent: intent, AdmittedAt: &at, AdmittedRevision: &revisions, Route: &route, OutcomeEvidence: []semreg.EvidenceRef{testEvidence()}}
}
func evseDispatch(delivery operation.DeliveryState) *operation.DispatchEvidence {
	completed := semreg.EvaluationContext{EvaluatedAt: evseTime("200"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "200"}}
	return &operation.DispatchEvidence{AttemptID: "attempt:1", Started: semreg.EvaluationContext{EvaluatedAt: evseTime("180"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "180"}}, Completed: &completed, Delivery: delivery, PossibleSideEffect: delivery != operation.DeliveryNotSent, Evidence: []semreg.EvidenceRef{testEvidence()}}
}
func evseAck(state operation.AckState) *operation.Acknowledgement {
	return &operation.Acknowledgement{State: state, At: evseTime("210"), Evidence: []semreg.EvidenceRef{testEvidence()}}
}
func updateEVSEReadback(t *testing.T, fixture evseFixture, value semreg.Value) semreg.Snapshot {
	t.Helper()
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:evse:2", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Sequence: "2", ExpectedSemanticRevision: fixture.snapshot.Revisions.Semantic, ObservedAt: evseTime("220"), SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{}, IdentityLinkUpserts: []semreg.IdentityLink{}, FactUpserts: []semreg.FactCandidate{evseFact(fixture.intent.ExpectedEffect.Fact, value, "220", "220", "2")}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := fixture.publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "220"})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func evseReadback(snapshot semreg.Snapshot, relation operation.ReadbackRelation) *operation.Readback {
	return &operation.Readback{SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions, CandidateID: "candidate:evse", CandidateRevision: "2", BindingID: "binding:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Relation: relation, Evaluation: semreg.EvaluationContext{EvaluatedAt: evseTime("240"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "240"}}, Evidence: []semreg.EvidenceRef{testEvidence()}}
}
func alternate(value semreg.Value) semreg.Value {
	result := value
	result.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: value.Quantity.Unit}
	if reflect.DeepEqual(result, value) {
		result.Quantity.Number = semreg.Decimal{Coefficient: "2"}
	}
	return result
}
