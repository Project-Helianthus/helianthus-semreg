package semreg

import "sort"

// Core relation types whose instances form a forest or must stay acyclic
// across all their definitions.
const (
	coreContains = "contains"
	coreGroups   = "groups"
	coreHosts    = "hosts"
	coreRelays   = "relays"
)

// graphRecordGateError admits relation instances and absence statements into
// a kernel that publishes resources; their publication rules run in applyTo.
// Every other kernel refuses graph members as graphPublicationError states.
func (k *PublicationKernel) graphRecordGateError(batch PublicationBatch) error {
	if k.registry.graphResources() != nil {
		return nil
	}
	return k.graphPublicationError(batch)
}

// nativeRelationBasis reports whether a basis class dies with its binding
// generation (REL-5).
func nativeRelationBasis(class RelationBasis) bool {
	return class == RelationNativeObservation || class == RelationGateRule
}

// relationBasisAgrees is the origin part of REL-3: a configuration basis has
// an operator origin, and a native observation or gate rule a native one.
func relationBasisAgrees(class RelationBasis, kind OriginKind) bool {
	if nativeRelationBasis(class) {
		return kind == OriginNativeObservation
	}
	return class == RelationConfiguration && kind == OriginOperator
}

// loadRelations indexes the relation records of the current snapshot. A
// stored conflict is derived, never published, so an instance is staged with
// the qualified state its publisher gave it; deriveRelationConflicts sets the
// conflicts of the post-state again.
func (s *applyState) loadRelations(snapshot *Snapshot) {
	s.relations = make(map[string]RelationInstance, len(snapshot.Relations))
	s.absences = make(map[string]RelationAbsence, len(snapshot.RelationAbsences))
	s.upsertedRelations = make(map[string]struct{}, len(s.batch.RelationUpserts))
	s.upsertedAbsences = make(map[string]struct{}, len(s.batch.AbsenceUpserts))
	for _, relation := range snapshot.Relations {
		key, ok := relationInstanceGraphKey(relation)
		if !ok {
			continue
		}
		if relation.State == LinkConflict {
			relation.State = LinkQualified
		}
		s.relations[key] = relation
	}
	for _, absence := range snapshot.RelationAbsences {
		if key, ok := absenceStatementGraphKey(absence); ok {
			s.absences[key] = absence
		}
	}
	s.originalRelations = cloneMap(s.relations)
	s.originalAbsences = cloneMap(s.absences)
}

// removeEndedRelations is REL-5: after the generation fences and source
// retirements of the batch, every instance and absence statement with a
// native basis whose origin binding is no longer current leaves the post-state.
// Configuration records stay until they are withdrawn.
func (s *applyState) removeEndedRelations() {
	ended := func(class RelationBasis, origin OriginRef) bool {
		if !nativeRelationBasis(class) || origin.BindingID == nil {
			return false
		}
		binding, ok := s.bindings[*origin.BindingID]
		return !ok || binding.State != BindingCurrent
	}
	for key, relation := range s.relations {
		if ended(relation.BasisClass, relation.Origin) {
			delete(s.relations, key)
		}
	}
	for key, absence := range s.absences {
		if ended(absence.BasisClass, absence.Origin) {
			delete(s.absences, key)
		}
	}
}

