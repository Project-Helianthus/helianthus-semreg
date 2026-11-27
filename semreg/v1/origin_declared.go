package semreg

// EvidenceKindCertification is the kind of an evidence reference that cites a
// certification reference. It is the one evidence kind accepted without a
// separator.
const EvidenceKindCertification DefinitionID = "certification"

func hasCertificationEvidence(evidence []EvidenceRef) bool {
	for _, e := range evidence {
		if e.Kind == EvidenceKindCertification {
			return true
		}
	}
	return false
}

// declaredEvidenceError requires a declared origin to carry at least one
// evidence reference of kind certification, and forbids that kind on every
// other origin kind.
func (o OriginRef) declaredEvidenceError() error {
	if o.Kind == OriginDeclared {
		if !hasCertificationEvidence(o.Evidence) {
			return errID(MissingMember, "declared origin certification evidence")
		}
		return nil
	}
	if hasCertificationEvidence(o.Evidence) {
		return errID(InvalidValue, "certification evidence on a non-declared origin")
	}
	return nil
}

// declaredError applies the candidate-level rules of the declared origin: no
// native source, epoch, binding or driver generation, no derivation, and
// certification evidence only on a declared candidate.
func (c FactCandidate) declaredError() error {
	if c.Origin.Kind != OriginDeclared {
		if hasCertificationEvidence(c.Evidence) {
			return errID(InvalidValue, "certification evidence on a non-declared candidate")
		}
		return nil
	}
	var errs []error
	if c.BindingID != nil || c.SourceEpochID != nil || c.DriverGeneration != nil {
		errs = append(errs, errID(InvalidValue, "declared source path"))
	}
	if c.Derivation != nil {
		errs = append(errs, errID(InvalidValue, "declared derivation"))
	}
	return bestError(errs...)
}
