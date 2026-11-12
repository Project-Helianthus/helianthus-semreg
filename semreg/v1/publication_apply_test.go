package semreg

import (
	"bytes"
	"reflect"
	"testing"
)

// applyPhaseVector is one rejected batch whose first collected diagnostic is
// raised in a named applyTo phase. The ID and detail pin the exact error value
// the kernel returns, so a phase split that reorders collection is caught.
type applyPhaseVector struct {
	name   string
	fresh  bool
	phase  string
	id     ErrorID
	detail string
	build  func() PublicationBatch
}

const (
	applyPhaseInput    = "input_validation"
	applyPhaseBindings = "binding_resolution"
	applyPhaseEval     = "evaluation"
	applyPhaseFacts    = "fact_assembly"
	applyPhaseCommit   = "commit"
)

func applyPhaseVectors() []applyPhaseVector {
	next := func() PublicationBatch {
		return publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	}
	return []applyPhaseVector{
		{name: "unknown source retirement", phase: applyPhaseInput, id: StaleSourceEpoch, detail: "source retirement", build: func() PublicationBatch {
			b := next()
			b.SourceRetirements = []SourceEpochID{"epoch:missing"}
			return b
		}},
		{name: "missing publication source", fresh: true, phase: applyPhaseInput, id: DanglingReference, detail: "publication source", build: func() PublicationBatch {
			return publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
		}},
		{name: "identity link without binding", phase: applyPhaseBindings, id: DanglingReference, detail: "identity binding", build: func() PublicationBatch {
			b := next()
			b.IdentityLinkUpserts = []IdentityLink{publicationLink("asset:site", "binding:missing")}
			return b
		}},
		{name: "binding reuse with another native resource", phase: applyPhaseBindings, id: IdentityNotQualified, detail: "binding reuse", build: func() PublicationBatch {
			b := next()
			binding := publicationBinding("asset:site", "source:a", "epoch:a", "binding:a", "1")
			binding.NativeResource = publicEvidence("7")
			binding.Revision = "2"
			b.BindingUpserts = []NativeBinding{binding}
			return b
		}},
		{name: "unknown fact withdrawal", phase: applyPhaseEval, id: DanglingReference, detail: "fact withdrawal", build: func() PublicationBatch {
			b := next()
			b.FactWithdrawals = []CandidateID{"candidate:missing"}
			return b
		}},
		{name: "service without binding", phase: applyPhaseEval, id: DanglingReference, detail: "service binding", build: func() PublicationBatch {
			b := next()
			b.ServiceUpserts = []ServiceInstance{publicationService("asset:site", "service:missing", "binding:missing", "epoch:a", "1")}
			return b
		}},
		{name: "unknown capability withdrawal", phase: applyPhaseEval, id: DanglingReference, detail: "capability withdrawal", build: func() PublicationBatch {
			b := next()
			b.CapabilityWithdrawals = []CapabilityInstanceID{"capability:missing"}
			return b
		}},
		{name: "derived fact with missing input", phase: applyPhaseFacts, id: DanglingReference, detail: "derivation input", build: func() PublicationBatch {
			b := next()
			absent := publicationCandidate("candidate:absent", "fact.absent", true, "source:a", "epoch:a", "binding:a", "1")
			b.FactUpserts = []FactCandidate{publicationDerivedCandidate("candidate:derived", "fact.derived", []FactCandidate{absent})}
			return b
		}},
		{name: "two current epochs of one source", fresh: true, phase: applyPhaseCommit, id: StaleSourceEpoch, detail: "multiple current source epochs", build: func() PublicationBatch {
			b := publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
			b.SourceUpserts = []SourceDescriptor{publicationSource("source:a", "epoch:a"), publicationSource("source:a", "epoch:b")}
			return b
		}},
		{name: "binding-phase failure outranks an equal later evaluation failure", phase: applyPhaseBindings, id: DanglingReference, detail: "identity binding", build: func() PublicationBatch {
			b := next()
			b.IdentityLinkUpserts = []IdentityLink{publicationLink("asset:site", "binding:missing")}
			b.FactWithdrawals = []CandidateID{"candidate:missing"}
			b.ServiceUpserts = []ServiceInstance{publicationService("asset:site", "service:missing", "binding:missing", "epoch:a", "1")}
			return b
		}},
		{name: "fact withdrawal outranks an equal later service failure", phase: applyPhaseEval, id: DanglingReference, detail: "fact withdrawal", build: func() PublicationBatch {
			b := next()
			b.FactWithdrawals = []CandidateID{"candidate:missing"}
			b.ServiceUpserts = []ServiceInstance{publicationService("asset:site", "service:missing", "binding:missing", "epoch:a", "1")}
			return b
		}},
		{name: "input failure outranks an equal later fact assembly failure", fresh: true, phase: applyPhaseInput, id: DanglingReference, detail: "publication source", build: func() PublicationBatch {
			b := publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
			absent := publicationCandidate("candidate:absent", "fact.absent", true, "source:a", "epoch:a", "binding:a", "1")
			b.FactUpserts = []FactCandidate{publicationDerivedCandidate("candidate:derived", "fact.derived", []FactCandidate{absent})}
			return b
		}},
	}
}

