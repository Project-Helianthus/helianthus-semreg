package graph_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// A vector the kernel cannot satisfy because the document's own record breaks
// a contract rule is reported as a document finding with the rule it
// contradicts. The harness checks the record against the rule itself, before
// and independently of the kernel, and attributes a refusal to a finding only
// when the kernel's stable error and detail are the ones that rule names. A
// refusal no finding explains fails, and so does a record that breaks a rule
// but is accepted. No vector is selected by identifier.

// gvFinding is one contract rule a document record breaks.
type gvFinding struct {
	rule   string
	path   string
	error  semreg.ErrorID
	detail string
}

func (f gvFinding) String() string {
	return fmt.Sprintf("%s at %s (%s)", f.rule, f.path, f.error)
}

// explains reports whether the refusal err is the kernel's stable error with
// exactly the detail the finding's rule produces; an error the kernel did not mint
// explains nothing.
func (f gvFinding) explains(err error) bool {
	var stable *semreg.Error
	return errors.As(err, &stable) && stable.ID == f.error && stable.Detail == f.detail
}

const (
	gvRuleOrigin      = "kernel.md OriginRef: operator, automation and projection origins omit native source fields"
	gvRuleDefinition  = "graph.md section 7.2: a relation instance or absence statement names its definition by a DefinitionRef (pack, id, version), not by an owner-tagged catalog reference"
	gvRuleDecimal     = "serialization.md canonical decimal: a non-zero coefficient has no leading zero and does not end in zero"
	gvRuleSnapshotID  = "kernel.md Snapshot identity: the snapshot identifier digests an empty retained_observations member that the kernel wire form and every kernel acceptance vector omit"
	gvRuleAbbreviated = "kernel.md Snapshot identity: a read snapshot that omits the kernel contract identifies the abbreviated object, which is not the kernel snapshot the identifier must digest"
)

// gvRecordFindings walks one document record (a batch, a snapshot or an
// intent) and returns every rule it breaks, sorted by path.
func gvRecordFindings(t *testing.T, raw json.RawMessage) []gvFinding {
	t.Helper()
	var findings []gvFinding
	gvWalkFindings(t, gvAny(t, raw), "$", &findings)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].path != findings[j].path {
			return findings[i].path < findings[j].path
		}
		return findings[i].rule < findings[j].rule
	})
	return findings
}

func gvWalkFindings(t *testing.T, node any, path string, findings *[]gvFinding) {
	switch value := node.(type) {
	case []any:
		for i, item := range value {
			gvWalkFindings(t, item, fmt.Sprintf("%s[%d]", path, i), findings)
		}
	case map[string]any:
		*findings = append(*findings, gvObjectFindings(t, value, path)...)
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			gvWalkFindings(t, value[key], path+"."+key, findings)
		}
	}
}

func gvObjectFindings(t *testing.T, object map[string]any, path string) []gvFinding {
	var out []gvFinding
	// An OriginRef is the object that carries origin_id and kind.
	if _, ok := object["origin_id"].(string); ok {
		switch kind, _ := object["kind"].(string); kind {
		case "operator", "automation", "projection":
			for _, member := range []string{"source_id", "source_epoch_id", "binding_id"} {
				if _, present := object[member]; present {
					out = append(out, gvFinding{rule: gvRuleOrigin, path: path + "." + member,
						error: semreg.InvalidValue, detail: "non-native origin path"})
				}
			}
		}
	}
	// A definition reference written with an owner instead of a pack.
	if definition, ok := object["definition"].(map[string]any); ok {
		if _, owner := definition["owner"]; owner {
			if _, pack := definition["pack"]; !pack {
				out = append(out, gvFinding{rule: gvRuleDefinition, path: path + ".definition",
					error: semreg.MissingMember, detail: "pack"})
			}
		}
	}
	// A Decimal is the object that carries coefficient and exponent10.
	if coefficient, ok := object["coefficient"].(string); ok {
		if _, exponent := object["exponent10"]; exponent && !gvCanonicalCoefficient(coefficient) {
			out = append(out, gvFinding{rule: gvRuleDecimal, path: path + ".coefficient",
				error: semreg.InvalidDecimal, detail: "decimal"})
		}
	}
	// A snapshot whose identifier is the digest over an empty
	// retained_observations member and not over the record without it.
	if id, ok := object["snapshot_id"].(string); ok {
		if retained, present := object["retained_observations"].([]any); present && len(retained) == 0 {
			with := gvWithout(object, "snapshot_id")
			without := gvWithout(with, "retained_observations")
			if string(gvDigestJSON(t, with)) == id && string(gvDigestJSON(t, without)) != id {
				out = append(out, gvFinding{rule: gvRuleSnapshotID, path: path + ".retained_observations",
					error: semreg.DigestMismatch, detail: "snapshot identity"})
			}
		}
		// A read snapshot with no contract member, no retained observations
		// and an identifier over that abbreviated object. The kernel digest
		// is of the completed snapshot, so the two identifiers differ.
		if gvAbbreviatedRead(object) && string(gvDigestJSON(t, gvWithout(object, "snapshot_id"))) == id {
			out = append(out, gvFinding{rule: gvRuleAbbreviated, path: path + ".snapshot_id",
				error: semreg.DigestMismatch, detail: "snapshot identity"})
		}
	}
	return out
}

