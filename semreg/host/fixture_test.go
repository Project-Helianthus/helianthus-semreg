package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/call"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/catalog"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

const (
	kindOwner    driver.DriverKind = "synth.owner"
	kindConsumer driver.DriverKind = "synth.consumer"
	kindPlain    driver.DriverKind = "synth.plain"

	zoneResource       v1.ResourceID = "resource:zone:synthetic"
	initialCoefficient               = "29315"
	targetCoefficient                = "29815"
)

// fixedClock is a mutable installation clock for the synthetic host.
type fixedClock struct {
	mu   sync.Mutex
	now  time.Time
	mono uint64
}

func newFixedClock(at time.Time) *fixedClock { return &fixedClock{now: at} }

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) set(at time.Time) {
	c.mu.Lock()
	c.now = at
	c.mu.Unlock()
}

func (c *fixedClock) Sample() (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mono, nil
}

func (c *fixedClock) Anchor(sample uint64) ([]byte, error) { return []byte(fmt.Sprint(sample)), nil }

func (c *fixedClock) Elapsed(anchor []byte, sample uint64) (uint64, error) {
	var base uint64
	if _, err := fmt.Sscan(string(anchor), &base); err != nil {
		return 0, err
	}
	if sample < base {
		return 0, errors.New("backward sample")
	}
	return sample - base, nil
}

func evidenceOf(label string) v1.EvidenceRef {
	sum := sha256.Sum256([]byte("host test " + label))
	return v1.EvidenceRef{
		Owner: "owner.test", Kind: "evidence.test", Digest: v1.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Contract: "test.contract/v1", Access: v1.EvidenceAccessPublic, Redaction: v1.RedactionNone,
	}
}

func timePoint(t time.Time) v1.TimePoint {
	return v1.TimePoint{UnixNanoseconds: v1.Int64(strconv.FormatInt(t.UnixNano(), 10)), ClockID: "clock.utc", UncertaintyNS: "0"}
}

func kelvin(coefficient string) v1.Value {
	return v1.Value{Kind: v1.ValueQuantity, Quantity: &v1.Quantity{Number: v1.Decimal{Coefficient: coefficient, Exponent10: -2}, Unit: "unit.kelvin"}}
}

func coefficientOfValue(value *v1.Value) string {
	if value == nil || value.Quantity == nil {
		return ""
	}
	return value.Quantity.Number.Coefficient
}

type operationFacts struct {
	op        v1.OperationMetadata
	dimension v1.DefinitionID
	kind      v1.DefinitionID
	class     v1.AnchorClass
	argument  v1.FactKey
}

func thermalSetting(_ testing.TB) operationFacts {
	meta := thermal.New().(v1.PackMetadataProvider).Metadata()
	var out operationFacts
	for _, op := range meta.Operations {
		if op.Ref.ID == "thermal.operation.set_temperature" {
			out.op = op
		}
	}
	if out.op.Ref.ID == "" {
		panic("thermal set_temperature is not in the pack metadata")
	}
	for _, field := range meta.Fields {
		if field.Ref.ID == out.op.Argument.ID {
			out.dimension = field.Dimension.ID
		}
	}
	catalog, err := graph.Embedded()
	if err != nil {
		panic(err)
	}
	kind, _ := catalog.ServiceSubjectKind(out.op.Service)
	class, _ := catalog.ResourceKindClass(kind)
	out.kind, out.class = kind, class
	text := string(zoneResource)
	out.argument = v1.FactKey{
		PackID: out.op.Ref.Pack.ID, PackVersion: out.op.Ref.Pack.Version, FactID: out.op.Argument.ID,
		Dimensions: []v1.Dimension{{ID: out.dimension, Value: v1.Value{Kind: v1.ValueText, Text: &text}}},
	}
	return out
}

// ownerState is what the synthetic setting owner holds: the one setting, the
// operation it last executed and its count.
type ownerState struct {
	mu       sync.Mutex
	value    string
	last     driver.Dispatch
	executed int
	sends    int
}

func (s *ownerState) read() (string, driver.Dispatch, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.last, s.executed
}

// synthFactory is the one synthetic factory of the host tests. It plans the
// three kinds and constructs instances by kind.
type synthFactory struct {
	mu          sync.Mutex
	clock       *fixedClock
	owner       *ownerState
	planned     map[string]driver.Config
	constructed []string
	started     map[string]*consumerInstance
	calendars   map[string]driver.Calendar
	families    []catalog.Family
	argv        [][]string
	owners      []*ownerInstance
	startErrs   []error
	hangStop    bool
}

