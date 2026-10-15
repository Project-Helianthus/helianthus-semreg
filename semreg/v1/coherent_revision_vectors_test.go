package semreg

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

type coherentBindingVector struct {
	BindingID NativeBindingID `json:"binding_id"`
	SourceID  SourceID        `json:"source_id"`
	Epoch     SourceEpochID   `json:"source_epoch_id"`
}

type coherentRevisionFixture struct {
	Contract string `json:"contract"`
	Source   struct {
		Contract ContractVersion `json:"contract"`
		Evidence string          `json:"evidence"`
		Status   string          `json:"status"`
	} `json:"source"`
	AssetID  AssetID                 `json:"asset_id"`
	Bindings []coherentBindingVector `json:"bindings"`
	Vectors  []struct {
		ID        string `json:"id"`
		Operation string `json:"operation"`
		Criterion string `json:"criterion"`
		Expect    struct {
			Revisions       RevisionVector `json:"revisions"`
			States          []BindingState `json:"states"`
			SnapshotErrorID ErrorID        `json:"snapshot_error_id"`
			ErrorID         ErrorID        `json:"error_id"`
		} `json:"expect"`
	} `json:"vectors"`
}

func loadCoherentRevisionVectors(t *testing.T) coherentRevisionFixture {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve coherent revision fixture")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "fixtures", "v1", "coherent-revision-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, duplicate, err := parseJSON(raw); err != nil || duplicate {
		t.Fatalf("non-strict fixture: duplicate=%v err=%v", duplicate, err)
	}
	var fixture coherentRevisionFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Contract != "helianthus.semantic.kernel.coherent-revision-fixtures/v1" || fixture.Source.Contract != ContractKernelV1 || fixture.Source.Evidence != "publication-vectors.json" || fixture.Source.Status != "synthetic" || len(fixture.Bindings) != 2 || len(fixture.Vectors) != 6 {
		t.Fatalf("unexpected coherent revision manifest: %+v", fixture)
	}
	if fixture.Bindings[0].BindingID >= fixture.Bindings[1].BindingID || fixture.Bindings[0].SourceID == fixture.Bindings[1].SourceID {
		t.Fatal("expected two ordered distinct binding perspectives and sources")
	}
	return fixture
}

