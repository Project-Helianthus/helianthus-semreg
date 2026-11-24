package identity

import (
	"errors"
	"reflect"
	"sort"
	"strconv"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// OperatorEvidenceOwner owns the audit evidence of an operator declaration. An
// identity link whose basis holds a reference of this owner is an operator
// identity declaration.
const OperatorEvidenceOwner semreg.DefinitionID = "helianthus.gateway.configuration"

// ErrOperatorLink refuses a batch whose operator identity links differ from the
// links pending for its source. A carried set that is exactly the pending
// withdrawals is accepted: those withdrawals commit, and a qualified
// declaration that is still pending stays unpublished.
var ErrOperatorLink = errors.New("identity: operator identity links differ from the pending declarations")

// OperatorLink is one operator identity declaration: the binding of the
// declaring source that observes the native resource Native observes Resource.
// A declaration names the native resource, not a binding identifier, so it
// survives a generation fence: the successor's binding of the same native
// resource carries it. Basis holds the declaration's audit evidence, at least
// one reference of OperatorEvidenceOwner; Key is the optional identity-object
// digest of the unit's native token seen through the binding. The association
// is a typed identity link from the binding to the resource. It is no
// certified join and adds no identity merge or route authority.
type OperatorLink struct {
	Native   semreg.EvidenceRef
	Resource semreg.ResourceID
	Key      *semreg.Digest
	Basis    []semreg.EvidenceRef
}

// OperatorBasis reports whether basis holds operator declaration evidence.
func OperatorBasis(basis []semreg.EvidenceRef) bool {
	for _, ref := range basis {
		if ref.Owner == OperatorEvidenceOwner {
			return true
		}
	}
	return false
}

// PendingOperatorLinks returns the operator links still to be published by
// (asset, source, epoch, generation) from the declared set, the asset's current
// snapshot and the batch's binding upserts: one link per binding of that source,
// epoch and generation that a declaration names, or whose published operator
// link is no longer declared (a withdrawal). The bindings are the upserts plus
// the bindings the snapshot already holds current for the generation. A binding
// whose link is already withdrawn takes no link again; its successor binding
// does. Links are ordered by binding. present says whether snapshot is the
// asset's committed snapshot.
func PendingOperatorLinks(declared []OperatorLink, snapshot semreg.Snapshot, present bool, id semreg.AssetID, src semreg.SourceID, epoch semreg.SourceEpochID, generation semreg.Uint64, upserts []semreg.NativeBinding) []semreg.IdentityLink {
	current := make(map[semreg.NativeBindingID]semreg.NativeBinding)
	links := make(map[semreg.NativeBindingID]semreg.IdentityLink)
	own := func(binding semreg.NativeBinding) bool {
		return binding.AssetID == id && binding.SourceID == src && binding.SourceEpochID == epoch && binding.DriverGeneration == generation && binding.State == semreg.BindingCurrent
	}
	if present {
		for _, binding := range snapshot.Bindings {
			if own(binding) {
				current[binding.BindingID] = binding
			}
		}
		for _, link := range snapshot.IdentityLinks {
			links[link.BindingID] = link
		}
	}
	for _, binding := range upserts {
		if own(binding) {
			current[binding.BindingID] = binding
		}
	}
	byNative := make(map[semreg.EvidenceRef]OperatorLink, len(declared))
	for _, link := range declared {
		byNative[link.Native] = link
	}
	out := []semreg.IdentityLink{}
	for bindingID, binding := range current {
		existing, linked := links[bindingID]
		if linked && existing.State == semreg.LinkWithdrawn {
			continue
		}
		declaration, ok := byNative[binding.NativeResource]
		if !ok {
			if linked && OperatorBasis(existing.Basis) {
				existing.State, existing.Revision = semreg.LinkWithdrawn, incrementUint(existing.Revision)
				out = append(out, existing)
			}
			continue
		}
		resource := declaration.Resource
		desired := semreg.IdentityLink{AssetID: id, BindingID: bindingID, State: semreg.LinkQualified, Basis: append([]semreg.EvidenceRef{}, declaration.Basis...), Revision: "1", Resource: &resource}
		if declaration.Key != nil {
			key := *declaration.Key
			desired.Key = &key
		}
		if linked {
			desired.Revision = existing.Revision
			if reflect.DeepEqual(existing, desired) {
				continue
			}
			desired.Revision = incrementUint(existing.Revision)
		}
		out = append(out, desired)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BindingID < out[j].BindingID })
	return out
}

