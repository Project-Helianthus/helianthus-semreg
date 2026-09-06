package operation

import (
	"bytes"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Record validates and appends one immutable terminal record to an admission.
// Dispatch, acknowledgement, and outcome evidence is supplied by the external
// native owner and must bind canonically to the admitted intent, revisions, and
// route. laterSnapshot is required exactly when readback is present; the kernel
// does not retain or invent a snapshot store.
func (k *Kernel) Record(admission *Admission, record ExecutionRecord, laterSnapshot *semreg.Snapshot) (ExecutionRecord, error) {
	if k == nil || admission == nil {
		return ExecutionRecord{}, opError(semreg.InvalidValue, "execution admission")
	}
	record = clone(record)
	if err := record.Validate(); err != nil {
		return ExecutionRecord{}, err
	}
	k.mu.Lock()
	owned := k.admissions[record.Intent.IdempotencyKey] == admission
	k.mu.Unlock()
	if !owned {
		return ExecutionRecord{}, opError(semreg.DanglingReference, "execution admission")
	}

	admission.mu.Lock()
	defer admission.mu.Unlock()
	canonical, err := semreg.CanonicalJSON(record)
	if err != nil {
		return ExecutionRecord{}, err
	}
	if admission.record != nil {
		if bytes.Equal(admission.recordBytes, canonical) {
			return clone(*admission.record), nil
		}
		return ExecutionRecord{}, opError(semreg.SequenceConflict, "idempotent outcome bytes")
	}
	if err := sameRecordAdmission(record, admission); err != nil {
		return ExecutionRecord{}, err
	}
	if record.Acknowledgement != nil && record.Dispatch != nil {
		if err := acknowledgementAfterStart(record.Dispatch.Started.EvaluatedAt, record.Acknowledgement.At); err != nil {
			return ExecutionRecord{}, err
		}
	}
	if record.Readback != nil {
		if laterSnapshot == nil {
			return ExecutionRecord{}, opError(semreg.DanglingReference, "readback snapshot")
		}
		snapshot := clone(*laterSnapshot)
		if err := k.validateReadback(record, snapshot); err != nil {
			return ExecutionRecord{}, err
		}
	}
	stored := clone(record)
	admission.record = &stored
	admission.recordBytes = append([]byte(nil), canonical...)
	return clone(stored), nil
}

// RecordRejection appends a stable admission rejection without route or
// dispatch evidence. Reusing the idempotency key with different valid bytes is
// a sequence_conflict; an identical record is returned byte-for-byte.
func (k *Kernel) RecordRejection(intent Intent, errorID semreg.ErrorID, evidence []semreg.EvidenceRef) (ExecutionRecord, error) {
	if k == nil {
		return ExecutionRecord{}, opError(semreg.DefinitionOwnerMissing, "operation kernel")
	}
	intent = clone(intent)
	if err := mostSpecific(k.ValidateIntent(intent), errorID.Validate()); err != nil {
		return ExecutionRecord{}, err
	}
	record := ExecutionRecord{
		Contract: ContractOperationV1,
		Intent:   clone(intent),
		Outcome:  OutcomeRejected,
		ErrorID: func() *semreg.ErrorID {
			value := errorID
			return &value
		}(),
		OutcomeEvidence: clone(evidence),
	}
	if record.OutcomeEvidence == nil {
		record.OutcomeEvidence = []semreg.EvidenceRef{}
	}
	if err := record.Validate(); err != nil {
		return ExecutionRecord{}, err
	}
	intentBytes, err := semreg.CanonicalJSON(intent)
	if err != nil {
		return ExecutionRecord{}, err
	}
	recordBytes, err := semreg.CanonicalJSON(record)
	if err != nil {
		return ExecutionRecord{}, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if prior, exists := k.admissions[intent.IdempotencyKey]; exists {
		prior.mu.Lock()
		defer prior.mu.Unlock()
		if !bytes.Equal(prior.intentBytes, intentBytes) {
			return ExecutionRecord{}, opError(semreg.SequenceConflict, "idempotency key")
		}
		if prior.record != nil && bytes.Equal(prior.recordBytes, recordBytes) {
			return clone(*prior.record), nil
		}
		return ExecutionRecord{}, opError(semreg.SequenceConflict, "idempotent outcome bytes")
	}
	stored := clone(record)
	k.admissions[intent.IdempotencyKey] = &Admission{
		intent:      clone(intent),
		intentBytes: append([]byte(nil), intentBytes...),
		record:      &stored,
		recordBytes: append([]byte(nil), recordBytes...),
	}
	return clone(stored), nil
}

func acknowledgementAfterStart(start, acknowledgement semreg.TimePoint) error {
	if start.ClockID != acknowledgement.ClockID {
		return nil
	}
	return wallPointChronological(start, acknowledgement, false)
}

func wallPointChronological(start, end semreg.TimePoint, strict bool) error {
	// Same named wall-clock realizations are comparable. Convert through the
	// UTC helper when applicable; the interval arithmetic is otherwise equal.
	if start.ClockID == "clock.utc" && end.ClockID == "clock.utc" {
		return wallChronological(start, end, strict)
	}
	startCopy, endCopy := start, end
	startCopy.ClockID, endCopy.ClockID = "clock.utc", "clock.utc"
	return wallChronological(startCopy, endCopy, strict)
}

func (k *Kernel) validateReadback(record ExecutionRecord, snapshot semreg.Snapshot) error {
	readback := record.Readback
	if readback == nil || record.Route == nil || record.AdmittedRevision == nil || record.Dispatch == nil {
		return opError(semreg.InvalidOutcome, "readback context")
	}
	var errs []error
	errs = append(errs, snapshot.Validate())
	if snapshot.SnapshotID != readback.SnapshotID || snapshot.Revisions != readback.Revisions {
		errs = append(errs, opError(semreg.DanglingReference, "exact readback snapshot"))
	}
	if compareUint64(readback.Revisions.Semantic, record.AdmittedRevision.Semantic) <= 0 {
		errs = append(errs, opError(semreg.InvalidOutcome, "readback semantic revision"))
	}
	if readback.SourceEpochID != record.Route.SourceEpochID {
		errs = append(errs, opError(semreg.StaleSourceEpoch, "readback route epoch"))
	}
	if readback.DriverGeneration != record.Route.DriverGeneration {
		errs = append(errs, opError(semreg.StaleDriverGeneration, "readback route generation"))
	}
	if readback.BindingID != record.Route.BindingID || readback.SourceID != record.Route.SourceID {
		errs = append(errs, opError(semreg.InvalidOutcome, "readback route"))
	}
	if err := mostSpecific(errs...); err != nil {
		return err
	}

	candidate, envelope, found := candidateByID(snapshot, readback.CandidateID)
	if !found || candidate.Revision != readback.CandidateRevision {
		return opError(semreg.DanglingReference, "readback candidate")
	}
	if candidate.Quality.Assertion != semreg.AssertionObserved || candidate.BindingID == nil || candidate.SourceEpochID == nil || candidate.DriverGeneration == nil {
		return opError(semreg.InvalidOutcome, "readback observed candidate")
	}
	if *candidate.SourceEpochID != readback.SourceEpochID {
		return opError(semreg.StaleSourceEpoch, "readback candidate epoch")
	}
	if *candidate.DriverGeneration != readback.DriverGeneration {
		return opError(semreg.StaleDriverGeneration, "readback candidate generation")
	}
	if *candidate.BindingID != readback.BindingID || candidate.Origin.SourceID == nil || *candidate.Origin.SourceID != readback.SourceID {
		return opError(semreg.InvalidOutcome, "readback candidate route")
	}
	if !factKeyEqual(candidate.Key, record.Intent.ExpectedEffect.Fact) {
		return opError(semreg.InvalidOutcome, "readback fact")
	}
	if err := snapshotRouteCurrent(snapshot, *record.Route); err != nil {
		return err
	}
	view, err := semreg.EvaluateSnapshot(snapshot, readback.Evaluation)
	if err != nil {
		return opError(semreg.InvalidOutcome, "readback evaluation")
	}
	var evaluated *semreg.EvaluatedFact
	for index := range view.Facts {
		if view.Facts[index].CandidateID == candidate.CandidateID {
			evaluated = &view.Facts[index]
			break
		}
	}
	if evaluated == nil || evaluated.CandidateRevision != candidate.Revision ||
		candidate.Quality.Qualification != semreg.QualificationQualified || candidate.Quality.Promotion != semreg.PromotionPromoted ||
		candidate.Quality.Validity != semreg.ValidityGood || evaluated.Freshness != semreg.FreshnessFresh ||
		evaluated.EffectiveAvailability != semreg.AvailabilityAvailable || candidateInOpenConflict(envelope, candidate.CandidateID) {
		return opError(semreg.InvalidOutcome, "readback eligibility")
	}
	if record.Dispatch.Completed == nil {
		return opError(semreg.InvalidOutcome, "readback dispatch completion")
	}
	if err := receiptAfterDispatch(*record.Dispatch.Completed, candidate.Times); err != nil {
		return err
	}
	validator, ok := k.validators[record.Intent.Kind.Pack]
	if !ok {
		return opError(semreg.DefinitionOwnerMissing, "readback operation pack")
	}
	validator.mu.Lock()
	relation, hookErr := validator.hook.EvaluateReadback(clone(record.Intent), clone(candidate))
	validator.mu.Unlock()
	if hookErr != nil {
		return opError(semreg.InvalidOutcome, "pack readback evaluation")
	}
	if relation != readback.Relation {
		return opError(semreg.InvalidOutcome, "readback relation")
	}
	return nil
}

func candidateByID(snapshot semreg.Snapshot, id semreg.CandidateID) (semreg.FactCandidate, semreg.FactEnvelope, bool) {
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			if candidate.CandidateID == id {
				return candidate, envelope, true
			}
		}
	}
	return semreg.FactCandidate{}, semreg.FactEnvelope{}, false
}

