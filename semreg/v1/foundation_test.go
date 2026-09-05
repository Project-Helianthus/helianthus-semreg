package semreg

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These fixtures copy the applicable fields from
// helianthus-docs-semantic@b16667d719defc7b0fef0400ee3ad387469018ac.
// Scenario shorthand is expanded below into visible complete typed records.
func TestPinnedFoundationVectors(t *testing.T) {
	vectors := loadFoundationVectors(t)
	t.Run("K-POS-001", func(t *testing.T) {
		raw := vectorJSON(t, vectors, "K-POS-001")
		record, err := DecodeDecimal(raw)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := CanonicalJSON(record)
		if err != nil || string(canonical) != string(raw) {
			t.Fatalf("canonical=%s err=%v", canonical, err)
		}
	})
	t.Run("K-POS-002", func(t *testing.T) {
		raw := vectorJSON(t, vectors, "K-POS-002")
		link, err := Decode[IdentityLink](raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(link.Basis) != 1 || link.State != LinkQualified {
			t.Fatal("qualified link did not retain its basis")
		}
	})
	t.Run("K-POS-003", func(t *testing.T) {
		candidate := validInferredCandidate()
		if err := candidate.Validate(); err != nil {
			t.Fatal(err)
		}
		if len(candidate.Derivation.Inputs) != 3 || candidate.Quality.Promotion != PromotionPromoted || len(candidate.Evidence) != 1 {
			t.Fatal("lineage or promotion was not retained")
		}
	})
	t.Run("K-POS-004", func(t *testing.T) {
		raw := vectorJSON(t, vectors, "K-POS-004")
		times, err := Decode[Times](raw)
		if err != nil {
			t.Fatal(err)
		}
		elapsed, err := times.ElapsedMonotonicNS()
		if err != nil || elapsed != "1000" {
			t.Fatalf("elapsed=%s err=%v", elapsed, err)
		}
	})
	t.Run("K-POS-005", func(t *testing.T) {
		t.Skip("restore and policy-aware freshness evaluation are deferred; no substitute assertion is reported")
	})
	t.Run("K-POS-006", func(t *testing.T) {
		raw := vectorJSON(t, vectors, "K-POS-006")
		symbol, err := Decode[Symbol](raw)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := CanonicalJSON(symbol)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip Symbol
		if err := json.Unmarshal(canonical, &roundTrip); err != nil || roundTrip != symbol {
			t.Fatalf("round trip=%+v err=%v", roundTrip, err)
		}
		if symbol.Known {
			t.Fatal("unknown native symbol became known")
		}
	})
	t.Run("K-POS-007", func(t *testing.T) {
		envelope := conflictingEnvelope(t)
		if err := envelope.Validate(); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Candidates) != 2 || len(envelope.Conflicts) != 1 || len(envelope.Conflicts[0].Evidence) != 2 {
			t.Fatal("derived conflict is incomplete")
		}
	})
	t.Run("K-POS-008", func(t *testing.T) {
		capability := validCapability()
		if capability.Definition.Version != "1.2.0" || capability.DriverGeneration != "7" {
			t.Fatal("pinned version or generation changed")
		}
		if err := capability.Validate(); err != nil {
			t.Fatal(err)
		}
		rangeV := VersionRange{Minimum: "1.0.0", MaximumExclusive: "2.0.0"}
		matched, err := rangeV.Matches(capability.Definition.Version)
		if err != nil || !matched {
			t.Fatalf("match=%v err=%v", matched, err)
		}
	})
	t.Run("K-POS-022", testExactRegistryDispatch)
	t.Run("K-NEG-002", func(t *testing.T) {
		binding := validBinding()
		binding.AssetID = " bus address 03 "
		requireID(t, binding.Validate(), InvalidIdentifier)
	})
	t.Run("K-NEG-003", func(t *testing.T) {
		_, err := Decode[Decimal](vectorJSON(t, vectors, "K-NEG-003"))
		requireID(t, err, InvalidDecimal)
	})
	t.Run("K-NEG-004", func(t *testing.T) {
		_, err := Decode[Value](vectorJSON(t, vectors, "K-NEG-004"))
		requireID(t, err, InvalidValue)
	})
	t.Run("K-NEG-005", func(t *testing.T) {
		p := validPolicy()
		p.FreshForNS = "300"
		p.RetainForNS = "300"
		requireID(t, p.Validate(), InvalidTime)
	})
	t.Run("K-NEG-006", func(t *testing.T) {
		t.Skip("the vector performs an incomparable-epoch elapsed operation; structural Times validation intentionally does not subtract epochs")
	})
	t.Run("K-NEG-007", func(t *testing.T) {
		_, err := Decode[EvidenceRef](vectorJSON(t, vectors, "K-NEG-007"))
		requireID(t, err, InvalidEvidence)
	})
	t.Run("K-NEG-008", func(t *testing.T) {
		t.Skip("two-node snapshot derivation-graph resolution is deferred; local self-reference remains covered below")
	})
	t.Run("K-NEG-009", func(t *testing.T) {
		requireID(t, (IdentityLink{AssetID: "asset:01", BindingID: "binding:02", State: LinkQualified, Basis: []EvidenceRef{}, Revision: "1"}).Validate(), IdentityNotQualified)
	})
	t.Run("K-NEG-014", func(t *testing.T) {
		t.Skip("capability admission runtime is deferred; syntax and exact owner validation are covered separately")
	})
	t.Run("K-NEG-015", func(t *testing.T) { t.Skip("degraded capability admission policy is deferred") })
	t.Run("K-NEG-027", func(t *testing.T) {
		_, err := DecodeDecimal(vectorJSON(t, vectors, "K-NEG-027"))
		requireID(t, err, DuplicateKey)
	})
	t.Run("K-NEG-028", func(t *testing.T) {
		_, err := DecodeDecimal(vectorJSON(t, vectors, "K-NEG-028"))
		requireID(t, err, UnknownMember)
	})
	t.Run("K-NEG-029", func(t *testing.T) {
		_, err := DecodeDecimal(vectorJSON(t, vectors, "K-NEG-029"))
		requireID(t, err, InvalidJSON)
	})
	t.Run("K-NEG-030", func(t *testing.T) {
		_, err := DecodeDecimal(vectorJSON(t, vectors, "K-NEG-030"))
		requireID(t, err, MissingMember)
	})
	t.Run("K-NEG-031", func(t *testing.T) {
		_, err := Decode[Quality](vectorJSON(t, vectors, "K-NEG-031"))
		requireID(t, err, InvalidEnum)
	})
	t.Run("K-NEG-032", func(t *testing.T) { f := validEnvelopeWithCount(t, 33); requireID(t, f.Validate(), BoundsExceeded) })
	t.Run("K-NEG-033", func(t *testing.T) {
		d := validInferredCandidate().Derivation
		d.Inputs[0], d.Inputs[1] = d.Inputs[1], d.Inputs[0]
		requireID(t, d.Validate(), NoncanonicalOrder)
	})
	t.Run("K-NEG-043", func(t *testing.T) {
		p1 := PackRef{ID: "helianthus.pack.alpha", Version: "1.0.0"}
		p2 := PackRef{ID: "helianthus.pack.beta", Version: "1.0.0"}
		a := DefinitionRef{Pack: p1, ID: "helianthus.operation.shared", Version: "1.0.0"}
		b := DefinitionRef{Pack: p2, ID: a.ID, Version: a.Version}
		_, err := NewRegistry(&countingValidator{pack: p1, index: indexWith(p1, DefinitionOperation, a)}, &countingValidator{pack: p2, index: indexWith(p2, DefinitionOperation, b)})
		requireID(t, err, DefinitionOwnerConflict)
	})
	t.Run("K-NEG-047", func(t *testing.T) {
		requireID(t, (IdentityLink{AssetID: "asset:01", BindingID: "binding:01", State: LinkQualified, Basis: []EvidenceRef{}, Revision: "1"}).Validate(), IdentityNotQualified)
	})
	t.Run("K-NEG-048", func(t *testing.T) {
		t.Skip("capability admission is deferred; the empty activation-proof validation partition is covered outside the vector namespace")
	})
	t.Run("K-NEG-054", func(t *testing.T) {
		f := conflictingEnvelope(t)
		f.Conflicts[0].Evidence = f.Conflicts[0].Evidence[:1]
		requireID(t, f.Validate(), InvalidValue)
	})
}

