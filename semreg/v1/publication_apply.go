package semreg

import (
	"reflect"
	"strings"
)

// applyState is the detached diagnostic post-state of one publication batch.
// Every map is built from a clone of the current snapshot. The phases run in a
// fixed order and collect independent failures in that order; none of them
// touches the kernel. Only applyLocked commits, after final validation.
type applyState struct {
	registry             *Registry
	batch                PublicationBatch
	publicationMonotonic MonotonicPoint
	snapshot             *Snapshot
	before               Snapshot
	failure              error

	sources      map[sourceKey]SourceDescriptor
	bindings     map[NativeBindingID]NativeBinding
	links        map[NativeBindingID]IdentityLink
	services     map[ServiceInstanceID]ServiceInstance
	capabilities map[CapabilityInstanceID]CapabilityInstance
	fences       map[cursorKey]GenerationFence
	cursors      map[cursorKey]PublicationCursor
	candidates   map[CandidateID]FactCandidate
	retained     map[string]RetainedObservationRecord

	originalBindings     map[NativeBindingID]NativeBinding
	originalServices     map[ServiceInstanceID]ServiceInstance
	originalCapabilities map[CapabilityInstanceID]CapabilityInstance
	originalCandidates   map[CandidateID]FactCandidate

	retirements          map[SourceEpochID]struct{}
	transitionedBindings map[NativeBindingID]NativeBinding
	upsertedCandidates   map[CandidateID]struct{}
	factEnvelopes        []FactEnvelope
}

func (k *PublicationKernel) applyTo(snapshot *Snapshot, batch PublicationBatch, publicationMonotonic MonotonicPoint) (componentChanges, error) {
	state := newApplyState(k.registry, snapshot, batch, publicationMonotonic)
	state.validateInput()
	state.resolveBindings()
	state.evaluate()
	state.assembleFacts()
	changed := state.commit()
	return changed, state.failure
}

func (s *applyState) check(err error) { s.failure = bestError(s.failure, err) }

func newApplyState(registry *Registry, snapshot *Snapshot, batch PublicationBatch, publicationMonotonic MonotonicPoint) *applyState {
	s := &applyState{
		registry: registry, batch: batch, publicationMonotonic: publicationMonotonic, snapshot: snapshot,
		before:             cloneSnapshot(*snapshot),
		sources:            make(map[sourceKey]SourceDescriptor, len(snapshot.Sources)),
		bindings:           make(map[NativeBindingID]NativeBinding, len(snapshot.Bindings)),
		links:              make(map[NativeBindingID]IdentityLink, len(snapshot.IdentityLinks)),
		services:           make(map[ServiceInstanceID]ServiceInstance, len(snapshot.Services)),
		capabilities:       make(map[CapabilityInstanceID]CapabilityInstance, len(snapshot.Capabilities)),
		fences:             make(map[cursorKey]GenerationFence, len(snapshot.Fences)),
		cursors:            cursorMap(snapshot),
		candidates:         make(map[CandidateID]FactCandidate),
		originalCandidates: make(map[CandidateID]FactCandidate),
		retained:           make(map[string]RetainedObservationRecord, len(snapshot.Retained)),
	}
	for _, source := range snapshot.Sources {
		s.sources[sourceKey{source.SourceID, source.SourceEpochID}] = source
	}
	for _, binding := range snapshot.Bindings {
		s.bindings[binding.BindingID] = binding
	}
	for _, link := range snapshot.IdentityLinks {
		s.links[link.BindingID] = link
	}
	for _, service := range snapshot.Services {
		s.services[service.InstanceID] = service
	}
	for _, capability := range snapshot.Capabilities {
		s.capabilities[capability.InstanceID] = capability
	}
	s.originalBindings = cloneMap(s.bindings)
	s.originalServices = cloneMap(s.services)
	s.originalCapabilities = cloneMap(s.capabilities)
	for _, fence := range snapshot.Fences {
		s.fences[cursorKey{fence.SourceID, fence.SourceEpochID, fence.DriverGeneration}] = fence
	}
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			s.candidates[candidate.CandidateID] = candidate
			s.originalCandidates[candidate.CandidateID] = candidate
		}
	}
	for _, record := range snapshot.Retained {
		identity, err := retainedObservationIdentity(record.Candidate)
		if err != nil {
			s.check(err)
			continue
		}
		s.retained[identity] = record
	}
	return s
}

