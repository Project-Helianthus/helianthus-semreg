package storage

import (
	"bytes"
	"reflect"
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestStorageLimitOperationsAdmitWithoutStatusPrecondition(t *testing.T) {
	v := New().(operation.OperationPackValidator)
	for _, id := range []semreg.DefinitionID{"storage.operation.set_charge_limit", "storage.operation.set_discharge_limit"} {
		i := storageIntent(id)
		if err := v.ValidateIntent(i); err != nil {
			t.Fatal(id, err)
		}
		k, err := operation.NewKernel(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := k.ValidateIntent(i); err != nil {
			t.Fatal(id, err)
		}
		extra := i
		extra.Preconditions = []operation.Precondition{storageCallerPrecondition()}
		if err := v.ValidateIntent(extra); err != nil {
			t.Fatal(id, "caller precondition", err)
		}
		c := storageCandidate(i.ExpectedEffect.Fact, i.ExpectedEffect.Expected)
		r, err := v.EvaluateReadback(i, c)
		if err != nil || r != operation.ReadbackConfirms {
			t.Fatal(r, err)
		}
		c.Value = &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.watt"}}
		r, err = v.EvaluateReadback(i, c)
		if err != nil || r != operation.ReadbackContradicts {
			t.Fatal(r, err)
		}
		c.Key.FactID = "storage.status.alarm"
		r, err = v.EvaluateReadback(i, c)
		if err != nil || r != operation.ReadbackInconclusive {
			t.Fatal(r, err)
		}
	}
}
func storageIntent(id semreg.DefinitionID) operation.Intent {
	_, cap, field, effect, ok := operationShape(id)
	if !ok {
		panic(id)
	}
	s := must(field)
	x := testValue(s)
	return operation.Intent{Contract: operation.ContractOperationV1, IntentID: "intent:1", Kind: definition(id), ExpectedEffect: operation.ExpectedEffect{Rule: definition(effect), Fact: testKey(s), Operator: semreg.PredicateEqual, Expected: x}, AssetID: "asset:1", Arguments: []semreg.TypedField{{ID: field, Value: x}}, RequiredCapability: operation.CapabilityRequirement{Pack: pack, DefinitionID: cap, Versions: semreg.VersionRange{Minimum: packVersion, MaximumExclusive: "2.0.0"}}, Authority: testEvidence(), Causal: semreg.CausalContext{Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{testEvidence()}}, CorrelationID: "correlation:1", MaxHops: 1, FirstSeenAt: storageTime("1"), ExpiresAt: storageTime("2"), Path: []semreg.TargetID{}}, ExpectedSemanticRevision: "1", ExpectedCapabilityRevision: "1", ExpectedCapabilityInstanceRevision: "1", ExpectedSourceEpochID: "epoch:1", ExpectedDriverGeneration: "1", Preconditions: []operation.Precondition{}, IdempotencyKey: "key:1", Deadline: storageTime("3")}
}
func storageCallerPrecondition() operation.Precondition {
	name := "id"
	return operation.Precondition{Fact: semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: "storage.status.alarm", Dimensions: []semreg.Dimension{{ID: "storage.dimension.pack", Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}}, CandidateID: "candidate:caller", CandidateRevision: "1", Operator: semreg.PredicateEqual, Expected: storageCallerValue()}
}
func storageCallerValue() semreg.Value {
	return semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: "storage.status.alarm", Token: "clear", Known: true}}
}
func storageCandidate(k semreg.FactKey, x semreg.Value) semreg.FactCandidate {
	b, e, g := semreg.NativeBindingID("binding:1"), semreg.SourceEpochID("epoch:1"), semreg.Uint64("1")
	source := semreg.SourceID("source:1")
	return semreg.FactCandidate{CandidateID: "candidate:1", Key: k, Value: &x, Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}}, Times: semreg.Times{ReceivedAt: storageTime("1"), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:1", Nanoseconds: "1"}, EvaluatedAt: storageTime("1"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:1", Nanoseconds: "1"}}, FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1", RetainForNS: "2", MaxWallUncertaintyNS: "0"}, BindingID: &b, SourceEpochID: &e, DriverGeneration: &g, Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &e, BindingID: &b, Evidence: []semreg.EvidenceRef{testEvidence()}}, Evidence: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}
}
func storageTime(n string) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(n), ClockID: "clock.utc", UncertaintyNS: "0"}
}

