package operation_test

import (
	"bytes"
	"encoding/json"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestFourthCorrectionPreconditionWireIdentityMatrix(t *testing.T) {
	type wireCase struct {
		name   string
		want   semreg.ErrorID
		mutate func(map[string]any, map[string]any)
	}
	cases := []wireCase{
		{"valid duplicate", semreg.DuplicateKey, func(map[string]any, map[string]any) {}},
		{"duplicate with missing document contract", semreg.DuplicateKey, func(document, _ map[string]any) { delete(document, "contract") }},
		{"duplicate with null document contract", semreg.DuplicateKey, func(document, _ map[string]any) { document["contract"] = nil }},
		{"duplicate with numeric document contract", semreg.DuplicateKey, func(document, _ map[string]any) { document["contract"] = 17 }},
		{"duplicate with missing non-key value", semreg.DuplicateKey, func(_, intent map[string]any) {
			delete(intent["preconditions"].([]any)[0].(map[string]any), "expected")
		}},
		{"duplicate with malformed non-key value", semreg.DuplicateKey, func(_, intent map[string]any) {
			intent["preconditions"].([]any)[0].(map[string]any)["expected"] = 17
		}},
		{"missing candidate has no key", semreg.MissingMember, func(_, intent map[string]any) {
			for _, item := range intent["preconditions"].([]any) {
				delete(item.(map[string]any), "candidate_id")
			}
		}},
		{"malformed candidate has no key", semreg.InvalidIdentifier, func(_, intent map[string]any) {
			for _, item := range intent["preconditions"].([]any) {
				item.(map[string]any)["candidate_id"] = ""
			}
		}},
		{"malformed revision has no key", semreg.InvalidIdentifier, func(_, intent map[string]any) {
			for _, item := range intent["preconditions"].([]any) {
				item.(map[string]any)["candidate_revision"] = "0"
			}
		}},
		{"missing fact has no key", semreg.MissingMember, func(_, intent map[string]any) {
			for _, item := range intent["preconditions"].([]any) {
				delete(item.(map[string]any), "fact")
			}
		}},
		{"contract without duplicate", semreg.InvalidContract, func(document, intent map[string]any) {
			document["contract"] = 17
			intent["preconditions"] = intent["preconditions"].([]any)[:1]
		}},
		{"missing value without duplicate", semreg.MissingMember, func(_, intent map[string]any) {
			intent["preconditions"] = intent["preconditions"].([]any)[:1]
			delete(intent["preconditions"].([]any)[0].(map[string]any), "expected")
		}},
		{"unknown member without duplicate", semreg.UnknownMember, func(_, intent map[string]any) {
			intent["preconditions"] = intent["preconditions"].([]any)[:1]
			intent["preconditions"].([]any)[0].(map[string]any)["future"] = true
		}},
		{"invalid value without duplicate", semreg.InvalidValue, func(_, intent map[string]any) {
			intent["preconditions"] = intent["preconditions"].([]any)[:1]
			intent["preconditions"].([]any)[0].(map[string]any)["expected"] = 17
		}},
	}

	for _, entry := range []string{"Intent", "ExecutionRecord"} {
		for _, tc := range cases {
			t.Run(entry+"/"+tc.name, func(t *testing.T) {
				intent := validIntent()
				intent.Preconditions = append(intent.Preconditions, intent.Preconditions[0])
				var value any = intent
				if entry == "ExecutionRecord" {
					id := semreg.InvalidValue
					value = operation.ExecutionRecord{Contract: operation.ContractOperationV1, Intent: intent, Outcome: operation.OutcomeRejected, ErrorID: &id, OutcomeEvidence: []semreg.EvidenceRef{}}
				}
				raw := mustJSON(t, value)
				var document map[string]any
				if err := json.Unmarshal(raw, &document); err != nil {
					t.Fatal(err)
				}
				intentObject := document
				if entry == "ExecutionRecord" {
					intentObject = document["intent"].(map[string]any)
				}
				tc.mutate(document, intentObject)
				raw = mustJSON(t, document)
				var err error
				if entry == "Intent" {
					_, err = operation.DecodeIntent(raw)
				} else {
					_, err = operation.DecodeExecutionRecord(raw)
				}
				demandNonNilError(t, err, tc.want)
			})
		}
		t.Run(entry+"/invalid JSON outranks duplicate", func(t *testing.T) {
			intent := validIntent()
			intent.Preconditions = append(intent.Preconditions, intent.Preconditions[0])
			var value any = intent
			if entry == "ExecutionRecord" {
				id := semreg.InvalidValue
				value = operation.ExecutionRecord{Contract: operation.ContractOperationV1, Intent: intent, Outcome: operation.OutcomeRejected, ErrorID: &id, OutcomeEvidence: []semreg.EvidenceRef{}}
			}
			raw := append(mustJSON(t, value), []byte(" {}")...)
			var err error
			if entry == "Intent" {
				_, err = operation.DecodeIntent(raw)
			} else {
				_, err = operation.DecodeExecutionRecord(raw)
			}
			demandNonNilError(t, err, semreg.InvalidJSON)
		})
	}
}

func TestFourthCorrectionTypedPreconditionIdentityMatrix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*operation.Precondition)
		want   semreg.ErrorID
	}{
		{"invalid non-key value", func(p *operation.Precondition) { p.Expected = semreg.Value{} }, semreg.DuplicateKey},
		{"missing fact", func(p *operation.Precondition) { p.Fact = semreg.FactKey{} }, semreg.MissingMember},
		{"missing candidate", func(p *operation.Precondition) { p.CandidateID = "" }, semreg.InvalidIdentifier},
		{"zero revision", func(p *operation.Precondition) { p.CandidateRevision = "0" }, semreg.InvalidIdentifier},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intent := validIntent()
			tc.mutate(&intent.Preconditions[0])
			intent.Preconditions = append(intent.Preconditions, intent.Preconditions[0])
			demandNonNilError(t, intent.Validate(), tc.want)
		})
	}
}

