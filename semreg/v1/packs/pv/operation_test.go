package pv

import (
	"bytes"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"reflect"
	"strings"
	"testing"
)

func TestActivePowerOperationAndReadback(t *testing.T) {
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
	for _, mut := range []func(*operation.Intent){func(x *operation.Intent) { x.Arguments = []semreg.TypedField{} }, func(x *operation.Intent) { x.RequiredCapability.DefinitionID = "pv.capability.read.connector" }, func(x *operation.Intent) { x.ExpectedEffect.Rule = definition("pv.effect.unknown") }, func(x *operation.Intent) { x.ExpectedEffect.Operator = semreg.PredicateNotEqual }, func(x *operation.Intent) { x.Kind = definition("pv.operation.unknown") }} {
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
	c.Value = &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: "unit.watt"}}
	r, err = v.EvaluateReadback(i, c)
	if err != nil || r != operation.ReadbackContradicts {
		t.Fatal(r, err)
	}
	c.Key.FactID = "pv.status.fault"
	r, err = v.EvaluateReadback(i, c)
	if err != nil || r != operation.ReadbackInconclusive {
		t.Fatal(r, err)
	}
}
func intent() operation.Intent {
	s := must("pv.limit.active_power")
	x := testValue(s)
	return operation.Intent{Contract: operation.ContractOperationV1, IntentID: "intent:1", Kind: definition("pv.operation.set_active_power_limit"), ExpectedEffect: operation.ExpectedEffect{Rule: definition("pv.effect.set_active_power_limit"), Fact: testKey(s), Operator: semreg.PredicateEqual, Expected: x}, AssetID: "asset:1", Arguments: []semreg.TypedField{{ID: s.id, Value: x}}, RequiredCapability: operation.CapabilityRequirement{Pack: pack, DefinitionID: "pv.capability.set_active_power_limit", Versions: semreg.VersionRange{Minimum: "1.0.0", MaximumExclusive: "2.0.0"}}, Authority: testEvidence(), Causal: semreg.CausalContext{Origin: semreg.OriginRef{OriginID: "origin:1", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{testEvidence()}}, CorrelationID: "correlation:1", MaxHops: 1, FirstSeenAt: time("1"), ExpiresAt: time("2"), Path: []semreg.TargetID{}}, ExpectedSemanticRevision: "1", ExpectedCapabilityRevision: "1", ExpectedCapabilityInstanceRevision: "1", ExpectedSourceEpochID: "epoch:1", ExpectedDriverGeneration: "1", Preconditions: []operation.Precondition{}, IdempotencyKey: "key:1", Deadline: time("3")}
}

