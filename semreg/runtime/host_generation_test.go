package runtime

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/sources"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

const binderKind driver.DriverKind = "driver.binder"

// binderFactory plans one instance of binderKind; its instances publish a native
// binding and declare a source, and derive a fact from an observation when
// derived is set.
type binderFactory struct {
	derived, relation, native bool
	// prefixed adds a second candidate whose id extends the first one's;
	// twoPaths declares a second path for the same element.
	prefixed, twoPaths bool
	// batch, when set, receives the batch the instance handed to Stage.
	batch *v1.PublicationBatch
	// links, when set, stages claims and ends the transaction as it directs.
	links *linkPlan
}

func (f binderFactory) Descriptor() driver.Descriptor { return driver.Descriptor{} }
func (f binderFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	return []driver.InstancePlan{{Driver: binderKind, Instance: driver.InstanceKey(c.Instance)}}, nil
}
func (f binderFactory) Construct(context.Context, driver.InstancePlan) (driver.Instance, error) {
	return &binderInstance{derived: f.derived, relation: f.relation, native: f.native, prefixed: f.prefixed, twoPaths: f.twoPaths, record: f.batch, links: f.links}, nil
}

type binderInstance struct {
	derived            bool
	relation           bool
	native             bool
	prefixed, twoPaths bool
	record             *v1.PublicationBatch
	links              *linkPlan
	source             driver.Source
}

func (i *binderInstance) FenceWithdrawal() {}
func (i *binderInstance) Stop(context.Context) driver.CloseReport {
	return driver.CloseReport{Confirmed: true}
}

func (i *binderInstance) Start(ctx context.Context, s driver.Session) error {
	return i.publish(ctx, s)
}