func coherentRevisionSnapshots(t *testing.T, fixture coherentRevisionFixture, retireSecond bool) (*PublicationKernel, Snapshot, Snapshot) {
	t.Helper()
	kernel := newTestPublicationKernel(t, fixture.AssetID)
	var before Snapshot
	for i, index := range []int{1, 0} {
		binding := fixture.Bindings[index]
		expected := Uint64("0")
		if i == 1 {
			expected = "1"
		}
		batch := publicationBatch(fixture.AssetID, binding.SourceID, binding.Epoch, "1", "1", expected)
		batch.SourceUpserts = []SourceDescriptor{publicationSource(binding.SourceID, binding.Epoch)}
		batch.BindingUpserts = []NativeBinding{publicationBinding(fixture.AssetID, binding.SourceID, binding.Epoch, binding.BindingID, "1")}
		link := publicationLink(fixture.AssetID, binding.BindingID)
		link.State = LinkCandidate
		batch.IdentityLinkUpserts = []IdentityLink{link}
		sealPublicationBatch(t, &batch)
		var err error
		before, _, err = kernel.Apply(batch, publicationMonotonic)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !retireSecond {
		return kernel, before, before
	}
	binding := fixture.Bindings[1]
	retire := publicationBatch(fixture.AssetID, binding.SourceID, binding.Epoch, "1", "2", before.Revisions.Semantic)
	retire.SourceRetirements = []SourceEpochID{binding.Epoch}
	sealPublicationBatch(t, &retire)
	after, _, err := kernel.Apply(retire, publicationMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	return kernel, before, after
}

func TestCoherentTwoBindingRevisionVectors(t *testing.T) {
	fixture := loadCoherentRevisionVectors(t)
	kernel, before, after := coherentRevisionSnapshots(t, fixture, true)
	_, repeatBefore, repeatAfter := coherentRevisionSnapshots(t, fixture, true)
	if !bytes.Equal(mustCanonical(t, before), mustCanonical(t, repeatBefore)) || !bytes.Equal(mustCanonical(t, after), mustCanonical(t, repeatAfter)) {
		t.Fatal("publication is not deterministic")
	}
	wantOperations := []string{"snapshot", "retire_second_binding", "later_revision_binding", "foreign_asset", "missing_binding", "duplicate_binding"}
	wantErrors := []ErrorID{"", "", RevisionConflict, DanglingReference, DanglingReference, DuplicateKey}
	wantIDs := []string{"S1C-POS-001", "S1C-POS-002", "S1C-NEG-001", "S1C-NEG-002", "S1C-NEG-003", "S1C-NEG-004"}
	for i, vector := range fixture.Vectors {
		if vector.ID != wantIDs[i] || vector.Operation != wantOperations[i] || vector.Expect.ErrorID != wantErrors[i] || vector.Criterion == "" {
			t.Fatalf("unexpected vector %d: %+v", i, vector)
		}
		t.Run(vector.ID, func(t *testing.T) {
			switch vector.Operation {
			case "snapshot", "retire_second_binding":
				snapshot := before
				if vector.Operation == "retire_second_binding" {
					snapshot = after
				}
				if vector.Expect.ErrorID != "" || len(vector.Expect.States) != 2 || snapshot.Revisions != vector.Expect.Revisions || len(snapshot.Bindings) != 2 {
					t.Fatalf("snapshot expectation: %+v", snapshot)
				}
				if err := snapshot.Validate(); err != nil {
					t.Fatal(err)
				}
				raw := mustCanonical(t, snapshot)
				decoded, err := Decode[Snapshot](raw)
				if err != nil || !bytes.Equal(raw, mustCanonical(t, decoded)) {
					t.Fatalf("canonical round trip: %v", err)
				}
				for j, binding := range snapshot.Bindings {
					if binding.BindingID != fixture.Bindings[j].BindingID || binding.AssetID != fixture.AssetID || binding.State != vector.Expect.States[j] || snapshot.IdentityLinks[j].State == LinkQualified {
						t.Fatalf("binding order/lifecycle/qualification: %+v", binding)
					}
				}
			case "later_revision_binding":
				// Object revisions are local counters, not semantic revision timestamps.
				// A future object revision must fail the existing upsert successor rule.
				freshKernel, coherent, _ := coherentRevisionSnapshots(t, fixture, false)
				binding := fixture.Bindings[1]
				future := publicationBatch(fixture.AssetID, binding.SourceID, binding.Epoch, "1", "2", coherent.Revisions.Semantic)
				future.BindingUpserts = []NativeBinding{coherent.Bindings[1]}
				future.BindingUpserts[0].Revision = "3"
				sealPublicationBatch(t, &future)
				assertRejectedUnchanged(t, freshKernel, future, vector.Expect.ErrorID)
				if vector.Expect.SnapshotErrorID != DanglingReference {
					t.Fatal("missing future snapshot graft expectation")
				}
				// The later tombstone cannot be spliced into the earlier lifecycle.
				invalid := cloneSnapshot(before)
				invalid.Bindings[1] = after.Bindings[1]
				recomputeSnapshotID(t, &invalid)
				requireID(t, invalid.Validate(), vector.Expect.SnapshotErrorID)
				encoded, err := json.Marshal(invalid)
				if err != nil {
					t.Fatal(err)
				}
				_, err = Decode[Snapshot](encoded)
				requireID(t, err, vector.Expect.SnapshotErrorID)
			case "foreign_asset", "missing_binding", "duplicate_binding":
				invalid := cloneSnapshot(before)
				switch vector.Operation {
				case "foreign_asset":
					invalid.Bindings[1].AssetID = "asset:synthetic:foreign"
				case "missing_binding":
					invalid.Bindings = invalid.Bindings[:1]
				case "duplicate_binding":
					invalid.Bindings = append(invalid.Bindings, invalid.Bindings[1])
				}
				// Reseal to exercise reference/order rejection rather than stale digest rejection.
				recomputeSnapshotID(t, &invalid)
				requireID(t, invalid.Validate(), vector.Expect.ErrorID)
				raw, err := json.Marshal(invalid)
				if err != nil {
					t.Fatal(err)
				}
				_, err = Decode[Snapshot](raw)
				requireID(t, err, vector.Expect.ErrorID)
			default:
				t.Fatalf("unhandled operation %q", vector.Operation)
			}
		})
	}
	// Normalize exactly the lifecycle closure of the second perspective; every
	// other byte must be retained, including the first binding and its lineage.
	normalized := cloneSnapshot(after)
	normalized.SnapshotID, normalized.Revisions = before.SnapshotID, before.Revisions
	normalized.EvaluatedAt = before.EvaluatedAt
	normalized.Sources[1].State, normalized.Sources[1].Revision = before.Sources[1].State, before.Sources[1].Revision
	normalized.Bindings[1].State, normalized.Bindings[1].Revision = before.Bindings[1].State, before.Bindings[1].Revision
	normalized.IdentityLinks[1].State, normalized.IdentityLinks[1].Revision = before.IdentityLinks[1].State, before.IdentityLinks[1].Revision
	normalized.Cursors[1].LastSequence, normalized.Cursors[1].LastBatchDigest = before.Cursors[1].LastSequence, before.Cursors[1].LastBatchDigest
	if !reflect.DeepEqual(normalized, before) {
		t.Fatal("later snapshot changed unrelated content")
	}
	if before.SnapshotID == after.SnapshotID || after.Cursors[1].LastSequence != "2" || after.Bindings[1].Revision != "2" || before.Bindings[1].Revision != "1" || after.Sources[1].State != SourceRetired || after.IdentityLinks[1].State != LinkWithdrawn {
		t.Fatal("retirement did not retain its expected tombstones")
	}
	current, raw, ok := kernel.Current()
	if !ok || !bytes.Equal(raw, mustCanonical(t, after)) || !reflect.DeepEqual(current, after) {
		t.Fatal("negative validation changed last known good publication")
	}
	if err := before.Validate(); err != nil {
		t.Fatalf("prior coherent snapshot changed: %v", err)
	}
}
