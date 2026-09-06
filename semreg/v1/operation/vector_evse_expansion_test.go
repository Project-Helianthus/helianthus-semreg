package operation_test

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

var vectorField = semreg.DefinitionRef{
	Pack: semreg.PackRef{ID: "helianthus.pack.evse", Version: "1.0.0"},
	ID:   "helianthus.evse.field.current_limit", Version: "1.0.0",
}

type evseIntentScenario struct {
	IntentID                           semreg.IntentID                 `json:"intent_id"`
	AssetID                            semreg.AssetID                  `json:"asset_id"`
	RequiredCapability                 operation.CapabilityRequirement `json:"required_capability"`
	ExpectedSemanticRevision           semreg.Uint64                   `json:"expected_semantic_revision"`
	ExpectedCapabilityRevision         semreg.Uint64                   `json:"expected_capability_revision"`
	ExpectedCapabilityInstanceRevision semreg.Uint64                   `json:"expected_capability_instance_revision"`
	ExpectedSourceEpochID              semreg.SourceEpochID            `json:"expected_source_epoch_id"`
	ExpectedDriverGeneration           semreg.Uint64                   `json:"expected_driver_generation"`
	Deadline                           semreg.TimePoint                `json:"deadline"`
	Kind                               semreg.DefinitionRef            `json:"kind"`
	ExpectedEffect                     operation.ExpectedEffect        `json:"expected_effect"`
	Preconditions                      []operation.Precondition        `json:"preconditions"`
}

type evseVectorPack struct {
	intent operation.Intent
	index  semreg.DefinitionIndex
}

type vectorFactPack struct{ pack semreg.PackRef }

func (p *vectorFactPack) Pack() semreg.PackRef { return p.pack }
func (p *vectorFactPack) Definitions() semreg.DefinitionIndex {
	return semreg.DefinitionIndex{Pack: p.pack, Fields: []semreg.DefinitionRef{}, Services: []semreg.DefinitionRef{}, Capabilities: []semreg.DefinitionRef{}, Operations: []semreg.DefinitionRef{}, EffectRules: []semreg.DefinitionRef{}}
}
func (p *vectorFactPack) ValidateFact(key semreg.FactKey, value *semreg.Value) error {
	if key.PackID != p.pack.ID || key.PackVersion != p.pack.Version || value == nil {
		return errors.New("unknown vector fact")
	}
	return value.Validate()
}
func (p *vectorFactPack) ValidateService(semreg.ServiceInstance) error       { return nil }
func (p *vectorFactPack) ValidateCapability(semreg.CapabilityInstance) error { return nil }
func (p *vectorFactPack) ValidateField(_ semreg.DefinitionRef, field semreg.TypedField) error {
	return field.Validate()
}
func (p *vectorFactPack) MatchConstraints(semreg.CapabilityInstance, []semreg.TypedField) error {
	return nil
}
func (p *vectorFactPack) EvaluatePredicate(semreg.FactCandidate, semreg.PredicateOp, semreg.Value) (bool, error) {
	return false, errors.New("no vector predicate")
}

func (p *evseVectorPack) Pack() semreg.PackRef                { return p.index.Pack }
func (p *evseVectorPack) Definitions() semreg.DefinitionIndex { return p.index }
func (p *evseVectorPack) ValidateFact(key semreg.FactKey, value *semreg.Value) error {
	if key.PackID != p.index.Pack.ID || key.PackVersion != p.index.Pack.Version || value == nil {
		return errors.New("unknown EVSE fact")
	}
	return value.Validate()
}
func (p *evseVectorPack) ValidateService(semreg.ServiceInstance) error       { return nil }
func (p *evseVectorPack) ValidateCapability(semreg.CapabilityInstance) error { return nil }
func (p *evseVectorPack) ValidateField(ref semreg.DefinitionRef, field semreg.TypedField) error {
	if ref != vectorField || field.ID != vectorField.ID || !reflect.DeepEqual(field.Value, p.intent.ExpectedEffect.Expected) {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "EVSE argument"}
	}
	return nil
}
func (p *evseVectorPack) MatchConstraints(_ semreg.CapabilityInstance, fields []semreg.TypedField) error {
	if len(fields) != 1 || fields[0].ID != vectorField.ID || !reflect.DeepEqual(fields[0].Value, p.intent.ExpectedEffect.Expected) {
		return errors.New("EVSE constraint")
	}
	return nil
}
func (p *evseVectorPack) EvaluatePredicate(candidate semreg.FactCandidate, operator semreg.PredicateOp, expected semreg.Value) (bool, error) {
	if operator != semreg.PredicateEqual || candidate.Value == nil {
		return false, errors.New("EVSE predicate")
	}
	return reflect.DeepEqual(*candidate.Value, expected), nil
}
func (p *evseVectorPack) ValidateIntent(intent operation.Intent) error {
	if !reflect.DeepEqual(intent.Kind, p.intent.Kind) || !reflect.DeepEqual(intent.ExpectedEffect, p.intent.ExpectedEffect) ||
		len(intent.Arguments) != 1 || !reflect.DeepEqual(intent.Arguments[0].Value, p.intent.ExpectedEffect.Expected) {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "EVSE effect"}
	}
	return nil
}
func (p *evseVectorPack) EvaluateReadback(intent operation.Intent, candidate semreg.FactCandidate) (operation.ReadbackRelation, error) {
	if candidate.Value == nil || !reflect.DeepEqual(candidate.Key, intent.ExpectedEffect.Fact) {
		return "", errors.New("EVSE readback fact")
	}
	if reflect.DeepEqual(*candidate.Value, intent.ExpectedEffect.Expected) {
		return operation.ReadbackConfirms, nil
	}
	return operation.ReadbackContradicts, nil
}

