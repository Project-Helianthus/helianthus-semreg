package semreg

// ContractKernelV1 is the root semantic-kernel contract identifier. It is the
// required contract of PublicationBatch and Snapshot. The normative record
// contract is owned by
// https://github.com/Project-Helianthus/helianthus-docs-semantic/blob/main/api/v1/kernel.md.
const ContractKernelV1 ContractVersion = "helianthus.semantic.kernel/v1"

// ContractEvaluationV1 and ContractSelectionV1 identify the two immutable
// time/presentation records. They are deliberately separate from Snapshot.
// ContractRetainedObservationV1 is the required contract of every
// RetainedObservationRecord.
const (
	ContractEvaluationV1          ContractVersion = "helianthus.semantic.evaluation/v1"
	ContractSelectionV1           ContractVersion = "helianthus.semantic.selection/v1"
	ContractRetainedObservationV1 ContractVersion = "helianthus.semantic.retained-observation/v1"
)

// ContractVersion names a versioned record contract and must end in a
// "/vN" or ".vN" major-version suffix. Records compare it for exact
// equality; a mismatch is invalid_contract, never a compatible fallback.
type ContractVersion string

// DefinitionID is a lowercase dotted, dashed or underscored identifier owned
// by a capability pack or contract. The kernel checks its syntax only and
// never assigns domain meaning to it.
type DefinitionID string

// OpaqueID is the case-sensitive base of every identity alias below. It is
// never trimmed or case-folded, and string equality between two different
// aliases does not make them the same identity.
type OpaqueID string

// Digest is "sha256:" followed by 64 lowercase hexadecimal characters. It
// identifies bytes under an owning contract and grants neither access nor
// qualification.
type Digest string

// ErrorID is a stable rejection identifier. Validate accepts only the
// identifiers this package defines; Error.Detail may add context but never
// replaces the identifier.
type ErrorID string

// SemanticVersion is a strict MAJOR.MINOR.PATCH version without leading
// zeroes, prerelease or build metadata. It orders numerically by component.
type SemanticVersion string

// VersionLabel preserves an externally owned protocol, profile, firmware or
// target version verbatim. It compares only for exact equality; lexical order
// does not mean newer.
type VersionLabel string

// Uint64 is an unsigned 64-bit integer carried as a canonical decimal string
// so JSON decoders cannot lose precision. Sequences, generations, revisions,
// monotonic ticks and nanosecond durations use it.
type Uint64 string

// Int64 is a signed 64-bit integer carried as a canonical decimal string; it
// holds wall-clock Unix nanoseconds.
type Int64 string

// AssetID identifies the one asset a PublicationKernel and its snapshots
// describe. A native bus or network address is not an AssetID.
type AssetID OpaqueID

// SourceID identifies one native source; the native owner allocates it.
type SourceID OpaqueID

// SourceEpochID identifies one sequencing epoch of a source. The native owner
// allocates a new unpredictable epoch whenever sequencing can restart without
// proved continuation or the source profile/version changes.
type SourceEpochID OpaqueID

// ClockEpochID identifies one monotonic clock realization. A restarted or
// reset monotonic clock needs a new epoch; ticks from different epochs are
// never ordered or subtracted.
type ClockEpochID OpaqueID

// NativeBindingID identifies an opaque route reference allocated by the native
// adapter. It is never a decoded native address or a live handle.
type NativeBindingID OpaqueID

// CandidateID is the stable logical identity of a fact candidate. It is unique
// across all envelopes of one snapshot, while retained history may hold
// several revisions or paths of the same CandidateID.
type CandidateID OpaqueID

// ConflictID identifies a kernel-derived value conflict. The kernel computes
// it as a digest; publishers never supply one.
type ConflictID OpaqueID

// CapabilityInstanceID identifies one published CapabilityInstance.
type CapabilityInstanceID OpaqueID

// ServiceInstanceID identifies one published ServiceInstance.
type ServiceInstanceID OpaqueID

// SnapshotID identifies a Snapshot by the digest of its canonical JSON with
// snapshot_id omitted, so equal IDs mean byte-identical snapshot content.
type SnapshotID OpaqueID

// BatchID is the publisher-chosen label of one PublicationBatch. Replay and
// ordering use the batch sequence and digest, not this label.
type BatchID OpaqueID

