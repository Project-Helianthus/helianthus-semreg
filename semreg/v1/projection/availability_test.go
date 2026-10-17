package projection_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

func m20Validators() []semreg.PackValidator {
	return []semreg.PackValidator{evse.New(), infrastructure.New(), pv.New(), storage.New(), thermal.New()}
}

func m20Evaluate(t *testing.T, s semreg.Snapshot) semreg.EvaluationView {
	t.Helper()
	v, err := semreg.EvaluateSnapshot(s, semreg.EvaluationContext{EvaluatedAt: s.EvaluatedAt, EvaluateMonotonic: s.EvaluateMonotonic})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func m20Fixture(t *testing.T) semreg.Snapshot {
	t.Helper()
	s, _ := revisionAxesFixture(t)
	candidate := s.Facts[0].Candidates[0]
	s.Facts = []semreg.FactEnvelope{}
	s.Services = []semreg.ServiceInstance{}
	s.Capabilities = []semreg.CapabilityInstance{}
	for _, validator := range m20Validators() {
		metadata := validator.(semreg.PackMetadataProvider).Metadata()
		var field semreg.FieldMetadata
		for _, f := range metadata.Fields {
			if f.CanonicalUnit != nil {
				field = f
				break
			}
		}
		label := "synthetic"
		key := semreg.FactKey{PackID: metadata.Pack.ID, PackVersion: metadata.Pack.Version, FactID: field.Ref.ID, Dimensions: []semreg.Dimension{{ID: field.Dimension.ID, Value: semreg.Value{Kind: semreg.ValueText, Text: &label}}}}
		c := candidate
		c.CandidateID = semreg.CandidateID("candidate:" + string(metadata.Pack.ID))
		c.Key = key
		c.Value = &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: field.CanonicalUnit.ID}}
		c.Quality.Reasons = []semreg.DefinitionID{"reason.synthetic"}
		s.Facts = append(s.Facts, semreg.FactEnvelope{AssetID: s.AssetID, Key: key, Candidates: []semreg.FactCandidate{c}, Conflicts: []semreg.Conflict{}, Revision: "1"})
		var cap semreg.CapabilityMetadata
		for _, item := range metadata.Capabilities {
			if bytes.Contains([]byte(item.Ref.ID), []byte(".read.")) {
				cap = item
				break
			}
		}
		id := string(metadata.Pack.ID)
		service := semreg.ServiceInstance{InstanceID: semreg.ServiceInstanceID("service:" + id), AssetID: s.AssetID, Definition: cap.Service, BindingID: s.Bindings[0].BindingID, SourceEpochID: s.Bindings[0].SourceEpochID, DriverGeneration: s.Bindings[0].DriverGeneration, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}
		s.Services = append(s.Services, service)
		s.Capabilities = append(s.Capabilities, semreg.CapabilityInstance{InstanceID: semreg.CapabilityInstanceID("capability:" + id), AssetID: s.AssetID, ServiceInstance: service.InstanceID, Definition: cap.Ref, BindingID: service.BindingID, SourceEpochID: service.SourceEpochID, DriverGeneration: service.DriverGeneration, Qualification: service.Qualification, Availability: service.Availability, Constraints: []semreg.TypedField{}, ActivationEvidence: []semreg.EvidenceRef{s.Bindings[0].NativeResource}, Revision: "1"})
	}
	sort.Slice(s.Facts, func(i, j int) bool {
		a, _ := semreg.CanonicalJSON(s.Facts[i].Key)
		b, _ := semreg.CanonicalJSON(s.Facts[j].Key)
		return bytes.Compare(a, b) < 0
	})
	sealProjectionSnapshot(t, &s)
	return s
}

