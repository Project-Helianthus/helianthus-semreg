package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/call"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

func hostSurface() v1.EvidenceRef {
	sum := sha256.Sum256([]byte("surface:host-program"))
	return v1.EvidenceRef{
		Owner: "semreg.host", Kind: "semantic.caller.host", Digest: v1.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Contract: operation.ContractOperationV1, Access: v1.EvidenceAccessPublic, Redaction: v1.RedactionNone,
	}
}

func hostValidators() []v1.PackValidator {
	return []v1.PackValidator{thermal.New(), pv.New(), storage.New(), evse.New(), infrastructure.New()}
}

func instanceDigest(id life.InstanceID) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

// generationKey names one lifecycle generation of one instance.
type generationKey struct {
	id         life.InstanceID
	epoch      life.SourceEpoch
	generation life.DriverGeneration
}

// hostAttempt is one native start attempt inside a lifecycle generation.
type hostAttempt struct {
	number uint64
	ref    tree.InstanceRef
	scoped *call.Scoped
}

// hostPublication is the life.Publication of the ordinary host: it opens each
// generation's session and withdraws what a generation registered.
type hostPublication struct {
	rt       *Runtime
	mu       sync.Mutex
	attempts uint64
	open     map[generationKey][]*hostAttempt
}

var _ life.Publication = (*hostPublication)(nil)

func (rt *Runtime) specFor(ref life.Ref, corr life.LifecycleCorrelation) SessionSpec {
	tag := instanceDigest(ref.ID())
	return SessionSpec{
		Driver: ref.Driver, Instance: ref.Instance,
		Asset: v1.AssetID("asset:instance:" + tag), Source: v1.SourceID("source:instance:" + tag),
		Epoch:      v1.SourceEpochID(corr.Epoch),
		Generation: v1.Uint64(strconv.FormatUint(uint64(corr.Generation), 10)),
		Validators: hostValidators(), Graph: true,
	}
}

func (p *hostPublication) OpenSession(_ context.Context, corr life.LifecycleCorrelation) (driver.Session, error) {
	planned, ok := p.rt.byID[corr.InstanceID]
	if !ok {
		return nil, fmt.Errorf("runtime: no planned instance for %s", corr.InstanceID)
	}
	spec := p.rt.specFor(planned.Ref, corr)
	svc := p.rt.svc
	svc.mu.Lock()
	delete(svc.fencedGen, fenceKey(spec.Source, spec.Epoch, spec.Generation))
	svc.mu.Unlock()
	session, err := svc.OpenSession(spec)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.attempts++
	attempt := &hostAttempt{number: p.attempts, ref: tree.InstanceRef{Driver: planned.Ref.Driver, Instance: planned.Ref.Instance}}
	p.mu.Unlock()
	native, err := p.rt.tree.Registry.Bind(driver.NamespaceID(planned.Ref.Driver), planned.Ref.Instance, attempt.number)
	if err != nil {
		return nil, err
	}
	scope := call.Scope{Instance: string(corr.InstanceID), Generation: attempt.number, Producer: native.Root()}
	attempt.scoped = p.rt.router.ScopeOf(scope)
	key := generationKey{id: corr.InstanceID, epoch: corr.Epoch, generation: corr.Generation}
	p.mu.Lock()
	if p.open == nil {
		p.open = map[generationKey][]*hostAttempt{}
	}
	p.open[key] = append(p.open[key], attempt)
	p.mu.Unlock()
	return &hostSession{
		native: native, spec: spec, scoped: attempt.scoped, calendar: p.rt.calendar,
		input:     &hostInput{scoped: p.rt.tree.SemanticInput(driver.Filter{}), session: session},
		publisher: &hostPublisher{session: session, rt: p.rt},
	}, nil
}

