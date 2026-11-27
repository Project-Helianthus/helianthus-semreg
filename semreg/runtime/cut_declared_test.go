package runtime

import (
	"strings"
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func TestCommitRejectsADeclaredLineageAndCandidateThatDisagree(t *testing.T) {
	cert := testEvidence("certification:one")
	cert.Kind = v1.EvidenceKindCertification
	for _, tc := range []struct {
		name    string
		lineage driver.OriginKind
		declare bool
	}{
		{"declared lineage with a native candidate", driver.OriginDeclared, false},
		{"measured lineage with a declared candidate", driver.OriginMeasured, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := newTestClock()
			svc := openService(t, clock)
			session := openPlain(t, svc, "unit.declared", "asset:declared", "source:declared", "epoch:declared", "1")
			fact := keepFresh(observedFact(session.spec, "candidate:declared", "binding:declared", "1", "32165", clock.Now(), 100))
			if tc.declare {
				fact.BindingID, fact.SourceEpochID, fact.DriverGeneration = nil, nil, nil
				fact.Quality.Assertion = v1.AssertionInferred
				fact.Origin = v1.OriginRef{OriginID: "origin:declared", Kind: v1.OriginDeclared, Evidence: []v1.EvidenceRef{cert}}
			}
			self := driver.InputRef{At: graph.CutRef{}, Candidate: "candidate:declared", Revision: "1"}
			err := commitErr(session, func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{fact} }, func(tx *transaction) {
				tx.self, tx.origin, tx.hasLine = self, driver.Origin{Kind: tc.lineage, Evidence: []v1.EvidenceRef{cert}}, true
			})
			if err == nil || !strings.Contains(err.Error(), "disagree") {
				t.Fatalf("commit error: %v", err)
			}
		})
	}
}
