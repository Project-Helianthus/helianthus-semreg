package driver

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// OperationRequest is a consumer's request for one operation. The driver supplies
// neither the effect, the route nor a causal expiry. Timeout is the finite
// request deadline of ordinary Invoke; dropping it cannot opt into InvokeCallBound.
type OperationRequest struct {
	AssetID                            v1.AssetID               `json:"asset_id"`
	BindingID                          v1.NativeBindingID       `json:"binding_id"`
	CapabilityInstanceID               v1.CapabilityInstanceID  `json:"capability_instance_id"`
	Operation                          v1.DefinitionRef         `json:"operation"`
	Arguments                          []v1.TypedField          `json:"arguments"`
	ExpectedSemanticRevision           v1.Uint64                `json:"expected_semantic_revision"`
	ExpectedCapabilityRevision         v1.Uint64                `json:"expected_capability_revision"`
	ExpectedCapabilityInstanceRevision v1.Uint64                `json:"expected_capability_instance_revision"`
	ExpectedDriverGeneration           v1.Uint64                `json:"expected_driver_generation"`
	ExpectedSourceEpochID              v1.SourceEpochID         `json:"expected_source_epoch_id"`
	Preconditions                      []operation.Precondition `json:"preconditions"`
	Timeout                            time.Duration            `json:"timeout,omitempty"`
}

// Validate checks every identity, the operation, each argument and precondition
// and the revision constraints.
func (r OperationRequest) Validate() error {
	if err := first(
		within("asset id", r.AssetID.Validate()), within("binding id", r.BindingID.Validate()),
		within("capability instance id", r.CapabilityInstanceID.Validate()), within("operation", r.Operation.Validate()),
		within("expected semantic revision", r.ExpectedSemanticRevision.Validate()),
		within("expected capability revision", r.ExpectedCapabilityRevision.Validate()),
		within("expected capability instance revision", r.ExpectedCapabilityInstanceRevision.Validate()),
		within("expected driver generation", r.ExpectedDriverGeneration.Validate()),
		within("expected source epoch id", r.ExpectedSourceEpochID.Validate()),
	); err != nil {
		return err
	}
	for i, a := range r.Arguments {
		if err := a.Validate(); err != nil {
			return fmt.Errorf("arguments[%d]: %w", i, err)
		}
	}
	for i, p := range r.Preconditions {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("preconditions[%d]: %w", i, err)
		}
	}
	return nil
}

// OperationStatus is the status of an operation result.
type OperationStatus string

// Validate requires a non-empty status.
func (s OperationStatus) Validate() error { return required("status", string(s)) }

// CompoundStep is one point of a compound execution, in order. A step that was
// not attempted carries no delivery, acknowledgement or readback relation. The
// reason is sanitized: no native coordinates, frame or unredacted transport error
// enter it.
type CompoundStep struct {
	Point            string                     `json:"point"`
	Attempted        bool                       `json:"attempted"`
	Delivery         operation.DeliveryState    `json:"delivery,omitempty"`
	Acknowledgement  operation.AckState         `json:"acknowledgement,omitempty"`
	ReadbackRelation operation.ReadbackRelation `json:"readback_relation,omitempty"`
	Reason           string                     `json:"reason,omitempty"`
}

// Validate checks the point name and the values that are present, and requires a
// step that was not attempted to carry no delivery, acknowledgement or readback.
func (s CompoundStep) Validate() error {
	if err := required("owner point name", s.Point); err != nil {
		return err
	}
	if !s.Attempted && (s.Delivery != "" || s.Acknowledgement != "" || s.ReadbackRelation != "") {
		return errors.New("a step that was not attempted carries no delivery, acknowledgement or readback")
	}
	return first(validDelivery(s.Delivery), validAck(s.Acknowledgement), validRelation(s.ReadbackRelation))
}

// CompoundExecution is the explicit result of a compound command: whether it is
// partial and its ordered steps.
type CompoundExecution struct {
	Partial bool           `json:"partial"`
	Steps   []CompoundStep `json:"steps"`
}

// Validate checks every step.
func (c CompoundExecution) Validate() error {
	for i, s := range c.Steps {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("steps[%d]: %w", i, err)
		}
	}
	return nil
}

// OperationResult is the one result shape of both invocations. An empty
// KernelOutcome, Delivery, Acknowledgement or ReadbackRelation is absent.
type OperationResult struct {
	Status            OperationStatus            `json:"status"`
	KernelOutcome     operation.Outcome          `json:"kernel_outcome,omitempty"`
	Delivery          operation.DeliveryState    `json:"delivery,omitempty"`
	Acknowledgement   operation.AckState         `json:"acknowledgement,omitempty"`
	ReadbackRelation  operation.ReadbackRelation `json:"readback_relation,omitempty"`
	Reason            string                     `json:"reason,omitempty"`
	CompoundExecution *CompoundExecution         `json:"compound_execution,omitempty"`
}

// Validate checks the status, the values that are present and the compound
// execution.
func (r OperationResult) Validate() error {
	if err := first(within("status", r.Status.Validate()), validOutcome(r.KernelOutcome), validDelivery(r.Delivery), validAck(r.Acknowledgement), validRelation(r.ReadbackRelation)); err != nil {
		return err
	}
	if r.CompoundExecution != nil {
		return within("compound execution", r.CompoundExecution.Validate())
	}
	return nil
}

