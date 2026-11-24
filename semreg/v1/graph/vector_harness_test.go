package graph_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

// The vector document writes its records in the kernel wire form but leaves
// out what no vector varies. The harness completes a record only by adding
// members that are absent, never by changing a present one, and then decodes
// it with the kernel's strict decoder. A present empty resources, relations
// or relation_absences collection is omitted, which is the kernel wire form
// of an empty graph collection:
//
//   - a batch gains the kernel contract, observed_at at the synthetic wall
//     time, every required collection it omits as empty, a source descriptor
//     for its header source epoch when the asset's current snapshot holds
//     none, and its computed batch digest;
//   - a fact candidate gains its time axes, a freshness policy and the
//     availability, freshness and reasons axes of its quality;
//   - an intent gains its contract, the one argument its expected effect
//     names, a causal context opened by an operator, the expected revisions
//     read from the snapshot it is admitted against, no precondition, an
//     idempotency key and a deadline that is open or closed as the prior
//     state's admission context says;
//   - a read snapshot gains the kernel contract, revisions, evaluation time,
//     the empty required collections, one synthetic source with its binding
//     and cursor, and the origin, revision and anchor evidence a resource,
//     relation or fact omits. snapshot_id, an owner-tagged definition and a
//     coefficient are left as written.
//
// Every completed value is synthetic. The synthetic wall time and the
// freshness policy are those the document's own cut snapshots carry.

const (
	gvWallNS           = int64(1000000000000)
	gvClockEpoch       = "clock-epoch:publication"
	gvMonotonicStep    = uint64(1000)
	gvLifetimeNS       = int64(30000000000)
	gvEvidenceOwner    = "helianthus.example"
	gvEvidenceKind     = "evidence.synthetic"
	gvEvidenceContract = "helianthus.example.evidence/v1"
	gvCatalogPath      = "embedded/element-catalog-v1.json"
)

// ------------------------------------------------------------ the document

type gvDocument struct {
	Contract       string `json:"contract"`
	KernelContract string `json:"kernel_contract"`
	ElementCatalog struct {
		Path     string        `json:"path"`
		Contract string        `json:"contract"`
		SHA256   semreg.Digest `json:"sha256"`
	} `json:"element_catalog"`
	SyntheticSchemes []string       `json:"synthetic_identification_schemes"`
	ErrorIDs         []string       `json:"error_ids"`
	CoverageIDs      []string       `json:"coverage_ids"`
	Operations       []string       `json:"operations"`
	PriorStates      []gvPriorState `json:"prior_states"`
	Vectors          []gvVector     `json:"vectors"`
}

type gvPriorState struct {
	Name             string              `json:"name"`
	Registry         *gvRegistry         `json:"registry"`
	Batches          []json.RawMessage   `json:"batches"`
	AdmissionContext *gvAdmissionContext `json:"admission_context,omitempty"`
}

type gvRegistry struct {
	ElementCatalog        string           `json:"element_catalog"`
	Packs                 []semreg.PackRef `json:"packs"`
	IdentificationSchemes []string         `json:"identification_schemes,omitempty"`
	// CatalogMutations are the stated set_disposition mutations with which
	// the prior state registers the element catalog.
	CatalogMutations []map[string]json.RawMessage `json:"catalog_mutations,omitempty"`
}

type gvAdmissionContext struct {
	AuthorityResolves bool `json:"authority_resolves"`
	DeadlineOpen      bool `json:"deadline_open"`
}

type gvVector struct {
	ID             string           `json:"id"`
	Polarity       string           `json:"polarity"`
	RecordType     string           `json:"record_type"`
	Operation      string           `json:"operation"`
	Coverage       []string         `json:"coverage"`
	PriorState     string           `json:"prior_state"`
	Input          json.RawMessage  `json:"input"`
	Expect         json.RawMessage  `json:"expect"`
	Definitions    []gvDefinition   `json:"definitions"`
	UncoveredPacks []semreg.PackRef `json:"uncovered_packs,omitempty"`
	AnchorKeys     []gvAnchorKey    `json:"anchor_keys,omitempty"`
	Criterion      string           `json:"criterion"`
}

type gvDefinition struct {
	Ref         gvElementRef `json:"ref"`
	Disposition string       `json:"disposition"`
	Planes      []string     `json:"planes,omitempty"`
}

