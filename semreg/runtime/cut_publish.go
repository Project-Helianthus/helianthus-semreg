package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/sources"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// SessionSpec is one instance session's ownership stamp. Validators and Graph
// are the publication kernel's construction contract: a fact the kernel's
// registered packs do not cover is rejected by that kernel.
type SessionSpec struct {
	Driver     driver.DriverKind
	Instance   driver.InstanceKey
	Asset      v1.AssetID
	Source     v1.SourceID
	Epoch      v1.SourceEpochID
	Generation v1.Uint64
	Validators []v1.PackValidator
	Graph      bool
	Scope      driver.Filter
}

// Session is one instance generation's transactional publisher and scoped read.
type Session struct {
	svc      *Service
	spec     SessionSpec
	scope    driver.Filter
	id       life.InstanceID
	mu       sync.Mutex
	pin      *graph.CutRef
	open     *transaction
	fenced   bool
	ops      []identity.OperatorLink
	seq      uint64
	lineages []sources.Lineage
	sources  []driver.Source
}

// OpenSession gives one started instance its publisher. The asset is known to
// capture from this moment, so a capture that does not include it lists it.
func (s *Service) OpenSession(spec SessionSpec) (*Session, error) {
	if err := spec.Driver.Validate(); err != nil {
		return nil, err
	}
	if err := spec.Instance.Validate(); err != nil {
		return nil, err
	}
	if err := spec.Asset.Validate(); err != nil {
		return nil, err
	}
	if err := spec.Source.Validate(); err != nil {
		return nil, err
	}
	if err := spec.Epoch.Validate(); err != nil {
		return nil, err
	}
	if err := spec.Generation.Validate(); err != nil {
		return nil, err
	}
	if spec.Generation == "0" {
		return nil, errors.New("runtime: generation is zero")
	}
	if err := spec.Scope.Validate(); err != nil {
		return nil, err
	}
	session := &Session{
		svc: s, spec: spec, scope: cloneFilter(spec.Scope),
		id: life.Ref{Driver: spec.Driver, Instance: spec.Instance}.ID(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed[session.id] {
		return nil, &v1.Error{ID: v1.StaleDriverGeneration, Detail: "fenced generation"}
	}
	if _, err := s.kernel(spec); err != nil {
		return nil, err
	}
	if _, _, ok := s.kernels[spec.Asset].Current(); !ok {
		s.known[spec.Asset] = graph.NotIncludedSourceUnavailable
	}
	s.ownerOf[spec.Asset] = session.id
	s.sessions = append(s.sessions, session)
	if s.recovery != nil {
		s.recovery.note(spec)
	}
	return session, nil
}

func (s *Service) kernel(spec SessionSpec) (*v1.PublicationKernel, error) {
	if kernel, ok := s.kernels[spec.Asset]; ok {
		return kernel, nil
	}
	var kernel *v1.PublicationKernel
	var err error
	if spec.Graph {
		kernel, err = v1.NewGraphPublicationKernel(spec.Asset, s.catalog, spec.Validators...)
	} else {
		kernel, err = v1.NewPublicationKernel(spec.Asset, spec.Validators...)
	}
	if err != nil {
		return nil, err
	}
	s.kernels[spec.Asset] = kernel
	return kernel, nil
}

// Fence discards later commits of this generation and keeps its declaration.
func (s *Session) Fence() {
	s.svc.mu.Lock()
	s.svc.stopGeneration(s.spec.Source, s.spec.Epoch, s.spec.Generation)
	s.svc.mu.Unlock()
	s.mu.Lock()
	s.fenced = true
	s.mu.Unlock()
}

func fenceKey(source v1.SourceID, epoch v1.SourceEpochID, generation v1.Uint64) string {
	return string(source) + "\x00" + string(epoch) + "\x00" + string(generation)
}

func (s *Session) fencedNow() bool {
	s.mu.Lock()
	local := s.fenced
	spec := s.spec
	s.mu.Unlock()
	if local {
		return true
	}
	s.svc.mu.Lock()
	defer s.svc.mu.Unlock()
	return s.svc.fencedGen[fenceKey(spec.Source, spec.Epoch, spec.Generation)]
}

// DeclareOperator stages an operator identity declaration on this session.
// It stays pending until a batch of this ownership carries it.
func (s *Session) DeclareOperator(link identity.OperatorLink) {
	s.mu.Lock()
	s.ops = append(s.ops, link)
	s.mu.Unlock()
}

// WithdrawOperator drops a declaration. The next eligible batch withdraws the
// published association through the same transaction.
func (s *Session) WithdrawOperator(native v1.EvidenceRef) {
	s.mu.Lock()
	kept := s.ops[:0]
	for _, link := range s.ops {
		if link.Native != native {
			kept = append(kept, link)
		}
	}
	s.ops = kept
	s.mu.Unlock()
}

// Lineages returns the lineage results of the last commit, including a
// missing, stale or withdrawn input.
func (s *Session) Lineages() []sources.Lineage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sources.Lineage(nil), s.lineages...)
}

