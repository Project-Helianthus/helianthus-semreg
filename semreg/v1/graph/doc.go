// Package graph holds the element catalog of the semantic resource graph:
// the planes, the core relation types, the identification schemes, the lenses
// and one entry per semantic element with its owner, disposition, plane
// membership, admission record and kind-specific attributes.
//
// The catalog document is helianthus.semantic.element-catalog/v1, defined by
// api/v1/graph.md section 10 of helianthus-docs-semantic. The repository keeps
// a byte copy in fixtures/v1/element-catalog-v1.json; PinnedDigest is the
// SHA-256 of those bytes and moves together with every in-place edit of the
// source document.
//
// Decode reads the document strictly: invalid UTF-8, trailing data, a repeated
// member (case variants included), an unknown member or a missing member is
// rejected with its stable semreg error, and every value is checked against
// the document's value vocabulary and identifier syntax. Check and Validate
// then apply the cross-entry rules CAT-1 to CAT-9 against the metadata and
// definition indexes of the accepted packs. Load accepts only the pinned bytes
// and returns a catalog that passed all of these checks.
//
// A Catalog is immutable. Every lookup returns a detached copy, and nothing in
// this package publishes, evaluates or admits anything: the catalog only
// records which element belongs to which plane and under which admission.
//
// The package imports the root semreg package and the accepted pack packages
// only.
package graph
