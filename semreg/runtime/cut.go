// Package runtime captures one coherent cut of an installation and resumes
// from that cut's cursor. Publication goes through each instance session's own
// transaction. The graph cut, its retention and its paging are the v1 store's.
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/sources"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// Options is the installation clock, the incarnation of this process and the
// monotonic point the owner supplies for evaluation. The cut store is the v1
// store timed by that clock. The catalog defaults to the embedded catalog.
type Options struct {
	Incarnation       string
	Clock             life.Clock
	EvaluateMonotonic v1.MonotonicPoint
	Catalog           *graph.Catalog
}

// View is one captured cut: the graph cut, the member snapshots of that same
// capture, the catalog, the evaluation context, the overlay and the omissions.
// Scope is part of the view, so two captures that share a graph digest and
// differ in scope stay distinct.
type View struct {
	Ref        graph.CutRef
	Cut        *graph.Cut
	Members    []v1.Snapshot
	Catalog    *graph.Catalog
	Evaluation v1.EvaluationContext
	Cursor     driver.Cursor
	Routes     []driver.SettingRoute
	Omissions  []graph.NotIncludedAsset
	Evals      map[sources.SourceKey]sources.MemberEvaluation
	Overlay    []OverlayLink
	Sources    []driver.Source
	Filter     driver.Filter
	Nodes      []placedNode
	Elements   []elementHold
}

// elementHold is the source resolution of one element at the capture, so a
// later publication does not rewrite that cut's view.
type elementHold struct {
	Element driver.Element
	State   sources.ElementState
}

// Service is the SemReg runtime a host or a consuming driver reads and
// publishes through.
type Service struct {
	mu           sync.Mutex
	catalog      *graph.Catalog
	store        *graph.CutStore
	clock        life.Clock
	eval         v1.MonotonicPoint
	opened       uint64
	incarnation  string
	ledger       *identity.Ledger
	table        *sources.Table
	receipt      *receiptClock
	kernels      map[v1.AssetID]*v1.PublicationKernel
	known        map[v1.AssetID]graph.NotIncludedReason
	hidden       map[v1.AssetID]bool
	ownerOf      map[v1.AssetID]life.InstanceID
	owners       map[string]factOwner
	basis        map[string]v1.Snapshot
	stopped      map[string]bool
	assetOf      map[string]v1.AssetID
	declared     map[string]driver.Source
	overlay      []OverlayLink
	routes       []routeRec
	position     uint64
	history      []histRec
	waiters      []chan struct{}
	views        map[string]*View
	order        []string
	demoted      map[v1.Digest]time.Time
	newest       v1.Digest
	recovery     *Recovery
	failed       map[life.InstanceID]bool
	fencedGen    map[string]bool
	sessions     []*Session
	nodes        []placedNode
	installation string
	bridge       driver.BridgeID
}

// placedNode is one node this runtime allocated for the installation.
type placedNode struct {
	Ref  driver.NodeRef
	Kind v1.DefinitionID
	Key  driver.NodeKey
}

type factOwner struct {
	Ref      driver.SourceRef
	Asset    v1.AssetID
	ID       v1.CandidateID
	Revision v1.Uint64
	Inferred bool
}

type routeRec struct {
	Route      driver.SettingRoute
	Asset      v1.AssetID
	Source     v1.SourceID
	Epoch      v1.SourceEpochID
	Generation v1.Uint64
	Live       bool
}

type histRec struct {
	Position  uint64
	Changed   v1.AssetID
	Snapshots []v1.Snapshot
	Routes    []driver.SettingRoute
}

type receiptClock struct {
	mu          sync.Mutex
	sample      func() v1.MonotonicPoint
	forced      *v1.MonotonicPoint
	publication *v1.MonotonicPoint
}

func (c *receiptClock) now() v1.MonotonicPoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.forced != nil {
		return *c.forced
	}
	return c.sample()
}

func (c *receiptClock) force(p v1.MonotonicPoint) {
	c.mu.Lock()
	c.forced = &p
	c.mu.Unlock()
}