// Prepare fills the ownership stamp, the next sequence, the expected semantic
// revision and the batch digest. The caller mutates the batch first.
func (s *Session) Prepare(mutate func(*v1.PublicationBatch)) (v1.PublicationBatch, error) {
	spec := s.spec
	s.svc.mu.Lock()
	kernel := s.svc.kernels[spec.Asset]
	s.svc.mu.Unlock()
	expected := v1.Uint64("0")
	// A rejected batch does not advance the kernel cursor, so the next batch
	// repeats that sequence. The first sequence of a generation stays 1 until
	// one commit is accepted.
	seq := uint64(1)
	if kernel != nil {
		if current, _, ok := kernel.Current(); ok {
			expected = current.Revisions.Semantic
			if n := cursorSequence(current, spec); n > 0 {
				seq = n + 1
			}
		}
	}
	s.mu.Lock()
	s.seq = seq
	s.mu.Unlock()
	batch := blankBatch(spec, seq, expected, s.svc.wall())
	if mutate != nil {
		mutate(&batch)
	}
	digest, err := batch.ComputedDigest()
	if err != nil {
		return v1.PublicationBatch{}, err
	}
	batch.BatchDigest = digest
	return batch, nil
}

func blankBatch(spec SessionSpec, seq uint64, expected v1.Uint64, observed v1.TimePoint) v1.PublicationBatch {
	return v1.PublicationBatch{
		Contract: v1.ContractKernelV1, BatchID: v1.BatchID(fmt.Sprintf("batch:%s:%s:%d", spec.Source, spec.Generation, seq)),
		AssetID: spec.Asset, SourceID: spec.Source, SourceEpochID: spec.Epoch, DriverGeneration: spec.Generation,
		Sequence: v1.Uint64(strconv.FormatUint(seq, 10)), ExpectedSemanticRevision: expected, ObservedAt: observed,
		SourceUpserts: []v1.SourceDescriptor{}, SourceRetirements: []v1.SourceEpochID{}, BindingUpserts: []v1.NativeBinding{},
		IdentityLinkUpserts: []v1.IdentityLink{}, FactUpserts: []v1.FactCandidate{}, FactWithdrawals: []v1.CandidateID{},
		ServiceUpserts: []v1.ServiceInstance{}, ServiceWithdrawals: []v1.ServiceInstanceID{},
		CapabilityUpserts: []v1.CapabilityInstance{}, CapabilityWithdrawals: []v1.CapabilityInstanceID{},
		GenerationFences: []v1.GenerationFence{},
	}
}

