package tree

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// ErrUnavailable is wrapped by a refusal to use a withdrawn member or method.
var ErrUnavailable = errors.New("unavailable")

// ErrUsed is returned by Call for a prepared call that was already called.
var ErrUsed = errors.New("prepared call was already called")

// CutID identifies one capture of the tree. Two captures never share one, also
// when their graph cuts share a digest.
type CutID string

// NodeRef refers to a node by bridge and resource id.
type NodeRef = driver.NodeRef

// Ref is the path of a node or the key of an attribute, method or view.
type Ref string

// AttrRef is the key of an attribute.
type AttrRef = Ref

// MethodRef is the key of a method.
type MethodRef = Ref

// Cursor is a position in the tree's history: the registry's, within one
// incarnation, and the semantic commit cursor of the cut source.
type Cursor struct {
	Incarnation string        `json:"incarnation"`
	Native      uint64        `json:"native"`
	Semantic    driver.Cursor `json:"semantic"`
}

// CutOptions are the options of Capture.
type CutOptions struct {
	Filter driver.Filter
}

// Changes is the result of Resume: the registry changes and the semantic
// commits after the cursor, in order, or an explicit resync.
type Changes struct {
	Cursor  Cursor
	Native  []Change
	Commits []driver.SemanticCut
	Resync  bool
}

// EntryKind is the kind of a listed entry.
type EntryKind string

// EntryKind values.
const (
	EntryNode      EntryKind = "node"
	EntryAttribute EntryKind = "attribute"
	EntryMethod    EntryKind = "method"
	EntryView      EntryKind = "view"
)

// Filter narrows a List by what the descriptors declare. An unset member does
// not filter.
type Filter struct {
	Instance *driver.InstanceKey
	Device   *driver.NodeID
	Kind     EntryKind
	Schema   driver.SchemaRef
	Unit     semreg.DefinitionID
	Contract driver.SchemaRef
}

// Page is a page request. Size is the caller's; no maximum is added.
type Page struct {
	Size   int
	Token  string
	Filter Filter
}

// Descriptor is the typed descriptor of a node, attribute, method or view.
type Descriptor struct {
	Ref        Ref
	Kind       EntryKind
	Domain     Domain
	Level      Level
	Namespace  driver.NamespaceID
	Instance   InstanceRef
	NodeKind   semreg.DefinitionID
	Contract   driver.SchemaRef
	Subject    *driver.NodeRef
	Local      string
	Schema     driver.SchemaRef
	Unit       semreg.DefinitionID
	Mode       driver.AccessMode
	Visibility string
	Channel    string
	Scope      driver.ScopeID
	Bounds     *driver.ValueLimits
	State      *InstanceState
	Withdrawn  bool
	View       *ViewDescription `json:",omitempty"`
}

// Entries is one page of a listing, pinned to one cut.
type Entries struct {
	Cut     CutID
	Entries []Descriptor
	Next    string
}

// Status is the status of a read value.
type Status string

// Status values. Pinned reads are current, stale or absent; a read that is not
// part of the cut is live_only, changed or unavailable.
const (
	StatusCurrent     Status = "current"
	StatusStale       Status = "stale"
	StatusAbsent      Status = "absent"
	StatusLiveOnly    Status = "live_only"
	StatusChanged     Status = "changed"
	StatusUnavailable Status = "unavailable"
)

// ReadOptions are the options of Read.
type ReadOptions struct{}

// Value is the envelope of a read attribute. A stale value carries its age and
// no sample. An unavailable value carries the instance state.
type Value struct {
	Ref          AttrRef
	Cut          CutID
	Status       Status
	Pinned       bool
	Encoded      []byte
	Raw          []byte
	Unit         semreg.DefinitionID
	Verification string
	ReceivedAt   *semreg.TimePoint
	Freshness    semreg.Freshness
	Age          *semreg.Uint64
	Revision     *semreg.Uint64
	Derived      bool
	Name         string
	Instance     *InstanceState
}

// Prepared is a prepared call. It is called at most once.
type Prepared struct {
	ID     PreparedID
	Method MethodRef
	Cut    CutID
}

// Receipt is the one receipt of a call. A semantic call carries the operation
// result the dispatcher returned, with its compound execution.
type Receipt struct {
	ID     ReceiptID
	Method MethodRef
	Result *driver.OperationResult
}

