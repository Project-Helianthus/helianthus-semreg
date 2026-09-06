package projection

import (
	"bytes"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"golang.org/x/text/unicode/norm"
)

const (
	maxProjectionPacks       = 256
	maxProjectionItems       = 4096
	maxDispositionSourceKeys = 32
	maxLossDetails           = 32
	maxLossSourceItems       = 32
)

func errorf(id semreg.ErrorID, detail string) error { return &semreg.Error{ID: id, Detail: detail} }

func ranked(errors ...error) error {
	var chosen error
	best := int(^uint(0) >> 1)
	for _, err := range errors {
		if err == nil {
			continue
		}
		rank, ok := errorRank[semreg.ErrorIdentifier(err)]
		if !ok {
			rank = errorRank[semreg.InvalidValue]
		}
		if rank < best {
			chosen, best = err, rank
		}
	}
	return chosen
}

var errorRank = map[semreg.ErrorID]int{
	semreg.InvalidJSON: 1, semreg.DuplicateKey: 2, semreg.InvalidContract: 3,
	semreg.MissingMember: 4, semreg.UnknownMember: 5, semreg.InvalidIdentifier: 6,
	semreg.InvalidDecimal: 7, semreg.InvalidValue: 8, semreg.InvalidTime: 9,
	semreg.InvalidEvidence: 10, semreg.InvalidEnum: 11, semreg.BoundsExceeded: 12,
	semreg.NoncanonicalOrder: 13, semreg.DigestMismatch: 14,
	semreg.DanglingReference: 15, semreg.DerivationCycle: 16,
	semreg.StaleSourceEpoch: 17, semreg.StaleDriverGeneration: 18,
	semreg.SequenceConflict: 19, semreg.RevisionConflict: 20,
	semreg.IncomparableClockEpoch: 21, semreg.GenerationTransitionIncomplete: 22,
	semreg.DefinitionOwnerConflict: 23, semreg.DefinitionOwnerMissing: 24,
	semreg.IdentityNotQualified: 25, semreg.CapabilityNotQualified: 26,
	semreg.CapabilityUnavailable: 27, semreg.AuthorityMissing: 28,
	semreg.DeadlineExpired: 29, semreg.PreconditionFailed: 30,
	semreg.RouteSelectionForbidden: 31, semreg.AmbiguousRoute: 32,
	semreg.RetryForbidden: 33, semreg.InvalidOutcome: 34,
	semreg.EchoSuppressed: 35, semreg.CausalBudgetExceeded: 36,
	semreg.ProjectionIncomplete: 37, semreg.AliasNotRoutable: 38,
}

func (k ItemKind) Validate() error {
	if k != ItemFact && k != ItemRelation && k != ItemCapability && k != ItemOperation {
		return errorf(semreg.InvalidEnum, "projection item kind")
	}
	return nil
}

func (k LossKind) Validate() error {
	switch k {
	case LossUnit, LossRange, LossPrecision, LossTime, LossSymbol, LossProvenance, LossIdentity, LossCapability, LossOperation, LossPolicy:
		return nil
	default:
		return errorf(semreg.InvalidEnum, "projection loss kind")
	}
}

func (o ProjectionOutcome) Validate() error {
	switch o {
	case ProjectionExact, ProjectionTransformed, ProjectionWithheld, ProjectionUnrepresentable, ProjectionUnsupported, ProjectionUnknown:
		return nil
	default:
		return errorf(semreg.InvalidEnum, "projection outcome")
	}
}

func (m ProjectionManifest) Validate() error {
	errs := []error{m.TargetID.Validate(), m.TargetVersion.Validate(), m.KernelVersion.Validate(), m.MappingRevision.Validate()}
	if m.KernelVersion != semreg.ContractKernelV1 {
		errs = append(errs, errorf(semreg.InvalidContract, "projection kernel version"))
	}
	if m.PackVersions == nil {
		errs = append(errs, errorf(semreg.MissingMember, "pack versions"))
	}
	if len(m.PackVersions) > maxProjectionPacks {
		return ranked(append(errs, errorf(semreg.BoundsExceeded, "pack versions"))...)
	}
	for _, pack := range m.PackVersions {
		errs = append(errs, pack.Validate())
	}
	if duplicate, ordered := orderedPacks(m.PackVersions); duplicate {
		errs = append(errs, errorf(semreg.DuplicateKey, "pack versions"))
	} else if !ordered {
		errs = append(errs, errorf(semreg.NoncanonicalOrder, "pack versions"))
	}
	return ranked(errs...)
}

func (r RequestedItem) Validate() error { return ranked(r.ItemID.Validate(), r.Kind.Validate()) }