// gvAbbreviatedRead is a read snapshot the document identifies without the
// kernel contract and without retained observations. The empty
// retained_observations rule owns that other shape.
func gvAbbreviatedRead(object map[string]any) bool {
	if _, present := object["contract"]; present {
		return false
	}
	if _, present := object["retained_observations"]; present {
		return false
	}
	for _, name := range []string{"resources", "relations", "facts"} {
		if _, present := object[name]; present {
			return true
		}
	}
	return false
}

// gvCanonicalCoefficient is the coefficient rule of serialization.md: "0", or
// an optionally negative integer without a leading zero that does not end in
// zero.
func gvCanonicalCoefficient(coefficient string) bool {
	if coefficient == "0" {
		return true
	}
	digits := strings.TrimPrefix(coefficient, "-")
	if digits == "" || digits[0] == '0' || digits[len(digits)-1] == '0' {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func gvWithout(object map[string]any, member string) map[string]any {
	out := make(map[string]any, len(object))
	for key, value := range object {
		if key != member {
			out[key] = value
		}
	}
	return out
}

// gvDocumentFindingStop ends a vector whose outcome a document finding
// explains; the subtest recovers it.
type gvDocumentFindingStop struct{}

// checkRecord holds a refusal or an acceptance of one document record
// against the rules the record breaks. A refusal a finding explains ends the
// vector as a document finding, even when it is the refusal a negative vector
// expects, because the record's own defect then hides the vector's criterion.
// A record that breaks a rule and is accepted fails, as does a refusal of a
// prior-state record that no finding explains.
func (c *vectorCase) checkRecord(where string, raw json.RawMessage, err error, prior bool) {
	c.t.Helper()
	findings := gvRecordFindings(c.t, raw)
	if err == nil {
		for _, finding := range findings {
			c.t.Errorf("%s breaks %s and was accepted", where, finding)
		}
		return
	}
	for _, finding := range findings {
		if finding.explains(err) {
			c.findings = append(c.findings, finding)
			c.t.Logf("document finding: %s refused with %v: %s", where, err, finding)
			panic(gvDocumentFindingStop{})
		}
	}
	if prior {
		c.t.Fatalf("%s rejected with %s: %v", where, semreg.ErrorIdentifier(err), err)
	}
}

// TestGraphVectorDocumentRules varies each member every rule reads, so a
// changed rule fails here and not only through the document.
func TestGraphVectorDocumentRules(t *testing.T) {
	evidence := `[{"owner":"o","kind":"k","digest":"d","contract":"c","access":"public","redaction":"none"}]`
	cases := []struct {
		name   string
		record string
		rules  []string
	}{
		{"operator origin without native fields", `{"origin":{"origin_id":"o","kind":"operator","evidence":` + evidence + `}}`, nil},
		{"operator origin with source", `{"origin":{"origin_id":"o","kind":"operator","source_id":"s","evidence":[]}}`, []string{gvRuleOrigin}},
		{"automation origin with epoch", `{"origin":{"origin_id":"o","kind":"automation","source_epoch_id":"e","evidence":[]}}`, []string{gvRuleOrigin}},
		{"projection origin with binding", `{"origin":{"origin_id":"o","kind":"projection","binding_id":"b","evidence":[]}}`, []string{gvRuleOrigin}},
		{"native origin with native fields", `{"origin":{"origin_id":"o","kind":"native_observation","source_id":"s","source_epoch_id":"e","binding_id":"b","evidence":[]}}`, nil},
		{"derived origin with source", `{"origin":{"origin_id":"o","kind":"derived","source_id":"s","evidence":[]}}`, nil},
		{"kind without origin_id", `{"kind":"operator","source_id":"s"}`, nil},
		{"pack definition", `{"definition":{"pack":{"id":"p","version":"1.0.0"},"id":"d","version":"1.0.0"}}`, nil},
		{"owner definition", `{"definition":{"owner":{"kind":"core","contract":"c"},"id":"d","version":"1.0.0"}}`, []string{gvRuleDefinition}},
		{"canonical decimal", `{"value":{"coefficient":"15","exponent10":2}}`, nil},
		{"negative canonical decimal", `{"value":{"coefficient":"-15","exponent10":0}}`, nil},
		{"zero decimal", `{"value":{"coefficient":"0","exponent10":0}}`, nil},
		{"trailing zero", `{"value":{"coefficient":"150","exponent10":0}}`, []string{gvRuleDecimal}},
		{"leading zero", `{"value":{"coefficient":"015","exponent10":0}}`, []string{gvRuleDecimal}},
		{"coefficient without exponent", `{"value":{"coefficient":"150"}}`, nil},
		{"nested in an array", `{"items":[{"value":{"coefficient":"-40","exponent10":1}}]}`, []string{gvRuleDecimal}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, finding := range gvRecordFindings(t, json.RawMessage(tc.record)) {
				got = append(got, finding.rule)
			}
			if !gvEqualStrings(got, tc.rules) {
				t.Fatalf("rules = %v, want %v", got, tc.rules)
			}
		})
	}

	t.Run("snapshot identity", func(t *testing.T) {
		snapshot := map[string]any{"asset_id": "asset:a", "retained_observations": []any{}}
		withEmpty := string(gvDigestJSON(t, snapshot))
		snapshot["snapshot_id"] = withEmpty
		if findings := gvRecordFindings(t, gvMarshal(t, snapshot)); len(findings) != 1 || findings[0].rule != gvRuleSnapshotID {
			t.Fatalf("identity over the empty member: %v", findings)
		}
		snapshot["snapshot_id"] = string(gvDigestJSON(t, map[string]any{"asset_id": "asset:a"}))
		if findings := gvRecordFindings(t, gvMarshal(t, snapshot)); len(findings) != 0 {
			t.Fatalf("identity without the empty member: %v", findings)
		}
		snapshot["snapshot_id"] = "sha256:other"
		if findings := gvRecordFindings(t, gvMarshal(t, snapshot)); len(findings) != 0 {
			t.Fatalf("unrelated identity: %v", findings)
		}
		filled := map[string]any{"asset_id": "asset:a", "retained_observations": []any{map[string]any{}}}
		filled["snapshot_id"] = string(gvDigestJSON(t, filled))
		if findings := gvRecordFindings(t, gvMarshal(t, filled)); len(findings) != 0 {
			t.Fatalf("non-empty retained observations: %v", findings)
		}
	})

	// Each case changes one field gvRuleAbbreviated reads. The production
	// mutation the case catches is named in the subtest.
	t.Run("abbreviated snapshot identity", func(t *testing.T) {
		body := map[string]any{
			"asset_id":  "asset:a",
			"resources": []any{map[string]any{"resource_id": "resource:a"}},
			"relations": []any{},
			"facts":     []any{},
		}
		identified := func(object map[string]any) map[string]any {
			object["snapshot_id"] = string(gvDigestJSON(t, object))
			return object
		}
		rulesOf := func(object map[string]any) []string {
			var got []string
			for _, finding := range gvRecordFindings(t, gvMarshal(t, object)) {
				got = append(got, finding.rule)
			}
			return got
		}
		if got := rulesOf(identified(body)); !gvEqualStrings(got, []string{gvRuleAbbreviated}) {
			t.Fatalf("abbreviated identity: %v", got)
		}
		// snapshot_id: one nibble changed, so the identifier is not the digest.
		changedID := identified(map[string]any{
			"asset_id": "asset:a", "resources": []any{map[string]any{"resource_id": "resource:a"}},
			"relations": []any{}, "facts": []any{},
		})
		identifier := changedID["snapshot_id"].(string)
		replacement := "1"
		if identifier[len(identifier)-1] == '1' {
			replacement = "0"
		}
		changedID["snapshot_id"] = identifier[:len(identifier)-1] + replacement
		if got := rulesOf(changedID); len(got) != 0 {
			t.Fatalf("snapshot_id nibble: %v", got)
		}
		// asset_id: a digested member changed after the identifier was fixed.
		changedAsset := identified(map[string]any{
			"asset_id": "asset:a", "resources": []any{map[string]any{"resource_id": "resource:a"}},
			"relations": []any{}, "facts": []any{},
		})
		changedAsset["asset_id"] = "asset:b"
		if got := rulesOf(changedAsset); len(got) != 0 {
			t.Fatalf("asset_id: %v", got)
		}
		// relations: a digested collection changed after the identifier was fixed.
		changedRelations := identified(map[string]any{
			"asset_id": "asset:a", "resources": []any{map[string]any{"resource_id": "resource:a"}},
			"relations": []any{}, "facts": []any{},
		})
		changedRelations["relations"] = []any{map[string]any{"from": "resource:a"}}
		if got := rulesOf(changedRelations); len(got) != 0 {
			t.Fatalf("relations: %v", got)
		}
		// contract: present, and the identifier is recomputed over that object,
		// so only the contract gate keeps the rule from firing.
		withContract := map[string]any{
			"asset_id": "asset:a", "contract": string(semreg.ContractKernelV1),
			"resources": []any{map[string]any{"resource_id": "resource:a"}},
			"relations": []any{}, "facts": []any{},
		}
		if got := rulesOf(identified(withContract)); len(got) != 0 {
			t.Fatalf("contract: %v", got)
		}
		// retained_observations: present and empty, identifier recomputed.
		// That shape belongs to the retained-observations rule only.
		withRetained := map[string]any{
			"asset_id": "asset:a", "retained_observations": []any{},
			"resources": []any{map[string]any{"resource_id": "resource:a"}},
			"relations": []any{}, "facts": []any{},
		}
		if got := rulesOf(identified(withRetained)); !gvEqualStrings(got, []string{gvRuleSnapshotID}) {
			t.Fatalf("retained_observations: %v", got)
		}
		// resources, relations and facts: all absent, identifier recomputed
		// over what remains, so only the read-body gate keeps the rule from firing.
		withoutBody := map[string]any{"asset_id": "asset:a"}
		if got := rulesOf(identified(withoutBody)); len(got) != 0 {
			t.Fatalf("read body: %v", got)
		}
	})

	t.Run("attribution", func(t *testing.T) {
		finding := gvFinding{rule: gvRuleOrigin, error: semreg.InvalidValue, detail: "non-native origin path"}
		for _, tc := range []struct {
			err  error
			want bool
		}{
			{nil, false},
			{fmt.Errorf("invalid_value: non-native origin path"), false},
			{gvErrorFor(t, `{"origin":{"origin_id":"origin:operator:1","kind":"operator","source_id":"source:configuration:01","evidence":[]}}`), true},
		} {
			if got := finding.explains(tc.err); got != tc.want {
				t.Errorf("explains(%v) = %v, want %v", tc.err, got, tc.want)
			}
		}
		refusal := gvErrorFor(t, `{"origin":{"origin_id":"origin:operator:1","kind":"operator","source_id":"source:configuration:01","evidence":[]}}`)
		for _, other := range []gvFinding{
			{rule: gvRuleOrigin, error: semreg.InvalidValue, detail: "some other detail"},
			{rule: gvRuleOrigin, error: semreg.MissingMember, detail: "non-native origin path"},
		} {
			if other.explains(refusal) {
				t.Errorf("%s/%s explains %v", other.error, other.detail, refusal)
			}
		}
	})
}

// gvErrorFor is the kernel's refusal of an origin record, decoded with the
// kernel's strict decoder.
func gvErrorFor(t *testing.T, record string) error {
	t.Helper()
	var wrapper struct {
		Origin json.RawMessage `json:"origin"`
	}
	if err := json.Unmarshal([]byte(record), &wrapper); err != nil {
		t.Fatal(err)
	}
	object := gvObject(t, wrapper.Origin)
	object["evidence"] = []any{gvEvidenceJSON("origin")}
	_, err := semreg.Decode[semreg.OriginRef](gvMarshal(t, object))
	if err == nil {
		t.Fatal("the kernel accepted an operator origin with a native source field")
	}
	return err
}
