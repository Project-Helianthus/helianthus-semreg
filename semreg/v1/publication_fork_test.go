package semreg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

func TestPublicationKernelForkFixture(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "v1", "kernel-fork-acceptance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Contract  string `json:"contract"`
		Signature string `json:"signature"`
		Scenarios []struct {
			ID string `json:"id"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Contract != "helianthus.semantic.kernel.fork.acceptance/v1" || fixture.Signature != "func (k *PublicationKernel) Fork() (*PublicationKernel, error)" {
		t.Fatalf("fork fixture metadata drifted: %+v", fixture)
	}
	want := []string{"KF-POS-001", "KF-POS-002", "KF-NEG-001", "KF-POS-003", "KF-POS-004", "KF-NEG-002", "KF-POS-005"}
	if len(fixture.Scenarios) != len(want) {
		t.Fatalf("got %d fork scenarios, want %d", len(fixture.Scenarios), len(want))
	}
	for i, id := range want {
		if fixture.Scenarios[i].ID != id {
			t.Fatalf("scenario %d is %q, want %q", i, fixture.Scenarios[i].ID, id)
		}
	}
}

func TestPublicationKernelForkKFPos001002003(t *testing.T) {
	source, initial := publicationKernelAtSequence32(t)
	before, beforeRaw, ok := source.Current()
	if !ok {
		t.Fatal("source has no accepted sequence 32")
	}

	fork, err := source.Fork()
	if err != nil {
		t.Fatal(err)
	}
	forkCurrent, forkRaw, ok := fork.Current()
	if !ok || !bytes.Equal(beforeRaw, forkRaw) || !bytes.Equal(mustCanonical(t, before), mustCanonical(t, forkCurrent)) {
		t.Fatal("KF-POS-001 fork did not preserve the sequence-32 point")
	}

	// KF-POS-003: each kernel retains a detached accepted-tuple replay result.
	sourceReplay, sourceReplayRaw, err := source.Apply(initial, publicationMonotonic)
	if err != nil || !bytes.Equal(sourceReplayRaw, beforeRaw) || sourceReplay.Revisions != before.Revisions {
		t.Fatalf("source replay changed accepted point: %v", err)
	}
	forkReplay, forkReplayRaw, err := fork.Apply(initial, publicationMonotonic)
	if err != nil || !bytes.Equal(forkReplayRaw, beforeRaw) || !bytes.Equal(sourceReplayRaw, forkReplayRaw) || forkReplay.Revisions != before.Revisions {
		t.Fatalf("fork replay changed accepted point: %v", err)
	}

	// KF-POS-002: sequence 33 advances only the fork in the same tuple domain.
	next := publicationBatch("asset:site", "source:a", "epoch:a", "1", "33", publicationExpectedRevision(t, fork))
	candidate := candidateByID(t, before, "candidate:binding:a")
	candidate.Revision = "2"
	candidate.Value = pointerRecord(booleanValue(false))
	next.FactUpserts = []FactCandidate{candidate}
	sealPublicationBatch(t, &next)
	advanced, advancedRaw, err := fork.Apply(next, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.Revisions.Semantic == before.Revisions.Semantic || cursorFor(t, advanced, "source:a", "epoch:a", "1").LastSequence != "33" {
		t.Fatalf("fork sequence-33 continuation is not in the source tuple: %+v", advanced)
	}
	_, sourceRaw, _ := source.Current()
	if !bytes.Equal(sourceRaw, beforeRaw) || bytes.Equal(advancedRaw, beforeRaw) {
		t.Fatal("fork continuation changed source or failed to advance fork")
	}
}

func TestPublicationKernelForkKFNeg001AndBidirectionalIndependence(t *testing.T) {
	source, initial := publicationKernelAtSequence32(t)
	fork, err := source.Fork()
	if err != nil {
		t.Fatal(err)
	}
	sourcePoint, sourceBefore := mustCurrent(t, source)
	forkBefore := mustCurrentBytes(t, fork)

	malformed := initial
	malformed.Sequence = "33"
	malformed.ExpectedSemanticRevision = "1"
	malformed.BatchDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	assertRejectedUnchanged(t, fork, malformed, DigestMismatch)
	conflict := initial
	conflict.BatchID = "batch:fork-conflict"
	sealPublicationBatch(t, &conflict)
	assertRejectedUnchanged(t, fork, conflict, SequenceConflict)
	if !bytes.Equal(mustCurrentBytes(t, source), sourceBefore) || !bytes.Equal(mustCurrentBytes(t, fork), forkBefore) {
		t.Fatal("KF-NEG-001 rejection changed a committed point")
	}

	// A later source-only continuation remains invisible to the fork as well.
	next := publicationBatch("asset:site", "source:a", "epoch:a", "1", "33", publicationExpectedRevision(t, source))
	candidate := candidateByID(t, sourcePoint, "candidate:binding:a")
	candidate.Revision = "2"
	candidate.Value = pointerRecord(booleanValue(false))
	next.FactUpserts = []FactCandidate{candidate}
	sealPublicationBatch(t, &next)
	if _, _, err := source.Apply(next, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustCurrentBytes(t, fork), forkBefore) {
		t.Fatal("source-only continuation changed fork")
	}
}

func TestPublicationKernelForkKFPos004(t *testing.T) {
	accepting := newTestPublicationKernel(t, "asset:site")
	acceptingFork, err := accepting.Fork()
	if err != nil {
		t.Fatal(err)
	}
	batch := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	sealPublicationBatch(t, &batch)
	if _, _, err := accepting.Apply(batch, publicationMonotonic); err != nil {
		t.Fatalf("source custom validator accept path: %v", err)
	}
	if _, _, err := acceptingFork.Apply(batch, publicationMonotonic); err != nil {
		t.Fatalf("fork custom validator accept path: %v", err)
	}

	pack := PackRef{ID: "pack.test", Version: "1.0.0"}
	rejectingValidator := &countingValidator{pack: pack, index: DefinitionIndex{Pack: pack, Fields: []DefinitionRef{}, Services: []DefinitionRef{{Pack: pack, ID: "service.test", Version: "1.0.0"}}, Capabilities: []DefinitionRef{{Pack: pack, ID: "capability.test", Version: "1.0.0"}}, Operations: []DefinitionRef{}, EffectRules: []DefinitionRef{}}, reject: errID(InvalidValue, "fork validator")}
	rejecting, err := NewPublicationKernel("asset:site", rejectingValidator)
	if err != nil {
		t.Fatal(err)
	}
	rejectingFork, err := rejecting.Fork()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := rejecting.Current(); ok {
		t.Fatal("empty source acquired state before first publication")
	}
	if _, _, ok := rejectingFork.Current(); ok {
		t.Fatal("empty fork acquired state before first publication")
	}
	for name, kernel := range map[string]*PublicationKernel{"source": rejecting, "fork": rejectingFork} {
		if _, _, err := kernel.Apply(batch, publicationMonotonic); ErrorIdentifier(err) != InvalidValue {
			t.Fatalf("%s custom validator reject path: %v", name, err)
		}
		if _, _, ok := kernel.Current(); ok {
			t.Fatalf("%s reject path changed empty state", name)
		}
	}
}

func TestPublicationKernelForkKFNeg002(t *testing.T) {
	source, initial := publicationKernelAtSequence32(t)
	fork, err := source.Fork()
	if err != nil {
		t.Fatal(err)
	}
	sourceBefore, sourceRaw := mustCurrent(t, source)
	forkBefore, forkRaw := mustCurrent(t, fork)
	mutateReturnedSnapshot(t, &sourceBefore, append([]byte(nil), sourceRaw...))
	mutateReturnedSnapshot(t, &forkBefore, append([]byte(nil), forkRaw...))

	// Apply and replay outputs are detached independently too.
	sourceResult, sourceResultRaw, err := source.Apply(initial, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	mutateReturnedSnapshot(t, &sourceResult, append([]byte(nil), sourceResultRaw...))
	forkResult, forkResultRaw, err := fork.Apply(initial, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	mutateReturnedSnapshot(t, &forkResult, append([]byte(nil), forkResultRaw...))
	if got := mustCurrentBytes(t, source); !bytes.Equal(got, sourceRaw) {
		t.Fatal("source attached state changed through returned nested value")
	}
	if got := mustCurrentBytes(t, fork); !bytes.Equal(got, forkRaw) {
		t.Fatal("fork attached state changed through returned nested value")
	}
}

func TestPublicationKernelForkKFPos005(t *testing.T) {
	kernel, _ := publicationKernelAtSequence32(t)
	errs := make(chan error, 1)
	var reportOnce sync.Once
	report := func(err error) { reportOnce.Do(func() { errs <- err }) }
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for n := 0; n < 100; n++ {
				if snapshot, raw, ok := kernel.Current(); ok {
					canonical, err := CanonicalJSON(snapshot)
					if err != nil || !bytes.Equal(canonical, raw) {
						report(fmt.Errorf("source current: %w", err))
						return
					}
				}
				fork, err := kernel.Fork()
				if err != nil {
					report(err)
					return
				}
				if snapshot, raw, ok := fork.Current(); ok {
					canonical, err := CanonicalJSON(snapshot)
					if err != nil || !bytes.Equal(canonical, raw) {
						report(fmt.Errorf("fork current: %w", err))
						return
					}
				}
			}
		}()
	}
	for sequence := 33; sequence <= 48; sequence++ {
		next := publicationBatch("asset:site", "source:a", "epoch:a", "1", Uint64(strconv.Itoa(sequence)), publicationExpectedRevision(t, kernel))
		sealPublicationBatch(t, &next)
		if _, _, err := kernel.Apply(next, publicationMonotonic); err != nil {
			t.Fatal(err)
		}
	}
	readers.Wait()
	select {
	case err := <-errs:
		t.Fatalf("concurrent point was incomplete: %v", err)
	default:
	}
}

func publicationKernelAtSequence32(t *testing.T) (*PublicationKernel, PublicationBatch) {
	t.Helper()
	kernel := newTestPublicationKernel(t, "asset:site")
	initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	sealPublicationBatch(t, &initial)
	if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	accepted := initial
	for sequence := 2; sequence <= 32; sequence++ {
		batch := publicationBatch("asset:site", "source:a", "epoch:a", "1", Uint64(strconv.Itoa(sequence)), publicationExpectedRevision(t, kernel))
		sealPublicationBatch(t, &batch)
		if _, _, err := kernel.Apply(batch, publicationMonotonic); err != nil {
			t.Fatalf("sequence %d: %v", sequence, err)
		}
		accepted = batch
	}
	return kernel, accepted
}

func mustCurrent(t *testing.T, kernel *PublicationKernel) (Snapshot, []byte) {
	t.Helper()
	snapshot, raw, ok := kernel.Current()
	if !ok {
		t.Fatal("missing current snapshot")
	}
	return snapshot, raw
}

func publicationExpectedRevision(t *testing.T, kernel *PublicationKernel) Uint64 {
	t.Helper()
	snapshot, _ := mustCurrent(t, kernel)
	return snapshot.Revisions.Semantic
}

func mustCanonical(t *testing.T, snapshot Snapshot) []byte {
	t.Helper()
	raw, err := CanonicalJSON(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func mutateReturnedSnapshot(t *testing.T, snapshot *Snapshot, raw []byte) {
	t.Helper()
	if len(raw) == 0 || len(snapshot.Sources) == 0 || len(snapshot.Bindings) == 0 || len(snapshot.Facts) == 0 || len(snapshot.Facts[0].Candidates) == 0 {
		t.Fatal("test fixture lacks nested values to mutate")
	}
	raw[0] = '!'
	snapshot.Sources[0].ProfileID = "profile:mutated"
	snapshot.Bindings[0].NativeResource.Owner = "native:mutated"
	candidate := &snapshot.Facts[0].Candidates[0]
	*candidate.BindingID = "binding:mutated"
	*candidate.SourceEpochID = "epoch:mutated"
	*candidate.DriverGeneration = "9"
	candidate.Origin.Evidence[0].Owner = "evidence:mutated"
	if candidate.Value == nil || candidate.Value.Boolean == nil {
		t.Fatal("test fixture lacks nested pointer value")
	}
	*candidate.Value.Boolean = !*candidate.Value.Boolean
}
