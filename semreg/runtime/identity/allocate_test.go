package identity

import (
	"errors"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// The vectors of the driver contract (identity.json, I-ALLOC-*), written out
// literally: an allocation that changes the domain, the field order, the length
// prefix or the separators fails here.
func TestPathNodeIDsMatchTheContractVectors(t *testing.T) {
	identity, err := IdentityKey("scheme.ieee.eui48", "", "0A1B2C3D4E5F")
	if err != nil {
		t.Fatal(err)
	}
	other, err := IdentityKey("scheme.ieee.eui48", "", "0A1B2C3D4E60")
	if err != nil {
		t.Fatal(err)
	}
	bridge := func(installation string, d driver.DriverKind, instance driver.InstanceKey, kind semreg.DefinitionID, anchor driver.AnchorKey) driver.NodeKey {
		return driver.NodeKey{Installation: installation, Driver: d, Instance: instance, Kind: kind, Anchor: anchor}
	}
	const parent1, parent2 = "resource:00000000000000000000000000000001", "resource:00000000000000000000000000000002"
	for name, tc := range map[string]struct {
		key  driver.NodeKey
		want semreg.ResourceID
	}{
		"I-ALLOC-PATH-SLOT": {bridge("inst-a", "driver-x", "i1", "resource.bridge", driver.AnchorKey{Slot: "primary"}), "resource:70dc2ac7d92d82c2e1f254a5c532541d"},
		"I-ALLOC-PATH-KEY":  {bridge("inst-a", "driver-x", "i1", "resource.bridge", driver.AnchorKey{Scheme: "scheme.ieee.eui48", Key: identity}), "resource:ee5fba45d9c4a6dfd8dad3236f362e93"},
		"injective first":   {bridge("a", "bc", "i1", "resource.bridge", driver.AnchorKey{Slot: "primary"}), "resource:cba8c8d09199dde66a1cb714962a26ed"},
		"injective second":  {bridge("ab", "c", "i1", "resource.bridge", driver.AnchorKey{Slot: "primary"}), "resource:247e6998a2c167a928e81ca4c1b6facb"},
		"vary base":         {bridge("inst-a", "driver-x", "i1", "resource.bridge", driver.AnchorKey{Parent: parent1, Slot: "primary"}), "resource:c26c0b683e132c9f0c564d4ad4513cf2"},
		"vary installation": {bridge("inst-b", "driver-x", "i1", "resource.bridge", driver.AnchorKey{Parent: parent1, Slot: "primary"}), "resource:97c451ded54fdd490ebab2c1b4749cf2"},
		"vary driver":       {bridge("inst-a", "driver-y", "i1", "resource.bridge", driver.AnchorKey{Parent: parent1, Slot: "primary"}), "resource:1b53bf34dc9352a6afccea5c05ce34b0"},
		"vary instance":     {bridge("inst-a", "driver-x", "i2", "resource.bridge", driver.AnchorKey{Parent: parent1, Slot: "primary"}), "resource:78bf3f7cd8944dce824e9655373a7970"},
		"vary node kind":    {bridge("inst-a", "driver-x", "i1", "resource.link", driver.AnchorKey{Parent: parent1, Slot: "primary"}), "resource:cd978b53e338794ad8dee422d892d828"},
		"vary parent":       {bridge("inst-a", "driver-x", "i1", "resource.bridge", driver.AnchorKey{Parent: parent2, Slot: "primary"}), "resource:eca0387b619acfda72959460a3c38e41"},
		"vary slot":         {bridge("inst-a", "driver-x", "i1", "resource.bridge", driver.AnchorKey{Parent: parent1, Slot: "secondary"}), "resource:169a3c1f6fbf49027d2ab9db12a2dfa0"},
		"vary anchor tag":   {bridge("inst-a", "driver-x", "i1", "resource.bridge", driver.AnchorKey{Scheme: "scheme.ieee.eui48", Key: identity}), "resource:ee5fba45d9c4a6dfd8dad3236f362e93"},
		"vary digest":       {bridge("inst-a", "driver-x", "i1", "resource.bridge", driver.AnchorKey{Scheme: "scheme.ieee.eui48", Key: other}), "resource:0ad6658796b5672a89c7b900b716b268"},
		"I-ALLOC-DOMAIN":    {bridge("x", "y", "z", "resource.bridge", driver.AnchorKey{Slot: "primary"}), ""},
	} {
		if tc.want == "" {
			continue
		}
		if got := pathNodeID(tc.key); got != tc.want {
			t.Errorf("%s: id = %s, want %s", name, got, tc.want)
		}
	}
}

// Legacy ids: the contract's two legacy vectors and the same members hashed under
// the other domain, which never collide (I-ALLOC-LEGACY-*, I-ALLOC-DOMAIN).
func TestAllocateKeepsTheLegacyIDsOfCatalogResourceKinds(t *testing.T) {
	catalog := embeddedCatalog(t)
	allocator, err := NewAllocator(NewMemoryStore(), catalog)
	if err != nil {
		t.Fatal(err)
	}
	const zoneParent = "resource:00000000000000000000000000000001"
	zone := slotKey("inst-a", "driver-x", "i1", "resource.zone", zoneParent, "zone-1")
	// The contract's literal parent is no id this allocator returned: the legacy
	// minter gives the vector, and Allocate commits nothing under that parent.
	if got := resourceID(AnchorKey{Kind: "resource.zone", Parent: zoneParent, Slot: "zone-1"}); got != "resource:a0b7d12925ca145e10e3670e018086ce" {
		t.Fatalf("legacy slot id = %q", got)
	}
	if got, err := allocator.Allocate(zone); got != "" || !errors.Is(err, ErrUnknownResource) {
		t.Fatalf("a parent never returned: id = %q, %v", got, err)
	}
	digest, _ := IdentityKey("scheme.ieee.eui48", "", "0A1B2C3D4E5F")
	equipment := driver.NodeKey{Installation: "inst-a", Driver: "driver-x", Instance: "i1", Kind: "resource.equipment", Anchor: driver.AnchorKey{Scheme: "scheme.ieee.eui48", Key: digest}}
	if got, err := allocator.Allocate(equipment); err != nil || got != "resource:eb2e2bec29873eff6599e98733aaa17e" {
		t.Fatalf("legacy key id = %q, %v", got, err)
	}
	// A kind the catalog does not list as a resource kind is a path node, and the
	// same members under the legacy domain differ from the path-node id.
	bridge := slotKey("inst-a", "driver-x", "i1", "resource.bridge", "", "primary")
	pathID, err := allocator.Allocate(bridge)
	if err != nil {
		t.Fatal(err)
	}
	if pathID == resourceID(AnchorKey{Kind: "resource.bridge", Slot: "primary"}) {
		t.Fatal("the legacy and the path-node domain gave one id")
	}
	// The installation root is not allocated by the legacy minter.
	site := slotKey("inst-a", SiteDriver, SiteInstance, "resource.site", "", SiteSlot)
	siteID, _ := allocator.Allocate(site)
	if siteID == resourceID(AnchorKey{Kind: "resource.site", Slot: SiteSlot}) || siteID != pathNodeID(site) {
		t.Fatalf("site id = %q", siteID)
	}
}

// A legacy key resolves to the id the donor resolver gives the same kind and
// anchor, and the same key gives the same id from a fresh allocator over an empty
// store and from one that reopens the recorded store.
func TestAllocateAgreesWithResolveAndSurvivesReopen(t *testing.T) {
	catalog := embeddedCatalog(t)
	store := NewMemoryStore()
	first, _ := NewAllocator(store, catalog)
	key := slotKey("inst-a", "driver-x", "i1", "resource.zone", "", "zone-1")
	id, err := first.Allocate(key)
	if err != nil {
		t.Fatal(err)
	}
	donor, _ := NewAllocator(NewMemoryStore(), catalog)
	donorID := mustResolve(t, donor, "resource.zone", zoneAnchor(semreg.AnchorOperatorSlot, "zone-1", "donor"))
	if donorID != id {
		t.Fatalf("Allocate = %q, Resolve = %q", id, donorID)
	}
	reopened, _ := NewAllocator(store, catalog)
	if again, err := reopened.Allocate(key); err != nil || again != id {
		t.Fatalf("reopened id = %q, %v; want %q", again, err, id)
	}
	fresh, _ := NewAllocator(NewMemoryStore(), catalog)
	if again, err := fresh.Allocate(key); err != nil || again != id {
		t.Fatalf("fresh id = %q, %v; want %q", again, err, id)
	}
	// A superseded key is never allocated again, and the upgraded key keeps the id.
	labelled := mustResolve(t, first, kindEquipment, operatorEquipment("unit-a", "x"))
	if _, _, err := first.Supersede(Claim{kindEquipment, operatorEquipment("unit-a", "x")}, Claim{kindEquipment, tokenAnchor("token-a", "x")}, continuity(ContinuityBinding, "x")); err != nil {
		t.Fatal(err)
	}
	digest, _ := IdentityKey(testScheme, testIssuer, "token-a")
	upgraded := driver.NodeKey{Installation: "inst-a", Driver: "driver-x", Instance: "i1", Kind: kindEquipment, Anchor: driver.AnchorKey{Scheme: string(testScheme), Issuer: testIssuer, Key: digest}}
	if got, err := first.Allocate(upgraded); err != nil || got != labelled {
		t.Fatalf("upgraded key = %q, %v; want %q", got, err, labelled)
	}
	if _, err := first.Allocate(slotKey("inst-a", "driver-x", "i1", kindEquipment, "", "unit-a")); !errors.Is(err, ErrAnchorSuperseded) {
		t.Fatalf("superseded key: %v", err)
	}
}

// A key whose members or anchor are incomplete gets no id, with the reason; an
// allocator without a catalog allocates nothing.
func TestAllocateRefusesAnIncompleteKeyWithTheReason(t *testing.T) {
	allocator, _ := NewAllocator(NewMemoryStore(), embeddedCatalog(t))
	complete := slotKey("inst-a", "driver-x", "i1", "resource.bridge", "", "primary")
	for name, mutate := range map[string]func(*driver.NodeKey){
		"installation": func(k *driver.NodeKey) { k.Installation = "" },
		"driver":       func(k *driver.NodeKey) { k.Driver = "" },
		"instance":     func(k *driver.NodeKey) { k.Instance = "" },
		"kind":         func(k *driver.NodeKey) { k.Kind = "" },
		"anchor":       func(k *driver.NodeKey) { k.Anchor = driver.AnchorKey{} },
		"slot":         func(k *driver.NodeKey) { k.Anchor = driver.AnchorKey{Parent: "resource:p"} },
		"native key":   func(k *driver.NodeKey) { k.Anchor = driver.AnchorKey{Scheme: "scheme.x_y"} },
	} {
		key := complete
		mutate(&key)
		id, err := allocator.Allocate(key)
		if id != "" || !errors.Is(err, ErrIncompleteKey) || err.Error() == ErrIncompleteKey.Error() {
			t.Errorf("%s: id=%q err=%v, want no id and a reason", name, id, err)
		}
	}
	bare, _ := NewAllocator(NewMemoryStore(), nil)
	if _, err := bare.Allocate(complete); !errors.Is(err, ErrNoCatalog) {
		t.Fatalf("without a catalog: err=%v", err)
	}
}

// Allocation reads the key alone: the same key gives the same id whatever was
// allocated before it, so no arrival index or counter enters the identity.
func TestAllocateDoesNotDependOnOrder(t *testing.T) {
	catalog := embeddedCatalog(t)
	keys := []driver.NodeKey{
		slotKey("inst-a", "driver-x", "i1", "resource.bridge", "", "b1"),
		slotKey("inst-a", "driver-x", "i2", "resource.bridge", "", "b1"),
		slotKey("inst-a", "driver-x", "i1", "resource.zone", "", "z1"),
		nativeKey(t, "inst-a", "driver-x", "i1", "resource.equipment", "token-a"),
	}
	forward, _ := NewAllocator(NewMemoryStore(), catalog)
	reverse, _ := NewAllocator(NewMemoryStore(), catalog)
	got := make([]semreg.ResourceID, len(keys))
	for i := range keys {
		got[i], _ = forward.Allocate(keys[i])
	}
	for i := len(keys) - 1; i >= 0; i-- {
		if id, _ := reverse.Allocate(keys[i]); id != got[i] {
			t.Errorf("key %d: %q after %q", i, id, got[i])
		}
	}
}

// A path-node id this allocator returned is a known parent: a resource anchored
// under it is allocated, resolved and superseded, and allocating the path node
// again makes it known. A legacy anchor whose parent was never returned is not
// committed.
func TestAPathNodeIDIsAKnownParent(t *testing.T) {
	catalog := embeddedCatalog(t)
	store := NewMemoryStore()
	allocator, _ := NewAllocator(store, catalog)
	bridge := slotKey("inst-a", "driver-x", "i1", "resource.bridge", "", "primary")
	child := func(parent semreg.ResourceID) driver.NodeKey {
		return slotKey("inst-a", "driver-x", "i1", "resource.zone", parent, "zone-1")
	}
	unknown := pathNodeID(bridge)
	if id, err := allocator.Allocate(child(unknown)); id != "" || !errors.Is(err, ErrUnknownResource) {
		t.Fatalf("a parent never returned: id = %q, %v", id, err)
	}
	if _, ok, _ := store.Lookup(AnchorKey{Kind: "resource.zone", Parent: unknown, Slot: "zone-1"}); ok {
		t.Fatal("an anchor under an unknown parent was committed")
	}
	parent, err := allocator.Allocate(bridge)
	if err != nil || parent != unknown {
		t.Fatalf("path node = %q, %v", parent, err)
	}
	zone, err := allocator.Allocate(child(parent))
	if err != nil {
		t.Fatalf("a zone under the returned path node: %v", err)
	}
	under := zoneAnchor(semreg.AnchorOperatorSlot, "zone-1", "parent")
	under.Parent = parent
	if got, _, err := allocator.Resolve("resource.zone", under); err != nil || got != zone {
		t.Fatalf("Resolve under the path node = %q, %v; want %q", got, err, zone)
	}
	native := zoneAnchor(semreg.AnchorNativeSlot, "zone-1", "native")
	native.Parent = parent
	if got, _, err := allocator.Supersede(Claim{"resource.zone", under}, Claim{"resource.zone", native}, continuity(ContinuityBinding, "zone")); err != nil || got != zone {
		t.Fatalf("Supersede under the path node = %q, %v; want %q", got, err, zone)
	}
	// The same holds from a fresh allocator once the path node is allocated again.
	again, _ := NewAllocator(store, catalog)
	if _, err := again.Allocate(child(parent)); !errors.Is(err, ErrUnknownResource) {
		t.Fatalf("fresh allocator before the path node: %v", err)
	}
	if _, err := again.Allocate(bridge); err != nil {
		t.Fatal(err)
	}
	if got, err := again.Allocate(child(parent)); err != nil || got != zone {
		t.Fatalf("fresh allocator after the path node = %q, %v", got, err)
	}
}

// A key whose class needs a parent or a slot the key does not carry gets no id
// and no anchor in the store; an equipment native digest still allocates.
func TestAllocateRefusesAKeyThatLacksWhatItsClassNeeds(t *testing.T) {
	catalog := embeddedCatalog(t)
	store := NewMemoryStore()
	allocator, _ := NewAllocator(store, catalog)
	parent := pathNodeID(slotKey("inst-a", "driver-x", "i1", "resource.bridge", "", "primary"))
	allocator.know(parent, "resource.bridge")
	for name, key := range map[string]driver.NodeKey{
		"component from a native digest": nativeKey(t, "inst-a", "driver-x", "i1", "resource.battery_cell", "cell-1"),
		"component without a parent":     slotKey("inst-a", "driver-x", "i1", "resource.battery_cell", "", "cell-1"),
	} {
		id, err := allocator.Allocate(key)
		if id != "" || !errors.Is(err, ErrIncompleteKey) || err.Error() == ErrIncompleteKey.Error() {
			t.Errorf("%s: id=%q err=%v, want no id and a reason", name, id, err)
		}
	}
	if table, err := store.Table(); err != nil || len(table.Anchors) != 0 {
		t.Fatalf("the store holds %v (%v)", table, err)
	}
	if id, err := allocator.Allocate(slotKey("inst-a", "driver-x", "i1", "resource.battery_cell", parent, "cell-1")); err != nil || id == "" {
		t.Fatalf("a component with a parent and a slot: id=%q err=%v", id, err)
	}
	if id, err := allocator.Allocate(nativeKey(t, "inst-a", "driver-x", "i1", "resource.equipment", "token-a")); err != nil || id == "" {
		t.Fatalf("an equipment native digest: id=%q err=%v", id, err)
	}
}
