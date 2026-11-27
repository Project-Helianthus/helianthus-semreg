package semreg

import "reflect"

// maxSnapshotResourceLinks bounds the identity links of one asset that carry
// a resource. The bound of maxSnapshotIdentity applies to the links without
// one.
const maxSnapshotResourceLinks = 1024

// identityLinkBoundsExceeded applies both identity-link bounds of an asset.
func identityLinkBoundsExceeded(links []IdentityLink) bool {
	withResource := 0
	for _, link := range links {
		if link.Resource != nil {
			withResource++
		}
	}
	return withResource > maxSnapshotResourceLinks || len(links)-withResource > maxSnapshotIdentity
}

// upsertResources is the resource-upsert phase of a kernel that publishes
// resources. Each upsert is checked against the catalog (RES-1, RES-2) and
// against its previous revision (RES-3, RES-9, the retired part of RES-4 and
// the object revision rule), then staged. Rules that need the complete
// post-state run in checkResourceGraph.
func (s *applyState) upsertResources() {
	catalog := s.registry.graphResources()
	if catalog == nil {
		return
	}
	for _, resource := range s.batch.ResourceUpserts {
		s.check(resourceKindError(catalog, resource))
		s.check(resourceAnchorError(catalog, resource))
		if existing, ok := s.originalResources[resource.ResourceID]; ok {
			s.check(resourceIdentityError(existing, resource))
			_, err := validateRevisionedUpsert(existing, resource)
			s.check(err)
		} else if resource.Supersedes != nil {
			s.check(errID(IdentityDrift, "supersession without a previous revision"))
		}
		s.resources[resource.ResourceID] = resource
	}
}

// resourceKindError is RES-1: the kind is a catalog resource kind with
// disposition member.
func resourceKindError(catalog GraphResourceCatalog, resource Resource) error {
	membership, ok := catalog.ResourceKindMembership(resource.Kind)
	if !ok || membership.Disposition != GraphMember {
		return errID(MembershipMissing, "resource kind is not a catalog member")
	}
	return nil
}

// resourceAnchorError is RES-2: the anchor class is the class the catalog
// declares for the kind, the class's members are present, the basis agrees
// with the origin kind and a native identity names a listed scheme. Scheme
// and key presence against the basis are value shapes of the anchor itself.
func resourceAnchorError(catalog GraphResourceCatalog, resource Resource) error {
	anchor := resource.Anchor
	var errs []error
	if class, ok := catalog.ResourceKindClass(resource.Kind); ok && class != anchor.Class {
		errs = append(errs, errID(InvalidValue, "anchor class differs from the kind's class"))
	}
	switch anchor.Class {
	case AnchorComponent, AnchorEpisode:
		if anchor.Parent == nil || anchor.Slot == nil {
			errs = append(errs, errID(InvalidValue, "anchor parent and slot"))
		}
	case AnchorFunction:
		if anchor.Slot == nil {
			errs = append(errs, errID(InvalidValue, "function anchor slot"))
		}
	case AnchorEquipment:
		if anchor.Basis != AnchorNativeIdentity && anchor.Slot == nil {
			errs = append(errs, errID(InvalidValue, "equipment anchor slot"))
		}
	}
	switch anchor.Basis {
	case AnchorNativeIdentity, AnchorNativeSlot:
		if resource.Origin.Kind != OriginNativeObservation {
			errs = append(errs, errID(InvalidValue, "native anchor without a native origin"))
		}
	case AnchorOperatorSlot:
		if resource.Origin.Kind != OriginOperator {
			errs = append(errs, errID(InvalidValue, "operator anchor without an operator origin"))
		}
	}
	if anchor.Basis == AnchorNativeIdentity && anchor.Scheme != nil && !catalog.SchemeListed(DefinitionID(*anchor.Scheme)) {
		errs = append(errs, errID(InvalidValue, "identification scheme is not listed"))
	}
	return bestError(errs...)
}

