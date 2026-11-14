package graph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Contract identifies the element catalog document.
const Contract = "helianthus.semantic.element-catalog/v1"

// KernelContract is the kernel contract that owns every core entry.
const KernelContract semreg.ContractVersion = "helianthus.semantic.kernel/v1"

// CatalogOrder is the fixed statement of the canonical entry order (CAT-8).
const CatalogOrder = "owner kind, pack id, pack version, element id, element version as canonical UTF-8 byte sequence"

// PinnedDigest is the SHA-256 of fixtures/v1/element-catalog-v1.json, the
// byte copy of the accepted catalog. Load accepts no other bytes.
const PinnedDigest semreg.Digest = "sha256:14c36bfcf1fafd22149e05f3ad4b11ecc4b8ba54723c1dc16a78f571c2713d17"

// Status is the document status.
type Status string

// Status values.
const (
	StatusDraft    Status = "draft"
	StatusAccepted Status = "accepted"
)

// PlaneID names one of the six planes.
type PlaneID string

// PlaneID values.
const (
	PlanePhysical   PlaneID = "physical"
	PlaneEnergy     PlaneID = "energy"
	PlaneHydraulic  PlaneID = "hydraulic"
	PlaneControl    PlaneID = "control"
	PlaneDiagnostic PlaneID = "diagnostic"
	PlaneProvenance PlaneID = "provenance"
)

// CoreType names a core relation type.
type CoreType string

// CoreType values.
const (
	CoreContains CoreType = "contains"
	CoreGroups   CoreType = "groups"
	CoreSupplies CoreType = "supplies"
	CoreMeasures CoreType = "measures"
	CoreControls CoreType = "controls"
	CoreHosts    CoreType = "hosts"
	CoreRelays   CoreType = "relays"
)

// Structure is the structural constraint of a core relation type.
type Structure string

// Structure values.
const (
	StructureForest  Structure = "forest"
	StructureAcyclic Structure = "acyclic"
	StructureGraph   Structure = "graph"
)

// ElementKind is the kind of a catalog entry.
type ElementKind string

// ElementKind values.
const (
	KindResourceKind   ElementKind = "resource_kind"
	KindRelation       ElementKind = "relation"
	KindDimension      ElementKind = "dimension"
	KindService        ElementKind = "service"
	KindFact           ElementKind = "fact"
	KindCapability     ElementKind = "capability"
	KindOperation      ElementKind = "operation"
	KindEffectRule     ElementKind = "effect_rule"
	KindEvidenceState  ElementKind = "evidence_state"
	KindLifecycleState ElementKind = "lifecycle_state"
	KindPresentation   ElementKind = "presentation"
)

// Disposition is the catalog disposition of an entry. It is distinct from
// every projection outcome.
type Disposition string

// Disposition values.
const (
	DispositionMember      Disposition = "member"
	DispositionUnsupported Disposition = "unsupported"
	DispositionUnknown     Disposition = "unknown"
)

// OwnerKind is the kind of owner of an entry.
type OwnerKind string

// OwnerKind values.
const (
	OwnerPack   OwnerKind = "pack"
	OwnerCore   OwnerKind = "core"
	OwnerNative OwnerKind = "native"
)

// AdmissionArm is the arm of the admission rule an admission record uses.
type AdmissionArm string

// AdmissionArm values.
const (
	ArmAcceptedPack   AdmissionArm = "accepted_pack"
	ArmAcceptedKernel AdmissionArm = "accepted_kernel"
	ArmOwnerDecision  AdmissionArm = "owner_decision"
	ArmMatterBaseline AdmissionArm = "matter_baseline"
	ArmEEBus          AdmissionArm = "eebus"
	ArmTwoVendors     AdmissionArm = "two_vendors"
	ArmPublicRegistry AdmissionArm = "public_registry"
)

// Orientation tells whether a relation definition reads its core type forwards
// or backwards.
type Orientation string

// Orientation values.
const (
	OrientationAsCore  Orientation = "as_core"
	OrientationInverse Orientation = "inverse"
)

// Cardinality is the cardinality of a relation definition.
type Cardinality string

// Cardinality values.
const (
	CardinalityOneToOne   Cardinality = "one_to_one"
	CardinalityOneToMany  Cardinality = "one_to_many"
	CardinalityManyToMany Cardinality = "many_to_many"
)

// Medium is the medium a supplies relation definition carries.
type Medium string

// Medium values.
const (
	MediumElectrical        Medium = "electrical"
	MediumHeatTransferFluid Medium = "heat_transfer_fluid"
	MediumRefrigerant       Medium = "refrigerant"
	MediumAir               Medium = "air"
)

// Aggregation tells whether a fact is about one member or an aggregate.
type Aggregation string

// Aggregation values.
const (
	AggregationMember        Aggregation = "member"
	AggregationAggregate     Aggregation = "aggregate"
	AggregationNotApplicable Aggregation = "not_applicable"
)

// Direction is the sign convention of a quantity fact.
type Direction string

// Direction values.
const (
	DirectionNotApplicable Direction = "not_applicable"
	DirectionNonNegative   Direction = "non_negative"
	DirectionSigned        Direction = "signed"
)

// SubjectStatus tells whether the subject of a fact is established.
type SubjectStatus string

// SubjectStatus values.
const (
	SubjectResolved   SubjectStatus = "resolved"
	SubjectUnresolved SubjectStatus = "unresolved"
)

// DimensionRole tells whether a dimension names a resource or qualifies a fact.
type DimensionRole string

// DimensionRole values.
const (
	RoleResource  DimensionRole = "resource"
	RoleQualifier DimensionRole = "qualifier"
)

// PresentationTarget is the target vocabulary a presentation entry belongs to.
type PresentationTarget string

// PresentationTarget values.
const (
	TargetPortal               PresentationTarget = "portal"
	TargetProjectionVocabulary PresentationTarget = "projection_vocabulary"
)

// SignNativeConvention is the sign reference of a fact that keeps the native
// sign convention of its seed pack.
const SignNativeConvention = "native_convention"

// IssuerNone is the issuer registry of a globally unique scheme.
const IssuerNone = "none"

// Owner is the owner of an entry. Pack is set for a pack owner, Contract for
// the core owner and Repository for a native documentation owner.
type Owner struct {
	Kind       OwnerKind
	Pack       semreg.PackRef
	Contract   semreg.ContractVersion
	Repository string
}

// ElementRef is the exact key of a catalog entry.
type ElementRef struct {
	Owner   Owner
	ID      semreg.DefinitionID
	Version semreg.SemanticVersion
}

// PackElement returns the catalog key of a pack-owned definition.
func PackElement(ref semreg.DefinitionRef) ElementRef {
	return ElementRef{Owner: Owner{Kind: OwnerPack, Pack: ref.Pack}, ID: ref.ID, Version: ref.Version}
}

// CoreElement returns the catalog key of a core-owned element.
func CoreElement(id semreg.DefinitionID, version semreg.SemanticVersion) ElementRef {
	return ElementRef{Owner: Owner{Kind: OwnerCore, Contract: KernelContract}, ID: id, Version: version}
}

