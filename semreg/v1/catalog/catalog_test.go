package catalog

import (
	"bytes"
	"encoding/json"
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

// specificationEvidence and hardwareEvidence are test-local evidence entries
// of the two kinds a driver cites, built with the exported kind constants.
func specificationEvidence() Evidence {
	return Evidence{
		Owner: "Project-Helianthus/example-reg@v1.0.0", Kind: EvidenceKindSpecification,
		Locator: "docs/specification.md", Statement: "the public specification declares the mapping",
	}
}

func hardwareEvidence() Evidence {
	return Evidence{
		Owner: "Project-Helianthus/example-reg@v1.0.0", Kind: EvidenceKindHardwareTestRecord,
		Locator: "docs/hardware-test.md", Statement: "a public hardware test record exists",
	}
}

// declaredFamily is a family record as a driver would declare it: qualified
// from its specification, composition claimed with module and version, and a
// recorded hardware test.
func declaredFamily() Family {
	return Family{
		FamilyID: "example-family", Name: "Example family", NativeOwner: "Project-Helianthus/example-reg",
		SemanticPack: packPV, State: semreg.QualificationQualified, Composition: CompositionComposed,
		Reason: ReasonDeclaredBySpecification, Evidence: []Evidence{specificationEvidence(), hardwareEvidence()},
		CompositionVerified: true, Verification: VerificationTestedOnHardware,
		Module: "github.com/Project-Helianthus/example-reg", Version: "v1.0.0",
	}
}

// notComposedFamily is a driver family that claims no composition.
func notComposedFamily() Family {
	return Family{
		FamilyID: "example-unknown", Name: "Example unknown family", State: semreg.QualificationUnknown,
		Composition: CompositionNotComposed, Reason: ReasonNoProducer, Evidence: []Evidence{specificationEvidence()},
		Verification: VerificationNotTestedOnHardware,
	}
}

func fixtureInfrastructure(t *testing.T) Family {
	t.Helper()
	c, _ := load(t)
	return familyByID(t, c, "infrastructure")
}

func documentWith(t *testing.T, families ...Family) Catalog {
	t.Helper()
	c, _ := load(t)
	c.Families = append(append([]Family(nil), families...), fixtureInfrastructure(t))
	return c
}

func encodeIndented(t *testing.T, v any, prefix string) []byte {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
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
	if len(c.Families) != len(RequiredFamilyIDs()) {
		t.Fatalf("rows = %d, required = %d", len(c.Families), len(RequiredFamilyIDs()))
	}
	if len(RequiredFamilyIDs()) != 1 || RequiredFamilyIDs()[0] != "infrastructure" {
		t.Fatalf("required families = %v", RequiredFamilyIDs())
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
			t.Errorf("%s: the fixture row stays unknown, not qualified", f.FamilyID)
		}
	}
}

func TestStatesSupportedByEvidence(t *testing.T) {
	c, _ := load(t)
	want := map[string]semreg.Qualification{"infrastructure": semreg.QualificationUnknown}
	wantReason := map[string]Reason{"infrastructure": ReasonNoProducer}
	for _, f := range c.Families {
		if f.State != want[f.FamilyID] {
			t.Errorf("%s: state %q", f.FamilyID, f.State)
		}
		if r, ok := wantReason[f.FamilyID]; ok && f.Reason != r {
			t.Errorf("%s: reason %q", f.FamilyID, f.Reason)
		}
	}
}

func TestFixtureRecordsVerificationWithoutChangingState(t *testing.T) {
	f := fixtureInfrastructure(t)
	if f.Verification != VerificationNotTestedOnHardware {
		t.Errorf("verification = %q", f.Verification)
	}
	if f.State != semreg.QualificationUnknown || f.Composition != CompositionNotComposed {
		t.Errorf("state %q composition %q", f.State, f.Composition)
	}
}

