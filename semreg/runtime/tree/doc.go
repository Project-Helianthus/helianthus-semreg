// Package tree is SemReg's one runtime tree: the installation, its instances,
// transports and devices as nodes, each with the semantic attributes and
// methods SemReg shows and the native attributes, methods and procedures its
// driver registers in its own namespace. GraphQL and MCP address the same keys.
//
// Reads are detached: nothing here sends on a bus. The graph reads (listing,
// traversal, lens, catalog) call the frozen semreg/v1/graph functions and their
// bounds unchanged, against the retained cut a cut source hands over. The
// package imports the standard library, semreg/v1 and semreg/v1/driver and
// graph only, and no host, driver or transport.
//
// namespace-plan.md states the names; plan.go is its Go form.
package tree
