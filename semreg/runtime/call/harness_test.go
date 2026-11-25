package call

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func pubEvidence(material string) semreg.EvidenceRef {
	sum := sha256.Sum256([]byte(material))
	return semreg.EvidenceRef{
		Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Contract: "test.evidence/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone,
	}
}

func digestOf(material string) semreg.Digest {
	sum := sha256.Sum256([]byte(material))
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func qty(coefficient, unit string) semreg.Value {
	return semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: coefficient}, Unit: semreg.DefinitionID(unit)}}
}

func qtyExp(coefficient string, exp int32, unit string) semreg.Value {
	value := qty(coefficient, unit)
	value.Quantity.Number.Exponent10 = exp
	return value
}

func booleanValue(v bool) semreg.Value {
	return semreg.Value{Kind: semreg.ValueBoolean, Boolean: &v}
}

func textDim(id semreg.DefinitionID, resource string) semreg.Dimension {
	return semreg.Dimension{ID: id, Value: textValue(resource)}
}

type plant struct {
	t            *testing.T
	validator    semreg.PackValidator
	operation    semreg.OperationMetadata
	publication  *semreg.PublicationKernel
	snapshot     semreg.Snapshot
	asset        semreg.AssetID
	binding      semreg.NativeBindingID
	source       semreg.SourceID
	epoch        semreg.SourceEpochID
	generation   semreg.Uint64
	service      semreg.ServiceInstanceID
	capability   semreg.CapabilityInstanceID
	resource     semreg.ResourceID
	base         time.Time
	sequence     int
	argumentFact semreg.FactKey
	resources    []semreg.Resource
	linkResource *semreg.ResourceID
	freshFor     string
}

type publishSpec struct {
	name         string
	validator    semreg.PackValidator
	operation    semreg.DefinitionID
	resource     semreg.ResourceID
	asset        semreg.AssetID
	constraints  []semreg.TypedField
	value        semreg.Value
	omitResource bool
	freshFor     string
	skipRegister bool
}

func publishPlant(t *testing.T, spec publishSpec) *plant {
	t.Helper()
	op, ok := findOperation(semreg.DefinitionRef{Pack: spec.validator.Pack(), ID: spec.operation, Version: spec.validator.Pack().Version})
	if !ok {
		t.Fatalf("operation %s is not in pack metadata", spec.operation)
	}
	dimension := fieldDimension(op.Ref.Pack, op.Argument.ID)
	resource := spec.resource
	key := semreg.FactKey{PackID: op.Ref.Pack.ID, PackVersion: op.Ref.Pack.Version, FactID: op.Argument.ID, Dimensions: []semreg.Dimension{textDim(dimension, string(resource))}}
	base := time.Unix(1_700_000_000, 0).UTC()
	plant := &plant{
		t: t, validator: spec.validator, operation: op, asset: spec.asset, binding: "binding:1", source: "source:1", epoch: "epoch:1",
		generation: "1", service: "service:1", capability: "cap:1", resource: resource, base: base, argumentFact: key,
		freshFor: spec.freshFor,
	}
	var publication *semreg.PublicationKernel
	var err error
	var resourcePtr *semreg.ResourceID
	if spec.omitResource {
		publication, err = semreg.NewPublicationKernel(spec.asset, spec.validator)
	} else {
		catalog, catalogErr := graph.Embedded()
		if catalogErr != nil {
			t.Fatal(catalogErr)
		}
		publication, err = semreg.NewGraphPublicationKernel(spec.asset, catalog, spec.validator)
		kind, ok := catalog.ServiceSubjectKind(op.Service)
		if !ok {
			t.Fatalf("service %s has no subject kind", op.Service.ID)
		}
		class, ok := catalog.ResourceKindClass(kind)
		if !ok {
			t.Fatalf("resource kind %s has no anchor class", kind)
		}
		slot := string(resource)
		anchor := semreg.ResourceAnchor{Class: class, Basis: semreg.AnchorOperatorSlot, Slot: &slot, Evidence: []semreg.EvidenceRef{pubEvidence("anchor:" + slot)}}
		if class == semreg.AnchorComponent || class == semreg.AnchorEpisode {
			parent := semreg.ResourceID("resource:parent:" + string(spec.asset))
			anchor.Parent = &parent
		}
		plant.resources = []semreg.Resource{{
			ResourceID: resource, Kind: kind,
			Anchor:   anchor,
			State:    semreg.ResourceCurrent,
			Origin:   semreg.OriginRef{OriginID: semreg.OriginID("origin:operator:" + slot), Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{pubEvidence("origin-resource:" + slot)}},
			Revision: "1",
		}}
		resourcePtr = &plant.resource
		plant.linkResource = resourcePtr
	}
	if err != nil {
		t.Fatal(err)
	}
	plant.publication = publication
	service := semreg.ServiceInstance{
		InstanceID: plant.service, AssetID: plant.asset, Definition: op.Service, BindingID: plant.binding, SourceEpochID: plant.epoch,
		DriverGeneration: plant.generation, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1", Resource: resourcePtr,
	}
	constraints := spec.constraints
	if constraints == nil {
		constraints = []semreg.TypedField{{ID: op.Argument.ID, Value: spec.value}}
	}
	capability := semreg.CapabilityInstance{
		InstanceID: plant.capability, AssetID: plant.asset, ServiceInstance: plant.service, Definition: op.Capability, BindingID: plant.binding,
		SourceEpochID: plant.epoch, DriverGeneration: plant.generation, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable,
		Constraints: constraints, ActivationEvidence: []semreg.EvidenceRef{pubEvidence("activation:" + spec.name)}, Revision: "1",
	}
	fact := plant.candidate(key, spec.value, base, "1000", "candidate:primary", "1")
	batch := plant.batch("batch:"+spec.name+":1", "1", "0", base, "1000", []semreg.FactCandidate{fact}, service, capability, true)
	plant.snapshot = plant.apply(batch, "1000")
	plant.sequence = 1
	return plant
}

