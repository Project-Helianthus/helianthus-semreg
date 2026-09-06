package operation

import (
	"bytes"
	"math/big"
	"reflect"
	"sort"
	"sync"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// AuthorityResolver is the runtime-owned authorization boundary. The operation
// kernel supplies immutable copies of the exact intent, route, and trusted
// current context. Any failure is classified as authority_missing.
type AuthorityResolver interface {
	Authorize(Intent, Route, semreg.EvaluationContext) error
}

type AuthorityResolverFunc func(Intent, Route, semreg.EvaluationContext) error

func (f AuthorityResolverFunc) Authorize(intent Intent, route Route, context semreg.EvaluationContext) error {
	return f(intent, route, context)
}

type lockedOperationValidator struct {
	mu   sync.Mutex
	hook OperationPackValidator
}

// Kernel owns immutable operation-pack registration and bounded idempotency
// records. It owns no snapshot lifecycle or native handle.
type Kernel struct {
	registry              *semreg.Registry
	validators            map[semreg.PackRef]*lockedOperationValidator
	maxRetainedOperations int

	mu         sync.Mutex
	admissions map[semreg.IdempotencyKey]*Admission
}

const (
	// DefaultMaxRetainedOperations bounds replay-safe in-memory history for the
	// convenience constructor. Callers with a measured workload can select a
	// different fail-closed bound through NewKernelWithOptions.
	DefaultMaxRetainedOperations = 1024
	MaxRetainedOperations        = 65536
)

// KernelOptions controls the bounded semantic idempotency history. The kernel
// never evicts retained keys: reaching the configured limit fails closed with
// bounds_exceeded, while retained identical replays and changed-byte conflicts
// remain available. Durable native replay ownership remains external.
type KernelOptions struct {
	MaxRetainedOperations int
}

// GuardClaims is immutable semantic data for the external native owner to
// recheck under its INT-06 lifecycle lock immediately before native dispatch.
// It carries no callback, handle, lock, release function, or lifecycle state.
type GuardClaims struct {
	AssetID                            semreg.AssetID
	ExpectedCapabilityRevision         semreg.Uint64
	CapabilityInstance                 semreg.CapabilityInstanceID
	ExpectedCapabilityInstanceRevision semreg.Uint64
	ServiceInstance                    semreg.ServiceInstanceID
	BindingID                          semreg.NativeBindingID
	SourceID                           semreg.SourceID
	SourceEpochID                      semreg.SourceEpochID
	DriverGeneration                   semreg.Uint64
}

// Admission is one immutable snapshot-bound semantic intent, revision vector,
// route, and external-owner guard claim set. Public accessors return values or
// deep copies; native dispatch and lifecycle are outside this package.
type Admission struct {
	mu sync.Mutex

	intent           Intent
	intentBytes      []byte
	admittedAt       semreg.TimePoint
	admittedContext  semreg.EvaluationContext
	admittedRevision semreg.RevisionVector
	route            Route
	hasRoute         bool
	record           *ExecutionRecord
	recordBytes      []byte
}

func NewKernel(validators ...semreg.PackValidator) (*Kernel, error) {
	return NewKernelWithOptions(KernelOptions{MaxRetainedOperations: DefaultMaxRetainedOperations}, validators...)
}

// NewKernelWithOptions constructs a kernel with an explicit replay-safe
// retention bound. A non-positive or excessively large bound is rejected.
func NewKernelWithOptions(options KernelOptions, validators ...semreg.PackValidator) (*Kernel, error) {
	if options.MaxRetainedOperations <= 0 || options.MaxRetainedOperations > MaxRetainedOperations {
		return nil, opError(semreg.BoundsExceeded, "operation retention capacity")
	}
	registry, err := semreg.NewRegistry(validators...)
	if err != nil {
		return nil, err
	}
	kernel := &Kernel{
		registry:              registry,
		validators:            make(map[semreg.PackRef]*lockedOperationValidator),
		maxRetainedOperations: options.MaxRetainedOperations,
		admissions:            make(map[semreg.IdempotencyKey]*Admission),
	}
	for _, validator := range validators {
		if isNilInterface(validator) {
			continue
		}
		index := validator.Definitions()
		if len(index.Operations) == 0 && len(index.EffectRules) == 0 {
			continue
		}
		operationValidator, ok := validator.(OperationPackValidator)
		if !ok || isNilInterface(operationValidator) {
			return nil, opError(semreg.DefinitionOwnerMissing, "operation pack hook")
		}
		kernel.validators[validator.Pack()] = &lockedOperationValidator{hook: operationValidator}
	}
	return kernel, nil
}

// ValidateIntent performs all state-independent validation and exact pack
// dispatch, including pack-owned expected-effect derivation.
func (k *Kernel) ValidateIntent(intent Intent) error {
	validation, _ := k.validateIntent(intent)
	return validation
}

// validateIntent separates typed structural safety from the final ranked
// admission result. Causal-domain rejections remain safe to inspect and do not
// suppress independent owner, field, effect, or authority diagnostics.
func (k *Kernel) validateIntent(intent Intent) (error, bool) {
	if k == nil || k.registry == nil {
		return opError(semreg.DefinitionOwnerMissing, "operation kernel"), false
	}
	typedErr := intent.Validate()
	if !intentSafeForHooks(typedErr) {
		return typedErr, false
	}
	intent = clone(intent)
	var errs []error
	_, operationOwnerErr := k.registry.Definition(semreg.DefinitionOperation, intent.Kind)
	_, effectOwnerErr := k.registry.Definition(semreg.DefinitionEffectRule, intent.ExpectedEffect.Rule)
	errs = append(errs, operationOwnerErr, effectOwnerErr)

	operationValidator, registered := k.validators[intent.Kind.Pack]
	if !registered {
		errs = append(errs, opError(semreg.DefinitionOwnerMissing, "operation pack hook"))
	}
	packValidator, packErr := k.registry.Validator(intent.Kind.Pack)
	errs = append(errs, packErr)
	constraintsSafe := packErr == nil
	if packErr == nil {
		index := packValidator.Definitions()
		for _, argument := range intent.Arguments {
			var exact *semreg.DefinitionRef
			ambiguous := false
			for _, field := range index.Fields {
				if field.ID == argument.ID && field.Pack == intent.Kind.Pack {
					if exact != nil {
						ambiguous = true
						break
					}
					copy := field
					exact = &copy
				}
			}
			if ambiguous {
				errs = append(errs, opError(semreg.DefinitionOwnerConflict, "operation argument field"))
				constraintsSafe = false
				continue
			}
			if exact == nil {
				errs = append(errs, opError(semreg.DefinitionOwnerMissing, "operation argument field"))
				constraintsSafe = false
				continue
			}
			if operationValidator != nil {
				operationValidator.mu.Lock()
				fieldErr := k.registry.ValidateField(*exact, clone(argument))
				operationValidator.mu.Unlock()
				errs = append(errs, fieldErr)
				if fieldErr != nil {
					constraintsSafe = false
				}
			}
		}
	}
	requirementValidator, requirementPackErr := k.registry.Validator(intent.RequiredCapability.Pack)
	errs = append(errs, requirementPackErr)
	if requirementPackErr == nil {
		found := false
		for _, capability := range requirementValidator.Definitions().Capabilities {
			matches, matchErr := intent.RequiredCapability.Versions.Matches(capability.Version)
			errs = append(errs, matchErr)
			found = found || matchErr == nil && matches && capability.ID == intent.RequiredCapability.DefinitionID
		}
		if !found {
			errs = append(errs, opError(semreg.DefinitionOwnerMissing, "required capability definition"))
		}
	}
	errs = append(errs, k.registry.ValidateFact(intent.ExpectedEffect.Fact, &intent.ExpectedEffect.Expected))
	for _, precondition := range intent.Preconditions {
		errs = append(errs, k.registry.ValidateFact(precondition.Fact, &precondition.Expected))
	}
	if errorBeforeHook := mostSpecific(errs...); errorBeforeHook != nil {
		return mostSpecific(typedErr, errorBeforeHook), constraintsSafe
	}
	operationValidator.mu.Lock()
	hookErr := operationValidator.hook.ValidateIntent(clone(intent))
	operationValidator.mu.Unlock()
	if hookErr != nil {
		errs = append(errs, stableHookError(hookErr, "operation intent"))
	}
	return mostSpecific(append([]error{typedErr}, errs...)...), constraintsSafe
}

func intentSafeForHooks(err error) bool {
	if err == nil {
		return true
	}
	switch semreg.ErrorIdentifier(err) {
	case semreg.CausalBudgetExceeded, semreg.EchoSuppressed:
		return true
	default:
		return false
	}
}

func stableHookError(err error, detail string) error {
	if err == nil {
		return nil
	}
	id := semreg.ErrorIdentifier(err)
	if _, known := operationErrorRank[id]; !known {
		id = semreg.InvalidValue
	}
	return opError(id, detail)
}

// Admit evaluates the supplied immutable snapshot at the caller's trusted
// current context, resolves one route, and atomically installs one idempotency
// entry. Repeating identical admitted bytes returns the same admission.
func (k *Kernel) Admit(snapshot semreg.Snapshot, current semreg.EvaluationContext, intent Intent, authority AuthorityResolver) (*Admission, error) {
	if k == nil {
		return nil, opError(semreg.DefinitionOwnerMissing, "operation kernel")
	}
	typedIntentErr := intent.Validate()
	snapshotErr, currentErr := snapshot.Validate(), current.Validate()
	intentErr, constraintsSafe := k.validateIntent(intent)
	if !intentSafeForHooks(typedIntentErr) || snapshotErr != nil || currentErr != nil {
		return nil, mostSpecific(intentErr, snapshotErr, currentErr)
	}
	intent = clone(intent)
	var intentBytes []byte
	if intentErr == nil {
		var err error
		intentBytes, err = semreg.CanonicalJSON(intent)
		if err != nil {
			return nil, err
		}
		if prior, err := k.lookupIdempotency(intent.IdempotencyKey, intentBytes); prior != nil || err != nil {
			return prior, err
		}
	}

	snapshot = clone(snapshot)
	current = clone(current)
	var errs []error
	errs = append(errs, intentErr)
	errs = append(errs, k.snapshotPackError(snapshot))
	if snapshot.AssetID != intent.AssetID {
		errs = append(errs, opError(semreg.RevisionConflict, "intent asset snapshot"))
	}
	if snapshot.Revisions.Semantic != intent.ExpectedSemanticRevision || snapshot.Revisions.Capabilities != intent.ExpectedCapabilityRevision {
		errs = append(errs, opError(semreg.RevisionConflict, "intent snapshot revisions"))
	}
	if current.EvaluatedAt.ClockID != "clock.utc" {
		errs = append(errs, opError(semreg.InvalidTime, "trusted operation wall clock"))
	}
	view, evaluationErr := semreg.EvaluateSnapshot(snapshot, current)
	errs = append(errs, evaluationErr)
	errs = append(errs, deadlineError(current.EvaluatedAt, intent.Deadline))
	errs = append(errs, causalAdmissionError(intent.Causal, current.EvaluatedAt))
	if evaluationErr == nil {
		errs = append(errs, k.preconditionError(snapshot, view, intent.Preconditions))
	}
	routes, routeErr := k.routes(snapshot, intent, constraintsSafe)
	errs = append(errs, routeErr)
	if isNilInterface(authority) {
		errs = append(errs, opError(semreg.AuthorityMissing, "authority resolver"))
	} else if len(routes) == 1 && routeErr == nil && intentSafeForHooks(intentErr) {
		if err := authority.Authorize(clone(intent), clone(routes[0]), clone(current)); err != nil {
			errs = append(errs, opError(semreg.AuthorityMissing, "authority resolution"))
		}
	}
	if err := mostSpecific(errs...); err != nil {
		return nil, err
	}
	if len(routes) != 1 {
		return nil, opError(semreg.AmbiguousRoute, "eligible route count")
	}
	admission := &Admission{
		intent:           clone(intent),
		intentBytes:      append([]byte(nil), intentBytes...),
		admittedAt:       current.EvaluatedAt,
		admittedContext:  clone(current),
		admittedRevision: snapshot.Revisions,
		route:            clone(routes[0]),
		hasRoute:         true,
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if prior, exists := k.admissions[intent.IdempotencyKey]; exists {
		if bytes.Equal(prior.intentBytes, intentBytes) {
			return prior, nil
		}
		return nil, opError(semreg.SequenceConflict, "idempotency key")
	}
	if len(k.admissions) >= k.maxRetainedOperations {
		return nil, opError(semreg.BoundsExceeded, "operation retention capacity")
	}
	k.admissions[intent.IdempotencyKey] = admission
	return admission, nil
}

func (k *Kernel) snapshotPackError(snapshot semreg.Snapshot) error {
	var errs []error
	for _, service := range snapshot.Services {
		errs = append(errs, k.registry.ValidateService(service))
	}
	for _, capability := range snapshot.Capabilities {
		errs = append(errs, k.registry.ValidateCapability(capability))
	}
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			errs = append(errs, k.registry.ValidateFactCandidate(candidate))
		}
	}
	return mostSpecific(errs...)
}