func TestSemanticPacksExist(t *testing.T) {
	packs := map[string]bool{}
	for _, p := range []semreg.PackRef{thermal.New().Pack(), pv.New().Pack(), storage.New().Pack(), evse.New().Pack(), infrastructure.New().Pack()} {
		packs[string(p.ID)+"@"+string(p.Version)] = true
	}
	c, _ := load(t)
	for _, f := range c.Families {
		for _, p := range f.Packs() {
			if !packs[p] {
				t.Errorf("%s: pack %s is not a built-in pack", f.FamilyID, p)
			}
		}
	}
}

func TestDecodeRejectsInvalidCatalogs(t *testing.T) {
	_, raw := load(t)
	text := string(raw)
	duplicate := documentWith(t, notComposedFamily())
	duplicate.Families[0].FamilyID = "infrastructure"
	duplicateRaw, err := json.Marshal(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	twice := documentWith(t)
	twice.Families = append(twice.Families, fixtureInfrastructure(t))
	twiceRaw, err := json.Marshal(twice)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown state":          strings.Replace(text, `"state": "unknown"`, `"state": "evidence_blocked"`, 1),
		"unknown member":         strings.Replace(text, `"family_id"`, `"extra": 1, "family_id"`, 1),
		"trailing":               text + "{}",
		"missing family":         strings.Replace(text, `"family_id": "infrastructure"`, `"family_id": "infra-x"`, 1),
		"bad reason":             strings.Replace(text, `"no_producer"`, `"fabricated"`, 1),
		"duplicate after rename": string(duplicateRaw),
		"duplicate family":       string(twiceRaw),
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

func TestDecodeAcceptsAdditionalDriverFamily(t *testing.T) {
	doc := documentWith(t, declaredFamily(), notComposedFamily())
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Decode(raw)
	if err != nil {
		t.Fatalf("catalog with infrastructure and driver-declared families: %v", err)
	}
	if len(c.Families) != 3 {
		t.Fatalf("families = %d", len(c.Families))
	}
	if got := familyByID(t, c, "example-family"); got.Module != "github.com/Project-Helianthus/example-reg" || got.Version != "v1.0.0" {
		t.Errorf("module/version = %q/%q", got.Module, got.Version)
	}
}

func TestDecodeStillRejectsMissingRequiredAndUnknownFamilyMember(t *testing.T) {
	c, _ := load(t)
	c.Families = []Family{declaredFamily()}
	missing, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(missing); err == nil || !strings.Contains(err.Error(), "missing families") {
		t.Errorf("missing infrastructure: err = %v", err)
	}
	withExtra, err := json.Marshal(documentWith(t, declaredFamily()))
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.Replace(string(withExtra), `"family_id":"example-family"`, `"unexpected":1,"family_id":"example-family"`, 1)
	if doc == string(withExtra) {
		t.Fatal("mutation did not apply")
	}
	if _, err := Decode([]byte(doc)); err == nil {
		t.Error("Family object with an unknown member accepted")
	}
}

func TestDecodeRejectsDuplicateKeysAtEveryLevel(t *testing.T) {
	_, raw := load(t)
	text := string(raw)
	cases := map[string]string{
		"top level":      strings.Replace(text, `"contract":`, `"contract": "x", "contract":`, 1),
		"row state":      strings.Replace(text, `"state": "unknown"`, `"state": "unknown", "state": "qualified"`, 1),
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

func TestVerificationRequiredWithExactlyTwoValues(t *testing.T) {
	for _, v := range []Verification{VerificationNotTestedOnHardware, VerificationTestedOnHardware} {
		f := declaredFamily()
		f.Verification = v
		if v == VerificationNotTestedOnHardware {
			f.Evidence = []Evidence{specificationEvidence()}
		}
		if err := f.Validate(); err != nil {
			t.Errorf("verification %q: %v", v, err)
		}
	}
	for name, v := range map[string]Verification{"missing": "", "unknown value": "tested", "case variant": "Tested_On_Hardware", "legacy wording": "unverified"} {
		f := declaredFamily()
		f.Verification = v
		if f.Validate() == nil {
			t.Errorf("%s: verification %q accepted", name, v)
		}
	}
	_, raw := load(t)
	text := string(raw)
	for name, doc := range map[string]string{
		"decoded member missing": strings.Replace(text, `"verification": "not_tested_on_hardware"`, `"verification": ""`, 1),
		"decoded unknown value":  strings.Replace(text, `"not_tested_on_hardware"`, `"seen_on_one_device"`, 1),
	} {
		if doc == text {
			t.Fatalf("%s: mutation did not apply", name)
		}
		if _, err := Decode([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTestedOnHardwareNeedsHardwareTestRecord(t *testing.T) {
	f := declaredFamily()
	f.Evidence = []Evidence{specificationEvidence()}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "hardware_test_record") {
		t.Errorf("tested_on_hardware without a hardware_test_record entry: err = %v", err)
	}
	f.Evidence = []Evidence{specificationEvidence(), hardwareEvidence()}
	if err := f.Validate(); err != nil {
		t.Errorf("with the record: %v", err)
	}
}

func TestVerificationNeverChangesState(t *testing.T) {
	for _, v := range []Verification{VerificationTestedOnHardware, VerificationNotTestedOnHardware} {
		f := declaredFamily()
		f.Verification = v
		f.Evidence = []Evidence{specificationEvidence(), hardwareEvidence()}
		if err := f.Validate(); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
		if f.State != semreg.QualificationQualified {
			t.Errorf("%s: state moved to %q", v, f.State)
		}
	}
	f := notComposedFamily()
	f.Verification = VerificationNotTestedOnHardware
	if err := f.Validate(); err != nil {
		t.Errorf("a family with no hardware test stays valid at state %q: %v", f.State, err)
	}
	q := declaredFamily()
	q.Verification = VerificationNotTestedOnHardware
	if err := q.Validate(); err != nil {
		t.Errorf("a qualified family with no hardware test is held back: %v", err)
	}
}

func TestQualifiedNeedsSpecificationEvidenceAndReason(t *testing.T) {
	f := declaredFamily()
	f.Verification = VerificationNotTestedOnHardware
	f.Evidence = []Evidence{specificationEvidence()}
	if err := f.Validate(); err != nil {
		t.Fatalf("qualified with specification evidence and reason: %v", err)
	}
	noSpec := f
	noSpec.Evidence = []Evidence{{Owner: "Project-Helianthus/helianthus-docs-semantic@v1.0.0", Kind: "semantic_contract", Locator: "api/v1/x.md", Statement: "a contract page"}}
	if err := noSpec.Validate(); err == nil || !strings.Contains(err.Error(), "specification") {
		t.Errorf("qualified without a specification entry: err = %v", err)
	}
	for _, reason := range []Reason{ReasonNoProducer, ReasonSemanticMappingUnknown, ReasonUnknownPendingNormativeSource, ReasonSpecificationConflict} {
		other := f
		other.Reason = reason
		if err := other.Validate(); err == nil || !strings.Contains(err.Error(), string(ReasonDeclaredBySpecification)) {
			t.Errorf("qualified with reason %q: err = %v", reason, err)
		}
	}
}

func TestDeclaredReasons(t *testing.T) {
	if ReasonDeclaredBySpecification != "declared_by_specification" || ReasonSpecificationConflict != "specification_conflict" {
		t.Fatal("reason wire values")
	}
	conflict := notComposedFamily()
	conflict.State = semreg.QualificationUnknown
	conflict.Reason = ReasonSpecificationConflict
	conflict.Evidence = []Evidence{specificationEvidence(), specificationEvidence()}
	if err := conflict.Validate(); err != nil {
		t.Errorf("specification_conflict: %v", err)
	}
	for _, r := range []Reason{ReasonSemanticMappingUnknown, ReasonUnknownPendingNormativeSource, ReasonNoProducer} {
		f := notComposedFamily()
		f.Reason = r
		if err := f.Validate(); err != nil {
			t.Errorf("kept reason %q: %v", r, err)
		}
	}
}

func TestRemovedReasonsRejected(t *testing.T) {
	removed := []Reason{
		"offline_profile_only", "offline_mapping_gate_only", "family_compatible_offline", "no_admissible_profile",
		"qualification_test_ready_offline", "evidence_blocked", "no_go_missing_evidence", "offline_replay_only",
		"legacy_layer_composition",
	}
	_, raw := load(t)
	text := string(raw)
	for _, r := range removed {
		f := notComposedFamily()
		f.Reason = r
		if f.Validate() == nil {
			t.Errorf("reason %q accepted by Family.Validate", r)
		}
		doc := strings.Replace(text, `"no_producer"`, `"`+string(r)+`"`, 1)
		if doc == text {
			t.Fatalf("%s: mutation did not apply", r)
		}
		if _, err := Decode([]byte(doc)); err == nil {
			t.Errorf("reason %q accepted by Decode", r)
		}
	}
}

func TestDriverFamilyRecordsValidate(t *testing.T) {
	qualified := declaredFamily()
	qualified.Verification = VerificationNotTestedOnHardware
	qualified.Evidence = []Evidence{specificationEvidence()}
	for name, f := range map[string]Family{
		"qualified citing the specification constant":    qualified,
		"hardware verified citing the hardware constant": declaredFamily(),
		"not composed": notComposedFamily(),
	} {
		if err := f.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if EvidenceKindSpecification != "specification" || EvidenceKindHardwareTestRecord != "hardware_test_record" {
		t.Error("evidence kind wire values")
	}
}

func TestClaimedCompositionNeedsModuleVersionAndVerification(t *testing.T) {
	for _, composition := range []Composition{CompositionComposed, CompositionComposedLegacy, CompositionProviderLocal} {
		f := declaredFamily()
		f.Composition = composition
		if err := f.Validate(); err != nil {
			t.Fatalf("%s: %v", composition, err)
		}
		unverified := f
		unverified.CompositionVerified = false
		if unverified.Validate() == nil {
			t.Errorf("%s: composition_verified=false accepted", composition)
		}
		noModule := f
		noModule.Module = ""
		if noModule.Validate() == nil {
			t.Errorf("%s: missing module accepted", composition)
		}
		noVersion := f
		noVersion.Version = ""
		if noVersion.Validate() == nil {
			t.Errorf("%s: missing version accepted", composition)
		}
	}
	verifiedWithoutEvidence := notComposedFamily()
	verifiedWithoutEvidence.Composition = CompositionComposed
	verifiedWithoutEvidence.CompositionVerified = true
	if verifiedWithoutEvidence.Validate() == nil {
		t.Error("composition_verified without module and version accepted")
	}
}

func TestEmptyModuleAndVersionAreOmitted(t *testing.T) {
	raw, err := json.Marshal(notComposedFamily())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"module"`)) || bytes.Contains(raw, []byte(`"version"`)) {
		t.Errorf("empty module or version encoded: %s", raw)
	}
	claimed, err := json.Marshal(declaredFamily())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(claimed, []byte(`"module":"github.com/Project-Helianthus/example-reg"`)) || !bytes.Contains(claimed, []byte(`"version":"v1.0.0"`)) {
		t.Errorf("module and version missing from %s", claimed)
	}
	idx := func(s string) int { return bytes.Index(claimed, []byte(s)) }
	if idx(`"composition_verified"`) >= idx(`"verification"`) || idx(`"verification"`) >= idx(`"module"`) || idx(`"module"`) >= idx(`"version"`) {
		t.Errorf("member order: %s", claimed)
	}
}

func TestNotComposedRowsCannotBeVerified(t *testing.T) {
	c, _ := load(t)
	infra := familyByID(t, c, "infrastructure")
	if infra.Composition != CompositionNotComposed {
		t.Fatalf("fixture row composition = %q", infra.Composition)
	}
	if err := infra.Validate(); err != nil {
		t.Fatal(err)
	}
	verified := infra
	verified.CompositionVerified = true
	if verified.Validate() == nil {
		t.Error("not_composed with composition_verified=true accepted")
	}
	withModule := infra
	withModule.Module = "github.com/Project-Helianthus/example-reg"
	if withModule.Validate() == nil {
		t.Error("not_composed carrying a module accepted")
	}
	withVersion := infra
	withVersion.Version = "v1.0.0"
	if withVersion.Validate() == nil {
		t.Error("not_composed carrying a version accepted")
	}
}

func TestDecodeRejectsCaseVariantMemberAliases(t *testing.T) {
	_, raw := load(t)
	text := string(raw)
	cases := map[string]string{
		"duplicate State alias":    strings.Replace(text, `"state": "unknown"`, `"state": "unknown", "State": "qualified"`, 1),
		"lone State alias":         strings.Replace(text, `"state": "unknown"`, `"State": "unknown"`, 1),
		"COMPOSITION_VERIFIED":     strings.Replace(text, `"composition_verified": false`, `"COMPOSITION_VERIFIED": false`, 1),
		"VERIFICATION":             strings.Replace(text, `"verification":`, `"VERIFICATION":`, 1),
		"evidence Kind alias":      strings.Replace(text, `"kind": "semantic_contract"`, `"Kind": "semantic_contract"`, 1),
		"evidence duplicate alias": strings.Replace(text, `"kind": "semantic_contract"`, `"kind": "semantic_contract", "KIND": "x"`, 1),
		"top-level Contract":       strings.Replace(text, `"contract":`, `"Contract":`, 1),
	}
	for name, doc := range cases {
		if doc == text {
			t.Fatalf("%s: mutation did not apply", name)
		}
		if _, err := Decode([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCommitPinsRequireLowercaseHex(t *testing.T) {
	c, _ := load(t)
	good40 := strings.Repeat("a", 40)
	for name, sha := range map[string]string{
		"wrong length": good40[:39], "uppercase": strings.Repeat("A", 40), "non-hex": strings.Repeat("z", 40),
	} {
		clone := c
		clone.SemanticDocsPin = "Project-Helianthus/helianthus-docs-semantic@" + sha
		if clone.Validate() == nil {
			t.Errorf("docs pin %s accepted", name)
		}
	}
	clone := c
	clone.SemanticDocsPin = "Project-Helianthus/helianthus-docs-semantic@" + good40
	if err := clone.Validate(); err != nil {
		t.Errorf("a full lowercase pin: %v", err)
	}
}

func TestDriverEvidenceOwnerIsNotACommitPin(t *testing.T) {
	f := declaredFamily()
	f.Evidence = []Evidence{
		{Owner: "Project-Helianthus/example-reg@v1.0.0", Kind: EvidenceKindSpecification, Locator: "docs/specification.md", Statement: "declares the mapping"},
		{Owner: "Project-Helianthus/example-reg#7", Kind: EvidenceKindHardwareTestRecord, Locator: "https://example.invalid/issues/7", Statement: "a public hardware test record"},
	}
	if err := f.Validate(); err != nil {
		t.Errorf("a tag or thread owner is not rejected for its commit-pin form: %v", err)
	}
}

func TestFamilyValidateKeepsStructuralChecks(t *testing.T) {
	for name, mutate := range map[string]func(*Family){
		"empty identity":      func(f *Family) { f.FamilyID = "" },
		"empty name":          func(f *Family) { f.Name = "" },
		"unknown state":       func(f *Family) { f.State = "evidence_blocked" },
		"unknown composition": func(f *Family) { f.Composition = "gateway_composed" },
		"no evidence":         func(f *Family) { f.Evidence = nil },
		"incomplete evidence": func(f *Family) {
			f.Evidence = append([]Evidence(nil), f.Evidence...)
			f.Evidence[0].Statement = ""
		},
	} {
		f := declaredFamily()
		mutate(&f)
		if f.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
