package graph

import (
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// LensResultContract names the lens result document.
const LensResultContract = "helianthus.semantic.lens-result/v1"

// NotIncludedReason says why an asset is absent from the cut a lens read.
type NotIncludedReason string

// NotIncludedReason values.
const (
	NotIncludedSourceUnavailable   NotIncludedReason = "source_unavailable"
	NotIncludedPublicationWithheld NotIncludedReason = "publication_withheld"
	NotIncludedExcluded            NotIncludedReason = "excluded"
	NotIncludedUngraphed           NotIncludedReason = "ungraphed"
)

// NotIncludedAsset is one asset a lens read does not hold, with the reason.
type NotIncludedAsset struct {
	AssetID semreg.AssetID    `json:"asset_id"`
	Reason  NotIncludedReason `json:"reason"`
}

// CutRef is the cut a lens result was read from (CUT-1).
type CutRef struct {
	CutID        semreg.Digest      `json:"cut_id"`
	Completeness Completeness       `json:"completeness"`
	Members      []CutMember        `json:"members"`
	NotIncluded  []NotIncludedAsset `json:"not_included,omitempty"`
}

// LensResource is one reached resource, at its smallest depth, in the lens
// planes it is in (LRS-1).
type LensResource struct {
	Resource semreg.Resource `json:"resource"`
	Depth    int             `json:"depth"`
	AssetID  semreg.AssetID  `json:"asset_id"`
	Planes   []PlaneID       `json:"planes,omitempty"`
}

// LensRelation is one relation instance shown by a lens. InPlane is false
// for a candidate, rejected, conflicting or non-coherent instance; those
// instances are not followed and add no resource (LRS-2).
type LensRelation struct {
	AssetID     semreg.AssetID       `json:"asset_id"`
	Definition  semreg.DefinitionRef `json:"definition"`
	From        semreg.ResourceID    `json:"from"`
	To          semreg.ResourceID    `json:"to"`
	State       semreg.LinkState     `json:"state"`
	Coherence   RelationCoherence    `json:"coherence"`
	InPlane     bool                 `json:"in_plane"`
	CoreType    CoreType             `json:"core_type,omitempty"`
	Orientation Orientation          `json:"orientation,omitempty"`
}

// LensSign is the sign reference copied onto a lens fact so a sum does not
// need the catalog (SUM-5).
type LensSign struct {
	NativeConvention bool                  `json:"native_convention,omitempty"`
	Relation         *semreg.DefinitionRef `json:"relation,omitempty"`
}

// LensFact is one fact whose subject is a reached resource and whose
// definition is in a lens plane. Candidates, conflicts and the evaluation
// view are the kernel's, unchanged (LRS-1).
type LensFact struct {
	AssetID     semreg.AssetID         `json:"asset_id"`
	Definition  semreg.DefinitionRef   `json:"definition"`
	Key         semreg.FactKey         `json:"key"`
	Subject     semreg.ResourceID      `json:"subject"`
	Candidates  []semreg.FactCandidate `json:"candidates"`
	Conflicts   []semreg.Conflict      `json:"conflicts"`
	Revision    semreg.Uint64          `json:"revision"`
	Evaluation  []semreg.EvaluatedFact `json:"evaluation"`
	Aggregation Aggregation            `json:"aggregation"`
	Direction   Direction              `json:"direction"`
	Sign        *LensSign              `json:"sign_reference,omitempty"`
}

// LensResult is the document a lens retrieval returns.
type LensResult struct {
	Contract     string                      `json:"contract"`
	Lens         semreg.DefinitionID         `json:"lens"`
	Catalog      semreg.Digest               `json:"catalog"`
	Cut          CutRef                      `json:"cut"`
	Entry        EntryPoint                  `json:"entry"`
	EntryStatus  string                      `json:"entry_status,omitempty"`
	Resources    []LensResource              `json:"resources"`
	Relations    []LensRelation              `json:"relations,omitempty"`
	Absences     []semreg.RelationAbsence    `json:"absences,omitempty"`
	Facts        []LensFact                  `json:"facts,omitempty"`
	Services     []semreg.ServiceInstance    `json:"services,omitempty"`
	Capabilities []semreg.CapabilityInstance `json:"capabilities,omitempty"`
	Findings     []CutFinding                `json:"findings,omitempty"`
	Frontier     []FrontierEntry             `json:"frontier,omitempty"`
	NextPage     *string                     `json:"next_page,omitempty"`
}

// LensRequest is one lens retrieval over a retained cut. Members are the
// snapshots the cut was assembled from; the store does not keep them.
type LensRequest struct {
	CutID       *semreg.Digest      `json:"cut_id,omitempty"`
	Lens        semreg.DefinitionID `json:"lens"`
	Entry       EntryPoint          `json:"entry"`
	Direction   TraversalDirection  `json:"direction"`
	Depth       int                 `json:"depth"`
	PageSize    int                 `json:"page_size"`
	PageToken   string              `json:"page_token,omitempty"`
	Members     []semreg.Snapshot   `json:"-"`
	NotIncluded []NotIncludedAsset  `json:"not_included,omitempty"`
}

// CanonicalJSON is the one encoder of a lens result.
func (r LensResult) CanonicalJSON() ([]byte, error) {
	return semreg.CanonicalJSON(r)
}

func (r LensResult) Validate() error {
	if r.Contract != LensResultContract {
		return readError(semreg.InvalidContract, "lens result contract")
	}
	if err := r.Lens.Validate(); err != nil {
		return err
	}
	if err := r.Catalog.Validate(); err != nil {
		return err
	}
	if err := r.Cut.validate(); err != nil {
		return err
	}
	if err := r.Entry.Validate(); err != nil {
		return err
	}
	switch r.EntryStatus {
	case "", EntryStatusNoSite, EntryStatusNoRoot:
	default:
		return readError(semreg.InvalidEnum, "entry status")
	}
	if r.Resources == nil {
		return readError(semreg.MissingMember, "lens resources")
	}
	for _, resource := range r.Resources {
		if err := resource.Resource.Validate(); err != nil {
			return err
		}
		if resource.Depth < 0 {
			return readError(semreg.InvalidValue, "depth")
		}
		if err := resource.AssetID.Validate(); err != nil {
			return err
		}
	}
	for _, relation := range r.Relations {
		if err := relation.validate(); err != nil {
			return err
		}
	}
	for _, absence := range r.Absences {
		if err := absence.Validate(); err != nil {
			return err
		}
	}
	for _, entry := range r.Frontier {
		if err := entry.Validate(); err != nil {
			return err
		}
	}
	if r.NextPage != nil && *r.NextPage != "" {
		if _, err := parsePageToken(*r.NextPage); err != nil {
			return err
		}
	}
	return nil
}

func (c CutRef) validate() error {
	if err := c.CutID.Validate(); err != nil {
		return err
	}
	if err := c.Completeness.Validate(); err != nil {
		return err
	}
	if c.Members == nil {
		return readError(semreg.MissingMember, "lens cut members")
	}
	for _, member := range c.Members {
		if err := member.Validate(); err != nil {
			return err
		}
	}
	for _, row := range c.NotIncluded {
		if err := row.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r LensRelation) validate() error {
	if err := r.AssetID.Validate(); err != nil {
		return err
	}
	if err := (semreg.RelationKey{Definition: r.Definition, From: r.From, To: r.To}).Validate(); err != nil {
		return err
	}
	switch r.State {
	case semreg.LinkCandidate, semreg.LinkQualified, semreg.LinkRejected, semreg.LinkConflict:
	default:
		return readError(semreg.InvalidEnum, "relation state")
	}
	return r.Coherence.Validate()
}

func (a NotIncludedAsset) validate() error {
	if err := a.AssetID.Validate(); err != nil {
		return err
	}
	switch a.Reason {
	case NotIncludedSourceUnavailable, NotIncludedPublicationWithheld, NotIncludedExcluded, NotIncludedUngraphed:
		return nil
	default:
		return readError(semreg.InvalidEnum, "not-included reason")
	}
}

// RetrieveLens returns one page of the lens result for a retained cut
// (LRS-1 to LRS-6). Facts carry the kernel evaluation of the supplied
// context. A lens with no root resource returns an empty result and the cut,
// not an error.
func RetrieveLens(store *CutStore, catalog *Catalog, request LensRequest, evaluation semreg.EvaluationContext) (LensResult, error) {
	if store == nil {
		return LensResult{}, readError(semreg.InvalidValue, "nil cut store")
	}
	if catalog == nil {
		return LensResult{}, readError(semreg.InvalidValue, "nil catalog")
	}
	if err := request.validate(); err != nil {
		return LensResult{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	held, err := store.held(request.CutID)
	if err != nil {
		return LensResult{}, err
	}
	if held.catalog == nil || catalog.Digest() != held.catalog.Digest() {
		return LensResult{}, readError(semreg.RevisionConflict, "catalog is not the catalog of the cut")
	}
	index, err := indexMembers(held.cut, request.Members)
	if err != nil {
		return LensResult{}, err
	}
	lens, ok := lensByID(catalog, request.Lens)
	if !ok {
		return LensResult{}, readError(semreg.MembershipMissing, "lens")
	}
	if request.Depth > lens.MaxDepth || request.Depth > MaxTraversalDepth {
		return LensResult{}, readError(semreg.BoundsExceeded, "depth")
	}
	definitions, err := lensDefinitions(catalog, lens)
	if err != nil {
		return LensResult{}, err
	}
	followed := map[string]struct{}{}
	for _, definition := range definitions {
		followed[canonicalKey(definition)] = struct{}{}
	}
	entries, status, err := lensEntries(catalog, index, lens, request.Entry)
	if err != nil {
		return LensResult{}, err
	}
	notIncluded := mergedNotIncluded(held.cut, request.NotIncluded)
	identity := lensIdentityOf(request, evaluation, catalog.Digest(), notIncluded)
	digest, err := semreg.DigestRecord(identity)
	if err != nil {
		return LensResult{}, err
	}
	cursor, err := boundCursor(request.PageToken, held.cut.ID(), digest, MaxTraversalResources)
	if err != nil {
		return LensResult{}, err
	}
	if err := evaluation.Validate(); err != nil {
		return LensResult{}, err
	}
	result, err := assembleLens(held.cut, index, catalog, lens, followed, request, evaluation, entries, status, notIncluded, cursor, digest)
	if err != nil {
		return LensResult{}, err
	}
	if result.NextPage != nil {
		store.note(held, *result.NextPage)
	}
	return result, nil
}

func (r LensRequest) validate() error {
	if err := r.Lens.Validate(); err != nil {
		return err
	}
	switch r.Direction {
	case DirectionOutgoing, DirectionIncoming, DirectionBoth:
	default:
		return readError(semreg.InvalidEnum, "direction")
	}
	if err := r.Entry.validateShape(r.Lens); err != nil {
		return err
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
	for _, row := range r.NotIncluded {
		if err := row.validate(); err != nil {
			return err
		}
	}
	return nil
}

type lensIdentity struct {
	Lens        string             `json:"lens"`
	EntryKind   string             `json:"entry_kind"`
	Resource    string             `json:"resource,omitempty"`
	Direction   string             `json:"direction"`
	Depth       int                `json:"depth"`
	PageSize    int                `json:"page_size"`
	Wall        string             `json:"wall"`
	Clock       string             `json:"clock"`
	Uncertainty string             `json:"uncertainty"`
	Epoch       string             `json:"epoch"`
	Ticks       string             `json:"ticks"`
	NotIncluded []NotIncludedAsset `json:"not_included,omitempty"`
	Catalog     semreg.Digest      `json:"catalog"`
}

func (id lensIdentity) Validate() error {
	if err := semreg.DefinitionID(id.Lens).Validate(); err != nil {
		return err
	}
	switch EntryKind(id.EntryKind) {
	case EntryResource, EntryLensRoots, EntrySites:
	default:
		return readError(semreg.InvalidEnum, "entry")
	}
	if id.Resource != "" {
		if err := semreg.ResourceID(id.Resource).Validate(); err != nil {
			return err
		}
	}
	switch TraversalDirection(id.Direction) {
	case DirectionOutgoing, DirectionIncoming, DirectionBoth:
	default:
		return readError(semreg.InvalidEnum, "direction")
	}
	if id.Depth < 0 || id.Depth > MaxTraversalDepth || id.PageSize < 1 || id.PageSize > MaxPageSize {
		return readError(semreg.BoundsExceeded, "lens identity")
	}
	for _, row := range id.NotIncluded {
		if err := row.validate(); err != nil {
			return err
		}
	}
	return id.Catalog.Validate()
}

func lensIdentityOf(request LensRequest, evaluation semreg.EvaluationContext, catalog semreg.Digest, notIncluded []NotIncludedAsset) lensIdentity {
	identity := lensIdentity{
		Lens: string(request.Lens), EntryKind: string(request.Entry.Kind),
		Direction: string(request.Direction), Depth: request.Depth, PageSize: request.PageSize,
		Wall: string(evaluation.EvaluatedAt.UnixNanoseconds), Clock: string(evaluation.EvaluatedAt.ClockID),
		Uncertainty: string(evaluation.EvaluatedAt.UncertaintyNS), Epoch: string(evaluation.EvaluateMonotonic.ClockEpochID),
		Ticks: string(evaluation.EvaluateMonotonic.Nanoseconds), NotIncluded: notIncluded, Catalog: catalog,
	}
	if request.Entry.Kind == EntryResource {
		identity.Resource = string(request.Entry.Resource)
	}
	return identity
}

func lensEntries(catalog *Catalog, index *cutIndex, lens Lens, entry EntryPoint) ([]semreg.ResourceID, string, error) {
	switch entry.Kind {
	case EntryLensRoots:
		entries := resourcesOfKinds(index, lens.RootKinds)
		if len(entries) == 0 {
			return nil, EntryStatusNoRoot, nil
		}
		return entries, "", nil
	case EntryResource:
		entries, err := oneCurrent(index, entry.Resource)
		return entries, "", err
	case EntrySites:
		root, ok := catalog.InstallationRoot()
		if !ok {
			return nil, EntryStatusNoSite, nil
		}
		entries := resourcesOfKinds(index, []semreg.DefinitionID{root})
		if len(entries) == 0 {
			return nil, EntryStatusNoSite, nil
		}
		return entries, "", nil
	default:
		return nil, "", readError(semreg.InvalidEnum, "entry")
	}
}

func mergedNotIncluded(cut *Cut, requested []NotIncludedAsset) []NotIncludedAsset {
	seen := map[semreg.AssetID]struct{}{}
	var out []NotIncludedAsset
	for _, row := range requested {
		if _, ok := seen[row.AssetID]; ok {
			continue
		}
		seen[row.AssetID] = struct{}{}
		out = append(out, row)
	}
	for _, asset := range cut.Excluded() {
		if _, ok := seen[asset]; ok {
			continue
		}
		seen[asset] = struct{}{}
		out = append(out, NotIncludedAsset{AssetID: asset, Reason: NotIncludedExcluded})
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AssetID != out[j].AssetID {
			return out[i].AssetID < out[j].AssetID
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

type evaluatedRow struct {
	asset semreg.AssetID
	id    semreg.CandidateID
}

func assembleLens(cut *Cut, index *cutIndex, catalog *Catalog, lens Lens, followed map[string]struct{}, request LensRequest, evaluation semreg.EvaluationContext, entries []semreg.ResourceID, status string, notIncluded []NotIncludedAsset, cursor int, digest semreg.Digest) (LensResult, error) {
	walk, err := walkCut(cut, index, followed, request.Direction, request.Depth, entries)
	if err != nil {
		return LensResult{}, err
	}
	if cursor > len(walk.resources) {
		return LensResult{}, readError(semreg.InvalidValue, "page token cursor")
	}
	excluded := map[semreg.AssetID]struct{}{}
	for _, asset := range cut.Excluded() {
		excluded[asset] = struct{}{}
	}
	snapshots := append([]semreg.Snapshot(nil), request.Members...)
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].AssetID != snapshots[j].AssetID {
			return snapshots[i].AssetID < snapshots[j].AssetID
		}
		return snapshots[i].SnapshotID < snapshots[j].SnapshotID
	})
	rows := map[evaluatedRow]semreg.EvaluatedFact{}
	bodies := map[semreg.ResourceID]semreg.Resource{}
	for _, snapshot := range snapshots {
		if _, drop := excluded[snapshot.AssetID]; drop {
			continue
		}
		view, err := semreg.EvaluateSnapshot(snapshot, evaluation)
		if err != nil {
			return LensResult{}, err
		}
		for _, fact := range view.Facts {
			rows[evaluatedRow{asset: snapshot.AssetID, id: fact.CandidateID}] = fact
		}
		for _, resource := range snapshot.Resources {
			if resource.State != semreg.ResourceCurrent {
				continue
			}
			if _, ok := bodies[resource.ResourceID]; ok {
				continue
			}
			bodies[resource.ResourceID] = resource
		}
	}
	depth := map[semreg.ResourceID]int{}
	assetOf := map[semreg.ResourceID]semreg.AssetID{}
	for _, resource := range walk.resources {
		depth[resource.ResourceID] = resource.Depth
		assetOf[resource.ResourceID] = resource.AssetID
	}
	relations := lensRelations(cut, catalog, followed, depth)
	absences := lensAbsences(snapshots, excluded, followed, depth)
	facts, err := lensFacts(catalog, snapshots, excluded, lens.Planes, depth, rows)
	if err != nil {
		return LensResult{}, err
	}
	services, capabilities := lensServices(catalog, snapshots, excluded, lens.Planes, depth)
	findings := lensFindings(cut.Findings(), depth, assetOf)
	listing := cutPage(cut, catalog.Digest())
	result := LensResult{
		Contract: LensResultContract, Lens: lens.ID, Catalog: listing.Catalog,
		Cut:   CutRef{CutID: listing.CutID, Completeness: listing.Completeness, Members: listing.Members, NotIncluded: notIncluded},
		Entry: normalizedLensEntry(request), EntryStatus: status, Resources: []LensResource{},
	}
	end := cursor + request.PageSize
	if end > len(walk.resources) {
		end = len(walk.resources)
	}
	onPage := map[semreg.ResourceID]struct{}{}
	for _, resource := range walk.resources[cursor:end] {
		body, ok := bodies[resource.ResourceID]
		if !ok {
			return LensResult{}, readError(semreg.InvalidValue, "reached resource")
		}
		result.Resources = append(result.Resources, LensResource{
			Resource: body, Depth: resource.Depth, AssetID: resource.AssetID,
			Planes: planesInLens(cut.PlanesOfResource(resource.ResourceID), lens.Planes),
		})
		onPage[resource.ResourceID] = struct{}{}
	}
	if result.Resources == nil {
		result.Resources = []LensResource{}
	}
	result.Relations = relationsOnPage(relations, onPage, depth)
	result.Absences = absencesOnPage(absences, onPage)
	result.Facts = factsOnPage(facts, onPage)
	result.Services = servicesOnPage(services, onPage)
	result.Capabilities = capabilitiesOnPage(capabilities, services, onPage)
	result.Findings = findingsOnPage(findings, onPage, depth, assetOf)
	for _, frontier := range walk.frontier {
		if _, ok := onPage[frontier.ResourceID]; ok {
			result.Frontier = append(result.Frontier, frontier)
		}
	}
	if end < len(walk.resources) {
		token, err := mintPageToken(cut.ID(), digest, end)
		if err != nil {
			return LensResult{}, err
		}
		result.NextPage = &token
	}
	return result, nil
}

func normalizedLensEntry(request LensRequest) EntryPoint {
	switch request.Entry.Kind {
	case EntryResource:
		return EntryPoint{Kind: EntryResource, Resource: request.Entry.Resource}
	case EntryLensRoots:
		return EntryPoint{Kind: EntryLensRoots, Lens: request.Lens}
	default:
		return EntryPoint{Kind: EntrySites}
	}
}

func planesInLens(resourcePlanes, lensPlanes []PlaneID) []PlaneID {
	if len(resourcePlanes) == 0 || len(lensPlanes) == 0 {
		return nil
	}
	allow := map[PlaneID]struct{}{}
	for _, plane := range lensPlanes {
		allow[plane] = struct{}{}
	}
	var out []PlaneID
	for _, plane := range resourcePlanes {
		if _, ok := allow[plane]; ok {
			out = append(out, plane)
		}
	}
	return out
}

func onLensPlane(planes, lensPlanes []PlaneID) bool {
	return len(planesInLens(planes, lensPlanes)) != 0
}

func lensRelations(cut *Cut, catalog *Catalog, followed map[string]struct{}, depth map[semreg.ResourceID]int) []LensRelation {
	var out []LensRelation
	for _, relation := range cut.Relations() {
		_, from := depth[relation.From]
		_, to := depth[relation.To]
		_, isFollowed := followed[canonicalKey(relation.Definition)]
		qualified := relation.State == semreg.LinkQualified && relation.Coherence == CoherenceCoherent
		inPlane := false
		switch {
		case qualified && isFollowed && from && to:
			inPlane = true
		case qualified && isFollowed:
			continue
		case !qualified && (from || to):
		default:
			continue
		}
		item := LensRelation{
			AssetID: relation.AssetID, Definition: relation.Definition, From: relation.From, To: relation.To,
			State: relation.State, Coherence: relation.Coherence, InPlane: inPlane,
		}
		if element, ok := memberRelation(catalog, relation.Definition); ok && element.Relation != nil {
			item.CoreType = element.Relation.CoreType
			item.Orientation = element.Relation.Orientation
		}
		out = append(out, item)
	}
	return out
}

func relationAnchor(relation LensRelation, depth map[semreg.ResourceID]int) semreg.ResourceID {
	from, fromOK := depth[relation.From]
	to, toOK := depth[relation.To]
	switch {
	case fromOK && toOK:
		if from < to || (from == to && relation.From <= relation.To) {
			return relation.From
		}
		return relation.To
	case fromOK:
		return relation.From
	default:
		return relation.To
	}
}

func relationsOnPage(relations []LensRelation, onPage map[semreg.ResourceID]struct{}, depth map[semreg.ResourceID]int) []LensRelation {
	var out []LensRelation
	for _, relation := range relations {
		if _, ok := onPage[relationAnchor(relation, depth)]; ok {
			out = append(out, relation)
		}
	}
	return out
}

type heldAbsence struct {
	asset   semreg.AssetID
	absence semreg.RelationAbsence
}

// absenceResultKey is the absence key: canonical definition, resource, side.
func absenceResultKey(absence semreg.RelationAbsence) string {
	return canonicalKey(absence.Definition) + "\x00" + string(absence.Resource) + "\x00" + string(absence.Side)
}

func lensAbsences(snapshots []semreg.Snapshot, excluded map[semreg.AssetID]struct{}, followed map[string]struct{}, depth map[semreg.ResourceID]int) []semreg.RelationAbsence {
	var held []heldAbsence
	for _, snapshot := range snapshots {
		if _, drop := excluded[snapshot.AssetID]; drop {
			continue
		}
		for _, absence := range snapshot.RelationAbsences {
			if _, ok := followed[canonicalKey(absence.Definition)]; !ok {
				continue
			}
			if _, ok := depth[absence.Resource]; !ok {
				continue
			}
			held = append(held, heldAbsence{asset: snapshot.AssetID, absence: absence})
		}
	}
	sort.Slice(held, func(i, j int) bool {
		left := absenceResultKey(held[i].absence)
		right := absenceResultKey(held[j].absence)
		if left != right {
			return left < right
		}
		return held[i].asset < held[j].asset
	})
	if len(held) == 0 {
		return nil
	}
	out := make([]semreg.RelationAbsence, len(held))
	for i, item := range held {
		out[i] = item.absence
	}
	return out
}

func absencesOnPage(absences []semreg.RelationAbsence, onPage map[semreg.ResourceID]struct{}) []semreg.RelationAbsence {
	var out []semreg.RelationAbsence
	for _, absence := range absences {
		if _, ok := onPage[absence.Resource]; ok {
			out = append(out, absence)
		}
	}
	return out
}

func lensFacts(catalog *Catalog, snapshots []semreg.Snapshot, excluded map[semreg.AssetID]struct{}, lensPlanes []PlaneID, depth map[semreg.ResourceID]int, rows map[evaluatedRow]semreg.EvaluatedFact) ([]LensFact, error) {
	var out []LensFact
	for _, snapshot := range snapshots {
		if _, drop := excluded[snapshot.AssetID]; drop {
			continue
		}
		for _, envelope := range snapshot.Facts {
			key := envelope.Key
			element, ok := catalog.packEntryOfKind(semreg.PackRef{ID: key.PackID, Version: key.PackVersion}, key.FactID, KindFact)
			if !ok || element.Disposition != DispositionMember || element.Fact == nil || !onLensPlane(element.Planes, lensPlanes) {
				continue
			}
			if element.Fact.Subject.Status != SubjectResolved || element.Fact.Subject.Dimension == nil {
				continue
			}
			subject, ok := subjectOf(key, element.Fact.Subject.Dimension.ID)
			if !ok {
				continue
			}
			if _, reached := depth[subject]; !reached {
				continue
			}
			definition, ok := element.Ref.DefinitionRef()
			if !ok {
				continue
			}
			evaluationRows := make([]semreg.EvaluatedFact, 0, len(envelope.Candidates))
			for _, candidate := range envelope.Candidates {
				row, ok := rows[evaluatedRow{asset: snapshot.AssetID, id: candidate.CandidateID}]
				if !ok {
					return nil, readError(semreg.InvalidValue, "fact evaluation")
				}
				evaluationRows = append(evaluationRows, row)
			}
			fact := LensFact{
				AssetID: snapshot.AssetID, Definition: definition, Key: key, Subject: subject,
				Candidates: append([]semreg.FactCandidate{}, envelope.Candidates...),
				Conflicts:  append([]semreg.Conflict{}, envelope.Conflicts...),
				Revision:   envelope.Revision, Evaluation: evaluationRows,
				Aggregation: element.Fact.Aggregation, Direction: element.Fact.Direction,
				Sign: signOf(element.Fact.SignReference),
			}
			out = append(out, fact)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := canonicalKey(out[i].Key), canonicalKey(out[j].Key)
		if left != right {
			return left < right
		}
		return out[i].AssetID < out[j].AssetID
	})
	return out, nil
}

func signOf(reference *SignReference) *LensSign {
	if reference == nil {
		return nil
	}
	sign := &LensSign{NativeConvention: reference.NativeConvention}
	if reference.Relation != nil {
		if definition, ok := reference.Relation.DefinitionRef(); ok {
			sign.Relation = &definition
		}
	}
	return sign
}

func factsOnPage(facts []LensFact, onPage map[semreg.ResourceID]struct{}) []LensFact {
	var out []LensFact
	for _, fact := range facts {
		if _, ok := onPage[fact.Subject]; ok {
			out = append(out, fact)
		}
	}
	return out
}

func lensServices(catalog *Catalog, snapshots []semreg.Snapshot, excluded map[semreg.AssetID]struct{}, lensPlanes []PlaneID, depth map[semreg.ResourceID]int) ([]semreg.ServiceInstance, []semreg.CapabilityInstance) {
	var services []semreg.ServiceInstance
	byID := map[semreg.ServiceInstanceID]semreg.ServiceInstance{}
	for _, snapshot := range snapshots {
		if _, drop := excluded[snapshot.AssetID]; drop {
			continue
		}
		for _, service := range snapshot.Services {
			if service.Availability == semreg.AvailabilityWithdrawn || service.Resource == nil {
				continue
			}
			byID[service.InstanceID] = service
			if _, reached := depth[*service.Resource]; !reached {
				continue
			}
			element, ok := catalog.PackEntry(service.Definition)
			if !ok || element.Disposition != DispositionMember || !onLensPlane(element.Planes, lensPlanes) {
				continue
			}
			services = append(services, service)
		}
	}
	var capabilities []semreg.CapabilityInstance
	for _, snapshot := range snapshots {
		if _, drop := excluded[snapshot.AssetID]; drop {
			continue
		}
		for _, capability := range snapshot.Capabilities {
			if capability.Availability == semreg.AvailabilityWithdrawn {
				continue
			}
			service, ok := byID[capability.ServiceInstance]
			if !ok || service.Resource == nil {
				continue
			}
			if _, reached := depth[*service.Resource]; !reached {
				continue
			}
			element, ok := catalog.PackEntry(capability.Definition)
			if !ok || element.Disposition != DispositionMember || !onLensPlane(element.Planes, lensPlanes) {
				continue
			}
			capabilities = append(capabilities, capability)
		}
	}
	sort.Slice(services, func(i, j int) bool { return services[i].InstanceID < services[j].InstanceID })
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].InstanceID < capabilities[j].InstanceID })
	return services, capabilities
}

func servicesOnPage(services []semreg.ServiceInstance, onPage map[semreg.ResourceID]struct{}) []semreg.ServiceInstance {
	var out []semreg.ServiceInstance
	for _, service := range services {
		if service.Resource != nil {
			if _, ok := onPage[*service.Resource]; ok {
				out = append(out, service)
			}
		}
	}
	return out
}

func capabilitiesOnPage(capabilities []semreg.CapabilityInstance, services []semreg.ServiceInstance, onPage map[semreg.ResourceID]struct{}) []semreg.CapabilityInstance {
	resourceOf := map[semreg.ServiceInstanceID]semreg.ResourceID{}
	for _, service := range services {
		if service.Resource != nil {
			resourceOf[service.InstanceID] = *service.Resource
		}
	}
	var out []semreg.CapabilityInstance
	for _, capability := range capabilities {
		resource, ok := resourceOf[capability.ServiceInstance]
		if !ok {
			continue
		}
		if _, on := onPage[resource]; on {
			out = append(out, capability)
		}
	}
	return out
}

func lensFindings(findings []CutFinding, depth map[semreg.ResourceID]int, assetOf map[semreg.ResourceID]semreg.AssetID) []CutFinding {
	var out []CutFinding
	for _, finding := range findings {
		if _, ok := findingAnchor(finding, depth, assetOf); ok {
			out = append(out, finding)
		}
	}
	return out
}

func findingAnchor(finding CutFinding, depth map[semreg.ResourceID]int, assetOf map[semreg.ResourceID]semreg.AssetID) (semreg.ResourceID, bool) {
	var best semreg.ResourceID
	found := false
	consider := func(id semreg.ResourceID) {
		if _, ok := depth[id]; !ok {
			return
		}
		if !found || depth[id] < depth[best] || (depth[id] == depth[best] && id < best) {
			best = id
			found = true
		}
	}
	for _, resource := range finding.Resources {
		consider(resource.ResourceID)
	}
	if finding.Relation != nil {
		consider(finding.Relation.From)
		consider(finding.Relation.To)
	}
	if finding.Absence != nil {
		consider(finding.Absence.Resource)
	}
	if !found {
		for id, asset := range assetOf {
			for _, named := range finding.Assets {
				if named == asset {
					consider(id)
				}
			}
		}
	}
	return best, found
}

func findingsOnPage(findings []CutFinding, onPage map[semreg.ResourceID]struct{}, depth map[semreg.ResourceID]int, assetOf map[semreg.ResourceID]semreg.AssetID) []CutFinding {
	var out []CutFinding
	for _, finding := range findings {
		anchor, ok := findingAnchor(finding, depth, assetOf)
		if !ok {
			continue
		}
		if _, on := onPage[anchor]; on {
			out = append(out, finding)
		}
	}
	return out
}
