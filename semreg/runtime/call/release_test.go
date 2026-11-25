package call

import (
	"context"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
)

func watt(coefficient string) semreg.Value { return qty(coefficient, "unit.watt") }

func wattExp(coefficient string, exp int32) semreg.Value {
	return qtyExp(coefficient, exp, "unit.watt")
}

func pvSpec(value semreg.Value) publishSpec {
	return publishSpec{
		name: "pv", validator: pv.New(), operation: "pv.operation.set_active_power_limit",
		resource: "resource:inverter:01", asset: "asset:pv", value: value,
	}
}

func TestReleaseReachesTheSettingOwner(t *testing.T) {
	cases := []struct {
		name, operation, fact, effect, dimension, resource, asset string
		validator                                                 semreg.PackValidator
		value                                                     semreg.Value
	}{
		{"pv", "pv.operation.set_active_power_limit", "pv.limit.active_power_enabled", "pv.effect.release_active_power_limit", "pv.dimension.inverter", "resource:inverter:01", "asset:pv", pv.New(), wattExp("1", 3)},
		{"evse", "evse.operation.set_allocated_current", "evse.limit.allocated_current_enabled", "evse.effect.release_allocated_current", "evse.dimension.connector", "resource:connector:01", "asset:evse", evse.New(), ampere("16")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newRig(t, publishSpec{name: tc.name, validator: tc.validator, operation: semreg.DefinitionID(tc.operation), resource: semreg.ResourceID(tc.resource), asset: semreg.AssetID(tc.asset), value: tc.value})
			falseValue := booleanValue(false)
			result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: semreg.DefinitionID(tc.fact), Value: falseValue}}, 20*time.Second))
			calls, sends := rig.exec.counts()
			if calls != 1 || sends != 1 || result.Status != StatusProtocolAck {
				t.Fatalf("calls %d sends %d result %#v", calls, sends, result)
			}
			effect := rig.exec.last.ExpectedEffect
			if effect.Rule.ID != semreg.DefinitionID(tc.effect) || effect.Operator != semreg.PredicateEqual || effect.Fact.FactID != semreg.DefinitionID(tc.fact) || effect.Fact.Dimensions[0].ID != semreg.DefinitionID(tc.dimension) {
				t.Fatalf("effect %#v", effect)
			}
			if effect.Expected.Boolean == nil || *effect.Expected.Boolean || len(rig.exec.last.Request.Arguments) != 1 {
				t.Fatalf("release value %#v args %#v", effect.Expected, rig.exec.last.Request.Arguments)
			}
			if rig.exec.last.Route.BindingID != rig.plant.binding || rig.exec.last.Route.CapabilityInstance != rig.plant.capability {
				t.Fatalf("route %#v", rig.exec.last.Route)
			}
		})
	}
}

func TestEqualNumericValueStaysTheSetCase(t *testing.T) {
	rig := newRig(t, pvSpec(wattExp("1", 3)))
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: "pv.limit.active_power", Value: wattExp("1", 3)}}, 20*time.Second))
	if result.Status != StatusProtocolAck || rig.exec.last.ExpectedEffect.Rule.ID != "pv.effect.set_active_power_limit" {
		t.Fatalf("%#v effect %#v", result, rig.exec.last.ExpectedEffect)
	}
	maximum := newRig(t, pvSpec(wattExp("1", 7)))
	maxResult := maximum.invoke(nil, maximum.plant.request([]semreg.TypedField{{ID: "pv.limit.active_power", Value: wattExp("1", 7)}}, 20*time.Second))
	if maxResult.Status != StatusProtocolAck || maximum.exec.last.ExpectedEffect.Rule.ID != "pv.effect.set_active_power_limit" {
		t.Fatalf("maximum %#v effect %#v", maxResult, maximum.exec.last.ExpectedEffect)
	}
}

func TestEvsePauseStaysTheSetCase(t *testing.T) {
	rig := newRig(t, evseSpec(ampere("0")))
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: "evse.limit.allocated_current", Value: ampere("0")}}, 20*time.Second))
	calls, _ := rig.exec.counts()
	if calls != 1 || result.Status != StatusProtocolAck || rig.exec.last.ExpectedEffect.Rule.ID != "evse.effect.set_allocated_current" {
		t.Fatalf("calls %d result %#v effect %#v", calls, result, rig.exec.last.ExpectedEffect)
	}
	if rig.exec.last.Request.Arguments[0].Value.Quantity.Number.Coefficient != "0" {
		t.Fatalf("pause %#v", rig.exec.last.Request.Arguments[0])
	}
}

