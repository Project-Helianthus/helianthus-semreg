package driver_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestOperationRequestIsTheClosedFieldSet(t *testing.T) {
	typ := reflect.TypeOf(driver.OperationRequest{})
	var names []string
	for i := 0; i < typ.NumField(); i++ {
		names = append(names, typ.Field(i).Name)
	}
	want := []string{
		"AssetID", "BindingID", "CapabilityInstanceID", "Operation", "Arguments", "ExpectedSemanticRevision",
		"ExpectedCapabilityRevision", "ExpectedCapabilityInstanceRevision", "ExpectedDriverGeneration",
		"ExpectedSourceEpochID", "Preconditions", "Timeout",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("fields = %v", names)
	}
	wantValid(t, "request", operationRequest().Validate())
}

func TestOperationRequestRejectsEachInvalidField(t *testing.T) {
	for name, mutate := range map[string]func(*driver.OperationRequest){
		"asset id":                              func(r *driver.OperationRequest) { r.AssetID = "" },
		"binding id":                            func(r *driver.OperationRequest) { r.BindingID = "" },
		"capability instance id":                func(r *driver.OperationRequest) { r.CapabilityInstanceID = "" },
		"operation":                             func(r *driver.OperationRequest) { r.Operation = v1.DefinitionRef{} },
		"argument":                              func(r *driver.OperationRequest) { r.Arguments = []v1.TypedField{{}} },
		"expected semantic revision":            func(r *driver.OperationRequest) { r.ExpectedSemanticRevision = "" },
		"expected capability revision":          func(r *driver.OperationRequest) { r.ExpectedCapabilityRevision = "" },
		"expected capability instance revision": func(r *driver.OperationRequest) { r.ExpectedCapabilityInstanceRevision = "" },
		"expected driver generation":            func(r *driver.OperationRequest) { r.ExpectedDriverGeneration = "" },
		"expected source epoch id":              func(r *driver.OperationRequest) { r.ExpectedSourceEpochID = "" },
		"precondition":                          func(r *driver.OperationRequest) { r.Preconditions = []operation.Precondition{{}} },
	} {
		r := operationRequest()
		mutate(&r)
		wantInvalid(t, "request with invalid "+name, r.Validate())
	}
}

// Both entries accept the same request and return the same result shape.
func TestScopedOperationsBothEntries(t *testing.T) {
	ops := &syntheticOperations{}
	var scoped driver.ScopedOperations = ops
	finite := operationRequest()
	finite.Timeout = 5 * time.Second
	callBound := operationRequest()

	finiteResult, err := scoped.Invoke(context.Background(), finite)
	if err != nil {
		t.Fatal(err)
	}
	boundResult, err := scoped.InvokeCallBound(context.Background(), callBound)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops.calls) != 2 || ops.calls[0].callBound || !ops.calls[1].callBound {
		t.Fatalf("calls = %+v", ops.calls)
	}
	if ops.calls[0].request.Timeout != 5*time.Second || ops.calls[1].request.Timeout != 0 {
		t.Error("the call-bound request carries a finite deadline")
	}
	wantValid(t, "finite result", finiteResult.Validate())
	wantValid(t, "call-bound result", boundResult.Validate())
	if reflect.TypeOf(finiteResult) != reflect.TypeOf(boundResult) {
		t.Error("the two entries return different result shapes")
	}
	if finiteResult.CompoundExecution != nil || boundResult.CompoundExecution == nil || !boundResult.CompoundExecution.Partial {
		t.Errorf("compound execution: %+v / %+v", finiteResult.CompoundExecution, boundResult.CompoundExecution)
	}
}

