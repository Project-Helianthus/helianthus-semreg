package sources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

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

const kelvin v1.DefinitionID = "unit.kelvin"

func quantity(coefficient string, exponent int32) v1.Quantity {
	return v1.Quantity{Number: v1.Decimal{Coefficient: coefficient, Exponent10: exponent}, Unit: kelvin}
}

func dhwKey() v1.FactKey {
	return v1.FactKey{PackID: labPack.ID, PackVersion: labPack.Version, FactID: "thermal.measurement.dhw_temperature", Dimensions: []v1.Dimension{}}
}

func dhwElement() driver.Element {
	return driver.Element{Subject: driver.NodeRef{Bridge: "bridge:lab", ID: "resource:dhw"}, Key: dhwKey()}
}

func digestOf(label string) v1.Digest {
	sum := sha256.Sum256([]byte(label))
	return v1.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func evidence(label string) v1.EvidenceRef {
	return v1.EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: digestOf(label), Contract: "test.contract/v1", Access: v1.EvidenceAccessPublic, Redaction: v1.RedactionNone}
}

const clockEpoch v1.ClockEpochID = "clock:a"

type lab struct {
	t       *testing.T
	table   *Table
	now     uint64
	tick    uint64
	frozen  bool
	epoch   v1.ClockEpochID
	kernels map[v1.AssetID]*v1.PublicationKernel
	drivers []*drv
	el      *driver.Element
}

func (l *lab) elem() driver.Element {
	if l.el != nil {
		return *l.el
	}
	return dhwElement()
}

func newLab(t *testing.T) *lab {
	t.Helper()
	l := &lab{t: t, kernels: map[v1.AssetID]*v1.PublicationKernel{}}
	table, err := NewTable(func() v1.MonotonicPoint {
		if !l.frozen {
			l.tick++
		}
		epoch := l.epoch
		if epoch == "" {
			epoch = "clock:table"
		}
		return v1.MonotonicPoint{ClockEpochID: epoch, Nanoseconds: v1.Uint64(strconv.FormatUint(l.tick, 10))}
	})
	if err != nil {
		t.Fatal(err)
	}
	l.table = table
	return l
}

func (l *lab) kernel(asset v1.AssetID) *v1.PublicationKernel {
	if k, ok := l.kernels[asset]; ok {
		return k
	}
	k, err := v1.NewPublicationKernel(asset, labValidator{})
	if err != nil {
		l.t.Fatal(err)
	}
	l.kernels[asset] = k
	return k
}

type owned struct {
	id       v1.CandidateID
	revision v1.Uint64
}

type drv struct {
	l          *lab
	name       string
	asset      v1.AssetID
	generation uint64
	seq        uint64
	started    bool
	source     driver.Source
	own        *owned
}

func (l *lab) addDriver(name string, asset v1.AssetID, priority int32, step *v1.Quantity) *drv {
	l.t.Helper()
	d := &drv{l: l, name: name, asset: asset, generation: 1}
	d.source = driver.Source{For: l.elem(), Producer: driver.NodeRef{Bridge: "bridge:lab", ID: v1.ResourceID("producer:" + name)}, PathKey: "path:" + name, Priority: priority, Step: step}
	if err := l.table.Declare(d.source); err != nil {
		l.t.Fatal(err)
	}
	l.drivers = append(l.drivers, d)
	return d
}

func (d *drv) ref() driver.SourceRef { return refOf(d.source) }

