package call

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

var idSeq atomic.Uint64

func freshID(prefix string) string {
	return prefix + ":" + strconv.FormatUint(idSeq.Add(1), 10)
}

// Draft is the closed request and the case the runtime intent owner completes.
// The caller does not supply the effect.
type Draft struct {
	Request   driver.OperationRequest
	Selection Selection
	Resource  semreg.ResourceID
	CallBound bool
	Now       time.Time
}

// IntentOwner builds the process intent for one request. A nil owner makes the
// router answer UNAVAILABLE and send nothing.
type IntentOwner interface {
	Complete(context.Context, Draft) (operation.Intent, error)
}

// NewIntentOwner returns the runtime intent owner for one composed target.
func NewIntentOwner(target semreg.TargetID) IntentOwner {
	return intentOwner{target: target}
}

type intentOwner struct {
	target semreg.TargetID
}

func (o intentOwner) Complete(ctx context.Context, draft Draft) (operation.Intent, error) {
	caller, hasCaller := CallerFrom(ctx)
	op := draft.Selection.Operation
	instance := draft.Request.CapabilityInstanceID
	text := string(draft.Resource)
	intent := operation.Intent{
		Contract: operation.ContractOperationV1,
		IntentID: semreg.IntentID(freshID("intent")),
		Kind:     op.Ref,
		ExpectedEffect: operation.ExpectedEffect{
			Rule:     draft.Selection.EffectRule,
			Fact:     effectFact(op, draft.Selection, text),
			Operator: semreg.PredicateEqual,
			Expected: draft.Selection.Value,
		},
		AssetID:   draft.Request.AssetID,
		Arguments: copyFields(draft.Request.Arguments),
		RequiredCapability: operation.CapabilityRequirement{
			Pack:         op.Capability.Pack,
			DefinitionID: op.Capability.ID,
			Versions: semreg.VersionRange{
				Minimum:          op.Capability.Version,
				MaximumExclusive: nextMajor(op.Capability.Version),
			},
			InstanceID: &instance,
		},
		Authority:                          authorityFor(caller, hasCaller),
		ExpectedSemanticRevision:           draft.Request.ExpectedSemanticRevision,
		ExpectedCapabilityRevision:         draft.Request.ExpectedCapabilityRevision,
		ExpectedCapabilityInstanceRevision: draft.Request.ExpectedCapabilityInstanceRevision,
		ExpectedSourceEpochID:              draft.Request.ExpectedSourceEpochID,
		ExpectedDriverGeneration:           draft.Request.ExpectedDriverGeneration,
		Preconditions:                      copyPreconditions(draft.Request.Preconditions),
		IdempotencyKey:                     idempotencyFor(caller, hasCaller),
	}
	now := timePoint(draft.Now)
	var deadline time.Time
	if !draft.CallBound {
		deadline = finiteDeadline(ctx, draft.Now, draft.Request.Timeout, caller, hasCaller)
		intent.Deadline = timePoint(deadline)
	}
	var causal semreg.CausalContext
	var err error
	if hasCaller && caller.Inbound != nil {
		if draft.CallBound {
			causal, err = operation.EnterCausalWithoutExpiry(*caller.Inbound, o.target, now)
		} else {
			causal, err = operation.EnterCausal(*caller.Inbound, o.target, now)
		}
		if err != nil {
			return operation.Intent{}, err
		}
	} else {
		causal = createdCausal(caller, hasCaller, o.target, now, draft.CallBound, deadline)
	}
	intent.Causal = causal
	return intent, nil
}

func copyFields(in []semreg.TypedField) []semreg.TypedField {
	if in == nil {
		return nil
	}
	out := make([]semreg.TypedField, len(in))
	copy(out, in)
	return out
}

func copyPreconditions(in []operation.Precondition) []operation.Precondition {
	if in == nil {
		return nil
	}
	out := make([]operation.Precondition, len(in))
	copy(out, in)
	return out
}

