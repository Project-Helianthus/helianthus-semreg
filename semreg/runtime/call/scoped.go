package call

import (
	"context"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Scoped is the SS01 scoped-operations implementation over one router.
type Scoped struct {
	router *Router
	scope  Scope
}

// ScopeOf returns the scoped operations and registration handle for one session.
func (r *Router) ScopeOf(scope Scope) *Scoped {
	return &Scoped{router: r, scope: scope}
}

// Invoke runs one finite request.
func (s *Scoped) Invoke(ctx context.Context, request driver.OperationRequest) (driver.OperationResult, error) {
	return s.router.Invoke(ctx, request)
}

// InvokeCallBound runs one cancellable in-process request with no absolute deadline.
func (s *Scoped) InvokeCallBound(ctx context.Context, request driver.OperationRequest) (driver.OperationResult, error) {
	return s.router.InvokeCallBound(ctx, request)
}

// RegisterExecutor records the executor for one setting route under this scope.
func (s *Scoped) RegisterExecutor(route driver.SettingRoute, exec driver.Executor) (func(), error) {
	return s.router.registry.Register(s.scope, route, exec)
}

// Retire disables every registration of this scope's generation.
func (s *Scoped) Retire() {
	s.router.registry.Retire(s.scope.Generation)
}
