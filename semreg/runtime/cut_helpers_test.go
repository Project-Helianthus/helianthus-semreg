package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

const testKind driver.DriverKind = "driver.test"

type testClock struct {
	mu   sync.Mutex
	now  time.Time
	mono uint64
	fail error
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mono += uint64(d)
	c.mu.Unlock()
}

func (c *testClock) Sample() (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail != nil {
		return 0, c.fail
	}
	return c.mono, nil
}

func (c *testClock) Anchor(sample uint64) ([]byte, error) {
	return []byte(fmt.Sprint(sample)), nil
}

func (c *testClock) Elapsed(anchor []byte, sample uint64) (uint64, error) {
	var base uint64
	if _, err := fmt.Sscan(string(anchor), &base); err != nil {
		return 0, err
	}
	if sample < base {
		return 0, errors.New("backward sample")
	}
	return sample - base, nil
}

type claimSpec struct {
	Provider string `json:"provider"`
	Config   string `json:"config"`
	Domain   string `json:"domain"`
	Slot     string `json:"slot,omitempty"`
}

func transportJSON(specs ...claimSpec) json.RawMessage {
	raw, _ := json.Marshal(specs)
	return raw
}

func (s claimSpec) claim() driver.TransportClaim {
	return driver.TransportClaim{
		Provider: s.Provider, ConfigKey: s.Config, AccessDomain: s.Domain,
		Share: driver.ShareSerialized, LinkNodeSlot: s.Slot,
	}
}

type synthFactory struct{ kind driver.DriverKind }

func (f synthFactory) Descriptor() driver.Descriptor { return driver.Descriptor{} }

func (f synthFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	var specs []claimSpec
	if len(c.Transport) > 0 {
		if err := json.Unmarshal(c.Transport, &specs); err != nil {
			return nil, err
		}
	}
	plan := driver.InstancePlan{Driver: f.kind, Instance: driver.InstanceKey(c.Instance)}
	for _, spec := range specs {
		plan.Transports = append(plan.Transports, spec.claim())
	}
	return []driver.InstancePlan{plan}, nil
}

func (f synthFactory) Construct(context.Context, driver.InstancePlan) (driver.Instance, error) {
	return nil, errors.New("runtime test: construct is not used")
}

