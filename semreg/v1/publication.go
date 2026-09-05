package semreg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"sync"
)

const (
	maxSnapshotSources      = 32
	maxSnapshotBindings     = 128
	maxSnapshotIdentity     = 128
	maxSnapshotEnvelopes    = 4096
	maxSnapshotServices     = 1024
	maxSnapshotCapabilities = 2048
	maxSnapshotFences       = 128
	maxSnapshotCursors      = 128
	maxDerivationNodes      = 4096
	maxDerivationDepth      = 32
)

func (f GenerationFence) Validate() error {
	var numeric error
	if !positive(f.DriverGeneration) || !positive(f.Revision) {
		numeric = errID(InvalidIdentifier, "generation fence")
	}
	return bestError(f.SourceID.Validate(), f.SourceEpochID.Validate(), numeric, f.Reason.Validate(), validateEvidenceSet(f.Evidence, 1, 32))
}

func (r RevisionVector) Validate() error {
	if !positive(r.Semantic) || !positive(r.Identity) || !positive(r.Facts) || !positive(r.Services) || !positive(r.Capabilities) {
		return errID(InvalidIdentifier, "revision vector")
	}
	return nil
}

func (c PublicationCursor) Validate() error {
	var numeric error
	if !positive(c.DriverGeneration) || !positive(c.LastSequence) {
		numeric = errID(InvalidIdentifier, "publication cursor")
	}
	return bestError(c.SourceID.Validate(), c.SourceEpochID.Validate(), numeric, c.LastBatchDigest.Validate())
}

func (b PublicationBatch) Validate() error {
	if err := b.validateStructure(true); err != nil {
		return err
	}
	digest, err := b.computedDigestUnchecked()
	if err != nil {
		return err
	}
	if digest != b.BatchDigest {
		return errID(DigestMismatch, "publication batch")
	}
	return nil
}

// ComputedDigest returns the canonical batch digest with batch_digest omitted.
// It is useful to seal a newly constructed typed batch before Apply.
func (b PublicationBatch) ComputedDigest() (Digest, error) {
	if err := b.validateStructure(false); err != nil {
		return "", err
	}
	return b.computedDigestUnchecked()
}

func (b PublicationBatch) computedDigestUnchecked() (Digest, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return "", errID(InvalidJSON, "publication batch")
	}
	node, duplicate, err := parseJSON(raw)
	if err != nil {
		return "", err
	}
	if duplicate {
		return "", errID(DuplicateKey, "publication batch")
	}
	filtered := node.object[:0]
	for _, member := range node.object {
		if member.key != "batch_digest" {
			filtered = append(filtered, member)
		}
	}
	node.object = filtered
	var out bytes.Buffer
	if err := writeCanonical(&out, node); err != nil {
		return "", errID(InvalidJSON, "publication batch")
	}
	sum := sha256.Sum256(out.Bytes())
	return Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

func (b PublicationBatch) validateStructure(requireDigest bool) error {
	var errs []error
	if b.Contract != ContractKernelV1 {
		errs = append(errs, errID(InvalidContract, "publication batch"))
	}
	errs = append(errs, b.BatchID.Validate(), b.AssetID.Validate(), b.SourceID.Validate(), b.SourceEpochID.Validate(), b.ObservedAt.Validate())
	if requireDigest {
		errs = append(errs, b.BatchDigest.Validate())
	}
	if !positive(b.DriverGeneration) || !positive(b.Sequence) {
		errs = append(errs, errID(InvalidIdentifier, "publication order"))
	}
	if _, ok := u64(b.ExpectedSemanticRevision); !ok {
		errs = append(errs, errID(InvalidValue, "expected semantic revision"))
	}

	required := []any{b.SourceUpserts, b.SourceRetirements, b.BindingUpserts, b.IdentityLinkUpserts, b.FactUpserts, b.FactWithdrawals, b.ServiceUpserts, b.ServiceWithdrawals, b.CapabilityUpserts, b.CapabilityWithdrawals, b.GenerationFences}
	for _, collection := range required {
		if reflect.ValueOf(collection).IsNil() {
			errs = append(errs, errID(MissingMember, "publication collection"))
		}
	}
	for _, record := range b.SourceUpserts {
		errs = append(errs, record.Validate())
		if record.State == SourceRetired {
			errs = append(errs, errID(StaleSourceEpoch, "publisher-retired source"))
		}
	}
	for _, epoch := range b.SourceRetirements {
		errs = append(errs, epoch.Validate())
	}
	for _, record := range b.BindingUpserts {
		errs = append(errs, record.Validate())
		if record.State == BindingFenced {
			errs = append(errs, errID(StaleDriverGeneration, "publisher-fenced binding"))
		} else if record.State == BindingRetired {
			errs = append(errs, errID(StaleSourceEpoch, "publisher-retired binding"))
		}
	}
	for _, record := range b.IdentityLinkUpserts {
		errs = append(errs, record.Validate())
	}
	for _, record := range b.FactUpserts {
		errs = append(errs, record.Validate())
	}
	for _, id := range b.FactWithdrawals {
		errs = append(errs, id.Validate())
	}
	for _, record := range b.ServiceUpserts {
		errs = append(errs, record.Validate())
	}
	for _, id := range b.ServiceWithdrawals {
		errs = append(errs, id.Validate())
	}
	for _, record := range b.CapabilityUpserts {
		errs = append(errs, record.Validate())
	}
	for _, id := range b.CapabilityWithdrawals {
		errs = append(errs, id.Validate())
	}
	for _, record := range b.GenerationFences {
		errs = append(errs, record.Validate())
	}

	errs = append(errs,
		orderedUnique(b.SourceUpserts, func(a, z SourceDescriptor) int { return compareSource(a, z) }),
		orderedUnique(b.SourceRetirements, func(a, z SourceEpochID) int { return strings.Compare(string(a), string(z)) }),
		orderedUnique(b.BindingUpserts, func(a, z NativeBinding) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) }),
		orderedUnique(b.IdentityLinkUpserts, func(a, z IdentityLink) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) }),
		orderedUnique(b.FactUpserts, func(a, z FactCandidate) int { return strings.Compare(string(a.CandidateID), string(z.CandidateID)) }),
		orderedUnique(b.FactWithdrawals, func(a, z CandidateID) int { return strings.Compare(string(a), string(z)) }),
		orderedUnique(b.ServiceUpserts, func(a, z ServiceInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) }),
		orderedUnique(b.ServiceWithdrawals, func(a, z ServiceInstanceID) int { return strings.Compare(string(a), string(z)) }),
		orderedUnique(b.CapabilityUpserts, func(a, z CapabilityInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) }),
		orderedUnique(b.CapabilityWithdrawals, func(a, z CapabilityInstanceID) int { return strings.Compare(string(a), string(z)) }),
		orderedUnique(b.GenerationFences, compareFence),
	)
	return bestError(errs...)
}

