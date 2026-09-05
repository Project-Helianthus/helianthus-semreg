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

// EvaluationDigest returns the digest defined for an EvaluationView: canonical
// JSON with evaluation_digest omitted. It does not modify the receiver.
func (v EvaluationView) EvaluationDigestValue() (Digest, error) {
	if err := v.validateStructure(false); err != nil {
		return "", err
	}
	return v.computedDigestUnchecked()
}

func (v EvaluationView) validateStructure(requireDigest bool) error {
	var errs []error
	if v.Contract != ContractEvaluationV1 {
		errs = append(errs, errID(InvalidContract, "evaluation view"))
	}
	errs = append(errs, v.SnapshotID.Validate(), v.Revisions.Validate(), v.Context.Validate())
	if v.Facts == nil {
		errs = append(errs, errID(MissingMember, "evaluation facts"))
	}
	if len(v.Facts) > maxDerivationNodes {
		errs = append(errs, errID(BoundsExceeded, "evaluation facts"))
	}
	dup, ordered := false, true
	seen := make(map[CandidateID]struct{}, len(v.Facts))
	for index, fact := range v.Facts {
		if _, exists := seen[fact.CandidateID]; exists {
			dup = true
		}
		seen[fact.CandidateID] = struct{}{}
		if index > 0 && strings.Compare(string(v.Facts[index-1].CandidateID), string(fact.CandidateID)) > 0 {
			ordered = false
		}
	}
	for _, fact := range v.Facts {
		errs = append(errs, fact.Validate())
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "evaluated candidate"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "evaluated candidates"))
	}
	structure := bestError(errs...)
	if !requireDigest {
		return structure
	}
	digestErr := v.EvaluationDigest.Validate()
	if best := bestError(structure, digestErr); best != nil && errorRanks[ErrorIdentifier(best)] < errorRanks[DigestMismatch] {
		return best
	}
	digest, err := v.computedDigestUnchecked()
	if err != nil {
		return bestError(structure, digestErr, err)
	}
	if digest != v.EvaluationDigest {
		return bestError(structure, digestErr, errID(DigestMismatch, "evaluation view"))
	}
	return bestError(structure, digestErr)
}

func (v EvaluationView) computedDigestUnchecked() (Digest, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", errID(InvalidJSON, "evaluation view")
	}
	node, duplicate, err := parseJSON(raw)
	if err != nil {
		return "", err
	}
	if duplicate {
		return "", errID(DuplicateKey, "evaluation view")
	}
	filtered := node.object[:0]
	for _, member := range node.object {
		if member.key != "evaluation_digest" {
			filtered = append(filtered, member)
		}
	}
	node.object = filtered
	var out bytes.Buffer
	if err := writeCanonical(&out, node); err != nil {
		return "", errID(InvalidJSON, "evaluation view")
	}
	sum := sha256.Sum256(out.Bytes())
	return Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

