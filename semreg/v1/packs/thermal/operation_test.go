package thermal

import (
	"bytes"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestBothOperationsEffectsAndReadback(t *testing.T) {
	validator := New().(operation.OperationPackValidator)
	for _, operationID := range []semreg.DefinitionID{"thermal.operation.set_temperature", "thermal.operation.set_mode"} {
		t.Run(string(operationID), func(t *testing.T) {
			intent := validIntent(operationID)
			if err := validator.ValidateIntent(intent); err != nil {
				t.Fatal(err)
			}
			kernel, err := operation.NewKernel(validator)
			if err != nil {
				t.Fatal(err)
			}
			if err := kernel.ValidateIntent(intent); err != nil {
				t.Fatal(err)
			}
			candidate := validCandidate(intent.ExpectedEffect.Fact, intent.ExpectedEffect.Expected)
			relation, err := validator.EvaluateReadback(intent, candidate)
			if err != nil || relation != operation.ReadbackConfirms {
				t.Fatalf("confirm: %s %v", relation, err)
			}
			candidate.Value = &semreg.Value{Kind: intent.ExpectedEffect.Expected.Kind, Quantity: intent.ExpectedEffect.Expected.Quantity, Symbol: intent.ExpectedEffect.Expected.Symbol}
			if candidate.Value.Quantity != nil {
				candidate.Value.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: candidate.Value.Quantity.Unit}
				if reflect.DeepEqual(*candidate.Value, intent.ExpectedEffect.Expected) {
					candidate.Value.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: candidate.Value.Quantity.Unit}
				}
			} else {
				candidate.Value.Symbol = &semreg.Symbol{Namespace: intent.ExpectedEffect.Expected.Symbol.Namespace, Token: "off", Known: true}
				if reflect.DeepEqual(*candidate.Value, intent.ExpectedEffect.Expected) {
					candidate.Value.Symbol.Token = "auto"
				}
			}
			relation, err = validator.EvaluateReadback(intent, candidate)
			if err != nil || relation != operation.ReadbackContradicts {
				t.Fatalf("contradiction: %s %v", relation, err)
			}
			candidate.Key.FactID = "thermal.status.fault"
			relation, err = validator.EvaluateReadback(intent, candidate)
			if err != nil || relation != operation.ReadbackInconclusive {
				t.Fatalf("inconclusive: %s %v", relation, err)
			}
		})
	}
}

func TestOperationRejectsIncorrectEffectAndArguments(t *testing.T) {
	validator := New().(operation.OperationPackValidator)
	cases := []struct {
		name   string
		mutate func(*operation.Intent)
		want   semreg.ErrorID
	}{
		{"wrong kind", func(v *operation.Intent) { v.Kind.ID = "thermal.operation.set_mode" }, semreg.InvalidValue},
		{"wrong operation pack", func(v *operation.Intent) { v.Kind.Pack.ID = "helianthus.pack.evse" }, semreg.InvalidValue},
		{"wrong operation version", func(v *operation.Intent) { v.Kind.Version = "1.0.1" }, semreg.DefinitionOwnerMissing},
		{"no arguments", func(v *operation.Intent) { v.Arguments = []semreg.TypedField{} }, semreg.InvalidValue},
		{"extra arguments", func(v *operation.Intent) { v.Arguments = append(v.Arguments, v.Arguments[0]) }, semreg.DuplicateKey},
		{"wrong argument field", func(v *operation.Intent) {
			v.Arguments[0] = semreg.TypedField{ID: "thermal.mode.system", Value: validValue(mustField("thermal.mode.system"))}
		}, semreg.InvalidValue},
		{"wrong argument value", func(v *operation.Intent) {
			v.Arguments[0].Value.Quantity.Number = above(mustField("thermal.setpoint.temperature").maximum.decimal())
		}, semreg.BoundsExceeded},
		{"wrong effect rule", func(v *operation.Intent) { v.ExpectedEffect.Rule = definition("thermal.effect.set_mode") }, semreg.InvalidValue},
		{"wrong effect fact", func(v *operation.Intent) { v.ExpectedEffect.Fact.FactID = "thermal.setpoint.dhw_temperature" }, semreg.InvalidValue},
		{"wrong effect operator", func(v *operation.Intent) { v.ExpectedEffect.Operator = semreg.PredicateNotEqual }, semreg.InvalidValue},
		{"wrong effect value", func(v *operation.Intent) {
			v.ExpectedEffect.Expected.Quantity.Number = semreg.Decimal{Coefficient: "0"}
		}, semreg.InvalidValue},
		{"wrong required capability", func(v *operation.Intent) { v.RequiredCapability.DefinitionID = "thermal.capability.set_mode" }, semreg.InvalidValue},
		{"wrong required capability pack", func(v *operation.Intent) { v.RequiredCapability.Pack.ID = "helianthus.pack.evse" }, semreg.InvalidValue},
		{"wrong required capability version", func(v *operation.Intent) { v.RequiredCapability.Versions.Minimum = "1.0.1" }, semreg.InvalidValue},
		{"unsupported operation", func(v *operation.Intent) { v.Kind = definition("thermal.operation.unknown") }, semreg.DefinitionOwnerMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := validIntent("thermal.operation.set_temperature")
			tc.mutate(&candidate)
			requireID(t, validator.ValidateIntent(candidate), tc.want)
		})
	}
}

