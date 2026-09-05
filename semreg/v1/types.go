package semreg

// ContractKernelV1 is the root semantic-kernel contract identifier.
const ContractKernelV1 ContractVersion = "helianthus.semantic.kernel/v1"

type ContractVersion string
type DefinitionID string
type OpaqueID string
type Digest string
type ErrorID string
type SemanticVersion string
type VersionLabel string
type Uint64 string
type Int64 string

type AssetID OpaqueID
type SourceID OpaqueID
type SourceEpochID OpaqueID
type ClockEpochID OpaqueID
type NativeBindingID OpaqueID
type CandidateID OpaqueID
type ConflictID OpaqueID
type CapabilityInstanceID OpaqueID
type ServiceInstanceID OpaqueID
type SnapshotID OpaqueID
type BatchID OpaqueID
type IntentID OpaqueID
type AttemptID OpaqueID
type OriginID OpaqueID
type CorrelationID OpaqueID
type IdempotencyKey OpaqueID
type PolicyID OpaqueID
type TargetID OpaqueID

type VersionRange struct {
	Minimum          SemanticVersion `json:"minimum"`
	MaximumExclusive SemanticVersion `json:"maximum_exclusive"`
}
type EvidenceAccess string

const (
	EvidenceAccessPublic     EvidenceAccess = "public"
	EvidenceAccessAuthorized EvidenceAccess = "authorized"
	EvidenceAccessRestricted EvidenceAccess = "restricted"
)

type RedactionState string

const (
	RedactionNone         RedactionState = "none"
	RedactionRedacted     RedactionState = "redacted"
	RedactionMetadataOnly RedactionState = "metadata_only"
)

type EvidenceRef struct {
	Owner     DefinitionID    `json:"owner"`
	Kind      DefinitionID    `json:"kind"`
	Digest    Digest          `json:"digest"`
	Contract  ContractVersion `json:"contract"`
	Access    EvidenceAccess  `json:"access"`
	Redaction RedactionState  `json:"redaction"`
}

type SourceState string

const (
	SourceCurrent SourceState = "current"
	SourceRetired SourceState = "retired"
)

type TimePoint struct {
	UnixNanoseconds Int64        `json:"unix_nanoseconds"`
	ClockID         DefinitionID `json:"clock_id"`
	UncertaintyNS   Uint64       `json:"uncertainty_ns"`
}
type MonotonicPoint struct {
	ClockEpochID ClockEpochID `json:"clock_epoch_id"`
	Nanoseconds  Uint64       `json:"nanoseconds"`
}
type SourceDescriptor struct {
	SourceID         SourceID      `json:"source_id"`
	SourceEpochID    SourceEpochID `json:"source_epoch_id"`
	ProtocolID       DefinitionID  `json:"protocol_id"`
	ProfileID        DefinitionID  `json:"profile_id"`
	ProfileVersion   VersionLabel  `json:"profile_version"`
	RegistryEvidence EvidenceRef   `json:"registry_evidence"`
	StartedAt        TimePoint     `json:"started_at"`
	State            SourceState   `json:"state"`
	Revision         Uint64        `json:"revision"`
}
type OriginKind string

const (
	OriginNativeObservation OriginKind = "native_observation"
	OriginDerived           OriginKind = "derived"
	OriginOperator          OriginKind = "operator"
	OriginAutomation        OriginKind = "automation"
	OriginProjection        OriginKind = "projection"
)

type OriginRef struct {
	OriginID      OriginID         `json:"origin_id"`
	Kind          OriginKind       `json:"kind"`
	SourceID      *SourceID        `json:"source_id,omitempty"`
	SourceEpochID *SourceEpochID   `json:"source_epoch_id,omitempty"`
	BindingID     *NativeBindingID `json:"binding_id,omitempty"`
	Evidence      []EvidenceRef    `json:"evidence"`
}
type BindingState string

const (
	BindingCurrent BindingState = "current"
	BindingFenced  BindingState = "fenced"
	BindingRetired BindingState = "retired"
)

type NativeBinding struct {
	BindingID        NativeBindingID `json:"binding_id"`
	AssetID          AssetID         `json:"asset_id"`
	SourceID         SourceID        `json:"source_id"`
	SourceEpochID    SourceEpochID   `json:"source_epoch_id"`
	DriverGeneration Uint64          `json:"driver_generation"`
	NativeResource   EvidenceRef     `json:"native_resource"`
	State            BindingState    `json:"state"`
	Revision         Uint64          `json:"revision"`
}
type LinkState string

const (
	LinkCandidate LinkState = "candidate"
	LinkQualified LinkState = "qualified"
	LinkRejected  LinkState = "rejected"
	LinkConflict  LinkState = "conflict"
	LinkWithdrawn LinkState = "withdrawn"
)

