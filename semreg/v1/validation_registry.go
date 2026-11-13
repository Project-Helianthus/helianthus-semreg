package semreg

import (
	"reflect"
	"strings"
	"sync"
)

func (p PackRef) Validate() error { return bestError(p.ID.Validate(), p.Version.Validate()) }
func (d DefinitionRef) Validate() error {
	return bestError(d.Pack.Validate(), d.ID.Validate(), d.Version.Validate())
}
func compareDefinition(a, b DefinitionRef) int {
	if c := strings.Compare(string(a.ID), string(b.ID)); c != 0 {
		return c
	}
	if c, ok := compareSemver(a.Version, b.Version); ok {
		return c
	}
	return strings.Compare(string(a.Version), string(b.Version))
}
func (d DefinitionIndex) Validate() error {
	return bestError(d.validationErrors(DuplicateKey)...)
}

func (d DefinitionIndex) validationErrors(duplicateClass ErrorID) []error {
	var errs []error
	errs = append(errs, d.Pack.Validate())
	for _, group := range [][]DefinitionRef{d.Fields, d.Services, d.Capabilities, d.Operations, d.EffectRules} {
		if group == nil {
			errs = append(errs, errID(MissingMember, "definition collection"))
		}
		dup, ordered := duplicateAndOrder(group, compareDefinition)
		for _, ref := range group {
			errs = append(errs, ref.Validate())
			if ref.Pack != d.Pack {
				errs = append(errs, errID(DefinitionOwnerConflict, "definition index pack"))
			}
		}
		if dup {
			errs = append(errs, errID(duplicateClass, "definitions"))
		}
		if !ordered {
			errs = append(errs, errID(NoncanonicalOrder, "definitions"))
		}
	}
	return errs
}
func (f TypedField) Validate() error { return bestError(f.ID.Validate(), f.Value.Validate()) }
func (p PredicateOp) Validate() error {
	if p != PredicateEqual && p != PredicateNotEqual && p != PredicateLess && p != PredicateLessEqual && p != PredicateGreater && p != PredicateGreaterEqual && p != PredicateContains {
		return errID(InvalidEnum, "predicate")
	}
	return nil
}
func (s ServiceInstance) Validate() error {
	var enum error
	if s.Qualification != QualificationCandidate && s.Qualification != QualificationQualified && s.Qualification != QualificationUnsupported && s.Qualification != QualificationUnknown && s.Qualification != QualificationRejected {
		enum = errID(InvalidEnum, "service qualification")
	}
	if s.Availability != AvailabilityAvailable && s.Availability != AvailabilityDegraded && s.Availability != AvailabilityUnavailable && s.Availability != AvailabilityWithdrawn {
		enum = bestError(enum, errID(InvalidEnum, "service availability"))
	}
	var numeric error
	if !positive(s.DriverGeneration) || !positive(s.Revision) {
		numeric = errID(InvalidIdentifier, "service generation/revision")
	}
	return bestError(s.InstanceID.Validate(), s.AssetID.Validate(), s.Definition.Validate(), s.BindingID.Validate(), s.SourceEpochID.Validate(), numeric, enum)
}
func (c CapabilityInstance) Validate() error {
	var errs []error
	errs = append(errs, validateConstraintFields(c.Constraints))
	errs = append(errs, c.InstanceID.Validate(), c.AssetID.Validate(), c.ServiceInstance.Validate(), c.Definition.Validate(), c.BindingID.Validate(), c.SourceEpochID.Validate())
	if !positive(c.DriverGeneration) || !positive(c.Revision) {
		errs = append(errs, errID(InvalidIdentifier, "capability generation/revision"))
	}
	if c.Qualification != QualificationCandidate && c.Qualification != QualificationQualified && c.Qualification != QualificationUnsupported && c.Qualification != QualificationUnknown && c.Qualification != QualificationRejected {
		errs = append(errs, errID(InvalidEnum, "capability qualification"))
	}
	if c.Availability != AvailabilityAvailable && c.Availability != AvailabilityDegraded && c.Availability != AvailabilityUnavailable && c.Availability != AvailabilityWithdrawn {
		errs = append(errs, errID(InvalidEnum, "capability availability"))
	}
	if c.ActivationEvidence == nil {
		errs = append(errs, errID(MissingMember, "activation evidence"))
	} else if len(c.ActivationEvidence) == 0 {
		errs = append(errs, errID(CapabilityNotQualified, "activation evidence"))
	} else {
		errs = append(errs, validateEvidenceSet(c.ActivationEvidence, 1, 32))
	}
	return bestError(errs...)
}

