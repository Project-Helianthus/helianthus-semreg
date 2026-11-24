package life

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errDecCommitFailure = errors.New("synthetic pre-persistence failure")

type decStore struct {
	Store
	mu             sync.Mutex
	record         ControlRecord
	revision       CatalogRevision
	fail           bool
	adopted        bool
	afterPlan      func()
	driverBarrier  func()
	catalogBarrier func()
}

func (p *decStore) AdmissionOpen() bool { return true }
func (p *decStore) Driver(id InstanceID) (ControlRecord, error) {
	p.mu.Lock()
	barrier := p.driverBarrier
	p.driverBarrier = nil
	p.mu.Unlock()
	if barrier != nil {
		barrier()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != p.record.InstanceID {
		return ControlRecord{}, errors.New("unknown synthetic family")
	}
	r := p.record
	r.Current, r.Reserved = cloneTransition(r.Current), cloneTransition(r.Reserved)
	return r, nil
}
func (p *decStore) Catalog() (ControlCatalog, error) {
	p.mu.Lock()
	barrier := p.catalogBarrier
	p.catalogBarrier = nil
	p.mu.Unlock()
	if barrier != nil {
		barrier()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.record
	r.Current, r.Reserved = cloneTransition(r.Current), cloneTransition(r.Reserved)
	return ControlCatalog{CatalogRevision: p.revision, Drivers: []ControlRecord{r}}, nil
}
func (p *decStore) AdvanceCatalog(context.Context) (CatalogRevision, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revision++
	return p.revision, nil
}
func (p *decStore) CommitWork(_ context.Context, id InstanceID, current, reserved *Transition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != p.record.InstanceID {
		return errors.New("wrong family")
	}
	p.record.Current, p.record.Reserved = cloneTransition(current), cloneTransition(reserved)
	return nil
}
func (p *decStore) Decide(_ context.Context, method Method, req Request, planner Planner) (DesiredView, error) {
	p.mu.Lock()
	if req.ExpectedRevision != p.record.DesiredRevision {
		p.mu.Unlock()
		return DesiredView{}, &Refusal{Code: RefusalRevisionConflict}
	}
	plan, refusal := planner(PlanInput{Method: method, InstanceID: req.InstanceID, HasRecord: true, Current: cloneTransition(p.record.Current), Reserved: cloneTransition(p.record.Reserved)})
	if refusal != nil {
		p.mu.Unlock()
		return DesiredView{}, refusal
	}
	fail, adopted, hook := p.fail, p.adopted, p.afterPlan
	p.afterPlan = nil
	if !fail || adopted {
		p.record.DesiredRevision++
		p.record.DesiredState = DesiredRunning
		if method == MethodStop {
			p.record.DesiredState = DesiredStopped
		}
		p.record.Current, p.record.Reserved = plan.Current, plan.Reserved
		next := p.record.Current
		if p.record.Reserved != nil {
			next = p.record.Reserved
		}
		next.DesiredRevision, next.Sequence = p.record.DesiredRevision, 1
		if p.record.Reserved != nil {
			next.Sequence = p.record.Current.Sequence + 1
		}
		p.revision++
	}
	view := DesiredView{DesiredState: p.record.DesiredState, DesiredRevision: p.record.DesiredRevision}
	p.mu.Unlock()
	// The store's serialization point has ended; the caller has not yet
	// processed the commit result. No store mutex is held at this barrier.
	if hook != nil {
		hook()
	}
	if fail {
		return DesiredView{}, errDecCommitFailure
	}
	return view, nil
}

func decService(t *testing.T, id InstanceID, revision DesiredRevision) (*Service, *decStore, *fakeExec) {
	t.Helper()
	store := &decStore{record: ControlRecord{InstanceID: id, Enabled: true, HasRecord: true, DesiredState: DesiredStopped, DesiredRevision: revision}, revision: 1}
	exec := &fakeExec{}
	svc, err := NewService(ServiceOptions{Store: store, Recovery: &fakeRecovery{}, Incarnation: "synthetic", Drivers: []ServiceInstance{{InstanceID: id, Executor: exec}}})
	if err != nil {
		t.Fatal(err)
	}
	svc.ctx, svc.cancel = context.WithCancel(context.Background())
	// Drive reconciliation explicitly so publication barriers cannot depend on
	// scheduler timing or an unrelated worker's catalog reads.
	svc.started, svc.admitting = true, true
	t.Cleanup(svc.Close)
	if !svc.commitView(svc.ctx, nil, false) {
		t.Fatal("initial publication failed")
	}
	return svc, store, exec
}

func decWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("barrier watchdog expired")
	}
}