func applyPhaseKernel(t *testing.T, fresh bool) *PublicationKernel {
	t.Helper()
	kernel := newTestPublicationKernel(t, "asset:site")
	if fresh {
		return kernel
	}
	initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	sealPublicationBatch(t, &initial)
	if _, _, err := kernel.Apply(initial, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	return kernel
}

func requireDiagnostic(t *testing.T, err error, id ErrorID, detail string) {
	t.Helper()
	diagnostic, ok := err.(*Error)
	if !ok || diagnostic.ID != id || diagnostic.Detail != detail {
		t.Fatalf("got %#v, want %s %q", err, id, detail)
	}
}

// TestApplyPhaseFailuresKeepDiagnosticsAndLeaveKernelUncommitted pins the
// returned error value of a failure raised in each applyTo phase and proves
// the kernel snapshot, canonical bytes and replay table stay untouched.
func TestApplyPhaseFailuresKeepDiagnosticsAndLeaveKernelUncommitted(t *testing.T) {
	for _, vector := range applyPhaseVectors() {
		t.Run(vector.name, func(t *testing.T) {
			kernel := applyPhaseKernel(t, vector.fresh)
			beforeSnapshot, beforeBytes, beforeOK := kernel.Current()
			beforeReplays := len(kernel.replays)
			batch := vector.build()
			sealPublicationBatch(t, &batch)

			_, _, err := kernel.Apply(batch, publicationMonotonic)
			requireDiagnostic(t, err, vector.id, vector.detail)

			afterSnapshot, afterBytes, afterOK := kernel.Current()
			if beforeOK != afterOK || !reflect.DeepEqual(beforeSnapshot, afterSnapshot) || !bytes.Equal(beforeBytes, afterBytes) {
				t.Fatal("rejected batch committed a snapshot")
			}
			if len(kernel.replays) != beforeReplays {
				t.Fatal("rejected batch recorded a replay result")
			}
		})
	}
}

// applyPhaseRun drives the applyTo phases one at a time on a detached working
// snapshot, exactly as applyLocked prepares it.
type applyPhaseRun struct {
	working  Snapshot
	pristine Snapshot
	state    *applyState
}

func newApplyPhaseRun(t *testing.T, kernel *PublicationKernel, batch PublicationBatch) *applyPhaseRun {
	t.Helper()
	run := &applyPhaseRun{working: emptySnapshot(kernel.asset)}
	if kernel.current != nil {
		run.working = cloneSnapshot(*kernel.current)
	}
	run.pristine = cloneSnapshot(run.working)
	run.state = newApplyState(kernel.registry, &run.working, clonePublicationBatch(batch), publicationMonotonic)
	if run.state.failure != nil {
		t.Fatalf("loading the prior state failed: %v", run.state.failure)
	}
	return run
}

func (r *applyPhaseRun) phases() []struct {
	name string
	run  func() componentChanges
} {
	wrap := func(phase func()) func() componentChanges {
		return func() componentChanges { phase(); return componentChanges{} }
	}
	return []struct {
		name string
		run  func() componentChanges
	}{
		{applyPhaseInput, wrap(r.state.validateInput)},
		{applyPhaseBindings, wrap(r.state.resolveBindings)},
		{applyPhaseEval, wrap(r.state.evaluate)},
		{applyPhaseFacts, wrap(r.state.assembleFacts)},
		{applyPhaseCommit, r.state.commit},
	}
}

func (r *applyPhaseRun) requireWorkingUntouched(t *testing.T, phase string) {
	t.Helper()
	if !reflect.DeepEqual(cloneSnapshot(r.working), r.pristine) {
		t.Fatalf("phase %s wrote the working snapshot before commit", phase)
	}
}

// TestApplyPhaseBoundaries runs each vector phase by phase. The first
// collected diagnostic appears in the phase the vector names and is not
// displaced by any later phase, no phase before commit writes the working
// snapshot, and no phase touches the kernel.
func TestApplyPhaseBoundaries(t *testing.T) {
	for _, vector := range applyPhaseVectors() {
		t.Run(vector.name, func(t *testing.T) {
			kernel := applyPhaseKernel(t, vector.fresh)
			_, kernelBefore, _ := kernel.Current()
			batch := vector.build()
			sealPublicationBatch(t, &batch)
			run := newApplyPhaseRun(t, kernel, batch)

			raised := ""
			for _, phase := range run.phases() {
				phase.run()
				if raised == "" && run.state.failure != nil {
					raised = phase.name
				}
				if raised != "" {
					requireDiagnostic(t, run.state.failure, vector.id, vector.detail)
				}
				if phase.name != applyPhaseCommit {
					run.requireWorkingUntouched(t, phase.name)
				}
			}
			if raised != vector.phase {
				t.Fatalf("first diagnostic raised in %q, want %q", raised, vector.phase)
			}
			if _, kernelAfter, _ := kernel.Current(); !bytes.Equal(kernelBefore, kernelAfter) {
				t.Fatal("a phase committed to the kernel")
			}
		})
	}
}

// TestApplyPhaseCommitReportsEachChangedComponent varies each componentChanges
// field on its own and proves the phase-by-phase post-state is the one Apply
// commits, with the matching revision vector.
func TestApplyPhaseCommitReportsEachChangedComponent(t *testing.T) {
	next := func() PublicationBatch {
		return publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	}
	vectors := []struct {
		name  string
		want  componentChanges
		build func() PublicationBatch
	}{
		{"no component", componentChanges{}, next},
		{"identity link", componentChanges{identity: true}, func() PublicationBatch {
			b := next()
			link := publicationLink("asset:site", "binding:a")
			link.Basis, link.Revision = []EvidenceRef{publicEvidence("6")}, "2"
			b.IdentityLinkUpserts = []IdentityLink{link}
			return b
		}},
		{"fact", componentChanges{facts: true}, func() PublicationBatch {
			b := next()
			candidate := publicationCandidate("candidate:binding:a", "fact.power", false, "source:a", "epoch:a", "binding:a", "1")
			candidate.Revision = "2"
			b.FactUpserts = []FactCandidate{candidate}
			return b
		}},
		{"service", componentChanges{services: true}, func() PublicationBatch {
			b := next()
			service := publicationService("asset:site", "service:binding:a", "binding:a", "epoch:a", "1")
			service.Qualification, service.Revision = QualificationCandidate, "2"
			b.ServiceUpserts = []ServiceInstance{service}
			return b
		}},
		{"capability", componentChanges{capabilities: true}, func() PublicationBatch {
			b := next()
			capability := publicationCapability("asset:site", "capability:binding:a", "service:binding:a", "binding:a", "epoch:a", "1")
			capability.Qualification, capability.Revision = QualificationCandidate, "2"
			b.CapabilityUpserts = []CapabilityInstance{capability}
			return b
		}},
	}
	for _, vector := range vectors {
		t.Run(vector.name, func(t *testing.T) {
			kernel := applyPhaseKernel(t, false)
			prior, _, _ := kernel.Current()
			batch := vector.build()
			sealPublicationBatch(t, &batch)
			run := newApplyPhaseRun(t, kernel, batch)
			var changed componentChanges
			for _, phase := range run.phases() {
				changed = phase.run()
				if run.state.failure != nil {
					t.Fatalf("phase %s: %v", phase.name, run.state.failure)
				}
				if phase.name != applyPhaseCommit {
					run.requireWorkingUntouched(t, phase.name)
				}
			}
			if changed != vector.want {
				t.Fatalf("changed components %+v, want %+v", changed, vector.want)
			}

			applied, _, err := kernel.Apply(batch, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if applied.Revisions != nextRevisionVector(prior.Revisions, false, vector.want) {
				t.Fatalf("revisions %+v after %+v", applied.Revisions, prior.Revisions)
			}
			staged := cloneSnapshot(run.working)
			staged.Contract, staged.AssetID, staged.EvaluatedAt, staged.EvaluateMonotonic = applied.Contract, applied.AssetID, applied.EvaluatedAt, applied.EvaluateMonotonic
			staged.Revisions, staged.SnapshotID = applied.Revisions, applied.SnapshotID
			if !reflect.DeepEqual(staged, applied) {
				t.Fatal("phase-by-phase post-state differs from the committed snapshot")
			}
		})
	}
}
