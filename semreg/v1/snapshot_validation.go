package semreg

import (
	"reflect"
	"strings"
)

// snapshotValidation carries the indexes that later snapshot checks read and
// collects every check's error in the order the checks run, so bestError picks
// the same error as a single pass would.
type snapshotValidation struct {
	snapshot     Snapshot
	errs         []error
	sources      map[sourceKey]SourceDescriptor
	fenceByKey   map[cursorKey]GenerationFence
	cursorByKey  map[cursorKey]PublicationCursor
	cursorSource map[sourceKey]bool
	bindings     map[NativeBindingID]NativeBinding
	services     map[ServiceInstanceID]ServiceInstance
	candidates   map[CandidateID]FactCandidate
}

func (s Snapshot) Validate() error {
	v := &snapshotValidation{snapshot: s}
	v.checkHeader()
	v.checkCollectionShape()
	v.checkSources()
	v.checkFences()
	v.checkCursors()
	v.checkSourceAndFenceCursors()
	v.checkBindings()
	v.checkIdentityLinks()
	v.checkServices()
	v.checkCapabilities()
	v.collectFactCandidates()
	v.checkRetainedObservations()
	v.checkCandidateGraph()
	v.checkSnapshotIdentity()
	return bestError(v.errs...)
}

func (v *snapshotValidation) add(errs ...error) {
	v.errs = append(v.errs, errs...)
}

func (v *snapshotValidation) checkHeader() {
	s := v.snapshot
	if s.Contract != ContractKernelV1 {
		v.add(errID(InvalidContract, "snapshot"))
	}
	v.add(s.SnapshotID.Validate(), s.AssetID.Validate(), s.Revisions.Validate(), s.EvaluatedAt.Validate(), s.EvaluateMonotonic.Validate())
}

func (v *snapshotValidation) checkCollectionShape() {
	s := v.snapshot
	for _, collection := range []any{s.Sources, s.Bindings, s.IdentityLinks, s.Facts, s.Services, s.Capabilities, s.Fences, s.Cursors} {
		if reflect.ValueOf(collection).IsNil() {
			v.add(errID(MissingMember, "snapshot collection"))
		}
	}
	if len(s.Sources) > maxSnapshotSources || len(s.Bindings) > maxSnapshotBindings || len(s.IdentityLinks) > maxSnapshotIdentity || len(s.Facts) > maxSnapshotEnvelopes || len(s.Retained) > maxSnapshotRetained || len(s.Services) > maxSnapshotServices || len(s.Capabilities) > maxSnapshotCapabilities || len(s.Fences) > maxSnapshotFences || len(s.Cursors) > maxSnapshotCursors {
		v.add(errID(BoundsExceeded, "snapshot collection"))
	}
	v.add(
		orderedUnique(s.Sources, compareSource),
		orderedUnique(s.Bindings, func(a, z NativeBinding) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) }),
		orderedUnique(s.IdentityLinks, func(a, z IdentityLink) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) }),
		orderedUnique(s.Facts, compareEnvelope),
		retainedObservationOrderError(s.Retained),
		orderedUnique(s.Services, func(a, z ServiceInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) }),
		orderedUnique(s.Capabilities, func(a, z CapabilityInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) }),
		orderedUnique(s.Fences, compareFence), orderedUnique(s.Cursors, compareCursor),
	)
}

func (v *snapshotValidation) checkSources() {
	v.sources = make(map[sourceKey]SourceDescriptor, len(v.snapshot.Sources))
	currentSources := make(map[SourceID]int)
	for _, source := range v.snapshot.Sources {
		v.add(source.Validate())
		v.sources[sourceKey{source.SourceID, source.SourceEpochID}] = source
		if source.State == SourceCurrent {
			currentSources[source.SourceID]++
			if currentSources[source.SourceID] > 1 {
				v.add(errID(StaleSourceEpoch, "multiple current source epochs"))
			}
		}
	}
}

func (v *snapshotValidation) checkFences() {
	v.fenceByKey = make(map[cursorKey]GenerationFence, len(v.snapshot.Fences))
	for _, fence := range v.snapshot.Fences {
		v.add(fence.Validate())
		_, ok := v.sources[sourceKey{fence.SourceID, fence.SourceEpochID}]
		if !ok {
			v.add(errID(DanglingReference, "fence source"))
		}
		v.fenceByKey[cursorKey{fence.SourceID, fence.SourceEpochID, fence.DriverGeneration}] = fence
	}
}

