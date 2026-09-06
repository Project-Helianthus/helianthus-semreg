package operation_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// Admission validates this exact argument shape. Readback deliberately relies
// on that guarantee, then mutates both detached inputs to test ownership.
type thirdCorrectionPack struct {
	*testOperationPack
	want operation.Intent
}

func (p *thirdCorrectionPack) EvaluateReadback(intent operation.Intent, candidate semreg.FactCandidate) (operation.ReadbackRelation, error) {
	p.readbackCalls++
	if !reflect.DeepEqual(intent, p.want) {
		panic("readback received unadmitted intent")
	}
	confirms := *intent.Arguments[0].Value.Boolean == *candidate.Value.Boolean
	*intent.Arguments[0].Value.Boolean = false
	*candidate.Value.Boolean = false
	if confirms {
		return operation.ReadbackConfirms, nil
	}
	return operation.ReadbackContradicts, nil
}

func TestThirdCorrectionRecordBindingMatrix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*operation.ExecutionRecord)
		want   semreg.ErrorID
	}{
		{"empty admitted arguments", func(r *operation.ExecutionRecord) { r.Intent.Arguments = []semreg.TypedField{} }, semreg.RevisionConflict},
		{"changed argument value", func(r *operation.ExecutionRecord) { r.Intent.Arguments[0].Value = boolValue(false) }, semreg.RevisionConflict},
		{"changed binding", func(r *operation.ExecutionRecord) { r.Route.BindingID = "binding:other" }, semreg.RevisionConflict},
		{"changed admission revision", func(r *operation.ExecutionRecord) { r.AdmittedRevision.Facts = "99" }, semreg.RevisionConflict},
		{"changed admission time", func(r *operation.ExecutionRecord) { r.AdmittedAt.UnixNanoseconds = "151" }, semreg.RevisionConflict},
		{"missing readback candidate", func(r *operation.ExecutionRecord) { r.Readback.CandidateID = "candidate:absent" }, semreg.DanglingReference},
		{"changed readback generation", func(r *operation.ExecutionRecord) { r.Readback.DriverGeneration = "2" }, semreg.StaleDriverGeneration},
		{"causal budget and generation", func(r *operation.ExecutionRecord) { r.Intent.Causal.HopCount = 99; r.Readback.DriverGeneration = "2" }, semreg.StaleDriverGeneration},
		{"foreign admission", func(r *operation.ExecutionRecord) {}, semreg.DanglingReference},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOperationFixture(t)
			p := &thirdCorrectionPack{testOperationPack: &testOperationPack{}, want: f.intent}
			var err error
			f.kernel, err = operation.NewKernel(p)
			if err != nil {
				t.Fatal(err)
			}
			a := mustAdmit(t, f)
			later := applyReadback(t, f, true, "220", "220")
			beforeSnapshot := mustJSON(t, later)
			submitted := admittedRecord(a)
			submitted.Outcome, submitted.Dispatch, submitted.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), readback(later, operation.ReadbackConfirms)
			goodBytes := mustJSON(t, submitted)
			tc.mutate(&submitted)
			before := mustJSON(t, submitted)
			kernel := f.kernel
			if tc.name == "foreign admission" {
				kernel, err = operation.NewKernel(p)
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err = kernel.Record(a, submitted, &later)
			errorID(t, err, tc.want)
			if p.readbackCalls != 0 {
				t.Fatalf("unsafe readback calls: %d", p.readbackCalls)
			}
			if _, ok := a.Recorded(); ok {
				t.Fatal("rejected record installed")
			}
			if !bytes.Equal(before, mustJSON(t, submitted)) || !bytes.Equal(beforeSnapshot, mustJSON(t, later)) || !reflect.DeepEqual(a.Intent(), f.intent) {
				t.Fatal("rejected input/admission mutated")
			}
			var good operation.ExecutionRecord
			if err := json.Unmarshal(goodBytes, &good); err != nil {
				t.Fatal(err)
			}
			stored, err := f.kernel.Record(a, good, &later)
			if err != nil || p.readbackCalls != 1 {
				t.Fatalf("valid readback after rejection: calls=%d err=%v", p.readbackCalls, err)
			}
			if !bytes.Equal(goodBytes, mustJSON(t, stored)) || !bytes.Equal(goodBytes, mustJSON(t, good)) || !bytes.Equal(beforeSnapshot, mustJSON(t, later)) {
				t.Fatal("hook inputs were not detached")
			}
		})
	}
}