func (c *receiptClock) pinPublication(p v1.MonotonicPoint) {
	c.mu.Lock()
	c.publication = &p
	c.mu.Unlock()
}

func (c *receiptClock) publicationPoint() (v1.MonotonicPoint, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.publication == nil {
		return v1.MonotonicPoint{}, false
	}
	return *c.publication, true
}

func (c *receiptClock) clear() {
	c.mu.Lock()
	c.forced = nil
	c.publication = nil
	c.mu.Unlock()
}

// NewCutService builds a runtime over the owner's clock and evaluation point.
func NewCutService(opts Options) (*Service, error) {
	if opts.Clock == nil {
		return nil, errors.New("runtime: a clock is required")
	}
	if opts.Incarnation == "" {
		return nil, errors.New("runtime: an incarnation is required")
	}
	if err := opts.EvaluateMonotonic.Validate(); err != nil {
		return nil, fmt.Errorf("runtime: evaluate monotonic: %w", err)
	}
	opened, err := opts.Clock.Sample()
	if err != nil {
		return nil, fmt.Errorf("runtime: clock: %w", err)
	}
	catalog := opts.Catalog
	if catalog == nil {
		loaded, err := graph.Embedded()
		if err != nil {
			return nil, err
		}
		catalog = loaded
	}
	alloc, err := identity.NewAllocator(identity.NewMemoryStore(), catalog)
	if err != nil {
		return nil, err
	}
	ledger, err := identity.NewLedger(alloc)
	if err != nil {
		return nil, err
	}
	svc := &Service{
		catalog:     catalog,
		clock:       opts.Clock,
		eval:        opts.EvaluateMonotonic,
		opened:      opened,
		incarnation: opts.Incarnation,
		ledger:      ledger,
		kernels:     map[v1.AssetID]*v1.PublicationKernel{},
		known:       map[v1.AssetID]graph.NotIncludedReason{},
		hidden:      map[v1.AssetID]bool{},
		ownerOf:     map[v1.AssetID]life.InstanceID{},
		owners:      map[string]factOwner{},
		basis:       map[string]v1.Snapshot{},
		stopped:     map[string]bool{},
		assetOf:     map[string]v1.AssetID{},
		declared:    map[string]driver.Source{},
		views:       map[string]*View{},
		demoted:     map[v1.Digest]time.Time{},
		failed:      map[life.InstanceID]bool{},
		fencedGen:   map[string]bool{},
	}
	svc.store = graph.NewCutStore(func() time.Time { return opts.Clock.Now() })
	svc.receipt = &receiptClock{sample: svc.receiptSample}
	table, err := sources.NewTable(svc.receipt.now)
	if err != nil {
		return nil, err
	}
	svc.table = table
	return svc, nil
}

func (s *Service) receiptSample() v1.MonotonicPoint {
	sample, err := s.clock.Sample()
	if err != nil {
		return s.eval
	}
	return v1.MonotonicPoint{ClockEpochID: s.eval.ClockEpochID, Nanoseconds: v1.Uint64(strconv.FormatUint(sample, 10))}
}

// Ledger is the identity ledger this runtime allocates and admits through.
func (s *Service) Ledger() *identity.Ledger { return s.ledger }

// Element resolves one element against the resolution frozen with the capture.
// A view that has no frozen resolution reads the live source table.
func (s *Service) Element(el driver.Element, v View) sources.ElementState {
	key := elementText(el)
	for _, held := range v.Elements {
		if elementText(held.Element) == key {
			return held.State
		}
	}
	return s.table.Element(el, v.Evals)
}

func (s *Service) wall() v1.TimePoint {
	return v1.TimePoint{
		UnixNanoseconds: v1.Int64(strconv.FormatInt(s.clock.Now().UnixNano(), 10)),
		ClockID:         "clock.utc", UncertaintyNS: "0",
	}
}

func (s *Service) context() (v1.EvaluationContext, error) {
	mono, err := s.liveMono()
	if err != nil {
		return v1.EvaluationContext{}, err
	}
	return v1.EvaluationContext{EvaluatedAt: s.wall(), EvaluateMonotonic: mono}, nil
}

