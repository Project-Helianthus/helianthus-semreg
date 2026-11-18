package graph

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// TestTraverseTokenFields varies every field of a page token.
// cut_id: boundCursor compares the token cut with the served cut, so a
// different cut id is revision_conflict.
// request: boundCursor compares the token request digest with this request,
// so a different digest is revision_conflict.
// cursor: tokenBody.Validate rejects a negative cursor as invalid_value, and
// boundCursor rejects a cursor past the result length as invalid_value.
func TestTraverseTokenFields(t *testing.T) {
	cutID := digestLabel("cut")
	request := digestLabel("request")
	token, err := mintPageToken(cutID, request, 2)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := boundCursor(token, cutID, request, 4)
	if err != nil || cursor != 2 {
		t.Fatalf("cursor = %d %v", cursor, err)
	}
	if _, err := boundCursor(token, digestLabel("other-cut"), request, 4); semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
		t.Fatalf("cut_id mutation = %v", err)
	}
	if _, err := boundCursor(token, cutID, digestLabel("other-request"), 4); semreg.ErrorIdentifier(err) != semreg.RevisionConflict {
		t.Fatalf("request mutation = %v", err)
	}
	if _, err := boundCursor(token, cutID, request, 1); semreg.ErrorIdentifier(err) != semreg.InvalidValue {
		t.Fatalf("cursor past the result = %v", err)
	}
	negative := `{"cursor":-1,"cut_id":"` + string(cutID) + `","request":"` + string(request) + `"}`
	if _, err := parsePageToken(pageTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(negative))); semreg.ErrorIdentifier(err) != semreg.InvalidValue {
		t.Fatalf("negative cursor = %v", err)
	}
	extra := `{"cursor":1,"cut_id":"` + string(cutID) + `","extra":true,"request":"` + string(request) + `"}`
	if _, err := parsePageToken(pageTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(extra))); semreg.ErrorIdentifier(err) != semreg.InvalidValue {
		t.Fatalf("extra field = %v", err)
	}
	if _, err := parsePageToken("nope"); semreg.ErrorIdentifier(err) != semreg.InvalidValue {
		t.Fatalf("prefix = %v", err)
	}
}

func TestExpiredPageTokensAreDropped(t *testing.T) {
	var now time.Time
	now = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	store := NewCutStore(func() time.Time { return now })
	id := digestLabel("retained-cut")
	held := &retainedCut{cut: &Cut{result: CutResult{CutID: id}}}
	store.newest = id
	store.cuts[id] = held

	const issued = 32
	issue := func(label string) {
		t.Helper()
		for i := 0; i < issued; i++ {
			token, err := mintPageToken(id, digestLabel(fmt.Sprintf("%s-%02d", label, i)), i)
			if err != nil {
				t.Fatal(err)
			}
			store.note(held, token)
		}
	}
	issue("page")
	if len(held.tokens) != issued {
		t.Fatalf("live tokens = %d, want %d", len(held.tokens), issued)
	}

	store.dropExpired(now)
	if len(held.tokens) != issued {
		t.Fatalf("live tokens dropped: %d", len(held.tokens))
	}
	if store.cuts[id] != held {
		t.Fatal("newest cut dropped")
	}

	now = now.Add(CutRetention)
	store.dropExpired(now)
	if len(held.tokens) != 0 {
		t.Fatalf("expired tokens kept on the newest cut: %d", len(held.tokens))
	}
	if store.cuts[id] != held {
		t.Fatal("newest cut dropped with its tokens")
	}

	issue("again")
	now = now.Add(CutRetention)
	fresh, err := mintPageToken(id, digestLabel("fresh"), 0)
	if err != nil {
		t.Fatal(err)
	}
	store.note(held, fresh)
	if len(held.tokens) != 1 || held.tokens[0].request != digestLabel("fresh") {
		t.Fatalf("note kept expired tokens: %d", len(held.tokens))
	}
	store.note(held, fresh)
	if len(held.tokens) != 1 {
		t.Fatalf("refreshed token duplicated: %d", len(held.tokens))
	}
}

func digestLabel(label string) semreg.Digest {
	sum := sha256.Sum256([]byte("page token " + label))
	return semreg.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