func (k *Kernel) lookupIdempotency(key semreg.IdempotencyKey, canonical []byte) (*Admission, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	prior, exists := k.admissions[key]
	if !exists {
		return nil, nil
	}
	if !bytes.Equal(prior.intentBytes, canonical) {
		return nil, opError(semreg.SequenceConflict, "idempotency key")
	}
	return prior, nil
}

func deadlineError(now, deadline semreg.TimePoint) error {
	if now.ClockID != "clock.utc" || deadline.ClockID != "clock.utc" {
		return opError(semreg.InvalidTime, "operation deadline clock")
	}
	nowNS, nok := new(big.Int).SetString(string(now.UnixNanoseconds), 10)
	deadlineNS, dok := new(big.Int).SetString(string(deadline.UnixNanoseconds), 10)
	nowU, nuok := new(big.Int).SetString(string(now.UncertaintyNS), 10)
	deadlineU, duok := new(big.Int).SetString(string(deadline.UncertaintyNS), 10)
	if !nok || !dok || !nuok || !duok {
		return opError(semreg.InvalidTime, "operation deadline")
	}
	latestNow := new(big.Int).Add(nowNS, nowU)
	earliestDeadline := new(big.Int).Sub(deadlineNS, deadlineU)
	if latestNow.Cmp(earliestDeadline) >= 0 {
		return opError(semreg.DeadlineExpired, "operation deadline")
	}
	return nil
}

