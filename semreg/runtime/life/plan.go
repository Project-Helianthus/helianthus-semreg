package life

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Node kinds and relations the planned topology uses.
const (
	// EquipmentKind is the node kind of the host bridge and of every link.
	EquipmentKind v1.DefinitionID = "resource.equipment"
	// RelaysRelation is the relation from the bridge to each link.
	RelaysRelation v1.DefinitionID = "relation.relays"
	// RuntimeDriver is the driver kind under which the runtime itself declares
	// the nodes it owns: the host bridge.
	RuntimeDriver driver.DriverKind = "semreg.runtime"
	// bridgeSlot is the configured slot of the host bridge node.
	bridgeSlot = "bridge"
	// linkSlotPrefix starts the configured slot of a link the runtime keys
	// itself.
	linkSlotPrefix = "link:"
)

// Clock is declared in recovery.go.

// Installation is the configured identity and inputs of one installation. Key
// and Bridge are the installation's configured identity. StateDir holds the
// durable control records. Calendar is the optional IANA location selection for
// the calendar service, kept separate from the clock.
type Installation struct {
	Key       string
	Bridge    driver.BridgeID
	StateDir  string
	Clock     Clock
	Calendar  *string
	Instances []InstanceConfig
}

// InstanceConfig is one configured instance of a driver. Config carries the two
// operands and the secret reference; the runtime passes the configured
// Instance and Enabled in the driver.Config it hands the factory.
type InstanceConfig struct {
	Driver   driver.DriverKind
	Instance driver.InstanceKey
	Enabled  bool
	Config   driver.Config
}

// Ref names one configured instance by its driver kind and instance key.
type Ref struct {
	Driver   driver.DriverKind
	Instance driver.InstanceKey
}

// ID returns the canonical instance id of the reference: a JSON array of the
// driver kind and the instance key, so no two references share an id.
func (r Ref) ID() InstanceID {
	raw, _ := json.Marshal([2]string{string(r.Driver), string(r.Instance)})
	return InstanceID(raw)
}

// String renders the reference for messages.
func (r Ref) String() string { return string(r.Driver) + "/" + string(r.Instance) }

// Link is one planned link: one transport claim of an instance with the node
// that declares it as equipment.
type Link struct {
	Claim driver.TransportClaim
	Key   driver.NodeKey
}

// Edge is one planned edge between two nodes by their durable keys.
type Edge struct {
	Relation v1.DefinitionID
	From     driver.NodeKey
	To       driver.NodeKey
	// Basis keeps the native-observation basis of the relation; the configured
	// durable keys do not relabel it.
	Basis v1.RelationBasis
}

// PlannedInstance is one configured instance with its pure plan.
type PlannedInstance struct {
	Ref     Ref
	Enabled bool
	Config  driver.Config
	Plan    driver.InstancePlan
	Links   []Link
	factory driver.Factory
}

// PlanSet is the validated, installable result of planning.
type PlanSet struct {
	Installation string
	Bridge       driver.BridgeID
	StateDir     string
	Clock        Clock
	Calendar     *string
	// Instances are ordered by driver kind and instance key.
	Instances []PlannedInstance
	// Nodes are the equipment nodes declared from planning on: the host bridge
	// and one per configured link, in canonical key order.
	Nodes []driver.NodeKey
	// Edges are the relays from the bridge to each link, in canonical order.
	Edges []Edge
}

// PlanProblem is one rejected condition of a plan set with the instances it
// names.
type PlanProblem struct {
	Reason    string
	Instances []Ref
}

// PlanError is the rejection of a plan set. A rejected set installs nothing.
type PlanError struct {
	Problems []PlanProblem
}

func (e *PlanError) Error() string {
	parts := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		names := make([]string, 0, len(p.Instances))
		for _, r := range p.Instances {
			names = append(names, r.String())
		}
		parts = append(parts, fmt.Sprintf("%s (instances: %s)", p.Reason, strings.Join(names, ", ")))
	}
	return "life: plan rejected: " + strings.Join(parts, "; ")
}

type planner struct {
	problems []PlanProblem
}

func (p *planner) fail(reason string, refs ...Ref) {
	p.problems = append(p.problems, PlanProblem{Reason: reason, Instances: uniqueRefs(refs)})
}

func uniqueRefs(refs []Ref) []Ref {
	seen := map[Ref]bool{}
	var out []Ref
	for _, r := range refs {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return refLess(out[i], out[j]) })
	return out
}

func refLess(a, b Ref) bool {
	if a.Driver != b.Driver {
		return a.Driver < b.Driver
	}
	return a.Instance < b.Instance
}

