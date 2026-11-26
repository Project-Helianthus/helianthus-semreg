package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/catalog"
)

// Config is the one configuration record of an instance, as the host supplies it.
// Instance is the exact configured InstanceKey and Enabled its configured flag.
// Transport and Options are two separate owned JSON byte operands that the
// factory decodes strictly. SecretRef is the instance's optional opaque
// reference exactly as configured, empty when omitted; it stays outside both
// operands and scoped to that instance. Plan neither dereferences it nor reads
// secret material.
type Config struct {
	Instance  string
	Enabled   bool
	Transport json.RawMessage
	Options   json.RawMessage
	SecretRef string
}

// RetryPolicy is the neutral retry record of an instance plan. Normalization
// and lifecycle are implemented by the runtime: a negative Budget becomes 0; an
// InitialDelay of zero or less becomes one second; a MaxDelay below InitialDelay
// becomes InitialDelay; a JitterRatio of 0 becomes 0.2, a negative one 0 and one
// above 0.5 becomes 0.5; with Persistent.Enabled its delays follow the same
// initial and maximum rules, and are left as given otherwise.
type RetryPolicy struct {
	Budget       int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	JitterRatio  float64
	Persistent   PersistentRetry
}

// PersistentRetry is the persistent part of a RetryPolicy.
type PersistentRetry struct {
	Enabled      bool
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// ShareMode says how a transport claim may be shared.
type ShareMode string

// ShareMode values.
const (
	// ShareSerialized is use of a compatible serialized bus.
	ShareSerialized ShareMode = "serialized"
	// ShareExclusive is exclusive use.
	ShareExclusive ShareMode = "exclusive"
)

// TransportClaim is one transport an instance plan claims. LinkNodeSlot, when
// non-empty, is the claiming driver's already configured durable equipment slot,
// inside the same driver and instance, for the link equipment it publishes
// itself; empty keeps the ordinary configured equipment key. It does not affect
// the transport overlap or sharing of Provider, ConfigKey and AccessDomain.
// Rejecting colliding claims is planning, not validation of one claim.
type TransportClaim struct {
	Provider     string
	ConfigKey    string
	AccessDomain string
	Share        ShareMode
	LinkNodeSlot string
}

// Validate checks the three identifying members and the share mode, and a
// non-empty LinkNodeSlot with the validation of a configured anchor slot.
func (c TransportClaim) Validate() error {
	if err := first(required("provider", c.Provider), required("config key", c.ConfigKey), required("access domain", c.AccessDomain)); err != nil {
		return err
	}
	if c.Share != ShareSerialized && c.Share != ShareExclusive {
		return fmt.Errorf("share mode %q is neither serialized nor exclusive", c.Share)
	}
	if c.LinkNodeSlot != "" {
		return within("link node slot", AnchorKey{Slot: c.LinkNodeSlot}.Validate())
	}
	return nil
}

// InstancePlan is one planned instance of a driver. Transports may be empty: an
// instance with no transport is valid.
type InstancePlan struct {
	Driver      DriverKind
	Instance    InstanceKey
	Transports  []TransportClaim
	RetryPolicy RetryPolicy
}

// Validate checks the driver kind, the instance key and every transport claim.
func (p InstancePlan) Validate() error {
	if err := first(within("driver", p.Driver.Validate()), within("instance", p.Instance.Validate())); err != nil {
		return err
	}
	for i, t := range p.Transports {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("transports[%d]: %w", i, err)
		}
	}
	return nil
}

// Descriptor carries the families a driver declares. The record type is
// catalog.Family, the one family record of SemReg, so any host reads every linked
// driver's families through SemReg. Kinds are the driver kinds the factory
// plans; the host takes the owner of a configured instance from them alone.
type Descriptor struct {
	Families []catalog.Family
	Kinds    []DriverKind
}

// Validate checks every family record and every declared kind.
func (d Descriptor) Validate() error {
	for i, k := range d.Kinds {
		if err := k.Validate(); err != nil {
			return fmt.Errorf("kinds[%d]: %w", i, err)
		}
	}
	for i, f := range d.Families {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("families[%d] %q: %w", i, f.FamilyID, err)
		}
	}
	return nil
}

// Factory is the public constructor surface of a driver.
type Factory interface {
	// Descriptor returns the families the driver declares.
	Descriptor() Descriptor
	// Plan is pure: it may return a set of endpoints and performs no dial and
	// no side effect.
	Plan(Config) ([]InstancePlan, error)
	// Construct builds one planned instance.
	Construct(context.Context, InstancePlan) (Instance, error)
}

// EarlyDispatcher is implemented by a factory that recognizes a command before
// the host parses configuration. The host calls the selected factories that
// implement it and returns the handled exit code or error; a factory that does
// not implement it is not called.
type EarlyDispatcher interface {
	DispatchEarly(context.Context, []string) (handled bool, exitCode int, err error)
}

// CloseReport is the result of stopping an instance. Confirmed is false when the
// close could not be confirmed; Err then says why.
type CloseReport struct {
	Confirmed bool
	Err       error
}

// Instance is one running instance of a driver.
type Instance interface {
	// Start returns after admission or a classified start result.
	Start(context.Context, Session) error
	// FenceWithdrawal is nonblocking and makes no callback into the supervisor.
	FenceWithdrawal()
	// Stop stops the work, drains and closes, and reports an unconfirmed close.
	Stop(context.Context) CloseReport
}
