package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

var (
	pubSun = Publisher{Driver: "driver-pv", Instance: "p1"}
	pubEE  = Publisher{Driver: "driver-ee", Instance: "e1"}
)

// catalogWith decodes the embedded catalog after a change, for the tests that
// need a listed scheme or the site relation row.
func catalogWith(t *testing.T, change func(doc map[string]any)) *graph.Catalog {
	t.Helper()
	base, err := graph.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(base.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	change(doc)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := graph.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return catalog
}

func listedScheme(doc map[string]any) {
	doc["identification_schemes"] = []any{map[string]any{
		"id": "scheme.synthetic", "issuer_registry": "none", "token_form": "synthetic token",
		"admission": map[string]any{"arm": "owner_decision", "evidence": []any{"https://example.org/scheme"}},
	}}
}

// pair is two members of one device seen over two protocols, each under its own
// native anchor.
type pair struct {
	l       *Ledger
	a, b    driver.NodeRef
	pa, pb  Publisher
	catalog *graph.Catalog
}

func newPair(t *testing.T, catalog *graph.Catalog, kind semreg.DefinitionID) pair {
	t.Helper()
	l := ledgerOn(t, NewMemoryStore(), catalog)
	return pair{
		l: l, catalog: catalog, pa: pubSun, pb: pubEE,
		a: mustNode(t, l, bridge, nativeKey(t, installation, pubSun.Driver, pubSun.Instance, kind, "sunspec-native-1")),
		b: mustNode(t, l, bridge, nativeKey(t, installation, pubEE.Driver, pubEE.Instance, kind, "eebus-native-1")),
	}
}

func serialClaim(member driver.NodeRef, manufacturer, serial, label string) driver.ManufacturerSerialClaim {
	return driver.ManufacturerSerialClaim{Member: member, Manufacturer: manufacturer, Serial: serial, Proof: proof(label)}
}

func (p pair) resolve(t *testing.T) Resolution {
	t.Helper()
	cut := cutOf(t, p.catalog, graph.CutComplete, asset{id: "asset:cut"})
	out, err := p.l.Resolve(cut)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func classOf(out Resolution, member driver.NodeRef) []driver.NodeRef {
	for _, resource := range out.Resources {
		for _, m := range resource.Members {
			if m.Ref == member {
				var refs []driver.NodeRef
				for _, m := range resource.Members {
					refs = append(refs, m.Ref)
				}
				return refs
			}
		}
	}
	return nil
}

// TestSunSpecEEBusInverterPairMeetsAtResolve is the two-protocol case: a native
// SunSpec inverter member and a remote eeBUS inverter member, each with the claim
// record its producer emits for the same complete manufacturer and serial, join
// into one resolved resource with both member references, in either insertion
// order. The pair is a synthetic literal: no frozen partner source carries one
// device on both protocols.
func TestSunSpecEEBusInverterPairMeetsAtResolve(t *testing.T) {
	const manufacturer, serial = "Synthetic Maker", "SYN-0001"
	var results [2]Resolution
	for i, reversed := range []bool{false, true} {
		p := newPair(t, embeddedCatalog(t), "resource.inverter")
		steps := []func() error{
			func() error {
				return p.l.AdmitManufacturerSerial(p.pa, serialClaim(p.a, manufacturer, serial, "sunspec common model"))
			},
			func() error {
				return p.l.AdmitManufacturerSerial(p.pb, serialClaim(p.b, manufacturer, serial, "eebus device classification"))
			},
		}
		if reversed {
			steps[0], steps[1] = steps[1], steps[0]
		}
		for _, step := range steps {
			if err := step(); err != nil {
				t.Fatal(err)
			}
		}
		results[i] = p.resolve(t)
		class := classOf(results[i], p.a)
		if len(results[i].Resources) != 1 || len(class) != 2 || !reflect.DeepEqual(class, classOf(results[i], p.b)) {
			t.Fatalf("order %v: resources=%+v", reversed, results[i].Resources)
		}
		if got := results[i].Resources[0].Representative; got != class[0] {
			t.Fatalf("representative = %v, want the smallest canonical member %v", got, class[0])
		}
		if len(results[i].Findings) != 0 {
			t.Fatalf("findings = %+v", results[i].Findings)
		}
	}
	if !reflect.DeepEqual(results[0], results[1]) {
		t.Fatalf("opposite insertion orders differ:\n%+v\n%+v", results[0], results[1])
	}
}

// An incomplete pair, a missing manufacturer or a missing serial, stays two
// resources with its reason and infers no identity (J-MFR-SERIAL-MISSING).
func TestIncompletePairStaysTwoResourcesWithItsReason(t *testing.T) {
	for name, tc := range map[string]struct {
		manufacturer, serial, reason string
	}{
		"missing manufacturer": {"", "SYN-0001", "the reported manufacturer is missing"},
		"missing serial":       {"Synthetic Maker", "", "the reported serial is missing"},
		"missing both":         {"", "", "the reported manufacturer and serial are missing"},
	} {
		p := newPair(t, embeddedCatalog(t), "resource.inverter")
		if err := p.l.AdmitManufacturerSerial(p.pa, serialClaim(p.a, tc.manufacturer, tc.serial, "first")); err != nil {
			t.Fatal(err)
		}
		if err := p.l.AdmitManufacturerSerial(p.pb, serialClaim(p.b, "Synthetic Maker", "SYN-0001", "second")); err != nil {
			t.Fatal(err)
		}
		out := p.resolve(t)
		if len(out.Resources) != 2 || len(out.Findings) != 1 || out.Findings[0].Kind != FindingIncomplete ||
			out.Findings[0].Reason != tc.reason || !reflect.DeepEqual(out.Findings[0].Members, []driver.NodeRef{p.a}) {
			t.Errorf("%s: %+v", name, out)
		}
	}
}

// Without a proof and without a certification the two observations stay two
// resources, and a name or a quantity resemblance is no evidence (J-NO-PROOF).
func TestNoProofKeepsTwoResources(t *testing.T) {
	p := newPair(t, embeddedCatalog(t), "resource.inverter")
	out := p.resolve(t)
	if len(out.Resources) != 2 || len(out.Findings) != 0 {
		t.Fatalf("%+v", out)
	}
	// Different pairs do not join.
	_ = p.l.AdmitManufacturerSerial(p.pa, serialClaim(p.a, "Synthetic Maker", "SYN-0001", "one"))
	_ = p.l.AdmitManufacturerSerial(p.pb, serialClaim(p.b, "Synthetic Maker", "SYN-0002", "two"))
	if out := p.resolve(t); len(out.Resources) != 2 {
		t.Fatalf("different serials joined: %+v", out)
	}
}

// One publisher attesting the same pair for two of its members is ambiguous: a
// finding and no join (J-MFR-SERIAL-AMBIGUOUS, J-AMBIGUOUS).
func TestAmbiguousProofGivesAFindingAndNoJoin(t *testing.T) {
	p := newPair(t, embeddedCatalog(t), "resource.inverter")
	second := mustNode(t, p.l, bridge, nativeKey(t, installation, pubSun.Driver, pubSun.Instance, "resource.inverter", "sunspec-native-2"))
	for member, publisher := range map[driver.NodeRef]Publisher{p.a: p.pa, second: p.pa, p.b: p.pb} {
		if err := p.l.AdmitManufacturerSerial(publisher, serialClaim(member, "Synthetic Maker", "SYN-0001", "claim")); err != nil {
			t.Fatal(err)
		}
	}
	out := p.resolve(t)
	if len(out.Resources) != 3 || len(out.Findings) != 1 || out.Findings[0].Kind != FindingAmbiguous || len(out.Findings[0].Members) != 3 || out.Findings[0].Reason == "" {
		t.Fatalf("%+v", out)
	}
}

// A claim that names another publisher's member, or lacks valid evidence, is not
// validated and leaves membership unchanged, whether it is a manufacturer and
// serial claim or an identifier claim.
func TestClaimsOfAnotherPublishersMemberOrWithoutEvidenceChangeNothing(t *testing.T) {
	catalog := catalogWith(t, listedScheme)
	p := newPair(t, catalog, "resource.inverter")
	otherInstance := mustNode(t, p.l, bridge, nativeKey(t, installation, pubSun.Driver, "p2", "resource.inverter", "same-driver-other-instance"))
	before, _ := p.l.Export()
	match := func(member driver.NodeRef, label string) driver.MatchClaim {
		return driver.MatchClaim{Member: member, Scheme: "scheme.synthetic", Key: sumOf("device"), Proof: proof(label)}
	}
	errs := map[string]error{
		"serial of the other publisher's member":        p.l.AdmitManufacturerSerial(p.pa, serialClaim(p.b, "Synthetic Maker", "SYN-0001", "x")),
		"serial of another instance of the same driver": p.l.AdmitManufacturerSerial(p.pa, serialClaim(otherInstance, "Synthetic Maker", "SYN-0001", "x")),
		"serial without proof":                          p.l.AdmitManufacturerSerial(p.pa, driver.ManufacturerSerialClaim{Member: p.a, Manufacturer: "Synthetic Maker", Serial: "SYN-0001"}),
		"serial of an unknown member":                   p.l.AdmitManufacturerSerial(p.pa, serialClaim(ref("resource:unknown"), "Synthetic Maker", "SYN-0001", "x")),
		"match of the other publisher's member":         p.l.AdmitMatch(p.pa, match(p.b, "x")),
		"match without proof":                           p.l.AdmitMatch(p.pa, driver.MatchClaim{Member: p.a, Scheme: "scheme.synthetic", Key: sumOf("device")}),
	}
	want := map[string]error{
		"serial of the other publisher's member": ErrNotOwner, "serial of another instance of the same driver": ErrNotOwner, "serial of an unknown member": ErrUnknownMember,
		"match of the other publisher's member": ErrNotOwner,
	}
	for name, err := range errs {
		if err == nil || (want[name] != nil && !errors.Is(err, want[name])) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if after, _ := p.l.Export(); !bytes.Equal(before, after) {
		t.Fatal("a refused claim changed the identity state")
	}
	if out := p.resolve(t); len(out.Resources) != 3 || len(out.Findings) != 0 {
		t.Fatalf("membership changed: %+v", out)
	}
}

// A catalog-backed identifier claim with a listed scheme, from two drivers that
// each name their own member, joins them. A scheme the catalog does not list is a
// stop and mints no scheme id, and the manufacturer and serial join does not wait
// on a listed scheme.
func TestMatchClaimsJoinOnAListedSchemeOnly(t *testing.T) {
	p := newPair(t, catalogWith(t, listedScheme), "resource.inverter")
	device := sumOf("host key of one device")
	for member, publisher := range map[driver.NodeRef]Publisher{p.a: p.pa, p.b: p.pb} {
		if err := p.l.AdmitMatch(publisher, driver.MatchClaim{Member: member, Scheme: "scheme.synthetic", Key: device, Proof: proof("listed")}); err != nil {
			t.Fatal(err)
		}
	}
	if class := classOf(p.resolve(t), p.a); len(class) != 2 {
		t.Fatalf("listed scheme did not join: %+v", class)
	}
	err := p.l.AdmitMatch(p.pa, driver.MatchClaim{Member: p.a, Scheme: "scheme.unlisted", Key: device, Proof: proof("unlisted")})
	if !errors.Is(err, ErrSchemeNotListed) {
		t.Fatalf("unlisted scheme: err=%v", err)
	}
	if issuer := p.l.AdmitMatch(p.pa, driver.MatchClaim{Member: p.a, Scheme: "scheme.synthetic", Issuer: "registry-1", Key: device, Proof: proof("issuer")}); !errors.Is(issuer, ErrIssuer) {
		t.Fatalf("issuer on a globally unique scheme: err=%v", issuer)
	}

	// The empty identification scheme list of the frozen catalog lists nothing, and the
	// manufacturer and serial join runs there.
	frozen := newPair(t, embeddedCatalog(t), "resource.inverter")
	if err := frozen.l.AdmitMatch(frozen.pa, driver.MatchClaim{Member: frozen.a, Scheme: "scheme.synthetic", Key: device, Proof: proof("frozen")}); !errors.Is(err, ErrSchemeNotListed) {
		t.Fatalf("frozen catalog: err=%v", err)
	}
	_ = frozen.l.AdmitManufacturerSerial(frozen.pa, serialClaim(frozen.a, "Synthetic Maker", "SYN-0001", "a"))
	_ = frozen.l.AdmitManufacturerSerial(frozen.pb, serialClaim(frozen.b, "Synthetic Maker", "SYN-0001", "b"))
	if class := classOf(frozen.resolve(t), frozen.a); len(class) != 2 {
		t.Fatalf("manufacturer and serial waited on a scheme: %+v", class)
	}
}

// Opposite publication orders of identifier claims give the same classes (J-ORDER).
func TestOppositeOrdersGiveTheSameClasses(t *testing.T) {
	var results [2]Resolution
	for i, reversed := range []bool{false, true} {
		p := newPair(t, catalogWith(t, listedScheme), "resource.inverter")
		claims := []func() error{
			func() error {
				return p.l.AdmitMatch(p.pa, driver.MatchClaim{Member: p.a, Scheme: "scheme.synthetic", Key: sumOf("k"), Proof: proof("one")})
			},
			func() error {
				return p.l.AdmitMatch(p.pb, driver.MatchClaim{Member: p.b, Scheme: "scheme.synthetic", Key: sumOf("k"), Proof: proof("two")})
			},
		}
		if reversed {
			claims[0], claims[1] = claims[1], claims[0]
		}
		for _, claim := range claims {
			if err := claim(); err != nil {
				t.Fatal(err)
			}
		}
		results[i] = p.resolve(t)
	}
	if !reflect.DeepEqual(results[0], results[1]) || len(results[0].Resources) != 1 {
		t.Fatalf("%+v vs %+v", results[0], results[1])
	}
}

// A valid explicit CertifiedJoin joins a pair whose native anchors differ and
// that has no driver evidence; the certified survivor is the representative; a
// replay changes nothing; the pre-existing operator-certified EVSE case is its own
// case (J-MATCH, J-REPLAY).
func TestCertifiedJoinTakesPrecedenceAndReplaysIdempotently(t *testing.T) {
	p := newPair(t, embeddedCatalog(t), "resource.charging_station")
	if p.a.ID == p.b.ID {
		t.Fatal("the native anchors do not differ")
	}
	join := driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.b, Statement: statement("1", "evse pair")}
	if err := p.l.Certify(join); err != nil {
		t.Fatal(err)
	}
	before, _ := p.l.Export()
	if err := p.l.Certify(join); err != nil {
		t.Fatal(err)
	}
	swapped := join
	swapped.A, swapped.B = join.B, join.A
	if err := p.l.Certify(swapped); err != nil {
		t.Fatal(err)
	}
	if after, _ := p.l.Export(); !bytes.Equal(before, after) {
		t.Fatal("a replayed certification changed the state")
	}
	out := p.resolve(t)
	if len(out.Resources) != 1 || out.Resources[0].Representative != p.b || len(out.Resources[0].Members) != 2 {
		t.Fatalf("%+v", out)
	}
	for _, m := range out.Resources[0].Members {
		if _, ok := p.l.Lookup(m.Ref); !ok {
			t.Fatalf("member %v is not resolvable", m.Ref)
		}
	}
}

// A certification takes precedence over contradictory driver evidence: the
// members report different serials and the override still joins them. Without it,
// the ambiguous driver proofs stay unapplied with a finding.
func TestOperatorStatementOverridesContradictoryDriverEvidence(t *testing.T) {
	p := newPair(t, embeddedCatalog(t), "resource.inverter")
	_ = p.l.AdmitManufacturerSerial(p.pa, serialClaim(p.a, "Synthetic Maker", "SYN-0001", "one"))
	_ = p.l.AdmitManufacturerSerial(p.pb, serialClaim(p.b, "Synthetic Maker", "SYN-0002", "two"))
	if out := p.resolve(t); len(out.Resources) != 2 {
		t.Fatalf("contradictory evidence joined: %+v", out)
	}
	if err := p.l.Certify(driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.a, Statement: statement("1", "override")}); err != nil {
		t.Fatal(err)
	}
	out := p.resolve(t)
	if len(out.Resources) != 1 || out.Resources[0].Representative != p.a {
		t.Fatalf("%+v", out)
	}
}

// Statements of one pair that name different survivors are disputed: unapplied
// with a finding. An unknown member is retained without inventing it.
func TestMutuallyInconsistentStatementsStayDisputed(t *testing.T) {
	p := newPair(t, embeddedCatalog(t), "resource.inverter")
	_ = p.l.Certify(driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.a, Statement: statement("1", "first")})
	_ = p.l.Certify(driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.b, Statement: statement("2", "second")})
	out := p.resolve(t)
	if len(out.Resources) != 2 || len(out.Findings) != 1 || out.Findings[0].Kind != FindingDisputed {
		t.Fatalf("%+v", out)
	}
	absent := driver.CertifiedJoin{A: p.a, B: ref("resource:not-yet"), Survivor: p.a, Statement: statement("3", "absent")}
	if err := p.l.Certify(absent); err != nil {
		t.Fatal(err)
	}
	out = p.resolve(t)
	if len(out.Resources) != 2 || len(out.Findings) != 2 {
		t.Fatalf("%+v", out)
	}
}

