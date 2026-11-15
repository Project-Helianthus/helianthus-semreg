package semreg

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

// relationTestCatalog is the synthetic resource catalog with relation
// definitions. Every definition is owned by the test pack.
type relationTestCatalog struct {
	*resourceTestCatalog
	relations map[DefinitionID]GraphRelationDefinition
}

func relationTestDefinition(core string, from, to []DefinitionID, cardinality string, basis ...RelationBasis) GraphRelationDefinition {
	return GraphRelationDefinition{
		CoreType: core, FromKinds: from, ToKinds: to, Cardinality: cardinality, BasisClasses: basis,
		Membership: GraphMembership{Disposition: GraphMember, Planes: []GraphPlane{"physical"}},
	}
}

func newRelationTestCatalog(t *testing.T) *relationTestCatalog {
	t.Helper()
	kinds := func(ids ...DefinitionID) []DefinitionID { return ids }
	unknown := relationTestDefinition("groups", kinds("resource.pv_system"), kinds("resource.inverter"), "one_to_many", RelationConfiguration)
	unknown.Membership.Disposition = GraphUnknown
	unsupported := unknown
	unsupported.Membership.Disposition = GraphUnsupported
	supplies := relationTestDefinition("supplies", kinds("resource.inverter"), kinds("resource.inverter"), "many_to_many", RelationConfiguration)
	supplies.Medium = "electrical"
	return &relationTestCatalog{
		resourceTestCatalog: newResourceTestCatalog(t),
		relations: map[DefinitionID]GraphRelationDefinition{
			"relation.system_inverter":   relationTestDefinition("groups", kinds("resource.pv_system"), kinds("resource.inverter"), "one_to_many", RelationGateRule, RelationConfiguration),
			"relation.inverter_group":    relationTestDefinition("groups", kinds("resource.inverter"), kinds("resource.inverter"), "many_to_many", RelationConfiguration),
			"relation.inverter_input":    relationTestDefinition("contains", kinds("resource.inverter"), kinds("resource.dc_input"), "one_to_many", RelationGateRule, RelationNativeObservation),
			"relation.system_input":      relationTestDefinition("contains", kinds("resource.pv_system"), kinds("resource.dc_input"), "one_to_many", RelationConfiguration),
			"relation.inverter_meter":    relationTestDefinition("measures", kinds("resource.inverter"), kinds("resource.meter"), "one_to_one", RelationConfiguration),
			"relation.inverter_relays":   relationTestDefinition("relays", kinds("resource.inverter"), kinds("resource.inverter", "resource.meter"), "many_to_many", RelationConfiguration, RelationNativeObservation),
			"relation.meter_relays":      relationTestDefinition("relays", kinds("resource.meter"), kinds("resource.inverter"), "many_to_many", RelationConfiguration),
			"relation.inverter_session":  relationTestDefinition("hosts", kinds("resource.inverter"), kinds("resource.charging_session"), "many_to_many", RelationConfiguration),
			"relation.meter_session":     relationTestDefinition("hosts", kinds("resource.meter"), kinds("resource.charging_session"), "many_to_many", RelationConfiguration),
			"relation.inverter_supplies": supplies,
			"relation.unknown":           unknown,
			"relation.unsupported":       unsupported,
		},
	}
}

func (c *relationTestCatalog) RelationDefinition(ref DefinitionRef) (GraphRelationDefinition, bool) {
	definition, ok := c.relations[ref.ID]
	if !ok || ref.Pack != resourceTestPack || ref.Version != "1.0.0" {
		return GraphRelationDefinition{}, false
	}
	definition.FromKinds = append([]DefinitionID(nil), definition.FromKinds...)
	definition.ToKinds = append([]DefinitionID(nil), definition.ToKinds...)
	definition.BasisClasses = append([]RelationBasis(nil), definition.BasisClasses...)
	definition.Membership.Planes = append([]GraphPlane(nil), definition.Membership.Planes...)
	return definition, true
}

func newRelationKernel(t *testing.T) *PublicationKernel {
	t.Helper()
	kernel, err := NewGraphPublicationKernel("asset:pv", newRelationTestCatalog(t), &countingValidator{pack: resourceTestPack, index: resourceTestIndex()})
	if err != nil {
		t.Fatal(err)
	}
	return kernel
}

// relationBaseKernel publishes, from the configuration source, two inverters,
// two meters, two PV systems and a charging session hosted by the first
// inverter; then, from the driver, its binding, an identity link naming the
// first inverter and the inverter's two native inputs.
func relationBaseKernel(t *testing.T) *PublicationKernel {
	t.Helper()
	kernel := newRelationKernel(t)
	inverter := ResourceID("resource:inverter:01")
	config := resourceConfigBatch("1", "0")
	config.ResourceUpserts = []Resource{
		operatorResource("resource:inverter:01", "resource.inverter", AnchorEquipment, "inverter-1", nil),
		operatorResource("resource:inverter:02", "resource.inverter", AnchorEquipment, "inverter-2", nil),
		operatorResource("resource:meter:01", "resource.meter", AnchorEquipment, "meter-1", nil),
		operatorResource("resource:meter:02", "resource.meter", AnchorEquipment, "meter-2", nil),
		operatorResource("resource:pv-system:01", "resource.pv_system", AnchorFunction, "pv-system-1", nil),
		operatorResource("resource:pv-system:02", "resource.pv_system", AnchorFunction, "pv-system-2", nil),
		operatorResource("resource:session:01", "resource.charging_session", AnchorEpisode, "session-1", &inverter),
	}
	applyResourceBatch(t, kernel, config)
	driver := resourceDriverBatch("1", "1")
	driver.ResourceUpserts = []Resource{
		nativeSlotResource("resource:dc-input:01", "resource.dc_input", AnchorComponent, inverter, "1"),
		nativeSlotResource("resource:dc-input:02", "resource.dc_input", AnchorComponent, inverter, "2"),
	}
	applyResourceBatch(t, kernel, driver)
	return kernel
}

// relationNextBatch continues the publication order of one source path on
// the current snapshot of the kernel.
func relationNextBatch(t *testing.T, kernel *PublicationKernel, source SourceID, epoch SourceEpochID, generation Uint64) PublicationBatch {
	t.Helper()
	snapshot, _, ok := kernel.Current()
	expected, sequence := Uint64("0"), Uint64("1")
	if ok {
		expected = snapshot.Revisions.Semantic
		for _, cursor := range snapshot.Cursors {
			if cursor.SourceID == source && cursor.SourceEpochID == epoch && cursor.DriverGeneration == generation {
				sequence = increment(cursor.LastSequence)
			}
		}
	}
	return publicationBatch("asset:pv", source, epoch, generation, sequence, expected)
}

func relationConfigBatch(t *testing.T, kernel *PublicationKernel) PublicationBatch {
	t.Helper()
	return relationNextBatch(t, kernel, resourceConfigSource, resourceConfigEpoch, "1")
}

func relationDriverBatch(t *testing.T, kernel *PublicationKernel) PublicationBatch {
	t.Helper()
	return relationNextBatch(t, kernel, resourceDriverSource, resourceDriverEpoch, "1")
}

