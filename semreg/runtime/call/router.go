package call

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// SnapshotCapture is the SS07 cut port. This package does not import the runtime.
type SnapshotCapture interface {
	Capture(context.Context, driver.Filter) (driver.SemanticCut, error)
}

// Config composes one router. A nil Owner answers UNAVAILABLE and sends nothing.
type Config struct {
	Capture SnapshotCapture
	Owner   IntentOwner
	Now     func() time.Time
}

type flight struct {
	done   chan struct{}
	result driver.OperationResult
	err    error
}

// Router dispatches a closed request to the one setting owner.
type Router struct {
	capture    SnapshotCapture
	owner      IntentOwner
	now        func() time.Time
	kernel     *operation.Kernel
	validators []semreg.PackValidator
	registry   *executorRegistry

	packOnce     sync.Once
	packRegistry *semreg.Registry
	packErr      error

	mu       sync.Mutex
	busy     int
	reserve  int
	results  map[string]driver.OperationResult
	inflight map[string]*flight
	noted    map[semreg.IntentID]string

	resultOrder []string
	notedOrder  []semreg.IntentID
}

// retainedBookkeeping bounds results and noted together to RetainedOperations,
// the same memory bound the kernel keeps for admissions. Each map holds half.
const retainedBookkeeping = RetainedOperations / 2

// NewRouter builds a router on the 8192-retention kernel.
func NewRouter(config Config) (*Router, error) {
	kernel, err := NewBuiltinKernel()
	if err != nil {
		return nil, err
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Router{
		capture:    config.Capture,
		owner:      config.Owner,
		now:        now,
		kernel:     kernel,
		validators: builtinValidators(),
		registry:   newExecutorRegistry(),
		results:    map[string]driver.OperationResult{},
		inflight:   map[string]*flight{},
		noted:      map[semreg.IntentID]string{},
	}, nil
}

// RecordedQualification returns the qualification recorded for an intent.
// It is never used as an admission condition.
func (r *Router) RecordedQualification(id semreg.IntentID) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.noted[id]
	return value, ok
}

// Invoke runs one finite request.
func (r *Router) Invoke(ctx context.Context, request driver.OperationRequest) (driver.OperationResult, error) {
	return r.invoke(ctx, request, false)
}

// InvokeCallBound runs one cancellable in-process request that has no absolute deadline.
func (r *Router) InvokeCallBound(ctx context.Context, request driver.OperationRequest) (driver.OperationResult, error) {
	return r.invoke(ctx, request, true)
}

