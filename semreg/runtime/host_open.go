package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/call"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// HostOptions are the inputs of OpenHost beyond the installation and the
// factories.
type HostOptions struct {
	// Statements are the trusted identity declarations of the embedding
	// application.
	Statements []OperatorIdentityStatement
}

// HostCut is one captured cut of the installation tree with the scoped cut
// reference it was assembled from.
type HostCut struct {
	*tree.TreeCut
	Ref graph.CutRef
	// Identity is the identity resolution at this capture's cut.
	Identity identity.Resolution
}

// Runtime is one assembled host runtime. It holds no concrete driver and no
// package-level state: two Runtime values in one process are independent.
type Runtime struct {
	plan      life.PlanSet
	byID      map[life.InstanceID]life.PlannedInstance
	svc       *Service
	recovery  *Recovery
	control   *life.FileStore
	sup       *life.Supervisor
	tree      *tree.Tree
	router    *call.Router
	cuts      *hostCuts
	lifecycle *hostLifecycle
	calendar  *hostCalendar
	pub       *hostPublication
	operation *hostOperations
	statePath string
	// exclusive is true when this runtime holds the installation lock; only
	// then may it write the identity checkpoint.
	exclusive bool
	checkMu   sync.Mutex
	captureMu chan struct{}
}

// hostRecovery runs the installation publication after the adapter restored
// the committed material or found the store empty, and before any admission.
type hostRecovery struct {
	*Recovery
	install func(context.Context) error
}

func (h hostRecovery) Restore(ctx context.Context, material *life.RecoveryMaterial) error {
	err := h.Recovery.Restore(ctx, material)
	if installErr := h.install(ctx); installErr != nil {
		return installErr
	}
	return err
}

func (h hostRecovery) RestoreEmpty(ctx context.Context) error {
	if err := h.Recovery.RestoreEmpty(ctx); err != nil {
		return err
	}
	return h.install(ctx)
}

