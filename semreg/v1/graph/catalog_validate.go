package graph

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

// Finding is one violation of a catalog rule. Rule is CAT-1 through CAT-9;
// Clause names the graph rule a CAT-4 finding enforces (PLN-5, PLN-6 or
// LNS-1) and is empty otherwise. ID is the stable semreg error.
type Finding struct {
	Rule   string
	Clause string
	ID     semreg.ErrorID
	Entry  string
	Detail string
}

// String renders the finding as "rule [clause] error entry: detail".
func (f Finding) String() string {
	rule := f.Rule
	if f.Clause != "" {
		rule += " " + f.Clause
	}
	return fmt.Sprintf("%s %s %s: %s", rule, f.ID, f.Entry, f.Detail)
}

// AcceptedPackMetadata returns fresh metadata sources of the five accepted
// packs, the inputs the committed catalog is validated against.
func AcceptedPackMetadata() []semreg.PackMetadataSource {
	return []semreg.PackMetadataSource{
		{Validator: thermal.New(), Metadata: thermal.Metadata()},
		{Validator: pv.New(), Metadata: pv.Metadata()},
		{Validator: storage.New(), Metadata: storage.Metadata()},
		{Validator: evse.New(), Metadata: evse.Metadata()},
		{Validator: infrastructure.New(), Metadata: infrastructure.Metadata()},
	}
}

// Validate applies CAT-1 to CAT-9 and returns the first finding as a stable
// semreg error, or nil when the catalog holds every rule.
func Validate(c *Catalog, metadata []semreg.PackMetadataSource) error {
	findings, err := Check(c, metadata)
	if err != nil {
		return err
	}
	if len(findings) == 0 {
		return nil
	}
	first := findings[0]
	return &semreg.Error{ID: first.ID, Detail: "element catalog: " + first.String()}
}

// Check returns every CAT-1 to CAT-9 finding in rule order. The pack inputs
// must form a valid semreg.PackMetadataRegistry; otherwise its error is
// returned and no rule is applied.
//
// The reference set of CAT-1 comes from the pack definition indexes (fields,
// services, capabilities, operations, effect rules) and the dimensions their
// metadata names. No Go pack table declares relation definitions or Portal
// contributions: for those, and for a dimension that only a relation endpoint
// names, Check requires an accepted owning pack, and the pack vector fixtures
// account for them entry by entry. The arm accepted_pack is allowed for an
// entry of an accepted pack and for a core resource kind that a resource
// dimension of an accepted pack reaches.
func Check(c *Catalog, metadata []semreg.PackMetadataSource) ([]Finding, error) {
	if c == nil {
		return nil, newError(semreg.InvalidValue, "nil catalog")
	}
	inputs, err := newPackInputs(metadata)
	if err != nil {
		return nil, err
	}
	v := &validator{c: c, in: inputs, kinds: map[semreg.DefinitionID]int{}}
	for i, element := range c.elements {
		if element.ElementKind == KindResourceKind {
			if _, seen := v.kinds[element.Ref.ID]; !seen {
				v.kinds[element.Ref.ID] = i
			}
		}
	}
	v.coreTypes = map[CoreType]CoreRelationType{}
	for _, core := range c.coreTypes {
		if _, seen := v.coreTypes[core.ID]; !seen {
			v.coreTypes[core.ID] = core
		}
	}
	v.planes = map[PlaneID]struct{}{}
	for _, plane := range c.planes {
		v.planes[plane.ID] = struct{}{}
	}
	v.packKinds = v.kindsByPack()
	for _, rule := range []func(){
		v.completeness, v.agreement, v.dispositions, v.closure, v.ownership,
		v.admission, v.signReferences, v.order, v.schemes,
	} {
		rule()
	}
	return v.findings, nil
}

// ------------------------------------------------------------- pack inputs

type packInputs struct {
	registry   *semreg.PackMetadataRegistry
	packs      map[semreg.PackRef]struct{}
	kinds      map[semreg.DefinitionRef]ElementKind
	refs       []semreg.DefinitionRef
	roles      map[semreg.DefinitionRef]DimensionRole
	fields     []semreg.FieldMetadata
	services   []semreg.ServiceMetadata
	caps       []semreg.CapabilityMetadata
	operations []semreg.OperationMetadata
}

func newPackInputs(sources []semreg.PackMetadataSource) (*packInputs, error) {
	registry, err := semreg.NewPackMetadataRegistry(sources...)
	if err != nil {
		return nil, err
	}
	in := &packInputs{
		registry: registry, packs: map[semreg.PackRef]struct{}{},
		kinds: map[semreg.DefinitionRef]ElementKind{}, roles: map[semreg.DefinitionRef]DimensionRole{},
	}
	for _, pack := range registry.Packs() {
		in.packs[pack] = struct{}{}
	}
	for _, source := range sources {
		index := source.Validator.Definitions()
		for _, group := range []struct {
			kind ElementKind
			refs []semreg.DefinitionRef
		}{
			{KindFact, index.Fields}, {KindService, index.Services}, {KindCapability, index.Capabilities},
			{KindOperation, index.Operations}, {KindEffectRule, index.EffectRules},
		} {
			for _, ref := range group.refs {
				in.kinds[ref] = group.kind
			}
		}
		for _, service := range source.Metadata.Services {
			in.kinds[service.FactKeyDimension] = KindDimension
			in.roles[service.FactKeyDimension] = RoleResource
		}
		for _, field := range source.Metadata.Fields {
			in.kinds[field.Dimension] = KindDimension
			if _, ok := in.roles[field.Dimension]; !ok {
				in.roles[field.Dimension] = RoleQualifier
			}
		}
		in.fields = append(in.fields, source.Metadata.Fields...)
		in.services = append(in.services, source.Metadata.Services...)
		in.caps = append(in.caps, source.Metadata.Capabilities...)
		in.operations = append(in.operations, source.Metadata.Operations...)
	}
	for ref := range in.kinds {
		in.refs = append(in.refs, ref)
	}
	sortDefinitionRefs(in.refs)
	sort.Slice(in.fields, func(i, j int) bool { return compareDefinitionRef(in.fields[i].Ref, in.fields[j].Ref) < 0 })
	sort.Slice(in.services, func(i, j int) bool { return compareDefinitionRef(in.services[i].Ref, in.services[j].Ref) < 0 })
	sort.Slice(in.caps, func(i, j int) bool { return compareDefinitionRef(in.caps[i].Ref, in.caps[j].Ref) < 0 })
	sort.Slice(in.operations, func(i, j int) bool {
		return compareDefinitionRef(in.operations[i].Ref, in.operations[j].Ref) < 0
	})
	return in, nil
}