func (r *Router) invoke(ctx context.Context, request driver.OperationRequest, callBound bool) (result driver.OperationResult, err error) {
	if r.owner == nil {
		return refused(StatusUnavailable, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonUnavailable).result(), nil
	}
	if callBound {
		if refusal := callBoundShape(ctx, request); refusal != nil {
			return refusal.result(), nil
		}
	}
	mode := dispatchMode(ctx)
	if mode == ModeReplay {
		return r.replay(request), nil
	}
	if refusal := validateRequest(request, !callBound); refusal != nil {
		return refusal.result(), nil
	}
	selection, refusal := selectCase(request)
	if refusal != nil {
		return refusal.result(), nil
	}
	var owned *flight
	if !callBound {
		key := requestKey(request)
		r.mu.Lock()
		if r.inflight[key] == nil {
			owned = &flight{done: make(chan struct{})}
			r.inflight[key] = owned
		}
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			r.storeResult(key, result)
			if owned != nil && r.inflight[key] == owned {
				delete(r.inflight, key)
			}
			r.mu.Unlock()
			if owned != nil {
				owned.result, owned.err = result, err
				close(owned.done)
			}
		}()
	}

	cut, err := r.capture.Capture(ctx, driver.Filter{Assets: []semreg.AssetID{request.AssetID}})
	if err != nil || len(cut.Snapshots) == 0 {
		return refused(StatusUnavailable, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonUnavailable).result(), nil
	}
	snap := cut.Snapshots[0]
	resource, refusal := resourceOf(snap, request)
	if refusal != nil {
		return refusal.result(), nil
	}
	intent, err := r.owner.Complete(ctx, Draft{Request: request, Selection: selection, Resource: resource, CallBound: callBound, Now: r.now()})
	if err != nil {
		return failed("semantic.operation.reason.owner:" + string(semreg.ErrorIdentifier(err))).result(), nil
	}
	if altered(request, intent) {
		return failed(ReasonOwnerAltered).result(), nil
	}
	qualification := ""
	if caller, ok := CallerFrom(ctx); ok {
		qualification = caller.Qualification
	}
	r.mu.Lock()
	r.storeNote(intent.IntentID, qualification)
	r.mu.Unlock()
	current := semreg.EvaluationContext{EvaluatedAt: timePoint(r.now()), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:call", Nanoseconds: "1"}}
	admission, kernel, admitErr := r.admit(snap, current, intent, callBound)
	routes := matchingRoutes(cut, request, selection, resource)
	chosen, refusal := chooseRoute(routes, snap)
	if refusal != nil {
		return refusal.result(), nil
	}
	if admitErr != nil {
		return failed("semantic.operation.reason.admission:" + string(semreg.ErrorIdentifier(admitErr))).result(), nil
	}
	r.mu.Lock()
	r.busy++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.busy--
		r.mu.Unlock()
	}()
	admittedRoute, _ := admission.Route()
	if !sameKernelRoute(admittedRoute, chosen) {
		return failed(ReasonRouteMismatch).result(), nil
	}
	if refusal := outsideLimit(selection, chosen.Limits); refusal != nil {
		return refusal.result(), nil
	}
	recheck, err := r.capture.Capture(ctx, driver.Filter{Assets: []semreg.AssetID{request.AssetID}})
	if err != nil || len(recheck.Snapshots) == 0 {
		return noContact().result(), nil
	}
	guard, _ := admission.GuardClaims()
	if refusal := guardRecheck(guard, admittedRoute, recheck.Snapshots[0]); refusal != nil {
		return refusal.result(), nil
	}
	exec, found, exact := r.registry.Lookup(chosen)
	if !found {
		return failed(ReasonExecutorMissing).result(), nil
	}
	if !exact {
		return failed(ReasonRouteMismatch).result(), nil
	}
	if ctx.Err() != nil {
		return noContact().result(), nil
	}
	at, revision, _ := admission.AdmittedAt()
	dispatch := driver.Dispatch{
		Request:          request,
		Route:            admittedRoute,
		Guard:            guard,
		ExpectedEffect:   intent.ExpectedEffect,
		AdmittedAt:       at,
		AdmittedRevision: revision,
	}
	completion, execErr := exec.Execute(ctx, dispatch)
	if execErr != nil && completion.Outcome == "" && completion.Dispatch == nil {
		return driver.OperationResult{Status: StatusUnknown, KernelOutcome: operation.OutcomeIndeterminate, Delivery: operation.DeliveryUnknown, Reason: ReasonUnavailable}, nil
	}
	_, admittedRevision, _ := admission.AdmittedAt()
	if selection.Release {
		completion.Readback = nil
		completion.ReadbackSnapshot = nil
	}
	relation, qualified := r.correlate(intent, admittedRoute, admittedRevision, completion)
	result = project(completion, relation, qualified)
	if !callBound {
		recorded := completion
		storedRelation := relation
		if result.KernelOutcome == operation.OutcomeAcknowledgedUnverified && (completion.Readback == nil || !qualified || relation != operation.ReadbackInconclusive) {
			recorded.Readback = nil
			recorded.ReadbackSnapshot = nil
			storedRelation = ""
		}
		if completion.Readback != nil && completion.Readback.NoCandidate() && result.KernelOutcome != operation.OutcomeConflict {
			recorded.Readback = nil
			recorded.ReadbackSnapshot = nil
			storedRelation = ""
		}
		if recordErr := r.record(kernel, admission, intent, result.KernelOutcome, recorded, storedRelation); recordErr != nil && possibleSend(completion) {
			if result.KernelOutcome == operation.OutcomeApplied || result.KernelOutcome == operation.OutcomeConflict {
				result.Status = StatusUnknown
				result.KernelOutcome = operation.OutcomeIndeterminate
			}
		}
	}
	return result, nil
}