// Serialization precedence applies across independently supplied values on both
// append entry paths. Invalid original unions must survive even a failed call.
func TestThirdCorrectionRecordInputCollectionMatrix(t *testing.T) {
	for _, entry := range []string{"Record", "RecordRejection"} {
		for _, defect := range []string{"typed union", "invalid enum", "invalid evidence", "invalid contract"} {
			t.Run(entry+"/"+defect, func(t *testing.T) {
				f := newOperationFixture(t)
				a := mustAdmit(t, f)
				later := applyReadback(t, f, true, "220", "220")
				r := admittedRecord(a)
				r.Outcome, r.Dispatch, r.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), readback(later, operation.ReadbackConfirms)
				id := semreg.InvalidValue
				switch defect {
				case "typed union":
					r.Intent.Arguments[0].Value.Symbols = []semreg.Symbol{}
				case "invalid enum":
					r.Dispatch.Delivery = "invalid"
					id = "invalid"
				case "invalid evidence":
					r.Intent.Authority.Owner = semreg.DefinitionID("owner." + string([]byte{255}))
				case "invalid contract":
					r.Intent.Contract = "wrong/v1"
				}
				// The duplicate is valid and independently identifiable in both paths.
				ev := []semreg.EvidenceRef{evidence(1), evidence(1)}
				later.Sources = append(later.Sources, later.Sources[0])
				originalUnion := r.Intent.Arguments[0].Value.Symbols
				originalOwner := r.Intent.Authority.Owner
				before := mustJSON(t, r)
				var err error
				if entry == "Record" {
					_, err = f.kernel.Record(a, r, &later)
				} else {
					// Use a fresh kernel so rejection failure must leave this key free.
					k, e := operation.NewKernel(f.pack)
					if e != nil {
						t.Fatal(e)
					}
					calls := f.pack.intentCalls
					_, err = k.RecordRejection(r.Intent, id, ev)
					if f.pack.intentCalls != calls {
						t.Fatal("rejection reran intent hook")
					}
					if _, e := k.Admit(f.snapshot, f.current, f.intent, operation.AuthorityResolverFunc(authorize)); e != nil {
						t.Fatalf("rejection retained key: %v", e)
					}
				}
				errorID(t, err, semreg.DuplicateKey)
				if !reflect.DeepEqual(originalUnion, r.Intent.Arguments[0].Value.Symbols) || originalOwner != r.Intent.Authority.Owner || !bytes.Equal(before, mustJSON(t, r)) {
					t.Fatal("original typed input was repaired")
				}
				if _, ok := a.Recorded(); ok {
					t.Fatal("invalid record installed")
				}
				if f.pack.readbackCalls != 0 {
					t.Fatal("unsafe readback hook")
				}
			})
		}
	}
	t.Run("snapshot ignored without readback", func(t *testing.T) {
		f := newOperationFixture(t)
		a := mustAdmit(t, f)
		r := admittedRecord(a)
		r.Outcome, r.Dispatch = operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true)
		bad := semreg.Snapshot{}
		if _, err := f.kernel.Record(a, r, &bad); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("nil admission still collects snapshot", func(t *testing.T) {
		f := newOperationFixture(t)
		a := mustAdmit(t, f)
		later := applyReadback(t, f, true, "220", "220")
		r := admittedRecord(a)
		r.Outcome, r.Dispatch, r.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), readback(later, operation.ReadbackConfirms)
		later.Sources = append(later.Sources, later.Sources[0])
		_, err := f.kernel.Record(nil, r, &later)
		errorID(t, err, semreg.DuplicateKey)
	})
}

