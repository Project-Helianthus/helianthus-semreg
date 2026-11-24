package tree_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// retained is a driver's retained value. Reading it never touches a transport.
type retained struct {
	mu  sync.Mutex
	obs tree.Observation
	err error
}

func (r *retained) set(obs tree.Observation) { r.mu.Lock(); r.obs = obs; r.mu.Unlock() }

func (r *retained) read(context.Context) (tree.Observation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.obs, r.err
}

func revision(n string) *semreg.Uint64 { v := semreg.Uint64(n); return &v }

// transportStub is the bus a detached read must never reach.
type transportStub struct {
	t       *testing.T
	touched int
}

func (s *transportStub) send() {
	s.touched++
	s.t.Errorf("the transport was touched by a detached read")
}

func TestRegistrationReadAndWithdrawalAcrossGeneration(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	instance := tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}
	w.life.set(instance, "running", 1)
	first := w.bind("ns_a", "inst.1", 1)
	value := &retained{obs: tree.Observation{Encoded: []byte("one")}}
	transport, err := first.Node(nodeSpec("transport.1", nil))
	if err != nil {
		t.Fatal(err)
	}
	device, err := first.Node(nodeSpec("device.1", &transport))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.ObserveMember(device, memberSpec("member.1"), value.read); err != nil {
		t.Fatal(err)
	}
	called := 0
	if err := first.Method(device, methodSpec("method.1"), func(context.Context, []byte) ([]byte, error) { called++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	ref := nativeRef(t, "ns_a", mustPath(t, device), tree.KeywordAttributes, "member.1")
	methodRef := nativeRef(t, "ns_a", mustPath(t, device), tree.KeywordMethods, "method.1")

	cut := w.capture()
	got, err := cut.Read(ctx, ref, tree.ReadOptions{})
	if err != nil || got.Status != tree.StatusLiveOnly || string(got.Encoded) != "one" {
		t.Fatalf("read before the fence = %+v, %v", got, err)
	}

	w.life.fence(instance, 1, "stopped")
	got, err = cut.Read(ctx, ref, tree.ReadOptions{})
	if err != nil || got.Status != tree.StatusUnavailable || got.Instance == nil || got.Instance.Name != "stopped" || got.Encoded != nil {
		t.Fatalf("read after the fence = %+v, %v", got, err)
	}
	described, err := cut.Describe(ctx, ref)
	if err != nil || !described.Withdrawn || described.State == nil || described.State.Name != "stopped" || described.Local != "member.1" {
		t.Fatalf("a stopped driver's member stays listed with its state: %+v, %v", described, err)
	}
	listed, err := cut.List(ctx, device, tree.Page{Size: 10})
	if err != nil || len(listed.Entries) != 2 {
		t.Fatalf("withdrawn descriptors are listed: %+v %v", listed, err)
	}
	if _, err := w.tree.Prepare(ctx, cut.ID(), methodRef, tree.Input{}); !errors.Is(err, tree.ErrUnavailable) {
		t.Fatalf("a withdrawn method prepares = %v", err)
	}

	if err := first.ReadMember(device, memberSpec("member.late"), func(context.Context) ([]byte, error) { return nil, nil }); !errors.Is(err, tree.ErrFenced) {
		t.Fatalf("late registration from the fenced generation = %v, want ErrFenced", err)
	}
	if _, err := first.Node(nodeSpec("device.late", &transport)); !errors.Is(err, tree.ErrFenced) {
		t.Fatalf("late node from the fenced generation = %v", err)
	}
	if _, err := w.tree.Registry.Bind("ns_a", "inst.1", 1); !errors.Is(err, tree.ErrFenced) {
		t.Fatalf("binding the fenced generation = %v", err)
	}

	w.life.set(instance, "running", 2)
	second := w.bind("ns_a", "inst.1", 2)
	transport2, err := second.Node(nodeSpec("transport.1", nil))
	if err != nil {
		t.Fatalf("the new generation registers the withdrawn node again: %v", err)
	}
	device2, err := second.Node(nodeSpec("device.1", &transport2))
	if err != nil {
		t.Fatal(err)
	}
	value.set(tree.Observation{Encoded: []byte("two")})
	if err := second.ObserveMember(device2, memberSpec("member.1"), value.read); err != nil {
		t.Fatalf("the new generation registers the withdrawn member again: %v", err)
	}
	if err := second.Method(device2, methodSpec("method.1"), func(context.Context, []byte) ([]byte, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	fresh := w.capture()
	got, err = fresh.Read(ctx, ref, tree.ReadOptions{})
	if err != nil || got.Status != tree.StatusLiveOnly || string(got.Encoded) != "two" {
		t.Fatalf("read in the new generation = %+v, %v", got, err)
	}
	old, err := cut.Read(ctx, ref, tree.ReadOptions{})
	if err != nil || old.Status != tree.StatusUnavailable || old.Instance == nil || old.Instance.Generation != 2 || old.Encoded != nil {
		t.Fatalf("old cut read = %+v, %v; the fenced generation's cut must read unavailable with the instance state", old, err)
	}
	// The old cut's descriptors stay withdrawn although a later generation
	// registered the same keys; the fresh cut's are current.
	for _, entry := range []tree.Ref{ref, methodRef, tree.Ref(mustPath(t, device).String())} {
		oldDescriptor, err := cut.Describe(ctx, entry)
		if err != nil || !oldDescriptor.Withdrawn || oldDescriptor.State == nil {
			t.Fatalf("old cut descriptor %s = %+v, %v; want withdrawn with the instance state", entry, oldDescriptor, err)
		}
		newDescriptor, err := fresh.Describe(ctx, entry)
		if err != nil || newDescriptor.Withdrawn {
			t.Fatalf("fresh cut descriptor %s = %+v, %v; want current", entry, newDescriptor, err)
		}
	}
	listedOld, err := cut.List(ctx, device, tree.Page{Size: 10})
	if err != nil || len(listedOld.Entries) != 2 {
		t.Fatalf("old cut list = %+v, %v", listedOld, err)
	}
	for _, entry := range listedOld.Entries {
		if !entry.Withdrawn {
			t.Fatalf("old cut lists %s as current after a later generation registered it", entry.Ref)
		}
	}
	if called != 0 {
		t.Fatal("a method ran without being called")
	}
}

func mustPath(t *testing.T, ref driver.NodeRef) tree.NodePath {
	t.Helper()
	path, err := tree.ParseNodePath("/" + string(ref.ID))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNativeReadNeverTouchesTheTransport(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	stub := &transportStub{t: t}
	b := w.bind("ns_a", "inst.1", 1)
	value := &retained{obs: tree.Observation{Encoded: []byte("cached"), Freshness: semreg.FreshnessFresh}}
	if err := b.ObserveMember(b.Root(), memberSpec("member.1"), value.read); err != nil {
		t.Fatal(err)
	}
	if err := b.Method(b.Root(), methodSpec("procedure.1"), func(context.Context, []byte) ([]byte, error) { return []byte("ran"), nil }); err != nil {
		t.Fatal(err)
	}
	cut := w.capture()
	ref := nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordAttributes, "member.1")
	if _, err := cut.Describe(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := cut.List(ctx, b.Root(), tree.Page{Size: 10}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := cut.Read(ctx, ref, tree.ReadOptions{})
		if err != nil || string(got.Encoded) != "cached" {
			t.Fatalf("read = %+v, %v", got, err)
		}
	}
	methodRef := nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordMethods, "procedure.1")
	if _, err := w.tree.Prepare(ctx, cut.ID(), methodRef, tree.Input{}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tree.Resume(ctx, cut.Cursor(), driver.Filter{}); err != nil {
		t.Fatal(err)
	}
	if stub.touched != 0 {
		t.Fatalf("the transport was touched %d times", stub.touched)
	}
}

func TestNativeUpdateAfterCapture(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	instance := tree.InstanceRef{Driver: "kind_a", Instance: "inst.1"}
	w.life.set(instance, "running", 1)
	b := w.bind("ns_a", "inst.1", 1)
	pinned := &retained{obs: tree.Observation{Encoded: []byte("pinned-1"), Revision: revision("1")}}
	same := &retained{obs: tree.Observation{Encoded: []byte("same")}}
	moved := &retained{obs: tree.Observation{Encoded: []byte("before")}}
	failing := &retained{obs: tree.Observation{Encoded: []byte("ok")}}
	for name, r := range map[string]*retained{"pinned": pinned, "same": same, "moved": moved, "failing": failing} {
		if err := b.ObserveMember(b.Root(), memberSpec(name), r.read); err != nil {
			t.Fatal(err)
		}
	}
	read := func(cut *tree.TreeCut, name string) tree.Value {
		t.Helper()
		got, err := cut.Read(ctx, nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordAttributes, name), tree.ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	cut := w.capture()

	pinned.set(tree.Observation{Encoded: []byte("pinned-2"), Revision: revision("2")})
	moved.set(tree.Observation{Encoded: []byte("after")})
	failing.err = errors.New("reader failed")

	if got := read(cut, "pinned"); got.Status != tree.StatusCurrent || !got.Pinned || string(got.Encoded) != "pinned-1" || *got.Revision != "1" {
		t.Fatalf("a changed observation with a pinned revision must return the pinned value: %+v", got)
	}
	if got := read(cut, "same"); got.Status != tree.StatusLiveOnly || got.Pinned || string(got.Encoded) != "same" {
		t.Fatalf("unpinned unchanged = %+v", got)
	}
	got := read(cut, "moved")
	if got.Status != tree.StatusChanged || got.Pinned || string(got.Encoded) != "after" {
		t.Fatalf("unpinned changed = %+v", got)
	}
	if got := read(cut, "failing"); got.Status != tree.StatusUnavailable || got.Instance == nil || got.Instance.Name != "running" || got.Encoded != nil {
		t.Fatalf("unpinned unavailable = %+v", got)
	}
	newer := w.capture()
	if got := read(newer, "pinned"); string(got.Encoded) != "pinned-2" || *got.Revision != "2" {
		t.Fatalf("a later capture pins the later revision: %+v", got)
	}
	w.life.fence(instance, 1, "stopped")
	if got := read(cut, "pinned"); got.Status != tree.StatusUnavailable {
		t.Fatalf("a pinned value of a withdrawn member = %+v", got)
	}
}

func TestStaleMemberReturnsItsAgeAndNoCurrentSample(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	b := w.bind("ns_a", "inst.1", 1)
	received := semreg.TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"}
	age := semreg.Uint64("90000000000")
	cases := map[string]tree.Observation{
		"stale":   {Encoded: []byte("42"), Raw: []byte{0x2a}, ReceivedAt: &received, Freshness: semreg.FreshnessStale, Age: &age},
		"expired": {Encoded: []byte("43"), ReceivedAt: &received, Freshness: semreg.FreshnessExpired, Age: &age},
		"fresh":   {Encoded: []byte("44"), Raw: []byte{0x2c}, ReceivedAt: &received, Freshness: semreg.FreshnessFresh},
	}
	for name, obs := range cases {
		r := &retained{obs: obs}
		if err := b.ObserveMember(b.Root(), memberSpec(name), r.read); err != nil {
			t.Fatal(err)
		}
	}
	cut := w.capture()
	read := func(name string) tree.Value {
		got, err := cut.Read(ctx, nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordAttributes, name), tree.ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, name := range []string{"stale", "expired"} {
		got := read(name)
		if got.Status != tree.StatusStale || got.Encoded != nil || got.Raw != nil || got.Age == nil || *got.Age != age || got.ReceivedAt == nil || got.Freshness != cases[name].Freshness {
			t.Fatalf("%s member = %+v, want its age, receive time and no sample", name, got)
		}
	}
	if got := read("fresh"); got.Status != tree.StatusLiveOnly || string(got.Encoded) != "44" || got.Freshness != semreg.FreshnessFresh {
		t.Fatalf("fresh member = %+v", got)
	}
}

func TestValueTheDocumentationDoesNotDefineStaysNativeAndNamed(t *testing.T) {
	w := newWorld(t)
	w.reserve("kind_a", "ns_a", "inst.1")
	b := w.bind("ns_a", "inst.1", 1)
	received := semreg.TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"}
	undefined := &retained{obs: tree.Observation{Encoded: []byte{0xde, 0xad}, Raw: []byte{0xde, 0xad}, Name: "native.register.0x7f", Verification: "level.observed", ReceivedAt: &received, Freshness: semreg.FreshnessFresh}}
	derived := &retained{obs: tree.Observation{Encoded: []byte("7"), Derived: true, Freshness: semreg.FreshnessFresh}}
	absent := &retained{obs: tree.Observation{Absent: true}}
	spec := memberSpec("undefined")
	spec.Units = "unit.as_reported"
	for name, r := range map[string]*retained{"undefined": undefined, "derived": derived, "absent": absent} {
		s := memberSpec(name)
		if name == "undefined" {
			s = spec
		}
		if err := b.ObserveMember(b.Root(), s, r.read); err != nil {
			t.Fatal(err)
		}
	}
	cut := w.capture()
	read := func(name string) tree.Value {
		got, err := cut.Read(ctx, nativeRef(t, "ns_a", mustPath(t, b.Root()), tree.KeywordAttributes, name), tree.ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := read("undefined")
	if got.Name != "native.register.0x7f" || string(got.Raw) != "\xde\xad" || string(got.Encoded) != "\xde\xad" || got.Verification != "level.observed" || got.Unit != "unit.as_reported" {
		t.Fatalf("undefined value = %+v", got)
	}
	if got := read("derived"); !got.Derived {
		t.Fatalf("a derived value is marked derived: %+v", got)
	}
	if got := read("absent"); got.Status != tree.StatusAbsent || got.Encoded != nil || got.Raw != nil {
		t.Fatalf("an absent value is absent, not zero: %+v", got)
	}
}