// storeResult keeps the newest retainedBookkeeping results. The caller holds r.mu.
func (r *Router) storeResult(key string, result driver.OperationResult) {
	if _, kept := r.results[key]; !kept {
		r.resultOrder = append(r.resultOrder, key)
		if len(r.resultOrder) > retainedBookkeeping {
			delete(r.results, r.resultOrder[0])
			r.resultOrder = r.resultOrder[1:]
		}
	}
	r.results[key] = result
}

// storeNote keeps the newest retainedBookkeeping qualifications. The caller holds r.mu.
func (r *Router) storeNote(id semreg.IntentID, qualification string) {
	if _, kept := r.noted[id]; !kept {
		r.notedOrder = append(r.notedOrder, id)
		if len(r.notedOrder) > retainedBookkeeping {
			delete(r.noted, r.notedOrder[0])
			r.notedOrder = r.notedOrder[1:]
		}
	}
	r.noted[id] = qualification
}

func (r *Router) admit(snap semreg.Snapshot, current semreg.EvaluationContext, intent operation.Intent, callBound bool) (*operation.Admission, *operation.Kernel, error) {
	replaced := false
	authority := operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error { return nil })
	for {
		r.mu.Lock()
		kernel := r.kernel
		r.reserve++
		r.mu.Unlock()
		var admission *operation.Admission
		var err error
		if callBound {
			admission, err = kernel.AdmitCallBound(snap, current, intent, authority)
		} else {
			admission, err = kernel.Admit(snap, current, intent, authority)
		}
		r.mu.Lock()
		r.reserve--
		if err == nil {
			r.mu.Unlock()
			return admission, kernel, nil
		}
		if !replaced && semreg.ErrorIdentifier(err) == semreg.BoundsExceeded && r.busy == 0 && r.reserve == 0 {
			fresh, kerr := NewBuiltinKernel()
			if kerr != nil {
				r.mu.Unlock()
				return nil, nil, kerr
			}
			r.kernel = fresh
			replaced = true
			r.mu.Unlock()
			continue
		}
		r.mu.Unlock()
		return nil, kernel, err
	}
}

func (r *Router) replay(request driver.OperationRequest) driver.OperationResult {
	key := requestKey(request)
	r.mu.Lock()
	if wait := r.inflight[key]; wait != nil {
		r.mu.Unlock()
		<-wait.done
		return wait.result
	}
	if result, ok := r.results[key]; ok {
		r.mu.Unlock()
		return result
	}
	r.mu.Unlock()
	return failed(ReasonReplay).result()
}

func (r *Router) correlate(intent operation.Intent, route operation.Route, admitted semreg.RevisionVector, completion driver.Completion) (operation.ReadbackRelation, bool) {
	if completion.Readback != nil && completion.Readback.NoCandidate() {
		return r.contradictionRelation(intent, route, admitted, completion), false
	}
	if completion.Readback == nil || completion.ReadbackSnapshot == nil {
		if completion.Readback != nil {
			return completion.Readback.Relation, false
		}
		return "", false
	}
	candidate, envelope, found := findCandidate(*completion.ReadbackSnapshot, completion.Readback.CandidateID)
	if !found {
		return operation.ReadbackInconclusive, false
	}
	relation, err := r.evaluate(intent, candidate)
	if err != nil {
		return operation.ReadbackInconclusive, false
	}
	return relation, readbackQualified(intent, route, admitted, completion, candidate, envelope)
}

