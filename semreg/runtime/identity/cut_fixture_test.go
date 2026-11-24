package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// asset is the published content of one member of a cut in the tests: resources
// and relation instances of one asset with a single current source and binding.
type asset struct {
	id        semreg.AssetID
	resources []semreg.Resource
	relations []semreg.RelationInstance
}

type jsonObject map[string]any

func (jsonObject) Validate() error { return nil }

func operatorOrigin(label string) semreg.OriginRef {
	return semreg.OriginRef{OriginID: semreg.OriginID("origin:operator:" + label), Kind: semreg.OriginOperator, Evidence: proof("operator")}
}

func resourceOf(id, kind string, class semreg.AnchorClass, slot string, state semreg.ResourceState) semreg.Resource {
	return semreg.Resource{
		ResourceID: semreg.ResourceID(id), Kind: semreg.DefinitionID(kind),
		Anchor: semreg.ResourceAnchor{Class: class, Basis: semreg.AnchorOperatorSlot, Slot: &slot, Evidence: proof("anchor")},
		State:  state, Origin: operatorOrigin(id), Revision: "1",
	}
}

func relationOf(definition semreg.DefinitionRef, from, to string) semreg.RelationInstance {
	return semreg.RelationInstance{
		Definition: definition, From: semreg.ResourceID(from), To: semreg.ResourceID(to), State: semreg.LinkQualified,
		BasisClass: semreg.RelationConfiguration, Basis: proof("relation"), Origin: operatorOrigin(from + to), Revision: "1",
	}
}

func (a asset) snapshot(t *testing.T) semreg.Snapshot {
	t.Helper()
	source, epoch, binding := semreg.SourceID("source:"+a.id), semreg.SourceEpochID("epoch:"+a.id), semreg.NativeBindingID("binding:"+a.id)
	generation := semreg.Uint64("1")
	at := semreg.TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"}
	s := semreg.Snapshot{
		Contract: semreg.ContractKernelV1, AssetID: a.id,
		Revisions:         semreg.RevisionVector{Semantic: "1", Identity: "1", Facts: "1", Services: "1", Capabilities: "1", Structure: "1"},
		EvaluatedAt:       semreg.TimePoint{UnixNanoseconds: "101", ClockID: "clock.utc", UncertaintyNS: "0"},
		EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:test", Nanoseconds: "6"},
		Sources: []semreg.SourceDescriptor{{
			SourceID: source, SourceEpochID: epoch, ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "release-1",
			RegistryEvidence: proof("registry")[0], StartedAt: at, State: semreg.SourceCurrent, Revision: "1",
		}},
		Bindings:      []semreg.NativeBinding{{BindingID: binding, AssetID: a.id, SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, NativeResource: proof("native")[0], State: semreg.BindingCurrent, Revision: "1"}},
		IdentityLinks: []semreg.IdentityLink{}, Facts: []semreg.FactEnvelope{}, Services: []semreg.ServiceInstance{}, Capabilities: []semreg.CapabilityInstance{}, Fences: []semreg.GenerationFence{},
		Cursors: []semreg.PublicationCursor{{SourceID: source, SourceEpochID: epoch, DriverGeneration: generation, LastSequence: "1", LastBatchDigest: proof("batch")[0].Digest}},
	}
	s.Resources = append([]semreg.Resource(nil), a.resources...)
	sort.Slice(s.Resources, func(i, j int) bool { return s.Resources[i].ResourceID < s.Resources[j].ResourceID })
	s.Relations = append([]semreg.RelationInstance(nil), a.relations...)
	sort.Slice(s.Relations, func(i, j int) bool {
		return string(s.Relations[i].From)+"\x00"+string(s.Relations[i].To) < string(s.Relations[j].From)+"\x00"+string(s.Relations[j].To)
	})
	if len(s.Resources) == 0 {
		s.Resources = nil
	}
	if len(s.Relations) == 0 {
		s.Relations = nil
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object jsonObject
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	delete(object, "snapshot_id")
	canonical, err := semreg.CanonicalJSON(object)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	s.SnapshotID = semreg.SnapshotID("sha256:" + hex.EncodeToString(sum[:]))
	if err := s.Validate(); err != nil {
		t.Fatalf("snapshot of %s is invalid: %v", a.id, err)
	}
	return s
}

func cutOf(t *testing.T, catalog *graph.Catalog, completeness graph.Completeness, assets ...asset) *graph.Cut {
	t.Helper()
	snapshots := make([]semreg.Snapshot, len(assets))
	for i, a := range assets {
		snapshots[i] = a.snapshot(t)
	}
	cut, err := graph.AssembleCut(snapshots, catalog, completeness)
	if err != nil {
		t.Fatalf("AssembleCut: %v", err)
	}
	return cut
}