func (p *plant) candidate(key semreg.FactKey, value semreg.Value, wall time.Time, ticks, id, revision string) semreg.FactCandidate {
	binding, epoch, generation := p.binding, p.epoch, p.generation
	source := p.source
	return semreg.FactCandidate{
		CandidateID: semreg.CandidateID(id), Key: key, Value: &value,
		Quality:         semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}},
		Times:           semreg.Times{ReceivedAt: timePoint(wall), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:pub", Nanoseconds: semreg.Uint64(ticks)}, EvaluatedAt: timePoint(wall), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:pub", Nanoseconds: semreg.Uint64(ticks)}},
		FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:fresh", Version: "1.0.0", FreshForNS: p.freshWindow(), RetainForNS: "120000000000", MaxWallUncertaintyNS: "0"},
		BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
		Origin:   semreg.OriginRef{OriginID: "origin:native", Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []semreg.EvidenceRef{pubEvidence("origin:" + id)}},
		Evidence: []semreg.EvidenceRef{pubEvidence("fact:" + id)}, Revision: semreg.Uint64(revision),
	}
}

func (p *plant) batch(id, sequence, expected string, observed time.Time, ticks string, facts []semreg.FactCandidate, service semreg.ServiceInstance, capability semreg.CapabilityInstance, initial bool) semreg.PublicationBatch {
	p.t.Helper()
	batch := semreg.PublicationBatch{
		Contract: semreg.ContractKernelV1, BatchID: semreg.BatchID(id), AssetID: p.asset, SourceID: p.source, SourceEpochID: p.epoch, DriverGeneration: p.generation,
		Sequence: semreg.Uint64(sequence), ExpectedSemanticRevision: semreg.Uint64(expected), ObservedAt: timePoint(observed),
		SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{}, IdentityLinkUpserts: []semreg.IdentityLink{},
		FactUpserts: facts, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{},
		CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{},
	}
	if initial {
		batch.SourceUpserts = []semreg.SourceDescriptor{{
			SourceID: p.source, SourceEpochID: p.epoch, ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1",
			RegistryEvidence: pubEvidence("registry:" + string(p.asset)), StartedAt: timePoint(observed.Add(-time.Second)), State: semreg.SourceCurrent, Revision: "1",
		}}
		batch.BindingUpserts = []semreg.NativeBinding{{
			BindingID: p.binding, AssetID: p.asset, SourceID: p.source, SourceEpochID: p.epoch, DriverGeneration: p.generation,
			NativeResource: pubEvidence("native:" + string(p.asset)), State: semreg.BindingCurrent, Revision: "1",
		}}
		batch.ResourceUpserts = p.resources
		batch.IdentityLinkUpserts = []semreg.IdentityLink{{
			AssetID: p.asset, BindingID: p.binding, State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{pubEvidence("link:" + string(p.asset))}, Revision: "1", Resource: p.linkResource,
		}}
		batch.ServiceUpserts = []semreg.ServiceInstance{service}
		batch.CapabilityUpserts = []semreg.CapabilityInstance{capability}
	}
	digest, err := batch.ComputedDigest()
	if err != nil {
		p.t.Fatal(err)
	}
	batch.BatchDigest = digest
	return batch
}

