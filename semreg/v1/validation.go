package semreg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	InvalidJSON             ErrorID = "invalid_json"
	DuplicateKey            ErrorID = "duplicate_key"
	InvalidContract         ErrorID = "invalid_contract"
	MissingMember           ErrorID = "missing_member"
	UnknownMember           ErrorID = "unknown_member"
	InvalidIdentifier       ErrorID = "invalid_identifier"
	InvalidDecimal          ErrorID = "invalid_decimal"
	InvalidValue            ErrorID = "invalid_value"
	InvalidTime             ErrorID = "invalid_time"
	InvalidEvidence         ErrorID = "invalid_evidence"
	InvalidEnum             ErrorID = "invalid_enum"
	BoundsExceeded          ErrorID = "bounds_exceeded"
	NoncanonicalOrder       ErrorID = "noncanonical_order"
	DefinitionOwnerConflict ErrorID = "definition_owner_conflict"
	DefinitionOwnerMissing  ErrorID = "definition_owner_missing"
	IdentityNotQualified    ErrorID = "identity_not_qualified"
	CapabilityNotQualified  ErrorID = "capability_not_qualified"
	DerivationCycle         ErrorID = "derivation_cycle"
	IncomparableClockEpoch  ErrorID = "incomparable_clock_epoch"
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

var (
	contractRE    = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._/-][a-z0-9]+)*(?:[./]v[1-9][0-9]*)$`)
	definitionRE  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	opaqueRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]*$`)
	semverRE      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	digestRE      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	coefficientRE = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)
)

func validDefinition(v DefinitionID) bool {
	return len(v) >= 3 && len(v) <= 160 && definitionRE.MatchString(string(v))
}
func validContract(v ContractVersion) bool {
	return len(v) >= 1 && len(v) <= 128 && contractRE.MatchString(string(v))
}
func validOpaque(v string) bool          { return len(v) >= 1 && len(v) <= 256 && opaqueRE.MatchString(v) }
func validSemver(v SemanticVersion) bool { return semverRE.MatchString(string(v)) }
func u64(v Uint64) (*big.Int, bool) {
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)$`).MatchString(string(v)) {
		return nil, false
	}
	n, ok := new(big.Int).SetString(string(v), 10)
	return n, ok && n.BitLen() <= 64
}
func i64(v Int64) (*big.Int, bool) {
	if !regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`).MatchString(string(v)) {
		return nil, false
	}
	n, ok := new(big.Int).SetString(string(v), 10)
	return n, ok && n.IsInt64()
}
func positive(v Uint64) bool { n, ok := u64(v); return ok && n.Sign() > 0 }
func sortedUnique[T interface{}](items []T, key func(T) string) (bool, bool) {
	previous := ""
	for i, x := range items {
		k := key(x)
		if i > 0 && k <= previous {
			return false, k == previous
		}
		previous = k
	}
	return true, false
}
func validateEvidence(e EvidenceRef) error {
	if !validDefinition(e.Owner) || !validDefinition(e.Kind) || !validContract(e.Contract) || !digestRE.MatchString(string(e.Digest)) {
		return errID(InvalidEvidence, "evidence reference")
	}
	if e.Access != EvidenceAccessPublic && e.Access != EvidenceAccessAuthorized && e.Access != EvidenceAccessRestricted {
		return errID(InvalidEnum, "evidence access")
	}
	if e.Redaction != RedactionNone && e.Redaction != RedactionRedacted && e.Redaction != RedactionMetadataOnly {
		return errID(InvalidEnum, "redaction")
	}
	if e.Access == EvidenceAccessRestricted && e.Redaction == RedactionNone {
		return errID(InvalidEvidence, "restricted evidence")
	}
	return nil
}

func (e EvidenceRef) Validate() error { return validateEvidence(e) }

