package operation_test

import (
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// actionablePack refuses a non-actionable capability in its capability hook
// and checks the capability before its constraints, as every shipped pack
// does. The plain test pack accepts any capability, so it cannot show a
// snapshot-wide hook refusing the retired generation's withdrawn tombstone.
type actionablePack struct{ *testOperationPack }

func (actionablePack) ValidateCapability(capability semreg.CapabilityInstance) error {
	if capability.Qualification != semreg.QualificationQualified {
		return &semreg.Error{ID: semreg.CapabilityNotQualified, Detail: "qualified current binding"}
	}
	if capability.Availability != semreg.AvailabilityAvailable {
		return &semreg.Error{ID: semreg.CapabilityUnavailable, Detail: "current binding"}
	}
	return nil
}

func (p actionablePack) MatchConstraints(capability semreg.CapabilityInstance, fields []semreg.TypedField) error {
	if err := p.ValidateCapability(capability); err != nil {
		return err
	}
	return p.testOperationPack.MatchConstraints(capability, fields)
}

const (
	retiredCapability   semreg.CapabilityInstanceID = "capability:test"
	successorBinding    semreg.NativeBindingID      = "binding:test:successor"
	successorService    semreg.ServiceInstanceID    = "service:test:successor"
	successorCapability semreg.CapabilityInstanceID = "capability:test:successor"
)

// restartShape is one lifecycle transition a driver restart publishes. Each
// shape changes exactly one lifecycle axis of the route, so together they
// vary both: a replaced driver keeps its source epoch and fences the prior
// generation; a restarted source retires its prior epoch and starts a new one
// at the same generation number. The source ID is held fixed because a
// restart preserves source identity; a different source is not a successor.
type restartShape struct {
	name       string
	epoch      semreg.SourceEpochID
	generation semreg.Uint64
	tombstone  semreg.BindingState
	stale      semreg.ErrorID
}

var restartShapes = []restartShape{
	{"successor generation", "epoch:test:1", "2", semreg.BindingFenced, semreg.StaleDriverGeneration},
	{"successor source epoch", "epoch:test:2", "1", semreg.BindingRetired, semreg.StaleSourceEpoch},
}

func applyBatch(t *testing.T, publication *semreg.PublicationKernel, batch semreg.PublicationBatch, ticks string) semreg.Snapshot {
	t.Helper()
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: semreg.Uint64(ticks)})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func successorFact(id semreg.CandidateID, key semreg.FactKey, value bool, shape restartShape, wall, revision string) semreg.FactCandidate {
	candidate := factCandidate(id, key, value, wall, wall, semreg.Uint64(revision))
	binding, epoch, generation := successorBinding, shape.epoch, shape.generation
	candidate.BindingID, candidate.SourceEpochID, candidate.DriverGeneration = &binding, &epoch, &generation
	candidate.Origin.BindingID, candidate.Origin.SourceEpochID = &binding, &epoch
	return candidate
}

func successorBatch(shape restartShape, id semreg.BatchID, sequence semreg.Uint64, expected semreg.Uint64, wall string) semreg.PublicationBatch {
	return semreg.PublicationBatch{
		Contract: semreg.ContractKernelV1, BatchID: id, AssetID: "asset:test", SourceID: "source:test", SourceEpochID: shape.epoch,
		DriverGeneration: shape.generation, Sequence: sequence, ExpectedSemanticRevision: expected, ObservedAt: timePoint(wall),
		SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{},
		IdentityLinkUpserts: []semreg.IdentityLink{}, FactUpserts: []semreg.FactCandidate{}, FactWithdrawals: []semreg.CandidateID{},
		ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{},
		CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{},
	}
}