type IdentityLink struct {
	AssetID   AssetID         `json:"asset_id"`
	BindingID NativeBindingID `json:"binding_id"`
	State     LinkState       `json:"state"`
	Basis     []EvidenceRef   `json:"basis"`
	Revision  Uint64          `json:"revision"`
}
type SourcePathRef struct {
	BindingID        NativeBindingID `json:"binding_id"`
	SourceID         SourceID        `json:"source_id"`
	SourceEpochID    SourceEpochID   `json:"source_epoch_id"`
	DriverGeneration Uint64          `json:"driver_generation"`
}
type DerivationInput struct {
	CandidateID       CandidateID     `json:"candidate_id"`
	CandidateRevision Uint64          `json:"candidate_revision"`
	SourcePaths       []SourcePathRef `json:"source_paths"`
}
type Derivation struct {
	Algorithm DefinitionID      `json:"algorithm"`
	Version   SemanticVersion   `json:"version"`
	Inputs    []DerivationInput `json:"inputs"`
	Evidence  []EvidenceRef     `json:"evidence"`
}

type Decimal struct {
	Coefficient string `json:"coefficient"`
	Exponent10  int32  `json:"exponent10"`
}
type Symbol struct {
	Namespace DefinitionID `json:"namespace"`
	Token     string       `json:"token"`
	Known     bool         `json:"known"`
}
type ValueKind string

const (
	ValueQuantity ValueKind = "quantity"
	ValueBoolean  ValueKind = "boolean"
	ValueText     ValueKind = "text"
	ValueSymbol   ValueKind = "symbol"
	ValueSymbols  ValueKind = "symbols"
	ValueTime     ValueKind = "time"
)

type Quantity struct {
	Number Decimal      `json:"number"`
	Unit   DefinitionID `json:"unit"`
}
type Value struct {
	Kind     ValueKind  `json:"kind"`
	Quantity *Quantity  `json:"quantity,omitempty"`
	Boolean  *bool      `json:"boolean,omitempty"`
	Text     *string    `json:"text,omitempty"`
	Symbol   *Symbol    `json:"symbol,omitempty"`
	Symbols  []Symbol   `json:"symbols,omitempty"`
	Time     *TimePoint `json:"time,omitempty"`
}
type Dimension struct {
	ID    DefinitionID `json:"id"`
	Value Value        `json:"value"`
}
type FactKey struct {
	PackID      DefinitionID    `json:"pack_id"`
	PackVersion SemanticVersion `json:"pack_version"`
	FactID      DefinitionID    `json:"fact_id"`
	Dimensions  []Dimension     `json:"dimensions"`
}
type Times struct {
	PhenomenonAt      *TimePoint     `json:"phenomenon_at,omitempty"`
	SourceAt          *TimePoint     `json:"source_at,omitempty"`
	ReceivedAt        TimePoint      `json:"received_at"`
	ReceiptMonotonic  MonotonicPoint `json:"receipt_monotonic"`
	EvaluatedAt       TimePoint      `json:"evaluated_at"`
	EvaluateMonotonic MonotonicPoint `json:"evaluate_monotonic"`
}
type FreshnessPolicy struct {
	PolicyID             PolicyID        `json:"policy_id"`
	Version              SemanticVersion `json:"version"`
	FreshForNS           Uint64          `json:"fresh_for_ns"`
	RetainForNS          Uint64          `json:"retain_for_ns"`
	MaxWallUncertaintyNS Uint64          `json:"max_wall_uncertainty_ns"`
}
type AssertionKind string

const (
	AssertionObserved AssertionKind = "observed"
	AssertionInferred AssertionKind = "inferred"
)

type Qualification string

const (
	QualificationCandidate   Qualification = "candidate"
	QualificationQualified   Qualification = "qualified"
	QualificationUnsupported Qualification = "unsupported"
	QualificationUnknown     Qualification = "unknown"
	QualificationRejected    Qualification = "rejected"
)

type Promotion string

const (
	PromotionUnpromoted Promotion = "unpromoted"
	PromotionPromoted   Promotion = "promoted"
)

type Validity string

const (
	ValidityGood    Validity = "good"
	ValiditySuspect Validity = "suspect"
	ValidityBad     Validity = "bad"
	ValidityUnknown Validity = "unknown"
)

type Availability string

const (
	AvailabilityAvailable   Availability = "available"
	AvailabilityDegraded    Availability = "degraded"
	AvailabilityUnavailable Availability = "unavailable"
	AvailabilityWithdrawn   Availability = "withdrawn"
)

type Freshness string

const (
	FreshnessFresh   Freshness = "fresh"
	FreshnessStale   Freshness = "stale"
	FreshnessExpired Freshness = "expired"
	FreshnessUnknown Freshness = "unknown"
)