// validateInput is the input-validation phase: source lifecycle, source
// upserts and the generation transition, checked against the prior state.
func (s *applyState) validateInput() {
	s.validateSourceRetirements()
	s.upsertSources()
	s.validateGenerationTransition()
}

func (s *applyState) validateSourceRetirements() {
	batch := s.batch
	s.retirements = make(map[SourceEpochID]struct{}, len(batch.SourceRetirements))
	for _, epoch := range batch.SourceRetirements {
		s.retirements[epoch] = struct{}{}
		source, ok := s.sources[sourceKey{batch.SourceID, epoch}]
		if !ok {
			// Retirement is a lifecycle assertion about the header source. An
			// epoch owned by another source, or absent from that source, is stale
			// lifecycle input rather than an unresolved graph reference.
			s.check(errID(StaleSourceEpoch, "source retirement"))
			continue
		}
		if source.State != SourceCurrent {
			s.check(errID(StaleSourceEpoch, "source retirement"))
		}
	}
	for _, source := range s.snapshot.Sources {
		if source.SourceID == batch.SourceID && source.State == SourceCurrent && source.SourceEpochID != batch.SourceEpochID {
			if _, retiring := s.retirements[source.SourceEpochID]; !retiring {
				s.check(errID(StaleSourceEpoch, "current source epoch"))
			}
		}
	}
	if source, ok := s.sources[sourceKey{batch.SourceID, batch.SourceEpochID}]; ok && source.State != SourceCurrent {
		s.check(errID(StaleSourceEpoch, "publication source epoch"))
	}
}

func (s *applyState) upsertSources() {
	batch := s.batch
	for _, source := range batch.SourceUpserts {
		key := sourceKey{source.SourceID, source.SourceEpochID}
		if existing, ok := s.sources[key]; ok {
			if existing.ProfileID != source.ProfileID || existing.ProfileVersion != source.ProfileVersion {
				// Metadata may compose with retirement, but a changed profile
				// cannot remain current under the old sequencing domain.
				_, retiring := s.retirements[source.SourceEpochID]
				if !retiring || source.SourceEpochID == batch.SourceEpochID {
					s.check(errID(StaleSourceEpoch, "profile change requires replacement epoch"))
				}
			}
			if existing.State != SourceCurrent {
				s.check(errID(StaleSourceEpoch, "source upsert"))
			}
			_, err := validateRevisionedUpsert(existing, source)
			s.check(err)
		}
		s.sources[key] = source
	}
	if _, ok := s.sources[sourceKey{batch.SourceID, batch.SourceEpochID}]; !ok {
		s.check(errID(DanglingReference, "publication source"))
	}
}

