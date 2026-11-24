package life

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Publication is the lifecycle-to-publication handoff of one runtime. The
// publication layer implements it: it hands each generation its session and
// withdraws everything published under a correlation. The lifecycle package
// constructs no publication of its own.
type Publication interface {
	// OpenSession returns the session of one generation. Everything the instance
	// stages through it carries the correlation's instance, epoch and
	// generation.
	OpenSession(ctx context.Context, corr LifecycleCorrelation) (driver.Session, error)
	// Withdraw withdraws what was registered in the correlation's generation:
	// effective bindings, extensions, native handles and operations. It is
	// idempotent and also settles a correlation of an earlier process, for which
	// no instance exists.
	Withdraw(ctx context.Context, corr LifecycleCorrelation) error
}

// withdrawPendingError is a start refused while the withdrawal of an earlier
// failed start is still outstanding. It is a retryable failure: the manager's
// retry series owns the next attempt, which settles the withdrawal first.
type withdrawPendingError struct{ err error }

func (e *withdrawPendingError) Error() string {
	return "life: the withdrawal of the failed start is still pending: " + e.err.Error()
}
func (e *withdrawPendingError) Unwrap() error   { return e.err }
func (e *withdrawPendingError) Retryable() bool { return true }

var errFencedGeneration = errors.New("life: the generation is fenced")

// tracker follows the publication transactions of one native generation. A
// generation is held out of RUNNING while a transaction it began is staged and
// not yet committed or aborted, so initial publications are staged before
// readiness is announced.
type tracker struct {
	mu     sync.Mutex
	open   int
	fenced bool
	idle   chan struct{}
}

func newTracker() *tracker { return &tracker{idle: make(chan struct{})} }

func (t *tracker) begin() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fenced {
		return errFencedGeneration
	}
	if t.open == 0 {
		t.idle = make(chan struct{})
	}
	t.open++
	return nil
}

func (t *tracker) end() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.open--
	if t.open == 0 {
		close(t.idle)
	}
}

func (t *tracker) fence() {
	t.mu.Lock()
	t.fenced = true
	t.mu.Unlock()
}

// waitIdle returns once no transaction of the generation is staged.
func (t *tracker) waitIdle(ctx context.Context) error {
	for {
		t.mu.Lock()
		if t.open == 0 {
			t.mu.Unlock()
			return nil
		}
		idle := t.idle
		t.mu.Unlock()
		select {
		case <-idle:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

type trackedSession struct {
	driver.Session
	track *tracker
}

func (s trackedSession) Publisher() driver.Publisher {
	return trackedPublisher{inner: s.Session.Publisher(), track: s.track}
}

type trackedPublisher struct {
	inner driver.Publisher
	track *tracker
}

func (p trackedPublisher) Begin(ctx context.Context) (driver.PublicationTransaction, error) {
	if err := p.track.begin(); err != nil {
		return nil, err
	}
	tx, err := p.inner.Begin(ctx)
	if err != nil {
		p.track.end()
		return nil, err
	}
	return &trackedTransaction{PublicationTransaction: tx, track: p.track}, nil
}

type trackedTransaction struct {
	driver.PublicationTransaction
	track *tracker
	once  sync.Once
}

func (t *trackedTransaction) Commit(ctx context.Context) error {
	err := t.PublicationTransaction.Commit(ctx)
	t.once.Do(t.track.end)
	return err
}

func (t *trackedTransaction) Abort(ctx context.Context) error {
	err := t.PublicationTransaction.Abort(ctx)
	t.once.Do(t.track.end)
	return err
}

// instanceRuntime is the Runtime of one planned instance: each native
// generation constructs the instance afresh and starts it on a session of the
// publication handoff.
type instanceRuntime struct {
	id      InstanceID
	plan    driver.InstancePlan
	factory driver.Factory
	pub     Publication

	mu          sync.Mutex
	inst        driver.Instance
	track       *tracker
	corr        LifecycleCorrelation
	generation  uint64
	revision    uint64
	quarantined bool
	// pending is the correlation of a failed start whose withdrawal did not
	// complete: the cleanup obligation stays until a withdrawal succeeds, and no
	// later start proceeds over it.
	pending    LifecycleCorrelation
	hasPending bool
}

func newInstanceRuntime(id InstanceID, pi PlannedInstance, pub Publication) *instanceRuntime {
	return &instanceRuntime{id: id, plan: pi.Plan, factory: pi.factory, pub: pub, revision: 1}
}

// bind records the correlation the next native generation belongs to.
func (r *instanceRuntime) bind(corr LifecycleCorrelation) {
	r.mu.Lock()
	r.corr = corr
	r.mu.Unlock()
}

func (r *instanceRuntime) Generation() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.generation
}

func (r *instanceRuntime) Revision() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revision
}

func (r *instanceRuntime) SafetyQuarantined() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.quarantined
}

