package projection_test

import (
	"bytes"
	"reflect"
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/projection"
)

func availabilityStructureCheck(t *testing.T, registry *semreg.PackMetadataRegistry, s semreg.Snapshot, valid bool) projection.PackAvailabilityView {
	t.Helper()
	sealProjectionSnapshot(t, &s)
	evaluation := m20Evaluate(t, s)
	beforeS, beforeE := revisionAxesJSON(t, s), revisionAxesJSON(t, evaluation)
	out, err := projection.DerivePackAvailability(registry, s, evaluation)
	if (err == nil) != valid {
		t.Fatalf("valid=%v, error=%v", valid, err)
	}
	if !bytes.Equal(beforeS, revisionAxesJSON(t, s)) || !bytes.Equal(beforeE, revisionAxesJSON(t, evaluation)) {
		t.Fatal("input changed")
	}
	return out
}

func TestAvailabilityStructureConstraints(t *testing.T) {
	for _, validator := range m20Validators() {
		metadata := validator.(semreg.PackMetadataProvider).Metadata()
		for _, operation := range metadata.Operations {
			// Every shipped operation has one required constraint. Exercise every ID
			// and its value independently, using the operation's exported argument.
			for _, state := range []string{"qualified", "unqualified", "unavailable", "both"} {
				for _, change := range []string{"valid", "missing", "extra", "duplicate", "wrong_id", "wrong_value"} {
					t.Run(string(operation.Ref.ID)+"/"+state+"/"+change, func(t *testing.T) {
						s := m20Fixture(t)
						s.Services, s.Capabilities = s.Services[:1], s.Capabilities[:1]
						s.Facts = []semreg.FactEnvelope{}
						service, capability := &s.Services[0], &s.Capabilities[0]
						service.Definition = operation.Service
						capability.Definition = operation.Capability
						var value semreg.Value
						for _, field := range metadata.Fields {
							if field.Ref == operation.Argument && field.CanonicalUnit != nil {
								value = semreg.Value{Kind: semreg.ValueQuantity, Quantity: &semreg.Quantity{Number: semreg.Decimal{Coefficient: "0"}, Unit: field.CanonicalUnit.ID}}
							}
						}
						if operation.Argument.ID == "thermal.mode.system" {
							value = semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: operation.Argument.ID, Token: "off", Known: true}}
						}
						capability.Constraints = []semreg.TypedField{{ID: operation.Argument.ID, Value: value}}
						if err := validator.ValidateCapability(*capability); err != nil {
							t.Fatalf("invalid control: %v", err)
						}
						if state == "unqualified" || state == "both" {
							capability.Qualification = semreg.QualificationUnknown
						}
						if state == "unavailable" || state == "both" {
							capability.Availability = semreg.AvailabilityWithdrawn
						}
						switch change {
						case "missing":
							capability.Constraints = []semreg.TypedField{}
						case "extra":
							extra := m20Clone(t, capability.Constraints[0])
							extra.ID = "synthetic.extra"
							capability.Constraints = append(capability.Constraints, extra)
							sort.Slice(capability.Constraints, func(i, j int) bool { return capability.Constraints[i].ID < capability.Constraints[j].ID })
						case "duplicate":
							sealProjectionSnapshot(t, &s)
							evaluation := m20Evaluate(t, s)
							capability.Constraints = append(capability.Constraints, m20Clone(t, capability.Constraints[0]))
							// Duplicate collections fail kernel validation before evaluation identity.

							before := revisionAxesJSON(t, s)
							if _, err := projection.DerivePackAvailability(m20Registry(t), s, evaluation); semreg.ErrorIdentifier(err) != semreg.DuplicateKey {
								t.Fatalf("duplicate constraint: %v", err)
							}
							if !bytes.Equal(before, revisionAxesJSON(t, s)) {
								t.Fatal("input changed")
							}
							return
						case "wrong_id":
							// Change only the ID. Prefer another field accepting the
							// unchanged value, so field checks cannot mask ID checks.
							var alternate semreg.DefinitionID
							for _, field := range metadata.Fields {
								if field.Ref.ID == operation.Argument.ID {
									continue
								}
								if alternate == "" {
									alternate = field.Ref.ID
								}
								probe := semreg.TypedField{ID: field.Ref.ID, Value: capability.Constraints[0].Value}
								if validator.ValidateField(field.Ref, probe) == nil {
									alternate = field.Ref.ID
									break
								}
							}
							if alternate == "" {
								t.Fatal("missing alternate field")
							}
							capability.Constraints[0].ID = alternate
						case "wrong_value":
							label := "synthetic"
							capability.Constraints[0].Value = semreg.Value{Kind: semreg.ValueText, Text: &label}
						}
						out := availabilityStructureCheck(t, m20Registry(t), s, change == "valid")
						if change == "valid" {
							found := false
							for _, row := range out.Packs {
								if row.Pack == metadata.Pack {
									found = true
									if len(row.Capabilities) != 1 || !reflect.DeepEqual(row.Capabilities[0], *capability) {
										t.Fatal("capability state or constraints changed")
									}
								}
							}
							if !found {
								t.Fatal("capability hidden")
							}
						}
					})
				}
			}
		}
		t.Run(string(metadata.Pack.ID)+"/read_extra", func(t *testing.T) {
			s := m20Fixture(t)
			for i := range s.Capabilities {
				if s.Capabilities[i].Definition.Pack == metadata.Pack {
					s.Capabilities[i].Qualification = semreg.QualificationUnknown
					field := s.Facts[i].Candidates[0]
					s.Capabilities[i].Constraints = []semreg.TypedField{{ID: field.Key.FactID, Value: *field.Value}}
				}
			}
			availabilityStructureCheck(t, m20Registry(t), s, false)
		})
	}
}

