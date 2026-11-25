package graph

import (
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// TraversalDirection is the direction of a walk along a definition's own
// from and to (TRV-2). Orientation of the core type does not flip it.
type TraversalDirection string

// TraversalDirection values.
const (
	DirectionOutgoing TraversalDirection = "outgoing"
	DirectionIncoming TraversalDirection = "incoming"
	DirectionBoth     TraversalDirection = "both"
)

// EntryKind is the one place a traversal starts (ENT-1).
type EntryKind string

// EntryKind values.
const (
	EntryResource  EntryKind = "resource"
	EntryLensRoots EntryKind = "lens_roots"
	EntrySites     EntryKind = "sites"
)

// EntryPoint is the start of a traversal. Resource is set for a resource
// entry. Lens is set for lens roots and names the same lens as the request.
type EntryPoint struct {
	Kind     EntryKind           `json:"kind"`
	Resource semreg.ResourceID   `json:"resource,omitempty"`
	Lens     semreg.DefinitionID `json:"lens,omitempty"`
}

// FrontierReason is the bound that stopped a walk at a resource (TRV-6).
type FrontierReason string

// FrontierReason values.
const (
	FrontierDepthLimit  FrontierReason = "depth_limit"
	FrontierFanoutLimit FrontierReason = "fanout_limit"
)

// FrontierEntry is one resource a walk stopped at, with the number of
// instances that were not followed.
type FrontierEntry struct {
	ResourceID  semreg.ResourceID `json:"resource_id"`
	Reason      FrontierReason    `json:"reason"`
	NotFollowed int               `json:"not_followed"`
}

// Entry status values. An empty walk says so instead of inventing a root (ENT-1).
const (
	EntryStatusNoSite = "no_site"
	EntryStatusNoRoot = "no_lens_root"
)

// ReachedResource is one resource of a traversal, at its smallest depth (TRV-4).
type ReachedResource struct {
	ResourceID semreg.ResourceID   `json:"resource_id"`
	Kind       semreg.DefinitionID `json:"kind"`
	AssetID    semreg.AssetID      `json:"asset_id"`
	Depth      int                 `json:"depth"`
	Planes     []PlaneID           `json:"planes,omitempty"`
}

// TraversalPage is one page of a traversal. It lists resources and relation
// instances and has no path (TRV-8). Every page carries the served cut (CUT-1).
type TraversalPage struct {
	CutID        semreg.Digest     `json:"cut_id"`
	Completeness Completeness      `json:"completeness"`
	Members      []CutMember       `json:"members"`
	Excluded     []semreg.AssetID  `json:"excluded,omitempty"`
	Catalog      semreg.Digest     `json:"catalog"`
	Entry        EntryPoint        `json:"entry"`
	EntryStatus  string            `json:"entry_status,omitempty"`
	Resources    []ReachedResource `json:"resources"`
	Relations    []CutRelation     `json:"relations,omitempty"`
	Frontier     []FrontierEntry   `json:"frontier,omitempty"`
	NextPage     string            `json:"next_page,omitempty"`
}

// TraversalRequest is one walk: an entry, either a lens or planes with an
// optional set of relation definitions, a direction, a depth and a page size
// (ENT-1, TRV-1). Planes alone follow every member relation of those planes.
type TraversalRequest struct {
	CutID       *semreg.Digest         `json:"cut_id,omitempty"`
	Entry       EntryPoint             `json:"entry"`
	Lens        semreg.DefinitionID    `json:"lens,omitempty"`
	Planes      []PlaneID              `json:"planes,omitempty"`
	Definitions []semreg.DefinitionRef `json:"definitions,omitempty"`
	Direction   TraversalDirection     `json:"direction"`
	Depth       int                    `json:"depth"`
	PageSize    int                    `json:"page_size"`
	PageToken   string                 `json:"page_token,omitempty"`
}

// CanonicalJSON is the one encoder of a traversal page.
func (p TraversalPage) CanonicalJSON() ([]byte, error) {
	return semreg.CanonicalJSON(p)
}

func (p TraversalPage) Validate() error {
	if err := p.CutID.Validate(); err != nil {
		return err
	}
	if err := p.Completeness.Validate(); err != nil {
		return err
	}
	if err := p.Catalog.Validate(); err != nil {
		return err
	}
	if err := p.Entry.Validate(); err != nil {
		return err
	}
	if p.Members == nil || p.Resources == nil {
		return readError(semreg.MissingMember, "traversal page")
	}
	for _, member := range p.Members {
		if err := member.Validate(); err != nil {
			return err
		}
	}
	for _, resource := range p.Resources {
		if err := resource.Validate(); err != nil {
			return err
		}
	}
	for _, relation := range p.Relations {
		if err := relation.Validate(); err != nil {
			return err
		}
	}
	for _, entry := range p.Frontier {
		if err := entry.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (e EntryPoint) Validate() error {
	switch e.Kind {
	case EntryResource, EntryLensRoots, EntrySites:
	default:
		return readError(semreg.InvalidEnum, "entry")
	}
	if e.Resource != "" {
		if err := e.Resource.Validate(); err != nil {
			return err
		}
	}
	if e.Lens != "" {
		if err := e.Lens.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r ReachedResource) Validate() error {
	if err := r.ResourceID.Validate(); err != nil {
		return err
	}
	if err := r.Kind.Validate(); err != nil {
		return err
	}
	if r.Depth < 0 {
		return readError(semreg.InvalidValue, "depth")
	}
	return r.AssetID.Validate()
}

func (e FrontierEntry) Validate() error {
	if err := e.ResourceID.Validate(); err != nil {
		return err
	}
	switch e.Reason {
	case FrontierDepthLimit, FrontierFanoutLimit:
	default:
		return readError(semreg.InvalidEnum, "frontier reason")
	}
	if e.NotFollowed < 1 {
		return readError(semreg.InvalidValue, "frontier count")
	}
	return nil
}

// Traverse walks one cut from one entry and returns one page. It is a pure
// function of the cut, the member snapshots, the catalog and the request.
// Only qualified coherent instances are followed (TRV-1).
func Traverse(cut *Cut, members []semreg.Snapshot, catalog *Catalog, request TraversalRequest) (TraversalPage, error) {
	if err := request.validate(); err != nil {
		return TraversalPage{}, err
	}
	if catalog == nil {
		return TraversalPage{}, readError(semreg.InvalidValue, "nil catalog")
	}
	index, err := indexMembers(cut, members)
	if err != nil {
		return TraversalPage{}, err
	}
	return traverseIndexed(cut, index, catalog, request)
}

func traverseIndexed(cut *Cut, index *cutIndex, catalog *Catalog, request TraversalRequest) (TraversalPage, error) {
	if err := request.validate(); err != nil {
		return TraversalPage{}, err
	}
	if cut == nil || catalog == nil || index == nil {
		return TraversalPage{}, readError(semreg.InvalidValue, "nil cut, catalog or index")
	}
	if len(cut.Members()) > MaxCutMembers {
		return TraversalPage{}, readError(semreg.BoundsExceeded, "cut members")
	}
	if err := pinMatches(request.CutID, cut.ID()); err != nil {
		return TraversalPage{}, err
	}
	digest, err := semreg.DigestRecord(request.identity())
	if err != nil {
		return TraversalPage{}, err
	}
	cursor, err := boundCursor(request.PageToken, cut.ID(), digest, MaxTraversalResources)
	if err != nil {
		return TraversalPage{}, err
	}
	followed, depthBound, entry, status, err := resolveWalk(catalog, index, request)
	if err != nil {
		return TraversalPage{}, err
	}
	if request.Depth > depthBound {
		return TraversalPage{}, readError(semreg.BoundsExceeded, "depth")
	}
	walk, err := walkCut(cut, index, followed, request.Direction, request.Depth, entry)
	if err != nil {
		return TraversalPage{}, err
	}
	if cursor > len(walk.resources) {
		return TraversalPage{}, readError(semreg.InvalidValue, "page token cursor")
	}
	return pageWalk(cut, catalog.Digest(), normalizedEntry(request), status, walk, cursor, request.PageSize, digest)
}

func normalizedEntry(request TraversalRequest) EntryPoint {
	switch request.Entry.Kind {
	case EntryResource:
		return EntryPoint{Kind: EntryResource, Resource: request.Entry.Resource}
	case EntryLensRoots:
		return EntryPoint{Kind: EntryLensRoots, Lens: request.Lens}
	default:
		return EntryPoint{Kind: EntrySites}
	}
}

func (r TraversalRequest) validate() error {
	switch r.Direction {
	case DirectionOutgoing, DirectionIncoming, DirectionBoth:
	default:
		return readError(semreg.InvalidEnum, "direction")
	}
	if err := r.Entry.validateShape(r.Lens); err != nil {
		return err
	}
	lensSet := r.Lens != ""
	planesSet := len(r.Planes) != 0 || len(r.Definitions) != 0
	if lensSet == planesSet {
		return readError(semreg.InvalidValue, "a traversal names a lens or planes, not both and not neither")
	}
	if r.Lens != "" {
		if err := r.Lens.Validate(); err != nil {
			return err
		}
	}
	if _, err := normalizePlanes(r.Planes); err != nil {
		return err
	}
	for _, definition := range r.Definitions {
		if err := definition.Validate(); err != nil {
			return err
		}
	}
	if r.Depth < 0 {
		return readError(semreg.InvalidValue, "depth")
	}
	if r.PageSize < 1 {
		return readError(semreg.InvalidValue, "page size")
	}
	if r.PageSize > MaxPageSize {
		return readError(semreg.BoundsExceeded, "page size")
	}
	if r.PageToken != "" {
		if _, err := parsePageToken(r.PageToken); err != nil {
			return err
		}
	}
	return nil
}

func (e EntryPoint) validateShape(lens semreg.DefinitionID) error {
	switch e.Kind {
	case EntryResource:
		if e.Resource == "" || e.Lens != "" {
			return readError(semreg.InvalidValue, "resource entry")
		}
		return e.Resource.Validate()
	case EntryLensRoots:
		if e.Resource != "" || lens == "" || (e.Lens != "" && e.Lens != lens) {
			return readError(semreg.InvalidValue, "lens root entry")
		}
		return nil
	case EntrySites:
		if e.Resource != "" || e.Lens != "" {
			return readError(semreg.InvalidValue, "sites entry")
		}
		return nil
	default:
		return readError(semreg.InvalidEnum, "entry")
	}
}

func (r TraversalRequest) identity() traverseIdentity {
	entry := EntryPoint{Kind: r.Entry.Kind}
	if r.Entry.Kind == EntryResource {
		entry.Resource = r.Entry.Resource
	}
	identity := traverseIdentity{
		EntryKind: string(entry.Kind), Resource: string(entry.Resource), Lens: string(r.Lens),
		Planes: normalizePlanesMust(r.Planes), Definitions: sortDefinitions(r.Definitions),
		Direction: string(r.Direction), Depth: r.Depth, PageSize: r.PageSize,
	}
	return identity
}

func normalizePlanesMust(planes []PlaneID) []PlaneID {
	out, _ := normalizePlanes(planes)
	return out
}

type traverseIdentity struct {
	EntryKind   string                 `json:"entry_kind"`
	Resource    string                 `json:"resource,omitempty"`
	Lens        string                 `json:"lens,omitempty"`
	Planes      []PlaneID              `json:"planes,omitempty"`
	Definitions []semreg.DefinitionRef `json:"definitions,omitempty"`
	Direction   string                 `json:"direction"`
	Depth       int                    `json:"depth"`
	PageSize    int                    `json:"page_size"`
}

func (id traverseIdentity) Validate() error {
	switch EntryKind(id.EntryKind) {
	case EntryResource, EntryLensRoots, EntrySites:
	default:
		return readError(semreg.InvalidEnum, "entry")
	}
	switch TraversalDirection(id.Direction) {
	case DirectionOutgoing, DirectionIncoming, DirectionBoth:
	default:
		return readError(semreg.InvalidEnum, "direction")
	}
	if id.Depth < 0 || id.Depth > MaxTraversalDepth || id.PageSize < 1 || id.PageSize > MaxPageSize {
		return readError(semreg.BoundsExceeded, "traversal identity")
	}
	return nil
}

// resolveWalk returns the definitions to follow, the depth bound, the entry
// resources and an entry status when the entry is empty.
func resolveWalk(catalog *Catalog, index *cutIndex, request TraversalRequest) (map[string]struct{}, int, []semreg.ResourceID, string, error) {
	var followed []semreg.DefinitionRef
	var depthBound int
	var entries []semreg.ResourceID
	status := ""
	if request.Lens != "" {
		lens, ok := lensByID(catalog, request.Lens)
		if !ok {
			return nil, 0, nil, "", readError(semreg.MembershipMissing, "lens")
		}
		var err error
		followed, err = lensDefinitions(catalog, lens)
		if err != nil {
			return nil, 0, nil, "", err
		}
		depthBound = lens.MaxDepth
		if depthBound == 0 {
			depthBound = MaxTraversalDepth
		}
		switch request.Entry.Kind {
		case EntryLensRoots:
			entries = resourcesOfKinds(index, lens.RootKinds)
			if len(entries) == 0 {
				status = EntryStatusNoRoot
			}
		case EntryResource:
			var err error
			entries, err = oneCurrent(index, request.Entry.Resource)
			if err != nil {
				return nil, 0, nil, "", err
			}
		case EntrySites:
			entries = resourcesOfKinds(index, []semreg.DefinitionID{installationRootKind})
			if len(entries) == 0 {
				status = EntryStatusNoSite
			}
		}
	} else {
		planes, err := normalizePlanes(request.Planes)
		if err != nil {
			return nil, 0, nil, "", err
		}
		followed, err = planeDefinitions(catalog, planes, request.Definitions)
		if err != nil {
			return nil, 0, nil, "", err
		}
		depthBound = MaxTraversalDepth
		switch request.Entry.Kind {
		case EntryResource:
			entries, err = oneCurrent(index, request.Entry.Resource)
			if err != nil {
				return nil, 0, nil, "", err
			}
		case EntrySites:
			entries = resourcesOfKinds(index, []semreg.DefinitionID{installationRootKind})
			if len(entries) == 0 {
				status = EntryStatusNoSite
			}
		default:
			return nil, 0, nil, "", readError(semreg.InvalidValue, "lens root entry")
		}
	}
	set := map[string]struct{}{}
	for _, definition := range followed {
		set[canonicalKey(definition)] = struct{}{}
	}
	return set, depthBound, entries, status, nil
}

func lensByID(catalog *Catalog, id semreg.DefinitionID) (Lens, bool) {
	for _, lens := range catalog.Lenses() {
		if lens.ID == id {
			return lens, true
		}
	}
	return Lens{}, false
}

func lensDefinitions(catalog *Catalog, lens Lens) ([]semreg.DefinitionRef, error) {
	if len(lens.Relations) == 0 {
		return relationsOnPlanes(catalog, lens.Planes), nil
	}
	var out []semreg.DefinitionRef
	for _, ref := range lens.Relations {
		definition, ok := ref.DefinitionRef()
		if !ok {
			return nil, readError(semreg.MembershipMissing, "lens relation")
		}
		if _, ok := memberRelation(catalog, definition); !ok {
			return nil, readError(semreg.MembershipMissing, "lens relation")
		}
		out = append(out, definition)
	}
	return sortDefinitions(out), nil
}

func planeDefinitions(catalog *Catalog, planes []PlaneID, requested []semreg.DefinitionRef) ([]semreg.DefinitionRef, error) {
	if len(requested) == 0 {
		return relationsOnPlanes(catalog, planes), nil
	}
	for _, definition := range sortDefinitions(requested) {
		element, ok := memberRelation(catalog, definition)
		if !ok || !intersectPlanes(element.Planes, planes) {
			return nil, readError(semreg.MembershipMissing, "definition outside the requested planes")
		}
	}
	return sortDefinitions(requested), nil
}

func relationsOnPlanes(catalog *Catalog, planes []PlaneID) []semreg.DefinitionRef {
	var out []semreg.DefinitionRef
	for _, element := range catalog.Elements() {
		if element.ElementKind != KindRelation || element.Disposition != DispositionMember || element.Relation == nil {
			continue
		}
		if !intersectPlanes(element.Planes, planes) {
			continue
		}
		definition, ok := element.Ref.DefinitionRef()
		if !ok {
			continue
		}
		out = append(out, definition)
	}
	return sortDefinitions(out)
}

func memberRelation(catalog *Catalog, definition semreg.DefinitionRef) (Element, bool) {
	element, ok := catalog.PackEntry(definition)
	if !ok || element.ElementKind != KindRelation || element.Disposition != DispositionMember || element.Relation == nil {
		return Element{}, false
	}
	return element, true
}

func resourcesOfKinds(index *cutIndex, kinds []semreg.DefinitionID) []semreg.ResourceID {
	allowed := map[semreg.DefinitionID]struct{}{}
	for _, kind := range kinds {
		allowed[kind] = struct{}{}
	}
	var out []semreg.ResourceID
	for _, record := range index.current {
		if _, ok := allowed[record.kind]; ok {
			out = append(out, record.id)
		}
	}
	return out
}

func oneCurrent(index *cutIndex, id semreg.ResourceID) ([]semreg.ResourceID, error) {
	record, ok := index.byID[id]
	if !ok || !record.current {
		return nil, readError(semreg.DanglingReference, "entry resource")
	}
	return []semreg.ResourceID{id}, nil
}

type walkEdge struct {
	key      string
	def      string
	relation CutRelation
}

func (e walkEdge) step(at semreg.ResourceID) semreg.ResourceID {
	if e.relation.From == at {
		return e.relation.To
	}
	return e.relation.From
}

type walkResult struct {
	resources []ReachedResource
	relations []walkedRelation
	frontier  []FrontierEntry
}

type walkedRelation struct {
	source   semreg.ResourceID
	relation CutRelation
	key      string
}

func walkCut(cut *Cut, index *cutIndex, followed map[string]struct{}, direction TraversalDirection, depth int, entries []semreg.ResourceID) (walkResult, error) {
	adjacent := adjacency(cut, followed, direction)
	current := append([]semreg.ResourceID(nil), entries...)
	sort.Slice(current, func(i, j int) bool { return current[i] < current[j] })
	seenNode := map[semreg.ResourceID]int{}
	for _, id := range current {
		seenNode[id] = 0
	}
	seenRelation := map[string]struct{}{}
	var result walkResult
	for level := 0; ; level++ {
		var next []semreg.ResourceID
		for _, id := range current {
			result.resources = append(result.resources, reached(index, cut, id, level))
			notFollowed, reason, edges := expand(adjacent[id], seenRelation, level == depth)
			if notFollowed > 0 {
				result.frontier = append(result.frontier, FrontierEntry{ResourceID: id, Reason: reason, NotFollowed: notFollowed})
			}
			for _, edge := range edges {
				seenRelation[edge.key] = struct{}{}
				result.relations = append(result.relations, walkedRelation{source: id, relation: edge.relation, key: edge.key})
				if level == depth {
					continue
				}
				other := edge.step(id)
				if _, ok := seenNode[other]; ok {
					continue
				}
				seenNode[other] = level + 1
				next = append(next, other)
			}
		}
		if len(result.frontier) > MaxFrontierEntries {
			return walkResult{}, readError(semreg.BoundsExceeded, "frontier")
		}
		if len(result.resources) > MaxTraversalResources {
			return walkResult{}, readError(semreg.BoundsExceeded, "resources")
		}
		if len(result.relations) > MaxTraversalRelations {
			return walkResult{}, readError(semreg.BoundsExceeded, "relations")
		}
		if level == depth || len(next) == 0 {
			break
		}
		sort.Slice(next, func(i, j int) bool { return next[i] < next[j] })
		current = uniqueIDs(next)
	}
	sort.Slice(result.resources, func(i, j int) bool {
		if result.resources[i].Depth != result.resources[j].Depth {
			return result.resources[i].Depth < result.resources[j].Depth
		}
		return result.resources[i].ResourceID < result.resources[j].ResourceID
	})
	sort.Slice(result.relations, func(i, j int) bool { return result.relations[i].key < result.relations[j].key })
	sort.Slice(result.frontier, func(i, j int) bool {
		if result.frontier[i].Reason != result.frontier[j].Reason {
			return result.frontier[i].Reason < result.frontier[j].Reason
		}
		return result.frontier[i].ResourceID < result.frontier[j].ResourceID
	})
	return result, nil
}

func reached(index *cutIndex, cut *Cut, id semreg.ResourceID, depth int) ReachedResource {
	resource := ReachedResource{ResourceID: id, Depth: depth, Planes: cut.PlanesOfResource(id)}
	if record, ok := index.byID[id]; ok {
		resource.Kind = record.kind
		resource.AssetID = record.asset
	}
	return resource
}

func expand(edges []walkEdge, seen map[string]struct{}, atDepth bool) (int, FrontierReason, []walkEdge) {
	notFollowed := 0
	var followed []walkEdge
	for i := 0; i < len(edges); {
		j := i + 1
		for j < len(edges) && edges[j].def == edges[i].def {
			j++
		}
		var fresh []walkEdge
		for _, edge := range edges[i:j] {
			if _, ok := seen[edge.key]; ok {
				continue
			}
			fresh = append(fresh, edge)
		}
		i = j
		if atDepth {
			notFollowed += len(fresh)
			continue
		}
		n := len(fresh)
		if n > MaxFanout {
			notFollowed += n - MaxFanout
			n = MaxFanout
		}
		followed = append(followed, fresh[:n]...)
	}
	reason := FrontierFanoutLimit
	if atDepth {
		reason = FrontierDepthLimit
	}
	return notFollowed, reason, followed
}

func adjacency(cut *Cut, followed map[string]struct{}, direction TraversalDirection) map[semreg.ResourceID][]walkEdge {
	out := map[semreg.ResourceID][]walkEdge{}
	for _, relation := range cut.Relations() {
		if relation.State != semreg.LinkQualified || relation.Coherence != CoherenceCoherent {
			continue
		}
		def := canonicalKey(relation.Definition)
		if _, ok := followed[def]; !ok {
			continue
		}
		edge := walkEdge{key: relationLookupKey(def, relation.From, relation.To), def: def, relation: relation}
		switch direction {
		case DirectionOutgoing:
			out[relation.From] = append(out[relation.From], edge)
		case DirectionIncoming:
			out[relation.To] = append(out[relation.To], edge)
		default:
			out[relation.From] = append(out[relation.From], edge)
			if relation.To != relation.From {
				out[relation.To] = append(out[relation.To], edge)
			}
		}
	}
	for id, edges := range out {
		sort.Slice(edges, func(i, j int) bool { return edges[i].key < edges[j].key })
		out[id] = edges
	}
	return out
}

func uniqueIDs(ids []semreg.ResourceID) []semreg.ResourceID {
	if len(ids) == 0 {
		return nil
	}
	out := ids[:1]
	for _, id := range ids[1:] {
		if id != out[len(out)-1] {
			out = append(out, id)
		}
	}
	return out
}

func pageWalk(cut *Cut, catalog semreg.Digest, entry EntryPoint, status string, walk walkResult, cursor, pageSize int, digest semreg.Digest) (TraversalPage, error) {
	listing := cutPage(cut, catalog)
	page := TraversalPage{
		CutID: listing.CutID, Completeness: listing.Completeness, Members: listing.Members,
		Excluded: listing.Excluded, Catalog: listing.Catalog, Entry: entry, EntryStatus: status,
		Resources: []ReachedResource{},
	}
	end := cursor + pageSize
	if end > len(walk.resources) {
		end = len(walk.resources)
	}
	onPage := map[semreg.ResourceID]struct{}{}
	for _, resource := range walk.resources[cursor:end] {
		page.Resources = append(page.Resources, resource)
		onPage[resource.ResourceID] = struct{}{}
	}
	if page.Resources == nil {
		page.Resources = []ReachedResource{}
	}
	for _, relation := range walk.relations {
		if _, ok := onPage[relation.source]; !ok {
			continue
		}
		page.Relations = append(page.Relations, relation.relation)
	}
	for _, frontier := range walk.frontier {
		if _, ok := onPage[frontier.ResourceID]; !ok {
			continue
		}
		page.Frontier = append(page.Frontier, frontier)
	}
	if end < len(walk.resources) {
		token, err := mintPageToken(cut.ID(), digest, end)
		if err != nil {
			return TraversalPage{}, err
		}
		page.NextPage = token
	}
	return page, nil
}