func sortDefinitionRefs(refs []semreg.DefinitionRef) {
	sort.Slice(refs, func(i, j int) bool { return compareDefinitionRef(refs[i], refs[j]) < 0 })
}

func compareDefinitionRef(a, b semreg.DefinitionRef) int {
	return compareParts(
		[]string{string(a.Pack.ID), string(a.Pack.Version), string(a.ID), string(a.Version)},
		[]string{string(b.Pack.ID), string(b.Pack.Version), string(b.ID), string(b.Version)},
	)
}

func compareParts(a, b []string) int {
	for i := range a {
		if c := bytes.Compare([]byte(a[i]), []byte(b[i])); c != 0 {
			return c
		}
	}
	return 0
}

// ------------------------------------------------------------- contract data

// kernelStateTypes lists the kernel state types of graph section 10.2 by
// their core element identifier.
var kernelStateTypes = func() map[semreg.DefinitionID]kernelStateType {
	out := map[semreg.DefinitionID]kernelStateType{}
	for kind, names := range map[ElementKind][]string{
		KindEvidenceState: {
			"OriginKind", "AssertionKind", "Qualification", "Promotion", "Validity", "Freshness", "EvidenceAccess",
			"RedactionState", "ConflictKind", "AnchorClass", "AnchorBasis", "RelationBasis",
		},
		KindLifecycleState: {
			"SourceState", "BindingState", "LinkState", "Availability", "RetainedRemoval", "ConflictState",
			"ResourceState", "RelationCoherence", "CutFindingKind", "Outcome", "DeliveryState", "AckState",
			"ReadbackRelation",
		},
		KindPresentation: {"ItemKind", "LossKind", "ProjectionOutcome"},
	} {
		for _, name := range names {
			out[semreg.DefinitionID("kernel."+snakeCase(name))] = kernelStateType{kind: kind, name: name}
		}
	}
	return out
}()

type kernelStateType struct {
	kind ElementKind
	name string
}

func snakeCase(name string) string {
	var out strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		out.WriteRune(r)
	}
	return out.String()
}

// contractPlanes are the planes of graph section 8.1.
var contractPlanes = []PlaneID{PlanePhysical, PlaneEnergy, PlaneHydraulic, PlaneControl, PlaneDiagnostic, PlaneProvenance}

type coreTypeLimits struct {
	structure Structure
	allowed   []PlaneID
	required  []PlaneID
}

// contractCoreTypes are the core relation types of graph section 7.1.
var contractCoreTypes = map[CoreType]coreTypeLimits{
	CoreContains: {StructureForest, []PlaneID{PlanePhysical, PlaneEnergy, PlaneHydraulic, PlaneDiagnostic}, []PlaneID{PlanePhysical}},
	CoreGroups:   {StructureAcyclic, []PlaneID{PlanePhysical, PlaneEnergy, PlaneHydraulic, PlaneControl}, nil},
	CoreSupplies: {StructureGraph, []PlaneID{PlaneEnergy, PlaneHydraulic}, nil},
	CoreMeasures: {StructureGraph, []PlaneID{PlaneEnergy, PlaneHydraulic, PlaneDiagnostic}, nil},
	CoreControls: {StructureGraph, []PlaneID{PlaneControl}, []PlaneID{PlaneControl}},
	CoreHosts:    {StructureForest, []PlaneID{PlaneEnergy, PlaneControl, PlaneDiagnostic}, nil},
	CoreRelays:   {StructureAcyclic, []PlaneID{PlaneProvenance, PlaneDiagnostic}, []PlaneID{PlaneProvenance}},
}

// mediumPlanes are the planes a supplies medium gives (graph section 7.1).
var mediumPlanes = map[Medium][]PlaneID{
	MediumElectrical:        {PlaneEnergy},
	MediumAir:               {PlaneEnergy},
	MediumHeatTransferFluid: {PlaneEnergy, PlaneHydraulic},
	MediumRefrigerant:       {PlaneEnergy, PlaneHydraulic},
}

// ownerKinds are the owners each element kind admits (graph section 10.2).
// A native extension entry may be of any kind (graph section 10.6).
var ownerKinds = map[ElementKind][]OwnerKind{
	KindResourceKind:   {OwnerCore},
	KindRelation:       {OwnerPack, OwnerCore},
	KindDimension:      {OwnerPack},
	KindService:        {OwnerPack},
	KindFact:           {OwnerPack},
	KindCapability:     {OwnerPack},
	KindOperation:      {OwnerPack},
	KindEffectRule:     {OwnerPack},
	KindEvidenceState:  {OwnerCore},
	KindLifecycleState: {OwnerCore},
	KindPresentation:   {OwnerPack, OwnerCore},
}

