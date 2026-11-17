package graph_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// ===================================================== construct_registry

type registryResult struct {
	registry   *semreg.Registry
	validators []semreg.PackValidator
}

func runConstructRegistry(c *vectorCase) {
	c.t.Helper()
	if c.state.Registry != nil || len(c.state.Batches) != 0 {
		c.t.Fatalf("prior state %s must hold no registry and no batch", c.state.Name)
	}
	var input gvRegistry
	if err := gvStrict(c.vector.Input, &input); err != nil {
		c.t.Fatalf("decode input: %v", err)
	}
	validators := gvValidators(input.Packs)
	construct := func(validators []semreg.PackValidator) (*semreg.Registry, error) {
		switch input.ElementCatalog {
		case "registered":
			return semreg.NewGraphRegistry(c.catalogFor(&input), validators...)
		case "absent":
			return semreg.NewRegistry(validators...)
		}
		c.t.Fatalf("element_catalog %q", input.ElementCatalog)
		return nil, nil
	}
	registry, err := construct(validators)
	if !c.verdict(err) {
		if registry != nil {
			c.t.Fatal("a refused construction returned a registry")
		}
		if len(c.vector.UncoveredPacks) != 0 {
			// The refusal is caused by the uncovered packs: the same
			// construction without them is accepted.
			covered := []semreg.PackValidator{}
			for _, validator := range validators {
				uncovered := false
				for _, pack := range c.vector.UncoveredPacks {
					uncovered = uncovered || validator.Pack() == pack
				}
				if !uncovered {
					covered = append(covered, validator)
				}
			}
			if len(covered) == len(validators) {
				c.t.Fatal("no registered pack is one of the uncovered packs")
			}
			if _, err := construct(covered); err != nil {
				c.t.Fatalf("construction without the uncovered packs = %v", err)
			}
		}
		return
	}
	gvAssert(c, registryAssertions, registryResult{registry: registry, validators: validators})
}

var registryAssertions = map[string]func(*vectorCase, registryResult) error{
	"registry_graph_enforced": func(_ *vectorCase, r registryResult) error {
		if !r.registry.GraphEnforced() {
			return fmt.Errorf("registry is not graph-enforced")
		}
		return nil
	},
	"catalog_digest_exposed": func(c *vectorCase, r registryResult) error {
		digest, ok := r.registry.CatalogDigest()
		if !ok || digest != graph.PinnedDigest || digest != c.doc.ElementCatalog.SHA256 {
			return fmt.Errorf("CatalogDigest() = %s, %v", digest, ok)
		}
		return nil
	},
	"every_registered_pack_covered": func(c *vectorCase, r registryResult) error {
		catalog := c.registeredCatalog()
		for _, validator := range r.validators {
			if err := catalog.Covers(validator.Definitions()); err != nil {
				return fmt.Errorf("pack %s: %v", validator.Pack().ID, err)
			}
			if _, err := r.registry.Validator(validator.Pack()); err != nil {
				return fmt.Errorf("pack %s is not registered: %v", validator.Pack().ID, err)
			}
		}
		return nil
	},
	"non_member_operations_registered_inadmissible": func(c *vectorCase, r registryResult) error {
		catalog := c.registeredCatalog()
		want := map[semreg.DefinitionRef]bool{}
		for _, ref := range c.expect.InadmissibleOperations {
			want[ref.packRef(c.t)] = true
		}
		got := map[semreg.DefinitionRef]bool{}
		operations := 0
		for _, validator := range r.validators {
			for _, op := range validator.Definitions().Operations {
				operations++
				err := r.registry.GraphAdmissionError(op, semreg.DefinitionRef{})
				membership, ok := catalog.Membership(op)
				member := ok && membership.Disposition == semreg.GraphMember
				switch {
				case err == nil && !member:
					return fmt.Errorf("operation %s is not a member but is admissible", op.ID)
				case err != nil && member:
					return fmt.Errorf("member operation %s is inadmissible: %v", op.ID, err)
				case err != nil && semreg.ErrorIdentifier(err) != semreg.MembershipMissing:
					return fmt.Errorf("operation %s: %v", op.ID, err)
				case err != nil:
					got[op] = true
				}
			}
		}
		// Every operation is checked against its catalog membership above;
		// the vector's list may be empty when every operation is a member.
		if operations == 0 || !gvDeepEqual(got, want) {
			return fmt.Errorf("inadmissible operations %v, the vector lists %v (of %d operations)", got, want, operations)
		}
		return nil
	},
}

// ======================================================= register_catalog

type catalogResult struct {
	catalog *graph.Catalog
	raw     []byte
}

func runRegisterCatalog(c *vectorCase) {
	c.t.Helper()
	if c.state.Registry != nil || len(c.state.Batches) != 0 {
		c.t.Fatalf("prior state %s must hold no registry and no batch", c.state.Name)
	}
	var input struct {
		Catalog   string                       `json:"catalog"`
		Mutations []map[string]json.RawMessage `json:"mutations"`
	}
	if err := gvStrict(c.vector.Input, &input); err != nil {
		c.t.Fatalf("decode input: %v", err)
	}
	if input.Catalog != "element_catalog" {
		c.t.Fatalf("catalog %q", input.Catalog)
	}
	committed, err := os.ReadFile(gvCatalogPath)
	if err != nil {
		c.t.Fatal(err)
	}
	raw := committed
	if len(input.Mutations) != 0 {
		doc := gvObject(c.t, committed)
		for _, mutation := range input.Mutations {
			gvMutateCatalog(c, doc, mutation)
		}
		raw = gvMarshal(c.t, doc)
		if _, err := graph.Load(raw); err == nil {
			c.t.Fatal("Load accepted a mutated catalog")
		}
	}
	catalog, err := graph.Decode(raw)
	if err == nil {
		err = graph.Validate(catalog, graph.AcceptedPackMetadata())
	}
	accepted := c.verdict(err)
	after, readErr := os.ReadFile(gvCatalogPath)
	if readErr != nil || !bytes.Equal(after, committed) || semreg.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(after))) != graph.PinnedDigest {
		c.t.Fatal("the committed catalog changed")
	}
	if gvEmbedded(c.t).Digest() != graph.PinnedDigest {
		c.t.Fatal("the embedded catalog is not the pinned copy")
	}
	if accepted {
		gvAssert(c, catalogAssertions, catalogResult{catalog: catalog, raw: raw})
	}
}

// gvMutateCatalog applies one mutation of a register_catalog vector to the
// decoded catalog document. Each mutation names exactly one entry.
func gvMutateCatalog(c *vectorCase, doc map[string]any, mutation map[string]json.RawMessage) {
	c.t.Helper()
	if len(mutation) != 1 {
		c.t.Fatalf("mutation %v must have one member", gvSortedKeys(mutation))
	}
	elements, _ := doc["elements"].([]any)
	find := func(ref json.RawMessage) int {
		found := -1
		for i, item := range elements {
			entry, _ := item.(map[string]any)
			if gvSameJSON(c.t, entry["ref"], ref) {
				if found >= 0 {
					c.t.Fatalf("mutation ref %s names two entries", ref)
				}
				found = i
			}
		}
		if found < 0 {
			c.t.Fatalf("mutation ref %s names no entry", ref)
		}
		return found
	}
	for kind, body := range mutation {
		switch kind {
		case "remove_element":
			i := find(body)
			doc["elements"] = append(append([]any{}, elements[:i]...), elements[i+1:]...)
		case "set_planes":
			var change struct {
				Ref    json.RawMessage `json:"ref"`
				Planes []string        `json:"planes"`
			}
			if err := gvStrict(body, &change); err != nil {
				c.t.Fatalf("set_planes: %v", err)
			}
			planes := make([]any, len(change.Planes))
			for i, plane := range change.Planes {
				planes[i] = plane
			}
			elements[find(change.Ref)].(map[string]any)["planes"] = planes
		case "set_disposition":
			var change struct {
				Ref         json.RawMessage `json:"ref"`
				Disposition string          `json:"disposition"`
				Reason      string          `json:"reason"`
				Tracking    string          `json:"tracking"`
			}
			if err := gvStrict(body, &change); err != nil {
				c.t.Fatalf("set_disposition: %v", err)
			}
			entry := elements[find(change.Ref)].(map[string]any)
			if change.Disposition != "unknown" || entry["disposition"] != "member" {
				c.t.Fatalf("set_disposition %s of a %v entry", change.Disposition, entry["disposition"])
			}
			entry["disposition"], entry["planes"], entry["reason"], entry["tracking"] = change.Disposition, []any{}, change.Reason, change.Tracking
		default:
			c.t.Fatalf("mutation %q is not implemented", kind)
		}
	}
}