func (s *applyState) validateGenerationTransition() {
	batch := s.batch
	currentGenerations := []Uint64{}
	for key, cursor := range s.cursors {
		if key.source == batch.SourceID && key.epoch == batch.SourceEpochID && !cursor.Fenced {
			currentGenerations = append(currentGenerations, key.generation)
		}
	}
	for _, generation := range currentGenerations {
		cmp := compareUint64(batch.DriverGeneration, generation)
		if cmp < 0 {
			s.check(errID(StaleDriverGeneration, "publication generation"))
		}
		if cmp > 0 && !containsFence(batch.GenerationFences, batch.SourceID, batch.SourceEpochID, generation) {
			s.check(errID(GenerationTransitionIncomplete, "missing generation fence"))
		}
	}
	for _, fence := range batch.GenerationFences {
		fenceKey := cursorKey{fence.SourceID, fence.SourceEpochID, fence.DriverGeneration}
		cursor, ok := s.cursors[fenceKey]
		if !ok {
			s.check(errID(DanglingReference, "generation fence cursor"))
			continue
		}
		if cursor.Fenced || s.fences[fenceKey].Revision != "" {
			s.check(errID(StaleDriverGeneration, "generation fence"))
		}
		if compareUint64(fence.DriverGeneration, batch.DriverGeneration) > 0 {
			s.check(errID(StaleDriverGeneration, "future generation fence"))
		}
	}
	// Fact lifecycle is checked against the transitioned bindings and complete
	// typed dependency graph below. An inferred fact has no header dependency.
	if containsFence(batch.GenerationFences, batch.SourceID, batch.SourceEpochID, batch.DriverGeneration) &&
		(len(batch.BindingUpserts) != 0 || len(batch.IdentityLinkUpserts) != 0 || len(batch.ServiceUpserts) != 0 || len(batch.CapabilityUpserts) != 0) {
		s.check(errID(StaleDriverGeneration, "fenced header generation upsert"))
	}
}

// resolveBindings is the binding-resolution phase. It applies the narrower
// generation transition first; if the same batch also retires the epoch,
// retirement is the stronger final tombstone state. Binding and identity-link
// upserts then resolve against the transitioned bindings.
func (s *applyState) resolveBindings() {
	s.applyGenerationFences()
	s.applySourceRetirements()
	s.transitionedBindings = cloneMap(s.bindings)
	s.upsertBindings()
	s.upsertIdentityLinks()
}

func (s *applyState) applyGenerationFences() {
	for _, fence := range s.batch.GenerationFences {
		fenceKey := cursorKey{fence.SourceID, fence.SourceEpochID, fence.DriverGeneration}
		cursor, ok := s.cursors[fenceKey]
		if !ok {
			continue
		}
		s.fences[fenceKey] = fence
		cursor.Fenced = true
		s.cursors[fenceKey] = cursor
		for _, binding := range s.bindings {
			if binding.SourceID == fence.SourceID && binding.SourceEpochID == fence.SourceEpochID && binding.DriverGeneration == fence.DriverGeneration {
				s.check(transitionBinding(binding, BindingFenced, fence.Evidence, s.bindings, s.links, s.services, s.capabilities))
			}
		}
	}
}

func (s *applyState) applySourceRetirements() {
	for epoch := range s.retirements {
		key := sourceKey{s.batch.SourceID, epoch}
		source, ok := s.sources[key]
		if !ok {
			continue
		}
		source.State = SourceRetired
		source.Revision = increment(source.Revision)
		s.sources[key] = source
		for _, binding := range s.bindings {
			if binding.SourceID == s.batch.SourceID && binding.SourceEpochID == epoch {
				s.check(transitionBinding(binding, BindingRetired, nil, s.bindings, s.links, s.services, s.capabilities))
			}
		}
	}
}

func (s *applyState) upsertBindings() {
	for _, binding := range s.batch.BindingUpserts {
		if source, ok := s.sources[sourceKey{binding.SourceID, binding.SourceEpochID}]; !ok {
			s.check(errID(DanglingReference, "binding source"))
		} else if source.State != SourceCurrent {
			s.check(errID(StaleSourceEpoch, "binding source epoch"))
		}
		if existing, ok := s.bindings[binding.BindingID]; ok {
			if existing.State != BindingCurrent {
				s.check(lifecycleError(existing))
			}
			if existing.AssetID != binding.AssetID || existing.SourceID != binding.SourceID || existing.SourceEpochID != binding.SourceEpochID || existing.DriverGeneration != binding.DriverGeneration || existing.NativeResource != binding.NativeResource {
				s.check(errID(IdentityNotQualified, "binding reuse"))
			}
			_, err := validateRevisionedUpsert(existing, binding)
			s.check(err)
		}
		s.bindings[binding.BindingID] = binding
	}
}