// Outcome is the outcome of a receipt: the operation result of a semantic
// method, or the encoded result or the error of a native one.
type Outcome struct {
	Receipt   ReceiptID
	Operation *driver.OperationResult
	Native    []byte
	Error     string
}

// Config configures a Tree.
type Config struct {
	Registry   RegistryConfig
	Cuts       CutSource
	Operations OperationPort
}

// Tree is the runtime tree.
type Tree struct {
	cfg      Config
	Registry *Registry

	mu       sync.Mutex
	sequence uint64
	cuts     map[CutID]*TreeCut
	prepared map[PreparedID]*preparedCall
	outcomes map[ReceiptID]Outcome
	// receipts lists, per cut, the receipts of the calls prepared on that cut.
	receipts map[CutID][]ReceiptID
	// dispatched maps a receipt id the tree issued to the id the dispatcher
	// issued; the two are never the same namespace.
	dispatched map[ReceiptID]ReceiptID
}

type preparedCall struct {
	method MethodRef
	cut    CutID
	native bool
	port   PortPrepared
	used   bool
	// generation is the generation of the native registration the cut holds.
	generation uint64
}

// New returns a Tree over the three injected ports.
func New(cfg Config) (*Tree, error) {
	if cfg.Cuts == nil || cfg.Operations == nil {
		return nil, errors.New("cut source and operation port are required")
	}
	registry, err := NewRegistry(cfg.Registry)
	if err != nil {
		return nil, err
	}
	return &Tree{cfg: cfg, Registry: registry, cuts: map[CutID]*TreeCut{}, prepared: map[PreparedID]*preparedCall{}, outcomes: map[ReceiptID]Outcome{}, receipts: map[CutID][]ReceiptID{}, dispatched: map[ReceiptID]ReceiptID{}}, nil
}

// Capture reads one cut of the semantic graph and the registrations of the tree
// together. Native snapshot revisions the drivers supply are pinned with the
// cut's membership.
func (t *Tree) Capture(ctx context.Context, options CutOptions) (*TreeCut, error) {
	view, err := t.cfg.Cuts.Capture(ctx, options.Filter)
	if err != nil {
		return nil, err
	}
	// The capture is answered from a body it retained itself, with its own
	// omission vector, not from the source's shared store.
	// The member snapshots and setting routes are copied too, so a later edit of
	// what the source handed over leaves this capture's reads unchanged.
	if view, err = detachView(view); err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.sequence++
	id := CutID(fmt.Sprintf("%s:%d", view.Ref.CutID, t.sequence))
	t.mu.Unlock()
	reg, position := t.Registry.detach()
	cut := &TreeCut{
		tree: t, id: id, view: view, reg: reg,
		cursor: Cursor{Incarnation: t.cfg.Registry.Incarnation, Native: position, Semantic: view.Cursor},
		pinned: map[string]Observation{}, baseline: map[string]Observation{},
	}
	for key, member := range cut.reg.members {
		if member.read == nil {
			continue
		}
		obs, err := observe(ctx, member.read)
		if err != nil {
			continue
		}
		if obs.Revision != nil {
			cut.pinned[key] = obs
		} else {
			cut.baseline[key] = obs
		}
	}
	t.mu.Lock()
	t.cuts[id] = cut
	t.mu.Unlock()
	return cut, nil
}

// Resume returns the changes after a cursor while its history is available,
// also when the cut it came from has expired, or an explicit resync.
func (t *Tree) Resume(ctx context.Context, cursor Cursor, filter driver.Filter) (Changes, error) {
	semantic, err := t.cfg.Cuts.Resume(ctx, cursor.Semantic, filter)
	if err != nil {
		return Changes{}, err
	}
	position := t.Registry.Position()
	next := Cursor{Incarnation: t.cfg.Registry.Incarnation, Native: position, Semantic: semantic.Cursor}
	if cursor.Incarnation != t.cfg.Registry.Incarnation || semantic.Resync {
		return Changes{Cursor: next, Resync: true}, nil
	}
	native, ok := t.Registry.Since(cursor.Native)
	if !ok {
		return Changes{Cursor: next, Resync: true}, nil
	}
	return Changes{Cursor: next, Native: native, Commits: semantic.Commits}, nil
}

func (t *Tree) openCut(id CutID) (*TreeCut, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cut, ok := t.cuts[id]
	if !ok {
		return nil, treeError(semreg.DanglingReference, "cut is not open")
	}
	return cut, nil
}