func orderedUnique[T any](values []T, compare func(T, T) int) error {
	duplicate, ordered := duplicateAndOrder(values, compare)
	var duplicateErr, orderErr error
	if duplicate {
		duplicateErr = errID(DuplicateKey, "collection")
	}
	if !ordered {
		orderErr = errID(NoncanonicalOrder, "collection")
	}
	return bestError(duplicateErr, orderErr)
}

func compareSource(a, b SourceDescriptor) int {
	if cmp := strings.Compare(string(a.SourceID), string(b.SourceID)); cmp != 0 {
		return cmp
	}
	return strings.Compare(string(a.SourceEpochID), string(b.SourceEpochID))
}

func compareFence(a, b GenerationFence) int {
	if cmp := strings.Compare(string(a.SourceID), string(b.SourceID)); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(string(a.SourceEpochID), string(b.SourceEpochID)); cmp != 0 {
		return cmp
	}
	return compareUint64(a.DriverGeneration, b.DriverGeneration)
}

type cursorKey struct {
	source     SourceID
	epoch      SourceEpochID
	generation Uint64
}

type sourceKey struct {
	source SourceID
	epoch  SourceEpochID
}

// PublicationKernel stores exactly one current immutable snapshot for one
// asset. Apply serializes all mutations under one lock and commits only after
// complete post-state validation and canonical serialization succeed.
type PublicationKernel struct {
	mu        sync.RWMutex
	asset     AssetID
	registry  *Registry
	current   *Snapshot
	canonical []byte
	replays   map[cursorKey]publicationResult
}

type publicationResult struct {
	snapshot  Snapshot
	canonical []byte
}

func NewPublicationKernel(asset AssetID, validators ...PackValidator) (*PublicationKernel, error) {
	if err := asset.Validate(); err != nil {
		return nil, err
	}
	registry, err := NewRegistry(validators...)
	if err != nil {
		return nil, err
	}
	return &PublicationKernel{asset: asset, registry: registry, replays: make(map[cursorKey]publicationResult)}, nil
}

