package thermal

import (
	"math/big"
	"reflect"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// New returns the immutable thermal/HVAC v1 validator.
func New() semreg.PackValidator              { return validator{} }
func NewPackValidator() semreg.PackValidator { return New() }

type validator struct{}

var _ operation.OperationPackValidator = validator{}

func (validator) Pack() semreg.PackRef                { return pack }
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
		return failure(semreg.DefinitionOwnerMissing, "thermal pack")
	}
	spec, ok := findField(key.FactID)
	if !ok {
		return failure(semreg.DefinitionOwnerMissing, "thermal fact")
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
		return failure(semreg.DefinitionOwnerMissing, "thermal service pack")
	}
	if _, ok := services[service.Definition.ID]; !ok {
		return failure(semreg.DefinitionOwnerMissing, "thermal service")
	}
	return nil
}
func (validator) ValidateCapability(capability semreg.CapabilityInstance) error {
	if err := capability.Validate(); err != nil {
		return err
	}
	if capability.Definition.Pack != pack || capability.Definition.Version != packVersion {
		return failure(semreg.DefinitionOwnerMissing, "thermal capability pack")
	}
	spec, ok := capabilities[capability.Definition.ID]
	if !ok {
		return failure(semreg.DefinitionOwnerMissing, "thermal capability")
	}
	if capability.Qualification != semreg.QualificationQualified {
		return failure(semreg.CapabilityNotQualified, "qualified current binding")
	}
	if capability.Availability != semreg.AvailabilityAvailable {
		return failure(semreg.CapabilityUnavailable, "current binding")
	}
	return validateConstraintIDs(capability.Constraints, spec.constraints)
}
func (validator) ValidateField(ref semreg.DefinitionRef, field semreg.TypedField) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if err := field.Validate(); err != nil {
		return err
	}
	if ref.Pack != pack || ref.Version != packVersion || ref.ID != field.ID {
		return failure(semreg.DefinitionOwnerMissing, "thermal field")
	}
	spec, ok := findField(ref.ID)
	if !ok {
		return failure(semreg.DefinitionOwnerMissing, "thermal field")
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
	spec := capabilities[capability.Definition.ID]
	if len(fields) != len(spec.constraints) {
		return failure(semreg.InvalidValue, "thermal capability constraints")
	}
	for i, field := range fields {
		if field.ID != spec.constraints[i] {
			return failure(semreg.InvalidValue, "thermal capability constraint")
		}
		if err := v.ValidateField(definition(field.ID), field); err != nil {
			return err
		}
	}
	return nil
}
func (v validator) EvaluatePredicate(candidate semreg.FactCandidate, predicate semreg.PredicateOp, expected semreg.Value) (bool, error) {
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
	if predicate == semreg.PredicateEqual {
		return equalValue(*candidate.Value, expected), nil
	}
	if predicate == semreg.PredicateNotEqual {
		return !equalValue(*candidate.Value, expected), nil
	}
	if candidate.Value.Kind != semreg.ValueQuantity || expected.Kind != semreg.ValueQuantity {
		return false, failure(semreg.InvalidValue, "thermal predicate")
	}
	comparison := compareDecimal(candidate.Value.Quantity.Number, expected.Quantity.Number)
	switch predicate {
	case semreg.PredicateLess:
		return comparison < 0, nil
	case semreg.PredicateLessEqual:
		return comparison <= 0, nil
	case semreg.PredicateGreater:
		return comparison > 0, nil
	case semreg.PredicateGreaterEqual:
		return comparison >= 0, nil
	}
	return false, failure(semreg.InvalidEnum, "thermal predicate")
}
func (v validator) ValidateIntent(intent operation.Intent) error {
	if err := intent.Validate(); err != nil {
		return err
	}
	if intent.Kind.Pack != pack || intent.Kind.Version != packVersion {
		return failure(semreg.DefinitionOwnerMissing, "thermal operation pack")
	}
	spec, ok := operations[intent.Kind.ID]
	if !ok {
		return failure(semreg.DefinitionOwnerMissing, "thermal operation")
	}
	if intent.RequiredCapability.Pack != pack || intent.RequiredCapability.DefinitionID != spec.capability || intent.RequiredCapability.Versions.Minimum != packVersion || intent.RequiredCapability.Versions.MaximumExclusive != "2.0.0" {
		return failure(semreg.InvalidValue, "thermal required capability")
	}
	if len(intent.Arguments) != 1 || intent.Arguments[0].ID != spec.argument {
		return failure(semreg.InvalidValue, "thermal operation arguments")
	}
	if err := v.ValidateField(definition(spec.argument), intent.Arguments[0]); err != nil {
		return err
	}
	effect := intent.ExpectedEffect
	if effect.Rule != definition(spec.effect) || effect.Fact.PackID != pack.ID || effect.Fact.PackVersion != packVersion || effect.Fact.FactID != spec.argument || effect.Operator != semreg.PredicateEqual {
		return failure(semreg.InvalidValue, "thermal expected effect")
	}
	field, _ := findField(spec.argument)
	if err := validateDimensions(effect.Fact.Dimensions, field.dimension); err != nil {
		return err
	}
	if err := validateValue(field, effect.Expected); err != nil {
		return err
	}
	if !equalValue(intent.Arguments[0].Value, effect.Expected) {
		return failure(semreg.InvalidValue, "thermal expected effect value")
	}
	return nil
}
func (v validator) EvaluateReadback(intent operation.Intent, candidate semreg.FactCandidate) (operation.ReadbackRelation, error) {
	if err := v.ValidateIntent(intent); err != nil {
		return "", err
	}
	if candidate.Value == nil || !reflect.DeepEqual(candidate.Key, intent.ExpectedEffect.Fact) {
		return operation.ReadbackInconclusive, nil
	}
	if err := v.ValidateFact(candidate.Key, candidate.Value); err != nil {
		return operation.ReadbackInconclusive, nil
	}
	if equalValue(*candidate.Value, intent.ExpectedEffect.Expected) {
		return operation.ReadbackConfirms, nil
	}
	return operation.ReadbackContradicts, nil
}
func findField(id semreg.DefinitionID) (fieldSpec, bool) {
	for _, field := range fields {
		if field.id == id {
			return field, true
		}
	}
	return fieldSpec{}, false
}
func validateDimensions(values []semreg.Dimension, expected semreg.DefinitionID) error {
	if len(values) != 1 {
		return failure(semreg.InvalidValue, "thermal fact dimensions")
	}
	value := values[0]
	if value.ID != expected || value.Value.Kind != semreg.ValueText || value.Value.Text == nil {
		return failure(semreg.InvalidValue, "thermal fact dimension")
	}
	return nil
}
func validateConstraintIDs(values []semreg.TypedField, expected []semreg.DefinitionID) error {
	if values == nil {
		return failure(semreg.MissingMember, "constraints")
	}
	if len(values) != len(expected) {
		return failure(semreg.InvalidValue, "thermal capability constraints")
	}
	for i, value := range values {
		if value.ID != expected[i] {
			return failure(semreg.InvalidValue, "thermal capability constraint")
		}
	}
	return nil
}
func validateValue(spec fieldSpec, value semreg.Value) error {
	if value.Kind != spec.kind {
		return failure(semreg.InvalidValue, "thermal value kind")
	}
	switch spec.kind {
	case semreg.ValueQuantity:
		if value.Quantity == nil || value.Quantity.Unit != spec.unit {
			return failure(semreg.InvalidValue, "thermal quantity unit")
		}
		if compareDecimal(value.Quantity.Number, spec.minimum.decimal()) < 0 || compareDecimal(value.Quantity.Number, spec.maximum.decimal()) > 0 {
			return failure(semreg.BoundsExceeded, "thermal quantity bounds")
		}
	case semreg.ValueSymbol:
		if value.Symbol == nil || !value.Symbol.Known || value.Symbol.Namespace != spec.id || !contains(spec.symbols, string(value.Symbol.Namespace)+"."+value.Symbol.Token) {
			return failure(semreg.InvalidValue, "thermal symbol")
		}
	default:
		return failure(semreg.InvalidValue, "thermal value kind")
	}
	return nil
}
func compareDecimal(a, b semreg.Decimal) int { return decimalRat(a).Cmp(decimalRat(b)) }
func decimalRat(value semreg.Decimal) *big.Rat {
	ratio := new(big.Rat)
	ratio.SetString(value.Coefficient)
	if value.Exponent10 >= 0 {
		return ratio.Mul(ratio, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(value.Exponent10)), nil)))
	}
	return ratio.Quo(ratio, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-value.Exponent10)), nil)))
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