// Public operation document discriminators have a different error class from
// required evidence metadata. Combine wire mutations to exercise precedence.
func TestThirdCorrectionDecodeDocumentMatrix(t *testing.T) {
	f := newOperationFixture(t)
	a := mustAdmit(t, f)
	r := admittedRecord(a)
	r.Outcome, r.Dispatch = operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true)
	for _, entry := range []struct {
		name   string
		value  semreg.Record
		decode func([]byte) (semreg.Record, error)
	}{
		{"Intent", f.intent, func(b []byte) (semreg.Record, error) { return operation.DecodeIntent(b) }},
		{"ExecutionRecord", r, func(b []byte) (semreg.Record, error) { return operation.DecodeExecutionRecord(b) }},
	} {
		canonical, err := semreg.CanonicalJSON(entry.value)
		if err != nil {
			t.Fatal(err)
		}
		for _, mutation := range []struct{ name, contract string }{
			{"missing", ""}, {"null", "null"}, {"number", "17"}, {"wrong", `"other/v1"`}, {"boolean", "false"}, {"object", "{}"}, {"array", "[]"},
		} {
			for _, combined := range []string{"alone", "duplicate member", "duplicate collection", "unknown member", "missing other", "trailing JSON", "invalid Unicode"} {
				t.Run(entry.name+"/"+mutation.name+"/"+combined, func(t *testing.T) {
					var object map[string]json.RawMessage
					if err := json.Unmarshal(canonical, &object); err != nil {
						t.Fatal(err)
					}
					if mutation.contract == "" {
						delete(object, "contract")
					} else {
						object["contract"] = json.RawMessage(mutation.contract)
					}
					want := semreg.InvalidContract
					switch combined {
					case "duplicate collection":
						if entry.name == "Intent" {
							object["arguments"] = bytes.Replace(object["arguments"], []byte("]"), append([]byte(","), object["arguments"][1:]...), 1)
						} else {
							ev, _ := json.Marshal([]semreg.EvidenceRef{evidence(1), evidence(1)})
							object["outcome_evidence"] = ev
						}
						want = semreg.DuplicateKey
					case "unknown member":
						object["future"] = json.RawMessage("true")
					case "missing other":
						if entry.name == "Intent" {
							delete(object, "intent_id")
						} else {
							delete(object, "outcome")
						}
					}
					raw, err := json.Marshal(object)
					if err != nil {
						t.Fatal(err)
					}
					switch combined {
					case "duplicate member":
						raw = append([]byte(`{"future":1,"future":2,`), raw[1:]...)
						want = semreg.DuplicateKey
					case "trailing JSON":
						raw = append(raw, []byte(" {}")...)
						want = semreg.InvalidJSON
					case "invalid Unicode":
						raw = append([]byte(`{"future":"\ud800",`), raw[1:]...)
						want = semreg.InvalidJSON
					}
					_, err = entry.decode(raw)
					errorID(t, err, want)
				})
			}
		}
		for _, mutation := range []struct {
			name, token string
			want        semreg.ErrorID
		}{
			{"missing", "", semreg.MissingMember}, {"null", "null", semreg.MissingMember}, {"number", "17", semreg.InvalidValue}, {"malformed", `""`, semreg.InvalidEvidence},
		} {
			t.Run(entry.name+"/nested evidence/"+mutation.name, func(t *testing.T) {
				var object map[string]any
				if err := json.Unmarshal(canonical, &object); err != nil {
					t.Fatal(err)
				}
				intent := object
				if entry.name == "ExecutionRecord" {
					intent = object["intent"].(map[string]any)
				}
				authority := intent["authority"].(map[string]any)
				if mutation.token == "" {
					delete(authority, "contract")
				} else {
					var token any
					if err := json.Unmarshal([]byte(mutation.token), &token); err != nil {
						t.Fatal(err)
					}
					authority["contract"] = token
				}
				raw := mustJSON(t, object)
				_, err := entry.decode(raw)
				errorID(t, err, mutation.want)
			})
		}
		t.Run(entry.name+"/valid round trip", func(t *testing.T) {
			decoded, err := entry.decode(canonical)
			if err != nil {
				t.Fatal(err)
			}
			again, err := semreg.CanonicalJSON(decoded)
			if err != nil || !bytes.Equal(canonical, again) {
				t.Fatalf("round trip: %v", err)
			}
		})
	}
}
