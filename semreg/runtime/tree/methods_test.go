package tree_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

type operations interface {
	Prepare(context.Context, tree.CutID, tree.MethodRef, tree.Input) (tree.Prepared, error)
	Call(context.Context, tree.Prepared, tree.CallOptions) (tree.Receipt, error)
	Outcome(context.Context, tree.ReceiptID) (tree.Outcome, error)
}

var _ operations = (*tree.Tree)(nil)

var setCurrent = semreg.DefinitionRef{
	Pack: semreg.PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}, ID: "evse.operation.set_allocated_current", Version: "1.0.0",
}

func semanticMethodRef(t *testing.T, ref semreg.DefinitionRef) tree.MethodRef {
	t.Helper()
	segment, err := tree.DefinitionSegment(ref)
	if err != nil {
		t.Fatal(err)
	}
	return tree.Ref(tree.Key{Node: tree.NodePath{Installation: installation}, Facet: tree.KeywordMethods, Domain: tree.KeywordSem, Segment: segment}.String())
}

func TestSemanticMethodTakesTheDispatcherPathOnceAndKeepsItsEvidence(t *testing.T) {
	w := threeResourceWorld(t)
	w.ops.result = driver.OperationResult{
		Status: "partial", KernelOutcome: operation.OutcomeIndeterminate, Reason: "second point failed",
		CompoundExecution: &driver.CompoundExecution{Partial: true, Steps: []driver.CompoundStep{
			{Point: "point.first", Attempted: true, Delivery: operation.DeliverySent, Acknowledgement: operation.AckAccepted},
			{Point: "point.second", Attempted: true, Delivery: operation.DeliveryUnknown},
			{Point: "point.third", Attempted: false},
		}},
	}
	cut := w.capture()
	method := semanticMethodRef(t, setCurrent)
	listed, err := cut.List(ctx, nodeRefOf(tree.NodePath{Installation: installation}), tree.Page{Size: 1000, Filter: tree.Filter{Kind: tree.EntryMethod}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range listed.Entries {
		found = found || d.Ref == method
	}
	if !found {
		t.Fatal("the installation node does not list the catalog's operation as a semantic method")
	}

	request := &driver.OperationRequest{Timeout: time.Second}
	prepared, err := w.tree.Prepare(ctx, cut.ID(), method, tree.Input{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if w.ops.prepares != 1 || w.ops.calls != 0 {
		t.Fatalf("prepare reached the dispatcher %d times and call %d times", w.ops.prepares, w.ops.calls)
	}
	receipt, err := w.tree.Call(ctx, prepared, tree.CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if w.ops.calls != 1 || receipt.Result == nil || receipt.Result.CompoundExecution == nil {
		t.Fatalf("receipt = %+v after %d dispatcher calls", receipt, w.ops.calls)
	}
	outcome, err := w.tree.Outcome(ctx, receipt.ID)
	if err != nil || outcome.Operation == nil {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	for name, result := range map[string]*driver.OperationResult{"receipt": receipt.Result, "outcome": outcome.Operation} {
		steps := result.CompoundExecution
		if steps == nil || !steps.Partial || len(steps.Steps) != 3 || steps.Steps[0].Point != "point.first" || steps.Steps[1].Point != "point.second" || steps.Steps[2].Point != "point.third" || steps.Steps[2].Attempted {
			t.Fatalf("%s lost the partial result or the step order: %+v", name, result)
		}
		if result.Status != "partial" || result.KernelOutcome != operation.OutcomeIndeterminate {
			t.Fatalf("%s result = %+v", name, result)
		}
	}
	if _, err := w.tree.Call(ctx, prepared, tree.CallOptions{}); !errors.Is(err, tree.ErrUsed) || w.ops.calls != 1 {
		t.Fatalf("a second call = %v after %d dispatcher calls", err, w.ops.calls)
	}
	index, err := cut.Read(ctx, tree.Ref(tree.Key{Node: tree.NodePath{Installation: installation}, Facet: tree.KeywordAttributes, Domain: tree.KeywordSem, Segment: mustSegment(t, tree.IndexPart, "outcomes")}.String()), tree.ReadOptions{})
	if err != nil || string(index.Encoded) != `["`+string(receipt.ID)+`"]` {
		t.Fatalf("outcomes index = %s, %v", index.Encoded, err)
	}
}

func TestCallWithoutFiniteAuthorityIsRefusedAndNothingIsDispatched(t *testing.T) {
	w := threeResourceWorld(t)
	cut := w.capture()
	method := semanticMethodRef(t, setCurrent)
	prepared, err := w.tree.Prepare(ctx, cut.ID(), method, tree.Input{Request: &driver.OperationRequest{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.tree.Call(ctx, prepared, tree.CallOptions{})
	mustCode(t, err, semreg.AuthorityMissing)
	if w.ops.calls != 0 {
		t.Fatalf("a call without a finite deadline reached the dispatcher %d times", w.ops.calls)
	}
	if _, err := w.tree.Call(ctx, prepared, tree.CallOptions{Timeout: time.Second}); err != nil || w.ops.calls != 1 {
		t.Fatalf("the finite authority of the call options = %v, %d calls", err, w.ops.calls)
	}
	if _, err := w.tree.Prepare(ctx, cut.ID(), method, tree.Input{}); semreg.ErrorIdentifier(err) != semreg.MissingMember {
		t.Fatalf("a semantic method without a request = %v", err)
	}
	unknown := setCurrent
	unknown.ID = "evse.operation.not_in_the_catalog"
	if _, err := w.tree.Prepare(ctx, cut.ID(), semanticMethodRef(t, unknown), tree.Input{Request: &driver.OperationRequest{Timeout: time.Second}}); semreg.ErrorIdentifier(err) != semreg.DanglingReference {
		t.Fatalf("an operation outside the catalog = %v", err)
	}
	if w.ops.prepares != 1 {
		t.Fatalf("dispatcher prepares = %d", w.ops.prepares)
	}
}

func TestNativeMethodIsCalledOnlyWhenCalled(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	instance := tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}
	w.life.set(instance, "running", 1)
	b := w.bind("ns_a", "inst.1", 1)
	var inputs [][]byte
	fail := false
	if err := b.Method(b.Root(), methodSpec("pairing"), func(_ context.Context, in []byte) ([]byte, error) {
		inputs = append(inputs, in)
		if fail {
			return []byte("partial"), errors.New("declined")
		}
		return []byte("paired"), nil
	}); err != nil {
		t.Fatal(err)
	}
	cut := w.capture()
	method := nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordMethods, "pairing")
	if _, err := cut.Describe(ctx, method); err != nil {
		t.Fatal(err)
	}
	prepared, err := w.tree.Prepare(ctx, cut.ID(), method, tree.Input{Encoded: []byte("arg")})
	if err != nil || len(inputs) != 0 {
		t.Fatalf("prepare called the method: %v, %d calls", err, len(inputs))
	}
	receipt, err := w.tree.Call(ctx, prepared, tree.CallOptions{})
	if err != nil || len(inputs) != 1 || string(inputs[0]) != "arg" {
		t.Fatalf("call = %v, inputs %q", err, inputs)
	}
	outcome, err := w.tree.Outcome(ctx, receipt.ID)
	if err != nil || string(outcome.Native) != "paired" || outcome.Error != "" || outcome.Operation != nil {
		t.Fatalf("outcome = %+v, %v", outcome, err)
	}
	if w.ops.prepares+w.ops.calls+w.ops.outcomes != 0 {
		t.Fatal("a native method went through the semantic dispatcher")
	}
	fail = true
	second, _ := w.tree.Prepare(ctx, cut.ID(), method, tree.Input{})
	receipt, err = w.tree.Call(ctx, second, tree.CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, _ := w.tree.Outcome(ctx, receipt.ID); outcome.Error != "declined" || string(outcome.Native) != "partial" {
		t.Fatalf("a failing native method = %+v", outcome)
	}
	if _, err := w.tree.Prepare(ctx, cut.ID(), nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordMethods, "missing"), tree.Input{}); semreg.ErrorIdentifier(err) != semreg.DanglingReference {
		t.Fatalf("an unregistered native method = %v", err)
	}
}
