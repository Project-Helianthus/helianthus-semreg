package projection_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

const acceptedVectorSHA256 = "51a177ce08e478cd9b003c213813abcf6e969ce1dc4cff4b276f7a2d480344f8"

type selectedVector struct {
	ID        string          `json:"id"`
	Polarity  string          `json:"polarity"`
	Record    string          `json:"record_type"`
	Operation string          `json:"operation"`
	Input     json.RawMessage `json:"input"`
	Expect    struct {
		Result     string         `json:"result"`
		ErrorID    semreg.ErrorID `json:"error_id"`
		Assertions []string       `json:"assertions"`
	} `json:"expect"`
}

type projectionVectorDisposition struct {
	Kind       projection.ItemKind          `json:"kind"`
	ItemID     semreg.DefinitionID          `json:"item_id"`
	Outcome    projection.ProjectionOutcome `json:"outcome"`
	SourceKeys []string                     `json:"source_keys"`
	Loss       []struct {
		Kind       projection.LossKind `json:"kind"`
		Reversible bool                `json:"reversible"`
	} `json:"loss"`
	Reason *semreg.DefinitionID `json:"reason"`
}

type projectionVectorInput struct {
	Requested        []projection.RequestedItem    `json:"requested"`
	Dispositions     []projectionVectorDisposition `json:"dispositions"`
	SnapshotRevision semreg.Uint64                 `json:"snapshot_revision"`
	CausalPreserved  bool                          `json:"causal_preserved"`
}

func loadSelectedVectors(t *testing.T) map[string]selectedVector {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve selected projection fixture")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "fixtures", "v1", "projection-compatibility-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Contract string `json:"contract"`
		Source   struct {
			Repository string `json:"repository"`
			Commit     string `json:"commit"`
			Path       string `json:"path"`
			SHA256     string `json:"sha256"`
			Selection  string `json:"selection"`
			Count      int    `json:"count"`
		} `json:"source"`
		Vectors []selectedVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.Contract != "helianthus.semantic.kernel.acceptance/v1" ||
		document.Source.Repository != "Project-Helianthus/helianthus-docs-semantic" ||
		document.Source.Commit != "da5ab4415d3bec73f9572aec1c495a6cdcbcba47" ||
		document.Source.Path != "api/v1/acceptance-vectors.json" ||
		document.Source.SHA256 != acceptedVectorSHA256 || document.Source.Count != 5 ||
		document.Source.Selection != "K-POS-016,K-POS-017,K-NEG-024,K-NEG-025,K-NEG-037" || len(document.Vectors) != 5 {
		t.Fatalf("unexpected selected fixture manifest: %+v vectors=%d", document.Source, len(document.Vectors))
	}
	result := make(map[string]selectedVector, len(document.Vectors))
	for _, vector := range document.Vectors {
		if _, exists := result[vector.ID]; exists {
			t.Fatalf("duplicate selected vector %s", vector.ID)
		}
		result[vector.ID] = vector
	}
	return result
}