// IntentID identifies one operation intent in the operation package.
type IntentID OpaqueID

// AttemptID identifies one dispatch attempt of an operation intent.
type AttemptID OpaqueID

// OriginID identifies the lineage record of an OriginRef.
type OriginID OpaqueID

// CorrelationID links the records of one causal chain; see CausalContext.
type CorrelationID OpaqueID

// IdempotencyKey deduplicates operation admission: the operation kernel
// returns the prior admission for a repeated key with identical canonical
// intent bytes and rejects reuse with different bytes as sequence_conflict.
type IdempotencyKey OpaqueID

// PolicyID identifies a freshness or presentation-selection policy; it is
// always paired with a SemanticVersion.
type PolicyID OpaqueID

// TargetID identifies one projection target or causal hop receiver.
type TargetID OpaqueID

// VersionRange is the half-open range [Minimum, MaximumExclusive). Validate
// requires Minimum below MaximumExclusive; matching never falls back to
// another definition ID.
type VersionRange struct {
	Minimum          SemanticVersion `json:"minimum"`
	MaximumExclusive SemanticVersion `json:"maximum_exclusive"`
}

// EvidenceAccess states who may resolve the evidence an EvidenceRef names. A
// digest never grants access by itself.
type EvidenceAccess string

// EvidenceAccess values. Restricted evidence must not use RedactionNone.
const (
	EvidenceAccessPublic     EvidenceAccess = "public"
	EvidenceAccessAuthorized EvidenceAccess = "authorized"
	EvidenceAccessRestricted EvidenceAccess = "restricted"
)

// RedactionState states how much of the referenced evidence a reader of the
// record may see.
type RedactionState string

// RedactionState values.
const (
	RedactionNone         RedactionState = "none"
	RedactionRedacted     RedactionState = "redacted"
	RedactionMetadataOnly RedactionState = "metadata_only"
)

// EvidenceRef points at evidence by owner, kind, contract and digest. It
// carries no payload or locator; the owning native repository remains
// responsible for the object, its authorization, retention and redaction.
type EvidenceRef struct {
	Owner     DefinitionID    `json:"owner"`
	Kind      DefinitionID    `json:"kind"`
	Digest    Digest          `json:"digest"`
	Contract  ContractVersion `json:"contract"`
	Access    EvidenceAccess  `json:"access"`
	Redaction RedactionState  `json:"redaction"`
}

// SourceState is the lifecycle of a SourceDescriptor.
type SourceState string

// SourceState values. Publishers upsert only SourceCurrent; SourceRetired is an
// irreversible kernel-produced tombstone, and a publisher-supplied retired
// upsert is stale_source_epoch.
const (
	SourceCurrent SourceState = "current"
	SourceRetired SourceState = "retired"
)

// TimePoint is a wall-clock estimate in Unix nanoseconds under the clock named
// by ClockID, with UncertaintyNS in nanoseconds. Only "clock.utc" points are
// compared across monotonic epochs; zero uncertainty asserts exactness and is
// not a default for an unknown clock.
type TimePoint struct {
	UnixNanoseconds Int64        `json:"unix_nanoseconds"`
	ClockID         DefinitionID `json:"clock_id"`
	UncertaintyNS   Uint64       `json:"uncertainty_ns"`
}

// MonotonicPoint is a nanosecond tick count within one monotonic clock epoch.
// Points are comparable only when ClockEpochID is identical.
type MonotonicPoint struct {
	ClockEpochID ClockEpochID `json:"clock_epoch_id"`
	Nanoseconds  Uint64       `json:"nanoseconds"`
}

// SourceDescriptor describes one epoch of a native source. ProfileID and
// ProfileVersion are native evidence, not capability proof. Revision is
// positive and orders metadata changes within the epoch; a snapshot holds at
// most one current epoch per SourceID.
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

// OriginKind classifies where a candidate came from.
type OriginKind string

// OriginKind values. Only OriginNativeObservation carries a native source
// path; OriginDerived requires a Derivation on the candidate and
// OriginProjection requires a causal context.
const (
	OriginNativeObservation OriginKind = "native_observation"
	OriginDerived           OriginKind = "derived"
	OriginOperator          OriginKind = "operator"
	OriginAutomation        OriginKind = "automation"
	OriginProjection        OriginKind = "projection"
)

