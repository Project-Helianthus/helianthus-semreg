package semreg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
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

func validateEvidence(e EvidenceRef) error {
	var syntax error
	if !validDefinition(e.Owner) || !validDefinition(e.Kind) || !validContract(e.Contract) || !digestRE.MatchString(string(e.Digest)) {
		syntax = errID(InvalidEvidence, "evidence reference")
	}
	var enum error
	if e.Access != EvidenceAccessPublic && e.Access != EvidenceAccessAuthorized && e.Access != EvidenceAccessRestricted {
		enum = errID(InvalidEnum, "evidence access")
	}
	if e.Redaction != RedactionNone && e.Redaction != RedactionRedacted && e.Redaction != RedactionMetadataOnly {
		enum = bestError(enum, errID(InvalidEnum, "redaction"))
	}
	var access error
	if e.Access == EvidenceAccessRestricted && e.Redaction == RedactionNone {
		access = errID(InvalidEvidence, "restricted evidence")
	}
	return bestError(syntax, access, enum)
}
func (e EvidenceRef) Validate() error { return validateEvidence(e) }
func compareEvidence(a, b EvidenceRef) int {
	ak := string(a.Owner) + "\x00" + string(a.Kind) + "\x00" + string(a.Contract) + "\x00" + string(a.Digest)
	bk := string(b.Owner) + "\x00" + string(b.Kind) + "\x00" + string(b.Contract) + "\x00" + string(b.Digest)
	return strings.Compare(ak, bk)
}
func validateEvidenceSet(es []EvidenceRef, min, max int) error {
	dup, ordered := duplicateAndOrder(es, compareEvidence)
	var errs []error
	if es == nil {
		errs = append(errs, errID(MissingMember, "evidence"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "evidence"))
	}
	for _, e := range es {
		errs = append(errs, e.Validate())
	}
	if len(es) < min || len(es) > max {
		errs = append(errs, errID(BoundsExceeded, "evidence"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "evidence"))
	}
	return bestError(errs...)
}

func (s SourceDescriptor) Validate() error {
	var enum error
	if s.State != SourceCurrent && s.State != SourceRetired {
		enum = errID(InvalidEnum, "source state")
	}
	return bestError(s.SourceID.Validate(), s.SourceEpochID.Validate(), s.ProtocolID.Validate(), s.ProfileID.Validate(), s.ProfileVersion.Validate(), s.RegistryEvidence.Validate(), s.StartedAt.Validate(), func() error {
		if !positive(s.Revision) {
			return errID(InvalidIdentifier, "source revision")
		}
		return nil
	}(), enum)
}
func (o OriginRef) Validate() error {
	var errs []error
	errs = append(errs, o.OriginID.Validate(), validateEvidenceSet(o.Evidence, 1, 32))
	var enum error
	if o.Kind != OriginNativeObservation && o.Kind != OriginDerived && o.Kind != OriginOperator && o.Kind != OriginAutomation && o.Kind != OriginProjection {
		enum = errID(InvalidEnum, "origin kind")
	}
	errs = append(errs, enum)
	if o.SourceID != nil {
		errs = append(errs, o.SourceID.Validate())
	}
	if o.SourceEpochID != nil {
		errs = append(errs, o.SourceEpochID.Validate())
	}
	if o.BindingID != nil {
		errs = append(errs, o.BindingID.Validate())
	}
	if o.Kind == OriginNativeObservation {
		if o.SourceID == nil || o.SourceEpochID == nil || o.BindingID == nil {
			errs = append(errs, errID(MissingMember, "native origin path"))
		}
	} else if o.SourceID != nil || o.SourceEpochID != nil || o.BindingID != nil {
		errs = append(errs, errID(InvalidValue, "non-native origin path"))
	}
	return bestError(errs...)
}
func (b NativeBinding) Validate() error {
	var enum error
	if b.State != BindingCurrent && b.State != BindingFenced && b.State != BindingRetired {
		enum = errID(InvalidEnum, "binding state")
	}
	return bestError(b.BindingID.Validate(), b.AssetID.Validate(), b.SourceID.Validate(), b.SourceEpochID.Validate(), func() error {
		if !positive(b.DriverGeneration) || !positive(b.Revision) {
			return errID(InvalidIdentifier, "native binding")
		}
		return nil
	}(), b.NativeResource.Validate(), enum)
}
func (i IdentityLink) Validate() error {
	var enum error
	if i.State != LinkCandidate && i.State != LinkQualified && i.State != LinkRejected && i.State != LinkConflict && i.State != LinkWithdrawn {
		enum = errID(InvalidEnum, "link state")
	}
	var proof error
	if len(i.Basis) == 0 {
		proof = errID(IdentityNotQualified, "identity basis")
	} else {
		proof = validateEvidenceSet(i.Basis, 1, 32)
	}
	return bestError(i.AssetID.Validate(), i.BindingID.Validate(), func() error {
		if !positive(i.Revision) {
			return errID(InvalidIdentifier, "identity revision")
		}
		return nil
	}(), enum, proof)
}
func (p SourcePathRef) Validate() error {
	return bestError(p.BindingID.Validate(), p.SourceID.Validate(), p.SourceEpochID.Validate(), func() error {
		if !positive(p.DriverGeneration) {
			return errID(InvalidIdentifier, "driver generation")
		}
		return nil
	}())
}
func compareSourcePath(a, b SourcePathRef) int {
	if c := strings.Compare(string(a.SourceID), string(b.SourceID)); c != 0 {
		return c
	}
	if c := strings.Compare(string(a.SourceEpochID), string(b.SourceEpochID)); c != 0 {
		return c
	}
	if c := compareUint64(a.DriverGeneration, b.DriverGeneration); c != 0 {
		return c
	}
	return strings.Compare(string(a.BindingID), string(b.BindingID))
}
func (d DerivationInput) Validate() error {
	dup, ordered := duplicateAndOrder(d.SourcePaths, compareSourcePath)
	var errs []error
	if d.SourcePaths == nil {
		errs = append(errs, errID(MissingMember, "source paths"))
	}
	errs = append(errs, d.CandidateID.Validate())
	if !positive(d.CandidateRevision) {
		errs = append(errs, errID(InvalidIdentifier, "candidate revision"))
	}
	for _, p := range d.SourcePaths {
		errs = append(errs, p.Validate())
	}
	if len(d.SourcePaths) < 1 || len(d.SourcePaths) > 32 {
		errs = append(errs, errID(BoundsExceeded, "source paths"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "source paths"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "source paths"))
	}
	return bestError(errs...)
}
func (d Derivation) Validate() error {
	dup, ordered := duplicateAndOrder(d.Inputs, func(a, b DerivationInput) int { return strings.Compare(string(a.CandidateID), string(b.CandidateID)) })
	var errs []error
	if d.Inputs == nil {
		errs = append(errs, errID(MissingMember, "derivation inputs"))
	}
	errs = append(errs, d.Algorithm.Validate(), d.Version.Validate())
	for _, in := range d.Inputs {
		errs = append(errs, in.Validate())
	}
	if len(d.Inputs) < 1 || len(d.Inputs) > 32 {
		errs = append(errs, errID(BoundsExceeded, "derivation inputs"))
	}
	if dup {
		errs = append(errs, errID(DerivationCycle, "derivation input"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "derivation inputs"))
	}
	errs = append(errs, validateEvidenceSet(d.Evidence, 0, 32))
	return bestError(errs...)
}

