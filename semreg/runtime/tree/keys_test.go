package tree_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestKeyRoundTripAndNonCanonicalRejected(t *testing.T) {
	node := tree.NodePath{Installation: installation, Driver: "kind.a", Instance: "inst.1", Transport: "transport.1", Devices: []driver.NodeID{"device.1", "device.2"}}
	segment, err := tree.NativeSegment("ns_a", "member.1")
	if err != nil {
		t.Fatal(err)
	}
	key := tree.Key{Node: node, Facet: tree.KeywordAttributes, Domain: tree.KeywordNative, Segment: segment}
	parsed, err := tree.ParseKey(key.String())
	if err != nil {
		t.Fatalf("ParseKey(%s) = %v", key, err)
	}
	if parsed.String() != key.String() || parsed.Segment != segment {
		t.Fatalf("round trip = %s, want %s", parsed, key)
	}
	namespace, local, err := tree.ParseNativeSegment(parsed.Segment)
	if err != nil || namespace != "ns_a" || local != "member.1" {
		t.Fatalf("native segment = %q %q %v", namespace, local, err)
	}

	one, _ := tree.EncodeSegment("a")
	padded := one + "=="
	standard := base64.StdEncoding.EncodeToString([]byte{0x01, 0x61})
	trailing := "AWF"
	tuple, _ := tree.EncodeSegment("x", "y")
	device := tree.NodePath{Installation: installation, Driver: "kind.a", Instance: "inst.1"}.String() + "/devices/" + one
	cases := map[string]string{
		"padded segment":                 strings.Replace(key.String(), segment, segment+"==", 1),
		"standard alphabet":              "/installations/" + standard + "/instances/" + one + "/" + one,
		"trailing bits":                  "/installations/" + trailing,
		"padded installation":            "/installations/" + padded + "/instances/" + one + "/" + one,
		"missing leading slash":          strings.TrimPrefix(key.String(), "/"),
		"device directly under instance": device + "/attributes/native/" + segment,
		"unknown facet":                  node.String() + "/events/native/" + segment,
		"unknown domain":                 node.String() + "/attributes/other/" + segment,
		"native with one part":           node.String() + "/attributes/native/" + one,
		"native with three parts":        node.String() + "/attributes/native/" + mustSegment(t, "a", "b", "c"),
		"semantic method with two parts": node.String() + "/methods/sem/" + tuple,
		"trailing segment":               key.String() + "/extra",
		"empty segment":                  node.String() + "/attributes/native/",
	}
	for name, input := range cases {
		if _, err := tree.ParseKey(input); err == nil {
			t.Errorf("%s: ParseKey(%q) accepted a non-canonical key", name, input)
		}
	}
	for _, bad := range []string{"", "!", "AA", "AQ", "Ag"} {
		if _, err := tree.DecodeSegment(bad); err == nil {
			t.Errorf("DecodeSegment(%q) accepted a malformed tuple", bad)
		}
	}
	if _, err := tree.EncodeSegment(); err == nil {
		t.Error("EncodeSegment() accepted an empty tuple")
	}
	if _, err := tree.EncodeSegment("a", ""); err == nil {
		t.Error("EncodeSegment accepted an empty part")
	}
}