func snapshotRouteCurrent(snapshot semreg.Snapshot, route Route) error {
	var sourceFound, bindingFound, serviceFound, capabilityFound bool
	for _, source := range snapshot.Sources {
		if source.SourceID == route.SourceID && source.SourceEpochID == route.SourceEpochID {
			sourceFound = source.State == semreg.SourceCurrent
		}
	}
	for _, fence := range snapshot.Fences {
		if fence.SourceID == route.SourceID && fence.SourceEpochID == route.SourceEpochID && fence.DriverGeneration == route.DriverGeneration {
			return opError(semreg.StaleDriverGeneration, "readback fenced route")
		}
	}
	for _, binding := range snapshot.Bindings {
		if binding.BindingID == route.BindingID {
			if binding.SourceEpochID != route.SourceEpochID {
				return opError(semreg.StaleSourceEpoch, "readback binding epoch")
			}
			if binding.DriverGeneration != route.DriverGeneration || binding.State == semreg.BindingFenced {
				return opError(semreg.StaleDriverGeneration, "readback binding generation")
			}
			bindingFound = binding.State == semreg.BindingCurrent && binding.SourceID == route.SourceID
		}
	}
	for _, service := range snapshot.Services {
		if service.InstanceID == route.ServiceInstance {
			serviceFound = service.BindingID == route.BindingID && service.SourceEpochID == route.SourceEpochID &&
				service.DriverGeneration == route.DriverGeneration && service.Availability != semreg.AvailabilityWithdrawn
		}
	}
	for _, capability := range snapshot.Capabilities {
		if capability.InstanceID == route.CapabilityInstance {
			capabilityFound = capability.ServiceInstance == route.ServiceInstance && capability.BindingID == route.BindingID &&
				capability.SourceEpochID == route.SourceEpochID && capability.DriverGeneration == route.DriverGeneration &&
				capability.Availability != semreg.AvailabilityWithdrawn
		}
	}
	if !sourceFound {
		return opError(semreg.StaleSourceEpoch, "readback source")
	}
	if !bindingFound || !serviceFound || !capabilityFound {
		return opError(semreg.InvalidOutcome, "readback current route")
	}
	return nil
}

func receiptAfterDispatch(completed semreg.EvaluationContext, times semreg.Times) error {
	if completed.EvaluateMonotonic.ClockEpochID == times.ReceiptMonotonic.ClockEpochID {
		if compareUint64(times.ReceiptMonotonic.Nanoseconds, completed.EvaluateMonotonic.Nanoseconds) <= 0 {
			return opError(semreg.InvalidOutcome, "readback receipt not post-dispatch")
		}
		return nil
	}
	if err := wallChronological(completed.EvaluatedAt, times.ReceivedAt, true); err != nil {
		return opError(semreg.InvalidOutcome, "readback receipt wall order")
	}
	return nil
}