func causalAdmissionError(causal semreg.CausalContext, now semreg.TimePoint) error {
	if causal.Origin.Kind == semreg.OriginNativeObservation || causal.Origin.Kind == semreg.OriginProjection {
		return opError(semreg.EchoSuppressed, "reflected origin cannot authorize intent")
	}
	if err := deadlineError(now, causal.ExpiresAt); err != nil {
		if semreg.ErrorIdentifier(err) == semreg.DeadlineExpired {
			return opError(semreg.CausalBudgetExceeded, "causal expiry")
		}
		return err
	}
	return nil
}

// EnterCausal performs the accepted receiver-ingress order without mutating
// the caller's context on rejection.
func EnterCausal(incoming semreg.CausalContext, receiver semreg.TargetID, now semreg.TimePoint) (semreg.CausalContext, error) {
	incoming = clone(incoming)
	if err := mostSpecific(incoming.Validate(), receiver.Validate(), now.Validate()); err != nil {
		return semreg.CausalContext{}, err
	}
	if err := causalAdmissionError(incoming, now); err != nil {
		return semreg.CausalContext{}, err
	}
	for _, entered := range incoming.Path {
		if entered == receiver {
			return semreg.CausalContext{}, opError(semreg.EchoSuppressed, "causal receiver")
		}
	}
	if incoming.HopCount >= incoming.MaxHops {
		return semreg.CausalContext{}, opError(semreg.CausalBudgetExceeded, "causal receiver capacity")
	}
	result := clone(incoming)
	result.Path = append(result.Path, receiver)
	result.HopCount++
	return result, nil
}