// A revocation is recorded with its statement, the revoked join is not applied,
// both members stay resolvable, a replay changes nothing, and a wrong pair, a
// wrong survivor or a wrong statement is refused.
func TestRevocationIsRecordedAndUnappliesTheJoin(t *testing.T) {
	p := newPair(t, embeddedCatalog(t), "resource.inverter")
	join := driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.a, Statement: statement("1", "certify")}
	if err := p.l.Certify(join); err != nil {
		t.Fatal(err)
	}
	other := mustNode(t, p.l, bridge, nativeKey(t, installation, "driver-x", "x1", "resource.inverter", "third"))
	wrongPair := driver.CertifiedJoin{A: p.a, B: other, Survivor: p.a, Statement: join.Statement}
	wrongSurvivor := driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.b, Statement: join.Statement}
	wrongStatement := driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.a, Statement: statement("2", "other")}
	for name, tc := range map[string]struct {
		join driver.CertifiedJoin
		want error
	}{"pair": {wrongPair, ErrUnknownStatement}, "survivor": {wrongSurvivor, ErrWrongSurvivor}, "statement": {wrongStatement, ErrWrongStatement}} {
		if err := p.l.Revoke(tc.join); !errors.Is(err, tc.want) {
			t.Errorf("wrong %s: err=%v, want %v", name, err, tc.want)
		}
	}
	if out := p.resolve(t); len(classOf(out, p.a)) != 2 {
		t.Fatalf("refused revocations changed the join: %+v", out)
	}
	if err := p.l.Revoke(join); err != nil {
		t.Fatal(err)
	}
	revoked, _ := p.l.Export()
	if err := p.l.Revoke(join); err != nil {
		t.Fatal(err)
	}
	if again, _ := p.l.Export(); !bytes.Equal(revoked, again) {
		t.Fatal("a second revocation changed the state")
	}
	if len(classOf(p.resolve(t), p.a)) != 1 {
		t.Fatal("the revoked join is still applied")
	}
	for _, member := range []driver.NodeRef{p.a, p.b} {
		if _, ok := p.l.Lookup(member); !ok {
			t.Fatalf("member %v is not resolvable after the revocation", member)
		}
	}
	// Replaying the original statement does not bring the join back.
	if err := p.l.Certify(join); err != nil {
		t.Fatal(err)
	}
	if len(classOf(p.resolve(t), p.a)) != 1 {
		t.Fatal("the replayed statement revived the revoked join")
	}
	// A new statement of the same pair is a new certification.
	if err := p.l.Certify(driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.a, Statement: statement("3", "again")}); err != nil {
		t.Fatal(err)
	}
	if len(classOf(p.resolve(t), p.a)) != 2 {
		t.Fatal("a new statement did not join")
	}
}

// Unequal reported pairs stay in different classes whatever bytes the strings
// hold: a separator inside a string never moves a byte from one field to the
// other.
func TestUnequalPairsWithSeparatorBytesStayTwoResources(t *testing.T) {
	p := newPair(t, embeddedCatalog(t), "resource.inverter")
	if err := p.l.AdmitManufacturerSerial(p.pa, serialClaim(p.a, "ACME", "\x001", "first")); err != nil {
		t.Skipf("the claim record refuses the bytes: %v", err)
	}
	if err := p.l.AdmitManufacturerSerial(p.pb, serialClaim(p.b, "ACME\x00", "1", "second")); err != nil {
		t.Skipf("the claim record refuses the bytes: %v", err)
	}
	if out := p.resolve(t); len(out.Resources) != 2 || len(out.Findings) != 0 {
		t.Fatalf("%+v", out)
	}
}
