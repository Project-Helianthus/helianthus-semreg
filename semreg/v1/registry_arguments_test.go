package semreg

import "testing"

func TestRegistryFactIndependentArguments(t *testing.T) {
	good := Value{Kind: ValueBoolean, Boolean: boolPtr(true)}
	orderedBad := validKey()
	orderedBad.Dimensions = []Dimension{{ID: "dimension.b", Value: good}, {ID: "dimension.a", Value: good}}
	kindBad := validKey()
	kindBad.Dimensions = []Dimension{{ID: "dimension.a", Value: Value{Kind: ValueTime, Time: conditionalPtr(validTimes().ReceivedAt)}}}
	for _, row := range []struct {
		name    string
		key     FactKey
		keyID   ErrorID
		value   Value
		valueID ErrorID
	}{
		{"order-and-decimal", orderedBad, NoncanonicalOrder, Value{Kind: ValueQuantity, Quantity: &Quantity{Number: Decimal{Coefficient: "10"}, Unit: "unit.volt"}}, InvalidDecimal},
		{"kind-and-unit", kindBad, InvalidValue, Value{Kind: ValueQuantity, Quantity: &Quantity{Number: Decimal{Coefficient: "1"}, Unit: "!"}}, InvalidIdentifier},
		{"order-and-duplicate", orderedBad, NoncanonicalOrder, Value{Kind: ValueSymbols, Symbols: []Symbol{{Namespace: "native.test", Token: "x"}, {Namespace: "native.test", Token: "x"}}}, DuplicateKey},
	} {
		for _, mode := range []string{"combined", "key-only", "value-only", "valid", "nil", "nil-bad-key"} {
			t.Run(row.name+"/"+mode, func(t *testing.T) {
				key, value, want := validKey(), &good, ErrorID("")
				switch mode {
				case "combined":
					key, value, want = row.key, &row.value, row.valueID
				case "key-only":
					key, want = row.key, row.keyID
				case "value-only":
					value, want = &row.value, row.valueID
				case "nil":
					value = nil
				case "nil-bad-key":
					key, value, want = row.key, nil, row.keyID
				}
				pack := PackRef{ID: "pack.test", Version: "1.0.0"}
				hook := &countingValidator{pack: pack, index: DefinitionIndex{Pack: pack, Fields: []DefinitionRef{}, Services: []DefinitionRef{}, Capabilities: []DefinitionRef{}, Operations: []DefinitionRef{}, EffectRules: []DefinitionRef{}}}
				registry, err := NewRegistry(hook)
				if err != nil {
					t.Fatal(err)
				}
				checkMatrixError(t, registry.ValidateFact(key, value), want)
				calls := 0
				if want == "" {
					calls = 1
				}
				if hook.factCalls != calls {
					t.Fatalf("direct fact calls=%d want=%d", hook.factCalls, calls)
				}
				candidate := validCandidate("a", true)
				candidate.Key = key
				candidate.Value = value
				// Nil is a valid pack-hook argument. The complete record separately
				// supplies Quality that allows the value to be absent.
				if value == nil {
					candidate.Quality.Qualification = QualificationUnsupported
					candidate.Quality.Promotion = PromotionUnpromoted
				}
				checkConditionalForms(t, candidate, want)
				checkMatrixError(t, registry.ValidateFactCandidate(candidate), want)
				if hook.factCalls != 2*calls || hook.fieldCalls != 0 || hook.serviceCalls != 0 || hook.capabilityCalls != 0 {
					t.Fatalf("unexpected dispatch counts: %+v", hook)
				}
			})
		}
	}
}

func TestRegistryFieldIndependentArguments(t *testing.T) {
	for _, mode := range []string{"valid", "duplicate-before-ref", "ref-before-decimal", "decimal-before-mismatch", "missing-version"} {
		t.Run(mode, func(t *testing.T) {
			pack := PackRef{ID: "pack.test", Version: "1.0.0"}
			ref := DefinitionRef{Pack: pack, ID: "field.a", Version: "1.0.0"}
			hook := &countingValidator{pack: pack, index: indexWith(pack, DefinitionField, ref)}
			registry, err := NewRegistry(hook)
			if err != nil {
				t.Fatal(err)
			}
			field := TypedField{ID: ref.ID, Value: Value{Kind: ValueBoolean, Boolean: boolPtr(true)}}
			want := ErrorID("")
			switch mode {
			case "duplicate-before-ref":
				ref.ID = "!"
				field.Value = Value{Kind: ValueSymbols, Symbols: []Symbol{{Namespace: "native.test", Token: "x"}, {Namespace: "native.test", Token: "x"}}}
				want = DuplicateKey
			case "ref-before-decimal":
				ref.ID = "!"
				field.Value = Value{Kind: ValueQuantity, Quantity: &Quantity{Number: Decimal{Coefficient: "10"}, Unit: "unit.volt"}}
				want = InvalidIdentifier
			case "decimal-before-mismatch":
				field.ID = "field.b"
				field.Value = Value{Kind: ValueQuantity, Quantity: &Quantity{Number: Decimal{Coefficient: "10"}, Unit: "unit.volt"}}
				want = InvalidDecimal
			case "missing-version":
				ref.Version = "2.0.0"
				want = DefinitionOwnerMissing
			}
			checkMatrixError(t, registry.ValidateField(ref, field), want)
			calls := 0
			if want == "" {
				calls = 1
			}
			if hook.fieldCalls != calls || hook.factCalls != 0 {
				t.Fatalf("unexpected dispatch counts: %+v", hook)
			}
		})
	}
}
