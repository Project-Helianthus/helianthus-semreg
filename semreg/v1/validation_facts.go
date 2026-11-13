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
)

func (d Decimal) Validate() error {
	if !coefficientRE.MatchString(d.Coefficient) || d.Exponent10 < -18 || d.Exponent10 > 18 || (d.Coefficient == "0" && d.Exponent10 != 0) || (d.Coefficient != "0" && strings.HasSuffix(d.Coefficient, "0")) {
		return errID(InvalidDecimal, "decimal")
	}
	return nil
}
func (s Symbol) Validate() error {
	var value error
	if s.Token == "" {
		value = errID(InvalidValue, "symbol token")
	} else if err := validateText(s.Token, 1, 256, false); err != nil {
		value = err
	}
	if strings.HasPrefix(string(s.Namespace), "native.") {
		if s.Known {
			value = bestError(value, errID(InvalidValue, "known native symbol"))
		}
	} else if !s.Known {
		value = bestError(value, errID(InvalidValue, "unknown symbol namespace"))
	}
	return bestError(s.Namespace.Validate(), value)
}
func (q Quantity) Validate() error { return bestError(q.Number.Validate(), q.Unit.Validate()) }
func (v Value) Validate() error {
	var errs []error
	count := 0
	if v.Quantity != nil {
		count++
		errs = append(errs, v.Quantity.Validate())
	}
	if v.Boolean != nil {
		count++
	}
	if v.Text != nil {
		count++
		errs = append(errs, validateText(*v.Text, 0, 4096, true))
	}
	if v.Symbol != nil {
		count++
		errs = append(errs, v.Symbol.Validate())
	}
	if v.Symbols != nil {
		count++
		errs = append(errs, validateSymbols(v.Symbols))
	}
	if v.Time != nil {
		count++
		errs = append(errs, v.Time.Validate())
	}
	if count != 1 {
		errs = append(errs, errID(InvalidValue, "value payload"))
	}
	switch v.Kind {
	case ValueQuantity:
		if v.Quantity == nil {
			errs = append(errs, errID(InvalidValue, "quantity payload"))
		}
	case ValueBoolean:
		if v.Boolean == nil {
			errs = append(errs, errID(InvalidValue, "boolean payload"))
		}
	case ValueText:
		if v.Text == nil {
			errs = append(errs, errID(InvalidValue, "text payload"))
		}
	case ValueSymbol:
		if v.Symbol == nil {
			errs = append(errs, errID(InvalidValue, "symbol payload"))
		}
	case ValueSymbols:
		if v.Symbols == nil {
			errs = append(errs, errID(InvalidValue, "symbols payload"))
		}
	case ValueTime:
		if v.Time == nil {
			errs = append(errs, errID(InvalidValue, "time payload"))
		}
	default:
		errs = append(errs, errID(InvalidEnum, "value kind"))
	}
	return bestError(errs...)
}