func TestOperationKernelDetachesThermalHookInput(t *testing.T) {
	hook := mutatingThermalHook{}
	kernel, err := operation.NewKernel(hook)
	if err != nil {
		t.Fatal(err)
	}
	intent := validIntent("thermal.operation.set_temperature")
	before := intent
	before.Arguments = append([]semreg.TypedField(nil), intent.Arguments...)
	if err := kernel.ValidateIntent(intent); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(intent, before) {
		t.Fatal("operation hook mutated caller intent")
	}
}

type mutatingThermalHook struct{ validator }

func (m mutatingThermalHook) ValidateIntent(intent operation.Intent) error {
	err := m.validator.ValidateIntent(intent)
	intent.Arguments[0].ID = "thermal.mutated.argument"
	return err
}

func TestEveryThermalTerminalOutcomeThroughKernel(t *testing.T) {
	for _, operationID := range []semreg.DefinitionID{"thermal.operation.set_temperature", "thermal.operation.set_mode"} {
		t.Run(string(operationID), func(t *testing.T) {
			fixture := newThermalFixture(t, operationID)
			first, err := fixture.kernel.RecordRejection(fixture.intent, semreg.AuthorityMissing, []semreg.EvidenceRef{})
			if err != nil || first.Outcome != operation.OutcomeRejected {
				t.Fatalf("rejected: %+v %v", first, err)
			}
		})
		for _, tc := range []struct {
			name     string
			outcome  operation.Outcome
			delivery operation.DeliveryState
			ack      *operation.Acknowledgement
			readback operation.ReadbackRelation
		}{
			{"failed_no_contact", operation.OutcomeFailedNoContact, operation.DeliveryNotSent, nil, ""},
			{"acknowledged_unverified", operation.OutcomeAcknowledgedUnverified, operation.DeliverySent, acceptedAck(), ""},
			{"applied", operation.OutcomeApplied, operation.DeliverySent, acceptedAck(), operation.ReadbackConfirms},
			{"no_effect", operation.OutcomeNoEffect, operation.DeliverySent, rejectedAck(), ""},
			{"conflict", operation.OutcomeConflict, operation.DeliverySent, nil, operation.ReadbackContradicts},
			{"indeterminate", operation.OutcomeIndeterminate, operation.DeliveryUnknown, nil, ""},
		} {
			t.Run(string(operationID)+"/"+tc.name, func(t *testing.T) {
				fixture := newThermalFixture(t, operationID)
				admission := admitThermal(t, fixture)
				record := thermalRecord(admission)
				record.Outcome, record.Dispatch, record.Acknowledgement = tc.outcome, thermalDispatch(tc.delivery), tc.ack
				var later *semreg.Snapshot
				if tc.readback != "" {
					value := fixture.intent.ExpectedEffect.Expected
					if tc.readback == operation.ReadbackContradicts {
						value = alternativeValue(value)
					}
					snapshot := updateThermalReadback(t, fixture, value)
					before, _ := semreg.CanonicalJSON(snapshot)
					fixture.hook.mutateReadback = true
					record.Readback = thermalReadback(snapshot, tc.readback)
					later = &snapshot
					defer func() {
						after, _ := semreg.CanonicalJSON(snapshot)
						if !bytes.Equal(before, after) {
							t.Error("readback hook mutated snapshot input")
						}
					}()
				}
				stored, err := fixture.kernel.Record(admission, record, later)
				if err != nil || stored.Outcome != tc.outcome {
					t.Fatalf("record %s: %+v %v", tc.name, stored, err)
				}
				if tc.readback != "" && fixture.hook.readbackCalls != 1 {
					t.Fatalf("readback calls = %d", fixture.hook.readbackCalls)
				}
				if tc.outcome == operation.OutcomeIndeterminate {
					requireID(t, operation.ValidateRetry(stored, true), semreg.RetryForbidden)
					rewrite := stored
					rewrite.Outcome = operation.OutcomeFailedNoContact
					rewrite.Dispatch = thermalDispatch(operation.DeliveryNotSent)
					_, err := fixture.kernel.Record(admission, rewrite, nil)
					requireID(t, err, semreg.SequenceConflict)
				}
			})
		}
	}
}

