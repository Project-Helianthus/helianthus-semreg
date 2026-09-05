package semreg

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Contract-derived controls: docs-semantic@b16667d719defc7b0fef0400ee3ad387469018ac,
// kernel PackValidator/Snapshot and serialization digest/error determinism.
type retainingBoundaryValidator struct {
	countingValidator
	retained                   []any
	matchCalls, predicateCalls int
}

func (v *retainingBoundaryValidator) ValidateFact(k FactKey, value *Value) error {
	v.factCalls++
	v.retained = append(v.retained, k, value)
	return nil
}
func (v *retainingBoundaryValidator) ValidateCapability(c CapabilityInstance) error {
	v.capabilityCalls++
	v.retained = append(v.retained, c)
	return nil
}

func (v *retainingBoundaryValidator) ValidateService(s ServiceInstance) error {
	v.serviceCalls++
	v.retained = append(v.retained, s)
	return nil
}
func (v *retainingBoundaryValidator) ValidateField(r DefinitionRef, f TypedField) error {
	v.fieldCalls++
	v.retained = append(v.retained, r, f)
	return nil
}
func (v *retainingBoundaryValidator) MatchConstraints(c CapabilityInstance, f []TypedField) error {
	v.matchCalls++
	v.retained = append(v.retained, c, f)
	return nil
}
func (v *retainingBoundaryValidator) EvaluatePredicate(c FactCandidate, p PredicateOp, value Value) (bool, error) {
	v.predicateCalls++
	v.retained = append(v.retained, c, p, value)
	return true, nil
}
func (v *retainingBoundaryValidator) calls() int {
	return v.factCalls + v.serviceCalls + v.capabilityCalls + v.fieldCalls + v.matchCalls + v.predicateCalls
}

// Mutate only memory reachable through pointer/slice arguments, after return.
// Struct values themselves are passed by value and cannot alias the caller.
func mutateBoundaryAliases(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			mutateBoundaryAliases(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			mutateBoundaryAliases(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			mutateBoundaryAliases(v.Index(i))
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString("mutated")
		}
	case reflect.Bool:
		if v.CanSet() {
			v.SetBool(!v.Bool())
		}
	case reflect.Uint16:
		if v.CanSet() {
			v.SetUint(0)
		}
	}
}

func boundaryValidator() *retainingBoundaryValidator {
	p := PackRef{ID: "pack.test", Version: "1.0.0"}
	return &retainingBoundaryValidator{countingValidator: countingValidator{pack: p, index: DefinitionIndex{
		Pack: p, Fields: []DefinitionRef{{Pack: p, ID: "field.test", Version: "1.0.0"}},
		Services:     []DefinitionRef{{Pack: p, ID: "service.test", Version: "1.0.0"}},
		Capabilities: []DefinitionRef{{Pack: p, ID: "capability.test", Version: "1.0.0"}},
		Operations:   []DefinitionRef{}, EffectRules: []DefinitionRef{},
	}}}
}

