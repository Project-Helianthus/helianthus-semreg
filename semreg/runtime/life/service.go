package life

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Service bounds. The operation budget and attempt limit are the contract's
// per-operation semantic bounds (8 attempts within 30 seconds); they persist
// in the typed slot and are never renewed by a wake, a no-op or recovery.
const (
	DefaultOperationBudget = 30 * time.Second
	DefaultMaxAttempts     = 8
	MaxDiagnostics         = 32
	MaxDiagnosticBytes     = 512
	maxIncarnationBytes    = 64
	maxPublishAttempts     = 4
	maxCommitAttempts      = 3
	maxPendingEvents       = 256
)

// Service errors outside the refusal table.
var (
	ErrInvalidConfig   = errors.New("life: invalid service configuration")
	ErrServiceState    = errors.New("life: service not started or already closed")
	ErrStaleSlot       = errors.New("life: work slot no longer current")
	ErrInvalidWork     = errors.New("life: invalid work update")
	ErrInvalidRecovery = errors.New("life: invalid recovery material")
	// ErrReadbackUnresolved reports that the bounded readback resolver ended
	// without reopening admission; control stays closed (fail closed).
	ErrReadbackUnresolved = errors.New("life: ambiguous persistence unresolved")
)

var incarnationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ServiceInstance is one instance served by the control service. Configured
// eligibility (enabled or disabled) comes from the store's committed view.
type ServiceInstance struct {
	InstanceID InstanceID
	Executor   LifecycleExecutor
}

// ServiceOptions configures a Service. Store, Recovery, Incarnation and at least one
// driver are required; there is no default recovery adapter and no fake
// success path.
type ServiceOptions struct {
	Store    Store
	Recovery RecoveryAdapter
	Drivers  []ServiceInstance
	// Incarnation names this process incarnation; every source epoch the
	// service allocates is derived from it, so epochs never repeat across
	// incarnations when the caller supplies a fresh value per process.
	Incarnation     string
	Now             func() time.Time
	OperationBudget time.Duration
	MaxAttempts     int
	// RetryDelay returns the backoff before the next attempt; attempt counts
	// the attempts already used.
	RetryDelay    func(attempt int) time.Duration
	EventCapacity int
	// OnError receives internal errors with their identity preserved: a
	// *SemanticError for recovery-adapter failures, a *LifecycleError for
	// executor failures and the store's own error for persistence failures.
	OnError func(error)
}

// DriverStatus is one entry of the cached catalog view. It carries no
// endpoint, credential, raw error, principal or authority material.
type DriverStatus struct {
	InstanceID        InstanceID
	Enabled           bool
	DesiredState      DesiredState
	DesiredRevision   DesiredRevision
	ObservedState     ObservedState
	Reason            ReasonCode
	SafetyQuarantined bool
	Revision          CatalogRevision
	// Epoch and Generation name the lifecycle correlation the observation
	// refers to; they are empty and zero before the instance's first attempt in
	// this process.
	Epoch      SourceEpoch
	Generation DriverGeneration
	// NextAttempt and NextAttemptUTC are the executor's scheduled retry while
	// ObservedState is a BACKOFF it reported (Service.ObserveNative): the
	// ordinal, from one, of the next attempt in the current failure series
	// and the time it is due. They are zero in every other state.
	NextAttempt    int
	NextAttemptUTC time.Time
	// LastFailureClass and LastFailureCause describe the failure that
	// scheduled that retry, as the executor reported it: one of the closed
	// failure classes (empty when the executor named none) and display text
	// that is bounded, printable ASCII and free of addresses. Both are empty
	// in every state but a BACKOFF with an executor-reported failure.
	// DriverStatus.Recovery is their rendering.
	LastFailureClass FailureClass
	LastFailureCause string
}

// Catalog is one coherent cached view: every served driver sorted bytewise by
// InstanceID, each entry carrying the view revision. EventCursor is the cursor
// of the newest event emitted at or before this view.
type Catalog struct {
	Revision    CatalogRevision
	EventCursor uint64
	Drivers     []DriverStatus
}

// SemanticError wraps a recovery-adapter (semantic registry) failure. It
// preserves the original error unchanged for errors.Is and errors.As and is
// never converted into a lifecycle refusal code.
type SemanticError struct {
	InstanceID InstanceID
	Op         string
	Err        error
}

func (e *SemanticError) Error() string {
	return fmt.Sprintf("life: semantic recovery %s failed for %q: %v", e.Op, e.InstanceID, e.Err)
}

func (e *SemanticError) Unwrap() error { return e.Err }

// LifecycleError wraps a lifecycle executor failure of one typed or untyped
// attempt. It is distinct from Refusal (a request refusal) and SemanticError.
type LifecycleError struct {
	InstanceID InstanceID
	Kind       TransitionKind
	Err        error
}

func (e *LifecycleError) Error() string {
	return fmt.Sprintf("life: lifecycle %s failed for %q: %v", e.Kind, e.InstanceID, e.Err)
}

func (e *LifecycleError) Unwrap() error { return e.Err }

// Diagnostic is one sanitized terminal summary kept in a bounded in-memory
// ring. It is never used for admission, deduplication or recovery and is
// dropped with the process.
type Diagnostic struct {
	Kind            TransitionKind
	DesiredRevision DesiredRevision
	Outcome         string
	AttemptsUsed    int
	Summary         string
}

type observation struct {
	state       ObservedState
	reason      ReasonCode
	quarantined bool
	epoch       SourceEpoch
	generation  DriverGeneration
	// nextAttempt and nextAttemptUTC carry an executor-reported BACKOFF
	// schedule; zero for every service-originated observation. failureClass
	// and failureCause describe the failure that scheduled it, with the cause
	// already bounded and address-free.
	nextAttempt    int
	nextAttemptUTC time.Time
	failureClass   FailureClass
	failureCause   string
}

