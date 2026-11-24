// Lifecycle vocabulary of the SemReg runtime: identity and ordering types, the
// durable control-record vocabulary, the control store seam and the injectable
// lifecycle executor and recovery adapter seams.
package life

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Identity and ordering types. Each is a distinct defined type so that no
// counter or identifier can stand in for another without an explicit
// conversion. None of them is an alias, and none is shared with the semantic
// publication hub.

// InstanceID is the stable configured runtime identity of one instance: the
// canonical encoding of its driver kind and instance key (see Ref.ID).
type InstanceID string

// SourceEpoch identifies one source epoch.
type SourceEpoch string

// DriverGeneration counts lifecycle generations inside one source epoch.
type DriverGeneration uint64

// DesiredRevision is the revision of one persistent desired-state record.
type DesiredRevision uint64

// CatalogRevision is the revision of the coherent registry list view.
type CatalogRevision uint64

// LifecycleSequence is the monotonic per-driver lifecycle sequence.
type LifecycleSequence uint64

// LifecycleCorrelation identifies one lifecycle generation of one driver
// source. It carries identity only.
type LifecycleCorrelation struct {
	InstanceID InstanceID
	Epoch      SourceEpoch
	Generation DriverGeneration
	Sequence   LifecycleSequence
}

// Desired-state service types. They mirror the durable control store's
// decision and work vocabulary: the service reaches the store only through the
// Store interface.

// DesiredState is durable driver intent: RUNNING or STOPPED.
type DesiredState string

const (
	DesiredRunning DesiredState = "RUNNING"
	DesiredStopped DesiredState = "STOPPED"
)

// Method selects a lifecycle decision.
type Method string

const (
	MethodStart   Method = "START"
	MethodStop    Method = "STOP"
	MethodRestart Method = "RESTART"
)

// TransitionKind is the exact stored kind of one typed work slot. KindCancel
// is the cancellation synthesized for an untyped attempt.
type TransitionKind string

const (
	KindStart   TransitionKind = "START"
	KindStop    TransitionKind = "STOP"
	KindRestart TransitionKind = "RESTART"
	KindCancel  TransitionKind = "CANCEL_ATTEMPT"
)

// TransitionPhase is the persisted phase of one typed work slot. It only
// moves forward: PENDING, ACTIVE, CLEANUP.
type TransitionPhase string

const (
	PhasePending TransitionPhase = "PENDING"
	PhaseActive  TransitionPhase = "ACTIVE"
	PhaseCleanup TransitionPhase = "CLEANUP"
)

// Transition is one bounded typed in-flight work slot. Sequence is internal
// recovery bookkeeping, never a client operation handle or receipt.
type Transition struct {
	Kind                   TransitionKind
	DesiredRevision        DesiredRevision
	Sequence               LifecycleSequence
	Phase                  TransitionPhase
	Epoch                  SourceEpoch
	Generation             DriverGeneration
	NativeClosePending     bool
	SemanticCleanupPending bool
	AttemptsUsed           int
	DeadlineUTC            time.Time
}

// ControlRecord is the store's coherent read of one visible driver.
type ControlRecord struct {
	InstanceID      InstanceID
	Enabled         bool
	Failed          bool
	ReadOnly        bool
	HasRecord       bool
	DesiredState    DesiredState
	DesiredRevision DesiredRevision
	Current         *Transition
	Reserved        *Transition
}

// ControlCatalog is the store's coherent read of every visible driver,
// sorted bytewise by InstanceID.
type ControlCatalog struct {
	ReadOnly        bool
	IssuedMark      DesiredRevision
	CatalogRevision CatalogRevision
	Drivers         []ControlRecord
}

// PlanInput is what the slot planner sees under the store's namespace-wide
// serialization point.
type PlanInput struct {
	Method     Method
	InstanceID InstanceID
	HasRecord  bool
	Current    *Transition
	Reserved   *Transition
}

// SlotPlan is the complete replacement of a driver's two typed slots. A new
// transition carries Sequence 0 and DesiredRevision 0; the store assigns the
// accepted revision and the next lifecycle sequences. A retained transition
// equals the stored one exactly.
type SlotPlan struct {
	Current  *Transition
	Reserved *Transition
}

// Planner decides slot placement. It runs under the store's serialization
// point, must not block or perform I/O, and may refuse only with
// refusal-table rows 7 (FAILED_PRECONDITION) or 8 (TRANSITION_BUSY).
type Planner func(PlanInput) (SlotPlan, *Refusal)

// RecoveryMaterial is the recovery envelope content. The payload is opaque
// to the service and the store.
type RecoveryMaterial struct {
	DriverIDs          []InstanceID
	MaxDesiredRevision DesiredRevision
	Payload            []byte
}

// RefusalCode is one closed lifecycle refusal code. Lifecycle refusals never
// rename a semantic error; see SemanticError.
type RefusalCode string

