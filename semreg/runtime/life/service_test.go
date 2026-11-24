package life

// Tests of the desired-state service over the durable control store. Every
// fixture is synthetic: generic instance ids (fam.*), fixed incarnations,
// injected lifecycle executors and recovery adapters, and a temporary state
// directory. Nothing here opens an endpoint.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// managerStore wires a FileStore to the Store seam under test.
type managerStore struct{ m *FileStore }

func (s *managerStore) Decide(ctx context.Context, method Method, req Request, planner Planner) (DesiredView, error) {
	return s.m.Decide(ctx, method, req, planner)
}

func (s *managerStore) CommitWork(ctx context.Context, id InstanceID, current, reserved *Transition) error {
	return s.m.CommitWork(ctx, id, current, reserved)
}

func (s *managerStore) AdvanceCatalog(ctx context.Context) (CatalogRevision, error) {
	return s.m.AdvanceCatalog(ctx)
}

func (s *managerStore) CommitRecovery(ctx context.Context, m *RecoveryMaterial) error {
	return s.m.CommitRecovery(ctx, m)
}
func (s *managerStore) Recovery() *RecoveryMaterial                 { return s.m.Recovery() }
func (s *managerStore) Catalog() (ControlCatalog, error)            { return s.m.Catalog() }
func (s *managerStore) Driver(id InstanceID) (ControlRecord, error) { return s.m.Driver(id) }
func (s *managerStore) AdmissionOpen() bool                         { return s.m.AdmissionOpen() }
func (s *managerStore) ResolveReadback(ctx context.Context) error {
	return s.m.ResolveReadback(ctx)
}

// Stop ends the store's use and relinquishes its lock.
func (s *FileStore) Stop(context.Context) error {
	s.Close()
	s.Release()
	return nil
}