// Admission arms allowed per owner and element kind (graph section 10.5).
var (
	fullRuleArms  = []AdmissionArm{ArmMatterBaseline, ArmEEBus, ArmTwoVendors}
	structureArms = append([]AdmissionArm{ArmOwnerDecision}, fullRuleArms...)
	packArms      = append([]AdmissionArm{ArmAcceptedPack}, fullRuleArms...)
	coreArms      = map[ElementKind][]AdmissionArm{
		KindResourceKind:   append([]AdmissionArm{ArmAcceptedPack}, structureArms...),
		KindRelation:       structureArms,
		KindEvidenceState:  {ArmAcceptedKernel, ArmOwnerDecision},
		KindLifecycleState: {ArmAcceptedKernel, ArmOwnerDecision},
		KindPresentation:   {ArmAcceptedKernel},
	}
)

const nativePrefix = "native."

const maxEvidence = 8

// ---------------------------------------------------------------- validator

type validator struct {
	c         *Catalog
	in        *packInputs
	kinds     map[semreg.DefinitionID]int
	coreTypes map[CoreType]CoreRelationType
	planes    map[PlaneID]struct{}
	packKinds map[semreg.PackRef]map[semreg.DefinitionID]struct{}
	findings  []Finding
}

func (v *validator) add(rule, clause string, id semreg.ErrorID, entry, format string, args ...any) {
	v.findings = append(v.findings, Finding{Rule: rule, Clause: clause, ID: id, Entry: entry, Detail: fmt.Sprintf(format, args...)})
}

func (v *validator) entry(ref ElementRef) (*Element, bool) {
	at, ok := v.c.byRef[ref]
	if !ok {
		return nil, false
	}
	return &v.c.elements[at], true
}

func (v *validator) kindPlanes(kind semreg.DefinitionID) []PlaneID {
	at, ok := v.kinds[kind]
	if !ok {
		return nil
	}
	return v.c.elements[at].Planes
}

func (v *validator) dimensionKind(ref ElementRef) (semreg.DefinitionID, bool) {
	entry, ok := v.entry(ref)
	if !ok || entry.Dimension == nil || entry.Dimension.ResourceKind == "" {
		return "", false
	}
	return entry.Dimension.ResourceKind, true
}

// kindsByPack returns the resource kinds reached through the resource
// dimensions of each pack.
func (v *validator) kindsByPack() map[semreg.PackRef]map[semreg.DefinitionID]struct{} {
	out := map[semreg.PackRef]map[semreg.DefinitionID]struct{}{}
	for _, element := range v.c.elements {
		if element.Dimension == nil || element.Dimension.Role != RoleResource || element.Ref.Owner.Kind != OwnerPack {
			continue
		}
		pack := element.Ref.Owner.Pack
		if out[pack] == nil {
			out[pack] = map[semreg.DefinitionID]struct{}{}
		}
		out[pack][element.Dimension.ResourceKind] = struct{}{}
	}
	return out
}

type namedRef struct {
	label  string
	ref    ElementRef
	target ElementKind
}

// attributeRefs returns the definitions an entry's attributes name, with the
// element kind each must be. The sign reference is excluded.
func attributeRefs(element *Element) []namedRef {
	var out []namedRef
	switch {
	case element.Service != nil:
		out = append(out, namedRef{"fact_key_dimension", element.Service.FactKeyDimension, KindDimension})
	case element.Capability != nil:
		out = append(out, namedRef{"service", element.Capability.Service, KindService})
		if element.Capability.Operation != nil {
			out = append(out, namedRef{"operation", *element.Capability.Operation, KindOperation})
		}
	case element.Operation != nil:
		out = append(out,
			namedRef{"capability", element.Operation.Capability, KindCapability},
			namedRef{"argument", element.Operation.Argument, KindFact},
			namedRef{"effect_rule", element.Operation.EffectRule, KindEffectRule})
	case element.EffectRule != nil:
		out = append(out,
			namedRef{"operation", element.EffectRule.Operation, KindOperation},
			namedRef{"fact", element.EffectRule.Fact, KindFact})
	case element.Fact != nil:
		if element.Fact.Subject.Dimension != nil {
			out = append(out, namedRef{"subject dimension", *element.Fact.Subject.Dimension, KindDimension})
		}
		if element.Fact.Subject.QualifierDimension != nil {
			out = append(out, namedRef{"subject qualifier_dimension", *element.Fact.Subject.QualifierDimension, KindDimension})
		}
	case element.Relation != nil && element.Relation.EndpointDimensions != nil:
		out = append(out,
			namedRef{"endpoint from", element.Relation.EndpointDimensions.From, KindDimension},
			namedRef{"endpoint to", element.Relation.EndpointDimensions.To, KindDimension})
	}
	return out
}

// attributeKinds returns the resource kinds an entry's attributes name.
func attributeKinds(element *Element) []semreg.DefinitionID {
	var out []semreg.DefinitionID
	switch {
	case element.Relation != nil:
		out = append(append(out, element.Relation.FromKinds...), element.Relation.ToKinds...)
	case element.Dimension != nil && element.Dimension.ResourceKind != "":
		out = append(out, element.Dimension.ResourceKind)
	case element.Service != nil:
		out = append(out, element.Service.SubjectKind)
	case element.Fact != nil && element.Fact.Subject.Kind != "":
		out = append(out, element.Fact.Subject.Kind)
	}
	return out
}

