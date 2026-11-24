package identity

import (
	"errors"
	"fmt"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Errors of operator statements.
var (
	ErrUnknownStatement = errors.New("identity: no recorded statement names this pair")
	ErrWrongSurvivor    = errors.New("identity: the recorded statements of this pair name another survivor")
	ErrWrongStatement   = errors.New("identity: the recorded statements of this pair and survivor are other statements")
	ErrWrongRebindPair  = errors.New("identity: the statement does not name the old and the new identity")
	ErrRebindSurvivor   = errors.New("identity: a rebind names the new identity as the survivor")
	ErrRebindConflict   = errors.New("identity: another rebind already names the old or the new identity")
	ErrRebindCycle      = errors.New("identity: the rebind would close a cycle")
)

// certRecord is one recorded certification. Revoked is the audited disposition
// of an explicit revocation; the statement stays with it.
type certRecord struct {
	Join    driver.CertifiedJoin `json:"join"`
	Revoked bool                 `json:"revoked"`
}

// rebindRecord is one recorded rebind with the statement a person made it by.
type rebindRecord struct {
	Old     driver.NodeRef       `json:"old"`
	New     driver.NodeRef       `json:"new"`
	Join    driver.CertifiedJoin `json:"join"`
	Revoked bool                 `json:"revoked"`
}

// samePair reports whether two joins name the same unordered pair of members.
func samePair(a, b driver.CertifiedJoin) bool {
	return (a.A == b.A && a.B == b.B) || (a.A == b.B && a.B == b.A)
}

func sameStatement(a, b driver.CertifiedJoin) bool {
	return samePair(a, b) && a.Survivor == b.Survivor && a.Statement == b.Statement
}

// Certify records an operator's certification of a join of the exact pair with
// the named survivor. The statement is validated here and is accepted as the
// trusted caller's: the driver-facing publication port offers no entry to it, and
// no payload marks itself authenticated. A second identical statement changes
// nothing, and neither does a statement that an explicit revocation already
// records. A pair whose members are not yet nodes of the ledger is retained and
// applies when both are.
func (l *Ledger) Certify(join driver.CertifiedJoin) error {
	if err := join.Validate(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.certs {
		if sameStatement(record.Join, join) {
			return nil
		}
	}
	l.certs = append(l.certs, certRecord{Join: join})
	return nil
}

// Rebind moves the links of the old identity to the new one, by the statement of
// a person. The statement names the pair {old, new} and the new identity as the
// survivor. The old identity stays resolvable with its history, and the move is
// a view: no link claim is re-keyed. A second identical statement changes
// nothing. A rebind whose identities are not yet nodes of the ledger is retained
// and applies when both are.
func (l *Ledger) Rebind(old, new driver.NodeRef, join driver.CertifiedJoin) error {
	if err := first(old.Validate(), new.Validate(), join.Validate()); err != nil {
		return err
	}
	if !((join.A == old && join.B == new) || (join.A == new && join.B == old)) {
		return ErrWrongRebindPair
	}
	if join.Survivor != new {
		return ErrRebindSurvivor
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.rebinds {
		if record.Old == old && record.New == new && sameStatement(record.Join, join) {
			return nil
		}
	}
	for _, record := range l.rebinds {
		if record.Revoked || (record.Old == old && record.New == new) {
			continue
		}
		if record.Old == old || record.New == new {
			return ErrRebindConflict
		}
	}
	if l.reaches(new, old) {
		return ErrRebindCycle
	}
	l.rebinds = append(l.rebinds, rebindRecord{Old: old, New: new, Join: join})
	return nil
}

// reaches reports whether following the applied rebinds from start arrives at
// target.
func (l *Ledger) reaches(start, target driver.NodeRef) bool {
	for at, steps := start, 0; steps <= len(l.rebinds); steps++ {
		if at == target {
			return true
		}
		next, moved := l.rebound(at)
		if !moved {
			return false
		}
		at = next
	}
	return false
}

// rebound returns the identity an applied rebind moved ref to.
func (l *Ledger) rebound(ref driver.NodeRef) (driver.NodeRef, bool) {
	for _, record := range l.rebinds {
		if !record.Revoked && record.Old == ref && l.present(record.Old) && l.present(record.New) {
			return record.New, true
		}
	}
	return driver.NodeRef{}, false
}

// effective returns a link key with each endpoint moved by the applied rebinds.
func (l *Ledger) effective(key driver.LinkKey) driver.LinkKey {
	move := func(ref driver.NodeRef) driver.NodeRef {
		for steps := 0; steps <= len(l.rebinds); steps++ {
			next, moved := l.rebound(ref)
			if !moved {
				break
			}
			ref = next
		}
		return ref
	}
	key.From, key.To = move(key.From), move(key.To)
	return key
}

// Rebinds returns the recorded rebinds with their statements, in the order they
// were recorded, for the owners of routes and settings to follow.
func (l *Ledger) Rebinds() []Rebind {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Rebind, 0, len(l.rebinds))
	for _, record := range l.rebinds {
		out = append(out, Rebind{Old: record.Old, New: record.New, Statement: record.Join, Revoked: record.Revoked})
	}
	return out
}

// Rebind is one recorded rebind.
type Rebind struct {
	Old, New  driver.NodeRef
	Statement driver.CertifiedJoin
	Revoked   bool
}

// Revoke records an explicit revocation of the certified join the statement
// names, and of a rebind made with the same statement. The statement is the
// exact original: a pair, a survivor or a statement that no record holds is
// refused. A revoked join is not in the set Resolve uses, a revoked rebind moves
// no link, and every member identity stays resolvable. The revocation stays with
// the statement and is part of the checkpoint. A second identical statement
// changes nothing.
func (l *Ledger) Revoke(join driver.CertifiedJoin) error {
	if err := join.Validate(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	pair, survivor, exact := false, false, false
	for i := range l.certs {
		record := &l.certs[i]
		switch {
		case sameStatement(record.Join, join):
			pair, survivor, exact = true, true, true
			record.Revoked = true
		case samePair(record.Join, join) && record.Join.Survivor == join.Survivor:
			pair, survivor = true, true
		case samePair(record.Join, join):
			pair = true
		}
	}
	for i := range l.rebinds {
		record := &l.rebinds[i]
		switch {
		case sameStatement(record.Join, join):
			pair, survivor, exact = true, true, true
			record.Revoked = true
		case samePair(record.Join, join) && record.Join.Survivor == join.Survivor:
			pair, survivor = true, true
		case samePair(record.Join, join):
			pair = true
		}
	}
	switch {
	case exact:
		return nil
	case survivor:
		return ErrWrongStatement
	case pair:
		return ErrWrongSurvivor
	}
	return fmt.Errorf("%w", ErrUnknownStatement)
}
