package semreg_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

// listedSchemes is the embedded element catalog with the synthetic
// identification scheme of the graph acceptance vectors listed. The accepted
// catalog lists no scheme yet; every other answer is the catalog's own.
type listedSchemes struct {
	*graph.Catalog
}

func (listedSchemes) SchemeListed(id semreg.DefinitionID) bool { return id == "synthetic.serial" }

var (
	catalogPV      = semreg.PackRef{ID: "helianthus.pack.pv", Version: "1.0.0"}
	catalogThermal = semreg.PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}
	catalogMono    = semreg.MonotonicPoint{ClockEpochID: "clock-epoch:publication", Nanoseconds: "100"}
)

func catalogKernel(t *testing.T, validators ...semreg.PackValidator) *semreg.PublicationKernel {
	t.Helper()
	embedded, err := graph.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := semreg.NewGraphPublicationKernel("asset:pv:01", listedSchemes{embedded}, validators...)
	if err != nil {
		t.Fatal(err)
	}
	return kernel
}

func catalogEvidence(n int) []semreg.EvidenceRef {
	return []semreg.EvidenceRef{{Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest(fmt.Sprintf("sha256:%064x", n)), Contract: "test.contract/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone}}
}

func catalogTime(ns string) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(ns), ClockID: "clock.utc", UncertaintyNS: "0"}
}

// catalogIdentityKey is the anchor key: sha256 over the canonical bytes of
// the identity object. A map of short ASCII strings marshals to its canonical
// form: sorted members, no whitespace, no escapes.
func catalogIdentityKey(t *testing.T, scheme, issuer, token string) semreg.Digest {
	t.Helper()
	canonical, err := json.Marshal(map[string]string{"scheme": scheme, "issuer": issuer, "token": token})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func catalogBatch(source semreg.SourceID, epoch semreg.SourceEpochID, sequence, expected semreg.Uint64) semreg.PublicationBatch {
	batch := semreg.PublicationBatch{
		Contract: semreg.ContractKernelV1, BatchID: semreg.BatchID("batch:" + string(source) + ":" + string(sequence)),
		AssetID: "asset:pv:01", SourceID: source, SourceEpochID: epoch, DriverGeneration: "1", Sequence: sequence,
		ExpectedSemanticRevision: expected, ObservedAt: catalogTime("20" + string(sequence)),
		SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{}, IdentityLinkUpserts: []semreg.IdentityLink{},
		FactUpserts: []semreg.FactCandidate{}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{},
		CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{},
	}
	if sequence == "1" {
		batch.SourceUpserts = []semreg.SourceDescriptor{{SourceID: source, SourceEpochID: epoch, ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "release-1", RegistryEvidence: catalogEvidence(1)[0], StartedAt: catalogTime("100"), State: semreg.SourceCurrent, Revision: "1"}}
	}
	return batch
}

func catalogApply(t *testing.T, kernel *semreg.PublicationKernel, batch semreg.PublicationBatch, want semreg.ErrorID) semreg.Snapshot {
	t.Helper()
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, _, err := kernel.Apply(batch, catalogMono)
	if want == "" {
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	if semreg.ErrorIdentifier(err) != want {
		t.Fatalf("got %v, want %s", err, want)
	}
	return semreg.Snapshot{}
}

func catalogFact(id semreg.CandidateID, pack semreg.PackRef, fact, dimension semreg.DefinitionID, text string, coefficient string, exponent int32, unit semreg.DefinitionID, binding semreg.NativeBindingID) semreg.FactCandidate {
	source, epoch, generation := semreg.SourceID("source:pv:01"), semreg.SourceEpochID("source-epoch:pv:01"), semreg.Uint64("1")
	return semreg.FactCandidate{
		CandidateID: id,
		Key:         semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: fact, Dimensions: []semreg.Dimension{{ID: dimension, Value: semreg.Value{Kind: semreg.ValueText, Text: &text}}}},
		Value:       &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: coefficient, Exponent10: exponent}, Unit: unit}},
		Quality:     semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}},
		Times: semreg.Times{
			ReceivedAt: catalogTime("100"), ReceiptMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:a", Nanoseconds: "5"},
			EvaluatedAt: catalogTime("101"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:a", Nanoseconds: "6"},
		},
		FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:a", Version: "1.0.0", FreshForNS: "30", RetainForNS: "300", MaxWallUncertaintyNS: "1"},
		BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
		Origin:   semreg.OriginRef{OriginID: semreg.OriginID("origin:" + string(id)), Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: catalogEvidence(4)},
		Evidence: catalogEvidence(5), Revision: "1",
	}
}