// OpenHost plans the instances, opens the durable lifecycle, identity and
// publication state under Installation.StateDir and assembles the runtime. It
// constructs no instance: Start does.
func OpenHost(ctx context.Context, inst life.Installation, options HostOptions, factories ...driver.Factory) (*Runtime, error) {
	if inst.StateDir == "" {
		return nil, errors.New("runtime: a state directory is required")
	}
	if inst.Clock == nil {
		clock, err := newProcessClock()
		if err != nil {
			return nil, err
		}
		inst.Clock = clock
	}
	plan, err := life.Plan(inst, factories...)
	if err != nil {
		return nil, err
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	incarnation := "incarnation." + hex.EncodeToString(raw[:])
	sample, err := inst.Clock.Sample()
	if err != nil {
		return nil, fmt.Errorf("runtime: clock: %w", err)
	}
	svc, err := Open(Options{
		Incarnation: incarnation, Clock: inst.Clock,
		EvaluateMonotonic: v1.MonotonicPoint{ClockEpochID: v1.ClockEpochID("clock.epoch." + incarnation), Nanoseconds: v1.Uint64(strconv.FormatUint(sample, 10))},
	})
	if err != nil {
		return nil, err
	}
	calendar, err := newHostCalendar(inst.Calendar, localLocationName, inst.Clock.Now)
	if err != nil {
		return nil, err
	}
	identityDir := filepath.Join(inst.StateDir, "identity")
	if err := os.MkdirAll(filepath.Join(identityDir, "anchors"), 0o700); err != nil {
		return nil, err
	}
	anchors, err := identity.OpenStore(filepath.Join(identityDir, "anchors"))
	if err != nil {
		return nil, err
	}
	allocator, err := identity.NewAllocator(anchors, svc.catalog)
	if err != nil {
		return nil, err
	}
	ledger, err := identity.NewLedger(allocator)
	if err != nil {
		return nil, err
	}
	statePath := filepath.Join(identityDir, "checkpoint.json")
	if data, err := os.ReadFile(statePath); err == nil {
		if err := ledger.Restore(data); err != nil {
			return nil, fmt.Errorf("runtime: restore the identity checkpoint: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := applyOperatorStatements(ledger, options.Statements); err != nil {
		return nil, err
	}
	svc.ledger = ledger

	if err := os.MkdirAll(filepath.Join(inst.StateDir, "lifecycle"), 0o700); err != nil {
		return nil, err
	}
	control, err := life.OpenFileStore(filepath.Join(inst.StateDir, "lifecycle"))
	if err != nil {
		return nil, err
	}
	opened := false
	exclusive := control.Exclusive()
	defer func() {
		if !opened {
			control.Close()
			control.Release()
		}
	}()
	rt := &Runtime{plan: plan, byID: map[life.InstanceID]life.PlannedInstance{}, svc: svc, control: control, calendar: calendar, statePath: statePath, exclusive: exclusive, captureMu: make(chan struct{}, 1)}
	for _, planned := range plan.Instances {
		rt.byID[planned.Ref.ID()] = planned
	}
	rt.recovery = svc.BindRecovery(control)
	rt.cuts = &hostCuts{svc: svc}
	rt.lifecycle = &hostLifecycle{}
	rt.router, err = call.NewRouter(call.Config{Capture: svc, Owner: call.NewIntentOwner("runtime:host"), Now: inst.Clock.Now})
	if err != nil {
		return nil, err
	}
	rt.operation = &hostOperations{router: rt.router}
	rt.tree, err = tree.New(tree.Config{
		Registry:   tree.RegistryConfig{Installation: plan.Installation, Bridge: plan.Bridge, Incarnation: incarnation, Lifecycle: rt.lifecycle},
		Cuts:       rt.cuts,
		Operations: rt.operation,
	})
	if err != nil {
		return nil, err
	}
	byKind := map[driver.DriverKind][]driver.InstanceKey{}
	for _, planned := range plan.Instances {
		byKind[planned.Ref.Driver] = append(byKind[planned.Ref.Driver], planned.Ref.Instance)
	}
	for kind, instances := range byKind {
		if err := rt.tree.Registry.Reserve(driver.Namespace{Owner: kind, Key: driver.NamespaceID(kind)}, instances); err != nil {
			return nil, err
		}
	}
	rt.pub = &hostPublication{rt: rt}
	for _, planned := range plan.Instances {
		rt.recovery.note(rt.specFor(planned.Ref, life.LifecycleCorrelation{InstanceID: planned.Ref.ID(), Epoch: "epoch.pending", Generation: 1}))
	}
	rt.sup, err = life.NewSupervisor(plan, life.Options{
		Store: control, Recovery: hostRecovery{Recovery: rt.recovery, install: rt.install},
		Publication: rt.pub, Incarnation: incarnation,
	})
	if err != nil {
		return nil, err
	}
	rt.lifecycle.mu.Lock()
	rt.lifecycle.sup = rt.sup
	rt.lifecycle.mu.Unlock()
	if exclusive && len(options.Statements) > 0 {
		if err := rt.saveCheckpoint(); err != nil {
			return nil, err
		}
	}
	opened = true
	return rt, nil
}

// install publishes the installation site, the bridge and the link nodes. It is
// rebuilt from configuration on every start, so it is not journaled.
func (rt *Runtime) install(ctx context.Context) error {
	rt.svc.mu.Lock()
	held := rt.svc.recovery
	rt.svc.recovery = nil
	rt.svc.mu.Unlock()
	defer func() {
		rt.svc.mu.Lock()
		rt.svc.recovery = held
		rt.svc.mu.Unlock()
	}()
	return rt.svc.Install(ctx, rt.plan)
}

// saveCheckpoint writes the identity checkpoint while this runtime holds the
// installation lock. A contending or superseded host writes nothing.
func (rt *Runtime) saveCheckpoint() error {
	rt.checkMu.Lock()
	defer rt.checkMu.Unlock()
	if !rt.exclusive || !rt.control.Exclusive() {
		return nil
	}
	data, err := rt.svc.Ledger().Export()
	if err != nil {
		return err
	}
	tmp := rt.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, rt.statePath)
}

// Start restores the committed publication, publishes the installation and
// starts the instances from their durable desired state.
func (rt *Runtime) Start(ctx context.Context) error { return rt.sup.Start(ctx) }

// WaitInitial returns when each started instance has committed its initial
// publication or reported its outcome.
func (rt *Runtime) WaitInitial(ctx context.Context) error {
	if err := rt.sup.WaitInitial(ctx); err != nil {
		return err
	}
	return rt.saveCheckpoint()
}

// Close stops the instances, closes the lifecycle store and keeps the identity
// checkpoint.
func (rt *Runtime) Close(ctx context.Context) error {
	before := rt.saveCheckpoint()
	err := rt.sup.Close(ctx)
	rt.tree.Registry.Close()
	return errors.Join(err, before, rt.saveCheckpoint())
}

// Cut captures the installation tree and its semantic cut together.
func (rt *Runtime) Cut(ctx context.Context) (*HostCut, error) {
	select {
	case rt.captureMu <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-rt.captureMu }()
	var sink captureSink
	cut, err := rt.tree.Capture(withRefSink(ctx, &sink), tree.CutOptions{})
	if err != nil {
		return nil, err
	}
	return &HostCut{TreeCut: cut, Ref: sink.Ref, Identity: sink.Identity}, nil
}

// Tree is SS09's tree.
func (rt *Runtime) Tree() *tree.Tree { return rt.tree }

// Prepare prepares a method of a cut.
func (rt *Runtime) Prepare(ctx context.Context, cut tree.CutID, method tree.MethodRef, input tree.Input) (tree.Prepared, error) {
	return rt.tree.Prepare(ctx, cut, method, input)
}

// Invoke calls a prepared method once.
func (rt *Runtime) Invoke(ctx context.Context, prepared tree.Prepared) (tree.Receipt, error) {
	return rt.tree.Call(ctx, prepared, tree.CallOptions{})
}

// Outcome returns the outcome of a receipt.
func (rt *Runtime) Outcome(ctx context.Context, id tree.ReceiptID) (tree.Outcome, error) {
	return rt.tree.Outcome(ctx, id)
}

// SetDesired asks the supervisor to start, stop or restart one instance.
func (rt *Runtime) SetDesired(ctx context.Context, ref life.Ref, method life.Method, precondition life.DesiredRevision) (life.DesiredView, error) {
	return rt.sup.SetDesired(ctx, ref, method, precondition)
}

// Snapshot returns the lifecycle state of one instance as observed.
func (rt *Runtime) Snapshot(ref life.Ref) (life.Status, error) { return rt.sup.Snapshot(ref) }

// Watch returns the lifecycle state of one instance and a channel closed when it
// next changes.
func (rt *Runtime) Watch(ref life.Ref) (life.Status, <-chan struct{}, error) {
	return rt.sup.Watch(ref)
}

// Plan is the installed plan set.
func (rt *Runtime) Plan() life.PlanSet { return rt.plan }
