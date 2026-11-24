package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Recovery adapts the session publisher to the lifecycle control store. The
// payload is opaque to the lifecycle service. Publication and Secure both
// refuse while an ambiguous readback is unsettled.
type Recovery struct {
	svc   *Service
	store life.Store

	// seqMu covers one intent, its apply and its outcome, and Secure.
	// Field locks do not: two commits can otherwise snapshot the same
	// envelope and the later write drops the other's accepted record.
	seqMu         sync.Mutex
	mu            sync.Mutex
	arm           func()
	memory        []byte
	amb           *ambiguousWrite
	pending       []byte
	seq           uint64
	applies       int
	beforeApply   func()
	specs         map[life.InstanceID]SessionSpec
	continuity    uint64
	continuityErr error
	obligations   map[life.InstanceID]*obligation
}

type ambiguousWrite struct {
	payload []byte
	prev    []byte
	intent  bool
}

type obligation struct {
	Instance               life.InstanceID
	NativeClosePending     bool
	SemanticCleanupPending bool
	AttemptsUsed           int
	NativeClosed           bool
	SemanticCleaned        bool
}

type diskEnv struct {
	Owners      []ownerBlob  `json:"owners"`
	Obligations []obligation `json:"obligations,omitempty"`
	Anchor      []byte       `json:"anchor,omitempty"`
	Sample      uint64       `json:"sample,omitempty"`
}

type ownerBlob struct {
	Instance life.InstanceID `json:"instance"`
	Records  json.RawMessage `json:"records"`
}

type journalRecord struct {
	Seq         uint64                           `json:"seq"`
	Batch       v1.PublicationBatch              `json:"batch"`
	Sources     []driver.Source                  `json:"sources,omitempty"`
	Routes      []driver.SettingRoute            `json:"routes,omitempty"`
	Serials     []driver.ManufacturerSerialClaim `json:"serials,omitempty"`
	Samples     []driver.Sample                  `json:"samples,omitempty"`
	Links       []driver.LinkClaim               `json:"links,omitempty"`
	Drop        []driver.SourceRef               `json:"drop,omitempty"`
	Ops         []identity.OperatorLink          `json:"ops,omitempty"`
	Receipt     v1.MonotonicPoint                `json:"receipt"`
	Instance    life.InstanceID                  `json:"instance"`
	Generation  v1.Uint64                        `json:"generation"`
	Asset       v1.AssetID                       `json:"asset"`
	Outcome     string                           `json:"outcome"`
	Position    uint64                           `json:"position"`
	Publication v1.MonotonicPoint                `json:"publication,omitempty"`
	Applied     bool                             `json:"applied"`
	Graph       bool                             `json:"graph,omitempty"`
	Driver      driver.DriverKind                `json:"driver"`
	Owner       driver.InstanceKey               `json:"owner"`
	Source      v1.SourceID                      `json:"source"`
	Epoch       v1.SourceEpochID                 `json:"epoch"`
}

// BindRecovery attaches the control store. Later commits of every session go
// through this adapter. The host does not open a second publisher or store.
func (s *Service) BindRecovery(store life.Store) *Recovery {
	recovery := &Recovery{
		svc: s, store: store,
		specs:       map[life.InstanceID]SessionSpec{},
		obligations: map[life.InstanceID]*obligation{},
	}
	s.mu.Lock()
	s.recovery = recovery
	for _, session := range s.sessions {
		recovery.specs[session.id] = session.spec
	}
	s.mu.Unlock()
	return recovery
}

func (r *Recovery) note(spec SessionSpec) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.specs == nil {
		r.specs = map[life.InstanceID]SessionSpec{}
	}
	r.specs[life.Ref{Driver: spec.Driver, Instance: spec.Instance}.ID()] = spec
}

func (r *Recovery) blocked() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.amb != nil
}

func (r *Recovery) refusal() error {
	return &life.Refusal{Code: life.RefusalUnavailable, Row: 3, Reason: "readback after ambiguous persistence still pending"}
}

// BindReadback saves the service's resolver. Start calls it before Restore.
func (r *Recovery) BindReadback(arm func()) {
	r.mu.Lock()
	r.arm = arm
	r.mu.Unlock()
}