func (b NativeBinding) Validate() error {
	if !validOpaque(string(b.BindingID)) || !validOpaque(string(b.AssetID)) || !validOpaque(string(b.SourceID)) || !validOpaque(string(b.SourceEpochID)) || !positive(b.DriverGeneration) || !positive(b.Revision) {
		return errID(InvalidIdentifier, "native binding")
	}
	if b.State != BindingCurrent && b.State != BindingFenced && b.State != BindingRetired {
		return errID(InvalidEnum, "binding state")
	}
	return b.NativeResource.Validate()
}
func validateEvidenceSet(es []EvidenceRef, min, max int) error {
	if len(es) < min || len(es) > max {
		return errID(BoundsExceeded, "evidence")
	}
	ok, dup := sortedUnique(es, func(e EvidenceRef) string {
		return string(e.Owner) + "\x00" + string(e.Kind) + "\x00" + string(e.Contract) + "\x00" + string(e.Digest)
	})
	if dup {
		return errID(DuplicateKey, "evidence")
	}
	if !ok {
		return errID(NoncanonicalOrder, "evidence")
	}
	for _, e := range es {
		if err := validateEvidence(e); err != nil {
			return err
		}
	}
	return nil
}
func validateText(s string, max int, controls bool) bool {
	if !utf8.ValidString(s) || len(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && (!controls || (r != '\t' && r != '\n' && r != '\r')) {
			return false
		}
	}
	return true
}