// DefinitionRef returns the pack definition reference of a pack-owned key.
func (r ElementRef) DefinitionRef() (semreg.DefinitionRef, bool) {
	if r.Owner.Kind != OwnerPack {
		return semreg.DefinitionRef{}, false
	}
	return semreg.DefinitionRef{Pack: r.Owner.Pack, ID: r.ID, Version: r.Version}, true
}

// String renders the key as id@version followed by its owner.
func (r ElementRef) String() string {
	owner := string(r.Owner.Kind)
	if r.Owner.Kind == OwnerPack {
		owner = string(r.Owner.Pack.ID) + "@" + string(r.Owner.Pack.Version)
	}
	return fmt.Sprintf("%s@%s [%s]", r.ID, r.Version, owner)
}

// Admission is the admission record of a plane, core type, entry or scheme.
// ResearchReport is empty unless the arm is two_vendors.
type Admission struct {
	Arm            AdmissionArm
	Evidence       []string
	ResearchReport string
}

// Plane is one plane of the catalog.
type Plane struct {
	ID        PlaneID
	Question  string
	Admission Admission
}

// CoreRelationType is one core relation type with its plane limits.
type CoreRelationType struct {
	ID             CoreType
	Reads          string
	Structure      Structure
	AllowedPlanes  []PlaneID
	RequiredPlanes []PlaneID
	Admission      Admission
}

// IdentificationScheme is one identification scheme. IssuerRegistry is
// IssuerNone for a globally unique scheme.
type IdentificationScheme struct {
	ID             semreg.DefinitionID
	IssuerRegistry string
	TokenForm      string
	Admission      Admission
}

// Lens selects planes, root kinds and optionally relation definitions of the
// graph up to a traversal depth. It creates nothing.
type Lens struct {
	ID        semreg.DefinitionID
	Question  string
	Planes    []PlaneID
	RootKinds []semreg.DefinitionID
	Relations []ElementRef
	MaxDepth  int
}

// ResourceKindAttributes are the attributes of a resource kind.
type ResourceKindAttributes struct {
	AnchorClass semreg.AnchorClass
}

// EndpointDimensions names the pack dimensions that reach the two endpoint
// kinds of a pack-owned relation definition.
type EndpointDimensions struct {
	From ElementRef
	To   ElementRef
}

// RelationAttributes are the attributes of a relation definition. Medium is
// set only for a supplies definition.
type RelationAttributes struct {
	CoreType           CoreType
	Orientation        Orientation
	FromKinds          []semreg.DefinitionID
	ToKinds            []semreg.DefinitionID
	Cardinality        Cardinality
	OverTime           bool
	Medium             Medium
	BasisClasses       []semreg.RelationBasis
	EndpointDimensions *EndpointDimensions
}

// DimensionAttributes are the attributes of a dimension. ResourceKind is set
// only for the resource role.
type DimensionAttributes struct {
	Role         DimensionRole
	ResourceKind semreg.DefinitionID
}

// ServiceAttributes are the attributes of a service.
type ServiceAttributes struct {
	FactKeyDimension ElementRef
	SubjectKind      semreg.DefinitionID
}

// FactSubject is the subject of a fact. A resolved subject has Dimension and
// Kind; an unresolved one may name its QualifierDimension.
type FactSubject struct {
	Status             SubjectStatus
	Dimension          *ElementRef
	Kind               semreg.DefinitionID
	QualifierDimension *ElementRef
}

// SignReference is the sign reference of a signed fact: a relation definition
// or the native convention of a seed entry.
type SignReference struct {
	NativeConvention bool
	Relation         *ElementRef
}

// FactAttributes are the attributes of a fact. SignReference is set exactly
// when Direction is signed.
type FactAttributes struct {
	ValueKind     semreg.ValueKind
	Subject       FactSubject
	Aggregation   Aggregation
	Direction     Direction
	SignReference *SignReference
}

// CapabilityAttributes are the attributes of a capability.
type CapabilityAttributes struct {
	Service   ElementRef
	Operation *ElementRef
}

// OperationAttributes are the attributes of an operation.
type OperationAttributes struct {
	Capability ElementRef
	Argument   ElementRef
	EffectRule ElementRef
}

// EffectRuleAttributes are the attributes of an effect rule.
type EffectRuleAttributes struct {
	Operation ElementRef
	Fact      ElementRef
}

// KernelStateAttributes are the attributes of a kernel state type.
type KernelStateAttributes struct {
	KernelType string
	Values     []string
}

// PresentationAttributes are the attributes of a presentation entry.
type PresentationAttributes struct {
	Target PresentationTarget
}

// Element is one catalog entry. Exactly one attribute pointer is set: the one
// of its ElementKind (KernelState for both kernel state kinds). Reason and
// Tracking are empty when absent.
type Element struct {
	Ref          ElementRef
	ElementKind  ElementKind
	Disposition  Disposition
	Planes       []PlaneID
	Reason       semreg.DefinitionID
	Tracking     string
	Admission    Admission
	ResourceKind *ResourceKindAttributes
	Relation     *RelationAttributes
	Dimension    *DimensionAttributes
	Service      *ServiceAttributes
	Fact         *FactAttributes
	Capability   *CapabilityAttributes
	Operation    *OperationAttributes
	EffectRule   *EffectRuleAttributes
	KernelState  *KernelStateAttributes
	Presentation *PresentationAttributes
}

// Catalog is an immutable decoded element catalog.
type Catalog struct {
	raw       []byte
	digest    semreg.Digest
	status    Status
	planes    []Plane
	coreTypes []CoreRelationType
	schemes   []IdentificationScheme
	elements  []Element
	lenses    []Lens
	byRef     map[ElementRef]int
	coreByID  map[semreg.DefinitionID][]int
}

// Load accepts exactly the pinned catalog bytes, decodes them and validates
// them against the accepted packs. Any other bytes are digest_mismatch.
func Load(raw []byte) (*Catalog, error) {
	if digestOf(raw) != PinnedDigest {
		return nil, newError(semreg.DigestMismatch, "element catalog bytes are not the pinned copy")
	}
	catalog, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	if err := Validate(catalog, AcceptedPackMetadata()); err != nil {
		return nil, err
	}
	return catalog, nil
}

// Digest returns the SHA-256 of the exact decoded bytes.
func (c *Catalog) Digest() semreg.Digest { return c.digest }

// Bytes returns a copy of the exact decoded bytes.
func (c *Catalog) Bytes() []byte { return append([]byte(nil), c.raw...) }

// Status returns the document status.
func (c *Catalog) Status() Status { return c.status }

// Planes returns the planes in document order.
func (c *Catalog) Planes() []Plane {
	out := make([]Plane, len(c.planes))
	for i, plane := range c.planes {
		out[i] = Plane{ID: plane.ID, Question: plane.Question, Admission: cloneAdmission(plane.Admission)}
	}
	return out
}

// CoreRelationTypes returns the core relation types in document order.
func (c *Catalog) CoreRelationTypes() []CoreRelationType {
	out := make([]CoreRelationType, len(c.coreTypes))
	for i, core := range c.coreTypes {
		out[i] = core
		out[i].AllowedPlanes = append([]PlaneID(nil), core.AllowedPlanes...)
		out[i].RequiredPlanes = append([]PlaneID(nil), core.RequiredPlanes...)
		out[i].Admission = cloneAdmission(core.Admission)
	}
	return out
}

