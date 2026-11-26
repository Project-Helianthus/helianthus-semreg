package operation_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestExactOperationVectorPinAndDisposition(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve operation fixture")
	}
	path := filepath.Join(filepath.Dir(source), "..", "..", "..", "fixtures", "v1", "operation-vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(filepath.Dir(path), "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, pin, found := strings.Cut(string(readme), "\nOperation vector fixture SHA-256: `")
	pin, _, closed := strings.Cut(pin, "`\n")
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); !found || !closed || got != pin {
		t.Fatalf("operation fixture SHA-256: got %s, documented pin %q", got, pin)
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Contract string `json:"contract"`
		Source   struct {
			Repository string `json:"repository"`
			Issue      int    `json:"issue"`
			Path       string `json:"path"`
			Selection  string `json:"selection"`
			Count      int    `json:"count"`
		} `json:"source"`
		Vectors []struct {
			ID        string   `json:"id"`
			Coverage  []string `json:"coverage"`
			Criterion string   `json:"criterion"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.Contract != "helianthus.semantic.kernel.acceptance/v1" ||
		document.Source.Repository != "Project-Helianthus/helianthus-docs-semantic" ||
		document.Source.Issue != 9 || document.Source.Path != "api/v1/acceptance-vectors.json" ||
		document.Source.Selection != "coverage=operation" || document.Source.Count != 29 || len(document.Vectors) != 29 {
		t.Fatalf("unexpected operation vector manifest: %+v count=%d", document.Source, len(document.Vectors))
	}
	expected := []string{
		"K-NEG-013", "K-NEG-016", "K-NEG-017", "K-NEG-018", "K-NEG-019", "K-NEG-020", "K-NEG-022", "K-NEG-023", "K-NEG-025", "K-NEG-026",
		"K-NEG-036", "K-NEG-038", "K-NEG-039", "K-NEG-040", "K-NEG-041", "K-NEG-043", "K-NEG-044", "K-NEG-045", "K-NEG-046", "K-NEG-049",
		"K-NEG-050", "K-NEG-051", "K-NEG-053", "K-POS-012", "K-POS-013", "K-POS-014", "K-POS-015", "K-POS-022", "K-POS-023",
	}
	actual := make([]string, 0, len(document.Vectors))
	for _, vector := range document.Vectors {
		covered := false
		for _, coverage := range vector.Coverage {
			covered = covered || coverage == "operation"
		}
		if !covered || vector.Criterion == "" {
			t.Fatalf("operation vector lost exact coverage/criterion: %+v", vector)
		}
		actual = append(actual, vector.ID)
	}
	sort.Strings(actual)
	if len(actual) != len(expected) {
		t.Fatalf("operation vector count: %d", len(actual))
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("operation vector IDs: got %v want %v", actual, expected)
		}
	}

	releaseIDs := []string{"SS16-NEG-01", "SS16-NEG-02", "SS16-NEG-03", "SS16-NEG-04", "SS16-NEG-05", "SS16-POS-01", "SS16-POS-02", "SS16-POS-03"}
	actual = actual[:0]
	for _, vector := range loadOperationVectorDocument(t).ReleaseVectors {
		if len(vector.Coverage) != 1 || vector.Coverage[0] != "operation" || vector.Criterion == "" || len(vector.Input) == 0 {
			t.Fatalf("incomplete release vector: %+v", vector)
		}
		actual = append(actual, vector.ID)
	}
	sort.Strings(actual)
	if strings.Join(actual, " ") != strings.Join(releaseIDs, " ") {
		t.Fatalf("release vector IDs: got %v want %v", actual, releaseIDs)
	}
}

func TestOperationVectorLoaderRejectsDuplicateKeys(t *testing.T) {
	raw := []byte(`{"contract":"helianthus.semantic.kernel.acceptance/v1","contract":"helianthus.semantic.kernel.acceptance/v1"}`)
	if err := rejectDuplicateJSONKeys(raw); err == nil {
		t.Fatal("duplicate-key vector document accepted")
	}
}

func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing vector JSON")
		}
		return err
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("non-string vector object key")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate vector object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("vector object end: %v", err)
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("vector array end: %v", err)
		}
	default:
		return fmt.Errorf("unexpected vector delimiter %q", delimiter)
	}
	return nil
}