func (v *validator) subjectKind(element *Element) (semreg.DefinitionID, bool) {
	switch {
	case element.Fact != nil:
		return element.Fact.Subject.Kind, element.Fact.Subject.Kind != ""
	case element.Service != nil:
		return element.Service.SubjectKind, true
	case element.Capability != nil:
		service, ok := v.entry(element.Capability.Service)
		if !ok || service.Service == nil {
			return "", false
		}
		return service.Service.SubjectKind, true
	}
	return "", false
}

// ------------------------------------------------------------------ CAT-1

func (v *validator) completeness() {
	seen := map[ElementRef]struct{}{}
	for _, element := range v.c.elements {
		if _, dup := seen[element.Ref]; dup {
			v.add("CAT-1", "", semreg.DuplicateIdentity, element.Ref.String(), "more than one entry")
		}
		seen[element.Ref] = struct{}{}
	}
	for _, ref := range v.in.refs {
		kind := v.in.kinds[ref]
		entry, ok := v.entry(PackElement(ref))
		switch {
		case !ok:
			v.add("CAT-1", "", semreg.MembershipMissing, PackElement(ref).String(), "pack %s has no catalog entry", kind)
		case entry.ElementKind != kind:
			v.add("CAT-1", "", semreg.InvalidValue, PackElement(ref).String(), "element kind %s, the pack declares a %s", entry.ElementKind, kind)
		}
	}
	endpointDimensions := map[ElementRef]struct{}{}
	for _, element := range v.c.elements {
		if element.Relation != nil && element.Relation.EndpointDimensions != nil && element.Ref.Owner.Kind == OwnerPack {
			ends := element.Relation.EndpointDimensions
			for _, end := range []ElementRef{ends.From, ends.To} {
				if end.Owner == element.Ref.Owner {
					endpointDimensions[end] = struct{}{}
				}
			}
		}
	}
	reported := map[ElementRef]struct{}{}
	for _, element := range v.c.elements {
		ref, isPack := element.Ref.DefinitionRef()
		if _, done := reported[element.Ref]; !isPack || done {
			continue
		}
		reported[element.Ref] = struct{}{}
		_, accepted := v.in.packs[ref.Pack]
		_, declared := v.in.kinds[ref]
		switch {
		case !accepted:
			v.add("CAT-1", "", semreg.DefinitionOwnerMissing, element.Ref.String(), "pack %s@%s is not an accepted pack", ref.Pack.ID, ref.Pack.Version)
		case declared:
		case element.ElementKind == KindRelation, element.ElementKind == KindPresentation:
		case element.ElementKind == KindDimension:
			if _, ok := endpointDimensions[element.Ref]; !ok {
				v.add("CAT-1", "", semreg.DefinitionOwnerMissing, element.Ref.String(), "no accepted pack field, service or relation names this dimension")
			}
		default:
			v.add("CAT-1", "", semreg.DefinitionOwnerMissing, element.Ref.String(), "no accepted pack declares it")
		}
	}
	v.kernelTypes()
}