func TestReleaseGuardChangeSendsNothing(t *testing.T) {
	rig := newRig(t, pvSpec(wattExp("1", 3)))
	second := rig.plant.snapshot
	sources := append([]semreg.SourceDescriptor(nil), second.Sources...)
	sources[0].SourceEpochID = "epoch:other"
	second.Sources = sources
	rig.capture.reset([]driver.SemanticCut{
		rig.plant.cut([]driver.SettingRoute{rig.plant.settingRoute(nil)}, rig.plant.snapshot),
		rig.plant.cut([]driver.SettingRoute{rig.plant.settingRoute(nil)}, second),
	})
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: "pv.limit.active_power_enabled", Value: booleanValue(false)}}, 20*time.Second))
	assertIdle(t, rig.exec)
	if result.Status != StatusFailed || result.Delivery != operation.DeliveryNotSent {
		t.Fatalf("%#v", result)
	}
}

func TestReleaseRejectionsSendNothing(t *testing.T) {
	cases := []struct {
		name   string
		spec   publishSpec
		args   []semreg.TypedField
		reason string
		skip   bool
		clear  bool
	}{
		{name: "true", spec: pvSpec(wattExp("1", 3)), args: []semreg.TypedField{{ID: "pv.limit.active_power_enabled", Value: booleanValue(true)}}, reason: ReasonReleaseTrue},
		{name: "mixed", spec: pvSpec(wattExp("1", 3)), args: []semreg.TypedField{{ID: "pv.limit.active_power", Value: wattExp("1", 3)}, {ID: "pv.limit.active_power_enabled", Value: booleanValue(false)}}, reason: ReasonMixedRelease},
		{name: "negative pv", spec: pvSpec(wattExp("1", 3)), args: []semreg.TypedField{{ID: "pv.limit.active_power", Value: watt("-1")}}, reason: ReasonNegativeSet},
		{name: "negative evse", spec: evseSpec(ampere("16")), args: []semreg.TypedField{{ID: "evse.limit.allocated_current", Value: ampere("-1")}}, reason: ReasonNegativeSet},
		{name: "no release case", spec: thermalSpec(), args: []semreg.TypedField{{ID: "thermal.limit.enabled", Value: booleanValue(false)}}, reason: ReasonNoReleaseCase},
		{name: "missing owner", spec: pvSpec(wattExp("1", 3)), args: []semreg.TypedField{{ID: "pv.limit.active_power_enabled", Value: booleanValue(false)}}, reason: ReasonNoRoute, clear: true},
		{name: "missing executor", spec: pvSpec(wattExp("1", 3)), args: []semreg.TypedField{{ID: "pv.limit.active_power_enabled", Value: booleanValue(false)}}, reason: ReasonExecutorMissing, skip: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.spec.skipRegister = tc.skip
			rig := newRig(t, tc.spec)
			if tc.clear {
				rig.use(nil)
			}
			result := rig.invoke(nil, rig.plant.request(tc.args, 20*time.Second))
			assertIdle(t, rig.exec)
			assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, tc.reason)
			if tc.skip && rig.capture.calls() < 1 {
				t.Fatal("missing executor returned before capture")
			}
		})
	}
}

func TestReleaseOutcomes(t *testing.T) {
	newRelease := func(t *testing.T) *rig {
		t.Helper()
		return newRig(t, pvSpec(wattExp("1", 3)))
	}
	args := []semreg.TypedField{{ID: "pv.limit.active_power_enabled", Value: booleanValue(false)}}
	t.Run("protocol_ack", func(t *testing.T) {
		rig := newRelease(t)
		result := rig.invoke(nil, rig.plant.request(args, 20*time.Second))
		if result.Status != StatusProtocolAck || result.KernelOutcome != operation.OutcomeAcknowledgedUnverified {
			t.Fatalf("%#v", result)
		}
	})
	t.Run("readback_confirmed", func(t *testing.T) {
		rig := newRelease(t)
		request := rig.plant.request(args, 20*time.Second)
		later := rig.plant.booleanSnapshot("pv.limit.active_power_enabled", false, rig.now.Add(3*time.Second))
		rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
			return readbackCompletion(dispatch, later, "candidate:release", "1", rig.now.Add(4*time.Second), operation.OutcomeApplied), nil
		}
		result := rig.invoke(nil, request)
		if result.Status != StatusReadbackConfirmed || result.ReadbackRelation != operation.ReadbackConfirms {
			t.Fatalf("%#v", result)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		rig := newRelease(t)
		rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
			completion := acknowledged(dispatch)
			completion.Acknowledgement = nil
			completion.Dispatch.Delivery = operation.DeliveryUnknown
			return completion, nil
		}
		result := rig.invoke(nil, rig.plant.request(args, 20*time.Second))
		if result.Status != StatusUnknown || result.KernelOutcome != operation.OutcomeIndeterminate {
			t.Fatalf("%#v", result)
		}
	})
}