func TestFourthCorrectionRecordEvidenceIdentityMatrix(t *testing.T) {
	for _, entry := range []string{"Record", "RecordRejection"} {
		for _, tc := range []struct {
			name    string
			mutate  func(*semreg.EvidenceRef)
			want    semreg.ErrorID
			sibling bool
		}{
			{"missing owner", func(e *semreg.EvidenceRef) { e.Owner = "" }, semreg.InvalidEvidence, false},
			{"missing kind", func(e *semreg.EvidenceRef) { e.Kind = "" }, semreg.InvalidEvidence, false},
			{"missing contract", func(e *semreg.EvidenceRef) { e.Contract = "" }, semreg.InvalidEvidence, false},
			{"missing digest", func(e *semreg.EvidenceRef) { e.Digest = "" }, semreg.InvalidEvidence, false},
			{"invalid non-key access", func(e *semreg.EvidenceRef) { e.Access = "invalid" }, semreg.DuplicateKey, false},
			{"valid duplicate after malformed sibling", func(*semreg.EvidenceRef) {}, semreg.DuplicateKey, true},
		} {
			t.Run(entry+"/"+tc.name, func(t *testing.T) {
				f := newOperationFixture(t)
				e := evidence(1)
				tc.mutate(&e)
				evidenceSet := []semreg.EvidenceRef{e, e}
				if tc.sibling {
					bad := evidence(2)
					bad.Owner = ""
					evidenceSet = append([]semreg.EvidenceRef{bad}, evidenceSet...)
				}
				intentBefore := mustJSON(t, f.intent)
				evidenceBefore := mustJSON(t, evidenceSet)
				var err error
				if entry == "Record" {
					a := mustAdmit(t, f)
					r := admittedRecord(a)
					r.Outcome, r.Dispatch, r.OutcomeEvidence = operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true), evidenceSet
					recordBefore := mustJSON(t, r)
					_, err = f.kernel.Record(a, r, nil)
					demandNonNilError(t, err, tc.want)
					if _, ok := a.Recorded(); ok {
						t.Fatal("failed record installed state")
					}
					if !bytes.Equal(recordBefore, mustJSON(t, r)) {
						t.Fatal("failed record mutated input")
					}
					good := admittedRecord(a)
					good.Outcome, good.Dispatch = operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true)
					if _, recoveryErr := f.kernel.Record(a, good, nil); recoveryErr != nil {
						t.Fatalf("valid record unavailable after rejection: %v", recoveryErr)
					}
				} else {
					_, err = f.kernel.RecordRejection(f.intent, semreg.AuthorityMissing, evidenceSet)
					demandNonNilError(t, err, tc.want)
					if f.pack.intentCalls != 0 {
						t.Fatal("failed rejection invoked semantic intent hook")
					}
					mustAdmit(t, f)
				}
				if f.pack.readbackCalls != 0 {
					t.Fatal("failed input invoked readback hook")
				}
				if !bytes.Equal(intentBefore, mustJSON(t, f.intent)) || !bytes.Equal(evidenceBefore, mustJSON(t, evidenceSet)) {
					t.Fatal("failed input mutated caller values")
				}
			})
		}
	}
}

func demandNonNilError(t *testing.T, err error, want semreg.ErrorID) {
	t.Helper()
	if err == nil || semreg.ErrorIdentifier(err) != want {
		t.Fatalf("got %v; want non-nil %s", err, want)
	}
}