func catalogOperatorResource(id semreg.ResourceID, kind semreg.DefinitionID, class semreg.AnchorClass, slot string, parent *semreg.ResourceID) semreg.Resource {
	return semreg.Resource{
		ResourceID: id, Kind: kind,
		Anchor: semreg.ResourceAnchor{Class: class, Basis: semreg.AnchorOperatorSlot, Parent: parent, Slot: &slot, Evidence: catalogEvidence(6)},
		State:  semreg.ResourceCurrent, Origin: semreg.OriginRef{OriginID: semreg.OriginID("origin:operator:" + slot), Kind: semreg.OriginOperator, Evidence: catalogEvidence(7)}, Revision: "1",
	}
}

func catalogDriverOrigin() semreg.OriginRef {
	source, epoch, binding := semreg.SourceID("source:pv:01"), semreg.SourceEpochID("source-epoch:pv:01"), semreg.NativeBindingID("binding:pv:01")
	return semreg.OriginRef{OriginID: "origin:binding:pv:01", Kind: semreg.OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: catalogEvidence(8)}
}

func catalogInput(id semreg.ResourceID, slot string) semreg.Resource {
	parent := semreg.ResourceID("resource:inverter:01")
	return semreg.Resource{
		ResourceID: id, Kind: "resource.dc_input",
		Anchor: semreg.ResourceAnchor{Class: semreg.AnchorComponent, Basis: semreg.AnchorNativeSlot, Parent: &parent, Slot: &slot, Evidence: catalogEvidence(9)},
		State:  semreg.ResourceCurrent, Origin: catalogDriverOrigin(), Revision: "1",
	}
}

// pvCatalogKernel publishes, against the accepted element catalog and the PV
// pack, a configured PV system and inverter, then the driver's binding with
// its identity link to the inverter, the inverter's two native inputs, a
// frequency fact about the inverter, a power fact about one input and the
// inverter service.
func pvCatalogKernel(t *testing.T) *semreg.PublicationKernel {
	t.Helper()
	kernel := catalogKernel(t, pv.New())
	config := catalogBatch("source:configuration:01", "source-epoch:configuration:01", "1", "0")
	config.ResourceUpserts = []semreg.Resource{
		catalogOperatorResource("resource:inverter:01", "resource.inverter", semreg.AnchorEquipment, "inverter-1", nil),
		catalogOperatorResource("resource:pv-system:01", "resource.pv_system", semreg.AnchorFunction, "pv-system-1", nil),
	}
	catalogApply(t, kernel, config, "")

	resource := semreg.ResourceID("resource:inverter:01")
	driver := catalogBatch("source:pv:01", "source-epoch:pv:01", "1", "1")
	driver.BindingUpserts = []semreg.NativeBinding{{BindingID: "binding:pv:01", AssetID: "asset:pv:01", SourceID: "source:pv:01", SourceEpochID: "source-epoch:pv:01", DriverGeneration: "1", NativeResource: catalogEvidence(2)[0], State: semreg.BindingCurrent, Revision: "1"}}
	driver.IdentityLinkUpserts = []semreg.IdentityLink{{AssetID: "asset:pv:01", BindingID: "binding:pv:01", State: semreg.LinkQualified, Basis: catalogEvidence(3), Revision: "1", Resource: &resource}}
	driver.ResourceUpserts = []semreg.Resource{catalogInput("resource:dc-input:01", "1"), catalogInput("resource:dc-input:02", "2")}
	driver.FactUpserts = []semreg.FactCandidate{
		catalogFact("candidate:dc-power:01", catalogPV, "pv.dc.power", "pv.dimension.input", "resource:dc-input:01", "15", 2, "unit.watt", "binding:pv:01"),
		catalogFact("candidate:frequency:01", catalogPV, "pv.ac.frequency", "pv.dimension.inverter", "resource:inverter:01", "5", 1, "unit.hertz", "binding:pv:01"),
	}
	driver.ServiceUpserts = []semreg.ServiceInstance{{
		InstanceID: "service:inverter:01", AssetID: "asset:pv:01", Definition: semreg.DefinitionRef{Pack: catalogPV, ID: "pv.service.inverter", Version: "1.0.0"},
		BindingID: "binding:pv:01", SourceEpochID: "source-epoch:pv:01", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1", Resource: &resource,
	}}
	snapshot := catalogApply(t, kernel, driver, "")
	if len(snapshot.Resources) != 4 || snapshot.Revisions.Structure != "2" {
		t.Fatalf("got %d resources at structure %q", len(snapshot.Resources), snapshot.Revisions.Structure)
	}
	return kernel
}

