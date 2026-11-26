package host

import (
	"context"
	"slices"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/identity"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// WalkFilter selects what Walk visits. PageSize is the caller's; no figure is
// added.
type WalkFilter struct {
	PageSize int
	Filter   tree.Filter
}

// AllVisible visits every entry of the cut, in pages of the given size.
func AllVisible(pageSize int) WalkFilter { return WalkFilter{PageSize: pageSize} }

// WalkEntry is one visited entry: its typed descriptor and, for an attribute,
// its detached value or the read error. Planned and retained links are visited
// with Link or RetainedLink respectively, and no descriptor.
type WalkEntry struct {
	Descriptor   tree.Descriptor
	Value        *tree.Value
	Err          error
	Link         *runtime.PlannedLink
	RetainedLink *identity.Link
}

// WalkReport reports the completeness of the walked cut and its omitted
// partitions.
type WalkReport struct {
	Completeness graph.Completeness
	NotIncluded  []graph.NotIncludedAsset
}

// Incomplete reports that the cut omits at least one partition.
func (r WalkReport) Incomplete() bool { return len(r.NotIncluded) > 0 }

// Cut is one capture of the whole installation.
type Cut struct {
	site *Site
	cut  *runtime.HostCut
}

// Cut captures the installation.
func (s *Site) Cut(ctx context.Context) (*Cut, error) {
	cut, err := s.rt.Cut(ctx)
	if err != nil {
		return nil, err
	}
	return &Cut{site: s, cut: cut}, nil
}

// Tree is the tree cut of this capture, with ID, Cursor, List, Describe, Read
// and Close.
func (c *Cut) Tree() *tree.TreeCut { return c.cut.TreeCut }

// Ref is the scoped cut reference: identifier, completeness, members and
// omissions.
func (c *Cut) Ref() graph.CutRef { return c.cut.Ref }

// Links are the retained links of this capture under their effective endpoints,
// each claim keeping its declared key and basis.
func (c *Cut) Links() []identity.Link { return c.cut.Links }

// Relays are the planned directed links of the installation, bridge to link
// node, with their type, endpoints and basis.
func (c *Cut) Relays() []runtime.PlannedLink {
	return append([]runtime.PlannedLink(nil), c.cut.Relays...)
}

// Close closes the capture.
func (c *Cut) Close() error { return c.cut.Close() }

// Prepare resolves the selection against this cut's catalog and scope. Nothing
// is sent.
func (c *Cut) Prepare(ctx context.Context, selection MethodSelection) (tree.Prepared, error) {
	return c.site.rt.Prepare(ctx, c.cut.ID(), selection.Method, selection.Input)
}

// Walk visits the installation node and everything beneath it in pages, calling
// emit for each descriptor, and reads every attribute. It reports an incomplete
// partition.
func (c *Cut) Walk(ctx context.Context, filter WalkFilter, emit func(WalkEntry) error) (WalkReport, error) {
	plan := c.site.rt.Plan()
	report := WalkReport{Completeness: c.cut.Ref.Completeness, NotIncluded: append([]graph.NotIncludedAsset(nil), c.cut.Ref.NotIncluded...)}
	root := tree.NodePath{Installation: plan.Installation}
	queue := []tree.NodePath{root}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		node := driver.NodeRef{Bridge: plan.Bridge, ID: path.NodeID()}
		token := ""
		for {
			page, err := c.cut.List(ctx, node, tree.Page{Size: filter.PageSize, Token: token, Filter: filter.Filter})
			if err != nil {
				return report, err
			}
			for _, d := range page.Entries {
				entry := WalkEntry{Descriptor: d}
				switch d.Kind {
				case tree.EntryNode:
					child, err := tree.ParseNodePath(string(d.Ref))
					if err != nil {
						return report, err
					}
					queue = append(queue, child)
				case tree.EntryAttribute:
					value, err := c.cut.Read(ctx, d.Ref, tree.ReadOptions{})
					if err != nil {
						entry.Err = err
					} else {
						entry.Value = &value
					}
				}
				if err := emit(entry); err != nil {
					return report, err
				}
			}
			if page.Next == "" {
				break
			}
			token = page.Next
		}
	}
	for i := range c.cut.Relays {
		link := c.cut.Relays[i]
		if err := emit(WalkEntry{Link: &link}); err != nil {
			return report, err
		}
	}
	for _, link := range c.Links() {
		link.Claims = slices.Clone(link.Claims)
		for i := range link.Claims {
			link.Claims[i].Basis = slices.Clone(link.Claims[i].Basis)
		}
		if err := emit(WalkEntry{RetainedLink: &link}); err != nil {
			return report, err
		}
	}
	return report, nil
}
