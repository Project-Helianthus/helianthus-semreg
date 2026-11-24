package driver_test

import (
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func nativeAnchor() driver.AnchorKey {
	return driver.AnchorKey{Scheme: "scheme.test.serial", Key: digestOf("token")}
}

func slotAnchor() driver.AnchorKey {
	return driver.AnchorKey{Parent: "resource:parent", Slot: "slot.one"}
}

func nodeKey(anchor driver.AnchorKey) driver.NodeKey {
	return driver.NodeKey{Installation: "installation.one", Driver: "driver.test", Instance: "instance.one", Kind: "resource.equipment", Anchor: anchor}
}

func TestNodeKeyAcceptsBothAnchorForms(t *testing.T) {
	wantValid(t, "native anchor", nodeKey(nativeAnchor()).Validate())
	withIssuer := nativeAnchor()
	withIssuer.Issuer = "issuer.test"
	wantValid(t, "native anchor with issuer", nodeKey(withIssuer).Validate())
	wantValid(t, "configured anchor with parent", nodeKey(slotAnchor()).Validate())
	wantValid(t, "configured anchor with no parent", nodeKey(driver.AnchorKey{Slot: "slot.one"}).Validate())
}

func TestNodeKeyRejectsAnythingButAnAnchor(t *testing.T) {
	wantInvalid(t, "no anchor", nodeKey(driver.AnchorKey{}).Validate())
	wantInvalid(t, "scheme without digest", nodeKey(driver.AnchorKey{Scheme: "scheme.test.serial"}).Validate())
	wantInvalid(t, "digest without scheme", nodeKey(driver.AnchorKey{Key: digestOf("token")}).Validate())
	wantInvalid(t, "digest that is not a digest", nodeKey(driver.AnchorKey{Scheme: "scheme.test.serial", Key: "192.0.2.7"}).Validate())
	wantInvalid(t, "parent without slot", nodeKey(driver.AnchorKey{Parent: "resource:parent"}).Validate())
	wantInvalid(t, "both forms", nodeKey(driver.AnchorKey{Scheme: "scheme.test.serial", Key: digestOf("token"), Slot: "slot.one"}).Validate())
	wantInvalid(t, "invalid parent", nodeKey(driver.AnchorKey{Parent: "!", Slot: "slot.one"}).Validate())
}

// Each member of the key is varied; a key with the member emptied is invalid.
func TestNodeKeyRequiresEveryMember(t *testing.T) {
	for name, mutate := range map[string]func(*driver.NodeKey){
		"installation": func(k *driver.NodeKey) { k.Installation = "" },
		"driver":       func(k *driver.NodeKey) { k.Driver = "" },
		"instance":     func(k *driver.NodeKey) { k.Instance = "" },
		"kind":         func(k *driver.NodeKey) { k.Kind = "" },
	} {
		k := nodeKey(nativeAnchor())
		mutate(&k)
		wantInvalid(t, name, k.Validate())
	}
}

func TestNodeRefLinkKeyAndClaim(t *testing.T) {
	wantValid(t, "node ref", node("a").Validate())
	wantInvalid(t, "node ref without bridge", driver.NodeRef{ID: "resource:a"}.Validate())
	wantInvalid(t, "node ref with invalid id", driver.NodeRef{Bridge: "bridge.local", ID: "!"}.Validate())

	key := driver.LinkKey{Type: defRef("relation.feeds"), From: node("a"), To: node("b")}
	wantValid(t, "link key", key.Validate())
	for name, mutate := range map[string]func(*driver.LinkKey){
		"type": func(k *driver.LinkKey) { k.Type = v1.DefinitionRef{} },
		"from": func(k *driver.LinkKey) { k.From = driver.NodeRef{} },
		"to":   func(k *driver.LinkKey) { k.To = driver.NodeRef{} },
	} {
		k := key
		mutate(&k)
		wantInvalid(t, "link key without "+name, k.Validate())
	}

	wantValid(t, "link claim", driver.LinkClaim{Key: key, Basis: evidenceSet("basis")}.Validate())
	wantInvalid(t, "link claim without basis", driver.LinkClaim{Key: key}.Validate())
	wantInvalid(t, "link claim with invalid key", driver.LinkClaim{Basis: evidenceSet("basis")}.Validate())
	wantInvalid(t, "link claim with invalid basis", driver.LinkClaim{Key: key, Basis: []v1.EvidenceRef{{}}}.Validate())
}

// A directed link is its ordered tuple: the reversed tuple is another key.
func TestLinkKeyIsTheOrderedTuple(t *testing.T) {
	forward := driver.LinkKey{Type: defRef("relation.feeds"), From: node("a"), To: node("b")}
	reverse := driver.LinkKey{Type: forward.Type, From: forward.To, To: forward.From}
	if forward == reverse {
		t.Fatal("the reversed tuple is the same key")
	}
}

func TestIdentityTypesExpressNoSuccession(t *testing.T) {
	// A changed identifier is a new key: the keys are plain comparable values and
	// no member of any identity record names a predecessor.
	old := nodeKey(driver.AnchorKey{Scheme: "scheme.test.serial", Key: digestOf("serial-1")})
	changed := nodeKey(driver.AnchorKey{Scheme: "scheme.test.serial", Key: digestOf("serial-2")})
	if old == changed {
		t.Fatal("a changed identifier produced the same key")
	}
	wantValid(t, "old", old.Validate())
	wantValid(t, "changed", changed.Validate())
}
