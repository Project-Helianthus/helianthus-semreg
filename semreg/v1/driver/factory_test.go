package driver_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/catalog"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func planFor(c driver.Config) []driver.InstancePlan {
	return []driver.InstancePlan{{Driver: "driver.test", Instance: driver.InstanceKey(c.Instance)}}
}

func TestConfigIsOneRecordWithTwoOwnedOperandsAndAReference(t *testing.T) {
	typ := reflect.TypeOf(driver.Config{})
	want := []struct {
		name string
		typ  reflect.Type
	}{
		{"Instance", reflect.TypeOf("")}, {"Enabled", reflect.TypeOf(true)},
		{"Transport", reflect.TypeOf(json.RawMessage{})}, {"Options", reflect.TypeOf(json.RawMessage{})}, {"SecretRef", reflect.TypeOf("")},
	}
	if typ.NumField() != len(want) {
		t.Fatalf("Config has %d fields", typ.NumField())
	}
	for i, w := range want {
		if f := typ.Field(i); f.Name != w.name || f.Type != w.typ {
			t.Errorf("field %d is %s %s, want %s %s", i, f.Name, f.Type, w.name, w.typ)
		}
	}
}

// Plan is pure: it keeps the instance key and the enabled flag, never reads the
// secret reference, and the two operands stay separate from it.
func TestPlanDoesNotTouchTheSecretReference(t *testing.T) {
	var seen driver.Config
	f := syntheticFactory{plan: func(c driver.Config) []driver.InstancePlan { seen = c; return planFor(c) }}
	cfg := driver.Config{Instance: "instance.one", Enabled: true, Transport: json.RawMessage(`{"endpoint":"one"}`), Options: json.RawMessage(`{"mode":"x"}`), SecretRef: "ref.one"}
	plans, err := f.Plan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, cfg) {
		t.Errorf("factory received %+v", seen)
	}
	if len(plans) != 1 || plans[0].Instance != "instance.one" {
		t.Fatalf("plans = %+v", plans)
	}
	encoded, _ := json.Marshal(plans)
	if strings.Contains(string(encoded), "ref.one") {
		t.Error("the plan carries the secret reference")
	}
	omitted := driver.Config{Instance: "instance.two", Enabled: true}
	if omitted.SecretRef != "" {
		t.Error("an omitted reference is not empty")
	}
	if got, _ := f.Plan(driver.Config{Instance: "instance.three"}); len(got) != 0 {
		t.Errorf("a disabled instance planned: %v", got)
	}
}

func TestRetryPolicyCarriesEveryField(t *testing.T) {
	p := driver.RetryPolicy{
		Budget: 5, InitialDelay: time.Second, MaxDelay: time.Minute, JitterRatio: 0.25,
		Persistent: driver.PersistentRetry{Enabled: true, InitialDelay: 2 * time.Second, MaxDelay: 3 * time.Minute},
	}
	plan := driver.InstancePlan{Driver: "driver.test", Instance: "instance.one", RetryPolicy: p}
	wantValid(t, "plan with a retry policy", plan.Validate())
	if plan.RetryPolicy != p {
		t.Fatalf("retry policy = %+v", plan.RetryPolicy)
	}
	names := func(v any) []string {
		var out []string
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			out = append(out, typ.Field(i).Name)
		}
		return out
	}
	if got := names(driver.RetryPolicy{}); !reflect.DeepEqual(got, []string{"Budget", "InitialDelay", "MaxDelay", "JitterRatio", "Persistent"}) {
		t.Errorf("RetryPolicy fields = %v", got)
	}
	if got := names(driver.PersistentRetry{}); !reflect.DeepEqual(got, []string{"Enabled", "InitialDelay", "MaxDelay"}) {
		t.Errorf("PersistentRetry fields = %v", got)
	}
	retry := reflect.TypeOf(driver.RetryPolicy{})
	for name, want := range map[string]reflect.Type{"Budget": reflect.TypeOf(0), "InitialDelay": reflect.TypeOf(time.Duration(0)), "MaxDelay": reflect.TypeOf(time.Duration(0)), "JitterRatio": reflect.TypeOf(0.0)} {
		if f, _ := retry.FieldByName(name); f.Type != want {
			t.Errorf("%s has type %s", name, f.Type)
		}
	}
}

