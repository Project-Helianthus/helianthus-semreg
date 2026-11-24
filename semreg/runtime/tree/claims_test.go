package tree_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// callTogether runs n calls of one prepared call at the same moment and returns
// how many were refused as already used.
func callTogether(t *testing.T, w *world, prepared tree.Prepared, n int) (used int) {
	t.Helper()
	var wg sync.WaitGroup
	start := make(chan struct{})
	var refused atomic.Int32
	var other atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := w.tree.Call(ctx, prepared, tree.CallOptions{Timeout: time.Second})
			switch {
			case err == nil:
			case errors.Is(err, tree.ErrUsed):
				refused.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if other.Load() != 0 {
		t.Fatalf("%d calls failed with an error other than ErrUsed", other.Load())
	}
	return int(refused.Load())
}

func TestConcurrentCallsOfOnePreparedNativeCallRunTheMethodOnce(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	w.life.set(tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}, "running", 1)
	b := w.bind("ns_a", "inst.1", 1)
	var ran atomic.Int32
	var empty atomic.Int32
	if err := b.Method(b.Root(), methodSpec("pairing"), func(_ context.Context, in []byte) ([]byte, error) {
		ran.Add(1)
		if len(in) == 0 {
			empty.Add(1)
		}
		return []byte("paired"), nil
	}); err != nil {
		t.Fatal(err)
	}
	method := nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordMethods, "pairing")
	const callers = 8
	for round := 0; round < 100; round++ {
		ran.Store(0)
		cut := w.capture()
		prepared, err := w.tree.Prepare(ctx, cut.ID(), method, tree.Input{Encoded: []byte("arg")})
		if err != nil {
			t.Fatal(err)
		}
		if used := callTogether(t, w, prepared, callers); used != callers-1 || ran.Load() != 1 || empty.Load() != 0 {
			t.Fatalf("round %d: %d refused, method ran %d times, %d with an empty argument", round, used, ran.Load(), empty.Load())
		}
		_ = cut.Close()
	}
}

func TestConcurrentCallsOfOnePreparedSemanticCallReachTheDispatcherOnce(t *testing.T) {
	w := threeResourceWorld(t)
	method := semanticMethodRef(t, setCurrent)
	const callers = 8
	for round := 0; round < 100; round++ {
		before := w.ops.calls
		cut := w.capture()
		prepared, err := w.tree.Prepare(ctx, cut.ID(), method, tree.Input{Request: &driver.OperationRequest{Timeout: time.Second}})
		if err != nil {
			t.Fatal(err)
		}
		if used := callTogether(t, w, prepared, callers); used != callers-1 || w.ops.calls-before != 1 {
			t.Fatalf("round %d: %d refused, %d dispatcher calls", round, used, w.ops.calls-before)
		}
		_ = cut.Close()
	}
}