type evseFixture struct {
	kernel     *operation.Kernel
	pack       *evseVectorPack
	electrical *vectorFactPack
	intent     operation.Intent
	snapshot   semreg.Snapshot
	current    semreg.EvaluationContext
}

func TestAcceptedEVSEVectorsUseExactTypedValues(t *testing.T) {
	vectors := loadOperationVectors(t)
	positive := vectorByID(t, vectors, "K-POS-012")
	fixture := newEVSEFixture(t, positive)
	admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.intent.IntentID != "intent:evse-limit:01" || fixture.intent.ExpectedSemanticRevision != "12" ||
		fixture.intent.ExpectedCapabilityRevision != "5" || fixture.intent.ExpectedCapabilityInstanceRevision != "3" ||
		fixture.intent.ExpectedDriverGeneration != "7" || fixture.intent.ExpectedEffect.Expected.Quantity == nil ||
		fixture.intent.ExpectedEffect.Expected.Quantity.Number.Coefficient != "16" || fixture.intent.ExpectedEffect.Expected.Quantity.Unit != "unit.ampere" {
		t.Fatalf("K-POS-012 exact values lost: %+v", fixture.intent)
	}
	route, ok := admission.Route()
	if !ok || route.CapabilityInstance != "capability:limit:01" || route.BindingID != "binding:evse:01" || route.SourceEpochID != "source-epoch:evse:01" || route.DriverGeneration != "7" {
		t.Fatalf("K-POS-012 route: %+v", route)
	}

	for _, id := range []string{"K-POS-014", "K-NEG-036", "K-NEG-041", "K-NEG-045", "K-NEG-046"} {
		t.Run(id, func(t *testing.T) {
			vector := vectorByID(t, vectors, id)
			fixture := newEVSEFixture(t, positive)
			admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			if err != nil {
				t.Fatal(err)
			}
			record, later := expandEVSEExecution(t, vector, admission, fixture.snapshot)
			admittedCandidate, admittedFound := vectorCandidateByID(fixture.snapshot, record.Readback.CandidateID)
			laterCandidate, laterFound := vectorCandidateByID(later, record.Readback.CandidateID)
			switch id {
			case "K-POS-014":
				if !admittedFound || !laterFound || admittedCandidate.Revision != "1" || laterCandidate.Revision != "2" {
					t.Fatalf("exact readback revision transition: admitted=%+v later=%+v", admittedCandidate, laterCandidate)
				}
			case "K-NEG-036":
				if !laterFound || laterCandidate.DriverGeneration == nil || *laterCandidate.DriverGeneration != "8" {
					t.Fatalf("exact replacement generation: %+v", laterCandidate)
				}
			case "K-NEG-045":
				admittedBytes, _ := semreg.CanonicalJSON(admittedCandidate)
				laterBytes, _ := semreg.CanonicalJSON(laterCandidate)
				if !admittedFound || !laterFound || !bytesEqual(admittedBytes, laterBytes) || len(later.Services) != len(fixture.snapshot.Services)+1 {
					t.Fatal("exact retained candidate/unrelated-service transition")
				}
			}
			before, _ := semreg.CanonicalJSON(later)
			stored, got := fixture.kernel.Record(admission, record, &later)
			if vector.Expect.Result == "accept" {
				if got != nil || stored.Outcome != operation.OutcomeApplied {
					t.Fatalf("exact EVSE execution: %+v %v", stored, got)
				}
				canonical, err := semreg.CanonicalJSON(stored)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := operation.DecodeExecutionRecord(canonical)
				if err != nil || !reflect.DeepEqual(decoded, stored) {
					t.Fatalf("canonical EVSE result: %v", err)
				}
			} else {
				errorID(t, got, vector.Expect.ErrorID)
				if _, recorded := admission.Recorded(); recorded {
					t.Fatal("rejected exact EVSE vector stored a record")
				}
			}
			after, _ := semreg.CanonicalJSON(later)
			if !bytesEqual(before, after) {
				t.Fatal("exact EVSE expansion mutated readback snapshot")
			}
		})
	}
}