var catalogAssertions = map[string]func(*vectorCase, catalogResult) error{
	"seed_catalog_registered": func(_ *vectorCase, r catalogResult) error {
		loaded, err := graph.Load(r.raw)
		if err != nil {
			return fmt.Errorf("Load = %v", err)
		}
		registry, err := semreg.NewGraphRegistry(loaded, gvAcceptedValidators()...)
		if err != nil || !registry.GraphEnforced() {
			return fmt.Errorf("NewGraphRegistry = %v", err)
		}
		return nil
	},
	"cat_1_to_cat_9_hold": func(_ *vectorCase, r catalogResult) error {
		findings, err := graph.Check(r.catalog, graph.AcceptedPackMetadata())
		if err != nil || len(findings) != 0 {
			return fmt.Errorf("Check = %v, %v", findings, err)
		}
		return nil
	},
	"digest_equals_pinned_digest": func(c *vectorCase, r catalogResult) error {
		if r.catalog.Digest() != graph.PinnedDigest || r.catalog.Digest() != c.doc.ElementCatalog.SHA256 {
			return fmt.Errorf("digest %s", r.catalog.Digest())
		}
		return nil
	},
	"element_member_of_several_planes": func(c *vectorCase, r catalogResult) error {
		if len(c.expect.MultipleMembership) == 0 {
			return fmt.Errorf("no element listed")
		}
		for _, ref := range c.expect.MultipleMembership {
			element := ref.element(c.t)
			disposition, _ := r.catalog.DispositionOf(element)
			planes, _ := r.catalog.PlanesOf(element)
			if disposition != graph.DispositionMember || len(planes) < 2 {
				return fmt.Errorf("%s is %s in %v", element, disposition, planes)
			}
		}
		return nil
	},
	"no_primary_plane": func(c *vectorCase, r catalogResult) error {
		// Membership is a set: every entry lists its planes in the one
		// catalog plane order, so no entry puts a plane of its own first.
		order := map[graph.PlaneID]int{}
		for i, plane := range r.catalog.Planes() {
			order[plane.ID] = i
		}
		for _, ref := range c.expect.MultipleMembership {
			planes, _ := r.catalog.PlanesOf(ref.element(c.t))
			for i := range planes {
				position, ok := order[planes[i]]
				if !ok || i > 0 && position <= order[planes[i-1]] {
					return fmt.Errorf("%s planes %v are not in catalog plane order", ref.element(c.t), planes)
				}
			}
		}
		return nil
	},
}

// ================================================================== apply

type applyResult struct {
	before semreg.Snapshot
	after  semreg.Snapshot
	batch  semreg.PublicationBatch
}

func runApply(c *vectorCase) {
	c.t.Helper()
	input := gvObject(c.t, c.vector.Input)
	asset, _ := input["asset_id"].(string)
	p := c.publisher(semreg.AssetID(asset), nil)
	before, beforeBytes, had := p.current()
	after, err := p.apply(c.vector.Input, nil)
	c.checkRecord("input batch", c.vector.Input, err, false)
	if !c.verdict(err) {
		_, nowBytes, has := p.current()
		if has != had || !bytes.Equal(nowBytes, beforeBytes) {
			c.t.Fatal("a rejected batch changed the published state")
		}
		return
	}
	result := applyResult{before: before, after: after, batch: p.last}
	for _, want := range c.expect.Resources {
		resource, ok := gvResource(after, want.ResourceID)
		if !ok || resource.Revision != want.Revision {
			c.t.Errorf("resource %s = %+v, want revision %s", want.ResourceID, resource, want.Revision)
		}
	}
	for _, want := range c.expect.RelationStates {
		relation, ok := gvRelationByJSON(c, after, want)
		if !ok || relation.State != want.State {
			c.t.Errorf("relation %s %s -> %s = %+v, want state %s", want.Definition, want.From, want.To, relation, want.State)
		}
	}
	gvAssert(c, applyAssertions, result)
}

func gvResource(snapshot semreg.Snapshot, id semreg.ResourceID) (semreg.Resource, bool) {
	for _, resource := range snapshot.Resources {
		if resource.ResourceID == id {
			return resource, true
		}
	}
	return semreg.Resource{}, false
}

func gvCurrentResource(snapshot semreg.Snapshot, id semreg.ResourceID) bool {
	resource, ok := gvResource(snapshot, id)
	return ok && resource.State == semreg.ResourceCurrent
}

func gvRelationByJSON(c *vectorCase, snapshot semreg.Snapshot, want gvRelationState) (semreg.RelationInstance, bool) {
	for _, relation := range snapshot.Relations {
		if relation.From == want.From && relation.To == want.To && gvSameJSON(c.t, relation.Definition, want.Definition) {
			return relation, true
		}
	}
	return semreg.RelationInstance{}, false
}

func gvRelationKey(relation semreg.RelationInstance) semreg.RelationKey {
	return semreg.RelationKey{Definition: relation.Definition, From: relation.From, To: relation.To}
}

func gvHasRelation(snapshot semreg.Snapshot, key semreg.RelationKey) bool {
	for _, relation := range snapshot.Relations {
		if gvRelationKey(relation) == key {
			return true
		}
	}
	return false
}

func gvNativeBasis(class semreg.RelationBasis) bool {
	return class == semreg.RelationNativeObservation || class == semreg.RelationGateRule
}

// gvCoreType is the core type of a relation definition in the catalog.
func gvCoreType(c *vectorCase, ref semreg.DefinitionRef) string {
	definition, ok := c.registeredCatalog().RelationDefinition(ref)
	if !ok {
		return ""
	}
	return definition.CoreType
}

// gvFenced reports whether the batch fences the generation of the binding an
// origin names.
func gvFenced(before semreg.Snapshot, batch semreg.PublicationBatch, origin semreg.OriginRef) bool {
	if origin.BindingID == nil {
		return false
	}
	for _, binding := range before.Bindings {
		if binding.BindingID != *origin.BindingID {
			continue
		}
		for _, fence := range batch.GenerationFences {
			if fence.SourceID == binding.SourceID && fence.SourceEpochID == binding.SourceEpochID && fence.DriverGeneration == binding.DriverGeneration {
				return true
			}
		}
	}
	return false
}

func gvRemovedNative(r applyResult, ended func(semreg.OriginRef) bool) error {
	removed := 0
	for _, relation := range r.before.Relations {
		if !gvNativeBasis(relation.BasisClass) || !ended(relation.Origin) {
			continue
		}
		removed++
		if gvHasRelation(r.after, gvRelationKey(relation)) {
			return fmt.Errorf("native instance %s -> %s survived", relation.From, relation.To)
		}
	}
	for _, relation := range r.after.Relations {
		if gvNativeBasis(relation.BasisClass) && ended(relation.Origin) {
			return fmt.Errorf("native instance %s -> %s of an ended binding is current", relation.From, relation.To)
		}
	}
	if removed == 0 {
		return fmt.Errorf("the prior state holds no native instance of an ended binding")
	}
	return nil
}

func gvKeptExcept(r applyResult, native bool) error {
	kept := 0
	for _, relation := range r.before.Relations {
		if gvNativeBasis(relation.BasisClass) == native {
			continue
		}
		kept++
		found := false
		for _, now := range r.after.Relations {
			found = found || gvDeepEqual(now, relation)
		}
		if !found {
			return fmt.Errorf("instance %s -> %s was not kept unchanged", relation.From, relation.To)
		}
	}
	if kept == 0 {
		return fmt.Errorf("the prior state holds no such instance")
	}
	return nil
}