// Begin opens the session's one transaction.
func (s *Session) Begin(ctx context.Context) (driver.PublicationTransaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.fencedNow() || s.svc.failedNow(s.id) {
		return nil, &v1.Error{ID: v1.StaleDriverGeneration, Detail: "fenced generation"}
	}
	if s.svc.recovery != nil && s.svc.recovery.blocked() {
		return nil, s.svc.recovery.refusal()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open != nil {
		return nil, errors.New("runtime: a transaction is already open")
	}
	tx := &transaction{session: s}
	s.open = tx
	return tx, nil
}

func (s *Service) failedNow(id life.InstanceID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed[id]
}

// transaction is one publication commit of a session.
type transaction struct {
	session *Session
	batch   *v1.PublicationBatch
	sources []driver.Source
	routes  []driver.SettingRoute
	serials []driver.ManufacturerSerialClaim
	samples []driver.Sample
	links   []driver.LinkClaim
	drop    []driver.SourceRef
	formula *driver.Formula
	origin  driver.Origin
	self    driver.InputRef
	hasLine bool
	closed  bool
}

func (t *transaction) OperatorLinks(upserts []v1.NativeBinding) []v1.IdentityLink {
	session := t.session
	session.mu.Lock()
	closed := t.closed
	spec := session.spec
	declared := append([]identity.OperatorLink(nil), session.ops...)
	session.mu.Unlock()
	if closed || session.fencedNow() {
		return nil
	}
	session.svc.mu.Lock()
	kernel := session.svc.kernels[spec.Asset]
	session.svc.mu.Unlock()
	var snap v1.Snapshot
	present := false
	if kernel != nil {
		snap, _, present = kernel.Current()
	}
	return identity.PendingOperatorLinks(declared, snap, present, spec.Asset, spec.Source, spec.Epoch, spec.Generation, upserts)
}

func (t *transaction) AdmitManufacturerSerial(claim driver.ManufacturerSerialClaim) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	if t.closed {
		return errors.New("runtime: the transaction is closed")
	}
	t.serials = append(t.serials, claim)
	return nil
}

func (t *transaction) Stage(batch v1.PublicationBatch) error {
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	if t.closed {
		return errors.New("runtime: the transaction is closed")
	}
	if t.batch != nil {
		return errors.New("runtime: a batch is already staged")
	}
	t.batch = &batch
	return nil
}

func (t *transaction) DeclareSource(source driver.Source) error {
	if err := source.Validate(); err != nil {
		return err
	}
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	if t.closed {
		return errors.New("runtime: the transaction is closed")
	}
	t.sources = append(t.sources, source)
	return nil
}

func (t *transaction) DeclareSettingRoute(route driver.SettingRoute) error {
	if err := route.Validate(); err != nil {
		return err
	}
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	if t.closed {
		return errors.New("runtime: the transaction is closed")
	}
	t.routes = append(t.routes, route)
	return nil
}

// StageSample stages one sample with the batch. A rejected commit admits none.
func (t *transaction) StageSample(sample driver.Sample) error {
	if err := sample.From.Validate(); err != nil {
		return err
	}
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	if t.closed {
		return errors.New("runtime: the transaction is closed")
	}
	t.samples = append(t.samples, sample)
	return nil
}

// StageLink stages one link claim. The claim is kept whether or not its
// target is present.
func (t *transaction) StageLink(claim driver.LinkClaim) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	if t.closed {
		return errors.New("runtime: the transaction is closed")
	}
	t.links = append(t.links, claim)
	return nil
}

// WithdrawSource stages an explicit withdrawal of a source declaration.
func (t *transaction) WithdrawSource(ref driver.SourceRef) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	if t.closed {
		return errors.New("runtime: the transaction is closed")
	}
	t.drop = append(t.drop, ref)
	return nil
}

// StageFormula resolves every input against the session's pinned cut before
// the commit changes source state.
func (t *transaction) StageFormula(self driver.InputRef, origin driver.Origin, formula *driver.Formula) error {
	if err := origin.Validate(); err != nil {
		return err
	}
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	if t.closed {
		return errors.New("runtime: the transaction is closed")
	}
	t.self, t.origin, t.formula, t.hasLine = self, origin, formula, true
	return nil
}

func (t *transaction) Abort(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	t.closed = true
	if t.session.open == t {
		t.session.open = nil
	}
	return nil
}

func (t *transaction) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session := t.session
	if session.fencedNow() || session.svc.failedNow(session.id) {
		return &v1.Error{ID: v1.StaleDriverGeneration, Detail: "fenced generation"}
	}
	session.mu.Lock()
	if t.closed {
		session.mu.Unlock()
		return errors.New("runtime: the transaction is closed")
	}
	t.closed = true
	if session.open == t {
		session.open = nil
	}
	if t.batch == nil {
		session.mu.Unlock()
		return errors.New("runtime: no batch is staged")
	}
	staged := t.snapshot()
	session.mu.Unlock()
	if err := staged.owns(session.spec); err != nil {
		return err
	}
	if err := staged.serialOwners(session); err != nil {
		return err
	}
	if err := staged.operators(session); err != nil {
		return err
	}
	lineage, err := staged.lineage(session)
	if err != nil {
		return err
	}
	session.mu.Lock()
	session.lineages = lineage
	session.mu.Unlock()
	if session.svc.recovery != nil {
		return session.svc.recovery.commit(ctx, session, staged)
	}
	return session.svc.apply(ctx, session, staged, true)
}