func TestPublicationBoundaryRetainedCallbacks(t *testing.T) {
	for _, family := range []string{"value", "dimensions", "constraints", "activation-evidence"} {
		t.Run(family, func(t *testing.T) {
			hook := boundaryValidator()
			k, err := NewPublicationKernel("asset:site", hook)
			if err != nil {
				t.Fatal(err)
			}
			b := completePublicationBatch("asset:site", "source:a", "epoch:a", "binding:a", "1", "1", "0")
			b.FactUpserts[0].Key.Dimensions = []Dimension{{ID: "dimension.test", Value: booleanValue(true)}}
			b.CapabilityUpserts[0].Constraints = []TypedField{{ID: "field.test", Value: booleanValue(true)}}
			sealPublicationBatch(t, &b)
			historical, raw, err := k.Apply(b, publicationMonotonic)
			if err != nil {
				t.Fatal(err)
			}
			if hook.factCalls != 1 || hook.serviceCalls != 1 || hook.capabilityCalls != 1 {
				t.Fatalf("one owner per upsert: %+v", hook.countingValidator)
			}
			for _, arg := range hook.retained {
				switch x := arg.(type) {
				case *Value:
					if family == "value" {
						*x.Boolean = false
					}
				case FactKey:
					if family == "dimensions" {
						*x.Dimensions[0].Value.Boolean = false
					}
				case CapabilityInstance:
					if family == "constraints" {
						*x.Constraints[0].Value.Boolean = false
					}
					if family == "activation-evidence" {
						x.ActivationEvidence[0].Digest = publicEvidence("b").Digest
					}
				}
			}
			current, currentRaw, _ := k.Current()
			if !reflect.DeepEqual(current, historical) || !bytes.Equal(raw, currentRaw) {
				t.Error("callback mutation changed current fields/bytes without publication")
			}
			canonical, err := CanonicalJSON(current)
			if err != nil || !bytes.Equal(canonical, currentRaw) {
				t.Error("Current fields disagree with retained canonical bytes")
			}
			replay, replayRaw, err := k.Apply(b, publicationMonotonic)
			if err != nil || !reflect.DeepEqual(replay, current) || !bytes.Equal(replayRaw, currentRaw) {
				t.Error("replay disagrees with Current at the same revision")
			}
			assertHistoricalBytes(t, historical, raw)
			if hook.factCalls != 1 || hook.serviceCalls != 1 || hook.capabilityCalls != 1 {
				t.Fatal("replay invoked hooks")
			}
		})
	}
}

// Independent digest oracle is used only after constructing a complete record.
// It deliberately does not call production ComputedDigest/Validate.
func boundarySealBytes(t *testing.T, b *PublicationBatch) {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "batch_digest")
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = canonicalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	b.BatchDigest = Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(raw)))
}

func TestPublicationBoundaryDigestPrecedence(t *testing.T) {
	for _, fault := range []string{"valid", "retired-source", "fenced-binding", "self-cycle", "identity-proof", "activation-proof", "causal-budget"} {
		for _, digest := range []string{"correct", "incorrect", "malformed"} {
			t.Run(fault+"/"+digest, func(t *testing.T) {
				k, a, old, oldRaw := invariantInitial(t)
				b := publicationBatch(a.AssetID, a.SourceID, a.SourceEpochID, "1", "2", "1")
				want := ErrorID("")
				switch fault {
				case "retired-source":
					s := a.SourceUpserts[0]
					s.State = SourceRetired
					b.SourceUpserts, want = []SourceDescriptor{s}, StaleSourceEpoch
				case "fenced-binding":
					s := a.BindingUpserts[0]
					s.State = BindingFenced
					b.BindingUpserts, want = []NativeBinding{s}, StaleDriverGeneration
				case "self-cycle":
					c := publicationDerivedCandidate("candidate:loop", "fact.loop", a.FactUpserts)
					c.Derivation.Inputs[0].CandidateID = c.CandidateID
					b.FactUpserts, want = []FactCandidate{c}, DerivationCycle
				case "identity-proof":
					link := a.IdentityLinkUpserts[0]
					link.Revision = "2"
					link.Basis = []EvidenceRef{}
					b.IdentityLinkUpserts, want = []IdentityLink{link}, IdentityNotQualified
				case "activation-proof":
					cap := a.CapabilityUpserts[0]
					cap.Revision = "2"
					cap.ActivationEvidence = []EvidenceRef{}
					b.CapabilityUpserts, want = []CapabilityInstance{cap}, CapabilityNotQualified
				case "causal-budget":
					c := a.FactUpserts[0]
					c.Revision = "2"
					c.Causal = pointerRecord(validCausal())
					c.Causal.MaxHops = 0
					b.FactUpserts, want = []FactCandidate{c}, CausalBudgetExceeded
				}
				boundarySealBytes(t, &b)
				if digest == "incorrect" {
					b.BatchDigest, want = Digest("sha256:"+strings.Repeat("f", 64)), DigestMismatch
				}
				if digest == "malformed" {
					b.BatchDigest, want = "bad", InvalidValue
				}
				before, _ := json.Marshal(b)
				for _, form := range []string{"Validate", "Decode", "CanonicalJSON", "Apply"} {
					t.Run(form, func(t *testing.T) {
						var err error
						switch form {
						case "Validate":
							err = b.Validate()
						case "Decode":
							_, err = Decode[PublicationBatch](before)
						case "CanonicalJSON":
							_, err = CanonicalJSON(b)
						case "Apply":
							if want != "" {
								assertRejectedUnchanged(t, k, b, want)
								return
							}
							_, _, err = k.Apply(b, publicationMonotonic)
						}
						checkMatrixError(t, err, want)
					})
				}
				after, _ := json.Marshal(b)
				if !bytes.Equal(before, after) {
					t.Fatal("validation mutated caller batch")
				}
				assertHistoricalBytes(t, old, oldRaw)
			})
		}
	}
}

