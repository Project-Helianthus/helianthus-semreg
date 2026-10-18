package projection

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
)

func providerEdgesJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func providerEdgesSnapshot(t *testing.T) semreg.Snapshot {
	t.Helper()
	// Entirely synthetic: one source, binding, candidate and referenced fact.
	digest := sha256.Sum256([]byte("synthetic projection revision axes evidence"))
	evidence := semreg.EvidenceRef{
		Owner: "owner.test", Kind: "evidence.test", Digest: semreg.Digest(fmt.Sprintf("sha256:%x", digest)),
		Contract: "test.contract/v1", Access: semreg.EvidenceAccessPublic, Redaction: semreg.RedactionNone,
	}
	source := semreg.SourceID("source:test")
	epoch := semreg.SourceEpochID("epoch:test")
	binding := semreg.NativeBindingID("binding:test")
	generation := semreg.Uint64("1")
	point := semreg.TimePoint{UnixNanoseconds: "1", ClockID: "clock.test", UncertaintyNS: "0"}
	monotonic := semreg.MonotonicPoint{ClockEpochID: "clock-epoch:test", Nanoseconds: "1"}
	key := semreg.FactKey{PackID: "pack.test", PackVersion: "1.0.0", FactID: "fact.test", Dimensions: []semreg.Dimension{}}
	value := true
	snapshot := semreg.Snapshot{
		Contract: semreg.ContractKernelV1, AssetID: "asset:test",
		Revisions:   semreg.RevisionVector{Semantic: "2", Identity: "3", Facts: "4", Services: "5", Capabilities: "6"},
		EvaluatedAt: point, EvaluateMonotonic: monotonic,
		Sources: []semreg.SourceDescriptor{{
			SourceID: source, SourceEpochID: epoch, ProtocolID: "protocol.test", ProfileID: "profile.test", ProfileVersion: "test-1",
			RegistryEvidence: evidence, StartedAt: point, State: semreg.SourceCurrent, Revision: "1",
		}},
		Bindings: []semreg.NativeBinding{{
			BindingID: binding, AssetID: "asset:test", SourceID: source, SourceEpochID: epoch, DriverGeneration: generation,
			NativeResource: evidence, State: semreg.BindingCurrent, Revision: "1",
		}},
		IdentityLinks: []semreg.IdentityLink{},
		Facts: []semreg.FactEnvelope{{
			AssetID: "asset:test", Key: key, Revision: "1", Conflicts: []semreg.Conflict{},
			Candidates: []semreg.FactCandidate{{
				CandidateID: "candidate:test", Key: key, Value: &semreg.Value{Kind: semreg.ValueBoolean, Boolean: &value},
				Quality: semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified,
					Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable,
					Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}},
				Times:           semreg.Times{ReceivedAt: point, ReceiptMonotonic: monotonic, EvaluatedAt: point, EvaluateMonotonic: monotonic},
				FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy:test", Version: "1.0.0", FreshForNS: "1", RetainForNS: "2", MaxWallUncertaintyNS: "0"},
				BindingID:       &binding, SourceEpochID: &epoch, DriverGeneration: &generation,
				Origin: semreg.OriginRef{OriginID: "origin:test", Kind: semreg.OriginNativeObservation,
					SourceID: &source, SourceEpochID: &epoch, BindingID: &binding, Evidence: []semreg.EvidenceRef{evidence}},
				Evidence: []semreg.EvidenceRef{evidence}, Revision: "1",
			}},
		}},
		Services: []semreg.ServiceInstance{}, Capabilities: []semreg.CapabilityInstance{}, Fences: []semreg.GenerationFence{},
		Cursors: []semreg.PublicationCursor{{SourceID: source, SourceEpochID: epoch, DriverGeneration: generation,
			LastSequence: "1", LastBatchDigest: evidence.Digest}},
	}
	var document map[string]any
	if err := json.Unmarshal(providerEdgesJSON(t, snapshot), &document); err != nil {
		t.Fatal(err)
	}
	delete(document, "snapshot_id")
	snapshotDigest := sha256.Sum256(providerEdgesJSON(t, document))
	snapshot.SnapshotID = semreg.SnapshotID(fmt.Sprintf("sha256:%x", snapshotDigest))
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("synthetic snapshot: %v", err)
	}
	return snapshot
}
func providerEdgesSeal(t *testing.T, snapshot *semreg.Snapshot) {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	delete(document, "snapshot_id")
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	snapshot.SnapshotID = semreg.SnapshotID(fmt.Sprintf("sha256:%x", digest))
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("expanded projection snapshot: %v", err)
	}
}

