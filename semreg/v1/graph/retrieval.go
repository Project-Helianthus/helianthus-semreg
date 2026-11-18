package graph

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Bounds of one read. A request that asks for more is bounds_exceeded
// (TRV-5, TRV-6, ENT-2).
const (
	MaxPageSize           = 4096
	MaxTraversalDepth     = 32
	MaxFanout             = 256
	MaxTraversalResources = 4096
	MaxTraversalRelations = 8192
	MaxFrontierEntries    = 4096
	MaxRetainedCuts       = 8
)

// installationRootKind is the seed's installation root (ENT-1). The catalog
// decoder does not carry an installation-root flag; sites are the current
// resources of this kind.
const installationRootKind semreg.DefinitionID = "resource.site"

const pageTokenPrefix = "pt1."

// ResourceEntry is one current resource of a listing: its kind, its home
// asset and the planes it is in.
type ResourceEntry struct {
	ResourceID semreg.ResourceID   `json:"resource_id"`
	Kind       semreg.DefinitionID `json:"kind"`
	AssetID    semreg.AssetID      `json:"asset_id"`
	Planes     []PlaneID           `json:"planes,omitempty"`
}

// ResourcePage is one page of a listing. Every page carries the served cut
// (CUT-1). NextPage is empty on the last page.
type ResourcePage struct {
	CutID        semreg.Digest    `json:"cut_id"`
	Completeness Completeness     `json:"completeness"`
	Members      []CutMember      `json:"members"`
	Excluded     []semreg.AssetID `json:"excluded,omitempty"`
	Catalog      semreg.Digest    `json:"catalog"`
	Resources    []ResourceEntry  `json:"resources"`
	NextPage     string           `json:"next_page,omitempty"`
}

// ListRequest is one listing. CutID nil or empty serves the cut the caller
// already holds; a store reads the newest cut when it is empty and that cut
// when it is set (CUT-2). Kind and Plane are optional filters (ENT-2).
type ListRequest struct {
	CutID     *semreg.Digest       `json:"cut_id,omitempty"`
	Kind      *semreg.DefinitionID `json:"kind,omitempty"`
	Plane     *PlaneID             `json:"plane,omitempty"`
	PageSize  int                  `json:"page_size"`
	PageToken string               `json:"page_token,omitempty"`
}

// CanonicalJSON is the one encoder of a listing page.
func (p ResourcePage) CanonicalJSON() ([]byte, error) {
	return semreg.CanonicalJSON(p)
}

