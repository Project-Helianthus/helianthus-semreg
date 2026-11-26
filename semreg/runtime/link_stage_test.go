package runtime

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

const (
	endCommit = "commit"
	endAbort  = "abort"
	endReject = "reject"
)

// linkPlan drives a binder instance's publication through driver.Session only:
// it stages the claims and, when asked, a sample of the declared source, then ends the
// transaction as ending directs, and records what the session reported.
type linkPlan struct {
	ending string
	// stageSample has the driver stage its own sample through the public
	// transaction; silent makes the instance publish nothing at start.
	stageSample bool
	silent      bool
	staged      int
	claims      func(root driver.NodeRef, subject v1.ResourceID) []driver.LinkClaim

	mu        sync.Mutex
	commitErr error
	stageErr  error
	prepared  v1.PublicationBatch
}

func (p *linkPlan) finish(ctx context.Context, s driver.Session, tx driver.PublicationTransaction, root driver.NodeRef, subject v1.ResourceID, source driver.Source, candidate v1.FactCandidate) error {
	prepared, err := s.Prepare(nil)
	p.mu.Lock()
	p.prepared = prepared
	p.mu.Unlock()
	if err != nil {
		return err
	}
	for _, claim := range p.claims(root, subject) {
		if err := tx.StageLink(claim); err != nil {
			p.mu.Lock()
			p.stageErr = err
			p.mu.Unlock()
			_ = tx.Abort(ctx)
			return nil
		}
	}
	if p.stageSample {
		if err := tx.StageSample(driver.Sample{From: source, Candidate: candidate}); err != nil {
			return err
		}
		ht := unwrapHostTransaction(tx)
		if ht == nil {
			return errors.New("the host transaction is not reachable under the lifecycle wrapper")
		}
		if err := ht.stageSamples(); err != nil {
			return err
		}
		p.mu.Lock()
		p.staged = len(ht.PublicationTransaction.(*transaction).samples)
		p.mu.Unlock()
	}
	if p.ending == endAbort {
		return tx.Abort(ctx)
	}
	if p.ending == endReject {
		err := tx.Commit(ctx)
		p.mu.Lock()
		p.commitErr = err
		p.mu.Unlock()
		return nil
	}
	err = tx.Commit(ctx)
	p.mu.Lock()
	p.commitErr = err
	p.mu.Unlock()
	return nil
}

var linkType = v1.DefinitionRef{ID: "relation.test_reach", Version: "1.0.0", Pack: v1.PackRef{ID: "pack.test", Version: "1.0.0"}}

func linkNode(id string) driver.NodeRef {
	return driver.NodeRef{Bridge: "bridge.test", ID: v1.ResourceID(id)}
}

func claimOf(from, to driver.NodeRef, basis string) driver.LinkClaim {
	return driver.LinkClaim{Key: driver.LinkKey{Type: linkType, From: from, To: to}, Basis: []v1.EvidenceRef{testEvidence(basis)}}
}

func openLinkHost(t *testing.T, instances map[string]*linkPlan, relation bool) *Runtime {
	t.Helper()
	return openLinkHostAt(t, t.TempDir(), instances, relation)
}

func openLinkHostAt(t *testing.T, stateDir string, instances map[string]*linkPlan, relation bool) *Runtime {
	t.Helper()
	ctx := context.Background()
	var configs []life.InstanceConfig
	factory := linkFactory{plans: instances, relation: relation}
	for name := range instances {
		configs = append(configs, life.InstanceConfig{Driver: binderKind, Instance: driver.InstanceKey(name), Enabled: true, Config: driver.Config{Instance: name, Enabled: true}})
	}
	rt, err := OpenHost(ctx, life.Installation{Key: "installation.test", Bridge: "bridge.test", StateDir: stateDir, Clock: newTestClock(), Instances: configs}, HostOptions{}, factory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(ctx) })
	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitInitial(ctx); err != nil {
		t.Fatal(err)
	}
	return rt
}

type linkFactory struct {
	plans    map[string]*linkPlan
	relation bool
}

func (f linkFactory) Descriptor() driver.Descriptor { return driver.Descriptor{} }
func (f linkFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	return []driver.InstancePlan{{Driver: binderKind, Instance: driver.InstanceKey(c.Instance)}}, nil
}
func (f linkFactory) Construct(_ context.Context, plan driver.InstancePlan) (driver.Instance, error) {
	return &binderInstance{relation: f.relation, links: f.plans[string(plan.Instance)]}, nil
}

