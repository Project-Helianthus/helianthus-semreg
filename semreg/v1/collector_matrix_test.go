package semreg

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestCollectorAllocationScaling(t *testing.T) {
	var previous uint64
	for _, n := range []int{1024, 2048} {
		raw := []byte(`{"assertion":23,"qualification":"qualified","promotion":"promoted","validity":"good","availability":"available","freshness":"fresh","reasons":[` + strings.TrimSuffix(strings.Repeat(`"reason.a",`, n), ",") + `]}`)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err := Decode[Quality](raw)
		runtime.ReadMemStats(&after)
		requireID(t, err, DuplicateKey)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("reasons=%d input=%d allocated=%d", n, len(raw), allocated)
		if allocated > 32<<20 {
			t.Errorf("bounded input allocated %d bytes, limit 32 MiB", allocated)
		}
		if previous != 0 && allocated > 3*previous+(512<<10) {
			t.Errorf("doubling input grew allocation from %d to %d", previous, allocated)
		}
		previous = allocated
	}
}

func TestCollectorCombinedMutationMatrix(t *testing.T) {
	t.Run("root-contract-discriminator", func(t *testing.T) {
		_, err := Decode[ContractVersion]([]byte(`"!"`))
		checkMatrixError(t, err, InvalidContract)
		_, err = Decode[ContractVersion]([]byte(`"helianthus.semantic.kernel/v1"`))
		checkMatrixError(t, err, "")
	})
	// Cross product of independent payload and selection defects, rather than
	// testing each reported input only in isolation. Cover typed and wire paths.
	for _, kind := range []ValueKind{ValueQuantity, ValueBoolean, "invalid"} {
		for _, coefficient := range []string{"1", "10"} {
			for _, unit := range []DefinitionID{"unit.volt", "!"} {
				for _, duplicate := range []bool{false, true} {
					t.Run(fmt.Sprintf("value/%s/%s/%s/%t", kind, coefficient, unit, duplicate), func(t *testing.T) {
						v := Value{Kind: kind, Quantity: &Quantity{Number: Decimal{Coefficient: coefficient}, Unit: unit}}
						want := ErrorID("")
						if kind == ValueBoolean {
							v.Boolean = boolPtr(true)
							want = InvalidValue
						}
						if kind == "invalid" {
							want = InvalidEnum
						}
						if coefficient == "10" {
							want = InvalidDecimal
						}
						if unit == "!" {
							want = InvalidIdentifier
						}
						if duplicate {
							v.Symbols = []Symbol{{Namespace: "native.test", Token: "x"}, {Namespace: "native.test", Token: "x"}}
							want = DuplicateKey
						}
						checkMatrixError(t, v.Validate(), want)
						raw, _ := json.Marshal(v)
						for _, input := range [][]byte{raw, reverseMembers(t, raw)} {
							_, err := Decode[Value](input)
							checkMatrixError(t, err, want)
						}
					})
				}
			}
		}
	}
	for _, hop := range []string{"1", "65536", "23.5"} {
		for _, clock := range []DefinitionID{"clock.utc", "clock.other"} {
			for _, expires := range []Int64{"100", "-1"} {
				t.Run(fmt.Sprintf("causal/%s/%s/%s", hop, clock, expires), func(t *testing.T) {
					c := validCausal()
					c.HopCount = 1
					c.FirstSeenAt.UnixNanoseconds = "0"
					c.FirstSeenAt.ClockID = clock
					c.ExpiresAt.UnixNanoseconds = expires
					raw, _ := json.Marshal(c)
					raw = []byte(strings.Replace(string(raw), `"hop_count":1`, `"hop_count":`+hop, 1))
					want := ErrorID("")
					if hop == "65536" {
						want = CausalBudgetExceeded
					}
					if clock != "clock.utc" || expires == "-1" {
						want = InvalidTime
					}
					if hop == "23.5" {
						want = InvalidValue
					}
					for _, input := range [][]byte{raw, reverseMembers(t, raw)} {
						_, err := Decode[CausalContext](input)
						checkMatrixError(t, err, want)
					}
				})
			}
		}
	}
	for _, field := range []string{"owner", "kind", "digest", "contract", "access", "redaction"} {
		for _, mutation := range []string{"omit", "null", "malformed"} {
			t.Run("evidence/"+field+"/"+mutation, func(t *testing.T) {
				raw, _ := json.Marshal(validOrigin("a").Evidence[0])
				var members map[string]json.RawMessage
				if err := json.Unmarshal(raw, &members); err != nil {
					t.Fatal(err)
				}
				want := MissingMember
				switch mutation {
				case "omit":
					delete(members, field)
				case "null":
					members[field] = json.RawMessage(`null`)
				case "malformed":
					members[field] = json.RawMessage(`"!"`)
					want = InvalidEvidence
					if field == "access" || field == "redaction" {
						want = InvalidEnum
					}
				}
				raw, _ = json.Marshal(members)
				for _, input := range [][]byte{raw, reverseMembers(t, raw)} {
					_, err := Decode[EvidenceRef](input)
					checkMatrixError(t, err, want)
					origin := []byte(`{"origin_id":"origin:a","kind":"operator","evidence":[` + string(input) + `]}`)
					_, err = Decode[OriginRef](origin)
					checkMatrixError(t, err, want)
				}
			})
		}
	}
}

func checkMatrixError(t *testing.T, err error, want ErrorID) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if err == nil || ErrorIdentifier(err) != want {
		t.Errorf("got %v, want %s", err, want)
	}
}