// Release closes the capture.
func (t *Tree) Release(id CutID) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.cuts[id]; !ok {
		return treeError(semreg.DanglingReference, "cut is not open")
	}
	delete(t.cuts, id)
	return nil
}

// Prepare resolves the method in the cut and prepares its call. Nothing is sent.
func (t *Tree) Prepare(ctx context.Context, id CutID, method MethodRef, input Input) (Prepared, error) {
	cut, err := t.openCut(id)
	if err != nil {
		return Prepared{}, err
	}
	key, err := ParseKey(string(method))
	if err != nil {
		return Prepared{}, err
	}
	if key.Facet != KeywordMethods {
		return Prepared{}, keyError("%s is not a method", method)
	}
	call := &preparedCall{method: method, cut: id}
	if key.Domain == KeywordNative {
		held, ok := cut.reg.methods[key.String()]
		if !ok {
			return Prepared{}, treeError(semreg.DanglingReference, "method is not in the cut")
		}
		live, ok := t.Registry.method(key.String())
		if !ok || live.withdrawn {
			return Prepared{}, fmt.Errorf("%w: method %s is withdrawn (%s)", ErrUnavailable, method, t.stateText(live.instance))
		}
		if live.generation != held.generation {
			return Prepared{}, fmt.Errorf("%w: method %s was replaced by another generation", ErrUnavailable, method)
		}
		call.native = true
		call.generation = held.generation
		public := t.register(call)
		cut.nativeInput(public, input.Encoded)
		return Prepared{ID: public, Method: method, Cut: id}, nil
	}
	definition, err := ParseDefinitionSegment(key.Segment)
	if err != nil {
		return Prepared{}, err
	}
	if !cut.hasOperation(definition) {
		return Prepared{}, treeError(semreg.DanglingReference, "operation is not in the cut")
	}
	if input.Request == nil {
		return Prepared{}, treeError(semreg.MissingMember, "a semantic method takes an operation request")
	}
	port, err := t.cfg.Operations.Prepare(ctx, cut.view.Ref, definition, input)
	if err != nil {
		return Prepared{}, err
	}
	call.port = port
	public := t.register(call)
	return Prepared{ID: public, Method: method, Cut: id}, nil
}

// register stores a prepared call under a handle the tree owns, so a handle the
// dispatcher issues can never collide with another prepared call.
func (t *Tree) register(call *preparedCall) PreparedID {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sequence++
	public := PreparedID(fmt.Sprintf("prepared-%d", t.sequence))
	t.prepared[public] = call
	return public
}

// claim moves a prepared call from unused to used. Only one caller wins.
func (t *Tree) claim(call *preparedCall) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if call.used {
		return ErrUsed
	}
	call.used = true
	return nil
}

// Call calls a prepared method once. A semantic call without the finite
// authority of a deadline is refused and the dispatcher is not called; the
// tree has no call-bound path.
func (t *Tree) Call(ctx context.Context, prepared Prepared, options CallOptions) (Receipt, error) {
	t.mu.Lock()
	call, ok := t.prepared[prepared.ID]
	if ok && call.used {
		t.mu.Unlock()
		return Receipt{}, ErrUsed
	}
	t.mu.Unlock()
	if !ok {
		return Receipt{}, treeError(semreg.DanglingReference, "prepared call")
	}
	if call.native {
		return t.callNative(ctx, prepared, call)
	}
	if call.port.Request.Timeout <= 0 && options.Timeout <= 0 {
		return Receipt{}, treeError(semreg.AuthorityMissing, "a prepared call needs a finite deadline")
	}
	if err := t.claim(call); err != nil {
		return Receipt{}, err
	}
	receipt, err := t.cfg.Operations.Call(ctx, call.port, options)
	if err != nil {
		return Receipt{}, err
	}
	t.mu.Lock()
	t.sequence++
	public := ReceiptID(fmt.Sprintf("receipt-%d", t.sequence))
	t.dispatched[public] = receipt.ID
	t.receipts[call.cut] = append(t.receipts[call.cut], public)
	t.mu.Unlock()
	return Receipt{ID: public, Method: call.method, Result: receipt.Result}, nil
}