func boundaryValues() []Value {
	return []Value{
		booleanValue(true),
		{Kind: ValueQuantity, Quantity: &Quantity{Number: Decimal{Coefficient: "123", Exponent10: -2}, Unit: "unit.volt"}},
		{Kind: ValueText, Text: pointerRecord("exact text")},
		{Kind: ValueSymbol, Symbol: &Symbol{Namespace: "native.test", Token: "a"}},
		{Kind: ValueSymbols, Symbols: []Symbol{{Namespace: "native.test", Token: "a"}, {Namespace: "native.test", Token: "b"}}},
		{Kind: ValueTime, Time: pointerRecord(validTimes().ReceivedAt)},
	}
}

func TestPublicationBoundaryHookMatrix(t *testing.T) {
	for _, method := range []string{"fact", "candidate", "service", "capability", "field", "constraints", "predicate", "derived-predicate"} {
		for _, route := range []string{"registry", "validator", "definition"} {
			// These callbacks currently have only the public PackValidator surface.
			if route == "registry" && (method == "constraints" || strings.Contains(method, "predicate")) {
				continue
			}
			for _, mode := range []string{"valid", "invalid", "wrong-owner"} {
				t.Run(method+"/"+route+"/"+mode, func(t *testing.T) {
					hook, other := boundaryValidator(), boundaryValidator()
					other.pack.ID, other.index.Pack.ID = "pack.other", "pack.other"
					for _, refs := range [][]DefinitionRef{other.index.Fields, other.index.Services, other.index.Capabilities} {
						for i := range refs {
							refs[i].Pack = other.pack
							refs[i].ID += ".other"
						}
					}
					r, err := NewRegistry(other, hook)
					if err != nil {
						t.Fatal(err)
					}
					for _, value := range boundaryValues() {
						c := validCandidate("a", true)
						if method == "derived-predicate" {
							c = publicationDerivedCandidate("candidate:derived", "fact.derived", []FactCandidate{c})
							c.Derivation.Evidence = []EvidenceRef{publicEvidence("a")}
						}
						c.Value = &value
						for i, dimension := range boundaryValues()[:4] {
							c.Key.Dimensions = append(c.Key.Dimensions, Dimension{ID: DefinitionID(fmt.Sprintf("dimension.d%d", i)), Value: dimension})
						}
						c.Quality.Reasons = []DefinitionID{"reason.test"}
						c.Times.SourceAt, c.Times.PhenomenonAt = pointerRecord(c.Times.ReceivedAt), pointerRecord(c.Times.ReceivedAt)
						c.Causal = pointerRecord(validCausal())
						c.Causal.ParentCorrelationID = pointerRecord(CorrelationID("correlation:parent"))
						s := validService()
						cap := publicationCapability("asset:a", "capability:a", "service:a", "binding:a", "epoch:a", "1")
						field := TypedField{ID: "field.test", Value: value}
						cap.Constraints = []TypedField{field}
						ref := hook.index.Fields[0]
						predicate := PredicateEqual
						want := ErrorID("")
						if mode == "invalid" {
							c.Key.FactID, s.InstanceID, cap.InstanceID, field.ID = "!", "!", "!", "!"
							want = InvalidIdentifier
						}
						if mode == "wrong-owner" {
							c.Key.PackVersion, s.Definition.Version, cap.Definition.Version, ref.Version = "2.0.0", "2.0.0", "2.0.0", "2.0.0"
							want = DefinitionOwnerMissing
						}
						kind, lookup := DefinitionField, hook.index.Fields[0]
						if method == "service" {
							kind, lookup = DefinitionService, hook.index.Services[0]
						}
						if method == "capability" || method == "constraints" {
							kind, lookup = DefinitionCapability, hook.index.Capabilities[0]
						}
						v, err := r.Validator(hook.pack)
						if route == "definition" {
							v, err = r.Definition(kind, lookup)
						}
						if err != nil {
							t.Fatal(err)
						}
						var args []any
						switch method {
						case "fact", "candidate":
							args = []any{c.Key, c.Value}
						case "service":
							args = []any{s}
						case "capability":
							args = []any{cap}
						case "field":
							args = []any{ref, field}
						case "constraints":
							args = []any{cap, []TypedField{field}}
						default:
							args = []any{c, predicate, value}
						}
						before, _ := json.Marshal(args)
						previousCalls := hook.calls()
						hook.retained = nil
						switch method {
						case "fact":
							if route == "registry" {
								err = r.ValidateFact(c.Key, c.Value)
							} else {
								err = v.ValidateFact(c.Key, c.Value)
							}
						case "candidate":
							if route == "registry" {
								err = r.ValidateFactCandidate(c)
							} else {
								err = v.ValidateFact(c.Key, c.Value)
							}
						case "service":
							if route == "registry" {
								err = r.ValidateService(s)
							} else {
								err = v.ValidateService(s)
							}
						case "capability":
							if route == "registry" {
								err = r.ValidateCapability(cap)
							} else {
								err = v.ValidateCapability(cap)
							}
						case "field":
							if route == "registry" {
								err = r.ValidateField(ref, field)
							} else {
								err = v.ValidateField(ref, field)
							}
						case "constraints":
							err = v.MatchConstraints(cap, args[1].([]TypedField))
						default:
							var result bool
							result, err = v.EvaluatePredicate(c, predicate, value)
							if result != (want == "") {
								t.Fatalf("predicate result=%v", result)
							}
						}
						checkMatrixError(t, err, want)
						calls := 0
						if want == "" {
							calls = 1
							if !reflect.DeepEqual(args, hook.retained) {
								t.Fatal("hook input values changed at dispatch")
							}
							for _, arg := range hook.retained {
								mutateBoundaryAliases(reflect.ValueOf(arg))
							}
						}
						if hook.calls()-previousCalls != calls || other.calls() != 0 {
							t.Fatal("not exactly one owner for valid / zero hooks for invalid")
						}
						after, _ := json.Marshal(args)
						if !bytes.Equal(before, after) {
							t.Fatalf("%s callback retained caller aliases", value.Kind)
						}
					}
				})
			}
		}
	}
}