const (
	RefusalInvalidArgument    RefusalCode = "INVALID_ARGUMENT"
	RefusalNotFound           RefusalCode = "NOT_FOUND"
	RefusalFailedPrecondition RefusalCode = "FAILED_PRECONDITION"
	RefusalUnavailable        RefusalCode = "UNAVAILABLE"
	RefusalDeadlineExceeded   RefusalCode = "DEADLINE_EXCEEDED"
	RefusalRevisionConflict   RefusalCode = "REVISION_CONFLICT"
	RefusalTransitionBusy     RefusalCode = "TRANSITION_BUSY"
	RefusalResourceExhausted  RefusalCode = "RESOURCE_EXHAUSTED"
	RefusalCounterExhausted   RefusalCode = "COUNTER_EXHAUSTED"
)

// Refusal is a refused lifecycle request. Row is the refusal-table order, or
// 0 for the pre-commit write failure outside the ordered checks. View is set
// only for REVISION_CONFLICT. A refusal leaves durable state unchanged.
type Refusal struct {
	Code   RefusalCode
	Row    int
	View   *DesiredView
	Reason string
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("life: %s (row %d): %s", r.Code, r.Row, r.Reason)
}

// Retryable reports the refusal table's retryable column.
func (r *Refusal) Retryable() bool {
	switch r.Code {
	case RefusalUnavailable, RefusalDeadlineExceeded, RefusalRevisionConflict,
		RefusalTransitionBusy, RefusalResourceExhausted:
		return true
	}
	return false
}

// DesiredView is the closed decision result: desired state and desired
// revision only. It carries no operation ID, sequence, disposition or
// completion claim.
type DesiredView struct {
	DesiredState    DesiredState
	DesiredRevision DesiredRevision
}

// MarshalJSON encodes exactly {desired_state, desired_revision}, with the
// revision as a canonical decimal string.
func (v DesiredView) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		DesiredState    DesiredState `json:"desired_state"`
		DesiredRevision string       `json:"desired_revision"`
	}{v.DesiredState, strconv.FormatUint(uint64(v.DesiredRevision), 10)})
}

// Request is the closed two-field lifecycle request.
type Request struct {
	InstanceID       InstanceID
	ExpectedRevision DesiredRevision
}

// Store is the durable control store seen by the service: the control records behind a protocol-neutral seam. Every write is
// durable before it returns; reads return committed state only and never wait
// for an in-flight write.
type Store interface {
	// Decide validates, decides and durably commits one request under the
	// namespace-wide serialization point, consulting the planner for slot
	// placement. It returns the current view for a same-desired no-op.
	Decide(ctx context.Context, method Method, req Request, planner Planner) (DesiredView, error)
	// CommitWork durably replaces a driver's typed slots without changing
	// desired state, its revision or the mark.
	CommitWork(ctx context.Context, id InstanceID, current, reserved *Transition) error
	// AdvanceCatalog durably advances the catalog view revision once.
	AdvanceCatalog(ctx context.Context) (CatalogRevision, error)
	// CommitRecovery durably stores the recovery envelope.
	CommitRecovery(ctx context.Context, material *RecoveryMaterial) error
	// Recovery returns a detached copy of the committed recovery envelope.
	Recovery() *RecoveryMaterial
	// Catalog returns one coherent read of committed control state.
	Catalog() (ControlCatalog, error)
	// Driver returns a coherent read of one visible driver.
	Driver(id InstanceID) (ControlRecord, error)
	// AdmissionOpen reports whether decisions can be admitted.
	AdmissionOpen() bool
	// ResolveReadback runs the readback after ambiguous persistence.
	ResolveReadback(ctx context.Context) error
}

// LifecycleExecutor is the injectable native lifecycle seam of one driver.
// The service never constructs it and never performs endpoint I/O itself.
type LifecycleExecutor interface {
	// Start opens one generation under the given correlation. A nil error
	// means the generation is admitted. A *RecoveringStart is not a failure:
	// the generation is admitted into its native owner's own recovery. A
	// failed Start leaves nothing open. An error whose Retryable method
	// reports true schedules a bounded retry.
	Start(ctx context.Context, correlation LifecycleCorrelation) error
	// Stop withdraws and closes the generation of the given correlation. It
	// must accept a correlation whose generation never opened.
	Stop(ctx context.Context, correlation LifecycleCorrelation) StopOutcome
}

// StopOutcome reports the two independent cleanup obligations of a stop.
type StopOutcome struct {
	NativeClosed    bool
	SemanticCleaned bool
}

// RecoveryAdapter is the injectable recovery seam. There is no default
// implementation: a service without one refuses to start.
type RecoveryAdapter interface {
	// Restore replays committed recovery material before any admission.
	Restore(ctx context.Context, material *RecoveryMaterial) error
	// Secure returns the recovery material that must be durable before the
	// given terminal transition is pruned, or nil when nothing new must be
	// retained.
	Secure(ctx context.Context, id InstanceID, terminal Transition) (*RecoveryMaterial, error)
}

func validInstanceID(id InstanceID) bool { return id != "" }
