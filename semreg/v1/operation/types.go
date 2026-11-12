// Package operation implements the protocol-neutral v1 operation contract.
// It owns semantic admission and immutable execution evidence, never native
// handles, transport I/O, or gateway lifecycle.
package operation

import semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"

// ContractOperationV1 identifies Intent and ExecutionRecord wire records.
const ContractOperationV1 semreg.ContractVersion = "helianthus.semantic.operation/v1"

// CapabilityRequirement names the capability an intent needs. It matches only
// definitions owned by the exact Pack; neither DefinitionID nor Versions can
// select another pack. A non-nil InstanceID pins one capability instance, and
// AllowDegraded admits a route whose capability or service is degraded.
type CapabilityRequirement struct {
	Pack          semreg.PackRef               `json:"pack"`
	DefinitionID  semreg.DefinitionID          `json:"definition_id"`
	Versions      semreg.VersionRange          `json:"versions"`
	InstanceID    *semreg.CapabilityInstanceID `json:"instance_id,omitempty"`
	AllowDegraded bool                         `json:"allow_degraded"`
}

// Precondition is a fact predicate checked at admission. Fact, CandidateID and
// CandidateRevision select exactly one candidate in the admitted snapshot; that
// candidate must be qualified, promoted, good, fresh, available and outside any
// open conflict, otherwise admission fails with precondition_failed. Another
// candidate of the same fact is never searched as a fallback.
type Precondition struct {
	Fact              semreg.FactKey     `json:"fact"`
	CandidateID       semreg.CandidateID `json:"candidate_id"`
	CandidateRevision semreg.Uint64      `json:"candidate_revision"`
	Operator          semreg.PredicateOp `json:"operator"`
	Expected          semreg.Value       `json:"expected"`
}

// ExpectedEffect is the typed fact predicate the operation is meant to make
// true. The operation pack derives it from the operation and arguments; the
// caller's copy must match byte for byte, and Rule.Pack must equal the intent's
// Kind.Pack. The fact may belong to another pack without transferring its
// meaning.
type ExpectedEffect struct {
	Rule     semreg.DefinitionRef `json:"rule"`
	Fact     semreg.FactKey       `json:"fact"`
	Operator semreg.PredicateOp   `json:"operator"`
	Expected semreg.Value         `json:"expected"`
}

// Intent is one requested semantic operation, a ContractOperationV1 wire
// record. Authority is an opaque reference the runtime resolves; its presence
// grants nothing. The expected revisions and driver generation are positive;
// they and the expected source epoch must still hold at admission and again
// before guarded dispatch.
// Arguments are sorted by field ID, preconditions by canonical fact key,
// candidate ID and candidate revision, each with at most 64 unique entries.
// Deadline uses clock.utc. IdempotencyKey deduplicates identical intent and
// outcome bytes only; it does not make a native operation replay-safe.
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

// Route is the single native path resolved at admission from current source,
// binding, service and capability records of one snapshot. Presentation
// selections, compatibility aliases, projections and caller-supplied native IDs
// never select a route.
type Route struct {
	CapabilityInstance semreg.CapabilityInstanceID `json:"capability_instance"`
	ServiceInstance    semreg.ServiceInstanceID    `json:"service_instance"`
	BindingID          semreg.NativeBindingID      `json:"binding_id"`
	SourceID           semreg.SourceID             `json:"source_id"`
	SourceEpochID      semreg.SourceEpochID        `json:"source_epoch_id"`
	DriverGeneration   semreg.Uint64               `json:"driver_generation"`
}

// DeliveryState records whether a dispatch attempt reached the native side.
type DeliveryState string

// DeliveryState values.
const (
	// DeliveryNotSent means evidence proves nothing was sent; it forbids a
	// possible side effect.
	DeliveryNotSent DeliveryState = "not_sent"
	// DeliverySent means the request was sent.
	DeliverySent DeliveryState = "sent"
	// DeliveryUnknown means delivery cannot be established.
	DeliveryUnknown DeliveryState = "unknown"
)