func TestAuditStrictRecursiveDecoding(t *testing.T) {
	t.Run("null-required-and-optional", func(t *testing.T) {
		for _, raw := range []string{`{"coefficient":"23","exponent10":null}`, `{"coefficient":null,"exponent10":0}`, `{"kind":"boolean","boolean":true,"text":null}`} {
			if strings.Contains(raw, `"kind"`) {
				_, err := Decode[Value]([]byte(raw))
				requireID(t, err, MissingMember)
			} else {
				_, err := DecodeDecimal([]byte(raw))
				requireID(t, err, MissingMember)
			}
		}
	})
	t.Run("duplicate-unknown-trailing-precedence", func(t *testing.T) {
		_, err := DecodeDecimal([]byte(`{"coefficient":"23","coefficient":"24","exponent10":1} trailing`))
		requireID(t, err, InvalidJSON)
		_, err = Decode[Value]([]byte(`{"kind":"boolean","boolean":false,"boolean":true}`))
		requireID(t, err, DuplicateKey)
		_, err = Decode[Quantity]([]byte(`{"number":{"coefficient":"23","exponent10":0,"x":{"a":1,"a":2}},"unit":"unit.volt"}`))
		requireID(t, err, DuplicateKey)
	})
	t.Run("wrong-tokens-and-malformed-utf8", func(t *testing.T) {
		_, err := DecodeDecimal([]byte(`{"coefficient":23,"exponent10":0}`))
		requireID(t, err, InvalidValue)
		raw := append([]byte(`{"kind":"text","text":"`), 0xff)
		raw = append(raw, []byte(`"}`)...)
		_, err = Decode[Value](raw)
		requireID(t, err, InvalidJSON)
	})
	t.Run("required-empty-array-presence", func(t *testing.T) {
		_, err := Decode[Quality]([]byte(`{"assertion":"observed","qualification":"qualified","promotion":"promoted","validity":"good","availability":"available","freshness":"fresh"}`))
		requireID(t, err, MissingMember)
	})
	t.Run("complete-record-round-trips", func(t *testing.T) {
		assertRoundTrip(t, ContractKernelV1)
		assertRoundTrip(t, DefinitionID("definition.test"))
		assertRoundTrip(t, OpaqueID("opaque:test"))
		assertRoundTrip(t, SemanticVersion("1.10.0"))
		assertRoundTrip(t, VersionLabel("release-1"))
		assertRoundTrip(t, Uint64("18446744073709551615"))
		assertRoundTrip(t, Int64("-9223372036854775808"))
		assertRoundTrip(t, Digest("sha256:"+strings.Repeat("a", 64)))
		assertRoundTrip(t, InvalidValue)
		assertRoundTrip(t, VersionRange{Minimum: "1.0.0", MaximumExclusive: "2.0.0"})
		assertRoundTrip(t, validSource())
		assertRoundTrip(t, validOrigin("a"))
		assertRoundTrip(t, validPath("a", "1"))
		assertRoundTrip(t, validInferredCandidate().Derivation.Inputs[0])
		assertRoundTrip(t, Decimal{Coefficient: "23", Exponent10: 1})
		assertRoundTrip(t, Symbol{Namespace: "native.test", Token: "x", Known: false})
		text := "x"
		assertRoundTrip(t, Value{Kind: ValueText, Text: &text})
		assertRoundTrip(t, Quantity{Number: Decimal{Coefficient: "23", Exponent10: 1}, Unit: "unit.volt"})
		assertRoundTrip(t, Dimension{ID: "dimension.phase", Value: Value{Kind: ValueText, Text: &text}})
		assertRoundTrip(t, validKey())
		assertRoundTrip(t, validTimes())
		assertRoundTrip(t, validPolicy())
		assertRoundTrip(t, validQuality())
		assertRoundTrip(t, validCandidate("a", true))
		assertRoundTrip(t, conflictingEnvelope(t).Conflicts[0])
		assertRoundTrip(t, conflictingEnvelope(t))
		assertRoundTrip(t, PackRef{ID: "pack.test", Version: "1.0.0"})
		assertRoundTrip(t, DefinitionRef{Pack: PackRef{ID: "pack.test", Version: "1.0.0"}, ID: "field.test", Version: "1.0.0"})
		assertRoundTrip(t, indexWith(PackRef{ID: "pack.test", Version: "1.0.0"}, DefinitionField, DefinitionRef{Pack: PackRef{ID: "pack.test", Version: "1.0.0"}, ID: "field.test", Version: "1.0.0"}))
		assertRoundTrip(t, TypedField{ID: "field.test", Value: Value{Kind: ValueText, Text: &text}})
		assertRoundTrip(t, PredicateEqual)
		assertRoundTrip(t, validService())
		assertRoundTrip(t, validCapability())
		assertRoundTrip(t, validCausal())
	})
}

