package semreg

import (
	"reflect"
	"sort"
	"strings"
)

// pinnedCatalogDigest is the SHA-256 of fixtures/v1/element-catalog-v1.json,
// the byte copy of the accepted element catalog. A graph-enforced registry
// accepts no catalog with another digest. It moves together with every
// in-place edit of the catalog copy.
const pinnedCatalogDigest Digest = "sha256:17a741942cfa778fabc5e4eb6e79aa69a7557df17cf578924768445def86f4e8"

// GraphDisposition is the catalog disposition of an element.
type GraphDisposition string

// GraphDisposition values. Only GraphMember places an element in a plane.
const (
	GraphMember      GraphDisposition = "member"
	GraphUnsupported GraphDisposition = "unsupported"
	GraphUnknown     GraphDisposition = "unknown"
)

// GraphPlane names one plane of the element catalog.
type GraphPlane string

// GraphMembership is the disposition and the planes of one catalog entry.
type GraphMembership struct {
	Disposition GraphDisposition
	Planes      []GraphPlane
}

// GraphRelationDefinition holds the catalog attributes of a relation
// definition that the publication rules read: its core type, the resource
// kinds admitted at each end, its cardinality, whether it holds over time,
// the medium of a supplies definition (empty otherwise), the admitted basis
// classes and its membership.
type GraphRelationDefinition struct {
	CoreType     string
	FromKinds    []DefinitionID
	ToKinds      []DefinitionID
	Cardinality  string
	OverTime     bool
	Medium       string
	BasisClasses []RelationBasis
	Membership   GraphMembership
}

// GraphCatalog is the element catalog as the root package reads it. The
// package semreg/v1/graph implements it; the root package never imports that
// package. Every lookup returns a detached copy and reports false for a key
// the catalog does not list.
//
// Covers applies the completeness rule of the catalog to one registered pack:
// every definition of the index has an entry of its kind, and every entry of
// those kinds owned by the pack is declared by the index. A non-nil result
// means the pack is not covered.
type GraphCatalog interface {
	Digest() Digest
	Covers(index DefinitionIndex) error
	Membership(ref DefinitionRef) (GraphMembership, bool)
	ResourceKindMembership(kind DefinitionID) (GraphMembership, bool)
	DimensionKind(ref DefinitionRef) (DefinitionID, bool)
	ServiceSubjectKind(ref DefinitionRef) (DefinitionID, bool)
	RelationDefinition(ref DefinitionRef) (GraphRelationDefinition, bool)
	SchemeListed(id DefinitionID) bool
}

// GraphFactSubject is the subject a catalog declares for one fact definition,
// with the definition's disposition. A resolved subject names its resource
// dimension and that dimension's resource kind. Qualifiers lists the
// qualifier dimensions declared for the field; an unresolved subject names no
// resource dimension.
type GraphFactSubject struct {
	Disposition GraphDisposition
	Resolved    bool
	Dimension   DefinitionID
	Kind        DefinitionID
	Qualifiers  []DefinitionID
}

// GraphDimension is the role of a pack dimension: a resource dimension of a
// resource kind, or a qualifier dimension (Resource false, Kind empty).
type GraphDimension struct {
	Resource bool
	Kind     DefinitionID
}

// GraphResourceCatalog is a GraphCatalog with the lookups the resource
// publication rules read: the anchor class of a resource kind, the subject of
// a fact and the role of a dimension. A fact key and its dimensions carry no
// definition version, so the last two are keyed by the pack and the
// identifier; a catalog answers false unless exactly one entry of the pack
// has that identifier. A graph-enforced registry whose catalog lacks these
// lookups refuses every graph member with membership_missing.
type GraphResourceCatalog interface {
	GraphCatalog
	ResourceKindClass(kind DefinitionID) (AnchorClass, bool)
	FactSubject(pack PackRef, fact DefinitionID) (GraphFactSubject, bool)
	PackDimension(pack PackRef, dimension DefinitionID) (GraphDimension, bool)
}

// graphState is the graph enforcement of a registry, fixed at construction.
// A registry without it is not graph-enforced. resources is the catalog when
// it provides the resource lookups, nil otherwise.
type graphState struct {
	digest       Digest
	inadmissible map[inadmissibleKey]struct{}
	resources    GraphResourceCatalog
}

type inadmissibleKey struct {
	kind DefinitionKind
	ref  DefinitionRef
}

