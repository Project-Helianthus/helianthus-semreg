package graph_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// gvConformanceFindings is the order and the wording of the document-finding
// classes in the graph conformance paragraph of README.md.
var gvConformanceFindings = []struct {
	rule  string
	label string
}{
	{gvRuleDefinition, "relation definitions are written as owner-tagged catalog references instead of a definition reference"},
	{gvRuleOrigin, "operator origins carry native source fields"},
	{gvRuleDecimal, "decimals are not canonical"},
	{gvRuleSnapshotID, "snapshot identifiers digest an empty retained-observation list the kernel omits"},
	{gvRuleAbbreviated, "a read snapshot's identifier is of the abbreviated object rather than the kernel snapshot"},
}

// gvCheckConformanceReport fails when the graph conformance paragraph of
// README.md does not report this run. The paragraph is the published account
// of how many vectors pass, fail and end as document findings, and of what
// the two digest assertions compare.
func gvCheckConformanceReport(t *testing.T, outcomes map[string]string, byRule map[string][]string) {
	t.Helper()
	raw, err := os.ReadFile("../../../README.md")
	if err != nil {
		t.Fatalf("read conformance account: %v", err)
	}
	const marker = "Conformance is not reached yet:"
	text := string(raw)
	start := strings.Index(text, marker)
	if start < 0 {
		t.Fatal("README has no graph conformance account")
	}
	section := text[start:]
	end := strings.Index(section, "\n## ")
	if end < 0 {
		t.Fatal("graph conformance account does not end before the next heading")
	}
	got := strings.Join(strings.Fields(section[:end]), " ")
	want := strings.Join(strings.Fields(gvConformanceAccount(t, outcomes, byRule)), " ")
	if got != want {
		t.Fatalf("graph conformance account = %s, want %s", got, want)
	}
}

func gvConformanceAccount(t *testing.T, outcomes map[string]string, byRule map[string][]string) string {
	t.Helper()
	var pass, fail []string
	findings := 0
	for id, outcome := range outcomes {
		switch outcome {
		case "pass":
			pass = append(pass, id)
		case "fail":
			fail = append(fail, id)
		case "document finding":
			findings++
		default:
			t.Fatalf("vector %s has outcome %q", id, outcome)
		}
	}
	sort.Strings(pass)
	sort.Strings(fail)
	known := map[string]string{}
	for _, item := range gvConformanceFindings {
		known[item.rule] = item.label
	}
	var clauses []string
	accounted := 0
	for _, item := range gvConformanceFindings {
		n := len(byRule[item.rule])
		if n == 0 {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("%s (%d)", item.label, n))
		accounted += n
	}
	if accounted != findings {
		var extra []string
		for rule, ids := range byRule {
			if _, ok := known[rule]; !ok && len(ids) != 0 {
				extra = append(extra, fmt.Sprintf("%s (%d)", rule, len(ids)))
			}
		}
		sort.Strings(extra)
		t.Fatalf("document findings %d, accounted %d, unlisted %v", findings, accounted, extra)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Conformance is not reached yet: it requires every vector to pass. %d vectors execute and none are skipped. ", len(outcomes))
	if len(pass) == 0 {
		b.WriteString("0 pass. ")
	} else {
		fmt.Fprintf(&b, "%d pass: %s. ", len(pass), gvJoinBackticks(pass))
	}
	if len(fail) == 0 {
		b.WriteString("None fail. ")
	} else {
		fmt.Fprintf(&b, "%d fail: %s. ", len(fail), gvJoinBackticks(fail))
	}
	b.WriteString("The copied document records the element-catalog digest of the seed it was written against. ")
	b.WriteString("`catalog_digest_exposed` and `digest_equals_pinned_digest` require the loaded catalog digest to equal `graph.PinnedDigest`; the recorded citation is not an input to either assertion. ")
	b.WriteString("The pinned catalog is unchanged. ")
	if findings == 0 {
		b.WriteString("No vector ends as a document finding. ")
	} else {
		fmt.Fprintf(&b, "The other %d vectors end as document findings: %s. ", findings, gvJoinClauses(clauses))
	}
	b.WriteString("Each finding is resolved by an in-place edit of the vector document, of the contract, of the kernel or of the catalog copy; behind the first refusal further disagreements may remain, and they surface as findings or failures once the first is resolved.")
	return b.String()
}

func gvJoinBackticks(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "`" + id + "`"
	}
	switch len(quoted) {
	case 0:
		return ""
	case 1:
		return quoted[0]
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	}
}

func gvJoinClauses(clauses []string) string {
	switch len(clauses) {
	case 0:
		return ""
	case 1:
		return clauses[0]
	default:
		return strings.Join(clauses[:len(clauses)-1], "; ") + "; and " + clauses[len(clauses)-1]
	}
}

// TestDigestAssertionsIgnoreRecordedCitation pins the comparison the
// conformance account describes. The copied document cites an element-catalog
// digest other than graph.PinnedDigest. Both assertions still accept the
// loaded catalog, because they compare that catalog with graph.PinnedDigest
// and do not read the recorded citation.
func TestDigestAssertionsIgnoreRecordedCitation(t *testing.T) {
	doc := gvDecodeDocument(t, gvReadFixture(t))
	if err := doc.ElementCatalog.SHA256.Validate(); err != nil {
		t.Fatalf("recorded citation: %v", err)
	}
	if doc.ElementCatalog.SHA256 == graph.PinnedDigest {
		t.Fatal("recorded citation equals graph.PinnedDigest")
	}
	catalog := gvEmbedded(t)
	if catalog.Digest() != graph.PinnedDigest {
		t.Fatalf("loaded catalog digest = %s", catalog.Digest())
	}
	registry, err := semreg.NewGraphRegistry(catalog, gvAcceptedValidators()...)
	if err != nil {
		t.Fatal(err)
	}
	c := &vectorCase{t: t, doc: doc}
	if err := registryAssertions["catalog_digest_exposed"](c, registryResult{registry: registry}); err != nil {
		t.Fatalf("catalog_digest_exposed: %v", err)
	}
	if err := catalogAssertions["digest_equals_pinned_digest"](c, catalogResult{catalog: catalog}); err != nil {
		t.Fatalf("digest_equals_pinned_digest: %v", err)
	}
}
