package life

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func planInstallation(f *testFactory, instances ...InstanceConfig) Installation {
	return Installation{Key: "installation.test", Bridge: "bridge.test", Clock: newTestClock(), Instances: instances}
}

func withClaims(factory *testFactory, key string, enabled bool, specs ...claimSpec) InstanceConfig {
	cfg := InstanceConfig{Driver: factory.kind, Instance: driver.InstanceKey(key), Enabled: enabled}
	if len(specs) > 0 {
		cfg.Config.Transport = transportJSON(specs...)
	}
	return cfg
}

func mustPlan(t *testing.T, inst Installation, factories ...driver.Factory) PlanSet {
	t.Helper()
	set, err := Plan(inst, factories...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func planError(t *testing.T, err error) *PlanError {
	t.Helper()
	var pe *PlanError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want a plan error", err)
	}
	return pe
}

// Three instances of one driver kind with distinct keys, one claim each, plan
// together; every configured instance, enabled or not, is planned once, and no
// instance is constructed.
func TestPlanThreeInstancesOfOneKind(t *testing.T) {
	f := newTestFactory(testKind)
	set := mustPlan(t, planInstallation(f,
		withClaims(f, "bus.one", true, claimSpec{Provider: "provider.a", Config: "config.one", Domain: "domain.one", Exclusive: true}),
		withClaims(f, "bus.two", true, claimSpec{Provider: "provider.a", Config: "config.two", Domain: "domain.one", Exclusive: true}),
		withClaims(f, "bus.three", false, claimSpec{Provider: "provider.a", Config: "config.three", Domain: "domain.one", Exclusive: true}),
	), f)
	if len(set.Instances) != 3 {
		t.Fatalf("instances = %d", len(set.Instances))
	}
	if f.planCalls() != 3 || f.constructed() != 0 {
		t.Fatalf("plan calls %d, constructs %d", f.planCalls(), f.constructed())
	}
	if set.Instances[1].Ref.Instance != "bus.three" || set.Instances[1].Enabled || !set.Instances[0].Enabled || !set.Instances[2].Enabled {
		t.Fatalf("enablement lost: %v", set.Instances)
	}
	var seen []string
	for _, in := range set.Instances {
		seen = append(seen, string(in.Ref.Instance))
		if in.Ref.Driver != testKind {
			t.Fatalf("kind = %s", in.Ref.Driver)
		}
	}
	if !reflect.DeepEqual(seen, []string{"bus.one", "bus.three", "bus.two"}) {
		t.Fatalf("instances are not in key order: %v", seen)
	}
}

// A plan with no transport claim is valid: it has its instance and no link
// node.
func TestPlanWithoutTransportHasNoLinkNode(t *testing.T) {
	f := newTestFactory(testKind)
	set := mustPlan(t, planInstallation(f, withClaims(f, "bus.none", true)), f)
	if len(set.Instances) != 1 || len(set.Instances[0].Links) != 0 || len(set.Edges) != 0 {
		t.Fatalf("set = %+v", set)
	}
	if len(set.Nodes) != 1 || set.Nodes[0].Driver != RuntimeDriver {
		t.Fatalf("only the bridge node is declared: %+v", set.Nodes)
	}
}

// Two exclusive claims on one transport fail planning with both instance ids,
// and nothing is constructed. A shared serialized claim is valid; an exclusive
// claim against a serialized one is not, and a disabled instance's claim takes
// part.
func TestPlanExclusiveTransportCollisionNamesBothInstances(t *testing.T) {
	claim := func(exclusive bool) claimSpec {
		return claimSpec{Provider: "provider.a", Config: "config.shared", Domain: "domain.one", Exclusive: exclusive}
	}
	cases := []struct {
		name     string
		a, b     claimSpec
		enabledB bool
		collides bool
	}{
		{"both exclusive", claim(true), claim(true), true, true},
		{"exclusive and serialized", claim(true), claim(false), true, true},
		{"serialized and exclusive", claim(false), claim(true), true, true},
		{"both serialized", claim(false), claim(false), true, false},
		{"disabled instance claim", claim(true), claim(true), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestFactory(testKind)
			_, err := Plan(planInstallation(f, withClaims(f, "bus.one", true, tc.a), withClaims(f, "bus.two", tc.enabledB, tc.b)), f)
			if !tc.collides {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			pe := planError(t, err)
			if len(pe.Problems) != 1 || !reflect.DeepEqual(pe.Problems[0].Instances, []Ref{ref("bus.one"), ref("bus.two")}) {
				t.Fatalf("problems = %+v", pe.Problems)
			}
			if !strings.Contains(err.Error(), "driver.test/bus.one") || !strings.Contains(err.Error(), "driver.test/bus.two") {
				t.Fatalf("message does not name both instances: %v", err)
			}
			if f.constructed() != 0 {
				t.Fatal("a plan error constructed an instance")
			}
		})
	}
}

// The claim identity is normalized: composed and decomposed forms of one text
// are the same transport.
func TestPlanTransportClaimsMatchAfterNormalization(t *testing.T) {
	f := newTestFactory(testKind)
	composed := claimSpec{Provider: "provider.é", Config: "config.one", Domain: "domain.one", Exclusive: true}
	decomposed := claimSpec{Provider: "provider.é", Config: "config.one", Domain: "domain.one", Exclusive: true}
	_, err := Plan(planInstallation(f, withClaims(f, "bus.one", true, composed), withClaims(f, "bus.two", true, decomposed)), f)
	planError(t, err)
}

// Two plans of one driver kind and instance key are a plan error, and so is a
// configured instance with no factory.
func TestPlanDuplicateAndUnplannedInstances(t *testing.T) {
	f := newTestFactory(testKind)
	dup := withClaims(f, "bus.one", true)
	_, err := Plan(planInstallation(f, dup, dup), f)
	pe := planError(t, err)
	if len(pe.Problems) != 1 || pe.Problems[0].Reason != "duplicate driver and instance pair" || !reflect.DeepEqual(pe.Problems[0].Instances, []Ref{ref("bus.one")}) {
		t.Fatalf("duplicate = %+v", pe.Problems)
	}
	other := withClaims(f, "bus.one", true)
	other.Driver = "driver.other"
	_, err = Plan(planInstallation(f, other), f)
	planError(t, err)
}

// A rejected plan set installs nothing, and a driver outside it keeps running:
// Plan returns no set and the first supervisor keeps its instances.
func TestPlanRejectedSetReturnsNoSet(t *testing.T) {
	f := newTestFactory(testKind)
	spec := claimSpec{Provider: "provider.a", Config: "config.shared", Domain: "domain.one", Exclusive: true}
	set, err := Plan(planInstallation(f, withClaims(f, "bus.one", true, spec), withClaims(f, "bus.two", true, spec)), f)
	planError(t, err)
	if len(set.Instances) != 0 || len(set.Nodes) != 0 || len(set.Edges) != 0 {
		t.Fatalf("a rejected plan returned a set: %+v", set)
	}
}

// Every field of the installation, bridge and instance identity is validated.
func TestPlanRejectsEachMissingIdentityMember(t *testing.T) {
	f := newTestFactory(testKind)
	base := func() Installation { return planInstallation(f, withClaims(f, "bus.one", true)) }
	mutations := map[string]func(*Installation){
		"installation key": func(i *Installation) { i.Key = "" },
		"bridge id":        func(i *Installation) { i.Bridge = "" },
		"driver kind":      func(i *Installation) { i.Instances[0].Driver = "" },
		"instance key":     func(i *Installation) { i.Instances[0].Instance = "" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			inst := base()
			mutate(&inst)
			planError(t, func() error { _, err := Plan(inst, f); return err }())
		})
	}
}