type thermalRecordHook struct {
	validator
	readbackCalls  int
	mutateReadback bool
}

func (h *thermalRecordHook) EvaluateReadback(intent operation.Intent, candidate semreg.FactCandidate) (operation.ReadbackRelation, error) {
	h.readbackCalls++
	relation, err := h.validator.EvaluateReadback(intent, candidate)
	if h.mutateReadback && candidate.Value != nil {
		candidate.Value.Quantity = nil
		candidate.Value.Symbol = nil
	}
	return relation, err
}

type thermalFixture struct {
	hook        *thermalRecordHook
	publication *semreg.PublicationKernel
	kernel      *operation.Kernel
	snapshot    semreg.Snapshot
	intent      operation.Intent
	current     semreg.EvaluationContext
}

func newThermalFixture(t *testing.T, operationID semreg.DefinitionID) thermalFixture {
	t.Helper()
	hook := &thermalRecordHook{}
	publication, err := semreg.NewPublicationKernel("asset:1", hook)
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := operation.NewKernel(hook)
	if err != nil {
		t.Fatal(err)
	}
	intent := validIntent(operationID)
	intent.Causal.FirstSeenAt, intent.Causal.ExpiresAt, intent.Deadline = thermalTime("100"), thermalTime("400"), thermalTime("300")
	capability := capabilities[operations[operationID].capability]
	service := semreg.ServiceInstance{InstanceID: "service:1", AssetID: "asset:1", Definition: definition(capability.service), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}
	cap := validCapability(operations[operationID].capability)
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:thermal:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Sequence: "1", ExpectedSemanticRevision: "0", ObservedAt: thermalTime("100"), SourceUpserts: []semreg.SourceDescriptor{{SourceID: "source:1", SourceEpochID: "epoch:1", ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1", RegistryEvidence: evidence(), StartedAt: thermalTime("90"), State: semreg.SourceCurrent, Revision: "1"}}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{{BindingID: "binding:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", NativeResource: evidence(), State: semreg.BindingCurrent, Revision: "1"}}, IdentityLinkUpserts: []semreg.IdentityLink{{AssetID: "asset:1", BindingID: "binding:1", State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{evidence()}, Revision: "1"}}, FactUpserts: []semreg.FactCandidate{thermalFact(intent.ExpectedEffect.Fact, intent.ExpectedEffect.Expected, "100", "100", "1")}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{service}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{cap}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "100"})
	if err != nil {
		t.Fatal(err)
	}
	return thermalFixture{hook: hook, publication: publication, kernel: kernel, snapshot: snapshot, intent: intent, current: semreg.EvaluationContext{EvaluatedAt: thermalTime("150"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "150"}}}
}
func thermalTime(ns string) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(ns), ClockID: "clock.utc", UncertaintyNS: "0"}
}
func thermalFact(key semreg.FactKey, value semreg.Value, wall, ticks, revision string) semreg.FactCandidate {
	candidate := validCandidate(key, value)
	candidate.CandidateID = "candidate:thermal"
	candidate.Revision = semreg.Uint64(revision)
	candidate.Times = semreg.Times{ReceivedAt: thermalTime(wall), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: semreg.Uint64(ticks)}, EvaluatedAt: thermalTime(wall), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: semreg.Uint64(ticks)}}
	candidate.FreshnessPolicy = semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1000", RetainForNS: "2000", MaxWallUncertaintyNS: "0"}
	return candidate
}
func admitThermal(t *testing.T, fixture thermalFixture) *operation.Admission {
	t.Helper()
	admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	return admission
}
func thermalRecord(admission *operation.Admission) operation.ExecutionRecord {
	intent := admission.Intent()
	route, _ := admission.Route()
	at, revisions, _ := admission.AdmittedAt()
	return operation.ExecutionRecord{Contract: operation.ContractOperationV1, Intent: intent, AdmittedAt: &at, AdmittedRevision: &revisions, Route: &route, OutcomeEvidence: []semreg.EvidenceRef{evidence()}}
}
func thermalDispatch(delivery operation.DeliveryState) *operation.DispatchEvidence {
	completed := semreg.EvaluationContext{EvaluatedAt: thermalTime("200"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "200"}}
	return &operation.DispatchEvidence{AttemptID: "attempt:1", Started: semreg.EvaluationContext{EvaluatedAt: thermalTime("180"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "180"}}, Completed: &completed, Delivery: delivery, PossibleSideEffect: delivery != operation.DeliveryNotSent, Evidence: []semreg.EvidenceRef{evidence()}}
}
func acceptedAck() *operation.Acknowledgement {
	return &operation.Acknowledgement{State: operation.AckAccepted, At: thermalTime("210"), Evidence: []semreg.EvidenceRef{evidence()}}
}
func rejectedAck() *operation.Acknowledgement {
	return &operation.Acknowledgement{State: operation.AckRejected, At: thermalTime("210"), Evidence: []semreg.EvidenceRef{evidence()}}
}
func updateThermalReadback(t *testing.T, fixture thermalFixture, value semreg.Value) semreg.Snapshot {
	t.Helper()
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:thermal:2", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Sequence: "2", ExpectedSemanticRevision: fixture.snapshot.Revisions.Semantic, ObservedAt: thermalTime("220"), SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{}, IdentityLinkUpserts: []semreg.IdentityLink{}, FactUpserts: []semreg.FactCandidate{thermalFact(fixture.intent.ExpectedEffect.Fact, value, "220", "220", "2")}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
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
func thermalReadback(snapshot semreg.Snapshot, relation operation.ReadbackRelation) *operation.Readback {
	return &operation.Readback{SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions, CandidateID: "candidate:thermal", CandidateRevision: "2", BindingID: "binding:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Relation: relation, Evaluation: semreg.EvaluationContext{EvaluatedAt: thermalTime("240"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "240"}}, Evidence: []semreg.EvidenceRef{evidence()}}
}
func alternativeValue(value semreg.Value) semreg.Value {
	result := value
	if result.Quantity != nil {
		result.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: result.Quantity.Unit}
		if reflect.DeepEqual(result, value) {
			result.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: result.Quantity.Unit}
		}
	} else {
		result.Symbol = &semreg.Symbol{Namespace: value.Symbol.Namespace, Token: "off", Known: true}
		if reflect.DeepEqual(result, value) {
			result.Symbol.Token = "auto"
		}
	}
	return result
}

