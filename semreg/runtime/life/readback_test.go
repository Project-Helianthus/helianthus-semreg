package life

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// ledgerEntry is one entry of the synthetic adapter's write-ahead ledger: an
// intent that the publisher has not applied, or its accepted outcome.
type ledgerEntry struct {
	Seq      int  `json:"seq"`
	Accepted bool `json:"accepted"`
}

type ambiguousWrite struct {
	payload []byte
	prev    []byte
	intent  bool
}

// ledgerAdapter is a synthetic self-writing recovery adapter: it keeps a
// write-ahead ledger in the recovery envelope through the store, around one
// call of a synthetic publisher. It binds the service's readback resolver and
// settles its held material against the committed bytes when the service
// reports the readback resolved.
type ledgerAdapter struct {
	store Store
	log   *orderLog

	mu         sync.Mutex
	arm        func()
	memory     []byte
	amb        *ambiguousWrite
	pending    []byte // an accepted outcome whose write was ambiguous and reverted
	seq        int
	publisher  map[int]int
	beforeHub  func()
	replayed   map[int]int
	unresolved int
}

func newLedger(store Store, log *orderLog) *ledgerAdapter {
	return &ledgerAdapter{store: store, log: log, publisher: map[int]int{}, replayed: map[int]int{}}
}

func encodeLedger(entries []ledgerEntry) []byte {
	raw, _ := json.Marshal(entries)
	return raw
}

func decodeLedger(raw []byte) []ledgerEntry {
	var entries []ledgerEntry
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &entries)
	}
	return entries
}

func (a *ledgerAdapter) material(payload []byte) *RecoveryMaterial {
	return &RecoveryMaterial{DriverIDs: []InstanceID{"fam.alpha"}, MaxDesiredRevision: 1, Payload: payload}
}

func (a *ledgerAdapter) BindReadback(arm func()) {
	a.log.add("bind")
	a.mu.Lock()
	a.arm = arm
	a.mu.Unlock()
}

func (a *ledgerAdapter) Restore(_ context.Context, m *RecoveryMaterial) error {
	a.log.add("restore")
	a.mu.Lock()
	defer a.mu.Unlock()
	a.memory = append([]byte(nil), m.Payload...)
	for _, e := range decodeLedger(m.Payload) {
		if e.Accepted {
			a.replayed[e.Seq]++
		} else {
			a.unresolved++
		}
		if e.Seq > a.seq {
			a.seq = e.Seq
		}
	}
	return nil
}

func (a *ledgerAdapter) refusal() error {
	return &Refusal{Code: RefusalUnavailable, Row: 3, Reason: "readback after ambiguous persistence still pending"}
}

// hold records the ambiguous write and arms the resolver after the adapter
// lock is released.
func (a *ledgerAdapter) hold(w *ambiguousWrite, err error) error {
	a.mu.Lock()
	a.amb = w
	if !w.intent {
		a.pending = w.payload
	}
	arm := a.arm
	a.mu.Unlock()
	if arm != nil {
		arm()
	}
	return err
}

// publish runs one intent, one publisher call and one outcome.
func (a *ledgerAdapter) publish(ctx context.Context) (accepted bool, err error) {
	a.mu.Lock()
	if a.amb != nil {
		a.mu.Unlock()
		return false, a.refusal()
	}
	a.seq++
	seq := a.seq
	prev := append([]byte(nil), a.memory...)
	intent := encodeLedger(append(decodeLedger(prev), ledgerEntry{Seq: seq}))
	a.mu.Unlock()

	if err := a.store.CommitRecovery(ctx, a.material(intent)); err != nil {
		if !a.store.AdmissionOpen() {
			return false, a.hold(&ambiguousWrite{payload: intent, prev: prev, intent: true}, err)
		}
		return false, err
	}
	a.mu.Lock()
	a.memory = intent
	before := a.beforeHub
	a.mu.Unlock()
	if before != nil {
		before()
	}
	a.mu.Lock()
	a.publisher[seq]++
	entries := decodeLedger(intent)
	for i := range entries {
		if entries[i].Seq == seq {
			entries[i].Accepted = true
		}
	}
	outcome := encodeLedger(entries)
	a.mu.Unlock()

	if err := a.store.CommitRecovery(ctx, a.material(outcome)); err != nil {
		if !a.store.AdmissionOpen() {
			return true, a.hold(&ambiguousWrite{payload: outcome, prev: intent}, err)
		}
		a.mu.Lock()
		a.pending = outcome
		a.mu.Unlock()
		return true, err
	}
	a.mu.Lock()
	a.memory, a.pending = outcome, nil
	a.mu.Unlock()
	return true, nil
}

// Secure makes a pending accepted outcome durable; with a readback pending it
// refuses retryably and writes nothing.
func (a *ledgerAdapter) Secure(ctx context.Context, _ InstanceID, _ Transition) (*RecoveryMaterial, error) {
	a.mu.Lock()
	if a.amb != nil {
		a.mu.Unlock()
		return nil, a.refusal()
	}
	pending := a.pending
	a.mu.Unlock()
	if pending != nil {
		if err := a.store.CommitRecovery(ctx, a.material(pending)); err != nil {
			return nil, err
		}
		a.mu.Lock()
		a.memory, a.pending = pending, nil
		a.mu.Unlock()
	}
	return nil, nil
}