func validateConstraintFields(fields []TypedField) error {
	var errs []error
	if fields == nil {
		errs = append(errs, errID(MissingMember, "constraints"))
	}
	dup, ordered := duplicateAndOrder(fields, func(a, b TypedField) int { return strings.Compare(string(a.ID), string(b.ID)) })
	for _, field := range fields {
		errs = append(errs, field.Validate())
	}
	if len(fields) > 64 {
		errs = append(errs, errID(BoundsExceeded, "constraints"))
	}
	if dup {
		errs = append(errs, errID(DuplicateKey, "constraints"))
	}
	if !ordered {
		errs = append(errs, errID(NoncanonicalOrder, "constraints"))
	}
	return bestError(errs...)
}

type DefinitionKind string

const (
	DefinitionField      DefinitionKind = "field"
	DefinitionService    DefinitionKind = "service"
	DefinitionCapability DefinitionKind = "capability"
	DefinitionOperation  DefinitionKind = "operation"
	DefinitionEffectRule DefinitionKind = "effect_rule"
)

type definitionKey struct {
	kind    DefinitionKind
	id      DefinitionID
	version SemanticVersion
}
type Registry struct {
	validators map[PackRef]PackValidator
	indexes    map[PackRef]DefinitionIndex
	owners     map[definitionKey]PackRef
}
type frozenValidator struct {
	hook   PackValidator
	index  DefinitionIndex
	owners map[definitionKey]PackRef
	// mutex is a pointer so registry value copies, including a fork-shared
	// registry, serialize calls into the same external hook instance.
	mutex *sync.Mutex
}

func (v frozenValidator) owns(kind DefinitionKind, ref DefinitionRef) error {
	if owner, ok := v.owners[definitionKey{kind, ref.ID, ref.Version}]; !ok || owner != ref.Pack || owner != v.Pack() {
		return errID(DefinitionOwnerMissing, "definition")
	}
	return nil
}

