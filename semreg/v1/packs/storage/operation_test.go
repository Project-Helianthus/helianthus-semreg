package storage

import (
	"bytes"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestStorageOperationsCanonicalInterlockAndReadback(t *testing.T) {
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
func TestStorageInterlockRejectionMatrix(t *testing.T) {
	v := New().(operation.OperationPackValidator)
	for _, tc := range []struct {
		name   string
		mutate func(*operation.Intent)
	}{
		{"missing", func(i *operation.Intent) { i.Preconditions = []operation.Precondition{} }},
		{"duplicate", func(i *operation.Intent) { i.Preconditions = append(i.Preconditions, i.Preconditions[0]) }},
		{"unrelated", func(i *operation.Intent) {
			i.Preconditions[0].Fact.FactID = "storage.status.alarm"
			i.Preconditions[0].Expected = semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: "storage.status.alarm", Token: "clear", Known: true}}
		}},
		{"wrong pack", func(i *operation.Intent) { i.Preconditions[0].Fact.PackID = "helianthus.pack.evse" }},
		{"wrong version", func(i *operation.Intent) { i.Preconditions[0].Fact.PackVersion = "1.0.0" }},
		{"wrong dimension", func(i *operation.Intent) { i.Preconditions[0].Fact.Dimensions[0].ID = "storage.dimension.pack" }},
		{"different interface value", func(i *operation.Intent) {
			other := "other-interface"
			i.Preconditions[0].Fact.Dimensions[0].Value.Text = &other
		}},
		{"wrong operator", func(i *operation.Intent) { i.Preconditions[0].Operator = semreg.PredicateNotEqual }},
		{"active", func(i *operation.Intent) { i.Preconditions[0].Expected.Symbol.Token = "active" }},
		{"unknown", func(i *operation.Intent) { i.Preconditions[0].Expected.Symbol.Known = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := storageIntent("storage.operation.set_charge_limit")
			tc.mutate(&i)
			if v.ValidateIntent(i) == nil {
				t.Fatal("invalid interlock accepted")
			}
		})
	}
}
func storageIntent(id semreg.DefinitionID) operation.Intent {
	_, cap, field, effect, ok := operationShape(id)
	if !ok {
		panic(id)
	}
	s := must(field)
	x := testValue(s)
	name := "id"
	interlock := operation.Precondition{Fact: semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: "storage.status.interlock", Dimensions: []semreg.Dimension{{ID: "storage.dimension.interface", Value: semreg.Value{Kind: semreg.ValueText, Text: &name}}}}, CandidateID: "candidate:interlock", CandidateRevision: "1", Operator: semreg.PredicateEqual, Expected: clearValue()}
	return operation.Intent{Contract: operation.ContractOperationV1, IntentID: "intent:1", Kind: definition(id), ExpectedEffect: operation.ExpectedEffect{Rule: definition(effect), Fact: testKey(s), Operator: semreg.PredicateEqual, Expected: x}, AssetID: "asset:1", Arguments: []semreg.TypedField{{ID: field, Value: x}}, RequiredCapability: operation.CapabilityRequirement{Pack: pack, DefinitionID: cap, Versions: semreg.VersionRange{Minimum: packVersion, MaximumExclusive: "2.0.0"}}, Authority: testEvidence(), Causal: semreg.CausalContext{Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{testEvidence()}}, CorrelationID: "correlation:1", MaxHops: 1, FirstSeenAt: storageTime("1"), ExpiresAt: storageTime("2"), Path: []semreg.TargetID{}}, ExpectedSemanticRevision: "1", ExpectedCapabilityRevision: "1", ExpectedCapabilityInstanceRevision: "1", ExpectedSourceEpochID: "epoch:1", ExpectedDriverGeneration: "1", Preconditions: []operation.Precondition{interlock}, IdempotencyKey: "key:1", Deadline: storageTime("3")}
}
func storageCandidate(k semreg.FactKey, x semreg.Value) semreg.FactCandidate {
	b, e, g := semreg.NativeBindingID("binding:1"), semreg.SourceEpochID("epoch:1"), semreg.Uint64("1")
	source := semreg.SourceID("source:1")
	return semreg.FactCandidate{CandidateID: "candidate:1", Key: k, Value: &x, Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}}, Times: semreg.Times{ReceivedAt: storageTime("1"), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:1", Nanoseconds: "1"}, EvaluatedAt: storageTime("1"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:1", Nanoseconds: "1"}}, FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1", RetainForNS: "2", MaxWallUncertaintyNS: "0"}, BindingID: &b, SourceEpochID: &e, DriverGeneration: &g, Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &e, BindingID: &b, Evidence: []semreg.EvidenceRef{testEvidence()}}, Evidence: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}
}
func storageTime(n string) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(n), ClockID: "clock.utc", UncertaintyNS: "0"}
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
		t.Run(string(operationID)+"/clear", func(t *testing.T) {
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

func TestStorageKernelRejectsIntentPredicateBeforeAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*operation.Intent)
	}{
		{"missing", func(i *operation.Intent) { i.Preconditions = []operation.Precondition{} }},
		{"duplicate", func(i *operation.Intent) { i.Preconditions = append(i.Preconditions, i.Preconditions[0]) }},
		{"unrelated", func(i *operation.Intent) {
			i.Preconditions[0].Fact.FactID = "storage.status.alarm"
			i.Preconditions[0].Expected = semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: "storage.status.alarm", Token: "clear", Known: true}}
		}},
		{"wrong interface", func(i *operation.Intent) {
			other := "other-interface"
			i.Preconditions[0].Fact.Dimensions[0].Value.Text = &other
		}},
		{"active", func(i *operation.Intent) { i.Preconditions[0].Expected.Symbol.Token = "active" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newStorageAdmissionFixture(t, "storage.operation.set_charge_limit", nil)
			tc.mutate(&fixture.intent)
			calls := 0
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { calls++; return nil }))
			if err == nil {
				t.Fatal("invalid predicate admitted")
			}
			if calls != 0 {
				t.Fatalf("authority called %d", calls)
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
	interlock := storageFact(requested.Preconditions[0].Fact, clearValue(), "candidate:interlock", "1")
	if mutate != nil {
		mutate(&interlock)
	}
	effect := storageFact(requested.ExpectedEffect.Fact, requested.ExpectedEffect.Expected, "candidate:effect", "1")
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:storage:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Sequence: "1", ExpectedSemanticRevision: "0", ObservedAt: storageTime("100"), SourceUpserts: []semreg.SourceDescriptor{{SourceID: "source:1", SourceEpochID: "epoch:1", ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1", RegistryEvidence: testEvidence(), StartedAt: storageTime("90"), State: semreg.SourceCurrent, Revision: "1"}}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{{BindingID: "binding:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", NativeResource: testEvidence(), State: semreg.BindingCurrent, Revision: "1"}}, IdentityLinkUpserts: []semreg.IdentityLink{{AssetID: "asset:1", BindingID: "binding:1", State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}}, FactUpserts: []semreg.FactCandidate{effect, interlock}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{service}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{capability}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
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
