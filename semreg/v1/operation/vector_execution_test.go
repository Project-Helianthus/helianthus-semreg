package operation_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

type operationVector struct {
	ID         string          `json:"id"`
	Polarity   string          `json:"polarity"`
	RecordType string          `json:"record_type"`
	Operation  string          `json:"operation"`
	Coverage   []string        `json:"coverage"`
	Input      json.RawMessage `json:"input"`
	PriorState json.RawMessage `json:"prior_state,omitempty"`
	Expect     struct {
		Result     string         `json:"result"`
		ErrorID    semreg.ErrorID `json:"error_id,omitempty"`
		Assertions []string       `json:"assertions,omitempty"`
		Unchanged  bool           `json:"state_unchanged,omitempty"`
	} `json:"expect"`
	Criterion string `json:"criterion"`
}

type vectorExpansion struct {
	Intent    *operation.Intent
	Snapshot  *semreg.Snapshot
	Current   *semreg.EvaluationContext
	Record    *operation.ExecutionRecord
	Causal    *semreg.CausalContext
	Selection *semreg.Selection
}

type vectorIndexPack struct {
	index       semreg.DefinitionIndex
	intentCalls int
}

func (p *vectorIndexPack) Pack() semreg.PackRef                { return p.index.Pack }
func (p *vectorIndexPack) Definitions() semreg.DefinitionIndex { return p.index }
func (p *vectorIndexPack) ValidateFact(_ semreg.FactKey, value *semreg.Value) error {
	if value == nil {
		return nil
	}
	return value.Validate()
}
func (p *vectorIndexPack) ValidateService(semreg.ServiceInstance) error       { return nil }
func (p *vectorIndexPack) ValidateCapability(semreg.CapabilityInstance) error { return nil }
func (p *vectorIndexPack) ValidateField(_ semreg.DefinitionRef, field semreg.TypedField) error {
	return field.Validate()
}
func (p *vectorIndexPack) MatchConstraints(semreg.CapabilityInstance, []semreg.TypedField) error {
	return nil
}
func (p *vectorIndexPack) EvaluatePredicate(semreg.FactCandidate, semreg.PredicateOp, semreg.Value) (bool, error) {
	return true, nil
}
func (p *vectorIndexPack) ValidateIntent(operation.Intent) error {
	p.intentCalls++
	return nil
}
func (p *vectorIndexPack) EvaluateReadback(operation.Intent, semreg.FactCandidate) (operation.ReadbackRelation, error) {
	return operation.ReadbackConfirms, nil
}

