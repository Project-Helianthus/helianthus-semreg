package semreg

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// Contract-derived regression at docs-semantic@b16667d719defc7b0fef0400ee3ad387469018ac:
// immutable retained records make cross-source ownership knowable before a
// malformed batch can enter partial-record staging.
func TestPublicationRetainedSourceOwnershipPrecedence(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(PublicationBatch, *PublicationBatch)
	}{
		{"identity-link-upsert", func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.IdentityLinkUpserts[0]
			value.Basis, value.Revision = []EvidenceRef{publicEvidence("6")}, "2"
			next.IdentityLinkUpserts = []IdentityLink{value}
		}},
		{"candidate-upsert", func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.FactUpserts[0]
			value.Revision = "2"
			value.Origin.SourceID = retainedPtr(SourceID(next.SourceID))
			value.Origin.SourceEpochID = retainedPtr(SourceEpochID(next.SourceEpochID))
			value.SourceEpochID = retainedPtr(SourceEpochID(next.SourceEpochID))
			value.DriverGeneration = retainedPtr(Uint64(next.DriverGeneration))
			next.FactUpserts = []FactCandidate{value}
		}},
		{"service-upsert", func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.ServiceUpserts[0]
			value.Revision = "2"
			value.SourceEpochID = next.SourceEpochID
			value.DriverGeneration = next.DriverGeneration
			next.ServiceUpserts = []ServiceInstance{value}
		}},
		{"capability-upsert", func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.CapabilityUpserts[0]
			value.Revision = "2"
			value.SourceEpochID = next.SourceEpochID
			value.DriverGeneration = next.DriverGeneration
			next.CapabilityUpserts = []CapabilityInstance{value}
		}},
		{"fact-withdrawal", func(initial PublicationBatch, next *PublicationBatch) {
			next.FactWithdrawals = []CandidateID{initial.FactUpserts[0].CandidateID}
		}},
		{"service-withdrawal", func(initial PublicationBatch, next *PublicationBatch) {
			next.ServiceWithdrawals = []ServiceInstanceID{initial.ServiceUpserts[0].InstanceID}
		}},
		{"capability-withdrawal", func(initial PublicationBatch, next *PublicationBatch) {
			next.CapabilityWithdrawals = []CapabilityInstanceID{initial.CapabilityUpserts[0].InstanceID}
		}},
	}
	for _, tc := range cases {
		for _, fault := range []string{"ownership-only", "malformed-time", "malformed-enum"} {
			t.Run(tc.name+"/"+fault, func(t *testing.T) {
				kernel, initial, historical, historicalRaw, hooks := invariantInitialWithHooks(t)
				next := publicationBatch(initial.AssetID, "source:b", "epoch:b", "1", "1", "1")
				next.SourceUpserts = []SourceDescriptor{publicationSource(next.SourceID, next.SourceEpochID)}
				tc.mutate(initial, &next)
				switch fault {
				case "malformed-time":
					next.ObservedAt.UnixNanoseconds = "bad"
				case "malformed-enum":
					next.SourceUpserts[0].State = "bad"
				}
				sealRetainedUnchecked(t, &next)
				before, err := json.Marshal(next)
				if err != nil {
					t.Fatal(err)
				}
				assertRejectedUnchanged(t, kernel, next, InvalidValue)
				after, err := json.Marshal(next)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("caller batch mutated: %v", err)
				}
				if hooks.calls() != 0 || len(hooks.retained) != 0 {
					t.Fatalf("retained ownership preflight reached hooks: calls=%d retained=%d", hooks.calls(), len(hooks.retained))
				}
				assertHistoricalBytes(t, historical, historicalRaw)
				replay, replayRaw, err := kernel.Apply(initial, publicationMonotonic)
				if err != nil || !reflect.DeepEqual(replay, historical) || !bytes.Equal(replayRaw, historicalRaw) {
					t.Fatalf("historical replay changed: %v", err)
				}
			})
		}
	}
}

