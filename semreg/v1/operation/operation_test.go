package operation_test

import (
	"testing"

	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func TestPublicOperationSurface(t *testing.T) {
	if operation.ContractOperationV1 != "helianthus.semantic.operation/v1" {
		t.Fatalf("unexpected operation contract: %q", operation.ContractOperationV1)
	}
	_ = operation.Intent{}
}