// RestoreEmpty opens nothing of its own. The publisher is already the session's.
func (r *Recovery) RestoreEmpty(context.Context) error { return nil }

// Restore replays accepted records from the committed envelope. An owner whose
// bytes do not decode is failed closed; another owner still restores.
func (r *Recovery) Restore(ctx context.Context, material *life.RecoveryMaterial) error {
	if material == nil {
		return nil
	}
	var env diskEnv
	if err := json.Unmarshal(material.Payload, &env); err != nil {
		return err
	}
	var failed []life.InstanceID
	var cause error
	var items []replayItem
	for _, owner := range env.Owners {
		var records []journalRecord
		if err := json.Unmarshal(owner.Records, &records); err != nil {
			failed = append(failed, owner.Instance)
			if cause == nil {
				cause = err
			}
			r.svc.markFailed(owner.Instance)
			continue
		}
		for _, record := range records {
			if record.Outcome != "accepted" || !record.Applied {
				continue
			}
			items = append(items, replayItem{owner: owner.Instance, record: record})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].record.Position != items[j].record.Position {
			return items[i].record.Position < items[j].record.Position
		}
		return items[i].record.Seq < items[j].record.Seq
	})
	skip := map[life.InstanceID]bool{}
	for _, item := range items {
		if skip[item.owner] {
			continue
		}
		if err := r.replay(ctx, item.record); err != nil {
			skip[item.owner] = true
			failed = append(failed, item.owner)
			if cause == nil {
				cause = err
			}
			r.svc.markFailed(item.owner)
		}
	}
	r.noteContinuity(env)
	r.mu.Lock()
	r.memory = append([]byte(nil), material.Payload...)
	r.obligations = map[life.InstanceID]*obligation{}
	for i := range env.Obligations {
		ob := env.Obligations[i]
		r.obligations[ob.Instance] = &ob
	}
	r.mu.Unlock()
	if len(failed) > 0 {
		if cause == nil {
			cause = errors.New("runtime: recovery failed closed")
		}
		return &life.RestoreError{Drivers: failed, Err: cause}
	}
	return nil
}

func (r *Recovery) noteContinuity(env diskEnv) {
	if len(env.Anchor) == 0 {
		return
	}
	sample, err := r.svc.clock.Sample()
	if err != nil {
		r.mu.Lock()
		r.continuity, r.continuityErr = 0, err
		r.mu.Unlock()
		return
	}
	elapsed, err := r.svc.clock.Elapsed(env.Anchor, sample)
	r.mu.Lock()
	if err != nil {
		r.continuity, r.continuityErr = 0, err
	} else {
		r.continuity, r.continuityErr = elapsed, nil
	}
	r.mu.Unlock()
}

// Continuity is the elapsed time from the stored anchor. An unverifiable
// anchor yields the clock's error and a zero elapsed time.
func (r *Recovery) Continuity() (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.continuity, r.continuityErr
}

type replayItem struct {
	owner  life.InstanceID
	record journalRecord
}

func (r *Recovery) replay(ctx context.Context, record journalRecord) error {
	// The kernel rejects an older sequence. A prefix the cursor has already
	// passed was accepted; applying it again would fail a second restore.
	if r.appliedAhead(record.Batch) {
		return nil
	}
	spec := r.specFor(record)
	session := &Session{svc: r.svc, spec: spec, id: record.Instance, scope: driver.Filter{}}
	r.svc.mu.Lock()
	if _, err := r.svc.kernel(spec); err != nil {
		r.svc.mu.Unlock()
		return err
	}
	r.svc.mu.Unlock()
	r.svc.receipt.force(record.Receipt)
	if record.Publication.Nanoseconds != "" {
		r.svc.receipt.pinPublication(record.Publication)
	}
	defer r.svc.receipt.clear()
	staged := stagedCommit{
		batch: record.Batch, sources: record.Sources, routes: record.Routes, serials: record.Serials,
		samples: record.Samples, links: record.Links, drop: record.Drop, ops: record.Ops,
	}
	return r.svc.apply(ctx, session, staged, true)
}