func TestAcceptedOperationVectorsExecute(t *testing.T) {
	vectors := loadOperationVectors(t)
	if len(vectors) != 29 {
		t.Fatalf("selected vector count: %d", len(vectors))
	}
	for _, vector := range vectors {
		vector := vector
		t.Run(vector.ID, func(t *testing.T) {
			if len(vector.Input) == 0 || vector.Criterion == "" || vector.Expect.Result == "" {
				t.Fatal("incomplete selected vector")
			}
			fixture := newOperationFixture(t)
			stateBefore, err := semreg.CanonicalJSON(fixture.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			expansion := vectorExpansion{Intent: &fixture.intent, Snapshot: &fixture.snapshot, Current: &fixture.current}
			var result semreg.Record
			var got error
			admit := func(authority operation.AuthorityResolver) *operation.Admission {
				stateBefore, err = semreg.CanonicalJSON(fixture.snapshot)
				if err != nil {
					t.Fatal(err)
				}
				admission, err := fixture.kernel.Admit(fixture.snapshot, fixture.current, fixture.intent, authority)
				got = err
				if admission != nil {
					result = admission.Intent()
				}
				return admission
			}

			switch vector.ID {
			case "K-POS-012":
				admission := admit(operation.AuthorityResolverFunc(authorize))
				if admission != nil {
					claims, ok := admission.GuardClaims()
					if !ok || claims.SourceEpochID != fixture.intent.ExpectedSourceEpochID || claims.DriverGeneration != fixture.intent.ExpectedDriverGeneration {
						t.Fatal("inexact guarded route claims")
					}
				}
			case "K-POS-013":
				var input struct {
					Route            semreg.CapabilityInstanceID `json:"route"`
					DispatchDelivery operation.DeliveryState     `json:"dispatch_delivery"`
					Acknowledgement  operation.AckState          `json:"acknowledgement"`
					Readback         string                      `json:"readback"`
					Outcome          operation.Outcome           `json:"outcome"`
				}
				decodeVectorInput(t, vector.Input, &input)
				fixture.snapshot.Capabilities[0].InstanceID = input.Route
				sealCorrectionSnapshot(t, &fixture.snapshot)
				admission := admit(operation.AuthorityResolverFunc(authorize))
				if got == nil {
					route, _ := admission.Route()
					if route.CapabilityInstance != input.Route || input.Readback != "absent" {
						t.Fatalf("exact acknowledgement vector values: %+v route=%+v", input, route)
					}
					record := admittedRecord(admission)
					record.Outcome = input.Outcome
					record.Dispatch = dispatch(input.DispatchDelivery, true)
					record.Acknowledgement = &operation.Acknowledgement{State: input.Acknowledgement, At: timePoint("210"), Evidence: []semreg.EvidenceRef{evidence(8)}}
					stored, err := fixture.kernel.Record(admission, record, nil)
					got, result, expansion.Record = err, stored, &record
				}
			case "K-POS-014":
				admission := admit(operation.AuthorityResolverFunc(authorize))
				if got == nil {
					snapshot := applyReadback(t, fixture, true, "220", "220")
					record := admittedRecord(admission)
					record.Outcome, record.Dispatch, record.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), readback(snapshot, operation.ReadbackConfirms)
					stored, err := fixture.kernel.Record(admission, record, &snapshot)
					got, result, expansion.Record, expansion.Snapshot = err, stored, &record, &snapshot
				}
			case "K-POS-015":
				var input struct {
					Origin struct {
						OriginID      semreg.OriginID   `json:"origin_id"`
						Kind          semreg.OriginKind `json:"kind"`
						EvidenceCount int               `json:"evidence_count"`
					} `json:"origin"`
					CorrelationID semreg.CorrelationID `json:"correlation_id"`
					HopCount      uint16               `json:"hop_count"`
					MaxHops       uint16               `json:"max_hops"`
					Path          []semreg.TargetID    `json:"path"`
					LifetimeNS    semreg.Uint64        `json:"lifetime_ns"`
					Authority     bool                 `json:"authority_resolves"`
				}
				var prior struct {
					SameValue     bool                 `json:"earlier_projection_same_value"`
					CorrelationID semreg.CorrelationID `json:"earlier_correlation_id"`
				}
				decodeVectorInput(t, vector.Input, &input)
				decodeVectorInput(t, vector.PriorState, &prior)
				if !input.Authority || !prior.SameValue || input.LifetimeNS != "60000000000" || prior.CorrelationID == input.CorrelationID || input.Origin.EvidenceCount != 1 {
					t.Fatalf("exact independent-intent vector values: input=%+v prior=%+v", input, prior)
				}
				fixture.intent.IntentID = "intent:independent"
				fixture.intent.IdempotencyKey = "idempotency:independent"
				fixture.intent.Causal.CorrelationID = input.CorrelationID
				fixture.intent.Causal.Origin.OriginID, fixture.intent.Causal.Origin.Kind = input.Origin.OriginID, input.Origin.Kind
				fixture.intent.Causal.HopCount = input.HopCount
				fixture.intent.Causal.MaxHops = input.MaxHops
				fixture.intent.Causal.Path = input.Path
				admit(operation.AuthorityResolverFunc(authorize))
			case "K-POS-022":
				var input struct {
					Indexes []semreg.DefinitionIndex `json:"validators_in_registration_order"`
					Kind    semreg.DefinitionRef     `json:"intent_kind"`
					Reverse bool                     `json:"repeat_with_registration_order_reversed"`
				}
				decodeVectorInput(t, vector.Input, &input)
				if len(input.Indexes) != 2 || !input.Reverse || len(input.Indexes[1].Operations) != 1 || input.Indexes[1].Operations[0] != input.Kind {
					t.Fatalf("exact owner vector values: %+v", input)
				}
				var executedIntent operation.Intent
				for _, reverse := range []bool{false, true} {
					other := &vectorIndexPack{index: completeVectorIndex(input.Indexes[0])}
					owner := &vectorIndexPack{index: completeVectorIndex(input.Indexes[1])}
					packs := []semreg.PackValidator{other, owner}
					if reverse {
						packs[0], packs[1] = packs[1], packs[0]
					}
					kernel, err := operation.NewKernel(packs...)
					if err != nil {
						t.Fatal(err)
					}
					intent := fixture.intent
					intent.Kind = input.Kind
					intent.Arguments, intent.Preconditions = []semreg.TypedField{}, []operation.Precondition{}
					intent.ExpectedEffect = operation.ExpectedEffect{Rule: owner.index.EffectRules[0], Fact: semreg.FactKey{PackID: owner.index.Pack.ID, PackVersion: owner.index.Pack.Version, FactID: "helianthus.evse.fact.current_limit", Dimensions: []semreg.Dimension{}}, Operator: semreg.PredicateEqual, Expected: boolValue(true)}
					intent.RequiredCapability = operation.CapabilityRequirement{Pack: owner.index.Pack, DefinitionID: owner.index.Capabilities[0].ID, Versions: semreg.VersionRange{Minimum: owner.index.Capabilities[0].Version, MaximumExclusive: "2.0.0"}}
					executedIntent = intent
					got = kernel.ValidateIntent(intent)
					if got != nil || owner.intentCalls != 1 || other.intentCalls != 0 {
						t.Fatalf("registration-order dispatch: owner=%d other=%d err=%v", owner.intentCalls, other.intentCalls, got)
					}
				}
				result = executedIntent
			case "K-POS-023":
				var input struct {
					OriginTarget semreg.TargetID `json:"origin_target"`
					MaxHops      uint16          `json:"max_hops"`
					States       []struct {
						Event       string            `json:"event"`
						Receiver    semreg.TargetID   `json:"receiver"`
						Accepted    []semreg.TargetID `json:"accepted_path"`
						AcceptedHop uint16            `json:"accepted_hop_count"`
					} `json:"states"`
				}
				decodeVectorInput(t, vector.Input, &input)
				if len(input.States) == 0 || input.States[0].Receiver != input.OriginTarget {
					t.Fatalf("exact causal path vector values: %+v", input)
				}
				causal := fixture.intent.Causal
				causal.MaxHops = input.MaxHops
				for _, state := range input.States {
					if state.Receiver == "" {
						continue
					}
					causal, got = operation.EnterCausal(causal, state.Receiver, timePoint("150"))
					if got != nil || !bytes.Equal(mustJSON(t, causal.Path), mustJSON(t, state.Accepted)) || causal.HopCount != state.AcceptedHop {
						t.Fatalf("%s expansion: path=%v err=%v", state.Event, causal.Path, got)
					}
				}
				expansion.Causal, result = &causal, causal
			case "K-NEG-013":
				fixture.snapshot.Bindings[0].State = semreg.BindingFenced
				fixture.snapshot.IdentityLinks[0].State = semreg.LinkWithdrawn
				fixture.snapshot.Services[0].Availability = semreg.AvailabilityWithdrawn
				fixture.snapshot.Capabilities[0].Availability = semreg.AvailabilityWithdrawn
				fixture.snapshot.Facts = []semreg.FactEnvelope{}
				fixture.snapshot.Cursors[0].Fenced = true
				fixture.snapshot.Fences = []semreg.GenerationFence{{SourceID: "source:test", SourceEpochID: "epoch:test:1", DriverGeneration: "1", Reason: "reason.fence", Evidence: []semreg.EvidenceRef{evidence(6)}, Revision: "1"}}
				sealCorrectionSnapshot(t, &fixture.snapshot)
				admit(operation.AuthorityResolverFunc(authorize))
			case "K-NEG-016":
				second := fixture.snapshot.Capabilities[0]
				second.InstanceID = "capability:limit:02"
				fixture.snapshot.Capabilities = append([]semreg.CapabilityInstance{second}, fixture.snapshot.Capabilities...)
				sealCorrectionSnapshot(t, &fixture.snapshot)
				admit(operation.AuthorityResolverFunc(authorize))
			case "K-NEG-017":
				var input struct{ Now, Deadline semreg.Int64 }
				var raw map[string]semreg.Int64
				decodeVectorInput(t, vector.Input, &raw)
				input.Now, input.Deadline = raw["now_latest_ns"], raw["deadline_earliest_ns"]
				fixture.current = context(string(input.Now), string(input.Now))
				fixture.intent.Deadline = timePoint(string(input.Deadline))
				fixture.intent.Causal.ExpiresAt = timePoint("3000")
				admit(operation.AuthorityResolverFunc(authorize))
			case "K-NEG-018":
				fixture = newOperationFixtureWith(t, func(batch *semreg.PublicationBatch) {
					batch.FactUpserts = append(batch.FactUpserts, factCandidate("candidate:z", preKey, false, "100", "100", "1"))
				})
				expansion.Intent, expansion.Snapshot, expansion.Current = &fixture.intent, &fixture.snapshot, &fixture.current
				admit(operation.AuthorityResolverFunc(authorize))
			case "K-NEG-019":
				var input struct {
					Authority  string `json:"authority"`
					OtherValid bool   `json:"other_fields_valid"`
				}
				decodeVectorInput(t, vector.Input, &input)
				if input.Authority != "unresolved" || !input.OtherValid {
					t.Fatalf("exact authority vector values: %+v", input)
				}
				admit(nil)
			case "K-NEG-020":
				var input struct {
					RouteSource       string             `json:"route_source"`
					SelectedCandidate semreg.CandidateID `json:"selected_candidate"`
				}
				decodeVectorInput(t, vector.Input, &input)
				selection := semreg.Selection{Contract: semreg.ContractSelectionV1, SnapshotID: fixture.snapshot.SnapshotID, Revisions: fixture.snapshot.Revisions, EvaluationDigest: semreg.Digest("sha256:" + repeatHex(10)), Context: fixture.current, Key: preKey, PolicyID: "policy.test", PolicyVersion: "1.0.0", SelectedCandidate: "candidate:source:a", CandidateRevision: "1", PresentationOnly: true}
				selection.SelectedCandidate = input.SelectedCandidate
				if input.RouteSource != "presentation_selection" {
					t.Fatalf("exact selection route source: %+v", input)
				}
				expansion.Selection = &selection
				_, got = fixture.kernel.AdmitSelectionRoute(selection)
			case "K-NEG-022":
				var input struct {
					CorrelationID semreg.CorrelationID `json:"correlation_id"`
					Path          []semreg.TargetID    `json:"path"`
					Ingress       semreg.TargetID      `json:"ingress_target"`
					CreateIntent  bool                 `json:"attempts_to_create_intent"`
				}
				decodeVectorInput(t, vector.Input, &input)
				causal := fixture.intent.Causal
				causal.CorrelationID, causal.Path, causal.HopCount = input.CorrelationID, input.Path, uint16(len(input.Path))
				if !input.CreateIntent {
					t.Fatal("exact reflected-intent attempt lost")
				}
				_, got = operation.EnterCausal(causal, input.Ingress, timePoint("150"))
				expansion.Causal = &causal
			case "K-NEG-023":
				var input struct {
					PriorOutcome operation.Outcome `json:"prior_outcome"`
					Possible     bool              `json:"possible_side_effect"`
					Action       string            `json:"requested_action"`
				}
				decodeVectorInput(t, vector.Input, &input)
				admission := admit(operation.AuthorityResolverFunc(authorize))
				if got == nil {
					record := admittedRecord(admission)
					record.Outcome, record.Dispatch = input.PriorOutcome, dispatch(operation.DeliveryUnknown, input.Possible)
					if input.Action != "fallback_to_second_route" {
						t.Fatalf("exact retry action: %+v", input)
					}
					got, expansion.Record = operation.ValidateRetry(record, true), &record
				}
			case "K-NEG-025":
				var input struct {
					LegacyID semreg.OpaqueID `json:"legacy_id"`
				}
				decodeVectorInput(t, vector.Input, &input)
				_, got = fixture.kernel.AdmitAliasRoute(input.LegacyID)
			case "K-NEG-026":
				var input struct {
					Delivery operation.DeliveryState `json:"dispatch_delivery"`
					Possible bool                    `json:"possible_side_effect"`
					Ack      string                  `json:"acknowledgement"`
					Readback string                  `json:"readback"`
					Outcome  operation.Outcome       `json:"outcome"`
				}
				decodeVectorInput(t, vector.Input, &input)
				admission := admit(operation.AuthorityResolverFunc(authorize))
				if got == nil {
					record := admittedRecord(admission)
					record.Outcome, record.Dispatch = input.Outcome, dispatch(input.Delivery, input.Possible)
					if input.Ack != "absent" || input.Readback != "absent" {
						t.Fatalf("exact no-contact vector values: %+v", input)
					}
					got, expansion.Record = record.Validate(), &record
				}
			case "K-NEG-036", "K-NEG-041", "K-NEG-045", "K-NEG-046":
				admission := admit(operation.AuthorityResolverFunc(authorize))
				if got == nil {
					wall, ticks := "220", "220"
					if vector.ID == "K-NEG-045" {
						wall, ticks = "190", "190"
					}
					snapshot := applyReadback(t, fixture, true, wall, ticks)
					record := admittedRecord(admission)
					record.Outcome, record.Dispatch, record.Readback = operation.OutcomeApplied, dispatch(operation.DeliverySent, true), readback(snapshot, operation.ReadbackConfirms)
					switch vector.ID {
					case "K-NEG-036":
						record.Readback.DriverGeneration = "2"
					case "K-NEG-041":
						record.Readback.CandidateID = "candidate:interlock"
						record.Readback.CandidateRevision = "1"
					case "K-NEG-046":
						record.Readback.Evaluation.EvaluateMonotonic.ClockEpochID = "clock-epoch:restart"
						record.Readback.Evaluation.EvaluatedAt.UncertaintyNS = "11"
					}
					_, got = fixture.kernel.Record(admission, record, &snapshot)
					expansion.Record, expansion.Snapshot = &record, &snapshot
				}
			case "K-NEG-038":
				fixture.current = context("1100", "1100")
				fixture.intent.Deadline = timePoint("2000")
				fixture.intent.Causal.ExpiresAt = timePoint("2000")
				admit(operation.AuthorityResolverFunc(authorize))
			case "K-NEG-039", "K-NEG-040":
				fixture = newOperationFixtureWith(t, func(batch *semreg.PublicationBatch) {
					batch.FactUpserts[0].Value = func() *semreg.Value { value := boolValue(false); return &value }()
					candidate := factCandidate("candidate:z", preKey, true, "100", "100", "1")
					candidate.Quality.Qualification, candidate.Quality.Promotion = semreg.QualificationCandidate, semreg.PromotionUnpromoted
					batch.FactUpserts = append(batch.FactUpserts, candidate)
				})
				if vector.ID == "K-NEG-039" {
					fixture.intent.Preconditions[0].CandidateID = "candidate:z"
				}
				expansion.Intent, expansion.Snapshot, expansion.Current = &fixture.intent, &fixture.snapshot, &fixture.current
				admit(operation.AuthorityResolverFunc(authorize))
			case "K-NEG-043":
				var input struct {
					Indexes []semreg.DefinitionIndex `json:"indexes"`
				}
				decodeVectorInput(t, vector.Input, &input)
				if len(input.Indexes) != 2 || len(input.Indexes[0].Operations) != 1 || len(input.Indexes[1].Operations) != 1 || input.Indexes[0].Operations[0].ID != input.Indexes[1].Operations[0].ID {
					t.Fatalf("exact owner-conflict vector values: %+v", input)
				}
				_, got = operation.NewKernel(&vectorIndexPack{index: completeVectorIndex(input.Indexes[0])}, &vectorIndexPack{index: completeVectorIndex(input.Indexes[1])})
			case "K-NEG-044":
				var input struct {
					Registered semreg.PackRef         `json:"registered_pack"`
					Operations []semreg.DefinitionRef `json:"indexed_operations"`
					Kind       semreg.DefinitionRef   `json:"intent_kind"`
					Fallback   semreg.PackRef         `json:"registration_order_fallback_candidate"`
				}
				decodeVectorInput(t, vector.Input, &input)
				missing := &vectorIndexPack{index: completeVectorIndex(semreg.DefinitionIndex{Pack: input.Registered, Operations: input.Operations})}
				fallbackRef := input.Kind
				fallbackRef.Pack = input.Fallback
				fallback := &vectorIndexPack{index: completeVectorIndex(semreg.DefinitionIndex{Pack: input.Fallback, Operations: []semreg.DefinitionRef{fallbackRef}})}
				kernel, err := operation.NewKernel(missing, fallback)
				if err != nil {
					t.Fatal(err)
				}
				intent := fixture.intent
				intent.Kind = input.Kind
				intent.ExpectedEffect.Rule.Pack = input.Registered
				got = kernel.ValidateIntent(intent)
				if missing.intentCalls != 0 || fallback.intentCalls != 0 {
					t.Fatal("missing exact owner probed a hook")
				}
			case "K-NEG-049":
				fixture.intent.Preconditions[0].CandidateID = "candidate:missing"
				admit(operation.AuthorityResolverFunc(authorize))
			case "K-NEG-050":
				fixture.intent.Preconditions[0].CandidateRevision = "2"
				admit(operation.AuthorityResolverFunc(authorize))
			case "K-NEG-051":
				var input struct {
					Receiver      semreg.TargetID   `json:"receiver"`
					IncomingPath  []semreg.TargetID `json:"incoming_path"`
					IncomingHops  uint16            `json:"incoming_hop_count"`
					MaxHops       uint16            `json:"max_hops"`
					AttemptedPath []semreg.TargetID `json:"attempted_path_after_rejection"`
				}
				decodeVectorInput(t, vector.Input, &input)
				causal := fixture.intent.Causal
				causal.Path, causal.HopCount, causal.MaxHops = input.IncomingPath, input.IncomingHops, input.MaxHops
				_, got = operation.EnterCausal(causal, input.Receiver, timePoint("150"))
				if !bytes.Equal(mustJSON(t, causal.Path), mustJSON(t, input.AttemptedPath)) {
					t.Fatal("exact reflected path mutated")
				}
				expansion.Causal = &causal
			case "K-NEG-053":
				var input struct {
					Receiver     semreg.TargetID   `json:"receiver"`
					IncomingPath []semreg.TargetID `json:"incoming_path"`
					IncomingHops uint16            `json:"incoming_hop_count"`
					MaxHops      uint16            `json:"max_hops"`
					Valid        bool              `json:"all_wire_fields_syntactically_valid"`
				}
				decodeVectorInput(t, vector.Input, &input)
				causal := fixture.intent.Causal
				causal.Path, causal.HopCount, causal.MaxHops = input.IncomingPath, input.IncomingHops, input.MaxHops
				if !input.Valid {
					t.Fatal("exact causal budget vector marked malformed")
				}
				_, got = operation.EnterCausal(causal, input.Receiver, timePoint("150"))
				expansion.Causal = &causal
			default:
				t.Fatal("unmapped selected vector")
			}

			if vector.Expect.Result == "accept" {
				if got != nil {
					t.Fatal(got)
				}
				if result != nil {
					canonical, err := semreg.CanonicalJSON(result)
					if err != nil || len(canonical) == 0 {
						t.Fatalf("canonical accepted result: %v", err)
					}
				}
			} else {
				errorID(t, got, vector.Expect.ErrorID)
				after, err := semreg.CanonicalJSON(fixture.snapshot)
				if err != nil || !bytes.Equal(stateBefore, after) {
					t.Fatal("rejected vector mutated semantic state")
				}
			}
			logVectorExpansion(t, vector, expansion)
		})
	}
}