func newSynthFactory(clock *fixedClock) *synthFactory {
	return &synthFactory{
		clock: clock, owner: &ownerState{value: initialCoefficient}, planned: map[string]driver.Config{},
		started: map[string]*consumerInstance{}, calendars: map[string]driver.Calendar{},
	}
}

type claimSpec struct {
	Provider string `json:"provider"`
	Config   string `json:"config"`
	Domain   string `json:"domain"`
}

func (f *synthFactory) Descriptor() driver.Descriptor { return driver.Descriptor{Families: f.families} }

func (f *synthFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	f.mu.Lock()
	f.planned[c.Instance] = driver.Config{
		Instance: c.Instance, Enabled: c.Enabled, Transport: append(json.RawMessage(nil), c.Transport...),
		Options: append(json.RawMessage(nil), c.Options...), SecretRef: c.SecretRef,
	}
	f.mu.Unlock()
	var options struct {
		Kind driver.DriverKind `json:"kind"`
	}
	if len(c.Options) > 0 {
		if err := json.Unmarshal(c.Options, &options); err != nil {
			return nil, err
		}
	}
	if options.Kind == "" {
		return nil, nil
	}
	plan := driver.InstancePlan{Driver: options.Kind, Instance: driver.InstanceKey(c.Instance)}
	var specs []claimSpec
	if len(c.Transport) > 0 {
		if err := json.Unmarshal(c.Transport, &specs); err != nil {
			return nil, err
		}
	}
	for _, spec := range specs {
		plan.Transports = append(plan.Transports, driver.TransportClaim{
			Provider: spec.Provider, ConfigKey: spec.Config, AccessDomain: spec.Domain, Share: driver.ShareSerialized,
		})
	}
	return []driver.InstancePlan{plan}, nil
}

func (f *synthFactory) Construct(_ context.Context, plan driver.InstancePlan) (driver.Instance, error) {
	f.mu.Lock()
	f.constructed = append(f.constructed, string(plan.Driver)+"/"+string(plan.Instance))
	f.mu.Unlock()
	switch plan.Driver {
	case kindOwner:
		return &ownerInstance{f: f, plan: plan}, nil
	case kindConsumer:
		return &consumerInstance{f: f, plan: plan}, nil
	}
	return &plainInstance{f: f, plan: plan}, nil
}

func (f *synthFactory) ownerInstances() []*ownerInstance {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*ownerInstance(nil), f.owners...)
}

func (f *synthFactory) constructedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.constructed...)
}

func (f *synthFactory) DispatchEarly(_ context.Context, argv []string) (bool, int, error) {
	f.mu.Lock()
	f.argv = append(f.argv, append([]string(nil), argv...))
	f.mu.Unlock()
	if len(argv) > 1 && argv[1] == "synth-child" {
		return true, 7, nil
	}
	if len(argv) > 1 && argv[1] == "synth-fail" {
		return true, 0, errors.New("synthetic child failed")
	}
	return false, 0, nil
}

type plainInstance struct {
	f    *synthFactory
	plan driver.InstancePlan
}

func (i *plainInstance) Start(_ context.Context, s driver.Session) error {
	i.f.mu.Lock()
	i.f.calendars[string(i.plan.Instance)] = s.Calendar()
	i.f.mu.Unlock()
	return nil
}
func (i *plainInstance) FenceWithdrawal() {}
func (i *plainInstance) Stop(context.Context) driver.CloseReport {
	return driver.CloseReport{Confirmed: true}
}

// consumerInstance holds the session of a consuming instance.
type consumerInstance struct {
	f       *synthFactory
	plan    driver.InstancePlan
	session driver.Session
}

func (i *consumerInstance) Start(_ context.Context, s driver.Session) error {
	i.session = s
	i.f.mu.Lock()
	i.f.started[string(i.plan.Instance)] = i
	i.f.mu.Unlock()
	return nil
}
func (i *consumerInstance) FenceWithdrawal() {}
func (i *consumerInstance) Stop(context.Context) driver.CloseReport {
	return driver.CloseReport{Confirmed: true}
}

// ownerInstance is the synthetic setting owner. Its Start publishes one setting
// and registers the executor that changes it.
type ownerInstance struct {
	f          *synthFactory
	plan       driver.InstancePlan
	session    driver.Session
	facts      operationFacts
	producer   driver.NodeRef
	binding    v1.NativeBindingID
	current    v1.CandidateID
	sequence   int
	unregister func()
	route      driver.SettingRoute
	started    atomic.Bool
}