// NewGraphRegistry builds a graph-enforced registry. The catalog must be the
// pinned catalog and must cover every registered pack; otherwise construction
// fails with membership_missing. Validator registration errors are reported
// as NewRegistry reports them. Every operation and effect rule whose catalog
// disposition is not member is recorded as inadmissible. When the catalog
// provides the resource lookups, the registry keeps it for the resource
// publication rules and passes each fact's declared subject to the pack
// validators.
func NewGraphRegistry(catalog GraphCatalog, validators ...PackValidator) (*Registry, error) {
	if catalog == nil || reflect.ValueOf(catalog).Kind() == reflect.Ptr && reflect.ValueOf(catalog).IsNil() {
		return nil, errID(MembershipMissing, "graph-enforced registry without the element catalog")
	}
	digest := catalog.Digest()
	if digest != pinnedCatalogDigest {
		return nil, errID(MembershipMissing, "element catalog is not the pinned copy")
	}
	registry, err := NewRegistry(validators...)
	if err != nil {
		return nil, err
	}
	packs := make([]PackRef, 0, len(registry.indexes))
	for pack := range registry.indexes {
		packs = append(packs, pack)
	}
	sort.Slice(packs, func(i, j int) bool {
		if c := strings.Compare(string(packs[i].ID), string(packs[j].ID)); c != 0 {
			return c < 0
		}
		return strings.Compare(string(packs[i].Version), string(packs[j].Version)) < 0
	})
	state := &graphState{digest: digest, inadmissible: map[inadmissibleKey]struct{}{}}
	if resources, ok := catalog.(GraphResourceCatalog); ok {
		state.resources = resources
	}
	for _, pack := range packs {
		index := registry.indexes[pack]
		if err := catalog.Covers(cloneIndex(index)); err != nil {
			return nil, errID(MembershipMissing, "element catalog does not cover pack "+string(pack.ID)+"@"+string(pack.Version)+": "+err.Error())
		}
		for _, group := range []struct {
			kind DefinitionKind
			refs []DefinitionRef
		}{{DefinitionOperation, index.Operations}, {DefinitionEffectRule, index.EffectRules}} {
			for _, ref := range group.refs {
				membership, ok := catalog.Membership(ref)
				if !ok {
					return nil, errID(MembershipMissing, "element catalog has no entry for "+string(group.kind)+" "+string(ref.ID))
				}
				if membership.Disposition != GraphMember {
					state.inadmissible[inadmissibleKey{group.kind, ref}] = struct{}{}
				}
			}
		}
	}
	if state.resources != nil {
		for pack, validator := range registry.validators {
			if frozen, ok := validator.(frozenValidator); ok {
				frozen.subjects = state.resources
				registry.validators[pack] = frozen
			}
		}
	}
	registry.graph = state
	return registry, nil
}

// NewGraphPublicationKernel is NewPublicationKernel with a graph-enforced
// registry built by NewGraphRegistry. With a catalog that provides the
// resource lookups it publishes resources, the resource members of identity
// links and services, and fact subjects under the resource rules; it refuses
// relation instances and absence statements with membership_missing until
// their publication rules exist. With any other catalog it refuses every
// graph member with membership_missing. A batch without graph members whose
// facts, services and capabilities need no resource is published exactly as
// a kernel without the catalog publishes it.
func NewGraphPublicationKernel(asset AssetID, catalog GraphCatalog, validators ...PackValidator) (*PublicationKernel, error) {
	if err := asset.Validate(); err != nil {
		return nil, err
	}
	registry, err := NewGraphRegistry(catalog, validators...)
	if err != nil {
		return nil, err
	}
	return newPublicationKernel(asset, registry), nil
}

// GraphEnforced reports whether the registry was built with the element
// catalog.
func (r *Registry) GraphEnforced() bool { return r != nil && r.graph != nil }

// CatalogDigest returns the digest of the element catalog of a graph-enforced
// registry, for the release bill of materials, and false for any other
// registry.
func (r *Registry) CatalogDigest() (Digest, bool) {
	if !r.GraphEnforced() {
		return "", false
	}
	return r.graph.digest, true
}

// GraphAdmissionError is the one admission lookup of a graph-enforced
// registry: membership_missing when the operation or the effect rule was
// recorded as inadmissible at construction, nil otherwise. It reads only the
// registry's own index and never the catalog or a graph record. A registry
// without the catalog admits every operation as before.
func (r *Registry) GraphAdmissionError(operation, effectRule DefinitionRef) error {
	if r == nil {
		return errID(DefinitionOwnerMissing, "registry")
	}
	if r.graph == nil {
		return nil
	}
	if _, ok := r.graph.inadmissible[inadmissibleKey{DefinitionOperation, operation}]; ok {
		return errID(MembershipMissing, "operation is not a catalog member")
	}
	if _, ok := r.graph.inadmissible[inadmissibleKey{DefinitionEffectRule, effectRule}]; ok {
		return errID(MembershipMissing, "effect rule is not a catalog member")
	}
	return nil
}

// graphPublicationError refuses the graph members that no publication rule
// covers yet. A kernel without the catalog refuses every graph member as
// before. A graph-enforced kernel refuses relation instances and absence
// statements until their rules exist, and every graph member when its
// catalog has no resource lookups, so no partially validated graph record
// enters a snapshot.
func (k *PublicationKernel) graphPublicationError(batch PublicationBatch) error {
	if !k.registry.GraphEnforced() {
		return batch.graphMembershipError()
	}
	if k.registry.graph.resources == nil {
		if batch.graphMembers() {
			return errID(MembershipMissing, "graph member without the resource lookups of the catalog")
		}
		return nil
	}
	if len(batch.RelationUpserts) != 0 || len(batch.RelationWithdrawals) != 0 || len(batch.AbsenceUpserts) != 0 || len(batch.AbsenceWithdrawals) != 0 {
		return errID(MembershipMissing, "relation instance or absence statement before their publication rules")
	}
	return nil
}

// graphResources returns the catalog of a registry that publishes resources,
// nil for every other registry.
func (r *Registry) graphResources() GraphResourceCatalog {
	if r == nil || r.graph == nil {
		return nil
	}
	return r.graph.resources
}
