package semreg

import (
	"reflect"
	"strings"
)

// ResourceID identifies one resource of the semantic graph below an asset. The
// gateway allocates it. String equality with an AssetID, ServiceInstanceID or
// dimension text never makes them the same identity.
type ResourceID OpaqueID

// ResourceState is the lifecycle of a Resource.
type ResourceState string

// ResourceState values. Withdrawn is reversible to current with a new
// revision; retired is irreversible.
const (
	ResourceCurrent   ResourceState = "current"
	ResourceWithdrawn ResourceState = "withdrawn"
	ResourceRetired   ResourceState = "retired"
)

// AnchorClass states what a resource anchor follows.
type AnchorClass string

// AnchorClass values.
const (
	AnchorEquipment AnchorClass = "equipment"
	AnchorComponent AnchorClass = "component"
	AnchorFunction  AnchorClass = "function"
	AnchorEpisode   AnchorClass = "episode"
)

// AnchorBasis states what establishes a resource anchor.
type AnchorBasis string

// AnchorBasis values. Only AnchorNativeIdentity carries an anchor key.
const (
	AnchorNativeIdentity AnchorBasis = "native_identity"
	AnchorNativeSlot     AnchorBasis = "native_slot"
	AnchorOperatorSlot   AnchorBasis = "operator_slot"
)

// RelationBasis is the basis class of a relation instance or absence
// statement.
type RelationBasis string

// RelationBasis values.
const (
	RelationNativeObservation RelationBasis = "native_observation"
	RelationConfiguration     RelationBasis = "configuration"
	RelationGateRule          RelationBasis = "gate_rule"
)

// AbsenceSide names the end of a relation definition an absence statement is
// about.
type AbsenceSide string

// AbsenceSide values.
const (
	AbsenceFrom AbsenceSide = "from"
	AbsenceTo   AbsenceSide = "to"
)

// ResourceAnchor is the identity basis of a resource. Key is present exactly
// when Basis is native_identity. Slot is NFC text of 1 through 128 bytes
// without control characters. Evidence holds 1 through 32 unique, sorted
// references.
type ResourceAnchor struct {
	Class    AnchorClass   `json:"class"`
	Basis    AnchorBasis   `json:"basis"`
	Key      *Digest       `json:"key,omitempty"`
	Parent   *ResourceID   `json:"parent,omitempty"`
	Slot     *string       `json:"slot,omitempty"`
	Evidence []EvidenceRef `json:"evidence"`
}

// AnchorSupersession records the exact previous anchor of a resource whose
// anchor was upgraded, with its continuity evidence. An empty evidence list is
// identity_drift.
type AnchorSupersession struct {
	Previous ResourceAnchor `json:"previous"`
	Evidence []EvidenceRef  `json:"evidence"`
}

// Resource is the identity record of one resource homed in the asset of its
// snapshot. It is not an observation. Revision is positive.
type Resource struct {
	ResourceID ResourceID          `json:"resource_id"`
	Kind       DefinitionID        `json:"kind"`
	Anchor     ResourceAnchor      `json:"anchor"`
	Supersedes *AnchorSupersession `json:"supersedes,omitempty"`
	State      ResourceState       `json:"state"`
	Origin     OriginRef           `json:"origin"`
	Revision   Uint64              `json:"revision"`
}

// RelationInstance relates two resources under one relation definition. Its
// key is (definition, from, to). Basis holds 1 through 32 unique, sorted
// references; an empty basis is relation_not_qualified. Conflict is derived
// by the kernel and is never upserted by a publisher.
type RelationInstance struct {
	Definition DefinitionRef `json:"definition"`
	From       ResourceID    `json:"from"`
	To         ResourceID    `json:"to"`
	State      LinkState     `json:"state"`
	BasisClass RelationBasis `json:"basis_class"`
	Basis      []EvidenceRef `json:"basis"`
	Origin     OriginRef     `json:"origin"`
	Revision   Uint64        `json:"revision"`
}

// RelationKey names one relation instance, for example in a withdrawal.
type RelationKey struct {
	Definition DefinitionRef `json:"definition"`
	From       ResourceID    `json:"from"`
	To         ResourceID    `json:"to"`
}

// RelationAbsence states that a resource has no relation of a definition on
// one side. Its key is (definition, resource, side) and its state is candidate
// or qualified. Without an instance or an absence statement the relation is
// unknown, never "none".
type RelationAbsence struct {
	Definition DefinitionRef `json:"definition"`
	Resource   ResourceID    `json:"resource"`
	Side       AbsenceSide   `json:"side"`
	State      LinkState     `json:"state"`
	BasisClass RelationBasis `json:"basis_class"`
	Basis      []EvidenceRef `json:"basis"`
	Origin     OriginRef     `json:"origin"`
	Revision   Uint64        `json:"revision"`
}

