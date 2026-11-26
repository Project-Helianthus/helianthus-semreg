package driver

import (
	"context"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// NamespaceID is the key of a driver's diagnostic namespace.
type NamespaceID string

// Validate requires a non-empty id.
func (n NamespaceID) Validate() error { return required("namespace id", string(n)) }

// NodeID is the local id of a node inside a namespace.
type NodeID string

// Validate requires a non-empty id.
func (n NodeID) Validate() error { return required("node id", string(n)) }

// MemberID is the local id of a member.
type MemberID string

// Validate requires a non-empty id.
func (m MemberID) Validate() error { return required("member id", string(m)) }

// SchemaRef names the schema of a member or method.
type SchemaRef string

// Validate requires a non-empty reference.
func (s SchemaRef) Validate() error { return required("schema", string(s)) }

// AccessMode is the access mode of a member.
type AccessMode string

// Validate requires a non-empty mode.
func (m AccessMode) Validate() error { return required("access mode", string(m)) }

// ScopeID names the scope of a member.
type ScopeID string

// Validate requires a non-empty scope.
func (s ScopeID) Validate() error { return required("scope", string(s)) }

// Namespace is a diagnostic namespace: its owning driver kind and its key.
type Namespace struct {
	Owner DriverKind  `json:"owner"`
	Key   NamespaceID `json:"key"`
}

// Validate checks the owner and the key.
func (n Namespace) Validate() error {
	return first(within("owner", n.Owner.Validate()), within("key", n.Key.Validate()))
}

// MemberKey is the one key that addresses a member in GraphQL and MCP.
type MemberKey struct {
	Namespace NamespaceID `json:"namespace"`
	Instance  InstanceKey `json:"instance"`
	Node      NodeID      `json:"node"`
	Local     MemberID    `json:"local"`
}

// Validate checks every member of the key.
func (k MemberKey) Validate() error {
	return first(within("namespace", k.Namespace.Validate()), within("instance", k.Instance.Validate()), within("node", k.Node.Validate()), within("local", k.Local.Validate()))
}

// Member is one member of a diagnostic namespace.
type Member struct {
	Key    MemberKey  `json:"key"`
	Schema SchemaRef  `json:"schema"`
	Mode   AccessMode `json:"mode"`
	Scope  ScopeID    `json:"scope"`
}

// Validate checks the key, the schema, the mode and the scope.
func (m Member) Validate() error {
	return first(within("key", m.Key.Validate()), within("schema", m.Schema.Validate()), within("mode", m.Mode.Validate()), within("scope", m.Scope.Validate()))
}

// NodeSpec declares one node of the native registry: its parent, kind, stable
// local identity, native contract and an optional semantic subject link.
type NodeSpec struct {
	Parent   *NodeRef        `json:"parent,omitempty"`
	Kind     v1.DefinitionID `json:"kind"`
	Local    NodeID          `json:"local"`
	Contract SchemaRef       `json:"contract"`
	Subject  *NodeRef        `json:"subject,omitempty"`
}

// Validate checks the kind, the local identity, the contract and the optional
// references.
func (s NodeSpec) Validate() error {
	if err := first(within("kind", s.Kind.Validate()), within("local", s.Local.Validate()), within("contract", s.Contract.Validate())); err != nil {
		return err
	}
	if s.Parent != nil {
		if err := s.Parent.Validate(); err != nil {
			return within("parent", err)
		}
	}
	if s.Subject != nil {
		return within("subject", s.Subject.Validate())
	}
	return nil
}

// MemberSpec declares one readable member of a node: its stable id, schema,
// units, mode, visibility, channel and scope, and bounds.
type MemberSpec struct {
	Local      MemberID        `json:"local"`
	Schema     SchemaRef       `json:"schema"`
	Units      v1.DefinitionID `json:"units,omitempty"`
	Mode       AccessMode      `json:"mode"`
	Visibility string          `json:"visibility,omitempty"`
	Channel    string          `json:"channel,omitempty"`
	Scope      ScopeID         `json:"scope"`
	Bounds     *ValueLimits    `json:"bounds,omitempty"`
}

// Validate checks the id, schema, mode and scope, the units when present and the
// bounds with their origin when present.
func (s MemberSpec) Validate() error {
	if err := first(within("local", s.Local.Validate()), within("schema", s.Schema.Validate()), within("mode", s.Mode.Validate()), within("scope", s.Scope.Validate())); err != nil {
		return err
	}
	if s.Units != "" {
		if err := s.Units.Validate(); err != nil {
			return within("units", err)
		}
	}
	if s.Bounds != nil {
		return within("bounds", s.Bounds.Validate())
	}
	return nil
}

// MethodSpec declares one method of a node.
type MethodSpec struct {
	Local      MemberID  `json:"local"`
	Schema     SchemaRef `json:"schema"`
	Visibility string    `json:"visibility,omitempty"`
	Scope      ScopeID   `json:"scope"`
}

// Validate checks the id, the schema and the scope.
func (s MethodSpec) Validate() error {
	return first(within("local", s.Local.Validate()), within("schema", s.Schema.Validate()), within("scope", s.Scope.Validate()))
}

// NativeReader reads the current encoding of one member, as its schema states.
type NativeReader func(context.Context) ([]byte, error)

// NativeMethod invokes one method with an encoded argument and returns its
// encoded result, as its schema states.
type NativeMethod func(context.Context, []byte) ([]byte, error)

// NativeRegistry is the instance- and generation-rooted registry of the driver's
// diagnostic namespace. Instance and transport setup are registered before
// device discovery. SemReg's runtime implements it.
type NativeRegistry interface {
	// Root is the NodeRef of the calling instance's own node.
	Root() NodeRef
	Node(NodeSpec) (NodeRef, error)
	ReadMember(NodeRef, MemberSpec, NativeReader) error
	Method(NodeRef, MethodSpec, NativeMethod) error
	View(ViewSpec) error
}
