package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// bystander is a selected factory that claims no instance. It records every
// configuration it is asked to plan and can fail with the reference it saw.
type bystander struct {
	seen []driver.Config
	fail bool
}

func (b *bystander) Descriptor() driver.Descriptor { return driver.Descriptor{} }
func (b *bystander) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	b.seen = append(b.seen, c)
	if b.fail {
		return nil, fmt.Errorf("cannot plan %s with %q", c.Instance, c.SecretRef)
	}
	return nil, nil
}
func (b *bystander) Construct(context.Context, driver.InstancePlan) (driver.Instance, error) {
	return nil, errors.New("bystander constructs nothing")
}

func TestSecretReferenceReachesOnlyTheMatchingFactory(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(time.Now())
	cfg := baseConfig(t, clock)
	other := &bystander{}
	owner := newSynthFactory(clock)
	site, err := Open(ctx, cfg, other, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer site.Close(ctx)
	if len(other.seen) == 0 {
		t.Fatal("the unrelated factory was never asked to plan")
	}
	for _, c := range other.seen {
		if c.SecretRef == secretAlpha || c.SecretRef == secretBeta {
			t.Fatalf("the unrelated factory received a configured secret reference for %s", c.Instance)
		}
	}
	assertConfigMapping(t, owner, cfg)

	failing := &bystander{fail: true}
	broken := cfg
	broken.StateDir = t.TempDir()
	_, err = Open(ctx, broken, failing, newSynthFactory(clock))
	if err == nil {
		t.Fatal("a failing factory must fail planning")
	}
	if strings.Contains(err.Error(), secretAlpha) || strings.Contains(err.Error(), secretBeta) {
		t.Fatalf("a diagnostic exposes a reference: %v", err)
	}
}

// requiringFactory validates that an instance configured with a reference
// carries a nonempty one when it plans.
type requiringFactory struct {
	*synthFactory
	needs map[string]bool
}

func (f *requiringFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	if f.needs[c.Instance] && c.SecretRef == "" {
		return nil, fmt.Errorf("instance %s needs its secret reference", c.Instance)
	}
	return f.synthFactory.Plan(c)
}

func TestFactoryRequiringItsSecretReferencePlansFromTheConfiguredOperands(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(time.Now())
	cfg := baseConfig(t, clock)
	other := &bystander{}
	owner := &requiringFactory{synthFactory: newSynthFactory(clock), needs: map[string]bool{"plain.a": true, "plain.b": true}}
	site, err := Open(ctx, cfg, other, owner)
	if err != nil {
		t.Fatalf("a correctly configured instance was rejected: %v", err)
	}
	defer func() { _ = site.Close(ctx) }()
	assertConfigMapping(t, owner.synthFactory, cfg)
	for _, c := range other.seen {
		if c.SecretRef == secretAlpha || c.SecretRef == secretBeta {
			t.Fatalf("the unrelated factory received a configured reference for %s", c.Instance)
		}
	}
}

func TestOwnerScopedStartErrorsDoNotEndTheHost(t *testing.T) {
	scoped := &life.SemanticError{InstanceID: "driver/one", Op: "restore", Err: errors.New("records do not decode")}
	if !ownerScoped(errors.Join(scoped, &life.SemanticError{InstanceID: "driver/two", Op: "restore", Err: errors.New("x")})) {
		t.Fatal("owner-scoped recovery errors must keep the host running")
	}
	if ownerScoped(errors.Join(scoped, errors.New("store register failed"))) || ownerScoped(errors.New("store register failed")) || ownerScoped(nil) {
		t.Fatal("any other start error is fatal")
	}
}

func TestStoppedInstanceLeavesCurrentReadsAndStaysGoneAfterReopen(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	cfg := baseConfig(t, clock)
	cfg.Instances = cfg.Instances[:1]
	ref := life.Ref{Driver: kindOwner, Instance: "owner.one"}

	factory := newSynthFactory(clock)
	site := openStarted(t, cfg, factory)
	cut, err := site.Tree().SemanticInput(driver.Filter{}).Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := zoneFactCoefficient(t, cut); value != initialCoefficient {
		t.Fatalf("before the stop the owner publishes %q", value)
	}
	status, err := site.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.SetDesired(ctx, ref, life.MethodStop, status.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	waitStopped(t, site, ref)
	assertNoSetting(t, ctx, site)
	if err := site.Close(ctx); err != nil {
		t.Fatal(err)
	}

	again := newSynthFactory(clock)
	reopened := openStarted(t, cfg, again)
	defer reopened.Close(ctx)
	waitStopped(t, reopened, ref)
	assertNoSetting(t, ctx, reopened)
	if got := again.constructedNames(); len(got) != 0 {
		t.Fatalf("a stopped instance was constructed: %v", got)
	}
}

func waitStopped(t *testing.T, site *Site, ref life.Ref) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if st, _ := site.Snapshot(ref); st.ObservedState == life.ObservedStopped {
			return
		}
	}
	t.Fatal("the instance did not reach stopped")
}

