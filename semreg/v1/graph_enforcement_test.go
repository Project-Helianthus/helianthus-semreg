package semreg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeGraphCatalog is a synthetic GraphCatalog. It covers exactly the packs
// whose indexes it holds and records how often each lookup is called.
type fakeGraphCatalog struct {
	digest   Digest
	covered  map[PackRef]DefinitionIndex
	members  map[DefinitionRef]GraphMembership
	coverErr error
	covers   []DefinitionIndex
	lookups  int
}

func (c *fakeGraphCatalog) Digest() Digest { return c.digest }

func (c *fakeGraphCatalog) Covers(index DefinitionIndex) error {
	c.covers = append(c.covers, index)
	if c.coverErr != nil {
		return c.coverErr
	}
	want, ok := c.covered[index.Pack]
	if !ok || !reflect.DeepEqual(want, index) {
		return &Error{ID: MembershipMissing, Detail: "fake catalog does not cover the pack"}
	}
	return nil
}

func (c *fakeGraphCatalog) Membership(ref DefinitionRef) (GraphMembership, bool) {
	c.lookups++
	membership, ok := c.members[ref]
	membership.Planes = append([]GraphPlane(nil), membership.Planes...)
	return membership, ok
}

func (c *fakeGraphCatalog) ResourceKindMembership(DefinitionID) (GraphMembership, bool) {
	c.lookups++
	return GraphMembership{}, false
}

func (c *fakeGraphCatalog) DimensionKind(DefinitionRef) (DefinitionID, bool) {
	c.lookups++
	return "", false
}

func (c *fakeGraphCatalog) ServiceSubjectKind(DefinitionRef) (DefinitionID, bool) {
	c.lookups++
	return "", false
}

func (c *fakeGraphCatalog) RelationDefinition(DefinitionRef) (GraphRelationDefinition, bool) {
	c.lookups++
	return GraphRelationDefinition{}, false
}

func (c *fakeGraphCatalog) SchemeListed(DefinitionID) bool {
	c.lookups++
	return false
}

var (
	enforcementPack      = PackRef{ID: "pack.test", Version: "1.0.0"}
	enforcementOperation = DefinitionRef{Pack: enforcementPack, ID: "operation.test.set", Version: "1.0.0"}
	enforcementEffect    = DefinitionRef{Pack: enforcementPack, ID: "effect.test.set", Version: "1.0.0"}
	enforcementOtherOp   = DefinitionRef{Pack: enforcementPack, ID: "operation.test.unset", Version: "1.0.0"}
	enforcementOtherRule = DefinitionRef{Pack: enforcementPack, ID: "effect.test.unset", Version: "1.0.0"}
)

func enforcementIndex() DefinitionIndex {
	return DefinitionIndex{
		Pack: enforcementPack, Fields: []DefinitionRef{},
		Services:     []DefinitionRef{{Pack: enforcementPack, ID: "service.test", Version: "1.0.0"}},
		Capabilities: []DefinitionRef{{Pack: enforcementPack, ID: "capability.test", Version: "1.0.0"}},
		Operations:   []DefinitionRef{enforcementOperation, enforcementOtherOp},
		EffectRules:  []DefinitionRef{enforcementEffect, enforcementOtherRule},
	}
}

func enforcementValidator() *countingValidator {
	return &countingValidator{pack: enforcementPack, index: enforcementIndex()}
}

// pinnedFixtureDigest computes the SHA-256 of the committed catalog copy.
func pinnedFixtureDigest(t *testing.T) Digest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "v1", "element-catalog-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// coveringCatalog covers the enforcement pack; every definition is a member
// unless a disposition overrides it.
func coveringCatalog(t *testing.T, dispositions map[DefinitionRef]GraphDisposition) *fakeGraphCatalog {
	t.Helper()
	index := enforcementIndex()
	members := map[DefinitionRef]GraphMembership{}
	for _, group := range [][]DefinitionRef{index.Fields, index.Services, index.Capabilities, index.Operations, index.EffectRules} {
		for _, ref := range group {
			members[ref] = GraphMembership{Disposition: GraphMember, Planes: []GraphPlane{"control"}}
		}
	}
	for ref, disposition := range dispositions {
		members[ref] = GraphMembership{Disposition: disposition, Planes: []GraphPlane{}}
	}
	return &fakeGraphCatalog{digest: pinnedFixtureDigest(t), covered: map[PackRef]DefinitionIndex{enforcementPack: index}, members: members}
}