func m20Registry(t *testing.T) *semreg.PackMetadataRegistry {
	t.Helper()
	r, err := packs.NewMetadataRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestM20FivePacks(t *testing.T) {
	r := m20Registry(t)
	for _, state := range []string{"available", "absent", "withheld", "unknown"} {
		t.Run(state, func(t *testing.T) {
			s := m20Fixture(t)
			if state == "absent" {
				s.Services = []semreg.ServiceInstance{}
				s.Capabilities = []semreg.CapabilityInstance{}
				s.Facts = []semreg.FactEnvelope{}
			}
			if state == "unknown" {
				for i := range s.Capabilities {
					s.Capabilities[i].Qualification = semreg.QualificationUnknown
				}
				for i := range s.Facts {
					s.Facts[i].Candidates[0].Quality.Qualification = semreg.QualificationUnknown
					s.Facts[i].Candidates[0].Quality.Validity = semreg.ValidityUnknown
					s.Facts[i].Candidates[0].Quality.Promotion = semreg.PromotionUnpromoted
				}
			}
			sealProjectionSnapshot(t, &s)
			v := m20Evaluate(t, s)
			before := revisionAxesJSON(t, s)
			out, err := projection.DerivePackAvailability(r, s, v)
			if err != nil {
				t.Fatal(err)
			}
			if out.AssetID != s.AssetID || out.SnapshotID != s.SnapshotID || out.Revisions != s.Revisions || out.EvaluationDigest != v.EvaluationDigest || !reflect.DeepEqual(out.Context, v.Context) {
				t.Fatal("identity lost")
			}
			if len(out.Packs) != 5 {
				t.Fatalf("packs=%d", len(out.Packs))
			}
			for i, row := range out.Packs {
				if row.Pack != r.Packs()[i] {
					t.Fatal("pack ordering/reference")
				}
				if state == "absent" {
					if len(row.Services)+len(row.Capabilities)+len(row.Facts)+len(row.Retained) != 0 {
						t.Fatal("metadata synthesized records")
					}
					continue
				}
				if len(row.Services) != 1 || len(row.Capabilities) != 1 || len(row.Facts) != 1 {
					t.Fatalf("incomplete row %+v", row)
				}
				if !reflect.DeepEqual(row.Capabilities[0], s.Capabilities[i]) {
					t.Fatal("capability promoted or changed")
				}
				if !reflect.DeepEqual(row.Facts[0].Envelope, s.Facts[i]) {
					t.Fatal("stored quality, unknown axes or evidence changed")
				}
				if row.Facts[0].Envelope.Candidates[0].Value.Quantity.Number.Coefficient != "0" || row.Facts[0].Envelope.Candidates[0].Quality.Reasons[0] != "reason.synthetic" {
					t.Fatal("zero or reasons lost")
				}
				if !reflect.DeepEqual(row.Facts[0].Evaluation, []semreg.EvaluatedFact{v.Facts[i]}) {
					t.Fatal("evaluated candidate join")
				}
				if state == "withheld" {
					reason := semreg.DefinitionID("reason.presentation_policy")
					requested := []projection.RequestedItem{{Kind: projection.ItemCapability, ItemID: row.Capabilities[0].Definition.ID}}
					dispositions := []projection.ProjectionDisposition{{Kind: projection.ItemCapability, ItemID: requested[0].ItemID, Outcome: projection.ProjectionWithheld, Reason: &reason, SourceKeys: []semreg.FactKey{row.Facts[0].Envelope.Key}, Loss: []projection.LossDetail{}}}
					manifest := projection.ProjectionManifest{TargetID: "target:synthetic", TargetVersion: "test-1", KernelVersion: semreg.ContractKernelV1, PackVersions: r.Packs(), MappingRevision: "1"}
					report, err := projection.Project(s, manifest, requested, dispositions, nil)
					if err != nil {
						t.Fatal(err)
					}
					if report.Dispositions[0].Outcome != projection.ProjectionWithheld || *report.Dispositions[0].Reason != reason {
						t.Fatal("withholding lost")
					}
				}
			}
			if !bytes.Equal(before, revisionAxesJSON(t, s)) {
				t.Fatal("input mutated")
			}
		})
	}
}

func TestM20RejectInconsistentEvaluation(t *testing.T) {
	s := m20Fixture(t)
	r := m20Registry(t)
	for _, tc := range []struct {
		name string
		edit func(*semreg.EvaluationView)
	}{
		{"snapshot", func(v *semreg.EvaluationView) { v.SnapshotID = "snapshot:other" }},
		{"semantic", func(v *semreg.EvaluationView) { v.Revisions.Semantic = "9" }},
		{"identity", func(v *semreg.EvaluationView) { v.Revisions.Identity = "9" }},
		{"facts", func(v *semreg.EvaluationView) { v.Revisions.Facts = "9" }},
		{"services", func(v *semreg.EvaluationView) { v.Revisions.Services = "9" }},
		{"capabilities", func(v *semreg.EvaluationView) { v.Revisions.Capabilities = "9" }},
		{"candidate_id", func(v *semreg.EvaluationView) { v.Facts[len(v.Facts)-1].CandidateID = "candidate:other" }},
		{"candidate_revision", func(v *semreg.EvaluationView) { v.Facts[0].CandidateRevision = "9" }},
		{"missing_candidate", func(v *semreg.EvaluationView) { v.Facts = v.Facts[1:] }},
		{"extra_candidate", func(v *semreg.EvaluationView) {
			v.Facts = append(v.Facts, semreg.EvaluatedFact{CandidateID: "candidate:zz-extra", CandidateRevision: "1", Freshness: semreg.FreshnessFresh, EffectiveAvailability: semreg.AvailabilityAvailable})
		}},
		{"forged_freshness", func(v *semreg.EvaluationView) {
			v.Facts[0].Freshness = semreg.FreshnessStale
			v.Facts[0].EffectiveAvailability = semreg.AvailabilityDegraded
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := m20Evaluate(t, s)
			tc.edit(&v)
			var err error
			v.EvaluationDigest, err = v.EvaluationDigestValue()
			if err != nil {
				t.Fatal(err)
			}
			out, err := projection.DerivePackAvailability(r, s, v)
			if err == nil || !reflect.DeepEqual(out, projection.PackAvailabilityView{}) {
				t.Fatalf("inconsistent evaluation accepted: %v", err)
			}
		})
	}
}

func TestM20Detached(t *testing.T) {
	s := m20Fixture(t)
	r := m20Registry(t)
	v := m20Evaluate(t, s)
	beforeS, beforeV := revisionAxesJSON(t, s), revisionAxesJSON(t, v)
	a, err := projection.DerivePackAvailability(r, s, v)
	if err != nil {
		t.Fatal(err)
	}
	b, err := projection.DerivePackAvailability(r, s, v)
	if err != nil {
		t.Fatal(err)
	}
	beforeB := revisionAxesJSON(t, b)
	if !bytes.Equal(revisionAxesJSON(t, a), beforeB) {
		t.Fatal("nondeterministic")
	}
	a.Packs[0].Pack.Version = "9.0.0"
	a.Packs[0].Capabilities[0].ActivationEvidence[0].Owner = "owner.changed"
	a.Packs[0].Facts[0].Envelope.Candidates[0].Value.Quantity.Number.Coefficient = "99"
	a.Packs[0].Facts[0].Envelope.Candidates[0].Quality.Reasons[0] = "reason.changed"
	*a.Packs[0].Facts[0].Envelope.Key.Dimensions[0].Value.Text = "changed"
	a.Packs[0].Facts[0].Evaluation[0].Freshness = semreg.FreshnessExpired
	if !bytes.Equal(beforeS, revisionAxesJSON(t, s)) || !bytes.Equal(beforeV, revisionAxesJSON(t, v)) || !bytes.Equal(beforeB, revisionAxesJSON(t, b)) {
		t.Fatal("result aliases input or another result")
	}
	c, err := projection.DerivePackAvailability(r, s, v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeB, revisionAxesJSON(t, c)) {
		t.Fatal("metadata mutated")
	}
}

// Keep JSON cloning local to synthetic test data.
func m20Clone[T any](t *testing.T, value T) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(revisionAxesJSON(t, value), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestM20LaterEvaluationAndUnknownFreshness(t *testing.T) {
	s := m20Fixture(t)
	r := m20Registry(t)
	original := m20Evaluate(t, s)
	for _, tc := range []struct {
		name         string
		context      semreg.EvaluationContext
		freshness    semreg.Freshness
		availability semreg.Availability
	}{
		{"stale", semreg.EvaluationContext{EvaluatedAt: s.EvaluatedAt, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: s.EvaluateMonotonic.ClockEpochID, Nanoseconds: "2"}}, semreg.FreshnessStale, semreg.AvailabilityDegraded},
		{"expired", semreg.EvaluationContext{EvaluatedAt: s.EvaluatedAt, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: s.EvaluateMonotonic.ClockEpochID, Nanoseconds: "3"}}, semreg.FreshnessExpired, semreg.AvailabilityUnavailable},
		{"unknown", semreg.EvaluationContext{EvaluatedAt: s.EvaluatedAt, EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:unrelated", Nanoseconds: "1"}}, semreg.FreshnessUnknown, semreg.AvailabilityDegraded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := semreg.EvaluateSnapshot(s, tc.context)
			if err != nil {
				t.Fatal(err)
			}
			out, err := projection.DerivePackAvailability(r, s, v)
			if err != nil {
				t.Fatal(err)
			}
			if out.EvaluationDigest == original.EvaluationDigest || !reflect.DeepEqual(out.Context, tc.context) {
				t.Fatal("evaluation identity collapsed")
			}
			for _, row := range out.Packs {
				fact := row.Facts[0]
				if fact.Evaluation[0].Freshness != tc.freshness || fact.Evaluation[0].EffectiveAvailability != tc.availability {
					t.Fatal("evaluated quality lost")
				}
				if fact.Envelope.Candidates[0].Quality.Availability != semreg.AvailabilityAvailable || fact.Envelope.Candidates[0].Value.Quantity.Number.Coefficient != "0" || fact.Envelope.Candidates[0].Value.Boolean != nil {
					t.Fatal("unknown/expiry rewrote stored value or quality")
				}
				if !reflect.DeepEqual(fact.Envelope.Candidates[0].Times, s.Facts[0].Candidates[0].Times) {
					t.Fatal("original times renewed")
				}
			}
		})
	}
}

