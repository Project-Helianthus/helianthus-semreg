package tree

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// ListResult is a frozen listing page with the cut's omission vector beside it.
type ListResult struct {
	Page        graph.ResourcePage
	NotIncluded []graph.NotIncludedAsset
}

// TraversalResult is a frozen traversal page with the cut's omission vector
// beside it.
type TraversalResult struct {
	Page        graph.TraversalPage
	NotIncluded []graph.NotIncludedAsset
}

// CatalogResult is the element catalog read: its digest, planes, lenses and
// entries.
type CatalogResult struct {
	Digest  semreg.Digest
	Status  graph.Status
	Planes  []graph.Plane
	Lenses  []graph.Lens
	Entries []graph.Element
}

func omissions(view CutView) []graph.NotIncludedAsset {
	return append([]graph.NotIncludedAsset(nil), view.Ref.NotIncluded...)
}

// conflict names the pinned revision, and its expiry when the cut source says
// when it expired, in a revision_conflict.
func conflict(pinned semreg.Digest, err error) error {
	var expired *ExpiredError
	if errors.As(err, &expired) {
		return &semreg.Error{ID: semreg.RevisionConflict, Detail: fmt.Sprintf("tree: pinned revision %s expired at %s", pinned, expired.ExpiredAt.UTC().Format(time.RFC3339Nano))}
	}
	var graphError *semreg.Error
	if errors.As(err, &graphError) && graphError.ID == semreg.RevisionConflict {
		return &semreg.Error{ID: semreg.RevisionConflict, Detail: fmt.Sprintf("tree: pinned revision %s: %s", pinned, graphError.Detail)}
	}
	return err
}

// serving returns the view a read is served from: the capture's own cut when
// the request names no pin or names that cut, the exact retained cut of another
// pin, or revision_conflict.
func (c *TreeCut) serving(ctx context.Context, pin *semreg.Digest) (CutView, semreg.Digest, error) {
	if pin == nil || *pin == "" || *pin == c.view.Ref.CutID {
		// The capture answers from the body it retained itself, for as long as the
		// cut source still retains the cut; once it does not, the read is a
		// revision_conflict naming the revision.
		if _, err := c.tree.cfg.Cuts.Lookup(ctx, c.view.Ref); err != nil {
			return CutView{}, c.view.Ref.CutID, conflict(c.view.Ref.CutID, err)
		}
		return c.view, c.view.Ref.CutID, nil
	}
	view, err := c.tree.cfg.Cuts.Lookup(ctx, graph.CutRef{CutID: *pin})
	if err != nil {
		return CutView{}, *pin, conflict(*pin, err)
	}
	return view, *pin, nil
}

// ListResources serves the frozen listing from the retained cut of this capture
// (or the cut a pin names), with the cut's omission vector beside the page.
func (c *TreeCut) ListResources(ctx context.Context, request graph.ListRequest) (ListResult, error) {
	view, digest, err := c.serving(ctx, request.CutID)
	if err != nil {
		return ListResult{}, err
	}
	request.CutID = &digest
	page, err := view.Store.ListResources(request)
	if err != nil {
		return ListResult{}, conflict(digest, err)
	}
	return ListResult{Page: page, NotIncluded: omissions(view)}, nil
}

// Traverse serves the frozen traversal the same way.
func (c *TreeCut) Traverse(ctx context.Context, request graph.TraversalRequest) (TraversalResult, error) {
	view, digest, err := c.serving(ctx, request.CutID)
	if err != nil {
		return TraversalResult{}, err
	}
	request.CutID = &digest
	page, err := view.Store.Traverse(request)
	if err != nil {
		return TraversalResult{}, conflict(digest, err)
	}
	return TraversalResult{Page: page, NotIncluded: omissions(view)}, nil
}

// RetrieveLens serves the frozen lens retrieval with the members, the omission
// vector and the evaluation context of the view the capture holds, so a later
// page is answered from the first page's cut and context.
func (c *TreeCut) RetrieveLens(ctx context.Context, request graph.LensRequest) (graph.LensResult, error) {
	view, digest, err := c.serving(ctx, request.CutID)
	if err != nil {
		return graph.LensResult{}, err
	}
	return retrieveLens(view, digest, request)
}