// Current returns a deep copy and a copy of its canonical bytes.
func (k *PublicationKernel) Current() (Snapshot, []byte, bool) {
	if k == nil {
		return Snapshot{}, nil, false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.current == nil {
		return Snapshot{}, nil, false
	}
	return cloneSnapshot(*k.current), append([]byte(nil), k.canonical...), true
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	raw, _ := json.Marshal(snapshot)
	var clone Snapshot
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func clonePublicationBatch(batch PublicationBatch) PublicationBatch {
	raw, _ := json.Marshal(batch)
	var clone PublicationBatch
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func (k *PublicationKernel) replayResult(key cursorKey) (Snapshot, []byte, error) {
	result, ok := k.replays[key]
	if !ok {
		return Snapshot{}, nil, errID(DanglingReference, "publication replay")
	}
	return cloneSnapshot(result.snapshot), append([]byte(nil), result.canonical...), nil
}

// Apply validates and applies one complete patch. publicationMonotonic is an
// explicit caller-supplied point; the kernel never reads an implicit clock.
func (k *PublicationKernel) Apply(batch PublicationBatch, publicationMonotonic MonotonicPoint) (Snapshot, []byte, error) {
	if k == nil {
		return Snapshot{}, nil, errID(InvalidValue, "publication kernel")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := batch.validateStructure(false); err != nil {
		return Snapshot{}, nil, err
	}
	if err := batch.BatchDigest.Validate(); err != nil {
		return Snapshot{}, nil, err
	}
	if err := publicationMonotonic.Validate(); err != nil {
		return Snapshot{}, nil, err
	}
	if batch.AssetID != k.asset {
		return Snapshot{}, nil, errID(InvalidValue, "publication asset")
	}

	key := cursorKey{batch.SourceID, batch.SourceEpochID, batch.DriverGeneration}
	cursors := cursorMap(k.current)
	if cursor, ok := cursors[key]; ok {
		cmp := compareUint64(batch.Sequence, cursor.LastSequence)
		if cmp < 0 {
			return Snapshot{}, nil, errID(SequenceConflict, "publication sequence")
		}
		if cmp == 0 {
			digest, digestErr := batch.computedDigestUnchecked()
			if digestErr != nil || digest != batch.BatchDigest || batch.BatchDigest != cursor.LastBatchDigest {
				return Snapshot{}, nil, errID(SequenceConflict, "publication replay bytes")
			}
			return k.replayResult(key)
		}
		if cursor.Fenced {
			return Snapshot{}, nil, errID(StaleDriverGeneration, "fenced publication cursor")
		}
	} else if batch.Sequence != "1" {
		return Snapshot{}, nil, errID(SequenceConflict, "first publication sequence")
	}
	for knownKey := range cursors {
		if knownKey.source == batch.SourceID && knownKey.epoch == batch.SourceEpochID && compareUint64(batch.DriverGeneration, knownKey.generation) < 0 {
			return Snapshot{}, nil, errID(StaleDriverGeneration, "regressed publication generation")
		}
	}
	digest, err := batch.computedDigestUnchecked()
	if err != nil {
		return Snapshot{}, nil, err
	}
	if digest != batch.BatchDigest {
		return Snapshot{}, nil, errID(DigestMismatch, "publication batch")
	}
	currentRevision := Uint64("0")
	if k.current != nil {
		currentRevision = k.current.Revisions.Semantic
	}
	if batch.ExpectedSemanticRevision != currentRevision {
		return Snapshot{}, nil, errID(RevisionConflict, "semantic revision")
	}
	// Detach every pointer and slice before state construction. The digest was
	// already verified while holding the publication lock.
	batch = clonePublicationBatch(batch)

	working := emptySnapshot(k.asset)
	if k.current != nil {
		working = cloneSnapshot(*k.current)
	}
	changed, err := k.applyTo(&working, batch)
	if err != nil {
		return Snapshot{}, nil, err
	}
	working.Contract = ContractKernelV1
	working.AssetID = k.asset
	working.EvaluatedAt = batch.ObservedAt
	working.EvaluateMonotonic = publicationMonotonic
	working.Revisions = nextRevisionVector(working.Revisions, k.current == nil, changed)
	working.SnapshotID = "snapshot:pending"
	id, err := working.computedID()
	if err != nil {
		return Snapshot{}, nil, err
	}
	working.SnapshotID = id
	if err := working.Validate(); err != nil {
		return Snapshot{}, nil, err
	}
	canonical, err := CanonicalJSON(working)
	if err != nil {
		return Snapshot{}, nil, err
	}
	k.current = &working
	k.canonical = append([]byte(nil), canonical...)
	k.replays[key] = publicationResult{snapshot: cloneSnapshot(working), canonical: append([]byte(nil), canonical...)}
	return cloneSnapshot(working), append([]byte(nil), canonical...), nil
}

type componentChanges struct {
	identity, facts, services, capabilities bool
}

func nextRevisionVector(old RevisionVector, first bool, changed componentChanges) RevisionVector {
	if first {
		return RevisionVector{Semantic: "1", Identity: "1", Facts: "1", Services: "1", Capabilities: "1"}
	}
	result := old
	result.Semantic = increment(old.Semantic)
	if changed.identity {
		result.Identity = increment(old.Identity)
	}
	if changed.facts {
		result.Facts = increment(old.Facts)
	}
	if changed.services {
		result.Services = increment(old.Services)
	}
	if changed.capabilities {
		result.Capabilities = increment(old.Capabilities)
	}
	return result
}

func increment(value Uint64) Uint64 {
	n, _ := u64(value)
	if n == nil {
		return "1"
	}
	n.Add(n, bigOne)
	return Uint64(n.String())
}

var bigOne = func() *big.Int {
	return big.NewInt(1)
}()

func emptySnapshot(asset AssetID) Snapshot {
	return Snapshot{
		Contract: ContractKernelV1, AssetID: asset,
		Sources: []SourceDescriptor{}, Bindings: []NativeBinding{}, IdentityLinks: []IdentityLink{}, Facts: []FactEnvelope{},
		Services: []ServiceInstance{}, Capabilities: []CapabilityInstance{}, Fences: []GenerationFence{}, Cursors: []PublicationCursor{},
	}
}

func cursorMap(snapshot *Snapshot) map[cursorKey]PublicationCursor {
	result := make(map[cursorKey]PublicationCursor)
	if snapshot == nil {
		return result
	}
	for _, cursor := range snapshot.Cursors {
		result[cursorKey{cursor.SourceID, cursor.SourceEpochID, cursor.DriverGeneration}] = cursor
	}
	return result
}

func (k *PublicationKernel) applyTo(snapshot *Snapshot, batch PublicationBatch) (componentChanges, error) {
	before := cloneSnapshot(*snapshot)
	sources := make(map[sourceKey]SourceDescriptor, len(snapshot.Sources))
	bindings := make(map[NativeBindingID]NativeBinding, len(snapshot.Bindings))
	links := make(map[NativeBindingID]IdentityLink, len(snapshot.IdentityLinks))
	services := make(map[ServiceInstanceID]ServiceInstance, len(snapshot.Services))
	capabilities := make(map[CapabilityInstanceID]CapabilityInstance, len(snapshot.Capabilities))
	fences := make(map[cursorKey]GenerationFence, len(snapshot.Fences))
	cursors := cursorMap(snapshot)
	candidates := make(map[CandidateID]FactCandidate)
	originalCandidates := make(map[CandidateID]FactCandidate)
	for _, source := range snapshot.Sources {
		sources[sourceKey{source.SourceID, source.SourceEpochID}] = source
	}
	for _, binding := range snapshot.Bindings {
		bindings[binding.BindingID] = binding
	}
	for _, link := range snapshot.IdentityLinks {
		links[link.BindingID] = link
	}
	for _, service := range snapshot.Services {
		services[service.InstanceID] = service
	}
	for _, capability := range snapshot.Capabilities {
		capabilities[capability.InstanceID] = capability
	}
	originalBindings := cloneMap(bindings)
	originalServices := cloneMap(services)
	originalCapabilities := cloneMap(capabilities)
	for _, fence := range snapshot.Fences {
		fences[cursorKey{fence.SourceID, fence.SourceEpochID, fence.DriverGeneration}] = fence
	}
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			candidates[candidate.CandidateID] = candidate
			originalCandidates[candidate.CandidateID] = candidate
		}
	}

	retirements := make(map[SourceEpochID]struct{}, len(batch.SourceRetirements))
	for _, epoch := range batch.SourceRetirements {
		retirements[epoch] = struct{}{}
		source, ok := sources[sourceKey{batch.SourceID, epoch}]
		if !ok {
			return componentChanges{}, errID(DanglingReference, "source retirement")
		}
		if source.State != SourceCurrent {
			return componentChanges{}, errID(StaleSourceEpoch, "source retirement")
		}
	}
	for _, source := range snapshot.Sources {
		if source.SourceID == batch.SourceID && source.State == SourceCurrent && source.SourceEpochID != batch.SourceEpochID {
			if _, retiring := retirements[source.SourceEpochID]; !retiring {
				return componentChanges{}, errID(StaleSourceEpoch, "current source epoch")
			}
		}
	}
	if source, ok := sources[sourceKey{batch.SourceID, batch.SourceEpochID}]; ok && source.State != SourceCurrent {
		return componentChanges{}, errID(StaleSourceEpoch, "publication source epoch")
	}
	for _, source := range batch.SourceUpserts {
		if source.SourceID != batch.SourceID {
			return componentChanges{}, errID(InvalidValue, "source ownership")
		}
		key := sourceKey{source.SourceID, source.SourceEpochID}
		if existing, ok := sources[key]; ok {
			if existing.State != SourceCurrent {
				return componentChanges{}, errID(StaleSourceEpoch, "source upsert")
			}
			changed, err := validateRevisionedUpsert(existing, source)
			if err != nil {
				return componentChanges{}, err
			}
			if changed {
				sources[key] = source
			}
		} else {
			if source.Revision != "1" {
				return componentChanges{}, errID(RevisionConflict, "new source revision")
			}
			sources[key] = source
		}
	}
	if _, ok := sources[sourceKey{batch.SourceID, batch.SourceEpochID}]; !ok {
		return componentChanges{}, errID(DanglingReference, "publication source")
	}

	currentGenerations := []Uint64{}
	for key, cursor := range cursors {
		if key.source == batch.SourceID && key.epoch == batch.SourceEpochID && !cursor.Fenced {
			currentGenerations = append(currentGenerations, key.generation)
		}
	}
	for _, generation := range currentGenerations {
		cmp := compareUint64(batch.DriverGeneration, generation)
		if cmp < 0 {
			return componentChanges{}, errID(StaleDriverGeneration, "publication generation")
		}
		if cmp > 0 && !containsFence(batch.GenerationFences, batch.SourceID, batch.SourceEpochID, generation) {
			return componentChanges{}, errID(GenerationTransitionIncomplete, "missing generation fence")
		}
	}
	for _, fence := range batch.GenerationFences {
		if fence.SourceID != batch.SourceID || fence.SourceEpochID != batch.SourceEpochID {
			return componentChanges{}, errID(InvalidValue, "generation fence ownership")
		}
		fenceKey := cursorKey{fence.SourceID, fence.SourceEpochID, fence.DriverGeneration}
		cursor, ok := cursors[fenceKey]
		if !ok {
			return componentChanges{}, errID(DanglingReference, "generation fence cursor")
		}
		if cursor.Fenced || fences[fenceKey].Revision != "" {
			return componentChanges{}, errID(StaleDriverGeneration, "generation fence")
		}
		if compareUint64(fence.DriverGeneration, batch.DriverGeneration) > 0 {
			return componentChanges{}, errID(StaleDriverGeneration, "future generation fence")
		}
		if fence.Revision != "1" {
			return componentChanges{}, errID(RevisionConflict, "new fence revision")
		}
	}
	if containsFence(batch.GenerationFences, batch.SourceID, batch.SourceEpochID, batch.DriverGeneration) &&
		(len(batch.BindingUpserts) != 0 || len(batch.IdentityLinkUpserts) != 0 || len(batch.FactUpserts) != 0 || len(batch.ServiceUpserts) != 0 || len(batch.CapabilityUpserts) != 0) {
		return componentChanges{}, errID(StaleDriverGeneration, "fenced header generation upsert")
	}

	// Apply the narrower generation transition first. If the same batch also
	// retires the epoch, retirement is the stronger final tombstone state.
	for _, fence := range batch.GenerationFences {
		fenceKey := cursorKey{fence.SourceID, fence.SourceEpochID, fence.DriverGeneration}
		fences[fenceKey] = fence
		cursor := cursors[fenceKey]
		cursor.Fenced = true
		cursors[fenceKey] = cursor
		for _, binding := range bindings {
			if binding.SourceID == fence.SourceID && binding.SourceEpochID == fence.SourceEpochID && binding.DriverGeneration == fence.DriverGeneration {
				if err := transitionBinding(binding, BindingFenced, fence.Evidence, bindings, links, services, capabilities); err != nil {
					return componentChanges{}, err
				}
			}
		}
	}
	for epoch := range retirements {
		key := sourceKey{batch.SourceID, epoch}
		source := sources[key]
		source.State = SourceRetired
		source.Revision = increment(source.Revision)
		sources[key] = source
		for _, binding := range bindings {
			if binding.SourceID == batch.SourceID && binding.SourceEpochID == epoch {
				if err := transitionBinding(binding, BindingRetired, nil, bindings, links, services, capabilities); err != nil {
					return componentChanges{}, err
				}
			}
		}
	}

	for _, binding := range batch.BindingUpserts {
		if binding.AssetID != batch.AssetID || binding.SourceID != batch.SourceID || binding.SourceEpochID != batch.SourceEpochID || binding.DriverGeneration != batch.DriverGeneration {
			return componentChanges{}, errID(InvalidValue, "binding ownership")
		}
		if existing, ok := bindings[binding.BindingID]; ok {
			if existing.State != BindingCurrent {
				return componentChanges{}, lifecycleError(existing)
			}
			if existing.AssetID != binding.AssetID || existing.SourceID != binding.SourceID || existing.SourceEpochID != binding.SourceEpochID || existing.DriverGeneration != binding.DriverGeneration || existing.NativeResource != binding.NativeResource {
				return componentChanges{}, errID(IdentityNotQualified, "binding reuse")
			}
			changed, err := validateRevisionedUpsert(existing, binding)
			if err != nil {
				return componentChanges{}, err
			}
			if changed {
				bindings[binding.BindingID] = binding
			}
		} else {
			if binding.Revision != "1" {
				return componentChanges{}, errID(RevisionConflict, "new binding revision")
			}
			bindings[binding.BindingID] = binding
		}
	}
	for _, link := range batch.IdentityLinkUpserts {
		binding, ok := bindings[link.BindingID]
		if !ok {
			return componentChanges{}, errID(DanglingReference, "identity binding")
		}
		if link.AssetID != batch.AssetID || binding.SourceID != batch.SourceID || binding.SourceEpochID != batch.SourceEpochID || binding.DriverGeneration != batch.DriverGeneration || binding.State != BindingCurrent {
			return componentChanges{}, lifecycleOrValue(binding, "identity ownership")
		}
		if existing, ok := links[link.BindingID]; ok {
			if existing.State == LinkWithdrawn {
				return componentChanges{}, lifecycleError(binding)
			}
			changed, err := validateRevisionedUpsert(existing, link)
			if err != nil {
				return componentChanges{}, err
			}
			if changed {
				links[link.BindingID] = link
			}
		} else {
			if link.Revision != "1" {
				return componentChanges{}, errID(RevisionConflict, "new identity revision")
			}
			links[link.BindingID] = link
		}
	}

	upsertedCandidates := make(map[CandidateID]struct{}, len(batch.FactUpserts))
	for _, candidate := range batch.FactUpserts {
		if err := k.registry.ValidateFactCandidate(candidate); err != nil {
			return componentChanges{}, err
		}
		if candidate.Quality.Assertion == AssertionObserved {
			if err := validateObservedForBatch(candidate, batch, bindings); err != nil {
				return componentChanges{}, err
			}
		}
		if existing, ok := candidates[candidate.CandidateID]; ok {
			changed, err := validateRevisionedUpsert(existing, candidate)
			if err != nil {
				return componentChanges{}, err
			}
			if changed {
				candidates[candidate.CandidateID] = candidate
			}
		} else {
			if candidate.Revision != "1" {
				return componentChanges{}, errID(RevisionConflict, "new candidate revision")
			}
			candidates[candidate.CandidateID] = candidate
		}
		upsertedCandidates[candidate.CandidateID] = struct{}{}
	}
	for _, id := range batch.FactWithdrawals {
		candidate, ok := originalCandidates[id]
		if !ok {
			return componentChanges{}, errID(DanglingReference, "fact withdrawal")
		}
		if candidate.Quality.Assertion == AssertionObserved {
			binding, bindingOK := originalBindings[*candidate.BindingID]
			if !bindingOK || !withdrawalCovered(batch, binding.SourceID, binding.SourceEpochID, binding.DriverGeneration) {
				return componentChanges{}, errID(InvalidValue, "fact withdrawal ownership")
			}
		}
		delete(candidates, id)
	}

	for _, service := range batch.ServiceUpserts {
		if err := k.registry.ValidateService(service); err != nil {
			return componentChanges{}, err
		}
		binding, ok := bindings[service.BindingID]
		if !ok {
			return componentChanges{}, errID(DanglingReference, "service binding")
		}
		if service.AssetID != batch.AssetID || binding.SourceID != batch.SourceID || service.SourceEpochID != batch.SourceEpochID || service.DriverGeneration != batch.DriverGeneration || binding.State != BindingCurrent {
			return componentChanges{}, lifecycleOrValue(binding, "service ownership")
		}
		if existing, ok := originalServices[service.InstanceID]; ok {
			if existing.Availability == AvailabilityWithdrawn {
				return componentChanges{}, lifecycleError(binding)
			}
			changed, err := validateRevisionedUpsert(existing, service)
			if err != nil {
				return componentChanges{}, err
			}
			if changed {
				services[service.InstanceID] = service
			}
		} else {
			if service.Revision != "1" {
				return componentChanges{}, errID(RevisionConflict, "new service revision")
			}
			services[service.InstanceID] = service
		}
	}
	for _, id := range batch.ServiceWithdrawals {
		original, ok := originalServices[id]
		if !ok {
			return componentChanges{}, errID(DanglingReference, "service withdrawal")
		}
		binding, bindingOK := originalBindings[original.BindingID]
		if !bindingOK || original.SourceEpochID != binding.SourceEpochID || original.DriverGeneration != binding.DriverGeneration || !withdrawalCovered(batch, binding.SourceID, original.SourceEpochID, original.DriverGeneration) {
			return componentChanges{}, errID(InvalidValue, "service withdrawal ownership")
		}
		service := services[id]
		if service.Availability != AvailabilityWithdrawn {
			service.Availability, service.Revision = AvailabilityWithdrawn, increment(service.Revision)
			services[id] = service
		}
	}

	for _, capability := range batch.CapabilityUpserts {
		if err := k.registry.ValidateCapability(capability); err != nil {
			return componentChanges{}, err
		}
		binding, ok := bindings[capability.BindingID]
		if !ok {
			return componentChanges{}, errID(DanglingReference, "capability binding")
		}
		service, serviceOK := services[capability.ServiceInstance]
		if !serviceOK {
			return componentChanges{}, errID(DanglingReference, "capability service")
		}
		serviceForValidation := service
		if original, existed := originalServices[capability.ServiceInstance]; existed {
			serviceForValidation = original
		}
		if capability.AssetID != batch.AssetID || binding.SourceID != batch.SourceID || capability.SourceEpochID != batch.SourceEpochID || capability.DriverGeneration != batch.DriverGeneration || binding.State != BindingCurrent || serviceForValidation.Availability == AvailabilityWithdrawn {
			return componentChanges{}, lifecycleOrValue(binding, "capability ownership")
		}
		if existing, ok := originalCapabilities[capability.InstanceID]; ok {
			if existing.Availability == AvailabilityWithdrawn {
				return componentChanges{}, lifecycleError(binding)
			}
			changed, err := validateRevisionedUpsert(existing, capability)
			if err != nil {
				return componentChanges{}, err
			}
			if changed {
				capabilities[capability.InstanceID] = capability
			}
		} else {
			if capability.Revision != "1" {
				return componentChanges{}, errID(RevisionConflict, "new capability revision")
			}
			capabilities[capability.InstanceID] = capability
		}
	}
	for _, id := range batch.CapabilityWithdrawals {
		original, ok := originalCapabilities[id]
		if !ok {
			return componentChanges{}, errID(DanglingReference, "capability withdrawal")
		}
		binding, bindingOK := originalBindings[original.BindingID]
		if !bindingOK || original.SourceEpochID != binding.SourceEpochID || original.DriverGeneration != binding.DriverGeneration || !withdrawalCovered(batch, binding.SourceID, original.SourceEpochID, original.DriverGeneration) {
			return componentChanges{}, errID(InvalidValue, "capability withdrawal ownership")
		}
		capability := capabilities[id]
		if capability.Availability != AvailabilityWithdrawn {
			capability.Availability, capability.Revision = AvailabilityWithdrawn, increment(capability.Revision)
			capabilities[id] = capability
		}
	}

	if err := closeCandidateGraph(candidates, upsertedCandidates, bindings); err != nil {
		return componentChanges{}, err
	}
	factEnvelopes, err := rebuildEnvelopes(snapshot.Facts, snapshot.AssetID, candidates)
	if err != nil {
		return componentChanges{}, err
	}

	newCursor := PublicationCursor{SourceID: batch.SourceID, SourceEpochID: batch.SourceEpochID, DriverGeneration: batch.DriverGeneration, LastSequence: batch.Sequence, LastBatchDigest: batch.BatchDigest, Fenced: containsFence(batch.GenerationFences, batch.SourceID, batch.SourceEpochID, batch.DriverGeneration)}
	cursors[cursorKey{batch.SourceID, batch.SourceEpochID, batch.DriverGeneration}] = newCursor

	normalizeObjectRevisions(before.Sources, sources, func(value SourceDescriptor) sourceKey {
		return sourceKey{value.SourceID, value.SourceEpochID}
	})
	normalizeObjectRevisions(before.Bindings, bindings, func(value NativeBinding) NativeBindingID { return value.BindingID })
	normalizeObjectRevisions(before.IdentityLinks, links, func(value IdentityLink) NativeBindingID { return value.BindingID })
	normalizeObjectRevisions(before.Services, services, func(value ServiceInstance) ServiceInstanceID { return value.InstanceID })
	normalizeObjectRevisions(before.Capabilities, capabilities, func(value CapabilityInstance) CapabilityInstanceID { return value.InstanceID })

	snapshot.Sources = sortedMapValues(sources, func(a, z SourceDescriptor) int { return compareSource(a, z) })
	snapshot.Bindings = sortedMapValues(bindings, func(a, z NativeBinding) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) })
	snapshot.IdentityLinks = sortedMapValues(links, func(a, z IdentityLink) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) })
	snapshot.Facts = factEnvelopes
	snapshot.Services = sortedMapValues(services, func(a, z ServiceInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) })
	snapshot.Capabilities = sortedMapValues(capabilities, func(a, z CapabilityInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) })
	snapshot.Fences = sortedMapValues(fences, compareFence)
	snapshot.Cursors = sortedMapValues(cursors, compareCursor)

	changed := componentChanges{
		identity:     !reflect.DeepEqual(before.Sources, snapshot.Sources) || !reflect.DeepEqual(before.Bindings, snapshot.Bindings) || !reflect.DeepEqual(before.IdentityLinks, snapshot.IdentityLinks) || !reflect.DeepEqual(before.Fences, snapshot.Fences),
		facts:        !reflect.DeepEqual(before.Facts, snapshot.Facts),
		services:     !reflect.DeepEqual(before.Services, snapshot.Services),
		capabilities: !reflect.DeepEqual(before.Capabilities, snapshot.Capabilities),
	}
	return changed, nil
}