func (p *plant) apply(batch semreg.PublicationBatch, ticks string) semreg.Snapshot {
	p.t.Helper()
	snapshot, _, err := p.publication.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:pub", Nanoseconds: semreg.Uint64(ticks)})
	if err != nil {
		p.t.Fatal(err)
	}
	return snapshot
}

func (p *plant) laterFact(key semreg.FactKey, value semreg.Value, wall time.Time, id, revision string) semreg.Snapshot {
	return p.laterFactReplacing(key, value, wall, id, revision, nil)
}

func (p *plant) laterFactReplacing(key semreg.FactKey, value semreg.Value, wall time.Time, id, revision string, withdraw []semreg.CandidateID) semreg.Snapshot {
	p.t.Helper()
	p.sequence++
	candidate := p.candidate(key, value, wall, "9000", id, revision)
	batch := p.batch("batch:later:"+id, itoa(p.sequence), string(p.snapshot.Revisions.Semantic), wall, "9000", []semreg.FactCandidate{candidate}, semreg.ServiceInstance{}, semreg.CapabilityInstance{}, false)
	if len(withdraw) > 0 {
		batch.FactWithdrawals = withdraw
		digest, err := batch.ComputedDigest()
		if err != nil {
			p.t.Fatal(err)
		}
		batch.BatchDigest = digest
	}
	p.snapshot = p.apply(batch, "9000")
	return p.snapshot
}

func (p *plant) settingRoute(limits *driver.ValueLimits) driver.SettingRoute {
	element := driver.Element{Subject: p.node(), Key: p.argumentFact}
	return driver.SettingRoute{
		For: element, Operation: p.operation.Ref, Owner: driver.SourceRef{For: element, Producer: p.node(), PathKey: "configuration"},
		Route: operation.Route{
			CapabilityInstance: p.capability, ServiceInstance: p.service, BindingID: p.binding, SourceID: p.source, SourceEpochID: p.epoch, DriverGeneration: p.generation,
		},
		Limits: limits,
	}
}

func (p *plant) node() driver.NodeRef {
	return driver.NodeRef{Bridge: "bridge:test", ID: p.resource}
}

func (p *plant) request(arguments []semreg.TypedField, timeout time.Duration) driver.OperationRequest {
	return driver.OperationRequest{
		AssetID: p.asset, BindingID: p.binding, CapabilityInstanceID: p.capability, Operation: p.operation.Ref, Arguments: arguments,
		ExpectedSemanticRevision: p.snapshot.Revisions.Semantic, ExpectedCapabilityRevision: p.snapshot.Revisions.Capabilities,
		ExpectedCapabilityInstanceRevision: p.capabilityRevision(), ExpectedSourceEpochID: p.epoch, ExpectedDriverGeneration: p.generation,
		Preconditions: []operation.Precondition{}, Timeout: timeout,
	}
}

func (p *plant) freshWindow() semreg.Uint64 {
	if p.freshFor == "" {
		return "30000000000"
	}
	return semreg.Uint64(p.freshFor)
}