func TestStoragePowerDurationForms(t *testing.T) {
	v := New().(operation.OperationPackValidator)
	signed := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "-15", Exponent10: 2}, Unit: "unit.watt"}}
	active := semreg.TypedField{ID: "storage.setpoint.active", Value: testValue(must("storage.setpoint.active"))}
	fullDuration := semreg.TypedField{ID: "storage.setpoint.duration", Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "6", Exponent10: 1}, Unit: "unit.second"}}}
	zeroDuration := semreg.TypedField{ID: "storage.setpoint.duration", Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: "unit.second"}}}
	for _, id := range []semreg.DefinitionID{"storage.operation.set_ac_power", "storage.operation.set_dc_power", "storage.operation.set_pcc_power"} {
		_, _, field, effect, ok := operationShape(id)
		if !ok {
			t.Fatal(id)
		}
		base := storageIntent(id)
		base.Arguments[0].Value = signed
		base.ExpectedEffect.Expected = signed
		forms := [][]semreg.TypedField{
			{base.Arguments[0], active, fullDuration},
			{base.Arguments[0], active},
			{base.Arguments[0], active, zeroDuration},
		}
		for _, arguments := range forms {
			intent := base
			intent.Arguments = append([]semreg.TypedField{}, arguments...)
			sort.Slice(intent.Arguments, func(i, j int) bool { return intent.Arguments[i].ID < intent.Arguments[j].ID })
			if err := v.ValidateIntent(intent); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			if intent.ExpectedEffect.Fact.FactID != field || intent.ExpectedEffect.Rule.ID != effect || !reflect.DeepEqual(intent.ExpectedEffect.Expected, signed) {
				t.Fatalf("%s effect %+v", id, intent.ExpectedEffect)
			}
		}
		missing := base
		missing.Arguments = []semreg.TypedField{active, fullDuration}
		sort.Slice(missing.Arguments, func(i, j int) bool { return missing.Arguments[i].ID < missing.Arguments[j].ID })
		if v.ValidateIntent(missing) == nil {
			t.Fatalf("%s missing primary accepted", id)
		}
	}
	for _, operation := range Metadata().Operations {
		parameters, ok := operationParameters[operation.Ref.ID]
		if !ok {
			continue
		}
		if len(operation.Parameters) != len(parameters) {
			t.Fatalf("%s parameters %+v", operation.Ref.ID, operation.Parameters)
		}
		for i, id := range parameters {
			if operation.Parameters[i].ID != id {
				t.Fatalf("%s parameter %d = %s, want %s", operation.Ref.ID, i, operation.Parameters[i].ID, id)
			}
		}
	}
}

var _ = reflect.DeepEqual

type storageAdmissionFixture struct {
	publication *semreg.PublicationKernel
	kernel      *operation.Kernel
	snapshot    semreg.Snapshot
	intent      operation.Intent
	current     semreg.EvaluationContext
}

func TestStorageKernelAdmissionFailClosedBeforeAuthority(t *testing.T) {
	for _, operationID := range []semreg.DefinitionID{"storage.operation.set_charge_limit", "storage.operation.set_discharge_limit"} {
		t.Run(string(operationID)+"/caller precondition met", func(t *testing.T) {
			fixture := newStorageAdmissionFixture(t, operationID, nil)
			calls := 0
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { calls++; return nil }))
			if err != nil || calls != 1 {
				t.Fatalf("admission %v calls=%d", err, calls)
			}
		})
		for _, tc := range []struct {
			name   string
			mutate func(*semreg.FactCandidate)
		}{
			{"stale", func(c *semreg.FactCandidate) { c.FreshnessPolicy.FreshForNS = "1" }},
			{"unqualified", func(c *semreg.FactCandidate) {
				c.Quality.Qualification = semreg.QualificationCandidate
				c.Quality.Promotion = semreg.PromotionUnpromoted
			}},
			{"unpromoted", func(c *semreg.FactCandidate) { c.Quality.Promotion = semreg.PromotionUnpromoted }},
			{"degraded", func(c *semreg.FactCandidate) { c.Quality.Availability = semreg.AvailabilityDegraded }},
			{"unavailable", func(c *semreg.FactCandidate) {
				c.Quality.Availability = semreg.AvailabilityUnavailable
				c.Quality.Promotion = semreg.PromotionUnpromoted
			}},
			{"unknown", func(c *semreg.FactCandidate) {
				c.Quality.Validity = semreg.ValidityUnknown
				c.Quality.Promotion = semreg.PromotionUnpromoted
			}},
			{"candidate revision", func(c *semreg.FactCandidate) { c.Revision = "2" }},
		} {
			t.Run(string(operationID)+"/"+tc.name, func(t *testing.T) {
				fixture := newStorageAdmissionFixture(t, operationID, tc.mutate)
				_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil }))
				if err == nil {
					t.Fatal("fail-closed state admitted")
				}
			})
		}
	}
}