func (d Decimal) Validate() error {
	if !coefficientRE.MatchString(d.Coefficient) || d.Exponent10 < -18 || d.Exponent10 > 18 || (d.Coefficient == "0" && d.Exponent10 != 0) || (d.Coefficient != "0" && strings.HasSuffix(d.Coefficient, "0")) {
		return errID(InvalidDecimal, "decimal")
	}
	return nil
}
func (s Symbol) Validate() error {
	var value error
	if s.Token == "" {
		value = errID(InvalidValue, "symbol token")
	} else if err := validateText(s.Token, 1, 256, false); err != nil {
		value = err
	}
	if strings.HasPrefix(string(s.Namespace), "native.") {
		if s.Known {
			value = bestError(value, errID(InvalidValue, "known native symbol"))
		}
	} else if !s.Known {
		value = bestError(value, errID(InvalidValue, "unknown symbol namespace"))
	}
	return bestError(s.Namespace.Validate(), value)
}
func (q Quantity) Validate() error { return bestError(q.Number.Validate(), q.Unit.Validate()) }
func (v Value) Validate() error {
	var errs []error
	count := 0
	if v.Quantity != nil {
		count++
		errs = append(errs, v.Quantity.Validate())
	}
	if v.Boolean != nil {
		count++
	}
	if v.Text != nil {
		count++
		errs = append(errs, validateText(*v.Text, 0, 4096, true))
	}
	if v.Symbol != nil {
		count++
		errs = append(errs, v.Symbol.Validate())
	}
	if v.Symbols != nil {
		count++
		errs = append(errs, validateSymbols(v.Symbols))
	}
	if v.Time != nil {
		count++
		errs = append(errs, v.Time.Validate())
	}
	if count != 1 {
		errs = append(errs, errID(InvalidValue, "value payload"))
	}
	switch v.Kind {
	case ValueQuantity:
		if v.Quantity == nil {
			errs = append(errs, errID(InvalidValue, "quantity payload"))
		}
	case ValueBoolean:
		if v.Boolean == nil {
			errs = append(errs, errID(InvalidValue, "boolean payload"))
		}
	case ValueText:
		if v.Text == nil {
			errs = append(errs, errID(InvalidValue, "text payload"))
		}
	case ValueSymbol:
		if v.Symbol == nil {
			errs = append(errs, errID(InvalidValue, "symbol payload"))
		}
	case ValueSymbols:
		if v.Symbols == nil {
			errs = append(errs, errID(InvalidValue, "symbols payload"))
		}
	case ValueTime:
		if v.Time == nil {
			errs = append(errs, errID(InvalidValue, "time payload"))
		}
	default:
		errs = append(errs, errID(InvalidEnum, "value kind"))
	}
	return bestError(errs...)
}

