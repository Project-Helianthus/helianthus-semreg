package call

import (
	"context"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
)

// TestRouterBookkeepingStaysWithinTheKernelRetention drives more distinct
// requests than the kernel retains and requires the replay results and the
// recorded qualifications together to stay within that same bound. Removing the
// eviction in storeResult or storeNote lets either map grow with every request.
func TestRouterBookkeepingStaysWithinTheKernelRetention(t *testing.T) {
	rig := newRig(t, thermalSpec())
	rig.use(nil)
	argument := []semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}
	total := RetainedOperations + 200
	var first, last semreg.IntentID
	for i := 0; i < total; i++ {
		result := rig.invoke(nil, rig.plant.request(argument, 20*time.Second+time.Duration(i)))
		assertRefusal(t, result, StatusFailed, operation.OutcomeRejected, operation.DeliveryNotSent, ReasonNoRoute)
		intent := rig.owner.intent().IntentID
		if i == 0 {
			first = intent
		}
		last = intent
	}
	assertIdle(t, rig.exec)
	rig.router.mu.Lock()
	combined := len(rig.router.results) + len(rig.router.noted)
	orders := len(rig.router.resultOrder) + len(rig.router.notedOrder)
	inflight := len(rig.router.inflight)
	rig.router.mu.Unlock()
	if combined > RetainedOperations || orders != combined || inflight != 0 {
		t.Fatalf("results and noted hold %d entries (order %d, inflight %d), bound %d", combined, orders, inflight, RetainedOperations)
	}
	if _, ok := rig.router.RecordedQualification(last); !ok {
		t.Fatal("the newest qualification was evicted")
	}
	if _, ok := rig.router.RecordedQualification(first); ok {
		t.Fatal("the oldest qualification was kept past the bound")
	}
}

// TestStorageMatchConstraintsAtThePackLevel calls the storage pack's
// MatchConstraints directly. Constraints alone and constraints plus each subset
// of the operation's optional parameters match; each of the following is
// refused: a changed constraint value, a repeated constraint, an undeclared
// field, and a repeated optional parameter. Narrowing the subset match to the
// full parameter list, or dropping the declared-parameter check, fails a case.
func TestStorageMatchConstraintsAtThePackLevel(t *testing.T) {
	validator := storage.New()
	var checked int
	for _, op := range storage.Metadata().Operations {
		if len(op.Parameters) == 0 {
			continue
		}
		checked++
		power := qtyExp("-25", 1, "unit.watt")
		other := qtyExp("-26", 1, "unit.watt")
		capability := semreg.CapabilityInstance{
			InstanceID: "cap:1", AssetID: "asset:storage", ServiceInstance: "service:1", Definition: op.Capability, BindingID: "binding:1",
			SourceEpochID: "epoch:1", DriverGeneration: "1", Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable,
			Constraints:        []semreg.TypedField{{ID: op.Argument.ID, Value: power}},
			ActivationEvidence: []semreg.EvidenceRef{pubEvidence("match:" + string(op.Ref.ID))},
			Revision:           "1",
		}
		constraint := semreg.TypedField{ID: op.Argument.ID, Value: power}
		parameter := func(ref semreg.DefinitionRef) semreg.TypedField {
			if ref.ID == "storage.setpoint.active" {
				return semreg.TypedField{ID: ref.ID, Value: booleanValue(true)}
			}
			return semreg.TypedField{ID: ref.ID, Value: qtyExp("3", 1, "unit.second")}
		}
		accepted := map[string][]semreg.TypedField{"constraints alone": {constraint}}
		for i, ref := range op.Parameters {
			accepted["constraints plus "+string(ref.ID)] = []semreg.TypedField{constraint, parameter(ref)}
			for _, later := range op.Parameters[i+1:] {
				accepted["constraints plus "+string(ref.ID)+" and "+string(later.ID)] = []semreg.TypedField{constraint, parameter(ref), parameter(later)}
			}
		}
		for name, fields := range accepted {
			if err := validator.MatchConstraints(capability, fields); err != nil {
				t.Fatalf("%s %s: %v", op.Ref.ID, name, err)
			}
		}
		first := parameter(op.Parameters[0])
		var foreign semreg.DefinitionID
		for _, candidate := range storage.Metadata().Operations {
			if candidate.Argument.ID != op.Argument.ID {
				foreign = candidate.Argument.ID
			}
		}
		rejected := map[string][]semreg.TypedField{
			"changed constraint value":                          {{ID: op.Argument.ID, Value: other}},
			"changed constraint with parameter":                 {{ID: op.Argument.ID, Value: other}, first},
			"repeated constraint":                               {constraint, constraint, first},
			"valid field that is no parameter of the operation": {constraint, {ID: foreign, Value: power}},
			"repeated optional parameter":                       {constraint, first, first},
			"parameter without the constraint":                  {first, parameter(op.Parameters[len(op.Parameters)-1])},
		}
		for name, fields := range rejected {
			if err := validator.MatchConstraints(capability, fields); err == nil {
				t.Fatalf("%s %s: accepted", op.Ref.ID, name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("the storage pack declares no operation with optional parameters")
	}
}

type contextProbeKey struct{}

// TestFiniteExecutorContextIsTheCallerContext records the existing behaviour
// the finite path keeps: the executor receives the caller's context unchanged,
// so the timeout and the intent deadline reach it through the dispatch and the
// intent, and no deadline of the router's own is put on the context, which
// leaves the native transport's established timeout in charge.
func TestFiniteExecutorContextIsTheCallerContext(t *testing.T) {
	rig := newRig(t, thermalSpec())
	var seen context.Context
	rig.exec.complete = func(ctx context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
		seen = ctx
		return acknowledged(dispatch), nil
	}
	ctx := context.WithValue(context.Background(), contextProbeKey{}, "caller")
	result := rig.invoke(ctx, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, 20*time.Second))
	if result.Status != StatusProtocolAck {
		t.Fatal(result.Reason)
	}
	if seen == nil || seen.Value(contextProbeKey{}) != "caller" {
		t.Fatal("the executor did not receive the caller's context")
	}
	if _, bounded := seen.Deadline(); bounded {
		t.Fatal("the router put a deadline on the executor context")
	}
	if rig.exec.last.Request.Timeout != 20*time.Second {
		t.Fatalf("dispatch timeout %v", rig.exec.last.Request.Timeout)
	}
	if want := timePoint(rig.now.Add(20 * time.Second)); rig.owner.intent().Deadline != want {
		t.Fatalf("intent deadline %#v, want %#v", rig.owner.intent().Deadline, want)
	}
}