func (k *Kernel) preconditionError(snapshot semreg.Snapshot, view semreg.EvaluationView, preconditions []Precondition) error {
	evaluated := make(map[semreg.CandidateID]semreg.EvaluatedFact, len(view.Facts))
	for _, fact := range view.Facts {
		evaluated[fact.CandidateID] = fact
	}
	var errs []error
	for _, precondition := range preconditions {
		validator, err := k.registry.Validator(semreg.PackRef{ID: precondition.Fact.PackID, Version: precondition.Fact.PackVersion})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		candidate, envelope, found := exactCandidate(snapshot, precondition.Fact, precondition.CandidateID)
		if !found || candidate.Revision != precondition.CandidateRevision {
			errs = append(errs, opError(semreg.PreconditionFailed, "exact candidate"))
			continue
		}
		evaluation, ok := evaluated[candidate.CandidateID]
		eligible := ok && candidate.Quality.Qualification == semreg.QualificationQualified &&
			candidate.Quality.Promotion == semreg.PromotionPromoted && candidate.Quality.Validity == semreg.ValidityGood &&
			evaluation.Freshness == semreg.FreshnessFresh && evaluation.EffectiveAvailability == semreg.AvailabilityAvailable &&
			!candidateInOpenConflict(envelope, candidate.CandidateID)
		if !eligible {
			errs = append(errs, opError(semreg.PreconditionFailed, "candidate eligibility"))
			continue
		}
		matched, predicateErr := validator.EvaluatePredicate(clone(candidate), precondition.Operator, clone(precondition.Expected))
		if predicateErr != nil || !matched {
			errs = append(errs, opError(semreg.PreconditionFailed, "predicate"))
		}
	}
	return mostSpecific(errs...)
}