func (v *snapshotValidation) checkCursors() {
	v.cursorByKey = make(map[cursorKey]PublicationCursor, len(v.snapshot.Cursors))
	v.cursorSource = make(map[sourceKey]bool)
	unfenced := make(map[[2]string]int)
	for _, cursor := range v.snapshot.Cursors {
		v.add(cursor.Validate())
		source, ok := v.sources[sourceKey{cursor.SourceID, cursor.SourceEpochID}]
		if !ok {
			v.add(errID(DanglingReference, "cursor source"))
		}
		key := cursorKey{cursor.SourceID, cursor.SourceEpochID, cursor.DriverGeneration}
		v.cursorByKey[key] = cursor
		v.cursorSource[sourceKey{cursor.SourceID, cursor.SourceEpochID}] = true
		_, fenced := v.fenceByKey[key]
		if cursor.Fenced != fenced {
			v.add(errID(DanglingReference, "cursor fence"))
		}
		if !cursor.Fenced && ok && source.State == SourceCurrent {
			group := [2]string{string(cursor.SourceID), string(cursor.SourceEpochID)}
			unfenced[group]++
			if unfenced[group] > 1 {
				v.add(errID(StaleDriverGeneration, "multiple current generations"))
			}
		}
	}
}

func (v *snapshotValidation) checkSourceAndFenceCursors() {
	for key := range v.sources {
		if !v.cursorSource[key] {
			v.add(errID(DanglingReference, "source epoch cursor"))
		}
	}
	for key := range v.fenceByKey {
		cursor, ok := v.cursorByKey[key]
		if !ok || !cursor.Fenced {
			v.add(errID(DanglingReference, "fence cursor"))
		}
	}
}

func (v *snapshotValidation) checkBindings() {
	v.bindings = make(map[NativeBindingID]NativeBinding, len(v.snapshot.Bindings))
	for _, binding := range v.snapshot.Bindings {
		v.add(binding.Validate())
		v.bindings[binding.BindingID] = binding
		source, ok := v.sources[sourceKey{binding.SourceID, binding.SourceEpochID}]
		if !ok || binding.AssetID != v.snapshot.AssetID {
			v.add(errID(DanglingReference, "binding source"))
			continue
		}
		v.checkBindingLifecycle(binding, source)
	}
}

func (v *snapshotValidation) checkBindingLifecycle(binding NativeBinding, source SourceDescriptor) {
	key := cursorKey{binding.SourceID, binding.SourceEpochID, binding.DriverGeneration}
	cursor, cursorOK := v.cursorByKey[key]
	if !cursorOK {
		v.add(errID(DanglingReference, "binding cursor"))
	}
	switch binding.State {
	case BindingCurrent:
		if source.State != SourceCurrent || !cursorOK || cursor.Fenced {
			v.add(errID(DanglingReference, "current binding lifecycle"))
		}
	case BindingFenced:
		if _, ok := v.fenceByKey[key]; !ok {
			v.add(errID(DanglingReference, "fenced binding fence"))
		}
	case BindingRetired:
		if source.State != SourceRetired {
			v.add(errID(DanglingReference, "retired binding source"))
		}
	}
}

func (v *snapshotValidation) checkIdentityLinks() {
	for _, link := range v.snapshot.IdentityLinks {
		v.add(link.Validate())
		binding, ok := v.bindings[link.BindingID]
		if !ok || link.AssetID != v.snapshot.AssetID {
			v.add(errID(DanglingReference, "identity binding"))
		} else if link.State != LinkWithdrawn && binding.State != BindingCurrent {
			v.add(errID(DanglingReference, "actionable identity binding"))
		}
	}
}

func (v *snapshotValidation) checkServices() {
	v.services = make(map[ServiceInstanceID]ServiceInstance, len(v.snapshot.Services))
	for _, service := range v.snapshot.Services {
		v.add(service.Validate())
		v.services[service.InstanceID] = service
		binding, ok := v.bindings[service.BindingID]
		if !ok || service.AssetID != v.snapshot.AssetID || binding.SourceEpochID != service.SourceEpochID || binding.DriverGeneration != service.DriverGeneration {
			v.add(errID(DanglingReference, "service binding"))
		} else if service.Availability != AvailabilityWithdrawn && binding.State != BindingCurrent {
			v.add(errID(DanglingReference, "actionable service binding"))
		}
	}
}

