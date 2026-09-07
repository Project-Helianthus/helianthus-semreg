package semreg

import (
	"sync"
	"testing"
)

func TestPublicationKernelForkSerializesSharedValidatorHooks(t *testing.T) {
	pack := PackRef{ID: "pack.test", Version: "1.0.0"}
	validator := &countingValidator{pack: pack, index: DefinitionIndex{Pack: pack, Fields: []DefinitionRef{}, Services: []DefinitionRef{{Pack: pack, ID: "service.test", Version: "1.0.0"}}, Capabilities: []DefinitionRef{{Pack: pack, ID: "capability.test", Version: "1.0.0"}}, Operations: []DefinitionRef{}, EffectRules: []DefinitionRef{}}}
	source, err := NewPublicationKernel("asset:site", validator)
	if err != nil {
		t.Fatal(err)
	}
	initial := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	sealPublicationBatch(t, &initial)
	if _, _, err := source.Apply(initial, publicationMonotonic); err != nil {
		t.Fatal(err)
	}
	fork, err := source.Fork()
	if err != nil {
		t.Fatal(err)
	}
	next := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	candidate := initial.FactUpserts[0]
	candidate.Revision = "2"
	candidate.Value = pointerRecord(booleanValue(false))
	next.FactUpserts = []FactCandidate{candidate}
	sealPublicationBatch(t, &next)

	errs := make(chan error, 2)
	var applies sync.WaitGroup
	for _, kernel := range []*PublicationKernel{source, fork} {
		applies.Add(1)
		go func(kernel *PublicationKernel) {
			defer applies.Done()
			_, _, err := kernel.Apply(next, publicationMonotonic)
			errs <- err
		}(kernel)
	}
	applies.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, kernel := range map[string]*PublicationKernel{"source": source, "fork": fork} {
		snapshot, _, ok := kernel.Current()
		if !ok || cursorFor(t, snapshot, "source:a", "epoch:a", "1").LastSequence != "2" || snapshot.Revisions.Semantic != "2" {
			t.Fatalf("%s did not independently commit sequence 2: %+v", name, snapshot)
		}
	}
	if validator.factCalls != 3 {
		t.Fatalf("shared stateful validator calls = %d, want 3", validator.factCalls)
	}
}
