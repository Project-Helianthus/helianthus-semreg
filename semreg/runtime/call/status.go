package call

import (
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// Caller-facing statuses. Failed keeps the kernel outcomes apart.
const (
	StatusAccepted          driver.OperationStatus = "accepted"
	StatusProtocolAck       driver.OperationStatus = "protocol_ack"
	StatusReadbackConfirmed driver.OperationStatus = "readback_confirmed"
	StatusFailed            driver.OperationStatus = "failed"
	StatusUnknown           driver.OperationStatus = "unknown"
	StatusUnavailable       driver.OperationStatus = "UNAVAILABLE"
)

// Reasons name why nothing was sent.
const (
	ReasonExecutorMissing       = "semantic.operation.reason.executor_missing"
	ReasonNoRoute               = "semantic.operation.reason.no_route"
	ReasonAmbiguousRoute        = "semantic.operation.reason.ambiguous_route"
	ReasonWithdrawnRoute        = "semantic.operation.reason.withdrawn_route"
	ReasonStaleOwner            = "semantic.operation.reason.stale_owner"
	ReasonUnknownOwner          = "semantic.operation.reason.unknown_owner"
	ReasonNoContact             = "semantic.operation.reason.no_contact"
	ReasonGuardChanged          = "semantic.operation.reason.guard_changed"
	ReasonRouteMismatch         = "semantic.operation.reason.route_mismatch"
	ReasonOutsideLimit          = "semantic.operation.reason.outside_limit"
	ReasonReleaseTrue           = "semantic.operation.reason.release_true"
	ReasonMixedRelease          = "semantic.operation.reason.mixed_release"
	ReasonNegativeSet           = "semantic.operation.reason.negative_set"
	ReasonNoReleaseCase         = "semantic.operation.reason.no_release_case"
	ReasonOwnerAltered          = "semantic.operation.reason.owner_altered"
	ReasonUnavailable           = "semantic.operation.reason.unavailable"
	ReasonCapabilityMissing     = "semantic.operation.reason.capability_instance_missing"
	ReasonServiceMissing        = "semantic.operation.reason.service_instance_missing"
	ReasonResourceMissing       = "semantic.operation.reason.service_resource_missing"
	ReasonInvalidArgument       = "semantic.operation.reason.invalid_argument"
	ReasonNoncanonicalArguments = "semantic.operation.reason.noncanonical_arguments"
	ReasonDuplicateArgument     = "semantic.operation.reason.duplicate_argument"
	ReasonNoncanonicalPre       = "semantic.operation.reason.noncanonical_preconditions"
	ReasonDuplicatePrecondition = "semantic.operation.reason.duplicate_precondition"
	ReasonMissingPrimary        = "semantic.operation.reason.missing_primary"
	ReasonUndeclaredParameter   = "semantic.operation.reason.undeclared_parameter"
	ReasonCallBoundInvalid      = "semantic.operation.reason.call_bound_invalid"
	ReasonReplay                = "semantic.operation.reason.replay"
)

// DispatchMode separates a live call from persisted, queued or replayed input.
type DispatchMode int

const (
	ModeDirect DispatchMode = iota
	ModePersisted
	ModeAsync
	ModeReplay
)

type routeRefusal struct {
	status   driver.OperationStatus
	outcome  operation.Outcome
	delivery operation.DeliveryState
	reason   string
}

func refused(status driver.OperationStatus, outcome operation.Outcome, delivery operation.DeliveryState, reason string) routeRefusal {
	return routeRefusal{status: status, outcome: outcome, delivery: delivery, reason: reason}
}

func (r routeRefusal) result() driver.OperationResult {
	return driver.OperationResult{Status: r.status, KernelOutcome: r.outcome, Delivery: r.delivery, Reason: r.reason}
}

func failed(reason string) *routeRefusal {
	refusal := refused(StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, reason)
	return &refusal
}

func noContact() *routeRefusal {
	refusal := refused(StatusFailed, operation.OutcomeFailedNoContact, operation.DeliveryNotSent, ReasonNoContact)
	return &refusal
}

func unavailable(reason string) *routeRefusal {
	refusal := refused(StatusUnavailable, operation.OutcomeRejected, operation.DeliveryNotSent, reason)
	return &refusal
}