func completeVectorIndex(index semreg.DefinitionIndex) semreg.DefinitionIndex {
	if index.Fields == nil {
		index.Fields = []semreg.DefinitionRef{}
	}
	if index.Services == nil {
		index.Services = []semreg.DefinitionRef{}
	}
	if index.Capabilities == nil {
		index.Capabilities = []semreg.DefinitionRef{}
	}
	if index.Operations == nil {
		index.Operations = []semreg.DefinitionRef{}
	}
	if index.EffectRules == nil {
		index.EffectRules = []semreg.DefinitionRef{}
	}
	return index
}

func loadOperationVectors(t *testing.T) []operationVector {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve vector test source")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "fixtures", "v1", "operation-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		t.Fatal(err)
	}
	var document struct {
		Vectors []operationVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document.Vectors
}

func decodeVectorInput(t *testing.T, raw json.RawMessage, target any) {
	t.Helper()
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}

func logVectorExpansion(t *testing.T, vector operationVector, expansion vectorExpansion) {
	t.Helper()
	value := struct {
		ID         string
		Criterion  string
		Input      json.RawMessage
		PriorState json.RawMessage
		Expect     any
		Expansion  vectorExpansion
	}{vector.ID, vector.Criterion, vector.Input, vector.PriorState, vector.Expect, expansion}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	t.Logf("typed expansion: input=%dB expansion=%dB sha256=%x", len(vector.Input), len(raw), sum)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