// Plan plans every configured instance, enabled or disabled, before any I/O. It
// calls Factory.Plan, which is pure, and validates all plans together:
// duplicate driver and instance pairs, two plans claiming one static node key
// and two transport claims on one normalized provider, config key and access
// domain where either is exclusive are each a plan error naming the instances.
// A plan error means the set installs nothing. Nothing is constructed.
func Plan(inst Installation, factories ...driver.Factory) (PlanSet, error) {
	p := &planner{}
	if inst.Key == "" {
		p.fail("installation key is required")
	}
	if err := inst.Bridge.Validate(); err != nil {
		p.fail("bridge: " + err.Error())
	}
	set := PlanSet{Installation: inst.Key, Bridge: inst.Bridge, StateDir: inst.StateDir, Clock: inst.Clock}
	if inst.Calendar != nil {
		calendar := *inst.Calendar
		set.Calendar = &calendar
	}

	configured := map[Ref]bool{}
	for _, ic := range inst.Instances {
		ref := Ref{Driver: ic.Driver, Instance: ic.Instance}
		if err := ic.Driver.Validate(); err != nil {
			p.fail("driver: "+err.Error(), ref)
			continue
		}
		if err := ic.Instance.Validate(); err != nil {
			p.fail("instance: "+err.Error(), ref)
			continue
		}
		if configured[ref] {
			p.fail("duplicate driver and instance pair", ref)
			continue
		}
		configured[ref] = true
		config := driver.Config{
			Instance:  string(ic.Instance),
			Enabled:   ic.Enabled,
			Transport: append(json.RawMessage(nil), ic.Config.Transport...),
			Options:   append(json.RawMessage(nil), ic.Config.Options...),
			SecretRef: ic.Config.SecretRef,
		}
		planned, ok := p.planInstance(ref, ic.Enabled, config, factories)
		if ok {
			set.Instances = append(set.Instances, planned)
		}
	}
	sort.Slice(set.Instances, func(i, j int) bool { return refLess(set.Instances[i].Ref, set.Instances[j].Ref) })

	p.checkTransports(set.Instances)
	if inst.Key != "" && inst.Bridge.Validate() == nil {
		set.Nodes, set.Edges = p.topology(set)
	}
	if len(p.problems) > 0 {
		sort.SliceStable(p.problems, func(i, j int) bool { return p.problems[i].Reason < p.problems[j].Reason })
		return PlanSet{}, &PlanError{Problems: p.problems}
	}
	return set, nil
}

// planInstance calls Plan on each supplied factory with the instance's
// configuration. A factory handles the instance when one of its plans names the
// configured driver kind; exactly one factory may handle it, and every plan of
// that factory for the configuration carries the configured identity.
func (p *planner) planInstance(ref Ref, enabled bool, config driver.Config, factories []driver.Factory) (PlannedInstance, bool) {
	var handled []driver.Factory
	var plans [][]driver.InstancePlan
	for _, f := range factories {
		got, err := f.Plan(config)
		if err != nil {
			p.fail("factory plan: "+err.Error(), ref)
			return PlannedInstance{}, false
		}
		var mine []driver.InstancePlan
		for _, plan := range got {
			if plan.Driver == ref.Driver {
				mine = append(mine, plan)
			}
		}
		if len(mine) > 0 {
			handled = append(handled, f)
			plans = append(plans, mine)
		}
	}
	switch {
	case len(handled) == 0 && enabled:
		p.fail("no factory plans the instance", ref)
		return PlannedInstance{}, false
	case len(handled) == 0:
		return PlannedInstance{Ref: ref, Enabled: false, Config: config, Plan: driver.InstancePlan{Driver: ref.Driver, Instance: ref.Instance}}, true
	case len(handled) > 1:
		p.fail("more than one factory plans the instance", ref)
		return PlannedInstance{}, false
	}
	var chosen driver.InstancePlan
	seen := map[Ref]bool{}
	for i, plan := range plans[0] {
		if err := plan.Validate(); err != nil {
			p.fail("invalid plan: "+err.Error(), ref)
			return PlannedInstance{}, false
		}
		got := Ref{Driver: plan.Driver, Instance: plan.Instance}
		if got != ref {
			p.fail(fmt.Sprintf("the factory planned %s for the configured instance", got), ref)
			return PlannedInstance{}, false
		}
		if seen[got] {
			p.fail("duplicate driver and instance pair", ref)
			return PlannedInstance{}, false
		}
		seen[got] = true
		if i == 0 {
			chosen = plan
		}
	}
	chosen.Transports = append([]driver.TransportClaim(nil), chosen.Transports...)
	out := PlannedInstance{Ref: ref, Enabled: enabled, Config: config, Plan: chosen, factory: handled[0]}
	for _, claim := range chosen.Transports {
		out.Links = append(out.Links, Link{Claim: claim})
	}
	return out, true
}