func TestStorageLimitOperationsCallerPreconditionStillGoverned(t *testing.T) {
	for _, operationID := range []semreg.DefinitionID{"storage.operation.set_charge_limit", "storage.operation.set_discharge_limit"} {
		for _, tc := range []struct {
			name   string
			mutate func(*operation.Intent)
		}{
			{"unpublished candidate", func(i *operation.Intent) { i.Preconditions[0].CandidateID = "candidate:missing" }},
			{"stale revision", func(i *operation.Intent) { i.Preconditions[0].CandidateRevision = "2" }},
			{"unmet expected value", func(i *operation.Intent) { i.Preconditions[0].Expected.Symbol.Token = "present" }},
		} {
			t.Run(string(operationID)+"/"+tc.name, func(t *testing.T) {
				fixture := newStorageAdmissionFixture(t, operationID, nil)
				tc.mutate(&fixture.intent)
				_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil }))
				if got := semreg.ErrorIdentifier(err); got != semreg.PreconditionFailed {
					t.Fatalf("error %q (%v)", got, err)
				}
			})
		}
		t.Run(string(operationID)+"/no precondition", func(t *testing.T) {
			fixture := newStorageAdmissionFixture(t, operationID, nil)
			fixture.intent.Preconditions = []operation.Precondition{}
			calls := 0
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { calls++; return nil }))
			if err != nil || calls != 1 {
				t.Fatalf("admission %v calls=%d", err, calls)
			}
		})
	}
}

func newStorageAdmissionFixture(t *testing.T, operationID semreg.DefinitionID, mutate func(*semreg.FactCandidate)) storageAdmissionFixture {
	t.Helper()
	hook := New()
	publication, err := semreg.NewPublicationKernel("asset:1", hook)
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := operation.NewKernel(hook)
	if err != nil {
		t.Fatal(err)
	}
	requested := storageIntent(operationID)
	requested.Causal.FirstSeenAt, requested.Causal.ExpiresAt, requested.Deadline = storageTime("100"), storageTime("400"), storageTime("300")
	service := semreg.ServiceInstance{InstanceID: "service:1", AssetID: "asset:1", Definition: definition("storage.service.interface"), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}
	capability := testCapability(requested.RequiredCapability.DefinitionID)
	requested.Preconditions = []operation.Precondition{storageCallerPrecondition()}
	caller := storageFact(requested.Preconditions[0].Fact, storageCallerValue(), "candidate:caller", "1")
	if mutate != nil {
		mutate(&caller)
	}
	effect := storageFact(requested.ExpectedEffect.Fact, requested.ExpectedEffect.Expected, "candidate:effect", "1")
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:storage:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Sequence: "1", ExpectedSemanticRevision: "0", ObservedAt: storageTime("100"), SourceUpserts: []semreg.SourceDescriptor{{SourceID: "source:1", SourceEpochID: "epoch:1", ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1", RegistryEvidence: testEvidence(), StartedAt: storageTime("90"), State: semreg.SourceCurrent, Revision: "1"}}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{{BindingID: "binding:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", NativeResource: testEvidence(), State: semreg.BindingCurrent, Revision: "1"}}, IdentityLinkUpserts: []semreg.IdentityLink{{AssetID: "asset:1", BindingID: "binding:1", State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}}, FactUpserts: []semreg.FactCandidate{caller, effect}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{service}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{capability}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "100"})
	if err != nil {
		t.Fatal(err)
	}
	return storageAdmissionFixture{publication: publication, kernel: kernel, snapshot: snapshot, intent: requested, current: semreg.EvaluationContext{EvaluatedAt: storageTime("150"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "150"}}}
}
func storageFact(key semreg.FactKey, value semreg.Value, id semreg.CandidateID, revision semreg.Uint64) semreg.FactCandidate {
	c := storageCandidate(key, value)
	c.CandidateID = id
	c.Revision = revision
	c.Times = semreg.Times{ReceivedAt: storageTime("100"), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "100"}, EvaluatedAt: storageTime("100"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "100"}}
	c.FreshnessPolicy = semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1000", RetainForNS: "2000", MaxWallUncertaintyNS: "0"}
	return c
}

var _ = bytes.Equal

func TestStorageTerminalOutcomesThroughKernel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outcome  operation.Outcome
		delivery operation.DeliveryState
		relation operation.ReadbackRelation
	}{
		{"rejected", operation.OutcomeRejected, "", ""},
		{"failed", operation.OutcomeFailedNoContact, operation.DeliveryNotSent, ""},
		{"acknowledged", operation.OutcomeAcknowledgedUnverified, operation.DeliverySent, ""},
		{"applied", operation.OutcomeApplied, operation.DeliverySent, operation.ReadbackConfirms},
		{"no_effect", operation.OutcomeNoEffect, operation.DeliverySent, ""},
		{"conflict", operation.OutcomeConflict, operation.DeliverySent, operation.ReadbackContradicts},
		{"indeterminate", operation.OutcomeIndeterminate, operation.DeliveryUnknown, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newStorageAdmissionFixture(t, "storage.operation.set_charge_limit", nil)
			if tc.outcome == operation.OutcomeRejected {
				record, err := fixture.kernel.RecordRejection(fixture.intent, semreg.AuthorityMissing, []semreg.EvidenceRef{})
				if err != nil || record.Outcome != tc.outcome {
					t.Fatal(record, err)
				}
				return
			}
			admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil }))
			if err != nil {
				t.Fatal(err)
			}
			intent := admission.Intent()
			route, _ := admission.Route()
			at, revisions, _ := admission.AdmittedAt()
			record := operation.ExecutionRecord{Contract: operation.ContractOperationV1, Intent: intent, AdmittedAt: &at, AdmittedRevision: &revisions, Route: &route, Outcome: tc.outcome, Dispatch: storageDispatch(tc.delivery), OutcomeEvidence: []semreg.EvidenceRef{testEvidence()}}
			if tc.outcome == operation.OutcomeAcknowledgedUnverified || tc.outcome == operation.OutcomeApplied {
				record.Acknowledgement = &operation.Acknowledgement{State: operation.AckAccepted, At: storageTime("210"), Evidence: []semreg.EvidenceRef{testEvidence()}}
			}
			var later *semreg.Snapshot
			if tc.relation != "" {
				value := fixture.intent.ExpectedEffect.Expected
				if tc.relation == operation.ReadbackContradicts {
					value = alternateStorage(value)
				}
				snapshot := storageUpdateReadback(t, fixture, value)
				record.Readback, later = storageReadback(snapshot, tc.relation), &snapshot
			}
			stored, err := fixture.kernel.Record(admission, record, later)
			if err != nil || stored.Outcome != tc.outcome {
				t.Fatal(stored, err)
			}
			if tc.outcome == operation.OutcomeIndeterminate {
				require(t, operation.ValidateRetry(stored, true), semreg.RetryForbidden)
			}
		})
	}
}

