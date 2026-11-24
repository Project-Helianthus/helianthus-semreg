package driver

import (
	"errors"
	"fmt"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// DriverKind is the stable kind string of a driver.
type DriverKind string

// Validate requires a non-empty kind.
func (k DriverKind) Validate() error { return required("driver kind", string(k)) }

// InstanceKey is the immutable configured key of one instance of a driver. It
// is never a display name.
type InstanceKey string

// Validate requires a non-empty key.
func (k InstanceKey) Validate() error { return required("instance key", string(k)) }

// BridgeID names the bridge that homes a resource.
type BridgeID string

// Validate requires a non-empty bridge id.
func (b BridgeID) Validate() error { return required("bridge id", string(b)) }

// AnchorKey is the durable anchor of a node: either a native anchor, a
// validated scheme, issuer and token digest, or a configured anchor, a durable
// parent plus a slot. Arrival order, an epoch, a generation, a network address,
// a credential and a current value have no member here, so no key is built from
// them.
type AnchorKey struct {
	Scheme string        `json:"scheme,omitempty"`
	Issuer string        `json:"issuer,omitempty"`
	Key    v1.Digest     `json:"key,omitempty"`
	Parent v1.ResourceID `json:"parent,omitempty"`
	Slot   string        `json:"slot,omitempty"`
}

// Validate accepts exactly one of the two anchor forms. The native form needs a
// scheme and a digest, and its issuer may be empty. The configured form needs a
// slot, and its parent is empty when the anchor has none.
func (a AnchorKey) Validate() error {
	native := a.Scheme != "" || a.Issuer != "" || a.Key != ""
	configured := a.Slot != "" || a.Parent != ""
	switch {
	case native && configured:
		return errors.New("an anchor is either a scheme, issuer and digest or a parent and slot, not both")
	case native:
		return first(required("anchor scheme", a.Scheme), within("anchor key", a.Key.Validate()))
	case configured:
		if err := required("anchor slot", a.Slot); err != nil {
			return err
		}
		if a.Parent != "" {
			return within("anchor parent", a.Parent.Validate())
		}
		return nil
	}
	return errors.New("a durable anchor is required")
}

// NodeKey is the durable key of one node: installation, driver kind, instance
// key, node kind and anchor. A changed identifier is a new key; no type of this
// package expresses a succession from one identifier to another.
type NodeKey struct {
	Installation string          `json:"installation"`
	Driver       DriverKind      `json:"driver"`
	Instance     InstanceKey     `json:"instance"`
	Kind         v1.DefinitionID `json:"kind"`
	Anchor       AnchorKey       `json:"anchor"`
}

// Validate checks every member and the anchor.
func (k NodeKey) Validate() error {
	return first(
		required("installation", k.Installation), within("driver", k.Driver.Validate()),
		within("instance", k.Instance.Validate()), within("kind", k.Kind.Validate()), within("anchor", k.Anchor.Validate()),
	)
}

// NodeRef refers to a node by its bridge and resource id.
type NodeRef struct {
	Bridge BridgeID      `json:"bridge"`
	ID     v1.ResourceID `json:"id"`
}

// Validate checks the bridge and the resource id.
func (r NodeRef) Validate() error {
	return first(within("bridge", r.Bridge.Validate()), within("id", r.ID.Validate()))
}

// LinkKey identifies a directed link by its ordered tuple of type, From and To.
type LinkKey struct {
	Type v1.DefinitionRef `json:"type"`
	From NodeRef          `json:"from"`
	To   NodeRef          `json:"to"`
}

// Validate checks the type and both endpoints.
func (k LinkKey) Validate() error {
	return first(within("type", k.Type.Validate()), within("from", k.From.Validate()), within("to", k.To.Validate()))
}

// LinkClaim is one publisher's claim of a link with its basis. Each publisher's
// claim and basis stay separate under the link key.
type LinkClaim struct {
	Key   LinkKey          `json:"key"`
	Basis []v1.EvidenceRef `json:"basis"`
}

// Validate checks the key and requires a valid, non-empty basis.
func (c LinkClaim) Validate() error {
	return first(within("key", c.Key.Validate()), validateEvidence("basis", c.Basis))
}

func validateEvidence(name string, evidence []v1.EvidenceRef) error {
	if len(evidence) == 0 {
		return fmt.Errorf("%s needs at least one evidence reference", name)
	}
	for i, e := range evidence {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("%s[%d]: %w", name, i, err)
		}
	}
	return nil
}