func (d *drv) sourceID() v1.SourceID   { return v1.SourceID("source:" + d.name) }
func (d *drv) epoch() v1.SourceEpochID { return v1.SourceEpochID("epoch:" + d.name) }
func (d *drv) binding() v1.NativeBindingID {
	return v1.NativeBindingID(fmt.Sprintf("binding:%s:g%d", d.name, d.generation))
}
func (d *drv) gen() v1.Uint64 { return v1.Uint64(strconv.FormatUint(d.generation, 10)) }
func at(ns uint64) v1.TimePoint {
	return v1.TimePoint{UnixNanoseconds: v1.Int64(strconv.FormatUint(1000+ns, 10)), ClockID: "clock.utc", UncertaintyNS: "0"}
}
func mono(ns uint64) v1.MonotonicPoint {
	return v1.MonotonicPoint{ClockEpochID: clockEpoch, Nanoseconds: v1.Uint64(strconv.FormatUint(ns, 10))}
}
func (l *lab) context() v1.EvaluationContext {
	return v1.EvaluationContext{EvaluatedAt: at(l.now), EvaluateMonotonic: mono(l.now)}
}

func (d *drv) times(receipt uint64) v1.Times {
	return v1.Times{ReceivedAt: at(receipt), ReceiptMonotonic: mono(receipt), EvaluatedAt: at(receipt), EvaluateMonotonic: mono(receipt)}
}

// obs builds an observation candidate of the winner-element fact with the
// default freshness policy: fresh for 30 ns, retained for 300 ns.
func (d *drv) obs(id string, value v1.Quantity, revision string, receipt uint64, acquisitionLabel string) v1.FactCandidate {
	return d.obsValue(id, &v1.Value{Kind: v1.ValueQuantity, Quantity: &value}, revision, receipt, acquisitionLabel)
}

func (d *drv) obsValue(id string, value *v1.Value, revision string, receipt uint64, acquisitionLabel string) v1.FactCandidate {
	binding, epoch, generation := d.binding(), d.epoch(), d.gen()
	sourceID := d.sourceID()
	return v1.FactCandidate{
		CandidateID: v1.CandidateID(id), Key: d.source.For.Key, Value: value,
		Quality:         v1.Quality{Assertion: v1.AssertionObserved, Qualification: v1.QualificationQualified, Promotion: v1.PromotionPromoted, Validity: v1.ValidityGood, Availability: v1.AvailabilityAvailable, Freshness: v1.FreshnessFresh, Reasons: []v1.DefinitionID{}},
		Times:           d.times(receipt),
		FreshnessPolicy: v1.FreshnessPolicy{PolicyID: "policy:a", Version: "1.0.0", FreshForNS: "30", RetainForNS: "300", MaxWallUncertaintyNS: "1"},
		BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
		Origin:   v1.OriginRef{OriginID: v1.OriginID("origin:" + id), Kind: v1.OriginNativeObservation, SourceID: &sourceID, SourceEpochID: &epoch, BindingID: &binding, Evidence: []v1.EvidenceRef{evidence("origin:" + acquisitionLabel)}},
		Evidence: []v1.EvidenceRef{evidence("native:" + acquisitionLabel)},
		Revision: v1.Uint64(revision),
	}
}

func (d *drv) batch(mutate func(*v1.PublicationBatch)) v1.PublicationBatch {
	d.seq++
	k := d.l.kernel(d.asset)
	expected := v1.Uint64("0")
	if current, _, ok := k.Current(); ok {
		expected = current.Revisions.Semantic
	}
	seq := v1.Uint64(strconv.FormatUint(d.seq, 10))
	b := v1.PublicationBatch{
		Contract: v1.ContractKernelV1, BatchID: v1.BatchID(fmt.Sprintf("batch:%s:%d:%d", d.name, d.generation, d.seq)),
		AssetID: d.asset, SourceID: d.sourceID(), SourceEpochID: d.epoch(), DriverGeneration: d.gen(), Sequence: seq,
		ExpectedSemanticRevision: expected, ObservedAt: v1.TimePoint{UnixNanoseconds: v1.Int64(strconv.FormatUint(2000+d.seq, 10)), ClockID: "clock.utc", UncertaintyNS: "0"},
		SourceUpserts: []v1.SourceDescriptor{}, SourceRetirements: []v1.SourceEpochID{}, BindingUpserts: []v1.NativeBinding{}, IdentityLinkUpserts: []v1.IdentityLink{},
		FactUpserts: []v1.FactCandidate{}, FactWithdrawals: []v1.CandidateID{}, ServiceUpserts: []v1.ServiceInstance{}, ServiceWithdrawals: []v1.ServiceInstanceID{},
		CapabilityUpserts: []v1.CapabilityInstance{}, CapabilityWithdrawals: []v1.CapabilityInstanceID{}, GenerationFences: []v1.GenerationFence{},
	}
	if !d.started {
		d.started = true
		b.SourceUpserts = []v1.SourceDescriptor{d.descriptor()}
		b.BindingUpserts = []v1.NativeBinding{d.nativeBinding()}
		b.IdentityLinkUpserts = []v1.IdentityLink{d.link()}
	}
	if mutate != nil {
		mutate(&b)
	}
	digest, err := b.ComputedDigest()
	if err != nil {
		d.l.t.Fatal(err)
	}
	b.BatchDigest = digest
	return b
}