func TestAuditJCSNFCAndBounds(t *testing.T) {
	textValue := "<>&\u2028\u2029\"\\\t\n\r"
	canonical, err := CanonicalJSON(Value{Kind: ValueText, Text: &textValue})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"kind\":\"text\",\"text\":\"<>&\u2028\u2029\\\"\\\\\\t\\n\\r\"}"
	if string(canonical) != want {
		t.Fatalf("JCS=%q want=%q", canonical, want)
	}
	digest, err := DigestRecord(Value{Kind: ValueText, Text: &textValue})
	if err != nil {
		t.Fatal(err)
	}
	if digest != "sha256:ca14559a096bbdb0b4a9151bba7a640252943a065771b5def50a593fe42773c9" {
		t.Fatalf("digest=%s", digest)
	}
	keys, err := canonicalize([]byte(`{"�":2,"😀":1}`))
	if err != nil || string(keys) != `{"😀":1,"�":2}` {
		t.Fatalf("UTF-16 key order=%s err=%v", keys, err)
	}
	for _, record := range []Record{Symbol{Namespace: "native.test", Token: "", Known: false}, Symbol{Namespace: "native.test", Token: "e\u0301", Known: false}, Value{Kind: ValueText, Text: pointer("e\u0301")}} {
		requireID(t, record.Validate(), func() ErrorID {
			if _, ok := record.(Symbol); ok && record.(Symbol).Token == "" {
				return BoundsExceeded
			}
			return InvalidValue
		}())
	}
	tooLong := strings.Repeat("x", 4097)
	requireID(t, (Value{Kind: ValueText, Text: &tooLong}).Validate(), BoundsExceeded)
	control := "bad\x01text"
	requireID(t, (Value{Kind: ValueText, Text: &control}).Validate(), InvalidValue)
}

