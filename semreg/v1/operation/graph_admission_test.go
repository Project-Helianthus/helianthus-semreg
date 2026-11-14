package operation_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// admissionCatalog is a synthetic GraphCatalog covering the test pack. Every
// definition is a member unless a disposition overrides it.
type admissionCatalog struct {
	digest       semreg.Digest
	dispositions map[semreg.DefinitionRef]semreg.GraphDisposition
	lookups      int
}

func (c *admissionCatalog) Digest() semreg.Digest { return c.digest }

func (c *admissionCatalog) Covers(index semreg.DefinitionIndex) error {
	if !reflect.DeepEqual(index, (&testOperationPack{}).Definitions()) {
		return &semreg.Error{ID: semreg.MembershipMissing, Detail: "pack not covered"}
	}
	return nil
}

func (c *admissionCatalog) Membership(ref semreg.DefinitionRef) (semreg.GraphMembership, bool) {
	c.lookups++
	if ref.Pack != testPackRef {
		return semreg.GraphMembership{}, false
	}
	if disposition, ok := c.dispositions[ref]; ok {
		return semreg.GraphMembership{Disposition: disposition, Planes: []semreg.GraphPlane{}}, true
	}
	return semreg.GraphMembership{Disposition: semreg.GraphMember, Planes: []semreg.GraphPlane{"control"}}, true
}

func (c *admissionCatalog) ResourceKindMembership(semreg.DefinitionID) (semreg.GraphMembership, bool) {
	c.lookups++
	return semreg.GraphMembership{}, false
}

func (c *admissionCatalog) DimensionKind(semreg.DefinitionRef) (semreg.DefinitionID, bool) {
	c.lookups++
	return "", false
}

func (c *admissionCatalog) ServiceSubjectKind(semreg.DefinitionRef) (semreg.DefinitionID, bool) {
	c.lookups++
	return "", false
}

func (c *admissionCatalog) RelationDefinition(semreg.DefinitionRef) (semreg.GraphRelationDefinition, bool) {
	c.lookups++
	return semreg.GraphRelationDefinition{}, false
}

func (c *admissionCatalog) SchemeListed(semreg.DefinitionID) bool {
	c.lookups++
	return false
}

// fixtureCatalogDigest computes the SHA-256 of the committed catalog copy,
// the only digest a graph-enforced registry accepts.
func fixtureCatalogDigest(t *testing.T) semreg.Digest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "fixtures", "v1", "element-catalog-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func graphKernel(t *testing.T, pack *testOperationPack, dispositions map[semreg.DefinitionRef]semreg.GraphDisposition) (*operation.Kernel, *admissionCatalog) {
	t.Helper()
	catalog := &admissionCatalog{digest: fixtureCatalogDigest(t), dispositions: dispositions}
	kernel, err := operation.NewGraphKernel(operation.KernelOptions{MaxRetainedOperations: operation.DefaultMaxRetainedOperations}, catalog, pack)
	if err != nil {
		t.Fatal(err)
	}
	return kernel, catalog
}

// G-NEG-ACT-3: an intent for an operation (or its effect rule) whose catalog
// disposition is not member is rejected with membership_missing under
// enforcement, before any route is resolved, and is admitted exactly as
// today without enforcement.
func TestGraphAdmissionRejectsAnInadmissibleOperation(t *testing.T) {
	cases := map[string]map[semreg.DefinitionRef]semreg.GraphDisposition{
		"unknown operation":       {testOp: semreg.GraphUnknown},
		"unsupported operation":   {testOp: semreg.GraphUnsupported},
		"unknown effect rule":     {testEffect: semreg.GraphUnknown},
		"unsupported effect rule": {testEffect: semreg.GraphUnsupported},
	}
	for name, dispositions := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			plain := mustAdmit(t, fixture)

			enforced, catalog := graphKernel(t, fixture.pack, dispositions)
			lookups := catalog.lookups
			intentCalls := fixture.pack.intentCalls
			admission, err := enforced.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			errorID(t, err, semreg.MembershipMissing)
			if admission != nil {
				t.Fatal("inadmissible operation returned an admission")
			}
			if catalog.lookups != lookups {
				t.Fatal("admission read the catalog")
			}
			if fixture.pack.intentCalls != intentCalls {
				t.Fatal("inadmissible operation reached the pack hook")
			}
			// The refusal installs no idempotency entry: the same key stays
			// free and is refused again.
			_, err = enforced.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			errorID(t, err, semreg.MembershipMissing)

			// Without enforcement the same intent is admitted as before.
			again, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			if err != nil || again != plain {
				t.Fatalf("plain kernel replay: %v", err)
			}
		})
	}
}