func storageDispatch(delivery operation.DeliveryState) *operation.DispatchEvidence {
	completed := semreg.EvaluationContext{EvaluatedAt: storageTime("200"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "200"}}
	return &operation.DispatchEvidence{AttemptID: "attempt:1", Started: semreg.EvaluationContext{EvaluatedAt: storageTime("180"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "180"}}, Completed: &completed, Delivery: delivery, PossibleSideEffect: delivery != operation.DeliveryNotSent, Evidence: []semreg.EvidenceRef{testEvidence()}}
}

func storageUpdateReadback(t *testing.T, fixture storageAdmissionFixture, value semreg.Value) semreg.Snapshot {
	t.Helper()
	updated := storageFact(fixture.intent.ExpectedEffect.Fact, value, "candidate:effect", "2")
	updated.Times = semreg.Times{ReceivedAt: storageTime("220"), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "220"}, EvaluatedAt: storageTime("220"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "220"}}
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:storage:2", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Sequence: "2", ExpectedSemanticRevision: fixture.snapshot.Revisions.Semantic, ObservedAt: storageTime("220"), SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{}, IdentityLinkUpserts: []semreg.IdentityLink{}, FactUpserts: []semreg.FactCandidate{updated}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
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
func storageReadback(snapshot semreg.Snapshot, relation operation.ReadbackRelation) *operation.Readback {
	return &operation.Readback{SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions, CandidateID: "candidate:effect", CandidateRevision: "2", BindingID: "binding:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Relation: relation, Evaluation: semreg.EvaluationContext{EvaluatedAt: storageTime("240"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "240"}}, Evidence: []semreg.EvidenceRef{testEvidence()}}
}
func alternateStorage(value semreg.Value) semreg.Value {
	result := value
	result.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: value.Quantity.Unit}
	if reflect.DeepEqual(result, value) {
		result.Quantity.Number = semreg.Decimal{Coefficient: "2"}
	}
	return result
}