func (v *snapshotValidation) checkCapabilities() {
	for _, capability := range v.snapshot.Capabilities {
		v.add(capability.Validate())
		binding, bindingOK := v.bindings[capability.BindingID]
		service, serviceOK := v.services[capability.ServiceInstance]
		if !bindingOK || !serviceOK || capability.AssetID != v.snapshot.AssetID || binding.SourceEpochID != capability.SourceEpochID || binding.DriverGeneration != capability.DriverGeneration || service.BindingID != capability.BindingID {
			v.add(errID(DanglingReference, "capability references"))
		} else if capability.Availability != AvailabilityWithdrawn && (binding.State != BindingCurrent || service.Availability == AvailabilityWithdrawn) {
			v.add(errID(DanglingReference, "actionable capability binding"))
		} else if binding.State != BindingCurrent && (capability.Availability != AvailabilityWithdrawn || service.Availability != AvailabilityWithdrawn) {
			v.add(errID(DanglingReference, "capability tombstone"))
		}
	}
}

func (v *snapshotValidation) collectFactCandidates() {
	v.candidates = make(map[CandidateID]FactCandidate)
	for _, envelope := range v.snapshot.Facts {
		v.add(envelope.Validate())
		if envelope.AssetID != v.snapshot.AssetID {
			v.add(errID(InvalidValue, "fact asset"))
		}
		for _, candidate := range envelope.Candidates {
			if candidate.CandidateID.Validate() != nil {
				// Match partial-wire identity collection: malformed IDs are
				// member errors, not keys in the global candidate namespace.
				continue
			}
			if _, exists := v.candidates[candidate.CandidateID]; exists {
				v.add(errID(DuplicateKey, "candidate id"))
			}
			v.candidates[candidate.CandidateID] = candidate
		}
	}
}

func (v *snapshotValidation) checkRetainedObservations() {
	for _, record := range v.snapshot.Retained {
		v.add(record.Validate())
		candidate := record.Candidate
		if candidate.Quality.Assertion != AssertionObserved || candidate.BindingID == nil || candidate.SourceEpochID == nil || candidate.DriverGeneration == nil || candidate.Origin.SourceID == nil {
			v.add(errID(InvalidValue, "retained observation path"))
			continue
		}
		v.checkRetainedObservationLifecycle(record)
	}
}

func (v *snapshotValidation) checkRetainedObservationLifecycle(record RetainedObservationRecord) {
	candidate := record.Candidate
	binding, exists := v.bindings[*candidate.BindingID]
	if !exists || binding.State == BindingCurrent || binding.SourceID != *candidate.Origin.SourceID || binding.SourceEpochID != *candidate.SourceEpochID || binding.DriverGeneration != *candidate.DriverGeneration || (record.Removal == RetainedRemovalGenerationFence && binding.State != BindingFenced && binding.State != BindingRetired) || (record.Removal == RetainedRemovalSourceRetirement && binding.State != BindingRetired) {
		v.add(errID(DanglingReference, "retained observation binding"))
	}
	if record.Removal == RetainedRemovalGenerationFence && !containsFence(v.snapshot.Fences, binding.SourceID, binding.SourceEpochID, binding.DriverGeneration) {
		v.add(errID(DanglingReference, "retained observation fence"))
	}
	if record.Removal == RetainedRemovalGenerationFence && binding.State == BindingRetired {
		source, ok := v.sources[sourceKey{binding.SourceID, binding.SourceEpochID}]
		if !ok || source.State != SourceRetired {
			v.add(errID(DanglingReference, "retained observation retired source"))
		}
	}
	if record.Removal == RetainedRemovalSourceRetirement {
		source, ok := v.sources[sourceKey{binding.SourceID, binding.SourceEpochID}]
		if !ok || source.State != SourceRetired {
			v.add(errID(DanglingReference, "retained observation source"))
		}
	}
}

func (v *snapshotValidation) checkCandidateGraph() {
	if len(v.candidates) > maxDerivationNodes {
		v.add(errID(BoundsExceeded, "candidate nodes"))
	} else {
		v.add(closeCandidateGraphStrict(v.candidates, v.bindings))
	}
}

func (v *snapshotValidation) checkSnapshotIdentity() {
	if expected, err := v.snapshot.computedID(); err != nil {
		v.add(err)
	} else if expected != v.snapshot.SnapshotID {
		v.add(errID(DigestMismatch, "snapshot identity"))
	}
}
