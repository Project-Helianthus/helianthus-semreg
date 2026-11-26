package tree

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Registration errors. Each is wrapped with the detail of the rejection.
var (
	ErrNamespaceConflict = errors.New("namespace is reserved by another owner")
	ErrNotReserved       = errors.New("namespace or instance is not reserved")
	ErrDuplicate         = errors.New("key is already registered")
	ErrUnknownParent     = errors.New("parent node is not registered")
	ErrOutsideInstance   = errors.New("node lies outside the calling instance's subtree")
	ErrFenced            = errors.New("generation is fenced")
)

// Observation is what a driver's detached retained read supplies for one
// member: its encoding as its schema states, the raw value beside it, the
// verification level, the receive time, the freshness and age, the snapshot
// revision where the driver has one, whether the value is derived, and the
// native name of a value the documentation does not define. Nothing here is
// interpreted by the tree.
type Observation struct {
	Encoded      []byte
	Raw          []byte
	Verification string
	ReceivedAt   *semreg.TimePoint
	Freshness    semreg.Freshness
	Age          *semreg.Uint64
	Revision     *semreg.Uint64
	Derived      bool
	Name         string
	Absent       bool
}

// ObservedReader reads one member's retained observation. It never sends on a
// bus.
type ObservedReader func(context.Context) (Observation, error)

// ChangeKind is the kind of a registry change.
type ChangeKind string

// ChangeKind values.
const (
	ChangeNode      ChangeKind = "node"
	ChangeMember    ChangeKind = "member"
	ChangeMethod    ChangeKind = "method"
	ChangeView      ChangeKind = "view"
	ChangeWithdrawn ChangeKind = "withdrawn"
)

// Change is one ordered registry change. Position is its place in the
// incarnation's history.
type Change struct {
	Position uint64
	Kind     ChangeKind
	Ref      Ref
}

type instanceRecord struct {
	ref        InstanceRef
	namespaces map[driver.NamespaceID]struct{}
	fenced     uint64
	hasFence   bool
}

type nodeRecord struct {
	path       NodePath
	spec       *driver.NodeSpec
	namespace  driver.NamespaceID
	instance   InstanceRef
	generation uint64
	withdrawn  bool
}

type memberRecord struct {
	key        Key
	spec       driver.MemberSpec
	read       ObservedReader
	namespace  driver.NamespaceID
	instance   InstanceRef
	generation uint64
	withdrawn  bool
}

type methodRecord struct {
	key        Key
	spec       driver.MethodSpec
	call       driver.NativeMethod
	namespace  driver.NamespaceID
	instance   InstanceRef
	generation uint64
	withdrawn  bool
}

// RegistryConfig is the configuration of a Registry.
type RegistryConfig struct {
	Installation string
	Bridge       driver.BridgeID
	Incarnation  string
	Lifecycle    LifecycleSource
}

// Registry holds the namespace reservations and every registration of the tree.
type Registry struct {
	mu        sync.Mutex
	cfg       RegistryConfig
	reserved  map[driver.NamespaceID]driver.Namespace
	instances map[InstanceRef]*instanceRecord
	nodes     map[string]*nodeRecord
	members   map[string]*memberRecord
	methods   map[string]*methodRecord
	views     map[string]*viewRecord
	log       []Change
	stop      func()
}

// NewRegistry returns an empty registry with the installation node, and
// subscribes to the lifecycle source's generation-fence callbacks.
func NewRegistry(cfg RegistryConfig) (*Registry, error) {
	switch {
	case cfg.Installation == "":
		return nil, errors.New("installation is required")
	case cfg.Incarnation == "":
		return nil, errors.New("incarnation is required")
	case cfg.Lifecycle == nil:
		return nil, errors.New("lifecycle source is required")
	}
	if err := cfg.Bridge.Validate(); err != nil {
		return nil, err
	}
	r := &Registry{
		cfg: cfg, reserved: map[driver.NamespaceID]driver.Namespace{}, instances: map[InstanceRef]*instanceRecord{},
		nodes: map[string]*nodeRecord{}, members: map[string]*memberRecord{}, methods: map[string]*methodRecord{}, views: map[string]*viewRecord{},
	}
	root := NodePath{Installation: cfg.Installation}
	r.nodes[root.String()] = &nodeRecord{path: root}
	r.stop = cfg.Lifecycle.OnFence(r.Fence)
	return r, nil
}