func (t *Tree) callNative(ctx context.Context, prepared Prepared, call *preparedCall) (Receipt, error) {
	cut, err := t.openCut(call.cut)
	if err != nil {
		return Receipt{}, err
	}
	key, err := ParseKey(string(call.method))
	if err != nil {
		return Receipt{}, err
	}
	live, ok := t.Registry.method(key.String())
	if !ok || live.call == nil || live.withdrawn {
		return Receipt{}, fmt.Errorf("%w: method %s is withdrawn (%s)", ErrUnavailable, call.method, t.stateText(live.instance))
	}
	if live.generation != call.generation {
		return Receipt{}, fmt.Errorf("%w: method %s was replaced by another generation", ErrUnavailable, call.method)
	}
	if err := t.claim(call); err != nil {
		return Receipt{}, err
	}
	t.mu.Lock()
	t.sequence++
	id := ReceiptID(fmt.Sprintf("receipt-%d", t.sequence))
	t.mu.Unlock()
	result, callErr := live.call(ctx, cut.takeNativeInput(prepared.ID))
	// The method may return a buffer it reuses; the receipt keeps its own copy.
	outcome := Outcome{Receipt: id, Native: cloneBytes(result)}
	if callErr != nil {
		outcome.Error = callErr.Error()
	}
	t.mu.Lock()
	t.outcomes[id] = outcome
	t.receipts[call.cut] = append(t.receipts[call.cut], id)
	t.mu.Unlock()
	return Receipt{ID: id, Method: call.method}, nil
}

// Outcome returns the outcome of a receipt.
func (t *Tree) Outcome(ctx context.Context, id ReceiptID) (Outcome, error) {
	t.mu.Lock()
	outcome, ok := t.outcomes[id]
	port, dispatched := t.dispatched[id]
	t.mu.Unlock()
	if ok {
		// A caller's edit of what it was handed never reaches the stored outcome.
		outcome.Native = cloneBytes(outcome.Native)
		return outcome, nil
	}
	if !dispatched {
		return Outcome{}, treeError(semreg.DanglingReference, "receipt")
	}
	result, err := t.cfg.Operations.Outcome(ctx, port)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Receipt: id, Operation: &result}, nil
}

func (t *Tree) stateText(instance InstanceRef) string {
	state, err := t.cfg.Registry.Lifecycle.State(instance)
	if err != nil {
		return "state unknown"
	}
	return "instance state " + state.Name
}

func (t *Tree) state(instance InstanceRef) *InstanceState {
	if instance == (InstanceRef{}) {
		return nil
	}
	state, err := t.cfg.Registry.Lifecycle.State(instance)
	if err != nil {
		return nil
	}
	return &state
}

func treeError(id semreg.ErrorID, detail string) error {
	return &semreg.Error{ID: id, Detail: "tree: " + detail}
}

// TreeCut is one capture: the graph cut, the registrations at that moment and
// the native observations pinned with them.
type TreeCut struct {
	tree     *Tree
	id       CutID
	cursor   Cursor
	view     CutView
	reg      registered
	pinned   map[string]Observation
	baseline map[string]Observation

	mu     sync.Mutex
	inputs map[PreparedID][]byte
	closed bool
}

// ID returns the capture's identifier.
func (c *TreeCut) ID() CutID { return c.id }

// Cursor returns the position the capture was taken at.
func (c *TreeCut) Cursor() Cursor { return c.cursor }

// Close releases the capture.
func (c *TreeCut) Close() error { return c.tree.Release(c.id) }

func (c *TreeCut) nativeInput(id PreparedID, encoded []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inputs == nil {
		c.inputs = map[PreparedID][]byte{}
	}
	c.inputs[id] = append([]byte(nil), encoded...)
}

func (c *TreeCut) takeNativeInput(id PreparedID) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	encoded := c.inputs[id]
	delete(c.inputs, id)
	return encoded
}

func (c *TreeCut) hasOperation(ref semreg.DefinitionRef) bool {
	for _, operation := range c.operations() {
		if operation == ref {
			return true
		}
	}
	return false
}

func (c *TreeCut) operations() []semreg.DefinitionRef {
	var out []semreg.DefinitionRef
	for _, element := range c.view.Catalog.Elements() {
		if element.ElementKind != graph.KindOperation || element.Disposition != graph.DispositionMember {
			continue
		}
		if ref, ok := element.Ref.DefinitionRef(); ok {
			out = append(out, ref)
		}
	}
	return out
}