func effectFact(op semreg.OperationMetadata, sel Selection, resource string) semreg.FactKey {
	return semreg.FactKey{
		PackID:      op.Ref.Pack.ID,
		PackVersion: op.Ref.Pack.Version,
		FactID:      sel.EffectField,
		Dimensions:  []semreg.Dimension{{ID: sel.Dimension, Value: textValue(resource)}},
	}
}

func authorityFor(caller Caller, hasCaller bool) semreg.EvidenceRef {
	if hasCaller && caller.Surface.Digest != "" {
		return caller.Surface
	}
	kind := "semantic.caller.unspecified"
	material := "caller:unspecified"
	if hasCaller && caller.Output != nil {
		kind = "semantic.caller.output"
		material = "caller:output:" + caller.Output.DriverID
	} else if hasCaller && caller.GraphQL {
		kind = "semantic.caller.graphql"
		material = "caller:graphql"
	}
	return evidenceFor("semantic.caller", kind, material)
}

func idempotencyFor(caller Caller, hasCaller bool) semreg.IdempotencyKey {
	if hasCaller && caller.Output != nil && caller.Output.Key != "" {
		return semreg.IdempotencyKey(caller.Output.DriverID + ":" + caller.Output.Key)
	}
	return semreg.IdempotencyKey(freshID("idem"))
}

func createdCausal(caller Caller, hasCaller bool, target semreg.TargetID, now semreg.TimePoint, callBound bool, deadline time.Time) semreg.CausalContext {
	origin := semreg.OriginRef{
		OriginID: "origin:unspecified",
		Kind:     semreg.OriginOperator,
		Evidence: []semreg.EvidenceRef{evidenceFor("semantic.caller", "semantic.caller.origin", "origin:unspecified")},
	}
	if hasCaller && caller.Output != nil {
		origin = semreg.OriginRef{
			OriginID: semreg.OriginID(caller.Output.DriverID),
			Kind:     semreg.OriginAutomation,
			Evidence: []semreg.EvidenceRef{evidenceFor("semantic.caller", "semantic.caller.origin", "origin:"+caller.Output.DriverID)},
		}
	} else if hasCaller && caller.GraphQL {
		origin.OriginID = "origin:graphql"
		origin.Evidence = []semreg.EvidenceRef{evidenceFor("semantic.caller", "semantic.caller.origin", "origin:graphql")}
	}
	causal := semreg.CausalContext{
		Origin:        origin,
		CorrelationID: semreg.CorrelationID(freshID("corr")),
		HopCount:      1,
		MaxHops:       8,
		FirstSeenAt:   now,
		Path:          []semreg.TargetID{target},
	}
	if !callBound {
		causal.ExpiresAt = timePoint(deadline)
	}
	return causal
}

func finiteDeadline(ctx context.Context, now time.Time, timeout time.Duration, caller Caller, hasCaller bool) time.Time {
	deadline := now.Add(timeout)
	if hasCaller && caller.Deadline != nil {
		if absolute, ok := pointTime(*caller.Deadline); ok && absolute.Before(deadline) {
			deadline = absolute
		}
	}
	if absolute, ok := ctx.Deadline(); ok && absolute.Before(deadline) {
		deadline = absolute
	}
	return deadline
}

func timePoint(t time.Time) semreg.TimePoint {
	return semreg.TimePoint{UnixNanoseconds: semreg.Int64(strconv.FormatInt(t.UnixNano(), 10)), ClockID: "clock.utc", UncertaintyNS: "0"}
}

func pointTime(point semreg.TimePoint) (time.Time, bool) {
	ns, err := strconv.ParseInt(string(point.UnixNanoseconds), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, ns).UTC(), true
}

func nextMajor(version semreg.SemanticVersion) semreg.SemanticVersion {
	major := 0
	for _, c := range version {
		if c < '0' || c > '9' {
			break
		}
		major = major*10 + int(c-'0')
	}
	return semreg.SemanticVersion(strconv.Itoa(major+1) + ".0.0")
}
