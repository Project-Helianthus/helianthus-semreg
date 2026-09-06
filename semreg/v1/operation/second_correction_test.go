package operation_test

import (
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestSecondCorrectionRejectsLossyTypedInputsBeforeCopy(t *testing.T) {
	for _, method := range []string{"validate", "admit", "reject", "record"} {
		t.Run(method, func(t *testing.T) {
			fixture := newOperationFixture(t)
			bad := fixture.intent
			bad.Arguments[0].Value.Symbols = []semreg.Symbol{}
			errorID(t, bad.Validate(), semreg.InvalidValue)
			var err error
			switch method {
			case "validate":
				err = fixture.kernel.ValidateIntent(bad)
			case "admit":
				var admission *operation.Admission
				admission, err = fixture.kernel.Admit(fixture.snapshot, fixture.current, bad, operation.AuthorityResolverFunc(authorize))
				if admission != nil {
					t.Fatal("invalid typed intent installed")
				}
			case "reject":
				_, err = fixture.kernel.RecordRejection(bad, semreg.InvalidValue, []semreg.EvidenceRef{})
			case "record":
				fixture.intent = validIntent()
				admission := mustAdmit(t, fixture)
				record := admittedRecord(admission)
				record.Outcome, record.Dispatch = operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true)
				record.Intent.Arguments[0].Value.Symbols = []semreg.Symbol{}
				_, err = fixture.kernel.Record(admission, record, nil)
				if _, recorded := admission.Recorded(); recorded {
					t.Fatal("invalid typed execution installed")
				}
			}
			errorID(t, err, semreg.InvalidValue)
		})
	}

	t.Run("invalid UTF-8", func(t *testing.T) {
		for _, method := range []string{"validate", "admit", "reject", "record"} {
			t.Run(method, func(t *testing.T) {
				fixture := newOperationFixture(t)
				malformed := "x" + string([]byte{0xff})
				bad := fixture.intent
				bad.Arguments = append([]semreg.TypedField(nil), bad.Arguments...)
				bad.Arguments[0].Value = semreg.Value{Kind: semreg.ValueText, Text: &malformed}
				var err error
				switch method {
				case "validate":
					err = fixture.kernel.ValidateIntent(bad)
				case "admit":
					_, err = fixture.kernel.Admit(fixture.snapshot, fixture.current, bad, operation.AuthorityResolverFunc(authorize))
				case "reject":
					_, err = fixture.kernel.RecordRejection(bad, semreg.InvalidValue, []semreg.EvidenceRef{})
				case "record":
					admission := mustAdmit(t, fixture)
					record := admittedRecord(admission)
					record.Outcome, record.Dispatch = operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true)
					record.Intent.Arguments[0].Value = bad.Arguments[0].Value
					_, err = fixture.kernel.Record(admission, record, nil)
				}
				errorID(t, err, semreg.InvalidValue)
			})
		}
	})

	t.Run("rejection evidence invalid UTF-8", func(t *testing.T) {
		fixture := newOperationFixture(t)
		badEvidence := evidence(1)
		badEvidence.Owner = semreg.DefinitionID("x" + string([]byte{0xff}))
		_, err := fixture.kernel.RecordRejection(fixture.intent, semreg.InvalidValue, []semreg.EvidenceRef{badEvidence})
		errorID(t, err, semreg.InvalidEvidence)
	})
}

type secondCorrectionCountingPack struct {
	*testOperationPack
	fieldCalls      int
	constraintCalls int
}

func (p *secondCorrectionCountingPack) ValidateField(ref semreg.DefinitionRef, field semreg.TypedField) error {
	p.fieldCalls++
	return p.testOperationPack.ValidateField(ref, field)
}

func (p *secondCorrectionCountingPack) MatchConstraints(capability semreg.CapabilityInstance, fields []semreg.TypedField) error {
	p.constraintCalls++
	return p.testOperationPack.MatchConstraints(capability, fields)
}

