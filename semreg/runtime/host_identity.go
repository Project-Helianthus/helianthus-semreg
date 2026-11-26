package runtime

import (
	"errors"
	"fmt"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// OperatorAction is the closed action of an operator identity statement.
type OperatorAction string

// OperatorAction values.
const (
	OperatorCertify OperatorAction = "certify"
	OperatorRebind  OperatorAction = "rebind"
	OperatorRevoke  OperatorAction = "revoke"
)

// ErrStatementRevision is returned for a revocation whose target revision is not
// the revision of the statement it revokes.
var ErrStatementRevision = errors.New("runtime: the revocation names another revision than the statement it revokes")

// OperatorIdentityStatement is an identity declaration of the embedding
// application, at the same trusted host boundary as the installation
// configuration. Join.Statement carries the event revision and the
// authenticated origin evidence. TargetRevision is set on a revocation only and
// refers to the certification or rebind it revokes; a revocation carries that
// statement's exact Join. The type is typed and has no wire form: only the
// trusted embedding handoff can populate it.
type OperatorIdentityStatement struct {
	Action         OperatorAction
	Join           driver.CertifiedJoin
	TargetRevision v1.Uint64
}

// Validate checks the action, the join and the target revision.
func (s OperatorIdentityStatement) Validate() error {
	if err := s.Join.Validate(); err != nil {
		return err
	}
	switch s.Action {
	case OperatorCertify, OperatorRebind:
		if s.TargetRevision != "" {
			return fmt.Errorf("runtime: a %s statement carries no target revision", s.Action)
		}
		return nil
	case OperatorRevoke:
		return s.TargetRevision.Validate()
	}
	return fmt.Errorf("runtime: %q is not certify, rebind or revoke", s.Action)
}

// applyOperatorStatements hands the statements to the identity owner in order. A
// statement whose pair the ledger does not yet hold is retained there and
// applies when both members are present.
func applyOperatorStatements(ledger *identity.Ledger, statements []OperatorIdentityStatement) error {
	for i, s := range statements {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("identity statement %d: %w", i, err)
		}
		var err error
		switch s.Action {
		case OperatorCertify:
			err = ledger.Certify(s.Join)
		case OperatorRebind:
			old := s.Join.A
			if old == s.Join.Survivor {
				old = s.Join.B
			}
			err = ledger.Rebind(old, s.Join.Survivor, s.Join)
		case OperatorRevoke:
			if s.TargetRevision != s.Join.Statement.Revision {
				err = ErrStatementRevision
			} else {
				err = ledger.Revoke(s.Join)
			}
		}
		if err != nil {
			return fmt.Errorf("identity statement %d (%s): %w", i, s.Action, err)
		}
	}
	return nil
}
