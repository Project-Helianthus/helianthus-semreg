package operation_test

import (
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestAdmissionRejectsRetainedObservationAsCurrentPrecondition(t *testing.T) {
	fixture := newOperationFixture(t)
	transition := semreg.PublicationBatch{
		Contract:                 semreg.ContractKernelV1,
		BatchID:                  "batch:test:retire-precondition",
		AssetID:                  "asset:test",
		SourceID:                 "source:test",
		SourceEpochID:            "epoch:test:1",
		DriverGeneration:         "2",
		Sequence:                 "1",
		ExpectedSemanticRevision: "1",
		ObservedAt:               timePoint("150"),
		SourceUpserts:            []semreg.SourceDescriptor{},
		SourceRetirements:        []semreg.SourceEpochID{},
		BindingUpserts: []semreg.NativeBinding{{
			BindingID: "binding:test:new", AssetID: "asset:test", SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "2", NativeResource: evidence(2), State: semreg.BindingCurrent, Revision: "1",
		}},
		IdentityLinkUpserts: []semreg.IdentityLink{{
			AssetID: "asset:test", BindingID: "binding:test:new", State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{evidence(3)}, Revision: "1",
		}},
		FactUpserts:     []semreg.FactCandidate{},
		FactWithdrawals: []semreg.CandidateID{},
		ServiceUpserts: []semreg.ServiceInstance{{
			InstanceID: "service:test:new", AssetID: "asset:test", Definition: testService, BindingID: "binding:test:new", SourceEpochID: "epoch:test:1", DriverGeneration: "2", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1",
		}},
		ServiceWithdrawals: []semreg.ServiceInstanceID{},
		CapabilityUpserts: []semreg.CapabilityInstance{{
			InstanceID: "capability:test:new", AssetID: "asset:test", ServiceInstance: "service:test:new", Definition: testCap, BindingID: "binding:test:new", SourceEpochID: "epoch:test:1", DriverGeneration: "2", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Constraints: []semreg.TypedField{}, ActivationEvidence: []semreg.EvidenceRef{evidence(4)}, Revision: "1",
		}},
		CapabilityWithdrawals: []semreg.CapabilityInstanceID{},
		GenerationFences: []semreg.GenerationFence{{
			SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1",
			Reason: "lifecycle.driver_replaced", Evidence: []semreg.EvidenceRef{evidence(9)}, Revision: "1",
		}},
	}
	digest, err := transition.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	transition.BatchDigest = digest
	snapshot, _, err := fixture.publication.Apply(transition, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: "150"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Retained) != 1 || snapshot.Retained[0].Candidate.CandidateID != "candidate:interlock" {
		t.Fatalf("missing retained precondition witness: %+v", snapshot.Retained)
	}
	fixture.snapshot = snapshot
	fixture.intent.ExpectedSemanticRevision = snapshot.Revisions.Semantic
	fixture.intent.ExpectedCapabilityRevision = snapshot.Revisions.Capabilities
	fixture.intent.ExpectedDriverGeneration = "2"
	_, err = fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
	errorID(t, err, semreg.PreconditionFailed)
}
