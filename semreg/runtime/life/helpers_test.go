package life

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

const testKind driver.DriverKind = "driver.test"

// testClock is a synthetic installation clock.
type testClock struct {
	mu   sync.Mutex
	now  time.Time
	mono uint64
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mono += uint64(d)
	c.mu.Unlock()
}

func (c *testClock) Sample() (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mono, nil
}

func (c *testClock) Anchor(sample uint64) ([]byte, error) { return []byte(fmt.Sprint(sample)), nil }

func (c *testClock) Elapsed(anchor []byte, sample uint64) (uint64, error) {
	var base uint64
	if _, err := fmt.Sscan(string(anchor), &base); err != nil {
		return 0, err
	}
	if sample < base {
		return 0, errors.New("backward sample")
	}
	return sample - base, nil
}

// claimSpec is the JSON form of one transport claim in a test configuration.
type claimSpec struct {
	Provider  string `json:"provider"`
	Config    string `json:"config"`
	Domain    string `json:"domain"`
	Exclusive bool   `json:"exclusive"`
	Slot      string `json:"slot,omitempty"`
}

func transportJSON(specs ...claimSpec) json.RawMessage {
	raw, _ := json.Marshal(specs)
	return raw
}

func (s claimSpec) claim() driver.TransportClaim {
	share := driver.ShareSerialized
	if s.Exclusive {
		share = driver.ShareExclusive
	}
	return driver.TransportClaim{Provider: s.Provider, ConfigKey: s.Config, AccessDomain: s.Domain, Share: share, LinkNodeSlot: s.Slot}
}

// startScript says how one synthetic instance behaves.
type startScript struct {
	// startErrs are returned, in order, by successive native starts.
	startErrs []error
	// stage keeps a publication transaction open across Start until release.
	stage chan struct{}
	// stopReport is what Stop reports; the zero value is a confirmed close.
	stopReport *driver.CloseReport
	// entered, when set, is closed once Start began; block, when set, holds Start
	// until it is closed, ignoring cancellation.
	entered chan struct{}
	block   chan struct{}
}

// testFactory is a synthetic factory of one driver kind.
type testFactory struct {
	log    *callLog
	kind   driver.DriverKind
	retry  driver.RetryPolicy
	mu     sync.Mutex
	plans  []driver.Config
	made   []*testInstance
	script map[driver.InstanceKey]*startScript
}

func newTestFactory(kind driver.DriverKind) *testFactory {
	return &testFactory{kind: kind, script: map[driver.InstanceKey]*startScript{}}
}

func (f *testFactory) Descriptor() driver.Descriptor { return driver.Descriptor{} }

func (f *testFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	f.mu.Lock()
	f.plans = append(f.plans, c)
	f.mu.Unlock()
	var specs []claimSpec
	if len(c.Transport) > 0 {
		if err := json.Unmarshal(c.Transport, &specs); err != nil {
			return nil, err
		}
	}
	plan := driver.InstancePlan{Driver: f.kind, Instance: driver.InstanceKey(c.Instance), RetryPolicy: f.retry}
	for _, s := range specs {
		plan.Transports = append(plan.Transports, s.claim())
	}
	return []driver.InstancePlan{plan}, nil
}

func (f *testFactory) Construct(_ context.Context, p driver.InstancePlan) (driver.Instance, error) {
	f.log.add("construct")
	f.mu.Lock()
	defer f.mu.Unlock()
	inst := &testInstance{plan: p, script: f.script[p.Instance], log: f.log}
	f.made = append(f.made, inst)
	return inst, nil
}

func (f *testFactory) constructed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.made)
}

func (f *testFactory) instance(i int) *testInstance {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.made[i]
}

func (f *testFactory) planCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.plans)
}

type testInstance struct {
	log    *callLog
	plan   driver.InstancePlan
	script *startScript

	mu      sync.Mutex
	starts  int
	stops   int
	fenced  bool
	session driver.Session
}