func (i *binderInstance) publish(ctx context.Context, s driver.Session) error {
	var op v1.OperationMetadata
	meta := thermal.New().(v1.PackMetadataProvider).Metadata()
	for _, candidate := range meta.Operations {
		if candidate.Ref.ID == "thermal.operation.set_temperature" {
			op = candidate
		}
	}
	var dimension v1.DefinitionID
	for _, field := range meta.Fields {
		if field.Ref.ID == op.Argument.ID {
			dimension = field.Dimension.ID
		}
	}
	catalog, err := graph.Embedded()
	if err != nil {
		return err
	}
	kind, _ := catalog.ServiceSubjectKind(op.Service)
	class, _ := catalog.ResourceKindClass(kind)
	text := "resource:zone:binder"
	key := v1.FactKey{PackID: op.Ref.Pack.ID, PackVersion: op.Ref.Pack.Version, FactID: op.Argument.ID,
		Dimensions: []v1.Dimension{{ID: dimension, Value: v1.Value{Kind: v1.ValueText, Text: &text}}}}
	producer := s.Native().(interface{ Root() driver.NodeRef }).Root()
	resourceID := v1.ResourceID(text)
	anchor := v1.ResourceAnchor{Class: class, Basis: v1.AnchorOperatorSlot, Slot: &text, Evidence: []v1.EvidenceRef{testEvidence("anchor")}}
	if class == v1.AnchorComponent || class == v1.AnchorEpisode {
		parent := v1.ResourceID("resource:parent:binder")
		anchor.Parent = &parent
	}
	resource := v1.Resource{ResourceID: resourceID, Kind: kind, Anchor: anchor, State: v1.ResourceCurrent,
		Origin: v1.OriginRef{OriginID: "origin:operator:binder", Kind: v1.OriginOperator, Evidence: []v1.EvidenceRef{testEvidence("origin-resource")}}, Revision: "1"}
	binding := v1.NativeBindingID("binding:b")
	epoch, generation, source := v1.SourceEpochID("placeholder"), v1.Uint64("1"), v1.SourceID("placeholder")
	if i.native {
		resource.Anchor.Basis = v1.AnchorNativeSlot
		resource.Origin = v1.OriginRef{OriginID: "origin:native:binder", Kind: v1.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []v1.EvidenceRef{testEvidence("origin-resource")}}
	}
	now := wallPoint(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	value := kelvinQuantity("29315", -2)
	quality := v1.Quality{Assertion: v1.AssertionObserved, Qualification: v1.QualificationQualified, Promotion: v1.PromotionPromoted, Validity: v1.ValidityGood, Availability: v1.AvailabilityAvailable, Freshness: v1.FreshnessFresh, Reasons: []v1.DefinitionID{}}
	policy := v1.FreshnessPolicy{PolicyID: "policy:fresh", Version: "1.0.0", FreshForNS: "30000000000", RetainForNS: "120000000000", MaxWallUncertaintyNS: "0"}
	observed := v1.FactCandidate{
		CandidateID: "candidate:observed:1", Key: key, Value: &v1.Value{Kind: v1.ValueQuantity, Quantity: &value}, Quality: quality,
		Times: v1.Times{ReceivedAt: now, EvaluatedAt: now}, FreshnessPolicy: policy,
		BindingID: &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
		Origin:   v1.OriginRef{OriginID: "origin:native", Kind: v1.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []v1.EvidenceRef{testEvidence("origin")}},
		Evidence: []v1.EvidenceRef{testEvidence("fact")}, Revision: "1",
	}
	batch := v1.PublicationBatch{
		SourceUpserts: []v1.SourceDescriptor{{ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "1",
			RegistryEvidence: testEvidence("registry"), StartedAt: now, State: v1.SourceCurrent, Revision: "1"}},
		BindingUpserts:      []v1.NativeBinding{{BindingID: binding, NativeResource: testEvidence("native"), State: v1.BindingCurrent, Revision: "1"}},
		ResourceUpserts:     []v1.Resource{resource},
		IdentityLinkUpserts: []v1.IdentityLink{{BindingID: binding, State: v1.LinkQualified, Basis: []v1.EvidenceRef{testEvidence("link")}, Revision: "1", Resource: &resourceID}},
		ObservedAt:          now,
	}
	if i.relation {
		detectorText := "resource:detector:binder"
		detectorID := v1.ResourceID(detectorText)
		detectorKind := v1.DefinitionID("resource.detector")
		detectorClass, _ := catalog.ResourceKindClass(detectorKind)
		detectorAnchor := v1.ResourceAnchor{Class: detectorClass, Basis: v1.AnchorOperatorSlot, Slot: &detectorText, Evidence: []v1.EvidenceRef{testEvidence("anchor-detector")}}
		if detectorClass == v1.AnchorComponent || detectorClass == v1.AnchorEpisode {
			detectorAnchor.Parent = &resourceID
		}
		batch.ResourceUpserts = append([]v1.Resource{{ResourceID: detectorID, Kind: detectorKind, Anchor: detectorAnchor, State: v1.ResourceCurrent,
			Origin: v1.OriginRef{OriginID: "origin:operator:detector", Kind: v1.OriginOperator, Evidence: []v1.EvidenceRef{testEvidence("origin-detector")}}, Revision: "1"}}, batch.ResourceUpserts...)
		batch.RelationUpserts = []v1.RelationInstance{{
			Definition: v1.DefinitionRef{Pack: v1.PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}, ID: "thermal.relation.detector_member", Version: "1.0.0"},
			From:       resourceID, To: detectorID, State: v1.LinkQualified, BasisClass: v1.RelationGateRule, Basis: []v1.EvidenceRef{testEvidence("relation-basis")},
			Origin:   v1.OriginRef{OriginID: "origin:native:relation", Kind: v1.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []v1.EvidenceRef{testEvidence("relation-origin")}},
			Revision: "1",
		}}
	}
	if i.derived {
		derived := observed
		derived.CandidateID = "candidate:derived:1"
		derived.Quality.Assertion = v1.AssertionInferred
		derived.BindingID, derived.SourceEpochID, derived.DriverGeneration = nil, nil, nil
		derived.Origin = v1.OriginRef{OriginID: "origin:derived", Kind: v1.OriginDerived, Evidence: []v1.EvidenceRef{testEvidence("origin:derived")}}
		derived.Evidence = []v1.EvidenceRef{testEvidence("fact:derived")}
		derived.Derivation = &v1.Derivation{Algorithm: "algorithm.total", Version: "1.0.0", Evidence: []v1.EvidenceRef{}, Inputs: []v1.DerivationInput{{
			CandidateID: observed.CandidateID, CandidateRevision: observed.Revision,
			SourcePaths: []v1.SourcePathRef{{BindingID: binding, SourceID: source, SourceEpochID: epoch, DriverGeneration: generation}},
		}}}
		batch.FactUpserts = []v1.FactCandidate{derived, observed}
	} else {
		batch.FactUpserts = []v1.FactCandidate{observed}
	}
	if i.prefixed {
		longer := observed
		longer.CandidateID = "candidate:observed:1-2"
		batch.FactUpserts = append(batch.FactUpserts, longer)
	}
	if i.links != nil && i.links.ending == endReject {
		batch.FactUpserts[0].Key.FactID = "thermal.fact.undeclared"
	}
	if i.record != nil {
		*i.record = batch
	}
	tx, err := s.Publisher().Begin(ctx)
	if err != nil {
		return err
	}
	if err := tx.Stage(batch); err != nil {
		_ = tx.Abort(ctx)
		return err
	}
	i.source = driver.Source{For: driver.Element{Subject: driver.NodeRef{Bridge: producer.Bridge, ID: resourceID}, Key: key}, Producer: producer, PathKey: "configuration", Priority: 10}
	if err := tx.DeclareSource(i.source); err != nil {
		_ = tx.Abort(ctx)
		return err
	}
	if i.twoPaths {
		other := i.source
		other.PathKey = "alternate"
		if err := tx.DeclareSource(other); err != nil {
			_ = tx.Abort(ctx)
			return err
		}
	}
	if i.links != nil {
		return i.links.finish(ctx, s, tx, producer, resourceID, observed.CandidateID)
	}
	return tx.Commit(ctx)
}