func (p *plant) capabilityRevision() semreg.Uint64 {
	for _, capability := range p.snapshot.Capabilities {
		if capability.InstanceID == p.capability {
			return capability.Revision
		}
	}
	return "1"
}

func (p *plant) cut(routes []driver.SettingRoute, snap semreg.Snapshot) driver.SemanticCut {
	return driver.SemanticCut{Ref: graph.CutRef{CutID: digestOf("cut:" + string(snap.SnapshotID))}, Snapshots: []semreg.Snapshot{snap}, SettingRoutes: routes}
}

type captureScript struct {
	mu   sync.Mutex
	cuts []driver.SemanticCut
	errs []error
	n    int
}

func (c *captureScript) Capture(context.Context, driver.Filter) (driver.SemanticCut, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.n
	c.n++
	if index < len(c.errs) && c.errs[index] != nil {
		return driver.SemanticCut{}, c.errs[index]
	}
	if len(c.cuts) == 0 {
		return driver.SemanticCut{}, errRegister("capture")
	}
	if index >= len(c.cuts) {
		return c.cuts[len(c.cuts)-1], nil
	}
	return c.cuts[index], nil
}

func (c *captureScript) reset(cuts []driver.SemanticCut) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cuts = cuts
	c.n = 0
	c.errs = nil
}

func (c *captureScript) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

type recordingOwner struct {
	inner IntentOwner
	mu    sync.Mutex
	last  operation.Intent
}

func (o *recordingOwner) Complete(ctx context.Context, draft Draft) (operation.Intent, error) {
	intent, err := o.inner.Complete(ctx, draft)
	o.mu.Lock()
	o.last = intent
	o.mu.Unlock()
	return intent, err
}

func (o *recordingOwner) intent() operation.Intent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.last
}

type scriptExec struct {
	mu               sync.Mutex
	calls            int
	sends            int
	last             driver.Dispatch
	release          chan struct{}
	entered          chan struct{}
	retire           func() bool
	complete         func(context.Context, driver.Dispatch) (driver.Completion, error)
	allowAfterCancel bool
}

func (e *scriptExec) Execute(ctx context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
	e.mu.Lock()
	e.calls++
	e.last = dispatch
	entered := e.entered
	release := e.release
	retire := e.retire
	complete := e.complete
	allow := e.allowAfterCancel
	e.mu.Unlock()
	if entered != nil {
		select {
		case <-entered:
		default:
			close(entered)
		}
	}
	if release != nil {
		<-release
	}
	if retire != nil && retire() {
		return notSent(dispatch), nil
	}
	if ctx.Err() != nil && !allow {
		return notSent(dispatch), nil
	}
	e.mu.Lock()
	e.sends++
	e.mu.Unlock()
	if complete != nil {
		return complete(ctx, dispatch)
	}
	return acknowledged(dispatch), nil
}

func (e *scriptExec) counts() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls, e.sends
}