// The factory receives the configured instance key and enablement, owned copies
// of the two operands and the instance's own secret reference unchanged; a
// missing reference stays empty.
func TestPlanPassesTheConfiguredRecordToTheFactory(t *testing.T) {
	f := newTestFactory(testKind)
	transport := json.RawMessage(`[]`)
	options := json.RawMessage(`{"mode":"synthetic"}`)
	one := InstanceConfig{Driver: testKind, Instance: "bus.one", Enabled: true, Config: driver.Config{
		Instance: "ignored", Enabled: false, Transport: transport, Options: options, SecretRef: "ref.one"}}
	two := InstanceConfig{Driver: testKind, Instance: "bus.two", Enabled: false}
	mustPlan(t, planInstallation(f, one, two), f)
	got := f.plans
	byInstance := map[string]driver.Config{}
	for _, c := range got {
		byInstance[c.Instance] = c
	}
	c1, c2 := byInstance["bus.one"], byInstance["bus.two"]
	if c1.Instance != "bus.one" || !c1.Enabled || c1.SecretRef != "ref.one" || string(c1.Options) != `{"mode":"synthetic"}` {
		t.Fatalf("first record = %+v", c1)
	}
	if c2.Instance != "bus.two" || c2.Enabled || c2.SecretRef != "" || len(c2.Transport) != 0 || len(c2.Options) != 0 {
		t.Fatalf("second record = %+v", c2)
	}
	options[2] = 'X'
	if string(c1.Options) != `{"mode":"synthetic"}` {
		t.Fatal("the factory's operand aliases the caller's bytes")
	}
}