// ParseRequest decodes the two-member request body the tests build.
func ParseRequest(data []byte) (Request, error) {
	var in struct {
		InstanceID       string `json:"InstanceID"`
		ExpectedRevision string `json:"ExpectedRevision"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return Request{}, &Refusal{Code: RefusalInvalidArgument, Row: 1, Reason: err.Error()}
	}
	rev, ok := parseCounter(in.ExpectedRevision, false)
	if !ok {
		return Request{}, &Refusal{Code: RefusalInvalidArgument, Row: 1, Reason: "ExpectedRevision must be a canonical nonzero decimal string"}
	}
	return Request{InstanceID: InstanceID(in.InstanceID), ExpectedRevision: DesiredRevision(rev)}, nil
}

// Control decodes a request body and decides it.
func (s *Service) Control(ctx context.Context, method Method, body []byte) (DesiredView, error) {
	if !validMethod(method) {
		return DesiredView{}, &Refusal{Code: RefusalInvalidArgument, Row: 1, Reason: "unknown method"}
	}
	req, err := ParseRequest(body)
	if err != nil {
		return DesiredView{}, err
	}
	return s.Decide(ctx, method, req)
}

// --- filesystem hooks with an injectable block and failures ---

type blockHooks struct {
	osHooks
	mu         sync.Mutex
	blockNext  chan struct{}
	entered    chan struct{}
	failRename error
	// failDirSync makes the next N parent-directory fsyncs fail.
	failDirSync int
	writes      int
	// afterRename runs after each successful rename, with the renamed path.
	afterRename func(path string)
}

func (h *blockHooks) FsyncDir(path string) error {
	h.mu.Lock()
	fail := h.failDirSync > 0
	if fail {
		h.failDirSync--
	}
	h.mu.Unlock()
	if fail {
		return errors.New("synthetic directory fsync failure")
	}
	// The data reaches the file system in this process either way, and a
	// device flush is what these fixtures spend their time on; the tests of
	// the flush itself go through OpenFileStore, which keeps it.
	return nil
}

func (h *blockHooks) FsyncFile(string) error { return nil }

func (h *blockHooks) WriteFile(path string, data []byte, perm uint32) error {
	h.mu.Lock()
	block, entered := h.blockNext, h.entered
	h.blockNext, h.entered = nil, nil
	h.writes++
	h.mu.Unlock()
	if block != nil {
		close(entered)
		<-block
	}
	return h.osHooks.WriteFile(path, data, perm)
}

func (h *blockHooks) Rename(oldpath, newpath string) error {
	h.mu.Lock()
	fail := h.failRename
	after := h.afterRename
	h.mu.Unlock()
	if fail != nil {
		return fail
	}
	err := h.osHooks.Rename(oldpath, newpath)
	if err == nil && after != nil {
		after(newpath)
	}
	return err
}

func (h *blockHooks) arm() (entered, release chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.blockNext, h.entered = make(chan struct{}), make(chan struct{})
	return h.entered, h.blockNext
}

// --- fakes ---

type orderLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *orderLog) add(entry string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.entries = append(l.entries, entry)
	l.mu.Unlock()
}

func (l *orderLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

type execCall struct {
	Op   string
	Corr LifecycleCorrelation
}

type fakeExec struct {
	id    InstanceID
	log   *orderLog
	mu    sync.Mutex
	calls []execCall
	start func(ctx context.Context, c LifecycleCorrelation) error
	stop  func(ctx context.Context, c LifecycleCorrelation) StopOutcome
}

func (f *fakeExec) Start(ctx context.Context, c LifecycleCorrelation) error {
	f.mu.Lock()
	f.calls = append(f.calls, execCall{"start", c})
	fn := f.start
	f.mu.Unlock()
	f.log.add("start:" + string(f.id))
	if fn != nil {
		return fn(ctx, c)
	}
	return nil
}

func (f *fakeExec) Stop(ctx context.Context, c LifecycleCorrelation) StopOutcome {
	f.mu.Lock()
	f.calls = append(f.calls, execCall{"stop", c})
	fn := f.stop
	f.mu.Unlock()
	f.log.add("stop:" + string(f.id))
	if fn != nil {
		return fn(ctx, c)
	}
	return StopOutcome{NativeClosed: true, SemanticCleaned: true}
}

func (f *fakeExec) setStart(fn func(context.Context, LifecycleCorrelation) error) {
	f.mu.Lock()
	f.start = fn
	f.mu.Unlock()
}

func (f *fakeExec) setStop(fn func(context.Context, LifecycleCorrelation) StopOutcome) {
	f.mu.Lock()
	f.stop = fn
	f.mu.Unlock()
}

func (f *fakeExec) snapshot() []execCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]execCall(nil), f.calls...)
}

func (f *fakeExec) count(op string) int {
	n := 0
	for _, c := range f.snapshot() {
		if c.Op == op {
			n++
		}
	}
	return n
}

type retryErr struct{ msg string }

func (e retryErr) Error() string   { return e.msg }
func (e retryErr) Retryable() bool { return true }

type fakeRecovery struct {
	log        *orderLog
	mu         sync.Mutex
	restoreErr error
	restored   []*RecoveryMaterial
	secure     func(id InstanceID, t Transition) (*RecoveryMaterial, error)
	secured    []Transition
}

func (r *fakeRecovery) Restore(_ context.Context, m *RecoveryMaterial) error {
	r.log.add("restore")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.restored = append(r.restored, cloneMaterial(m))
	return r.restoreErr
}

func (r *fakeRecovery) Secure(_ context.Context, id InstanceID, t Transition) (*RecoveryMaterial, error) {
	r.log.add("secure:" + string(id))
	r.mu.Lock()
	fn := r.secure
	r.secured = append(r.secured, t)
	r.mu.Unlock()
	if fn != nil {
		return fn(id, t)
	}
	return nil, nil
}

func (r *fakeRecovery) securedFor(id InstanceID) []Transition {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Transition(nil), r.secured...)
}

type errSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *errSink) add(err error) {
	s.mu.Lock()
	s.errs = append(s.errs, err)
	s.mu.Unlock()
}

func (s *errSink) list() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.errs...)
}

// --- fixture ---

type svcFixture struct {
	t     *testing.T
	path  string
	hooks *blockHooks
	m     *FileStore
	ms    *managerStore
	store Store
	svc   *Service
	execs map[InstanceID]*fakeExec
	rec   *fakeRecovery
	errs  *errSink
	log   *orderLog
}

type svcOpts struct {
	drivers     []ControlInstance
	wrap        func(Store) Store
	opts        func(*ServiceOptions)
	state       func(t *testing.T, path string)
	incarnation string
	noStart     bool
	recovery    *fakeRecovery
	adapter     RecoveryAdapter
	setup       func(*svcFixture)
}

// openManager opens the durable store at the state file path and registers the
// configured instances.
func openManager(t *testing.T, path string, hooks *blockHooks, drivers []ControlInstance) *FileStore {
	t.Helper()
	m, err := openFileStore(filepath.Dir(path), hooks, acquireStoreLock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Register(context.Background(), drivers); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return m
}

func newFixture(t *testing.T, fo svcOpts) *svcFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), controlFileName)
	if fo.state != nil {
		fo.state(t, path)
	}
	hooks := &blockHooks{}
	m := openManager(t, path, hooks, fo.drivers)
	f := &svcFixture{t: t, path: path, hooks: hooks, m: m, ms: &managerStore{m: m}, execs: map[InstanceID]*fakeExec{},
		errs: &errSink{}, log: &orderLog{}}
	f.store = f.ms
	if fo.wrap != nil {
		f.store = fo.wrap(f.ms)
	}
	f.rec = fo.recovery
	if f.rec == nil {
		f.rec = &fakeRecovery{}
	}
	f.rec.log = f.log
	var cfgs []ServiceInstance
	for _, d := range fo.drivers {
		e := &fakeExec{id: d.InstanceID, log: f.log}
		f.execs[d.InstanceID] = e
		cfgs = append(cfgs, ServiceInstance{InstanceID: d.InstanceID, Executor: e})
	}
	inc := fo.incarnation
	if inc == "" {
		inc = "inc1"
	}
	var adapter RecoveryAdapter = f.rec
	if fo.adapter != nil {
		adapter = fo.adapter
	}
	opts := ServiceOptions{Store: f.store, Recovery: adapter, Drivers: cfgs, Incarnation: inc,
		RetryDelay: func(int) time.Duration { return 2 * time.Millisecond }, OnError: f.errs.add}
	if fo.opts != nil {
		fo.opts(&opts)
	}
	svc, err := NewService(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.svc = svc
	t.Cleanup(func() {
		f.svc.Close()
		_ = f.m.Stop(context.Background())
	})
	if fo.setup != nil {
		fo.setup(f)
	}
	if !fo.noStart {
		if err := svc.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
	}
	return f
}

func enabled(ids ...string) []ControlInstance {
	out := make([]ControlInstance, 0, len(ids))
	for _, id := range ids {
		out = append(out, ControlInstance{InstanceID: InstanceID(id), Enabled: true})
	}
	return out
}

func deadlineCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *svcFixture) status(id InstanceID) DriverStatus {
	f.t.Helper()
	st, err := f.svc.Get(id)
	if err != nil {
		f.t.Fatalf("Get(%s): %v", id, err)
	}
	return st
}

func (f *svcFixture) record(id InstanceID) ControlRecord {
	f.t.Helper()
	rec, err := f.ms.Driver(id)
	if err != nil {
		f.t.Fatalf("Driver(%s): %v", id, err)
	}
	return rec
}

func (f *svcFixture) waitObserved(id InstanceID, state ObservedState) {
	f.t.Helper()
	waitFor(f.t, fmt.Sprintf("%s observed %s", id, state), func() bool { return f.status(id).ObservedState == state })
}

func (f *svcFixture) waitIdle(id InstanceID) {
	f.t.Helper()
	waitFor(f.t, fmt.Sprintf("%s slots empty", id), func() bool {
		rec := f.record(id)
		return rec.Current == nil && rec.Reserved == nil
	})
}

func (f *svcFixture) waitSettled(id InstanceID, state ObservedState) {
	f.t.Helper()
	f.waitIdle(id)
	f.waitObserved(id, state)
}

func body(id InstanceID, rev DesiredRevision) []byte {
	return []byte(fmt.Sprintf(`{"InstanceID":%q,"ExpectedRevision":"%d"}`, id, rev))
}

func (f *svcFixture) decide(method Method, id InstanceID) (DesiredView, error) {
	f.t.Helper()
	return f.svc.Control(deadlineCtx(f.t), method, body(id, f.status(id).DesiredRevision))
}

func (f *svcFixture) mustDecide(method Method, id InstanceID) DesiredView {
	f.t.Helper()
	view, err := f.decide(method, id)
	if err != nil {
		f.t.Fatalf("%s %s: %v", method, id, err)
	}
	return view
}

// quiesce waits until no store write happened for a short interval, so an
// armed filesystem block catches the next deliberate write.
func (f *svcFixture) quiesce() {
	f.t.Helper()
	count := func() int {
		f.hooks.mu.Lock()
		defer f.hooks.mu.Unlock()
		return f.hooks.writes
	}
	waitFor(f.t, "store writes to quiesce", func() bool {
		before := count()
		time.Sleep(50 * time.Millisecond)
		return count() == before
	})
}

func (f *svcFixture) fileBytes() []byte {
	f.t.Helper()
	data, err := os.ReadFile(f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	return data
}

// fileDriver decodes one instance record straight from the state file.
func (f *svcFixture) fileDriver(id InstanceID) map[string]any {
	f.t.Helper()
	var doc struct {
		Instances map[string]map[string]any `json:"instances"`
	}
	if err := json.Unmarshal(f.fileBytes(), &doc); err != nil {
		f.t.Fatal(err)
	}
	return doc.Instances[string(id)]
}

func (f *svcFixture) fileDesired(id InstanceID) string {
	state, _ := f.fileDriver(id)["desired_state"].(string)
	return state
}

func (f *svcFixture) fileCurrentKind(id InstanceID) string {
	current, _ := f.fileDriver(id)["current"].(map[string]any)
	kind, _ := current["kind"].(string)
	return kind
}

// refusalOf asserts the refusal code and row and returns the refusal as a
// value (the value type is not an error, so callers may ignore it).
func refusalOf(t *testing.T, err error, code RefusalCode, row int) Refusal {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("error = %v, want refusal %s", err, code)
	}
	if r.Code != code || r.Row != row {
		t.Fatalf("refusal = %s row %d (%s), want %s row %d", r.Code, r.Row, r.Reason, code, row)
	}
	return *r
}

// blockingStart returns a start function that blocks until release is
// closed or the attempt context ends, reporting entry on entered.
func blockingStart(entered chan<- LifecycleCorrelation, release <-chan struct{}, lateResult error) func(context.Context, LifecycleCorrelation) error {
	return func(ctx context.Context, c LifecycleCorrelation) error {
		entered <- c
		select {
		case <-release:
			return lateResult
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func TestServiceConfigValidationRejectsWithoutEffects(t *testing.T) {
	t.Parallel()
	f := newFixture(t, svcOpts{drivers: enabled("fam.alpha", "fam.beta"), noStart: true})
	before := f.fileBytes()
	exec := &fakeExec{id: "fam.alpha"}
	var nilExec *fakeExec
	var nilStore *managerStore
	good := func() ServiceOptions {
		return ServiceOptions{Store: f.store, Recovery: f.rec, Incarnation: "inc1",
			Drivers: []ServiceInstance{{InstanceID: "fam.alpha", Executor: exec}}}
	}
	cases := map[string]func(*ServiceOptions){
		"nil store":          func(o *ServiceOptions) { o.Store = nil },
		"typed nil store":    func(o *ServiceOptions) { o.Store = nilStore },
		"nil recovery":       func(o *ServiceOptions) { o.Recovery = nil },
		"empty incarnation":  func(o *ServiceOptions) { o.Incarnation = "" },
		"spaced incarnation": func(o *ServiceOptions) { o.Incarnation = "inc 1" },
		"long incarnation":   func(o *ServiceOptions) { o.Incarnation = strings.Repeat("a", maxIncarnationBytes+1) },
		"empty id":           func(o *ServiceOptions) { o.Drivers[0].InstanceID = "" },
		"duplicate id":       func(o *ServiceOptions) { o.Drivers = append(o.Drivers, o.Drivers[0]) },
		"nil executor":       func(o *ServiceOptions) { o.Drivers[0].Executor = nil },
		"typed nil executor": func(o *ServiceOptions) { o.Drivers[0].Executor = nilExec },
		"attempts above 8":   func(o *ServiceOptions) { o.MaxAttempts = DefaultMaxAttempts + 1 },
		"negative budget":    func(o *ServiceOptions) { o.OperationBudget = -time.Second },
		"budget above 30s":   func(o *ServiceOptions) { o.OperationBudget = DefaultOperationBudget + time.Nanosecond },
		"negative max":       func(o *ServiceOptions) { o.MaxAttempts = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			opts := good()
			mutate(&opts)
			if _, err := NewService(opts); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New error = %v, want ErrInvalidConfig", err)
			}
		})
	}
	// A configured set that differs from the store's visible drivers.
	svc, err := NewService(good())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(context.Background()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Start error = %v, want ErrInvalidConfig", err)
	}
	svc.Close()
	if len(exec.snapshot()) != 0 || len(f.rec.securedFor("")) != 0 || len(f.log.list()) != 0 {
		t.Fatalf("configuration refusal invoked an executor or recovery adapter: %v", f.log.list())
	}
	if after := f.fileBytes(); string(after) != string(before) {
		t.Fatalf("configuration refusal changed durable state")
	}
	if _, err := svc.Decide(deadlineCtx(t), MethodStop, Request{InstanceID: "fam.alpha", ExpectedRevision: 1}); err == nil {
		t.Fatalf("a closed service accepted a decision")
	}
}

type stateProbe struct {
	file   string
	view   Catalog
	cursor uint64
	calls  int
}

func (f *svcFixture) probe() stateProbe {
	calls := 0
	for _, e := range f.execs {
		calls += len(e.snapshot())
	}
	return stateProbe{file: string(f.fileBytes()), view: f.svc.List(), cursor: f.svc.events.cursor(), calls: calls}
}

func (f *svcFixture) assertUnchanged(what string, before stateProbe) {
	f.t.Helper()
	after := f.probe()
	if after.file != before.file {
		f.t.Fatalf("%s changed durable state", what)
	}
	if !reflect.DeepEqual(after.view, before.view) {
		f.t.Fatalf("%s changed the view: %+v -> %+v", what, before.view, after.view)
	}
	if after.cursor != before.cursor {
		f.t.Fatalf("%s emitted an event", what)
	}
	if after.calls != before.calls {
		f.t.Fatalf("%s invoked an executor", what)
	}
}

// TestServiceRefusalsLeaveStateUnchanged drives one request per refusal the
// service can reach (invalid ID, malformed request, disabled, deadline,
// stale revision, RESTART in STOPPED) and requires no durable, view, event
// or executor change.
func TestServiceRefusalsLeaveStateUnchanged(t *testing.T) {
	t.Parallel()
	drivers := []ControlInstance{
		{InstanceID: "fam.alpha", Enabled: true}, {InstanceID: "fam.beta", Enabled: false}, {InstanceID: "fam.gamma", Enabled: true},
	}
	f := newFixture(t, svcOpts{drivers: drivers})
	f.waitSettled("fam.alpha", ObservedRunning)
	f.waitSettled("fam.gamma", ObservedRunning)
	f.mustDecide(MethodStop, "fam.gamma")
	f.waitSettled("fam.gamma", ObservedStopped)
	if st := f.status("fam.beta"); st.ObservedState != ObservedDisabled || f.execs["fam.beta"].count("start") != 0 {
		t.Fatalf("disabled driver = %+v", st)
	}
	alpha := f.status("fam.alpha")
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	cases := []struct {
		name   string
		call   func() error
		code   RefusalCode
		row    int
		verify func(Refusal)
	}{
		{"malformed body", func() error {
			_, err := f.svc.Control(deadlineCtx(t), MethodStop, []byte(`{"InstanceID":"fam.alpha","ExpectedRevision":"01"}`))
			return err
		}, RefusalInvalidArgument, 1, nil},
		{"unknown method", func() error {
			_, err := f.svc.Control(deadlineCtx(t), Method("PAUSE"), body("fam.alpha", alpha.DesiredRevision))
			return err
		}, RefusalInvalidArgument, 1, nil},
		{"unregistered id", func() error {
			_, err := f.svc.Control(deadlineCtx(t), MethodStart, body("fam.delta", alpha.DesiredRevision))
			return err
		}, RefusalNotFound, 2, nil},
		{"noncanonical id", func() error {
			_, err := f.svc.Control(deadlineCtx(t), MethodStart, body("Fam.Alpha", alpha.DesiredRevision))
			return err
		}, RefusalNotFound, 2, nil},
		{"disabled", func() error {
			_, err := f.svc.Control(deadlineCtx(t), MethodStart, body("fam.beta", f.status("fam.beta").DesiredRevision))
			return err
		}, RefusalFailedPrecondition, 4, nil},
		{"expired deadline", func() error {
			_, err := f.svc.Control(expired, MethodStop, body("fam.alpha", alpha.DesiredRevision))
			return err
		}, RefusalDeadlineExceeded, 5, nil},
		{"stale changing", func() error {
			_, err := f.svc.Control(deadlineCtx(t), MethodStart, body("fam.gamma", f.status("fam.gamma").DesiredRevision-1))
			return err
		}, RefusalRevisionConflict, 6, func(r Refusal) {
			gamma := f.status("fam.gamma")
			if r.View == nil || *r.View != (DesiredView{DesiredStopped, gamma.DesiredRevision}) {
				t.Fatalf("conflict view = %+v", r.View)
			}
		}},
		{"stale same-desired", func() error {
			_, err := f.svc.Control(deadlineCtx(t), MethodStart, body("fam.alpha", alpha.DesiredRevision+1))
			return err
		}, RefusalRevisionConflict, 6, nil},
		{"restart while stopped", func() error {
			_, err := f.svc.Control(deadlineCtx(t), MethodRestart, body("fam.gamma", f.status("fam.gamma").DesiredRevision))
			return err
		}, RefusalFailedPrecondition, 6, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := f.probe()
			r := refusalOf(t, tc.call(), tc.code, tc.row)
			if tc.verify != nil {
				tc.verify(r)
			}
			f.assertUnchanged(tc.name, before)
		})
	}
}

// TestServiceCounterOverflowRejectsWithoutStateChange stores the mark at the
// unsigned 64-bit maximum: a changing START refuses COUNTER_EXHAUSTED with no
// durable, view or event change.
func TestServiceCounterOverflowRejectsWithoutStateChange(t *testing.T) {
	t.Parallel()
	maxRev := strconv.FormatUint(math.MaxUint64, 10)
	f := newFixture(t, svcOpts{drivers: enabled("fam.alpha"), state: func(t *testing.T, path string) {
		body := fmt.Sprintf(`{"schema_version":1,"issued_revision_mark":%q,"catalog_revision":"5","instances":{`+
			`"fam.alpha":{"desired_state":"STOPPED","desired_revision":%q,"lifecycle_sequence":"0","current":null,"reserved":null}}}`,
			maxRev, maxRev)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}})
	f.waitObserved("fam.alpha", ObservedStopped)
	before := f.probe()
	_, err := f.svc.Control(deadlineCtx(t), MethodStart, body("fam.alpha", math.MaxUint64))
	refusalOf(t, err, RefusalCounterExhausted, 10)
	f.assertUnchanged("overflow", before)
	// A same-desired STOP still answers as a no-op without any counter.
	view, err := f.svc.Control(deadlineCtx(t), MethodStop, body("fam.alpha", math.MaxUint64))
	if err != nil || view != (DesiredView{DesiredStopped, math.MaxUint64}) {
		t.Fatalf("no-op at the maximum = %+v, %v", view, err)
	}
	f.assertUnchanged("no-op at the maximum", before)
}

// --- same-desired no-ops ---

// TestServiceSameDesiredNoOpsKeepRetryAndQuarantine: START while desired
// RUNNING in BACKOFF (untyped and typed) and FAILED, and STOP while desired
// STOPPED with an unconfirmed close, return desired/revision only and change
// nothing: no revision, mark, slot, budget, observation, event or wake.
func TestServiceSameDesiredNoOpsKeepRetryAndQuarantine(t *testing.T) {
	t.Parallel()
	f := newFixture(t, svcOpts{
		drivers: enabled("fam.backoff", "fam.failed", "fam.quar", "fam.typed"),
		opts:    func(o *ServiceOptions) { o.RetryDelay = func(int) time.Duration { return time.Hour } },
		setup: func(f *svcFixture) {
			f.execs["fam.backoff"].setStart(func(context.Context, LifecycleCorrelation) error { return retryErr{"busy"} })
		},
	})
	f.waitSettled("fam.typed", ObservedRunning)
	f.waitSettled("fam.failed", ObservedRunning)
	f.waitSettled("fam.quar", ObservedRunning)
	// Typed BACKOFF: STOP, then a START that keeps failing retryably.
	f.mustDecide(MethodStop, "fam.typed")
	f.waitSettled("fam.typed", ObservedStopped)
	f.execs["fam.typed"].setStart(func(context.Context, LifecycleCorrelation) error { return retryErr{"busy"} })
	f.mustDecide(MethodStart, "fam.typed")
	f.waitObserved("fam.typed", ObservedBackoff)
	// FAILED: a terminal start failure after STOP/START.
	f.mustDecide(MethodStop, "fam.failed")
	f.waitSettled("fam.failed", ObservedStopped)
	f.execs["fam.failed"].setStart(func(context.Context, LifecycleCorrelation) error { return errors.New("terminal") })
	f.mustDecide(MethodStart, "fam.failed")
	f.waitSettled("fam.failed", ObservedFailed)
	// Quarantine: STOP whose native close cannot be proven.
	f.execs["fam.quar"].setStop(func(context.Context, LifecycleCorrelation) StopOutcome { return StopOutcome{SemanticCleaned: true} })
	f.mustDecide(MethodStop, "fam.quar")
	waitFor(t, "quarantine", func() bool { return f.status("fam.quar").SafetyQuarantined })
	// Untyped BACKOFF: the seeded driver's process-start attempt fails.
	f.waitObserved("fam.backoff", ObservedBackoff)
	if rec := f.record("fam.backoff"); rec.Current != nil {
		t.Fatalf("untyped attempt persisted typed work: %+v", rec.Current)
	}

	typedSlot := f.record("fam.typed").Current
	if typedSlot == nil || typedSlot.AttemptsUsed != 1 || typedSlot.Kind != KindStart {
		t.Fatalf("typed backoff slot = %+v", typedSlot)
	}
	before := f.probe()
	catalogBefore, _ := f.ms.Catalog()
	for _, tc := range []struct {
		id     InstanceID
		method Method
		state  DesiredState
	}{
		{"fam.backoff", MethodStart, DesiredRunning},
		{"fam.typed", MethodStart, DesiredRunning},
		{"fam.failed", MethodStart, DesiredRunning},
		{"fam.quar", MethodStop, DesiredStopped},
	} {
		rev := f.status(tc.id).DesiredRevision
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				view, err := f.svc.Control(deadlineCtx(t), tc.method, body(tc.id, rev))
				if err != nil || view != (DesiredView{tc.state, rev}) {
					t.Errorf("%s %s no-op = %+v, %v", tc.method, tc.id, view, err)
				}
			}()
		}
		wg.Wait()
	}
	f.assertUnchanged("same-desired no-ops", before)
	catalogAfter, _ := f.ms.Catalog()
	if catalogAfter.IssuedMark != catalogBefore.IssuedMark || catalogAfter.CatalogRevision != catalogBefore.CatalogRevision {
		t.Fatalf("no-op moved the mark or catalog: %+v -> %+v", catalogBefore, catalogAfter)
	}
	if slot := f.record("fam.typed").Current; !reflect.DeepEqual(slot, typedSlot) {
		t.Fatalf("no-op changed the retry budget: %+v -> %+v", typedSlot, slot)
	}
	// Quarantine blocks admission, never cleared by a request.
	_, err := f.decide(MethodStart, "fam.quar")
	refusalOf(t, err, RefusalFailedPrecondition, 7)
	_, err = f.decide(MethodRestart, "fam.quar")
	refusalOf(t, err, RefusalFailedPrecondition, 6)
	if st := f.status("fam.quar"); !st.SafetyQuarantined || st.Reason != ReasonCloseUnconfirmed {
		t.Fatalf("quarantine cleared: %+v", st)
	}
	if slot := f.record("fam.quar").Current; slot == nil || slot.Kind != KindStop || !slot.NativeClosePending {
		t.Fatalf("unconfirmed close did not retain its cleanup obligation: %+v", slot)
	}
}

// --- acceptance durability and ordering ---

type gateStore struct {
	Store
	mu      sync.Mutex
	gate    bool
	reached chan struct{}
	release chan struct{}
	ops     *orderLog
}

func (g *gateStore) Decide(ctx context.Context, m Method, r Request, p Planner) (DesiredView, error) {
	view, err := g.Store.Decide(ctx, m, r, p)
	g.mu.Lock()
	gate, reached, release := g.gate, g.reached, g.release
	g.gate = false
	g.mu.Unlock()
	if gate {
		close(reached)
		<-release
	}
	return view, err
}

func (g *gateStore) CommitWork(ctx context.Context, id InstanceID, cur, res *Transition) error {
	err := g.Store.CommitWork(ctx, id, cur, res)
	if err == nil && cur == nil && res == nil {
		g.ops.add("prune:" + string(id))
	}
	return err
}

func (g *gateStore) CommitRecovery(ctx context.Context, m *RecoveryMaterial) error {
	err := g.Store.CommitRecovery(ctx, m)
	if err == nil {
		g.ops.add("recovery")
	}
	return err
}

func (g *gateStore) arm() (reached, release chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gate, g.reached, g.release = true, make(chan struct{}), make(chan struct{})
	return g.reached, g.release
}

// TestServiceAcceptanceIsDurableBeforePublishWakeAndEvent holds the service
// right after the store's durable commit: the decision and its typed slot are
// already on disk while the view, the event log and the reconciler are still
// untouched. Releasing it publishes, emits and wakes, and the response is the
// committed desired state and revision, incremented exactly once.
func TestServiceAcceptanceIsDurableBeforePublishWakeAndEvent(t *testing.T) {
	t.Parallel()
	gate := &gateStore{ops: &orderLog{}}
	f := newFixture(t, svcOpts{drivers: enabled("fam.alpha"), wrap: func(s Store) Store { gate.Store = s; return gate }})
	f.waitSettled("fam.alpha", ObservedRunning)
	f.mustDecide(MethodStop, "fam.alpha")
	f.waitSettled("fam.alpha", ObservedStopped)
	before := f.status("fam.alpha")
	markBefore, _ := f.ms.Catalog()
	starts := f.execs["fam.alpha"].count("start")
	cursor := f.svc.events.cursor()

	reached, release := gate.arm()
	type result struct {
		view DesiredView
		err  error
	}
	done := make(chan result, 1)
	go func() {
		view, err := f.svc.Control(deadlineCtx(t), MethodStart, body("fam.alpha", before.DesiredRevision))
		done <- result{view, err}
	}()
	<-reached
	rec := f.record("fam.alpha")
	if rec.DesiredState != DesiredRunning || rec.DesiredRevision != markBefore.IssuedMark+1 ||
		rec.Current == nil || rec.Current.Kind != KindStart || rec.Current.Phase != PhasePending {
		t.Fatalf("decision not durable before notification: %+v", rec)
	}
	if got := f.fileDesired("fam.alpha"); got != "RUNNING" {
		t.Fatalf("committed decision missing from the state file: %q", got)
	}
	time.Sleep(20 * time.Millisecond)
	if st := f.status("fam.alpha"); st != before {
		t.Fatalf("view exposed before the service resumed: %+v", st)
	}
	if f.svc.events.cursor() != cursor || f.execs["fam.alpha"].count("start") != starts {
		t.Fatalf("event or wake before the service resumed")
	}
	close(release)
	res := <-done
	if res.err != nil || res.view != (DesiredView{DesiredRunning, markBefore.IssuedMark + 1}) {
		t.Fatalf("accepted response = %+v, %v", res.view, res.err)
	}
	page := f.svc.EventsSince(cursor)
	if len(page.Events) == 0 || page.Events[0].Kind != EventDesiredAccepted || page.Events[0].DesiredRevision != res.view.DesiredRevision {
		t.Fatalf("first event after acceptance = %+v", page.Events)
	}
	f.waitSettled("fam.alpha", ObservedRunning)
	if got := f.execs["fam.alpha"].count("start"); got != starts+1 {
		t.Fatalf("accepted START executed %d times, want 1", got-starts)
	}
}

// TestServiceObservedTransitionsNeverBumpDesiredRevision: every observed
// transition advances only the catalog revision; desired revision and the
// mark keep the accepted value.
func TestServiceObservedTransitionsNeverBumpDesiredRevision(t *testing.T) {
	t.Parallel()
	// The deadline only diagnoses a hung test, so it is generous for a loaded
	// race run; all progress is ordered by signals, never by elapsed time.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	await := func(signal <-chan struct{}, what string) {
		t.Helper()
		select {
		case <-signal:
		case <-ctx.Done():
			t.Fatalf("hung waiting for %s: %v", what, ctx.Err())
		}
	}
	f := newFixture(t, svcOpts{drivers: enabled("fam.alpha")})
	// Register before reading the view so publication cannot lose a wakeup.
	waitPublished := func(state ObservedState) {
		t.Helper()
		for {
			notify := f.svc.EventNotify()
			if f.status("fam.alpha").ObservedState == state {
				return
			}
			await(notify, string(state))
		}
	}
	waitPublished(ObservedRunning)
	stop := f.mustDecide(MethodStop, "fam.alpha")
	waitPublished(ObservedStopped)

	var began, release [3]chan struct{}
	for i := range began {
		began[i], release[i] = make(chan struct{}), make(chan struct{})
	}
	succeeded := make(chan struct{})
	attempts := 0 // Only the serial executor callback accesses this counter.
	f.execs["fam.alpha"].setStart(func(execCtx context.Context, _ LifecycleCorrelation) error {
		i := attempts
		attempts++
		if i >= len(began) {
			return errors.New("unexpected extra start attempt")
		}
		close(began[i])
		select {
		case <-release[i]:
		case <-execCtx.Done():
			return execCtx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
		if i < len(began)-1 {
			return retryErr{"warming"}
		}
		close(succeeded)
		return nil
	})
	// Close cancels blocked callbacks and joins the reconciler even on Fatal.
	defer f.svc.Close()
	// STOPPED may be published before the terminal work slot is pruned. Only
	// TRANSITION_BUSY is retried, and the hang-detection deadline bounds it.
	var accepted DesiredView
	for {
		var err error
		accepted, err = f.svc.Control(ctx, MethodStart, body("fam.alpha", stop.DesiredRevision))
		if err == nil {
			break
		}
		var refusal *Refusal
		if !errors.As(err, &refusal) || refusal.Code != RefusalTransitionBusy {
			t.Fatalf("START after STOPPED: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("terminal pruning did not finish: %v", ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if accepted.DesiredRevision != stop.DesiredRevision+1 {
		t.Fatalf("acceptance incremented the revision by %d", accepted.DesiredRevision-stop.DesiredRevision)
	}
	await(began[0], "first retry-sequence attempt")
	baseline := func() (uint64, ControlCatalog) {
		t.Helper()
		select {
		case <-began[0]:
		default:
			t.Fatal("baseline read before first attempt signal")
		}
		select {
		case <-release[0]:
			t.Fatal("baseline read after retry sequence released")
		default:
		}
		// The executor is blocked: neither events nor catalog can advance.
		cursor := f.svc.events.cursor()
		catalog, err := f.ms.Catalog()
		if err != nil {
			t.Fatal(err)
		}
		return cursor, catalog
	}
	cursor, catalogAtBaseline := baseline()
	for i := range began {
		await(began[i], fmt.Sprintf("attempt %d", i+1))
		close(release[i])
	}
	await(succeeded, "final executor success")
	waitPublished(ObservedRunning)
	f.svc.Close() // No publisher remains while the final snapshots are compared.
	catalog, err := f.ms.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.IssuedMark != accepted.DesiredRevision || f.record("fam.alpha").DesiredRevision != accepted.DesiredRevision {
		t.Fatalf("observed transitions moved desired revision or mark: %+v", catalog)
	}
	observedEvents := func() EventPage {
		t.Helper()
		select {
		case <-succeeded:
		default:
			t.Fatal("events counted before retry sequence success signal")
		}
		if f.status("fam.alpha").ObservedState != ObservedRunning {
			t.Fatal("events counted before RUNNING publication")
		}
		return f.svc.EventsSince(cursor)
	}
	observed := 0
	for _, ev := range observedEvents().Events {
		if ev.Kind == EventObservedChanged {
			observed++
			if ev.DesiredRevision != accepted.DesiredRevision {
				t.Fatalf("observed event carries desired revision %d", ev.DesiredRevision)
			}
		}
	}
	if observed < 4 || catalog.CatalogRevision < catalogAtBaseline.CatalogRevision+CatalogRevision(observed) {
		t.Fatalf("observed=%d catalog %d -> %d", observed, catalogAtBaseline.CatalogRevision, catalog.CatalogRevision)
	}
	if got := f.status("fam.alpha"); got.Revision != f.svc.List().Revision || got.DesiredRevision != accepted.DesiredRevision {
		t.Fatalf("view = %+v", got)
	}
}

// TestServiceListSnapshotsAreCoherentUnderRaces runs decisions on four
// drivers while readers list concurrently: every list is sorted, carries one
// revision on every entry, revisions never go backwards for a reader, and a
// revision always shows the same content to every reader.
func TestServiceListSnapshotsAreCoherentUnderRaces(t *testing.T) {
	t.Parallel()
	ids := []InstanceID{"fam.d", "fam.b", "fam.c", "fam.a"}
	f := newFixture(t, svcOpts{drivers: enabled("fam.d", "fam.b", "fam.c", "fam.a")})
	for _, id := range ids {
		f.waitSettled(id, ObservedRunning)
	}
	stop := make(chan struct{})
	var seen sync.Map
	var readers, writers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			var last CatalogRevision
			for {
				select {
				case <-stop:
					return
				default:
				}
				view := f.svc.List()
				if view.Revision < last {
					t.Errorf("revision went backwards: %d -> %d", last, view.Revision)
					return
				}
				last = view.Revision
				if !sort.SliceIsSorted(view.Drivers, func(i, j int) bool { return view.Drivers[i].InstanceID < view.Drivers[j].InstanceID }) || len(view.Drivers) != 4 {
					t.Errorf("unsorted or incomplete list: %+v", view.Drivers)
					return
				}
				for _, entry := range view.Drivers {
					if entry.Revision != view.Revision {
						t.Errorf("entry revision %d in view %d", entry.Revision, view.Revision)
						return
					}
				}
				if prior, loaded := seen.LoadOrStore(view.Revision, view.Drivers); loaded && !reflect.DeepEqual(prior, view.Drivers) {
					t.Errorf("revision %d shows two contents", view.Revision)
					return
				}
			}
		}()
	}
	for _, id := range ids {
		writers.Add(1)
		go func(id InstanceID) {
			defer writers.Done()
			for i := 0; i < 6; i++ {
				method := MethodStop
				if f.status(id).DesiredState == DesiredStopped {
					method = MethodStart
				}
				if _, err := f.svc.Control(deadlineCtx(t), method, body(id, f.status(id).DesiredRevision)); err != nil {
					var r *Refusal
					if !errors.As(err, &r) || (r.Code != RefusalTransitionBusy && r.Code != RefusalRevisionConflict) {
						t.Errorf("%s %s: %v", method, id, err)
						return
					}
				}
				time.Sleep(time.Millisecond)
			}
		}(id)
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	for _, id := range ids {
		f.waitIdle(id)
	}
}

// --- concurrency, capacity and restart ordering ---

func raceDecisions(t *testing.T, f *svcFixture, id InstanceID, methods []Method, rev DesiredRevision) ([]DesiredView, []error) {
	t.Helper()
	views := make([]DesiredView, len(methods))
	errs := make([]error, len(methods))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, m := range methods {
		wg.Add(1)
		go func(i int, m Method) {
			defer wg.Done()
			<-start
			views[i], errs[i] = f.svc.Control(deadlineCtx(t), m, body(id, rev))
		}(i, m)
	}
	close(start)
	wg.Wait()
	return views, errs
}

func countAccepted(t *testing.T, views []DesiredView, errs []error, rev DesiredRevision) int {
	t.Helper()
	accepted := 0
	for i, err := range errs {
		if err == nil {
			if views[i].DesiredRevision == rev {
				t.Fatalf("a changing request returned the old revision")
			}
			accepted++
			continue
		}
		refusalOf(t, err, RefusalRevisionConflict, 6)
	}
	return accepted
}

// TestServiceConcurrentRequestsObeyConflictAndCapacity: simultaneous
// changing requests carrying the same expected revision accept exactly one
// (the rest conflict); simultaneous same-desired requests are all no-ops;
// RESTART races accept one; STOP in flight makes RESTART FAILED_PRECONDITION,
// START takes the reserved slot and any further changing request is
// TRANSITION_BUSY (at the service).
func TestServiceConcurrentRequestsObeyConflictAndCapacity(t *testing.T) {
	t.Parallel()
	f := newFixture(t, svcOpts{drivers: enabled("fam.alpha")})
	id := InstanceID("fam.alpha")
	f.waitSettled(id, ObservedRunning)

	rev := f.status(id).DesiredRevision
	views, errs := raceDecisions(t, f, id, []Method{MethodRestart, MethodRestart, MethodRestart}, rev)
	if n := countAccepted(t, views, errs, rev); n != 1 {
		t.Fatalf("RESTART race accepted %d", n)
	}
	f.waitSettled(id, ObservedRunning)

	rev = f.status(id).DesiredRevision
	views, errs = raceDecisions(t, f, id, []Method{MethodStop, MethodRestart, MethodStop}, rev)
	if n := countAccepted(t, views, errs, rev); n != 1 {
		t.Fatalf("STOP/RESTART race accepted %d", n)
	}
	f.waitIdle(id)
	if f.status(id).DesiredState == DesiredRunning {
		f.mustDecide(MethodStop, id)
	}
	f.waitSettled(id, ObservedStopped)

	rev = f.status(id).DesiredRevision
	views, errs = raceDecisions(t, f, id, []Method{MethodStop, MethodStop, MethodStop, MethodStop}, rev)
	for i, err := range errs {
		if err != nil || views[i] != (DesiredView{DesiredStopped, rev}) {
			t.Fatalf("same-desired race: %+v %v", views[i], err)
		}
	}

	// Capacity during STOPPING.
	f.mustDecide(MethodStart, id)
	f.waitSettled(id, ObservedRunning)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f.execs[id].setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
		once.Do(func() { close(entered) })
		<-release
		return StopOutcome{NativeClosed: true, SemanticCleaned: true}
	})
	f.mustDecide(MethodStop, id)
	<-entered
	f.waitObserved(id, ObservedStopping)
	_, err := f.decide(MethodRestart, id)
	refusalOf(t, err, RefusalFailedPrecondition, 6)
	if v, err := f.decide(MethodStop, id); err != nil || v.DesiredState != DesiredStopped {
		t.Fatalf("STOP during STOPPING = %+v, %v", v, err)
	}
	startView := f.mustDecide(MethodStart, id)
	rec := f.record(id)
	if rec.Current == nil || rec.Current.Kind != KindStop || rec.Reserved == nil || rec.Reserved.Kind != KindStart ||
		rec.Reserved.DesiredRevision != startView.DesiredRevision {
		t.Fatalf("START during STOPPING did not take the reserved slot: %+v", rec)
	}
	for _, m := range []Method{MethodRestart, MethodStop} {
		_, err := f.decide(m, id)
		refusalOf(t, err, RefusalTransitionBusy, 8)
	}
	if v, err := f.decide(MethodStart, id); err != nil || v != startView {
		t.Fatalf("same-desired START with both slots occupied = %+v, %v", v, err)
	}
	close(release)
	f.waitSettled(id, ObservedRunning)
}

type fencingCase struct {
	name    string
	untyped bool
	backoff bool
	method  Method
}

// TestServiceStopOrRestartDuringStartingOrBackoffFencesAndKeepsCleanup is
// STARTING and BACKOFF, each with a typed START and with an untyped
// process-start attempt. STOP or RESTART takes the reserved slot (the untyped
// case synthesizes the cancellation as the current slot with the attempt's
// correlation and obligations), fences the old attempt, keeps its cleanup
// obligations until a Stop with the old correlation settles them, never
// publishes a late result, refuses a further changing request as
// TRANSITION_BUSY, and loses no accepted RESTART.
func TestServiceStopOrRestartDuringStartingOrBackoffFencesAndKeepsCleanup(t *testing.T) {
	t.Parallel()
	for _, tc := range []fencingCase{
		{"typed starting stop", false, false, MethodStop},
		{"typed starting restart", false, false, MethodRestart},
		{"typed backoff stop", false, true, MethodStop},
		{"typed backoff restart", false, true, MethodRestart},
		{"untyped starting stop", true, false, MethodStop},
		{"untyped starting restart", true, false, MethodRestart},
		{"untyped backoff stop", true, true, MethodStop},
		{"untyped backoff restart", true, true, MethodRestart},
	} {
		t.Run(tc.name, func(t *testing.T) { runFencingCase(t, tc) })
	}
}

func runFencingCase(t *testing.T, tc fencingCase) {
	id := InstanceID("fam.alpha")
	entered := make(chan LifecycleCorrelation, 16)
	var attemptCtx atomic.Value
	failing := func(ctx context.Context, c LifecycleCorrelation) error {
		entered <- c
		return retryErr{"warming"}
	}
	blocking := func(ctx context.Context, c LifecycleCorrelation) error {
		attemptCtx.Store(ctx)
		entered <- c
		<-ctx.Done()
		return nil // a success reported only after the attempt was fenced
	}
	pick := blocking
	if tc.backoff {
		pick = failing
	}
	f := newFixture(t, svcOpts{
		drivers: enabled(string(id)),
		opts:    func(o *ServiceOptions) { o.RetryDelay = func(int) time.Duration { return time.Hour } },
		setup: func(f *svcFixture) {
			if tc.untyped {
				f.execs[id].setStart(pick)
			}
		},
	})
	stopRelease := make(chan struct{})
	if !tc.untyped {
		f.waitSettled(id, ObservedRunning)
		f.mustDecide(MethodStop, id)
		f.waitSettled(id, ObservedStopped)
		f.execs[id].setStart(pick)
		f.mustDecide(MethodStart, id)
	}
	old := <-entered
	if tc.backoff {
		f.waitObserved(id, ObservedBackoff)
	} else {
		f.waitObserved(id, ObservedStarting)
	}
	before := f.record(id)
	// Hold the cleanup Stop so the slot layout can be inspected.
	stopEntered := make(chan LifecycleCorrelation, 4)
	f.execs[id].setStop(func(_ context.Context, c LifecycleCorrelation) StopOutcome {
		stopEntered <- c
		<-stopRelease
		return StopOutcome{NativeClosed: true, SemanticCleaned: true}
	})
	f.execs[id].setStart(nil)
	cursor := f.svc.events.cursor()
	view := f.mustDecide(tc.method, id)

	rec := f.record(id)
	if rec.Reserved == nil || rec.Reserved.Kind != TransitionKind(tc.method) || rec.Reserved.DesiredRevision != view.DesiredRevision {
		t.Fatalf("%s did not take the reserved slot: %+v", tc.method, rec)
	}
	if tc.untyped {
		cur := rec.Current
		if before.Current != nil || cur == nil || cur.Kind != KindCancel || cur.Epoch != old.Epoch ||
			cur.Generation != old.Generation || !cur.NativeClosePending || !cur.SemanticCleanupPending || cur.DeadlineUTC.IsZero() {
			t.Fatalf("untyped cancellation not synthesized with the attempt's correlation: %+v (attempt %+v)", cur, old)
		}
	} else {
		cur := rec.Current
		if cur == nil || !sameIdentity(cur, before.Current) || cur.Epoch != old.Epoch || !cur.NativeClosePending || !cur.SemanticCleanupPending {
			t.Fatalf("typed START slot or its obligations changed: %+v -> %+v", before.Current, cur)
		}
	}
	// Any further changing request while both slots are occupied is busy.
	other := MethodStart
	if view.DesiredState == DesiredRunning {
		other = MethodStop
	}
	_, err := f.decide(other, id)
	refusalOf(t, err, RefusalTransitionBusy, 8)

	cleanup := <-stopEntered
	if cleanup.Epoch != old.Epoch || cleanup.Generation != old.Generation {
		t.Fatalf("cleanup used correlation %+v, want the fenced attempt's %+v", cleanup, old)
	}
	if !tc.backoff {
		if ctx, _ := attemptCtx.Load().(context.Context); ctx == nil || ctx.Err() == nil {
			t.Fatalf("fenced attempt context not canceled")
		}
	}
	close(stopRelease)
	want := ObservedStopped
	if tc.method == MethodRestart {
		want = ObservedRunning
	}
	f.waitSettled(id, want)
	for _, ev := range f.svc.EventsSince(cursor).Events {
		if ev.Kind == EventObservedChanged && ev.ObservedState == ObservedRunning && tc.method == MethodStop {
			t.Fatalf("fenced attempt published RUNNING after acceptance")
		}
	}
	if tc.method == MethodRestart {
		calls := f.execs[id].snapshot()
		last := calls[len(calls)-1]
		if last.Op != "start" || last.Corr.Epoch == old.Epoch || last.Corr.Generation != 1 {
			t.Fatalf("accepted RESTART did not start a new epoch: %+v", last)
		}
	}
}

// TestServiceEveryAcceptedRestartExecutesInOrder: each accepted RESTART in
// RUNNING takes a new revision and performs exactly one stop-then-start with
// a new epoch, in acceptance order; a RESTART while one executes is
// TRANSITION_BUSY; an old-revision retry conflicts and a fresh-revision retry
// after settlement is another restart.
func TestServiceEveryAcceptedRestartExecutesInOrder(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id))})
	f.waitSettled(id, ObservedRunning)
	var epochs []SourceEpoch
	epochs = append(epochs, f.execs[id].snapshot()[0].Corr.Epoch)
	for i := 0; i < 3; i++ {
		prior := f.status(id).DesiredRevision
		calls := len(f.execs[id].snapshot())
		view := f.mustDecide(MethodRestart, id)
		if view.DesiredState != DesiredRunning || view.DesiredRevision <= prior {
			t.Fatalf("RESTART view = %+v", view)
		}
		f.waitSettled(id, ObservedRunning)
		got := f.execs[id].snapshot()[calls:]
		if len(got) != 2 || got[0].Op != "stop" || got[0].Corr.Epoch != epochs[len(epochs)-1] || got[1].Op != "start" {
			t.Fatalf("RESTART %d executed %+v", i, got)
		}
		for _, e := range epochs {
			if got[1].Corr.Epoch == e {
				t.Fatalf("RESTART reused epoch %s", e)
			}
		}
		epochs = append(epochs, got[1].Corr.Epoch)
		_, err := f.svc.Control(deadlineCtx(t), MethodRestart, body(id, prior))
		refusalOf(t, err, RefusalRevisionConflict, 6)
	}
	// A RESTART while another RESTART executes is busy, never coalesced.
	entered := make(chan LifecycleCorrelation, 1)
	release := make(chan struct{})
	f.execs[id].setStart(blockingStart(entered, release, nil))
	f.mustDecide(MethodRestart, id)
	<-entered
	_, err := f.decide(MethodRestart, id)
	refusalOf(t, err, RefusalTransitionBusy, 8)
	close(release)
	f.waitSettled(id, ObservedRunning)
}

// TestServiceResultsAreNotReceipts: a decision result encodes exactly
// desired_state and desired_revision, and no result, view, event or
// diagnostic type carries an operation handle, receipt, key, principal,
// authority or endpoint field.
func TestServiceResultsAreNotReceipts(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(DesiredView{DesiredState: DesiredRunning, DesiredRevision: 42})
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]any
	if err := json.Unmarshal(raw, &members); err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members["desired_state"] != "RUNNING" || members["desired_revision"] != "42" {
		t.Fatalf("result = %s", raw)
	}
	forbidden := []string{"receipt", "idempoten", "principal", "authoriz", "authority", "permission", "actor",
		"token", "credential", "operation", "sequence", "endpoint", "address", "host", "url", "payload", "error"}
	visited := map[reflect.Type]bool{}
	var walk func(path string, typ reflect.Type)
	walk = func(path string, typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if visited[typ] || typ.Kind() != reflect.Struct {
			return
		}
		visited[typ] = true
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.ToLower(field.Name)
			for _, word := range forbidden {
				if strings.Contains(name, word) {
					t.Fatalf("%s.%s is a forbidden field", path, field.Name)
				}
			}
			walk(path+"."+field.Name, field.Type)
		}
	}
	// The event shape is pinned: categorical state plus the lifecycle
	// correlation (epoch, generation) of S6.1, and no operation handle.
	var eventFields []string
	for i := 0; i < reflect.TypeOf(Event{}).NumField(); i++ {
		eventFields = append(eventFields, reflect.TypeOf(Event{}).Field(i).Name)
	}
	wantFields := []string{"Cursor", "Kind", "InstanceID", "CatalogRevision", "DesiredState", "DesiredRevision",
		"ObservedState", "Reason", "SafetyQuarantined", "Epoch", "Generation"}
	if !reflect.DeepEqual(eventFields, wantFields) {
		t.Fatalf("event fields = %v, want %v", eventFields, wantFields)
	}
	if reflect.TypeOf(Event{}.Epoch) != reflect.TypeOf(SourceEpoch("")) || reflect.TypeOf(Event{}.Generation) != reflect.TypeOf(DriverGeneration(0)) {
		t.Fatalf("event correlation fields use the wrong identity types")
	}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(DesiredView{}), reflect.TypeOf(Catalog{}), reflect.TypeOf(DriverStatus{}),
		reflect.TypeOf(Event{}), reflect.TypeOf(EventPage{}), reflect.TypeOf(Diagnostic{}), reflect.TypeOf(Request{}),
	} {
		walk(typ.Name(), typ)
	}
}

// TestServiceCancellationAfterAcceptanceKeepsWork: cancelling the caller's
// context after acceptance neither retracts the decision nor cancels the
// persisted work, which runs under the service's own lifetime and bounded
// budget; an exhausted budget ends the work FAILED with desired intact.
func TestServiceCancellationAfterAcceptanceKeepsWork(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id), "fam.beta"),
		opts: func(o *ServiceOptions) { o.OperationBudget = 300 * time.Millisecond }})
	f.waitSettled(id, ObservedRunning)
	f.mustDecide(MethodStop, id)
	f.waitSettled(id, ObservedStopped)
	entered := make(chan LifecycleCorrelation, 1)
	release := make(chan struct{})
	var startCtx atomic.Value
	f.execs[id].setStart(func(ctx context.Context, c LifecycleCorrelation) error {
		startCtx.Store(ctx)
		entered <- c
		<-release
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	view, err := f.svc.Control(ctx, MethodStart, body(id, f.status(id).DesiredRevision))
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if c := startCtx.Load().(context.Context); c.Err() != nil {
		t.Fatalf("caller cancellation reached the persisted work")
	}
	close(release)
	f.waitSettled(id, ObservedRunning)
	if rec := f.record(id); rec.DesiredRevision != view.DesiredRevision || rec.DesiredState != DesiredRunning {
		t.Fatalf("cancellation retracted intent: %+v", rec)
	}

	// Bounded budget: a START that keeps failing retryably ends FAILED.
	beta := InstanceID("fam.beta")
	f.waitSettled(beta, ObservedRunning)
	f.mustDecide(MethodStop, beta)
	f.waitSettled(beta, ObservedStopped)
	f.execs[beta].setStart(func(context.Context, LifecycleCorrelation) error { return retryErr{"never"} })
	startsBefore := f.execs[beta].count("start")
	accepted := f.mustDecide(MethodStart, beta)
	f.waitSettled(beta, ObservedFailed)
	if st := f.status(beta); st.Reason != ReasonRetryExhausted || st.DesiredState != DesiredRunning || st.DesiredRevision != accepted.DesiredRevision {
		t.Fatalf("exhausted START = %+v", st)
	}
	starts := f.execs[beta].count("start") - startsBefore
	if starts == 0 || starts > DefaultMaxAttempts {
		t.Fatalf("attempts = %d, want within the bound", starts)
	}
}

// --- isolation between drivers ---

// Documented responsiveness bound: list and get read an atomically published
// view and never wait for a store write, a reconciler or a decision, so each
// read returns within readBound even while another driver blocks. A decision
// for another driver waits at most the store's bounded serialization wait.
const readBound = 50 * time.Millisecond

func assertReadsResponsive(t *testing.T, f *svcFixture, ids ...InstanceID) {
	t.Helper()
	for i := 0; i < 50; i++ {
		start := time.Now()
		f.svc.List()
		for _, id := range ids {
			if _, err := f.svc.Get(id); err != nil {
				t.Fatal(err)
			}
		}
		if elapsed := time.Since(start); elapsed > readBound {
			t.Fatalf("list/get took %v, above the documented %v bound", elapsed, readBound)
		}
	}
}

// TestServiceTwoDriversStayResponsiveWhileOneBlocks: a reconciliation that
// blocks in driver A's executor, a terminal failure in A, and a store write
// for A blocked inside the filesystem each leave list/get responsive within
// the documented bound; B keeps deciding and reconciling (its decision waits
// only until its own context ends while A's write holds the store).
func TestServiceTwoDriversStayResponsiveWhileOneBlocks(t *testing.T) {
	t.Parallel()
	a, b := InstanceID("fam.a"), InstanceID("fam.b")
	cycleB := func(t *testing.T, f *svcFixture) {
		start := time.Now()
		f.mustDecide(MethodStop, b)
		f.waitSettled(b, ObservedStopped)
		f.mustDecide(MethodStart, b)
		f.waitSettled(b, ObservedRunning)
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("driver B took %v while A was blocked", elapsed)
		}
	}
	t.Run("blocked reconciliation", func(t *testing.T) {
		f := newFixture(t, svcOpts{drivers: enabled(string(a), string(b))})
		f.waitSettled(a, ObservedRunning)
		f.waitSettled(b, ObservedRunning)
		f.mustDecide(MethodStop, a)
		f.waitSettled(a, ObservedStopped)
		entered := make(chan LifecycleCorrelation, 1)
		f.execs[a].setStart(blockingStart(entered, make(chan struct{}), nil))
		f.mustDecide(MethodStart, a)
		<-entered
		assertReadsResponsive(t, f, a, b)
		cycleB(t, f)
		assertReadsResponsive(t, f, a, b)
		if st := f.status(a); st.ObservedState != ObservedStarting {
			t.Fatalf("blocked driver = %+v", st)
		}
	})
	t.Run("failure", func(t *testing.T) {
		f := newFixture(t, svcOpts{drivers: enabled(string(a), string(b)), setup: func(f *svcFixture) {
			f.execs[a].setStart(func(context.Context, LifecycleCorrelation) error { return errors.New("terminal") })
		}})
		f.waitObserved(a, ObservedFailed)
		f.waitSettled(b, ObservedRunning)
		cycleB(t, f)
		assertReadsResponsive(t, f, a, b)
	})
	t.Run("blocked persistence", func(t *testing.T) {
		const waitBound = 300 * time.Millisecond
		f := newFixture(t, svcOpts{drivers: enabled(string(a), string(b))})
		f.waitSettled(a, ObservedRunning)
		f.waitSettled(b, ObservedRunning)
		f.quiesce()
		entered, release := f.hooks.arm()
		done := make(chan error, 1)
		go func() {
			_, err := f.svc.Control(deadlineCtx(t), MethodStop, body(a, f.status(a).DesiredRevision))
			done <- err
		}()
		<-entered
		assertReadsResponsive(t, f, a, b)
		ctx, cancel := context.WithTimeout(context.Background(), waitBound)
		defer cancel()
		start := time.Now()
		_, err := f.svc.Control(ctx, MethodStop, body(b, f.status(b).DesiredRevision))
		elapsed := time.Since(start)
		refusalOf(t, err, RefusalDeadlineExceeded, 5)
		if elapsed > waitBound+250*time.Millisecond {
			t.Fatalf("B's decision waited %v, above the bounded wait %v", elapsed, waitBound)
		}
		assertReadsResponsive(t, f, a, b)
		close(release)
		if err := <-done; err != nil {
			t.Fatalf("A's blocked STOP: %v", err)
		}
		f.waitSettled(a, ObservedStopped)
		cycleB(t, f)
	})
}

// --- recovery from in-flight state only ---

func transitionJSON(kind TransitionKind, rev, seq uint64, phase TransitionPhase, epoch string, gen uint64, native, semantic bool, attempts int, deadline string) string {
	return fmt.Sprintf(`{"kind":%q,"desired_revision":"%d","sequence":"%d","phase":%q,"epoch":%q,"generation":"%d",`+
		`"native_close_pending":%t,"semantic_cleanup_pending":%t,"attempts_used":%d,"deadline_utc":%q}`,
		kind, rev, seq, phase, epoch, gen, native, semantic, attempts, deadline)
}

func recordJSON(state DesiredState, rev, lifecycle uint64, current string) string {
	if current == "" {
		current = "null"
	}
	return fmt.Sprintf(`{"desired_state":%q,"desired_revision":"%d","lifecycle_sequence":"%d","current":%s,"reserved":null}`,
		state, rev, lifecycle, current)
}

func recoveryJSON(ids []string, maxRev uint64, payload []byte) string {
	sum := sha256.Sum256(payload)
	quoted, _ := json.Marshal(ids)
	return fmt.Sprintf(`{"format":%q,"instance_ids":%s,"max_desired_revision":"%d","payload_sha256":%q,"payload":%q}`,
		controlRecoveryFormat, quoted, maxRev, hex.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString(payload))
}

func controlState(mark, catalog uint64, drivers map[string]string, recovery string) func(*testing.T, string) {
	return func(t *testing.T, path string) {
		t.Helper()
		ids := make([]string, 0, len(drivers))
		for id := range drivers {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var parts []string
		for _, id := range ids {
			parts = append(parts, fmt.Sprintf("%q:%s", id, drivers[id]))
		}
		rec := ""
		if recovery != "" {
			rec = `,"recovery":` + recovery
		}
		body := fmt.Sprintf(`{"schema_version":1,"issued_revision_mark":"%d","catalog_revision":"%d","instances":{%s}%s}`,
			mark, catalog, strings.Join(parts, ","), rec)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestServiceRecoveryUsesOnlyInFlightState restarts over committed in-flight
// work: recovery material is restored before any executor call; a PENDING START
// starts under a new epoch; an ACTIVE RESTART left by an earlier incarnation
// settles its historical correlation and performs its one replacement; a
// terminal STOP is only pruned; desired RUNNING without work gets the untyped
// process-start attempt and desired STOPPED without work stays untouched.
// Only the pruned slots are secured, and no completed-operation history
// exists to consult.
func TestServiceRecoveryUsesOnlyInFlightState(t *testing.T) {
	t.Parallel()
	far := "2099-01-01T00:00:00Z"
	payload := []byte("synthetic-d5-material")
	drivers := map[string]string{
		"fam.a": recordJSON(DesiredRunning, 1, 1, transitionJSON(KindStart, 1, 1, PhasePending, "", 0, false, false, 0, "")),
		"fam.b": recordJSON(DesiredRunning, 2, 4, transitionJSON(KindRestart, 2, 4, PhaseActive, "old.fam.b.1", 3, true, true, 1, far)),
		"fam.c": recordJSON(DesiredStopped, 3, 2, transitionJSON(KindStop, 3, 2, PhaseCleanup, "old.fam.c.1", 1, false, false, 0, far)),
		"fam.d": recordJSON(DesiredRunning, 4, 0, ""),
		"fam.e": recordJSON(DesiredStopped, 5, 0, ""),
	}
	f := newFixture(t, svcOpts{
		drivers:     enabled("fam.a", "fam.b", "fam.c", "fam.d", "fam.e"),
		state:       controlState(5, 9, drivers, recoveryJSON([]string{"fam.b"}, 2, payload)),
		incarnation: "inc2",
	})
	for id, want := range map[InstanceID]ObservedState{
		"fam.a": ObservedRunning, "fam.b": ObservedRunning, "fam.c": ObservedStopped,
		"fam.d": ObservedRunning, "fam.e": ObservedStopped,
	} {
		f.waitSettled(id, want)
	}
	entries := f.log.list()
	if len(entries) == 0 || entries[0] != "restore" {
		t.Fatalf("recovery restore did not precede executor work: %v", entries)
	}
	if len(f.rec.restored) != 1 || string(f.rec.restored[0].Payload) != string(payload) {
		t.Fatalf("restored material = %+v", f.rec.restored)
	}
	want := map[InstanceID][]execCall{
		"fam.a": {{"start", LifecycleCorrelation{InstanceID: "fam.a", Epoch: makeEpoch("inc2", "fam.a", 1), Generation: 1, Sequence: 1}}},
		"fam.b": {
			{"stop", LifecycleCorrelation{InstanceID: "fam.b", Epoch: "old.fam.b.1", Generation: 3, Sequence: 4}},
			{"start", LifecycleCorrelation{InstanceID: "fam.b", Epoch: makeEpoch("inc2", "fam.b", 1), Generation: 1, Sequence: 4}},
		},
		"fam.c": nil,
		"fam.d": {{"start", LifecycleCorrelation{InstanceID: "fam.d", Epoch: makeEpoch("inc2", "fam.d", 1), Generation: 1}}},
		"fam.e": nil,
	}
	for id, calls := range want {
		if got := f.execs[id].snapshot(); !reflect.DeepEqual(got, calls) {
			t.Fatalf("%s executor calls = %+v, want %+v", id, got, calls)
		}
	}
	securedIDs := map[InstanceID]int{}
	for _, entry := range f.log.list() {
		if id, ok := strings.CutPrefix(entry, "secure:"); ok {
			securedIDs[InstanceID(id)]++
		}
	}
	if !reflect.DeepEqual(securedIDs, map[InstanceID]int{"fam.a": 1, "fam.b": 1, "fam.c": 1}) {
		t.Fatalf("secured slots = %v", securedIDs)
	}
	for _, tr := range f.rec.securedFor("") {
		if !terminal(&tr) {
			t.Fatalf("secured a non-terminal slot: %+v", tr)
		}
	}
	if m := f.ms.Recovery(); m == nil || string(m.Payload) != string(payload) {
		t.Fatalf("committed recovery material changed: %+v", m)
	}
	for _, id := range f.svc.order {
		if len(f.svc.Diagnostics(id)) > 1 {
			t.Fatalf("diagnostics carried history across the restart")
		}
	}
}

// TestServiceRestartRecoveredAfterProcessLoss covers: process memory is
// lost while an accepted RESTART is ACTIVE; the new incarnation settles the
// historical correlation and performs the retained replacement under a new
// epoch before dropping the slot, although desired already says RUNNING.
func TestServiceRestartRecoveredAfterProcessLoss(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id))})
	f.waitSettled(id, ObservedRunning)
	entered := make(chan LifecycleCorrelation, 1)
	f.execs[id].setStart(blockingStart(entered, make(chan struct{}), nil))
	view := f.mustDecide(MethodRestart, id)
	active := <-entered
	held := f.record(id).Current
	if held == nil || held.Kind != KindRestart || held.Phase != PhaseActive || held.Epoch != active.Epoch {
		t.Fatalf("RESTART slot = %+v", held)
	}
	f.svc.Close()
	if after := f.record(id).Current; !reflect.DeepEqual(after, held) {
		t.Fatalf("closing the service rewrote in-flight work: %+v -> %+v", held, after)
	}
	if err := f.m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	m2 := openManager(t, f.path, &blockHooks{}, enabled(string(id)))
	exec2 := &fakeExec{id: id}
	svc2, err := NewService(ServiceOptions{Store: &managerStore{m: m2}, Recovery: &fakeRecovery{}, Incarnation: "inc2",
		Drivers: []ServiceInstance{{InstanceID: id, Executor: exec2}}, RetryDelay: func(int) time.Duration { return 2 * time.Millisecond }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		svc2.Close()
		_ = m2.Stop(context.Background())
	})
	if err := svc2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "replacement after process loss", func() bool {
		st, _ := svc2.Get(id)
		rec, _ := m2.Driver(id)
		return st.ObservedState == ObservedRunning && rec.Current == nil
	})
	calls := exec2.snapshot()
	if len(calls) != 2 || calls[0].Op != "stop" || calls[0].Corr.Epoch != active.Epoch || calls[0].Corr.Generation != active.Generation ||
		calls[1].Op != "start" || !strings.HasPrefix(string(calls[1].Corr.Epoch), "inc2.") || calls[1].Corr.Generation != 1 {
		t.Fatalf("recovered RESTART executed %+v", calls)
	}
	if st, _ := svc2.Get(id); st.DesiredRevision != view.DesiredRevision {
		t.Fatalf("recovery changed desired revision: %+v", st)
	}
}

// --- pruning, recovery material and diagnostics ---

// TestServicePrunesOnlyAfterRecoveryMaterialSecured: a terminal slot stays
// durable while the recovery adapter cannot secure its material; once it
// can, the material is committed before the slot is pruned. Diagnostics stay
// a bounded sanitized in-memory ring.
func TestServicePrunesOnlyAfterRecoveryMaterialSecured(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	sentinel := errors.New("synthetic secure rejection")
	var allow atomic.Bool
	rec := &fakeRecovery{secure: func(_ InstanceID, tr Transition) (*RecoveryMaterial, error) {
		if !allow.Load() {
			return nil, fmt.Errorf("wrapped: %w", sentinel)
		}
		return &RecoveryMaterial{DriverIDs: []InstanceID{id}, MaxDesiredRevision: tr.DesiredRevision,
			Payload: []byte(fmt.Sprintf("d5-%d", tr.Sequence))}, nil
	}}
	gate := &gateStore{ops: &orderLog{}}
	f := newFixture(t, svcOpts{drivers: enabled(string(id)), recovery: rec,
		wrap: func(s Store) Store { gate.Store = s; return gate }})
	f.waitObserved(id, ObservedRunning)
	f.mustDecide(MethodStop, id)
	waitFor(t, "a secure failure", func() bool {
		for _, err := range f.errs.list() {
			var se *SemanticError
			if errors.As(err, &se) && se.Op == "secure" && errors.Is(err, sentinel) && se.InstanceID == id {
				return true
			}
		}
		return false
	})
	slot := f.record(id).Current
	if slot == nil || !terminal(slot) || slot.Kind != KindStop {
		t.Fatalf("terminal slot pruned before its material was secured: %+v", slot)
	}
	if got := f.fileCurrentKind(id); got != "STOP" {
		t.Fatalf("terminal slot missing from durable state while unsecured: %q", got)
	}
	allow.Store(true)
	f.waitSettled(id, ObservedStopped)
	if m := f.ms.Recovery(); m == nil || string(m.Payload) != fmt.Sprintf("d5-%d", slot.Sequence) {
		t.Fatalf("secured material = %+v", m)
	}
	ops := gate.ops.list()
	if len(ops) < 2 || ops[len(ops)-2] != "recovery" || ops[len(ops)-1] != "prune:"+string(id) {
		t.Fatalf("material not committed before pruning: %v", ops)
	}

	// Bounded sanitized diagnostics.
	f.execs[id].setStart(func(context.Context, LifecycleCorrelation) error {
		return errors.New("refused by endpoint 192.0.2.7:4444 with secret=abc")
	})
	f.mustDecide(MethodStart, id)
	f.waitSettled(id, ObservedFailed)
	f.execs[id].setStart(nil)
	for i := 0; i < 17; i++ {
		if f.status(id).DesiredState == DesiredStopped {
			f.mustDecide(MethodStart, id)
		} else {
			f.mustDecide(MethodRestart, id)
		}
		f.waitSettled(id, ObservedRunning)
		f.mustDecide(MethodStop, id)
		f.waitSettled(id, ObservedStopped)
	}
	diags := f.svc.Diagnostics(id)
	if len(diags) != MaxDiagnostics {
		t.Fatalf("diagnostics = %d, want the %d bound", len(diags), MaxDiagnostics)
	}
	for _, d := range diags {
		if len(d.Summary) > MaxDiagnosticBytes || strings.Contains(d.Summary, "192.0.2") || strings.Contains(d.Summary, "secret") {
			t.Fatalf("unsanitized diagnostic %q", d.Summary)
		}
	}
	fresh, err := NewService(ServiceOptions{Store: f.store, Recovery: rec, Incarnation: "inc3",
		Drivers: []ServiceInstance{{InstanceID: id, Executor: f.execs[id]}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Diagnostics(id)) != 0 {
		t.Fatalf("diagnostics survived a new service instance")
	}
}

// --- events ---

// TestServiceEventsAreCommittedBoundedAndResyncable: refusals and a
// pre-commit write failure emit nothing; accepted decisions and observed
// transitions emit events that reference a published catalog revision; the
// ring is bounded and a reader behind the window is told to resync.
func TestServiceEventsAreCommittedBoundedAndResyncable(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id)), opts: func(o *ServiceOptions) { o.EventCapacity = 8 }})
	f.waitSettled(id, ObservedRunning)
	cursor := f.svc.events.cursor()
	view := f.svc.List()
	running := f.execs[id].snapshot()[0].Corr

	f.hooks.mu.Lock()
	f.hooks.failRename = errors.New("synthetic rename refusal")
	f.hooks.mu.Unlock()
	_, err := f.decide(MethodStop, id)
	refusalOf(t, err, RefusalUnavailable, 0)
	f.hooks.mu.Lock()
	f.hooks.failRename = nil
	f.hooks.mu.Unlock()
	if f.svc.events.cursor() != cursor || !reflect.DeepEqual(f.svc.List(), view) {
		t.Fatalf("pre-commit failure emitted an event or changed the view")
	}

	notify := f.svc.EventNotify()
	accepted := f.mustDecide(MethodStop, id)
	select {
	case <-notify:
	case <-time.After(5 * time.Second):
		t.Fatalf("no event notification")
	}
	f.waitSettled(id, ObservedStopped)
	page := f.svc.EventsSince(cursor)
	if page.Resync || len(page.Events) < 2 || page.Events[0].Kind != EventDesiredAccepted ||
		page.Events[0].DesiredRevision != accepted.DesiredRevision || page.Events[0].DesiredState != DesiredStopped {
		t.Fatalf("events after acceptance = %+v", page)
	}
	last := page.Events[len(page.Events)-1]
	if last.Kind != EventObservedChanged || last.ObservedState != ObservedStopped {
		t.Fatalf("last event = %+v", last)
	}
	// Every event of this STOP names the withdrawn generation's correlation.
	for _, ev := range page.Events {
		if ev.Epoch != running.Epoch || ev.Generation != running.Generation {
			t.Fatalf("event correlation = %s/%d, want %s/%d", ev.Epoch, ev.Generation, running.Epoch, running.Generation)
		}
	}
	// A later START carries the new attempt's epoch and generation 1.
	startCursor := f.svc.events.cursor()
	f.mustDecide(MethodStart, id)
	f.waitSettled(id, ObservedRunning)
	calls := f.execs[id].snapshot()
	started := calls[len(calls)-1].Corr
	sawRunning := false
	for _, ev := range f.svc.EventsSince(startCursor).Events {
		if ev.Kind == EventObservedChanged && ev.ObservedState == ObservedRunning {
			sawRunning = true
			if ev.Epoch != started.Epoch || ev.Generation != 1 || ev.Epoch == running.Epoch {
				t.Fatalf("RUNNING event correlation = %s/%d, want new epoch %s/1", ev.Epoch, ev.Generation, started.Epoch)
			}
		}
	}
	if !sawRunning {
		t.Fatalf("no RUNNING event after START")
	}
	f.mustDecide(MethodStop, id)
	f.waitSettled(id, ObservedStopped)
	for i, ev := range page.Events {
		if ev.Cursor != cursor+uint64(i)+1 || ev.CatalogRevision == 0 || ev.CatalogRevision > f.svc.List().Revision {
			t.Fatalf("event %d = %+v", i, ev)
		}
	}
	if got := f.svc.List(); got.EventCursor != f.svc.events.cursor() {
		t.Fatalf("view cursor %d, log cursor %d", got.EventCursor, f.svc.events.cursor())
	}
	for i := 0; i < 3; i++ {
		f.mustDecide(MethodStart, id)
		f.waitSettled(id, ObservedRunning)
		f.mustDecide(MethodStop, id)
		f.waitSettled(id, ObservedStopped)
	}
	newest := f.svc.events.cursor()
	if p := f.svc.EventsSince(cursor); !p.Resync || len(p.Events) != 0 || p.Next != newest {
		t.Fatalf("reader behind the window = %+v", p)
	}
	if p := f.svc.EventsSince(newest - 3); p.Resync || len(p.Events) != 3 {
		t.Fatalf("reader inside the window = %+v", p)
	}
	if p := f.svc.EventsSince(newest); p.Resync || len(p.Events) != 0 {
		t.Fatalf("current reader = %+v", p)
	}
	if p := f.svc.EventsSince(newest + 5); !p.Resync {
		t.Fatalf("future cursor not resynced")
	}
}

// --- error identity ---

// TestServiceErrorIdentitySeparation: recovery-adapter (semantic registry)
// failures keep their original error identity inside SemanticError and never
// become lifecycle refusals; executor failures are LifecycleError; request
// refusals are Refusal. An unresolved restore withholds the driver FAILED and
// refuses admission (row 7) while an unaffected driver runs.
func TestServiceErrorIdentitySeparation(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("synthetic semantic rejection")
	payload := []byte("synthetic-d5")
	drivers := map[string]string{
		"fam.alpha": recordJSON(DesiredRunning, 1, 0, ""),
		"fam.beta":  recordJSON(DesiredRunning, 2, 0, ""),
	}
	f := newFixture(t, svcOpts{drivers: enabled("fam.alpha", "fam.beta"), noStart: true,
		state:    controlState(2, 3, drivers, recoveryJSON([]string{"fam.alpha"}, 1, payload)),
		recovery: &fakeRecovery{restoreErr: fmt.Errorf("restore: %w", sentinel)}})
	err := f.svc.Start(context.Background())
	var se *SemanticError
	var refusal *Refusal
	var le *LifecycleError
	if !errors.Is(err, sentinel) || !errors.As(err, &se) || se.InstanceID != "fam.alpha" || se.Op != "restore" {
		t.Fatalf("restore error = %v", err)
	}
	if errors.As(err, &refusal) || errors.As(err, &le) {
		t.Fatalf("semantic error converted into a lifecycle error: %v", err)
	}
	f.waitSettled("fam.beta", ObservedRunning)
	if st := f.status("fam.alpha"); st.ObservedState != ObservedFailed || st.Reason != ReasonDependencyUnavailable {
		t.Fatalf("unrestored driver = %+v", st)
	}
	if f.execs["fam.alpha"].count("start") != 0 {
		t.Fatalf("unrestored driver was started")
	}
	_, err = f.decide(MethodRestart, "fam.alpha")
	refusalOf(t, err, RefusalFailedPrecondition, 7)
	if errors.As(err, &se) {
		t.Fatalf("refusal carries a semantic error")
	}

	execSentinel := errors.New("synthetic executor failure")
	f.mustDecide(MethodStop, "fam.beta")
	f.waitSettled("fam.beta", ObservedStopped)
	f.execs["fam.beta"].setStart(func(context.Context, LifecycleCorrelation) error { return fmt.Errorf("start: %w", execSentinel) })
	f.mustDecide(MethodStart, "fam.beta")
	f.waitSettled("fam.beta", ObservedFailed)
	found := false
	for _, e := range f.errs.list() {
		if errors.Is(e, execSentinel) {
			found = true
			if !errors.As(e, &le) || le.InstanceID != "fam.beta" || le.Kind != KindStart || errors.As(e, &se) {
				t.Fatalf("executor error identity = %#v", e)
			}
		}
	}
	if !found {
		t.Fatalf("executor failure not reported")
	}
}

// --- planner, plan validation and work-update guard units ---

// TestServicePlanValidationRequiresRequestedTransition closes the planner
// residual on the service side: an accepted changing request must enqueue
// exactly one new transition of the requested kind and keep stored work.
func TestServicePlanValidationRequiresRequestedTransition(t *testing.T) {
	t.Parallel()
	stored := &Transition{Kind: KindStart, DesiredRevision: 4, Sequence: 2, Phase: PhaseActive, Epoch: "inc1.fam.a.1", Generation: 1}
	newT := func(kind TransitionKind) *Transition { return &Transition{Kind: kind, Phase: PhasePending} }
	cancel := &Transition{Kind: KindCancel, Phase: PhasePending, Epoch: "inc1.fam.a.2", Generation: 1, NativeClosePending: true}
	modified := *stored
	modified.Generation = 9
	cases := []struct {
		name string
		in   PlanInput
		plan SlotPlan
		ok   bool
	}{
		{"current request", PlanInput{Method: MethodStop}, SlotPlan{Current: newT(KindStop)}, true},
		{"empty plan", PlanInput{Method: MethodStop}, SlotPlan{}, false},
		{"wrong kind", PlanInput{Method: MethodStop}, SlotPlan{Current: newT(KindStart)}, false},
		{"two requested", PlanInput{Method: MethodStop}, SlotPlan{Current: newT(KindStop), Reserved: newT(KindStop)}, false},
		{"reserved only", PlanInput{Method: MethodStop}, SlotPlan{Reserved: newT(KindStop)}, false},
		{"not pending", PlanInput{Method: MethodStop}, SlotPlan{Current: &Transition{Kind: KindStop, Phase: PhaseActive}}, false},
		{"preassigned revision", PlanInput{Method: MethodStop}, SlotPlan{Current: &Transition{Kind: KindStop, Phase: PhasePending, DesiredRevision: 3}}, false},
		{"retained only", PlanInput{Method: MethodStop, Current: stored}, SlotPlan{Current: stored}, false},
		{"behind stored", PlanInput{Method: MethodStop, Current: stored}, SlotPlan{Current: stored, Reserved: newT(KindStop)}, true},
		{"stored dropped", PlanInput{Method: MethodStop, Current: stored}, SlotPlan{Current: newT(KindStop)}, false},
		{"stored modified", PlanInput{Method: MethodStop, Current: stored}, SlotPlan{Current: &modified, Reserved: newT(KindStop)}, false},
		{"cancel synthesized", PlanInput{Method: MethodRestart}, SlotPlan{Current: cancel, Reserved: newT(KindRestart)}, true},
		{"cancel alone", PlanInput{Method: MethodRestart}, SlotPlan{Current: cancel}, false},
		{"cancel reserved", PlanInput{Method: MethodRestart}, SlotPlan{Current: newT(KindRestart), Reserved: cancel}, false},
		{"cancel behind stored", PlanInput{Method: MethodStop, Current: stored}, SlotPlan{Current: stored, Reserved: cancel}, false},
	}
	for _, tc := range cases {
		if err := validatePlan(tc.in, tc.plan); (err == nil) != tc.ok {
			t.Fatalf("%s: validatePlan error = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// TestServicePlannerSlotRules covers the planner's placement and refusals
// without the store: pruning and occupied slots are TRANSITION_BUSY,
// quarantine and unresolved recovery refuse admission at row 7 but allow STOP.
func TestServicePlannerSlotRules(t *testing.T) {
	t.Parallel()
	svc, err := NewService(ServiceOptions{Store: &managerStore{}, Recovery: &fakeRecovery{}, Incarnation: "inc1",
		Drivers: []ServiceInstance{{InstanceID: "fam.a", Executor: &fakeExec{}}}})
	if err != nil {
		t.Fatal(err)
	}
	d := svc.drivers["fam.a"]
	plan := func(in PlanInput) (SlotPlan, *Refusal, bool, bool) {
		var planned, fence bool
		p, r := svc.planner(d, &planned, &fence)(in)
		if planned {
			d.deciding--
		}
		return p, r, planned, fence
	}
	start := &Transition{Kind: KindStart, DesiredRevision: 2, Sequence: 1, Phase: PhaseActive, Epoch: "e.1", Generation: 1, NativeClosePending: true, SemanticCleanupPending: true}
	restart := &Transition{Kind: KindRestart, DesiredRevision: 3, Sequence: 2, Phase: PhaseActive}
	doneRestart := &Transition{Kind: KindRestart, DesiredRevision: 3, Sequence: 2, Phase: PhaseCleanup}
	if p, r, _, fence := plan(PlanInput{Method: MethodStop, Current: start}); r != nil || p.Reserved == nil || p.Reserved.Kind != KindStop || !fence {
		t.Fatalf("STOP behind START = %+v %v fence=%v", p, r, fence)
	}
	if _, r, _, _ := plan(PlanInput{Method: MethodStop, Current: restart}); r == nil || r.Code != RefusalTransitionBusy || r.Row != 8 {
		t.Fatalf("request during executing RESTART = %v", r)
	}
	if p, r, _, _ := plan(PlanInput{Method: MethodStop, Current: doneRestart}); r != nil || p.Reserved == nil {
		t.Fatalf("request behind a terminal RESTART = %+v %v", p, r)
	}
	if _, r, _, _ := plan(PlanInput{Method: MethodStart, Current: start, Reserved: &Transition{Kind: KindStop, DesiredRevision: 3, Sequence: 2, Phase: PhasePending}}); r == nil || r.Code != RefusalTransitionBusy {
		t.Fatalf("both slots occupied = %v", r)
	}
	d.untyped = &attempt{corr: LifecycleCorrelation{InstanceID: "fam.a", Epoch: "inc1.fam.a.1", Generation: 2}, cancel: func() {}}
	if p, r, _, fence := plan(PlanInput{Method: MethodRestart}); r != nil || p.Current == nil || p.Current.Kind != KindCancel ||
		p.Current.Generation != 2 || p.Reserved.Kind != KindRestart || !fence {
		t.Fatalf("RESTART during untyped attempt = %+v %v", p, r)
	}
	d.untyped = nil
	d.pruning = true
	if _, r, planned, _ := plan(PlanInput{Method: MethodStart, Current: doneRestart}); r == nil || r.Code != RefusalTransitionBusy || planned {
		t.Fatalf("decision during an uncommitted prune = %v", r)
	}
	if p, r, _, _ := plan(PlanInput{Method: MethodStop}); r != nil || p.Current == nil {
		t.Fatalf("decision after the prune committed = %+v %v", p, r)
	}
	d.pruning = false
	for _, flag := range []*bool{&d.quarantined, &d.recoveryBlocked} {
		*flag = true
		for _, m := range []Method{MethodStart, MethodRestart} {
			if _, r, _, _ := plan(PlanInput{Method: m}); r == nil || r.Code != RefusalFailedPrecondition || r.Row != 7 {
				t.Fatalf("%s while blocked = %v", m, r)
			}
		}
		if _, r, _, _ := plan(PlanInput{Method: MethodStop}); r != nil {
			t.Fatalf("STOP while blocked refused: %v", r)
		}
		*flag = false
	}
}

// TestServiceWorkUpdateGuard closes the correlation and obligation residuals
// for every work update the service writes. Each case varies one field of a
// stored transition; the comment names the mutation of checkWorkUpdate it
// catches.
func TestServiceWorkUpdateGuard(t *testing.T) {
	t.Parallel()
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	stored := Transition{Kind: KindStart, DesiredRevision: 5, Sequence: 3, Phase: PhaseActive, Epoch: "inc1.fam.a.1",
		Generation: 2, NativeClosePending: true, SemanticCleanupPending: true, AttemptsUsed: 2, DeadlineUTC: deadline}
	cleared := stored
	cleared.NativeClosePending, cleared.SemanticCleanupPending = false, false
	pending := Transition{Kind: KindStop, DesiredRevision: 6, Sequence: 4, Phase: PhasePending}
	with := func(base Transition, fn func(*Transition)) Transition { fn(&base); return base }
	cases := []struct {
		name   string
		stored Transition
		next   Transition
		ok     bool
	}{
		{"unchanged", stored, stored, true},
		// identity: dropping any sameIdentity comparison lets a stale worker rewrite another transition.
		{"kind", stored, with(stored, func(t *Transition) { t.Kind = KindRestart }), false},
		{"desired revision", stored, with(stored, func(t *Transition) { t.DesiredRevision = 6 }), false},
		{"sequence", stored, with(stored, func(t *Transition) { t.Sequence = 4 }), false},
		// phase: dropping the rank check lets a stale update move CLEANUP back to ACTIVE.
		{"phase backwards", stored, with(stored, func(t *Transition) { t.Phase = PhasePending }), false},
		{"phase forwards", stored, with(stored, func(t *Transition) { t.Phase = PhaseCleanup }), true},
		{"phase unknown", stored, with(stored, func(t *Transition) { t.Phase = "PAUSED" }), false},
		// attempts and deadline: dropping either check renews the bounded budget.
		{"attempts lowered", stored, with(stored, func(t *Transition) { t.AttemptsUsed = 1 }), false},
		{"attempts raised", stored, with(stored, func(t *Transition) { t.AttemptsUsed = 3 }), true},
		{"deadline extended", stored, with(stored, func(t *Transition) { t.DeadlineUTC = deadline.Add(time.Second) }), false},
		{"deadline removed", stored, with(stored, func(t *Transition) { t.DeadlineUTC = time.Time{} }), false},
		{"deadline shortened", stored, with(stored, func(t *Transition) { t.DeadlineUTC = deadline.Add(-time.Second) }), true},
		// epoch: allowing removal or early replacement resumes cleanup against the wrong correlation.
		{"epoch removed", stored, with(stored, func(t *Transition) { t.Epoch = "" }), false},
		{"epoch replaced with obligations pending", stored, with(stored, func(t *Transition) { t.Epoch = "inc2.fam.a.1" }), false},
		{"epoch replaced after obligations cleared", cleared, with(cleared, func(t *Transition) {
			t.Epoch = "inc2.fam.a.1"
			t.Generation = 1
			t.NativeClosePending = true
			t.SemanticCleanupPending = true
		}), true},
		// generation: allowing a decrease reuses a fenced generation.
		{"generation lowered", stored, with(stored, func(t *Transition) { t.Generation = 1 }), false},
		{"generation raised", stored, with(stored, func(t *Transition) { t.Generation = 3 }), true},
		// obligations: allowing false->true repeats a completed native close or semantic cleanup.
		{"native close re-activated", with(stored, func(t *Transition) { t.NativeClosePending = false }), stored, false},
		{"semantic cleanup re-activated", with(stored, func(t *Transition) { t.SemanticCleanupPending = false }), stored, false},
		{"native close cleared", stored, with(stored, func(t *Transition) { t.NativeClosePending = false }), true},
		{"semantic cleanup cleared", stored, with(stored, func(t *Transition) { t.SemanticCleanupPending = false }), true},
		// initial assignment at PENDING -> ACTIVE incurs both obligations.
		{"initial assignment", pending, with(pending, func(t *Transition) {
			t.Phase = PhaseActive
			t.Epoch, t.Generation = "inc1.fam.a.2", 1
			t.NativeClosePending, t.SemanticCleanupPending = true, true
			t.DeadlineUTC = deadline
		}), true},
	}
	for _, tc := range cases {
		s, n := tc.stored, tc.next
		if err := checkWorkUpdate(&s, &n); (err == nil) != tc.ok {
			t.Fatalf("%s: checkWorkUpdate error = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// --- review round: durability of withdrawal, bounded cleanup, races ---

// hookStore wraps a Store with test-controlled faults.
type hookStore struct {
	Store
	failAdvance atomic.Bool
	driverHook  atomic.Pointer[func(InstanceID) ControlRecord]
	advanceHook atomic.Pointer[func()]
}

func (h *hookStore) AdvanceCatalog(ctx context.Context) (CatalogRevision, error) {
	if fn := h.advanceHook.Swap(nil); fn != nil {
		(*fn)()
	}
	if h.failAdvance.Load() {
		return 0, errors.New("synthetic advance failure")
	}
	return h.Store.AdvanceCatalog(ctx)
}

func (h *hookStore) Driver(id InstanceID) (ControlRecord, error) {
	rec, err := h.Store.Driver(id)
	if fn := h.driverHook.Swap(nil); fn != nil && err == nil {
		(*fn)(id)
	}
	return rec, err
}

// TestServiceRestartPersistsPredecessorBeforeStop: while the executor is
// withdrawing the admitted generation for a RESTART, the durable slot already
// carries that generation's correlation and both cleanup obligations.
func TestServiceRestartPersistsPredecessorBeforeStop(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id))})
	f.waitSettled(id, ObservedRunning)
	live := f.execs[id].snapshot()[0].Corr
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.execs[id].setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
		once.Do(func() { close(entered) })
		<-release
		return StopOutcome{NativeClosed: true, SemanticCleaned: true}
	})
	f.mustDecide(MethodRestart, id)
	<-entered
	cur := f.record(id).Current
	if cur == nil || cur.Kind != KindRestart || cur.Phase != PhaseActive || cur.Epoch != live.Epoch ||
		cur.Generation != live.Generation || !cur.NativeClosePending || !cur.SemanticCleanupPending || cur.DeadlineUTC.IsZero() {
		t.Fatalf("slot during withdrawal = %+v, want the predecessor correlation with both obligations", cur)
	}
	close(release)
	f.waitSettled(id, ObservedRunning)
}

// TestServiceCleanupStopIsBounded: a context-respecting executor cannot block
// cleanup past the operation budget, and an unfinished semantic cleanup is
// retried only a bounded number of times per series.
func TestServiceCleanupStopIsBounded(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		opts: func(o *ServiceOptions) { o.OperationBudget = 100 * time.Millisecond; o.MaxAttempts = 3 }})
	f.waitSettled(id, ObservedRunning)
	var hasDeadline atomic.Bool
	hasDeadline.Store(true)
	f.execs[id].setStop(func(ctx context.Context, _ LifecycleCorrelation) StopOutcome {
		if _, ok := ctx.Deadline(); !ok {
			hasDeadline.Store(false)
		}
		<-ctx.Done()
		return StopOutcome{NativeClosed: true}
	})
	f.mustDecide(MethodStop, id)
	waitFor(t, "cleanup stalled", func() bool { return f.status(id).ObservedState == ObservedFailed })
	time.Sleep(300 * time.Millisecond)
	if !hasDeadline.Load() {
		t.Fatal("Stop ran without a deadline")
	}
	// The first Stop consumed the whole nonrenewable budget, so no retry
	// follows; the attempt limit (3) bounds a series with budget left.
	if n := f.execs[id].count("stop"); n != 1 {
		t.Fatalf("semantic cleanup ran %d times, want 1 once the deadline is spent", n)
	}
	if cur := f.record(id).Current; cur == nil || !cur.SemanticCleanupPending || cur.NativeClosePending {
		t.Fatalf("unfinished cleanup not retained: %+v", cur)
	}
}

// TestServiceStopBeforeUntypedInstallClosesGeneration: a STOP accepted after
// the reconciler read its (empty) slots but before the process-start attempt
// was installed must prevent that attempt from starting at all.
func TestServiceStopBeforeUntypedInstallClosesGeneration(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var hs *hookStore
	reached, release := make(chan struct{}), make(chan struct{})
	hook := func(InstanceID) ControlRecord {
		close(reached)
		<-release
		return ControlRecord{}
	}
	f := newFixture(t, svcOpts{drivers: enabled(string(id)), noStart: true,
		wrap: func(s Store) Store { hs = &hookStore{Store: s}; hs.driverHook.Store(&hook); return hs }})
	if err := f.svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-reached
	f.mustDecide(MethodStop, id)
	close(release)
	f.waitSettled(id, ObservedStopped)
	if n := f.execs[id].count("start"); n != 0 {
		t.Fatalf("a start ran after the STOP was accepted (%d)", n)
	}
}

// TestServiceLateStartSuccessIsNotAdmitted: a START that returns nil after
// its attempt deadline is withdrawn and never published RUNNING, for the
// typed and the process-start path.
func TestServiceLateStartSuccessIsNotAdmitted(t *testing.T) {
	t.Parallel()
	late := func(ctx context.Context, _ LifecycleCorrelation) error {
		<-ctx.Done()
		return nil
	}
	assertNeverRunning := func(t *testing.T, f *svcFixture, id InstanceID, stopsWant int) {
		waitFor(t, "failed", func() bool { return f.status(id).ObservedState == ObservedFailed })
		if st := f.status(id); st.Reason != ReasonRetryExhausted {
			t.Fatalf("status = %+v", st)
		}
		waitFor(t, "late generation withdrawn", func() bool { return f.execs[id].count("stop") >= stopsWant })
		if st := f.status(id); st.ObservedState == ObservedRunning {
			t.Fatalf("late success published RUNNING: %+v", st)
		}
	}
	id := InstanceID("fam.alpha")
	t.Run("untyped", func(t *testing.T) {
		f := newFixture(t, svcOpts{drivers: enabled(string(id)),
			opts:  func(o *ServiceOptions) { o.OperationBudget = 100 * time.Millisecond },
			setup: func(f *svcFixture) { f.execs[id].setStart(late) }})
		assertNeverRunning(t, f, id, 1)
	})
	t.Run("typed", func(t *testing.T) {
		f := newFixture(t, svcOpts{drivers: enabled(string(id)),
			opts: func(o *ServiceOptions) { o.OperationBudget = 200 * time.Millisecond }})
		f.waitSettled(id, ObservedRunning)
		f.execs[id].setStart(late)
		f.mustDecide(MethodRestart, id)
		assertNeverRunning(t, f, id, 2)
	})
}

// TestServiceFailedObservationPublicationIsRetried: a transient catalog
// failure neither loses the transition nor its event; both appear in order
// once the store recovers, with no further action.
func TestServiceFailedObservationPublicationIsRetried(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var hs *hookStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		wrap: func(s Store) Store { hs = &hookStore{Store: s}; return hs }})
	f.waitSettled(id, ObservedRunning)
	cursor := f.svc.List().EventCursor
	hs.failAdvance.Store(true)
	f.mustDecide(MethodStop, id)
	f.waitIdle(id)
	time.Sleep(20 * time.Millisecond)
	if st := f.status(id); st.ObservedState == ObservedStopped {
		t.Fatalf("transition published despite failing catalog: %+v", st)
	}
	hs.failAdvance.Store(false)
	f.waitObserved(id, ObservedStopped)
	page := f.svc.EventsSince(cursor)
	last := page.Events[len(page.Events)-1]
	if page.Resync || last.Kind != EventObservedChanged || last.ObservedState != ObservedStopped {
		t.Fatalf("events after recovery = %+v", page)
	}
	for i := 1; i < len(page.Events); i++ {
		if page.Events[i].Cursor != page.Events[i-1].Cursor+1 || page.Events[i].CatalogRevision < page.Events[i-1].CatalogRevision {
			t.Fatalf("events out of order: %+v", page.Events)
		}
	}
}

// TestServiceEventsFollowTheirView: a reader woken by an event always finds a
// list at least as new as the event's catalog revision.
func TestServiceEventsFollowTheirView(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id))})
	f.waitSettled(id, ObservedRunning)
	stop := make(chan struct{})
	bad := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		cursor := f.svc.List().EventCursor
		for {
			notify := f.svc.EventNotify()
			page := f.svc.EventsSince(cursor)
			for _, e := range page.Events {
				if got := f.svc.List(); got.Revision < e.CatalogRevision || got.EventCursor < e.Cursor {
					select {
					case bad <- fmt.Sprintf("event %+v seen before list %d/%d", e, got.Revision, got.EventCursor):
					default:
					}
				}
			}
			cursor = page.Next
			select {
			case <-notify:
			case <-stop:
				return
			}
		}
	}()
	for i := 0; i < 20; i++ {
		f.mustDecide(MethodRestart, id)
		f.waitSettled(id, ObservedRunning)
	}
	close(stop)
	<-done
	select {
	case msg := <-bad:
		t.Fatal(msg)
	default:
	}
}

// TestServiceFatalStartLeavesServiceNotRunning: a Start refused by startup
// validation accepts no decision afterwards.
func TestServiceFatalStartLeavesServiceNotRunning(t *testing.T) {
	t.Parallel()
	a, b := InstanceID("fam.alpha"), InstanceID("fam.beta")
	f := newFixture(t, svcOpts{drivers: enabled(string(a), string(b)), noStart: true,
		opts: func(o *ServiceOptions) { o.Drivers = o.Drivers[:1] }})
	if err := f.svc.Start(context.Background()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Start error = %v", err)
	}
	before := f.probe()
	_, err := f.svc.Control(deadlineCtx(t), MethodStart, body(a, 1))
	refusalOf(t, err, RefusalUnavailable, 3)
	f.assertUnchanged("decision after fatal start", before)
}

// TestServiceInitialViewIsRetried: a transient failure while publishing the
// first view is retried instead of leaving the constructor's empty catalog.
func TestServiceInitialViewIsRetried(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var hs *hookStore
	f := newFixture(t, svcOpts{drivers: []ControlInstance{{InstanceID: id}}, noStart: true,
		wrap: func(s Store) Store { hs = &hookStore{Store: s}; return hs }})
	hs.failAdvance.Store(true)
	if err := f.svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(f.svc.List().Drivers); n != 0 {
		t.Fatalf("view published despite failing catalog: %d", n)
	}
	hs.failAdvance.Store(false)
	waitFor(t, "initial view", func() bool { return len(f.svc.List().Drivers) == 1 })
}

// TestServiceUntypedLateSuccessKeepsUnfinishedCleanupBlocking: a process-start
// success after its deadline whose withdrawal closes natively but leaves
// semantic cleanup unfinished is not discarded: the driver is FAILED with a
// CLEANUP_STALLED diagnostic and a later RESTART is refused at row 7. The
// late withdrawal stays bounded by the attempt's original deadline: its one
// Stop receives an already-expired context and is never retried with a fresh
// budget.
func TestServiceUntypedLateSuccessKeepsUnfinishedCleanupBlocking(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var renewed atomic.Bool
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		opts: func(o *ServiceOptions) { o.OperationBudget = 100 * time.Millisecond; o.MaxAttempts = 3 },
		setup: func(f *svcFixture) {
			f.execs[id].setStart(func(ctx context.Context, _ LifecycleCorrelation) error {
				<-ctx.Done()
				return nil
			})
			f.execs[id].setStop(func(ctx context.Context, _ LifecycleCorrelation) StopOutcome {
				if ctx.Err() == nil {
					renewed.Store(true)
				}
				return StopOutcome{NativeClosed: true}
			})
		}})
	waitFor(t, "failed", func() bool { return f.status(id).ObservedState == ObservedFailed })
	waitFor(t, "cleanup stalled diagnostic", func() bool { return hasDiagnostic(f.svc.Diagnostics(id), "CLEANUP_STALLED") })
	time.Sleep(200 * time.Millisecond)
	if n := f.execs[id].count("stop"); n != 1 {
		t.Fatalf("late withdrawal ran Stop %d times, want 1 bounded by the original deadline", n)
	}
	if renewed.Load() {
		t.Fatal("late withdrawal Stop received a renewed deadline")
	}
	_, err := f.decide(MethodRestart, id)
	refusalOf(t, err, RefusalFailedPrecondition, 7)
}

func hasDiagnostic(diags []Diagnostic, outcome string) bool {
	for _, d := range diags {
		if d.Outcome == outcome {
			return true
		}
	}
	return false
}

// TestServiceAcceptedEventCarriesObservedState: DESIRED_ACCEPTED reports the
// driver's observed state at acceptance, not an empty one.
func TestServiceAcceptedEventCarriesObservedState(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id))})
	f.waitSettled(id, ObservedRunning)
	cursor := f.svc.events.cursor()
	f.mustDecide(MethodStop, id)
	for _, e := range f.svc.EventsSince(cursor).Events {
		if e.Kind == EventDesiredAccepted {
			if e.ObservedState == "" {
				t.Fatalf("accepted event has no observed state: %+v", e)
			}
			return
		}
	}
	t.Fatal("no accepted event")
}

// TestServiceCleanupAttemptsArePersisted: each cleanup Stop is counted in the
// slot's persisted AttemptsUsed so process recovery cannot renew it.
func TestServiceCleanupAttemptsArePersisted(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		opts: func(o *ServiceOptions) { o.MaxAttempts = 3 }})
	f.waitSettled(id, ObservedRunning)
	f.execs[id].setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
		return StopOutcome{NativeClosed: true}
	})
	f.mustDecide(MethodStop, id)
	waitFor(t, "cleanup stalled", func() bool { return f.status(id).ObservedState == ObservedFailed })
	if cur := f.record(id).Current; cur == nil || cur.AttemptsUsed != 3 {
		t.Fatalf("persisted cleanup attempts = %+v, want 3", cur)
	}
}

// TestServiceCleanupRetryNeverRenewsExpiredDeadline: once the persisted
// deadline has passed, an unfinished semantic cleanup is not retried with a
// fresh budget; the obligation stays durable.
func TestServiceCleanupRetryNeverRenewsExpiredDeadline(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		opts: func(o *ServiceOptions) { o.OperationBudget = 100 * time.Millisecond }})
	f.waitSettled(id, ObservedRunning)
	f.execs[id].setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
		time.Sleep(150 * time.Millisecond) // outlives the slot's deadline
		return StopOutcome{NativeClosed: true}
	})
	f.mustDecide(MethodStop, id)
	waitFor(t, "cleanup stalled", func() bool { return f.status(id).ObservedState == ObservedFailed })
	time.Sleep(400 * time.Millisecond)
	if n := f.execs[id].count("stop"); n != 1 {
		t.Fatalf("Stop ran %d times, want 1 (no fresh budget after the deadline)", n)
	}
	if cur := f.record(id).Current; cur == nil || !cur.SemanticCleanupPending {
		t.Fatalf("unfinished cleanup not retained: %+v", cur)
	}
}

// TestServiceDeferredObservationsMatchTheirView: transitions queued while
// publication fails are coalesced per driver, so no replayed event reports an
// observation the view at its catalog revision no longer shows.
func TestServiceDeferredObservationsMatchTheirView(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var hs *hookStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		wrap: func(s Store) Store { hs = &hookStore{Store: s}; return hs }})
	f.waitSettled(id, ObservedRunning)
	cursor := f.svc.List().EventCursor
	hs.failAdvance.Store(true)
	f.mustDecide(MethodStop, id)
	f.waitIdle(id)
	f.mustDecide(MethodStart, id)
	f.waitIdle(id)
	hs.failAdvance.Store(false)
	f.waitObserved(id, ObservedRunning)
	view := f.svc.List()
	for _, e := range f.svc.EventsSince(cursor).Events {
		if e.Kind == EventObservedChanged && e.CatalogRevision == view.Revision && e.ObservedState != f.status(id).ObservedState {
			t.Fatalf("event %+v reports an observation the view at its revision does not show", e)
		}
	}
}

// TestServicePendingEventOverflowForcesResync: events discarded before they
// received a cursor cannot leave a silent gap; readers positioned before it
// are told to resynchronise.
func TestServicePendingEventOverflowForcesResync(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var hs *hookStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		wrap: func(s Store) Store { hs = &hookStore{Store: s}; return hs }})
	f.waitSettled(id, ObservedRunning)
	cursor := f.svc.List().EventCursor
	hs.failAdvance.Store(true)
	for i := 0; i < maxPendingEvents+1; i++ {
		f.svc.deferPublication(&Event{Kind: EventDesiredAccepted, InstanceID: id})
	}
	hs.failAdvance.Store(false)
	waitFor(t, "pending events published", func() bool { return f.svc.EventsSince(cursor).Next > cursor })
	if page := f.svc.EventsSince(cursor); !page.Resync {
		t.Fatalf("overflow left a silent gap: %+v", page)
	}
}

// --- author round: readback resolver, nonrenewable cleanup, views, cursor ---

// TestServiceResolvesAmbiguousCommitWithoutFurtherRequest: a decision whose
// commit is ambiguous (directory fsync failure after the rename) closes
// admission. The service runs the bounded readback itself: with no further
// request, admission reopens, the adopted decision appears in List and the
// reconciler executes it, within the documented bound.
func TestServiceResolvesAmbiguousCommitWithoutFurtherRequest(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id))})
	f.waitSettled(id, ObservedRunning)
	before := f.status(id)
	f.quiesce()
	f.hooks.mu.Lock()
	f.hooks.failDirSync = 1
	f.hooks.mu.Unlock()
	start := time.Now()
	_, err := f.decide(MethodStop, id)
	refusalOf(t, err, RefusalUnavailable, 3)
	waitFor(t, "ambiguous decision adopted without a further request", func() bool {
		st := f.status(id)
		return f.ms.AdmissionOpen() && st.DesiredState == DesiredStopped && st.DesiredRevision > before.DesiredRevision &&
			st.ObservedState == ObservedStopped
	})
	// Bound: the first readback runs at once; the series is at most
	// MaxAttempts readbacks spaced by RetryDelay (2 ms here).
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("ambiguous commit resolved after %v, beyond the bound", elapsed)
	}
	if n := f.execs[id].count("stop"); n != 1 {
		t.Fatalf("adopted STOP executed %d times, want 1", n)
	}
	if got := f.fileDesired(id); got != string(DesiredStopped) {
		t.Fatalf("durable desired = %q", got)
	}
}

// TestServiceExpiredCleanupDeadlineIsNeverRenewed: an ACTIVE slot recovered
// after its persisted deadline gets no fresh budget. Its one cleanup Stop
// receives an already-expired context; the unfinished obligation ends FAILED
// with CLEANUP_STALLED, blocks START and RESTART (row 7), is never retried
// and keeps the original deadline durably.
func TestServiceExpiredCleanupDeadlineIsNeverRenewed(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	past := "2001-01-01T00:00:00Z"
	var renewed atomic.Bool
	f := newFixture(t, svcOpts{
		drivers: enabled(string(id)),
		state: controlState(1, 3, map[string]string{
			string(id): recordJSON(DesiredRunning, 1, 1, transitionJSON(KindStart, 1, 1, PhaseActive, "old.fam.alpha.1", 1, true, true, 1, past)),
		}, ""),
		incarnation: "inc2",
		setup: func(f *svcFixture) {
			f.execs[id].setStop(func(ctx context.Context, _ LifecycleCorrelation) StopOutcome {
				if ctx.Err() == nil {
					renewed.Store(true)
				}
				return StopOutcome{NativeClosed: true}
			})
		},
	})
	waitFor(t, "cleanup stalled", func() bool {
		return f.status(id).ObservedState == ObservedFailed && hasDiagnostic(f.svc.Diagnostics(id), "CLEANUP_STALLED")
	})
	time.Sleep(200 * time.Millisecond)
	if renewed.Load() {
		t.Fatal("cleanup Stop received a renewed deadline")
	}
	if n := f.execs[id].count("stop"); n != 1 {
		t.Fatalf("expired cleanup ran Stop %d times, want 1", n)
	}
	if n := f.execs[id].count("start"); n != 0 {
		t.Fatalf("a start ran over unfinished cleanup (%d)", n)
	}
	cur := f.record(id).Current
	want, _ := time.Parse(time.RFC3339, past)
	if cur == nil || !cur.SemanticCleanupPending || cur.NativeClosePending || !cur.DeadlineUTC.Equal(want) {
		t.Fatalf("slot after stalled cleanup = %+v, want the obligation and the original deadline", cur)
	}
	_, err := f.decide(MethodRestart, id)
	refusalOf(t, err, RefusalFailedPrecondition, 7)
}

// TestServiceCleanupAttemptChargedBeforeStop: the cleanup attempt is durable
// before the executor's Stop runs, so process loss during Stop cannot grant
// an extra attempt on recovery.
func TestServiceCleanupAttemptChargedBeforeStop(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id))})
	f.waitSettled(id, ObservedRunning)
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock) // runs before the fixture's Close, even after a failure
	f.execs[id].setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
		once.Do(func() { close(entered) })
		<-release
		return StopOutcome{NativeClosed: true, SemanticCleaned: true}
	})
	f.mustDecide(MethodStop, id)
	<-entered
	cur := f.record(id).Current
	if cur == nil || cur.AttemptsUsed != 1 || !cur.NativeClosePending || !cur.SemanticCleanupPending {
		t.Fatalf("slot while Stop runs = %+v, want the attempt already charged with both obligations pending", cur)
	}
	unblock()
	f.waitSettled(id, ObservedStopped)
}

// roStore reports the committed catalog as read-only (a LOCKED or
// persistence-blocked restart).
type roStore struct{ Store }

func (r roStore) Catalog() (ControlCatalog, error) {
	c, err := r.Store.Catalog()
	c.ReadOnly = true
	return c, err
}

// TestServiceReadOnlyViewKeepsStoredRevisions: a read-only catalog at Start
// withholds every driver as FAILED but keeps the stored catalog revision and
// each stored desired revision unchanged, never zero.
func TestServiceReadOnlyViewKeepsStoredRevisions(t *testing.T) {
	t.Parallel()
	a, b := InstanceID("fam.a"), InstanceID("fam.b")
	f := newFixture(t, svcOpts{
		drivers: enabled(string(a), string(b)),
		state: controlState(7, 11, map[string]string{
			string(a): recordJSON(DesiredRunning, 5, 0, ""),
			string(b): recordJSON(DesiredStopped, 7, 0, ""),
		}, ""),
		wrap:    func(s Store) Store { return roStore{s} },
		noStart: true,
	})
	stored, err := f.ms.Catalog()
	if err != nil || stored.CatalogRevision == 0 {
		t.Fatalf("stored catalog = %+v, %v", stored, err)
	}
	err = f.svc.Start(context.Background())
	refusalOf(t, err, RefusalFailedPrecondition, 3)
	view := f.svc.List()
	if view.Revision != stored.CatalogRevision {
		t.Fatalf("read-only view revision = %d, want stored %d", view.Revision, stored.CatalogRevision)
	}
	want := map[InstanceID]DesiredRevision{}
	for _, rec := range stored.Drivers {
		want[rec.InstanceID] = rec.DesiredRevision
	}
	for _, e := range view.Drivers {
		if e.Revision != stored.CatalogRevision || e.DesiredRevision != want[e.InstanceID] || e.DesiredRevision == 0 ||
			e.ObservedState != ObservedFailed {
			t.Fatalf("read-only entry = %+v, want stored revisions %d/%d", e, stored.CatalogRevision, want[e.InstanceID])
		}
	}
}

// TestServicePublishedCursorEventsExist: a reader that resyncs at a published
// EventCursor always finds that cursor's events, deterministically at the
// event log and under concurrent publications.
func TestServicePublishedCursorEventsExist(t *testing.T) {
	t.Parallel()
	t.Run("log", func(t *testing.T) {
		l := newEventLog(8)
		got := make(chan EventPage, 1)
		l.publish([]Event{{Kind: EventObservedChanged}, {Kind: EventObservedChanged}}, false, func(cursor uint64) {
			// A reader that saw the exposed cursor reads right away; it
			// must wait for the publication and then find both events.
			go func() { got <- l.since(cursor - 2) }()
		})
		page := <-got
		if page.Resync || len(page.Events) != 2 || page.Next != 2 {
			t.Fatalf("resync at the exposed cursor = %+v", page)
		}
	})
	t.Run("service", func(t *testing.T) {
		id := InstanceID("fam.alpha")
		f := newFixture(t, svcOpts{drivers: enabled(string(id))})
		f.waitSettled(id, ObservedRunning)
		stop := make(chan struct{})
		bad := make(chan string, 1)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case <-stop:
					return
				default:
				}
				view := f.svc.List()
				if view.EventCursor == 0 {
					continue
				}
				page := f.svc.EventsSince(view.EventCursor - 1)
				if page.Resync || len(page.Events) == 0 || page.Events[0].Cursor != view.EventCursor {
					select {
					case bad <- fmt.Sprintf("cursor %d published before its event: %+v", view.EventCursor, page):
					default:
					}
					return
				}
			}
		}()
		for i := 0; i < 15; i++ {
			f.mustDecide(MethodRestart, id)
			f.waitSettled(id, ObservedRunning)
		}
		close(stop)
		<-done
		select {
		case msg := <-bad:
			t.Fatal(msg)
		default:
		}
	})
}

// TestServicePublishCoalescesSupersededObservations: when an earlier
// observation's publication failed and a newer observation of the same
// driver is published successfully, the superseded event is not stamped with
// the newer view's revision.
func TestServicePublishCoalescesSupersededObservations(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var hs *hookStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		wrap: func(s Store) Store { hs = &hookStore{Store: s}; return hs }})
	f.waitSettled(id, ObservedRunning)
	cursor := f.svc.List().EventCursor
	d := f.svc.drivers[id]
	hs.failAdvance.Store(true)
	stopping := observation{state: ObservedStopping, reason: ReasonStopRequested}
	if f.svc.stageObservation(id, stopping) {
		f.svc.commitObserved(d, stopping)
	}
	stopped := observation{state: ObservedStopped, reason: ReasonOperatorStopped}
	f.svc.stageObservation(id, stopped)
	hs.failAdvance.Store(false)
	f.svc.commitObserved(d, stopped)
	f.waitObserved(id, ObservedStopped)
	page := f.svc.EventsSince(cursor)
	sawStopped := false
	for _, e := range page.Events {
		if e.Kind != EventObservedChanged {
			continue
		}
		if e.ObservedState == ObservedStopping {
			t.Fatalf("superseded STOPPING event emitted with revision %d: %+v", e.CatalogRevision, page.Events)
		}
		sawStopped = sawStopped || e.ObservedState == ObservedStopped
	}
	if page.Resync || !sawStopped {
		t.Fatalf("events after publication = %+v", page)
	}
}

// TestServiceSynthesizedCancelInheritsUntypedBudget: a STOP accepted during an
// untyped BACKOFF synthesizes the cancellation with the attempt's own
// nonrenewable deadline and attempts already used, never a fresh budget.
func TestServiceSynthesizedCancelInheritsUntypedBudget(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	entered := make(chan LifecycleCorrelation, 16)
	f := newFixture(t, svcOpts{
		drivers: enabled(string(id)),
		opts:    func(o *ServiceOptions) { o.RetryDelay = func(int) time.Duration { return time.Hour } },
		setup: func(f *svcFixture) {
			f.execs[id].setStart(func(ctx context.Context, c LifecycleCorrelation) error {
				entered <- c
				return retryErr{"warming"}
			})
		},
	})
	<-entered
	f.waitObserved(id, ObservedBackoff)
	time.Sleep(60 * time.Millisecond)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.execs[id].setStop(func(ctx context.Context, _ LifecycleCorrelation) StopOutcome {
		<-release
		return StopOutcome{NativeClosed: true, SemanticCleaned: true}
	})
	decided := time.Now()
	f.mustDecide(MethodStop, id)
	cur := f.record(id).Current
	if cur == nil || cur.Kind != KindCancel {
		t.Fatalf("cancellation not synthesized: %+v", cur)
	}
	if cur.AttemptsUsed < 1 {
		t.Fatalf("cancellation restarted the attempt count: %+v", cur)
	}
	if !cur.DeadlineUTC.Before(decided.Add(f.svc.budget).Add(-30 * time.Millisecond)) {
		t.Fatalf("cancellation got a renewed deadline %v (accepted %v)", cur.DeadlineUTC, decided)
	}
}

// TestServiceAmbiguousStopFencesUntypedBackoffAfterReadback: a STOP whose commit
// is ambiguous during an untyped BACKOFF is adopted by the readback; the
// attempt must not open another generation, and the cleanup closes the
// generation the cancellation slot names.
func TestServiceAmbiguousStopFencesUntypedBackoffAfterReadback(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var mu sync.Mutex
	var started []LifecycleCorrelation
	var stopped []LifecycleCorrelation
	entered := make(chan struct{}, 16)
	f := newFixture(t, svcOpts{
		drivers: enabled(string(id)),
		opts:    func(o *ServiceOptions) { o.RetryDelay = func(int) time.Duration { return 150 * time.Millisecond } },
		setup: func(f *svcFixture) {
			f.execs[id].setStart(func(ctx context.Context, c LifecycleCorrelation) error {
				mu.Lock()
				started = append(started, c)
				mu.Unlock()
				entered <- struct{}{}
				return retryErr{"warming"}
			})
			f.execs[id].setStop(func(ctx context.Context, c LifecycleCorrelation) StopOutcome {
				mu.Lock()
				stopped = append(stopped, c)
				mu.Unlock()
				return StopOutcome{NativeClosed: true, SemanticCleaned: true}
			})
		},
	})
	<-entered
	f.waitObserved(id, ObservedBackoff)
	f.hooks.mu.Lock()
	f.hooks.failDirSync = 1
	f.hooks.mu.Unlock()
	_, err := f.decide(MethodStop, id)
	refusalOf(t, err, RefusalUnavailable, 3)
	waitFor(t, "adopted STOP cleaned up", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(stopped) > 0 && f.status(id).DesiredState == DesiredStopped
	})
	time.Sleep(400 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	last := started[len(started)-1]
	for _, c := range stopped {
		if c.Epoch != last.Epoch || c.Generation != last.Generation {
			t.Fatalf("cleanup closed %+v but the last opened generation is %+v", c, last)
		}
	}
	if len(started) != 1 {
		t.Fatalf("attempt opened %d generations after the adopted STOP", len(started))
	}
}

// TestServiceSettleObligationsNeverRunsStopWithoutPersistedAttempt: a slot
// whose persisted attempts are spent runs no uncharged Stop, and a START that
// keeps failing leaves its own reserved attempt for the cleanup.
func TestServiceSettleObligationsNeverRunsStopWithoutPersistedAttempt(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		opts: func(o *ServiceOptions) { o.MaxAttempts = 3 }})
	f.waitSettled(id, ObservedRunning)
	stops := f.execs[id].count("stop")
	spent := Transition{Kind: KindStart, Phase: PhaseCleanup, Epoch: "e", Generation: 1,
		NativeClosePending: true, SemanticCleanupPending: true, AttemptsUsed: 4}
	next, err := f.svc.settleObligations(f.svc.drivers[id], spent)
	if err != nil || !next.NativeClosePending || !next.SemanticCleanupPending || next.AttemptsUsed != 4 {
		t.Fatalf("spent slot settled = %+v, %v", next, err)
	}
	if n := f.execs[id].count("stop"); n != stops {
		t.Fatalf("uncharged Stop ran %d times", n-stops)
	}

	f.mustDecide(MethodStop, id)
	f.waitSettled(id, ObservedStopped)
	f.execs[id].setStart(func(context.Context, LifecycleCorrelation) error { return retryErr{"never"} })
	startsBefore := f.execs[id].count("start")
	f.mustDecide(MethodStart, id)
	f.waitSettled(id, ObservedFailed)
	if n := f.execs[id].count("start") - startsBefore; n != 3 {
		t.Fatalf("START attempts = %d, want 3 (MaxAttempts counts start attempts; cleanup has its own reserved attempt)", n)
	}
}

// TestServiceAdoptedAmbiguousDecisionEmitsDesiredAccepted: a decision whose
// commit was ambiguous and later adopted by the readback is not silently
// absent from the event stream.
func TestServiceAdoptedAmbiguousDecisionEmitsDesiredAccepted(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id))})
	f.waitSettled(id, ObservedRunning)
	before := f.status(id)
	cursor := f.svc.List().EventCursor
	f.quiesce()
	f.hooks.mu.Lock()
	f.hooks.failDirSync = 1
	f.hooks.mu.Unlock()
	_, err := f.decide(MethodStop, id)
	refusalOf(t, err, RefusalUnavailable, 3)
	waitFor(t, "adopted decision event", func() bool {
		for _, e := range f.svc.EventsSince(cursor).Events {
			if e.Kind == EventDesiredAccepted && e.InstanceID == id &&
				e.DesiredState == DesiredStopped && e.DesiredRevision == before.DesiredRevision+1 {
				return true
			}
		}
		return false
	})
}

// TestServiceSupersededPendingDesiredEventIsNotStampedWithLaterView: a pending
// DESIRED_ACCEPTED that a later desired transition superseded before its view
// published is not stamped with that view; readers are told to resync.
func TestServiceSupersededPendingDesiredEventIsNotStampedWithLaterView(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var hs *hookStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		wrap: func(s Store) Store { hs = &hookStore{Store: s}; return hs }})
	f.waitSettled(id, ObservedRunning)
	f.mustDecide(MethodStop, id)
	f.waitSettled(id, ObservedStopped)
	cur := f.status(id)
	cursor := f.svc.List().EventCursor
	hs.failAdvance.Store(true)
	f.svc.deferPublication(&Event{Kind: EventDesiredAccepted, InstanceID: id,
		DesiredState: DesiredRunning, DesiredRevision: cur.DesiredRevision - 1})
	f.svc.deferPublication(&Event{Kind: EventDesiredAccepted, InstanceID: id,
		DesiredState: DesiredStopped, DesiredRevision: cur.DesiredRevision})
	hs.failAdvance.Store(false)
	waitFor(t, "pending events published", func() bool { return f.svc.EventsSince(cursor).Next > cursor })
	page := f.svc.EventsSince(cursor)
	if !page.Resync {
		t.Fatalf("superseded event left a silent gap: %+v", page)
	}
	for _, e := range page.Events {
		if e.Kind == EventDesiredAccepted && e.DesiredRevision != cur.DesiredRevision {
			t.Fatalf("superseded event stamped with a later view: %+v", e)
		}
	}
}

// --- author round 5: one rule for superseding decisions and their readback ---

// readbackStore wraps a Store: an armed readback gate holds ResolveReadback, and
// an armed afterDecide hook runs once after a store Decide that succeeded.
type readbackStore struct {
	Store
	readback    atomic.Pointer[chan struct{}]
	afterDecide atomic.Pointer[func()]
}

func (g *readbackStore) ResolveReadback(ctx context.Context) error {
	if gate := g.readback.Load(); gate != nil {
		select {
		case <-*gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return g.Store.ResolveReadback(ctx)
}

func (g *readbackStore) Decide(ctx context.Context, method Method, req Request, planner Planner) (DesiredView, error) {
	view, err := g.Store.Decide(ctx, method, req, planner)
	if err == nil {
		if fn := g.afterDecide.Swap(nil); fn != nil {
			(*fn)()
		}
	}
	return view, err
}

func gatedFixture(t *testing.T, ids ...string) (*svcFixture, *readbackStore) {
	t.Helper()
	var g *readbackStore
	f := newFixture(t, svcOpts{drivers: enabled(ids...),
		wrap: func(s Store) Store { g = &readbackStore{Store: s}; return g }})
	return f, g
}

// heldStart is a typed START whose one Start call blocks until result
// delivers its outcome or the attempt's context ends.
type heldStart struct {
	entered chan struct{}
	result  chan error
	mu      sync.Mutex
	ctx     context.Context
	corr    LifecycleCorrelation
}

func (h *heldStart) attempt() (context.Context, LifecycleCorrelation) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ctx, h.corr
}

// typedStartInFlight stops the driver, accepts a typed START and returns
// once its Start call is executing and the store is quiet.
func (f *svcFixture) typedStartInFlight(id InstanceID) *heldStart {
	f.t.Helper()
	f.waitSettled(id, ObservedRunning)
	f.mustDecide(MethodStop, id)
	f.waitSettled(id, ObservedStopped)
	h := &heldStart{entered: make(chan struct{}), result: make(chan error, 1)}
	var once sync.Once
	f.execs[id].setStart(func(ctx context.Context, c LifecycleCorrelation) error {
		h.mu.Lock()
		h.ctx, h.corr = ctx, c
		h.mu.Unlock()
		once.Do(func() { close(h.entered) })
		select {
		case err := <-h.result:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	f.mustDecide(MethodStart, id)
	<-h.entered
	f.quiesce()
	return h
}

// ambiguousStop decides a STOP whose commit is ambiguous (directory fsync
// failure after the rename) while the readback is held at the gate. It
// returns the gate and the state file as committed before the STOP.
func (f *svcFixture) ambiguousStop(g *readbackStore, id InstanceID) (gate chan struct{}, committed []byte) {
	f.t.Helper()
	committed = f.fileBytes()
	gate = make(chan struct{})
	g.readback.Store(&gate)
	f.hooks.mu.Lock()
	f.hooks.failDirSync = 1
	f.hooks.mu.Unlock()
	_, err := f.decide(MethodStop, id)
	refusalOf(f.t, err, RefusalUnavailable, 3)
	return gate, committed
}

// rejectOnReadback makes the pending readback find the committed namespace:
// the STOP did not persist.
func (f *svcFixture) rejectOnReadback(committed []byte) {
	f.t.Helper()
	if err := os.WriteFile(f.path, committed, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *svcFixture) waitDecisionSettled(id InstanceID) {
	f.t.Helper()
	d := f.svc.drivers[id]
	waitFor(f.t, "held decision settled", func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return f.ms.AdmissionOpen() && d.ambiguous == nil && d.deciding == 0
	})
}

func driverEvents(f *svcFixture, cursor uint64, id InstanceID) []Event {
	var out []Event
	for _, e := range f.svc.EventsSince(cursor).Events {
		if e.InstanceID == id {
			out = append(out, e)
		}
	}
	return out
}

func countOutcome(f *svcFixture, id InstanceID, outcome string) int {
	n := 0
	for _, d := range f.svc.Diagnostics(id) {
		if strings.Contains(d.Summary, "outcome="+outcome) {
			n++
		}
	}
	return n
}

// assertAcceptedThenCanceled checks the adopted outcome: exactly one
// DESIRED_ACCEPTED for the STOP, published before every observation the
// cancellation produced, and exactly one cancellation.
func assertAcceptedThenCanceled(t *testing.T, f *svcFixture, cursor uint64, id InstanceID, stopsBefore int) {
	t.Helper()
	events := driverEvents(f, cursor, id)
	final := f.status(id)
	accepted, firstObserved := -1, -1
	for i, e := range events {
		switch {
		case e.Kind == EventDesiredAccepted:
			if accepted >= 0 || e.DesiredState != DesiredStopped || e.DesiredRevision != final.DesiredRevision {
				t.Fatalf("acceptance events %+v, want exactly one for the STOP", events)
			}
			accepted = i
		case e.Kind == EventObservedChanged && firstObserved < 0:
			firstObserved = i
		}
		if e.Kind == EventObservedChanged && e.ObservedState == ObservedRunning {
			t.Fatalf("superseded START was admitted: %+v", events)
		}
	}
	if accepted < 0 || firstObserved < 0 || firstObserved < accepted {
		t.Fatalf("events %+v: acceptance must precede the cancellation", events)
	}
	if n := f.execs[id].count("stop") - stopsBefore; n != 1 {
		t.Fatalf("cancellation ran Stop %d times, want 1", n)
	}
	if n := countOutcome(f, id, "CANCELED"); n != 1 {
		t.Fatalf("CANCELED diagnostics = %d, want 1", n)
	}
}

// TestServiceRejectedSupersedingStopResumesTypedStart: a STOP behind an
// executing typed START whose commit is ambiguous does not cancel the START
// while unresolved. When the readback finds the committed namespace (the STOP
// did not persist), the START resumes with its attempt, budget and deadline
// unchanged and completes; no Stop runs and no event names the STOP.
func TestServiceRejectedSupersedingStopResumesTypedStart(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f, g := gatedFixture(t, string(id))
	h := f.typedStartInFlight(id)
	starts, stops := f.execs[id].count("start"), f.execs[id].count("stop")
	slot := *f.record(id).Current
	before := f.status(id)
	cursor := f.svc.List().EventCursor
	gate, committed := f.ambiguousStop(g, id)
	f.rejectOnReadback(committed)
	ctx, corr := h.attempt()
	if ctx.Err() != nil {
		t.Fatal("START canceled while its superseding STOP was unresolved")
	}
	close(gate)
	f.waitDecisionSettled(id)
	if rec := f.record(id); rec.Reserved != nil || rec.Current == nil || *rec.Current != slot {
		t.Fatalf("START slot after the rejected STOP = %+v, want unchanged %+v", rec.Current, slot)
	}
	if ctx.Err() != nil {
		t.Fatal("rejected STOP canceled the START")
	}
	h.result <- nil
	f.waitSettled(id, ObservedRunning)
	st := f.status(id)
	if st.DesiredState != DesiredRunning || st.DesiredRevision != before.DesiredRevision {
		t.Fatalf("desired after rejection = %s@%d, want %s@%d", st.DesiredState, st.DesiredRevision, before.DesiredState, before.DesiredRevision)
	}
	if n := f.execs[id].count("start"); n != starts {
		t.Fatalf("START made %d further attempts after the rejection", n-starts)
	}
	if n := f.execs[id].count("stop"); n != stops {
		t.Fatalf("rejected STOP ran Stop %d times", n-stops)
	}
	d := f.svc.drivers[id]
	d.mu.Lock()
	live := d.live
	d.mu.Unlock()
	if live == nil || live.corr != corr {
		t.Fatalf("admitted generation %+v, want the resumed attempt %+v", live, corr)
	}
	for _, e := range driverEvents(f, cursor, id) {
		if e.Kind == EventDesiredAccepted {
			t.Fatalf("rejected STOP left an event: %+v", e)
		}
	}
	if n := countOutcome(f, id, "CANCELED"); n != 0 {
		t.Fatalf("rejected STOP canceled the START (%d diagnostics)", n)
	}
}

// TestServiceAdoptedSupersedingStopAcceptsBeforeCanceling: when the readback
// adopts an ambiguous STOP behind an executing typed START, its
// DESIRED_ACCEPTED is published at adoption (never earlier, never twice) and
// only then is the START canceled, exactly once. A decision on another driver
// moved the namespace mark past this driver's revision, so adoption is read
// from the committed record, not from expected+1.
func TestServiceAdoptedSupersedingStopAcceptsBeforeCanceling(t *testing.T) {
	t.Parallel()
	alpha, beta := InstanceID("fam.alpha"), InstanceID("fam.beta")
	f, g := gatedFixture(t, string(alpha), string(beta))
	f.waitSettled(alpha, ObservedRunning)
	h := f.typedStartInFlight(beta)
	f.mustDecide(MethodStop, alpha)
	f.waitSettled(alpha, ObservedStopped)
	f.quiesce()
	before := f.status(beta)
	stops := f.execs[beta].count("stop")
	cursor := f.svc.List().EventCursor
	gate, _ := f.ambiguousStop(g, beta)
	time.Sleep(50 * time.Millisecond)
	if ctx, _ := h.attempt(); ctx.Err() != nil {
		t.Fatal("START canceled before the STOP was adopted")
	}
	if n := f.execs[beta].count("stop"); n != stops {
		t.Fatal("cancellation ran before the STOP was adopted")
	}
	if events := driverEvents(f, cursor, beta); len(events) != 0 {
		t.Fatalf("events before adoption: %+v", events)
	}
	close(gate)
	f.waitSettled(beta, ObservedStopped)
	if final := f.status(beta); final.DesiredRevision <= before.DesiredRevision+1 {
		t.Fatalf("test premise: adopted revision %d should skip past %d+1", final.DesiredRevision, before.DesiredRevision)
	}
	assertAcceptedThenCanceled(t, f, cursor, beta, stops)
	if ctx, _ := h.attempt(); ctx.Err() == nil {
		t.Fatal("adopted STOP left the START attempt running")
	}
}

// TestServiceReadbackOutcomeRacesStartExecution: the typed START's Start
// succeeds while the superseding STOP is still unresolved. The success is not
// admitted while the decision is held; the readback outcome then decides:
// adopted cancels the START (acceptance first, one cancellation, never
// RUNNING), rejected admits the same attempt.
func TestServiceReadbackOutcomeRacesStartExecution(t *testing.T) {
	t.Parallel()
	for _, adopted := range []bool{true, false} {
		name := map[bool]string{true: "adopted", false: "rejected"}[adopted]
		t.Run(name, func(t *testing.T) {
			id := InstanceID("fam.alpha")
			f, g := gatedFixture(t, string(id))
			h := f.typedStartInFlight(id)
			starts, stops := f.execs[id].count("start"), f.execs[id].count("stop")
			cursor := f.svc.List().EventCursor
			gate, committed := f.ambiguousStop(g, id)
			h.result <- nil
			time.Sleep(50 * time.Millisecond)
			d := f.svc.drivers[id]
			d.mu.Lock()
			live := d.live
			d.mu.Unlock()
			if live != nil || f.status(id).ObservedState == ObservedRunning {
				t.Fatal("START admitted while its superseding STOP was unresolved")
			}
			if !adopted {
				f.rejectOnReadback(committed)
			}
			close(gate)
			if adopted {
				f.waitSettled(id, ObservedStopped)
				assertAcceptedThenCanceled(t, f, cursor, id, stops)
				return
			}
			f.waitSettled(id, ObservedRunning)
			_, corr := h.attempt()
			d.mu.Lock()
			live = d.live
			d.mu.Unlock()
			if live == nil || live.corr != corr || f.execs[id].count("start") != starts || f.execs[id].count("stop") != stops {
				t.Fatalf("rejected STOP: live=%+v corr=%+v starts+%d stops+%d", live, corr,
					f.execs[id].count("start")-starts, f.execs[id].count("stop")-stops)
			}
			for _, e := range driverEvents(f, cursor, id) {
				if e.Kind == EventDesiredAccepted {
					t.Fatalf("rejected STOP left an event: %+v", e)
				}
			}
		})
	}
}

// TestServiceCommittedSupersedingStopHoldsCancelUntilAccepted: a STOP behind
// an executing typed START is committed, but Decide has not yet returned from
// the store. The START's Start returns meanwhile and the reserved slot is
// visible; the reconciler must not cancel (no Stop, no observation) until
// the acceptance is published.
func TestServiceCommittedSupersedingStopHoldsCancelUntilAccepted(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f, g := gatedFixture(t, string(id))
	h := f.typedStartInFlight(id)
	stops := f.execs[id].count("stop")
	cursor := f.svc.List().EventCursor
	committedCh, release := make(chan struct{}), make(chan struct{})
	hook := func() {
		close(committedCh)
		<-release
	}
	g.afterDecide.Store(&hook)
	done := make(chan error, 1)
	go func() {
		_, err := f.decide(MethodStop, id)
		done <- err
	}()
	<-committedCh
	if rec := f.record(id); rec.Reserved == nil {
		t.Fatal("test premise: the STOP should be committed into the reserved slot")
	}
	h.result <- errors.New("start failed")
	time.Sleep(100 * time.Millisecond)
	if n := f.execs[id].count("stop"); n != stops {
		t.Fatal("cancellation ran before the acceptance was published")
	}
	if events := driverEvents(f, cursor, id); len(events) != 0 {
		t.Fatalf("events before the acceptance: %+v", events)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("STOP: %v", err)
	}
	f.waitSettled(id, ObservedStopped)
	assertAcceptedThenCanceled(t, f, cursor, id, stops)
}

// recoveryGateStore blocks the startup recovery read until released.
type recoveryGateStore struct {
	Store
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *recoveryGateStore) Recovery() *RecoveryMaterial {
	g.once.Do(func() { close(g.entered) })
	<-g.release
	return g.Store.Recovery()
}

// TestServiceAdmissionStaysClosedUntilStartupCompletes: a decision arriving
// while startup (catalog validation and recovery restore) is still running is
// refused, and is accepted once Start has returned.
func TestServiceAdmissionStaysClosedUntilStartupCompletes(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var g *recoveryGateStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)), noStart: true,
		wrap: func(s Store) Store {
			g = &recoveryGateStore{Store: s, entered: make(chan struct{}), release: make(chan struct{})}
			return g
		}})
	done := make(chan error, 1)
	go func() { done <- f.svc.Start(context.Background()) }()
	<-g.entered
	_, err := f.svc.Decide(deadlineCtx(t), MethodRestart, Request{InstanceID: id, ExpectedRevision: 1})
	refusalOf(t, err, RefusalUnavailable, 3)
	close(g.release)
	if err := <-done; err != nil {
		t.Fatalf("Start: %v", err)
	}
	f.waitSettled(id, ObservedRunning)
	if _, err := f.decide(MethodRestart, id); err != nil {
		t.Fatalf("decision after startup: %v", err)
	}
}

// TestServiceStartSuccessAfterBudgetExpiredDuringHoldIsWithdrawn: a typed
// START succeeds inside its budget but waits behind an unresolved superseding
// STOP; when the readback rejects that STOP after the budget ran out, the
// success is withdrawn and never published as RUNNING.
func TestServiceStartSuccessAfterBudgetExpiredDuringHoldIsWithdrawn(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var g *readbackStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		opts: func(o *ServiceOptions) {
			o.OperationBudget = time.Second
			// A slow readback series outlasts the START's budget.
			o.RetryDelay = func(int) time.Duration { return 300 * time.Millisecond }
		},
		wrap: func(s Store) Store { g = &readbackStore{Store: s}; return g }})
	h := f.typedStartInFlight(id)
	gate, committed := f.ambiguousStop(g, id)
	h.result <- nil
	time.Sleep(1300 * time.Millisecond)
	f.rejectOnReadback(committed)
	close(gate)
	f.waitDecisionSettled(id)
	f.waitObserved(id, ObservedFailed)
	d := f.svc.drivers[id]
	d.mu.Lock()
	live := d.live
	d.mu.Unlock()
	if live != nil && live.corr == func() LifecycleCorrelation { _, c := h.attempt(); return c }() {
		t.Fatal("expired START success was admitted")
	}
	if st := f.status(id); st.ObservedState == ObservedRunning {
		t.Fatalf("expired START published %s", st.ObservedState)
	}
}

// catalogFailStore fails the service-side catalog read on demand, so a view
// publication fails while the store still accepts decisions.
type catalogFailStore struct {
	Store
	fail atomic.Bool
}

func (c *catalogFailStore) Catalog() (ControlCatalog, error) {
	if c.fail.Load() {
		return ControlCatalog{}, errors.New("catalog unreadable")
	}
	return c.Store.Catalog()
}

// TestServiceFailedAcceptancePublicationKeepsSupersededStartRunning: when the
// acceptance cannot be published, the superseded START is neither canceled nor
// is any Stop run until a later publication carries the acceptance.
func TestServiceFailedAcceptancePublicationKeepsSupersededStartRunning(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var c *catalogFailStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		wrap: func(s Store) Store { c = &catalogFailStore{Store: s}; return c }})
	h := f.typedStartInFlight(id)
	stops := f.execs[id].count("stop")
	cursor := f.svc.List().EventCursor
	c.fail.Store(true)
	if _, err := f.decide(MethodStop, id); err != nil {
		t.Fatalf("STOP: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if ctx, _ := h.attempt(); ctx.Err() != nil {
		t.Fatal("START canceled before the acceptance was published")
	}
	if n := f.execs[id].count("stop"); n != stops {
		t.Fatal("cancellation ran before the acceptance was published")
	}
	if events := driverEvents(f, cursor, id); len(events) != 0 {
		t.Fatalf("events before publication: %+v", events)
	}
	c.fail.Store(false)
	f.waitSettled(id, ObservedStopped)
	assertAcceptedThenCanceled(t, f, cursor, id, stops)
}

// TestServiceMaxAttemptsOneReservesCleanupAttempt: MaxAttempts counts start
// attempts only. With a limit of one, a START that succeeds after its
// deadline still gets the reserved cleanup attempt: it runs once under the
// original deadline; obligations are then settled, or the cleanup stalls with
// the diagnostic when the Stop leaves semantic cleanup unfinished.
func TestServiceMaxAttemptsOneReservesCleanupAttempt(t *testing.T) {
	t.Parallel()
	for _, settled := range []bool{true, false} {
		name := map[bool]string{true: "settled", false: "stalled"}[settled]
		t.Run(name, func(t *testing.T) {
			// Each case waits out its own budget; the two do not share state.
			t.Parallel()
			id := InstanceID("fam.alpha")
			f := newFixture(t, svcOpts{drivers: enabled(string(id)),
				// The budget also bounds the fixture's first START, which has a single attempt, so it
				// must be long enough for a loaded host; the START under test ends at the budget by itself.
				opts: func(o *ServiceOptions) { o.OperationBudget = time.Second; o.MaxAttempts = 1 }})
			f.waitSettled(id, ObservedRunning)
			f.mustDecide(MethodStop, id)
			f.waitSettled(id, ObservedStopped)
			f.execs[id].setStart(func(ctx context.Context, _ LifecycleCorrelation) error {
				<-ctx.Done() // succeeds only after its budget ended
				return nil
			})
			f.execs[id].setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
				return StopOutcome{NativeClosed: true, SemanticCleaned: settled}
			})
			stops := f.execs[id].count("stop")
			f.mustDecide(MethodStart, id)
			waitFor(t, "failed", func() bool { return f.status(id).ObservedState == ObservedFailed })
			if settled {
				f.waitIdle(id)
			} else {
				waitFor(t, "cleanup stalled diagnostic", func() bool { return hasDiagnostic(f.svc.Diagnostics(id), "CLEANUP_STALLED") })
			}
			time.Sleep(200 * time.Millisecond)
			if n := f.execs[id].count("stop") - stops; n != 1 {
				t.Fatalf("cleanup ran Stop %d times, want exactly the one reserved attempt", n)
			}
			cur := f.record(id).Current
			if settled && cur != nil {
				t.Fatalf("settled cleanup left a slot: %+v", cur)
			}
			if !settled && (cur == nil || !cur.SemanticCleanupPending) {
				t.Fatalf("stalled cleanup lost its obligation: %+v", cur)
			}
			if hasDiagnostic(f.svc.Diagnostics(id), "CLEANUP_STALLED") == settled {
				t.Fatalf("CLEANUP_STALLED diagnostic = %v, want %v", !settled, !settled)
			}
		})
	}
}

// TestServiceUntypedSuccessExpiredWhileHeldIsWithdrawn: an untyped START whose
// Start succeeds before its deadline but waits behind an ambiguous STOP that
// is later rejected after the deadline is not installed as RUNNING: the
// admission is revalidated under the hold and the late generation is
// withdrawn.
func TestServiceUntypedSuccessExpiredWhileHeldIsWithdrawn(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var g *readbackStore
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		wrap: func(s Store) Store { g = &readbackStore{Store: s}; return g },
		// One readback attempt spans the whole budget, so the gate can hold
		// the decision past the attempt's deadline.
		opts: func(o *ServiceOptions) { o.OperationBudget = 600 * time.Millisecond; o.MaxAttempts = 1 },
		setup: func(f *svcFixture) {
			f.execs[id].setStart(func(context.Context, LifecycleCorrelation) error {
				once.Do(func() { close(entered) })
				<-release
				return nil
			})
		}})
	<-entered
	time.Sleep(250 * time.Millisecond) // the readback outlives the attempt's deadline
	cursor := f.svc.List().EventCursor
	gate, committed := f.ambiguousStop(g, id)
	close(release) // success before the deadline, held behind the decision
	time.Sleep(400 * time.Millisecond)
	f.rejectOnReadback(committed)
	close(gate)
	f.waitDecisionSettled(id)
	waitFor(t, "late generation withdrawn", func() bool { return f.execs[id].count("stop") == 1 })
	f.waitObserved(id, ObservedFailed)
	started, stopped := f.execs[id].snapshot()[0].Corr, f.execs[id].snapshot()
	if c := stopped[len(stopped)-1]; c.Op != "stop" || c.Corr.Epoch != started.Epoch || c.Corr.Generation != started.Generation {
		t.Fatalf("withdrawal %+v does not name the late generation %+v", c, started)
	}
	for _, e := range driverEvents(f, cursor, id) {
		if e.Kind == EventObservedChanged && e.ObservedState == ObservedRunning {
			t.Fatalf("expired success was published as RUNNING: %+v", e)
		}
	}
	d := f.svc.drivers[id]
	d.mu.Lock()
	live := d.live
	d.mu.Unlock()
	if live != nil {
		t.Fatalf("expired success installed as live: %+v", live)
	}
}

// TestServiceRetryGenerationIncrementIsCommittedBeforeNextStart: when
// persisting a retry's generation increment fails, the reconciler keeps the
// pending increment and commits it before the next Start, so the executor
// never sees the previous generation twice.
func TestServiceRetryGenerationIncrementIsCommittedBeforeNextStart(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var ws *workFaultStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		wrap: func(s Store) Store { ws = &workFaultStore{Store: s}; return ws }})
	f.waitSettled(id, ObservedRunning)
	f.mustDecide(MethodStop, id)
	f.waitSettled(id, ObservedStopped)
	var mu sync.Mutex
	var gens []uint64
	f.execs[id].setStart(func(_ context.Context, c LifecycleCorrelation) error {
		mu.Lock()
		defer mu.Unlock()
		gens = append(gens, uint64(c.Generation))
		if len(gens) == 1 {
			ws.failFrom.Store(int32(maxCommitAttempts)) // the increment cannot commit
			return retryErr{"warming"}
		}
		return nil
	})
	f.mustDecide(MethodStart, id)
	f.waitSettled(id, ObservedRunning)
	mu.Lock()
	defer mu.Unlock()
	if len(gens) != 2 || gens[0] != 1 || gens[1] != 2 {
		t.Fatalf("Start generations = %v, want [1 2]", gens)
	}
}

// workFaultStore fails the next failFrom CommitWork calls.
type workFaultStore struct {
	Store
	failFrom atomic.Int32
}

func (w *workFaultStore) CommitWork(ctx context.Context, id InstanceID, current, reserved *Transition) error {
	if w.failFrom.Add(-1) >= 0 {
		return errors.New("synthetic work commit failure")
	}
	return w.Store.CommitWork(ctx, id, current, reserved)
}

// TestServiceRestartEndsFailedWhenPredecessorCleanupSpentTheStartBudget: with
// a start limit of one, the predecessor's cleanup uses the only permitted
// attempt, so the RESTART ends FAILED without creating or starting a
// replacement, and its cleanup retry state is reset.
func TestServiceRestartEndsFailedWhenPredecessorCleanupSpentTheStartBudget(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		opts: func(o *ServiceOptions) { o.MaxAttempts = 1 }})
	f.waitSettled(id, ObservedRunning)
	starts, stops := f.execs[id].count("start"), f.execs[id].count("stop")
	f.mustDecide(MethodRestart, id)
	f.waitSettled(id, ObservedFailed)
	time.Sleep(100 * time.Millisecond)
	if n := f.execs[id].count("start") - starts; n != 0 {
		t.Fatalf("a replacement was started after the budget was spent: %d Start calls", n)
	}
	if n := f.execs[id].count("stop") - stops; n != 1 {
		t.Fatalf("predecessor cleanup ran Stop %d times, want 1", n)
	}
	if !hasDiagnostic(f.svc.Diagnostics(id), "FAILED") {
		t.Fatalf("no budget-exhausted diagnostic: %+v", f.svc.Diagnostics(id))
	}
	d := f.svc.drivers[id]
	d.mu.Lock()
	blocked := d.quarantined || d.recoveryBlocked
	d.mu.Unlock()
	if blocked {
		t.Fatal("the spent RESTART quarantined or blocked the driver")
	}
}

// TestServiceRestartResetsCleanupRetriesAfterPredecessorCleanup: a
// predecessor whose semantic cleanup needed a retry leaves no retry count
// behind for an unrelated later STOP.
func TestServiceRestartResetsCleanupRetriesAfterPredecessorCleanup(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	f := newFixture(t, svcOpts{drivers: enabled(string(id))})
	f.waitSettled(id, ObservedRunning)
	var calls atomic.Int32
	f.execs[id].setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
		return StopOutcome{NativeClosed: true, SemanticCleaned: calls.Add(1) > 1}
	})
	f.mustDecide(MethodRestart, id)
	f.waitSettled(id, ObservedRunning)
	if calls.Load() < 2 {
		t.Fatalf("test premise: the predecessor needed a semantic retry, Stop ran %d times", calls.Load())
	}
	d := f.svc.drivers[id]
	d.mu.Lock()
	retries := d.cleanupRetries
	d.mu.Unlock()
	if retries != 0 {
		t.Fatalf("cleanupRetries = %d after the predecessor's cleanup succeeded, want 0", retries)
	}
}

// TestServiceStopDuringLastUntypedAttemptStillCleansUp: a STOP accepted
// during the eighth untyped attempt does not inherit a count that skips the
// reserved cleanup Stop.
func TestServiceStopDuringLastUntypedAttemptStillCleansUp(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	entered := make(chan struct{})
	var calls atomic.Int32
	f := newFixture(t, svcOpts{drivers: enabled(string(id)), setup: func(f *svcFixture) {
		f.execs[id].setStart(func(ctx context.Context, _ LifecycleCorrelation) error {
			if calls.Add(1) < DefaultMaxAttempts {
				return retryErr{"warming"}
			}
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
	}})
	<-entered
	f.mustDecide(MethodStop, id)
	f.waitSettled(id, ObservedStopped)
	if n := f.execs[id].count("stop"); n != 1 {
		t.Fatalf("cancellation ran Stop %d times, want 1", n)
	}
	d := f.svc.drivers[id]
	d.mu.Lock()
	blocked := d.quarantined || d.recoveryBlocked
	d.mu.Unlock()
	if blocked {
		t.Fatal("the canceled eighth attempt left the driver quarantined")
	}
}

// applyThenFailStore persists a matching charge commit and then reports an
// error, the shape of an ambiguous commit that readback adopts.
type applyThenFailStore struct {
	Store
	failures atomic.Int32
}

func (w *applyThenFailStore) CommitWork(ctx context.Context, id InstanceID, cur, res *Transition) error {
	err := w.Store.CommitWork(ctx, id, cur, res)
	if err == nil && cur != nil && cur.Phase == PhaseActive && cur.AttemptsUsed == 1 && w.failures.Add(-1) >= 0 {
		return errors.New("synthetic ambiguous commit")
	}
	return err
}

// TestServiceAdoptedStartChargeIsNotChargedAgain: a start-attempt charge that
// was persisted but reported as failed is adopted; the attempt runs Start
// instead of charging again and exhausting a one-attempt budget.
func TestServiceAdoptedStartChargeIsNotChargedAgain(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var w *applyThenFailStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		opts: func(o *ServiceOptions) { o.MaxAttempts = 1 },
		wrap: func(s Store) Store { w = &applyThenFailStore{Store: s}; return w }})
	f.waitSettled(id, ObservedRunning)
	f.mustDecide(MethodStop, id)
	f.waitSettled(id, ObservedStopped)
	starts := f.execs[id].count("start")
	w.failures.Store(int32(maxCommitAttempts))
	f.mustDecide(MethodStart, id)
	f.waitSettled(id, ObservedRunning)
	if n := f.execs[id].count("start") - starts; n != 1 {
		t.Fatalf("Start ran %d times, want 1", n)
	}
}

// TestServiceAdoptedCleanupChargeIsNotChargedAgain: an adopted cleanup charge
// leaves the persisted count at one while Stop runs.
func TestServiceAdoptedCleanupChargeIsNotChargedAgain(t *testing.T) {
	t.Parallel()
	id := InstanceID("fam.alpha")
	var w *applyThenFailStore
	f := newFixture(t, svcOpts{drivers: enabled(string(id)),
		opts: func(o *ServiceOptions) { o.MaxAttempts = 1 },
		wrap: func(s Store) Store { w = &applyThenFailStore{Store: s}; return w }})
	f.waitSettled(id, ObservedRunning)
	var used atomic.Int32
	f.execs[id].setStop(func(context.Context, LifecycleCorrelation) StopOutcome {
		if cur := f.record(id).Current; cur != nil {
			used.Store(int32(cur.AttemptsUsed))
		}
		return StopOutcome{NativeClosed: true, SemanticCleaned: true}
	})
	w.failures.Store(int32(maxCommitAttempts))
	f.mustDecide(MethodStop, id)
	f.waitSettled(id, ObservedStopped)
	if got := used.Load(); got != 1 {
		t.Fatalf("persisted attempts while Stop ran = %d, want 1", got)
	}
}

// frozenReadStore serves one pinned record while armed, the view a reader
// has before a pending decision's commit becomes visible.
type frozenReadStore struct {
	Store
	frozen atomic.Pointer[ControlRecord]
}

func (w *frozenReadStore) Driver(id InstanceID) (ControlRecord, error) {
	if rec := w.frozen.Load(); rec != nil {
		return *rec, nil
	}
	return w.Store.Driver(id)
}

// TestServiceStartErrorWaitsForPendingDecision: a Start error that arrives
// while a STOP is between its planner and its durable commit is classified
// only after that decision publishes its acceptance, for typed and untyped
// attempts.
func TestServiceStartErrorWaitsForPendingDecision(t *testing.T) {
	t.Parallel()
	for _, typed := range []bool{true, false} {
		name := map[bool]string{true: "typed", false: "untyped"}[typed]
		t.Run(name, func(t *testing.T) {
			id := InstanceID("fam.alpha")
			result := make(chan error, 1)
			enteredCh := make(chan struct{}, 1)
			blocking := func(ctx context.Context, _ LifecycleCorrelation) error {
				select {
				case enteredCh <- struct{}{}:
				default:
				}
				select {
				case err := <-result:
					return err
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			var f *svcFixture
			var fr *frozenReadStore
			var g *readbackStore
			wrap := func(s Store) Store {
				g = &readbackStore{Store: s}
				fr = &frozenReadStore{Store: g}
				return fr
			}
			if typed {
				f = newFixture(t, svcOpts{drivers: enabled(string(id)), wrap: wrap})
				f.waitSettled(id, ObservedRunning)
				f.mustDecide(MethodStop, id)
				f.waitSettled(id, ObservedStopped)
				f.execs[id].setStart(blocking)
				f.mustDecide(MethodStart, id)
			} else {
				f = newFixture(t, svcOpts{drivers: enabled(string(id)), wrap: wrap,
					setup: func(f *svcFixture) { f.execs[id].setStart(blocking) }})
			}
			<-enteredCh
			f.quiesce()
			stops := f.execs[id].count("stop")
			cursor := f.svc.List().EventCursor
			pinned := f.record(id)
			committed, release := make(chan struct{}), make(chan struct{})
			hook := func() {
				close(committed)
				<-release
			}
			g.afterDecide.Store(&hook)
			done := make(chan error, 1)
			go func() {
				_, err := f.decide(MethodStop, id)
				done <- err
			}()
			<-committed
			fr.frozen.Store(&pinned)
			result <- retryErr{"warming"}
			time.Sleep(100 * time.Millisecond)
			if events := driverEvents(f, cursor, id); len(events) != 0 {
				t.Fatalf("events before the acceptance: %+v", events)
			}
			fr.frozen.Store(nil)
			close(release)
			if err := <-done; err != nil {
				t.Fatalf("STOP: %v", err)
			}
			f.waitSettled(id, ObservedStopped)
			assertAcceptedThenCanceled(t, f, cursor, id, stops)
		})
	}
}
