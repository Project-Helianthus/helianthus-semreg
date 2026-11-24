package identity

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

const (
	installation = "inst-a"
	bridge       = driver.BridgeID("bridge.local")

	idInstance1  = "resource:e1939cf97c2edcd9f31d7fe41e4622c9"
	idTransport1 = "resource:22e6d4b74df6a6b30b0fbbeb7e9fb14d"
	idInstance2  = "resource:d1853e3731f4caeb41e3c2b3a98cfa38"
	idTransport2 = "resource:07dc99873ee423825acdc6af9352d6be"
	idInstance3  = "resource:38e413013000dd80534499201a034f7d"
	idTransport3 = "resource:d66342694c763db6ab618fba84ea8ecc"
	idBus        = "resource:ae278489067880417278188da4b47bd1"
	idDevice1    = "resource:178c8bc3622efa7ebf7b50fa71aebe9c"
	idSerialOld  = "resource:bc5f7e951b91117ca180eb070e494406"
	idSerialNew  = "resource:fa68938ff9b76a5992c04f57381d2566"
	idTarget     = "resource:ac423d6ba9f81e7853c45ef879517ab7"
	idSite       = "resource:d6e1ccab64c9306c13621a001224bcad"
)

var (
	modbus = func(instance string) Publisher {
		return Publisher{Driver: "driver-modbus", Instance: driver.InstanceKey(instance)}
	}
	busPublisher = Publisher{Driver: "driver-bus", Instance: "b1"}
	reach        = semreg.DefinitionRef{Pack: semreg.PackRef{ID: "pack.test", Version: "1.0.0"}, ID: "relation.test_reach", Version: "1.0.0"}
)

func ref(id string) driver.NodeRef { return driver.NodeRef{Bridge: bridge, ID: semreg.ResourceID(id)} }

// topology is the recorded installation of the lifecycle tests: three instances
// of one driver kind, one device reached through a shared bus link and two direct
// links, a serial that changed, and a link to a target that is not there yet.
type topology struct {
	t       *testing.T
	proof   string
	nodes   []func(*Ledger)
	claims  []func(*Ledger)
	returns []func(*Ledger)
}

func newTopology(t *testing.T, proofLabel string) *topology {
	topo := &topology{t: t, proof: proofLabel}
	node := func(key driver.NodeKey) func(*Ledger) {
		return func(l *Ledger) { mustNode(t, l, bridge, key) }
	}
	for _, m := range []string{"m1", "m2", "m3"} {
		topo.nodes = append(topo.nodes,
			node(slotKey(installation, "driver-modbus", driver.InstanceKey(m), "resource.instance", "", "instance")),
			node(slotKey(installation, "driver-modbus", driver.InstanceKey(m), "resource.transport", "", "transport")))
	}
	topo.nodes = append(topo.nodes,
		node(slotKey(installation, "driver-bus", "b1", "resource.bus", "", "bus")),
		node(nativeKey(t, installation, "driver-modbus", "m1", "resource.equipment", "dev-1")),
		node(nativeKey(t, installation, "driver-modbus", "m2", "resource.equipment", "dev-1")),
		node(nativeKey(t, installation, "driver-bus", "b1", "resource.equipment", "dev-1")),
		node(nativeKey(t, installation, "driver-modbus", "m3", "resource.equipment", "sn-old")),
		node(nativeKey(t, installation, "driver-modbus", "m3", "resource.equipment", "sn-new")))
	claim := func(p Publisher, from, to string) func(*Ledger) {
		return func(l *Ledger) {
			if err := l.ClaimLink(p, driver.LinkClaim{Key: driver.LinkKey{Type: reach, From: ref(from), To: ref(to)}, Basis: proof(topo.proof + from + to)}); err != nil {
				t.Fatalf("ClaimLink: %v", err)
			}
		}
	}
	source := func(producer string, priority int32) func(*Ledger) {
		return func(l *Ledger) {
			err := l.RecordSource(driver.Source{
				For:      driver.Element{Subject: ref(idDevice1), Key: semreg.FactKey{PackID: "pack.test", PackVersion: "1.0.0", FactID: "fact.flow", Dimensions: []semreg.Dimension{}}},
				Producer: ref(producer), PathKey: "path-" + producer, Priority: priority,
			})
			if err != nil {
				t.Fatalf("RecordSource: %v", err)
			}
		}
	}
	topo.claims = []func(*Ledger){
		claim(busPublisher, idBus, idDevice1),
		claim(modbus("m1"), idInstance1, idDevice1),
		claim(modbus("m2"), idInstance2, idDevice1),
		claim(modbus("m3"), idTransport3, idSerialOld),
		claim(modbus("m1"), idInstance1, idTarget),
		claim(busPublisher, idInstance1, idTarget),
		source(idInstance1, 10),
		source(idBus, 20),
	}
	topo.returns = []func(*Ledger){func(l *Ledger) {
		mustNode(t, l, bridge, nativeKey(t, installation, "driver-bus", "b1", "resource.equipment", "tgt-1"))
	}}
	return topo
}

// publish runs the nodes and then the claims in the order given, discovery of
// the missing target last.
func (topo *topology) publish(l *Ledger, reversed bool) {
	for _, steps := range [][]func(*Ledger){topo.nodes, topo.claims, topo.returns} {
		if reversed {
			for i := len(steps) - 1; i >= 0; i-- {
				steps[i](l)
			}
		} else {
			for _, step := range steps {
				step(l)
			}
		}
	}
}

