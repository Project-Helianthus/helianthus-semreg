// Package catalog defines the protocol-neutral family record and the family
// catalog document: one record per device or protocol family with the
// qualification state its published evidence supports, the composition
// evidence that names the registration of the driver that declares it, and the
// recorded hardware-test level. A driver declares its own families through the
// driver contract; SemReg stores no vendor family row and no gateway
// composition record. A record never creates a native mapping, route,
// authority or live operation; it only records state and where the evidence
// lives, and it carries no write-authority field and no default-deny rule. The
// catalog imports no transport, protocol, vendor, gateway or driver code.
//
// # Contract family-catalog/v1
//
// The document is a JSON object with members contract, semantic_docs_pin,
// states and families. Each family has family_id, name, native_owner,
// semantic_pack, state, composition, reason, evidence (owner, kind, locator,
// statement), composition_verified and verification, and the optional members
// module and version. native_owner and semantic_pack may be empty when no
// owner or pack exists. The optional member further_semantic_packs names the
// family's other accepted packs; it is omitted when empty, needs a non-empty
// semantic_pack, and holds unique exact PackRef strings, with no count bound,
// in ascending byte order after semantic_pack, so Packs lists every pack of the
// row in canonical order. state is exactly one of the semantic Qualification
// values (candidate, qualified, unsupported, unknown, rejected) and the
// declared states member must equal that set. A qualified row is declared by
// the specification or public documentation its native owner pins and bound at
// runtime: it has the reason declared_by_specification and a specification
// evidence entry. verification is tested_on_hardware or not_tested_on_hardware;
// it is a recorded level beside state and never changes it, and
// tested_on_hardware needs a hardware_test_record evidence entry. composition
// is composed, composed_legacy, provider_local or not_composed. module and
// version are the evidence of the registration of the driver that declares the
// family; each is omitted when empty. A claimed composition requires
// composition_verified=true and both module and version; a not_composed row has
// composition_verified=false and carries neither. reason is one of the Reason
// constants below.
//
// A document must hold every family in RequiredFamilyIDs; a missing required
// family and an unknown member of a Family object are rejected, and a valid
// additional family that a driver declares is accepted. There is no fixed
// family count.
//
// Compatibility rule: the members verification, module and version, the
// reasons declared_by_specification and specification_conflict and the
// evidence kinds specification and hardware_test_record are in place in v1
// before the 0.7 release, as are the removal of the gateway_composition
// evidence kind and the evidence-grade reasons and of the further_semantic_packs
// count bound; no alias and no revision bump carries the earlier form. The
// document rejects duplicate and unknown members at every object level. The
// contract page is api/v1/family-catalog-v1.md in
// Project-Helianthus/helianthus-docs-semantic; the page is authoritative.
package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
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
	// composition through SemReg is claimed.
	CompositionProviderLocal Composition = "provider_local"
	// CompositionNotComposed means no composition is claimed.
	CompositionNotComposed Composition = "not_composed"
)

// Reason states the basis of a row's state. A qualified row is declared by the
// specification or public documentation its native owner pins and bound at
// runtime; a hardware test is recorded beside it as Verification and never
// changes it.
type Reason string

// Reason values. The set is closed: Validate rejects any other value, and a
// value keeps its meaning for the whole of v1. None of them grants live, write
// or support authority.
const (
	// ReasonDeclaredBySpecification means the pinned native specification or
	// public documentation declares the family's mapping and the declaration is
	// bound at runtime. A qualified row has this reason and a specification
	// evidence entry.
	ReasonDeclaredBySpecification Reason = "declared_by_specification"
	// ReasonSpecificationConflict means two public sources state conflicting
	// facts; both are recorded as evidence.
	ReasonSpecificationConflict Reason = "specification_conflict"
	// ReasonSemanticMappingUnknown means native receive-side evidence exists but
	// no semantic mapping of the family is established; native state stays
	// opaque.
	ReasonSemanticMappingUnknown Reason = "semantic_mapping_unknown"
	// ReasonUnknownPendingNormativeSource means the family stays unknown
	// while the normative source the accepted packs record under this same
	// coded value is unresolved; no normative mapping or conformance claim
	// exists. The dependency itself is defined by the packs, not by the
	// catalog.
	ReasonUnknownPendingNormativeSource Reason = "unknown_pending_normative_source"
	// ReasonNoProducer means no producer, topology instance or mapping is
	// established for the family.
	ReasonNoProducer Reason = "no_producer"
)