func TestGraphEnforcementPinIsTheFixtureDigest(t *testing.T) {
	if got := pinnedFixtureDigest(t); got != pinnedCatalogDigest {
		t.Fatalf("pinned catalog digest %s, fixture digest %s", pinnedCatalogDigest, got)
	}
}

// ACT-1: construction without a usable catalog never falls back to a
// registry that is not graph-enforced.
func TestGraphEnforcementRequiresACatalog(t *testing.T) {
	var typedNil *fakeGraphCatalog
	for name, catalog := range map[string]GraphCatalog{"nil": nil, "typed nil": typedNil} {
		t.Run(name, func(t *testing.T) {
			registry, err := NewGraphRegistry(catalog, enforcementValidator())
			requireID(t, err, MembershipMissing)
			if registry != nil {
				t.Fatal("registry returned with an error")
			}
			kernel, err := NewGraphPublicationKernel("asset:site", catalog, enforcementValidator())
			requireID(t, err, MembershipMissing)
			if kernel != nil {
				t.Fatal("kernel returned with an error")
			}
		})
	}
}

// ACT-1: a catalog whose digest differs from the pin is refused.
func TestGraphEnforcementRejectsAnotherCatalog(t *testing.T) {
	pinned := pinnedFixtureDigest(t)
	other := []byte(pinned)
	other[len(other)-1] ^= 1
	for name, digest := range map[string]Digest{"other digest": Digest(other), "empty": "", "malformed": "sha256:zz"} {
		t.Run(name, func(t *testing.T) {
			catalog := coveringCatalog(t, nil)
			catalog.digest = digest
			_, err := NewGraphRegistry(catalog, enforcementValidator())
			requireID(t, err, MembershipMissing)
			_, err = NewGraphPublicationKernel("asset:site", catalog, enforcementValidator())
			requireID(t, err, MembershipMissing)
			if len(catalog.covers) != 0 {
				t.Fatal("coverage was checked against a catalog that is not the pinned one")
			}
		})
	}
}

// G-NEG-ACT-2: construction with a registered pack the catalog does not cover
// is membership_missing; every registered pack is checked with its own index.
func TestGraphEnforcementRejectsAnUncoveredPack(t *testing.T) {
	other := PackRef{ID: "pack.other", Version: "1.0.0"}
	otherValidator := &countingValidator{pack: other, index: indexWith(other, DefinitionService, DefinitionRef{Pack: other, ID: "service.other", Version: "1.0.0"})}
	catalog := coveringCatalog(t, nil)
	_, err := NewGraphRegistry(catalog, enforcementValidator(), otherValidator)
	requireID(t, err, MembershipMissing)
	_, err = NewGraphPublicationKernel("asset:site", coveringCatalog(t, nil), enforcementValidator(), otherValidator)
	requireID(t, err, MembershipMissing)

	// A synthetic definition under a covered pack reference is not covered.
	synthetic := enforcementValidator()
	synthetic.index.Capabilities = append(synthetic.index.Capabilities, DefinitionRef{Pack: enforcementPack, ID: "capability.test.synthetic", Version: "1.0.0"})
	_, err = NewGraphRegistry(coveringCatalog(t, nil), synthetic)
	requireID(t, err, MembershipMissing)

	// Whatever the catalog reports for an uncovered pack, construction fails
	// with membership_missing.
	for name, coverErr := range map[string]error{"plain error": errors.New("not covered"), "other stable error": &Error{ID: DefinitionOwnerMissing, Detail: "extra entry"}} {
		t.Run(name, func(t *testing.T) {
			failing := coveringCatalog(t, nil)
			failing.coverErr = coverErr
			_, err := NewGraphRegistry(failing, enforcementValidator())
			requireID(t, err, MembershipMissing)
		})
	}

	covered := coveringCatalog(t, nil)
	registry, err := NewGraphRegistry(covered, enforcementValidator())
	if err != nil {
		t.Fatal(err)
	}
	if !registry.GraphEnforced() {
		t.Fatal("registry with the catalog is not graph-enforced")
	}
	if digest, ok := registry.CatalogDigest(); !ok || digest != pinnedCatalogDigest {
		t.Fatalf("catalog digest %q %v", digest, ok)
	}
	if len(covered.covers) != 1 || !reflect.DeepEqual(covered.covers[0], enforcementIndex()) {
		t.Fatalf("coverage calls %#v", covered.covers)
	}
}

