package call

import (
	"reflect"
	"strings"
	"sync"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

// Selection is the one pack case matched to a request's arguments.
type Selection struct {
	Operation  semreg.OperationMetadata
	Release    bool
	EffectRule semreg.DefinitionRef
	Dimension  semreg.DefinitionID
	Value      semreg.Value
}

var (
	metaOnce sync.Once
	metas    []semreg.PackMetadata
)

func packMetadata() []semreg.PackMetadata {
	metaOnce.Do(func() {
		for _, validator := range []semreg.PackValidator{thermal.New(), pv.New(), storage.New(), evse.New(), infrastructure.New()} {
			provider, ok := validator.(semreg.PackMetadataProvider)
			if ok {
				metas = append(metas, provider.Metadata())
			}
		}
	})
	return metas
}

func findOperation(ref semreg.DefinitionRef) (semreg.OperationMetadata, bool) {
	for _, meta := range packMetadata() {
		if meta.Pack != ref.Pack {
			continue
		}
		for _, op := range meta.Operations {
			if op.Ref == ref {
				return op, true
			}
		}
	}
	return semreg.OperationMetadata{}, false
}

func fieldDimension(pack semreg.PackRef, id semreg.DefinitionID) semreg.DefinitionID {
	for _, meta := range packMetadata() {
		if meta.Pack != pack {
			continue
		}
		for _, field := range meta.Fields {
			if field.Ref.ID == id {
				return field.Dimension.ID
			}
		}
	}
	return ""
}

func selectCase(req driver.OperationRequest) (Selection, *routeRefusal) {
	if refusal := argumentShape(req.Arguments); refusal != nil {
		return Selection{}, refusal
	}
	op, ok := findOperation(req.Operation)
	if !ok {
		return Selection{}, failed(ReasonInvalidArgument)
	}
	dimension := fieldDimension(op.Ref.Pack, op.Argument.ID)
	args := indexArgs(req.Arguments)
	if op.Release != nil {
		if field, present := args[op.Argument.ID]; present && field.Value.Kind == semreg.ValueBoolean {
			if len(args) != 1 {
				return Selection{}, failed(ReasonMixedRelease)
			}
			if !reflect.DeepEqual(field.Value, op.Release.Argument) {
				if field.Value.Boolean == nil {
					return Selection{}, failed(ReasonInvalidArgument)
				}
				return Selection{}, failed(ReasonReleaseTrue)
			}
			return Selection{
				Operation:  op,
				Release:    true,
				EffectRule: op.Effect,
				Dimension:  dimension,
				Value:      field.Value,
			}, nil
		}
	}
	if op.Release == nil && releaseShaped(op, args) {
		return Selection{}, failed(ReasonNoReleaseCase)
	}
	primary, ok := args[op.Argument.ID]
	if !ok {
		return Selection{}, failed(ReasonMissingPrimary)
	}
	for id := range args {
		if id == op.Argument.ID {
			continue
		}
		if !declaredParameter(op, id) {
			return Selection{}, failed(ReasonUndeclaredParameter)
		}
	}
	if negativeQuantity(op.Argument.ID, primary.Value) {
		return Selection{}, failed(ReasonNegativeSet)
	}
	return Selection{
		Operation:  op,
		EffectRule: op.Effect,
		Dimension:  dimension,
		Value:      primary.Value,
	}, nil
}

func indexArgs(args []semreg.TypedField) map[semreg.DefinitionID]semreg.TypedField {
	out := make(map[semreg.DefinitionID]semreg.TypedField, len(args))
	for _, arg := range args {
		out[arg.ID] = arg
	}
	return out
}

func declaredParameter(op semreg.OperationMetadata, id semreg.DefinitionID) bool {
	for _, parameter := range op.Parameters {
		if parameter.ID == id {
			return true
		}
	}
	return false
}

func releaseShaped(op semreg.OperationMetadata, args map[semreg.DefinitionID]semreg.TypedField) bool {
	if len(args) != 1 {
		return false
	}
	for id, field := range args {
		if id == op.Argument.ID || declaredParameter(op, id) {
			return false
		}
		return field.Value.Kind == semreg.ValueBoolean
	}
	return false
}

func negativeQuantity(id semreg.DefinitionID, value semreg.Value) bool {
	if id != "pv.limit.active_power" && id != "evse.limit.allocated_current" {
		return false
	}
	if value.Kind != semreg.ValueQuantity || value.Quantity == nil {
		return false
	}
	return strings.HasPrefix(value.Quantity.Number.Coefficient, "-")
}

func argumentShape(args []semreg.TypedField) *routeRefusal {
	if args == nil {
		return failed(ReasonInvalidArgument)
	}
	seen := map[semreg.DefinitionID]struct{}{}
	for i, arg := range args {
		if _, dup := seen[arg.ID]; dup {
			return failed(ReasonDuplicateArgument)
		}
		seen[arg.ID] = struct{}{}
		if i > 0 && args[i-1].ID > arg.ID {
			return failed(ReasonNoncanonicalArguments)
		}
	}
	return nil
}