func (c *TreeCut) factKeys() []semreg.FactKey {
	seen := map[string]semreg.FactKey{}
	for _, member := range c.view.Members {
		if c.excluded(member.AssetID) {
			continue
		}
		for _, fact := range member.Facts {
			canonical, err := semreg.CanonicalJSON(fact.Key)
			if err == nil {
				seen[string(canonical)] = fact.Key
			}
		}
	}
	out := make([]semreg.FactKey, 0, len(seen))
	for _, k := range sortedKeys(seen) {
		out = append(out, seen[k])
	}
	return out
}

func (c *TreeCut) excluded(asset semreg.AssetID) bool {
	for _, excluded := range c.view.Cut.Excluded() {
		if excluded == asset {
			return true
		}
	}
	return false
}

// IndexCategories are the installation node's indexes of the cut.
var IndexCategories = []string{"catalog", "resources", "relations", "planes", "lenses", "capabilities", "methods", "facts", "sources", "outcomes"}

func (c *TreeCut) installation() NodePath {
	return NodePath{Installation: c.tree.cfg.Registry.Installation}
}

func (c *TreeCut) indexKey(category string) Key {
	segment, _ := EncodeSegment(IndexPart, category)
	return Key{Node: c.installation(), Facet: KeywordAttributes, Domain: KeywordSem, Segment: segment}
}

func (c *TreeCut) nodeDescriptor(path NodePath) (Descriptor, bool) {
	record, ok := c.reg.nodes[path.String()]
	if !ok {
		return Descriptor{}, false
	}
	level, _ := path.Level()
	d := Descriptor{Ref: Ref(path.String()), Kind: EntryNode, Level: level, Namespace: record.namespace, Instance: record.instance}
	if record.spec != nil {
		d.NodeKind, d.Contract, d.Subject, d.Local = record.spec.Kind, record.spec.Contract, cloneNodeRef(record.spec.Subject), string(record.spec.Local)
		d.Withdrawn = c.liveWithdrawn(path, record.generation)
	}
	d.State = c.tree.state(record.instance)
	return d, true
}

// liveWithdrawn reports whether the registration this cut captured, of the
// given generation, is withdrawn: fenced, gone, or replaced by another
// generation's registration of the same key.
func (c *TreeCut) liveWithdrawn(path NodePath, generation uint64) bool {
	c.tree.Registry.mu.Lock()
	defer c.tree.Registry.mu.Unlock()
	record, ok := c.tree.Registry.nodes[path.String()]
	return !ok || record.withdrawn || record.generation != generation
}

func (c *TreeCut) memberDescriptor(m memberRecord) Descriptor {
	d := Descriptor{
		Ref: Ref(m.key.String()), Kind: EntryAttribute, Domain: KeywordNative, Namespace: m.namespace, Instance: m.instance,
		Local: string(m.spec.Local), Schema: m.spec.Schema, Unit: m.spec.Units, Mode: m.spec.Mode, Visibility: m.spec.Visibility,
		Channel: m.spec.Channel, Scope: m.spec.Scope, Bounds: cloneLimits(m.spec.Bounds), State: c.tree.state(m.instance),
	}
	if live, ok := c.tree.Registry.member(m.key.String()); !ok || live.withdrawn || live.generation != m.generation {
		d.Withdrawn = true
	}
	return d
}

func (c *TreeCut) methodDescriptor(m methodRecord) Descriptor {
	d := Descriptor{
		Ref: Ref(m.key.String()), Kind: EntryMethod, Domain: KeywordNative, Namespace: m.namespace, Instance: m.instance,
		Local: string(m.spec.Local), Schema: m.spec.Schema, Visibility: m.spec.Visibility, Scope: m.spec.Scope, State: c.tree.state(m.instance),
	}
	if live, ok := c.tree.Registry.method(m.key.String()); !ok || live.withdrawn || live.generation != m.generation {
		d.Withdrawn = true
	}
	return d
}

func (c *TreeCut) semanticDescriptors() []Descriptor {
	var out []Descriptor
	for _, category := range IndexCategories {
		out = append(out, Descriptor{Ref: Ref(c.indexKey(category).String()), Kind: EntryAttribute, Domain: KeywordSem, Local: category})
	}
	for _, fact := range c.factKeys() {
		segment, err := FactSegment(fact)
		if err != nil {
			continue
		}
		key := Key{Node: c.installation(), Facet: KeywordAttributes, Domain: KeywordSem, Segment: segment}
		out = append(out, Descriptor{Ref: Ref(key.String()), Kind: EntryAttribute, Domain: KeywordSem, Local: string(fact.FactID)})
	}
	for _, operation := range c.operations() {
		segment, err := DefinitionSegment(operation)
		if err != nil {
			continue
		}
		key := Key{Node: c.installation(), Facet: KeywordMethods, Domain: KeywordSem, Segment: segment}
		out = append(out, Descriptor{Ref: Ref(key.String()), Kind: EntryMethod, Domain: KeywordSem, Local: string(operation.ID)})
	}
	return out
}