// FenceWithdrawal is nonblocking: it fences the generation's publications and
// the instance's own work. Resource drain and close proof remain Stop's job.
func (r *instanceRuntime) FenceWithdrawal() {
	r.mu.Lock()
	inst, track := r.inst, r.track
	r.mu.Unlock()
	if track != nil {
		track.fence()
	}
	if inst != nil {
		inst.FenceWithdrawal()
	}
}

// Start constructs the instance and starts it on its session. A failed start
// leaves nothing open: a partially started instance is stopped, and an
// unconfirmed close quarantines the runtime.
func (r *instanceRuntime) Start(ctx context.Context) (uint64, error) {
	r.mu.Lock()
	corr := r.corr
	pending, hasPending := r.pending, r.hasPending
	r.mu.Unlock()
	if hasPending {
		if err := r.pub.Withdraw(ctx, pending); err != nil {
			return 0, &withdrawPendingError{err: err}
		}
		r.mu.Lock()
		if r.hasPending && r.pending == pending {
			r.hasPending = false
		}
		r.mu.Unlock()
	}
	inst, err := r.factory.Construct(ctx, r.plan)
	if err != nil {
		return 0, err
	}
	if inst == nil {
		return 0, errors.New("life: the factory constructed no instance")
	}
	session, err := r.pub.OpenSession(ctx, corr)
	if err != nil {
		// The constructed instance is stopped before the error is returned: its
		// close result decides quarantine and the release of the store lock.
		r.retire(inst, newTracker())
		return 0, err
	}
	track := newTracker()
	r.mu.Lock()
	r.inst, r.track = inst, track
	r.mu.Unlock()
	if err := inst.Start(ctx, trackedSession{Session: session, track: track}); err != nil {
		r.retire(inst, track)
		if werr := r.withdrawFailed(corr); werr != nil {
			return 0, errors.Join(err, werr)
		}
		return 0, err
	}
	r.mu.Lock()
	r.generation++
	r.revision++
	generation := r.generation
	r.mu.Unlock()
	return generation, nil
}

// Replace closes the current instance and withdraws what its generation
// registered, then starts a new native generation.
func (r *instanceRuntime) Replace(ctx context.Context) (uint64, error) {
	r.mu.Lock()
	corr := r.corr
	r.mu.Unlock()
	if err := r.Stop(ctx); err != nil {
		return 0, err
	}
	if err := r.pub.Withdraw(ctx, corr); err != nil {
		return 0, err
	}
	return r.Start(ctx)
}

// Stop stops the current instance. An unconfirmed close quarantines the
// runtime and is returned as ErrSafetyQuarantined.
func (r *instanceRuntime) Stop(ctx context.Context) error {
	r.mu.Lock()
	inst, track := r.inst, r.track
	r.mu.Unlock()
	if inst == nil {
		return nil
	}
	if track != nil {
		track.fence()
	}
	report := inst.Stop(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inst == inst {
		r.inst, r.track = nil, nil
	}
	if !report.Confirmed {
		r.quarantined = true
		if report.Err != nil {
			return fmt.Errorf("%w: %v", ErrSafetyQuarantined, report.Err)
		}
		return ErrSafetyQuarantined
	}
	return nil
}

// retire closes a partially started instance on the manager's independent
// late-retirement deadline: nothing stays open after a failed start.
func (r *instanceRuntime) retire(inst driver.Instance, track *tracker) {
	track.fence()
	ctx, cancel := context.WithTimeout(context.Background(), defaultLateRetireTimeout)
	defer cancel()
	report := inst.Stop(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inst == inst {
		r.inst, r.track = nil, nil
	}
	if !report.Confirmed {
		r.quarantined = true
	}
}

// withdrawFailed withdraws what a failed start registered, on the manager's
// independent late-retirement deadline. A withdrawal that fails stays pending,
// and the next start settles it first.
func (r *instanceRuntime) withdrawFailed(corr LifecycleCorrelation) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultLateRetireTimeout)
	defer cancel()
	err := r.pub.Withdraw(ctx, corr)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.pending, r.hasPending = corr, true
		return fmt.Errorf("life: withdrawing the failed start: %w", err)
	}
	r.hasPending = false
	return nil
}

// holdAdmission is the manager's admission hold: the generation is admitted
// only once its staged publications are committed or aborted.
func (r *instanceRuntime) holdAdmission(ctx context.Context, _ AdmissionHold) error {
	r.mu.Lock()
	track := r.track
	r.mu.Unlock()
	if track == nil {
		return nil
	}
	return track.waitIdle(ctx)
}
