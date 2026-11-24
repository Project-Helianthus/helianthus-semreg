package runtime

import (
	"context"
	"fmt"
	"sort"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// installationAsset is the graph asset of the installation root, the bridge
// and the link equipment.
const (
	installationAsset  v1.AssetID       = "asset:installation"
	installationSource v1.SourceID      = "source:installation"
	installationEpoch  v1.SourceEpochID = "epoch:installation"
)

// Install publishes the installation site, the bridge and every link node in
// the plan, including a link whose instance is stopped or disabled. Relays
// stay on the runtime overlay. Site relations the catalog defines are
// published through the graph kernel when their start is current.
func (s *Service) Install(ctx context.Context, plan life.PlanSet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.Bridge == "" || plan.Installation == "" {
		return fmt.Errorf("runtime: the plan names no installation")
	}
	session, err := s.OpenSession(SessionSpec{
		Driver: life.RuntimeDriver, Instance: driver.InstanceKey(plan.Bridge),
		Asset: installationAsset, Source: installationSource, Epoch: installationEpoch, Generation: "1",
		Graph: true,
	})
	if err != nil {
		return err
	}
	site, err := s.ledger.Site(plan.Bridge, plan.Installation, nil)
	if err != nil {
		return err
	}
	root, _ := s.catalog.InstallationRoot()
	nodes := []placedNode{{Ref: site, Kind: root, Key: driver.NodeKey{Installation: plan.Installation, Driver: identity.SiteDriver, Instance: identity.SiteInstance, Kind: root, Anchor: driver.AnchorKey{Slot: identity.SiteSlot}}}}
	refs := map[string]driver.NodeRef{}
	var resources []v1.Resource
	siteResource, err := s.operatorResource(site.ID, root, identity.SiteSlot)
	if err != nil {
		return err
	}
	resources = append(resources, siteResource)
	for _, key := range plan.Nodes {
		ref, err := s.ledger.Node(plan.Bridge, key)
		if err != nil {
			return err
		}
		resource, err := s.operatorResource(ref.ID, key.Kind, key.Anchor.Slot)
		if err != nil {
			return err
		}
		resources = append(resources, resource)
		nodes = append(nodes, placedNode{Ref: ref, Kind: key.Kind, Key: key})
		refs[nodeText(key)] = ref
	}
	var overlay []OverlayLink
	for _, edge := range plan.Edges {
		from, fromOK := refs[nodeText(edge.From)]
		to, toOK := refs[nodeText(edge.To)]
		if !fromOK || !toOK {
			return fmt.Errorf("runtime: a relays end is not a planned node")
		}
		overlay = append(overlay, OverlayLink{Relation: edge.Relation, From: from, To: to})
	}
	current := make([]v1.Resource, len(resources))
	copy(current, resources)
	claims := identity.SiteRelations(s.catalog, site, current)
	var relations []v1.RelationInstance
	var kept []driver.LinkClaim
	for _, claim := range claims {
		if len(claim.Basis) == 0 {
			continue
		}
		relations = append(relations, relationOf(claim))
		kept = append(kept, claim)
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].ResourceID < resources[j].ResourceID })
	sort.Slice(relations, func(i, j int) bool {
		return relationGraphKey(relations[i]) < relationGraphKey(relations[j])
	})
	batch, err := session.Prepare(func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{installationSourceDescriptor(s.wall())}
		b.ResourceUpserts = resources
		if len(relations) > 0 {
			b.RelationUpserts = relations
		}
	})
	if err != nil {
		return err
	}
	tx, err := session.Begin(ctx)
	if err != nil {
		return err
	}
	concrete := tx.(*transaction)
	if err := concrete.Stage(batch); err != nil {
		return err
	}
	for _, claim := range kept {
		if err := concrete.StageLink(claim); err != nil {
			_ = concrete.Abort(ctx)
			return err
		}
	}
	if err := concrete.Commit(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.installation = plan.Installation
	s.bridge = plan.Bridge
	s.nodes = nodes
	s.overlay = append([]OverlayLink(nil), overlay...)
	s.mu.Unlock()
	return nil
}

func (s *Service) operatorResource(id v1.ResourceID, kind v1.DefinitionID, slot string) (v1.Resource, error) {
	class, ok := s.catalog.ResourceKindClass(kind)
	if !ok {
		return v1.Resource{}, fmt.Errorf("runtime: %s is not a catalog resource kind", kind)
	}
	if slot == "" {
		return v1.Resource{}, fmt.Errorf("runtime: %s has no operator slot", id)
	}
	evidence := []v1.EvidenceRef{evidenceRef("anchor:" + string(id))}
	originSlot := slot
	if len(originSlot) > 200 {
		originSlot = originSlot[:200]
	}
	return v1.Resource{
		ResourceID: id, Kind: kind,
		Anchor:   v1.ResourceAnchor{Class: class, Basis: v1.AnchorOperatorSlot, Slot: &slot, Evidence: evidence},
		State:    v1.ResourceCurrent,
		Origin:   v1.OriginRef{OriginID: v1.OriginID("origin:operator:" + originSlot), Kind: v1.OriginOperator, Evidence: []v1.EvidenceRef{evidenceRef("origin:" + string(id))}},
		Revision: "1",
	}, nil
}

func installationSourceDescriptor(started v1.TimePoint) v1.SourceDescriptor {
	return v1.SourceDescriptor{
		SourceID: installationSource, SourceEpochID: installationEpoch,
		ProtocolID: "protocol.runtime", ProfileID: "profile.runtime", ProfileVersion: "release-1",
		RegistryEvidence: evidenceRef("registry:installation"), StartedAt: started, State: v1.SourceCurrent, Revision: "1",
	}
}

func relationOf(claim driver.LinkClaim) v1.RelationInstance {
	return v1.RelationInstance{
		Definition: claim.Key.Type, From: claim.Key.From.ID, To: claim.Key.To.ID,
		State: v1.LinkQualified, BasisClass: v1.RelationConfiguration, Basis: append([]v1.EvidenceRef(nil), claim.Basis...),
		Origin:   v1.OriginRef{OriginID: "origin:operator:relation", Kind: v1.OriginOperator, Evidence: append([]v1.EvidenceRef(nil), claim.Basis...)},
		Revision: "1",
	}
}

func relationGraphKey(r v1.RelationInstance) string {
	return string(r.Definition.ID) + "\x00" + string(r.From) + "\x00" + string(r.To)
}

func nodeText(key driver.NodeKey) string {
	return key.Installation + "\x00" + string(key.Driver) + "\x00" + string(key.Instance) + "\x00" + string(key.Kind) + "\x00" + key.Anchor.Slot
}
