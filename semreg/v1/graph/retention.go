package graph

import (
	"sync"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// CutRetention is the longest a cut is kept after it stops being the newest (CUT-3).
const CutRetention = 120 * time.Second

// Clock supplies the store's time. Tests pass a clock they move. A nil clock
// passed to NewCutStore reads time.Now. The clock is called with the store
// lock held and must not call the store.
type Clock func() time.Time

// CutStore retains graph cuts for reads. It keeps the newest cut and every
// cut an unexpired page token names, at most MaxRetainedCuts, and drops a
// cut at most CutRetention after it stops being the newest (CUT-3). Expired
// token records are dropped, including on the newest cut. The store is the
// only state; listing and traversal are pure functions of a cut.
type CutStore struct {
	now    Clock
	mu     sync.Mutex
	newest semreg.Digest
	cuts   map[semreg.Digest]*retainedCut
}

type retainedCut struct {
	cut     *Cut
	index   *cutIndex
	catalog *Catalog
	demoted time.Time
	tokens  []issuedToken
}

type issuedToken struct {
	request semreg.Digest
	cursor  int
	expires time.Time
}

// NewCutStore returns an empty store. now may be nil.
func NewCutStore(now Clock) *CutStore {
	if now == nil {
		now = time.Now
	}
	return &CutStore{now: now, cuts: map[semreg.Digest]*retainedCut{}}
}

// Retain keeps a cut assembled from members with catalog. The same cut
// identifier becomes the newest again. Snapshots are indexed and not kept.
func (s *CutStore) Retain(cut *Cut, members []semreg.Snapshot, catalog *Catalog) error {
	if s == nil {
		return readError(semreg.InvalidValue, "nil cut store")
	}
	if catalog == nil {
		return readError(semreg.InvalidValue, "nil catalog")
	}
	index, err := indexMembers(cut, members)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.dropExpired(now)
	id := cut.ID()
	s.demoteOthers(id, now)
	held, ok := s.cuts[id]
	if !ok {
		held = &retainedCut{}
		s.cuts[id] = held
	}
	held.cut = cut
	held.index = index
	held.catalog = catalog
	held.demoted = time.Time{}
	s.newest = id
	s.evictToCap(now)
	return nil
}

// ListResources serves ListResources from the newest cut, or from the pinned
// cut (CUT-2). A pin the store does not retain is revision_conflict.
func (s *CutStore) ListResources(request ListRequest) (ResourcePage, error) {
	if s == nil {
		return ResourcePage{}, readError(semreg.InvalidValue, "nil cut store")
	}
	if err := request.validate(); err != nil {
		return ResourcePage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	held, err := s.held(request.CutID)
	if err != nil {
		return ResourcePage{}, err
	}
	page, err := listIndexed(held.cut, held.index, held.catalog, request)
	if err != nil {
		return ResourcePage{}, err
	}
	s.note(held, page.NextPage)
	return page, nil
}

// Traverse serves Traverse from the newest cut, or from the pinned cut.
func (s *CutStore) Traverse(request TraversalRequest) (TraversalPage, error) {
	if s == nil {
		return TraversalPage{}, readError(semreg.InvalidValue, "nil cut store")
	}
	if err := request.validate(); err != nil {
		return TraversalPage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	held, err := s.held(request.CutID)
	if err != nil {
		return TraversalPage{}, err
	}
	page, err := traverseIndexed(held.cut, held.index, held.catalog, request)
	if err != nil {
		return TraversalPage{}, err
	}
	s.note(held, page.NextPage)
	return page, nil
}

func (s *CutStore) held(pin *semreg.Digest) (*retainedCut, error) {
	now := s.now()
	s.dropExpired(now)
	id := s.newest
	if pin != nil && *pin != "" {
		id = *pin
	}
	held := s.cuts[id]
	if held == nil || id == "" {
		return nil, readError(semreg.RevisionConflict, "cut is not retained")
	}
	return held, nil
}

func (s *CutStore) note(held *retainedCut, token string) {
	if token == "" {
		return
	}
	body, err := parsePageToken(token)
	if err != nil || body.CutID != held.cut.ID() {
		return
	}
	now := s.now()
	pruneExpiredTokens(held, now)
	expires := now.Add(CutRetention)
	for i := range held.tokens {
		if held.tokens[i].request == body.Request && held.tokens[i].cursor == body.Cursor {
			held.tokens[i].expires = expires
			return
		}
	}
	held.tokens = append(held.tokens, issuedToken{request: body.Request, cursor: body.Cursor, expires: expires})
}

// pruneExpiredTokens drops token records whose expiry has passed. The newest
// cut is never removed, so this is what keeps its token list bounded.
func pruneExpiredTokens(held *retainedCut, now time.Time) {
	if held == nil || len(held.tokens) == 0 {
		return
	}
	n := 0
	for _, token := range held.tokens {
		if now.Before(token.expires) {
			held.tokens[n] = token
			n++
		}
	}
	for i := n; i < len(held.tokens); i++ {
		held.tokens[i] = issuedToken{}
	}
	if n == 0 {
		held.tokens = nil
		return
	}
	held.tokens = held.tokens[:n]
}

func (s *CutStore) dropExpired(now time.Time) {
	for id, held := range s.cuts {
		pruneExpiredTokens(held, now)
		if id == s.newest || held.demoted.IsZero() {
			continue
		}
		if !now.Before(held.demoted.Add(CutRetention)) {
			delete(s.cuts, id)
		}
	}
}

func (s *CutStore) demoteOthers(newest semreg.Digest, now time.Time) {
	for id, held := range s.cuts {
		if id == newest {
			continue
		}
		if held.demoted.IsZero() {
			held.demoted = now
		}
	}
}

func (s *CutStore) evictToCap(now time.Time) {
	for len(s.cuts) > MaxRetainedCuts {
		victim := s.evictionVictim(now)
		if victim == "" {
			return
		}
		delete(s.cuts, victim)
	}
}

func (s *CutStore) evictionVictim(now time.Time) semreg.Digest {
	var oldestPlain, oldestToken semreg.Digest
	var plainAt, tokenAt time.Time
	for id, held := range s.cuts {
		if id == s.newest || held.demoted.IsZero() {
			continue
		}
		if tokenPinned(held, now) {
			if oldestToken == "" || held.demoted.Before(tokenAt) {
				oldestToken, tokenAt = id, held.demoted
			}
			continue
		}
		if oldestPlain == "" || held.demoted.Before(plainAt) {
			oldestPlain, plainAt = id, held.demoted
		}
	}
	if oldestPlain != "" {
		return oldestPlain
	}
	return oldestToken
}

func tokenPinned(held *retainedCut, now time.Time) bool {
	for _, token := range held.tokens {
		if now.Before(token.expires) {
			return true
		}
	}
	return false
}