func openService(t *testing.T, clock *testClock) *Service {
	t.Helper()
	svc, err := Open(Options{
		Incarnation:       "incarnation.test",
		Clock:             clock,
		EvaluateMonotonic: v1.MonotonicPoint{ClockEpochID: "clock.epoch.test", Nanoseconds: "1000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func installationPlan(t *testing.T) life.PlanSet {
	t.Helper()
	set, err := life.Plan(life.Installation{
		Key:    "installation.test",
		Bridge: "bridge.test",
		Instances: []life.InstanceConfig{
			{Driver: testKind, Instance: "unit.one", Enabled: true, Config: driver.Config{Transport: transportJSON(claimSpec{Provider: "family.serial", Config: "lane.a", Domain: "domain.a"})}},
			{Driver: testKind, Instance: "unit.two", Enabled: true, Config: driver.Config{Transport: transportJSON(claimSpec{Provider: "family.field", Config: "lane.b", Domain: "domain.b"})}},
			{Driver: testKind, Instance: "unit.three", Enabled: false, Config: driver.Config{Transport: transportJSON(claimSpec{Provider: "family.radio", Config: "lane.c", Domain: "domain.c"})}},
		},
	}, synthFactory{kind: testKind})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func digestOf(label string) v1.Digest {
	sum := sha256.Sum256([]byte(label))
	return v1.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func testEvidence(label string) v1.EvidenceRef {
	return v1.EvidenceRef{
		Owner: "owner.test", Kind: "evidence.test", Digest: digestOf(label),
		Contract: "test.contract/v1", Access: v1.EvidenceAccessPublic, Redaction: v1.RedactionNone,
	}
}

var labPack = v1.PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}

type labValidator struct{}

func (labValidator) Pack() v1.PackRef { return labPack }
func (labValidator) Definitions() v1.DefinitionIndex {
	return v1.DefinitionIndex{Pack: labPack, Fields: []v1.DefinitionRef{}, Services: []v1.DefinitionRef{}, Capabilities: []v1.DefinitionRef{}, Operations: []v1.DefinitionRef{}, EffectRules: []v1.DefinitionRef{}}
}
func (labValidator) ValidateFact(v1.FactKey, *v1.Value) error                      { return nil }
func (labValidator) ValidateService(v1.ServiceInstance) error                      { return nil }
func (labValidator) ValidateCapability(v1.CapabilityInstance) error                { return nil }
func (labValidator) ValidateField(v1.DefinitionRef, v1.TypedField) error           { return nil }
func (labValidator) MatchConstraints(v1.CapabilityInstance, []v1.TypedField) error { return nil }
func (labValidator) EvaluatePredicate(v1.FactCandidate, v1.PredicateOp, v1.Value) (bool, error) {
	return true, nil
}

func kelvinQuantity(coefficient string, exponent int32) v1.Quantity {
	return v1.Quantity{Number: v1.Decimal{Coefficient: coefficient, Exponent10: exponent}, Unit: "unit.kelvin"}
}

func dhwKey() v1.FactKey {
	return v1.FactKey{PackID: labPack.ID, PackVersion: labPack.Version, FactID: "thermal.measurement.dhw_temperature", Dimensions: []v1.Dimension{}}
}

func atNS(ns uint64) v1.TimePoint {
	return v1.TimePoint{UnixNanoseconds: v1.Int64(strconv.FormatUint(1000+ns, 10)), ClockID: "clock.utc", UncertaintyNS: "0"}
}

func monoNS(ns uint64) v1.MonotonicPoint {
	return v1.MonotonicPoint{ClockEpochID: "clock.epoch.test", Nanoseconds: v1.Uint64(strconv.FormatUint(ns, 10))}
}

func wallPoint(at time.Time) v1.TimePoint {
	return v1.TimePoint{UnixNanoseconds: v1.Int64(strconv.FormatInt(at.UnixNano(), 10)), ClockID: "clock.utc", UncertaintyNS: "0"}
}

func openPlain(t *testing.T, svc *Service, instance, asset, source, epoch, generation string) *Session {
	t.Helper()
	session, err := svc.OpenSession(SessionSpec{
		Driver: testKind, Instance: driver.InstanceKey(instance),
		Asset: v1.AssetID(asset), Source: v1.SourceID(source), Epoch: v1.SourceEpochID(epoch),
		Generation: v1.Uint64(generation), Validators: []v1.PackValidator{labValidator{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func descriptorOf(spec SessionSpec, evidence string) v1.SourceDescriptor {
	return v1.SourceDescriptor{
		SourceID: spec.Source, SourceEpochID: spec.Epoch,
		ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "release-1",
		RegistryEvidence: testEvidence(evidence), StartedAt: atNS(1), State: v1.SourceCurrent, Revision: "1",
	}
}

func bindingOf(spec SessionSpec, id, evidence string) v1.NativeBinding {
	return v1.NativeBinding{
		BindingID: v1.NativeBindingID(id), AssetID: spec.Asset, SourceID: spec.Source, SourceEpochID: spec.Epoch,
		DriverGeneration: spec.Generation, NativeResource: testEvidence(evidence), State: v1.BindingCurrent, Revision: "1",
	}
}

func linkOf(spec SessionSpec, binding string, evidence string, resource *v1.ResourceID) v1.IdentityLink {
	return v1.IdentityLink{
		AssetID: spec.Asset, BindingID: v1.NativeBindingID(binding), State: v1.LinkQualified,
		Basis: []v1.EvidenceRef{testEvidence(evidence)}, Revision: "1", Resource: resource,
	}
}

func observedFact(spec SessionSpec, id, binding, revision, coefficient string, received time.Time, receipt uint64) v1.FactCandidate {
	epoch, generation := spec.Epoch, spec.Generation
	source := spec.Source
	bind := v1.NativeBindingID(binding)
	value := kelvinQuantity(coefficient, -2)
	return v1.FactCandidate{
		CandidateID: v1.CandidateID(id), Key: dhwKey(), Value: &v1.Value{Kind: v1.ValueQuantity, Quantity: &value},
		Quality:         v1.Quality{Assertion: v1.AssertionObserved, Qualification: v1.QualificationQualified, Promotion: v1.PromotionPromoted, Validity: v1.ValidityGood, Availability: v1.AvailabilityAvailable, Freshness: v1.FreshnessFresh, Reasons: []v1.DefinitionID{}},
		Times:           v1.Times{ReceivedAt: wallPoint(received), ReceiptMonotonic: monoNS(receipt), EvaluatedAt: wallPoint(received), EvaluateMonotonic: monoNS(receipt)},
		FreshnessPolicy: v1.FreshnessPolicy{PolicyID: "policy:a", Version: "1.0.0", FreshForNS: "30", RetainForNS: "300", MaxWallUncertaintyNS: "1"},
		BindingID:       &bind, SourceEpochID: &epoch, DriverGeneration: &generation,
		Origin:   v1.OriginRef{OriginID: v1.OriginID("origin:" + id), Kind: v1.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &bind, Evidence: []v1.EvidenceRef{testEvidence("origin:" + id)}},
		Evidence: []v1.EvidenceRef{testEvidence("native:" + id)},
		Revision: v1.Uint64(revision),
	}
}

func commit(t *testing.T, session *Session, mutate func(*v1.PublicationBatch), stage func(*transaction)) {
	t.Helper()
	if err := commitErr(session, mutate, stage); err != nil {
		t.Fatal(err)
	}
}

func commitErr(session *Session, mutate func(*v1.PublicationBatch), stage func(*transaction)) error {
	batch, err := session.Prepare(mutate)
	if err != nil {
		return err
	}
	tx, err := session.Begin(context.Background())
	if err != nil {
		return err
	}
	concrete := tx.(*transaction)
	if err := concrete.Stage(batch); err != nil {
		_ = concrete.Abort(context.Background())
		return err
	}
	if stage != nil {
		stage(concrete)
	}
	return concrete.Commit(context.Background())
}

func declareFact(subject, producer driver.NodeRef, path string, priority int32, fact v1.FactCandidate) driver.Source {
	return driver.Source{For: driver.Element{Subject: subject, Key: fact.Key}, Producer: producer, PathKey: path, Priority: priority}
}

func publishFact(t *testing.T, session *Session, binding string, source driver.Source, fact v1.FactCandidate) {
	t.Helper()
	commit(t, session, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(session.spec, "registry:"+string(session.spec.Source))}
		b.BindingUpserts = []v1.NativeBinding{bindingOf(session.spec, binding, "native-resource:"+binding)}
		b.IdentityLinkUpserts = []v1.IdentityLink{linkOf(session.spec, binding, "basis:"+binding, nil)}
		b.FactUpserts = []v1.FactCandidate{fact}
	}, func(tx *transaction) {
		if err := tx.DeclareSource(source); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageSample(driver.Sample{From: source, Candidate: fact}); err != nil {
			t.Fatal(err)
		}
	})
}

func nodeOf(svc *Service, instance driver.InstanceKey) driver.NodeRef {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	for _, node := range svc.nodes {
		if node.Key.Driver == testKind && node.Key.Instance == instance {
			return node.Ref
		}
	}
	return driver.NodeRef{}
}

func installSession(svc *Service) *Session {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	for _, session := range svc.sessions {
		if session.spec.Asset == installationAsset {
			return session
		}
	}
	return nil
}

func topologyText(view View) string {
	var lines []string
	for _, node := range view.Nodes {
		lines = append(lines, fmt.Sprintf("node %s %s", node.Ref.ID, node.Kind))
	}
	for _, link := range view.Overlay {
		lines = append(lines, fmt.Sprintf("link %s %s %s", link.Relation, link.From.ID, link.To.ID))
	}
	for _, source := range view.Sources {
		lines = append(lines, fmt.Sprintf("source %s %s %s %d", source.Producer.ID, source.PathKey, source.For.Subject.ID, source.Priority))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func coefficientOf(snap v1.Snapshot, id v1.CandidateID) string {
	for _, envelope := range snap.Facts {
		for i := range envelope.Candidates {
			if envelope.Candidates[i].CandidateID == id && envelope.Candidates[i].Value != nil && envelope.Candidates[i].Value.Quantity != nil {
				return envelope.Candidates[i].Value.Quantity.Number.Coefficient
			}
		}
	}
	for _, record := range snap.Retained {
		if record.Candidate.CandidateID == id && record.Candidate.Value != nil && record.Candidate.Value.Quantity != nil {
			return record.Candidate.Value.Quantity.Number.Coefficient
		}
	}
	return ""
}

func snapOf(snaps []v1.Snapshot, asset v1.AssetID) (v1.Snapshot, bool) {
	for _, snap := range snaps {
		if snap.AssetID == asset {
			return snap, true
		}
	}
	return v1.Snapshot{}, false
}

func requireIdent(t *testing.T, err error, id v1.ErrorID) {
	t.Helper()
	if v1.ErrorIdentifier(err) != id {
		t.Fatalf("error = %v, want %s", err, id)
	}
}

func memberNode(t *testing.T, svc *Service, instance, slot string) driver.NodeRef {
	t.Helper()
	ref, err := svc.Ledger().Node("bridge.test", driver.NodeKey{
		Installation: "installation.test", Driver: testKind, Instance: driver.InstanceKey(instance),
		Kind: "resource.equipment", Anchor: driver.AnchorKey{Slot: slot},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func keepFresh(fact v1.FactCandidate) v1.FactCandidate {
	fact.FreshnessPolicy.FreshForNS = "5000"
	fact.FreshnessPolicy.RetainForNS = "9000"
	fact.Times.ReceiptMonotonic = monoNS(100)
	fact.Times.EvaluateMonotonic = monoNS(100)
	return fact
}

func retitle(fact v1.FactCandidate, factID, candidate string) v1.FactCandidate {
	fact.Key.FactID = v1.DefinitionID(factID)
	fact.CandidateID = v1.CandidateID(candidate)
	fact.Origin.OriginID = v1.OriginID("origin:" + candidate)
	return fact
}

func sortFacts(facts []v1.FactCandidate) []v1.FactCandidate {
	sort.Slice(facts, func(i, j int) bool { return facts[i].CandidateID < facts[j].CandidateID })
	return facts
}

func loadStore(t *testing.T, ids ...life.InstanceID) *life.MemoryStore {
	t.Helper()
	store := life.NewMemoryStore()
	var configs []life.ControlInstance
	for _, id := range ids {
		configs = append(configs, life.ControlInstance{InstanceID: id, Enabled: true})
	}
	if _, err := store.Register(context.Background(), configs); err != nil {
		t.Fatal(err)
	}
	return store
}
