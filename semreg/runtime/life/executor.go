package life

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	errExecutorForeign      = errors.New("life: the correlation names another instance")
	errExecutorStale        = errors.New("life: the generation is stale or fenced")
	errExecutorPredecessor  = errors.New("life: the predecessor epoch is not fenced and closed")
	errExecutorStartFailed  = errors.New("life: native start failed")
	errExecutorStopRequired = errors.New("life: the instance was stopped")
)

// executorError is a closed, non-retryable executor failure. The manager stays
// the only native retry owner, so the executor never asks the service to retry.
type executorError struct {
	reason ReasonCode
	err    error
}

func (e *executorError) Error() string          { return e.err.Error() }
func (e *executorError) Unwrap() error          { return e.err }
func (e *executorError) ReasonCode() ReasonCode { return e.reason }

func executorFailure(reason ReasonCode, err error) error {
	return &executorError{reason: reason, err: err}
}

// epochState is one source epoch of the executor in this process.
type epochState struct {
	corr    LifecycleCorrelation
	fenced  bool
	fenceCh chan struct{}

	// recovering reports that the opening START was admitted into the manager's
	// persistent recovery.
	recovering bool
	admitted   bool

	// bound reports that a start of this process bound the epoch; withdrawn
	// that the semantic withdrawal of the latest bound correlation succeeded.
	bound     bool
	withdrawn bool

	stopMu     sync.Mutex
	closed     bool
	closeClass CloseClass
}

// instanceExecutor is the LifecycleExecutor of one instance over the manager:
// it is the ordinary lifecycle assembly's native lifecycle seam. It performs the
// native start and stop of the generations this process owns, and settles the
// native and semantic cleanup of a correlation an earlier process left.
type instanceExecutor struct {
	id          InstanceID
	manager     *Manager
	runtime     *instanceRuntime
	pub         Publication
	incarnation string
	exclusive   func() bool

	mu      sync.Mutex
	epochs  map[SourceEpoch]*epochState
	current *epochState
	// observe forwards the manager's later transitions of an admitted generation
	// to the service.
	observe func(LifecycleCorrelation, NativeObservation) bool
	// unproven records a native close the executor could not prove.
	unproven bool
}

func newInstanceExecutor(id InstanceID, manager *Manager, rt *instanceRuntime, pub Publication, incarnation string, exclusive func() bool) *instanceExecutor {
	return &instanceExecutor{id: id, manager: manager, runtime: rt, pub: pub, incarnation: incarnation, exclusive: exclusive,
		epochs: map[SourceEpoch]*epochState{}}
}

var _ LifecycleExecutor = (*instanceExecutor)(nil)

func (e *instanceExecutor) fenceLocked(st *epochState) {
	if !st.fenced {
		st.fenced = true
		close(st.fenceCh)
	}
}

// bind resolves the epoch of corr: a later generation of the epoch already
// bound, or a new epoch after the predecessor was fenced and its native close
// proven.
func (e *instanceExecutor) bind(corr LifecycleCorrelation) (*epochState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if st := e.epochs[corr.Epoch]; st != nil {
		if st.fenced {
			return nil, executorFailure(ReasonInternalError, errExecutorStale)
		}
		st.corr = corr
		st.withdrawn = false
		e.current = st
		return st, nil
	}
	if pred := e.current; pred != nil {
		pred.stopMu.Lock()
		closed := pred.closed
		pred.stopMu.Unlock()
		if !pred.fenced || !closed {
			return nil, executorFailure(ReasonInternalError, errExecutorPredecessor)
		}
	}
	st := &epochState{corr: corr, fenceCh: make(chan struct{}), bound: true}
	e.epochs[corr.Epoch] = st
	e.current = st
	return st, nil
}