// upsertRelations stages the relation upserts and withdrawals of a kernel
// that publishes resources. An upsert of a staged record carries its next
// revision, and a staged native record is replaced only by its own source. A
// withdrawal names a record of the current snapshot; a native record is
// withdrawn only by its own source path or by a fence or retirement of the
// batch. One key is never upserted and withdrawn in one batch. Rules that
// need the complete post-state run in checkRelationGraph.
func (s *applyState) upsertRelations() {
	if s.registry.graphResources() == nil {
		return
	}
	withdrawnRelations := make(map[string]struct{}, len(s.batch.RelationWithdrawals))
	for _, withdrawal := range s.batch.RelationWithdrawals {
		if key, ok := relationGraphKey(withdrawal); ok {
			withdrawnRelations[key] = struct{}{}
		}
	}
	for _, relation := range s.batch.RelationUpserts {
		key, ok := relationInstanceGraphKey(relation)
		if !ok {
			continue
		}
		if _, withdrawn := withdrawnRelations[key]; withdrawn {
			s.check(errID(InvalidValue, "relation upserted and withdrawn in one batch"))
		}
		if existing, ok := s.relations[key]; ok {
			s.check(s.priorRelationOwnershipError(existing.BasisClass, existing.Origin))
			_, err := validateRevisionedUpsert(existing, relation)
			s.check(err)
		}
		s.relations[key] = relation
		s.upsertedRelations[key] = struct{}{}
	}
	for _, withdrawal := range s.batch.RelationWithdrawals {
		key, ok := relationGraphKey(withdrawal)
		if !ok {
			continue
		}
		original, exists := s.originalRelations[key]
		if !exists {
			s.check(errID(DanglingReference, "relation withdrawal"))
			continue
		}
		s.check(s.relationWithdrawalOwnershipError(original.BasisClass, original.Origin))
		delete(s.relations, key)
	}

	withdrawnAbsences := make(map[string]struct{}, len(s.batch.AbsenceWithdrawals))
	for _, withdrawal := range s.batch.AbsenceWithdrawals {
		if key, ok := absenceGraphKey(withdrawal); ok {
			withdrawnAbsences[key] = struct{}{}
		}
	}
	for _, absence := range s.batch.AbsenceUpserts {
		key, ok := absenceStatementGraphKey(absence)
		if !ok {
			continue
		}
		if _, withdrawn := withdrawnAbsences[key]; withdrawn {
			s.check(errID(InvalidValue, "absence statement upserted and withdrawn in one batch"))
		}
		if existing, ok := s.absences[key]; ok {
			s.check(s.priorRelationOwnershipError(existing.BasisClass, existing.Origin))
			_, err := validateRevisionedUpsert(existing, absence)
			s.check(err)
		}
		s.absences[key] = absence
		s.upsertedAbsences[key] = struct{}{}
	}
	for _, withdrawal := range s.batch.AbsenceWithdrawals {
		key, ok := absenceGraphKey(withdrawal)
		if !ok {
			continue
		}
		original, exists := s.originalAbsences[key]
		if !exists {
			s.check(errID(DanglingReference, "absence statement withdrawal"))
			continue
		}
		s.check(s.relationWithdrawalOwnershipError(original.BasisClass, original.Origin))
		delete(s.absences, key)
	}
}

// priorRelationOwnershipError keeps a staged native record with the source
// that observed it. A configuration record carries no source and any source
// of the asset may replace it.
func (s *applyState) priorRelationOwnershipError(class RelationBasis, origin OriginRef) error {
	if !nativeRelationBasis(class) || origin.BindingID == nil {
		return nil
	}
	binding, ok := s.originalBindings[*origin.BindingID]
	if !ok || binding.SourceID != s.batch.SourceID {
		return errID(InvalidValue, "prior relation source ownership")
	}
	return nil
}

func (s *applyState) relationWithdrawalOwnershipError(class RelationBasis, origin OriginRef) error {
	if !nativeRelationBasis(class) || origin.BindingID == nil {
		return nil
	}
	binding, ok := s.originalBindings[*origin.BindingID]
	if !ok || !withdrawalCovered(s.batch, binding.SourceID, binding.SourceEpochID, binding.DriverGeneration) {
		return errID(InvalidValue, "relation withdrawal ownership")
	}
	return nil
}

// checkRelationGraph runs the relation rules that read the complete
// post-state: REL-1 to REL-4 and the origin path for every upsert, RES-8 for
// every record that stays, REL-7, and then the REL-6 conflict derivation.
func (s *applyState) checkRelationGraph() {
	catalog := s.registry.graphResources()
	if catalog == nil {
		return
	}
	// A resource that this asset's own records name as a starting end or as
	// the resource of an absence statement is homed here (PUB-1), so a far
	// end that names it resolves in this snapshot.
	homed := make(map[ResourceID]struct{}, len(s.relations)+len(s.absences))
	for _, relation := range s.relations {
		homed[relation.From] = struct{}{}
	}
	for _, absence := range s.absences {
		homed[absence.Resource] = struct{}{}
	}
	for key, relation := range s.relations {
		if _, upserted := s.upsertedRelations[key]; upserted {
			s.check(s.relationUpsertError(catalog, relation, homed))
			continue
		}
		s.check(s.namedResourceError(relation.From))
		if _, held := s.resources[relation.To]; held {
			s.check(s.namedResourceError(relation.To))
		}
	}
	for key, absence := range s.absences {
		if _, upserted := s.upsertedAbsences[key]; upserted {
			s.check(s.absenceUpsertError(catalog, absence))
			continue
		}
		s.check(s.namedResourceError(absence.Resource))
	}
	s.checkInstanceAbsencePairs()
	s.deriveRelationConflicts(catalog)
}