func (v *validator) kernelTypes() {
	ids := make([]string, 0, len(kernelStateTypes))
	for id := range kernelStateTypes {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	for _, id := range ids {
		want := kernelStateTypes[semreg.DefinitionID(id)]
		at, ok := v.c.coreByID[semreg.DefinitionID(id)]
		if !ok {
			v.add("CAT-1", "", semreg.MembershipMissing, id, "kernel %s %s has no entry", want.kind, want.name)
			continue
		}
		entry := v.c.elements[at[0]]
		if entry.ElementKind != want.kind || (entry.KernelState != nil && entry.KernelState.KernelType != want.name) {
			v.add("CAT-1", "", semreg.InvalidValue, id, "entry does not list kernel %s %s", want.kind, want.name)
		}
	}
	reported := map[semreg.DefinitionID]struct{}{}
	for _, element := range v.c.elements {
		if element.Ref.Owner.Kind != OwnerCore || element.KernelState == nil && element.ElementKind != KindPresentation {
			continue
		}
		if _, listed := kernelStateTypes[element.Ref.ID]; listed {
			continue
		}
		if _, done := reported[element.Ref.ID]; !done {
			reported[element.Ref.ID] = struct{}{}
			v.add("CAT-1", "", semreg.DefinitionOwnerMissing, element.Ref.String(), "graph section 10.2 lists no such kernel type")
		}
	}
}

// ------------------------------------------------------------------ CAT-2

func (v *validator) disagree(ref ElementRef, format string, args ...any) {
	v.add("CAT-2", "", semreg.InvalidValue, ref.String(), format, args...)
}

func (v *validator) packEntry(ref semreg.DefinitionRef, kind ElementKind) (*Element, bool) {
	entry, ok := v.entry(PackElement(ref))
	if !ok || entry.ElementKind != kind {
		return nil, false
	}
	return entry, true
}

func (v *validator) agreement() {
	for _, field := range v.in.fields {
		entry, ok := v.packEntry(field.Ref, KindFact)
		if !ok {
			continue
		}
		subject := entry.Fact.Subject.Dimension
		if subject == nil {
			subject = entry.Fact.Subject.QualifierDimension
		}
		if subject == nil || *subject != PackElement(field.Dimension) {
			v.disagree(entry.Ref, "subject dimension differs from the pack field dimension %s", field.Dimension.ID)
		}
		_, quantity := v.in.registry.CanonicalUnit(field.Ref)
		if quantity != (entry.Fact.ValueKind == semreg.ValueQuantity) {
			v.disagree(entry.Ref, "value kind %s disagrees with the pack canonical unit", entry.Fact.ValueKind)
		}
	}
	for _, service := range v.in.services {
		if entry, ok := v.packEntry(service.Ref, KindService); ok && entry.Service.FactKeyDimension != PackElement(service.FactKeyDimension) {
			v.disagree(entry.Ref, "fact_key_dimension differs from the pack dimension %s", service.FactKeyDimension.ID)
		}
	}
	for _, capability := range v.in.caps {
		entry, ok := v.packEntry(capability.Ref, KindCapability)
		if !ok {
			continue
		}
		service, isPack := entry.Capability.Service.DefinitionRef()
		if !isPack || !v.in.registry.ServiceOwnsCapability(service, capability.Ref) {
			v.disagree(entry.Ref, "service %s does not own the capability in the pack metadata", entry.Capability.Service)
		}
	}
	for _, operation := range v.in.operations {
		v.operationAgreement(operation)
	}
	refs := make([]semreg.DefinitionRef, 0, len(v.in.roles))
	for ref := range v.in.roles {
		refs = append(refs, ref)
	}
	sortDefinitionRefs(refs)
	for _, ref := range refs {
		if entry, ok := v.packEntry(ref, KindDimension); ok && entry.Dimension.Role != v.in.roles[ref] {
			v.disagree(entry.Ref, "role %s, the pack uses it as a %s dimension", entry.Dimension.Role, v.in.roles[ref])
		}
	}
	v.subjectKinds()
}

func (v *validator) operationAgreement(operation semreg.OperationMetadata) {
	if entry, ok := v.packEntry(operation.Ref, KindOperation); ok {
		attributes := entry.Operation
		capability, capOK := attributes.Capability.DefinitionRef()
		argument, argOK := attributes.Argument.DefinitionRef()
		effect, effectOK := attributes.EffectRule.DefinitionRef()
		service := operation.Service
		if capEntry, ok := v.packEntry(capability, KindCapability); ok {
			if ref, isPack := capEntry.Capability.Service.DefinitionRef(); isPack {
				service = ref
			}
		}
		if !capOK || !argOK || !effectOK || !v.in.registry.OperationMatches(operation.Ref, capability, service, argument, effect) {
			v.disagree(entry.Ref, "capability, argument and effect rule are not the pack operation shape")
		}
	}
	if entry, ok := v.packEntry(operation.Capability, KindCapability); ok {
		if entry.Capability.Operation == nil || *entry.Capability.Operation != PackElement(operation.Ref) {
			v.disagree(entry.Ref, "operation differs from the pack operation %s", operation.Ref.ID)
		}
	}
	if entry, ok := v.packEntry(operation.Effect, KindEffectRule); ok {
		if entry.EffectRule.Operation != PackElement(operation.Ref) {
			v.disagree(entry.Ref, "operation differs from the pack operation %s", operation.Ref.ID)
		}
		if entry.EffectRule.Fact != PackElement(operation.Argument) {
			v.disagree(entry.Ref, "fact differs from the pack operation argument %s", operation.Argument.ID)
		}
	}
}

func (v *validator) subjectKinds() {
	for i := range v.c.elements {
		element := &v.c.elements[i]
		type pair struct {
			kind      semreg.DefinitionID
			dimension ElementRef
		}
		var pairs []pair
		switch {
		case element.Service != nil:
			pairs = append(pairs, pair{element.Service.SubjectKind, element.Service.FactKeyDimension})
		case element.Fact != nil && element.Fact.Subject.Status == SubjectResolved:
			pairs = append(pairs, pair{element.Fact.Subject.Kind, *element.Fact.Subject.Dimension})
		case element.Relation != nil && element.Relation.EndpointDimensions != nil:
			for _, kind := range element.Relation.FromKinds {
				pairs = append(pairs, pair{kind, element.Relation.EndpointDimensions.From})
			}
			for _, kind := range element.Relation.ToKinds {
				pairs = append(pairs, pair{kind, element.Relation.EndpointDimensions.To})
			}
		}
		for _, p := range pairs {
			if kind, ok := v.dimensionKind(p.dimension); !ok || kind != p.kind {
				v.disagree(element.Ref, "kind %s is not the resource kind of dimension %s", p.kind, p.dimension)
			}
		}
	}
}

// ------------------------------------------------------------------ CAT-3

func (v *validator) dispositions() {
	for _, element := range v.c.elements {
		for _, problem := range dispositionProblems(element) {
			v.add("CAT-3", "", semreg.MembershipMissing, element.Ref.String(), "%s", problem)
		}
	}
}

func dispositionProblems(element Element) []string {
	var problems []string
	switch element.Disposition {
	case DispositionMember:
		if len(element.Planes) == 0 {
			problems = append(problems, "member with no plane")
		}
		if element.Reason != "" || element.Tracking != "" {
			problems = append(problems, "member with a reason or tracking issue")
		}
	case DispositionUnsupported, DispositionUnknown:
		if len(element.Planes) > 0 {
			problems = append(problems, fmt.Sprintf("%s entry is in planes %v", element.Disposition, element.Planes))
		}
		if element.Reason == "" {
			problems = append(problems, fmt.Sprintf("%s entry has no reason", element.Disposition))
		}
		if element.Disposition == DispositionUnknown && element.Tracking == "" {
			problems = append(problems, "unknown entry has no tracking issue")
		}
		if element.Disposition == DispositionUnsupported && element.Tracking != "" {
			problems = append(problems, "unsupported entry carries a tracking issue; its reason is final")
		}
	}
	return problems
}

// ------------------------------------------------------------------ CAT-4

func (v *validator) closure() {
	v.structure()
	v.planeNames()
	v.references()
	v.pln5()
	v.pln6()
	v.lns1()
}

func (v *validator) structure() {
	seenPlanes := map[PlaneID]struct{}{}
	for _, plane := range v.c.planes {
		if _, dup := seenPlanes[plane.ID]; dup {
			v.add("CAT-4", "", semreg.DuplicateIdentity, "plane "+string(plane.ID), "identifier is not unique")
		}
		seenPlanes[plane.ID] = struct{}{}
	}
	seenTypes := map[CoreType]struct{}{}
	for _, core := range v.c.coreTypes {
		if _, dup := seenTypes[core.ID]; dup {
			v.add("CAT-4", "", semreg.DuplicateIdentity, "core type "+string(core.ID), "identifier is not unique")
		}
		seenTypes[core.ID] = struct{}{}
	}
	seenLenses := map[semreg.DefinitionID]struct{}{}
	for _, lens := range v.c.lenses {
		if _, dup := seenLenses[lens.ID]; dup {
			v.add("CAT-4", "", semreg.DuplicateIdentity, "lens "+string(lens.ID), "identifier is not unique")
		}
		seenLenses[lens.ID] = struct{}{}
	}
	for _, plane := range contractPlanes {
		if _, ok := v.planes[plane]; !ok {
			v.add("CAT-4", "", semreg.MembershipMissing, "plane "+string(plane), "graph section 8.1 plane is not listed")
		}
	}
	for _, id := range []CoreType{CoreContains, CoreGroups, CoreSupplies, CoreMeasures, CoreControls, CoreHosts, CoreRelays} {
		limits := contractCoreTypes[id]
		core, ok := v.coreTypes[id]
		switch {
		case !ok:
			v.add("CAT-4", "", semreg.MembershipMissing, "core type "+string(id), "graph section 7.1 type is not listed")
		case core.Structure != limits.structure || !samePlanes(core.AllowedPlanes, limits.allowed) || !samePlanes(core.RequiredPlanes, limits.required):
			v.add("CAT-4", "", semreg.InvalidValue, "core type "+string(id), "structure or planes differ from graph section 7.1")
		}
	}
}

func (v *validator) unknownPlanes(owner string, planes []PlaneID) {
	for _, plane := range planes {
		if _, ok := v.planes[plane]; !ok {
			v.add("CAT-4", "", semreg.MembershipMissing, owner, "plane %s is not in the catalog", plane)
		}
	}
}

func (v *validator) planeNames() {
	for _, element := range v.c.elements {
		v.unknownPlanes(element.Ref.String(), element.Planes)
	}
	for _, core := range v.c.coreTypes {
		v.unknownPlanes("core type "+string(core.ID), append(append([]PlaneID(nil), core.AllowedPlanes...), core.RequiredPlanes...))
	}
	for _, lens := range v.c.lenses {
		v.unknownPlanes("lens "+string(lens.ID), lens.Planes)
	}
}

func (v *validator) references() {
	for i := range v.c.elements {
		element := &v.c.elements[i]
		for _, named := range attributeRefs(element) {
			if entry, ok := v.entry(named.ref); !ok || entry.ElementKind != named.target {
				v.add("CAT-4", "", semreg.MembershipMissing, element.Ref.String(), "%s %s is not a %s entry", named.label, named.ref, named.target)
			}
		}
		for _, kind := range attributeKinds(element) {
			if _, ok := v.kinds[kind]; !ok {
				v.add("CAT-4", "", semreg.MembershipMissing, element.Ref.String(), "kind %s is not a resource kind", kind)
			}
		}
	}
}

func (v *validator) pln5() {
	for i := range v.c.elements {
		element := &v.c.elements[i]
		type bound struct {
			what   string
			planes []PlaneID
			exact  bool
		}
		var bounds []bound
		switch {
		case element.Relation != nil:
			for _, kind := range append(append([]semreg.DefinitionID(nil), element.Relation.FromKinds...), element.Relation.ToKinds...) {
				bounds = append(bounds, bound{"kind " + string(kind), v.kindPlanes(kind), false})
			}
		case element.EffectRule != nil:
			if operation, ok := v.entry(element.EffectRule.Operation); ok {
				bounds = append(bounds, bound{"its operation", operation.Planes, true})
			}
		case element.Dimension != nil && element.Dimension.Role == RoleResource:
			bounds = append(bounds, bound{"its kind " + string(element.Dimension.ResourceKind), v.kindPlanes(element.Dimension.ResourceKind), true})
		default:
			if kind, ok := v.subjectKind(element); ok {
				bounds = append(bounds, bound{"subject kind " + string(kind), v.kindPlanes(kind), false})
			}
		}
		for _, b := range bounds {
			if b.exact && !samePlanes(element.Planes, b.planes) {
				v.add("CAT-4", "PLN-5", semreg.MembershipMissing, element.Ref.String(), "planes %v differ from %s %v", element.Planes, b.what, b.planes)
			}
			if !b.exact && !subsetPlanes(element.Planes, b.planes) {
				v.add("CAT-4", "PLN-5", semreg.MembershipMissing, element.Ref.String(), "planes %v are not admitted by %s %v", element.Planes, b.what, b.planes)
			}
		}
	}
}

func (v *validator) pln6() {
	for _, element := range v.c.elements {
		if element.Relation == nil || element.Disposition != DispositionMember {
			continue
		}
		core, ok := v.coreTypes[element.Relation.CoreType]
		if !ok {
			v.add("CAT-4", "PLN-6", semreg.MembershipMissing, element.Ref.String(), "core type %s is not in the catalog", element.Relation.CoreType)
			continue
		}
		if !subsetPlanes(element.Planes, core.AllowedPlanes) {
			v.add("CAT-4", "PLN-6", semreg.MembershipMissing, element.Ref.String(), "planes %v exceed the allowed planes of %s", element.Planes, core.ID)
		}
		if !subsetPlanes(core.RequiredPlanes, element.Planes) {
			v.add("CAT-4", "PLN-6", semreg.MembershipMissing, element.Ref.String(), "planes %v miss the required planes of %s", element.Planes, core.ID)
		}
		if element.Relation.Medium != "" && !subsetPlanes(element.Planes, mediumPlanes[element.Relation.Medium]) {
			v.add("CAT-4", "PLN-6", semreg.MembershipMissing, element.Ref.String(), "planes %v are not given by medium %s", element.Planes, element.Relation.Medium)
		}
	}
}

func (v *validator) lns1() {
	for _, lens := range v.c.lenses {
		name := "lens " + string(lens.ID)
		for _, kind := range lens.RootKinds {
			at, ok := v.kinds[kind]
			if !ok || v.c.elements[at].Disposition != DispositionMember || !intersectPlanes(lens.Planes, v.c.elements[at].Planes) {
				v.add("CAT-4", "LNS-1", semreg.MembershipMissing, name, "root kind %s is not a member kind admitting a lens plane", kind)
			}
		}
		for _, ref := range lens.Relations {
			entry, ok := v.entry(ref)
			switch {
			case !ok || entry.ElementKind != KindRelation || entry.Disposition != DispositionMember:
				v.add("CAT-4", "LNS-1", semreg.MembershipMissing, name, "relation %s is not a member relation entry", ref)
			case !intersectPlanes(lens.Planes, entry.Planes):
				v.add("CAT-4", "LNS-1", semreg.MembershipMissing, name, "relation %s is a member of no lens plane", ref)
			}
		}
	}
}

func subsetPlanes(planes, bound []PlaneID) bool {
	for _, plane := range planes {
		if !containsPlane(bound, plane) {
			return false
		}
	}
	return true
}

func samePlanes(a, b []PlaneID) bool { return subsetPlanes(a, b) && subsetPlanes(b, a) }

func intersectPlanes(a, b []PlaneID) bool {
	for _, plane := range a {
		if containsPlane(b, plane) {
			return true
		}
	}
	return false
}

func containsPlane(planes []PlaneID, plane PlaneID) bool {
	for _, candidate := range planes {
		if candidate == plane {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------ CAT-5

func (v *validator) ownership() {
	for i := range v.c.elements {
		element := &v.c.elements[i]
		for _, problem := range v.ownerProblems(element) {
			v.add("CAT-5", "", semreg.DefinitionOwnerConflict, element.Ref.String(), "%s", problem)
		}
	}
}

func (v *validator) ownerProblems(element *Element) []string {
	var problems []string
	owner := element.Ref.Owner
	if owner.Kind != OwnerNative && !containsOwner(ownerKinds[element.ElementKind], owner.Kind) {
		problems = append(problems, fmt.Sprintf("a %s is never %s-owned", element.ElementKind, owner.Kind))
	}
	if strings.HasPrefix(string(element.Ref.ID), nativePrefix) != (owner.Kind == OwnerNative) {
		problems = append(problems, "only a native-owned entry uses the "+nativePrefix+" namespace")
	}
	if owner.Kind == OwnerNative && element.Disposition != DispositionUnknown {
		problems = append(problems, "an extension entry is always unknown")
	}
	if owner.Kind != OwnerPack {
		return problems
	}
	for _, named := range attributeRefs(element) {
		if named.ref.Owner != owner {
			problems = append(problems, fmt.Sprintf("%s %s belongs to another owner", named.label, named.ref))
		}
	}
	if element.Relation != nil {
		reached := v.packKinds[owner.Pack]
		for _, kind := range append(append([]semreg.DefinitionID(nil), element.Relation.FromKinds...), element.Relation.ToKinds...) {
			if _, ok := reached[kind]; !ok {
				problems = append(problems, fmt.Sprintf("endpoint kind %s is not reached through a dimension of its pack", kind))
			}
		}
	}
	return problems
}

func containsOwner(kinds []OwnerKind, kind OwnerKind) bool {
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------ CAT-6

// seed reports whether an entry may carry the arm accepted_pack: an entry of
// an accepted pack, or a core resource kind reached through a resource
// dimension of an accepted pack.
func (v *validator) seed(element *Element) bool {
	switch element.Ref.Owner.Kind {
	case OwnerPack:
		_, accepted := v.in.packs[element.Ref.Owner.Pack]
		return accepted
	case OwnerCore:
		if element.ElementKind != KindResourceKind {
			return false
		}
		for pack, kinds := range v.packKinds {
			if _, accepted := v.in.packs[pack]; !accepted {
				continue
			}
			if _, ok := kinds[element.Ref.ID]; ok {
				return true
			}
		}
	}
	return false
}

func (v *validator) allowedArms(element *Element) []AdmissionArm {
	switch element.Ref.Owner.Kind {
	case OwnerPack:
		return packArms
	case OwnerNative:
		return structureArms
	}
	return coreArms[element.ElementKind]
}

func (v *validator) admission() {
	for _, plane := range v.c.planes {
		v.admissionRecord("CAT-6", "plane "+string(plane.ID), plane.Admission, structureArms)
	}
	for _, core := range v.c.coreTypes {
		v.admissionRecord("CAT-6", "core type "+string(core.ID), core.Admission, structureArms)
	}
	for i := range v.c.elements {
		element := &v.c.elements[i]
		if !v.admissionRecord("CAT-6", element.Ref.String(), element.Admission, v.allowedArms(element)) {
			continue
		}
		if element.Admission.Arm == ArmAcceptedPack && !v.seed(element) {
			v.add("CAT-6", "", semreg.InvalidValue, element.Ref.String(), "arm accepted_pack outside the accepted seed")
		}
	}
}

// admissionRecord checks the arm against the allowed arms and the record's
// evidence shape; it reports whether the arm is allowed.
func (v *validator) admissionRecord(rule, name string, admission Admission, allowed []AdmissionArm) bool {
	armAllowed := false
	for _, arm := range allowed {
		armAllowed = armAllowed || arm == admission.Arm
	}
	if !armAllowed {
		v.add(rule, "", semreg.InvalidValue, name, "arm %s is not allowed", admission.Arm)
	}
	for _, problem := range evidenceProblems(admission) {
		v.add(rule, "", semreg.InvalidValue, name, "%s", problem)
	}
	return armAllowed
}

func evidenceProblems(admission Admission) []string {
	var problems []string
	if len(admission.Evidence) == 0 || len(admission.Evidence) > maxEvidence {
		problems = append(problems, fmt.Sprintf("%d evidence links, 1 through %d required", len(admission.Evidence), maxEvidence))
	}
	seen := map[string]struct{}{}
	for _, link := range admission.Evidence {
		if _, dup := seen[link]; dup {
			problems = append(problems, "evidence link repeated: "+link)
		}
		seen[link] = struct{}{}
		if !pinnedPublicURL(link) {
			problems = append(problems, "evidence is not a public link pinned to a revision: "+link)
		}
		if admission.Arm == ArmOwnerDecision && !issueURLPattern.MatchString(link) {
			problems = append(problems, "owner_decision evidence must be an issue: "+link)
		}
	}
	if (admission.ResearchReport != "") != (admission.Arm == ArmTwoVendors) {
		problems = append(problems, "a research report is required for, and only for, the arm two_vendors")
	}
	if admission.ResearchReport != "" && !issueURLPattern.MatchString(admission.ResearchReport) {
		problems = append(problems, "research report is not an issue")
	}
	return problems
}

func pinnedPublicURL(link string) bool {
	if strings.HasPrefix(link, "https://github.com/") {
		return githubURLPattern.MatchString(link)
	}
	return otherURLPattern.MatchString(link)
}

// ------------------------------------------------------------------ CAT-7

func (v *validator) signReferences() {
	for i := range v.c.elements {
		element := &v.c.elements[i]
		if element.Fact == nil || element.Fact.Direction != DirectionSigned {
			continue
		}
		sign := element.Fact.SignReference
		switch {
		case sign == nil:
			v.add("CAT-7", "", semreg.InvalidValue, element.Ref.String(), "signed fact names no sign reference")
		case sign.NativeConvention:
			if element.Admission.Arm != ArmAcceptedPack || !v.seed(element) {
				v.add("CAT-7", "", semreg.InvalidValue, element.Ref.String(), "native_convention is allowed only for a seed entry")
			}
		default:
			named, ok := v.entry(*sign.Relation)
			switch {
			case !ok:
				v.add("CAT-7", "", semreg.MembershipMissing, element.Ref.String(), "sign reference %s has no entry", *sign.Relation)
			case named.Relation == nil || named.Relation.CoreType != CoreSupplies:
				v.add("CAT-7", "", semreg.InvalidValue, element.Ref.String(), "sign reference %s is not a supplies relation definition", *sign.Relation)
			}
		}
	}
}

// ------------------------------------------------------------------ CAT-8

func orderParts(ref ElementRef) []string {
	packID, packVersion := "", ""
	if ref.Owner.Kind == OwnerPack {
		packID, packVersion = string(ref.Owner.Pack.ID), string(ref.Owner.Pack.Version)
	}
	return []string{string(ref.Owner.Kind), packID, packVersion, string(ref.ID), string(ref.Version)}
}

func (v *validator) order() {
	for i := 1; i < len(v.c.elements); i++ {
		earlier, later := v.c.elements[i-1].Ref, v.c.elements[i].Ref
		if compareParts(orderParts(later), orderParts(earlier)) < 0 {
			v.add("CAT-8", "", semreg.NoncanonicalOrder, later.String(), "sorts before %s", earlier)
		}
	}
}

// ------------------------------------------------------------------ CAT-9

func (v *validator) schemes() {
	seen := map[semreg.DefinitionID]struct{}{}
	for _, scheme := range v.c.schemes {
		name := "scheme " + string(scheme.ID)
		if _, dup := seen[scheme.ID]; dup {
			v.add("CAT-9", "", semreg.DuplicateIdentity, name, "identifier is not unique")
		}
		seen[scheme.ID] = struct{}{}
		v.admissionRecord("CAT-9", name, scheme.Admission, []AdmissionArm{ArmPublicRegistry})
	}
}
