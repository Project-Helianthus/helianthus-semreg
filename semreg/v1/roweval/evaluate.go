package roweval

import (
	"fmt"
	"sort"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Evaluation is the result of evaluating one observation. Native is the native
// evaluation of the matched row; it is kept when no candidate follows. Candidate
// and Subject are set together. When Candidate is nil, Reason says why.
type Evaluation struct {
	Native    *Native
	Reason    Reason
	Detail    string
	Candidate *v1.FactCandidate
	Subject   *Subject
	Citations []v1.EvidenceRef
}

// Evaluate matches the observation to exactly one native row, evaluates it, and
// when exactly one applicable binding has a ReadRow turns it into a candidate
// that is the caller's template with only the fact key and the transformed
// value written. The typed error is an InputError; the Evaluation returned with
// it keeps the native evaluation computed so far.
func Evaluate[S any](t Table[S], a Acquired[S]) (Evaluation, error) {
	rows, missing := matchNative(t, a.Selector, a.Qualifiers)
	switch len(rows) {
	case 0:
		return Evaluation{Reason: ReasonNoMatch, Detail: "no native row applies" + missingText(missing)}, nil
	case 1:
	default:
		return Evaluation{Reason: ReasonAmbiguous, Detail: fmt.Sprintf("%d native rows apply", len(rows))}, nil
	}
	row := rows[0]
	inputs := detach(append([]Input{{Payload: a.Payload, Receipt: a.Receipt}}, a.Inputs...), a.Qualifiers, a.Coordinates)
	native, env := evalNative(t, a, row, inputs)
	ev := Evaluation{Native: &native, Citations: env.citations}
	if native.Reason != "" {
		ev.Reason, ev.Detail = native.Reason, native.Detail
		return ev, nil
	}

	var bindings []BindingRow
	for _, b := range t.Bindings {
		if b.NativeID == row.ID && b.Read != nil && b.When.check(a.Qualifiers).ok {
			bindings = append(bindings, b)
		}
	}
	switch len(bindings) {
	case 0:
		ev.Reason, ev.Detail = ReasonNoBinding, "no applicable binding with a read row"
		return ev, nil
	case 1:
	default:
		ev.Reason, ev.Detail = ReasonAmbiguous, fmt.Sprintf("%d bindings apply", len(bindings))
		return ev, nil
	}
	b := bindings[0]

	subject, err := resolve(a.Roles, b)
	if err != nil {
		return ev, err
	}
	env.row, env.dir = RowID(row.ID), Forward
	value, reason, detail := env.apply(b.Read.ToRegistry, *native.Value, b.Read.Unit)
	ev.Citations = env.citations
	if reason != "" {
		ev.Reason, ev.Detail = reason, detail
		return ev, nil
	}
	if err := value.Validate(); err != nil {
		ev.Reason, ev.Detail = ReasonRegistry, err.Error()
		return ev, nil
	}
	key, err := factKey(a, b)
	if err != nil {
		return ev, err
	}
	if a.Template.FreshnessPolicy.PolicyID != b.Read.Policy {
		return ev, &InputError{Kind: InputPolicyMismatch, Detail: fmt.Sprintf("template policy %q, read row policy %q", a.Template.FreshnessPolicy.PolicyID, b.Read.Policy)}
	}
	cand := cloneCandidate(a.Template)
	cand.Key = key
	cand.Value = nil
	q := cand.Quality
	if !(q.Qualification == v1.QualificationUnsupported || q.Qualification == v1.QualificationRejected || q.Availability == v1.AvailabilityWithdrawn) {
		cand.Value = &value
	}
	if err := cand.Validate(); err != nil {
		return ev, &InputError{Kind: InputMissingMetadata, Detail: "candidate template", Err: err}
	}
	ev.Candidate, ev.Subject = &cand, &subject
	return ev, nil
}

func hasDefinition(c []v1.DefinitionIndex, ref v1.DefinitionRef, pick func(v1.DefinitionIndex) []v1.DefinitionRef) bool {
	for _, idx := range c {
		if idx.Pack != ref.Pack {
			continue
		}
		for _, d := range pick(idx) {
			if d == ref {
				return true
			}
		}
	}
	return false
}

// resolve resolves the subject role and the catalog definitions of a binding.
func resolve(r Roles, b BindingRow) (Subject, error) {
	bound, ok := r.Subjects[b.SubjectRole]
	if !ok || b.SubjectRole == "" {
		return Subject{}, &InputError{Kind: InputUnresolvedRole, Detail: fmt.Sprintf("role %q has no binding", b.SubjectRole)}
	}
	if err := bound.ServiceInstance.Validate(); err != nil {
		return Subject{}, &InputError{Kind: InputUnresolvedRole, Detail: fmt.Sprintf("role %q service instance", b.SubjectRole), Err: err}
	}
	if !hasDefinition(r.Catalog, b.Service, func(i v1.DefinitionIndex) []v1.DefinitionRef { return i.Services }) {
		return Subject{}, &InputError{Kind: InputUnresolvedRole, Detail: fmt.Sprintf("service %q of role %q is not in the catalog", b.Service.ID, b.SubjectRole)}
	}
	if b.Capability != (v1.DefinitionRef{}) && !hasDefinition(r.Catalog, b.Capability, func(i v1.DefinitionIndex) []v1.DefinitionRef { return i.Capabilities }) {
		return Subject{}, &InputError{Kind: InputUnresolvedRole, Detail: fmt.Sprintf("capability %q of role %q is not in the catalog", b.Capability.ID, b.SubjectRole)}
	}
	if b.Read != nil && !hasDefinition(r.Catalog, b.Read.Fact, func(i v1.DefinitionIndex) []v1.DefinitionRef { return i.Fields }) {
		return Subject{}, &InputError{Kind: InputUnresolvedRole, Detail: fmt.Sprintf("fact %q of role %q is not in the catalog", b.Read.Fact.ID, b.SubjectRole)}
	}
	if b.Write != nil && !hasDefinition(r.Catalog, b.Write.Operation, func(i v1.DefinitionIndex) []v1.DefinitionRef { return i.Operations }) {
		return Subject{}, &InputError{Kind: InputUnresolvedRole, Detail: fmt.Sprintf("operation %q of role %q is not in the catalog", b.Write.Operation.ID, b.SubjectRole)}
	}
	return Subject{Role: b.SubjectRole, Service: b.Service, Capability: b.Capability, ServiceInstance: bound.ServiceInstance, CapabilityInstance: bound.CapabilityInstance}, nil
}

// factKey builds the key of the read row's fact. Each declared dimension is the
// text of its bound role, or of the value of its bound slot.
func factKey[S any](a Acquired[S], b BindingRow) (v1.FactKey, error) {
	key := v1.FactKey{PackID: b.Read.Fact.Pack.ID, PackVersion: b.Read.Fact.Pack.Version, FactID: b.Read.Fact.ID, Dimensions: []v1.Dimension{}}
	ids := make([]string, 0, len(b.Read.Dimensions))
	for id := range b.Read.Dimensions {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	for _, id := range ids {
		name := b.Read.Dimensions[v1.DefinitionID(id)]
		_, isRole := a.Roles.Subjects[name]
		slot, isSlot := a.Coordinates[name]
		var text string
		switch {
		case isRole && isSlot:
			return key, &InputError{Kind: InputUnresolvedSlot, Detail: fmt.Sprintf("dimension %q: %q is both a role and a slot", id, name)}
		case isRole:
			text = name
		case isSlot:
			text = slot
		default:
			return key, &InputError{Kind: InputUnresolvedSlot, Detail: fmt.Sprintf("dimension %q: %q is neither a bound role nor a bound slot", id, name)}
		}
		key.Dimensions = append(key.Dimensions, v1.Dimension{ID: v1.DefinitionID(id), Value: v1.Value{Kind: v1.ValueText, Text: &text}})
	}
	return key, nil
}