func (v frozenValidator) Pack() PackRef                { return v.index.Pack }
func (v frozenValidator) Definitions() DefinitionIndex { return cloneIndex(v.index) }
func (v frozenValidator) ValidateFact(k FactKey, value *Value) error {
	var valueErr error
	if value != nil {
		valueErr = value.Validate()
	}
	if err := bestError(k.Validate(), valueErr); err != nil {
		return err
	}
	if (PackRef{ID: k.PackID, Version: k.PackVersion}) != v.Pack() {
		return errID(DefinitionOwnerMissing, "fact pack")
	}
	if value != nil {
		detached := copyHookValue(*value)
		value = &detached
	}
	v.mutex.Lock()
	defer v.mutex.Unlock()
	return v.hook.ValidateFact(copyHookKey(k), value)
}
func (v frozenValidator) ValidateService(s ServiceInstance) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := v.owns(DefinitionService, s.Definition); err != nil {
		return err
	}
	// ServiceInstance contains only scalar/value fields.
	v.mutex.Lock()
	defer v.mutex.Unlock()
	return v.hook.ValidateService(s)
}
func (v frozenValidator) ValidateCapability(c CapabilityInstance) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := v.owns(DefinitionCapability, c.Definition); err != nil {
		return err
	}
	v.mutex.Lock()
	defer v.mutex.Unlock()
	return v.hook.ValidateCapability(copyHookCapability(c))
}
func (v frozenValidator) ValidateField(r DefinitionRef, f TypedField) error {
	if err := bestError(r.Validate(), f.Validate()); err != nil {
		return err
	}
	if r.ID != f.ID {
		return errID(InvalidValue, "field definition")
	}
	if err := v.owns(DefinitionField, r); err != nil {
		return err
	}
	v.mutex.Lock()
	defer v.mutex.Unlock()
	return v.hook.ValidateField(r, copyHookField(f))
}
func (v frozenValidator) MatchConstraints(c CapabilityInstance, f []TypedField) error {
	if err := bestError(c.Validate(), validateConstraintFields(f)); err != nil {
		return err
	}
	if err := v.owns(DefinitionCapability, c.Definition); err != nil {
		return err
	}
	v.mutex.Lock()
	defer v.mutex.Unlock()
	return v.hook.MatchConstraints(copyHookCapability(c), copyHookFields(f))
}
func (v frozenValidator) EvaluatePredicate(c FactCandidate, p PredicateOp, value Value) (bool, error) {
	if err := bestError(c.Validate(), p.Validate(), value.Validate()); err != nil {
		return false, err
	}
	if (PackRef{ID: c.Key.PackID, Version: c.Key.PackVersion}) != v.Pack() {
		return false, errID(DefinitionOwnerMissing, "fact pack")
	}
	v.mutex.Lock()
	defer v.mutex.Unlock()
	return v.hook.EvaluatePredicate(copyHookCandidate(c), p, copyHookValue(value))
}
func cloneRefs(in []DefinitionRef) []DefinitionRef {
	if in == nil {
		return nil
	}
	return append(make([]DefinitionRef, 0, len(in)), in...)
}
func cloneIndex(i DefinitionIndex) DefinitionIndex {
	i.Fields = cloneRefs(i.Fields)
	i.Services = cloneRefs(i.Services)
	i.Capabilities = cloneRefs(i.Capabilities)
	i.Operations = cloneRefs(i.Operations)
	i.EffectRules = cloneRefs(i.EffectRules)
	return i
}
func NewRegistry(validators ...PackValidator) (*Registry, error) {
	r := &Registry{validators: map[PackRef]PackValidator{}, indexes: map[PackRef]DefinitionIndex{}, owners: map[definitionKey]PackRef{}}
	var errs []error
	for _, hook := range validators {
		if hook == nil || reflect.ValueOf(hook).Kind() == reflect.Ptr && reflect.ValueOf(hook).IsNil() {
			errs = append(errs, errID(DefinitionOwnerConflict, "nil validator"))
			continue
		}
		pack := hook.Pack()
		index := cloneIndex(hook.Definitions())
		// Classify the complete diagnostic set in registration context before
		// ranking: a later ownership conflict cannot hide an earlier bad ID.
		errs = append(errs, pack.Validate())
		errs = append(errs, index.validationErrors(DefinitionOwnerConflict)...)
		if index.Pack != pack {
			errs = append(errs, errID(DefinitionOwnerConflict, "validator pack mismatch"))
		}
		if _, exists := r.validators[pack]; exists {
			errs = append(errs, errID(DefinitionOwnerConflict, "duplicate pack"))
			continue
		}
		frozen := frozenValidator{hook: hook, index: index, owners: r.owners, mutex: &sync.Mutex{}}
		r.validators[pack] = frozen
		r.indexes[pack] = index
		groups := []struct {
			kind DefinitionKind
			refs []DefinitionRef
		}{{DefinitionField, index.Fields}, {DefinitionService, index.Services}, {DefinitionCapability, index.Capabilities}, {DefinitionOperation, index.Operations}, {DefinitionEffectRule, index.EffectRules}}
		for _, group := range groups {
			for _, ref := range group.refs {
				if ref.Validate() != nil {
					continue
				}
				key := definitionKey{group.kind, ref.ID, ref.Version}
				if owner, exists := r.owners[key]; exists && owner != ref.Pack {
					errs = append(errs, errID(DefinitionOwnerConflict, "definition owner"))
				} else if exists {
					errs = append(errs, errID(DefinitionOwnerConflict, "duplicate definition"))
				} else {
					r.owners[key] = ref.Pack
				}
			}
		}
	}
	if err := bestError(errs...); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Registry) Validator(pack PackRef) (PackValidator, error) {
	if r == nil {
		return nil, errID(DefinitionOwnerMissing, "registry")
	}
	v, ok := r.validators[pack]
	if !ok {
		return nil, errID(DefinitionOwnerMissing, "pack")
	}
	return v, nil
}
func (r *Registry) Definition(kind DefinitionKind, ref DefinitionRef) (PackValidator, error) {
	if r == nil {
		return nil, errID(DefinitionOwnerMissing, "registry")
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	owner, ok := r.owners[definitionKey{kind, ref.ID, ref.Version}]
	if !ok || owner != ref.Pack {
		return nil, errID(DefinitionOwnerMissing, "definition")
	}
	return r.Validator(ref.Pack)
}
func (r *Registry) ValidateFact(key FactKey, value *Value) error {
	keyErr := key.Validate()
	var valueErr error
	if value != nil {
		valueErr = value.Validate()
	}
	if err := bestError(keyErr, valueErr); err != nil {
		return err
	}
	validator, err := r.Validator(PackRef{ID: key.PackID, Version: key.PackVersion})
	if err != nil {
		return err
	}
	return validator.ValidateFact(key, value)
}
func (r *Registry) ValidateService(service ServiceInstance) error {
	if err := service.Validate(); err != nil {
		return err
	}
	validator, err := r.Definition(DefinitionService, service.Definition)
	if err != nil {
		return err
	}
	return validator.ValidateService(service)
}
func (r *Registry) ValidateCapability(capability CapabilityInstance) error {
	if err := capability.Validate(); err != nil {
		return err
	}
	validator, err := r.Definition(DefinitionCapability, capability.Definition)
	if err != nil {
		return err
	}
	return validator.ValidateCapability(capability)
}
func (r *Registry) ValidateFactCandidate(candidate FactCandidate) error {
	if err := candidate.Validate(); err != nil {
		return err
	}
	return r.ValidateFact(candidate.Key, candidate.Value)
}
func (r *Registry) ValidateField(ref DefinitionRef, field TypedField) error {
	if err := bestError(ref.Validate(), field.Validate()); err != nil {
		return err
	}
	if ref.ID != field.ID {
		return errID(InvalidValue, "field definition")
	}
	validator, err := r.Definition(DefinitionField, ref)
	if err != nil {
		return err
	}
	return validator.ValidateField(ref, field)
}
