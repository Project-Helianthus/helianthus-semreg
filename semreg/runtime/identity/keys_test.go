package identity

import (
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Every field of a link key selects its own link. Each variant changes one field
// of the base and must stay a separate link with its own claim.
//
//	field               production mutation caught
//	type.pack.id        link text leaves the pack id out
//	type.pack.version   link text leaves the pack version out
//	type.id             link text leaves the relation id out
//	type.version        link text leaves the relation version out
//	from.bridge         link text leaves the From bridge out
//	from.id             link text leaves the From id out
//	to.bridge           link text leaves the To bridge out
//	to.id               link text leaves the To id out
//	direction           the tuple is stored unordered (From and To swapped)
func TestEveryFieldOfALinkKeySelectsItsOwnLink(t *testing.T) {
	base := driver.LinkKey{
		Type: semreg.DefinitionRef{Pack: semreg.PackRef{ID: "pack.base", Version: "1.0.0"}, ID: "relation.base", Version: "1.0.0"},
		From: driver.NodeRef{Bridge: "bridge.from", ID: "resource:from"}, To: driver.NodeRef{Bridge: "bridge.to", ID: "resource:to"},
	}
	variants := map[string]func(*driver.LinkKey){
		"type.pack.id":      func(k *driver.LinkKey) { k.Type.Pack.ID = "pack.other" },
		"type.pack.version": func(k *driver.LinkKey) { k.Type.Pack.Version = "1.0.1" },
		"type.id":           func(k *driver.LinkKey) { k.Type.ID = "relation.other" },
		"type.version":      func(k *driver.LinkKey) { k.Type.Version = "1.0.1" },
		"from.bridge":       func(k *driver.LinkKey) { k.From.Bridge = "bridge.other" },
		"from.id":           func(k *driver.LinkKey) { k.From.ID = "resource:other" },
		"to.bridge":         func(k *driver.LinkKey) { k.To.Bridge = "bridge.other" },
		"to.id":             func(k *driver.LinkKey) { k.To.ID = "resource:other" },
		"direction":         func(k *driver.LinkKey) { k.From, k.To = k.To, k.From },
	}
	l := ledgerOn(t, NewMemoryStore(), embeddedCatalog(t))
	p := Publisher{Driver: "driver-a", Instance: "a1"}
	if err := l.ClaimLink(p, driver.LinkClaim{Key: base, Basis: proof("base")}); err != nil {
		t.Fatal(err)
	}
	for name, vary := range variants {
		key := base
		vary(&key)
		if err := l.ClaimLink(p, driver.LinkClaim{Key: key, Basis: proof(name)}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := len(l.Claims(base)); got != 1 {
			t.Errorf("%s: the base link holds %d claims", name, got)
		}
		if got := len(l.Claims(key)); got != 1 {
			t.Errorf("%s: the variant holds %d claims", name, got)
		}
	}
	if got := len(l.Links()); got != len(variants)+1 {
		t.Fatalf("links = %d, want %d distinct", got, len(variants)+1)
	}
}

// The publisher of a claim is its driver kind and its instance key: either one
// alone keeps a second claim beside the first.
//
//	field     production mutation caught
//	driver    the claim slot is keyed by the instance only
//	instance  the claim slot is keyed by the driver kind only
func TestPublisherDriverAndInstanceEachKeepTheirOwnClaim(t *testing.T) {
	l := ledgerOn(t, NewMemoryStore(), embeddedCatalog(t))
	key := driver.LinkKey{Type: reach, From: ref("resource:a"), To: ref("resource:b")}
	for _, p := range []Publisher{{"driver-a", "a1"}, {"driver-b", "a1"}, {"driver-a", "a2"}} {
		if err := l.ClaimLink(p, driver.LinkClaim{Key: key, Basis: proof(string(p.Driver) + string(p.Instance))}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(l.Claims(key)); got != 3 {
		t.Fatalf("claims = %d, want one per publisher", got)
	}
}

// Identifier claims join only when the scheme, the issuer and the key are all
// equal, and manufacturer and serial claims only when both are equal as reported.
//
//	field         production mutation caught
//	scheme        the group key leaves the scheme out
//	issuer        the group key leaves the issuer out
//	key           the group key leaves the digest out
//	manufacturer  the group key leaves the manufacturer out
//	serial        the group key leaves the serial out
//	publisher     two members of one publisher count as two paths
func TestEveryFieldOfTheEvidenceGroupKeyDecidesTheJoin(t *testing.T) {
	catalog := catalogWith(t, func(doc map[string]any) {
		doc["identification_schemes"] = []any{
			map[string]any{"id": "scheme.alpha", "issuer_registry": "none", "token_form": "token", "admission": map[string]any{"arm": "owner_decision", "evidence": []any{"https://example.org/alpha"}}},
			map[string]any{"id": "scheme.beta", "issuer_registry": "registry.test", "token_form": "token", "admission": map[string]any{"arm": "owner_decision", "evidence": []any{"https://example.org/beta"}}},
		}
	})
	joined := func(p pair) bool {
		cut := cutOf(t, catalog, "complete", asset{id: "asset:cut"})
		out, err := p.l.Resolve(cut)
		if err != nil {
			t.Fatal(err)
		}
		return len(classOf(out, p.a)) == 2
	}
	match := func(scheme, issuer, key string) func(p pair) (driver.MatchClaim, driver.MatchClaim) {
		return func(p pair) (driver.MatchClaim, driver.MatchClaim) {
			return driver.MatchClaim{Member: p.a, Scheme: "scheme.beta", Issuer: "issuer-1", Key: sumOf("k"), Proof: proof("a")},
				driver.MatchClaim{Member: p.b, Scheme: scheme, Issuer: issuer, Key: sumOf(key), Proof: proof("b")}
		}
	}
	for name, tc := range map[string]struct {
		claims func(pair) (driver.MatchClaim, driver.MatchClaim)
		want   bool
	}{
		"all equal":      {match("scheme.beta", "issuer-1", "k"), true},
		"another scheme": {match("scheme.alpha", "", "k"), false},
		"another issuer": {match("scheme.beta", "issuer-2", "k"), false},
		"another key":    {match("scheme.beta", "issuer-1", "other"), false},
	} {
		p := newPair(t, catalog, "resource.inverter")
		first, second := tc.claims(p)
		if err := p.l.AdmitMatch(p.pa, first); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := p.l.AdmitMatch(p.pb, second); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := joined(p); got != tc.want {
			t.Errorf("%s: joined = %v, want %v", name, got, tc.want)
		}
	}
	for name, tc := range map[string]struct {
		manufacturer, serial string
		want                 bool
	}{
		"both equal":                   {"Synthetic Maker", "SYN-1", true},
		"another manufacturer":         {"Other Maker", "SYN-1", false},
		"another serial":               {"Synthetic Maker", "SYN-2", false},
		"manufacturer in another case": {"synthetic maker", "SYN-1", false},
	} {
		p := newPair(t, catalog, "resource.inverter")
		_ = p.l.AdmitManufacturerSerial(p.pa, serialClaim(p.a, "Synthetic Maker", "SYN-1", "a"))
		_ = p.l.AdmitManufacturerSerial(p.pb, serialClaim(p.b, tc.manufacturer, tc.serial, "b"))
		if got := joined(p); got != tc.want {
			t.Errorf("%s: joined = %v, want %v", name, got, tc.want)
		}
	}
	// Two members of one publisher with an equal pair are one path, not two.
	p := newPair(t, catalog, "resource.inverter")
	sibling := mustNode(t, p.l, bridge, nativeKey(t, installation, pubSun.Driver, pubSun.Instance, "resource.inverter", "sibling"))
	_ = p.l.AdmitManufacturerSerial(p.pa, serialClaim(p.a, "Synthetic Maker", "SYN-1", "a"))
	_ = p.l.AdmitManufacturerSerial(p.pa, serialClaim(sibling, "Synthetic Maker", "SYN-1", "sibling"))
	if joined(p) {
		t.Error("two members of one publisher joined")
	}
}

// Every field of a statement identifies it: a changed survivor, revision or
// origin evidence is another statement and is recorded; the same pair in the other
// order is the same statement and changes nothing.
//
//	field              production mutation caught
//	pair (A, B)        statements are compared by A only
//	pair order         the swapped pair is recorded as another statement
//	survivor           statements are compared without the survivor
//	statement.revision statements are compared without the revision
//	statement.evidence statements are compared without the origin evidence
func TestEveryFieldOfAStatementIdentifiesIt(t *testing.T) {
	p := newPair(t, embeddedCatalog(t), "resource.inverter")
	third := mustNode(t, p.l, bridge, nativeKey(t, installation, "driver-x", "x1", "resource.inverter", "third"))
	base := driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.a, Statement: statement("1", "base")}
	if err := p.l.Certify(base); err != nil {
		t.Fatal(err)
	}
	swapped := base
	swapped.A, swapped.B = base.B, base.A
	otherEvidence := base
	otherEvidence.Statement.Evidence = proof("other origin")[0]
	for name, tc := range map[string]struct {
		join driver.CertifiedJoin
		adds bool
	}{
		"pair order":         {swapped, false},
		"pair b":             {driver.CertifiedJoin{A: p.a, B: third, Survivor: p.a, Statement: base.Statement}, true},
		"pair a":             {driver.CertifiedJoin{A: third, B: p.b, Survivor: third, Statement: base.Statement}, true},
		"survivor":           {driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.b, Statement: base.Statement}, true},
		"statement revision": {driver.CertifiedJoin{A: p.a, B: p.b, Survivor: p.a, Statement: statement("2", "base")}, true},
		"statement evidence": {otherEvidence, true},
	} {
		before := len(p.l.certs)
		if err := p.l.Certify(tc.join); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if grew := len(p.l.certs) > before; grew != tc.adds {
			t.Errorf("%s: recorded = %v, want %v", name, grew, tc.adds)
		}
	}
}
