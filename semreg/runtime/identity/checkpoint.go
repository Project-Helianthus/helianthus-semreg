package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// The checkpoint document: a format name and a version compared exactly.
const (
	checkpointFormat  = "helianthus.semreg.identity-checkpoint"
	checkpointVersion = 1
)

// Errors of the checkpoint.
var (
	ErrCheckpointDamaged = errors.New("identity: the checkpoint is damaged")
	ErrCheckpointForeign = errors.New("identity: the checkpoint is of another format or version")
)

// RecordSource records one source of an element with its priority, keyed by the
// element, the producer and the path key. A later record of the same source
// replaces its priority. No share or budget is kept per source.
func (l *Ledger) RecordSource(source driver.Source) error {
	if err := source.Validate(); err != nil {
		return err
	}
	key, err := sourceKey(source)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sources[key] = source
	return nil
}

// Sources returns every recorded source with its priority, in canonical order.
func (l *Ledger) Sources() []driver.Source {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sourcesLocked()
}

func (l *Ledger) sourcesLocked() []driver.Source {
	keys := make([]string, 0, len(l.sources))
	for key := range l.sources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]driver.Source, 0, len(keys))
	for _, key := range keys {
		out = append(out, l.sources[key])
	}
	return out
}

func sourceKey(source driver.Source) (string, error) {
	canonical, err := semreg.CanonicalJSON(driver.SourceRef{For: source.For, Producer: source.Producer, PathKey: source.PathKey})
	return string(canonical), err
}

type checkpointNode struct {
	Ref  driver.NodeRef   `json:"ref"`
	Keys []driver.NodeKey `json:"keys"`
}

type checkpointSite struct {
	Installation string         `json:"installation"`
	Site         driver.NodeRef `json:"site"`
}

type checkpoint struct {
	Format         string           `json:"format"`
	Version        int              `json:"version"`
	Anchors        Table            `json:"anchors"`
	Nodes          []checkpointNode `json:"nodes"`
	Links          []linkRecord     `json:"links"`
	Matches        []matchRecord    `json:"matches"`
	Serials        []serialRecord   `json:"manufacturer_serials"`
	Certifications []certRecord     `json:"certifications"`
	Rebinds        []rebindRecord   `json:"rebinds"`
	Sources        []driver.Source  `json:"sources"`
	Sites          []checkpointSite `json:"sites"`
}

// Export writes the identity state: the anchors and supersessions of the store,
// every node with its keys, the link claims of every publisher, the evidence
// claims, the certified statements and rebinds with their revocations, every
// source with its priority and the sites. The checkpoint of an asset with
// several sources carries every source and its priority. The bytes are the same
// for the same state.
func (l *Ledger) Export() ([]byte, error) {
	table, err := l.allocator.store.Table()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStore, err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	doc := checkpoint{
		Format: checkpointFormat, Version: checkpointVersion, Anchors: table,
		Nodes: []checkpointNode{}, Links: []linkRecord{}, Matches: []matchRecord{}, Serials: []serialRecord{},
		Certifications: append([]certRecord{}, l.certs...), Rebinds: append([]rebindRecord{}, l.rebinds...),
		Sources: append([]driver.Source{}, l.sourcesLocked()...), Sites: []checkpointSite{},
	}
	for ref, keys := range l.nodes {
		doc.Nodes = append(doc.Nodes, checkpointNode{Ref: ref, Keys: keys})
	}
	sort.Slice(doc.Nodes, func(i, j int) bool { return compareRef(doc.Nodes[i].Ref, doc.Nodes[j].Ref) < 0 })
	for _, record := range l.links {
		doc.Links = append(doc.Links, record)
	}
	sort.Slice(doc.Links, func(i, j int) bool {
		a, _ := linkText(doc.Links[i].Key)
		b, _ := linkText(doc.Links[j].Key)
		if a != b {
			return a < b
		}
		return publisherText(doc.Links[i].Publisher) < publisherText(doc.Links[j].Publisher)
	})
	for _, record := range l.matches {
		doc.Matches = append(doc.Matches, record)
	}
	sort.Slice(doc.Matches, func(i, j int) bool {
		a, b := doc.Matches[i].Claim, doc.Matches[j].Claim
		if c := compareRef(a.Member, b.Member); c != 0 {
			return c < 0
		}
		return a.Scheme+"\x00"+a.Issuer < b.Scheme+"\x00"+b.Issuer
	})
	for _, record := range l.serials {
		doc.Serials = append(doc.Serials, record)
	}
	sort.Slice(doc.Serials, func(i, j int) bool { return compareRef(doc.Serials[i].Claim.Member, doc.Serials[j].Claim.Member) < 0 })
	for installation, site := range l.sites {
		doc.Sites = append(doc.Sites, checkpointSite{Installation: installation, Site: site})
	}
	sort.Slice(doc.Sites, func(i, j int) bool { return doc.Sites[i].Installation < doc.Sites[j].Installation })
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("identity: checkpoint encode: %w", err)
	}
	return append(data, '\n'), nil
}