type gvElementRef struct {
	Owner struct {
		Kind     string          `json:"kind"`
		Pack     *semreg.PackRef `json:"pack,omitempty"`
		Contract string          `json:"contract,omitempty"`
	} `json:"owner"`
	ID      semreg.DefinitionID    `json:"id"`
	Version semreg.SemanticVersion `json:"version"`
}

type gvAnchorKey struct {
	Identity struct {
		Scheme string `json:"scheme"`
		Issuer string `json:"issuer"`
		Token  string `json:"token"`
	} `json:"identity"`
	Key semreg.Digest `json:"key"`
}

type gvExpect struct {
	Result                 string            `json:"result"`
	ErrorID                semreg.ErrorID    `json:"error_id,omitempty"`
	StateUnchanged         bool              `json:"state_unchanged,omitempty"`
	Assertions             []string          `json:"assertions,omitempty"`
	Read                   json.RawMessage   `json:"read,omitempty"`
	InadmissibleOperations []gvElementRef    `json:"inadmissible_operations,omitempty"`
	Resources              []gvResourceState `json:"resources,omitempty"`
	RelationStates         []gvRelationState `json:"relation_states,omitempty"`
	MultipleMembership     []gvElementRef    `json:"multiple_membership,omitempty"`
	Cut                    *gvExpectedCut    `json:"cut,omitempty"`
}

type gvResourceState struct {
	ResourceID semreg.ResourceID `json:"resource_id"`
	Revision   semreg.Uint64     `json:"revision"`
}

type gvRelationState struct {
	Definition json.RawMessage   `json:"definition"`
	From       semreg.ResourceID `json:"from"`
	To         semreg.ResourceID `json:"to"`
	State      semreg.LinkState  `json:"state"`
}

type gvExpectedCut struct {
	CutID             semreg.Digest     `json:"cut_id"`
	Completeness      string            `json:"completeness"`
	Members           []json.RawMessage `json:"members"`
	ExcludedMembers   []semreg.AssetID  `json:"excluded_members"`
	Findings          []gvCutFinding    `json:"findings"`
	RelationCoherence []json.RawMessage `json:"relation_coherence"`
	PlaneMembership   []struct {
		ResourceID semreg.ResourceID `json:"resource_id"`
		Planes     []string          `json:"planes"`
	} `json:"plane_membership"`
}

type gvCutFinding struct {
	Kind    string          `json:"kind"`
	Subject json.RawMessage `json:"subject"`
}

// ------------------------------------------------------------- the harness

// gvOperations maps each operation of the document to its runner.
var gvOperations = map[string]func(*vectorCase){
	"construct_registry": runConstructRegistry,
	"register_catalog":   runRegisterCatalog,
	"apply":              runApply,
	"assemble_cut":       runAssembleCut,
	"admit":              runAdmit,
	"list_resources":     runListResources,
	"traverse":           runTraverse,
	"retrieve_lens":      runRetrieveLens,
	"sum_members":        runSumMembers,
	"project":            runProject,
}

type vectorCase struct {
	t      *testing.T
	doc    *gvDocument
	vector gvVector
	state  gvPriorState
	expect gvExpect
	// findings holds the document finding that ended the vector, if any.
	findings []gvFinding
}

func newVectorCase(t *testing.T, doc *gvDocument, vector gvVector, state gvPriorState) *vectorCase {
	c := &vectorCase{t: t, doc: doc, vector: vector, state: state}
	if err := gvStrict(vector.Expect, &c.expect); err != nil {
		t.Fatalf("decode expect: %v", err)
	}
	return c
}