// EvaluateSnapshot computes a complete immutable time view. It only reads its
// arguments and has no publication, registry, clock, or I/O dependency.
func EvaluateSnapshot(snapshot Snapshot, context EvaluationContext) (EvaluationView, error) {
	if err := bestError(snapshot.Validate(), context.Validate()); err != nil {
		return EvaluationView{}, err
	}
	if err := contextNotEarlierThanSnapshot(snapshot, context); err != nil {
		return EvaluationView{}, err
	}

	candidates := make(map[CandidateID]FactCandidate)
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			candidates[candidate.CandidateID] = candidate
		}
	}
	memo := make(map[CandidateID]EvaluatedFact, len(candidates))
	visiting := make(map[CandidateID]bool, len(candidates))
	var evaluate func(CandidateID) (EvaluatedFact, error)
	evaluate = func(id CandidateID) (EvaluatedFact, error) {
		if result, ok := memo[id]; ok {
			return result, nil
		}
		candidate, ok := candidates[id]
		if !ok {
			return EvaluatedFact{}, errID(DanglingReference, "evaluation derivation input")
		}
		if visiting[id] {
			return EvaluatedFact{}, errID(DerivationCycle, "evaluation derivation")
		}
		visiting[id] = true
		freshness, err := evaluateFreshness(candidate.Times, candidate.FreshnessPolicy, context)
		if err == nil && candidate.Quality.Assertion == AssertionInferred {
			for _, input := range candidate.Derivation.Inputs {
				inputResult, inputErr := evaluate(input.CandidateID)
				if inputErr != nil {
					err = inputErr
					break
				}
				freshness = combineFreshness(freshness, inputResult.Freshness)
			}
		}
		visiting[id] = false
		if err != nil {
			return EvaluatedFact{}, err
		}
		result := EvaluatedFact{
			CandidateID:           candidate.CandidateID,
			CandidateRevision:     candidate.Revision,
			Freshness:             freshness,
			EffectiveAvailability: effectiveAvailability(candidate.Quality.Availability, freshness),
		}
		memo[id] = result
		return result, nil
	}

	ids := make([]CandidateID, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	facts := make([]EvaluatedFact, 0, len(ids))
	for _, id := range ids {
		fact, err := evaluate(id)
		if err != nil {
			return EvaluationView{}, err
		}
		facts = append(facts, fact)
	}
	view := EvaluationView{Contract: ContractEvaluationV1, SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions, Context: context, Facts: facts}
	digest, err := view.EvaluationDigestValue()
	if err != nil {
		return EvaluationView{}, err
	}
	view.EvaluationDigest = digest
	return view, nil
}

func contextNotEarlierThanSnapshot(snapshot Snapshot, context EvaluationContext) error {
	if snapshot.EvaluateMonotonic.ClockEpochID == context.EvaluateMonotonic.ClockEpochID {
		snapshotTicks, sok := u64(snapshot.EvaluateMonotonic.Nanoseconds)
		contextTicks, cok := u64(context.EvaluateMonotonic.Nanoseconds)
		if !sok || !cok || contextTicks.Cmp(snapshotTicks) < 0 {
			return errID(InvalidTime, "evaluation before snapshot")
		}
		return nil
	}
	return earlierComparableWall(snapshot.EvaluatedAt, context.EvaluatedAt)
}

// earlierComparableWall rejects a demonstrably earlier point. Identically
// named wall-clock realizations are comparable even when they are not UTC;
// differently named clocks are intentionally not ordered here.
func earlierComparableWall(then, now TimePoint) error {
	if then.ClockID != now.ClockID {
		return nil
	}
	thenNS, tok := i64(then.UnixNanoseconds)
	nowNS, nok := i64(now.UnixNanoseconds)
	thenU, tuok := u64(then.UncertaintyNS)
	nowU, nuok := u64(now.UncertaintyNS)
	if !tok || !nok || !tuok || !nuok {
		return errID(InvalidTime, "wall point")
	}
	u := new(big.Int).Add(thenU, nowU)
	if u.BitLen() > 64 {
		return errID(InvalidTime, "wall uncertainty overflow")
	}
	delta := new(big.Int).Sub(nowNS, thenNS)
	if new(big.Int).Add(delta, u).Sign() < 0 {
		return errID(InvalidTime, "evaluation before snapshot")
	}
	return nil
}

func evaluateFreshness(times Times, policy FreshnessPolicy, context EvaluationContext) (Freshness, error) {
	if err := bestError(times.Validate(), policy.Validate(), context.Validate()); err != nil {
		return "", err
	}
	if times.ReceiptMonotonic.ClockEpochID == context.EvaluateMonotonic.ClockEpochID {
		receipt, rok := u64(times.ReceiptMonotonic.Nanoseconds)
		evaluation, eok := u64(context.EvaluateMonotonic.Nanoseconds)
		if !rok || !eok || evaluation.Cmp(receipt) < 0 {
			return "", errID(InvalidTime, "evaluation monotonic order")
		}
		return classifyElapsed(new(big.Int).Sub(evaluation, receipt), policy)
	}
	return evaluateCrossEpoch(times.ReceivedAt, policy, context.EvaluatedAt)
}

