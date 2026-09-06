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

type ItemKind string

const (
	ItemFact       ItemKind = "fact"
	ItemRelation   ItemKind = "relation"
	ItemCapability ItemKind = "capability"
	ItemOperation  ItemKind = "operation"
)

type LossKind string

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

type ProjectionOutcome string

const (
	ProjectionExact           ProjectionOutcome = "exact"
	ProjectionTransformed     ProjectionOutcome = "transformed"
	ProjectionWithheld        ProjectionOutcome = "withheld"
	ProjectionUnrepresentable ProjectionOutcome = "unrepresentable"
	ProjectionUnsupported     ProjectionOutcome = "unsupported"
	ProjectionUnknown         ProjectionOutcome = "unknown"
)

type ProjectionManifest struct {
	TargetID        semreg.TargetID        `json:"target_id"`
	TargetVersion   semreg.VersionLabel    `json:"target_version"`
	KernelVersion   semreg.ContractVersion `json:"kernel_version"`
	PackVersions    []semreg.PackRef       `json:"pack_versions"`
	MappingRevision semreg.Uint64          `json:"mapping_revision"`
}

type RequestedItem struct {
	ItemID semreg.DefinitionID `json:"item_id"`
	Kind   ItemKind            `json:"kind"`
}

type LossDetail struct {
	Kind        LossKind              `json:"kind"`
	SourceItems []semreg.DefinitionID `json:"source_items"`
	Description string                `json:"description"`
	Reversible  bool                  `json:"reversible"`
}

type ProjectionDisposition struct {
	Kind       ItemKind             `json:"kind"`
	ItemID     semreg.DefinitionID  `json:"item_id"`
	Outcome    ProjectionOutcome    `json:"outcome"`
	SourceKeys []semreg.FactKey     `json:"source_keys"`
	Loss       []LossDetail         `json:"loss"`
	Reason     *semreg.DefinitionID `json:"reason,omitempty"`
}

type ProjectionReport struct {
	Contract     semreg.ContractVersion  `json:"contract"`
	Manifest     ProjectionManifest      `json:"manifest"`
	SnapshotID   semreg.SnapshotID       `json:"snapshot_id"`
	Revisions    semreg.RevisionVector   `json:"revisions"`
	Requested    []RequestedItem         `json:"requested"`
	Dispositions []ProjectionDisposition `json:"dispositions"`
	Causal       *semreg.CausalContext   `json:"causal,omitempty"`
}

type CompatibilityAlias struct {
	AliasContract semreg.ContractVersion  `json:"alias_contract"`
	LegacyID      semreg.OpaqueID         `json:"legacy_id"`
	AssetID       semreg.AssetID          `json:"asset_id"`
	ValidFrom     semreg.SemanticVersion  `json:"valid_from"`
	ValidUntil    *semreg.SemanticVersion `json:"valid_until,omitempty"`
	Routable      bool                    `json:"routable"`
	Evidence      []semreg.EvidenceRef    `json:"evidence"`
}

func (ProjectionReport) ContractDiscriminator() (string, semreg.ContractVersion) {
	return "contract", ContractProjectionV1
}

func (ProjectionManifest) ContractDiscriminator() (string, semreg.ContractVersion) {
	return "kernel_version", semreg.ContractKernelV1
}

func (LossDetail) WireCollectionKeyFields() []string {
	return []string{"kind", "source_items", "description"}
}

func (CompatibilityAlias) ContractDiscriminator() (string, semreg.ContractVersion) {
	return "alias_contract", ContractAliasV1
}
