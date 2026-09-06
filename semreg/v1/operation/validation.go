package operation

import (
	"bytes"
	"encoding/json"
	"math/big"
	"reflect"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

var operationErrorRank = map[semreg.ErrorID]int{
	semreg.InvalidJSON: 1, semreg.DuplicateKey: 2, semreg.InvalidContract: 3,
	semreg.MissingMember: 4, semreg.UnknownMember: 5, semreg.InvalidIdentifier: 6,
	semreg.InvalidDecimal: 7, semreg.InvalidValue: 8, semreg.InvalidTime: 9,
	semreg.InvalidEvidence: 10, semreg.InvalidEnum: 11, semreg.BoundsExceeded: 12,
	semreg.NoncanonicalOrder: 13, semreg.DigestMismatch: 14, semreg.DanglingReference: 15,
	semreg.DerivationCycle: 16, semreg.StaleSourceEpoch: 17,
	semreg.StaleDriverGeneration: 18, semreg.SequenceConflict: 19,
	semreg.RevisionConflict: 20, semreg.IncomparableClockEpoch: 21,
	semreg.GenerationTransitionIncomplete: 22, semreg.DefinitionOwnerConflict: 23,
	semreg.DefinitionOwnerMissing: 24, semreg.IdentityNotQualified: 25,
	semreg.CapabilityNotQualified: 26, semreg.CapabilityUnavailable: 27,
	semreg.AuthorityMissing: 28, semreg.DeadlineExpired: 29,
	semreg.PreconditionFailed: 30, semreg.RouteSelectionForbidden: 31,
	semreg.AmbiguousRoute: 32, semreg.RetryForbidden: 33, semreg.InvalidOutcome: 34,
	semreg.EchoSuppressed: 35, semreg.CausalBudgetExceeded: 36,
	semreg.ProjectionIncomplete: 37, semreg.AliasNotRoutable: 38,
}

func opError(id semreg.ErrorID, detail string) error {
	return &semreg.Error{ID: id, Detail: detail}
}

func mostSpecific(errs ...error) error {
	var best error
	bestRank := int(^uint(0) >> 1)
	for _, candidate := range errs {
		if candidate == nil {
			continue
		}
		rank, ok := operationErrorRank[semreg.ErrorIdentifier(candidate)]
		if !ok {
			rank = operationErrorRank[semreg.InvalidValue]
		}
		if rank < bestRank {
			best, bestRank = candidate, rank
		}
	}
	return best
}

func positive(v semreg.Uint64) bool {
	if err := v.Validate(); err != nil {
		return false
	}
	n, ok := new(big.Int).SetString(string(v), 10)
	return ok && n.Sign() > 0
}

func compareUint64(left, right semreg.Uint64) int {
	a, aok := new(big.Int).SetString(string(left), 10)
	b, bok := new(big.Int).SetString(string(right), 10)
	if !aok || !bok {
		return strings.Compare(string(left), string(right))
	}
	return a.Cmp(b)
}

func requiredEvidence(evidence []semreg.EvidenceRef, detail string) error {
	var errs []error
	seen := make(map[string]struct{}, len(evidence))
	var prior *semreg.EvidenceRef
	if evidence == nil {
		errs = append(errs, opError(semreg.MissingMember, detail))
	}
	if len(evidence) == 0 {
		errs = append(errs, opError(semreg.InvalidEvidence, detail))
	}
	if len(evidence) > 32 {
		errs = append(errs, opError(semreg.BoundsExceeded, detail))
	}
	for _, item := range evidence {
		errs = append(errs, item.Validate())
		if evidenceCollectionKeyValid(item) {
			key := string(item.Owner) + "\x00" + string(item.Kind) + "\x00" + string(item.Contract) + "\x00" + string(item.Digest)
			if _, duplicate := seen[key]; duplicate {
				errs = append(errs, opError(semreg.DuplicateKey, detail))
			}
			seen[key] = struct{}{}
			if prior != nil && compareEvidence(*prior, item) > 0 {
				errs = append(errs, opError(semreg.NoncanonicalOrder, detail))
			}
			copy := item
			prior = &copy
		}
	}
	return mostSpecific(errs...)
}

// Evidence collection identity is exactly these four projected fields. Access
// and redaction remain independently validated non-key metadata.
func evidenceCollectionKeyValid(evidence semreg.EvidenceRef) bool {
	return evidence.Owner.Validate() == nil && evidence.Kind.Validate() == nil &&
		evidence.Contract.Validate() == nil && evidence.Digest.Validate() == nil
}

func optionalEvidence(evidence []semreg.EvidenceRef, detail string) error {
	if evidence == nil {
		return opError(semreg.MissingMember, detail)
	}
	if len(evidence) == 0 {
		return nil
	}
	return requiredEvidence(evidence, detail)
}

func compareEvidence(left, right semreg.EvidenceRef) int {
	if cmp := strings.Compare(string(left.Owner), string(right.Owner)); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(string(left.Kind), string(right.Kind)); cmp != 0 {
		return cmp
	}
	if cmp := strings.Compare(string(left.Contract), string(right.Contract)); cmp != 0 {
		return cmp
	}
	return strings.Compare(string(left.Digest), string(right.Digest))
}

func (r CapabilityRequirement) Validate() error {
	var instanceErr error
	if r.InstanceID != nil {
		instanceErr = r.InstanceID.Validate()
	}
	return mostSpecific(r.Pack.Validate(), r.DefinitionID.Validate(), r.Versions.Validate(), instanceErr)
}

func (p Precondition) Validate() error {
	var revisionErr error
	if !positive(p.CandidateRevision) {
		revisionErr = opError(semreg.InvalidIdentifier, "precondition candidate revision")
	}
	return mostSpecific(p.Fact.Validate(), p.CandidateID.Validate(), revisionErr, p.Operator.Validate(), p.Expected.Validate())
}

func (e ExpectedEffect) Validate() error {
	return mostSpecific(e.Rule.Validate(), e.Fact.Validate(), e.Operator.Validate(), e.Expected.Validate())
}

func (i Intent) Validate() error {
	var errs []error
	if i.Contract != ContractOperationV1 {
		errs = append(errs, opError(semreg.InvalidContract, "intent"))
	}
	errs = append(errs, i.IntentID.Validate(), i.Kind.Validate(), i.ExpectedEffect.Validate(), i.AssetID.Validate(),
		i.RequiredCapability.Validate(), i.Authority.Validate(), i.Causal.Validate(), i.ExpectedSourceEpochID.Validate(),
		i.IdempotencyKey.Validate(), i.Deadline.Validate())
	if i.Deadline.ClockID != "clock.utc" {
		errs = append(errs, opError(semreg.InvalidTime, "intent deadline clock"))
	}
	if i.ExpectedEffect.Rule.Pack != i.Kind.Pack {
		errs = append(errs, opError(semreg.InvalidValue, "effect rule pack"))
	}
	if !positive(i.ExpectedSemanticRevision) || !positive(i.ExpectedCapabilityRevision) ||
		!positive(i.ExpectedCapabilityInstanceRevision) || !positive(i.ExpectedDriverGeneration) {
		errs = append(errs, opError(semreg.InvalidIdentifier, "intent expected revision"))
	}
	if i.Arguments == nil {
		errs = append(errs, opError(semreg.MissingMember, "intent arguments"))
	}
	if len(i.Arguments) > 64 {
		errs = append(errs, opError(semreg.BoundsExceeded, "intent arguments"))
	}
	seenArguments := make(map[semreg.DefinitionID]struct{}, len(i.Arguments))
	for index, field := range i.Arguments {
		errs = append(errs, field.Validate())
		if field.ID.Validate() == nil {
			if _, duplicate := seenArguments[field.ID]; duplicate {
				errs = append(errs, opError(semreg.DuplicateKey, "intent arguments"))
			}
			seenArguments[field.ID] = struct{}{}
		}
		if index > 0 {
			cmp := strings.Compare(string(i.Arguments[index-1].ID), string(field.ID))
			if cmp > 0 {
				errs = append(errs, opError(semreg.NoncanonicalOrder, "intent arguments"))
			}
		}
	}
	if i.Preconditions == nil {
		errs = append(errs, opError(semreg.MissingMember, "intent preconditions"))
	}
	if len(i.Preconditions) > 64 {
		errs = append(errs, opError(semreg.BoundsExceeded, "intent preconditions"))
	}
	var prior *Precondition
	seenPreconditions := make(map[string]struct{}, len(i.Preconditions))
	for _, precondition := range i.Preconditions {
		errs = append(errs, precondition.Validate())
		key, valid := preconditionCollectionKey(precondition)
		if valid {
			if _, duplicate := seenPreconditions[string(key)]; duplicate {
				errs = append(errs, opError(semreg.DuplicateKey, "intent preconditions"))
			}
			seenPreconditions[string(key)] = struct{}{}
			if prior != nil && comparePrecondition(*prior, precondition) > 0 {
				errs = append(errs, opError(semreg.NoncanonicalOrder, "intent preconditions"))
			}
			copy := precondition
			prior = &copy
		}
	}
	return mostSpecific(errs...)
}

func preconditionCollectionKey(p Precondition) ([]byte, bool) {
	if p.Fact.Validate() != nil || p.CandidateID.Validate() != nil || !positive(p.CandidateRevision) {
		return nil, false
	}
	fact, err := semreg.CanonicalJSON(p.Fact)
	if err != nil {
		return nil, false
	}
	return []byte(string(fact) + "\x00" + string(p.CandidateID) + "\x00" + string(p.CandidateRevision)), true
}

func comparePrecondition(left, right Precondition) int {
	leftFact, leftErr := semreg.CanonicalJSON(left.Fact)
	rightFact, rightErr := semreg.CanonicalJSON(right.Fact)
	if leftErr == nil && rightErr == nil {
		if cmp := bytes.Compare(leftFact, rightFact); cmp != 0 {
			return cmp
		}
	}
	if cmp := strings.Compare(string(left.CandidateID), string(right.CandidateID)); cmp != 0 {
		return cmp
	}
	return compareUint64(left.CandidateRevision, right.CandidateRevision)
}

func (r Route) Validate() error {
	var generationErr error
	if !positive(r.DriverGeneration) {
		generationErr = opError(semreg.InvalidIdentifier, "route generation")
	}
	return mostSpecific(r.CapabilityInstance.Validate(), r.ServiceInstance.Validate(), r.BindingID.Validate(),
		r.SourceID.Validate(), r.SourceEpochID.Validate(), generationErr)
}

func (d DispatchEvidence) Validate() error {
	var errs []error
	errs = append(errs, d.AttemptID.Validate(), d.Started.Validate(), requiredEvidence(d.Evidence, "dispatch evidence"))
	if d.Completed != nil {
		errs = append(errs, d.Completed.Validate(), contextsChronological(d.Started, *d.Completed, false))
	}
	if d.Delivery != DeliveryNotSent && d.Delivery != DeliverySent && d.Delivery != DeliveryUnknown {
		errs = append(errs, opError(semreg.InvalidEnum, "dispatch delivery"))
	}
	if d.Delivery == DeliveryNotSent && d.PossibleSideEffect {
		errs = append(errs, opError(semreg.InvalidOutcome, "not-sent side effect"))
	}
	return mostSpecific(errs...)
}

func (a Acknowledgement) Validate() error {
	var enumErr error
	if a.State != AckAccepted && a.State != AckRejected && a.State != AckProvisional {
		enumErr = opError(semreg.InvalidEnum, "acknowledgement state")
	}
	return mostSpecific(enumErr, a.At.Validate(), requiredEvidence(a.Evidence, "acknowledgement evidence"))
}

func (r Readback) Validate() error {
	var errs []error
	errs = append(errs, r.SnapshotID.Validate(), r.Revisions.Validate(), r.CandidateID.Validate(), r.BindingID.Validate(),
		r.SourceID.Validate(), r.SourceEpochID.Validate(), r.Evaluation.Validate(), requiredEvidence(r.Evidence, "readback evidence"))
	if !positive(r.CandidateRevision) || !positive(r.DriverGeneration) {
		errs = append(errs, opError(semreg.InvalidIdentifier, "readback revision"))
	}
	if r.Relation != ReadbackConfirms && r.Relation != ReadbackContradicts && r.Relation != ReadbackInconclusive {
		errs = append(errs, opError(semreg.InvalidEnum, "readback relation"))
	}
	return mostSpecific(errs...)
}

func (r ExecutionRecord) Validate() error {
	var errs []error
	if r.Contract != ContractOperationV1 {
		errs = append(errs, opError(semreg.InvalidContract, "execution record"))
	}
	errs = append(errs, r.Intent.Validate(), optionalEvidence(r.OutcomeEvidence, "outcome evidence"))
	if r.AdmittedAt != nil {
		errs = append(errs, r.AdmittedAt.Validate())
	}
	if r.AdmittedRevision != nil {
		errs = append(errs, r.AdmittedRevision.Validate())
	}
	if r.Route != nil {
		errs = append(errs, r.Route.Validate())
	}
	if r.Dispatch != nil {
		errs = append(errs, r.Dispatch.Validate())
	}
	if r.Acknowledgement != nil {
		errs = append(errs, r.Acknowledgement.Validate())
	}
	if r.Readback != nil {
		errs = append(errs, r.Readback.Validate())
	}
	if r.ErrorID != nil {
		errs = append(errs, r.ErrorID.Validate())
	}
	if r.Outcome != OutcomeRejected && r.Outcome != OutcomeFailedNoContact && r.Outcome != OutcomeAcknowledgedUnverified &&
		r.Outcome != OutcomeApplied && r.Outcome != OutcomeNoEffect && r.Outcome != OutcomeConflict && r.Outcome != OutcomeIndeterminate {
		errs = append(errs, opError(semreg.InvalidEnum, "execution outcome"))
	}
	errs = append(errs, r.outcomeCombinationError())
	return mostSpecific(errs...)
}

func (r ExecutionRecord) outcomeCombinationError() error {
	if r.Outcome == OutcomeRejected {
		if r.ErrorID == nil || r.AdmittedAt != nil || r.AdmittedRevision != nil || r.Route != nil || r.Dispatch != nil || r.Acknowledgement != nil || r.Readback != nil {
			return opError(semreg.InvalidOutcome, "rejected evidence")
		}
		return nil
	}
	if r.ErrorID != nil || r.AdmittedAt == nil || r.AdmittedRevision == nil || r.Route == nil || len(r.OutcomeEvidence) == 0 {
		return opError(semreg.InvalidOutcome, "terminal evidence")
	}
	if r.Dispatch == nil {
		return opError(semreg.InvalidOutcome, "missing dispatch")
	}
	switch r.Outcome {
	case OutcomeFailedNoContact:
		if r.Dispatch.Delivery != DeliveryNotSent || r.Dispatch.PossibleSideEffect || r.Acknowledgement != nil || r.Readback != nil {
			return opError(semreg.InvalidOutcome, "failed-no-contact evidence")
		}
	case OutcomeAcknowledgedUnverified:
		if r.Dispatch.Delivery != DeliverySent || !r.Dispatch.PossibleSideEffect || r.Acknowledgement == nil ||
			(r.Acknowledgement.State != AckAccepted && r.Acknowledgement.State != AckProvisional) ||
			(r.Readback != nil && r.Readback.Relation != ReadbackInconclusive) {
			return opError(semreg.InvalidOutcome, "acknowledged-unverified evidence")
		}
	case OutcomeApplied:
		if r.Dispatch.Delivery != DeliverySent || !r.Dispatch.PossibleSideEffect || r.Dispatch.Completed == nil || r.Readback == nil || r.Readback.Relation != ReadbackConfirms {
			return opError(semreg.InvalidOutcome, "applied evidence")
		}
	case OutcomeNoEffect:
		if r.Dispatch.Delivery != DeliverySent || (r.Readback != nil && r.Readback.Relation == ReadbackConfirms) {
			return opError(semreg.InvalidOutcome, "no-effect evidence")
		}
	case OutcomeConflict:
		if r.Dispatch.Delivery != DeliverySent || !r.Dispatch.PossibleSideEffect || r.Readback == nil || r.Readback.Relation != ReadbackContradicts {
			return opError(semreg.InvalidOutcome, "conflict evidence")
		}
	case OutcomeIndeterminate:
		if r.Dispatch.Delivery == DeliveryNotSent || !r.Dispatch.PossibleSideEffect || (r.Readback != nil && r.Readback.Relation != ReadbackInconclusive) {
			return opError(semreg.InvalidOutcome, "indeterminate evidence")
		}
	}
	return nil
}

func contextsChronological(start, end semreg.EvaluationContext, strict bool) error {
	if start.EvaluateMonotonic.ClockEpochID == end.EvaluateMonotonic.ClockEpochID {
		cmp := compareUint64(end.EvaluateMonotonic.Nanoseconds, start.EvaluateMonotonic.Nanoseconds)
		if cmp < 0 || strict && cmp == 0 {
			return opError(semreg.InvalidOutcome, "monotonic chronology")
		}
		return nil
	}
	return wallChronological(start.EvaluatedAt, end.EvaluatedAt, strict)
}

func wallChronological(start, end semreg.TimePoint, strict bool) error {
	if start.ClockID != "clock.utc" || end.ClockID != "clock.utc" {
		return opError(semreg.IncomparableClockEpoch, "wall chronology")
	}
	startNS, sok := new(big.Int).SetString(string(start.UnixNanoseconds), 10)
	endNS, eok := new(big.Int).SetString(string(end.UnixNanoseconds), 10)
	startU, suok := new(big.Int).SetString(string(start.UncertaintyNS), 10)
	endU, euok := new(big.Int).SetString(string(end.UncertaintyNS), 10)
	if !sok || !eok || !suok || !euok {
		return opError(semreg.InvalidTime, "wall chronology")
	}
	latestStart := new(big.Int).Add(startNS, startU)
	earliestEnd := new(big.Int).Sub(endNS, endU)
	cmp := earliestEnd.Cmp(latestStart)
	if cmp < 0 || strict && cmp == 0 {
		return opError(semreg.InvalidOutcome, "wall chronology")
	}
	return nil
}

func DecodeIntent(raw []byte) (Intent, error) {
	return decodeOperationDocument[Intent](raw, operationPreconditionWireError(raw, false))
}

func DecodeExecutionRecord(raw []byte) (ExecutionRecord, error) {
	return decodeOperationDocument[ExecutionRecord](raw, operationPreconditionWireError(raw, true))
}

// Operation wrappers own their document discriminator. Keep the root decoder's
// strict syntax, shape and independent collection diagnostics, including errors
// in nested evidence metadata, without teaching it about operation types.
func decodeOperationDocument[T semreg.Record](raw []byte, collectionErr error) (T, error) {
	value, decodeErr := semreg.Decode[T](raw)
	var object map[string]json.RawMessage
	var contractErr error
	if json.Unmarshal(raw, &object) == nil && object != nil {
		var contract semreg.ContractVersion
		if json.Unmarshal(object["contract"], &contract) != nil || contract != ContractOperationV1 {
			contractErr = opError(semreg.InvalidContract, "operation document contract")
		}
	}
	if err := mostSpecific(decodeErr, contractErr, collectionErr); err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}

// The semantic root cannot import this operation package, so operation owns
// partial wire collection for its precondition tuple. Only fully valid key
// components participate; malformed non-key members do not hide a valid key.
func operationPreconditionWireError(raw []byte, nested bool) error {
	var document map[string]json.RawMessage
	if json.Unmarshal(raw, &document) != nil || document == nil {
		return nil
	}
	intentRaw := json.RawMessage(raw)
	if nested {
		var present bool
		intentRaw, present = document["intent"]
		if !present {
			return nil
		}
	}
	var intent map[string]json.RawMessage
	if json.Unmarshal(intentRaw, &intent) != nil || intent == nil {
		return nil
	}
	preconditionsRaw, present := intent["preconditions"]
	if !present {
		return nil
	}
	var preconditions []json.RawMessage
	if json.Unmarshal(preconditionsRaw, &preconditions) != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(preconditions))
	var prior *Precondition
	var errs []error
	for _, rawPrecondition := range preconditions {
		precondition, valid := wirePreconditionCollectionKey(rawPrecondition)
		if !valid {
			continue
		}
		key, _ := preconditionCollectionKey(precondition)
		if _, duplicate := seen[string(key)]; duplicate {
			errs = append(errs, opError(semreg.DuplicateKey, "intent preconditions"))
		}
		seen[string(key)] = struct{}{}
		if prior != nil && comparePrecondition(*prior, precondition) > 0 {
			errs = append(errs, opError(semreg.NoncanonicalOrder, "intent preconditions"))
		}
		copy := precondition
		prior = &copy
	}
	return mostSpecific(errs...)
}

func wirePreconditionCollectionKey(raw json.RawMessage) (Precondition, bool) {
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil || members == nil {
		return Precondition{}, false
	}
	factRaw, factPresent := members["fact"]
	candidateRaw, candidatePresent := members["candidate_id"]
	revisionRaw, revisionPresent := members["candidate_revision"]
	if !factPresent || !candidatePresent || !revisionPresent {
		return Precondition{}, false
	}
	fact, factErr := semreg.Decode[semreg.FactKey](factRaw)
	candidate, candidateErr := semreg.Decode[semreg.CandidateID](candidateRaw)
	revision, revisionErr := semreg.Decode[semreg.Uint64](revisionRaw)
	precondition := Precondition{Fact: fact, CandidateID: candidate, CandidateRevision: revision}
	if factErr != nil || candidateErr != nil || revisionErr != nil {
		return Precondition{}, false
	}
	_, valid := preconditionCollectionKey(precondition)
	return precondition, valid
}

func clone[T any](value T) T {
	raw, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(raw, &result)
	return result
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func factKeyEqual(left, right semreg.FactKey) bool {
	a, errA := semreg.CanonicalJSON(left)
	b, errB := semreg.CanonicalJSON(right)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}