func TestPublicationBoundaryUnsafeDigestInputs(t *testing.T) {
	for _, fault := range []string{"contract", "missing-collection", "missing-path", "missing-identity-proof", "missing-activation-proof", "identifier", "decimal", "value", "time", "evidence", "enum", "bounds", "order", "duplicate"} {
		t.Run(fault, func(t *testing.T) {
			k, a, _, _ := invariantInitial(t)
			b := clonePublicationBatch(a)
			b.Sequence, b.ExpectedSemanticRevision = "2", "1"
			b.SourceUpserts[0].State = SourceRetired
			b.BatchDigest = Digest("sha256:" + strings.Repeat("f", 64))
			var want ErrorID
			switch fault {
			case "contract":
				b.Contract, want = "other.contract/v1", InvalidContract
			case "missing-collection":
				b.FactWithdrawals, want = nil, MissingMember
			case "missing-path":
				b.FactUpserts[0].BindingID, want = nil, MissingMember
			case "missing-identity-proof":
				b.IdentityLinkUpserts[0].Basis, want = nil, MissingMember
			case "missing-activation-proof":
				b.CapabilityUpserts[0].ActivationEvidence, want = nil, MissingMember
			case "identifier":
				b.AssetID, want = "!", InvalidIdentifier
			case "decimal":
				b.FactUpserts[0].Value, want = &Value{Kind: ValueQuantity, Quantity: &Quantity{Number: Decimal{Coefficient: "10"}, Unit: "unit.volt"}}, InvalidDecimal
			case "value":
				b.FactUpserts[0].Value, want = &Value{Kind: ValueBoolean}, InvalidValue
			case "time":
				b.ObservedAt.UnixNanoseconds, want = "x", InvalidTime
			case "evidence":
				b.SourceUpserts[0].RegistryEvidence.Digest, want = "bad", InvalidEvidence
			case "enum":
				b.BindingUpserts[0].State, want = "bad", InvalidEnum
			case "bounds":
				for i := 0; i < 17; i++ {
					b.FactUpserts[0].Key.Dimensions = append(b.FactUpserts[0].Key.Dimensions, Dimension{ID: DefinitionID(fmt.Sprintf("dimension.d%02d", i)), Value: booleanValue(true)})
				}
				want = BoundsExceeded
			case "order":
				b.FactWithdrawals, want = []CandidateID{"candidate:z", "candidate:a"}, NoncanonicalOrder
			case "duplicate":
				b.FactWithdrawals, want = []CandidateID{"candidate:a", "candidate:a"}, DuplicateKey
			}
			checkMatrixError(t, b.Validate(), want)
			raw, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Decode[PublicationBatch](raw)
			checkMatrixError(t, err, want)
			_, err = CanonicalJSON(b)
			checkMatrixError(t, err, want)
			digest, err := b.ComputedDigest()
			checkMatrixError(t, err, want)
			if digest != "" {
				t.Fatal("fabricated digest through unsafe fields")
			}
			assertRejectedUnchanged(t, k, b, want)
		})
	}
	for _, fault := range []string{"absent", "null", "token", "unknown", "duplicate", "json"} {
		t.Run("wire/"+fault, func(t *testing.T) {
			b := publicationBatch("asset:a", "source:a", "epoch:a", "1", "1", "0")
			b.BatchDigest = Digest("sha256:" + strings.Repeat("f", 64))
			raw, _ := json.Marshal(b)
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			want := MissingMember
			switch fault {
			case "absent":
				delete(fields, "source_upserts")
			case "null":
				fields["source_upserts"] = json.RawMessage(`null`)
			case "token":
				fields["source_upserts"], want = json.RawMessage(`23`), InvalidValue
			case "unknown":
				fields["extra"], want = json.RawMessage(`[]`), UnknownMember
			case "duplicate":
				want = DuplicateKey
			case "json":
				want = InvalidJSON
			}
			raw, _ = json.Marshal(fields)
			if fault == "duplicate" {
				raw = append([]byte(`{"batch_id":"batch:a",`), raw[1:]...)
			}
			if fault == "json" {
				raw = raw[:len(raw)-1]
			}
			_, err := Decode[PublicationBatch](raw)
			checkMatrixError(t, err, want)
		})
	}
}