func (s *applyState) upsertIdentityLinks() {
	batch := s.batch
	for _, link := range batch.IdentityLinkUpserts {
		if _, exists := s.originalBindings[link.BindingID]; exists {
			s.check(validatePriorBindingForBatch(link.BindingID, batch, s.transitionedBindings))
		}
		binding, ok := s.bindings[link.BindingID]
		if !ok {
			s.check(errID(DanglingReference, "identity binding"))
			continue
		}
		if link.AssetID != batch.AssetID || binding.SourceID != batch.SourceID || binding.SourceEpochID != batch.SourceEpochID || binding.DriverGeneration != batch.DriverGeneration || binding.State != BindingCurrent {
			s.check(lifecycleOrValue(binding, "identity ownership"))
		}
		if existing, ok := s.links[link.BindingID]; ok {
			if existing.State == LinkWithdrawn {
				s.check(lifecycleError(binding))
			}
			_, err := validateRevisionedUpsert(existing, link)
			s.check(err)
		}
		s.links[link.BindingID] = link
	}
}

// evaluate is the evaluation phase: every fact, service and capability upsert
// is evaluated by the pack registry and against the resolved bindings, and the
// withdrawals are staged.
func (s *applyState) evaluate() {
	s.stageFactUpserts()
	s.stageFactWithdrawals()
	s.stageServiceUpserts()
	s.stageServiceWithdrawals()
	s.stageCapabilityUpserts()
	s.stageCapabilityWithdrawals()
}

func (s *applyState) stageFactUpserts() {
	batch := s.batch
	s.upsertedCandidates = make(map[CandidateID]struct{}, len(batch.FactUpserts))
	for _, candidate := range batch.FactUpserts {
		s.check(s.registry.ValidateFactCandidate(candidate))
		if candidate.Quality.Assertion == AssertionObserved {
			s.check(validateObservedForBatch(candidate, batch, s.bindings))
		}
		if existing, ok := s.originalCandidates[candidate.CandidateID]; ok {
			if existing.Quality.Assertion == AssertionObserved {
				// Observations may revise their path across a valid transition;
				// unlike service/capability instances, they leave no tombstone.
				if s.originalBindings[*existing.BindingID].SourceID != batch.SourceID {
					s.check(errID(InvalidValue, "prior candidate source ownership"))
				}
			}
			_, err := validateRevisionedUpsert(existing, candidate)
			s.check(err)
		}
		if _, exists := s.candidates[candidate.CandidateID]; !exists && len(s.candidates) >= maxStagedDerivationNodes {
			// More than max retained plus max final nodes cannot become a
			// conforming graph: new upserts are either accepted or reject the
			// batch, never cascade-removed. Continue collecting higher-ranked
			// ownership/validator diagnostics without growing graph state.
			s.check(errID(BoundsExceeded, "derivation staging nodes"))
			continue
		}
		s.candidates[candidate.CandidateID] = candidate
		s.upsertedCandidates[candidate.CandidateID] = struct{}{}
	}
}

func (s *applyState) stageFactWithdrawals() {
	for _, id := range s.batch.FactWithdrawals {
		s.stageFactWithdrawal(id)
	}
}

func (s *applyState) stageFactWithdrawal(id CandidateID) {
	batch := s.batch
	candidate, ok := s.originalCandidates[id]
	if ok && candidate.Quality.Assertion == AssertionObserved && candidate.SourceEpochID != nil && candidate.DriverGeneration != nil && (containsFence(batch.GenerationFences, batch.SourceID, *candidate.SourceEpochID, *candidate.DriverGeneration) || func() bool {
		_, retiring := findRetirement(batch.SourceRetirements, *candidate.SourceEpochID)
		return retiring
	}()) {
		s.check(errID(InvalidValue, "retained fact withdrawal overlap"))
	}
	retainedMatches := make([]RetainedObservationRecord, 0)
	for _, record := range s.retained {
		if record.Candidate.CandidateID == id {
			retainedMatches = append(retainedMatches, record)
		}
	}
	if !ok && len(retainedMatches) == 0 {
		s.check(errID(DanglingReference, "fact withdrawal"))
		return
	}
	if ok && candidate.Quality.Assertion == AssertionObserved {
		binding, bindingOK := s.originalBindings[*candidate.BindingID]
		if !bindingOK || !withdrawalCovered(batch, binding.SourceID, binding.SourceEpochID, binding.DriverGeneration) {
			s.check(errID(InvalidValue, "fact withdrawal ownership"))
		}
	}
	for _, record := range retainedMatches {
		candidate := record.Candidate
		binding, bindingOK := s.bindings[*candidate.BindingID]
		if !bindingOK || binding.SourceID != batch.SourceID || binding.State == BindingCurrent {
			s.check(errID(InvalidValue, "retained fact withdrawal ownership"))
		}
		identity, err := retainedObservationIdentity(candidate)
		if err != nil {
			s.check(err)
		} else {
			delete(s.retained, identity)
		}
	}
	delete(s.candidates, id)
}