// Withdraw stops what the correlation's generation registered: its native
// registrations, its executor registrations and the sources it declared. It is
// idempotent and also settles a correlation of an earlier process.
func (p *hostPublication) Withdraw(ctx context.Context, corr life.LifecycleCorrelation) error {
	planned, ok := p.rt.byID[corr.InstanceID]
	if !ok {
		return fmt.Errorf("runtime: no planned instance for %s", corr.InstanceID)
	}
	key := generationKey{id: corr.InstanceID, epoch: corr.Epoch, generation: corr.Generation}
	p.mu.Lock()
	attempts := p.open[key]
	delete(p.open, key)
	p.mu.Unlock()
	for _, attempt := range attempts {
		attempt.scoped.Retire()
		p.rt.lifecycle.fence(tree.Fence{Instance: attempt.ref, Generation: attempt.number})
	}
	spec := p.rt.specFor(planned.Ref, corr)
	if err := p.rt.withdrawCommitted(ctx, spec); err != nil {
		return err
	}
	p.rt.svc.withdrawGeneration(spec.Asset, spec.Source, spec.Epoch, spec.Generation)
	return nil
}

// withdrawCommitted commits, through the managed publication path, the
// withdrawal of everything the asset currently publishes, so the generation
// leaves current reads and a reopened installation restores no value of it. It
// commits nothing when nothing is current.
func (rt *Runtime) withdrawCommitted(ctx context.Context, spec SessionSpec) error {
	if err := rt.commitWithdrawal(ctx, spec, false); err != nil {
		return err
	}
	return rt.commitWithdrawal(ctx, spec, true)
}