// applyRestart publishes the successor's first batch: its lifecycle boundary,
// binding, identity link, service, capability and the interlock fact it
// observes. The retired binding's link, service and capability stay in the
// snapshot as withdrawn tombstones.
func applyRestart(t *testing.T, fixture operationFixture, shape restartShape) semreg.Snapshot {
	t.Helper()
	batch := successorBatch(shape, "batch:test:restart", "1", fixture.snapshot.Revisions.Semantic, "120")
	if shape.epoch == "epoch:test:1" {
		batch.GenerationFences = []semreg.GenerationFence{{SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1", Reason: "lifecycle.driver_replaced", Evidence: []semreg.EvidenceRef{evidence(9)}, Revision: "1"}}
	} else {
		batch.SourceUpserts = []semreg.SourceDescriptor{{SourceID: "source:test", SourceEpochID: shape.epoch, ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1", RegistryEvidence: evidence(1), StartedAt: timePoint("110"), State: semreg.SourceCurrent, Revision: "1"}}
		batch.SourceRetirements = []semreg.SourceEpochID{"epoch:test:1"}
	}
	batch.BindingUpserts = []semreg.NativeBinding{{BindingID: successorBinding, AssetID: "asset:test", SourceID: "source:test", SourceEpochID: shape.epoch, DriverGeneration: shape.generation, NativeResource: evidence(10), State: semreg.BindingCurrent, Revision: "1"}}
	batch.IdentityLinkUpserts = []semreg.IdentityLink{{AssetID: "asset:test", BindingID: successorBinding, State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{evidence(11)}, Revision: "1"}}
	batch.FactUpserts = []semreg.FactCandidate{successorFact("candidate:interlock", preKey, true, shape, "120", "2")}
	batch.ServiceUpserts = []semreg.ServiceInstance{{InstanceID: successorService, AssetID: "asset:test", Definition: testService, BindingID: successorBinding, SourceEpochID: shape.epoch, DriverGeneration: shape.generation, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}}
	batch.CapabilityUpserts = []semreg.CapabilityInstance{{InstanceID: successorCapability, AssetID: "asset:test", ServiceInstance: successorService, Definition: testCap, BindingID: successorBinding, SourceEpochID: shape.epoch, DriverGeneration: shape.generation, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Constraints: []semreg.TypedField{}, ActivationEvidence: []semreg.EvidenceRef{evidence(12)}, Revision: "1"}}
	return applyBatch(t, fixture.publication, batch, "120")
}

// requireRetiredTombstone proves the restart left the retired route beside the
// successor as a non-actionable tombstone, which is the snapshot shape every
// post-restart admission receives.
func requireRetiredTombstone(t *testing.T, snapshot semreg.Snapshot, retired operation.Route, state semreg.BindingState) {
	t.Helper()
	var binding, capability bool
	for _, candidate := range snapshot.Bindings {
		binding = binding || candidate.BindingID == retired.BindingID && candidate.State == state
	}
	for _, candidate := range snapshot.Capabilities {
		capability = capability || candidate.InstanceID == retired.CapabilityInstance && candidate.Availability == semreg.AvailabilityWithdrawn
	}
	if !binding || !capability || len(snapshot.Capabilities) != 2 {
		t.Fatalf("restart did not retain the retired route as a %s tombstone: %+v %+v", state, snapshot.Bindings, snapshot.Capabilities)
	}
}

func successorIntent(snapshot semreg.Snapshot, shape restartShape, key string) operation.Intent {
	intent := validIntent()
	intent.IntentID = semreg.IntentID("intent:test:" + key)
	intent.IdempotencyKey = semreg.IdempotencyKey("idempotency:test:" + key)
	intent.Causal.CorrelationID = semreg.CorrelationID("correlation:test:" + key)
	intent.ExpectedSemanticRevision = snapshot.Revisions.Semantic
	intent.ExpectedCapabilityRevision = snapshot.Revisions.Capabilities
	intent.ExpectedSourceEpochID = shape.epoch
	intent.ExpectedDriverGeneration = shape.generation
	intent.Preconditions[0].CandidateRevision = "2"
	return intent
}

func successorReadback(snapshot semreg.Snapshot, shape restartShape) *operation.Readback {
	return &operation.Readback{
		SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions, CandidateID: "candidate:level", CandidateRevision: "1",
		BindingID: successorBinding, SourceID: "source:test", SourceEpochID: shape.epoch, DriverGeneration: shape.generation,
		Relation: operation.ReadbackConfirms, Evaluation: context("240", "240"), Evidence: []semreg.EvidenceRef{evidence(9)},
	}
}

func appliedRecord(admission *operation.Admission, readback *operation.Readback) operation.ExecutionRecord {
	record := admittedRecord(admission)
	record.Outcome, record.Dispatch = operation.OutcomeApplied, dispatch(operation.DeliverySent, true)
	record.Acknowledgement = &operation.Acknowledgement{State: operation.AckAccepted, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}
	record.Readback = readback
	return record
}

// After a driver restart the snapshot holds the retired route's withdrawn
// tombstone beside the successor's current route. Admission and readback
// validation must check only the required route for actionability: the
// successor admits and records confirming evidence, while every operation or
// late completion naming the retired route stays refused.
//
// Identity fields that differ between the retired and successor routes, and
// the production mutation each assertion catches:
//   - source epoch (successor source epoch shape): without the route's epoch
//     comparison an intent naming the retired epoch routes to the successor.
//   - driver generation (successor generation shape): without the route's
//     generation comparison an intent naming the retired generation routes to
//     the successor.
//   - binding, service and capability instance (both shapes): the exact route
//     and guard-claim comparison fails if admission returns any retired
//     reference; ignoring an explicit instance pin admits a successor intent
//     pinned to the retired capability.
//   - capability actionability: validating every snapshot capability's
//     availability through the pack hook (the defect) refuses the successor
//     admission with capability_unavailable, and doing so in readback
//     validation alone refuses its confirming readback; without the route's
//     own availability check a withdrawn successor capability fails as
//     ambiguous_route instead of capability_unavailable.
//
// The late retired completion is pinned to its stale lifecycle error only:
// readback's route comparison and its current-route recheck are redundant
// layers, so removing either one alone keeps that completion refused.
func TestAdmissionAfterRestartRoutesOnlyTheSuccessorGeneration(t *testing.T) {
	for _, shape := range restartShapes {
		t.Run(shape.name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			kernel, err := operation.NewKernel(actionablePack{fixture.pack})
			if err != nil {
				t.Fatal(err)
			}
			fixture.kernel = kernel
			authorityCalls := 0
			authority := operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error {
				authorityCalls++
				return nil
			})

			// The retired generation admits while it is current.
			retired, err := kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, authority)
			if err != nil {
				t.Fatalf("retired generation before restart: %v", err)
			}
			retiredRoute, _ := retired.Route()
			restarted := applyRestart(t, fixture, shape)
			requireRetiredTombstone(t, restarted, retiredRoute, shape.tombstone)

			// The successor admits through its own route only.
			want := operation.Route{CapabilityInstance: successorCapability, ServiceInstance: successorService, BindingID: successorBinding, SourceID: "source:test", SourceEpochID: shape.epoch, DriverGeneration: shape.generation}
			pinned := successorCapability
			for _, test := range []struct {
				name string
				pin  *semreg.CapabilityInstanceID
			}{{"successor", nil}, {"successor-pinned", &pinned}} {
				intent := successorIntent(restarted, shape, test.name)
				intent.RequiredCapability.InstanceID = test.pin
				admission, err := kernel.Admit(restarted, fixture.current, intent, authority)
				if err != nil {
					t.Fatalf("%s admission after restart: %v", test.name, err)
				}
				route, routed := admission.Route()
				claims, claimed := admission.GuardClaims()
				wantClaims := operation.GuardClaims{
					AssetID: "asset:test", ExpectedCapabilityRevision: restarted.Revisions.Capabilities, CapabilityInstance: want.CapabilityInstance,
					ExpectedCapabilityInstanceRevision: "1", ServiceInstance: want.ServiceInstance, BindingID: want.BindingID,
					SourceID: want.SourceID, SourceEpochID: want.SourceEpochID, DriverGeneration: want.DriverGeneration,
				}
				if !routed || !claimed || !reflect.DeepEqual(route, want) || !reflect.DeepEqual(claims, wantClaims) {
					t.Fatalf("%s route %+v claims %+v; want %+v %+v", test.name, route, claims, want, wantClaims)
				}
			}
			admittedCalls := authorityCalls

			// Every intent naming the retired route stays refused before
			// authority resolution.
			retiredGeneration := successorIntent(restarted, shape, "retired-generation")
			retiredGeneration.ExpectedSourceEpochID, retiredGeneration.ExpectedDriverGeneration = "epoch:test:1", "1"
			instance := retiredCapability
			retiredInstance := successorIntent(restarted, shape, "retired-instance")
			retiredInstance.RequiredCapability.InstanceID = &instance
			for _, refused := range []struct {
				name   string
				intent operation.Intent
			}{{"retired generation", retiredGeneration}, {"retired instance", retiredInstance}} {
				_, err := kernel.Admit(restarted, fixture.current, refused.intent, authority)
				if semreg.ErrorIdentifier(err) != shape.stale {
					t.Fatalf("%s after restart: %v; want %s", refused.name, err, shape.stale)
				}
			}
			if authorityCalls != admittedCalls {
				t.Fatalf("a retired-route intent reached authority resolution: %d calls", authorityCalls-admittedCalls)
			}

			// The successor's confirming readback records although the later
			// snapshot still holds the retired tombstone; a late completion of
			// the retired admission reading back the successor is refused.
			readbackBatch := successorBatch(shape, "batch:test:successor-readback", "2", restarted.Revisions.Semantic, "220")
			readbackBatch.FactUpserts = []semreg.FactCandidate{successorFact("candidate:level", readbackKey, true, shape, "220", "1")}
			later := applyBatch(t, fixture.publication, readbackBatch, "220")
			successor, err := kernel.Admit(restarted, fixture.current, successorIntent(restarted, shape, "successor"), authority)
			if err != nil {
				t.Fatal(err)
			}
			if stored, err := kernel.Record(successor, appliedRecord(successor, successorReadback(later, shape)), &later); err != nil || stored.Outcome != operation.OutcomeApplied {
				t.Fatalf("successor readback after restart: %+v %v", stored, err)
			}
			if _, err := kernel.Record(retired, appliedRecord(retired, successorReadback(later, shape)), &later); semreg.ErrorIdentifier(err) != shape.stale {
				t.Fatalf("late retired completion: %v; want %s", err, shape.stale)
			}

			// The successor's own withdrawn capability stays refused: only
			// snapshot-wide validation ignores actionability.
			withdrawal := successorBatch(shape, "batch:test:successor-withdrawal", "3", later.Revisions.Semantic, "230")
			withdrawal.CapabilityWithdrawals = []semreg.CapabilityInstanceID{successorCapability}
			withdrawn := applyBatch(t, fixture.publication, withdrawal, "230")
			intent := successorIntent(withdrawn, shape, "successor-withdrawn")
			intent.RequiredCapability.InstanceID = &pinned
			intent.ExpectedCapabilityInstanceRevision = "2"
			_, err = kernel.Admit(withdrawn, context("250", "250"), intent, authority)
			errorID(t, err, semreg.CapabilityUnavailable)
		})
	}
}