// transportKey is the normalized provider, config key and access domain of a
// claim. The link node slot does not enter it.
type transportKey struct{ provider, configKey, accessDomain string }

func normalizeText(s string) string { return norm.NFC.String(s) }

func claimKey(c driver.TransportClaim) transportKey {
	return transportKey{normalizeText(c.Provider), normalizeText(c.ConfigKey), normalizeText(c.AccessDomain)}
}

// checkTransports rejects two claims on one transport where either is
// exclusive, naming every instance on that transport. Claims of disabled
// instances take part.
func (p *planner) checkTransports(instances []PlannedInstance) {
	type use struct {
		ref       Ref
		exclusive bool
	}
	groups := map[transportKey][]use{}
	var order []transportKey
	for _, in := range instances {
		for _, claim := range in.Plan.Transports {
			key := claimKey(claim)
			if _, ok := groups[key]; !ok {
				order = append(order, key)
			}
			groups[key] = append(groups[key], use{ref: in.Ref, exclusive: claim.Share == driver.ShareExclusive})
		}
	}
	for _, key := range order {
		uses := groups[key]
		if len(uses) < 2 {
			continue
		}
		exclusive := false
		var refs []Ref
		for _, u := range uses {
			exclusive = exclusive || u.exclusive
			refs = append(refs, u.ref)
		}
		if exclusive {
			p.fail(fmt.Sprintf("conflicting exclusive transport claim on provider %q, config key %q, access domain %q", key.provider, key.configKey, key.accessDomain), refs...)
		}
	}
}

// topology declares the bridge and the links as equipment with a relays edge
// from the bridge to each link, from configuration only. A link the driver
// publishes itself is keyed by the claim's LinkNodeSlot; any other link takes
// the slot derived from its claim identity.
func (p *planner) topology(set PlanSet) ([]driver.NodeKey, []Edge) {
	bridge := driver.NodeKey{
		Installation: set.Installation, Driver: RuntimeDriver, Instance: driver.InstanceKey(set.Bridge),
		Kind: EquipmentKind, Anchor: driver.AnchorKey{Slot: bridgeSlot},
	}
	keys := map[string]driver.NodeKey{nodeText(bridge): bridge}
	owners := map[string][]Ref{}
	var edges []Edge
	for i := range set.Instances {
		in := &set.Instances[i]
		for j := range in.Links {
			claim := in.Links[j].Claim
			slot := claim.LinkNodeSlot
			if slot == "" {
				slot = ordinaryLinkSlot(claim)
			}
			key := driver.NodeKey{
				Installation: set.Installation, Driver: in.Ref.Driver, Instance: in.Ref.Instance,
				Kind: EquipmentKind, Anchor: driver.AnchorKey{Slot: slot},
			}
			in.Links[j].Key = key
			text := nodeText(key)
			owners[text] = append(owners[text], in.Ref)
			if _, dup := keys[text]; dup {
				continue
			}
			keys[text] = key
			edges = append(edges, Edge{Relation: RelaysRelation, From: bridge, To: key, Basis: v1.RelationNativeObservation})
		}
	}
	for text, refs := range owners {
		if len(refs) > 1 {
			p.fail("two claims for one static node key "+text, refs...)
		}
	}
	nodes := make([]driver.NodeKey, 0, len(keys))
	for _, k := range keys {
		nodes = append(nodes, k)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodeText(nodes[i]) < nodeText(nodes[j]) })
	sort.Slice(edges, func(i, j int) bool {
		if a, b := nodeText(edges[i].From), nodeText(edges[j].From); a != b {
			return a < b
		}
		return nodeText(edges[i].To) < nodeText(edges[j].To)
	})
	return nodes, edges
}

// ordinaryLinkSlot derives the configured slot of a link from its claim
// identity: the digest of the normalized provider, config key and access
// domain. It depends on no address, host name, index or role string.
func ordinaryLinkSlot(c driver.TransportClaim) string {
	k := claimKey(c)
	raw, _ := json.Marshal([3]string{k.provider, k.configKey, k.accessDomain})
	sum := sha256.Sum256(raw)
	return linkSlotPrefix + hex.EncodeToString(sum[:])
}

// nodeText is the canonical text of a node key.
func nodeText(k driver.NodeKey) string {
	var b bytes.Buffer
	raw, _ := json.Marshal(k)
	b.Write(raw)
	return b.String()
}