func (d Decimal) Validate() error {
	if !coefficientRE.MatchString(d.Coefficient) || d.Exponent10 < -18 || d.Exponent10 > 18 || (d.Coefficient == "0" && d.Exponent10 != 0) || (d.Coefficient != "0" && strings.HasSuffix(d.Coefficient, "0")) {
		return errID(InvalidDecimal, "decimal")
	}
	return nil
}
func (s Symbol) Validate() error {
	if !validDefinition(s.Namespace) || !validateText(s.Token, 256, false) || (strings.HasPrefix(string(s.Namespace), "native.") && s.Known) {
		return errID(InvalidValue, "symbol")
	}
	if !s.Known && !strings.HasPrefix(string(s.Namespace), "native.") {
		return errID(InvalidValue, "unknown symbol namespace")
	}
	return nil
}
func (v Value) Validate() error {
	count := 0
	if v.Quantity != nil {
		count++
	}
	if v.Boolean != nil {
		count++
	}
	if v.Text != nil {
		count++
	}
	if v.Symbol != nil {
		count++
	}
	if v.Symbols != nil {
		count++
	}
	if v.Time != nil {
		count++
	}
	if count != 1 {
		return errID(InvalidValue, "value payload")
	}
	switch v.Kind {
	case ValueQuantity:
		if v.Quantity == nil || v.Quantity.Number.Validate() != nil || !validDefinition(v.Quantity.Unit) {
			return errID(InvalidValue, "quantity")
		}
	case ValueBoolean:
		if v.Boolean == nil {
			return errID(InvalidValue, "boolean")
		}
	case ValueText:
		if v.Text == nil || !validateText(*v.Text, 4096, true) {
			return errID(InvalidValue, "text")
		}
	case ValueSymbol:
		if v.Symbol == nil {
			return errID(InvalidValue, "symbol")
		}
	case ValueSymbols:
		if len(v.Symbols) < 1 || len(v.Symbols) > 64 {
			return errID(BoundsExceeded, "symbols")
		}
		ok, dup := sortedUnique(v.Symbols, func(s Symbol) string { return string(s.Namespace) + "\x00" + s.Token })
		if dup {
			return errID(DuplicateKey, "symbols")
		}
		if !ok {
			return errID(NoncanonicalOrder, "symbols")
		}
		for _, s := range v.Symbols {
			if err := s.Validate(); err != nil {
				return err
			}
		}
	case ValueTime:
		if v.Time == nil {
			return errID(InvalidValue, "time")
		}
	default:
		return errID(InvalidEnum, "value kind")
	}
	if v.Symbol != nil {
		return v.Symbol.Validate()
	}
	if v.Time != nil {
		return v.Time.Validate()
	}
	return nil
}
func (t TimePoint) Validate() error {
	if _, ok := i64(t.UnixNanoseconds); !ok || !validDefinition(t.ClockID) || !func() bool { _, ok := u64(t.UncertaintyNS); return ok }() {
		return errID(InvalidTime, "time point")
	}
	return nil
}
func (m MonotonicPoint) Validate() error {
	if !validOpaque(string(m.ClockEpochID)) {
		return errID(InvalidIdentifier, "clock epoch")
	}
	if _, ok := u64(m.Nanoseconds); !ok {
		return errID(InvalidTime, "monotonic")
	}
	return nil
}
func (t Times) Validate() error {
	if err := t.ReceivedAt.Validate(); err != nil {
		return err
	}
	if err := t.EvaluatedAt.Validate(); err != nil {
		return err
	}
	if err := t.ReceiptMonotonic.Validate(); err != nil {
		return err
	}
	if err := t.EvaluateMonotonic.Validate(); err != nil {
		return err
	}
	if t.PhenomenonAt != nil {
		if err := t.PhenomenonAt.Validate(); err != nil {
			return err
		}
	}
	if t.SourceAt != nil {
		if err := t.SourceAt.Validate(); err != nil {
			return err
		}
	}
	if t.ReceiptMonotonic.ClockEpochID != t.EvaluateMonotonic.ClockEpochID {
		return errID(IncomparableClockEpoch, "monotonic epoch")
	}
	r, _ := u64(t.ReceiptMonotonic.Nanoseconds)
	e, _ := u64(t.EvaluateMonotonic.Nanoseconds)
	if e.Cmp(r) < 0 {
		return errID(InvalidTime, "monotonic order")
	}
	return nil
}
func (p FreshnessPolicy) Validate() error {
	if !validOpaque(string(p.PolicyID)) || !validSemver(p.Version) || !positive(p.FreshForNS) {
		return errID(InvalidTime, "freshness policy")
	}
	f, _ := u64(p.FreshForNS)
	r, ok := u64(p.RetainForNS)
	if !ok || r.Cmp(f) <= 0 {
		return errID(InvalidTime, "retention")
	}
	if _, ok := u64(p.MaxWallUncertaintyNS); !ok {
		return errID(InvalidTime, "uncertainty")
	}
	return nil
}
func (q Quality) Validate() error {
	if q.Assertion != AssertionObserved && q.Assertion != AssertionInferred {
		return errID(InvalidEnum, "assertion")
	}
	if q.Qualification != QualificationCandidate && q.Qualification != QualificationQualified && q.Qualification != QualificationUnsupported && q.Qualification != QualificationUnknown && q.Qualification != QualificationRejected {
		return errID(InvalidEnum, "qualification")
	}
	if q.Promotion != PromotionUnpromoted && q.Promotion != PromotionPromoted {
		return errID(InvalidEnum, "promotion")
	}
	if q.Validity != ValidityGood && q.Validity != ValiditySuspect && q.Validity != ValidityBad && q.Validity != ValidityUnknown {
		return errID(InvalidEnum, "validity")
	}
	if q.Availability != AvailabilityAvailable && q.Availability != AvailabilityDegraded && q.Availability != AvailabilityUnavailable && q.Availability != AvailabilityWithdrawn {
		return errID(InvalidEnum, "availability")
	}
	if q.Freshness != FreshnessFresh && q.Freshness != FreshnessStale && q.Freshness != FreshnessExpired && q.Freshness != FreshnessUnknown {
		return errID(InvalidEnum, "freshness")
	}
	if q.Promotion == PromotionPromoted && (q.Qualification != QualificationQualified || (q.Validity != ValidityGood && q.Validity != ValiditySuspect) || (q.Availability != AvailabilityAvailable && q.Availability != AvailabilityDegraded)) {
		return errID(InvalidValue, "promotion")
	}
	ok, dup := sortedUnique(q.Reasons, func(v DefinitionID) string { return string(v) })
	if dup {
		return errID(DuplicateKey, "reasons")
	}
	if !ok {
		return errID(NoncanonicalOrder, "reasons")
	}
	if len(q.Reasons) > 16 {
		return errID(BoundsExceeded, "reasons")
	}
	for _, r := range q.Reasons {
		if !validDefinition(r) {
			return errID(InvalidIdentifier, "reason")
		}
	}
	return nil
}
func (k FactKey) Validate() error {
	if !validDefinition(k.PackID) || !validSemver(k.PackVersion) || !validDefinition(k.FactID) || len(k.Dimensions) > 16 {
		return errID(InvalidIdentifier, "fact key")
	}
	ok, dup := sortedUnique(k.Dimensions, func(d Dimension) string { return string(d.ID) })
	if dup {
		return errID(DuplicateKey, "dimensions")
	}
	if !ok {
		return errID(NoncanonicalOrder, "dimensions")
	}
	for _, d := range k.Dimensions {
		if !validDefinition(d.ID) || d.Value.Validate() != nil {
			return errID(InvalidValue, "dimension")
		}
	}
	return nil
}
func (i IdentityLink) Validate() error {
	if !validOpaque(string(i.AssetID)) || !validOpaque(string(i.BindingID)) || !positive(i.Revision) {
		return errID(InvalidIdentifier, "identity")
	}
	if i.State != LinkCandidate && i.State != LinkQualified && i.State != LinkRejected && i.State != LinkConflict && i.State != LinkWithdrawn {
		return errID(InvalidEnum, "link state")
	}
	if len(i.Basis) == 0 {
		return errID(IdentityNotQualified, "identity basis")
	}
	return validateEvidenceSet(i.Basis, 1, 32)
}
func (d Derivation) Validate() error {
	if !validDefinition(d.Algorithm) || !validSemver(d.Version) || len(d.Inputs) < 1 || len(d.Inputs) > 32 {
		return errID(BoundsExceeded, "derivation")
	}
	ok, dup := sortedUnique(d.Inputs, func(v DerivationInput) string { return string(v.CandidateID) })
	if dup {
		return errID(DerivationCycle, "derivation input")
	}
	if !ok {
		return errID(NoncanonicalOrder, "derivation inputs")
	}
	for _, in := range d.Inputs {
		if !validOpaque(string(in.CandidateID)) || !positive(in.CandidateRevision) || len(in.SourcePaths) < 1 || len(in.SourcePaths) > 32 {
			return errID(InvalidIdentifier, "derivation input")
		}
		ok, dup = sortedUnique(in.SourcePaths, func(p SourcePathRef) string {
			return string(p.SourceID) + "\x00" + string(p.SourceEpochID) + "\x00" + string(p.DriverGeneration) + "\x00" + string(p.BindingID)
		})
		if dup {
			return errID(DuplicateKey, "source paths")
		}
		if !ok {
			return errID(NoncanonicalOrder, "source paths")
		}
	}
	return validateEvidenceSet(d.Evidence, 0, 32)
}
func (c FactCandidate) Validate() error {
	if !validOpaque(string(c.CandidateID)) || !positive(c.Revision) {
		return errID(InvalidIdentifier, "candidate")
	}
	if err := c.Key.Validate(); err != nil {
		return err
	}
	if err := c.Quality.Validate(); err != nil {
		return err
	}
	if err := c.Times.Validate(); err != nil {
		return err
	}
	if err := c.FreshnessPolicy.Validate(); err != nil {
		return err
	}
	if err := validateEvidenceSet(c.Evidence, 1, 32); err != nil {
		return err
	}
	if (c.Quality.Qualification == QualificationUnsupported || c.Quality.Qualification == QualificationRejected || c.Quality.Availability == AvailabilityWithdrawn) && c.Value != nil {
		return errID(InvalidValue, "forbidden candidate value")
	}
	if c.Value == nil && c.Quality.Qualification != QualificationUnsupported && c.Quality.Qualification != QualificationRejected && c.Quality.Availability != AvailabilityWithdrawn {
		return errID(MissingMember, "candidate value")
	}
	if c.Value != nil {
		if err := c.Value.Validate(); err != nil {
			return err
		}
	}
	if c.Quality.Assertion == AssertionInferred {
		if c.Derivation == nil {
			return errID(MissingMember, "derivation")
		}
		return c.Derivation.Validate()
	}
	if c.Derivation != nil {
		return errID(InvalidValue, "observed derivation")
	}
	if c.BindingID == nil || c.SourceEpochID == nil || c.DriverGeneration == nil || !positive(*c.DriverGeneration) {
		return errID(MissingMember, "observed source path")
	}
	return nil
}

