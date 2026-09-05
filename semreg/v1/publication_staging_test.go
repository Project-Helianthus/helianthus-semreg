package semreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Synthetic contract-derived controls: docs-semantic@b16667d719defc7b0fef0400ee3ad387469018ac,
// kernel publication dependency/lifecycle and global candidate identity rules;
// serialization independent-error precedence. No new fixture IDs or contracts.
func TestPublicationTypedLifecycleClosure(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, mode := range []string{"fence", "retire", "supersede"} {
			for _, dependency := range []string{"observed", "own", "other", "transitive-own", "transitive-other", "mixed"} {
				t.Run(fmt.Sprintf("%t/%s/%s", reverse, mode, dependency), func(t *testing.T) {
					k := newTestPublicationKernel(t, "asset:site")
					a := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
					b := completePublicationBatch("asset:site", "source:b", "epoch:b", "binding:b", "1", "1", "1")
					setup := []PublicationBatch{a, b}
					if reverse {
						setup = []PublicationBatch{b, a}
					}
					for i, batch := range setup {
						batch.ExpectedSemanticRevision = Uint64(fmt.Sprint(i))
						sealPublicationBatch(t, &batch)
						if _, _, err := k.Apply(batch, publicationMonotonic); err != nil {
							t.Fatal(err)
						}
					}
					old, oldRaw, _ := k.Current()
					n := publicationBatch(a.AssetID, a.SourceID, a.SourceEpochID, "1", "2", "2")
					want := StaleDriverGeneration
					if mode == "retire" {
						n.SourceRetirements = []SourceEpochID{a.SourceEpochID}
						want = StaleSourceEpoch
					} else {
						n.GenerationFences = []GenerationFence{publicationFence(a.SourceID, a.SourceEpochID, "1", publicEvidence("b"))}
					}
					if mode == "supersede" {
						n.DriverGeneration, n.Sequence = "2", "1"
						if dependency == "observed" {
							want = InvalidValue // old observed path is outside the new header
						}
					}
					inputs := b.FactUpserts
					if strings.Contains(dependency, "own") {
						inputs = a.FactUpserts
					}
					if dependency == "mixed" {
						inputs = append(append([]FactCandidate{}, inputs...), a.FactUpserts...)
					}
					if strings.HasPrefix(dependency, "transitive") {
						middle := publicationDerivedCandidate("candidate:middle", "fact.middle", inputs)
						n.FactUpserts = append(n.FactUpserts, middle)
						inputs = []FactCandidate{middle}
					}
					derived := publicationDerivedCandidate("candidate:result", "fact.result", inputs)
					n.FactUpserts = append(n.FactUpserts, derived)
					if dependency == "observed" {
						n.FactUpserts = a.FactUpserts
					}
					sealPublicationBatch(t, &n)
					if !strings.Contains(dependency, "other") {
						assertRejectedUnchanged(t, k, n, want)
						return
					}
					s, raw, err := k.Apply(n, publicationMonotonic)
					if err != nil {
						t.Fatal(err)
					}
					if s.Revisions != (RevisionVector{Semantic: "3", Identity: "3", Facts: "3", Services: "3", Capabilities: "3"}) || hasCandidate(s, a.FactUpserts[0].CandidateID) || !hasCandidate(s, derived.CandidateID) || !reflect.DeepEqual(candidateByID(t, s, b.FactUpserts[0].CandidateID), b.FactUpserts[0]) {
						t.Fatal("closure, unrelated candidate or once-only component revisions")
					}
					service := serviceByID(t, s, a.ServiceUpserts[0].InstanceID)
					capability := capabilityByID(t, s, a.CapabilityUpserts[0].InstanceID)
					if service.Revision != "2" || service.Availability != AvailabilityWithdrawn || capability.Revision != "2" || capability.Availability != AvailabilityWithdrawn {
						t.Fatal("transition tombstones")
					}
					if _, err := Decode[Snapshot](raw); err != nil {
						t.Fatal(err)
					}
					for _, binding := range s.Bindings {
						if binding.SourceID == a.SourceID && (binding.Revision != "2" || binding.State == BindingCurrent) {
							t.Fatal("binding transition")
						}
					}
					if mode != "retire" && !cursorMap(&s)[cursorKey{a.SourceID, a.SourceEpochID, "1"}].Fenced {
						t.Fatal("lost fenced cursor")
					}
					replay, replayRaw, err := k.Apply(n, publicationMonotonic)
					if err != nil || !reflect.DeepEqual(s, replay) || !bytes.Equal(raw, replayRaw) {
						t.Fatal("exact replay")
					}
					assertHistoricalBytes(t, old, oldRaw)
				})
			}
		}
	}
}