func openBinder(t *testing.T, factory binderFactory) (*Runtime, life.Ref) {
	t.Helper()
	ctx := context.Background()
	ref := life.Ref{Driver: binderKind, Instance: "binder.one"}
	rt, err := OpenHost(ctx, life.Installation{
		Key: "installation.test", Bridge: "bridge.test", StateDir: t.TempDir(), Clock: newTestClock(),
		Instances: []life.InstanceConfig{{Driver: ref.Driver, Instance: ref.Instance, Enabled: true, Config: driver.Config{Instance: string(ref.Instance), Enabled: true}}},
	}, HostOptions{}, factory)
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
	return rt, ref
}

func TestHostStampingKeepsADerivedPublicationsExactDependencies(t *testing.T) {
	rt, ref := openBinder(t, binderFactory{derived: true})
	status, err := rt.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedState != life.ObservedRunning {
		t.Fatalf("a valid derived publication was rejected: %s %+v", status.ObservedState, status.Reason)
	}
}

func TestHostStopRetiresBindingsAndStopsDeclaredSourcesWithoutSamples(t *testing.T) {
	ctx := context.Background()
	rt, ref := openBinder(t, binderFactory{})
	status, err := rt.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	spec := rt.specFor(ref, life.LifecycleCorrelation{InstanceID: ref.ID(), Epoch: status.Epoch, Generation: status.Generation})
	currentBindings := func() int {
		rt.svc.mu.Lock()
		kernel := rt.svc.kernels[spec.Asset]
		rt.svc.mu.Unlock()
		current, _, ok := kernel.Current()
		if !ok {
			t.Fatal("the instance published nothing")
		}
		n := 0
		for _, binding := range current.Bindings {
			if binding.State == v1.BindingCurrent {
				n++
			}
		}
		return n
	}
	if currentBindings() != 1 {
		t.Fatal("the committed binding is not current before the stop")
	}
	if _, err := rt.SetDesired(ctx, ref, life.MethodStop, status.DesiredRevision); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if s, _ := rt.Snapshot(ref); s.ObservedState == life.ObservedStopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the instance did not stop")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := currentBindings(); n != 0 {
		t.Fatalf("%d bindings stay current after the generation stopped", n)
	}
	rt.svc.mu.Lock()
	defer rt.svc.mu.Unlock()
	if len(rt.svc.declared) == 0 {
		t.Fatal("the source declaration is gone")
	}
	for key := range rt.svc.declared {
		if !rt.svc.stopped[key] {
			t.Fatalf("declared source %s never received the generation stop", key)
		}
	}
}

