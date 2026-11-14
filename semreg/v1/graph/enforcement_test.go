package graph_test

import (
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

func acceptedValidators() []semreg.PackValidator {
	return []semreg.PackValidator{thermal.New(), pv.New(), storage.New(), evse.New(), infrastructure.New()}
}

// stubValidator registers a fixed definition index. Its hooks accept
// everything; only registration is exercised.
type stubValidator struct{ index semreg.DefinitionIndex }

func (v stubValidator) Pack() semreg.PackRef                             { return v.index.Pack }
func (v stubValidator) Definitions() semreg.DefinitionIndex              { return v.index }
func (stubValidator) ValidateFact(semreg.FactKey, *semreg.Value) error   { return nil }
func (stubValidator) ValidateService(semreg.ServiceInstance) error       { return nil }
func (stubValidator) ValidateCapability(semreg.CapabilityInstance) error { return nil }
func (stubValidator) ValidateField(semreg.DefinitionRef, semreg.TypedField) error {
	return nil
}
func (stubValidator) MatchConstraints(semreg.CapabilityInstance, []semreg.TypedField) error {
	return nil
}
func (stubValidator) EvaluatePredicate(semreg.FactCandidate, semreg.PredicateOp, semreg.Value) (bool, error) {
	return false, nil
}

func refByID(t *testing.T, refs []semreg.DefinitionRef, id semreg.DefinitionID) semreg.DefinitionRef {
	t.Helper()
	for _, ref := range refs {
		if ref.ID == id {
			return ref
		}
	}
	t.Fatalf("no definition %s", id)
	return semreg.DefinitionRef{}
}

func TestGraphEnforcementEmbeddedCatalogIsThePinnedCopy(t *testing.T) {
	catalog, err := graph.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Digest() != graph.PinnedDigest || catalog.Digest() != committedCatalog(t).Digest() {
		t.Fatalf("embedded catalog digest %s", catalog.Digest())
	}
	if string(catalog.Bytes()) != string(fixtureBytes(t)) {
		t.Fatal("embedded catalog bytes differ from the fixture")
	}
	again, err := graph.Embedded()
	if err != nil || again.Digest() != catalog.Digest() {
		t.Fatalf("second Embedded = %v", err)
	}
}

// ACT-1: Enforced builds a graph-enforced registry over the accepted packs and
// exposes the pinned digest.
func TestGraphEnforcementEnforcedAcceptsTheAcceptedPacks(t *testing.T) {
	registry, err := graph.Enforced(acceptedValidators()...)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.GraphEnforced() {
		t.Fatal("Enforced registry is not graph-enforced")
	}
	if digest, ok := registry.CatalogDigest(); !ok || digest != graph.PinnedDigest {
		t.Fatalf("catalog digest %q %v", digest, ok)
	}
	for _, validator := range acceptedValidators() {
		single, err := graph.Enforced(validator)
		if err != nil {
			t.Fatalf("Enforced(%s) = %v", validator.Pack().ID, err)
		}
		if !single.GraphEnforced() {
			t.Fatal("single-pack registry is not graph-enforced")
		}
	}
	catalog, err := graph.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := semreg.NewGraphPublicationKernel("asset:site", catalog, acceptedValidators()...)
	if err != nil || kernel == nil {
		t.Fatalf("NewGraphPublicationKernel = %v", err)
	}
}

// G-NEG-ACT-2: a registered pack the catalog does not cover is refused with
// membership_missing, whether its reference is unknown to the catalog or a
// real pack reference carries definitions the catalog does not list.
func TestGraphEnforcementRejectsAnUncoveredPack(t *testing.T) {
	synthetic := semreg.PackRef{ID: "pack.synthetic", Version: "1.0.0"}
	empty := func(pack semreg.PackRef) semreg.DefinitionIndex {
		return semreg.DefinitionIndex{Pack: pack, Fields: []semreg.DefinitionRef{}, Services: []semreg.DefinitionRef{}, Capabilities: []semreg.DefinitionRef{}, Operations: []semreg.DefinitionRef{}, EffectRules: []semreg.DefinitionRef{}}
	}
	unknownPack := empty(synthetic)
	unknownPack.Services = []semreg.DefinitionRef{{Pack: synthetic, ID: "synthetic.service.thing", Version: "1.0.0"}}

	// Synthetic definitions under the EVSE pack reference, as the kernel
	// vectors use them, are not covered.
	syntheticUnderReal := empty(evsePack)
	syntheticUnderReal.Capabilities = []semreg.DefinitionRef{{Pack: evsePack, ID: "evse.capability.synthetic", Version: "1.0.0"}}

	// The accepted index with one definition removed leaves a catalog entry
	// the registered pack does not declare.
	partial := evse.New().Definitions()
	partial.Capabilities = partial.Capabilities[1:]

	// The accepted index with a definition of the wrong kind.
	wrongKind := evse.New().Definitions()
	wrongKind.Services = append([]semreg.DefinitionRef{}, wrongKind.Services...)
	moved := wrongKind.Capabilities[0]
	wrongKind.Capabilities = wrongKind.Capabilities[1:]
	wrongKind.Services = append(wrongKind.Services, moved)
	sortRefs(wrongKind.Services)

	emptyKnown := empty(evsePack)

	for name, index := range map[string]semreg.DefinitionIndex{
		"unknown pack":                 unknownPack,
		"empty unknown pack":           empty(synthetic),
		"synthetic under a real pack":  syntheticUnderReal,
		"empty real pack":              emptyKnown,
		"declared subset of real pack": partial,
		"definition of the wrong kind": wrongKind,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := graph.Enforced(thermal.New(), stubValidator{index: index})
			if semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
				t.Fatalf("Enforced = %v, want membership_missing", err)
			}
			catalog, err := graph.Embedded()
			if err != nil {
				t.Fatal(err)
			}
			if err := catalog.Covers(index); semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
				t.Fatalf("Covers = %v, want membership_missing", err)
			}
		})
	}
}

