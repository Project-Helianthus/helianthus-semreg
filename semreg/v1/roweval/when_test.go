package roweval

import "testing"

// A row applies only to what the observation supplied. A qualifier that was not
// supplied is neither a wildcard nor a known-false feature.
func TestWhenIsMetOnlyByWhatIsSupplied(t *testing.T) {
	v := func(m map[string][]uint16) Qualifiers { return Qualifiers{Revisions: m} }
	f := func(m map[string]bool) Qualifiers { return Qualifiers{Features: m} }
	e := func(m map[string]string) Qualifiers { return Qualifiers{Values: m} }
	rng := When{Versions: []VersionRange{{Axis: "api", Min: []uint16{2, 0}, Before: []uint16{3, 1}}}}
	cases := []struct {
		name string
		w    When
		q    Qualifiers
		ok   bool
	}{
		{"empty condition", When{}, Qualifiers{}, true},
		{"equal met", When{Equal: map[string]string{"role": "a"}}, e(map[string]string{"role": "a", "other": "b"}), true},
		{"equal differs", When{Equal: map[string]string{"role": "a"}}, e(map[string]string{"role": "b"}), false},
		{"equal empty text is a value", When{Equal: map[string]string{"role": ""}}, e(map[string]string{"role": ""}), true},
		{"equal not supplied", When{Equal: map[string]string{"role": ""}}, Qualifiers{}, false},
		{"both equals", When{Equal: map[string]string{"a": "1", "b": "2"}}, e(map[string]string{"a": "1", "b": "3"}), false},
		{"version at the minimum", rng, v(map[string][]uint16{"api": {2, 0}}), true},
		{"version below the minimum", rng, v(map[string][]uint16{"api": {1, 65535}}), false},
		{"version padded with zeros", rng, v(map[string][]uint16{"api": {2}}), true},
		{"version longer than the bound", rng, v(map[string][]uint16{"api": {2, 0, 7}}), true},
		{"version just before the end", rng, v(map[string][]uint16{"api": {3, 0, 65535}}), true},
		{"version at the end", rng, v(map[string][]uint16{"api": {3, 1}}), false},
		{"version past the end", rng, v(map[string][]uint16{"api": {4}}), false},
		{"version axis not supplied", rng, v(map[string][]uint16{"hw": {2, 5}}), false},
		{"version none supplied", rng, Qualifiers{}, false},
		{"requires met", When{Requires: []string{"x"}}, f(map[string]bool{"x": true}), true},
		{"requires known false", When{Requires: []string{"x"}}, f(map[string]bool{"x": false}), false},
		{"requires unknown", When{Requires: []string{"x"}}, f(map[string]bool{"y": true}), false},
		{"forbids met", When{Forbids: []string{"x"}}, f(map[string]bool{"x": false}), true},
		{"forbids present", When{Forbids: []string{"x"}}, f(map[string]bool{"x": true}), false},
		{"forbids unknown is not known false", When{Forbids: []string{"x"}}, Qualifiers{}, false},
	}
	for _, c := range cases {
		if got := c.w.check(c.q); got.ok != c.ok {
			t.Errorf("%s: %v, want %v", c.name, got.ok, c.ok)
		}
	}
}

// A condition that failed for want of a qualifier says so.
func TestWhenNamesWhatWasNotSupplied(t *testing.T) {
	w := When{Equal: map[string]string{"role": "a"}, Versions: []VersionRange{{Axis: "api", Min: []uint16{1}, Before: []uint16{2}}}, Requires: []string{"x"}, Forbids: []string{"y"}}
	got := w.check(Qualifiers{})
	want := []string{"value role", "revision api", "feature x", "feature y"}
	if got.ok || len(got.missing) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got.missing[i] != want[i] {
			t.Errorf("missing[%d] = %q, want %q", i, got.missing[i], want[i])
		}
	}
	if got := (When{Equal: map[string]string{"role": "a"}}).check(Qualifiers{Values: map[string]string{"role": "b"}}); got.ok || len(got.missing) != 0 {
		t.Errorf("a qualifier that differs was not missing: %+v", got)
	}
}