func validateSymbols(symbols []Symbol) error {
	var errs []error
	dup, ordered := duplicateAndOrder(symbols, func(a, b Symbol) int {
		if c := strings.Compare(string(a.Namespace), string(b.Namespace)); c != 0 {
			return c
		}
		return strings.Compare(a.Token, b.Token)
	})
	for _, s := range symbols {
		errs = append(errs, s.Validate())
	}
	if len(symbols) < 1 || len(symbols) > 64 {
		errs = append(errs, errID(BoundsExceeded, "symbols"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "symbols"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "symbols"))
	}
	return bestError(errs...)
}
func (d Dimension) Validate() error {
	var kind error
	if d.Value.Kind != ValueBoolean && d.Value.Kind != ValueText && d.Value.Kind != ValueSymbol && d.Value.Kind != ValueQuantity {
		kind = errID(InvalidValue, "dimension kind")
	}
	return bestError(d.ID.Validate(), d.Value.Validate(), kind)
}
func (k FactKey) Validate() error {
	dup, ordered := duplicateAndOrder(k.Dimensions, func(a, b Dimension) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	var errs []error
	if k.Dimensions == nil {
		errs = append(errs, errID(MissingMember, "dimensions"))
	}
	errs = append(errs, k.PackID.Validate(), k.PackVersion.Validate(), k.FactID.Validate())
	for _, d := range k.Dimensions {
		errs = append(errs, d.Validate())
	}
	if len(k.Dimensions) > 16 {
		errs = append(errs, errID(BoundsExceeded, "dimensions"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "dimensions"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "dimensions"))
	}
	return bestError(errs...)
}

func (t TimePoint) Validate() error {
	var timeErr error
	if _, ok := i64(t.UnixNanoseconds); !ok {
		timeErr = errID(InvalidTime, "unix nanoseconds")
	}
	if _, ok := u64(t.UncertaintyNS); !ok {
		timeErr = bestError(timeErr, errID(InvalidTime, "uncertainty"))
	}
	return bestError(t.ClockID.Validate(), timeErr)
}
func (m MonotonicPoint) Validate() error {
	var timeErr error
	if _, ok := u64(m.Nanoseconds); !ok {
		timeErr = errID(InvalidTime, "monotonic")
	}
	return bestError(m.ClockEpochID.Validate(), timeErr)
}
func (t Times) Validate() error {
	var errs []error
	errs = append(errs, t.ReceivedAt.Validate(), t.ReceiptMonotonic.Validate(), t.EvaluatedAt.Validate(), t.EvaluateMonotonic.Validate())
	if t.PhenomenonAt != nil {
		errs = append(errs, t.PhenomenonAt.Validate())
	}
	if t.SourceAt != nil {
		errs = append(errs, t.SourceAt.Validate())
	}
	if t.ReceiptMonotonic.ClockEpochID == t.EvaluateMonotonic.ClockEpochID {
		r, rok := u64(t.ReceiptMonotonic.Nanoseconds)
		e, eok := u64(t.EvaluateMonotonic.Nanoseconds)
		if rok && eok && e.Cmp(r) < 0 {
			errs = append(errs, errID(InvalidTime, "monotonic order"))
		}
	}
	return bestError(errs...)
}

// ElapsedMonotonicNS returns a same-epoch elapsed interval. It never compares
// numeric ticks from different clock epochs.
func (t Times) ElapsedMonotonicNS() (Uint64, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	if t.ReceiptMonotonic.ClockEpochID != t.EvaluateMonotonic.ClockEpochID {
		return "", errID(IncomparableClockEpoch, "monotonic epoch")
	}
	received, _ := u64(t.ReceiptMonotonic.Nanoseconds)
	evaluated, _ := u64(t.EvaluateMonotonic.Nanoseconds)
	return Uint64(new(big.Int).Sub(evaluated, received).String()), nil
}
func (p FreshnessPolicy) Validate() error {
	var errs []error
	errs = append(errs, p.PolicyID.Validate(), p.Version.Validate())
	f, fok := u64(p.FreshForNS)
	r, rok := u64(p.RetainForNS)
	_, uok := u64(p.MaxWallUncertaintyNS)
	if !fok || !rok || !uok || f.Sign() <= 0 || (fok && rok && r.Cmp(f) <= 0) {
		errs = append(errs, errID(InvalidTime, "freshness policy"))
	}
	return bestError(errs...)
}
func (q Quality) Validate() error {
	dup, ordered := duplicateAndOrder(q.Reasons, func(a, b DefinitionID) int { return strings.Compare(string(a), string(b)) })
	var errs []error
	if q.Reasons == nil {
		errs = append(errs, errID(MissingMember, "reasons"))
	}
	for _, r := range q.Reasons {
		errs = append(errs, r.Validate())
	}
	if q.Assertion != AssertionObserved && q.Assertion != AssertionInferred {
		errs = append(errs, errID(InvalidEnum, "assertion"))
	}
	if q.Qualification != QualificationCandidate && q.Qualification != QualificationQualified && q.Qualification != QualificationUnsupported && q.Qualification != QualificationUnknown && q.Qualification != QualificationRejected {
		errs = append(errs, errID(InvalidEnum, "qualification"))
	}
	if q.Promotion != PromotionUnpromoted && q.Promotion != PromotionPromoted {
		errs = append(errs, errID(InvalidEnum, "promotion"))
	}
	if q.Validity != ValidityGood && q.Validity != ValiditySuspect && q.Validity != ValidityBad && q.Validity != ValidityUnknown {
		errs = append(errs, errID(InvalidEnum, "validity"))
	}
	if q.Availability != AvailabilityAvailable && q.Availability != AvailabilityDegraded && q.Availability != AvailabilityUnavailable && q.Availability != AvailabilityWithdrawn {
		errs = append(errs, errID(InvalidEnum, "availability"))
	}
	if q.Freshness != FreshnessFresh && q.Freshness != FreshnessStale && q.Freshness != FreshnessExpired && q.Freshness != FreshnessUnknown {
		errs = append(errs, errID(InvalidEnum, "freshness"))
	}
	if q.Promotion == PromotionPromoted && (q.Qualification != QualificationQualified || (q.Validity != ValidityGood && q.Validity != ValiditySuspect) || (q.Availability != AvailabilityAvailable && q.Availability != AvailabilityDegraded)) {
		errs = append(errs, errID(InvalidValue, "promotion"))
	}
	if len(q.Reasons) > 16 {
		errs = append(errs, errID(BoundsExceeded, "reasons"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "reasons"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "reasons"))
	}
	return bestError(errs...)
}
func (c CausalContext) Validate() error {
	var errs []error
	if c.Path == nil {
		errs = append(errs, errID(MissingMember, "causal path"))
	}
	errs = append(errs, c.Origin.Validate(), c.CorrelationID.Validate(), c.FirstSeenAt.Validate(), c.ExpiresAt.Validate())
	if c.ParentCorrelationID != nil {
		errs = append(errs, c.ParentCorrelationID.Validate())
	}
	for _, target := range c.Path {
		errs = append(errs, target.Validate())
	}
	dup, _ := duplicateAndOrder(c.Path, func(a, b TargetID) int { return strings.Compare(string(a), string(b)) })
	errs = append(errs, causalTimeErrors(&c.FirstSeenAt, &c.ExpiresAt)...)
	if c.MaxHops < 1 || c.MaxHops > 16 || c.HopCount > c.MaxHops || int(c.HopCount) != len(c.Path) || dup {
		errs = append(errs, errID(CausalBudgetExceeded, "causal path"))
	}
	return bestError(errs...)
}

// Shared by typed validation and partial wire validation: time rules depend
// only on the supplied time points, never on whether hop counters can bind.
func causalTimeErrors(firstPoint, expiresPoint *TimePoint) []error {
	var errs []error
	if firstPoint != nil && firstPoint.ClockID != "clock.utc" || expiresPoint != nil && expiresPoint.ClockID != "clock.utc" {
		errs = append(errs, errID(InvalidTime, "causal clock"))
	}
	if firstPoint == nil || expiresPoint == nil {
		return errs
	}
	if first, ok1 := i64(firstPoint.UnixNanoseconds); ok1 {
		if expires, ok2 := i64(expiresPoint.UnixNanoseconds); ok2 {
			delta := new(big.Int).Sub(expires, first)
			limit := big.NewInt(300_000_000_000)
			if delta.Sign() < 0 {
				errs = append(errs, errID(InvalidTime, "causal order"))
			} else if delta.Cmp(limit) > 0 {
				errs = append(errs, errID(CausalBudgetExceeded, "causal lifetime"))
			}
		}
	}
	return errs
}
func (c FactCandidate) Validate() error {
	// Presence determines member validation; assertion and origin determine only
	// relationships. Forbidden data still contributes its own stable errors.
	errs := c.presentMemberErrors()
	errs = append(errs, c.CandidateID.Validate(), c.Key.Validate(), c.Quality.Validate(), c.Times.Validate(), c.FreshnessPolicy.Validate(), c.Origin.Validate(), validateEvidenceSet(c.Evidence, 1, 32))
	if !positive(c.Revision) {
		errs = append(errs, errID(InvalidIdentifier, "candidate revision"))
	}
	valueForbidden := c.Quality.Qualification == QualificationUnsupported || c.Quality.Qualification == QualificationRejected || c.Quality.Availability == AvailabilityWithdrawn
	if valueForbidden && c.Value != nil {
		errs = append(errs, errID(InvalidValue, "forbidden candidate value"))
	}
	if !valueForbidden && c.Value == nil {
		errs = append(errs, errID(MissingMember, "candidate value"))
	}
	if c.Quality.Assertion == AssertionInferred {
		if c.BindingID != nil || c.SourceEpochID != nil || c.DriverGeneration != nil {
			errs = append(errs, errID(InvalidValue, "inferred source path"))
		}
		if c.Derivation == nil {
			errs = append(errs, errID(MissingMember, "derivation"))
		}
	}
	if c.Quality.Assertion == AssertionObserved {
		if c.Derivation != nil {
			errs = append(errs, errID(InvalidValue, "observed derivation"))
		}
		if c.BindingID == nil || c.SourceEpochID == nil || c.DriverGeneration == nil {
			errs = append(errs, errID(MissingMember, "observed source path"))
		}
	}
	if c.Origin.Kind == OriginDerived && c.Derivation == nil {
		errs = append(errs, errID(MissingMember, "derived origin derivation"))
	}
	if c.Origin.Kind == OriginNativeObservation {
		if c.BindingID == nil || c.SourceEpochID == nil || c.DriverGeneration == nil {
			errs = append(errs, errID(MissingMember, "native observation path"))
		} else {
			if c.BindingID != nil && c.Origin.BindingID != nil && *c.BindingID != *c.Origin.BindingID {
				errs = append(errs, errID(InvalidValue, "origin binding"))
			}
			if c.SourceEpochID != nil && c.Origin.SourceEpochID != nil && *c.SourceEpochID != *c.Origin.SourceEpochID {
				errs = append(errs, errID(InvalidValue, "origin epoch"))
			}
		}
	}
	if c.Origin.Kind == OriginProjection && c.Causal == nil {
		errs = append(errs, errID(MissingMember, "projection causal context"))
	}
	return bestError(errs...)
}

func (c FactCandidate) presentMemberErrors() []error {
	var errs []error
	if c.Value != nil {
		errs = append(errs, c.Value.Validate())
	}
	if c.Causal != nil {
		errs = append(errs, c.Causal.Validate())
	}
	if c.BindingID != nil {
		errs = append(errs, c.BindingID.Validate())
	}
	if c.SourceEpochID != nil {
		errs = append(errs, c.SourceEpochID.Validate())
	}
	if c.DriverGeneration != nil && !positive(*c.DriverGeneration) {
		errs = append(errs, errID(InvalidIdentifier, "driver generation"))
	}
	if c.Derivation != nil {
		errs = append(errs, c.Derivation.Validate())
		for _, in := range c.Derivation.Inputs {
			if in.CandidateID == c.CandidateID {
				errs = append(errs, errID(DerivationCycle, "self reference"))
			}
		}
	}
	return errs
}

func (p PackRef) Validate() error { return bestError(p.ID.Validate(), p.Version.Validate()) }
func (d DefinitionRef) Validate() error {
	return bestError(d.Pack.Validate(), d.ID.Validate(), d.Version.Validate())
}
func compareDefinition(a, b DefinitionRef) int {
	if c := strings.Compare(string(a.ID), string(b.ID)); c != 0 {
		return c
	}
	if c, ok := compareSemver(a.Version, b.Version); ok {
		return c
	}
	return strings.Compare(string(a.Version), string(b.Version))
}
func (d DefinitionIndex) Validate() error {
	return bestError(d.validationErrors(DuplicateKey)...)
}

func (d DefinitionIndex) validationErrors(duplicateClass ErrorID) []error {
	var errs []error
	errs = append(errs, d.Pack.Validate())
	for _, group := range [][]DefinitionRef{d.Fields, d.Services, d.Capabilities, d.Operations, d.EffectRules} {
		if group == nil {
			errs = append(errs, errID(MissingMember, "definition collection"))
		}
		dup, ordered := duplicateAndOrder(group, compareDefinition)
		for _, ref := range group {
			errs = append(errs, ref.Validate())
			if ref.Pack != d.Pack {
				errs = append(errs, errID(DefinitionOwnerConflict, "definition index pack"))
			}
		}
		if dup {
			errs = append(errs, errID(duplicateClass, "definitions"))
		}
		if !ordered {
			errs = append(errs, errID(NoncanonicalOrder, "definitions"))
		}
	}
	return errs
}
func (f TypedField) Validate() error { return bestError(f.ID.Validate(), f.Value.Validate()) }
func (p PredicateOp) Validate() error {
	if p != PredicateEqual && p != PredicateNotEqual && p != PredicateLess && p != PredicateLessEqual && p != PredicateGreater && p != PredicateGreaterEqual && p != PredicateContains {
		return errID(InvalidEnum, "predicate")
	}
	return nil
}
func (s ServiceInstance) Validate() error {
	var enum error
	if s.Qualification != QualificationCandidate && s.Qualification != QualificationQualified && s.Qualification != QualificationUnsupported && s.Qualification != QualificationUnknown && s.Qualification != QualificationRejected {
		enum = errID(InvalidEnum, "service qualification")
	}
	if s.Availability != AvailabilityAvailable && s.Availability != AvailabilityDegraded && s.Availability != AvailabilityUnavailable && s.Availability != AvailabilityWithdrawn {
		enum = bestError(enum, errID(InvalidEnum, "service availability"))
	}
	var numeric error
	if !positive(s.DriverGeneration) || !positive(s.Revision) {
		numeric = errID(InvalidIdentifier, "service generation/revision")
	}
	return bestError(s.InstanceID.Validate(), s.AssetID.Validate(), s.Definition.Validate(), s.BindingID.Validate(), s.SourceEpochID.Validate(), numeric, enum)
}
func (c CapabilityInstance) Validate() error {
	var errs []error
	if c.Constraints == nil {
		errs = append(errs, errID(MissingMember, "constraints"))
	}
	errs = append(errs, c.InstanceID.Validate(), c.AssetID.Validate(), c.ServiceInstance.Validate(), c.Definition.Validate(), c.BindingID.Validate(), c.SourceEpochID.Validate())
	if !positive(c.DriverGeneration) || !positive(c.Revision) {
		errs = append(errs, errID(InvalidIdentifier, "capability generation/revision"))
	}
	if c.Qualification != QualificationCandidate && c.Qualification != QualificationQualified && c.Qualification != QualificationUnsupported && c.Qualification != QualificationUnknown && c.Qualification != QualificationRejected {
		errs = append(errs, errID(InvalidEnum, "capability qualification"))
	}
	if c.Availability != AvailabilityAvailable && c.Availability != AvailabilityDegraded && c.Availability != AvailabilityUnavailable && c.Availability != AvailabilityWithdrawn {
		errs = append(errs, errID(InvalidEnum, "capability availability"))
	}
	dup, ordered := duplicateAndOrder(c.Constraints, func(a, b TypedField) int { return strings.Compare(string(a.ID), string(b.ID)) })
	for _, field := range c.Constraints {
		errs = append(errs, field.Validate())
	}
	if len(c.Constraints) > 64 {
		errs = append(errs, errID(BoundsExceeded, "constraints"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "constraints"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "constraints"))
	}
	if len(c.ActivationEvidence) == 0 {
		errs = append(errs, errID(CapabilityNotQualified, "activation evidence"))
	} else {
		errs = append(errs, validateEvidenceSet(c.ActivationEvidence, 1, 32))
	}
	return bestError(errs...)
}

type DefinitionKind string

const (
	DefinitionField      DefinitionKind = "field"
	DefinitionService    DefinitionKind = "service"
	DefinitionCapability DefinitionKind = "capability"
	DefinitionOperation  DefinitionKind = "operation"
	DefinitionEffectRule DefinitionKind = "effect_rule"
)

type definitionKey struct {
	kind    DefinitionKind
	id      DefinitionID
	version SemanticVersion
}
type Registry struct {
	validators map[PackRef]PackValidator
	indexes    map[PackRef]DefinitionIndex
	owners     map[definitionKey]PackRef
}
type frozenValidator struct {
	hook  PackValidator
	index DefinitionIndex
}

func (v frozenValidator) Pack() PackRef                { return v.index.Pack }
func (v frozenValidator) Definitions() DefinitionIndex { return cloneIndex(v.index) }
func (v frozenValidator) ValidateFact(k FactKey, value *Value) error {
	return v.hook.ValidateFact(k, value)
}
func (v frozenValidator) ValidateService(s ServiceInstance) error { return v.hook.ValidateService(s) }
func (v frozenValidator) ValidateCapability(c CapabilityInstance) error {
	return v.hook.ValidateCapability(c)
}
func (v frozenValidator) ValidateField(r DefinitionRef, f TypedField) error {
	return v.hook.ValidateField(r, f)
}
func (v frozenValidator) MatchConstraints(c CapabilityInstance, f []TypedField) error {
	return v.hook.MatchConstraints(c, f)
}
func (v frozenValidator) EvaluatePredicate(c FactCandidate, p PredicateOp, value Value) (bool, error) {
	return v.hook.EvaluatePredicate(c, p, value)
}
func cloneRefs(in []DefinitionRef) []DefinitionRef {
	if in == nil {
		return nil
	}
	return append(make([]DefinitionRef, 0, len(in)), in...)
}
func cloneIndex(i DefinitionIndex) DefinitionIndex {
	i.Fields = cloneRefs(i.Fields)
	i.Services = cloneRefs(i.Services)
	i.Capabilities = cloneRefs(i.Capabilities)
	i.Operations = cloneRefs(i.Operations)
	i.EffectRules = cloneRefs(i.EffectRules)
	return i
}
func NewRegistry(validators ...PackValidator) (*Registry, error) {
	r := &Registry{validators: map[PackRef]PackValidator{}, indexes: map[PackRef]DefinitionIndex{}, owners: map[definitionKey]PackRef{}}
	var errs []error
	for _, hook := range validators {
		if hook == nil || reflect.ValueOf(hook).Kind() == reflect.Ptr && reflect.ValueOf(hook).IsNil() {
			errs = append(errs, errID(DefinitionOwnerConflict, "nil validator"))
			continue
		}
		pack := hook.Pack()
		index := cloneIndex(hook.Definitions())
		// Classify the complete diagnostic set in registration context before
		// ranking: a later ownership conflict cannot hide an earlier bad ID.
		errs = append(errs, pack.Validate())
		errs = append(errs, index.validationErrors(DefinitionOwnerConflict)...)
		if index.Pack != pack {
			errs = append(errs, errID(DefinitionOwnerConflict, "validator pack mismatch"))
		}
		if _, exists := r.validators[pack]; exists {
			errs = append(errs, errID(DefinitionOwnerConflict, "duplicate pack"))
			continue
		}
		frozen := frozenValidator{hook: hook, index: index}
		r.validators[pack] = frozen
		r.indexes[pack] = index
		groups := []struct {
			kind DefinitionKind
			refs []DefinitionRef
		}{{DefinitionField, index.Fields}, {DefinitionService, index.Services}, {DefinitionCapability, index.Capabilities}, {DefinitionOperation, index.Operations}, {DefinitionEffectRule, index.EffectRules}}
		for _, group := range groups {
			for _, ref := range group.refs {
				if ref.Validate() != nil {
					continue
				}
				key := definitionKey{group.kind, ref.ID, ref.Version}
				if owner, exists := r.owners[key]; exists && owner != ref.Pack {
					errs = append(errs, errID(DefinitionOwnerConflict, "definition owner"))
				} else if exists {
					errs = append(errs, errID(DefinitionOwnerConflict, "duplicate definition"))
				} else {
					r.owners[key] = ref.Pack
				}
			}
		}
	}
	if err := bestError(errs...); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Registry) Validator(pack PackRef) (PackValidator, error) {
	if r == nil {
		return nil, errID(DefinitionOwnerMissing, "registry")
	}
	v, ok := r.validators[pack]
	if !ok {
		return nil, errID(DefinitionOwnerMissing, "pack")
	}
	return v, nil
}
func (r *Registry) Definition(kind DefinitionKind, ref DefinitionRef) (PackValidator, error) {
	if r == nil {
		return nil, errID(DefinitionOwnerMissing, "registry")
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	owner, ok := r.owners[definitionKey{kind, ref.ID, ref.Version}]
	if !ok || owner != ref.Pack {
		return nil, errID(DefinitionOwnerMissing, "definition")
	}
	return r.Validator(ref.Pack)
}
func (r *Registry) ValidateFact(key FactKey, value *Value) error {
	keyErr := key.Validate()
	var valueErr error
	if value != nil {
		valueErr = value.Validate()
	}
	if err := bestError(keyErr, valueErr); err != nil {
		return err
	}
	validator, err := r.Validator(PackRef{ID: key.PackID, Version: key.PackVersion})
	if err != nil {
		return err
	}
	return validator.ValidateFact(key, value)
}
func (r *Registry) ValidateService(service ServiceInstance) error {
	if err := service.Validate(); err != nil {
		return err
	}
	validator, err := r.Definition(DefinitionService, service.Definition)
	if err != nil {
		return err
	}
	return validator.ValidateService(service)
}
func (r *Registry) ValidateCapability(capability CapabilityInstance) error {
	if err := capability.Validate(); err != nil {
		return err
	}
	validator, err := r.Definition(DefinitionCapability, capability.Definition)
	if err != nil {
		return err
	}
	return validator.ValidateCapability(capability)
}
func (r *Registry) ValidateFactCandidate(candidate FactCandidate) error {
	if err := candidate.Validate(); err != nil {
		return err
	}
	return r.ValidateFact(candidate.Key, candidate.Value)
}
func (r *Registry) ValidateField(ref DefinitionRef, field TypedField) error {
	if err := bestError(ref.Validate(), field.Validate()); err != nil {
		return err
	}
	if ref.ID != field.ID {
		return errID(InvalidValue, "field definition")
	}
	validator, err := r.Definition(DefinitionField, ref)
	if err != nil {
		return err
	}
	return validator.ValidateField(ref, field)
}

func (c Conflict) Validate() error {
	dup, ordered := duplicateAndOrder(c.Candidates, func(a, b CandidateID) int { return strings.Compare(string(a), string(b)) })
	var errs []error
	errs = append(errs, c.ConflictID.Validate())
	for _, id := range c.Candidates {
		errs = append(errs, id.Validate())
	}
	if c.Kind != ConflictValue {
		errs = append(errs, errID(InvalidEnum, "conflict kind"))
	}
	if c.State != ConflictOpen {
		errs = append(errs, errID(InvalidEnum, "conflict state"))
	}
	if len(c.Candidates) < 2 || len(c.Candidates) > 32 {
		errs = append(errs, errID(BoundsExceeded, "conflict candidates"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "conflict candidates"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "conflict candidates"))
	}
	errs = append(errs, validateEvidenceSet(c.Evidence, 1, 1024))
	return bestError(errs...)
}
func sameFactKey(a, b FactKey) bool {
	ab, ea := CanonicalJSON(a)
	bb, eb := CanonicalJSON(b)
	return ea == nil && eb == nil && bytes.Equal(ab, bb)
}
func deriveConflicts(asset AssetID, key FactKey, candidates []FactCandidate) ([]Conflict, error) {
	eligible := make([]FactCandidate, 0, len(candidates))
	values := map[string]struct{}{}
	evidence := []EvidenceRef{}
	for _, candidate := range candidates {
		if candidate.Value == nil || candidate.Quality.Qualification != QualificationQualified || candidate.Quality.Promotion != PromotionPromoted {
			continue
		}
		canonical, err := CanonicalJSON(*candidate.Value)
		if err != nil {
			return nil, err
		}
		values[string(canonical)] = struct{}{}
		eligible = append(eligible, candidate)
		evidence = append(evidence, candidate.Evidence...)
	}
	if len(values) < 2 {
		return []Conflict{}, nil
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].CandidateID < eligible[j].CandidateID })
	ids := make([]CandidateID, len(eligible))
	for i, candidate := range eligible {
		ids[i] = candidate.CandidateID
	}
	sort.Slice(evidence, func(i, j int) bool { return compareEvidence(evidence[i], evidence[j]) < 0 })
	dedup := evidence[:0]
	for _, item := range evidence {
		if len(dedup) == 0 || compareEvidence(dedup[len(dedup)-1], item) != 0 {
			dedup = append(dedup, item)
		}
	}
	identity := struct {
		Contract   ContractVersion `json:"contract"`
		AssetID    AssetID         `json:"asset_id"`
		Key        FactKey         `json:"key"`
		Kind       ConflictKind    `json:"kind"`
		Candidates []CandidateID   `json:"candidates"`
	}{"helianthus.semantic.conflict-id/v1", asset, key, ConflictValue, ids}
	raw, err := json.Marshal(identity)
	if err != nil {
		return nil, errID(InvalidJSON, "conflict id")
	}
	canonical, err := canonicalize(raw)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonical)
	return []Conflict{{ConflictID: ConflictID("sha256:" + hex.EncodeToString(sum[:])), Kind: ConflictValue, Candidates: ids, Evidence: append([]EvidenceRef(nil), dedup...), State: ConflictOpen}}, nil
}
func (f FactEnvelope) Validate() error {
	dup, ordered := duplicateAndOrder(f.Candidates, func(a, b FactCandidate) int { return strings.Compare(string(a.CandidateID), string(b.CandidateID)) })
	var errs []error
	if f.Candidates == nil {
		errs = append(errs, errID(MissingMember, "candidates"))
	}
	if f.Conflicts == nil {
		errs = append(errs, errID(MissingMember, "conflicts"))
	}
	errs = append(errs, f.AssetID.Validate(), f.Key.Validate())
	if !positive(f.Revision) {
		errs = append(errs, errID(InvalidIdentifier, "fact revision"))
	}
	for _, candidate := range f.Candidates {
		errs = append(errs, candidate.Validate())
		if !sameFactKey(candidate.Key, f.Key) {
			errs = append(errs, errID(InvalidValue, "candidate key"))
		}
	}
	if len(f.Candidates) < 1 || len(f.Candidates) > 32 {
		errs = append(errs, errID(BoundsExceeded, "candidates"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "candidates"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "candidates"))
	}
	cdup, cordered := duplicateAndOrder(f.Conflicts, func(a, b Conflict) int { return strings.Compare(string(a.ConflictID), string(b.ConflictID)) })
	for _, conflict := range f.Conflicts {
		errs = append(errs, conflict.Validate())
	}
	if cdup {
		errs = append(errs, errID(DuplicateKey, "conflicts"))
	}
	if !cordered {
		errs = append(errs, errID(NoncanonicalOrder, "conflicts"))
	}
	expected, deriveErr := deriveConflicts(f.AssetID, f.Key, f.Candidates)
	errs = append(errs, deriveErr)
	if deriveErr == nil && !reflect.DeepEqual(expected, f.Conflicts) {
		errs = append(errs, errID(InvalidValue, "derived conflicts"))
	}
	return bestError(errs...)
}

