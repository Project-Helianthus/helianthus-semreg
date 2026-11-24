package graph

import (
	_ "embed"
	"sync"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// A Catalog is the element catalog the root package reads through its own
// interface, including the lookups of the resource publication rules.
var _ semreg.GraphResourceCatalog = (*Catalog)(nil)

var embedded struct {
	once    sync.Once
	catalog *Catalog
	err     error
}

// Embedded returns the catalog compiled into this package: the bytes of
// semreg/v1/graph/embedded/element-catalog-v1.json, accepted by Load only when their
// SHA-256 is PinnedDigest and they pass every catalog rule against the
// accepted packs. The catalog is loaded once and is immutable.
func Embedded() (*Catalog, error) {
	embedded.once.Do(func() {
		embedded.catalog, embedded.err = Load([]byte(embeddedCatalog))
	})
	return embedded.catalog, embedded.err
}

// Enforced builds a graph-enforced registry with the embedded catalog. Every
// registered pack must be covered by the catalog (CAT-1 for that pack),
// otherwise construction fails with membership_missing; every operation and
// effect rule whose disposition is not member is recorded as inadmissible.
func Enforced(validators ...semreg.PackValidator) (*semreg.Registry, error) {
	catalog, err := Embedded()
	if err != nil {
		return nil, err
	}
	return semreg.NewGraphRegistry(catalog, validators...)
}

// Covers applies CAT-1 to one registered pack: every definition of the index
// has an entry of its kind, every fact, service, capability, operation and
// effect-rule entry owned by the pack is declared by the index, and the
// catalog lists at least one entry of the pack. Any other index is
// membership_missing.
func (c *Catalog) Covers(index semreg.DefinitionIndex) error {
	if c == nil {
		return newError(semreg.MembershipMissing, "nil catalog")
	}
	pack := index.Pack
	declared := map[semreg.DefinitionRef]struct{}{}
	for _, group := range []struct {
		kind ElementKind
		refs []semreg.DefinitionRef
	}{
		{KindFact, index.Fields}, {KindService, index.Services}, {KindCapability, index.Capabilities},
		{KindOperation, index.Operations}, {KindEffectRule, index.EffectRules},
	} {
		for _, ref := range group.refs {
			at, ok := c.byRef[PackElement(ref)]
			if ref.Pack != pack || !ok {
				return errorf(semreg.MembershipMissing, "%s %s has no entry", group.kind, PackElement(ref))
			}
			if c.elements[at].ElementKind != group.kind {
				return errorf(semreg.MembershipMissing, "%s %s is a %s entry", group.kind, PackElement(ref), c.elements[at].ElementKind)
			}
			declared[ref] = struct{}{}
		}
	}
	listed := false
	for _, element := range c.elements {
		if element.Ref.Owner.Kind != OwnerPack || element.Ref.Owner.Pack != pack {
			continue
		}
		listed = true
		switch element.ElementKind {
		case KindFact, KindService, KindCapability, KindOperation, KindEffectRule:
			ref, _ := element.Ref.DefinitionRef()
			if _, ok := declared[ref]; !ok {
				return errorf(semreg.MembershipMissing, "entry %s is not declared by the registered pack", element.Ref)
			}
		}
	}
	if !listed {
		return errorf(semreg.MembershipMissing, "pack %s@%s has no entry", pack.ID, pack.Version)
	}
	return nil
}

// Membership returns the disposition and planes of a pack-owned entry.
func (c *Catalog) Membership(ref semreg.DefinitionRef) (semreg.GraphMembership, bool) {
	if c == nil {
		return semreg.GraphMembership{}, false
	}
	at, ok := c.byRef[PackElement(ref)]
	if !ok {
		return semreg.GraphMembership{}, false
	}
	return membershipOf(c.elements[at]), true
}

// ResourceKindMembership returns the disposition and planes of the core
// resource kind with the identifier.
func (c *Catalog) ResourceKindMembership(kind semreg.DefinitionID) (semreg.GraphMembership, bool) {
	if c == nil {
		return semreg.GraphMembership{}, false
	}
	at := c.coreByID[kind]
	if len(at) != 1 || c.elements[at[0]].ElementKind != KindResourceKind {
		return semreg.GraphMembership{}, false
	}
	return membershipOf(c.elements[at[0]]), true
}

// RelationDefinition returns the attributes and membership of a pack-owned
// relation definition.
func (c *Catalog) RelationDefinition(ref semreg.DefinitionRef) (semreg.GraphRelationDefinition, bool) {
	if c == nil {
		return semreg.GraphRelationDefinition{}, false
	}
	at, ok := c.byRef[PackElement(ref)]
	if !ok || c.elements[at].Relation == nil {
		return semreg.GraphRelationDefinition{}, false
	}
	element := c.elements[at]
	relation := element.Relation
	return semreg.GraphRelationDefinition{
		CoreType:     string(relation.CoreType),
		FromKinds:    append([]semreg.DefinitionID(nil), relation.FromKinds...),
		ToKinds:      append([]semreg.DefinitionID(nil), relation.ToKinds...),
		Cardinality:  string(relation.Cardinality),
		OverTime:     relation.OverTime,
		Medium:       string(relation.Medium),
		BasisClasses: append([]semreg.RelationBasis(nil), relation.BasisClasses...),
		Membership:   membershipOf(element),
	}, true
}

// SchemeListed reports whether the identification scheme is listed.
func (c *Catalog) SchemeListed(id semreg.DefinitionID) bool {
	if c == nil {
		return false
	}
	for _, scheme := range c.schemes {
		if scheme.ID == id {
			return true
		}
	}
	return false
}

// ResourceKindClass returns the anchor class the catalog declares for the
// core resource kind with the identifier.
func (c *Catalog) ResourceKindClass(kind semreg.DefinitionID) (semreg.AnchorClass, bool) {
	if c == nil {
		return "", false
	}
	at := c.coreByID[kind]
	if len(at) != 1 || c.elements[at[0]].ResourceKind == nil {
		return "", false
	}
	return c.elements[at[0]].ResourceKind.AnchorClass, true
}

// FactSubject returns the subject and disposition of the one fact entry of
// the pack with the identifier: a resolved subject names its resource
// dimension and kind, an unresolved one its qualifier dimension when the
// entry declares it. Qualifiers lists that dimension and every qualifier
// dimension the fact names, as a detached copy.
func (c *Catalog) FactSubject(pack semreg.PackRef, fact semreg.DefinitionID) (semreg.GraphFactSubject, bool) {
	element, ok := c.packEntryOfKind(pack, fact, KindFact)
	if !ok || element.Fact == nil {
		return semreg.GraphFactSubject{}, false
	}
	subject := element.Fact.Subject
	result := semreg.GraphFactSubject{Disposition: semreg.GraphDisposition(element.Disposition), Resolved: subject.Status == SubjectResolved}
	if result.Resolved {
		if subject.Dimension == nil {
			return semreg.GraphFactSubject{}, false
		}
		result.Dimension, result.Kind = subject.Dimension.ID, subject.Kind
	}
	qualifiers := make([]semreg.DefinitionID, 0, len(element.Fact.Qualifiers)+1)
	for _, qualifier := range element.Fact.Qualifiers {
		qualifiers = append(qualifiers, qualifier.ID)
	}
	if subject.QualifierDimension != nil {
		qualifiers = append(qualifiers, subject.QualifierDimension.ID)
	}
	if len(qualifiers) > 0 {
		result.Qualifiers = qualifiers
	}
	return result, true
}

// PackDimension returns the role of the one dimension entry of the pack with
// the identifier, and the resource kind of a resource dimension.
func (c *Catalog) PackDimension(pack semreg.PackRef, dimension semreg.DefinitionID) (semreg.GraphDimension, bool) {
	element, ok := c.packEntryOfKind(pack, dimension, KindDimension)
	if !ok || element.Dimension == nil {
		return semreg.GraphDimension{}, false
	}
	if element.Dimension.Role != RoleResource {
		return semreg.GraphDimension{}, true
	}
	return semreg.GraphDimension{Resource: true, Kind: element.Dimension.ResourceKind}, true
}

// packEntryOfKind returns the pack's entry with the identifier when exactly
// one entry of the pack has it and it is of the kind.
func (c *Catalog) packEntryOfKind(pack semreg.PackRef, id semreg.DefinitionID, kind ElementKind) (Element, bool) {
	if c == nil {
		return Element{}, false
	}
	at := c.byPackID[packIDKey{pack, id}]
	if len(at) != 1 || c.elements[at[0]].ElementKind != kind {
		return Element{}, false
	}
	return c.elements[at[0]], true
}

func membershipOf(element Element) semreg.GraphMembership {
	planes := make([]semreg.GraphPlane, len(element.Planes))
	for i, plane := range element.Planes {
		planes[i] = semreg.GraphPlane(plane)
	}
	return semreg.GraphMembership{Disposition: semreg.GraphDisposition(element.Disposition), Planes: planes}
}

//go:embed embedded/element-catalog-v1.json
var embeddedCatalog string