// namedResourceError is RES-8 for a record that stays in the snapshot: a
// resource it names is current.
func (s *applyState) namedResourceError(id ResourceID) error {
	if resource, ok := s.resources[id]; !ok || resource.State != ResourceCurrent {
		return errID(DanglingReference, "resource named by a relation record is not current")
	}
	return nil
}

func (s *applyState) relationUpsertError(catalog GraphResourceCatalog, relation RelationInstance, homed map[ResourceID]struct{}) error {
	definition, ok := catalog.RelationDefinition(relation.Definition)
	if !ok || definition.Membership.Disposition != GraphMember {
		return errID(MembershipMissing, "relation definition is not a catalog member")
	}
	return bestError(
		s.relationEndError(relation.From, definition.FromKinds, "relation starting end"),
		s.relationFarEndError(relation.To, definition.ToKinds, homed),
		relationBasisError(definition, relation.BasisClass, relation.Origin),
		s.relationOriginError(relation.Origin),
	)
}

func (s *applyState) absenceUpsertError(catalog GraphResourceCatalog, absence RelationAbsence) error {
	definition, ok := catalog.RelationDefinition(absence.Definition)
	if !ok || definition.Membership.Disposition != GraphMember {
		return errID(MembershipMissing, "relation definition is not a catalog member")
	}
	kinds := definition.FromKinds
	if absence.Side == AbsenceTo {
		kinds = definition.ToKinds
	}
	return bestError(
		s.relationEndError(absence.Resource, kinds, "absence statement resource"),
		relationBasisError(definition, absence.BasisClass, absence.Origin),
		s.relationOriginError(absence.Origin),
	)
}

// relationEndError is REL-2: the resource is current in this asset
// (dangling_reference otherwise) and of an admitted kind (identity_drift
// otherwise).
func (s *applyState) relationEndError(id ResourceID, kinds []DefinitionID, detail string) error {
	resource, ok := s.resources[id]
	if !ok || resource.State != ResourceCurrent {
		return errID(DanglingReference, detail)
	}
	if !containsDefinitionID(kinds, resource.Kind) {
		return errID(IdentityDrift, detail+" kind")
	}
	return nil
}

// relationFarEndError is REL-4. A far end held by this asset is current
// (stale_relation otherwise) and of an admitted kind (identity_drift
// otherwise). A far end this asset holds no record of is homed elsewhere and
// resolves in a graph cut, unless the asset's own records place it here, in
// which case it is dangling.
func (s *applyState) relationFarEndError(id ResourceID, kinds []DefinitionID, homed map[ResourceID]struct{}) error {
	resource, ok := s.resources[id]
	if !ok {
		if _, here := homed[id]; here {
			return errID(DanglingReference, "relation far end")
		}
		return nil
	}
	if resource.State != ResourceCurrent {
		return errID(StaleRelation, "relation far end is not current")
	}
	if !containsDefinitionID(kinds, resource.Kind) {
		return errID(IdentityDrift, "relation far end kind")
	}
	return nil
}

// relationBasisError is REL-3: the basis class is admitted by the definition
// and agrees with the origin kind. An empty basis is rejected with the same
// error by the record's own validation.
func relationBasisError(definition GraphRelationDefinition, class RelationBasis, origin OriginRef) error {
	admitted := false
	for _, candidate := range definition.BasisClasses {
		admitted = admitted || candidate == class
	}
	if !admitted || !relationBasisAgrees(class, origin.Kind) {
		return errID(RelationNotQualified, "relation basis class")
	}
	return nil
}