func containsFence(fences []GenerationFence, source SourceID, epoch SourceEpochID, generation Uint64) bool {
	for _, fence := range fences {
		if fence.SourceID == source && fence.SourceEpochID == epoch && fence.DriverGeneration == generation {
			return true
		}
	}
	return false
}

func withdrawalCovered(batch PublicationBatch, source SourceID, epoch SourceEpochID, generation Uint64) bool {
	if source != batch.SourceID {
		return false
	}
	if epoch == batch.SourceEpochID && generation == batch.DriverGeneration {
		return true
	}
	if containsFence(batch.GenerationFences, source, epoch, generation) {
		return true
	}
	if _, retiring := findRetirement(batch.SourceRetirements, epoch); retiring {
		return true
	}
	return false
}

func findRetirement(retirements []SourceEpochID, epoch SourceEpochID) (SourceEpochID, bool) {
	for _, retired := range retirements {
		if retired == epoch {
			return retired, true
		}
	}
	return "", false
}

func transitionBinding(binding NativeBinding, state BindingState, evidence []EvidenceRef, bindings map[NativeBindingID]NativeBinding, links map[NativeBindingID]IdentityLink, services map[ServiceInstanceID]ServiceInstance, capabilities map[CapabilityInstanceID]CapabilityInstance) error {
	if binding.State != state {
		binding.State, binding.Revision = state, increment(binding.Revision)
		bindings[binding.BindingID] = binding
	}
	if link, ok := links[binding.BindingID]; ok {
		basis, err := unionEvidence(link.Basis, evidence)
		if err != nil {
			return err
		}
		if link.State != LinkWithdrawn || !reflect.DeepEqual(link.Basis, basis) {
			link.Basis = basis
			link.State, link.Revision = LinkWithdrawn, increment(link.Revision)
			links[binding.BindingID] = link
		}
	}
	for id, service := range services {
		if service.BindingID == binding.BindingID && service.Availability != AvailabilityWithdrawn {
			service.Availability, service.Revision = AvailabilityWithdrawn, increment(service.Revision)
			services[id] = service
		}
	}
	for id, capability := range capabilities {
		if capability.BindingID == binding.BindingID && capability.Availability != AvailabilityWithdrawn {
			capability.Availability, capability.Revision = AvailabilityWithdrawn, increment(capability.Revision)
			capabilities[id] = capability
		}
	}
	return nil
}

