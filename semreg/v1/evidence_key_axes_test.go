package semreg

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEvidenceKeyAxes(t *testing.T) {
	digest := func(label string) Digest {
		return Digest(fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(label))))
	}
	base := EvidenceRef{
		Owner: "owner.synthetic.a", Kind: "evidence.synthetic.a",
		Contract: "synthetic.evidence/v1", Digest: digest("synthetic evidence a"),
		Access: EvidenceAccessPublic, Redaction: RedactionNone,
	}
	check := func(t *testing.T, first, second EvidenceRef, want ErrorID) {
		t.Helper()
		for i, ref := range []EvidenceRef{first, second} {
			if err := ref.Validate(); err != nil {
				t.Fatalf("invalid evidence fixture %d: %v", i, err)
			}
		}
		origin := OriginRef{OriginID: "origin:synthetic", Kind: OriginOperator, Evidence: []EvidenceRef{first, second}}
		assertResult := func(t *testing.T, err error) {
			t.Helper()
			if want == "" {
				if err != nil {
					t.Fatalf("distinct sorted evidence rejected: %v", err)
				}
			} else if err == nil || ErrorIdentifier(err) != want {
				t.Fatalf("got %v, want %s", err, want)
			}
		}
		t.Run("typed", func(t *testing.T) { assertResult(t, origin.Validate()) })
		t.Run("json", func(t *testing.T) {
			raw, err := json.Marshal(origin)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Decode[OriginRef](raw)
			assertResult(t, err)
		})
	}
	for _, axis := range []struct {
		name   string
		change func(*EvidenceRef)
	}{
		{"owner", func(ref *EvidenceRef) { ref.Owner = "owner.synthetic.b" }},
		{"kind", func(ref *EvidenceRef) { ref.Kind = "evidence.synthetic.b" }},
		{"contract", func(ref *EvidenceRef) { ref.Contract = "synthetic.evidence/v2" }},
		{"digest", func(ref *EvidenceRef) { ref.Digest = digest("synthetic evidence b") }},
	} {
		t.Run(axis.name, func(t *testing.T) {
			first, second := base, base
			axis.change(&second)
			// Only one field differs, so its lexical order determines the key order.
			// Keep this oracle independent of the production comparator.
			if axis.name == "digest" && strings.Compare(string(first.Digest), string(second.Digest)) > 0 {
				first, second = second, first
			}
			t.Run("distinct_sorted", func(t *testing.T) { check(t, first, second, "") })
			t.Run("distinct_reversed", func(t *testing.T) { check(t, second, first, NoncanonicalOrder) })
		})
	}
	for _, payload := range []struct {
		name   string
		change func(*EvidenceRef)
	}{
		{"access", func(ref *EvidenceRef) { ref.Access = EvidenceAccessAuthorized }},
		{"redaction", func(ref *EvidenceRef) { ref.Redaction = RedactionRedacted }},
		{"access_and_redaction", func(ref *EvidenceRef) {
			ref.Access = EvidenceAccessAuthorized
			ref.Redaction = RedactionRedacted
		}},
	} {
		t.Run("same_key_different_"+payload.name, func(t *testing.T) {
			other := base
			payload.change(&other)
			check(t, base, other, DuplicateKey)
		})
	}
}
