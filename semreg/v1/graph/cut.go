package graph

import (
	"sort"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// CutContract names the graph cut: the contract member of the identity
// object its identifier digests and of its canonical output.
const CutContract = "helianthus.semantic.graph-cut/v1"

// MaxCutMembers is the most snapshots one cut reads together.
const MaxCutMembers = 256

// Completeness states whether a cut holds every current asset of the
// installation. The caller asserts complete; a cut that excludes a member is
// partial whatever the caller asserted.
type Completeness string

// Completeness values.
const (
	CutComplete Completeness = "complete"
	CutPartial  Completeness = "partial"
)

// RelationCoherence is the state of one relation instance in a cut. Only a
// qualified, coherent instance is a current edge.
type RelationCoherence string

// RelationCoherence values.
const (
	CoherenceCoherent   RelationCoherence = "coherent"
	CoherenceStale      RelationCoherence = "stale"
	CoherenceDangling   RelationCoherence = "dangling"
	CoherenceOutsideCut RelationCoherence = "outside_cut"
	CoherenceExcluded   RelationCoherence = "excluded"
	CoherenceConflict   RelationCoherence = "conflict"
)

// CutFindingKind classifies a problem a cut found inside or between its
// members. A finding never blocks the read.
type CutFindingKind string

// CutFindingKind values.
const (
	CutDuplicateIdentity      CutFindingKind = "duplicate_identity"
	CutSameUnit               CutFindingKind = "same_unit"
	CutContradictedAbsence    CutFindingKind = "contradicted_absence"
	CutAnchorParentUnresolved CutFindingKind = "anchor_parent_unresolved"
	CutUngraphed              CutFindingKind = "ungraphed"
)

// CutMember is one snapshot a cut read: the per-asset component of the
// coherent revision of a graph read.
type CutMember struct {
	AssetID    semreg.AssetID    `json:"asset_id"`
	SnapshotID semreg.SnapshotID `json:"snapshot_id"`
}

// CutResourceRef names a resource record held by one member.
type CutResourceRef struct {
	AssetID    semreg.AssetID    `json:"asset_id"`
	ResourceID semreg.ResourceID `json:"resource_id"`
}

// CutRelationRef names a relation instance and the member holding it.
type CutRelationRef struct {
	AssetID    semreg.AssetID       `json:"asset_id"`
	Definition semreg.DefinitionRef `json:"definition"`
	From       semreg.ResourceID    `json:"from"`
	To         semreg.ResourceID    `json:"to"`
}

// CutAbsenceRef names an absence statement and the member holding it.
type CutAbsenceRef struct {
	AssetID    semreg.AssetID       `json:"asset_id"`
	Definition semreg.DefinitionRef `json:"definition"`
	Resource   semreg.ResourceID    `json:"resource"`
	Side       semreg.AbsenceSide   `json:"side"`
}

// CutFinding is one finding with its subject. An ungraphed finding names its
// member in Assets; a duplicate identity, same unit or unresolved anchor
// parent names the resource records in Resources, sorted by asset and
// resource; a contradicted absence names the absence statement and the
// instance it denies.
type CutFinding struct {
	Kind      CutFindingKind   `json:"kind"`
	Assets    []semreg.AssetID `json:"assets,omitempty"`
	Resources []CutResourceRef `json:"resources,omitempty"`
	Relation  *CutRelationRef  `json:"relation,omitempty"`
	Absence   *CutAbsenceRef   `json:"absence,omitempty"`
}

// CutRelation is one relation instance of a kept member with its coherence.
// State is the instance's state in its snapshot, except that a qualified
// instance the cut finds in conflict is conflict.
type CutRelation struct {
	AssetID    semreg.AssetID       `json:"asset_id"`
	Definition semreg.DefinitionRef `json:"definition"`
	From       semreg.ResourceID    `json:"from"`
	To         semreg.ResourceID    `json:"to"`
	State      semreg.LinkState     `json:"state"`
	Coherence  RelationCoherence    `json:"coherence"`
}

// CutResult is the canonical output of a cut. Members lists every snapshot
// read, sorted by asset; Excluded the members excluded for duplicate
// identity, sorted; Findings is ordered by kind and canonical subject;
// Relations, the instances of the kept members, by canonical definition,
// from and to.
type CutResult struct {
	Contract     string           `json:"contract"`
	CutID        semreg.Digest    `json:"cut_id"`
	Completeness Completeness     `json:"completeness"`
	Members      []CutMember      `json:"members"`
	Excluded     []semreg.AssetID `json:"excluded"`
	Findings     []CutFinding     `json:"findings"`
	Relations    []CutRelation    `json:"relations"`
}

// Cut is an immutable graph cut: per-asset snapshots read together with the
// findings, the coherence of every relation instance and the plane
// membership the element catalog gives them. It holds no global revision;
// its identifier digests the vector of member snapshot identifiers.
type Cut struct {
	result         CutResult
	relationIndex  map[string]int
	resourcePlanes map[semreg.ResourceID][]PlaneID
	relationPlanes map[string][]PlaneID
	factPlanes     map[string][]PlaneID
}

// AssembleCut reads snapshots of distinct assets together. It is pure and
// deterministic: the output depends on neither input order nor map order,
// and the input is not changed. It rejects only invalid input: an invalid
// completeness (invalid_enum), a nil catalog (invalid_value), more than
// MaxCutMembers snapshots (bounds_exceeded), a snapshot that fails its own
// validation, or two snapshots of one asset (duplicate_key). Problems inside
// or between members are findings and coherence states.
func AssembleCut(snapshots []semreg.Snapshot, catalog *Catalog, completeness Completeness) (*Cut, error) {
	if err := completeness.Validate(); err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, cutError(semreg.InvalidValue, "nil catalog")
	}
	if len(snapshots) > MaxCutMembers {
		return nil, cutError(semreg.BoundsExceeded, "more than 256 member snapshots")
	}
	sorted := make([]*semreg.Snapshot, len(snapshots))
	for i := range snapshots {
		sorted[i] = &snapshots[i]
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].AssetID != sorted[j].AssetID {
			return sorted[i].AssetID < sorted[j].AssetID
		}
		return sorted[i].SnapshotID < sorted[j].SnapshotID
	})
	for _, snapshot := range sorted {
		if err := snapshot.Validate(); err != nil {
			return nil, within("graph cut member "+string(snapshot.AssetID), err)
		}
	}
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1].AssetID == sorted[i].AssetID {
			return nil, cutError(semreg.DuplicateKey, "two snapshots of asset "+string(sorted[i].AssetID))
		}
	}
	a := newAssembly(catalog, sorted, completeness)
	a.excludeDuplicateIdentities()
	a.loadRelations()
	a.deriveConflicts()
	a.assignCoherence()
	a.reportAnchorParents()
	a.reportSameUnits()
	a.reportUngraphed()
	return a.build()
}

