package catalog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

func load(t *testing.T) (Catalog, []byte) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "fixtures", "v1", "family-catalog-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c, raw
}

func TestCatalogCompleteAgainstFamilyList(t *testing.T) {
	c, _ := load(t)
	got := map[string]bool{}
	for _, f := range c.Families {
		got[f.FamilyID] = true
	}
	for _, id := range RequiredFamilyIDs() {
		if !got[id] {
			t.Errorf("family %s has no catalog row", id)
		}
	}
	if len(c.Families) != len(RequiredFamilyIDs()) || len(RequiredFamilyIDs()) != 14 {
		t.Fatalf("rows = %d", len(c.Families))
	}
}

func TestEveryRowCarriesEvidenceAndAllowedState(t *testing.T) {
	c, _ := load(t)
	allowed := map[semreg.Qualification]bool{}
	for _, s := range AllowedStates() {
		allowed[s] = true
	}
	for _, f := range c.Families {
		if len(f.Evidence) == 0 {
			t.Errorf("%s: no evidence", f.FamilyID)
		}
		if !allowed[f.State] {
			t.Errorf("%s: state %q", f.FamilyID, f.State)
		}
		if f.State == semreg.QualificationQualified {
			t.Errorf("%s: no family has evidence for a qualified row today", f.FamilyID)
		}
	}
}

func TestStatesSupportedByEvidence(t *testing.T) {
	c, _ := load(t)
	want := map[string]semreg.Qualification{
		"sunspec-fronius-pv": semreg.QualificationCandidate, "growatt-bms-rs485-v202": semreg.QualificationCandidate,
		"tesla-gen3-evse": semreg.QualificationCandidate, "tesla-legacy-fbe0-fde0": semreg.QualificationCandidate,
		"growatt-protocol-ii": semreg.QualificationUnknown, "huawei-smartlogger": semreg.QualificationCandidate,
		"huawei-emma": semreg.QualificationUnknown, "huawei-sdongle": semreg.QualificationUnknown,
		"outback-axs": semreg.QualificationUnknown, "growatt-bms-can-v104": semreg.QualificationCandidate,
		"gree-vrf-can": semreg.QualificationCandidate, "vaillant-ebus-thermal": semreg.QualificationCandidate,
		"eebus-inbound": semreg.QualificationUnknown, "infrastructure": semreg.QualificationUnknown,
	}
	wantReason := map[string]Reason{
		"outback-axs": ReasonNoGoMissingEvidence, "growatt-protocol-ii": ReasonNoAdmissibleProfile,
		"huawei-emma": ReasonEvidenceBlocked, "huawei-sdongle": ReasonEvidenceBlocked,
		"gree-vrf-can": ReasonSemanticMappingUnknown, "infrastructure": ReasonNoProducer,
		"eebus-inbound": ReasonUnknownPendingSTD01,
	}
	for _, f := range c.Families {
		if f.State != want[f.FamilyID] {
			t.Errorf("%s: state %q", f.FamilyID, f.State)
		}
		if r, ok := wantReason[f.FamilyID]; ok && f.Reason != r {
			t.Errorf("%s: reason %q", f.FamilyID, f.Reason)
		}
	}
}

func TestSemanticPacksExist(t *testing.T) {
	packs := map[string]bool{}
	for _, p := range []semreg.PackRef{thermal.New().Pack(), pv.New().Pack(), storage.New().Pack(), evse.New().Pack(), infrastructure.New().Pack()} {
		packs[string(p.ID)+"@"+string(p.Version)] = true
	}
	c, _ := load(t)
	for _, f := range c.Families {
		if f.SemanticPack != "" && !packs[f.SemanticPack] {
			t.Errorf("%s: pack %s is not a built-in pack", f.FamilyID, f.SemanticPack)
		}
	}
}

func TestDecodeRejectsInvalidCatalogs(t *testing.T) {
	_, raw := load(t)
	text := string(raw)
	cases := map[string]string{
		"unknown state":  strings.Replace(text, `"state": "unknown"`, `"state": "evidence_blocked"`, 1),
		"unknown member": strings.Replace(text, `"family_id"`, `"extra": 1, "family_id"`, 1),
		"trailing":       text + "{}",
		"missing family": strings.Replace(text, `"family_id": "outback-axs"`, `"family_id": "outback-x"`, 1),
		"bad reason":     strings.Replace(text, `"no_go_missing_evidence"`, `"fabricated"`, 1),
		"duplicate":      strings.Replace(text, `"family_id": "outback-axs"`, `"family_id": "gree-vrf-can"`, 1),
	}
	for name, doc := range cases {
		if doc == text && name != "trailing" {
			t.Fatalf("%s: mutation did not apply", name)
		}
		if _, err := Decode([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	c, _ := load(t)
	c.Families[0].Evidence = nil
	if c.Validate() == nil {
		t.Error("row without evidence accepted")
	}
}

func TestDecodeRejectsDuplicateKeysAtEveryLevel(t *testing.T) {
	_, raw := load(t)
	text := string(raw)
	cases := map[string]string{
		"top level":      strings.Replace(text, `"contract":`, `"contract": "x", "contract":`, 1),
		"row state":      strings.Replace(text, `"state": "candidate"`, `"state": "candidate", "state": "qualified"`, 1),
		"evidence entry": strings.Replace(text, `"kind":`, `"kind": "a", "kind":`, 1),
	}
	for name, doc := range cases {
		if doc == text {
			t.Fatalf("%s: mutation did not apply", name)
		}
		if _, err := Decode([]byte(doc)); err == nil || !strings.Contains(err.Error(), "duplicate member") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestStatesSetEquality(t *testing.T) {
	c, _ := load(t)
	c.States = []semreg.Qualification{"candidate", "candidate", "candidate", "candidate", "candidate"}
	if c.Validate() == nil {
		t.Error("repeated state accepted")
	}
	c.States = c.States[:4]
	if c.Validate() == nil {
		t.Error("incomplete state set accepted")
	}
}

func TestCompositionClaimsNeedGatewayEvidence(t *testing.T) {
	c, _ := load(t)
	for i, f := range c.Families {
		if f.Composition == CompositionNotComposed {
			if f.CompositionVerified {
				t.Errorf("%s: not_composed cannot be verified", f.FamilyID)
			}
			continue
		}
		clone := c
		clone.Families = append([]Family(nil), c.Families...)
		clone.Families[i].CompositionVerified = false
		if clone.Validate() == nil {
			t.Errorf("%s: unverified composition claim accepted", f.FamilyID)
		}
		clone.Families[i] = f
		clone.Families[i].Evidence = nil
		for _, e := range f.Evidence {
			if e.Kind != EvidenceKindGatewayComposition {
				clone.Families[i].Evidence = append(clone.Families[i].Evidence, e)
			}
		}
		if clone.Validate() == nil {
			t.Errorf("%s: composition without gateway evidence accepted", f.FamilyID)
		}
	}
}

func TestNotComposedRowsCannotBeVerified(t *testing.T) {
	c, _ := load(t)
	for i, f := range c.Families {
		if f.FamilyID != "sunspec-fronius-pv" {
			continue
		}
		clone := c
		clone.Families = append([]Family(nil), c.Families...)
		clone.Families[i].Composition = CompositionNotComposed
		if clone.Validate() == nil {
			t.Error("not_composed with composition_verified=true and gateway evidence accepted")
		}
		clone.Families[i].CompositionVerified = false
		if clone.Validate() == nil {
			t.Error("not_composed carrying gateway composition evidence accepted")
		}
		return
	}
	t.Fatal("PV row missing")
}