func (c *TreeCut) children(path NodePath) []Descriptor {
	var out []Descriptor
	for _, key := range sortedKeys(c.reg.nodes) {
		record := c.reg.nodes[key]
		if parent, ok := record.path.Parent(); ok && parent.equal(path) {
			if d, ok := c.nodeDescriptor(record.path); ok {
				out = append(out, d)
			}
		}
	}
	for _, key := range sortedKeys(c.reg.members) {
		if m := c.reg.members[key]; m.key.Node.equal(path) {
			out = append(out, c.memberDescriptor(m))
		}
	}
	for _, key := range sortedKeys(c.reg.methods) {
		if m := c.reg.methods[key]; m.key.Node.equal(path) {
			out = append(out, c.methodDescriptor(m))
		}
	}
	for _, view := range c.reg.views {
		if view.key.Node.equal(path) {
			out = append(out, c.viewDescriptor(view))
		}
	}
	if level, _ := path.Level(); level == LevelInstallation {
		out = append(out, c.semanticDescriptors()...)
	}
	return out
}

func (f Filter) admits(d Descriptor) bool {
	if f.Kind != "" && d.Kind != f.Kind {
		return false
	}
	if f.Instance != nil && d.Instance.Instance != *f.Instance {
		return false
	}
	if f.Device != nil && !pathHasDevice(d.Ref, *f.Device) {
		return false
	}
	if f.Schema != "" && d.Schema != f.Schema {
		return false
	}
	if f.Unit != "" && d.Unit != f.Unit {
		return false
	}
	if f.Contract != "" && d.Contract != f.Contract {
		return false
	}
	return true
}

func pathHasDevice(ref Ref, device driver.NodeID) bool {
	node, rest, err := parseNode(string(ref))
	_ = rest
	if err != nil {
		return false
	}
	for _, d := range node.Devices {
		if d == device {
			return true
		}
	}
	return false
}

type pageToken struct {
	Cut   CutID `json:"cut"`
	After Ref   `json:"after"`
}

func mintToken(cut CutID, after Ref) string {
	raw, _ := json.Marshal(pageToken{Cut: cut, After: after})
	return "tp1." + base64.RawURLEncoding.EncodeToString(raw)
}

func parseToken(token string) (pageToken, error) {
	body, ok := strings.CutPrefix(token, "tp1.")
	if !ok {
		return pageToken{}, treeError(semreg.InvalidValue, "malformed page token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return pageToken{}, treeError(semreg.InvalidValue, "malformed page token")
	}
	var out pageToken
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return pageToken{}, treeError(semreg.InvalidValue, "malformed page token")
	}
	return out, nil
}

// List returns the children of a node: child nodes, attributes, methods and views, in
// the order of their canonical id bytes, one page of the caller's size, pinned
// to this cut.
func (c *TreeCut) List(ctx context.Context, node NodeRef, page Page) (Entries, error) {
	if page.Size < 1 {
		return Entries{}, treeError(semreg.InvalidValue, "page size")
	}
	if err := node.Validate(); err != nil {
		return Entries{}, err
	}
	path, err := ParseNodePath("/" + string(node.ID))
	if err != nil {
		return Entries{}, treeError(semreg.DanglingReference, "node")
	}
	if _, ok := c.reg.nodes[path.String()]; !ok {
		return Entries{}, treeError(semreg.DanglingReference, "node")
	}
	var after Ref
	if page.Token != "" {
		token, err := parseToken(page.Token)
		if err != nil {
			return Entries{}, err
		}
		if token.Cut != c.id {
			return Entries{}, treeError(semreg.RevisionConflict, "page token is for another cut")
		}
		after = token.After
	}
	all := c.children(path)
	sort.Slice(all, func(i, j int) bool { return all[i].Ref < all[j].Ref })
	out := Entries{Cut: c.id, Entries: []Descriptor{}}
	for _, d := range all {
		if after != "" && d.Ref <= after {
			continue
		}
		if !page.Filter.admits(d) {
			continue
		}
		if len(out.Entries) == page.Size {
			out.Next = mintToken(c.id, out.Entries[len(out.Entries)-1].Ref)
			break
		}
		out.Entries = append(out.Entries, d)
	}
	return out, nil
}