func (d *drv) descriptor() v1.SourceDescriptor {
	return v1.SourceDescriptor{SourceID: d.sourceID(), SourceEpochID: d.epoch(), ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "release-1", RegistryEvidence: evidence("registry"), StartedAt: at(1), State: v1.SourceCurrent, Revision: "1"}
}

func (d *drv) nativeBinding() v1.NativeBinding {
	return v1.NativeBinding{BindingID: d.binding(), AssetID: d.asset, SourceID: d.sourceID(), SourceEpochID: d.epoch(), DriverGeneration: d.gen(), NativeResource: evidence("resource:" + d.name), State: v1.BindingCurrent, Revision: "1"}
}

func (d *drv) link() v1.IdentityLink {
	return v1.IdentityLink{AssetID: d.asset, BindingID: d.binding(), State: v1.LinkQualified, Basis: []v1.EvidenceRef{evidence("basis")}, Revision: "1"}
}

// apply commits a batch to the asset's kernel and, on acceptance, applies the
// source-table inputs a publication commit owes it.
func (d *drv) apply(b v1.PublicationBatch) (v1.Snapshot, error) {
	d.l.t.Helper()
	k := d.l.kernel(d.asset)
	before, _, _ := k.Current()
	after, _, err := k.Apply(b, mono(d.l.now))
	if err != nil {
		return v1.Snapshot{}, err
	}
	for _, c := range b.FactUpserts {
		if canonicalKey(c.Key) != canonicalKey(d.source.For.Key) {
			continue
		}
		if err := d.l.table.Admit(driver.Sample{From: d.source, Candidate: c}, after); err != nil {
			d.l.t.Fatalf("admit %s: %v", c.CandidateID, err)
		}
		d.own = &owned{id: c.CandidateID, revision: c.Revision}
	}
	explicit := map[v1.CandidateID]bool{}
	for _, id := range b.FactWithdrawals {
		explicit[id] = true
		if d.own != nil && d.own.id == id {
			d.l.table.WithdrawFact(d.ref(), d.asset, id, d.own.revision, ExplicitWithdrawal)
		}
	}
	for _, owner := range d.l.drivers {
		if owner.asset != d.asset || owner.own == nil || explicit[owner.own.id] {
			continue
		}
		if c := candidate(before, owner.own.id); c != nil && c.Quality.Assertion == v1.AssertionInferred && candidate(after, owner.own.id) == nil {
			d.l.table.WithdrawFact(owner.ref(), owner.asset, owner.own.id, owner.own.revision, DependencyClosure)
		}
	}
	return after, nil
}