func TestPublicationBoundaryReplayPartition(t *testing.T) {
	for _, fault := range []string{"valid", "digest", "retired-source", "fenced-binding", "self-cycle"} {
		t.Run(fault, func(t *testing.T) {
			k, a, old, oldRaw := invariantInitial(t)
			b := clonePublicationBatch(a)
			want := ErrorID("")
			switch fault {
			case "digest":
				b.BatchDigest, want = Digest("sha256:"+strings.Repeat("f", 64)), SequenceConflict
			case "retired-source":
				b.SourceUpserts[0].State, want = SourceRetired, StaleSourceEpoch
			case "fenced-binding":
				b.BindingUpserts[0].State, want = BindingFenced, StaleDriverGeneration
			case "self-cycle":
				c := publicationDerivedCandidate("candidate:loop", "fact.loop", a.FactUpserts)
				c.Derivation.Inputs[0].CandidateID = c.CandidateID
				b.FactUpserts, want = []FactCandidate{c}, DerivationCycle
			}
			if want != "" {
				assertRejectedUnchanged(t, k, b, want)
				return
			}
			// Another source advances current state while A's last replay remains historical.
			n := completePublicationBatch(a.AssetID, "source:b", "epoch:b", "binding:b", "1", "1", "1")
			sealPublicationBatch(t, &n)
			if _, _, err := k.Apply(n, publicationMonotonic); err != nil {
				t.Fatal(err)
			}
			current, raw, _ := k.Current()
			replay, replayRaw, err := k.Apply(b, publicationMonotonic)
			if err != nil || !reflect.DeepEqual(replay, old) || !bytes.Equal(replayRaw, oldRaw) {
				t.Fatal("lost historical replay")
			}
			after, afterRaw, _ := k.Current()
			if !reflect.DeepEqual(current, after) || !bytes.Equal(raw, afterRaw) {
				t.Fatal("replay changed current")
			}
		})
	}
}