func TestPlanCarriesTheCalendarSelectionApartFromTheClock(t *testing.T) {
	f := newTestFactory(testKind)
	zone := "Europe/Bucharest"
	inst := planInstallation(f, withClaims(f, "bus.one", true))
	inst.Calendar = &zone
	set := mustPlan(t, inst, f)
	if set.Calendar == nil || *set.Calendar != zone || set.Calendar == inst.Calendar {
		t.Fatalf("calendar = %v", set.Calendar)
	}
	if set.Clock == nil {
		t.Fatal("the clock was dropped")
	}
	none := mustPlan(t, planInstallation(f, withClaims(f, "bus.one", true)), f)
	if none.Calendar != nil {
		t.Fatal("an absent calendar selection became present")
	}
}

func nodeTexts(keys []driver.NodeKey) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, nodeText(k))
	}
	return out
}

// The bridge and link keys and the relays edges are equal across two Plan calls
// with the factories and the instances given in reverse order.
func TestPlanTopologyIsStableAcrossOrder(t *testing.T) {
	build := func(reverse bool) PlanSet {
		a, b := newTestFactory("driver.alpha"), newTestFactory("driver.beta")
		cfgs := []InstanceConfig{
			withClaims(a, "bus.one", true, claimSpec{Provider: "provider.a", Config: "config.one", Domain: "domain.one", Exclusive: true}),
			withClaims(a, "bus.two", true, claimSpec{Provider: "provider.a", Config: "config.two", Domain: "domain.one"}),
			withClaims(b, "bus.one", false, claimSpec{Provider: "provider.b", Config: "config.three", Domain: "domain.two", Exclusive: true}),
			withClaims(b, "bus.four", true),
		}
		factories := []driver.Factory{a, b}
		if reverse {
			for i, j := 0, len(cfgs)-1; i < j; i, j = i+1, j-1 {
				cfgs[i], cfgs[j] = cfgs[j], cfgs[i]
			}
			factories = []driver.Factory{b, a}
		}
		return mustPlan(t, planInstallation(a, cfgs...), factories...)
	}
	first, second := build(false), build(true)
	if !reflect.DeepEqual(nodeTexts(first.Nodes), nodeTexts(second.Nodes)) {
		t.Fatalf("node keys differ:\n%v\n%v", nodeTexts(first.Nodes), nodeTexts(second.Nodes))
	}
	if !reflect.DeepEqual(first.Edges, second.Edges) {
		t.Fatalf("edges differ:\n%+v\n%+v", first.Edges, second.Edges)
	}
	if len(first.Nodes) != 4 || len(first.Edges) != 3 {
		t.Fatalf("nodes %d edges %d", len(first.Nodes), len(first.Edges))
	}
	bridge := first.Edges[0].From
	for _, e := range first.Edges {
		if e.Relation != RelaysRelation || e.Basis != v1.RelationNativeObservation || e.From != bridge {
			t.Fatalf("edge = %+v", e)
		}
		if e.To.Kind != EquipmentKind || e.To.Installation != "installation.test" {
			t.Fatalf("link key = %+v", e.To)
		}
	}
	if bridge.Driver != RuntimeDriver || bridge.Instance != "bridge.test" || bridge.Kind != EquipmentKind || bridge.Anchor.Slot != bridgeSlot || bridge.Anchor.Parent != "" {
		t.Fatalf("bridge key = %+v", bridge)
	}
}

