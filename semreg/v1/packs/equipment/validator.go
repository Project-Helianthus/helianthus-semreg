package equipment

import (
	"math/big"
	"reflect"
	"sort"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

func New() semreg.PackValidator { return validator{} }

func NewPackValidator() semreg.PackValidator { return New() }

type validator struct{}

var _ operation.OperationPackValidator = validator{}
var _ semreg.PackMetadataProvider = validator{}
var _ semreg.GraphFactValidator = validator{}

func (validator) Pack() semreg.PackRef                { return pack }
func (validator) Definitions() semreg.DefinitionIndex { return index() }
func (validator) Metadata() semreg.PackMetadata       { return Metadata() }

func (validator) ValidateFact(key semreg.FactKey, value *semreg.Value) error {
	return validateFact(key, value, func(values []semreg.Dimension, spec fieldSpec) error {
		return dimensionsOK(values, spec)
	})
}

func (validator) ValidateGraphFact(key semreg.FactKey, value *semreg.Value, subject semreg.GraphFactSubject) error {
	return validateFact(key, value, func(values []semreg.Dimension, spec fieldSpec) error {
		return graphDimensionsOK(values, spec, subject)
	})
}

func validateFact(key semreg.FactKey, value *semreg.Value, dimensionsCheck func([]semreg.Dimension, fieldSpec) error) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if value == nil {
		return fail(semreg.MissingMember, "fact value")
	}
	if err := value.Validate(); err != nil {
		return err
	}
	if key.PackID != pack.ID || key.PackVersion != pack.Version {
		return fail(semreg.DefinitionOwnerMissing, "equipment pack")
	}
	spec, ok := findField(key.FactID)
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "equipment fact")
	}
	if err := dimensionsCheck(key.Dimensions, spec); err != nil {
		return err
	}
	return valueOK(spec, *value)
}

func (validator) ValidateService(service semreg.ServiceInstance) error {
	if err := service.Validate(); err != nil {
		return err
	}
	if service.Definition.Pack != pack || service.Definition.Version != packVersion {
		return fail(semreg.DefinitionOwnerMissing, "equipment service pack")
	}
	if _, ok := services[service.Definition.ID]; !ok {
		return fail(semreg.DefinitionOwnerMissing, "equipment service")
	}
	return nil
}

func (v validator) ValidateCapability(capability semreg.CapabilityInstance) error {
	if err := capability.Validate(); err != nil {
		return err
	}
	if capability.Definition.Pack != pack || capability.Definition.Version != packVersion {
		return fail(semreg.DefinitionOwnerMissing, "equipment capability pack")
	}
	spec, ok := capabilities[capability.Definition.ID]
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "equipment capability")
	}
	if capability.Qualification != semreg.QualificationQualified {
		return fail(semreg.CapabilityNotQualified, "qualified current binding")
	}
	if capability.Availability != semreg.AvailabilityAvailable {
		return fail(semreg.CapabilityUnavailable, "current binding")
	}
	return v.constraintsOK(capability.Constraints, spec.constraints)
}

func (validator) ValidateField(ref semreg.DefinitionRef, field semreg.TypedField) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if err := field.Validate(); err != nil {
		return err
	}
	if ref.Pack != pack || ref.Version != packVersion || ref.ID != field.ID {
		return fail(semreg.DefinitionOwnerMissing, "equipment field")
	}
	spec, ok := findField(ref.ID)
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "equipment field")
	}
	return valueOK(spec, field.Value)
}