// Start opens the generation of corr. It returns nil only when the manager
// published RUNNING for the native generation this attempt started, after the
// instance's staged publications committed. A failed Start leaves nothing open.
func (e *instanceExecutor) Start(ctx context.Context, corr LifecycleCorrelation) error {
	if corr.InstanceID != e.id || corr.Epoch == "" {
		return executorFailure(ReasonConfigInvalid, errExecutorForeign)
	}
	if corr.Generation == 0 {
		return executorFailure(ReasonConfigInvalid, errExecutorStale)
	}
	st, err := e.bind(corr)
	if err != nil {
		return err
	}
	e.runtime.bind(corr)
	before, _ := e.manager.Snapshot(string(e.id))
	for {
		err := e.manager.StartAsync(context.Background(), string(e.id))
		if !errors.Is(err, ErrUnavailable) {
			if err != nil {
				reason := ReasonProviderUnavailable
				if errors.Is(err, ErrSafetyQuarantined) {
					reason = ReasonInternalError
				}
				return executorFailure(reason, fmt.Errorf("%w: %w", errExecutorStartFailed, err))
			}
			break
		}
		select {
		case <-time.After(2 * time.Millisecond):
		case <-st.fenceCh:
			return executorFailure(ReasonInternalError, errExecutorStale)
		case <-ctx.Done():
			return executorFailure(ReasonInternalError, ctx.Err())
		}
	}
	for {
		snapshot, changed, _ := e.manager.Watch(string(e.id))
		e.mu.Lock()
		fenced, current := st.fenced, e.current == st
		e.mu.Unlock()
		if fenced || !current {
			return executorFailure(ReasonInternalError, errExecutorStale)
		}
		if snapshot.Revision > before.Revision {
			switch snapshot.ObservedState {
			case ObservedRunning, ObservedDegraded:
				e.mu.Lock()
				st.admitted = true
				e.mu.Unlock()
				e.forward(st)
				return nil
			case ObservedBackoff:
				if obs, ok := recoveryObservation(snapshot); ok {
					e.mu.Lock()
					st.recovering, st.admitted = true, true
					e.mu.Unlock()
					e.forward(st)
					return &RecoveringStart{Observation: obs}
				}
			case ObservedFailed:
				if snapshot.ActiveOperation == 0 {
					reason := snapshot.Reason.Code
					if reason == ReasonRetryExhausted || reason == ReasonCloseUnconfirmed {
						if reason == ReasonCloseUnconfirmed {
							return executorFailure(ReasonCloseUnconfirmed, fmt.Errorf("%w: %s", errExecutorStartFailed, reason))
						}
						reason = ReasonInternalError
					}
					return executorFailure(reason, fmt.Errorf("%w: %s", errExecutorStartFailed, snapshot.Reason.Code))
				}
			case ObservedStopped, ObservedDisabled:
				if snapshot.DesiredState == DesiredStopped {
					return executorFailure(ReasonInternalError, errExecutorStopRequired)
				}
			}
		}
		select {
		case <-changed:
		case <-st.fenceCh:
		case <-ctx.Done():
			return executorFailure(ReasonInternalError, ctx.Err())
		}
	}
}

// recoveryObservation is the BACKOFF observation of a manager retry scheduled in
// a persistent series; only such a retry is admitted into the native owner's
// recovery.
func recoveryObservation(snapshot Snapshot) (NativeObservation, bool) {
	retry := snapshot.Retry
	if snapshot.ObservedState != ObservedBackoff || snapshot.Reason.Code != ReasonRetryScheduled ||
		snapshot.DesiredState != DesiredRunning || snapshot.SafetyQuarantined ||
		retry == nil || !retry.Persistent || retry.Attempt < 1 || retry.NotBeforeUTC.IsZero() {
		return NativeObservation{}, false
	}
	return NativeObservation{State: ObservedBackoff, Reason: ReasonRetryScheduled,
		NextAttempt: retry.Attempt, NextAttemptUTC: retry.NotBeforeUTC.UTC(),
		FailureClass: retry.LastFailure.Class, FailureCause: retry.LastFailure.Cause}, true
}

// nativeObservation maps one manager snapshot of an admitted generation to the
// observation the service publishes; a snapshot that belongs to the service's
// own stop path maps to none.
func nativeObservation(snapshot Snapshot) (NativeObservation, bool) {
	switch snapshot.ObservedState {
	case ObservedRunning:
		return NativeObservation{State: ObservedRunning, Reason: ReasonNone}, true
	case ObservedDegraded:
		return NativeObservation{State: ObservedDegraded, Reason: snapshot.Reason.Code}, true
	case ObservedStarting:
		return NativeObservation{State: ObservedStarting, Reason: ReasonStartRequested, Quarantined: false}, true
	case ObservedBackoff:
		if obs, ok := recoveryObservation(snapshot); ok {
			return obs, true
		}
		obs := NativeObservation{State: ObservedBackoff, Reason: ReasonRetryScheduled}
		if retry := snapshot.Retry; retry != nil && retry.Attempt >= 1 && !retry.NotBeforeUTC.IsZero() {
			obs.NextAttempt, obs.NextAttemptUTC = retry.Attempt, retry.NotBeforeUTC.UTC()
			if retry.LastFailure.Class != "" {
				obs.FailureClass, obs.FailureCause = retry.LastFailure.Class, retry.LastFailure.Cause
			}
		}
		return obs, true
	case ObservedFailed:
		return NativeObservation{State: ObservedFailed, Reason: snapshot.Reason.Code, Quarantined: snapshot.SafetyQuarantined}, true
	}
	return NativeObservation{}, false
}

// forward relays the manager's transitions of the admitted generation to the
// service until the epoch is fenced.
func (e *instanceExecutor) forward(st *epochState) {
	e.mu.Lock()
	observe := e.observe
	corr := st.corr
	e.mu.Unlock()
	if observe == nil {
		return
	}
	go func() {
		var last uint64
		for {
			snapshot, changed, ok := e.manager.Watch(string(e.id))
			if !ok {
				return
			}
			e.mu.Lock()
			fenced := st.fenced
			e.mu.Unlock()
			if fenced {
				return
			}
			if snapshot.Revision != last {
				last = snapshot.Revision
				if obs, ok := nativeObservation(snapshot); ok {
					observe(corr, obs)
				}
			}
			select {
			case <-changed:
			case <-st.fenceCh:
				return
			}
		}
	}()
}