func mustSegment(t *testing.T, parts ...string) string {
	t.Helper()
	out, err := tree.EncodeSegment(parts...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNodePathAncestry(t *testing.T) {
	installationOnly := tree.NodePath{Installation: installation}
	if _, err := installationOnly.Child("transport.1"); err == nil {
		t.Fatal("a driver cannot declare a child of the installation")
	}
	instance := tree.NodePath{Installation: installation, Driver: "kind.a", Instance: "inst.1"}
	transport, err := instance.Child("transport.1")
	if err != nil {
		t.Fatal(err)
	}
	device, err := transport.Child("device.1")
	if err != nil {
		t.Fatal(err)
	}
	nested, err := device.Child("device.2")
	if err != nil {
		t.Fatal(err)
	}
	if !nested.Within(instance) || instance.Within(nested) {
		t.Fatal("Within does not follow the ancestry")
	}
	parsed, err := tree.ParseNodePath(nested.String())
	if err != nil || parsed.String() != nested.String() {
		t.Fatalf("ParseNodePath = %v, %v", parsed, err)
	}
	if _, err := tree.ParseNodePath(instance.String() + "/devices/" + mustSegment(t, "d")); err == nil {
		t.Fatal("a device directly under an instance was accepted")
	}
}

func TestDimensionedFactsHaveDistinctSemanticAddresses(t *testing.T) {
	w := newWorld(t)
	a := siteAsset("asset.a", "res:site", "res:grid1")
	a.facts = []semreg.FactKey{factKey("fact.power", "device.one"), factKey("fact.power", "device.two")}
	w.cuts.add(a)
	cut := w.capture()
	one, err := tree.FactSegment(a.facts[0])
	if err != nil {
		t.Fatal(err)
	}
	two, err := tree.FactSegment(a.facts[1])
	if err != nil {
		t.Fatal(err)
	}
	if one == two {
		t.Fatal("two dimensioned facts of one definition share a semantic address")
	}
	back, err := tree.ParseFactSegment(one)
	if err != nil {
		t.Fatal(err)
	}
	if back.FactID != a.facts[0].FactID || *back.Dimensions[0].Value.Text != "device.one" {
		t.Fatalf("fact segment is not reversible: %+v", back)
	}
	root := tree.NodePath{Installation: installation}
	refs := map[tree.Ref]bool{}
	for _, segment := range []string{one, two} {
		ref := tree.Ref(tree.Key{Node: root, Facet: tree.KeywordAttributes, Domain: tree.KeywordSem, Segment: segment}.String())
		refs[ref] = true
		value, err := cut.Read(ctx, ref, tree.ReadOptions{})
		if err != nil || value.Status != tree.StatusCurrent || len(value.Encoded) == 0 {
			t.Fatalf("Read(%s) = %+v, %v", ref, value, err)
		}
		graphql, err := tree.TreeName(tree.SurfaceGraphQL, tree.VerbRead)
		if err != nil {
			t.Fatal(err)
		}
		mcp, err := tree.TreeName(tree.SurfaceMCP, tree.VerbRead)
		if err != nil || graphql == mcp {
			t.Fatalf("names = %q %q %v", graphql, mcp, err)
		}
	}
	if len(refs) != 2 {
		t.Fatalf("addresses = %d, want 2", len(refs))
	}
	entries, err := cut.List(ctx, nodeRefOf(root), tree.Page{Size: 1000})
	if err != nil {
		t.Fatal(err)
	}
	listed := 0
	for ref := range refs {
		for _, d := range entries.Entries {
			if d.Ref == ref {
				listed++
			}
		}
	}
	if listed != 2 {
		t.Fatalf("installation lists %d of the 2 fact addresses", listed)
	}
}

func TestEveryKeyFieldChangesTheKey(t *testing.T) {
	base := tree.Key{
		Node:  tree.NodePath{Installation: "inst-a", Driver: "kind.a", Instance: "inst.1", Transport: "transport.1", Devices: []driver.NodeID{"device.1"}},
		Facet: tree.KeywordAttributes, Domain: tree.KeywordNative,
	}
	segment := func(ns, local string) string { return mustSegment(t, ns, local) }
	base.Segment = segment("ns_a", "member.1")
	with := func(change func(k *tree.Key)) tree.Key {
		k := base
		k.Node.Devices = append([]driver.NodeID(nil), base.Node.Devices...)
		change(&k)
		return k
	}
	mutations := map[string]tree.Key{
		"installation":     with(func(k *tree.Key) { k.Node.Installation = "inst-b" }),
		"driver kind":      with(func(k *tree.Key) { k.Node.Driver = "kind.b" }),
		"instance":         with(func(k *tree.Key) { k.Node.Instance = "inst.2" }),
		"transport":        with(func(k *tree.Key) { k.Node.Transport = "transport.2" }),
		"device":           with(func(k *tree.Key) { k.Node.Devices = []driver.NodeID{"device.2"} }),
		"nested device":    with(func(k *tree.Key) { k.Node.Devices = append(k.Node.Devices, "device.9") }),
		"facet":            with(func(k *tree.Key) { k.Facet = tree.KeywordMethods }),
		"namespace":        with(func(k *tree.Key) { k.Segment = segment("ns_b", "member.1") }),
		"local member id":  with(func(k *tree.Key) { k.Segment = segment("ns_a", "member.2") }),
		"namespace shifts": with(func(k *tree.Key) { k.Segment = segment("ns_a.member", ".1") }),
	}
	seen := map[string]string{base.String(): "base"}
	for name, key := range mutations {
		text := key.String()
		if other, dup := seen[text]; dup {
			t.Errorf("changing the %s gives the same key as %s", name, other)
		}
		seen[text] = name
		parsed, err := tree.ParseKey(text)
		if err != nil || parsed.String() != text {
			t.Errorf("%s: key does not round trip: %v", name, err)
		}
	}
	semantic := factKey("fact.power", "device.one")
	segmentOf := func(k semreg.FactKey) string {
		out, err := tree.FactSegment(k)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	other := func(change func(k *semreg.FactKey)) semreg.FactKey {
		k := factKey("fact.power", "device.one")
		change(&k)
		return k
	}
	facts := map[string]semreg.FactKey{
		"pack id":         other(func(k *semreg.FactKey) { k.PackID = "pack.other" }),
		"pack version":    other(func(k *semreg.FactKey) { k.PackVersion = "2.0.0" }),
		"fact id":         other(func(k *semreg.FactKey) { k.FactID = "fact.energy" }),
		"dimension id":    other(func(k *semreg.FactKey) { k.Dimensions[0].ID = "dimension.zone" }),
		"dimension value": other(func(k *semreg.FactKey) { k.Dimensions[0].Value.Text = text("device.two") }),
		"second dimension": other(func(k *semreg.FactKey) {
			k.Dimensions = append(k.Dimensions, semreg.Dimension{ID: "dimension.zone", Value: semreg.Value{Kind: semreg.ValueText, Text: text("zone.one")}})
		}),
	}
	taken := map[string]string{segmentOf(semantic): "base"}
	for name, key := range facts {
		segment := segmentOf(key)
		if other, dup := taken[segment]; dup {
			t.Errorf("changing the %s gives the same semantic address as %s", name, other)
		}
		taken[segment] = name
		back, err := tree.ParseFactSegment(segment)
		if err != nil || segmentOf(back) != segment {
			t.Errorf("%s: semantic segment does not round trip: %v", name, err)
		}
	}
	pack := semreg.PackRef{ID: "pack.test", Version: "1.0.0"}
	ref := semreg.DefinitionRef{Pack: pack, ID: "operation.set", Version: "1.0.0"}
	definition := func(change func(r *semreg.DefinitionRef)) string {
		r := ref
		change(&r)
		out, err := tree.DefinitionSegment(r)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	methods := map[string]string{
		"base":         definition(func(*semreg.DefinitionRef) {}),
		"pack id":      definition(func(r *semreg.DefinitionRef) { r.Pack.ID = "pack.other" }),
		"pack version": definition(func(r *semreg.DefinitionRef) { r.Pack.Version = "2.0.0" }),
		"operation id": definition(func(r *semreg.DefinitionRef) { r.ID = "operation.other" }),
		"version":      definition(func(r *semreg.DefinitionRef) { r.Version = "2.0.0" }),
	}
	distinct := map[string]string{}
	for name, segment := range methods {
		if other, dup := distinct[segment]; dup {
			t.Errorf("method %s collides with %s", name, other)
		}
		distinct[segment] = name
	}
}
