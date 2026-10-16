package semreg_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

// K15 uses only synthetic kernel fixtures and Infrastructure v1 definitions.
// Projection validates supplied accounting; pack validation owns qualification.
func TestK15InfrastructureCapabilityWithholding(t *testing.T) {
	for _, qualification := range []semreg.Qualification{semreg.QualificationCandidate, semreg.QualificationUnknown, semreg.QualificationUnsupported} {
		t.Run(string(qualification), func(t *testing.T) {
			snapshot := k15Snapshot(t)
			validator := infrastructure.New()
			capability := snapshot.Capabilities[0]
			if err := validator.ValidateCapability(capability); err != nil {
				t.Fatal(err)
			}
			capability.Qualification = qualification
			if got := semreg.ErrorIdentifier(validator.ValidateCapability(capability)); got != semreg.CapabilityNotQualified {
				t.Fatalf("unqualified capability: %s", got)
			}

			candidate := &snapshot.Facts[0].Candidates[0]
			if err := validator.ValidateFact(candidate.Key, candidate.Value); err != nil {
				t.Fatal(err)
			}
			candidate.Quality.Qualification = qualification
			candidate.Quality.Promotion = semreg.PromotionUnpromoted
			if qualification == semreg.QualificationUnsupported {
				if got := semreg.ErrorIdentifier(candidate.Validate()); got != semreg.InvalidValue {
					t.Fatalf("unsupported value accepted: %s", got)
				}
				candidate.Value = nil
			}
			if err := candidate.Validate(); err != nil {
				t.Fatal(err)
			}
			promoted := *candidate
			promoted.Quality.Promotion = semreg.PromotionPromoted
			if promoted.Validate() == nil {
				t.Fatal("unqualified observation became promoted")
			}
			snapshot.Capabilities = []semreg.CapabilityInstance{}
			k15Seal(t, &snapshot)
			before := k15Canonical(t, snapshot)

			outcome := projection.ProjectionWithheld
			if qualification == semreg.QualificationUnknown {
				outcome = projection.ProjectionUnknown
			}
			if qualification == semreg.QualificationUnsupported {
				outcome = projection.ProjectionUnsupported
			}
			reason := semreg.DefinitionID("infrastructure.reason.qualification_required")
			requested := []projection.RequestedItem{
				{Kind: projection.ItemCapability, ItemID: capability.Definition.ID},
				{Kind: projection.ItemFact, ItemID: candidate.Key.FactID},
			}
			dispositions := []projection.ProjectionDisposition{
				{Kind: projection.ItemCapability, ItemID: capability.Definition.ID, Outcome: outcome, SourceKeys: []semreg.FactKey{candidate.Key}, Loss: []projection.LossDetail{{Kind: projection.LossCapability, SourceItems: []semreg.DefinitionID{capability.Definition.ID}, Description: "No qualified public capability."}}, Reason: &reason},
				{Kind: projection.ItemFact, ItemID: candidate.Key.FactID, Outcome: outcome, SourceKeys: []semreg.FactKey{candidate.Key}, Loss: []projection.LossDetail{}, Reason: &reason},
			}
			report := k15Project(t, snapshot, requested, dispositions)
			if report.Dispositions[0].Outcome == projection.ProjectionExact || len(snapshot.Capabilities) != 0 {
				t.Fatal("candidate synthesized a supported capability")
			}
			if !reflect.DeepEqual(before, k15Canonical(t, snapshot)) {
				t.Fatal("projection changed candidate evidence or snapshot state")
			}
			missingReason := append([]projection.ProjectionDisposition(nil), dispositions...)
			missingReason[0].Reason = nil
			if _, err := projection.Project(snapshot, k15Manifest(), requested, missingReason, nil); semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
				t.Fatalf("missing withholding reason: %v", err)
			}
		})
	}
}