// attempt is one in-memory start attempt and its correlation. fenced is
// guarded by the owning driverState.mu.
type attempt struct {
	corr     LifecycleCorrelation
	ctx      context.Context
	cancel   context.CancelFunc
	fenced   bool
	sequence LifecycleSequence // typed slot sequence, 0 for an untyped attempt
	admitted bool
	// recovering marks an admission into the native owner's recovery
	// (RecoveringStart) rather than RUNNING.
	recovering bool
	// pendingGen marks a retry whose generation increment is not yet
	// committed; only the driver's reconciler touches it.
	pendingGen bool
	// chargedTo is the AttemptsUsed value this attempt's charge commit tried
	// to persist. A commit with an unknown outcome that readback later
	// adopts leaves the stored count at this value, and the next pass starts
	// the attempt instead of charging again. Reconciler-only.
	chargedTo int
	// deadline and attemptsUsed carry an untyped attempt's nonrenewable
	// budget so a synthesized cancellation inherits it; attemptsUsed is
	// guarded by the owning driverState.mu.
	deadline     time.Time
	attemptsUsed int
}

type driverState struct {
	id   InstanceID
	exec LifecycleExecutor
	wake chan struct{}

	// Lock order: mu before Service.viewMu when both are needed.
	mu   sync.Mutex
	cond *sync.Cond
	// deciding counts decisions whose planner ran for this driver and that
	// are not yet released: a committed decision is released only after its
	// DESIRED_ACCEPTED is published, a decision with an unknown outcome only
	// once the readback settled it. While it is raised no attempt settles, no
	// slot is pruned and the reconciler takes no step for the driver.
	deciding int
	// ambiguous retains a planned decision whose commit outcome is unknown.
	// Its deciding count stays raised (the hold) until resolveAmbiguous
	// reads the committed record after admission reopened.
	ambiguous *ambiguousDecision
	// ambiguousHeld mirrors ambiguous != nil for publication without mu.
	// Update both under mu; viewMu holders must never acquire mu.
	ambiguousHeld   atomic.Bool
	pruning         bool
	untyped         *attempt
	inflight        *attempt
	live            *attempt
	quarantined     bool
	recoveryBlocked bool
	// cleanupRetries counts consecutive unfinished semantic cleanups.
	cleanupRetries int
	// cleanupChargeSeq and cleanupChargeTo identify the last cleanup charge
	// whose commit failed, so an adopted charge is not charged twice.
	// Reconciler-only.
	cleanupChargeSeq LifecycleSequence
	cleanupChargeTo  int
	autoStartDone    bool
	epochOrdinal     uint64
	// corr is the correlation the driver's current observation refers to:
	// the attempt being started or the generation being withdrawn.
	corr LifecycleCorrelation
	// native is the newest executor-originated observation awaiting the
	// reconciler (ObserveNative); nil when none is queued.
	native *pendingNative

	diagMu sync.Mutex
	diags  []Diagnostic
}

