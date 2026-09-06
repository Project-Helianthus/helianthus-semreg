// Package operation implements the protocol-neutral v1 operation contract.
// It owns semantic admission and immutable execution evidence, never native
// handles, transport I/O, or gateway lifecycle.
package operation

import semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"

// ContractOperationV1 identifies Intent and ExecutionRecord wire records.
const ContractOperationV1 semreg.ContractVersion = "helianthus.semantic.operation/v1"

type CapabilityRequirement struct {
	Pack          semreg.PackRef               `json:"pack"`
	DefinitionID  semreg.DefinitionID          `json:"definition_id"`
	Versions      semreg.VersionRange          `json:"versions"`
	InstanceID    *semreg.CapabilityInstanceID `json:"instance_id,omitempty"`
	AllowDegraded bool                         `json:"allow_degraded"`
}

type Precondition struct {
	Fact              semreg.FactKey     `json:"fact"`
	CandidateID       semreg.CandidateID `json:"candidate_id"`
	CandidateRevision semreg.Uint64      `json:"candidate_revision"`
	Operator          semreg.PredicateOp `json:"operator"`
	Expected          semreg.Value       `json:"expected"`
}

type ExpectedEffect struct {
	Rule     semreg.DefinitionRef `json:"rule"`
	Fact     semreg.FactKey       `json:"fact"`
	Operator semreg.PredicateOp   `json:"operator"`
	Expected semreg.Value         `json:"expected"`
}

type Intent struct {
	Contract                           semreg.ContractVersion `json:"contract"`
	IntentID                           semreg.IntentID        `json:"intent_id"`
	Kind                               semreg.DefinitionRef   `json:"kind"`
	ExpectedEffect                     ExpectedEffect         `json:"expected_effect"`
	AssetID                            semreg.AssetID         `json:"asset_id"`
	Arguments                          []semreg.TypedField    `json:"arguments"`
	RequiredCapability                 CapabilityRequirement  `json:"required_capability"`
	Authority                          semreg.EvidenceRef     `json:"authority"`
	Causal                             semreg.CausalContext   `json:"causal"`
	ExpectedSemanticRevision           semreg.Uint64          `json:"expected_semantic_revision"`
	ExpectedCapabilityRevision         semreg.Uint64          `json:"expected_capability_revision"`
	ExpectedCapabilityInstanceRevision semreg.Uint64          `json:"expected_capability_instance_revision"`
	ExpectedSourceEpochID              semreg.SourceEpochID   `json:"expected_source_epoch_id"`
	ExpectedDriverGeneration           semreg.Uint64          `json:"expected_driver_generation"`
	Preconditions                      []Precondition         `json:"preconditions"`
	IdempotencyKey                     semreg.IdempotencyKey  `json:"idempotency_key"`
	Deadline                           semreg.TimePoint       `json:"deadline"`
}

type Route struct {
	CapabilityInstance semreg.CapabilityInstanceID `json:"capability_instance"`
	ServiceInstance    semreg.ServiceInstanceID    `json:"service_instance"`
	BindingID          semreg.NativeBindingID      `json:"binding_id"`
	SourceID           semreg.SourceID             `json:"source_id"`
	SourceEpochID      semreg.SourceEpochID        `json:"source_epoch_id"`
	DriverGeneration   semreg.Uint64               `json:"driver_generation"`
}

type DeliveryState string

const (
	DeliveryNotSent DeliveryState = "not_sent"
	DeliverySent    DeliveryState = "sent"
	DeliveryUnknown DeliveryState = "unknown"
)

type DispatchEvidence struct {
	AttemptID          semreg.AttemptID          `json:"attempt_id"`
	Started            semreg.EvaluationContext  `json:"started"`
	Completed          *semreg.EvaluationContext `json:"completed,omitempty"`
	Delivery           DeliveryState             `json:"delivery"`
	PossibleSideEffect bool                      `json:"possible_side_effect"`
	Evidence           []semreg.EvidenceRef      `json:"evidence"`
}

type AckState string

const (
	AckAccepted    AckState = "accepted"
	AckRejected    AckState = "rejected"
	AckProvisional AckState = "provisional"
)

type Acknowledgement struct {
	State    AckState             `json:"state"`
	At       semreg.TimePoint     `json:"at"`
	Evidence []semreg.EvidenceRef `json:"evidence"`
}

type ReadbackRelation string

const (
	ReadbackConfirms     ReadbackRelation = "confirms"
	ReadbackContradicts  ReadbackRelation = "contradicts"
	ReadbackInconclusive ReadbackRelation = "inconclusive"
)

type Readback struct {
	SnapshotID        semreg.SnapshotID        `json:"snapshot_id"`
	Revisions         semreg.RevisionVector    `json:"revisions"`
	CandidateID       semreg.CandidateID       `json:"candidate_id"`
	CandidateRevision semreg.Uint64            `json:"candidate_revision"`
	BindingID         semreg.NativeBindingID   `json:"binding_id"`
	SourceID          semreg.SourceID          `json:"source_id"`
	SourceEpochID     semreg.SourceEpochID     `json:"source_epoch_id"`
	DriverGeneration  semreg.Uint64            `json:"driver_generation"`
	Relation          ReadbackRelation         `json:"relation"`
	Evaluation        semreg.EvaluationContext `json:"evaluation"`
	Evidence          []semreg.EvidenceRef     `json:"evidence"`
}

type Outcome string

const (
	OutcomeRejected               Outcome = "rejected"
	OutcomeFailedNoContact        Outcome = "failed_no_contact"
	OutcomeAcknowledgedUnverified Outcome = "acknowledged_unverified"
	OutcomeApplied                Outcome = "applied"
	OutcomeNoEffect               Outcome = "no_effect"
	OutcomeConflict               Outcome = "conflict"
	OutcomeIndeterminate          Outcome = "indeterminate"
)

type ExecutionRecord struct {
	Contract         semreg.ContractVersion `json:"contract"`
	Intent           Intent                 `json:"intent"`
	AdmittedAt       *semreg.TimePoint      `json:"admitted_at,omitempty"`
	AdmittedRevision *semreg.RevisionVector `json:"admitted_revision,omitempty"`
	Route            *Route                 `json:"route,omitempty"`
	Dispatch         *DispatchEvidence      `json:"dispatch,omitempty"`
	Acknowledgement  *Acknowledgement       `json:"acknowledgement,omitempty"`
	Readback         *Readback              `json:"readback,omitempty"`
	Outcome          Outcome                `json:"outcome"`
	ErrorID          *semreg.ErrorID        `json:"error_id,omitempty"`
	OutcomeEvidence  []semreg.EvidenceRef   `json:"outcome_evidence"`
}

// OperationPackValidator is the exact pack-owned semantic boundary. The
// kernel invokes only the validator keyed by Intent.Kind.Pack.
type OperationPackValidator interface {
	semreg.PackValidator
	ValidateIntent(Intent) error
	EvaluateReadback(Intent, semreg.FactCandidate) (ReadbackRelation, error)
}
