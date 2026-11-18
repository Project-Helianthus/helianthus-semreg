package projection_test

import (
	"go/build"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectionImportsNoPack is the package boundary of TGT-6: the
// projection package holds target records and its non-test files import no
// pack. A pack import in any of those files fails this test.
func TestProjectionImportsNoPack(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := build.Default.ImportDir(filepath.Join(root, "semreg", "v1", "projection"), 0)
	if err != nil {
		t.Fatalf("read projection package: %v", err)
	}
	var packs []string
	for _, imported := range pkg.Imports {
		if strings.Contains(imported, "/packs/") {
			packs = append(packs, imported)
		}
	}
	if len(packs) != 0 {
		t.Fatalf("projection imports pack packages: %v", packs)
	}
}