func intentFor(id semreg.DefinitionID) operation.Intent {
	i := intent()
	if id == "pv.operation.set_export_limit" {
		_, cap, field, effect, _ := pvOperationShape(id)
		s := must(field)
		x := testValue(s)
		i.Kind = definition(id)
		i.ExpectedEffect = operation.ExpectedEffect{Rule: definition(effect), Fact: testKey(s), Operator: semreg.PredicateEqual, Expected: x}
		i.Arguments = []semreg.TypedField{{ID: field, Value: x}}
		i.RequiredCapability.DefinitionID = cap
	}
	return i
}
func TestPVExportKernelLifecycle(t *testing.T) {
	t.Run("constraint rejects before authority", func(t *testing.T) {
		f := newPVFixtureFor(t, "pv.operation.set_export_limit", func(c *semreg.CapabilityInstance) { c.Constraints[0].Value = alternate(c.Constraints[0].Value) })
		calls := 0
		_, e := f.kernel.Admit(f.snapshot, f.current, f.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { calls++; return nil }))
		require(t, e, semreg.AmbiguousRoute)
		if calls != 0 {
			t.Fatal(calls)
		}
	})
	for _, outcome := range []operation.Outcome{operation.OutcomeRejected, operation.OutcomeFailedNoContact, operation.OutcomeAcknowledgedUnverified, operation.OutcomeApplied, operation.OutcomeNoEffect, operation.OutcomeConflict, operation.OutcomeIndeterminate} {
		t.Run(string(outcome), func(t *testing.T) {
			f := newPVFixtureFor(t, "pv.operation.set_export_limit", nil)
			if outcome == operation.OutcomeRejected {
				r, e := f.kernel.RecordRejection(f.intent, semreg.AuthorityMissing, []semreg.EvidenceRef{})
				if e != nil || r.Outcome != outcome {
					t.Fatal(e, r)
				}
				return
			}
			a := admitPV(t, f)
			r := pvRecord(a)
			r.Outcome = outcome
			d := operation.DeliverySent
			if outcome == operation.OutcomeFailedNoContact {
				d = operation.DeliveryNotSent
			}
			if outcome == operation.OutcomeIndeterminate {
				d = operation.DeliveryUnknown
			}
			r.Dispatch = pvDispatch(d)
			if outcome == operation.OutcomeAcknowledgedUnverified || outcome == operation.OutcomeApplied {
				r.Acknowledgement = pvAck(operation.AckAccepted)
			}
			if outcome == operation.OutcomeNoEffect {
				r.Acknowledgement = pvAck(operation.AckRejected)
			}
			if outcome == operation.OutcomeApplied || outcome == operation.OutcomeConflict {
				value := f.intent.ExpectedEffect.Expected
				if outcome == operation.OutcomeConflict {
					value = alternate(value)
				}
				snap := updatePVReadback(t, f, value)
				rel := operation.ReadbackConfirms
				if outcome == operation.OutcomeConflict {
					rel = operation.ReadbackContradicts
				}
				r.Readback = pvReadback(snap, rel)
				stored, e := f.kernel.Record(a, r, &snap)
				if e != nil || stored.Outcome != outcome {
					t.Fatal(e, stored)
				}
			} else {
				stored, e := f.kernel.Record(a, r, nil)
				if e != nil || stored.Outcome != outcome {
					t.Fatal(e, stored)
				}
				if outcome == operation.OutcomeIndeterminate {
					require(t, operation.ValidateRetry(stored, true), semreg.RetryForbidden)
				}
			}
		})
	}
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

func TestPVOperationRejectionMatrix(t *testing.T) {
	v := New().(operation.OperationPackValidator)
	for _, tc := range []struct {
		name   string
		mutate func(*operation.Intent)
		want   semreg.ErrorID
	}{
		{"wrong kind", func(i *operation.Intent) { i.Kind.ID = "pv.operation.other" }, semreg.DefinitionOwnerMissing},
		{"wrong pack", func(i *operation.Intent) { i.Kind.Pack.ID = "helianthus.pack.thermal" }, semreg.InvalidValue},
		{"wrong version", func(i *operation.Intent) { i.Kind.Version = "1.0.1" }, semreg.DefinitionOwnerMissing},
		{"argument count", func(i *operation.Intent) { i.Arguments = []semreg.TypedField{} }, semreg.InvalidValue},
		{"argument field", func(i *operation.Intent) { i.Arguments[0].ID = "pv.limit.actual_current" }, semreg.InvalidValue},
		{"argument value", func(i *operation.Intent) {
			i.Arguments[0].Value.Quantity.Number = above(must("pv.limit.active_power").maximum.decimal())
		}, semreg.BoundsExceeded},
		{"effect rule", func(i *operation.Intent) { i.ExpectedEffect.Rule.ID = "pv.effect.other" }, semreg.InvalidValue},
		{"effect fact", func(i *operation.Intent) { i.ExpectedEffect.Fact.FactID = "pv.limit.actual_current" }, semreg.InvalidValue},
		{"effect operator", func(i *operation.Intent) { i.ExpectedEffect.Operator = semreg.PredicateNotEqual }, semreg.InvalidValue},
		{"effect value", func(i *operation.Intent) {
			i.ExpectedEffect.Expected.Quantity.Number = semreg.Decimal{Coefficient: "1"}
		}, semreg.InvalidValue},
		{"required capability", func(i *operation.Intent) { i.RequiredCapability.DefinitionID = "pv.capability.read.connector" }, semreg.InvalidValue},
		{"unsupported operation", func(i *operation.Intent) { i.Kind = definition("pv.operation.unsupported") }, semreg.DefinitionOwnerMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := intent()
			tc.mutate(&candidate)
			require(t, v.ValidateIntent(candidate), tc.want)
		})
	}
}