func providerEdgesRetain(s *semreg.Snapshot, candidate semreg.FactCandidate) {
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
	candidate.BindingID = &old.BindingID
	candidate.DriverGeneration = &old.DriverGeneration
	candidate.Origin.BindingID = &old.BindingID
	s.Retained = []semreg.RetainedObservationRecord{{Contract: semreg.ContractRetainedObservationV1, Candidate: candidate, Removal: semreg.RetainedRemovalGenerationFence}}
	s.Facts = []semreg.FactEnvelope{}
}

// Embedding only PackValidator intentionally hides the optional metadata API.
type providerEdgesBare struct{ semreg.PackValidator }

type providerEdgesReject struct {
	semreg.PackMetadataProvider
	calls int
}

func (v *providerEdgesReject) ValidateFact(key semreg.FactKey, value *semreg.Value) error {
	v.calls++
	return errorf(semreg.InvalidValue, "synthetic fact rejection")
}

type providerEdgesMetadata struct {
	semreg.PackValidator
	metadata semreg.PackMetadata
	index    semreg.DefinitionIndex
}

func (v providerEdgesMetadata) Metadata() semreg.PackMetadata       { return v.metadata }
func (v providerEdgesMetadata) Definitions() semreg.DefinitionIndex { return v.index }

func providerEdgesCandidate() semreg.FactCandidate {
	label := "synthetic"
	return semreg.FactCandidate{Key: semreg.FactKey{
		PackID: "helianthus.pack.evse", PackVersion: "1.0.0", FactID: "evse.ac.active_power",
		Dimensions: []semreg.Dimension{{ID: "evse.dimension.phase", Value: semreg.Value{Kind: semreg.ValueText, Text: &label}}},
	}}
}
func providerEdgesRegistry(t *testing.T) *semreg.PackMetadataRegistry {
	t.Helper()
	r, err := packs.NewMetadataRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAvailabilityProviderEdgesMissingProvider(t *testing.T) {
	c := providerEdgesCandidate()
	v := providerEdgesBare{evse.New()}
	c.Value = &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: "unit.watt"}}
	if err := v.ValidateFact(c.Key, c.Value); err != nil {
		t.Fatalf("invalid control validator: %v", err)
	}
	if _, ok := any(v).(semreg.PackMetadataProvider); ok {
		t.Fatal("control exposes metadata")
	}
	if err := availabilityFactValid(providerEdgesRegistry(t), v, c); semreg.ErrorIdentifier(err) != semreg.DefinitionOwnerMissing {
		t.Fatalf("missing provider: %v", err)
	}
}