// Describe returns the descriptor of a node, attribute, method or view.
func (c *TreeCut) Describe(ctx context.Context, ref Ref) (Descriptor, error) {
	if path, err := ParseNodePath(string(ref)); err == nil {
		if d, ok := c.nodeDescriptor(path); ok {
			return d, nil
		}
		return Descriptor{}, treeError(semreg.DanglingReference, "node")
	}
	key, err := ParseKey(string(ref))
	if err != nil {
		if _, viewErr := parseViewKey(string(ref)); viewErr != nil {
			return Descriptor{}, err
		}
		if view, ok := c.reg.views[string(ref)]; ok {
			return c.viewDescriptor(view), nil
		}
		return Descriptor{}, treeError(semreg.DanglingReference, "view")
	}
	if key.Domain == KeywordNative {
		if key.Facet == KeywordAttributes {
			if m, ok := c.reg.members[key.String()]; ok {
				return c.memberDescriptor(m), nil
			}
		} else if m, ok := c.reg.methods[key.String()]; ok {
			return c.methodDescriptor(m), nil
		}
		return Descriptor{}, treeError(semreg.DanglingReference, "member")
	}
	for _, d := range c.semanticDescriptors() {
		if d.Ref == ref {
			return d, nil
		}
	}
	return Descriptor{}, treeError(semreg.DanglingReference, "member")
}

// Read reads an attribute. A native read returns the driver's detached retained
// value and never starts a bus request.
func (c *TreeCut) Read(ctx context.Context, ref AttrRef, _ ReadOptions) (Value, error) {
	key, err := ParseKey(string(ref))
	if err != nil {
		return Value{}, err
	}
	if key.Facet != KeywordAttributes {
		return Value{}, keyError("%s is not an attribute", ref)
	}
	if key.Domain == KeywordSem {
		return c.readSemantic(key)
	}
	member, ok := c.reg.members[key.String()]
	if !ok {
		return Value{}, treeError(semreg.DanglingReference, "member")
	}
	return c.readNative(ctx, ref, key, member)
}

func (c *TreeCut) readNative(ctx context.Context, ref AttrRef, key Key, member memberRecord) (Value, error) {
	out := Value{Ref: ref, Cut: c.id, Unit: member.spec.Units}
	live, ok := c.tree.Registry.member(key.String())
	if !ok || live.withdrawn || live.read == nil || live.generation != member.generation {
		out.Status, out.Instance = StatusUnavailable, c.tree.state(member.instance)
		return out, nil
	}
	if pin, ok := c.pinned[key.String()]; ok {
		out.Pinned = true
		return fill(out, pin), nil
	}
	obs, err := observe(ctx, live.read)
	if err != nil {
		out.Status, out.Instance = StatusUnavailable, c.tree.state(member.instance)
		return out, nil
	}
	out = fill(out, obs)
	if out.Status == StatusCurrent {
		out.Status = StatusLiveOnly
		if base, ok := c.baseline[key.String()]; ok && !sameObservation(base, obs) {
			out.Status = StatusChanged
		}
	}
	return out, nil
}

func sameObservation(a, b Observation) bool {
	return bytes.Equal(a.Encoded, b.Encoded) && bytes.Equal(a.Raw, b.Raw) && a.Absent == b.Absent && a.Freshness == b.Freshness
}

// clone returns an observation that shares no memory with the driver's: the
// byte slices and the revision, receive-time and age values are copied.
func (o Observation) clone() Observation {
	o.Encoded = cloneBytes(o.Encoded)
	o.Raw = cloneBytes(o.Raw)
	if o.ReceivedAt != nil {
		received := *o.ReceivedAt
		o.ReceivedAt = &received
	}
	if o.Age != nil {
		age := *o.Age
		o.Age = &age
	}
	if o.Revision != nil {
		revision := *o.Revision
		o.Revision = &revision
	}
	return o
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte{}, b...)
}