// gvUpsertedResources are the resources the batch upserts, as published.
func gvUpsertedResources(r applyResult) ([]semreg.Resource, error) {
	out := []semreg.Resource{}
	for _, upsert := range r.batch.ResourceUpserts {
		resource, ok := gvResource(r.after, upsert.ResourceID)
		if !ok {
			return nil, fmt.Errorf("upserted resource %s is not published", upsert.ResourceID)
		}
		out = append(out, resource)
	}
	return out, nil
}

// gvLinkedResources are the resources the batch's identity links name.
func gvLinkedResources(r applyResult) map[semreg.ResourceID]struct{} {
	out := map[semreg.ResourceID]struct{}{}
	for _, link := range r.batch.IdentityLinkUpserts {
		if link.Resource != nil {
			out[*link.Resource] = struct{}{}
		}
	}
	return out
}

var applyAssertions = map[string]func(*vectorCase, applyResult) error{
	"resource_records_unchanged": func(_ *vectorCase, r applyResult) error {
		if len(r.before.Resources) == 0 || !gvDeepEqual(r.before.Resources, r.after.Resources) {
			return fmt.Errorf("resources %+v became %+v", r.before.Resources, r.after.Resources)
		}
		return nil
	},
	"same_resource_ids": func(_ *vectorCase, r applyResult) error {
		ids := func(s semreg.Snapshot) []semreg.ResourceID {
			out := []semreg.ResourceID{}
			for _, resource := range s.Resources {
				out = append(out, resource.ResourceID)
			}
			return out
		}
		if len(ids(r.before)) == 0 || !gvDeepEqual(ids(r.before), ids(r.after)) {
			return fmt.Errorf("resource ids %v became %v", ids(r.before), ids(r.after))
		}
		return nil
	},
	"native_basis_relations_of_retired_epoch_removed": func(_ *vectorCase, r applyResult) error {
		if len(r.batch.SourceRetirements) == 0 {
			return fmt.Errorf("the batch retires no source epoch")
		}
		return gvRemovedNative(r, func(origin semreg.OriginRef) bool {
			for _, epoch := range r.batch.SourceRetirements {
				if origin.SourceEpochID != nil && *origin.SourceEpochID == epoch {
					return true
				}
			}
			return false
		})
	},
	"same_resource_id": func(_ *vectorCase, r applyResult) error {
		upgraded := 0
		for _, upsert := range r.batch.ResourceUpserts {
			if upsert.Supersedes == nil {
				continue
			}
			upgraded++
			if _, ok := gvResource(r.before, upsert.ResourceID); !ok {
				return fmt.Errorf("%s was not published before", upsert.ResourceID)
			}
			if !gvCurrentResource(r.after, upsert.ResourceID) {
				return fmt.Errorf("%s is not current after", upsert.ResourceID)
			}
		}
		if upgraded == 0 || len(r.before.Resources) != len(r.after.Resources) {
			return fmt.Errorf("%d supersessions; resources %d became %d", upgraded, len(r.before.Resources), len(r.after.Resources))
		}
		return nil
	},
	"anchor_upgraded_to_native_identity": func(c *vectorCase, r applyResult) error {
		keys := map[semreg.Digest]bool{}
		for _, anchor := range c.vector.AnchorKeys {
			keys[anchor.Key] = true
		}
		for _, upsert := range r.batch.ResourceUpserts {
			if upsert.Supersedes == nil {
				continue
			}
			prior, _ := gvResource(r.before, upsert.ResourceID)
			now, _ := gvResource(r.after, upsert.ResourceID)
			if prior.Anchor.Basis == semreg.AnchorNativeIdentity || now.Anchor.Basis != semreg.AnchorNativeIdentity ||
				now.Anchor.Key == nil || !keys[*now.Anchor.Key] || now.Anchor.Scheme == nil {
				return fmt.Errorf("%s anchor %+v became %+v", upsert.ResourceID, prior.Anchor, now.Anchor)
			}
		}
		return nil
	},
	"supersedes_recorded": func(_ *vectorCase, r applyResult) error {
		for _, upsert := range r.batch.ResourceUpserts {
			if upsert.Supersedes == nil {
				continue
			}
			prior, _ := gvResource(r.before, upsert.ResourceID)
			now, _ := gvResource(r.after, upsert.ResourceID)
			if now.Supersedes == nil || !gvDeepEqual(*now.Supersedes, *upsert.Supersedes) || !gvDeepEqual(now.Supersedes.Previous, prior.Anchor) {
				return fmt.Errorf("%s supersedes %+v; prior anchor %+v", upsert.ResourceID, now.Supersedes, prior.Anchor)
			}
		}
		return nil
	},
	"two_resources": func(_ *vectorCase, r applyResult) error {
		upserted, err := gvUpsertedResources(r)
		if err != nil {
			return err
		}
		if len(upserted) != 2 || upserted[0].ResourceID == upserted[1].ResourceID {
			return fmt.Errorf("upserted %+v", upserted)
		}
		for _, resource := range upserted {
			if resource.State != semreg.ResourceCurrent {
				return fmt.Errorf("%s is %s", resource.ResourceID, resource.State)
			}
		}
		return nil
	},
	"no_duplicate_identity": func(_ *vectorCase, r applyResult) error {
		seen := map[string]semreg.ResourceID{}
		for _, resource := range r.after.Resources {
			if resource.State != semreg.ResourceCurrent || resource.Anchor.Key == nil {
				continue
			}
			key := string(resource.Kind) + "\x00" + string(*resource.Anchor.Key)
			if other, ok := seen[key]; ok {
				return fmt.Errorf("%s and %s share kind and anchor key", other, resource.ResourceID)
			}
			seen[key] = resource.ResourceID
		}
		return nil
	},
	"keys_differ_by_issuer": func(c *vectorCase, r applyResult) error {
		upserted, err := gvUpsertedResources(r)
		if err != nil {
			return err
		}
		identities := map[semreg.Digest]gvAnchorKey{}
		for _, anchor := range c.vector.AnchorKeys {
			identities[anchor.Key] = anchor
		}
		named := []gvAnchorKey{}
		for _, resource := range upserted {
			if resource.Anchor.Key == nil {
				return fmt.Errorf("%s has no anchor key", resource.ResourceID)
			}
			anchor, ok := identities[*resource.Anchor.Key]
			if !ok {
				return fmt.Errorf("%s key is not a listed anchor key", resource.ResourceID)
			}
			named = append(named, anchor)
		}
		if len(named) != 2 || named[0].Key == named[1].Key || named[0].Identity.Scheme != named[1].Identity.Scheme ||
			named[0].Identity.Token != named[1].Identity.Token || named[0].Identity.Issuer == named[1].Identity.Issuer {
			return fmt.Errorf("identities %+v", named)
		}
		return nil
	},
	"one_resource": func(_ *vectorCase, r applyResult) error {
		linked := gvLinkedResources(r)
		if len(linked) != 1 || len(r.after.Resources) != len(r.before.Resources) {
			return fmt.Errorf("links name %v; resources %d became %d", linked, len(r.before.Resources), len(r.after.Resources))
		}
		for id := range linked {
			if !gvCurrentResource(r.before, id) || !gvCurrentResource(r.after, id) {
				return fmt.Errorf("%s is not one current resource", id)
			}
		}
		return nil
	},
	"two_identity_links_name_the_resource": func(_ *vectorCase, r applyResult) error {
		for id := range gvLinkedResources(r) {
			naming := 0
			for _, link := range r.after.IdentityLinks {
				if link.Resource != nil && *link.Resource == id && link.State == semreg.LinkQualified {
					naming++
				}
			}
			if naming != 2 {
				return fmt.Errorf("%d qualified identity links name %s", naming, id)
			}
			return nil
		}
		return fmt.Errorf("the batch links no resource")
	},
	"equipment_component_function_and_episode_resources_current": func(_ *vectorCase, r applyResult) error {
		classes := map[semreg.AnchorClass]bool{}
		for _, resource := range r.after.Resources {
			if resource.State == semreg.ResourceCurrent {
				classes[resource.Anchor.Class] = true
			}
		}
		for _, class := range []semreg.AnchorClass{semreg.AnchorEquipment, semreg.AnchorComponent, semreg.AnchorFunction, semreg.AnchorEpisode} {
			if !classes[class] {
				return fmt.Errorf("no current %s resource", class)
			}
		}
		return nil
	},
	"episode_hosted_by_connector": func(c *vectorCase, r applyResult) error {
		episodes := 0
		for _, resource := range r.after.Resources {
			if resource.Anchor.Class != semreg.AnchorEpisode {
				continue
			}
			episodes++
			if resource.Anchor.Parent == nil {
				return fmt.Errorf("episode %s has no parent", resource.ResourceID)
			}
			parent, ok := gvResource(r.after, *resource.Anchor.Parent)
			if !ok || parent.Kind != "resource.connector" {
				return fmt.Errorf("episode %s parent %+v is not a connector", resource.ResourceID, parent)
			}
			hosted := false
			for _, relation := range r.after.Relations {
				hosted = hosted || relation.From == parent.ResourceID && relation.To == resource.ResourceID &&
					relation.State == semreg.LinkQualified && gvCoreType(c, relation.Definition) == string(graph.CoreHosts)
			}
			if !hosted {
				return fmt.Errorf("no qualified hosts instance from %s to %s", parent.ResourceID, resource.ResourceID)
			}
		}
		if episodes == 0 {
			return fmt.Errorf("no episode resource")
		}
		return nil
	},
	"derivation_inputs_resolve_in_one_home_asset": func(_ *vectorCase, r applyResult) error {
		candidates := map[semreg.CandidateID]semreg.Uint64{}
		for _, envelope := range r.after.Facts {
			for _, candidate := range envelope.Candidates {
				candidates[candidate.CandidateID] = candidate.Revision
			}
		}
		derived := 0
		for _, upsert := range r.batch.FactUpserts {
			if upsert.Derivation == nil {
				continue
			}
			derived++
			if _, ok := candidates[upsert.CandidateID]; !ok {
				return fmt.Errorf("derived candidate %s is not published", upsert.CandidateID)
			}
			if len(upsert.Derivation.Inputs) < 2 {
				return fmt.Errorf("derivation of %s has %d inputs", upsert.CandidateID, len(upsert.Derivation.Inputs))
			}
			for _, input := range upsert.Derivation.Inputs {
				if revision, ok := candidates[input.CandidateID]; !ok || revision != input.CandidateRevision {
					return fmt.Errorf("input %s@%s does not resolve in %s", input.CandidateID, input.CandidateRevision, r.after.AssetID)
				}
			}
		}
		if derived == 0 {
			return fmt.Errorf("the batch derives no fact")
		}
		return nil
	},
	"derived_fact_subject_is_inverter": func(c *vectorCase, r applyResult) error {
		catalog := c.registeredCatalog()
		for _, upsert := range r.batch.FactUpserts {
			if upsert.Derivation == nil {
				continue
			}
			pack := semreg.PackRef{ID: upsert.Key.PackID, Version: upsert.Key.PackVersion}
			subject, ok := catalog.FactSubject(pack, upsert.Key.FactID)
			if !ok || !subject.Resolved || subject.Kind != "resource.inverter" {
				return fmt.Errorf("fact %s subject %+v", upsert.Key.FactID, subject)
			}
			named := false
			for _, dimension := range upsert.Key.Dimensions {
				if dimension.ID != subject.Dimension || dimension.Value.Text == nil {
					continue
				}
				resource, ok := gvResource(r.after, semreg.ResourceID(*dimension.Value.Text))
				named = ok && resource.Kind == subject.Kind && resource.State == semreg.ResourceCurrent
			}
			if !named {
				return fmt.Errorf("fact %s names no current inverter", upsert.Key.FactID)
			}
			return nil
		}
		return fmt.Errorf("the batch derives no fact")
	},
	"relation_instance_stored": func(_ *vectorCase, r applyResult) error {
		if len(r.batch.RelationUpserts) == 0 {
			return fmt.Errorf("the batch upserts no relation instance")
		}
		for _, upsert := range r.batch.RelationUpserts {
			found := false
			for _, relation := range r.after.Relations {
				found = found || gvDeepEqual(relation, upsert)
			}
			if !found {
				return fmt.Errorf("instance %s -> %s is not stored as upserted", upsert.From, upsert.To)
			}
		}
		return nil
	},
	"endpoints_resolve_in_asset": func(_ *vectorCase, r applyResult) error {
		for _, upsert := range r.batch.RelationUpserts {
			if !gvCurrentResource(r.after, upsert.From) || !gvCurrentResource(r.after, upsert.To) {
				return fmt.Errorf("instance %s -> %s has an endpoint that is not a current resource of %s", upsert.From, upsert.To, r.after.AssetID)
			}
		}
		return nil
	},
	"both_contains_instances_conflict": func(c *vectorCase, r applyResult) error {
		return gvConflicting(c, r, string(graph.CoreContains), false)
	},
	"every_cycle_instance_conflict": func(c *vectorCase, r applyResult) error {
		return gvConflicting(c, r, string(graph.CoreRelays), true)
	},
	"no_instance_chosen": func(_ *vectorCase, r applyResult) error {
		// A conflict is never resolved by keeping one side: no instance
		// that shares an endpoint with a conflicting one stays qualified.
		ends := map[semreg.ResourceID]bool{}
		conflicts := 0
		for _, relation := range r.after.Relations {
			if relation.State == semreg.LinkConflict {
				conflicts++
				ends[relation.From], ends[relation.To] = true, true
			}
		}
		for _, relation := range r.after.Relations {
			if relation.State == semreg.LinkQualified && ends[relation.To] {
				return fmt.Errorf("instance %s -> %s was chosen", relation.From, relation.To)
			}
		}
		if conflicts < 2 {
			return fmt.Errorf("%d conflicting instances", conflicts)
		}
		return nil
	},
	"native_basis_instances_removed": func(_ *vectorCase, r applyResult) error {
		return gvRemovedNative(r, func(origin semreg.OriginRef) bool { return gvFenced(r.before, r.batch, origin) })
	},
	"native_basis_instance_removed": func(_ *vectorCase, r applyResult) error {
		return gvRemovedNative(r, func(origin semreg.OriginRef) bool { return gvFenced(r.before, r.batch, origin) })
	},
	"configured_instance_kept": func(_ *vectorCase, r applyResult) error {
		for _, relation := range r.before.Relations {
			if relation.BasisClass == semreg.RelationConfiguration {
				return gvKeptExcept(r, true)
			}
		}
		return fmt.Errorf("the prior state holds no configured instance")
	},
	"contributor_instance_kept": func(_ *vectorCase, r applyResult) error {
		// The contributor's instance has a basis the fence does not end.
		return gvKeptExcept(r, true)
	},
	"resources_unaffected": func(_ *vectorCase, r applyResult) error {
		if len(r.before.Resources) == 0 || !gvDeepEqual(r.before.Resources, r.after.Resources) {
			return fmt.Errorf("resources %+v became %+v", r.before.Resources, r.after.Resources)
		}
		return nil
	},
	"absence_statement_stored": func(_ *vectorCase, r applyResult) error {
		if len(r.batch.AbsenceUpserts) == 0 {
			return fmt.Errorf("the batch states no absence")
		}
		for _, upsert := range r.batch.AbsenceUpserts {
			found := false
			for _, absence := range r.after.RelationAbsences {
				found = found || gvDeepEqual(absence, upsert)
			}
			if !found {
				return fmt.Errorf("absence of %s is not stored as upserted", upsert.Resource)
			}
		}
		return nil
	},
	"relation_known_to_be_none": func(_ *vectorCase, r applyResult) error {
		for _, absence := range r.batch.AbsenceUpserts {
			for _, relation := range r.after.Relations {
				end := relation.From
				if absence.Side == semreg.AbsenceTo {
					end = relation.To
				}
				if relation.Definition == absence.Definition && end == absence.Resource {
					return fmt.Errorf("instance %s -> %s contradicts the absence", relation.From, relation.To)
				}
			}
		}
		return nil
	},
}