type stagedCommit struct {
	batch   v1.PublicationBatch
	sources []driver.Source
	routes  []driver.SettingRoute
	serials []driver.ManufacturerSerialClaim
	samples []driver.Sample
	links   []driver.LinkClaim
	drop    []driver.SourceRef
	formula *driver.Formula
	origin  driver.Origin
	self    driver.InputRef
	hasLine bool
	ops     []identity.OperatorLink
}

func (t *transaction) snapshot() stagedCommit {
	return stagedCommit{
		batch: *t.batch, sources: append([]driver.Source(nil), t.sources...),
		routes: append([]driver.SettingRoute(nil), t.routes...), serials: append([]driver.ManufacturerSerialClaim(nil), t.serials...),
		samples: append([]driver.Sample(nil), t.samples...), links: append([]driver.LinkClaim(nil), t.links...),
		drop: append([]driver.SourceRef(nil), t.drop...), formula: t.formula, origin: t.origin, self: t.self, hasLine: t.hasLine,
		ops: append([]identity.OperatorLink(nil), t.session.ops...),
	}
}

func (c stagedCommit) owns(spec SessionSpec) error {
	b := c.batch
	if b.AssetID != spec.Asset || b.SourceID != spec.Source || b.SourceEpochID != spec.Epoch || b.DriverGeneration != spec.Generation {
		return &v1.Error{ID: v1.StaleDriverGeneration, Detail: "publication ownership"}
	}
	return nil
}

func (c stagedCommit) serialOwners(session *Session) error {
	publisher := identity.Publisher{Driver: session.spec.Driver, Instance: session.spec.Instance}
	for _, claim := range c.serials {
		node, ok := session.svc.ledger.Lookup(claim.Member)
		if !ok {
			return identity.ErrUnknownMember
		}
		owned := false
		for _, key := range node.Keys {
			if key.Driver == publisher.Driver && key.Instance == publisher.Instance {
				owned = true
				break
			}
		}
		if !owned {
			return identity.ErrNotOwner
		}
	}
	return nil
}

func (c stagedCommit) operators(session *Session) error {
	spec := session.spec
	session.svc.mu.Lock()
	kernel := session.svc.kernels[spec.Asset]
	session.svc.mu.Unlock()
	var snap v1.Snapshot
	present := false
	if kernel != nil {
		snap, _, present = kernel.Current()
	}
	pending := identity.PendingOperatorLinks(c.ops, snap, present, spec.Asset, spec.Source, spec.Epoch, spec.Generation, c.batch.BindingUpserts)
	return identity.CheckOperatorLinks(c.batch.IdentityLinkUpserts, snap, present, pending)
}

func (c stagedCommit) lineage(session *Session) ([]sources.Lineage, error) {
	if !c.hasLine {
		return nil, nil
	}
	line, err := sources.CheckLineage(c.self, c.origin, c.formula, session.resolveInput)
	if err != nil {
		return nil, err
	}
	return []sources.Lineage{line}, nil
}

