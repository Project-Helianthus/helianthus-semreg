package call

import (
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

func TestSmokeThermalDispatch(t *testing.T) {
	rig := newRig(t, publishSpec{
		name: "thermal", validator: thermal.New(), operation: "thermal.operation.set_temperature",
		resource: "resource:zone:01", asset: "asset:thermal", value: qtyExp("29315", -2, "unit.kelvin"),
	})
	value := qtyExp("29315", -2, "unit.kelvin")
	result := rig.invoke(nil, rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: value}}, 20*time.Second))
	calls, sends := rig.exec.counts()
	if calls != 1 || sends != 1 {
		t.Fatalf("calls %d sends %d result %#v", calls, sends, result)
	}
	if result.Status != StatusProtocolAck || result.KernelOutcome != operation.OutcomeAcknowledgedUnverified || result.Delivery != operation.DeliverySent {
		t.Fatalf("result %#v", result)
	}
	effect := rig.exec.last.ExpectedEffect
	if effect.Rule.ID != "thermal.effect.set_temperature" || effect.Operator != "equal" || effect.Fact.Dimensions[0].ID != "thermal.dimension.zone" {
		t.Fatalf("effect %#v", effect)
	}
}