// ownsEpoch reports whether this process's incarnation allocated the epoch.
func (e *instanceExecutor) ownsEpoch(epoch SourceEpoch) bool {
	incarnation, ok := epochIncarnation(epoch)
	return ok && incarnation == e.incarnation
}

// Stop fences the correlation's epoch, then attempts the two independent
// obligations: the native close with its classified result, and the semantic
// withdrawal of the correlation's generation. Neither proves a device setting
// was undone, and neither waits for the other's success.
//
//   - A correlation of an epoch this process owns stops only that attempt and
//     keeps its proven, pending or quarantined native close outcome.
//   - A correlation this incarnation allocated but never bound opened nothing.
//   - A correlation of an earlier process, with no live attempt, proves the
//     native close only while this process holds the control store exclusively,
//     for the native handles that earlier process owned; otherwise the close is
//     unproven.
func (e *instanceExecutor) Stop(ctx context.Context, corr LifecycleCorrelation) StopOutcome {
	if corr.InstanceID != e.id || corr.Epoch == "" {
		return StopOutcome{}
	}
	e.mu.Lock()
	st := e.epochs[corr.Epoch]
	if st == nil && e.ownsEpoch(corr.Epoch) {
		// The correlation never bound an epoch: it opened nothing natively and
		// published nothing. A late Start for it is refused.
		tomb := &epochState{corr: corr, fenceCh: make(chan struct{}), closed: true, closeClass: CloseNotRequired}
		e.fenceLocked(tomb)
		e.epochs[corr.Epoch] = tomb
		e.mu.Unlock()
		return StopOutcome{NativeClosed: true, SemanticCleaned: true}
	}
	if st == nil {
		e.mu.Unlock()
		out := StopOutcome{}
		if e.exclusive != nil && e.exclusive() {
			out.NativeClosed = true
		}
		out.SemanticCleaned = e.pub.Withdraw(ctx, corr) == nil
		return out
	}
	e.fenceLocked(st)
	isCurrent := e.current == st
	e.mu.Unlock()

	native := e.ensureNativeStop(ctx, st, isCurrent)
	semantic := e.pub.Withdraw(ctx, corr) == nil
	if semantic {
		e.mu.Lock()
		if st.corr == corr {
			st.withdrawn = true
		}
		e.mu.Unlock()
	}
	return StopOutcome{NativeClosed: native, SemanticCleaned: semantic}
}

// withdrawRemaining withdraws the publications of every generation this
// process started and no stop has withdrawn, so shutdown leaves nothing
// registered after its native owner stopped. A failed withdrawal is reported.
func (e *instanceExecutor) withdrawRemaining(ctx context.Context) error {
	e.mu.Lock()
	var todo []*epochState
	for _, st := range e.epochs {
		if st.bound && !st.withdrawn {
			todo = append(todo, st)
		}
	}
	e.mu.Unlock()
	var errs []error
	for _, st := range todo {
		e.mu.Lock()
		corr := st.corr
		e.mu.Unlock()
		if err := e.pub.Withdraw(ctx, corr); err != nil {
			errs = append(errs, fmt.Errorf("life: withdrawing %s at shutdown: %w", e.id, err))
			continue
		}
		e.mu.Lock()
		if st.corr == corr {
			st.withdrawn = true
		}
		e.mu.Unlock()
	}
	return errors.Join(errs...)
}

// ensureNativeStop closes the epoch's native generation through the manager. A
// proven close is never repeated; an unproven one is reported again.
func (e *instanceExecutor) ensureNativeStop(ctx context.Context, st *epochState, isCurrent bool) bool {
	st.stopMu.Lock()
	defer st.stopMu.Unlock()
	if st.closed {
		return true
	}
	if !isCurrent {
		return false
	}
	report, err := e.manager.StopWithReport(ctx, string(e.id))
	if err != nil {
		return false
	}
	st.closeClass = report.Class
	st.closed = report.Class.Proven()
	if !st.closed {
		e.mu.Lock()
		e.unproven = true
		e.mu.Unlock()
	}
	return st.closed
}

// observeClose records the classified close of every manager withdrawal, the
// shutdown stop included. It takes no epoch lock: the manager calls it while a
// stop of the epoch may be in flight.
func (e *instanceExecutor) observeClose(report CloseReport) {
	e.mu.Lock()
	e.unproven = !report.Class.Proven()
	e.mu.Unlock()
}

// nativeSafe reports that every native close this executor attempted was proven.
func (e *instanceExecutor) nativeSafe() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.unproven
}