func TestSecondCorrectionCollectsIndependentAdmissionErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*operationFixture)
		auth   operation.AuthorityResolver
		want   semreg.ErrorID
	}{
		{"causal", func(f *operationFixture) { f.intent.Causal.MaxHops = 0 }, operation.AuthorityResolverFunc(authorize), semreg.CausalBudgetExceeded},
		{"causal authority", func(f *operationFixture) { f.intent.Causal.MaxHops = 0 }, nil, semreg.AuthorityMissing},
		{"causal owner", func(f *operationFixture) { f.intent.Causal.MaxHops = 0; f.intent.Kind.ID = "operation.test.absent" }, operation.AuthorityResolverFunc(authorize), semreg.DefinitionOwnerMissing},
		{"causal effect", func(f *operationFixture) {
			f.intent.Causal.MaxHops = 0
			f.intent.ExpectedEffect.Expected = boolValue(false)
		}, operation.AuthorityResolverFunc(authorize), semreg.InvalidValue},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOperationFixture(t)
			test.mutate(&fixture)
			admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, test.auth)
			if admission != nil {
				t.Fatal("rejected intent installed")
			}
			errorID(t, err, test.want)
		})
	}

	t.Run("malformed input invokes no dependent hooks", func(t *testing.T) {
		fixture := newOperationFixture(t)
		pack := &secondCorrectionCountingPack{testOperationPack: &testOperationPack{}}
		kernel, err := operation.NewKernel(pack)
		if err != nil {
			t.Fatal(err)
		}
		fixture.intent.Arguments[0].Value.Symbols = []semreg.Symbol{}
		authorityCalls := 0
		resolver := operation.AuthorityResolverFunc(func(operation.Intent, operation.Route, semreg.EvaluationContext) error {
			authorityCalls++
			return nil
		})
		_, err = kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, resolver)
		errorID(t, err, semreg.InvalidValue)
		if pack.fieldCalls != 0 || pack.intentCalls != 0 || pack.constraintCalls != 0 || authorityCalls != 0 {
			t.Fatalf("dependent hooks called: field=%d intent=%d constraint=%d authority=%d", pack.fieldCalls, pack.intentCalls, pack.constraintCalls, authorityCalls)
		}
	})
}

func TestSecondCorrectionRecordRanksReadbackRouteErrors(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	later := applyReadback(t, fixture, true, "220", "220")
	record := admittedRecord(admission)
	record.Outcome, record.Dispatch, record.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), readback(later, operation.ReadbackConfirms)
	record.Dispatch.PossibleSideEffect = false
	record.Readback.DriverGeneration = "2"
	_, err := fixture.kernel.Record(admission, record, &later)
	errorID(t, err, semreg.StaleDriverGeneration)
	if _, recorded := admission.Recorded(); recorded {
		t.Fatal("invalid record installed")
	}
}

type secondCorrectionInconclusivePack struct{ *testOperationPack }

func (p *secondCorrectionInconclusivePack) EvaluateReadback(operation.Intent, semreg.FactCandidate) (operation.ReadbackRelation, error) {
	return operation.ReadbackInconclusive, nil
}

func TestSecondCorrectionReadbackTimingIsOutcomeSpecific(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run("acknowledged", func(t *testing.T) {
			fixture := newOperationFixture(t)
			var err error
			fixture.kernel, err = operation.NewKernel(&secondCorrectionInconclusivePack{testOperationPack: &testOperationPack{}})
			if err != nil {
				t.Fatal(err)
			}
			admission := mustAdmit(t, fixture)
			later := applyReadback(t, fixture, true, "190", "190")
			record := admittedRecord(admission)
			record.Outcome, record.Dispatch = operation.OutcomeAcknowledgedUnverified, dispatch(operation.DeliverySent, true)
			if !completed {
				record.Dispatch.Completed = nil
			}
			record.Acknowledgement = &operation.Acknowledgement{State: operation.AckAccepted, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}
			record.Readback = readback(later, operation.ReadbackInconclusive)
			if _, err = fixture.kernel.Record(admission, record, &later); err != nil {
				t.Fatal(err)
			}
		})
	}

	t.Run("applied still requires post-completion receipt", func(t *testing.T) {
		fixture := newOperationFixture(t)
		admission := mustAdmit(t, fixture)
		later := applyReadback(t, fixture, true, "190", "190")
		record := admittedRecord(admission)
		record.Outcome, record.Dispatch, record.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), readback(later, operation.ReadbackConfirms)
		_, err := fixture.kernel.Record(admission, record, &later)
		errorID(t, err, semreg.InvalidOutcome)
	})

	t.Run("conflict requires post-start receipt", func(t *testing.T) {
		fixture := newOperationFixture(t)
		admission := mustAdmit(t, fixture)
		later := applyReadback(t, fixture, false, "170", "170")
		record := admittedRecord(admission)
		record.Outcome, record.Dispatch, record.Readback = operation.OutcomeConflict, dispatch(operation.DeliverySent, true), readback(later, operation.ReadbackContradicts)
		_, err := fixture.kernel.Record(admission, record, &later)
		errorID(t, err, semreg.InvalidOutcome)
	})
}

type secondCorrectionPointerResolver struct{}

func (*secondCorrectionPointerResolver) Authorize(operation.Intent, operation.Route, semreg.EvaluationContext) error {
	return nil
}

func TestSecondCorrectionNilCapableResolversFailClosed(t *testing.T) {
	fixture := newOperationFixture(t)
	var function operation.AuthorityResolverFunc
	_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, function)
	errorID(t, err, semreg.AuthorityMissing)

	var pointer *secondCorrectionPointerResolver
	_, err = fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, pointer)
	errorID(t, err, semreg.AuthorityMissing)

	if _, err = fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize)); err != nil {
		t.Fatal(err)
	}
}