func unionEvidence(sets ...[]EvidenceRef) ([]EvidenceRef, error) {
	var result []EvidenceRef
	for _, set := range sets {
		result = append(result, set...)
	}
	sort.Slice(result, func(i, j int) bool { return compareEvidence(result[i], result[j]) < 0 })
	dedup := result[:0]
	for _, item := range result {
		if len(dedup) == 0 || compareEvidence(dedup[len(dedup)-1], item) != 0 {
			dedup = append(dedup, item)
		}
	}
	if err := validateEvidenceSet(dedup, 1, 32); err != nil {
		return nil, err
	}
	return append([]EvidenceRef(nil), dedup...), nil
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	result := make(map[K]V, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func normalizeObjectRevisions[K comparable, V any](before []V, after map[K]V, key func(V) K) {
	previous := make(map[K]V, len(before))
	for _, value := range before {
		previous[key(value)] = value
	}
	for objectKey, value := range after {
		old, ok := previous[objectKey]
		if !ok {
			continue
		}
		oldValue, nextValue := reflect.ValueOf(old), reflect.ValueOf(value)
		oldRevision := oldValue.FieldByName("Revision").Interface().(Uint64)
		oldClone, nextClone := reflect.New(oldValue.Type()).Elem(), reflect.New(nextValue.Type()).Elem()
		oldClone.Set(oldValue)
		nextClone.Set(nextValue)
		oldClone.FieldByName("Revision").Set(reflect.ValueOf(Uint64("0")))
		nextClone.FieldByName("Revision").Set(reflect.ValueOf(Uint64("0")))
		revision := oldRevision
		if !reflect.DeepEqual(oldClone.Interface(), nextClone.Interface()) {
			revision = increment(oldRevision)
		}
		nextClone.FieldByName("Revision").Set(reflect.ValueOf(revision))
		after[objectKey] = nextClone.Interface().(V)
	}
}

func validateRevisionedUpsert[T any](old, next T) (bool, error) {
	if reflect.DeepEqual(old, next) {
		return false, nil
	}
	oldValue, nextValue := reflect.ValueOf(old), reflect.ValueOf(next)
	oldRevision := oldValue.FieldByName("Revision").Interface().(Uint64)
	nextRevision := nextValue.FieldByName("Revision").Interface().(Uint64)
	oldClone, nextClone := reflect.New(oldValue.Type()).Elem(), reflect.New(nextValue.Type()).Elem()
	oldClone.Set(oldValue)
	nextClone.Set(nextValue)
	oldClone.FieldByName("Revision").Set(reflect.ValueOf(Uint64("0")))
	nextClone.FieldByName("Revision").Set(reflect.ValueOf(Uint64("0")))
	if reflect.DeepEqual(oldClone.Interface(), nextClone.Interface()) || nextRevision != increment(oldRevision) {
		return false, errID(RevisionConflict, "object revision")
	}
	return true, nil
}

func lifecycleError(binding NativeBinding) error {
	if binding.State == BindingRetired {
		return errID(StaleSourceEpoch, "retired binding")
	}
	return errID(StaleDriverGeneration, "fenced binding")
}

func lifecycleOrValue(binding NativeBinding, detail string) error {
	if binding.State != BindingCurrent {
		return lifecycleError(binding)
	}
	return errID(InvalidValue, detail)
}

func validateObservedForBatch(candidate FactCandidate, batch PublicationBatch, bindings map[NativeBindingID]NativeBinding) error {
	binding, ok := bindings[*candidate.BindingID]
	if !ok {
		return errID(DanglingReference, "candidate binding")
	}
	if candidate.Origin.SourceID == nil || *candidate.Origin.SourceID != batch.SourceID || *candidate.SourceEpochID != batch.SourceEpochID || *candidate.DriverGeneration != batch.DriverGeneration || binding.SourceID != batch.SourceID || binding.SourceEpochID != batch.SourceEpochID || binding.DriverGeneration != batch.DriverGeneration {
		return errID(InvalidValue, "candidate ownership")
	}
	if binding.State != BindingCurrent {
		return lifecycleError(binding)
	}
	return nil
}

func sortedMapValues[K comparable, V any](values map[K]V, compare func(V, V) int) []V {
	result := make([]V, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return compare(result[i], result[j]) < 0 })
	return result
}

func compareCursor(a, b PublicationCursor) int {
	if cmp := strings.Compare(string(a.SourceID), string(b.SourceID)); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(string(a.SourceEpochID), string(b.SourceEpochID)); cmp != 0 {
		return cmp
	}
	return compareUint64(a.DriverGeneration, b.DriverGeneration)
}

func closeCandidateGraph(candidates map[CandidateID]FactCandidate, upserted map[CandidateID]struct{}, bindings map[NativeBindingID]NativeBinding) error {
	if len(candidates) > maxDerivationNodes {
		return errID(BoundsExceeded, "derivation nodes")
	}
	resolver := candidateGraphResolver{
		candidates: candidates,
		bindings:   bindings,
		states:     make(map[CandidateID]uint8, len(candidates)),
		results:    make(map[CandidateID]candidateGraphResult, len(candidates)),
	}
	ids := make([]CandidateID, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var upsertErrors []error
	var remove []CandidateID
	for _, id := range ids {
		if _, err := resolver.resolve(id); err != nil {
			if _, isUpsert := upserted[id]; isUpsert {
				upsertErrors = append(upsertErrors, err)
			} else {
				remove = append(remove, id)
			}
		}
	}
	if err := bestError(upsertErrors...); err != nil {
		return err
	}
	for _, id := range remove {
		delete(candidates, id)
	}
	return nil
}

type candidateGraphResult struct {
	paths []SourcePathRef
	depth int
	err   error
}

type candidateGraphResolver struct {
	candidates map[CandidateID]FactCandidate
	bindings   map[NativeBindingID]NativeBinding
	states     map[CandidateID]uint8
	results    map[CandidateID]candidateGraphResult
}

func (r *candidateGraphResolver) resolve(id CandidateID) (candidateGraphResult, error) {
	if r.states[id] == 1 {
		return candidateGraphResult{}, errID(DerivationCycle, "candidate graph")
	}
	if r.states[id] == 2 {
		result := r.results[id]
		return result, result.err
	}
	candidate, ok := r.candidates[id]
	if !ok {
		return candidateGraphResult{}, errID(DanglingReference, "derivation input")
	}
	r.states[id] = 1
	result := candidateGraphResult{depth: 1}
	finish := func(err error) (candidateGraphResult, error) {
		result.err = err
		r.states[id], r.results[id] = 2, result
		return result, err
	}
	if candidate.Quality.Assertion == AssertionObserved {
		if candidate.BindingID == nil || candidate.SourceEpochID == nil || candidate.DriverGeneration == nil || candidate.Origin.SourceID == nil {
			return finish(errID(DanglingReference, "observed candidate path"))
		}
		binding, bindingOK := r.bindings[*candidate.BindingID]
		if !bindingOK {
			return finish(errID(DanglingReference, "observed candidate binding"))
		}
		if binding.State != BindingCurrent {
			return finish(lifecycleError(binding))
		}
		if binding.SourceID != *candidate.Origin.SourceID || binding.SourceEpochID != *candidate.SourceEpochID || binding.DriverGeneration != *candidate.DriverGeneration {
			return finish(errID(DanglingReference, "observed candidate path"))
		}
		result.paths = []SourcePathRef{{BindingID: *candidate.BindingID, SourceID: *candidate.Origin.SourceID, SourceEpochID: *candidate.SourceEpochID, DriverGeneration: *candidate.DriverGeneration}}
		return finish(nil)
	}
	if candidate.Derivation == nil {
		return finish(errID(DanglingReference, "derived candidate"))
	}
	var paths []SourcePathRef
	for _, input := range candidate.Derivation.Inputs {
		dependency, dependencyOK := r.candidates[input.CandidateID]
		if !dependencyOK || dependency.Revision != input.CandidateRevision {
			return finish(errID(DanglingReference, "derivation input"))
		}
		resolved, err := r.resolve(input.CandidateID)
		if err != nil {
			return finish(err)
		}
		if !reflect.DeepEqual(resolved.paths, input.SourcePaths) {
			return finish(errID(DanglingReference, "derivation source paths"))
		}
		if resolved.depth+1 > result.depth {
			result.depth = resolved.depth + 1
		}
		paths = append(paths, resolved.paths...)
	}
	if result.depth > maxDerivationDepth {
		return finish(errID(DerivationCycle, "derivation depth"))
	}
	sort.Slice(paths, func(i, j int) bool { return compareSourcePath(paths[i], paths[j]) < 0 })
	dedup := paths[:0]
	for _, path := range paths {
		if len(dedup) == 0 || compareSourcePath(dedup[len(dedup)-1], path) != 0 {
			dedup = append(dedup, path)
		}
	}
	if len(dedup) > 32 {
		return finish(errID(BoundsExceeded, "derivation source paths"))
	}
	result.paths = append([]SourcePathRef(nil), dedup...)
	return finish(nil)
}

func rebuildEnvelopes(previous []FactEnvelope, asset AssetID, candidates map[CandidateID]FactCandidate) ([]FactEnvelope, error) {
	oldByKey := make(map[string]FactEnvelope, len(previous))
	for _, envelope := range previous {
		key, err := factKeyIdentity(envelope.Key)
		if err != nil {
			return nil, err
		}
		oldByKey[key] = envelope
	}
	type group struct {
		key        FactKey
		candidates []FactCandidate
	}
	groups := make(map[string]*group)
	for _, candidate := range candidates {
		key, err := factKeyIdentity(candidate.Key)
		if err != nil {
			return nil, err
		}
		if groups[key] == nil {
			groups[key] = &group{key: candidate.Key}
		}
		groups[key].candidates = append(groups[key].candidates, candidate)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]FactEnvelope, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		sort.Slice(group.candidates, func(i, j int) bool { return group.candidates[i].CandidateID < group.candidates[j].CandidateID })
		conflicts, err := deriveConflicts(asset, group.key, group.candidates)
		if err != nil {
			return nil, err
		}
		revision := Uint64("1")
		if old, ok := oldByKey[key]; ok {
			revision = old.Revision
			if !reflect.DeepEqual(old.Candidates, group.candidates) || !reflect.DeepEqual(old.Conflicts, conflicts) {
				revision = increment(old.Revision)
			}
		}
		result = append(result, FactEnvelope{AssetID: asset, Key: group.key, Candidates: group.candidates, Conflicts: conflicts, Revision: revision})
	}
	return result, nil
}

func factKeyIdentity(key FactKey) (string, error) {
	canonical, err := CanonicalJSON(key)
	return string(canonical), err
}

func (s Snapshot) computedID() (SnapshotID, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", errID(InvalidJSON, "snapshot identity")
	}
	node, duplicate, err := parseJSON(raw)
	if err != nil {
		return "", err
	}
	if duplicate {
		return "", errID(DuplicateKey, "snapshot identity")
	}
	filtered := node.object[:0]
	for _, member := range node.object {
		if member.key != "snapshot_id" {
			filtered = append(filtered, member)
		}
	}
	node.object = filtered
	var out bytes.Buffer
	if err := writeCanonical(&out, node); err != nil {
		return "", errID(InvalidJSON, "snapshot identity")
	}
	sum := sha256.Sum256(out.Bytes())
	return SnapshotID("sha256:" + hex.EncodeToString(sum[:])), nil
}