func committedRelations(t *testing.T, rt *Runtime, name string) int {
	t.Helper()
	ref := life.Ref{Driver: binderKind, Instance: driver.InstanceKey(name)}
	status, err := rt.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	spec := rt.specFor(ref, life.LifecycleCorrelation{InstanceID: ref.ID(), Epoch: status.Epoch, Generation: status.Generation})
	rt.svc.mu.Lock()
	kernel := rt.svc.kernels[spec.Asset]
	rt.svc.mu.Unlock()
	if kernel == nil {
		return 0
	}
	current, _, ok := kernel.Current()
	if !ok {
		return 0
	}
	return len(current.Relations)
}

func claimsFor(rt *Runtime, key driver.LinkKey) []identity.LinkClaimView {
	return rt.svc.Ledger().Claims(key)
}

func singleClaim(basis string) func(driver.NodeRef, v1.ResourceID) []driver.LinkClaim {
	return func(driver.NodeRef, v1.ResourceID) []driver.LinkClaim {
		return []driver.LinkClaim{claimOf(linkNode("from.a"), linkNode("to.a"), basis)}
	}
}

func TestStageLinkCommitKeepsRelationsAndClaims(t *testing.T) {
	plan := &linkPlan{ending: endCommit, claims: singleClaim("one")}
	rt := openLinkHost(t, map[string]*linkPlan{"binder.one": plan}, true)
	if plan.stageErr != nil || plan.commitErr != nil {
		t.Fatalf("stage %v commit %v", plan.stageErr, plan.commitErr)
	}
	if plan.prepared.Sequence == "" || plan.prepared.ExpectedSemanticRevision == "" {
		t.Fatalf("Prepare returned an unstamped batch: %+v", plan.prepared)
	}
	if got := committedRelations(t, rt, "binder.one"); got != 1 {
		t.Fatalf("relations %d, want the batch's one", got)
	}
	claims := claimsFor(rt, claimOf(linkNode("from.a"), linkNode("to.a"), "one").Key)
	if len(claims) != 1 || claims[0].Publisher.Driver != binderKind || claims[0].Publisher.Instance != "binder.one" || len(claims[0].Basis) != 1 {
		t.Fatalf("claims %+v", claims)
	}
}

func TestStageLinkAbortKeepsNeitherRelationsNorClaims(t *testing.T) {
	plan := &linkPlan{ending: endAbort, claims: singleClaim("one")}
	rt := openLinkHost(t, map[string]*linkPlan{"binder.one": plan}, true)
	if got := committedRelations(t, rt, "binder.one"); got != 0 {
		t.Fatalf("an aborted transaction left %d relations", got)
	}
	if claims := claimsFor(rt, claimOf(linkNode("from.a"), linkNode("to.a"), "one").Key); len(claims) != 0 {
		t.Fatalf("an aborted transaction left claims %+v", claims)
	}
}

func TestStageLinkRejectedCommitKeepsNeitherRelationsNorClaims(t *testing.T) {
	plan := &linkPlan{ending: endReject, claims: singleClaim("one")}
	rt := openLinkHost(t, map[string]*linkPlan{"binder.one": plan}, true)
	if plan.stageErr != nil {
		t.Fatal(plan.stageErr)
	}
	if plan.commitErr == nil {
		t.Fatal("the commit was not rejected")
	}
	if got := committedRelations(t, rt, "binder.one"); got != 0 {
		t.Fatalf("a rejected commit left %d relations", got)
	}
	if claims := claimsFor(rt, claimOf(linkNode("from.a"), linkNode("to.a"), "one").Key); len(claims) != 0 {
		t.Fatalf("a rejected commit left claims %+v", claims)
	}
}

func TestStageLinkTwoPublishersKeepTwoClaims(t *testing.T) {
	a := &linkPlan{ending: endCommit, claims: singleClaim("basis-a")}
	b := &linkPlan{ending: endCommit, claims: singleClaim("basis-b")}
	rt := openLinkHost(t, map[string]*linkPlan{"binder.one": a, "binder.two": b}, false)
	claims := claimsFor(rt, claimOf(linkNode("from.a"), linkNode("to.a"), "x").Key)
	if len(claims) != 2 || claims[0].Publisher == claims[1].Publisher || claims[0].Basis[0] == claims[1].Basis[0] {
		t.Fatalf("claims %+v", claims)
	}
}