func TestOperationResultAndCompoundExecution(t *testing.T) {
	full := driver.OperationResult{
		Status: "accepted", KernelOutcome: operation.OutcomeAcknowledgedUnverified, Delivery: operation.DeliverySent,
		Acknowledgement: operation.AckAccepted, ReadbackRelation: operation.ReadbackInconclusive, Reason: "awaiting readback",
		CompoundExecution: &driver.CompoundExecution{Partial: true, Steps: []driver.CompoundStep{
			{Point: "point.one", Attempted: true, Delivery: operation.DeliverySent, Acknowledgement: operation.AckAccepted, ReadbackRelation: operation.ReadbackConfirms, Reason: "done"},
			{Point: "point.two", Attempted: false, Reason: "an earlier step failed"},
		}},
	}
	wantValid(t, "result", full.Validate())
	wantValid(t, "result of the status alone", driver.OperationResult{Status: "rejected"}.Validate())

	for name, mutate := range map[string]func(*driver.OperationResult){
		"status":            func(r *driver.OperationResult) { r.Status = "" },
		"kernel outcome":    func(r *driver.OperationResult) { r.KernelOutcome = "done" },
		"delivery":          func(r *driver.OperationResult) { r.Delivery = "maybe" },
		"acknowledgement":   func(r *driver.OperationResult) { r.Acknowledgement = "ok" },
		"readback relation": func(r *driver.OperationResult) { r.ReadbackRelation = "matches" },
		"step point": func(r *driver.OperationResult) {
			r.CompoundExecution = &driver.CompoundExecution{Steps: []driver.CompoundStep{{}}}
		},
	} {
		r := full
		mutate(&r)
		wantInvalid(t, "result with invalid "+name, r.Validate())
	}
	for name, mutate := range map[string]func(*driver.CompoundStep){
		"delivery":          func(s *driver.CompoundStep) { s.Delivery = operation.DeliveryNotSent },
		"acknowledgement":   func(s *driver.CompoundStep) { s.Acknowledgement = operation.AckRejected },
		"readback relation": func(s *driver.CompoundStep) { s.ReadbackRelation = operation.ReadbackContradicts },
	} {
		s := driver.CompoundStep{Point: "point.two", Attempted: false}
		mutate(&s)
		wantInvalid(t, "step that was not attempted carrying "+name, s.Validate())
		s.Attempted = true
		wantValid(t, "attempted step carrying "+name, s.Validate())
	}
	wantInvalid(t, "step without a point name", driver.CompoundStep{Attempted: true}.Validate())
}

func dispatch() driver.Dispatch {
	return driver.Dispatch{
		Request: operationRequest(), Route: route(),
		Guard: operation.GuardClaims{
			AssetID: "asset:one", ExpectedCapabilityRevision: "1", CapabilityInstance: "capability:one", ExpectedCapabilityInstanceRevision: "1",
			ServiceInstance: "service:one", BindingID: "binding:one", SourceID: "source:one", SourceEpochID: "epoch:one", DriverGeneration: "1",
		},
		ExpectedEffect: expectedEffect(), AdmittedAt: timePoint(), AdmittedRevision: revisionVector(),
	}
}

