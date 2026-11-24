package identity

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// ErrNoCut refuses Resolve without a cut to pin.
var ErrNoCut = errors.New("identity: resolution needs a cut")

// FindingKind classifies a join that stays unapplied.
type FindingKind string

// FindingKind values.
const (
	// FindingDisputed: operator statements of one pair name different survivors.
	FindingDisputed FindingKind = "disputed"
	// FindingAmbiguous: one publisher attests the same identifier or the same
	// manufacturer and serial for more than one of its members.
	FindingAmbiguous FindingKind = "ambiguous"
	// FindingIncomplete: a reported manufacturer or serial is missing.
	FindingIncomplete FindingKind = "incomplete"
	// FindingMemberAbsent: an operator statement names a member that is not a
	// node of the ledger.
	FindingMemberAbsent FindingKind = "member_absent"
)

// Finding reports one join that is unapplied, with its members and the reason.
type Finding struct {
	Kind    FindingKind
	Members []driver.NodeRef
	Reason  string
}

// Member is one immutable member reference of a resolved resource with its keys.
type Member struct {
	Ref  driver.NodeRef
	Keys []driver.NodeKey
}

// Resolved is one resolved resource: the representative and every member. The
// representative is a view; the member references, their history and their
// stores are never re-keyed.
type Resolved struct {
	Representative driver.NodeRef
	Members        []Member
}

// Resolution is the result of Resolve at one pinned cut.
type Resolution struct {
	Cut       semreg.Digest
	Resources []Resolved
	Findings  []Finding
}

// Resolve computes the resolved resources from the validated claims and the
// operator statements at one cut. The result does not depend on the order the
// claims were admitted in. Two observations are joined only on evidence a driver
// attests (an identifier two publishers attest for their own members, or an equal
// complete manufacturer and serial they report) or on a certification an operator
// made and did not revoke; a certified pair is joined whatever the driver
// evidence says. The survivor is the certified one when exactly one is certified
// in the class, otherwise the smallest canonical member reference. A join the
// evidence does not support stays unapplied with a finding.
func (l *Ledger) Resolve(cut *graph.Cut) (Resolution, error) {
	if cut == nil {
		return Resolution{}, ErrNoCut
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	members := make([]driver.NodeRef, 0, len(l.nodes))
	for ref := range l.nodes {
		members = append(members, ref)
	}
	sortRefs(members)
	parent := make(map[driver.NodeRef]driver.NodeRef, len(members))
	for _, ref := range members {
		parent[ref] = ref
	}
	var find func(driver.NodeRef) driver.NodeRef
	find = func(ref driver.NodeRef) driver.NodeRef {
		for parent[ref] != ref {
			parent[ref] = parent[parent[ref]]
			ref = parent[ref]
		}
		return ref
	}
	union := func(a, b driver.NodeRef) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		if compareRef(ra, rb) > 0 {
			ra, rb = rb, ra
		}
		parent[rb] = ra
	}
	var findings []Finding

	certified := map[driver.NodeRef]bool{}
	for _, group := range l.certifiedPairs() {
		a, b := group.join.A, group.join.B
		switch {
		case !l.present(a) || !l.present(b):
			findings = append(findings, Finding{Kind: FindingMemberAbsent, Members: pairOf(a, b), Reason: "a member of the certified pair is not a node of the ledger"})
		case group.disputed:
			findings = append(findings, Finding{Kind: FindingDisputed, Members: pairOf(a, b), Reason: "operator statements of the pair name different survivors"})
		default:
			union(a, b)
			certified[group.join.Survivor] = true
		}
	}

	type attestation struct {
		member    driver.NodeRef
		publisher Publisher
	}
	groups := map[string][]attestation{}
	var groupKeys []string
	add := func(key string, a attestation) {
		if _, ok := groups[key]; !ok {
			groupKeys = append(groupKeys, key)
		}
		groups[key] = append(groups[key], a)
	}
	for member, record := range l.serials {
		manufacturer, serial := record.Claim.Manufacturer, record.Claim.Serial
		switch {
		case manufacturer == "" && serial == "":
			findings = append(findings, Finding{Kind: FindingIncomplete, Members: []driver.NodeRef{member}, Reason: "the reported manufacturer and serial are missing"})
		case manufacturer == "":
			findings = append(findings, Finding{Kind: FindingIncomplete, Members: []driver.NodeRef{member}, Reason: "the reported manufacturer is missing"})
		case serial == "":
			findings = append(findings, Finding{Kind: FindingIncomplete, Members: []driver.NodeRef{member}, Reason: "the reported serial is missing"})
		default:
			add(groupKey("manufacturer-serial", manufacturer, serial), attestation{member, record.Publisher})
		}
	}
	for key, record := range l.matches {
		add(groupKey("match", key.scheme, key.issuer, string(record.Claim.Key)), attestation{key.member, record.Publisher})
	}
	sort.Strings(groupKeys)
	for _, key := range groupKeys {
		attested := groups[key]
		perPublisher := map[Publisher]int{}
		for _, a := range attested {
			perPublisher[a.publisher]++
		}
		refs := make([]driver.NodeRef, len(attested))
		for i, a := range attested {
			refs[i] = a.member
		}
		sortRefs(refs)
		ambiguous := false
		for _, count := range perPublisher {
			if count > 1 {
				ambiguous = true
			}
		}
		switch {
		case ambiguous:
			kind := "the same identifier"
			if strings.HasPrefix(key, "manufacturer-serial") {
				kind = "the same manufacturer and serial"
			}
			findings = append(findings, Finding{Kind: FindingAmbiguous, Members: refs, Reason: "a publisher attests " + kind + " for more than one of its members"})
		case len(perPublisher) >= 2:
			for _, ref := range refs[1:] {
				union(refs[0], ref)
			}
		}
	}

	classes := map[driver.NodeRef][]driver.NodeRef{}
	for _, ref := range members {
		root := find(ref)
		classes[root] = append(classes[root], ref)
	}
	out := Resolution{Cut: cut.ID()}
	for _, class := range classes {
		sortRefs(class)
		representative := class[0]
		var survivors []driver.NodeRef
		for _, ref := range class {
			if certified[ref] {
				survivors = append(survivors, ref)
			}
		}
		if len(survivors) == 1 {
			representative = survivors[0]
		}
		resolved := Resolved{Representative: representative}
		for _, ref := range class {
			resolved.Members = append(resolved.Members, Member{Ref: ref, Keys: append([]driver.NodeKey(nil), l.nodes[ref]...)})
		}
		out.Resources = append(out.Resources, resolved)
	}
	sort.Slice(out.Resources, func(i, j int) bool {
		return compareRef(out.Resources[i].Members[0].Ref, out.Resources[j].Members[0].Ref) < 0
	})
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return compareRefs(findings[i].Members, findings[j].Members) < 0
	})
	out.Findings = findings
	return out, nil
}

