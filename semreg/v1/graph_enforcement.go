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
const pinnedCatalogDigest Digest = "sha256:14c36bfcf1fafd22149e05f3ad4b11ecc4b8ba54723c1dc16a78f571c2713d17"

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

// graphState is the graph enforcement of a registry, fixed at construction.
// A registry without it is not graph-enforced.
type graphState struct {
	digest       Digest
	inadmissible map[inadmissibleKey]struct{}
}

type inadmissibleKey struct {
	kind DefinitionKind
	ref  DefinitionRef
}

// NewGraphRegistry builds a graph-enforced registry. The catalog must be the
// pinned catalog and must cover every registered pack; otherwise construction
// fails with membership_missing. Validator registration errors are reported
// as NewRegistry reports them. Every operation and effect rule whose catalog
// disposition is not member is recorded as inadmissible.
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
	registry.graph = state
	return registry, nil
}

// NewGraphPublicationKernel is NewPublicationKernel with a graph-enforced
// registry built by NewGraphRegistry. Until the graph publication rules
// exist, it refuses every graph member of a batch with membership_missing,
// like a kernel without the catalog, and publishes every other batch exactly
// as that kernel does.
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

// graphPublicationError refuses the graph members of a batch. A kernel
// without the catalog refuses them as before; a graph-enforced kernel refuses
// them until the graph publication rules exist, so no partially validated
// graph record enters a snapshot.
func (k *PublicationKernel) graphPublicationError(batch PublicationBatch) error {
	if !k.registry.GraphEnforced() {
		return batch.graphMembershipError()
	}
	if batch.graphMembers() {
		return errID(MembershipMissing, "graph member before the graph publication rules")
	}
	return nil
}
