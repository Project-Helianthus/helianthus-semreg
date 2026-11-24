package identity

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// Errors of the ledger.
var (
	ErrNilAllocator    = errors.New("identity: nil allocator")
	ErrUnknownMember   = errors.New("identity: the member is not a node of this ledger")
	ErrNotOwner        = errors.New("identity: the member belongs to another publisher")
	ErrSchemeNotListed = errors.New("identity: the scheme is not listed in the catalog")
	ErrIssuer          = errors.New("identity: the issuer does not fit the scheme's issuer registry")
	ErrSecondSite      = errors.New("identity: the installation already has another site")
)

// Publisher is the driver instance whose session commits a claim. Epoch and
// generation are fences of one transaction, not part of what a claim attests,
// so they are not part of the publisher.
type Publisher struct {
	Driver   driver.DriverKind  `json:"driver"`
	Instance driver.InstanceKey `json:"instance"`
}

func publisherText(p Publisher) string { return string(p.Driver) + "\x00" + string(p.Instance) }

// Validate checks the driver kind and the instance key.
func (p Publisher) Validate() error {
	if err := p.Driver.Validate(); err != nil {
		return err
	}
	return p.Instance.Validate()
}

// NodeState is the binding state of an identity.
type NodeState string

// NodeState values. A node is unbound until a person rebinds an earlier
// identity to it; an unexplained changed identifier gives an unbound node.
const (
	NodeUnbound NodeState = "unbound"
	NodeRebound NodeState = "rebound"
)

// Node is one identity the ledger knows. Keys are every key that allocated to
// the reference, in canonical order. ReboundFrom is the earlier identity a
// person rebound to this one, when an applied rebind names it.
type Node struct {
	Ref         driver.NodeRef
	Keys        []driver.NodeKey
	State       NodeState
	ReboundFrom *driver.NodeRef
}

// LinkClaimView is one publisher's claim of a link with its basis, and the key
// the publisher declared it under.
type LinkClaimView struct {
	Publisher Publisher
	Declared  driver.LinkKey
	Basis     []semreg.EvidenceRef
}

// Link is a link under its effective key, with every publisher's claim.
type Link struct {
	Key    driver.LinkKey
	Claims []LinkClaimView
}

type matchRecord struct {
	Publisher Publisher         `json:"publisher"`
	Claim     driver.MatchClaim `json:"claim"`
}

type serialRecord struct {
	Publisher Publisher                      `json:"publisher"`
	Claim     driver.ManufacturerSerialClaim `json:"claim"`
}

type linkRecord struct {
	Publisher Publisher            `json:"publisher"`
	Key       driver.LinkKey       `json:"key"`
	Basis     []semreg.EvidenceRef `json:"basis"`
}

type matchKey struct {
	member         driver.NodeRef
	scheme, issuer string
}

type linkSlot struct {
	key       string
	publisher Publisher
}

// Ledger is the identity state of one installation: the nodes allocated, the
// link claims, the evidence claims, the certified statements and their
// revocations, the rebinds, the sources with their priorities and the sites. It
// is safe for concurrent use. The identifiers are the allocator's; the ledger
// never re-keys one.
type Ledger struct {
	mu        sync.Mutex
	allocator *Allocator
	nodes     map[driver.NodeRef][]driver.NodeKey
	links     map[linkSlot]linkRecord
	matches   map[matchKey]matchRecord
	serials   map[driver.NodeRef]serialRecord
	certs     []certRecord
	rebinds   []rebindRecord
	sources   map[string]driver.Source
	sites     map[string]driver.NodeRef
}

// NewLedger returns an empty ledger over an allocator, which names the catalog
// the ledger admits identification schemes against.
func NewLedger(allocator *Allocator) (*Ledger, error) {
	if allocator == nil {
		return nil, ErrNilAllocator
	}
	return &Ledger{
		allocator: allocator,
		nodes:     make(map[driver.NodeRef][]driver.NodeKey),
		links:     make(map[linkSlot]linkRecord),
		matches:   make(map[matchKey]matchRecord),
		serials:   make(map[driver.NodeRef]serialRecord),
		sources:   make(map[string]driver.Source),
		sites:     make(map[string]driver.NodeRef),
	}, nil
}

// Node allocates the identity a key names under a bridge and records it. A key
// whose token changed is another key: it gets another identity, unbound, and the
// earlier identity stays with its links and history.
func (l *Ledger) Node(bridge driver.BridgeID, key driver.NodeKey) (driver.NodeRef, error) {
	if err := bridge.Validate(); err != nil {
		return driver.NodeRef{}, fmt.Errorf("bridge: %w", err)
	}
	// The anchor is committed and the node recorded in one critical section, so
	// a restore or an export never sees one without the other. Lock order: the
	// ledger, then the allocator.
	l.mu.Lock()
	defer l.mu.Unlock()
	id, err := l.allocator.Allocate(key)
	if err != nil {
		return driver.NodeRef{}, err
	}
	ref := driver.NodeRef{Bridge: bridge, ID: id}
	l.addKey(ref, key)
	return ref, nil
}

func (l *Ledger) addKey(ref driver.NodeRef, key driver.NodeKey) {
	for _, held := range l.nodes[ref] {
		if held == key {
			return
		}
	}
	keys := append(l.nodes[ref], key)
	sort.Slice(keys, func(i, j int) bool { return keyOrder(keys[i]) < keyOrder(keys[j]) })
	l.nodes[ref] = keys
}