// OriginRef records lineage. The source, epoch and binding pointers are
// required for a native observation and must be absent for every other kind.
// Evidence holds 1 through 32 unique, sorted references. An origin never
// grants operation authority.
type OriginRef struct {
	OriginID      OriginID         `json:"origin_id"`
	Kind          OriginKind       `json:"kind"`
	SourceID      *SourceID        `json:"source_id,omitempty"`
	SourceEpochID *SourceEpochID   `json:"source_epoch_id,omitempty"`
	BindingID     *NativeBindingID `json:"binding_id,omitempty"`
	Evidence      []EvidenceRef    `json:"evidence"`
}

// BindingState is the lifecycle of a NativeBinding.
type BindingState string

// BindingState values. Publishers upsert only BindingCurrent; BindingFenced
// and BindingRetired are irreversible kernel-produced tombstones that resolve
// references but are never routes.
const (
	BindingCurrent BindingState = "current"
	BindingFenced  BindingState = "fenced"
	BindingRetired BindingState = "retired"
)

// NativeBinding ties an asset to one native resource of one source epoch and
// driver generation. The native adapter allocates the binding and keeps sole
// ownership of any live handle. DriverGeneration and Revision are positive.
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

// LinkState is the qualification state of an IdentityLink.
type LinkState string

// LinkState values. Contradictory qualified links become LinkConflict; the
// kernel never chooses one silently. Fencing or retirement withdraws links.
const (
	LinkCandidate LinkState = "candidate"
	LinkQualified LinkState = "qualified"
	LinkRejected  LinkState = "rejected"
	LinkConflict  LinkState = "conflict"
	LinkWithdrawn LinkState = "withdrawn"
)

// IdentityLink records the evidence that a binding belongs to an asset. Basis
// holds 1 through 32 unique, sorted references; an empty basis is
// identity_not_qualified. A non-withdrawn link must resolve a current binding.
// A link is never identity or route authority. The optional Resource states
// that the binding observes that resource, and the optional Key is the
// identity-object digest of the unit's native token seen through the binding.
type IdentityLink struct {
	AssetID   AssetID         `json:"asset_id"`
	BindingID NativeBindingID `json:"binding_id"`
	State     LinkState       `json:"state"`
	Basis     []EvidenceRef   `json:"basis"`
	Revision  Uint64          `json:"revision"`
	Resource  *ResourceID     `json:"resource,omitempty"`
	Key       *Digest         `json:"key,omitempty"`
}

// SourcePathRef names one exact native dependency without a live handle. All
// members must resolve to the same current NativeBinding.
type SourcePathRef struct {
	BindingID        NativeBindingID `json:"binding_id"`
	SourceID         SourceID        `json:"source_id"`
	SourceEpochID    SourceEpochID   `json:"source_epoch_id"`
	DriverGeneration Uint64          `json:"driver_generation"`
}

// DerivationInput binds one exact candidate revision and every transitive
// source path it depends on: 1 through 32 paths sorted by source, epoch,
// generation and binding. A revision or path-set mismatch with the referenced
// candidate rejects the publication.
type DerivationInput struct {
	CandidateID       CandidateID     `json:"candidate_id"`
	CandidateRevision Uint64          `json:"candidate_revision"`
	SourcePaths       []SourcePathRef `json:"source_paths"`
}

// Derivation is required on an inferred candidate and forbidden on an observed
// one. Inputs holds 1 through 32 candidates sorted by ID; a duplicate or
// self-reference is derivation_cycle. Evidence may be empty but not nil.
type Derivation struct {
	Algorithm DefinitionID      `json:"algorithm"`
	Version   SemanticVersion   `json:"version"`
	Inputs    []DerivationInput `json:"inputs"`
	Evidence  []EvidenceRef     `json:"evidence"`
}

// Decimal is the exact number Coefficient × 10^Exponent10. The coefficient is
// a canonical decimal integer without trailing zeroes (zero uses exponent 0)
// and the exponent lies in -18..18. Floating point is never authoritative.
type Decimal struct {
	Coefficient string `json:"coefficient"`
	Exponent10  int32  `json:"exponent10"`
}

