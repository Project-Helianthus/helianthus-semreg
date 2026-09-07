package semreg_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	. "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

const canonicalPVFixtureDirectory = "fixtures/v1/compatibility/canonical-pv-v1"

type canonicalPVFactFixture struct {
	Legacy            string `json:"legacy"`
	Target            string `json:"target"`
	Dimension         string `json:"dimension"`
	DimensionValue    string `json:"dimension_value"`
	Coefficient       string `json:"coefficient"`
	Exponent10        int32  `json:"exponent10"`
	Unit              string `json:"unit"`
	Symbol            string `json:"symbol"`
	LegacyUnit        string `json:"legacy_unit"`
	LegacyCoefficient string `json:"legacy_coefficient"`
	LegacyScale       int32  `json:"legacy_scale"`
}

func canonicalPVPublicationBatch(asset AssetID, source SourceID, epoch SourceEpochID, generation, sequence, expected Uint64) PublicationBatch {
	return PublicationBatch{
		Contract: ContractKernelV1, BatchID: BatchID("batch:" + string(source) + ":" + string(epoch) + ":" + string(generation) + ":" + string(sequence)),
		AssetID: asset, SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, Sequence: sequence, ExpectedSemanticRevision: expected,
		ObservedAt:    TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"},
		SourceUpserts: []SourceDescriptor{}, SourceRetirements: []SourceEpochID{}, BindingUpserts: []NativeBinding{}, IdentityLinkUpserts: []IdentityLink{},
		FactUpserts: []FactCandidate{}, FactWithdrawals: []CandidateID{}, ServiceUpserts: []ServiceInstance{}, ServiceWithdrawals: []ServiceInstanceID{},
		CapabilityUpserts: []CapabilityInstance{}, CapabilityWithdrawals: []CapabilityInstanceID{}, GenerationFences: []GenerationFence{},
	}
}

func sealCanonicalPVBatch(t *testing.T, batch *PublicationBatch) {
	t.Helper()
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
}

func assertCanonicalPVRejectedUnchanged(t *testing.T, kernel *PublicationKernel, batch PublicationBatch, want ErrorID, monotonic MonotonicPoint) {
	t.Helper()
	before, beforeBytes, beforeOK := kernel.Current()
	_, _, err := kernel.Apply(batch, monotonic)
	if got := ErrorIdentifier(err); got != want {
		t.Fatalf("rejection=%s want=%s err=%v", got, want, err)
	}
	after, afterBytes, afterOK := kernel.Current()
	if beforeOK != afterOK || before.SnapshotID != after.SnapshotID || !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("rejection changed canonical snapshot")
	}
}

type canonicalPVCounterFixture struct {
	ID                     string  `json:"id"`
	Current                string  `json:"current"`
	Event                  string  `json:"event"`
	Modulus                string  `json:"modulus"`
	Want                   string  `json:"want"`
	WantError              ErrorID `json:"want_error"`
	Previous               *string `json:"previous"`
	BoundaryVerified       bool    `json:"boundary_verified"`
	EvidenceDigest         string  `json:"evidence_digest"`
	ExpectedEvidenceDigest string  `json:"expected_evidence_digest"`
}

type canonicalPVFixture struct {
	CorpusKind string `json:"corpus_kind"`
	Clock      struct {
		ReceiptNS   string `json:"receipt_ns"`
		FreshForNS  string `json:"fresh_for_ns"`
		RetainForNS string `json:"retain_for_ns"`
	} `json:"clock"`
	Provenance struct {
		Protocol          string `json:"protocol"`
		ProfileID         string `json:"profile_id"`
		ProfileVersion    string `json:"profile_version"`
		Validity          string `json:"validity"`
		RegistryDigest    string `json:"registry_digest"`
		ObservationDigest string `json:"observation_digest"`
		ShadowDigest      string `json:"shadow_digest"`
		EvidenceDigest    string `json:"evidence_digest"`
	} `json:"provenance"`
	Facts            []canonicalPVFactFixture    `json:"facts"`
	Counters         []canonicalPVCounterFixture `json:"counters"`
	CounterNegatives []canonicalPVCounterFixture `json:"counter_negatives"`
}

type canonicalPVDispositionFixture struct {
	Legacy   string `json:"legacy"`
	Target   string `json:"target"`
	Outcome  string `json:"outcome"`
	Loss     string `json:"loss"`
	Rollback string `json:"rollback"`
}

type canonicalPVDispositionDocument struct {
	Dispositions []canonicalPVDispositionFixture `json:"dispositions"`
}

type canonicalPVManifest struct {
	Status                         string `json:"status"`
	GatewayGoldenRole              string `json:"gateway_golden_role"`
	ProducerRequestedOutputWitness struct {
		Repository         string   `json:"repository"`
		Commit             string   `json:"commit"`
		ProducerPath       string   `json:"producer_path"`
		ExercisePath       string   `json:"exercise_path"`
		RequestedNativeIDs []string `json:"requested_native_ids"`
	} `json:"producer_requested_output_witness"`
	Sources []struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
		Path       string `json:"path"`
		BlobSHA1   string `json:"blob_sha1"`
		SHA256     string `json:"sha256"`
	} `json:"sources"`
}

func canonicalPVFixturePath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate canonical PV comparator fixture")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", canonicalPVFixtureDirectory, name)
}