func TestPVTerminalOutcomesAndConstraintGate(t *testing.T) {
	t.Run("matching published constraint admits", func(t *testing.T) {
		fixture := newPVFixture(t, nil)
		calls := 0
		_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { calls++; return nil }))
		if err != nil || calls != 1 {
			t.Fatalf("matching admit %v, authority calls %d", err, calls)
		}
	})
	t.Run("non-admitting published constraint rejects before authority", func(t *testing.T) {
		fixture := newPVFixture(t, func(capability *semreg.CapabilityInstance) {
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
		{"acknowledged_unverified", operation.OutcomeAcknowledgedUnverified, operation.DeliverySent, pvAck(operation.AckAccepted), ""},
		{"applied", operation.OutcomeApplied, operation.DeliverySent, pvAck(operation.AckAccepted), operation.ReadbackConfirms},
		{"no_effect", operation.OutcomeNoEffect, operation.DeliverySent, pvAck(operation.AckRejected), ""},
		{"conflict", operation.OutcomeConflict, operation.DeliverySent, nil, operation.ReadbackContradicts},
		{"indeterminate", operation.OutcomeIndeterminate, operation.DeliveryUnknown, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPVFixture(t, nil)
			if tc.outcome == operation.OutcomeRejected {
				record, err := fixture.kernel.RecordRejection(fixture.intent, semreg.AuthorityMissing, []semreg.EvidenceRef{})
				if err != nil || record.Outcome != tc.outcome {
					t.Fatalf("rejection: %+v %v", record, err)
				}
				return
			}
			admission := admitPV(t, fixture)
			record := pvRecord(admission)
			record.Outcome, record.Dispatch, record.Acknowledgement = tc.outcome, pvDispatch(tc.delivery), tc.acknowledgement
			var later *semreg.Snapshot
			if tc.relation != "" {
				value := fixture.intent.ExpectedEffect.Expected
				if tc.relation == operation.ReadbackContradicts {
					value = alternate(value)
				}
				snapshot := updatePVReadback(t, fixture, value)
				before, _ := semreg.CanonicalJSON(snapshot)
				fixture.hook.mutateReadback = true
				record.Readback, later = pvReadback(snapshot, tc.relation), &snapshot
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
				rewrite.Outcome, rewrite.Dispatch = operation.OutcomeFailedNoContact, pvDispatch(operation.DeliveryNotSent)
				_, err := fixture.kernel.Record(admission, rewrite, nil)
				require(t, err, semreg.SequenceConflict)
			}
		})
	}
}

type pvRecordHook struct {
	validator
	readbackCalls  int
	mutateReadback bool
}

func (h *pvRecordHook) EvaluateReadback(intent operation.Intent, candidate semreg.FactCandidate) (operation.ReadbackRelation, error) {
	h.readbackCalls++
	relation, err := h.validator.EvaluateReadback(intent, candidate)
	if h.mutateReadback && candidate.Value != nil {
		candidate.Value.Quantity = nil
	}
	return relation, err
}

type pvFixture struct {
	hook        *pvRecordHook
	publication *semreg.PublicationKernel
	kernel      *operation.Kernel
	snapshot    semreg.Snapshot
	intent      operation.Intent
	current     semreg.EvaluationContext
}

func newPVFixture(t *testing.T, mutate func(*semreg.CapabilityInstance)) pvFixture {
	return newPVFixtureFor(t, "pv.operation.set_active_power_limit", mutate)
}
func newPVFixtureFor(t *testing.T, operationID semreg.DefinitionID, mutate func(*semreg.CapabilityInstance)) pvFixture {
	t.Helper()
	hook := &pvRecordHook{}
	publication, err := semreg.NewPublicationKernel("asset:1", hook)
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := operation.NewKernel(hook)
	if err != nil {
		t.Fatal(err)
	}
	requested := intentFor(operationID)
	requested.Causal.FirstSeenAt, requested.Causal.ExpiresAt, requested.Deadline = pvTime("100"), pvTime("400"), pvTime("300")
	service := semreg.ServiceInstance{InstanceID: "service:1", AssetID: "asset:1", Definition: definition(map[bool]semreg.DefinitionID{true: "pv.service.system", false: "pv.service.inverter"}[operationID == "pv.operation.set_export_limit"]), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}
	_, capabilityID, _, _, _ := pvOperationShape(operationID)
	capability := testCapability(capabilityID)
	if mutate != nil {
		mutate(&capability)
	}
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:pv:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Sequence: "1", ExpectedSemanticRevision: "0", ObservedAt: pvTime("100"), SourceUpserts: []semreg.SourceDescriptor{{SourceID: "source:1", SourceEpochID: "epoch:1", ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1", RegistryEvidence: testEvidence(), StartedAt: pvTime("90"), State: semreg.SourceCurrent, Revision: "1"}}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{{BindingID: "binding:1", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", NativeResource: testEvidence(), State: semreg.BindingCurrent, Revision: "1"}}, IdentityLinkUpserts: []semreg.IdentityLink{{AssetID: "asset:1", BindingID: "binding:1", State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}}, FactUpserts: []semreg.FactCandidate{pvFact(requested.ExpectedEffect.Fact, requested.ExpectedEffect.Expected, "100", "100", "1")}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{service}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{capability}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "100"})
	if err != nil {
		t.Fatal(err)
	}
	return pvFixture{hook: hook, publication: publication, kernel: kernel, snapshot: snapshot, intent: requested, current: semreg.EvaluationContext{EvaluatedAt: pvTime("150"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "150"}}}
}
func pvTime(ns string) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(ns), ClockID: "clock.utc", UncertaintyNS: "0"}
}
func pvFact(key semreg.FactKey, value semreg.Value, wall, ticks, revision string) semreg.FactCandidate {
	candidate := candidate(key, value)
	candidate.CandidateID, candidate.Revision = "candidate:pv", semreg.Uint64(revision)
	candidate.Times = semreg.Times{ReceivedAt: pvTime(wall), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: semreg.Uint64(ticks)}, EvaluatedAt: pvTime(wall), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: semreg.Uint64(ticks)}}
	candidate.FreshnessPolicy = semreg.FreshnessPolicy{PolicyID: "policy:1", Version: "1.0.0", FreshForNS: "1000", RetainForNS: "2000", MaxWallUncertaintyNS: "0"}
	return candidate
}
func admitPV(t *testing.T, fixture pvFixture) *operation.Admission {
	t.Helper()
	admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	return admission
}
func pvRecord(admission *operation.Admission) operation.ExecutionRecord {
	intent := admission.Intent()
	route, _ := admission.Route()
	at, revisions, _ := admission.AdmittedAt()
	return operation.ExecutionRecord{Contract: operation.ContractOperationV1, Intent: intent, AdmittedAt: &at, AdmittedRevision: &revisions, Route: &route, OutcomeEvidence: []semreg.EvidenceRef{testEvidence()}}
}
func pvDispatch(delivery operation.DeliveryState) *operation.DispatchEvidence {
	completed := semreg.EvaluationContext{EvaluatedAt: pvTime("200"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "200"}}
	return &operation.DispatchEvidence{AttemptID: "attempt:1", Started: semreg.EvaluationContext{EvaluatedAt: pvTime("180"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "180"}}, Completed: &completed, Delivery: delivery, PossibleSideEffect: delivery != operation.DeliveryNotSent, Evidence: []semreg.EvidenceRef{testEvidence()}}
}
func pvAck(state operation.AckState) *operation.Acknowledgement {
	return &operation.Acknowledgement{State: state, At: pvTime("210"), Evidence: []semreg.EvidenceRef{testEvidence()}}
}
func updatePVReadback(t *testing.T, fixture pvFixture, value semreg.Value) semreg.Snapshot {
	t.Helper()
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:pv:2", AssetID: "asset:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Sequence: "2", ExpectedSemanticRevision: fixture.snapshot.Revisions.Semantic, ObservedAt: pvTime("220"), SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{}, IdentityLinkUpserts: []semreg.IdentityLink{}, FactUpserts: []semreg.FactCandidate{pvFact(fixture.intent.ExpectedEffect.Fact, value, "220", "220", "2")}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
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
func pvReadback(snapshot semreg.Snapshot, relation operation.ReadbackRelation) *operation.Readback {
	return &operation.Readback{SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions, CandidateID: "candidate:pv", CandidateRevision: "2", BindingID: "binding:1", SourceID: "source:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Relation: relation, Evaluation: semreg.EvaluationContext{EvaluatedAt: pvTime("240"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:1", Nanoseconds: "240"}}, Evidence: []semreg.EvidenceRef{testEvidence()}}
}
func alternate(value semreg.Value) semreg.Value {
	result := value
	result.Quantity = &semreg.Quantity{Number: semreg.Decimal{Coefficient: "1"}, Unit: value.Quantity.Unit}
	if reflect.DeepEqual(result, value) {
		result.Quantity.Number = semreg.Decimal{Coefficient: "2"}
	}
	return result
}