// observe reads one observation and detaches it from the driver's buffers, so a
// driver that updates its retained value in place cannot change what a cut
// pinned or compared.
func observe(ctx context.Context, read ObservedReader) (Observation, error) {
	obs, err := read(ctx)
	if err != nil {
		return Observation{}, err
	}
	return obs.clone(), nil
}

func fill(out Value, obs Observation) Value {
	obs = obs.clone()
	out.Verification, out.ReceivedAt, out.Freshness, out.Age = obs.Verification, obs.ReceivedAt, obs.Freshness, obs.Age
	out.Revision, out.Derived, out.Name = obs.Revision, obs.Derived, obs.Name
	switch {
	case obs.Absent:
		out.Status = StatusAbsent
	case obs.Freshness == semreg.FreshnessStale || obs.Freshness == semreg.FreshnessExpired:
		out.Status = StatusStale
	default:
		out.Status = StatusCurrent
		out.Encoded, out.Raw = obs.Encoded, obs.Raw
	}
	return out
}

func (c *TreeCut) readSemantic(key Key) (Value, error) {
	out := Value{Ref: Ref(key.String()), Cut: c.id, Status: StatusCurrent, Pinned: true}
	parts, err := DecodeSegment(key.Segment)
	if err != nil {
		return Value{}, err
	}
	if len(parts) == 2 && parts[0] == IndexPart {
		document, err := c.index(parts[1])
		if err != nil {
			return Value{}, err
		}
		out.Encoded = document
		if parts[1] == "outcomes" {
			// The cut's outcomes can still gain a receipt, so the list is a live read
			// of this cut's own calls, never a pinned value.
			out.Status, out.Pinned = StatusLiveOnly, false
		}
		return out, nil
	}
	fact, err := ParseFactSegment(key.Segment)
	if err != nil {
		return Value{}, err
	}
	canonical, err := semreg.CanonicalJSON(fact)
	if err != nil {
		return Value{}, err
	}
	var envelopes []semreg.FactEnvelope
	for _, member := range c.view.Members {
		if c.excluded(member.AssetID) {
			continue
		}
		for _, envelope := range member.Facts {
			if again, err := semreg.CanonicalJSON(envelope.Key); err == nil && bytes.Equal(again, canonical) {
				envelopes = append(envelopes, envelope)
			}
		}
	}
	if len(envelopes) == 0 {
		return Value{}, treeError(semreg.DanglingReference, "fact is not in the cut")
	}
	out.Encoded, err = json.Marshal(envelopes)
	return out, err
}

func (c *TreeCut) index(category string) ([]byte, error) {
	switch category {
	case "catalog":
		return json.Marshal(struct {
			Digest semreg.Digest `json:"digest"`
			Status graph.Status  `json:"status"`
		}{c.view.Catalog.Digest(), c.view.Catalog.Status()})
	case "planes":
		return json.Marshal(c.view.Catalog.Planes())
	case "lenses":
		return json.Marshal(c.view.Catalog.Lenses())
	case "relations":
		return json.Marshal(c.view.Cut.Relations())
	case "methods":
		return json.Marshal(c.operations())
	case "facts":
		return json.Marshal(c.factKeys())
	case "outcomes":
		c.tree.mu.Lock()
		defer c.tree.mu.Unlock()
		return json.Marshal(append([]ReceiptID{}, c.tree.receipts[c.id]...))
	case "resources", "capabilities", "sources":
		return c.snapshotIndex(category)
	}
	return nil, treeError(semreg.DanglingReference, "index")
}

func (c *TreeCut) snapshotIndex(category string) ([]byte, error) {
	type resource struct {
		Asset semreg.AssetID       `json:"asset_id"`
		ID    semreg.ResourceID    `json:"resource_id"`
		Kind  semreg.DefinitionID  `json:"kind"`
		State semreg.ResourceState `json:"state"`
	}
	var resources []resource
	var capabilities []semreg.CapabilityInstance
	var sources []semreg.SourceDescriptor
	for _, member := range c.view.Members {
		if c.excluded(member.AssetID) {
			continue
		}
		for _, r := range member.Resources {
			resources = append(resources, resource{member.AssetID, r.ResourceID, r.Kind, r.State})
		}
		capabilities = append(capabilities, member.Capabilities...)
		sources = append(sources, member.Sources...)
	}
	switch category {
	case "resources":
		sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
		return json.Marshal(resources)
	case "capabilities":
		return json.Marshal(capabilities)
	}
	return json.Marshal(sources)
}