// contradictionRelation applies, in both invocation forms, the conditions under
// which Kernel.Record accepts the no-candidate readback as conflict: delivery
// sent with a possible side effect, and a later snapshot that passes the pack
// validation of every record it holds and the kernel's readback checks.
func (r *Router) contradictionRelation(intent operation.Intent, route operation.Route, admitted semreg.RevisionVector, completion driver.Completion) operation.ReadbackRelation {
	readback, snap, dispatch := completion.Readback, completion.ReadbackSnapshot, completion.Dispatch
	if readback == nil || snap == nil || dispatch == nil || readback.Relation != operation.ReadbackContradicts || readback.Validate() != nil || snap.Validate() != nil {
		return operation.ReadbackInconclusive
	}
	if dispatch.Delivery != operation.DeliverySent || !dispatch.PossibleSideEffect {
		return operation.ReadbackInconclusive
	}
	if !r.snapshotPackValid(*snap) {
		return operation.ReadbackInconclusive
	}
	if snap.AssetID != intent.AssetID || snap.SnapshotID != readback.SnapshotID || snap.Revisions != readback.Revisions {
		return operation.ReadbackInconclusive
	}
	if compareUint(readback.Revisions.Semantic, admitted.Semantic) <= 0 {
		return operation.ReadbackInconclusive
	}
	if readback.BindingID != route.BindingID || readback.SourceID != route.SourceID || readback.SourceEpochID != route.SourceEpochID || readback.DriverGeneration != route.DriverGeneration {
		return operation.ReadbackInconclusive
	}
	if !routeStillCurrent(*snap, route, intent.AssetID) {
		return operation.ReadbackInconclusive
	}
	return operation.ReadbackContradicts
}

// snapshotPackValid repeats the pack validation of every service, capability and
// fact candidate of a snapshot that the kernel applies to a readback snapshot.
func (r *Router) snapshotPackValid(snap semreg.Snapshot) bool {
	r.packOnce.Do(func() { r.packRegistry, r.packErr = semreg.NewRegistry(r.validators...) })
	if r.packErr != nil {
		return false
	}
	for _, service := range snap.Services {
		if r.packRegistry.ValidateService(service) != nil {
			return false
		}
	}
	for _, capability := range snap.Capabilities {
		structural := capability
		structural.Qualification, structural.Availability = semreg.QualificationQualified, semreg.AvailabilityAvailable
		if capability.Validate() != nil || r.packRegistry.ValidateCapability(structural) != nil {
			return false
		}
	}
	for _, envelope := range snap.Facts {
		for _, candidate := range envelope.Candidates {
			if r.packRegistry.ValidateFactCandidate(candidate) != nil {
				return false
			}
		}
	}
	return true
}

func (r *Router) evaluate(intent operation.Intent, candidate semreg.FactCandidate) (operation.ReadbackRelation, error) {
	for _, validator := range r.validators {
		if validator.Pack() != intent.Kind.Pack {
			continue
		}
		hook, ok := validator.(operation.OperationPackValidator)
		if !ok {
			return "", errRegister("readback pack")
		}
		return hook.EvaluateReadback(intent, candidate)
	}
	return "", errRegister("readback pack")
}

func (r *Router) record(kernel *operation.Kernel, admission *operation.Admission, intent operation.Intent, outcome operation.Outcome, completion driver.Completion, relation operation.ReadbackRelation) error {
	if outcome == "" || outcome == operation.OutcomeRejected {
		return nil
	}
	at, revision, ok := admission.AdmittedAt()
	route, routed := admission.Route()
	if !ok || !routed {
		return errRegister("admission")
	}
	record := operation.ExecutionRecord{
		Contract:         operation.ContractOperationV1,
		Intent:           admission.Intent(),
		AdmittedAt:       &at,
		AdmittedRevision: &revision,
		Route:            &route,
		Dispatch:         completion.Dispatch,
		Acknowledgement:  completion.Acknowledgement,
		Outcome:          outcome,
		OutcomeEvidence:  completion.OutcomeEvidence,
	}
	if record.OutcomeEvidence == nil {
		record.OutcomeEvidence = []semreg.EvidenceRef{evidenceFor("semantic.caller", "semantic.caller.outcome", "outcome:"+string(intent.IntentID))}
	}
	if completion.Readback != nil {
		readback := *completion.Readback
		readback.Relation = relation
		record.Readback = &readback
	}
	_, err := kernel.Record(admission, record, completion.ReadbackSnapshot)
	return err
}

