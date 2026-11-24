package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/sources"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

func TestPublishUndeclaredSampleLeavesKernelUnchanged(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	node := memberNode(t, svc, "unit.one", "slot.one")
	session := openPlain(t, svc, "unit.one", "asset:one", "source:one", "epoch:one", "1")
	fact := keepFresh(observedFact(session.spec, "candidate:one", "binding:one", "1", "32165", clock.Now(), 100))
	decoy := declareFact(node, node, "path.decoy", 10, fact)
	missing := declareFact(node, node, "path.missing", 20, fact)
	err := commitErr(session, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(session.spec, "registry:one")}
		b.BindingUpserts = []v1.NativeBinding{bindingOf(session.spec, "binding:one", "native-resource:binding:one")}
		b.IdentityLinkUpserts = []v1.IdentityLink{linkOf(session.spec, "binding:one", "basis:one", nil)}
		b.FactUpserts = []v1.FactCandidate{fact}
	}, func(tx *transaction) {
		if err := tx.DeclareSource(decoy); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageSample(driver.Sample{From: missing, Candidate: fact}); err != nil {
			t.Fatal(err)
		}
	})
	if !errors.Is(err, sources.ErrUndeclared) {
		t.Fatalf("commit error %v", err)
	}
	if _, _, ok := currentOf(svc, "asset:one"); ok {
		t.Fatal("a rejected sample published the kernel")
	}
	if svc.positionUnsafe() != 0 {
		t.Fatal("a rejected sample advanced the cursor")
	}
	if len(svc.Ledger().Sources()) != 0 {
		t.Fatal("a rejected sample recorded a source")
	}
	publishFact(t, session, "binding:one", declareFact(node, node, "path.one", 30, fact), fact)
	if coefficientOf(assetSnap(t, svc, "asset:one"), "candidate:one") != "32165" || svc.positionUnsafe() != 1 {
		t.Fatal("the following commit did not take the rejected sequence")
	}
}

func TestPublishFencedGenerationLeavesStoreUnchanged(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	node := memberNode(t, svc, "unit.one", "slot.one")
	session := openPlain(t, svc, "unit.one", "asset:unit.one", "source:unit.one", "epoch:unit.one", "1")
	fact := keepFresh(observedFact(session.spec, "candidate:dhw.one", "binding:unit.one:g1", "1", "32165", clock.Now(), 100))
	publishFact(t, session, "binding:unit.one:g1", declareFact(node, node, "path.dhw", 30, fact), fact)
	before := assetSnap(t, svc, "asset:unit.one")
	session.Fence()
	err := commitErr(session, func(b *v1.PublicationBatch) {
		next := fact
		next.Revision = "2"
		value := kelvinQuantity("32265", -2)
		next.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: &value}
		b.FactUpserts = []v1.FactCandidate{next}
	}, nil)
	requireIdent(t, err, v1.StaleDriverGeneration)
	after := assetSnap(t, svc, "asset:unit.one")
	if before.SnapshotID != after.SnapshotID || coefficientOf(after, "candidate:dhw.one") != "32165" {
		t.Fatalf("fenced commit changed %s %s", after.SnapshotID, coefficientOf(after, "candidate:dhw.one"))
	}
}

