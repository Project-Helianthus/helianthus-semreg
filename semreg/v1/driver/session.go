package driver

import (
	"context"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// PublicationTransaction is the session's publication port for one transaction.
// Everything a driver stages through it carries that transaction's driver,
// instance, epoch and generation ownership stamp.
type PublicationTransaction interface {
	// OperatorLinks takes the caller's batch binding upserts and returns the
	// pending operator associations for this transaction's ownership scope, with
	// the runtime's current committed bindings included. It confers no
	// configuration authority.
	OperatorLinks([]v1.NativeBinding) []v1.IdentityLink
	// AdmitManufacturerSerial admits a driver's own device-reported
	// manufacturer and serial evidence in this transaction.
	AdmitManufacturerSerial(ManufacturerSerialClaim) error
	// Stage adds a kernel publication batch (facts, sources, bindings, identity
	// links, services, capabilities and their withdrawals) to this transaction.
	Stage(v1.PublicationBatch) error
	// DeclareSource stages the declaration of a source of one element, with its
	// driver-supplied priority, in this transaction.
	DeclareSource(Source) error
	// DeclareSettingRoute stages the route to the owner of one setting in this
	// transaction, so the source, its candidate and its route commit together.
	DeclareSettingRoute(SettingRoute) error
	// Commit publishes everything staged in this transaction under its ownership
	// stamp as one commit.
	Commit(context.Context) error
	// Abort discards everything staged and ends the transaction.
	Abort(context.Context) error
}

// Publisher is the session's own transactional publisher.
type Publisher interface {
	Begin(context.Context) (PublicationTransaction, error)
}

// Session is what Instance.Start receives. A driver imports neither a host nor a
// concrete runtime to use it.
type Session interface {
	// Native is the instance- and generation-rooted native registry.
	Native() NativeRegistry
	// SemanticInput is the scoped read of SemReg.
	SemanticInput() SemanticInput
	// ScopedOperations invokes operations with the session's caller identity and
	// scope.
	ScopedOperations() ScopedOperations
	// Calendar is the installation's calendar service.
	Calendar() Calendar
	// Publisher is the session's own transactional publisher.
	Publisher() Publisher
	// RegisterExecutor registers the driver's own callback for its setting
	// route. Registration may precede the matching route publication in the
	// same generation, but alone it cannot authorize a send. Stopping or
	// replacing the generation disables it, and an unregister returned for an old
	// generation cannot remove its successor.
	RegisterExecutor(SettingRoute, Executor) (unregister func(), err error)
}