// Validator registration errors are reported as before; the catalog does not
// hide them.
func TestGraphEnforcementKeepsRegistrationErrors(t *testing.T) {
	_, plainErr := NewRegistry(enforcementValidator(), enforcementValidator())
	requireID(t, plainErr, DefinitionOwnerConflict)
	_, err := NewGraphRegistry(coveringCatalog(t, nil), enforcementValidator(), enforcementValidator())
	requireID(t, err, DefinitionOwnerConflict)
	_, err = NewGraphRegistry(coveringCatalog(t, nil), nil)
	requireID(t, err, DefinitionOwnerConflict)
}

// ACT-2 and G-NEG-ACT-1: a registry or kernel without the catalog is not
// graph-enforced, exposes no digest and refuses every graph member.
func TestGraphEnforcementWithoutCatalogIsUnchanged(t *testing.T) {
	registry, err := NewRegistry(enforcementValidator())
	if err != nil {
		t.Fatal(err)
	}
	if registry.GraphEnforced() {
		t.Fatal("registry without the catalog is graph-enforced")
	}
	if digest, ok := registry.CatalogDigest(); ok || digest != "" {
		t.Fatalf("registry without the catalog exposes digest %q", digest)
	}
	var none *Registry
	if none.GraphEnforced() {
		t.Fatal("nil registry is graph-enforced")
	}
	if _, ok := none.CatalogDigest(); ok {
		t.Fatal("nil registry exposes a digest")
	}
	requireID(t, none.GraphAdmissionError(enforcementOperation, enforcementEffect), DefinitionOwnerMissing)
	for _, refs := range [][2]DefinitionRef{{enforcementOperation, enforcementEffect}, {enforcementOtherOp, enforcementOtherRule}} {
		if err := registry.GraphAdmissionError(refs[0], refs[1]); err != nil {
			t.Fatalf("registry without the catalog refuses %v: %v", refs, err)
		}
	}

	kernel := newTestPublicationKernel(t, "asset:site")
	if kernel.registry.GraphEnforced() {
		t.Fatal("NewPublicationKernel built a graph-enforced registry")
	}
	batch := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
	batch.ResourceUpserts = []Resource{graphSlotResource("resource:inverter")}
	sealPublicationBatch(t, &batch)
	assertRejectedUnchanged(t, kernel, batch, MembershipMissing)
}

