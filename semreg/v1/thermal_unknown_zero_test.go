package semreg_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

// Contract sources: fixtures/v1/thermal-hvac-pack-vectors.json (thermal v1,
// pack 1.0.0), and the public kernel/projection v1 APIs. All observations are
// synthetic. Thermal status fields are symbols; the pack has no boolean fact.
func TestK14ZeroUnknownProjection(t *testing.T) {
	kernel, batch := k14Initial(t)
	snapshot, raw := k14Apply(t, kernel, batch)
	decoded, err := semreg.Decode[semreg.Snapshot](raw)
	if err != nil || !reflect.DeepEqual(snapshot, decoded) {
		t.Fatalf("snapshot round trip: %v", err)
	}
	zero := decoded.Facts[0].Candidates[0].Value
	if zero == nil || zero.Kind != semreg.ValueQuantity || zero.Quantity == nil || zero.Quantity.Number != (semreg.Decimal{Coefficient: "0", Exponent10: 0}) || zero.Quantity.Unit != "unit.celsius" {
		t.Fatalf("valid zero temperature lost: %+v", zero)
	}
	unknown := semreg.DefinitionID("thermal.measurement.humidity_relative")
	reason := semreg.DefinitionID("reason.value_unknown")
	requested := []projection.RequestedItem{{ItemID: decoded.Facts[0].Key.FactID, Kind: projection.ItemFact}, {ItemID: unknown, Kind: projection.ItemFact}}
	dispositions := []projection.ProjectionDisposition{
		{Kind: projection.ItemFact, ItemID: requested[0].ItemID, Outcome: projection.ProjectionExact, SourceKeys: []semreg.FactKey{decoded.Facts[0].Key}, Loss: []projection.LossDetail{}},
		{Kind: projection.ItemFact, ItemID: unknown, Outcome: projection.ProjectionUnknown, SourceKeys: []semreg.FactKey{}, Loss: []projection.LossDetail{}, Reason: &reason},
	}
	manifest := projection.ProjectionManifest{TargetID: "target:thermal:synthetic", TargetVersion: "v1", KernelVersion: semreg.ContractKernelV1, PackVersions: []semreg.PackRef{thermal.New().Pack()}, MappingRevision: "1"}
	report, err := projection.Project(decoded, manifest, requested, dispositions, nil)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := semreg.CanonicalJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	report, err = semreg.Decode[projection.ProjectionReport](wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projection.ValidateReport(decoded, report); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Dispositions, dispositions) || len(decoded.Facts) != 2 {
		t.Fatal("exact zero or explicit unknown accounting changed")
	}
	for _, fact := range decoded.Facts {
		if fact.Key.FactID == unknown {
			t.Fatal("unknown field acquired a zero/false fact")
		}
	}
	// Missing a required unknown reason is an accounting error, not exact zero.
	report.Dispositions[1].Reason = nil
	k14Error(t, report.Validate(), semreg.ProjectionIncomplete)
	key := decoded.Facts[0].Key
	key.FactID = unknown
	k14Error(t, thermal.New().ValidateFact(key, nil), semreg.MissingMember)
}

func TestK14FalsePresenceAndThermalKindBoundary(t *testing.T) {
	value, err := semreg.Decode[semreg.Value]([]byte(`{"kind":"boolean","boolean":false}`))
	if err != nil || value.Boolean == nil || *value.Boolean {
		t.Fatalf("present false lost: %+v, %v", value, err)
	}
	wire, err := semreg.CanonicalJSON(value)
	if err != nil || !bytes.Equal(wire, []byte(`{"boolean":false,"kind":"boolean"}`)) {
		t.Fatalf("false serialization: %s, %v", wire, err)
	}
	for _, tc := range []struct {
		raw  string
		want semreg.ErrorID
	}{
		{`{"kind":"boolean"}`, semreg.InvalidValue},
		{`{"kind":"boolean","boolean":null}`, semreg.MissingMember},
	} {
		_, err := semreg.Decode[semreg.Value]([]byte(tc.raw))
		k14Error(t, err, tc.want)
	}
	for _, field := range thermal.Metadata().Fields {
		ref := field.Ref
		k14Error(t, thermal.New().ValidateField(ref, semreg.TypedField{ID: ref.ID, Value: value}), semreg.InvalidValue)
	}
}