type jsonNode struct {
	scalar any
	object []jsonMember
	array  []jsonNode
	kind   byte
}
type jsonMember struct {
	key   string
	value jsonNode
}

func validateUnicodeEscapes(raw []byte) bool {
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if !inString {
			if b == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			escaped = false
			if b != 'u' {
				continue
			}
			if i+4 >= len(raw) {
				return false
			}
			n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
			if err != nil {
				return false
			}
			i += 4
			if n >= 0xD800 && n <= 0xDBFF {
				if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
					return false
				}
				m, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
				if err != nil || m < 0xDC00 || m > 0xDFFF {
					return false
				}
				i += 6
			} else if n >= 0xDC00 && n <= 0xDFFF {
				return false
			}
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == '"' {
			inString = false
		}
	}
	return !inString && !escaped
}
func parseJSON(raw []byte) (jsonNode, bool, error) {
	if len(raw) == 0 || bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) || !utf8.Valid(raw) || !validateUnicodeEscapes(raw) {
		return jsonNode{}, false, errID(InvalidJSON, "utf-8 json")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	duplicate := false
	node, err := parseJSONNode(decoder, &duplicate)
	if err != nil {
		return jsonNode{}, duplicate, errID(InvalidJSON, "json value")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return jsonNode{}, duplicate, errID(InvalidJSON, "trailing data")
	}
	return node, duplicate, nil
}
func parseJSONNode(decoder *json.Decoder, duplicate *bool) (jsonNode, error) {
	token, err := decoder.Token()
	if err != nil {
		return jsonNode{}, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			node := jsonNode{kind: 'o'}
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return jsonNode{}, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return jsonNode{}, fmt.Errorf("object key")
				}
				child, err := parseJSONNode(decoder, duplicate)
				if err != nil {
					return jsonNode{}, err
				}
				if seen[key] {
					*duplicate = true
				}
				seen[key] = true
				node.object = append(node.object, jsonMember{key, child})
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return jsonNode{}, fmt.Errorf("object end")
			}
			return node, nil
		case '[':
			node := jsonNode{kind: 'a'}
			for decoder.More() {
				child, err := parseJSONNode(decoder, duplicate)
				if err != nil {
					return jsonNode{}, err
				}
				node.array = append(node.array, child)
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return jsonNode{}, fmt.Errorf("array end")
			}
			return node, nil
		default:
			return jsonNode{}, fmt.Errorf("delimiter")
		}
	case nil:
		return jsonNode{kind: 'n'}, nil
	default:
		return jsonNode{kind: 's', scalar: value}, nil
	}
}
func validateShape(node jsonNode, t reflect.Type, errors *[]error) {
	validateShapeWithNumberDomain(node, t, errors, InvalidValue)
}