// Evidence kinds a validation rule names. The kind vocabulary is otherwise
// open and additive.
const (
	// EvidenceKindSpecification marks evidence citing the pinned native
	// specification or public documentation that declares the family's mapping.
	EvidenceKindSpecification = "specification"
	// EvidenceKindHardwareTestRecord marks evidence citing a public hardware
	// test record in a project repository.
	EvidenceKindHardwareTestRecord = "hardware_test_record"
)

// Verification is the recorded hardware-test level of a family. It is recorded
// beside State and never raises or lowers qualification, promotion or an
// operation.
type Verification string

// Verification values.
const (
	// VerificationTestedOnHardware means a public hardware test record exists;
	// the row carries a hardware_test_record evidence entry.
	VerificationTestedOnHardware Verification = "tested_on_hardware"
	// VerificationNotTestedOnHardware means no hardware test is recorded. It
	// holds no row below qualified.
	VerificationNotTestedOnHardware Verification = "not_tested_on_hardware"
)

var reasons = map[Reason]struct{}{
	ReasonDeclaredBySpecification: {}, ReasonSpecificationConflict: {}, ReasonSemanticMappingUnknown: {},
	ReasonUnknownPendingNormativeSource: {}, ReasonNoProducer: {},
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

// Family is one family record, declared by a driver or held in a catalog
// document. Its fields are declared in the member order of the fixture, so a
// decoded record re-encodes to the same bytes.
type Family struct {
	FamilyID     string `json:"family_id"`
	Name         string `json:"name"`
	NativeOwner  string `json:"native_owner"`
	SemanticPack string `json:"semantic_pack"`
	// FurtherSemanticPacks names the other accepted packs the family maps to.
	// It is omitted when empty; when present it holds one or more exact
	// PackRef strings in ascending byte order, each sorting after
	// SemanticPack, with no count bound. No pack of a row is primary.
	FurtherSemanticPacks []string             `json:"further_semantic_packs,omitempty"`
	State                semreg.Qualification `json:"state"`
	Composition          Composition          `json:"composition"`
	Reason               Reason               `json:"reason"`
	Evidence             []Evidence           `json:"evidence"`
	// CompositionVerified is true only for a claimed composition whose Module
	// and Version name the registration of the declaring driver.
	CompositionVerified bool `json:"composition_verified"`
	// Verification is the recorded hardware-test level; it never changes State.
	Verification Verification `json:"verification"`
	// Module and Version are the composition evidence of the registration of
	// the driver that declares the family. Each is omitted when empty.
	Module  string `json:"module,omitempty"`
	Version string `json:"version,omitempty"`
}

// Packs returns the row's packs in canonical order: SemanticPack followed by
// FurtherSemanticPacks. It is empty when the row names no pack. The returned
// slice is a copy.
func (f Family) Packs() []string {
	if f.SemanticPack == "" {
		return nil
	}
	return append([]string{f.SemanticPack}, f.FurtherSemanticPacks...)
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

// RequiredFamilyIDs lists the families a catalog document must hold: the one
// family that names no vendor or protocol. Every other family is declared by
// its driver.
func RequiredFamilyIDs() []string {
	return []string{"infrastructure"}
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
	if err := requireCanonicalMembers(raw, c); err != nil {
		return Catalog{}, err
	}
	if err := c.Validate(); err != nil {
		return Catalog{}, err
	}
	return c, nil
}

// requireCanonicalMembers rejects any member name that is not the exact
// canonical spelling (encoding/json would otherwise bind a case-variant alias)
// and any omitted member, by requiring the decoded value to re-encode to the
// same document.
func requireCanonicalMembers(raw []byte, c Catalog) error {
	canonical, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("family catalog: %w", err)
	}
	var got, want any
	if err := json.Unmarshal(raw, &got); err != nil {
		return fmt.Errorf("family catalog: %w", err)
	}
	if err := json.Unmarshal(canonical, &want); err != nil {
		return fmt.Errorf("family catalog: %w", err)
	}
	if !reflect.DeepEqual(got, want) {
		return errors.New("family catalog: non-canonical or missing member name")
	}
	return nil
}

func isCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
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
				// encoding/json matches member names case-insensitively, so
				// case variants are duplicates of each other.
				key = strings.ToLower(key)
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
	if !strings.HasPrefix(c.SemanticDocsPin, pinPrefix) || !isCommitSHA(strings.TrimPrefix(c.SemanticDocsPin, pinPrefix)) {
		return errors.New("family catalog: semantic docs pin must name a full commit")
	}
	allowed := allowedStates()
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
		if err := f.Validate(); err != nil {
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
	return nil
}

func allowedStates() map[semreg.Qualification]struct{} {
	allowed := map[semreg.Qualification]struct{}{}
	for _, s := range AllowedStates() {
		allowed[s] = struct{}{}
	}
	return allowed
}

// Validate checks one family record as a driver declares it: identity, an
// allowed state, composition, reason and verification, packs, evidence, the
// qualified and tested_on_hardware evidence rules and the composition evidence.
// It never invents or upgrades a state.
func (f Family) Validate() error {
	if f.FamilyID == "" || f.Name == "" {
		return errors.New("identity is required")
	}
	if _, ok := allowedStates()[f.State]; !ok {
		return fmt.Errorf("state %q is not allowed", f.State)
	}
	if _, ok := compositions[f.Composition]; !ok {
		return fmt.Errorf("composition %q is not allowed", f.Composition)
	}
	if _, ok := reasons[f.Reason]; !ok {
		return fmt.Errorf("reason %q is not allowed", f.Reason)
	}
	if f.Verification != VerificationTestedOnHardware && f.Verification != VerificationNotTestedOnHardware {
		return fmt.Errorf("verification %q is not allowed", f.Verification)
	}
	if err := f.validatePacks(); err != nil {
		return err
	}
	if len(f.Evidence) == 0 {
		return errors.New("evidence reference is required")
	}
	specification, hardware := false, false
	for _, e := range f.Evidence {
		if e.Owner == "" || e.Kind == "" || e.Locator == "" || e.Statement == "" {
			return errors.New("every evidence reference needs owner, kind, locator and statement")
		}
		switch e.Kind {
		case EvidenceKindSpecification:
			specification = true
		case EvidenceKindHardwareTestRecord:
			hardware = true
		}
	}
	if f.Verification == VerificationTestedOnHardware && !hardware {
		return errors.New("tested_on_hardware needs a hardware_test_record evidence entry")
	}
	if f.State == semreg.QualificationQualified && (f.Reason != ReasonDeclaredBySpecification || !specification) {
		return errors.New("a qualified row needs the reason declared_by_specification and a specification evidence entry")
	}
	if f.Composition == CompositionNotComposed {
		if f.CompositionVerified || f.Module != "" || f.Version != "" {
			return errors.New("not_composed rows have composition_verified=false and carry neither module nor version")
		}
		return nil
	}
	if !f.CompositionVerified || f.Module == "" || f.Version == "" {
		return errors.New("claimed composition needs composition_verified and both module and version")
	}
	return nil
}

// validatePacks checks further_semantic_packs: absent, or one or more exact
// PackRef strings after a non-empty semantic_pack, unique, none equal to
// semantic_pack and in ascending byte order. A row in any other order is
// rejected, never reordered.
func (f Family) validatePacks() error {
	if f.FurtherSemanticPacks == nil {
		return nil
	}
	if len(f.FurtherSemanticPacks) == 0 {
		return errors.New("further_semantic_packs is present but empty")
	}
	if f.SemanticPack == "" {
		return errors.New("further_semantic_packs needs a non-empty semantic_pack, got an empty semantic_pack")
	}
	seen := map[string]struct{}{}
	prev := f.SemanticPack
	for _, p := range f.FurtherSemanticPacks {
		if err := validPackRef(p); err != nil {
			return fmt.Errorf("further_semantic_packs entry %q: %w", p, err)
		}
		if p == f.SemanticPack {
			return fmt.Errorf("further_semantic_packs entry %q equals semantic_pack", p)
		}
		if _, dup := seen[p]; dup {
			return fmt.Errorf("further_semantic_packs repeats %q", p)
		}
		seen[p] = struct{}{}
		if p < prev {
			return fmt.Errorf("further_semantic_packs entry %q is not in canonical order after %q", p, prev)
		}
		prev = p
	}
	return nil
}

// validPackRef accepts the exact <id>@<version> form of a PackRef.
func validPackRef(s string) error {
	id, version, ok := strings.Cut(s, "@")
	if !ok {
		return errors.New("not an exact PackRef")
	}
	if err := (semreg.PackRef{ID: semreg.DefinitionID(id), Version: semreg.SemanticVersion(version)}).Validate(); err != nil {
		return fmt.Errorf("not an exact PackRef: %w", err)
	}
	return nil
}