// relationOriginError resolves a native origin to a binding of the asset with
// the origin's source and epoch (dangling_reference otherwise) and requires
// that binding to be current and of the batch's source, epoch and
// generation, so a fenced or retired generation publishes no record.
func (s *applyState) relationOriginError(origin OriginRef) error {
	if origin.Kind != OriginNativeObservation || origin.BindingID == nil || origin.SourceID == nil || origin.SourceEpochID == nil {
		return nil
	}
	binding, ok := s.bindings[*origin.BindingID]
	if !ok || binding.SourceID != *origin.SourceID || binding.SourceEpochID != *origin.SourceEpochID {
		return errID(DanglingReference, "relation origin binding")
	}
	batch := s.batch
	if binding.SourceID != batch.SourceID || binding.SourceEpochID != batch.SourceEpochID || binding.DriverGeneration != batch.DriverGeneration || binding.State != BindingCurrent {
		return lifecycleOrValue(binding, "relation origin ownership")
	}
	return nil
}

// checkInstanceAbsencePairs is REL-7: no absence statement names the
// definition, resource and side of an end of a current instance, whatever
// the instance's state.
func (s *applyState) checkInstanceAbsencePairs() {
	if len(s.absences) == 0 {
		return
	}
	for _, relation := range s.relations {
		for _, end := range []RelationAbsenceKey{{relation.Definition, relation.From, AbsenceFrom}, {relation.Definition, relation.To, AbsenceTo}} {
			if key, ok := absenceGraphKey(end); ok {
				if _, both := s.absences[key]; both {
					s.check(errID(InvalidValue, "relation instance and absence statement for one end"))
				}
			}
		}
	}
}

// deriveRelationConflicts is REL-6: every qualified instance that takes part
// in a conflict becomes conflict; every other qualified instance stays
// qualified. No instance is chosen.
func (s *applyState) deriveRelationConflicts(catalog GraphResourceCatalog) {
	conflicts := relationConflicts(s.relations, catalog.RelationDefinition)
	for key := range conflicts {
		relation := s.relations[key]
		relation.State = LinkConflict
		s.relations[key] = relation
	}
}

type relationEdge struct {
	key        string
	definition string
	from, to   ResourceID
}

// relationConflicts returns the keys of the qualified instances that take
// part in a cardinality violation of their definition, a forest violation of
// contains or hosts across definitions, or a cycle of contains, groups, hosts
// or relays across definitions. The result is a set, so it does not depend on
// map order.
func relationConflicts(relations map[string]RelationInstance, lookup func(DefinitionRef) (GraphRelationDefinition, bool)) map[string]struct{} {
	conflicts := make(map[string]struct{})
	byFrom := make(map[string][]string)
	byTo := make(map[string][]string)
	forest := make(map[string][]string)
	cyclic := make(map[string][]relationEdge)
	for key, relation := range relations {
		if relation.State != LinkQualified {
			continue
		}
		definition, ok := lookup(relation.Definition)
		if !ok {
			continue
		}
		definitionKey, ok := canonicalDefinitionKey(relation.Definition)
		if !ok {
			continue
		}
		switch definition.Cardinality {
		case "one_to_one":
			byFrom[definitionKey+"\x00"+string(relation.From)] = append(byFrom[definitionKey+"\x00"+string(relation.From)], key)
			byTo[definitionKey+"\x00"+string(relation.To)] = append(byTo[definitionKey+"\x00"+string(relation.To)], key)
		case "one_to_many":
			byTo[definitionKey+"\x00"+string(relation.To)] = append(byTo[definitionKey+"\x00"+string(relation.To)], key)
		}
		switch definition.CoreType {
		case coreContains, coreHosts:
			forest[definition.CoreType+"\x00"+string(relation.To)] = append(forest[definition.CoreType+"\x00"+string(relation.To)], key)
			cyclic[definition.CoreType] = append(cyclic[definition.CoreType], relationEdge{key, definitionKey, relation.From, relation.To})
		case coreGroups, coreRelays:
			cyclic[definition.CoreType] = append(cyclic[definition.CoreType], relationEdge{key, definitionKey, relation.From, relation.To})
		}
	}
	for _, groups := range []map[string][]string{byFrom, byTo, forest} {
		for _, keys := range groups {
			if len(keys) > 1 {
				for _, key := range keys {
					conflicts[key] = struct{}{}
				}
			}
		}
	}
	for _, edges := range cyclic {
		for _, key := range cycleEdges(edges) {
			conflicts[key] = struct{}{}
		}
	}
	return conflicts
}