func TestAvailabilityProviderEdgesDimensions(t *testing.T) {
	label := "other"
	for _, tc := range []struct {
		name       string
		dimensions []semreg.Dimension
		want       semreg.ErrorID
	}{
		{"valid_text", providerEdgesCandidate().Key.Dimensions, ""},
		{"other_text", []semreg.Dimension{{ID: "evse.dimension.phase", Value: semreg.Value{Kind: semreg.ValueText, Text: &label}}}, ""},
		{"symbol", []semreg.Dimension{{ID: "evse.dimension.phase", Value: semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: "synthetic.dimension", Token: "phase", Known: true}}}}, semreg.InvalidValue},
		{"quantity", []semreg.Dimension{{ID: "evse.dimension.phase", Value: semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: "unit.watt"}}}}, semreg.InvalidValue},
		{"nil_text", []semreg.Dimension{{ID: "evse.dimension.phase", Value: semreg.Value{Kind: semreg.ValueText}}}, semreg.InvalidValue},
		{"missing", []semreg.Dimension{}, semreg.InvalidValue},
		{"wrong_id", []semreg.Dimension{{ID: "synthetic.other", Value: semreg.Value{Kind: semreg.ValueText, Text: &label}}}, semreg.InvalidValue},
		{"extra", []semreg.Dimension{{ID: "evse.dimension.phase", Value: semreg.Value{Kind: semreg.ValueText, Text: &label}}, {ID: "synthetic.other", Value: semreg.Value{Kind: semreg.ValueText, Text: &label}}}, semreg.InvalidValue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := providerEdgesCandidate()
			c.Key.Dimensions = tc.dimensions
			err := availabilityFactValid(providerEdgesRegistry(t), evse.New(), c)
			if (tc.want == "" && err != nil) || (tc.want != "" && (err == nil || semreg.ErrorIdentifier(err) != tc.want)) {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
}

func TestAvailabilityProviderEdgesDelegation(t *testing.T) {
	for _, nonnil := range []bool{false, true} {
		t.Run(fmt.Sprint(nonnil), func(t *testing.T) {
			v := &providerEdgesReject{PackMetadataProvider: evse.New().(semreg.PackMetadataProvider)}
			c := providerEdgesCandidate()
			if nonnil {
				c.Value = &semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: "unit.watt"}}
			}
			err := availabilityFactValid(providerEdgesRegistry(t), v, c)
			if nonnil {
				if err == nil || semreg.ErrorIdentifier(err) != semreg.InvalidValue || v.calls != 1 {
					t.Fatalf("delegation calls=%d, error=%v", v.calls, err)
				}
			} else if err != nil || v.calls != 0 {
				t.Fatalf("tombstone calls=%d, error=%v", v.calls, err)
			}
		})
	}
}

func TestAvailabilityProviderEdgesFieldDefinition(t *testing.T) {
	for _, axis := range []string{"id", "definition_version"} {
		t.Run(axis, func(t *testing.T) {
			v := evse.New()
			m := v.(semreg.PackMetadataProvider).Metadata()
			index := v.Definitions()
			original := semreg.DefinitionRef{Pack: v.Pack(), ID: "evse.ac.active_power", Version: "1.0.0"}
			replacement := original
			if axis == "id" {
				replacement.ID = "synthetic.other"
			} else {
				replacement.Version = "1.0.1"
			}
			for i := range m.Fields {
				if m.Fields[i].Ref == original {
					m.Fields[i].Ref = replacement
				}
			}
			for i := range index.Fields {
				if index.Fields[i] == original {
					index.Fields[i] = replacement
				}
			}
			sort.Slice(index.Fields, func(i, j int) bool { return index.Fields[i].ID < index.Fields[j].ID })
			wrapper := providerEdgesMetadata{v, m, index}
			r, err := semreg.NewPackMetadataRegistry(semreg.PackMetadataSource{Validator: wrapper, Metadata: m})
			if err != nil {
				t.Fatal(err)
			}
			if r.HasDefinition(original) || !r.HasDefinition(replacement) {
				t.Fatal("invalid registry control")
			}
			if err := availabilityFactValid(r, v, providerEdgesCandidate()); semreg.ErrorIdentifier(err) != semreg.DefinitionOwnerMissing {
				t.Fatalf("missing exact field: %v", err)
			}
		})
	}
}