// Symbol is an enumerated token. Known symbols belong to a capability-pack
// namespace and must be declared by that exact pack version; an unknown native
// token round-trips under a "native." namespace with Known false and never
// becomes a supported semantic symbol.
type Symbol struct {
	Namespace DefinitionID `json:"namespace"`
	Token     string       `json:"token"`
	Known     bool         `json:"known"`
}

// ValueKind selects which single payload of a Value is present.
type ValueKind string

// ValueKind values.
const (
	ValueQuantity ValueKind = "quantity"
	ValueBoolean  ValueKind = "boolean"
	ValueText     ValueKind = "text"
	ValueSymbol   ValueKind = "symbol"
	ValueSymbols  ValueKind = "symbols"
	ValueTime     ValueKind = "time"
)

// Quantity is an exact number with its unit. The unit is a pack-owned
// definition ID; no conversion or default unit is applied.
type Quantity struct {
	Number Decimal      `json:"number"`
	Unit   DefinitionID `json:"unit"`
}

// Value is a tagged union: exactly the payload matching Kind is non-nil and
// every other payload is nil. Text is NFC UTF-8 of at most 4096 bytes;
// Symbols holds 1 through 64 unique symbols sorted by namespace then token.
// The owning pack defines the allowed kind, unit, range and symbol set.
type Value struct {
	Kind     ValueKind  `json:"kind"`
	Quantity *Quantity  `json:"quantity,omitempty"`
	Boolean  *bool      `json:"boolean,omitempty"`
	Text     *string    `json:"text,omitempty"`
	Symbol   *Symbol    `json:"symbol,omitempty"`
	Symbols  []Symbol   `json:"symbols,omitempty"`
	Time     *TimePoint `json:"time,omitempty"`
}

// Dimension qualifies a fact key with a pack-defined member. Its value is
// restricted to boolean, text, symbol or quantity.
type Dimension struct {
	ID    DefinitionID `json:"id"`
	Value Value        `json:"value"`
}

// FactKey identifies one fact by all four members together; a key of one pack
// version never equals the same text in another version. Dimensions holds at
// most 16 entries sorted by ID and must be non-nil.
type FactKey struct {
	PackID      DefinitionID    `json:"pack_id"`
	PackVersion SemanticVersion `json:"pack_version"`
	FactID      DefinitionID    `json:"fact_id"`
	Dimensions  []Dimension     `json:"dimensions"`
}

// Times records the time axes of one candidate. Missing source or phenomenon
// time stays nil and is never synthesized. Receipt and evaluation are
// required; within one monotonic epoch evaluation must not precede receipt.
type Times struct {
	PhenomenonAt      *TimePoint     `json:"phenomenon_at,omitempty"`
	SourceAt          *TimePoint     `json:"source_at,omitempty"`
	ReceivedAt        TimePoint      `json:"received_at"`
	ReceiptMonotonic  MonotonicPoint `json:"receipt_monotonic"`
	EvaluatedAt       TimePoint      `json:"evaluated_at"`
	EvaluateMonotonic MonotonicPoint `json:"evaluate_monotonic"`
}

// FreshnessPolicy is a pack-supplied lifetime in nanoseconds: fresh below
// FreshForNS, stale below RetainForNS, expired afterwards. FreshForNS is
// positive and RetainForNS exceeds it. Across monotonic epochs only clock.utc
// points whose summed uncertainty is within MaxWallUncertaintyNS are compared,
// conservatively; any other case is unknown, never fresh.
type FreshnessPolicy struct {
	PolicyID             PolicyID        `json:"policy_id"`
	Version              SemanticVersion `json:"version"`
	FreshForNS           Uint64          `json:"fresh_for_ns"`
	RetainForNS          Uint64          `json:"retain_for_ns"`
	MaxWallUncertaintyNS Uint64          `json:"max_wall_uncertainty_ns"`
}

// AssertionKind states whether a value was observed on a native path or
// inferred through a Derivation.
type AssertionKind string

// AssertionKind values. Observed candidates carry one native source path and
// no derivation; inferred candidates carry a derivation and no single path.
const (
	AssertionObserved AssertionKind = "observed"
	AssertionInferred AssertionKind = "inferred"
)

// Qualification is the evidence state of a fact, service or capability. It is
// independent of the other Quality axes.
type Qualification string