func validIntent(operationID semreg.DefinitionID) operation.Intent {
	spec := operations[operationID]
	field := mustField(spec.argument)
	value := validValue(field)
	effectKey := validKey(field)
	return operation.Intent{Contract: operation.ContractOperationV1, IntentID: "intent:1", Kind: definition(operationID), ExpectedEffect: operation.ExpectedEffect{Rule: definition(spec.effect), Fact: effectKey, Operator: semreg.PredicateEqual, Expected: value}, AssetID: "asset:1", Arguments: []semreg.TypedField{{ID: spec.argument, Value: value}}, RequiredCapability: operation.CapabilityRequirement{Pack: pack, DefinitionID: spec.capability, Versions: semreg.VersionRange{Minimum: "1.0.0", MaximumExclusive: "2.0.0"}}, Authority: evidence(), Causal: semreg.CausalContext{Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{evidence()}}, CorrelationID: "correlation:1", HopCount: 0, MaxHops: 1, FirstSeenAt: semreg.TimePoint{UnixNanoseconds: "1", ClockID: "clock.utc", UncertaintyNS: "0"}, ExpiresAt: semreg.TimePoint{UnixNanoseconds: "2", ClockID: "clock.utc", UncertaintyNS: "0"}, Path: []semreg.TargetID{}}, ExpectedSemanticRevision: "1", ExpectedCapabilityRevision: "1", ExpectedCapabilityInstanceRevision: "1", ExpectedSourceEpochID: "epoch:1", ExpectedDriverGeneration: "1", Preconditions: []operation.Precondition{}, IdempotencyKey: "idempotency:1", Deadline: semreg.TimePoint{UnixNanoseconds: "3", ClockID: "clock.utc", UncertaintyNS: "0"}}
}
func validCandidate(key semreg.FactKey, value semreg.Value) semreg.FactCandidate {
	binding, epoch, generation := semreg.NativeBindingID("binding:1"), semreg.SourceEpochID("epoch:1"), semreg.Uint64("1")
	source := semreg.SourceID("source:1")
	return semreg.FactCandidate{CandidateID: "candidate:1", Key: key, Value: &value, Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}}, Times: semreg.Times{ReceivedAt: semreg.TimePoint{UnixNanoseconds: "1", ClockID: "clock.utc", UncertaintyNS: "0"}, ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "1"}, EvaluatedAt: semreg.TimePoint{UnixNanoseconds: "1", ClockID: "clock.utc", UncertaintyNS: "0"}, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "1"}}, FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1", RetainForNS: "2", MaxWallUncertaintyNS: "0"}, BindingID: &binding, SourceEpochID: &epoch, DriverGeneration: &generation, Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []semreg.EvidenceRef{evidence()}}, Evidence: []semreg.EvidenceRef{evidence()}, Revision: "1"}
}
func mustField(id semreg.DefinitionID) fieldSpec {
	field, ok := findField(id)
	if !ok {
		panic(id)
	}
	return field
}