func providerEdgesCheck(t *testing.T, registry *semreg.PackMetadataRegistry, s semreg.Snapshot, valid bool) PackAvailabilityView {
	t.Helper()
	providerEdgesSeal(t, &s)
	evaluation, err := semreg.EvaluateSnapshot(s, semreg.EvaluationContext{EvaluatedAt: s.EvaluatedAt, EvaluateMonotonic: s.EvaluateMonotonic})
	if err != nil {
		t.Fatal(err)
	}
	beforeS, beforeE := providerEdgesJSON(t, s), providerEdgesJSON(t, evaluation)
	out, err := DerivePackAvailability(registry, s, evaluation)
	if (err == nil) != valid {
		t.Fatalf("valid=%v error=%v", valid, err)
	}
	if !valid && !reflect.DeepEqual(out, PackAvailabilityView{}) {
		t.Fatal("failure exposed partial view")
	}
	if !bytes.Equal(beforeS, providerEdgesJSON(t, s)) || !bytes.Equal(beforeE, providerEdgesJSON(t, evaluation)) {
		t.Fatal("input changed")
	}
	return out
}

func TestAvailabilityProviderEdgesCandidatePaths(t *testing.T) {
	for _, retained := range []bool{false, true} {
		for _, valid := range []bool{false, true} {
			t.Run(fmt.Sprintf("retained=%v/valid=%v", retained, valid), func(t *testing.T) {
				s := providerEdgesSnapshot(t)
				c := s.Facts[0].Candidates[0]
				c.Key = providerEdgesCandidate().Key
				c.Value = nil
				c.Quality.Availability = semreg.AvailabilityWithdrawn
				c.Quality.Promotion = semreg.PromotionUnpromoted
				if !valid {
					c.Key.Dimensions[0].ID = "synthetic.other"
				}
				if retained {
					providerEdgesRetain(&s, c)
				} else {
					s.Facts = []semreg.FactEnvelope{{AssetID: s.AssetID, Key: c.Key, Candidates: []semreg.FactCandidate{c}, Conflicts: []semreg.Conflict{}, Revision: "1"}}
				}
				out := providerEdgesCheck(t, providerEdgesRegistry(t), s, valid)
				if valid {
					found := false
					for _, row := range out.Packs {
						if row.Pack == evse.New().Pack() {
							found = true
							if retained {
								if len(row.Retained) != 1 || !reflect.DeepEqual(row.Retained[0].Observation, s.Retained[0]) {
									t.Fatal("retained record lost")
								}
							} else if len(row.Facts) != 1 || !reflect.DeepEqual(row.Facts[0].Envelope, s.Facts[0]) {
								t.Fatal("active record lost")
							}
						}
					}
					if !found {
						t.Fatal("missing pack")
					}
				}
			})
		}
	}
}

func providerEdgesServiceSnapshot(t *testing.T) semreg.Snapshot {
	t.Helper()
	s := providerEdgesSnapshot(t)
	s.Facts = []semreg.FactEnvelope{}
	s.Services = []semreg.ServiceInstance{{InstanceID: "service:synthetic", AssetID: s.AssetID, Definition: semreg.DefinitionRef{Pack: evse.New().Pack(), ID: "evse.service.connector", Version: "1.0.0"}, BindingID: s.Bindings[0].BindingID, SourceEpochID: s.Bindings[0].SourceEpochID, DriverGeneration: s.Bindings[0].DriverGeneration, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "1"}}
	return s
}