func (p ResourcePage) Validate() error {
	if err := p.CutID.Validate(); err != nil {
		return err
	}
	if err := p.Completeness.Validate(); err != nil {
		return err
	}
	if err := p.Catalog.Validate(); err != nil {
		return err
	}
	if p.Members == nil || p.Resources == nil {
		return readError(semreg.MissingMember, "listing page")
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
	return nil
}

func (e ResourceEntry) Validate() error {
	if err := e.ResourceID.Validate(); err != nil {
		return err
	}
	if err := e.Kind.Validate(); err != nil {
		return err
	}
	return e.AssetID.Validate()
}

// ListResources returns the current resources of one cut, in ResourceID
// order, in pages of at most MaxPageSize (ENT-2). It is a pure function of
// the cut, the member snapshots the cut was assembled from, the catalog and
// the request.
func ListResources(cut *Cut, members []semreg.Snapshot, catalog *Catalog, request ListRequest) (ResourcePage, error) {
	if err := request.validate(); err != nil {
		return ResourcePage{}, err
	}
	if catalog == nil {
		return ResourcePage{}, readError(semreg.InvalidValue, "nil catalog")
	}
	index, err := indexMembers(cut, members)
	if err != nil {
		return ResourcePage{}, err
	}
	return listIndexed(cut, index, catalog, request)
}

func listIndexed(cut *Cut, index *cutIndex, catalog *Catalog, request ListRequest) (ResourcePage, error) {
	if err := request.validate(); err != nil {
		return ResourcePage{}, err
	}
	if cut == nil || catalog == nil || index == nil {
		return ResourcePage{}, readError(semreg.InvalidValue, "nil cut, catalog or index")
	}
	if err := pinMatches(request.CutID, cut.ID()); err != nil {
		return ResourcePage{}, err
	}
	identity := listIdentity{PageSize: request.PageSize}
	if request.Kind != nil {
		identity.Kind = string(*request.Kind)
	}
	if request.Plane != nil {
		identity.Plane = string(*request.Plane)
	}
	digest, err := semreg.DigestRecord(identity)
	if err != nil {
		return ResourcePage{}, err
	}
	var selected []ResourceEntry
	for _, record := range index.current {
		if request.Kind != nil && record.kind != *request.Kind {
			continue
		}
		planes := cut.PlanesOfResource(record.id)
		if request.Plane != nil && !containsPlane(planes, *request.Plane) {
			continue
		}
		selected = append(selected, ResourceEntry{
			ResourceID: record.id, Kind: record.kind, AssetID: record.asset, Planes: planes,
		})
	}
	if selected == nil {
		selected = []ResourceEntry{}
	}
	cursor, err := boundCursor(request.PageToken, cut.ID(), digest, len(selected))
	if err != nil {
		return ResourcePage{}, err
	}
	end := cursor + request.PageSize
	if end > len(selected) {
		end = len(selected)
	}
	page := cutPage(cut, catalog.Digest())
	page.Resources = append([]ResourceEntry(nil), selected[cursor:end]...)
	if page.Resources == nil {
		page.Resources = []ResourceEntry{}
	}
	if end < len(selected) {
		token, err := mintPageToken(cut.ID(), digest, end)
		if err != nil {
			return ResourcePage{}, err
		}
		page.NextPage = token
	}
	return page, nil
}

func (r ListRequest) validate() error {
	if r.PageSize < 1 {
		return readError(semreg.InvalidValue, "page size")
	}
	if r.PageSize > MaxPageSize {
		return readError(semreg.BoundsExceeded, "page size")
	}
	if r.Kind != nil {
		if err := r.Kind.Validate(); err != nil {
			return err
		}
	}
	if r.Plane != nil {
		if _, err := normalizePlanes([]PlaneID{*r.Plane}); err != nil {
			return err
		}
	}
	if r.PageToken != "" {
		if _, err := parsePageToken(r.PageToken); err != nil {
			return err
		}
	}
	return nil
}

type listIdentity struct {
	Kind     string `json:"kind,omitempty"`
	Plane    string `json:"plane,omitempty"`
	PageSize int    `json:"page_size"`
}

func (id listIdentity) Validate() error {
	if id.PageSize < 1 || id.PageSize > MaxPageSize {
		return readError(semreg.BoundsExceeded, "page size")
	}
	return nil
}

// cutIndex is the resource index of one cut. It is immutable after indexMembers.
type cutIndex struct {
	byID    map[semreg.ResourceID]resourceRecord
	current []resourceRecord
}

type resourceRecord struct {
	id      semreg.ResourceID
	kind    semreg.DefinitionID
	asset   semreg.AssetID
	current bool
}

func indexMembers(cut *Cut, members []semreg.Snapshot) (*cutIndex, error) {
	if cut == nil {
		return nil, readError(semreg.InvalidValue, "nil cut")
	}
	want := cut.Members()
	if len(members) != len(want) {
		return nil, readError(semreg.InvalidValue, "member snapshots do not match the cut")
	}
	got := append([]semreg.Snapshot(nil), members...)
	sort.Slice(got, func(i, j int) bool {
		if got[i].AssetID != got[j].AssetID {
			return got[i].AssetID < got[j].AssetID
		}
		return got[i].SnapshotID < got[j].SnapshotID
	})
	excluded := map[semreg.AssetID]struct{}{}
	for _, asset := range cut.Excluded() {
		excluded[asset] = struct{}{}
	}
	index := &cutIndex{byID: map[semreg.ResourceID]resourceRecord{}}
	for i, member := range want {
		if got[i].AssetID != member.AssetID || got[i].SnapshotID != member.SnapshotID {
			return nil, readError(semreg.InvalidValue, "member snapshots do not match the cut")
		}
		_, drop := excluded[member.AssetID]
		for _, resource := range got[i].Resources {
			record := resourceRecord{
				id: resource.ResourceID, kind: resource.Kind, asset: member.AssetID,
				current: resource.State == semreg.ResourceCurrent && !drop,
			}
			index.add(record)
		}
	}
	for _, record := range index.byID {
		if record.current {
			index.current = append(index.current, record)
		}
	}
	sort.Slice(index.current, func(i, j int) bool { return index.current[i].id < index.current[j].id })
	return index, nil
}

func (index *cutIndex) add(record resourceRecord) {
	previous, ok := index.byID[record.id]
	if !ok || (record.current && !previous.current) {
		index.byID[record.id] = record
	}
}

func cutPage(cut *Cut, catalog semreg.Digest) ResourcePage {
	page := ResourcePage{
		CutID: cut.ID(), Completeness: cut.Completeness(), Members: cut.Members(), Catalog: catalog,
		Resources: []ResourceEntry{},
	}
	if excluded := cut.Excluded(); len(excluded) != 0 {
		page.Excluded = excluded
	}
	return page
}

func pinMatches(pin *semreg.Digest, id semreg.Digest) error {
	if pin != nil && *pin != "" && *pin != id {
		return readError(semreg.RevisionConflict, "pinned cut is not the served cut")
	}
	return nil
}

func normalizePlanes(planes []PlaneID) ([]PlaneID, error) {
	seen := map[PlaneID]struct{}{}
	for _, plane := range planes {
		switch plane {
		case PlanePhysical, PlaneEnergy, PlaneHydraulic, PlaneControl, PlaneDiagnostic, PlaneProvenance:
			seen[plane] = struct{}{}
		default:
			return nil, readError(semreg.InvalidEnum, "plane")
		}
	}
	var out []PlaneID
	for _, plane := range []PlaneID{PlanePhysical, PlaneEnergy, PlaneHydraulic, PlaneControl, PlaneDiagnostic, PlaneProvenance} {
		if _, ok := seen[plane]; ok {
			out = append(out, plane)
		}
	}
	return out, nil
}

func sortDefinitions(definitions []semreg.DefinitionRef) []semreg.DefinitionRef {
	out := append([]semreg.DefinitionRef(nil), definitions...)
	sort.Slice(out, func(i, j int) bool { return canonicalKey(out[i]) < canonicalKey(out[j]) })
	unique := out[:0]
	for _, definition := range out {
		if len(unique) != 0 && canonicalKey(unique[len(unique)-1]) == canonicalKey(definition) {
			continue
		}
		unique = append(unique, definition)
	}
	if len(unique) == 0 {
		return nil
	}
	return unique
}

type tokenBody struct {
	CutID   semreg.Digest `json:"cut_id"`
	Request semreg.Digest `json:"request"`
	Cursor  int           `json:"cursor"`
}

func (t tokenBody) Validate() error {
	if err := t.CutID.Validate(); err != nil {
		return err
	}
	if err := t.Request.Validate(); err != nil {
		return err
	}
	if t.Cursor < 0 {
		return readError(semreg.InvalidValue, "page token cursor")
	}
	return nil
}

func mintPageToken(cutID, request semreg.Digest, cursor int) (string, error) {
	raw, err := semreg.CanonicalJSON(tokenBody{CutID: cutID, Request: request, Cursor: cursor})
	if err != nil {
		return "", err
	}
	return pageTokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func parsePageToken(token string) (tokenBody, error) {
	if len(token) <= len(pageTokenPrefix) || token[:len(pageTokenPrefix)] != pageTokenPrefix {
		return tokenBody{}, readError(semreg.InvalidValue, "malformed page token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token[len(pageTokenPrefix):])
	if err != nil {
		return tokenBody{}, readError(semreg.InvalidValue, "malformed page token")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body tokenBody
	if err := decoder.Decode(&body); err != nil {
		return tokenBody{}, readError(semreg.InvalidValue, "malformed page token")
	}
	if _, err := decoder.Token(); !errorsIsEOF(err) {
		return tokenBody{}, readError(semreg.InvalidValue, "malformed page token")
	}
	if err := body.Validate(); err != nil {
		return tokenBody{}, readError(semreg.InvalidValue, "malformed page token")
	}
	return body, nil
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}

// boundCursor checks a page token against the served cut and the request
// digest (TRV-7). An empty token is the first page. limit is one past the
// last acceptable cursor; the caller tightens it once the page length is known.
func boundCursor(token string, cutID, request semreg.Digest, limit int) (int, error) {
	if token == "" {
		return 0, nil
	}
	body, err := parsePageToken(token)
	if err != nil {
		return 0, err
	}
	if body.CutID != cutID || body.Request != request {
		return 0, readError(semreg.RevisionConflict, "page token is for another cut or request")
	}
	if body.Cursor > limit {
		return 0, readError(semreg.InvalidValue, "page token cursor")
	}
	return body.Cursor, nil
}

func readError(id semreg.ErrorID, detail string) error {
	return &semreg.Error{ID: id, Detail: "graph read: " + detail}
}
