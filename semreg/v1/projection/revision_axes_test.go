package projection_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

func revisionAxesJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func revisionAxesFixture(t *testing.T) (semreg.Snapshot, projection.ProjectionReport) {
	t.Helper()
	// Entirely synthetic: one source, binding, candidate and referenced fact.
	digest := sha256.Sum256([]byte("synthetic projection revision axes evidence"))
	evidence := semreg.EvidenceRef{
		Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest(fmt.Sprintf("sha256:%x", digest)),
		Contract: "test.contract/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone,
	}
	source := semreg.SourceID("source:test")
	epoch := semreg.SourceEpochID("epoch:test")
	binding := semreg.NativeBindingID("binding:test")
	generation := semreg.Uint64("1")
	point := semreg.TimePoint{UnixNanoseconds: "1", ClockID: "clock.test", UncertaintyNS: "0"}
	monotonic := semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: "1"}
	key := semreg.FactKey{PackID: "pack.test", PackVersion: "1.0.0", FactID: "fact.test", Dimensions: []semreg.Dimension{}}
	value := true
	snapshot := semreg.Snapshot{
		Contract: semreg.ContractKernelV1, AssetID: "asset:test",
		Revisions:   semreg.RevisionVector{Semantic: "2", Identity: "3", Facts: "4", Services: "5", Capabilities: "6"},
		EvaluatedAt: point, EvaluateMonotonic: monotonic,
		Sources: []semreg.SourceDescriptor{{
			SourceID: source, SourceEpochID: epoch, ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "test-1",
			RegistryEvidence: evidence, StartedAt: point, State: semreg.SourceCurrent, Revision: "1",
		}},
		Bindings: []semreg.NativeBinding{{
			BindingID: binding, AssetID: "asset:test", SourceID: source, SourceEpochID: epoch, DriverGeneration: generation,
			NativeResource: evidence, State: semreg.BindingCurrent, Revision: "1",
		}},
		IdentityLinks: []semreg.IdentityLink{},
		Facts: []semreg.FactEnvelope{{
			AssetID: "asset:test", Key: key, Revision: "1", Conflicts: []semreg.Conflict{},
			Candidates: []semreg.FactCandidate{{
				CandidateID: "candidate:test", Key: key, Value: &semreg.Value{Kind: semreg.ValueBoolean, Boolean: &value},
				Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified,
					Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable,
					Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}},
				Times:           semreg.Times{ReceivedAt: point, ReceiptMonotonic: monotonic, EvaluatedAt: point, EvaluateMonotonic: monotonic},
				FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:test", Version: "1.0.0", FreshForNS: "1", RetainForNS: "2", MaxWallUncertaintyNS: "0"},
				BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
				Origin: semreg.OriginRef{OriginID: "origin:test", Kind: semreg.OriginNativeObservation,
					SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []semreg.EvidenceRef{evidence}},
				Evidence: []semreg.EvidenceRef{evidence}, Revision: "1",
			}},
		}},
		Services: []semreg.ServiceInstance{}, Capabilities: []semreg.CapabilityInstance{}, Fences: []semreg.GenerationFence{},
		Cursors: []semreg.PublicationCursor{{SourceID: source, SourceEpochID: epoch, DriverGeneration: generation,
			LastSequence: "1", LastBatchDigest: evidence.Digest}},
	}
	var document map[string]any
	if err := json.Unmarshal(revisionAxesJSON(t, snapshot), &document); err != nil {
		t.Fatal(err)
	}
	delete(document, "snapshot_id")
	snapshotDigest := sha256.Sum256(revisionAxesJSON(t, document))
	snapshot.SnapshotID = semreg.SnapshotID(fmt.Sprintf("sha256:%x", snapshotDigest))
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("synthetic snapshot: %v", err)
	}
	report := projection.ProjectionReport{
		Contract: projection.ContractProjectionV1, SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions,
		Manifest: projection.ProjectionManifest{TargetID: "target:test", TargetVersion: "test-1", KernelVersion: semreg.ContractKernelV1,
			PackVersions: []semreg.PackRef{{ID: "pack.test", Version: "1.0.0"}}, MappingRevision: "1"},
		Requested: []projection.RequestedItem{{Kind: projection.ItemFact, ItemID: "fact.test"}},
		Dispositions: []projection.ProjectionDisposition{{Kind: projection.ItemFact, ItemID: "fact.test", Outcome: projection.ProjectionExact,
			SourceKeys: []semreg.FactKey{key}, Loss: []projection.LossDetail{}}},
	}
	return snapshot, report
}

func TestProjectionRevisionAxes(t *testing.T) {
	snapshot, valid := revisionAxesFixture(t)
	validBefore := revisionAxesJSON(t, valid)
	snapshotBefore := revisionAxesJSON(t, snapshot)
	for _, test := range []struct {
		name string
		edit func(*projection.ProjectionReport)
		want semreg.ErrorID
	}{
		{name: "matching"},
		{"semantic", func(r *projection.ProjectionReport) { r.Revisions.Semantic = "7" }, semreg.RevisionConflict},
		{"identity", func(r *projection.ProjectionReport) { r.Revisions.Identity = "7" }, semreg.RevisionConflict},
		{"facts", func(r *projection.ProjectionReport) { r.Revisions.Facts = "7" }, semreg.RevisionConflict},
		{"services", func(r *projection.ProjectionReport) { r.Revisions.Services = "7" }, semreg.RevisionConflict},
		{"capabilities", func(r *projection.ProjectionReport) { r.Revisions.Capabilities = "7" }, semreg.RevisionConflict},
		{"snapshot_id_only", func(r *projection.ProjectionReport) { r.SnapshotID = "snapshot:other" }, semreg.DanglingReference},
	} {
		t.Run(test.name, func(t *testing.T) {
			var report projection.ProjectionReport
			if err := json.Unmarshal(validBefore, &report); err != nil {
				t.Fatal(err)
			}
			if test.edit != nil {
				test.edit(&report)
			}
			if err := report.Validate(); err != nil {
				t.Fatalf("case must retain a valid report: %v", err)
			}
			before := revisionAxesJSON(t, report)
			out, err := projection.ValidateReport(snapshot, report)
			if !bytes.Equal(before, revisionAxesJSON(t, report)) || !bytes.Equal(snapshotBefore, revisionAxesJSON(t, snapshot)) {
				t.Fatal("ValidateReport mutated input")
			}
			if !bytes.Equal(validBefore, revisionAxesJSON(t, valid)) {
				t.Fatal("case aliases the original report")
			}
			if test.want == "" {
				if err != nil {
					t.Fatalf("matching report: %v", err)
				}
				if !reflect.DeepEqual(out, report) {
					t.Fatal("matching report changed")
				}
				return
			}
			if err == nil || semreg.ErrorIdentifier(err) != test.want {
				t.Errorf("error=%v identifier=%s; want %s", err, semreg.ErrorIdentifier(err), test.want)
			}
			if !reflect.DeepEqual(out, projection.ProjectionReport{}) {
				t.Error("rejection returned a nonzero report")
			}
		})
	}
}
