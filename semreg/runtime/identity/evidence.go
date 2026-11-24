package identity

import (
	"fmt"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func first(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// AdmitMatch admits one publisher's attestation of its own observation of a
// scoped identifier. The claim is validated for its member, scheme, digest and
// proof; the named member must be a node of the publisher; and the scheme must
// be listed in the catalog, with an issuer that fits the scheme's issuer
// registry. A claim whose scheme is not listed is refused with
// ErrSchemeNotListed and no scheme id is minted. A claim that names another
// publisher's member, or lacks valid evidence, leaves membership unchanged. A
// later claim of the same member, scheme and issuer replaces the earlier one.
func (l *Ledger) AdmitMatch(p Publisher, claim driver.MatchClaim) error {
	if err := first(p.Validate(), claim.Validate()); err != nil {
		return err
	}
	scheme, listed := l.scheme(claim.Scheme)
	if !listed {
		return fmt.Errorf("%w: %s", ErrSchemeNotListed, claim.Scheme)
	}
	if (scheme.IssuerRegistry == graph.IssuerNone) != (claim.Issuer == "") {
		return fmt.Errorf("%w: %s", ErrIssuer, claim.Scheme)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ownedBy(claim.Member, p); err != nil {
		return err
	}
	claim.Proof = append(claim.Proof[:0:0], claim.Proof...)
	l.matches[matchKey{member: claim.Member, scheme: claim.Scheme, issuer: claim.Issuer}] = matchRecord{Publisher: p, Claim: claim}
	return nil
}

func (l *Ledger) scheme(id string) (graph.IdentificationScheme, bool) {
	if l.allocator.catalog == nil {
		return graph.IdentificationScheme{}, false
	}
	for _, scheme := range l.allocator.catalog.IdentificationSchemes() {
		if string(scheme.ID) == id {
			return scheme, true
		}
	}
	return graph.IdentificationScheme{}, false
}

// AdmitManufacturerSerial admits one publisher's device-reported manufacturer
// and serial for its own member. The claim is validated for its member and
// proof, and the member must be a node of the publisher. A missing component is
// kept and judged by Resolve, which reports it and infers no identity. No catalog
// scheme is involved. A later claim of the same member replaces the earlier one.
func (l *Ledger) AdmitManufacturerSerial(p Publisher, claim driver.ManufacturerSerialClaim) error {
	if err := first(p.Validate(), claim.Validate()); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.ownedBy(claim.Member, p); err != nil {
		return err
	}
	claim.Proof = append(claim.Proof[:0:0], claim.Proof...)
	l.serials[claim.Member] = serialRecord{Publisher: p, Claim: claim}
	return nil
}