// cutMember is the assembly view of one member snapshot.
type cutMember struct {
	asset     semreg.AssetID
	snapshot  *semreg.Snapshot
	resources map[semreg.ResourceID]semreg.Resource
	excluded  bool
}

// cutRelation is the assembly view of one relation instance of a kept
// member. definition is nil when the catalog lists no member relation
// definition for it.
type cutRelation struct {
	member        *cutMember
	instance      semreg.RelationInstance
	definition    *Element
	definitionKey string
	key           string
	conflict      bool
	coherence     RelationCoherence
}

type assembly struct {
	catalog      *Catalog
	completeness Completeness
	members      []*cutMember
	relations    []*cutRelation
	findings     []CutFinding
}

func newAssembly(catalog *Catalog, sorted []*semreg.Snapshot, completeness Completeness) *assembly {
	a := &assembly{catalog: catalog, completeness: completeness, members: make([]*cutMember, len(sorted))}
	for i, snapshot := range sorted {
		member := &cutMember{asset: snapshot.AssetID, snapshot: snapshot, resources: make(map[semreg.ResourceID]semreg.Resource, len(snapshot.Resources))}
		for _, resource := range snapshot.Resources {
			member.resources[resource.ResourceID] = resource
		}
		a.members[i] = member
	}
	return a
}

// kept returns the members that were not excluded, in asset order.
func (a *assembly) kept() []*cutMember {
	out := make([]*cutMember, 0, len(a.members))
	for _, member := range a.members {
		if !member.excluded {
			out = append(out, member)
		}
	}
	return out
}

// relationDefinition returns the catalog entry of a member relation
// definition.
func (a *assembly) relationDefinition(ref semreg.DefinitionRef) *Element {
	at, ok := a.catalog.byRef[PackElement(ref)]
	if !ok {
		return nil
	}
	element := &a.catalog.elements[at]
	if element.Relation == nil || element.Disposition != DispositionMember {
		return nil
	}
	return element
}

func (a *assembly) loadRelations() {
	for _, member := range a.kept() {
		for _, instance := range member.snapshot.Relations {
			definitionKey := canonicalKey(instance.Definition)
			a.relations = append(a.relations, &cutRelation{
				member: member, instance: instance, definition: a.relationDefinition(instance.Definition),
				definitionKey: definitionKey, key: relationLookupKey(definitionKey, instance.From, instance.To),
				conflict: instance.State == semreg.LinkConflict,
			})
		}
	}
}

