package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

var baseTime = time.Unix(1_700_000_000, 0).UTC()

const (
	secretAlpha = "ref.synthetic.alpha"
	secretBeta  = "ref.synthetic.beta"
)

func options(kind driver.DriverKind) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"kind": string(kind)})
	return raw
}

func transport(name string) json.RawMessage {
	raw, _ := json.Marshal([]claimSpec{{Provider: "family.serial", Config: "lane." + name, Domain: "domain." + name}})
	return raw
}

func ptr(s string) *string { return &s }

func baseConfig(t *testing.T, clock life.Clock) Config {
	t.Helper()
	return Config{
		InstallationKey: "installation.test", BridgeID: "bridge.test", StateDir: t.TempDir(), Clock: clock, Calendar: ptr("UTC"),
		Instances: []InstanceConfig{
			{Kind: kindOwner, Instance: "owner.one", Enabled: true, Options: options(kindOwner)},
			{Kind: kindConsumer, Instance: "consumer.one", Enabled: true, Options: options(kindConsumer)},
			{Kind: kindPlain, Instance: "plain.a", Enabled: true, Transport: transport("a"), Options: options(kindPlain), SecretRef: secretAlpha},
			{Kind: kindPlain, Instance: "plain.b", Enabled: true, Transport: transport("b"), Options: options(kindPlain), SecretRef: secretBeta},
			{Kind: kindPlain, Instance: "plain.c", Enabled: true, Transport: transport("c"), Options: options(kindPlain)},
			{Kind: kindPlain, Instance: "idle.one", Enabled: false, Options: options(kindPlain)},
		},
	}
}

func openStarted(t *testing.T, cfg Config, factory *synthFactory) *Site {
	t.Helper()
	ctx := context.Background()
	site, err := Open(ctx, cfg, factory)
	if err != nil {
		t.Fatal(err)
	}
	if err := site.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := site.WaitInitial(ctx); err != nil {
		t.Fatal(err)
	}
	return site
}

func semanticMethod(t *testing.T, site *Site, facts operationFacts) tree.MethodRef {
	t.Helper()
	segment, err := tree.DefinitionSegment(facts.op.Ref)
	if err != nil {
		t.Fatal(err)
	}
	return tree.MethodRef(tree.Key{Node: tree.NodePath{Installation: site.rt.Plan().Installation}, Facet: tree.KeywordMethods, Domain: tree.KeywordSem, Segment: segment}.String())
}

// requestFor completes the guard fields of a request from the current semantic
// capture of the owner's setting.
func requestFor(t *testing.T, ctx context.Context, site *Site, facts operationFacts, coefficient string) *driver.OperationRequest {
	t.Helper()
	cut, err := site.Tree().SemanticInput(driver.Filter{}).Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return requestIn(t, cut, facts, coefficient)
}

func requestIn(t *testing.T, cut driver.SemanticCut, facts operationFacts, coefficient string) *driver.OperationRequest {
	t.Helper()
	for _, snap := range cut.Snapshots {
		for _, resource := range snap.Resources {
			if resource.ResourceID != zoneResource {
				continue
			}
			var binding v1.NativeBinding
			for _, b := range snap.Bindings {
				if b.State == v1.BindingCurrent {
					binding = b
				}
			}
			var capability v1.CapabilityInstance
			for _, c := range snap.Capabilities {
				if c.BindingID == binding.BindingID {
					capability = c
				}
			}
			return &driver.OperationRequest{
				AssetID: snap.AssetID, BindingID: binding.BindingID, CapabilityInstanceID: capability.InstanceID, Operation: facts.op.Ref,
				Arguments:                          []v1.TypedField{{ID: facts.op.Argument.ID, Value: kelvin(coefficient)}},
				ExpectedSemanticRevision:           snap.Revisions.Semantic,
				ExpectedCapabilityRevision:         snap.Revisions.Capabilities,
				ExpectedCapabilityInstanceRevision: capability.Revision,
				ExpectedDriverGeneration:           binding.DriverGeneration, ExpectedSourceEpochID: binding.SourceEpochID,
				Preconditions: []operation.Precondition{},
			}
		}
	}
	t.Fatal("the owner's setting is not in the capture")
	return nil
}

func zoneFactCoefficient(t *testing.T, cut driver.SemanticCut) (string, v1.NativeBinding) {
	t.Helper()
	for _, snap := range cut.Snapshots {
		for _, envelope := range snap.Facts {
			for _, candidate := range envelope.Candidates {
				if candidate.Key.FactID == thermalSetting(t).argument.FactID && candidate.Value != nil {
					var binding v1.NativeBinding
					for _, b := range snap.Bindings {
						if b.BindingID == *candidate.BindingID {
							binding = b
						}
					}
					return coefficientOfValue(candidate.Value), binding
				}
			}
		}
	}
	return "", v1.NativeBinding{}
}

// graphRecord is what a cut shows of the installation graph, independent of
// ids the allocator minted: kinds, relations and their endpoints by kind.
type graphRecord struct {
	Kinds     []string
	Relations []string
	Members   []string
}

func lensRequest() graph.LensRequest {
	return graph.LensRequest{
		Lens: "lens.installation_hierarchy", Entry: graph.EntryPoint{Kind: graph.EntryLensRoots},
		Direction: graph.DirectionBoth, Depth: 4, PageSize: 50,
	}
}

func graphOf(t *testing.T, ctx context.Context, site *Site, cut *Cut) (graphRecord, graph.LensResult) {
	t.Helper()
	input := site.Tree().SemanticInput(driver.Filter{})
	semantic, err := input.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := input.RetrieveLens(ctx, semantic.Ref, lensRequest())
	if err != nil {
		t.Fatal(err)
	}
	kindOf := map[v1.ResourceID]string{}
	var record graphRecord
	for _, r := range result.Resources {
		kindOf[r.Resource.ResourceID] = string(r.Resource.Kind)
		record.Kinds = append(record.Kinds, string(r.Resource.Kind))
	}
	for _, r := range result.Relations {
		record.Relations = append(record.Relations, fmt.Sprintf("%s %s->%s", r.Definition.ID, kindOf[r.From], kindOf[r.To]))
	}
	for _, m := range semantic.Ref.Members {
		record.Members = append(record.Members, string(m.AssetID))
	}
	sort.Strings(record.Kinds)
	sort.Strings(record.Relations)
	sort.Strings(record.Members)
	return record, result
}

// walkRecord is the hand-comparable result of a walk.
type walkRecord struct {
	Nodes      []string
	Attributes map[string]string
	Methods    int
	Report     WalkReport
}