// RelationAbsenceKey names one absence statement, for example in a
// withdrawal.
type RelationAbsenceKey struct {
	Definition DefinitionRef `json:"definition"`
	Resource   ResourceID    `json:"resource"`
	Side       AbsenceSide   `json:"side"`
}

// Per-snapshot bounds of the graph collections.
const (
	maxSnapshotResources = 4096
	maxSnapshotRelations = 8192
	maxSnapshotAbsences  = 1024
	maxGraphSlotBytes    = 128
)

func (v ResourceID) Validate() error { return validateOpaqueValue(string(v), "resource id") }

func (s ResourceState) Validate() error {
	if s != ResourceCurrent && s != ResourceWithdrawn && s != ResourceRetired {
		return errID(InvalidEnum, "resource state")
	}
	return nil
}

func (c AnchorClass) Validate() error {
	if c != AnchorEquipment && c != AnchorComponent && c != AnchorFunction && c != AnchorEpisode {
		return errID(InvalidEnum, "anchor class")
	}
	return nil
}

func (b AnchorBasis) Validate() error {
	if b != AnchorNativeIdentity && b != AnchorNativeSlot && b != AnchorOperatorSlot {
		return errID(InvalidEnum, "anchor basis")
	}
	return nil
}

func (b RelationBasis) Validate() error {
	if b != RelationNativeObservation && b != RelationConfiguration && b != RelationGateRule {
		return errID(InvalidEnum, "relation basis class")
	}
	return nil
}

func (s AbsenceSide) Validate() error {
	if s != AbsenceFrom && s != AbsenceTo {
		return errID(InvalidEnum, "absence side")
	}
	return nil
}

func (a ResourceAnchor) Validate() error {
	errs := []error{a.Class.Validate(), a.Basis.Validate(), validateEvidenceSet(a.Evidence, 1, 32)}
	if a.Key != nil {
		errs = append(errs, a.Key.Validate())
	}
	if (a.Basis == AnchorNativeIdentity) != (a.Key != nil) {
		errs = append(errs, errID(InvalidValue, "anchor key presence"))
	}
	if a.Parent != nil {
		errs = append(errs, a.Parent.Validate())
	}
	if a.Slot != nil {
		errs = append(errs, validateText(*a.Slot, 1, maxGraphSlotBytes, false))
	}
	return bestError(errs...)
}

func (s AnchorSupersession) Validate() error {
	return bestError(s.Previous.Validate(), validateGraphBasis(s.Evidence, IdentityDrift, "supersession evidence"))
}

func (r Resource) Validate() error {
	errs := []error{r.ResourceID.Validate(), r.Kind.Validate(), r.Anchor.Validate(), r.State.Validate(), r.Origin.Validate(), graphRevisionError(r.Revision, "resource revision")}
	if r.Supersedes != nil {
		errs = append(errs, r.Supersedes.Validate())
	}
	return bestError(errs...)
}

func (r RelationInstance) Validate() error {
	var state error
	if r.State != LinkCandidate && r.State != LinkQualified && r.State != LinkRejected && r.State != LinkConflict {
		state = errID(InvalidEnum, "relation state")
	}
	return bestError(RelationKey{r.Definition, r.From, r.To}.Validate(), state, r.BasisClass.Validate(), validateGraphBasis(r.Basis, RelationNotQualified, "relation basis"), r.Origin.Validate(), graphRevisionError(r.Revision, "relation revision"))
}

func (k RelationKey) Validate() error {
	return bestError(k.Definition.Validate(), k.From.Validate(), k.To.Validate())
}

func (a RelationAbsence) Validate() error {
	var state error
	if a.State != LinkCandidate && a.State != LinkQualified {
		state = errID(InvalidEnum, "absence state")
	}
	return bestError(RelationAbsenceKey{a.Definition, a.Resource, a.Side}.Validate(), state, a.BasisClass.Validate(), validateGraphBasis(a.Basis, RelationNotQualified, "absence basis"), a.Origin.Validate(), graphRevisionError(a.Revision, "absence revision"))
}

func (k RelationAbsenceKey) Validate() error {
	return bestError(k.Definition.Validate(), k.Resource.Validate(), k.Side.Validate())
}

