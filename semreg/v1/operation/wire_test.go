package operation_test

import (
	"bytes"
	"encoding/json"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestIntentStrictWireAndErrorPrecedence(t *testing.T) {
	raw, err := json.Marshal(validIntent())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.DecodeIntent(raw); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		raw  []byte
		want semreg.ErrorID
	}{
		{"invalid json", append(append([]byte(nil), raw...), 'x'), semreg.InvalidJSON},
		{"duplicate", bytes.Replace(raw, []byte(`{"contract":`), []byte(`{"contract":"helianthus.semantic.operation/v1","contract":`), 1), semreg.DuplicateKey},
		{"unknown before invalid identifier", bytes.Replace(raw, []byte(`{"contract":`), []byte(`{"future":true,"contract":`), 1), semreg.UnknownMember},
		{"null optional", bytes.Replace(raw, []byte(`"allow_degraded":false`), []byte(`"instance_id":null,"allow_degraded":false`), 1), semreg.MissingMember},
		{"uint64 number token", bytes.Replace(raw, []byte(`"expected_driver_generation":"1"`), []byte(`"expected_driver_generation":1`), 1), semreg.InvalidValue},
		{"caller native route forbidden by shape", bytes.Replace(raw, []byte(`{"contract":`), []byte(`{"binding_id":"binding:caller","contract":`), 1), semreg.UnknownMember},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := operation.DecodeIntent(test.raw)
			errorID(t, err, test.want)
		})
	}
}

func TestOperationCanonicalJSONAndHashDeterministic(t *testing.T) {
	intent := validIntent()
	first, err := semreg.CanonicalJSON(intent)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := operation.DecodeIntent(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := semreg.CanonicalJSON(decoded)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("canonical round trip: %v", err)
	}
	firstDigest, err := semreg.DigestRecord(intent)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := semreg.DigestRecord(decoded)
	if err != nil || firstDigest != secondDigest {
		t.Fatalf("deterministic digest: %q %q %v", firstDigest, secondDigest, err)
	}
}

func TestExecutionRecordStrictWireAndHash(t *testing.T) {
	fixture := newOperationFixture(t)
	admission := mustAdmit(t, fixture)
	record := admittedRecord(admission)
	record.Outcome, record.Dispatch = operation.OutcomeIndeterminate, dispatch(operation.DeliveryUnknown, true)
	raw, err := semreg.CanonicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := operation.DecodeExecutionRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := semreg.CanonicalJSON(decoded)
	if !bytes.Equal(raw, second) {
		t.Fatal("execution canonical round trip")
	}
	firstDigest, _ := semreg.DigestRecord(record)
	secondDigest, _ := semreg.DigestRecord(decoded)
	if firstDigest != secondDigest {
		t.Fatal("execution deterministic digest")
	}
	unknown := bytes.Replace(raw, []byte(`{"admitted_at":`), []byte(`{"future":true,"admitted_at":`), 1)
	_, err = operation.DecodeExecutionRecord(unknown)
	errorID(t, err, semreg.UnknownMember)
}