func validateShapeWithNumberDomain(node jsonNode, t reflect.Type, errors *[]error, numberRangeError ErrorID) {
	if t.Kind() == reflect.Pointer {
		if node.kind == 'n' {
			*errors = append(*errors, errID(MissingMember, "null"))
			return
		}
		validateShapeWithNumberDomain(node, t.Elem(), errors, numberRangeError)
		return
	}
	if node.kind == 'n' {
		*errors = append(*errors, errID(MissingMember, "null"))
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		if node.kind != 'o' {
			*errors = append(*errors, errID(InvalidValue, "object token"))
			return
		}
		fields := map[string]struct {
			t        reflect.Type
			optional bool
		}{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			tag := field.Tag.Get("json")
			if tag == "-" {
				continue
			}
			parts := strings.Split(tag, ",")
			name := parts[0]
			if name == "" {
				name = field.Name
			}
			optional := false
			for _, part := range parts[1:] {
				if part == "omitempty" {
					optional = true
				}
			}
			fields[name] = struct {
				t        reflect.Type
				optional bool
			}{field.Type, optional}
		}
		seen := map[string]bool{}
		for _, member := range node.object {
			field, ok := fields[member.key]
			if !ok {
				*errors = append(*errors, errID(UnknownMember, member.key))
				continue
			}
			seen[member.key] = true
			fieldRangeError := InvalidValue
			if t == reflect.TypeOf(Decimal{}) && member.key == "exponent10" {
				fieldRangeError = InvalidDecimal
			}
			if t == reflect.TypeOf(CausalContext{}) && (member.key == "hop_count" || member.key == "max_hops") {
				fieldRangeError = CausalBudgetExceeded
			}
			validateShapeWithNumberDomain(member.value, field.t, errors, fieldRangeError)
		}
		for name, field := range fields {
			if !field.optional && !seen[name] {
				// Foundation record members are references, not semantic document
				// discriminators. In particular EvidenceRef.contract is required
				// evidence metadata, even when EvidenceRef is the Decode target.
				*errors = append(*errors, errID(MissingMember, name))
			}
		}
	case reflect.Slice:
		if node.kind != 'a' {
			*errors = append(*errors, errID(InvalidValue, "array token"))
			return
		}
		for _, child := range node.array {
			validateShapeWithNumberDomain(child, t.Elem(), errors, numberRangeError)
		}
	case reflect.String:
		if node.kind != 's' {
			*errors = append(*errors, errID(InvalidValue, "string token"))
			return
		}
		if _, ok := node.scalar.(string); !ok {
			*errors = append(*errors, errID(InvalidValue, "string token"))
		}
	case reflect.Bool:
		if node.kind != 's' {
			*errors = append(*errors, errID(InvalidValue, "boolean token"))
			return
		}
		if _, ok := node.scalar.(bool); !ok {
			*errors = append(*errors, errID(InvalidValue, "boolean token"))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if node.kind != 's' {
			*errors = append(*errors, errID(InvalidValue, "number token"))
			return
		}
		number, ok := node.scalar.(json.Number)
		if !ok {
			*errors = append(*errors, errID(InvalidValue, "number token"))
			return
		}
		if t.Kind() >= reflect.Int && t.Kind() <= reflect.Int64 {
			if _, err := strconv.ParseInt(number.String(), 10, t.Bits()); err != nil {
				id := InvalidValue
				if numberRangeError != InvalidValue {
					if _, ok := new(big.Int).SetString(number.String(), 10); ok {
						id = numberRangeError
					}
				}
				*errors = append(*errors, errID(id, "integer token"))
			}
		} else if _, err := strconv.ParseUint(number.String(), 10, t.Bits()); err != nil {
			id := InvalidValue
			if numberRangeError != InvalidValue && i64RE.MatchString(number.String()) {
				id = numberRangeError
			}
			*errors = append(*errors, errID(id, "integer token"))
		}
	default:
		*errors = append(*errors, errID(InvalidValue, "unsupported wire type"))
	}
}

var recordInterface = reflect.TypeOf((*Record)(nil)).Elem()

func independentlyKnowableErrors(node jsonNode, t reflect.Type) []error {
	if node.kind == 'n' {
		return nil
	}
	if t.Kind() == reflect.Pointer {
		return independentlyKnowableErrors(node, t.Elem())
	}
	var shapeErrors []error
	validateShape(node, t, &shapeErrors)
	bindable := true
	for _, err := range shapeErrors {
		if ErrorIdentifier(err) != UnknownMember {
			bindable = false
		}
	}
	if bindable && (t.Implements(recordInterface) || reflect.PointerTo(t).Implements(recordInterface)) {
		encoded, err := json.Marshal(jsonNodeValue(node))
		if err == nil {
			value := reflect.New(t)
			if json.Unmarshal(encoded, value.Interface()) == nil {
				if record, ok := value.Elem().Interface().(Record); ok {
					return []error{record.Validate()}
				}
				if record, ok := value.Interface().(Record); ok {
					return []error{record.Validate()}
				}
			}
		}
	}
	var errs []error
	errs = append(errs, conditionalMemberErrors(node, t)...)
	errs = append(errs, enclosingWireErrors(node, t)...)
	switch t.Kind() {
	case reflect.Struct:
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				fields[name] = field.Type
			}
		}
		for _, member := range node.object {
			if fieldType, ok := fields[member.key]; ok {
				errs = append(errs, fieldSemanticErrors(t, member.key, member.value, fieldType)...)
			}
		}
	case reflect.Slice:
		errs = append(errs, wireCollectionErrors(node, t.Elem())...)
		for _, child := range node.array {
			errs = append(errs, independentlyKnowableErrors(child, t.Elem())...)
		}
	}
	return errs
}