// IdentificationSchemes returns the identification schemes in document order.
func (c *Catalog) IdentificationSchemes() []IdentificationScheme {
	out := make([]IdentificationScheme, len(c.schemes))
	for i, scheme := range c.schemes {
		out[i] = scheme
		out[i].Admission = cloneAdmission(scheme.Admission)
	}
	return out
}

// Lenses returns the lenses in document order.
func (c *Catalog) Lenses() []Lens {
	out := make([]Lens, len(c.lenses))
	for i, lens := range c.lenses {
		out[i] = lens
		out[i].Planes = append([]PlaneID(nil), lens.Planes...)
		out[i].RootKinds = append([]semreg.DefinitionID(nil), lens.RootKinds...)
		out[i].Relations = append([]ElementRef(nil), lens.Relations...)
	}
	return out
}

// Elements returns every entry in document order.
func (c *Catalog) Elements() []Element {
	out := make([]Element, len(c.elements))
	for i := range c.elements {
		out[i] = cloneElement(c.elements[i])
	}
	return out
}

// Entry returns the entry with the exact key. When a key is repeated (which
// CAT-1 rejects) the first entry is returned.
func (c *Catalog) Entry(ref ElementRef) (Element, bool) {
	at, ok := c.byRef[ref]
	if !ok {
		return Element{}, false
	}
	return cloneElement(c.elements[at]), true
}

// PackEntry returns the entry of an exact pack definition reference.
func (c *Catalog) PackEntry(ref semreg.DefinitionRef) (Element, bool) {
	return c.Entry(PackElement(ref))
}

// CoreEntry returns the core entry with the identifier when exactly one core
// entry has it.
func (c *Catalog) CoreEntry(id semreg.DefinitionID) (Element, bool) {
	at := c.coreByID[id]
	if len(at) != 1 {
		return Element{}, false
	}
	return cloneElement(c.elements[at[0]]), true
}

// PlanesOf returns the planes of an entry.
func (c *Catalog) PlanesOf(ref ElementRef) ([]PlaneID, bool) {
	at, ok := c.byRef[ref]
	if !ok {
		return nil, false
	}
	return append([]PlaneID(nil), c.elements[at].Planes...), true
}

// DispositionOf returns the disposition of an entry.
func (c *Catalog) DispositionOf(ref ElementRef) (Disposition, bool) {
	at, ok := c.byRef[ref]
	if !ok {
		return "", false
	}
	return c.elements[at].Disposition, true
}

// DimensionKind returns the resource kind of a resource dimension. A qualifier
// dimension has no kind.
func (c *Catalog) DimensionKind(ref semreg.DefinitionRef) (semreg.DefinitionID, bool) {
	at, ok := c.byRef[PackElement(ref)]
	if !ok || c.elements[at].Dimension == nil || c.elements[at].Dimension.Role != RoleResource {
		return "", false
	}
	return c.elements[at].Dimension.ResourceKind, true
}

// ServiceSubjectKind returns the subject kind of a service.
func (c *Catalog) ServiceSubjectKind(ref semreg.DefinitionRef) (semreg.DefinitionID, bool) {
	at, ok := c.byRef[PackElement(ref)]
	if !ok || c.elements[at].Service == nil {
		return "", false
	}
	return c.elements[at].Service.SubjectKind, true
}

// Relation returns the core type, orientation, endpoint kinds, cardinality,
// basis classes and medium of a relation definition.
func (c *Catalog) Relation(ref ElementRef) (RelationAttributes, bool) {
	at, ok := c.byRef[ref]
	if !ok || c.elements[at].Relation == nil {
		return RelationAttributes{}, false
	}
	return *cloneRelation(c.elements[at].Relation), true
}

// RelationEndpointKinds returns the kinds admitted at the from and to ends of
// a relation definition.
func (c *Catalog) RelationEndpointKinds(ref ElementRef) (from, to []semreg.DefinitionID, ok bool) {
	relation, ok := c.Relation(ref)
	if !ok {
		return nil, nil, false
	}
	return relation.FromKinds, relation.ToKinds, true
}