// Service is the internal desired-state service. Its only global lock
// (viewMu) guards in-memory view assembly and is never held across store
// writes, executor calls or recovery-adapter calls; list and get read an
// atomically published view and never wait.
type Service struct {
	store       Store
	recovery    RecoveryAdapter
	incarnation string
	now         func() time.Time
	budget      time.Duration
	maxAttempts int
	retryDelay  func(int) time.Duration
	onError     func(error)

	drivers map[InstanceID]*driverState
	order   []InstanceID
	events  *eventLog

	// viewMu follows driverState.mu; never acquire a driver lock while held.
	viewMu   sync.Mutex
	observed map[InstanceID]observation
	// pendingEv holds committed-in-memory events whose view publication
	// failed; they are emitted, in order, with the next published view.
	pendingEv []Event
	// pendingLost is set when a pending event was discarded; the next
	// publication marks a gap so readers resynchronise.
	pendingLost bool
	dirty       bool
	// releases end decision holds whose acceptance is not yet published;
	// they run once a publication succeeds.
	releases   []func()
	retryArmed bool
	view       atomic.Pointer[Catalog]

	// readbackArmed is set while the bounded readback resolver runs.
	readbackArmed atomic.Bool

	lifeMu  sync.Mutex
	started bool
	// admitting opens only after startup (catalog validation and recovery
	// restore) completed, so no decision commits ahead of recovery.
	admitting bool
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Map, reflect.Slice, reflect.Interface, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// NewService validates the configuration and builds a Service. Validation performs
// no store access and invokes no executor or recovery adapter.
func NewService(opts ServiceOptions) (*Service, error) {
	if isNil(opts.Store) {
		return nil, fmt.Errorf("%w: missing store", ErrInvalidConfig)
	}
	if isNil(opts.Recovery) {
		return nil, fmt.Errorf("%w: missing recovery adapter", ErrInvalidConfig)
	}
	if len(opts.Incarnation) == 0 || len(opts.Incarnation) > maxIncarnationBytes || !incarnationPattern.MatchString(opts.Incarnation) {
		return nil, fmt.Errorf("%w: invalid incarnation", ErrInvalidConfig)
	}
	if opts.OperationBudget < 0 || opts.OperationBudget > DefaultOperationBudget || opts.MaxAttempts < 0 || opts.MaxAttempts > DefaultMaxAttempts {
		return nil, fmt.Errorf("%w: invalid operation bounds", ErrInvalidConfig)
	}
	s := &Service{
		store:       opts.Store,
		recovery:    opts.Recovery,
		incarnation: opts.Incarnation,
		now:         opts.Now,
		budget:      opts.OperationBudget,
		maxAttempts: opts.MaxAttempts,
		retryDelay:  opts.RetryDelay,
		onError:     opts.OnError,
		drivers:     make(map[InstanceID]*driverState, len(opts.Drivers)),
		events:      newEventLog(opts.EventCapacity),
		observed:    make(map[InstanceID]observation, len(opts.Drivers)),
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.budget == 0 {
		s.budget = DefaultOperationBudget
	}
	if s.maxAttempts == 0 {
		s.maxAttempts = DefaultMaxAttempts
	}
	if s.retryDelay == nil {
		s.retryDelay = defaultRetryDelay
	}
	for _, cfg := range opts.Drivers {
		if !validInstanceID(cfg.InstanceID) {
			return nil, fmt.Errorf("%w: empty instance id", ErrInvalidConfig)
		}
		if _, dup := s.drivers[cfg.InstanceID]; dup {
			return nil, fmt.Errorf("%w: duplicate driver id %s", ErrInvalidConfig, cfg.InstanceID)
		}
		if isNil(cfg.Executor) {
			return nil, fmt.Errorf("%w: driver %s has no lifecycle executor", ErrInvalidConfig, cfg.InstanceID)
		}
		d := &driverState{id: cfg.InstanceID, exec: cfg.Executor, wake: make(chan struct{}, 1)}
		d.cond = sync.NewCond(&d.mu)
		s.drivers[cfg.InstanceID] = d
		s.order = append(s.order, cfg.InstanceID)
	}
	sort.Slice(s.order, func(i, j int) bool { return s.order[i] < s.order[j] })
	s.view.Store(&Catalog{})
	return s, nil
}

func defaultRetryDelay(attempt int) time.Duration {
	delay := 100 * time.Millisecond << uint(min(attempt, 6))
	return min(delay, 5*time.Second)
}

func (s *Service) report(err error) {
	if err != nil && s.onError != nil {
		s.onError(err)
	}
}

// Start restores recovery material before any admission, derives each driver's
// initial observation from durable state only, publishes the first view and
// starts one reconciler per eligible driver. It returns the store or restore
// failure; the view stays readable either way and failed drivers report
// FAILED.
func (s *Service) Start(ctx context.Context) error {
	s.lifeMu.Lock()
	if s.started || s.closed {
		s.lifeMu.Unlock()
		return ErrServiceState
	}
	s.started = true
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.lifeMu.Unlock()

	if err := s.start(ctx); err != nil {
		var fatal *startFatal
		if errors.As(err, &fatal) {
			s.lifeMu.Lock()
			s.started, s.admitting = false, false
			s.cancel()
			s.lifeMu.Unlock()
			return fatal.err
		}
		s.openAdmission()
		return err
	}
	s.openAdmission()
	return nil
}

// startFatal marks a Start failure after which no reconciler runs and the
// service accepts no decision; Start may be called again.
type startFatal struct{ err error }

func (e *startFatal) Error() string { return e.err.Error() }
func (e *startFatal) Unwrap() error { return e.err }

func (s *Service) start(ctx context.Context) error {
	catalog, err := s.store.Catalog()
	if err != nil || catalog.ReadOnly {
		s.publishWithheld(catalog, err == nil)
		if err == nil {
			err = &Refusal{Code: RefusalFailedPrecondition, Row: 3, Reason: "control state is read-only"}
		}
		return &startFatal{err}
	}
	records := make(map[InstanceID]ControlRecord, len(catalog.Drivers))
	for _, rec := range catalog.Drivers {
		records[rec.InstanceID] = rec
	}
	if len(records) != len(s.drivers) {
		return &startFatal{fmt.Errorf("%w: service drivers differ from the store's visible drivers", ErrInvalidConfig)}
	}
	for id := range s.drivers {
		if _, ok := records[id]; !ok {
			return &startFatal{fmt.Errorf("%w: driver %s is not visible in the store", ErrInvalidConfig, id)}
		}
	}

	// Recovery first: restore before any executor call or admission. An adapter
	// that implements EmptyRestorer also learns that no material exists, so
	// its managed writers open only after this step.
	// An adapter that writes its own envelope gets the same bounded readback
	// resolver as service-owned writes, before its first store write.
	if rb, ok := s.recovery.(ReadbackBinder); ok {
		rb.BindReadback(s.armReadback)
	}
	var restoreErrs []error
	material := s.store.Recovery()
	var rerr error
	switch empty, ok := s.recovery.(EmptyRestorer); {
	case material != nil:
		rerr = s.recovery.Restore(ctx, cloneMaterial(material))
	case ok:
		rerr = empty.RestoreEmpty(ctx)
	}
	if rerr != nil {
		var affected []InstanceID
		if material != nil {
			affected = material.DriverIDs
		}
		// A managed restoration names exactly the drivers it failed closed;
		// healthy unrelated drivers keep running.
		var scoped *RestoreError
		if errors.As(rerr, &scoped) && len(scoped.Drivers) > 0 {
			affected = scoped.Drivers
		}
		if len(affected) == 0 {
			affected = s.order
		}
		for _, id := range affected {
			if d, ok := s.drivers[id]; ok {
				d.mu.Lock()
				d.recoveryBlocked = true
				d.mu.Unlock()
			}
			restoreErrs = append(restoreErrs, &SemanticError{InstanceID: id, Op: "restore", Err: rerr})
		}
	}

	for _, id := range s.order {
		d := s.drivers[id]
		d.mu.Lock()
		blocked := d.recoveryBlocked
		d.mu.Unlock()
		s.viewMu.Lock()
		s.observed[id] = initialObservation(records[id], blocked)
		s.viewMu.Unlock()
	}
	if !s.commitView(s.ctx, nil, true) {
		s.report(fmt.Errorf("%w: initial view not committed", ErrServiceState))
	}

	for _, id := range s.order {
		rec := records[id]
		d := s.drivers[id]
		d.mu.Lock()
		blocked := d.recoveryBlocked
		d.mu.Unlock()
		if rec.Failed || !rec.Enabled || blocked {
			continue
		}
		s.wg.Add(1)
		go s.worker(d)
		s.wakeDriver(d)
	}
	return errors.Join(restoreErrs...)
}

// Close cancels every reconciler and waits for them. Durable intent and
// in-flight work are left exactly as committed.
func (s *Service) Close() {
	s.lifeMu.Lock()
	s.closed = true
	cancel := s.cancel
	s.lifeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, d := range s.drivers {
		d.mu.Lock()
		d.cond.Broadcast()
		d.mu.Unlock()
	}
	s.wg.Wait()
}

func (s *Service) openAdmission() {
	s.lifeMu.Lock()
	s.admitting = true
	s.lifeMu.Unlock()
}

func (s *Service) running() bool {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	return s.started && s.admitting && !s.closed
}

func initialObservation(rec ControlRecord, blocked bool) observation {
	switch {
	case rec.Failed:
		return observation{state: ObservedFailed, reason: ReasonInternalError}
	case !rec.Enabled:
		return observation{state: ObservedDisabled, reason: ReasonConfigDisabled}
	case blocked:
		return observation{state: ObservedFailed, reason: ReasonDependencyUnavailable}
	}
	if t := rec.Current; t != nil {
		if t.Kind == KindStop || t.Kind == KindCancel {
			return observation{state: ObservedStopping, reason: ReasonStopRequested}
		}
		return observation{state: ObservedStarting, reason: ReasonStartRequested}
	}
	if rec.DesiredState == DesiredRunning {
		return observation{state: ObservedStarting, reason: ReasonStartRequested}
	}
	return observation{state: ObservedStopped, reason: ReasonNone}
}

// publishWithheld exposes every served driver as FAILED when control state
// cannot be used; a read-only catalog keeps its last-known-good intent.
func (s *Service) publishWithheld(catalog ControlCatalog, readable bool) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	records := map[InstanceID]ControlRecord{}
	if readable {
		for _, rec := range catalog.Drivers {
			records[rec.InstanceID] = rec
		}
	}
	// A readable catalog keeps its committed revisions: the withheld view
	// carries the stored catalog revision and each stored desired revision
	// unchanged, so readers never observe a rollback to zero.
	view := &Catalog{EventCursor: s.events.cursor()}
	if readable {
		view.Revision = catalog.CatalogRevision
	}
	for _, id := range s.order {
		rec := records[id]
		if !readable {
			// The store can still expose configured eligibility and any
			// last-known intent when its catalog is unavailable.
			if stored, err := s.store.Driver(id); err == nil {
				rec = stored
			}
		}
		desired := rec.DesiredState
		if desired == "" {
			desired = DesiredStopped
		}
		s.observed[id] = observation{state: ObservedFailed, reason: ReasonInternalError}
		view.Drivers = append(view.Drivers, DriverStatus{
			InstanceID: id, Enabled: rec.Enabled, DesiredState: desired, DesiredRevision: rec.DesiredRevision,
			ObservedState: ObservedFailed, Reason: ReasonInternalError,
			Revision: view.Revision,
		})
	}
	s.view.Store(view)
}

// List returns the current coherent cached view. It never waits for a store
// write, a reconciler or a decision.
func (s *Service) List() Catalog {
	return copyCatalog(s.view.Load())
}

// Get returns one entry of the current cached view with its revision, or a
// NOT_FOUND refusal for a noncanonical or unserved ID.
func (s *Service) Get(id InstanceID) (DriverStatus, error) {
	if !validInstanceID(id) {
		return DriverStatus{}, &Refusal{Code: RefusalNotFound, Row: 2, Reason: "driver not registered"}
	}
	if _, ok := s.drivers[id]; !ok {
		return DriverStatus{}, &Refusal{Code: RefusalNotFound, Row: 2, Reason: "driver not registered"}
	}
	view := s.view.Load()
	for _, entry := range view.Drivers {
		if entry.InstanceID == id {
			return entry, nil
		}
	}
	return DriverStatus{InstanceID: id, Revision: view.Revision, DesiredState: DesiredStopped,
		ObservedState: ObservedFailed, Reason: ReasonInternalError}, nil
}

// Diagnostics returns a copy of the driver's bounded terminal-summary ring.
func (s *Service) Diagnostics(id InstanceID) []Diagnostic {
	d, ok := s.drivers[id]
	if !ok {
		return nil
	}
	d.diagMu.Lock()
	defer d.diagMu.Unlock()
	return append([]Diagnostic(nil), d.diags...)
}

func copyCatalog(view *Catalog) Catalog {
	if view == nil {
		return Catalog{}
	}
	out := *view
	out.Drivers = append([]DriverStatus(nil), view.Drivers...)
	return out
}

// Decide validates the method, request shape and registration (rows 1 and
// 2) without the store, then lets the store apply rows 3 to 10 under its
// namespace-wide serialization point. An accepted changing decision is
// durable before the call returns, and only then is the view published, the
// event emitted, an untyped or superseded attempt fenced and the reconciler
// woken. A same-desired no-op writes, publishes, emits and wakes nothing.
// Cancelling ctx after the store accepted cannot retract the decision: the
// reconciler runs the persisted work under the service's own lifetime.
func (s *Service) Decide(ctx context.Context, method Method, req Request) (DesiredView, error) {
	if !validMethod(method) || req.ExpectedRevision == 0 {
		return DesiredView{}, &Refusal{Code: RefusalInvalidArgument, Row: 1, Reason: "invalid method or expected revision"}
	}
	d, ok := s.drivers[req.InstanceID]
	if !validInstanceID(req.InstanceID) || !ok {
		return DesiredView{}, &Refusal{Code: RefusalNotFound, Row: 2, Reason: "driver id noncanonical or not registered"}
	}
	if !s.running() {
		catalog, err := s.store.Catalog()
		var refusal *Refusal
		if errors.As(err, &refusal) && refusal.Code == RefusalFailedPrecondition {
			return DesiredView{}, refusal
		}
		if err == nil && catalog.ReadOnly {
			return DesiredView{}, &Refusal{Code: RefusalFailedPrecondition, Row: 3, Reason: "control state is read-only"}
		}
		return DesiredView{}, &Refusal{Code: RefusalUnavailable, Row: 3, Reason: "control service not running"}
	}
	if !s.store.AdmissionOpen() {
		if _, hasDeadline := ctx.Deadline(); hasDeadline && s.store.ResolveReadback(ctx) == nil && s.store.AdmissionOpen() {
			s.readbackResolved()
		}
	}
	var planned, fence bool
	view, err := s.store.Decide(ctx, method, req, s.planner(d, &planned, &fence))
	if err != nil {
		if planned {
			// The error alone does not tell whether the decision persisted:
			// an ambiguous commit keeps it staged until a readback. The
			// decision stays held, so a START it would supersede is neither
			// canceled nor left to run ahead of its acceptance. With
			// admission open the committed record already answers (a
			// refusal, a pre-commit failure, or a readback that settled it
			// concurrently); otherwise the readback resolver settles it.
			d.mu.Lock()
			d.ambiguous = &ambiguousDecision{method: method, expected: req.ExpectedRevision, fence: fence}
			d.ambiguousHeld.Store(true)
			d.mu.Unlock()
			if s.store.AdmissionOpen() {
				s.resolveAmbiguous([]*driverState{d})
			}
		}
		// An ambiguous commit closes admission until a readback: the
		// service resolves it itself, without waiting for another request.
		s.armReadback()
		return DesiredView{}, err
	}
	changed := view.DesiredRevision != req.ExpectedRevision
	published := true
	if changed {
		ev := s.desiredEvent(d, view.DesiredState, view.DesiredRevision)
		published = s.commitView(s.ctx, &ev, false)
	}
	if planned {
		// Acceptance is published before the hold ends: only now may the
		// superseded attempt be fenced and the reconciler act on the slots.
		s.afterPublish(published, func() { s.releaseDecision(d, fence && changed) })
	}
	return view, nil
}

// ambiguousDecision is a planned decision whose commit outcome is unknown.
type ambiguousDecision struct {
	method    Method
	expected  DesiredRevision
	fence     bool
	resolving bool
}

// resolveAmbiguous settles the held decisions of the given drivers once the
// committed namespace is readable with admission open. A decision was
// adopted exactly when the driver's committed desired revision moved off the
// request's expected revision: the store assigns a new revision only to a
// committed decision, and the planner refuses further decisions for the
// driver while one is held. An adopted decision publishes its
// DESIRED_ACCEPTED with the refreshed view, as a direct commit does, and
// only then fences the superseded attempt and ends the hold. A rejected
// decision emits nothing and ends the hold without fencing, so a typed START
// it would have canceled resumes with its attempt, budget and deadline
// unchanged. It reports whether it published a view and whether another
// resolver owns an in-progress resolution. Either suppresses fallback
// publication so an adopted view cannot precede its acceptance event.
func (s *Service) resolveAmbiguous(ds []*driverState) (published, resolving bool) {
	type settled struct {
		d     *driverState
		fence bool
	}
	var done []settled
	var events []Event
	lost := false
	for _, d := range ds {
		d.mu.Lock()
		amb := d.ambiguous
		if amb == nil {
			d.mu.Unlock()
			continue
		}
		if amb.resolving {
			resolving = true
			d.mu.Unlock()
			continue
		}
		amb.resolving = true
		d.mu.Unlock()
		rec, err := s.store.Driver(d.id)
		d.mu.Lock()
		if d.ambiguous != amb {
			d.mu.Unlock()
			continue // a concurrent resolution took it
		}
		// Retain ownership until the adopted view and acceptance are published.
		d.mu.Unlock()
		if err != nil {
			// The outcome cannot be read: release without fencing (the
			// reconciler cancels from the committed slots) and tell event
			// readers to resynchronise instead of holding the driver.
			s.report(err)
			lost = true
			done = append(done, settled{d: d})
			continue
		}
		adopted := rec.DesiredRevision != amb.expected
		if adopted {
			events = append(events, s.desiredEvent(d, rec.DesiredState, rec.DesiredRevision))
		}
		done = append(done, settled{d: d, fence: amb.fence && adopted})
	}
	committed := true
	if len(events) > 0 || lost {
		if lost {
			s.viewMu.Lock()
			s.pendingLost = true
			s.viewMu.Unlock()
		}
		committed = s.commitEvents(s.ctx, events, false)
		published = true
	}
	for _, x := range done {
		x := x
		s.afterPublish(committed, func() { s.releaseDecision(x.d, x.fence) })
	}
	return published, resolving
}

// releaseDecision ends a decision's hold, after its acceptance (if any) was
// published. With fence set it first fences the superseded untyped or typed
// START attempt. It always wakes the reconciler, which skips its steps while
// a decision is held.
func (s *Service) releaseDecision(d *driverState, fence bool) {
	d.mu.Lock()
	if fence {
		for _, a := range []*attempt{d.untyped, d.inflight} {
			if a != nil {
				a.fenced = true
				a.cancel()
			}
		}
	}
	d.ambiguous = nil
	d.ambiguousHeld.Store(false)
	d.deciding--
	d.cond.Broadcast()
	d.mu.Unlock()
	s.wakeDriver(d)
}

func (s *Service) desiredEvent(d *driverState, state DesiredState, rev DesiredRevision) Event {
	d.mu.Lock()
	corr := d.corr
	d.mu.Unlock()
	s.viewMu.Lock()
	obs := s.observed[d.id]
	s.viewMu.Unlock()
	return Event{Kind: EventDesiredAccepted, InstanceID: d.id,
		DesiredState: state, DesiredRevision: rev,
		ObservedState: obs.state, Reason: obs.reason, SafetyQuarantined: obs.quarantined,
		Epoch: corr.Epoch, Generation: corr.Generation}
}

// planner places a changing request in the typed slots. It runs under the
// store's serialization point and takes only the driver's own mutex.
func (s *Service) planner(d *driverState, planned, fence *bool) Planner {
	return func(in PlanInput) (SlotPlan, *Refusal) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.pruning && in.Current != nil {
			// The prune of this terminal slot has not committed yet and would
			// clear a slot reserved now. Once it committed, the planner sees
			// no current slot and proceeds.
			return SlotPlan{}, &Refusal{Code: RefusalTransitionBusy, Row: 8, Reason: "terminal work slot is being pruned"}
		}
		if in.Method != MethodStop && (d.quarantined || d.recoveryBlocked) {
			return SlotPlan{}, &Refusal{Code: RefusalFailedPrecondition, Row: 7, Reason: "quarantine or unresolved recovery blocks admission"}
		}
		if d.deciding > 0 || d.ambiguous != nil {
			// One owner spans planning, store I/O and acceptance publication.
			// The outcome of this driver's previous decision is still being
			// settled; deciding on top of it would make adoption unreadable.
			return SlotPlan{}, &Refusal{Code: RefusalTransitionBusy, Row: 8, Reason: "previous decision outcome is being resolved"}
		}
		next := &Transition{Kind: TransitionKind(in.Method), Phase: PhasePending}
		var plan SlotPlan
		fenceIt := false
		switch {
		case in.Current == nil:
			if a := d.untyped; a != nil && in.Method != MethodStart && !a.fenced {
				// An untyped STARTING or BACKOFF attempt (process-start
				// reconciliation): synthesize its cancellation as the current
				// slot with its correlation, obligations and budget.
				plan = SlotPlan{
					Current: &Transition{
						Kind: KindCancel, Phase: PhasePending, Epoch: a.corr.Epoch, Generation: a.corr.Generation,
						NativeClosePending: true, SemanticCleanupPending: true,
						DeadlineUTC: a.deadline.UTC(), AttemptsUsed: min(a.attemptsUsed, s.startLimit()),
					},
					Reserved: next,
				}
				fenceIt = true
			} else {
				plan = SlotPlan{Current: next}
			}
		case in.Reserved != nil || (in.Current.Kind == KindRestart && !terminal(in.Current)):
			return SlotPlan{}, &Refusal{Code: RefusalTransitionBusy, Row: 8, Reason: "typed transition slots are occupied"}
		default:
			plan = SlotPlan{Current: cloneTransition(in.Current), Reserved: next}
			fenceIt = in.Current.Kind == KindStart && in.Method != MethodStart
		}
		if err := validatePlan(in, plan); err != nil {
			return SlotPlan{}, &Refusal{Code: RefusalTransitionBusy, Row: 8, Reason: err.Error()}
		}
		*planned = true
		*fence = fenceIt
		d.deciding++
		return plan, nil
	}
}