func TestPreparedNativeCallIsRefusedAfterAnotherGenerationReplacedTheMethod(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	instance := tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}
	w.life.set(instance, "running", 1)
	first := w.bind("ns_a", "inst.1", 1)
	var oldRan, newRan int
	if err := first.Method(first.Root(), methodSpec("pairing"), func(context.Context, []byte) ([]byte, error) { oldRan++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	method := nativeRef(t, "ns_a", mustPath(t, first.Root()), tree.KeywordMethods, "pairing")
	cut := w.capture()
	prepared, err := w.tree.Prepare(ctx, cut.ID(), method, tree.Input{Encoded: []byte("old arguments")})
	if err != nil {
		t.Fatal(err)
	}
	w.life.fence(instance, 1, "stopped")
	w.life.set(instance, "running", 2)
	second := w.bind("ns_a", "inst.1", 2)
	if err := second.Method(second.Root(), methodSpec("pairing"), func(context.Context, []byte) ([]byte, error) { newRan++; return nil, nil }); err != nil {
		t.Fatalf("the new generation registers the withdrawn method again: %v", err)
	}
	if _, err := w.tree.Call(ctx, prepared, tree.CallOptions{}); !errors.Is(err, tree.ErrUnavailable) {
		t.Fatalf("a call prepared before the replacement = %v, want unavailable", err)
	}
	if oldRan != 0 || newRan != 0 {
		t.Fatalf("the replaced preparation ran a method: old %d, new %d", oldRan, newRan)
	}
	fresh := w.capture()
	again, err := w.tree.Prepare(ctx, fresh.ID(), method, tree.Input{Encoded: []byte("new arguments")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.tree.Call(ctx, again, tree.CallOptions{}); err != nil || newRan != 1 {
		t.Fatalf("a call prepared in the new generation = %v, ran %d", err, newRan)
	}
}

// collidingOps issues handles in the same form the tree issues its own.
type collidingOps struct {
	mu       sync.Mutex
	prepares int
	calls    int
}

func (o *collidingOps) Prepare(_ context.Context, _ graph.CutRef, _ semreg.DefinitionRef, input tree.Input) (tree.PortPrepared, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.prepares++
	return tree.PortPrepared{ID: tree.PreparedID("prepared-" + strconv.Itoa(o.prepares)), Request: *input.Request}, nil
}

func (o *collidingOps) Call(context.Context, tree.PortPrepared, tree.CallOptions) (tree.PortReceipt, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	result := driver.OperationResult{Status: "done"}
	return tree.PortReceipt{ID: tree.ReceiptID("receipt-" + strconv.Itoa(o.calls)), Result: &result}, nil
}

func (o *collidingOps) Outcome(context.Context, tree.ReceiptID) (driver.OperationResult, error) {
	return driver.OperationResult{Status: "from dispatcher"}, nil
}

func TestDispatcherHandlesNeverCollideWithTreeHandles(t *testing.T) {
	w := threeResourceWorld(t)
	ops := &collidingOps{}
	replaced, err := tree.New(tree.Config{
		Registry:   tree.RegistryConfig{Installation: installation, Bridge: bridge, Incarnation: incarnation, Lifecycle: w.life},
		Cuts:       w.cuts,
		Operations: ops,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.tree = replaced
	w.reserve("kind_a", "ns_a", "inst.1")
	w.life.set(tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}, "running", 1)
	b := w.bind("ns_a", "inst.1", 1)
	natives := map[string]bool{}
	if err := b.Method(b.Root(), methodSpec("pairing"), func(_ context.Context, in []byte) ([]byte, error) {
		natives[string(in)] = true
		return []byte("native " + string(in)), nil
	}); err != nil {
		t.Fatal(err)
	}
	native := nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordMethods, "pairing")
	semantic := semanticMethodRef(t, setCurrent)

	cut := w.capture()
	type handle struct {
		prepared tree.Prepared
		native   bool
		arg      string
	}
	var handles []handle
	seen := map[tree.PreparedID]bool{}
	for i := 0; i < 4; i++ {
		s, err := w.tree.Prepare(ctx, cut.ID(), semantic, tree.Input{Request: &driver.OperationRequest{Timeout: time.Second}})
		if err != nil {
			t.Fatal(err)
		}
		n, err := w.tree.Prepare(ctx, cut.ID(), native, tree.Input{Encoded: []byte(fmt.Sprint("arg", i))})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []tree.Prepared{s, n} {
			if seen[p.ID] {
				t.Fatalf("prepared handle %s was issued twice", p.ID)
			}
			seen[p.ID] = true
		}
		handles = append(handles, handle{s, false, ""}, handle{n, true, fmt.Sprint("arg", i)})
	}
	receipts := map[tree.ReceiptID]bool{}
	for _, h := range handles {
		receipt, err := w.tree.Call(ctx, h.prepared, tree.CallOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if receipts[receipt.ID] {
			t.Fatalf("receipt %s was issued twice", receipt.ID)
		}
		receipts[receipt.ID] = true
		outcome, err := w.tree.Outcome(ctx, receipt.ID)
		if err != nil {
			t.Fatal(err)
		}
		if h.native && (string(outcome.Native) != "native "+h.arg || outcome.Operation != nil) {
			t.Fatalf("native outcome = %+v", outcome)
		}
		if !h.native && (outcome.Operation == nil || outcome.Operation.Status != "from dispatcher") {
			t.Fatalf("semantic outcome = %+v", outcome)
		}
	}
	if ops.prepares != 4 || ops.calls != 4 || len(natives) != 4 {
		t.Fatalf("dispatcher prepares %d, calls %d, native inputs %v", ops.prepares, ops.calls, natives)
	}
	if _, err := w.tree.Outcome(ctx, "receipt-9999"); semreg.ErrorIdentifier(err) != semreg.DanglingReference {
		t.Fatalf("an unissued receipt = %v", err)
	}
}

func TestCaptureHoldsEveryRegistrationEitherInItOrAfterItsCursor(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	w.life.set(tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}, "running", 1)
	b := w.bind("ns_a", "inst.1", 1)
	base := w.capture()
	origin := base.Cursor().Native
	_ = base.Close()

	const registrations = 400
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < registrations; i++ {
			if err := b.ObserveMember(b.Root(), memberSpec(fmt.Sprint("member.", i)), (&retained{}).read); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	check := func() {
		cut := w.capture()
		defer func() { _ = cut.Close() }()
		listed, err := cut.List(ctx, b.Root(), tree.Page{Size: registrations + 10, Filter: tree.Filter{Kind: tree.EntryAttribute}})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := uint64(len(listed.Entries)), cut.Cursor().Native-origin; got != want {
			t.Fatalf("capture lists %d members but its cursor is %d registrations after the start", got, want)
		}
	}
	for finished := false; !finished; {
		select {
		case <-done:
			finished = true
		default:
		}
		check()
	}
	check()
}