type Registry struct {
	validators map[PackRef]PackValidator
	owners     map[string]PackRef
}

func NewRegistry(validators ...PackValidator) (*Registry, error) {
	r := &Registry{validators: make(map[PackRef]PackValidator), owners: make(map[string]PackRef)}
	for _, v := range validators {
		if v == nil {
			return nil, errID(DefinitionOwnerConflict, "nil validator")
		}
		p := v.Pack()
		if _, ok := r.validators[p]; ok {
			return nil, errID(DefinitionOwnerConflict, "duplicate pack")
		}
		idx := v.Definitions()
		if idx.Pack != p {
			return nil, errID(DefinitionOwnerConflict, "index pack")
		}
		if err := idx.Validate(); err != nil {
			return nil, err
		}
		r.validators[p] = v
		for _, group := range []struct {
			k  string
			rs []DefinitionRef
		}{{"field", idx.Fields}, {"service", idx.Services}, {"capability", idx.Capabilities}, {"operation", idx.Operations}, {"effect", idx.EffectRules}} {
			for _, d := range group.rs {
				k := group.k + "\x00" + string(d.ID) + "\x00" + string(d.Version)
				if _, ok := r.owners[k]; ok {
					return nil, errID(DefinitionOwnerConflict, "definition")
				}
				r.owners[k] = p
			}
		}
	}
	return r, nil
}
func (r *Registry) Validator(pack PackRef) (PackValidator, error) {
	v, ok := r.validators[pack]
	if !ok {
		return nil, errID(DefinitionOwnerMissing, "pack")
	}
	return v, nil
}
func (p PackRef) Validate() error {
	if !validDefinition(p.ID) || !validSemver(p.Version) {
		return errID(InvalidIdentifier, "pack")
	}
	return nil
}
func (d DefinitionRef) Validate() error {
	if d.Pack.Validate() != nil || !validDefinition(d.ID) || !validSemver(d.Version) {
		return errID(InvalidIdentifier, "definition")
	}
	return nil
}
func (d DefinitionIndex) Validate() error {
	if err := d.Pack.Validate(); err != nil {
		return err
	}
	for _, rs := range [][]DefinitionRef{d.Fields, d.Services, d.Capabilities, d.Operations, d.EffectRules} {
		ok, dup := sortedUnique(rs, func(r DefinitionRef) string { return string(r.ID) + "\x00" + string(r.Version) })
		if dup {
			return errID(DuplicateKey, "definitions")
		}
		if !ok {
			return errID(NoncanonicalOrder, "definitions")
		}
		for _, r := range rs {
			if r.Validate() != nil || r.Pack != d.Pack {
				return errID(DefinitionOwnerConflict, "definition index")
			}
		}
	}
	return nil
}
func (c CapabilityInstance) Validate() error {
	if !validOpaque(string(c.InstanceID)) || !validOpaque(string(c.AssetID)) || !validOpaque(string(c.ServiceInstance)) || c.Definition.Validate() != nil || !validOpaque(string(c.BindingID)) || !validOpaque(string(c.SourceEpochID)) || !positive(c.DriverGeneration) || !positive(c.Revision) {
		return errID(InvalidIdentifier, "capability")
	}
	if c.Qualification != QualificationCandidate && c.Qualification != QualificationQualified && c.Qualification != QualificationUnsupported && c.Qualification != QualificationUnknown && c.Qualification != QualificationRejected {
		return errID(InvalidEnum, "capability qualification")
	}
	if c.Availability != AvailabilityAvailable && c.Availability != AvailabilityDegraded && c.Availability != AvailabilityUnavailable && c.Availability != AvailabilityWithdrawn {
		return errID(InvalidEnum, "capability availability")
	}
	if len(c.ActivationEvidence) == 0 {
		return errID(CapabilityNotQualified, "activation evidence")
	}
	return validateEvidenceSet(c.ActivationEvidence, 1, 32)
}

