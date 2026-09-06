package projection_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

func correctionError(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted input; want %s", want)
	}
	if semreg.ErrorIdentifier(err) != want {
		t.Fatalf("error=%v id=%s want=%s", err, semreg.ErrorIdentifier(err), want)
	}
}

func sourceBoundReport(t *testing.T) (semreg.Snapshot, projection.ProjectionReport) {
	t.Helper()
	snapshot := projectionSnapshot(t)
	disposition := projection.ProjectionDisposition{
		Kind: projection.ItemFact, ItemID: "fact.power", Outcome: projection.ProjectionExact,
		SourceKeys: []semreg.FactKey{snapshot.Facts[0].Key}, Loss: []projection.LossDetail{},
	}
	report, err := projection.Project(snapshot, manifest(), []projection.RequestedItem{{Kind: disposition.Kind, ItemID: disposition.ItemID}}, []projection.ProjectionDisposition{disposition}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, report
}

func TestCorrectionTypedAndCanonicalMatrix(t *testing.T) {
	validLoss := projection.LossDetail{Kind: projection.LossPrecision, SourceItems: []semreg.DefinitionID{"fact.power"}, Description: ""}
	if err := validLoss.Validate(); err != nil {
		t.Fatalf("empty public NFC description must remain valid: %v", err)
	}
	for _, control := range []string{"\t", "\n", "\r", "\x00"} {
		t.Run(fmt.Sprintf("description_%U", []rune(control)[0]), func(t *testing.T) {
			loss := validLoss
			loss.Description = "public" + control + "text"
			correctionError(t, loss.Validate(), semreg.InvalidValue)
		})
	}

	t.Run("manifest_duplicate_precedes_bounds", func(t *testing.T) {
		value := manifest()
		value.PackVersions = make([]semreg.PackRef, 257)
		for index := range value.PackVersions {
			value.PackVersions[index] = semreg.PackRef{ID: "pack.test", Version: "1.0.0"}
		}
		correctionError(t, value.Validate(), semreg.DuplicateKey)
	})
	t.Run("loss_identifier_precedes_bounds", func(t *testing.T) {
		value := validLoss
		value.SourceItems = make([]semreg.DefinitionID, 33)
		correctionError(t, value.Validate(), semreg.InvalidIdentifier)
	})
	t.Run("disposition_reason_precedes_bounds", func(t *testing.T) {
		badReason := semreg.DefinitionID("!")
		value := projection.ProjectionDisposition{Kind: projection.ItemFact, ItemID: "fact.power", Outcome: projection.ProjectionWithheld, SourceKeys: make([]semreg.FactKey, 33), Loss: []projection.LossDetail{}, Reason: &badReason}
		for index := range value.SourceKeys {
			value.SourceKeys[index] = semreg.FactKey{PackID: "pack.test", PackVersion: "1.0.0", FactID: semreg.DefinitionID(fmt.Sprintf("fact.test%02d", index)), Dimensions: []semreg.Dimension{}}
		}
		correctionError(t, value.Validate(), semreg.InvalidIdentifier)
	})
	t.Run("report_member_precedes_bounds", func(t *testing.T) {
		_, value := sourceBoundReport(t)
		value.Requested = make([]projection.RequestedItem, 4097)
		value.Causal = &semreg.CausalContext{}
		correctionError(t, value.Validate(), semreg.MissingMember)
	})
	t.Run("alias_identifier_precedes_bounds", func(t *testing.T) {
		value := alias(false)
		invalid := semreg.SemanticVersion("invalid")
		value.ValidUntil = &invalid
		value.Evidence = make([]semreg.EvidenceRef, 33)
		correctionError(t, value.Validate(), semreg.InvalidIdentifier)
	})

	base := projection.LossDetail{Kind: projection.LossPrecision, SourceItems: []semreg.DefinitionID{"fact.power"}, Description: "Precision reduced."}
	for _, test := range []struct {
		name string
		loss []projection.LossDetail
		want semreg.ErrorID
	}{
		{"duplicate", []projection.LossDetail{base, base}, semreg.DuplicateKey},
		{"duplicate_ignores_reversible", []projection.LossDetail{base, func() projection.LossDetail { value := base; value.Reversible = true; return value }()}, semreg.DuplicateKey},
		{"descending", []projection.LossDetail{func() projection.LossDetail { value := base; value.Kind = projection.LossUnit; return value }(), base}, semreg.NoncanonicalOrder},
	} {
		t.Run("loss_collection_"+test.name, func(t *testing.T) {
			value := projection.ProjectionDisposition{Kind: projection.ItemFact, ItemID: "fact.power", Outcome: projection.ProjectionTransformed, SourceKeys: []semreg.FactKey{}, Loss: test.loss}
			correctionError(t, value.Validate(), test.want)
			_, err := semreg.CanonicalJSON(value)
			correctionError(t, err, test.want)
			raw, _ := json.Marshal(value)
			_, err = semreg.Decode[projection.ProjectionDisposition](raw)
			correctionError(t, err, test.want)
		})
	}

	t.Run("invalid_fact_keys_have_no_fallback_identity", func(t *testing.T) {
		first := semreg.FactKey{PackID: "pack.test", PackVersion: "1.0.0", FactID: "fact.power", Dimensions: []semreg.Dimension{{ID: "!first", Value: semreg.Value{Kind: semreg.ValueBoolean, Boolean: boolPointer(true)}}}}
		second := first
		second.Dimensions = []semreg.Dimension{{ID: "!second", Value: semreg.Value{Kind: semreg.ValueBoolean, Boolean: boolPointer(true)}}}
		value := projection.ProjectionDisposition{Kind: projection.ItemFact, ItemID: "fact.power", Outcome: projection.ProjectionExact, SourceKeys: []semreg.FactKey{first, second}, Loss: []projection.LossDetail{}}
		correctionError(t, value.Validate(), semreg.InvalidIdentifier)
	})
}

func boolPointer(value bool) *bool { return &value }

func TestCorrectionWireCombinedPrecedence(t *testing.T) {
	t.Run("pack_refs", func(t *testing.T) {
		value := manifest()
		value.PackVersions = append(value.PackVersions, value.PackVersions[0])
		raw, _ := json.Marshal(value)
		raw = bytes.Replace(raw, []byte(`"mapping_revision":"1"`), []byte(`"mapping_revision":7`), 1)
		_, err := semreg.Decode[projection.ProjectionManifest](raw)
		correctionError(t, err, semreg.DuplicateKey)
	})
	t.Run("fact_keys", func(t *testing.T) {
		snapshot, _ := sourceBoundReport(t)
		value := projection.ProjectionDisposition{Kind: projection.ItemFact, ItemID: "fact.power", Outcome: projection.ProjectionExact, SourceKeys: []semreg.FactKey{snapshot.Facts[0].Key, snapshot.Facts[0].Key}, Loss: []projection.LossDetail{}}
		raw, _ := json.Marshal(value)
		raw = bytes.TrimSuffix(raw, []byte("}"))
		raw = append(raw, []byte(`,"reason":7}`)...)
		_, err := semreg.Decode[projection.ProjectionDisposition](raw)
		correctionError(t, err, semreg.DuplicateKey)
	})
}

func TestCorrectionSnapshotBindingMatrix(t *testing.T) {
	snapshot, valid := sourceBoundReport(t)
	if _, err := projection.ValidateReport(snapshot, valid); err != nil {
		t.Fatalf("valid report control: %v", err)
	}
	if _, err := projection.Project(snapshot, valid.Manifest, []projection.RequestedItem{{Kind: projection.ItemOperation, ItemID: "item.source_free"}}, []projection.ProjectionDisposition{{Kind: projection.ItemOperation, ItemID: "item.source_free", Outcome: projection.ProjectionExact, SourceKeys: []semreg.FactKey{}, Loss: []projection.LossDetail{}}}, nil); err != nil {
		t.Fatalf("source-free accepted outcome: %v", err)
	}

	for _, mutation := range []struct {
		name string
		edit func(*semreg.FactKey)
	}{
		{"fact_id", func(key *semreg.FactKey) { key.FactID = "fact.absent" }},
		{"pack_version", func(key *semreg.FactKey) { key.PackVersion = "2.0.0" }},
		{"dimensions", func(key *semreg.FactKey) {
			key.Dimensions = []semreg.Dimension{{ID: "dimension.test", Value: semreg.Value{Kind: semreg.ValueBoolean, Boolean: boolPointer(true)}}}
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			report := valid
			report.Dispositions = append([]projection.ProjectionDisposition(nil), valid.Dispositions...)
			report.Dispositions[0].SourceKeys = append([]semreg.FactKey(nil), valid.Dispositions[0].SourceKeys...)
			mutation.edit(&report.Dispositions[0].SourceKeys[0])
			before, _ := json.Marshal(report)
			out, err := projection.Project(snapshot, report.Manifest, report.Requested, report.Dispositions, report.Causal)
			correctionError(t, err, semreg.DanglingReference)
			if !reflect.DeepEqual(out, projection.ProjectionReport{}) {
				t.Fatal("Project returned a partial report")
			}
			validated, err := projection.ValidateReport(snapshot, report)
			correctionError(t, err, semreg.DanglingReference)
			if !reflect.DeepEqual(validated, projection.ProjectionReport{}) {
				t.Fatal("ValidateReport returned a partial report")
			}
			after, _ := json.Marshal(report)
			if !bytes.Equal(before, after) {
				t.Fatal("rejection mutated input")
			}
		})
	}

	revision := valid
	revision.Revisions.Facts = "8"
	_, err := projection.ValidateReport(snapshot, revision)
	correctionError(t, err, semreg.RevisionConflict)
	identity := valid
	identity.SnapshotID = "snapshot:absent"
	_, err = projection.ValidateReport(snapshot, identity)
	correctionError(t, err, semreg.DanglingReference)
}

func deepResolvedInputs(t *testing.T) (semreg.Snapshot, projection.ProjectionReport) {
	t.Helper()
	snapshot := projectionSnapshot(t)
	resolved := snapshot.Facts[0].Key
	resolved.Dimensions = []semreg.Dimension{{ID: "dimension.test", Value: semreg.Value{Kind: semreg.ValueBoolean, Boolean: boolPointer(true)}}}
	snapshot.Facts[0].Key = resolved
	for index := range snapshot.Facts[0].Candidates {
		snapshot.Facts[0].Candidates[index].Key = resolved
	}
	sealProjectionSnapshot(t, &snapshot)
	canonical, err := semreg.CanonicalJSON(resolved)
	if err != nil {
		t.Fatal(err)
	}
	detachedKey, err := semreg.Decode[semreg.FactKey](canonical)
	if err != nil {
		t.Fatal(err)
	}
	reason := semreg.DefinitionID("reason.test")
	disposition := projection.ProjectionDisposition{
		Kind: projection.ItemFact, ItemID: "fact.power", Outcome: projection.ProjectionTransformed,
		SourceKeys: []semreg.FactKey{detachedKey}, Loss: []projection.LossDetail{{Kind: projection.LossPrecision, SourceItems: []semreg.DefinitionID{"fact.power"}, Description: "Precision reduced."}}, Reason: &reason,
	}
	causal := validCausal()
	report, err := projection.Project(snapshot, manifest(), []projection.RequestedItem{{Kind: disposition.Kind, ItemID: disposition.ItemID}}, []projection.ProjectionDisposition{disposition}, &causal)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, report
}

func TestCorrectionDeepDetachmentWithResolvedSources(t *testing.T) {
	for _, operation := range []string{"Project", "ValidateReport"} {
		t.Run(operation, func(t *testing.T) {
			snapshot, input := deepResolvedInputs(t)
			var output projection.ProjectionReport
			var err error
			if operation == "Project" {
				output, err = projection.Project(snapshot, input.Manifest, input.Requested, input.Dispositions, input.Causal)
			} else {
				output, err = projection.ValidateReport(snapshot, input)
			}
			if err != nil {
				t.Fatal(err)
			}
			outputBefore, _ := json.Marshal(output)
			snapshotBefore, _ := json.Marshal(snapshot)
			*input.Dispositions[0].SourceKeys[0].Dimensions[0].Value.Boolean = false
			input.Dispositions[0].Loss[0].SourceItems[0] = "fact.changed"
			*input.Dispositions[0].Reason = "reason.changed"
			input.Causal.Path[0] = "target:changed"
			input.Causal.Origin.Evidence[0].Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			if current, _ := json.Marshal(output); !bytes.Equal(outputBefore, current) {
				t.Fatal("output aliases input")
			}
			if current, _ := json.Marshal(snapshot); !bytes.Equal(snapshotBefore, current) {
				t.Fatal("operation mutated snapshot")
			}
			inputBefore, _ := json.Marshal(input)
			*output.Dispositions[0].SourceKeys[0].Dimensions[0].Value.Boolean = false
			output.Dispositions[0].Loss[0].Description = "Changed."
			output.Causal.Path[0] = "target:output-changed"
			if current, _ := json.Marshal(input); !bytes.Equal(inputBefore, current) {
				t.Fatal("input aliases output")
			}
		})
	}
}

type optionalCorrectionContract struct {
	Contract semreg.ContractVersion `json:"contract,omitempty"`
}

func (optionalCorrectionContract) Validate() error { return nil }
func (optionalCorrectionContract) ContractDiscriminator() (string, semreg.ContractVersion) {
	return "contract", projection.ContractProjectionV1
}

type phantomCorrectionContract struct{}

func (phantomCorrectionContract) Validate() error { return nil }
func (phantomCorrectionContract) ContractDiscriminator() (string, semreg.ContractVersion) {
	return "contract", projection.ContractProjectionV1
}

type invalidExpectedCorrectionContract struct {
	Contract semreg.ContractVersion `json:"contract"`
}

func (invalidExpectedCorrectionContract) Validate() error { return nil }
func (invalidExpectedCorrectionContract) ContractDiscriminator() (string, semreg.ContractVersion) {
	return "contract", "invalid"
}

type validCorrectionContract struct {
	Contract semreg.ContractVersion `json:"contract"`
}

func (validCorrectionContract) Validate() error { return nil }
func (validCorrectionContract) ContractDiscriminator() (string, semreg.ContractVersion) {
	return "contract", projection.ContractProjectionV1
}

func TestCorrectionDiscriminatorMetadata(t *testing.T) {
	for name, decode := range map[string]func() error{
		"optional": func() error { _, err := semreg.Decode[optionalCorrectionContract]([]byte(`{}`)); return err },
		"phantom":  func() error { _, err := semreg.Decode[phantomCorrectionContract]([]byte(`{}`)); return err },
		"invalid_expected": func() error {
			_, err := semreg.Decode[invalidExpectedCorrectionContract]([]byte(`{"contract":"helianthus.semantic.projection/v1"}`))
			return err
		},
	} {
		t.Run(name, func(t *testing.T) { correctionError(t, decode(), semreg.InvalidContract) })
	}
	decoded, err := semreg.Decode[validCorrectionContract]([]byte(`{"contract":"helianthus.semantic.projection/v1"}`))
	if err != nil || decoded.Contract != projection.ContractProjectionV1 {
		t.Fatalf("valid child discriminator control err=%v value=%+v", err, decoded)
	}
}
