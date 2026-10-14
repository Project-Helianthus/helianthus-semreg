// Package catalog defines the protocol-neutral family catalog: one row per
// device or protocol family with the qualification state its published
// evidence supports today. A row never creates a native mapping, route,
// authority or live operation; it only records state and where the evidence
// lives. The catalog imports no transport, protocol, vendor or gateway code.
//
// # Contract family-catalog/v1
//
// The document is a JSON object with members contract, semantic_docs_pin,
// states and families. Each family has family_id, name, native_owner,
// semantic_pack, state, composition, composition_verified, reason and
// evidence (owner, kind, locator, statement). native_owner and semantic_pack
// may be empty when no owner or pack exists. state is exactly one of the
// semantic Qualification values (candidate, qualified, unsupported, unknown,
// rejected) and the declared states member must equal that set. composition is
// composed, composed_legacy, provider_local or not_composed. A claimed
// composition requires composition_verified=true and a gateway_composition
// evidence reference; otherwise composition_verified is false and the value
// is unverified. reason is one of the Reason constants below.
//
// Compatibility rule: within v1 only additive values (new families, reasons,
// compositions or evidence kinds) may be added; removing or renaming a value,
// changing a member's meaning, or adding a member requires v2. The document
// rejects duplicate and unknown members at every object level. The
// documentation companion is Project-Helianthus/helianthus-docs-semantic#33.
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

// EvidenceKindGatewayComposition marks evidence citing gateway code at an
// exact revision for a composition claim.
const EvidenceKindGatewayComposition = "gateway_composition"

const gatewayOwnerPrefix = "Project-Helianthus/helianthus-ebusgateway@"

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
	// CompositionVerified is true only when gateway code at a pinned revision
	// is cited as gateway_composition evidence for a claimed composition.
	CompositionVerified bool       `json:"composition_verified"`
	Reason              Reason     `json:"reason"`
	Evidence            []Evidence `json:"evidence"`
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
	if err := rejectDuplicateKeys(raw); err != nil {
		return Catalog{}, fmt.Errorf("family catalog: %w", err)
	}
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

// rejectDuplicateKeys walks the token stream and rejects a repeated member name
// in any object at any depth, which encoding/json would silently resolve to the
// last value.
func rejectDuplicateKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]struct{}{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				key, _ := kt.(string)
				if _, dup := keys[key]; dup {
					return fmt.Errorf("duplicate member %q", key)
				}
				keys[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		}
		_, err = dec.Token()
		return err
	}
	return walk()
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
	declared := map[semreg.Qualification]struct{}{}
	for _, s := range c.States {
		if _, ok := allowed[s]; !ok {
			return fmt.Errorf("family catalog: state %q is not in the semantic contract", s)
		}
		if _, dup := declared[s]; dup {
			return fmt.Errorf("family catalog: state %q declared twice", s)
		}
		declared[s] = struct{}{}
	}
	if len(declared) != len(allowed) {
		return errors.New("family catalog: declared states differ from the semantic contract")
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
	gateway := false
	for _, e := range f.Evidence {
		if e.Owner == "" || e.Kind == "" || e.Locator == "" || e.Statement == "" {
			return errors.New("every evidence reference needs owner, kind, locator and statement")
		}
		if e.Kind == EvidenceKindGatewayComposition {
			if !strings.HasPrefix(e.Owner, gatewayOwnerPrefix) || len(e.Owner) != len(gatewayOwnerPrefix)+40 {
				return errors.New("gateway composition evidence must pin a full gateway commit")
			}
			gateway = true
		}
	}
	if f.Composition != CompositionNotComposed && !(f.CompositionVerified && gateway) {
		return errors.New("claimed composition needs composition_verified and gateway composition evidence")
	}
	if f.Composition == CompositionNotComposed && (f.CompositionVerified || gateway) {
		return errors.New("not_composed rows stay unverified and carry no gateway composition evidence")
	}
	if f.CompositionVerified && !gateway {
		return errors.New("composition_verified needs gateway composition evidence")
	}
	return nil
}