func TestPublicationRetainedSourceOwnershipHigherRankedControls(t *testing.T) {
	controls := []struct {
		name   string
		want   ErrorID
		mutate func(PublicationBatch, *PublicationBatch)
	}{
		{"missing-optional-binding", MissingMember, func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.FactUpserts[0]
			value.Revision = "2"
			value.BindingID = nil
			next.FactUpserts = []FactCandidate{value}
		}},
		{"invalid-optional-binding", InvalidIdentifier, func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.FactUpserts[0]
			value.Revision = "2"
			value.BindingID = retainedPtr(NativeBindingID("!"))
			next.FactUpserts = []FactCandidate{value}
		}},
		{"duplicate-withdrawal", DuplicateKey, func(initial PublicationBatch, next *PublicationBatch) {
			id := initial.FactUpserts[0].CandidateID
			next.FactWithdrawals = []CandidateID{id, id}
		}},
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			kernel, initial, _, _, hooks := invariantInitialWithHooks(t)
			next := publicationBatch(initial.AssetID, "source:b", "epoch:b", "1", "1", "1")
			next.SourceUpserts = []SourceDescriptor{publicationSource(next.SourceID, next.SourceEpochID)}
			control.mutate(initial, &next)
			next.ObservedAt.UnixNanoseconds = "bad"
			sealRetainedUnchecked(t, &next)
			assertRejectedUnchanged(t, kernel, next, control.want)
			if hooks.calls() != 0 || len(hooks.retained) != 0 {
				t.Fatal("malformed partial record reached hooks")
			}
		})
	}
}

func TestPublicationRetainedSourceOwnershipUnknownReferenceControls(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PublicationBatch)
	}{
		{"identity-link", func(next *PublicationBatch) {
			next.IdentityLinkUpserts = []IdentityLink{publicationLink(next.AssetID, "binding:missing")}
		}},
		{"candidate", func(next *PublicationBatch) {
			next.FactUpserts = []FactCandidate{publicationCandidate("candidate:missing", "fact.power", true, next.SourceID, next.SourceEpochID, "binding:missing", next.DriverGeneration)}
		}},
		{"service", func(next *PublicationBatch) {
			next.ServiceUpserts = []ServiceInstance{publicationService(next.AssetID, "service:missing", "binding:missing", next.SourceEpochID, next.DriverGeneration)}
		}},
		{"capability", func(next *PublicationBatch) {
			next.CapabilityUpserts = []CapabilityInstance{publicationCapability(next.AssetID, "capability:missing", "service:missing", "binding:missing", next.SourceEpochID, next.DriverGeneration)}
		}},
		{"fact-withdrawal", func(next *PublicationBatch) {
			next.FactWithdrawals = []CandidateID{"candidate:missing"}
		}},
		{"service-withdrawal", func(next *PublicationBatch) {
			next.ServiceWithdrawals = []ServiceInstanceID{"service:missing"}
		}},
		{"capability-withdrawal", func(next *PublicationBatch) {
			next.CapabilityWithdrawals = []CapabilityInstanceID{"capability:missing"}
		}},
		{"source-retirement", func(next *PublicationBatch) {
			next.SourceRetirements = []SourceEpochID{"epoch:missing"}
		}},
		{"generation-fence", func(next *PublicationBatch) {
			next.GenerationFences = []GenerationFence{publicationFence(next.SourceID, next.SourceEpochID, next.DriverGeneration, publicEvidence("6"))}
		}},
	}
	for _, tc := range cases {
		for _, malformedTime := range []bool{false, true} {
			name := tc.name + "/reference-only"
			want := DanglingReference
			if malformedTime {
				name, want = tc.name+"/malformed-time", InvalidTime
			}
			t.Run(name, func(t *testing.T) {
				kernel, initial, _, _ := invariantInitial(t)
				next := publicationBatch(initial.AssetID, "source:b", "epoch:b", "1", "1", "1")
				next.SourceUpserts = []SourceDescriptor{publicationSource(next.SourceID, next.SourceEpochID)}
				tc.mutate(&next)
				if malformedTime {
					next.ObservedAt.UnixNanoseconds = "bad"
				}
				sealRetainedUnchecked(t, &next)
				assertRejectedUnchanged(t, kernel, next, want)
			})
		}
	}
}

