// Package catalog defines the protocol-neutral family catalog: one row per
// device or protocol family with the qualification state its published
// evidence supports today. A row never creates a native mapping, route,
// authority or live operation; it only records state and where the evidence
// lives. The catalog imports no transport, protocol, vendor or gateway code.
package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Contract identifies the catalog document version.
const Contract = "helianthus.semreg.family-catalog/v1"

// Composition records how the family reaches consumers today.
type Composition string

const (
	// CompositionComposed means the family is composed through SemReg.
	CompositionComposed Composition = "composed"
	// CompositionComposedLegacy means the family is composed on a legacy layer,
	// not through a SemReg pack.
	CompositionComposedLegacy Composition = "composed_legacy"
	// CompositionProviderLocal means the native provider emits the family and no
	// gateway composition is claimed.
	CompositionProviderLocal Composition = "provider_local"
	// CompositionNotComposed means no composition is claimed.
	CompositionNotComposed Composition = "not_composed"
)

// Reason explains why a row is not qualified. The semantic contract has no
// "evidence-blocked" or "no admissible profile" qualification value, so those
// conditions stay candidate or unknown and are named here instead.
type Reason string

const (
	ReasonOfflineProfileOnly         Reason = "offline_profile_only"
	ReasonOfflineMappingGateOnly     Reason = "offline_mapping_gate_only"
	ReasonFamilyCompatibleOffline    Reason = "family_compatible_offline"
	ReasonNoAdmissibleProfile        Reason = "no_admissible_profile"
	ReasonQualificationTestReadyOnly Reason = "qualification_test_ready_offline"
	ReasonEvidenceBlocked            Reason = "evidence_blocked"
	ReasonNoGoMissingEvidence        Reason = "no_go_missing_evidence"
	ReasonOfflineReplayOnly          Reason = "offline_replay_only"
	ReasonSemanticMappingUnknown     Reason = "semantic_mapping_unknown"
	ReasonLegacyLayerComposition     Reason = "legacy_layer_composition"
	ReasonUnknownPendingSTD01        Reason = "unknown_pending_std_01"
	ReasonNoProducer                 Reason = "no_producer"
)

var reasons = map[Reason]struct{}{
	ReasonOfflineProfileOnly: {}, ReasonOfflineMappingGateOnly: {}, ReasonFamilyCompatibleOffline: {},
	ReasonNoAdmissibleProfile: {}, ReasonQualificationTestReadyOnly: {}, ReasonEvidenceBlocked: {},
	ReasonNoGoMissingEvidence: {}, ReasonOfflineReplayOnly: {}, ReasonSemanticMappingUnknown: {},
	ReasonLegacyLayerComposition: {}, ReasonUnknownPendingSTD01: {}, ReasonNoProducer: {},
}

var compositions = map[Composition]struct{}{
	CompositionComposed: {}, CompositionComposedLegacy: {}, CompositionProviderLocal: {}, CompositionNotComposed: {},
}

// Evidence is one publishable evidence reference. Owner and Locator identify
// the public repository, revision or issue and the path or URL inside it.
type Evidence struct {
	Owner     string `json:"owner"`
	Kind      string `json:"kind"`
	Locator   string `json:"locator"`
	Statement string `json:"statement"`
}

// Family is one catalog row.
type Family struct {
	FamilyID     string               `json:"family_id"`
	Name         string               `json:"name"`
	NativeOwner  string               `json:"native_owner"`
	SemanticPack string               `json:"semantic_pack"`
	State        semreg.Qualification `json:"state"`
	Composition  Composition          `json:"composition"`
	Reason       Reason               `json:"reason"`
	Evidence     []Evidence           `json:"evidence"`
}

// Catalog is the decoded family catalog.
type Catalog struct {
	Contract        string                 `json:"contract"`
	SemanticDocsPin string                 `json:"semantic_docs_pin"`
	States          []semreg.Qualification `json:"states"`
	Families        []Family               `json:"families"`
}

