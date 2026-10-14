package operation_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// capabilityAdmissionVector is one CapabilityInstance "admit" vector copied from
// helianthus-docs-semantic@b16667d719defc7b0fef0400ee3ad387469018ac.
type capabilityAdmissionVector struct {
	ID         string `json:"id"`
	RecordType string `json:"record_type"`
	Operation  string `json:"operation"`
	Input      struct {
		Qualification       semreg.Qualification `json:"qualification"`
		Availability        semreg.Availability  `json:"availability"`
		ActivationEvidence  *[]json.RawMessage   `json:"activation_evidence"`
		AllowDegraded       *bool                `json:"allow_degraded"`
		BindingGenCurrent   *bool                `json:"binding_and_generation_current"`
		OwnerRegistered     *bool                `json:"definition_owner_registered"`
		AllOtherFieldsValid *bool                `json:"all_other_fields_valid"`
	} `json:"input"`
	Expect struct {
		Result         string         `json:"result"`
		ErrorID        semreg.ErrorID `json:"error_id"`
		StateUnchanged bool           `json:"state_unchanged"`
	} `json:"expect"`
}

func loadCapabilityAdmissionVectors(t *testing.T) map[string]capabilityAdmissionVector {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve foundation fixture")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "fixtures", "v1", "foundation-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Contract string `json:"contract"`
		Source   struct {
			Repository string `json:"repository"`
			Commit     string `json:"commit"`
			Path       string `json:"path"`
		} `json:"source"`
		Vectors []capabilityAdmissionVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.Contract != "helianthus.semantic.kernel.acceptance/v1" || document.Source.Repository != "Project-Helianthus/helianthus-docs-semantic" ||
		document.Source.Commit != "b16667d719defc7b0fef0400ee3ad387469018ac" || document.Source.Path != "api/v1/acceptance-vectors.json" {
		t.Fatalf("unexpected foundation fixture manifest: %+v", document.Source)
	}
	result := make(map[string]capabilityAdmissionVector)
	for _, vector := range document.Vectors {
		result[vector.ID] = vector
	}
	return result
}

func requireVector(t *testing.T, vectors map[string]capabilityAdmissionVector, id string, want semreg.ErrorID) capabilityAdmissionVector {
	t.Helper()
	vector, ok := vectors[id]
	if !ok {
		t.Fatalf("missing pinned vector %s", id)
	}
	if vector.RecordType != "CapabilityInstance" || vector.Operation != "admit" || vector.Expect.Result != "reject" ||
		vector.Expect.ErrorID != want || !vector.Expect.StateUnchanged {
		t.Fatalf("pinned vector %s changed: %+v", id, vector)
	}
	return vector
}

// vectorCapabilityBatch publishes a second capability instance on top of the
// fixture snapshot with the vector's qualification, availability and evidence.
func vectorCapabilityBatch(t *testing.T, fixture operationFixture, vector capabilityAdmissionVector, evidenceCount int) semreg.PublicationBatch {
	t.Helper()
	capability := fixture.snapshot.Capabilities[0]
	capability.InstanceID = "capability:vector"
	capability.Qualification, capability.Availability = vector.Input.Qualification, vector.Input.Availability
	capability.ActivationEvidence = []semreg.EvidenceRef{}
	for i := 0; i < evidenceCount; i++ {
		capability.ActivationEvidence = append(capability.ActivationEvidence, evidence(byte(10+i)))
	}
	capability.Revision = "1"
	batch := semreg.PublicationBatch{
		Contract: semreg.ContractKernelV1, BatchID: "batch:vector", AssetID: "asset:test", SourceID: "source:test", SourceEpochID: "epoch:test:1",
		DriverGeneration: "1", Sequence: "2", ExpectedSemanticRevision: fixture.snapshot.Revisions.Semantic, ObservedAt: timePoint("120"),
		SourceUpserts: []semreg.SourceDescriptor{}, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: []semreg.NativeBinding{},
		IdentityLinkUpserts: []semreg.IdentityLink{}, FactUpserts: []semreg.FactCandidate{}, FactWithdrawals: []semreg.CandidateID{},
		ServiceUpserts: []semreg.ServiceInstance{}, ServiceWithdrawals: []semreg.ServiceInstanceID{},
		CapabilityUpserts: []semreg.CapabilityInstance{capability}, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{},
	}
	// ComputedDigest validates the batch, so seal over the canonical bytes
	// without the digest member to cover invalid (empty-proof) batches too.
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&members); err != nil {
		t.Fatal(err)
	}
	delete(members, "batch_digest")
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(members); err != nil {
		t.Fatal(err)
	}
	canonicalBytes := bytes.TrimSuffix(canonical.Bytes(), []byte("\n"))
	batch.BatchDigest = semreg.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(canonicalBytes)))
	return batch
}

var vectorMonotonic = semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: "120"}

