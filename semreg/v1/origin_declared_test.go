package semreg

import (
	"encoding/json"
	"os"
	"testing"
)

type certificationOriginVector struct {
	ID        string   `json:"id"`
	Baseline  string   `json:"baseline"`
	Mutations []string `json:"mutations"`
	Expect    *string  `json:"expect"`
}

type certificationOriginDocument struct {
	Contract  string                      `json:"contract"`
	Mutations []string                    `json:"mutations"`
	Vectors   []certificationOriginVector `json:"vectors"`
}

func certificationEvidence(digit string) EvidenceRef {
	evidence := publicEvidence(digit)
	evidence.Kind = EvidenceKindCertification
	return evidence
}

func declaredCandidate() FactCandidate {
	candidate := validCandidate("declared", true)
	candidate.BindingID, candidate.SourceEpochID, candidate.DriverGeneration = nil, nil, nil
	candidate.Origin = OriginRef{OriginID: "origin:declared", Kind: OriginDeclared, Evidence: []EvidenceRef{certificationEvidence("3")}}
	candidate.Evidence = []EvidenceRef{certificationEvidence("3")}
	candidate.Quality.Assertion = AssertionInferred
	return candidate
}

func applyCertificationMutation(t *testing.T, candidate *FactCandidate, name string) {
	t.Helper()
	source, epoch, binding, generation := SourceID("source:x"), SourceEpochID("epoch:x"), NativeBindingID("binding:x"), Uint64("1")
	switch name {
	case "origin_source_id":
		candidate.Origin.SourceID = &source
	case "origin_source_epoch_id":
		candidate.Origin.SourceEpochID = &epoch
	case "origin_binding_id":
		candidate.Origin.BindingID = &binding
	case "candidate_binding_id":
		candidate.BindingID = &binding
	case "candidate_source_epoch_id":
		candidate.SourceEpochID = &epoch
	case "candidate_driver_generation":
		candidate.DriverGeneration = &generation
	case "candidate_derivation":
		candidate.Derivation = validInferredCandidate().Derivation
	case "origin_evidence_empty":
		candidate.Origin.Evidence = []EvidenceRef{}
	case "origin_evidence_without_certification":
		candidate.Origin.Evidence = []EvidenceRef{publicEvidence("4")}
	case "origin_evidence_certification":
		candidate.Origin.Evidence = []EvidenceRef{certificationEvidence("5")}
	case "candidate_evidence_certification":
		candidate.Evidence = []EvidenceRef{certificationEvidence("5")}
	default:
		t.Fatalf("unknown mutation %q", name)
	}
}

func TestCertificationOriginVectors(t *testing.T) {
	raw, err := os.ReadFile("../../fixtures/v1/certification-origin-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var document certificationOriginDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.Contract != "helianthus.semreg.certification-origin-vectors/v1" || len(document.Vectors) != 12 {
		t.Fatalf("document: %+v", document)
	}
	listed := map[string]bool{}
	for _, name := range document.Mutations {
		listed[name] = true
	}
	for _, vector := range document.Vectors {
		t.Run(vector.ID, func(t *testing.T) {
			var candidate FactCandidate
			switch vector.Baseline {
			case "declared":
				candidate = declaredCandidate()
			case "native_observation":
				candidate = validCandidate("native", true)
			default:
				t.Fatalf("baseline %q", vector.Baseline)
			}
			if err := candidate.Validate(); err != nil && vector.Baseline == "native_observation" {
				t.Fatalf("baseline: %v", err)
			}
			for _, name := range vector.Mutations {
				if !listed[name] {
					t.Fatalf("mutation %q is not listed", name)
				}
				applyCertificationMutation(t, &candidate, name)
			}
			err := candidate.Validate()
			if vector.Expect == nil {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil || string(ErrorIdentifier(err)) != *vector.Expect {
				t.Fatalf("want %s, got %v", *vector.Expect, err)
			}
		})
	}
}

func TestDeclaredOriginKindIsAccepted(t *testing.T) {
	origin := OriginRef{OriginID: "origin:declared", Kind: OriginDeclared, Evidence: []EvidenceRef{certificationEvidence("3")}}
	if err := origin.Validate(); err != nil {
		t.Fatal(err)
	}
	if OriginDeclared != "declared" {
		t.Fatal(OriginDeclared)
	}
}

func TestCertificationEvidenceKindIsNoGeneralSingleWordKind(t *testing.T) {
	evidence := publicEvidence("1")
	evidence.Kind = "specification"
	if evidence.Validate() == nil {
		t.Fatal("a single-word kind other than certification was accepted")
	}
}

func TestDeclaredCandidateDecodesFromWire(t *testing.T) {
	declared := declaredCandidate()
	raw, err := json.Marshal(declared)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeFactCandidate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin.Kind != OriginDeclared || got.BindingID != nil || got.Derivation != nil {
		t.Fatalf("decoded: %+v", got)
	}
	native := validCandidate("native", true)
	native.Evidence = []EvidenceRef{certificationEvidence("5")}
	raw, err = json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeFactCandidate(raw); err == nil {
		t.Fatal("a native candidate with certification evidence was decoded")
	}
}

func TestPublicationAcceptsADeclaredCandidateAndKeepsItsOrigin(t *testing.T) {
	for _, assertion := range []AssertionKind{AssertionInferred, AssertionObserved} {
		t.Run(string(assertion), func(t *testing.T) {
			kernel := newTestPublicationKernel(t, "asset:site")
			batch := publicationBatch("asset:site", "source:a", "epoch:a", "1", "1", "0")
			batch.SourceUpserts = []SourceDescriptor{publicationSource("source:a", "epoch:a")}
			batch.BindingUpserts = []NativeBinding{publicationBinding("asset:site", "source:a", "epoch:a", "binding:a", "1")}
			batch.IdentityLinkUpserts = []IdentityLink{publicationLink("asset:site", "binding:a")}
			candidate := declaredCandidate()
			candidate.CandidateID, candidate.Key.FactID = "candidate:declared", "fact.power"
			candidate.Quality.Assertion = assertion
			batch.FactUpserts = []FactCandidate{candidate}
			sealPublicationBatch(t, &batch)
			snapshot, _, err := kernel.Apply(batch, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if got := candidateByID(t, snapshot, "candidate:declared").Origin.Kind; got != OriginDeclared {
				t.Fatalf("stored origin kind %q", got)
			}
			if err := snapshot.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
