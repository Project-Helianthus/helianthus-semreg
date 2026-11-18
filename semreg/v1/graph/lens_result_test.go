package graph_test

import (
	"bytes"
	"go/build"
	"path/filepath"
	"strings"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func lensCatalog(t *testing.T) *graph.Catalog {
	t.Helper()
	return decodeLensCatalog(t, "What is this piece of equipment made of?")
}

func decodeLensCatalog(t *testing.T, question string) *graph.Catalog {
	t.Helper()
	doc := decodeDocument(t)
	core := document{"kind": "core", "contract": string(graph.KernelContract)}
	pack := packOwner(cutPack)
	planes := func(ids ...string) []any {
		out := []any{}
		for _, id := range ids {
			out = append(out, id)
		}
		return out
	}
	entry := func(owner document, id, kind, disposition string, planeList []any, attributes document) document {
		return document{
			"ref": elementRef(owner, id), "element_kind": kind, "disposition": disposition, "planes": planeList,
			"admission":  document{"arm": "accepted_pack", "evidence": []any{"https://github.com/Project-Helianthus/helianthus-docs-semantic/issues/67"}},
			"attributes": attributes,
		}
	}
	kind := func(id, class string, planeList []any, root bool) document {
		attributes := document{"anchor_class": class}
		if root {
			attributes["installation_root"] = true
		}
		return entry(core, id, "resource_kind", "member", planeList, attributes)
	}
	relation := func(ref semreg.DefinitionRef, coreType, from, to, cardinality, medium string, planeList []any) document {
		attributes := document{
			"core_type": coreType, "orientation": "as_core", "from_kinds": planes(from), "to_kinds": planes(to),
			"cardinality": cardinality, "over_time": false, "basis_classes": planes("configuration"),
		}
		if medium != "" {
			attributes["medium"] = medium
		}
		return entry(pack, string(ref.ID), "relation", "member", planeList, attributes)
	}
	deviceDimension := elementRef(pack, dimensionDevice)
	resolved := document{"status": "resolved", "dimension": deviceDimension, "kind": kindDevice}
	fact := func(id string, planeList []any) document {
		return entry(pack, id, "fact", "member", planeList, document{
			"value_kind": "boolean", "subject": resolved, "aggregation": "not_applicable", "direction": "not_applicable",
		})
	}
	doc["identification_schemes"] = []any{}
	doc["lenses"] = []any{
		document{
			"id": "lens.equipment_composition", "question": question,
			"planes": planes("physical"), "root_kinds": planes(kindDevice), "max_depth": 8,
		},
		document{
			"id": "lens.part_composition", "question": "Which components belong to this equipment?",
			"planes": planes("physical"), "root_kinds": planes(kindDevice), "max_depth": 8,
		},
	}
	doc["elements"] = []any{
		kind(kindDevice, "equipment", planes("physical", "energy"), false),
		kind(kindPart, "component", planes("physical"), false),
		kind("resource.site", "function", planes("physical"), true),
		relation(relDevicePart, "contains", kindDevice, kindPart, "one_to_many", "", planes("physical")),
		relation(cutRef("cut.relation.part_nest"), "contains", kindPart, kindPart, "one_to_many", "", planes("physical")),
		relation(relSupplies, "supplies", kindDevice, kindDevice, "many_to_many", "electrical", planes("energy")),
		entry(pack, dimensionDevice, "dimension", "member", planes("physical"), document{"role": "resource", "resource_kind": kindDevice}),
		fact(factPower, planes("physical")),
		fact(factFault, planes("diagnostic")),
		entry(pack, string(serviceDevice.ID), "service", "member", planes("physical"), document{"fact_key_dimension": deviceDimension, "subject_kind": kindDevice}),
		entry(pack, string(capabilityDevice.ID), "capability", "member", planes("physical"), document{"service": elementRef(pack, string(serviceDevice.ID))}),
	}
	sortElements(doc)
	catalog, err := graph.Decode(encode(t, doc))
	if err != nil {
		t.Fatalf("Decode(lens catalog) = %v", err)
	}
	return catalog
}

func lensContext() semreg.EvaluationContext {
	return semreg.EvaluationContext{
		EvaluatedAt:       semreg.TimePoint{UnixNanoseconds: "130", ClockID: "clock.utc", UncertaintyNS: "0"},
		EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:cut", Nanoseconds: "20"},
	}
}

func lensStore(t *testing.T, catalog *graph.Catalog, cut *graph.Cut, snaps []semreg.Snapshot) *graph.CutStore {
	t.Helper()
	store := graph.NewCutStore(func() time.Time { return time.Unix(1_700_000_000, 0).UTC() })
	if err := store.Retain(cut, snaps, catalog); err != nil {
		t.Fatal(err)
	}
	return store
}

func compositionRequest(snaps []semreg.Snapshot) graph.LensRequest {
	return graph.LensRequest{
		Lens: "lens.equipment_composition", Entry: graph.EntryPoint{Kind: graph.EntryLensRoots},
		Direction: graph.DirectionBoth, Depth: 8, PageSize: 100, Members: snaps,
	}
}

func resourceIDsOf(result graph.LensResult) []string {
	out := make([]string, len(result.Resources))
	for i, resource := range result.Resources {
		out[i] = string(resource.Resource.ResourceID)
	}
	return out
}

func TestLensResult(t *testing.T) {
	catalog := lensCatalog(t)
	ctx := lensContext()
	equip := device("resource:equip", "equip")
	partA := part("resource:part-a", "resource:equip", "a")
	partB := part("resource:part-b", "resource:equip", "b")
	partC := part("resource:part-c", "resource:part-a", "c")

	t.Run("content", func(t *testing.T) {
		plant := cutAsset{
			id:        "asset:plant",
			resources: []semreg.Resource{equip, partA, partB},
			relations: []semreg.RelationInstance{
				qualified(relDevicePart, "resource:equip", "resource:part-a"),
				qualified(relDevicePart, "resource:equip", "resource:part-b"),
			},
			absences: []semreg.RelationAbsence{
				absence(relDevicePart, "resource:part-a", semreg.AbsenceFrom),
				absence(relSupplies, "resource:equip", semreg.AbsenceFrom),
			},
			facts:    []semreg.FactKey{deviceFact(factFault, "resource:equip"), deviceFact(factPower, "resource:equip")},
			services: []string{"resource:equip"},
		}
		cut, snaps := mustCut(t, catalog, graph.CutComplete, plant)
		result, err := graph.RetrieveLens(lensStore(t, catalog, cut, snaps), catalog, compositionRequest(snaps), ctx)
		if err != nil {
			t.Fatal(err)
		}
		if result.Contract != graph.LensResultContract || result.Lens != "lens.equipment_composition" || result.Catalog != catalog.Digest() {
			t.Fatalf("header = %s %s %s", result.Contract, result.Lens, result.Catalog)
		}
		if result.Cut.CutID != cut.ID() || result.Cut.Completeness != graph.CutComplete || len(result.Cut.Members) != 1 {
			t.Fatalf("cut = %+v", result.Cut)
		}
		if got := resourceIDsOf(result); !sameStrings(got, []string{"resource:equip", "resource:part-a", "resource:part-b"}) {
			t.Fatalf("resources = %v", got)
		}
		if result.Resources[0].Depth != 0 || result.Resources[1].Depth != 1 || result.Resources[2].Depth != 1 {
			t.Fatalf("depths = %d %d %d", result.Resources[0].Depth, result.Resources[1].Depth, result.Resources[2].Depth)
		}
		if result.Resources[0].AssetID != "asset:plant" || len(result.Resources[0].Planes) == 0 || result.Resources[0].Planes[0] != graph.PlanePhysical {
			t.Fatalf("equip planes = %+v asset %s", result.Resources[0].Planes, result.Resources[0].AssetID)
		}
		if len(result.Relations) != 2 || !result.Relations[0].InPlane || result.Relations[0].Coherence != graph.CoherenceCoherent {
			t.Fatalf("relations = %+v", result.Relations)
		}
		if result.Relations[0].To != "resource:part-a" || result.Relations[1].To != "resource:part-b" {
			t.Fatalf("relation order = %s %s", result.Relations[0].To, result.Relations[1].To)
		}
		if len(result.Absences) != 1 || result.Absences[0].Definition != relDevicePart {
			t.Fatalf("absences = %+v", result.Absences)
		}
		if len(result.Facts) != 1 || result.Facts[0].Key.FactID != factPower {
			t.Fatalf("facts = %+v", result.Facts)
		}
		view, err := semreg.EvaluateSnapshot(snaps[0], ctx)
		if err != nil {
			t.Fatal(err)
		}
		var want semreg.EvaluatedFact
		for _, fact := range view.Facts {
			if fact.CandidateID == result.Facts[0].Evaluation[0].CandidateID {
				want = fact
			}
		}
		if len(result.Facts[0].Evaluation) != 1 || result.Facts[0].Evaluation[0] != want {
			t.Fatalf("evaluation = %+v, want %+v", result.Facts[0].Evaluation, want)
		}
		left, err := semreg.CanonicalJSON(result.Facts[0].Candidates[0])
		if err != nil {
			t.Fatal(err)
		}
		var snapshotCandidate semreg.FactCandidate
		for _, envelope := range snaps[0].Facts {
			if envelope.Key.FactID == factPower {
				snapshotCandidate = envelope.Candidates[0]
			}
		}
		right, err := semreg.CanonicalJSON(snapshotCandidate)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(left, right) {
			t.Fatal("fact candidate bytes differ from the snapshot")
		}
		if len(result.Services) != 1 || result.Services[0].InstanceID != "service:resource:equip" {
			t.Fatalf("services = %+v", result.Services)
		}
		if len(result.Capabilities) != 1 || result.Capabilities[0].Definition != capabilityDevice {
			t.Fatalf("capabilities = %+v", result.Capabilities)
		}
		if result.NextPage != nil {
			t.Fatalf("next page = %q", *result.NextPage)
		}
		again, err := graph.RetrieveLens(lensStore(t, catalog, cut, snaps), catalog, compositionRequest(snaps), ctx)
		if err != nil {
			t.Fatal(err)
		}
		first, err := result.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		second, err := again.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatal("lens result bytes differ")
		}
	})

	t.Run("non-member relations are shown and not followed", func(t *testing.T) {
		plant := cutAsset{
			id:        "asset:plant",
			resources: []semreg.Resource{equip, partA, partB},
			relations: []semreg.RelationInstance{
				qualified(relDevicePart, "resource:equip", "resource:part-a"),
				relation(relDevicePart, "resource:equip", "resource:outside", semreg.LinkCandidate),
				relation(relDevicePart, "resource:part-a", "resource:part-b", semreg.LinkConflict),
			},
		}
		cut, snaps := mustCut(t, catalog, graph.CutComplete, plant)
		result, err := graph.RetrieveLens(lensStore(t, catalog, cut, snaps), catalog, compositionRequest(snaps), ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got := resourceIDsOf(result); !sameStrings(got, []string{"resource:equip", "resource:part-a"}) {
			t.Fatalf("resources = %v", got)
		}
		var candidate, conflict bool
		for _, item := range result.Relations {
			if item.InPlane {
				continue
			}
			if item.To == "resource:outside" && item.State == semreg.LinkCandidate {
				candidate = true
			}
			if item.State == semreg.LinkConflict {
				conflict = true
			}
		}
		if !candidate || !conflict {
			t.Fatalf("out-of-plane relations = %+v", result.Relations)
		}
	})

	t.Run("unknown is not none", func(t *testing.T) {
		plant := cutAsset{id: "asset:plant", resources: []semreg.Resource{equip}}
		cut, snaps := mustCut(t, catalog, graph.CutComplete, plant)
		result, err := graph.RetrieveLens(lensStore(t, catalog, cut, snaps), catalog, compositionRequest(snaps), ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Resources) != 1 || len(result.Relations) != 0 || len(result.Absences) != 0 {
			t.Fatalf("resources %d relations %d absences %d", len(result.Resources), len(result.Relations), len(result.Absences))
		}
	})

	t.Run("findings that name a reached resource", func(t *testing.T) {
		left := cutAsset{id: "asset:left", resources: []semreg.Resource{device("resource:left", "same")}}
		right := cutAsset{id: "asset:right", resources: []semreg.Resource{device("resource:right", "same")}}
		cut, snaps := mustCut(t, catalog, graph.CutComplete, left, right)
		result, err := graph.RetrieveLens(lensStore(t, catalog, cut, snaps), catalog, compositionRequest(snaps), ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Findings) != 1 || result.Findings[0].Kind != graph.CutSameUnit {
			t.Fatalf("findings = %+v", result.Findings)
		}
		if len(result.Findings[0].Resources) != 2 {
			t.Fatalf("same_unit resources = %+v", result.Findings[0].Resources)
		}
	})

	t.Run("visibility is not authority", func(t *testing.T) {
		plant := cutAsset{id: "asset:plant", resources: []semreg.Resource{equip}, services: []string{"resource:equip"}}
		cut, snaps := mustCut(t, catalog, graph.CutComplete, plant)
		result, err := graph.RetrieveLens(lensStore(t, catalog, cut, snaps), catalog, compositionRequest(snaps), ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Capabilities) != 1 {
			t.Fatalf("capabilities = %+v", result.Capabilities)
		}
		root, err := filepath.Abs(filepath.Join("..", "..", ".."))
		if err != nil {
			t.Fatal(err)
		}
		pkg, err := build.Default.ImportDir(filepath.Join(root, "semreg", "v1", "operation"), 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range pkg.Imports {
			if imported == "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph" || strings.Contains(imported, "/semreg/v1/graph/") {
				t.Fatalf("operation imports %s", imported)
			}
		}
	})

	t.Run("absent roots are an empty result", func(t *testing.T) {
		site := slotResource("resource:site", "resource.site", semreg.AnchorFunction, "", "site")
		cut, snaps := mustCut(t, catalog, graph.CutComplete, cutAsset{id: "asset:site", resources: []semreg.Resource{site}})
		result, err := graph.RetrieveLens(lensStore(t, catalog, cut, snaps), catalog, compositionRequest(snaps), ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Resources) != 0 || result.Cut.CutID != cut.ID() || result.EntryStatus != graph.EntryStatusNoRoot {
			t.Fatalf("empty roots = %+v status %q", result.Cut, result.EntryStatus)
		}
		sites := compositionRequest(snaps)
		sites.Entry = graph.EntryPoint{Kind: graph.EntrySites}
		onlyDevice, deviceSnaps := mustCut(t, catalog, graph.CutComplete, cutAsset{id: "asset:plant", resources: []semreg.Resource{equip}})
		// The sites request still carries the plant snapshots, which do not match this cut.
		if _, err = graph.RetrieveLens(lensStore(t, catalog, onlyDevice, deviceSnaps), catalog, sites, ctx); err == nil {
			t.Fatal("member snapshots that are not the cut were read")
		}
		sites.Members = deviceSnaps
		result, err = graph.RetrieveLens(lensStore(t, catalog, onlyDevice, deviceSnaps), catalog, sites, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Resources) != 0 || result.Cut.CutID != onlyDevice.ID() || result.EntryStatus != graph.EntryStatusNoSite {
			t.Fatalf("empty sites = status %q resources %d", result.EntryStatus, len(result.Resources))
		}
	})

	t.Run("absences follow the absence key", func(t *testing.T) {
		// One definition, interleaved sides and resources: equip/to must
		// precede part-b/from. Those ends are free of the contains instances.
		// A later definition on the earlier resource stays after both.
		plant := cutAsset{
			id:        "asset:plant",
			resources: []semreg.Resource{equip, partA, partB},
			relations: []semreg.RelationInstance{
				qualified(relDevicePart, "resource:equip", "resource:part-a"),
				qualified(relDevicePart, "resource:equip", "resource:part-b"),
			},
			absences: []semreg.RelationAbsence{
				absence(cutRef("cut.relation.part_nest"), "resource:equip", semreg.AbsenceFrom),
				absence(relDevicePart, "resource:part-b", semreg.AbsenceFrom),
				absence(relDevicePart, "resource:equip", semreg.AbsenceTo),
			},
		}
		cut, snaps := mustCut(t, catalog, graph.CutComplete, plant)
		result, err := graph.RetrieveLens(lensStore(t, catalog, cut, snaps), catalog, compositionRequest(snaps), ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Absences) != 3 {
			t.Fatalf("absences = %+v", result.Absences)
		}
		want := []struct {
			definition semreg.DefinitionRef
			resource   semreg.ResourceID
			side       semreg.AbsenceSide
		}{
			{relDevicePart, "resource:equip", semreg.AbsenceTo},
			{relDevicePart, "resource:part-b", semreg.AbsenceFrom},
			{cutRef("cut.relation.part_nest"), "resource:equip", semreg.AbsenceFrom},
		}
		for i, absence := range want {
			got := result.Absences[i]
			if got.Definition != absence.definition || got.Resource != absence.resource || got.Side != absence.side {
				t.Fatalf("absence %d = %s %s %s", i, got.Definition.ID, got.Resource, got.Side)
			}
		}
	})

	t.Run("frontier", func(t *testing.T) {
		plant := cutAsset{
			id:        "asset:plant",
			resources: []semreg.Resource{equip, partA, partC},
			relations: []semreg.RelationInstance{
				qualified(relDevicePart, "resource:equip", "resource:part-a"),
				qualified(cutRef("cut.relation.part_nest"), "resource:part-a", "resource:part-c"),
			},
		}
		cut, snaps := mustCut(t, catalog, graph.CutComplete, plant)
		request := compositionRequest(snaps)
		request.Depth = 1
		result, err := graph.RetrieveLens(lensStore(t, catalog, cut, snaps), catalog, request, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got := resourceIDsOf(result); !sameStrings(got, []string{"resource:equip", "resource:part-a"}) {
			t.Fatalf("resources = %v", got)
		}
		if len(result.Frontier) != 1 || result.Frontier[0].ResourceID != "resource:part-a" || result.Frontier[0].Reason != graph.FrontierDepthLimit || result.Frontier[0].NotFollowed < 1 {
			t.Fatalf("frontier = %+v", result.Frontier)
		}
	})
}

// TestLensResultTokenFields varies every field of the lens page identity and
// the cut identifier carried beside it.
// lens: another lens that the catalog does declare, so a read that ignored
// the lens would still accept the token. entry kind: sites instead of lens
// roots. entry resource: the same resource entry with another current
// resource, so a read that ignored the resource would still accept the
// token. direction, depth and page size: another admitted value. context
// wall, clock, uncertainty, epoch and ticks: one clock field each. not-included
// asset and not-included reason: one row, then that row's reason. catalog:
// another catalog digest is not the cut's catalog. cut identifier: the token
// of one retained cut read against another cut's members. Each of these is
// revision_conflict. A read that dropped the field would return the page.
func TestLensResultTokenFields(t *testing.T) {
	catalog := lensCatalog(t)
	ctx := lensContext()
	plant := cutAsset{
		id: "asset:plant",
		resources: []semreg.Resource{
			device("resource:equip", "equip"),
			part("resource:part-a", "resource:equip", "a"),
			part("resource:part-b", "resource:equip", "b"),
		},
		relations: []semreg.RelationInstance{
			qualified(relDevicePart, "resource:equip", "resource:part-a"),
			qualified(relDevicePart, "resource:equip", "resource:part-b"),
		},
	}
	cut, snaps := mustCut(t, catalog, graph.CutComplete, plant)
	store := lensStore(t, catalog, cut, snaps)
	request := compositionRequest(snaps)
	request.PageSize = 1
	first, err := graph.RetrieveLens(store, catalog, request, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.NextPage == nil || len(first.Resources) != 1 {
		t.Fatalf("first page resources %d token %v", len(first.Resources), first.NextPage)
	}
	request.PageToken = *first.NextPage
	second, err := graph.RetrieveLens(store, catalog, request, ctx)
	if err != nil || second.NextPage == nil || len(second.Resources) != 1 {
		t.Fatalf("second page = %v resources %d err %v", second.NextPage, len(second.Resources), err)
	}
	edits := []struct {
		name string
		edit func(*graph.LensRequest, *semreg.EvaluationContext)
	}{
		{"lens", func(r *graph.LensRequest, _ *semreg.EvaluationContext) { r.Lens = "lens.part_composition" }},
		{"entry kind", func(r *graph.LensRequest, _ *semreg.EvaluationContext) {
			r.Entry = graph.EntryPoint{Kind: graph.EntrySites}
		}},
		{"direction", func(r *graph.LensRequest, _ *semreg.EvaluationContext) { r.Direction = graph.DirectionOutgoing }},
		{"depth", func(r *graph.LensRequest, _ *semreg.EvaluationContext) { r.Depth = 2 }},
		{"page size", func(r *graph.LensRequest, _ *semreg.EvaluationContext) { r.PageSize = 2 }},
		{"context wall", func(_ *graph.LensRequest, c *semreg.EvaluationContext) { c.EvaluatedAt.UnixNanoseconds = "140" }},
		{"context clock", func(_ *graph.LensRequest, c *semreg.EvaluationContext) { c.EvaluatedAt.ClockID = "clock.other" }},
		{"context uncertainty", func(_ *graph.LensRequest, c *semreg.EvaluationContext) { c.EvaluatedAt.UncertaintyNS = "1" }},
		{"context epoch", func(_ *graph.LensRequest, c *semreg.EvaluationContext) {
			c.EvaluateMonotonic.ClockEpochID = "clock:other"
		}},
		{"context ticks", func(_ *graph.LensRequest, c *semreg.EvaluationContext) { c.EvaluateMonotonic.Nanoseconds = "21" }},
		{"not-included asset", func(r *graph.LensRequest, _ *semreg.EvaluationContext) {
			r.NotIncluded = []graph.NotIncludedAsset{{AssetID: "asset:missing", Reason: graph.NotIncludedExcluded}}
		}},
		{"not-included reason", func(r *graph.LensRequest, _ *semreg.EvaluationContext) {
			r.NotIncluded = []graph.NotIncludedAsset{{AssetID: "asset:missing", Reason: graph.NotIncludedPublicationWithheld}}
		}},
	}
	for _, edit := range edits {
		t.Run(edit.name, func(t *testing.T) {
			next := compositionRequest(snaps)
			next.PageSize = 1
			next.PageToken = *first.NextPage
			context := ctx
			edit.edit(&next, &context)
			if edit.name == "not-included reason" {
				next.NotIncluded = []graph.NotIncludedAsset{{AssetID: "asset:missing", Reason: graph.NotIncludedExcluded}}
				next.PageToken = ""
				if _, err := graph.RetrieveLens(store, catalog, next, context); err != nil {
					t.Fatal(err)
				}
				next.NotIncluded[0].Reason = graph.NotIncludedPublicationWithheld
				next.PageToken = *first.NextPage
			}
			_, err := graph.RetrieveLens(store, catalog, next, context)
			requireReadError(t, err, semreg.RevisionConflict)
		})
	}

	t.Run("entry resource", func(t *testing.T) {
		rooted := compositionRequest(snaps)
		rooted.Entry = graph.EntryPoint{Kind: graph.EntryResource, Resource: "resource:equip"}
		rooted.PageSize = 1
		page, err := graph.RetrieveLens(store, catalog, rooted, ctx)
		if err != nil || page.NextPage == nil {
			t.Fatalf("resource page: %v token %v", err, page.NextPage)
		}
		rooted.PageToken = *page.NextPage
		rooted.Entry.Resource = "resource:part-a"
		_, err = graph.RetrieveLens(store, catalog, rooted, ctx)
		requireReadError(t, err, semreg.RevisionConflict)
	})

	t.Run("catalog", func(t *testing.T) {
		other := decodeLensCatalog(t, "Which equipment is installed on this site?")
		next := compositionRequest(snaps)
		next.PageSize = 1
		next.PageToken = *first.NextPage
		_, err := graph.RetrieveLens(store, other, next, ctx)
		requireReadError(t, err, semreg.RevisionConflict)
	})

	t.Run("cut identifier", func(t *testing.T) {
		otherPlant := plant
		otherPlant.id = "asset:other"
		otherCut, otherSnaps := mustCut(t, catalog, graph.CutComplete, otherPlant)
		if err := store.Retain(otherCut, otherSnaps, catalog); err != nil {
			t.Fatal(err)
		}
		next := compositionRequest(otherSnaps)
		next.PageSize = 1
		next.CutID = idPtr(otherCut.ID())
		next.PageToken = *first.NextPage
		_, err := graph.RetrieveLens(store, catalog, next, ctx)
		requireReadError(t, err, semreg.RevisionConflict)
	})
}
