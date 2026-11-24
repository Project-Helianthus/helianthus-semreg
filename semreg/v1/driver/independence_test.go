package driver_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/Project-Helianthus/helianthus-semreg/"

func parseDir(t *testing.T, dir string, comments bool) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	mode := parser.SkipObjectResolution | parser.ImportsOnly
	if comments {
		mode = parser.SkipObjectResolution
	}
	files := map[string]*ast.File{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, mode)
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = f
	}
	return files
}

func imports(f *ast.File) []string {
	var out []string
	for _, spec := range f.Imports {
		p, _ := strconv.Unquote(spec.Path.Value)
		out = append(out, p)
	}
	return out
}

// The driver package imports the standard library and these packages of this
// module only; it names no driver, transport, runtime or host.
func TestDriverPackageImportsOnlyTheContractPackages(t *testing.T) {
	allowed := map[string]bool{
		modulePath + "semreg/v1": true, modulePath + "semreg/v1/operation": true,
		modulePath + "semreg/v1/catalog": true, modulePath + "semreg/v1/graph": true,
	}
	for name, f := range parseDir(t, ".", false) {
		for _, p := range imports(f) {
			first, _, _ := strings.Cut(p, "/")
			if !strings.Contains(first, ".") {
				continue
			}
			if !allowed[p] {
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
}

func TestCatalogDoesNotImportTheDriverPackage(t *testing.T) {
	for name, f := range parseDir(t, filepath.Join("..", "catalog"), false) {
		for _, p := range imports(f) {
			if strings.HasPrefix(p, modulePath+"semreg/v1/driver") {
				t.Errorf("catalog/%s imports %s", name, p)
			}
		}
	}
}

// Qualification alone is not a guard of the contract: no non-test identifier of
// the driver package names a qualification.
func TestDriverPackageHoldsNoQualificationGuard(t *testing.T) {
	for name, f := range parseDir(t, ".", true) {
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && strings.Contains(id.Name, "Qualif") {
				t.Errorf("%s names %s", name, id.Name)
			}
			return true
		})
	}
}