func decBusy(t *testing.T, err error) {
	t.Helper()
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Code != RefusalTransitionBusy || refusal.Row != 8 {
		t.Fatalf("want row-8 TRANSITION_BUSY, got %v", err)
	}
}

func TestFailedCommitsBalanceHolds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		id       InstanceID
		revision DesiredRevision
	}{
		{"base", "family.alpha", 11},
		{"driver", "family.beta", 11},
		{"revision", "family.alpha", 17},
	} {
		t.Run(tc.name, func(t *testing.T) { decFailedCommitsBalanceHolds(t, tc.id, tc.revision) })
	}
}

func decFailedCommitsBalanceHolds(t *testing.T, id InstanceID, revision DesiredRevision) {
	t.Helper()
	svc, store, exec := decService(t, id, revision)
	d := svc.drivers[id]
	// Two failed commits, each with a concurrent changing contender in the
	// post-planner/pre-result window. A contender must not acquire a second hold.
	for round := 0; round < 2; round++ {
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		t.Cleanup(func() { unblock(); decWait(t, done) })
		store.mu.Lock()
		store.fail = true
		store.afterPlan = func() { close(entered); <-release }
		store.mu.Unlock()
		var failed error
		go func() {
			defer close(done)
			_, failed = svc.Decide(context.Background(), MethodStart, Request{InstanceID: d.id, ExpectedRevision: revision})
		}()
		decWait(t, entered)
		_, err := svc.Decide(context.Background(), MethodRestart, Request{InstanceID: d.id, ExpectedRevision: revision})
		decBusy(t, err)
		d.mu.Lock()
		holds := d.deciding
		d.mu.Unlock()
		if holds != 1 {
			t.Fatalf("overlapping owner: deciding=%d", holds)
		}
		unblock()
		decWait(t, done)
		if !errors.Is(failed, errDecCommitFailure) {
			t.Fatalf("failure = %v", failed)
		}
		d.mu.Lock()
		holds, ambiguous := d.deciding, d.ambiguous
		d.mu.Unlock()
		if holds != 0 || ambiguous != nil {
			t.Fatalf("leaked decision after failed commit: %d %+v", holds, ambiguous)
		}
	}
	if len(svc.EventsSince(0).Events) != 0 {
		t.Fatal("failed commits emitted acceptance")
	}
	store.mu.Lock()
	store.fail = false
	store.mu.Unlock()
	if _, err := svc.Decide(context.Background(), MethodStart, Request{InstanceID: d.id, ExpectedRevision: revision}); err != nil {
		t.Fatal(err)
	}
	if progressed, _ := svc.reconcileStep(d); !progressed {
		t.Fatal("reconciliation did not progress")
	}
	if exec.count("start") != 1 {
		t.Fatalf("subsequent starts = %d", exec.count("start"))
	}
}

func TestAdoptedPublicationRetainsOwner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		id       InstanceID
		revision DesiredRevision
	}{
		{"base", "family.alpha", 11},
		{"driver", "family.beta", 11},
		{"revision", "family.alpha", 17},
	} {
		for _, blockRead := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/block-read-%t", tc.name, blockRead), func(t *testing.T) {
				decAdoptedPublicationRetainsOwner(t, tc.id, tc.revision, blockRead)
			})
		}
	}
}

