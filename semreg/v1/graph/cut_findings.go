package graph

import (
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// excludeDuplicateIdentities reports every ResourceID current in two
// members, or retired in one and current in another, and excludes every
// member holding it current or retired. Neither member is chosen. A
// withdrawn record takes no part: a resource moving home is withdrawn in
// the old home and current in the new one.
func (a *assembly) excludeDuplicateIdentities() {
	holders := map[semreg.ResourceID][]*cutMember{}
	for _, member := range a.members {
		for _, resource := range member.snapshot.Resources {
			if resource.State == semreg.ResourceCurrent || resource.State == semreg.ResourceRetired {
				holders[resource.ResourceID] = append(holders[resource.ResourceID], member)
			}
		}
	}
	for id, members := range holders {
		current := 0
		for _, member := range members {
			if member.resources[id].State == semreg.ResourceCurrent {
				current++
			}
		}
		if len(members) < 2 || current == 0 {
			continue
		}
		finding := CutFinding{Kind: CutDuplicateIdentity}
		for _, member := range members {
			member.excluded = true
			finding.Resources = append(finding.Resources, CutResourceRef{AssetID: member.asset, ResourceID: id})
		}
		a.findings = append(a.findings, finding)
	}
}

// deriveConflicts marks as conflict every qualified instance that, together
// with instances of any kept member, violates the cardinality of its
// definition, the forest rule of contains or hosts across definitions, or the
// acyclicity of contains, groups, hosts or relays across definitions; and
// every qualified instance an absence statement of another member denies,
// which is reported as contradicted_absence. A stored conflict counts as the
// qualified instance its publisher upserted. No instance is chosen.
func (a *assembly) deriveConflicts() {
	groups := map[string][]*cutRelation{}
	cyclic := map[CoreType][]*cutRelation{}
	ends := map[string][]*cutRelation{}
	for _, relation := range a.relations {
		if !relation.structural() {
			continue
		}
		attributes := relation.definition.Relation
		from, to := string(relation.instance.From), string(relation.instance.To)
		switch attributes.Cardinality {
		case CardinalityOneToOne:
			groups["from\x00"+relation.definitionKey+"\x00"+from] = append(groups["from\x00"+relation.definitionKey+"\x00"+from], relation)
			groups["to\x00"+relation.definitionKey+"\x00"+to] = append(groups["to\x00"+relation.definitionKey+"\x00"+to], relation)
		case CardinalityOneToMany:
			groups["to\x00"+relation.definitionKey+"\x00"+to] = append(groups["to\x00"+relation.definitionKey+"\x00"+to], relation)
		}
		switch attributes.CoreType {
		case CoreContains, CoreHosts:
			forest := "forest\x00" + string(attributes.CoreType) + "\x00" + string(relation.part())
			groups[forest] = append(groups[forest], relation)
			cyclic[attributes.CoreType] = append(cyclic[attributes.CoreType], relation)
		case CoreGroups, CoreRelays:
			cyclic[attributes.CoreType] = append(cyclic[attributes.CoreType], relation)
		}
		ends[absenceLookupKey(relation.definitionKey, semreg.AbsenceFrom, relation.instance.From)] = append(ends[absenceLookupKey(relation.definitionKey, semreg.AbsenceFrom, relation.instance.From)], relation)
		ends[absenceLookupKey(relation.definitionKey, semreg.AbsenceTo, relation.instance.To)] = append(ends[absenceLookupKey(relation.definitionKey, semreg.AbsenceTo, relation.instance.To)], relation)
	}
	for _, group := range groups {
		if len(group) > 1 {
			for _, relation := range group {
				relation.conflict = true
			}
		}
	}
	for _, edges := range cyclic {
		for _, relation := range cycleRelations(edges) {
			relation.conflict = true
		}
	}
	for _, member := range a.kept() {
		for _, statement := range member.snapshot.RelationAbsences {
			for _, relation := range ends[absenceLookupKey(canonicalKey(statement.Definition), statement.Side, statement.Resource)] {
				if relation.member == member {
					continue
				}
				relation.conflict = true
				instance := relation.instance
				a.findings = append(a.findings, CutFinding{
					Kind:     CutContradictedAbsence,
					Relation: &CutRelationRef{AssetID: relation.member.asset, Definition: instance.Definition, From: instance.From, To: instance.To},
					Absence:  &CutAbsenceRef{AssetID: member.asset, Definition: statement.Definition, Resource: statement.Resource, Side: statement.Side},
				})
			}
		}
	}
}

func absenceLookupKey(definitionKey string, side semreg.AbsenceSide, resource semreg.ResourceID) string {
	return definitionKey + "\x00" + string(side) + "\x00" + string(resource)
}

// reportAnchorParents reports every current resource of a kept member whose
// anchor parent is not the whole of a contains, groups or hosts instance to
// it that is a current edge of the cut. Such a resource keeps its identity
// but is presented under no parent.
func (a *assembly) reportAnchorParents() {
	attached := map[[2]semreg.ResourceID]struct{}{}
	for _, relation := range a.relations {
		if relation.definition == nil || !relation.edge() {
			continue
		}
		switch relation.definition.Relation.CoreType {
		case CoreContains, CoreGroups, CoreHosts:
			attached[[2]semreg.ResourceID{relation.whole(), relation.part()}] = struct{}{}
		}
	}
	for _, member := range a.kept() {
		for _, resource := range member.snapshot.Resources {
			if resource.State != semreg.ResourceCurrent || resource.Anchor.Parent == nil {
				continue
			}
			if _, ok := attached[[2]semreg.ResourceID{*resource.Anchor.Parent, resource.ResourceID}]; ok {
				continue
			}
			a.findings = append(a.findings, CutFinding{Kind: CutAnchorParentUnresolved, Resources: []CutResourceRef{{AssetID: member.asset, ResourceID: resource.ResourceID}}})
		}
	}
}

// reportSameUnits reports every set of current resources of one kind with
// one anchor key held by two or more kept members: the same native key, or
// the same parent and slot. They stay in the cut; their facts must not be
// added together. Anchors that share no key are not detectable.
func (a *assembly) reportSameUnits() {
	type holder struct {
		ref    CutResourceRef
		member *cutMember
	}
	units := map[string][]holder{}
	for _, member := range a.kept() {
		for _, resource := range member.snapshot.Resources {
			if resource.State != semreg.ResourceCurrent {
				continue
			}
			key, ok := anchorKey(resource)
			if !ok {
				continue
			}
			units[key] = append(units[key], holder{CutResourceRef{AssetID: member.asset, ResourceID: resource.ResourceID}, member})
		}
	}
	for _, holders := range units {
		members := map[*cutMember]struct{}{}
		finding := CutFinding{Kind: CutSameUnit}
		for _, h := range holders {
			members[h.member] = struct{}{}
			finding.Resources = append(finding.Resources, h.ref)
		}
		if len(members) < 2 {
			continue
		}
		sort.Slice(finding.Resources, func(i, j int) bool {
			if finding.Resources[i].AssetID != finding.Resources[j].AssetID {
				return finding.Resources[i].AssetID < finding.Resources[j].AssetID
			}
			return finding.Resources[i].ResourceID < finding.Resources[j].ResourceID
		})
		a.findings = append(a.findings, finding)
	}
}

// anchorKey is (kind, key) for a native identity and (kind, parent, slot)
// otherwise. An anchor without either has no key.
func anchorKey(resource semreg.Resource) (string, bool) {
	anchor := resource.Anchor
	if anchor.Basis == semreg.AnchorNativeIdentity {
		if anchor.Key == nil {
			return "", false
		}
		return string(resource.Kind) + "\x00native\x00" + string(*anchor.Key), true
	}
	if anchor.Slot == nil {
		return "", false
	}
	parent := "\x01"
	if anchor.Parent != nil {
		parent = string(*anchor.Parent)
	}
	return string(resource.Kind) + "\x00slot\x00" + parent + "\x00" + *anchor.Slot, true
}

// reportUngraphed reports every member that holds a fact of a definition
// with disposition member but no resource, relation instance or absence
// statement. Facts of unknown definitions do not count.
func (a *assembly) reportUngraphed() {
	for _, member := range a.members {
		s := member.snapshot
		if len(s.Resources) != 0 || len(s.Relations) != 0 || len(s.RelationAbsences) != 0 {
			continue
		}
		for _, envelope := range s.Facts {
			key := envelope.Key
			element, ok := a.catalog.packEntryOfKind(semreg.PackRef{ID: key.PackID, Version: key.PackVersion}, key.FactID, KindFact)
			if ok && element.Disposition == DispositionMember {
				a.findings = append(a.findings, CutFinding{Kind: CutUngraphed, Assets: []semreg.AssetID{member.asset}})
				break
			}
		}
	}
}

// cycleRelations returns the instances whose whole and part lie in one
// strongly connected component of the whole-to-part graph, a loop on one
// resource included.
func cycleRelations(relations []*cutRelation) []*cutRelation {
	adjacency := map[semreg.ResourceID][]semreg.ResourceID{}
	for _, relation := range relations {
		adjacency[relation.whole()] = append(adjacency[relation.whole()], relation.part())
	}
	nodes := make([]semreg.ResourceID, 0, len(adjacency))
	for node := range adjacency {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	component := components(nodes, adjacency)
	var out []*cutRelation
	for _, relation := range relations {
		whole, wholeOK := component[relation.whole()]
		part, partOK := component[relation.part()]
		if wholeOK && partOK && whole == part {
			out = append(out, relation)
		}
	}
	return out
}

// components numbers the strongly connected components with an iterative
// Tarjan search, so a long chain cannot exhaust the stack.
func components(nodes []semreg.ResourceID, adjacency map[semreg.ResourceID][]semreg.ResourceID) map[semreg.ResourceID]int {
	index := map[semreg.ResourceID]int{}
	low := map[semreg.ResourceID]int{}
	onStack := map[semreg.ResourceID]bool{}
	component := map[semreg.ResourceID]int{}
	var stack []semreg.ResourceID
	next, count := 0, 0
	type frame struct {
		node semreg.ResourceID
		edge int
	}
	visit := func(node semreg.ResourceID) {
		index[node], low[node] = next, next
		next++
		stack = append(stack, node)
		onStack[node] = true
	}
	for _, root := range nodes {
		if _, seen := index[root]; seen {
			continue
		}
		visit(root)
		frames := []frame{{node: root}}
		for len(frames) > 0 {
			top := &frames[len(frames)-1]
			if top.edge < len(adjacency[top.node]) {
				target := adjacency[top.node][top.edge]
				top.edge++
				if _, seen := index[target]; !seen {
					visit(target)
					frames = append(frames, frame{node: target})
				} else if onStack[target] && index[target] < low[top.node] {
					low[top.node] = index[target]
				}
				continue
			}
			node := top.node
			frames = frames[:len(frames)-1]
			if len(frames) > 0 {
				parent := frames[len(frames)-1].node
				if low[node] < low[parent] {
					low[parent] = low[node]
				}
			}
			if low[node] == index[node] {
				for {
					member := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[member] = false
					component[member] = count
					if member == node {
						break
					}
				}
				count++
			}
		}
	}
	return component
}