// checkHeader holds the statements every vector makes about itself: polarity
// agrees with the expected result, the expected error is a listed stable
// error, every declared definition has the disposition and planes the
// vector assumes in the registered catalog, every anchor key is the digest
// of its identity object, and every pack named uncovered is not covered.
func (c *vectorCase) checkHeader() {
	c.t.Helper()
	switch {
	case c.expect.Result == "accept" && (c.vector.Polarity == "positive" || c.vector.Polarity == "negative"):
	case c.vector.Polarity == "negative" && c.expect.Result == "reject":
		if !gvContains(c.doc.ErrorIDs, string(c.expect.ErrorID)) || c.expect.ErrorID.Validate() != nil {
			c.t.Fatalf("expected error %q is not a listed stable error", c.expect.ErrorID)
		}
		if !c.expect.StateUnchanged {
			c.t.Fatal("a negative vector must state that the state is unchanged")
		}
	default:
		c.t.Fatalf("polarity %q with result %q", c.vector.Polarity, c.expect.Result)
	}
	for _, area := range c.vector.Coverage {
		if !gvContains(c.doc.CoverageIDs, area) {
			c.t.Fatalf("coverage %q is not listed", area)
		}
	}
	catalog := c.registeredCatalog()
	for _, definition := range c.vector.Definitions {
		ref := definition.Ref.element(c.t)
		disposition, ok := catalog.DispositionOf(ref)
		if !ok {
			c.t.Fatalf("definition %s has no catalog entry", ref)
		}
		if string(disposition) != definition.Disposition {
			c.t.Fatalf("definition %s disposition = %s, the vector assumes %s", ref, disposition, definition.Disposition)
		}
		if definition.Planes != nil {
			planes, _ := catalog.PlanesOf(ref)
			if !gvEqualStrings(gvPlaneStrings(planes), definition.Planes) {
				c.t.Fatalf("definition %s planes = %v, the vector assumes %v", ref, planes, definition.Planes)
			}
		}
	}
	for _, anchor := range c.vector.AnchorKeys {
		if got := gvDigestJSON(c.t, anchor.Identity); got != anchor.Key {
			c.t.Fatalf("anchor key of %+v = %s, the vector states %s", anchor.Identity, got, anchor.Key)
		}
		if !gvContains(c.doc.SyntheticSchemes, anchor.Identity.Scheme) {
			c.t.Fatalf("anchor key scheme %q is not a synthetic scheme of the document", anchor.Identity.Scheme)
		}
	}
	for _, pack := range c.vector.UncoveredPacks {
		if catalog.Covers(gvValidator(pack).Definitions()) == nil {
			c.t.Fatalf("pack %s@%s is named uncovered but the catalog covers it", pack.ID, pack.Version)
		}
	}
}

// verdict compares an operation's result with the expectation and reports
// whether the operation was accepted.
func (c *vectorCase) verdict(err error) bool {
	c.t.Helper()
	if c.expect.Result == "accept" {
		if err != nil {
			c.t.Fatalf("rejected with %s (%v); %s", semreg.ErrorIdentifier(err), err, c.vector.Criterion)
		}
		return true
	}
	if err == nil {
		c.t.Fatalf("accepted; want %s; %s", c.expect.ErrorID, c.vector.Criterion)
	}
	if got := semreg.ErrorIdentifier(err); got != c.expect.ErrorID {
		c.t.Fatalf("rejected with %s (%v); want %s; %s", got, err, c.expect.ErrorID, c.vector.Criterion)
	}
	return false
}

// gvAssert runs every named assertion of the vector against the result of
// its operation. An assertion the harness does not implement fails.
func gvAssert[X any](c *vectorCase, table map[string]func(*vectorCase, X) error, result X) {
	c.t.Helper()
	for _, name := range c.expect.Assertions {
		check, ok := table[name]
		if !ok {
			c.t.Errorf("assertion %q is not implemented for %s", name, c.vector.Operation)
			continue
		}
		if err := check(c, result); err != nil {
			c.t.Errorf("assertion %q: %v", name, err)
		}
	}
}

// ----------------------------------------------------- catalog and packs

func gvEmbedded(t *testing.T) *graph.Catalog {
	t.Helper()
	catalog, err := graph.Embedded()
	if err != nil {
		t.Fatalf("Embedded() = %v", err)
	}
	return catalog
}

// gvSchemeCatalog is the pinned catalog with the synthetic identification
// schemes a prior state registers: the seed lists no scheme, and the vector
// document declares the synthetic ones its native anchors use.
type gvSchemeCatalog struct {
	semreg.GraphResourceCatalog
	schemes map[semreg.DefinitionID]struct{}
}

func (c gvSchemeCatalog) SchemeListed(id semreg.DefinitionID) bool {
	if _, ok := c.schemes[id]; ok {
		return true
	}
	return c.GraphResourceCatalog.SchemeListed(id)
}