// CarryOperatorLinks appends the pending links to the links the publisher
// already built, by binding: a pending link replaces the publisher's link of
// the same binding, so the result holds one entry per binding, and the links of
// other bindings are untouched. An empty pending set returns the built links
// unchanged. The result is ordered by binding.
func CarryOperatorLinks(built, pending []semreg.IdentityLink) []semreg.IdentityLink {
	return carry(built, pending)
}

// CarryOperatorWithdrawals appends only the withdrawals of the pending set, all
// of them, by binding as CarryOperatorLinks does. A qualified association that
// is pending beside them stays pending.
func CarryOperatorWithdrawals(built, pending []semreg.IdentityLink) []semreg.IdentityLink {
	return carry(built, operatorWithdrawals(pending))
}

func carry(built, pending []semreg.IdentityLink) []semreg.IdentityLink {
	if len(pending) == 0 {
		return built
	}
	replaced := make(map[semreg.NativeBindingID]bool, len(pending))
	for _, link := range pending {
		replaced[link.BindingID] = true
	}
	out := make([]semreg.IdentityLink, 0, len(built)+len(pending))
	for _, link := range built {
		if !replaced[link.BindingID] {
			out = append(out, link)
		}
	}
	out = append(out, pending...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].BindingID < out[j].BindingID })
	return out
}

// CheckOperatorLinks compares the operator links a batch carries with the links
// pending for its source. A link of the batch is an operator link when its basis
// holds operator evidence or when it replaces a published operator link of the
// snapshot that is not withdrawn; present says whether snapshot is the asset's
// committed snapshot. A batch that carries no operator link is accepted and
// leaves the pending links pending. A carried set that is exactly the pending set
// is accepted, and so is a carried set of withdrawals that is exactly the
// pending withdrawals. The operator links are ordered by binding, as the pending
// set is.
func CheckOperatorLinks(batch []semreg.IdentityLink, snapshot semreg.Snapshot, present bool, pending []semreg.IdentityLink) error {
	published := make(map[semreg.NativeBindingID]bool)
	if present {
		for _, link := range snapshot.IdentityLinks {
			published[link.BindingID] = link.State != semreg.LinkWithdrawn && OperatorBasis(link.Basis)
		}
	}
	carried := []semreg.IdentityLink{}
	for _, link := range batch {
		if OperatorBasis(link.Basis) || published[link.BindingID] {
			carried = append(carried, link)
		}
	}
	if len(carried) == 0 {
		return nil
	}
	sort.Slice(carried, func(i, j int) bool { return carried[i].BindingID < carried[j].BindingID })
	if reflect.DeepEqual(carried, pending) {
		return nil
	}
	if operatorWithdrawalsOnly(carried) && reflect.DeepEqual(carried, operatorWithdrawals(pending)) {
		return nil
	}
	return ErrOperatorLink
}

func operatorWithdrawalsOnly(links []semreg.IdentityLink) bool {
	if len(links) == 0 {
		return false
	}
	for _, link := range links {
		if link.State != semreg.LinkWithdrawn {
			return false
		}
	}
	return true
}

func operatorWithdrawals(links []semreg.IdentityLink) []semreg.IdentityLink {
	out := make([]semreg.IdentityLink, 0, len(links))
	for _, link := range links {
		if link.State == semreg.LinkWithdrawn {
			out = append(out, link)
		}
	}
	return out
}

func incrementUint(value semreg.Uint64) semreg.Uint64 {
	n, err := strconv.ParseUint(string(value), 10, 64)
	if err != nil || n == ^uint64(0) {
		return ""
	}
	return semreg.Uint64(strconv.FormatUint(n+1, 10))
}