func TestPublicationBoundaryOptionalAndIndependentHookArguments(t *testing.T) {
	for _, mode := range []string{"nil-fact", "nil-candidate", "bad-value", "bad-field-value", "bad-constraint-value", "nil-constraints", "duplicate-constraints", "bad-predicate", "bad-predicate-value"} {
		t.Run(mode, func(t *testing.T) {
			hook := boundaryValidator()
			r, err := NewRegistry(hook)
			if err != nil {
				t.Fatal(err)
			}
			v, err := r.Validator(hook.pack)
			if err != nil {
				t.Fatal(err)
			}
			c := validCandidate("a", true)
			cap := publicationCapability("asset:a", "capability:a", "service:a", "binding:a", "epoch:a", "1")
			badValue := Value{Kind: ValueBoolean}
			field := TypedField{ID: "field.test", Value: badValue}
			want, calls := InvalidValue, 0
			switch mode {
			case "nil-fact":
				err, want, calls = v.ValidateFact(c.Key, nil), "", 1
			case "nil-candidate":
				c.Value = nil
				c.Quality.Qualification, c.Quality.Promotion = QualificationUnsupported, PromotionUnpromoted
				err, want, calls = r.ValidateFactCandidate(c), "", 1
			case "bad-value":
				err = v.ValidateFact(c.Key, &badValue)
			case "bad-field-value":
				err = v.ValidateField(hook.index.Fields[0], field)
			case "bad-constraint-value":
				err = v.MatchConstraints(cap, []TypedField{field})
			case "nil-constraints":
				err, want = v.MatchConstraints(cap, nil), MissingMember
			case "duplicate-constraints":
				err, want = v.MatchConstraints(cap, []TypedField{field, field}), DuplicateKey
			case "bad-predicate":
				_, err = v.EvaluatePredicate(c, "bad", booleanValue(true))
				want = InvalidEnum
			case "bad-predicate-value":
				_, err = v.EvaluatePredicate(c, PredicateEqual, badValue)
			}
			checkMatrixError(t, err, want)
			if hook.calls() != calls {
				t.Fatal("invalid secondary argument reached hook")
			}
			if calls == 1 && hook.retained[1].(*Value) != nil {
				t.Fatal("nil fact value changed")
			}
		})
	}
}

func TestPublicationBoundaryHookResource(t *testing.T) {
	var previous uint64
	for _, count := range []int{16, 32, 64} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			hook := boundaryValidator()
			r, err := NewRegistry(hook)
			if err != nil {
				t.Fatal(err)
			}
			c := publicationCapability("asset:a", "capability:a", "service:a", "binding:a", "epoch:a", "1")
			for i := 0; i < count; i++ {
				v := Value{Kind: ValueSymbols, Symbols: []Symbol{}}
				for j := 0; j < 64; j++ {
					v.Symbols = append(v.Symbols, Symbol{Namespace: "native.test", Token: fmt.Sprintf("s%02d", j)})
				}
				c.Constraints = append(c.Constraints, TypedField{ID: DefinitionID(fmt.Sprintf("field.f%02d", i)), Value: v})
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			err = r.ValidateCapability(c)
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("fields=%d symbols/field=64 allocation_bytes=%d", count, allocated)
			if allocated > 16<<20 || previous != 0 && allocated > 3*previous+1<<20 {
				t.Fatal("unbounded hook isolation")
			}
			previous = allocated
			for _, arg := range hook.retained {
				mutateBoundaryAliases(reflect.ValueOf(arg))
			}
			if err := c.Validate(); err != nil {
				t.Fatal("retained max-size alias", err)
			}
		})
	}
}
