package tree

import (
	"context"
	"fmt"
	"time"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// CutView is one captured cut as the cut source hands it over: the scoped
// CutRef (identifier, completeness, member vector and the omission vector with
// each omitted asset's existing reason), the graph cut, the member snapshots the
// cut was assembled from, the retention context (the retained CutStore and the
// catalog held with that cut) and the evaluation context of the captured owner
// view, retained with that view.
type CutView struct {
	Ref           graph.CutRef
	Cut           *graph.Cut
	Members       []semreg.Snapshot
	Store         *graph.CutStore
	Catalog       *graph.Catalog
	Evaluation    semreg.EvaluationContext
	Cursor        driver.Cursor
	SettingRoutes []driver.SettingRoute
}

// CutSource is the injected cut port. Capture reads the newest cut inside the
// filter; Lookup is the exact retained-cut lookup for a pinned cut; Resume
// returns the semantic commits after a cursor or an explicit resync.
type CutSource interface {
	Capture(context.Context, driver.Filter) (CutView, error)
	Lookup(context.Context, graph.CutRef) (CutView, error)
	Resume(context.Context, driver.Cursor, driver.Filter) (driver.Changes, error)
}

// ExpiredError is what Lookup returns for a pinned cut it no longer retains.
type ExpiredError struct {
	CutID     semreg.Digest
	ExpiredAt time.Time
}

func (e *ExpiredError) Error() string {
	return fmt.Sprintf("cut %s expired at %s", e.CutID, e.ExpiredAt.UTC().Format(time.RFC3339Nano))
}

// InstanceRef names one instance of a driver kind.
type InstanceRef struct {
	Driver   driver.DriverKind
	Instance driver.InstanceKey
}

// InstanceState is the lifecycle state of an instance as its owner names it,
// with the generation it belongs to. The tree does not interpret the name.
type InstanceState struct {
	Name       string `json:"name"`
	Generation uint64 `json:"generation"`
}

// Fence is a generation-fence callback: every registration of the instance
// made at or before the generation is withdrawn.
type Fence struct {
	Instance   InstanceRef
	Generation uint64
}

// LifecycleSource is the injected lifecycle port.
type LifecycleSource interface {
	State(InstanceRef) (InstanceState, error)
	OnFence(func(Fence)) (stop func())
}

// ReceiptID identifies one call.
type ReceiptID string

// PreparedID identifies one prepared call.
type PreparedID string

// Input is the typed input of a method. A semantic method takes the operation
// request; a native method takes its encoded argument.
type Input struct {
	Request *driver.OperationRequest
	Encoded []byte
}

// CallOptions are the options of Call. Timeout is the finite authority of a
// semantic call that its request does not carry.
type CallOptions struct {
	Timeout time.Duration
}

// PortPrepared is what the dispatcher returns from Prepare.
type PortPrepared struct {
	ID      PreparedID
	Request driver.OperationRequest
}

// PortReceipt is what the dispatcher returns from Call: the one receipt and,
// when the dispatcher has it, the operation result.
type PortReceipt struct {
	ID     ReceiptID
	Result *driver.OperationResult
}

// OperationPort is the injected operation port: Prepare, Call and Outcome of the
// one dispatcher. The synchronous call-bound path is the scoped session's and is
// not on this port.
type OperationPort interface {
	Prepare(context.Context, graph.CutRef, semreg.DefinitionRef, Input) (PortPrepared, error)
	Call(context.Context, PortPrepared, CallOptions) (PortReceipt, error)
	Outcome(context.Context, ReceiptID) (driver.OperationResult, error)
}