// A disabled instance's configured link node stays declared from planning, with
// no construction, and so does the link of an instance that is never started.
func TestPlanDisabledInstanceKeepsItsLinkNode(t *testing.T) {
	f := newTestFactory(testKind)
	spec := claimSpec{Provider: "provider.a", Config: "config.one", Domain: "domain.one", Exclusive: true}
	set := mustPlan(t, planInstallation(f, withClaims(f, "bus.off", false, spec)), f)
	if len(set.Instances) != 1 || set.Instances[0].Enabled || len(set.Instances[0].Links) != 1 {
		t.Fatalf("instances = %+v", set.Instances)
	}
	link := set.Instances[0].Links[0].Key
	found := false
	for _, n := range set.Nodes {
		found = found || nodeText(n) == nodeText(link)
	}
	if !found || len(set.Edges) != 1 || set.Edges[0].To != link {
		t.Fatalf("the disabled instance's link is not declared: %+v %+v", set.Nodes, set.Edges)
	}
	if f.constructed() != 0 {
		t.Fatal("a disabled instance was constructed")
	}
}

// Each field of the link key, and what in the claim or configuration feeds it:
// the key differs when the installation key, the driver kind, the instance key,
// the provider, the config key, the access domain or the link slot differs, and
// is equal when only the sharing mode, the factory order or the endpoint
// operand differs.
func TestPlanLinkKeyVariesWithEveryIdentityField(t *testing.T) {
	baseSpec := claimSpec{Provider: "provider.a", Config: "config.one", Domain: "domain.one", Exclusive: true}
	key := func(mutate func(*Installation, *claimSpec, *InstanceConfig)) driver.NodeKey {
		f := newTestFactory(testKind)
		spec := baseSpec
		cfg := withClaims(f, "bus.one", true, spec)
		inst := planInstallation(f, cfg)
		mutate(&inst, &spec, &cfg)
		cfg.Config.Transport = transportJSON(spec)
		inst.Instances = []InstanceConfig{cfg}
		factory := newTestFactory(cfg.Driver)
		return mustPlan(t, inst, factory).Instances[0].Links[0].Key
	}
	base := key(func(*Installation, *claimSpec, *InstanceConfig) {})
	varied := map[string]func(*Installation, *claimSpec, *InstanceConfig){
		"installation key": func(i *Installation, _ *claimSpec, _ *InstanceConfig) { i.Key = "installation.other" },
		"driver kind":      func(_ *Installation, _ *claimSpec, c *InstanceConfig) { c.Driver = "driver.other" },
		"instance key":     func(_ *Installation, _ *claimSpec, c *InstanceConfig) { c.Instance = "bus.other" },
		"provider":         func(_ *Installation, s *claimSpec, _ *InstanceConfig) { s.Provider = "provider.b" },
		"config key":       func(_ *Installation, s *claimSpec, _ *InstanceConfig) { s.Config = "config.two" },
		"access domain":    func(_ *Installation, s *claimSpec, _ *InstanceConfig) { s.Domain = "domain.two" },
		"link slot":        func(_ *Installation, s *claimSpec, _ *InstanceConfig) { s.Slot = "slot.driver" },
	}
	for name, mutate := range varied {
		if got := key(mutate); got == base {
			t.Errorf("varying the %s did not change the link key", name)
		}
	}
	equal := map[string]func(*Installation, *claimSpec, *InstanceConfig){
		"share mode":    func(_ *Installation, s *claimSpec, _ *InstanceConfig) { s.Exclusive = false },
		"enablement":    func(_ *Installation, _ *claimSpec, c *InstanceConfig) { c.Enabled = false },
		"secret ref":    func(_ *Installation, _ *claimSpec, c *InstanceConfig) { c.Config.SecretRef = "ref.other" },
		"bridge id":     func(i *Installation, _ *claimSpec, _ *InstanceConfig) { i.Bridge = "bridge.other" },
		"options bytes": func(_ *Installation, _ *claimSpec, c *InstanceConfig) { c.Config.Options = json.RawMessage(`{"x":1}`) },
	}
	for name, mutate := range equal {
		if got := key(mutate); got != base {
			t.Errorf("varying the %s changed the link key: %+v", name, got)
		}
	}
	// The ordinary slot is the digest of the normalized claim identity, computed
	// here from the same three members.
	raw, _ := json.Marshal([3]string{"provider.a", "config.one", "domain.one"})
	sum := sha256.Sum256(raw)
	if want := "link:" + hex.EncodeToString(sum[:]); base.Anchor.Slot != want || base.Anchor.Parent != "" {
		t.Fatalf("ordinary slot = %q", base.Anchor.Slot)
	}
}

