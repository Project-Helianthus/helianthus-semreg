package driver_test

import (
	"context"
	"errors"
	"sync"
	"time"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/catalog"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// The types below are synthetic implementations written against the public
// contract only. They stand in for a runtime and a driver; they import neither.

type syntheticFactory struct {
	families []catalog.Family
	plan     func(driver.Config) []driver.InstancePlan
}

func (f syntheticFactory) Descriptor() driver.Descriptor {
	return driver.Descriptor{Families: f.families}
}

func (f syntheticFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	if !c.Enabled {
		return nil, nil
	}
	return f.plan(c), nil
}

func (f syntheticFactory) Construct(_ context.Context, p driver.InstancePlan) (driver.Instance, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &syntheticInstance{plan: p}, nil
}

type earlyFactory struct {
	syntheticFactory
	args []string
}

func (f *earlyFactory) DispatchEarly(_ context.Context, args []string) (bool, int, error) {
	f.args = args
	if len(args) > 0 && args[0] == "child" {
		return true, 3, nil
	}
	if len(args) > 0 && args[0] == "fail" {
		return true, 0, errors.New("child failed")
	}
	return false, 0, nil
}

type syntheticInstance struct {
	plan    driver.InstancePlan
	session driver.Session
	fenced  bool
}

func (i *syntheticInstance) Start(_ context.Context, s driver.Session) error {
	i.session = s
	return nil
}

func (i *syntheticInstance) FenceWithdrawal() { i.fenced = true }

func (i *syntheticInstance) Stop(context.Context) driver.CloseReport {
	return driver.CloseReport{Confirmed: true}
}

type syntheticCalendarReference struct{ at time.Time }

func (r syntheticCalendarReference) Instant() time.Time { return r.at }
func (r syntheticCalendarReference) Date() driver.CalendarDate {
	return driver.CalendarDate{Year: r.at.Year(), Month: r.at.Month(), Day: r.at.Day()}
}
func (r syntheticCalendarReference) Year() int         { return r.at.Year() }
func (r syntheticCalendarReference) PreviousYear() int { return r.at.Year() - 1 }
func (r syntheticCalendarReference) Month() driver.CalendarMonth {
	return driver.CalendarMonth{Year: r.at.Year(), Month: r.at.Month()}
}
func (r syntheticCalendarReference) PreviousMonth() driver.CalendarMonth {
	p := r.at.AddDate(0, -1, 0)
	return driver.CalendarMonth{Year: p.Year(), Month: p.Month()}
}

type syntheticAcquisition struct {
	owner *syntheticCalendar
	at    time.Time
}

func (a syntheticAcquisition) Reference() driver.CalendarReference {
	return syntheticCalendarReference{at: a.at}
}

func (a syntheticAcquisition) Check(c driver.Calendar) error {
	if c != driver.Calendar(a.owner) {
		return errors.New("another calendar")
	}
	return nil
}

type syntheticCalendar struct{ now time.Time }

func (c *syntheticCalendar) BeginAcquisition() driver.CalendarAcquisition {
	return syntheticAcquisition{owner: c, at: c.now}
}

type syntheticNative struct {
	nodes   []driver.NodeSpec
	members []driver.MemberSpec
	methods []driver.MethodSpec
	readers map[driver.MemberID]driver.NativeReader
	invoke  map[driver.MemberID]driver.NativeMethod
}

func (n *syntheticNative) Node(s driver.NodeSpec) (driver.NodeRef, error) {
	if err := s.Validate(); err != nil {
		return driver.NodeRef{}, err
	}
	n.nodes = append(n.nodes, s)
	return driver.NodeRef{Bridge: "bridge.local", ID: v1.ResourceID("resource:" + string(s.Local))}, nil
}

func (n *syntheticNative) ReadMember(_ driver.NodeRef, s driver.MemberSpec, r driver.NativeReader) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if n.readers == nil {
		n.readers = map[driver.MemberID]driver.NativeReader{}
	}
	n.members = append(n.members, s)
	n.readers[s.Local] = r
	return nil
}

func (n *syntheticNative) Method(_ driver.NodeRef, s driver.MethodSpec, m driver.NativeMethod) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if n.invoke == nil {
		n.invoke = map[driver.MemberID]driver.NativeMethod{}
	}
	n.methods = append(n.methods, s)
	n.invoke[s.Local] = m
	return nil
}

type syntheticTransaction struct {
	links     []v1.IdentityLink
	claims    []driver.ManufacturerSerialClaim
	batches   []v1.PublicationBatch
	sources   []driver.Source
	routes    []driver.SettingRoute
	committed bool
	aborted   bool
}

func (t *syntheticTransaction) Stage(b v1.PublicationBatch) error {
	t.batches = append(t.batches, b)
	return nil
}

func (t *syntheticTransaction) DeclareSource(s driver.Source) error {
	if err := s.Validate(); err != nil {
		return err
	}
	t.sources = append(t.sources, s)
	return nil
}

func (t *syntheticTransaction) DeclareSettingRoute(r driver.SettingRoute) error {
	if err := r.Validate(); err != nil {
		return err
	}
	t.routes = append(t.routes, r)
	return nil
}

func (t *syntheticTransaction) StageSample(s driver.Sample) error { return s.Validate() }

func (t *syntheticTransaction) StageLink(c driver.LinkClaim) error { return c.Validate() }

func (t *syntheticTransaction) Commit(context.Context) error {
	t.committed = true
	return nil
}