// Under enforcement an operation whose operation and effect rule are members
// is admitted with the same route, revision and guard claims as without it.
func TestGraphAdmissionAdmitsMemberOperationsUnchanged(t *testing.T) {
	fixture := newOperationFixture(t)
	plain := mustAdmit(t, fixture)
	enforced, _ := graphKernel(t, fixture.pack, nil)
	admission, err := enforced.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
	if err != nil {
		t.Fatal(err)
	}
	plainRoute, _ := plain.Route()
	route, _ := admission.Route()
	plainClaims, _ := plain.GuardClaims()
	claims, _ := admission.GuardClaims()
	plainAt, plainRevision, _ := plain.AdmittedAt()
	at, revision, _ := admission.AdmittedAt()
	if !reflect.DeepEqual(route, plainRoute) || !reflect.DeepEqual(claims, plainClaims) || !reflect.DeepEqual(at, plainAt) || !reflect.DeepEqual(revision, plainRevision) || !reflect.DeepEqual(admission.Intent(), plain.Intent()) {
		t.Fatal("enforced admission differs from the admission without enforcement")
	}
}

// The graph kernel is constructed only with the pinned catalog that covers
// every registered pack.
func TestGraphAdmissionKernelConstruction(t *testing.T) {
	options := operation.KernelOptions{MaxRetainedOperations: operation.DefaultMaxRetainedOperations}
	pack := &testOperationPack{}
	var typedNil *admissionCatalog
	for name, catalog := range map[string]semreg.GraphCatalog{"nil": nil, "typed nil": typedNil} {
		t.Run(name, func(t *testing.T) {
			kernel, err := operation.NewGraphKernel(options, catalog, pack)
			errorID(t, err, semreg.MembershipMissing)
			if kernel != nil {
				t.Fatal("kernel returned with an error")
			}
		})
	}
	other := &admissionCatalog{digest: semreg.Digest("sha256:" + repeatHex(0))}
	_, err := operation.NewGraphKernel(options, other, pack)
	errorID(t, err, semreg.MembershipMissing)

	uncovered := &admissionCatalog{digest: fixtureCatalogDigest(t)}
	_, err = operation.NewGraphKernel(options, uncovered, pack, &extraPack{})
	errorID(t, err, semreg.MembershipMissing)

	_, err = operation.NewGraphKernel(operation.KernelOptions{}, &admissionCatalog{digest: fixtureCatalogDigest(t)}, pack)
	errorID(t, err, semreg.BoundsExceeded)
}

// extraPack registers a second pack the test catalog does not cover.
type extraPack struct{ testOperationPack }

func (p *extraPack) Pack() semreg.PackRef {
	return semreg.PackRef{ID: "pack.extra", Version: "1.0.0"}
}

func (p *extraPack) Definitions() semreg.DefinitionIndex {
	pack := p.Pack()
	return semreg.DefinitionIndex{
		Pack: pack, Fields: []semreg.DefinitionRef{}, Services: []semreg.DefinitionRef{{Pack: pack, ID: "service.extra", Version: "1.0.0"}},
		Capabilities: []semreg.DefinitionRef{}, Operations: []semreg.DefinitionRef{}, EffectRules: []semreg.DefinitionRef{},
	}
}
