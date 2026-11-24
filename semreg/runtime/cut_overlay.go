package runtime

import (
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// OverlayLink is a declaration whose relation is not a pack definition, kept
// with the cut so the publication relation rules stay the kernel's. A relays
// link is one of these: its type is a core definition id, not a pack ref.
type OverlayLink struct {
	Relation v1.DefinitionID
	From     driver.NodeRef
	To       driver.NodeRef
}

// Coherence reports a pack relation from the graph cut when the cut holds it,
// and an overlay declaration otherwise. An absent target is dangling on a
// complete cut and outside-cut on a partial cut. No current resource is added
// for the missing end.
func (v View) Coherence(key driver.LinkKey) (graph.RelationCoherence, bool) {
	if v.Cut != nil {
		if rel, ok := v.Cut.Relation(v1.RelationKey{Definition: key.Type, From: key.From.ID, To: key.To.ID}); ok {
			return rel.Coherence, true
		}
	}
	return v.overlayCoherence(key.Type.ID, key.From.ID, key.To.ID)
}

// OverlayCoherence reports one overlay declaration. relation is the core
// definition id, such as relation.relays.
func (v View) OverlayCoherence(relation v1.DefinitionID, from, to v1.ResourceID) (graph.RelationCoherence, bool) {
	return v.overlayCoherence(relation, from, to)
}

func (v View) overlayCoherence(relation v1.DefinitionID, from, to v1.ResourceID) (graph.RelationCoherence, bool) {
	found := false
	for _, link := range v.Overlay {
		if link.Relation == relation && link.From.ID == from && link.To.ID == to {
			found = true
			break
		}
	}
	if !found {
		return "", false
	}
	start := resourceState(v, from)
	target := resourceState(v, to)
	completeness := graph.CutComplete
	if v.Cut != nil {
		completeness = v.Cut.Completeness()
	}
	switch {
	case start.current && target.current:
		return graph.CoherenceCoherent, true
	case start.current && target.excluded:
		return graph.CoherenceExcluded, true
	case start.current && target.present:
		return graph.CoherenceStale, true
	case start.current && completeness == graph.CutComplete:
		return graph.CoherenceDangling, true
	case start.current:
		return graph.CoherenceOutsideCut, true
	default:
		return graph.CoherenceDangling, true
	}
}

type resourceSeen struct {
	present, current, excluded bool
}

func resourceState(v View, id v1.ResourceID) resourceSeen {
	var seen resourceSeen
	excluded := map[v1.AssetID]bool{}
	if v.Cut != nil {
		for _, asset := range v.Cut.Excluded() {
			excluded[asset] = true
		}
	}
	for _, snap := range v.Members {
		for _, resource := range snap.Resources {
			if resource.ResourceID != id {
				continue
			}
			if excluded[snap.AssetID] {
				seen.excluded = true
				continue
			}
			seen.present = true
			if resource.State == v1.ResourceCurrent {
				seen.current = true
			}
		}
	}
	return seen
}