func (f TypedField) Validate() error {
	if !validDefinition(f.ID) {
		return errID(InvalidIdentifier, "field")
	}
	return f.Value.Validate()
}
func (s ServiceInstance) Validate() error {
	if !validOpaque(string(s.InstanceID)) || !validOpaque(string(s.AssetID)) || s.Definition.Validate() != nil || !validOpaque(string(s.BindingID)) || !validOpaque(string(s.SourceEpochID)) || !positive(s.DriverGeneration) || !positive(s.Revision) {
		return errID(InvalidIdentifier, "service")
	}
	if s.Qualification != QualificationCandidate && s.Qualification != QualificationQualified && s.Qualification != QualificationUnsupported && s.Qualification != QualificationUnknown && s.Qualification != QualificationRejected {
		return errID(InvalidEnum, "service qualification")
	}
	if s.Availability != AvailabilityAvailable && s.Availability != AvailabilityDegraded && s.Availability != AvailabilityUnavailable && s.Availability != AvailabilityWithdrawn {
		return errID(InvalidEnum, "service availability")
	}
	return nil
}
func (f FactEnvelope) Validate() error {
	if !validOpaque(string(f.AssetID)) || f.Key.Validate() != nil || !positive(f.Revision) {
		return errID(InvalidIdentifier, "fact envelope")
	}
	if len(f.Candidates) < 1 || len(f.Candidates) > 32 {
		return errID(BoundsExceeded, "candidates")
	}
	ok, dup := sortedUnique(f.Candidates, func(c FactCandidate) string { return string(c.CandidateID) })
	if dup {
		return errID(DuplicateKey, "candidates")
	}
	if !ok {
		return errID(NoncanonicalOrder, "candidates")
	}
	for _, c := range f.Candidates {
		if !sameFactKey(c.Key, f.Key) {
			return errID(InvalidValue, "candidate key")
		}
		if err := c.Validate(); err != nil {
			return err
		}
	}
	ok, dup = sortedUnique(f.Conflicts, func(c Conflict) string { return string(c.ConflictID) })
	if dup {
		return errID(DuplicateKey, "conflicts")
	}
	if !ok {
		return errID(NoncanonicalOrder, "conflicts")
	}
	for _, c := range f.Conflicts {
		if c.Kind != ConflictValue || c.State != ConflictOpen || len(c.Candidates) < 2 || len(c.Candidates) > 32 {
			return errID(InvalidValue, "conflict")
		}
		ok, dup := sortedUnique(c.Candidates, func(id CandidateID) string { return string(id) })
		if dup {
			return errID(DuplicateKey, "conflict candidates")
		}
		if !ok {
			return errID(NoncanonicalOrder, "conflict candidates")
		}
		if err := validateEvidenceSet(c.Evidence, 1, 1024); err != nil {
			return err
		}
	}
	return nil
}