func TestM20RetainedExpiryAndDetachment(t *testing.T) {
	s := m20Fixture(t)
	r := m20Registry(t)
	old := s.Bindings[0]
	old.BindingID = "binding:historical"
	old.DriverGeneration = "2"
	old.State = semreg.BindingFenced
	s.Bindings = append(s.Bindings, old)
	sort.Slice(s.Bindings, func(i, j int) bool { return s.Bindings[i].BindingID < s.Bindings[j].BindingID })
	s.Fences = []semreg.GenerationFence{{SourceID: old.SourceID, SourceEpochID: old.SourceEpochID, DriverGeneration: old.DriverGeneration, Reason: "reason.synthetic", Evidence: []semreg.EvidenceRef{old.NativeResource}, Revision: "1"}}
	cursor := s.Cursors[0]
	cursor.DriverGeneration = "2"
	cursor.Fenced = true
	s.Cursors = append(s.Cursors, cursor)
	for _, envelope := range s.Facts {
		c := m20Clone(t, envelope.Candidates[0])
		c.BindingID = &old.BindingID
		c.DriverGeneration = &old.DriverGeneration
		c.Origin.BindingID = &old.BindingID
		s.Retained = append(s.Retained, semreg.RetainedObservationRecord{Contract: semreg.ContractRetainedObservationV1, Candidate: c, Removal: semreg.RetainedRemovalGenerationFence})
	}
	// Candidate IDs are distinct here; the kernel owns complete retained ordering.
	sort.Slice(s.Retained, func(i, j int) bool { return s.Retained[i].Candidate.CandidateID < s.Retained[j].Candidate.CandidateID })
	sealProjectionSnapshot(t, &s)
	v := m20Evaluate(t, s)
	beforeS, beforeV := revisionAxesJSON(t, s), revisionAxesJSON(t, v)
	out, err := projection.DerivePackAvailability(r, s, v)
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range out.Packs {
		if len(row.Facts) != 1 || len(row.Retained) != 1 || !reflect.DeepEqual(row.Retained[0], v.Retained[i]) {
			t.Fatal("retained replaced current or changed")
		}
	}
	out.Packs[0].Retained[0].Observation.Candidate.Value.Quantity.Number.Coefficient = "99"
	if !bytes.Equal(beforeS, revisionAxesJSON(t, s)) || !bytes.Equal(beforeV, revisionAxesJSON(t, v)) {
		t.Fatal("retained aliases input")
	}
	context := v.Context
	context.EvaluateMonotonic.Nanoseconds = "3"
	expired, err := semreg.EvaluateSnapshot(s, context)
	if err != nil {
		t.Fatal(err)
	}
	later, err := projection.DerivePackAvailability(r, s, expired)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range later.Packs {
		if len(row.Retained) != 0 || len(row.Facts) != 1 {
			t.Fatal("expired retained survived or erased current")
		}
	}
	// A correctly re-signed omission or resurrection still contradicts the kernel.
	omitted := m20Clone(t, v)
	omitted.Retained = nil
	resurrected := m20Clone(t, expired)
	resurrected.Retained = v.Retained
	for _, bad := range []semreg.EvaluationView{omitted, resurrected} {
		bad.EvaluationDigest, err = bad.EvaluationDigestValue()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = projection.DerivePackAvailability(r, s, bad); err == nil {
			t.Fatal("forged retained set accepted")
		}
	}
}