// resourceIdentityError compares an upsert with the previous revision of its
// ResourceID: the kind never changes (RES-3), a retired ResourceID is never
// current or withdrawn again (RES-4), and an anchor change is a supersession
// that names exactly the previous anchor and is an allowed upgrade (RES-9).
// Without an anchor change the recorded supersession stays as it is.
func resourceIdentityError(previous, next Resource) error {
	var errs []error
	if previous.Kind != next.Kind {
		errs = append(errs, errID(IdentityDrift, "resource kind changed"))
	}
	if previous.State == ResourceRetired && next.State != ResourceRetired {
		errs = append(errs, errID(DuplicateIdentity, "retired resource is current again"))
	}
	if sameAnchorIdentity(previous.Anchor, next.Anchor) {
		if !reflect.DeepEqual(previous.Supersedes, next.Supersedes) {
			errs = append(errs, errID(IdentityDrift, "supersession without an anchor change"))
		}
		return bestError(errs...)
	}
	switch {
	case next.Supersedes == nil:
		errs = append(errs, errID(IdentityDrift, "anchor change without a supersession"))
	case !reflect.DeepEqual(next.Supersedes.Previous, previous.Anchor):
		errs = append(errs, errID(IdentityDrift, "supersession does not name the previous anchor"))
	case !allowedAnchorUpgrade(previous.Anchor, next.Anchor):
		errs = append(errs, errID(IdentityDrift, "anchor change is not an allowed upgrade"))
	}
	return bestError(errs...)
}

// sameAnchorIdentity compares every anchor member except its evidence.
func sameAnchorIdentity(a, b ResourceAnchor) bool {
	return a.Class == b.Class && a.Basis == b.Basis && reflect.DeepEqual(a.Scheme, b.Scheme) && reflect.DeepEqual(a.Key, b.Key) && reflect.DeepEqual(a.Parent, b.Parent) && reflect.DeepEqual(a.Slot, b.Slot)
}

// allowedAnchorUpgrade lists the upgrades of section 6.5: an operator slot
// to a native slot or identity, a native slot to a native identity, and a
// native identity to a native identity of the same scheme. The class never
// changes.
func allowedAnchorUpgrade(previous, next ResourceAnchor) bool {
	if previous.Class != next.Class {
		return false
	}
	switch previous.Basis {
	case AnchorOperatorSlot:
		return next.Basis == AnchorNativeSlot || next.Basis == AnchorNativeIdentity
	case AnchorNativeSlot:
		return next.Basis == AnchorNativeIdentity
	case AnchorNativeIdentity:
		return next.Basis == AnchorNativeIdentity && previous.Scheme != nil && next.Scheme != nil && *previous.Scheme == *next.Scheme
	}
	return false
}

// anchorKey is the anchor key of a resource: (kind, key) for a native
// identity and (kind, parent, slot) otherwise. A NUL separator cannot occur
// in an identifier, a digest or slot text.
func anchorKey(resource Resource) string {
	anchor := resource.Anchor
	if anchor.Basis == AnchorNativeIdentity {
		key := ""
		if anchor.Key != nil {
			key = string(*anchor.Key)
		}
		return string(resource.Kind) + "\x00key\x00" + key
	}
	parent, slot := "", ""
	if anchor.Parent != nil {
		parent = string(*anchor.Parent)
	}
	if anchor.Slot != nil {
		slot = *anchor.Slot
	}
	return string(resource.Kind) + "\x00slot\x00" + parent + "\x00" + slot
}

// checkResourceGraph runs the resource rules that read the complete
// post-state, in the order of the publication phases: service resources,
// fact subjects, identity-link resources and keys, anchor-key uniqueness and
// origin resolution. Withdrawal and retirement (RES-8) need no rule of their
// own: every current fact and service must still name a current resource.
func (s *applyState) checkResourceGraph() {
	catalog := s.registry.graphResources()
	if catalog == nil {
		return
	}
	s.checkServiceResources(catalog)
	s.checkFactSubjects(catalog)
	s.checkIdentityLinkResources()
	s.checkAnchorKeys()
	s.checkResourceOrigins()
}