func (v validator) MatchConstraints(capability semreg.CapabilityInstance, fields []semreg.TypedField) error {
	if err := v.ValidateCapability(capability); err != nil {
		return err
	}
	if fields == nil {
		return fail(semreg.MissingMember, "constraints")
	}
	spec := capabilities[capability.Definition.ID]
	if len(fields) != len(spec.constraints) {
		return fail(semreg.InvalidValue, "equipment capability constraints")
	}
	for i, field := range fields {
		if field.ID != spec.constraints[i] {
			return fail(semreg.InvalidValue, "equipment capability constraint")
		}
		if err := v.ValidateField(definition(field.ID), field); err != nil {
			return err
		}
		if !equalValue(field.Value, capability.Constraints[i].Value) {
			return fail(semreg.InvalidValue, "equipment capability constraint value")
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
		return false, fail(semreg.InvalidValue, "equipment predicate")
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
	return false, fail(semreg.InvalidEnum, "equipment predicate")
}

func (v validator) ValidateIntent(intent operation.Intent) error {
	if err := intent.Validate(); err != nil {
		return err
	}
	if intent.Kind.Pack != pack || intent.Kind.Version != packVersion {
		return fail(semreg.DefinitionOwnerMissing, "equipment operation pack")
	}
	spec, ok := operations[intent.Kind.ID]
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "equipment operation")
	}
	if intent.RequiredCapability.Pack != pack || intent.RequiredCapability.DefinitionID != spec.capability || intent.RequiredCapability.Versions.Minimum != packVersion || intent.RequiredCapability.Versions.MaximumExclusive != "2.0.0" {
		return fail(semreg.InvalidValue, "equipment required capability")
	}
	if err := argumentsOK(v, intent.Kind.ID, spec.argument, intent.Arguments); err != nil {
		return err
	}
	argument, ok := argumentField(spec.argument, intent.Arguments)
	if !ok {
		return fail(semreg.InvalidValue, "equipment operation arguments")
	}
	effect := intent.ExpectedEffect
	if effect.Rule != definition(spec.effect) || effect.Fact.PackID != pack.ID || effect.Fact.PackVersion != packVersion || effect.Fact.FactID != spec.argument || effect.Operator != semreg.PredicateEqual {
		return fail(semreg.InvalidValue, "equipment expected effect")
	}
	field, _ := findField(spec.argument)
	if err := dimensionsOK(effect.Fact.Dimensions, field); err != nil {
		return err
	}
	if err := valueOK(field, effect.Expected); err != nil {
		return err
	}
	if !equalValue(argument.Value, effect.Expected) {
		return fail(semreg.InvalidValue, "equipment expected effect value")
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

func argumentsOK(v validator, operationID, argument semreg.DefinitionID, arguments []semreg.TypedField) error {
	seen := map[semreg.DefinitionID]int{}
	found := false
	for _, field := range arguments {
		seen[field.ID]++
		if seen[field.ID] > 1 {
			return fail(semreg.InvalidValue, "equipment repeated field")
		}
		if field.ID == argument {
			found = true
			if err := v.ValidateField(definition(field.ID), field); err != nil {
				return err
			}
			continue
		}
		if !containsID(operationParameters[operationID], field.ID) {
			return fail(semreg.InvalidValue, "equipment undeclared field")
		}
		if err := v.ValidateField(definition(field.ID), field); err != nil {
			return err
		}
	}
	if !found {
		return fail(semreg.InvalidValue, "equipment missing argument")
	}
	return nil
}

func argumentField(argument semreg.DefinitionID, arguments []semreg.TypedField) (semreg.TypedField, bool) {
	for _, field := range arguments {
		if field.ID == argument {
			return field, true
		}
	}
	return semreg.TypedField{}, false
}

func findField(id semreg.DefinitionID) (fieldSpec, bool) {
	for _, field := range fields {
		if field.id == id {
			return field, true
		}
	}
	return fieldSpec{}, false
}

func dimensionsOK(values []semreg.Dimension, spec fieldSpec) error {
	want := append([]semreg.DefinitionID{}, fieldQualifiers[spec.id]...)
	want = append(want, spec.dimension)
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if len(values) != len(want) {
		return fail(semreg.InvalidValue, "equipment dimensions")
	}
	for i, value := range values {
		if value.ID != want[i] || !dimensionShapeOK(value) {
			return fail(semreg.InvalidValue, "equipment dimension")
		}
	}
	return nil
}

func (v validator) constraintsOK(values []semreg.TypedField, expected []semreg.DefinitionID) error {
	if values == nil {
		return fail(semreg.MissingMember, "constraints")
	}
	if len(values) != len(expected) {
		return fail(semreg.InvalidValue, "equipment capability constraints")
	}
	for i, value := range values {
		if value.ID != expected[i] {
			return fail(semreg.InvalidValue, "equipment capability constraint")
		}
		if err := v.ValidateField(definition(value.ID), value); err != nil {
			return err
		}
	}
	return nil
}

func valueOK(spec fieldSpec, value semreg.Value) error {
	if value.Kind != spec.kind {
		return fail(semreg.InvalidValue, "equipment value kind")
	}
	switch spec.kind {
	case semreg.ValueQuantity:
		if value.Quantity == nil || value.Quantity.Unit != spec.unit {
			return fail(semreg.InvalidValue, "equipment quantity unit")
		}
		if spec.minimum != nil && compareDecimal(value.Quantity.Number, spec.minimum.decimal()) < 0 {
			return fail(semreg.BoundsExceeded, "equipment quantity bounds")
		}
		if spec.maximum != nil && compareDecimal(value.Quantity.Number, spec.maximum.decimal()) > 0 {
			return fail(semreg.BoundsExceeded, "equipment quantity bounds")
		}
	case semreg.ValueBoolean:
		if value.Boolean == nil {
			return fail(semreg.InvalidValue, "equipment boolean")
		}
	case semreg.ValueText:
		if value.Text == nil {
			return fail(semreg.InvalidValue, "equipment text")
		}
	case semreg.ValueTime:
		if value.Time == nil {
			return fail(semreg.InvalidValue, "equipment time")
		}
	case semreg.ValueSymbol:
		if !symbolOK(spec, value.Symbol) {
			return fail(semreg.InvalidValue, "equipment symbol")
		}
	case semreg.ValueSymbols:
		if len(value.Symbols) == 0 {
			return fail(semreg.InvalidValue, "equipment symbols")
		}
		for i := range value.Symbols {
			if !symbolOK(spec, &value.Symbols[i]) {
				return fail(semreg.InvalidValue, "equipment symbols")
			}
		}
	default:
		return fail(semreg.InvalidValue, "equipment value kind")
	}
	return nil
}

func symbolOK(spec fieldSpec, symbol *semreg.Symbol) bool {
	if symbol == nil || !symbol.Known || symbol.Namespace != spec.id {
		return false
	}
	return contains(spec.symbols, string(symbol.Namespace)+"."+symbol.Token) || contains(spec.symbols, symbol.Token)
}

func dimensionShapeOK(value semreg.Dimension) bool {
	if kind, ok := qualifierKinds[value.ID]; ok {
		if kind == semreg.ValueSymbol {
			symbols := qualifierSymbols[value.ID]
			return value.Value.Kind == semreg.ValueSymbol && value.Value.Symbol != nil && value.Value.Symbol.Known && value.Value.Symbol.Namespace == value.ID && (contains(symbols, value.Value.Symbol.Token) || contains(symbols, string(value.ID)+"."+value.Value.Symbol.Token))
		}
		if kind == semreg.ValueQuantity {
			return value.Value.Kind == semreg.ValueQuantity && value.Value.Quantity != nil && value.Value.Quantity.Unit == "unit.one"
		}
		return value.Value.Kind == semreg.ValueText && value.Value.Text != nil
	}
	return dimensions[value.ID] == value.Value.Kind && value.Value.Text != nil
}

func graphDimensionsOK(values []semreg.Dimension, spec fieldSpec, subject semreg.GraphFactSubject) error {
	if err := dimensionsOK(values, spec); err != nil {
		return err
	}
	if !subject.Resolved || subject.Dimension != spec.dimension || !sameIDs(subject.Qualifiers, fieldQualifiers[spec.id]) {
		return fail(semreg.InvalidValue, "equipment fact subject")
	}
	if subject.Kind == "" && len(subject.Classes) == 0 {
		return fail(semreg.InvalidValue, "equipment fact subject class")
	}
	for _, value := range values {
		if value.ID == spec.dimension && semreg.ResourceID(*value.Value.Text).Validate() != nil {
			return fail(semreg.InvalidValue, "equipment fact resource dimension")
		}
	}
	return nil
}

func sameIDs(got, want []semreg.DefinitionID) bool {
	if len(got) != len(want) {
		return false
	}
	left := append([]semreg.DefinitionID{}, got...)
	right := append([]semreg.DefinitionID{}, want...)
	sort.Slice(left, func(i, j int) bool { return left[i] < left[j] })
	sort.Slice(right, func(i, j int) bool { return right[i] < right[j] })
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
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
func containsID(values []semreg.DefinitionID, expected semreg.DefinitionID) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
func fail(id semreg.ErrorID, detail string) error {
	return &semreg.Error{ID: id, Detail: detail}
}
