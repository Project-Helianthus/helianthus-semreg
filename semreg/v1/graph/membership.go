package graph

import semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"

// Plane membership is derived from the catalog and the cut, never published.
// Only kept members contribute. A relation instance is in the planes of its
// definition when it is qualified and coherent (PLN-2, COH-1). A fact,
// service or capability instance is in the planes of its definition when
// its definition is a member and its subject is current in its member
// (PLN-2); a capability's subject is its service's. A current resource is in
// the planes its kind admits in which it is physical equipment or a
// component, the subject of an instance in that plane, or an endpoint of a
// relation instance in that plane (PLN-3). A resource that is not current
// is in no plane, and an ungraphed member holds no subject, so its facts
// are in no plane.

// PlanesOfResource returns the planes of a resource in catalog plane order;
// nil when it is in none or not current in a kept member.
func (c *Cut) PlanesOfResource(id semreg.ResourceID) []PlaneID {
	if c == nil {
		return nil
	}
	return append([]PlaneID(nil), c.resourcePlanes[id]...)
}

// PlanesOfRelation returns the planes of a relation instance held by a kept
// member; nil when it is not a current edge.
func (c *Cut) PlanesOfRelation(key semreg.RelationKey) []PlaneID {
	if c == nil {
		return nil
	}
	return append([]PlaneID(nil), c.relationPlanes[relationLookupKey(canonicalKey(key.Definition), key.From, key.To)]...)
}

// PlanesOfFact returns the planes of the fact with the exact key held by the
// asset; nil when the asset is not a kept member, holds no such fact, or the
// fact is in no plane.
func (c *Cut) PlanesOfFact(asset semreg.AssetID, key semreg.FactKey) []PlaneID {
	if c == nil {
		return nil
	}
	canonical := canonicalKey(key)
	if canonical == "" {
		return nil
	}
	return append([]PlaneID(nil), c.factPlanes[factLookupKey(asset, canonical)]...)
}

func factLookupKey(asset semreg.AssetID, canonicalFactKey string) string {
	return string(asset) + "\x00" + canonicalFactKey
}

// planeSet collects planes and returns them in catalog plane order.
type planeSet map[PlaneID]struct{}

func (s planeSet) add(planes []PlaneID) {
	for _, plane := range planes {
		s[plane] = struct{}{}
	}
}

func (a *assembly) ordered(set planeSet) []PlaneID {
	var out []PlaneID
	for _, plane := range a.catalog.planes {
		if _, ok := set[plane.ID]; ok {
			out = append(out, plane.ID)
		}
	}
	return out
}

// memberPlanes returns the planes of a pack entry that is a member.
func (a *assembly) memberPlanes(ref semreg.DefinitionRef) []PlaneID {
	at, ok := a.catalog.byRef[PackElement(ref)]
	if !ok || a.catalog.elements[at].Disposition != DispositionMember {
		return nil
	}
	return a.catalog.elements[at].Planes
}

func currentIn(member *cutMember, id semreg.ResourceID) bool {
	resource, ok := member.resources[id]
	return ok && resource.State == semreg.ResourceCurrent
}

// membership derives every plane answer of the cut once, so the cut keeps no
// reference to its input.
func (a *assembly) membership(cut *Cut) {
	cut.relationPlanes = map[string][]PlaneID{}
	cut.factPlanes = map[string][]PlaneID{}
	cut.resourcePlanes = map[semreg.ResourceID][]PlaneID{}
	subjects := map[semreg.ResourceID]planeSet{}
	contribute := func(id semreg.ResourceID, planes []PlaneID) {
		if len(planes) == 0 {
			return
		}
		if subjects[id] == nil {
			subjects[id] = planeSet{}
		}
		subjects[id].add(planes)
	}
	for _, relation := range a.relations {
		if relation.definition == nil || !relation.edge() || len(relation.definition.Planes) == 0 {
			continue
		}
		planes := relation.definition.Planes
		set := planeSet{}
		set.add(cut.relationPlanes[relation.key])
		set.add(planes)
		cut.relationPlanes[relation.key] = a.ordered(set)
		contribute(relation.instance.From, planes)
		contribute(relation.instance.To, planes)
	}
	for _, member := range a.kept() {
		a.factMembership(cut, member, contribute)
		a.serviceMembership(member, contribute)
	}
	for _, member := range a.kept() {
		for _, resource := range member.snapshot.Resources {
			if resource.State != semreg.ResourceCurrent {
				continue
			}
			kind, ok := a.catalog.CoreEntry(resource.Kind)
			if !ok || kind.ElementKind != KindResourceKind || kind.Disposition != DispositionMember {
				continue
			}
			set := planeSet{}
			for _, plane := range kind.Planes {
				_, subject := subjects[resource.ResourceID][plane]
				physical := plane == PlanePhysical && (resource.Anchor.Class == semreg.AnchorEquipment || resource.Anchor.Class == semreg.AnchorComponent)
				if physical || subject {
					set[plane] = struct{}{}
				}
			}
			if planes := a.ordered(set); len(planes) != 0 {
				cut.resourcePlanes[resource.ResourceID] = planes
			}
		}
	}
}

// factMembership places every fact of a member definition whose subject, the
// value of its resource dimension, is current in the member.
func (a *assembly) factMembership(cut *Cut, member *cutMember, contribute func(semreg.ResourceID, []PlaneID)) {
	for _, envelope := range member.snapshot.Facts {
		key := envelope.Key
		element, ok := a.catalog.packEntryOfKind(semreg.PackRef{ID: key.PackID, Version: key.PackVersion}, key.FactID, KindFact)
		if !ok || element.Disposition != DispositionMember || element.Fact == nil || element.Fact.Subject.Status != SubjectResolved || element.Fact.Subject.Dimension == nil {
			continue
		}
		subject, ok := subjectOf(key, element.Fact.Subject.Dimension.ID)
		if !ok || !currentIn(member, subject) || len(element.Planes) == 0 {
			continue
		}
		canonical := canonicalKey(key)
		if canonical == "" {
			continue
		}
		cut.factPlanes[factLookupKey(member.asset, canonical)] = append([]PlaneID(nil), element.Planes...)
		contribute(subject, element.Planes)
	}
}

// subjectOf reads the ResourceID a fact key carries in its resource
// dimension.
func subjectOf(key semreg.FactKey, dimension semreg.DefinitionID) (semreg.ResourceID, bool) {
	for _, candidate := range key.Dimensions {
		if candidate.ID != dimension {
			continue
		}
		value := candidate.Value
		if value.Kind != semreg.ValueText || value.Text == nil {
			return "", false
		}
		id := semreg.ResourceID(*value.Text)
		return id, id.Validate() == nil
	}
	return "", false
}

// serviceMembership places every service and capability that is not
// withdrawn, of a member definition, whose service resource is current in
// the member.
func (a *assembly) serviceMembership(member *cutMember, contribute func(semreg.ResourceID, []PlaneID)) {
	services := make(map[semreg.ServiceInstanceID]semreg.ServiceInstance, len(member.snapshot.Services))
	for _, service := range member.snapshot.Services {
		if service.Availability == semreg.AvailabilityWithdrawn || service.Resource == nil || !currentIn(member, *service.Resource) {
			continue
		}
		services[service.InstanceID] = service
		contribute(*service.Resource, a.memberPlanes(service.Definition))
	}
	for _, capability := range member.snapshot.Capabilities {
		service, ok := services[capability.ServiceInstance]
		if !ok || capability.Availability == semreg.AvailabilityWithdrawn {
			continue
		}
		contribute(*service.Resource, a.memberPlanes(capability.Definition))
	}
}
