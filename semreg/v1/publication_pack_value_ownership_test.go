package semreg

import (
	"bytes"
	"testing"
)

// These synthetic pack.test/v1 vectors extend the boolean ownership controls:
// quantity has a nested decimal behind a pointer; symbols has a mutable slice
// nested inside both a candidate and a capability's constraint collection.
func TestK16PublicationPackValueOwnership(t *testing.T) {
	cases := []struct {
		name   string
		value  func() Value
		mutate func(*Value)
	}{
		{
			name: "quantity",
			value: func() Value {
				return Value{Kind: ValueQuantity, Quantity: &Quantity{
					Number: Decimal{Coefficient: "125", Exponent10: -1}, Unit: "unit.watt",
				}}
			},
			mutate: func(v *Value) {
				v.Quantity.Number.Coefficient += "1"
				v.Quantity.Number.Exponent10--
				v.Quantity.Unit = "unit.kilowatt"
			},
		},
		{
			name: "symbols",
			value: func() Value {
				return Value{Kind: ValueSymbols, Symbols: []Symbol{
					{Namespace: "state.test", Token: "active", Known: true},
					{Namespace: "state.test", Token: "ready", Known: true},
				}}
			},
			mutate: func(v *Value) {
				v.Symbols[0].Token += "-changed"
				v.Symbols[1].Token += "-changed"
			},
		},
	}
	for _, tc := range cases {
		for _, boundary := range []string{"input", "apply-result", "current-result", "replay-result", "rejected-input"} {
			t.Run(tc.name+"/"+boundary, func(t *testing.T) {
				kernel, err := NewPublicationKernel("asset:site", boundaryValidator())
				if err != nil {
					t.Fatal(err)
				}
				batch := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
				batch.FactUpserts[0].Value = retainedPtr(tc.value())
				batch.CapabilityUpserts[0].Constraints = []TypedField{{ID: "field.test", Value: tc.value()}}
				sealPublicationBatch(t, &batch)
				// Preserve an independent replay request before mutating any input.
				replayBatch := clonePublicationBatch(batch)
				applied, appliedRaw, err := kernel.Apply(batch, publicationMonotonic)
				if err != nil {
					t.Fatal(err)
				}
				wantRaw := append([]byte(nil), appliedRaw...)
				reader, readerRaw := mustCurrent(t, kernel)
				mutateSnapshot := func(snapshot *Snapshot, raw []byte) {
					candidate := candidateByID(t, *snapshot, "candidate:binding:a")
					tc.mutate(candidate.Value)
					tc.mutate(&snapshot.Capabilities[0].Constraints[0].Value)
					candidate.Evidence[0].Owner = "evidence.changed"
					raw[0] = '!'
				}
				switch boundary {
				case "input":
					tc.mutate(batch.FactUpserts[0].Value)
					tc.mutate(&batch.CapabilityUpserts[0].Constraints[0].Value)
					batch.FactUpserts[0].Evidence[0].Owner = "evidence.changed"
				case "apply-result":
					mutateSnapshot(&applied, appliedRaw)
				case "current-result":
					returned, raw := mustCurrent(t, kernel)
					mutateSnapshot(&returned, raw)
				case "replay-result":
					returned, raw, err := kernel.Apply(replayBatch, publicationMonotonic)
					if err != nil {
						t.Fatal(err)
					}
					mutateSnapshot(&returned, raw)
				case "rejected-input":
					next := publicationBatch(batch.AssetID, batch.SourceID, batch.SourceEpochID, "1", "2", "1")
					candidate := candidateByID(t, reader, "candidate:binding:a")
					candidate.Revision = "2"
					// Deliberately share the reader's nested values with the rejected request.
					next.FactUpserts = []FactCandidate{candidate}
					capability := reader.Capabilities[0]
					capability.Revision = "2"
					next.CapabilityUpserts = []CapabilityInstance{capability}
					service := reader.Services[0]
					service.Revision, service.BindingID = "2", "binding:missing"
					next.ServiceUpserts = []ServiceInstance{service}
					tc.mutate(next.FactUpserts[0].Value)
					tc.mutate(&next.CapabilityUpserts[0].Constraints[0].Value)
					sealPublicationBatch(t, &next)
					assertRejectedUnchanged(t, kernel, next, DanglingReference)
					// Use a different reader for the post-rejection alias check.
					reader, readerRaw = mustCurrent(t, kernel)
					tc.mutate(next.FactUpserts[0].Value)
					tc.mutate(&next.CapabilityUpserts[0].Constraints[0].Value)
					next.FactUpserts[0].Evidence[0].Owner = "evidence.changed"
				}
				assertHistoricalBytes(t, reader, wantRaw)
				if !bytes.Equal(readerRaw, wantRaw) {
					t.Fatal("another reader's canonical bytes changed")
				}
				current, currentRaw := mustCurrent(t, kernel)
				assertHistoricalBytes(t, current, wantRaw)
				if !bytes.Equal(currentRaw, wantRaw) {
					t.Fatal("retained canonical bytes changed")
				}
				replayed, replayRaw, err := kernel.Apply(replayBatch, publicationMonotonic)
				if err != nil || !bytes.Equal(replayRaw, wantRaw) {
					t.Fatalf("accepted replay changed: %v", err)
				}
				assertHistoricalBytes(t, replayed, wantRaw)
			})
		}
	}
}