func (s Snapshot) Validate() error {
	var errs []error
	if s.Contract != ContractKernelV1 {
		errs = append(errs, errID(InvalidContract, "snapshot"))
	}
	errs = append(errs, s.SnapshotID.Validate(), s.AssetID.Validate(), s.Revisions.Validate(), s.EvaluatedAt.Validate(), s.EvaluateMonotonic.Validate())
	for _, collection := range []any{s.Sources, s.Bindings, s.IdentityLinks, s.Facts, s.Services, s.Capabilities, s.Fences, s.Cursors} {
		if reflect.ValueOf(collection).IsNil() {
			errs = append(errs, errID(MissingMember, "snapshot collection"))
		}
	}
	if len(s.Sources) > maxSnapshotSources || len(s.Bindings) > maxSnapshotBindings || len(s.IdentityLinks) > maxSnapshotIdentity || len(s.Facts) > maxSnapshotEnvelopes || len(s.Services) > maxSnapshotServices || len(s.Capabilities) > maxSnapshotCapabilities || len(s.Fences) > maxSnapshotFences || len(s.Cursors) > maxSnapshotCursors {
		errs = append(errs, errID(BoundsExceeded, "snapshot collection"))
	}
	errs = append(errs,
		orderedUnique(s.Sources, compareSource),
		orderedUnique(s.Bindings, func(a, z NativeBinding) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) }),
		orderedUnique(s.IdentityLinks, func(a, z IdentityLink) int { return strings.Compare(string(a.BindingID), string(z.BindingID)) }),
		orderedUnique(s.Facts, compareEnvelope),
		orderedUnique(s.Services, func(a, z ServiceInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) }),
		orderedUnique(s.Capabilities, func(a, z CapabilityInstance) int { return strings.Compare(string(a.InstanceID), string(z.InstanceID)) }),
		orderedUnique(s.Fences, compareFence), orderedUnique(s.Cursors, compareCursor),
	)
	sources := make(map[sourceKey]SourceDescriptor, len(s.Sources))
	currentSources := make(map[SourceID]int)
	for _, source := range s.Sources {
		errs = append(errs, source.Validate())
		sources[sourceKey{source.SourceID, source.SourceEpochID}] = source
		if source.State == SourceCurrent {
			currentSources[source.SourceID]++
			if currentSources[source.SourceID] > 1 {
				errs = append(errs, errID(StaleSourceEpoch, "multiple current source epochs"))
			}
		}
	}
	fenceByKey := make(map[cursorKey]GenerationFence, len(s.Fences))
	for _, fence := range s.Fences {
		errs = append(errs, fence.Validate())
		_, ok := sources[sourceKey{fence.SourceID, fence.SourceEpochID}]
		if !ok {
			errs = append(errs, errID(DanglingReference, "fence source"))
		}
		fenceByKey[cursorKey{fence.SourceID, fence.SourceEpochID, fence.DriverGeneration}] = fence
	}
	cursorByKey := make(map[cursorKey]PublicationCursor, len(s.Cursors))
	unfenced := make(map[[2]string]int)
	for _, cursor := range s.Cursors {
		errs = append(errs, cursor.Validate())
		source, ok := sources[sourceKey{cursor.SourceID, cursor.SourceEpochID}]
		if !ok {
			errs = append(errs, errID(DanglingReference, "cursor source"))
		}
		key := cursorKey{cursor.SourceID, cursor.SourceEpochID, cursor.DriverGeneration}
		cursorByKey[key] = cursor
		_, fenced := fenceByKey[key]
		if cursor.Fenced != fenced {
			errs = append(errs, errID(DanglingReference, "cursor fence"))
		}
		if !cursor.Fenced && ok && source.State == SourceCurrent {
			group := [2]string{string(cursor.SourceID), string(cursor.SourceEpochID)}
			unfenced[group]++
			if unfenced[group] > 1 {
				errs = append(errs, errID(StaleDriverGeneration, "multiple current generations"))
			}
		}
	}
	for key := range fenceByKey {
		cursor, ok := cursorByKey[key]
		if !ok || !cursor.Fenced {
			errs = append(errs, errID(DanglingReference, "fence cursor"))
		}
	}
	bindings := make(map[NativeBindingID]NativeBinding, len(s.Bindings))
	for _, binding := range s.Bindings {
		errs = append(errs, binding.Validate())
		bindings[binding.BindingID] = binding
		source, ok := sources[sourceKey{binding.SourceID, binding.SourceEpochID}]
		if !ok || binding.AssetID != s.AssetID {
			errs = append(errs, errID(DanglingReference, "binding source"))
			continue
		}
		key := cursorKey{binding.SourceID, binding.SourceEpochID, binding.DriverGeneration}
		cursor, cursorOK := cursorByKey[key]
		switch binding.State {
		case BindingCurrent:
			if source.State != SourceCurrent || !cursorOK || cursor.Fenced {
				errs = append(errs, errID(DanglingReference, "current binding lifecycle"))
			}
		case BindingFenced:
			if _, ok := fenceByKey[key]; !ok {
				errs = append(errs, errID(DanglingReference, "fenced binding fence"))
			}
		case BindingRetired:
			if source.State != SourceRetired {
				errs = append(errs, errID(DanglingReference, "retired binding source"))
			}
		}
	}
	links := make(map[NativeBindingID]IdentityLink, len(s.IdentityLinks))
	for _, link := range s.IdentityLinks {
		errs = append(errs, link.Validate())
		links[link.BindingID] = link
		binding, ok := bindings[link.BindingID]
		if !ok || link.AssetID != s.AssetID {
			errs = append(errs, errID(DanglingReference, "identity binding"))
		} else if link.State != LinkWithdrawn && binding.State != BindingCurrent {
			errs = append(errs, errID(DanglingReference, "actionable identity binding"))
		}
	}
	services := make(map[ServiceInstanceID]ServiceInstance, len(s.Services))
	for _, service := range s.Services {
		errs = append(errs, service.Validate())
		services[service.InstanceID] = service
		binding, ok := bindings[service.BindingID]
		if !ok || service.AssetID != s.AssetID || binding.SourceEpochID != service.SourceEpochID || binding.DriverGeneration != service.DriverGeneration {
			errs = append(errs, errID(DanglingReference, "service binding"))
		} else if service.Availability != AvailabilityWithdrawn && binding.State != BindingCurrent {
			errs = append(errs, errID(DanglingReference, "actionable service binding"))
		}
	}
	for _, capability := range s.Capabilities {
		errs = append(errs, capability.Validate())
		binding, bindingOK := bindings[capability.BindingID]
		service, serviceOK := services[capability.ServiceInstance]
		if !bindingOK || !serviceOK || capability.AssetID != s.AssetID || binding.SourceEpochID != capability.SourceEpochID || binding.DriverGeneration != capability.DriverGeneration || service.BindingID != capability.BindingID {
			errs = append(errs, errID(DanglingReference, "capability references"))
		} else if capability.Availability != AvailabilityWithdrawn && (binding.State != BindingCurrent || service.Availability == AvailabilityWithdrawn) {
			errs = append(errs, errID(DanglingReference, "actionable capability binding"))
		} else if binding.State != BindingCurrent && (capability.Availability != AvailabilityWithdrawn || service.Availability != AvailabilityWithdrawn) {
			errs = append(errs, errID(DanglingReference, "capability tombstone"))
		}
	}
	candidates := make(map[CandidateID]FactCandidate)
	for _, envelope := range s.Facts {
		errs = append(errs, envelope.Validate())
		if envelope.AssetID != s.AssetID {
			errs = append(errs, errID(InvalidValue, "fact asset"))
		}
		for _, candidate := range envelope.Candidates {
			if _, exists := candidates[candidate.CandidateID]; exists {
				errs = append(errs, errID(DuplicateKey, "candidate id"))
			}
			candidates[candidate.CandidateID] = candidate
		}
	}
	if len(candidates) > maxDerivationNodes {
		errs = append(errs, errID(BoundsExceeded, "candidate nodes"))
	} else {
		errs = append(errs, closeCandidateGraphStrict(candidates, bindings))
	}
	if expected, err := s.computedID(); err != nil {
		errs = append(errs, err)
	} else if expected != s.SnapshotID {
		errs = append(errs, errID(DigestMismatch, "snapshot identity"))
	}
	return bestError(errs...)
}

func compareEnvelope(a, b FactEnvelope) int {
	aKey, _ := factKeyIdentity(a.Key)
	bKey, _ := factKeyIdentity(b.Key)
	return strings.Compare(aKey, bKey)
}

func closeCandidateGraphStrict(candidates map[CandidateID]FactCandidate, bindings map[NativeBindingID]NativeBinding) error {
	all := make(map[CandidateID]struct{}, len(candidates))
	for id := range candidates {
		all[id] = struct{}{}
	}
	return closeCandidateGraph(candidates, all, bindings)
}
