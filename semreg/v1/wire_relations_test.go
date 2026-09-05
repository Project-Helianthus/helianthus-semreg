package semreg

import (
	"encoding/json"
	"testing"
)

func checkWireTimeRelation[T Record](t *testing.T, name string, valid, invalid T, sibling string) {
	t.Helper()
	for _, token := range incompatibleStringTokens {
		t.Run(name+"/"+token, func(t *testing.T) {
			checkMatrixError(t, invalid.Validate(), InvalidTime)
			for _, tc := range []struct {
				v      T
				mutate bool
				want   ErrorID
			}{{valid, false, ""}, {invalid, false, InvalidTime}, {valid, true, InvalidValue}, {invalid, true, InvalidValue}} {
				raw, _ := json.Marshal(tc.v)
				if tc.mutate {
					raw = mutateWire(t, raw, sibling, token)
				}
				for _, input := range [][]byte{raw, reverseMembers(t, raw)} {
					_, err := Decode[T](input)
					checkMatrixError(t, err, tc.want)
				}
			}
		})
	}
}

func TestWireEnclosingTimeRelations(t *testing.T) {
	policy := validPolicy()
	zero := policy
	zero.FreshForNS = "0"
	reversed := policy
	reversed.RetainForNS = "20"
	checkWireTimeRelation(t, "freshness-zero", policy, zero, "policy_id")
	checkWireTimeRelation(t, "freshness-retain-order", policy, reversed, "policy_id")
	times := validTimes()
	backward := times
	backward.EvaluateMonotonic.Nanoseconds = "4"
	checkWireTimeRelation(t, "monotonic-backward", times, backward, "received_at.clock_id")
}