// Qualification values. Unsupported and rejected candidates carry no value;
// only qualified records may be promoted or actionable.
const (
	QualificationCandidate   Qualification = "candidate"
	QualificationQualified   Qualification = "qualified"
	QualificationUnsupported Qualification = "unsupported"
	QualificationUnknown     Qualification = "unknown"
	QualificationRejected    Qualification = "rejected"
)

// Promotion states whether a qualified value is exposed as a supported
// semantic fact.
type Promotion string

// Promotion values. PromotionPromoted requires qualified qualification, good
// or suspect validity, and available or degraded availability.
const (
	PromotionUnpromoted Promotion = "unpromoted"
	PromotionPromoted   Promotion = "promoted"
)

// Validity is the publisher's judgement of the value itself.
type Validity string

// Validity values.
const (
	ValidityGood    Validity = "good"
	ValiditySuspect Validity = "suspect"
	ValidityBad     Validity = "bad"
	ValidityUnknown Validity = "unknown"
)

// Availability is the reachability of a fact, service or capability.
// Evaluation may only lower it; it never improves a stored availability.
type Availability string

// Availability values. AvailabilityWithdrawn forbids a candidate value and
// marks non-actionable service and capability tombstones.
const (
	AvailabilityAvailable   Availability = "available"
	AvailabilityDegraded    Availability = "degraded"
	AvailabilityUnavailable Availability = "unavailable"
	AvailabilityWithdrawn   Availability = "withdrawn"
)

// Freshness is the age class of a value under its FreshnessPolicy. The stored
// value is publication-time only; current freshness comes from
// EvaluateSnapshot.
type Freshness string

// Freshness values. When inferred inputs are combined, expired wins, then
// unknown, then stale.
const (
	FreshnessFresh   Freshness = "fresh"
	FreshnessStale   Freshness = "stale"
	FreshnessExpired Freshness = "expired"
	FreshnessUnknown Freshness = "unknown"
)

// Quality keeps the independent axes of a candidate; they are never collapsed
// into one status. Reasons holds at most 16 unique, sorted namespaced codes and
// must be non-nil.
type Quality struct {
	Assertion     AssertionKind  `json:"assertion"`
	Qualification Qualification  `json:"qualification"`
	Promotion     Promotion      `json:"promotion"`
	Validity      Validity       `json:"validity"`
	Availability  Availability   `json:"availability"`
	Freshness     Freshness      `json:"freshness"`
	Reasons       []DefinitionID `json:"reasons"`
}

// CausalContext bounds a causal chain for echo and loop suppression. HopCount
// equals the length of Path, whose receivers are unique; MaxHops is 1 through
// 16. FirstSeenAt and ExpiresAt use clock.utc and span at most 300 seconds.
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

// FactCandidate is one alternative value for a FactKey. Value is required
// unless Quality forbids it. An observed candidate requires BindingID,
// SourceEpochID and a positive DriverGeneration and omits Derivation; an
// inferred candidate omits those three and requires Derivation. Evidence holds
// 1 through 32 unique references. Revision is positive and must change
// whenever any other member changes. Origin and Causal never grant authority.
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

// ConflictKind classifies a Conflict; v1 defines only ConflictValue.
type ConflictKind string

// ConflictValue marks promoted, qualified candidates of one key whose
// canonical values differ.
const ConflictValue ConflictKind = "value"

// ConflictState is the state of a Conflict; v1 defines only ConflictOpen.
type ConflictState string

// ConflictOpen is the only v1 conflict state. A conflict whose condition no
// longer holds is absent from the next snapshot rather than resolved.
const ConflictOpen ConflictState = "open"

// Conflict is kernel-derived metadata over 2 through 32 sorted candidate IDs;
// Evidence is the sorted union of their evidence. Publishers never submit,
// resolve or select conflicts, and a decoded conflict that differs from the
// derived one is invalid_value.
type Conflict struct {
	ConflictID ConflictID    `json:"conflict_id"`
	Kind       ConflictKind  `json:"kind"`
	Candidates []CandidateID `json:"candidates"`
	Evidence   []EvidenceRef `json:"evidence"`
	State      ConflictState `json:"state"`
}