func sortRefs(refs []semreg.DefinitionRef) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && string(refs[j].ID) < string(refs[j-1].ID); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

// ACT-1: a decoded catalog whose bytes are not the pinned copy is refused,
// even when it holds the same entries.
func TestGraphEnforcementRejectsAnotherCatalog(t *testing.T) {
	reencoded, err := graph.Decode(encode(t, decodeDocument(t)))
	if err != nil {
		t.Fatal(err)
	}
	if reencoded.Digest() == graph.PinnedDigest {
		t.Fatal("re-encoded catalog kept the pinned digest")
	}
	_, err = semreg.NewGraphRegistry(reencoded, thermal.New())
	if semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
		t.Fatalf("NewGraphRegistry(other catalog) = %v, want membership_missing", err)
	}
	_, err = semreg.NewGraphPublicationKernel("asset:site", reencoded, thermal.New())
	if semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
		t.Fatalf("NewGraphPublicationKernel(other catalog) = %v, want membership_missing", err)
	}
	var none *graph.Catalog
	if _, err := semreg.NewGraphRegistry(none, thermal.New()); semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
		t.Fatalf("NewGraphRegistry(nil catalog) = %v, want membership_missing", err)
	}
}

// ACT-4 and G-NEG-ACT-3 over the committed catalog: the thermal temperature
// operation and its effect rule are unknown and therefore inadmissible; the
// mode operation is a member.
func TestGraphEnforcementRecordsUnknownOperationsInadmissible(t *testing.T) {
	index := thermal.New().Definitions()
	setTemperature := refByID(t, index.Operations, "thermal.operation.set_temperature")
	temperatureEffect := refByID(t, index.EffectRules, "thermal.effect.set_temperature")
	setMode := refByID(t, index.Operations, "thermal.operation.set_mode")
	modeEffect := refByID(t, index.EffectRules, "thermal.effect.set_mode")
	catalog := committedCatalog(t)
	for _, ref := range []semreg.DefinitionRef{setTemperature, temperatureEffect} {
		if disposition, _ := catalog.DispositionOf(graph.PackElement(ref)); disposition != graph.DispositionUnknown {
			t.Fatalf("%s disposition %s; the vector needs an unknown operation", ref.ID, disposition)
		}
	}

	registry, err := graph.Enforced(acceptedValidators()...)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.GraphAdmissionError(setTemperature, temperatureEffect); semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
		t.Fatalf("set_temperature admission = %v", err)
	}
	if err := registry.GraphAdmissionError(setTemperature, modeEffect); semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
		t.Fatalf("unknown operation with a member effect rule = %v", err)
	}
	if err := registry.GraphAdmissionError(setMode, temperatureEffect); semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
		t.Fatalf("member operation with an unknown effect rule = %v", err)
	}
	if err := registry.GraphAdmissionError(setMode, modeEffect); err != nil {
		t.Fatalf("set_mode admission = %v", err)
	}
	for _, validator := range acceptedValidators() {
		definitions := validator.Definitions()
		for _, operationRef := range definitions.Operations {
			for _, effectRef := range definitions.EffectRules {
				want := semreg.ErrorID("")
				for _, ref := range []semreg.DefinitionRef{operationRef, effectRef} {
					if disposition, _ := catalog.DispositionOf(graph.PackElement(ref)); disposition != graph.DispositionMember {
						want = semreg.MembershipMissing
					}
				}
				err := registry.GraphAdmissionError(operationRef, effectRef)
				if (want == "" && err != nil) || (want != "" && semreg.ErrorIdentifier(err) != want) {
					t.Fatalf("%s / %s admission = %v, want %q", operationRef.ID, effectRef.ID, err, want)
				}
			}
		}
	}

	plain, err := semreg.NewRegistry(acceptedValidators()...)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.GraphAdmissionError(setTemperature, temperatureEffect); err != nil {
		t.Fatalf("registry without the catalog refuses set_temperature: %v", err)
	}

	// The operation kernel refuses the intent before any route is resolved.
	options := operation.KernelOptions{MaxRetainedOperations: operation.DefaultMaxRetainedOperations}
	enforced, err := operation.NewGraphKernel(options, catalog, thermal.New())
	if err != nil {
		t.Fatal(err)
	}
	intent := operation.Intent{Kind: setTemperature, ExpectedEffect: operation.ExpectedEffect{Rule: temperatureEffect}}
	_, err = enforced.Admit(semreg.Snapshot{}, semreg.EvaluationContext{}, intent, nil)
	if semreg.ErrorIdentifier(err) != semreg.MembershipMissing {
		t.Fatalf("enforced Admit = %v, want membership_missing", err)
	}
	unenforced, err := operation.NewKernel(thermal.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unenforced.Admit(semreg.Snapshot{}, semreg.EvaluationContext{}, intent, nil); semreg.ErrorIdentifier(err) == semreg.MembershipMissing {
		t.Fatal("kernel without the catalog refused with membership_missing")
	}
}