func evaluateCrossEpoch(received TimePoint, policy FreshnessPolicy, evaluated TimePoint) (Freshness, error) {
	if received.ClockID != "clock.utc" || evaluated.ClockID != "clock.utc" {
		return FreshnessUnknown, nil
	}
	receivedNS, rok := i64(received.UnixNanoseconds)
	evaluatedNS, eok := i64(evaluated.UnixNanoseconds)
	receivedU, ruok := u64(received.UncertaintyNS)
	evaluatedU, euok := u64(evaluated.UncertaintyNS)
	maxU, mok := u64(policy.MaxWallUncertaintyNS)
	if !rok || !eok || !ruok || !euok || !mok {
		return "", errID(InvalidTime, "cross epoch wall point")
	}
	u := new(big.Int).Add(receivedU, evaluatedU)
	if u.BitLen() > 64 {
		return "", errID(InvalidTime, "wall uncertainty overflow")
	}
	delta := new(big.Int).Sub(evaluatedNS, receivedNS)
	upper := new(big.Int).Add(delta, u)
	if upper.Sign() < 0 {
		return "", errID(InvalidTime, "negative wall delta")
	}
	if upper.BitLen() > 64 {
		return "", errID(InvalidTime, "elapsed interval overflow")
	}
	lower := new(big.Int).Sub(delta, u)
	if lower.Sign() < 0 {
		lower.SetInt64(0)
	}
	if lower.BitLen() > 64 {
		return "", errID(InvalidTime, "elapsed interval overflow")
	}
	if u.Cmp(maxU) > 0 {
		return FreshnessUnknown, nil
	}
	fresh, _ := u64(policy.FreshForNS)
	retain, _ := u64(policy.RetainForNS)
	if upper.Cmp(fresh) < 0 {
		return FreshnessFresh, nil
	}
	if lower.Cmp(fresh) >= 0 && upper.Cmp(retain) < 0 {
		return FreshnessStale, nil
	}
	if lower.Cmp(retain) >= 0 {
		return FreshnessExpired, nil
	}
	return FreshnessUnknown, nil
}

func classifyElapsed(elapsed *big.Int, policy FreshnessPolicy) (Freshness, error) {
	if elapsed.Sign() < 0 || elapsed.BitLen() > 64 {
		return "", errID(InvalidTime, "elapsed interval")
	}
	fresh, fok := u64(policy.FreshForNS)
	retain, rok := u64(policy.RetainForNS)
	if !fok || !rok {
		return "", errID(InvalidTime, "freshness policy")
	}
	if elapsed.Cmp(fresh) < 0 {
		return FreshnessFresh, nil
	}
	if elapsed.Cmp(retain) < 0 {
		return FreshnessStale, nil
	}
	return FreshnessExpired, nil
}

func combineFreshness(left, right Freshness) Freshness {
	order := map[Freshness]int{FreshnessFresh: 0, FreshnessStale: 1, FreshnessUnknown: 2, FreshnessExpired: 3}
	if order[right] > order[left] {
		return right
	}
	return left
}

func effectiveAvailability(stored Availability, freshness Freshness) Availability {
	if stored == AvailabilityWithdrawn || freshness == FreshnessFresh {
		return stored
	}
	if freshness == FreshnessExpired {
		return AvailabilityUnavailable
	}
	if stored == AvailabilityAvailable && (freshness == FreshnessStale || freshness == FreshnessUnknown) {
		return AvailabilityDegraded
	}
	return stored
}

type selectionPolicyKey struct {
	id      PolicyID
	version SemanticVersion
}

// SelectionKernel owns exact policy registrations. It stores no snapshots or
// evaluations, so dispatch remains pure with respect to its explicit inputs.
type SelectionKernel struct {
	mu       sync.RWMutex
	policies map[selectionPolicyKey]SelectionPolicy
}