// FactEnvelope groups 1 through 32 coexisting candidates of one key, sorted by
// candidate ID; every candidate repeats the envelope key exactly. Conflicts is
// derived by the kernel and must be non-nil. Revision increments once per
// batch whenever the candidates or derived conflicts change.
type FactEnvelope struct {
	AssetID    AssetID         `json:"asset_id"`
	Key        FactKey         `json:"key"`
	Candidates []FactCandidate `json:"candidates"`
	Conflicts  []Conflict      `json:"conflicts"`
	Revision   Uint64          `json:"revision"`
}

// RetainedRemoval records why an observed candidate left current state. It
// does not alter the retained copy or grant lifecycle authority.
type RetainedRemoval string

// RetainedRemoval values. A generation fence or a source retirement retains
// the affected observed candidates; an explicit fact withdrawal retains none.
const (
	RetainedRemovalGenerationFence  RetainedRemoval = "generation_fence"
	RetainedRemovalSourceRetirement RetainedRemoval = "source_retirement"
)

// RetainedObservationRecord preserves one immutable, formerly current
// observation after its source path has been fenced or retired. It is
// historical/read-only state: it never re-enters Facts, is not a replacement
// binding, and cannot satisfy operation authority. Candidate is an unmodified
// copy of the accepted candidate.
type RetainedObservationRecord struct {
	Contract  ContractVersion `json:"contract"`
	Candidate FactCandidate   `json:"candidate"`
	Removal   RetainedRemoval `json:"removal"`
}

// PackRef is the exact registry key of a capability pack. Pack identity is
// never parsed from a definition ID.
type PackRef struct {
	ID      DefinitionID    `json:"id"`
	Version SemanticVersion `json:"version"`
}

// DefinitionRef names one pack-owned definition. Its Pack must be the single
// registered owner of the definition's kind, ID and version.
type DefinitionRef struct {
	Pack    PackRef         `json:"pack"`
	ID      DefinitionID    `json:"id"`
	Version SemanticVersion `json:"version"`
}

// DefinitionIndex lists every definition a pack owns. Each collection is
// non-nil, sorted by ID then version, free of duplicates, and holds only
// references whose Pack equals the index Pack.
type DefinitionIndex struct {
	Pack         PackRef         `json:"pack"`
	Fields       []DefinitionRef `json:"fields"`
	Services     []DefinitionRef `json:"services"`
	Capabilities []DefinitionRef `json:"capabilities"`
	Operations   []DefinitionRef `json:"operations"`
	EffectRules  []DefinitionRef `json:"effect_rules"`
}

// TypedField is a value for a field definition that an accepted pack declares
// with its kind, unit, range and presence. Field collections hold at most 64
// entries sorted by ID.
type TypedField struct {
	ID    DefinitionID `json:"id"`
	Value Value        `json:"value"`
}

// PredicateOp is a comparison a pack validator evaluates against a fact.
type PredicateOp string

// PredicateOp values. A pack must reject operators or value/unit
// combinations its exact fact definition does not support.
const (
	PredicateEqual        PredicateOp = "equal"
	PredicateNotEqual     PredicateOp = "not_equal"
	PredicateLess         PredicateOp = "less"
	PredicateLessEqual    PredicateOp = "less_equal"
	PredicateGreater      PredicateOp = "greater"
	PredicateGreaterEqual PredicateOp = "greater_equal"
	PredicateContains     PredicateOp = "contains"
)

// ServiceInstance is a published service on one binding. DriverGeneration and
// Revision are positive. Unless withdrawn, it must resolve to a current binding
// of the same source epoch and generation. The optional Resource names the
// resource the service is about.
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
	Resource         *ResourceID       `json:"resource,omitempty"`
}

// CapabilityInstance is a published capability of a service on the same
// binding. ActivationEvidence holds 1 through 32 references (empty is
// capability_not_qualified) and stays as history on a tombstone. Constraints
// is non-nil and sorted. Only a qualified, current, non-withdrawn instance can
// be actionable; catalog completeness never creates one.
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

// GenerationFence is the irreversible publication boundary for one driver
// generation. Fenced objects remain resolvable in snapshots but non-actionable.
type GenerationFence struct {
	SourceID         SourceID      `json:"source_id"`
	SourceEpochID    SourceEpochID `json:"source_epoch_id"`
	DriverGeneration Uint64        `json:"driver_generation"`
	Reason           DefinitionID  `json:"reason"`
	Evidence         []EvidenceRef `json:"evidence"`
	Revision         Uint64        `json:"revision"`
}