// A link the driver publishes itself, such as a logger, is one equipment node
// declared from configuration under the driver's own slot before any
// publication, also while its instance is disabled; no second configured-slot
// node is declared, the bridge's relays edge ends at that key, and the driver's
// first publication under the same key leaves one logger carrying both relays
// segments. Neither the slot nor the order lets a second claim bypass the
// exclusive transport check or duplicate the key.
func TestPlanLoggerIsDeclaredOnceUnderTheDriversSlot(t *testing.T) {
	logger := claimSpec{Provider: "provider.a", Config: "config.logger", Domain: "domain.one", Exclusive: true, Slot: "slot.logger"}
	published := func(set PlanSet) driver.NodeKey {
		// The driver's first publication names the logger by the same durable
		// parent-and-slot key it declared from configuration.
		return driver.NodeKey{Installation: set.Installation, Driver: testKind, Instance: "bus.logger", Kind: EquipmentKind, Anchor: driver.AnchorKey{Slot: "slot.logger"}}
	}
	for _, enabled := range []bool{true, false} {
		f := newTestFactory(testKind)
		set := mustPlan(t, planInstallation(f, withClaims(f, "bus.logger", enabled, logger)), f)
		link := set.Instances[0].Links[0].Key
		if link != published(set) {
			t.Fatalf("enabled=%v: planned key %+v differs from the publisher's %+v", enabled, link, published(set))
		}
		slotted := 0
		for _, n := range set.Nodes {
			if n.Driver == testKind {
				slotted++
				if n.Anchor.Slot != "slot.logger" {
					t.Fatalf("a second configured-slot node is declared: %+v", n)
				}
			}
		}
		if slotted != 1 || len(set.Edges) != 1 || set.Edges[0].To != link {
			t.Fatalf("enabled=%v: nodes %v edges %+v", enabled, nodeTexts(set.Nodes), set.Edges)
		}
		// The first publication adds the logger-to-device segment on the same key:
		// merging by key leaves one logger carrying both segments.
		device := driver.NodeKey{Installation: set.Installation, Driver: testKind, Instance: "bus.logger", Kind: EquipmentKind, Anchor: driver.AnchorKey{Slot: "slot.device"}}
		nodes := map[string]int{}
		for _, n := range append(append([]driver.NodeKey(nil), set.Nodes...), published(set), device) {
			nodes[nodeText(n)]++
		}
		segments := 0
		for _, e := range append(append([]Edge(nil), set.Edges...), Edge{Relation: RelaysRelation, From: published(set), To: device}) {
			if e.From == link || e.To == link {
				segments++
			}
		}
		if len(nodes) != 3 || segments != 2 {
			t.Fatalf("enabled=%v: merged nodes %d, relays segments on the logger %d", enabled, len(nodes), segments)
		}
	}

	t.Run("a nonempty slot does not enter exclusivity matching", func(t *testing.T) {
		f := newTestFactory(testKind)
		a := claimSpec{Provider: "provider.a", Config: "config.shared", Domain: "domain.one", Exclusive: true, Slot: "slot.a"}
		b := claimSpec{Provider: "provider.a", Config: "config.shared", Domain: "domain.one", Exclusive: true, Slot: "slot.b"}
		_, err := Plan(planInstallation(f, withClaims(f, "bus.one", true, a), withClaims(f, "bus.two", true, b)), f)
		planError(t, err)
		_, err = Plan(planInstallation(f, withClaims(f, "bus.two", true, b), withClaims(f, "bus.one", true, a)), f)
		planError(t, err)
	})
	t.Run("two claims for one static key are a duplicate", func(t *testing.T) {
		f := newTestFactory(testKind)
		first := claimSpec{Provider: "provider.a", Config: "config.one", Domain: "domain.one", Slot: "slot.same"}
		second := claimSpec{Provider: "provider.a", Config: "config.two", Domain: "domain.one", Slot: "slot.same"}
		_, err := Plan(planInstallation(f, withClaims(f, "bus.one", true, first, second)), f)
		pe := planError(t, err)
		if len(pe.Problems) != 1 || !reflect.DeepEqual(pe.Problems[0].Instances, []Ref{ref("bus.one")}) {
			t.Fatalf("problems = %+v", pe.Problems)
		}
		same := withClaims(f, "bus.one", true, first, first)
		_, err = Plan(planInstallation(f, same), f)
		planError(t, err)
	})
	t.Run("an empty slot keeps the ordinary key", func(t *testing.T) {
		f := newTestFactory(testKind)
		plain := claimSpec{Provider: "provider.a", Config: "config.one", Domain: "domain.one"}
		set := mustPlan(t, planInstallation(f, withClaims(f, "bus.one", true, plain)), f)
		if got := set.Instances[0].Links[0].Key.Anchor.Slot; !strings.HasPrefix(got, linkSlotPrefix) {
			t.Fatalf("slot = %q", got)
		}
	})
}

// The runtime declares no communication session as a node: the planned nodes are
// the bridge and the links, nothing else.
func TestPlanDeclaresNoSessionNode(t *testing.T) {
	f := newTestFactory(testKind)
	spec := claimSpec{Provider: "provider.a", Config: "config.one", Domain: "domain.one"}
	set := mustPlan(t, planInstallation(f, withClaims(f, "bus.one", true, spec, claimSpec{Provider: "provider.a", Config: "config.two", Domain: "domain.one"})), f)
	if len(set.Nodes) != 3 {
		t.Fatalf("nodes = %v", nodeTexts(set.Nodes))
	}
	for _, n := range set.Nodes {
		if n.Kind != EquipmentKind {
			t.Fatalf("non-equipment node %+v", n)
		}
	}
}

func TestRefIDIsUnambiguous(t *testing.T) {
	a := Ref{Driver: "a", Instance: "b/c"}
	b := Ref{Driver: "a/b", Instance: "c"}
	if a.ID() == b.ID() {
		t.Fatal("two references share an id")
	}
	if a.ID() != (Ref{Driver: "a", Instance: "b/c"}).ID() {
		t.Fatal("equal references have different ids")
	}
}
