package graph_test

import (
	"crypto/sha256"
	"encoding/json"
	"sort"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// graphVectorsPath is the graph acceptance document this module exercises.
const graphVectorsPath = "../../../fixtures/v1/graph-acceptance-vectors.json"

// graphVectorsDigest is the SHA-256 of that document, kept as bytes so that a
// changed file fails here before any vector runs.
var graphVectorsDigest = [sha256.Size]byte{
	0xf3, 0x34, 0x22, 0xcc, 0xcb, 0x2b, 0xb8, 0xd4,
	0x24, 0xcf, 0xc1, 0x86, 0x37, 0xf3, 0x30, 0x8d,
	0x96, 0x65, 0x9d, 0xb5, 0x03, 0x7e, 0x8a, 0x0f,
	0xde, 0x31, 0xda, 0x6b, 0x45, 0xdb, 0x2e, 0x73,
}

// TestGraphAcceptanceVectorsPin checks the copied bytes and the document
// header: the contract names, the catalog citation recorded in those bytes,
// and the closed operation, coverage and error vocabularies. The catalog
// copy and graph.PinnedDigest are a separate lock.
func TestGraphAcceptanceVectorsPin(t *testing.T) {
	raw := gvReadFixture(t)
	if sha256.Sum256(raw) != graphVectorsDigest {
		t.Fatalf("graph vector copy digest = %x, want %x", sha256.Sum256(raw), graphVectorsDigest)
	}
	doc := gvDecodeDocument(t, raw)
	if doc.Contract != "helianthus.semantic.graph.acceptance/v1" {
		t.Errorf("contract = %q", doc.Contract)
	}
	if doc.KernelContract != string(graph.KernelContract) {
		t.Errorf("kernel_contract = %q, want %q", doc.KernelContract, graph.KernelContract)
	}
	if doc.ElementCatalog.Contract != graph.Contract || doc.ElementCatalog.Path != "api/v1/element-catalog-v1.json" {
		t.Errorf("element_catalog = %+v", doc.ElementCatalog)
	}
	// The recorded digest is the seed the document was written against.
	// Those bytes are pinned above. Validate rejects a citation that is not a digest.
	if err := doc.ElementCatalog.SHA256.Validate(); err != nil {
		t.Errorf("element_catalog.sha256 = %s", doc.ElementCatalog.SHA256)
	}
	if got := gvSortedKeys(gvOperations); !gvEqualStrings(doc.Operations, got) {
		t.Errorf("operations = %v, the harness implements %v", doc.Operations, got)
	}
	ids := map[string]struct{}{}
	for _, vector := range doc.Vectors {
		if _, repeated := ids[vector.ID]; repeated {
			t.Errorf("vector %s is repeated", vector.ID)
		}
		ids[vector.ID] = struct{}{}
	}
	states := map[string]struct{}{}
	for _, state := range doc.PriorStates {
		if _, repeated := states[state.Name]; repeated {
			t.Errorf("prior state %s is repeated", state.Name)
		}
		states[state.Name] = struct{}{}
	}
}

// TestGraphAcceptanceVectors executes every vector of the copied document by
// identifier through the public API: construct_registry, register_catalog,
// apply, assemble_cut, admit, list_resources, traverse, retrieve_lens,
// sum_members and project, with the element catalog registered wherever the
// prior state registers it. A vector whose input carries subject_check is
// pinned with the document and is not executed. No vector is selected by
// identifier.
// Each vector ends in one of three outcomes: it passes; it fails,
// for an operation or assertion the harness does not implement or for any
// outcome no document finding explains; or it ends as a document finding,
// when the kernel refuses a record of the document with exactly the stable
// error of a contract rule that record breaks (vector_findings_test.go).
// Conformance to graph.md section 1 holds only when no vector ends as a
// document finding; the closing log line lists them by rule.
func TestGraphAcceptanceVectors(t *testing.T) {
	doc := gvDecodeDocument(t, gvReadFixture(t))
	states := map[string]gvPriorState{}
	for _, state := range doc.PriorStates {
		states[state.Name] = state
	}
	outcomes := map[string]string{}
	byRule := map[string][]string{}
	coverage := map[string]map[string]bool{}
	pinned := 0
	for _, vector := range doc.Vectors {
		vector := vector
		if gvInputHasSubjectCheck(vector.Input) {
			pinned++
			continue
		}
		var finding []gvFinding
		passed := t.Run(vector.ID, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if _, stop := r.(gvDocumentFindingStop); !stop {
						panic(r)
					}
				}
			}()
			run, ok := gvOperations[vector.Operation]
			if !ok {
				t.Fatalf("operation %q is not implemented by the vector harness", vector.Operation)
			}
			state, ok := states[vector.PriorState]
			if !ok {
				t.Fatalf("prior state %q is not defined", vector.PriorState)
			}
			c := newVectorCase(t, doc, vector, state)
			defer func() { finding = c.findings }()
			c.checkHeader()
			run(c)
		})
		switch {
		case !passed:
			outcomes[vector.ID] = "fail"
		case len(finding) != 0:
			outcomes[vector.ID] = "document finding"
			byRule[finding[0].rule] = append(byRule[finding[0].rule], vector.ID)
		default:
			outcomes[vector.ID] = "pass"
		}
		for _, area := range vector.Coverage {
			if coverage[area] == nil {
				coverage[area] = map[string]bool{}
			}
			coverage[area][vector.Polarity] = true
		}
	}
	if pinned == 0 {
		t.Error("the copied document pins no subject_check vector")
	}
	if len(outcomes) != len(doc.Vectors)-pinned {
		t.Errorf("executed %d of %d vectors", len(outcomes), len(doc.Vectors)-pinned)
	}
	for _, area := range doc.CoverageIDs {
		if !coverage[area]["positive"] || !coverage[area]["negative"] {
			t.Errorf("coverage %s lacks a positive or a negative vector", area)
		}
	}
	count := map[string][]string{}
	for id, outcome := range outcomes {
		count[outcome] = append(count[outcome], id)
	}
	for _, ids := range count {
		sort.Strings(ids)
	}
	t.Logf("graph vectors: %d executed, %d passed %v, %d failed %v, %d document findings",
		len(outcomes), len(count["pass"]), count["pass"], len(count["fail"]), count["fail"], len(count["document finding"]))
	rules := make([]string, 0, len(byRule))
	for rule := range byRule {
		rules = append(rules, rule)
	}
	sort.Strings(rules)
	for _, rule := range rules {
		sort.Strings(byRule[rule])
		t.Logf("document finding, %d vectors: %s: %v", len(byRule[rule]), rule, byRule[rule])
	}
	gvCheckConformanceReport(t, outcomes, byRule)
}

func gvInputHasSubjectCheck(raw json.RawMessage) bool {
	var input map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &input) != nil {
		return false
	}
	_, ok := input["subject_check"]
	return ok
}