func exactCandidate(snapshot semreg.Snapshot, key semreg.FactKey, id semreg.CandidateID) (semreg.FactCandidate, semreg.FactEnvelope, bool) {
	for _, envelope := range snapshot.Facts {
		if !factKeyEqual(envelope.Key, key) {
			continue
		}
		for _, candidate := range envelope.Candidates {
			if candidate.CandidateID == id {
				return candidate, envelope, true
			}
		}
		return semreg.FactCandidate{}, envelope, false
	}
	return semreg.FactCandidate{}, semreg.FactEnvelope{}, false
}

func candidateInOpenConflict(envelope semreg.FactEnvelope, id semreg.CandidateID) bool {
	for _, conflict := range envelope.Conflicts {
		if conflict.State != semreg.ConflictOpen {
			continue
		}
		for _, candidate := range conflict.Candidates {
			if candidate == id {
				return true
			}
		}
	}
	return false
}

func (k *Kernel) routes(snapshot semreg.Snapshot, intent Intent, invokeConstraintHook bool) ([]Route, error) {
	bindings := make(map[semreg.NativeBindingID]semreg.NativeBinding, len(snapshot.Bindings))
	links := make(map[semreg.NativeBindingID]semreg.IdentityLink, len(snapshot.IdentityLinks))
	sources := make(map[[2]string]semreg.SourceDescriptor, len(snapshot.Sources))
	services := make(map[semreg.ServiceInstanceID]semreg.ServiceInstance, len(snapshot.Services))
	for _, binding := range snapshot.Bindings {
		bindings[binding.BindingID] = binding
	}
	for _, link := range snapshot.IdentityLinks {
		links[link.BindingID] = link
	}
	for _, source := range snapshot.Sources {
		sources[[2]string{string(source.SourceID), string(source.SourceEpochID)}] = source
	}
	for _, service := range snapshot.Services {
		services[service.InstanceID] = service
	}

	var routes []Route
	var sawDefinition bool
	var skippedConstraints bool
	var candidateErrors []error
	for _, capability := range snapshot.Capabilities {
		if capability.AssetID != intent.AssetID || capability.Definition.Pack != intent.RequiredCapability.Pack ||
			capability.Definition.ID != intent.RequiredCapability.DefinitionID {
			continue
		}
		matches, rangeErr := intent.RequiredCapability.Versions.Matches(capability.Definition.Version)
		if rangeErr != nil {
			candidateErrors = append(candidateErrors, rangeErr)
			continue
		}
		if !matches || intent.RequiredCapability.InstanceID != nil && capability.InstanceID != *intent.RequiredCapability.InstanceID {
			continue
		}
		sawDefinition = true
		var errs []error
		if capability.Revision != intent.ExpectedCapabilityInstanceRevision {
			errs = append(errs, opError(semreg.RevisionConflict, "capability instance revision"))
		}
		binding, bindingOK := bindings[capability.BindingID]
		service, serviceOK := services[capability.ServiceInstance]
		if !bindingOK || !serviceOK {
			candidateErrors = append(candidateErrors, append(errs, opError(semreg.DanglingReference, "operation route"))...)
			continue
		}
		link, linkOK := links[capability.BindingID]
		if !linkOK || link.AssetID != intent.AssetID || link.State != semreg.LinkQualified {
			errs = append(errs, opError(semreg.IdentityNotQualified, "operation identity link"))
		}
		if binding.AssetID != intent.AssetID || service.AssetID != intent.AssetID || service.BindingID != capability.BindingID {
			errs = append(errs, opError(semreg.IdentityNotQualified, "operation asset binding"))
		}
		if binding.SourceEpochID != intent.ExpectedSourceEpochID || capability.SourceEpochID != intent.ExpectedSourceEpochID || service.SourceEpochID != intent.ExpectedSourceEpochID {
			errs = append(errs, opError(semreg.StaleSourceEpoch, "operation route"))
		}
		if binding.DriverGeneration != intent.ExpectedDriverGeneration || capability.DriverGeneration != intent.ExpectedDriverGeneration || service.DriverGeneration != intent.ExpectedDriverGeneration {
			errs = append(errs, opError(semreg.StaleDriverGeneration, "operation route"))
		}
		source, sourceOK := sources[[2]string{string(binding.SourceID), string(binding.SourceEpochID)}]
		if !sourceOK || source.State != semreg.SourceCurrent {
			errs = append(errs, opError(semreg.StaleSourceEpoch, "operation source"))
		}
		if binding.State != semreg.BindingCurrent {
			if binding.State == semreg.BindingFenced {
				errs = append(errs, opError(semreg.StaleDriverGeneration, "operation binding"))
			} else {
				errs = append(errs, opError(semreg.StaleSourceEpoch, "operation binding"))
			}
		}
		if capability.Qualification != semreg.QualificationQualified || service.Qualification != semreg.QualificationQualified {
			errs = append(errs, opError(semreg.CapabilityNotQualified, "operation capability"))
		}
		degraded := capability.Availability == semreg.AvailabilityDegraded || service.Availability == semreg.AvailabilityDegraded
		unavailable := capability.Availability == semreg.AvailabilityUnavailable || capability.Availability == semreg.AvailabilityWithdrawn ||
			service.Availability == semreg.AvailabilityUnavailable || service.Availability == semreg.AvailabilityWithdrawn
		if unavailable || degraded && !intent.RequiredCapability.AllowDegraded {
			errs = append(errs, opError(semreg.CapabilityUnavailable, "operation capability"))
		}
		validator, definitionErr := k.registry.Definition(semreg.DefinitionCapability, capability.Definition)
		if definitionErr != nil {
			errs = append(errs, definitionErr)
		}
		if definitionErr == nil {
			if !invokeConstraintHook {
				skippedConstraints = true
			} else if err := validator.MatchConstraints(clone(capability), clone(intent.Arguments)); err != nil {
				errs = append(errs, opError(semreg.AmbiguousRoute, "capability constraints"))
			}
		}
		if err := mostSpecific(errs...); err != nil {
			candidateErrors = append(candidateErrors, errs...)
			continue
		}
		if invokeConstraintHook {
			routes = append(routes, Route{
				CapabilityInstance: capability.InstanceID,
				ServiceInstance:    capability.ServiceInstance,
				BindingID:          capability.BindingID,
				SourceID:           binding.SourceID,
				SourceEpochID:      capability.SourceEpochID,
				DriverGeneration:   capability.DriverGeneration,
			})
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].CapabilityInstance < routes[j].CapabilityInstance })
	if len(routes) > 1 {
		return nil, opError(semreg.AmbiguousRoute, "eligible route count")
	}
	if len(routes) == 0 {
		if err := mostSpecific(candidateErrors...); err != nil {
			return nil, err
		}
		if skippedConstraints {
			return nil, nil
		}
		if sawDefinition {
			return nil, opError(semreg.AmbiguousRoute, "capability constraints")
		}
		return nil, opError(semreg.AmbiguousRoute, "eligible route count")
	}
	return routes, nil
}