func assertNoSetting(t *testing.T, ctx context.Context, site *Site) {
	t.Helper()
	cut, err := site.Tree().SemanticInput(driver.Filter{}).Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := zoneFactCoefficient(t, cut); value != "" {
		t.Fatalf("the stopped generation's value %q is still current", value)
	}
}

func TestSiteCutReferenceBelongsToItsOwnCapture(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	site := openStarted(t, baseConfig(t, clock), newSynthFactory(clock))
	defer site.Close(ctx)
	asset := ""
	if probe, err := site.Cut(ctx); err == nil {
		asset = string(probe.Ref().Members[0].AssetID)
		probe.Close()
	} else {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if c, err := site.Tree().Capture(ctx, tree.CutOptions{Filter: driver.Filter{Assets: []v1.AssetID{v1.AssetID(asset)}}}); err == nil {
				_ = c.Close()
			}
		}
	}()
	for i := 0; i < 40; i++ {
		cut, err := site.Cut(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if want := string(cut.Ref().CutID) + ":"; !strings.HasPrefix(string(cut.Tree().ID()), want) {
			t.Fatalf("cut %s carries the reference of another capture %s", cut.Tree().ID(), cut.Ref().CutID)
		}
		if cut.Ref().Completeness != graph.CutPartial || len(cut.Ref().NotIncluded) != 4 {
			t.Fatalf("the whole-site capture reports %+v: the filtered capture's omissions leaked", cut.Ref())
		}
		cut.Close()
	}
	close(stop)
	<-done
}