func TestPublicationRetainedSourceOwnershipClassificationControls(t *testing.T) {
	t.Run("binding-id-reuse-remains-identity-class", func(t *testing.T) {
		for _, malformedTime := range []bool{false, true} {
			kernel, initial, _, _ := invariantInitial(t)
			next := publicationBatch(initial.AssetID, "source:b", "epoch:b", "1", "1", "1")
			next.SourceUpserts = []SourceDescriptor{publicationSource(next.SourceID, next.SourceEpochID)}
			binding := initial.BindingUpserts[0]
			binding.SourceID, binding.SourceEpochID, binding.Revision = next.SourceID, next.SourceEpochID, "2"
			next.BindingUpserts = []NativeBinding{binding}
			want := IdentityNotQualified
			if malformedTime {
				next.ObservedAt.UnixNanoseconds, want = "bad", InvalidTime
			}
			sealRetainedUnchecked(t, &next)
			assertRejectedUnchanged(t, kernel, next, want)
		}
	})

	t.Run("same-sequence-remains-replay-partition", func(t *testing.T) {
		kernel, initial, _, _ := invariantInitial(t)
		accepted := publicationBatch(initial.AssetID, "source:b", "epoch:b", "1", "1", "1")
		accepted.SourceUpserts = []SourceDescriptor{publicationSource(accepted.SourceID, accepted.SourceEpochID)}
		sealPublicationBatch(t, &accepted)
		if _, _, err := kernel.Apply(accepted, publicationMonotonic); err != nil {
			t.Fatal(err)
		}

		changed := clonePublicationBatch(accepted)
		link := initial.IdentityLinkUpserts[0]
		link.Basis, link.Revision = []EvidenceRef{publicEvidence("6")}, "2"
		changed.IdentityLinkUpserts = []IdentityLink{link}
		sealRetainedUnchecked(t, &changed)
		assertRejectedUnchanged(t, kernel, changed, SequenceConflict)

		changed.ObservedAt.UnixNanoseconds = "bad"
		sealRetainedUnchecked(t, &changed)
		assertRejectedUnchanged(t, kernel, changed, InvalidTime)
	})
}

func TestPublicationRetainedSourceOwnershipSameSourceControls(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(PublicationBatch, *PublicationBatch)
	}{
		{"identity-link-revision", func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.IdentityLinkUpserts[0]
			value.Basis, value.Revision = []EvidenceRef{publicEvidence("6")}, "2"
			next.IdentityLinkUpserts = []IdentityLink{value}
		}},
		{"candidate-revision", func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.FactUpserts[0]
			value.Evidence, value.Revision = []EvidenceRef{publicEvidence("6")}, "2"
			next.FactUpserts = []FactCandidate{value}
		}},
		{"service-revision", func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.ServiceUpserts[0]
			value.Availability, value.Revision = AvailabilityDegraded, "2"
			next.ServiceUpserts = []ServiceInstance{value}
		}},
		{"capability-revision", func(initial PublicationBatch, next *PublicationBatch) {
			value := initial.CapabilityUpserts[0]
			value.Availability, value.Revision = AvailabilityDegraded, "2"
			next.CapabilityUpserts = []CapabilityInstance{value}
		}},
		{"fact-withdrawal", func(initial PublicationBatch, next *PublicationBatch) {
			next.FactWithdrawals = []CandidateID{initial.FactUpserts[0].CandidateID}
		}},
		{"service-withdrawal", func(initial PublicationBatch, next *PublicationBatch) {
			next.ServiceWithdrawals = []ServiceInstanceID{initial.ServiceUpserts[0].InstanceID}
			next.CapabilityWithdrawals = []CapabilityInstanceID{initial.CapabilityUpserts[0].InstanceID}
		}},
		{"capability-withdrawal", func(initial PublicationBatch, next *PublicationBatch) {
			next.CapabilityWithdrawals = []CapabilityInstanceID{initial.CapabilityUpserts[0].InstanceID}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kernel, initial, historical, historicalRaw := invariantInitial(t)
			next := publicationBatch(initial.AssetID, initial.SourceID, initial.SourceEpochID, "1", "2", "1")
			tc.mutate(initial, &next)
			sealPublicationBatch(t, &next)
			if _, _, err := kernel.Apply(next, publicationMonotonic); err != nil {
				t.Fatal(err)
			}
			assertHistoricalBytes(t, historical, historicalRaw)
		})
	}
}

func retainedPtr[T any](value T) *T { return &value }

func sealRetainedUnchecked(t *testing.T, batch *PublicationBatch) {
	t.Helper()
	digest, err := batch.computedDigestUnchecked()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
}