func validOutcome(o operation.Outcome) error {
	switch o {
	case "", operation.OutcomeRejected, operation.OutcomeFailedNoContact, operation.OutcomeAcknowledgedUnverified,
		operation.OutcomeApplied, operation.OutcomeNoEffect, operation.OutcomeConflict, operation.OutcomeIndeterminate:
		return nil
	}
	return fmt.Errorf("outcome %q is not an operation outcome", o)
}

func validDelivery(d operation.DeliveryState) error {
	switch d {
	case "", operation.DeliveryNotSent, operation.DeliverySent, operation.DeliveryUnknown:
		return nil
	}
	return fmt.Errorf("delivery %q is not a delivery state", d)
}

func validAck(a operation.AckState) error {
	switch a {
	case "", operation.AckAccepted, operation.AckRejected, operation.AckProvisional:
		return nil
	}
	return fmt.Errorf("acknowledgement %q is not an acknowledgement state", a)
}

func validRelation(r operation.ReadbackRelation) error {
	switch r {
	case "", operation.ReadbackConfirms, operation.ReadbackContradicts, operation.ReadbackInconclusive:
		return nil
	}
	return fmt.Errorf("readback relation %q is not a readback relation", r)
}

// ScopedOperations invokes the registered setting-owner router with the session's
// actual caller identity and scope.
type ScopedOperations interface {
	// Invoke uses the existing finite request and caller deadline rules.
	Invoke(context.Context, OperationRequest) (OperationResult, error)
	// InvokeCallBound accepts only a live, cancellable, in-process invocation
	// without an absolute execution deadline. It cannot be serialized, prepared
	// for later dispatch or replayed.
	InvokeCallBound(context.Context, OperationRequest) (OperationResult, error)
}

// Dispatch is what the router hands the setting owner: the admitted request, the
// route, the guard claims, the expected effect and the admission time and
// revision, detached from router state. A call-bound invocation carries no
// finite-wire intent and no invented UTC or causal expiry.
type Dispatch struct {
	Request          OperationRequest
	Route            operation.Route
	Guard            operation.GuardClaims
	ExpectedEffect   operation.ExpectedEffect
	AdmittedAt       v1.TimePoint
	AdmittedRevision v1.RevisionVector
}

// Validate checks every member.
func (d Dispatch) Validate() error {
	g := d.Guard
	return first(
		within("request", d.Request.Validate()), within("route", d.Route.Validate()),
		within("guard", first(
			g.AssetID.Validate(), g.ExpectedCapabilityRevision.Validate(), g.CapabilityInstance.Validate(),
			g.ExpectedCapabilityInstanceRevision.Validate(), g.ServiceInstance.Validate(), g.BindingID.Validate(),
			g.SourceID.Validate(), g.SourceEpochID.Validate(), g.DriverGeneration.Validate(),
		)),
		within("expected effect", d.ExpectedEffect.Validate()), within("admitted at", d.AdmittedAt.Validate()),
		within("admitted revision", d.AdmittedRevision.Validate()),
	)
}

// Completion is the evidence the executor supplies. No terminal outcome means no
// terminal evidence; the router keeps the admitted intent, the exact later
// snapshot and the pack correlation.
type Completion struct {
	Dispatch          *operation.DispatchEvidence
	Acknowledgement   *operation.Acknowledgement
	Readback          *operation.Readback
	ReadbackSnapshot  *v1.Snapshot
	Outcome           operation.Outcome
	ErrorID           *v1.ErrorID
	OutcomeEvidence   []v1.EvidenceRef
	CompoundExecution *CompoundExecution
}

// Validate checks the evidence that is present. Without an Outcome there is no
// dispatch, acknowledgement, readback, error or outcome evidence.
func (c Completion) Validate() error {
	if err := validOutcome(c.Outcome); err != nil {
		return err
	}
	if c.Outcome == "" && (c.Dispatch != nil || c.Acknowledgement != nil || c.Readback != nil || c.ReadbackSnapshot != nil || c.ErrorID != nil || len(c.OutcomeEvidence) > 0) {
		return errors.New("no terminal outcome means no terminal evidence")
	}
	if c.Dispatch != nil {
		if err := c.Dispatch.Validate(); err != nil {
			return within("dispatch", err)
		}
	}
	if c.Acknowledgement != nil {
		if err := c.Acknowledgement.Validate(); err != nil {
			return within("acknowledgement", err)
		}
	}
	if c.Readback != nil {
		if err := c.Readback.Validate(); err != nil {
			return within("readback", err)
		}
	}
	if c.ReadbackSnapshot != nil {
		if err := c.ReadbackSnapshot.Validate(); err != nil {
			return within("readback snapshot", err)
		}
	}
	if c.ErrorID != nil {
		if err := c.ErrorID.Validate(); err != nil {
			return within("error id", err)
		}
	}
	for i, e := range c.OutcomeEvidence {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("outcome evidence[%d]: %w", i, err)
		}
	}
	if c.CompoundExecution != nil {
		return within("compound execution", c.CompoundExecution.Validate())
	}
	return nil
}

// Executor is the setting owner's native callback. The exact caller context
// flows to it. It rechecks Dispatch.Guard and the active cancellation context
// immediately before the native send.
type Executor interface {
	Execute(context.Context, Dispatch) (Completion, error)
}