// The resource rules against the accepted element catalog and the accepted
// PV and thermal packs, with the identity vectors that need a listed scheme.
func TestResourcePublicationAgainstTheElementCatalog(t *testing.T) {
	t.Run("PV system, inverter, inputs, facts and service", func(t *testing.T) {
		pvCatalogKernel(t)
	})

	t.Run("G-NEG-RES-2 a resource dimension naming no resource", func(t *testing.T) {
		kernel := pvCatalogKernel(t)
		batch := catalogBatch("source:pv:01", "source-epoch:pv:01", "2", "2")
		batch.FactUpserts = []semreg.FactCandidate{catalogFact("candidate:dc-power:99", catalogPV, "pv.dc.power", "pv.dimension.input", "resource:dc-input:99", "15", 2, "unit.watt", "binding:pv:01")}
		catalogApply(t, kernel, batch, semreg.DanglingReference)
	})

	t.Run("a resource dimension naming a resource of another kind", func(t *testing.T) {
		kernel := pvCatalogKernel(t)
		batch := catalogBatch("source:pv:01", "source-epoch:pv:01", "2", "2")
		batch.FactUpserts = []semreg.FactCandidate{catalogFact("candidate:dc-power:02", catalogPV, "pv.dc.power", "pv.dimension.input", "resource:inverter:01", "15", 2, "unit.watt", "binding:pv:01")}
		catalogApply(t, kernel, batch, semreg.IdentityDrift)
	})

	t.Run("the PV pack validates the resource dimension under enforcement only", func(t *testing.T) {
		embedded, err := graph.Embedded()
		if err != nil {
			t.Fatal(err)
		}
		enforced, err := semreg.NewGraphRegistry(listedSchemes{embedded}, pv.New())
		if err != nil {
			t.Fatal(err)
		}
		plain, err := semreg.NewRegistry(pv.New())
		if err != nil {
			t.Fatal(err)
		}
		fact := catalogFact("candidate:dc-power:01", catalogPV, "pv.dc.power", "pv.dimension.input", "input 1", "15", 2, "unit.watt", "binding:pv:01")
		if got := semreg.ErrorIdentifier(enforced.ValidateFact(fact.Key, fact.Value)); got != semreg.InvalidValue {
			t.Fatalf("enforced registry accepted a non-identifier resource dimension: %s", got)
		}
		if err := plain.ValidateFact(fact.Key, fact.Value); err != nil {
			t.Fatalf("registry without the catalog changed: %v", err)
		}
		fact = catalogFact("candidate:dc-power:01", catalogPV, "pv.dc.power", "pv.dimension.input", "resource:dc-input:01", "15", 2, "unit.watt", "binding:pv:01")
		if err := enforced.ValidateFact(fact.Key, fact.Value); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("G-POS-ID-2 then G-NEG-ID-8", func(t *testing.T) {
		kernel := pvCatalogKernel(t)
		current, _, _ := kernel.Current()
		var previous semreg.Resource
		for _, resource := range current.Resources {
			if resource.ResourceID == "resource:inverter:01" {
				previous = resource
			}
		}
		scheme := "synthetic.serial"
		key := catalogIdentityKey(t, scheme, "0001", "TOKEN-0001")
		upgraded := semreg.Resource{
			ResourceID: "resource:inverter:01", Kind: "resource.inverter",
			Anchor:     semreg.ResourceAnchor{Class: semreg.AnchorEquipment, Basis: semreg.AnchorNativeIdentity, Scheme: &scheme, Key: &key, Evidence: catalogEvidence(10)},
			Supersedes: &semreg.AnchorSupersession{Previous: previous.Anchor, Evidence: catalogEvidence(11)},
			State:      semreg.ResourceCurrent, Origin: catalogDriverOrigin(), Revision: "2",
		}
		batch := catalogBatch("source:pv:01", "source-epoch:pv:01", "2", "2")
		batch.ResourceUpserts = []semreg.Resource{upgraded}
		snapshot := catalogApply(t, kernel, batch, "")
		if snapshot.Revisions.Structure != "3" {
			t.Fatalf("structure %q after the supersession", snapshot.Revisions.Structure)
		}

		unlisted := "synthetic.unlisted"
		other := catalogIdentityKey(t, scheme, "0001", "TOKEN-0003")
		third := upgraded
		third.ResourceID, third.Supersedes, third.Revision = "resource:inverter:03", nil, "1"
		third.Anchor.Scheme, third.Anchor.Key = &unlisted, &other
		next := catalogBatch("source:pv:01", "source-epoch:pv:01", "3", "3")
		next.ResourceUpserts = []semreg.Resource{third}
		catalogApply(t, kernel, next, semreg.InvalidValue)
	})

	t.Run("G-NEG-RES-1 and PLN-7 with the thermal pack", func(t *testing.T) {
		kernel := catalogKernel(t, thermal.New())
		system := catalogBatch("source:configuration:01", "source-epoch:configuration:01", "1", "0")
		system.ResourceUpserts = []semreg.Resource{catalogOperatorResource("resource:thermal-system:01", "resource.thermal_system", semreg.AnchorFunction, "thermal-system-1", nil)}
		catalogApply(t, kernel, system, "")

		parent := semreg.ResourceID("resource:thermal-system:01")
		circuit := catalogBatch("source:configuration:01", "source-epoch:configuration:01", "2", "1")
		circuit.ResourceUpserts = []semreg.Resource{catalogOperatorResource("resource:thermal-circuit:01", "resource.thermal_circuit", semreg.AnchorFunction, "circuit-1", &parent)}
		catalogApply(t, kernel, circuit, semreg.MembershipMissing)

		// A fact of an unknown definition names no resource and is published.
		driver := catalogBatch("source:pv:01", "source-epoch:pv:01", "1", "1")
		driver.BindingUpserts = []semreg.NativeBinding{{BindingID: "binding:pv:01", AssetID: "asset:pv:01", SourceID: "source:pv:01", SourceEpochID: "source-epoch:pv:01", DriverGeneration: "1", NativeResource: catalogEvidence(2)[0], State: semreg.BindingCurrent, Revision: "1"}}
		driver.IdentityLinkUpserts = []semreg.IdentityLink{{AssetID: "asset:pv:01", BindingID: "binding:pv:01", State: semreg.LinkQualified, Basis: catalogEvidence(3), Revision: "1"}}
		driver.FactUpserts = []semreg.FactCandidate{catalogFact("candidate:air:01", catalogThermal, "thermal.measurement.air_temperature", "thermal.dimension.temperature", "room", "21", 0, "unit.celsius", "binding:pv:01")}
		catalogApply(t, kernel, driver, "")
	})
}
