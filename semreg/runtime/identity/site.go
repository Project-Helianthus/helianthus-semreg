package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// The members of the key of the default site, besides the installation key. The
// site belongs to no driver; these constants fill the members NodeKey requires.
const (
	SiteDriver   driver.DriverKind  = "semreg.runtime"
	SiteInstance driver.InstanceKey = "site"
	SiteSlot                        = "site"
)

// systemBatteryRelation is the catalog relation of the configured storage
// grouping.
var systemBatteryRelation = semreg.DefinitionRef{
	Pack:    semreg.PackRef{ID: "helianthus.pack.storage", Version: "1.1.0"},
	ID:      "storage.relation.system_battery",
	Version: "1.1.0",
}

// SiteKey is the key of the default site of an installation: the same on every
// start, derived from the configured installation key alone.
func SiteKey(installation string, catalog *graph.Catalog) (driver.NodeKey, error) {
	root, ok := catalog.InstallationRoot()
	if !ok {
		return driver.NodeKey{}, fmt.Errorf("identity: the catalog marks no installation root")
	}
	return driver.NodeKey{Installation: installation, Driver: SiteDriver, Instance: SiteInstance, Kind: root, Anchor: driver.AnchorKey{Slot: SiteSlot}}, nil
}

// Site returns the one site of an installation. When the operator's records
// already declare a site for it, that declared site is the installation root and
// no other is created. Otherwise the site is the default one, allocated from the
// installation key. Naming a second, different site for the installation is
// refused with ErrSecondSite.
func (l *Ledger) Site(bridge driver.BridgeID, installation string, declared *semreg.ResourceID) (driver.NodeRef, error) {
	if err := bridge.Validate(); err != nil {
		return driver.NodeRef{}, fmt.Errorf("bridge: %w", err)
	}
	var site driver.NodeRef
	if declared != nil {
		if err := declared.Validate(); err != nil {
			return driver.NodeRef{}, fmt.Errorf("declared site: %w", err)
		}
		site = driver.NodeRef{Bridge: bridge, ID: *declared}
	} else {
		key, err := SiteKey(installation, l.allocator.catalog)
		if err != nil {
			return driver.NodeRef{}, err
		}
		if err := key.Validate(); err != nil {
			return driver.NodeRef{}, err
		}
		site = driver.NodeRef{Bridge: bridge, ID: pathNodeID(key)}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if held, ok := l.sites[installation]; ok && held != site {
		return driver.NodeRef{}, ErrSecondSite
	}
	if declared == nil {
		key, _ := SiteKey(installation, l.allocator.catalog)
		l.addKey(site, key)
	}
	root, _ := l.allocator.catalog.InstallationRoot()
	l.allocator.know(site.ID, root)
	l.sites[installation] = site
	return site, nil
}

// SiteRelations links the site by every relation the catalog defines from the
// installation root: relation.site_system to each current system resource and
// each configured generic equipment root the catalog admits as its to end, the
// site-grid-connection relation to a current grid-connection resource, and the
// relation the catalog defines for the meters, whenever those resources are
// published. The basis is configuration and the origin the operator. A resource
// that is not current has no link, so a withdrawn resource's link is withdrawn
// with it. Nothing is derived from an index, a name or an address; the
// resources are in the site's bridge. When the catalog has no relation.site_system
// no claim of it is made: the catalog has to publish that relation first.
func SiteRelations(catalog *graph.Catalog, site driver.NodeRef, current []semreg.Resource) []driver.LinkClaim {
	var claims []driver.LinkClaim
	if catalog == nil {
		return claims
	}
	root, ok := catalog.InstallationRoot()
	if !ok {
		return claims
	}
	for _, element := range catalog.Elements() {
		ref, packed := element.Ref.DefinitionRef()
		if element.Relation == nil || element.Disposition != graph.DispositionMember || !packed {
			continue
		}
		switch {
		case containsKind(element.Relation.FromKinds, root):
			for _, resource := range current {
				if resource.State == semreg.ResourceCurrent && containsKind(element.Relation.ToKinds, resource.Kind) && configuredRoot(resource) {
					claims = append(claims, configurationClaim(ref, site, driver.NodeRef{Bridge: site.Bridge, ID: resource.ResourceID}))
				}
			}
		case containsKind(element.Relation.FromKinds, gridConnectionKind) && containsKind(element.Relation.ToKinds, meterKind) && containsBasis(element.Relation.BasisClasses, semreg.RelationBasis("configuration")):
			// The meter relation runs from the grid connection, not from the
			// site: the relation is one to many, so a meter has one grid
			// connection. With exactly one current grid connection it links each
			// current meter; with several, nothing in the published resources
			// says which one a meter belongs to, so no meter is linked.
			var grids []semreg.Resource
			for _, grid := range current {
				if grid.State == semreg.ResourceCurrent && grid.Kind == gridConnectionKind {
					grids = append(grids, grid)
				}
			}
			if len(grids) != 1 {
				continue
			}
			for _, meter := range current {
				if meter.State == semreg.ResourceCurrent && meter.Kind == meterKind {
					claims = append(claims, configurationClaim(ref, driver.NodeRef{Bridge: site.Bridge, ID: grids[0].ResourceID}, driver.NodeRef{Bridge: site.Bridge, ID: meter.ResourceID}))
				}
			}
		}
	}
	sort.Slice(claims, func(i, j int) bool {
		a, b := claims[i].Key, claims[j].Key
		if a.Type.ID != b.Type.ID {
			return a.Type.ID < b.Type.ID
		}
		if c := compareRef(a.From, b.From); c != 0 {
			return c < 0
		}
		return compareRef(a.To, b.To) < 0
	})
	return claims
}

// The kinds of the grid connection and of the meter the catalog's meter
// relation runs between.
const (
	gridConnectionKind semreg.DefinitionID = "resource.grid_connection"
	meterKind          semreg.DefinitionID = "resource.meter"
)

func containsBasis(bases []semreg.RelationBasis, basis semreg.RelationBasis) bool {
	for _, b := range bases {
		if b == basis {
			return true
		}
	}
	return false
}

// genericEquipment is the kind of a generic equipment root, which is linked to
// the site only when the operator configured it: an operator slot without a
// parent.
const genericEquipment semreg.DefinitionID = "resource.equipment"

func configuredRoot(resource semreg.Resource) bool {
	if resource.Kind != genericEquipment {
		return true
	}
	return resource.Anchor.Basis == semreg.AnchorOperatorSlot && resource.Anchor.Parent == nil
}

func containsKind(kinds []semreg.DefinitionID, kind semreg.DefinitionID) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// StorageGrouping links the configured storage system to each configured
// battery by system_battery, with the basis configuration and the origin the
// operator. Each identity comes from its operator slot, so the links are the
// same on every start.
func StorageGrouping(system driver.NodeRef, batteries []driver.NodeRef) []driver.LinkClaim {
	claims := make([]driver.LinkClaim, 0, len(batteries))
	for _, battery := range batteries {
		claims = append(claims, configurationClaim(systemBatteryRelation, system, battery))
	}
	return claims
}

// configurationClaim is the claim of one link whose basis is the operator's
// configuration. The evidence digest is the SHA-256 of the canonical link key,
// so it is the same on every start.
func configurationClaim(relation semreg.DefinitionRef, from, to driver.NodeRef) driver.LinkClaim {
	key := driver.LinkKey{Type: relation, From: from, To: to}
	canonical, err := semreg.CanonicalJSON(key)
	if err != nil {
		return driver.LinkClaim{Key: key}
	}
	sum := sha256.Sum256(canonical)
	return driver.LinkClaim{Key: key, Basis: []semreg.EvidenceRef{{
		Owner: OperatorEvidenceOwner, Kind: "evidence.configuration", Digest: semreg.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Contract: "helianthus.configuration/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone,
	}}}
}