// liveMono is the owner's evaluation point plus the clock's elapsed sample
// since Open. A captured view keeps the point it stored. A pinned publication
// point belongs to replay and is not used here.
func (s *Service) liveMono() (v1.MonotonicPoint, error) {
	sample, err := s.clock.Sample()
	if err != nil {
		return v1.MonotonicPoint{}, fmt.Errorf("runtime: clock: %w", err)
	}
	if sample < s.opened {
		return v1.MonotonicPoint{}, errors.New("runtime: the evaluation clock moved backward")
	}
	return addMono(s.eval, sample-s.opened)
}

func addMono(base v1.MonotonicPoint, delta uint64) (v1.MonotonicPoint, error) {
	n, err := strconv.ParseUint(string(base.Nanoseconds), 10, 64)
	if err != nil {
		return v1.MonotonicPoint{}, fmt.Errorf("runtime: evaluate monotonic: %w", err)
	}
	sum := n + delta
	if sum < n {
		return v1.MonotonicPoint{}, errors.New("runtime: the evaluation clock overflowed")
	}
	return v1.MonotonicPoint{ClockEpochID: base.ClockEpochID, Nanoseconds: v1.Uint64(strconv.FormatUint(sum, 10))}, nil
}

func (s *Service) cursorAt(position uint64) driver.Cursor {
	return driver.Cursor{Incarnation: s.incarnation, Position: v1.Uint64(strconv.FormatUint(position, 10))}
}

// Capture reads every current committed snapshot in the filter under one
// consistent view and assembles them with the v1 cut.
func (s *Service) Capture(ctx context.Context, filter driver.Filter) (driver.SemanticCut, error) {
	view, err := s.CaptureView(ctx, filter)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	return semanticOf(view), nil
}

// CaptureView is Capture plus the retained snapshots, catalog, overlay,
// omissions and evaluation context of that capture.
func (s *Service) CaptureView(ctx context.Context, filter driver.Filter) (View, error) {
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	if err := filter.Validate(); err != nil {
		return View{}, err
	}
	s.mu.Lock()
	snaps, position, known := s.consistent()
	eval, err := s.context()
	if err != nil {
		s.mu.Unlock()
		return View{}, err
	}
	overlay := append([]OverlayLink(nil), s.overlay...)
	sourcesNow := s.ledger.Sources()
	nodes := append([]placedNode(nil), s.nodes...)
	routes := append([]routeRec(nil), s.routes...)
	basis := map[string]v1.Snapshot{}
	for k, v := range s.basis {
		basis[k] = v
	}
	stopped := map[string]bool{}
	for k, v := range s.stopped {
		stopped[k] = v
	}
	assetOf := map[string]v1.AssetID{}
	for k, v := range s.assetOf {
		assetOf[k] = v
	}
	declared := map[string]driver.Source{}
	for k, v := range s.declared {
		declared[k] = v
	}
	catalog := s.catalog
	s.mu.Unlock()

	scoped := filterSnaps(snaps, filter)
	omissions := omissionsFor(known, snaps, filter)
	completeness := graph.CutComplete
	if len(omissions) > 0 {
		completeness = graph.CutPartial
	}
	cut, err := graph.AssembleCut(scoped, catalog, completeness)
	if err != nil {
		return View{}, err
	}
	omissions = append([]graph.NotIncludedAsset(nil), omissions...)
	omissions = mergeFindings(cut, scoped, omissions)
	if len(omissions) > 0 && cut.Completeness() == graph.CutComplete {
		// AssembleCut keeps the completeness it was given when it excludes
		// nobody. A non-empty omission list is a partial cut (CUT-4).
		cut, err = graph.AssembleCut(scoped, catalog, graph.CutPartial)
		if err != nil {
			return View{}, err
		}
	}
	evals, err := memberEvals(eval, scoped, declared, basis, stopped, assetOf)
	if err != nil {
		return View{}, err
	}
	visible := visibleSnaps(scoped, cut)
	ref := graph.CutRef{
		CutID: cut.ID(), Completeness: cut.Completeness(),
		Members: cut.Members(), NotIncluded: sortOmissions(omissions),
	}
	view := &View{
		Ref: ref, Cut: cut, Members: cloneSnaps(visible), Catalog: catalog,
		Evaluation: eval, Cursor: s.cursorAt(position),
		Routes: routeViews(routes, visible, cut), Omissions: sortOmissions(omissions),
		Evals: evals, Overlay: overlay, Sources: sourcesNow, Filter: cloneFilter(filter),
		Nodes: nodes, Elements: s.elementStates(declared, evals),
	}
	if err := s.remember(view); err != nil {
		return View{}, err
	}
	return *view, nil
}