type Quality struct {
	Assertion     AssertionKind  `json:"assertion"`
	Qualification Qualification  `json:"qualification"`
	Promotion     Promotion      `json:"promotion"`
	Validity      Validity       `json:"validity"`
	Availability  Availability   `json:"availability"`
	Freshness     Freshness      `json:"freshness"`
	Reasons       []DefinitionID `json:"reasons"`
}
type CausalContext struct {
	Origin              OriginRef      `json:"origin"`
	CorrelationID       CorrelationID  `json:"correlation_id"`
	ParentCorrelationID *CorrelationID `json:"parent_correlation_id,omitempty"`
	HopCount            uint16         `json:"hop_count"`
	MaxHops             uint16         `json:"max_hops"`
	FirstSeenAt         TimePoint      `json:"first_seen_at"`
	ExpiresAt           TimePoint      `json:"expires_at"`
	Path                []TargetID     `json:"path"`
}
type FactCandidate struct {
	CandidateID      CandidateID      `json:"candidate_id"`
	Key              FactKey          `json:"key"`
	Value            *Value           `json:"value,omitempty"`
	Quality          Quality          `json:"quality"`
	Times            Times            `json:"times"`
	FreshnessPolicy  FreshnessPolicy  `json:"freshness_policy"`
	BindingID        *NativeBindingID `json:"binding_id,omitempty"`
	SourceEpochID    *SourceEpochID   `json:"source_epoch_id,omitempty"`
	DriverGeneration *Uint64          `json:"driver_generation,omitempty"`
	Origin           OriginRef        `json:"origin"`
	Causal           *CausalContext   `json:"causal,omitempty"`
	Evidence         []EvidenceRef    `json:"evidence"`
	Derivation       *Derivation      `json:"derivation,omitempty"`
	Revision         Uint64           `json:"revision"`
}
type ConflictKind string

const ConflictValue ConflictKind = "value"

type ConflictState string

const ConflictOpen ConflictState = "open"

type Conflict struct {
	ConflictID ConflictID    `json:"conflict_id"`
	Kind       ConflictKind  `json:"kind"`
	Candidates []CandidateID `json:"candidates"`
	Evidence   []EvidenceRef `json:"evidence"`
	State      ConflictState `json:"state"`
}
type FactEnvelope struct {
	AssetID    AssetID         `json:"asset_id"`
	Key        FactKey         `json:"key"`
	Candidates []FactCandidate `json:"candidates"`
	Conflicts  []Conflict      `json:"conflicts"`
	Revision   Uint64          `json:"revision"`
}

type PackRef struct {
	ID      DefinitionID    `json:"id"`
	Version SemanticVersion `json:"version"`
}
type DefinitionRef struct {
	Pack    PackRef         `json:"pack"`
	ID      DefinitionID    `json:"id"`
	Version SemanticVersion `json:"version"`
}
type DefinitionIndex struct {
	Pack         PackRef         `json:"pack"`
	Fields       []DefinitionRef `json:"fields"`
	Services     []DefinitionRef `json:"services"`
	Capabilities []DefinitionRef `json:"capabilities"`
	Operations   []DefinitionRef `json:"operations"`
	EffectRules  []DefinitionRef `json:"effect_rules"`
}
type TypedField struct {
	ID    DefinitionID `json:"id"`
	Value Value        `json:"value"`
}
type PredicateOp string

const (
	PredicateEqual        PredicateOp = "equal"
	PredicateNotEqual     PredicateOp = "not_equal"
	PredicateLess         PredicateOp = "less"
	PredicateLessEqual    PredicateOp = "less_equal"
	PredicateGreater      PredicateOp = "greater"
	PredicateGreaterEqual PredicateOp = "greater_equal"
	PredicateContains     PredicateOp = "contains"
)

type ServiceInstance struct {
	InstanceID       ServiceInstanceID `json:"instance_id"`
	AssetID          AssetID           `json:"asset_id"`
	Definition       DefinitionRef     `json:"definition"`
	BindingID        NativeBindingID   `json:"binding_id"`
	SourceEpochID    SourceEpochID     `json:"source_epoch_id"`
	DriverGeneration Uint64            `json:"driver_generation"`
	Qualification    Qualification     `json:"qualification"`
	Availability     Availability      `json:"availability"`
	Revision         Uint64            `json:"revision"`
}
type CapabilityInstance struct {
	InstanceID         CapabilityInstanceID `json:"instance_id"`
	AssetID            AssetID              `json:"asset_id"`
	ServiceInstance    ServiceInstanceID    `json:"service_instance"`
	Definition         DefinitionRef        `json:"definition"`
	BindingID          NativeBindingID      `json:"binding_id"`
	SourceEpochID      SourceEpochID        `json:"source_epoch_id"`
	DriverGeneration   Uint64               `json:"driver_generation"`
	Qualification      Qualification        `json:"qualification"`
	Availability       Availability         `json:"availability"`
	Constraints        []TypedField         `json:"constraints"`
	ActivationEvidence []EvidenceRef        `json:"activation_evidence"`
	Revision           Uint64               `json:"revision"`
}

type PackValidator interface {
	Pack() PackRef
	Definitions() DefinitionIndex
	ValidateFact(FactKey, *Value) error
	ValidateService(ServiceInstance) error
	ValidateCapability(CapabilityInstance) error
	ValidateField(DefinitionRef, TypedField) error
	MatchConstraints(CapabilityInstance, []TypedField) error
	EvaluatePredicate(FactCandidate, PredicateOp, Value) (bool, error)
}