func sameFactKey(left, right FactKey) bool {
	a, errA := json.Marshal(left)
	b, errB := json.Marshal(right)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

// DecodeDecimal applies the v1 wire checks before binding a decimal. It is
// intentionally narrow: later records each expose an equally typed decoder.
func DecodeDecimal(raw []byte) (Decimal, error) {
	members, err := strictObject(raw)
	if err != nil {
		return Decimal{}, err
	}
	if len(members) != 2 {
		if _, ok := members["coefficient"]; !ok {
			return Decimal{}, errID(MissingMember, "coefficient")
		}
		if _, ok := members["exponent10"]; !ok {
			return Decimal{}, errID(MissingMember, "exponent10")
		}
		return Decimal{}, errID(UnknownMember, "decimal member")
	}
	coefficient, ok := members["coefficient"]
	if !ok {
		return Decimal{}, errID(MissingMember, "coefficient")
	}
	exponent, ok := members["exponent10"]
	if !ok {
		return Decimal{}, errID(MissingMember, "exponent10")
	}
	var d Decimal
	if json.Unmarshal(coefficient, &d.Coefficient) != nil || json.Unmarshal(exponent, &d.Exponent10) != nil {
		return Decimal{}, errID(InvalidValue, "decimal token")
	}
	if err := d.Validate(); err != nil {
		return Decimal{}, err
	}
	return d, nil
}

func strictObject(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	t, err := d.Token()
	if err != nil {
		return nil, errID(InvalidJSON, "object")
	}
	if delim, ok := t.(json.Delim); !ok || delim != '{' {
		return nil, errID(InvalidJSON, "object")
	}
	m := make(map[string]json.RawMessage)
	for d.More() {
		keyToken, err := d.Token()
		if err != nil {
			return nil, errID(InvalidJSON, "member")
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errID(InvalidJSON, "key")
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, errID(InvalidJSON, "value")
		}
		if _, exists := m[key]; exists {
			return nil, errID(DuplicateKey, "member")
		}
		m[key] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, errID(InvalidJSON, "end object")
	}
	var extra json.RawMessage
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errID(InvalidJSON, "trailing data")
	}
	return m, nil
}