func (s *Service) consistent() (snaps []v1.Snapshot, position uint64, known map[v1.AssetID]graph.NotIncludedReason) {
	for asset, kernel := range s.kernels {
		if s.hidden[asset] {
			continue
		}
		if cur, _, ok := kernel.Current(); ok {
			snaps = append(snaps, cur)
		}
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].AssetID < snaps[j].AssetID })
	known = map[v1.AssetID]graph.NotIncludedReason{}
	for id, reason := range s.known {
		known[id] = reason
	}
	return snaps, s.position, known
}

func (s *Service) remember(view *View) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	id := view.Cut.ID()
	if s.newest != "" && s.newest != id {
		if _, ok := s.demoted[s.newest]; !ok {
			s.demoted[s.newest] = now
		}
	}
	if err := s.store.Retain(view.Cut, view.Members, view.Catalog); err != nil {
		return err
	}
	delete(s.demoted, id)
	s.newest = id
	key := viewKey(view.Ref)
	s.views[key] = view
	s.order = append(s.order, key)
	return nil
}

func filterSnaps(snaps []v1.Snapshot, filter driver.Filter) []v1.Snapshot {
	if len(filter.Assets) == 0 {
		return append([]v1.Snapshot(nil), snaps...)
	}
	out := make([]v1.Snapshot, 0, len(snaps))
	for _, snap := range snaps {
		if containsAsset(filter.Assets, snap.AssetID) {
			out = append(out, snap)
		}
	}
	return out
}

func omissionsFor(known map[v1.AssetID]graph.NotIncludedReason, snaps []v1.Snapshot, filter driver.Filter) []graph.NotIncludedAsset {
	have := map[v1.AssetID]bool{}
	for _, snap := range snaps {
		have[snap.AssetID] = true
	}
	var out []graph.NotIncludedAsset
	for asset := range known {
		if have[asset] || !inFilter(filter, asset) {
			continue
		}
		out = append(out, graph.NotIncludedAsset{AssetID: asset, Reason: graph.NotIncludedSourceUnavailable})
	}
	return sortOmissions(out)
}

func mergeFindings(cut *graph.Cut, snaps []v1.Snapshot, omissions []graph.NotIncludedAsset) []graph.NotIncludedAsset {
	seen := map[v1.AssetID]bool{}
	for _, row := range omissions {
		seen[row.AssetID] = true
	}
	have := map[v1.AssetID]bool{}
	for _, snap := range snaps {
		have[snap.AssetID] = true
	}
	for _, asset := range cut.Excluded() {
		if seen[asset] {
			continue
		}
		omissions = append(omissions, graph.NotIncludedAsset{AssetID: asset, Reason: graph.NotIncludedExcluded})
		seen[asset] = true
	}
	for _, finding := range cut.Findings() {
		if finding.Kind != graph.CutUngraphed {
			continue
		}
		for _, asset := range finding.Assets {
			if have[asset] || seen[asset] {
				continue
			}
			omissions = append(omissions, graph.NotIncludedAsset{AssetID: asset, Reason: graph.NotIncludedUngraphed})
			seen[asset] = true
		}
	}
	return sortOmissions(omissions)
}