// appliedAhead reports that this batch's cursor already passed its sequence.
// An equal sequence is left to the kernel, which replays the same digest
// without a second publication.
func (r *Recovery) appliedAhead(batch v1.PublicationBatch) bool {
	want, err := strconv.ParseUint(string(batch.Sequence), 10, 64)
	if err != nil {
		return false
	}
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	kernel := r.svc.kernels[batch.AssetID]
	if kernel == nil {
		return false
	}
	current, _, ok := kernel.Current()
	if !ok {
		return false
	}
	for _, cursor := range current.Cursors {
		if cursor.SourceID != batch.SourceID || cursor.SourceEpochID != batch.SourceEpochID || cursor.DriverGeneration != batch.DriverGeneration {
			continue
		}
		have, err := strconv.ParseUint(string(cursor.LastSequence), 10, 64)
		if err != nil {
			return false
		}
		return have > want
	}
	return false
}

func (r *Recovery) specFor(record journalRecord) SessionSpec {
	r.mu.Lock()
	spec, ok := r.specs[record.Instance]
	r.mu.Unlock()
	if ok && spec.Asset == record.Asset {
		return spec
	}
	return SessionSpec{
		Driver: record.Driver, Instance: record.Owner, Asset: record.Asset, Source: record.Source,
		Epoch: record.Epoch, Generation: record.Generation, Graph: record.Graph,
		Validators: spec.Validators,
	}
}

// Secure settles a pending outcome before a terminal slot can be pruned.
// One close obligation does not clear the other. A readback still pending
// refuses without a store write.
func (r *Recovery) Secure(ctx context.Context, id life.InstanceID, terminal life.Transition) (*life.RecoveryMaterial, error) {
	r.seqMu.Lock()
	defer r.seqMu.Unlock()
	if err := r.settlePending(ctx); err != nil {
		return nil, err
	}
	if !transitionMatters(terminal) {
		return nil, nil
	}
	r.mu.Lock()
	next, ob, err := r.withObligation(id, terminal)
	prev := append([]byte(nil), r.memory...)
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := r.store.CommitRecovery(ctx, r.material(next)); err != nil {
		if !r.store.AdmissionOpen() {
			return nil, r.hold(&ambiguousWrite{payload: next, prev: prev}, err)
		}
		return nil, err
	}
	r.mu.Lock()
	r.memory = next
	if r.obligations == nil {
		r.obligations = map[life.InstanceID]*obligation{}
	}
	recorded := ob
	r.obligations[id] = &recorded
	r.mu.Unlock()
	return nil, nil
}

func transitionMatters(t life.Transition) bool {
	return t.Phase != "" || t.NativeClosePending || t.SemanticCleanupPending || t.AttemptsUsed != 0
}

func (r *Recovery) withObligation(id life.InstanceID, terminal life.Transition) ([]byte, obligation, error) {
	env := decodeEnv(r.memory)
	ob := obligationOf(env, id)
	ob.NativeClosePending = terminal.NativeClosePending
	ob.SemanticCleanupPending = terminal.SemanticCleanupPending
	if terminal.AttemptsUsed > ob.AttemptsUsed {
		ob.AttemptsUsed = terminal.AttemptsUsed
	}
	if terminal.Phase == life.PhaseCleanup && !terminal.NativeClosePending {
		ob.NativeClosed = true
	}
	if terminal.Phase == life.PhaseCleanup && !terminal.SemanticCleanupPending {
		ob.SemanticCleaned = true
	}
	putObligation(&env, ob)
	raw, err := json.Marshal(env)
	return raw, ob, err
}

// Obligation returns the recorded close obligations of one instance.
func (r *Recovery) Obligation(id life.InstanceID) obligation {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ob := r.obligations[id]; ob != nil {
		return *ob
	}
	return obligation{Instance: id}
}

// Applies is the number of publisher calls that reached the kernel.
func (r *Recovery) Applies() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.applies
}