func TestRegistryQualifiedValidationBoundary(t *testing.T) {
	pack := PackRef{ID: "pack.test", Version: "1.0.0"}
	factRef := DefinitionRef{Pack: pack, ID: "fact.test", Version: "1.0.0"}
	serviceRef := DefinitionRef{Pack: pack, ID: "service.test", Version: "1.0.0"}
	capabilityRef := DefinitionRef{Pack: pack, ID: "capability.test", Version: "1.0.0"}
	fieldRef := DefinitionRef{Pack: pack, ID: "field.test", Version: "1.0.0"}
	validator := &countingValidator{pack: pack, index: DefinitionIndex{Pack: pack, Fields: []DefinitionRef{factRef, fieldRef}, Services: []DefinitionRef{serviceRef}, Capabilities: []DefinitionRef{capabilityRef}, Operations: []DefinitionRef{}, EffectRules: []DefinitionRef{}}}
	registry, err := NewRegistry(validator)
	if err != nil {
		t.Fatal(err)
	}
	candidate := validCandidate("a", true)
	if err := registry.ValidateFactCandidate(candidate); err != nil || validator.factCalls != 1 {
		t.Fatalf("fact calls=%d err=%v", validator.factCalls, err)
	}
	service := validService()
	service.Definition = serviceRef
	if err := registry.ValidateService(service); err != nil || validator.serviceCalls != 1 {
		t.Fatalf("service calls=%d err=%v", validator.serviceCalls, err)
	}
	capability := validCapability()
	capability.Definition = capabilityRef
	if err := registry.ValidateCapability(capability); err != nil || validator.capabilityCalls != 1 {
		t.Fatalf("capability calls=%d err=%v", validator.capabilityCalls, err)
	}
	field := TypedField{ID: fieldRef.ID, Value: booleanValue(true)}
	if err := registry.ValidateField(fieldRef, field); err != nil || validator.fieldCalls != 1 {
		t.Fatalf("field calls=%d err=%v", validator.fieldCalls, err)
	}
	empty, _ := NewRegistry()
	requireID(t, empty.ValidateFactCandidate(candidate), DefinitionOwnerMissing)
	_, err = registry.Definition(DefinitionCapability, serviceRef)
	requireID(t, err, DefinitionOwnerMissing)
	wrongVersion := factRef
	wrongVersion.Version = "1.0.1"
	_, err = registry.Definition(DefinitionField, wrongVersion)
	requireID(t, err, DefinitionOwnerMissing)
	wrongPack := factRef
	wrongPack.Pack = PackRef{ID: "pack.other", Version: "1.0.0"}
	_, err = registry.Definition(DefinitionField, wrongPack)
	requireID(t, err, DefinitionOwnerMissing)
	rejecting := &countingValidator{pack: pack, index: validator.index, reject: errID(InvalidValue, "pack hook")}
	rejectRegistry, err := NewRegistry(rejecting)
	if err != nil {
		t.Fatal(err)
	}
	requireID(t, rejectRegistry.ValidateFactCandidate(candidate), InvalidValue)
	if rejecting.factCalls != 1 {
		t.Fatalf("rejecting hook calls=%d", rejecting.factCalls)
	}
}

