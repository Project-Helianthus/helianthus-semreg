package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func embeddedCatalog(t *testing.T) *graph.Catalog {
	t.Helper()
	catalog, err := graph.Embedded()
	if err != nil {
		t.Fatalf("graph.Embedded: %v", err)
	}
	return catalog
}

func ledgerOn(t *testing.T, store Store, catalog *graph.Catalog) *Ledger {
	t.Helper()
	allocator, err := NewAllocator(store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := NewLedger(allocator)
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func sumOf(text string) semreg.Digest {
	sum := sha256.Sum256([]byte(text))
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func proof(label string) []semreg.EvidenceRef {
	return []semreg.EvidenceRef{{Owner: "owner.test", Kind: "evidence.test", Digest: sumOf("proof " + label), Contract: "test.contract/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone}}
}

func statement(revision, label string) driver.StatementRef {
	return driver.StatementRef{Revision: semreg.Uint64(revision), Evidence: proof("statement " + label)[0]}
}

func nativeKey(t *testing.T, installation string, d driver.DriverKind, instance driver.InstanceKey, kind semreg.DefinitionID, token string) driver.NodeKey {
	t.Helper()
	digest, err := IdentityKey("scheme.test_serial", "", token)
	if err != nil {
		t.Fatal(err)
	}
	return driver.NodeKey{Installation: installation, Driver: d, Instance: instance, Kind: kind, Anchor: driver.AnchorKey{Scheme: "scheme.test_serial", Key: digest}}
}

func slotKey(installation string, d driver.DriverKind, instance driver.InstanceKey, kind semreg.DefinitionID, parent semreg.ResourceID, slot string) driver.NodeKey {
	return driver.NodeKey{Installation: installation, Driver: d, Instance: instance, Kind: kind, Anchor: driver.AnchorKey{Parent: parent, Slot: slot}}
}

func mustNode(t *testing.T, l *Ledger, bridge driver.BridgeID, key driver.NodeKey) driver.NodeRef {
	t.Helper()
	ref, err := l.Node(bridge, key)
	if err != nil {
		t.Fatalf("Node(%+v): %v", key, err)
	}
	return ref
}

var testPack = semreg.PackRef{ID: "helianthus.pack.storage", Version: "1.1.0"}

func linkType(id string) semreg.DefinitionRef {
	return semreg.DefinitionRef{Pack: testPack, ID: semreg.DefinitionID(id), Version: "1.1.0"}
}