func walkAll(t *testing.T, ctx context.Context, cut *Cut, page int) walkRecord {
	t.Helper()
	record := walkRecord{Attributes: map[string]string{}}
	report, err := cut.Walk(ctx, AllVisible(page), func(e WalkEntry) error {
		switch e.Descriptor.Kind {
		case tree.EntryNode:
			record.Nodes = append(record.Nodes, string(e.Descriptor.Ref))
		case tree.EntryAttribute:
			if e.Err != nil {
				return e.Err
			}
			record.Attributes[string(e.Descriptor.Ref)] = string(e.Value.Encoded)
		case tree.EntryMethod:
			record.Methods++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(record.Nodes)
	record.Report = report
	return record
}

func expectedInstanceNodes(installation string, instances map[driver.DriverKind][]driver.InstanceKey) []string {
	var out []string
	for kind, keys := range instances {
		for _, key := range keys {
			out = append(out, tree.NodePath{Installation: installation, Driver: kind, Instance: key}.String())
		}
	}
	sort.Strings(out)
	return out
}

func expectedIndexRefs(installation string) []string {
	var out []string
	for _, category := range tree.IndexCategories {
		segment, _ := tree.EncodeSegment(tree.IndexPart, category)
		out = append(out, tree.Key{Node: tree.NodePath{Installation: installation}, Facet: tree.KeywordAttributes, Domain: tree.KeywordSem, Segment: segment}.String())
	}
	return out
}

// secondHostRun is one complete lifecycle of the second host: open, start, wait,
// application (cut, defer close, walk, prepare, invoke, wait for the outcome),
// close.
type secondHostRun struct {
	Walk      walkRecord
	Graph     graphRecord
	Outcome   tree.Outcome
	Receipt   tree.Receipt
	Request   driver.OperationRequest
	Value     string
	Last      driver.Dispatch
	Executed  int
	Feed      driver.Changes
	FeedFrom  driver.Cursor
	SumBefore graph.MemberSum
	LensA     []byte
	LensB     []byte
}

func runSecondHost(t *testing.T, cfg Config, factory *synthFactory, page int, target string) secondHostRun {
	t.Helper()
	ctx := context.Background()
	facts := thermalSetting(t)
	site := openStarted(t, cfg, factory)
	var out secondHostRun
	app := func(ctx context.Context, site *Site, task Task) error {
		cut, err := site.Cut(ctx)
		if err != nil {
			return err
		}
		defer cut.Close()
		out.Walk = walkAll(t, ctx, cut, page)
		out.Graph, _ = graphOf(t, ctx, site, cut)
		consumer := factory.started["consumer.one"]
		input := consumer.session.SemanticInput()
		captured, err := input.Capture(ctx, driver.Filter{})
		if err != nil {
			return err
		}
		out.FeedFrom = cut.Tree().Cursor().Semantic
		out.LensA, _ = mustLens(t, ctx, input, captured.Ref)
		sum, err := input.SumMembers(ctx, captured.Ref, lensRequest(), v1.DefinitionRef{Pack: facts.op.Ref.Pack, ID: facts.op.Argument.ID, Version: facts.op.Ref.Version}, anyResource(t, ctx, input, captured.Ref))
		if err != nil {
			return err
		}
		out.SumBefore = sum
		request := requestIn(t, captured, facts, target)
		request.Timeout = 20 * time.Second
		prepared, err := cut.Prepare(ctx, MethodSelection{Method: semanticMethod(t, site, facts), Input: tree.Input{Request: request}})
		if err != nil {
			return err
		}
		out.Request = *request
		receipt, err := site.Invoke(ctx, prepared)
		if err != nil {
			return err
		}
		out.Receipt = receipt
		out.Outcome, err = site.WaitOutcome(ctx, receipt.ID)
		if err != nil {
			return err
		}
		out.LensB, _ = mustLens(t, ctx, input, captured.Ref)
		feedCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out.Feed, err = input.Resume(feedCtx, out.FeedFrom, driver.Filter{})
		if err != nil {
			st, _ := site.Snapshot(life.Ref{Driver: kindOwner, Instance: "owner.one"})
			return fmt.Errorf("feed: %w (outcome %+v) owner %+v errs %v", err, out.Outcome.Operation, st, factory.startErrs)
		}
		return nil
	}
	if err := app(ctx, site, Task{}); err != nil {
		t.Fatal(err)
	}
	out.Value, out.Last, out.Executed = factory.owner.read()
	if err := site.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustLens(t *testing.T, ctx context.Context, input driver.SemanticInput, ref graph.CutRef) ([]byte, graph.LensResult) {
	t.Helper()
	result, err := input.RetrieveLens(ctx, ref, lensRequest())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := result.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return raw, result
}

func anyResource(t *testing.T, ctx context.Context, input driver.SemanticInput, ref graph.CutRef) v1.ResourceID {
	t.Helper()
	_, result := mustLens(t, ctx, input, ref)
	for _, r := range result.Resources {
		if r.Resource.Kind == "resource.site" {
			return r.Resource.ResourceID
		}
	}
	t.Fatal("the lens shows no site")
	return ""
}

var _ = bytes.MinRead
var _ = os.ErrNotExist
var _ = filepath.Join
var _ = strings.Join

func TestHostSecondHost(t *testing.T) {
	clock := newFixedClock(baseTime.Add(time.Second))
	cfg := baseConfig(t, clock)
	facts := thermalSetting(t)
	factory := newSynthFactory(clock)
	first := runSecondHost(t, cfg, factory, 3, targetCoefficient)

	installation := cfg.InstallationKey
	expectedNodes := expectedInstanceNodes(installation, map[driver.DriverKind][]driver.InstanceKey{
		kindOwner: {"owner.one"}, kindConsumer: {"consumer.one"}, kindPlain: {"plain.a", "plain.b", "plain.c", "idle.one"},
	})
	if strings.Join(first.Walk.Nodes, "\n") != strings.Join(expectedNodes, "\n") {
		t.Fatalf("walked nodes\n%s\nwant\n%s", strings.Join(first.Walk.Nodes, "\n"), strings.Join(expectedNodes, "\n"))
	}
	for _, ref := range expectedIndexRefs(installation) {
		if _, ok := first.Walk.Attributes[ref]; !ok {
			t.Fatalf("the installation node does not index %s", ref)
		}
	}
	if len(tree.IndexCategories) != 10 {
		t.Fatalf("index categories %v", tree.IndexCategories)
	}
	factRef := ""
	for ref, encoded := range first.Walk.Attributes {
		if strings.Contains(encoded, initialCoefficient) {
			factRef = ref
		}
	}
	if factRef == "" {
		t.Fatalf("no walked attribute carries the owner's initial value %s", initialCoefficient)
	}
	if first.Walk.Methods == 0 {
		t.Fatal("the walk shows no method")
	}
	if first.Walk.Report.Completeness != graph.CutPartial || len(first.Walk.Report.NotIncluded) != 4 {
		t.Fatalf("report %+v: the four instances with nothing published are omitted as source_unavailable", first.Walk.Report)
	}
	for _, omitted := range first.Walk.Report.NotIncluded {
		if omitted.Reason != graph.NotIncludedSourceUnavailable {
			t.Fatalf("omission %+v", omitted)
		}
	}
	// The outcome, the owner's value read back and the consumer's own feed.
	op := first.Outcome.Operation
	if op == nil || op.Status != "protocol_ack" || op.KernelOutcome != operation.OutcomeAcknowledgedUnverified ||
		op.Delivery != operation.DeliverySent || op.Acknowledgement != operation.AckAccepted {
		t.Fatalf("outcome %+v", first.Outcome)
	}
	if first.Outcome.Receipt != first.Receipt.ID {
		t.Fatalf("outcome %s is not for receipt %s", first.Outcome.Receipt, first.Receipt.ID)
	}
	if first.Value != targetCoefficient || first.Executed != 1 {
		t.Fatalf("owner holds %s after %d executions, want %s after 1", first.Value, first.Executed, targetCoefficient)
	}
	last := first.Last.Request
	if last.AssetID != first.Request.AssetID || last.Operation != facts.op.Ref || len(last.Arguments) != 1 ||
		coefficientOfValue(&last.Arguments[0].Value) != targetCoefficient || last.ExpectedDriverGeneration != first.Request.ExpectedDriverGeneration {
		t.Fatalf("the owner executed %+v, not the invoked %+v", last, first.Request)
	}
	if len(first.Feed.Commits) == 0 || first.Feed.Resync {
		t.Fatalf("feed %+v", first.Feed)
	}
	var seen bool
	for _, commit := range first.Feed.Commits {
		value, binding := zoneFactCoefficient(t, commit)
		if value == targetCoefficient {
			seen = true
			if binding.DriverGeneration != first.Request.ExpectedDriverGeneration || binding.AssetID != first.Request.AssetID {
				t.Fatalf("the changed fact came under %s generation %s", binding.AssetID, binding.DriverGeneration)
			}
		}
	}
	if !seen {
		t.Fatal("the consumer's own feed did not carry the changed value")
	}
	if !bytes.Equal(first.LensA, first.LensB) {
		t.Fatal("the lens read of the captured cut changed with the intervening commit")
	}
	if !first.SumBefore.Withheld || first.SumBefore.Reason != graph.SumPartialCut {
		t.Fatalf("sum %+v: a partial captured cut returns its withheld reason", first.SumBefore)
	}
	wantKinds := []string{"resource.equipment", "resource.equipment", "resource.equipment", "resource.equipment", "resource.site"}
	sort.Strings(wantKinds)
	if strings.Join(first.Graph.Kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("graph kinds %v, want %v", first.Graph.Kinds, wantKinds)
	}

	// A second lifecycle over the same state directory shows the same graph.
	second := runSecondHost(t, cfg, newSynthFactory(clock), 100, targetCoefficient)
	if strings.Join(second.Walk.Nodes, "\n") != strings.Join(first.Walk.Nodes, "\n") {
		t.Fatalf("nodes differ across lifecycles:\n%v\n%v", second.Walk.Nodes, first.Walk.Nodes)
	}
	if fmt.Sprint(second.Graph) != fmt.Sprint(first.Graph) {
		t.Fatalf("graph differs across lifecycles:\n%+v\n%+v", second.Graph, first.Graph)
	}
}

// synthFactoryNoEarly is a nonparticipating factory: it implements Factory and
// not EarlyDispatcher.
type synthFactoryNoEarly struct{ constructed int }

func (f *synthFactoryNoEarly) Descriptor() driver.Descriptor { return driver.Descriptor{} }
func (f *synthFactoryNoEarly) Plan(driver.Config) ([]driver.InstancePlan, error) {
	return nil, nil
}
func (f *synthFactoryNoEarly) Construct(context.Context, driver.InstancePlan) (driver.Instance, error) {
	f.constructed++
	return nil, fmt.Errorf("not used")
}

func testEnvironment(t *testing.T) (environment, *bytes.Buffer, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	stderr := &bytes.Buffer{}
	return environment{root: func() (string, error) { return root, nil }, stderr: stderr}, stderr, root
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchEarlyThroughMainAndOpen(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(time.Now())
	selected := newSynthFactory(clock)
	bystander := &synthFactoryNoEarly{}
	argv := []string{"program", "synth-child", "/unusable/installation.json"}
	handled, code, err := DispatchEarly(ctx, argv, bystander, selected)
	if !handled || code != 7 || err != nil {
		t.Fatalf("dispatch %v %d %v", handled, code, err)
	}
	if len(selected.argv) != 1 || strings.Join(selected.argv[0], " ") != strings.Join(argv, " ") {
		t.Fatalf("the factory saw %v, want the full argv including the program", selected.argv)
	}
	if handled, _, err = DispatchEarly(ctx, []string{"program", "synth-fail"}, selected); !handled && err == nil {
		t.Fatal("an early error is passed through")
	}
	if handled, code, err = DispatchEarly(ctx, []string{"program", "ordinary"}, bystander, selected); handled || code != 0 || err != nil {
		t.Fatalf("no match must continue ordinary startup: %v %d %v", handled, code, err)
	}
	if got := selected.constructedNames(); len(got) != 0 || len(selected.planned) != 0 {
		t.Fatalf("the dispatch scan planned %v or constructed %v", selected.planned, got)
	}

	env, stderr, _ := testEnvironment(t)
	ran := false
	app := func(context.Context, *Site, Task) error { ran = true; return nil }
	if got := run(env, argv, app, bystander, selected); got != 7 || ran {
		t.Fatalf("Main exit %d ran %v: a handled early argv succeeds with an unusable config path", got, ran)
	}
	if got := run(env, []string{"program", "synth-fail"}, app, selected); got != 1 || !strings.Contains(stderr.String(), "synthetic child failed") {
		t.Fatalf("Main exit %d %q", got, stderr.String())
	}
	if len(selected.planned) != 0 || len(selected.constructedNames()) != 0 {
		t.Fatal("an early command touched planning or construction")
	}
	// No match: ordinary startup continues in Main.
	if got := run(env, []string{"program"}, app, bystander, selected); got != 0 || !ran {
		t.Fatalf("no match must continue to the application: exit %d ran %v %s", got, ran, stderr.String())
	}
	if bystander.constructed != 0 {
		t.Fatal("a nonparticipating factory constructed an instance")
	}
}

func TestMainRunsExplicitInstallationAndSelection(t *testing.T) {
	t.Setenv("TZ", "UTC")
	facts := thermalSetting(t)
	clock := newFixedClock(time.Now())
	factory := newSynthFactory(clock)
	env, stderr, _ := testEnvironment(t)
	dir := t.TempDir()
	cfg := baseConfig(t, nil)
	cfg.StateDir = filepath.Join(dir, "state")
	installation := filepath.Join(dir, "installation.json")
	writeJSON(t, installation, cfg)
	var site0 *Site
	selection := MethodSelection{Input: tree.Input{Request: &driver.OperationRequest{
		Operation: facts.op.Ref, Arguments: []v1.TypedField{{ID: facts.op.Argument.ID, Value: kelvin(targetCoefficient)}},
	}}}
	// The method ref names the installation; it is known from the configured key.
	selection.Method = tree.MethodRef(tree.Key{Node: tree.NodePath{Installation: cfg.InstallationKey}, Facet: tree.KeywordMethods, Domain: tree.KeywordSem, Segment: mustSegment(t, facts)}.String())
	selectionFile := filepath.Join(dir, "selection.json")
	writeJSON(t, selectionFile, selection)

	var got Task
	app := func(ctx context.Context, site *Site, task Task) error {
		site0, got = site, task
		cut, err := site.Cut(ctx)
		if err != nil {
			return err
		}
		defer cut.Close()
		if task.Selection == nil {
			return nil
		}
		request := requestFor(t, ctx, site, facts, targetCoefficient)
		request.Arguments = task.Selection.Input.Request.Arguments
		request.Timeout = 20 * time.Second
		prepared, err := cut.Prepare(ctx, MethodSelection{Method: task.Selection.Method, Input: tree.Input{Request: request}})
		if err != nil {
			return err
		}
		receipt, err := site.Invoke(ctx, prepared)
		if err != nil {
			return err
		}
		outcome, err := site.WaitOutcome(ctx, receipt.ID)
		if err != nil {
			return err
		}
		if outcome.Operation == nil || outcome.Operation.KernelOutcome != operation.OutcomeAcknowledgedUnverified {
			return fmt.Errorf("outcome %+v", outcome.Operation)
		}
		return nil
	}
	if code := run(env, []string{"program", installation, selectionFile}, app, factory); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if got.Selection == nil || got.Selection.Method != selection.Method || got.Selection.Input.Request == nil ||
		got.Selection.Input.Request.Operation != facts.op.Ref || len(got.Selection.Input.Request.Arguments) != 1 {
		t.Fatalf("the application got %+v, not the selected method and typed input", got.Selection)
	}
	if value, _, executed := factory.owner.read(); value != targetCoefficient || executed != 1 {
		t.Fatalf("owner %s after %d executions", value, executed)
	}
	_ = site0

	// One positional file, and the read-only marker, walk and invoke nothing.
	for _, args := range [][]string{{"program", installation}, {"program", installation, "-"}} {
		walkOnly := newSynthFactory(clock)
		cfg.StateDir = filepath.Join(t.TempDir(), "state")
		writeJSON(t, installation, cfg)
		got = Task{}
		if code := run(env, args, app, walkOnly); code != 0 || got.Selection != nil {
			t.Fatalf("%v: exit %d task %+v %s", args, code, got, stderr.String())
		}
		if _, _, executed := walkOnly.owner.read(); executed != 0 {
			t.Fatalf("%v invoked a setting", args)
		}
	}
}

func mustSegment(t *testing.T, facts operationFacts) string {
	t.Helper()
	segment, err := tree.DefinitionSegment(facts.op.Ref)
	if err != nil {
		t.Fatal(err)
	}
	return segment
}

func TestMainDefaultRootAndExplicitFailures(t *testing.T) {
	t.Setenv("TZ", "UTC")
	clock := newFixedClock(time.Now())
	env, stderr, root := testEnvironment(t)
	app := func(ctx context.Context, site *Site, task Task) error {
		cut, err := site.Cut(ctx)
		if err != nil {
			return err
		}
		return cut.Close()
	}
	// No file: walk-only with a durable default identity, created below the root.
	factory := newSynthFactory(clock)
	if code := run(env, []string{"program"}, app, factory); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if len(factory.planned) != 0 || len(factory.constructedNames()) != 0 {
		t.Fatal("an absent default file configures no instance")
	}
	recordPath := filepath.Join(root, localIdentityFile)
	first, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var identity localIdentity
	if err := json.Unmarshal(first, &identity); err != nil || identity.InstallationKey == "" || identity.BridgeID == "" {
		t.Fatalf("identity record %s %v", first, err)
	}
	if code := run(env, []string{"program"}, app, newSynthFactory(clock)); code != 0 {
		t.Fatalf("reopen exit %d: %s", code, stderr.String())
	}
	if again, _ := os.ReadFile(recordPath); !bytes.Equal(first, again) {
		t.Fatal("reopening changed the committed identity")
	}
	// A valid default file starts its active instances, under a durable identity
	// of its own root.
	env2, stderr2, root2 := testEnvironment(t)
	cfg := baseConfig(t, nil)
	cfg.StateDir, cfg.InstallationKey, cfg.BridgeID = "", "", ""
	writeJSON(t, filepath.Join(root2, "installation.json"), cfg)
	started := newSynthFactory(clock)
	if code := run(env2, []string{"program"}, app, started); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr2.String())
	}
	if got := started.constructedNames(); len(got) != 5 {
		t.Fatalf("constructed %v, want the five enabled instances", got)
	}
	if _, err := os.Stat(filepath.Join(root2, localIdentityFile)); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(root, "installation.json"), cfg)
	// A malformed default is an error and starts nothing.
	if err := os.WriteFile(filepath.Join(root, "installation.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := newSynthFactory(clock)
	if code := run(env, []string{"program"}, app, bad); code == 0 || len(bad.constructedNames()) != 0 {
		t.Fatalf("malformed default exit %d constructed %v", code, bad.constructedNames())
	}
	// A missing or malformed explicit path exits before native startup.
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.json"), filepath.Join(root, "installation.json")} {
		explicit := newSynthFactory(clock)
		if code := run(env, []string{"program", path}, app, explicit); code == 0 || len(explicit.constructedNames()) != 0 {
			t.Fatalf("%s exit %d constructed %v", path, code, explicit.constructedNames())
		}
	}
}

func TestLocalIdentityIsPerStateDirectoryAndConfiguredValuesWin(t *testing.T) {
	a, b := Config{StateDir: t.TempDir()}, Config{StateDir: t.TempDir()}
	for _, cfg := range []*Config{&a, &b} {
		if err := withLocalIdentity(cfg); err != nil {
			t.Fatal(err)
		}
	}
	if a.InstallationKey == b.InstallationKey || a.BridgeID == b.BridgeID {
		t.Fatalf("two independent state directories share an identity: %+v %+v", a, b)
	}
	again := Config{StateDir: a.StateDir}
	if err := withLocalIdentity(&again); err != nil || again.InstallationKey != a.InstallationKey || again.BridgeID != a.BridgeID {
		t.Fatalf("reopening: %+v %v", again, err)
	}
	explicit := Config{StateDir: t.TempDir(), InstallationKey: "installation.configured", BridgeID: "bridge.configured"}
	if err := withLocalIdentity(&explicit); err != nil || explicit.InstallationKey != "installation.configured" || explicit.BridgeID != "bridge.configured" {
		t.Fatalf("configured identity %+v %v", explicit, err)
	}
	if _, err := os.Stat(filepath.Join(explicit.StateDir, localIdentityFile)); err == nil {
		t.Fatal("a fully configured identity wrote a local record")
	}
	partial := Config{StateDir: a.StateDir, InstallationKey: "installation.configured"}
	if err := withLocalIdentity(&partial); err != nil || partial.InstallationKey != "installation.configured" || partial.BridgeID != a.BridgeID {
		t.Fatalf("a configured value takes precedence over the record: %+v %v", partial, err)
	}
}

// assertConfigMapping compares what each factory Plan received with the
// configuration: the configured key and enablement, the two separate byte
// operands untouched, and the secret reference only on its own instance.
func assertConfigMapping(t *testing.T, factory *synthFactory, cfg Config) {
	t.Helper()
	want := map[string]string{"plain.a": secretAlpha, "plain.b": secretBeta}
	for _, ic := range cfg.Instances {
		got, ok := factory.planned[string(ic.Instance)]
		if !ok {
			t.Fatalf("%s was not planned", ic.Instance)
		}
		if got.Instance != string(ic.Instance) || got.Enabled != ic.Enabled {
			t.Fatalf("%s: instance %q enabled %v, want %q %v", ic.Instance, got.Instance, got.Enabled, ic.Instance, ic.Enabled)
		}
		if !bytes.Equal(got.Transport, ic.Transport) || !bytes.Equal(got.Options, ic.Options) {
			t.Fatalf("%s: operands changed: transport %s options %s", ic.Instance, got.Transport, got.Options)
		}
		if got.SecretRef != want[string(ic.Instance)] {
			t.Fatalf("%s received secret reference %q, want %q", ic.Instance, got.SecretRef, want[string(ic.Instance)])
		}
		if bytes.Contains(got.Transport, []byte("ref.synthetic")) || bytes.Contains(got.Options, []byte("ref.synthetic")) {
			t.Fatalf("%s: a reference entered a driver-owned operand", ic.Instance)
		}
	}
}

func publicText(t *testing.T, ctx context.Context, site *Site) string {
	t.Helper()
	var out strings.Builder
	cut, err := site.Cut(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cut.Close()
	record := walkAll(t, ctx, cut, 7)
	for ref, encoded := range record.Attributes {
		out.WriteString(ref + encoded)
	}
	out.WriteString(strings.Join(record.Nodes, ""))
	fmt.Fprintf(&out, "%+v", record.Report)
	for _, ic := range site.rt.Plan().Instances {
		status, err := site.Snapshot(ic.Ref)
		fmt.Fprintf(&out, "%+v %v", status, err)
	}
	return out.String()
}

func TestSecretReferencesReachOnlyTheirInstanceThroughFileAndEmbedding(t *testing.T) {
	t.Setenv("TZ", "UTC")
	ctx := context.Background()
	clock := newFixedClock(time.Now())
	cfg := baseConfig(t, nil)
	cfg.Clock = clock

	embedded := newSynthFactory(clock)
	site := openStarted(t, cfg, embedded)
	assertConfigMapping(t, embedded, cfg)
	if text := publicText(t, ctx, site); strings.Contains(text, secretAlpha) || strings.Contains(text, secretBeta) {
		t.Fatal("a secret reference reached public output")
	}
	if err := site.Close(ctx); err != nil {
		t.Fatal(err)
	}

	env, stderr, _ := testEnvironment(t)
	fileCfg := cfg
	fileCfg.StateDir = filepath.Join(t.TempDir(), "state")
	path := filepath.Join(t.TempDir(), "installation.json")
	writeJSON(t, path, fileCfg)
	fromFile := newSynthFactory(clock)
	var text string
	app := func(ctx context.Context, site *Site, _ Task) error {
		text = publicText(t, ctx, site)
		return nil
	}
	if code := run(env, []string{"program", path}, app, fromFile); code != 0 {
		t.Fatalf("exit %d %s", code, stderr.String())
	}
	assertConfigMapping(t, fromFile, cfg)
	if strings.Contains(text, secretAlpha) || strings.Contains(text, secretBeta) || strings.Contains(stderr.String(), secretAlpha) || strings.Contains(stderr.String(), secretBeta) {
		t.Fatal("a secret reference reached public output or a diagnostic")
	}

	// An ordinary diagnostic error names neither reference.
	broken := cfg
	broken.StateDir = t.TempDir()
	broken.Instances = append(append([]InstanceConfig(nil), cfg.Instances...), cfg.Instances[2])
	if _, err := Open(ctx, broken, newSynthFactory(clock)); err == nil || strings.Contains(err.Error(), secretAlpha) || strings.Contains(err.Error(), secretBeta) {
		t.Fatalf("a duplicate instance must be refused without naming a reference: %v", err)
	}
}

func TestUnmatchedKindIsReportedAndStoppedInstanceStaysUnconstructed(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	cfg := baseConfig(t, clock)
	cfg.Instances = []InstanceConfig{
		{Kind: kindPlain, Instance: "plain.a", Enabled: true, Transport: transport("a"), Options: options(kindPlain)},
		{Kind: kindPlain, Instance: "plain.b", Enabled: true, Transport: transport("b"), Options: options(kindPlain)},
	}
	first := openStarted(t, cfg, newSynthFactory(clock))
	ref := life.Ref{Driver: kindPlain, Instance: "plain.a"}
	status, err := first.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetDesired(ctx, ref, life.MethodStop, status.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}

	cfg.Instances = append(cfg.Instances, InstanceConfig{Kind: "synth.ghost", Instance: "ghost.one", Enabled: true, Options: options("synth.ghost.unplanned")})
	cfg.Instances[2].Options = json.RawMessage(`{}`)
	factory := newSynthFactory(clock)
	second := openStarted(t, cfg, factory)
	defer second.Close(ctx)
	if got := second.Unmatched(); len(got) != 1 || got[0] != "synth.ghost/ghost.one" {
		t.Fatalf("unmatched %v, want the kind and instance by name", got)
	}
	constructed := strings.Join(factory.constructedNames(), ",")
	if strings.Contains(constructed, "plain.a") || !strings.Contains(constructed, "plain.b") {
		t.Fatalf("constructed %q: the persisted STOP stays unconstructed and the peer starts", constructed)
	}
	stopped, err := second.Snapshot(ref)
	if err != nil || stopped.DesiredState != life.DesiredStopped {
		t.Fatalf("plain.a %+v %v", stopped, err)
	}
	peer, err := second.Snapshot(life.Ref{Driver: kindPlain, Instance: "plain.b"})
	if err != nil || peer.ObservedState != life.ObservedRunning {
		t.Fatalf("plain.b %+v %v", peer, err)
	}
}

type checkpointView struct {
	Nodes []struct {
		Ref driver.NodeRef `json:"ref"`
	} `json:"nodes"`
	Certifications []struct {
		Join    driver.CertifiedJoin `json:"join"`
		Revoked bool                 `json:"revoked"`
	} `json:"certifications"`
	Rebinds []struct {
		Old     driver.NodeRef       `json:"old"`
		New     driver.NodeRef       `json:"new"`
		Join    driver.CertifiedJoin `json:"join"`
		Revoked bool                 `json:"revoked"`
	} `json:"rebinds"`
}

func readCheckpoint(t *testing.T, stateDir string) checkpointView {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(stateDir, "identity", "checkpoint.json"))
	if err != nil {
		t.Fatal(err)
	}
	var view checkpointView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func statementJoin(a, b, survivor driver.NodeRef, revision string, label string) driver.CertifiedJoin {
	return driver.CertifiedJoin{A: a, B: b, Survivor: survivor, Statement: driver.StatementRef{Revision: v1.Uint64(revision), Evidence: evidenceOf("statement:" + label)}}
}

func TestOperatorIdentityStatementsOnlyThroughTheTrustedTypedHandoff(t *testing.T) {
	t.Setenv("TZ", "UTC")
	ctx := context.Background()
	clock := newFixedClock(time.Now())
	cfg := baseConfig(t, clock)
	cfg.Instances = cfg.Instances[2:5]
	for i := range cfg.Instances {
		cfg.Instances[i].SecretRef = ""
	}

	// Initial launch: separate resources, no statement.
	factory := newSynthFactory(clock)
	site := openStarted(t, cfg, factory)
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
	if len(members) < 3 {
		t.Fatalf("members %v", members)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
	a, b := members[0], members[1]
	view := readCheckpoint(t, cfg.StateDir)
	if len(view.Certifications) != 0 || len(view.Rebinds) != 0 {
		t.Fatalf("separate resources carry no statement: %+v", view)
	}

	// The ordinary file cases carry statement-looking JSON and apply nothing.
	env, stderr, _ := testEnvironment(t)
	raw, _ := json.Marshal(cfg)
	var document map[string]any
	_ = json.Unmarshal(raw, &document)
	statement := map[string]any{"Action": "certify", "Join": statementJoin(a, b, a, "1", "file"), "operator_identity_statements": true}
	document["OperatorIdentityStatements"] = []any{statement}
	document["operator_identity_statements"] = []any{statement}
	for name, state := range map[string]string{"explicit": "", "default": "default"} {
		fileCfg := document
		path := filepath.Join(t.TempDir(), "installation.json")
		args := []string{"program", path}
		if state == "default" {
			args = []string{"program"}
			_, _, root := testEnvironment(t)
			env = environment{root: func() (string, error) { return root, nil }, stderr: stderr}
			path = filepath.Join(root, "installation.json")
		}
		writeJSON(t, path, fileCfg)
		if code := run(env, args, func(context.Context, *Site, Task) error { return nil }, newSynthFactory(clock)); code != 0 {
			t.Fatalf("%s: exit %d %s", name, code, stderr.String())
		}
	}
	view = readCheckpoint(t, cfg.StateDir)
	if len(view.Certifications) != 0 || len(view.Rebinds) != 0 {
		t.Fatalf("file input applied a statement: %+v", view)
	}

	certify := runtime.OperatorIdentityStatement{Action: runtime.OperatorCertify, Join: statementJoin(a, b, a, "1", "certify")}
	rebind := runtime.OperatorIdentityStatement{Action: runtime.OperatorRebind, Join: statementJoin(a, b, b, "2", "rebind")}
	reopen := func(statements ...runtime.OperatorIdentityStatement) error {
		c := cfg
		c.OperatorIdentityStatements = statements
		opened, err := Open(ctx, c, newSynthFactory(clock))
		if err != nil {
			return err
		}
		if err := opened.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if err := opened.WaitInitial(ctx); err != nil {
			t.Fatal(err)
		}
		return opened.Close(ctx)
	}
	if err := reopen(certify, rebind); err != nil {
		t.Fatal(err)
	}
	view = readCheckpoint(t, cfg.StateDir)
	if len(view.Certifications) != 1 || view.Certifications[0].Revoked || view.Certifications[0].Join.Survivor != a ||
		len(view.Rebinds) != 1 || view.Rebinds[0].Old != a || view.Rebinds[0].New != b || view.Rebinds[0].Revoked {
		t.Fatalf("exact certify and rebind: %+v", view)
	}

	// A stale or mismatched revocation is rejected and changes nothing.
	staleJoin := statementJoin(a, b, a, "9", "stale")
	mismatch := runtime.OperatorIdentityStatement{Action: runtime.OperatorRevoke, Join: certify.Join, TargetRevision: "2"}
	stale := runtime.OperatorIdentityStatement{Action: runtime.OperatorRevoke, Join: staleJoin, TargetRevision: "9"}
	if err := reopen(mismatch); !errors.Is(err, runtime.ErrStatementRevision) {
		t.Fatalf("mismatched revision: %v", err)
	}
	if err := reopen(stale); !errors.Is(err, identity.ErrWrongStatement) {
		t.Fatalf("stale statement: %v", err)
	}
	if view = readCheckpoint(t, cfg.StateDir); view.Certifications[0].Revoked || view.Rebinds[0].Revoked {
		t.Fatalf("a rejected revocation changed the disposition: %+v", view)
	}

	// An exact revocation revokes the certification; the rebind it did not name stays.
	if err := reopen(runtime.OperatorIdentityStatement{Action: runtime.OperatorRevoke, Join: certify.Join, TargetRevision: "1"}); err != nil {
		t.Fatal(err)
	}
	view = readCheckpoint(t, cfg.StateDir)
	if !view.Certifications[0].Revoked || view.Rebinds[0].Revoked {
		t.Fatalf("exact revocation: %+v", view)
	}
	// A second launch with no statement restores both refs and the disposition.
	if err := reopen(); err != nil {
		t.Fatal(err)
	}
	again := readCheckpoint(t, cfg.StateDir)
	if fmt.Sprint(again) != fmt.Sprint(view) {
		t.Fatalf("restored %+v, want %+v", again, view)
	}
	var seenA, seenB bool
	for _, n := range again.Nodes {
		seenA, seenB = seenA || n.Ref == a, seenB || n.Ref == b
	}
	if !seenA || !seenB {
		t.Fatal("both member references stay resolvable")
	}
}

func TestHostUsesConfiguredClockAndCalendar(t *testing.T) {
	ctx := context.Background()
	// 2030-06-15T23:30:00Z is the next civil day in Auckland.
	instant := time.Date(2030, 6, 15, 23, 30, 0, 0, time.UTC)
	clock := newFixedClock(instant)
	clock.mono = 123456
	cfg := baseConfig(t, clock)
	cfg.Calendar = ptr("Pacific/Auckland")
	cfg.Instances = append([]InstanceConfig{cfg.Instances[0], cfg.Instances[1]}, cfg.Instances[2:4]...)
	factory := newSynthFactory(clock)
	site := openStarted(t, cfg, factory)
	defer site.Close(ctx)

	// An accepted STOP persists the configured clock's timing, not ambient time.
	ref := life.Ref{Driver: kindPlain, Instance: "plain.b"}
	status, err := site.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.SetDesired(ctx, ref, life.MethodStop, status.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if st, _ := site.Snapshot(ref); st.ObservedState == life.ObservedStopped {
			break
		}
	}
	var persisted strings.Builder
	_ = filepath.Walk(filepath.Join(cfg.StateDir, "lifecycle"), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			raw, _ := os.ReadFile(path)
			persisted.Write(raw)
		}
		return nil
	})
	if !strings.Contains(persisted.String(), "2030-06-15T23:30:30Z") || strings.Contains(persisted.String(), fmt.Sprint(time.Now().Year())+"-") {
		t.Fatalf("persisted timing does not come from the configured clock: %.2500s", persisted.String())
	}
	// The recovery anchor is the configured clock's.
	var store struct {
		Recovery struct {
			Payload []byte `json:"payload"`
		} `json:"recovery"`
	}
	var envelope struct {
		Anchor []byte `json:"anchor"`
	}
	for _, name := range []string{"runtime-control.json"} {
		if raw, err := os.ReadFile(filepath.Join(cfg.StateDir, "lifecycle", name)); err == nil {
			_ = json.Unmarshal(raw, &store)
		}
	}
	if err := json.Unmarshal(store.Recovery.Payload, &envelope); err != nil || string(envelope.Anchor) != "123456" {
		t.Fatalf("recovery anchor %q (%v) is not the configured clock's sample", envelope.Anchor, err)
	}
	// The capture's evaluation clock is the configured clock too.
	capture, err := site.Tree().SemanticInput(driver.Filter{}).Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, snap := range capture.Snapshots {
		if got, want := snap.EvaluatedAt.UnixNanoseconds, timePoint(instant).UnixNanoseconds; got != want {
			t.Fatalf("capture evaluated at %s, want the configured instant %s", got, want)
		}
	}

	calendar := factory.calendars["plain.a"]
	if calendar == nil {
		t.Fatal("the started instance received no calendar")
	}
	acquisition := calendar.BeginAcquisition()
	reference := acquisition.Reference()
	if reference.Date() != (driver.CalendarDate{Year: 2030, Month: time.June, Day: 16}) || !reference.Instant().Equal(instant) {
		t.Fatalf("reference %+v at %s: the civil date is Auckland's, the instant the observation's", reference.Date(), reference.Instant())
	}
	if reference.Month() != (driver.CalendarMonth{Year: 2030, Month: time.June}) || reference.PreviousMonth() != (driver.CalendarMonth{Year: 2030, Month: time.May}) ||
		reference.Year() != 2030 || reference.PreviousYear() != 2029 {
		t.Fatalf("civil components %+v", reference)
	}
	// A reply on the same civil date is accepted.
	clock.set(instant.Add(20 * time.Minute))
	if err := acquisition.Check(calendar); err != nil {
		t.Fatalf("same date: %v", err)
	}
	if !acquisition.Reference().Instant().Equal(instant) {
		t.Fatal("the original observation instant was not preserved")
	}
	// At the configured civil day boundary a late reply is refused, and stays refused.
	clock.set(time.Date(2030, 6, 16, 12, 0, 0, 0, time.UTC))
	if err := acquisition.Check(calendar); err == nil {
		t.Fatal("a reply after the civil day boundary was accepted")
	}
	clock.set(instant)
	if err := acquisition.Check(calendar); err == nil {
		t.Fatal("an aborted acquisition recovered when the clock returned")
	}
	// Another site's calendar is another generation: no cross-site reuse.
	otherCfg := baseConfig(t, clock)
	otherCfg.Calendar = ptr("Pacific/Auckland")
	otherCfg.Instances = otherCfg.Instances[2:3]
	otherFactory := newSynthFactory(clock)
	other := openStarted(t, otherCfg, otherFactory)
	defer other.Close(ctx)
	fresh := calendar.BeginAcquisition()
	if err := fresh.Check(otherFactory.calendars["plain.a"]); err == nil {
		t.Fatal("an acquisition was accepted by another site's calendar")
	}
	if err := calendar.BeginAcquisition().Check(calendar); err != nil {
		t.Fatalf("an acquisition checked against its own calendar: %v", err)
	}
	// An invalid explicit location is refused.
	for _, bad := range []string{"", "Local", "Etc/GMT-3", "CET", " Europe/Paris", "Not/AZone"} {
		invalid := baseConfig(t, clock)
		invalid.Calendar = ptr(bad)
		if _, err := Open(ctx, invalid, newSynthFactory(clock)); err == nil {
			t.Fatalf("explicit location %q was accepted", bad)
		}
	}
}

func TestHostExecutorRegistrationIsScopedToItsInstanceAndGeneration(t *testing.T) {
	ctx := context.Background()
	facts := thermalSetting(t)
	clock := newFixedClock(baseTime.Add(time.Second))
	cfg := baseConfig(t, clock)
	cfg.Instances = cfg.Instances[:2]
	factory := newSynthFactory(clock)
	site := openStarted(t, cfg, factory)
	defer site.Close(ctx)
	consumer := factory.started["consumer.one"]
	owner := factory.ownerInstances()[0]
	operations := consumer.session.ScopedOperations()

	// Both paths reach the one executor the owner registered during Start.
	finite := requestFor(t, ctx, site, facts, targetCoefficient)
	finite.Timeout = 20 * time.Second
	result, err := operations.Invoke(ctx, *finite)
	if err != nil || result.KernelOutcome != operation.OutcomeAcknowledgedUnverified {
		t.Fatalf("finite %+v %v", result, err)
	}
	bound := requestFor(t, ctx, site, facts, targetCoefficient)
	boundCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result, err = operations.InvokeCallBound(boundCtx, *bound)
	if err != nil || result.KernelOutcome != operation.OutcomeAcknowledgedUnverified {
		t.Fatalf("call-bound %+v %v", result, err)
	}
	if _, _, executed := factory.owner.read(); executed != 2 {
		t.Fatalf("the owner's callback ran %d times, want 2", executed)
	}

	// A registration from another instance's session is foreign and sends nothing.
	if _, err := consumer.session.RegisterExecutor(owner.route, owner); err == nil {
		t.Fatal("a foreign instance registered the owner's route")
	}
	if _, _, executed := factory.owner.read(); executed != 2 {
		t.Fatal("a foreign registration sent")
	}

	// A restart retires the generation: the old unregister cannot touch the successor.
	oldUnregister, oldSession := owner.unregister, owner.session
	ref := life.Ref{Driver: kindOwner, Instance: "owner.one"}
	status, err := site.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.SetDesired(ctx, ref, life.MethodRestart, status.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); len(factory.ownerInstances()) < 2 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	if len(factory.ownerInstances()) < 2 {
		t.Fatal("the owner did not restart")
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if st, _ := site.Snapshot(ref); st.ObservedState == life.ObservedRunning && st.Generation >= 1 && factory.ownerInstances()[1].started.Load() {
			break
		}
	}
	if _, err := oldSession.RegisterExecutor(owner.route, owner); err == nil {
		t.Fatal("a retired session registered an executor")
	}
	oldUnregister()
	before, _, _ := factory.owner.read()
	_ = before
	next := requestFor(t, ctx, site, facts, targetCoefficient)
	next.Timeout = 20 * time.Second
	result, err = operations.Invoke(ctx, *next)
	if err != nil || result.KernelOutcome != operation.OutcomeAcknowledgedUnverified {
		t.Fatalf("the successor was disabled by the old unregister: %+v %v", result, err)
	}
	if _, _, executed := factory.owner.read(); executed != 3 {
		t.Fatalf("executed %d, want the successor's callback to run once more", executed)
	}
	// The request of the retired generation reaches no callback.
	stale := *finite
	stale.Timeout = 20 * time.Second
	result, err = operations.Invoke(ctx, stale)
	if err != nil || result.KernelOutcome == operation.OutcomeAcknowledgedUnverified || result.Delivery == operation.DeliverySent {
		t.Fatalf("a retired generation's request was sent: %+v %v", result, err)
	}
	if _, _, executed := factory.owner.read(); executed != 3 {
		t.Fatalf("a retired request sent: executed %d", executed)
	}
}

const childEnv = "SEMREG_HOST_TEST_CHILD"

// TestHostChildProcess is the synthetic child host of the process-loss test. It
// does nothing unless the parent test started it.
func TestHostChildProcess(t *testing.T) {
	dir := os.Getenv(childEnv)
	if dir == "" {
		t.Skip("child host of TestHostResumesManagedPublicationAfterProcessLoss")
	}
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	factory := newSynthFactory(clock)
	cfg := baseConfig(t, clock)
	cfg.StateDir = dir
	cfg.Instances = cfg.Instances[:1]
	site := openStarted(t, cfg, factory)
	ref := life.Ref{Driver: kindOwner, Instance: "owner.one"}
	status, err := site.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	factory.hangStop = true
	method := life.Method(os.Getenv(childEnv + "_METHOD"))
	if _, err := site.SetDesired(ctx, ref, method, status.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if st, _ := site.Snapshot(ref); st.ObservedState == life.ObservedStopping || st.ObservedState == life.ObservedStarting {
			break
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "child.ready"), []byte(string(status.Epoch)), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Minute)
}

func TestHostResumesManagedPublicationAfterProcessLoss(t *testing.T) {
	ctx := context.Background()
	for _, method := range []life.Method{life.MethodStop, life.MethodRestart} {
		t.Run(string(method), func(t *testing.T) {
			clock := newFixedClock(baseTime.Add(time.Second))
			cfg := baseConfig(t, clock)
			cfg.Instances = cfg.Instances[:1]
			ref := life.Ref{Driver: kindOwner, Instance: "owner.one"}

			cmd := exec.Command(os.Args[0], "-test.run=^TestHostChildProcess$")
			cmd.Env = append(os.Environ(), childEnv+"="+cfg.StateDir, childEnv+"_METHOD="+string(method))
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			var originalEpoch []byte
			for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				if raw, err := os.ReadFile(filepath.Join(cfg.StateDir, "child.ready")); err == nil {
					originalEpoch = raw
					break
				}
			}
			if len(originalEpoch) == 0 {
				t.Fatal("the child host never committed its transition")
			}
			control := filepath.Join(cfg.StateDir, "lifecycle", "runtime-control.json")
			var committed []byte
			for stable := 0; stable < 5; {
				time.Sleep(60 * time.Millisecond)
				raw, err := os.ReadFile(control)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Equal(raw, committed) {
					stable++
				} else {
					committed, stable = raw, 0
				}
			}

			// While the child still holds the lock, nothing is admitted and the store is untouched.
			held := newSynthFactory(clock)
			heldSite, err := Open(ctx, cfg, held)
			if err != nil {
				t.Fatal(err)
			}
			_ = heldSite.Start(ctx)
			_ = heldSite.Close(ctx)
			if got := held.constructedNames(); len(got) != 0 {
				t.Fatalf("a second host constructed %v while the child held the lock", got)
			}
			if after, _ := os.ReadFile(control); !bytes.Equal(after, committed) {
				t.Fatal("the held store was changed")
			}

			// The child is lost with the transition still active.
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()

			factory := newSynthFactory(clock)
			site := openStarted(t, cfg, factory)
			defer site.Close(ctx)
			var status life.Status
			for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				status, _ = site.Snapshot(ref)
				if method == life.MethodStop && status.ObservedState == life.ObservedStopped {
					break
				}
				if method == life.MethodRestart && status.ObservedState == life.ObservedRunning && string(status.Epoch) != string(originalEpoch) {
					break
				}
			}
			constructed := factory.constructedNames()
			switch method {
			case life.MethodStop:
				if status.ObservedState != life.ObservedStopped || len(constructed) != 0 {
					t.Fatalf("STOP: %+v constructed %v: it reaches stopped with no new construction", status, constructed)
				}
			case life.MethodRestart:
				if status.ObservedState != life.ObservedRunning || string(status.Epoch) == string(originalEpoch) || len(constructed) != 1 {
					t.Fatalf("RESTART: %+v constructed %v (original epoch %s): one construction under a new correlation", status, constructed, originalEpoch)
				}
			}
			// Both obligations were settled under the original correlation, and the work slot is gone.
			type storeFile struct {
				Instances map[string]struct {
					Current *struct {
						Kind                   string `json:"kind"`
						Phase                  string `json:"phase"`
						NativeClosePending     bool   `json:"native_close_pending"`
						SemanticCleanupPending bool   `json:"semantic_cleanup_pending"`
					} `json:"current"`
				} `json:"instances"`
			}
			var store storeFile
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				raw, err := os.ReadFile(control)
				if err != nil {
					t.Fatal(err)
				}
				store = storeFile{}
				if err := json.Unmarshal(raw, &store); err != nil {
					t.Fatal(err)
				}
				if store.Instances[string(ref.ID())].Current == nil {
					break
				}
			}
			if record := store.Instances[string(ref.ID())]; record.Current != nil {
				t.Fatalf("the transition was not settled: %+v", record.Current)
			}
			if method == life.MethodStop {
				cut, err := site.Tree().SemanticInput(driver.Filter{}).Capture(ctx, driver.Filter{})
				if err != nil {
					t.Fatal(err)
				}
				value, _ := zoneFactCoefficient(t, cut)
				_ = value
			}
		})
	}
}
