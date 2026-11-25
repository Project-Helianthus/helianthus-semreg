package call

import (
	"reflect"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/operation"
)

// releaseCase is the typed boolean alternative of one existing operation.
// The field and effect are admitted by this kernel composition because the
// published definition index does not carry them.
type releaseCase struct {
	operation  semreg.DefinitionID
	field      semreg.DefinitionID
	effect     semreg.DefinitionID
	dimension  semreg.DefinitionID
	capability semreg.DefinitionID
}

type releasePack struct {
	inner semreg.PackValidator
	spec  releaseCase
}

func newReleasePack(inner semreg.PackValidator, spec releaseCase) semreg.PackValidator {
	provider, ok := inner.(semreg.PackMetadataProvider)
	if ok {
		meta := provider.Metadata()
		var argument semreg.DefinitionID
		for _, op := range meta.Operations {
			if op.Ref.ID == spec.operation {
				spec.capability = op.Capability.ID
				argument = op.Argument.ID
			}
		}
		for _, field := range meta.Fields {
			if field.Ref.ID == argument {
				spec.dimension = field.Dimension.ID
			}
		}
	}
	return &releasePack{inner: inner, spec: spec}
}

func (p *releasePack) Pack() semreg.PackRef { return p.inner.Pack() }

func (p *releasePack) Definitions() semreg.DefinitionIndex {
	index := p.inner.Definitions()
	pack := index.Pack
	index.Fields = insertRef(index.Fields, semreg.DefinitionRef{Pack: pack, ID: p.spec.field, Version: pack.Version})
	index.EffectRules = insertRef(index.EffectRules, semreg.DefinitionRef{Pack: pack, ID: p.spec.effect, Version: pack.Version})
	return index
}

func (p *releasePack) ValidateService(s semreg.ServiceInstance) error {
	return p.inner.ValidateService(s)
}

func (p *releasePack) ValidateCapability(c semreg.CapabilityInstance) error {
	return p.inner.ValidateCapability(c)
}

func (p *releasePack) EvaluatePredicate(c semreg.FactCandidate, op semreg.PredicateOp, v semreg.Value) (bool, error) {
	return p.inner.EvaluatePredicate(c, op, v)
}

func (p *releasePack) ValidateFact(key semreg.FactKey, value *semreg.Value) error {
	if key.FactID == p.spec.field {
		return p.releaseValue(key, value)
	}
	return p.inner.ValidateFact(key, value)
}

func (p *releasePack) ValidateField(ref semreg.DefinitionRef, field semreg.TypedField) error {
	if field.ID == p.spec.field {
		pack := p.Pack()
		if ref.Pack != pack || ref.ID != field.ID || ref.Version != pack.Version {
			return &semreg.Error{ID: semreg.DefinitionOwnerMissing, Detail: "release field"}
		}
		return p.releaseValue(semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: field.ID, Dimensions: []semreg.Dimension{{ID: p.spec.dimension, Value: textValue("resource")}}}, &field.Value)
	}
	return p.inner.ValidateField(ref, field)
}

func (p *releasePack) MatchConstraints(c semreg.CapabilityInstance, fields []semreg.TypedField) error {
	if len(fields) == 1 && fields[0].ID == p.spec.field {
		if err := p.inner.ValidateCapability(c); err != nil {
			return err
		}
		if fields[0].Value.Kind != semreg.ValueBoolean || fields[0].Value.Boolean == nil || *fields[0].Value.Boolean {
			return &semreg.Error{ID: semreg.InvalidValue, Detail: "release argument"}
		}
		return nil
	}
	return p.inner.MatchConstraints(c, fields)
}