func digestOf(raw []byte) semreg.Digest {
	sum := sha256.Sum256(raw)
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func newError(id semreg.ErrorID, detail string) error {
	return &semreg.Error{ID: id, Detail: "element catalog: " + detail}
}

func errorf(id semreg.ErrorID, format string, args ...any) error {
	return newError(id, fmt.Sprintf(format, args...))
}

// within adds context to a stable error without hiding its identifier.
func within(context string, err error) error {
	var stable *semreg.Error
	if errors.As(err, &stable) {
		return &semreg.Error{ID: stable.ID, Detail: context + ": " + stable.Detail}
	}
	return err
}

// ------------------------------------------------------------------ decoding

type packWire struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type ownerWire struct {
	Kind       string    `json:"kind"`
	Pack       *packWire `json:"pack,omitempty"`
	Contract   *string   `json:"contract,omitempty"`
	Repository *string   `json:"repository,omitempty"`
}

type refWire struct {
	Owner   ownerWire `json:"owner"`
	ID      string    `json:"id"`
	Version string    `json:"version"`
}

type admissionWire struct {
	Arm            string   `json:"arm"`
	Evidence       []string `json:"evidence"`
	ResearchReport *string  `json:"research_report,omitempty"`
}

type planeWire struct {
	ID        string        `json:"id"`
	Question  string        `json:"question"`
	Admission admissionWire `json:"admission"`
}

type coreTypeWire struct {
	ID             string        `json:"id"`
	Reads          string        `json:"reads"`
	Structure      string        `json:"structure"`
	AllowedPlanes  []string      `json:"allowed_planes"`
	RequiredPlanes []string      `json:"required_planes"`
	Admission      admissionWire `json:"admission"`
}

type schemeWire struct {
	ID             string        `json:"id"`
	IssuerRegistry string        `json:"issuer_registry"`
	TokenForm      string        `json:"token_form"`
	Admission      admissionWire `json:"admission"`
}

type lensWire struct {
	ID        string     `json:"id"`
	Question  string     `json:"question"`
	Planes    []string   `json:"planes"`
	RootKinds []string   `json:"root_kinds"`
	Relations *[]refWire `json:"relations,omitempty"`
	MaxDepth  int        `json:"max_depth"`
}

type elementWire struct {
	Ref         refWire         `json:"ref"`
	ElementKind string          `json:"element_kind"`
	Disposition string          `json:"disposition"`
	Planes      []string        `json:"planes"`
	Reason      *string         `json:"reason,omitempty"`
	Tracking    *string         `json:"tracking,omitempty"`
	Admission   admissionWire   `json:"admission"`
	Attributes  json.RawMessage `json:"attributes"`
}

type documentWire struct {
	Contract              string         `json:"contract"`
	KernelContract        string         `json:"kernel_contract"`
	Status                string         `json:"status"`
	CatalogOrder          string         `json:"catalog_order"`
	Planes                []planeWire    `json:"planes"`
	CoreRelationTypes     []coreTypeWire `json:"core_relation_types"`
	IdentificationSchemes []schemeWire   `json:"identification_schemes"`
	Elements              []elementWire  `json:"elements"`
	Lenses                []lensWire     `json:"lenses"`
}

type resourceKindWire struct {
	AnchorClass string `json:"anchor_class"`
}

type endpointsWire struct {
	From refWire `json:"from"`
	To   refWire `json:"to"`
}

type relationWire struct {
	CoreType           string         `json:"core_type"`
	Orientation        string         `json:"orientation"`
	FromKinds          []string       `json:"from_kinds"`
	ToKinds            []string       `json:"to_kinds"`
	Cardinality        string         `json:"cardinality"`
	OverTime           bool           `json:"over_time"`
	Medium             *string        `json:"medium,omitempty"`
	BasisClasses       []string       `json:"basis_classes"`
	EndpointDimensions *endpointsWire `json:"endpoint_dimensions,omitempty"`
}

type dimensionWire struct {
	Role         string  `json:"role"`
	ResourceKind *string `json:"resource_kind,omitempty"`
}

type serviceWire struct {
	FactKeyDimension refWire `json:"fact_key_dimension"`
	SubjectKind      string  `json:"subject_kind"`
}

type subjectWire struct {
	Status             string   `json:"status"`
	Dimension          *refWire `json:"dimension,omitempty"`
	Kind               *string  `json:"kind,omitempty"`
	QualifierDimension *refWire `json:"qualifier_dimension,omitempty"`
}

type factWire struct {
	ValueKind     string          `json:"value_kind"`
	Subject       subjectWire     `json:"subject"`
	Aggregation   string          `json:"aggregation"`
	Direction     string          `json:"direction"`
	SignReference json.RawMessage `json:"sign_reference,omitempty"`
}

type capabilityWire struct {
	Service   refWire  `json:"service"`
	Operation *refWire `json:"operation,omitempty"`
}

type operationWire struct {
	Capability refWire `json:"capability"`
	Argument   refWire `json:"argument"`
	EffectRule refWire `json:"effect_rule"`
}

type effectRuleWire struct {
	Operation refWire `json:"operation"`
	Fact      refWire `json:"fact"`
}

type kernelStateWire struct {
	KernelType string   `json:"kernel_type"`
	Values     []string `json:"values"`
}

type presentationWire struct {
	Target string `json:"target"`
}

var (
	definitionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	semverPattern       = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	issueURLPattern     = regexp.MustCompile(`^https://github\.com/Project-Helianthus/[a-z0-9.-]+/issues/[1-9][0-9]*$`)
	githubURLPattern    = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/(issues/[1-9][0-9]*|blob/[0-9a-f]{40}/[^\s]+)$`)
	otherURLPattern     = regexp.MustCompile(`^https://[^\s/]+\.[^\s/]+/[^\s]*$`)
	nativeRepoPattern   = regexp.MustCompile(`^https://github\.com/Project-Helianthus/helianthus-docs-[a-z0-9-]+$`)
	kernelTypePattern   = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	kernelValuePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

var (
	planeIDs      = set(PlanePhysical, PlaneEnergy, PlaneHydraulic, PlaneControl, PlaneDiagnostic, PlaneProvenance)
	coreTypeIDs   = set(CoreContains, CoreGroups, CoreSupplies, CoreMeasures, CoreControls, CoreHosts, CoreRelays)
	structures    = set(StructureForest, StructureAcyclic, StructureGraph)
	elementKinds  = set(KindResourceKind, KindRelation, KindDimension, KindService, KindFact, KindCapability, KindOperation, KindEffectRule, KindEvidenceState, KindLifecycleState, KindPresentation)
	dispositions  = set(DispositionMember, DispositionUnsupported, DispositionUnknown)
	arms          = set(ArmAcceptedPack, ArmAcceptedKernel, ArmOwnerDecision, ArmMatterBaseline, ArmEEBus, ArmTwoVendors, ArmPublicRegistry)
	orientations  = set(OrientationAsCore, OrientationInverse)
	cardinalities = set(CardinalityOneToOne, CardinalityOneToMany, CardinalityManyToMany)
	media         = set(MediumElectrical, MediumHeatTransferFluid, MediumRefrigerant, MediumAir)
	basisClasses  = set(semreg.RelationNativeObservation, semreg.RelationConfiguration, semreg.RelationGateRule)
	anchorClasses = set(semreg.AnchorEquipment, semreg.AnchorComponent, semreg.AnchorFunction, semreg.AnchorEpisode)
	roles         = set(RoleResource, RoleQualifier)
	valueKinds    = set(semreg.ValueQuantity, semreg.ValueBoolean, semreg.ValueText, semreg.ValueSymbol, semreg.ValueSymbols, semreg.ValueTime)
	aggregations  = set(AggregationMember, AggregationAggregate, AggregationNotApplicable)
	directions    = set(DirectionNotApplicable, DirectionNonNegative, DirectionSigned)
	targets       = set(TargetPortal, TargetProjectionVocabulary)
	statuses      = set(StatusDraft, StatusAccepted)
)

const (
	maxTextRunes   = 240
	maxPlaneCount  = 6
	minPlaneCount  = 6
	coreTypeCount  = 7
	maxLensDepth   = 32
	maxBasisCount  = 3
	maxStateValues = 32
)

func set[T ~string](values ...T) map[T]struct{} {
	out := make(map[T]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func inSet[T ~string](values map[T]struct{}, value string) bool {
	_, ok := values[T(value)]
	return ok
}

// Decode strictly decodes an element catalog document and checks every value
// against the document vocabulary. It does not apply CAT-1 to CAT-9.
func Decode(raw []byte) (*Catalog, error) {
	if !utf8.Valid(raw) {
		return nil, newError(semreg.InvalidJSON, "invalid UTF-8")
	}
	if err := rejectDuplicateMembers(raw); err != nil {
		return nil, err
	}
	var doc documentWire
	if err := strictDecode(raw, &doc, "document"); err != nil {
		return nil, err
	}
	if doc.Contract != Contract || doc.KernelContract != string(KernelContract) {
		return nil, errorf(semreg.InvalidContract, "contract %q with kernel contract %q", doc.Contract, doc.KernelContract)
	}
	if !inSet(statuses, doc.Status) {
		return nil, errorf(semreg.InvalidEnum, "status %q", doc.Status)
	}
	if doc.CatalogOrder != CatalogOrder {
		return nil, newError(semreg.InvalidValue, "catalog_order is not the canonical order statement")
	}
	c := &Catalog{
		raw: append([]byte(nil), raw...), digest: digestOf(raw), status: Status(doc.Status),
		byRef: map[ElementRef]int{}, coreByID: map[semreg.DefinitionID][]int{},
	}
	var err error
	if c.planes, err = decodePlanes(doc.Planes); err != nil {
		return nil, err
	}
	if c.coreTypes, err = decodeCoreTypes(doc.CoreRelationTypes); err != nil {
		return nil, err
	}
	if c.schemes, err = decodeSchemes(doc.IdentificationSchemes); err != nil {
		return nil, err
	}
	if c.lenses, err = decodeLenses(doc.Lenses); err != nil {
		return nil, err
	}
	if len(doc.Elements) == 0 {
		return nil, newError(semreg.InvalidValue, "elements is empty")
	}
	c.elements = make([]Element, len(doc.Elements))
	for i, wire := range doc.Elements {
		element, err := decodeElement(wire)
		if err != nil {
			return nil, within(fmt.Sprintf("element %d", i), err)
		}
		c.elements[i] = element
		if _, seen := c.byRef[element.Ref]; !seen {
			c.byRef[element.Ref] = i
		}
		if element.Ref.Owner.Kind == OwnerCore {
			c.coreByID[element.Ref.ID] = append(c.coreByID[element.Ref.ID], i)
		}
	}
	return c, nil
}

func decodePlanes(wires []planeWire) ([]Plane, error) {
	if len(wires) < minPlaneCount {
		return nil, errorf(semreg.InvalidValue, "%d planes, at least %d required", len(wires), minPlaneCount)
	}
	out := make([]Plane, len(wires))
	for i, wire := range wires {
		if !inSet(planeIDs, wire.ID) {
			return nil, errorf(semreg.InvalidEnum, "plane %q", wire.ID)
		}
		if err := checkText("plane question", wire.Question); err != nil {
			return nil, err
		}
		admission, err := decodeAdmission(wire.Admission)
		if err != nil {
			return nil, err
		}
		out[i] = Plane{ID: PlaneID(wire.ID), Question: wire.Question, Admission: admission}
	}
	return out, nil
}

func decodeCoreTypes(wires []coreTypeWire) ([]CoreRelationType, error) {
	if len(wires) != coreTypeCount {
		return nil, errorf(semreg.InvalidValue, "%d core relation types, exactly %d required", len(wires), coreTypeCount)
	}
	out := make([]CoreRelationType, len(wires))
	for i, wire := range wires {
		if !inSet(coreTypeIDs, wire.ID) {
			return nil, errorf(semreg.InvalidEnum, "core relation type %q", wire.ID)
		}
		if !inSet(structures, wire.Structure) {
			return nil, errorf(semreg.InvalidEnum, "structure %q", wire.Structure)
		}
		if err := checkText("core relation type reading", wire.Reads); err != nil {
			return nil, err
		}
		allowed, err := decodePlaneList(wire.AllowedPlanes, 1)
		if err != nil {
			return nil, err
		}
		required, err := decodePlaneList(wire.RequiredPlanes, 0)
		if err != nil {
			return nil, err
		}
		admission, err := decodeAdmission(wire.Admission)
		if err != nil {
			return nil, err
		}
		out[i] = CoreRelationType{
			ID: CoreType(wire.ID), Reads: wire.Reads, Structure: Structure(wire.Structure),
			AllowedPlanes: allowed, RequiredPlanes: required, Admission: admission,
		}
	}
	return out, nil
}

func decodeSchemes(wires []schemeWire) ([]IdentificationScheme, error) {
	out := make([]IdentificationScheme, len(wires))
	for i, wire := range wires {
		if err := checkDefinitionID(wire.ID); err != nil {
			return nil, err
		}
		if wire.IssuerRegistry != IssuerNone {
			if err := checkDefinitionID(wire.IssuerRegistry); err != nil {
				return nil, err
			}
		}
		if err := checkText("scheme token form", wire.TokenForm); err != nil {
			return nil, err
		}
		admission, err := decodeAdmission(wire.Admission)
		if err != nil {
			return nil, err
		}
		out[i] = IdentificationScheme{
			ID: semreg.DefinitionID(wire.ID), IssuerRegistry: wire.IssuerRegistry, TokenForm: wire.TokenForm, Admission: admission,
		}
	}
	return out, nil
}

func decodeLenses(wires []lensWire) ([]Lens, error) {
	out := make([]Lens, len(wires))
	for i, wire := range wires {
		if err := checkDefinitionID(wire.ID); err != nil {
			return nil, err
		}
		if err := checkText("lens question", wire.Question); err != nil {
			return nil, err
		}
		planes, err := decodePlaneList(wire.Planes, 1)
		if err != nil {
			return nil, err
		}
		roots, err := decodeKindList(wire.RootKinds)
		if err != nil {
			return nil, err
		}
		lens := Lens{ID: semreg.DefinitionID(wire.ID), Question: wire.Question, Planes: planes, RootKinds: roots, MaxDepth: wire.MaxDepth}
		if wire.MaxDepth < 1 || wire.MaxDepth > maxLensDepth {
			return nil, errorf(semreg.InvalidValue, "lens %s depth %d outside 1 through %d", wire.ID, wire.MaxDepth, maxLensDepth)
		}
		if wire.Relations != nil {
			if len(*wire.Relations) == 0 {
				return nil, errorf(semreg.InvalidValue, "lens %s names an empty relation list", wire.ID)
			}
			seen := map[ElementRef]struct{}{}
			for _, item := range *wire.Relations {
				ref, err := decodeRef(item)
				if err != nil {
					return nil, err
				}
				if _, dup := seen[ref]; dup {
					return nil, errorf(semreg.InvalidValue, "lens %s repeats relation %s", wire.ID, ref)
				}
				seen[ref] = struct{}{}
				lens.Relations = append(lens.Relations, ref)
			}
		}
		out[i] = lens
	}
	return out, nil
}

func decodeElement(wire elementWire) (Element, error) {
	ref, err := decodeRef(wire.Ref)
	if err != nil {
		return Element{}, err
	}
	if !inSet(elementKinds, wire.ElementKind) {
		return Element{}, errorf(semreg.InvalidEnum, "%s element kind %q", ref, wire.ElementKind)
	}
	if !inSet(dispositions, wire.Disposition) {
		return Element{}, errorf(semreg.InvalidEnum, "%s disposition %q", ref, wire.Disposition)
	}
	planes, err := decodePlaneList(wire.Planes, 0)
	if err != nil {
		return Element{}, within(ref.String(), err)
	}
	element := Element{Ref: ref, ElementKind: ElementKind(wire.ElementKind), Disposition: Disposition(wire.Disposition), Planes: planes}
	if wire.Reason != nil {
		if err := checkDefinitionID(*wire.Reason); err != nil {
			return Element{}, err
		}
		element.Reason = semreg.DefinitionID(*wire.Reason)
	}
	if wire.Tracking != nil {
		if !issueURLPattern.MatchString(*wire.Tracking) {
			return Element{}, errorf(semreg.InvalidValue, "%s tracking %q is not an issue", ref, *wire.Tracking)
		}
		element.Tracking = *wire.Tracking
	}
	if element.Admission, err = decodeAdmission(wire.Admission); err != nil {
		return Element{}, err
	}
	if err := decodeAttributes(&element, wire.Attributes); err != nil {
		return Element{}, within(ref.String()+" attributes", err)
	}
	return element, nil
}

func decodeAttributes(element *Element, raw json.RawMessage) error {
	switch element.ElementKind {
	case KindResourceKind:
		var wire resourceKindWire
		if err := strictDecode(raw, &wire, "resource kind attributes"); err != nil {
			return err
		}
		if !inSet(anchorClasses, wire.AnchorClass) {
			return errorf(semreg.InvalidEnum, "anchor class %q", wire.AnchorClass)
		}
		element.ResourceKind = &ResourceKindAttributes{AnchorClass: semreg.AnchorClass(wire.AnchorClass)}
	case KindRelation:
		return decodeRelation(element, raw)
	case KindDimension:
		var wire dimensionWire
		if err := strictDecode(raw, &wire, "dimension attributes"); err != nil {
			return err
		}
		if !inSet(roles, wire.Role) {
			return errorf(semreg.InvalidEnum, "dimension role %q", wire.Role)
		}
		attributes := &DimensionAttributes{Role: DimensionRole(wire.Role)}
		if (wire.ResourceKind != nil) != (attributes.Role == RoleResource) {
			return newError(semreg.InvalidValue, "a dimension names a resource kind exactly when its role is resource")
		}
		if wire.ResourceKind != nil {
			if err := checkDefinitionID(*wire.ResourceKind); err != nil {
				return err
			}
			attributes.ResourceKind = semreg.DefinitionID(*wire.ResourceKind)
		}
		element.Dimension = attributes
	case KindService:
		var wire serviceWire
		if err := strictDecode(raw, &wire, "service attributes"); err != nil {
			return err
		}
		dimension, err := decodeRef(wire.FactKeyDimension)
		if err != nil {
			return err
		}
		if err := checkDefinitionID(wire.SubjectKind); err != nil {
			return err
		}
		element.Service = &ServiceAttributes{FactKeyDimension: dimension, SubjectKind: semreg.DefinitionID(wire.SubjectKind)}
	case KindFact:
		return decodeFact(element, raw)
	case KindCapability:
		var wire capabilityWire
		if err := strictDecode(raw, &wire, "capability attributes"); err != nil {
			return err
		}
		service, err := decodeRef(wire.Service)
		if err != nil {
			return err
		}
		element.Capability = &CapabilityAttributes{Service: service}
		if wire.Operation != nil {
			operation, err := decodeRef(*wire.Operation)
			if err != nil {
				return err
			}
			element.Capability.Operation = &operation
		}
	case KindOperation:
		var wire operationWire
		if err := strictDecode(raw, &wire, "operation attributes"); err != nil {
			return err
		}
		refs, err := decodeRefs(wire.Capability, wire.Argument, wire.EffectRule)
		if err != nil {
			return err
		}
		element.Operation = &OperationAttributes{Capability: refs[0], Argument: refs[1], EffectRule: refs[2]}
	case KindEffectRule:
		var wire effectRuleWire
		if err := strictDecode(raw, &wire, "effect rule attributes"); err != nil {
			return err
		}
		refs, err := decodeRefs(wire.Operation, wire.Fact)
		if err != nil {
			return err
		}
		element.EffectRule = &EffectRuleAttributes{Operation: refs[0], Fact: refs[1]}
	case KindEvidenceState, KindLifecycleState:
		var wire kernelStateWire
		if err := strictDecode(raw, &wire, "kernel state attributes"); err != nil {
			return err
		}
		if !kernelTypePattern.MatchString(wire.KernelType) {
			return errorf(semreg.InvalidIdentifier, "kernel type %q", wire.KernelType)
		}
		if len(wire.Values) == 0 || len(wire.Values) > maxStateValues {
			return errorf(semreg.InvalidValue, "kernel type %s lists %d values", wire.KernelType, len(wire.Values))
		}
		seen := map[string]struct{}{}
		for _, value := range wire.Values {
			if !kernelValuePattern.MatchString(value) {
				return errorf(semreg.InvalidIdentifier, "kernel value %q", value)
			}
			if _, dup := seen[value]; dup {
				return errorf(semreg.InvalidValue, "kernel type %s repeats %q", wire.KernelType, value)
			}
			seen[value] = struct{}{}
		}
		element.KernelState = &KernelStateAttributes{KernelType: wire.KernelType, Values: append([]string(nil), wire.Values...)}
	case KindPresentation:
		var wire presentationWire
		if err := strictDecode(raw, &wire, "presentation attributes"); err != nil {
			return err
		}
		if !inSet(targets, wire.Target) {
			return errorf(semreg.InvalidEnum, "presentation target %q", wire.Target)
		}
		element.Presentation = &PresentationAttributes{Target: PresentationTarget(wire.Target)}
	}
	return nil
}

func decodeRelation(element *Element, raw json.RawMessage) error {
	var wire relationWire
	if err := strictDecode(raw, &wire, "relation attributes"); err != nil {
		return err
	}
	if !inSet(coreTypeIDs, wire.CoreType) {
		return errorf(semreg.InvalidEnum, "core type %q", wire.CoreType)
	}
	if !inSet(orientations, wire.Orientation) {
		return errorf(semreg.InvalidEnum, "orientation %q", wire.Orientation)
	}
	if !inSet(cardinalities, wire.Cardinality) {
		return errorf(semreg.InvalidEnum, "cardinality %q", wire.Cardinality)
	}
	from, err := decodeKindList(wire.FromKinds)
	if err != nil {
		return err
	}
	to, err := decodeKindList(wire.ToKinds)
	if err != nil {
		return err
	}
	attributes := &RelationAttributes{
		CoreType: CoreType(wire.CoreType), Orientation: Orientation(wire.Orientation), FromKinds: from, ToKinds: to,
		Cardinality: Cardinality(wire.Cardinality), OverTime: wire.OverTime,
	}
	if (wire.Medium != nil) != (attributes.CoreType == CoreSupplies) {
		return newError(semreg.InvalidValue, "a relation declares a medium exactly when its core type is supplies")
	}
	if wire.Medium != nil {
		if !inSet(media, *wire.Medium) {
			return errorf(semreg.InvalidEnum, "medium %q", *wire.Medium)
		}
		attributes.Medium = Medium(*wire.Medium)
	}
	if len(wire.BasisClasses) == 0 || len(wire.BasisClasses) > maxBasisCount {
		return errorf(semreg.InvalidValue, "%d basis classes", len(wire.BasisClasses))
	}
	for _, basis := range wire.BasisClasses {
		if !inSet(basisClasses, basis) {
			return errorf(semreg.InvalidEnum, "basis class %q", basis)
		}
		for _, prior := range attributes.BasisClasses {
			if string(prior) == basis {
				return errorf(semreg.InvalidValue, "basis class %q repeated", basis)
			}
		}
		attributes.BasisClasses = append(attributes.BasisClasses, semreg.RelationBasis(basis))
	}
	if wire.EndpointDimensions != nil {
		refs, err := decodeRefs(wire.EndpointDimensions.From, wire.EndpointDimensions.To)
		if err != nil {
			return err
		}
		attributes.EndpointDimensions = &EndpointDimensions{From: refs[0], To: refs[1]}
	}
	element.Relation = attributes
	return nil
}

func decodeFact(element *Element, raw json.RawMessage) error {
	var wire factWire
	if err := strictDecode(raw, &wire, "fact attributes"); err != nil {
		return err
	}
	if !inSet(valueKinds, wire.ValueKind) {
		return errorf(semreg.InvalidEnum, "value kind %q", wire.ValueKind)
	}
	if !inSet(aggregations, wire.Aggregation) {
		return errorf(semreg.InvalidEnum, "aggregation %q", wire.Aggregation)
	}
	if !inSet(directions, wire.Direction) {
		return errorf(semreg.InvalidEnum, "direction %q", wire.Direction)
	}
	attributes := &FactAttributes{
		ValueKind: semreg.ValueKind(wire.ValueKind), Aggregation: Aggregation(wire.Aggregation), Direction: Direction(wire.Direction),
	}
	if attributes.ValueKind != semreg.ValueQuantity &&
		(attributes.Direction != DirectionNotApplicable || attributes.Aggregation != AggregationNotApplicable) {
		return newError(semreg.InvalidValue, "a fact that is not a quantity has no direction and no aggregation")
	}
	subject, err := decodeSubject(wire.Subject)
	if err != nil {
		return err
	}
	attributes.Subject = subject
	if (len(wire.SignReference) > 0) != (attributes.Direction == DirectionSigned) {
		return newError(semreg.InvalidValue, "a fact names a sign reference exactly when its direction is signed")
	}
	if len(wire.SignReference) > 0 {
		sign, err := decodeSignReference(wire.SignReference)
		if err != nil {
			return err
		}
		attributes.SignReference = sign
	}
	element.Fact = attributes
	return nil
}

func decodeSubject(wire subjectWire) (FactSubject, error) {
	switch SubjectStatus(wire.Status) {
	case SubjectResolved:
		if wire.QualifierDimension != nil {
			return FactSubject{}, newError(semreg.UnknownMember, "a resolved subject has no qualifier_dimension")
		}
		if wire.Dimension == nil || wire.Kind == nil {
			return FactSubject{}, newError(semreg.MissingMember, "a resolved subject names its dimension and kind")
		}
		dimension, err := decodeRef(*wire.Dimension)
		if err != nil {
			return FactSubject{}, err
		}
		if err := checkDefinitionID(*wire.Kind); err != nil {
			return FactSubject{}, err
		}
		return FactSubject{Status: SubjectResolved, Dimension: &dimension, Kind: semreg.DefinitionID(*wire.Kind)}, nil
	case SubjectUnresolved:
		if wire.Dimension != nil || wire.Kind != nil {
			return FactSubject{}, newError(semreg.UnknownMember, "an unresolved subject has no dimension and no kind")
		}
		subject := FactSubject{Status: SubjectUnresolved}
		if wire.QualifierDimension != nil {
			qualifier, err := decodeRef(*wire.QualifierDimension)
			if err != nil {
				return FactSubject{}, err
			}
			subject.QualifierDimension = &qualifier
		}
		return subject, nil
	}
	return FactSubject{}, errorf(semreg.InvalidEnum, "subject status %q", wire.Status)
}

func decodeSignReference(raw json.RawMessage) (*SignReference, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, newError(semreg.InvalidValue, "sign reference")
		}
		if text != SignNativeConvention {
			return nil, errorf(semreg.InvalidValue, "sign reference %q", text)
		}
		return &SignReference{NativeConvention: true}, nil
	}
	var wire refWire
	if err := strictDecode(trimmed, &wire, "sign reference"); err != nil {
		return nil, err
	}
	ref, err := decodeRef(wire)
	if err != nil {
		return nil, err
	}
	return &SignReference{Relation: &ref}, nil
}

func decodeRefs(wires ...refWire) ([]ElementRef, error) {
	out := make([]ElementRef, len(wires))
	for i, wire := range wires {
		ref, err := decodeRef(wire)
		if err != nil {
			return nil, err
		}
		out[i] = ref
	}
	return out, nil
}

func decodeRef(wire refWire) (ElementRef, error) {
	owner, err := decodeOwner(wire.Owner)
	if err != nil {
		return ElementRef{}, err
	}
	if err := checkDefinitionID(wire.ID); err != nil {
		return ElementRef{}, err
	}
	if !semverPattern.MatchString(wire.Version) {
		return ElementRef{}, errorf(semreg.InvalidIdentifier, "version %q", wire.Version)
	}
	return ElementRef{Owner: owner, ID: semreg.DefinitionID(wire.ID), Version: semreg.SemanticVersion(wire.Version)}, nil
}

func decodeOwner(wire ownerWire) (Owner, error) {
	present := map[string]bool{"pack": wire.Pack != nil, "contract": wire.Contract != nil, "repository": wire.Repository != nil}
	var member string
	switch OwnerKind(wire.Kind) {
	case OwnerPack:
		member = "pack"
	case OwnerCore:
		member = "contract"
	case OwnerNative:
		member = "repository"
	default:
		return Owner{}, errorf(semreg.InvalidEnum, "owner kind %q", wire.Kind)
	}
	for _, name := range []string{"pack", "contract", "repository"} {
		if present[name] && name != member {
			return Owner{}, errorf(semreg.UnknownMember, "a %s owner has no %s member", wire.Kind, name)
		}
	}
	if !present[member] {
		return Owner{}, errorf(semreg.MissingMember, "a %s owner names its %s", wire.Kind, member)
	}
	switch OwnerKind(wire.Kind) {
	case OwnerPack:
		if err := checkDefinitionID(wire.Pack.ID); err != nil {
			return Owner{}, err
		}
		if !semverPattern.MatchString(wire.Pack.Version) {
			return Owner{}, errorf(semreg.InvalidIdentifier, "pack version %q", wire.Pack.Version)
		}
		return Owner{Kind: OwnerPack, Pack: semreg.PackRef{ID: semreg.DefinitionID(wire.Pack.ID), Version: semreg.SemanticVersion(wire.Pack.Version)}}, nil
	case OwnerCore:
		if *wire.Contract != string(KernelContract) {
			return Owner{}, errorf(semreg.InvalidContract, "core owner contract %q", *wire.Contract)
		}
		return Owner{Kind: OwnerCore, Contract: KernelContract}, nil
	default:
		if !nativeRepoPattern.MatchString(*wire.Repository) {
			return Owner{}, errorf(semreg.InvalidValue, "native owner repository %q", *wire.Repository)
		}
		return Owner{Kind: OwnerNative, Repository: *wire.Repository}, nil
	}
}

func decodeAdmission(wire admissionWire) (Admission, error) {
	if !inSet(arms, wire.Arm) {
		return Admission{}, errorf(semreg.InvalidEnum, "admission arm %q", wire.Arm)
	}
	admission := Admission{Arm: AdmissionArm(wire.Arm), Evidence: append([]string(nil), wire.Evidence...)}
	if wire.ResearchReport != nil {
		if *wire.ResearchReport == "" {
			return Admission{}, newError(semreg.InvalidValue, "empty research report")
		}
		admission.ResearchReport = *wire.ResearchReport
	}
	return admission, nil
}

func decodePlaneList(values []string, minimum int) ([]PlaneID, error) {
	if len(values) < minimum || len(values) > maxPlaneCount {
		return nil, errorf(semreg.InvalidValue, "plane list of %d entries", len(values))
	}
	out := make([]PlaneID, 0, len(values))
	for _, value := range values {
		if !inSet(planeIDs, value) {
			return nil, errorf(semreg.InvalidEnum, "plane %q", value)
		}
		for _, prior := range out {
			if string(prior) == value {
				return nil, errorf(semreg.InvalidValue, "plane %q repeated", value)
			}
		}
		out = append(out, PlaneID(value))
	}
	return out, nil
}

func decodeKindList(values []string) ([]semreg.DefinitionID, error) {
	if len(values) == 0 {
		return nil, newError(semreg.InvalidValue, "empty kind list")
	}
	out := make([]semreg.DefinitionID, 0, len(values))
	for _, value := range values {
		if err := checkDefinitionID(value); err != nil {
			return nil, err
		}
		for _, prior := range out {
			if string(prior) == value {
				return nil, errorf(semreg.InvalidValue, "kind %q repeated", value)
			}
		}
		out = append(out, semreg.DefinitionID(value))
	}
	return out, nil
}

func checkDefinitionID(value string) error {
	if len(value) < 3 || len(value) > 160 || !definitionIDPattern.MatchString(value) {
		return errorf(semreg.InvalidIdentifier, "identifier %q", value)
	}
	return nil
}

func checkText(what, value string) error {
	if n := utf8.RuneCountInString(value); n < 1 || n > maxTextRunes {
		return errorf(semreg.InvalidValue, "%s has %d characters", what, n)
	}
	return nil
}

// strictDecode decodes one JSON value into a wire struct, refusing unknown
// members and trailing data, then requires that the struct re-encodes to the
// same members: a member present only under another letter case is unknown,
// an omitted required member is missing, and null is never a value.
func strictDecode(raw []byte, out any, what string) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return classifyDecodeError(what, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errorf(semreg.InvalidJSON, "%s: trailing data", what)
	}
	canonical, err := json.Marshal(out)
	if err != nil {
		return errorf(semreg.InvalidValue, "%s: %v", what, err)
	}
	var got, want any
	if err := json.Unmarshal(raw, &got); err != nil {
		return classifyDecodeError(what, err)
	}
	if err := json.Unmarshal(canonical, &want); err != nil {
		return errorf(semreg.InvalidValue, "%s: %v", what, err)
	}
	return compareMembers(what, got, want)
}

func classifyDecodeError(what string, err error) error {
	var syntax *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return errorf(semreg.InvalidJSON, "%s: %v", what, err)
	case errors.As(err, &typeError):
		return errorf(semreg.InvalidValue, "%s: %v", what, err)
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return errorf(semreg.UnknownMember, "%s: %v", what, err)
	}
	var semantic *semreg.Error
	if errors.As(err, &semantic) {
		return err
	}
	return errorf(semreg.InvalidJSON, "%s: %v", what, err)
}