// WireCollectionKeyFields and ValidateWireCollectionKey let Decode report a
// duplicate or misordered graph key when another member prevents binding.
func (Resource) WireCollectionKeyFields() []string  { return []string{"resource_id"} }
func (r Resource) ValidateWireCollectionKey() error { return r.ResourceID.Validate() }
func (RelationInstance) WireCollectionKeyFields() []string {
	return []string{"definition", "from", "to"}
}
func (r RelationInstance) ValidateWireCollectionKey() error {
	return RelationKey{r.Definition, r.From, r.To}.Validate()
}
func (RelationKey) WireCollectionKeyFields() []string  { return []string{"definition", "from", "to"} }
func (k RelationKey) ValidateWireCollectionKey() error { return k.Validate() }
func (RelationAbsence) WireCollectionKeyFields() []string {
	return []string{"definition", "resource", "side"}
}
func (a RelationAbsence) ValidateWireCollectionKey() error {
	return RelationAbsenceKey{a.Definition, a.Resource, a.Side}.Validate()
}
func (RelationAbsenceKey) WireCollectionKeyFields() []string {
	return []string{"definition", "resource", "side"}
}
func (k RelationAbsenceKey) ValidateWireCollectionKey() error { return k.Validate() }

// validateGraphBasis keeps the contract's error for an empty list: nil is a
// missing member, empty is the record's qualification error, and a non-empty
// list holds 1 through 32 unique, sorted references.
func validateGraphBasis(evidence []EvidenceRef, empty ErrorID, detail string) error {
	if evidence == nil {
		return errID(MissingMember, detail)
	}
	if len(evidence) == 0 {
		return errID(empty, detail)
	}
	return validateEvidenceSet(evidence, 1, 32)
}

func graphRevisionError(revision Uint64, detail string) error {
	if !positive(revision) {
		return errID(InvalidIdentifier, detail)
	}
	return nil
}

// Graph keys compare by the canonical bytes of the definition and then the
// remaining key members. A NUL separator cannot occur in canonical JSON or an
// opaque identifier, so byte order of the joined key is the tuple order.
func canonicalDefinitionKey(ref DefinitionRef) (string, bool) {
	canonical, err := CanonicalJSON(ref)
	return string(canonical), err == nil
}

func resourceGraphKey(r Resource) (string, bool) {
	return string(r.ResourceID), r.ResourceID.Validate() == nil
}

func relationGraphKey(k RelationKey) (string, bool) {
	definition, ok := canonicalDefinitionKey(k.Definition)
	if !ok || k.From.Validate() != nil || k.To.Validate() != nil {
		return "", false
	}
	return definition + "\x00" + string(k.From) + "\x00" + string(k.To), true
}

func absenceGraphKey(k RelationAbsenceKey) (string, bool) {
	definition, ok := canonicalDefinitionKey(k.Definition)
	if !ok || k.Resource.Validate() != nil || k.Side.Validate() != nil {
		return "", false
	}
	return definition + "\x00" + string(k.Resource) + "\x00" + string(k.Side), true
}

func relationInstanceGraphKey(r RelationInstance) (string, bool) {
	return relationGraphKey(RelationKey{r.Definition, r.From, r.To})
}

func absenceStatementGraphKey(a RelationAbsence) (string, bool) {
	return absenceGraphKey(RelationAbsenceKey{a.Definition, a.Resource, a.Side})
}

// graphOrderError rejects duplicate and descending keys. An invalid key takes
// no part, as in the kernel's other collections; its record error remains.
func graphOrderError[T any](items []T, key func(T) (string, bool)) error {
	seen := make(map[string]struct{}, len(items))
	var previous string
	havePrevious := false
	var duplicate, descending error
	for _, item := range items {
		current, ok := key(item)
		if !ok {
			continue
		}
		if _, exists := seen[current]; exists {
			duplicate = errID(DuplicateKey, "graph collection")
		}
		if havePrevious && strings.Compare(previous, current) > 0 {
			descending = errID(NoncanonicalOrder, "graph collection")
		}
		seen[current] = struct{}{}
		previous, havePrevious = current, true
	}
	return bestError(duplicate, descending)
}

func graphRecordsError[T Record](items []T, key func(T) (string, bool)) error {
	errs := make([]error, 0, len(items)+1)
	for _, item := range items {
		errs = append(errs, item.Validate())
	}
	return bestError(append(errs, graphOrderError(items, key))...)
}

func (i IdentityLink) graphMembersError() error {
	var errs []error
	if i.Resource != nil {
		errs = append(errs, i.Resource.Validate())
	}
	if i.Key != nil {
		errs = append(errs, i.Key.Validate())
	}
	return bestError(errs...)
}

func (s ServiceInstance) graphMembersError() error {
	if s.Resource != nil {
		return s.Resource.Validate()
	}
	return nil
}

