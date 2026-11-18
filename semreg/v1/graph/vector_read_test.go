package graph_test

import (
	"encoding/json"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

// Read vectors name member snapshots and a request. The snapshots in the
// document are abbreviated: the harness completes them, and a record the
// kernel refuses is a document finding when a rule of vector_findings_test.go
// names that refusal. No vector is selected by identifier. A record the
// kernel accepts is listed, walked, retrieved, summed or projected, and the
// result is compared with expect.read.

type gvReadInput struct {
	Completeness string                   `json:"completeness,omitempty"`
	Snapshots    []json.RawMessage        `json:"snapshots"`
	Request      json.RawMessage          `json:"request"`
	NotIncluded  []graph.NotIncludedAsset `json:"not_included,omitempty"`
	Findings     json.RawMessage          `json:"findings,omitempty"`
}

type gvReadRequest struct {
	CutID       *semreg.Digest           `json:"cut_id,omitempty"`
	Kind        *semreg.DefinitionID     `json:"kind,omitempty"`
	Plane       *graph.PlaneID           `json:"plane,omitempty"`
	PageSize    int                      `json:"page_size,omitempty"`
	PageToken   string                   `json:"page_token,omitempty"`
	Entry       graph.EntryPoint         `json:"entry,omitempty"`
	Lens        semreg.DefinitionID      `json:"lens,omitempty"`
	Planes      []graph.PlaneID          `json:"planes,omitempty"`
	Definitions []json.RawMessage        `json:"definitions,omitempty"`
	Direction   graph.TraversalDirection `json:"direction,omitempty"`
	Depth       int                      `json:"depth,omitempty"`
	Fact        json.RawMessage          `json:"fact,omitempty"`
	Scope       semreg.ResourceID        `json:"scope,omitempty"`
	Report      json.RawMessage          `json:"report,omitempty"`
}

func (c *vectorCase) readInput() gvReadInput {
	c.t.Helper()
	var input gvReadInput
	if err := gvStrict(c.vector.Input, &input); err != nil {
		c.t.Fatalf("decode input: %v", err)
	}
	if input.Snapshots == nil || len(input.Request) == 0 {
		c.t.Fatal("a read is member snapshots plus a request")
	}
	return input
}

func (c *vectorCase) readRequest(raw json.RawMessage) gvReadRequest {
	c.t.Helper()
	var request gvReadRequest
	if err := gvStrict(raw, &request); err != nil {
		c.t.Fatalf("decode request: %v", err)
	}
	if request.Entry.Kind == graph.EntryLensRoots && request.Entry.Lens == "" {
		request.Entry.Lens = request.Lens
	}
	return request
}

func (c *vectorCase) readSnapshots(raws []json.RawMessage) ([]semreg.Snapshot, error) {
	c.t.Helper()
	snapshots := make([]semreg.Snapshot, 0, len(raws))
	for _, raw := range raws {
		snapshot, err := semreg.Decode[semreg.Snapshot](raw)
		c.checkRecord("input snapshot", raw, err, false)
		if err != nil {
			snapshot, err = semreg.Decode[semreg.Snapshot](gvCompleteReadSnapshot(c.t, raw))
			c.checkRecord("input snapshot", raw, err, false)
		}
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func (c *vectorCase) retainRead(input gvReadInput, snapshots []semreg.Snapshot) (*graph.CutStore, error) {
	c.t.Helper()
	completeness := graph.CutComplete
	if input.Completeness != "" {
		completeness = graph.Completeness(input.Completeness)
	}
	cut, err := graph.AssembleCut(snapshots, c.registeredCatalog(), completeness)
	if err != nil {
		return nil, err
	}
	store := graph.NewCutStore(func() time.Time { return time.Unix(0, gvWallNS).UTC() })
	if err := store.Retain(cut, snapshots, c.registeredCatalog()); err != nil {
		return nil, err
	}
	return store, nil
}

func gvReadBounds(catalog *graph.Catalog, request gvReadRequest) (graph.TraversalDirection, int, int) {
	direction := request.Direction
	if direction == "" {
		direction = graph.DirectionOutgoing
	}
	pageSize := request.PageSize
	if pageSize == 0 {
		pageSize = graph.MaxPageSize
	}
	depth := request.Depth
	if depth == 0 {
		depth = graph.MaxTraversalDepth
		for _, lens := range catalog.Lenses() {
			if lens.ID == request.Lens && lens.MaxDepth > 0 {
				depth = lens.MaxDepth
				break
			}
		}
	}
	return direction, depth, pageSize
}

func (c *vectorCase) retrieveLens(store *graph.CutStore, input gvReadInput, request gvReadRequest, snapshots []semreg.Snapshot) (graph.LensResult, error) {
	c.t.Helper()
	direction, depth, pageSize := gvReadBounds(c.registeredCatalog(), request)
	return graph.RetrieveLens(store, c.registeredCatalog(), graph.LensRequest{
		CutID: request.CutID, Lens: request.Lens, Entry: request.Entry,
		Direction: direction, Depth: depth, PageSize: pageSize, PageToken: request.PageToken,
		Members: snapshots, NotIncluded: input.NotIncluded,
	}, c.admissionContext())
}

func (c *vectorCase) compareRead(result any) {
	c.t.Helper()
	if len(c.expect.Read) == 0 {
		c.t.Fatal("an accepted read states no read")
	}
	if !gvSameJSON(c.t, result, json.RawMessage(c.expect.Read)) {
		c.t.Errorf("read differs from the vector")
	}
}

func runListResources(c *vectorCase) {
	c.t.Helper()
	input := c.readInput()
	snapshots, err := c.readSnapshots(input.Snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	store, err := c.retainRead(input, snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	request := c.readRequest(input.Request)
	_, _, pageSize := gvReadBounds(c.registeredCatalog(), request)
	page, err := store.ListResources(graph.ListRequest{
		CutID: request.CutID, Kind: request.Kind, Plane: request.Plane,
		PageSize: pageSize, PageToken: request.PageToken,
	})
	if !c.verdict(err) {
		return
	}
	c.compareRead(page)
}

func runTraverse(c *vectorCase) {
	c.t.Helper()
	input := c.readInput()
	snapshots, err := c.readSnapshots(input.Snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	store, err := c.retainRead(input, snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	request := c.readRequest(input.Request)
	definitions, err := c.definitionRefs(request.Definitions)
	if err != nil {
		c.verdict(err)
		return
	}
	direction, depth, pageSize := gvReadBounds(c.registeredCatalog(), request)
	page, err := store.Traverse(graph.TraversalRequest{
		CutID: request.CutID, Entry: request.Entry, Lens: request.Lens, Planes: request.Planes,
		Definitions: definitions, Direction: direction, Depth: depth, PageSize: pageSize,
		PageToken: request.PageToken,
	})
	if !c.verdict(err) {
		return
	}
	c.compareRead(page)
}

func runRetrieveLens(c *vectorCase) {
	c.t.Helper()
	input := c.readInput()
	snapshots, err := c.readSnapshots(input.Snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	store, err := c.retainRead(input, snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	request := c.readRequest(input.Request)
	result, err := c.retrieveLens(store, input, request, snapshots)
	if !c.verdict(err) {
		return
	}
	c.compareRead(result)
}

func runSumMembers(c *vectorCase) {
	c.t.Helper()
	input := c.readInput()
	snapshots, err := c.readSnapshots(input.Snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	store, err := c.retainRead(input, snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	request := c.readRequest(input.Request)
	result, err := c.retrieveLens(store, input, request, snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	fact, err := semreg.Decode[semreg.DefinitionRef](request.Fact)
	c.checkRecord("sum fact", request.Fact, err, false)
	if err != nil {
		c.verdict(err)
		return
	}
	sum, err := graph.SumMembers(result, fact, request.Scope)
	if !c.verdict(err) {
		return
	}
	c.compareRead(sum)
}

func runProject(c *vectorCase) {
	c.t.Helper()
	input := c.readInput()
	snapshots, err := c.readSnapshots(input.Snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	request := c.readRequest(input.Request)
	report, err := semreg.Decode[projection.ProjectionReport](request.Report)
	c.checkRecord("projection report", request.Report, err, false)
	if err != nil {
		c.verdict(err)
		return
	}
	store, err := c.retainRead(input, snapshots)
	if err != nil {
		c.verdict(err)
		return
	}
	var lens []*graph.LensResult
	if request.Lens != "" || report.Manifest.Lens != nil {
		result, err := c.retrieveLens(store, input, request, snapshots)
		if err != nil {
			c.verdict(err)
			return
		}
		lens = []*graph.LensResult{&result}
	}
	snapshot := snapshots[0]
	for _, candidate := range snapshots {
		if candidate.SnapshotID == report.SnapshotID {
			snapshot = candidate
			break
		}
	}
	projected, err := projection.Project(snapshot, report.Manifest, report.Requested, report.Dispositions, report.Causal, lens...)
	if !c.verdict(err) {
		return
	}
	c.compareRead(projected)
}

func (c *vectorCase) definitionRefs(raws []json.RawMessage) ([]semreg.DefinitionRef, error) {
	c.t.Helper()
	refs := make([]semreg.DefinitionRef, 0, len(raws))
	for _, raw := range raws {
		ref, err := semreg.Decode[semreg.DefinitionRef](raw)
		c.checkRecord("request definition", raw, err, false)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// gvCompleteReadSnapshot adds the kernel members a read snapshot omits and
// drops a present empty graph collection. It does not change snapshot_id, a
// definition or a coefficient.
func gvCompleteReadSnapshot(t *testing.T, raw json.RawMessage) json.RawMessage {
	object := gvObject(t, raw)
	asset, _ := object["asset_id"].(string)
	label := asset
	if label == "" {
		label = "synthetic"
	}
	source, epoch, binding := "source:"+label, "epoch:"+label, "binding:"+label
	if resources, ok := object["resources"].([]any); ok {
		for _, item := range resources {
			resource, ok := item.(map[string]any)
			if !ok {
				continue
			}
			id, _ := resource["resource_id"].(string)
			gvDefault(resource, "origin", gvOperatorOriginJSON(id))
			gvDefault(resource, "revision", "1")
			if anchor, ok := resource["anchor"].(map[string]any); ok {
				gvDefault(anchor, "evidence", []any{gvEvidenceJSON("anchor " + id)})
			}
		}
	}
	if relations, ok := object["relations"].([]any); ok {
		for _, item := range relations {
			relation, ok := item.(map[string]any)
			if !ok {
				continue
			}
			gvDefault(relation, "basis_class", "configuration")
			gvDefault(relation, "basis", []any{gvEvidenceJSON("relation")})
			gvDefault(relation, "origin", gvOperatorOriginJSON("relation"))
			gvDefault(relation, "revision", "1")
		}
	}
	if facts, ok := object["facts"].([]any); ok {
		for _, item := range facts {
			envelope, ok := item.(map[string]any)
			if !ok {
				continue
			}
			gvDefault(envelope, "asset_id", asset)
			gvDefault(envelope, "conflicts", []any{})
			gvDefault(envelope, "revision", "1")
			candidates, _ := envelope["candidates"].([]any)
			for _, item := range candidates {
				gvCompleteCandidate(item, source, epoch, binding)
			}
		}
	}
	for _, name := range []string{"resources", "relations", "relation_absences"} {
		if items, ok := object[name].([]any); ok && len(items) == 0 {
			delete(object, name)
		}
	}
	gvDefault(object, "contract", string(semreg.ContractKernelV1))
	gvDefault(object, "revisions", map[string]any{
		"semantic": "1", "identity": "1", "facts": "1", "services": "1", "capabilities": "1",
	})
	if revisions, ok := object["revisions"].(map[string]any); ok {
		if _, resources := object["resources"]; resources {
			gvDefault(revisions, "structure", "1")
		}
		if _, relations := object["relations"]; relations {
			gvDefault(revisions, "structure", "1")
		}
	}
	gvDefault(object, "evaluated_at", gvTimeJSON(0))
	gvDefault(object, "evaluate_monotonic", gvMonotonicJSON(1))
	gvDefault(object, "sources", []any{map[string]any{
		"source_id": source, "source_epoch_id": epoch,
		"protocol_id": "protocol.synthetic.vector", "profile_id": "profile.synthetic.vector", "profile_version": "1",
		"registry_evidence": gvEvidenceJSON("source " + label), "started_at": gvTimeJSON(0),
		"state": "current", "revision": "1",
	}})
	gvDefault(object, "bindings", []any{map[string]any{
		"binding_id": binding, "asset_id": asset, "source_id": source, "source_epoch_id": epoch,
		"driver_generation": "1", "native_resource": gvEvidenceJSON("native " + label),
		"state": "current", "revision": "1",
	}})
	gvDefault(object, "identity_links", []any{})
	gvDefault(object, "facts", []any{})
	gvDefault(object, "services", []any{})
	gvDefault(object, "capabilities", []any{})
	gvDefault(object, "fences", []any{})
	gvDefault(object, "cursors", []any{map[string]any{
		"source_id": source, "source_epoch_id": epoch, "driver_generation": "1",
		"last_sequence": "1", "last_batch_digest": string(gvDigest("cursor " + label)), "fenced": false,
	}})
	return gvMarshal(t, object)
}

func gvCompleteCandidate(item any, source, epoch, binding string) {
	candidate, ok := item.(map[string]any)
	if !ok {
		return
	}
	id, _ := candidate["candidate_id"].(string)
	if quality, ok := candidate["quality"].(map[string]any); ok {
		gvDefault(quality, "assertion", "observed")
		gvDefault(quality, "qualification", "qualified")
		gvDefault(quality, "promotion", "promoted")
		gvDefault(quality, "reasons", []any{})
	}
	monotonic := gvMonotonicJSON(1)
	gvDefault(candidate, "times", map[string]any{
		"received_at": gvTimeJSON(0), "receipt_monotonic": monotonic,
		"evaluated_at": gvTimeJSON(0), "evaluate_monotonic": monotonic,
	})
	gvDefault(candidate, "freshness_policy", map[string]any{
		"policy_id": "policy:synthetic", "version": "1.0.0", "fresh_for_ns": "30000000000",
		"retain_for_ns": "120000000000", "max_wall_uncertainty_ns": "1000000",
	})
	gvDefault(candidate, "binding_id", binding)
	gvDefault(candidate, "source_epoch_id", epoch)
	gvDefault(candidate, "driver_generation", "1")
	gvDefault(candidate, "origin", map[string]any{
		"origin_id": "origin:native:" + id, "kind": "native_observation",
		"source_id": source, "source_epoch_id": epoch, "binding_id": binding,
		"evidence": []any{gvEvidenceJSON("origin " + id)},
	})
	gvDefault(candidate, "evidence", []any{gvEvidenceJSON("candidate " + id)})
	gvDefault(candidate, "revision", "1")
}

func gvOperatorOriginJSON(label string) map[string]any {
	if label == "" {
		label = "synthetic"
	}
	return map[string]any{
		"origin_id": "origin:operator:" + label, "kind": "operator",
		"evidence": []any{gvEvidenceJSON("origin " + label)},
	}
}
