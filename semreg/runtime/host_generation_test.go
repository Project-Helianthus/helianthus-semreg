package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

const binderKind driver.DriverKind = "driver.binder"

// binderFactory plans one instance of binderKind; its instances publish a native
// binding and declare a source, and derive a fact from an observation when
// derived is set.
type binderFactory struct{ derived bool }

func (f binderFactory) Descriptor() driver.Descriptor { return driver.Descriptor{} }
func (f binderFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	return []driver.InstancePlan{{Driver: binderKind, Instance: driver.InstanceKey(c.Instance)}}, nil
}
func (f binderFactory) Construct(context.Context, driver.InstancePlan) (driver.Instance, error) {
	return &binderInstance{derived: f.derived}, nil
}

type binderInstance struct {
	derived bool
	source  driver.Source
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
