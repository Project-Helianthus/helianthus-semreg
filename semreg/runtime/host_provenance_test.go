package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestHostStampKeepsAnotherProducersCapturedDerivationInput(t *testing.T) {
	ctx := context.Background()
	producer, producerRef := openBinder(t, binderFactory{})
	cut, err := producer.svc.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var observed v1.FactCandidate
	for _, member := range cut.Snapshots {
		for _, fact := range member.Facts {
			for _, candidate := range fact.Candidates {
				if candidate.BindingID != nil {
					observed = candidate
				}
			}
		}
	}
	if observed.CandidateID == "" {
		t.Fatal("fixture did not publish a retained candidate")
	}
	foreign := v1.DerivationInput{CandidateID: observed.CandidateID, CandidateRevision: observed.Revision,
		SourcePaths: []v1.SourcePathRef{{BindingID: *observed.BindingID, SourceID: *observed.Origin.SourceID,
			SourceEpochID: *observed.SourceEpochID, DriverGeneration: *observed.DriverGeneration}}}

	publisher := producer
	ref := life.Ref{Driver: binderKind, Instance: "binder.two"}
	spec := publisher.specFor(ref, life.LifecycleCorrelation{InstanceID: ref.ID(), Epoch: "epoch:publisher", Generation: 5})
	session, err := publisher.svc.OpenSession(spec)
	if err != nil {
		t.Fatal(err)
	}
	// A local ID collision alone cannot make the captured dependency local.
	local := observed
	localSource, localEpoch, localGeneration, localBinding := v1.SourceID("local:source"), v1.SourceEpochID("local:epoch"), v1.Uint64("17"), v1.NativeBindingID("local:binding")
	local.Origin.SourceID, local.SourceEpochID, local.DriverGeneration, local.BindingID = &localSource, &localEpoch, &localGeneration, &localBinding
	local.Origin.BindingID, local.Origin.SourceEpochID = &localBinding, &localEpoch
	derived := observed
	derived.CandidateID = "candidate:dependent"
	derived.Quality.Assertion = v1.AssertionInferred
	derived.BindingID, derived.SourceEpochID, derived.DriverGeneration = nil, nil, nil
	derived.Origin = v1.OriginRef{OriginID: "origin:derived", Kind: v1.OriginDerived, Evidence: []v1.EvidenceRef{testEvidence("derived origin")}}
	derived.Derivation = &v1.Derivation{Algorithm: "algorithm.fixture", Version: "1.0.0", Evidence: []v1.EvidenceRef{}, Inputs: []v1.DerivationInput{foreign}}
	front, err := (&hostTransaction{session: session}).stamp(v1.PublicationBatch{FactUpserts: []v1.FactCandidate{local, derived}})
	if err != nil {
		t.Fatal(err)
	}
	if len(front.FactUpserts) != 2 || front.FactUpserts[0].Derivation == nil || !reflect.DeepEqual(front.FactUpserts[0].Derivation.Inputs, []v1.DerivationInput{foreign}) {
		t.Fatalf("rewritten retained dependency: %+v, want %+v", front.FactUpserts, foreign)
	}

	// Publish on the same asset as the dependency: the existing kernel admits
	// it only if the exact referenced candidate, revision and binding are live.
	status, err := producer.Snapshot(producerRef)
	if err != nil {
		t.Fatal(err)
	}
	ownerSpec := producer.specFor(producerRef, life.LifecycleCorrelation{InstanceID: producerRef.ID(), Epoch: status.Epoch, Generation: status.Generation})
	spec.Asset = ownerSpec.Asset
	session, err = publisher.svc.OpenSession(spec)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := (&hostPublisher{session: session, rt: publisher}).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Abort(ctx)
	batch := v1.PublicationBatch{FactUpserts: []v1.FactCandidate{derived}, SourceUpserts: []v1.SourceDescriptor{{
		ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1", RegistryEvidence: testEvidence("derived registry"),
		StartedAt: observed.Times.ReceivedAt, State: v1.SourceCurrent, Revision: "1",
	}}}
	if err := tx.Stage(batch); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("publication with an exact retained dependency: %v", err)
	}
	view, err := producer.svc.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range view.Snapshots {
		for _, fact := range member.Facts {
			for _, candidate := range fact.Candidates {
				if candidate.CandidateID == v1.CandidateID(scopedID(spec, string(derived.CandidateID))) && candidate.Derivation != nil && reflect.DeepEqual(candidate.Derivation.Inputs, []v1.DerivationInput{foreign}) {
					return
				}
			}
		}
	}
	t.Fatal("accepted cut does not contain the derived candidate with exact provenance")
}

func TestHostStampIdentifiesLocalDependenciesByTheWholePath(t *testing.T) {
	spec := SessionSpec{Source: "source:target", Epoch: "epoch:target", Generation: "101"}
	local := v1.SourcePathRef{BindingID: "binding:local", SourceID: "source:local", SourceEpochID: "epoch:local", DriverGeneration: "27"}
	const localID v1.CandidateID = "candidate:local"
	for _, tc := range []struct {
		field string
		vary  func(*v1.DerivationInput)
	}{
		{"local", nil},
		{"candidate ID", func(in *v1.DerivationInput) { in.CandidateID = "candidate:other" }},
		{"binding ID", func(in *v1.DerivationInput) { in.SourcePaths[0].BindingID = "binding:other" }},
		{"source ID", func(in *v1.DerivationInput) { in.SourcePaths[0].SourceID = "source:other" }},
		{"epoch", func(in *v1.DerivationInput) { in.SourcePaths[0].SourceEpochID = "epoch:other" }},
		{"generation", func(in *v1.DerivationInput) { in.SourcePaths[0].DriverGeneration = "31" }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			for _, revision := range []v1.Uint64{"3", "29"} {
				input := v1.DerivationInput{CandidateID: localID, CandidateRevision: revision, SourcePaths: []v1.SourcePathRef{local}}
				if tc.vary != nil {
					tc.vary(&input)
				}
				want := input
				if tc.vary == nil {
					want.CandidateID = v1.CandidateID(scopedID(spec, string(localID)))
					want.SourcePaths = []v1.SourcePathRef{{BindingID: v1.NativeBindingID(scopedID(spec, string(local.BindingID))), SourceID: spec.Source, SourceEpochID: spec.Epoch, DriverGeneration: spec.Generation}}
				}
				d := v1.Derivation{Inputs: []v1.DerivationInput{input}}
				stampDerivation(spec, &d, map[v1.CandidateID]bool{localID: true}, map[v1.SourcePathRef]bool{local: true})
				if !reflect.DeepEqual(d.Inputs, []v1.DerivationInput{want}) {
					t.Fatalf("got %+v, want %+v", d.Inputs, want)
				}
			}
		})
	}
}