func TestHostStampingKeepsNativeRelationOriginsResolvable(t *testing.T) {
	rt, ref := openBinder(t, binderFactory{relation: true})
	status, err := rt.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedState != life.ObservedRunning {
		t.Fatalf("a native relation referring to the batch binding was rejected: %s %+v", status.ObservedState, status.Reason)
	}
	spec := rt.specFor(ref, life.LifecycleCorrelation{InstanceID: ref.ID(), Epoch: status.Epoch, Generation: status.Generation})
	rt.svc.mu.Lock()
	kernel := rt.svc.kernels[spec.Asset]
	rt.svc.mu.Unlock()
	current, _, _ := kernel.Current()
	if len(current.Relations) != 1 {
		t.Fatalf("relations %d, want the one declared", len(current.Relations))
	}
	relation := current.Relations[0]
	if relation.BasisClass != v1.RelationGateRule || relation.From != "resource:zone:binder" || relation.To != "resource:detector:binder" {
		t.Fatalf("the relation key or basis changed: %+v", relation)
	}
	origin := relation.Origin
	if origin.BindingID == nil || *origin.BindingID != v1.NativeBindingID(scopedID(spec, "binding:b")) || origin.SourceID == nil || *origin.SourceID != spec.Source || origin.SourceEpochID == nil || *origin.SourceEpochID != spec.Epoch {
		t.Fatalf("the relation origin does not resolve to the batch binding, source and epoch: %+v", origin)
	}
}

func TestHostCommitAdmitsTheAcceptedCandidateToItsDeclaredSource(t *testing.T) {
	ctx := context.Background()
	rt, _ := openBinder(t, binderFactory{})
	rt.svc.mu.Lock()
	var declared driver.Source
	n := 0
	for _, source := range rt.svc.declared {
		declared = source
		n++
	}
	rt.svc.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d declared sources, want 1", n)
	}
	view, err := rt.svc.CaptureView(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	state := rt.svc.Element(declared.For, view)
	if len(state.States) != 1 || state.States[0].Disposition != sources.DispositionCurrent {
		t.Fatalf("the declared source does not hold the committed fact: %+v", state.States)
	}
	latest := state.States[0].Latest
	if latest == nil || latest.Candidate.Value == nil || latest.Candidate.Value.Quantity == nil || latest.Candidate.Revision != "1" {
		t.Fatalf("the source sample is not the accepted candidate and revision: %+v", latest)
	}
	if valueCoefficient(state.Value) != "29315" {
		t.Fatalf("the source-table value is %+v", state.Value)
	}
}