type snapshotViewCorrespondence struct {
	candidates          map[CandidateID]FactCandidate
	envelopeByCandidate map[CandidateID]FactEnvelope
	evaluated           map[CandidateID]EvaluatedFact
}

// snapshotViewBinding builds the one shared complete correspondence used by
// policy dispatch and public result validation. It collects missing/extra and
// revision failures independently so global error ranking is traversal-free.
func snapshotViewBinding(snapshot Snapshot, view EvaluationView) (snapshotViewCorrespondence, error) {
	binding := snapshotViewCorrespondence{
		candidates:          make(map[CandidateID]FactCandidate),
		envelopeByCandidate: make(map[CandidateID]FactEnvelope),
		evaluated:           make(map[CandidateID]EvaluatedFact, len(view.Facts)),
	}
	var errs []error
	if view.SnapshotID != snapshot.SnapshotID || view.Revisions != snapshot.Revisions {
		errs = append(errs, errID(RevisionConflict, "snapshot evaluation binding"))
	}
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			binding.candidates[candidate.CandidateID] = candidate
			binding.envelopeByCandidate[candidate.CandidateID] = envelope
		}
	}
	for _, fact := range view.Facts {
		binding.evaluated[fact.CandidateID] = fact
		candidate, ok := binding.candidates[fact.CandidateID]
		if !ok {
			errs = append(errs, errID(DanglingReference, "evaluated candidate"))
			continue
		}
		if candidate.Revision != fact.CandidateRevision {
			errs = append(errs, errID(RevisionConflict, "evaluated candidate revision"))
		}
	}
	for id := range binding.candidates {
		if _, ok := binding.evaluated[id]; !ok {
			errs = append(errs, errID(DanglingReference, "missing evaluated candidate"))
		}
	}
	return binding, bestError(errs...)
}

func NewSelectionKernel(policies ...SelectionPolicy) (*SelectionKernel, error) {
	kernel := &SelectionKernel{policies: make(map[selectionPolicyKey]SelectionPolicy)}
	for _, policy := range policies {
		if err := kernel.RegisterSelectionPolicy(policy); err != nil {
			return nil, err
		}
	}
	return kernel, nil
}

func (k *SelectionKernel) RegisterSelectionPolicy(policy SelectionPolicy) error {
	if k == nil || policy == nil || (reflect.ValueOf(policy).Kind() == reflect.Ptr && reflect.ValueOf(policy).IsNil()) {
		return errID(DefinitionOwnerConflict, "nil selection policy")
	}
	key := selectionPolicyKey{id: policy.PolicyID(), version: policy.Version()}
	if err := bestError(key.id.Validate(), key.version.Validate()); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, exists := k.policies[key]; exists {
		return errID(DefinitionOwnerConflict, "selection policy")
	}
	k.policies[key] = policy
	return nil
}

// SelectPresentation validates the full snapshot/view binding before looking
// up and invoking exactly one registered presentation policy.
func (k *SelectionKernel) SelectPresentation(snapshot Snapshot, view EvaluationView, key FactKey, policyID PolicyID, policyVersion SemanticVersion) (Selection, error) {
	if k == nil {
		return Selection{}, errID(DefinitionOwnerMissing, "selection kernel")
	}
	binding, correspondenceErr := snapshotViewBinding(snapshot, view)
	requested, found := findEnvelope(snapshot.Facts, key)
	var requestedErr error
	if !found {
		requestedErr = errID(DanglingReference, "requested fact key")
	}
	policyKey := selectionPolicyKey{id: policyID, version: policyVersion}
	k.mu.RLock()
	policy, registered := k.policies[policyKey]
	k.mu.RUnlock()
	var policyErr error
	if !registered {
		policyErr = errID(DefinitionOwnerMissing, "selection policy")
	}
	if err := bestError(
		view.Validate(), snapshot.Validate(), key.Validate(), policyID.Validate(), policyVersion.Validate(),
		correspondenceErr, requestedErr, policyErr,
	); err != nil {
		return Selection{}, err
	}

	facts := make([]EvaluatedFact, 0, len(requested.Candidates))
	for _, candidate := range requested.Candidates {
		fact, ok := binding.evaluated[candidate.CandidateID]
		if !ok {
			return Selection{}, errID(DanglingReference, "requested evaluated candidate")
		}
		facts = append(facts, fact)
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].CandidateID < facts[j].CandidateID })
	selected, err := policy.Select(cloneEnvelope(requested), append([]EvaluatedFact(nil), facts...))
	if err != nil {
		return Selection{}, errID(InvalidValue, "selection policy")
	}
	candidate, ok := binding.candidates[selected]
	if !ok || !selectionFactKeysEqual(binding.envelopeByCandidate[selected].Key, key) {
		return Selection{}, errID(InvalidValue, "selected candidate")
	}
	if _, ok := binding.evaluated[selected]; !ok {
		return Selection{}, errID(InvalidValue, "selected candidate evaluation")
	}
	return Selection{Contract: ContractSelectionV1, SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions, EvaluationDigest: view.EvaluationDigest, Context: view.Context, Key: cloneFactKey(key), PolicyID: policyID, PolicyVersion: policyVersion, SelectedCandidate: selected, CandidateRevision: candidate.Revision, PresentationOnly: true}, nil
}

