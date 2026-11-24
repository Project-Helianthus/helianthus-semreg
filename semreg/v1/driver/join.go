package driver

import (
	"errors"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// MatchClaim is a driver's attestation of its own observation of a scoped
// identifier. The key is comparison evidence, never the resource id. No claim is
// made from a name, an address, a quantity or a resemblance.
type MatchClaim struct {
	Member NodeRef          `json:"member"`
	Scheme string           `json:"scheme"`
	Issuer string           `json:"issuer,omitempty"`
	Key    v1.Digest        `json:"key"`
	Proof  []v1.EvidenceRef `json:"proof"`
}

// Validate checks the member, the scheme, the digest key and the proof.
func (c MatchClaim) Validate() error {
	return first(within("member", c.Member.Validate()), required("scheme", c.Scheme), within("key", c.Key.Validate()), validateEvidence("proof", c.Proof))
}

// ManufacturerSerialClaim is the device-reported manufacturer and serial
// evidence of one member. It is a separate record, not a MatchClaim scheme. It is
// admitted through the consuming session's transactional publisher with that
// transaction's ownership stamp, and a driver attests only its own reported
// evidence. Missing or ambiguous evidence keeps its reason and infers no
// identity.
type ManufacturerSerialClaim struct {
	Member       NodeRef          `json:"member"`
	Manufacturer string           `json:"manufacturer"`
	Serial       string           `json:"serial"`
	Proof        []v1.EvidenceRef `json:"proof"`
}

// Validate checks the member and the proof. A missing component is representable
// and is judged when claims are resolved, not here.
func (c ManufacturerSerialClaim) Validate() error {
	return first(within("member", c.Member.Validate()), validateEvidence("proof", c.Proof))
}

// StatementRef is an operator statement as the trusted caller accepted it: its
// revision and the verified origin evidence for the exact pair and survivor. A
// payload cannot mark itself authenticated; the runtime accepts this origin only
// on its trusted configuration input.
type StatementRef struct {
	Revision v1.Uint64      `json:"revision"`
	Evidence v1.EvidenceRef `json:"evidence"`
}

// Validate checks the revision and the origin evidence.
func (s StatementRef) Validate() error {
	return first(within("revision", s.Revision.Validate()), within("evidence", s.Evidence.Validate()))
}

// CertifiedJoin is an authenticated, revisioned operator statement naming the
// exact pair and the survivor.
type CertifiedJoin struct {
	A         NodeRef      `json:"a"`
	B         NodeRef      `json:"b"`
	Survivor  NodeRef      `json:"survivor"`
	Statement StatementRef `json:"statement"`
}

// Validate checks the two members, requires them to differ, requires the
// survivor to be one of them and checks the statement.
func (j CertifiedJoin) Validate() error {
	if err := first(within("a", j.A.Validate()), within("b", j.B.Validate()), within("survivor", j.Survivor.Validate())); err != nil {
		return err
	}
	if j.A == j.B {
		return errors.New("a join names two different members")
	}
	if j.Survivor != j.A && j.Survivor != j.B {
		return errors.New("the survivor is neither member of the pair")
	}
	return within("statement", j.Statement.Validate())
}