// PublicationBatch is one complete atomic patch for an asset/source/epoch and
// generation. Empty collections are required and mean "no change". The graph
// collections are optional instead: each is omitted when empty, and a present
// empty one is invalid_value.
type PublicationBatch struct {
	Contract                 ContractVersion        `json:"contract"`
	BatchID                  BatchID                `json:"batch_id"`
	BatchDigest              Digest                 `json:"batch_digest"`
	AssetID                  AssetID                `json:"asset_id"`
	SourceID                 SourceID               `json:"source_id"`
	SourceEpochID            SourceEpochID          `json:"source_epoch_id"`
	DriverGeneration         Uint64                 `json:"driver_generation"`
	Sequence                 Uint64                 `json:"sequence"`
	ExpectedSemanticRevision Uint64                 `json:"expected_semantic_revision"`
	ObservedAt               TimePoint              `json:"observed_at"`
	SourceUpserts            []SourceDescriptor     `json:"source_upserts"`
	SourceRetirements        []SourceEpochID        `json:"source_retirements"`
	BindingUpserts           []NativeBinding        `json:"binding_upserts"`
	IdentityLinkUpserts      []IdentityLink         `json:"identity_link_upserts"`
	FactUpserts              []FactCandidate        `json:"fact_upserts"`
	FactWithdrawals          []CandidateID          `json:"fact_withdrawals"`
	ServiceUpserts           []ServiceInstance      `json:"service_upserts"`
	ServiceWithdrawals       []ServiceInstanceID    `json:"service_withdrawals"`
	CapabilityUpserts        []CapabilityInstance   `json:"capability_upserts"`
	CapabilityWithdrawals    []CapabilityInstanceID `json:"capability_withdrawals"`
	GenerationFences         []GenerationFence      `json:"generation_fences"`
	ResourceUpserts          []Resource             `json:"resource_upserts,omitempty"`
	RelationUpserts          []RelationInstance     `json:"relation_upserts,omitempty"`
	RelationWithdrawals      []RelationKey          `json:"relation_withdrawals,omitempty"`
	AbsenceUpserts           []RelationAbsence      `json:"absence_upserts,omitempty"`
	AbsenceWithdrawals       []RelationAbsenceKey   `json:"absence_withdrawals,omitempty"`
}

// RevisionVector is the per-asset revision set of a snapshot. Every present
// member is positive once published; Semantic increments once per accepted
// batch and each component increments only when that component changes.
// Structure counts changes to resources, relation instances and absence
// statements; it is omitted until the asset's first such change.
type RevisionVector struct {
	Semantic     Uint64 `json:"semantic"`
	Identity     Uint64 `json:"identity"`
	Facts        Uint64 `json:"facts"`
	Services     Uint64 `json:"services"`
	Capabilities Uint64 `json:"capabilities"`
	Structure    Uint64 `json:"structure,omitempty"`
}

// PublicationCursor is the kernel-maintained replay state of one source,
// epoch and generation. Sequences start at 1; only the last sequence with the
// identical batch digest replays, and an older sequence is sequence_conflict.
// Fenced is irreversible.
type PublicationCursor struct {
	SourceID         SourceID      `json:"source_id"`
	SourceEpochID    SourceEpochID `json:"source_epoch_id"`
	DriverGeneration Uint64        `json:"driver_generation"`
	LastSequence     Uint64        `json:"last_sequence"`
	LastBatchDigest  Digest        `json:"last_batch_digest"`
	Fenced           bool          `json:"fenced"`
}