func callBoundShape(ctx context.Context, request driver.OperationRequest) *routeRefusal {
	if request.Timeout != 0 || ctx == nil || ctx.Done() == nil {
		return failed(ReasonCallBoundInvalid)
	}
	if _, ok := ctx.Deadline(); ok {
		return failed(ReasonCallBoundInvalid)
	}
	if caller, ok := CallerFrom(ctx); ok && caller.Deadline != nil {
		return failed(ReasonCallBoundInvalid)
	}
	switch dispatchMode(ctx) {
	case ModePersisted, ModeAsync, ModeReplay:
		return failed(ReasonCallBoundInvalid)
	}
	return nil
}

func resourceOf(snap semreg.Snapshot, request driver.OperationRequest) (semreg.ResourceID, *routeRefusal) {
	var serviceID semreg.ServiceInstanceID
	found := false
	for _, capability := range snap.Capabilities {
		if capability.InstanceID == request.CapabilityInstanceID && capability.AssetID == request.AssetID {
			serviceID = capability.ServiceInstance
			found = true
		}
	}
	if !found {
		return "", unavailable(ReasonCapabilityMissing)
	}
	for _, service := range snap.Services {
		if service.InstanceID == serviceID && service.AssetID == request.AssetID {
			if service.Resource == nil || *service.Resource == "" {
				return "", unavailable(ReasonResourceMissing)
			}
			return *service.Resource, nil
		}
	}
	return "", unavailable(ReasonServiceMissing)
}

func altered(request driver.OperationRequest, intent operation.Intent) bool {
	if intent.AssetID != request.AssetID || intent.Kind != request.Operation {
		return true
	}
	if intent.ExpectedSemanticRevision != request.ExpectedSemanticRevision || intent.ExpectedCapabilityRevision != request.ExpectedCapabilityRevision || intent.ExpectedCapabilityInstanceRevision != request.ExpectedCapabilityInstanceRevision || intent.ExpectedDriverGeneration != request.ExpectedDriverGeneration || intent.ExpectedSourceEpochID != request.ExpectedSourceEpochID {
		return true
	}
	if intent.RequiredCapability.InstanceID == nil || *intent.RequiredCapability.InstanceID != request.CapabilityInstanceID {
		return true
	}
	if !reflect.DeepEqual(intent.Arguments, request.Arguments) || !reflect.DeepEqual(intent.Preconditions, request.Preconditions) {
		return true
	}
	return false
}

func possibleSend(completion driver.Completion) bool {
	return completion.Dispatch != nil && (completion.Dispatch.PossibleSideEffect || completion.Dispatch.Delivery == operation.DeliverySent || completion.Dispatch.Delivery == operation.DeliveryUnknown)
}

func requestKey(request driver.OperationRequest) string {
	raw, _ := json.Marshal(request)
	return string(raw)
}

func findCandidate(snap semreg.Snapshot, id semreg.CandidateID) (semreg.FactCandidate, semreg.FactEnvelope, bool) {
	for _, envelope := range snap.Facts {
		for _, candidate := range envelope.Candidates {
			if candidate.CandidateID == id {
				return candidate, envelope, true
			}
		}
	}
	return semreg.FactCandidate{}, semreg.FactEnvelope{}, false
}

