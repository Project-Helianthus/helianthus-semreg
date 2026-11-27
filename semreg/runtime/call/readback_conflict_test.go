package call

import (
	"context"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

type invocationForm struct {
	name    string
	timeout time.Duration
	invoke  func(*rig, driver.OperationRequest) driver.OperationResult
}

func invocationForms() []invocationForm {
	return []invocationForm{
		{"Invoke", 20 * time.Second, func(r *rig, request driver.OperationRequest) driver.OperationResult {
			return r.invoke(nil, request)
		}},
		{"InvokeCallBound", 0, func(r *rig, request driver.OperationRequest) driver.OperationResult {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result, err := r.router.InvokeCallBound(ctx, request)
			if err != nil {
				r.plant.t.Fatal(err)
			}
			return result
		}},
	}
}

// permissiveUnit publishes a value the thermal pack refuses, so that a later
// snapshot can be built that is well formed but fails the pack validation the
// router and the kernel apply to a readback snapshot.
type permissiveUnit struct {
	semreg.PackValidator
}

func (p permissiveUnit) ValidateFact(key semreg.FactKey, value *semreg.Value) error {
	if value != nil && value.Quantity != nil && value.Quantity.Unit == "unit.ampere" {
		return nil
	}
	return p.PackValidator.ValidateFact(key, value)
}

func noCandidateCompletion(dispatch driver.Dispatch, snap semreg.Snapshot, wall time.Time, relation operation.ReadbackRelation) driver.Completion {
	completion := readbackCompletion(dispatch, snap, "", "", wall, operation.OutcomeConflict)
	completion.Readback.Relation = relation
	completion.Readback.Evidence = []semreg.EvidenceRef{pubEvidence("readback:contradicting-row")}
	return completion
}

func TestContradictingReadbackWithoutCandidateInBothForms(t *testing.T) {
	for _, form := range invocationForms() {
		t.Run(form.name, func(t *testing.T) {
			setup := func() (*rig, driver.OperationRequest, semreg.Snapshot) {
				rig := newRig(t, thermalSpec())
				request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, form.timeout)
				later := rig.plant.laterFact(rig.plant.argumentFact, zoneTemperature(), rig.now.Add(3*time.Second), "candidate:value-row", "2")
				return rig, request, later
			}
			t.Run("contradicts is conflict", func(t *testing.T) {
				rig, request, later := setup()
				rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
					return noCandidateCompletion(dispatch, later, rig.now.Add(4*time.Second), operation.ReadbackContradicts), nil
				}
				result := form.invoke(rig, request)
				assertRefusal(t, result, StatusFailed, operation.OutcomeConflict, operation.DeliverySent, "")
				if result.ReadbackRelation != operation.ReadbackContradicts {
					t.Fatalf("relation %q", result.ReadbackRelation)
				}
			})
			t.Run("confirms is not conflict or applied", func(t *testing.T) {
				rig, request, later := setup()
				rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
					return noCandidateCompletion(dispatch, later, rig.now.Add(4*time.Second), operation.ReadbackConfirms), nil
				}
				result := form.invoke(rig, request)
				if result.Status != StatusProtocolAck || result.KernelOutcome != operation.OutcomeAcknowledgedUnverified || result.ReadbackRelation != operation.ReadbackInconclusive {
					t.Fatalf("%#v", result)
				}
			})
			t.Run("candidate of another fact is not conflict or applied", func(t *testing.T) {
				rig, request, _ := setup()
				otherKey := rig.plant.argumentFact
				otherKey.FactID = "thermal.measurement.air_temperature"
				other := rig.plant.laterFact(otherKey, zoneTemperature(), rig.now.Add(3*time.Second), "candidate:other-fact", "2")
				rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
					completion := readbackCompletion(dispatch, other, "candidate:other-fact", "2", rig.now.Add(4*time.Second), operation.OutcomeApplied)
					completion.Readback.Relation = operation.ReadbackContradicts
					return completion, nil
				}
				result := form.invoke(rig, request)
				if result.KernelOutcome == operation.OutcomeConflict || result.KernelOutcome == operation.OutcomeApplied || result.Status != StatusProtocolAck {
					t.Fatalf("%#v", result)
				}
			})
			for _, flaw := range []struct {
				name   string
				mutate func(*driver.Completion)
			}{
				{"another source epoch", func(c *driver.Completion) { c.Readback.SourceEpochID = "epoch:other" }},
				{"another binding", func(c *driver.Completion) { c.Readback.BindingID = "binding:other" }},
				{"another generation", func(c *driver.Completion) { c.Readback.DriverGeneration = "2" }},
				{"no evidence", func(c *driver.Completion) { c.Readback.Evidence = []semreg.EvidenceRef{} }},
				{"no snapshot", func(c *driver.Completion) { c.ReadbackSnapshot = nil }},
			} {
				t.Run("flawed "+flaw.name+" gives the same outcome", func(t *testing.T) {
					rig, request, later := setup()
					rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
						completion := noCandidateCompletion(dispatch, later, rig.now.Add(4*time.Second), operation.ReadbackContradicts)
						flaw.mutate(&completion)
						return completion, nil
					}
					result := form.invoke(rig, request)
					if result.KernelOutcome == operation.OutcomeConflict || result.Status != StatusProtocolAck {
						t.Fatalf("%#v", result)
					}
				})
			}
			t.Run("unknown delivery is indeterminate", func(t *testing.T) {
				rig, request, later := setup()
				rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
					completion := noCandidateCompletion(dispatch, later, rig.now.Add(4*time.Second), operation.ReadbackContradicts)
					completion.Acknowledgement = nil
					completion.Dispatch.Delivery = operation.DeliveryUnknown
					return completion, nil
				}
				result := form.invoke(rig, request)
				if result.Status != StatusUnknown || result.KernelOutcome != operation.OutcomeIndeterminate || result.ReadbackRelation == operation.ReadbackContradicts {
					t.Fatalf("%#v", result)
				}
			})
			t.Run("sent without a possible side effect is not conflict", func(t *testing.T) {
				rig, request, later := setup()
				rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
					completion := noCandidateCompletion(dispatch, later, rig.now.Add(4*time.Second), operation.ReadbackContradicts)
					completion.Dispatch.PossibleSideEffect = false
					return completion, nil
				}
				result := form.invoke(rig, request)
				if result.KernelOutcome == operation.OutcomeConflict || result.ReadbackRelation == operation.ReadbackContradicts || result.Status != StatusProtocolAck {
					t.Fatalf("%#v", result)
				}
			})
			t.Run("a later snapshot failing pack validation is not conflict", func(t *testing.T) {
				spec := thermalSpec()
				spec.validator = permissiveUnit{spec.validator}
				rig := newRig(t, spec)
				request := rig.plant.request([]semreg.TypedField{{ID: rig.plant.operation.Argument.ID, Value: zoneTemperature()}}, form.timeout)
				broken := rig.plant.laterFact(rig.plant.argumentFact, qtyExp("1", 0, "unit.ampere"), rig.now.Add(3*time.Second), "candidate:wrong-unit", "2")
				rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
					return noCandidateCompletion(dispatch, broken, rig.now.Add(4*time.Second), operation.ReadbackContradicts), nil
				}
				result := form.invoke(rig, request)
				if result.KernelOutcome == operation.OutcomeConflict || result.ReadbackRelation == operation.ReadbackContradicts || result.Status != StatusProtocolAck {
					t.Fatalf("%#v", result)
				}
			})
			t.Run("not sent without side effect is failed_no_contact", func(t *testing.T) {
				rig, request, _ := setup()
				rig.exec.complete = func(_ context.Context, dispatch driver.Dispatch) (driver.Completion, error) {
					return notSent(dispatch), nil
				}
				result := form.invoke(rig, request)
				assertRefusal(t, result, StatusFailed, operation.OutcomeFailedNoContact, operation.DeliveryNotSent, ReasonNoContact)
			})
		})
	}
}