func TestStorageCOB012CanonicalOrder(t *testing.T) {
	power := wattExp("-25", 1)
	active := booleanValue(true)
	duration := qtyExp("3", 1, "unit.second")
	zero := qty("0", "unit.second")
	spec := publishSpec{
		name: "storage", validator: storage.New(), operation: "storage.operation.set_dc_power",
		resource: "resource:interface:01", asset: "asset:storage", value: power,
	}
	canonical := []semreg.TypedField{
		{ID: "storage.setpoint.active", Value: active},
		{ID: "storage.setpoint.dc_power", Value: power},
		{ID: "storage.setpoint.duration", Value: duration},
	}
	rig := newRig(t, spec)
	result := rig.invoke(nil, rig.plant.request(canonical, 20*time.Second))
	calls, sends := rig.exec.counts()
	if calls != 1 || sends != 1 || result.Status != StatusProtocolAck {
		t.Fatalf("calls %d sends %d result %#v", calls, sends, result)
	}
	got := rig.exec.last.Request.Arguments
	if len(got) != 3 || got[1].Value.Quantity.Number.Coefficient != "-25" || got[1].Value.Quantity.Number.Exponent10 != 1 || got[0].Value.Boolean == nil || !*got[0].Value.Boolean || got[2].ID != "storage.setpoint.duration" {
		t.Fatalf("arguments %#v", got)
	}
	if rig.exec.last.ExpectedEffect.Rule.ID != "storage.effect.set_dc_power" || rig.exec.last.ExpectedEffect.Fact.FactID != "storage.setpoint.dc_power" {
		t.Fatalf("effect %#v", rig.exec.last.ExpectedEffect)
	}
	declared := newRig(t, spec)
	declaredResult := declared.invoke(nil, declared.plant.request([]semreg.TypedField{
		{ID: "storage.setpoint.dc_power", Value: power},
		{ID: "storage.setpoint.active", Value: active},
		{ID: "storage.setpoint.duration", Value: duration},
	}, 20*time.Second))
	assertIdle(t, declared.exec)
	assertRefusal(t, declaredResult, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonNoncanonicalArguments)
	without := newRig(t, spec)
	withoutResult := without.invoke(nil, without.plant.request([]semreg.TypedField{
		{ID: "storage.setpoint.active", Value: active},
		{ID: "storage.setpoint.dc_power", Value: power},
	}, 20*time.Second))
	if withoutResult.Status != StatusProtocolAck || len(without.exec.last.Request.Arguments) != 2 || without.exec.last.ExpectedEffect.Rule.ID != "storage.effect.set_dc_power" {
		t.Fatalf("without %#v args %#v", withoutResult, without.exec.last.Request.Arguments)
	}
	explicit := newRig(t, spec)
	explicitRequest := explicit.plant.request([]semreg.TypedField{
		{ID: "storage.setpoint.active", Value: active},
		{ID: "storage.setpoint.dc_power", Value: power},
		{ID: "storage.setpoint.duration", Value: zero},
	}, 20*time.Second)
	explicitResult := explicit.invoke(nil, explicitRequest)
	if explicitResult.Status != StatusProtocolAck || len(explicit.exec.last.Request.Arguments) != 3 || explicit.exec.last.Request.Arguments[2].Value.Quantity.Number.Coefficient != "0" {
		t.Fatalf("zero %#v args %#v", explicitResult, explicit.exec.last.Request.Arguments)
	}
	if explicit.exec.last.ExpectedEffect.Rule.ID != without.exec.last.ExpectedEffect.Rule.ID || explicit.owner.intent().Deadline == (semreg.TimePoint{}) || without.owner.intent().Deadline == (semreg.TimePoint{}) {
		t.Fatal("duration changed the effect or the finite deadline")
	}
	missing := newRig(t, spec)
	missingResult := missing.invoke(nil, missing.plant.request([]semreg.TypedField{{ID: "storage.setpoint.active", Value: active}}, 20*time.Second))
	assertIdle(t, missing.exec)
	assertRefusal(t, missingResult, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonMissingPrimary)
	duplicate := newRig(t, spec)
	duplicateResult := duplicate.invoke(nil, duplicate.plant.request([]semreg.TypedField{
		{ID: "storage.setpoint.dc_power", Value: power},
		{ID: "storage.setpoint.dc_power", Value: power},
	}, 20*time.Second))
	assertIdle(t, duplicate.exec)
	assertRefusal(t, duplicateResult, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonDuplicateArgument)
	undeclared := newRig(t, spec)
	undeclaredResult := undeclared.invoke(nil, undeclared.plant.request([]semreg.TypedField{
		{ID: "storage.field.other", Value: active},
		{ID: "storage.setpoint.dc_power", Value: power},
	}, 20*time.Second))
	assertIdle(t, undeclared.exec)
	assertRefusal(t, undeclaredResult, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonUndeclaredParameter)
}