func (i *ownerInstance) element() driver.Element {
	return driver.Element{Subject: driver.NodeRef{Bridge: i.producer.Bridge, ID: zoneResource}, Key: i.facts.argument}
}

func (i *ownerInstance) fact(id string, revision string, coefficient string) v1.FactCandidate {
	binding := i.binding
	epoch := v1.SourceEpochID("placeholder")
	generation := v1.Uint64("1")
	source := v1.SourceID("placeholder")
	now := i.f.clock.Now()
	value := kelvin(coefficient)
	return v1.FactCandidate{
		CandidateID: v1.CandidateID(id), Key: i.facts.argument, Value: &value,
		Quality:         v1.Quality{Assertion: v1.AssertionObserved, Qualification: v1.QualificationQualified, Promotion: v1.PromotionPromoted, Validity: v1.ValidityGood, Availability: v1.AvailabilityAvailable, Freshness: v1.FreshnessFresh, Reasons: []v1.DefinitionID{}},
		Times:           v1.Times{ReceivedAt: timePoint(now), EvaluatedAt: timePoint(now)},
		FreshnessPolicy: v1.FreshnessPolicy{PolicyID: "policy:fresh", Version: "1.0.0", FreshForNS: "30000000000", RetainForNS: "120000000000", MaxWallUncertaintyNS: "0"},
		BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
		Origin:   v1.OriginRef{OriginID: "origin:native", Kind: v1.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []v1.EvidenceRef{evidenceOf("origin:" + id)}},
		Evidence: []v1.EvidenceRef{evidenceOf("fact:" + id)}, Revision: v1.Uint64(revision),
	}
}