func pairOf(a, b driver.NodeRef) []driver.NodeRef {
	pair := []driver.NodeRef{a, b}
	sortRefs(pair)
	return pair
}

func compareRefs(a, b []driver.NodeRef) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareRef(a[i], b[i]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

// pairGroup is the set of unrevoked operator statements of one unordered pair.
type pairGroup struct {
	join     driver.CertifiedJoin
	disputed bool
}

// certifiedPairs groups the unrevoked certifications by unordered pair in
// canonical order. A pair whose statements name different survivors is disputed.
func (l *Ledger) certifiedPairs() []pairGroup {
	var groups []pairGroup
	for _, record := range l.certs {
		if record.Revoked {
			continue
		}
		join := record.Join
		a, b := pairOf(join.A, join.B)[0], pairOf(join.A, join.B)[1]
		join.A, join.B = a, b
		merged := false
		for i := range groups {
			if samePair(groups[i].join, join) {
				if groups[i].join.Survivor != join.Survivor {
					groups[i].disputed = true
				}
				merged = true
			}
		}
		if !merged {
			groups = append(groups, pairGroup{join: join})
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i].join, groups[j].join
		if c := compareRef(a.A, b.A); c != 0 {
			return c < 0
		}
		return compareRef(a.B, b.B) < 0
	})
	return groups
}

// groupKey joins the parts of an equivalence class key with a length before each
// part, so that two unequal lists of parts never give one key whatever bytes the
// parts hold.
func groupKey(kind string, parts ...string) string {
	var b strings.Builder
	b.WriteString(kind)
	for _, part := range parts {
		fmt.Fprintf(&b, "\x00%d:%s", len(part), part)
	}
	return b.String()
}