func must(id semreg.DefinitionID) fieldSpec {
	s, ok := findField(id)
	if !ok {
		panic(id)
	}
	return s
}
func testKey(s fieldSpec) semreg.FactKey {
	n := "id"
	return semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: s.id, Dimensions: []semreg.Dimension{{ID: s.dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &n}}}}
}
func testValue(s fieldSpec) semreg.Value {
	if s.kind == semreg.ValueSymbol {
		return semreg.Value{Kind: s.kind, Symbol: &semreg.Symbol{Namespace: s.id, Token: strings.TrimPrefix(s.symbols[0], string(s.id)+"."), Known: true}}
	}
	n := semreg.Decimal{Coefficient: "0"}
	if s.minimum != nil {
		n = s.minimum.decimal()
	}
	return semreg.Value{Kind: s.kind, Quantity: &semreg.Quantity{Number: n, Unit: s.unit}}
}
func testCapability(id semreg.DefinitionID) semreg.CapabilityInstance {
	cs := []semreg.TypedField{}
	for _, f := range capabilities[id].constraints {
		cs = append(cs, semreg.TypedField{ID: f, Value: testValue(must(f))})
	}
	return semreg.CapabilityInstance{InstanceID: "cap:1", AssetID: "asset:1", ServiceInstance: "service:1", Definition: definition(id), BindingID: "binding:1", SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Constraints: cs, ActivationEvidence: []semreg.EvidenceRef{testEvidence()}, Revision: "1"}
}
func testEvidence() semreg.EvidenceRef {
	return semreg.EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Contract: "test.evidence/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone}
}
func above(v semreg.Decimal) semreg.Decimal {
	return semreg.Decimal{Coefficient: "999999999", Exponent10: v.Exponent10}
}
func require(t *testing.T, e error, w semreg.ErrorID) {
	t.Helper()
	if semreg.ErrorIdentifier(e) != w {
		t.Fatalf("%v want %s", e, w)
	}
}
