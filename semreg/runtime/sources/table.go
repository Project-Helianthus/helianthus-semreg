package sources

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// FactRemovalCause is why an accepted publication removed the candidate of a
// source. ExplicitWithdrawal is a known accepted FactWithdrawals target;
// DependencyClosure is the kernel's automatic removal of a derived candidate
// whose input changed.
type FactRemovalCause string

// FactRemovalCause values.
const (
	ExplicitWithdrawal FactRemovalCause = "explicit_withdrawal"
	DependencyClosure  FactRemovalCause = "dependency_closure"
)

// ErrUndeclared is returned when a sample names a source that is not declared.
var ErrUndeclared = errors.New("the source is not declared")

// Unresolved is a value whose mapping to an element is not resolved by evidence
// or certification. It stays a separate second view and is never an element.
type Unresolved struct {
	Producer driver.NodeRef   `json:"producer"`
	PathKey  string           `json:"path_key"`
	Key      v1.FactKey       `json:"key"`
	Value    v1.Value         `json:"value"`
	Evidence []v1.EvidenceRef `json:"evidence"`
}

type acquisition struct {
	asset      v1.AssetID
	candidate  v1.CandidateID
	epoch      *v1.SourceEpochID
	generation *v1.Uint64
	identity   string
	receivedAt v1.TimePoint
	receipt    v1.MonotonicPoint
	stamp      driver.ReceiptStamp
}

type held struct {
	candidate v1.FactCandidate
	member    v1.Snapshot
}

type entry struct {
	source  driver.Source
	stopped bool
	asset   v1.AssetID
	sample  *held
	acq     *acquisition
	removal FactRemovalCause
}

// Table holds the source declarations of every element and the latest accepted
// sample of each source.
type Table struct {
	mu         sync.Mutex
	clock      func() v1.MonotonicPoint
	entries    map[string]*entry
	unresolved []Unresolved
}

// NewTable returns an empty table whose runtime receipts are stamped from
// clock at admission.
func NewTable(clock func() v1.MonotonicPoint) (*Table, error) {
	if clock == nil {
		return nil, errors.New("a receipt clock is required")
	}
	return &Table{clock: clock, entries: make(map[string]*entry)}, nil
}

func refOf(s driver.Source) driver.SourceRef {
	return driver.SourceRef{For: s.For, Producer: s.Producer, PathKey: s.PathKey}
}

func canonicalKey(k v1.FactKey) string {
	raw, err := v1.CanonicalJSON(k)
	if err != nil {
		raw, _ = json.Marshal(k)
	}
	return string(raw)
}

func refKey(r driver.SourceRef) string {
	return fmt.Sprintf("%q|%q|%q|%q|%q|%q", r.For.Subject.Bridge, r.For.Subject.ID, canonicalKey(r.For.Key), r.Producer.Bridge, r.Producer.ID, r.PathKey)
}

func elementKey(e driver.Element) string {
	return fmt.Sprintf("%q|%q|%q", e.Subject.Bridge, e.Subject.ID, canonicalKey(e.Key))
}

func compareRef(a, b driver.SourceRef) int {
	for _, c := range []int{
		strings.Compare(string(a.For.Subject.Bridge), string(b.For.Subject.Bridge)),
		strings.Compare(string(a.For.Subject.ID), string(b.For.Subject.ID)),
		bytes.Compare([]byte(canonicalKey(a.For.Key)), []byte(canonicalKey(b.For.Key))),
		strings.Compare(string(a.Producer.Bridge), string(b.Producer.Bridge)),
		strings.Compare(string(a.Producer.ID), string(b.Producer.ID)),
		strings.Compare(a.PathKey, b.PathKey),
	} {
		if c != 0 {
			return c
		}
	}
	return 0
}

func clone[T any](v T) T {
	raw, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}

