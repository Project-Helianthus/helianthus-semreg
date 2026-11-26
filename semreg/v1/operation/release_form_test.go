package operation_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

var (
	releaseParameter = semreg.DefinitionRef{Pack: testPackRef, ID: "field.test.parameter", Version: "1.0.0"}
	ordinaryOp       = semreg.DefinitionRef{Pack: testPackRef, ID: testOp.ID, Version: "2.0.0"}
	ordinaryOpID     = semreg.DefinitionRef{Pack: testPackRef, ID: "operation.test.other", Version: "1.0.0"}
	otherEffect      = semreg.DefinitionRef{Pack: testPackRef, ID: testEffect.ID, Version: "2.0.0"}
	otherRuleID      = semreg.DefinitionRef{Pack: testPackRef, ID: "effect.test.other", Version: "1.0.0"}
)

type releaseTestPack struct {
	testOperationPack
	fieldCalls, constraintCalls, constraintCount int
	allowBoolField, denyIntent                   bool
	literal                                      bool
	metadataPack                                 semreg.PackRef
}

func (p *releaseTestPack) Definitions() semreg.DefinitionIndex {
	index := p.testOperationPack.Definitions()
	index.Fields = append(index.Fields, releaseParameter)
	index.Operations = []semreg.DefinitionRef{ordinaryOpID, testOp, ordinaryOp}
	index.EffectRules = append(index.EffectRules, otherEffect, otherRuleID)
	return index
}

func (p *releaseTestPack) Metadata() semreg.PackMetadata {
	ref := testOp
	ref.Pack = p.metadataPack
	return semreg.PackMetadata{Pack: testPackRef, Operations: []semreg.OperationMetadata{
		{Ref: ordinaryOpID, Argument: testField, Effect: testEffect},
		{Ref: ref, Argument: testField, Effect: testEffect, Parameters: []semreg.DefinitionRef{releaseParameter},
			Release: &semreg.ReleaseMetadata{Argument: boolValue(p.literal)}},
		{Ref: ordinaryOp, Argument: testField, Effect: testEffect},
	}}
}

func (p *releaseTestPack) ValidateField(_ semreg.DefinitionRef, field semreg.TypedField) error {
	p.fieldCalls++
	if field.Value.Kind == semreg.ValueQuantity || p.allowBoolField {
		return nil
	}
	return errors.New("the set field requires a quantity")
}

func (p *releaseTestPack) MatchConstraints(_ semreg.CapabilityInstance, fields []semreg.TypedField) error {
	p.constraintCalls++
	p.constraintCount = len(fields)
	return nil
}

func (p *releaseTestPack) ValidateIntent(operation.Intent) error {
	p.intentCalls++
	if p.denyIntent {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "pack denied the intent"}
	}
	return nil
}

func newReleaseFixture(t *testing.T) (operationFixture, *releaseTestPack) {
	t.Helper()
	fixture := newOperationFixture(t)
	pack := &releaseTestPack{metadataPack: testPackRef}
	kernel, err := operation.NewKernel(pack)
	if err != nil {
		t.Fatal(err)
	}
	fixture.kernel = kernel
	fixture.intent.ExpectedEffect = operation.ExpectedEffect{Rule: testEffect}
	fixture.intent.Arguments[0].Value = boolValue(false)
	return fixture, pack
}

func releaseQuantity() semreg.Value {
	return semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Unit: "unit.watt", Number: semreg.Decimal{Coefficient: "1"}}}
}

func TestReleaseAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*operation.Intent, *releaseTestPack)
		want   bool
	}{
		{name: "declared literal", want: true},
		{name: "uses metadata rather than a hard-coded value", want: true, mutate: func(i *operation.Intent, p *releaseTestPack) {
			i.Arguments[0].Value = boolValue(true)
			p.literal = true
		}},
		{name: "true", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.Arguments[0].Value = boolValue(true) }},
		{name: "numeric", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.Arguments[0].Value = releaseQuantity() }},
		{name: "second argument", mutate: func(i *operation.Intent, _ *releaseTestPack) {
			i.Arguments = append(i.Arguments, semreg.TypedField{ID: "field.test.second", Value: releaseQuantity()})
		}},
		{name: "declared parameter", mutate: func(i *operation.Intent, _ *releaseTestPack) {
			i.Arguments = append(i.Arguments, semreg.TypedField{ID: releaseParameter.ID, Value: releaseQuantity()})
		}},
		{name: "predicate", mutate: func(i *operation.Intent, p *releaseTestPack) {
			i.ExpectedEffect = expectedEffect(false)
			p.allowBoolField = true
		}},
		{name: "operation without release", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.Kind = ordinaryOp }},
		{name: "operation with another ID", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.Kind = ordinaryOpID }},
		{name: "declaration has wrong operation pack ID", mutate: func(_ *operation.Intent, p *releaseTestPack) { p.metadataPack.ID = "pack.other" }},
		{name: "declaration has wrong operation pack version", mutate: func(_ *operation.Intent, p *releaseTestPack) { p.metadataPack.Version = "2.0.0" }},
		{name: "wrong argument ID", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.Arguments[0].ID = releaseParameter.ID }},
		{name: "no argument", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.Arguments = i.Arguments[:0] }},
		{name: "wrong rule version", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.ExpectedEffect.Rule = otherEffect }},
		{name: "wrong rule ID", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.ExpectedEffect.Rule.ID = "effect.test.other" }},
		{name: "wrong rule pack ID", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.ExpectedEffect.Rule.Pack.ID = "pack.other" }},
		{name: "wrong rule pack version", mutate: func(i *operation.Intent, _ *releaseTestPack) { i.ExpectedEffect.Rule.Pack.Version = "2.0.0" }},
		{name: "pack hook still owns intent", mutate: func(_ *operation.Intent, p *releaseTestPack) { p.denyIntent = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, pack := newReleaseFixture(t)
			if tc.mutate != nil {
				tc.mutate(&fixture.intent, pack)
			}
			admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			if !tc.want {
				errorID(t, err, semreg.InvalidValue)
				if admission != nil {
					t.Fatal("invalid intent admitted")
				}
				return
			}
			if err != nil || admission == nil {
				t.Fatalf("release admission: %v", err)
			}
			if pack.intentCalls != 1 || pack.fieldCalls != 0 || pack.constraintCalls != 1 || pack.constraintCount != 0 {
				t.Fatalf("hook calls: intent=%d field=%d constraints=%d constraint fields=%d", pack.intentCalls, pack.fieldCalls, pack.constraintCalls, pack.constraintCount)
			}
			if got := admission.Intent(); !got.ExpectedEffect.Unconfirmed() || len(got.Arguments) != 1 || got.Arguments[0].ID != testField.ID || got.Arguments[0].Value.Boolean == nil || *got.Arguments[0].Value.Boolean != pack.literal {
				t.Fatalf("admitted release changed: %+v", got)
			}
		})
	}
}

func TestReleaseTerminalRecords(t *testing.T) {
	t.Run("rejected", func(t *testing.T) {
		fixture, _ := newReleaseFixture(t)
		record, err := fixture.kernel.RecordRejection(fixture.intent, semreg.AuthorityMissing, nil)
		if err != nil || record.Outcome != operation.OutcomeRejected {
			t.Fatalf("record rejection = %q, %v", record.Outcome, err)
		}
	})
	for _, tc := range []struct {
		name     string
		outcome  operation.Outcome
		delivery operation.DeliveryState
		ack      operation.AckState
		relation operation.ReadbackRelation
		valid    bool
	}{
		{"failed_no_contact", operation.OutcomeFailedNoContact, operation.DeliveryNotSent, "", "", true},
		{"no_effect with rejected ack", operation.OutcomeNoEffect, operation.DeliverySent, operation.AckRejected, "", true},
		{"acknowledged_unverified (accepted)", operation.OutcomeAcknowledgedUnverified, operation.DeliverySent, operation.AckAccepted, "", true},
		{"acknowledged_unverified (provisional)", operation.OutcomeAcknowledgedUnverified, operation.DeliverySent, operation.AckProvisional, "", true},
		{"indeterminate", operation.OutcomeIndeterminate, operation.DeliveryUnknown, "", "", true},
		{"no_effect without ack", operation.OutcomeNoEffect, operation.DeliverySent, "", "", false},
		{"no_effect with accepted ack", operation.OutcomeNoEffect, operation.DeliverySent, operation.AckAccepted, "", false},
		{"no_effect with provisional ack", operation.OutcomeNoEffect, operation.DeliverySent, operation.AckProvisional, "", false},
		{"applied", operation.OutcomeApplied, operation.DeliverySent, operation.AckAccepted, operation.ReadbackConfirms, false},
		{"conflict", operation.OutcomeConflict, operation.DeliverySent, operation.AckAccepted, operation.ReadbackContradicts, false},
		{"inconclusive readback", operation.OutcomeAcknowledgedUnverified, operation.DeliverySent, operation.AckAccepted, operation.ReadbackInconclusive, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, pack := newReleaseFixture(t)
			admission := mustAdmit(t, fixture)
			record := admittedRecord(admission)
			record.Outcome, record.Dispatch = tc.outcome, dispatch(tc.delivery, tc.delivery != operation.DeliveryNotSent)
			if tc.ack != "" {
				record.Acknowledgement = &operation.Acknowledgement{State: tc.ack, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}
			}
			if tc.relation != "" {
				record.Readback = readback(fixture.snapshot, tc.relation)
				_, err := fixture.kernel.Record(admission, record, &fixture.snapshot)
				errorID(t, err, semreg.InvalidOutcome)
			}
			got, err := fixture.kernel.Record(admission, record, nil)
			if tc.valid {
				if err != nil || got.Outcome != tc.outcome || got.Readback != nil {
					t.Fatalf("record = %q, %v", got.Outcome, err)
				}
				raw, err := semreg.CanonicalJSON(got)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := operation.DecodeExecutionRecord(raw); err != nil {
					t.Fatal(err)
				}
			} else {
				errorID(t, record.Validate(), semreg.InvalidOutcome)
				errorID(t, err, semreg.InvalidOutcome)
			}
			if pack.readbackCalls != 0 {
				t.Fatal("rule-only intent reached the readback hook")
			}
		})
	}
}

