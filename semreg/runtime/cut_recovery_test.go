package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/sources"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func instanceOf(name string) life.InstanceID {
	return life.Ref{Driver: testKind, Instance: driver.InstanceKey(name)}.ID()
}

type quietExec struct{}

func (quietExec) Start(context.Context, life.LifecycleCorrelation) error { return nil }

func (quietExec) Stop(context.Context, life.LifecycleCorrelation) life.StopOutcome {
	return life.StopOutcome{NativeClosed: true, SemanticCleaned: true}
}

func registerIDs(t *testing.T, store life.ControlStore, enabled bool, ids ...life.InstanceID) {
	t.Helper()
	configs := make([]life.ControlInstance, len(ids))
	for i, id := range ids {
		configs[i] = life.ControlInstance{InstanceID: id, Enabled: enabled}
	}
	if _, err := store.Register(context.Background(), configs); err != nil {
		t.Fatal(err)
	}
}

func refusalOf(err error) *life.Refusal {
	var rf *life.Refusal
	if errors.As(err, &rf) {
		return rf
	}
	return nil
}

func requireRetryable(t *testing.T, what string, err error) {
	t.Helper()
	rf := refusalOf(err)
	if rf == nil || rf.Code != life.RefusalUnavailable || !rf.Retryable() {
		t.Fatalf("%s during the readback window must be refused retryably: %v", what, err)
	}
}

// gateStore is the control store the recovery adapter writes, with one
// ambiguous commit: admission closes, the staged bytes stay unexposed until
// ResolveReadback, and a persisted commit becomes readable only then.
type gateStore struct {
	*life.MemoryStore
	mu       sync.Mutex
	closed   bool
	fail     bool
	persist  bool
	failOpen bool
	openFail int
	held     *life.RecoveryMaterial
	entered  chan struct{}
	release  chan struct{}
	writes   int
}

func (g *gateStore) CommitRecovery(ctx context.Context, material *life.RecoveryMaterial) error {
	g.mu.Lock()
	g.writes++
	if g.failOpen {
		g.mu.Unlock()
		return &life.Refusal{Code: life.RefusalUnavailable, Row: 3, Reason: "secure write failed"}
	}
	if g.openFail > 0 {
		g.openFail--
		g.mu.Unlock()
		return &life.Refusal{Code: life.RefusalUnavailable, Row: 3, Reason: "outcome write failed"}
	}
	if g.fail {
		g.fail = false
		g.closed = true
		g.held = cloneMaterial(material)
		g.mu.Unlock()
		return &life.Refusal{Code: life.RefusalUnavailable, Row: 3, Reason: "ambiguous persistence: directory sync"}
	}
	g.mu.Unlock()
	return g.MemoryStore.CommitRecovery(ctx, material)
}

func (g *gateStore) AdmissionOpen() bool {
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	if closed {
		return false
	}
	return g.MemoryStore.AdmissionOpen()
}

func (g *gateStore) ResolveReadback(ctx context.Context) error {
	g.mu.Lock()
	entered, release := g.entered, g.release
	g.entered, g.release = nil, nil
	held, persist := g.held, g.persist
	g.held = nil
	g.mu.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if held != nil && persist {
		if err := g.MemoryStore.CommitRecovery(ctx, held); err != nil {
			return err
		}
	}
	g.mu.Lock()
	g.closed = false
	g.mu.Unlock()
	return nil
}

func (g *gateStore) arm(persist bool) (entered, release chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fail = true
	g.persist = persist
	g.entered, g.release = make(chan struct{}), make(chan struct{})
	return g.entered, g.release
}

func (g *gateStore) writeCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.writes
}

func cloneMaterial(m *life.RecoveryMaterial) *life.RecoveryMaterial {
	if m == nil {
		return nil
	}
	out := *m
	out.DriverIDs = append([]life.InstanceID(nil), m.DriverIDs...)
	out.Payload = append([]byte(nil), m.Payload...)
	return &out
}

func journalOf(t *testing.T, payload []byte) []journalRecord {
	t.Helper()
	var env diskEnv
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatal(err)
	}
	var records []journalRecord
	for _, owner := range env.Owners {
		var part []journalRecord
		if err := json.Unmarshal(owner.Records, &part); err != nil {
			t.Fatal(err)
		}
		records = append(records, part...)
	}
	return records
}