func jsonNodeValue(node jsonNode) interface{} {
	switch node.kind {
	case 'o':
		value := make(map[string]interface{}, len(node.object))
		for _, member := range node.object {
			value[member.key] = jsonNodeValue(member.value)
		}
		return value
	case 'a':
		value := make([]interface{}, len(node.array))
		for i, child := range node.array {
			value[i] = jsonNodeValue(child)
		}
		return value
	case 'n':
		return nil
	default:
		return node.scalar
	}
}

func decodeRecord[T Record](raw []byte) (T, error) {
	var zero T
	node, duplicate, syntax := parseJSON(raw)
	if syntax != nil {
		return zero, syntax
	}
	var errs []error
	if duplicate {
		errs = append(errs, errID(DuplicateKey, "member"))
	}
	targetType := reflect.TypeOf((*T)(nil)).Elem()
	validateShape(node, targetType, &errs)
	// Validate the original tree, including presence, before binding. Partial
	// unmarshalling synthesizes missing keys and may leave a typed nil receiver.
	errs = append(errs, independentlyKnowableErrors(node, targetType)...)
	if err := bestError(errs...); err != nil {
		return zero, err
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, errID(InvalidValue, "record binding")
	}
	return result, nil
}
func Decode[T Record](raw []byte) (T, error)                { return decodeRecord[T](raw) }
func DecodeDecimal(raw []byte) (Decimal, error)             { return Decode[Decimal](raw) }
func DecodeValue(raw []byte) (Value, error)                 { return Decode[Value](raw) }
func DecodeFactCandidate(raw []byte) (FactCandidate, error) { return Decode[FactCandidate](raw) }
func DecodeFactEnvelope(raw []byte) (FactEnvelope, error)   { return Decode[FactEnvelope](raw) }
func DecodeCapabilityInstance(raw []byte) (CapabilityInstance, error) {
	return Decode[CapabilityInstance](raw)
}