func TestM20TombstonesAndIncompleteInputs(t *testing.T) {
	s := m20Fixture(t)
	r := m20Registry(t)
	for i := range s.Facts {
		c := &s.Facts[i].Candidates[0]
		c.Value = nil
		c.Quality.Availability = semreg.AvailabilityWithdrawn
		c.Quality.Promotion = semreg.PromotionUnpromoted
	}
	for i := range s.Capabilities {
		s.Capabilities[i].Availability = semreg.AvailabilityWithdrawn
		s.Services[i].Availability = semreg.AvailabilityWithdrawn
	}
	sealProjectionSnapshot(t, &s)
	v := m20Evaluate(t, s)
	out, err := projection.DerivePackAvailability(r, s, v)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range out.Packs {
		if row.Facts[0].Envelope.Candidates[0].Value != nil || row.Capabilities[0].Availability != semreg.AvailabilityWithdrawn {
			t.Fatal("tombstone resurrected or dropped")
		}
	}
	if _, err := projection.DerivePackAvailability(nil, s, v); err == nil {
		t.Fatal("nil metadata accepted")
	}
	empty, err := semreg.NewPackMetadataRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projection.DerivePackAvailability(empty, s, v); err == nil {
		t.Fatal("missing five-pack metadata accepted")
	}
	if _, err := projection.DerivePackAvailability(r, semreg.Snapshot{}, v); err == nil {
		t.Fatal("missing snapshot accepted")
	}
	if _, err := projection.DerivePackAvailability(r, s, semreg.EvaluationView{}); err == nil {
		t.Fatal("missing evaluation accepted")
	}
	missing := m20Clone(t, s)
	missing.Facts = nil
	if _, err := projection.DerivePackAvailability(r, missing, v); err == nil {
		t.Fatal("missing snapshot collection accepted")
	}
	missingV := m20Clone(t, v)
	missingV.Facts = nil
	if _, err := projection.DerivePackAvailability(r, s, missingV); err == nil {
		t.Fatal("missing evaluation collection accepted")
	}
	v.EvaluationDigest = "sha256:invalid"
	if _, err := projection.DerivePackAvailability(r, s, v); err == nil {
		t.Fatal("bad evaluation digest accepted")
	}
}

