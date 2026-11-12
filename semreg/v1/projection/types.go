// Package projection implements the protocol-neutral v1 projection report and
// compatibility-alias records. It has no registry, route, native, or retained
// lifecycle state.
package projection

import semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"

const (
	// ContractProjectionV1 is the immutable projection report contract ID.
	ContractProjectionV1 semreg.ContractVersion = "helianthus.semantic.projection/v1"
	// ContractAliasV1 is the immutable compatibility alias contract ID.
	ContractAliasV1 semreg.ContractVersion = "helianthus.semantic.alias/v1"
)

// ItemKind is the kind of semantic item a projection target requests. The
// same item ID under two kinds is two independent requests.
type ItemKind string

// ItemKind values.
const (
	// ItemFact requests a fact.
	ItemFact ItemKind = "fact"
	// ItemRelation requests a relation.
	ItemRelation ItemKind = "relation"
	// ItemCapability requests a capability.
	ItemCapability ItemKind = "capability"
	// ItemOperation requests an operation.
	ItemOperation ItemKind = "operation"
)

// LossKind names the aspect of a semantic item that a transformed projection
// does not carry over unchanged.
type LossKind string

// LossKind values: each names the aspect that was lost or changed, such as the
// unit, value range, precision, time domain, symbol set, provenance, identity,
// capability, operation or policy of the source items.
const (
	LossUnit       LossKind = "unit"
	LossRange      LossKind = "range"
	LossPrecision  LossKind = "precision"
	LossTime       LossKind = "time"
	LossSymbol     LossKind = "symbol"
	LossProvenance LossKind = "provenance"
	LossIdentity   LossKind = "identity"
	LossCapability LossKind = "capability"
	LossOperation  LossKind = "operation"
	LossPolicy     LossKind = "policy"
)

// ProjectionOutcome is the disposition of one requested item in a projection
// report.
type ProjectionOutcome string

// ProjectionOutcome values. ProjectionExact forbids loss details,
// ProjectionTransformed requires at least one, and every other outcome requires
// a reason; ProjectionDisposition.Validate reports a violation as
// projection_incomplete.
const (
	// ProjectionExact means the item is projected without loss.
	ProjectionExact ProjectionOutcome = "exact"
	// ProjectionTransformed means the item is projected with the listed loss.
	ProjectionTransformed ProjectionOutcome = "transformed"
	// ProjectionWithheld means the item is not projected, for the stated
	// reason.
	ProjectionWithheld ProjectionOutcome = "withheld"
	// ProjectionUnrepresentable means the target cannot express the item.
	ProjectionUnrepresentable ProjectionOutcome = "unrepresentable"
	// ProjectionUnsupported means the mapping does not support the item.
	ProjectionUnsupported ProjectionOutcome = "unsupported"
	// ProjectionUnknown means the disposition of the item is not established.
	ProjectionUnknown ProjectionOutcome = "unknown"
)

// ProjectionManifest identifies the target, its version, the kernel contract
// (always semreg.ContractKernelV1), the exact pack versions sorted by ID and
// version without duplicates, and the mapping revision a report was produced
// with.
type ProjectionManifest struct {
	TargetID        semreg.TargetID        `json:"target_id"`
	TargetVersion   semreg.VersionLabel    `json:"target_version"`
	KernelVersion   semreg.ContractVersion `json:"kernel_version"`
	PackVersions    []semreg.PackRef       `json:"pack_versions"`
	MappingRevision semreg.Uint64          `json:"mapping_revision"`
}

// RequestedItem is one (kind, item ID) tuple a projection target asked for.
type RequestedItem struct {
	ItemID semreg.DefinitionID `json:"item_id"`
	Kind   ItemKind            `json:"kind"`
}

// LossDetail describes one loss in a transformed projection. SourceItems is
// sorted and unique, with at most 32 entries; Description is public NFC text
// without control characters, never native payload. Kind, SourceItems and
// Description form the collection key; Reversible is not part of it.
type LossDetail struct {
	Kind        LossKind              `json:"kind"`
	SourceItems []semreg.DefinitionID `json:"source_items"`
	Description string                `json:"description"`
	Reversible  bool                  `json:"reversible"`
}

// ProjectionDisposition is the result for one requested (kind, item ID) tuple:
// its outcome, the sorted unique source fact keys (at most 32), the sorted
// unique loss details (at most 32) and an optional reason, which the withheld,
// unrepresentable, unsupported and unknown outcomes require.
type ProjectionDisposition struct {
	Kind       ItemKind             `json:"kind"`
	ItemID     semreg.DefinitionID  `json:"item_id"`
	Outcome    ProjectionOutcome    `json:"outcome"`
	SourceKeys []semreg.FactKey     `json:"source_keys"`
	Loss       []LossDetail         `json:"loss"`
	Reason     *semreg.DefinitionID `json:"reason,omitempty"`
}

// ProjectionReport is the ContractProjectionV1 record of one projection of one
// snapshot. Every requested tuple has exactly one disposition with the same
// tuple; both lists are sorted by kind and item ID and hold at most 4096
// entries. A report preserves snapshot and causal revisions and cannot create
// facts, capabilities, authority, intents or routes.
type ProjectionReport struct {
	Contract     semreg.ContractVersion  `json:"contract"`
	Manifest     ProjectionManifest      `json:"manifest"`
	SnapshotID   semreg.SnapshotID       `json:"snapshot_id"`
	Revisions    semreg.RevisionVector   `json:"revisions"`
	Requested    []RequestedItem         `json:"requested"`
	Dispositions []ProjectionDisposition `json:"dispositions"`
	Causal       *semreg.CausalContext   `json:"causal,omitempty"`
}

// CompatibilityAlias is a ContractAliasV1 record that keeps a legacy public
// identifier pointing at an asset during migration. Routable must be false: an
// alias never selects a binding, capability, native endpoint or operation
// route. Evidence holds 1 to 32 unique references in canonical order, and
// ValidUntil, when present, is later than ValidFrom. Expiry does not delete
// historical snapshots.
type CompatibilityAlias struct {
	AliasContract semreg.ContractVersion  `json:"alias_contract"`
	LegacyID      semreg.OpaqueID         `json:"legacy_id"`
	AssetID       semreg.AssetID          `json:"asset_id"`
	ValidFrom     semreg.SemanticVersion  `json:"valid_from"`
	ValidUntil    *semreg.SemanticVersion `json:"valid_until,omitempty"`
	Routable      bool                    `json:"routable"`
	Evidence      []semreg.EvidenceRef    `json:"evidence"`
}

// ContractDiscriminator names the contract member semreg.Decode checks before
// binding a report.
func (ProjectionReport) ContractDiscriminator() (string, semreg.ContractVersion) {
	return "contract", ContractProjectionV1
}

// WireFixedContract declares that kernel_version must equal
// semreg.ContractKernelV1 on the wire; an absent member stays missing_member.
func (ProjectionManifest) WireFixedContract() (string, semreg.ContractVersion) {
	return "kernel_version", semreg.ContractKernelV1
}

// WireCollectionKeyFields lists the members that identify a loss detail, so
// semreg.Decode still reports duplicate or misordered loss details when another
// member prevents binding the whole record.
func (LossDetail) WireCollectionKeyFields() []string {
	return []string{"kind", "source_items", "description"}
}

// ContractDiscriminator names the alias_contract member semreg.Decode checks
// before binding an alias.
func (CompatibilityAlias) ContractDiscriminator() (string, semreg.ContractVersion) {
	return "alias_contract", ContractAliasV1
}