// resourceSubjectError is RES-5 for one reference: the resource exists and is
// current (dangling_reference otherwise) and has the declared kind
// (identity_drift otherwise).
func (s *applyState) resourceClassError(catalog GraphResourceCatalog, classes []AnchorClass, id ResourceID, detail string) error {
	resource, ok := s.resources[id]
	if !ok || resource.State != ResourceCurrent {
		return errID(DanglingReference, detail)
	}
	class, ok := catalog.ResourceKindClass(resource.Kind)
	if !ok || !containsAnchor(classes, class) {
		return errID(IdentityDrift, detail+" class")
	}
	return nil
}

func containsAnchor(classes []AnchorClass, class AnchorClass) bool {
	for _, candidate := range classes {
		if candidate == class {
			return true
		}
	}
	return false
}

func (s *applyState) resourceSubjectError(kind DefinitionID, id ResourceID, detail string) error {
	resource, ok := s.resources[id]
	if !ok || resource.State != ResourceCurrent {
		return errID(DanglingReference, detail)
	}
	if resource.Kind != kind {
		return errID(IdentityDrift, detail+" kind")
	}
	return nil
}

// checkServiceResources applies section 6.2 and PLN-7 to every service and
// capability that is not withdrawn: its definition is a catalog member, and a
// service names a current resource of the kind the catalog declares for it.
func (s *applyState) checkServiceResources(catalog GraphResourceCatalog) {
	for _, service := range s.services {
		if service.Availability == AvailabilityWithdrawn {
			continue
		}
		if membership, ok := catalog.Membership(service.Definition); !ok || membership.Disposition != GraphMember {
			s.check(errID(MembershipMissing, "service is not a catalog member"))
			continue
		}
		kind, classes, ok := catalog.ServiceSubject(service.Definition)
		if !ok {
			s.check(errID(MembershipMissing, "service without a subject kind"))
			continue
		}
		if service.Resource == nil {
			s.check(errID(DanglingReference, "service without its resource"))
			continue
		}
		if kind != "" {
			s.check(s.resourceSubjectError(kind, *service.Resource, "service resource"))
			continue
		}
		s.check(s.resourceClassError(catalog, classes, *service.Resource, "service resource"))
	}
	for _, capability := range s.capabilities {
		if capability.Availability == AvailabilityWithdrawn {
			continue
		}
		if membership, ok := catalog.Membership(capability.Definition); !ok || membership.Disposition != GraphMember {
			s.check(errID(MembershipMissing, "capability is not a catalog member"))
		}
	}
}

// checkFactSubjects applies RES-5, RES-6 and PLN-7 to every current fact
// candidate. A fact of an unsupported definition is not published; a fact of
// an unknown definition names no resource and stays publishable.
func (s *applyState) checkFactSubjects(catalog GraphResourceCatalog) {
	for _, candidate := range s.candidates {
		key := candidate.Key
		pack := PackRef{ID: key.PackID, Version: key.PackVersion}
		subject, ok := catalog.FactSubject(pack, key.FactID)
		if !ok {
			s.check(errID(MembershipMissing, "fact without a catalog entry"))
			continue
		}
		if subject.Disposition == GraphUnsupported {
			s.check(errID(MembershipMissing, "fact of an unsupported definition"))
			continue
		}
		var subjectID *ResourceID
		for _, dimension := range key.Dimensions {
			role, ok := catalog.PackDimension(pack, dimension.ID)
			if !ok {
				s.check(errID(MembershipMissing, "dimension without a catalog entry"))
				continue
			}
			if !role.Resource {
				continue
			}
			if dimension.Value.Kind != ValueText || dimension.Value.Text == nil || ResourceID(*dimension.Value.Text).Validate() != nil {
				s.check(errID(InvalidValue, "resource dimension value is not a resource identifier"))
				continue
			}
			id := ResourceID(*dimension.Value.Text)
			if role.Kind != "" {
				s.check(s.resourceSubjectError(role.Kind, id, "fact resource dimension"))
			} else if len(role.Classes) > 0 {
				s.check(s.resourceClassError(catalog, role.Classes, id, "fact resource dimension"))
			} else {
				s.check(errID(IdentityDrift, "fact resource dimension"))
			}
			if subject.Resolved && dimension.ID == subject.Dimension {
				subjectID = &id
			}
		}
		if subject.Resolved && subjectID == nil {
			s.check(errID(InvalidValue, "fact without its resource dimension"))
			continue
		}
		if subjectID != nil && candidate.observedPath() {
			s.check(s.identityQualificationError(candidate, *subjectID))
		}
	}
}

