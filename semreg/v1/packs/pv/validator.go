package pv

import (
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
	"math/big"
	"reflect"
	"strings"
)

func New() semreg.PackValidator              { return validator{} }
func NewPackValidator() semreg.PackValidator { return New() }

type validator struct{}

var _ operation.OperationPackValidator = validator{}

func (validator) Pack() semreg.PackRef                { return pack }
func (validator) Definitions() semreg.DefinitionIndex { return index() }
func (v validator) ValidateFact(k semreg.FactKey, value *semreg.Value) error {
	if err := k.Validate(); err != nil {
		return err
	}
	if value == nil {
		return fail(semreg.MissingMember, "fact value")
	}
	if err := value.Validate(); err != nil {
		return err
	}
	if k.PackID != pack.ID || k.PackVersion != pack.Version {
		return fail(semreg.DefinitionOwnerMissing, "pv pack")
	}
	s, ok := findField(k.FactID)
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "pv fact")
	}
	if err := dimensionsOK(k.Dimensions, s.dimension); err != nil {
		return err
	}
	return valueOK(s, *value)
}
func (v validator) ValidateService(s semreg.ServiceInstance) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.Definition.Pack != pack || s.Definition.Version != packVersion {
		return fail(semreg.DefinitionOwnerMissing, "pv service pack")
	}
	if _, ok := services[s.Definition.ID]; !ok {
		return fail(semreg.DefinitionOwnerMissing, "pv service")
	}
	return nil
}
func (v validator) ValidateCapability(c semreg.CapabilityInstance) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Definition.Pack != pack || c.Definition.Version != packVersion {
		return fail(semreg.DefinitionOwnerMissing, "pv capability pack")
	}
	s, ok := capabilities[c.Definition.ID]
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "pv capability")
	}
	if c.Qualification != semreg.QualificationQualified {
		return fail(semreg.CapabilityNotQualified, "qualified current binding")
	}
	if c.Availability != semreg.AvailabilityAvailable {
		return fail(semreg.CapabilityUnavailable, "current binding")
	}
	return v.constraintsOK(c.Constraints, s.constraints)
}
func (v validator) ValidateField(r semreg.DefinitionRef, f semreg.TypedField) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := f.Validate(); err != nil {
		return err
	}
	if r.Pack != pack || r.Version != packVersion || r.ID != f.ID {
		return fail(semreg.DefinitionOwnerMissing, "pv field")
	}
	s, ok := findField(r.ID)
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "pv field")
	}
	return valueOK(s, f.Value)
}
func (v validator) MatchConstraints(c semreg.CapabilityInstance, fs []semreg.TypedField) error {
	if err := v.ValidateCapability(c); err != nil {
		return err
	}
	if fs == nil {
		return fail(semreg.MissingMember, "constraints")
	}
	s := capabilities[c.Definition.ID]
	if len(fs) != len(s.constraints) {
		return fail(semreg.InvalidValue, "pv capability constraints")
	}
	for i, f := range fs {
		if f.ID != s.constraints[i] {
			return fail(semreg.InvalidValue, "pv capability constraint")
		}
		if err := v.ValidateField(definition(f.ID), f); err != nil {
			return err
		}
		if !equal(f.Value, c.Constraints[i].Value) {
			return fail(semreg.InvalidValue, "pv capability constraint value")
		}
	}
	return nil
}
func (v validator) EvaluatePredicate(c semreg.FactCandidate, p semreg.PredicateOp, e semreg.Value) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	if err := e.Validate(); err != nil {
		return false, err
	}
	if err := v.ValidateFact(c.Key, c.Value); err != nil {
		return false, err
	}
	if err := v.ValidateFact(c.Key, &e); err != nil {
		return false, err
	}
	if p == semreg.PredicateEqual {
		return equal(*c.Value, e), nil
	}
	if p == semreg.PredicateNotEqual {
		return !equal(*c.Value, e), nil
	}
	if c.Value.Kind != semreg.ValueQuantity || e.Kind != semreg.ValueQuantity {
		return false, fail(semreg.InvalidValue, "pv predicate")
	}
	n := compare(c.Value.Quantity.Number, e.Quantity.Number)
	switch p {
	case semreg.PredicateLess:
		return n < 0, nil
	case semreg.PredicateLessEqual:
		return n <= 0, nil
	case semreg.PredicateGreater:
		return n > 0, nil
	case semreg.PredicateGreaterEqual:
		return n >= 0, nil
	}
	return false, fail(semreg.InvalidEnum, "pv predicate")
}
func (v validator) ValidateIntent(i operation.Intent) error {
	if err := i.Validate(); err != nil {
		return err
	}
	op, capability, field, effect, ok := pvOperationShape(i.Kind.ID)
	if !ok || i.Kind != definition(op) {
		return fail(semreg.DefinitionOwnerMissing, "pv operation")
	}
	if i.RequiredCapability.Pack != pack || i.RequiredCapability.DefinitionID != capability || i.RequiredCapability.Versions.Minimum != packVersion || i.RequiredCapability.Versions.MaximumExclusive != "2.0.0" {
		return fail(semreg.InvalidValue, "pv required capability")
	}
	if len(i.Arguments) != 1 || i.Arguments[0].ID != field {
		return fail(semreg.InvalidValue, "pv operation arguments")
	}
	if err := v.ValidateField(definition(field), i.Arguments[0]); err != nil {
		return err
	}
	e := i.ExpectedEffect
	if e.Rule != definition(effect) || e.Fact.PackID != pack.ID || e.Fact.PackVersion != pack.Version || e.Fact.FactID != field || e.Operator != semreg.PredicateEqual {
		return fail(semreg.InvalidValue, "pv expected effect")
	}
	s, _ := findField(field)
	if err := dimensionsOK(e.Fact.Dimensions, s.dimension); err != nil {
		return err
	}
	if err := valueOK(s, e.Expected); err != nil {
		return err
	}
	if !equal(i.Arguments[0].Value, e.Expected) {
		return fail(semreg.InvalidValue, "pv expected effect value")
	}
	return nil
}
func pvOperationShape(id semreg.DefinitionID) (semreg.DefinitionID, semreg.DefinitionID, semreg.DefinitionID, semreg.DefinitionID, bool) {
	switch id {
	case "pv.operation.set_active_power_limit":
		return id, "pv.capability.set_active_power_limit", "pv.limit.active_power", "pv.effect.set_active_power_limit", true
	case "pv.operation.set_export_limit":
		return id, "pv.capability.set_export_limit", "pv.limit.export_power", "pv.effect.set_export_limit", true
	}
	return "", "", "", "", false
}
func (v validator) EvaluateReadback(i operation.Intent, c semreg.FactCandidate) (operation.ReadbackRelation, error) {
	if err := v.ValidateIntent(i); err != nil {
		return "", err
	}
	if c.Value == nil || !reflect.DeepEqual(c.Key, i.ExpectedEffect.Fact) {
		return operation.ReadbackInconclusive, nil
	}
	if err := v.ValidateFact(c.Key, c.Value); err != nil {
		return operation.ReadbackInconclusive, nil
	}
	if equal(*c.Value, i.ExpectedEffect.Expected) {
		return operation.ReadbackConfirms, nil
	}
	return operation.ReadbackContradicts, nil
}
func findField(id semreg.DefinitionID) (fieldSpec, bool) {
	for _, f := range fields {
		if f.id == id {
			return f, true
		}
	}
	return fieldSpec{}, false
}
func dimensionsOK(ds []semreg.Dimension, want semreg.DefinitionID) error {
	if len(ds) != 1 {
		return fail(semreg.InvalidValue, "pv dimensions")
	}
	d := ds[0]
	if d.ID != want || d.Value.Kind != semreg.ValueText || d.Value.Text == nil {
		return fail(semreg.InvalidValue, "pv dimension")
	}
	return nil
}
func (v validator) constraintsOK(values []semreg.TypedField, want []semreg.DefinitionID) error {
	if values == nil {
		return fail(semreg.MissingMember, "constraints")
	}
	if len(values) != len(want) {
		return fail(semreg.InvalidValue, "pv capability constraints")
	}
	for i, value := range values {
		if value.ID != want[i] {
			return fail(semreg.InvalidValue, "pv capability constraint")
		}
		if err := v.ValidateField(definition(value.ID), value); err != nil {
			return err
		}
	}
	return nil
}
func valueOK(s fieldSpec, v semreg.Value) error {
	if v.Kind != s.kind {
		return fail(semreg.InvalidValue, "pv value kind")
	}
	if s.kind == semreg.ValueQuantity {
		if v.Quantity == nil || v.Quantity.Unit != s.unit {
			return fail(semreg.InvalidValue, "pv quantity unit")
		}
		if compare(v.Quantity.Number, s.minimum.decimal()) < 0 || compare(v.Quantity.Number, s.maximum.decimal()) > 0 {
			return fail(semreg.BoundsExceeded, "pv quantity bounds")
		}
		return nil
	}
	if s.kind == semreg.ValueSymbol {
		if v.Symbol == nil || !v.Symbol.Known || v.Symbol.Namespace != s.id || !contains(s.symbols, string(v.Symbol.Namespace)+"."+v.Symbol.Token) {
			return fail(semreg.InvalidValue, "pv symbol")
		}
		return nil
	}
	return fail(semreg.InvalidValue, "pv value kind")
}
func compare(a, b semreg.Decimal) int { return rat(a).Cmp(rat(b)) }
func rat(v semreg.Decimal) *big.Rat {
	r := new(big.Rat)
	r.SetString(v.Coefficient)
	if v.Exponent10 >= 0 {
		return r.Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(v.Exponent10)), nil)))
	}
	return r.Quo(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-v.Exponent10)), nil)))
}
func equal(a, b semreg.Value) bool {
	if a.Kind == semreg.ValueQuantity && b.Kind == semreg.ValueQuantity && a.Quantity != nil && b.Quantity != nil {
		return a.Quantity.Unit == b.Quantity.Unit && compare(a.Quantity.Number, b.Quantity.Number) == 0
	}
	return reflect.DeepEqual(a, b)
}
func contains(vs []string, w string) bool {
	for _, v := range vs {
		if strings.Compare(v, w) == 0 {
			return true
		}
	}
	return false
}
func fail(id semreg.ErrorID, d string) error { return &semreg.Error{ID: id, Detail: d} }
