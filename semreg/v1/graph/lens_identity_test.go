package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// TestLensIdentityDigestFields varies every field lensIdentityOf copies into
// the page identity. The production mutation each field catches is lensIdentityOf
// dropping that field: the digest would stay the digest of the base request.
func TestLensIdentityDigestFields(t *testing.T) {
	ctx := semreg.EvaluationContext{
		EvaluatedAt:       semreg.TimePoint{UnixNanoseconds: "130", ClockID: "clock.utc", UncertaintyNS: "0"},
		EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock:cut", Nanoseconds: "20"},
	}
	catalog := identityDigest("lens-catalog-a")
	request := LensRequest{
		Lens: "lens.equipment_composition", Entry: EntryPoint{Kind: EntryLensRoots},
		Direction: DirectionBoth, Depth: 8, PageSize: 1,
	}
	base, err := semreg.DigestRecord(lensIdentityOf(request, ctx, catalog, nil))
	if err != nil {
		t.Fatal(err)
	}
	differ := func(name string, got lensIdentity) {
		t.Helper()
		digest, err := semreg.DigestRecord(got)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if digest == base {
			t.Fatalf("%s does not change the lens page identity", name)
		}
	}
	edited := request
	edited.Lens = "lens.part_composition"
	differ("lens", lensIdentityOf(edited, ctx, catalog, nil))
	edited = request
	edited.Entry = EntryPoint{Kind: EntrySites}
	differ("entry kind", lensIdentityOf(edited, ctx, catalog, nil))
	edited = request
	edited.Direction = DirectionOutgoing
	differ("direction", lensIdentityOf(edited, ctx, catalog, nil))
	edited = request
	edited.Depth = 2
	differ("depth", lensIdentityOf(edited, ctx, catalog, nil))
	edited = request
	edited.PageSize = 2
	differ("page size", lensIdentityOf(edited, ctx, catalog, nil))
	wall := ctx
	wall.EvaluatedAt.UnixNanoseconds = "140"
	differ("context wall", lensIdentityOf(request, wall, catalog, nil))
	clock := ctx
	clock.EvaluatedAt.ClockID = "clock.other"
	differ("context clock", lensIdentityOf(request, clock, catalog, nil))
	uncertainty := ctx
	uncertainty.EvaluatedAt.UncertaintyNS = "1"
	differ("context uncertainty", lensIdentityOf(request, uncertainty, catalog, nil))
	epoch := ctx
	epoch.EvaluateMonotonic.ClockEpochID = "clock:other"
	differ("context epoch", lensIdentityOf(request, epoch, catalog, nil))
	ticks := ctx
	ticks.EvaluateMonotonic.Nanoseconds = "21"
	differ("context ticks", lensIdentityOf(request, ticks, catalog, nil))
	included := []NotIncludedAsset{{AssetID: "asset:missing", Reason: NotIncludedExcluded}}
	differ("not-included asset", lensIdentityOf(request, ctx, catalog, included))
	otherReason := []NotIncludedAsset{{AssetID: "asset:missing", Reason: NotIncludedPublicationWithheld}}
	withAsset, err := semreg.DigestRecord(lensIdentityOf(request, ctx, catalog, included))
	if err != nil {
		t.Fatal(err)
	}
	withReason, err := semreg.DigestRecord(lensIdentityOf(request, ctx, catalog, otherReason))
	if err != nil {
		t.Fatal(err)
	}
	if withReason == withAsset {
		t.Fatal("not-included reason does not change the lens page identity")
	}
	differ("catalog", lensIdentityOf(request, ctx, identityDigest("lens-catalog-b"), nil))

	rooted := request
	rooted.Entry = EntryPoint{Kind: EntryResource, Resource: "resource:equip"}
	fromEquip, err := semreg.DigestRecord(lensIdentityOf(rooted, ctx, catalog, nil))
	if err != nil {
		t.Fatal(err)
	}
	rooted.Entry.Resource = "resource:part-a"
	fromPart, err := semreg.DigestRecord(lensIdentityOf(rooted, ctx, catalog, nil))
	if err != nil {
		t.Fatal(err)
	}
	if fromPart == fromEquip {
		t.Fatal("entry resource does not change the lens page identity")
	}
}

func identityDigest(label string) semreg.Digest {
	sum := sha256.Sum256([]byte(label))
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
