package semreg

import (
	"bytes"
	"sync"
	"testing"
)

func evaluationSnapshot(t *testing.T, candidates ...FactCandidate) Snapshot {
	t.Helper()
	kernel := newTestPublicationKernel(t, "asset:evaluation")
	batch := publicationBatch("asset:evaluation", "source:evaluation", "epoch:evaluation", "1", "1", "0")
	batch.SourceUpserts = []SourceDescriptor{publicationSource("source:evaluation", "epoch:evaluation")}
	batch.BindingUpserts = []NativeBinding{publicationBinding("asset:evaluation", "source:evaluation", "epoch:evaluation", "binding:evaluation", "1")}
	batch.IdentityLinkUpserts = []IdentityLink{publicationLink("asset:evaluation", "binding:evaluation")}
	batch.FactUpserts = candidates
	batch.ServiceUpserts = []ServiceInstance{publicationService("asset:evaluation", "service:evaluation", "binding:evaluation", "epoch:evaluation", "1")}
	batch.CapabilityUpserts = []CapabilityInstance{publicationCapability("asset:evaluation", "capability:evaluation", "service:evaluation", "binding:evaluation", "epoch:evaluation", "1")}
	sealPublicationBatch(t, &batch)
	snapshot, _, err := kernel.Apply(batch, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func evaluationCandidate(id CandidateID, fact DefinitionID, receipt, tick Uint64) FactCandidate {
	candidate := publicationCandidate(id, fact, true, "source:evaluation", "epoch:evaluation", "binding:evaluation", "1")
	candidate.Times = Times{
		ReceivedAt:        TimePoint{UnixNanoseconds: Int64(receipt), ClockID: "clock.utc", UncertaintyNS: "0"},
		ReceiptMonotonic:  MonotonicPoint{ClockEpochID: "clock-epoch:evaluation", Nanoseconds: tick},
		EvaluatedAt:       TimePoint{UnixNanoseconds: Int64(receipt), ClockID: "clock.utc", UncertaintyNS: "0"},
		EvaluateMonotonic: MonotonicPoint{ClockEpochID: "clock-epoch:evaluation", Nanoseconds: tick},
	}
	candidate.FreshnessPolicy = FreshnessPolicy{PolicyID: "policy:evaluation", Version: "1.0.0", FreshForNS: "30", RetainForNS: "120", MaxWallUncertaintyNS: "2"}
	return candidate
}

func evaluationContext(wall Int64, epoch ClockEpochID, tick Uint64, uncertainty Uint64) EvaluationContext {
	return EvaluationContext{EvaluatedAt: TimePoint{UnixNanoseconds: wall, ClockID: "clock.utc", UncertaintyNS: uncertainty}, EvaluateMonotonic: MonotonicPoint{ClockEpochID: epoch, Nanoseconds: tick}}
}

func factResult(t *testing.T, view EvaluationView, id CandidateID) EvaluatedFact {
	t.Helper()
	for _, fact := range view.Facts {
		if fact.CandidateID == id {
			return fact
		}
	}
	t.Fatalf("missing evaluated candidate %s", id)
	return EvaluatedFact{}
}

func TestEvaluationContractMatrix(t *testing.T) {
	candidate := evaluationCandidate("candidate:evaluation:one", "fact.power", "1000", "100")
	snapshot := evaluationSnapshot(t, candidate)
	before, err := CanonicalJSON(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name         string
		context      EvaluationContext
		freshness    Freshness
		availability Availability
	}{
		{"fresh before exact boundary", evaluationContext("1029", "clock-epoch:evaluation", "129", "0"), FreshnessFresh, AvailabilityAvailable},
		{"stale at exact boundary", evaluationContext("1030", "clock-epoch:evaluation", "130", "0"), FreshnessStale, AvailabilityDegraded},
		{"expired at exact boundary", evaluationContext("1120", "clock-epoch:evaluation", "220", "0"), FreshnessExpired, AvailabilityUnavailable},
		// K-POS-005: restart never subtracts ticks; comparable UTC makes this stale.
		{"K-POS-005 cross epoch stale", evaluationContext("1040", "clock-epoch:after-restart", "1", "1"), FreshnessStale, AvailabilityDegraded},
		{"cross epoch uncertainty straddles fresh boundary", evaluationContext("1030", "clock-epoch:after-restart", "1", "2"), FreshnessUnknown, AvailabilityDegraded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view, err := EvaluateSnapshot(snapshot, tc.context)
			if err != nil {
				t.Fatal(err)
			}
			fact := factResult(t, view, candidate.CandidateID)
			if fact.Freshness != tc.freshness || fact.EffectiveAvailability != tc.availability {
				t.Fatalf("got %+v", fact)
			}
			if view.Contract != ContractEvaluationV1 || view.SnapshotID != snapshot.SnapshotID || view.Revisions != snapshot.Revisions || view.EvaluationDigest == "" {
				t.Fatalf("bad view binding: %+v", view)
			}
			if _, err := CanonicalJSON(view); err != nil {
				t.Fatal(err)
			}
		})
	}
	after, err := CanonicalJSON(snapshot)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("evaluation mutated snapshot: %v", err)
	}
}