func retrieveLens(view CutView, digest semreg.Digest, request graph.LensRequest) (graph.LensResult, error) {
	request.CutID = &digest
	request.Members = view.Members
	request.NotIncluded = omissions(view)
	result, err := graph.RetrieveLens(view.Store, view.Catalog, request, view.Evaluation)
	if err != nil {
		return graph.LensResult{}, conflict(digest, err)
	}
	return result, nil
}

// Catalog reads the element catalog held with the cut.
func (c *TreeCut) Catalog(ctx context.Context) (CatalogResult, error) {
	view, _, err := c.serving(ctx, nil)
	if err != nil {
		return CatalogResult{}, err
	}
	return catalogOf(view.Catalog), nil
}

func catalogOf(catalog *graph.Catalog) CatalogResult {
	return CatalogResult{Digest: catalog.Digest(), Status: catalog.Status(), Planes: catalog.Planes(), Lenses: catalog.Lenses(), Entries: catalog.Elements()}
}

// ListResources is the unpinned (newest cut) or pinned listing without a
// capture in hand.
func (t *Tree) ListResources(ctx context.Context, request graph.ListRequest) (ListResult, error) {
	view, digest, err := t.view(ctx, request.CutID)
	if err != nil {
		return ListResult{}, err
	}
	request.CutID = &digest
	page, err := view.Store.ListResources(request)
	if err != nil {
		return ListResult{}, conflict(digest, err)
	}
	return ListResult{Page: page, NotIncluded: omissions(view)}, nil
}

// Traverse is the traversal without a capture in hand.
func (t *Tree) Traverse(ctx context.Context, request graph.TraversalRequest) (TraversalResult, error) {
	view, digest, err := t.view(ctx, request.CutID)
	if err != nil {
		return TraversalResult{}, err
	}
	request.CutID = &digest
	page, err := view.Store.Traverse(request)
	if err != nil {
		return TraversalResult{}, conflict(digest, err)
	}
	return TraversalResult{Page: page, NotIncluded: omissions(view)}, nil
}

// RetrieveLens is the lens retrieval without a capture in hand.
func (t *Tree) RetrieveLens(ctx context.Context, request graph.LensRequest) (graph.LensResult, error) {
	view, digest, err := t.view(ctx, request.CutID)
	if err != nil {
		return graph.LensResult{}, err
	}
	return retrieveLens(view, digest, request)
}

// Catalog is the catalog read of the newest cut.
func (t *Tree) Catalog(ctx context.Context) (CatalogResult, error) {
	view, err := t.cfg.Cuts.Capture(ctx, driver.Filter{})
	if err != nil {
		return CatalogResult{}, err
	}
	return catalogOf(view.Catalog), nil
}

func (t *Tree) view(ctx context.Context, pin *semreg.Digest) (CutView, semreg.Digest, error) {
	if pin == nil || *pin == "" {
		view, err := t.cfg.Cuts.Capture(ctx, driver.Filter{})
		if err != nil {
			return CutView{}, "", err
		}
		return view, view.Ref.CutID, nil
	}
	view, err := t.cfg.Cuts.Lookup(ctx, graph.CutRef{CutID: *pin})
	if err != nil {
		return CutView{}, *pin, conflict(*pin, err)
	}
	return view, *pin, nil
}

// ScopedInput is the driver.SemanticInput of one consuming session. Its scope is
// the session's filter; a capture can narrow it and never widen it.
type ScopedInput struct {
	tree  *Tree
	scope driver.Filter
	views map[string]CutView
}

var _ driver.SemanticInput = (*ScopedInput)(nil)

// SemanticInput returns the scoped semantic input of a session with the scope.
func (t *Tree) SemanticInput(scope driver.Filter) *ScopedInput {
	return &ScopedInput{tree: t, scope: scope, views: map[string]CutView{}}
}