func (i *testInstance) Start(ctx context.Context, s driver.Session) error {
	i.log.add("instance-start")
	i.mu.Lock()
	i.starts++
	i.session = s
	i.mu.Unlock()
	if i.script != nil && i.script.entered != nil {
		close(i.script.entered)
		i.script.entered = nil
	}
	if i.script != nil && i.script.block != nil {
		<-i.script.block
	}
	if i.script != nil && len(i.script.startErrs) > 0 {
		err := i.script.startErrs[0]
		i.script.startErrs = i.script.startErrs[1:]
		if err != nil {
			return err
		}
	}
	tx, err := s.Publisher().Begin(ctx)
	if err != nil {
		return err
	}
	if i.script != nil && i.script.stage != nil {
		stage := i.script.stage
		go func() {
			<-stage
			_ = tx.Commit(context.Background())
		}()
		return nil
	}
	return tx.Commit(ctx)
}

func (i *testInstance) FenceWithdrawal() {
	i.mu.Lock()
	i.fenced = true
	i.mu.Unlock()
}

func (i *testInstance) Stop(context.Context) driver.CloseReport {
	i.mu.Lock()
	i.stops++
	i.mu.Unlock()
	if i.script != nil && i.script.stopReport != nil {
		return *i.script.stopReport
	}
	return driver.CloseReport{Confirmed: true}
}

func (i *testInstance) stopCount() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.stops
}

// retryableError is a start error the manager's retry series owns.
type retryableError struct{ msg string }

func (e *retryableError) Error() string   { return e.msg }
func (e *retryableError) Retryable() bool { return true }

// testSession is a synthetic session whose only live member is its publisher.
type testSession struct {
	driver.Session
	pub *testPublisher
}

func (s *testSession) Publisher() driver.Publisher { return s.pub }

type testPublisher struct {
	mu      sync.Mutex
	commits int
	aborts  int
}

func (p *testPublisher) Begin(context.Context) (driver.PublicationTransaction, error) {
	return &testTransaction{pub: p}, nil
}

type testTransaction struct {
	driver.PublicationTransaction
	pub *testPublisher
}

func (t *testTransaction) StageSample(driver.Sample) error { return nil }

func (t *testTransaction) StageLink(driver.LinkClaim) error { return nil }

func (t *testTransaction) Commit(context.Context) error {
	t.pub.mu.Lock()
	t.pub.commits++
	t.pub.mu.Unlock()
	return nil
}

func (t *testTransaction) Abort(context.Context) error {
	t.pub.mu.Lock()
	t.pub.aborts++
	t.pub.mu.Unlock()
	return nil
}

// testPublication is the synthetic lifecycle-to-publication handoff.
type testPublication struct {
	mu        sync.Mutex
	opened    []LifecycleCorrelation
	withdrawn []LifecycleCorrelation
	sessions  []*testPublisher
	// withdrawErr, when set, makes Withdraw fail; openErr makes OpenSession fail.
	withdrawErr error
	openErr     error
	log         *callLog
	// onWithdraw, when set, runs on every Withdraw before it returns.
	onWithdraw func(LifecycleCorrelation)
}

func (p *testPublication) OpenSession(_ context.Context, corr LifecycleCorrelation) (driver.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opened = append(p.opened, corr)
	if p.openErr != nil {
		return nil, p.openErr
	}
	pub := &testPublisher{}
	p.sessions = append(p.sessions, pub)
	p.log.add("open")
	return &testSession{pub: pub}, nil
}

func (p *testPublication) Withdraw(_ context.Context, corr LifecycleCorrelation) error {
	p.mu.Lock()
	hook := p.onWithdraw
	p.mu.Unlock()
	if hook != nil {
		hook(corr)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.withdrawn = append(p.withdrawn, corr)
	p.log.add("withdraw")
	return p.withdrawErr
}

func (p *testPublication) withdrawals() []LifecycleCorrelation {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]LifecycleCorrelation(nil), p.withdrawn...)
}

func (p *testPublication) openedSessions() []LifecycleCorrelation {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]LifecycleCorrelation(nil), p.opened...)
}

// callLog is an ordered log of the calls the synthetic collaborators receive.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(call string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.calls = append(l.calls, call)
	l.mu.Unlock()
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func (l *callLog) count(call string) int {
	n := 0
	for _, c := range l.snapshot() {
		if c == call {
			n++
		}
	}
	return n
}