// requireAdmissionRejectedUnchanged applies the batch and requires the exact
// stable error with byte-identical current publication state afterwards.
func requireAdmissionRejectedUnchanged(t *testing.T, fixture operationFixture, batch semreg.PublicationBatch, want semreg.ErrorID) {
	t.Helper()
	beforeSnapshot, beforeBytes, beforeOK := fixture.publication.Current()
	_, _, err := fixture.publication.Apply(batch, vectorMonotonic)
	errorID(t, err, want)
	afterSnapshot, afterBytes, afterOK := fixture.publication.Current()
	if beforeOK != afterOK || !reflect.DeepEqual(beforeSnapshot, afterSnapshot) || !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatalf("%s rejection mutated publication state", want)
	}
}

func TestCapabilityAdmissionFoundationVectors(t *testing.T) {
	vectors := loadCapabilityAdmissionVectors(t)

	t.Run("K-NEG-014", func(t *testing.T) {
		vector := requireVector(t, vectors, "K-NEG-014", semreg.CapabilityNotQualified)
		if vector.Input.Qualification != semreg.QualificationCandidate || vector.Input.Availability != semreg.AvailabilityAvailable ||
			vector.Input.ActivationEvidence == nil || len(*vector.Input.ActivationEvidence) != 0 {
			t.Fatal("pinned K-NEG-014 input changed")
		}
		// The exact vector input cannot enter a snapshot.
		fixture := newOperationFixture(t)
		requireAdmissionRejectedUnchanged(t, fixture, vectorCapabilityBatch(t, fixture, vector, 0), semreg.CapabilityNotQualified)
		// A published candidate capability, even with activation evidence, is
		// not an actionable route.
		candidate := newOperationFixtureWith(t, func(b *semreg.PublicationBatch) {
			b.CapabilityUpserts[0].Qualification = vector.Input.Qualification
			b.CapabilityUpserts[0].Availability = vector.Input.Availability
		})
		_, err := candidate.kernel.Admit(candidate.snapshot, candidate.current, candidate.intent, operation.AuthorityResolverFunc(authorize))
		errorID(t, err, semreg.CapabilityNotQualified)
		// The same intent still admits once the capability is qualified, so no
		// idempotency entry was retained by the rejection.
		mustAdmit(t, newOperationFixture(t))
	})

	t.Run("K-NEG-015", func(t *testing.T) {
		vector := requireVector(t, vectors, "K-NEG-015", semreg.CapabilityUnavailable)
		if vector.Input.Qualification != semreg.QualificationQualified || vector.Input.Availability != semreg.AvailabilityDegraded ||
			vector.Input.AllowDegraded == nil || *vector.Input.AllowDegraded {
			t.Fatal("pinned K-NEG-015 input changed")
		}
		fixture := newOperationFixtureWith(t, func(b *semreg.PublicationBatch) {
			b.CapabilityUpserts[0].Qualification = vector.Input.Qualification
			b.CapabilityUpserts[0].Availability = vector.Input.Availability
		})
		fixture.intent.RequiredCapability.AllowDegraded = *vector.Input.AllowDegraded
		before := fixture.snapshot
		_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
		errorID(t, err, semreg.CapabilityUnavailable)
		if !reflect.DeepEqual(before, fixture.snapshot) {
			t.Fatal("rejection mutated the admitted snapshot")
		}
		// An explicit permitting requirement under the same idempotency key
		// admits; a retained entry from the rejection would conflict instead.
		fixture.intent.RequiredCapability.AllowDegraded = true
		mustAdmit(t, fixture)
	})

	t.Run("K-NEG-048", func(t *testing.T) {
		vector := requireVector(t, vectors, "K-NEG-048", semreg.CapabilityNotQualified)
		in := vector.Input
		if in.Qualification != semreg.QualificationQualified || in.Availability != semreg.AvailabilityAvailable ||
			in.ActivationEvidence == nil || len(*in.ActivationEvidence) != 0 ||
			in.BindingGenCurrent == nil || !*in.BindingGenCurrent || in.OwnerRegistered == nil || !*in.OwnerRegistered ||
			in.AllOtherFieldsValid == nil || !*in.AllOtherFieldsValid {
			t.Fatal("pinned K-NEG-048 input changed")
		}
		// Control: with non-empty activation proof the same record is valid, so
		// the empty proof is the only reason for rejection.
		control := newOperationFixture(t)
		if _, _, err := control.publication.Apply(vectorCapabilityBatch(t, control, vector, 1), vectorMonotonic); err != nil {
			t.Fatalf("control record with activation proof rejected: %v", err)
		}
		fixture := newOperationFixture(t)
		requireAdmissionRejectedUnchanged(t, fixture, vectorCapabilityBatch(t, fixture, vector, 0), semreg.CapabilityNotQualified)
	})
}
