package roweval

import v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"

// VersionRange is the half-open range [Min, Before) on one version axis. A
// version is compared element by element, the shorter side padded with zeros.
type VersionRange struct {
	Axis   string
	Min    []uint16
	Before []uint16
}

// When states the conditions of a row on vendor, product, role, profile,
// hardware or API revision and instance features. A condition on something the
// observation did not supply is not met: a missing qualifier is neither a
// wildcard nor a known-false feature.
type When struct {
	Equal    map[string]string
	Versions []VersionRange
	Requires []string
	Forbids  []string
}

// Layout places one raw field in a payload. The field is the Width bits that
// start at BitOffset, where bit p of the payload is bit p%8 of byte p/8 and the
// first bit of the field is the least significant bit of the raw value. The
// BitOrder, ByteOrder and WordOrder identifiers then name the driver's
// reordering of that raw value, in that order, and Codec names how it becomes a
// number. Sentinels map a raw pattern, taken after the reordering, to a
// disposition before any scaling.
type Layout struct {
	BitOffset uint16
	Width     uint16
	Codec     string
	ByteOrder string
	WordOrder string
	BitOrder  string
	Sentinels map[uint64]string
}

// Affine states value = x × Num/Den × 10^exponent + Add. ExponentRow names the
// signed power-of-ten row of the same observation set; without it the exponent
// is zero. An Add that is the zero value of Decimal adds nothing.
type Affine struct {
	Num, Den    int64
	Add         v1.Decimal
	ExponentRow string
}

// Convert turns one value into another. A nil Numeric is the identity. Round
// names the rounding applied to a result that is not exact, and Hook names a
// RowHook declared in the table.
type Convert struct {
	Numeric *Affine
	Symbols map[string]string
	Round   string
	Hook    string
}

// Bounds are the optional minimum, maximum and step of a value.
type Bounds struct {
	Min, Max, Step *v1.Decimal
}

// NativeRow is one native value: where it is read (At, in the driver's
// selector geometry S), the conditions under which the row applies, its layout
// and its decoding, and the domain the decoded native value must lie in.
type NativeRow[S any] struct {
	ID      string
	Unit    string
	SpecRef string
	At      S
	When    When
	Wire    Layout
	Decode  Convert
	Domain  Bounds
}

// BindingRow binds the native row named by NativeID to the semantic service,
// capability, fact and operation of a subject role. A nil Write means the
// element has no semantic write.
type BindingRow struct {
	NativeID    string
	SubjectRole string
	SpecRef     string
	When        When
	Service     v1.DefinitionRef
	Capability  v1.DefinitionRef
	Read        *ReadRow
	Write       *WriteRow
}

// ReadRow states the fact a native value becomes.
type ReadRow struct {
	Fact       v1.DefinitionRef
	Dimensions map[v1.DefinitionID]string
	Unit       v1.DefinitionID
	Policy     v1.PolicyID
	ToRegistry Convert
}

// WriteRow states the native write of an operation argument.
type WriteRow struct {
	When         When
	Operation    v1.DefinitionRef
	Argument     v1.DefinitionID
	TargetRow    string
	ReadbackRow  string
	Inverse      Convert
	NativeBounds Bounds
}

// Direction is the direction a RowHook is applied in.
type Direction string

// Direction values.
const (
	Forward Direction = "forward"
	Inverse Direction = "inverse"
)

// RowID is the ID of the row a hook is registered beside.
type RowID string

// TypedValue is the value a hook is applied to.
type TypedValue = v1.Value

// Reason is a stated reason. An empty Reason returned by a hook means the hook
// produced its value; a non-empty one means it did not.
type Reason string

// DerivedValue is the value a hook derived and the citations of the inputs it
// used.
type DerivedValue struct {
	Value     v1.Value
	Citations []v1.EvidenceRef
}

// RowHook is a function for what a row cannot express. It is registered by name
// in Table.Hooks, next to the rows that use it.
type RowHook func(DetachedInputs, RowID, Direction, TypedValue) (DerivedValue, Reason)

// Table is the rows of one driver in the selector geometry S, and the hooks its
// rows name.
type Table[S any] struct {
	Native   []NativeRow[S]
	Bindings []BindingRow
	Hooks    map[string]RowHook
}
