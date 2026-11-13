package semreg

import (
	"math/big"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	InvalidJSON                    ErrorID = "invalid_json"
	DuplicateKey                   ErrorID = "duplicate_key"
	InvalidContract                ErrorID = "invalid_contract"
	MissingMember                  ErrorID = "missing_member"
	UnknownMember                  ErrorID = "unknown_member"
	InvalidIdentifier              ErrorID = "invalid_identifier"
	InvalidDecimal                 ErrorID = "invalid_decimal"
	InvalidValue                   ErrorID = "invalid_value"
	InvalidTime                    ErrorID = "invalid_time"
	InvalidEvidence                ErrorID = "invalid_evidence"
	InvalidEnum                    ErrorID = "invalid_enum"
	BoundsExceeded                 ErrorID = "bounds_exceeded"
	NoncanonicalOrder              ErrorID = "noncanonical_order"
	DigestMismatch                 ErrorID = "digest_mismatch"
	DanglingReference              ErrorID = "dangling_reference"
	DerivationCycle                ErrorID = "derivation_cycle"
	StaleSourceEpoch               ErrorID = "stale_source_epoch"
	StaleDriverGeneration          ErrorID = "stale_driver_generation"
	SequenceConflict               ErrorID = "sequence_conflict"
	RevisionConflict               ErrorID = "revision_conflict"
	IncomparableClockEpoch         ErrorID = "incomparable_clock_epoch"
	GenerationTransitionIncomplete ErrorID = "generation_transition_incomplete"
	DefinitionOwnerConflict        ErrorID = "definition_owner_conflict"
	DefinitionOwnerMissing         ErrorID = "definition_owner_missing"
	IdentityNotQualified           ErrorID = "identity_not_qualified"
	CapabilityNotQualified         ErrorID = "capability_not_qualified"
	CapabilityUnavailable          ErrorID = "capability_unavailable"
	AuthorityMissing               ErrorID = "authority_missing"
	DeadlineExpired                ErrorID = "deadline_expired"
	PreconditionFailed             ErrorID = "precondition_failed"
	RouteSelectionForbidden        ErrorID = "route_selection_forbidden"
	AmbiguousRoute                 ErrorID = "ambiguous_route"
	RetryForbidden                 ErrorID = "retry_forbidden"
	InvalidOutcome                 ErrorID = "invalid_outcome"
	EchoSuppressed                 ErrorID = "echo_suppressed"
	CausalBudgetExceeded           ErrorID = "causal_budget_exceeded"
	ProjectionIncomplete           ErrorID = "projection_incomplete"
	AliasNotRoutable               ErrorID = "alias_not_routable"
)

type Error struct {
	ID     ErrorID
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return string(e.ID)
	}
	return string(e.ID) + ": " + e.Detail
}
func errID(id ErrorID, detail string) error { return &Error{ID: id, Detail: detail} }
func ErrorIdentifier(err error) ErrorID {
	if e, ok := err.(*Error); ok {
		return e.ID
	}
	return InvalidValue
}

var errorRanks = map[ErrorID]int{
	InvalidJSON: 1, DuplicateKey: 2, InvalidContract: 3, MissingMember: 4, UnknownMember: 5,
	InvalidIdentifier: 6, InvalidDecimal: 7, InvalidValue: 8, InvalidTime: 9, InvalidEvidence: 10,
	InvalidEnum: 11, BoundsExceeded: 12, NoncanonicalOrder: 13, DigestMismatch: 14,
	DanglingReference: 15, DerivationCycle: 16, StaleSourceEpoch: 17, StaleDriverGeneration: 18,
	SequenceConflict: 19, RevisionConflict: 20, IncomparableClockEpoch: 21,
	GenerationTransitionIncomplete: 22, DefinitionOwnerConflict: 23, DefinitionOwnerMissing: 24,
	IdentityNotQualified: 25, CapabilityNotQualified: 26, CapabilityUnavailable: 27,
	AuthorityMissing: 28, DeadlineExpired: 29, PreconditionFailed: 30,
	RouteSelectionForbidden: 31, AmbiguousRoute: 32, RetryForbidden: 33, InvalidOutcome: 34,
	EchoSuppressed: 35, CausalBudgetExceeded: 36, ProjectionIncomplete: 37, AliasNotRoutable: 38,
}