func readbackQualified(intent operation.Intent, route operation.Route, admitted semreg.RevisionVector, completion driver.Completion, candidate semreg.FactCandidate, envelope semreg.FactEnvelope) bool {
	readback := completion.Readback
	snap := completion.ReadbackSnapshot
	dispatch := completion.Dispatch
	if readback == nil || snap == nil || dispatch == nil || dispatch.Completed == nil {
		return false
	}
	if snap.AssetID != intent.AssetID || snap.SnapshotID != readback.SnapshotID || snap.Revisions != readback.Revisions {
		return false
	}
	if compareUint(readback.Revisions.Semantic, admitted.Semantic) <= 0 {
		return false
	}
	if readback.BindingID != route.BindingID || readback.SourceID != route.SourceID || readback.SourceEpochID != route.SourceEpochID || readback.DriverGeneration != route.DriverGeneration {
		return false
	}
	if candidate.Revision != readback.CandidateRevision || candidate.Quality.Assertion != semreg.AssertionObserved {
		return false
	}
	if candidate.BindingID == nil || candidate.SourceEpochID == nil || candidate.DriverGeneration == nil || candidate.Origin.SourceID == nil {
		return false
	}
	if *candidate.BindingID != readback.BindingID || *candidate.SourceEpochID != readback.SourceEpochID || *candidate.DriverGeneration != readback.DriverGeneration || *candidate.Origin.SourceID != readback.SourceID {
		return false
	}
	if !reflect.DeepEqual(candidate.Key, intent.ExpectedEffect.Fact) {
		return false
	}
	if candidate.Quality.Qualification != semreg.QualificationQualified || candidate.Quality.Promotion != semreg.PromotionPromoted || candidate.Quality.Validity != semreg.ValidityGood || candidate.Quality.Availability != semreg.AvailabilityAvailable {
		return false
	}
	if openConflict(envelope, candidate.CandidateID) {
		return false
	}
	view, err := semreg.EvaluateSnapshot(*snap, readback.Evaluation)
	if err != nil {
		return false
	}
	fresh := false
	for _, fact := range view.Facts {
		if fact.CandidateID == candidate.CandidateID && fact.CandidateRevision == candidate.Revision && fact.Freshness == semreg.FreshnessFresh && fact.EffectiveAvailability == semreg.AvailabilityAvailable {
			fresh = true
		}
	}
	if !fresh || !receiptAfter(*dispatch.Completed, candidate.Times) {
		return false
	}
	return routeStillCurrent(*snap, route, intent.AssetID)
}

func openConflict(envelope semreg.FactEnvelope, id semreg.CandidateID) bool {
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

func receiptAfter(completed semreg.EvaluationContext, times semreg.Times) bool {
	if completed.EvaluateMonotonic.ClockEpochID == times.ReceiptMonotonic.ClockEpochID {
		return compareUint(times.ReceiptMonotonic.Nanoseconds, completed.EvaluateMonotonic.Nanoseconds) > 0
	}
	receipt, rok := pointTime(times.ReceivedAt)
	done, dok := pointTime(completed.EvaluatedAt)
	return rok && dok && receipt.After(done)
}

func routeStillCurrent(snap semreg.Snapshot, route operation.Route, asset semreg.AssetID) bool {
	sourceCurrent := false
	for _, source := range snap.Sources {
		if source.SourceID == route.SourceID && source.SourceEpochID == route.SourceEpochID && source.State == semreg.SourceCurrent {
			sourceCurrent = true
		}
	}
	if !sourceCurrent {
		return false
	}
	for _, fence := range snap.Fences {
		if fence.SourceID == route.SourceID && fence.SourceEpochID == route.SourceEpochID && fence.DriverGeneration == route.DriverGeneration {
			return false
		}
	}
	bindingOK := false
	for _, binding := range snap.Bindings {
		if binding.BindingID == route.BindingID && binding.AssetID == asset && binding.State == semreg.BindingCurrent && binding.SourceID == route.SourceID && binding.SourceEpochID == route.SourceEpochID && binding.DriverGeneration == route.DriverGeneration {
			bindingOK = true
		}
	}
	identityOK := false
	for _, link := range snap.IdentityLinks {
		if link.BindingID == route.BindingID && link.AssetID == asset && link.State == semreg.LinkQualified {
			identityOK = true
		}
	}
	serviceOK := false
	for _, service := range snap.Services {
		if service.InstanceID == route.ServiceInstance && service.AssetID == asset && service.BindingID == route.BindingID && service.SourceEpochID == route.SourceEpochID && service.DriverGeneration == route.DriverGeneration && service.Availability != semreg.AvailabilityWithdrawn {
			serviceOK = true
		}
	}
	capabilityOK := false
	for _, capability := range snap.Capabilities {
		if capability.InstanceID == route.CapabilityInstance && capability.AssetID == asset && capability.ServiceInstance == route.ServiceInstance && capability.BindingID == route.BindingID && capability.SourceEpochID == route.SourceEpochID && capability.DriverGeneration == route.DriverGeneration && capability.Availability != semreg.AvailabilityWithdrawn {
			capabilityOK = true
		}
	}
	return bindingOK && identityOK && serviceOK && capabilityOK
}