// testRecovery is a synthetic recovery adapter.
type testRecovery struct {
	log *callLog

	mu           sync.Mutex
	restored     []*RecoveryMaterial
	restoreErr   error
	emptyErr     error
	secureErr    error
	secureReturn func(InstanceID, Transition) *RecoveryMaterial
	secured      []Transition
	hasEmpty     bool
}

func (r *testRecovery) Restore(_ context.Context, m *RecoveryMaterial) error {
	r.log.add("restore")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.restored = append(r.restored, m)
	return r.restoreErr
}

func (r *testRecovery) Secure(_ context.Context, id InstanceID, t Transition) (*RecoveryMaterial, error) {
	r.log.add("secure")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secured = append(r.secured, t)
	if r.secureErr != nil {
		return nil, r.secureErr
	}
	if r.secureReturn != nil {
		return r.secureReturn(id, t), nil
	}
	return nil, nil
}

type emptyRecovery struct{ *testRecovery }

func (r emptyRecovery) RestoreEmpty(context.Context) error {
	r.log.add("restore-empty")
	return r.emptyErr
}

func (r *testRecovery) securedTransitions() []Transition {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Transition(nil), r.secured...)
}

// rig assembles a supervisor over synthetic collaborators.
type rig struct {
	t        *testing.T
	log      *callLog
	clock    *testClock
	factory  *testFactory
	pub      *testPublication
	recovery *testRecovery
	set      PlanSet
	store    ControlStore
	sup      *Supervisor
	adapter  RecoveryAdapter
}

func instanceConfigs(factory *testFactory, enabled bool, transports map[string]claimSpec, keys ...string) []InstanceConfig {
	var out []InstanceConfig
	for _, key := range keys {
		cfg := InstanceConfig{Driver: factory.kind, Instance: driver.InstanceKey(key), Enabled: enabled}
		if spec, ok := transports[key]; ok {
			cfg.Config.Transport = transportJSON(spec)
		}
		out = append(out, cfg)
	}
	return out
}

func newRig(t *testing.T, store ControlStore, instances func(*testFactory) []InstanceConfig) *rig {
	t.Helper()
	log := &callLog{}
	factory := newTestFactory(testKind)
	factory.log = log
	r := &rig{t: t, log: log, clock: newTestClock(), factory: factory, pub: &testPublication{log: log}, recovery: &testRecovery{log: log}, store: store}
	set, err := Plan(Installation{Key: "installation.test", Bridge: "bridge.test", Clock: r.clock, Instances: instances(factory)}, factory)
	if err != nil {
		t.Fatal(err)
	}
	r.set = set
	return r
}

// build creates the supervisor; withEmpty selects an adapter that implements
// EmptyRestorer.
func (r *rig) build(incarnation string, withEmpty bool) {
	r.t.Helper()
	r.adapter = r.recovery
	if withEmpty {
		r.adapter = emptyRecovery{r.recovery}
	}
	sup, err := NewSupervisor(r.set, Options{Store: r.store, Recovery: r.adapter, Publication: r.pub, Incarnation: incarnation})
	if err != nil {
		r.t.Fatal(err)
	}
	r.sup = sup
}

func (r *rig) start() {
	r.t.Helper()
	if err := r.sup.Start(context.Background()); err != nil {
		r.t.Fatalf("start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.sup.WaitInitial(ctx); err != nil {
		r.t.Fatalf("wait initial: %v", err)
	}
}

func (r *rig) close() {
	if r.sup == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = r.sup.Close(ctx)
}

func ref(key string) Ref { return Ref{Driver: testKind, Instance: driver.InstanceKey(key)} }

func (r *rig) status(key string) Status {
	r.t.Helper()
	st, err := r.sup.Snapshot(ref(key))
	if err != nil {
		r.t.Fatal(err)
	}
	return st
}

func (r *rig) waitState(key string, state ObservedState) Status {
	r.t.Helper()
	waitFor(r.t, key+" "+string(state), func() bool { return r.status(key).ObservedState == state })
	return r.status(key)
}

var _ = v1.RelationNativeObservation