// configRelation is a qualified configuration instance with an operator
// origin; nativeRelation is a qualified gate-rule instance observed through
// the driver's binding.
func configRelation(definition DefinitionID, from, to ResourceID) RelationInstance {
	return graphRelation(resourceTestRef(definition), from, to)
}

func nativeRelation(definition DefinitionID, from, to ResourceID) RelationInstance {
	relation := configRelation(definition, from, to)
	relation.BasisClass, relation.Origin = RelationGateRule, driverOrigin()
	return relation
}

func configAbsence(definition DefinitionID, resource ResourceID, side AbsenceSide) RelationAbsence {
	return graphAbsence(resourceTestRef(definition), resource, side)
}

func nativeAbsence(definition DefinitionID, resource ResourceID, side AbsenceSide) RelationAbsence {
	absence := configAbsence(definition, resource, side)
	absence.BasisClass, absence.Origin = RelationGateRule, driverOrigin()
	return absence
}

func findRelation(snapshot Snapshot, definition DefinitionID, from, to ResourceID) (RelationInstance, bool) {
	for _, relation := range snapshot.Relations {
		if relation.Definition == resourceTestRef(definition) && relation.From == from && relation.To == to {
			return relation, true
		}
	}
	return RelationInstance{}, false
}

func findAbsence(snapshot Snapshot, definition DefinitionID, resource ResourceID, side AbsenceSide) (RelationAbsence, bool) {
	for _, absence := range snapshot.RelationAbsences {
		if absence.Definition == resourceTestRef(definition) && absence.Resource == resource && absence.Side == side {
			return absence, true
		}
	}
	return RelationAbsence{}, false
}

func requireRelationState(t *testing.T, snapshot Snapshot, definition DefinitionID, from, to ResourceID, state LinkState, revision Uint64) {
	t.Helper()
	relation, ok := findRelation(snapshot, definition, from, to)
	if !ok {
		t.Fatalf("%s %s -> %s is not in the snapshot", definition, from, to)
	}
	if relation.State != state || relation.Revision != revision {
		t.Fatalf("%s %s -> %s is %s at revision %s, want %s at revision %s", definition, from, to, relation.State, relation.Revision, state, revision)
	}
}

func relationsSorted(relations []RelationInstance) []RelationInstance {
	sort.Slice(relations, func(i, j int) bool {
		left, _ := relationInstanceGraphKey(relations[i])
		right, _ := relationInstanceGraphKey(relations[j])
		return left < right
	})
	return relations
}

// REL-1 and PLN-7: an instance or absence statement names a relation
// definition whose catalog disposition is member.
func TestRelationPublicationREL1Membership(t *testing.T) {
	for _, definition := range []DefinitionID{"relation.unknown", "relation.unsupported", "relation.undeclared"} {
		t.Run(string(definition)+" instance", func(t *testing.T) {
			kernel := relationBaseKernel(t)
			batch := relationConfigBatch(t, kernel)
			batch.RelationUpserts = []RelationInstance{configRelation(definition, "resource:pv-system:01", "resource:inverter:01")}
			rejectResourceBatch(t, kernel, batch, MembershipMissing)
		})
		t.Run(string(definition)+" absence", func(t *testing.T) {
			kernel := relationBaseKernel(t)
			batch := relationConfigBatch(t, kernel)
			batch.AbsenceUpserts = []RelationAbsence{configAbsence(definition, "resource:pv-system:01", AbsenceFrom)}
			rejectResourceBatch(t, kernel, batch, MembershipMissing)
		})
	}
	t.Run("definition of another version", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		batch := relationConfigBatch(t, kernel)
		relation := configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01")
		relation.Definition.Version = "2.0.0"
		batch.RelationUpserts = []RelationInstance{relation}
		rejectResourceBatch(t, kernel, batch, MembershipMissing)
	})
	t.Run("member definition", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		batch := relationConfigBatch(t, kernel)
		batch.RelationUpserts = []RelationInstance{configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01")}
		batch.AbsenceUpserts = []RelationAbsence{configAbsence("relation.system_inverter", "resource:pv-system:02", AbsenceFrom)}
		snapshot := applyResourceBatch(t, kernel, batch)
		requireRelationState(t, snapshot, "relation.system_inverter", "resource:pv-system:01", "resource:inverter:01", LinkQualified, "1")
		if _, ok := findAbsence(snapshot, "relation.system_inverter", "resource:pv-system:02", AbsenceFrom); !ok {
			t.Fatal("absence statement is not in the snapshot")
		}
	})
}

// REL-2 and G-NEG-REL-5: the starting end of an instance, and the resource
// of an absence statement, is a current resource of the asset of a kind the
// definition admits at that end.
func TestRelationPublicationREL2StartingEnd(t *testing.T) {
	withdraw := func(t *testing.T, kernel *PublicationKernel, id ResourceID) {
		t.Helper()
		resource := currentResource(t, kernel, id)
		resource.State, resource.Revision = ResourceWithdrawn, increment(resource.Revision)
		batch := relationConfigBatch(t, kernel)
		batch.ResourceUpserts = []Resource{resource}
		applyResourceBatch(t, kernel, batch)
	}
	cases := map[string]struct {
		withdrawn ResourceID
		relation  *RelationInstance
		absence   *RelationAbsence
		want      ErrorID
	}{
		"G-NEG-REL-5 from is no resource":        {relation: pointerTo(configRelation("relation.system_inverter", "resource:pv-system:99", "resource:inverter:01")), want: DanglingReference},
		"from is withdrawn":                      {withdrawn: "resource:pv-system:02", relation: pointerTo(configRelation("relation.system_inverter", "resource:pv-system:02", "resource:inverter:01")), want: DanglingReference},
		"from has a kind the end does not admit": {relation: pointerTo(configRelation("relation.system_inverter", "resource:meter:01", "resource:inverter:01")), want: IdentityDrift},
		"absence resource is no resource":        {absence: pointerTo(configAbsence("relation.system_inverter", "resource:pv-system:99", AbsenceFrom)), want: DanglingReference},
		"absence resource is withdrawn":          {withdrawn: "resource:pv-system:02", absence: pointerTo(configAbsence("relation.system_inverter", "resource:pv-system:02", AbsenceFrom)), want: DanglingReference},
		"absence from side with a to kind":       {absence: pointerTo(configAbsence("relation.system_inverter", "resource:inverter:01", AbsenceFrom)), want: IdentityDrift},
		"absence to side with a from kind":       {absence: pointerTo(configAbsence("relation.system_inverter", "resource:pv-system:01", AbsenceTo)), want: IdentityDrift},
		"absence to side with a to kind":         {absence: pointerTo(configAbsence("relation.system_inverter", "resource:inverter:01", AbsenceTo))},
		"absence from side with a from kind":     {absence: pointerTo(configAbsence("relation.system_inverter", "resource:pv-system:01", AbsenceFrom))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := relationBaseKernel(t)
			if tc.withdrawn != "" {
				withdraw(t, kernel, tc.withdrawn)
			}
			batch := relationConfigBatch(t, kernel)
			if tc.relation != nil {
				batch.RelationUpserts = []RelationInstance{*tc.relation}
			}
			if tc.absence != nil {
				batch.AbsenceUpserts = []RelationAbsence{*tc.absence}
			}
			if tc.want != "" {
				rejectResourceBatch(t, kernel, batch, tc.want)
				return
			}
			applyResourceBatch(t, kernel, batch)
		})
	}
}