func TestPublishManufacturerSerialOwnershipFence(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	ownerNode := memberNode(t, svc, "unit.owner", "slot.owner")
	otherNode := memberNode(t, svc, "unit.other", "slot.other")
	owner := openPlain(t, svc, "unit.owner", "asset:owner", "source:owner", "epoch:owner", "1")
	other := openPlain(t, svc, "unit.other", "asset:other", "source:other", "epoch:other", "1")
	fact := keepFresh(observedFact(owner.spec, "candidate:owner", "binding:owner", "1", "32165", clock.Now(), 100))
	source := declareFact(ownerNode, ownerNode, "path.owner", 30, fact)
	first := driver.ManufacturerSerialClaim{Member: ownerNode, Manufacturer: "maker.alpha", Serial: "token.alpha", Proof: []v1.EvidenceRef{testEvidence("proof:alpha")}}
	commit(t, owner, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(owner.spec, "registry:owner")}
		b.BindingUpserts = []v1.NativeBinding{bindingOf(owner.spec, "binding:owner", "native-resource:binding:owner")}
		b.IdentityLinkUpserts = []v1.IdentityLink{linkOf(owner.spec, "binding:owner", "basis:owner", nil)}
		b.FactUpserts = []v1.FactCandidate{fact}
	}, func(tx *transaction) {
		if err := tx.DeclareSource(source); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageSample(driver.Sample{From: source, Candidate: fact}); err != nil {
			t.Fatal(err)
		}
		if err := tx.AdmitManufacturerSerial(first); err != nil {
			t.Fatal(err)
		}
	})
	stored, err := svc.Ledger().Export()
	if err != nil {
		t.Fatal(err)
	}
	proof := testEvidence("proof:alpha")
	if !bytes.Contains(stored, []byte("maker.alpha")) || !bytes.Contains(stored, []byte("token.alpha")) || !bytes.Contains(stored, []byte(proof.Digest)) {
		t.Fatalf("the owned claim did not record manufacturer, serial and proof\n%s", stored)
	}
	unknown := driver.NodeRef{Bridge: "bridge.test", ID: "resource:missing"}
	if err := commitErr(owner, func(b *v1.PublicationBatch) {}, func(tx *transaction) {
		if err := tx.AdmitManufacturerSerial(driver.ManufacturerSerialClaim{Member: unknown, Manufacturer: "maker.unknown", Serial: "token.unknown", Proof: []v1.EvidenceRef{testEvidence("proof:unknown")}}); err != nil {
			t.Fatal(err)
		}
	}); !errors.Is(err, identity.ErrUnknownMember) {
		t.Fatalf("unknown member %v", err)
	}
	foreign := driver.ManufacturerSerialClaim{Member: ownerNode, Manufacturer: "maker.foreign", Serial: "token.foreign", Proof: []v1.EvidenceRef{testEvidence("proof:foreign")}}
	if err := commitErr(other, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(other.spec, "registry:other")}
	}, func(tx *transaction) {
		if err := tx.AdmitManufacturerSerial(foreign); err != nil {
			t.Fatal(err)
		}
	}); !errors.Is(err, identity.ErrNotOwner) {
		t.Fatalf("other owner %v", err)
	}
	if err := commitErr(other, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(other.spec, "registry:other")}
	}, func(tx *transaction) {
		if err := tx.AdmitManufacturerSerial(driver.ManufacturerSerialClaim{Member: otherNode, Manufacturer: "maker.other", Serial: "token.other", Proof: []v1.EvidenceRef{testEvidence("proof:other-node")}}); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	held, err := svc.Ledger().Export()
	if err != nil {
		t.Fatal(err)
	}
	otherProof := testEvidence("proof:other-node")
	if bytes.Contains(held, []byte("token.foreign")) || bytes.Contains(held, []byte("maker.unknown")) || bytes.Contains(held, []byte("maker.foreign")) {
		t.Fatal("a rejected claim was stored")
	}
	if !bytes.Contains(held, []byte("maker.other")) || !bytes.Contains(held, []byte("token.other")) || !bytes.Contains(held, []byte(otherProof.Digest)) {
		t.Fatal("the other owner claim of its own member was not stored")
	}
	second := driver.ManufacturerSerialClaim{Member: ownerNode, Manufacturer: "maker.beta", Serial: "token.beta", Proof: []v1.EvidenceRef{testEvidence("proof:beta")}}
	commit(t, owner, func(b *v1.PublicationBatch) {}, func(tx *transaction) {
		if err := tx.AdmitManufacturerSerial(second); err != nil {
			t.Fatal(err)
		}
	})
	updated, err := svc.Ledger().Export()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(updated, []byte("maker.beta")) || !bytes.Contains(updated, []byte("token.beta")) || bytes.Contains(updated, []byte("token.alpha")) {
		t.Fatal("the owner update did not replace manufacturer and serial")
	}
	owner.Fence()
	before, _ := svc.Ledger().Export()
	err = commitErr(owner, func(b *v1.PublicationBatch) {}, func(tx *transaction) {
		if err := tx.AdmitManufacturerSerial(driver.ManufacturerSerialClaim{Member: ownerNode, Manufacturer: "maker.fenced", Serial: "token.fenced", Proof: []v1.EvidenceRef{testEvidence("proof:fenced")}}); err != nil {
			t.Fatal(err)
		}
	})
	requireIdent(t, err, v1.StaleDriverGeneration)
	after, _ := svc.Ledger().Export()
	if !bytes.Equal(before, after) {
		t.Fatal("a fenced generation changed the serial claim")
	}
}