// validatePlan checks that an accepted changing request enqueues exactly one
// new transition of the requested kind, keeps every stored transition
// unchanged and never leaves a reserved slot without a current slot. A
// synthesized cancellation is the only other new transition allowed, and only
// in the current slot ahead of the requested one.
func validatePlan(in PlanInput, plan SlotPlan) error {
	want := TransitionKind(in.Method)
	if plan.Reserved != nil && plan.Current == nil {
		return fmt.Errorf("%w: reserved slot without current slot", ErrInvalidWork)
	}
	if in.Current != nil && (plan.Current == nil || *plan.Current != *in.Current) {
		return fmt.Errorf("%w: stored current transition not retained", ErrInvalidWork)
	}
	if in.Reserved != nil && (plan.Reserved == nil || *plan.Reserved != *in.Reserved) {
		return fmt.Errorf("%w: stored reserved transition not retained", ErrInvalidWork)
	}
	requested, others := 0, 0
	for i, t := range []*Transition{plan.Current, plan.Reserved} {
		if t == nil || t.Sequence != 0 {
			continue
		}
		switch {
		case t.Kind == want && t.Phase == PhasePending && t.DesiredRevision == 0:
			requested++
		case t.Kind == KindCancel && i == 0 && in.Current == nil && plan.Reserved != nil:
			others++
		default:
			return fmt.Errorf("%w: unexpected new transition %s", ErrInvalidWork, t.Kind)
		}
	}
	if requested != 1 {
		return fmt.Errorf("%w: plan must enqueue exactly one %s transition", ErrInvalidWork, want)
	}
	if others == 1 && (plan.Reserved == nil || plan.Reserved.Kind != want || plan.Reserved.Sequence != 0) {
		return fmt.Errorf("%w: cancellation must precede the requested transition", ErrInvalidWork)
	}
	return nil
}

