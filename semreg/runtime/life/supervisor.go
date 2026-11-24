package life

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Options are the inputs of a Supervisor besides the plan set.
type Options struct {
	// Store holds the desired-state decisions, the accepted transitions and the
	// recovery material: MemoryStore, or FileStore under the installation's
	// state directory.
	Store ControlStore
	// Recovery is the lifecycle-to-publication recovery handoff. There is no
	// default.
	Recovery RecoveryAdapter
	// Publication hands each generation its session and withdraws what the
	// generation published.
	Publication Publication
	// Incarnation names this process; every source epoch is derived from it, so
	// epochs never repeat across processes that carry distinct names.
	Incarnation string
	// OnError receives internal errors with their identity preserved.
	OnError func(error)
}

// Status is the observable state of one instance.
type Status = DriverStatus

// Supervisor runs the planned instances: it decides start, stop and restart of
// each through the control store, starts and stops each under generation fences
// and resumes the accepted work a process loss interrupted.
type Supervisor struct {
	set     PlanSet
	opts    Options
	manager *Manager
	service *Service
	byID    map[InstanceID]*supervised

	mu       sync.Mutex
	started  bool
	closed   bool
	expected []InstanceID
	done     chan struct{}
}

type supervised struct {
	planned  PlannedInstance
	runtime  *instanceRuntime
	executor *instanceExecutor
}

// NewSupervisor builds the supervisor of an installed plan set. It constructs no
// instance and performs no I/O.
func NewSupervisor(set PlanSet, opts Options) (*Supervisor, error) {
	switch {
	case isNil(opts.Store):
		return nil, fmt.Errorf("%w: missing control store", ErrInvalidConfig)
	case isNil(opts.Recovery):
		return nil, errNoRecoveryAdapter
	case isNil(opts.Publication):
		return nil, fmt.Errorf("%w: missing publication handoff", ErrInvalidConfig)
	case isNil(set.Clock):
		return nil, fmt.Errorf("%w: missing installation clock", ErrInvalidConfig)
	}
	s := &Supervisor{set: set, opts: opts, byID: map[InstanceID]*supervised{}, done: make(chan struct{})}
	managed := make([]ManagedConfig, 0, len(set.Instances))
	instances := make([]ServiceInstance, 0, len(set.Instances))
	for _, pi := range set.Instances {
		id := pi.Ref.ID()
		rt := newInstanceRuntime(id, pi, opts.Publication)
		managed = append(managed, ManagedConfig{
			ID: string(id), Enabled: pi.Enabled, Runtime: rt, ClassifyError: classifyError, Retry: pi.Plan.RetryPolicy,
		})
		s.byID[id] = &supervised{planned: pi, runtime: rt}
		instances = append(instances, ServiceInstance{InstanceID: id})
	}
	manager, err := NewManager(ManagerConfig{Drivers: managed, Now: set.Clock.Now})
	if err != nil {
		return nil, err
	}
	s.manager = manager
	for i := range instances {
		id := instances[i].InstanceID
		sv := s.byID[id]
		sv.executor = newInstanceExecutor(id, manager, sv.runtime, opts.Publication, opts.Incarnation, opts.Store.Exclusive)
		if err := manager.BindHooks(string(id), LifecycleHooks{HoldAdmission: sv.runtime.holdAdmission, Closed: sv.executor.observeClose}); err != nil {
			return nil, err
		}
		instances[i].Executor = sv.executor
	}
	service, err := NewService(ServiceOptions{
		Store: opts.Store, Recovery: opts.Recovery, Drivers: instances, Incarnation: opts.Incarnation,
		Now: set.Clock.Now, OnError: opts.OnError,
	})
	if err != nil {
		return nil, err
	}
	s.service = service
	for _, sv := range s.byID {
		sv.executor.observe = service.ObserveNative
	}
	return s, nil
}

// classifyError is the neutral failure classification of a start error: an
// error that reports Retryable is a retryable dependency failure of the
// manager's retry series; any other error is final.
func classifyError(err error) Failure {
	reason := ReasonInternalError
	var coded interface{ ReasonCode() ReasonCode }
	if errors.As(err, &coded) {
		reason = coded.ReasonCode()
	}
	failure := Failure{Reason: Reason{Code: reason}}
	if retryable(err) {
		failure.Reason = Reason{Code: ReasonDependencyUnavailable, Retryable: true}
		var detailed interface{ FailureDetail() FailureDetail }
		if errors.As(err, &detailed) {
			failure.Detail = detailed.FailureDetail()
		}
	}
	return failure
}

func (s *Supervisor) controlInstances() []ControlInstance {
	out := make([]ControlInstance, 0, len(s.set.Instances))
	for _, pi := range s.set.Instances {
		out = append(out, ControlInstance{InstanceID: pi.Ref.ID(), Enabled: pi.Enabled})
	}
	return out
}