func TestPublishOperatorLinksPendingCarryAndWithdrawal(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	session, err := svc.OpenSession(SessionSpec{
		Driver: testKind, Instance: "unit.op", Asset: "asset:op", Source: "source:op", Epoch: "epoch:op",
		Generation: "1", Graph: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	selectedRes, err := svc.operatorResource("resource:selected", life.EquipmentKind, "slot.selected")
	if err != nil {
		t.Fatal(err)
	}
	thirdRes, err := svc.operatorResource("resource:third", life.EquipmentKind, "slot.third")
	if err != nil {
		t.Fatal(err)
	}
	bindings := []v1.NativeBinding{
		bindingOf(session.spec, "binding:a", "native-resource:binding:a"),
		bindingOf(session.spec, "binding:b", "native-resource:binding:b"),
	}
	commit(t, session, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(session.spec, "registry:op")}
		b.BindingUpserts = bindings
		b.ResourceUpserts = []v1.Resource{selectedRes, thirdRes}
		b.IdentityLinkUpserts = []v1.IdentityLink{
			linkOf(session.spec, "binding:a", "basis:a", nil),
			linkOf(session.spec, "binding:b", "basis:b", nil),
		}
	}, nil)
	nativeA := testEvidence("native-resource:binding:a")
	session.DeclareOperator(operatorDecl(nativeA, "resource:selected"))
	pending := operatorPending(t, session, nil)
	if len(pending) != 1 || pending[0].BindingID != "binding:a" || pending[0].State != v1.LinkQualified || pending[0].Resource == nil || *pending[0].Resource != "resource:selected" {
		t.Fatalf("pending without an upsert = %+v", pending)
	}
	commit(t, session, func(b *v1.PublicationBatch) {}, nil)
	if again := operatorPending(t, session, nil); len(again) != 1 || again[0].BindingID != pending[0].BindingID {
		t.Fatalf("a batch with no carry cleared the declaration: %+v", again)
	}
	carryOperator(t, session, nil, pending)
	snap := assetSnap(t, svc, "asset:op")
	if resourceOf(snap, "binding:a") != "resource:selected" || resourceOf(snap, "binding:b") != "" {
		t.Fatalf("links after carry a=%s b=%s", resourceOf(snap, "binding:a"), resourceOf(snap, "binding:b"))
	}
	if leftover := operatorPending(t, session, nil); len(leftover) != 0 {
		t.Fatalf("pending after carry %+v", leftover)
	}
	foreign := openPlain(t, svc, "unit.foreign", "asset:op", "source:foreign", "epoch:foreign", "1")
	err = commitErr(foreign, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(foreign.spec, "registry:foreign")}
		b.IdentityLinkUpserts = []v1.IdentityLink{pending[0]}
	}, nil)
	if err == nil {
		t.Fatal("a foreign owner carried the association")
	}
	if resourceOf(assetSnap(t, svc, "asset:op"), "binding:a") != "resource:selected" {
		t.Fatal("the foreign batch changed the association")
	}
	session.WithdrawOperator(nativeA)
	nativeC := testEvidence("native-resource:binding:c")
	session.DeclareOperator(operatorDecl(nativeC, "resource:third"))
	session.DeclareOperator(operatorDecl(nativeA, "resource:selected"))
	session.WithdrawOperator(nativeA)
	bindingC := bindingOf(session.spec, "binding:c", "native-resource:binding:c")
	openTx, err := session.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	both := openTx.(*transaction).OperatorLinks([]v1.NativeBinding{bindingC})
	var withdrawals []v1.IdentityLink
	qualified := 0
	for _, link := range both {
		if link.State == v1.LinkWithdrawn {
			withdrawals = append(withdrawals, link)
		} else {
			qualified++
		}
	}
	if len(withdrawals) != 1 || withdrawals[0].BindingID != "binding:a" || qualified != 1 {
		t.Fatalf("pending withdrawal and declaration = %+v", both)
	}
	batch, err := session.Prepare(func(b *v1.PublicationBatch) {
		b.BindingUpserts = []v1.NativeBinding{bindingC}
		b.IdentityLinkUpserts = withdrawals
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := openTx.(*transaction).Stage(batch); err != nil {
		t.Fatal(err)
	}
	if err := openTx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap = assetSnap(t, svc, "asset:op")
	if linkState(snap, "binding:a") != v1.LinkWithdrawn || resourceOf(snap, "binding:b") != "" || resourceOf(snap, "binding:c") != "" {
		t.Fatalf("withdrawal batch a=%s b=%s c=%s", linkState(snap, "binding:a"), resourceOf(snap, "binding:b"), resourceOf(snap, "binding:c"))
	}
	still := operatorPending(t, session, nil)
	if len(still) != 1 || still[0].BindingID != "binding:c" || still[0].State != v1.LinkQualified {
		t.Fatalf("the independent declaration did not stay pending: %+v", still)
	}
	held, err := session.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session.Fence()
	if got := held.(*transaction).OperatorLinks(nil); got != nil {
		t.Fatalf("fenced owner links %+v", got)
	}
	if err := held.Abort(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := held.(*transaction).OperatorLinks(nil); got != nil {
		t.Fatalf("closed transaction links %+v", got)
	}
	before := assetSnap(t, svc, "asset:op").SnapshotID
	err = commitErr(session, func(b *v1.PublicationBatch) {}, nil)
	requireIdent(t, err, v1.StaleDriverGeneration)
	if assetSnap(t, svc, "asset:op").SnapshotID != before {
		t.Fatal("a fenced owner commit changed the snapshot")
	}
}

func TestPublishJoinedOwnersKeepOwnFacts(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	leftNode := memberNode(t, svc, "unit.left", "slot.left")
	rightNode := memberNode(t, svc, "unit.right", "slot.right")
	left := openPlain(t, svc, "unit.left", "asset:left", "source:left", "epoch:left", "1")
	right := openPlain(t, svc, "unit.right", "asset:right", "source:right", "epoch:right", "1")
	leftFact := keepFresh(observedFact(left.spec, "candidate:shared", "binding:left", "1", "32165", clock.Now(), 1000))
	leftFact.Times.EvaluateMonotonic = monoNS(1000)
	leftFact.Times.ReceiptMonotonic = monoNS(1000)
	leftFact.FreshnessPolicy.FreshForNS = "100"
	rightFact := keepFresh(observedFact(right.spec, "candidate:shared", "binding:right", "1", "10001", clock.Now(), 100))
	rightFact.FreshnessPolicy.FreshForNS = "50"
	leftSource := declareFact(leftNode, leftNode, "path.left", 30, leftFact)
	rightSource := declareFact(rightNode, rightNode, "path.right", 30, rightFact)
	publishWithSerial(t, left, "binding:left", leftSource, leftFact, driver.ManufacturerSerialClaim{Member: leftNode, Manufacturer: "maker.synthetic", Serial: "token.shared", Proof: []v1.EvidenceRef{testEvidence("proof:left")}})
	publishWithSerial(t, right, "binding:right", rightSource, rightFact, driver.ManufacturerSerialClaim{Member: rightNode, Manufacturer: "maker.synthetic", Serial: "token.shared", Proof: []v1.EvidenceRef{testEvidence("proof:right")}})
	view, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	leftEl := svc.Element(leftSource.For, view)
	rightEl := svc.Element(rightSource.For, view)
	if valueCoefficient(leftEl.Value) != "32165" || leftEl.Freshness != v1.FreshnessFresh {
		t.Fatalf("left %+v %s", leftEl.Freshness, valueCoefficient(leftEl.Value))
	}
	if valueCoefficient(rightEl.Value) != "10001" || rightEl.Freshness != v1.FreshnessStale {
		t.Fatalf("right %+v %s", rightEl.Freshness, valueCoefficient(rightEl.Value))
	}
	reversed := view
	for i, j := 0, len(reversed.Members)-1; i < j; i, j = i+1, j-1 {
		reversed.Members[i], reversed.Members[j] = reversed.Members[j], reversed.Members[i]
	}
	if valueCoefficient(svc.Element(leftSource.For, reversed).Value) != "32165" || valueCoefficient(svc.Element(rightSource.For, reversed).Value) != "10001" {
		t.Fatal("member order changed the reported values")
	}
	resolved, err := svc.Ledger().Resolve(view.Cut)
	if err != nil || !sameClass(resolved, leftNode, rightNode) {
		t.Fatalf("join %v %+v", err, resolved.Resources)
	}
}

func TestPublishMirrorStopAndGenerationWithdrawal(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	node := memberNode(t, svc, "unit.heat", "slot.heat")
	otherNode := memberNode(t, svc, "unit.other", "slot.other")
	primary := openPlain(t, svc, "unit.heat", "asset:heat", "source:primary", "epoch:heat", "1")
	mirror := openPlain(t, svc, "unit.mirror", "asset:heat", "source:mirror", "epoch:mirror", "1")
	other := openPlain(t, svc, "unit.other", "asset:other", "source:other", "epoch:other", "1")
	primaryFact := retitle(keepFresh(observedFact(primary.spec, "candidate:primary", "binding:primary", "1", "32165", clock.Now(), 100)), "thermal.measurement.derived.heat", "candidate:primary")
	mirrorFact := keepFresh(observedFact(mirror.spec, "candidate:mirror", "binding:mirror", "1", "11111", clock.Now(), 100))
	mirrorFact.Key = primaryFact.Key
	otherFact := keepFresh(observedFact(other.spec, "candidate:primary", "binding:other", "1", "99999", clock.Now(), 100))
	// The mirror commit below advances the clock by a millisecond. Retention
	// has to outlast that sample or the primary basis expires before the stop.
	primaryFact.FreshnessPolicy.RetainForNS = "10000000000"
	mirrorFact.FreshnessPolicy.RetainForNS = "10000000000"
	otherFact.FreshnessPolicy.RetainForNS = "10000000000"
	primarySource := declareFact(node, node, "path.primary", 40, primaryFact)
	mirrorSource := declareFact(node, node, "path.mirror", 10, mirrorFact)
	publishFact(t, primary, "binding:primary", primarySource, primaryFact)
	publishFact(t, mirror, "binding:mirror", mirrorSource, mirrorFact)
	publishFact(t, other, "binding:other", declareFact(otherNode, otherNode, "path.other", 30, otherFact), otherFact)
	before, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := receiptNS(svc, before, primarySource)
	beforeID := assetSnap(t, svc, "asset:heat").SnapshotID
	clock.advance(time.Millisecond)
	nextMirror := mirrorFact
	nextMirror.Revision = "2"
	value := kelvinQuantity("22222", -2)
	nextMirror.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: &value}
	commit(t, mirror, func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{nextMirror} }, func(tx *transaction) {
		if err := tx.StageSample(driver.Sample{From: mirrorSource, Candidate: nextMirror}); err != nil {
			t.Fatal(err)
		}
	})
	advanced, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if receiptNS(svc, advanced, primarySource) != receipt || assetSnap(t, svc, "asset:heat").SnapshotID == beforeID {
		t.Fatalf("mirror update refreshed the primary receipt %s or did not advance the member", receiptNS(svc, advanced, primarySource))
	}
	if valueCoefficient(svc.Element(primarySource.For, advanced).Value) != "32165" {
		t.Fatal(valueCoefficient(svc.Element(primarySource.For, advanced).Value))
	}
	primary.Fence()
	stopped, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	stoppedEl := svc.Element(primarySource.For, stopped)
	if sourceState(stoppedEl, "path.primary").Disposition != sources.DispositionStopped || valueCoefficient(stoppedEl.Value) != "32165" {
		t.Fatalf("stopped basis %+v %s", sourceState(stoppedEl, "path.primary").Disposition, valueCoefficient(stoppedEl.Value))
	}
	historical := stopped
	nextGen := openPlain(t, svc, "unit.heat", "asset:heat", "source:primary", "epoch:heat", "2")
	commit(t, nextGen, func(b *v1.PublicationBatch) {
		b.GenerationFences = []v1.GenerationFence{{
			SourceID: primary.spec.Source, SourceEpochID: primary.spec.Epoch, DriverGeneration: "1",
			Reason: "lifecycle.driver_replaced", Evidence: []v1.EvidenceRef{testEvidence("fence:primary")}, Revision: "1",
		}}
	}, nil)
	if err := commitErr(nextGen, func(b *v1.PublicationBatch) {
		b.FactWithdrawals = []v1.CandidateID{"candidate:missing"}
	}, nil); err == nil {
		t.Fatal("an unknown withdrawal was accepted")
	}
	if coefficientOf(assetSnap(t, svc, "asset:heat"), "candidate:primary") == "" || coefficientOf(assetSnap(t, svc, "asset:other"), "candidate:primary") != "99999" {
		t.Fatal("the rejected withdrawal changed a member")
	}
	commit(t, nextGen, func(b *v1.PublicationBatch) {
		b.FactWithdrawals = []v1.CandidateID{"candidate:primary"}
	}, nil)
	cleared, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	clearedEl := svc.Element(primarySource.For, cleared)
	if sourceState(clearedEl, "path.primary").Disposition != sources.DispositionWithdrawn || clearedEl.Value != nil {
		t.Fatalf("withdrawal %+v value %s", sourceState(clearedEl, "path.primary").Disposition, valueCoefficient(clearedEl.Value))
	}
	if sourceState(clearedEl, "path.mirror").Disposition != sources.DispositionCurrent || valueCoefficient(sourceState(clearedEl, "path.mirror").Latest.Candidate.Value) != "22222" {
		t.Fatal("the fresh mirror was dropped or substituted")
	}
	if !sourceListed(cleared, "path.primary", 40) || coefficientOf(assetSnap(t, svc, "asset:other"), "candidate:primary") != "99999" {
		t.Fatal("the declaration or the other owner changed")
	}
	held, err := svc.Lookup(context.Background(), historical.Ref)
	if err != nil || valueCoefficient(svc.Element(primarySource.For, held).Value) != "32165" {
		t.Fatalf("historical view %v", err)
	}
	nextGen.Fence()
	afterStop, err := svc.CaptureView(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if valueCoefficient(svc.Element(primarySource.For, afterStop).Value) != "" || sourceState(svc.Element(primarySource.For, afterStop), "path.primary").Disposition == sources.DispositionCurrent {
		t.Fatal("stopping the generation revived the cleared basis")
	}
}

func TestPublishDependencyClosureAndExplicitWithdrawal(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "dependency closure"
		if explicit {
			name = "explicit withdrawal"
		}
		t.Run(name, func(t *testing.T) {
			clock := newTestClock()
			svc := openService(t, clock)
			node := memberNode(t, svc, "unit.heat", "slot.heat")
			otherNode := memberNode(t, svc, "unit.other", "slot.other")
			inputS := openPlain(t, svc, "unit.input", "asset:heat", "source:input", "epoch:input", "1")
			derivedS := openPlain(t, svc, "unit.derived", "asset:heat", "source:derived", "epoch:derived", "1")
			mirrorS := openPlain(t, svc, "unit.mirror", "asset:heat", "source:mirror", "epoch:mirror", "1")
			other := openPlain(t, svc, "unit.other", "asset:other", "source:other", "epoch:other", "1")
			inputFact := keepFresh(observedFact(inputS.spec, "candidate:input", "binding:input", "1", "32165", clock.Now(), 100))
			derivedFact := inferredFact("candidate:derived", "thermal.measurement.derived.heat", "45001", inputFact, clock.Now())
			mirrorFact := keepFresh(observedFact(mirrorS.spec, "candidate:mirror", "binding:mirror", "1", "11111", clock.Now(), 100))
			mirrorFact.Key = derivedFact.Key
			otherFact := retitle(keepFresh(observedFact(other.spec, "candidate:derived", "binding:other", "1", "99999", clock.Now(), 100)), "thermal.measurement.derived.heat", "candidate:derived")
			inputSource := declareFact(node, node, "path.input", 20, inputFact)
			derivedSource := declareFact(node, node, "path.derived", 40, derivedFact)
			mirrorSource := declareFact(node, node, "path.mirror", 10, mirrorFact)
			publishFact(t, inputS, "binding:input", inputSource, inputFact)
			publishInferred(t, derivedS, derivedSource, derivedFact)
			publishFact(t, mirrorS, "binding:mirror", mirrorSource, mirrorFact)
			publishFact(t, other, "binding:other", declareFact(otherNode, otherNode, "path.other", 30, otherFact), otherFact)
			original, err := svc.CaptureView(context.Background(), driver.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			originalEl := svc.Element(derivedSource.For, original)
			if valueCoefficient(originalEl.Value) != "45001" || len(originalEl.Resolution.Sources) != 2 {
				t.Fatalf("original derived view %s sources %d", valueCoefficient(originalEl.Value), len(originalEl.Resolution.Sources))
			}
			revised := inputFact
			revised.Revision = "2"
			revisedValue := kelvinQuantity("40001", -2)
			revised.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: &revisedValue}
			commit(t, inputS, func(b *v1.PublicationBatch) {
				b.FactUpserts = []v1.FactCandidate{revised}
				if explicit {
					b.FactWithdrawals = []v1.CandidateID{"candidate:derived"}
				}
			}, func(tx *transaction) {
				if err := tx.StageSample(driver.Sample{From: inputSource, Candidate: revised}); err != nil {
					t.Fatal(err)
				}
			})
			next, err := svc.CaptureView(context.Background(), driver.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			heat := assetSnap(t, svc, "asset:heat")
			if snapshotHas(heat, "candidate:derived") || coefficientOf(heat, "candidate:input") != "40001" || coefficientOf(heat, "candidate:mirror") != "11111" {
				t.Fatalf("surviving facts derived %v input %s mirror %s", snapshotHas(heat, "candidate:derived"), coefficientOf(heat, "candidate:input"), coefficientOf(heat, "candidate:mirror"))
			}
			el := svc.Element(derivedSource.For, next)
			want := sources.DispositionDependencyLost
			if explicit {
				want = sources.DispositionWithdrawn
			}
			if sourceState(el, "path.derived").Disposition != want || el.Value != nil || !sourceListed(next, "path.derived", 40) || !sourceListed(next, "path.mirror", 10) {
				t.Fatalf("disposition %s value %s", sourceState(el, "path.derived").Disposition, valueCoefficient(el.Value))
			}
			if coefficientOf(assetSnap(t, svc, "asset:other"), "candidate:derived") != "99999" {
				t.Fatal("the independent member changed")
			}
			held, err := svc.Lookup(context.Background(), original.Ref)
			if err != nil || valueCoefficient(svc.Element(derivedSource.For, held).Value) != "45001" {
				t.Fatalf("historical derived view %v", err)
			}
			derivedS.Fence()
			stopped, err := svc.CaptureView(context.Background(), driver.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			if valueCoefficient(svc.Element(derivedSource.For, stopped).Value) != "" {
				t.Fatal("the stop revived the derived value")
			}
		})
	}
}

func TestPublishCrossAssetFrozenLineage(t *testing.T) {
	clock := newTestClock()
	svc := openService(t, clock)
	leftNode := memberNode(t, svc, "unit.left", "slot.left")
	rightNode := memberNode(t, svc, "unit.right", "slot.right")
	totalNode := memberNode(t, svc, "unit.total", "slot.total")
	left := openPlain(t, svc, "unit.left", "asset:left", "source:left", "epoch:left", "1")
	right := openPlain(t, svc, "unit.right", "asset:right", "source:right", "epoch:right", "1")
	total := openScoped(t, svc, "unit.total", "asset:total", "source:total", "epoch:total", "1", "asset:left", "asset:right", "asset:total")
	leftFact := keepFresh(observedFact(left.spec, "candidate:left", "binding:left", "1", "32165", clock.Now(), 100))
	rightFact := keepFresh(observedFact(right.spec, "candidate:right", "binding:right", "1", "101", clock.Now(), 100))
	stale := keepFresh(retitle(observedFact(left.spec, "candidate:stale", "binding:left", "1", "501", clock.Now(), 100), "thermal.measurement.stale.heat", "candidate:stale"))
	stale.FreshnessPolicy.FreshForNS = "50"
	withdrawn := keepFresh(retitle(observedFact(left.spec, "candidate:withdrawn", "binding:left", "1", "701", clock.Now(), 100), "thermal.measurement.withdrawn.heat", "candidate:withdrawn"))
	withdrawn.Quality.Availability = v1.AvailabilityWithdrawn
	withdrawn.Value = nil
	publishFact(t, left, "binding:left", declareFact(leftNode, leftNode, "path.left", 30, leftFact), leftFact)
	publishFact(t, right, "binding:right", declareFact(rightNode, rightNode, "path.right", 30, rightFact), rightFact)
	commit(t, left, func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{stale} }, func(tx *transaction) {
		source := declareFact(leftNode, leftNode, "path.stale", 10, stale)
		if err := tx.DeclareSource(source); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageSample(driver.Sample{From: source, Candidate: stale}); err != nil {
			t.Fatal(err)
		}
	})
	commit(t, left, func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{withdrawn} }, func(tx *transaction) {
		source := declareFact(leftNode, leftNode, "path.withdrawn", 10, withdrawn)
		if err := tx.DeclareSource(source); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageSample(driver.Sample{From: source, Candidate: withdrawn}); err != nil {
			t.Fatal(err)
		}
	})
	cut, err := total.Input().Capture(context.Background(), driver.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	pin := cut.Ref
	if _, _, ok := currentOf(svc, "asset:total"); ok {
		t.Fatal("the total asset has a snapshot before its publication")
	}
	cycle := driver.InputRef{At: pin, Subject: totalNode, Candidate: "candidate:cycle", Revision: "1"}
	err = commitFormula(t, total, nil, cycle, &driver.Formula{Algorithm: algorithmTotal(), Inputs: []driver.InputRef{cycle}})
	requireIdent(t, err, v1.DerivationCycle)
	if _, _, ok := currentOf(svc, "asset:total"); ok {
		t.Fatal("a cycle minted a snapshot")
	}
	revised := leftFact
	revised.Revision = "2"
	revisedValue := kelvinQuantity("40001", -2)
	revised.Value = &v1.Value{Kind: v1.ValueQuantity, Quantity: &revisedValue}
	commit(t, left, func(b *v1.PublicationBatch) { b.FactUpserts = []v1.FactCandidate{revised} }, func(tx *transaction) {
		if err := tx.StageSample(driver.Sample{From: declareFact(leftNode, leftNode, "path.left", 30, revised), Candidate: revised}); err != nil {
			t.Fatal(err)
		}
	})
	totalFact := keepFresh(observedFact(total.spec, "candidate:total", "binding:total", "1", "32265", clock.Now(), 100))
	totalSource := declareFact(totalNode, totalNode, "path.total", 30, totalFact)
	leftRef := driver.InputRef{At: pin, Subject: leftNode, Candidate: "candidate:left", Revision: "1"}
	rightRef := driver.InputRef{At: pin, Subject: rightNode, Candidate: "candidate:right", Revision: "1"}
	self := driver.InputRef{At: pin, Subject: totalNode, Candidate: "candidate:total", Revision: "1"}
	commit(t, total, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(total.spec, "registry:total")}
		b.BindingUpserts = []v1.NativeBinding{bindingOf(total.spec, "binding:total", "native-resource:binding:total")}
		b.IdentityLinkUpserts = []v1.IdentityLink{linkOf(total.spec, "binding:total", "basis:total", nil)}
		b.FactUpserts = []v1.FactCandidate{totalFact}
	}, func(tx *transaction) {
		if err := tx.DeclareSource(totalSource); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageSample(driver.Sample{From: totalSource, Candidate: totalFact}); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageFormula(self, derivedOrigin(), &driver.Formula{Algorithm: algorithmTotal(), Inputs: []driver.InputRef{leftRef, rightRef}}); err != nil {
			t.Fatal(err)
		}
	})
	lines := total.Lineages()
	if len(lines) != 1 || len(lines[0].Inputs) != 2 || !lines[0].Inputs[0].Found || !lines[0].Inputs[1].Found {
		t.Fatalf("lineage %+v", lines)
	}
	if lines[0].Inputs[0].Input.Candidate.Revision != "1" || valueCoefficient(lines[0].Inputs[0].Input.Candidate.Value) != "32165" {
		t.Fatal("the left operand did not stay at its frozen revision")
	}
	if lines[0].Inputs[1].Input.Candidate.Revision != "1" || valueCoefficient(lines[0].Inputs[1].Input.Candidate.Value) != "101" {
		t.Fatal("the right operand did not stay at its frozen revision")
	}
	totalSnap := assetSnap(t, svc, "asset:total")
	if coefficientOf(totalSnap, "candidate:total") != "32265" || snapshotHas(totalSnap, "candidate:left") || snapshotHas(totalSnap, "candidate:right") {
		t.Fatal("the total snapshot shadowed an operand")
	}
	if coefficientOf(assetSnap(t, svc, "asset:left"), "candidate:left") != "40001" {
		t.Fatal("the live left candidate did not advance")
	}
	assertUnresolved(t, total, pin, leftNode, "candidate:missing", "1")
	staleLine := commitNamed(t, total, pin, leftNode, "candidate:stale", "1")
	if !named(staleLine, "candidate:stale") || staleLine.Freshness != v1.FreshnessStale {
		t.Fatalf("stale %+v", staleLine)
	}
	withdrawnLine := commitNamed(t, total, pin, leftNode, "candidate:withdrawn", "1")
	if !named(withdrawnLine, "candidate:withdrawn") || withdrawnLine.Freshness != v1.FreshnessUnknown {
		t.Fatalf("withdrawn %+v", withdrawnLine)
	}
	narrow := openScoped(t, svc, "unit.narrow", "asset:narrow", "source:narrow", "epoch:narrow", "1", "asset:narrow")
	commit(t, narrow, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(narrow.spec, "registry:narrow")}
	}, nil)
	narrow.Pin(pin)
	outside := commitNamed(t, narrow, pin, leftNode, "candidate:left", "1")
	if outside.Inputs[0].Found {
		t.Fatal("an outside-scope operand was readable")
	}
	if _, err := svc.CaptureView(context.Background(), driver.Filter{}); err != nil {
		t.Fatal(err)
	}
	clock.advance(graph.CutRetention)
	expired := commitNamed(t, total, pin, leftNode, "candidate:left", "1")
	if expired.Inputs[0].Found {
		t.Fatal("an expired cut still supplied the operand")
	}
}