func TestM20ExactReferences(t *testing.T) {
	r := m20Registry(t)
	for _, tc := range []struct {
		name    string
		edit    func(*semreg.DefinitionRef)
		outside bool
	}{
		{"pack_id", func(d *semreg.DefinitionRef) { d.Pack.ID = "pack.synthetic.other" }, true},
		{"pack_version", func(d *semreg.DefinitionRef) { d.Pack.Version = "9.0.0" }, true},
		{"definition_id", func(d *semreg.DefinitionRef) { d.ID = "capability.synthetic.other" }, false},
		{"definition_version", func(d *semreg.DefinitionRef) { d.Version = "9.0.0" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := m20Fixture(t)
			tc.edit(&s.Capabilities[0].Definition)
			if tc.outside {
				s.Services[0].Definition.Pack = s.Capabilities[0].Definition.Pack
			}
			sealProjectionSnapshot(t, &s)
			v := m20Evaluate(t, s)
			out, err := projection.DerivePackAvailability(r, s, v)
			if tc.outside {
				if err != nil {
					t.Fatal(err)
				}
				if len(out.Packs[0].Capabilities) != 0 || len(out.Packs[0].Services) != 0 {
					t.Fatal("inexact PackRef matched")
				}
			} else if err == nil {
				t.Fatal("inexact DefinitionRef matched")
			}
		})
	}
	s := m20Fixture(t)
	// Both definitions exist, but the owning service relation does not.
	for _, service := range evse.Metadata().Services {
		if !r.ServiceOwnsCapability(service.Ref, s.Capabilities[0].Definition) {
			s.Services[0].Definition = service.Ref
			break
		}
	}
	if r.ServiceOwnsCapability(s.Services[0].Definition, s.Capabilities[0].Definition) {
		t.Fatal("fixture must use a different service")
	}
	sealProjectionSnapshot(t, &s)
	if _, err := projection.DerivePackAvailability(r, s, m20Evaluate(t, s)); err == nil {
		t.Fatal("wrong service owner accepted")
	}
}

