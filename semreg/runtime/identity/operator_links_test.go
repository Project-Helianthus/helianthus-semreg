package identity

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

const (
	linkAsset  semreg.AssetID         = "asset:site"
	linkSource semreg.SourceID        = "source:site"
	linkEpoch  semreg.SourceEpochID   = "epoch:site"
	linkGen    semreg.Uint64          = "1"
	selected   semreg.ResourceID      = "resource:selected"
	otherPick  semreg.ResourceID      = "resource:other-selected"
	bindingOne semreg.NativeBindingID = "binding:one"
	bindingTwo semreg.NativeBindingID = "binding:two"
)

func nativeRef(label string) semreg.EvidenceRef { return proof("native " + label)[0] }

func operatorEvidence(label string) []semreg.EvidenceRef {
	e := proof("operator declaration " + label)[0]
	e.Owner = OperatorEvidenceOwner
	return []semreg.EvidenceRef{e}
}

func bindingOf(id semreg.NativeBindingID, native string, generation semreg.Uint64) semreg.NativeBinding {
	return semreg.NativeBinding{BindingID: id, AssetID: linkAsset, SourceID: linkSource, SourceEpochID: linkEpoch, DriverGeneration: generation, NativeResource: nativeRef(native), State: semreg.BindingCurrent, Revision: "1"}
}

func publisherLink(id semreg.NativeBindingID, label string) semreg.IdentityLink {
	return semreg.IdentityLink{AssetID: linkAsset, BindingID: id, State: semreg.LinkQualified, Basis: proof("publisher " + label), Revision: "1"}
}

func declare(native string, resource semreg.ResourceID) OperatorLink {
	return OperatorLink{Native: nativeRef(native), Resource: resource, Basis: operatorEvidence(native)}
}

// kernel stands for the owner's committed state: the bindings and identity links
// its batches published.
type kernel struct {
	bindings []semreg.NativeBinding
	links    map[semreg.NativeBindingID]semreg.IdentityLink
}

func (k *kernel) snapshot() semreg.Snapshot {
	s := semreg.Snapshot{Bindings: k.bindings}
	for _, link := range k.links {
		s.IdentityLinks = append(s.IdentityLinks, link)
	}
	return s
}

func (k *kernel) pending(declared []OperatorLink, upserts ...semreg.NativeBinding) []semreg.IdentityLink {
	return PendingOperatorLinks(declared, k.snapshot(), true, linkAsset, linkSource, linkEpoch, linkGen, upserts)
}

// commit applies a batch: its binding upserts and its identity link upserts.
func (k *kernel) commit(upserts []semreg.NativeBinding, links []semreg.IdentityLink) {
	if k.links == nil {
		k.links = map[semreg.NativeBindingID]semreg.IdentityLink{}
	}
	for _, binding := range upserts {
		replaced := false
		for i := range k.bindings {
			if k.bindings[i].BindingID == binding.BindingID {
				k.bindings[i], replaced = binding, true
			}
		}
		if !replaced {
			k.bindings = append(k.bindings, binding)
		}
	}
	for _, link := range links {
		k.links[link.BindingID] = link
	}
}

func (k *kernel) resource(id semreg.NativeBindingID) *semreg.ResourceID {
	if link, ok := k.links[id]; ok && link.State == semreg.LinkQualified {
		return link.Resource
	}
	return nil
}

// An association declared after the binding was published and deliberately left
// unlinked appears only when the owning publisher's eligible next batch carries
// it; a batch that does not carry it leaves it pending. The association is an
// identity link of the binding: it adds no merge or route authority, and the
// ledger is untouched (J-BINDING-LINK).
func TestAssociationIsCarriedOnlyByTheOwnersNextBatch(t *testing.T) {
	ledger, _ := mixedLedger(t, NewMemoryStore())
	identityBefore, _ := ledger.Export()
	var k kernel
	binding := bindingOf(bindingOne, "unit", linkGen)
	k.commit([]semreg.NativeBinding{binding}, []semreg.IdentityLink{publisherLink(bindingOne, "own")})
	declared := []OperatorLink{declare("unit", selected)}

	pending := k.pending(declared)
	if len(pending) != 1 || pending[0].BindingID != bindingOne || pending[0].State != semreg.LinkQualified || pending[0].Resource == nil || *pending[0].Resource != selected || pending[0].Revision != "2" {
		t.Fatalf("pending = %+v", pending)
	}
	if err := pending[0].Validate(); err != nil {
		t.Fatalf("the carried link is invalid: %v", err)
	}
	// A batch that does not carry the declaration leaves it pending.
	k.commit(nil, []semreg.IdentityLink{publisherLink(bindingOne, "own again")})
	if resource := k.resource(bindingOne); resource != nil {
		t.Fatalf("a batch that did not carry the declaration published it: %v", *resource)
	}
	if got := k.pending(declared); len(got) != 1 {
		t.Fatalf("the declaration did not stay pending: %+v", got)
	}
	// The eligible batch carries it.
	built := []semreg.IdentityLink{publisherLink(bindingOne, "own again")}
	carried := CarryOperatorLinks(built, k.pending(declared))
	if err := CheckOperatorLinks(carried, k.pending(declared)); err != nil {
		t.Fatalf("the carried batch is refused: %v", err)
	}
	k.commit(nil, carried)
	if resource := k.resource(bindingOne); resource == nil || *resource != selected {
		t.Fatalf("the binding resolves to %v", resource)
	}
	if again := k.pending(declared); len(again) != 0 {
		t.Fatalf("pending after the carry = %+v", again)
	}

	// Withdraw it through the same batch path: the declaration is gone and the
	// published operator link is withdrawn by the next carry.
	withdrawals := k.pending(nil)
	if len(withdrawals) != 1 || withdrawals[0].State != semreg.LinkWithdrawn || withdrawals[0].BindingID != bindingOne {
		t.Fatalf("withdrawals = %+v", withdrawals)
	}
	k.commit(nil, CarryOperatorLinks(nil, withdrawals))
	if resource := k.resource(bindingOne); resource != nil || k.links[bindingOne].State != semreg.LinkWithdrawn {
		t.Fatalf("the association was not withdrawn: %+v", k.links[bindingOne])
	}

	if identityAfter, _ := ledger.Export(); !bytes.Equal(identityBefore, identityAfter) {
		t.Fatal("an association changed membership or routes")
	}
}

