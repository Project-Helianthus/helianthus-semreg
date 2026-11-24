package driver

import (
	"context"
	"errors"
	"fmt"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// Filter narrows a capture within the session's scope. It cannot widen it.
type Filter struct {
	Assets []v1.AssetID `json:"assets,omitempty"`
}

// Validate checks every asset id.
func (f Filter) Validate() error {
	for i, a := range f.Assets {
		if err := a.Validate(); err != nil {
			return fmt.Errorf("assets[%d]: %w", i, err)
		}
	}
	return nil
}

// SemanticCut is one scoped capture. Snapshots and SettingRoutes are detached
// from the same commit and filtered to the session's scope: the frozen snapshot
// capabilities, bindings and revisions and the committed setting-route
// declarations. A consumer acts through ScopedOperations, which revalidates an
// operation independently; there is no second capture or route authority.
type SemanticCut struct {
	Ref           graph.CutRef   `json:"ref"`
	Snapshots     []v1.Snapshot  `json:"snapshots"`
	SettingRoutes []SettingRoute `json:"setting_routes"`
}

// Validate checks the cut id, every snapshot and every setting route.
func (c SemanticCut) Validate() error {
	if err := within("cut id", c.Ref.CutID.Validate()); err != nil {
		return err
	}
	for i, s := range c.Snapshots {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("snapshots[%d]: %w", i, err)
		}
	}
	for i, r := range c.SettingRoutes {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("setting_routes[%d]: %w", i, err)
		}
	}
	return nil
}

// Cursor is an incarnation plus an ordered commit position, separate from the
// cut id.
type Cursor struct {
	Incarnation string    `json:"incarnation"`
	Position    v1.Uint64 `json:"position"`
}

// Validate checks the incarnation and the position.
func (c Cursor) Validate() error {
	return first(required("incarnation", c.Incarnation), within("position", c.Position.Validate()))
}

// Changes is the result of Resume: every intervening commit in order, so A to B
// to A returns both transitions, or an explicit resync when the history is
// unavailable.
type Changes struct {
	Cursor  Cursor        `json:"cursor"`
	Commits []SemanticCut `json:"commits,omitempty"`
	Resync  bool          `json:"resync,omitempty"`
}

// Validate checks the cursor and every commit, and requires a resync to carry no
// commit.
func (c Changes) Validate() error {
	if err := within("cursor", c.Cursor.Validate()); err != nil {
		return err
	}
	if c.Resync && len(c.Commits) > 0 {
		return errors.New("a resync carries no commits")
	}
	for i, commit := range c.Commits {
		if err := commit.Validate(); err != nil {
			return fmt.Errorf("commits[%d]: %w", i, err)
		}
	}
	return nil
}

// SemanticInput is the scoped read of SemReg that a consumer driver receives
// through its Session. At the two graph calls the session's capture is
// authoritative for the cut id, the scoped members and omissions and the
// associated catalog and evaluation context; the request fields for the cut,
// members and omissions cannot change that source. The lens, traversal and
// paging selection, and the sum's member definition and scope, come from the
// caller. Site.Watch is lifecycle state, never a replacement feed.
type SemanticInput interface {
	Capture(context.Context, Filter) (SemanticCut, error)
	Resume(context.Context, Cursor, Filter) (Changes, error)
	RetrieveLens(context.Context, graph.CutRef, graph.LensRequest) (graph.LensResult, error)
	SumMembers(context.Context, graph.CutRef, graph.LensRequest, v1.DefinitionRef, v1.ResourceID) (graph.MemberSum, error)
}

// InputRef names one input of a derived value: the captured cut, the foreign
// subject, its candidate and the exact revision.
type InputRef struct {
	At        graph.CutRef   `json:"at"`
	Subject   NodeRef        `json:"subject"`
	Candidate v1.CandidateID `json:"candidate"`
	Revision  v1.Uint64      `json:"revision"`
}

// Validate checks the cut id, the subject, the candidate and the revision.
func (r InputRef) Validate() error {
	return first(within("cut id", r.At.CutID.Validate()), within("subject", r.Subject.Validate()), within("candidate", r.Candidate.Validate()), within("revision", r.Revision.Validate()))
}

// OriginKind marks how a published value came to be. A declared or derived
// value is marked as such and never presented as measured.
type OriginKind string

// OriginKind values.
const (
	OriginMeasured OriginKind = "measured"
	OriginDeclared OriginKind = "declared"
	OriginDerived  OriginKind = "derived"
)

// Origin is the kind of a published value with the evidence that supports the
// declaration.
type Origin struct {
	Kind     OriginKind       `json:"kind"`
	Evidence []v1.EvidenceRef `json:"evidence"`
}

// Validate accepts only the three kinds and checks the evidence.
func (o Origin) Validate() error {
	if o.Kind != OriginMeasured && o.Kind != OriginDeclared && o.Kind != OriginDerived {
		return fmt.Errorf("origin kind %q is not measured, declared or derived", o.Kind)
	}
	for i, e := range o.Evidence {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("evidence[%d]: %w", i, err)
		}
	}
	return nil
}

// Formula is the lineage of a derived value: its algorithm and the inputs it
// used.
type Formula struct {
	Algorithm v1.DefinitionRef `json:"algorithm"`
	Inputs    []InputRef       `json:"inputs"`
}

// Validate checks the algorithm and requires at least one valid input.
func (f Formula) Validate() error {
	if err := within("algorithm", f.Algorithm.Validate()); err != nil {
		return err
	}
	if len(f.Inputs) == 0 {
		return errors.New("a formula needs at least one input")
	}
	for i, in := range f.Inputs {
		if err := in.Validate(); err != nil {
			return fmt.Errorf("inputs[%d]: %w", i, err)
		}
	}
	return nil
}

// SemanticOutput is the northbound side of a two-way driver. It takes its
// semantic data from SemReg only. Qualification alone never prevents StartOutput
// from receiving otherwise eligible input; the target's remaining guards,
// including freshness and availability, still govern payload delivery.
type SemanticOutput interface {
	StartOutput(context.Context, SemanticInput, ScopedOperations) error
}