// gvConflicting checks that the instances of a core type the vector lists
// are all conflict; for a cycle, that they also close a cycle.
func gvConflicting(c *vectorCase, r applyResult, coreType string, cycle bool) error {
	listed := 0
	next := map[semreg.ResourceID]semreg.ResourceID{}
	for _, want := range c.expect.RelationStates {
		relation, ok := gvRelationByJSON(c, r.after, want)
		if !ok || gvCoreType(c, relation.Definition) != coreType {
			continue
		}
		listed++
		if relation.State != semreg.LinkConflict {
			return fmt.Errorf("%s -> %s is %s", relation.From, relation.To, relation.State)
		}
		next[relation.From] = relation.To
	}
	if listed < 2 {
		return fmt.Errorf("%d %s instances listed", listed, coreType)
	}
	if cycle {
		for start := range next {
			at, steps := next[start], 1
			for at != start && steps <= len(next) {
				at, steps = next[at], steps+1
			}
			if at != start {
				return fmt.Errorf("the %s instances do not close a cycle through %s", coreType, start)
			}
		}
	}
	return nil
}

// =========================================================== assemble_cut

type cutResult struct {
	cut       *graph.Cut
	snapshots []semreg.Snapshot
}

func runAssembleCut(c *vectorCase) {
	c.t.Helper()
	if c.state.Registry == nil || c.state.Registry.ElementCatalog != "registered" || len(c.state.Batches) != 0 {
		c.t.Fatalf("prior state %s must register the catalog and hold no batch", c.state.Name)
	}
	var input struct {
		Completeness string            `json:"completeness"`
		Snapshots    []json.RawMessage `json:"snapshots"`
	}
	if err := gvStrict(c.vector.Input, &input); err != nil {
		c.t.Fatalf("decode input: %v", err)
	}
	snapshots := []semreg.Snapshot{}
	var err error
	for i, raw := range input.Snapshots {
		var snapshot semreg.Snapshot
		snapshot, err = semreg.Decode[semreg.Snapshot](raw)
		c.checkRecord(fmt.Sprintf("input snapshot %d", i), raw, err, false)
		if err != nil {
			break
		}
		snapshots = append(snapshots, snapshot)
	}
	var cut *graph.Cut
	if err == nil {
		inputs := gvCanonicalSnapshots(c, snapshots)
		cut, err = graph.AssembleCut(snapshots, c.registeredCatalog(), graph.Completeness(input.Completeness))
		if !bytes.Equal(inputs, gvCanonicalSnapshots(c, snapshots)) {
			c.t.Fatal("AssembleCut changed its input")
		}
	}
	if !c.verdict(err) {
		if cut != nil {
			c.t.Fatal("a refused assembly returned a cut")
		}
		return
	}
	c.compareCut(cut, snapshots)
	gvAssert(c, cutAssertions, cutResult{cut: cut, snapshots: snapshots})
}