func TestAcceptedRouteVectorsUseExactTypedValues(t *testing.T) {
	vectors := loadOperationVectors(t)
	positive := vectorByID(t, vectors, "K-POS-012")
	for _, id := range []string{"K-NEG-013", "K-NEG-016"} {
		t.Run(id, func(t *testing.T) {
			fixture := newEVSEFixture(t, positive)
			vector := vectorByID(t, vectors, id)
			switch id {
			case "K-NEG-013":
				var input struct {
					SourceEpochID    semreg.SourceEpochID `json:"source_epoch_id"`
					DriverGeneration semreg.Uint64        `json:"driver_generation"`
				}
				var prior struct {
					Fenced  semreg.Uint64 `json:"fenced_driver_generation"`
					Current semreg.Uint64 `json:"current_driver_generation"`
				}
				decodeVectorInput(t, vector.Input, &input)
				decodeVectorInput(t, vector.PriorState, &prior)
				if input.SourceEpochID != fixture.intent.ExpectedSourceEpochID || input.DriverGeneration != prior.Fenced || prior.Current != "8" {
					t.Fatalf("exact generation vector values: input=%+v prior=%+v", input, prior)
				}
				fixture.snapshot.Bindings[0].State = semreg.BindingFenced
				fixture.snapshot.IdentityLinks[0].State = semreg.LinkWithdrawn
				fixture.snapshot.Services[0].Availability = semreg.AvailabilityWithdrawn
				fixture.snapshot.Capabilities[0].Availability = semreg.AvailabilityWithdrawn
				fixture.snapshot.Facts = []semreg.FactEnvelope{}
				fixture.snapshot.Cursors[0].Fenced = true
				fixture.snapshot.Fences = []semreg.GenerationFence{{
					SourceID: fixture.snapshot.Bindings[0].SourceID, SourceEpochID: input.SourceEpochID,
					DriverGeneration: input.DriverGeneration, Reason: "reason.generation-fenced",
					Evidence: []semreg.EvidenceRef{evidence(6)}, Revision: "1",
				}}
			case "K-NEG-016":
				var input struct {
					RequiredCapability semreg.DefinitionID           `json:"required_capability"`
					EligibleRoutes     []semreg.CapabilityInstanceID `json:"eligible_routes"`
				}
				decodeVectorInput(t, vector.Input, &input)
				if input.RequiredCapability != fixture.intent.RequiredCapability.DefinitionID || len(input.EligibleRoutes) != 2 {
					t.Fatalf("exact ambiguity vector values: %+v", input)
				}
				first := fixture.snapshot.Capabilities[0]
				first.InstanceID = input.EligibleRoutes[0]
				second := first
				second.InstanceID = input.EligibleRoutes[1]
				fixture.snapshot.Capabilities = []semreg.CapabilityInstance{first, second}
			}
			sealCorrectionSnapshot(t, &fixture.snapshot)
			before, _ := semreg.CanonicalJSON(fixture.snapshot)
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			errorID(t, err, vector.Expect.ErrorID)
			after, _ := semreg.CanonicalJSON(fixture.snapshot)
			if !bytesEqual(before, after) {
				t.Fatal("exact route vector mutated snapshot")
			}
		})
	}
}