func (s *Session) resolveInput(ref driver.InputRef) (sources.Input, bool) {
	if s.fencedNow() || s.svc.failedNow(s.id) {
		return sources.Input{}, false
	}
	s.mu.Lock()
	pin := s.pin
	s.mu.Unlock()
	if pin == nil || viewKey(*pin) != viewKey(ref.At) {
		return sources.Input{}, false
	}
	view, err := s.svc.Lookup(context.Background(), ref.At)
	if err != nil {
		return sources.Input{}, false
	}
	var matched []v1.Snapshot
	for _, snap := range view.Members {
		if !inFilter(s.scope, snap.AssetID) || !inFilter(view.Filter, snap.AssetID) {
			continue
		}
		if homes(view, snap, ref) {
			matched = append(matched, snap)
		}
	}
	if len(matched) != 1 {
		return sources.Input{}, false
	}
	candidate := factCandidate(matched[0], ref.Candidate, ref.Revision)
	if candidate == nil {
		return sources.Input{}, false
	}
	evaluated, err := v1.EvaluateSnapshot(matched[0], view.Evaluation)
	if err != nil {
		return sources.Input{}, false
	}
	var fact v1.EvaluatedFact
	found := false
	for _, item := range evaluated.Facts {
		if item.CandidateID == ref.Candidate && item.CandidateRevision == ref.Revision {
			fact, found = item, true
			break
		}
	}
	if !found {
		return sources.Input{}, false
	}
	return sources.Input{Candidate: *candidate, Evaluation: fact}, true
}

func homes(view View, snap v1.Snapshot, ref driver.InputRef) bool {
	if factCandidate(snap, ref.Candidate, ref.Revision) == nil {
		return false
	}
	for _, resource := range snap.Resources {
		if resource.ResourceID == ref.Subject.ID {
			return true
		}
	}
	for _, envelope := range snap.Facts {
		for _, dimension := range envelope.Key.Dimensions {
			if dimension.Value.Text != nil && *dimension.Value.Text == string(ref.Subject.ID) {
				return true
			}
		}
	}
	for _, source := range view.Sources {
		if source.For.Subject != ref.Subject {
			continue
		}
		ev, ok := view.Evals[sources.KeyOf(driver.SourceRef{For: source.For, Producer: source.Producer, PathKey: source.PathKey})]
		if ok && ev.Member.AssetID == snap.AssetID && ev.Member.SnapshotID == snap.SnapshotID {
			return true
		}
	}
	return false
}

func factCandidate(snap v1.Snapshot, id v1.CandidateID, revision v1.Uint64) *v1.FactCandidate {
	for _, envelope := range snap.Facts {
		for i := range envelope.Candidates {
			if envelope.Candidates[i].CandidateID == id && envelope.Candidates[i].Revision == revision {
				candidate := envelope.Candidates[i]
				return &candidate
			}
		}
	}
	for _, record := range snap.Retained {
		if record.Candidate.CandidateID == id && record.Candidate.Revision == revision {
			candidate := record.Candidate
			return &candidate
		}
	}
	return nil
}

func (s *Service) apply(ctx context.Context, session *Session, staged stagedCommit, live bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	mono, err := s.publicationMono()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fencedGen[fenceKey(session.spec.Source, session.spec.Epoch, session.spec.Generation)] || s.failed[session.id] || s.hidden[staged.batch.AssetID] {
		return &v1.Error{ID: v1.StaleDriverGeneration, Detail: "fenced generation"}
	}
	kernel := s.kernels[staged.batch.AssetID]
	if kernel == nil {
		return errors.New("runtime: the asset has no publication kernel")
	}
	before, _, _ := kernel.Current()
	// An equal sequence is this source's accepted batch. Apply returns the
	// historical snapshot and leaves the kernel's current member unchanged,
	// so admission and the runtime cursor must not follow that snapshot.
	if acceptedSequence(before, staged.batch) {
		_, _, err := kernel.Apply(staged.batch, mono)
		return err
	}
	fork, err := kernel.Fork()
	if err != nil {
		return err
	}
	probed, _, err := fork.Apply(staged.batch, mono)
	if err != nil {
		return err
	}
	unchanged := before.SnapshotID != "" && before.SnapshotID == probed.SnapshotID
	if !unchanged {
		if err := s.admissionReady(session, staged, probed); err != nil {
			return err
		}
	}
	after, _, err := kernel.Apply(staged.batch, mono)
	if err != nil {
		return err
	}
	current, _, ok := kernel.Current()
	if !ok {
		current = after
	}
	if before.SnapshotID != "" && current.SnapshotID == before.SnapshotID {
		return nil
	}
	if err := s.admitLocked(session, staged, before, current); err != nil {
		return err
	}
	if live {
		s.advance(staged.batch.AssetID, s.currentLocked(), s.routesForLocked(staged.batch.AssetID))
	}
	delete(s.known, staged.batch.AssetID)
	s.ownerOf[staged.batch.AssetID] = session.id
	return nil
}