func TestHostCommitResolvesTheCommittedIdentityCutWithoutACapture(t *testing.T) {
	ctx := context.Background()
	ref := life.Ref{Driver: binderKind, Instance: "binder.one"}
	inst := life.Installation{Key: "installation.test", Bridge: "bridge.test", StateDir: t.TempDir(), Clock: newTestClock(),
		Instances: []life.InstanceConfig{{Driver: ref.Driver, Instance: ref.Instance, Enabled: true, Config: driver.Config{Instance: string(ref.Instance), Enabled: true}}}}
	var open func(context.Context, life.Installation, ...driver.Factory) (*Runtime, error) = Open
	rt, err := open(ctx, inst, binderFactory{})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	// Add two ledger members and their exact statement without a host capture.
	a := memberNode(t, rt.svc, "unit.a", "a")
	b := memberNode(t, rt.svc, "unit.b", "b")
	join := driver.CertifiedJoin{A: a, B: b, Survivor: b, Statement: driver.StatementRef{Revision: "7", Evidence: testEvidence("durable join")}}
	if err := rt.svc.ledger.Certify(join); err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitInitial(ctx); err != nil {
		t.Fatal(err)
	}
	// Read the commit's resolution *before* any capture can resolve identities.
	rt.svc.mu.Lock()
	resolved, cursor := rt.cuts.resolved, rt.cuts.resolvedAt
	snaps, position, _ := rt.svc.consistent()
	rt.svc.mu.Unlock()
	actual, err := graph.AssembleCut(snaps, rt.svc.catalog, graph.CutComplete)
	if err != nil {
		t.Fatal(err)
	}
	if cursor != rt.svc.cursorAt(position) || resolved.Cut != actual.ID() {
		t.Fatalf("commit resolved %s at %+v, want %s at %d", resolved.Cut, cursor, actual.ID(), position)
	}
	found := false
	for _, resource := range resolved.Resources {
		if resource.Representative != b {
			continue
		}
		found = len(resource.Members) == 2 && ((resource.Members[0].Ref == a && resource.Members[1].Ref == b) || (resource.Members[0].Ref == b && resource.Members[1].Ref == a))
	}
	if !found {
		t.Fatalf("exact pair not resolved to its survivor: %+v", resolved.Resources)
	}
	data, err := os.ReadFile(rt.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint struct {
		Certifications []struct {
			Join    driver.CertifiedJoin
			Revoked bool
		}
	}
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if len(checkpoint.Certifications) != 1 || checkpoint.Certifications[0].Join != join || checkpoint.Certifications[0].Revoked {
		t.Fatalf("statement not in the same checkpoint: %+v", checkpoint)
	}
}

func TestHostCheckpointFailureAfterCommitIsNotAFailedPublication(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	ref := life.Ref{Driver: binderKind, Instance: "binder.one"}
	rt, err := OpenHost(ctx, life.Installation{
		Key: "installation.test", Bridge: "bridge.test", StateDir: dir, Clock: newTestClock(),
		Instances: []life.InstanceConfig{{Driver: ref.Driver, Instance: ref.Instance, Enabled: true, Config: driver.Config{Instance: string(ref.Instance), Enabled: true}}},
	}, HostOptions{}, binderFactory{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(ctx) })
	// The temporary checkpoint path is a directory, so every checkpoint write fails.
	if err := os.MkdirAll(rt.statePath+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := rt.Snapshot(ref)
	deadline := time.Now().Add(10 * time.Second)
	for err == nil && status.ObservedState != life.ObservedRunning && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		status, err = rt.Snapshot(ref)
	}
	if err != nil || status.ObservedState != life.ObservedRunning {
		t.Fatalf("a committed publication was reported as failed because the checkpoint write failed: %v %s %+v", err, status.ObservedState, status.Reason)
	}
	if saveErr := rt.saveCheckpoint(); saveErr == nil {
		t.Fatal("the checkpoint write was expected to fail")
	}
	// The failure of the commit's own checkpoint write stays observable, and a
	// later write exports the whole identity state once the path is writable.
	if rt.CheckpointErr() == nil {
		t.Fatal("the failed checkpoint write is not reported")
	}
	if err := os.Remove(rt.statePath + ".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := rt.saveCheckpoint(); err != nil {
		t.Fatalf("the checkpoint write still fails: %v", err)
	}
	if rt.CheckpointErr() != nil {
		t.Fatal("a successful checkpoint write left the failure reported")
	}
	if _, err := os.Stat(rt.statePath); err != nil {
		t.Fatalf("the identity checkpoint was not persisted: %v", err)
	}
}

func TestHostStampingResolvesNativeResourceOriginsToTheBatchBinding(t *testing.T) {
	rt, ref := openBinder(t, binderFactory{native: true})
	status, err := rt.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedState != life.ObservedRunning {
		t.Fatalf("a native-slot resource with its binding in one batch was rejected: %s %+v", status.ObservedState, status.Reason)
	}
	spec := rt.specFor(ref, life.LifecycleCorrelation{InstanceID: ref.ID(), Epoch: status.Epoch, Generation: status.Generation})
	rt.svc.mu.Lock()
	kernel := rt.svc.kernels[spec.Asset]
	rt.svc.mu.Unlock()
	current, _, _ := kernel.Current()
	if len(current.Resources) != 1 {
		t.Fatalf("resources %d, want the one declared", len(current.Resources))
	}
	origin := current.Resources[0].Origin
	if origin.BindingID == nil || *origin.BindingID != v1.NativeBindingID(scopedID(spec, "binding:b")) || origin.SourceID == nil || *origin.SourceID != spec.Source || origin.SourceEpochID == nil || *origin.SourceEpochID != spec.Epoch {
		t.Fatalf("the resource origin does not resolve to the batch binding, source and epoch: %+v", origin)
	}
	if origin.OriginID != "origin:native:binder" {
		t.Fatalf("the declared origin evidence changed: %+v", origin)
	}
}

func TestHostStagingLeavesTheDriversBatchAsDeclared(t *testing.T) {
	var batch v1.PublicationBatch
	openBinder(t, binderFactory{derived: true, relation: true, native: true, batch: &batch})
	if got := batch.BindingUpserts[0].BindingID; got != "binding:b" {
		t.Fatalf("the driver's binding was rewritten to %q", got)
	}
	if got := batch.IdentityLinkUpserts[0].BindingID; got != "binding:b" {
		t.Fatalf("the driver's identity link was rewritten to %q", got)
	}
	for _, f := range batch.FactUpserts {
		if f.CandidateID != "candidate:derived:1" && f.CandidateID != "candidate:observed:1" {
			t.Fatalf("the driver's candidate was rewritten to %q", f.CandidateID)
		}
		if f.Derivation != nil {
			path := f.Derivation.Inputs[0].SourcePaths[0]
			if f.Derivation.Inputs[0].CandidateID != "candidate:observed:1" || path.BindingID != "binding:b" || path.SourceID != "placeholder" {
				t.Fatalf("the driver's derivation was rewritten: %+v", f.Derivation)
			}
		}
	}
	for _, r := range batch.ResourceUpserts {
		if r.Origin.BindingID != nil && *r.Origin.BindingID != "binding:b" || r.Origin.SourceID != nil && *r.Origin.SourceID != "placeholder" {
			t.Fatalf("the driver's resource origin was rewritten: %+v", r.Origin)
		}
	}
	for _, r := range batch.RelationUpserts {
		if *r.Origin.BindingID != "binding:b" || *r.Origin.SourceID != "placeholder" {
			t.Fatalf("the driver's relation origin was rewritten: %+v", r.Origin)
		}
	}
}

func TestHostStampingKeepsCanonicalOrderWhenOneIdentifierIsAPrefixOfAnother(t *testing.T) {
	rt, ref := openBinder(t, binderFactory{prefixed: true})
	status, err := rt.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	if status.ObservedState != life.ObservedRunning {
		t.Fatalf("a canonically ordered batch was rejected after stamping: %s %+v", status.ObservedState, status.Reason)
	}
}

// pathStates returns, per declared path key, the sample the source holds.
func pathStates(t *testing.T, rt *Runtime) map[string]bool {
	t.Helper()
	rt.svc.mu.Lock()
	var declared driver.Source
	for _, source := range rt.svc.declared {
		declared = source
	}
	rt.svc.mu.Unlock()
	view, err := rt.svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]bool{}
	for _, state := range rt.svc.Element(declared.For, view).States {
		held[state.Source.PathKey] = state.Latest != nil
	}
	return held
}