func TestK14InvalidNumericPartialObservation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value semreg.Value
		want  semreg.ErrorID
	}{
		{"wrong-unit", k14Temperature("23", 0, "unit.watt"), semreg.InvalidValue},
		{"noncanonical-zero-scale", k14Temperature("0", -1, "unit.celsius"), semreg.InvalidDecimal},
		{"scale-outside-kernel", k14Temperature("1", 19, "unit.celsius"), semreg.InvalidDecimal},
		{"malformed-number", k14Temperature("NaN", 0, "unit.celsius"), semreg.InvalidDecimal},
		{"noncanonical-number", k14Temperature("230", -1, "unit.celsius"), semreg.InvalidDecimal},
		{"scaled-value-outside-pack", k14Temperature("3", 2, "unit.celsius"), semreg.BoundsExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kernel, initial := k14Initial(t)
			before, beforeRaw := k14Apply(t, kernel, initial)
			partial := k14Batch(initial, "2", "1")
			good, bad := initial.FactUpserts[0], initial.FactUpserts[1]
			good.Revision, bad.Revision = "2", "2"
			goodValue := k14Temperature("1", 0, "unit.celsius")
			good.Value, bad.Value = &goodValue, &tc.value
			registry, err := semreg.NewRegistry(thermal.New())
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.ValidateFactCandidate(good); err != nil {
				t.Fatalf("independently valid field: %v", err)
			}
			k14Error(t, registry.ValidateFactCandidate(bad), tc.want)
			partial.FactUpserts = []semreg.FactCandidate{good, bad}
			// Malformed decimals cannot even be sealed by the public API.
			digest, err := partial.ComputedDigest()
			if tc.want == semreg.InvalidDecimal {
				k14Error(t, err, tc.want)
				partial.BatchDigest = before.Cursors[0].LastBatchDigest
			} else {
				if err != nil {
					t.Fatal(err)
				}
				partial.BatchDigest = digest
			}
			_, _, err = kernel.Apply(partial, before.EvaluateMonotonic)
			k14Error(t, err, tc.want)
			after, afterRaw, ok := kernel.Current()
			if !ok || !reflect.DeepEqual(after, before) || !bytes.Equal(afterRaw, beforeRaw) {
				t.Fatal("rejected observation destroyed last-known-good state")
			}
			// Publication is atomic. A permitted partial update supplies only
			// the valid field and retains the omitted field's prior evidence.
			partial.FactUpserts = []semreg.FactCandidate{good}
			updated, _ := k14Apply(t, kernel, partial)
			if len(updated.Facts) != 2 || !reflect.DeepEqual(updated.Facts[0].Candidates[0], good) || !reflect.DeepEqual(updated.Facts[1], before.Facts[1]) {
				t.Fatal("valid partial update lost its sibling or source evidence")
			}
		})
	}
}

func k14Temperature(coefficient string, exponent int32, unit semreg.DefinitionID) semreg.Value {
	return semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: coefficient, Exponent10: exponent}, Unit: unit}}
}

func k14Initial(t *testing.T) (*semreg.PublicationKernel, semreg.PublicationBatch) {
	t.Helper()
	// Reuse only the synthetic source/evidence/time scaffolding of K-POS-019.
	raw, err := os.ReadFile("../../fixtures/v1/evaluation-selection-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			ID    string
			Input struct{ Snapshot semreg.Snapshot }
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	var template semreg.Snapshot
	for _, vector := range fixture.Vectors {
		if vector.ID == "K-POS-019" {
			template = vector.Input.Snapshot
		}
	}
	if err := template.Validate(); err != nil {
		t.Fatal(err)
	}
	kernel, err := semreg.NewPublicationKernel(template.AssetID, thermal.New())
	if err != nil {
		t.Fatal(err)
	}
	batch := k14Batch(semreg.PublicationBatch{AssetID: template.AssetID, SourceID: template.Sources[0].SourceID, SourceEpochID: template.Sources[0].SourceEpochID, DriverGeneration: "1", ObservedAt: template.EvaluatedAt}, "1", "0")
	batch.SourceUpserts, batch.BindingUpserts, batch.IdentityLinkUpserts = template.Sources, template.Bindings, template.IdentityLinks
	for i, id := range []semreg.DefinitionID{"thermal.measurement.air_temperature", "thermal.measurement.water_temperature"} {
		candidate := template.Facts[0].Candidates[0]
		candidate.CandidateID = semreg.CandidateID("candidate:" + string(id))
		candidate.Revision = "1"
		dimension := "synthetic"
		candidate.Key = semreg.FactKey{PackID: thermal.New().Pack().ID, PackVersion: "1.0.0", FactID: id, Dimensions: []semreg.Dimension{{ID: "thermal.dimension.temperature", Value: semreg.Value{Kind: semreg.ValueText, Text: &dimension}}}}
		value := k14Temperature([]string{"0", "23"}[i], 0, "unit.celsius")
		candidate.Value = &value
		batch.FactUpserts = append(batch.FactUpserts, candidate)
	}
	return kernel, batch
}

func k14Batch(base semreg.PublicationBatch, sequence, revision semreg.Uint64) semreg.PublicationBatch {
	return semreg.PublicationBatch{
		Contract: semreg.ContractKernelV1, BatchID: semreg.BatchID("batch:thermal:" + string(sequence)), AssetID: base.AssetID, SourceID: base.SourceID, SourceEpochID: base.SourceEpochID, DriverGeneration: base.DriverGeneration, Sequence: sequence, ExpectedSemanticRevision: revision, ObservedAt: base.ObservedAt,
		SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{}, IdentityLinkUpserts: []semreg.IdentityLink{}, FactUpserts: []semreg.FactCandidate{}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: []semreg.CapabilityInstance{}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{},
	}
}

func k14Apply(t *testing.T, kernel *semreg.PublicationKernel, batch semreg.PublicationBatch) (semreg.Snapshot, []byte) {
	t.Helper()
	digest, err := batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	batch.BatchDigest = digest
	snapshot, raw, err := kernel.Apply(batch, semreg.MonotonicPoint{ClockEpochID: "clock-epoch:publication", Nanoseconds: "100"})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, raw
}

func k14Error(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if got := semreg.ErrorIdentifier(err); got != want {
		t.Fatalf("error = %s (%v), want %s", got, err, want)
	}
}