// The GraphCatalog lookups agree with the catalog's own lookups for every
// entry and return detached copies.
func TestGraphEnforcementLookupsAgreeWithTheCatalog(t *testing.T) {
	catalog := committedCatalog(t)
	var asInterface semreg.GraphCatalog = catalog
	relations, kinds, members := 0, 0, 0
	for _, element := range catalog.Elements() {
		ref, packOwned := element.Ref.DefinitionRef()
		if packOwned {
			members++
			membership, ok := asInterface.Membership(ref)
			if !ok || string(membership.Disposition) != string(element.Disposition) || !samePlaneNames(membership.Planes, element.Planes) {
				t.Fatalf("Membership(%s) = %#v %v", element.Ref, membership, ok)
			}
			if len(membership.Planes) > 0 {
				membership.Planes[0] = "mutated"
				again, _ := asInterface.Membership(ref)
				if again.Planes[0] == "mutated" {
					t.Fatal("Membership returned shared planes")
				}
			}
			kind, ok := asInterface.DimensionKind(ref)
			wantKind, wantOK := catalog.DimensionKind(ref)
			if kind != wantKind || ok != wantOK {
				t.Fatalf("DimensionKind(%s) = %s %v", element.Ref, kind, ok)
			}
			subject, ok := asInterface.ServiceSubjectKind(ref)
			wantSubject, wantSubjectOK := catalog.ServiceSubjectKind(ref)
			if subject != wantSubject || ok != wantSubjectOK {
				t.Fatalf("ServiceSubjectKind(%s) = %s %v", element.Ref, subject, ok)
			}
			definition, ok := asInterface.RelationDefinition(ref)
			if (element.Relation != nil) != ok {
				t.Fatalf("RelationDefinition(%s) found %v", element.Ref, ok)
			}
			if ok {
				relations++
				want := element.Relation
				if definition.CoreType != string(want.CoreType) || !reflect.DeepEqual(definition.FromKinds, want.FromKinds) || !reflect.DeepEqual(definition.ToKinds, want.ToKinds) ||
					definition.Cardinality != string(want.Cardinality) || definition.OverTime != want.OverTime || definition.Medium != string(want.Medium) ||
					!reflect.DeepEqual(definition.BasisClasses, want.BasisClasses) || string(definition.Membership.Disposition) != string(element.Disposition) ||
					!samePlaneNames(definition.Membership.Planes, element.Planes) {
					t.Fatalf("RelationDefinition(%s) = %#v", element.Ref, definition)
				}
				definition.FromKinds[0] = "mutated"
				again, _ := asInterface.RelationDefinition(ref)
				if again.FromKinds[0] == "mutated" {
					t.Fatal("RelationDefinition returned shared kinds")
				}
			}
			continue
		}
		if element.ElementKind == graph.KindResourceKind {
			kinds++
			membership, ok := asInterface.ResourceKindMembership(element.Ref.ID)
			if !ok || string(membership.Disposition) != string(element.Disposition) || !samePlaneNames(membership.Planes, element.Planes) {
				t.Fatalf("ResourceKindMembership(%s) = %#v %v", element.Ref.ID, membership, ok)
			}
		}
	}
	if relations == 0 || kinds == 0 || members == 0 {
		t.Fatalf("lookups exercised %d relations, %d kinds, %d pack entries", relations, kinds, members)
	}

	// Each field of the pack reference is part of the key.
	field := thermal.New().Definitions().Fields[0]
	if _, ok := asInterface.Membership(field); !ok {
		t.Fatal("thermal field has no membership")
	}
	for name, variant := range map[string]semreg.DefinitionRef{
		"pack id":            {Pack: semreg.PackRef{ID: "helianthus.pack.other", Version: field.Pack.Version}, ID: field.ID, Version: field.Version},
		"pack version":       {Pack: semreg.PackRef{ID: field.Pack.ID, Version: "9.0.0"}, ID: field.ID, Version: field.Version},
		"definition id":      {Pack: field.Pack, ID: "thermal.other.field", Version: field.Version},
		"definition version": {Pack: field.Pack, ID: field.ID, Version: "9.0.0"},
	} {
		if _, ok := asInterface.Membership(variant); ok {
			t.Fatalf("%s variant has a membership", name)
		}
	}
	if _, ok := asInterface.ResourceKindMembership("resource.not_listed"); ok {
		t.Fatal("unlisted resource kind has a membership")
	}
	if _, ok := asInterface.ResourceKindMembership(semreg.DefinitionID(field.ID)); ok {
		t.Fatal("a pack definition is reported as a resource kind")
	}
	var nilCatalog *graph.Catalog
	if _, ok := nilCatalog.Membership(field); ok {
		t.Fatal("nil catalog has a membership")
	}
}

// SchemeListed answers from the identification scheme list only.
func TestGraphEnforcementSchemeListed(t *testing.T) {
	catalog := committedCatalog(t)
	for _, scheme := range catalog.IdentificationSchemes() {
		if !catalog.SchemeListed(scheme.ID) {
			t.Fatalf("scheme %s is not listed", scheme.ID)
		}
	}
	if catalog.SchemeListed("scheme.not_listed") {
		t.Fatal("unlisted scheme is listed")
	}
	doc := decodeDocument(t)
	doc["identification_schemes"] = []any{document{
		"id": "scheme.synthetic", "issuer_registry": graph.IssuerNone, "token_form": "opaque token",
		"admission": document{"arm": "owner_decision", "evidence": []any{"https://example.org/scheme"}},
	}}
	withScheme, err := graph.Decode(encode(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	if !withScheme.SchemeListed("scheme.synthetic") || withScheme.SchemeListed("scheme.synthetic.other") {
		t.Fatal("SchemeListed does not follow the scheme list")
	}
}

func samePlaneNames(got []semreg.GraphPlane, want []graph.PlaneID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if string(got[i]) != string(want[i]) {
			return false
		}
	}
	return true
}