func TestPrepareBindsTargetToTheSelectedCutAndKeepsItsInput(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	factory := newSynthFactory(clock)
	site := openStarted(t, baseConfig(t, clock), factory)
	defer site.Close(ctx)
	facts := thermalSetting(t)
	request := requestFor(t, ctx, site, facts, targetCoefficient)
	request.Timeout = 20 * time.Second
	method := semanticMethod(t, site, facts)

	whole, err := site.Cut(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer whole.Close()
	other := v1.AssetID("")
	for _, m := range whole.Ref().Members {
		if m.AssetID != request.AssetID {
			other = m.AssetID
		}
	}
	if other == "" {
		t.Fatal("the capture has no other member")
	}
	filtered, err := site.Tree().Capture(ctx, tree.CutOptions{Filter: driver.Filter{Assets: []v1.AssetID{other}}})
	if err != nil {
		t.Fatal(err)
	}
	defer filtered.Close()
	if _, err := site.Tree().Prepare(ctx, filtered.ID(), method, tree.Input{Request: request}); err == nil {
		t.Fatal("a request for an asset the selected cut omits must not be prepared")
	}

	prepared, err := whole.Prepare(ctx, MethodSelection{Method: method, Input: tree.Input{Request: request}})
	if err != nil {
		t.Fatal(err)
	}
	// Editing the caller's slices afterwards leaves the prepared input unchanged.
	request.Arguments[0].Value = kelvin("999")
	receipt, err := site.Invoke(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.WaitOutcome(ctx, receipt.ID); err != nil {
		t.Fatal(err)
	}
	if value, _, executed := factory.owner.read(); value != targetCoefficient || executed != 1 {
		t.Fatalf("the owner holds %s after %d executions, want the prepared %s", value, executed, targetCoefficient)
	}
}

func TestPublicationKeepsTheDriversReceiptTime(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	factory := newSynthFactory(clock)
	receipt := v1.MonotonicPoint{ClockEpochID: "clock.epoch.driver.receipt", Nanoseconds: "5"}
	factory.receipt = &receipt
	cfg := baseConfig(t, clock)
	cfg.Instances = cfg.Instances[:1]
	site := openStarted(t, cfg, factory)
	defer site.Close(ctx)
	cut, err := site.Tree().SemanticInput(driver.Filter{}).Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, snap := range cut.Snapshots {
		for _, envelope := range snap.Facts {
			for _, candidate := range envelope.Candidates {
				if candidate.Key.FactID == thermalSetting(t).argument.FactID {
					found = true
					if candidate.Times.ReceiptMonotonic != receipt || candidate.Times.EvaluateMonotonic != receipt {
						t.Fatalf("publication replaced the receipt time evidence: %+v", candidate.Times)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("the owner's fact is not current")
	}
}

func TestLockDeniedHostLeavesTheIdentityCheckpointAlone(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	cfg := baseConfig(t, clock)
	cfg.Instances = cfg.Instances[:1]
	active, err := Open(ctx, cfg, newSynthFactory(clock))
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close(ctx)
	// The contender loads the checkpoint before the active host writes its own.
	contender, err := Open(ctx, cfg, newSynthFactory(clock))
	if err != nil {
		t.Fatal(err)
	}
	if err := active.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := active.WaitInitial(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.StateDir, "identity", "checkpoint.json")
	before, err := os.ReadFile(path)
	if err != nil || len(before) == 0 {
		t.Fatalf("the active host wrote no checkpoint: %v", err)
	}
	_ = contender.Start(ctx)
	_ = contender.Close(ctx)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("a lock-denied host altered the committed identity checkpoint: %v", err)
	}
}

func TestAcceptedIdentityStatementSurvivesProcessLoss(t *testing.T) {
	t.Setenv("TZ", "UTC")
	ctx := context.Background()
	clock := newFixedClock(time.Now())
	cfg := baseConfig(t, clock)
	cfg.Instances = cfg.Instances[2:5]
	for i := range cfg.Instances {
		cfg.Instances[i].SecretRef = ""
	}
	site := openStarted(t, cfg, newSynthFactory(clock))
	cut, err := site.Cut(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, lens := graphOf(t, ctx, site, cut)
	var members []driver.NodeRef
	for _, r := range lens.Resources {
		if r.Resource.Kind == "resource.equipment" {
			members = append(members, driver.NodeRef{Bridge: cfg.BridgeID, ID: r.Resource.ResourceID})
		}
	}
	cut.Close()
	if err := site.Close(ctx); err != nil {
		t.Fatal(err)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	a, b := members[0], members[1]

	c := cfg
	c.OperatorIdentityStatements = []runtime.OperatorIdentityStatement{{Action: runtime.OperatorCertify, Join: statementJoin(a, b, a, "1", "certify")}}
	opened, err := Open(ctx, c, newSynthFactory(clock))
	if err != nil {
		t.Fatal(err)
	}
	// No Start and no Close: the process is lost here.
	view := readCheckpoint(t, cfg.StateDir)
	if len(view.Certifications) != 1 || view.Certifications[0].Join.Survivor != a {
		t.Fatalf("the accepted statement was not durable before shutdown: %+v", view)
	}
	_ = opened
}

func TestConcurrentFirstOpensCommitOneLocalIdentity(t *testing.T) {
	dir := t.TempDir()
	const opens = 16
	results := make([]Config, opens)
	errs := make([]error, opens)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = Config{StateDir: dir}
			errs[i] = withLocalIdentity(&results[i])
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if results[i].InstallationKey != results[0].InstallationKey || results[i].BridgeID != results[0].BridgeID {
			t.Fatalf("open %d published a different identity: %+v vs %+v", i, results[i], results[0])
		}
	}
	again := Config{StateDir: dir}
	if err := withLocalIdentity(&again); err != nil || again.InstallationKey != results[0].InstallationKey {
		t.Fatalf("the committed identity was not reused: %+v %v", again, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("a temporary identity file stays behind: %s", entry.Name())
		}
	}
}

// kindedFactory plans one driver kind. It records every configuration it is
// asked to plan and rejects a reference that is not one it was configured with.
type kindedFactory struct {
	kind  driver.DriverKind
	valid string
	seen  []driver.Config
}

func (f *kindedFactory) Descriptor() driver.Descriptor { return driver.Descriptor{} }
func (f *kindedFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	f.seen = append(f.seen, c)
	if c.SecretRef != "" && c.SecretRef != f.valid {
		return nil, fmt.Errorf("the reference %q is not a configured one", c.SecretRef)
	}
	return []driver.InstancePlan{{Driver: f.kind, Instance: driver.InstanceKey(c.Instance)}}, nil
}
func (f *kindedFactory) Construct(context.Context, driver.InstancePlan) (driver.Instance, error) {
	return nil, errors.New("constructs nothing")
}

func TestPlanningKeepsEachKindInstancePairsOperandsAndReference(t *testing.T) {
	ctx := context.Background()
	const ref = "synthetic-ref-for-a"
	a := &kindedFactory{kind: "synth.kind_a", valid: ref}
	b := &kindedFactory{kind: "synth.kind_b"}
	cfg := Config{
		InstallationKey: "installation.test", BridgeID: "bridge.test", StateDir: t.TempDir(), Clock: newFixedClock(time.Now()), Calendar: ptr("UTC"),
		Instances: []InstanceConfig{
			{Kind: "synth.kind_a", Instance: "main", Enabled: true, Transport: []byte(`{"link":"a"}`), Options: []byte(`{"o":"a"}`), SecretRef: ref},
			{Kind: "synth.kind_b", Instance: "main", Enabled: true, Transport: []byte(`{"link":"b"}`), Options: []byte(`{"o":"b"}`)},
		},
	}
	site, err := Open(ctx, cfg, a, b)
	if err != nil {
		t.Fatalf("two kinds reusing one instance key must plan from their own records: %v", err)
	}
	defer func() { _ = site.Close(ctx) }()
	plan := site.rt.Plan()
	if len(plan.Instances) != 2 {
		t.Fatalf("planned %+v", plan.Instances)
	}
	for _, planned := range plan.Instances {
		wantRef, wantTransport := "", `{"link":"b"}`
		if planned.Ref.Driver == "synth.kind_a" {
			wantRef, wantTransport = ref, `{"link":"a"}`
		}
		if planned.Config.SecretRef != wantRef || string(planned.Config.Transport) != wantTransport {
			t.Fatalf("%s planned with %+v, want reference %q and transport %s", planned.Ref, planned.Config, wantRef, wantTransport)
		}
	}
	for _, c := range b.seen {
		if c.SecretRef != "" && c.SecretRef != probeSecretRef {
			t.Fatalf("the other kind's factory received a configured reference: %+v", c)
		}
	}
}