func TestM20AlternativesConflictsAndPartialPublication(t *testing.T) {
	s := m20Fixture(t)
	r := m20Registry(t)
	kernel, err := semreg.NewPublicationKernel(s.AssetID, m20Validators()...)
	if err != nil {
		t.Fatal(err)
	}
	batch := semreg.PublicationBatch{Contract: semreg.ContractKernelV1, BatchID: "batch:synthetic", AssetID: s.AssetID, SourceID: s.Sources[0].SourceID, SourceEpochID: s.Sources[0].SourceEpochID, DriverGeneration: "1", Sequence: "1", ExpectedSemanticRevision: "0", ObservedAt: s.EvaluatedAt,
		SourceUpserts: s.Sources, SourceRetirements: []semreg.SourceEpochID{}, BindingUpserts: s.Bindings, IdentityLinkUpserts: s.IdentityLinks, FactUpserts: []semreg.FactCandidate{}, FactWithdrawals: []semreg.CandidateID{}, ServiceUpserts: s.Services, ServiceWithdrawals: []semreg.ServiceInstanceID{}, CapabilityUpserts: s.Capabilities, CapabilityWithdrawals: []semreg.CapabilityInstanceID{}, GenerationFences: []semreg.GenerationFence{}}
	for _, envelope := range s.Facts {
		c := envelope.Candidates[0]
		batch.FactUpserts = append(batch.FactUpserts, c)
		alternative := m20Clone(t, c)
		alternative.CandidateID = semreg.CandidateID(string(c.CandidateID) + ":other")
		alternative.Value.Quantity.Number.Coefficient = "1"
		batch.FactUpserts = append(batch.FactUpserts, alternative)
	}
	sort.Slice(batch.FactUpserts, func(i, j int) bool { return batch.FactUpserts[i].CandidateID < batch.FactUpserts[j].CandidateID })
	batch.BatchDigest, err = batch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	published, _, err := kernel.Apply(batch, s.EvaluateMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	view, err := projection.DerivePackAvailability(r, published, m20Evaluate(t, published))
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range view.Packs {
		fact := row.Facts[0]
		if len(fact.Envelope.Candidates) != 2 || len(fact.Envelope.Conflicts) != 1 || len(fact.Evaluation) != 2 {
			t.Fatal("alternative/conflict lost")
		}
		if !reflect.DeepEqual(fact.Envelope, published.Facts[i]) {
			t.Fatal("stored evidence changed")
		}
		for j, c := range fact.Envelope.Candidates {
			if fact.Evaluation[j].CandidateID != c.CandidateID || fact.Evaluation[j].CandidateRevision != c.Revision {
				t.Fatal("wrong alternative evaluation")
			}
		}
	}
	// Publish just one independent update; the other current observations remain
	// last-known-good records with their original revision, value and times.
	patch := batch
	patch.BatchID = "batch:partial"
	patch.Sequence = "2"
	patch.ExpectedSemanticRevision = published.Revisions.Semantic
	patch.SourceUpserts = []semreg.SourceDescriptor{}
	patch.BindingUpserts = []semreg.NativeBinding{}
	patch.IdentityLinkUpserts = []semreg.IdentityLink{}
	patch.ServiceUpserts = []semreg.ServiceInstance{}
	patch.CapabilityUpserts = []semreg.CapabilityInstance{}
	c := m20Clone(t, published.Facts[0].Candidates[0])
	c.Value.Quantity.Number.Coefficient = "2"
	c.Revision = "2"
	patch.FactUpserts = []semreg.FactCandidate{c}
	patch.BatchDigest, err = patch.ComputedDigest()
	if err != nil {
		t.Fatal(err)
	}
	partial, _, err := kernel.Apply(patch, s.EvaluateMonotonic)
	if err != nil {
		t.Fatal(err)
	}
	derived, err := projection.DerivePackAvailability(r, partial, m20Evaluate(t, partial))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(derived.Packs); i++ {
		if !reflect.DeepEqual(derived.Packs[i].Facts[0].Envelope, published.Facts[i]) {
			t.Fatal("partial publication erased or renewed last-known-good")
		}
	}
}

func TestM20FactKeysAndOrdering(t *testing.T) {
	r := m20Registry(t)
	for _, tc := range []struct {
		name             string
		edit             func(*semreg.FactKey)
		outside, invalid bool
	}{
		{"pack_id", func(k *semreg.FactKey) { k.PackID = "pack.synthetic.other" }, true, false},
		{"pack_version", func(k *semreg.FactKey) { k.PackVersion = "9.0.0" }, true, false},
		{"fact_id", func(k *semreg.FactKey) { k.FactID = "fact.synthetic.other" }, false, true},
		{"dimension_id", func(k *semreg.FactKey) { k.Dimensions[0].ID = "dimension.synthetic.other" }, false, true},
		{"dimension_value", func(k *semreg.FactKey) { *k.Dimensions[0].Value.Text = "another" }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := m20Fixture(t)
			extra := m20Clone(t, s.Facts[0])
			tc.edit(&extra.Key)
			extra.Candidates[0].Key = m20Clone(t, extra.Key)
			extra.Candidates[0].CandidateID = "candidate:zz-extra"
			s.Facts = append(s.Facts, extra)
			sort.Slice(s.Facts, func(i, j int) bool {
				a, _ := semreg.CanonicalJSON(s.Facts[i].Key)
				b, _ := semreg.CanonicalJSON(s.Facts[j].Key)
				return bytes.Compare(a, b) < 0
			})
			sealProjectionSnapshot(t, &s)
			out, err := projection.DerivePackAvailability(r, s, m20Evaluate(t, s))
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid pack fact accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if !tc.outside {
				want = 2
			}
			if len(out.Packs[0].Facts) != want {
				t.Fatal("fact keys conflated or inexact pack matched")
			}
			if !tc.outside {
				a, _ := semreg.CanonicalJSON(out.Packs[0].Facts[0].Envelope.Key)
				b, _ := semreg.CanonicalJSON(out.Packs[0].Facts[1].Envelope.Key)
				if bytes.Compare(a, b) >= 0 {
					t.Fatal("fact ordering changed")
				}
			}
		})
	}
	s := m20Fixture(t)
	service := m20Clone(t, s.Services[0])
	service.InstanceID += "-other"
	s.Services = append(s.Services, service)
	capability := m20Clone(t, s.Capabilities[0])
	capability.InstanceID += "-other"
	capability.ServiceInstance = service.InstanceID
	s.Capabilities = append(s.Capabilities, capability)
	sort.Slice(s.Services, func(i, j int) bool { return s.Services[i].InstanceID < s.Services[j].InstanceID })
	sort.Slice(s.Capabilities, func(i, j int) bool { return s.Capabilities[i].InstanceID < s.Capabilities[j].InstanceID })
	sealProjectionSnapshot(t, &s)
	out, err := projection.DerivePackAvailability(r, s, m20Evaluate(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Packs[0].Services) != 2 || len(out.Packs[0].Capabilities) != 2 || out.Packs[0].Services[0].InstanceID >= out.Packs[0].Services[1].InstanceID || out.Packs[0].Capabilities[0].InstanceID >= out.Packs[0].Capabilities[1].InstanceID {
		t.Fatal("instance order/identity lost")
	}
}

func TestM20EvaluationContextDigestBinding(t *testing.T) {
	s := m20Fixture(t)
	r := m20Registry(t)
	for _, tc := range []struct {
		name string
		edit func(*semreg.EvaluationContext)
	}{
		{"wall_estimate", func(c *semreg.EvaluationContext) { c.EvaluatedAt.UnixNanoseconds = "2" }},
		{"wall_domain", func(c *semreg.EvaluationContext) { c.EvaluatedAt.ClockID = "clock.other" }},
		{"wall_uncertainty", func(c *semreg.EvaluationContext) { c.EvaluatedAt.UncertaintyNS = "1" }},
		{"monotonic_epoch", func(c *semreg.EvaluationContext) { c.EvaluateMonotonic.ClockEpochID = "clock-epoch:other" }},
		{"monotonic_point", func(c *semreg.EvaluationContext) { c.EvaluateMonotonic.Nanoseconds = "2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := m20Evaluate(t, s)
			bad := m20Clone(t, original)
			tc.edit(&bad.Context)
			if _, err := projection.DerivePackAvailability(r, s, bad); semreg.ErrorIdentifier(err) != semreg.DigestMismatch {
				t.Fatalf("tampered context error: %v", err)
			}
			fresh, err := semreg.EvaluateSnapshot(s, bad.Context)
			if err != nil {
				t.Fatal(err)
			}
			out, err := projection.DerivePackAvailability(r, s, fresh)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(out.Context, bad.Context) || out.EvaluationDigest == original.EvaluationDigest {
				t.Fatal("evaluation context field lost")
			}
		})
	}
}