func loadCanonicalPVFixture(t *testing.T) canonicalPVFixture {
	t.Helper()
	raw, err := os.ReadFile(canonicalPVFixturePath(t, "comparator-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture canonicalPVFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.CorpusKind != "synthetic_complete_mapper_witness" || len(fixture.Facts) != 11 || len(fixture.Counters) != 6 || len(fixture.CounterNegatives) != 3 || fixture.Clock.FreshForNS != "30000000000" || fixture.Clock.RetainForNS != "300000000000" {
		t.Fatalf("unexpected canonical PV fixture: facts=%d counters=%d negatives=%d clock=%+v", len(fixture.Facts), len(fixture.Counters), len(fixture.CounterNegatives), fixture.Clock)
	}
	return fixture
}

func loadCanonicalPVDispositions(t *testing.T) []canonicalPVDispositionFixture {
	t.Helper()
	raw, err := os.ReadFile(canonicalPVFixturePath(t, "dispositions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document canonicalPVDispositionDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Dispositions) != 14 {
		t.Fatalf("want 14 requested outputs, got %d", len(document.Dispositions))
	}
	return document.Dispositions
}

func canonicalPVEvidence(digest string) EvidenceRef {
	return EvidenceRef{Owner: "helianthus.compatibility", Kind: "canonical_pv.fixture", Digest: Digest(digest), Contract: "helianthus.canonical-pv/v1", Access: EvidenceAccessPublic, Redaction: RedactionNone}
}

func canonicalPVTime(ns string) TimePoint {
	return TimePoint{UnixNanoseconds: Int64(ns), ClockID: "clock.utc", UncertaintyNS: "0"}
}

func canonicalPVCandidate(f canonicalPVFactFixture, index int, source SourceID, epoch SourceEpochID, binding NativeBindingID, generation Uint64, fixture canonicalPVFixture) FactCandidate {
	dimension := f.DimensionValue
	value := Value{Kind: ValueQuantity, Quantity: &Quantity{Number: Decimal{Coefficient: f.Coefficient, Exponent10: f.Exponent10}, Unit: DefinitionID(f.Unit)}}
	if f.Symbol != "" {
		value = Value{Kind: ValueSymbol, Symbol: &Symbol{Namespace: DefinitionID(f.Target), Token: f.Symbol, Known: true}}
	}
	return FactCandidate{
		CandidateID:     CandidateID("candidate:canonical-pv:" + string(rune('a'+index))),
		Key:             FactKey{PackID: "helianthus.pack.pv", PackVersion: "1.0.0", FactID: DefinitionID(f.Target), Dimensions: []Dimension{{ID: DefinitionID(f.Dimension), Value: Value{Kind: ValueText, Text: &dimension}}}},
		Value:           &value,
		Quality:         Quality{Assertion: AssertionObserved, Qualification: QualificationCandidate, Promotion: PromotionUnpromoted, Validity: ValidityGood, Availability: AvailabilityAvailable, Freshness: FreshnessFresh, Reasons: []DefinitionID{}},
		Times:           Times{ReceivedAt: canonicalPVTime(fixture.Clock.ReceiptNS), ReceiptMonotonic: MonotonicPoint{ClockEpochID: "clock-epoch:canonical-pv", Nanoseconds: Uint64(fixture.Clock.ReceiptNS)}, EvaluatedAt: canonicalPVTime(fixture.Clock.ReceiptNS), EvaluateMonotonic: MonotonicPoint{ClockEpochID: "clock-epoch:canonical-pv", Nanoseconds: Uint64(fixture.Clock.ReceiptNS)}},
		FreshnessPolicy: FreshnessPolicy{PolicyID: "pv.telemetry.fast", Version: "1.0.0", FreshForNS: Uint64(fixture.Clock.FreshForNS), RetainForNS: Uint64(fixture.Clock.RetainForNS), MaxWallUncertaintyNS: "0"},
		BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
		Origin:   OriginRef{OriginID: OriginID("origin:canonical-pv:" + string(rune('a'+index))), Kind: OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []EvidenceRef{canonicalPVEvidence(fixture.Provenance.ObservationDigest)}},
		Evidence: []EvidenceRef{canonicalPVEvidence(fixture.Provenance.EvidenceDigest)}, Revision: "1",
	}
}

func canonicalPVBatch(t *testing.T, fixture canonicalPVFixture, sequence, expected Uint64) PublicationBatch {
	t.Helper()
	const asset AssetID = "asset:canonical-pv:fixture"
	const source SourceID = "source:sunspec:fixture"
	const epoch SourceEpochID = "source-epoch:sunspec:fixture"
	const binding NativeBindingID = "binding:sunspec:fixture"
	const generation Uint64 = "1"
	batch := canonicalPVPublicationBatch(asset, source, epoch, generation, sequence, expected)
	registry := canonicalPVEvidence(fixture.Provenance.RegistryDigest)
	batch.SourceUpserts = []SourceDescriptor{{SourceID: source, SourceEpochID: epoch, ProtocolID: DefinitionID(fixture.Provenance.Protocol), ProfileID: DefinitionID(fixture.Provenance.ProfileID), ProfileVersion: VersionLabel(fixture.Provenance.ProfileVersion), RegistryEvidence: registry, StartedAt: canonicalPVTime(fixture.Clock.ReceiptNS), State: SourceCurrent, Revision: "1"}}
	batch.BindingUpserts = []NativeBinding{{BindingID: binding, AssetID: asset, SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, NativeResource: canonicalPVEvidence(fixture.Provenance.ShadowDigest), State: BindingCurrent, Revision: "1"}}
	batch.IdentityLinkUpserts = []IdentityLink{{AssetID: asset, BindingID: binding, State: LinkQualified, Basis: []EvidenceRef{canonicalPVEvidence(fixture.Provenance.EvidenceDigest)}, Revision: "1"}}
	batch.FactUpserts = make([]FactCandidate, 0, len(fixture.Facts))
	for index, fact := range fixture.Facts {
		candidate := canonicalPVCandidate(fact, index, source, epoch, binding, generation, fixture)
		if err := pv.New().ValidateFact(candidate.Key, candidate.Value); err != nil {
			t.Fatalf("fixture fact %s is not PV-valid: %v", fact.Legacy, err)
		}
		batch.FactUpserts = append(batch.FactUpserts, candidate)
	}
	sort.Slice(batch.FactUpserts, func(i, j int) bool { return batch.FactUpserts[i].CandidateID < batch.FactUpserts[j].CandidateID })
	sealCanonicalPVBatch(t, &batch)
	return batch
}

func canonicalPVFixtureFactKeys(fixture canonicalPVFixture) map[string]FactKey {
	keys := make(map[string]FactKey, len(fixture.Facts))
	for _, fact := range fixture.Facts {
		dimension := fact.DimensionValue
		keys[fact.Legacy] = FactKey{PackID: "helianthus.pack.pv", PackVersion: "1.0.0", FactID: DefinitionID(fact.Target), Dimensions: []Dimension{{ID: DefinitionID(fact.Dimension), Value: Value{Kind: ValueText, Text: &dimension}}}}
	}
	return keys
}

func canonicalPVMapperFields() map[string]bool {
	return map[string]bool{
		"inverter.ac.current.phase_a": true, "inverter.ac.current.phase_b": true, "inverter.ac.current.phase_c": true,
		"inverter.ac.voltage.phase_a": true, "inverter.ac.voltage.phase_b": true, "inverter.ac.voltage.phase_c": true,
		"inverter.ac.power.active": true, "inverter.ac.frequency": true, "inverter.ac.energy_lifetime": true,
		"inverter.temperature.cabinet": true, "inverter.operating_state": true,
		"inverter.ac.current.total": true, "inverter.events.1": true, "inverter.events.2": true,
	}
}

// canonicalPVProducerRequestedIDs freezes the complete native requested-output
// witness emitted by the mapper pinned in manifest.json. SemReg deliberately
// does not import the gateway: the self-contained compatibility fixture keeps
// the pinned public witness available to this test instead.
var canonicalPVProducerRequestedIDs = map[string]struct{}{
	"inverter.ac.current.phase_a": {}, "inverter.ac.current.phase_b": {}, "inverter.ac.current.phase_c": {},
	"inverter.ac.current.total": {}, "inverter.ac.frequency": {}, "inverter.ac.power.active": {},
	"inverter.ac.voltage.phase_a": {}, "inverter.ac.voltage.phase_b": {}, "inverter.ac.voltage.phase_c": {},
	"inverter.ac.energy_lifetime": {}, "inverter.events.1": {}, "inverter.events.2": {},
	"inverter.operating_state": {}, "inverter.temperature.cabinet": {},
}

type canonicalPVWithheldWitness struct {
	Target   string
	Loss     string
	Rollback string
}

var canonicalPVWithheldProducerWitness = map[string]canonicalPVWithheldWitness{
	"inverter.ac.current.total": {Target: "legacy.withheld.aggregate_current", Loss: "topology: aggregate_current_not_synthesized", Rollback: "select_legacy_output"},
	"inverter.events.1":         {Target: "legacy.withheld.event_1", Loss: "semantics: event_meaning_unknown", Rollback: "select_legacy_output"},
	"inverter.events.2":         {Target: "legacy.withheld.event_2", Loss: "semantics: event_meaning_unknown", Rollback: "select_legacy_output"},
}

func canonicalPVMatchesProducerRequestedWitness(rows []canonicalPVDispositionFixture) bool {
	ids := make([]string, 0, len(canonicalPVProducerRequestedIDs))
	for id := range canonicalPVProducerRequestedIDs {
		ids = append(ids, id)
	}
	return canonicalPVMatchesRequestedIDSet(rows, ids)
}

func canonicalPVMatchesRequestedIDSet(rows []canonicalPVDispositionFixture, ids []string) bool {
	if len(rows) != len(ids) {
		return false
	}
	expected := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := expected[id]; duplicate {
			return false
		}
		expected[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, wanted := expected[row.Legacy]; !wanted {
			return false
		}
		if _, duplicate := seen[row.Legacy]; duplicate {
			return false
		}
		seen[row.Legacy] = struct{}{}
	}
	return len(seen) == len(expected)
}

func canonicalPVMatchesPinnedProducerRequestedIDs(ids []string) bool {
	if len(ids) != len(canonicalPVProducerRequestedIDs) {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, wanted := canonicalPVProducerRequestedIDs[id]; !wanted {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return len(seen) == len(canonicalPVProducerRequestedIDs)
}

func canonicalPVMatchesWithheldProducerPolicy(rows []canonicalPVDispositionFixture) bool {
	seen := make(map[string]struct{}, len(canonicalPVWithheldProducerWitness))
	for _, row := range rows {
		witness, withheld := canonicalPVWithheldProducerWitness[row.Legacy]
		if !withheld {
			continue
		}
		if row.Outcome != string(projection.ProjectionWithheld) || row.Target != witness.Target || row.Loss != witness.Loss || row.Rollback != witness.Rollback {
			return false
		}
		seen[row.Legacy] = struct{}{}
	}
	return len(seen) == len(canonicalPVWithheldProducerWitness)
}

func TestCanonicalPVDispositionFrozenProducerWitness(t *testing.T) {
	rows := loadCanonicalPVDispositions(t)
	if !canonicalPVMatchesProducerRequestedWitness(rows) {
		t.Fatal("dispositions do not exactly cover the pinned gateway producer requested-output witness")
	}
	if !canonicalPVMatchesWithheldProducerPolicy(rows) {
		t.Fatal("withheld producer outputs do not preserve their fail-closed loss and rollback treatment")
	}

	clone := func() []canonicalPVDispositionFixture { return append([]canonicalPVDispositionFixture(nil), rows...) }
	mutations := []struct {
		name string
		rows []canonicalPVDispositionFixture
	}{
		{name: "old_synthetic_substitution", rows: func() []canonicalPVDispositionFixture {
			mutated := clone()
			mutated[11].Legacy = "inverter.ac.power.apparent"
			return mutated
		}()},
		{name: "missing_identity", rows: rows[:len(rows)-1]},
		{name: "duplicate_identity", rows: func() []canonicalPVDispositionFixture {
			mutated := clone()
			mutated[11].Legacy = mutated[0].Legacy
			return mutated
		}()},
		{name: "extra_identity", rows: append(clone(), canonicalPVDispositionFixture{Legacy: "inverter.ac.power.apparent"})},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			if canonicalPVMatchesProducerRequestedWitness(mutation.rows) {
				t.Fatal("producer witness accepted a changed requested-output identity set")
			}
		})
	}
	policyMutation := clone()
	policyMutation[11].Outcome = string(projection.ProjectionExact)
	if canonicalPVMatchesWithheldProducerPolicy(policyMutation) {
		t.Fatal("withheld producer policy accepted a synthesized aggregate current")
	}
}

func canonicalPVRational(coefficient string, exponent int32) *big.Rat {
	rational := new(big.Rat)
	if _, ok := rational.SetString(coefficient); !ok {
		panic("fixture decimal is invalid")
	}
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(absCanonicalPVExponent(exponent))), nil)
	if exponent < 0 {
		return rational.Quo(rational, new(big.Rat).SetInt(factor))
	}
	return rational.Mul(rational, new(big.Rat).SetInt(factor))
}