// ReadbackResolved settles the held material against the committed bytes: an
// intent whose publisher call never ran is not applied, and an accepted outcome
// is adopted exactly when it is committed, otherwise it stays pending.
func (a *ledgerAdapter) ReadbackResolved() {
	committed := a.store.Recovery()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.amb == nil {
		return
	}
	var payload []byte
	if committed != nil {
		payload = committed.Payload
	}
	switch {
	case a.amb.intent && bytes.Equal(payload, a.amb.payload):
		a.memory = a.amb.prev
	case !a.amb.intent && bytes.Equal(payload, a.amb.payload):
		a.memory, a.pending = payload, nil
	}
	a.amb = nil
}

func (a *ledgerAdapter) snapshot() (memory []byte, amb *ambiguousWrite) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]byte(nil), a.memory...), a.amb
}

func (a *ledgerAdapter) publisherCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, c := range a.publisher {
		n += c
	}
	return n
}

// holdStore delays the readback until released, so a test sees the window in
// which the resolver is pending.
type holdStore struct {
	Store
	mu      sync.Mutex
	entered chan struct{}
	release chan struct{}
}

func (h *holdStore) arm() (entered, release chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entered, h.release = make(chan struct{}), make(chan struct{})
	return h.entered, h.release
}

func (h *holdStore) ResolveReadback(ctx context.Context) error {
	h.mu.Lock()
	entered, release := h.entered, h.release
	h.entered, h.release = nil, nil
	h.mu.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return h.Store.ResolveReadback(ctx)
}