func decAdoptedPublicationRetainsOwner(t *testing.T, id InstanceID, revision DesiredRevision, blockRead bool) {
	t.Helper()
	svc, store, _ := decService(t, id, revision)
	d := svc.drivers[id]
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); decWait(t, done) })
	store.mu.Lock()
	store.fail, store.adopted = true, true
	barrier := func() { close(entered); <-release }
	if blockRead {
		store.driverBarrier = barrier
	} else {
		store.catalogBarrier = barrier
	}
	store.mu.Unlock()
	var failed error
	go func() {
		defer close(done)
		_, failed = svc.Decide(context.Background(), MethodStart, Request{InstanceID: d.id, ExpectedRevision: revision})
	}()
	decWait(t, entered)
	// Direct marker oracle: deciding is a redundant admission guard, so busy
	// alone cannot detect premature removal of ambiguous ownership.
	d.mu.Lock()
	retained := d.ambiguous != nil && d.ambiguous.resolving && d.deciding == 1
	d.mu.Unlock()
	if !retained {
		t.Error("ambiguous owner cleared before acceptance publication")
	}
	if svc.List().Drivers[0].DesiredRevision != revision {
		t.Error("view published before barrier")
	}
	_, err := svc.Decide(context.Background(), MethodStop, Request{InstanceID: d.id, ExpectedRevision: revision + 1})
	decBusy(t, err)
	// A concurrent readback must return without publishing or waiting for the
	// resolver that owns the acceptance, whether it is reading or publishing.
	readbackDone := make(chan struct{})
	t.Cleanup(func() { unblock(); decWait(t, readbackDone) })
	go func() {
		defer close(readbackDone)
		svc.readbackResolved()
	}()
	decWait(t, readbackDone)
	if svc.List().Drivers[0].DesiredRevision != revision {
		t.Error("concurrent readback published adopted view before acceptance")
	}
	unblock()
	decWait(t, done)
	if !errors.Is(failed, errDecCommitFailure) {
		t.Fatalf("commit result = %v", failed)
	}
	page := svc.EventsSince(0)
	if page.Resync || len(page.Events) != 1 {
		t.Fatalf("want one retained acceptance: %+v", page)
	}
	event := page.Events[0]
	if event.Kind != EventDesiredAccepted || event.InstanceID != id || event.DesiredRevision != revision+1 || event.DesiredState != DesiredRunning {
		t.Fatalf("acceptance = %+v", event)
	}
	if svc.List().Drivers[0].DesiredRevision != revision+1 {
		t.Fatal("adopted view not published")
	}
	d.mu.Lock()
	holds, ambiguous := d.deciding, d.ambiguous
	d.mu.Unlock()
	if holds != 0 || ambiguous != nil {
		t.Fatalf("owner not released: %d %+v", holds, ambiguous)
	}
	store.mu.Lock()
	store.fail, store.adopted = false, false
	store.mu.Unlock()
	if _, err := svc.Decide(context.Background(), MethodStop, Request{InstanceID: d.id, ExpectedRevision: revision + 1}); err != nil {
		t.Fatalf("post-publication admission: %v", err)
	}
}

// decTwoDriverStore composes two synthetic records into one catalog, as the
// production store does. Only the first driver's read is held by the barrier.
type decTwoDriverStore struct {
	*decStore
	other *decStore
}

func (p *decTwoDriverStore) Driver(id InstanceID) (ControlRecord, error) {
	if id == p.other.record.InstanceID {
		return p.other.Driver(id)
	}
	return p.decStore.Driver(id)
}

func (p *decTwoDriverStore) Catalog() (ControlCatalog, error) {
	catalog, err := p.decStore.Catalog()
	if err != nil {
		return ControlCatalog{}, err
	}
	other, err := p.other.Catalog()
	catalog.Drivers = append(catalog.Drivers, other.Drivers...)
	return catalog, err
}

