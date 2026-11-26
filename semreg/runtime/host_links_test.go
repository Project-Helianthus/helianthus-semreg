package runtime

import (
	"context"
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/call"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestHostCaptureShowsRebindAndRevocationInLinkEndpoints(t *testing.T) {
	ctx := context.Background()
	svc := openService(t, newTestClock())
	old := memberNode(t, svc, "unit.old", "a")
	survivor := memberNode(t, svc, "unit.new", "b")
	peer := memberNode(t, svc, "unit.peer", "c")
	reach := v1.DefinitionRef{ID: "relation.test_reach", Version: "1.0.0", Pack: v1.PackRef{ID: "pack.test", Version: "1.0.0"}}
	declared := driver.LinkKey{Type: reach, From: old, To: peer}
	basis := []v1.EvidenceRef{testEvidence("link basis")}
	publisher := identity.Publisher{Driver: testKind, Instance: "unit.peer"}
	if err := svc.Ledger().ClaimLink(publisher, driver.LinkClaim{Key: declared, Basis: basis}); err != nil {
		t.Fatal(err)
	}
	join := driver.CertifiedJoin{A: old, B: survivor, Survivor: survivor, Statement: driver.StatementRef{Revision: "1", Evidence: testEvidence("statement")}}
	capture := func() []identity.Link {
		var sink captureSink
		if _, err := (&hostCuts{svc: svc}).Capture(withRefSink(ctx, &sink), driver.Filter{}); err != nil {
			t.Fatal(err)
		}
		return sink.Links
	}
	check := func(label string, from driver.NodeRef) {
		t.Helper()
		links := capture()
		if len(links) != 1 || links[0].Key.From != from || links[0].Key.To != peer || len(links[0].Claims) != 1 {
			t.Fatalf("%s: links %+v", label, links)
		}
		claim := links[0].Claims[0]
		if claim.Declared != declared || len(claim.Basis) != 1 || claim.Basis[0] != basis[0] {
			t.Fatalf("%s: claim lost its declared key or basis: %+v", label, claim)
		}
	}
	check("before rebind", old)
	if err := svc.Ledger().Rebind(old, survivor, join); err != nil {
		t.Fatal(err)
	}
	check("after rebind", survivor)
	if err := svc.Ledger().Revoke(join); err != nil {
		t.Fatal(err)
	}
	check("after revocation", old)
}

func TestHostSessionOperationsCarryTheCallingSurface(t *testing.T) {
	plain := withSessionSurface(context.Background())
	caller, ok := call.CallerFrom(plain)
	if !ok || caller.Surface != hostSurface() {
		t.Fatalf("a plain context lost the session's surface: %+v", caller)
	}
	named := v1.EvidenceRef{Owner: "other", Kind: "semantic.caller.other", Digest: "sha256:aa"}
	kept, _ := call.CallerFrom(withSessionSurface(call.WithCaller(context.Background(), call.Caller{Surface: named})))
	if kept.Surface != named {
		t.Fatalf("a surface the caller already named was replaced: %+v", kept.Surface)
	}
}