func (l LossDetail) Validate() error {
	errs := []error{l.Kind.Validate(), validatePublicText(l.Description)}
	if l.SourceItems == nil {
		errs = append(errs, errorf(semreg.MissingMember, "loss source items"))
	}
	if len(l.SourceItems) > maxLossSourceItems {
		return ranked(append(errs, errorf(semreg.BoundsExceeded, "loss source items"))...)
	}
	for _, item := range l.SourceItems {
		errs = append(errs, item.Validate())
	}
	if duplicate, ordered := orderedDefinitionIDs(l.SourceItems); duplicate {
		errs = append(errs, errorf(semreg.DuplicateKey, "loss source items"))
	} else if !ordered {
		errs = append(errs, errorf(semreg.NoncanonicalOrder, "loss source items"))
	}
	return ranked(errs...)
}

func (d ProjectionDisposition) Validate() error {
	errs := []error{d.Kind.Validate(), d.ItemID.Validate(), d.Outcome.Validate()}
	if d.SourceKeys == nil {
		errs = append(errs, errorf(semreg.MissingMember, "projection source keys"))
	}
	if d.Loss == nil {
		errs = append(errs, errorf(semreg.MissingMember, "projection loss"))
	}
	if len(d.SourceKeys) > maxDispositionSourceKeys || len(d.Loss) > maxLossDetails {
		return ranked(append(errs, errorf(semreg.BoundsExceeded, "projection disposition"))...)
	}
	for _, key := range d.SourceKeys {
		errs = append(errs, key.Validate())
	}
	for _, loss := range d.Loss {
		errs = append(errs, loss.Validate())
	}
	if duplicate, ordered := orderedFactKeys(d.SourceKeys); duplicate {
		errs = append(errs, errorf(semreg.DuplicateKey, "projection source keys"))
	} else if !ordered {
		errs = append(errs, errorf(semreg.NoncanonicalOrder, "projection source keys"))
	}
	if d.Outcome == ProjectionExact && len(d.Loss) != 0 {
		errs = append(errs, errorf(semreg.ProjectionIncomplete, "exact projection loss"))
	}
	if d.Outcome == ProjectionTransformed && len(d.Loss) == 0 {
		errs = append(errs, errorf(semreg.ProjectionIncomplete, "transformed projection loss"))
	}
	if d.Outcome == ProjectionWithheld || d.Outcome == ProjectionUnrepresentable || d.Outcome == ProjectionUnsupported || d.Outcome == ProjectionUnknown {
		if d.Reason == nil {
			errs = append(errs, errorf(semreg.ProjectionIncomplete, "projection reason"))
		}
	}
	if d.Reason != nil {
		errs = append(errs, d.Reason.Validate())
	}
	return ranked(errs...)
}

func (r ProjectionReport) Validate() error {
	errs := []error{r.Manifest.Validate(), r.SnapshotID.Validate(), r.Revisions.Validate()}
	if r.Contract != ContractProjectionV1 {
		errs = append(errs, errorf(semreg.InvalidContract, "projection report"))
	}
	if r.Requested == nil {
		errs = append(errs, errorf(semreg.MissingMember, "requested"))
	}
	if r.Dispositions == nil {
		errs = append(errs, errorf(semreg.MissingMember, "dispositions"))
	}
	if len(r.Requested) > maxProjectionItems || len(r.Dispositions) > maxProjectionItems {
		return ranked(append(errs, errorf(semreg.BoundsExceeded, "projection items"))...)
	}
	for _, requested := range r.Requested {
		errs = append(errs, requested.Validate())
	}
	for _, disposition := range r.Dispositions {
		errs = append(errs, disposition.Validate())
	}
	if duplicate, ordered := orderedRequested(r.Requested); duplicate {
		errs = append(errs, errorf(semreg.ProjectionIncomplete, "requested tuples"))
	} else if !ordered {
		errs = append(errs, errorf(semreg.NoncanonicalOrder, "requested tuples"))
	}
	if duplicate, ordered := orderedDispositions(r.Dispositions); duplicate {
		errs = append(errs, errorf(semreg.ProjectionIncomplete, "projection dispositions"))
	} else if !ordered {
		errs = append(errs, errorf(semreg.NoncanonicalOrder, "projection dispositions"))
	}
	if !sameRequestedTuples(r.Requested, r.Dispositions) {
		errs = append(errs, errorf(semreg.ProjectionIncomplete, "projection tuple accounting"))
	}
	if r.Causal != nil {
		errs = append(errs, r.Causal.Validate())
	}
	return ranked(errs...)
}

func (a CompatibilityAlias) Validate() error {
	errs := []error{a.LegacyID.Validate(), a.AssetID.Validate(), a.ValidFrom.Validate()}
	if a.AliasContract != ContractAliasV1 {
		errs = append(errs, errorf(semreg.InvalidContract, "compatibility alias"))
	}
	if a.Evidence == nil {
		errs = append(errs, errorf(semreg.MissingMember, "alias evidence"))
	}
	if len(a.Evidence) > 32 {
		return ranked(append(errs, errorf(semreg.BoundsExceeded, "alias evidence"))...)
	}
	for _, evidence := range a.Evidence {
		errs = append(errs, evidence.Validate())
	}
	if len(a.Evidence) == 0 {
		errs = append(errs, errorf(semreg.BoundsExceeded, "alias evidence"))
	}
	if duplicate, ordered := orderedEvidence(a.Evidence); duplicate {
		errs = append(errs, errorf(semreg.DuplicateKey, "alias evidence"))
	} else if !ordered {
		errs = append(errs, errorf(semreg.NoncanonicalOrder, "alias evidence"))
	}
	if a.ValidUntil != nil {
		errs = append(errs, a.ValidUntil.Validate())
		if a.ValidFrom.Validate() == nil && a.ValidUntil.Validate() == nil && compareSemver(a.ValidFrom, *a.ValidUntil) >= 0 {
			errs = append(errs, errorf(semreg.InvalidValue, "alias validity interval"))
		}
	}
	if a.Routable {
		errs = append(errs, errorf(semreg.AliasNotRoutable, "compatibility alias"))
	}
	return ranked(errs...)
}

