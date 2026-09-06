package infrastructure

import (
	"math/big"
	"reflect"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// New returns the immutable validator for helianthus.pack.infrastructure v1.
func New() semreg.PackValidator { return validator{} }

// NewPackValidator is an explicit constructor name for registry composition.
func NewPackValidator() semreg.PackValidator { return New() }

type validator struct{}

func (validator) Pack() semreg.PackRef { return pack }

func (validator) Definitions() semreg.DefinitionIndex { return index() }

func (validator) ValidateFact(key semreg.FactKey, value *semreg.Value) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if value == nil {
		return failure(semreg.MissingMember, "fact value")
	}
	if err := value.Validate(); err != nil {
		return err
	}
	if key.PackID != pack.ID || key.PackVersion != pack.Version {
		return failure(semreg.DefinitionOwnerMissing, "infrastructure pack")
	}
	spec, ok := findField(key.FactID)
	if !ok {
		return failure(semreg.DefinitionOwnerMissing, "infrastructure fact")
	}
	if err := validateDimensions(key.Dimensions, spec.dimension); err != nil {
		return err
	}
	return validateValue(spec, *value)
}

func (validator) ValidateService(service semreg.ServiceInstance) error {
	if err := service.Validate(); err != nil {
		return err
	}
	if service.Definition.Pack != pack || service.Definition.Version != packVersion {
		return failure(semreg.DefinitionOwnerMissing, "infrastructure service pack")
	}
	if _, ok := services[service.Definition.ID]; !ok {
		return failure(semreg.DefinitionOwnerMissing, "infrastructure service")
	}
	return nil
}

func (validator) ValidateCapability(capability semreg.CapabilityInstance) error {
	if err := capability.Validate(); err != nil {
		return err
	}
	if capability.Definition.Pack != pack || capability.Definition.Version != packVersion {
		return failure(semreg.DefinitionOwnerMissing, "infrastructure capability pack")
	}
	if _, ok := capabilities[capability.Definition.ID]; !ok {
		return failure(semreg.DefinitionOwnerMissing, "infrastructure capability")
	}
	if capability.Qualification != semreg.QualificationQualified {
		return failure(semreg.CapabilityNotQualified, "qualified current binding")
	}
	if capability.Availability != semreg.AvailabilityAvailable {
		return failure(semreg.CapabilityUnavailable, "current binding")
	}
	if len(capability.Constraints) != 0 {
		return failure(semreg.InvalidValue, "read capability constraints")
	}
	return nil
}

func (validator) ValidateField(ref semreg.DefinitionRef, field semreg.TypedField) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if err := field.Validate(); err != nil {
		return err
	}
	if ref.Pack != pack || ref.Version != packVersion || ref.ID != field.ID {
		return failure(semreg.DefinitionOwnerMissing, "infrastructure field")
	}
	spec, ok := findField(ref.ID)
	if !ok {
		return failure(semreg.DefinitionOwnerMissing, "infrastructure field")
	}
	return validateValue(spec, field.Value)
}

func (v validator) MatchConstraints(capability semreg.CapabilityInstance, fields []semreg.TypedField) error {
	if err := v.ValidateCapability(capability); err != nil {
		return err
	}
	if fields == nil {
		return failure(semreg.MissingMember, "constraints")
	}
	if len(fields) != 0 {
		return failure(semreg.InvalidValue, "read capability constraints")
	}
	return nil
}

func (v validator) EvaluatePredicate(candidate semreg.FactCandidate, operation semreg.PredicateOp, expected semreg.Value) (bool, error) {
	if err := candidate.Validate(); err != nil {
		return false, err
	}
	if err := expected.Validate(); err != nil {
		return false, err
	}
	if err := v.ValidateFact(candidate.Key, candidate.Value); err != nil {
		return false, err
	}
	if err := v.ValidateFact(candidate.Key, &expected); err != nil {
		return false, err
	}
	if operation == semreg.PredicateEqual {
		return equalValue(*candidate.Value, expected), nil
	}
	if operation == semreg.PredicateNotEqual {
		return !equalValue(*candidate.Value, expected), nil
	}
	if candidate.Value.Kind != semreg.ValueQuantity || expected.Kind != semreg.ValueQuantity {
		return false, failure(semreg.InvalidValue, "infrastructure predicate")
	}
	compare := compareDecimal(candidate.Value.Quantity.Number, expected.Quantity.Number)
	switch operation {
	case semreg.PredicateLess:
		return compare < 0, nil
	case semreg.PredicateLessEqual:
		return compare <= 0, nil
	case semreg.PredicateGreater:
		return compare > 0, nil
	case semreg.PredicateGreaterEqual:
		return compare >= 0, nil
	default:
		return false, failure(semreg.InvalidEnum, "infrastructure predicate")
	}
}

func findField(id semreg.DefinitionID) (fieldSpec, bool) {
	for _, field := range fields {
		if field.id == id {
			return field, true
		}
	}
	return fieldSpec{}, false
}

func validateDimensions(dimensionsValue []semreg.Dimension, expected semreg.DefinitionID) error {
	if len(dimensionsValue) != 1 {
		return failure(semreg.InvalidValue, "infrastructure fact dimensions")
	}
	dimension := dimensionsValue[0]
	if dimension.ID != expected || dimensions[dimension.ID] != dimension.Value.Kind || dimension.Value.Text == nil {
		return failure(semreg.InvalidValue, "infrastructure fact dimension")
	}
	return nil
}

func validateValue(spec fieldSpec, value semreg.Value) error {
	if value.Kind != spec.kind {
		return failure(semreg.InvalidValue, "infrastructure value kind")
	}
	switch spec.kind {
	case semreg.ValueQuantity:
		if value.Quantity == nil || value.Quantity.Unit != spec.unit {
			return failure(semreg.InvalidValue, "infrastructure quantity unit")
		}
		if compareDecimal(value.Quantity.Number, spec.minimum.decimal()) < 0 || compareDecimal(value.Quantity.Number, spec.maximum.decimal()) > 0 {
			return failure(semreg.BoundsExceeded, "infrastructure quantity bounds")
		}
	case semreg.ValueSymbol:
		if value.Symbol == nil || !value.Symbol.Known || value.Symbol.Namespace != spec.id || !contains(spec.symbols, string(value.Symbol.Namespace)+"."+value.Symbol.Token) {
			return failure(semreg.InvalidValue, "infrastructure status symbol")
		}
	default:
		return failure(semreg.InvalidValue, "infrastructure value kind")
	}
	return nil
}

func compareDecimal(a, b semreg.Decimal) int {
	return decimalRat(a).Cmp(decimalRat(b))
}

func decimalRat(value semreg.Decimal) *big.Rat {
	ratio := new(big.Rat)
	ratio.SetString(value.Coefficient)
	if value.Exponent10 >= 0 {
		ratio.Mul(ratio, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(value.Exponent10)), nil)))
		return ratio
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-value.Exponent10)), nil)
	return ratio.Quo(ratio, new(big.Rat).SetInt(denominator))
}

func equalValue(a, b semreg.Value) bool {
	if a.Kind == semreg.ValueQuantity && b.Kind == semreg.ValueQuantity && a.Quantity != nil && b.Quantity != nil {
		return a.Quantity.Unit == b.Quantity.Unit && compareDecimal(a.Quantity.Number, b.Quantity.Number) == 0
	}
	return reflect.DeepEqual(a, b)
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if strings.Compare(value, expected) == 0 {
			return true
		}
	}
	return false
}

func failure(id semreg.ErrorID, detail string) error { return &semreg.Error{ID: id, Detail: detail} }