// Snapshot is the complete immutable semantic state for one asset revision.
// Callers receive deep copies; CanonicalJSON returns the stable wire value.
// The graph collections are omitted when empty, so a snapshot without graph
// records keeps its canonical bytes and identifier.
type Snapshot struct {
	Contract          ContractVersion             `json:"contract"`
	SnapshotID        SnapshotID                  `json:"snapshot_id"`
	AssetID           AssetID                     `json:"asset_id"`
	Revisions         RevisionVector              `json:"revisions"`
	EvaluatedAt       TimePoint                   `json:"evaluated_at"`
	EvaluateMonotonic MonotonicPoint              `json:"evaluate_monotonic"`
	Sources           []SourceDescriptor          `json:"sources"`
	Bindings          []NativeBinding             `json:"bindings"`
	IdentityLinks     []IdentityLink              `json:"identity_links"`
	Facts             []FactEnvelope              `json:"facts"`
	Retained          []RetainedObservationRecord `json:"retained_observations,omitempty"`
	Services          []ServiceInstance           `json:"services"`
	Capabilities      []CapabilityInstance        `json:"capabilities"`
	Fences            []GenerationFence           `json:"fences"`
	Cursors           []PublicationCursor         `json:"cursors"`
	Resources         []Resource                  `json:"resources,omitempty"`
	Relations         []RelationInstance          `json:"relations,omitempty"`
	RelationAbsences  []RelationAbsence           `json:"relation_absences,omitempty"`
}

// EvaluationContext is supplied by the caller. Evaluation never obtains time
// from a process clock or from publication state.
type EvaluationContext struct {
	EvaluatedAt       TimePoint      `json:"evaluated_at"`
	EvaluateMonotonic MonotonicPoint `json:"evaluate_monotonic"`
}

// EvaluatedFact is the time-dependent result for one current candidate at an
// EvaluationContext. EffectiveAvailability starts from the stored availability
// and is only ever lowered by freshness; evaluation never promotes, qualifies
// or restores a candidate.
type EvaluatedFact struct {
	CandidateID           CandidateID  `json:"candidate_id"`
	CandidateRevision     Uint64       `json:"candidate_revision"`
	Freshness             Freshness    `json:"freshness"`
	EffectiveAvailability Availability `json:"effective_availability"`
}

// EvaluatedRetainedObservation exposes the original freshness lifetime for a
// historical observation. Retained state is deliberately separate from the
// current Fact evaluation collection so presentation selection and operations
// cannot mistake it for current authority.
type EvaluatedRetainedObservation struct {
	Observation RetainedObservationRecord `json:"observation"`
	Freshness   Freshness                 `json:"freshness"`
}

// EvaluationView is a complete, snapshot-bound, time-only result. Its digest
// is SHA-256 over this record with EvaluationDigest omitted.
type EvaluationView struct {
	Contract         ContractVersion                `json:"contract"`
	SnapshotID       SnapshotID                     `json:"snapshot_id"`
	Revisions        RevisionVector                 `json:"revisions"`
	Context          EvaluationContext              `json:"context"`
	Facts            []EvaluatedFact                `json:"facts"`
	Retained         []EvaluatedRetainedObservation `json:"retained_observations,omitempty"`
	EvaluationDigest Digest                         `json:"evaluation_digest"`
}

// Selection is a presentation-only result bound to a complete immutable
// snapshot and evaluation view. It is never part of either input record.
type Selection struct {
	Contract          ContractVersion   `json:"contract"`
	SnapshotID        SnapshotID        `json:"snapshot_id"`
	Revisions         RevisionVector    `json:"revisions"`
	EvaluationDigest  Digest            `json:"evaluation_digest"`
	Context           EvaluationContext `json:"context"`
	Key               FactKey           `json:"key"`
	PolicyID          PolicyID          `json:"policy_id"`
	PolicyVersion     SemanticVersion   `json:"policy_version"`
	SelectedCandidate CandidateID       `json:"selected_candidate"`
	CandidateRevision Uint64            `json:"candidate_revision"`
	PresentationOnly  bool              `json:"presentation_only"`
}

// SelectionPolicy owns only cross-source presentation choice. It cannot
// publish, route, or grant operation authority.
//
// Select must be pure and deterministic over its arguments, which are
// detached copies. The kernel holds no lock while Select runs and may call it
// from several goroutines at once. Any error, or a candidate outside the
// supplied envelope, is reported as invalid_value.
type SelectionPolicy interface {
	PolicyID() PolicyID
	Version() SemanticVersion
	Select(FactEnvelope, []EvaluatedFact) (CandidateID, error)
}

// PackValidator is the typed hook of one capability pack. NewRegistry reads
// Pack and Definitions once and freezes them. Before each later call the
// registry validates the record's structure and checks that the fact key or
// definition belongs to this pack, then passes detached copies. Calls into one
// registered hook are serialized, including across registry forks; a hook
// registered in several registries must synchronize itself. An error that is
// not an *Error is classified as invalid_value.
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