func TestPublicationSemanticStagingMatrix(t *testing.T) {
	for _, fault := range []string{"none", "source", "binding", "cycle"} {
		for _, reference := range []string{"none", "fact", "service", "capability", "retirement", "cursor", "link"} {
			t.Run(fault+"/"+reference, func(t *testing.T) {
				k, a, old, raw := invariantInitial(t)
				n := publicationBatch(a.AssetID, a.SourceID, a.SourceEpochID, "1", "2", "1")
				var want ErrorID
				switch fault {
				case "source":
					s := a.SourceUpserts[0]
					s.State = SourceRetired
					n.SourceUpserts, want = []SourceDescriptor{s}, StaleSourceEpoch
				case "binding":
					b := a.BindingUpserts[0]
					b.State = BindingFenced
					n.BindingUpserts, want = []NativeBinding{b}, StaleDriverGeneration
				case "cycle":
					d := publicationDerivedCandidate("candidate:loop", "fact.loop", a.FactUpserts)
					d.Derivation.Inputs[0].CandidateID = d.CandidateID
					n.FactUpserts, want = []FactCandidate{d}, DerivationCycle
				}
				switch reference {
				case "fact":
					n.FactWithdrawals = []CandidateID{"candidate:unknown"}
				case "service":
					n.ServiceWithdrawals = []ServiceInstanceID{"service:unknown"}
				case "capability":
					n.CapabilityWithdrawals = []CapabilityInstanceID{"capability:unknown"}
				case "retirement":
					n.SourceRetirements = []SourceEpochID{"epoch:unknown"}
				case "cursor":
					n.GenerationFences = []GenerationFence{publicationFence(a.SourceID, a.SourceEpochID, "2", publicEvidence("b"))}
				case "link":
					n.IdentityLinkUpserts = []IdentityLink{publicationLink(a.AssetID, "binding:unknown")}
				}
				if reference != "none" {
					want = DanglingReference
				}
				// Seal intentional semantic failures without repairing their fields.
				var err error
				n.BatchDigest, err = n.computedDigestUnchecked()
				if err != nil {
					t.Fatal(err)
				}
				if want == "" {
					_, _, err = k.Apply(n, publicationMonotonic)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					assertRejectedUnchanged(t, k, n, want)
					// An unseen digest failure outranks lifecycle/reference errors.
					n.BatchDigest = a.BatchDigest
					assertRejectedUnchanged(t, k, n, DigestMismatch)
					// Accepted sequence reuse is its own partition; it must not
					// acquire reference errors from a hypothetical new update.
					n.Sequence = "1"
					n.BatchDigest, _ = n.computedDigestUnchecked()
					replayWant := map[string]ErrorID{"none": SequenceConflict, "source": StaleSourceEpoch, "binding": StaleDriverGeneration, "cycle": DerivationCycle}[fault]
					assertRejectedUnchanged(t, k, n, replayWant)
				}
				assertHistoricalBytes(t, old, raw)
			})
		}
	}
}

