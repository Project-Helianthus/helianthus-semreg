package operation_test

import (
	"go/build"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	modulePath    = "github.com/Project-Helianthus/helianthus-semreg"
	rootPath      = modulePath + "/semreg/v1"
	graphPath     = modulePath + "/semreg/v1/graph"
	operationPath = modulePath + "/semreg/v1/operation"
)

// moduleDeps is the transitive import closure of one package of this module,
// read from the non-test files that the current build context selects, as
// go list -deps reports it: every package of the module it reaches and every
// package outside the module it imports directly from one of them. Packages
// outside the module are not followed; none of them can import this module.
func moduleDeps(t *testing.T, start string) map[string]bool {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	deps := map[string]bool{}
	var visit func(path string)
	visit = func(path string) {
		if deps[path] {
			return
		}
		deps[path] = true
		if path != modulePath && !strings.HasPrefix(path, modulePath+"/") {
			return
		}
		dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, modulePath)))
		pkg, err := build.Default.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("read package %s: %v", path, err)
		}
		for _, imported := range pkg.Imports {
			if !strings.Contains(strings.SplitN(imported, "/", 2)[0], ".") {
				continue // standard library
			}
			visit(imported)
		}
	}
	visit(start)
	return deps
}

func sortedDeps(deps map[string]bool) []string {
	out := make([]string, 0, len(deps))
	for path := range deps {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// TestOperationDoesNotImportGraph encodes the go list -deps evidence for
// AUT-1 and ACT-4: admission reads no graph record, plane membership, lens
// result, relation instance or absence statement, so neither the operation
// package nor any package it depends on imports semreg/v1/graph. The root
// package, which the graph package implements its catalog interface for,
// does not import it either. Adding an import of semreg/v1/graph to any
// non-test file of the operation package, of the root package, or of any
// module package they reach makes this test fail.
func TestOperationDoesNotImportGraph(t *testing.T) {
	for _, start := range []string{operationPath, rootPath} {
		deps := moduleDeps(t, start)
		if deps[graphPath] {
			t.Errorf("%s depends on %s: %v", start, graphPath, sortedDeps(deps))
		}
		t.Logf("%s deps: %v", start, sortedDeps(deps))
	}
	// The walk is not vacuous: it reaches the root package from the operation
	// package, and from the graph package it reaches the root package and,
	// through the accepted packs, the operation package.
	if !moduleDeps(t, operationPath)[rootPath] {
		t.Fatalf("the operation package's dependencies lack %s", rootPath)
	}
	graphDeps := moduleDeps(t, graphPath)
	if !graphDeps[rootPath] || !graphDeps[operationPath] {
		t.Fatalf("graph package dependencies %v", sortedDeps(graphDeps))
	}
}
