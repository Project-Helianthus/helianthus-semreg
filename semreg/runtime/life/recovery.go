package life

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RecoveryClock supplies process monotonic samples and restart-comparable
// elapsed-time anchors in nanoseconds. Elapsed must fail when continuity
// cannot be verified, including a backward or foreign anchor.
type RecoveryClock interface {
	Sample() (uint64, error)
	Anchor(sample uint64) ([]byte, error)
	Elapsed(anchor []byte, sample uint64) (uint64, error)
}

// Clock is the installation's ordinary clock service: the wall time the
// lifecycle supervisor takes its deadlines from, and the recovery clock.
type Clock interface {
	Now() time.Time
	RecoveryClock
}

// EmptyRestorer is an optional RecoveryAdapter extension. Start calls
// RestoreEmpty when no recovery material is committed, so the adapter opens its
// managed writers only after the service's recovery step.
type EmptyRestorer interface {
	RestoreEmpty(ctx context.Context) error
}

// ReadbackBinder is an optional RecoveryAdapter extension for an adapter that
// writes its own recovery envelope through Store. Start binds the service's
// bounded readback resolver before restoration, so an ambiguous outcome of the
// adapter's own store write arms the same resolver as a service-owned write,
// and the service calls ReadbackResolved whenever a readback reopened
// admission. Neither call writes to the store or calls the publisher.
type ReadbackBinder interface {
	BindReadback(arm func())
	ReadbackResolved()
}

// RestoreError names the instances a restoration failed closed. Start withholds
// only the named instances.
type RestoreError struct {
	Drivers []InstanceID
	Err     error
}

func (e *RestoreError) Error() string {
	return fmt.Sprintf("life: recovery failed closed for instances %v: %v", e.Drivers, e.Err)
}

func (e *RestoreError) Unwrap() error { return e.Err }

var errNoRecoveryAdapter = errors.New("life: a recovery adapter is required")