// Close ends the subscription to the lifecycle source.
func (r *Registry) Close() {
	if r.stop != nil {
		r.stop()
	}
}

func (r *Registry) record(kind ChangeKind, ref string) {
	r.log = append(r.log, Change{Position: uint64(len(r.log)) + 1, Kind: kind, Ref: Ref(ref)})
}

// Reserve reserves the namespace for its owner at planning time and creates the
// instance nodes of the listed instances. A second owner for the same
// NamespaceID is a planning error; the same owner may add instances.
func (r *Registry) Reserve(ns driver.Namespace, instances []driver.InstanceKey) error {
	if err := ns.Validate(); err != nil {
		return err
	}
	for i, instance := range instances {
		if err := instance.Validate(); err != nil {
			return fmt.Errorf("instances[%d]: %w", i, err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if held, ok := r.reserved[ns.Key]; ok && held.Owner != ns.Owner {
		return fmt.Errorf("%w: %q is held by %q", ErrNamespaceConflict, ns.Key, held.Owner)
	}
	r.reserved[ns.Key] = ns
	for _, instance := range instances {
		ref := InstanceRef{Driver: ns.Owner, Instance: instance}
		record := r.instances[ref]
		if record == nil {
			record = &instanceRecord{ref: ref, namespaces: map[driver.NamespaceID]struct{}{}}
			r.instances[ref] = record
			path := NodePath{Installation: r.cfg.Installation, Driver: ns.Owner, Instance: instance}
			r.nodes[path.String()] = &nodeRecord{path: path, namespace: ns.Key, instance: ref}
			r.record(ChangeNode, path.String())
		}
		record.namespaces[ns.Key] = struct{}{}
	}
	return nil
}

// Bind returns the registry of one instance's session at one generation. It is
// rooted at the instance and is what Session.Native returns.
func (r *Registry) Bind(ns driver.NamespaceID, instance driver.InstanceKey, generation uint64) (*InstanceRegistry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reservation, ok := r.reserved[ns]
	if !ok {
		return nil, fmt.Errorf("%w: namespace %q", ErrNotReserved, ns)
	}
	ref := InstanceRef{Driver: reservation.Owner, Instance: instance}
	record := r.instances[ref]
	if record == nil {
		return nil, fmt.Errorf("%w: instance %q of %q", ErrNotReserved, instance, reservation.Owner)
	}
	if _, ok := record.namespaces[ns]; !ok {
		return nil, fmt.Errorf("%w: instance %q is not planned under %q", ErrNotReserved, instance, ns)
	}
	if record.hasFence && generation <= record.fenced {
		return nil, fmt.Errorf("%w: generation %d", ErrFenced, generation)
	}
	root := NodePath{Installation: r.cfg.Installation, Driver: ref.Driver, Instance: ref.Instance}
	return &InstanceRegistry{r: r, ns: ns, instance: ref, generation: generation, root: root}, nil
}

// Fence is the generation-fence callback: it withdraws every registration the
// instance made at or before the generation. The descriptors stay listed. A
// registration from a fenced generation that arrives later is discarded.
func (r *Registry) Fence(f Fence) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.instances[f.Instance]
	if record == nil {
		return
	}
	if !record.hasFence || f.Generation > record.fenced {
		record.fenced, record.hasFence = f.Generation, true
	}
	changed := false
	for _, n := range r.nodes {
		if n.instance == f.Instance && n.spec != nil && n.generation <= record.fenced && !n.withdrawn {
			n.withdrawn, changed = true, true
		}
	}
	for _, m := range r.members {
		if m.instance == f.Instance && m.generation <= record.fenced && !m.withdrawn {
			m.withdrawn, m.read, changed = true, nil, true
		}
	}
	for _, m := range r.methods {
		if m.instance == f.Instance && m.generation <= record.fenced && !m.withdrawn {
			m.withdrawn, m.call, changed = true, nil, true
		}
	}
	for _, v := range r.views {
		if v.instance == f.Instance && v.generation <= record.fenced && !v.withdrawn {
			v.withdrawn, changed = true, true
		}
	}
	if changed {
		root := NodePath{Installation: r.cfg.Installation, Driver: f.Instance.Driver, Instance: f.Instance.Instance}
		r.record(ChangeWithdrawn, root.String())
	}
}

// Position is the position of the newest change.
func (r *Registry) Position() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return uint64(len(r.log))
}

// Since returns the changes after a position, in order.
func (r *Registry) Since(position uint64) ([]Change, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if position > uint64(len(r.log)) {
		return nil, false
	}
	return append([]Change(nil), r.log[position:]...), true
}

// InstanceRegistry is the driver.NativeRegistry of one instance session. It is
// bound to its instance's root node and generation.
type InstanceRegistry struct {
	r          *Registry
	ns         driver.NamespaceID
	instance   InstanceRef
	generation uint64
	root       NodePath
}

var _ driver.NativeRegistry = (*InstanceRegistry)(nil)

// Root is the NodeRef of the calling instance's own node, where its
// procedures are registered.
func (b *InstanceRegistry) Root() driver.NodeRef {
	return driver.NodeRef{Bridge: b.r.cfg.Bridge, ID: b.root.NodeID()}
}

func (b *InstanceRegistry) live() error {
	record := b.r.instances[b.instance]
	if record != nil && record.hasFence && b.generation <= record.fenced {
		return fmt.Errorf("%w: generation %d is discarded", ErrFenced, b.generation)
	}
	return nil
}

func (b *InstanceRegistry) resolve(ref driver.NodeRef) (NodePath, error) {
	if err := ref.Validate(); err != nil {
		return NodePath{}, err
	}
	if ref.Bridge != b.r.cfg.Bridge {
		return NodePath{}, fmt.Errorf("%w: bridge %q", ErrUnknownParent, ref.Bridge)
	}
	path, err := ParseNodePath("/" + string(ref.ID))
	if err != nil {
		return NodePath{}, fmt.Errorf("%w: %v", ErrUnknownParent, err)
	}
	if _, ok := b.r.nodes[path.String()]; !ok {
		return NodePath{}, fmt.Errorf("%w: %s", ErrUnknownParent, path.String())
	}
	if !path.Within(b.root) {
		return NodePath{}, fmt.Errorf("%w: %s", ErrOutsideInstance, path.String())
	}
	return path, nil
}

// Node registers one node under the declared parent, or under the instance node
// when the spec names none.
func (b *InstanceRegistry) Node(spec driver.NodeSpec) (driver.NodeRef, error) {
	if err := spec.Validate(); err != nil {
		return driver.NodeRef{}, err
	}
	b.r.mu.Lock()
	defer b.r.mu.Unlock()
	if err := b.live(); err != nil {
		return driver.NodeRef{}, err
	}
	parent := b.root
	if spec.Parent != nil {
		var err error
		if parent, err = b.resolve(*spec.Parent); err != nil {
			return driver.NodeRef{}, err
		}
	}
	child, err := parent.Child(spec.Local)
	if err != nil {
		return driver.NodeRef{}, err
	}
	ref := driver.NodeRef{Bridge: b.r.cfg.Bridge, ID: child.NodeID()}
	if err := ref.Validate(); err != nil {
		return driver.NodeRef{}, err
	}
	if held, ok := b.r.nodes[child.String()]; ok && !(held.withdrawn && held.generation < b.generation) {
		return driver.NodeRef{}, fmt.Errorf("%w: %s", ErrDuplicate, child.String())
	}
	stored := spec
	stored.Parent, stored.Subject = cloneNodeRef(spec.Parent), cloneNodeRef(spec.Subject)
	b.r.nodes[child.String()] = &nodeRecord{path: child, spec: &stored, namespace: b.ns, instance: b.instance, generation: b.generation}
	b.r.record(ChangeNode, child.String())
	return ref, nil
}

// ReadMember registers a readable native member whose reader returns the
// member's encoding.
func (b *InstanceRegistry) ReadMember(ref driver.NodeRef, spec driver.MemberSpec, read driver.NativeReader) error {
	if read == nil {
		return errors.New("reader is required")
	}
	return b.ObserveMember(ref, spec, func(ctx context.Context) (Observation, error) {
		encoded, err := read(ctx)
		return Observation{Encoded: encoded}, err
	})
}

// ObserveMember registers a readable native member whose reader returns its
// full observation.
func (b *InstanceRegistry) ObserveMember(ref driver.NodeRef, spec driver.MemberSpec, read ObservedReader) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if read == nil {
		return errors.New("reader is required")
	}
	b.r.mu.Lock()
	defer b.r.mu.Unlock()
	if err := b.live(); err != nil {
		return err
	}
	node, err := b.resolve(ref)
	if err != nil {
		return err
	}
	segment, err := NativeSegment(b.ns, spec.Local)
	if err != nil {
		return err
	}
	key := Key{Node: node, Facet: KeywordAttributes, Domain: KeywordNative, Segment: segment}
	if held, ok := b.r.members[key.String()]; ok && !(held.withdrawn && held.generation < b.generation) {
		return fmt.Errorf("%w: %s", ErrDuplicate, key.String())
	}
	spec.Bounds = cloneLimits(spec.Bounds)
	b.r.members[key.String()] = &memberRecord{key: key, spec: spec, read: read, namespace: b.ns, instance: b.instance, generation: b.generation}
	b.r.record(ChangeMember, key.String())
	return nil
}