func (s *ScopedInput) narrow(filter driver.Filter) (driver.Filter, error) {
	if len(s.scope.Assets) == 0 {
		return filter, nil
	}
	if len(filter.Assets) == 0 {
		return s.scope, nil
	}
	allowed := map[semreg.AssetID]struct{}{}
	for _, asset := range s.scope.Assets {
		allowed[asset] = struct{}{}
	}
	for _, asset := range filter.Assets {
		if _, ok := allowed[asset]; !ok {
			return driver.Filter{}, treeError(semreg.DanglingReference, "asset is outside the session scope")
		}
	}
	return filter, nil
}

func refKey(ref graph.CutRef) (string, error) {
	raw, err := json.Marshal(ref)
	return string(raw), err
}

// Capture captures inside the session scope and keeps the view the CutRef names.
func (s *ScopedInput) Capture(ctx context.Context, filter driver.Filter) (driver.SemanticCut, error) {
	effective, err := s.narrow(filter)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	view, err := s.tree.cfg.Cuts.Capture(ctx, effective)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	key, err := refKey(view.Ref)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	s.expire(ctx)
	// The session keeps its own copy of the capture, and its own retention of
	// the captured cut body, so neither a consumer's edit of what it was handed
	// nor a later retain of the same digest changes what a page continuation of
	// this CutRef is answered from. A later capture that yields an equal CutRef
	// does not replace the view the first capture issued its tokens on.
	s.tree.mu.Lock()
	held, kept := s.views[key]
	s.tree.mu.Unlock()
	if !kept {
		if held, err = detachView(view); err != nil {
			return driver.SemanticCut{}, err
		}
		s.tree.mu.Lock()
		if again, ok := s.views[key]; ok {
			held = again
		} else {
			s.views[key] = held
		}
		s.tree.mu.Unlock()
	}
	handed, err := cloneSnapshots(held.Members)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	ref := held.Ref
	ref.Members = append([]graph.CutMember(nil), ref.Members...)
	ref.NotIncluded = append([]graph.NotIncludedAsset(nil), ref.NotIncluded...)
	routes, err := cloneRoutes(held.SettingRoutes)
	if err != nil {
		return driver.SemanticCut{}, err
	}
	return driver.SemanticCut{Ref: ref, Snapshots: handed, SettingRoutes: routes}, nil
}