// structural reports whether an instance takes part in the cardinality,
// forest and acyclicity rules: its publisher qualified it, so a stored
// conflict counts, and its definition is a catalog member.
func (r *cutRelation) structural() bool {
	return r.definition != nil && (r.instance.State == semreg.LinkQualified || r.instance.State == semreg.LinkConflict)
}

// whole and part read the instance along its core type, so an inverse
// definition is compared the same way as one that reads as the core type.
func (r *cutRelation) whole() semreg.ResourceID {
	if r.definition != nil && r.definition.Relation.Orientation == OrientationInverse {
		return r.instance.To
	}
	return r.instance.From
}

func (r *cutRelation) part() semreg.ResourceID {
	if r.definition != nil && r.definition.Relation.Orientation == OrientationInverse {
		return r.instance.From
	}
	return r.instance.To
}

// assignCoherence gives every instance of a kept member one coherence. A
// conflict comes first, including an instance whose definition is not a
// member relation of the catalog or whose starting end or current far end
// is of a kind the definition does not admit; otherwise the far end
// decides: current in a kept
// member is coherent, held by an excluded member is excluded, held only
// withdrawn or retired is stale, and held by no member is dangling in a
// complete cut and outside_cut in a partial one.
func (a *assembly) assignCoherence() {
	completeness := a.effectiveCompleteness()
	for _, relation := range a.relations {
		far, current, present, excluded := a.farEnd(relation.instance.To)
		if relation.definition == nil {
			relation.conflict = true
		} else {
			if from, ok := relation.member.resources[relation.instance.From]; !ok || !containsKind(relation.definition.Relation.FromKinds, from.Kind) {
				relation.conflict = true
			}
			if current && !containsKind(relation.definition.Relation.ToKinds, far.Kind) {
				relation.conflict = true
			}
		}
		switch {
		case relation.conflict:
			relation.coherence = CoherenceConflict
		case current:
			relation.coherence = CoherenceCoherent
		case excluded:
			relation.coherence = CoherenceExcluded
		case present:
			relation.coherence = CoherenceStale
		case completeness == CutComplete:
			relation.coherence = CoherenceDangling
		default:
			relation.coherence = CoherenceOutsideCut
		}
	}
}

// farEnd looks a far end up in every member: the record of the kept member
// holding it current, whether any kept member holds it, and whether an
// excluded member holds it.
func (a *assembly) farEnd(id semreg.ResourceID) (far semreg.Resource, current, present, excluded bool) {
	for _, member := range a.members {
		resource, ok := member.resources[id]
		if !ok {
			continue
		}
		if member.excluded {
			excluded = true
			continue
		}
		present = true
		if resource.State == semreg.ResourceCurrent && !current {
			far, current = resource, true
		}
	}
	return far, current, present, excluded
}

func (a *assembly) effectiveCompleteness() Completeness {
	for _, member := range a.members {
		if member.excluded {
			return CutPartial
		}
	}
	return a.completeness
}

// state is the instance's state in the cut: a qualified instance the cut
// finds in conflict is conflict.
func (r *cutRelation) state() semreg.LinkState {
	if r.coherence == CoherenceConflict && r.instance.State == semreg.LinkQualified {
		return semreg.LinkConflict
	}
	return r.instance.State
}

// edge reports whether the instance is a current edge (COH-1).
func (r *cutRelation) edge() bool {
	return r.instance.State == semreg.LinkQualified && r.coherence == CoherenceCoherent
}