// supersede starts generation n+1: a new binding, with the previous generation
// fenced in the same commit, and tells the table the old generation stopped.
func (d *drv) supersede() v1.Snapshot {
	d.l.t.Helper()
	old := d.generation
	d.generation++
	d.seq = 0
	d.started = true
	b := d.batch(func(b *v1.PublicationBatch) {
		b.BindingUpserts = []v1.NativeBinding{d.nativeBinding()}
		b.IdentityLinkUpserts = []v1.IdentityLink{d.link()}
		b.GenerationFences = []v1.GenerationFence{{SourceID: d.sourceID(), SourceEpochID: d.epoch(), DriverGeneration: v1.Uint64(strconv.FormatUint(old, 10)), Reason: "lifecycle.driver_replaced", Evidence: []v1.EvidenceRef{evidence("fence")}, Revision: "1"}}
	})
	snapshot, err := d.apply(b)
	if err != nil {
		d.l.t.Fatalf("supersede: %v", err)
	}
	if !d.l.table.GenerationStopped(d.ref()) {
		d.l.t.Fatal("source not declared")
	}
	return snapshot
}

func candidate(s v1.Snapshot, id v1.CandidateID) *v1.FactCandidate {
	for _, envelope := range s.Facts {
		for i := range envelope.Candidates {
			if envelope.Candidates[i].CandidateID == id {
				c := envelope.Candidates[i]
				return &c
			}
		}
	}
	return nil
}

// publish commits one observation upsert.
func (d *drv) publish(c v1.FactCandidate) v1.Snapshot {
	d.l.t.Helper()
	snapshot, err := d.apply(d.batch(func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{c} }))
	if err != nil {
		d.l.t.Fatalf("publish %s: %v", c.CandidateID, err)
	}
	return snapshot
}

func (d *drv) withdraw(id v1.CandidateID) v1.Snapshot {
	d.l.t.Helper()
	snapshot, err := d.apply(d.batch(func(b *v1.PublicationBatch) { b.FactWithdrawals = []v1.CandidateID{id} }))
	if err != nil {
		d.l.t.Fatalf("withdraw %s: %v", id, err)
	}
	return snapshot
}

// stop fences the generation, as the runtime does when a generation stops, and
// tells the table.
func (d *drv) stop() v1.Snapshot {
	d.l.t.Helper()
	snapshot, err := d.apply(d.batch(func(b *v1.PublicationBatch) {
		b.GenerationFences = []v1.GenerationFence{{SourceID: d.sourceID(), SourceEpochID: d.epoch(), DriverGeneration: d.gen(), Reason: "lifecycle.driver_replaced", Evidence: []v1.EvidenceRef{evidence("fence")}, Revision: "1"}}
	}))
	if err != nil {
		d.l.t.Fatalf("fence: %v", err)
	}
	if !d.l.table.GenerationStopped(d.ref()) {
		d.l.t.Fatal("source not declared")
	}
	return snapshot
}

// view evaluates the asset's current snapshot at the lab time.
func (l *lab) view(asset v1.AssetID) MemberEvaluation {
	l.t.Helper()
	snapshot, view, _, ok, err := l.kernel(asset).CurrentAt(l.context())
	if err != nil || !ok {
		l.t.Fatalf("evaluate %s: ok=%v err=%v", asset, ok, err)
	}
	return MemberEvaluation{Member: snapshot, View: view}
}

func (l *lab) evals() map[SourceKey]MemberEvaluation {
	l.t.Helper()
	out := map[SourceKey]MemberEvaluation{}
	byAsset := map[v1.AssetID]MemberEvaluation{}
	for _, d := range l.drivers {
		if !d.started {
			continue
		}
		if _, ok := byAsset[d.asset]; !ok {
			byAsset[d.asset] = l.view(d.asset)
		}
		out[KeyOf(d.ref())] = byAsset[d.asset]
	}
	return out
}

func (l *lab) element() ElementState { l.t.Helper(); return l.table.Element(l.elem(), l.evals()) }

func (s ElementState) state(d *drv) SourceState {
	for _, st := range s.States {
		if refKey(refOf(st.Source)) == refKey(d.ref()) {
			return st
		}
	}
	panic("no state for " + d.name)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func sortedNames(s ElementState) []string {
	var out []string
	for _, v := range s.Resolution.Sources {
		out = append(out, string(v.Source.Producer.ID))
	}
	return out
}

var _ = sort.Strings
