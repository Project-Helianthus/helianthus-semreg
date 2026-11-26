package runtime

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
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
	spec := p.rt.openSpec(p.rt.specFor(planned.Ref, corr))
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
		prepare:   session.Prepare,
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
	spec := p.rt.withdrawSpec(p.rt.specFor(planned.Ref, corr))
	if err := p.rt.withdrawCommitted(ctx, spec); err != nil {
		return err
	}
	root := tree.NodePath{Installation: p.rt.plan.Installation, Driver: planned.Ref.Driver, Instance: planned.Ref.Instance}.NodeID()
	p.rt.svc.withdrawGeneration(spec.Asset, spec.Source, spec.Epoch, spec.Generation, root)
	return nil
}

// retryEpoch names the n-th source epoch of one lifecycle correlation. A
// withdrawal retires the epoch it publishes under, and a retired epoch accepts no
// publication, so a retry of the same correlation publishes under the next one.
func retryEpoch(base v1.SourceEpochID, n int) v1.SourceEpochID {
	if n == 0 {
		return base
	}
	return v1.SourceEpochID(fmt.Sprintf("%s.retry%d", base, n))
}

// epochStates lists the states of the epochs of the spec's source.
func (rt *Runtime) epochStates(spec SessionSpec) map[v1.SourceEpochID]v1.SourceState {
	rt.svc.mu.Lock()
	kernel := rt.svc.kernels[spec.Asset]
	rt.svc.mu.Unlock()
	states := map[v1.SourceEpochID]v1.SourceState{}
	if kernel == nil {
		return states
	}
	current, _, ok := kernel.Current()
	if !ok {
		return states
	}
	for _, source := range current.Sources {
		if source.SourceID == spec.Source {
			states[source.SourceEpochID] = source.State
		}
	}
	return states
}

// openSpec gives the spec the first epoch of its correlation that is not
// retired. A retry after a withdrawal opens a fresh epoch: the failed attempt's
// bindings, routes and executor stay withdrawn and fenced under the old one.
func (rt *Runtime) openSpec(spec SessionSpec) SessionSpec {
	states := rt.epochStates(spec)
	base := spec.Epoch
	for n := 0; ; n++ {
		epoch := retryEpoch(base, n)
		if state, ok := states[epoch]; ok && state != v1.SourceCurrent {
			continue
		}
		spec.Epoch = epoch
		return spec
	}
}

