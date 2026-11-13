package semreg

import (
	"strings"
)

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
	if i.Basis == nil {
		proof = errID(MissingMember, "identity basis")
	} else if len(i.Basis) == 0 {
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
