package call

import (
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

// RetainedOperations is the in-memory admission bound of this router. It is a
// memory bound, not a product limit.
const RetainedOperations = 8192

func builtinValidators() []semreg.PackValidator {
	return []semreg.PackValidator{
		thermal.New(),
		pv.New(),
		storage.New(),
		evse.New(),
		infrastructure.New(),
	}
}

// NewBuiltinKernel composes the thermal, PV, storage, EVSE and infrastructure
// packs and retains RetainedOperations admissions.
func NewBuiltinKernel() (*operation.Kernel, error) {
	return operation.NewKernelWithOptions(operation.KernelOptions{MaxRetainedOperations: RetainedOperations}, builtinValidators()...)
}