func TestHostDoesNotAssignACandidateToPathsThatDidNotSupplyIt(t *testing.T) {
	rt, ref := openBinder(t, binderFactory{twoPaths: true})
	if status, err := rt.Snapshot(ref); err != nil || status.ObservedState != life.ObservedRunning {
		t.Fatalf("%v %s %+v", err, status.ObservedState, status.Reason)
	}
	held := pathStates(t, rt)
	if len(held) != 2 {
		t.Fatalf("declared paths %v, want two", held)
	}
	for path, has := range held {
		if has {
			t.Fatalf("path %q holds a sample although the driver never said it supplied one", path)
		}
	}
}

// A withdrawal retires the epoch its correlation published under. The retry of
// the same correlation must publish under a fresh epoch, and a later withdrawal
// must reach what the retry published.
func TestHostRetryOfAWithdrawnCorrelationPublishesUnderAFreshEpoch(t *testing.T) {
	ctx := context.Background()
	rt, ref := openBinder(t, binderFactory{})
	status, err := rt.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	corr := life.LifecycleCorrelation{InstanceID: ref.ID(), Epoch: status.Epoch, Generation: status.Generation}
	spec := rt.specFor(ref, corr)
	current := func() v1.Snapshot {
		rt.svc.mu.Lock()
		kernel := rt.svc.kernels[spec.Asset]
		rt.svc.mu.Unlock()
		snap, _, ok := kernel.Current()
		if !ok {
			t.Fatal("the instance published nothing")
		}
		return snap
	}
	currentBindings := func() (n int) {
		for _, binding := range current().Bindings {
			if binding.State == v1.BindingCurrent {
				n++
			}
		}
		return n
	}
	if err := rt.pub.Withdraw(ctx, corr); err != nil {
		t.Fatal(err)
	}
	if n := currentBindings(); n != 0 {
		t.Fatalf("%d bindings stay current after the withdrawal", n)
	}
	session, err := rt.pub.OpenSession(ctx, corr)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&binderInstance{}).publish(ctx, session); err != nil {
		t.Fatalf("the retry of the withdrawn correlation could not publish: %v", err)
	}
	if n := currentBindings(); n != 1 {
		t.Fatalf("%d current bindings after the retry published, want 1", n)
	}
	for _, binding := range current().Bindings {
		if binding.State == v1.BindingCurrent && binding.SourceEpochID == spec.Epoch {
			t.Fatal("the retry revived the failed attempt's epoch")
		}
	}
	if err := rt.pub.Withdraw(ctx, corr); err != nil {
		t.Fatal(err)
	}
	if n := currentBindings(); n != 0 {
		t.Fatalf("%d bindings the retry published stay current after its withdrawal", n)
	}
}