func gvCanonicalSnapshots(c *vectorCase, snapshots []semreg.Snapshot) []byte {
	var out []byte
	for _, snapshot := range snapshots {
		raw, err := semreg.CanonicalJSON(snapshot)
		if err != nil {
			c.t.Fatalf("CanonicalJSON(snapshot %s) = %v", snapshot.AssetID, err)
		}
		out = append(append(out, raw...), '\n')
	}
	return out
}

// compareCut compares the cut with the expected cut of the vector: its
// identifier, completeness, members, excluded members, findings, the
// coherence of every relation instance and the planes of every resource a
// member holds.
func (c *vectorCase) compareCut(cut *graph.Cut, snapshots []semreg.Snapshot) {
	c.t.Helper()
	want := c.expect.Cut
	if want == nil {
		c.t.Fatal("an accepted cut vector states no cut")
	}
	if cut.ID() != want.CutID {
		c.t.Errorf("cut id = %s, want %s", cut.ID(), want.CutID)
	}
	if string(cut.Completeness()) != want.Completeness {
		c.t.Errorf("completeness = %s, want %s", cut.Completeness(), want.Completeness)
	}
	if !gvSameJSON(c.t, cut.Members(), want.Members) {
		c.t.Errorf("members = %s, want %s", gvMarshal(c.t, cut.Members()), gvMarshal(c.t, want.Members))
	}
	if !gvDeepEqual(append([]semreg.AssetID{}, cut.Excluded()...), append([]semreg.AssetID{}, want.ExcludedMembers...)) {
		c.t.Errorf("excluded = %v, want %v", cut.Excluded(), want.ExcludedMembers)
	}
	findings := []gvCutFinding{}
	for _, finding := range cut.Findings() {
		findings = append(findings, c.findingSubject(finding))
	}
	if !gvSameJSON(c.t, findings, want.Findings) {
		c.t.Errorf("findings = %s, want %s", gvMarshal(c.t, findings), gvMarshal(c.t, want.Findings))
	}
	// An empty list and an absent one are the same canonical output.
	relations := append([]graph.CutRelation{}, cut.Relations()...)
	if !gvSameJSON(c.t, relations, want.RelationCoherence) {
		c.t.Errorf("relation coherence = %s, want %s", gvMarshal(c.t, relations), gvMarshal(c.t, want.RelationCoherence))
	}
	held := map[semreg.ResourceID]bool{}
	for _, snapshot := range snapshots {
		for _, resource := range snapshot.Resources {
			held[resource.ResourceID] = true
		}
	}
	listed := map[semreg.ResourceID]bool{}
	for _, entry := range want.PlaneMembership {
		listed[entry.ResourceID] = true
		if got := gvPlaneStrings(cut.PlanesOfResource(entry.ResourceID)); !gvEqualStrings(got, entry.Planes) {
			c.t.Errorf("planes of %s = %v, want %v", entry.ResourceID, got, entry.Planes)
		}
	}
	if !gvDeepEqual(held, listed) {
		c.t.Errorf("plane membership lists %v; the members hold %v", gvSortedKeys(listed), gvSortedKeys(held))
	}
}

// findingSubject writes a cut finding in the notation of the vector
// document: the subject of a duplicate identity is the resource and its
// holders, of a same unit the two resources, of an ungraphed finding its
// member, of an unresolved anchor parent the resource, and of a
// contradicted absence the statement and the instance it denies.
func (c *vectorCase) findingSubject(finding graph.CutFinding) gvCutFinding {
	c.t.Helper()
	var subject any
	switch finding.Kind {
	case graph.CutDuplicateIdentity:
		assets := []semreg.AssetID{}
		var id semreg.ResourceID
		for _, ref := range finding.Resources {
			assets, id = append(assets, ref.AssetID), ref.ResourceID
		}
		subject = map[string]any{"resource_id": id, "asset_ids": assets}
	case graph.CutSameUnit:
		ids := []string{}
		for _, ref := range finding.Resources {
			ids = append(ids, string(ref.ResourceID))
		}
		sort.Strings(ids)
		subject = map[string]any{"resource_ids": ids}
	case graph.CutUngraphed:
		if len(finding.Assets) != 1 {
			c.t.Fatalf("ungraphed finding names %v", finding.Assets)
		}
		subject = map[string]any{"asset_id": finding.Assets[0]}
	case graph.CutAnchorParentUnresolved:
		if len(finding.Resources) != 1 {
			c.t.Fatalf("anchor parent finding names %v", finding.Resources)
		}
		subject = map[string]any{"resource_id": finding.Resources[0].ResourceID}
	case graph.CutContradictedAbsence:
		subject = map[string]any{"absence": finding.Absence, "relation": finding.Relation}
	default:
		c.t.Fatalf("finding kind %s", finding.Kind)
	}
	return gvCutFinding{Kind: string(finding.Kind), Subject: gvMarshal(c.t, subject)}
}