func (s *Service) publicationMono() (v1.MonotonicPoint, error) {
	if point, ok := s.receipt.publicationPoint(); ok {
		return point, nil
	}
	return s.liveMono()
}

func (s *Service) currentLocked() []v1.Snapshot {
	var snaps []v1.Snapshot
	for asset, kernel := range s.kernels {
		if s.hidden[asset] {
			continue
		}
		if cur, _, ok := kernel.Current(); ok {
			snaps = append(snaps, cur)
		}
	}
	return snaps
}

func (s *Service) routesForLocked(asset v1.AssetID) []driver.SettingRoute {
	var snaps []v1.Snapshot
	if kernel := s.kernels[asset]; kernel != nil {
		if cur, _, ok := kernel.Current(); ok {
			snaps = []v1.Snapshot{cur}
		}
	}
	var routes []routeRec
	for _, route := range s.routes {
		if route.Asset == asset {
			routes = append(routes, route)
		}
	}
	return routeViews(routes, snaps, &graph.Cut{})
}

// acceptedSequence reports that this source's cursor already accepted the
// batch sequence. The kernel then replays that digest or rejects a conflict.
func acceptedSequence(current v1.Snapshot, batch v1.PublicationBatch) bool {
	for _, cursor := range current.Cursors {
		if cursor.SourceID == batch.SourceID && cursor.SourceEpochID == batch.SourceEpochID && cursor.DriverGeneration == batch.DriverGeneration {
			return cursor.LastSequence == batch.Sequence
		}
	}
	return false
}