// cycleEdges returns the keys of the edges that lie on a directed cycle: the
// edges whose two ends are in one strongly connected component, a loop on one
// resource included.
func cycleEdges(edges []relationEdge) []string {
	adjacency := make(map[ResourceID][]ResourceID)
	for _, edge := range edges {
		adjacency[edge.from] = append(adjacency[edge.from], edge.to)
	}
	nodes := make([]ResourceID, 0, len(adjacency))
	for node := range adjacency {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	component := stronglyConnected(nodes, adjacency)
	var result []string
	for _, edge := range edges {
		from, fromOK := component[edge.from]
		to, toOK := component[edge.to]
		if fromOK && toOK && from == to {
			result = append(result, edge.key)
		}
	}
	return result
}

// stronglyConnected numbers the strongly connected components of a directed
// graph with an iterative Tarjan search, so a long chain cannot exhaust the
// stack. A node that is only ever a far end has no outgoing edge and lies on
// no cycle; it is still numbered.
func stronglyConnected(nodes []ResourceID, adjacency map[ResourceID][]ResourceID) map[ResourceID]int {
	index := make(map[ResourceID]int)
	low := make(map[ResourceID]int)
	onStack := make(map[ResourceID]bool)
	component := make(map[ResourceID]int)
	var stack []ResourceID
	next, components := 0, 0
	type frame struct {
		node ResourceID
		edge int
	}
	for _, root := range nodes {
		if _, seen := index[root]; seen {
			continue
		}
		index[root], low[root] = next, next
		next++
		stack = append(stack, root)
		onStack[root] = true
		frames := []frame{{node: root}}
		for len(frames) > 0 {
			top := &frames[len(frames)-1]
			if top.edge < len(adjacency[top.node]) {
				target := adjacency[top.node][top.edge]
				top.edge++
				if _, seen := index[target]; !seen {
					index[target], low[target] = next, next
					next++
					stack = append(stack, target)
					onStack[target] = true
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
					component[member] = components
					if member == node {
						break
					}
				}
				components++
			}
		}
	}
	return component
}

func containsDefinitionID(ids []DefinitionID, id DefinitionID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

// writeRelations writes the staged relation records in canonical key order.
// An asset without them omits both collections, so its snapshot keeps the
// bytes it had before relation records existed.
func (s *applyState) writeRelations() {
	s.snapshot.Relations = nil
	if len(s.relations) != 0 {
		keys := sortedGraphKeys(s.relations)
		s.snapshot.Relations = make([]RelationInstance, 0, len(keys))
		for _, key := range keys {
			s.snapshot.Relations = append(s.snapshot.Relations, s.relations[key])
		}
	}
	s.snapshot.RelationAbsences = nil
	if len(s.absences) != 0 {
		keys := sortedGraphKeys(s.absences)
		s.snapshot.RelationAbsences = make([]RelationAbsence, 0, len(keys))
		for _, key := range keys {
			s.snapshot.RelationAbsences = append(s.snapshot.RelationAbsences, s.absences[key])
		}
	}
}

func sortedGraphKeys[V any](records map[string]V) []string {
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// normalizeRelationRevisions gives every relation record that changed in this
// batch, by its publisher or by a derived conflict, the next revision of its
// record in the current snapshot. A new record keeps its published revision.
func (s *applyState) normalizeRelationRevisions() {
	normalizeObjectRevisions(s.before.Relations, s.relations, func(value RelationInstance) string {
		key, _ := relationInstanceGraphKey(value)
		return key
	})
	normalizeObjectRevisions(s.before.RelationAbsences, s.absences, func(value RelationAbsence) string {
		key, _ := absenceStatementGraphKey(value)
		return key
	})
}