func compareMembers(path string, got, want any) error {
	if got == nil {
		return errorf(semreg.InvalidValue, "%s is null", path)
	}
	switch g := got.(type) {
	case map[string]any:
		w, ok := want.(map[string]any)
		if !ok {
			return errorf(semreg.InvalidValue, "%s is an object", path)
		}
		keys := make([]string, 0, len(g))
		for key := range g {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, ok := w[key]; !ok {
				return errorf(semreg.UnknownMember, "%s.%s", path, key)
			}
		}
		wanted := make([]string, 0, len(w))
		for key := range w {
			wanted = append(wanted, key)
		}
		sort.Strings(wanted)
		for _, key := range wanted {
			if _, ok := g[key]; !ok {
				return errorf(semreg.MissingMember, "%s.%s", path, key)
			}
		}
		for _, key := range keys {
			if err := compareMembers(path+"."+key, g[key], w[key]); err != nil {
				return err
			}
		}
		return nil
	case []any:
		w, ok := want.([]any)
		if !ok || len(w) != len(g) {
			return errorf(semreg.InvalidValue, "%s array", path)
		}
		for i := range g {
			if err := compareMembers(fmt.Sprintf("%s[%d]", path, i), g[i], w[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if !reflect.DeepEqual(got, want) {
		return errorf(semreg.InvalidValue, "%s value", path)
	}
	return nil
}

// rejectDuplicateMembers rejects a repeated member name, compared without
// letter case, in any object at any depth.
func rejectDuplicateMembers(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(path string) error
	walk = func(path string) error {
		token, err := decoder.Token()
		if err != nil {
			return errorf(semreg.InvalidJSON, "%s: %v", path, err)
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return errorf(semreg.InvalidJSON, "%s: %v", path, err)
				}
				name, _ := token.(string)
				folded := strings.ToLower(name)
				if _, dup := seen[folded]; dup {
					return errorf(semreg.DuplicateKey, "%s.%s", path, name)
				}
				seen[folded] = struct{}{}
				if err := walk(path + "." + name); err != nil {
					return err
				}
			}
		case '[':
			for i := 0; decoder.More(); i++ {
				if err := walk(fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
		if _, err := decoder.Token(); err != nil {
			return errorf(semreg.InvalidJSON, "%s: %v", path, err)
		}
		return nil
	}
	if err := walk("document"); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return newError(semreg.InvalidJSON, "trailing data")
	}
	return nil
}

// ------------------------------------------------------------------ copies

func cloneAdmission(in Admission) Admission {
	out := in
	out.Evidence = append([]string(nil), in.Evidence...)
	return out
}

func cloneRef(in *ElementRef) *ElementRef {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneRelation(in *RelationAttributes) *RelationAttributes {
	out := *in
	out.FromKinds = append([]semreg.DefinitionID(nil), in.FromKinds...)
	out.ToKinds = append([]semreg.DefinitionID(nil), in.ToKinds...)
	out.BasisClasses = append([]semreg.RelationBasis(nil), in.BasisClasses...)
	if in.EndpointDimensions != nil {
		ends := *in.EndpointDimensions
		out.EndpointDimensions = &ends
	}
	return &out
}

func cloneElement(in Element) Element {
	out := in
	out.Planes = append([]PlaneID(nil), in.Planes...)
	out.Admission = cloneAdmission(in.Admission)
	if in.ResourceKind != nil {
		value := *in.ResourceKind
		out.ResourceKind = &value
	}
	if in.Relation != nil {
		out.Relation = cloneRelation(in.Relation)
	}
	if in.Dimension != nil {
		value := *in.Dimension
		out.Dimension = &value
	}
	if in.Service != nil {
		value := *in.Service
		out.Service = &value
	}
	if in.Fact != nil {
		value := *in.Fact
		value.Subject.Dimension = cloneRef(in.Fact.Subject.Dimension)
		value.Subject.QualifierDimension = cloneRef(in.Fact.Subject.QualifierDimension)
		if in.Fact.SignReference != nil {
			sign := *in.Fact.SignReference
			sign.Relation = cloneRef(in.Fact.SignReference.Relation)
			value.SignReference = &sign
		}
		out.Fact = &value
	}
	if in.Capability != nil {
		value := *in.Capability
		value.Operation = cloneRef(in.Capability.Operation)
		out.Capability = &value
	}
	if in.Operation != nil {
		value := *in.Operation
		out.Operation = &value
	}
	if in.EffectRule != nil {
		value := *in.EffectRule
		out.EffectRule = &value
	}
	if in.KernelState != nil {
		value := *in.KernelState
		value.Values = append([]string(nil), in.KernelState.Values...)
		out.KernelState = &value
	}
	if in.Presentation != nil {
		value := *in.Presentation
		out.Presentation = &value
	}
	return out
}