func operatorDecl(native v1.EvidenceRef, resource v1.ResourceID) identity.OperatorLink {
	basis := testEvidence("audit:" + string(resource))
	basis.Owner = identity.OperatorEvidenceOwner
	return identity.OperatorLink{Native: native, Resource: resource, Basis: []v1.EvidenceRef{basis}}
}

func operatorPending(t *testing.T, session *Session, upserts []v1.NativeBinding) []v1.IdentityLink {
	t.Helper()
	tx, err := session.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	links := tx.(*transaction).OperatorLinks(upserts)
	if err := tx.Abort(context.Background()); err != nil {
		t.Fatal(err)
	}
	return links
}

func carryOperator(t *testing.T, session *Session, upserts []v1.NativeBinding, links []v1.IdentityLink) {
	t.Helper()
	tx, err := session.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	batch, err := session.Prepare(func(b *v1.PublicationBatch) {
		b.BindingUpserts = append([]v1.NativeBinding{}, upserts...)
		b.IdentityLinkUpserts = append([]v1.IdentityLink{}, links...)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.(*transaction).Stage(batch); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func resourceOf(snap v1.Snapshot, id v1.NativeBindingID) string {
	for _, link := range snap.IdentityLinks {
		if link.BindingID == id && link.Resource != nil && link.State == v1.LinkQualified {
			return string(*link.Resource)
		}
	}
	return ""
}

func linkState(snap v1.Snapshot, id v1.NativeBindingID) v1.LinkState {
	for _, link := range snap.IdentityLinks {
		if link.BindingID == id {
			return link.State
		}
	}
	return ""
}

func currentOf(svc *Service, asset v1.AssetID) (v1.Snapshot, []byte, bool) {
	svc.mu.Lock()
	kernel := svc.kernels[asset]
	svc.mu.Unlock()
	if kernel == nil {
		return v1.Snapshot{}, nil, false
	}
	return kernel.Current()
}

func assetSnap(t *testing.T, svc *Service, asset v1.AssetID) v1.Snapshot {
	t.Helper()
	svc.mu.Lock()
	kernel := svc.kernels[asset]
	svc.mu.Unlock()
	if kernel == nil {
		t.Fatalf("no kernel %s", asset)
	}
	snap, _, ok := kernel.Current()
	if !ok {
		t.Fatalf("no snapshot %s", asset)
	}
	return snap
}

func publishWithSerial(t *testing.T, session *Session, binding string, source driver.Source, fact v1.FactCandidate, claim driver.ManufacturerSerialClaim) {
	t.Helper()
	commit(t, session, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(session.spec, "registry:"+string(session.spec.Source))}
		b.BindingUpserts = []v1.NativeBinding{bindingOf(session.spec, binding, "native-resource:"+binding)}
		b.IdentityLinkUpserts = []v1.IdentityLink{linkOf(session.spec, binding, "basis:"+binding, nil)}
		b.FactUpserts = []v1.FactCandidate{fact}
	}, func(tx *transaction) {
		if err := tx.DeclareSource(source); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageSample(driver.Sample{From: source, Candidate: fact}); err != nil {
			t.Fatal(err)
		}
		if err := tx.AdmitManufacturerSerial(claim); err != nil {
			t.Fatal(err)
		}
	})
}

func sameClass(res identity.Resolution, a, b driver.NodeRef) bool {
	for _, class := range res.Resources {
		hasA, hasB := false, false
		for _, member := range class.Members {
			if member.Ref == a {
				hasA = true
			}
			if member.Ref == b {
				hasB = true
			}
		}
		if hasA && hasB {
			return true
		}
	}
	return false
}

func valueCoefficient(v *v1.Value) string {
	if v == nil || v.Quantity == nil {
		return ""
	}
	return v.Quantity.Number.Coefficient
}

func receiptNS(svc *Service, view View, source driver.Source) string {
	el := svc.Element(source.For, view)
	for _, state := range el.States {
		if state.Source.PathKey == source.PathKey && state.Latest != nil {
			return string(state.Latest.Receipt.At.Nanoseconds)
		}
	}
	return ""
}

func sourceState(el sources.ElementState, path string) sources.SourceState {
	for _, state := range el.States {
		if state.Source.PathKey == path {
			return state
		}
	}
	return sources.SourceState{}
}

func sourceListed(view View, path string, priority int32) bool {
	for _, source := range view.Sources {
		if source.PathKey == path && source.Priority == priority {
			return true
		}
	}
	return false
}

func snapshotHas(snap v1.Snapshot, id v1.CandidateID) bool {
	for _, envelope := range snap.Facts {
		for _, candidate := range envelope.Candidates {
			if candidate.CandidateID == id {
				return true
			}
		}
	}
	for _, record := range snap.Retained {
		if record.Candidate.CandidateID == id {
			return true
		}
	}
	return false
}

func inferredFact(id, factID, coefficient string, input v1.FactCandidate, at time.Time) v1.FactCandidate {
	value := kelvinQuantity(coefficient, -2)
	return v1.FactCandidate{
		CandidateID: v1.CandidateID(id), Key: v1.FactKey{PackID: labPack.ID, PackVersion: labPack.Version, FactID: v1.DefinitionID(factID), Dimensions: []v1.Dimension{}},
		Value:           &v1.Value{Kind: v1.ValueQuantity, Quantity: &value},
		Quality:         v1.Quality{Assertion: v1.AssertionInferred, Qualification: v1.QualificationQualified, Promotion: v1.PromotionPromoted, Validity: v1.ValidityGood, Availability: v1.AvailabilityAvailable, Freshness: v1.FreshnessFresh, Reasons: []v1.DefinitionID{}},
		Times:           v1.Times{ReceivedAt: wallPoint(at), ReceiptMonotonic: monoNS(100), EvaluatedAt: wallPoint(at), EvaluateMonotonic: monoNS(100)},
		FreshnessPolicy: v1.FreshnessPolicy{PolicyID: "policy:a", Version: "1.0.0", FreshForNS: "5000", RetainForNS: "9000", MaxWallUncertaintyNS: "1"},
		Origin:          v1.OriginRef{OriginID: v1.OriginID("origin:" + id), Kind: v1.OriginDerived, Evidence: []v1.EvidenceRef{testEvidence("origin:" + id)}},
		Evidence:        []v1.EvidenceRef{testEvidence("native:" + id)}, Revision: "1",
		Derivation: &v1.Derivation{Algorithm: "algorithm.total", Version: "1.0.0", Evidence: []v1.EvidenceRef{}, Inputs: []v1.DerivationInput{{
			CandidateID: input.CandidateID, CandidateRevision: input.Revision,
			SourcePaths: []v1.SourcePathRef{{BindingID: *input.BindingID, SourceID: *input.Origin.SourceID, SourceEpochID: *input.SourceEpochID, DriverGeneration: *input.DriverGeneration}},
		}}},
	}
}

func publishInferred(t *testing.T, session *Session, source driver.Source, fact v1.FactCandidate) {
	t.Helper()
	commit(t, session, func(b *v1.PublicationBatch) {
		b.SourceUpserts = []v1.SourceDescriptor{descriptorOf(session.spec, "registry:"+string(session.spec.Source))}
		b.FactUpserts = []v1.FactCandidate{fact}
	}, func(tx *transaction) {
		if err := tx.DeclareSource(source); err != nil {
			t.Fatal(err)
		}
		if err := tx.StageSample(driver.Sample{From: source, Candidate: fact}); err != nil {
			t.Fatal(err)
		}
	})
}

func openScoped(t *testing.T, svc *Service, instance, asset, source, epoch, generation string, assets ...v1.AssetID) *Session {
	t.Helper()
	session, err := svc.OpenSession(SessionSpec{
		Driver: testKind, Instance: driver.InstanceKey(instance), Asset: v1.AssetID(asset), Source: v1.SourceID(source),
		Epoch: v1.SourceEpochID(epoch), Generation: v1.Uint64(generation), Validators: []v1.PackValidator{labValidator{}},
		Scope: driver.Filter{Assets: assets},
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func algorithmTotal() v1.DefinitionRef {
	return v1.DefinitionRef{Pack: labPack, ID: "algorithm.total", Version: "1.0.0"}
}

func derivedOrigin() driver.Origin {
	return driver.Origin{Kind: driver.OriginDerived, Evidence: []v1.EvidenceRef{testEvidence("formula:total")}}
}

func commitFormula(t *testing.T, session *Session, fact *v1.FactCandidate, self driver.InputRef, formula *driver.Formula) error {
	t.Helper()
	return commitErr(session, func(b *v1.PublicationBatch) {
		if fact != nil {
			b.FactUpserts = []v1.FactCandidate{*fact}
		}
	}, func(tx *transaction) {
		if err := tx.StageFormula(self, derivedOrigin(), formula); err != nil {
			t.Error(err)
		}
	})
}

func commitNamed(t *testing.T, session *Session, pin graph.CutRef, subject driver.NodeRef, candidate, revision string) sources.Lineage {
	t.Helper()
	ref := driver.InputRef{At: pin, Subject: subject, Candidate: v1.CandidateID(candidate), Revision: v1.Uint64(revision)}
	self := driver.InputRef{At: pin, Subject: subject, Candidate: "candidate:reader", Revision: "1"}
	if err := commitFormula(t, session, nil, self, &driver.Formula{Algorithm: algorithmTotal(), Inputs: []driver.InputRef{ref}}); err != nil {
		t.Fatal(err)
	}
	lines := session.Lineages()
	if len(lines) != 1 {
		t.Fatalf("lineage %+v", lines)
	}
	return lines[0]
}

func named(line sources.Lineage, candidate string) bool {
	for _, input := range line.Inputs {
		if string(input.Ref.Candidate) == candidate && input.Found {
			return true
		}
	}
	for _, input := range line.Named {
		if string(input.Candidate) == candidate {
			return true
		}
	}
	return false
}

func assertUnresolved(t *testing.T, session *Session, pin graph.CutRef, subject driver.NodeRef, candidate, revision string) {
	t.Helper()
	line := commitNamed(t, session, pin, subject, candidate, revision)
	if line.Inputs[0].Found || line.Freshness != v1.FreshnessUnknown {
		t.Fatalf("missing operand %+v", line)
	}
}
