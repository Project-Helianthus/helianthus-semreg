package semreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"testing"
	"time"
)

// Contract-derived transitions at docs-semantic@b16667d719defc7b0fef0400ee3ad387469018ac:
// kernel source epochs, publication ownership, retained tombstones/cursors and
// serialization's independent-error precedence. Inputs are synthetic/public.
func invariantInitial(t *testing.T) (*PublicationKernel, PublicationBatch, Snapshot, []byte) {
	t.Helper()
	k := newTestPublicationKernel(t, "asset:site")
	b := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	sealPublicationBatch(t, &b)
	s, raw, err := k.Apply(b, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	return k, b, s, raw
}

func assertHistoricalBytes(t *testing.T, s Snapshot, raw []byte) {
	t.Helper()
	encoded, err := CanonicalJSON(s)
	if err != nil || !bytes.Equal(encoded, raw) {
		t.Fatalf("historical snapshot changed: %v", err)
	}
}

func invariantInitialWithHooks(t *testing.T) (*PublicationKernel, PublicationBatch, Snapshot, []byte, *retainingBoundaryValidator) {
	t.Helper()
	hooks := boundaryValidator()
	k, err := NewPublicationKernel("asset:site", hooks)
	if err != nil {
		t.Fatal(err)
	}
	b := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	sealPublicationBatch(t, &b)
	s, raw, err := k.Apply(b, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	hooks.factCalls, hooks.serviceCalls, hooks.capabilityCalls, hooks.fieldCalls = 0, 0, 0, 0
	hooks.matchCalls, hooks.predicateCalls, hooks.retained = 0, 0, nil
	return k, b, s, raw, hooks
}

func TestPublicationHeaderOwnershipErrorRanking(t *testing.T) {
	owners := []struct {
		name   string
		mutate func(PublicationBatch, PublicationBatch) PublicationBatch
	}{
		{"kernel-asset", func(_, next PublicationBatch) PublicationBatch {
			next.AssetID = "asset:other"
			return next
		}},
		{"binding-asset", func(initial, next PublicationBatch) PublicationBatch {
			next.BindingUpserts = []NativeBinding{publicationBinding("asset:other", initial.SourceID, initial.SourceEpochID, "binding:new", "1")}
			return next
		}},
		{"source-id", func(_, next PublicationBatch) PublicationBatch {
			next.SourceUpserts = []SourceDescriptor{publicationSource("source:other", "epoch:other")}
			return next
		}},
		{"generation-fence", func(_, next PublicationBatch) PublicationBatch {
			next.GenerationFences = []GenerationFence{publicationFence("source:other", next.SourceEpochID, next.DriverGeneration, publicEvidence("b"))}
			return next
		}},
		{"identity-asset", func(_, next PublicationBatch) PublicationBatch {
			next.IdentityLinkUpserts = []IdentityLink{publicationLink("asset:other", "binding:a")}
			return next
		}},
		{"observed-candidate", func(initial, next PublicationBatch) PublicationBatch {
			next.FactUpserts = []FactCandidate{publicationCandidate("candidate:new", "fact.power", true, "source:other", initial.SourceEpochID, "binding:a", "1")}
			return next
		}},
		{"service-asset", func(initial, next PublicationBatch) PublicationBatch {
			next.ServiceUpserts = []ServiceInstance{publicationService("asset:other", "service:new", "binding:a", initial.SourceEpochID, "1")}
			return next
		}},
		{"capability-asset", func(initial, next PublicationBatch) PublicationBatch {
			next.CapabilityUpserts = []CapabilityInstance{publicationCapability("asset:other", "capability:new", initial.ServiceUpserts[0].InstanceID, "binding:a", initial.SourceEpochID, "1")}
			return next
		}},
	}
	sealUnchecked := func(t *testing.T, batch *PublicationBatch) {
		t.Helper()
		digest, err := batch.computedDigestUnchecked()
		if err != nil {
			t.Fatal(err)
		}
		batch.BatchDigest = digest
	}
	for _, owner := range owners {
		for _, fault := range []string{"ownership-only", "ownership-and-time", "ownership-and-enum"} {
			t.Run(owner.name+"/"+fault, func(t *testing.T) {
				k, initial, _, _, hooks := invariantInitialWithHooks(t)
				next := publicationBatch(initial.AssetID, initial.SourceID, initial.SourceEpochID, "1", "2", "1")
				next = owner.mutate(initial, next)
				if fault == "ownership-and-time" {
					next.ObservedAt.UnixNanoseconds = "bad"
				}
				if fault == "ownership-and-enum" {
					if len(next.IdentityLinkUpserts) == 0 {
						next.IdentityLinkUpserts = []IdentityLink{initial.IdentityLinkUpserts[0]}
					}
					next.IdentityLinkUpserts[0].State = "bad"
				}
				sealUnchecked(t, &next)
				assertRejectedUnchanged(t, k, next, InvalidValue)
				if hooks.calls() != 0 || len(hooks.retained) != 0 {
					t.Fatalf("preflight rejection reached hooks: calls=%d retained=%d", hooks.calls(), len(hooks.retained))
				}
			})
		}
	}
	for _, fault := range []struct {
		name string
		want ErrorID
	}{
		{"time-only", InvalidTime},
		{"enum-only", InvalidEnum},
	} {
		t.Run(fault.name, func(t *testing.T) {
			k, initial, _, _ := invariantInitial(t)
			next := publicationBatch(initial.AssetID, initial.SourceID, initial.SourceEpochID, "1", "2", "1")
			if fault.want == InvalidTime {
				next.ObservedAt.UnixNanoseconds = "bad"
			} else {
				link := initial.IdentityLinkUpserts[0]
				link.State = "bad"
				next.IdentityLinkUpserts = []IdentityLink{link}
			}
			sealUnchecked(t, &next)
			assertRejectedUnchanged(t, k, next, fault.want)
		})
	}
	for _, control := range []struct {
		name   string
		want   ErrorID
		mutate func(*FactCandidate)
	}{
		{"missing-source", MissingMember, func(candidate *FactCandidate) { candidate.Origin.SourceID = nil }},
		{"missing-epoch", MissingMember, func(candidate *FactCandidate) { candidate.SourceEpochID = nil }},
		{"missing-generation", MissingMember, func(candidate *FactCandidate) { candidate.DriverGeneration = nil }},
		{"invalid-source", InvalidIdentifier, func(candidate *FactCandidate) {
			invalid := SourceID("!")
			candidate.Origin.SourceID = &invalid
		}},
		{"invalid-epoch", InvalidIdentifier, func(candidate *FactCandidate) {
			invalid := SourceEpochID("!")
			candidate.SourceEpochID = &invalid
		}},
		{"invalid-generation", InvalidIdentifier, func(candidate *FactCandidate) {
			invalid := Uint64("bad")
			candidate.DriverGeneration = &invalid
		}},
	} {
		t.Run("candidate-pointer/"+control.name, func(t *testing.T) {
			k, initial, _, _, hooks := invariantInitialWithHooks(t)
			next := publicationBatch(initial.AssetID, initial.SourceID, initial.SourceEpochID, "1", "2", "1")
			candidate := publicationCandidate("candidate:new", "fact.power", true, initial.SourceID, initial.SourceEpochID, "binding:a", "1")
			control.mutate(&candidate)
			next.FactUpserts = []FactCandidate{candidate}
			sealUnchecked(t, &next)
			assertRejectedUnchanged(t, k, next, control.want)
			if hooks.calls() != 0 || len(hooks.retained) != 0 {
				t.Fatalf("partial candidate reached hooks: calls=%d retained=%d", hooks.calls(), len(hooks.retained))
			}
		})
	}
}

func TestPublicationOwnershipMatrix(t *testing.T) {
	for _, kind := range []string{"observed", "inferred-over-observed", "binding", "link", "service", "capability"} {
		t.Run(kind, func(t *testing.T) {
			k, a, old, oldRaw := invariantInitial(t)
			b := completePublicationBatch(a.AssetID, "source:b", "epoch:b", "binding:b", "1", "1", "1")
			b.FactUpserts[0].Value = pointerRecord(booleanValue(false))
			sealPublicationBatch(t, &b)
			before, beforeRaw, err := k.Apply(b, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if len(envelopeByFact(t, before, "fact.power").Conflicts) != 1 {
				t.Fatal("distinct sources lost their alternatives")
			}
			next := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "1", "2", "2")
			want := InvalidValue
			switch kind {
			case "observed":
				c := b.FactUpserts[0]
				c.CandidateID, c.Revision = a.FactUpserts[0].CandidateID, "2"
				next.FactUpserts = []FactCandidate{c}
			case "inferred-over-observed":
				c := publicationDerivedCandidate(a.FactUpserts[0].CandidateID, "fact.power", b.FactUpserts)
				c.Revision = "2"
				next.FactUpserts = []FactCandidate{c}
			case "binding":
				c := b.BindingUpserts[0]
				c.BindingID, c.Revision = a.BindingUpserts[0].BindingID, "2"
				next.BindingUpserts = []NativeBinding{c}
				want = IdentityNotQualified
			case "link":
				c := a.IdentityLinkUpserts[0]
				c.Basis, c.Revision = []EvidenceRef{publicEvidence("6")}, "2"
				next.IdentityLinkUpserts = []IdentityLink{c}
			case "service":
				c := b.ServiceUpserts[0]
				c.InstanceID, c.Revision = a.ServiceUpserts[0].InstanceID, "2"
				next.ServiceUpserts = []ServiceInstance{c}
			case "capability":
				c := b.CapabilityUpserts[0]
				c.InstanceID, c.Revision = a.CapabilityUpserts[0].InstanceID, "2"
				next.CapabilityUpserts = []CapabilityInstance{c}
			}
			sealPublicationBatch(t, &next)
			assertRejectedUnchanged(t, k, next, want)
			assertHistoricalBytes(t, before, beforeRaw)
			assertHistoricalBytes(t, old, oldRaw)

			// Same-source observed revision, a distinct alternative and a multi-source
			// derivation are all legal. The inferred record has no synthetic owner.
			legal := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "1", "2", "2")
			revised := b.FactUpserts[0]
			revised.Revision, revised.Evidence = "2", []EvidenceRef{publicEvidence("7")}
			alternative := revised
			alternative.CandidateID, alternative.Revision = "candidate:extra", "9"
			derived := publicationDerivedCandidate("candidate:derived", "fact.net", []FactCandidate{a.FactUpserts[0], revised})
			legal.FactUpserts = []FactCandidate{revised, alternative, derived}
			sort.Slice(legal.FactUpserts, func(i, j int) bool { return legal.FactUpserts[i].CandidateID < legal.FactUpserts[j].CandidateID })
			sealPublicationBatch(t, &legal)
			result, raw, err := k.Apply(legal, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if len(envelopeByFact(t, result, "fact.power").Candidates) != 3 || len(candidateByID(t, result, derived.CandidateID).Derivation.Inputs) != 2 {
				t.Fatal("legal lineage/alternatives lost")
			}
			_, replayRaw, err := k.Apply(legal, publicationMonotonic)
			if err != nil || !bytes.Equal(raw, replayRaw) {
				t.Fatalf("replay: %v", err)
			}
		})
	}
}

func TestPublicationTombstoneMatrix(t *testing.T) {
	for _, transition := range []string{"fence", "retirement"} {
		for _, kind := range []string{"service", "capability"} {
			for _, overlap := range []bool{false, true} {
				name := transition + "/" + kind
				if overlap {
					name += "/explicit-overlap"
				} else {
					name += "/automatic"
				}
				t.Run(name, func(t *testing.T) {
					k, initial, old, oldRaw := invariantInitial(t)
					epoch, generation := initial.SourceEpochID, Uint64("2")
					want := StaleDriverGeneration
					if transition == "retirement" {
						epoch, generation, want = "epoch:new", "1", StaleSourceEpoch
					}
					next := completePublicationBatch(initial.AssetID, initial.SourceID, epoch, "binding:new", generation, "1", "1")
					if transition == "fence" {
						next.SourceUpserts = []SourceDescriptor{}
						next.GenerationFences = []GenerationFence{publicationFence(initial.SourceID, initial.SourceEpochID, "1", publicEvidence("b"))}
					} else {
						next.SourceRetirements = []SourceEpochID{initial.SourceEpochID}
					}
					if overlap {
						next.ServiceWithdrawals = []ServiceInstanceID{initial.ServiceUpserts[0].InstanceID}
						next.CapabilityWithdrawals = []CapabilityInstanceID{initial.CapabilityUpserts[0].InstanceID}
					}
					legal := clonePublicationBatch(next)
					if kind == "service" {
						next.ServiceUpserts[0].InstanceID, next.ServiceUpserts[0].Revision = initial.ServiceUpserts[0].InstanceID, "2"
						next.CapabilityUpserts[0].ServiceInstance = initial.ServiceUpserts[0].InstanceID
					} else {
						next.CapabilityUpserts[0].InstanceID, next.CapabilityUpserts[0].Revision = initial.CapabilityUpserts[0].InstanceID, "2"
					}
					sealPublicationBatch(t, &next)
					reuseError := want
					if kind == "service" {
						// Rebinding only the service also breaks the retained
						// capability's exact service/binding correspondence.
						reuseError = DanglingReference
					}
					assertRejectedUnchanged(t, k, next, reuseError)
					assertHistoricalBytes(t, old, oldRaw)
					sealPublicationBatch(t, &legal)
					result, raw, err := k.Apply(legal, publicationMonotonic)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := Decode[Snapshot](raw); err != nil {
						t.Fatal(err)
					}
					service := serviceByID(t, result, initial.ServiceUpserts[0].InstanceID)
					cap := capabilityByID(t, result, initial.CapabilityUpserts[0].InstanceID)
					if service.BindingID != initial.BindingUpserts[0].BindingID || cap.BindingID != service.BindingID || service.Availability != AvailabilityWithdrawn || cap.Availability != AvailabilityWithdrawn || service.Revision != "2" || cap.Revision != "2" {
						t.Fatal("tombstone identity/revision lost")
					}
					if serviceByID(t, result, legal.ServiceUpserts[0].InstanceID).Availability != AvailabilityAvailable {
						t.Fatal("distinct replacement lost")
					}
					// Retained tombstones also forbid ID reuse in a later unrelated patch.
					later := publicationBatch(legal.AssetID, legal.SourceID, legal.SourceEpochID, legal.DriverGeneration, "2", "2")
					if kind == "service" {
						later.ServiceUpserts = next.ServiceUpserts
					} else {
						later.CapabilityUpserts = next.CapabilityUpserts
					}
					sealPublicationBatch(t, &later)
					assertRejectedUnchanged(t, k, later, reuseError)
					unrelated := completePublicationBatch(initial.AssetID, "source:b", "epoch:b", "binding:b", "1", "1", "2")
					sealPublicationBatch(t, &unrelated)
					if _, _, err := k.Apply(unrelated, publicationMonotonic); err != nil {
						t.Fatal(err)
					}
					assertHistoricalBytes(t, result, raw)
				})
			}
		}
	}
}

func TestPublicationProfileEpochMatrix(t *testing.T) {
	for _, field := range []string{"profile-id", "profile-version", "metadata"} {
		t.Run(field, func(t *testing.T) {
			k, b, old, raw := invariantInitial(t)
			next := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "1", "2", "1")
			source := b.SourceUpserts[0]
			source.Revision = "2"
			switch field {
			case "profile-id":
				source.ProfileID = "profile.changed"
			case "profile-version":
				source.ProfileVersion = "2.0.0"
			case "metadata":
				source.RegistryEvidence = publicEvidence("8")
			}
			next.SourceUpserts = []SourceDescriptor{source}
			sealPublicationBatch(t, &next)
			if field == "metadata" {
				result, _, err := k.Apply(next, publicationMonotonic)
				if err != nil || result.Sources[0].Revision != "2" {
					t.Fatalf("metadata revision: %v", err)
				}
			} else {
				assertRejectedUnchanged(t, k, next, StaleSourceEpoch)
				next.SourceEpochID, next.Sequence = "epoch:new", "1"
				source.SourceEpochID, source.Revision = "epoch:new", "7"
				next.SourceUpserts = []SourceDescriptor{source}
				next.SourceRetirements = []SourceEpochID{b.SourceEpochID}
				sealPublicationBatch(t, &next)
				result, _, err := k.Apply(next, publicationMonotonic)
				if err != nil || sourceByEpoch(t, result, b.SourceEpochID).State != SourceRetired {
					t.Fatalf("replacement epoch: %v", err)
				}
			}
			assertHistoricalBytes(t, old, raw)
		})
	}
}

var initialRevisionFields = map[string]func(*PublicationBatch) *Uint64{
	"source":     func(b *PublicationBatch) *Uint64 { return &b.SourceUpserts[0].Revision },
	"binding":    func(b *PublicationBatch) *Uint64 { return &b.BindingUpserts[0].Revision },
	"link":       func(b *PublicationBatch) *Uint64 { return &b.IdentityLinkUpserts[0].Revision },
	"candidate":  func(b *PublicationBatch) *Uint64 { return &b.FactUpserts[0].Revision },
	"service":    func(b *PublicationBatch) *Uint64 { return &b.ServiceUpserts[0].Revision },
	"capability": func(b *PublicationBatch) *Uint64 { return &b.CapabilityUpserts[0].Revision },
}

func TestPublicationInitialRevisionMatrix(t *testing.T) {
	for kind, revision := range initialRevisionFields {
		t.Run(kind, func(t *testing.T) {
			k := newTestPublicationKernel(t, "asset:site")
			b := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
			for _, invalid := range []Uint64{"0", "07", "-1"} {
				*revision(&b) = invalid
				b.BatchDigest = Digest("sha256:" + string(bytes.Repeat([]byte("a"), 64)))
				assertRejectedUnchanged(t, k, b, InvalidIdentifier)
			}
			*revision(&b) = "7"
			sealPublicationBatch(t, &b)
			old, raw, err := k.Apply(b, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if publicationObjectRevision(old, kind) != "7" {
				t.Fatal("producer's initial revision was not preserved")
			}
			// Unchanged objects retain the producer's initial revision in another batch.
			next := clonePublicationBatch(b)
			next.Sequence, next.ExpectedSemanticRevision = "2", "1"
			sealPublicationBatch(t, &next)
			same, _, err := k.Apply(next, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(old.Sources, same.Sources) || !reflect.DeepEqual(old.Bindings, same.Bindings) || !reflect.DeepEqual(old.IdentityLinks, same.IdentityLinks) || !reflect.DeepEqual(old.Facts, same.Facts) || !reflect.DeepEqual(old.Services, same.Services) || !reflect.DeepEqual(old.Capabilities, same.Capabilities) {
				t.Fatal("unchanged upsert altered initial revision")
			}
			next.Sequence, next.ExpectedSemanticRevision = "3", "2"
			*revision(&next) = "8"
			sealPublicationBatch(t, &next)
			assertRejectedUnchanged(t, k, next, RevisionConflict) // revision-only changes conflict
			switch kind {
			case "source":
				next.SourceUpserts[0].RegistryEvidence = publicEvidence("8")
			case "binding":
				assertHistoricalBytes(t, old, raw)
				return // all publisher fields identify the immutable binding
			case "link":
				next.IdentityLinkUpserts[0].Basis = []EvidenceRef{publicEvidence("8")}
			case "candidate":
				next.FactUpserts[0].Value = pointerRecord(booleanValue(false))
			case "service":
				next.ServiceUpserts[0].Availability = AvailabilityUnavailable
			case "capability":
				next.CapabilityUpserts[0].Availability = AvailabilityDegraded
			}
			*revision(&next) = "9"
			sealPublicationBatch(t, &next)
			assertRejectedUnchanged(t, k, next, RevisionConflict)
			*revision(&next) = "8"
			sealPublicationBatch(t, &next)
			result, _, err := k.Apply(next, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if publicationObjectRevision(result, kind) != "8" {
				t.Fatal("object revision did not increment exactly once")
			}
			assertHistoricalBytes(t, old, raw)
		})
	}
}

func TestPublicationRetainedCursorMatrix(t *testing.T) {
	for _, state := range []string{"current", "fenced", "retired"} {
		t.Run(state, func(t *testing.T) {
			k, b, s, raw := invariantInitial(t)
			if state != "current" {
				next := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "2", "1", "1")
				if state == "fenced" {
					next.GenerationFences = []GenerationFence{publicationFence(b.SourceID, b.SourceEpochID, "1", publicEvidence("b"))}
				} else {
					next.SourceEpochID, next.DriverGeneration = "epoch:new", "1"
					next.SourceUpserts = []SourceDescriptor{publicationSource(b.SourceID, "epoch:new")}
					next.SourceRetirements = []SourceEpochID{b.SourceEpochID}
				}
				sealPublicationBatch(t, &next)
				var err error
				s, raw, err = k.Apply(next, publicationMonotonic)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Decode[Snapshot](raw); err != nil {
				t.Fatal(err)
			}
			for _, mutation := range []string{"missing", "wrong-epoch", "wrong-generation"} {
				t.Run(mutation, func(t *testing.T) {
					broken := cloneSnapshot(s)
					for i, c := range broken.Cursors {
						if c.SourceEpochID == b.SourceEpochID && c.DriverGeneration == "1" {
							switch mutation {
							case "missing":
								broken.Cursors = append(broken.Cursors[:i], broken.Cursors[i+1:]...)
							case "wrong-epoch":
								broken.Cursors[i].SourceEpochID = "epoch:unknown"
							case "wrong-generation":
								broken.Cursors[i].DriverGeneration = "7"
							}
							break
						}
					}
					sort.Slice(broken.Cursors, func(i, j int) bool { return compareCursor(broken.Cursors[i], broken.Cursors[j]) < 0 })
					recomputeSnapshotID(t, &broken)
					requireID(t, broken.Validate(), DanglingReference)
					encoded, _ := json.Marshal(broken)
					_, err := Decode[Snapshot](encoded)
					requireID(t, err, DanglingReference)
					assertHistoricalBytes(t, s, raw)
				})
			}
		})
	}
}

func TestPublicationErrorTransitionMatrix(t *testing.T) {
	// Each mutation is independently knowable from pre-state or proposed refs.
	// No pairwise diagnostics are needed: the public winner is accumulated once.
	mutations := map[string]struct {
		want  ErrorID
		apply func(*PublicationBatch, PublicationBatch)
	}{
		"unknown-fact-withdrawal": {DanglingReference, func(n *PublicationBatch, b PublicationBatch) { n.FactWithdrawals = []CandidateID{"candidate:missing"} }},
		"unknown-service-withdrawal": {DanglingReference, func(n *PublicationBatch, b PublicationBatch) {
			n.ServiceWithdrawals = []ServiceInstanceID{"service:missing"}
		}},
		"unknown-capability-withdrawal": {DanglingReference, func(n *PublicationBatch, b PublicationBatch) {
			n.CapabilityWithdrawals = []CapabilityInstanceID{"capability:missing"}
		}},
		"unknown-retirement": {DanglingReference, func(n *PublicationBatch, b PublicationBatch) { n.SourceRetirements = []SourceEpochID{"epoch:missing"} }},
		"unknown-link-binding": {DanglingReference, func(n *PublicationBatch, b PublicationBatch) {
			n.IdentityLinkUpserts = []IdentityLink{publicationLink(b.AssetID, "binding:missing")}
		}},
		"binding-owner": {InvalidValue, func(n *PublicationBatch, b PublicationBatch) {
			v := b.BindingUpserts[0]
			v.BindingID, v.SourceID = "binding:new", "source:wrong"
			n.BindingUpserts = []NativeBinding{v}
		}},
		"candidate-owner": {InvalidValue, func(n *PublicationBatch, b PublicationBatch) {
			n.FactUpserts = []FactCandidate{publicationCandidate("candidate:new", "fact.power", true, "source:wrong", b.SourceEpochID, b.BindingUpserts[0].BindingID, "1")}
		}},
		"link-asset": {InvalidValue, func(n *PublicationBatch, b PublicationBatch) {
			v := b.IdentityLinkUpserts[0]
			v.AssetID, v.Revision = "asset:wrong", "2"
			n.IdentityLinkUpserts = []IdentityLink{v}
		}},
		"service-asset": {InvalidValue, func(n *PublicationBatch, b PublicationBatch) {
			v := b.ServiceUpserts[0]
			v.AssetID, v.Revision = "asset:wrong", "2"
			n.ServiceUpserts = []ServiceInstance{v}
		}},
		"capability-asset": {InvalidValue, func(n *PublicationBatch, b PublicationBatch) {
			v := b.CapabilityUpserts[0]
			v.AssetID, v.Revision = "asset:wrong", "2"
			n.CapabilityUpserts = []CapabilityInstance{v}
		}},
	}
	for _, revisionFault := range []string{"semantic", "source-object"} {
		for name, mutation := range mutations {
			t.Run(revisionFault+"/"+name, func(t *testing.T) {
				k, b, old, raw := invariantInitial(t)
				n := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "1", "2", "1")
				if revisionFault == "semantic" {
					n.ExpectedSemanticRevision = "0"
				} else {
					v := b.SourceUpserts[0]
					v.Revision = "3"
					v.RegistryEvidence = publicEvidence("8")
					n.SourceUpserts = []SourceDescriptor{v}
				}
				mutation.apply(&n, b)
				sealPublicationBatch(t, &n)
				for i := 0; i < 3; i++ {
					assertRejectedUnchanged(t, k, n, mutation.want)
				}
				assertHistoricalBytes(t, old, raw)
			})
		}
	}
	for _, transition := range []string{"fence", "retirement", "cycle"} {
		for _, missingFirst := range []bool{false, true} {
			name := transition + "/missing-last"
			if missingFirst {
				name = transition + "/missing-first"
			}
			t.Run(name, func(t *testing.T) {
				k, b, old, raw := invariantInitial(t)
				n := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "2", "1", "1")
				root := b.FactUpserts[0]
				d := publicationDerivedCandidate("candidate:y", "fact.derived", []FactCandidate{root})
				missing := d.Derivation.Inputs[0]
				missing.CandidateID = "candidate:zzmissing"
				if missingFirst {
					missing.CandidateID = "candidate:aa-missing"
				}
				d.Derivation.Inputs = append(d.Derivation.Inputs, missing)
				switch transition {
				case "fence":
					n.GenerationFences = []GenerationFence{publicationFence(b.SourceID, b.SourceEpochID, "1", publicEvidence("b"))}
				case "retirement":
					n.SourceEpochID, n.DriverGeneration = "epoch:new", "1"
					n.SourceUpserts = []SourceDescriptor{publicationSource(b.SourceID, "epoch:new")}
					n.SourceRetirements = []SourceEpochID{b.SourceEpochID}
				case "cycle":
					n.DriverGeneration, n.Sequence = "1", "2"
					z := publicationDerivedCandidate("candidate:z", "fact.other", []FactCandidate{d})
					z.Derivation.Inputs[0].SourcePaths = candidateSourcePaths(root)
					d.Derivation.Inputs[0].CandidateID = z.CandidateID
					n.FactUpserts = append(n.FactUpserts, z)
				}
				sort.Slice(d.Derivation.Inputs, func(i, j int) bool { return d.Derivation.Inputs[i].CandidateID < d.Derivation.Inputs[j].CandidateID })
				n.FactUpserts = append(n.FactUpserts, d)
				sort.Slice(n.FactUpserts, func(i, j int) bool { return n.FactUpserts[i].CandidateID < n.FactUpserts[j].CandidateID })
				sealPublicationBatch(t, &n)
				assertRejectedUnchanged(t, k, n, DanglingReference)
				assertHistoricalBytes(t, old, raw)
			})
		}
	}
}

func TestPublicationContractRecordContext(t *testing.T) {
	_, b, s, _ := invariantInitial(t)
	for name, record := range map[string]Record{"batch": b, "snapshot": s, "evidence": publicEvidence("a")} {
		for _, token := range []string{"absent", "null", "23", "true", "[]", "{}", "\"\"", "\"wrong\""} {
			t.Run(name+"/"+token, func(t *testing.T) {
				raw, _ := json.Marshal(record)
				var object map[string]json.RawMessage
				_ = json.Unmarshal(raw, &object)
				if token == "absent" {
					delete(object, "contract")
				} else {
					object["contract"] = json.RawMessage(token)
				}
				decode := func(raw []byte) error {
					switch name {
					case "batch":
						_, err := Decode[PublicationBatch](raw)
						return err
					case "snapshot":
						_, err := Decode[Snapshot](raw)
						return err
					default:
						_, err := Decode[EvidenceRef](raw)
						return err
					}
				}
				want := InvalidContract
				if name == "evidence" {
					switch token {
					case "absent", "null":
						want = MissingMember
					case "\"\"", "\"wrong\"":
						want = InvalidEvidence
					default:
						want = InvalidValue
					}
				}
				raw, _ = json.Marshal(object)
				requireID(t, decode(raw), want)
				delete(object, "asset_id")
				if name == "evidence" {
					delete(object, "owner")
					want = MissingMember
				}
				raw, _ = json.Marshal(object)
				requireID(t, decode(raw), want)
				// Duplicate object member outranks both missing and invalid contracts.
				raw = append([]byte(`{"revision":"1","revision":"1",`), raw[1:]...)
				requireID(t, decode(raw), DuplicateKey)
			})
		}
	}
}

func publicationObjectRevision(s Snapshot, kind string) Uint64 {
	switch kind {
	case "source":
		return s.Sources[0].Revision
	case "binding":
		return s.Bindings[0].Revision
	case "link":
		return s.IdentityLinks[0].Revision
	case "candidate":
		return s.Facts[0].Candidates[0].Revision
	case "service":
		return s.Services[0].Revision
	case "capability":
		return s.Capabilities[0].Revision
	}
	return ""
}

func TestPublicationSameSourceCandidatePathRevision(t *testing.T) {
	for _, transition := range []string{"fence", "retirement"} {
		t.Run(transition, func(t *testing.T) {
			k, b, old, raw := invariantInitial(t)
			n := completePublicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "binding:new", "2", "1", "1")
			if transition == "fence" {
				n.SourceUpserts = []SourceDescriptor{}
				n.GenerationFences = []GenerationFence{publicationFence(b.SourceID, b.SourceEpochID, "1", publicEvidence("b"))}
			} else {
				n = completePublicationBatch(b.AssetID, b.SourceID, "epoch:new", "binding:new", "1", "1", "1")
				n.SourceRetirements = []SourceEpochID{b.SourceEpochID}
			}
			n.FactUpserts[0].CandidateID, n.FactUpserts[0].Revision = b.FactUpserts[0].CandidateID, "2"
			sealPublicationBatch(t, &n)
			result, _, err := k.Apply(n, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if got := candidateByID(t, result, b.FactUpserts[0].CandidateID); got.Revision != "2" || *got.BindingID != "binding:new" {
				t.Fatal("same-source candidate path revision lost")
			}
			assertHistoricalBytes(t, old, raw)
		})
	}
}

func TestPublicationSourceOnlyCursorCompleteness(t *testing.T) {
	k := newTestPublicationKernel(t, "asset:site")
	b := publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
	b.SourceUpserts = []SourceDescriptor{publicationSource(b.SourceID, b.SourceEpochID)}
	sealPublicationBatch(t, &b)
	s, raw, err := k.Apply(b, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	for _, retire := range []bool{false, true} {
		if retire {
			n := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "1", "2", "1")
			n.SourceRetirements = []SourceEpochID{b.SourceEpochID}
			sealPublicationBatch(t, &n)
			s, raw, err = k.Apply(n, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
		}
		broken := cloneSnapshot(s)
		broken.Cursors = []PublicationCursor{}
		recomputeSnapshotID(t, &broken)
		requireID(t, broken.Validate(), DanglingReference)
		encoded, _ := json.Marshal(broken)
		_, err = Decode[Snapshot](encoded)
		requireID(t, err, DanglingReference)
		assertHistoricalBytes(t, s, raw)
	}
}

func TestPublicationGraphErrorResource(t *testing.T) {
	for _, count := range []int{64, 256, 1024, 4096} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			// Each layer shares both inputs. A missing leaf must propagate once per
			// retained node, even when the full 4096-node graph is invalid.
			root := publicationCandidate("candidate:missing", "fact.root", true, "source:a", "epoch:a", "binding:a", "1")
			template := publicationDerivedCandidate("candidate:template", "fact.derived", []FactCandidate{root})
			candidates := make(map[CandidateID]FactCandidate, count)
			ids := make([]CandidateID, 0, count)
			previous := []CandidateID{root.CandidateID}
			for i := 0; i < count; i += 2 {
				var layer []CandidateID
				for col := 0; col < 2; col++ {
					c := template
					c.CandidateID = CandidateID(fmt.Sprintf("candidate:n%04d", i+col))
					d := *template.Derivation
					d.Inputs = nil
					for _, input := range previous {
						v := template.Derivation.Inputs[0]
						v.CandidateID = input
						d.Inputs = append(d.Inputs, v)
					}
					c.Derivation = &d
					candidates[c.CandidateID] = c
					ids = append(ids, c.CandidateID)
					layer = append(layer, c.CandidateID)
				}
				previous = layer
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			started := time.Now()
			r := candidateGraphResolver{candidates: candidates, bindings: map[NativeBindingID]NativeBinding{}, states: make(map[CandidateID]uint8), results: make(map[CandidateID]candidateGraphResult)}
			r.findCycles(ids)
			for _, id := range ids {
				_, err := r.resolve(id)
				requireID(t, err, DanglingReference)
			}
			runtime.ReadMemStats(&after)
			if len(r.results) != count || len(r.states) != count {
				t.Fatalf("memo count: results=%d states=%d nodes=%d", len(r.results), len(r.states), count)
			}
			t.Logf("nodes=%d memo_results=%d allocations=%d allocated_bytes=%d elapsed=%s", count, len(r.results), after.Mallocs-before.Mallocs, after.TotalAlloc-before.TotalAlloc, time.Since(started))
		})
	}
}

func TestPublicationOwnershipWithMissingReferences(t *testing.T) {
	for _, kind := range []string{"candidate", "link", "service", "capability"} {
		t.Run(kind, func(t *testing.T) {
			k, b, old, raw := invariantInitial(t)
			n := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "1", "2", "1")
			switch kind {
			case "candidate":
				n.FactUpserts = []FactCandidate{publicationCandidate("candidate:new", "fact.power", true, "source:wrong", b.SourceEpochID, "binding:missing", "1")}
			case "link":
				n.IdentityLinkUpserts = []IdentityLink{publicationLink("asset:wrong", "binding:missing")}
			case "service":
				v := b.ServiceUpserts[0]
				v.AssetID, v.BindingID, v.Revision = "asset:wrong", "binding:missing", "2"
				n.ServiceUpserts = []ServiceInstance{v}
			case "capability":
				v := b.CapabilityUpserts[0]
				v.AssetID, v.BindingID, v.ServiceInstance, v.Revision = "asset:wrong", "binding:missing", "service:missing", "2"
				n.CapabilityUpserts = []CapabilityInstance{v}
			}
			sealPublicationBatch(t, &n)
			assertRejectedUnchanged(t, k, n, InvalidValue)
			assertHistoricalBytes(t, old, raw)
		})
	}
	for _, kind := range []string{"service", "capability"} {
		t.Run("prior-owner/"+kind, func(t *testing.T) {
			k, a, old, raw := invariantInitial(t)
			n := publicationBatch(a.AssetID, "source:b", "epoch:b", "1", "1", "1")
			n.SourceUpserts = []SourceDescriptor{publicationSource(n.SourceID, n.SourceEpochID)}
			if kind == "service" {
				v := a.ServiceUpserts[0]
				v.SourceEpochID, v.BindingID, v.Revision = n.SourceEpochID, "binding:missing", "2"
				n.ServiceUpserts = []ServiceInstance{v}
			} else {
				v := a.CapabilityUpserts[0]
				v.SourceEpochID, v.BindingID, v.Revision = n.SourceEpochID, "binding:missing", "2"
				n.CapabilityUpserts = []CapabilityInstance{v}
			}
			sealPublicationBatch(t, &n)
			assertRejectedUnchanged(t, k, n, InvalidValue)
			assertHistoricalBytes(t, old, raw)
		})
	}
}

func TestPublicationFenceInitialRevision(t *testing.T) {
	k, b, _, _ := invariantInitial(t)
	n := publicationBatch(b.AssetID, b.SourceID, b.SourceEpochID, "2", "1", "1")
	n.GenerationFences = []GenerationFence{publicationFence(b.SourceID, b.SourceEpochID, "1", publicEvidence("b"))}
	n.GenerationFences[0].Revision = "7"
	sealPublicationBatch(t, &n)
	result, _, err := k.Apply(n, publicationMonotonic)
	if err != nil || result.Fences[0].Revision != "7" {
		t.Fatalf("positive initial fence revision: %v", err)
	}
}