func (s *applyState) stageServiceUpserts() {
	batch := s.batch
	for _, service := range batch.ServiceUpserts {
		s.check(s.registry.ValidateService(service))
		if existing, ok := s.originalServices[service.InstanceID]; ok {
			s.check(validatePriorBindingForBatch(existing.BindingID, batch, s.transitionedBindings))
			if existing.Availability == AvailabilityWithdrawn {
				s.check(lifecycleError(s.transitionedBindings[existing.BindingID]))
			}
			_, err := validateRevisionedUpsert(existing, service)
			s.check(err)
		}
		binding, ok := s.bindings[service.BindingID]
		if !ok {
			s.check(errID(DanglingReference, "service binding"))
			continue
		}
		if binding.SourceEpochID != service.SourceEpochID || binding.DriverGeneration != service.DriverGeneration {
			s.check(errID(DanglingReference, "service binding path"))
		}
		if service.AssetID != batch.AssetID || binding.SourceID != batch.SourceID || service.SourceEpochID != batch.SourceEpochID || service.DriverGeneration != batch.DriverGeneration || binding.State != BindingCurrent {
			s.check(lifecycleOrValue(binding, "service ownership"))
		}
		s.services[service.InstanceID] = service
	}
}

func (s *applyState) stageServiceWithdrawals() {
	for _, id := range s.batch.ServiceWithdrawals {
		original, ok := s.originalServices[id]
		if !ok {
			s.check(errID(DanglingReference, "service withdrawal"))
			continue
		}
		binding, bindingOK := s.originalBindings[original.BindingID]
		if !bindingOK || original.SourceEpochID != binding.SourceEpochID || original.DriverGeneration != binding.DriverGeneration || !withdrawalCovered(s.batch, binding.SourceID, original.SourceEpochID, original.DriverGeneration) {
			s.check(errID(InvalidValue, "service withdrawal ownership"))
		}
		service := s.services[id]
		if service.Availability != AvailabilityWithdrawn {
			service.Availability, service.Revision = AvailabilityWithdrawn, increment(service.Revision)
			s.services[id] = service
		}
	}
}

func (s *applyState) stageCapabilityUpserts() {
	batch := s.batch
	for _, capability := range batch.CapabilityUpserts {
		s.check(s.registry.ValidateCapability(capability))
		if existing, ok := s.originalCapabilities[capability.InstanceID]; ok {
			s.check(validatePriorBindingForBatch(existing.BindingID, batch, s.transitionedBindings))
			if existing.Availability == AvailabilityWithdrawn {
				s.check(lifecycleError(s.transitionedBindings[existing.BindingID]))
			}
			_, err := validateRevisionedUpsert(existing, capability)
			s.check(err)
		}
		binding, ok := s.bindings[capability.BindingID]
		if !ok {
			s.check(errID(DanglingReference, "capability binding"))
			continue
		}
		if binding.SourceID != batch.SourceID {
			s.check(errID(InvalidValue, "capability source ownership"))
		}
		service, serviceOK := s.services[capability.ServiceInstance]
		if !serviceOK {
			s.check(errID(DanglingReference, "capability service"))
			continue
		}
		if binding.SourceEpochID != capability.SourceEpochID || binding.DriverGeneration != capability.DriverGeneration || service.BindingID != capability.BindingID {
			s.check(errID(DanglingReference, "capability reference path"))
		}
		serviceForValidation := service
		if original, existed := s.originalServices[capability.ServiceInstance]; existed {
			serviceForValidation = original
		}
		if capability.AssetID != batch.AssetID || binding.SourceID != batch.SourceID || capability.SourceEpochID != batch.SourceEpochID || capability.DriverGeneration != batch.DriverGeneration || binding.State != BindingCurrent || serviceForValidation.Availability == AvailabilityWithdrawn {
			s.check(lifecycleOrValue(binding, "capability ownership"))
		}
		s.capabilities[capability.InstanceID] = capability
	}
}

