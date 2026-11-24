package roweval

import (
	"fmt"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Order is one reordering of a raw value of the given width, with its way back.
// The driver supplies the orders it names in a Layout.
type Order struct {
	Decode func(raw uint64, width uint16) (uint64, error)
	Encode func(raw uint64, width uint16) (uint64, error)
}

// Codec turns a raw value of the given width into an exact number and back. The
// driver supplies the codecs it names in a Layout.
type Codec struct {
	Decode func(raw uint64, width uint16) (v1.Decimal, error)
	Encode func(number v1.Decimal, width uint16) (uint64, error)
}

// Descriptors are the native codec and layout descriptors of a driver. This
// package holds no catalog of its own: an identifier a Layout names that is not
// in these maps is unknown. Extent reports the payload length, in bytes, that
// the selector geometry fixes for a row at the given position; ok is false when
// the geometry does not fix it.
type Descriptors[S any] struct {
	Codecs     map[string]Codec
	ByteOrders map[string]Order
	WordOrders map[string]Order
	BitOrders  map[string]Order
	Extent     func(at S) (length int, ok bool)
}

// Qualifiers are what the driver knows about the instance a payload or an
// argument belongs to. Values answer When.Equal, Revisions answer
// When.Versions and Features answer When.Requires and When.Forbids. A feature
// that is absent from Features is unknown.
type Qualifiers struct {
	Values    map[string]string
	Revisions map[string][]uint16
	Features  map[string]bool
}

// Receipt is when the driver received a payload.
type Receipt struct {
	ReceivedAt v1.TimePoint
	Monotonic  v1.MonotonicPoint
}

// Input is one acquired payload with its receipt.
type Input struct {
	Payload []byte
	Receipt Receipt
}

// RoleBinding is what the caller resolved a subject role to.
type RoleBinding struct {
	ServiceInstance    v1.ServiceInstanceID
	CapabilityInstance *v1.CapabilityInstanceID
}

// Roles are the caller's role bindings and the catalog definitions that the
// rows may reference.
type Roles struct {
	Subjects map[string]RoleBinding
	Catalog  []v1.DefinitionIndex
}

// Subject is the subject a binding resolved to. FactCandidate has no subject
// member, so it is returned beside the candidate.
type Subject struct {
	Role               string
	Service            v1.DefinitionRef
	Capability         v1.DefinitionRef
	ServiceInstance    v1.ServiceInstanceID
	CapabilityInstance *v1.CapabilityInstanceID
}

// DetachedInputs is the immutable set of payloads, qualifiers, receipts and
// instance coordinates a RowHook is given. It is a copy: nothing a hook does
// reaches the caller's data.
type DetachedInputs struct {
	inputs      []Input
	qualifiers  Qualifiers
	coordinates map[string]string
}

// Inputs returns a copy of the payloads and their receipts.
func (d DetachedInputs) Inputs() []Input {
	out := make([]Input, len(d.inputs))
	for i, in := range d.inputs {
		out[i] = Input{Payload: append([]byte(nil), in.Payload...), Receipt: in.Receipt}
	}
	return out
}

// Qualifiers returns a copy of the qualifiers.
func (d DetachedInputs) Qualifiers() Qualifiers { return cloneQualifiers(d.qualifiers) }

// Coordinates returns a copy of the instance coordinates.
func (d DetachedInputs) Coordinates() map[string]string { return cloneStrings(d.coordinates) }

func detach(inputs []Input, q Qualifiers, coordinates map[string]string) DetachedInputs {
	d := DetachedInputs{qualifiers: cloneQualifiers(q), coordinates: cloneStrings(coordinates)}
	d.inputs = DetachedInputs{inputs: inputs}.Inputs()
	return d
}

// Acquired is what an acquired observation supplies to Evaluate: the selector,
// payload, qualifiers, receipt and instance coordinates, with the complete
// caller-owned candidate template, the resolved subject roles and the driver's
// native descriptors. Inputs are further payloads a hook may use.
type Acquired[S any] struct {
	Selector    S
	Payload     []byte
	Qualifiers  Qualifiers
	Receipt     Receipt
	Coordinates map[string]string
	Inputs      []Input
	Template    v1.FactCandidate
	Roles       Roles
	Descriptors Descriptors[S]
}

// Writing is what Invert is given: the binding to write, the admitted argument,
// the qualifiers of the target, the limits the driver published and the
// driver's native descriptors. A non-nil Limits is the limit that applies; nil
// means the target has no limit query and only then the row's NativeBounds
// apply.
type Writing[S any] struct {
	NativeID    string
	SubjectRole string
	Operation   v1.DefinitionRef
	Argument    v1.TypedField
	Qualifiers  Qualifiers
	Coordinates map[string]string
	Inputs      []Input
	Limits      *driver.ValueLimits
	Descriptors Descriptors[S]
}

// InputErrorKind classifies an InputError.
type InputErrorKind string

// InputErrorKind values.
const (
	InputUnresolvedRole  InputErrorKind = "unresolved_role"
	InputUnresolvedSlot  InputErrorKind = "unresolved_slot"
	InputMissingMetadata InputErrorKind = "missing_metadata"
	InputPolicyMismatch  InputErrorKind = "policy_mismatch"
)

// InputError is the typed error of an input the caller did not complete. No
// candidate comes with it.
type InputError struct {
	Kind   InputErrorKind
	Detail string
	Err    error
}

func (e *InputError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("roweval: %s: %s: %v", e.Kind, e.Detail, e.Err)
	}
	return fmt.Sprintf("roweval: %s: %s", e.Kind, e.Detail)
}

// Unwrap returns the underlying validation error, if any.
func (e *InputError) Unwrap() error { return e.Err }

func cloneStrings(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneQualifiers(q Qualifiers) Qualifiers {
	out := Qualifiers{Values: cloneStrings(q.Values)}
	if q.Revisions != nil {
		out.Revisions = make(map[string][]uint16, len(q.Revisions))
		for k, v := range q.Revisions {
			out.Revisions[k] = append([]uint16(nil), v...)
		}
	}
	if q.Features != nil {
		out.Features = make(map[string]bool, len(q.Features))
		for k, v := range q.Features {
			out.Features[k] = v
		}
	}
	return out
}
