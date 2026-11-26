package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/call"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// hostCuts is the tree's cut port over the SS07 service.
type hostCuts struct {
	svc *Service
}

// captureSink receives what one capture produced: the scoped cut reference and
// the identity resolution at that same cut, so a caller reads the values of its
// own capture.
type captureSink struct {
	Ref      graph.CutRef
	Identity identity.Resolution
	Links    []identity.Link
}

type refSinkKey struct{}

func withRefSink(ctx context.Context, sink *captureSink) context.Context {
	return context.WithValue(ctx, refSinkKey{}, sink)
}

var _ tree.CutSource = (*hostCuts)(nil)

func (h *hostCuts) viewOf(v View) tree.CutView {
	return tree.CutView{
		Ref: v.Ref, Cut: v.Cut, Members: v.Members, Store: h.svc.store, Catalog: v.Catalog,
		Evaluation: v.Evaluation, Cursor: v.Cursor, SettingRoutes: v.Routes,
	}
}

func (h *hostCuts) Capture(ctx context.Context, filter driver.Filter) (tree.CutView, error) {
	for {
		view, err := h.svc.CaptureView(ctx, filter)
		if err != nil {
			return tree.CutView{}, err
		}
		resolution, err := h.svc.ledger.Resolve(view.Cut)
		if err != nil {
			return tree.CutView{}, err
		}
		links := h.svc.ledger.Links()
		// A commit holds the service lock from its identity claims to the
		// advance of the position. The identity read describes the captured
		// state only when the position is still the capture's; otherwise a
		// commit landed in between and the capture is taken again.
		h.svc.mu.Lock()
		same := h.svc.cursorAt(h.svc.position) == view.Cursor
		h.svc.mu.Unlock()
		if !same {
			if err := ctx.Err(); err != nil {
				return tree.CutView{}, err
			}
			continue
		}
		if sink, ok := ctx.Value(refSinkKey{}).(*captureSink); ok {
			sink.Ref, sink.Identity, sink.Links = view.Ref, resolution, links
		}
		return h.viewOf(view), nil
	}
}

func (h *hostCuts) Lookup(ctx context.Context, ref graph.CutRef) (tree.CutView, error) {
	view, err := h.svc.Lookup(ctx, ref)
	if err != nil {
		return tree.CutView{}, err
	}
	return h.viewOf(view), nil
}

func (h *hostCuts) Resume(ctx context.Context, cursor driver.Cursor, filter driver.Filter) (driver.Changes, error) {
	return h.svc.Resume(ctx, cursor, filter)
}

// hostLifecycle is the tree's lifecycle port over the supervisor. Fence
// callbacks fire when a generation actually stops.
type hostLifecycle struct {
	mu   sync.Mutex
	sup  *life.Supervisor
	next int
	subs map[int]func(tree.Fence)
}

var _ tree.LifecycleSource = (*hostLifecycle)(nil)

func (l *hostLifecycle) State(ref tree.InstanceRef) (tree.InstanceState, error) {
	l.mu.Lock()
	sup := l.sup
	l.mu.Unlock()
	if sup == nil {
		return tree.InstanceState{}, errors.New("runtime: the supervisor is not open")
	}
	status, err := sup.Snapshot(life.Ref{Driver: ref.Driver, Instance: ref.Instance})
	if err != nil {
		return tree.InstanceState{}, err
	}
	return tree.InstanceState{Name: string(status.ObservedState), Generation: uint64(status.Generation)}, nil
}

func (l *hostLifecycle) OnFence(fn func(tree.Fence)) func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.subs == nil {
		l.subs = map[int]func(tree.Fence){}
	}
	l.next++
	id := l.next
	l.subs[id] = fn
	return func() {
		l.mu.Lock()
		delete(l.subs, id)
		l.mu.Unlock()
	}
}

func (l *hostLifecycle) fence(f tree.Fence) {
	l.mu.Lock()
	fns := make([]func(tree.Fence), 0, len(l.subs))
	for _, fn := range l.subs {
		fns = append(fns, fn)
	}
	l.mu.Unlock()
	for _, fn := range fns {
		fn(f)
	}
}