func TestK15InfrastructureSupportedAndUnsupportedSibling(t *testing.T) {
	snapshot := k15Snapshot(t)
	validator := infrastructure.New()
	supported := snapshot.Facts[0]
	if err := validator.ValidateFact(supported.Key, supported.Candidates[0].Value); err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateCapability(snapshot.Capabilities[0]); err != nil {
		t.Fatal(err)
	}
	// Current is a declared optional sibling, not an invented canonical field.
	sibling := supported
	sibling.Key.FactID = "infrastructure.ac.current"
	sibling.Candidates = append([]semreg.FactCandidate(nil), supported.Candidates...)
	candidate := &sibling.Candidates[0]
	candidate.CandidateID = "candidate:unsupported-current"
	candidate.Key = sibling.Key
	candidate.Value = &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "12", Exponent10: 0}, Unit: "unit.ampere"}}
	if err := validator.ValidateFact(candidate.Key, candidate.Value); err != nil {
		t.Fatal(err)
	}
	candidate.Quality.Qualification = semreg.QualificationUnsupported
	candidate.Quality.Promotion = semreg.PromotionUnpromoted
	if got := semreg.ErrorIdentifier(candidate.Validate()); got != semreg.InvalidValue {
		t.Fatalf("unsupported sibling retained value: %s", got)
	}
	candidate.Value = nil
	if err := candidate.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot.Facts = []semreg.FactEnvelope{sibling, supported}
	k15Seal(t, &snapshot)
	before := k15Canonical(t, snapshot)
	reason := semreg.DefinitionID("infrastructure.reason.native_field_unsupported")
	requested := []projection.RequestedItem{{Kind: projection.ItemFact, ItemID: sibling.Key.FactID}, {Kind: projection.ItemFact, ItemID: supported.Key.FactID}}
	dispositions := []projection.ProjectionDisposition{
		{Kind: projection.ItemFact, ItemID: sibling.Key.FactID, Outcome: projection.ProjectionUnsupported, SourceKeys: []semreg.FactKey{sibling.Key}, Loss: []projection.LossDetail{{Kind: projection.LossCapability, SourceItems: []semreg.DefinitionID{sibling.Key.FactID}, Description: "Optional native field unsupported; value withheld."}}, Reason: &reason},
		{Kind: projection.ItemFact, ItemID: supported.Key.FactID, Outcome: projection.ProjectionExact, SourceKeys: []semreg.FactKey{supported.Key}, Loss: []projection.LossDetail{}},
	}
	report := k15Project(t, snapshot, requested, dispositions)
	if report.Dispositions[0].Outcome != projection.ProjectionUnsupported || len(report.Dispositions[0].Loss) != 1 || report.Dispositions[1].Outcome != projection.ProjectionExact || len(report.Dispositions[1].Loss) != 0 {
		t.Fatal("sibling dispositions or loss collapsed")
	}
	if !reflect.DeepEqual(before, k15Canonical(t, snapshot)) {
		t.Fatal("projection changed supported last-known-good value or unsupported lineage")
	}
	if _, err := projection.Project(snapshot, k15Manifest(), requested, dispositions[:1], nil); semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
		t.Fatalf("missing supported disposition: %v", err)
	}
	foreign := append([]projection.ProjectionDisposition(nil), dispositions...)
	absent := sibling.Key
	absent.FactID = "infrastructure.ac.reactive_power"
	foreign[0].SourceKeys = []semreg.FactKey{absent}
	if _, err := projection.Project(snapshot, k15Manifest(), requested, foreign, nil); semreg.ErrorIdentifier(err) != semreg.DanglingReference {
		t.Fatalf("unbound sibling evidence: %v", err)
	}
}

func k15Manifest() projection.ProjectionManifest {
	return projection.ProjectionManifest{TargetID: "target:synthetic-infrastructure", TargetVersion: "test-1", KernelVersion: semreg.ContractKernelV1, PackVersions: []semreg.PackRef{infrastructure.New().Pack()}, MappingRevision: "1"}
}

func k15Project(t *testing.T, snapshot semreg.Snapshot, requested []projection.RequestedItem, dispositions []projection.ProjectionDisposition) projection.ProjectionReport {
	t.Helper()
	report, err := projection.Project(snapshot, k15Manifest(), requested, dispositions, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.SnapshotID != snapshot.SnapshotID || report.Revisions != snapshot.Revisions || !reflect.DeepEqual(report.Requested, requested) || !reflect.DeepEqual(report.Dispositions, dispositions) {
		t.Fatal("projection lost snapshot binding, source keys, reasons, or loss")
	}
	if _, err := projection.ValidateReport(snapshot, report); err != nil {
		t.Fatal(err)
	}
	return report
}

func k15Canonical(t *testing.T, value semreg.Record) []byte {
	t.Helper()
	raw, err := semreg.CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func k15Seal(t *testing.T, snapshot *semreg.Snapshot) {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	delete(document, "snapshot_id")
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.SnapshotID = semreg.SnapshotID(fmt.Sprintf("sha256:%x", sha256.Sum256(raw)))
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
}

func k15Snapshot(t *testing.T) semreg.Snapshot {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate synthetic fixture")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "fixtures", "v1", "evaluation-selection-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			ID    string `json:"id"`
			Input struct {
				Snapshot semreg.Snapshot `json:"snapshot"`
			} `json:"input"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, vector := range fixture.Vectors {
		if vector.ID != "K-POS-019" {
			continue
		}
		snapshot := vector.Input.Snapshot
		if err := snapshot.Validate(); err != nil {
			t.Fatal(err)
		}
		pack := infrastructure.New().Pack()
		dimension := "phase:synthetic"
		key := semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: "infrastructure.ac.voltage", Dimensions: []semreg.Dimension{{ID: "infrastructure.dimension.phase", Value: semreg.Value{Kind: semreg.ValueText, Text: &dimension}}}}
		snapshot.Facts[0].Key = key
		snapshot.Facts[0].Candidates[0].Key = key
		snapshot.Facts[0].Candidates[0].Value = &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "23", Exponent10: 1}, Unit: "unit.volt"}}
		snapshot.Services[0].Definition = semreg.DefinitionRef{Pack: pack, ID: "infrastructure.service.phase", Version: "1.0.0"}
		snapshot.Capabilities[0].Definition = semreg.DefinitionRef{Pack: pack, ID: "infrastructure.capability.read.phase", Version: "1.0.0"}
		if err := infrastructure.New().ValidateService(snapshot.Services[0]); err != nil {
			t.Fatal(err)
		}
		k15Seal(t, &snapshot)
		return snapshot
	}
	t.Fatal("synthetic K-POS-019 snapshot missing")
	return semreg.Snapshot{}
}