func TestEvaluationRejectsTimeFailuresAndCombinesDeepInputs(t *testing.T) {
	leaf := evaluationCandidate("candidate:evaluation:leaf", "fact.leaf", "1000", "100")
	middle := publicationDerivedCandidate("candidate:evaluation:middle", "fact.middle", []FactCandidate{leaf})
	middle.Times, middle.FreshnessPolicy = leaf.Times, leaf.FreshnessPolicy
	root := publicationDerivedCandidate("candidate:evaluation:root", "fact.root", []FactCandidate{middle})
	root.Times, root.FreshnessPolicy = leaf.Times, leaf.FreshnessPolicy
	snapshot := evaluationSnapshot(t, leaf, middle, root)
	view, err := EvaluateSnapshot(snapshot, evaluationContext("1120", "clock-epoch:evaluation", "220", "0"))
	if err != nil {
		t.Fatal(err)
	}
	if got := factResult(t, view, root.CandidateID); got.Freshness != FreshnessExpired || got.EffectiveAvailability != AvailabilityUnavailable {
		t.Fatalf("transitive expiry lost: %+v", got)
	}
	for _, tc := range []struct {
		name    string
		context EvaluationContext
	}{
		{"same epoch backwards", evaluationContext("1001", "clock-epoch:evaluation", "99", "0")},
		{"negative cross epoch outside uncertainty", evaluationContext("900", "clock-epoch:restart", "1", "0")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := EvaluateSnapshot(snapshot, tc.context)
			if ErrorIdentifier(err) != InvalidTime {
				t.Fatalf("got %v, want invalid_time", err)
			}
		})
	}
	overflow := evaluationCandidate("candidate:evaluation:overflow", "fact.overflow", "1000", "100")
	overflow.Times.ReceivedAt.UncertaintyNS = "18446744073709551615"
	overflowSnapshot := evaluationSnapshot(t, overflow)
	if _, err := EvaluateSnapshot(overflowSnapshot, evaluationContext("1001", "clock-epoch:restart", "1", "18446744073709551615")); ErrorIdentifier(err) != InvalidTime {
		t.Fatalf("uncertainty overflow: %v", err)
	}
}

type recordingSelectionPolicy struct {
	id      PolicyID
	version SemanticVersion
	chosen  CandidateID
	mu      sync.Mutex
	calls   int
	mutate  bool
}

func (p *recordingSelectionPolicy) PolicyID() PolicyID       { return p.id }
func (p *recordingSelectionPolicy) Version() SemanticVersion { return p.version }
func (p *recordingSelectionPolicy) Select(envelope FactEnvelope, facts []EvaluatedFact) (CandidateID, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.mutate {
		envelope.Candidates[0].CandidateID = "candidate:policy:mutated"
		facts[0].CandidateID = "candidate:policy:mutated"
	}
	return p.chosen, nil
}