func gvFindings(cut *graph.Cut, kind graph.CutFindingKind) []graph.CutFinding {
	out := []graph.CutFinding{}
	for _, finding := range cut.Findings() {
		if finding.Kind == kind {
			out = append(out, finding)
		}
	}
	return out
}

func gvAnyFinding(kind graph.CutFindingKind) func(*vectorCase, cutResult) error {
	return func(_ *vectorCase, r cutResult) error {
		if len(gvFindings(r.cut, kind)) == 0 {
			return fmt.Errorf("no %s finding in %+v", kind, r.cut.Findings())
		}
		return nil
	}
}

func gvAnyCoherence(coherence graph.RelationCoherence) func(*vectorCase, cutResult) error {
	return func(_ *vectorCase, r cutResult) error {
		for _, relation := range r.cut.Relations() {
			if relation.Coherence == coherence {
				return nil
			}
		}
		return fmt.Errorf("no %s relation in %+v", coherence, r.cut.Relations())
	}
}

// gvHeld finds a resource record and its holder among the input snapshots.
func gvHeld(r cutResult, id semreg.ResourceID, asset semreg.AssetID) (semreg.Resource, semreg.Snapshot, bool) {
	for _, snapshot := range r.snapshots {
		if asset != "" && snapshot.AssetID != asset {
			continue
		}
		for _, resource := range snapshot.Resources {
			if resource.ResourceID == id {
				return resource, snapshot, true
			}
		}
	}
	return semreg.Resource{}, semreg.Snapshot{}, false
}

func gvExcluded(r cutResult, asset semreg.AssetID) bool {
	for _, excluded := range r.cut.Excluded() {
		if excluded == asset {
			return true
		}
	}
	return false
}

// gvSameUnitPair returns the two resource records of each same unit finding.
func gvSameUnitPairs(r cutResult) ([][2]semreg.Resource, [][2]semreg.Snapshot, error) {
	pairs, holders := [][2]semreg.Resource{}, [][2]semreg.Snapshot{}
	for _, finding := range gvFindings(r.cut, graph.CutSameUnit) {
		if len(finding.Resources) != 2 {
			return nil, nil, fmt.Errorf("same unit finding names %v", finding.Resources)
		}
		var pair [2]semreg.Resource
		var held [2]semreg.Snapshot
		for i, ref := range finding.Resources {
			resource, snapshot, ok := gvHeld(r, ref.ResourceID, ref.AssetID)
			if !ok {
				return nil, nil, fmt.Errorf("%s is not held by %s", ref.ResourceID, ref.AssetID)
			}
			pair[i], held[i] = resource, snapshot
		}
		pairs, holders = append(pairs, pair), append(holders, held)
	}
	if len(pairs) == 0 {
		return nil, nil, fmt.Errorf("no same unit finding")
	}
	return pairs, holders, nil
}

func gvNativeKeyPair(pair [2]semreg.Resource) bool {
	return pair[0].Anchor.Basis == semreg.AnchorNativeIdentity && pair[1].Anchor.Basis == semreg.AnchorNativeIdentity &&
		pair[0].Anchor.Key != nil && pair[1].Anchor.Key != nil && *pair[0].Anchor.Key == *pair[1].Anchor.Key
}

func gvSlotPair(pair [2]semreg.Resource) bool {
	return pair[0].Anchor.Parent != nil && pair[1].Anchor.Parent != nil && pair[0].Anchor.Slot != nil && pair[1].Anchor.Slot != nil &&
		*pair[0].Anchor.Parent == *pair[1].Anchor.Parent && *pair[0].Anchor.Slot == *pair[1].Anchor.Slot
}

func gvRelationInNoPlane(_ *vectorCase, r cutResult) error {
	others := 0
	for _, relation := range r.cut.Relations() {
		if relation.Coherence == graph.CoherenceCoherent && relation.State == semreg.LinkQualified {
			continue
		}
		others++
		key := semreg.RelationKey{Definition: relation.Definition, From: relation.From, To: relation.To}
		if planes := r.cut.PlanesOfRelation(key); planes != nil {
			return fmt.Errorf("%s %s -> %s is in %v", relation.Coherence, relation.From, relation.To, planes)
		}
	}
	if others == 0 {
		return fmt.Errorf("no relation that is not a coherent qualified edge")
	}
	return nil
}

func gvBothKept(_ *vectorCase, r cutResult) error {
	pairs, holders, err := gvSameUnitPairs(r)
	if err != nil {
		return err
	}
	for i, pair := range pairs {
		for j := range pair {
			if gvExcluded(r, holders[i][j].AssetID) || pair[j].State != semreg.ResourceCurrent {
				return fmt.Errorf("%s of %s is not kept", pair[j].ResourceID, holders[i][j].AssetID)
			}
		}
	}
	return nil
}