// gvMutatedCatalog is the pinned catalog as a prior state registers it with
// stated catalog mutations: the decoded mutated document, presented under the
// pinned digest because the kernel admits no other catalog and the document
// states the mutations as part of the registration.
type gvMutatedCatalog struct{ *graph.Catalog }

func (gvMutatedCatalog) Digest() semreg.Digest { return graph.PinnedDigest }

// registeredCatalog is the catalog the vector's prior state registers: the
// pinned catalog, with the prior state's catalog mutations applied.
func (c *vectorCase) registeredCatalog() *graph.Catalog {
	c.t.Helper()
	if c.state.Registry == nil {
		return gvEmbedded(c.t)
	}
	return c.mutatedCatalog(c.state.Registry)
}

// mutatedCatalog applies the registry's catalog mutations to a copy of the
// pinned catalog. Each mutation names one member entry once.
func (c *vectorCase) mutatedCatalog(registry *gvRegistry) *graph.Catalog {
	c.t.Helper()
	if len(registry.CatalogMutations) == 0 {
		return gvEmbedded(c.t)
	}
	if registry.ElementCatalog != "registered" {
		c.t.Fatal("catalog mutations need a registered element catalog")
	}
	doc := gvObject(c.t, gvEmbedded(c.t).Bytes())
	for _, mutation := range registry.CatalogMutations {
		if _, ok := mutation["set_disposition"]; !ok || len(mutation) != 1 {
			c.t.Fatalf("catalog mutation %v is not one set_disposition", gvSortedKeys(mutation))
		}
		gvMutateCatalog(c, doc, mutation)
	}
	catalog, err := graph.Decode(gvMarshal(c.t, doc))
	if err != nil {
		c.t.Fatalf("decode the mutated catalog: %v", err)
	}
	return catalog
}

func (c *vectorCase) catalogFor(registry *gvRegistry) semreg.GraphCatalog {
	c.t.Helper()
	var catalog semreg.GraphResourceCatalog = gvEmbedded(c.t)
	if len(registry.CatalogMutations) != 0 {
		catalog = gvMutatedCatalog{c.mutatedCatalog(registry)}
	}
	if len(registry.IdentificationSchemes) == 0 {
		return catalog
	}
	schemes := map[semreg.DefinitionID]struct{}{}
	for _, scheme := range registry.IdentificationSchemes {
		if !gvContains(c.doc.SyntheticSchemes, scheme) {
			c.t.Fatalf("registered scheme %q is not a synthetic scheme of the document", scheme)
		}
		schemes[semreg.DefinitionID(scheme)] = struct{}{}
	}
	return gvSchemeCatalog{GraphResourceCatalog: catalog, schemes: schemes}
}

func gvAcceptedValidators() []semreg.PackValidator {
	return []semreg.PackValidator{evse.New(), infrastructure.New(), pv.New(), storage.New(), thermal.New()}
}

// gvValidator returns the accepted pack's validator, or a synthetic pack
// with one service for a pack reference no accepted pack has.
func gvValidator(ref semreg.PackRef) semreg.PackValidator {
	for _, validator := range gvAcceptedValidators() {
		if validator.Pack() == ref {
			return validator
		}
	}
	return gvSyntheticPack{pack: ref}
}

func gvValidators(refs []semreg.PackRef) []semreg.PackValidator {
	out := make([]semreg.PackValidator, 0, len(refs))
	for _, ref := range refs {
		out = append(out, gvValidator(ref))
	}
	return out
}

type gvSyntheticPack struct{ pack semreg.PackRef }

func (p gvSyntheticPack) Pack() semreg.PackRef { return p.pack }
func (p gvSyntheticPack) Definitions() semreg.DefinitionIndex {
	return semreg.DefinitionIndex{
		Pack: p.pack, Fields: []semreg.DefinitionRef{},
		Services:     []semreg.DefinitionRef{{Pack: p.pack, ID: "service.synthetic", Version: "1.0.0"}},
		Capabilities: []semreg.DefinitionRef{}, Operations: []semreg.DefinitionRef{}, EffectRules: []semreg.DefinitionRef{},
	}
}
func (gvSyntheticPack) ValidateFact(semreg.FactKey, *semreg.Value) error   { return nil }
func (gvSyntheticPack) ValidateService(semreg.ServiceInstance) error       { return nil }
func (gvSyntheticPack) ValidateCapability(semreg.CapabilityInstance) error { return nil }
func (gvSyntheticPack) ValidateField(semreg.DefinitionRef, semreg.TypedField) error {
	return nil
}
func (gvSyntheticPack) MatchConstraints(semreg.CapabilityInstance, []semreg.TypedField) error {
	return nil
}
func (gvSyntheticPack) EvaluatePredicate(semreg.FactCandidate, semreg.PredicateOp, semreg.Value) (bool, error) {
	return false, nil
}