func availabilityStructureRetain(s *semreg.Snapshot, candidate semreg.FactCandidate) {
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

func TestAvailabilityStructureDimensions(t *testing.T) {
	for _, validator := range m20Validators() {
		metadata := validator.(semreg.PackMetadataProvider).Metadata()
		for _, field := range metadata.Fields {
			for _, retained := range []bool{false, true} {
				for _, change := range []string{"valid", "other_text", "missing", "extra", "wrong_id", "wrong_type"} {
					name := string(field.Ref.ID) + "/current/" + change
					if retained {
						name = string(field.Ref.ID) + "/retained/" + change
					}
					t.Run(name, func(t *testing.T) {
						s := m20Fixture(t)
						c := m20Clone(t, s.Facts[0].Candidates[0])
						s.Services = []semreg.ServiceInstance{}
						s.Capabilities = []semreg.CapabilityInstance{}
						label := "synthetic"
						c.Key = semreg.FactKey{PackID: metadata.Pack.ID, PackVersion: metadata.Pack.Version, FactID: field.Ref.ID, Dimensions: []semreg.Dimension{{ID: field.Dimension.ID, Value: semreg.Value{Kind: semreg.ValueText, Text: &label}}}}
						c.Value = nil
						c.Quality.Availability = semreg.AvailabilityWithdrawn
						c.Quality.Promotion = semreg.PromotionUnpromoted
						switch change {
						case "other_text":
							*c.Key.Dimensions[0].Value.Text = "other"
						case "missing":
							c.Key.Dimensions = []semreg.Dimension{}
						case "extra":
							extra := m20Clone(t, c.Key.Dimensions[0])
							extra.ID = "synthetic.extra"
							c.Key.Dimensions = append(c.Key.Dimensions, extra)
							sort.Slice(c.Key.Dimensions, func(i, j int) bool { return c.Key.Dimensions[i].ID < c.Key.Dimensions[j].ID })
						case "wrong_id":
							c.Key.Dimensions[0].ID = "synthetic.other"
						case "wrong_type":
							v := true
							c.Key.Dimensions[0].Value = semreg.Value{Kind: semreg.ValueBoolean, Boolean: &v}
						}
						if retained {
							availabilityStructureRetain(&s, c)
						} else {
							s.Facts = []semreg.FactEnvelope{{AssetID: s.AssetID, Key: m20Clone(t, c.Key), Candidates: []semreg.FactCandidate{c}, Conflicts: []semreg.Conflict{}, Revision: "1"}}
						}
						valid := change == "valid" || change == "other_text"
						out := availabilityStructureCheck(t, m20Registry(t), s, valid)
						if valid {
							for _, row := range out.Packs {
								if row.Pack != metadata.Pack {
									continue
								}
								if retained {
									if len(row.Retained) != 1 || !reflect.DeepEqual(row.Retained[0].Observation, s.Retained[0]) {
										t.Fatal("retained tombstone changed or hidden")
									}
								} else if len(row.Facts) != 1 || !reflect.DeepEqual(row.Facts[0].Envelope, s.Facts[0]) {
									t.Fatal("current tombstone changed or hidden")
								}
							}
						}
					})
				}
			}
		}
	}
}

// Synthetic metadata variants isolate the registry check from the shipped
// service validator, so deleting either check independently is observable.
type availabilityStructureMetadata struct {
	semreg.PackValidator
	metadata semreg.PackMetadata
	index    semreg.DefinitionIndex
}

func (v availabilityStructureMetadata) Metadata() semreg.PackMetadata       { return v.metadata }
func (v availabilityStructureMetadata) Definitions() semreg.DefinitionIndex { return v.index }

func TestAvailabilityStructureServices(t *testing.T) {
	for _, mode := range []string{"valid", "unregistered", "validator_rejected"} {
		t.Run(mode, func(t *testing.T) {
			s := m20Fixture(t)
			s.Services = s.Services[:1]
			s.Capabilities = []semreg.CapabilityInstance{}
			s.Facts = []semreg.FactEnvelope{}
			sources := []semreg.PackMetadataSource{}
			for _, v := range m20Validators() {
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
					v = availabilityStructureMetadata{v, m, index}
				}
				sources = append(sources, semreg.PackMetadataSource{Validator: v, Metadata: m})
			}
			registry, err := semreg.NewPackMetadataRegistry(sources...)
			if err != nil {
				t.Fatal(err)
			}
			availabilityStructureCheck(t, registry, s, mode == "valid")
		})
	}
}