func TestDispatchAndCompletion(t *testing.T) {
	wantValid(t, "dispatch", dispatch().Validate())
	for name, mutate := range map[string]func(*driver.Dispatch){
		"request":           func(d *driver.Dispatch) { d.Request.AssetID = "" },
		"route":             func(d *driver.Dispatch) { d.Route.BindingID = "" },
		"guard":             func(d *driver.Dispatch) { d.Guard.SourceID = "" },
		"expected effect":   func(d *driver.Dispatch) { d.ExpectedEffect = operation.ExpectedEffect{} },
		"admission time":    func(d *driver.Dispatch) { d.AdmittedAt = v1.TimePoint{} },
		"admitted revision": func(d *driver.Dispatch) { d.AdmittedRevision = v1.RevisionVector{} },
	} {
		d := dispatch()
		mutate(&d)
		wantInvalid(t, "dispatch with invalid "+name, d.Validate())
	}

	wantValid(t, "completion with no terminal outcome", driver.Completion{}.Validate())
	errID := v1.ErrorID("not_a_defined_error")
	for name, c := range map[string]driver.Completion{
		"dispatch evidence": {Dispatch: &operation.DispatchEvidence{}},
		"acknowledgement":   {Acknowledgement: &operation.Acknowledgement{}},
		"readback":          {Readback: &operation.Readback{}},
		"readback snapshot": {ReadbackSnapshot: &v1.Snapshot{}},
		"error id":          {ErrorID: &errID},
		"outcome evidence":  {OutcomeEvidence: evidenceSet("x")},
	} {
		wantInvalid(t, "no outcome with "+name, c.Validate())
	}
	wantInvalid(t, "unknown outcome", driver.Completion{Outcome: "done"}.Validate())
	wantValid(t, "terminal outcome with evidence", driver.Completion{Outcome: operation.OutcomeIndeterminate, OutcomeEvidence: evidenceSet("x")}.Validate())
	wantInvalid(t, "terminal outcome with an invalid dispatch record", driver.Completion{Outcome: operation.OutcomeIndeterminate, Dispatch: &operation.DispatchEvidence{}}.Validate())
	wantInvalid(t, "terminal outcome with an invalid acknowledgement", driver.Completion{Outcome: operation.OutcomeApplied, Acknowledgement: &operation.Acknowledgement{}}.Validate())
	wantInvalid(t, "terminal outcome with an invalid readback", driver.Completion{Outcome: operation.OutcomeApplied, Readback: &operation.Readback{}}.Validate())
	wantInvalid(t, "terminal outcome with an invalid snapshot", driver.Completion{Outcome: operation.OutcomeApplied, ReadbackSnapshot: &v1.Snapshot{}}.Validate())
	wantInvalid(t, "terminal outcome with an invalid error id", driver.Completion{Outcome: operation.OutcomeRejected, ErrorID: &errID}.Validate())
	wantInvalid(t, "terminal outcome with invalid evidence", driver.Completion{Outcome: operation.OutcomeApplied, OutcomeEvidence: []v1.EvidenceRef{{}}}.Validate())
	wantInvalid(t, "completion with an invalid compound execution", driver.Completion{CompoundExecution: &driver.CompoundExecution{Steps: []driver.CompoundStep{{}}}}.Validate())
	wantValid(t, "completion with a compound execution", driver.Completion{CompoundExecution: &driver.CompoundExecution{Partial: true, Steps: []driver.CompoundStep{{Point: "point.one", Attempted: true}}}}.Validate())
}

// A driver registers its own setting route and callback on the session, in the
// exact RegisterExecutor and Execute signatures.
func TestRegisterExecutorSignatures(t *testing.T) {
	session := newSession()
	var s driver.Session = session
	completion := driver.Completion{Outcome: operation.OutcomeIndeterminate, OutcomeEvidence: evidenceSet("evidence")}
	var executor driver.Executor = syntheticExecutor{completion: completion}
	route := settingRoute("a")

	unregister, err := s.RegisterExecutor(route, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.registered) != 1 || !reflect.DeepEqual(session.registered[0].route, route) {
		t.Fatalf("registered = %+v", session.registered)
	}
	got, err := session.registered[0].executor.Execute(context.Background(), dispatch())
	if err != nil {
		t.Fatal(err)
	}
	wantValid(t, "completion", got.Validate())
	unregister()
	if session.removed != 1 {
		t.Errorf("unregister removed %d registrations", session.removed)
	}
	registerSignature := (func(driver.SettingRoute, driver.Executor) (func(), error))(s.RegisterExecutor)
	executeSignature := (func(context.Context, driver.Dispatch) (driver.Completion, error))(executor.Execute)
	_, _ = registerSignature, executeSignature

	bad := route
	bad.Owner = driver.SourceRef{}
	if _, err := s.RegisterExecutor(bad, executor); err == nil {
		t.Error("a registration for an invalid route was accepted")
	}
}