// cloneRoutes copies setting routes so the copy shares no memory with the
// source's slice or the values it points to.
func cloneRoutes(in []driver.SettingRoute) ([]driver.SettingRoute, error) {
	if in == nil {
		return nil, nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	out := []driver.SettingRoute{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func cloneSnapshots(in []semreg.Snapshot) ([]semreg.Snapshot, error) {
	out := make([]semreg.Snapshot, len(in))
	for i, snapshot := range in {
		raw, err := semreg.CanonicalJSON(snapshot)
		if err != nil {
			return nil, err
		}
		if out[i], err = semreg.Decode[semreg.Snapshot](raw); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// detachView copies the members and the reference vectors of a view and retains
// the captured cut with its members and catalog in a store of its own.
func detachView(view CutView) (CutView, error) {
	members, err := cloneSnapshots(view.Members)
	if err != nil {
		return CutView{}, err
	}
	view.Members = members
	if view.SettingRoutes, err = cloneRoutes(view.SettingRoutes); err != nil {
		return CutView{}, err
	}
	return retainOwn(view)
}

// retainOwn copies the reference vectors of a view and retains the captured cut
// with its members and catalog in a store of its own, so a later retain of the
// same cut digest in the source's store cannot change the body, or the
// completeness, this capture is answered from.
func retainOwn(view CutView) (CutView, error) {
	view.Ref.Members = append([]graph.CutMember(nil), view.Ref.Members...)
	view.Ref.NotIncluded = append([]graph.NotIncludedAsset(nil), view.Ref.NotIncluded...)
	store := graph.NewCutStore(nil)
	if err := store.Retain(view.Cut, view.Members, view.Catalog); err != nil {
		return CutView{}, err
	}
	view.Store = store
	return view, nil
}

// gone reports that the cut source no longer retains the cut it was asked for.
func gone(err error) bool {
	var expired *ExpiredError
	if errors.As(err, &expired) {
		return true
	}
	var graphError *semreg.Error
	return errors.As(err, &graphError) && graphError.ID == semreg.RevisionConflict
}

// expire drops the session views whose cut the cut source no longer retains, so
// the session keeps a view only as long as the source's own retention does.
func (s *ScopedInput) expire(ctx context.Context) {
	s.tree.mu.Lock()
	refs := make(map[string]graph.CutRef, len(s.views))
	for key, view := range s.views {
		refs[key] = view.Ref
	}
	s.tree.mu.Unlock()
	for key, ref := range refs {
		if _, err := s.tree.cfg.Cuts.Lookup(ctx, ref); gone(err) {
			s.tree.mu.Lock()
			delete(s.views, key)
			s.tree.mu.Unlock()
		}
	}
}

// Resume returns the commits after the cursor inside the session scope.
func (s *ScopedInput) Resume(ctx context.Context, cursor driver.Cursor, filter driver.Filter) (driver.Changes, error) {
	effective, err := s.narrow(filter)
	if err != nil {
		return driver.Changes{}, err
	}
	return s.tree.cfg.Cuts.Resume(ctx, cursor, effective)
}

// resolve returns the view the session captured for the CutRef while the cut
// source still retains that cut; a continuation that outlives the source's
// retention is a revision_conflict naming the revision.
func (s *ScopedInput) resolve(ctx context.Context, ref graph.CutRef) (CutView, error) {
	key, err := refKey(ref)
	if err != nil {
		return CutView{}, err
	}
	s.tree.mu.Lock()
	view, ok := s.views[key]
	s.tree.mu.Unlock()
	_, lookup := s.tree.cfg.Cuts.Lookup(ctx, ref)
	if gone(lookup) {
		if ok {
			s.tree.mu.Lock()
			delete(s.views, key)
			s.tree.mu.Unlock()
		}
		return CutView{}, conflict(ref.CutID, lookup)
	}
	if !ok {
		return CutView{}, treeError(semreg.DanglingReference, "cut is not captured in this session")
	}
	return view, nil
}

// RetrieveLens runs the frozen lens retrieval on the captured cut. The request
// cannot add a member, override the cut or widen the captured filter.
func (s *ScopedInput) RetrieveLens(ctx context.Context, ref graph.CutRef, request graph.LensRequest) (graph.LensResult, error) {
	view, err := s.resolve(ctx, ref)
	if err != nil {
		return graph.LensResult{}, err
	}
	return retrieveLens(view, view.Ref.CutID, request)
}

// SumMembers runs the frozen sum on the scoped lens result. A scope resource
// outside the captured filter is refused with dangling_reference before the
// frozen sum is called.
func (s *ScopedInput) SumMembers(ctx context.Context, ref graph.CutRef, request graph.LensRequest, fact semreg.DefinitionRef, scope semreg.ResourceID) (graph.MemberSum, error) {
	view, err := s.resolve(ctx, ref)
	if err != nil {
		return graph.MemberSum{}, err
	}
	if err := scope.Validate(); err != nil {
		return graph.MemberSum{}, err
	}
	if !currentIn(view, scope) {
		return graph.MemberSum{}, treeError(semreg.DanglingReference, "sum scope is outside the captured filter")
	}
	result, err := retrieveLens(view, view.Ref.CutID, request)
	if err != nil {
		return graph.MemberSum{}, err
	}
	return graph.SumMembers(result, fact, scope)
}

func currentIn(view CutView, id semreg.ResourceID) bool {
	excluded := map[semreg.AssetID]struct{}{}
	for _, asset := range view.Cut.Excluded() {
		excluded[asset] = struct{}{}
	}
	for _, member := range view.Members {
		if _, drop := excluded[member.AssetID]; drop {
			continue
		}
		for _, resource := range member.Resources {
			if resource.ResourceID == id && resource.State == semreg.ResourceCurrent {
				return true
			}
		}
	}
	return false
}