func (a *assembly) build() (*Cut, error) {
	result := CutResult{
		Contract: CutContract, Completeness: a.effectiveCompleteness(),
		Members: make([]CutMember, len(a.members)), Excluded: []semreg.AssetID{}, Findings: []CutFinding{}, Relations: []CutRelation{},
	}
	for i, member := range a.members {
		result.Members[i] = CutMember{AssetID: member.asset, SnapshotID: member.snapshot.SnapshotID}
		if member.excluded {
			result.Excluded = append(result.Excluded, member.asset)
		}
	}
	id, err := semreg.DigestRecord(cutIdentity{Contract: CutContract, Members: result.Members})
	if err != nil {
		return nil, err
	}
	result.CutID = id

	type keyed[T any] struct {
		key   string
		value T
	}
	findings := make([]keyed[CutFinding], len(a.findings))
	for i, finding := range a.findings {
		canonical, err := semreg.CanonicalJSON(finding)
		if err != nil {
			return nil, err
		}
		findings[i] = keyed[CutFinding]{string(finding.Kind) + "\x00" + string(canonical), finding}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].key < findings[j].key })
	for i, finding := range findings {
		if i > 0 && finding.key == findings[i-1].key {
			continue
		}
		result.Findings = append(result.Findings, finding.value)
	}

	relations := append([]*cutRelation(nil), a.relations...)
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].key != relations[j].key {
			return relations[i].key < relations[j].key
		}
		return relations[i].member.asset < relations[j].member.asset
	})
	cut := &Cut{relationIndex: make(map[string]int, len(relations))}
	for _, relation := range relations {
		if _, seen := cut.relationIndex[relation.key]; !seen {
			cut.relationIndex[relation.key] = len(result.Relations)
		}
		instance := relation.instance
		result.Relations = append(result.Relations, CutRelation{
			AssetID: relation.member.asset, Definition: instance.Definition, From: instance.From, To: instance.To,
			State: relation.state(), Coherence: relation.coherence,
		})
	}
	cut.result = result
	a.membership(cut)
	return cut, nil
}

// cutIdentity is the object the cut identifier digests.
type cutIdentity struct {
	Contract string      `json:"contract"`
	Members  []CutMember `json:"members"`
}

func (c cutIdentity) Validate() error {
	if c.Contract != CutContract {
		return cutError(semreg.InvalidContract, "cut identity contract")
	}
	return membersError(c.Members)
}

func membersError(members []CutMember) error {
	if members == nil {
		return cutError(semreg.MissingMember, "members")
	}
	for i, member := range members {
		if err := member.Validate(); err != nil {
			return err
		}
		if i > 0 && members[i-1].AssetID >= member.AssetID {
			return cutError(semreg.NoncanonicalOrder, "members are not sorted by asset")
		}
	}
	return nil
}

// ID returns the cut identifier: sha256 over the JCS bytes of the contract
// and the members sorted by asset.
func (c *Cut) ID() semreg.Digest {
	if c == nil {
		return ""
	}
	return c.result.CutID
}

// Completeness returns complete only when the caller asserted it and no
// member was excluded.
func (c *Cut) Completeness() Completeness {
	if c == nil {
		return ""
	}
	return c.result.Completeness
}

// Members returns every snapshot read, sorted by asset.
func (c *Cut) Members() []CutMember {
	if c == nil {
		return nil
	}
	return append([]CutMember(nil), c.result.Members...)
}

// Excluded returns the members excluded for duplicate identity, sorted.
func (c *Cut) Excluded() []semreg.AssetID {
	if c == nil {
		return nil
	}
	return append([]semreg.AssetID(nil), c.result.Excluded...)
}

// Findings returns the findings in canonical order.
func (c *Cut) Findings() []CutFinding {
	if c == nil {
		return nil
	}
	out := make([]CutFinding, len(c.result.Findings))
	for i, finding := range c.result.Findings {
		out[i] = cloneFinding(finding)
	}
	return out
}

// Relations returns every relation instance of the kept members with its
// coherence, in canonical order.
func (c *Cut) Relations() []CutRelation {
	if c == nil {
		return nil
	}
	return append([]CutRelation(nil), c.result.Relations...)
}

// Relation returns the instance with the key held by a kept member.
func (c *Cut) Relation(key semreg.RelationKey) (CutRelation, bool) {
	if c == nil {
		return CutRelation{}, false
	}
	at, ok := c.relationIndex[relationLookupKey(canonicalKey(key.Definition), key.From, key.To)]
	if !ok {
		return CutRelation{}, false
	}
	return c.result.Relations[at], true
}

// Result returns a detached copy of the canonical output.
func (c *Cut) Result() CutResult {
	if c == nil {
		return CutResult{}
	}
	result := c.result
	result.Members = c.Members()
	result.Excluded = c.Excluded()
	result.Findings = c.Findings()
	result.Relations = c.Relations()
	return result
}

// CanonicalJSON is the one encoder of the cut's canonical output: identical
// inputs give identical bytes.
func (c *Cut) CanonicalJSON() ([]byte, error) {
	if c == nil {
		return nil, cutError(semreg.InvalidValue, "nil cut")
	}
	return semreg.CanonicalJSON(c.Result())
}

func cloneFinding(in CutFinding) CutFinding {
	out := in
	out.Assets = append([]semreg.AssetID(nil), in.Assets...)
	out.Resources = append([]CutResourceRef(nil), in.Resources...)
	if in.Relation != nil {
		relation := *in.Relation
		out.Relation = &relation
	}
	if in.Absence != nil {
		absence := *in.Absence
		out.Absence = &absence
	}
	return out
}

