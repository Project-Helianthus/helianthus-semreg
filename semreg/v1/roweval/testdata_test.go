package roweval

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type readmeRow struct{ file, sha, kind, source string }

func readmeRows(t testing.TB) []readmeRow {
	t.Helper()
	var rows []readmeRow
	for _, line := range strings.Split(string(readFile(t, "README")), "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| file ") || strings.HasPrefix(line, "| ---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "| "), " | ")
		if len(cells) != 4 {
			t.Fatalf("malformed README row %q", line)
		}
		rows = append(rows, readmeRow{cells[0], cells[1], cells[2], cells[3]})
	}
	return rows
}

// Every file of testdata has a README row, every row has a file, and the
// recorded hash is the hash of the file. A byte copy has the hash of its source.
func TestTestdataMatchesReadme(t *testing.T) {
	rows := readmeRows(t)
	if len(rows) == 0 {
		t.Fatal("README lists no file")
	}
	listed := map[string]bool{}
	hex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, r := range rows {
		listed[r.file] = true
		if got := fileSHA(readFile(t, r.file)); got != r.sha {
			t.Errorf("%s: recorded %s, file has %s", r.file, r.sha, got)
		}
		if !hex.MatchString(r.source) {
			t.Errorf("%s: source record hash %q", r.file, r.source)
		}
		if r.kind == "byte copy" && r.source != r.sha {
			t.Errorf("%s: a byte copy has the hash of its source", r.file)
		}
		if r.kind != "byte copy" && r.source == r.sha {
			t.Errorf("%s: an extract cannot have the hash of its source", r.file)
		}
	}
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "README" && !listed[e.Name()] {
			t.Errorf("%s has no README row", e.Name())
		}
	}
}

// The neutral operand of a point-named record has the native value, unit and
// disposition fields only, and no member that names a point.
func TestNeutralOperandsCarryNativeFieldsOnly(t *testing.T) {
	files, _ := filepath.Glob("testdata/wordblk-*.operand.json")
	if len(files) != 8 {
		t.Fatalf("got %d operands, want 8", len(files))
	}
	for _, f := range files {
		m := readJSON(t, filepath.Base(f))
		for k := range m {
			if k != "case" && k != "native" && k != "disposition" {
				t.Errorf("%s: member %q", f, k)
			}
		}
		if n, ok := m["native"].(map[string]any); ok {
			if len(n) != 2 || n["number"] == nil || n["unit"] == nil {
				t.Errorf("%s: native members %v", f, n)
			}
		}
	}
	// The unit keeps its exact native value.
	if u := asString(at(readJSON(t, "wordblk-103-w-1234-sf-minus1.operand.json"), "native", "unit")); u != "W" {
		t.Errorf("unit %q", u)
	}
}