func TestRecoveryDurablePublishWithdrawAndRestore(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	alphaID := instanceOf("unit.alpha")
	betaID := instanceOf("unit.beta")
	dir := t.TempDir()
	store, err := life.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	registerIDs(t, store, false, alphaID, betaID)
	recovery := svc.BindRecovery(store)
	alpha := openPlain(t, svc, "unit.alpha", "asset:alpha", "source:alpha", "epoch:alpha", "1")
	beta := openPlain(t, svc, "unit.beta", "asset:beta", "source:beta", "epoch:beta", "1")
	alphaNode := memberNode(t, svc, "unit.alpha", "slot.alpha")
	betaNode := memberNode(t, svc, "unit.beta", "slot.beta")
	alphaFact := keepFresh(observedFact(alpha.spec, "candidate:alpha", "binding:alpha", "1", "32165", clock.Now(), 100))
	betaFact := keepFresh(observedFact(beta.spec, "candidate:beta", "binding:beta", "1", "11111", clock.Now(), 100))
	alphaSource := declareFact(alphaNode, alphaNode, "path.alpha", 30, alphaFact)
	betaSource := declareFact(betaNode, betaNode, "path.beta", 30, betaFact)
	claim := driver.ManufacturerSerialClaim{Member: alphaNode, Manufacturer: "maker.alpha", Serial: "token.alpha", Proof: []v1.EvidenceRef{testEvidence("proof:alpha")}}
	publishWithSerial(t, alpha, "binding:alpha", alphaSource, alphaFact, claim)
	publishFact(t, beta, "binding:beta", betaSource, betaFact)
	foreign := driver.ManufacturerSerialClaim{Member: alphaNode, Manufacturer: "maker.foreign", Serial: "token.foreign", Proof: []v1.EvidenceRef{testEvidence("proof:foreign")}}
	if err := commitErr(beta, func(b *v1.PublicationBatch) {}, func(tx *transaction) {
		if err := tx.AdmitManufacturerSerial(foreign); err != nil {
			t.Fatal(err)
		}
	}); !errors.Is(err, identity.ErrNotOwner) {
		t.Fatalf("colliding claim %v", err)
	}
	commit(t, alpha, func(b *v1.PublicationBatch) {
		b.FactWithdrawals = []v1.CandidateID{"candidate:alpha"}
	}, nil)
	view, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := receiptNS(svc, view, betaSource)
	if receipt == "" || valueCoefficient(svc.Element(betaSource.For, view).Value) != "11111" {
		t.Fatalf("beta value %s receipt %s", valueCoefficient(svc.Element(betaSource.For, view).Value), receipt)
	}
	if sourceState(svc.Element(alphaSource.For, view), "path.alpha").Disposition != sources.DispositionWithdrawn || svc.Element(alphaSource.For, view).Value != nil {
		t.Fatal("the withdrawal did not clear the alpha value")
	}
	payload := append([]byte(nil), store.Recovery().Payload...)
	records := journalOf(t, payload)
	if len(records) < 2 || records[0].Batch.FactUpserts[0].CandidateID != "candidate:alpha" || valueCoefficient(records[0].Batch.FactUpserts[0].Value) != "32165" {
		t.Fatal("the committed journal is not the accepted batch")
	}
	var withdrawn bool
	for _, record := range records {
		for _, id := range record.Batch.FactWithdrawals {
			if id == "candidate:alpha" && record.Applied && record.Outcome == "accepted" {
				withdrawn = true
			}
		}
	}
	if !withdrawn {
		t.Fatal("the withdrawal was not the committed batch")
	}
	if recovery.Applies() != 3 {
		t.Fatalf("publisher applies %d, want the publication, the beta fact and the withdrawal", recovery.Applies())
	}
	position := svc.positionUnsafe()
	store.Close()
	store.Release()

	clock.advance(time.Second)
	reopened, err := life.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reopened.Close()
		reopened.Release()
	})
	if !bytes.Equal(reopened.Recovery().Payload, payload) {
		t.Fatal("reopening the control store changed the committed bytes")
	}
	registerIDs(t, reopened, false, alphaID, betaID)
	restored := openService(t, clock)
	memberNode(t, restored, "unit.alpha", "slot.alpha")
	memberNode(t, restored, "unit.beta", "slot.beta")
	openPlain(t, restored, "unit.alpha", "asset:alpha", "source:alpha", "epoch:alpha", "1")
	openPlain(t, restored, "unit.beta", "asset:beta", "source:beta", "epoch:beta", "1")
	again := restored.BindRecovery(reopened)
	lifeSvc, err := life.NewService(life.ServiceOptions{
		Store: reopened, Recovery: again, Incarnation: "inc.restore",
		Drivers: []life.ServiceInstance{
			{InstanceID: alphaID, Executor: quietExec{}},
			{InstanceID: betaID, Executor: quietExec{}},
		},
		OperationBudget: 30 * time.Second, MaxAttempts: 1,
		RetryDelay: func(int) time.Duration { return time.Millisecond },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lifeSvc.Close)
	if err := lifeSvc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if again.Applies() != 0 {
		t.Fatalf("restore applied the publication again: %d", again.Applies())
	}
	revived, err := restored.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if valueCoefficient(restored.Element(betaSource.For, revived).Value) != "11111" || receiptNS(restored, revived, betaSource) != receipt {
		t.Fatalf("restored beta value %s receipt %s, want 11111 %s", valueCoefficient(restored.Element(betaSource.For, revived).Value), receiptNS(restored, revived, betaSource), receipt)
	}
	if sourceState(restored.Element(alphaSource.For, revived), "path.alpha").Disposition != sources.DispositionWithdrawn {
		t.Fatal("restore revived the withdrawn alpha value")
	}
	exported, err := restored.Ledger().Export()
	if err != nil || !bytes.Contains(exported, []byte("maker.alpha")) || !bytes.Contains(exported, []byte("token.alpha")) || bytes.Contains(exported, []byte("token.foreign")) {
		t.Fatal("restore did not keep the owner's serial or kept the colliding claim")
	}
	once := restored.positionUnsafe()
	snap := assetSnap(t, restored, "asset:beta").SnapshotID
	if err := again.Restore(context.Background(), reopened.Recovery()); err != nil {
		t.Fatal(err)
	}
	if restored.positionUnsafe() != once || assetSnap(t, restored, "asset:beta").SnapshotID != snap {
		t.Fatal("replaying an accepted step duplicated the publication")
	}
	if restored.positionUnsafe() == 0 || position == 0 {
		t.Fatal("the restored history has no commit position")
	}
	betaAgain := openPlain(t, restored, "unit.beta", "asset:beta", "source:beta", "epoch:beta", "2")
	if err := commitErr(betaAgain, func(b *v1.PublicationBatch) {
		b.FactUpserts = []v1.FactCandidate{keepFresh(observedFact(betaAgain.spec, "candidate:beta", "binding:beta", "1", "22222", clock.Now(), 100))}
	}, nil); err == nil {
		t.Fatal("a generation that does not own the current epoch published")
	}

	corrupt := append([]byte(nil), payload...)
	var env diskEnv
	if err := json.Unmarshal(corrupt, &env); err != nil {
		t.Fatal(err)
	}
	for i := range env.Owners {
		if env.Owners[i].Instance == betaID {
			env.Owners[i].Records = json.RawMessage("0")
		}
	}
	corrupt, err = json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	failed := openService(t, clock)
	memberNode(t, failed, "unit.alpha", "slot.alpha")
	memberNode(t, failed, "unit.beta", "slot.beta")
	openPlain(t, failed, "unit.alpha", "asset:alpha", "source:alpha", "epoch:alpha", "1")
	openPlain(t, failed, "unit.beta", "asset:beta", "source:beta", "epoch:beta", "1")
	failedRec := failed.BindRecovery(life.NewMemoryStore())
	err = failedRec.Restore(context.Background(), &life.RecoveryMaterial{DriverIDs: []life.InstanceID{alphaID, betaID}, Payload: corrupt})
	var scoped *life.RestoreError
	if !errors.As(err, &scoped) || len(scoped.Drivers) != 1 || scoped.Drivers[0] != betaID {
		t.Fatalf("failed owner %v", err)
	}
	if _, err := failed.OpenSession(SessionSpec{
		Driver: testKind, Instance: "unit.beta", Asset: "asset:beta", Source: "source:beta", Epoch: "epoch:beta", Generation: "3",
	}); v1.ErrorIdentifier(err) != v1.StaleDriverGeneration {
		t.Fatalf("failed owner admitted %v", err)
	}
	alphaBack := openPlain(t, failed, "unit.alpha", "asset:alpha", "source:alpha", "epoch:alpha", "2")
	cut, err := failed.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapOf(cut.Members, "asset:beta"); ok || valueCoefficient(failed.Element(alphaSource.For, cut).Value) != "" {
		t.Fatal("the failed owner was current or the restored alpha value returned")
	}
	if alphaBack == nil {
		t.Fatal("the unaffected owner did not reopen")
	}
	continuity, cerr := failedRec.Continuity()
	if cerr != nil || continuity == 0 {
		t.Fatalf("continuity %d %v", continuity, cerr)
	}
	var broken diskEnv
	if err := json.Unmarshal(payload, &broken); err != nil {
		t.Fatal(err)
	}
	broken.Anchor = []byte("foreign")
	raw, err := json.Marshal(broken)
	if err != nil {
		t.Fatal(err)
	}
	foreignSvc := openService(t, clock)
	memberNode(t, foreignSvc, "unit.alpha", "slot.alpha")
	memberNode(t, foreignSvc, "unit.beta", "slot.beta")
	openPlain(t, foreignSvc, "unit.alpha", "asset:alpha", "source:alpha", "epoch:alpha", "1")
	openPlain(t, foreignSvc, "unit.beta", "asset:beta", "source:beta", "epoch:beta", "1")
	foreignRec := foreignSvc.BindRecovery(life.NewMemoryStore())
	if err := foreignRec.Restore(context.Background(), &life.RecoveryMaterial{Payload: raw}); err != nil {
		t.Fatal(err)
	}
	if elapsed, err := foreignRec.Continuity(); elapsed != 0 || err == nil {
		t.Fatalf("foreign anchor elapsed %d err %v", elapsed, err)
	}
}