// A declaration a recovery replay restored lives in the source table and the
// declaration map only: its replay session is not registered and no sample ever
// named its asset. The withdrawal of its generation must still stop it.
func TestHostWithdrawalStopsARecoveredDeclarationThatNeverHeldASample(t *testing.T) {
	ctx := context.Background()
	rt, ref := openBinder(t, binderFactory{twoPaths: true})
	status, err := rt.Snapshot(ref)
	if err != nil {
		t.Fatal(err)
	}
	rt.svc.mu.Lock()
	rt.svc.sessions = nil
	declared := len(rt.svc.declared)
	if declared != 2 || len(rt.svc.assetOf) != 0 {
		rt.svc.mu.Unlock()
		t.Fatalf("%d declarations and %d sampled sources, want two declarations and no sample", declared, len(rt.svc.assetOf))
	}
	rt.svc.mu.Unlock()
	corr := life.LifecycleCorrelation{InstanceID: ref.ID(), Epoch: status.Epoch, Generation: status.Generation}
	if err := rt.pub.Withdraw(ctx, corr); err != nil {
		t.Fatal(err)
	}
	rt.svc.mu.Lock()
	defer rt.svc.mu.Unlock()
	for key := range rt.svc.declared {
		if !rt.svc.stopped[key] {
			t.Fatalf("declared source %s never received the generation stop", key)
		}
	}
}