func acknowledged(dispatch driver.Dispatch) driver.Completion {
	started := timePoint(mustPoint(dispatch.AdmittedAt).Add(time.Second))
	return driver.Completion{
		Outcome: operation.OutcomeAcknowledgedUnverified,
		Dispatch: &operation.DispatchEvidence{
			AttemptID: "attempt:1",
			Started:   semreg.EvaluationContext{EvaluatedAt: dispatch.AdmittedAt, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:exec", Nanoseconds: "2"}},
			Completed: &semreg.EvaluationContext{EvaluatedAt: started, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:exec", Nanoseconds: "3"}},
			Delivery:  operation.DeliverySent, PossibleSideEffect: true, Evidence: []semreg.EvidenceRef{pubEvidence("dispatch")},
		},
		Acknowledgement: &operation.Acknowledgement{State: operation.AckAccepted, At: started, Evidence: []semreg.EvidenceRef{pubEvidence("ack")}},
		OutcomeEvidence: []semreg.EvidenceRef{pubEvidence("outcome")},
	}
}

func notSent(dispatch driver.Dispatch) driver.Completion {
	return driver.Completion{
		Outcome: operation.OutcomeFailedNoContact,
		Dispatch: &operation.DispatchEvidence{
			AttemptID: "attempt:1",
			Started:   semreg.EvaluationContext{EvaluatedAt: dispatch.AdmittedAt, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:exec", Nanoseconds: "2"}},
			Delivery:  operation.DeliveryNotSent, Evidence: []semreg.EvidenceRef{pubEvidence("dispatch-not-sent")},
		},
		OutcomeEvidence: []semreg.EvidenceRef{pubEvidence("outcome-not-sent")},
	}
}

func mustPoint(point semreg.TimePoint) time.Time {
	parsed, ok := pointTime(point)
	if !ok {
		panic("time point")
	}
	return parsed
}

type rig struct {
	plant      *plant
	capture    *captureScript
	owner      *recordingOwner
	router     *Router
	exec       *scriptExec
	scope      Scope
	now        time.Time
	unregister func()
}

func newRig(t *testing.T, spec publishSpec) *rig {
	t.Helper()
	plant := publishPlant(t, spec)
	now := plant.base.Add(time.Second)
	capture := &captureScript{cuts: []driver.SemanticCut{plant.cut([]driver.SettingRoute{plant.settingRoute(nil)}, plant.snapshot)}}
	owner := &recordingOwner{inner: NewIntentOwner("runtime:call")}
	router, err := NewRouter(Config{Capture: capture, Owner: owner, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	exec := &scriptExec{}
	scope := Scope{Instance: "session:1", Generation: 1, Producer: plant.node()}
	var unregister func()
	if !spec.skipRegister {
		unregister, err = router.ScopeOf(scope).RegisterExecutor(plant.settingRoute(nil), exec)
		if err != nil {
			t.Fatal(err)
		}
	}
	return &rig{plant: plant, capture: capture, owner: owner, router: router, exec: exec, scope: scope, now: now, unregister: unregister}
}

func (r *rig) use(routes []driver.SettingRoute, snaps ...semreg.Snapshot) {
	if len(snaps) == 0 {
		snaps = []semreg.Snapshot{r.plant.snapshot}
	}
	cuts := make([]driver.SemanticCut, len(snaps))
	for i, snap := range snaps {
		cuts[i] = r.plant.cut(routes, snap)
	}
	r.capture.reset(cuts)
}

func (r *rig) invoke(ctx context.Context, request driver.OperationRequest) driver.OperationResult {
	r.plant.t.Helper()
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := r.router.Invoke(ctx, request)
	if err != nil {
		r.plant.t.Fatal(err)
	}
	return result
}

func (p *plant) booleanSnapshot(fact semreg.DefinitionID, value bool, wall time.Time) semreg.Snapshot {
	p.t.Helper()
	release, ok := releaseOf(p.operation.Ref.ID)
	if !ok {
		p.t.Fatalf("%s has no release case", p.operation.Ref.ID)
	}
	adapter := newReleasePack(p.validator, releaseCase{operation: p.operation.Ref.ID, field: release.field, effect: release.effect})
	publication, err := semreg.NewPublicationKernel(p.asset, adapter)
	if err != nil {
		p.t.Fatal(err)
	}
	service := semreg.ServiceInstance{
		InstanceID: p.service, AssetID: p.asset, Definition: p.operation.Service, BindingID: p.binding, SourceEpochID: p.epoch,
		DriverGeneration: p.generation, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1",
	}
	var constraints []semreg.TypedField
	for _, capability := range p.snapshot.Capabilities {
		if capability.InstanceID == p.capability {
			constraints = append([]semreg.TypedField(nil), capability.Constraints...)
		}
	}
	capability := semreg.CapabilityInstance{
		InstanceID: p.capability, AssetID: p.asset, ServiceInstance: p.service, Definition: p.operation.Capability, BindingID: p.binding,
		SourceEpochID: p.epoch, DriverGeneration: p.generation, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable,
		Constraints: constraints, ActivationEvidence: []semreg.EvidenceRef{pubEvidence("activation:release:" + string(fact))}, Revision: "1",
	}
	first := releaseBatch(p, "batch:release:1", "1", "0", wall, nil, &service, &capability)
	snap, _, err := publication.Apply(first, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:pub", Nanoseconds: "1000"})
	if err != nil {
		p.t.Fatal(err)
	}
	key := semreg.FactKey{PackID: p.operation.Ref.Pack.ID, PackVersion: p.operation.Ref.Pack.Version, FactID: fact, Dimensions: []semreg.Dimension{textDim(p.argumentFact.Dimensions[0].ID, string(p.resource))}}
	candidate := p.candidate(key, booleanValue(value), wall, "9000", "candidate:release", "1")
	second := releaseBatch(p, "batch:release:2", "2", string(snap.Revisions.Semantic), wall, []semreg.FactCandidate{candidate}, nil, nil)
	snap, _, err = publication.Apply(second, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:pub", Nanoseconds: "9000"})
	if err != nil {
		p.t.Fatal(err)
	}
	return snap
}

func releaseBatch(p *plant, id, sequence, expected string, observed time.Time, facts []semreg.FactCandidate, service *semreg.ServiceInstance, capability *semreg.CapabilityInstance) semreg.PublicationBatch {
	p.t.Helper()
	if facts == nil {
		facts = []semreg.FactCandidate{}
	}
	batch := semreg.PublicationBatch{
		Contract: semreg.ContractKernelV1, BatchID: semreg.BatchID(id), AssetID: p.asset, SourceID: p.source, SourceEpochID: p.epoch, DriverGeneration: p.generation,
		Sequence: semreg.Uint64(sequence), ExpectedSemanticRevision: semreg.Uint64(expected), ObservedAt: timePoint(observed),
		SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{}, IdentityLinkUpserts: []semreg.IdentityLink{},
		FactUpserts: facts, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{},
		CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{},
	}
	if service != nil {
		batch.SourceUpserts = []semreg.SourceDescriptor{{
			SourceID: p.source, SourceEpochID: p.epoch, ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1",
			RegistryEvidence: pubEvidence("registry:release:" + string(p.asset)), StartedAt: timePoint(observed.Add(-time.Second)), State: semreg.SourceCurrent, Revision: "1",
		}}
		batch.BindingUpserts = []semreg.NativeBinding{{
			BindingID: p.binding, AssetID: p.asset, SourceID: p.source, SourceEpochID: p.epoch, DriverGeneration: p.generation,
			NativeResource: pubEvidence("native:release:" + string(p.asset)), State: semreg.BindingCurrent, Revision: "1",
		}}
		batch.IdentityLinkUpserts = []semreg.IdentityLink{{
			AssetID: p.asset, BindingID: p.binding, State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{pubEvidence("link:release:" + string(p.asset))}, Revision: "1",
		}}
		batch.ServiceUpserts = []semreg.ServiceInstance{*service}
		batch.CapabilityUpserts = []semreg.CapabilityInstance{*capability}
	}
	digest, err := batch.ComputedDigest()
	if err != nil {
		p.t.Fatal(err)
	}
	batch.BatchDigest = digest
	return batch
}

func readbackCompletion(dispatch driver.Dispatch, snap semreg.Snapshot, candidate semreg.CandidateID, revision semreg.Uint64, wall time.Time, outcome operation.Outcome) driver.Completion {
	completion := acknowledged(dispatch)
	completion.Outcome = outcome
	completion.ReadbackSnapshot = &snap
	completion.Readback = &operation.Readback{
		SnapshotID: snap.SnapshotID, Revisions: snap.Revisions, CandidateID: candidate, CandidateRevision: revision,
		BindingID: dispatch.Route.BindingID, SourceID: dispatch.Route.SourceID, SourceEpochID: dispatch.Route.SourceEpochID, DriverGeneration: dispatch.Route.DriverGeneration,
		Relation: operation.ReadbackConfirms, Evaluation: semreg.EvaluationContext{EvaluatedAt: timePoint(wall), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:read", Nanoseconds: "4"}},
		Evidence: []semreg.EvidenceRef{pubEvidence("readback:" + string(candidate))},
	}
	return completion
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	return string(digits[i:])
}