func TestAcceptedPreconditionVectorsUseExactTypedValues(t *testing.T) {
	vectors := loadOperationVectors(t)
	positive := vectorByID(t, vectors, "K-POS-012")
	for _, id := range []string{"K-NEG-018", "K-NEG-038", "K-NEG-039", "K-NEG-040", "K-NEG-049", "K-NEG-050"} {
		t.Run(id, func(t *testing.T) {
			fixture := newEVSEFixture(t, positive)
			vector := vectorByID(t, vectors, id)
			switch id {
			case "K-NEG-018":
				var input struct {
					Fact                   semreg.FactKey       `json:"fact"`
					CandidateID            semreg.CandidateID   `json:"candidate_id"`
					CandidateRevision      semreg.Uint64        `json:"candidate_revision"`
					Operator               semreg.PredicateOp   `json:"operator"`
					Expected               semreg.Value         `json:"expected"`
					OpenConflictCandidates []semreg.CandidateID `json:"open_conflict_candidates"`
				}
				decodeVectorInput(t, vector.Input, &input)
				fixture.intent.Preconditions = []operation.Precondition{{Fact: input.Fact, CandidateID: input.CandidateID, CandidateRevision: input.CandidateRevision, Operator: input.Operator, Expected: input.Expected}}
				first := exactEVSECandidate(fixture.snapshot, input.OpenConflictCandidates[0], input.Fact, input.Expected, input.CandidateRevision)
				otherValue := boolValue(false)
				second := exactEVSECandidate(fixture.snapshot, input.OpenConflictCandidates[1], input.Fact, otherValue, input.CandidateRevision)
				candidates := []semreg.FactCandidate{first, second}
				sort.Slice(candidates, func(i, j int) bool { return candidates[i].CandidateID < candidates[j].CandidateID })
				conflict := exactConflict(t, fixture.snapshot.AssetID, input.Fact, input.OpenConflictCandidates)
				fixture.snapshot.Facts = []semreg.FactEnvelope{{AssetID: fixture.snapshot.AssetID, Key: input.Fact, Candidates: candidates, Conflicts: []semreg.Conflict{conflict}, Revision: "8"}}
			case "K-NEG-038":
				var input struct {
					CandidateID semreg.CandidateID `json:"precondition_candidate_id"`
					Revision    semreg.Uint64      `json:"candidate_revision"`
					Current     semreg.Int64       `json:"evaluation_context_wall_ns"`
				}
				var prior struct {
					Receipt  semreg.Int64  `json:"receipt_wall_ns"`
					FreshFor semreg.Uint64 `json:"fresh_for_ns"`
					Semantic semreg.Uint64 `json:"semantic_revision"`
				}
				decodeVectorInput(t, vector.Input, &input)
				decodeVectorInput(t, vector.PriorState, &prior)
				candidate := exactEVSECandidate(fixture.snapshot, input.CandidateID, fixture.intent.Preconditions[0].Fact, boolValue(true), input.Revision)
				candidate.Times.ReceivedAt = vectorTime(prior.Receipt, "0")
				candidate.Times.ReceiptMonotonic.Nanoseconds = "100000000000"
				candidate.Times.EvaluatedAt = candidate.Times.ReceivedAt
				candidate.Times.EvaluateMonotonic = candidate.Times.ReceiptMonotonic
				candidate.FreshnessPolicy.FreshForNS = prior.FreshFor
				candidate.FreshnessPolicy.RetainForNS = "120000000000"
				fixture.snapshot.Revisions.Semantic = prior.Semantic
				fixture.snapshot.Facts = []semreg.FactEnvelope{{AssetID: fixture.snapshot.AssetID, Key: candidate.Key, Candidates: []semreg.FactCandidate{candidate}, Conflicts: []semreg.Conflict{}, Revision: "8"}}
				fixture.intent.ExpectedSemanticRevision = prior.Semantic
				fixture.intent.Preconditions[0].CandidateID, fixture.intent.Preconditions[0].CandidateRevision = input.CandidateID, input.Revision
				fixture.current = semreg.EvaluationContext{EvaluatedAt: vectorTime(input.Current, "0"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:boot-1", Nanoseconds: "140000000000"}}
				fixture.intent.Deadline, fixture.intent.Causal.ExpiresAt = vectorTime("200000000000", "0"), vectorTime("199000000000", "0")
			case "K-NEG-039", "K-NEG-040":
				var input struct {
					Precondition operation.Precondition `json:"precondition"`
					Candidates   []struct {
						CandidateID   semreg.CandidateID   `json:"candidate_id"`
						Revision      semreg.Uint64        `json:"revision"`
						Value         semreg.Value         `json:"value"`
						Qualification semreg.Qualification `json:"qualification"`
						Promotion     semreg.Promotion     `json:"promotion"`
						Validity      semreg.Validity      `json:"validity"`
					} `json:"envelope_candidates"`
				}
				decodeVectorInput(t, vector.Input, &input)
				fixture.intent.Preconditions = []operation.Precondition{input.Precondition}
				candidates := make([]semreg.FactCandidate, 0, len(input.Candidates))
				for _, item := range input.Candidates {
					candidate := exactEVSECandidate(fixture.snapshot, item.CandidateID, input.Precondition.Fact, item.Value, item.Revision)
					candidate.Quality.Qualification, candidate.Quality.Promotion, candidate.Quality.Validity = item.Qualification, item.Promotion, item.Validity
					candidates = append(candidates, candidate)
				}
				sort.Slice(candidates, func(i, j int) bool { return candidates[i].CandidateID < candidates[j].CandidateID })
				fixture.snapshot.Facts = []semreg.FactEnvelope{{AssetID: fixture.snapshot.AssetID, Key: input.Precondition.Fact, Candidates: candidates, Conflicts: []semreg.Conflict{}, Revision: "8"}}
			case "K-NEG-049", "K-NEG-050":
				var input struct {
					Fact                      semreg.FactKey       `json:"fact"`
					CandidateID               semreg.CandidateID   `json:"candidate_id"`
					CandidateRevision         semreg.Uint64        `json:"candidate_revision"`
					Expected                  semreg.Value         `json:"expected"`
					Operator                  semreg.PredicateOp   `json:"operator"`
					SnapshotCandidates        []semreg.CandidateID `json:"snapshot_candidates"`
					ResolvedCandidateRevision semreg.Uint64        `json:"resolved_candidate_revision"`
				}
				decodeVectorInput(t, vector.Input, &input)
				fixture.intent.Preconditions = []operation.Precondition{{Fact: input.Fact, CandidateID: input.CandidateID, CandidateRevision: input.CandidateRevision, Operator: input.Operator, Expected: input.Expected}}
				storedID, storedRevision := input.CandidateID, input.ResolvedCandidateRevision
				if len(input.SnapshotCandidates) != 0 {
					storedID, storedRevision = input.SnapshotCandidates[0], input.CandidateRevision
				}
				candidate := exactEVSECandidate(fixture.snapshot, storedID, input.Fact, input.Expected, storedRevision)
				fixture.snapshot.Facts = []semreg.FactEnvelope{{AssetID: fixture.snapshot.AssetID, Key: input.Fact, Candidates: []semreg.FactCandidate{candidate}, Conflicts: []semreg.Conflict{}, Revision: "8"}}
			}
			sealCorrectionSnapshot(t, &fixture.snapshot)
			before, _ := semreg.CanonicalJSON(fixture.snapshot)
			_, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, operation.AuthorityResolverFunc(authorize))
			errorID(t, err, vector.Expect.ErrorID)
			after, _ := semreg.CanonicalJSON(fixture.snapshot)
			if !bytesEqual(before, after) {
				t.Fatal("exact precondition vector mutated snapshot")
			}
		})
	}
}

func exactEVSECandidate(snapshot semreg.Snapshot, id semreg.CandidateID, key semreg.FactKey, value semreg.Value, revision semreg.Uint64) semreg.FactCandidate {
	binding := snapshot.Bindings[0]
	return vectorCandidate(id, key, value, revision, binding.BindingID, binding.SourceID, binding.SourceEpochID, binding.DriverGeneration,
		vectorTime("149000000000", "0"), semreg.MonotonicPoint{ClockEpochID: "clock-epoch:boot-1", Nanoseconds: "800"})
}

func exactConflict(t *testing.T, asset semreg.AssetID, key semreg.FactKey, candidates []semreg.CandidateID) semreg.Conflict {
	t.Helper()
	ids := append([]semreg.CandidateID(nil), candidates...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	identity := conflictIdentity{Contract: "helianthus.semantic.conflict-id/v1", AssetID: asset, Key: key, Kind: semreg.ConflictValue, Candidates: ids}
	digest, err := semreg.DigestRecord(identity)
	if err != nil {
		t.Fatal(err)
	}
	return semreg.Conflict{ConflictID: semreg.ConflictID(digest), Kind: semreg.ConflictValue, Candidates: ids, Evidence: []semreg.EvidenceRef{evidence(5)}, State: semreg.ConflictOpen}
}

type conflictIdentity struct {
	Contract   semreg.ContractVersion `json:"contract"`
	AssetID    semreg.AssetID         `json:"asset_id"`
	Key        semreg.FactKey         `json:"key"`
	Kind       semreg.ConflictKind    `json:"kind"`
	Candidates []semreg.CandidateID   `json:"candidates"`
}

func (conflictIdentity) Validate() error { return nil }

func newEVSEFixture(t *testing.T, vector operationVector) evseFixture {
	t.Helper()
	var input evseIntentScenario
	decodeVectorInput(t, vector.Input, &input)
	argument := semreg.TypedField{ID: vectorField.ID, Value: input.ExpectedEffect.Expected}
	intent := operation.Intent{
		Contract: operation.ContractOperationV1, IntentID: input.IntentID, Kind: input.Kind, ExpectedEffect: input.ExpectedEffect,
		AssetID: input.AssetID, Arguments: []semreg.TypedField{argument}, RequiredCapability: input.RequiredCapability,
		Authority: evidence(1), Causal: semreg.CausalContext{
			Origin:        semreg.OriginRef{OriginID: "origin:operator:01", Kind: semreg.OriginOperator, Evidence: []semreg.EvidenceRef{evidence(1)}},
			CorrelationID: "correlation:evse:01", HopCount: 0, MaxHops: 8,
			FirstSeenAt: vectorTime("140000000000", "1000"), ExpiresAt: vectorTime("199000000000", "1000"), Path: []semreg.TargetID{},
		},
		ExpectedSemanticRevision: input.ExpectedSemanticRevision, ExpectedCapabilityRevision: input.ExpectedCapabilityRevision,
		ExpectedCapabilityInstanceRevision: input.ExpectedCapabilityInstanceRevision, ExpectedSourceEpochID: input.ExpectedSourceEpochID,
		ExpectedDriverGeneration: input.ExpectedDriverGeneration, Preconditions: input.Preconditions,
		IdempotencyKey: "idempotency:evse-limit:01", Deadline: input.Deadline,
	}
	serviceRef := semreg.DefinitionRef{Pack: input.Kind.Pack, ID: "helianthus.evse.service.control", Version: "1.0.0"}
	capRef := semreg.DefinitionRef{Pack: input.RequiredCapability.Pack, ID: input.RequiredCapability.DefinitionID, Version: "1.0.0"}
	pack := &evseVectorPack{intent: intent, index: semreg.DefinitionIndex{
		Pack: input.Kind.Pack, Fields: []semreg.DefinitionRef{vectorField}, Services: []semreg.DefinitionRef{serviceRef},
		Capabilities: []semreg.DefinitionRef{capRef}, Operations: []semreg.DefinitionRef{input.Kind}, EffectRules: []semreg.DefinitionRef{input.ExpectedEffect.Rule},
	}}
	electrical := &vectorFactPack{pack: semreg.PackRef{ID: "helianthus.pack.electrical", Version: "1.0.0"}}
	kernel, err := operation.NewKernel(pack, electrical)
	if err != nil {
		t.Fatal(err)
	}
	bindingID := semreg.NativeBindingID("binding:evse:01")
	sourceID := semreg.SourceID("source:evse:01")
	candidate := vectorCandidate(input.Preconditions[0].CandidateID, input.Preconditions[0].Fact, input.Preconditions[0].Expected,
		input.Preconditions[0].CandidateRevision, bindingID, sourceID, input.ExpectedSourceEpochID, input.ExpectedDriverGeneration,
		vectorTime("149000000000", "1000"), semreg.MonotonicPoint{ClockEpochID: "clock-epoch:boot-1", Nanoseconds: "800"})
	readbackScenario := acceptedResolvedCandidate(t, vectorByID(t, loadOperationVectors(t), "K-NEG-045"))
	admittedReadback := vectorCandidate(readbackScenario.CandidateID, readbackScenario.Fact, readbackScenario.Value,
		readbackScenario.CandidateRevision, bindingID, sourceID, input.ExpectedSourceEpochID, input.ExpectedDriverGeneration,
		readbackScenario.Times.ReceivedAt, readbackScenario.Times.ReceiptMonotonic)
	admittedReadback.Quality.Assertion, admittedReadback.Quality.Qualification, admittedReadback.Quality.Promotion = readbackScenario.Assertion, readbackScenario.Qualification, readbackScenario.Promotion
	admittedReadback.Quality.Validity, admittedReadback.Quality.Availability, admittedReadback.Quality.Freshness = readbackScenario.Validity, semreg.AvailabilityAvailable, semreg.FreshnessFresh
	snapshot := semreg.Snapshot{
		Contract: semreg.ContractKernelV1, AssetID: input.AssetID,
		Revisions:   semreg.RevisionVector{Semantic: input.ExpectedSemanticRevision, Identity: "2", Facts: "8", Services: "3", Capabilities: input.ExpectedCapabilityRevision},
		EvaluatedAt: vectorTime("149000000000", "1000"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:boot-1", Nanoseconds: "800"},
		Sources:       []semreg.SourceDescriptor{{SourceID: sourceID, SourceEpochID: input.ExpectedSourceEpochID, ProtocolID: "protocol.evse", ProfileID: "profile.evse", ProfileVersion: "1", RegistryEvidence: evidence(1), StartedAt: vectorTime("100000000000", "1000"), State: semreg.SourceCurrent, Revision: "1"}},
		Bindings:      []semreg.NativeBinding{{BindingID: bindingID, AssetID: input.AssetID, SourceID: sourceID, SourceEpochID: input.ExpectedSourceEpochID, DriverGeneration: input.ExpectedDriverGeneration, NativeResource: evidence(2), State: semreg.BindingCurrent, Revision: "1"}},
		IdentityLinks: []semreg.IdentityLink{{AssetID: input.AssetID, BindingID: bindingID, State: semreg.LinkQualified, Basis: []semreg.EvidenceRef{evidence(3)}, Revision: "1"}},
		Facts: []semreg.FactEnvelope{
			{AssetID: input.AssetID, Key: candidate.Key, Candidates: []semreg.FactCandidate{candidate}, Conflicts: []semreg.Conflict{}, Revision: "8"},
			{AssetID: input.AssetID, Key: admittedReadback.Key, Candidates: []semreg.FactCandidate{admittedReadback}, Conflicts: []semreg.Conflict{}, Revision: "8"},
		},
		Services:     []semreg.ServiceInstance{{InstanceID: "service:evse:control:01", AssetID: input.AssetID, Definition: serviceRef, BindingID: bindingID, SourceEpochID: input.ExpectedSourceEpochID, DriverGeneration: input.ExpectedDriverGeneration, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Revision: "3"}},
		Capabilities: []semreg.CapabilityInstance{{InstanceID: "capability:limit:01", AssetID: input.AssetID, ServiceInstance: "service:evse:control:01", Definition: capRef, BindingID: bindingID, SourceEpochID: input.ExpectedSourceEpochID, DriverGeneration: input.ExpectedDriverGeneration, Qualification: semreg.QualificationQualified, Availability: semreg.AvailabilityAvailable, Constraints: []semreg.TypedField{}, ActivationEvidence: []semreg.EvidenceRef{evidence(4)}, Revision: input.ExpectedCapabilityInstanceRevision}},
		Fences:       []semreg.GenerationFence{}, Cursors: []semreg.PublicationCursor{{SourceID: sourceID, SourceEpochID: input.ExpectedSourceEpochID, DriverGeneration: input.ExpectedDriverGeneration, LastSequence: "1", LastBatchDigest: semreg.Digest("sha256:" + repeatHex(6)), Fenced: false}},
	}
	sort.Slice(snapshot.Facts, func(i, j int) bool {
		left, _ := semreg.CanonicalJSON(snapshot.Facts[i].Key)
		right, _ := semreg.CanonicalJSON(snapshot.Facts[j].Key)
		return string(left) < string(right)
	})
	sealCorrectionSnapshot(t, &snapshot)
	return evseFixture{kernel: kernel, pack: pack, electrical: electrical, intent: intent, snapshot: snapshot, current: semreg.EvaluationContext{EvaluatedAt: vectorTime("150000000000", "1000"), EvaluateMonotonic: semreg.MonotonicPoint{ClockEpochID: "clock-epoch:boot-1", Nanoseconds: "850"}}}
}

type dispatchScenario struct {
	Started            semreg.EvaluationContext  `json:"started"`
	Completed          *semreg.EvaluationContext `json:"completed"`
	Delivery           operation.DeliveryState   `json:"delivery"`
	PossibleSideEffect bool                      `json:"possible_side_effect"`
	EvidenceCount      int                       `json:"evidence_count"`
}

type readbackScenario struct {
	SnapshotID        semreg.SnapshotID          `json:"snapshot_id"`
	Revisions         semreg.RevisionVector      `json:"revisions"`
	CandidateID       semreg.CandidateID         `json:"candidate_id"`
	CandidateRevision semreg.Uint64              `json:"candidate_revision"`
	BindingID         semreg.NativeBindingID     `json:"binding_id"`
	SourceID          semreg.SourceID            `json:"source_id"`
	SourceEpochID     semreg.SourceEpochID       `json:"source_epoch_id"`
	DriverGeneration  semreg.Uint64              `json:"driver_generation"`
	Relation          operation.ReadbackRelation `json:"relation"`
	Evaluation        semreg.EvaluationContext   `json:"evaluation"`
	EvidenceCount     int                        `json:"evidence_count"`
}

type resolvedCandidateScenario struct {
	CandidateID       semreg.CandidateID   `json:"candidate_id"`
	CandidateRevision semreg.Uint64        `json:"candidate_revision"`
	Fact              semreg.FactKey       `json:"fact"`
	Value             semreg.Value         `json:"value"`
	Assertion         semreg.AssertionKind `json:"assertion"`
	Qualification     semreg.Qualification `json:"qualification"`
	Promotion         semreg.Promotion     `json:"promotion"`
	Validity          semreg.Validity      `json:"validity"`
	Availability      semreg.Availability  `json:"availability"`
	StoredFreshness   semreg.Freshness     `json:"stored_freshness"`
	Times             struct {
		ReceivedAt       semreg.TimePoint      `json:"received_at"`
		ReceiptMonotonic semreg.MonotonicPoint `json:"receipt_monotonic"`
	} `json:"times"`
	FreshnessPolicy *semreg.FreshnessPolicy `json:"freshness_policy"`
}

func expandEVSEExecution(t *testing.T, vector operationVector, admission *operation.Admission, base semreg.Snapshot) (operation.ExecutionRecord, semreg.Snapshot) {
	t.Helper()
	var input struct {
		Dispatch                  dispatchScenario          `json:"dispatch"`
		Readback                  readbackScenario          `json:"readback"`
		ResolvedCandidate         resolvedCandidateScenario `json:"resolved_candidate"`
		Outcome                   operation.Outcome         `json:"outcome"`
		Acknowledgement           string                    `json:"acknowledgement"`
		ExpectedEffect            operation.ExpectedEffect  `json:"expected_effect"`
		AdmittedCandidateRevision semreg.Uint64             `json:"admitted_candidate_revision"`
	}
	decodeVectorInput(t, vector.Input, &input)
	if vector.ID == "K-NEG-036" {
		decodeVectorInput(t, vector.Input, &input.Readback)
	}
	if input.Dispatch.Delivery == "" { // K-NEG-036 supplies only readback; expand the accepted dispatch scaffold.
		positive := vectorByID(t, loadOperationVectors(t), "K-POS-014")
		var accepted struct {
			Dispatch dispatchScenario `json:"dispatch"`
		}
		decodeVectorInput(t, positive.Input, &accepted)
		input.Dispatch = accepted.Dispatch
		input.Outcome = operation.OutcomeApplied
		input.ExpectedEffect = admission.Intent().ExpectedEffect
		input.ResolvedCandidate = acceptedResolvedCandidate(t, positive)
	}
	if input.ResolvedCandidate.CandidateID == "" {
		positive := vectorByID(t, loadOperationVectors(t), "K-POS-014")
		input.ResolvedCandidate = acceptedResolvedCandidate(t, positive)
	}
	if input.ResolvedCandidate.Availability == "" {
		input.ResolvedCandidate.Availability = semreg.AvailabilityAvailable
	}
	if input.ResolvedCandidate.StoredFreshness == "" {
		input.ResolvedCandidate.StoredFreshness = semreg.FreshnessFresh
	}
	if input.ResolvedCandidate.FreshnessPolicy == nil {
		input.ResolvedCandidate.FreshnessPolicy = &semreg.FreshnessPolicy{PolicyID: "policy.evse", Version: "1.0.0", FreshForNS: "30000000000", RetainForNS: "120000000000", MaxWallUncertaintyNS: "1000000"}
	}
	if input.ResolvedCandidate.FreshnessPolicy.PolicyID == "" {
		input.ResolvedCandidate.FreshnessPolicy.PolicyID = "policy.evse"
	}
	if input.ResolvedCandidate.FreshnessPolicy.Version == "" {
		input.ResolvedCandidate.FreshnessPolicy.Version = "1.0.0"
	}
	candidate := vectorCandidate(input.ResolvedCandidate.CandidateID, input.ResolvedCandidate.Fact, input.ResolvedCandidate.Value,
		input.ResolvedCandidate.CandidateRevision, input.Readback.BindingID, input.Readback.SourceID, input.Readback.SourceEpochID,
		input.Readback.DriverGeneration, input.ResolvedCandidate.Times.ReceivedAt, input.ResolvedCandidate.Times.ReceiptMonotonic)
	candidate.Quality.Assertion, candidate.Quality.Qualification, candidate.Quality.Promotion = input.ResolvedCandidate.Assertion, input.ResolvedCandidate.Qualification, input.ResolvedCandidate.Promotion
	candidate.Quality.Validity, candidate.Quality.Availability, candidate.Quality.Freshness = input.ResolvedCandidate.Validity, input.ResolvedCandidate.Availability, input.ResolvedCandidate.StoredFreshness
	candidate.FreshnessPolicy = *input.ResolvedCandidate.FreshnessPolicy
	baseJSON, err := semreg.CanonicalJSON(base)
	if err != nil {
		t.Fatal(err)
	}
	later, err := semreg.Decode[semreg.Snapshot](baseJSON)
	if err != nil {
		t.Fatal(err)
	}
	later.Revisions = input.Readback.Revisions
	later.EvaluatedAt, later.EvaluateMonotonic = input.ResolvedCandidate.Times.ReceivedAt, input.ResolvedCandidate.Times.ReceiptMonotonic
	if vector.ID == "K-NEG-045" {
		if input.AdmittedCandidateRevision != candidate.Revision {
			t.Fatal("retained readback revision differs from admitted revision")
		}
		later.Services = append(later.Services, semreg.ServiceInstance{
			InstanceID: "service:evse:unrelated:01", AssetID: later.AssetID, Definition: later.Services[0].Definition,
			BindingID: later.Bindings[0].BindingID, SourceEpochID: later.Bindings[0].SourceEpochID,
			DriverGeneration: later.Bindings[0].DriverGeneration, Qualification: semreg.QualificationQualified,
			Availability: semreg.AvailabilityAvailable, Revision: "1",
		})
		sort.Slice(later.Services, func(i, j int) bool { return later.Services[i].InstanceID < later.Services[j].InstanceID })
	} else {
		replaced := false
		for envelopeIndex := range later.Facts {
			for candidateIndex := range later.Facts[envelopeIndex].Candidates {
				if later.Facts[envelopeIndex].Candidates[candidateIndex].CandidateID == candidate.CandidateID {
					later.Facts[envelopeIndex].Candidates[candidateIndex] = candidate
					later.Facts[envelopeIndex].Revision = input.Readback.Revisions.Facts
					replaced = true
				}
			}
		}
		if !replaced {
			later.Facts = append(later.Facts, semreg.FactEnvelope{AssetID: later.AssetID, Key: candidate.Key, Candidates: []semreg.FactCandidate{candidate}, Conflicts: []semreg.Conflict{}, Revision: input.Readback.Revisions.Facts})
		}
	}
	if vector.ID == "K-NEG-036" {
		for index := range later.Bindings {
			later.Bindings[index].DriverGeneration = input.Readback.DriverGeneration
		}
		for index := range later.Services {
			later.Services[index].DriverGeneration = input.Readback.DriverGeneration
		}
		for index := range later.Capabilities {
			later.Capabilities[index].DriverGeneration = input.Readback.DriverGeneration
		}
		for index := range later.Cursors {
			later.Cursors[index].DriverGeneration = input.Readback.DriverGeneration
		}
		facts := later.Facts[:0]
		for _, envelope := range later.Facts {
			if factKeyEqualForVector(envelope.Key, candidate.Key) {
				facts = append(facts, envelope)
			}
		}
		later.Facts = facts
	}
	sort.Slice(later.Facts, func(i, j int) bool {
		left, _ := semreg.CanonicalJSON(later.Facts[i].Key)
		right, _ := semreg.CanonicalJSON(later.Facts[j].Key)
		return string(left) < string(right)
	})
	sealCorrectionSnapshot(t, &later)
	intent := admission.Intent()
	if input.ExpectedEffect.Rule.ID != "" {
		if !reflect.DeepEqual(input.ExpectedEffect, intent.ExpectedEffect) {
			t.Fatal("vector expected effect differs from admitted exact effect")
		}
	}
	route, _ := admission.Route()
	admittedAt, revisions, _ := admission.AdmittedAt()
	record := operation.ExecutionRecord{
		Contract: operation.ContractOperationV1, Intent: intent, AdmittedAt: &admittedAt, AdmittedRevision: &revisions, Route: &route,
		Dispatch: &operation.DispatchEvidence{AttemptID: "attempt:evse:01", Started: input.Dispatch.Started, Completed: input.Dispatch.Completed, Delivery: input.Dispatch.Delivery, PossibleSideEffect: input.Dispatch.PossibleSideEffect, Evidence: []semreg.EvidenceRef{evidence(7)}},
		Readback: &operation.Readback{SnapshotID: later.SnapshotID, Revisions: input.Readback.Revisions, CandidateID: input.Readback.CandidateID, CandidateRevision: input.Readback.CandidateRevision, BindingID: input.Readback.BindingID, SourceID: input.Readback.SourceID, SourceEpochID: input.Readback.SourceEpochID, DriverGeneration: input.Readback.DriverGeneration, Relation: input.Readback.Relation, Evaluation: input.Readback.Evaluation, Evidence: []semreg.EvidenceRef{evidence(9)}},
		Outcome:  input.Outcome, OutcomeEvidence: []semreg.EvidenceRef{evidence(8)},
	}
	if input.Acknowledgement == "accepted" {
		record.Acknowledgement = &operation.Acknowledgement{State: operation.AckAccepted, At: vectorTime("201000000000", "1000"), Evidence: []semreg.EvidenceRef{evidence(8)}}
	}
	return record, later
}

func acceptedResolvedCandidate(t *testing.T, vector operationVector) resolvedCandidateScenario {
	t.Helper()
	var input struct {
		Resolved resolvedCandidateScenario `json:"resolved_candidate"`
	}
	decodeVectorInput(t, vector.Input, &input)
	return input.Resolved
}

func vectorCandidate(id semreg.CandidateID, key semreg.FactKey, value semreg.Value, revision semreg.Uint64, binding semreg.NativeBindingID, source semreg.SourceID, epoch semreg.SourceEpochID, generation semreg.Uint64, received semreg.TimePoint, receipt semreg.MonotonicPoint) semreg.FactCandidate {
	valueCopy, bindingCopy, epochCopy, generationCopy, sourceCopy := value, binding, epoch, generation, source
	return semreg.FactCandidate{
		CandidateID: id, Key: key, Value: &valueCopy,
		Quality:         semreg.Quality{Assertion: semreg.AssertionObserved, Qualification: semreg.QualificationQualified, Promotion: semreg.PromotionPromoted, Validity: semreg.ValidityGood, Availability: semreg.AvailabilityAvailable, Freshness: semreg.FreshnessFresh, Reasons: []semreg.DefinitionID{}},
		Times:           semreg.Times{ReceivedAt: received, ReceiptMonotonic: receipt, EvaluatedAt: received, EvaluateMonotonic: receipt},
		FreshnessPolicy: semreg.FreshnessPolicy{PolicyID: "policy.evse", Version: "1.0.0", FreshForNS: "30000000000", RetainForNS: "120000000000", MaxWallUncertaintyNS: "1000000"},
		BindingID:       &bindingCopy, SourceEpochID: &epochCopy, DriverGeneration: &generationCopy,
		Origin:   semreg.OriginRef{OriginID: semreg.OriginID("origin:" + string(id)), Kind: semreg.OriginNativeObservation, SourceID: &sourceCopy, SourceEpochID: &epochCopy, BindingID: &bindingCopy, Evidence: []semreg.EvidenceRef{evidence(5)}},
		Evidence: []semreg.EvidenceRef{evidence(5)}, Revision: revision,
	}
}

func vectorByID(t *testing.T, vectors []operationVector, id string) operationVector {
	t.Helper()
	for _, vector := range vectors {
		if vector.ID == id {
			return vector
		}
	}
	t.Fatalf("missing vector %s", id)
	return operationVector{}
}

func vectorTime(ns semreg.Int64, uncertainty semreg.Uint64) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: ns, ClockID: "clock.utc", UncertaintyNS: uncertainty}
}

func bytesEqual(left, right []byte) bool { return reflect.DeepEqual(left, right) }

func factKeyEqualForVector(left, right semreg.FactKey) bool {
	leftJSON, _ := semreg.CanonicalJSON(left)
	rightJSON, _ := semreg.CanonicalJSON(right)
	return bytesEqual(leftJSON, rightJSON)
}

func vectorCandidateByID(snapshot semreg.Snapshot, id semreg.CandidateID) (semreg.FactCandidate, bool) {
	for _, envelope := range snapshot.Facts {
		for _, candidate := range envelope.Candidates {
			if candidate.CandidateID == id {
				return candidate, true
			}
		}
	}
	return semreg.FactCandidate{}, false
}