// commitWithdrawal commits one withdrawal batch. The first withdraws the facts,
// services and capabilities that are current; the second retires the source
// epoch while the generation still has a current binding, so its effective
// bindings leave with it. A batch that has nothing to withdraw commits nothing.
func (rt *Runtime) commitWithdrawal(ctx context.Context, spec SessionSpec, retireBindings bool) error {
	svc := rt.svc
	svc.mu.Lock()
	kernel := svc.kernels[spec.Asset]
	svc.mu.Unlock()
	if kernel == nil {
		return nil
	}
	current, _, ok := kernel.Current()
	if !ok {
		return nil
	}
	var services []v1.ServiceInstanceID
	var capabilities []v1.CapabilityInstanceID
	var facts []v1.CandidateID
	retire := false
	if retireBindings {
		for _, binding := range current.Bindings {
			if binding.SourceID == spec.Source && binding.SourceEpochID == spec.Epoch && binding.DriverGeneration == spec.Generation && binding.State == v1.BindingCurrent {
				retire = true
			}
		}
		if !retire {
			return nil
		}
	} else {
		for _, service := range current.Services {
			if service.Availability != v1.AvailabilityWithdrawn {
				services = append(services, service.InstanceID)
			}
		}
		for _, capability := range current.Capabilities {
			if capability.Availability != v1.AvailabilityWithdrawn {
				capabilities = append(capabilities, capability.InstanceID)
			}
		}
		for _, envelope := range current.Facts {
			for _, candidate := range envelope.Candidates {
				facts = append(facts, candidate.CandidateID)
			}
		}
		if len(facts) == 0 && len(services) == 0 && len(capabilities) == 0 {
			return nil
		}
	}
	svc.mu.Lock()
	delete(svc.fencedGen, fenceKey(spec.Source, spec.Epoch, spec.Generation))
	svc.mu.Unlock()
	session, err := svc.OpenSession(spec)
	if err != nil {
		return err
	}
	batch, err := session.Prepare(func(b *v1.PublicationBatch) {
		b.FactWithdrawals = append(b.FactWithdrawals, facts...)
		b.ServiceWithdrawals = append(b.ServiceWithdrawals, services...)
		b.CapabilityWithdrawals = append(b.CapabilityWithdrawals, capabilities...)
		b.SourceRetirements = append(b.SourceRetirements, session.priorEpochs()...)
		if retire {
			b.SourceRetirements = append(b.SourceRetirements, spec.Epoch)
		}
	})
	if err != nil {
		return err
	}
	tx, err := session.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tx.Stage(batch); err != nil {
		_ = tx.Abort(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// withdrawGeneration marks the generation fenced, its routes not live and every
// source declared for the asset stopped, so SS06 resolves no value from it.
func (s *Service) withdrawGeneration(asset v1.AssetID, source v1.SourceID, epoch v1.SourceEpochID, generation v1.Uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopGeneration(source, epoch, generation)
	for key, owner := range s.assetOf {
		if owner != asset {
			continue
		}
		declared, ok := s.declared[key]
		if !ok {
			continue
		}
		s.table.GenerationStopped(driver.SourceRef{For: declared.For, Producer: declared.Producer, PathKey: declared.PathKey})
		s.stopped[key] = true
	}
}

// hostSession is the driver.Session of one native start attempt.
type hostSession struct {
	native    driver.NativeRegistry
	input     driver.SemanticInput
	scoped    *call.Scoped
	calendar  driver.Calendar
	publisher driver.Publisher
	spec      SessionSpec
}

var _ driver.Session = (*hostSession)(nil)

func (s *hostSession) Native() driver.NativeRegistry             { return s.native }
func (s *hostSession) SemanticInput() driver.SemanticInput       { return s.input }
func (s *hostSession) ScopedOperations() driver.ScopedOperations { return s.scoped }
func (s *hostSession) Calendar() driver.Calendar                 { return s.calendar }
func (s *hostSession) Publisher() driver.Publisher               { return s.publisher }

func (s *hostSession) RegisterExecutor(route driver.SettingRoute, exec driver.Executor) (func(), error) {
	return s.scoped.RegisterExecutor(stampRoute(s.spec, route), exec)
}

// scopedID makes a driver-chosen object identifier belong to one source epoch,
// so an epoch that replaces a retired one never re-owns an identifier the
// retired epoch's objects hold.
func scopedID(spec SessionSpec, id string) string {
	sum := sha256.Sum256([]byte(spec.Epoch))
	return id + "@" + hex.EncodeToString(sum[:4])
}

func stampRoute(spec SessionSpec, route driver.SettingRoute) driver.SettingRoute {
	route.Route.CapabilityInstance = v1.CapabilityInstanceID(scopedID(spec, string(route.Route.CapabilityInstance)))
	route.Route.ServiceInstance = v1.ServiceInstanceID(scopedID(spec, string(route.Route.ServiceInstance)))
	route.Route.BindingID = v1.NativeBindingID(scopedID(spec, string(route.Route.BindingID)))
	route.Route.SourceID = spec.Source
	route.Route.SourceEpochID = spec.Epoch
	route.Route.DriverGeneration = spec.Generation
	return route
}

// hostPublisher stamps everything a driver stages with the session's ownership.
type hostPublisher struct {
	session *Session
	rt      *Runtime
}

func (p *hostPublisher) Begin(ctx context.Context) (driver.PublicationTransaction, error) {
	tx, err := p.session.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &hostTransaction{PublicationTransaction: tx, session: p.session, rt: p.rt}, nil
}

type hostTransaction struct {
	driver.PublicationTransaction
	session *Session
	rt      *Runtime
}

func (t *hostTransaction) Stage(batch v1.PublicationBatch) error {
	stamped, err := t.stamp(batch)
	if err != nil {
		return err
	}
	return t.PublicationTransaction.Stage(stamped)
}

// Commit publishes the transaction and then persists the identity checkpoint,
// so the statements and dispositions the commit made resolvable survive a
// process loss without a clean shutdown.
func (t *hostTransaction) Commit(ctx context.Context) error {
	if err := t.PublicationTransaction.Commit(ctx); err != nil {
		return err
	}
	return t.rt.saveCheckpoint()
}

func (t *hostTransaction) DeclareSettingRoute(route driver.SettingRoute) error {
	return t.PublicationTransaction.DeclareSettingRoute(stampRoute(t.session.spec, route))
}

// stamp gives a driver's batch the transaction's asset, source, epoch,
// generation, sequence and expected revision, in the batch and in each record
// that carries them, and recomputes the digest.
func (t *hostTransaction) stamp(b v1.PublicationBatch) (v1.PublicationBatch, error) {
	spec := t.session.spec
	base, err := t.session.Prepare(nil)
	if err != nil {
		return v1.PublicationBatch{}, err
	}
	if b.Contract == "" {
		b.Contract = base.Contract
	}
	b.BatchID, b.AssetID, b.SourceID = base.BatchID, spec.Asset, spec.Source
	b.SourceEpochID, b.DriverGeneration = spec.Epoch, spec.Generation
	b.Sequence, b.ExpectedSemanticRevision = base.Sequence, base.ExpectedSemanticRevision
	if b.ObservedAt == (v1.TimePoint{}) {
		b.ObservedAt = base.ObservedAt
	}
	if b.SourceUpserts == nil {
		b.SourceUpserts = []v1.SourceDescriptor{}
	}
	if b.SourceRetirements == nil {
		b.SourceRetirements = []v1.SourceEpochID{}
	}
	b.SourceRetirements = append(b.SourceRetirements, t.session.priorEpochs()...)
	if b.BindingUpserts == nil {
		b.BindingUpserts = []v1.NativeBinding{}
	}
	if b.IdentityLinkUpserts == nil {
		b.IdentityLinkUpserts = []v1.IdentityLink{}
	}
	if b.FactUpserts == nil {
		b.FactUpserts = []v1.FactCandidate{}
	}
	if b.FactWithdrawals == nil {
		b.FactWithdrawals = []v1.CandidateID{}
	}
	if b.ServiceUpserts == nil {
		b.ServiceUpserts = []v1.ServiceInstance{}
	}
	if b.ServiceWithdrawals == nil {
		b.ServiceWithdrawals = []v1.ServiceInstanceID{}
	}
	if b.CapabilityUpserts == nil {
		b.CapabilityUpserts = []v1.CapabilityInstance{}
	}
	if b.CapabilityWithdrawals == nil {
		b.CapabilityWithdrawals = []v1.CapabilityInstanceID{}
	}
	if b.GenerationFences == nil {
		b.GenerationFences = []v1.GenerationFence{}
	}
	binding := func(id v1.NativeBindingID) v1.NativeBindingID { return v1.NativeBindingID(scopedID(spec, string(id))) }
	for i := range b.FactWithdrawals {
		b.FactWithdrawals[i] = v1.CandidateID(scopedID(spec, string(b.FactWithdrawals[i])))
	}
	for i := range b.ServiceWithdrawals {
		b.ServiceWithdrawals[i] = v1.ServiceInstanceID(scopedID(spec, string(b.ServiceWithdrawals[i])))
	}
	for i := range b.CapabilityWithdrawals {
		b.CapabilityWithdrawals[i] = v1.CapabilityInstanceID(scopedID(spec, string(b.CapabilityWithdrawals[i])))
	}
	for i := range b.SourceUpserts {
		b.SourceUpserts[i].SourceID, b.SourceUpserts[i].SourceEpochID = spec.Source, spec.Epoch
	}
	for i := range b.BindingUpserts {
		n := &b.BindingUpserts[i]
		n.AssetID, n.SourceID, n.SourceEpochID, n.DriverGeneration = spec.Asset, spec.Source, spec.Epoch, spec.Generation
		n.BindingID = binding(n.BindingID)
	}
	for i := range b.IdentityLinkUpserts {
		b.IdentityLinkUpserts[i].AssetID = spec.Asset
		b.IdentityLinkUpserts[i].BindingID = binding(b.IdentityLinkUpserts[i].BindingID)
	}
	for i := range b.ServiceUpserts {
		n := &b.ServiceUpserts[i]
		n.AssetID, n.SourceEpochID, n.DriverGeneration = spec.Asset, spec.Epoch, spec.Generation
		n.InstanceID = v1.ServiceInstanceID(scopedID(spec, string(n.InstanceID)))
		n.BindingID = binding(n.BindingID)
	}
	for i := range b.CapabilityUpserts {
		n := &b.CapabilityUpserts[i]
		n.AssetID, n.SourceEpochID, n.DriverGeneration = spec.Asset, spec.Epoch, spec.Generation
		n.InstanceID = v1.CapabilityInstanceID(scopedID(spec, string(n.InstanceID)))
		n.ServiceInstance = v1.ServiceInstanceID(scopedID(spec, string(n.ServiceInstance)))
		n.BindingID = binding(n.BindingID)
	}
	mono, err := t.session.svc.liveMono()
	if err != nil {
		return v1.PublicationBatch{}, err
	}
	for i := range b.FactUpserts {
		f := &b.FactUpserts[i]
		if f.Times.ReceiptMonotonic == (v1.MonotonicPoint{}) {
			f.Times.ReceiptMonotonic = mono
		}
		if f.Times.EvaluateMonotonic == (v1.MonotonicPoint{}) {
			f.Times.EvaluateMonotonic = mono
		}
		f.CandidateID = v1.CandidateID(scopedID(spec, string(f.CandidateID)))
		if f.Derivation != nil {
			stampDerivation(spec, f.Derivation)
		}
		if f.BindingID != nil {
			id := binding(*f.BindingID)
			f.BindingID = &id
		}
		if f.Origin.BindingID != nil {
			id := binding(*f.Origin.BindingID)
			f.Origin.BindingID = &id
		}
		epoch, generation, source := spec.Epoch, spec.Generation, spec.Source
		if f.SourceEpochID != nil {
			f.SourceEpochID = &epoch
		}
		if f.DriverGeneration != nil {
			f.DriverGeneration = &generation
		}
		if f.Origin.SourceID != nil {
			f.Origin.SourceID = &source
		}
		if f.Origin.SourceEpochID != nil {
			e := spec.Epoch
			f.Origin.SourceEpochID = &e
		}
	}
	digest, err := b.ComputedDigest()
	if err != nil {
		return v1.PublicationBatch{}, err
	}
	b.BatchDigest = digest
	return b, nil
}

// stampDerivation carries a derived candidate's exact dependencies through the
// host's identifier stamping: each input keeps its candidate revision and
// resolves to the stamped candidate and to the stamped source paths.
func stampDerivation(spec SessionSpec, d *v1.Derivation) {
	inputs := make([]v1.DerivationInput, len(d.Inputs))
	for i, input := range d.Inputs {
		paths := make([]v1.SourcePathRef, len(input.SourcePaths))
		for j, path := range input.SourcePaths {
			paths[j] = v1.SourcePathRef{
				BindingID: v1.NativeBindingID(scopedID(spec, string(path.BindingID))),
				SourceID:  spec.Source, SourceEpochID: spec.Epoch, DriverGeneration: spec.Generation,
			}
		}
		sort.SliceStable(paths, func(a, b int) bool { return paths[a].BindingID < paths[b].BindingID })
		inputs[i] = v1.DerivationInput{
			CandidateID:       v1.CandidateID(scopedID(spec, string(input.CandidateID))),
			CandidateRevision: input.CandidateRevision,
			SourcePaths:       paths,
		}
	}
	sort.SliceStable(inputs, func(a, b int) bool { return inputs[a].CandidateID < inputs[b].CandidateID })
	d.Inputs = inputs
}

// priorEpochs lists the current epochs of this session's source that an earlier
// lifecycle generation published: a new epoch retires them in its first batch.
func (s *Session) priorEpochs() []v1.SourceEpochID {
	s.svc.mu.Lock()
	kernel := s.svc.kernels[s.spec.Asset]
	s.svc.mu.Unlock()
	if kernel == nil {
		return nil
	}
	current, _, ok := kernel.Current()
	if !ok {
		return nil
	}
	var out []v1.SourceEpochID
	for _, source := range current.Sources {
		if source.SourceID == s.spec.Source && source.SourceEpochID != s.spec.Epoch && source.State == v1.SourceCurrent {
			out = append(out, source.SourceEpochID)
		}
	}
	return out
}