func TestRecoveryRestoresSharedAssetInCommitOrder(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	store := life.NewMemoryStore()
	registerIDs(t, store, false, instanceOf("unit.primary"), instanceOf("unit.mirror"))
	svc.BindRecovery(store)
	primaryNode := memberNode(t, svc, "unit.primary", "slot.primary")
	mirrorNode := memberNode(t, svc, "unit.mirror", "slot.mirror")
	primary := openPlain(t, svc, "unit.primary", "asset:shared", "source:primary", "epoch:primary", "1")
	mirror := openPlain(t, svc, "unit.mirror", "asset:shared", "source:mirror", "epoch:mirror", "1")
	primaryFact := keepFresh(observedFact(primary.spec, "candidate:primary", "binding:primary", "1", "11111", clock.Now(), 100))
	mirrorFact := keepFresh(observedFact(mirror.spec, "candidate:mirror", "binding:mirror", "1", "22222", clock.Now(), 100))
	revised := primaryFact
	revised.Revision = "2"
	value := kelvinQuantity("33333", -2)
	revised.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: &value}
	publishFact(t, primary, "binding:primary", declareFact(primaryNode, primaryNode, "path.primary", 30, primaryFact), primaryFact)
	publishFact(t, mirror, "binding:mirror", declareFact(mirrorNode, mirrorNode, "path.mirror", 30, mirrorFact), mirrorFact)
	commit(t, primary, func(b *v1.PublicationBatch) {
		b.FactUpserts = []v1.FactCandidate{revised}
	}, func(tx *transaction) {
		if err := tx.StageSample(driver.Sample{From: declareFact(primaryNode, primaryNode, "path.primary", 30, revised), Candidate: revised}); err != nil {
			t.Fatal(err)
		}
	})
	payload := append([]byte(nil), store.Recovery().Payload...)
	restored := openService(t, clock)
	openPlain(t, restored, "unit.primary", "asset:shared", "source:primary", "epoch:primary", "1")
	openPlain(t, restored, "unit.mirror", "asset:shared", "source:mirror", "epoch:mirror", "1")
	if err := restored.BindRecovery(life.NewMemoryStore()).Restore(context.Background(), &life.RecoveryMaterial{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	snap := assetSnap(t, restored, "asset:shared")
	if coefficientOf(snap, "candidate:primary") != "33333" || coefficientOf(snap, "candidate:mirror") != "22222" {
		t.Fatalf("restored primary %s mirror %s", coefficientOf(snap, "candidate:primary"), coefficientOf(snap, "candidate:mirror"))
	}
}

func TestRecoveryConcurrentCommitsKeepEveryAcceptedRecord(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	store := life.NewMemoryStore()
	alphaID := instanceOf("unit.alpha")
	registerIDs(t, store, false, alphaID, instanceOf("unit.beta"))
	recovery := svc.BindRecovery(store)
	alphaNode := memberNode(t, svc, "unit.alpha", "slot.alpha")
	betaNode := memberNode(t, svc, "unit.beta", "slot.beta")
	alpha := openPlain(t, svc, "unit.alpha", "asset:alpha", "source:alpha", "epoch:alpha", "1")
	beta := openPlain(t, svc, "unit.beta", "asset:beta", "source:beta", "epoch:beta", "1")
	alphaFact := keepFresh(observedFact(alpha.spec, "candidate:alpha", "binding:alpha", "1", "11111", clock.Now(), 100))
	betaFact := keepFresh(observedFact(beta.spec, "candidate:beta", "binding:beta", "1", "22222", clock.Now(), 100))
	alphaSource := declareFact(alphaNode, alphaNode, "path.alpha", 30, alphaFact)
	betaSource := declareFact(betaNode, betaNode, "path.beta", 30, betaFact)
	alphaDone := make(chan error, 1)
	betaDone := make(chan error, 1)
	secDone := make(chan error, 1)
	var once sync.Once
	recovery.beforeApply = func() {
		once.Do(func() {
			go func() {
				betaDone <- commitErr(beta, func(b *v1.PublicationBatch) {
					b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(beta.spec, "registry:beta")}
					b.BindingUpserts = []v1.NativeBinding{bindingOf(beta.spec, "binding:beta", "native-resource:binding:beta")}
					b.IdentityLinkUpserts = []v1.IdentityLink{linkOf(beta.spec, "binding:beta", "basis:beta", nil)}
					b.FactUpserts = []v1.FactCandidate{betaFact}
				}, func(tx *transaction) {
					if err := tx.DeclareSource(betaSource); err != nil {
						panic(err)
					}
					if err := tx.StageSample(driver.Sample{From: betaSource, Candidate: betaFact}); err != nil {
						panic(err)
					}
				})
			}()
			go func() {
				_, err := recovery.Secure(context.Background(), alphaID, life.Transition{Phase: life.PhaseCleanup, NativeClosePending: false, SemanticCleanupPending: true, AttemptsUsed: 1})
				secDone <- err
			}()
			time.Sleep(400 * time.Millisecond)
		})
	}
	go func() {
		alphaDone <- commitErr(alpha, func(b *v1.PublicationBatch) {
			b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(alpha.spec, "registry:alpha")}
			b.BindingUpserts = []v1.NativeBinding{bindingOf(alpha.spec, "binding:alpha", "native-resource:binding:alpha")}
			b.IdentityLinkUpserts = []v1.IdentityLink{linkOf(alpha.spec, "binding:alpha", "basis:alpha", nil)}
			b.FactUpserts = []v1.FactCandidate{alphaFact}
		}, func(tx *transaction) {
			if err := tx.DeclareSource(alphaSource); err != nil {
				panic(err)
			}
			if err := tx.StageSample(driver.Sample{From: alphaSource, Candidate: alphaFact}); err != nil {
				panic(err)
			}
		})
	}()
	timeout := time.After(8 * time.Second)
	var alphaErr, betaErr, secureErr error
	for n := 0; n < 3; {
		select {
		case alphaErr = <-alphaDone:
			n++
		case betaErr = <-betaDone:
			n++
		case secureErr = <-secDone:
			n++
		case <-timeout:
			t.Fatal("the commits did not finish")
		}
	}
	if alphaErr != nil || betaErr != nil || secureErr != nil {
		t.Fatalf("alpha %v beta %v secure %v", alphaErr, betaErr, secureErr)
	}
	if !recovery.Obligation(alphaID).NativeClosed {
		t.Fatal("Secure lost the recorded close")
	}
	payload := append([]byte(nil), store.Recovery().Payload...)
	restored := openService(t, clock)
	openPlain(t, restored, "unit.alpha", "asset:alpha", "source:alpha", "epoch:alpha", "1")
	openPlain(t, restored, "unit.beta", "asset:beta", "source:beta", "epoch:beta", "1")
	if err := restored.BindRecovery(life.NewMemoryStore()).Restore(context.Background(), &life.RecoveryMaterial{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if coefficientOf(assetSnap(t, restored, "asset:alpha"), "candidate:alpha") != "11111" || coefficientOf(assetSnap(t, restored, "asset:beta"), "candidate:beta") != "22222" {
		t.Fatal("restore dropped an accepted record")
	}
}

func TestRecoveryAmbiguousIntentAndOutcome(t *testing.T) {
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
			clock := newTestClock()
			svc := openService(t, clock)
			id := instanceOf("unit.readback")
			inner := life.NewMemoryStore()
			registerIDs(t, inner, false, id)
			gate := &gateStore{MemoryStore: inner}
			recovery := svc.BindRecovery(gate)
			node := memberNode(t, svc, "unit.readback", "slot.readback")
			session := openPlain(t, svc, "unit.readback", "asset:readback", "source:readback", "epoch:readback", "1")
			fact := keepFresh(observedFact(session.spec, "candidate:readback", "binding:readback", "1", "32165", clock.Now(), 100))
			source := declareFact(node, node, "path.readback", 30, fact)
			errs := make(chan error, 4)
			lifeSvc, err := life.NewService(life.ServiceOptions{
				Store: gate, Recovery: recovery, Incarnation: "inc.readback",
				Drivers:         []life.ServiceInstance{{InstanceID: id, Executor: quietExec{}}},
				OperationBudget: 30 * time.Second, MaxAttempts: 1,
				RetryDelay: func(int) time.Duration { return time.Millisecond },
				OnError: func(err error) {
					select {
					case errs <- err:
					default:
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(lifeSvc.Close)
			if err := lifeSvc.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			publishFact(t, session, "binding:readback", source, fact)
			beforeApplies := recovery.Applies()
			beforePosition := svc.positionUnsafe()
			previous := append([]byte(nil), gate.Recovery().Payload...)
			var entered, release chan struct{}
			if tc.intent {
				entered, release = gate.arm(tc.persisted)
			} else {
				recovery.beforeApply = func() {
					previous = append([]byte(nil), gate.Recovery().Payload...)
					entered, release = gate.arm(tc.persisted)
				}
			}
			next := fact
			next.Revision = "2"
			revised := kelvinQuantity("32265", -2)
			next.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: &revised}
			err = commitErr(session, func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{next} }, func(tx *transaction) {
				if err := tx.StageSample(driver.Sample{From: source, Candidate: next}); err != nil {
					t.Fatal(err)
				}
			})
			requireRetryable(t, "publication", err)
			recovery.mu.Lock()
			amb := recovery.amb
			var staged, prior []byte
			if amb != nil {
				staged = append([]byte(nil), amb.payload...)
				prior = append([]byte(nil), amb.prev...)
			}
			intent := amb != nil && amb.intent
			recovery.mu.Unlock()
			if amb == nil || intent != tc.intent || !bytes.Equal(prior, previous) {
				t.Fatalf("ambiguous write intent %v prior match %v", intent, amb != nil && bytes.Equal(prior, previous))
			}
			if tc.intent && recovery.Applies() != beforeApplies {
				t.Fatal("an unknown intent called the publisher")
			}
			if !tc.intent && recovery.Applies() != beforeApplies+1 {
				t.Fatalf("accepted outcome applies %d", recovery.Applies())
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("the ambiguous write did not arm the resolver")
			}
			writes, calls := gate.writeCount(), recovery.Applies()
			position := svc.positionUnsafe()
			_, serr := recovery.Secure(context.Background(), id, life.Transition{})
			requireRetryable(t, "Secure", serr)
			if err := commitErr(session, func(b *v1.PublicationBatch) {}, nil); refusalOf(err) == nil {
				t.Fatalf("publication during the window: %v", err)
			}
			if gate.writeCount() != writes || recovery.Applies() != calls || svc.positionUnsafe() != position {
				t.Fatal("the window wrote the store or applied the publisher")
			}
			if gate.AdmissionOpen() {
				t.Fatal("admission is open during the readback window")
			}
			close(release)
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				recovery.mu.Lock()
				pending := recovery.amb != nil
				recovery.mu.Unlock()
				if !pending && gate.AdmissionOpen() {
					break
				}
				time.Sleep(time.Millisecond)
			}
			recovery.mu.Lock()
			pending := recovery.amb != nil
			memory := append([]byte(nil), recovery.memory...)
			recovery.mu.Unlock()
			if pending || !gate.AdmissionOpen() {
				t.Fatal("the readback did not settle")
			}
			committed := append([]byte(nil), gate.Recovery().Payload...)
			want := previous
			if tc.persisted {
				want = staged
			}
			if !bytes.Equal(committed, want) {
				t.Fatal("the readback settled on unexpected committed bytes")
			}
			switch {
			case tc.intent && tc.persisted:
				if !bytes.Equal(memory, previous) || recovery.Applies() != beforeApplies {
					t.Fatal("a persisted intent was applied or entered memory")
				}
				quiet := openService(t, clock)
				memberNode(t, quiet, "unit.readback", "slot.readback")
				openPlain(t, quiet, "unit.readback", "asset:readback", "source:readback", "epoch:readback", "1")
				held := quiet.BindRecovery(life.NewMemoryStore())
				if err := held.Restore(context.Background(), gate.Recovery()); err != nil {
					t.Fatal(err)
				}
				if coefficientOf(assetSnap(t, quiet, "asset:readback"), "candidate:readback") != "32165" {
					t.Fatal("restoring an unapplied intent changed the fact")
				}
			case tc.intent:
				if !bytes.Equal(memory, committed) || recovery.Applies() != beforeApplies {
					t.Fatal("a reverted intent changed memory or the publisher")
				}
			case tc.persisted:
				if !bytes.Equal(memory, committed) || svc.positionUnsafe() != position {
					t.Fatal("an adopted outcome was applied again")
				}
			default:
				if svc.positionUnsafe() != position || recovery.Applies() != calls {
					t.Fatal("a reverted outcome was applied again")
				}
			}
			recovery.beforeApply = nil
			if _, err := recovery.Secure(context.Background(), id, life.Transition{}); err != nil {
				t.Fatalf("Secure after the readback: %v", err)
			}
			calls = recovery.Applies()
			later := keepFresh(observedFact(session.spec, "candidate:later", "binding:readback", "1", "33333", clock.Now(), 100))
			commit(t, session, func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{later} }, func(tx *transaction) {
				if err := tx.DeclareSource(source); err != nil {
					t.Fatal(err)
				}
				if err := tx.StageSample(driver.Sample{From: source, Candidate: later}); err != nil {
					t.Fatal(err)
				}
			})
			if recovery.Applies() != calls+1 {
				t.Fatalf("publisher calls %d, want %d", recovery.Applies(), calls+1)
			}
			if coefficientOf(assetSnap(t, svc, "asset:readback"), "candidate:later") != "33333" {
				t.Fatal("publication after settlement did not commit")
			}
			if beforePosition == 0 {
				t.Fatal("the cursor had no commit")
			}
			select {
			case err := <-errs:
				t.Fatalf("resolver error %v", err)
			default:
			}
		})
	}
}