var cutAssertions = map[string]func(*vectorCase, cutResult) error{
	"finding_duplicate_identity": gvAnyFinding(graph.CutDuplicateIdentity),
	"both_members_excluded": func(_ *vectorCase, r cutResult) error {
		for _, finding := range gvFindings(r.cut, graph.CutDuplicateIdentity) {
			if len(finding.Resources) < 2 {
				return fmt.Errorf("duplicate identity names %v", finding.Resources)
			}
			for _, ref := range finding.Resources {
				if !gvExcluded(r, ref.AssetID) {
					return fmt.Errorf("holder %s is kept", ref.AssetID)
				}
			}
			return nil
		}
		return fmt.Errorf("no duplicate identity finding")
	},
	"no_member_chosen": func(_ *vectorCase, r cutResult) error {
		for _, finding := range gvFindings(r.cut, graph.CutDuplicateIdentity) {
			for _, ref := range finding.Resources {
				if planes := r.cut.PlanesOfResource(ref.ResourceID); planes != nil {
					return fmt.Errorf("%s is presented in %v", ref.ResourceID, planes)
				}
				for _, member := range r.cut.Members() {
					if member.AssetID == ref.AssetID && !gvExcluded(r, member.AssetID) {
						return fmt.Errorf("member %s was chosen", member.AssetID)
					}
				}
			}
		}
		return nil
	},
	"cut_partial": func(_ *vectorCase, r cutResult) error {
		if r.cut.Completeness() != graph.CutPartial {
			return fmt.Errorf("completeness %s", r.cut.Completeness())
		}
		return nil
	},
	"relation_conflict": gvAnyCoherence(graph.CoherenceConflict),
	"to_kind_not_admitted": func(c *vectorCase, r cutResult) error {
		catalog := c.registeredCatalog()
		for _, relation := range r.cut.Relations() {
			if relation.Coherence != graph.CoherenceConflict {
				continue
			}
			_, to, ok := catalog.RelationEndpointKinds(graph.PackElement(relation.Definition))
			far, _, held := gvHeld(r, relation.To, "")
			if !ok || !held {
				continue
			}
			admitted := false
			for _, kind := range to {
				admitted = admitted || kind == far.Kind
			}
			if !admitted {
				return nil
			}
		}
		return fmt.Errorf("no conflicting relation whose to has a kind the definition does not admit")
	},
	"relation_in_no_plane":                  gvRelationInNoPlane,
	"relation_stale":                        gvAnyCoherence(graph.CoherenceStale),
	"relation_dangling":                     gvAnyCoherence(graph.CoherenceDangling),
	"relation_excluded":                     gvAnyCoherence(graph.CoherenceExcluded),
	"relation_outside_cut":                  gvAnyCoherence(graph.CoherenceOutsideCut),
	"relation_coherent":                     gvAnyCoherence(graph.CoherenceCoherent),
	"finding_same_unit":                     gvAnyFinding(graph.CutSameUnit),
	"finding_ungraphed":                     gvAnyFinding(graph.CutUngraphed),
	"finding_contradicted_absence":          gvAnyFinding(graph.CutContradictedAbsence),
	"finding_anchor_parent_unresolved":      gvAnyFinding(graph.CutAnchorParentUnresolved),
	"both_resources_kept":                   gvBothKept,
	"finding_same_unit_for_native_key":      gvSameUnitBy("the same native key", gvNativeKeyPair),
	"finding_same_unit_for_parent_and_slot": gvSameUnitBy("the same parent and slot", gvSlotPair),
	"keys_equal_across_protocols": func(_ *vectorCase, r cutResult) error {
		pairs, holders, err := gvSameUnitPairs(r)
		if err != nil {
			return err
		}
		for i, pair := range pairs {
			if !gvNativeKeyPair(pair) {
				continue
			}
			protocols := [2]map[semreg.DefinitionID]bool{{}, {}}
			for j := range holders[i] {
				for _, source := range holders[i][j].Sources {
					protocols[j][source.ProtocolID] = true
				}
			}
			shared := false
			for protocol := range protocols[0] {
				shared = shared || protocols[1][protocol]
			}
			if !shared && len(protocols[0]) > 0 && len(protocols[1]) > 0 {
				return nil
			}
		}
		return fmt.Errorf("no same unit pair with one key seen through two protocols")
	},
	"member_in_no_plane": func(_ *vectorCase, r cutResult) error {
		for _, finding := range gvFindings(r.cut, graph.CutUngraphed) {
			for _, snapshot := range r.snapshots {
				if snapshot.AssetID != finding.Assets[0] {
					continue
				}
				if len(snapshot.Facts) == 0 {
					return fmt.Errorf("ungraphed member %s holds no fact", snapshot.AssetID)
				}
				for _, envelope := range snapshot.Facts {
					if planes := r.cut.PlanesOfFact(snapshot.AssetID, envelope.Key); planes != nil {
						return fmt.Errorf("fact %s of %s is in %v", envelope.Key.FactID, snapshot.AssetID, planes)
					}
				}
			}
			return nil
		}
		return fmt.Errorf("no ungraphed finding")
	},
	"absence_statement_kept": func(_ *vectorCase, r cutResult) error {
		for _, finding := range gvFindings(r.cut, graph.CutContradictedAbsence) {
			absence := finding.Absence
			for _, snapshot := range r.snapshots {
				for _, statement := range snapshot.RelationAbsences {
					if snapshot.AssetID == absence.AssetID && statement.Definition == absence.Definition &&
						statement.Resource == absence.Resource && statement.Side == absence.Side {
						return nil
					}
				}
			}
			return fmt.Errorf("the contradicted absence %+v is not held", absence)
		}
		return fmt.Errorf("no contradicted absence finding")
	},
	"no_ungraphed_finding": func(_ *vectorCase, r cutResult) error {
		if found := gvFindings(r.cut, graph.CutUngraphed); len(found) != 0 {
			return fmt.Errorf("ungraphed findings %+v", found)
		}
		return nil
	},
	"unknown_facts_not_counted": func(c *vectorCase, r cutResult) error {
		catalog := c.registeredCatalog()
		facts := 0
		for _, snapshot := range r.snapshots {
			for _, envelope := range snapshot.Facts {
				facts++
				subject, ok := catalog.FactSubject(semreg.PackRef{ID: envelope.Key.PackID, Version: envelope.Key.PackVersion}, envelope.Key.FactID)
				if !ok || subject.Disposition == semreg.GraphMember {
					return fmt.Errorf("fact %s is a catalog member", envelope.Key.FactID)
				}
				if planes := r.cut.PlanesOfFact(snapshot.AssetID, envelope.Key); planes != nil {
					return fmt.Errorf("unknown fact %s is in %v", envelope.Key.FactID, planes)
				}
			}
		}
		if facts == 0 {
			return fmt.Errorf("no member holds a fact")
		}
		return nil
	},
	"no_finding": func(_ *vectorCase, r cutResult) error {
		if len(r.cut.Findings()) != 0 {
			return fmt.Errorf("findings %+v", r.cut.Findings())
		}
		return nil
	},
	"not_detectable_without_a_shared_key": func(_ *vectorCase, r cutResult) error {
		// Two resources of one kind in two members whose anchors share no
		// key yield no same unit finding.
		for i, a := range r.snapshots {
			for _, b := range r.snapshots[i+1:] {
				for _, x := range a.Resources {
					for _, y := range b.Resources {
						pair := [2]semreg.Resource{x, y}
						if x.Kind == y.Kind && !gvNativeKeyPair(pair) && !gvSlotPair(pair) {
							if len(gvFindings(r.cut, graph.CutSameUnit)) != 0 {
								return fmt.Errorf("a same unit finding was reported")
							}
							return nil
						}
					}
				}
			}
		}
		return fmt.Errorf("no pair of one kind without a shared key")
	},
	"two_resources": func(_ *vectorCase, r cutResult) error {
		ids := map[semreg.ResourceID]bool{}
		for _, snapshot := range r.snapshots {
			for _, resource := range snapshot.Resources {
				if resource.State == semreg.ResourceCurrent && r.cut.PlanesOfResource(resource.ResourceID) != nil {
					ids[resource.ResourceID] = true
				}
			}
		}
		if len(ids) != 2 {
			return fmt.Errorf("presented resources %v", gvSortedKeys(ids))
		}
		return nil
	},
	"cross_asset_relation": func(_ *vectorCase, r cutResult) error {
		for _, relation := range r.cut.Relations() {
			if relation.Coherence != graph.CoherenceCoherent {
				continue
			}
			if _, holder, ok := gvHeld(r, relation.To, ""); ok && holder.AssetID != relation.AssetID {
				return nil
			}
		}
		return fmt.Errorf("no coherent relation whose to is homed in another member")
	},
	"relation_in_its_planes": func(c *vectorCase, r cutResult) error {
		catalog := c.registeredCatalog()
		edges := 0
		for _, relation := range r.cut.Relations() {
			if relation.Coherence != graph.CoherenceCoherent || relation.State != semreg.LinkQualified {
				continue
			}
			edges++
			want, _ := catalog.PlanesOf(graph.PackElement(relation.Definition))
			key := semreg.RelationKey{Definition: relation.Definition, From: relation.From, To: relation.To}
			if got := r.cut.PlanesOfRelation(key); len(want) == 0 || !gvDeepEqual(got, want) {
				return fmt.Errorf("%s -> %s planes %v, catalog %v", relation.From, relation.To, got, want)
			}
		}
		if edges == 0 {
			return fmt.Errorf("no coherent qualified relation")
		}
		return nil
	},
	"identity_kept": func(_ *vectorCase, r cutResult) error {
		for _, finding := range gvFindings(r.cut, graph.CutAnchorParentUnresolved) {
			ref := finding.Resources[0]
			resource, _, ok := gvHeld(r, ref.ResourceID, ref.AssetID)
			if !ok || gvExcluded(r, ref.AssetID) || resource.State != semreg.ResourceCurrent || resource.Anchor.Parent == nil {
				return fmt.Errorf("%s did not keep its identity", ref.ResourceID)
			}
			return nil
		}
		return fmt.Errorf("no anchor parent finding")
	},
	"presented_under_no_parent": func(c *vectorCase, r cutResult) error {
		for _, finding := range gvFindings(r.cut, graph.CutAnchorParentUnresolved) {
			id := finding.Resources[0].ResourceID
			for _, relation := range r.cut.Relations() {
				core := gvCoreType(c, relation.Definition)
				if relation.To == id && relation.Coherence == graph.CoherenceCoherent && relation.State == semreg.LinkQualified &&
					(core == string(graph.CoreContains) || core == string(graph.CoreGroups) || core == string(graph.CoreHosts)) {
					return fmt.Errorf("%s is presented under %s", id, relation.From)
				}
			}
			return nil
		}
		return fmt.Errorf("no anchor parent finding")
	},
	"relaying_resource_in_physical_and_provenance_only": func(c *vectorCase, r cutResult) error {
		for _, relation := range r.cut.Relations() {
			if relation.Coherence != graph.CoherenceCoherent || gvCoreType(c, relation.Definition) != string(graph.CoreRelays) {
				continue
			}
			other := false
			for _, edge := range r.cut.Relations() {
				other = other || edge != relation && (edge.From == relation.From || edge.To == relation.From)
			}
			if other {
				continue
			}
			planes := gvPlaneStrings(r.cut.PlanesOfResource(relation.From))
			if !gvEqualStrings(planes, []string{string(graph.PlanePhysical), string(graph.PlaneProvenance)}) {
				return fmt.Errorf("relaying %s is in %v", relation.From, planes)
			}
			return nil
		}
		return fmt.Errorf("no resource that only relays")
	},
	"kind_planes_only_bound_membership": func(c *vectorCase, r cutResult) error {
		catalog := c.registeredCatalog()
		bounded := false
		for _, snapshot := range r.snapshots {
			for _, resource := range snapshot.Resources {
				membership, _ := catalog.ResourceKindMembership(resource.Kind)
				allowed := map[string]bool{}
				for _, plane := range membership.Planes {
					allowed[string(plane)] = true
				}
				planes := gvPlaneStrings(r.cut.PlanesOfResource(resource.ResourceID))
				for _, plane := range planes {
					if !allowed[plane] {
						return fmt.Errorf("%s is in %s outside its kind's planes", resource.ResourceID, plane)
					}
				}
				bounded = bounded || len(planes) < len(allowed)
			}
		}
		if !bounded {
			return fmt.Errorf("every resource is in all planes of its kind")
		}
		return nil
	},
}