func TestTwoResolverAcceptanceOrdering(t *testing.T) {
	t.Parallel()
	const revision DesiredRevision = 11
	a := &decStore{record: ControlRecord{InstanceID: "family.alpha", Enabled: true, HasRecord: true, DesiredState: DesiredStopped, DesiredRevision: revision}, revision: 1}
	b := &decStore{record: ControlRecord{InstanceID: "family.beta", Enabled: true, HasRecord: true, DesiredState: DesiredStopped, DesiredRevision: revision}, revision: 1}
	store := &decTwoDriverStore{decStore: a, other: b}
	svc, err := NewService(ServiceOptions{Store: store, Recovery: &fakeRecovery{}, Incarnation: "synthetic", Drivers: []ServiceInstance{
		{InstanceID: a.record.InstanceID, Executor: &fakeExec{}},
		{InstanceID: b.record.InstanceID, Executor: &fakeExec{}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	svc.ctx, svc.cancel = context.WithCancel(context.Background())
	svc.started, svc.admitting = true, true
	t.Cleanup(svc.Close)
	if !svc.commitView(svc.ctx, nil, false) {
		t.Fatal("initial publication failed")
	}
	// Both ambiguous commits have been adopted by readback. Their acceptance
	// events are still owned by the independent resolvers below.
	for _, p := range []*decStore{a, b} {
		p.record.DesiredState = DesiredRunning
		p.record.DesiredRevision++
		d := svc.drivers[p.record.InstanceID]
		d.mu.Lock()
		d.deciding = 1
		d.ambiguous = &ambiguousDecision{method: MethodStart, expected: revision}
		d.ambiguousHeld.Store(true)
		d.mu.Unlock()
	}
	a.revision++
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); decWait(t, done) })
	a.driverBarrier = func() { close(entered); <-release }
	go func() {
		defer close(done)
		svc.resolveAmbiguous([]*driverState{svc.drivers[a.record.InstanceID]})
	}()
	decWait(t, entered)
	// Resolver B skips A's in-progress marker and adopts B. This must finish
	// while A is blocked, without exposing A's adopted desired revision.
	svc.readbackResolved()
	view := svc.List()
	if view.Drivers[0].DesiredRevision != revision || view.Drivers[0].DesiredState != DesiredStopped {
		t.Errorf("resolver B exposed A before acceptance: %+v", view)
	}
	if view.Drivers[1].DesiredRevision != revision+1 || view.Drivers[1].DesiredState != DesiredRunning {
		t.Errorf("resolver B did not publish B: %+v", view)
	}
	page := svc.EventsSince(0)
	if page.Resync || len(page.Events) != 1 || page.Events[0].Kind != EventDesiredAccepted || page.Events[0].InstanceID != b.record.InstanceID || page.Events[0].DesiredRevision != revision+1 || view.EventCursor != page.Events[0].Cursor {
		t.Errorf("resolver B acceptance = %+v, view = %+v", page, view)
	}
	d := svc.drivers[a.record.InstanceID]
	d.mu.Lock()
	retained := d.deciding == 1 && d.ambiguous != nil && d.ambiguous.resolving
	d.mu.Unlock()
	if !retained {
		t.Error("resolver B released A's ownership")
	}
	unblock()
	decWait(t, done)
	view = svc.List()
	page = svc.EventsSince(0)
	if page.Resync || len(page.Events) != 2 {
		t.Fatalf("want both acceptances without a gap: %+v", page)
	}
	event := page.Events[1]
	if event.Kind != EventDesiredAccepted || event.InstanceID != a.record.InstanceID || event.DesiredRevision != revision+1 || event.DesiredState != DesiredRunning || view.EventCursor != event.Cursor || view.Revision != event.CatalogRevision {
		t.Fatalf("resolver A acceptance = %+v, view = %+v", event, view)
	}
	for _, entry := range view.Drivers {
		if entry.DesiredRevision != revision+1 || entry.DesiredState != DesiredRunning {
			t.Errorf("adopted view missing: %+v", entry)
		}
		d := svc.drivers[entry.InstanceID]
		d.mu.Lock()
		released := d.deciding == 0 && d.ambiguous == nil
		d.mu.Unlock()
		if !released {
			t.Errorf("owner not released for %s", entry.InstanceID)
		}
	}
}

func TestRecoveredCleanupCorrelation(t *testing.T) {
	t.Parallel()
	type identity struct {
		id         InstanceID
		epoch      SourceEpoch
		generation DriverGeneration
		revision   DesiredRevision
		sequence   LifecycleSequence
	}
	base := identity{"family.alpha", "historical.alpha", 3, 11, 7}
	variants := []struct {
		name     string
		identity identity
	}{{"base", base}}
	for _, field := range []string{"driver", "epoch", "generation", "revision", "sequence"} {
		v := base
		switch field {
		case "driver":
			v.id = "family.beta"
		case "epoch":
			v.epoch = "historical.beta"
		case "generation":
			v.generation = 5
		case "revision":
			v.revision = 17
		case "sequence":
			v.sequence = 13
		}
		variants = append(variants, struct {
			name     string
			identity identity
		}{field, v})
	}
	for _, kind := range []TransitionKind{KindStart, KindRestart} {
		for _, semantic := range []bool{false, true} {
			for _, variant := range variants {
				t.Run(fmt.Sprintf("%s/semantic-%t/%s", kind, semantic, variant.name), func(t *testing.T) {
					v := variant.identity
					svc, store, exec := decService(t, v.id, v.revision)
					d := svc.drivers[v.id]
					transition := Transition{Kind: kind, Phase: PhaseActive, Epoch: v.epoch, Generation: v.generation, DesiredRevision: v.revision, Sequence: v.sequence, NativeClosePending: true, SemanticCleanupPending: true, AttemptsUsed: svc.cleanupLimit() - 1, DeadlineUTC: time.Now().Add(svc.budget)}
					store.mu.Lock()
					store.record.DesiredState = DesiredRunning
					store.record.DesiredRevision = v.revision
					store.record.Current = &transition
					store.revision++
					store.mu.Unlock()
					exec.setStop(func(_ context.Context, c LifecycleCorrelation) StopOutcome {
						if c.InstanceID != v.id || c.Epoch != v.epoch || c.Generation != v.generation || c.Sequence != v.sequence {
							t.Errorf("cleanup correlation = %+v, want %+v", c, v)
						}
						r, err := store.Driver(v.id)
						if err != nil || r.Current.AttemptsUsed != svc.cleanupLimit() {
							t.Error("cleanup effect preceded durable charge")
						}
						return StopOutcome{NativeClosed: semantic, SemanticCleaned: false}
					})
					svc.reconcileStep(d)
					if exec.count("stop") != 1 || exec.count("start") != 0 {
						t.Fatalf("unexpected executor calls: %+v", exec.snapshot())
					}
					events := svc.EventsSince(0).Events
					if len(events) != 1 {
						t.Fatalf("failure event count: %+v", events)
					}
					e := events[0]
					reason := ReasonCloseUnconfirmed
					outcome := "CLOSE_UNCONFIRMED"
					if semantic {
						reason = ReasonRetryExhausted
						outcome = "CLEANUP_STALLED"
					}
					if e.Kind != EventObservedChanged || e.ObservedState != ObservedFailed || e.Reason != reason || e.InstanceID != v.id || e.Epoch != v.epoch || e.Generation != v.generation || e.DesiredRevision != v.revision {
						t.Fatalf("failure event = %+v, want %+v reason %s", e, v, reason)
					}
					// Events omit sequence; check the installed correlation directly as an
					// independent oracle rather than claiming event coverage of that field.
					d.mu.Lock()
					corr := d.corr
					d.mu.Unlock()
					if corr.InstanceID != v.id || corr.Epoch != v.epoch || corr.Generation != v.generation || corr.Sequence != v.sequence {
						t.Fatalf("installed correlation = %+v, want %+v", corr, v)
					}
					diags := svc.Diagnostics(v.id)
					if len(diags) != 1 || diags[0].Outcome != outcome || diags[0].DesiredRevision != v.revision {
						t.Fatalf("cleanup diagnostic = %+v", diags)
					}
					r, _ := store.Driver(v.id)
					if r.Current.Epoch != v.epoch || r.Current.Generation != v.generation || r.Current.Sequence != v.sequence || r.Current.DesiredRevision != v.revision || !r.Current.SemanticCleanupPending || r.Current.NativeClosePending == semantic {
						t.Fatalf("lost native evidence: %+v", r.Current)
					}
				})
			}
		}
	}
}

// The second clock read after Start returns is the deadline revalidation in
// settle's callback, while d.mu is held, immediately before stageObservation.
func TestStartSettlementConcurrentPublication(t *testing.T) {
	t.Parallel()
	for _, typed := range []bool{true, false} {
		t.Run(fmt.Sprintf("typed-%t", typed), func(t *testing.T) {
			const id InstanceID = "family.alpha"
			store := &decStore{record: ControlRecord{InstanceID: id, Enabled: true, HasRecord: true, DesiredState: DesiredRunning, DesiredRevision: 11}, revision: 1}
			exec := &fakeExec{}
			svc, err := NewService(ServiceOptions{Store: store, Recovery: &fakeRecovery{}, Incarnation: "synthetic", Drivers: []ServiceInstance{{InstanceID: id, Executor: exec}}})
			if err != nil {
				t.Fatal(err)
			}
			svc.ctx, svc.cancel = context.WithCancel(context.Background())
			// Cancel without taking driver locks even if the watchdog finds a
			// deadlock. Close is checked after both goroutines finish below.
			t.Cleanup(svc.cancel)
			if !svc.commitView(svc.ctx, nil, false) {
				t.Fatal("initial publication failed")
			}
			settling, resumeSettlement := make(chan struct{}), make(chan struct{})
			publishing, resumePublication := make(chan struct{}), make(chan struct{})
			var releaseSettlement, releasePublication sync.Once
			unblockSettlement := func() { releaseSettlement.Do(func() { close(resumeSettlement) }) }
			unblockPublication := func() { releasePublication.Do(func() { close(resumePublication) }) }
			t.Cleanup(unblockSettlement)
			t.Cleanup(unblockPublication)
			var returned atomic.Bool
			var clockReads atomic.Int32
			svc.now = func() time.Time {
				if returned.Load() && clockReads.Add(1) == 2 {
					close(settling)
					<-resumeSettlement
				}
				return time.Now()
			}
			exec.setStart(func(context.Context, LifecycleCorrelation) error {
				returned.Store(true)
				return nil
			})
			d := svc.drivers[id]
			settled := make(chan struct{})
			go func() {
				defer close(settled)
				if typed {
					transition := Transition{Kind: KindStart, Phase: PhaseActive, Epoch: "synthetic.start", Generation: 1, DesiredRevision: 11, Sequence: 1, NativeClosePending: true, SemanticCleanupPending: true, DeadlineUTC: time.Now().Add(svc.budget)}
					store.mu.Lock()
					store.record.Current = &transition
					store.mu.Unlock()
					a := svc.newAttempt(svc.corr(d, transition), transition.DeadlineUTC, transition.Sequence)
					defer a.cancel()
					svc.runTypedAttempt(d, transition, a)
				} else {
					svc.runUntyped(d)
				}
			}()
			decWait(t, settling)
			store.mu.Lock()
			store.catalogBarrier = func() {
				// publish owns viewMu; settlement already owns d.mu.
				close(publishing)
				<-resumePublication
			}
			store.mu.Unlock()
			published := make(chan struct{})
			go func() {
				defer close(published)
				svc.publish(nil)
			}()
			decWait(t, publishing)
			unblockSettlement()
			unblockPublication()
			decWait(t, published)
			decWait(t, settled)
			view, err := svc.Get(id)
			if err != nil || view.ObservedState != ObservedRunning {
				t.Fatalf("settled view = %+v, error = %v", view, err)
			}
			if exec.count("start") != 1 {
				t.Fatalf("starts = %d", exec.count("start"))
			}
			closed := make(chan struct{})
			go func() { svc.Close(); close(closed) }()
			decWait(t, closed)
		})
	}
}

// A Service assembled without a driver map must still publish; the held-
// intent guard may only consult drivers that exist.
func TestPublishToleratesUnregisteredDriver(t *testing.T) {
	t.Parallel()
	for _, id := range []InstanceID{"fam.alpha", "fam.beta"} {
		store := &decStore{record: ControlRecord{InstanceID: id, Enabled: true, HasRecord: true, DesiredState: DesiredRunning, DesiredRevision: 2}, revision: 3}
		s := &Service{store: store, ctx: context.Background(), order: []InstanceID{id}, events: newEventLog(8),
			observed: map[InstanceID]observation{id: {state: ObservedRunning, reason: ReasonNone}}}
		s.view.Store(&Catalog{})
		if !s.publish(nil) {
			t.Fatalf("%s: publish refused", id)
		}
		if got := s.List(); got.Revision != 3 || len(got.Drivers) != 1 || got.Drivers[0].DesiredRevision != 2 {
			t.Fatalf("%s: unexpected view %+v", id, got)
		}
	}
}
