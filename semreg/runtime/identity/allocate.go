package identity

import (
	"errors"
	"fmt"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Errors of the allocation of a node key.
var (
	// ErrNoCatalog refuses Allocate on an allocator built without a catalog.
	ErrNoCatalog = errors.New("identity: allocation needs the element catalog")
	// ErrIncompleteKey refuses a key whose members or anchor are incomplete.
	// The driver lists that entity in its diagnostics until its native identity
	// is complete.
	ErrIncompleteKey = errors.New("identity: the node key is incomplete")
)

// Allocate returns the identifier of the node a key names. The same key always
// gives the same identifier: no arrival index, epoch, generation, clock reading,
// network address, host name, role string, credential, certificate fingerprint
// or current value enters it. A key whose members or anchor are incomplete gets
// no identifier and the reason.
//
// A kind the catalog lists as a resource kind, other than the installation
// root, keeps the legacy allocation: the identifier is the one Resolve gives
// the same kind and anchor, and it is recorded in the store. Every other kind
// is a path node and is minted under the path-node domain from the whole key.
// The key carries neither a class nor a basis, so a key the store already holds
// resolves to its recorded identifier whatever its class and basis, and a key
// it does not hold is recorded with the class the catalog gives the kind and
// the native identity basis for a key anchor, the operator slot basis for a
// configured one.
func (a *Allocator) Allocate(key driver.NodeKey) (semreg.ResourceID, error) {
	if err := key.Validate(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrIncompleteKey, err)
	}
	if a.catalog == nil {
		return "", ErrNoCatalog
	}
	entry, listed := a.catalog.CoreEntry(key.Kind)
	if !listed || entry.ResourceKind == nil || entry.ResourceKind.InstallationRoot {
		return pathNodeID(key), nil
	}
	return a.allocateLegacy(key, entry.ResourceKind.AnchorClass)
}

func (a *Allocator) allocateLegacy(key driver.NodeKey, class semreg.AnchorClass) (semreg.ResourceID, error) {
	anchorKey := AnchorKey{Kind: key.Kind}
	basis := semreg.AnchorOperatorSlot
	if key.Anchor.Key != "" {
		anchorKey.Key = key.Anchor.Key
		basis = semreg.AnchorNativeIdentity
	} else {
		anchorKey.Parent, anchorKey.Slot = key.Anchor.Parent, key.Anchor.Slot
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, recorded, err := a.lookup(anchorKey)
	if err != nil {
		return "", err
	}
	id := resourceID(anchorKey)
	if recorded {
		if entry.Superseded {
			return "", ErrAnchorSuperseded
		}
		id, class, basis = entry.Resource, entry.Class, entry.Basis
	} else if err := a.store.Commit(Supersession{Resource: id, Class: class, Previous: anchorKey, PreviousBasis: basis, PreviousParent: anchorKey.Parent, PreviousSlot: anchorKey.Slot, Next: anchorKey, NextBasis: basis, NextParent: anchorKey.Parent, NextSlot: anchorKey.Slot}); err != nil {
		return "", fmt.Errorf("%w: %w", ErrStore, err)
	}
	a.held[anchorKey] = heldAnchor{resource: id, class: class, basis: basis}
	a.kinds[id] = key.Kind
	return id, nil
}