func gvSameUnitBy(what string, match func([2]semreg.Resource) bool) func(*vectorCase, cutResult) error {
	return func(_ *vectorCase, r cutResult) error {
		pairs, _, err := gvSameUnitPairs(r)
		if err != nil {
			return err
		}
		for _, pair := range pairs {
			if match(pair) {
				return nil
			}
		}
		return fmt.Errorf("no same unit finding for %s", what)
	}
}

// ================================================================== admit

type admitResult struct {
	snapshot  semreg.Snapshot
	intent    operation.Intent
	admission *operation.Admission
	asset     semreg.AssetID
	input     map[string]any
}

func (c *vectorCase) operationKernel() *operation.Kernel {
	c.t.Helper()
	registry := c.state.Registry
	validators := gvValidators(registry.Packs)
	options := operation.KernelOptions{MaxRetainedOperations: operation.DefaultMaxRetainedOperations}
	var kernel *operation.Kernel
	var err error
	if registry.ElementCatalog == "registered" {
		kernel, err = operation.NewGraphKernel(options, c.catalogFor(registry), validators...)
	} else {
		kernel, err = operation.NewKernelWithOptions(options, validators...)
	}
	if err != nil {
		c.t.Fatalf("operation kernel: %v", err)
	}
	return kernel
}

func (c *vectorCase) admit(snapshot semreg.Snapshot, input map[string]any) (*operation.Admission, operation.Intent, error) {
	c.t.Helper()
	intent, err := c.expandIntent(input, snapshot)
	if err != nil {
		return nil, intent, err
	}
	admission, err := c.operationKernel().Admit(snapshot, c.admissionContext(), intent, c.authority())
	return admission, intent, err
}

func runAdmit(c *vectorCase) {
	c.t.Helper()
	input := gvObject(c.t, c.vector.Input)
	asset, _ := input["asset_id"].(string)
	p := c.publisher(semreg.AssetID(asset), nil)
	snapshot, before, ok := p.current()
	if !ok {
		c.t.Fatalf("prior state %s publishes no snapshot", c.state.Name)
	}
	intent, err := c.expandIntent(input, snapshot)
	var admission *operation.Admission
	kernel := c.operationKernel()
	if err == nil {
		admission, err = kernel.Admit(snapshot, c.admissionContext(), intent, c.authority())
	}
	c.checkRecord("input intent", c.vector.Input, err, false)
	if !c.verdict(err) {
		if admission != nil {
			c.t.Fatal("a refused intent returned an admission")
		}
		if _, now, _ := p.current(); !bytes.Equal(now, before) {
			c.t.Fatal("admission changed the published state")
		}
		// The refusal installed nothing: the same intent is refused again.
		if decodeErr := err; decodeErr != nil && intent.IdempotencyKey != "" {
			_, again := kernel.Admit(snapshot, c.admissionContext(), intent, c.authority())
			if semreg.ErrorIdentifier(again) != c.expect.ErrorID {
				c.t.Fatalf("repeated admission = %v", again)
			}
		}
		return
	}
	gvAssert(c, admitAssertions, admitResult{snapshot: snapshot, intent: intent, admission: admission, asset: semreg.AssetID(asset), input: input})
}

// gvEffectSubject is the resource the expected-effect fact is about: the
// value of the fact's resource dimension in the catalog.
func gvEffectSubject(c *vectorCase, intent operation.Intent) (semreg.ResourceID, error) {
	key := intent.ExpectedEffect.Fact
	subject, ok := c.registeredCatalog().FactSubject(semreg.PackRef{ID: key.PackID, Version: key.PackVersion}, key.FactID)
	if !ok || !subject.Resolved {
		return "", fmt.Errorf("fact %s has no resolved subject", key.FactID)
	}
	for _, dimension := range key.Dimensions {
		if dimension.ID == subject.Dimension && dimension.Value.Text != nil {
			return semreg.ResourceID(*dimension.Value.Text), nil
		}
	}
	return "", fmt.Errorf("fact %s names no subject resource", key.FactID)
}

func gvRouteService(r admitResult) (semreg.ServiceInstance, operation.Route, error) {
	route, ok := r.admission.Route()
	if !ok {
		return semreg.ServiceInstance{}, route, fmt.Errorf("admission has no route")
	}
	for _, service := range r.snapshot.Services {
		if service.InstanceID == route.ServiceInstance {
			return service, route, nil
		}
	}
	return semreg.ServiceInstance{}, route, fmt.Errorf("route service %s is not in the snapshot", route.ServiceInstance)
}

var admitAssertions = map[string]func(*vectorCase, admitResult) error{
	"route_is_target_resource_capability": func(c *vectorCase, r admitResult) error {
		target, err := gvEffectSubject(c, r.intent)
		if err != nil {
			return err
		}
		service, _, err := gvRouteService(r)
		if err != nil {
			return err
		}
		if service.Resource == nil || *service.Resource != target {
			return fmt.Errorf("route service %s is about %v, the effect about %s", service.InstanceID, service.Resource, target)
		}
		return nil
	},
	"route_uses_relaying_binding": func(c *vectorCase, r admitResult) error {
		target, err := gvEffectSubject(c, r.intent)
		if err != nil {
			return err
		}
		_, route, err := gvRouteService(r)
		if err != nil {
			return err
		}
		for _, link := range r.snapshot.IdentityLinks {
			if link.BindingID != route.BindingID || link.Resource == nil || *link.Resource == target {
				continue
			}
			for _, relation := range r.snapshot.Relations {
				if relation.From == *link.Resource && relation.To == target && gvCoreType(c, relation.Definition) == string(graph.CoreRelays) {
					return nil
				}
			}
		}
		return fmt.Errorf("route binding %s does not observe a resource that relays %s", route.BindingID, target)
	},
	"same_result_without_relation_instance": func(c *vectorCase, r admitResult) error {
		if len(r.snapshot.Relations) == 0 {
			return fmt.Errorf("the prior state holds no relation instance")
		}
		without := c.publisher(r.asset, func(batch map[string]any) {
			for _, member := range []string{"relation_upserts", "relation_withdrawals", "absence_upserts", "absence_withdrawals"} {
				delete(batch, member)
			}
		})
		snapshot, _, _ := without.current()
		if len(snapshot.Relations) != 0 {
			return fmt.Errorf("the stripped prior state still holds relations")
		}
		admission, _, err := c.admit(snapshot, r.input)
		if err != nil {
			return fmt.Errorf("admission without the relation instance = %v", err)
		}
		route, _ := r.admission.Route()
		other, _ := admission.Route()
		claims, _ := r.admission.GuardClaims()
		otherClaims, _ := admission.GuardClaims()
		if !gvDeepEqual(route, other) || !gvDeepEqual(claims, otherClaims) {
			return fmt.Errorf("route %+v / %+v differs from %+v / %+v", other, otherClaims, route, claims)
		}
		return nil
	},
}