func TestAuditFoundationInvariants(t *testing.T) {
	t.Run("dimension-kind-subset", func(t *testing.T) {
		for _, kind := range []ValueKind{ValueTime, ValueSymbols} {
			d := Dimension{ID: "dimension.test", Value: Value{Kind: kind}}
			if kind == ValueTime {
				timePoint := validTimes().ReceivedAt
				d.Value.Time = &timePoint
			} else {
				d.Value.Symbols = []Symbol{{Namespace: "native.test", Token: "x", Known: false}}
			}
			requireID(t, d.Validate(), InvalidValue)
		}
	})
	t.Run("origin-and-candidate", func(t *testing.T) {
		c := validCandidate("a", true)
		c.Origin = OriginRef{}
		requireID(t, c.Validate(), MissingMember)
		c = validCandidate("a", true)
		bad := NativeBindingID(" bad ")
		c.BindingID = &bad
		empty := SourceEpochID("")
		c.SourceEpochID = &empty
		requireID(t, c.Validate(), InvalidIdentifier)
		inferred := validInferredCandidate()
		inferred.Derivation.Inputs[0].CandidateID = inferred.CandidateID
		requireID(t, inferred.Validate(), DerivationCycle)
		c = validCandidate("a", true)
		other := SourceEpochID("epoch:other")
		c.Origin.SourceEpochID = &other
		requireID(t, c.Validate(), InvalidValue)
	})
	t.Run("causal-field", func(t *testing.T) {
		c := validCandidate("a", true)
		c.Causal = pointerRecord(validCausal())
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		canonical, err := CanonicalJSON(c)
		if err != nil || !bytes.Contains(canonical, []byte(`"causal"`)) {
			t.Fatalf("causal not retained: %s %v", canonical, err)
		}
	})
	t.Run("constraints", func(t *testing.T) {
		c := validCapability()
		c.Constraints = []TypedField{{ID: "!", Value: Value{}}}
		requireID(t, c.Validate(), InvalidIdentifier)
		c = validCapability()
		for i := 0; i < 65; i++ {
			c.Constraints = append(c.Constraints, TypedField{ID: DefinitionID("field." + letterID(i)), Value: booleanValue(true)})
		}
		requireID(t, c.Validate(), BoundsExceeded)
		c = validCapability()
		c.Constraints = []TypedField{{ID: "field.b", Value: booleanValue(true)}, {ID: "field.a", Value: booleanValue(true)}, {ID: "field.b", Value: booleanValue(true)}}
		requireID(t, c.Validate(), DuplicateKey)
	})
	t.Run("activation-proof-partition", func(t *testing.T) {
		c := validCapability()
		c.ActivationEvidence = []EvidenceRef{}
		requireID(t, c.Validate(), CapabilityNotQualified)
	})
	t.Run("conflicts", func(t *testing.T) {
		a := validCandidate("a", true)
		b := validCandidate("b", false)
		f := FactEnvelope{AssetID: "asset:a", Key: validKey(), Candidates: []FactCandidate{a, b}, Conflicts: []Conflict{}, Revision: "1"}
		requireID(t, f.Validate(), InvalidValue)
		f = conflictingEnvelope(t)
		f.Conflicts[0].ConflictID = "conflict:wrong"
		requireID(t, f.Validate(), InvalidValue)
		f = conflictingEnvelope(t)
		f.Conflicts[0].Candidates = []CandidateID{"candidate:a", "candidate:missing"}
		requireID(t, f.Validate(), InvalidValue)
	})
	t.Run("cross-epoch-structure", func(t *testing.T) {
		times := validTimes()
		times.EvaluateMonotonic.ClockEpochID = "clock:restart"
		if err := times.Validate(); err != nil {
			t.Fatal(err)
		}
		_, err := times.ElapsedMonotonicNS()
		requireID(t, err, IncomparableClockEpoch)
	})
	t.Run("numeric-ordering", func(t *testing.T) {
		pack := PackRef{ID: "pack.test", Version: "1.0.0"}
		idx := DefinitionIndex{Pack: pack, Fields: []DefinitionRef{{Pack: pack, ID: "field.test", Version: "1.2.0"}, {Pack: pack, ID: "field.test", Version: "1.10.0"}}, Services: []DefinitionRef{}, Capabilities: []DefinitionRef{}, Operations: []DefinitionRef{}, EffectRules: []DefinitionRef{}}
		if err := idx.Validate(); err != nil {
			t.Fatal(err)
		}
		d := validInferredCandidate().Derivation
		d.Inputs = d.Inputs[:1]
		d.Inputs[0].SourcePaths = []SourcePathRef{validPath("a", "2"), validPath("b", "10")}
		if err := d.Validate(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("stable-error-precedence", func(t *testing.T) {
		q := validQuality()
		q.Reasons = []DefinitionID{"reason.b", "reason.a", "reason.b"}
		requireID(t, q.Validate(), DuplicateKey)
		q = validQuality()
		q.Reasons = []DefinitionID{"!"}
		q.Assertion = "invalid"
		requireID(t, q.Validate(), InvalidIdentifier)
		value := Value{Kind: ValueQuantity, Quantity: &Quantity{Unit: "unit.volt", Number: Decimal{Coefficient: "230", Exponent10: 0}}}
		requireID(t, value.Validate(), InvalidDecimal)
	})
	t.Run("typed-nil", func(t *testing.T) {
		var decimal *Decimal
		_, err := CanonicalJSON(decimal)
		requireID(t, err, InvalidValue)
	})
}

func requireID(t *testing.T, err error, want ErrorID) {
	t.Helper()
	if err == nil || ErrorIdentifier(err) != want {
		t.Fatalf("got %v (%s), want %s", err, ErrorIdentifier(err), want)
	}
}

type fixtureVector struct {
	ID        string          `json:"id"`
	Operation string          `json:"operation"`
	Input     json.RawMessage `json:"input"`
	InputJSON string          `json:"input_json"`
	Expect    struct {
		Result        string `json:"result"`
		ErrorID       string `json:"error_id"`
		CanonicalJSON string `json:"canonical_json"`
	} `json:"expect"`
}

func loadFoundationVectors(t *testing.T) map[string]fixtureVector {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve fixture path")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "fixtures", "v1", "foundation-vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, duplicate, err := parseJSON(raw); err != nil || duplicate {
		t.Fatalf("fixture JSON is not strict: duplicate=%v err=%v", duplicate, err)
	}
	var document struct {
		Contract ContractVersion `json:"contract"`
		Source   struct {
			Repository string `json:"repository"`
			Commit     string `json:"commit"`
			Path       string `json:"path"`
		} `json:"source"`
		Vectors []fixtureVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.Contract != "helianthus.semantic.kernel.acceptance/v1" || document.Source.Repository != "Project-Helianthus/helianthus-docs-semantic" || document.Source.Commit != "b16667d719defc7b0fef0400ee3ad387469018ac" || document.Source.Path != "api/v1/acceptance-vectors.json" || len(document.Vectors) != 30 {
		t.Fatalf("unexpected fixture manifest: contract=%s source=%+v vectors=%d", document.Contract, document.Source, len(document.Vectors))
	}
	result := make(map[string]fixtureVector, len(document.Vectors))
	for _, vector := range document.Vectors {
		if _, exists := result[vector.ID]; exists {
			t.Fatalf("duplicate vector %s", vector.ID)
		}
		result[vector.ID] = vector
	}
	return result
}

func vectorJSON(t *testing.T, vectors map[string]fixtureVector, id string) []byte {
	t.Helper()
	vector, ok := vectors[id]
	if !ok {
		t.Fatalf("missing pinned vector %s", id)
	}
	if vector.InputJSON != "" {
		return []byte(vector.InputJSON)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, vector.Input); err != nil {
		t.Fatalf("%s input: %v", id, err)
	}
	return compact.Bytes()
}
func pointer(s string) *string    { return &s }
func pointerRecord[T any](v T) *T { return &v }
func booleanValue(v bool) Value   { return Value{Kind: ValueBoolean, Boolean: &v} }
func publicEvidence(ch string) EvidenceRef {
	return EvidenceRef{Owner: "owner.test", Kind: "evidence.test", Digest: Digest("sha256:" + strings.Repeat(ch, 64)[:64]), Contract: "test.contract/v1", Access: EvidenceAccessPublic, Redaction: RedactionNone}
}
func validKey() FactKey {
	return FactKey{PackID: "pack.test", PackVersion: "1.0.0", FactID: "fact.test", Dimensions: []Dimension{}}
}
func validTimes() Times {
	return Times{ReceivedAt: TimePoint{UnixNanoseconds: "100", ClockID: "clock.utc", UncertaintyNS: "0"}, ReceiptMonotonic: MonotonicPoint{ClockEpochID: "clock:a", Nanoseconds: "5"}, EvaluatedAt: TimePoint{UnixNanoseconds: "101", ClockID: "clock.utc", UncertaintyNS: "0"}, EvaluateMonotonic: MonotonicPoint{ClockEpochID: "clock:a", Nanoseconds: "6"}}
}
func validPolicy() FreshnessPolicy {
	return FreshnessPolicy{PolicyID: "policy:a", Version: "1.0.0", FreshForNS: "30", RetainForNS: "300", MaxWallUncertaintyNS: "1"}
}
func validQuality() Quality {
	return Quality{Assertion: AssertionObserved, Qualification: QualificationQualified, Promotion: PromotionPromoted, Validity: ValidityGood, Availability: AvailabilityAvailable, Freshness: FreshnessFresh, Reasons: []DefinitionID{}}
}
func validOrigin(id string) OriginRef {
	source := SourceID("source:" + id)
	epoch := SourceEpochID("epoch:" + id)
	binding := NativeBindingID("binding:" + id)
	return OriginRef{OriginID: OriginID("origin:" + id), Kind: OriginNativeObservation, SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []EvidenceRef{publicEvidence("1")}}
}
func validPath(id, generation string) SourcePathRef {
	return SourcePathRef{BindingID: NativeBindingID("binding:" + id), SourceID: SourceID("source:" + id), SourceEpochID: SourceEpochID("epoch:" + id), DriverGeneration: Uint64(generation)}
}
func validCandidate(id string, value bool) FactCandidate {
	binding := NativeBindingID("binding:" + id)
	epoch := SourceEpochID("epoch:" + id)
	generation := Uint64("1")
	return FactCandidate{CandidateID: CandidateID("candidate:" + id), Key: validKey(), Value: pointerRecord(booleanValue(value)), Quality: validQuality(), Times: validTimes(), FreshnessPolicy: validPolicy(), BindingID: &binding, SourceEpochID: &epoch, DriverGeneration: &generation, Origin: validOrigin(id), Evidence: []EvidenceRef{publicEvidence(map[bool]string{true: "1", false: "2"}[value])}, Revision: "1"}
}
func validInferredCandidate() FactCandidate {
	quality := validQuality()
	quality.Assertion = AssertionInferred
	evidence := publicEvidence("2")
	inputs := []DerivationInput{{CandidateID: "candidate:phase:l1", CandidateRevision: "4", SourcePaths: []SourcePathRef{{BindingID: "binding:meter:l1", SourceID: "source:meter:01", SourceEpochID: "source-epoch:meter:01", DriverGeneration: "3"}}}, {CandidateID: "candidate:phase:l2", CandidateRevision: "4", SourcePaths: []SourcePathRef{{BindingID: "binding:meter:l2", SourceID: "source:meter:01", SourceEpochID: "source-epoch:meter:01", DriverGeneration: "3"}}}, {CandidateID: "candidate:phase:l3", CandidateRevision: "4", SourcePaths: []SourcePathRef{{BindingID: "binding:meter:l3", SourceID: "source:meter:01", SourceEpochID: "source-epoch:meter:01", DriverGeneration: "3"}}}}
	origin := OriginRef{OriginID: "origin:derived:power-total", Kind: OriginDerived, Evidence: []EvidenceRef{evidence}}
	return FactCandidate{CandidateID: "candidate:derived:power-total", Key: validKey(), Value: pointerRecord(booleanValue(true)), Quality: quality, Times: validTimes(), FreshnessPolicy: validPolicy(), Origin: origin, Evidence: []EvidenceRef{evidence}, Derivation: &Derivation{Algorithm: "helianthus.sum.phases", Version: "1.0.0", Inputs: inputs, Evidence: []EvidenceRef{}}, Revision: "1"}
}
func validBinding() NativeBinding {
	return NativeBinding{BindingID: "binding:01", AssetID: "asset:01", SourceID: "source:01", SourceEpochID: "epoch:01", DriverGeneration: "1", NativeResource: publicEvidence("1"), State: BindingCurrent, Revision: "1"}
}
func validSource() SourceDescriptor {
	return SourceDescriptor{SourceID: "source:01", SourceEpochID: "epoch:01", ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "release-1", RegistryEvidence: publicEvidence("1"), StartedAt: validTimes().ReceivedAt, State: SourceCurrent, Revision: "1"}
}
func validService() ServiceInstance {
	return ServiceInstance{InstanceID: "service:a", AssetID: "asset:a", Definition: DefinitionRef{Pack: PackRef{ID: "pack.test", Version: "1.0.0"}, ID: "service.test", Version: "1.0.0"}, BindingID: "binding:a", SourceEpochID: "epoch:a", DriverGeneration: "1", Qualification: QualificationQualified, Availability: AvailabilityAvailable, Revision: "1"}
}
func validCapability() CapabilityInstance {
	return CapabilityInstance{InstanceID: "capability:limit:01", AssetID: "asset:a", ServiceInstance: "service:a", Definition: DefinitionRef{Pack: PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}, ID: "helianthus.evse.current_limit", Version: "1.2.0"}, BindingID: "binding:evse:01", SourceEpochID: "source-epoch:evse:01", DriverGeneration: "7", Qualification: QualificationQualified, Availability: AvailabilityAvailable, Constraints: []TypedField{}, ActivationEvidence: []EvidenceRef{{Owner: "helianthus.gateway", Kind: "driver.activation", Digest: "sha256:3333333333333333333333333333333333333333333333333333333333333333", Contract: "helianthus.gateway.driver.activation.v1", Access: EvidenceAccessPublic, Redaction: RedactionMetadataOnly}}, Revision: "1"}
}
func validCausal() CausalContext {
	return CausalContext{Origin: validOrigin("a"), CorrelationID: "correlation:a", HopCount: 1, MaxHops: 8, FirstSeenAt: TimePoint{UnixNanoseconds: "0", ClockID: "clock.utc", UncertaintyNS: "0"}, ExpiresAt: TimePoint{UnixNanoseconds: "300000000000", ClockID: "clock.utc", UncertaintyNS: "0"}, Path: []TargetID{"target:a"}}
}
func conflictingEnvelope(t *testing.T) FactEnvelope {
	t.Helper()
	a := validCandidate("a", true)
	b := validCandidate("b", false)
	candidates := []FactCandidate{a, b}
	conflicts, err := deriveConflicts("asset:a", validKey(), candidates)
	if err != nil {
		t.Fatal(err)
	}
	return FactEnvelope{AssetID: "asset:a", Key: validKey(), Candidates: candidates, Conflicts: conflicts, Revision: "1"}
}
func validEnvelopeWithCount(t *testing.T, count int) FactEnvelope {
	t.Helper()
	candidates := make([]FactCandidate, count)
	for i := 0; i < count; i++ {
		id := letterID(i)
		candidates[i] = validCandidate(id, true)
	}
	return FactEnvelope{AssetID: "asset:a", Key: validKey(), Candidates: candidates, Conflicts: []Conflict{}, Revision: "1"}
}
func letterID(i int) string { return strings.Repeat("a", i/26+1) + string(rune('a'+i%26)) }
func assertRoundTrip[T Record](t *testing.T, record T) {
	t.Helper()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode[T](raw)
	if err != nil {
		t.Fatalf("%T decode: %v; raw=%s", record, err, raw)
	}
	left, err := CanonicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	right, err := CanonicalJSON(decoded)
	if err != nil || !bytes.Equal(left, right) {
		t.Fatalf("%T round trip: %s %s %v", record, left, right, err)
	}
}

type countingValidator struct {
	pack                                                 PackRef
	index                                                DefinitionIndex
	factCalls, serviceCalls, capabilityCalls, fieldCalls int
	reject                                               error
}

func (v *countingValidator) Pack() PackRef                         { return v.pack }
func (v *countingValidator) Definitions() DefinitionIndex          { return v.index }
func (v *countingValidator) ValidateFact(FactKey, *Value) error    { v.factCalls++; return v.reject }
func (v *countingValidator) ValidateService(ServiceInstance) error { v.serviceCalls++; return v.reject }
func (v *countingValidator) ValidateCapability(CapabilityInstance) error {
	v.capabilityCalls++
	return v.reject
}
func (v *countingValidator) ValidateField(DefinitionRef, TypedField) error {
	v.fieldCalls++
	return v.reject
}
func (v *countingValidator) MatchConstraints(CapabilityInstance, []TypedField) error { return v.reject }
func (v *countingValidator) EvaluatePredicate(FactCandidate, PredicateOp, Value) (bool, error) {
	return true, v.reject
}
func indexWith(pack PackRef, kind DefinitionKind, ref DefinitionRef) DefinitionIndex {
	index := DefinitionIndex{Pack: pack, Fields: []DefinitionRef{}, Services: []DefinitionRef{}, Capabilities: []DefinitionRef{}, Operations: []DefinitionRef{}, EffectRules: []DefinitionRef{}}
	switch kind {
	case DefinitionField:
		index.Fields = []DefinitionRef{ref}
	case DefinitionService:
		index.Services = []DefinitionRef{ref}
	case DefinitionCapability:
		index.Capabilities = []DefinitionRef{ref}
	case DefinitionOperation:
		index.Operations = []DefinitionRef{ref}
	case DefinitionEffectRule:
		index.EffectRules = []DefinitionRef{ref}
	}
	return index
}
func testExactRegistryDispatch(t *testing.T) {
	thermalPack := PackRef{ID: "helianthus.pack.thermal", Version: "1.0.0"}
	evsePack := PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"}
	thermal := &countingValidator{pack: thermalPack, index: DefinitionIndex{Pack: thermalPack, Fields: []DefinitionRef{}, Services: []DefinitionRef{{Pack: thermalPack, ID: "helianthus.thermal.service.control", Version: "1.0.0"}}, Capabilities: []DefinitionRef{{Pack: thermalPack, ID: "helianthus.thermal.temperature_setpoint", Version: "1.0.0"}}, Operations: []DefinitionRef{{Pack: thermalPack, ID: "helianthus.operation.set_temperature", Version: "1.0.0"}}, EffectRules: []DefinitionRef{}}}
	evse := &countingValidator{pack: evsePack, index: DefinitionIndex{Pack: evsePack, Fields: []DefinitionRef{}, Services: []DefinitionRef{{Pack: evsePack, ID: "helianthus.evse.service.control", Version: "1.0.0"}}, Capabilities: []DefinitionRef{{Pack: evsePack, ID: "helianthus.evse.current_limit", Version: "1.0.0"}}, Operations: []DefinitionRef{{Pack: evsePack, ID: "helianthus.evse.set_current_limit", Version: "1.0.0"}}, EffectRules: []DefinitionRef{{Pack: evsePack, ID: "helianthus.evse.effect.current_limit", Version: "1.0.0"}}}}
	for _, validators := range [][]PackValidator{{thermal, evse}, {evse, thermal}} {
		registry, err := NewRegistry(validators...)
		if err != nil {
			t.Fatal(err)
		}
		for kind, ref := range map[DefinitionKind]DefinitionRef{DefinitionService: evse.index.Services[0], DefinitionCapability: evse.index.Capabilities[0], DefinitionOperation: evse.index.Operations[0], DefinitionEffectRule: evse.index.EffectRules[0]} {
			owner, err := registry.Definition(kind, ref)
			if err != nil || owner.Pack() != evsePack {
				t.Fatalf("kind=%s owner=%v err=%v", kind, owner, err)
			}
		}
	}
}