// ReadbackResolved adopts a committed intent without applying it, or adopts a
// committed outcome without applying it again. A contradictory readback
// changes nothing that was already in memory.
func (r *Recovery) ReadbackResolved() {
	committed := r.store.Recovery()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.amb == nil {
		return
	}
	var payload []byte
	if committed != nil {
		payload = committed.Payload
	}
	switch {
	case bytes.Equal(payload, r.amb.payload) && r.amb.intent:
		// The intent reached the store and the publisher was not called.
		r.memory = append([]byte(nil), r.amb.prev...)
		r.amb = nil
	case bytes.Equal(payload, r.amb.payload) && !r.amb.intent:
		r.memory = append([]byte(nil), payload...)
		r.pending = nil
		r.amb = nil
	case bytes.Equal(payload, r.amb.prev):
		if !r.amb.intent {
			r.pending = append([]byte(nil), r.amb.payload...)
		}
		r.amb = nil
	}
}

// settlePending writes an accepted outcome the publisher already applied when
// that outcome is not yet the committed envelope. The next commit has to
// build on it. Leaving the unapplied intent in memory and then clearing the
// pending outcome drops the accepted publication from recovery.
func (r *Recovery) settlePending(ctx context.Context) error {
	r.mu.Lock()
	if r.amb != nil {
		r.mu.Unlock()
		return r.refusal()
	}
	pending := append([]byte(nil), r.pending...)
	r.mu.Unlock()
	if pending == nil {
		return nil
	}
	if err := r.store.CommitRecovery(ctx, r.material(pending)); err != nil {
		if !r.store.AdmissionOpen() {
			r.mu.Lock()
			prev := append([]byte(nil), r.memory...)
			r.mu.Unlock()
			return r.hold(&ambiguousWrite{payload: pending, prev: prev}, err)
		}
		return err
	}
	r.mu.Lock()
	r.memory, r.pending = pending, nil
	r.mu.Unlock()
	return nil
}