// Restore replaces the identity state, and the content of the store, with a
// checkpoint. The same identifiers, link claims, sources with their priorities
// and revocations come back: a revoked join stays unapplied and both member
// references stay resolvable. A document of another format or version is
// refused with ErrCheckpointForeign, and a document with an invalid member with
// ErrCheckpointDamaged; the state is then unchanged.
func (l *Ledger) Restore(data []byte) error {
	var header struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return fmt.Errorf("%w: %v", ErrCheckpointDamaged, err)
	}
	if header.Format != checkpointFormat || header.Version != checkpointVersion {
		return ErrCheckpointForeign
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var doc checkpoint
	if err := decoder.Decode(&doc); err != nil {
		return fmt.Errorf("%w: %v", ErrCheckpointDamaged, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: data after the document", ErrCheckpointDamaged)
	}
	state, err := ledgerOf(l.allocator, doc)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCheckpointDamaged, err)
	}
	if err := l.allocator.store.Replace(doc.Anchors); err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nodes, l.links, l.matches, l.serials = state.nodes, state.links, state.matches, state.serials
	l.certs, l.rebinds, l.sources, l.sites = state.certs, state.rebinds, state.sources, state.sites
	return nil
}

// ledgerOf validates a decoded checkpoint and builds the state it holds.
func ledgerOf(allocator *Allocator, doc checkpoint) (*Ledger, error) {
	state, err := NewLedger(allocator)
	if err != nil {
		return nil, err
	}
	for _, node := range doc.Nodes {
		if err := node.Ref.Validate(); err != nil {
			return nil, err
		}
		if _, duplicate := state.nodes[node.Ref]; duplicate || len(node.Keys) == 0 {
			return nil, fmt.Errorf("node %s", node.Ref.ID)
		}
		for _, key := range node.Keys {
			if err := key.Validate(); err != nil {
				return nil, err
			}
			state.addKey(node.Ref, key)
		}
	}
	for _, record := range doc.Links {
		if err := first(record.Publisher.Validate(), (driver.LinkClaim{Key: record.Key, Basis: record.Basis}).Validate()); err != nil {
			return nil, err
		}
		text, _ := linkText(record.Key)
		slot := linkSlot{key: text, publisher: record.Publisher}
		if _, duplicate := state.links[slot]; duplicate {
			return nil, fmt.Errorf("duplicate link claim")
		}
		state.links[slot] = record
	}
	for _, record := range doc.Matches {
		if err := first(record.Publisher.Validate(), record.Claim.Validate()); err != nil {
			return nil, err
		}
		state.matches[matchKey{member: record.Claim.Member, scheme: record.Claim.Scheme, issuer: record.Claim.Issuer}] = record
	}
	for _, record := range doc.Serials {
		if err := first(record.Publisher.Validate(), record.Claim.Validate()); err != nil {
			return nil, err
		}
		state.serials[record.Claim.Member] = record
	}
	for _, record := range doc.Certifications {
		if err := record.Join.Validate(); err != nil {
			return nil, err
		}
	}
	for _, record := range doc.Rebinds {
		if err := first(record.Old.Validate(), record.New.Validate(), record.Join.Validate()); err != nil {
			return nil, err
		}
	}
	state.certs, state.rebinds = doc.Certifications, doc.Rebinds
	for _, source := range doc.Sources {
		if err := source.Validate(); err != nil {
			return nil, err
		}
		key, err := sourceKey(source)
		if err != nil {
			return nil, err
		}
		state.sources[key] = source
	}
	for _, site := range doc.Sites {
		if err := site.Site.Validate(); err != nil {
			return nil, err
		}
		state.sites[site.Installation] = site.Site
	}
	return state, nil
}