// Start starts the instances from the desired state in the control store,
// subject to configuration enablement. The first start of an empty store seeds
// it once: each configured instance is recorded enabled as start and disabled
// as stop. Committed recovery material is restored through the adapter before
// any executor call or admission, and every enabled instance first resumes the
// accepted transition restored from the store.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started || s.closed {
		s.mu.Unlock()
		return ErrServiceState
	}
	s.started = true
	s.mu.Unlock()

	plan, err := s.opts.Store.Register(ctx, s.controlInstances())
	if err != nil {
		return err
	}
	err = s.service.Start(ctx)
	if err != nil && !s.service.running() {
		return err
	}
	s.noteExpected(plan)
	return err
}

// noteExpected records the instances whose start Start launched: enabled and
// desired start, and not withheld by a failed recovery.
func (s *Supervisor) noteExpected(plan []ControlStartup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range plan {
		if entry.Action != StartupStarting {
			continue
		}
		if s.service.recoveryWithheld(entry.InstanceID) {
			continue
		}
		s.expected = append(s.expected, entry.InstanceID)
	}
}

// WaitInitial returns when each started instance has committed its initial
// publication or reported its outcome. An instance is reported ready only after
// its manager admitted it, which follows its staged publications.
func (s *Supervisor) WaitInitial(ctx context.Context) error {
	s.mu.Lock()
	expected := append([]InstanceID(nil), s.expected...)
	s.mu.Unlock()
	for {
		notify := s.service.EventNotify()
		pending := false
		for _, id := range expected {
			// An instance the cached view does not carry has published nothing
			// and recorded no outcome: it stays pending.
			st, published := s.service.viewEntry(id)
			if !published || st.ObservedState == ObservedStarting || st.ObservedState == "" {
				pending = true
				break
			}
		}
		if !pending {
			return nil
		}
		select {
		case <-notify:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// SetDesired asks the supervisor to start, stop or restart one instance. The
// precondition is the instance's desired revision the caller last saw: a
// different current revision is refused as a revision conflict.
func (s *Supervisor) SetDesired(ctx context.Context, ref Ref, method Method, precondition DesiredRevision) (DesiredView, error) {
	return s.service.Decide(ctx, method, Request{InstanceID: ref.ID(), ExpectedRevision: precondition})
}

// Snapshot returns the cached view's state of one instance. A served instance
// the view does not carry returns ErrInstanceAbsent and no desired or observed
// outcome.
func (s *Supervisor) Snapshot(ref Ref) (Status, error) {
	return s.service.Get(ref.ID())
}

// Watch returns the cached view's state of one instance and a channel closed
// when that state next changes. A served instance the view does not carry
// returns ErrInstanceAbsent and no desired or observed outcome.
func (s *Supervisor) Watch(ref Ref) (Status, <-chan struct{}, error) {
	id := ref.ID()
	for {
		notify := s.service.EventNotify()
		status, err := s.service.Get(id)
		if err != nil {
			return Status{}, nil, err
		}
		out := make(chan struct{})
		go func() {
			defer close(out)
			for {
				select {
				case <-notify:
				case <-s.done:
					return
				}
				next, err := s.service.Get(id)
				if err != nil || !sameState(next, status) {
					return
				}
				notify = s.service.EventNotify()
			}
		}()
		return status, out, nil
	}
}

// Close stops the reconcilers and every running instance, then closes the
// control store. The store's hold is relinquished only when every native close
// was proven: a native owner that can still publish keeps it, so a second host
// never joins it.
func (s *Supervisor) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()
	s.service.Close()
	err := s.manager.Shutdown(ctx)
	safe := err == nil
	for _, sv := range s.byID {
		if !sv.executor.nativeSafe() {
			safe = false
		}
	}
	// The generations the native owners published are withdrawn once those
	// owners have stopped; a failed withdrawal is reported.
	var withdrawn []error
	for _, pi := range s.set.Instances {
		if werr := s.byID[pi.Ref.ID()].executor.withdrawRemaining(ctx); werr != nil {
			withdrawn = append(withdrawn, werr)
		}
	}
	err = errors.Join(append([]error{err}, withdrawn...)...)
	if err == nil && !safe {
		err = errNativeCloseUnproven
	}
	s.opts.Store.Close()
	if safe {
		s.opts.Store.Release()
	}
	return err
}

// sameState compares two statuses of one instance except for the catalog
// revision, which any instance's event advances.
func sameState(a, b Status) bool {
	a.Revision, b.Revision = 0, 0
	return a.NextAttemptUTC.Equal(b.NextAttemptUTC) && withoutSchedule(a) == withoutSchedule(b)
}

func withoutSchedule(s Status) Status {
	s.NextAttemptUTC = time.Time{}
	return s
}