func TestSetIntentCanonicalBytesKeepPredicate(t *testing.T) {
	intent := validIntent()
	intent.Arguments[0].Value = releaseQuantity()
	intent.ExpectedEffect.Expected = releaseQuantity()
	// Freeze the pre-release wire shape independently of the custom marshaler.
	type setEffect struct {
		Rule     semreg.DefinitionRef `json:"rule"`
		Fact     semreg.FactKey       `json:"fact"`
		Operator semreg.PredicateOp   `json:"operator"`
		Expected semreg.Value         `json:"expected"`
	}
	legacy := struct {
		operation.Intent
		Effect setEffect `json:"expected_effect"`
	}{intent, setEffect{intent.ExpectedEffect.Rule, intent.ExpectedEffect.Fact, intent.ExpectedEffect.Operator, intent.ExpectedEffect.Expected}}
	got, err := semreg.CanonicalJSON(intent)
	if err != nil {
		t.Fatal(err)
	}
	want, err := semreg.CanonicalJSON(legacy)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("numeric set intent canonical bytes changed: %v", err)
	}
}

func TestReleaseExpectedEffectWire(t *testing.T) {
	fixture, _ := newReleaseFixture(t)
	intent := fixture.intent
	raw := mustJSON(t, intent)
	rule := mustJSON(t, intent.ExpectedEffect.Rule)
	want := append(append([]byte(`{"rule":`), rule...), '}')
	var fields map[string]json.RawMessage
	decodeVectorInput(t, raw, &fields)
	if !bytes.Equal(fields["expected_effect"], want) {
		t.Fatalf("rule-only bytes: got %s want %s", fields["expected_effect"], want)
	}
	decoded, err := operation.DecodeIntent(raw)
	if err != nil || !bytes.Equal(mustJSON(t, decoded), raw) {
		t.Fatalf("release did not round trip: %v", err)
	}
	predicate := expectedEffect(false)
	members := []struct {
		name  string
		value []byte
	}{
		{"fact", mustJSON(t, predicate.Fact)}, {"operator", mustJSON(t, predicate.Operator)}, {"expected", mustJSON(t, predicate.Expected)},
	}
	for mask := 1; mask < 7; mask++ {
		effect := map[string]json.RawMessage{"rule": rule}
		for index, member := range members {
			if mask&(1<<index) != 0 {
				effect[member.name] = member.value
			}
		}
		fields["expected_effect"] = mustJSON(t, effect)
		_, err := operation.DecodeIntent(mustJSON(t, fields))
		errorID(t, err, semreg.InvalidValue)
	}
	for _, member := range members {
		t.Run("null "+member.name, func(t *testing.T) {
			fields["expected_effect"] = mustJSON(t, map[string]json.RawMessage{"rule": rule, member.name: []byte("null")})
			_, err := operation.DecodeIntent(mustJSON(t, fields))
			errorID(t, err, semreg.MissingMember)
		})
	}
}
