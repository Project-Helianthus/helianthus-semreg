package storage

import (
	"math/big"
	"reflect"
	"strings"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	operation "github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
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
		return fail(semreg.DefinitionOwnerMissing, "storage pack")
	}
	s, ok := findField(k.FactID)
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "storage fact")
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
		return fail(semreg.DefinitionOwnerMissing, "storage service pack")
	}
	if _, ok := services[s.Definition.ID]; !ok {
		return fail(semreg.DefinitionOwnerMissing, "storage service")
	}
	return nil
}
func (v validator) ValidateCapability(c semreg.CapabilityInstance) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Definition.Pack != pack || c.Definition.Version != packVersion {
		return fail(semreg.DefinitionOwnerMissing, "storage capability pack")
	}
	s, ok := capabilities[c.Definition.ID]
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "storage capability")
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
		return fail(semreg.DefinitionOwnerMissing, "storage field")
	}
	s, ok := findField(r.ID)
	if !ok {
		return fail(semreg.DefinitionOwnerMissing, "storage field")
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
		return fail(semreg.InvalidValue, "storage capability constraints")
	}
	for i, f := range fs {
		if f.ID != s.constraints[i] {
			return fail(semreg.InvalidValue, "storage capability constraint")
		}
		if err := v.ValidateField(definition(f.ID), f); err != nil {
			return err
		}
		if !equal(f.Value, c.Constraints[i].Value) {
			return fail(semreg.InvalidValue, "storage capability constraint value")
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
		return false, fail(semreg.InvalidValue, "storage predicate")
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
	return false, fail(semreg.InvalidEnum, "storage predicate")
}
func (v validator) ValidateIntent(i operation.Intent) error {
	if err := i.Validate(); err != nil {
		return err
	}
	operationID, capability, field, effect, ok := operationShape(i.Kind.ID)
	if !ok || i.Kind != definition(operationID) {
		return fail(semreg.DefinitionOwnerMissing, "storage operation")
	}
	if i.RequiredCapability.Pack != pack || i.RequiredCapability.DefinitionID != capability || i.RequiredCapability.Versions.Minimum != packVersion || i.RequiredCapability.Versions.MaximumExclusive != "2.0.0" {
		return fail(semreg.InvalidValue, "storage required capability")
	}
	if len(i.Arguments) != 1 || i.Arguments[0].ID != field {
		return fail(semreg.InvalidValue, "storage operation arguments")
	}
	if err := v.ValidateField(definition(field), i.Arguments[0]); err != nil {
		return err
	}
	e := i.ExpectedEffect
	if e.Rule != definition(effect) || e.Fact.PackID != pack.ID || e.Fact.PackVersion != packVersion || e.Fact.FactID != field || e.Operator != semreg.PredicateEqual {
		return fail(semreg.InvalidValue, "storage expected effect")
	}
	s, _ := findField(field)
	if err := dimensionsOK(e.Fact.Dimensions, s.dimension); err != nil {
		return err
	}
	if err := valueOK(s, e.Expected); err != nil {
		return err
	}
	if !equal(i.Arguments[0].Value, e.Expected) {
		return fail(semreg.InvalidValue, "storage expected effect value")
	}
	canonical := 0
	for _, p := range i.Preconditions {
		if p.Fact.FactID == "storage.status.interlock" {
			canonical++
			if !canonicalInterlock(p, e.Fact) {
				return fail(semreg.InvalidValue, "storage interlock predicate")
			}
		}
	}
	if canonical != 1 {
		return fail(semreg.InvalidValue, "storage canonical interlock predicate")
	}
	return nil
}
func operationShape(id semreg.DefinitionID) (semreg.DefinitionID, semreg.DefinitionID, semreg.DefinitionID, semreg.DefinitionID, bool) {
	switch id {
	case "storage.operation.set_charge_limit":
		return id, "storage.capability.set_charge_limit", "storage.limit.charge_power", "storage.effect.set_charge_limit", true
	case "storage.operation.set_discharge_limit":
		return id, "storage.capability.set_discharge_limit", "storage.limit.discharge_power", "storage.effect.set_discharge_limit", true
	}
	return "", "", "", "", false
}
func canonicalInterlock(p operation.Precondition, effect semreg.FactKey) bool {
	return p.Fact.PackID == pack.ID && p.Fact.PackVersion == packVersion && p.Fact.FactID == "storage.status.interlock" && len(p.Fact.Dimensions) == 1 && p.Fact.Dimensions[0].ID == "storage.dimension.interface" && p.Fact.Dimensions[0].Value.Kind == semreg.ValueText && p.Fact.Dimensions[0].Value.Text != nil && reflect.DeepEqual(p.Fact.Dimensions, effect.Dimensions) && p.Operator == semreg.PredicateEqual && equal(p.Expected, clearValue())
}
func clearValue() semreg.Value {
	return semreg.Value{Kind: semreg.ValueSymbol, Symbol: &semreg.Symbol{Namespace: "storage.status.interlock", Token: "clear", Known: true}}
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
		return fail(semreg.InvalidValue, "storage dimensions")
	}
	d := ds[0]
	if d.ID != want || d.Value.Kind != semreg.ValueText || d.Value.Text == nil {
		return fail(semreg.InvalidValue, "storage dimension")
	}
	return nil
}
func (v validator) constraintsOK(values []semreg.TypedField, want []semreg.DefinitionID) error {
	if values == nil {
		return fail(semreg.MissingMember, "constraints")
	}
	if len(values) != len(want) {
		return fail(semreg.InvalidValue, "storage capability constraints")
	}
	for i, value := range values {
		if value.ID != want[i] {
			return fail(semreg.InvalidValue, "storage capability constraint")
		}
		if err := v.ValidateField(definition(value.ID), value); err != nil {
			return err
		}
	}
	return nil
}
func valueOK(s fieldSpec, v semreg.Value) error {
	if v.Kind != s.kind {
		return fail(semreg.InvalidValue, "storage value kind")
	}
	if s.kind == semreg.ValueQuantity {
		if v.Quantity == nil || v.Quantity.Unit != s.unit {
			return fail(semreg.InvalidValue, "storage quantity unit")
		}
		if s.minimum != nil && compare(v.Quantity.Number, s.minimum.decimal()) < 0 {
			return fail(semreg.BoundsExceeded, "storage quantity bounds")
		}
		if s.maximum != nil && compare(v.Quantity.Number, s.maximum.decimal()) > 0 {
			return fail(semreg.BoundsExceeded, "storage quantity bounds")
		}
		return nil
	}
	if s.kind == semreg.ValueSymbol {
		if v.Symbol == nil || !v.Symbol.Known || v.Symbol.Namespace != s.id || !contains(s.symbols, string(v.Symbol.Namespace)+"."+v.Symbol.Token) {
			return fail(semreg.InvalidValue, "storage symbol")
		}
		return nil
	}
	return fail(semreg.InvalidValue, "storage value kind")
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