func TestEarlyDispatcherIsOptional(t *testing.T) {
	plain := syntheticFactory{plan: planFor}
	if _, ok := any(plain).(driver.EarlyDispatcher); ok {
		t.Fatal("a factory without early dispatch implements it")
	}
	early := &earlyFactory{syntheticFactory: plain}
	var factory driver.Factory = early
	dispatcher, ok := factory.(driver.EarlyDispatcher)
	if !ok {
		t.Fatal("the factory with early dispatch does not implement it")
	}
	handled, code, err := dispatcher.DispatchEarly(context.Background(), []string{"child", "--flag"})
	if !handled || code != 3 || err != nil {
		t.Errorf("handled exit: %v %d %v", handled, code, err)
	}
	if handled, _, err := dispatcher.DispatchEarly(context.Background(), []string{"fail"}); !handled || err == nil {
		t.Errorf("handled error: %v %v", handled, err)
	}
	if handled, code, err := dispatcher.DispatchEarly(context.Background(), []string{"serve"}); handled || code != 0 || err != nil {
		t.Errorf("not handled: %v %d %v", handled, code, err)
	}
	if !reflect.DeepEqual(early.args, []string{"serve"}) {
		t.Errorf("arguments = %v", early.args)
	}
}

func TestInstancePlanTransportIsOptional(t *testing.T) {
	wantValid(t, "plan with no transport", driver.InstancePlan{Driver: "driver.test", Instance: "instance.one"}.Validate())
	wantValid(t, "plan with an empty transport list", driver.InstancePlan{Driver: "driver.test", Instance: "instance.one", Transports: []driver.TransportClaim{}}.Validate())
	one := driver.TransportClaim{Provider: "provider.test", ConfigKey: "config.one", AccessDomain: "domain.one", Share: driver.ShareSerialized}
	wantValid(t, "plan with a transport", driver.InstancePlan{Driver: "driver.test", Instance: "instance.one", Transports: []driver.TransportClaim{one}}.Validate())
	wantInvalid(t, "plan without a driver kind", driver.InstancePlan{Instance: "instance.one"}.Validate())
	wantInvalid(t, "plan without an instance key", driver.InstancePlan{Driver: "driver.test"}.Validate())
	wantInvalid(t, "plan with an invalid transport", driver.InstancePlan{Driver: "driver.test", Instance: "instance.one", Transports: []driver.TransportClaim{one, {}}}.Validate())
}

