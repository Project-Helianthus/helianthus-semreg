package driver_test

import (
	"context"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func memberKey() driver.MemberKey {
	return driver.MemberKey{Namespace: "namespace.test", Instance: "instance.one", Node: "node.one", Local: "member.one"}
}

func TestNamespaceMemberKeyAndMember(t *testing.T) {
	wantValid(t, "namespace", driver.Namespace{Owner: "driver.test", Key: "namespace.test"}.Validate())
	wantInvalid(t, "namespace without owner", driver.Namespace{Key: "namespace.test"}.Validate())
	wantInvalid(t, "namespace without key", driver.Namespace{Owner: "driver.test"}.Validate())

	wantValid(t, "member key", memberKey().Validate())
	for name, mutate := range map[string]func(*driver.MemberKey){
		"namespace": func(k *driver.MemberKey) { k.Namespace = "" },
		"instance":  func(k *driver.MemberKey) { k.Instance = "" },
		"node":      func(k *driver.MemberKey) { k.Node = "" },
		"local":     func(k *driver.MemberKey) { k.Local = "" },
	} {
		k := memberKey()
		mutate(&k)
		wantInvalid(t, "member key without "+name, k.Validate())
	}
	// Two instances of one driver address two members under one namespace.
	other := memberKey()
	other.Instance = "instance.two"
	if memberKey() == other {
		t.Error("two instances share one member key")
	}

	member := driver.Member{Key: memberKey(), Schema: "schema.test", Mode: "read", Scope: "scope.test"}
	wantValid(t, "member", member.Validate())
	for name, mutate := range map[string]func(*driver.Member){
		"key":    func(m *driver.Member) { m.Key = driver.MemberKey{} },
		"schema": func(m *driver.Member) { m.Schema = "" },
		"mode":   func(m *driver.Member) { m.Mode = "" },
		"scope":  func(m *driver.Member) { m.Scope = "" },
	} {
		m := member
		mutate(&m)
		wantInvalid(t, "member without "+name, m.Validate())
	}
}

func TestNativeRegistrySignaturesAndSpecs(t *testing.T) {
	native := &syntheticNative{}
	var registry driver.NativeRegistry = native
	parent, err := registry.Node(driver.NodeSpec{Kind: "resource.equipment", Local: "link", Contract: "schema.link"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := registry.Node(driver.NodeSpec{Parent: &parent, Kind: "resource.function", Local: "meter", Contract: "schema.meter", Subject: &parent})
	if err != nil {
		t.Fatal(err)
	}
	read := driver.NativeReader(func(context.Context) ([]byte, error) { return []byte(`{"v":1}`), nil })
	if err := registry.ReadMember(child, driver.MemberSpec{
		Local: "voltage", Schema: "schema.voltage", Units: "unit.volt", Mode: "read", Visibility: "diagnostic", Channel: "channel.a", Scope: "scope.test",
		Bounds: &driver.ValueLimits{Max: quantity("5", 2, "unit.volt"), Origin: driver.LimitSpecification, Evidence: evidenceSet("clause")},
	}, read); err != nil {
		t.Fatal(err)
	}
	call := driver.NativeMethod(func(_ context.Context, in []byte) ([]byte, error) { return in, nil })
	if err := registry.Method(child, driver.MethodSpec{Local: "reset", Schema: "schema.reset", Scope: "scope.test"}, call); err != nil {
		t.Fatal(err)
	}
	if len(native.nodes) != 2 || len(native.members) != 1 || len(native.methods) != 1 {
		t.Fatalf("registered %d nodes, %d members, %d methods", len(native.nodes), len(native.members), len(native.methods))
	}
	got, err := native.readers["voltage"](context.Background())
	if err != nil || string(got) != `{"v":1}` {
		t.Errorf("read = %q, %v", got, err)
	}
	if out, err := native.invoke["reset"](context.Background(), []byte("in")); err != nil || string(out) != "in" {
		t.Errorf("method = %q, %v", out, err)
	}
	if _, err := registry.Node(driver.NodeSpec{}); err == nil {
		t.Error("an empty node spec was registered")
	}
}

func TestNativeSpecRules(t *testing.T) {
	node := driver.NodeSpec{Kind: "resource.equipment", Local: "link", Contract: "schema.link"}
	wantValid(t, "node spec", node.Validate())
	for name, mutate := range map[string]func(*driver.NodeSpec){
		"kind":     func(s *driver.NodeSpec) { s.Kind = "" },
		"local":    func(s *driver.NodeSpec) { s.Local = "" },
		"contract": func(s *driver.NodeSpec) { s.Contract = "" },
		"parent":   func(s *driver.NodeSpec) { s.Parent = &driver.NodeRef{} },
		"subject":  func(s *driver.NodeSpec) { s.Subject = &driver.NodeRef{} },
	} {
		s := node
		mutate(&s)
		wantInvalid(t, "node spec with invalid "+name, s.Validate())
	}

	member := driver.MemberSpec{Local: "voltage", Schema: "schema.voltage", Mode: "read", Scope: "scope.test"}
	wantValid(t, "member spec", member.Validate())
	for name, mutate := range map[string]func(*driver.MemberSpec){
		"local":  func(s *driver.MemberSpec) { s.Local = "" },
		"schema": func(s *driver.MemberSpec) { s.Schema = "" },
		"mode":   func(s *driver.MemberSpec) { s.Mode = "" },
		"scope":  func(s *driver.MemberSpec) { s.Scope = "" },
		"units":  func(s *driver.MemberSpec) { s.Units = "!" },
		"bounds": func(s *driver.MemberSpec) {
			s.Bounds = &driver.ValueLimits{Origin: "guess", Evidence: evidenceSet("x")}
		},
	} {
		s := member
		mutate(&s)
		wantInvalid(t, "member spec with invalid "+name, s.Validate())
	}

	method := driver.MethodSpec{Local: "reset", Schema: "schema.reset", Scope: "scope.test"}
	wantValid(t, "method spec", method.Validate())
	for name, mutate := range map[string]func(*driver.MethodSpec){
		"local":  func(s *driver.MethodSpec) { s.Local = "" },
		"schema": func(s *driver.MethodSpec) { s.Schema = "" },
		"scope":  func(s *driver.MethodSpec) { s.Scope = "" },
	} {
		s := method
		mutate(&s)
		wantInvalid(t, "method spec with invalid "+name, s.Validate())
	}
	_ = v1.DefinitionID("")
}