// Method registers a native method or procedure on a node. A procedure is a
// method on the instance node or a transport node.
func (b *InstanceRegistry) Method(ref driver.NodeRef, spec driver.MethodSpec, call driver.NativeMethod) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if call == nil {
		return errors.New("method is required")
	}
	b.r.mu.Lock()
	defer b.r.mu.Unlock()
	if err := b.live(); err != nil {
		return err
	}
	node, err := b.resolve(ref)
	if err != nil {
		return err
	}
	segment, err := NativeSegment(b.ns, spec.Local)
	if err != nil {
		return err
	}
	key := Key{Node: node, Facet: KeywordMethods, Domain: KeywordNative, Segment: segment}
	if held, ok := b.r.methods[key.String()]; ok && !(held.withdrawn && held.generation < b.generation) {
		return fmt.Errorf("%w: %s", ErrDuplicate, key.String())
	}
	b.r.methods[key.String()] = &methodRecord{key: key, spec: spec, call: call, namespace: b.ns, instance: b.instance, generation: b.generation}
	b.r.record(ChangeMethod, key.String())
	return nil
}

// registered is a detached copy of every registration, taken at one moment.
type registered struct {
	nodes   map[string]nodeRecord
	members map[string]memberRecord
	methods map[string]methodRecord
	views   map[string]viewRecord
}