func (t *syntheticTransaction) Abort(context.Context) error {
	t.aborted = true
	return nil
}

func (t *syntheticTransaction) OperatorLinks(bindings []v1.NativeBinding) []v1.IdentityLink {
	out := make([]v1.IdentityLink, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, v1.IdentityLink{AssetID: b.AssetID, BindingID: b.BindingID, State: v1.LinkCandidate, Basis: evidenceSet("operator"), Revision: "1"})
	}
	t.links = out
	return out
}

func (t *syntheticTransaction) AdmitManufacturerSerial(c driver.ManufacturerSerialClaim) error {
	if err := c.Validate(); err != nil {
		return err
	}
	t.claims = append(t.claims, c)
	return nil
}

type syntheticPublisher struct{ tx *syntheticTransaction }

func (p syntheticPublisher) Begin(context.Context) (driver.PublicationTransaction, error) {
	return p.tx, nil
}

type syntheticInput struct {
	cut      driver.SemanticCut
	resumed  driver.Changes
	lens     graph.LensResult
	sum      graph.MemberSum
	captured []driver.Filter
}

func (s *syntheticInput) Capture(_ context.Context, f driver.Filter) (driver.SemanticCut, error) {
	s.captured = append(s.captured, f)
	return s.cut, nil
}

func (s *syntheticInput) Resume(context.Context, driver.Cursor, driver.Filter) (driver.Changes, error) {
	return s.resumed, nil
}

func (s *syntheticInput) RetrieveLens(_ context.Context, at graph.CutRef, _ graph.LensRequest) (graph.LensResult, error) {
	r := s.lens
	r.Cut = at
	return r, nil
}

func (s *syntheticInput) SumMembers(context.Context, graph.CutRef, graph.LensRequest, v1.DefinitionRef, v1.ResourceID) (graph.MemberSum, error) {
	return s.sum, nil
}

type invocation struct {
	request   driver.OperationRequest
	callBound bool
}

type syntheticOperations struct {
	mu    sync.Mutex
	calls []invocation
}

func (o *syntheticOperations) record(r driver.OperationRequest, callBound bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, invocation{request: r, callBound: callBound})
}

func (o *syntheticOperations) Invoke(_ context.Context, r driver.OperationRequest) (driver.OperationResult, error) {
	o.record(r, false)
	return driver.OperationResult{Status: "accepted"}, nil
}

func (o *syntheticOperations) InvokeCallBound(_ context.Context, r driver.OperationRequest) (driver.OperationResult, error) {
	o.record(r, true)
	return driver.OperationResult{Status: "accepted", CompoundExecution: &driver.CompoundExecution{Partial: true, Steps: []driver.CompoundStep{{Point: "point.one", Attempted: true}, {Point: "point.two"}}}}, nil
}

type registration struct {
	route    driver.SettingRoute
	executor driver.Executor
}

type syntheticSession struct {
	native     *syntheticNative
	input      *syntheticInput
	operations *syntheticOperations
	calendar   *syntheticCalendar
	publisher  syntheticPublisher
	registered []registration
	removed    int
}

func (s *syntheticSession) Native() driver.NativeRegistry             { return s.native }
func (s *syntheticSession) SemanticInput() driver.SemanticInput       { return s.input }
func (s *syntheticSession) ScopedOperations() driver.ScopedOperations { return s.operations }
func (s *syntheticSession) Calendar() driver.Calendar                 { return s.calendar }
func (s *syntheticSession) Publisher() driver.Publisher               { return s.publisher }

func (s *syntheticSession) Prepare(mutate func(*v1.PublicationBatch)) (v1.PublicationBatch, error) {
	var b v1.PublicationBatch
	if mutate != nil {
		mutate(&b)
	}
	return b, nil
}

func (s *syntheticSession) RegisterExecutor(r driver.SettingRoute, e driver.Executor) (func(), error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	s.registered = append(s.registered, registration{route: r, executor: e})
	return func() { s.removed++ }, nil
}

func newSession() *syntheticSession {
	return &syntheticSession{
		native: &syntheticNative{}, input: &syntheticInput{}, operations: &syntheticOperations{},
		calendar:  &syntheticCalendar{now: time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC)},
		publisher: syntheticPublisher{tx: &syntheticTransaction{}},
	}
}

type syntheticExecutor struct{ completion driver.Completion }

func (e syntheticExecutor) Execute(context.Context, driver.Dispatch) (driver.Completion, error) {
	return e.completion, nil
}

var (
	_ driver.Factory                = syntheticFactory{}
	_ driver.EarlyDispatcher        = (*earlyFactory)(nil)
	_ driver.Factory                = (*earlyFactory)(nil)
	_ driver.Instance               = (*syntheticInstance)(nil)
	_ driver.Session                = (*syntheticSession)(nil)
	_ driver.NativeRegistry         = (*syntheticNative)(nil)
	_ driver.SemanticInput          = (*syntheticInput)(nil)
	_ driver.ScopedOperations       = (*syntheticOperations)(nil)
	_ driver.Calendar               = (*syntheticCalendar)(nil)
	_ driver.CalendarAcquisition    = syntheticAcquisition{}
	_ driver.CalendarReference      = syntheticCalendarReference{}
	_ driver.Publisher              = syntheticPublisher{}
	_ driver.PublicationTransaction = (*syntheticTransaction)(nil)
	_ driver.Executor               = syntheticExecutor{}
)