func absCanonicalPVExponent(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}

// canonicalPVDonorCounterRules mirrors the public, pinned pv/counter.go input
// rules. It stays test-only because SemReg intentionally has no legacy counter
// continuity runtime type. The expected digest is the comparator's additional
// equality guard: valid but substituted donor evidence must still fail closed.
func canonicalPVDonorCounterRules(caseValue canonicalPVCounterFixture) (string, EvidenceRef, ErrorID) {
	emptyEvidence := caseValue.EvidenceDigest == ""
	if caseValue.Previous == nil {
		if caseValue.Event != "none" || caseValue.Modulus != "" || caseValue.BoundaryVerified || !emptyEvidence {
			return "discontinuity", EvidenceRef{}, InvalidEvidence
		}
		return "baseline", EvidenceRef{}, ""
	}
	previous, previousOK := new(big.Int).SetString(*caseValue.Previous, 10)
	current, currentOK := new(big.Int).SetString(caseValue.Current, 10)
	if !previousOK || !currentOK {
		return "discontinuity", EvidenceRef{}, InvalidValue
	}
	if current.Cmp(previous) >= 0 && caseValue.Event == "none" && caseValue.Modulus == "" && !caseValue.BoundaryVerified && emptyEvidence {
		return "contiguous", EvidenceRef{}, ""
	}
	if caseValue.Event == "none" {
		if caseValue.Modulus != "" || caseValue.BoundaryVerified || !emptyEvidence {
			return "discontinuity", EvidenceRef{}, InvalidEvidence
		}
		return "discontinuity", EvidenceRef{}, ""
	}
	evidence := canonicalPVEvidence(caseValue.EvidenceDigest)
	if err := evidence.Validate(); err != nil {
		return "discontinuity", EvidenceRef{}, InvalidEvidence
	}
	if caseValue.ExpectedEvidenceDigest == "" || caseValue.EvidenceDigest != caseValue.ExpectedEvidenceDigest {
		return "discontinuity", EvidenceRef{}, DigestMismatch
	}
	switch caseValue.Event {
	case "reset":
		if caseValue.Modulus != "" || caseValue.BoundaryVerified {
			return "discontinuity", EvidenceRef{}, InvalidEvidence
		}
		return "reset", evidence, ""
	case "rollover":
		modulus, modulusOK := new(big.Int).SetString(caseValue.Modulus, 10)
		if !modulusOK || modulus.Sign() <= 0 || current.Cmp(previous) >= 0 || !caseValue.BoundaryVerified || previous.Cmp(modulus) >= 0 {
			return "discontinuity", EvidenceRef{}, InvalidEvidence
		}
		return "rollover", evidence, ""
	default:
		return "discontinuity", EvidenceRef{}, InvalidEvidence
	}
}