// ValidateSelection verifies a selection result against the exact immutable
// snapshot and evaluation that bound it. It does not dispatch a policy.
func ValidateSelection(snapshot Snapshot, view EvaluationView, selection Selection) error {
	binding, correspondenceErr := snapshotViewBinding(snapshot, view)
	var bindingErr error
	if selection.SnapshotID != snapshot.SnapshotID || selection.Revisions != snapshot.Revisions || selection.EvaluationDigest != view.EvaluationDigest || !reflect.DeepEqual(selection.Context, view.Context) {
		bindingErr = errID(RevisionConflict, "selection evaluation binding")
	}
	requested, found := findEnvelope(snapshot.Facts, selection.Key)
	var requestedErr error
	if !found {
		requestedErr = errID(DanglingReference, "selection requested fact key")
	}
	var candidate *FactCandidate
	for index := range requested.Candidates {
		if requested.Candidates[index].CandidateID == selection.SelectedCandidate {
			candidate = &requested.Candidates[index]
			break
		}
	}
	var candidateErr error
	if candidate == nil {
		candidateErr = errID(DanglingReference, "selected candidate")
	} else if candidate.Revision != selection.CandidateRevision {
		candidateErr = errID(RevisionConflict, "selected candidate revision")
	}
	evaluated, evaluatedFound := binding.evaluated[selection.SelectedCandidate]
	var evaluatedErr error
	if !evaluatedFound {
		evaluatedErr = errID(DanglingReference, "selected evaluated candidate")
	} else if evaluated.CandidateRevision != selection.CandidateRevision {
		evaluatedErr = errID(RevisionConflict, "selected evaluated candidate revision")
	}
	return bestError(
		view.Validate(), snapshot.Validate(), selection.Validate(), correspondenceErr,
		bindingErr, requestedErr, candidateErr, evaluatedErr,
	)
}

func findEnvelope(envelopes []FactEnvelope, key FactKey) (FactEnvelope, bool) {
	want, err := factKeyIdentity(key)
	if err != nil {
		return FactEnvelope{}, false
	}
	for _, envelope := range envelopes {
		got, err := factKeyIdentity(envelope.Key)
		if err == nil && got == want {
			return envelope, true
		}
	}
	return FactEnvelope{}, false
}

func selectionFactKeysEqual(left, right FactKey) bool {
	a, errA := factKeyIdentity(left)
	b, errB := factKeyIdentity(right)
	return errA == nil && errB == nil && a == b
}

func cloneEnvelope(envelope FactEnvelope) FactEnvelope {
	raw, _ := json.Marshal(envelope)
	var clone FactEnvelope
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func cloneFactKey(key FactKey) FactKey {
	raw, _ := json.Marshal(key)
	var clone FactKey
	_ = json.Unmarshal(raw, &clone)
	return clone
}