func TestPublicationPartialStagingGuard(t *testing.T) {
	for _, field := range []string{"binding", "epoch", "generation", "origin-source", "derivation", "invalid-generation"} {
		t.Run(field, func(t *testing.T) {
			k, a, _, _ := invariantInitial(t)
			n := publicationBatch(a.AssetID, a.SourceID, a.SourceEpochID, "1", "2", "1")
			c := a.FactUpserts[0]
			want := MissingMember
			switch field {
			case "binding":
				c.BindingID = nil
			case "epoch":
				c.SourceEpochID = nil
			case "generation":
				c.DriverGeneration = nil
			case "origin-source":
				c.Origin.SourceID = nil
			case "derivation":
				c = publicationDerivedCandidate("candidate:partial", "fact.partial", a.FactUpserts)
				c.Derivation = nil
			case "invalid-generation":
				c.DriverGeneration = pointerRecord(Uint64("!"))
				want = InvalidIdentifier
			}
			n.FactUpserts, n.FactWithdrawals = []FactCandidate{c}, []CandidateID{"candidate:unknown"}
			n.BatchDigest, _ = n.computedDigestUnchecked()
			assertRejectedUnchanged(t, k, n, want)
		})
	}
}

func TestSnapshotWireGlobalIdentityMatrix(t *testing.T) {
	_, a, s, _ := invariantInitial(t)
	c := a.FactUpserts[0]
	c.Key.FactID = "fact.other"
	s.Facts = append(s.Facts, FactEnvelope{AssetID: s.AssetID, Key: c.Key, Candidates: []FactCandidate{c}, Conflicts: []Conflict{}, Revision: "1"})
	sort.Slice(s.Facts, func(i, j int) bool { return compareEnvelope(s.Facts[i], s.Facts[j]) < 0 })
	for _, fault := range []string{"none", "missing-contract", "wrong-contract", "missing-time", "wrong-time"} {
		for _, identity := range []string{"duplicate", "distinct", "absent", "null", "number", "malformed"} {
			t.Run(fault+"/"+identity, func(t *testing.T) {
				copy := cloneSnapshot(s)
				if identity == "distinct" {
					copy.Facts[1].Candidates[0].CandidateID = "candidate:distinct"
				}
				recomputeSnapshotID(t, &copy)
				raw, _ := json.Marshal(copy)
				var object map[string]any
				_ = json.Unmarshal(raw, &object)
				var want ErrorID
				for _, envelope := range object["facts"].([]any) {
					candidate := envelope.(map[string]any)["candidates"].([]any)[0].(map[string]any)
					switch identity {
					case "absent":
						delete(candidate, "candidate_id")
						want = MissingMember
					case "null":
						candidate["candidate_id"], want = nil, MissingMember
					case "number":
						candidate["candidate_id"], want = 23, InvalidValue
					case "malformed":
						candidate["candidate_id"], want = "!", InvalidIdentifier
					}
				}
				switch fault {
				case "missing-contract":
					delete(object, "contract")
					want = InvalidContract
				case "wrong-contract":
					object["contract"], want = "wrong/v1", InvalidContract
				case "missing-time":
					delete(object, "evaluated_at")
					want = MissingMember
				case "wrong-time":
					object["evaluated_at"] = 23
					if want == "" || errorRanks[want] > errorRanks[InvalidValue] {
						want = InvalidValue
					}
				}
				if identity == "duplicate" {
					want = DuplicateKey
				}
				raw, _ = json.Marshal(object)
				for _, input := range [][]byte{raw, reverseMembers(t, raw)} {
					_, err := Decode[Snapshot](input)
					checkMatrixError(t, err, want)
				}
			})
		}
	}
}

func TestSnapshotGlobalIdentityResource(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		var previous uint64
		for _, n := range []int{256, 512, 1024} {
			// Partial envelopes isolate independent collection through Decode.
			// A duplicate is last, so all distinct supplied IDs must be visited.
			var envelopes []string
			for i := 0; i < n; i++ {
				id := i
				if duplicate && i == n-1 {
					id = 0
				}
				envelopes = append(envelopes, fmt.Sprintf(`{"candidates":[{"candidate_id":"candidate:%04d"}]}`, id))
			}
			raw := []byte(`{"facts":[` + strings.Join(envelopes, ",") + `]}`)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := Decode[Snapshot](raw)
			runtime.ReadMemStats(&after)
			want := InvalidContract
			if duplicate {
				want = DuplicateKey
			}
			requireID(t, err, want)
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("IDs=%d duplicate=%t input=%d allocated=%d allocations=%d", n, duplicate, len(raw), allocated, after.Mallocs-before.Mallocs)
			if allocated > 64<<20 || previous != 0 && allocated > 3*previous+(1<<20) {
				t.Fatal("global collection exceeded proportional allocation bound")
			}
			previous = allocated
		}
	}
}

