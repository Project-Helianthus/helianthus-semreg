package semreg

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const correctedVectorSourceSHA256 = "51a177ce08e478cd9b003c213813abcf6e969ce1dc4cff4b276f7a2d480344f8"

type evaluationSelectionVector struct {
	ID     string          `json:"id"`
	Input  json.RawMessage `json:"input"`
	Expect struct {
		ErrorID         ErrorID   `json:"error_id"`
		Selection       Selection `json:"selection"`
		RepeatSelection Selection `json:"repeat_selection"`
	} `json:"expect"`
}

func loadEvaluationSelectionVectors(t *testing.T) map[string]evaluationSelectionVector {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve evaluation/selection fixture")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "fixtures", "v1", "evaluation-selection-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, duplicate, err := parseJSON(raw); err != nil || duplicate {
		t.Fatalf("fixture JSON is not strict: duplicate=%v err=%v", duplicate, err)
	}
	var document struct {
		Contract ContractVersion `json:"contract"`
		Source   struct {
			Repository      string `json:"repository"`
			Commit          string `json:"commit"`
			CorrectionIssue int    `json:"correction_issue"`
			Path            string `json:"path"`
			SHA256          string `json:"sha256"`
		} `json:"source"`
		Vectors []evaluationSelectionVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.Contract != "helianthus.semantic.kernel.acceptance/v1" ||
		document.Source.Repository != "Project-Helianthus/helianthus-docs-semantic" ||
		document.Source.Commit != "da5ab4415d3bec73f9572aec1c495a6cdcbcba47" ||
		document.Source.CorrectionIssue != 6 || document.Source.Path != "api/v1/acceptance-vectors.json" ||
		document.Source.SHA256 != correctedVectorSourceSHA256 || len(document.Vectors) != 12 {
		t.Fatalf("unexpected corrected fixture manifest: %+v vectors=%d", document.Source, len(document.Vectors))
	}
	vectors := make(map[string]evaluationSelectionVector, len(document.Vectors))
	for _, vector := range document.Vectors {
		if _, exists := vectors[vector.ID]; exists {
			t.Fatalf("duplicate vector %s", vector.ID)
		}
		vectors[vector.ID] = vector
	}
	return vectors
}

func TestExactCorrectedKPOS019(t *testing.T) {
	vector := loadEvaluationSelectionVectors(t)["K-POS-019"]
	var input struct {
		Snapshot    Snapshot    `json:"snapshot"`
		CandidateID CandidateID `json:"candidate_id"`
		Contexts    []struct {
			Context                  EvaluationContext `json:"context"`
			ExpectedFreshness        Freshness         `json:"expected_freshness"`
			ExpectedEvaluationDigest Digest            `json:"expected_evaluation_digest"`
		} `json:"contexts"`
	}
	if err := json.Unmarshal(vector.Input, &input); err != nil {
		t.Fatal(err)
	}
	if err := input.Snapshot.Validate(); err != nil {
		t.Fatalf("exact K-POS-019 snapshot: %v", err)
	}
	before, err := CanonicalJSON(input.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	seenDigests := make(map[Digest]struct{}, len(input.Contexts))
	for _, item := range input.Contexts {
		view, err := EvaluateSnapshot(input.Snapshot, item.Context)
		if err != nil {
			t.Fatal(err)
		}
		result := factResult(t, view, input.CandidateID)
		if result.Freshness != item.ExpectedFreshness || view.SnapshotID != input.Snapshot.SnapshotID || view.Revisions != input.Snapshot.Revisions || view.EvaluationDigest != item.ExpectedEvaluationDigest {
			t.Fatalf("exact K-POS-019 result: %+v", result)
		}
		seenDigests[view.EvaluationDigest] = struct{}{}
	}
	if len(input.Contexts) != 3 || len(seenDigests) != len(input.Contexts) {
		t.Fatalf("exact K-POS-019 contexts/digests: %d/%d", len(input.Contexts), len(seenDigests))
	}
	after, _ := CanonicalJSON(input.Snapshot)
	if !bytes.Equal(before, after) {
		t.Fatal("exact K-POS-019 mutated snapshot")
	}
}

type exactSelectionInput struct {
	Snapshot Snapshot       `json:"snapshot"`
	View     EvaluationView `json:"evaluation_view"`
	Key      FactKey        `json:"requested_key"`
	Policy   struct {
		ID      PolicyID        `json:"policy_id"`
		Version SemanticVersion `json:"policy_version"`
	} `json:"policy"`
}

func decodeExactSelectionInput(t *testing.T, raw json.RawMessage) exactSelectionInput {
	t.Helper()
	var input exactSelectionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	return input
}

func TestExactCorrectedKPOS025(t *testing.T) {
	vector := loadEvaluationSelectionVectors(t)["K-POS-025"]
	input := decodeExactSelectionInput(t, vector.Input)
	policy := &recordingSelectionPolicy{id: input.Policy.ID, version: input.Policy.Version, chosen: vector.Expect.Selection.SelectedCandidate}
	kernel, err := NewSelectionKernel(policy)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := kernel.SelectPresentation(input.Snapshot, input.View, input.Key, input.Policy.ID, input.Policy.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selection, vector.Expect.Selection) || !reflect.DeepEqual(selection, vector.Expect.RepeatSelection) {
		t.Fatalf("exact K-POS-025 selection differs: %+v", selection)
	}
	if policy.Calls() != 1 {
		t.Fatalf("exact K-POS-025 callbacks=%d", policy.Calls())
	}
}

type selectionMutation struct {
	Path              string          `json:"path"`
	Replacement       json.RawMessage `json:"replacement"`
	RemoveCandidateID CandidateID     `json:"remove_candidate_id"`
}

func TestExactCorrectedSelectionNegatives(t *testing.T) {
	vectors := loadEvaluationSelectionVectors(t)
	baseVector := vectors["K-POS-025"]
	for number := 56; number <= 65; number++ {
		id := "K-NEG-0" + string(rune('0'+number/10)) + string(rune('0'+number%10))
		vector := vectors[id]
		t.Run(id, func(t *testing.T) {
			input := decodeExactSelectionInput(t, baseVector.Input)
			expected := baseVector.Expect.Selection
			var descriptor struct {
				BaseVector        string            `json:"base_vector"`
				Mutation          selectionMutation `json:"mutation"`
				PolicyID          PolicyID          `json:"policy_id"`
				PolicyVersion     SemanticVersion   `json:"policy_version"`
				RegistrationCount int               `json:"registration_count"`
				PolicyReturn      struct {
					CandidateID CandidateID `json:"candidate_id"`
				} `json:"policy_return"`
			}
			if err := json.Unmarshal(vector.Input, &descriptor); err != nil {
				t.Fatal(err)
			}
			if number != 64 && descriptor.BaseVector != "K-POS-025" {
				t.Fatalf("%s does not resolve exact K-POS-025", id)
			}
			if number == 64 {
				policy := &recordingSelectionPolicy{id: descriptor.PolicyID, version: descriptor.PolicyVersion}
				kernel, err := NewSelectionKernel(policy)
				if err != nil {
					t.Fatal(err)
				}
				requireID(t, kernel.RegisterSelectionPolicy(policy), vector.Expect.ErrorID)
				return
			}

			chosen := expected.SelectedCandidate
			if number == 56 {
				chosen = descriptor.PolicyReturn.CandidateID
			}
			policy := &recordingSelectionPolicy{id: input.Policy.ID, version: input.Policy.Version, chosen: chosen}
			kernel, err := NewSelectionKernel(policy)
			if err != nil {
				t.Fatal(err)
			}
			selectPolicyID, selectPolicyVersion := policy.id, policy.version
			switch number {
			case 57:
				if descriptor.Mutation.Path != "evaluation_view.snapshot_id" || json.Unmarshal(descriptor.Mutation.Replacement, &input.View.SnapshotID) != nil {
					t.Fatal("invalid exact K-NEG-057 mutation")
				}
				sealEvaluationView(t, &input.View)
			case 58:
				if descriptor.Mutation.Path != "evaluation_view.revisions.facts" || json.Unmarshal(descriptor.Mutation.Replacement, &input.View.Revisions.Facts) != nil {
					t.Fatal("invalid exact K-NEG-058 mutation")
				}
				sealEvaluationView(t, &input.View)
			case 59:
				if descriptor.Mutation.Path != "evaluation_view.evaluation_digest" || json.Unmarshal(descriptor.Mutation.Replacement, &input.View.EvaluationDigest) != nil {
					t.Fatal("invalid exact K-NEG-059 mutation")
				}
			case 60:
				if descriptor.Mutation.Path != "requested_key" || json.Unmarshal(descriptor.Mutation.Replacement, &input.Key) != nil {
					t.Fatal("invalid exact K-NEG-060 mutation")
				}
			case 61:
				if descriptor.Mutation.Path != "evaluation_view.facts" {
					t.Fatal("invalid exact K-NEG-061 mutation")
				}
				retained := input.View.Facts[:0]
				for _, fact := range input.View.Facts {
					if fact.CandidateID != descriptor.Mutation.RemoveCandidateID {
						retained = append(retained, fact)
					}
				}
				input.View.Facts = retained
				sealEvaluationView(t, &input.View)
			case 62:
				if descriptor.Mutation.Path != "selection.context.evaluate_monotonic.nanoseconds" || json.Unmarshal(descriptor.Mutation.Replacement, &expected.Context.EvaluateMonotonic.Nanoseconds) != nil {
					t.Fatal("invalid exact K-NEG-062 mutation")
				}
				requireID(t, ValidateSelection(input.Snapshot, input.View, expected), vector.Expect.ErrorID)
				return
			case 63:
				selectPolicyID, selectPolicyVersion = descriptor.PolicyID, descriptor.PolicyVersion
			case 65:
				path := descriptor.Mutation.Path
				start, end := strings.IndexByte(path, '['), strings.IndexByte(path, ']')
				if start < 0 || end <= start {
					t.Fatal("invalid exact K-NEG-065 path")
				}
				candidateID := CandidateID(path[start+1 : end])
				var replacement Uint64
				if json.Unmarshal(descriptor.Mutation.Replacement, &replacement) != nil {
					t.Fatal("invalid exact K-NEG-065 replacement")
				}
				for index := range input.View.Facts {
					if input.View.Facts[index].CandidateID == candidateID {
						input.View.Facts[index].CandidateRevision = replacement
					}
				}
				sealEvaluationView(t, &input.View)
			}
			beforeSnapshot, _ := json.Marshal(input.Snapshot)
			beforeView, _ := json.Marshal(input.View)
			_, err = kernel.SelectPresentation(input.Snapshot, input.View, input.Key, selectPolicyID, selectPolicyVersion)
			requireID(t, err, vector.Expect.ErrorID)
			wantCalls := 0
			if number == 56 {
				wantCalls = 1
			}
			if policy.Calls() != wantCalls {
				t.Fatalf("%s callbacks=%d want=%d", id, policy.Calls(), wantCalls)
			}
			afterSnapshot, _ := json.Marshal(input.Snapshot)
			afterView, _ := json.Marshal(input.View)
			if !bytes.Equal(beforeSnapshot, afterSnapshot) || !bytes.Equal(beforeView, afterView) {
				t.Fatalf("%s mutated exact input", id)
			}
		})
	}
}