func TestThreePlansOfOneKindAreValid(t *testing.T) {
	f := syntheticFactory{plan: func(c driver.Config) []driver.InstancePlan {
		var plans []driver.InstancePlan
		for _, key := range []driver.InstanceKey{"bus.one", "bus.two", "bus.three"} {
			plans = append(plans, driver.InstancePlan{Driver: "driver.test", Instance: key})
		}
		return plans
	}}
	plans, err := f.Plan(driver.Config{Instance: "set", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[driver.InstanceKey]bool{}
	for _, p := range plans {
		wantValid(t, string(p.Instance), p.Validate())
		if p.Driver != "driver.test" || seen[p.Instance] {
			t.Errorf("plan %+v", p)
		}
		seen[p.Instance] = true
	}
	if len(seen) != 3 {
		t.Errorf("instances = %v", seen)
	}
}

func TestTransportClaimLinkNodeSlot(t *testing.T) {
	base := driver.TransportClaim{Provider: "provider.test", ConfigKey: "config.one", AccessDomain: "domain.one", Share: driver.ShareExclusive}
	wantValid(t, "empty slot keeps the ordinary configured key", base.Validate())
	slotted := base
	slotted.LinkNodeSlot = "slot.link"
	wantValid(t, "populated slot", slotted.Validate())
	if base.LinkNodeSlot != "" {
		t.Fatal("the base claim carries a slot")
	}
	// The slot is equipment identity only: the transport members are unchanged.
	if base.Provider != slotted.Provider || base.ConfigKey != slotted.ConfigKey || base.AccessDomain != slotted.AccessDomain || base.Share != slotted.Share {
		t.Error("the slot changed the transport members")
	}
	for _, p := range []driver.InstancePlan{
		{Driver: "driver.test", Instance: "instance.one", Transports: []driver.TransportClaim{base}},
		{Driver: "driver.test", Instance: "instance.one", Transports: []driver.TransportClaim{slotted}},
	} {
		inst, err := syntheticFactory{plan: planFor}.Construct(context.Background(), p)
		if err != nil || inst == nil {
			t.Errorf("construct %+v: %v", p.Transports, err)
		}
	}
}

func TestTransportClaimRules(t *testing.T) {
	good := driver.TransportClaim{Provider: "provider.test", ConfigKey: "config.one", AccessDomain: "domain.one", Share: driver.ShareSerialized}
	wantValid(t, "serialized", good.Validate())
	if driver.ShareSerialized == driver.ShareExclusive {
		t.Fatal("the two share modes are equal")
	}
	for name, mutate := range map[string]func(*driver.TransportClaim){
		"provider":           func(c *driver.TransportClaim) { c.Provider = "" },
		"config key":         func(c *driver.TransportClaim) { c.ConfigKey = "" },
		"access domain":      func(c *driver.TransportClaim) { c.AccessDomain = "" },
		"share mode":         func(c *driver.TransportClaim) { c.Share = "" },
		"unknown share mode": func(c *driver.TransportClaim) { c.Share = "shared" },
	} {
		c := good
		mutate(&c)
		wantInvalid(t, "claim with invalid "+name, c.Validate())
	}
}

// SS01 has no planner and adds no collision check: two claims of the same
// provider, config key and access domain, one exclusive and one carrying another
// slot, are each valid records. SS04 rejects the collision when planning.
func TestTransportClaimValidationAddsNoCollisionRule(t *testing.T) {
	a := driver.TransportClaim{Provider: "provider.test", ConfigKey: "config.one", AccessDomain: "domain.one", Share: driver.ShareExclusive}
	b := a
	b.Share = driver.ShareSerialized
	b.LinkNodeSlot = "slot.link"
	plan := driver.InstancePlan{Driver: "driver.test", Instance: "instance.one", Transports: []driver.TransportClaim{a, b}}
	wantValid(t, "plan holding both claims", plan.Validate())
}

func TestDescriptorCarriesTheCatalogFamilyRecord(t *testing.T) {
	d := driver.Descriptor{Families: []catalog.Family{sourceBackedFamily()}}
	wantValid(t, "descriptor", d.Validate())
	if got := d.Families[0].Module; got != "github.com/Project-Helianthus/example-reg" {
		t.Errorf("module = %q", got)
	}
	declared := syntheticFactory{families: d.Families}
	if got := declared.Descriptor().Families[0].FamilyID; got != "example-family" {
		t.Errorf("family = %q", got)
	}
	wantValid(t, "descriptor with no family", driver.Descriptor{}.Validate())

	unspecified := sourceBackedFamily()
	unspecified.Evidence = unspecified.Evidence[1:]
	wantInvalid(t, "qualified family without a specification entry", driver.Descriptor{Families: []catalog.Family{unspecified}}.Validate())
	unverified := sourceBackedFamily()
	unverified.CompositionVerified = false
	wantInvalid(t, "claimed composition not verified", driver.Descriptor{Families: []catalog.Family{sourceBackedFamily(), unverified}}.Validate())
	noModule := sourceBackedFamily()
	noModule.Module = ""
	wantInvalid(t, "claimed composition without a module", driver.Descriptor{Families: []catalog.Family{noModule}}.Validate())
}

func TestDescriptorDeclaresDriverKinds(t *testing.T) {
	wantValid(t, "descriptor with kinds", driver.Descriptor{Kinds: []driver.DriverKind{"driver.test"}}.Validate())
	wantInvalid(t, "descriptor with an empty kind", driver.Descriptor{Kinds: []driver.DriverKind{"driver.test", ""}}.Validate())
}

func TestInstanceLifecycleSignatures(t *testing.T) {
	f := syntheticFactory{plan: planFor}
	inst, err := f.Construct(context.Background(), driver.InstancePlan{Driver: "driver.test", Instance: "instance.one"})
	if err != nil {
		t.Fatal(err)
	}
	session := newSession()
	if err := inst.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	inst.FenceWithdrawal()
	if report := inst.Stop(context.Background()); !report.Confirmed || report.Err != nil {
		t.Errorf("close report = %+v", report)
	}
	if !inst.(*syntheticInstance).fenced || inst.(*syntheticInstance).session != driver.Session(session) {
		t.Error("the instance did not keep the fence and the session")
	}
	unconfirmed := driver.CloseReport{Err: context.DeadlineExceeded}
	if unconfirmed.Confirmed {
		t.Error("an unconfirmed close reads as confirmed")
	}
}