func TestPublicationStagingErrorResource(t *testing.T) {
	k, a, _, _ := invariantInitial(t)
	var previous uint64
	for _, n := range []int{256, 512, 1024} {
		batch := publicationBatch(a.AssetID, a.SourceID, a.SourceEpochID, "1", "2", "1")
		loop := publicationDerivedCandidate("candidate:loop", "fact.loop", a.FactUpserts)
		loop.Derivation.Inputs[0].CandidateID = loop.CandidateID
		batch.FactUpserts = []FactCandidate{loop}
		for i := 0; i < n; i++ {
			batch.FactWithdrawals = append(batch.FactWithdrawals, CandidateID(fmt.Sprintf("candidate:missing%04d", i)))
		}
		batch.BatchDigest, _ = batch.computedDigestUnchecked()
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, _, err := k.Apply(batch, publicationMonotonic)
		runtime.ReadMemStats(&after)
		requireID(t, err, DanglingReference)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("unknown references=%d allocated=%d allocations=%d", n, allocated, after.Mallocs-before.Mallocs)
		if allocated > 16<<20 || previous != 0 && allocated > 3*previous+(1<<20) {
			t.Fatal("staging error aggregation exceeded proportional allocation bound")
		}
		previous = allocated
		assertRejectedUnchanged(t, k, batch, DanglingReference)
	}
}

func TestPublicationFinalCandidateCardinality(t *testing.T) {
	k := newTestPublicationKernel(t, "asset:site")
	initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	root := initial.FactUpserts[0]
	for i := 1; i < maxDerivationNodes; i++ {
		candidate := publicationDerivedCandidate(
			CandidateID(fmt.Sprintf("candidate:retained%04d", i)),
			DefinitionID(fmt.Sprintf("fact.group%03d", i/32)),
			[]FactCandidate{root},
		)
		initial.FactUpserts = append(initial.FactUpserts, candidate)
	}
	sort.Slice(initial.FactUpserts, func(i, j int) bool {
		return initial.FactUpserts[i].CandidateID < initial.FactUpserts[j].CandidateID
	})
	sealPublicationBatch(t, &initial)
	before, beforeRaw, err := k.Apply(initial, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}

	next := publicationBatch(initial.AssetID, initial.SourceID, initial.SourceEpochID, "1", "2", "1")
	next.FactWithdrawals = []CandidateID{root.CandidateID}
	for _, id := range []CandidateID{"candidate:newa", "candidate:newb"} {
		next.FactUpserts = append(next.FactUpserts, publicationCandidate(id, "fact.new", true, initial.SourceID, initial.SourceEpochID, "binding:a", "1"))
	}
	sealPublicationBatch(t, &next)
	result, raw, err := k.Apply(next, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Facts) != 1 || len(result.Facts[0].Candidates) != 2 {
		t.Fatal("atomic cascade did not close to two candidates")
	}
	if result.Revisions != (RevisionVector{Semantic: "2", Identity: "1", Facts: "2", Services: "1", Capabilities: "1"}) {
		t.Fatalf("once-only revisions: %+v", result.Revisions)
	}
	for _, candidate := range result.Facts[0].Candidates {
		if candidate.Revision != "1" {
			t.Fatalf("new candidate revision: %s", candidate.Revision)
		}
	}
	replay, replayRaw, err := k.Apply(next, publicationMonotonic)
	if err != nil || !reflect.DeepEqual(result, replay) || !bytes.Equal(raw, replayRaw) {
		t.Fatal("atomic replacement replay changed")
	}
	assertHistoricalBytes(t, before, beforeRaw)
}