// hostOperations is the tree's operation port over the SS08 router.
type hostOperations struct {
	router *call.Router

	mu       sync.Mutex
	sequence uint64
	results  map[tree.ReceiptID]driver.OperationResult
}

var _ tree.OperationPort = (*hostOperations)(nil)

func (o *hostOperations) Prepare(_ context.Context, cut graph.CutRef, operation semreg.DefinitionRef, input tree.Input) (tree.PortPrepared, error) {
	if input.Request == nil {
		return tree.PortPrepared{}, errors.New("runtime: a semantic method takes an operation request")
	}
	if input.Request.Operation != operation {
		return tree.PortPrepared{}, errors.New("runtime: the request names another operation than the selected method")
	}
	inCut := false
	for _, member := range cut.Members {
		if member.AssetID == input.Request.AssetID {
			inCut = true
			break
		}
	}
	if !inCut {
		return tree.PortPrepared{}, errors.New("runtime: the request names an asset that is not in the selected cut")
	}
	// The prepared request keeps its own copy of the typed input, so a later
	// edit of the caller's slices leaves what was prepared unchanged.
	encoded, err := json.Marshal(input.Request)
	if err != nil {
		return tree.PortPrepared{}, err
	}
	var request driver.OperationRequest
	if err := json.Unmarshal(encoded, &request); err != nil {
		return tree.PortPrepared{}, err
	}
	request.Timeout = input.Request.Timeout
	o.mu.Lock()
	o.sequence++
	id := tree.PreparedID(fmt.Sprintf("op-prepared-%d", o.sequence))
	o.mu.Unlock()
	return tree.PortPrepared{ID: id, Request: request}, nil
}

func (o *hostOperations) Call(ctx context.Context, prepared tree.PortPrepared, options tree.CallOptions) (tree.PortReceipt, error) {
	request := prepared.Request
	if request.Timeout <= 0 {
		request.Timeout = options.Timeout
	}
	ctx = call.WithCaller(ctx, call.Caller{Surface: hostSurface()})
	result, err := o.router.Invoke(ctx, request)
	if err != nil {
		return tree.PortReceipt{}, err
	}
	o.mu.Lock()
	o.sequence++
	id := tree.ReceiptID(fmt.Sprintf("op-receipt-%d", o.sequence))
	if o.results == nil {
		o.results = map[tree.ReceiptID]driver.OperationResult{}
	}
	o.results[id] = result
	o.mu.Unlock()
	return tree.PortReceipt{ID: id, Result: &result}, nil
}

func (o *hostOperations) Outcome(_ context.Context, id tree.ReceiptID) (driver.OperationResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	result, ok := o.results[id]
	if !ok {
		return driver.OperationResult{}, errors.New("runtime: no operation receipt")
	}
	return result, nil
}

// hostInput is the SemanticInput of one consuming session: SS07 capture and
// resume through SS09's scoped input over the same cut source, with the
// session's pin kept for later formula publication.
type hostInput struct {
	scoped  *tree.ScopedInput
	session *Session
}

var _ driver.SemanticInput = (*hostInput)(nil)

func (h *hostInput) Capture(ctx context.Context, filter driver.Filter) (driver.SemanticCut, error) {
	cut, err := h.scoped.Capture(ctx, filter)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	h.session.Pin(cut.Ref)
	return cut, nil
}

func (h *hostInput) Resume(ctx context.Context, cursor driver.Cursor, filter driver.Filter) (driver.Changes, error) {
	return h.scoped.Resume(ctx, cursor, filter)
}

func (h *hostInput) RetrieveLens(ctx context.Context, ref graph.CutRef, request graph.LensRequest) (graph.LensResult, error) {
	return h.scoped.RetrieveLens(ctx, ref, request)
}

func (h *hostInput) SumMembers(ctx context.Context, ref graph.CutRef, request graph.LensRequest, fact semreg.DefinitionRef, scope semreg.ResourceID) (graph.MemberSum, error) {
	return h.scoped.SumMembers(ctx, ref, request, fact, scope)
}