// detach copies every registration together with the log position of the same
// moment: each registration is either in the copy or after the position.
func (r *Registry) detach() (registered, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := registered{nodes: map[string]nodeRecord{}, members: map[string]memberRecord{}, methods: map[string]methodRecord{}, views: map[string]viewRecord{}}
	for k, v := range r.nodes {
		out.nodes[k] = *v
	}
	for k, v := range r.members {
		out.members[k] = *v
	}
	for k, v := range r.methods {
		out.methods[k] = *v
	}
	for k, v := range r.views {
		out.views[k] = *v
	}
	return out, uint64(len(r.log))
}

func (r *Registry) member(key string) (memberRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[key]
	if !ok {
		return memberRecord{}, false
	}
	return *m, true
}

func (r *Registry) method(key string) (methodRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.methods[key]
	if !ok {
		return methodRecord{}, false
	}
	return *m, true
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cloneNodeRef copies a node reference so the copy shares no pointer with the
// registration it came from or is stored for.
func cloneNodeRef(in *driver.NodeRef) *driver.NodeRef {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// cloneLimits copies value limits, the quantities they point to and the evidence
// they carry, so the copy shares no pointer or slice with the original.
func cloneLimits(in *driver.ValueLimits) *driver.ValueLimits {
	if in == nil {
		return nil
	}
	out := *in
	out.Min, out.Max, out.Step = cloneQuantity(in.Min), cloneQuantity(in.Max), cloneQuantity(in.Step)
	if in.Evidence != nil {
		out.Evidence = append([]semreg.EvidenceRef{}, in.Evidence...)
	}
	return &out
}

func cloneQuantity(in *semreg.Quantity) *semreg.Quantity {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
