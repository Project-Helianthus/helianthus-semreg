package semreg

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSourcePathKeyAxes(t *testing.T) {
	// Each pair differs in exactly one field. The lower and upper values are
	// specified here, independently of production identity and sorting helpers.
	base := SourcePathRef{
		SourceID: "source:a", SourceEpochID: "source-epoch:a",
		DriverGeneration: "2", BindingID: "binding:a",
	}
	source, epoch, generation, binding := base, base, base, base
	source.SourceID = "source:b"
	epoch.SourceEpochID = "source-epoch:b"
	generation.DriverGeneration = "10"
	binding.BindingID = "binding:b"

	for _, axis := range []struct {
		name  string
		upper SourcePathRef
	}{
		{"source_id", source},
		{"source_epoch_id", epoch},
		{"driver_generation", generation},
		{"binding_id", binding},
	} {
		t.Run(axis.name, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				paths []SourcePathRef
				want  ErrorID
			}{
				{"distinct_sorted", []SourcePathRef{base, axis.upper}, ""},
				{"duplicate", []SourcePathRef{axis.upper, axis.upper}, DuplicateKey},
				{"reversed", []SourcePathRef{axis.upper, base}, NoncanonicalOrder},
			} {
				t.Run(tc.name, func(t *testing.T) {
					input := DerivationInput{
						CandidateID: "candidate:synthetic", CandidateRevision: "1",
						SourcePaths: tc.paths,
					}
					check := func(t *testing.T, err error) {
						t.Helper()
						if tc.want == "" {
							if err != nil {
								t.Fatalf("valid distinct sorted pair rejected: %v", err)
							}
						} else if err == nil || ErrorIdentifier(err) != tc.want {
							t.Fatalf("got %v (%s), want %s", err, ErrorIdentifier(err), tc.want)
						}
					}
					t.Run("Validate", func(t *testing.T) {
						check(t, input.Validate())
					})
					t.Run("CanonicalJSON", func(t *testing.T) {
						raw, err := CanonicalJSON(input)
						check(t, err)
						if tc.want == "" {
							var got DerivationInput
							if err := json.Unmarshal(raw, &got); err != nil {
								t.Fatal(err)
							}
							if !reflect.DeepEqual(got, input) {
								t.Fatalf("canonical encoding changed the input: got %+v, want %+v", got, input)
							}
						}
					})
					t.Run("Decode", func(t *testing.T) {
						// Marshal directly so rejected inputs reach Decode without
						// depending on CanonicalJSON admission or production sorting.
						raw, err := json.Marshal(input)
						if err != nil {
							t.Fatal(err)
						}
						got, err := Decode[DerivationInput](raw)
						check(t, err)
						if tc.want == "" && !reflect.DeepEqual(got, input) {
							t.Fatalf("decode changed the input: got %+v, want %+v", got, input)
						}
					})
				})
			}
		})
	}
}
