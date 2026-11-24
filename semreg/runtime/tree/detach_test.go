package tree_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// sharedReader hands out the same buffers and pointers on every read and
// updates them in place, as a driver that keeps one retained observation does.
type sharedReader struct {
	encoded  []byte
	raw      []byte
	revision semreg.Uint64
	received semreg.TimePoint
	age      semreg.Uint64
}

func (r *sharedReader) read(context.Context) (tree.Observation, error) {
	return tree.Observation{Encoded: r.encoded, Raw: r.raw, Revision: &r.revision, ReceivedAt: &r.received, Age: &r.age}, nil
}

func (r *sharedReader) updateInPlace() {
	copy(r.encoded, "new")
	copy(r.raw, "NEW")
	r.revision = "8"
	r.received.UnixNanoseconds = "999"
	r.age = "5"
}

func detachedMember(t *testing.T, w *world, reader tree.ObservedReader) tree.Ref {
	t.Helper()
	w.reserve("kind_a", "ns_a", "inst.1")
	w.life.set(tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}, "running", 1)
	b := w.bind("ns_a", "inst.1", 1)
	device, err := b.Node(nodeSpec("device.1", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ObserveMember(device, memberSpec("member.1"), reader); err != nil {
		t.Fatal(err)
	}
	return nativeRef(t, "ns_a", mustPath(t, device), tree.KeywordAttributes, "member.1")
}

func TestPinnedReadKeepsTheBytesAndRevisionObservedAtCapture(t *testing.T) {
	w := newWorld(t)
	driverSide := &sharedReader{encoded: []byte("old"), raw: []byte("OLD"), revision: "7", received: semreg.TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc"}, age: "1"}
	ref := detachedMember(t, w, driverSide.read)
	cut := w.capture()

	driverSide.updateInPlace()
	got, err := cut.Read(ctx, ref, tree.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != tree.StatusCurrent || !got.Pinned || string(got.Encoded) != "old" || string(got.Raw) != "OLD" ||
		got.Revision == nil || *got.Revision != "7" || got.ReceivedAt == nil || got.ReceivedAt.UnixNanoseconds != "100" || got.Age == nil || *got.Age != "1" {
		t.Fatalf("pinned read after an in-place update = %+v", got)
	}

	// What the consumer was handed is its own: editing it leaves the pin alone.
	got.Encoded[0] = 'X'
	*got.Revision = "99"
	again, err := cut.Read(ctx, ref, tree.ReadOptions{})
	if err != nil || string(again.Encoded) != "old" || *again.Revision != "7" {
		t.Fatalf("pinned read after the consumer edited its copy = %+v, %v", again, err)
	}
}

func TestUnpinnedBaselineIsNotChangedByAnInPlaceUpdate(t *testing.T) {
	w := newWorld(t)
	buffer := []byte("old")
	driverSide := func(context.Context) (tree.Observation, error) { return tree.Observation{Encoded: buffer}, nil }
	ref := detachedMember(t, w, driverSide)
	cut := w.capture()

	copy(buffer, "new")
	got, err := cut.Read(ctx, ref, tree.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != tree.StatusChanged || string(got.Encoded) != "new" || got.Pinned {
		t.Fatalf("an in-place update after Capture = %+v, want changed", got)
	}
}

func TestReadOfAReplacedRegistrationIsUnavailableNotTheNewReader(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	instance := tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}
	w.life.set(instance, "running", 1)
	first := w.bind("ns_a", "inst.1", 1)
	device, err := first.Node(nodeSpec("device.1", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ObserveMember(device, memberSpec("member.1"), func(context.Context) (tree.Observation, error) {
		return tree.Observation{Encoded: []byte("one")}, nil
	}); err != nil {
		t.Fatal(err)
	}
	ref := nativeRef(t, "ns_a", mustPath(t, device), tree.KeywordAttributes, "member.1")
	cut := w.capture()

	w.life.fence(instance, 1, "stopped")
	w.life.set(instance, "running", 2)
	second := w.bind("ns_a", "inst.1", 2)
	device2, err := second.Node(nodeSpec("device.1", nil))
	if err != nil {
		t.Fatal(err)
	}
	replacement := memberSpec("member.1")
	replacement.Units = "unit.other"
	if err := second.ObserveMember(device2, replacement, func(context.Context) (tree.Observation, error) {
		return tree.Observation{Encoded: []byte("two")}, nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := cut.Read(ctx, ref, tree.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != tree.StatusUnavailable || got.Encoded != nil || got.Instance == nil {
		t.Fatalf("an old cut read the replacement registration: %+v", got)
	}
	fresh, err := w.capture().Read(ctx, ref, tree.ReadOptions{})
	if err != nil || fresh.Status != tree.StatusLiveOnly || string(fresh.Encoded) != "two" || fresh.Unit != "unit.other" {
		t.Fatalf("a new cut reads the replacement: %+v, %v", fresh, err)
	}
}

func TestScopedCaptureHandedSnapshotsAreTheConsumersOwn(t *testing.T) {
	w := scopedWorld(t)
	input := w.tree.SemanticInput(driver.Filter{})
	cut, err := input.Capture(ctx, driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	original := graph.CutRef{
		CutID: cut.Ref.CutID, Completeness: cut.Ref.Completeness,
		Members:     append([]graph.CutMember(nil), cut.Ref.Members...),
		NotIncluded: append([]graph.NotIncludedAsset(nil), cut.Ref.NotIncluded...),
	}
	before, err := input.RetrieveLens(ctx, cut.Ref, lensRequest(5))
	if err != nil {
		t.Fatal(err)
	}
	sumBefore, err := input.SumMembers(ctx, cut.Ref, lensRequest(5), powerFact, "res:site")
	if err != nil {
		t.Fatal(err)
	}
	for i := range cut.Snapshots {
		for j := range cut.Snapshots[i].Resources {
			cut.Snapshots[i].Resources[j].State = semreg.ResourceRetired
		}
		cut.Snapshots[i].Facts = nil
	}
	cut.Ref.Members[0].SnapshotID = "snapshot:edited"
	cut.Ref.NotIncluded = append(cut.Ref.NotIncluded, graph.NotIncludedAsset{AssetID: "asset.x", Reason: graph.NotIncludedExcluded})

	after, err := input.RetrieveLens(ctx, original, lensRequest(5))
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("the retained capture changed with the consumer's edit: %v", err)
	}
	sumAfter, err := input.SumMembers(ctx, original, lensRequest(5), powerFact, "res:site")
	if err != nil || !reflect.DeepEqual(sumAfter, sumBefore) {
		t.Fatalf("the sum changed with the consumer's edit: %+v vs %+v, %v", sumAfter, sumBefore, err)
	}
}

func TestLensResultIsDetachedFromTheRetainedCapture(t *testing.T) {
	w := scopedWorld(t)
	cut := w.capture()
	before, err := cut.RetrieveLens(ctx, lensRequest(5))
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Resources) == 0 || before.Resources[0].Resource.Anchor.Slot == nil || len(before.Resources[0].Resource.Anchor.Evidence) == 0 {
		t.Fatalf("lens result has no anchored resource: %+v", before.Resources)
	}
	wantSlot := *before.Resources[0].Resource.Anchor.Slot
	wantEvidence := before.Resources[0].Resource.Anchor.Evidence[0]

	// Editing nested fields of a returned result, through the capture and through
	// the tree's unpinned read, must not reach what later reads are answered from.
	for _, read := range []func() (graph.LensResult, error){
		func() (graph.LensResult, error) { return cut.RetrieveLens(ctx, lensRequest(5)) },
		func() (graph.LensResult, error) { return w.tree.RetrieveLens(ctx, lensRequest(5)) },
	} {
		result, err := read()
		if err != nil {
			t.Fatal(err)
		}
		*result.Resources[0].Resource.Anchor.Slot = "slot-edited"
		result.Resources[0].Resource.Anchor.Evidence[0].Digest = "sha256:edited"
	}
	after, err := cut.RetrieveLens(ctx, lensRequest(5))
	if err != nil {
		t.Fatal(err)
	}
	if got := *after.Resources[0].Resource.Anchor.Slot; got != wantSlot {
		t.Fatalf("a consumer's edit of a lens result reached the retained capture: slot %q, want %q", got, wantSlot)
	}
	if got := after.Resources[0].Resource.Anchor.Evidence[0]; got != wantEvidence {
		t.Fatalf("a consumer's edit of a lens result reached the retained capture: evidence %+v, want %+v", got, wantEvidence)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("a later read differs from the first after the consumer edited a returned result")
	}
}
