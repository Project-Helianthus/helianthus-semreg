package host

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestWalkEmitsRetainedKeysAndDetachedClaims(t *testing.T) {
	ctx := context.Background()
	clock := newFixedClock(baseTime.Add(time.Second))
	site := openStarted(t, baseConfig(t, clock), newSynthFactory(clock))
	defer site.Close(ctx)
	cut, err := site.Cut(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cut.Close()

	links := func() []identity.Link {
		return []identity.Link{
			{
				Key: driver.LinkKey{
					Type: v1.DefinitionRef{ID: "relation.alpha", Version: "1.0.0", Pack: v1.PackRef{ID: "pack.alpha", Version: "1.1.0"}},
					From: driver.NodeRef{Bridge: "bridge.a", ID: "resource:a"}, To: driver.NodeRef{Bridge: "bridge.b", ID: "resource:b"},
				},
				Claims: []identity.LinkClaimView{{
					Publisher: identity.Publisher{Driver: "fixture.first", Instance: "first.one"}, Basis: []v1.EvidenceRef{evidenceOf("first claim")},
					Declared: driver.LinkKey{
						Type: v1.DefinitionRef{ID: "relation.alpha", Version: "1.0.0", Pack: v1.PackRef{ID: "pack.alpha", Version: "1.1.0"}},
						From: driver.NodeRef{Bridge: "bridge.old", ID: "resource:old"}, To: driver.NodeRef{Bridge: "bridge.b", ID: "resource:b"},
					},
				}},
			},
			{
				Key: driver.LinkKey{
					Type: v1.DefinitionRef{ID: "relation.beta", Version: "2.0.0", Pack: v1.PackRef{ID: "pack.beta", Version: "2.1.0"}},
					From: driver.NodeRef{Bridge: "bridge.c", ID: "resource:c"}, To: driver.NodeRef{Bridge: "bridge.d", ID: "resource:d"},
				},
				Claims: []identity.LinkClaimView{{
					Publisher: identity.Publisher{Driver: "fixture.second", Instance: "second.one"}, Basis: []v1.EvidenceRef{evidenceOf("second claim")},
					Declared: driver.LinkKey{
						Type: v1.DefinitionRef{ID: "relation.beta", Version: "2.0.0", Pack: v1.PackRef{ID: "pack.beta", Version: "2.1.0"}},
						From: driver.NodeRef{Bridge: "bridge.c", ID: "resource:c"}, To: driver.NodeRef{Bridge: "bridge.predecessor", ID: "resource:predecessor"},
					},
				}},
			},
		}
	}
	// Supply retained links at the Cut boundary; the runtime tests exercise
	// the ledger's capture of declared and rebound keys.
	cut.cut.Links = links()
	var visited []identity.Link
	_, err = cut.Walk(ctx, AllVisible(2), func(entry WalkEntry) error {
		if link := entry.RetainedLink; link != nil {
			visited = append(visited, *link)
		}
		return nil
	})
	want := links()
	if err != nil || !reflect.DeepEqual(visited, want) {
		t.Fatalf("retained walk: %+v (%v), want %+v", visited, err, want)
	}
	visited[0].Claims[0].Basis[0] = evidenceOf("caller edit")
	visited[0].Claims[0].Declared = driver.LinkKey{}
	if !reflect.DeepEqual(cut.Links(), want) {
		t.Fatal("editing a walk entry mutated the captured links")
	}
}
