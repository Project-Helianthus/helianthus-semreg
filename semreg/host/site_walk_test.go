package host

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

const kindNative driver.DriverKind = "synth.native"

// nativeFactory plans kindNative; its instance registers one native method on
// its own node and one on a device node beneath it.
type nativeFactory struct{}

func (nativeFactory) Descriptor() driver.Descriptor {
	return driver.Descriptor{Kinds: []driver.DriverKind{kindNative}}
}
func (nativeFactory) Plan(c driver.Config) ([]driver.InstancePlan, error) {
	return []driver.InstancePlan{{Driver: kindNative, Instance: driver.InstanceKey(c.Instance), Transports: []driver.TransportClaim{{Provider: "provider.synthetic", ConfigKey: "config.synthetic", AccessDomain: "domain.synthetic", Share: driver.ShareExclusive}}}}, nil
}
func (nativeFactory) Construct(context.Context, driver.InstancePlan) (driver.Instance, error) {
	return &nativeInstance{}, nil
}

type nativeInstance struct{}

func (*nativeInstance) FenceWithdrawal() {}
func (*nativeInstance) Stop(context.Context) driver.CloseReport {
	return driver.CloseReport{Confirmed: true}
}
func (*nativeInstance) Start(_ context.Context, s driver.Session) error {
	registry := s.Native()
	root := registry.Root()
	method := func(local string) driver.MethodSpec {
		return driver.MethodSpec{Local: driver.MemberID(local), Schema: "schema.synthetic", Scope: "scope.synthetic"}
	}
	noop := func(context.Context, []byte) ([]byte, error) { return nil, nil }
	if err := registry.Method(root, method("instance.method"), noop); err != nil {
		return err
	}
	device, err := registry.Node(driver.NodeSpec{Parent: &root, Kind: "synthetic.device", Local: "device.one", Contract: "contract.synthetic"})
	if err != nil {
		return err
	}
	return registry.Method(device, method("device.method"), noop)
}

func TestMethodsOnlyWalkReachesNativeMethodsBeneathNodesAndEmitsNoLink(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	cfg := baseConfig(t, clock)
	cfg.Instances = []InstanceConfig{{Kind: kindNative, Instance: "native.one", Enabled: true}}
	site, err := Open(ctx, cfg, nativeFactory{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = site.Close(ctx) }()
	if err := site.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := site.WaitInitial(ctx); err != nil {
		t.Fatal(err)
	}
	cut, err := site.Cut(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cut.Close()

	collect := func(filter WalkFilter) (methods []string, others, links int) {
		_, err := cut.Walk(ctx, filter, func(e WalkEntry) error {
			switch {
			case e.Link != nil || e.RetainedLink != nil:
				links++
			case e.Descriptor.Kind == tree.EntryMethod && e.Descriptor.Domain == tree.KeywordNative:
				methods = append(methods, string(e.Descriptor.Ref))
			case e.Descriptor.Kind != tree.EntryMethod:
				others++
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(methods)
		return
	}
	allMethods, _, allLinks := collect(AllVisible(2))
	if len(allMethods) != 2 {
		t.Fatalf("the unfiltered walk reaches %d native methods, want the instance and the device one", len(allMethods))
	}
	if allLinks == 0 {
		t.Fatal("the unfiltered walk emits no link, so the filtered case proves nothing")
	}
	only := WalkFilter{PageSize: 2, Filter: tree.Filter{Kind: tree.EntryMethod}}
	methods, others, links := collect(only)
	if len(methods) != len(allMethods) || methods[0] != allMethods[0] || methods[1] != allMethods[1] {
		t.Fatalf("the methods-only walk returned %v, want %v", methods, allMethods)
	}
	if others != 0 || links != 0 {
		t.Fatalf("the methods-only walk emitted %d nonmatching entries and %d links", others, links)
	}
}