func TestRecoveryPendingOutcomePrecedesNextCommit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		readback bool
	}{
		{name: "outcome write leaves admission open"},
		{name: "readback reverts the outcome", readback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := newTestClock()
			svc := openService(t, clock)
			id := instanceOf("unit.held")
			inner := life.NewMemoryStore()
			registerIDs(t, inner, false, id)
			gate := &gateStore{MemoryStore: inner}
			recovery := svc.BindRecovery(gate)
			node := memberNode(t, svc, "unit.held", "slot.held")
			session := openPlain(t, svc, "unit.held", "asset:held", "source:held", "epoch:held", "1")
			first := keepFresh(observedFact(session.spec, "candidate:first", "binding:held", "1", "11111", clock.Now(), 100))
			source := declareFact(node, node, "path.held", 30, first)
			publishFact(t, session, "binding:held", source, first)
			held := first
			held.CandidateID = "candidate:held"
			held.Origin.OriginID = "origin:candidate:held"
			heldValue := kelvinQuantity("22222", -2)
			held.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: &heldValue}
			var entered, release chan struct{}
			recovery.beforeApply = func() {
				if tc.readback {
					entered, release = gate.arm(false)
					return
				}
				gate.mu.Lock()
				gate.openFail = 1
				gate.mu.Unlock()
			}
			err := commitErr(session, func(b *v1.PublicationBatch) {
				b.FactUpserts = []v1.FactCandidate{held}
			}, func(tx *transaction) {
				if err := tx.StageSample(driver.Sample{From: source, Candidate: held}); err != nil {
					t.Fatal(err)
				}
			})
			requireRetryable(t, "outcome", err)
			recovery.beforeApply = nil
			if coefficientOf(assetSnap(t, svc, "asset:held"), "candidate:held") != "22222" {
				t.Fatal("the accepted publication was not applied")
			}
			if tc.readback {
				close(release)
				if err := gate.ResolveReadback(context.Background()); err != nil {
					t.Fatal(err)
				}
				recovery.ReadbackResolved()
				if entered == nil {
					t.Fatal("the outcome write did not arm readback")
				}
			}
			recovery.mu.Lock()
			pending := append([]byte(nil), recovery.pending...)
			recovery.mu.Unlock()
			if pending == nil || recovery.blocked() {
				t.Fatal("the accepted outcome is not waiting to be settled")
			}
			later := first
			later.CandidateID = "candidate:later"
			later.Origin.OriginID = "origin:candidate:later"
			laterValue := kelvinQuantity("33333", -2)
			later.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: &laterValue}
			commit(t, session, func(b *v1.PublicationBatch) {
				b.FactUpserts = []v1.FactCandidate{later}
			}, func(tx *transaction) {
				if err := tx.StageSample(driver.Sample{From: source, Candidate: later}); err != nil {
					t.Fatal(err)
				}
			})
			got := acceptedCandidates(t, gate.Recovery().Payload)
			if !got["candidate:first"] || !got["candidate:held"] || !got["candidate:later"] {
				t.Fatalf("committed outcomes %+v", got)
			}
			restored := openService(t, clock)
			memberNode(t, restored, "unit.held", "slot.held")
			openPlain(t, restored, "unit.held", "asset:held", "source:held", "epoch:held", "1")
			if err := restored.BindRecovery(life.NewMemoryStore()).Restore(context.Background(), gate.Recovery()); err != nil {
				t.Fatal(err)
			}
			snap := assetSnap(t, restored, "asset:held")
			if coefficientOf(snap, "candidate:first") != "11111" || coefficientOf(snap, "candidate:held") != "22222" || coefficientOf(snap, "candidate:later") != "33333" {
				t.Fatalf("restored first %s held %s later %s", coefficientOf(snap, "candidate:first"), coefficientOf(snap, "candidate:held"), coefficientOf(snap, "candidate:later"))
			}
		})
	}
}