func (r gvElementRef) element(t *testing.T) graph.ElementRef {
	t.Helper()
	switch r.Owner.Kind {
	case "pack":
		if r.Owner.Pack == nil {
			t.Fatalf("pack owner without pack in %+v", r)
		}
		return graph.PackElement(semreg.DefinitionRef{Pack: *r.Owner.Pack, ID: r.ID, Version: r.Version})
	case "core":
		ref := graph.CoreElement(r.ID, r.Version)
		if string(ref.Owner.Contract) != r.Owner.Contract {
			t.Fatalf("core owner contract %q", r.Owner.Contract)
		}
		return ref
	}
	t.Fatalf("owner kind %q", r.Owner.Kind)
	return graph.ElementRef{}
}

// packRef is the kernel definition reference of a pack-owned element.
func (r gvElementRef) packRef(t *testing.T) semreg.DefinitionRef {
	t.Helper()
	ref, ok := r.element(t).DefinitionRef()
	if !ok {
		t.Fatalf("%s is not pack-owned", r.element(t))
	}
	return ref
}

// ------------------------------------------------------------ publication

// gvPublisher is one asset's publication kernel built from a prior state.
type gvPublisher struct {
	c       *vectorCase
	kernel  *semreg.PublicationKernel
	applied uint64
	last    semreg.PublicationBatch
}

// publisher constructs the kernel the prior state names and applies every
// batch of the prior state; filter, when set, edits each batch first.
func (c *vectorCase) publisher(asset semreg.AssetID, filter func(map[string]any)) *gvPublisher {
	c.t.Helper()
	registry := c.state.Registry
	if registry == nil {
		c.t.Fatalf("prior state %s has no registry for %s", c.state.Name, c.vector.Operation)
	}
	validators := gvValidators(registry.Packs)
	var kernel *semreg.PublicationKernel
	var err error
	switch registry.ElementCatalog {
	case "registered":
		kernel, err = semreg.NewGraphPublicationKernel(asset, c.catalogFor(registry), validators...)
	case "absent":
		kernel, err = semreg.NewPublicationKernel(asset, validators...)
	default:
		c.t.Fatalf("element_catalog %q", registry.ElementCatalog)
	}
	if err != nil {
		c.t.Fatalf("prior state %s: kernel construction: %v", c.state.Name, err)
	}
	p := &gvPublisher{c: c, kernel: kernel}
	for i, raw := range c.state.Batches {
		_, err := p.apply(raw, filter)
		c.checkRecord(fmt.Sprintf("prior state %s batch %d", c.state.Name, i), raw, err, true)
	}
	return p
}

// apply completes, strictly decodes and applies one batch.
func (p *gvPublisher) apply(raw json.RawMessage, filter func(map[string]any)) (semreg.Snapshot, error) {
	p.c.t.Helper()
	object := gvObject(p.c.t, raw)
	if filter != nil {
		filter(object)
	}
	p.applied++
	p.expandBatch(object)
	batch, err := semreg.Decode[semreg.PublicationBatch](gvMarshal(p.c.t, object))
	if err != nil {
		return semreg.Snapshot{}, err
	}
	p.last = batch
	snapshot, _, err := p.kernel.Apply(batch, gvMonotonic(p.applied))
	return snapshot, err
}

func (p *gvPublisher) current() (semreg.Snapshot, []byte, bool) { return p.kernel.Current() }