// Record is a typed public semantic record accepted by CanonicalJSON.
type Record interface{ Validate() error }

func CanonicalJSON(record Record) ([]byte, error) {
	if record == nil {
		return nil, errID(InvalidValue, "record")
	}
	if err := record.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(record)
	if err != nil {
		return nil, errID(InvalidJSON, "marshal")
	}
	return canonicalize(b)
}
func DigestRecord(record Record) (Digest, error) {
	b, err := CanonicalJSON(record)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}
func canonicalize(raw []byte) ([]byte, error) {
	var out bytes.Buffer
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := canonValue(d, &out); err != nil {
		return nil, errID(InvalidJSON, "canonical json")
	}
	if d.More() {
		return nil, errID(InvalidJSON, "trailing data")
	}
	return out.Bytes(), nil
}
func canonValue(d *json.Decoder, out *bytes.Buffer) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	switch x := tok.(type) {
	case json.Delim:
		if x == '{' {
			members := map[string]json.RawMessage{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok {
					return fmt.Errorf("key")
				}
				var r json.RawMessage
				if err := d.Decode(&r); err != nil {
					return err
				}
				if _, ok := members[key]; ok {
					return fmt.Errorf("duplicate")
				}
				members[key] = r
			}
			if _, err := d.Token(); err != nil {
				return err
			}
			keys := make([]string, 0, len(members))
			for k := range members {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			out.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					out.WriteByte(',')
				}
				kb, _ := json.Marshal(k)
				out.Write(kb)
				out.WriteByte(':')
				nested := json.NewDecoder(bytes.NewReader(members[k]))
				nested.UseNumber()
				if err := canonValue(nested, out); err != nil {
					return err
				}
			}
			out.WriteByte('}')
			return nil
		}
		if x == '[' {
			out.WriteByte('[')
			for i := 0; d.More(); i++ {
				if i > 0 {
					out.WriteByte(',')
				}
				if err := canonValue(d, out); err != nil {
					return err
				}
			}
			_, err := d.Token()
			out.WriteByte(']')
			return err
		}
	case string:
		b, _ := json.Marshal(x)
		out.Write(b)
	case bool:
		out.WriteString(strconv.FormatBool(x))
	case nil:
		return fmt.Errorf("null")
	case json.Number:
		out.WriteString(x.String())
	default:
		return fmt.Errorf("token")
	}
	return nil
}