func (p *recordingSelectionPolicy) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestSelectionContractMatrix(t *testing.T) {
	power := evaluationCandidate("candidate:evaluation:power", "fact.power", "1000", "100")
	voltage := evaluationCandidate("candidate:evaluation:voltage", "fact.voltage", "1000", "100")
	snapshot := evaluationSnapshot(t, power, voltage)
	context := evaluationContext("1030", "clock-epoch:evaluation", "130", "0")
	view, err := EvaluateSnapshot(snapshot, context)
	if err != nil {
		t.Fatal(err)
	}
	policy := &recordingSelectionPolicy{id: "policy:presentation", version: "1.0.0", chosen: voltage.CandidateID, mutate: true}
	kernel, err := NewSelectionKernel(policy)
	if err != nil {
		t.Fatal(err)
	}
	before, err := CanonicalJSON(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := kernel.SelectPresentation(snapshot, view, voltage.Key, policy.id, policy.version)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Contract != ContractSelectionV1 || !selection.PresentationOnly || selection.SelectedCandidate != voltage.CandidateID || selection.CandidateRevision != voltage.Revision || selection.Context != context {
		t.Fatalf("unexpected selection: %+v", selection)
	}
	if policy.Calls() != 1 {
		t.Fatalf("calls = %d", policy.Calls())
	}
	after, _ := CanonicalJSON(snapshot)
	if !bytes.Equal(before, after) {
		t.Fatal("policy mutated caller snapshot")
	}
	if _, err := CanonicalJSON(selection); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSelection(snapshot, view, selection); err != nil {
		t.Fatal(err)
	}
	wrongContext := selection
	wrongContext.Context.EvaluateMonotonic.Nanoseconds = "131"
	if ErrorIdentifier(ValidateSelection(snapshot, view, wrongContext)) != RevisionConflict {
		t.Fatal("K-NEG-062 selection context mismatch was accepted")
	}
	replay, err := kernel.SelectPresentation(snapshot, view, voltage.Key, policy.id, policy.version)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, _ := CanonicalJSON(selection)
	replayBytes, _ := CanonicalJSON(replay)
	if !bytes.Equal(firstBytes, replayBytes) {
		t.Fatal("identical selection inputs were not canonical-byte deterministic")
	}
	if err := kernel.RegisterSelectionPolicy(policy); ErrorIdentifier(err) != DefinitionOwnerConflict {
		t.Fatalf("duplicate registration: %v", err)
	}

	cases := []struct {
		name   string
		view   EvaluationView
		key    FactKey
		policy PolicyID
		want   ErrorID
	}{
		{"K-NEG-056 out of envelope", view, voltage.Key, "policy:wrong-envelope", InvalidValue},
		{"K-NEG-057 snapshot mismatch", func() EvaluationView {
			v := copiedEvaluationView(t, view)
			v.SnapshotID = "snapshot:other"
			sealEvaluationView(t, &v)
			return v
		}(), voltage.Key, policy.id, RevisionConflict},
		{"K-NEG-058 revision vector mismatch", func() EvaluationView {
			v := copiedEvaluationView(t, view)
			v.Revisions.Facts = "99"
			sealEvaluationView(t, &v)
			return v
		}(), voltage.Key, policy.id, RevisionConflict},
		{"K-NEG-059 digest mismatch", func() EvaluationView {
			v := copiedEvaluationView(t, view)
			v.EvaluationDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
			return v
		}(), voltage.Key, policy.id, DigestMismatch},
		{"K-NEG-060 missing key", view, FactKey{PackID: "pack.test", PackVersion: "1.0.0", FactID: "fact.missing", Dimensions: []Dimension{}}, policy.id, DanglingReference},
		{"K-NEG-061 omitted fact", func() EvaluationView {
			v := copiedEvaluationView(t, view)
			v.Facts = v.Facts[:1]
			sealEvaluationView(t, &v)
			return v
		}(), voltage.Key, policy.id, DanglingReference},
		{"K-NEG-065 candidate revision", func() EvaluationView {
			v := copiedEvaluationView(t, view)
			v.Facts[1].CandidateRevision = "9"
			sealEvaluationView(t, &v)
			return v
		}(), voltage.Key, policy.id, RevisionConflict},
		{"K-NEG-063 missing policy", view, voltage.Key, "policy:missing", DefinitionOwnerMissing},
	}
	wrong := &recordingSelectionPolicy{id: "policy:wrong-envelope", version: "1.0.0", chosen: power.CandidateID}
	if err := kernel.RegisterSelectionPolicy(wrong); err != nil {
		t.Fatal(err)
	}
	baselineCalls := policy.Calls()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := kernel.SelectPresentation(snapshot, tc.view, tc.key, tc.policy, "1.0.0")
			if ErrorIdentifier(err) != tc.want {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
	if policy.Calls() != baselineCalls || wrong.Calls() != 1 {
		t.Fatalf("invalid input dispatched a policy: valid=%d wrong=%d", policy.Calls(), wrong.Calls())
	}
}

func TestEvaluationConcurrentReadOnlyDeterminism(t *testing.T) {
	candidate := evaluationCandidate("candidate:evaluation:concurrent", "fact.concurrent", "1000", "100")
	snapshot := evaluationSnapshot(t, candidate)
	before, err := CanonicalJSON(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	context := evaluationContext("1030", "clock-epoch:evaluation", "130", "0")
	want, err := EvaluateSnapshot(snapshot, context)
	if err != nil {
		t.Fatal(err)
	}
	wantBytes, _ := CanonicalJSON(want)
	var group sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			view, err := EvaluateSnapshot(snapshot, context)
			if err != nil {
				errs <- err
				return
			}
			got, err := CanonicalJSON(view)
			if err != nil || !bytes.Equal(got, wantBytes) {
				errs <- errID(InvalidValue, "nondeterministic evaluation")
			}
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	after, _ := CanonicalJSON(snapshot)
	if !bytes.Equal(before, after) {
		t.Fatal("concurrent evaluation mutated snapshot")
	}
}

func sealEvaluationView(t *testing.T, view *EvaluationView) {
	t.Helper()
	digest, err := view.EvaluationDigestValue()
	if err != nil {
		t.Fatal(err)
	}
	view.EvaluationDigest = digest
}

func copiedEvaluationView(t *testing.T, view EvaluationView) EvaluationView {
	t.Helper()
	raw, err := CanonicalJSON(view)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := DecodeEvaluationView(raw)
	if err != nil {
		t.Fatal(err)
	}
	return copy
}