// REL-3, G-NEG-REL-1 and G-NEG-REL-2: the basis class is admitted by the
// definition and agrees with the origin kind, and the basis is not empty.
func TestRelationPublicationREL3Basis(t *testing.T) {
	type record struct {
		driver   bool
		relation *RelationInstance
		absence  *RelationAbsence
		want     ErrorID
	}
	withClass := func(r RelationInstance, class RelationBasis) *RelationInstance {
		r.BasisClass = class
		return &r
	}
	absenceWithClass := func(a RelationAbsence, class RelationBasis) *RelationAbsence {
		a.BasisClass = class
		return &a
	}
	cases := map[string]record{
		"G-NEG-REL-1 configuration class with a native origin": {driver: true, relation: withClass(nativeRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01"), RelationConfiguration), want: RelationNotQualified},
		"G-NEG-REL-2 class the definition does not admit":      {driver: true, relation: withClass(nativeRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01"), RelationNativeObservation), want: RelationNotQualified},
		"gate rule class with an operator origin":              {relation: withClass(configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01"), RelationGateRule), want: RelationNotQualified},
		"native observation class with an operator origin":     {relation: withClass(configRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:01"), RelationNativeObservation), want: RelationNotQualified},
		"empty basis": {relation: func() *RelationInstance {
			r := configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01")
			r.Basis = []EvidenceRef{}
			return &r
		}(), want: RelationNotQualified},
		"absence with a class the definition does not admit": {absence: absenceWithClass(configAbsence("relation.inverter_input", "resource:inverter:01", AbsenceFrom), RelationConfiguration), want: RelationNotQualified},
		"absence configuration class with a native origin":   {driver: true, absence: absenceWithClass(nativeAbsence("relation.system_inverter", "resource:pv-system:01", AbsenceFrom), RelationConfiguration), want: RelationNotQualified},
		"absence with an empty basis": {absence: func() *RelationAbsence {
			a := configAbsence("relation.system_inverter", "resource:pv-system:01", AbsenceFrom)
			a.Basis = []EvidenceRef{}
			return &a
		}(), want: RelationNotQualified},
		"gate rule class with a native origin":          {driver: true, relation: pointerTo(nativeRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01"))},
		"native observation class with a native origin": {driver: true, relation: withClass(nativeRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:01"), RelationNativeObservation)},
		"configuration class with an operator origin":   {relation: pointerTo(configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01"))},
		"absence gate rule class with a native origin":  {driver: true, absence: pointerTo(nativeAbsence("relation.inverter_input", "resource:inverter:01", AbsenceFrom))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := relationBaseKernel(t)
			batch := relationConfigBatch(t, kernel)
			if tc.driver {
				batch = relationDriverBatch(t, kernel)
			}
			if tc.relation != nil {
				batch.RelationUpserts = []RelationInstance{*tc.relation}
			}
			if tc.absence != nil {
				batch.AbsenceUpserts = []RelationAbsence{*tc.absence}
			}
			if tc.want != "" {
				rejectResourceBatch(t, kernel, batch, tc.want)
				return
			}
			applyResourceBatch(t, kernel, batch)
		})
	}
}

// REL-4 and G-NEG-REL-3: a far end held by the asset is current and of an
// admitted kind; a far end the asset does not hold is homed elsewhere and
// stays unresolved until a graph cut, unless the asset itself places it here.
func TestRelationPublicationREL4FarEnd(t *testing.T) {
	ended := func(state ResourceState) func(t *testing.T, kernel *PublicationKernel) {
		return func(t *testing.T, kernel *PublicationKernel) {
			t.Helper()
			resource := currentResource(t, kernel, "resource:dc-input:02")
			resource.State, resource.Revision = state, increment(resource.Revision)
			batch := relationDriverBatch(t, kernel)
			batch.ResourceUpserts = []Resource{resource}
			applyResourceBatch(t, kernel, batch)
		}
	}
	cases := map[string]struct {
		setup     func(t *testing.T, kernel *PublicationKernel)
		relations []RelationInstance
		want      ErrorID
	}{
		"G-NEG-REL-3 to is withdrawn":          {setup: ended(ResourceWithdrawn), relations: []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:02")}, want: StaleRelation},
		"to is retired":                        {setup: ended(ResourceRetired), relations: []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:02")}, want: StaleRelation},
		"to has a kind the end does not admit": {relations: []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:meter:01")}, want: IdentityDrift},
		"to the asset places here is absent": {relations: []RelationInstance{
			configRelation("relation.inverter_relays", "resource:inverter:01", "resource:inverter:09"),
			configRelation("relation.inverter_relays", "resource:inverter:09", "resource:meter:01"),
		}, want: DanglingReference},
		"to is current and admitted": {relations: []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:02")}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := relationBaseKernel(t)
			if tc.setup != nil {
				tc.setup(t, kernel)
			}
			batch := relationDriverBatch(t, kernel)
			for i := range tc.relations {
				if tc.relations[i].Origin.Kind == OriginOperator {
					batch = relationConfigBatch(t, kernel)
				}
			}
			batch.RelationUpserts = relationsSorted(tc.relations)
			if tc.want != "" {
				rejectResourceBatch(t, kernel, batch, tc.want)
				return
			}
			applyResourceBatch(t, kernel, batch)
		})
	}

	t.Run("to homed in another asset is accepted unresolved", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		batch := relationConfigBatch(t, kernel)
		batch.RelationUpserts = []RelationInstance{configRelation("relation.inverter_relays", "resource:inverter:01", "resource:remote:inverter:07")}
		snapshot := applyResourceBatch(t, kernel, batch)
		requireRelationState(t, snapshot, "relation.inverter_relays", "resource:inverter:01", "resource:remote:inverter:07", LinkQualified, "1")
		if err := snapshot.Validate(); err != nil {
			t.Fatalf("snapshot with a far end homed elsewhere: %v", err)
		}
	})
}

// REL-5: a fence or a source retirement removes the instances and absence
// statements of a native basis in the same snapshot and keeps the
// configured ones; resources are not affected.
func TestRelationPublicationREL5FenceAndRetirement(t *testing.T) {
	publish := func(t *testing.T) *PublicationKernel {
		t.Helper()
		kernel := relationBaseKernel(t)
		native := relationDriverBatch(t, kernel)
		native.RelationUpserts = []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:01")}
		observed := nativeRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:02")
		observed.BasisClass = RelationNativeObservation
		native.RelationUpserts = relationsSorted(append(native.RelationUpserts, observed))
		native.AbsenceUpserts = []RelationAbsence{nativeAbsence("relation.inverter_input", "resource:inverter:02", AbsenceFrom)}
		applyResourceBatch(t, kernel, native)
		configured := relationConfigBatch(t, kernel)
		configured.RelationUpserts = []RelationInstance{
			configRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:01"),
			configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01"),
		}
		configured.AbsenceUpserts = []RelationAbsence{configAbsence("relation.system_inverter", "resource:pv-system:02", AbsenceFrom)}
		applyResourceBatch(t, kernel, configured)
		return kernel
	}
	check := func(t *testing.T, before, after Snapshot) {
		t.Helper()
		if _, ok := findRelation(after, "relation.inverter_input", "resource:inverter:01", "resource:dc-input:01"); ok {
			t.Fatal("gate-rule instance survived its binding generation")
		}
		if _, ok := findRelation(after, "relation.inverter_relays", "resource:inverter:01", "resource:meter:02"); ok {
			t.Fatal("native-observation instance survived its binding generation")
		}
		if _, ok := findAbsence(after, "relation.inverter_input", "resource:inverter:02", AbsenceFrom); ok {
			t.Fatal("native absence statement survived its binding generation")
		}
		requireRelationState(t, after, "relation.inverter_relays", "resource:inverter:01", "resource:meter:01", LinkQualified, "1")
		requireRelationState(t, after, "relation.system_inverter", "resource:pv-system:01", "resource:inverter:01", LinkQualified, "1")
		if _, ok := findAbsence(after, "relation.system_inverter", "resource:pv-system:02", AbsenceFrom); !ok {
			t.Fatal("configured absence statement was removed")
		}
		if !reflect.DeepEqual(after.Resources, before.Resources) {
			t.Fatal("removal changed a resource")
		}
		if after.Revisions.Structure != increment(before.Revisions.Structure) {
			t.Fatalf("structure %s after %s, want one increment", after.Revisions.Structure, before.Revisions.Structure)
		}
	}

	t.Run("generation fence", func(t *testing.T) {
		kernel := publish(t)
		before, _, _ := kernel.Current()
		fence := relationNextBatch(t, kernel, resourceDriverSource, resourceDriverEpoch, "2")
		fence.GenerationFences = []GenerationFence{publicationFence(resourceDriverSource, resourceDriverEpoch, "1", publicEvidence("5"))}
		check(t, before, applyResourceBatch(t, kernel, fence))
	})

	t.Run("source retirement", func(t *testing.T) {
		kernel := publish(t)
		before, _, _ := kernel.Current()
		epoch := SourceEpochID("epoch:pv:02")
		retire := relationNextBatch(t, kernel, resourceDriverSource, epoch, "1")
		retire.SourceUpserts = []SourceDescriptor{publicationSource(resourceDriverSource, epoch)}
		retire.SourceRetirements = []SourceEpochID{resourceDriverEpoch}
		check(t, before, applyResourceBatch(t, kernel, retire))
	})

	t.Run("an explicit withdrawal is the only removal of a configured instance", func(t *testing.T) {
		kernel := publish(t)
		batch := relationDriverBatch(t, kernel)
		batch.RelationWithdrawals = []RelationKey{{Definition: resourceTestRef("relation.inverter_relays"), From: "resource:inverter:01", To: "resource:meter:01"}}
		snapshot := applyResourceBatch(t, kernel, batch)
		if _, ok := findRelation(snapshot, "relation.inverter_relays", "resource:inverter:01", "resource:meter:01"); ok {
			t.Fatal("withdrawn configured instance is still current")
		}
	})

	t.Run("a native instance of a fenced generation is never published", func(t *testing.T) {
		kernel := publish(t)
		fence := relationNextBatch(t, kernel, resourceDriverSource, resourceDriverEpoch, "2")
		fence.GenerationFences = []GenerationFence{publicationFence(resourceDriverSource, resourceDriverEpoch, "1", publicEvidence("5"))}
		applyResourceBatch(t, kernel, fence)
		stale := relationNextBatch(t, kernel, resourceDriverSource, resourceDriverEpoch, "2")
		stale.RelationUpserts = []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:01")}
		rejectResourceBatch(t, kernel, stale, StaleDriverGeneration)
	})

	t.Run("a batch that fences its own generation publishes no relation record", func(t *testing.T) {
		for name, change := range map[string]func(*PublicationBatch){
			"instance": func(b *PublicationBatch) {
				b.RelationUpserts = []RelationInstance{configRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:02")}
			},
			"absence": func(b *PublicationBatch) {
				b.AbsenceUpserts = []RelationAbsence{configAbsence("relation.inverter_relays", "resource:inverter:02", AbsenceFrom)}
			},
		} {
			t.Run(name, func(t *testing.T) {
				kernel := relationBaseKernel(t)
				batch := relationDriverBatch(t, kernel)
				batch.GenerationFences = []GenerationFence{publicationFence(resourceDriverSource, resourceDriverEpoch, "1", publicEvidence("5"))}
				change(&batch)
				rejectResourceBatch(t, kernel, batch, StaleDriverGeneration)
			})
		}
	})
}

// REL-6, G-POS-REL-2 and G-POS-REL-3: after every batch the kernel derives
// conflict for every qualified instance in a cardinality violation of its
// definition, a forest violation of its core type across definitions, or a
// cycle of contains, groups, hosts or relays across definitions. It never
// chooses one instance, and a conflict ends when its cause ends.
func TestRelationPublicationREL6Conflicts(t *testing.T) {
	type edge struct {
		definition DefinitionID
		from, to   ResourceID
		state      LinkState
	}
	cases := map[string]struct {
		edges []edge
		want  []LinkState
	}{
		"G-POS-REL-2 two contains definitions claim one part": {[]edge{
			{"relation.inverter_input", "resource:inverter:01", "resource:dc-input:01", LinkQualified},
			{"relation.system_input", "resource:pv-system:01", "resource:dc-input:01", LinkQualified},
			{"relation.system_input", "resource:pv-system:01", "resource:dc-input:02", LinkQualified},
		}, []LinkState{LinkConflict, LinkConflict, LinkQualified}},
		"two hosts definitions claim one episode": {[]edge{
			{"relation.inverter_session", "resource:inverter:01", "resource:session:01", LinkQualified},
			{"relation.meter_session", "resource:meter:01", "resource:session:01", LinkQualified},
		}, []LinkState{LinkConflict, LinkConflict}},
		"a candidate claim takes no part": {[]edge{
			{"relation.inverter_input", "resource:inverter:01", "resource:dc-input:01", LinkQualified},
			{"relation.system_input", "resource:pv-system:01", "resource:dc-input:01", LinkCandidate},
		}, []LinkState{LinkQualified, LinkCandidate}},
		"a rejected claim takes no part": {[]edge{
			{"relation.inverter_input", "resource:inverter:01", "resource:dc-input:01", LinkQualified},
			{"relation.system_input", "resource:pv-system:01", "resource:dc-input:01", LinkRejected},
		}, []LinkState{LinkQualified, LinkRejected}},
		"groups is acyclic, not a forest": {[]edge{
			{"relation.system_inverter", "resource:pv-system:01", "resource:inverter:02", LinkQualified},
			{"relation.inverter_group", "resource:inverter:01", "resource:inverter:02", LinkQualified},
		}, []LinkState{LinkQualified, LinkQualified}},
		"G-POS-REL-3 relays cycle across definitions": {[]edge{
			{"relation.inverter_relays", "resource:inverter:01", "resource:meter:01", LinkQualified},
			{"relation.meter_relays", "resource:meter:01", "resource:inverter:01", LinkQualified},
			{"relation.inverter_relays", "resource:inverter:02", "resource:meter:01", LinkQualified},
		}, []LinkState{LinkConflict, LinkConflict, LinkQualified}},
		"relays cycle through three resources": {[]edge{
			{"relation.inverter_relays", "resource:inverter:01", "resource:inverter:02", LinkQualified},
			{"relation.inverter_relays", "resource:inverter:02", "resource:meter:01", LinkQualified},
			{"relation.meter_relays", "resource:meter:01", "resource:inverter:01", LinkQualified},
			{"relation.meter_relays", "resource:meter:02", "resource:inverter:01", LinkQualified},
		}, []LinkState{LinkConflict, LinkConflict, LinkConflict, LinkQualified}},
		"relays loop on one resource": {[]edge{
			{"relation.inverter_relays", "resource:inverter:01", "resource:inverter:01", LinkQualified},
		}, []LinkState{LinkConflict}},
		"groups cycle": {[]edge{
			{"relation.inverter_group", "resource:inverter:01", "resource:inverter:02", LinkQualified},
			{"relation.inverter_group", "resource:inverter:02", "resource:inverter:01", LinkQualified},
		}, []LinkState{LinkConflict, LinkConflict}},
		"a supplies cycle is no conflict": {[]edge{
			{"relation.inverter_supplies", "resource:inverter:01", "resource:inverter:02", LinkQualified},
			{"relation.inverter_supplies", "resource:inverter:02", "resource:inverter:01", LinkQualified},
		}, []LinkState{LinkQualified, LinkQualified}},
		"one to one: two far ends of one start": {[]edge{
			{"relation.inverter_meter", "resource:inverter:01", "resource:meter:01", LinkQualified},
			{"relation.inverter_meter", "resource:inverter:01", "resource:meter:02", LinkQualified},
			{"relation.inverter_meter", "resource:inverter:02", "resource:remote:meter:03", LinkQualified},
		}, []LinkState{LinkConflict, LinkConflict, LinkQualified}},
		"one to one: two starts of one far end": {[]edge{
			{"relation.inverter_meter", "resource:inverter:01", "resource:meter:01", LinkQualified},
			{"relation.inverter_meter", "resource:inverter:02", "resource:meter:01", LinkQualified},
		}, []LinkState{LinkConflict, LinkConflict}},
		"one to many: two starts of one far end": {[]edge{
			{"relation.system_inverter", "resource:pv-system:01", "resource:inverter:01", LinkQualified},
			{"relation.system_inverter", "resource:pv-system:02", "resource:inverter:01", LinkQualified},
			{"relation.system_inverter", "resource:pv-system:01", "resource:inverter:02", LinkQualified},
		}, []LinkState{LinkConflict, LinkConflict, LinkQualified}},
		"many to many: no cardinality": {[]edge{
			{"relation.inverter_relays", "resource:inverter:01", "resource:meter:01", LinkQualified},
			{"relation.inverter_relays", "resource:inverter:01", "resource:meter:02", LinkQualified},
			{"relation.inverter_relays", "resource:inverter:02", "resource:meter:01", LinkQualified},
		}, []LinkState{LinkQualified, LinkQualified, LinkQualified}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			kernel := relationBaseKernel(t)
			batch := relationConfigBatch(t, kernel)
			relations := make([]RelationInstance, 0, len(tc.edges))
			for _, e := range tc.edges {
				relation := configRelation(e.definition, e.from, e.to)
				if e.definition == "relation.inverter_input" {
					relation.BasisClass, relation.Origin = RelationGateRule, driverOrigin()
					continue
				}
				relation.State = e.state
				relations = append(relations, relation)
			}
			batch.RelationUpserts = relationsSorted(relations)
			// The driver's gate-rule instance is published first, by its own
			// source; the conflict is derived over both sources' instances.
			for _, e := range tc.edges {
				if e.definition == "relation.inverter_input" {
					driver := relationDriverBatch(t, kernel)
					driver.RelationUpserts = []RelationInstance{nativeRelation(e.definition, e.from, e.to)}
					applyResourceBatch(t, kernel, driver)
					batch = relationConfigBatch(t, kernel)
					batch.RelationUpserts = relationsSorted(relations)
				}
			}
			snapshot := applyResourceBatch(t, kernel, batch)
			for i, e := range tc.edges {
				relation, ok := findRelation(snapshot, e.definition, e.from, e.to)
				if !ok || relation.State != tc.want[i] {
					t.Fatalf("%s %s -> %s: got %+v, want %s", e.definition, e.from, e.to, relation, tc.want[i])
				}
			}
		})
	}

	t.Run("a conflict ends with its cause and every change counts as a revision", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		driver := relationDriverBatch(t, kernel)
		driver.RelationUpserts = []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:01")}
		first := applyResourceBatch(t, kernel, driver)
		requireRelationState(t, first, "relation.inverter_input", "resource:inverter:01", "resource:dc-input:01", LinkQualified, "1")

		claim := relationConfigBatch(t, kernel)
		claim.RelationUpserts = []RelationInstance{configRelation("relation.system_input", "resource:pv-system:01", "resource:dc-input:01")}
		second := applyResourceBatch(t, kernel, claim)
		requireRelationState(t, second, "relation.inverter_input", "resource:inverter:01", "resource:dc-input:01", LinkConflict, "2")
		requireRelationState(t, second, "relation.system_input", "resource:pv-system:01", "resource:dc-input:01", LinkConflict, "1")
		if second.Revisions.Structure != increment(first.Revisions.Structure) {
			t.Fatalf("structure %s after %s", second.Revisions.Structure, first.Revisions.Structure)
		}

		// The publisher repeats its record unchanged: the conflict stays and
		// nothing changes.
		repeat := relationDriverBatch(t, kernel)
		repeat.RelationUpserts = []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:01")}
		repeat.RelationUpserts[0].Revision = "2"
		third := applyResourceBatch(t, kernel, repeat)
		if !reflect.DeepEqual(third.Relations, second.Relations) || third.Revisions.Structure != second.Revisions.Structure {
			t.Fatal("an unchanged upsert changed the relations")
		}

		withdraw := relationConfigBatch(t, kernel)
		withdraw.RelationWithdrawals = []RelationKey{{Definition: resourceTestRef("relation.system_input"), From: "resource:pv-system:01", To: "resource:dc-input:01"}}
		fourth := applyResourceBatch(t, kernel, withdraw)
		requireRelationState(t, fourth, "relation.inverter_input", "resource:inverter:01", "resource:dc-input:01", LinkQualified, "3")
	})

	t.Run("a publisher-supplied conflict is invalid", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		batch := relationConfigBatch(t, kernel)
		relation := configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01")
		relation.State = LinkConflict
		batch.RelationUpserts = []RelationInstance{relation}
		rejectResourceBatch(t, kernel, batch, InvalidValue)
	})
}

// REL-7: an instance and an absence statement for one definition, resource
// and side are never both current. Each member of the absence key is varied
// alone against one instance.
func TestRelationPublicationREL7InstanceAndAbsence(t *testing.T) {
	instance := configRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:01")
	cases := map[string]struct {
		absence RelationAbsence
		want    ErrorID
	}{
		"from side of the starting end":                  {configAbsence("relation.inverter_relays", "resource:inverter:01", AbsenceFrom), InvalidValue},
		"to side of the far end":                         {configAbsence("relation.inverter_relays", "resource:meter:01", AbsenceTo), InvalidValue},
		"definition varied":                              {configAbsence("relation.inverter_meter", "resource:inverter:01", AbsenceFrom), ""},
		"resource varied":                                {configAbsence("relation.inverter_relays", "resource:inverter:02", AbsenceFrom), ""},
		"side varied on the starting end":                {configAbsence("relation.inverter_relays", "resource:inverter:01", AbsenceTo), ""},
		"far end on the from side of another definition": {configAbsence("relation.meter_relays", "resource:meter:01", AbsenceFrom), ""},
		"far end side of another relation":               {configAbsence("relation.inverter_relays", "resource:meter:02", AbsenceTo), ""},
	}
	for name, tc := range cases {
		for _, together := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s in one batch %t", name, together), func(t *testing.T) {
				kernel := relationBaseKernel(t)
				batch := relationConfigBatch(t, kernel)
				batch.RelationUpserts = []RelationInstance{instance}
				if !together {
					applyResourceBatch(t, kernel, batch)
					batch = relationConfigBatch(t, kernel)
				}
				batch.AbsenceUpserts = []RelationAbsence{tc.absence}
				if tc.want != "" {
					rejectResourceBatch(t, kernel, batch, tc.want)
					return
				}
				applyResourceBatch(t, kernel, batch)
			})
		}
	}
	t.Run("an instance against an existing absence statement", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		batch := relationConfigBatch(t, kernel)
		batch.AbsenceUpserts = []RelationAbsence{configAbsence("relation.inverter_relays", "resource:meter:01", AbsenceTo)}
		applyResourceBatch(t, kernel, batch)
		next := relationConfigBatch(t, kernel)
		next.RelationUpserts = []RelationInstance{instance}
		rejectResourceBatch(t, kernel, next, InvalidValue)
	})
}

// Section 11.5: at most 8192 relation instances and 1024 absence statements
// per asset; one more is rejected and never truncated.
func TestRelationPublicationBounds(t *testing.T) {
	t.Run("relation instances", func(t *testing.T) {
		relations := func(n int) []RelationInstance {
			result := make([]RelationInstance, n)
			for i := range result {
				result[i] = configRelation("relation.inverter_relays", "resource:inverter:01", ResourceID(fmt.Sprintf("resource:remote:%05d", i)))
			}
			return result
		}
		kernel := relationBaseKernel(t)
		over := relationConfigBatch(t, kernel)
		over.RelationUpserts = relations(maxSnapshotRelations + 1)
		rejectResourceBatch(t, kernel, over, BoundsExceeded)
		at := relationConfigBatch(t, kernel)
		at.RelationUpserts = relations(maxSnapshotRelations)
		if snapshot := applyResourceBatch(t, kernel, at); len(snapshot.Relations) != maxSnapshotRelations {
			t.Fatalf("got %d relation instances", len(snapshot.Relations))
		}
		one := relationConfigBatch(t, kernel)
		one.RelationUpserts = []RelationInstance{configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01")}
		rejectResourceBatch(t, kernel, one, BoundsExceeded)
	})
	t.Run("absence statements", func(t *testing.T) {
		systems := func(n int) ([]Resource, []RelationAbsence) {
			resources := make([]Resource, n)
			absences := make([]RelationAbsence, n)
			for i := range resources {
				id := ResourceID(fmt.Sprintf("resource:system:%05d", i))
				resources[i] = operatorResource(id, "resource.pv_system", AnchorFunction, fmt.Sprintf("system-%05d", i), nil)
				absences[i] = configAbsence("relation.system_inverter", id, AbsenceFrom)
			}
			return resources, absences
		}
		kernel := newRelationKernel(t)
		over := resourceConfigBatch("1", "0")
		over.ResourceUpserts, over.AbsenceUpserts = systems(maxSnapshotAbsences + 1)
		rejectResourceBatch(t, kernel, over, BoundsExceeded)
		at := resourceConfigBatch("1", "0")
		at.ResourceUpserts, at.AbsenceUpserts = systems(maxSnapshotAbsences)
		if snapshot := applyResourceBatch(t, kernel, at); len(snapshot.RelationAbsences) != maxSnapshotAbsences {
			t.Fatalf("got %d absence statements", len(snapshot.RelationAbsences))
		}
	})
}

// G-POS-PUB-1 and PUB-3: an operator record needs no second source. A
// configuration instance whose starting end the configuration source holds
// is published by that source beside the driver's source of the same asset;
// one whose starting end is the driver's resource is published through the
// driver's own source. Neither dies with the driver's generation.
func TestRelationPublicationSecondSourceConfiguration(t *testing.T) {
	kernel := relationBaseKernel(t)
	before, _, _ := kernel.Current()
	if len(before.Sources) != 2 {
		t.Fatalf("asset holds %d sources, want 2", len(before.Sources))
	}
	config := relationConfigBatch(t, kernel)
	config.RelationUpserts = []RelationInstance{configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01")}
	applyResourceBatch(t, kernel, config)
	injected := relationDriverBatch(t, kernel)
	injected.RelationUpserts = []RelationInstance{configRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:01")}
	snapshot := applyResourceBatch(t, kernel, injected)
	requireRelationState(t, snapshot, "relation.system_inverter", "resource:pv-system:01", "resource:inverter:01", LinkQualified, "1")
	requireRelationState(t, snapshot, "relation.inverter_relays", "resource:inverter:01", "resource:meter:01", LinkQualified, "1")

	fence := relationNextBatch(t, kernel, resourceDriverSource, resourceDriverEpoch, "2")
	fence.GenerationFences = []GenerationFence{publicationFence(resourceDriverSource, resourceDriverEpoch, "1", publicEvidence("5"))}
	after := applyResourceBatch(t, kernel, fence)
	if !reflect.DeepEqual(after.Relations, snapshot.Relations) || after.Revisions.Structure != snapshot.Revisions.Structure {
		t.Fatal("a fence changed configured instances")
	}
}

// Withdrawals name a current record by its whole key. Each key member is
// varied alone; a withdrawal keyed on fewer members would remove the record.
func TestRelationPublicationWithdrawals(t *testing.T) {
	setup := func(t *testing.T) *PublicationKernel {
		t.Helper()
		kernel := relationBaseKernel(t)
		batch := relationConfigBatch(t, kernel)
		batch.RelationUpserts = []RelationInstance{configRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:01")}
		batch.AbsenceUpserts = []RelationAbsence{configAbsence("relation.system_inverter", "resource:pv-system:02", AbsenceFrom)}
		applyResourceBatch(t, kernel, batch)
		return kernel
	}
	relationKey := RelationKey{Definition: resourceTestRef("relation.inverter_relays"), From: "resource:inverter:01", To: "resource:meter:01"}
	absenceKey := RelationAbsenceKey{Definition: resourceTestRef("relation.system_inverter"), Resource: "resource:pv-system:02", Side: AbsenceFrom}
	relationCases := map[string]func(RelationKey) RelationKey{
		"definition": func(k RelationKey) RelationKey { k.Definition = resourceTestRef("relation.inverter_meter"); return k },
		"version":    func(k RelationKey) RelationKey { k.Definition.Version = "1.0.1"; return k },
		"from":       func(k RelationKey) RelationKey { k.From = "resource:inverter:02"; return k },
		"to":         func(k RelationKey) RelationKey { k.To = "resource:meter:02"; return k },
	}
	for name, vary := range relationCases {
		t.Run("relation key with another "+name, func(t *testing.T) {
			kernel := setup(t)
			batch := relationConfigBatch(t, kernel)
			batch.RelationWithdrawals = []RelationKey{vary(relationKey)}
			rejectResourceBatch(t, kernel, batch, DanglingReference)
		})
	}
	absenceCases := map[string]func(RelationAbsenceKey) RelationAbsenceKey{
		"definition": func(k RelationAbsenceKey) RelationAbsenceKey {
			k.Definition = resourceTestRef("relation.system_input")
			return k
		},
		"resource": func(k RelationAbsenceKey) RelationAbsenceKey { k.Resource = "resource:pv-system:01"; return k },
		"side":     func(k RelationAbsenceKey) RelationAbsenceKey { k.Side = AbsenceTo; return k },
	}
	for name, vary := range absenceCases {
		t.Run("absence key with another "+name, func(t *testing.T) {
			kernel := setup(t)
			batch := relationConfigBatch(t, kernel)
			batch.AbsenceWithdrawals = []RelationAbsenceKey{vary(absenceKey)}
			rejectResourceBatch(t, kernel, batch, DanglingReference)
		})
	}
	t.Run("exact keys", func(t *testing.T) {
		kernel := setup(t)
		before, _, _ := kernel.Current()
		batch := relationConfigBatch(t, kernel)
		batch.RelationWithdrawals = []RelationKey{relationKey}
		batch.AbsenceWithdrawals = []RelationAbsenceKey{absenceKey}
		after := applyResourceBatch(t, kernel, batch)
		if after.Relations != nil || after.RelationAbsences != nil {
			t.Fatalf("withdrawn records remain: %+v %+v", after.Relations, after.RelationAbsences)
		}
		if after.Revisions.Structure != increment(before.Revisions.Structure) {
			t.Fatalf("structure %s after %s", after.Revisions.Structure, before.Revisions.Structure)
		}
	})
	t.Run("upsert and withdrawal of one key in one batch", func(t *testing.T) {
		kernel := setup(t)
		batch := relationConfigBatch(t, kernel)
		batch.RelationUpserts = []RelationInstance{configRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:01")}
		batch.RelationWithdrawals = []RelationKey{relationKey}
		rejectResourceBatch(t, kernel, batch, InvalidValue)
		batch = relationConfigBatch(t, kernel)
		batch.AbsenceUpserts = []RelationAbsence{configAbsence("relation.system_inverter", "resource:pv-system:02", AbsenceFrom)}
		batch.AbsenceWithdrawals = []RelationAbsenceKey{absenceKey}
		rejectResourceBatch(t, kernel, batch, InvalidValue)
	})
	t.Run("a native record belongs to its source", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		driver := relationDriverBatch(t, kernel)
		driver.RelationUpserts = []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:01")}
		driver.AbsenceUpserts = []RelationAbsence{nativeAbsence("relation.inverter_input", "resource:inverter:02", AbsenceFrom)}
		applyResourceBatch(t, kernel, driver)
		foreign := relationConfigBatch(t, kernel)
		foreign.RelationWithdrawals = []RelationKey{{Definition: resourceTestRef("relation.inverter_input"), From: "resource:inverter:01", To: "resource:dc-input:01"}}
		rejectResourceBatch(t, kernel, foreign, InvalidValue)
		foreign = relationConfigBatch(t, kernel)
		foreign.AbsenceWithdrawals = []RelationAbsenceKey{{Definition: resourceTestRef("relation.inverter_input"), Resource: "resource:inverter:02", Side: AbsenceFrom}}
		rejectResourceBatch(t, kernel, foreign, InvalidValue)
		own := relationDriverBatch(t, kernel)
		own.RelationWithdrawals = []RelationKey{{Definition: resourceTestRef("relation.inverter_input"), From: "resource:inverter:01", To: "resource:dc-input:01"}}
		own.AbsenceWithdrawals = []RelationAbsenceKey{{Definition: resourceTestRef("relation.inverter_input"), Resource: "resource:inverter:02", Side: AbsenceFrom}}
		applyResourceBatch(t, kernel, own)
	})
}

// A native record's origin is a current binding of the batch's own source,
// epoch and generation; a record keeps its revision unless it changes.
func TestRelationPublicationOriginAndRevision(t *testing.T) {
	t.Run("native origin from another source", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		batch := relationConfigBatch(t, kernel)
		batch.RelationUpserts = []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:01")}
		rejectResourceBatch(t, kernel, batch, InvalidValue)
	})
	t.Run("native origin through no binding of the asset", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		batch := relationDriverBatch(t, kernel)
		relation := nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:01")
		relation.Origin = nativeOrigin("binding:elsewhere", resourceDriverSource, resourceDriverEpoch)
		batch.RelationUpserts = []RelationInstance{relation}
		rejectResourceBatch(t, kernel, batch, DanglingReference)
		absence := nativeAbsence("relation.inverter_input", "resource:inverter:02", AbsenceFrom)
		absence.Origin = nativeOrigin(resourceDriverBind, resourceDriverSource, "epoch:pv:09")
		batch.RelationUpserts, batch.AbsenceUpserts = nil, []RelationAbsence{absence}
		rejectResourceBatch(t, kernel, batch, DanglingReference)
	})
	t.Run("a change needs the next revision", func(t *testing.T) {
		kernel := relationBaseKernel(t)
		batch := relationConfigBatch(t, kernel)
		batch.RelationUpserts = []RelationInstance{configRelation("relation.inverter_relays", "resource:inverter:01", "resource:meter:01")}
		batch.AbsenceUpserts = []RelationAbsence{configAbsence("relation.system_inverter", "resource:pv-system:02", AbsenceFrom)}
		first := applyResourceBatch(t, kernel, batch)

		same := relationConfigBatch(t, kernel)
		same.RelationUpserts, same.AbsenceUpserts = batch.RelationUpserts, batch.AbsenceUpserts
		if got := applyResourceBatch(t, kernel, same); got.Revisions.Structure != first.Revisions.Structure {
			t.Fatal("an unchanged upsert moved the structure revision")
		}

		changed := batch.RelationUpserts[0]
		changed.State = LinkCandidate
		stale := relationConfigBatch(t, kernel)
		stale.RelationUpserts = []RelationInstance{changed}
		rejectResourceBatch(t, kernel, stale, RevisionConflict)
		changedAbsence := batch.AbsenceUpserts[0]
		changedAbsence.State = LinkQualified
		stale.RelationUpserts, stale.AbsenceUpserts = nil, []RelationAbsence{changedAbsence}
		rejectResourceBatch(t, kernel, stale, RevisionConflict)

		changed.Revision, changedAbsence.Revision = "2", "2"
		next := relationConfigBatch(t, kernel)
		next.RelationUpserts, next.AbsenceUpserts = []RelationInstance{changed}, []RelationAbsence{changedAbsence}
		snapshot := applyResourceBatch(t, kernel, next)
		requireRelationState(t, snapshot, "relation.inverter_relays", "resource:inverter:01", "resource:meter:01", LinkCandidate, "2")
		if absence, _ := findAbsence(snapshot, "relation.system_inverter", "resource:pv-system:02", AbsenceFrom); absence.State != LinkQualified || absence.Revision != "2" {
			t.Fatalf("absence statement %+v", absence)
		}
		if snapshot.Revisions.Structure != increment(first.Revisions.Structure) {
			t.Fatalf("structure %s after %s", snapshot.Revisions.Structure, first.Revisions.Structure)
		}
	})
}

// RES-8 extended: a resource named by a current relation instance or absence
// statement is neither withdrawn nor retired.
func TestRelationPublicationRES8(t *testing.T) {
	cases := map[string]struct {
		relation *RelationInstance
		absence  *RelationAbsence
		resource ResourceID
	}{
		"starting end of an instance": {relation: pointerTo(configRelation("relation.inverter_relays", "resource:inverter:02", "resource:meter:01")), resource: "resource:inverter:02"},
		"far end of an instance":      {relation: pointerTo(configRelation("relation.inverter_relays", "resource:inverter:02", "resource:meter:01")), resource: "resource:meter:01"},
		"resource of an absence":      {absence: pointerTo(configAbsence("relation.inverter_relays", "resource:inverter:02", AbsenceFrom)), resource: "resource:inverter:02"},
		"far end of a rejected record": {relation: func() *RelationInstance {
			r := configRelation("relation.inverter_relays", "resource:inverter:02", "resource:meter:01")
			r.State = LinkRejected
			return &r
		}(), resource: "resource:meter:01"},
	}
	for name, tc := range cases {
		for _, state := range []ResourceState{ResourceWithdrawn, ResourceRetired} {
			t.Run(name+" "+string(state), func(t *testing.T) {
				kernel := relationBaseKernel(t)
				batch := relationConfigBatch(t, kernel)
				if tc.relation != nil {
					batch.RelationUpserts = []RelationInstance{*tc.relation}
				}
				if tc.absence != nil {
					batch.AbsenceUpserts = []RelationAbsence{*tc.absence}
				}
				applyResourceBatch(t, kernel, batch)
				ended := currentResource(t, kernel, tc.resource)
				ended.State, ended.Revision = state, increment(ended.Revision)
				end := relationConfigBatch(t, kernel)
				end.ResourceUpserts = []Resource{ended}
				rejectResourceBatch(t, kernel, end, DanglingReference)
				if tc.relation != nil {
					end.RelationWithdrawals = []RelationKey{{Definition: tc.relation.Definition, From: tc.relation.From, To: tc.relation.To}}
				}
				if tc.absence != nil {
					end.AbsenceWithdrawals = []RelationAbsenceKey{{Definition: tc.absence.Definition, Resource: tc.absence.Resource, Side: tc.absence.Side}}
				}
				applyResourceBatch(t, kernel, end)
			})
		}
	}
}

// Snapshot validation resolves every relation and absence reference inside
// the snapshot, except a far end the asset does not hold.
func TestRelationPublicationSnapshotValidation(t *testing.T) {
	kernel := relationBaseKernel(t)
	driver := relationDriverBatch(t, kernel)
	driver.RelationUpserts = []RelationInstance{nativeRelation("relation.inverter_input", "resource:inverter:01", "resource:dc-input:01")}
	applyResourceBatch(t, kernel, driver)
	config := relationConfigBatch(t, kernel)
	config.RelationUpserts = []RelationInstance{configRelation("relation.system_inverter", "resource:pv-system:01", "resource:inverter:01")}
	config.AbsenceUpserts = []RelationAbsence{configAbsence("relation.system_inverter", "resource:pv-system:02", AbsenceFrom)}
	base := applyResourceBatch(t, kernel, config)
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	withdrawnResource := func(s *Snapshot, id ResourceID) {
		for i := range s.Resources {
			if s.Resources[i].ResourceID == id {
				s.Resources[i].State = ResourceWithdrawn
			}
		}
	}
	cases := map[string]struct {
		change func(s *Snapshot)
		want   ErrorID
	}{
		"starting end is no resource": {func(s *Snapshot) { s.Relations[1].From = "resource:pv-system:99" }, DanglingReference},
		"starting end is withdrawn":   {func(s *Snapshot) { withdrawnResource(s, "resource:pv-system:01") }, DanglingReference},
		"held far end is withdrawn":   {func(s *Snapshot) { withdrawnResource(s, "resource:dc-input:01") }, DanglingReference},
		"absence resource is absent":  {func(s *Snapshot) { s.RelationAbsences[0].Resource = "resource:pv-system:99" }, DanglingReference},
		"absence resource withdrawn":  {func(s *Snapshot) { withdrawnResource(s, "resource:pv-system:02") }, DanglingReference},
		"native origin without its binding": {func(s *Snapshot) {
			binding := NativeBindingID("binding:elsewhere")
			s.Relations[0].Origin.BindingID = &binding
		}, DanglingReference},
		"native origin through a fenced binding": {func(s *Snapshot) {
			for i := range s.Bindings {
				s.Bindings[i].State = BindingFenced
			}
		}, DanglingReference},
		"basis class against the origin kind": {func(s *Snapshot) { s.Relations[1].BasisClass = RelationGateRule }, RelationNotQualified},
		"instance and absence statement": {func(s *Snapshot) {
			s.RelationAbsences[0].Resource = "resource:pv-system:01"
		}, InvalidValue},
		"far end homed elsewhere": {func(s *Snapshot) { s.Relations[1].To = "resource:remote:inverter:07" }, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			snapshot := cloneSnapshot(base)
			tc.change(&snapshot)
			snapshot.Relations = relationsSorted(snapshot.Relations)
			recomputeSnapshotID(t, &snapshot)
			err := snapshot.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			requireID(t, err, tc.want)
		})
	}
}

// A kernel without the catalog, or with a catalog without the resource
// lookups, still refuses every relation member.
func TestRelationPublicationWithoutEnforcement(t *testing.T) {
	plain, err := NewPublicationKernel("asset:pv", &countingValidator{pack: resourceTestPack, index: resourceTestIndex()})
	if err != nil {
		t.Fatal(err)
	}
	batch := resourceConfigBatch("1", "0")
	batch.AbsenceWithdrawals = []RelationAbsenceKey{{Definition: resourceTestRef("relation.system_inverter"), Resource: "resource:pv-system:01", Side: AbsenceFrom}}
	rejectResourceBatch(t, plain, batch, MembershipMissing)
	batch.AbsenceWithdrawals = nil
	batch.RelationWithdrawals = []RelationKey{{Definition: resourceTestRef("relation.system_inverter"), From: "resource:pv-system:01", To: "resource:inverter:01"}}
	rejectResourceBatch(t, plain, batch, MembershipMissing)

	limited := coveringCatalog(t, nil)
	limited.covered = map[PackRef]DefinitionIndex{resourceTestPack: resourceTestIndex()}
	kernel, err := NewGraphPublicationKernel("asset:pv", limited, &countingValidator{pack: resourceTestPack, index: resourceTestIndex()})
	if err != nil {
		t.Fatal(err)
	}
	rejectResourceBatch(t, kernel, batch, MembershipMissing)
}