func projectionSnapshot(t *testing.T) semreg.Snapshot {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve snapshot fixture")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "fixtures", "v1", "evaluation-selection-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Vectors []struct {
			ID    string `json:"id"`
			Input struct {
				Snapshot semreg.Snapshot `json:"snapshot"`
			} `json:"input"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for _, vector := range document.Vectors {
		if vector.ID == "K-POS-019" {
			if err := vector.Input.Snapshot.Validate(); err != nil {
				t.Fatalf("K-POS-019 snapshot: %v", err)
			}
			return vector.Input.Snapshot
		}
	}
	t.Fatal("K-POS-019 snapshot missing")
	return semreg.Snapshot{}
}

func projectionSnapshotForVector(t *testing.T, semanticRevision semreg.Uint64) semreg.Snapshot {
	t.Helper()
	snapshot := projectionSnapshot(t)
	snapshot.Revisions.Semantic = semanticRevision
	snapshot.Facts[0].Key = factKey()
	for index := range snapshot.Facts[0].Candidates {
		snapshot.Facts[0].Candidates[index].Key = factKey()
	}
	sealProjectionSnapshot(t, &snapshot)
	return snapshot
}

func sealProjectionSnapshot(t *testing.T, snapshot *semreg.Snapshot) {
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
	digest := sha256.Sum256(raw)
	snapshot.SnapshotID = semreg.SnapshotID(fmt.Sprintf("sha256:%x", digest))
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("expanded projection snapshot: %v", err)
	}
}

func manifest() projection.ProjectionManifest {
	return projection.ProjectionManifest{
		TargetID: "target:projection:test", TargetVersion: "target-1", KernelVersion: semreg.ContractKernelV1,
		PackVersions: []semreg.PackRef{{ID: "helianthus.pack.base", Version: "1.0.0"}}, MappingRevision: "1",
	}
}

func factKey() semreg.FactKey {
	return semreg.FactKey{PackID: "helianthus.pack.base", PackVersion: "1.0.0", FactID: "helianthus.pv.active_power", Dimensions: []semreg.Dimension{}}
}

func evidence(digest string) semreg.EvidenceRef {
	return semreg.EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest("sha256:" + digest), Contract: "test.contract/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone}
}

func alias(routable bool) projection.CompatibilityAlias {
	return projection.CompatibilityAlias{
		AliasContract: projection.ContractAliasV1, LegacyID: "Service:/HeatGenerator:1", AssetID: "asset:heat-pump:01", ValidFrom: "1.0.0", Routable: routable,
		Evidence: []semreg.EvidenceRef{evidence(strings.Repeat("1", 64))},
	}
}

func exact(kind projection.ItemKind, id semreg.DefinitionID) projection.ProjectionDisposition {
	return projection.ProjectionDisposition{Kind: kind, ItemID: id, Outcome: projection.ProjectionExact, SourceKeys: []semreg.FactKey{}, Loss: []projection.LossDetail{}}
}

func expandProjectionVector(t *testing.T, raw json.RawMessage) (projectionVectorInput, []projection.ProjectionDisposition) {
	t.Helper()
	var input projectionVectorInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	dispositions := make([]projection.ProjectionDisposition, len(input.Dispositions))
	for index, source := range input.Dispositions {
		disposition := projection.ProjectionDisposition{
			Kind: source.Kind, ItemID: source.ItemID, Outcome: source.Outcome,
			SourceKeys: []semreg.FactKey{}, Loss: []projection.LossDetail{}, Reason: source.Reason,
		}
		for _, sourceKey := range source.SourceKeys {
			if sourceKey != "pv-power-key" {
				t.Fatalf("unknown projection source-key fixture %q", sourceKey)
			}
			disposition.SourceKeys = append(disposition.SourceKeys, factKey())
		}
		for _, sourceLoss := range source.Loss {
			if sourceLoss.Kind != projection.LossPrecision {
				t.Fatalf("unsupported projection loss fixture %q", sourceLoss.Kind)
			}
			disposition.Loss = append(disposition.Loss, projection.LossDetail{
				Kind: sourceLoss.Kind, SourceItems: []semreg.DefinitionID{source.ItemID},
				Description: "Precision reduced.", Reversible: sourceLoss.Reversible,
			})
		}
		dispositions[index] = disposition
	}
	return input, dispositions
}

func TestSelectedProjectionCompatibilityVectors(t *testing.T) {
	vectors := loadSelectedVectors(t)
	for _, id := range []string{"K-POS-016", "K-POS-017", "K-NEG-024", "K-NEG-025", "K-NEG-037"} {
		vector, ok := vectors[id]
		if !ok {
			t.Fatalf("missing selected vector %s", id)
		}
		t.Run(id, func(t *testing.T) {
			switch id {
			case "K-POS-016":
				input, dispositions := expandProjectionVector(t, vector.Input)
				snapshot := projectionSnapshotForVector(t, input.SnapshotRevision)
				before, err := semreg.CanonicalJSON(snapshot)
				if err != nil {
					t.Fatal(err)
				}
				causal := validCausal()
				report, err := projection.Project(snapshot, manifest(), input.Requested, dispositions, &causal)
				if err != nil {
					t.Fatalf("%s unexpected error: %v", vector.ID, err)
				}
				if report.SnapshotID != snapshot.SnapshotID || report.Revisions != snapshot.Revisions || report.Revisions.Semantic != input.SnapshotRevision || report.Causal == nil || !reflect.DeepEqual(*report.Causal, causal) {
					t.Fatalf("%s did not preserve snapshot/revisions/causal: %+v", vector.ID, report)
				}
				if !reflect.DeepEqual(report.Requested, input.Requested) || !reflect.DeepEqual(report.Dispositions, dispositions) {
					t.Fatalf("%s did not execute exact expanded tuples/dispositions", vector.ID)
				}
				if len(report.Dispositions) != 3 || report.Dispositions[0].ItemID != report.Dispositions[2].ItemID || report.Dispositions[0].Kind == report.Dispositions[2].Kind {
					t.Fatalf("%s collapsed equal IDs across kinds: %+v", vector.ID, report.Dispositions)
				}
				precision := report.Dispositions[1]
				if precision.Outcome != projection.ProjectionTransformed || len(precision.SourceKeys) != 1 || !reflect.DeepEqual(precision.SourceKeys[0], factKey()) || len(precision.Loss) != 1 || precision.Loss[0].Kind != projection.LossPrecision || precision.Loss[0].Reversible {
					t.Fatalf("%s precision loss/source lineage not visible: %+v", vector.ID, precision)
				}
				after, _ := semreg.CanonicalJSON(snapshot)
				if !bytes.Equal(before, after) {
					t.Fatalf("%s mutated snapshot", vector.ID)
				}
			case "K-POS-017":
				var input struct {
					AliasContract semreg.ContractVersion `json:"alias_contract"`
					LegacyID      semreg.OpaqueID        `json:"legacy_id"`
					AssetID       semreg.AssetID         `json:"asset_id"`
					ValidFrom     semreg.SemanticVersion `json:"valid_from"`
					Routable      bool                   `json:"routable"`
					EvidenceCount int                    `json:"evidence_count"`
				}
				if err := json.Unmarshal(vector.Input, &input); err != nil {
					t.Fatal(err)
				}
				expanded := alias(input.Routable)
				expanded.AliasContract, expanded.LegacyID, expanded.AssetID, expanded.ValidFrom = input.AliasContract, input.LegacyID, input.AssetID, input.ValidFrom
				expanded.Evidence = expanded.Evidence[:input.EvidenceCount]
				raw, err := semreg.CanonicalJSON(expanded)
				if err != nil {
					t.Fatalf("%s unexpected error: %v", vector.ID, err)
				}
				decoded, err := semreg.Decode[projection.CompatibilityAlias](raw)
				if err != nil || !reflect.DeepEqual(decoded, expanded) || decoded.LegacyID != input.LegacyID || decoded.AssetID != input.AssetID {
					t.Fatalf("%s alias identity round trip err=%v decoded=%+v", vector.ID, err, decoded)
				}
				kernel, err := operation.NewKernel()
				if err != nil {
					t.Fatal(err)
				}
				if _, err = kernel.AdmitAliasRoute(decoded.LegacyID); semreg.ErrorIdentifier(err) != semreg.AliasNotRoutable {
					t.Fatalf("%s alias route err=%v", vector.ID, err)
				}
			case "K-NEG-024", "K-NEG-037":
				input, dispositions := expandProjectionVector(t, vector.Input)
				snapshot := projectionSnapshot(t)
				before, _ := semreg.CanonicalJSON(snapshot)
				_, err := projection.Project(snapshot, manifest(), input.Requested, dispositions, nil)
				if err == nil || semreg.ErrorIdentifier(err) != vector.Expect.ErrorID {
					t.Fatalf("%s err=%v want %s", vector.ID, err, vector.Expect.ErrorID)
				}
				after, _ := semreg.CanonicalJSON(snapshot)
				if !bytes.Equal(before, after) {
					t.Fatalf("%s mutated snapshot", vector.ID)
				}
			case "K-NEG-025":
				var input struct {
					LegacyID semreg.OpaqueID `json:"legacy_id"`
					Routable bool            `json:"routable"`
				}
				if err := json.Unmarshal(vector.Input, &input); err != nil {
					t.Fatal(err)
				}
				invalidAlias := alias(input.Routable)
				invalidAlias.LegacyID = input.LegacyID
				err := invalidAlias.Validate()
				if err == nil || semreg.ErrorIdentifier(err) != vector.Expect.ErrorID {
					t.Fatalf("%s err=%v want %s", vector.ID, err, vector.Expect.ErrorID)
				}
				kernel, err := operation.NewKernel()
				if err != nil {
					t.Fatal(err)
				}
				_, err = kernel.AdmitAliasRoute(input.LegacyID)
				if err == nil || semreg.ErrorIdentifier(err) != vector.Expect.ErrorID {
					t.Fatalf("%s route err=%v want %s", vector.ID, err, vector.Expect.ErrorID)
				}
			}
		})
	}
}

func TestProjectionContractMatrices(t *testing.T) {
	snapshot := projectionSnapshot(t)
	requested := []projection.RequestedItem{{Kind: projection.ItemFact, ItemID: "helianthus.pv.active_power"}}
	for _, outcome := range []projection.ProjectionOutcome{projection.ProjectionExact, projection.ProjectionTransformed, projection.ProjectionWithheld, projection.ProjectionUnrepresentable, projection.ProjectionUnsupported, projection.ProjectionUnknown} {
		t.Run(string(outcome), func(t *testing.T) {
			disposition := exact(projection.ItemFact, "helianthus.pv.active_power")
			disposition.Outcome = outcome
			if outcome == projection.ProjectionTransformed {
				disposition.Loss = []projection.LossDetail{{Kind: projection.LossPrecision, SourceItems: []semreg.DefinitionID{"helianthus.pv.active_power"}, Description: "Precision reduced."}}
			}
			if outcome == projection.ProjectionWithheld || outcome == projection.ProjectionUnrepresentable || outcome == projection.ProjectionUnsupported || outcome == projection.ProjectionUnknown {
				disposition.Reason = definition("policy.withheld")
			}
			if _, err := projection.Project(snapshot, manifest(), requested, []projection.ProjectionDisposition{disposition}, nil); err != nil {
				t.Fatalf("%s: %v", outcome, err)
			}
		})
	}

	for name, dispositions := range map[string][]projection.ProjectionDisposition{
		"missing": {}, "extra": {exact(projection.ItemFact, "helianthus.pv.active_power"), exact(projection.ItemOperation, "helianthus.pv.active_power")},
		"duplicate":  {exact(projection.ItemFact, "helianthus.pv.active_power"), exact(projection.ItemFact, "helianthus.pv.active_power")},
		"mismatched": {exact(projection.ItemOperation, "helianthus.pv.active_power")},
	} {
		t.Run("tuple_"+name, func(t *testing.T) {
			_, err := projection.Project(snapshot, manifest(), requested, dispositions, nil)
			if err == nil || semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
				t.Fatalf("%s err=%v", name, err)
			}
		})
	}

	badOrder := []projection.RequestedItem{{Kind: projection.ItemOperation, ItemID: "helianthus.pv.active_power"}, {Kind: projection.ItemFact, ItemID: "helianthus.pv.active_power"}}
	_, err := projection.Project(snapshot, manifest(), badOrder, []projection.ProjectionDisposition{exact(projection.ItemFact, "helianthus.pv.active_power"), exact(projection.ItemOperation, "helianthus.pv.active_power")}, nil)
	if semreg.ErrorIdentifier(err) != semreg.NoncanonicalOrder {
		t.Fatalf("order precedence: %v", err)
	}

	tooMany := make([]projection.RequestedItem, 4097)
	for index := range tooMany {
		tooMany[index] = projection.RequestedItem{Kind: projection.ItemFact, ItemID: "helianthus.pv.active_power"}
	}
	if _, err := projection.Project(snapshot, manifest(), tooMany, []projection.ProjectionDisposition{}, nil); semreg.ErrorIdentifier(err) != semreg.BoundsExceeded {
		t.Fatalf("bounded allocation rejection: %v", err)
	}
}

func TestProjectionPublicFormsOrderingAndPrecedence(t *testing.T) {
	orderedManifest := manifest()
	orderedManifest.PackVersions = []semreg.PackRef{{ID: "helianthus.pack.base", Version: "1.2.0"}, {ID: "helianthus.pack.base", Version: "1.10.0"}}
	if err := orderedManifest.Validate(); err != nil {
		t.Fatalf("numeric pack-version ordering: %v", err)
	}
	if err := (projection.RequestedItem{Kind: projection.ItemRelation, ItemID: "helianthus.relation.test"}).Validate(); err != nil {
		t.Fatalf("requested form: %v", err)
	}
	loss := projection.LossDetail{Kind: projection.LossProvenance, SourceItems: []semreg.DefinitionID{"helianthus.alpha.item", "helianthus.beta.item"}, Description: "Public provenance reduction."}
	if err := loss.Validate(); err != nil {
		t.Fatalf("loss form: %v", err)
	}
	blankDescription := loss
	blankDescription.Description = ""
	if err := blankDescription.Validate(); err != nil {
		t.Fatalf("empty public NFC description: %v", err)
	}
	decomposedDescription := loss
	decomposedDescription.Description = "Cafe\u0301"
	if semreg.ErrorIdentifier(decomposedDescription.Validate()) != semreg.InvalidValue {
		t.Fatalf("non-NFC description: %v", decomposedDescription.Validate())
	}
	loss.SourceItems[0], loss.SourceItems[1] = loss.SourceItems[1], loss.SourceItems[0]
	if semreg.ErrorIdentifier(loss.Validate()) != semreg.NoncanonicalOrder {
		t.Fatalf("loss source item ordering: %v", loss.Validate())
	}
	first, second := factKey(), factKey()
	first.FactID, second.FactID = "helianthus.pv.alpha", "helianthus.pv.beta"
	disposition := exact(projection.ItemFact, "helianthus.pv.active_power")
	disposition.SourceKeys = []semreg.FactKey{second, first}
	if semreg.ErrorIdentifier(disposition.Validate()) != semreg.NoncanonicalOrder {
		t.Fatalf("source-key ordering: %v", disposition.Validate())
	}

	snapshot := projectionSnapshot(t)
	requested := []projection.RequestedItem{{Kind: projection.ItemFact, ItemID: "helianthus.pv.active_power"}}
	transformed := exact(projection.ItemFact, "helianthus.pv.active_power")
	transformed.Outcome = projection.ProjectionTransformed
	if _, err := projection.Project(snapshot, manifest(), requested, []projection.ProjectionDisposition{transformed}, nil); semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
		t.Fatalf("transformed loss: %v", err)
	}
	exactWithLoss := exact(projection.ItemFact, "helianthus.pv.active_power")
	exactWithLoss.Loss = []projection.LossDetail{{Kind: projection.LossPrecision, SourceItems: []semreg.DefinitionID{"helianthus.pv.active_power"}, Description: "Precision reduced."}}
	if _, err := projection.Project(snapshot, manifest(), requested, []projection.ProjectionDisposition{exactWithLoss}, nil); semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
		t.Fatalf("exact loss: %v", err)
	}
	withheld := exact(projection.ItemFact, "helianthus.pv.active_power")
	withheld.Outcome = projection.ProjectionWithheld
	if _, err := projection.Project(snapshot, manifest(), requested, []projection.ProjectionDisposition{withheld}, nil); semreg.ErrorIdentifier(err) != semreg.ProjectionIncomplete {
		t.Fatalf("withheld reason: %v", err)
	}
	brokenManifest := manifest()
	brokenManifest.KernelVersion = "test.contract/v1"
	if _, err := projection.Project(snapshot, brokenManifest, requested, []projection.ProjectionDisposition{}, nil); semreg.ErrorIdentifier(err) != semreg.InvalidContract {
		t.Fatalf("error precedence: %v", err)
	}
}

func TestProjectionWireDeterminismAndIsolation(t *testing.T) {
	snapshot := projectionSnapshot(t)
	requested := []projection.RequestedItem{{Kind: projection.ItemFact, ItemID: "helianthus.pv.active_power"}}
	dispositions := []projection.ProjectionDisposition{exact(projection.ItemFact, "helianthus.pv.active_power")}
	report, err := projection.Project(snapshot, manifest(), requested, dispositions, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := semreg.CanonicalJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := semreg.Decode[projection.ProjectionReport](first)
	if err != nil || !reflect.DeepEqual(decoded, report) {
		t.Fatalf("strict round trip err=%v decoded=%+v", err, decoded)
	}
	for name, raw := range map[string]string{
		"wrong_contract": strings.Replace(string(first), string(projection.ContractProjectionV1), "test.contract/v1", 1),
		"unknown":        strings.TrimSuffix(string(first), "}") + ",\"unknown\":true}",
		"duplicate":      strings.Replace(string(first), "\"contract\":\""+string(projection.ContractProjectionV1)+"\"", "\"contract\":\""+string(projection.ContractProjectionV1)+"\",\"contract\":\""+string(projection.ContractProjectionV1)+"\"", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := semreg.Decode[projection.ProjectionReport]([]byte(raw)); err == nil {
				t.Fatal("strict decode accepted invalid wire")
			}
		})
	}
	beforeRequested, beforeDispositions := append([]projection.RequestedItem(nil), requested...), append([]projection.ProjectionDisposition(nil), dispositions...)
	report.Requested[0].ItemID = "helianthus.changed.item"
	if !reflect.DeepEqual(requested, beforeRequested) || !reflect.DeepEqual(dispositions, beforeDispositions) {
		t.Fatal("project result aliases caller input")
	}
	report, err = projection.Project(snapshot, manifest(), requested, dispositions, nil)
	if err != nil {
		t.Fatal(err)
	}
	report.Revisions.Semantic = "11"
	if _, err := projection.ValidateReport(snapshot, report); semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
		t.Fatalf("snapshot binding: %v", err)
	}
}

func TestCompatibilityAliasIntervalsEvidenceAndWire(t *testing.T) {
	valid := alias(false)
	until := semreg.SemanticVersion("1.10.0")
	valid.ValidFrom, valid.ValidUntil = "1.2.0", &until
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	equal := valid
	equal.ValidUntil = definitionVersion("1.2.0")
	if semreg.ErrorIdentifier(equal.Validate()) != semreg.InvalidValue {
		t.Fatalf("interval order: %v", equal.Validate())
	}
	duplicate := alias(false)
	duplicate.Evidence = append(duplicate.Evidence, duplicate.Evidence[0])
	if semreg.ErrorIdentifier(duplicate.Validate()) != semreg.DuplicateKey {
		t.Fatalf("duplicate evidence: %v", duplicate.Validate())
	}
	raw, err := semreg.CanonicalJSON(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := semreg.Decode[projection.CompatibilityAlias](raw); err != nil {
		t.Fatalf("alias strict decode: %v", err)
	}
	wrong := strings.Replace(string(raw), string(projection.ContractAliasV1), "test.contract/v1", 1)
	if _, err := semreg.Decode[projection.CompatibilityAlias]([]byte(wrong)); semreg.ErrorIdentifier(err) != semreg.InvalidContract {
		t.Fatalf("alias discriminator: %v", err)
	}
}

func validCausal() semreg.CausalContext {
	return semreg.CausalContext{Origin: semreg.OriginRef{OriginID: "origin:projection:test", Kind: semreg.OriginProjection, Evidence: []semreg.EvidenceRef{evidence(strings.Repeat("2", 64))}}, CorrelationID: "correlation:projection:test", HopCount: 1, MaxHops: 2, FirstSeenAt: semreg.TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"}, ExpiresAt: semreg.TimePoint{UnixNanoseconds: "200", ClockID: "clock.utc", UncertaintyNS: "0"}, Path: []semreg.TargetID{"target:projection:test"}}
}

func definition(value string) *semreg.DefinitionID {
	result := semreg.DefinitionID(value)
	return &result
}
func definitionVersion(value string) *semreg.SemanticVersion {
	result := semreg.SemanticVersion(value)
	return &result
}