// Declare adds the declaration of a source, or replaces the declaration of the
// same SourceRef with its new priority, path, resolution and relays. A held
// sample is kept.
func (t *Table) Declare(s driver.Source) error {
	if err := s.Validate(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := refKey(refOf(s))
	if existing, ok := t.entries[key]; ok {
		existing.source = clone(s)
	} else {
		t.entries[key] = &entry{source: clone(s)}
	}
	kept := t.unresolved[:0]
	for _, u := range t.unresolved {
		if u.Producer == s.Producer && u.PathKey == s.PathKey && canonicalKey(u.Key) == canonicalKey(s.For.Key) {
			continue
		}
		kept = append(kept, u)
	}
	t.unresolved = kept
	return nil
}

// Withdraw is the driver's explicit withdrawal of its declaration: the source
// leaves contention and nothing of it is kept, including its acquisition
// comparison fields.
func (t *Table) Withdraw(ref driver.SourceRef) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, refKey(ref))
}

// Hold keeps a value whose mapping to an element is unresolved as a separate
// second view. It is not an element and no source is declared for it.
func (t *Table) Hold(u Unresolved) error {
	if err := u.Producer.Validate(); err != nil {
		return err
	}
	if u.PathKey == "" {
		return errors.New("path key is required")
	}
	if err := u.Key.Validate(); err != nil {
		return err
	}
	if err := u.Value.Validate(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.unresolved = append(t.unresolved, clone(u))
	return nil
}

// Unresolved returns the second view of values with no resolved element.
func (t *Table) Unresolved() []Unresolved {
	t.mu.Lock()
	defer t.mu.Unlock()
	return clone(t.unresolved)
}

type acquiredTimes struct {
	AssetID       v1.AssetID        `json:"asset_id"`
	CandidateID   v1.CandidateID    `json:"candidate_id"`
	Origin        v1.OriginRef      `json:"origin"`
	Evidence      []v1.EvidenceRef  `json:"evidence"`
	PhenomenonAt  *v1.TimePoint     `json:"phenomenon_at,omitempty"`
	SourceAt      *v1.TimePoint     `json:"source_at,omitempty"`
	ReceivedAt    v1.TimePoint      `json:"received_at"`
	ReceiptMonoAt v1.MonotonicPoint `json:"receipt_monotonic"`
}

func acquisitionOf(asset v1.AssetID, c v1.FactCandidate) *acquisition {
	raw, _ := json.Marshal(acquiredTimes{AssetID: asset, CandidateID: c.CandidateID, Origin: c.Origin, Evidence: c.Evidence, PhenomenonAt: c.Times.PhenomenonAt, SourceAt: c.Times.SourceAt, ReceivedAt: c.Times.ReceivedAt, ReceiptMonoAt: c.Times.ReceiptMonotonic})
	return &acquisition{asset: asset, candidate: c.CandidateID, epoch: clone(c.SourceEpochID), generation: clone(c.DriverGeneration), identity: string(raw), receivedAt: c.Times.ReceivedAt, receipt: c.Times.ReceiptMonotonic}
}

func samePtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func bigOf(s string) (*big.Int, bool) { return new(big.Int).SetString(s, 10) }

// olderThan reports whether the acquisition a is demonstrably earlier than b
// under the clock they share: the same monotonic epoch, else the same wall clock
// once both uncertainties are included.
func olderThan(a, b *acquisition) bool {
	if a.receipt.ClockEpochID == b.receipt.ClockEpochID {
		x, xok := bigOf(string(a.receipt.Nanoseconds))
		y, yok := bigOf(string(b.receipt.Nanoseconds))
		return xok && yok && x.Cmp(y) < 0
	}
	if a.receivedAt.ClockID == b.receivedAt.ClockID {
		x, xok := bigOf(string(a.receivedAt.UnixNanoseconds))
		y, yok := bigOf(string(b.receivedAt.UnixNanoseconds))
		ua, uaok := bigOf(string(a.receivedAt.UncertaintyNS))
		ub, ubok := bigOf(string(b.receivedAt.UncertaintyNS))
		if !xok || !yok || !uaok || !ubok {
			return false
		}
		// Earlier only when the gap exceeds both uncertainties together.
		gap := new(big.Int).Sub(y, x)
		return gap.Cmp(new(big.Int).Add(ua, ub)) > 0
	}
	return false
}

func carries(member v1.Snapshot, key v1.FactKey, id v1.CandidateID, revision v1.Uint64) bool {
	want := canonicalKey(key)
	for _, envelope := range member.Facts {
		if envelope.AssetID != member.AssetID || canonicalKey(envelope.Key) != want {
			continue
		}
		for _, c := range envelope.Candidates {
			if c.CandidateID == id && c.Revision == revision {
				return true
			}
		}
	}
	return false
}

// Admit takes one sample of a declared source together with the exact
// immutable owner member accepted by the same publication commit. Every
// published fact is admitted; validity and availability travel as supplied and
// no unit is converted. The runtime stamps the receipt here. An older
// observation, or a replay of the held acquisition, refreshes neither the
// receipt nor the saved member basis; a higher revision of the held
// acquisition updates the current revision and the member basis and keeps the
// original receipt; a new acquisition is stamped with a new receipt.
func (t *Table) Admit(sample driver.Sample, member v1.Snapshot) error {
	if err := sample.From.Validate(); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if err := sample.Candidate.Validate(); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	if err := member.AssetID.Validate(); err != nil {
		return fmt.Errorf("member: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[refKey(refOf(sample.From))]
	if !ok {
		return ErrUndeclared
	}
	c := sample.Candidate
	if canonicalKey(c.Key) != canonicalKey(e.source.For.Key) {
		return errors.New("the candidate is not a fact of the source's element")
	}
	if !carries(member, c.Key, c.CandidateID, c.Revision) {
		return errors.New("the member does not carry the exact candidate and revision")
	}
	next := acquisitionOf(member.AssetID, c)
	if e.acq != nil && e.acq.asset == next.asset && e.acq.candidate == next.candidate && samePtr(e.acq.epoch, next.epoch) && samePtr(e.acq.generation, next.generation) {
		if e.acq.identity == next.identity {
			if e.sample == nil || !higherRevision(c.Revision, e.sample.candidate.Revision) {
				return nil
			}
			e.sample = &held{candidate: clone(c), member: clone(member)}
			return nil
		}
		if olderThan(next, e.acq) {
			return nil
		}
	}
	next.stamp = driver.ReceiptStamp{At: t.clock()}
	e.acq = next
	e.asset = member.AssetID
	e.sample = &held{candidate: clone(c), member: clone(member)}
	e.removal = ""
	e.stopped = false
	return nil
}

func higherRevision(next, current v1.Uint64) bool {
	x, xok := bigOf(string(next))
	y, yok := bigOf(string(current))
	return xok && yok && x.Cmp(y) > 0
}

// GenerationStopped keeps the declaration and its last value and exposes the
// source as stale while the value is within its retention deadline. It reports
// whether the source is declared.
func (t *Table) GenerationStopped(ref driver.SourceRef) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[refKey(ref)]
	if ok {
		e.stopped = true
	}
	return ok
}

// WithdrawFact applies an accepted removal of the exact candidate and revision a
// source owned on an asset. It clears the retained sample, its value, its
// evidence and its member basis, keeps the declaration and the acquisition
// comparison fields, and records the cause. It reports whether it removed
// anything; another asset, candidate or revision removes nothing.
func (t *Table) WithdrawFact(ref driver.SourceRef, asset v1.AssetID, candidate v1.CandidateID, revision v1.Uint64, cause FactRemovalCause) bool {
	if cause != ExplicitWithdrawal && cause != DependencyClosure {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[refKey(ref)]
	if !ok || e.sample == nil || e.asset != asset || e.sample.candidate.CandidateID != candidate || e.sample.candidate.Revision != revision {
		return false
	}
	e.sample = nil
	e.removal = cause
	return true
}