// ------------------------------------------------------------- validation

func (c Completeness) Validate() error {
	if c != CutComplete && c != CutPartial {
		return cutError(semreg.InvalidEnum, "completeness")
	}
	return nil
}

func (c RelationCoherence) Validate() error {
	switch c {
	case CoherenceCoherent, CoherenceStale, CoherenceDangling, CoherenceOutsideCut, CoherenceExcluded, CoherenceConflict:
		return nil
	}
	return cutError(semreg.InvalidEnum, "relation coherence")
}

func (k CutFindingKind) Validate() error {
	switch k {
	case CutDuplicateIdentity, CutSameUnit, CutContradictedAbsence, CutAnchorParentUnresolved, CutUngraphed:
		return nil
	}
	return cutError(semreg.InvalidEnum, "cut finding kind")
}

func (m CutMember) Validate() error {
	if err := m.AssetID.Validate(); err != nil {
		return err
	}
	return m.SnapshotID.Validate()
}

func (r CutResourceRef) Validate() error {
	if err := r.AssetID.Validate(); err != nil {
		return err
	}
	return r.ResourceID.Validate()
}

func (r CutRelationRef) Validate() error {
	if err := r.AssetID.Validate(); err != nil {
		return err
	}
	return semreg.RelationKey{Definition: r.Definition, From: r.From, To: r.To}.Validate()
}

func (r CutAbsenceRef) Validate() error {
	if err := r.AssetID.Validate(); err != nil {
		return err
	}
	return semreg.RelationAbsenceKey{Definition: r.Definition, Resource: r.Resource, Side: r.Side}.Validate()
}

func (f CutFinding) Validate() error {
	if err := f.Kind.Validate(); err != nil {
		return err
	}
	if len(f.Assets) == 0 && len(f.Resources) == 0 && f.Relation == nil && f.Absence == nil {
		return cutError(semreg.MissingMember, "finding subject")
	}
	for _, asset := range f.Assets {
		if err := asset.Validate(); err != nil {
			return err
		}
	}
	for _, resource := range f.Resources {
		if err := resource.Validate(); err != nil {
			return err
		}
	}
	if f.Relation != nil {
		if err := f.Relation.Validate(); err != nil {
			return err
		}
	}
	if f.Absence != nil {
		return f.Absence.Validate()
	}
	return nil
}

func (r CutRelation) Validate() error {
	if err := r.AssetID.Validate(); err != nil {
		return err
	}
	if err := (semreg.RelationKey{Definition: r.Definition, From: r.From, To: r.To}).Validate(); err != nil {
		return err
	}
	switch r.State {
	case semreg.LinkCandidate, semreg.LinkQualified, semreg.LinkRejected, semreg.LinkConflict:
	default:
		return cutError(semreg.InvalidEnum, "relation state")
	}
	return r.Coherence.Validate()
}

func (r CutResult) Validate() error {
	if r.Contract != CutContract {
		return cutError(semreg.InvalidContract, "cut contract")
	}
	if err := r.CutID.Validate(); err != nil {
		return err
	}
	if err := r.Completeness.Validate(); err != nil {
		return err
	}
	if err := membersError(r.Members); err != nil {
		return err
	}
	if r.Excluded == nil || r.Findings == nil || r.Relations == nil {
		return cutError(semreg.MissingMember, "cut collection")
	}
	for i, asset := range r.Excluded {
		if err := asset.Validate(); err != nil {
			return err
		}
		if i > 0 && r.Excluded[i-1] >= asset {
			return cutError(semreg.NoncanonicalOrder, "excluded members are not sorted")
		}
	}
	for _, finding := range r.Findings {
		if err := finding.Validate(); err != nil {
			return err
		}
	}
	for _, relation := range r.Relations {
		if err := relation.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- helpers

func cutError(id semreg.ErrorID, detail string) error {
	return &semreg.Error{ID: id, Detail: "graph cut: " + detail}
}

// canonicalKey is the canonical JSON of a record, or empty when it has none;
// an empty key matches no published record.
func canonicalKey(record semreg.Record) string {
	canonical, err := semreg.CanonicalJSON(record)
	if err != nil {
		return ""
	}
	return string(canonical)
}

// relationLookupKey joins the canonical definition and the endpoints. A NUL
// cannot occur in canonical JSON or an opaque identifier, so the joined key
// orders as the tuple.
func relationLookupKey(definitionKey string, from, to semreg.ResourceID) string {
	return strings.Join([]string{definitionKey, string(from), string(to)}, "\x00")
}

func containsKind(kinds []semreg.DefinitionID, kind semreg.DefinitionID) bool {
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}