func validatePublicText(value string) error {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) {
		return errorf(semreg.InvalidValue, "loss description")
	}
	for _, runeValue := range value {
		if unicode.IsControl(runeValue) && runeValue != '\t' && runeValue != '\n' && runeValue != '\r' {
			return errorf(semreg.InvalidValue, "loss description")
		}
	}
	return nil
}

func orderedPacks(items []semreg.PackRef) (bool, bool) {
	if len(items) < 2 {
		return false, true
	}
	ordered := true
	seen := make(map[string]struct{}, len(items))
	previous := items[0]
	seen[string(previous.ID)+"\x00"+string(previous.Version)] = struct{}{}
	for _, item := range items[1:] {
		key := string(item.ID) + "\x00" + string(item.Version)
		if _, exists := seen[key]; exists {
			return true, ordered
		}
		seen[key] = struct{}{}
		comparison := strings.Compare(string(previous.ID), string(item.ID))
		if comparison == 0 {
			comparison = compareSemver(previous.Version, item.Version)
		}
		if comparison > 0 {
			ordered = false
		}
		previous = item
	}
	return false, ordered
}
func orderedDefinitionIDs(items []semreg.DefinitionID) (bool, bool) {
	return ordered(items, func(item semreg.DefinitionID) string { return string(item) })
}
func orderedRequested(items []RequestedItem) (bool, bool) {
	return ordered(items, func(item RequestedItem) string { return string(item.Kind) + "\x00" + string(item.ItemID) })
}
func orderedDispositions(items []ProjectionDisposition) (bool, bool) {
	return ordered(items, func(item ProjectionDisposition) string { return string(item.Kind) + "\x00" + string(item.ItemID) })
}
func orderedEvidence(items []semreg.EvidenceRef) (bool, bool) {
	return ordered(items, func(item semreg.EvidenceRef) string {
		return string(item.Owner) + "\x00" + string(item.Kind) + "\x00" + string(item.Contract) + "\x00" + string(item.Digest)
	})
}
func orderedFactKeys(items []semreg.FactKey) (bool, bool) {
	return ordered(items, func(item semreg.FactKey) string {
		bytes, err := semreg.CanonicalJSON(item)
		if err != nil {
			return "\xff" + string(item.PackID) + "\x00" + string(item.PackVersion) + "\x00" + string(item.FactID)
		}
		return string(bytes)
	})
}
func ordered[T any](items []T, key func(T) string) (duplicate bool, ordered bool) {
	ordered = true
	if len(items) < 2 {
		return false, true
	}
	previous := key(items[0])
	seen := make(map[string]struct{}, len(items))
	seen[previous] = struct{}{}
	for _, item := range items[1:] {
		current := key(item)
		if _, exists := seen[current]; exists {
			duplicate = true
		}
		seen[current] = struct{}{}
		if bytes.Compare([]byte(previous), []byte(current)) > 0 {
			ordered = false
		}
		previous = current
	}
	return duplicate, ordered
}

func sameRequestedTuples(requested []RequestedItem, dispositions []ProjectionDisposition) bool {
	if len(requested) != len(dispositions) {
		return false
	}
	seen := make(map[string]struct{}, len(requested))
	for _, item := range requested {
		key := string(item.Kind) + "\x00" + string(item.ItemID)
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	for _, item := range dispositions {
		key := string(item.Kind) + "\x00" + string(item.ItemID)
		if _, exists := seen[key]; !exists {
			return false
		}
		delete(seen, key)
	}
	return len(seen) == 0
}

func compareSemver(left, right semreg.SemanticVersion) int {
	leftParts, rightParts := strings.Split(string(left), "."), strings.Split(string(right), ".")
	if len(leftParts) != 3 || len(rightParts) != 3 {
		return strings.Compare(string(left), string(right))
	}
	for index := range leftParts {
		leftValue, leftOK := new(big.Int).SetString(leftParts[index], 10)
		rightValue, rightOK := new(big.Int).SetString(rightParts[index], 10)
		if !leftOK || !rightOK {
			return strings.Compare(string(left), string(right))
		}
		if comparison := leftValue.Cmp(rightValue); comparison < 0 {
			return -1
		} else if comparison > 0 {
			return 1
		}
	}
	return 0
}