// terminal reports a slot whose work and cleanup obligations are settled and
// which only awaits pruning.
func terminal(t *Transition) bool {
	return t.Phase == PhaseCleanup && !t.NativeClosePending && !t.SemanticCleanupPending
}

// armReadback starts the bounded readback resolver when the store reports
// closed admission after a failed write (ambiguous persistence). It is
// idempotent while a resolver runs and does nothing once the service is
// closed. Nothing waits for a further request: List and Get keep serving the
// committed view, and the resolver either reopens admission (then publishes
// and wakes every reconciler) or leaves control closed and reports it.
func (s *Service) armReadback() {
	if s.store.AdmissionOpen() {
		return
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	if !s.started || s.closed || s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	if !s.readbackArmed.CompareAndSwap(false, true) {
		return
	}
	s.wg.Add(1)
	go s.resolveReadback(s.ctx)
}

// resolveReadback runs at most MaxAttempts readbacks: the first at once, the
// rest after RetryDelay, each bounded by OperationBudget/MaxAttempts. The
// store reopens admission only when the readback agrees with the staged or
// the committed namespace and directory durability is re-established; a
// disagreement makes the store fail control closed. With the defaults the
// whole series ends within about 50 seconds.
func (s *Service) resolveReadback(ctx context.Context) {
	defer s.wg.Done()
	defer s.readbackArmed.Store(false)
	perTry := s.budget / time.Duration(s.maxAttempts)
	for i := 0; i < s.maxAttempts; i++ {
		if i > 0 && !sleepCtx(ctx, s.retryDelay(i)) {
			return
		}
		if !s.store.AdmissionOpen() {
			tryCtx, cancel := context.WithTimeout(ctx, perTry)
			err := s.store.ResolveReadback(tryCtx)
			cancel()
			if err != nil {
				s.report(err)
			} else if !s.store.AdmissionOpen() {
				// Nothing is staged any more, yet control stays closed: the
				// store failed it closed (or persistence is blocked).
				s.report(fmt.Errorf("%w: control failed closed by the store", ErrReadbackUnresolved))
				return
			}
		}
		if s.store.AdmissionOpen() {
			s.readbackResolved()
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
	s.report(fmt.Errorf("%w after %d readbacks; admission stays closed", ErrReadbackUnresolved, s.maxAttempts))
}

// readbackResolved exposes the committed state after a readback adopted or
// rejected an ambiguous commit, lets a self-writing recovery adapter settle
// its own ambiguous write against the committed envelope, settles every held
// decision (an adopted one publishes its acceptance with the view) and lets
// every reconciler re-read its slots.
func (s *Service) readbackResolved() {
	if rb, ok := s.recovery.(ReadbackBinder); ok {
		rb.ReadbackResolved()
	}
	ds := make([]*driverState, 0, len(s.order))
	for _, id := range s.order {
		ds = append(ds, s.drivers[id])
	}
	if published, resolving := s.resolveAmbiguous(ds); !published && !resolving {
		s.commitView(s.ctx, nil, false)
	}
	for _, d := range ds {
		s.wakeDriver(d)
	}
}

func (s *Service) wakeDriver(d *driverState) {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// stageObservation records the driver's new observation for the next view.
// It reports whether the observation changed.
func (s *Service) stageObservation(id InstanceID, obs observation) bool {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if s.observed[id] == obs {
		return false
	}
	s.observed[id] = obs
	return true
}

// observe stages an observed transition, durably advances the catalog
// revision and publishes the view; the event follows the commit. Observed
// transitions never touch desired state or its revision.
func (s *Service) observe(d *driverState, obs observation) {
	d.mu.Lock()
	// Quarantine is latched for this process. Later cleanup observations
	// must preserve it even when their reason changes to retry exhaustion.
	obs.quarantined = obs.quarantined || d.quarantined
	obs.epoch, obs.generation = d.corr.Epoch, d.corr.Generation
	d.mu.Unlock()
	if s.stageObservation(d.id, obs) {
		s.commitObserved(d, obs)
	}
}

func (s *Service) commitObserved(d *driverState, obs observation) {
	s.commitView(s.ctx, &Event{Kind: EventObservedChanged, InstanceID: d.id, ObservedState: obs.state,
		Reason: obs.reason, SafetyQuarantined: obs.quarantined, Epoch: obs.epoch, Generation: obs.generation}, true)
}

// commitView publishes a view whose revision is durable. With advance set it
// first advances the catalog revision; if a concurrent publisher already
// exposed a different view at the same revision it advances again, within a
// small bound. It returns whether the view (and event) was published. A
// failed publication is never dropped: the event is retained in order and a
// bounded-delay retry republishes the view, so a staged transition cannot
// stay absent from List when no later action occurs.
func (s *Service) commitView(ctx context.Context, event *Event, advance bool) bool {
	if event == nil {
		return s.commitEvents(ctx, nil, advance)
	}
	return s.commitEvents(ctx, []Event{*event}, advance)
}

// commitEvents is commitView for any number of events published with one
// view.
func (s *Service) commitEvents(ctx context.Context, events []Event, advance bool) bool {
	for i := 0; i < maxPublishAttempts; i++ {
		if advance || i > 0 {
			if ctx.Err() != nil {
				break
			}
			if _, err := s.store.AdvanceCatalog(ctx); err != nil {
				s.report(err)
				s.armReadback()
				break
			}
		}
		if s.publish(events) {
			s.runReleases()
			return true
		}
	}
	if len(events) == 0 {
		s.deferPublication(nil)
	}
	for i := range events {
		s.deferPublication(&events[i])
	}
	return false
}

// runReleases ends the decision holds that waited for a publication.
func (s *Service) runReleases() {
	s.viewMu.Lock()
	rel := s.releases
	s.releases = nil
	s.viewMu.Unlock()
	for _, fn := range rel {
		fn()
	}
}

// afterPublish runs fn now when the acceptance was published, otherwise once
// a later publication carries it: the hold must outlast a failed publication.
func (s *Service) afterPublish(published bool, fn func()) {
	if !published {
		s.viewMu.Lock()
		if s.dirty {
			s.releases = append(s.releases, fn)
			s.viewMu.Unlock()
			return
		}
		s.viewMu.Unlock()
	}
	fn()
}

// deferPublication retains an unpublished event and arms one retry timer.
func (s *Service) deferPublication(event *Event) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if event != nil {
		if event.Kind == EventObservedChanged {
			// A newer observation of the driver supersedes an unpublished
			// one: replaying both would stamp the older with a view that
			// already shows the newer.
			kept := s.pendingEv[:0]
			for _, p := range s.pendingEv {
				if p.Kind != EventObservedChanged || p.InstanceID != event.InstanceID {
					kept = append(kept, p)
				}
			}
			s.pendingEv = kept
		}
		if len(s.pendingEv) == maxPendingEvents {
			copy(s.pendingEv, s.pendingEv[1:])
			s.pendingEv = s.pendingEv[:maxPendingEvents-1]
			s.pendingLost = true
		}
		s.pendingEv = append(s.pendingEv, *event)
	}
	s.dirty = true
	ctx := s.ctx
	if s.retryArmed || ctx == nil || ctx.Err() != nil {
		return
	}
	s.retryArmed = true
	time.AfterFunc(s.retryDelay(1), func() {
		s.viewMu.Lock()
		s.retryArmed = false
		s.viewMu.Unlock()
		if ctx.Err() != nil {
			return
		}
		s.commitView(ctx, nil, true)
	})
}

// publish rebuilds the view from one coherent store read plus the staged
// observations. A view is exposed only at a strictly higher revision, or
// re-exposed unchanged at the same revision, so content at a revision never
// changes once exposed. The view and its events are published atomically
// with respect to event readers: the events are retained and the view (with
// its EventCursor) stored under the event log's mutex, and waiters are woken
// only afterwards. A reader woken by an event therefore finds a list at
// least as new as the event's catalog revision, and a resync at a published
// EventCursor always finds that cursor's events. Pending OBSERVED_CHANGED
// events that the view no longer shows are coalesced away, with a resync gap
// when no event carries the current observation.
func (s *Service) publish(events []Event) bool {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	catalog, err := s.store.Catalog()
	if err != nil || catalog.ReadOnly {
		return false
	}
	built := s.buildLocked(catalog)
	current := s.view.Load()
	candidates := append(append([]Event(nil), s.pendingEv...), events...)
	// A catalog read can include another resolver's adopted decision before
	// that resolver has read its record or supplied its acceptance event.
	// Keep that driver's published intent until the matching acceptance can
	// accompany it atomically. Other drivers may still publish independently.
	for i := range built.Drivers {
		entry := &built.Drivers[i]
		d := s.drivers[entry.InstanceID]
		if d == nil || !d.ambiguousHeld.Load() {
			continue
		}
		accepted := false
		for _, e := range candidates {
			if e.Kind == EventDesiredAccepted && e.InstanceID == entry.InstanceID && e.DesiredRevision == entry.DesiredRevision {
				accepted = true
				break
			}
		}
		if !accepted {
			for _, prior := range current.Drivers {
				if prior.InstanceID == entry.InstanceID {
					entry.DesiredState, entry.DesiredRevision = prior.DesiredState, prior.DesiredRevision
					break
				}
			}
		}
	}
	switch {
	case built.Revision > current.Revision:
	case built.Revision == current.Revision && reflect.DeepEqual(built.Drivers, current.Drivers):
	default:
		return false
	}
	entries := make(map[InstanceID]DriverStatus, len(built.Drivers))
	for _, entry := range built.Drivers {
		entries[entry.InstanceID] = entry
	}
	shownEvents := make(map[InstanceID]bool)
	for _, e := range candidates {
		if e.Kind == EventObservedChanged && observationShown(e, entries[e.InstanceID], s.observed[e.InstanceID]) {
			shownEvents[e.InstanceID] = true
		}
	}
	emit := candidates[:0]
	supersededGap := false
	for i, e := range candidates {
		e.CatalogRevision = built.Revision
		if e.Kind == EventDesiredAccepted && entries[e.InstanceID].DesiredRevision != e.DesiredRevision {
			// A later desired transition superseded this one before its view
			// published: the view no longer shows it, so it is not stamped
			// with that view and the gap tells readers to resync.
			supersededGap = true
			continue
		}
		if e.Kind == EventObservedChanged {
			entry := entries[e.InstanceID]
			if !observationShown(e, entry, s.observed[e.InstanceID]) {
				// Deferral order need not match staging order: an older
				// publisher can replace the newer pending event. Signal a
				// gap when discarding a deferred event unless this
				// publication carries the current observation's event.
				if i < len(s.pendingEv) && !shownEvents[e.InstanceID] {
					supersededGap = true
				}
				continue
			}
			e.DesiredState = entry.DesiredState
			e.DesiredRevision = entry.DesiredRevision
		}
		emit = append(emit, e)
	}
	gap := s.pendingLost || supersededGap
	s.pendingLost = false
	s.events.publish(emit, gap, func(cursor uint64) {
		built.EventCursor = cursor
		s.view.Store(&built)
	})
	s.pendingEv, s.dirty = nil, false
	return true
}

// observationShown reports whether a view entry shows exactly the observed
// transition an OBSERVED_CHANGED event reports, including its staged lifecycle
// correlation. The caller holds viewMu so the entry and observation are coherent.
func observationShown(e Event, entry DriverStatus, obs observation) bool {
	return entry.ObservedState == e.ObservedState && entry.Reason == e.Reason &&
		entry.SafetyQuarantined == e.SafetyQuarantined &&
		obs.epoch == e.Epoch && obs.generation == e.Generation
}

func (s *Service) buildLocked(catalog ControlCatalog) Catalog {
	records := make(map[InstanceID]ControlRecord, len(catalog.Drivers))
	for _, rec := range catalog.Drivers {
		records[rec.InstanceID] = rec
	}
	view := Catalog{Revision: catalog.CatalogRevision, Drivers: make([]DriverStatus, 0, len(s.order))}
	for _, id := range s.order {
		rec, ok := records[id]
		obs := s.observed[id]
		entry := DriverStatus{InstanceID: id, Revision: catalog.CatalogRevision, DesiredState: DesiredStopped,
			ObservedState: obs.state, Reason: obs.reason, SafetyQuarantined: obs.quarantined,
			Epoch: obs.epoch, Generation: obs.generation}
		if obs.state == ObservedBackoff {
			entry.NextAttempt, entry.NextAttemptUTC = obs.nextAttempt, obs.nextAttemptUTC
			entry.LastFailureClass, entry.LastFailureCause = obs.failureClass, obs.failureCause
		}
		if ok {
			entry.Enabled = rec.Enabled
			entry.DesiredState = rec.DesiredState
			entry.DesiredRevision = rec.DesiredRevision
			if rec.Failed {
				entry.ObservedState, entry.Reason = ObservedFailed, ReasonInternalError
				entry.NextAttempt, entry.NextAttemptUTC = 0, time.Time{}
				entry.LastFailureClass, entry.LastFailureCause = "", ""
			}
		}
		if entry.ObservedState == "" {
			entry.ObservedState, entry.Reason = ObservedFailed, ReasonInternalError
		}
		view.Drivers = append(view.Drivers, entry)
	}
	return view
}

func (s *Service) appendDiagnostic(d *driverState, t Transition, outcome string) {
	summary := fmt.Sprintf("kind=%s outcome=%s desired_revision=%d attempts_used=%d", t.Kind, outcome, t.DesiredRevision, t.AttemptsUsed)
	if len(summary) > MaxDiagnosticBytes {
		summary = summary[:MaxDiagnosticBytes]
	}
	d.diagMu.Lock()
	defer d.diagMu.Unlock()
	if len(d.diags) == MaxDiagnostics {
		copy(d.diags, d.diags[1:])
		d.diags = d.diags[:MaxDiagnostics-1]
	}
	d.diags = append(d.diags, Diagnostic{Kind: t.Kind, DesiredRevision: t.DesiredRevision, Outcome: outcome,
		AttemptsUsed: t.AttemptsUsed, Summary: summary})
}

func cloneTransition(t *Transition) *Transition {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

func cloneMaterial(m *RecoveryMaterial) *RecoveryMaterial {
	if m == nil {
		return nil
	}
	return &RecoveryMaterial{
		DriverIDs:          append([]InstanceID(nil), m.DriverIDs...),
		MaxDesiredRevision: m.MaxDesiredRevision,
		Payload:            append([]byte(nil), m.Payload...),
	}
}

func validMethod(method Method) bool {
	return method == MethodStart || method == MethodStop || method == MethodRestart
}