func cursorSequence(snap v1.Snapshot, spec SessionSpec) uint64 {
	var max uint64
	for _, cursor := range snap.Cursors {
		if cursor.SourceID != spec.Source || cursor.SourceEpochID != spec.Epoch || cursor.DriverGeneration != spec.Generation {
			continue
		}
		n, err := strconv.ParseUint(string(cursor.LastSequence), 10, 64)
		if err != nil || n == ^uint64(0) {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max
}

// admissionReady reports the error admitLocked would return, without changing
// the kernel, the source table or the cursor.
func (s *Service) admissionReady(session *Session, staged stagedCommit, after v1.Snapshot) error {
	declared := map[string]driver.Source{}
	for key, source := range s.declared {
		declared[key] = source
	}
	for _, source := range staged.sources {
		if err := source.Validate(); err != nil {
			return err
		}
		ref := driver.SourceRef{For: source.For, Producer: source.Producer, PathKey: source.PathKey}
		if _, err := v1.CanonicalJSON(ref); err != nil {
			return err
		}
		declared[sourceKeyOf(source)] = source
	}
	for _, ref := range staged.drop {
		delete(declared, string(sources.KeyOf(ref)))
	}
	for _, sample := range staged.samples {
		candidate := factCandidate(after, sample.Candidate.CandidateID, sample.Candidate.Revision)
		if candidate == nil {
			continue
		}
		if err := admitReady(declared, driver.Sample{From: sample.From, Candidate: *candidate}, after); err != nil {
			return err
		}
	}
	sessionSources := append(append([]driver.Source{}, session.sources...), staged.sources...)
	if len(sessionSources) > 0 {
		for _, source := range sessionSources {
			for _, envelope := range after.Facts {
				if !sameFactKey(envelope.Key, source.For.Key) {
					continue
				}
				for _, candidate := range envelope.Candidates {
					if !upserted(staged.batch, candidate.CandidateID) || !sampleStaged(staged.samples, source, candidate.CandidateID) {
						continue
					}
					if err := admitReady(declared, driver.Sample{From: source, Candidate: candidate}, after); err != nil {
						return err
					}
				}
			}
		}
	}
	publisher := identity.Publisher{Driver: session.spec.Driver, Instance: session.spec.Instance}
	if len(staged.serials) > 0 {
		if err := publisher.Validate(); err != nil {
			return err
		}
	}
	for _, claim := range staged.serials {
		if err := claim.Validate(); err != nil {
			return err
		}
		node, ok := s.ledger.Lookup(claim.Member)
		if !ok {
			return identity.ErrUnknownMember
		}
		owned := false
		for _, key := range node.Keys {
			if key.Driver == publisher.Driver && key.Instance == publisher.Instance {
				owned = true
				break
			}
		}
		if !owned {
			return identity.ErrNotOwner
		}
	}
	if len(staged.links) > 0 {
		if err := publisher.Validate(); err != nil {
			return fmt.Errorf("publisher: %w", err)
		}
	}
	for _, claim := range staged.links {
		if err := claim.Validate(); err != nil {
			return err
		}
		if _, err := v1.CanonicalJSON(claim.Key); err != nil {
			return err
		}
	}
	return nil
}

func admitReady(declared map[string]driver.Source, sample driver.Sample, member v1.Snapshot) error {
	if err := sample.From.Validate(); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if err := sample.Candidate.Validate(); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if err := member.AssetID.Validate(); err != nil {
		return fmt.Errorf("member: %w", err)
	}
	source, ok := declared[sourceKeyOf(sample.From)]
	if !ok {
		return sources.ErrUndeclared
	}
	if canonicalFactKey(sample.Candidate.Key) != canonicalFactKey(source.For.Key) {
		return errors.New("the candidate is not a fact of the source's element")
	}
	if !memberCarries(member, sample.Candidate.Key, sample.Candidate.CandidateID, sample.Candidate.Revision) {
		return errors.New("the member does not carry the exact candidate and revision")
	}
	return nil
}

func canonicalFactKey(key v1.FactKey) string {
	raw, err := v1.CanonicalJSON(key)
	if err != nil {
		raw, _ = json.Marshal(key)
	}
	return string(raw)
}

func memberCarries(member v1.Snapshot, key v1.FactKey, id v1.CandidateID, revision v1.Uint64) bool {
	want := canonicalFactKey(key)
	for _, envelope := range member.Facts {
		if envelope.AssetID != member.AssetID || canonicalFactKey(envelope.Key) != want {
			continue
		}
		for _, candidate := range envelope.Candidates {
			if candidate.CandidateID == id && candidate.Revision == revision {
				return true
			}
		}
	}
	return false
}

func (s *Service) admitLocked(session *Session, staged stagedCommit, before, after v1.Snapshot) error {
	publisher := identity.Publisher{Driver: session.spec.Driver, Instance: session.spec.Instance}
	for _, source := range staged.sources {
		if err := s.table.Declare(source); err != nil {
			return err
		}
		if err := s.ledger.RecordSource(source); err != nil {
			return err
		}
		key := sourceKeyOf(source)
		s.declared[key] = source
		session.sources = append(session.sources, source)
	}
	for _, ref := range staged.drop {
		s.table.Withdraw(ref)
		delete(s.declared, string(sources.KeyOf(ref)))
		delete(s.basis, string(sources.KeyOf(ref)))
	}
	explicit := map[v1.CandidateID]bool{}
	for _, id := range staged.batch.FactWithdrawals {
		explicit[id] = true
	}
	sameFacts := reflect.DeepEqual(before.Facts, after.Facts) && reflect.DeepEqual(before.Retained, after.Retained)
	for key, owner := range s.owners {
		if owner.Asset != staged.batch.AssetID {
			continue
		}
		removed := candidateGone(before, after, owner.ID, owner.Revision)
		switch {
		case explicit[owner.ID] && removed:
			if s.table.WithdrawFact(owner.Ref, owner.Asset, owner.ID, owner.Revision, sources.ExplicitWithdrawal) {
				delete(s.basis, key)
				delete(s.owners, key)
			}
		case !explicit[owner.ID] && !sameFacts && owner.Inferred && removed:
			if s.table.WithdrawFact(owner.Ref, owner.Asset, owner.ID, owner.Revision, sources.DependencyClosure) {
				delete(s.basis, key)
				delete(s.owners, key)
			}
		}
	}
	for _, sample := range staged.samples {
		candidate := factCandidate(after, sample.Candidate.CandidateID, sample.Candidate.Revision)
		if candidate == nil {
			continue
		}
		sample.Candidate = *candidate
		if err := s.table.Admit(sample, after); err != nil {
			return err
		}
		s.rememberOwner(sample.From, after, *candidate)
	}
	for _, source := range s.sourcesOf(session) {
		for _, envelope := range after.Facts {
			if !sameFactKey(envelope.Key, source.For.Key) {
				continue
			}
			for _, candidate := range envelope.Candidates {
				if !upserted(staged.batch, candidate.CandidateID) || !sampleStaged(staged.samples, source, candidate.CandidateID) {
					continue
				}
				if err := s.table.Admit(driver.Sample{From: source, Candidate: candidate}, after); err != nil {
					return err
				}
				s.rememberOwner(source, after, candidate)
			}
		}
	}
	for _, claim := range staged.serials {
		if err := s.ledger.AdmitManufacturerSerial(publisher, claim); err != nil {
			return err
		}
	}
	for _, claim := range staged.links {
		if err := s.ledger.ClaimLink(publisher, claim); err != nil {
			return err
		}
	}
	for _, route := range staged.routes {
		s.routes = append(s.routes, routeRec{
			Route: route, Asset: staged.batch.AssetID, Source: staged.batch.SourceID,
			Epoch: staged.batch.SourceEpochID, Generation: staged.batch.DriverGeneration, Live: true,
		})
	}
	for _, fence := range staged.batch.GenerationFences {
		s.stopGeneration(fence.SourceID, fence.SourceEpochID, fence.DriverGeneration)
	}
	return nil
}

func (s *Service) sourcesOf(session *Session) []driver.Source {
	if len(session.sources) > 0 {
		return session.sources
	}
	return nil
}

func (s *Service) rememberOwner(source driver.Source, member v1.Snapshot, candidate v1.FactCandidate) {
	key := sourceKeyOf(source)
	ref := driver.SourceRef{For: source.For, Producer: source.Producer, PathKey: source.PathKey}
	cloned, err := cloneValue(member)
	if err != nil {
		cloned = member
	}
	s.basis[key] = cloned
	s.assetOf[key] = member.AssetID
	s.stopped[key] = false
	s.declared[key] = source
	s.owners[key] = factOwner{
		Ref: ref, Asset: member.AssetID, ID: candidate.CandidateID, Revision: candidate.Revision,
		Inferred: candidate.Quality.Assertion == v1.AssertionInferred,
	}
}

func (s *Service) stopGeneration(source v1.SourceID, epoch v1.SourceEpochID, generation v1.Uint64) {
	s.fencedGen[fenceKey(source, epoch, generation)] = true
	for i := range s.routes {
		if s.routes[i].Source == source && s.routes[i].Epoch == epoch && s.routes[i].Generation == generation {
			s.routes[i].Live = false
		}
	}
	for _, session := range s.sessions {
		if session.spec.Source != source || session.spec.Epoch != epoch || session.spec.Generation != generation {
			continue
		}
		for _, declared := range session.sources {
			ref := driver.SourceRef{For: declared.For, Producer: declared.Producer, PathKey: declared.PathKey}
			s.table.GenerationStopped(ref)
			s.stopped[sourceKeyOf(declared)] = true
		}
	}
}

func candidateGone(before, after v1.Snapshot, id v1.CandidateID, revision v1.Uint64) bool {
	return factCandidate(before, id, revision) != nil && factCandidate(after, id, revision) == nil
}

func sampleStaged(samples []driver.Sample, source driver.Source, id v1.CandidateID) bool {
	key := sourceKeyOf(source)
	for _, sample := range samples {
		if sourceKeyOf(sample.From) == key && sample.Candidate.CandidateID == id {
			return true
		}
	}
	return false
}

func upserted(batch v1.PublicationBatch, id v1.CandidateID) bool {
	for _, candidate := range batch.FactUpserts {
		if candidate.CandidateID == id {
			return true
		}
	}
	return false
}

func sameFactKey(a, b v1.FactKey) bool {
	left, lerr := v1.CanonicalJSON(a)
	right, rerr := v1.CanonicalJSON(b)
	return lerr == nil && rerr == nil && string(left) == string(right)
}

// Pin records the cut a later formula publication of this session reads.
func (s *Session) Pin(ref graph.CutRef) {
	s.mu.Lock()
	s.pin = &ref
	s.mu.Unlock()
}