func bestError(errors ...error) error {
	var best error
	bestRank := int(^uint(0) >> 1)
	for _, candidate := range errors {
		if candidate == nil {
			continue
		}
		rank, ok := errorRanks[ErrorIdentifier(candidate)]
		if !ok {
			rank = errorRanks[InvalidValue]
		}
		if rank < bestRank {
			best, bestRank = candidate, rank
		}
	}
	return best
}

var (
	contractRE    = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._/-][a-z0-9]+)*(?:[./]v[1-9][0-9]*)$`)
	definitionRE  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	opaqueRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]*$`)
	semverRE      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	digestRE      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	errorIDRE     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	coefficientRE = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)
	u64RE         = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	i64RE         = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)
)

func validDefinition(v DefinitionID) bool {
	return len(v) >= 3 && len(v) <= 160 && definitionRE.MatchString(string(v))
}
func validContract(v ContractVersion) bool {
	return len(v) >= 1 && len(v) <= 128 && contractRE.MatchString(string(v))
}
func validOpaque(v string) bool          { return len(v) >= 1 && len(v) <= 256 && opaqueRE.MatchString(v) }
func validSemver(v SemanticVersion) bool { return semverRE.MatchString(string(v)) }
func validVersionLabel(v VersionLabel) bool {
	s := string(v)
	if len(s) < 1 || len(s) > 128 || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func u64(v Uint64) (*big.Int, bool) {
	if !u64RE.MatchString(string(v)) {
		return nil, false
	}
	n, ok := new(big.Int).SetString(string(v), 10)
	return n, ok && n.Sign() >= 0 && n.BitLen() <= 64
}
func i64(v Int64) (*big.Int, bool) {
	if !i64RE.MatchString(string(v)) {
		return nil, false
	}
	n, ok := new(big.Int).SetString(string(v), 10)
	return n, ok && n.IsInt64()
}
func positive(v Uint64) bool { n, ok := u64(v); return ok && n.Sign() > 0 }
func compareSemver(a, b SemanticVersion) (int, bool) {
	if !validSemver(a) || !validSemver(b) {
		return 0, false
	}
	ap, bp := strings.Split(string(a), "."), strings.Split(string(b), ".")
	for i := range ap {
		an, _ := new(big.Int).SetString(ap[i], 10)
		bn, _ := new(big.Int).SetString(bp[i], 10)
		if cmp := an.Cmp(bn); cmp != 0 {
			return cmp, true
		}
	}
	return 0, true
}
func compareUint64(a, b Uint64) int {
	an, _ := u64(a)
	bn, _ := u64(b)
	if an == nil || bn == nil {
		return strings.Compare(string(a), string(b))
	}
	return an.Cmp(bn)
}
func validateText(s string, min, max int, controls bool) error {
	if !utf8.ValidString(s) || !norm.NFC.IsNormalString(s) {
		return errID(InvalidValue, "text")
	}
	if len(s) < min || len(s) > max {
		return errID(BoundsExceeded, "text")
	}
	for _, r := range s {
		if unicode.IsControl(r) && (!controls || (r != '\t' && r != '\n' && r != '\r')) {
			return errID(InvalidValue, "text control")
		}
	}
	return nil
}
func duplicateAndOrder[T any](items []T, compare func(T, T) int) (bool, bool) {
	// Invalid keys do not acquire an identity through their Go zero value.
	seen := make(map[string]struct{})
	var previous T
	havePrevious, duplicate, ordered := false, false, true
	for _, item := range items {
		if collectionKeyValid(reflect.ValueOf(item)) {
			key := collectionKey(reflect.ValueOf(item))
			if _, exists := seen[key]; exists {
				duplicate = true
			}
			seen[key] = struct{}{}
			if havePrevious && compare(previous, item) > 0 {
				ordered = false
			}
			previous, havePrevious = item, true
		}
	}
	return duplicate, ordered
}

type Record interface{ Validate() error }

// ContractDiscriminator identifies the required contract-bearing member for a
// public document implemented by a versioned child package. It lets Decode
// retain its strict, pre-binding discriminator validation without creating an
// import from the protocol-neutral root package to a child package.
type ContractDiscriminator interface {
	ContractDiscriminator() (member string, expected ContractVersion)
}

// WireFixedContract declares a required child-package member whose supplied
// value must equal an exact contract version. Unlike ContractDiscriminator,
// absence remains the ordinary MissingMember error for a nested record.
type WireFixedContract interface {
	WireFixedContract() (member string, expected ContractVersion)
}

// WireCollectionIdentity declares the independently knowable key fields of a
// child-package collection element. Decode uses it only when another member
// prevents whole-record binding, so malformed non-key siblings cannot suppress
// duplicate or ordering diagnostics for an otherwise complete key.
//
// Implementations must validate only the returned key fields. They must not
// derive an identity from malformed key data or inspect non-key payload.
type WireCollectionIdentity interface {
	WireCollectionKeyFields() []string
	ValidateWireCollectionKey() error
}

func (v ContractVersion) Validate() error {
	if !validContract(v) {
		return errID(InvalidContract, "contract")
	}
	return nil
}
func (v DefinitionID) Validate() error {
	if !validDefinition(v) {
		return errID(InvalidIdentifier, "definition")
	}
	return nil
}
func (v OpaqueID) Validate() error {
	if !validOpaque(string(v)) {
		return errID(InvalidIdentifier, "opaque id")
	}
	return nil
}
func (v Digest) Validate() error {
	if !digestRE.MatchString(string(v)) {
		return errID(InvalidValue, "digest")
	}
	return nil
}
func (v ErrorID) Validate() error {
	if !errorIDRE.MatchString(string(v)) {
		return errID(InvalidValue, "error id")
	}
	if _, ok := errorRanks[v]; !ok {
		return errID(InvalidValue, "error id")
	}
	return nil
}
func (v SemanticVersion) Validate() error {
	if !validSemver(v) {
		return errID(InvalidIdentifier, "semantic version")
	}
	return nil
}
func (v VersionLabel) Validate() error {
	if !validVersionLabel(v) {
		return errID(InvalidValue, "version label")
	}
	return nil
}
func (v Uint64) Validate() error {
	if _, ok := u64(v); !ok {
		return errID(InvalidValue, "uint64")
	}
	return nil
}
func (v Int64) Validate() error {
	if _, ok := i64(v); !ok {
		return errID(InvalidValue, "int64")
	}
	return nil
}
func (r VersionRange) Validate() error {
	cmp, ok := compareSemver(r.Minimum, r.MaximumExclusive)
	var syntax error
	if !validSemver(r.Minimum) || !validSemver(r.MaximumExclusive) {
		syntax = errID(InvalidIdentifier, "version range")
	}
	var order error
	if ok && cmp >= 0 {
		order = errID(InvalidValue, "version range")
	}
	return bestError(syntax, order)
}
func (r VersionRange) Matches(v SemanticVersion) (bool, error) {
	if err := bestError(r.Validate(), v.Validate()); err != nil {
		return false, err
	}
	low, _ := compareSemver(r.Minimum, v)
	high, _ := compareSemver(v, r.MaximumExclusive)
	return low <= 0 && high < 0, nil
}

func validateOpaqueValue(v string, detail string) error {
	if !validOpaque(v) {
		return errID(InvalidIdentifier, detail)
	}
	return nil
}
func (v AssetID) Validate() error         { return validateOpaqueValue(string(v), "asset id") }
func (v SourceID) Validate() error        { return validateOpaqueValue(string(v), "source id") }
func (v SourceEpochID) Validate() error   { return validateOpaqueValue(string(v), "source epoch id") }
func (v ClockEpochID) Validate() error    { return validateOpaqueValue(string(v), "clock epoch id") }
func (v NativeBindingID) Validate() error { return validateOpaqueValue(string(v), "binding id") }
func (v CandidateID) Validate() error     { return validateOpaqueValue(string(v), "candidate id") }
func (v ConflictID) Validate() error      { return validateOpaqueValue(string(v), "conflict id") }
func (v CapabilityInstanceID) Validate() error {
	return validateOpaqueValue(string(v), "capability instance id")
}
func (v ServiceInstanceID) Validate() error {
	return validateOpaqueValue(string(v), "service instance id")
}
func (v SnapshotID) Validate() error     { return validateOpaqueValue(string(v), "snapshot id") }
func (v BatchID) Validate() error        { return validateOpaqueValue(string(v), "batch id") }
func (v IntentID) Validate() error       { return validateOpaqueValue(string(v), "intent id") }
func (v AttemptID) Validate() error      { return validateOpaqueValue(string(v), "attempt id") }
func (v OriginID) Validate() error       { return validateOpaqueValue(string(v), "origin id") }
func (v CorrelationID) Validate() error  { return validateOpaqueValue(string(v), "correlation id") }
func (v IdempotencyKey) Validate() error { return validateOpaqueValue(string(v), "idempotency key") }
func (v PolicyID) Validate() error       { return validateOpaqueValue(string(v), "policy id") }
func (v TargetID) Validate() error       { return validateOpaqueValue(string(v), "target id") }
