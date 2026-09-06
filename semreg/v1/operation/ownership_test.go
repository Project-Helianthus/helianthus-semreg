package operation_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestSemanticOperationOwnershipBoundary guards the specific INT-06 mechanics
// removed by issue #9's ownership correction. Semantic admission may expose
// immutable claims, but it must not export or exercise native callback,
// publication-lock, release, or one-shot invocation lifecycle machinery.
func TestSemanticOperationOwnershipBoundary(t *testing.T) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve ownership test source")
	}
	operationDir := filepath.Dir(source)
	files, err := filepath.Glob(filepath.Join(operationDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join(filepath.Dir(operationDir), "publication.go"))

	forbidden := map[string]struct{}{
		"AcquireOperationGuard": {},
		"GenerationGuard":       {},
		"GuardedInvoke":         {},
		"NativeCallback":        {},
		"invocationClosed":      {},
		"invocationDone":        {},
	}
	fset := token.NewFileSet()
	var violations []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(fset, name, raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			if _, blocked := forbidden[identifier.Name]; blocked {
				position := fset.Position(identifier.Pos())
				violations = append(violations, fmt.Sprintf("%s:%d:%s", filepath.Base(position.Filename), position.Line, identifier.Name))
			}
			return true
		})
	}
	if len(violations) != 0 {
		sort.Strings(violations)
		t.Fatalf("INT-06 lifecycle mechanics present in semreg production source:\n%s", strings.Join(violations, "\n"))
	}
}