func (p *gvPublisher) expandBatch(batch map[string]any) {
	gvDefault(batch, "contract", string(graph.KernelContract))
	gvDefault(batch, "observed_at", gvTimeJSON(0))
	for _, name := range []string{
		"source_retirements", "binding_upserts", "identity_link_upserts", "fact_upserts", "fact_withdrawals",
		"service_upserts", "service_withdrawals", "capability_upserts", "capability_withdrawals", "generation_fences",
	} {
		gvDefault(batch, name, []any{})
	}
	if _, present := batch["source_upserts"]; !present {
		batch["source_upserts"] = []any{}
		source, _ := batch["source_id"].(string)
		epoch, _ := batch["source_epoch_id"].(string)
		snapshot, _, ok := p.current()
		known := false
		if ok {
			for _, descriptor := range snapshot.Sources {
				known = known || string(descriptor.SourceID) == source && string(descriptor.SourceEpochID) == epoch
			}
		}
		if !known {
			batch["source_upserts"] = []any{map[string]any{
				"source_id": source, "source_epoch_id": epoch,
				"protocol_id": "protocol.synthetic.vector", "profile_id": "profile.synthetic.vector", "profile_version": "1",
				"registry_evidence": gvEvidenceJSON("source " + epoch), "started_at": gvTimeJSON(0),
				"state": "current", "revision": "1",
			}}
		}
	}
	if candidates, ok := batch["fact_upserts"].([]any); ok {
		for _, item := range candidates {
			candidate, ok := item.(map[string]any)
			if !ok {
				continue
			}
			monotonic := gvMonotonicJSON(p.applied)
			gvDefault(candidate, "times", map[string]any{
				"received_at": gvTimeJSON(0), "receipt_monotonic": monotonic,
				"evaluated_at": gvTimeJSON(0), "evaluate_monotonic": monotonic,
			})
			gvDefault(candidate, "freshness_policy", map[string]any{
				"policy_id": "policy:synthetic", "version": "1.0.0", "fresh_for_ns": "30000000000",
				"retain_for_ns": "120000000000", "max_wall_uncertainty_ns": "1000000",
			})
			if quality, ok := candidate["quality"].(map[string]any); ok {
				gvDefault(quality, "availability", "available")
				gvDefault(quality, "freshness", "fresh")
				gvDefault(quality, "reasons", []any{})
			}
			if derivation, ok := candidate["derivation"].(map[string]any); ok {
				inputs, _ := derivation["inputs"].([]any)
				for _, item := range inputs {
					if input, ok := item.(map[string]any); ok {
						if paths, found := p.sourcePaths(input["candidate_id"]); found {
							gvDefault(input, "source_paths", paths)
						}
					}
				}
			}
		}
	}
	if _, present := batch["batch_digest"]; !present {
		// The digest is sha256 over the JCS bytes of every other member in the
		// kernel's wire form. ComputedDigest validates first, so a batch that
		// a negative vector makes invalid gets the same digest computed from
		// its bound form; a batch the binding cannot read keeps a well-formed
		// digest so that the strict decoder reports the batch's own defect.
		var partial semreg.PublicationBatch
		digest := gvDigest("batch " + fmt.Sprint(batch["batch_id"]))
		if json.Unmarshal(gvMarshal(p.c.t, batch), &partial) == nil {
			if computed, err := partial.ComputedDigest(); err == nil {
				digest = computed
			} else {
				bound := gvObject(p.c.t, gvMarshal(p.c.t, partial))
				delete(bound, "batch_digest")
				digest = gvDigestJSON(p.c.t, bound)
			}
		}
		batch["batch_digest"] = string(digest)
	}
}

// sourcePaths are the native source paths of a derivation input, read from
// the candidate it names in the current snapshot: the one path of an observed
// candidate, or the sorted union of the paths of an inferred one's inputs.
func (p *gvPublisher) sourcePaths(id any) ([]any, bool) {
	snapshot, _, ok := p.current()
	if !ok {
		return nil, false
	}
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			if string(candidate.CandidateID) != fmt.Sprint(id) {
				continue
			}
			paths := []semreg.SourcePathRef{}
			switch {
			case candidate.Derivation != nil:
				for _, input := range candidate.Derivation.Inputs {
					paths = append(paths, input.SourcePaths...)
				}
			case candidate.BindingID != nil && candidate.SourceEpochID != nil && candidate.DriverGeneration != nil && candidate.Origin.SourceID != nil:
				paths = append(paths, semreg.SourcePathRef{
					BindingID: *candidate.BindingID, SourceID: *candidate.Origin.SourceID,
					SourceEpochID: *candidate.SourceEpochID, DriverGeneration: *candidate.DriverGeneration,
				})
			default:
				return nil, false
			}
			sort.Slice(paths, func(i, j int) bool {
				a, b := paths[i], paths[j]
				if a.SourceID != b.SourceID {
					return a.SourceID < b.SourceID
				}
				if a.SourceEpochID != b.SourceEpochID {
					return a.SourceEpochID < b.SourceEpochID
				}
				if a.DriverGeneration != b.DriverGeneration {
					// Decimal strings without leading zeros order by length first.
					if len(a.DriverGeneration) != len(b.DriverGeneration) {
						return len(a.DriverGeneration) < len(b.DriverGeneration)
					}
					return a.DriverGeneration < b.DriverGeneration
				}
				return a.BindingID < b.BindingID
			})
			out := []any{}
			for i, path := range paths {
				if i > 0 && path == paths[i-1] {
					continue
				}
				out = append(out, gvAny(p.c.t, gvMarshal(p.c.t, path)))
			}
			return out, true
		}
	}
	return nil, false
}