func sortOmissions(in []graph.NotIncludedAsset) []graph.NotIncludedAsset {
	out := append([]graph.NotIncludedAsset(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].AssetID != out[j].AssetID {
			return out[i].AssetID < out[j].AssetID
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func visibleSnaps(snaps []v1.Snapshot, cut *graph.Cut) []v1.Snapshot {
	excluded := map[v1.AssetID]bool{}
	for _, id := range cut.Excluded() {
		excluded[id] = true
	}
	out := make([]v1.Snapshot, 0, len(snaps))
	for _, snap := range snaps {
		if !excluded[snap.AssetID] {
			out = append(out, snap)
		}
	}
	return out
}

func memberEvals(eval v1.EvaluationContext, snaps []v1.Snapshot, declared map[string]driver.Source, basis map[string]v1.Snapshot, stopped map[string]bool, assetOf map[string]v1.AssetID) (map[sources.SourceKey]sources.MemberEvaluation, error) {
	byAsset := map[v1.AssetID]v1.Snapshot{}
	for _, snap := range snaps {
		byAsset[snap.AssetID] = snap
	}
	out := map[sources.SourceKey]sources.MemberEvaluation{}
	for key, src := range declared {
		ref := driver.SourceRef{For: src.For, Producer: src.Producer, PathKey: src.PathKey}
		var member v1.Snapshot
		var ok bool
		if stopped[key] {
			member, ok = basis[key]
		} else if asset, have := assetOf[key]; have {
			member, ok = byAsset[asset]
		}
		if !ok {
			continue
		}
		view, err := v1.EvaluateSnapshot(member, eval)
		if err != nil {
			return nil, err
		}
		out[sources.KeyOf(ref)] = sources.MemberEvaluation{Member: member, View: view}
	}
	return out, nil
}

func routeViews(routes []routeRec, snaps []v1.Snapshot, cut *graph.Cut) []driver.SettingRoute {
	excluded := map[v1.AssetID]bool{}
	for _, id := range cut.Excluded() {
		excluded[id] = true
	}
	byAsset := map[v1.AssetID]v1.Snapshot{}
	for _, snap := range snaps {
		byAsset[snap.AssetID] = snap
	}
	var out []driver.SettingRoute
	for _, route := range routes {
		if !route.Live || excluded[route.Asset] {
			continue
		}
		snap, ok := byAsset[route.Asset]
		if !ok || !bindingCurrent(snap, route) {
			continue
		}
		out = append(out, route.Route)
	}
	return out
}

func bindingCurrent(snap v1.Snapshot, route routeRec) bool {
	for _, binding := range snap.Bindings {
		if binding.SourceID == route.Source && binding.SourceEpochID == route.Epoch && binding.DriverGeneration == route.Generation && binding.State == v1.BindingCurrent {
			return true
		}
	}
	return false
}

func semanticOf(view View) driver.SemanticCut {
	routes := append([]driver.SettingRoute(nil), view.Routes...)
	if routes == nil {
		routes = []driver.SettingRoute{}
	}
	return driver.SemanticCut{Ref: view.Ref, Snapshots: cloneSnaps(view.Members), SettingRoutes: routes}
}

// Lookup is the pinned retained-cut read. An unpinned ref (empty cut id)
// returns the newest capture. A pin that is not retained answers
// revision_conflict naming that revision. The expiry time is included only
// when this package recorded the cut expiring by time.
func (s *Service) Lookup(ctx context.Context, ref graph.CutRef) (View, error) {
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref.CutID == "" {
		if s.newest == "" {
			return View{}, &v1.Error{ID: v1.RevisionConflict, Detail: "pinned revision "}
		}
		for i := len(s.order) - 1; i >= 0; i-- {
			view := s.views[s.order[i]]
			if view != nil && view.Ref.CutID == s.newest {
				return *view, nil
			}
		}
		return View{}, &v1.Error{ID: v1.RevisionConflict, Detail: "pinned revision " + string(s.newest)}
	}
	view := s.views[viewKey(ref)]
	kept := s.retained(ref.CutID)
	if view != nil && kept && view.Ref.CutID == ref.CutID {
		return *view, nil
	}
	return View{}, s.conflict(ref.CutID)
}

func (s *Service) retained(id v1.Digest) bool {
	page, err := s.store.ListResources(graph.ListRequest{CutID: &id, PageSize: 1})
	return err == nil && page.CutID == id
}

func (s *Service) conflict(id v1.Digest) error {
	detail := "pinned revision " + string(id)
	if demoted, ok := s.demoted[id]; ok {
		expiry := demoted.Add(graph.CutRetention)
		if !s.clock.Now().Before(expiry) {
			detail += " expired at " + expiry.UTC().Format(time.RFC3339Nano)
		}
	}
	return &v1.Error{ID: v1.RevisionConflict, Detail: detail}
}

// Resume returns every ordered commit after the cursor, or an explicit resync
// when that history is not available. It waits under the caller context when
// the cursor is current and no matching commit has landed.
func (s *Service) Resume(ctx context.Context, cursor driver.Cursor, filter driver.Filter) (driver.Changes, error) {
	if err := ctx.Err(); err != nil {
		return driver.Changes{}, err
	}
	if err := cursor.Validate(); err != nil {
		return driver.Changes{}, err
	}
	if err := filter.Validate(); err != nil {
		return driver.Changes{}, err
	}
	for {
		s.mu.Lock()
		changes, wait, err := s.collect(cursor, filter)
		if err != nil || !wait {
			s.mu.Unlock()
			if err != nil {
				return driver.Changes{}, err
			}
			return changes, nil
		}
		ch := make(chan struct{}, 1)
		s.waiters = append(s.waiters, ch)
		changes, wait, err = s.collect(cursor, filter)
		if err != nil || !wait {
			s.removeWaiter(ch)
			s.mu.Unlock()
			if err != nil {
				return driver.Changes{}, err
			}
			return changes, nil
		}
		s.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			s.mu.Lock()
			s.removeWaiter(ch)
			s.mu.Unlock()
			return driver.Changes{}, ctx.Err()
		}
	}
}

func (s *Service) collect(cursor driver.Cursor, filter driver.Filter) (driver.Changes, bool, error) {
	if cursor.Incarnation != s.incarnation {
		return driver.Changes{Cursor: cursor, Resync: true}, false, nil
	}
	start, err := strconv.ParseUint(string(cursor.Position), 10, 64)
	if err != nil {
		return driver.Changes{Cursor: cursor, Resync: true}, false, nil
	}
	if start > s.position {
		return driver.Changes{Cursor: cursor, Resync: true}, false, nil
	}
	var matched []histRec
	end := start
	for _, rec := range s.history {
		if rec.Position <= start {
			continue
		}
		if rec.Position != end+1 {
			return driver.Changes{Cursor: cursor, Resync: true}, false, nil
		}
		end = rec.Position
		if historyMatches(rec, filter) {
			matched = append(matched, rec)
		}
	}
	if len(matched) == 0 {
		if end == s.position {
			return driver.Changes{}, true, nil
		}
		return driver.Changes{Cursor: cursor, Resync: true}, false, nil
	}
	last := matched[len(matched)-1].Position
	commits := make([]driver.SemanticCut, 0, len(matched))
	for _, rec := range matched {
		if rec.Position > last {
			break
		}
		cut, err := s.replayCut(rec, filter)
		if err != nil {
			return driver.Changes{}, false, err
		}
		commits = append(commits, cut)
	}
	return driver.Changes{Cursor: s.cursorAt(last), Commits: commits}, false, nil
}

func historyMatches(rec histRec, filter driver.Filter) bool {
	if len(filter.Assets) == 0 {
		return true
	}
	if rec.Changed != "" {
		return containsAsset(filter.Assets, rec.Changed)
	}
	for _, snap := range rec.Snapshots {
		if containsAsset(filter.Assets, snap.AssetID) {
			return true
		}
	}
	return false
}

func (s *Service) replayCut(rec histRec, filter driver.Filter) (driver.SemanticCut, error) {
	snaps := filterSnaps(rec.Snapshots, filter)
	completeness := graph.CutComplete
	cut, err := graph.AssembleCut(snaps, s.catalog, completeness)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	routes := make([]driver.SettingRoute, 0)
	for _, route := range rec.Routes {
		routes = append(routes, route)
	}
	return driver.SemanticCut{
		Ref:           graph.CutRef{CutID: cut.ID(), Completeness: cut.Completeness(), Members: cut.Members()},
		Snapshots:     cloneSnaps(snaps),
		SettingRoutes: routes,
	}, nil
}

func (s *Service) signal() {
	for _, ch := range s.waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	s.waiters = nil
}

func (s *Service) removeWaiter(ch chan struct{}) {
	out := s.waiters[:0]
	for _, waiter := range s.waiters {
		if waiter != ch {
			out = append(out, waiter)
		}
	}
	s.waiters = out
}

func (s *Service) advance(changed v1.AssetID, snaps []v1.Snapshot, routes []driver.SettingRoute) {
	s.position++
	s.history = append(s.history, histRec{Position: s.position, Changed: changed, Snapshots: cloneSnaps(snaps), Routes: routes})
	s.signal()
}

func (s *Service) hasPosition(position uint64) bool {
	for _, rec := range s.history {
		if rec.Position == position {
			return true
		}
	}
	return false
}

// Input is the scoped SemanticInput of one session.
type Input struct {
	svc     *Service
	session *Session
	scope   driver.Filter
}

// Input returns the session's scoped read.
func (s *Session) Input() driver.SemanticInput {
	return &Input{svc: s.svc, session: s, scope: cloneFilter(s.scope)}
}

func (in *Input) narrow(filter driver.Filter) (driver.Filter, error) {
	if err := filter.Validate(); err != nil {
		return driver.Filter{}, err
	}
	if len(in.scope.Assets) == 0 {
		return filter, nil
	}
	if len(filter.Assets) == 0 {
		return cloneFilter(in.scope), nil
	}
	for _, asset := range filter.Assets {
		if !containsAsset(in.scope.Assets, asset) {
			return driver.Filter{}, &v1.Error{ID: v1.DanglingReference, Detail: "asset outside the session scope"}
		}
	}
	return filter, nil
}

// Capture is the session's scoped capture. The ref it returns is the cut a
// later formula publication of this session resolves against.
func (in *Input) Capture(ctx context.Context, filter driver.Filter) (driver.SemanticCut, error) {
	narrowed, err := in.narrow(filter)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	view, err := in.svc.CaptureView(ctx, narrowed)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	if in.session != nil {
		in.session.Pin(view.Ref)
	}
	return semanticOf(view), nil
}

// Resume is the session's scoped resume.
func (in *Input) Resume(ctx context.Context, cursor driver.Cursor, filter driver.Filter) (driver.Changes, error) {
	narrowed, err := in.narrow(filter)
	if err != nil {
		return driver.Changes{}, err
	}
	return in.svc.Resume(ctx, cursor, narrowed)
}

// RetrieveLens continues a lens read against the captured view the ref names,
// including after the clock moves and after a later capture records a
// different omission list for the same graph digest.
func (in *Input) RetrieveLens(ctx context.Context, ref graph.CutRef, request graph.LensRequest) (graph.LensResult, error) {
	return in.svc.RetrieveLens(ctx, ref, request)
}

// SumMembers sums a lens of the captured view. A scope resource that is not
// in that view is dangling_reference.
func (in *Input) SumMembers(ctx context.Context, ref graph.CutRef, request graph.LensRequest, fact v1.DefinitionRef, scope v1.ResourceID) (graph.MemberSum, error) {
	view, err := in.svc.Lookup(ctx, ref)
	if err != nil {
		return graph.MemberSum{}, err
	}
	if !resourceInView(view, scope) {
		return graph.MemberSum{}, &v1.Error{ID: v1.DanglingReference, Detail: "scope is outside the captured cut"}
	}
	result, err := in.svc.RetrieveLens(ctx, ref, request)
	if err != nil {
		return graph.MemberSum{}, err
	}
	return graph.SumMembers(result, fact, scope)
}

// RetrieveLens reads one lens page of a retained capture. The page is checked
// against that capture's own cut, catalog and evaluation, not against whatever
// the v1 store last retained under the same digest.
func (s *Service) RetrieveLens(ctx context.Context, ref graph.CutRef, request graph.LensRequest) (graph.LensResult, error) {
	view, err := s.Lookup(ctx, ref)
	if err != nil {
		return graph.LensResult{}, err
	}
	op := graph.NewCutStore(func() time.Time { return evaluationWall(view.Evaluation) })
	if err := op.Retain(view.Cut, view.Members, view.Catalog); err != nil {
		return graph.LensResult{}, err
	}
	id := view.Ref.CutID
	request.CutID = &id
	request.Members = view.Members
	request.NotIncluded = append([]graph.NotIncludedAsset(nil), view.Omissions...)
	return graph.RetrieveLens(op, view.Catalog, request, view.Evaluation)
}

func resourceInView(view View, id v1.ResourceID) bool {
	for _, snap := range view.Members {
		if !inFilter(view.Filter, snap.AssetID) {
			continue
		}
		for _, resource := range snap.Resources {
			if resource.ResourceID == id {
				return true
			}
		}
	}
	return false
}

// wall reads the capture wall time back. A clock id other than clock.utc, or
// an unparsable instant, contributes the zero time. The lens store only needs
// a frozen instant; the evaluation context itself is what the page token binds.
func evaluationWall(e v1.EvaluationContext) time.Time {
	if e.EvaluatedAt.ClockID != "clock.utc" {
		return time.Unix(0, 0).UTC()
	}
	n, err := strconv.ParseInt(string(e.EvaluatedAt.UnixNanoseconds), 10, 64)
	if err != nil {
		return time.Unix(0, 0).UTC()
	}
	return time.Unix(0, n).UTC()
}

func viewKey(ref graph.CutRef) string {
	ref.NotIncluded = sortOmissions(ref.NotIncluded)
	raw, _ := json.Marshal(ref)
	return string(raw)
}

func inFilter(filter driver.Filter, asset v1.AssetID) bool {
	if len(filter.Assets) == 0 {
		return true
	}
	return containsAsset(filter.Assets, asset)
}

func containsAsset(ids []v1.AssetID, id v1.AssetID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func cloneFilter(filter driver.Filter) driver.Filter {
	return driver.Filter{Assets: append([]v1.AssetID(nil), filter.Assets...)}
}

func cloneSnaps(snaps []v1.Snapshot) []v1.Snapshot {
	out, err := cloneValue(snaps)
	if err != nil || out == nil {
		return append([]v1.Snapshot(nil), snaps...)
	}
	return out
}

func cloneValue[T any](v T) (T, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		var zero T
		return zero, err
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

func evidenceRef(label string) v1.EvidenceRef {
	sum := sha256.Sum256([]byte(label))
	return v1.EvidenceRef{
		Owner: "owner.runtime", Kind: "evidence.runtime",
		Digest:   v1.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Contract: "runtime.evidence/v1", Access: v1.EvidenceAccessPublic, Redaction: v1.RedactionNone,
	}
}

func (s *Service) elementStates(declared map[string]driver.Source, evals map[sources.SourceKey]sources.MemberEvaluation) []elementHold {
	seen := map[string]bool{}
	var out []elementHold
	for _, src := range declared {
		key := elementText(src.For)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, elementHold{Element: src.For, State: s.table.Element(src.For, evals)})
	}
	return out
}

func elementText(el driver.Element) string {
	raw, err := v1.CanonicalJSON(el)
	if err != nil {
		raw, _ = json.Marshal(el)
	}
	return string(raw)
}

func sourceKeyOf(src driver.Source) string {
	ref := driver.SourceRef{For: src.For, Producer: src.Producer, PathKey: src.PathKey}
	return string(sources.KeyOf(ref))
}