// An ambiguous commit of the adapter's own write arms the service's bounded
// readback resolver, without another request. While it is pending the adapter
// refuses retryably and neither writes to the store nor calls the publisher;
// once it settles the adapter's held material matches the exact committed bytes,
// admission reopens, and the next Secure and publication succeed. An intent
// whose publisher call never ran is not applied automatically, and accepted
// outcomes are never applied twice, including after reopening.
func TestAmbiguousAdapterCommitArmsTheServiceReadback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		intent    bool
		persisted bool
	}{
		{"outcome-persisted", false, true},
		{"outcome-not-persisted", false, false},
		{"intent-persisted", true, true},
		{"intent-not-persisted", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hold *holdStore
			var ledger *ledgerAdapter
			f := newFixture(t, svcOpts{drivers: enabled("fam.alpha"), noStart: true,
				wrap: func(s Store) Store { hold = &holdStore{Store: s}; return hold }})
			ledger = newLedger(f.store, f.log)
			// The fixture's service takes the adapter at construction: rebuild it.
			svc, err := NewService(ServiceOptions{Store: f.store, Recovery: ledger, Incarnation: "inc1",
				Drivers:    []ServiceInstance{{InstanceID: "fam.alpha", Executor: f.execs["fam.alpha"]}},
				RetryDelay: func(int) time.Duration { return 2 * time.Millisecond }, OnError: f.errs.add})
			if err != nil {
				t.Fatal(err)
			}
			f.svc.Close()
			f.svc = svc
			t.Cleanup(svc.Close)
			if err := svc.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.waitObserved("fam.alpha", ObservedRunning)
			if got := f.log.list(); len(got) < 2 || got[0] != "bind" || got[1] != "start:fam.alpha" {
				t.Fatalf("the resolver is bound before restoration and admission: %v", got)
			}
			ctx := context.Background()
			// One clean publication first, so the ledger holds a committed entry.
			if _, err := ledger.publish(ctx); err != nil {
				t.Fatal(err)
			}

			retryable := func(what string, err error) {
				t.Helper()
				var rf *Refusal
				if !errors.As(err, &rf) || rf.Code != RefusalUnavailable || !rf.Retryable() {
					t.Fatalf("%s during the readback window must be refused retryably: %v", what, err)
				}
			}
			var previous []byte
			inject := func() {
				previous = append([]byte(nil), f.store.Recovery().Payload...)
				whole, _ := os.ReadFile(f.path)
				f.hooks.mu.Lock()
				f.hooks.failDirSync = 1
				if !tc.persisted {
					f.hooks.afterRename = func(path string) { _ = os.WriteFile(path, whole, 0o644) }
				}
				f.hooks.mu.Unlock()
			}
			if tc.intent {
				inject()
			} else {
				ledger.beforeHub = inject
			}
			entered, release := hold.arm()
			accepted, err := ledger.publish(ctx)
			if err == nil {
				t.Fatal("the ambiguous write reported success")
			}
			_, amb := ledger.snapshot()
			if amb == nil || amb.intent != tc.intent || !bytes.Equal(amb.prev, previousOr(amb, previous, tc.intent)) {
				t.Fatalf("the ambiguous write was not retained as staged: %+v", amb)
			}
			if accepted == tc.intent {
				t.Fatalf("accepted=%v for an ambiguous %s write", accepted, map[bool]string{true: "intent", false: "outcome"}[tc.intent])
			}
			staged := amb.payload
			f.hooks.mu.Lock()
			f.hooks.afterRename = nil
			f.hooks.mu.Unlock()

			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("the ambiguous adapter write did not arm the service resolver")
			}
			writes, calls := hookWrites(f), ledger.publisherCalls()
			_, serr := ledger.Secure(ctx, "fam.alpha", Transition{})
			retryable("Secure", serr)
			_, perr := ledger.publish(ctx)
			retryable("publication", perr)
			if hookWrites(f) != writes || ledger.publisherCalls() != calls {
				t.Fatal("the window reached the store or the publisher")
			}
			if f.store.AdmissionOpen() {
				t.Fatal("admission is open during the readback window")
			}
			if st := f.status("fam.alpha"); st.ObservedState != ObservedRunning {
				t.Fatalf("the committed view must stay available while admission is closed: %+v", st)
			}

			close(release)
			waitFor(t, "readback settled", func() bool {
				_, held := ledger.snapshot()
				return held == nil && f.store.AdmissionOpen()
			})
			committed := f.store.Recovery().Payload
			want := previous
			if tc.persisted {
				want = staged
			}
			if !bytes.Equal(committed, want) {
				t.Fatal("the readback settled on unexpected committed bytes")
			}
			memory, _ := ledger.snapshot()
			switch {
			case tc.intent && tc.persisted:
				if !bytes.Equal(memory, previous) || bytes.Equal(memory, committed) {
					t.Fatal("an adopted intent whose publisher call never ran must stay out of memory")
				}
			case tc.intent:
				if !bytes.Equal(memory, committed) {
					t.Fatal("memory does not match the committed envelope")
				}
			case tc.persisted:
				if !bytes.Equal(memory, committed) {
					t.Fatal("an adopted outcome must be held in memory")
				}
			}

			if _, err := ledger.Secure(ctx, "fam.alpha", Transition{}); err != nil {
				t.Fatalf("Secure after the readback: %v", err)
			}
			if !tc.intent {
				for _, e := range decodeLedger(f.store.Recovery().Payload) {
					if !e.Accepted {
						t.Fatalf("the accepted outcome is not durable after Secure: %+v", e)
					}
				}
			}
			ledger.beforeHub = nil
			calls = ledger.publisherCalls()
			if _, err := ledger.publish(ctx); err != nil {
				t.Fatalf("the next publication: %v", err)
			}
			if ledger.publisherCalls() != calls+1 {
				t.Fatalf("publisher calls %d, want %d", ledger.publisherCalls(), calls+1)
			}
			for _, e := range decodeLedger(f.store.Recovery().Payload) {
				if !e.Accepted {
					t.Fatalf("a pending intent was left in the committed ledger: %+v", e)
				}
			}
			memory, _ = ledger.snapshot()
			if !bytes.Equal(memory, f.store.Recovery().Payload) {
				t.Fatal("memory diverged from the committed envelope")
			}
			for seq, n := range ledger.publisher {
				if n != 1 {
					t.Fatalf("intent %d reached the publisher %d times", seq, n)
				}
			}

			// Loss of the process: a fresh adapter restores from the committed
			// bytes, replaying each accepted entry once and resolving no intent.
			f.svc.Close()
			_ = f.m.Stop(context.Background())
			reopened := openManager(t, f.path, &blockHooks{}, enabled("fam.alpha"))
			t.Cleanup(func() { _ = reopened.Stop(context.Background()) })
			second := newLedger(&managerStore{m: reopened}, &orderLog{})
			svc2, err := NewService(ServiceOptions{Store: &managerStore{m: reopened}, Recovery: second, Incarnation: "inc2",
				Drivers: []ServiceInstance{{InstanceID: "fam.alpha", Executor: &fakeExec{id: "fam.alpha"}}}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(svc2.Close)
			if err := svc2.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			if second.unresolved != 0 || len(second.replayed) == 0 {
				t.Fatalf("restoration resolved %d intents and replayed %v", second.unresolved, second.replayed)
			}
			var seqs []int
			for seq, n := range second.replayed {
				seqs = append(seqs, seq)
				if n != 1 {
					t.Fatalf("accepted outcome %d was applied %d times", seq, n)
				}
			}
			sort.Ints(seqs)
			if len(seqs) != len(decodeLedger(f.store.Recovery().Payload)) {
				t.Fatalf("replayed %v of the committed ledger", seqs)
			}
		})
	}
}

// previousOr returns the previous payload the ambiguous write is expected to
// have been staged over.
func previousOr(amb *ambiguousWrite, previous []byte, intent bool) []byte {
	if intent {
		return previous
	}
	return amb.prev
}

func hookWrites(f *svcFixture) int {
	f.hooks.mu.Lock()
	defer f.hooks.mu.Unlock()
	return f.hooks.writes
}