func acceptedCandidates(t *testing.T, payload []byte) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	for _, record := range journalOf(t, payload) {
		if record.Outcome != "accepted" || !record.Applied {
			continue
		}
		for _, fact := range record.Batch.FactUpserts {
			got[string(fact.CandidateID)] = true
		}
	}
	return got
}

func TestRecoverySecureFailureKeepsSlotAndObligations(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	store := life.NewMemoryStore()
	id := life.InstanceID("unit.prune")
	registerIDs(t, store, true, id)
	if _, err := store.Decide(context.Background(), life.MethodStop, life.Request{InstanceID: id, ExpectedRevision: 1}, func(in life.PlanInput) (life.SlotPlan, *life.Refusal) {
		if in.Current != nil {
			return life.SlotPlan{}, &life.Refusal{Code: life.RefusalTransitionBusy, Row: 8, Reason: "slot occupied"}
		}
		return life.SlotPlan{Current: &life.Transition{Kind: life.KindStop, Phase: life.PhasePending}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	current, err := store.Driver(id)
	if err != nil || current.Current == nil {
		t.Fatal(err)
	}
	active := *current.Current
	active.Phase = life.PhaseActive
	active.Epoch = "e.1"
	active.Generation = 1
	active.NativeClosePending = true
	active.SemanticCleanupPending = true
	active.DeadlineUTC = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.CommitWork(context.Background(), id, &active, nil); err != nil {
		t.Fatal(err)
	}
	cleanup := active
	cleanup.Phase = life.PhaseCleanup
	cleanup.NativeClosePending = false
	cleanup.SemanticCleanupPending = false
	cleanup.AttemptsUsed = 1
	if err := store.CommitWork(context.Background(), id, &cleanup, nil); err != nil {
		t.Fatal(err)
	}
	gate := &gateStore{MemoryStore: store, failOpen: true}
	recovery := svc.BindRecovery(gate)
	native, err := recovery.Secure(context.Background(), id, life.Transition{Phase: life.PhaseCleanup, NativeClosePending: false, SemanticCleanupPending: true, AttemptsUsed: 1})
	if err == nil || native != nil {
		t.Fatalf("a failing Secure returned material %v %v", native, err)
	}
	if ob := recovery.Obligation(id); ob.NativeClosed || ob.SemanticCleaned || ob.AttemptsUsed != 0 {
		t.Fatalf("a failing Secure recorded %+v", ob)
	}
	seen := make(chan struct{}, 1)
	lifeSvc, err := life.NewService(life.ServiceOptions{
		Store: gate, Recovery: recovery, Incarnation: "inc.prune",
		Drivers:         []life.ServiceInstance{{InstanceID: id, Executor: quietExec{}}},
		OperationBudget: 30 * time.Second, MaxAttempts: 1,
		RetryDelay: func(int) time.Duration { return time.Millisecond },
		OnError: func(err error) {
			var semantic *life.SemanticError
			if errors.As(err, &semantic) && semantic.Op == "secure" {
				select {
				case seen <- struct{}{}:
				default:
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lifeSvc.Close)
	if err := lifeSvc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("the reconciler pruned without a successful Secure")
	}
	held, err := store.Driver(id)
	if err != nil || held.Current == nil || held.Current.Phase != life.PhaseCleanup || held.Current.AttemptsUsed != 1 || held.Current.NativeClosePending || held.Current.SemanticCleanupPending {
		t.Fatalf("slot after a failed Secure: %+v %v", held.Current, err)
	}

	plain := life.NewMemoryStore()
	registerIDs(t, plain, false, id)
	okSvc := openService(t, clock)
	okRec := okSvc.BindRecovery(plain)
	if _, err := okRec.Secure(context.Background(), id, life.Transition{Phase: life.PhaseCleanup, NativeClosePending: false, SemanticCleanupPending: true, AttemptsUsed: 1}); err != nil {
		t.Fatal(err)
	}
	if ob := okRec.Obligation(id); !ob.NativeClosed || ob.SemanticCleaned || ob.AttemptsUsed != 1 {
		t.Fatalf("native close %+v", ob)
	}
	if _, err := okRec.Secure(context.Background(), id, life.Transition{Phase: life.PhaseCleanup, NativeClosePending: true, SemanticCleanupPending: false, AttemptsUsed: 2}); err != nil {
		t.Fatal(err)
	}
	if ob := okRec.Obligation(id); !ob.NativeClosed || !ob.SemanticCleaned || ob.AttemptsUsed != 2 {
		t.Fatalf("independent obligations %+v", ob)
	}
}