// identityQualificationError is RES-6 for one observed fact about subject.
func (s *applyState) identityQualificationError(candidate FactCandidate, subject ResourceID) error {
	if candidate.BindingID == nil {
		return errID(IdentityNotQualified, "observed fact without a binding")
	}
	link, ok := s.links[*candidate.BindingID]
	if !ok || link.State == LinkWithdrawn || link.Resource == nil || !s.inAnchorChain(subject, *link.Resource) {
		return errID(IdentityNotQualified, "binding does not observe the fact's subject")
	}
	if candidate.Quality.Promotion == PromotionPromoted && link.State != LinkQualified {
		return errID(IdentityNotQualified, "promoted fact through an unqualified link")
	}
	return nil
}

// inAnchorChain reports whether named is subject or one of its anchor parents
// up to the first equipment or function resource.
func (s *applyState) inAnchorChain(subject, named ResourceID) bool {
	current := subject
	for steps := 0; steps <= len(s.resources); steps++ {
		if current == named {
			return true
		}
		resource, ok := s.resources[current]
		if !ok || resource.Anchor.Class == AnchorEquipment || resource.Anchor.Class == AnchorFunction || resource.Anchor.Parent == nil {
			return false
		}
		current = *resource.Anchor.Parent
	}
	return false
}

// checkIdentityLinkResources applies RES-7 to every identity link that is not
// withdrawn and names a resource: the resource exists, and a link that
// carries a key carries the anchor key of a natively anchored resource.
func (s *applyState) checkIdentityLinkResources() {
	for _, link := range s.links {
		if link.State == LinkWithdrawn || link.Resource == nil {
			continue
		}
		resource, ok := s.resources[*link.Resource]
		if !ok {
			s.check(errID(DanglingReference, "identity link resource"))
			continue
		}
		if link.Key != nil && resource.Anchor.Basis == AnchorNativeIdentity && resource.Anchor.Key != nil && *link.Key != *resource.Anchor.Key {
			s.check(errID(IdentityNotQualified, "identity link key differs from the anchor key"))
		}
	}
}

// checkAnchorKeys is RES-4 and the last clause of RES-9: no two current
// resources share an anchor key.
func (s *applyState) checkAnchorKeys() {
	holders := make(map[string]ResourceID, len(s.resources))
	for id, resource := range s.resources {
		if resource.State != ResourceCurrent {
			continue
		}
		key := anchorKey(resource)
		if other, held := holders[key]; held && other != id {
			s.check(errID(DuplicateIdentity, "anchor key held by two current resources"))
			continue
		}
		holders[key] = id
	}
}

// checkResourceOrigins is PUB-3 for every upsert: a native origin names a
// binding of the home asset, current or tombstoned, of the origin's source
// and epoch. An operator origin needs no binding.
func (s *applyState) checkResourceOrigins() {
	for _, resource := range s.batch.ResourceUpserts {
		origin := resource.Origin
		if origin.Kind != OriginNativeObservation || origin.BindingID == nil || origin.SourceID == nil || origin.SourceEpochID == nil {
			continue
		}
		binding, ok := s.bindings[*origin.BindingID]
		if !ok || binding.SourceID != *origin.SourceID || binding.SourceEpochID != *origin.SourceEpochID {
			s.check(errID(DanglingReference, "resource origin binding"))
		}
	}
}