func CanonicalJSON(record Record) ([]byte, error) {
	if record == nil {
		return nil, errID(InvalidValue, "record")
	}
	value := reflect.ValueOf(record)
	if (value.Kind() == reflect.Ptr || value.Kind() == reflect.Map || value.Kind() == reflect.Slice || value.Kind() == reflect.Interface) && value.IsNil() {
		return nil, errID(InvalidValue, "nil record")
	}
	if err := record.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, errID(InvalidJSON, "marshal")
	}
	return canonicalize(raw)
}
func DigestRecord(record Record) (Digest, error) {
	canonical, err := CanonicalJSON(record)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}
func canonicalize(raw []byte) ([]byte, error) {
	node, duplicate, err := parseJSON(raw)
	if err != nil {
		return nil, err
	}
	if duplicate {
		return nil, errID(DuplicateKey, "member")
	}
	var out bytes.Buffer
	if err := writeCanonical(&out, node); err != nil {
		return nil, errID(InvalidJSON, "canonical json")
	}
	return out.Bytes(), nil
}
func compareJCSKey(a, b string) int {
	au, bu := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(au) && i < len(bu); i++ {
		if au[i] < bu[i] {
			return -1
		}
		if au[i] > bu[i] {
			return 1
		}
	}
	if len(au) < len(bu) {
		return -1
	}
	if len(au) > len(bu) {
		return 1
	}
	return 0
}
func writeJSONString(out *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("invalid utf-8")
	}
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return nil
}
func writeCanonical(out *bytes.Buffer, node jsonNode) error {
	switch node.kind {
	case 'n':
		out.WriteString("null")
	case 's':
		switch value := node.scalar.(type) {
		case string:
			return writeJSONString(out, value)
		case bool:
			out.WriteString(strconv.FormatBool(value))
		case json.Number:
			out.WriteString(value.String())
		default:
			return fmt.Errorf("scalar")
		}
	case 'a':
		out.WriteByte('[')
		for i, child := range node.array {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, child); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case 'o':
		members := append([]jsonMember(nil), node.object...)
		sort.Slice(members, func(i, j int) bool { return compareJCSKey(members[i].key, members[j].key) < 0 })
		out.WriteByte('{')
		for i, member := range members {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeJSONString(out, member.key); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := writeCanonical(out, member.value); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("node")
	}
	return nil
}