func (i *ownerInstance) Start(ctx context.Context, s driver.Session) (err error) {
	defer func() {
		if err != nil {
			i.f.mu.Lock()
			i.f.startErrs = append(i.f.startErrs, err)
			i.f.mu.Unlock()
		}
	}()
	i.session = s
	i.f.mu.Lock()
	i.f.owners = append(i.f.owners, i)
	i.f.mu.Unlock()
	i.facts = thermalSetting(nil)
	i.producer = s.Native().(interface{ Root() driver.NodeRef }).Root()
	i.binding = "binding:owner"
	slot := string(zoneResource)
	anchor := v1.ResourceAnchor{Class: i.facts.class, Basis: v1.AnchorOperatorSlot, Slot: &slot, Evidence: []v1.EvidenceRef{evidenceOf("anchor:" + slot)}}
	if i.facts.class == v1.AnchorComponent || i.facts.class == v1.AnchorEpisode {
		parent := v1.ResourceID("resource:parent:synthetic")
		anchor.Parent = &parent
	}
	resourceID := zoneResource
	resource := v1.Resource{
		ResourceID: zoneResource, Kind: i.facts.kind, Anchor: anchor, State: v1.ResourceCurrent,
		Origin:   v1.OriginRef{OriginID: v1.OriginID("origin:operator:" + slot), Kind: v1.OriginOperator, Evidence: []v1.EvidenceRef{evidenceOf("origin-resource:" + slot)}},
		Revision: "1",
	}
	i.current = "candidate:primary:1"
	i.f.owner.mu.Lock()
	initial := i.f.owner.value
	i.f.owner.mu.Unlock()
	fact := i.fact(string(i.current), "1", initial)
	service := v1.ServiceInstance{
		InstanceID: "service:owner", Definition: i.facts.op.Service, BindingID: i.binding,
		Qualification: v1.QualificationQualified, Availability: v1.AvailabilityAvailable, Revision: "1", Resource: &resourceID,
	}
	capability := v1.CapabilityInstance{
		InstanceID: "cap:owner", ServiceInstance: "service:owner", Definition: i.facts.op.Capability, BindingID: i.binding,
		Qualification: v1.QualificationQualified, Availability: v1.AvailabilityAvailable,
		Constraints:        []v1.TypedField{{ID: i.facts.op.Argument.ID, Value: kelvin(targetCoefficient)}},
		ActivationEvidence: []v1.EvidenceRef{evidenceOf("activation")}, Revision: "1",
	}
	route := driver.SettingRoute{
		For: i.element(), Operation: i.facts.op.Ref, Owner: driver.SourceRef{For: i.element(), Producer: i.producer, PathKey: "configuration"},
		Route: operation.Route{CapabilityInstance: "cap:owner", ServiceInstance: "service:owner", BindingID: i.binding},
	}
	i.route = route
	unregister, err := s.RegisterExecutor(route, i)
	if err != nil {
		return err
	}
	i.unregister = unregister
	tx, err := s.Publisher().Begin(ctx)
	if err != nil {
		return err
	}
	batch := v1.PublicationBatch{
		SourceUpserts: []v1.SourceDescriptor{{
			ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1",
			RegistryEvidence: evidenceOf("registry"), StartedAt: timePoint(i.f.clock.Now().Add(-time.Second)), State: v1.SourceCurrent, Revision: "1",
		}},
		BindingUpserts:      []v1.NativeBinding{{BindingID: i.binding, NativeResource: evidenceOf("native"), State: v1.BindingCurrent, Revision: "1"}},
		ResourceUpserts:     []v1.Resource{resource},
		IdentityLinkUpserts: []v1.IdentityLink{{BindingID: i.binding, State: v1.LinkQualified, Basis: []v1.EvidenceRef{evidenceOf("link")}, Revision: "1", Resource: &resourceID}},
		FactUpserts:         []v1.FactCandidate{fact}, ServiceUpserts: []v1.ServiceInstance{service}, CapabilityUpserts: []v1.CapabilityInstance{capability},
		ObservedAt: timePoint(i.f.clock.Now()),
	}
	if err := tx.Stage(batch); err != nil {
		_ = tx.Abort(ctx)
		return err
	}
	if err := tx.DeclareSource(driver.Source{For: i.element(), Producer: i.producer, PathKey: "configuration", Priority: 10}); err != nil {
		_ = tx.Abort(ctx)
		return err
	}
	if err := tx.DeclareSettingRoute(route); err != nil {
		_ = tx.Abort(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	i.started.Store(true)
	return nil
}

func (i *ownerInstance) FenceWithdrawal() {}

func (i *ownerInstance) Stop(context.Context) driver.CloseReport {
	if i.f.hangStop {
		select {}
	}
	if i.unregister != nil {
		i.unregister()
	}
	return driver.CloseReport{Confirmed: true}
}

// Execute is the native callback: it changes the setting, publishes the new
// value and acknowledges.
func (i *ownerInstance) Execute(ctx context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
	i.f.owner.mu.Lock()
	i.f.owner.executed++
	i.f.owner.last = dispatch
	i.f.owner.mu.Unlock()
	if ctx.Err() != nil {
		return driver.Completion{}, ctx.Err()
	}
	value := dispatch.Request.Arguments[0].Value
	coefficient := coefficientOfValue(&value)
	i.sequence++
	old := i.current
	i.current = v1.CandidateID(fmt.Sprintf("candidate:primary:%d", i.sequence+1))
	tx, err := i.session.Publisher().Begin(ctx)
	if err != nil {
		return driver.Completion{}, err
	}
	batch := v1.PublicationBatch{
		FactUpserts: []v1.FactCandidate{i.fact(string(i.current), "1", coefficient)}, FactWithdrawals: []v1.CandidateID{old},
		ObservedAt: timePoint(i.f.clock.Now()),
	}
	if err := tx.Stage(batch); err != nil {
		_ = tx.Abort(ctx)
		return driver.Completion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return driver.Completion{}, err
	}
	i.f.owner.mu.Lock()
	i.f.owner.value = coefficient
	i.f.owner.sends++
	i.f.owner.mu.Unlock()
	started := timePoint(i.f.clock.Now())
	return driver.Completion{
		Outcome: operation.OutcomeAcknowledgedUnverified,
		Dispatch: &operation.DispatchEvidence{
			AttemptID: "attempt:1",
			Started:   v1.EvaluationContext{EvaluatedAt: dispatch.AdmittedAt, EvaluateMonotonic: v1.MonotonicPoint{ClockEpochID: "clock-epoch:exec", Nanoseconds: "2"}},
			Completed: &v1.EvaluationContext{EvaluatedAt: started, EvaluateMonotonic: v1.MonotonicPoint{ClockEpochID: "clock-epoch:exec", Nanoseconds: "3"}},
			Delivery:  operation.DeliverySent, PossibleSideEffect: true, Evidence: []v1.EvidenceRef{evidenceOf("dispatch")},
		},
		Acknowledgement: &operation.Acknowledgement{State: operation.AckAccepted, At: started, Evidence: []v1.EvidenceRef{evidenceOf("ack")}},
		OutcomeEvidence: []v1.EvidenceRef{evidenceOf("outcome")},
	}, nil
}

var _ = call.Scope{}
var _ = tree.CutID("")