// An enforced kernel publishes a batch without graph members exactly as a
// kernel without the catalog does, and refuses graph members until the graph
// publication rules exist.
func TestGraphEnforcementPublishesPlainBatchesUnchanged(t *testing.T) {
	plain := newTestPublicationKernel(t, "asset:site")
	catalog := coveringCatalog(t, nil)
	catalog.covered = map[PackRef]DefinitionIndex{enforcementPack: plain.registry.indexes[enforcementPack]}
	validator := &countingValidator{pack: enforcementPack, index: plain.registry.indexes[enforcementPack]}
	enforced, err := NewGraphPublicationKernel("asset:site", catalog, validator)
	if err != nil {
		t.Fatal(err)
	}
	if !enforced.registry.GraphEnforced() {
		t.Fatal("NewGraphPublicationKernel built a registry that is not graph-enforced")
	}
	batch := graphBatch(t, false)
	want, wantBytes, err := plain.Apply(batch, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	got, gotBytes, err := enforced.Apply(batch, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || !bytes.Equal(gotBytes, wantBytes) {
		t.Fatal("enforced kernel published a different snapshot")
	}
	fork, err := enforced.Fork()
	if err != nil {
		t.Fatal(err)
	}
	if !fork.registry.GraphEnforced() {
		t.Fatal("fork of an enforced kernel is not graph-enforced")
	}

	next := publicationBatch("asset:site", "source:a", "epoch:a", "1", "2", "1")
	next.ResourceUpserts = []Resource{graphSlotResource("resource:inverter")}
	sealPublicationBatch(t, &next)
	assertRejectedUnchanged(t, enforced, next, MembershipMissing)
	if catalog.lookups != 0 {
		t.Fatalf("publication read the catalog %d times", catalog.lookups)
	}
}

// ACT-4: every operation and effect rule whose disposition is not member is
// recorded at construction; admission reads only that index.
func TestGraphEnforcementRecordsInadmissibleDefinitions(t *testing.T) {
	for _, disposition := range []GraphDisposition{GraphUnknown, GraphUnsupported, "not-a-disposition"} {
		t.Run(string(disposition), func(t *testing.T) {
			catalog := coveringCatalog(t, map[DefinitionRef]GraphDisposition{enforcementOperation: disposition, enforcementOtherRule: disposition})
			registry, err := NewGraphRegistry(catalog, enforcementValidator())
			if err != nil {
				t.Fatal(err)
			}
			requireID(t, registry.GraphAdmissionError(enforcementOperation, enforcementEffect), MembershipMissing)
			requireID(t, registry.GraphAdmissionError(enforcementOtherOp, enforcementOtherRule), MembershipMissing)
			if err := registry.GraphAdmissionError(enforcementOtherOp, enforcementEffect); err != nil {
				t.Fatalf("member operation and effect rule refused: %v", err)
			}

			// Admission reads only the index fixed at construction.
			lookups := catalog.lookups
			for ref := range catalog.members {
				catalog.members[ref] = GraphMembership{Disposition: GraphMember}
			}
			delete(catalog.covered, enforcementPack)
			requireID(t, registry.GraphAdmissionError(enforcementOperation, enforcementEffect), MembershipMissing)
			if err := registry.GraphAdmissionError(enforcementOtherOp, enforcementEffect); err != nil {
				t.Fatal(err)
			}
			if catalog.lookups != lookups {
				t.Fatal("admission lookup read the catalog")
			}
		})
	}

	missing := coveringCatalog(t, nil)
	delete(missing.members, enforcementEffect)
	_, err := NewGraphRegistry(missing, enforcementValidator())
	requireID(t, err, MembershipMissing)
}

// The inadmissible key is (kind, pack id, pack version, definition id,
// definition version). Each variant changes one field of the recorded
// operation; a production key that dropped that field would refuse it. The
// definition id and version variants are registered member operations of the
// same pack; the pack variants cannot be registered beside the operation (the
// registry owns one pack per definition), so they are looked up unregistered.
func TestGraphEnforcementInadmissibleKeyVariesEveryField(t *testing.T) {
	pack := PackRef{ID: "pack.alpha", Version: "1.0.0"}
	operation := DefinitionRef{Pack: pack, ID: "alpha.operation.set", Version: "1.0.0"}
	effect := DefinitionRef{Pack: pack, ID: "alpha.effect.set", Version: "1.0.0"}
	idVariant := DefinitionRef{Pack: pack, ID: "alpha.operation.unset", Version: operation.Version}
	versionVariant := DefinitionRef{Pack: pack, ID: operation.ID, Version: "1.1.0"}
	index := DefinitionIndex{
		Pack: pack, Fields: []DefinitionRef{}, Services: []DefinitionRef{}, Capabilities: []DefinitionRef{},
		Operations:  []DefinitionRef{operation, versionVariant, idVariant},
		EffectRules: []DefinitionRef{effect},
	}
	members := map[DefinitionRef]GraphMembership{
		operation: {Disposition: GraphUnknown}, versionVariant: {Disposition: GraphMember},
		idVariant: {Disposition: GraphMember}, effect: {Disposition: GraphMember},
	}
	catalog := &fakeGraphCatalog{digest: pinnedFixtureDigest(t), covered: map[PackRef]DefinitionIndex{pack: index}, members: members}
	registry, err := NewGraphRegistry(catalog, &countingValidator{pack: pack, index: index})
	if err != nil {
		t.Fatal(err)
	}
	requireID(t, registry.GraphAdmissionError(operation, effect), MembershipMissing)
	for name, variant := range map[string]DefinitionRef{
		"pack id":            {Pack: PackRef{ID: "pack.beta", Version: pack.Version}, ID: operation.ID, Version: operation.Version},
		"pack version":       {Pack: PackRef{ID: pack.ID, Version: "2.0.0"}, ID: operation.ID, Version: operation.Version},
		"definition id":      idVariant,
		"definition version": versionVariant,
	} {
		if err := registry.GraphAdmissionError(variant, effect); err != nil {
			t.Fatalf("%s variant refused: %v", name, err)
		}
	}
	// kind: the inadmissible operation in the effect-rule position is not an
	// inadmissible effect rule.
	if err := registry.GraphAdmissionError(idVariant, operation); err != nil {
		t.Fatalf("kind variant refused: %v", err)
	}
}