// --------------------------------------------------------------- intents

func (c *vectorCase) expandIntent(input map[string]any, snapshot semreg.Snapshot) (operation.Intent, error) {
	c.t.Helper()
	context := c.state.AdmissionContext
	if context == nil {
		c.t.Fatalf("prior state %s has no admission context", c.state.Name)
	}
	intent := gvObject(c.t, gvMarshal(c.t, input))
	id, _ := intent["intent_id"].(string)
	gvDefault(intent, "contract", string(operation.ContractOperationV1))
	arguments := []any{}
	if effect, ok := intent["expected_effect"].(map[string]any); ok {
		fact, _ := effect["fact"].(map[string]any)
		arguments = append(arguments, map[string]any{"id": fact["fact_id"], "value": effect["expected"]})
	}
	gvDefault(intent, "arguments", arguments)
	gvDefault(intent, "causal", map[string]any{
		"origin": map[string]any{
			"origin_id": "origin:operator:" + id, "kind": "operator",
			"evidence": []any{gvEvidenceJSON("causal " + id)},
		},
		"correlation_id": "correlation:" + id, "hop_count": 0, "max_hops": 1,
		"first_seen_at": gvTimeJSON(0), "expires_at": gvTimeJSON(gvLifetimeNS), "path": []any{},
	})
	gvDefault(intent, "expected_semantic_revision", string(snapshot.Revisions.Semantic))
	gvDefault(intent, "expected_capability_revision", string(snapshot.Revisions.Capabilities))
	gvDefault(intent, "expected_capability_instance_revision", string(c.capabilityRevision(intent, snapshot)))
	gvDefault(intent, "preconditions", []any{})
	gvDefault(intent, "idempotency_key", "idempotency:"+id)
	deadline := gvTimeJSON(0)
	if context.DeadlineOpen {
		deadline = gvTimeJSON(gvLifetimeNS)
	}
	gvDefault(intent, "deadline", deadline)
	raw := gvMarshal(c.t, intent)
	if _, ok := intent["expected_effect"]; ok {
		return operation.DecodeIntent(raw)
	}
	// An intent that states no expected effect cannot pass the strict decoder;
	// it is bound without that member and still refused on any unknown one.
	var out operation.Intent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		c.t.Fatalf("bind intent: %v", err)
	}
	return out, nil
}

// capabilityRevision is the revision of the capability instances the intent
// can name: the pinned instance, or every instance of the required
// definition when they agree.
func (c *vectorCase) capabilityRevision(intent map[string]any, snapshot semreg.Snapshot) semreg.Uint64 {
	c.t.Helper()
	required, _ := intent["required_capability"].(map[string]any)
	definition, _ := required["definition_id"].(string)
	pinned, _ := required["instance_id"].(string)
	revisions := map[semreg.Uint64]struct{}{}
	for _, capability := range snapshot.Capabilities {
		if string(capability.Definition.ID) != definition || pinned != "" && string(capability.InstanceID) != pinned {
			continue
		}
		revisions[capability.Revision] = struct{}{}
	}
	switch len(revisions) {
	case 0:
		return "1"
	case 1:
		for revision := range revisions {
			return revision
		}
	}
	c.t.Fatalf("capability instances of %s disagree on their revision", definition)
	return ""
}

