package call

import (
	"reflect"
	"sync"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// Scope is the session scope and generation SS10 passes to registration.
type Scope struct {
	Instance   string
	Generation uint64
	Producer   driver.NodeRef
}

type registryKey struct {
	binding semreg.NativeBindingID
	pack    string
	version string
	op      string
}

type registration struct {
	scope Scope
	route driver.SettingRoute
	exec  driver.Executor
	token uint64
	gen   uint64
}

type executorRegistry struct {
	mu      sync.Mutex
	next    uint64
	entries map[registryKey]registration
	retired map[uint64]bool
}

func newExecutorRegistry() *executorRegistry {
	return &executorRegistry{entries: map[registryKey]registration{}, retired: map[uint64]bool{}}
}

func routeKey(route driver.SettingRoute) registryKey {
	return registryKey{
		binding: route.Route.BindingID,
		pack:    string(route.Operation.Pack.ID) + " " + string(route.Operation.Pack.Version),
		version: string(route.Operation.Version),
		op:      string(route.Operation.ID),
	}
}

func (r *executorRegistry) Register(scope Scope, route driver.SettingRoute, exec driver.Executor) (func(), error) {
	if scope.Generation == 0 || exec == nil {
		return nil, errRegister("registration")
	}
	if scope.Producer != route.Owner.Producer {
		return nil, errRegister("foreign source")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retired[scope.Generation] {
		return nil, errRegister("retired session")
	}
	key := routeKey(route)
	if _, exists := r.entries[key]; exists {
		return nil, errRegister("duplicate owner")
	}
	r.next++
	token := r.next
	r.entries[key] = registration{scope: scope, route: route, exec: exec, token: token, gen: scope.Generation}
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		current, ok := r.entries[key]
		if ok && current.token == token {
			delete(r.entries, key)
		}
	}, nil
}

func (r *executorRegistry) Retire(generation uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retired[generation] = true
	for key, entry := range r.entries {
		if entry.gen == generation {
			delete(r.entries, key)
		}
	}
}

func (r *executorRegistry) Lookup(route driver.SettingRoute) (driver.Executor, bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[routeKey(route)]
	if !ok || r.retired[entry.gen] {
		return nil, false, false
	}
	if !reflect.DeepEqual(entry.route, route) {
		return nil, true, false
	}
	return entry.exec, true, true
}

type registerError struct{ detail string }

func (e registerError) Error() string { return e.detail }

func errRegister(detail string) error { return registerError{detail: detail} }
