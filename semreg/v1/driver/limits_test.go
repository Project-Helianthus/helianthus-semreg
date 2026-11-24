package driver_test

import (
	"testing"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestValueLimitsOrigin(t *testing.T) {
	for _, origin := range []driver.LimitOrigin{driver.LimitDeviceQuery, driver.LimitSpecification} {
		l := driver.ValueLimits{Min: quantity("5", 0, "unit.kelvin"), Max: quantity("9", 1, "unit.kelvin"), Step: quantity("5", -1, "unit.kelvin"), Origin: origin, Evidence: evidenceSet(string(origin))}
		wantValid(t, string(origin), l.Validate())
	}
	if driver.LimitDeviceQuery != "device_query" || driver.LimitSpecification != "specification" {
		t.Fatal("limit origin wire values")
	}
	for name, origin := range map[string]driver.LimitOrigin{"empty": "", "profile": "profile_default", "case variant": "Device_Query", "heuristic": "observed_range"} {
		wantInvalid(t, "origin "+name, driver.ValueLimits{Origin: origin, Evidence: evidenceSet("x")}.Validate())
	}
}

func TestValueLimitsNeedEvidenceAndValidQuantities(t *testing.T) {
	wantInvalid(t, "empty evidence", driver.ValueLimits{Origin: driver.LimitSpecification}.Validate())
	wantInvalid(t, "invalid min", driver.ValueLimits{Min: quantity("01", 0, "unit.kelvin"), Origin: driver.LimitSpecification, Evidence: evidenceSet("x")}.Validate())
	wantInvalid(t, "invalid max", driver.ValueLimits{Max: quantity("1", 0, "!"), Origin: driver.LimitDeviceQuery, Evidence: evidenceSet("x")}.Validate())
	wantInvalid(t, "invalid step", driver.ValueLimits{Step: quantity("x", 0, "unit.kelvin"), Origin: driver.LimitDeviceQuery, Evidence: evidenceSet("x")}.Validate())
}

func TestSourceAndRouteCarryLimitsWithOrigin(t *testing.T) {
	good := &driver.ValueLimits{Min: quantity("5", 0, "unit.kelvin"), Origin: driver.LimitDeviceQuery, Evidence: evidenceSet("query")}
	bad := &driver.ValueLimits{Min: quantity("5", 0, "unit.kelvin"), Origin: "guess", Evidence: evidenceSet("query")}

	s := source("a", 1)
	s.Limits = good
	wantValid(t, "source with queried limits", s.Validate())
	s.Limits = bad
	wantInvalid(t, "source with an unorigined limit", s.Validate())

	r := settingRoute("a")
	r.Limits = &driver.ValueLimits{Max: quantity("9", 1, "unit.kelvin"), Origin: driver.LimitSpecification, Evidence: evidenceSet("clause")}
	wantValid(t, "route with specification limits", r.Validate())
	r.Limits = bad
	wantInvalid(t, "route with an unorigined limit", r.Validate())
}
