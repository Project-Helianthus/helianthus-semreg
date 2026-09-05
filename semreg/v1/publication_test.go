package semreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

var publicationMonotonic = MonotonicPoint{ClockEpochID: "clock-epoch:publication", Nanoseconds: "100"}

func TestPublicationFixturesArePinned(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "v1", "publication-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Contract string `json:"contract"`
		Pin      string `json:"documentation_pin"`
		Vectors  []struct {
			ID string `json:"id"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Contract != "helianthus.semantic.kernel.publication-fixtures/v1" || fixture.Pin != "b16667d719defc7b0fef0400ee3ad387469018ac" {
		t.Fatalf("fixture metadata drifted: %+v", fixture)
	}
	want := []string{"K-PUB-001", "K-PUB-002", "K-PUB-003", "K-PUB-004", "K-PUB-005", "K-PUB-006", "K-PUB-007", "K-PUB-008", "K-PUB-009", "K-PUB-010", "K-PUB-011", "K-PUB-012"}
	if len(fixture.Vectors) != len(want) {
		t.Fatalf("got %d publication fixtures", len(fixture.Vectors))
	}
	for i := range want {
		if fixture.Vectors[i].ID != want[i] {
			t.Fatalf("fixture %d is %q, want %q", i, fixture.Vectors[i].ID, want[i])
		}
	}
}

func TestPublicationInitialPatchWithdrawalAndReplay(t *testing.T) {
	kernel := newTestPublicationKernel(t, "asset:site")
	initial := publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
	initial.SourceUpserts = []SourceDescriptor{publicationSource("source:a", "epoch:a")}
	initial.BindingUpserts = []NativeBinding{publicationBinding("asset:site", "source:a", "epoch:a", "binding:a", "1")}
	initial.IdentityLinkUpserts = []IdentityLink{publicationLink("asset:site", "binding:a")}
	initial.FactUpserts = []FactCandidate{
		publicationCandidate("candidate:a:power", "fact.power", true, "source:a", "epoch:a", "binding:a", "1"),
		publicationCandidate("candidate:a:yield", "fact.yield", true, "source:a", "epoch:a", "binding:a", "1"),
	}
	initial.ServiceUpserts = []ServiceInstance{publicationService("asset:site", "service:a", "binding:a", "epoch:a", "1")}
	initial.CapabilityUpserts = []CapabilityInstance{publicationCapability("asset:site", "capability:a", "service:a", "binding:a", "epoch:a", "1")}
	sealPublicationBatch(t, &initial)

	snapshot1, bytes1, err := kernel.Apply(initial, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot1.Revisions != (RevisionVector{Semantic: "1", Identity: "1", Facts: "1", Services: "1", Capabilities: "1"}) {
		t.Fatalf("initial revisions: %+v", snapshot1.Revisions)
	}
	if len(snapshot1.Facts) != 2 || snapshot1.SnapshotID == "" || len(bytes1) == 0 {
		t.Fatalf("incomplete initial snapshot: %+v", snapshot1)
	}

	partial := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	changed := publicationCandidate("candidate:a:power", "fact.power", false, "source:a", "epoch:a", "binding:a", "1")
	changed.Revision = "2"
	partial.FactUpserts = []FactCandidate{changed}
	sealPublicationBatch(t, &partial)
	snapshot2, _, err := kernel.Apply(partial, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot2.Revisions.Semantic != "2" || snapshot2.Revisions.Facts != "2" || snapshot2.Revisions.Identity != "1" || len(snapshot2.Facts) != 2 {
		t.Fatalf("partial patch did not retain state/revisions: %+v", snapshot2.Revisions)
	}
	if candidateByID(t, snapshot2, "candidate:a:yield").Revision != "1" {
		t.Fatal("unrelated candidate changed")
	}

	replay, replayBytes, err := kernel.Apply(partial, publicationMonotonic)
	if err != nil || replay.SnapshotID != snapshot2.SnapshotID || !bytes.Equal(replayBytes, mustCurrentBytes(t, kernel)) {
		t.Fatalf("replay changed result: %v", err)
	}
	mutatedReplay := partial
	mutatedReplay.BatchID = "batch:mutated-replay"
	assertRejectedUnchanged(t, kernel, mutatedReplay, SequenceConflict)

	conflict := partial
	conflict.BatchID = "batch:conflict"
	conflict.ObservedAt.UnixNanoseconds = "202"
	sealPublicationBatch(t, &conflict)
	assertRejectedUnchanged(t, kernel, conflict, SequenceConflict)

	badDigest := publicationBatch("asset:site", "source:a", "epoch:a", "1", "3", "2")
	badDigest.BatchDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	assertRejectedUnchanged(t, kernel, badDigest, DigestMismatch)

	withdraw := publicationBatch("asset:site", "source:a", "epoch:a", "1", "3", "2")
	withdraw.FactWithdrawals = []CandidateID{"candidate:a:power"}
	sealPublicationBatch(t, &withdraw)
	snapshot3, _, err := kernel.Apply(withdraw, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot3.Facts) != 1 || candidateByID(t, snapshot3, "candidate:a:yield").CandidateID != "candidate:a:yield" || snapshot3.Revisions.Facts != "3" {
		t.Fatalf("withdrawal result: %+v", snapshot3.Facts)
	}

	withdrawRuntime := publicationBatch("asset:site", "source:a", "epoch:a", "1", "4", "3")
	withdrawRuntime.ServiceWithdrawals = []ServiceInstanceID{"service:a"}
	withdrawRuntime.CapabilityWithdrawals = []CapabilityInstanceID{"capability:a"}
	sealPublicationBatch(t, &withdrawRuntime)
	snapshot4, _, err := kernel.Apply(withdrawRuntime, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if serviceByID(t, snapshot4, "service:a").Availability != AvailabilityWithdrawn || capabilityByID(t, snapshot4, "capability:a").Availability != AvailabilityWithdrawn || snapshot4.Revisions.Services != "2" || snapshot4.Revisions.Capabilities != "2" {
		t.Fatalf("explicit runtime withdrawals: %+v", snapshot4.Revisions)
	}

	stale := publicationBatch("asset:site", "source:a", "epoch:a", "1", "5", "3")
	sealPublicationBatch(t, &stale)
	assertRejectedUnchanged(t, kernel, stale, RevisionConflict)
}

func TestPublicationConflictReconciliationAndDerivedCascade(t *testing.T) {
	kernel := newTestPublicationKernel(t, "asset:site")
	a := publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
	a.SourceUpserts = []SourceDescriptor{publicationSource("source:a", "epoch:a")}
	a.BindingUpserts = []NativeBinding{publicationBinding("asset:site", "source:a", "epoch:a", "binding:a", "1")}
	a.FactUpserts = []FactCandidate{publicationCandidate("candidate:a", "fact.power", true, "source:a", "epoch:a", "binding:a", "1")}
	sealPublicationBatch(t, &a)
	if _, _, err := kernel.Apply(a, publicationMonotonic); err != nil {
		t.Fatal(err)
	}

	b := publicationBatch("asset:site", "source:b", "epoch:b", "1", "1", "1")
	b.SourceUpserts = []SourceDescriptor{publicationSource("source:b", "epoch:b")}
	b.BindingUpserts = []NativeBinding{publicationBinding("asset:site", "source:b", "epoch:b", "binding:b", "1")}
	observedB := publicationCandidate("candidate:b", "fact.power", false, "source:b", "epoch:b", "binding:b", "1")
	derived := publicationDerivedCandidate("candidate:derived", "fact.net", []FactCandidate{a.FactUpserts[0], observedB})
	dependent := publicationDerivedCandidate("candidate:dependent", "fact.flex", []FactCandidate{derived})
	b.FactUpserts = []FactCandidate{observedB, dependent, derived}
	sort.Slice(b.FactUpserts, func(i, j int) bool { return b.FactUpserts[i].CandidateID < b.FactUpserts[j].CandidateID })
	sealPublicationBatch(t, &b)
	snapshot, _, err := kernel.Apply(b, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if len(envelopeByFact(t, snapshot, "fact.power").Conflicts) != 1 {
		t.Fatal("qualified disagreement did not create conflict")
	}
	replayedA, _, err := kernel.Apply(a, publicationMonotonic)
	if err != nil || replayedA.Revisions.Semantic != "1" {
		t.Fatalf("cross-source replay did not return its prior snapshot: revision=%s err=%v", replayedA.Revisions.Semantic, err)
	}
	current, _, ok := kernel.Current()
	if !ok || current.Revisions.Semantic != "2" {
		t.Fatal("historical replay replaced current snapshot")
	}

	fence := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "2")
	fence.GenerationFences = []GenerationFence{{SourceID: "source:a", SourceEpochID: "epoch:a", DriverGeneration: "1", Reason: "lifecycle.driver_replaced", Evidence: []EvidenceRef{publicEvidence("a")}, Revision: "1"}}
	sealPublicationBatch(t, &fence)
	result, _, err := kernel.Apply(fence, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if hasCandidate(result, "candidate:a") || hasCandidate(result, "candidate:derived") || hasCandidate(result, "candidate:dependent") {
		t.Fatalf("dependency closure incomplete: %+v", result.Facts)
	}
	power := envelopeByFact(t, result, "fact.power")
	if len(power.Candidates) != 1 || len(power.Conflicts) != 0 || power.Revision != "3" {
		t.Fatalf("conflict was not reconciled exactly once: %+v", power)
	}
	if result.Revisions.Facts != "3" || result.Revisions.Semantic != "3" {
		t.Fatalf("cascade revisions: %+v", result.Revisions)
	}

	dangling := publicationBatch("asset:site", "source:b", "epoch:b", "1", "2", "3")
	bad := publicationDerivedCandidate("candidate:bad", "fact.bad", []FactCandidate{observedB})
	bad.Derivation.Inputs[0].CandidateID = "candidate:missing"
	dangling.FactUpserts = []FactCandidate{bad}
	sealPublicationBatch(t, &dangling)
	assertRejectedUnchanged(t, kernel, dangling, DanglingReference)
}

func TestPublicationCandidateRevisionCascadesDependents(t *testing.T) {
	kernel := newTestPublicationKernel(t, "asset:site")
	initial := publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
	initial.SourceUpserts = []SourceDescriptor{publicationSource("source:a", "epoch:a")}
	initial.BindingUpserts = []NativeBinding{publicationBinding("asset:site", "source:a", "epoch:a", "binding:a", "1")}
	observed := publicationCandidate("candidate:observed", "fact.power", true, "source:a", "epoch:a", "binding:a", "1")
	derived := publicationDerivedCandidate("candidate:derived", "fact.net", []FactCandidate{observed})
	initial.FactUpserts = []FactCandidate{derived, observed}
	sealPublicationBatch(t, &initial)
	if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	update := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	observed.Value = pointerRecord(booleanValue(false))
	observed.Revision = "2"
	update.FactUpserts = []FactCandidate{observed}
	sealPublicationBatch(t, &update)
	result, _, err := kernel.Apply(update, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if hasCandidate(result, "candidate:derived") || candidateByID(t, result, "candidate:observed").Revision != "2" || result.Revisions.Facts != "2" {
		t.Fatalf("candidate revision cascade failed: %+v", result.Facts)
	}
}

func TestPublicationConcurrentReadersSeeWholeSnapshots(t *testing.T) {
	kernel := newTestPublicationKernel(t, "asset:site")
	initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	sealPublicationBatch(t, &initial)
	if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	update := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	changed := initial.FactUpserts[0]
	changed.Value = pointerRecord(booleanValue(false))
	changed.Revision = "2"
	update.FactUpserts = []FactCandidate{changed}
	sealPublicationBatch(t, &update)

	start := make(chan struct{})
	errs := make(chan error, 8)
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for j := 0; j < 50; j++ {
				snapshot, _, ok := kernel.Current()
				if !ok {
					errs <- fmt.Errorf("reader observed no snapshot")
					return
				}
				candidate := candidateByIDNoTest(snapshot, "candidate:binding:a")
				if candidate == nil || snapshot.Revisions.Semantic == "1" && candidate.Revision != "1" || snapshot.Revisions.Semantic == "2" && candidate.Revision != "2" || snapshot.Revisions.Semantic != "1" && snapshot.Revisions.Semantic != "2" {
					errs <- fmt.Errorf("mixed snapshot semantic=%s candidate=%v", snapshot.Revisions.Semantic, candidate)
					return
				}
			}
		}()
	}
	close(start)
	if _, _, err := kernel.Apply(update, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	readers.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestPublicationRetirementAndGenerationSupersession(t *testing.T) {
	t.Run("source-retirement", func(t *testing.T) {
		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:old", "binding:old", "1", "1", "0")
		sealPublicationBatch(t, &initial)
		if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
		restart := publicationBatch("asset:site", "source:a", "epoch:new", "1", "1", "1")
		restart.SourceRetirements = []SourceEpochID{"epoch:old"}
		restart.SourceUpserts = []SourceDescriptor{publicationSource("source:a", "epoch:new")}
		sealPublicationBatch(t, &restart)
		result, _, err := kernel.Apply(restart, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		if sourceByEpoch(t, result, "epoch:old").State != SourceRetired || bindingByID(t, result, "binding:old").State != BindingRetired {
			t.Fatal("retirement tombstones missing")
		}
		if linkByBinding(t, result, "binding:old").State != LinkWithdrawn || serviceByID(t, result, "service:binding:old").Availability != AvailabilityWithdrawn || capabilityByID(t, result, "capability:binding:old").Availability != AvailabilityWithdrawn {
			t.Fatal("retirement dependents remain actionable")
		}
		if hasCandidate(result, "candidate:binding:old") {
			t.Fatal("retired observation remained current")
		}
	})

	t.Run("generation-fence", func(t *testing.T) {
		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:g1", "1", "1", "0")
		sealPublicationBatch(t, &initial)
		if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
		missingFence := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:g2", "2", "1", "1")
		missingFence.SourceUpserts = []SourceDescriptor{}
		sealPublicationBatch(t, &missingFence)
		assertRejectedUnchanged(t, kernel, missingFence, GenerationTransitionIncomplete)

		supersede := missingFence
		supersede.GenerationFences = []GenerationFence{{SourceID: "source:a", SourceEpochID: "epoch:a", DriverGeneration: "1", Reason: "lifecycle.driver_replaced", Evidence: []EvidenceRef{publicEvidence("b")}, Revision: "1"}}
		sealPublicationBatch(t, &supersede)
		result, _, err := kernel.Apply(supersede, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		if bindingByID(t, result, "binding:g1").State != BindingFenced || bindingByID(t, result, "binding:g2").State != BindingCurrent || !cursorFor(t, result, "source:a", "epoch:a", "1").Fenced {
			t.Fatalf("generation fence state is incomplete: %+v", result)
		}
		if linkByBinding(t, result, "binding:g1").State != LinkWithdrawn || serviceByID(t, result, "service:binding:g1").Availability != AvailabilityWithdrawn || capabilityByID(t, result, "capability:binding:g1").Availability != AvailabilityWithdrawn {
			t.Fatal("fenced generation remains actionable")
		}
		stale := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "2")
		sealPublicationBatch(t, &stale)
		assertRejectedUnchanged(t, kernel, stale, StaleDriverGeneration)
	})
}

func TestPublicationBoundsCanonicalReferencesAndCallerIsolation(t *testing.T) {
	sequenceKernel := newTestPublicationKernel(t, "asset:sequence")
	badFirstSequence := publicationBatch("asset:sequence", "source:a", "epoch:a", "1", "2", "0")
	badFirstSequence.SourceUpserts = []SourceDescriptor{publicationSource("source:a", "epoch:a")}
	sealPublicationBatch(t, &badFirstSequence)
	assertRejectedUnchanged(t, sequenceKernel, badFirstSequence, SequenceConflict)

	kernel := newTestPublicationKernel(t, "asset:site")
	initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	sealPublicationBatch(t, &initial)
	inputCandidate := &initial.FactUpserts[0]
	result, acceptedBytes, err := kernel.Apply(initial, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	*inputCandidate.Value.Boolean = false
	result.Facts[0].Candidates[0].Evidence[0].Owner = "owner.mutated"
	current, currentBytes, ok := kernel.Current()
	currentValue := *candidateByID(t, current, "candidate:binding:a").Value.Boolean
	if !ok || !bytes.Equal(acceptedBytes, currentBytes) || currentValue != true {
		t.Fatalf("caller mutation changed published state: ok=%v bytes_equal=%v value=%v", ok, bytes.Equal(acceptedBytes, currentBytes), currentValue)
	}

	over := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	for i := 0; i < 128; i++ {
		over.BindingUpserts = append(over.BindingUpserts, publicationBinding("asset:site", "source:a", "epoch:a", NativeBindingID(fmt.Sprintf("binding:z%03d", i)), "1"))
	}
	sealPublicationBatch(t, &over)
	assertRejectedUnchanged(t, kernel, over, BoundsExceeded)

	broken := current
	broken.Bindings = broken.Bindings[:0]
	recomputeSnapshotID(t, &broken)
	requireID(t, broken.Validate(), DanglingReference)

	badOrder := initial
	badOrder.FactUpserts = []FactCandidate{
		publicationCandidate("candidate:z", "fact.power", true, "source:a", "epoch:a", "binding:a", "1"),
		publicationCandidate("candidate:a", "fact.power", true, "source:a", "epoch:a", "binding:a", "1"),
	}
	_, err = badOrder.ComputedDigest()
	requireID(t, err, NoncanonicalOrder)
	badOrder.FactUpserts[1] = badOrder.FactUpserts[0]
	_, err = badOrder.ComputedDigest()
	requireID(t, err, DuplicateKey)
}

func TestPublicationTransitionMatrixCompositionAndWithdrawals(t *testing.T) {
	t.Run("source-upsert-and-retirement-compose-once", func(t *testing.T) {
		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
		sealPublicationBatch(t, &initial)
		if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
		restart := publicationBatch("asset:site", "source:a", "epoch:new", "1", "1", "1")
		changed := initial.SourceUpserts[0]
		changed.ProfileVersion = "2.0.0"
		changed.Revision = "2"
		restart.SourceUpserts = []SourceDescriptor{changed, publicationSource("source:a", "epoch:new")}
		restart.SourceRetirements = []SourceEpochID{"epoch:a"}
		sealPublicationBatch(t, &restart)
		result, _, err := kernel.Apply(restart, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		retired := sourceByEpoch(t, result, "epoch:a")
		if retired.State != SourceRetired || retired.ProfileVersion != "2.0.0" || retired.Revision != "2" {
			t.Fatalf("source mutations did not compose once: %+v", retired)
		}
	})

	t.Run("service-capability-upsert-and-withdrawal-compose-once", func(t *testing.T) {
		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
		sealPublicationBatch(t, &initial)
		before, _, err := kernel.Apply(initial, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		update := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
		service := initial.ServiceUpserts[0]
		service.Availability, service.Revision = AvailabilityUnavailable, "2"
		capability := initial.CapabilityUpserts[0]
		capability.Availability, capability.Revision = AvailabilityUnavailable, "2"
		update.ServiceUpserts = []ServiceInstance{service}
		update.ServiceWithdrawals = []ServiceInstanceID{service.InstanceID}
		update.CapabilityUpserts = []CapabilityInstance{capability}
		update.CapabilityWithdrawals = []CapabilityInstanceID{capability.InstanceID}
		sealPublicationBatch(t, &update)
		result, _, err := kernel.Apply(update, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		if got := serviceByID(t, result, service.InstanceID); got.Availability != AvailabilityWithdrawn || got.Revision != "2" {
			t.Fatalf("service mutations did not compose once: %+v", got)
		}
		if got := capabilityByID(t, result, capability.InstanceID); got.Availability != AvailabilityWithdrawn || got.Revision != "2" {
			t.Fatalf("capability mutations did not compose once: %+v", got)
		}
		if result.Revisions.Identity != before.Revisions.Identity || result.Revisions.Facts != before.Revisions.Facts || bindingByID(t, result, "binding:a").Revision != "1" || candidateByID(t, result, "candidate:binding:a").Revision != "1" {
			t.Fatalf("unchanged objects or components changed: before=%+v after=%+v", before.Revisions, result.Revisions)
		}
	})

	for _, kind := range []string{"fact", "service", "capability"} {
		t.Run("covered-fence-with-redundant-"+kind+"-withdrawal", func(t *testing.T) {
			kernel := newTestPublicationKernel(t, "asset:site")
			initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
			sealPublicationBatch(t, &initial)
			if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
				t.Fatal(err)
			}
			next := publicationBatch("asset:site", "source:a", "epoch:a", "2", "1", "1")
			next.GenerationFences = []GenerationFence{publicationFence("source:a", "epoch:a", "1", publicEvidence("b"))}
			switch kind {
			case "fact":
				next.BindingUpserts = []NativeBinding{publicationBinding("asset:site", "source:a", "epoch:a", "binding:new", "2")}
				next.FactUpserts = []FactCandidate{publicationCandidate("candidate:new", "fact.power", false, "source:a", "epoch:a", "binding:new", "2")}
				next.FactWithdrawals = []CandidateID{initial.FactUpserts[0].CandidateID}
			case "service":
				next.ServiceWithdrawals = []ServiceInstanceID{initial.ServiceUpserts[0].InstanceID}
			case "capability":
				next.CapabilityWithdrawals = []CapabilityInstanceID{initial.CapabilityUpserts[0].InstanceID}
			}
			sealPublicationBatch(t, &next)
			result, _, err := kernel.Apply(next, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if bindingByID(t, result, "binding:a").Revision != "2" || linkByBinding(t, result, "binding:a").Revision != "2" || serviceByID(t, result, "service:binding:a").Revision != "2" || capabilityByID(t, result, "capability:binding:a").Revision != "2" {
				t.Fatalf("overlapping transition changed an object more than once: %+v", result)
			}
			if kind == "fact" && (!hasCandidate(result, "candidate:new") || hasCandidate(result, initial.FactUpserts[0].CandidateID)) {
				t.Fatalf("fact upsert and covered lifecycle withdrawal did not compose: %+v", result.Facts)
			}
		})
	}

	t.Run("covered-retirement-with-redundant-withdrawals", func(t *testing.T) {
		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
		sealPublicationBatch(t, &initial)
		if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
		restart := publicationBatch("asset:site", "source:a", "epoch:new", "1", "1", "1")
		restart.SourceUpserts = []SourceDescriptor{publicationSource("source:a", "epoch:new")}
		restart.SourceRetirements = []SourceEpochID{"epoch:a"}
		restart.FactWithdrawals = []CandidateID{initial.FactUpserts[0].CandidateID}
		restart.ServiceWithdrawals = []ServiceInstanceID{initial.ServiceUpserts[0].InstanceID}
		restart.CapabilityWithdrawals = []CapabilityInstanceID{initial.CapabilityUpserts[0].InstanceID}
		sealPublicationBatch(t, &restart)
		result, _, err := kernel.Apply(restart, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		if sourceByEpoch(t, result, "epoch:a").State != SourceRetired || serviceByID(t, result, initial.ServiceUpserts[0].InstanceID).Revision != "2" || capabilityByID(t, result, initial.CapabilityUpserts[0].InstanceID).Revision != "2" {
			t.Fatalf("retirement withdrawals did not compose with tombstones: %+v", result)
		}
	})

	t.Run("unrelated-and-uncovered-withdrawals-reject", func(t *testing.T) {
		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
		sealPublicationBatch(t, &initial)
		if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
		unrelated := publicationBatch("asset:site", "source:b", "epoch:b", "1", "1", "1")
		unrelated.SourceUpserts = []SourceDescriptor{publicationSource("source:b", "epoch:b")}
		unrelated.FactWithdrawals = []CandidateID{initial.FactUpserts[0].CandidateID}
		sealPublicationBatch(t, &unrelated)
		assertRejectedUnchanged(t, kernel, unrelated, InvalidValue)

		supersede := publicationBatch("asset:site", "source:a", "epoch:a", "2", "1", "1")
		supersede.GenerationFences = []GenerationFence{publicationFence("source:a", "epoch:a", "1", publicEvidence("b"))}
		sealPublicationBatch(t, &supersede)
		if _, _, err := kernel.Apply(supersede, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
		uncovered := publicationBatch("asset:site", "source:a", "epoch:a", "2", "2", "2")
		uncovered.ServiceWithdrawals = []ServiceInstanceID{initial.ServiceUpserts[0].InstanceID}
		sealPublicationBatch(t, &uncovered)
		assertRejectedUnchanged(t, kernel, uncovered, InvalidValue)
	})
}

func TestPublicationTransitionMatrixEvidenceAndSnapshotCompleteness(t *testing.T) {
	t.Run("automatic-basis-union-is-canonical", func(t *testing.T) {
		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
		initial.IdentityLinkUpserts[0].Basis = []EvidenceRef{publicEvidence("3")}
		sealPublicationBatch(t, &initial)
		if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
		next := publicationBatch("asset:site", "source:a", "epoch:a", "2", "1", "1")
		next.GenerationFences = []GenerationFence{publicationFence("source:a", "epoch:a", "1", publicEvidence("2"), publicEvidence("3"))}
		sealPublicationBatch(t, &next)
		result, _, err := kernel.Apply(next, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		basis := linkByBinding(t, result, "binding:a").Basis
		if len(basis) != 2 || basis[0].Digest != publicEvidence("2").Digest || basis[1].Digest != publicEvidence("3").Digest {
			t.Fatalf("transition basis is not the canonical union: %+v", basis)
		}
	})

	t.Run("automatic-basis-union-does-not-truncate", func(t *testing.T) {
		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
		initial.IdentityLinkUpserts[0].Basis = publicationEvidenceRange(1, 32)
		sealPublicationBatch(t, &initial)
		if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
		next := publicationBatch("asset:site", "source:a", "epoch:a", "2", "1", "1")
		next.GenerationFences = []GenerationFence{publicationFence("source:a", "epoch:a", "1", publicationEvidenceRange(33, 33)...)}
		sealPublicationBatch(t, &next)
		assertRejectedUnchanged(t, kernel, next, BoundsExceeded)
	})

	t.Run("retained-fence-tombstone-requires-cursor", func(t *testing.T) {
		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
		sealPublicationBatch(t, &initial)
		if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
		next := publicationBatch("asset:site", "source:a", "epoch:a", "2", "1", "1")
		next.GenerationFences = []GenerationFence{publicationFence("source:a", "epoch:a", "1", publicEvidence("b"))}
		sealPublicationBatch(t, &next)
		result, _, err := kernel.Apply(next, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		if bindingByID(t, result, "binding:a").State != BindingFenced {
			t.Fatal("test setup did not retain the fenced binding tombstone")
		}
		for i, cursor := range result.Cursors {
			if cursor.DriverGeneration == "1" {
				result.Cursors = append(result.Cursors[:i], result.Cursors[i+1:]...)
				break
			}
		}
		recomputeSnapshotID(t, &result)
		requireID(t, result.Validate(), DanglingReference)
	})
}

func TestPublicationTransitionMatrixSharedDAGAndConcurrentReader(t *testing.T) {
	kernel := newTestPublicationKernel(t, "asset:site")
	initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	sealPublicationBatch(t, &initial)
	if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	batch := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	root := initial.FactUpserts[0]
	prior := []FactCandidate{root}
	path := candidateSourcePaths(root)
	for layer := 1; layer <= 29; layer++ {
		var next []FactCandidate
		for column := 0; column < 2; column++ {
			candidate := publicationDerivedCandidate(CandidateID(fmt.Sprintf("candidate:layer%02d-%d", layer, column)), DefinitionID(fmt.Sprintf("fact.layer%02d-%d", layer, column)), prior)
			for i := range candidate.Derivation.Inputs {
				candidate.Derivation.Inputs[i].SourcePaths = append([]SourcePathRef(nil), path...)
			}
			next = append(next, candidate)
			batch.FactUpserts = append(batch.FactUpserts, candidate)
		}
		prior = next
	}
	sealPublicationBatch(t, &batch)
	type applyResult struct {
		snapshot Snapshot
		err      error
	}
	start := make(chan struct{})
	applied := make(chan applyResult, 1)
	read := make(chan Snapshot, 1)
	go func() {
		<-start
		snapshot, _, err := kernel.Apply(batch, publicationMonotonic)
		applied <- applyResult{snapshot: snapshot, err: err}
	}()
	go func() {
		<-start
		snapshot, _, _ := kernel.Current()
		read <- snapshot
	}()
	started := time.Now()
	close(start)
	var result Snapshot
	// This is only a runaway/deadlock guard for the bounded regression, not a
	// product latency requirement.
	select {
	case outcome := <-applied:
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		result = outcome.snapshot
	case <-time.After(30 * time.Second):
		t.Fatal("bounded shared DAG did not complete")
	}
	var observed Snapshot
	select {
	case observed = <-read:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent reader did not complete")
	}
	if len(result.Facts) != 59 || result.Revisions.Semantic != "2" {
		t.Fatalf("shared DAG snapshot incomplete: facts=%d revisions=%+v", len(result.Facts), result.Revisions)
	}
	if observed.Revisions.Semantic != "1" && observed.Revisions.Semantic != "2" {
		t.Fatalf("reader saw a mixed semantic revision: %+v", observed.Revisions)
	}
	t.Logf("accepted 59-node depth-30 shared DAG with concurrent reader in %s", time.Since(started))
}

func TestPublicationTransitionMatrixDuplicatePrecedence(t *testing.T) {
	sourceA, sourceB := publicationSource("source:a", "epoch:a"), publicationSource("source:a", "epoch:b")
	bindingA, bindingB := publicationBinding("asset:site", "source:a", "epoch:a", "binding:a", "1"), publicationBinding("asset:site", "source:a", "epoch:a", "binding:b", "1")
	linkA, linkB := publicationLink("asset:site", "binding:a"), publicationLink("asset:site", "binding:b")
	candidateA, candidateB := publicationCandidate("candidate:a", "fact.a", true, "source:a", "epoch:a", "binding:a", "1"), publicationCandidate("candidate:b", "fact.b", true, "source:a", "epoch:a", "binding:b", "1")
	serviceA, serviceB := publicationService("asset:site", "service:a", "binding:a", "epoch:a", "1"), publicationService("asset:site", "service:b", "binding:b", "epoch:a", "1")
	capabilityA, capabilityB := publicationCapability("asset:site", "capability:a", "service:a", "binding:a", "epoch:a", "1"), publicationCapability("asset:site", "capability:b", "service:b", "binding:b", "epoch:a", "1")
	fenceA, fenceB := publicationFence("source:a", "epoch:a", "1", publicEvidence("1")), publicationFence("source:a", "epoch:a", "2", publicEvidence("2"))
	cursorA := PublicationCursor{SourceID: "source:a", SourceEpochID: "epoch:a", DriverGeneration: "1", LastSequence: "1", LastBatchDigest: Digest("sha256:" + fmt.Sprintf("%064x", 1)), Fenced: true}
	cursorB := cursorA
	cursorB.DriverGeneration = "2"
	envelopeA := FactEnvelope{AssetID: "asset:site", Key: candidateA.Key, Candidates: []FactCandidate{candidateA}, Conflicts: []Conflict{}, Revision: "1"}
	envelopeB := FactEnvelope{AssetID: "asset:site", Key: candidateB.Key, Candidates: []FactCandidate{candidateB}, Conflicts: []Conflict{}, Revision: "1"}

	cases := map[string]func() error{
		"sources": func() error { return orderedUnique([]SourceDescriptor{sourceB, sourceA, sourceB}, compareSource) },
		"source-retirements": func() error {
			return orderedUnique([]SourceEpochID{"epoch:b", "epoch:a", "epoch:b"}, func(a, z SourceEpochID) int { return strings.Compare(string(a), string(z)) })
		},
		"bindings": func() error {
			return orderedUnique([]NativeBinding{bindingB, bindingA, bindingB}, func(a, z NativeBinding) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) })
		},
		"identity-links": func() error {
			return orderedUnique([]IdentityLink{linkB, linkA, linkB}, func(a, z IdentityLink) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) })
		},
		"fact-upserts": func() error {
			return orderedUnique([]FactCandidate{candidateB, candidateA, candidateB}, func(a, z FactCandidate) int { return strings.Compare(string(a.CandidateID), string(z.CandidateID)) })
		},
		"fact-withdrawals": func() error {
			return orderedUnique([]CandidateID{"candidate:b", "candidate:a", "candidate:b"}, func(a, z CandidateID) int { return strings.Compare(string(a), string(z)) })
		},
		"services": func() error {
			return orderedUnique([]ServiceInstance{serviceB, serviceA, serviceB}, func(a, z ServiceInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) })
		},
		"service-withdrawals": func() error {
			return orderedUnique([]ServiceInstanceID{"service:b", "service:a", "service:b"}, func(a, z ServiceInstanceID) int { return strings.Compare(string(a), string(z)) })
		},
		"capabilities": func() error {
			return orderedUnique([]CapabilityInstance{capabilityB, capabilityA, capabilityB}, func(a, z CapabilityInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) })
		},
		"capability-withdrawals": func() error {
			return orderedUnique([]CapabilityInstanceID{"capability:b", "capability:a", "capability:b"}, func(a, z CapabilityInstanceID) int { return strings.Compare(string(a), string(z)) })
		},
		"fences":         func() error { return orderedUnique([]GenerationFence{fenceB, fenceA, fenceB}, compareFence) },
		"fact-envelopes": func() error { return orderedUnique([]FactEnvelope{envelopeB, envelopeA, envelopeB}, compareEnvelope) },
		"cursors":        func() error { return orderedUnique([]PublicationCursor{cursorB, cursorA, cursorB}, compareCursor) },
	}
	for name, check := range cases {
		t.Run(name, func(t *testing.T) { requireID(t, check(), DuplicateKey) })
	}

	t.Run("duplicate-precedes-order-and-invalid-member", func(t *testing.T) {
		batch := publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
		serviceB.Availability = Availability("invalid")
		batch.ServiceUpserts = []ServiceInstance{serviceB, serviceA, serviceB}
		_, err := batch.ComputedDigest()
		requireID(t, err, DuplicateKey)

		kernel := newTestPublicationKernel(t, "asset:site")
		initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
		sealPublicationBatch(t, &initial)
		snapshot, _, err := kernel.Apply(initial, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
		batch.Sequence, batch.ExpectedSemanticRevision, batch.BatchDigest = "2", "1", initial.BatchDigest
		assertRejectedUnchanged(t, kernel, batch, DuplicateKey)

		duplicateBinding := snapshot.Bindings[0]
		duplicateBinding.BindingID = "binding:b"
		duplicateBinding.State = BindingState("invalid")
		snapshot.Bindings = []NativeBinding{duplicateBinding, snapshot.Bindings[0], duplicateBinding}
		recomputeSnapshotID(t, &snapshot)
		requireID(t, snapshot.Validate(), DuplicateKey)
	})
}

func publicationBatch(asset AssetID, source SourceID, epoch SourceEpochID, generation, sequence, expected Uint64) PublicationBatch {
	return PublicationBatch{
		Contract: ContractKernelV1, BatchID: BatchID("batch:" + string(source) + ":" + string(epoch) + ":" + string(generation) + ":" + string(sequence)),
		AssetID: asset, SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, Sequence: sequence,
		ExpectedSemanticRevision: expected, ObservedAt: TimePoint{UnixNanoseconds: Int64("20" + string(sequence)), ClockID: "clock.utc", UncertaintyNS: "0"},
		SourceUpserts: []SourceDescriptor{}, SourceRetirements: []SourceEpochID{}, BindingUpserts: []NativeBinding{}, IdentityLinkUpserts: []IdentityLink{},
		FactUpserts: []FactCandidate{}, FactWithdrawals: []CandidateID{}, ServiceUpserts: []ServiceInstance{}, ServiceWithdrawals: []ServiceInstanceID{},
		CapabilityUpserts: []CapabilityInstance{}, CapabilityWithdrawals: []CapabilityInstanceID{}, GenerationFences: []GenerationFence{},
	}
}

func publicationFence(source SourceID, epoch SourceEpochID, generation Uint64, evidence ...EvidenceRef) GenerationFence {
	return GenerationFence{SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, Reason: "lifecycle.driver_replaced", Evidence: evidence, Revision: "1"}
}

func publicationEvidenceRange(first, last int) []EvidenceRef {
	result := make([]EvidenceRef, 0, last-first+1)
	for value := first; value <= last; value++ {
		evidence := publicEvidence("1")
		evidence.Digest = Digest("sha256:" + fmt.Sprintf("%064x", value))
		result = append(result, evidence)
	}
	return result
}

func completePublicationBatch(asset AssetID, source SourceID, epoch SourceEpochID, binding NativeBindingID, generation, sequence, expected Uint64) PublicationBatch {
	b := publicationBatch(asset, source, epoch, generation, sequence, expected)
	b.SourceUpserts = []SourceDescriptor{publicationSource(source, epoch)}
	b.BindingUpserts = []NativeBinding{publicationBinding(asset, source, epoch, binding, generation)}
	b.IdentityLinkUpserts = []IdentityLink{publicationLink(asset, binding)}
	b.FactUpserts = []FactCandidate{publicationCandidate(CandidateID("candidate:"+string(binding)), "fact.power", true, source, epoch, binding, generation)}
	b.ServiceUpserts = []ServiceInstance{publicationService(asset, ServiceInstanceID("service:"+string(binding)), binding, epoch, generation)}
	b.CapabilityUpserts = []CapabilityInstance{publicationCapability(asset, CapabilityInstanceID("capability:"+string(binding)), ServiceInstanceID("service:"+string(binding)), binding, epoch, generation)}
	return b
}

func publicationSource(source SourceID, epoch SourceEpochID) SourceDescriptor {
	s := validSource()
	s.SourceID, s.SourceEpochID = source, epoch
	return s
}

func publicationBinding(asset AssetID, source SourceID, epoch SourceEpochID, binding NativeBindingID, generation Uint64) NativeBinding {
	b := validBinding()
	b.AssetID, b.SourceID, b.SourceEpochID, b.BindingID, b.DriverGeneration = asset, source, epoch, binding, generation
	return b
}

func publicationLink(asset AssetID, binding NativeBindingID) IdentityLink {
	return IdentityLink{AssetID: asset, BindingID: binding, State: LinkQualified, Basis: []EvidenceRef{publicEvidence("3")}, Revision: "1"}
}

func publicationCandidate(id CandidateID, fact DefinitionID, value bool, source SourceID, epoch SourceEpochID, binding NativeBindingID, generation Uint64) FactCandidate {
	c := validCandidate("publication", value)
	c.CandidateID, c.Key.FactID, c.BindingID, c.SourceEpochID, c.DriverGeneration = id, fact, &binding, &epoch, &generation
	c.Origin = OriginRef{OriginID: OriginID("origin:" + string(id)), Kind: OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []EvidenceRef{publicEvidence("4")}}
	return c
}

func publicationDerivedCandidate(id CandidateID, fact DefinitionID, inputs []FactCandidate) FactCandidate {
	quality := validQuality()
	quality.Assertion = AssertionInferred
	evidence := publicEvidence("5")
	d := FactCandidate{CandidateID: id, Key: validKey(), Value: pointerRecord(booleanValue(true)), Quality: quality, Times: validTimes(), FreshnessPolicy: validPolicy(), Origin: OriginRef{OriginID: OriginID("origin:" + string(id)), Kind: OriginDerived, Evidence: []EvidenceRef{evidence}}, Evidence: []EvidenceRef{evidence}, Revision: "1"}
	d.Key.FactID = fact
	for _, input := range inputs {
		paths := candidateSourcePaths(input)
		d.Derivation = ensureDerivation(d.Derivation)
		d.Derivation.Inputs = append(d.Derivation.Inputs, DerivationInput{CandidateID: input.CandidateID, CandidateRevision: input.Revision, SourcePaths: paths})
	}
	sort.Slice(d.Derivation.Inputs, func(i, j int) bool { return d.Derivation.Inputs[i].CandidateID < d.Derivation.Inputs[j].CandidateID })
	return d
}

func ensureDerivation(d *Derivation) *Derivation {
	if d != nil {
		return d
	}
	return &Derivation{Algorithm: "algorithm.test", Version: "1.0.0", Inputs: []DerivationInput{}, Evidence: []EvidenceRef{}}
}

func candidateSourcePaths(candidate FactCandidate) []SourcePathRef {
	if candidate.Quality.Assertion == AssertionObserved {
		return []SourcePathRef{{BindingID: *candidate.BindingID, SourceID: *candidate.Origin.SourceID, SourceEpochID: *candidate.SourceEpochID, DriverGeneration: *candidate.DriverGeneration}}
	}
	var paths []SourcePathRef
	for _, input := range candidate.Derivation.Inputs {
		paths = append(paths, input.SourcePaths...)
	}
	sort.Slice(paths, func(i, j int) bool { return compareSourcePath(paths[i], paths[j]) < 0 })
	return paths
}

func publicationService(asset AssetID, id ServiceInstanceID, binding NativeBindingID, epoch SourceEpochID, generation Uint64) ServiceInstance {
	s := validService()
	s.InstanceID, s.AssetID, s.BindingID, s.SourceEpochID, s.DriverGeneration = id, asset, binding, epoch, generation
	return s
}

func publicationCapability(asset AssetID, id CapabilityInstanceID, service ServiceInstanceID, binding NativeBindingID, epoch SourceEpochID, generation Uint64) CapabilityInstance {
	c := validCapability()
	c.InstanceID, c.AssetID, c.ServiceInstance, c.BindingID, c.SourceEpochID, c.DriverGeneration = id, asset, service, binding, epoch, generation
	c.Definition = DefinitionRef{Pack: PackRef{ID: "pack.test", Version: "1.0.0"}, ID: "capability.test", Version: "1.0.0"}
	return c
}

func newTestPublicationKernel(t *testing.T, asset AssetID) *PublicationKernel {
	t.Helper()
	pack := PackRef{ID: "pack.test", Version: "1.0.0"}
	validator := &countingValidator{pack: pack, index: DefinitionIndex{Pack: pack, Fields: []DefinitionRef{}, Services: []DefinitionRef{{Pack: pack, ID: "service.test", Version: "1.0.0"}}, Capabilities: []DefinitionRef{{Pack: pack, ID: "capability.test", Version: "1.0.0"}}, Operations: []DefinitionRef{}, EffectRules: []DefinitionRef{}}}
	kernel, err := NewPublicationKernel(asset, validator)
	if err != nil {
		t.Fatal(err)
	}
	return kernel
}

func sealPublicationBatch(t *testing.T, batch *PublicationBatch) {
	t.Helper()
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
}

func assertRejectedUnchanged(t *testing.T, kernel *PublicationKernel, batch PublicationBatch, want ErrorID) {
	t.Helper()
	_, before, _ := kernel.Current()
	_, _, err := kernel.Apply(batch, publicationMonotonic)
	requireID(t, err, want)
	_, after, _ := kernel.Current()
	if !bytes.Equal(before, after) {
		t.Fatalf("%s rejection mutated state", want)
	}
}

func mustCurrentBytes(t *testing.T, kernel *PublicationKernel) []byte {
	t.Helper()
	_, raw, ok := kernel.Current()
	if !ok {
		t.Fatal("missing current snapshot")
	}
	return raw
}

func hasCandidate(snapshot Snapshot, id CandidateID) bool {
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			if candidate.CandidateID == id {
				return true
			}
		}
	}
	return false
}

func candidateByID(t *testing.T, snapshot Snapshot, id CandidateID) FactCandidate {
	t.Helper()
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			if candidate.CandidateID == id {
				return candidate
			}
		}
	}
	t.Fatalf("missing candidate %s", id)
	return FactCandidate{}
}

func candidateByIDNoTest(snapshot Snapshot, id CandidateID) *FactCandidate {
	for _, envelope := range snapshot.Facts {
		for i := range envelope.Candidates {
			if envelope.Candidates[i].CandidateID == id {
				candidate := envelope.Candidates[i]
				return &candidate
			}
		}
	}
	return nil
}

func envelopeByFact(t *testing.T, snapshot Snapshot, id DefinitionID) FactEnvelope {
	t.Helper()
	for _, envelope := range snapshot.Facts {
		if envelope.Key.FactID == id {
			return envelope
		}
	}
	t.Fatalf("missing fact %s", id)
	return FactEnvelope{}
}

func sourceByEpoch(t *testing.T, snapshot Snapshot, epoch SourceEpochID) SourceDescriptor {
	t.Helper()
	for _, source := range snapshot.Sources {
		if source.SourceEpochID == epoch {
			return source
		}
	}
	t.Fatalf("missing source epoch %s", epoch)
	return SourceDescriptor{}
}

func bindingByID(t *testing.T, snapshot Snapshot, id NativeBindingID) NativeBinding {
	t.Helper()
	for _, binding := range snapshot.Bindings {
		if binding.BindingID == id {
			return binding
		}
	}
	t.Fatalf("missing binding %s", id)
	return NativeBinding{}
}

func linkByBinding(t *testing.T, snapshot Snapshot, id NativeBindingID) IdentityLink {
	t.Helper()
	for _, link := range snapshot.IdentityLinks {
		if link.BindingID == id {
			return link
		}
	}
	t.Fatalf("missing link %s", id)
	return IdentityLink{}
}

func serviceByID(t *testing.T, snapshot Snapshot, id ServiceInstanceID) ServiceInstance {
	t.Helper()
	for _, service := range snapshot.Services {
		if service.InstanceID == id {
			return service
		}
	}
	t.Fatalf("missing service %s", id)
	return ServiceInstance{}
}

func capabilityByID(t *testing.T, snapshot Snapshot, id CapabilityInstanceID) CapabilityInstance {
	t.Helper()
	for _, capability := range snapshot.Capabilities {
		if capability.InstanceID == id {
			return capability
		}
	}
	t.Fatalf("missing capability %s", id)
	return CapabilityInstance{}
}

func cursorFor(t *testing.T, snapshot Snapshot, source SourceID, epoch SourceEpochID, generation Uint64) PublicationCursor {
	t.Helper()
	for _, cursor := range snapshot.Cursors {
		if cursor.SourceID == source && cursor.SourceEpochID == epoch && cursor.DriverGeneration == generation {
			return cursor
		}
	}
	t.Fatalf("missing cursor %s/%s/%s", source, epoch, generation)
	return PublicationCursor{}
}

func recomputeSnapshotID(t *testing.T, snapshot *Snapshot) {
	t.Helper()
	id, err := snapshot.computedID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.SnapshotID = id
}