func (s *applyState) stageCapabilityWithdrawals() {
	for _, id := range s.batch.CapabilityWithdrawals {
		original, ok := s.originalCapabilities[id]
		if !ok {
			s.check(errID(DanglingReference, "capability withdrawal"))
			continue
		}
		binding, bindingOK := s.originalBindings[original.BindingID]
		if !bindingOK || original.SourceEpochID != binding.SourceEpochID || original.DriverGeneration != binding.DriverGeneration || !withdrawalCovered(s.batch, binding.SourceID, original.SourceEpochID, original.DriverGeneration) {
			s.check(errID(InvalidValue, "capability withdrawal ownership"))
		}
		capability := s.capabilities[id]
		if capability.Availability != AvailabilityWithdrawn {
			capability.Availability, capability.Revision = AvailabilityWithdrawn, increment(capability.Revision)
			s.capabilities[id] = capability
		}
	}
}

// assembleFacts is the fact-assembly phase: the candidate graph is closed,
// observations whose source path is no longer current are retained, expired
// retained records are dropped, and the fact envelopes are rebuilt.
func (s *applyState) assembleFacts() {
	s.check(closeCandidateGraph(s.candidates, s.upsertedCandidates, s.bindings))
	s.retainNoncurrentObservations()
	s.expireRetainedObservations()
	factEnvelopes, err := rebuildEnvelopes(s.snapshot.Facts, s.snapshot.AssetID, s.candidates)
	s.check(err)
	s.factEnvelopes = factEnvelopes
}

// retainNoncurrentObservations preserves each original observation whose exact
// source path is no longer current, even when a newer current observation
// reuses its stable candidate ID. Retained identity is derived from the
// immutable original observation, never from a replacement candidate ID or
// binding.
func (s *applyState) retainNoncurrentObservations() {
	explicitWithdrawals := make(map[CandidateID]struct{}, len(s.batch.FactWithdrawals))
	for _, id := range s.batch.FactWithdrawals {
		explicitWithdrawals[id] = struct{}{}
	}
	for id, candidate := range s.originalCandidates {
		if _, withdrawn := explicitWithdrawals[id]; withdrawn {
			continue
		}
		if candidate.Quality.Assertion != AssertionObserved {
			continue
		}
		binding, exists := s.bindings[*candidate.BindingID]
		if !exists || binding.State == BindingCurrent {
			continue
		}
		retentionID, retainedErr := retainedObservationIdentity(candidate)
		if retainedErr != nil {
			s.check(retainedErr)
			continue
		}
		removal := RetainedRemovalGenerationFence
		if binding.State == BindingRetired {
			removal = RetainedRemovalSourceRetirement
		}
		s.retained[retentionID] = RetainedObservationRecord{Contract: ContractRetainedObservationV1, Candidate: candidate, Removal: removal}
	}
}