func (r *Recovery) commit(ctx context.Context, session *Session, staged stagedCommit) error {
	r.seqMu.Lock()
	defer r.seqMu.Unlock()
	if err := r.settlePending(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	if r.amb != nil {
		r.mu.Unlock()
		return r.refusal()
	}
	mono, err := r.svc.publicationMono()
	if err != nil {
		r.mu.Unlock()
		return err
	}
	r.seq++
	seq := r.seq
	prev := append([]byte(nil), r.memory...)
	receipt := r.svc.receipt.now()
	record := journalRecord{
		Seq: seq, Batch: staged.batch, Sources: staged.sources, Routes: staged.routes, Serials: staged.serials,
		Samples: staged.samples, Links: staged.links, Drop: staged.drop, Ops: staged.ops, Receipt: receipt,
		Instance: session.id, Generation: session.spec.Generation, Asset: session.spec.Asset, Outcome: "",
		Graph: session.spec.Graph, Driver: session.spec.Driver, Owner: session.spec.Instance,
		Source: session.spec.Source, Epoch: session.spec.Epoch, Publication: mono,
	}
	intent, err := r.finish(appendRecord(prev, record))
	if err != nil {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	if err := r.store.CommitRecovery(ctx, r.material(intent)); err != nil {
		if !r.store.AdmissionOpen() {
			return r.hold(&ambiguousWrite{payload: intent, prev: prev, intent: true}, err)
		}
		return err
	}
	r.mu.Lock()
	r.memory = intent
	before := r.beforeApply
	r.mu.Unlock()
	if before != nil {
		before()
	}
	r.svc.receipt.force(receipt)
	r.svc.receipt.pinPublication(mono)
	err = r.svc.apply(ctx, session, staged, true)
	r.svc.receipt.clear()
	if err != nil {
		return err
	}
	position := r.svc.positionUnsafe()
	r.mu.Lock()
	r.applies++
	record.Outcome, record.Applied, record.Position = "accepted", true, position
	outcome, err := r.finish(appendRecord(prev, record))
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if err := r.store.CommitRecovery(ctx, r.material(outcome)); err != nil {
		if !r.store.AdmissionOpen() {
			return r.hold(&ambiguousWrite{payload: outcome, prev: intent}, err)
		}
		r.mu.Lock()
		r.pending = outcome
		r.mu.Unlock()
		return err
	}
	r.mu.Lock()
	r.memory, r.pending = outcome, nil
	r.mu.Unlock()
	return nil
}

func (s *Service) positionUnsafe() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.position
}

func (r *Recovery) hold(w *ambiguousWrite, err error) error {
	r.mu.Lock()
	r.amb = w
	if !w.intent {
		r.pending = append([]byte(nil), w.payload...)
	}
	arm := r.arm
	r.mu.Unlock()
	if arm != nil {
		arm()
	}
	return err
}

func (r *Recovery) material(payload []byte) *life.RecoveryMaterial {
	return &life.RecoveryMaterial{DriverIDs: r.driverIDs(payload), MaxDesiredRevision: 0, Payload: payload}
}

func (r *Recovery) driverIDs(payload []byte) []life.InstanceID {
	env := decodeEnv(payload)
	seen := map[life.InstanceID]bool{}
	var ids []life.InstanceID
	for _, owner := range env.Owners {
		if owner.Instance != "" && !seen[owner.Instance] {
			seen[owner.Instance] = true
			ids = append(ids, owner.Instance)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (s *Service) markFailed(id life.InstanceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed[id] = true
	for asset, owner := range s.ownerOf {
		if owner == id {
			s.hidden[asset] = true
		}
	}
}

func appendRecord(prev []byte, record journalRecord) ([]byte, error) {
	env := decodeEnv(prev)
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var records []json.RawMessage
	for i := range env.Owners {
		if env.Owners[i].Instance != record.Instance {
			continue
		}
		if len(env.Owners[i].Records) > 0 {
			if err := json.Unmarshal(env.Owners[i].Records, &records); err != nil {
				return nil, err
			}
		}
		records = append(records, raw)
		encoded, err := json.Marshal(records)
		if err != nil {
			return nil, err
		}
		env.Owners[i].Records = encoded
		return stamp(env)
	}
	encoded, err := json.Marshal([]json.RawMessage{raw})
	if err != nil {
		return nil, err
	}
	env.Owners = append(env.Owners, ownerBlob{Instance: record.Instance, Records: encoded})
	sort.Slice(env.Owners, func(i, j int) bool { return env.Owners[i].Instance < env.Owners[j].Instance })
	return stamp(env)
}

func stamp(env diskEnv) ([]byte, error) {
	return json.Marshal(env)
}

func (r *Recovery) finish(raw []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	env := decodeEnv(raw)
	sample, sampleErr := r.svc.clock.Sample()
	if sampleErr != nil {
		return raw, nil
	}
	anchor, anchorErr := r.svc.clock.Anchor(sample)
	if anchorErr != nil {
		return raw, nil
	}
	env.Anchor, env.Sample = anchor, sample
	return json.Marshal(env)
}

func decodeEnv(raw []byte) diskEnv {
	var env diskEnv
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &env)
	}
	return env
}

func obligationOf(env diskEnv, id life.InstanceID) obligation {
	for _, ob := range env.Obligations {
		if ob.Instance == id {
			return ob
		}
	}
	return obligation{Instance: id}
}

func putObligation(env *diskEnv, ob obligation) {
	for i := range env.Obligations {
		if env.Obligations[i].Instance == ob.Instance {
			env.Obligations[i] = ob
			return
		}
	}
	env.Obligations = append(env.Obligations, ob)
	sort.Slice(env.Obligations, func(i, j int) bool { return env.Obligations[i].Instance < env.Obligations[j].Instance })
}

// anchorNow records the clock anchor on the committed envelope when a sample
// is available. A sample error leaves the previous anchor in place.
func (r *Recovery) anchorNow(ctx context.Context) error {
	sample, err := r.svc.clock.Sample()
	if err != nil {
		return err
	}
	anchor, err := r.svc.clock.Anchor(sample)
	if err != nil {
		return err
	}
	r.mu.Lock()
	env := decodeEnv(r.memory)
	env.Anchor, env.Sample = anchor, sample
	raw, err := json.Marshal(env)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if err := r.store.CommitRecovery(ctx, r.material(raw)); err != nil {
		return err
	}
	r.mu.Lock()
	r.memory = raw
	r.mu.Unlock()
	return nil
}
