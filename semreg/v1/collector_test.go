package semreg

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCollectorRegression(t *testing.T) {
	t.Run("quality-independent-token", func(t *testing.T) {
		for _, raw := range []string{
			`{"assertion":23,"qualification":"qualified","promotion":"promoted","validity":"good","availability":"available","freshness":"fresh","reasons":["reason.a","reason.a"]}`,
			`{"reasons":["reason.a","reason.a"],"freshness":"fresh","availability":"available","validity":"good","promotion":"promoted","qualification":"qualified","assertion":23}`,
		} {
			_, err := Decode[Quality]([]byte(raw))
			requireID(t, err, DuplicateKey)
			_, err = Decode[Quality]([]byte(strings.Replace(raw, `23`, `"observed"`, 1)))
			requireID(t, err, DuplicateKey)
			_, err = Decode[Quality]([]byte(strings.Replace(raw, `"reason.a","reason.a"`, `"reason.a","reason.b"`, 1)))
			requireID(t, err, InvalidValue)
		}
	})
	t.Run("dimension-key-presence", func(t *testing.T) {
		for _, dims := range []string{
			`[{"value":{"kind":"boolean","boolean":true}},{"value":{"kind":"boolean","boolean":false}}]`,
			`[{"value":{"boolean":false,"kind":"boolean"}},{"value":{"boolean":true,"kind":"boolean"}}]`,
		} {
			_, err := Decode[FactKey]([]byte(`{"pack_id":"pack.test","pack_version":"1.0.0","fact_id":"fact.test","dimensions":` + dims + `}`))
			requireID(t, err, MissingMember)
		}
		for _, tc := range []struct {
			a, b string
			want ErrorID
		}{
			{"dimension.a", "dimension.a", DuplicateKey}, {"dimension.b", "dimension.a", NoncanonicalOrder},
			{"dimension.a", "dimension.b", ""}, {"!", "!", InvalidIdentifier},
		} {
			key := validKey()
			key.Dimensions = []Dimension{{ID: DefinitionID(tc.a), Value: Value{Kind: ValueBoolean, Boolean: boolPtr(true)}}, {ID: DefinitionID(tc.b), Value: Value{Kind: ValueBoolean, Boolean: boolPtr(false)}}}
			raw, _ := json.Marshal(key)
			_, err := Decode[FactKey](raw)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireID(t, err, tc.want)
			}
		}
	})
	t.Run("registry-final-partition", func(t *testing.T) {
		pack := PackRef{ID: "pack.test", Version: "1.0.0"}
		good := DefinitionRef{Pack: pack, ID: "field.good", Version: "1.0.0"}
		bad := DefinitionRef{Pack: pack, ID: "!", Version: "1.0.0"}
		for _, refs := range [][]DefinitionRef{{good, good, bad}, {bad, good, good}, {good, good}, {good}} {
			index := indexWith(pack, DefinitionField, good)
			index.Fields = refs
			r, err := NewRegistry(&countingValidator{pack: pack, index: index})
			if len(refs) == 1 {
				if err != nil {
					t.Fatal(err)
				}
				continue
			}
			want := DefinitionOwnerConflict
			if len(refs) == 3 {
				want = InvalidIdentifier
			}
			requireID(t, err, want)
			if r != nil {
				t.Fatal("rejected registry returned")
			}
		}
	})
	t.Run("causal-integral-domain", func(t *testing.T) {
		raw, _ := json.Marshal(validCausal())
		var members map[string]json.RawMessage
		if err := json.Unmarshal(raw, &members); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"hop_count", "max_hops"} {
			original := members[field]
			for _, token := range []string{"65536", "999999999999999999999999999999999999", "-1", "1.5", "1e5", `"65536"`, "null"} {
				members[field] = json.RawMessage(token)
				wire, _ := json.Marshal(members)
				want := CausalBudgetExceeded
				if token == "1.5" || token == "1e5" || token == `"65536"` {
					want = InvalidValue
				}
				if token == "null" {
					want = MissingMember
				}
				for _, input := range [][]byte{wire, reverseMembers(t, wire)} {
					_, err := Decode[CausalContext](input)
					requireID(t, err, want)
				}
			}
			members[field] = original
		}
		assertRoundTrip(t, validCausal())
	})
	t.Run("null-value-and-pointer", func(t *testing.T) {
		_, err := Decode[Decimal]([]byte("null"))
		requireID(t, err, MissingMember)
		_, err = Decode[*Decimal]([]byte("null"))
		requireID(t, err, MissingMember)
		for _, raw := range []string{`{"coefficient":"23","exponent10":0}`, `{"exponent10":0,"coefficient":"23"}`} {
			v, err := Decode[*Decimal]([]byte(raw))
			if err != nil || v == nil || v.Coefficient != "23" {
				t.Fatalf("pointer control: %v %v", v, err)
			}
			_, err = Decode[Decimal]([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestDiscriminatorMetadataRejectsAmbiguousJSONTag(t *testing.T) {
	contractType := reflect.TypeOf(ContractVersion(""))
	child := reflect.StructOf([]reflect.StructField{
		{Name: "First", Type: contractType, Tag: `json:"contract"`},
		{Name: "Second", Type: contractType, Tag: `json:"contract"`},
	})
	if validDiscriminatorMetadata(child, "contract", ContractKernelV1) {
		t.Fatal("ambiguous JSON discriminator metadata accepted")
	}
}

func boolPtr(v bool) *bool { return &v }

func TestCollectorIndependentKeysAndDomains(t *testing.T) {
	decode := func(raw []byte, target string) error {
		var err error
		switch target {
		case "key":
			_, err = Decode[FactKey](raw)
		case "origin":
			_, err = Decode[OriginRef](raw)
		case "capability":
			_, err = Decode[CapabilityInstance](raw)
		case "envelope":
			_, err = Decode[FactEnvelope](raw)
		case "evidence":
			_, err = Decode[EvidenceRef](raw)
		case "candidate":
			_, err = Decode[FactCandidate](raw)
		}
		return err
	}
	key := validKey()
	key.Dimensions = []Dimension{{ID: "dimension.a", Value: Value{Kind: ValueBoolean, Boolean: boolPtr(true)}}, {ID: "dimension.a", Value: Value{Kind: ValueBoolean, Boolean: boolPtr(false)}}}
	origin := validOrigin("a")
	origin.Evidence = append(origin.Evidence, origin.Evidence[0])
	capability := validCapability()
	capability.Constraints = []TypedField{{ID: "field.a", Value: Value{Kind: ValueBoolean, Boolean: boolPtr(true)}}, {ID: "field.a", Value: Value{Kind: ValueBoolean, Boolean: boolPtr(false)}}}
	envelope := conflictingEnvelope(t)
	envelope.Candidates = []FactCandidate{envelope.Candidates[0], envelope.Candidates[0]}
	for _, tc := range []struct {
		name   string
		record Record
		field  string
	}{
		{"key", key, "pack_id"}, {"origin", origin, "origin_id"}, {"capability", capability, "instance_id"}, {"envelope", envelope, "revision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.record)
			var members map[string]json.RawMessage
			if err := json.Unmarshal(raw, &members); err != nil {
				t.Fatal(err)
			}
			members[tc.field] = json.RawMessage(`23`)
			raw, _ = json.Marshal(members)
			for _, input := range [][]byte{raw, reverseMembers(t, raw)} {
				requireID(t, decode(input, tc.name), DuplicateKey)
			}
		})
	}
	t.Run("incomplete-payload-retains-present-keys", func(t *testing.T) {
		for _, payload := range []string{`{}`, `{"kind":23}`, `null`} {
			raw := []byte(`{"pack_id":"pack.test","pack_version":"1.0.0","fact_id":"fact.test","dimensions":[{"id":"dimension.a","value":` + payload + `},{"id":"dimension.a","value":` + payload + `}]}`)
			requireID(t, decode(raw, "key"), DuplicateKey)
		}
	})
	t.Run("evidence-final-domain-before-ranking", func(t *testing.T) {
		raw, _ := json.Marshal(origin.Evidence[0])
		var members map[string]json.RawMessage
		if err := json.Unmarshal(raw, &members); err != nil {
			t.Fatal(err)
		}
		members["contract"], members["access"] = json.RawMessage(`"!"`), json.RawMessage(`23`)
		raw, _ = json.Marshal(members)
		requireID(t, decode(raw, "evidence"), InvalidValue)
	})
	t.Run("conditional-member-before-unrelated-token", func(t *testing.T) {
		raw, _ := json.Marshal(validCandidate("a", true))
		var members map[string]json.RawMessage
		if err := json.Unmarshal(raw, &members); err != nil {
			t.Fatal(err)
		}
		delete(members, "binding_id")
		members["revision"] = json.RawMessage(`23`)
		raw, _ = json.Marshal(members)
		requireID(t, decode(raw, "candidate"), MissingMember)
	})
}

func reverseMembers(t *testing.T, raw []byte) []byte {
	t.Helper()
	node, _, err := parseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for i := len(node.object) - 1; i >= 0; i-- {
		key, _ := json.Marshal(node.object[i].key)
		value, _ := json.Marshal(jsonNodeValue(node.object[i].value))
		parts = append(parts, string(key)+":"+string(value))
	}
	return []byte("{" + strings.Join(parts, ",") + "}")
}