// AdmitSelectionRoute is the public fail-closed boundary for callers that try
// to turn a presentation-only Selection into operation routing authority.
func (k *Kernel) AdmitSelectionRoute(selection semreg.Selection) (*Admission, error) {
	if k == nil {
		return nil, opError(semreg.DefinitionOwnerMissing, "operation kernel")
	}
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	return nil, opError(semreg.RouteSelectionForbidden, "presentation selection route")
}

// AdmitAliasRoute is the public fail-closed boundary for attempts to use a
// compatibility alias as a native endpoint or operation route. Alias storage
// and evaluation remain outside the operation package.
func (k *Kernel) AdmitAliasRoute(legacyID semreg.OpaqueID) (*Admission, error) {
	if k == nil {
		return nil, opError(semreg.DefinitionOwnerMissing, "operation kernel")
	}
	if err := legacyID.Validate(); err != nil {
		return nil, err
	}
	return nil, opError(semreg.AliasNotRoutable, "compatibility alias route")
}

func (a *Admission) Intent() Intent {
	if a == nil {
		return Intent{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return clone(a.intent)
}

func (a *Admission) Route() (Route, bool) {
	if a == nil {
		return Route{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return clone(a.route), a.hasRoute
}

func (a *Admission) AdmittedAt() (semreg.TimePoint, semreg.RevisionVector, bool) {
	if a == nil {
		return semreg.TimePoint{}, semreg.RevisionVector{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.hasRoute {
		return semreg.TimePoint{}, semreg.RevisionVector{}, false
	}
	return a.admittedAt, a.admittedRevision, true
}

// GuardClaims returns the exact source epoch, driver generation, route, and
// capability revision claims that the external native owner must recheck under
// its own lifecycle lock immediately before invoking its adapter.
func (a *Admission) GuardClaims() (GuardClaims, bool) {
	if a == nil {
		return GuardClaims{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.hasRoute {
		return GuardClaims{}, false
	}
	return GuardClaims{
		AssetID:                            a.intent.AssetID,
		ExpectedCapabilityRevision:         a.intent.ExpectedCapabilityRevision,
		CapabilityInstance:                 a.route.CapabilityInstance,
		ExpectedCapabilityInstanceRevision: a.intent.ExpectedCapabilityInstanceRevision,
		ServiceInstance:                    a.route.ServiceInstance,
		BindingID:                          a.route.BindingID,
		SourceID:                           a.route.SourceID,
		SourceEpochID:                      a.route.SourceEpochID,
		DriverGeneration:                   a.route.DriverGeneration,
	}, true
}

func (a *Admission) Recorded() (ExecutionRecord, bool) {
	if a == nil {
		return ExecutionRecord{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.record == nil {
		return ExecutionRecord{}, false
	}
	return clone(*a.record), true
}

// ValidateRetry always fails after a route may have produced a side effect.
// A failed_no_contact record still requires an owner-specific replay policy,
// which v1 intentionally does not invent.
func ValidateRetry(record ExecutionRecord, fallback bool) error {
	if err := record.Validate(); err != nil {
		return err
	}
	if fallback || record.Dispatch == nil || record.Dispatch.PossibleSideEffect || record.Outcome != OutcomeFailedNoContact {
		return opError(semreg.RetryForbidden, "operation retry")
	}
	return opError(semreg.RetryForbidden, "owner replay policy required")
}

func sameRecordAdmission(record ExecutionRecord, admission *Admission) error {
	intentBytes, err := semreg.CanonicalJSON(record.Intent)
	if err != nil {
		return err
	}
	if !bytes.Equal(intentBytes, admission.intentBytes) || record.AdmittedAt == nil || *record.AdmittedAt != admission.admittedAt ||
		record.AdmittedRevision == nil || *record.AdmittedRevision != admission.admittedRevision || record.Route == nil ||
		!reflect.DeepEqual(*record.Route, admission.route) {
		return opError(semreg.RevisionConflict, "execution admission binding")
	}
	return nil
}