// An empty pending set preserves the publisher's built batch, other bindings'
// links survive a carry, and where the publisher already emitted a link for the
// same binding the batch holds one entry per binding, the pending one.
func TestCarryKeepsOneEntryPerBindingAndTheRestUntouched(t *testing.T) {
	built := []semreg.IdentityLink{publisherLink(bindingOne, "competing"), publisherLink(bindingTwo, "unrelated")}
	if got := CarryOperatorLinks(built, nil); !reflect.DeepEqual(got, built) {
		t.Fatalf("an empty pending set changed the batch: %+v", got)
	}
	resource := selected
	pending := []semreg.IdentityLink{{AssetID: linkAsset, BindingID: bindingOne, State: semreg.LinkQualified, Basis: operatorEvidence("unit"), Revision: "2", Resource: &resource}}
	carried := CarryOperatorLinks(built, pending)
	if len(carried) != 2 || carried[0].BindingID != bindingOne || carried[0].Resource == nil || *carried[0].Resource != selected || !reflect.DeepEqual(carried[1], built[1]) {
		t.Fatalf("carried = %+v", carried)
	}
	if !reflect.DeepEqual(built[0].Basis, publisherLink(bindingOne, "competing").Basis) {
		t.Fatal("the publisher's batch was changed in place")
	}
	seen := map[semreg.NativeBindingID]int{}
	for _, link := range carried {
		seen[link.BindingID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("binding %s has %d entries", id, n)
		}
	}
}

// A binding whose link was withdrawn takes no link again when the same native
// reference is declared again while that binding stays current; the successor
// binding of the same native resource carries the declaration.
func TestWithdrawnLinkStaysWithdrawnAndTheSuccessorCarriesIt(t *testing.T) {
	var k kernel
	k.commit([]semreg.NativeBinding{bindingOf(bindingOne, "unit", linkGen)}, nil)
	declared := []OperatorLink{declare("unit", selected)}
	k.commit(nil, CarryOperatorLinks(nil, k.pending(declared)))
	k.commit(nil, CarryOperatorLinks(nil, k.pending(nil)))
	if k.links[bindingOne].State != semreg.LinkWithdrawn {
		t.Fatalf("link = %+v", k.links[bindingOne])
	}
	if got := k.pending(declared); len(got) != 0 {
		t.Fatalf("a withdrawn binding took the link again: %+v", got)
	}
	successor := bindingOf(bindingTwo, "unit", linkGen)
	got := k.pending(declared, successor)
	if len(got) != 1 || got[0].BindingID != bindingTwo || got[0].Resource == nil || *got[0].Resource != selected {
		t.Fatalf("the successor did not carry the declaration: %+v", got)
	}
}

// A withdrawals-only carry succeeds and carries every pending withdrawal while a
// different qualified association stays pending; a qualified carry must match the
// whole pending set.
func TestWithdrawalsOnlyCarryWhileAnotherAssociationStaysPending(t *testing.T) {
	var k kernel
	k.commit([]semreg.NativeBinding{bindingOf(bindingOne, "first", linkGen), bindingOf(bindingTwo, "second", linkGen)}, nil)
	k.commit(nil, CarryOperatorLinks(nil, k.pending([]OperatorLink{declare("first", selected)})))
	// The first declaration is removed and a different association is declared for the second binding.
	declared := []OperatorLink{declare("second", otherPick)}
	pending := k.pending(declared)
	if len(pending) != 2 || pending[0].State != semreg.LinkWithdrawn || pending[1].State != semreg.LinkQualified {
		t.Fatalf("pending = %+v", pending)
	}
	withdrawals := CarryOperatorWithdrawals(nil, pending)
	if len(withdrawals) != 1 || withdrawals[0].BindingID != bindingOne {
		t.Fatalf("withdrawals = %+v", withdrawals)
	}
	if err := CheckOperatorLinks(withdrawals, pending); err != nil {
		t.Fatalf("a withdrawals-only batch was refused: %v", err)
	}
	if err := CheckOperatorLinks(pending, pending); err != nil {
		t.Fatalf("the whole pending set was refused: %v", err)
	}
	for name, carried := range map[string][]semreg.IdentityLink{
		"only the qualified link":            pending[1:],
		"a qualified link and no withdrawal": {pending[1]},
		"another set":                        {publisherLink(bindingOne, "x"), pending[1]},
	} {
		if err := CheckOperatorLinks(carried, pending); !errors.Is(err, ErrOperatorLink) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if err := CheckOperatorLinks(nil, pending); err != nil {
		t.Fatalf("a batch carrying no operator link: %v", err)
	}
	k.commit(nil, withdrawals)
	if k.links[bindingOne].State != semreg.LinkWithdrawn || k.resource(bindingTwo) != nil {
		t.Fatalf("links = %+v", k.links)
	}
	if left := k.pending(declared); len(left) != 1 || left[0].BindingID != bindingTwo || left[0].State != semreg.LinkQualified {
		t.Fatalf("the qualified association did not stay pending: %+v", left)
	}
}