// expireRetainedObservations drops retained records at their original
// retention deadline and bounds what remains.
func (s *applyState) expireRetainedObservations() {
	context := EvaluationContext{EvaluatedAt: s.batch.ObservedAt, EvaluateMonotonic: s.publicationMonotonic}
	for id, record := range s.retained {
		freshness, retainedErr := evaluateFreshness(record.Candidate.Times, record.Candidate.FreshnessPolicy, context)
		if retainedErr != nil {
			s.check(retainedErr)
			continue
		}
		if freshness == FreshnessExpired {
			delete(s.retained, id)
		}
	}
	if len(s.retained) > maxSnapshotRetained {
		s.check(errID(BoundsExceeded, "retained observations"))
	}
}

// commit is the final phase. It writes the staged post-state into the
// detached working snapshot, reports which revision components changed and
// adds the post-state content checks. It never touches the kernel.
func (s *applyState) commit() componentChanges {
	batch := s.batch
	newCursor := PublicationCursor{SourceID: batch.SourceID, SourceEpochID: batch.SourceEpochID, DriverGeneration: batch.DriverGeneration, LastSequence: batch.Sequence, LastBatchDigest: batch.BatchDigest, Fenced: containsFence(batch.GenerationFences, batch.SourceID, batch.SourceEpochID, batch.DriverGeneration)}
	s.cursors[cursorKey{batch.SourceID, batch.SourceEpochID, batch.DriverGeneration}] = newCursor
	s.normalizeRevisions()
	s.writeSnapshot()

	before, snapshot := s.before, s.snapshot
	changed := componentChanges{
		identity:     !reflect.DeepEqual(before.Sources, snapshot.Sources) || !reflect.DeepEqual(before.Bindings, snapshot.Bindings) || !reflect.DeepEqual(before.IdentityLinks, snapshot.IdentityLinks) || !reflect.DeepEqual(before.Fences, snapshot.Fences),
		facts:        !reflect.DeepEqual(before.Facts, snapshot.Facts) || !reflect.DeepEqual(before.Retained, snapshot.Retained),
		services:     !reflect.DeepEqual(before.Services, snapshot.Services),
		capabilities: !reflect.DeepEqual(before.Capabilities, snapshot.Capabilities),
	}
	s.check(snapshot.publicationContentErrors())
	return changed
}

func (s *applyState) normalizeRevisions() {
	normalizeObjectRevisions(s.before.Sources, s.sources, func(value SourceDescriptor) sourceKey {
		return sourceKey{value.SourceID, value.SourceEpochID}
	})
	normalizeObjectRevisions(s.before.Bindings, s.bindings, func(value NativeBinding) NativeBindingID { return value.BindingID })
	normalizeObjectRevisions(s.before.IdentityLinks, s.links, func(value IdentityLink) NativeBindingID { return value.BindingID })
	normalizeObjectRevisions(s.before.Services, s.services, func(value ServiceInstance) ServiceInstanceID { return value.InstanceID })
	normalizeObjectRevisions(s.before.Capabilities, s.capabilities, func(value CapabilityInstance) CapabilityInstanceID { return value.InstanceID })
}

func (s *applyState) writeSnapshot() {
	snapshot := s.snapshot
	snapshot.Sources = sortedMapValues(s.sources, func(a, z SourceDescriptor) int { return compareSource(a, z) })
	snapshot.Bindings = sortedMapValues(s.bindings, func(a, z NativeBinding) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) })
	snapshot.IdentityLinks = sortedMapValues(s.links, func(a, z IdentityLink) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) })
	snapshot.Facts = s.factEnvelopes
	if len(s.retained) == 0 {
		snapshot.Retained = nil
	} else {
		snapshot.Retained = sortedMapValues(s.retained, func(a, z RetainedObservationRecord) int {
			left, _ := retainedObservationIdentity(a.Candidate)
			right, _ := retainedObservationIdentity(z.Candidate)
			return strings.Compare(left, right)
		})
	}
	snapshot.Services = sortedMapValues(s.services, func(a, z ServiceInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) })
	snapshot.Capabilities = sortedMapValues(s.capabilities, func(a, z CapabilityInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) })
	snapshot.Fences = sortedMapValues(s.fences, compareFence)
	snapshot.Cursors = sortedMapValues(s.cursors, compareCursor)
}