// graphMembersError checks the value shapes and canonical order of the graph
// members of a batch. It applies no publication rule.
func (b PublicationBatch) graphMembersError() error {
	errs := []error{
		graphRecordsError(b.ResourceUpserts, resourceGraphKey),
		graphRecordsError(b.RelationUpserts, relationInstanceGraphKey),
		graphRecordsError(b.RelationWithdrawals, relationGraphKey),
		graphRecordsError(b.AbsenceUpserts, absenceStatementGraphKey),
		graphRecordsError(b.AbsenceWithdrawals, absenceGraphKey),
	}
	for _, relation := range b.RelationUpserts {
		if relation.State == LinkConflict {
			errs = append(errs, errID(InvalidValue, "publisher-supplied relation conflict"))
		}
	}
	for _, link := range b.IdentityLinkUpserts {
		errs = append(errs, link.graphMembersError())
	}
	for _, service := range b.ServiceUpserts {
		errs = append(errs, service.graphMembersError())
	}
	return bestError(errs...)
}

// graphMembers reports whether a batch carries any graph member, including
// the resource and key members of identity links and services.
func (b PublicationBatch) graphMembers() bool {
	if len(b.ResourceUpserts) != 0 || len(b.RelationUpserts) != 0 || len(b.RelationWithdrawals) != 0 || len(b.AbsenceUpserts) != 0 || len(b.AbsenceWithdrawals) != 0 {
		return true
	}
	for _, link := range b.IdentityLinkUpserts {
		if link.Resource != nil || link.Key != nil {
			return true
		}
	}
	for _, service := range b.ServiceUpserts {
		if service.Resource != nil {
			return true
		}
	}
	return false
}

// graphMembershipError is the refusal of a kernel constructed without the
// element catalog: no graph member is published until graph enforcement
// exists, so no partially validated graph record can enter a snapshot.
func (b PublicationBatch) graphMembershipError() error {
	if b.graphMembers() {
		return errID(MembershipMissing, "graph member in a kernel without the element catalog")
	}
	return nil
}

// graphMembersError checks the value shapes, bounds and canonical order of the
// graph members of a snapshot. It resolves no reference.
func (s Snapshot) graphMembersError() error {
	errs := []error{
		graphRecordsError(s.Resources, resourceGraphKey),
		graphRecordsError(s.Relations, relationInstanceGraphKey),
		graphRecordsError(s.RelationAbsences, absenceStatementGraphKey),
	}
	if len(s.Resources) > maxSnapshotResources || len(s.Relations) > maxSnapshotRelations || len(s.RelationAbsences) > maxSnapshotAbsences {
		errs = append(errs, errID(BoundsExceeded, "snapshot graph collection"))
	}
	for _, link := range s.IdentityLinks {
		errs = append(errs, link.graphMembersError())
	}
	for _, service := range s.Services {
		errs = append(errs, service.graphMembersError())
	}
	return bestError(errs...)
}

// recordGraphMembersError checks the graph members of a record whose own
// Validate predates them. Batches and snapshots check theirs in Validate.
func recordGraphMembersError(record any) error {
	switch value := record.(type) {
	case IdentityLink:
		return value.graphMembersError()
	case *IdentityLink:
		if value != nil {
			return value.graphMembersError()
		}
	case ServiceInstance:
		return value.graphMembersError()
	case *ServiceInstance:
		if value != nil {
			return value.graphMembersError()
		}
	}
	return nil
}

// graphOptionalMembers lists, per record, the optional graph members that are
// omitted when empty. A present empty collection or an empty structure revision
// would decode to the omitted form and change the canonical bytes.
var graphOptionalMembers = map[reflect.Type][]string{
	reflect.TypeOf(PublicationBatch{}): {"resource_upserts", "relation_upserts", "relation_withdrawals", "absence_upserts", "absence_withdrawals"},
	reflect.TypeOf(Snapshot{}):         {"resources", "relations", "relation_absences"},
	reflect.TypeOf(RevisionVector{}):   {"structure"},
}

func graphPresentEmptyErrors(node jsonNode, t reflect.Type) []error {
	members, ok := graphOptionalMembers[t]
	if !ok || node.kind != 'o' {
		return nil
	}
	var errs []error
	for _, name := range members {
		member, present := nodeMember(node, name)
		if !present {
			continue
		}
		if member.kind == 'a' && len(member.array) == 0 {
			errs = append(errs, errID(InvalidValue, "present empty "+name))
		}
		if value, isString := member.scalar.(string); member.kind == 's' && isString && value == "" {
			errs = append(errs, errID(InvalidIdentifier, "present empty "+name))
		}
	}
	return errs
}