func TestAvailabilityProviderEdgesConstraints(t *testing.T) {
	for _, change := range []string{"valid", "missing", "extra", "wrong_id"} {
		t.Run(change, func(t *testing.T) {
			s := providerEdgesServiceSnapshot(t)
			value := semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: "unit.ampere"}}
			constraint := semreg.TypedField{ID: "evse.limit.allocated_current", Value: value}
			s.Capabilities = []semreg.CapabilityInstance{{InstanceID: "capability:synthetic", AssetID: s.AssetID, ServiceInstance: s.Services[0].InstanceID, Definition: semreg.DefinitionRef{Pack: evse.New().Pack(), ID: "evse.capability.set_allocated_current", Version: "1.0.0"}, BindingID: s.Bindings[0].BindingID, SourceEpochID: s.Bindings[0].SourceEpochID, DriverGeneration: s.Bindings[0].DriverGeneration, Qualification: semreg.QualificationUnknown, Availability: semreg.AvailabilityWithdrawn, Constraints: []semreg.TypedField{constraint}, ActivationEvidence: []semreg.EvidenceRef{s.Bindings[0].NativeResource}, Revision: "1"}}
			switch change {
			case "missing":
				s.Capabilities[0].Constraints = []semreg.TypedField{}
			case "extra":
				s.Capabilities[0].Constraints = append(s.Capabilities[0].Constraints, semreg.TypedField{ID: "evse.limit.advertised_current", Value: value})
				sort.Slice(s.Capabilities[0].Constraints, func(i, j int) bool { return s.Capabilities[0].Constraints[i].ID < s.Capabilities[0].Constraints[j].ID })
			case "wrong_id":
				s.Capabilities[0].Constraints[0].ID = "evse.limit.advertised_current"
				// The replacement accepts the same value, isolating the required ID.
				if err := evse.New().ValidateField(semreg.DefinitionRef{Pack: evse.New().Pack(), ID: "evse.limit.advertised_current", Version: "1.0.0"}, s.Capabilities[0].Constraints[0]); err != nil {
					t.Fatal(err)
				}
			}
			out := providerEdgesCheck(t, providerEdgesRegistry(t), s, change == "valid")
			if change == "valid" {
				for _, row := range out.Packs {
					if row.Pack == evse.New().Pack() && (len(row.Capabilities) != 1 || !reflect.DeepEqual(row.Capabilities[0], s.Capabilities[0])) {
						t.Fatal("capability state changed")
					}
				}
			}
		})
	}
}

func providerEdgesValidators() []semreg.PackValidator {
	return []semreg.PackValidator{evse.New(), infrastructure.New(), pv.New(), storage.New(), thermal.New()}
}

func TestAvailabilityProviderEdgesServices(t *testing.T) {
	for _, mode := range []string{"valid", "unregistered", "validator_rejected"} {
		t.Run(mode, func(t *testing.T) {
			s := providerEdgesServiceSnapshot(t)
			s.Services = s.Services[:1]
			s.Capabilities = []semreg.CapabilityInstance{}
			s.Facts = []semreg.FactEnvelope{}
			sources := []semreg.PackMetadataSource{}
			for _, v := range providerEdgesValidators() {
				m := v.(semreg.PackMetadataProvider).Metadata()
				index := v.Definitions()
				if v.Pack() == s.Services[0].Definition.Pack && mode != "valid" {
					m.Services = []semreg.ServiceMetadata{}
					m.Capabilities = []semreg.CapabilityMetadata{}
					m.Operations = []semreg.OperationMetadata{}
					index.Services = []semreg.DefinitionRef{}
					index.Capabilities = []semreg.DefinitionRef{}
					index.Operations = []semreg.DefinitionRef{}
					if mode == "validator_rejected" {
						ref := s.Services[0].Definition
						ref.ID = "synthetic.service.other"
						s.Services[0].Definition = ref
						m.Services = []semreg.ServiceMetadata{{Ref: ref, FactKeyDimension: v.(semreg.PackMetadataProvider).Metadata().Services[0].FactKeyDimension}}
						index.Services = []semreg.DefinitionRef{ref}
					}
					v = providerEdgesMetadata{v, m, index}
				}
				sources = append(sources, semreg.PackMetadataSource{Validator: v, Metadata: m})
			}
			registry, err := semreg.NewPackMetadataRegistry(sources...)
			if err != nil {
				t.Fatal(err)
			}
			providerEdgesCheck(t, registry, s, mode == "valid")
		})
	}
}