// graphOf is the canonical content compared between lifecycles: sorted node
// ids, link keys with endpoints, types and publishers, member keys and source
// links with priorities. Timestamps, leases, cut digests and proofs are not in it.
type graphOf struct {
	Nodes   []string
	Members []string
	Links   []string
	Sources []string
}

func readGraph(t *testing.T, l *Ledger) graphOf {
	t.Helper()
	var g graphOf
	l.mu.Lock()
	for node, keys := range l.nodes {
		g.Nodes = append(g.Nodes, string(node.ID))
		for _, key := range keys {
			g.Members = append(g.Members, string(node.ID)+" "+string(key.Driver)+"/"+string(key.Instance)+" "+string(key.Kind))
		}
	}
	l.mu.Unlock()
	for _, link := range l.Links() {
		var publishers []string
		for _, c := range link.Claims {
			publishers = append(publishers, string(c.Publisher.Driver)+"/"+string(c.Publisher.Instance))
		}
		g.Links = append(g.Links, string(link.Key.Type.ID)+" "+string(link.Key.From.ID)+" "+string(link.Key.To.ID)+" "+strings.Join(publishers, ","))
	}
	for _, s := range l.Sources() {
		g.Sources = append(g.Sources, string(s.Producer.ID)+" "+s.PathKey+" "+string(s.For.Subject.ID)+" "+itoa(s.Priority))
	}
	sort.Strings(g.Nodes)
	sort.Strings(g.Members)
	sort.Strings(g.Links)
	sort.Strings(g.Sources)
	return g
}

func itoa(n int32) string { return strconv.Itoa(int(n)) }

// expectedGraph is written by hand from the independent derivation of the ids.
func expectedGraph() graphOf {
	g := graphOf{
		Nodes: []string{idInstance1, idTransport1, idInstance2, idTransport2, idInstance3, idTransport3, idBus, idDevice1, idSerialOld, idSerialNew, idTarget},
		Members: []string{
			idInstance1 + " driver-modbus/m1 resource.instance",
			idTransport1 + " driver-modbus/m1 resource.transport",
			idInstance2 + " driver-modbus/m2 resource.instance",
			idTransport2 + " driver-modbus/m2 resource.transport",
			idInstance3 + " driver-modbus/m3 resource.instance",
			idTransport3 + " driver-modbus/m3 resource.transport",
			idBus + " driver-bus/b1 resource.bus",
			idDevice1 + " driver-bus/b1 resource.equipment",
			idDevice1 + " driver-modbus/m1 resource.equipment",
			idDevice1 + " driver-modbus/m2 resource.equipment",
			idSerialOld + " driver-modbus/m3 resource.equipment",
			idSerialNew + " driver-modbus/m3 resource.equipment",
			idTarget + " driver-bus/b1 resource.equipment",
		},
		Links: []string{
			"relation.test_reach " + idBus + " " + idDevice1 + " driver-bus/b1",
			"relation.test_reach " + idInstance1 + " " + idDevice1 + " driver-modbus/m1",
			"relation.test_reach " + idInstance2 + " " + idDevice1 + " driver-modbus/m2",
			"relation.test_reach " + idTransport3 + " " + idSerialOld + " driver-modbus/m3",
			"relation.test_reach " + idInstance1 + " " + idTarget + " driver-bus/b1,driver-modbus/m1",
		},
		Sources: []string{
			idBus + " path-" + idBus + " " + idDevice1 + " 20",
			idInstance1 + " path-" + idInstance1 + " " + idDevice1 + " 10",
		},
	}
	sort.Strings(g.Nodes)
	sort.Strings(g.Members)
	sort.Strings(g.Links)
	sort.Strings(g.Sources)
	return g
}

func TestTwoLifecyclesGiveTheSameGraph(t *testing.T) {
	catalog := embeddedCatalog(t)
	want := expectedGraph()

	// Lifecycle one: discovery in recorded order over a fresh in-memory store.
	first := ledgerOn(t, NewMemoryStore(), catalog)
	newTopology(t, "lifecycle-one ").publish(first, false)
	if got := readGraph(t, first); !reflect.DeepEqual(got, want) {
		t.Fatalf("lifecycle one differs from the hand-written graph:\n got %+v\nwant %+v", got, want)
	}

	// Lifecycle two: independent initial allocation, factories and discovery in
	// reverse order, other proofs standing for other clocks and generations.
	second := ledgerOn(t, NewMemoryStore(), catalog)
	newTopology(t, "lifecycle-two-later ").publish(second, true)
	if got := readGraph(t, second); !reflect.DeepEqual(got, want) {
		t.Fatalf("independent allocation differs from the hand-written graph:\n got %+v\nwant %+v", got, want)
	}

	// Lifecycle three: the file store is written in reverse order, closed and
	// reopened, and the same keys are allocated again in recorded order.
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	newTopology(t, "lifecycle-three ").publish(ledgerOn(t, store, catalog), true)
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	again := ledgerOn(t, reopened, catalog)
	newTopology(t, "lifecycle-four ").publish(again, false)
	if got := readGraph(t, again); !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened store differs from the hand-written graph:\n got %+v\nwant %+v", got, want)
	}
}