// DispatchEvidence is native-owner evidence for one dispatch attempt. Started
// and the optional Completed carry explicit wall and monotonic contexts, and
// completion cannot precede start. Completion is not a protocol
// acknowledgement. Evidence holds 1 to 32 references in canonical order.
type DispatchEvidence struct {
	AttemptID          semreg.AttemptID          `json:"attempt_id"`
	Started            semreg.EvaluationContext  `json:"started"`
	Completed          *semreg.EvaluationContext `json:"completed,omitempty"`
	Delivery           DeliveryState             `json:"delivery"`
	PossibleSideEffect bool                      `json:"possible_side_effect"`
	Evidence           []semreg.EvidenceRef      `json:"evidence"`
}

// AckState is the state a native protocol acknowledgement reported.
type AckState string

// AckState values.
const (
	// AckAccepted means the native side accepted the request.
	AckAccepted AckState = "accepted"
	// AckRejected means the native side rejected the request.
	AckRejected AckState = "rejected"
	// AckProvisional means the native side acknowledged the request
	// provisionally.
	AckProvisional AckState = "provisional"
)

// Acknowledgement is native protocol evidence that a request was answered. It
// is never confirming readback and cannot by itself prove OutcomeApplied.
type Acknowledgement struct {
	State    AckState             `json:"state"`
	At       semreg.TimePoint     `json:"at"`
	Evidence []semreg.EvidenceRef `json:"evidence"`
}

// ReadbackRelation is how an observed candidate relates to the intent's
// expected effect, as computed by the operation pack's EvaluateReadback; a
// caller-supplied relation is never evidence by itself.
type ReadbackRelation string

// ReadbackRelation values.
const (
	// ReadbackConfirms means the observation satisfies the expected effect.
	ReadbackConfirms ReadbackRelation = "confirms"
	// ReadbackContradicts means the observation contradicts the expected effect.
	ReadbackContradicts ReadbackRelation = "contradicts"
	// ReadbackInconclusive means the observation neither confirms nor
	// contradicts the expected effect.
	ReadbackInconclusive ReadbackRelation = "inconclusive"
)

// Readback names one observed candidate in a later snapshot whose semantic
// revision exceeds the admitted one. The candidate's binding, source, source
// epoch and driver generation must equal these fields and the admitted Route;
// Evaluation is the context used to evaluate that snapshot.
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

// Outcome is the terminal result of an ExecutionRecord. Each value requires a
// specific combination of evidence, checked by ExecutionRecord.Validate.
type Outcome string

// Outcome values.
const (
	// OutcomeRejected means admission failed: there is no route or dispatch,
	// and the record carries the stable admission error ID.
	OutcomeRejected Outcome = "rejected"
	// OutcomeFailedNoContact means dispatch evidence proves not_sent with no
	// possible side effect. A timeout is not this outcome, and a retry still
	// needs the owner's replay policy.
	OutcomeFailedNoContact Outcome = "failed_no_contact"
	// OutcomeAcknowledgedUnverified means the request was sent and accepted or
	// provisionally acknowledged, but no confirming readback exists.
	OutcomeAcknowledgedUnverified Outcome = "acknowledged_unverified"
	// OutcomeApplied means the request was sent, dispatch completed, and an
	// exact current-generation readback after completion confirms the effect.
	OutcomeApplied Outcome = "applied"
	// OutcomeNoEffect means the request was sent and evidence shows the effect
	// did not occur.
	OutcomeNoEffect Outcome = "no_effect"
	// OutcomeConflict means post-dispatch readback contradicts the effect.
	OutcomeConflict Outcome = "conflict"
	// OutcomeIndeterminate means dispatch may have had a side effect and the
	// evidence proves neither applied nor no effect. Blind retry and fallback
	// to another route are forbidden.
	OutcomeIndeterminate Outcome = "indeterminate"
)

// ExecutionRecord is the immutable ContractOperationV1 evidence record of one
// intent. Only a rejected record carries ErrorID, and it omits admission,
// route, dispatch, acknowledgement and readback; every other outcome omits
// ErrorID and needs admission, route, dispatch and non-empty OutcomeEvidence.
// Later reconciliation never rewrites a record.
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