func (p *releasePack) ValidateIntent(intent operation.Intent) error {
	if !p.isRelease(intent) {
		return p.operationHook().ValidateIntent(intent)
	}
	if err := intent.ValidateWithoutLifetime(); err != nil {
		return err
	}
	pack := p.Pack()
	if intent.Kind.Pack != pack || intent.Kind.ID != p.spec.operation || intent.Kind.Version != pack.Version {
		return &semreg.Error{ID: semreg.DefinitionOwnerMissing, Detail: "release operation"}
	}
	if intent.RequiredCapability.Pack != pack || intent.RequiredCapability.DefinitionID != p.spec.capability || intent.RequiredCapability.Versions.Minimum != pack.Version || intent.RequiredCapability.Versions.MaximumExclusive != "2.0.0" {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "release capability"}
	}
	if len(intent.Arguments) != 1 || intent.Arguments[0].ID != p.spec.field {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "release arguments"}
	}
	if err := p.releaseValue(intent.ExpectedEffect.Fact, &intent.Arguments[0].Value); err != nil {
		return err
	}
	if intent.Arguments[0].Value.Boolean == nil || *intent.Arguments[0].Value.Boolean {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "release value"}
	}
	effect := intent.ExpectedEffect
	if effect.Rule.Pack != pack || effect.Rule.ID != p.spec.effect || effect.Rule.Version != pack.Version || effect.Operator != semreg.PredicateEqual {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "release effect"}
	}
	if !reflect.DeepEqual(effect.Fact, semreg.FactKey{PackID: pack.ID, PackVersion: pack.Version, FactID: p.spec.field, Dimensions: effect.Fact.Dimensions}) {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "release fact"}
	}
	if !reflect.DeepEqual(effect.Expected, intent.Arguments[0].Value) {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "release effect value"}
	}
	return nil
}

func (p *releasePack) EvaluateReadback(intent operation.Intent, candidate semreg.FactCandidate) (operation.ReadbackRelation, error) {
	if !p.isRelease(intent) {
		return p.operationHook().EvaluateReadback(intent, candidate)
	}
	if err := p.ValidateIntent(intent); err != nil {
		return "", err
	}
	if candidate.Value == nil || !reflect.DeepEqual(candidate.Key, intent.ExpectedEffect.Fact) {
		return operation.ReadbackInconclusive, nil
	}
	if candidate.Value.Kind != semreg.ValueBoolean || candidate.Value.Boolean == nil {
		return operation.ReadbackInconclusive, nil
	}
	want := intent.ExpectedEffect.Expected
	if want.Kind == semreg.ValueBoolean && want.Boolean != nil && !*want.Boolean && !*candidate.Value.Boolean {
		return operation.ReadbackConfirms, nil
	}
	return operation.ReadbackContradicts, nil
}

func (p *releasePack) isRelease(intent operation.Intent) bool {
	if intent.ExpectedEffect.Rule.ID == p.spec.effect {
		return true
	}
	return len(intent.Arguments) == 1 && intent.Arguments[0].ID == p.spec.field
}

func (p *releasePack) operationHook() operation.OperationPackValidator {
	return p.inner.(operation.OperationPackValidator)
}

func (p *releasePack) releaseValue(key semreg.FactKey, value *semreg.Value) error {
	pack := p.Pack()
	if value == nil || value.Kind != semreg.ValueBoolean || value.Boolean == nil {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "release boolean"}
	}
	if err := value.Validate(); err != nil {
		return err
	}
	if key.PackID != pack.ID || key.PackVersion != pack.Version || key.FactID != p.spec.field {
		return &semreg.Error{ID: semreg.DefinitionOwnerMissing, Detail: "release fact"}
	}
	if len(key.Dimensions) != 1 || key.Dimensions[0].ID != p.spec.dimension || key.Dimensions[0].Value.Kind != semreg.ValueText || key.Dimensions[0].Value.Text == nil || *key.Dimensions[0].Value.Text == "" {
		return &semreg.Error{ID: semreg.InvalidValue, Detail: "release dimension"}
	}
	return nil
}

func insertRef(refs []semreg.DefinitionRef, ref semreg.DefinitionRef) []semreg.DefinitionRef {
	out := make([]semreg.DefinitionRef, 0, len(refs)+1)
	placed := false
	for _, cur := range refs {
		if !placed && cur.ID > ref.ID {
			out = append(out, ref)
			placed = true
		}
		out = append(out, cur)
	}
	if !placed {
		out = append(out, ref)
	}
	return out
}

func textValue(v string) semreg.Value {
	return semreg.Value{Kind: semreg.ValueText, Text: &v}
}