func TestPublicationFinalCandidateCardinalityControls(t *testing.T) {
	binding := publicationBinding("asset:site", "source:a", "epoch:a", "binding:a", "1")
	bindings := map[NativeBindingID]NativeBinding{binding.BindingID: binding}

	t.Run("oversized-final", func(t *testing.T) {
		candidates := make(map[CandidateID]FactCandidate, maxDerivationNodes+1)
		upserted := make(map[CandidateID]struct{}, maxDerivationNodes+1)
		for i := 0; i <= maxDerivationNodes; i++ {
			id := CandidateID(fmt.Sprintf("candidate:new%04d", i))
			candidates[id] = publicationCandidate(id, "fact.new", true, binding.SourceID, binding.SourceEpochID, binding.BindingID, binding.DriverGeneration)
			upserted[id] = struct{}{}
		}
		requireID(t, closeCandidateGraph(candidates, upserted, bindings), BoundsExceeded)
		if len(candidates) != maxDerivationNodes+1 {
			t.Fatal("oversized upserts were silently removed")
		}
	})

	t.Run("invalid-same-batch-inferred", func(t *testing.T) {
		root := publicationCandidate("candidate:missing", "fact.root", true, binding.SourceID, binding.SourceEpochID, binding.BindingID, binding.DriverGeneration)
		candidate := publicationDerivedCandidate("candidate:invalid", "fact.invalid", []FactCandidate{root})
		candidates := map[CandidateID]FactCandidate{candidate.CandidateID: candidate}
		upserted := map[CandidateID]struct{}{candidate.CandidateID: {}}
		requireID(t, closeCandidateGraph(candidates, upserted, bindings), DanglingReference)
		if len(candidates) != 1 {
			t.Fatal("invalid same-batch upsert was silently removed")
		}
	})

	t.Run("staging-operation-bound", func(t *testing.T) {
		batch := publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
		for i := 0; i <= maxStagedDerivationNodes; i++ {
			id := CandidateID(fmt.Sprintf("candidate:staged%04d", i))
			batch.FactUpserts = append(batch.FactUpserts, publicationCandidate(id, "fact.staged", true, binding.SourceID, binding.SourceEpochID, binding.BindingID, binding.DriverGeneration))
		}
		requireID(t, batch.validateStructure(false), BoundsExceeded)
	})
}

func TestPublicationCascadeStagingResource(t *testing.T) {
	binding := publicationBinding("asset:site", "source:a", "epoch:a", "binding:a", "1")
	bindings := map[NativeBindingID]NativeBinding{binding.BindingID: binding}
	var previous uint64
	for _, finalNodes := range []int{256, 1024, maxDerivationNodes} {
		t.Run(fmt.Sprint(finalNodes), func(t *testing.T) {
			missing := publicationCandidate("candidate:withdrawn", "fact.root", true, binding.SourceID, binding.SourceEpochID, binding.BindingID, binding.DriverGeneration)
			candidates := make(map[CandidateID]FactCandidate, 2*finalNodes-1)
			upserted := make(map[CandidateID]struct{}, finalNodes)
			for i := 0; i < finalNodes-1; i++ {
				id := CandidateID(fmt.Sprintf("candidate:retained%04d", i))
				candidates[id] = publicationDerivedCandidate(id, "fact.retained", []FactCandidate{missing})
			}
			for i := 0; i < finalNodes; i++ {
				id := CandidateID(fmt.Sprintf("candidate:new%04d", i))
				candidates[id] = publicationCandidate(id, "fact.new", true, binding.SourceID, binding.SourceEpochID, binding.BindingID, binding.DriverGeneration)
				upserted[id] = struct{}{}
			}
			transient := len(candidates)
			if transient > maxStagedDerivationNodes {
				t.Fatalf("transient graph %d exceeds staging bound %d", transient, maxStagedDerivationNodes)
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			if err := closeCandidateGraph(candidates, upserted, bindings); err != nil {
				t.Fatal(err)
			}
			runtime.ReadMemStats(&after)
			if len(candidates) != finalNodes {
				t.Fatalf("final graph nodes=%d want=%d", len(candidates), finalNodes)
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("transient_nodes=%d final_nodes=%d allocated=%d allocations=%d", transient, len(candidates), allocated, after.Mallocs-before.Mallocs)
			if allocated > 64<<20 || previous != 0 && allocated > 5*previous+(2<<20) {
				t.Fatal("cascade staging exceeded proportional allocation bound")
			}
			previous = allocated
		})
	}
}