func canonicalPVCarryCounterEvidence(candidate FactCandidate, evidence EvidenceRef) FactCandidate {
	candidate.Evidence = append(append([]EvidenceRef(nil), candidate.Evidence...), evidence)
	candidate.Origin.Evidence = append(append([]EvidenceRef(nil), candidate.Origin.Evidence...), evidence)
	sort.Slice(candidate.Evidence, func(i, j int) bool { return candidate.Evidence[i].Digest < candidate.Evidence[j].Digest })
	sort.Slice(candidate.Origin.Evidence, func(i, j int) bool { return candidate.Origin.Evidence[i].Digest < candidate.Origin.Evidence[j].Digest })
	return candidate
}

func canonicalPVHasEvidence(evidence []EvidenceRef, digest Digest) bool {
	for _, item := range evidence {
		if item.Digest == digest {
			return true
		}
	}
	return false
}

func TestCanonicalPVV1ComparatorFixturesArePinned(t *testing.T) {
	golden, err := os.ReadFile(canonicalPVFixturePath(t, "legacy-gateway-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(golden)
	if got := hex.EncodeToString(sum[:]); got != "731127d1caa09d2a293e543e53b885eb59e328d3b9e87987189786846ec805fa" {
		t.Fatalf("gateway fixture SHA-256 = %s", got)
	}
	var gateway struct {
		Data struct {
			ContractID         string `json:"contract_id"`
			AssetRef           string `json:"asset_ref"`
			EvaluatedMonotonic int64  `json:"evaluated_monotonic_ns"`
			Capability         []struct {
				Outcome string `json:"outcome"`
			} `json:"capabilities"`
			RequestedOutputs []json.RawMessage `json:"requested_outputs"`
			ProjectionReport []json.RawMessage `json:"projection_report"`
			SourceProvenance struct {
				Registry       string `json:"source_registry_ref"`
				Observation    string `json:"source_observation_ref"`
				Shadow         string `json:"source_shadow_ref"`
				Evidence       string `json:"evidence_ref"`
				ProfileID      string `json:"source_profile_id"`
				ProfileVersion string `json:"source_profile_version"`
				Protocol       string `json:"source_protocol"`
				Validity       string `json:"source_validity"`
			} `json:"source_provenance"`
			Facts []struct {
				FactID       string `json:"fact_id"`
				Unit         string `json:"unit"`
				Quality      string `json:"quality"`
				Availability string `json:"availability"`
				Freshness    string `json:"freshness"`
				OriginRef    string `json:"origin_ref"`
				Value        struct {
					Coefficient string `json:"coefficient"`
					Scale       int32  `json:"scale"`
				} `json:"value"`
				Temporal struct {
					Receipt     int64 `json:"receipt_monotonic_ns"`
					FreshUntil  int64 `json:"fresh_until_monotonic_ns"`
					RetainUntil int64 `json:"retain_until_monotonic_ns"`
				} `json:"temporal"`
			} `json:"facts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(golden, &gateway); err != nil || gateway.Data.ContractID != "helianthus.canonical-pv/v1" || gateway.Data.AssetRef != "pv-asset-fixture" || len(gateway.Data.Facts) != 1 || len(gateway.Data.Capability) != 1 || gateway.Data.Capability[0].Outcome != "NOT_SATISFIED" || len(gateway.Data.RequestedOutputs) != 1 || len(gateway.Data.ProjectionReport) != 1 {
		t.Fatalf("immutable gateway donor drift: err=%v data=%+v", err, gateway.Data)
	}
	active := gateway.Data.Facts[0]
	if active.FactID != "pv.ac.power.active" || active.Unit != "W" || active.Value.Coefficient != "7310" || active.Value.Scale != 0 || active.Quality != "GOOD" || active.Availability != "AVAILABLE" || active.Freshness != "FRESH" || active.OriginRef != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || active.Temporal.Receipt != 100 || active.Temporal.FreshUntil != 30000000100 || active.Temporal.RetainUntil != 300000000100 || gateway.Data.EvaluatedMonotonic != 100 {
		t.Fatalf("gateway golden active-power/time fixture drifted: %+v", active)
	}
	if gateway.Data.SourceProvenance.Registry != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || gateway.Data.SourceProvenance.Observation != active.OriginRef || gateway.Data.SourceProvenance.Shadow != "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" || gateway.Data.SourceProvenance.Evidence != "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" || gateway.Data.SourceProvenance.Protocol != "sunspec_modbus" || gateway.Data.SourceProvenance.ProfileID != "sunspec.inverter.three_phase.monitoring@1.0.0" || gateway.Data.SourceProvenance.ProfileVersion != "1.0.0" || gateway.Data.SourceProvenance.Validity != "terminal_verified" {
		t.Fatalf("gateway golden provenance drifted: %+v", gateway.Data.SourceProvenance)
	}
	baseline := loadCanonicalPVFixture(t)
	if baseline.Clock.ReceiptNS != "100" || baseline.Provenance.Protocol != gateway.Data.SourceProvenance.Protocol || baseline.Provenance.ProfileID+"@"+baseline.Provenance.ProfileVersion != gateway.Data.SourceProvenance.ProfileID || baseline.Provenance.RegistryDigest != gateway.Data.SourceProvenance.Registry || baseline.Provenance.ObservationDigest != gateway.Data.SourceProvenance.Observation || baseline.Provenance.ShadowDigest != gateway.Data.SourceProvenance.Shadow || baseline.Provenance.EvidenceDigest != gateway.Data.SourceProvenance.Evidence || canonicalPVRational(baseline.Facts[0].Coefficient, baseline.Facts[0].Exponent10).Cmp(big.NewRat(7310, 1)) != 0 {
		t.Fatal("synthetic SemReg baseline no longer matches the single-fact gateway golden witness")
	}
	manifest, err := os.ReadFile(canonicalPVFixturePath(t, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pinned canonicalPVManifest
	if err := json.Unmarshal(manifest, &pinned); err != nil || pinned.Status != "test_only_non_runtime" || pinned.GatewayGoldenRole != "byte_exact_single_fact_gateway_output" || len(pinned.Sources) != 6 {
		t.Fatalf("invalid comparator donor manifest: err=%v manifest=%+v", err, pinned)
	}
	witness := pinned.ProducerRequestedOutputWitness
	if witness.Repository != "Project-Helianthus/helianthus-ebusgateway" || witness.Commit != "f5cd9c51c60bdf422e8fc1b5690fbde52a393be3" || witness.ProducerPath != "internal/modbusadapter/canonical_pv.go" || witness.ExercisePath != "internal/modbusadapter/canonical_pv_red_test.go" || !canonicalPVMatchesPinnedProducerRequestedIDs(witness.RequestedNativeIDs) {
		t.Fatalf("invalid pinned gateway requested-output witness: %+v", witness)
	}
	if !canonicalPVMatchesRequestedIDSet(loadCanonicalPVDispositions(t), witness.RequestedNativeIDs) {
		t.Fatal("dispositions do not exactly match the pinned gateway requested-output witness")
	}
	wantGateway := map[string]string{
		"internal/modbusadapter/canonical_pv.go":          "4171468c974468a82b645077f2b54359213d4841/608aa647e22e63009a9e52bf427d3e14dcdd685aa5286af85fbf462e1a3688fd",
		"internal/modbusadapter/canonical_pv_red_test.go": "53c364b9c4a6dc94f4fa7e32b06567753303cde3/35217729237360b8fb7ea1a3764409e420ea9f35aa39a78a099cffc563dbbfcb",
	}
	for _, source := range pinned.Sources {
		if want, ok := wantGateway[source.Path]; ok && source.Repository == "Project-Helianthus/helianthus-ebusgateway" && source.Commit == "f5cd9c51c60bdf422e8fc1b5690fbde52a393be3" && source.BlobSHA1+"/"+source.SHA256 == want {
			delete(wantGateway, source.Path)
		}
	}
	if len(wantGateway) != 0 {
		t.Fatal("missing immutable donor provenance or test-only boundary")
	}
}

func TestCanonicalPVV1ComparatorPipelineAndAccounting(t *testing.T) {
	fixture := loadCanonicalPVFixture(t)
	dispositionRows := loadCanonicalPVDispositions(t)
	batch := canonicalPVBatch(t, fixture, "1", "0")
	kernel, err := NewPublicationKernel("asset:canonical-pv:fixture", pv.New())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, canonical, err := kernel.Apply(batch, MonotonicPoint{ClockEpochID: "clock-epoch:canonical-pv", Nanoseconds: Uint64(fixture.Clock.ReceiptNS)})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Facts) != 11 || len(snapshot.Services) != 0 || len(snapshot.Capabilities) != 0 || len(canonical) == 0 {
		t.Fatalf("migration comparator manufactured runtime state: facts=%d services=%d capabilities=%d", len(snapshot.Facts), len(snapshot.Services), len(snapshot.Capabilities))
	}
	if len(snapshot.Sources) != 1 || snapshot.Sources[0].ProtocolID != DefinitionID(fixture.Provenance.Protocol) || snapshot.Sources[0].ProfileID != DefinitionID(fixture.Provenance.ProfileID) || snapshot.Sources[0].ProfileVersion != VersionLabel(fixture.Provenance.ProfileVersion) || snapshot.Sources[0].RegistryEvidence.Digest != Digest(fixture.Provenance.RegistryDigest) {
		t.Fatalf("source protocol/profile/registry provenance drifted: %+v", snapshot.Sources)
	}
	if candidate := snapshot.Facts[0].Candidates[0]; candidate.SourceEpochID == nil || candidate.DriverGeneration == nil || candidate.Evidence[0].Digest != Digest(fixture.Provenance.EvidenceDigest) || candidate.Quality.Qualification != QualificationCandidate || candidate.Quality.Promotion != PromotionUnpromoted {
		t.Fatalf("candidate promoted or lost source lineage: %+v", candidate)
	}
	if encoded, err := CanonicalJSON(snapshot); err != nil || !bytes.Equal(encoded, canonical) {
		t.Fatalf("snapshot canonical bytes drift: err=%v", err)
	}
	if decoded, err := Decode[Snapshot](canonical); err != nil || decoded.SnapshotID != snapshot.SnapshotID {
		t.Fatalf("snapshot canonical bytes are not decodable: %v", err)
	}

	for _, check := range []struct {
		name, ns, freshness string
	}{{"fresh", "100", "fresh"}, {"stale", "30000000100", "stale"}, {"expired", "300000000100", "expired"}} {
		view, err := EvaluateSnapshot(snapshot, EvaluationContext{EvaluatedAt: canonicalPVTime(check.ns), EvaluateMonotonic: MonotonicPoint{ClockEpochID: "clock-epoch:canonical-pv", Nanoseconds: Uint64(check.ns)}})
		if err != nil || len(view.Facts) != 11 {
			t.Fatalf("%s evaluation failed: %v", check.name, err)
		}
		for _, fact := range view.Facts {
			if string(fact.Freshness) != check.freshness {
				t.Fatalf("%s freshness=%s for %s", check.name, fact.Freshness, fact.CandidateID)
			}
		}
	}

	keys := canonicalPVFixtureFactKeys(fixture)
	mapperFields := canonicalPVMapperFields()
	if len(keys) != 11 {
		t.Fatalf("mapped fixture corpus has %d fields", len(keys))
	}
	for legacy := range keys {
		if !mapperFields[legacy] {
			t.Fatalf("mapped fixture invented legacy field %q", legacy)
		}
	}
	requested := make([]projection.RequestedItem, 0, len(dispositionRows))
	dispositions := make([]projection.ProjectionDisposition, 0, len(dispositionRows))
	mapped, withheld := 0, 0
	for _, row := range dispositionRows {
		if !mapperFields[row.Legacy] {
			t.Fatalf("disposition invented legacy field %q", row.Legacy)
		}
		delete(mapperFields, row.Legacy)
		outcome := projection.ProjectionOutcome(row.Outcome)
		if outcome != projection.ProjectionExact && (row.Loss == "" || row.Rollback == "") {
			t.Fatalf("non-exact disposition %s omitted loss or rollback", row.Legacy)
		}
		requested = append(requested, projection.RequestedItem{Kind: projection.ItemFact, ItemID: DefinitionID(row.Target)})
		disposition := projection.ProjectionDisposition{Kind: projection.ItemFact, ItemID: DefinitionID(row.Target), Outcome: outcome, SourceKeys: []FactKey{}, Loss: []projection.LossDetail{}}
		if key, ok := keys[row.Legacy]; ok {
			disposition.SourceKeys = []FactKey{key}
		}
		switch outcome {
		case projection.ProjectionExact:
			mapped++
		case projection.ProjectionTransformed:
			mapped++
			kind := projection.LossUnit
			if strings.HasPrefix(row.Loss, "symbol:") {
				kind = projection.LossSymbol
			} else if strings.HasPrefix(row.Loss, "provenance:") {
				kind = projection.LossProvenance
			}
			disposition.Loss = []projection.LossDetail{{Kind: kind, SourceItems: []DefinitionID{DefinitionID(row.Legacy)}, Description: row.Loss, Reversible: kind == projection.LossUnit}}
		case projection.ProjectionWithheld:
			withheld++
			disposition.Loss = []projection.LossDetail{{Kind: projection.LossProvenance, SourceItems: []DefinitionID{DefinitionID(row.Legacy)}, Description: row.Loss, Reversible: false}}
			reason := DefinitionID("compatibility.legacy_path_required")
			disposition.Reason = &reason
		default:
			t.Fatalf("unexpected fixture outcome %q", outcome)
		}
		dispositions = append(dispositions, disposition)
	}
	if len(mapperFields) != 0 || mapped != 11 || withheld != 3 {
		t.Fatalf("requested output accounting mapped=%d withheld=%d", mapped, withheld)
	}
	exact, transformed := 0, 0
	for _, disposition := range dispositions {
		if disposition.Outcome == projection.ProjectionExact {
			exact++
		}
		if disposition.Outcome == projection.ProjectionTransformed {
			transformed++
		}
	}
	if exact != 8 || transformed != 3 {
		t.Fatalf("mapper disposition counts exact=%d transformed=%d", exact, transformed)
	}
	sort.Slice(requested, func(i, j int) bool { return requested[i].ItemID < requested[j].ItemID })
	sort.Slice(dispositions, func(i, j int) bool { return dispositions[i].ItemID < dispositions[j].ItemID })
	report, err := projection.Project(snapshot, projection.ProjectionManifest{TargetID: "target:canonical-pv-comparator", TargetVersion: "1.0.0", KernelVersion: ContractKernelV1, PackVersions: []PackRef{{ID: "helianthus.pack.pv", Version: "1.0.0"}}, MappingRevision: "1"}, requested, dispositions, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Requested) != 14 || len(report.Dispositions) != 14 {
		t.Fatalf("projection omitted requested outputs: %+v", report)
	}
	if _, err := CanonicalJSON(report); err != nil {
		t.Fatalf("projection did not produce canonical JSON: %v", err)
	}

	energyCandidate := batch.FactUpserts[2]
	for _, counter := range fixture.Counters {
		state, evidence, failure := canonicalPVDonorCounterRules(counter)
		if failure != "" || state != counter.Want {
			t.Fatalf("counter %s state=%s failure=%s want=%s", counter.ID, state, failure, counter.Want)
		}
		if counter.EvidenceDigest == "" {
			if evidence.Digest != "" {
				t.Fatalf("counter %s manufactured evidence", counter.ID)
			}
			continue
		}
		if evidence.Digest != Digest(counter.ExpectedEvidenceDigest) || evidence.Validate() != nil {
			t.Fatalf("counter %s did not retain expected digest: %+v", counter.ID, evidence)
		}
		carried := canonicalPVCarryCounterEvidence(energyCandidate, evidence)
		raw, err := CanonicalJSON(carried)
		if err != nil {
			t.Fatalf("counter %s canonical provenance: %v", counter.ID, err)
		}
		decoded, err := DecodeFactCandidate(raw)
		if err != nil || !canonicalPVHasEvidence(decoded.Evidence, evidence.Digest) || !canonicalPVHasEvidence(decoded.Origin.Evidence, evidence.Digest) {
			t.Fatalf("counter %s lost provenance after canonical round-trip: %v", counter.ID, err)
		}
		reencoded, err := CanonicalJSON(decoded)
		if err != nil || !bytes.Equal(raw, reencoded) {
			t.Fatalf("counter %s digest bytes changed: %v", counter.ID, err)
		}
	}
	for _, counter := range fixture.CounterNegatives {
		state, evidence, failure := canonicalPVDonorCounterRules(counter)
		if failure != counter.WantError || state != "discontinuity" || evidence.Digest != "" {
			t.Fatalf("counter negative %s state=%s evidence=%+v failure=%s want=%s", counter.ID, state, evidence, failure, counter.WantError)
		}
	}
	energy := fixture.Facts[2]
	if canonicalPVRational(energy.LegacyCoefficient, energy.LegacyScale).Cmp(new(big.Rat).Mul(canonicalPVRational(energy.Coefficient, energy.Exponent10), big.NewRat(1000, 1))) != 0 {
		t.Fatal("Wh-to-kWh transform is not exact and reversible")
	}
}

func TestCanonicalPVV1ComparatorRejectsMutationAndReplays(t *testing.T) {
	fixture := loadCanonicalPVFixture(t)
	initial := canonicalPVBatch(t, fixture, "1", "0")
	kernel, err := NewPublicationKernel("asset:canonical-pv:fixture", pv.New())
	if err != nil {
		t.Fatal(err)
	}
	monotonic := MonotonicPoint{ClockEpochID: "clock-epoch:canonical-pv", Nanoseconds: Uint64(fixture.Clock.ReceiptNS)}
	first, firstBytes, err := kernel.Apply(initial, monotonic)
	if err != nil {
		t.Fatal(err)
	}
	for index, quality := range []Quality{
		{Assertion: AssertionObserved, Qualification: QualificationCandidate, Promotion: PromotionUnpromoted, Validity: ValidityGood, Availability: AvailabilityAvailable, Freshness: FreshnessFresh, Reasons: []DefinitionID{}},
		{Assertion: AssertionObserved, Qualification: QualificationCandidate, Promotion: PromotionUnpromoted, Validity: ValiditySuspect, Availability: AvailabilityAvailable, Freshness: FreshnessFresh, Reasons: []DefinitionID{}},
		{Assertion: AssertionObserved, Qualification: QualificationCandidate, Promotion: PromotionUnpromoted, Validity: ValidityBad, Availability: AvailabilityUnavailable, Freshness: FreshnessFresh, Reasons: []DefinitionID{}},
		{Assertion: AssertionObserved, Qualification: QualificationUnsupported, Promotion: PromotionUnpromoted, Validity: ValidityUnknown, Availability: AvailabilityUnavailable, Freshness: FreshnessUnknown, Reasons: []DefinitionID{}},
	} {
		candidate := initial.FactUpserts[0]
		candidate.Quality = quality
		err := candidate.Validate()
		if (index < 3 && err != nil) || (index == 3 && ErrorIdentifier(err) != InvalidValue) || candidate.Quality.Qualification == QualificationQualified || candidate.Quality.Promotion == PromotionPromoted {
			t.Fatalf("legacy quality/availability was promoted instead of fail-closed: quality=%+v err=%v", quality, err)
		}
	}
	unitDrift := canonicalPVBatch(t, fixture, "2", "1")
	unitDrift.FactUpserts = append([]FactCandidate(nil), unitDrift.FactUpserts...)
	unitValue := *unitDrift.FactUpserts[0].Value
	unitQuantity := *unitValue.Quantity
	unitQuantity.Unit = "unit.kilowatt_hour"
	unitValue.Quantity = &unitQuantity
	unitDrift.FactUpserts[0].Value = &unitValue
	assertCanonicalPVRejectedUnchanged(t, kernel, unitDrift, InvalidValue, monotonic)
	wrongDimension := canonicalPVBatch(t, fixture, "2", "1")
	wrongDimension.FactUpserts = append([]FactCandidate(nil), wrongDimension.FactUpserts...)
	wrongDimension.FactUpserts[0].Key.Dimensions = append([]Dimension(nil), wrongDimension.FactUpserts[0].Key.Dimensions...)
	wrongDimension.FactUpserts[0].Key.Dimensions[0].ID = "pv.dimension.system"
	assertCanonicalPVRejectedUnchanged(t, kernel, wrongDimension, InvalidValue, monotonic)
	evidenceMutation := canonicalPVBatch(t, fixture, "2", "1")
	evidenceMutation.FactUpserts = append([]FactCandidate(nil), evidenceMutation.FactUpserts...)
	evidenceMutation.FactUpserts[0].Evidence = append([]EvidenceRef(nil), evidenceMutation.FactUpserts[0].Evidence...)
	evidenceMutation.FactUpserts[0].Evidence[0].Digest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	sealCanonicalPVBatch(t, &evidenceMutation)
	assertCanonicalPVRejectedUnchanged(t, kernel, evidenceMutation, RevisionConflict, monotonic)
	replay, replayBytes, err := kernel.Apply(initial, monotonic)
	if err != nil || replay.SnapshotID != first.SnapshotID || !bytes.Equal(replayBytes, firstBytes) {
		t.Fatalf("identical bytes were not idempotent: %v", err)
	}
	conflict := initial
	conflict.BatchID = "batch:canonical-pv:conflict"
	conflict.ObservedAt = canonicalPVTime("101")
	if digest, err := conflict.ComputedDigest(); err == nil {
		conflict.BatchDigest = digest
	} else {
		t.Fatal(err)
	}
	assertCanonicalPVRejectedUnchanged(t, kernel, conflict, SequenceConflict, monotonic)
	profileMutation := canonicalPVBatch(t, fixture, "2", "1")
	profileMutation.SourceUpserts[0].ProfileID = "sunspec.inverter.mutated"
	sealCanonicalPVBatch(t, &profileMutation)
	assertCanonicalPVRejectedUnchanged(t, kernel, profileMutation, StaleSourceEpoch, monotonic)
	sourceDigestMutation := canonicalPVBatch(t, fixture, "2", "1")
	sourceDigestMutation.SourceUpserts[0].RegistryEvidence.Digest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	sealCanonicalPVBatch(t, &sourceDigestMutation)
	assertCanonicalPVRejectedUnchanged(t, kernel, sourceDigestMutation, RevisionConflict, monotonic)

	partial := canonicalPVPublicationBatch("asset:canonical-pv:fixture", "source:sunspec:fixture", "source-epoch:sunspec:fixture", "1", "2", "1")
	updated := initial.FactUpserts[0]
	updated.Revision = "2"
	updated.Value = &Value{Kind: ValueQuantity, Quantity: &Quantity{Number: Decimal{Coefficient: "7311", Exponent10: 0}, Unit: "unit.watt"}}
	partial.FactUpserts = []FactCandidate{updated}
	sealCanonicalPVBatch(t, &partial)
	second, secondBytes, err := kernel.Apply(partial, monotonic)
	if err != nil || len(second.Facts) != 11 {
		t.Fatalf("partial retained update failed: %v", err)
	}
	bad := partial
	bad.BatchID = "batch:canonical-pv:bad"
	bad.FactUpserts = append([]FactCandidate(nil), partial.FactUpserts...)
	bad.FactUpserts[0].Key.Dimensions = append(bad.FactUpserts[0].Key.Dimensions, bad.FactUpserts[0].Key.Dimensions[0])
	assertCanonicalPVRejectedUnchanged(t, kernel, bad, DuplicateKey, monotonic)
	if _, current, ok := kernel.Current(); !ok || !bytes.Equal(current, secondBytes) {
		t.Fatal("invalid batch changed retained canonical bytes")
	}

	fresh, err := NewPublicationKernel("asset:canonical-pv:fixture", pv.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fresh.Apply(initial, monotonic); err != nil {
		t.Fatal(err)
	}
	_, rebuilt, err := fresh.Apply(partial, monotonic)
	if err != nil || !bytes.Equal(rebuilt, secondBytes) {
		t.Fatalf("ordered fixture replay was not deterministic: %v", err)
	}

	alias := projection.CompatibilityAlias{AliasContract: projection.ContractAliasV1, LegacyID: "pv-asset-fixture", AssetID: "asset:canonical-pv:fixture", ValidFrom: "1.0.0", Routable: true, Evidence: []EvidenceRef{canonicalPVEvidence(fixture.Provenance.EvidenceDigest)}}
	if got := ErrorIdentifier(alias.Validate()); got != AliasNotRoutable {
		t.Fatalf("legacy alias route result = %s", got)
	}
}