func (c *vectorCase) admissionContext() semreg.EvaluationContext {
	return semreg.EvaluationContext{EvaluatedAt: gvTimePoint(0), EvaluateMonotonic: gvMonotonic(1 << 20)}
}

func (c *vectorCase) authority() operation.AuthorityResolver {
	resolves := c.state.AdmissionContext != nil && c.state.AdmissionContext.AuthorityResolves
	return operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error {
		if resolves {
			return nil
		}
		return fmt.Errorf("authority does not resolve")
	})
}

// ------------------------------------------------------------- values

func gvTimePoint(offset int64) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(strconv.FormatInt(gvWallNS+offset, 10)), ClockID: "clock.utc", UncertaintyNS: "0"}
}

func gvTimeJSON(offset int64) map[string]any {
	return map[string]any{"unix_nanoseconds": strconv.FormatInt(gvWallNS+offset, 10), "clock_id": "clock.utc", "uncertainty_ns": "0"}
}

func gvMonotonic(step uint64) semreg.MonotonicPoint {
	return semreg.MonotonicPoint{ClockEpochID: gvClockEpoch, Nanoseconds: semreg.Uint64(strconv.FormatUint(step*gvMonotonicStep, 10))}
}

func gvMonotonicJSON(step uint64) map[string]any {
	return map[string]any{"clock_epoch_id": gvClockEpoch, "nanoseconds": strconv.FormatUint(step*gvMonotonicStep, 10)}
}

func gvDigest(label string) semreg.Digest {
	sum := sha256.Sum256([]byte("graph vector " + label))
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func gvEvidenceJSON(label string) map[string]any {
	return map[string]any{
		"owner": gvEvidenceOwner, "kind": gvEvidenceKind, "digest": string(gvDigest(label)),
		"contract": gvEvidenceContract, "access": "public", "redaction": "none",
	}
}

// gvDigestJSON is sha256 over the JCS bytes of a value of ASCII strings.
func gvDigestJSON(t *testing.T, value any) semreg.Digest {
	t.Helper()
	sum := sha256.Sum256(gvCanonical(t, value))
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// gvCanonical encodes a value with sorted object members and no HTML
// escaping, which is JCS for the ASCII strings and integers vectors hold.
func gvCanonical(t *testing.T, value any) []byte {
	t.Helper()
	var generic any
	if raw, ok := value.(json.RawMessage); ok {
		generic = gvAny(t, raw)
	} else {
		generic = gvAny(t, gvMarshal(t, value))
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(generic); err != nil {
		t.Fatal(err)
	}
	return bytes.TrimRight(out.Bytes(), "\n")
}

func gvSameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	return bytes.Equal(gvCanonical(t, a), gvCanonical(t, b))
}

func gvAny(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out any
	if err := decoder.Decode(&out); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return out
}

func gvObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	object, ok := gvAny(t, raw).(map[string]any)
	if !ok {
		t.Fatalf("not a JSON object: %s", raw)
	}
	return object
}

func gvMarshal(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func gvDefault(object map[string]any, member string, value any) {
	if _, present := object[member]; !present {
		object[member] = value
	}
}

func gvPlaneStrings(planes []graph.PlaneID) []string {
	out := make([]string, len(planes))
	for i, plane := range planes {
		out[i] = string(plane)
	}
	return out
}

func gvContains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func gvDeepEqual(a, b any) bool { return reflect.DeepEqual(a, b) }

// --------------------------------------------------------------- decoding

func gvReadFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(graphVectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func gvDecodeDocument(t *testing.T, raw []byte) *gvDocument {
	t.Helper()
	var doc gvDocument
	if err := gvStrict(raw, &doc); err != nil {
		t.Fatalf("decode graph vectors: %v", err)
	}
	return &doc
}

// gvStrict decodes one JSON value with no unknown member, no repeated member
// at any level and no trailing data.
func gvStrict(raw []byte, out any) error {
	if err := gvRejectRepeated(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing data")
	}
	return nil
}

func gvRejectRepeated(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]struct{}{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name := key.(string)
				if _, repeated := seen[name]; repeated {
					return fmt.Errorf("repeated member %q", name)
				}
				seen[name] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case json.Delim('['):
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		}
		return nil
	}
	return walk()
}

func gvSortedKeys[K ~string, V any](m map[K]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, string(key))
	}
	sort.Strings(out)
	return out
}

func gvEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
