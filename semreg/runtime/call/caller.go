package call

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

type callerKey struct{}
type modeKey struct{}

// Caller is the calling surface supplied with a request. An absent caller is
// unspecified and is not treated as GraphQL.
type Caller struct {
	Surface       semreg.EvidenceRef
	GraphQL       bool
	Output        *OutputIdentity
	Inbound       *semreg.CausalContext
	Deadline      *semreg.TimePoint
	Qualification string
}

// OutputIdentity is an in-process output. Its idempotency key is used, prefixed
// by DriverID.
type OutputIdentity struct {
	DriverID string
	Key      string
}

// WithCaller returns a context that carries the calling surface.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

// CallerFrom returns the calling surface, if one was supplied.
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	return caller, ok
}

// WithDispatchMode marks persisted, queued or replayed input. The zero mode is
// a direct call.
func WithDispatchMode(ctx context.Context, mode DispatchMode) context.Context {
	return context.WithValue(ctx, modeKey{}, mode)
}

func dispatchMode(ctx context.Context) DispatchMode {
	mode, _ := ctx.Value(modeKey{}).(DispatchMode)
	return mode
}

func evidenceFor(owner, kind, material string) semreg.EvidenceRef {
	sum := sha256.Sum256([]byte(material))
	return semreg.EvidenceRef{
		Owner:     semreg.DefinitionID(owner),
		Kind:      semreg.DefinitionID(kind),
		Digest:    semreg.Digest("sha256:" + hex.EncodeToString(sum[:])),
		Contract:  operation.ContractOperationV1,
		Access:    semreg.EvidenceAccessPublic,
		Redaction: semreg.RedactionNone,
	}
}
