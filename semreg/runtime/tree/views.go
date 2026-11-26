package tree

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// View registration errors. Duplicate records wrap ErrDuplicate.
var (
	ErrUnknownViewMember = errors.New("view member is not registered")
	ErrUnknownViewRecord = errors.New("view record is not declared")
)

const keywordViews Keyword = "views"

// ViewDescription is the driver's declared shape with its member bindings
// resolved to the same attribute keys used by TreeCut.Read.
type ViewDescription struct {
	Root    string       `json:"root"`
	Records []ViewRecord `json:"records"`
}

// ViewRecord is a named record in a view descriptor.
type ViewRecord struct {
	Name   string      `json:"name"`
	Fields []ViewField `json:"fields"`
}

// ViewField carries the driver-declared name, type and unit, and the key of
// the member that supplies its value.
type ViewField struct {
	Name   string          `json:"name"`
	Type   driver.ViewType `json:"type"`
	Unit   string          `json:"unit,omitempty"`
	Member AttrRef         `json:"member"`
}

type viewRecord struct {
	key        Key
	shape      ViewDescription
	namespace  driver.NamespaceID
	instance   InstanceRef
	generation uint64
	withdrawn  bool
}

// View registers a typed view at the calling instance's root. Its fields bind
// to readable members already registered by this instance in this generation.
func (b *InstanceRegistry) View(spec driver.ViewSpec) error {
	b.r.mu.Lock()
	defer b.r.mu.Unlock()
	if err := b.live(); err != nil {
		return err
	}
	names := make(map[string]struct{}, len(spec.Records))
	for _, record := range spec.Records {
		if _, ok := names[record.Name]; ok {
			return fmt.Errorf("%w: view record %q", ErrDuplicate, record.Name)
		}
		names[record.Name] = struct{}{}
	}
	if _, ok := names[spec.Root]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownViewRecord, spec.Root)
	}
	for _, held := range b.r.views {
		if held.namespace != b.ns || held.instance != b.instance || (held.withdrawn && held.generation < b.generation) {
			continue
		}
		for _, record := range held.shape.Records {
			if _, ok := names[record.Name]; ok {
				return fmt.Errorf("%w: view record %q", ErrDuplicate, record.Name)
			}
		}
	}
	shape := ViewDescription{Root: spec.Root, Records: make([]ViewRecord, 0, len(spec.Records))}
	for _, record := range spec.Records {
		out := ViewRecord{Name: record.Name, Fields: make([]ViewField, 0, len(record.Fields))}
		for _, field := range record.Fields {
			if _, ok := names[field.Type.Name]; field.Type.Record && !ok {
				return fmt.Errorf("%w: %q at %s.%s", ErrUnknownViewRecord, field.Type.Name, record.Name, field.Name)
			}
			member, ok := b.viewMember(field.Member)
			if !ok {
				return fmt.Errorf("%w: %+v at %s.%s", ErrUnknownViewMember, field.Member, record.Name, field.Name)
			}
			out.Fields = append(out.Fields, ViewField{Name: field.Name, Type: field.Type, Unit: field.Unit, Member: member})
		}
		shape.Records = append(shape.Records, out)
	}
	segment, err := EncodeSegment(string(b.ns), spec.Root)
	if err != nil {
		return err
	}
	key := Key{Node: b.root, Facet: keywordViews, Domain: KeywordNative, Segment: segment}
	b.r.views[key.String()] = &viewRecord{key: key, shape: shape, namespace: b.ns, instance: b.instance, generation: b.generation}
	b.r.record(ChangeView, key.String())
	return nil
}

// viewMember resolves a driver member key under the registry lock.
func (b *InstanceRegistry) viewMember(binding driver.MemberKey) (AttrRef, bool) {
	if binding.Namespace != b.ns || binding.Instance != b.instance.Instance {
		return "", false
	}
	segment, err := NativeSegment(binding.Namespace, binding.Local)
	if err != nil {
		return "", false
	}
	// The node ID is the canonical path without the leading slash, and the
	// lookup only accepts keys of actual attribute registrations.
	ref := AttrRef("/" + string(binding.Node) + "/attributes/native/" + segment)
	held, ok := b.r.members[string(ref)]
	return ref, ok && held.instance == b.instance && !held.withdrawn && held.generation == b.generation
}

func parseViewKey(s string) (Key, error) {
	node, rest, err := parseNode(s)
	if err != nil {
		return Key{}, err
	}
	if level, _ := node.Level(); level != LevelInstance || len(rest) != 3 || rest[0] != string(keywordViews) || rest[1] != string(KeywordNative) {
		return Key{}, keyError("a view key is an instance path, views, native and a segment")
	}
	if _, _, err := ParseNativeSegment(rest[2]); err != nil {
		return Key{}, err
	}
	return Key{Node: node, Facet: keywordViews, Domain: KeywordNative, Segment: rest[2]}, nil
}

func (c *TreeCut) viewDescriptor(view viewRecord) Descriptor {
	shape := view.shape
	shape.Records = slices.Clone(shape.Records)
	for i := range shape.Records {
		shape.Records[i].Fields = slices.Clone(shape.Records[i].Fields)
	}
	ref := view.key.String()
	d := Descriptor{
		Ref: Ref(ref), Kind: EntryView, Domain: KeywordNative, Namespace: view.namespace, Instance: view.instance,
		Local: shape.Root, State: c.tree.state(view.instance), View: &shape,
	}
	registry := c.tree.Registry
	registry.mu.Lock()
	live, ok := registry.views[ref]
	d.Withdrawn = !ok || live.withdrawn || live.generation != view.generation
	registry.mu.Unlock()
	return d
}