// withdrawSpec gives the spec the latest epoch of its correlation that is
// current, so a withdrawal reaches what a retry published; with none current it
// keeps the correlation's own epoch.
func (rt *Runtime) withdrawSpec(spec SessionSpec) SessionSpec {
	states := rt.epochStates(spec)
	base := spec.Epoch
	for n := 0; ; n++ {
		state, ok := states[retryEpoch(base, n)]
		if !ok {
			if n == 0 {
				continue
			}
			return spec
		}
		if state == v1.SourceCurrent {
			spec.Epoch = retryEpoch(base, n)
		}
	}
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
		facts = candidateIDsAscending(current.Facts)
		sort.Slice(services, func(i, j int) bool { return services[i] < services[j] })
		sort.Slice(capabilities, func(i, j int) bool { return capabilities[i] < capabilities[j] })
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

// candidateIDsAscending lists every candidate of the envelopes in the ascending
// identifier order a withdrawal batch requires; the envelopes themselves are
// ordered by fact key, which is a different order.
func candidateIDsAscending(envelopes []v1.FactEnvelope) []v1.CandidateID {
	var ids []v1.CandidateID
	for _, envelope := range envelopes {
		for _, candidate := range envelope.Candidates {
			ids = append(ids, candidate.CandidateID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// withdrawGeneration marks the generation fenced, its routes not live and every
// source declared for the asset stopped, so SS06 resolves no value from it. A
// declaration that never held a sample, and one a recovery replay restored
// through a session that is no longer registered, belongs to the asset by its
// producer: a node at or below the instance's root.
func (s *Service) withdrawGeneration(asset v1.AssetID, source v1.SourceID, epoch v1.SourceEpochID, generation v1.Uint64, root v1.ResourceID) {
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
	for key, declared := range s.declared {
		if declared.Producer.ID != root && !strings.HasPrefix(string(declared.Producer.ID), string(root)+"/") {
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
	prepare   func(func(*v1.PublicationBatch)) (v1.PublicationBatch, error)
}

var _ driver.Session = (*hostSession)(nil)

func (s *hostSession) Native() driver.NativeRegistry       { return s.native }
func (s *hostSession) SemanticInput() driver.SemanticInput { return s.input }
func (s *hostSession) ScopedOperations() driver.ScopedOperations {
	return hostScopedOps{inner: s.scoped}
}
func (s *hostSession) Calendar() driver.Calendar   { return s.calendar }
func (s *hostSession) Publisher() driver.Publisher { return s.publisher }

func (s *hostSession) Prepare(mutate func(*v1.PublicationBatch)) (v1.PublicationBatch, error) {
	return s.prepare(mutate)
}

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
	session  *Session
	rt       *Runtime
	mu       sync.Mutex
	facts    []v1.FactCandidate
	declared []driver.Source
	// driverSampled holds the samples the driver staged itself, so the commit
	// does not stage a second sample for the same fact of the same source. The
	// host rewrites candidate ids when it stamps a batch, so a fact is matched by
	// its key and source.
	driverSampled []driver.Sample
	samplesStaged bool
}

// StageSample records the driver's own sample. The host stamps a batch and
// rewrites the candidate ids of its facts, so the sample is handed to the
// transaction at commit, carrying the stamped candidate of the fact it names.
func (t *hostTransaction) StageSample(sample driver.Sample) error {
	if err := sample.From.Validate(); err != nil {
		return err
	}
	t.mu.Lock()
	t.driverSampled = append(t.driverSampled, sample)
	t.mu.Unlock()
	return nil
}

// DeclareSource stages the declaration and remembers it, so the commit admits
// the transaction's accepted candidates of that element to the source.
func (t *hostTransaction) DeclareSource(source driver.Source) error {
	if err := t.PublicationTransaction.DeclareSource(source); err != nil {
		return err
	}
	t.mu.Lock()
	t.declared = append(t.declared, source)
	t.mu.Unlock()
	return nil
}

// stageSamples stages, with the same commit, a sample of every fact this
// transaction upserted for the element of a source this session declared (in
// this transaction or earlier). The commit admits the candidate and revision the
// member accepted, and none a rejected commit leaves out. A candidate carries no
// statement of which declared path supplied it, so the host associates it only
// when exactly one declared path claims its element; when several paths claim

func sampledByDriver(samples []driver.Sample, fact v1.FactCandidate) bool {
	for _, sample := range samples {
		if sameFactKey(sample.Candidate.Key, fact.Key) && sourceKeyOf(sample.From) != "" {
			return true
		}
	}
	return false
}

func (t *hostTransaction) stageSamples() error {
	t.mu.Lock()
	done := t.samplesStaged
	t.samplesStaged = true
	t.mu.Unlock()
	if done {
		return nil
	}
	t.session.mu.Lock()
	sources := append([]driver.Source(nil), t.session.sources...)
	t.session.mu.Unlock()
	t.mu.Lock()
	sources = append(sources, t.declared...)
	facts := append([]v1.FactCandidate(nil), t.facts...)
	driverSampled := append([]driver.Sample(nil), t.driverSampled...)
	t.mu.Unlock()
	for _, sample := range driverSampled {
		for _, fact := range facts {
			if sameFactKey(sample.Candidate.Key, fact.Key) {
				sample.Candidate = fact
				break
			}
		}
		if err := t.PublicationTransaction.StageSample(sample); err != nil {
			return err
		}
	}
	for _, fact := range facts {
		if sampledByDriver(driverSampled, fact) {
			continue
		}
		var claimed []driver.Source
		seen := map[string]bool{}
		for _, source := range sources {
			if !sameFactKey(fact.Key, source.For.Key) {
				continue
			}
			key := sourceKeyOf(source)
			if seen[key] {
				continue
			}
			seen[key] = true
			claimed = append(claimed, source)
		}
		if len(claimed) != 1 {
			continue
		}
		if err := t.PublicationTransaction.StageSample(driver.Sample{From: claimed[0], Candidate: fact}); err != nil {
			return err
		}
	}
	return nil
}

func (t *hostTransaction) Stage(batch v1.PublicationBatch) error {
	stamped, err := t.stamp(batch)
	if err != nil {
		return err
	}
	if err := t.PublicationTransaction.Stage(stamped); err != nil {
		return err
	}
	t.mu.Lock()
	t.facts = append(t.facts, stamped.FactUpserts...)
	t.mu.Unlock()
	return nil
}

// Commit publishes the transaction and then persists the identity checkpoint,
// so the statements and dispositions the commit made resolvable survive a
// process loss without a clean shutdown. The publication is already committed
// when the checkpoint is written, so a checkpoint failure does not turn the
// result into a failed publication. The failure stays reported by
// Runtime.CheckpointErr, and the next checkpoint write, at the following commit,
// at WaitInitial or at Close, exports the whole identity state again and reports
// its own failure.
func (t *hostTransaction) Commit(ctx context.Context) error {
	if err := t.stageSamples(); err != nil {
		return err
	}
	if err := t.PublicationTransaction.Commit(ctx); err != nil {
		return err
	}
	_ = t.rt.resolveAndSaveCheckpoint()
	return nil
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
	// The batch is the driver's: every slice the stamping rewrites is copied
	// first, so a retry of the same batch stamps from what the driver declared.
	b.FactWithdrawals = cloneOf(b.FactWithdrawals)
	b.ServiceWithdrawals = cloneOf(b.ServiceWithdrawals)
	b.CapabilityWithdrawals = cloneOf(b.CapabilityWithdrawals)
	b.SourceUpserts = cloneOf(b.SourceUpserts)
	b.BindingUpserts = cloneOf(b.BindingUpserts)
	b.IdentityLinkUpserts = cloneOf(b.IdentityLinkUpserts)
	b.ServiceUpserts = cloneOf(b.ServiceUpserts)
	b.CapabilityUpserts = cloneOf(b.CapabilityUpserts)
	b.ResourceUpserts = cloneOf(b.ResourceUpserts)
	b.RelationUpserts = cloneOf(b.RelationUpserts)
	b.AbsenceUpserts = cloneOf(b.AbsenceUpserts)
	b.FactUpserts = cloneOf(b.FactUpserts)
	if b.SourceUpserts == nil {
		b.SourceUpserts = []v1.SourceDescriptor{}
	}
	if b.SourceRetirements == nil {
		b.SourceRetirements = []v1.SourceEpochID{}
	}
	b.SourceRetirements = append(cloneOf(b.SourceRetirements), t.session.priorEpochs()...)
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
	for i := range b.RelationUpserts {
		stampOrigin(spec, &b.RelationUpserts[i].Origin)
	}
	for i := range b.AbsenceUpserts {
		stampOrigin(spec, &b.AbsenceUpserts[i].Origin)
	}
	for i := range b.ResourceUpserts {
		stampOrigin(spec, &b.ResourceUpserts[i].Origin)
	}
	mono, err := t.session.svc.liveMono()
	if err != nil {
		return v1.PublicationBatch{}, err
	}
	localPaths := make(map[v1.SourcePathRef]bool)
	localCandidates := make(map[v1.CandidateID]bool)
	for _, fact := range b.FactUpserts {
		localCandidates[fact.CandidateID] = true
		if fact.BindingID != nil && fact.Origin.SourceID != nil && fact.SourceEpochID != nil && fact.DriverGeneration != nil {
			localPaths[v1.SourcePathRef{BindingID: *fact.BindingID, SourceID: *fact.Origin.SourceID, SourceEpochID: *fact.SourceEpochID, DriverGeneration: *fact.DriverGeneration}] = true
		}
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
			derivation := *f.Derivation
			stampDerivation(spec, &derivation, localCandidates, localPaths)
			f.Derivation = &derivation
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
	// The suffix changes how identifiers compare (an identifier that is a prefix
	// of another no longer sorts first), so the collections the contract orders by
	// identifier are put back in canonical order before sealing.
	sort.SliceStable(b.BindingUpserts, func(i, j int) bool { return b.BindingUpserts[i].BindingID < b.BindingUpserts[j].BindingID })
	sort.SliceStable(b.IdentityLinkUpserts, func(i, j int) bool { return b.IdentityLinkUpserts[i].BindingID < b.IdentityLinkUpserts[j].BindingID })
	sort.SliceStable(b.FactUpserts, func(i, j int) bool { return b.FactUpserts[i].CandidateID < b.FactUpserts[j].CandidateID })
	sort.SliceStable(b.FactWithdrawals, func(i, j int) bool { return b.FactWithdrawals[i] < b.FactWithdrawals[j] })
	sort.SliceStable(b.ServiceUpserts, func(i, j int) bool { return b.ServiceUpserts[i].InstanceID < b.ServiceUpserts[j].InstanceID })
	sort.SliceStable(b.ServiceWithdrawals, func(i, j int) bool { return b.ServiceWithdrawals[i] < b.ServiceWithdrawals[j] })
	sort.SliceStable(b.CapabilityUpserts, func(i, j int) bool { return b.CapabilityUpserts[i].InstanceID < b.CapabilityUpserts[j].InstanceID })
	sort.SliceStable(b.CapabilityWithdrawals, func(i, j int) bool { return b.CapabilityWithdrawals[i] < b.CapabilityWithdrawals[j] })
	digest, err := b.ComputedDigest()
	if err != nil {
		return v1.PublicationBatch{}, err
	}
	b.BatchDigest = digest
	return b, nil
}

// stampOrigin gives a native relation or absence origin the stamped binding,
// source and epoch of the session, so it resolves to the batch's own binding;
// the record's key, basis and evidence stay as declared.
func stampOrigin(spec SessionSpec, o *v1.OriginRef) {
	if o.BindingID != nil {
		id := v1.NativeBindingID(scopedID(spec, string(*o.BindingID)))
		o.BindingID = &id
	}
	if o.SourceID != nil {
		source := spec.Source
		o.SourceID = &source
	}
	if o.SourceEpochID != nil {
		epoch := spec.Epoch
		o.SourceEpochID = &epoch
	}
}

// stampDerivation maps driver-local references declared by this batch. A
// reference from a retained cut is already in the runtime's coordinates and
// must be handed to publication validation without changing its provenance.
func stampDerivation(spec SessionSpec, d *v1.Derivation, localCandidates map[v1.CandidateID]bool, localPaths map[v1.SourcePathRef]bool) {
	d.Inputs = cloneOf(d.Inputs)
	for i := range d.Inputs {
		input := &d.Inputs[i]
		input.SourcePaths = cloneOf(input.SourcePaths)
		if !localCandidates[input.CandidateID] {
			continue
		}
		stamped := false
		for j, path := range input.SourcePaths {
			if !localPaths[path] {
				continue
			}
			input.SourcePaths[j] = v1.SourcePathRef{
				BindingID: v1.NativeBindingID(scopedID(spec, string(path.BindingID))),
				SourceID:  spec.Source, SourceEpochID: spec.Epoch, DriverGeneration: spec.Generation,
			}
			stamped = true
		}
		if stamped {
			input.CandidateID = v1.CandidateID(scopedID(spec, string(input.CandidateID)))
			slices.SortStableFunc(input.SourcePaths, func(a, b v1.SourcePathRef) int {
				return cmp.Or(cmp.Compare(a.SourceID, b.SourceID), cmp.Compare(a.SourceEpochID, b.SourceEpochID),
					cmp.Compare(len(a.DriverGeneration), len(b.DriverGeneration)), cmp.Compare(a.DriverGeneration, b.DriverGeneration), cmp.Compare(a.BindingID, b.BindingID))
			})
		}
	}
	sort.SliceStable(d.Inputs, func(a, b int) bool { return d.Inputs[a].CandidateID < d.Inputs[b].CandidateID })
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

// cloneOf copies a slice, keeping a nil slice nil.
func cloneOf[T any](s []T) []T {
	if s == nil {
		return nil
	}
	return append(make([]T, 0, len(s)), s...)
}

// hostScopedOps carries the session's calling surface into both invoke paths.
// A surface the caller's context already names stays; nothing else is added.
type hostScopedOps struct{ inner *call.Scoped }

func withSessionSurface(ctx context.Context) context.Context {
	caller, _ := call.CallerFrom(ctx)
	if caller.Surface.Digest == "" {
		caller.Surface = hostSurface()
	}
	return call.WithCaller(ctx, caller)
}

func (o hostScopedOps) Invoke(ctx context.Context, request driver.OperationRequest) (driver.OperationResult, error) {
	return o.inner.Invoke(withSessionSurface(ctx), request)
}

func (o hostScopedOps) InvokeCallBound(ctx context.Context, request driver.OperationRequest) (driver.OperationResult, error) {
	return o.inner.InvokeCallBound(withSessionSurface(ctx), request)
}