func keyOrder(k driver.NodeKey) string {
	return strings.Join([]string{k.Installation, string(k.Driver), string(k.Instance), string(k.Kind), k.Anchor.Scheme, k.Anchor.Issuer, string(k.Anchor.Key), string(k.Anchor.Parent), k.Anchor.Slot}, "\x00")
}

// Lookup returns the identity a reference names. An identity that was replaced
// by a rebind is still resolvable.
func (l *Ledger) Lookup(ref driver.NodeRef) (Node, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	keys, ok := l.nodes[ref]
	if !ok {
		return Node{}, false
	}
	node := Node{Ref: ref, Keys: append([]driver.NodeKey(nil), keys...), State: NodeUnbound}
	for _, r := range l.rebinds {
		if !r.Revoked && r.New == ref && l.present(r.Old) {
			from := r.Old
			node.State, node.ReboundFrom = NodeRebound, &from
		}
	}
	return node, true
}

func (l *Ledger) present(ref driver.NodeRef) bool {
	_, ok := l.nodes[ref]
	return ok
}

// compareRef orders node references canonically: bridge, then resource id.
func compareRef(a, b driver.NodeRef) int {
	if c := strings.Compare(string(a.Bridge), string(b.Bridge)); c != 0 {
		return c
	}
	return strings.Compare(string(a.ID), string(b.ID))
}

func sortRefs(refs []driver.NodeRef) {
	sort.Slice(refs, func(i, j int) bool { return compareRef(refs[i], refs[j]) < 0 })
}

// ownedBy requires the member to be a node of the ledger that one of its keys
// attributes to the publisher.
func (l *Ledger) ownedBy(member driver.NodeRef, p Publisher) error {
	keys, ok := l.nodes[member]
	if !ok {
		return ErrUnknownMember
	}
	for _, key := range keys {
		if key.Driver == p.Driver && key.Instance == p.Instance {
			return nil
		}
	}
	return ErrNotOwner
}

// ClaimLink keeps one publisher's claim of a link under its link key. A second
// publisher's claim of the same directed tuple is kept beside the first, and a
// publisher's later claim replaces only its own basis. The claim is kept whether
// or not the target is present.
func (l *Ledger) ClaimLink(p Publisher, claim driver.LinkClaim) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("publisher: %w", err)
	}
	if err := claim.Validate(); err != nil {
		return err
	}
	text, err := linkText(claim.Key)
	if err != nil {
		return err
	}
	basis := append([]semreg.EvidenceRef(nil), claim.Basis...)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.links[linkSlot{key: text, publisher: p}] = linkRecord{Publisher: p, Key: claim.Key, Basis: basis}
	return nil
}

// WithdrawLink withdraws one publisher's claim of a link. Another publisher's
// claim of the same tuple stays.
func (l *Ledger) WithdrawLink(p Publisher, key driver.LinkKey) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("publisher: %w", err)
	}
	text, err := linkText(key)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.links, linkSlot{key: text, publisher: p})
	return nil
}

func linkText(key driver.LinkKey) (string, error) {
	if err := key.Validate(); err != nil {
		return "", err
	}
	canonical, err := semreg.CanonicalJSON(key)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

// Links returns every retained link under its effective key: a declared
// endpoint that an applied rebind replaced is shown at the identity it was
// rebound to. Claims keep the key they were declared under. The order is
// canonical.
func (l *Ledger) Links() []Link {
	l.mu.Lock()
	defer l.mu.Unlock()
	grouped := map[string]*Link{}
	var order []string
	for _, record := range l.links {
		effective := l.effective(record.Key)
		text, _ := linkText(effective)
		link, ok := grouped[text]
		if !ok {
			link = &Link{Key: effective}
			grouped[text] = link
			order = append(order, text)
		}
		link.Claims = append(link.Claims, LinkClaimView{Publisher: record.Publisher, Declared: record.Key, Basis: append([]semreg.EvidenceRef(nil), record.Basis...)})
	}
	sort.Strings(order)
	out := make([]Link, 0, len(order))
	for _, text := range order {
		link := grouped[text]
		sort.Slice(link.Claims, func(i, j int) bool {
			a, b := link.Claims[i], link.Claims[j]
			if a.Publisher != b.Publisher {
				return publisherText(a.Publisher) < publisherText(b.Publisher)
			}
			x, _ := linkText(a.Declared)
			y, _ := linkText(b.Declared)
			return x < y
		})
		out = append(out, *link)
	}
	return out
}

// Claims returns every publisher's claim of one link key, as declared.
func (l *Ledger) Claims(key driver.LinkKey) []LinkClaimView {
	text, err := linkText(key)
	if err != nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []LinkClaimView
	for slot, record := range l.links {
		if slot.key == text {
			out = append(out, LinkClaimView{Publisher: record.Publisher, Declared: record.Key, Basis: append([]semreg.EvidenceRef(nil), record.Basis...)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return publisherText(out[i].Publisher) < publisherText(out[j].Publisher)
	})
	return out
}

// LinkCoherence reports how a cut classifies the link a key names, by the rules
// of the cut: coherent, stale, dangling on a complete cut, outside the cut on a
// partial cut, or excluded. The second result is false when no member of the
// cut holds a relation instance for the key. The link is then retained here with
// the same To, there is no traversable qualified edge, and no resource is
// invented for it.
func (l *Ledger) LinkCoherence(cut *graph.Cut, key driver.LinkKey) (graph.RelationCoherence, bool) {
	relation, ok := cut.Relation(semreg.RelationKey{Definition: key.Type, From: key.From.ID, To: key.To.ID})
	if !ok {
		return "", false
	}
	return relation.Coherence, true
}