func TestStageLinkClaimToAbsentTargetIsKept(t *testing.T) {
	plan := &linkPlan{ending: endCommit, claims: func(root driver.NodeRef, _ v1.ResourceID) []driver.LinkClaim {
		return []driver.LinkClaim{claimOf(root, linkNode("absent.target"), "absent")}
	}}
	rt := openLinkHost(t, map[string]*linkPlan{"binder.one": plan}, false)
	if plan.commitErr != nil {
		t.Fatal(plan.commitErr)
	}
	root := rt.svc.Ledger().Links()
	if len(root) != 1 || len(root[0].Claims) != 1 || root[0].Key.To != linkNode("absent.target") {
		t.Fatalf("links %+v", root)
	}
}

func TestStageLinkInvalidClaimIsRefusedAtStaging(t *testing.T) {
	plan := &linkPlan{ending: endCommit, claims: func(driver.NodeRef, v1.ResourceID) []driver.LinkClaim {
		return []driver.LinkClaim{{Key: claimOf(linkNode("a"), linkNode("b"), "x").Key}}
	}}
	rt := openLinkHost(t, map[string]*linkPlan{"binder.one": plan}, false)
	if plan.stageErr == nil {
		t.Fatal("a claim without a basis was staged")
	}
	if links := rt.svc.Ledger().Links(); len(links) != 0 {
		t.Fatalf("links %+v", links)
	}
}

func TestStageLinkSampleAndClaimCommitTogetherAndASecondHostResolvesTheClaim(t *testing.T) {
	dir := t.TempDir()
	plan := &linkPlan{ending: endCommit, stageSample: true, claims: singleClaim("one")}
	first := openLinkHostAt(t, dir, map[string]*linkPlan{"binder.one": plan}, false)
	if plan.stageErr != nil || plan.commitErr != nil {
		t.Fatalf("stage %v commit %v", plan.stageErr, plan.commitErr)
	}
	key := claimOf(linkNode("from.a"), linkNode("to.a"), "x").Key
	if claims := claimsFor(first, key); len(claims) != 1 {
		t.Fatalf("first host claims %+v", claims)
	}
	if got := sampleCount(t, first); got != 1 {
		t.Fatalf("first host resolves %d sampled sources", got)
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if plan.staged != 1 {
		t.Fatalf("the driver's sample and the host's automatic sample were both staged: %d", plan.staged)
	}
	second := openLinkHostAt(t, dir, map[string]*linkPlan{"binder.one": {silent: true}}, false)
	claims := claimsFor(second, key)
	if len(claims) != 1 || claims[0].Publisher.Instance != "binder.one" || len(claims[0].Basis) != 1 {
		t.Fatalf("second host claims %+v", claims)
	}
}

func sampleCount(t *testing.T, rt *Runtime) int {
	t.Helper()
	rt.svc.mu.Lock()
	var declared []driver.Source
	for _, source := range rt.svc.declared {
		declared = append(declared, source)
	}
	rt.svc.mu.Unlock()
	view, err := rt.svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, source := range declared {
		for _, state := range rt.svc.Element(source.For, view).States {
			if state.Latest != nil && state.Latest.Candidate.Value != nil {
				n++
			}
		}
	}
	return n
}

// unwrapHostTransaction follows the embedded transaction of the lifecycle
// wrapper down to the host's transaction.
func unwrapHostTransaction(tx driver.PublicationTransaction) *hostTransaction {
	for tx != nil {
		if ht, ok := tx.(*hostTransaction); ok {
			return ht
		}
		v := reflect.ValueOf(tx)
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return nil
		}
		inner := v.FieldByName("PublicationTransaction")
		if !inner.IsValid() || inner.IsNil() {
			return nil
		}
		next, ok := reflect.NewAt(inner.Type(), inner.Addr().UnsafePointer()).Elem().Interface().(driver.PublicationTransaction)
		if !ok {
			return nil
		}
		tx = next
	}
	return nil
}
