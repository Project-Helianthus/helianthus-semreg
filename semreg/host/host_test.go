package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/call"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/catalog"
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
	Nodes         []string
	Attributes    map[string]string
	Methods       int
	Report        WalkReport
	Links         []runtime.PlannedLink
	RetainedLinks []identity.Link
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
		if e.Link != nil {
			record.Links = append(record.Links, *e.Link)
		}
		if e.RetainedLink != nil {
			record.RetainedLinks = append(record.RetainedLinks, *e.RetainedLink)
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
	Walk                          walkRecord
	Graph                         graphRecord
	Lens                          graph.LensResult
	Outcome                       tree.Outcome
	Receipt                       tree.Receipt
	Request                       driver.OperationRequest
	Value                         string
	Last                          driver.Dispatch
	Executed                      int
	Feed                          driver.Changes
	SumBefore                     graph.MemberSum
	CompleteBefore, CompleteAfter graph.MemberSum
	Semantic                      driver.SemanticCut
	LensA                         []byte
	LensB                         []byte
}

func runSecondHost(t *testing.T, cfg Config, factory *synthFactory, page int, target string) secondHostRun {
	t.Helper()
	ctx := context.Background()
	facts := thermalSetting(t)
	factory.groupZone = true
	factory.families = []catalog.Family{
		{FamilyID: "fixture.members", Name: "Synthetic members", State: v1.QualificationCandidate},
		{FamilyID: "fixture.settings", Name: "Synthetic settings", State: v1.QualificationUnknown},
	}
	site := openStarted(t, cfg, factory)
	if got := site.Families(); !reflect.DeepEqual(got, factory.families) {
		t.Fatalf("families: %+v, want %+v", got, factory.families)
	}
	if slices.Contains(factory.constructedNames(), "synth.plain/idle.one") {
		t.Fatal("disabled instance constructed")
	}
	var out secondHostRun
	app := func(ctx context.Context, site *Site, task Task) error {
		cut, err := site.Cut(ctx)
		if err != nil {
			return err
		}
		defer cut.Close()
		out.Walk = walkAll(t, ctx, cut, page)
		out.Graph, out.Lens = graphOf(t, ctx, site, cut)
		consumer := factory.started["consumer.one"]
		input := consumer.session.SemanticInput()
		captured, err := input.Capture(ctx, driver.Filter{})
		if err != nil {
			return err
		}
		out.LensA, _ = mustLens(t, ctx, input, captured.Ref)
		sum, err := input.SumMembers(ctx, captured.Ref, lensRequest(), v1.DefinitionRef{Pack: facts.op.Ref.Pack, ID: facts.op.Argument.ID, Version: facts.op.Ref.Version}, anyResource(t, ctx, input, captured.Ref))
		if err != nil {
			return err
		}
		out.SumBefore = sum
		out.Semantic = captured
		value, binding := zoneFactCoefficient(t, captured)
		if value != initialCoefficient || binding.AssetID == "" {
			t.Fatalf("initial setting %q with binding %+v", value, binding)
		}
		// The owner partition is complete; its thermal system groups the zone.
		sumCut, err := input.Capture(ctx, driver.Filter{Assets: []v1.AssetID{binding.AssetID}})
		if err != nil {
			return err
		}
		system := v1.ResourceID("resource:thermal:synthetic")
		sumLens := lensRequest()
		sumLens.Entry = graph.EntryPoint{Kind: graph.EntryResource, Resource: system}
		definition := v1.DefinitionRef{Pack: facts.op.Ref.Pack, ID: "thermal.measurement.air_temperature", Version: "1.0.0"}
		out.CompleteBefore, err = input.SumMembers(ctx, sumCut.Ref, sumLens, definition, system)
		if err != nil {
			return err
		}
		// A different started session cannot use the consumer's captured scope.
		other := factory.ownerInstances()[0].session.SemanticInput()
		if widened, err := other.RetrieveLens(ctx, sumCut.Ref, sumLens); v1.ErrorIdentifier(err) != v1.DanglingReference || len(widened.Resources) != 0 {
			t.Fatalf("another session read the consumer cut: %+v (%v)", widened, err)
		}
		if widened, err := other.SumMembers(ctx, sumCut.Ref, sumLens, definition, system); v1.ErrorIdentifier(err) != v1.DanglingReference || widened.Value != nil {
			t.Fatalf("another session summed the consumer cut: %+v (%v)", widened, err)
		}
		request := requestIn(t, captured, facts, target)
		request.Timeout = 20 * time.Second
		selection := MethodSelection{Method: semanticMethod(t, site, facts), Input: tree.Input{Request: request}}
		out.Request = *request
		if page == 3 {
			api := site.Tree()
			direct, err := api.Capture(ctx, tree.CutOptions{})
			if err != nil {
				return err
			}
			defer direct.Close()
			assertTreeReads(t, ctx, direct, cfg)
			prepared, err := api.Prepare(ctx, direct.ID(), selection.Method, selection.Input)
			if err != nil {
				return err
			}
			out.Receipt, err = api.Call(ctx, prepared, tree.CallOptions{})
			if err != nil {
				return err
			}
			out.Outcome, err = api.Outcome(ctx, out.Receipt.ID)
			if err != nil {
				return err
			}
		} else {
			prepared, err := cut.Prepare(ctx, selection)
			if err != nil {
				return err
			}
			out.Receipt, err = site.Invoke(ctx, prepared)
			if err != nil {
				return err
			}
			out.Outcome, err = site.WaitOutcome(ctx, out.Receipt.ID)
			if err != nil {
				return err
			}
		}
		out.LensB, _ = mustLens(t, ctx, input, captured.Ref)
		out.CompleteAfter, err = input.SumMembers(ctx, sumCut.Ref, sumLens, definition, system)
		if err != nil {
			return err
		}
		feedCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out.Feed, err = input.Resume(feedCtx, driver.Cursor{}, driver.Filter{})
		if err != nil {
			st, _ := site.Snapshot(life.Ref{Driver: kindOwner, Instance: "owner.one"})
			return fmt.Errorf("feed: %w (outcome %+v) owner %+v errs %v", err, out.Outcome.Operation, st, factory.startErrs)
		}
		latest, err := input.Capture(ctx, driver.Filter{Assets: []v1.AssetID{binding.AssetID}})
		if err != nil {
			return err
		}
		newSum, err := input.SumMembers(ctx, latest.Ref, sumLens, definition, system)
		if err != nil {
			return err
		}
		if want := kelvin(target).Quantity; newSum.Withheld || !reflect.DeepEqual(newSum.Value, want) {
			t.Fatalf("new sum %+v, want %+v", newSum, want)
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

func assertTreeReads(t *testing.T, ctx context.Context, cut *tree.TreeCut, cfg Config) {
	t.Helper()
	root := driver.NodeRef{Bridge: cfg.BridgeID, ID: tree.NodePath{Installation: cfg.InstallationKey}.NodeID()}
	var attrs []tree.Ref
	for token := ""; ; {
		page, err := cut.List(ctx, root, tree.Page{Size: 3, Token: token})
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Entries {
			if entry.Kind == tree.EntryAttribute {
				attrs = append(attrs, entry.Ref)
			}
		}
		if page.Next == "" {
			break
		}
		token = page.Next
	}
	var readSetting bool
	for _, ref := range attrs {
		desc, err := cut.Describe(ctx, ref)
		if err != nil || desc.Ref != ref || desc.Kind != tree.EntryAttribute {
			t.Fatalf("describe %s: %+v %v", ref, desc, err)
		}
		value, err := cut.Read(ctx, ref, tree.ReadOptions{})
		if err != nil || value.Cut != cut.ID() {
			t.Fatalf("read %s: %+v %v", ref, value, err)
		}
		if strings.Contains(string(value.Encoded), initialCoefficient) && strings.Contains(string(value.Encoded), "unit.kelvin") && strings.Contains(string(value.Encoded), "thermal.setpoint.temperature_setpoint") {
			readSetting = true
		}
	}
	if !readSetting {
		t.Fatal("the TreeCut did not list, describe and read the owner's setting")
	}
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
	wantRetained := seedRetainedLink(t, cfg)
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
	wantKinds := []string{"resource.equipment", "resource.equipment", "resource.equipment", "resource.equipment", "resource.site", "resource.thermal_system", "resource.zone"}
	sort.Strings(wantKinds)
	if strings.Join(first.Graph.Kinds, ",") != strings.Join(wantKinds, ",") {
		t.Fatalf("graph kinds %v, want %v", first.Graph.Kinds, wantKinds)
	}

	// The walk exposes the three planned bridge-to-link relations: one relays
	// relation per transport-claiming instance, all from the one bridge node, each
	// to its own link node, with the native observation basis, and every end is an
	// equipment resource of the same capture's graph.
	assertPlannedLinks(t, first, cfg)

	// A second lifecycle over the same state directory shows the same graph.
	second := runSecondHost(t, cfg, newSynthFactory(clock), 100, targetCoefficient)
	if strings.Join(second.Walk.Nodes, "\n") != strings.Join(first.Walk.Nodes, "\n") {
		t.Fatalf("nodes differ across lifecycles:\n%v\n%v", second.Walk.Nodes, first.Walk.Nodes)
	}
	assertPlannedLinks(t, second, cfg)
	if fmt.Sprint(second.Walk.Links) != fmt.Sprint(first.Walk.Links) {
		t.Fatalf("planned links differ across lifecycles:\n%+v\n%+v", second.Walk.Links, first.Walk.Links)
	}
	for _, run := range []secondHostRun{first, second} {
		want := graph.MemberSum{Value: kelvin(initialCoefficient).Quantity}
		if !reflect.DeepEqual(run.CompleteBefore, want) || !reflect.DeepEqual(run.CompleteAfter, want) {
			t.Fatalf("complete sum: before %+v after %+v, want %+v", run.CompleteBefore, run.CompleteAfter, want)
		}
		assertSourceGraph(t, cfg, run.Semantic)
		if !reflect.DeepEqual(run.Walk.RetainedLinks, wantRetained) {
			t.Fatalf("retained graph links: %+v, want %+v", run.Walk.RetainedLinks, wantRetained)
		}
		if run.Value != targetCoefficient || run.Executed != 1 || run.Outcome.Receipt != run.Receipt.ID {
			t.Fatalf("readback %q after %d executions, outcome %s for receipt %s", run.Value, run.Executed, run.Outcome.Receipt, run.Receipt.ID)
		}
	}
	if fmt.Sprint(second.Graph) != fmt.Sprint(first.Graph) {
		t.Fatalf("graph differs across lifecycles:\n%+v\n%+v", second.Graph, first.Graph)
	}
}

// expectedEquipment names a configured equipment slot independently of the
// host's plan and its allocated endpoints.
func expectedEquipment(slot string) v1.ResourceID {
	sum := sha256.Sum256([]byte("helianthus.semgraph.resource/v1\x00resource.equipment\x00slot\x00\x00" + slot))
	return v1.ResourceID(fmt.Sprintf("resource:%x", sum[:16]))
}

// seedRetainedLink gives the host a durable ledger claim to restore through
// ordinary Open. The expected graph uses the handwritten link's endpoints
// and basis, not the ledger's eventual readout.
func seedRetainedLink(t *testing.T, cfg Config) []identity.Link {
	t.Helper()
	cat, err := graph.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	allocator, err := identity.NewAllocator(identity.NewMemoryStore(), cat)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := identity.NewLedger(allocator)
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range []string{"a", "b", "c"} {
		if _, err := ledger.Node(cfg.BridgeID, driver.NodeKey{Installation: cfg.InstallationKey, Driver: "synth.plain", Instance: driver.InstanceKey("plain." + slot), Kind: "resource.equipment", Anchor: driver.AnchorKey{Slot: "fixture.link." + slot}}); err != nil {
			t.Fatal(err)
		}
	}
	ref := func(name string) driver.NodeRef {
		return driver.NodeRef{Bridge: cfg.BridgeID, ID: expectedEquipment("fixture.link." + name)}
	}
	from, to, successor := ref("a"), ref("b"), ref("c")
	declared := driver.LinkKey{Type: v1.DefinitionRef{ID: "fixture.relation.peer", Version: "2.0.0", Pack: v1.PackRef{ID: "fixture.pack", Version: "1.1.0"}}, From: from, To: to}
	basis := []v1.EvidenceRef{evidenceOf("retained link basis")}
	publisher := identity.Publisher{Driver: "synth.plain", Instance: "plain.a"}
	if err := ledger.ClaimLink(publisher, driver.LinkClaim{Key: declared, Basis: basis}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Rebind(from, successor, statementJoin(from, successor, successor, "7", "retained link rebind")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cfg.StateDir, "identity")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := ledger.Export()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "checkpoint.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	effective := declared
	effective.From = successor
	return []identity.Link{{Key: effective, Claims: []identity.LinkClaimView{{Publisher: publisher, Declared: declared, Basis: basis}}}}
}

func assertPlannedLinks(t *testing.T, run secondHostRun, cfg Config) {
	t.Helper()
	bridge := driver.NodeRef{Bridge: cfg.BridgeID, ID: expectedEquipment("bridge")}
	want := map[runtime.PlannedLink]bool{}
	for _, suffix := range []string{"a", "b", "c"} {
		// The second-host fixture configures distinct, named synthetic link slots.
		end := driver.NodeRef{Bridge: cfg.BridgeID, ID: expectedEquipment("fixture.link." + suffix)}
		want[runtime.PlannedLink{Relation: "relation.relays", From: bridge, To: end, Basis: v1.RelationNativeObservation}] = true
	}
	for _, link := range run.Walk.Links {
		if !want[link] {
			t.Fatalf("unexpected planned link %+v (remaining %+v)", link, want)
		}
		delete(want, link)
	}
	if len(want) != 0 {
		t.Fatalf("missing planned links %+v", want)
	}
}

func assertSourceGraph(t *testing.T, cfg Config, cut driver.SemanticCut) {
	t.Helper()
	value, binding := zoneFactCoefficient(t, cut)
	ownerID := sha256.Sum256([]byte(`["synth.owner","owner.one"]`))
	asset := v1.AssetID(fmt.Sprintf("asset:instance:%x", ownerID[:8]))
	source := v1.SourceID(fmt.Sprintf("source:instance:%x", ownerID[:8]))
	epoch := sha256.Sum256([]byte(binding.SourceEpochID))
	boundID := v1.NativeBindingID(fmt.Sprintf("binding:owner@%x", epoch[:4]))
	if value != initialCoefficient || binding.AssetID != asset || binding.SourceID != source || binding.BindingID != boundID ||
		binding.DriverGeneration != "1" || binding.State != v1.BindingCurrent || binding.SourceEpochID == "" || binding.NativeResource != evidenceOf("native") {
		t.Fatalf("initial fact %q / source binding %+v", value, binding)
	}
	owner := driver.NodeRef{Bridge: cfg.BridgeID, ID: (tree.NodePath{Installation: cfg.InstallationKey, Driver: kindOwner, Instance: "owner.one"}).NodeID()}
	wantSource := driver.SourceRef{For: driver.Element{Subject: driver.NodeRef{Bridge: cfg.BridgeID, ID: zoneResource}, Key: thermalSetting(t).argument}, Producer: owner, PathKey: "configuration"}
	if len(cut.SettingRoutes) != 1 || !reflect.DeepEqual(cut.SettingRoutes[0].Owner, wantSource) {
		t.Fatalf("source paths %+v, want %+v", cut.SettingRoutes, wantSource)
	}
	for _, snap := range cut.Snapshots {
		if snap.AssetID != asset {
			continue
		}
		if len(snap.Relations) != 1 {
			t.Fatalf("graph of the owner: %+v %+v", snap.Relations, snap.IdentityLinks)
		}
		link := snap.Relations[0]
		if link.Definition != (v1.DefinitionRef{ID: "thermal.relation.system_member", Version: "1.0.0", Pack: v1.PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}}) ||
			link.From != "resource:thermal:synthetic" || link.To != zoneResource || link.State != v1.LinkQualified || link.BasisClass != v1.RelationConfiguration || !reflect.DeepEqual(link.Basis, []v1.EvidenceRef{evidenceOf("zone member")}) {
			t.Fatalf("member link %+v", link)
		}
		var qualified int
		for _, native := range snap.IdentityLinks {
			if native.State == v1.LinkWithdrawn {
				continue
			}
			qualified++
			if native.AssetID != asset || native.BindingID != boundID || native.Resource == nil || *native.Resource != zoneResource || native.State != v1.LinkQualified || !reflect.DeepEqual(native.Basis, []v1.EvidenceRef{evidenceOf("link")}) {
				t.Fatalf("native source link %+v", native)
			}
		}
		if qualified != 1 {
			t.Fatalf("owner has %d current identity links", qualified)
		}
		return
	}
	t.Fatal("owner partition missing from graph")
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
	// The selection file carries what the file can know before startup: the
	// operation, the argument, the preconditions and the timeout. The asset,
	// binding, capability, revisions and epoch belong to the running lifecycle.
	fileRequest := &driver.OperationRequest{
		Operation:     facts.op.Ref,
		Arguments:     []v1.TypedField{{ID: facts.op.Argument.ID, Value: kelvin(targetCoefficient)}},
		Preconditions: []operation.Precondition{},
		Timeout:       20 * time.Second,
	}
	selection := MethodSelection{Input: tree.Input{Request: fileRequest}}
	// The method ref names the installation; it is known from the configured key.
	selection.Method = tree.MethodRef(tree.Key{Node: tree.NodePath{Installation: cfg.InstallationKey}, Facet: tree.KeywordMethods, Domain: tree.KeywordSem, Segment: mustSegment(t, facts)}.String())
	selectionFile := filepath.Join(dir, "selection.json")
	writeJSON(t, selectionFile, selection)

	var got Task
	var wantRequest driver.OperationRequest
	app := func(ctx context.Context, site *Site, task Task) error {
		got = task
		cut, err := site.Cut(ctx)
		if err != nil {
			return err
		}
		defer cut.Close()
		if task.Selection == nil {
			assertWalkOnly(t, ctx, cut, cfg.InstallationKey, true)
			return nil
		}
		// Only the lifecycle's own identities and revisions are added to the
		// request the file decoded; every other field reaches Prepare as read.
		live := requestFor(t, ctx, site, facts, targetCoefficient)
		request := *task.Selection.Input.Request
		request.AssetID, request.BindingID, request.CapabilityInstanceID = live.AssetID, live.BindingID, live.CapabilityInstanceID
		request.ExpectedSemanticRevision, request.ExpectedCapabilityRevision = live.ExpectedSemanticRevision, live.ExpectedCapabilityRevision
		request.ExpectedCapabilityInstanceRevision, request.ExpectedDriverGeneration = live.ExpectedCapabilityInstanceRevision, live.ExpectedDriverGeneration
		request.ExpectedSourceEpochID = live.ExpectedSourceEpochID
		wantRequest = request
		prepared, err := cut.Prepare(ctx, MethodSelection{Method: task.Selection.Method, Input: tree.Input{Request: &request}})
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
	if !reflect.DeepEqual(got.Selection, &selection) {
		t.Fatalf("the application got %+v, not the selected method and typed input", got.Selection)
	}
	value, dispatched, executed := factory.owner.read()
	if value != targetCoefficient || executed != 1 {
		t.Fatalf("owner %s after %d executions", value, executed)
	}
	if !reflect.DeepEqual(dispatched.Request, wantRequest) {
		t.Fatalf("the operation did not carry the file's request:\n got %+v\nwant %+v", dispatched.Request, wantRequest)
	}
	if dispatched.Request.Operation != fileRequest.Operation || dispatched.Request.Timeout != fileRequest.Timeout ||
		!reflect.DeepEqual(dispatched.Request.Arguments, fileRequest.Arguments) || !reflect.DeepEqual(dispatched.Request.Preconditions, fileRequest.Preconditions) {
		t.Fatalf("a field the file states was not the one invoked: %+v", dispatched.Request)
	}

	// One positional file, and the read-only marker, walk and invoke nothing.
	for _, args := range [][]string{{"program", installation}, {"program", installation, "-"}} {
		walkOnly := newSynthFactory(clock)
		cfg.StateDir = filepath.Join(t.TempDir(), "state")
		writeJSON(t, installation, cfg)
		got = Task{}
		if code := run(env, args, app, walkOnly); code != 0 || got.Selection != nil {
			t.Fatalf("%v: exit %d task %+v %s", args, code, got, stderr.String())
		}
		if value, _, executed := walkOnly.owner.read(); executed != 0 || value != initialCoefficient {
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

func assertWalkOnly(t *testing.T, ctx context.Context, cut *Cut, installation string, wantSetting bool) {
	t.Helper()
	walk := walkAll(t, ctx, cut, 3)
	for _, ref := range expectedIndexRefs(installation) {
		if _, ok := walk.Attributes[ref]; !ok {
			t.Fatalf("walk did not visit %s", ref)
		}
	}
	if wantSetting && walk.Methods == 0 {
		t.Fatal("walk did not visit the configured setting method")
	}
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
		defer cut.Close()
		if task.Selection != nil {
			t.Fatal("default startup received a method selection")
		}
		assertWalkOnly(t, ctx, cut, site.rt.Plan().Installation, false)
		return nil
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

	// Exercise both ordinary files while the exact typed certification exists,
	// so accepting the forged rebind or revocation would change its disposition.
	certify := runtime.OperatorIdentityStatement{Action: runtime.OperatorCertify, Join: statementJoin(a, b, a, "1", "certify")}
	rebind := runtime.OperatorIdentityStatement{Action: runtime.OperatorRebind, Join: statementJoin(a, b, b, "2", "rebind")}
	revoke := runtime.OperatorIdentityStatement{Action: runtime.OperatorRevoke, Join: certify.Join, TargetRevision: "1"}
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
	if err := reopen(certify); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	statements := []runtime.OperatorIdentityStatement{rebind, revoke}
	document["OperatorIdentityStatements"] = statements
	document["operator_identity_statements"] = statements
	for _, name := range []string{"explicit", "default"} {
		t.Run(name, func(t *testing.T) {
			env, stderr, root := testEnvironment(t)
			path := filepath.Join(root, "installation.json")
			args := []string{"program"}
			if name == "explicit" {
				args = append(args, path)
			}
			writeJSON(t, path, document)
			if code := run(env, args, func(context.Context, *Site, Task) error { return nil }, newSynthFactory(clock)); code != 0 {
				t.Fatalf("exit %d %s", code, stderr.String())
			}
			view := readCheckpoint(t, cfg.StateDir)
			if len(view.Certifications) != 1 || view.Certifications[0].Join != certify.Join || view.Certifications[0].Revoked || len(view.Rebinds) != 0 {
				t.Fatalf("ordinary JSON changed the trusted records: %+v", view)
			}
		})
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
	gate := make(chan struct{})
	entered := make(chan string, 1)
	factory.mu.Lock()
	factory.holdStop = map[string]chan struct{}{"plain.b": gate}
	factory.stopEntered = entered
	factory.mu.Unlock()
	status, err := site.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := site.SetDesired(ctx, ref, life.MethodStop, status.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the stop never reached the instance")
	}
	// The stop is held in flight: its timing is in the record until released.
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
	close(gate)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if st, _ := site.Snapshot(ref); st.ObservedState == life.ObservedStopped {
			break
		}
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

	owner := factory.ownerInstances()[0]
	calendar := owner.session.Calendar()
	if calendar == nil {
		t.Fatal("the started instance received no calendar")
	}
	var query driver.CalendarDate
	acquisition := owner.energyQuery(func(date driver.CalendarDate) { query = date })
	reference := acquisition.Reference()
	if query != (driver.CalendarDate{Year: 2030, Month: time.June, Day: 16}) || reference.Date() != query || !reference.Instant().Equal(instant) {
		t.Fatalf("reference %+v at %s: the civil date is Auckland's, the instant the observation's", reference.Date(), reference.Instant())
	}
	if reference.Month() != (driver.CalendarMonth{Year: 2030, Month: time.June}) || reference.PreviousMonth() != (driver.CalendarMonth{Year: 2030, Month: time.May}) ||
		reference.Year() != 2030 || reference.PreviousYear() != 2029 {
		t.Fatalf("civil components %+v", reference)
	}
	// A reply on the same civil date is accepted.
	clock.set(instant.Add(20 * time.Minute))
	receipt := v1.MonotonicPoint{ClockEpochID: "clock-epoch:fixture-energy", Nanoseconds: "7531"}
	if err := owner.acceptEnergyReply(ctx, acquisition, receipt, "217"); err != nil {
		t.Fatalf("same-date energy reply: %v", err)
	}
	assertEnergy := func() {
		t.Helper()
		cut, err := owner.session.SemanticInput().Capture(ctx, driver.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, snap := range cut.Snapshots {
			for _, fact := range snap.Facts {
				for _, c := range fact.Candidates {
					if c.Key.FactID != "pv.energy.yield_day" {
						continue
					}
					count++
					wantReceipt := timePoint(instant.Add(20 * time.Minute))
					if c.Times.PhenomenonAt == nil || *c.Times.PhenomenonAt != timePoint(instant) || c.Times.ReceivedAt != wantReceipt || c.Times.ReceiptMonotonic != receipt ||
						coefficientOfValue(c.Value) != "217" || c.Value.Quantity.Unit != "unit.joule" || c.Origin.BindingID == nil {
						t.Fatalf("published energy evidence %+v", c)
					}
				}
			}
		}
		if count != 1 {
			t.Fatalf("published %d energy candidates", count)
		}
	}
	assertEnergy()
	late := owner.energyQuery(func(date driver.CalendarDate) {
		if date != query {
			t.Fatalf("late query date %+v, want %+v", date, query)
		}
	})
	if !acquisition.Reference().Instant().Equal(instant) {
		t.Fatal("the original observation instant was not preserved")
	}
	// At the configured civil day boundary a late reply is refused, and stays refused.
	clock.set(time.Date(2030, 6, 16, 12, 0, 0, 0, time.UTC))
	if err := owner.acceptEnergyReply(ctx, late, v1.MonotonicPoint{ClockEpochID: receipt.ClockEpochID, Nanoseconds: "9184"}, "319"); err == nil {
		t.Fatal("a reply after the civil day boundary was accepted")
	}
	clock.set(instant)
	if err := late.Check(calendar); err == nil {
		t.Fatal("an aborted acquisition recovered when the clock returned")
	}
	clock.set(instant.Add(20 * time.Minute))
	assertEnergy()
	clock.set(instant)
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
	factory.keepExecutor = true
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

	assertLocalAbsence := func(req driver.OperationRequest, want driver.OperationResult, count int) {
		t.Helper()
		req.Timeout = 20 * time.Second
		finite, err := operations.Invoke(ctx, req)
		if err != nil || !reflect.DeepEqual(finite, want) {
			t.Fatalf("finite local absence: %+v (%v), want %+v", finite, err, want)
		}
		req.Timeout = 0
		bound, err := operations.InvokeCallBound(boundCtx, req)
		if err != nil || !reflect.DeepEqual(bound, want) {
			t.Fatalf("call-bound local absence: %+v (%v), want %+v", bound, err, want)
		}
		factory.owner.mu.Lock()
		defer factory.owner.mu.Unlock()
		if factory.owner.executed != count || factory.owner.sends != count {
			t.Fatalf("local absence changed executions/sends: %d/%d, want %d/%d", factory.owner.executed, factory.owner.sends, count, count)
		}
	}
	// Keep the valid semantic route but remove the owner registration. A
	// foreign registration must not fill that exact local absence.
	owner.unregister()
	if _, err := consumer.session.RegisterExecutor(owner.route, owner); err == nil {
		t.Fatal("a foreign instance registered the owner's route")
	}
	missing := driver.OperationResult{Status: call.StatusFailed, KernelOutcome: operation.OutcomeRejected, Delivery: operation.DeliveryNotSent, Reason: call.ReasonExecutorMissing}
	assertLocalAbsence(*requestFor(t, ctx, site, facts, targetCoefficient), missing, 2)
	owner.unregister, err = owner.session.RegisterExecutor(owner.route, owner)
	if err != nil {
		t.Fatal(err)
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
	for _, invoke := range []struct {
		fn      func(context.Context, driver.OperationRequest) (driver.OperationResult, error)
		ctx     context.Context
		timeout time.Duration
	}{{operations.Invoke, ctx, 20 * time.Second}, {operations.InvokeCallBound, boundCtx, 0}} {
		next := requestFor(t, ctx, site, facts, targetCoefficient)
		next.Timeout = invoke.timeout
		result, err = invoke.fn(invoke.ctx, *next)
		if err != nil || result.KernelOutcome != operation.OutcomeAcknowledgedUnverified {
			t.Fatalf("the successor was disabled by the old unregister: %+v %v", result, err)
		}
	}
	assertLocalAbsence(*finite, driver.OperationResult{Status: call.StatusFailed, KernelOutcome: operation.OutcomeRejected, Delivery: operation.DeliveryNotSent, Reason: call.ReasonNoRoute}, 4)

}

const childEnv = "SEMREG_HOST_TEST_CHILD"

// previousSetting is deliberately distinct from the next generation's first value.
const previousSetting = "28715"

type transitionView struct {
	Kind, Phase, Epoch, Generation, Sequence string
	DesiredRevision                          string `json:"desired_revision"`
	NativeClosePending                       bool   `json:"native_close_pending"`
	SemanticCleanupPending                   bool   `json:"semantic_cleanup_pending"`
	AttemptsUsed                             int    `json:"attempts_used"`
}

type childPublication struct {
	Binding    v1.NativeBinding
	Transition transitionView
}

type controlRecord struct {
	Instances map[string]struct {
		Current *transitionView
	} `json:"instances"`
	Recovery struct{ Payload []byte } `json:"recovery"`
}

func readControl(t *testing.T, path string) controlRecord {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var store controlRecord
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatal(err)
	}
	return store
}

func sameCorrelation(a, b transitionView) bool {
	return a.Epoch == b.Epoch && a.Generation == b.Generation && a.Sequence == b.Sequence && a.DesiredRevision == b.DesiredRevision && a.Kind == b.Kind
}

func waitForHost(t *testing.T, description string, ready func() bool) {
	t.Helper()
	for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ready() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", description)
}

// TestHostChildProcess is the synthetic child host of the process-loss tests.
func TestHostChildProcess(t *testing.T) {
	dir := os.Getenv(childEnv)
	if dir == "" {
		t.Skip("requires the parent recovery test")
	}
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	factory := newSynthFactory(clock)
	factory.owner.value = previousSetting
	factory.hangStop = true
	factory.nativeHandle = os.NewFile(3, "inert-native-handle")
	cfg := baseConfig(t, clock)
	cfg.StateDir = dir
	cfg.Instances = []InstanceConfig{cfg.Instances[0], cfg.Instances[4]}
	site := openStarted(t, cfg, factory)
	ref := life.Ref{Driver: kindOwner, Instance: "owner.one"}
	status, err := site.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	value, binding := recoverySetting(t, ctx, site)
	if value != previousSetting || binding.SourceEpochID != v1.SourceEpochID(status.Epoch) || binding.DriverGeneration != v1.Uint64(fmt.Sprint(status.Generation)) {
		t.Fatalf("child did not publish the accepted generation: %q %+v %+v", value, binding, status)
	}
	method := life.Method(os.Getenv(childEnv + "_METHOD"))
	if _, err := site.SetDesired(ctx, ref, method, status.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(dir, "lifecycle", "runtime-control.json")
	var record *transitionView
	waitForHost(t, "child to commit both cleanup obligations", func() bool {
		record = readControl(t, control).Instances[string(ref.ID())].Current
		return record != nil && record.Phase == "ACTIVE" && record.Epoch == string(binding.SourceEpochID) &&
			record.Generation == string(binding.DriverGeneration) && record.NativeClosePending && record.SemanticCleanupPending && record.AttemptsUsed > 0
	})
	writeJSON(t, filepath.Join(dir, "child.ready"), childPublication{Binding: binding, Transition: *record})
	// The parent terminates this host while the committed transition remains active.
	time.Sleep(time.Minute)
}

func recoverySetting(t *testing.T, ctx context.Context, site *Site) (string, v1.NativeBinding) {
	t.Helper()
	cut, err := site.Tree().SemanticInput(driver.Filter{}).Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return zoneFactCoefficient(t, cut)
}

func TestHostResumesManagedPublicationAfterProcessLoss(t *testing.T) {
	for _, method := range []life.Method{life.MethodStop, life.MethodRestart} {
		t.Run(string(method), func(t *testing.T) { exerciseHostRecovery(t, method, false) })
	}
}

func TestHostRecoveredCleanupFailureKeepsNativeProofAndRetries(t *testing.T) {
	for _, method := range []life.Method{life.MethodStop, life.MethodRestart} {
		t.Run(string(method), func(t *testing.T) { exerciseHostRecovery(t, method, true) })
	}
}

func exerciseHostRecovery(t *testing.T, method life.Method, failSemantic bool) {
	t.Helper()
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	cfg := baseConfig(t, clock)
	cfg.Instances = []InstanceConfig{cfg.Instances[0], cfg.Instances[4]}
	ref := life.Ref{Driver: kindOwner, Instance: "owner.one"}
	peer := life.Ref{Driver: kindPlain, Instance: "plain.c"}
	id := string(ref.ID())
	control := filepath.Join(cfg.StateDir, "lifecycle", "runtime-control.json")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHostChildProcess$")
	cmd.Env = append(os.Environ(), childEnv+"="+cfg.StateDir, childEnv+"_METHOD="+string(method))
	cmd.ExtraFiles = []*os.File{writer}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Log(output.String())
		}
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var old childPublication
	waitForHost(t, "child publication and active transition", func() bool {
		raw, err := os.ReadFile(filepath.Join(cfg.StateDir, "child.ready"))
		return err == nil && json.Unmarshal(raw, &old) == nil && old.Binding.BindingID != ""
	})
	committed, err := os.ReadFile(control)
	if err != nil {
		t.Fatal(err)
	}
	var readBuffer [1]byte
	if err := reader.SetReadDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if n, err := reader.Read(readBuffer[:]); n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("native handle not held by child: %d %v", n, err)
	}

	held := newSynthFactory(clock)
	contender, err := Open(ctx, cfg, held)
	if err != nil {
		t.Fatal(err)
	}
	if err := contender.Start(ctx); err == nil {
		t.Fatal("contender admitted with child holding the lock")
	}
	_ = contender.Close(ctx)
	if after, err := os.ReadFile(control); err != nil || !bytes.Equal(after, committed) || len(held.constructedNames()) != 0 {
		t.Fatalf("contender changed the locked installation: %v, constructed %v", err, held.constructedNames())
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if n, err := reader.Read(readBuffer[:]); n != 0 || err != io.EOF {
		t.Fatalf("process loss did not close its handle: %d %v", n, err)
	}

	factory := newSynthFactory(clock)
	site, err := Open(ctx, cfg, factory)
	if err != nil {
		t.Fatal(err)
	}
	defer site.Close(ctx)
	var restored atomic.Bool
	clock.afterReplay = func() {
		// Continuity is consulted after replay and before lifecycle admission.
		value, binding := recoverySetting(t, ctx, site)
		if value != previousSetting || !reflect.DeepEqual(binding, old.Binding) || len(factory.constructedNames()) != 0 {
			t.Fatalf("before admission: value %q binding %+v constructed %v, want %q and %+v", value, binding, factory.constructedNames(), previousSetting, old.Binding)
		}
		restored.Store(true)
	}
	var replacementChecked atomic.Bool
	factory.beforeConstruct = func(plan driver.InstancePlan) error {
		if !restored.Load() {
			return errors.New("construction preceded restore")
		}
		if plan.Driver != kindOwner {
			return nil
		}
		cur := readControl(t, control).Instances[id].Current
		if cur == nil || cur.Epoch == old.Transition.Epoch || cur.Sequence != old.Transition.Sequence {
			return fmt.Errorf("replacement did not start under its new correlation: %+v", cur)
		}
		value, _ := recoverySetting(t, ctx, site)
		if value != "" {
			return fmt.Errorf("replacement admitted before old value was withdrawn: %q", value)
		}
		replacementChecked.Store(true)
		return nil
	}
	var anchorCalls atomic.Int32
	atRetry := make(chan struct{})
	resumeRetry := make(chan struct{})
	// Drain a paused retry before Close on any failing assertion.
	defer func() {
		select {
		case <-resumeRetry:
		default:
			close(resumeRetry)
		}
	}()
	if failSemantic {
		clock.beforeAnchor = func() {
			switch anchorCalls.Add(1) {
			case 1:
				// This is the first publication intent (not a lifecycle charge).
				// Fail its actual durable write once, after the native lock proof.
				for deadline := time.Now().Add(time.Second); ; time.Sleep(time.Millisecond) {
					err := os.Mkdir(control+".tmp", 0o700)
					if err == nil {
						break
					}
					if !errors.Is(err, os.ErrExist) || time.Now().After(deadline) {
						t.Error(err)
						break
					}
				}
			case 2:
				close(atRetry)
				<-resumeRetry
			}
		}
	}
	if err := site.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !restored.Load() {
		t.Fatal("no restoration observed before startup returned")
	}
	if failSemantic {
		select {
		case <-atRetry:
		case <-time.After(12 * time.Second):
			t.Fatal("semantic cleanup did not retry")
		}
		cur := readControl(t, control).Instances[id].Current
		wantPhase := "CLEANUP"
		if method == life.MethodRestart {
			wantPhase = "ACTIVE"
		}
		if cur == nil || !sameCorrelation(*cur, old.Transition) || cur.Phase != wantPhase || cur.NativeClosePending || !cur.SemanticCleanupPending {
			t.Fatalf("native close did not settle independently of failed semantic cleanup: %+v", cur)
		}
		if value, binding := recoverySetting(t, ctx, site); value != previousSetting || binding.BindingID != old.Binding.BindingID {
			t.Fatalf("failed withdrawal changed the restored value: %q %+v", value, binding)
		}
		waitForHost(t, "independent peer during semantic failure", func() bool {
			st, _ := site.Snapshot(peer)
			return st.ObservedState == life.ObservedRunning
		})
		close(resumeRetry)
	}
	waitForHost(t, "original cleanup to settle and peer to run", func() bool {
		st, _ := site.Snapshot(ref)
		ps, _ := site.Snapshot(peer)
		want := life.ObservedStopped
		if method == life.MethodRestart {
			want = life.ObservedRunning
		}
		return st.ObservedState == want && ps.ObservedState == life.ObservedRunning && readControl(t, control).Instances[id].Current == nil
	})
	constructed := factory.constructedNames()
	sort.Strings(constructed)
	wantNames := []string{"synth.plain/plain.c"}
	wantValue := ""
	if method == life.MethodRestart {
		wantNames = []string{"synth.owner/owner.one", "synth.plain/plain.c"}
		wantValue = initialCoefficient
	}
	value, binding := recoverySetting(t, ctx, site)
	if !reflect.DeepEqual(constructed, wantNames) || value != wantValue || binding.SourceEpochID == old.Binding.SourceEpochID || replacementChecked.Load() != (method == life.MethodRestart) {
		t.Fatalf("after settlement: constructed %v; current %q %+v; replacement checked %v", constructed, value, binding, replacementChecked.Load())
	}
	if n := factory.nativeCloses.Load(); n != 0 {
		t.Fatalf("recovery called native Stop %d times in the replacement process", n)
	}
	assertWithdrawalJournal(t, control, id, old)
}

func assertWithdrawalJournal(t *testing.T, path, id string, old childPublication) {
	t.Helper()
	var material struct {
		Owners []struct {
			Instance string
			Records  []struct {
				Applied bool
				Outcome string
				Batch   v1.PublicationBatch
			}
		}
		Obligations []struct {
			Instance                      string
			NativeClosed, SemanticCleaned bool
		}
	}
	if err := json.Unmarshal(readControl(t, path).Recovery.Payload, &material); err != nil {
		t.Fatal(err)
	}
	var facts, retirements int
	var settled bool
	for _, owner := range material.Owners {
		if owner.Instance != id {
			continue
		}
		for _, record := range owner.Records {
			b := record.Batch
			if !record.Applied || record.Outcome != "accepted" || b.SourceEpochID != old.Binding.SourceEpochID || b.DriverGeneration != old.Binding.DriverGeneration {
				continue
			}
			facts += len(b.FactWithdrawals)
			if slices.Contains(b.SourceRetirements, old.Binding.SourceEpochID) {
				retirements++
			}
		}
	}
	for _, proof := range material.Obligations {
		if proof.Instance == id {
			settled = proof.NativeClosed && proof.SemanticCleaned
		}
	}
	if facts != 1 || retirements != 1 || !settled {
		t.Fatalf("old correlation: %d accepted fact withdrawals, %d epoch retirements, obligations settled=%v", facts, retirements, settled)
	}
}