// AllowedStates is the exact qualification vocabulary of the pinned semantic
// contract. The catalog adds no state of its own.
func AllowedStates() []semreg.Qualification {
	return []semreg.Qualification{
		semreg.QualificationCandidate, semreg.QualificationQualified, semreg.QualificationUnsupported,
		semreg.QualificationUnknown, semreg.QualificationRejected,
	}
}

// RequiredFamilyIDs lists the families of the INT-07 draft that the catalog
// must cover.
func RequiredFamilyIDs() []string {
	return []string{
		"sunspec-fronius-pv", "growatt-bms-rs485-v202", "tesla-gen3-evse", "tesla-legacy-fbe0-fde0",
		"growatt-protocol-ii", "huawei-smartlogger", "huawei-emma", "huawei-sdongle", "outback-axs",
		"growatt-bms-can-v104", "gree-vrf-can", "vaillant-ebus-thermal", "eebus-inbound", "infrastructure",
	}
}

// Decode strictly decodes and validates a catalog document.
func Decode(raw []byte) (Catalog, error) {
	var c Catalog
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Catalog{}, fmt.Errorf("family catalog: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Catalog{}, errors.New("family catalog: trailing data")
	}
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	return c, nil
}

// Validate checks structure, allowed states, evidence and completeness against
// RequiredFamilyIDs. It never invents or upgrades a state.
func (c Catalog) Validate() error {
	if c.Contract != Contract {
		return fmt.Errorf("family catalog: contract %q", c.Contract)
	}
	const pinPrefix = "Project-Helianthus/helianthus-docs-semantic@"
	if !strings.HasPrefix(c.SemanticDocsPin, pinPrefix) || len(c.SemanticDocsPin) != len(pinPrefix)+40 {
		return errors.New("family catalog: semantic docs pin must name a full commit")
	}
	allowed := map[semreg.Qualification]struct{}{}
	for _, s := range AllowedStates() {
		allowed[s] = struct{}{}
	}
	if len(c.States) != len(allowed) {
		return errors.New("family catalog: declared states differ from the semantic contract")
	}
	for _, s := range c.States {
		if _, ok := allowed[s]; !ok {
			return fmt.Errorf("family catalog: state %q is not in the semantic contract", s)
		}
	}
	seen := map[string]struct{}{}
	for _, f := range c.Families {
		if err := f.validate(allowed); err != nil {
			return fmt.Errorf("family %q: %w", f.FamilyID, err)
		}
		if _, dup := seen[f.FamilyID]; dup {
			return fmt.Errorf("family %q: duplicate", f.FamilyID)
		}
		seen[f.FamilyID] = struct{}{}
	}
	var missing []string
	for _, id := range RequiredFamilyIDs() {
		if _, ok := seen[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("family catalog: missing families %v", missing)
	}
	if len(seen) != len(RequiredFamilyIDs()) {
		return errors.New("family catalog: unlisted family")
	}
	return nil
}

func (f Family) validate(allowed map[semreg.Qualification]struct{}) error {
	if f.FamilyID == "" || f.Name == "" {
		return errors.New("identity is required")
	}
	if _, ok := allowed[f.State]; !ok {
		return fmt.Errorf("state %q is not allowed", f.State)
	}
	if _, ok := compositions[f.Composition]; !ok {
		return fmt.Errorf("composition %q is not allowed", f.Composition)
	}
	if _, ok := reasons[f.Reason]; !ok {
		return fmt.Errorf("reason %q is not allowed", f.Reason)
	}
	if len(f.Evidence) == 0 {
		return errors.New("evidence reference is required")
	}
	for _, e := range f.Evidence {
		if e.Owner == "" || e.Kind == "" || e.Locator == "" || e.Statement == "" {
			return errors.New("every evidence reference needs owner, kind, locator and statement")
		}
	}
	return nil
}