func validateSymbols(symbols []Symbol) error {
	var errs []error
	dup, ordered := duplicateAndOrder(symbols, func(a, b Symbol) int {
		if c := strings.Compare(string(a.Namespace), string(b.Namespace)); c != 0 {
			return c
		}
		return strings.Compare(a.Token, b.Token)
	})
	for _, s := range symbols {
		errs = append(errs, s.Validate())
	}
	if len(symbols) < 1 || len(symbols) > 64 {
		errs = append(errs, errID(BoundsExceeded, "symbols"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "symbols"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "symbols"))
	}
	return bestError(errs...)
}
func (d Dimension) Validate() error {
	var kind error
	if d.Value.Kind != ValueBoolean && d.Value.Kind != ValueText && d.Value.Kind != ValueSymbol && d.Value.Kind != ValueQuantity {
		kind = errID(InvalidValue, "dimension kind")
	}
	return bestError(d.ID.Validate(), d.Value.Validate(), kind)
}
func (k FactKey) Validate() error {
	dup, ordered := duplicateAndOrder(k.Dimensions, func(a, b Dimension) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	var errs []error
	if k.Dimensions == nil {
		errs = append(errs, errID(MissingMember, "dimensions"))
	}
	errs = append(errs, k.PackID.Validate(), k.PackVersion.Validate(), k.FactID.Validate())
	for _, d := range k.Dimensions {
		errs = append(errs, d.Validate())
	}
	if len(k.Dimensions) > 16 {
		errs = append(errs, errID(BoundsExceeded, "dimensions"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "dimensions"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "dimensions"))
	}
	return bestError(errs...)
}

func (t TimePoint) Validate() error {
	var timeErr error
	if _, ok := i64(t.UnixNanoseconds); !ok {
		timeErr = errID(InvalidTime, "unix nanoseconds")
	}
	if _, ok := u64(t.UncertaintyNS); !ok {
		timeErr = bestError(timeErr, errID(InvalidTime, "uncertainty"))
	}
	return bestError(t.ClockID.Validate(), timeErr)
}
func (m MonotonicPoint) Validate() error {
	var timeErr error
	if _, ok := u64(m.Nanoseconds); !ok {
		timeErr = errID(InvalidTime, "monotonic")
	}
	return bestError(m.ClockEpochID.Validate(), timeErr)
}
func (t Times) Validate() error {
	var errs []error
	errs = append(errs, t.ReceivedAt.Validate(), t.ReceiptMonotonic.Validate(), t.EvaluatedAt.Validate(), t.EvaluateMonotonic.Validate())
	if t.PhenomenonAt != nil {
		errs = append(errs, t.PhenomenonAt.Validate())
	}
	if t.SourceAt != nil {
		errs = append(errs, t.SourceAt.Validate())
	}
	if t.ReceiptMonotonic.ClockEpochID == t.EvaluateMonotonic.ClockEpochID {
		r, rok := u64(t.ReceiptMonotonic.Nanoseconds)
		e, eok := u64(t.EvaluateMonotonic.Nanoseconds)
		if rok && eok && e.Cmp(r) < 0 {
			errs = append(errs, errID(InvalidTime, "monotonic order"))
		}
	}
	return bestError(errs...)
}

// ElapsedMonotonicNS returns a same-epoch elapsed interval. It never compares
// numeric ticks from different clock epochs.
func (t Times) ElapsedMonotonicNS() (Uint64, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	if t.ReceiptMonotonic.ClockEpochID != t.EvaluateMonotonic.ClockEpochID {
		return "", errID(IncomparableClockEpoch, "monotonic epoch")
	}
	received, _ := u64(t.ReceiptMonotonic.Nanoseconds)
	evaluated, _ := u64(t.EvaluateMonotonic.Nanoseconds)
	return Uint64(new(big.Int).Sub(evaluated, received).String()), nil
}
func (p FreshnessPolicy) Validate() error {
	var errs []error
	errs = append(errs, p.PolicyID.Validate(), p.Version.Validate())
	f, fok := u64(p.FreshForNS)
	r, rok := u64(p.RetainForNS)
	_, uok := u64(p.MaxWallUncertaintyNS)
	if !fok || !rok || !uok || f.Sign() <= 0 || (fok && rok && r.Cmp(f) <= 0) {
		errs = append(errs, errID(InvalidTime, "freshness policy"))
	}
	return bestError(errs...)
}

func (c EvaluationContext) Validate() error {
	return bestError(c.EvaluatedAt.Validate(), c.EvaluateMonotonic.Validate())
}

func (f EvaluatedFact) Validate() error {
	var errs []error
	errs = append(errs, f.CandidateID.Validate())
	if !positive(f.CandidateRevision) {
		errs = append(errs, errID(InvalidIdentifier, "evaluated candidate revision"))
	}
	if f.Freshness != FreshnessFresh && f.Freshness != FreshnessStale && f.Freshness != FreshnessExpired && f.Freshness != FreshnessUnknown {
		errs = append(errs, errID(InvalidEnum, "evaluated freshness"))
	}
	if f.EffectiveAvailability != AvailabilityAvailable && f.EffectiveAvailability != AvailabilityDegraded && f.EffectiveAvailability != AvailabilityUnavailable && f.EffectiveAvailability != AvailabilityWithdrawn {
		errs = append(errs, errID(InvalidEnum, "effective availability"))
	}
	return bestError(errs...)
}

func (f EvaluatedRetainedObservation) Validate() error {
	if f.Freshness != FreshnessFresh && f.Freshness != FreshnessStale && f.Freshness != FreshnessExpired && f.Freshness != FreshnessUnknown {
		return errID(InvalidEnum, "evaluated retained freshness")
	}
	return f.Observation.Validate()
}

func retainedRemovalError(removal RetainedRemoval) error {
	if removal != RetainedRemovalGenerationFence && removal != RetainedRemovalSourceRetirement {
		return errID(InvalidEnum, "retained removal")
	}
	return nil
}

func (v EvaluationView) Validate() error {
	return v.validateStructure(true)
}

func (s Selection) Validate() error {
	var errs []error
	if s.Contract != ContractSelectionV1 {
		errs = append(errs, errID(InvalidContract, "selection"))
	}
	errs = append(errs, s.SnapshotID.Validate(), s.Revisions.Validate(), s.EvaluationDigest.Validate(), s.Context.Validate(), s.Key.Validate(), s.PolicyID.Validate(), s.PolicyVersion.Validate(), s.SelectedCandidate.Validate())
	if !positive(s.CandidateRevision) {
		errs = append(errs, errID(InvalidIdentifier, "selection candidate revision"))
	}
	if !s.PresentationOnly {
		errs = append(errs, errID(InvalidValue, "presentation only"))
	}
	return bestError(errs...)
}
func (q Quality) Validate() error {
	dup, ordered := duplicateAndOrder(q.Reasons, func(a, b DefinitionID) int { return strings.Compare(string(a), string(b)) })
	var errs []error
	if q.Reasons == nil {
		errs = append(errs, errID(MissingMember, "reasons"))
	}
	for _, r := range q.Reasons {
		errs = append(errs, r.Validate())
	}
	if q.Assertion != AssertionObserved && q.Assertion != AssertionInferred {
		errs = append(errs, errID(InvalidEnum, "assertion"))
	}
	if q.Qualification != QualificationCandidate && q.Qualification != QualificationQualified && q.Qualification != QualificationUnsupported && q.Qualification != QualificationUnknown && q.Qualification != QualificationRejected {
		errs = append(errs, errID(InvalidEnum, "qualification"))
	}
	if q.Promotion != PromotionUnpromoted && q.Promotion != PromotionPromoted {
		errs = append(errs, errID(InvalidEnum, "promotion"))
	}
	if q.Validity != ValidityGood && q.Validity != ValiditySuspect && q.Validity != ValidityBad && q.Validity != ValidityUnknown {
		errs = append(errs, errID(InvalidEnum, "validity"))
	}
	if q.Availability != AvailabilityAvailable && q.Availability != AvailabilityDegraded && q.Availability != AvailabilityUnavailable && q.Availability != AvailabilityWithdrawn {
		errs = append(errs, errID(InvalidEnum, "availability"))
	}
	if q.Freshness != FreshnessFresh && q.Freshness != FreshnessStale && q.Freshness != FreshnessExpired && q.Freshness != FreshnessUnknown {
		errs = append(errs, errID(InvalidEnum, "freshness"))
	}
	if q.Promotion == PromotionPromoted && (q.Qualification != QualificationQualified || (q.Validity != ValidityGood && q.Validity != ValiditySuspect) || (q.Availability != AvailabilityAvailable && q.Availability != AvailabilityDegraded)) {
		errs = append(errs, errID(InvalidValue, "promotion"))
	}
	if len(q.Reasons) > 16 {
		errs = append(errs, errID(BoundsExceeded, "reasons"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "reasons"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "reasons"))
	}
	return bestError(errs...)
}
func (c CausalContext) Validate() error {
	var errs []error
	if c.Path == nil {
		errs = append(errs, errID(MissingMember, "causal path"))
	}
	errs = append(errs, c.Origin.Validate(), c.CorrelationID.Validate(), c.FirstSeenAt.Validate(), c.ExpiresAt.Validate())
	if c.ParentCorrelationID != nil {
		errs = append(errs, c.ParentCorrelationID.Validate())
	}
	for _, target := range c.Path {
		errs = append(errs, target.Validate())
	}
	dup, _ := duplicateAndOrder(c.Path, func(a, b TargetID) int { return strings.Compare(string(a), string(b)) })
	errs = append(errs, causalTimeErrors(&c.FirstSeenAt, &c.ExpiresAt)...)
	if c.MaxHops < 1 || c.MaxHops > 16 || c.HopCount > c.MaxHops || int(c.HopCount) != len(c.Path) || dup {
		errs = append(errs, errID(CausalBudgetExceeded, "causal path"))
	}
	return bestError(errs...)
}

// Shared by typed validation and partial wire validation: time rules depend
// only on the supplied time points, never on whether hop counters can bind.
func causalTimeErrors(firstPoint, expiresPoint *TimePoint) []error {
	var errs []error
	if firstPoint != nil && firstPoint.ClockID != "clock.utc" || expiresPoint != nil && expiresPoint.ClockID != "clock.utc" {
		errs = append(errs, errID(InvalidTime, "causal clock"))
	}
	if firstPoint == nil || expiresPoint == nil {
		return errs
	}
	if first, ok1 := i64(firstPoint.UnixNanoseconds); ok1 {
		if expires, ok2 := i64(expiresPoint.UnixNanoseconds); ok2 {
			delta := new(big.Int).Sub(expires, first)
			limit := big.NewInt(300_000_000_000)
			if delta.Sign() < 0 {
				errs = append(errs, errID(InvalidTime, "causal order"))
			} else if delta.Cmp(limit) > 0 {
				errs = append(errs, errID(CausalBudgetExceeded, "causal lifetime"))
			}
		}
	}
	return errs
}
func (c FactCandidate) Validate() error {
	// Presence determines member validation; assertion and origin determine only
	// relationships. Forbidden data still contributes its own stable errors.
	errs := c.presentMemberErrors()
	errs = append(errs, c.CandidateID.Validate(), c.Key.Validate(), c.Quality.Validate(), c.Times.Validate(), c.FreshnessPolicy.Validate(), c.Origin.Validate(), validateEvidenceSet(c.Evidence, 1, 32))
	if !positive(c.Revision) {
		errs = append(errs, errID(InvalidIdentifier, "candidate revision"))
	}
	valueForbidden := c.Quality.Qualification == QualificationUnsupported || c.Quality.Qualification == QualificationRejected || c.Quality.Availability == AvailabilityWithdrawn
	if valueForbidden && c.Value != nil {
		errs = append(errs, errID(InvalidValue, "forbidden candidate value"))
	}
	if !valueForbidden && c.Value == nil {
		errs = append(errs, errID(MissingMember, "candidate value"))
	}
	if c.Quality.Assertion == AssertionInferred {
		if c.BindingID != nil || c.SourceEpochID != nil || c.DriverGeneration != nil {
			errs = append(errs, errID(InvalidValue, "inferred source path"))
		}
		if c.Derivation == nil {
			errs = append(errs, errID(MissingMember, "derivation"))
		}
	}
	if c.Quality.Assertion == AssertionObserved {
		if c.Derivation != nil {
			errs = append(errs, errID(InvalidValue, "observed derivation"))
		}
		if c.BindingID == nil || c.SourceEpochID == nil || c.DriverGeneration == nil {
			errs = append(errs, errID(MissingMember, "observed source path"))
		}
	}
	if c.Origin.Kind == OriginDerived && c.Derivation == nil {
		errs = append(errs, errID(MissingMember, "derived origin derivation"))
	}
	if c.Origin.Kind == OriginNativeObservation {
		if c.BindingID == nil || c.SourceEpochID == nil || c.DriverGeneration == nil {
			errs = append(errs, errID(MissingMember, "native observation path"))
		} else {
			if c.BindingID != nil && c.Origin.BindingID != nil && *c.BindingID != *c.Origin.BindingID {
				errs = append(errs, errID(InvalidValue, "origin binding"))
			}
			if c.SourceEpochID != nil && c.Origin.SourceEpochID != nil && *c.SourceEpochID != *c.Origin.SourceEpochID {
				errs = append(errs, errID(InvalidValue, "origin epoch"))
			}
		}
	}
	if c.Origin.Kind == OriginProjection && c.Causal == nil {
		errs = append(errs, errID(MissingMember, "projection causal context"))
	}
	return bestError(errs...)
}

func (r RetainedObservationRecord) Validate() error {
	return bestError(func() error {
		if r.Contract != ContractRetainedObservationV1 {
			return errID(InvalidContract, "retained observation")
		}
		return nil
	}(), retainedRemovalError(r.Removal), r.Candidate.Validate())
}

func (c FactCandidate) presentMemberErrors() []error {
	var errs []error
	if c.Value != nil {
		errs = append(errs, c.Value.Validate())
	}
	if c.Causal != nil {
		errs = append(errs, c.Causal.Validate())
	}
	if c.BindingID != nil {
		errs = append(errs, c.BindingID.Validate())
	}
	if c.SourceEpochID != nil {
		errs = append(errs, c.SourceEpochID.Validate())
	}
	if c.DriverGeneration != nil && !positive(*c.DriverGeneration) {
		errs = append(errs, errID(InvalidIdentifier, "driver generation"))
	}
	if c.Derivation != nil {
		errs = append(errs, c.Derivation.Validate())
		for _, in := range c.Derivation.Inputs {
			if in.CandidateID == c.CandidateID {
				errs = append(errs, errID(DerivationCycle, "self reference"))
			}
		}
	}
	return errs
}

func (c Conflict) Validate() error {
	dup, ordered := duplicateAndOrder(c.Candidates, func(a, b CandidateID) int { return strings.Compare(string(a), string(b)) })
	var errs []error
	errs = append(errs, c.ConflictID.Validate())
	for _, id := range c.Candidates {
		errs = append(errs, id.Validate())
	}
	if c.Kind != ConflictValue {
		errs = append(errs, errID(InvalidEnum, "conflict kind"))
	}
	if c.State != ConflictOpen {
		errs = append(errs, errID(InvalidEnum, "conflict state"))
	}
	if len(c.Candidates) < 2 || len(c.Candidates) > 32 {
		errs = append(errs, errID(BoundsExceeded, "conflict candidates"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "conflict candidates"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "conflict candidates"))
	}
	errs = append(errs, validateEvidenceSet(c.Evidence, 1, 1024))
	return bestError(errs...)
}
func sameFactKey(a, b FactKey) bool {
	ab, ea := CanonicalJSON(a)
	bb, eb := CanonicalJSON(b)
	return ea == nil && eb == nil && bytes.Equal(ab, bb)
}
func deriveConflicts(asset AssetID, key FactKey, candidates []FactCandidate) ([]Conflict, error) {
	eligible := make([]FactCandidate, 0, len(candidates))
	values := map[string]struct{}{}
	evidence := []EvidenceRef{}
	for _, candidate := range candidates {
		if candidate.Value == nil || candidate.Quality.Qualification != QualificationQualified || candidate.Quality.Promotion != PromotionPromoted {
			continue
		}
		canonical, err := CanonicalJSON(*candidate.Value)
		if err != nil {
			return nil, err
		}
		values[string(canonical)] = struct{}{}
		eligible = append(eligible, candidate)
		evidence = append(evidence, candidate.Evidence...)
	}
	if len(values) < 2 {
		return []Conflict{}, nil
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].CandidateID < eligible[j].CandidateID })
	ids := make([]CandidateID, len(eligible))
	for i, candidate := range eligible {
		ids[i] = candidate.CandidateID
	}
	sort.Slice(evidence, func(i, j int) bool { return compareEvidence(evidence[i], evidence[j]) < 0 })
	dedup := evidence[:0]
	for _, item := range evidence {
		if len(dedup) == 0 || compareEvidence(dedup[len(dedup)-1], item) != 0 {
			dedup = append(dedup, item)
		}
	}
	identity := struct {
		Contract   ContractVersion `json:"contract"`
		AssetID    AssetID         `json:"asset_id"`
		Key        FactKey         `json:"key"`
		Kind       ConflictKind    `json:"kind"`
		Candidates []CandidateID   `json:"candidates"`
	}{"helianthus.semantic.conflict-id/v1", asset, key, ConflictValue, ids}
	raw, err := json.Marshal(identity)
	if err != nil {
		return nil, errID(InvalidJSON, "conflict id")
	}
	canonical, err := canonicalize(raw)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonical)
	return []Conflict{{ConflictID: ConflictID("sha256:" + hex.EncodeToString(sum[:])), Kind: ConflictValue, Candidates: ids, Evidence: append([]EvidenceRef(nil), dedup...), State: ConflictOpen}}, nil
}
func (f FactEnvelope) Validate() error {
	dup, ordered := duplicateAndOrder(f.Candidates, func(a, b FactCandidate) int { return strings.Compare(string(a.CandidateID), string(b.CandidateID)) })
	var errs []error
	if f.Candidates == nil {
		errs = append(errs, errID(MissingMember, "candidates"))
	}
	if f.Conflicts == nil {
		errs = append(errs, errID(MissingMember, "conflicts"))
	}
	errs = append(errs, f.AssetID.Validate(), f.Key.Validate())
	if !positive(f.Revision) {
		errs = append(errs, errID(InvalidIdentifier, "fact revision"))
	}
	for _, candidate := range f.Candidates {
		errs = append(errs, candidate.Validate())
		if !sameFactKey(candidate.Key, f.Key) {
			errs = append(errs, errID(InvalidValue, "candidate key"))
		}
	}
	if len(f.Candidates) < 1 || len(f.Candidates) > 32 {
		errs = append(errs, errID(BoundsExceeded, "candidates"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "candidates"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "candidates"))
	}
	cdup, cordered := duplicateAndOrder(f.Conflicts, func(a, b Conflict) int { return strings.Compare(string(a.ConflictID), string(b.ConflictID)) })
	for _, conflict := range f.Conflicts {
		errs = append(errs, conflict.Validate())
	}
	if cdup {
		errs = append(errs, errID(DuplicateKey, "conflicts"))
	}
	if !cordered {
		errs = append(errs, errID(NoncanonicalOrder, "conflicts"))
	}
	expected, deriveErr := deriveConflicts(f.AssetID, f.Key, f.Candidates)
	errs = append(errs, deriveErr)
	if deriveErr == nil && !reflect.DeepEqual(expected, f.Conflicts) {
		errs = append(errs, errID(InvalidValue, "derived conflicts"))
	}
	return bestError(errs...)
}
