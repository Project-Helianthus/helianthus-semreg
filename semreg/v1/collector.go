package semreg

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Collection identity depends on these fields only. Payload validity and
// unrelated members must neither suppress a supplied key nor fabricate one.
// The same projection guards typed comparisons and wire-tree comparisons.
func collectionKeyFields(t reflect.Type) []string {
	switch t {
	case reflect.TypeOf(EvidenceRef{}):
		return []string{"owner", "kind", "contract", "digest"}
	case reflect.TypeOf(SourcePathRef{}):
		return []string{"source_id", "source_epoch_id", "driver_generation", "binding_id"}
	case reflect.TypeOf(DefinitionRef{}):
		return []string{"id", "version"}
	case reflect.TypeOf(Symbol{}):
		return []string{"namespace", "token"}
	case reflect.TypeOf(Dimension{}), reflect.TypeOf(TypedField{}):
		return []string{"id"}
	case reflect.TypeOf(DerivationInput{}), reflect.TypeOf(FactCandidate{}):
		return []string{"candidate_id"}
	case reflect.TypeOf(Conflict{}):
		return []string{"conflict_id"}
	}
	return nil
}

func wireField(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if strings.Split(f.Tag.Get("json"), ",")[0] == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

func collectionKeyValid(v reflect.Value) bool {
	fields := collectionKeyFields(v.Type())
	if len(fields) == 0 {
		if record, ok := v.Interface().(Record); ok {
			return record.Validate() == nil
		}
		return true
	}
	for _, name := range fields {
		field, _ := wireField(v.Type(), name)
		value := v.FieldByIndex(field.Index)
		if record, ok := value.Interface().(Record); ok && record.Validate() != nil {
			return false
		}
		if name == "driver_generation" && !positive(value.Interface().(Uint64)) {
			return false
		}
		if v.Type() == reflect.TypeOf(Symbol{}) && name == "token" && (value.String() == "" || validateText(value.String(), 0, 256, false) != nil) {
			return false
		}
	}
	return true
}

func collectionCompare(a, b reflect.Value) int {
	switch av := a.Interface().(type) {
	case EvidenceRef:
		return compareEvidence(av, b.Interface().(EvidenceRef))
	case SourcePathRef:
		return compareSourcePath(av, b.Interface().(SourcePathRef))
	case DefinitionRef:
		return compareDefinition(av, b.Interface().(DefinitionRef))
	}
	for _, name := range collectionKeyFields(a.Type()) {
		field, _ := wireField(a.Type(), name)
		if cmp := strings.Compare(a.FieldByIndex(field.Index).String(), b.FieldByIndex(field.Index).String()); cmp != 0 {
			return cmp
		}
	}
	if a.Kind() == reflect.String {
		return strings.Compare(a.String(), b.String())
	}
	return 0
}

func nodeMember(node jsonNode, name string) (jsonNode, bool) {
	for _, member := range node.object {
		if member.key == name {
			return member.value, true
		}
	}
	return jsonNode{}, false
}

// Scalar aliases have an enclosing domain: an EvidenceRef contract is evidence
// syntax, time counters are time syntax, and revision/generation counters are
// positive identities. Classify here before any enclosing collector ranks them.
func fieldSemanticErrors(parent reflect.Type, name string, node jsonNode, t reflect.Type) []error {
	errs := independentlyKnowableErrors(node, t)
	var shape []error
	validateShape(node, t, &shape)
	if len(shape) != 0 {
		return errs
	}
	value, isString := node.scalar.(string)
	if !isString {
		return errs
	}
	domain := ErrorID("")
	switch parent {
	case reflect.TypeOf(EvidenceRef{}):
		if name == "owner" || name == "kind" || name == "contract" || name == "digest" {
			domain = InvalidEvidence
		}
	case reflect.TypeOf(TimePoint{}), reflect.TypeOf(MonotonicPoint{}), reflect.TypeOf(FreshnessPolicy{}):
		if t == reflect.TypeOf(Uint64("")) || t == reflect.TypeOf(Int64("")) {
			domain = InvalidTime
		}
	case reflect.TypeOf(Decimal{}):
		if name == "coefficient" && (!coefficientRE.MatchString(value) || value != "0" && strings.HasSuffix(value, "0")) {
			errs = append(errs, errID(InvalidDecimal, "coefficient"))
		}
	}
	if t == reflect.TypeOf(Uint64("")) && (name == "revision" || name == "candidate_revision" || name == "driver_generation") {
		domain = InvalidIdentifier
		if !positive(Uint64(value)) {
			errs = append(errs, errID(domain, name))
		}
	}
	if domain != "" {
		for i, err := range errs {
			if err != nil {
				errs[i] = errID(domain, name)
			}
		}
	}
	return errs
}

func wireCollectionErrors(node jsonNode, element reflect.Type) []error {
	if node.kind != 'a' || (element.Kind() != reflect.String && len(collectionKeyFields(element)) == 0) {
		return nil
	}
	var keys []reflect.Value
	for _, child := range node.array {
		projection := child
		if fields := collectionKeyFields(element); len(fields) != 0 {
			projection = jsonNode{kind: 'o'}
			for _, name := range fields {
				member, present := nodeMember(child, name)
				field, _ := wireField(element, name)
				var errs []error
				validateShape(member, field.Type, &errs)
				if !present || len(errs) != 0 {
					break
				}
				projection.object = append(projection.object, jsonMember{name, member})
			}
			if len(projection.object) != len(fields) {
				continue
			}
		} else {
			var errs []error
			validateShape(child, element, &errs)
			if len(errs) != 0 {
				continue
			}
		}
		raw, _ := json.Marshal(jsonNodeValue(projection))
		value := reflect.New(element)
		if json.Unmarshal(raw, value.Interface()) == nil && collectionKeyValid(value.Elem()) {
			keys = append(keys, value.Elem())
		}
	}
	duplicateClass, ordered := DuplicateKey, true
	if element == reflect.TypeOf(DerivationInput{}) {
		duplicateClass = DerivationCycle
	}
	if element == reflect.TypeOf(TargetID("")) {
		duplicateClass, ordered = CausalBudgetExceeded, false
	}
	var errs []error
	for i := range keys {
		for j := 0; j < i; j++ {
			if collectionCompare(keys[j], keys[i]) == 0 {
				errs = append(errs, errID(duplicateClass, "collection key"))
			}
		}
		if ordered && i > 0 && collectionCompare(keys[i-1], keys[i]) > 0 {
			errs = append(errs, errID(NoncanonicalOrder, "collection keys"))
		}
	}
	return errs
}

// Conditional required members remain knowable even when an unrelated field
// has the wrong token. Read discriminants from supplied string tokens only.
func conditionalMemberErrors(node jsonNode, t reflect.Type) []error {
	if node.kind != 'o' {
		return nil
	}
	stringAt := func(names ...string) string {
		current := node
		for _, name := range names {
			var ok bool
			current, ok = nodeMember(current, name)
			if !ok {
				return ""
			}
		}
		value, _ := current.scalar.(string)
		return value
	}
	var required []string
	switch t {
	case reflect.TypeOf(OriginRef{}):
		if stringAt("kind") == string(OriginNativeObservation) {
			required = append(required, "source_id", "source_epoch_id", "binding_id")
		}
	case reflect.TypeOf(FactCandidate{}):
		qualification, availability := stringAt("quality", "qualification"), stringAt("quality", "availability")
		if qualification != "" && availability != "" && qualification != string(QualificationUnsupported) && qualification != string(QualificationRejected) && availability != string(AvailabilityWithdrawn) {
			required = append(required, "value")
		}
		switch stringAt("quality", "assertion") {
		case string(AssertionInferred):
			required = append(required, "derivation")
		case string(AssertionObserved):
			required = append(required, "binding_id", "source_epoch_id", "driver_generation")
		}
		switch stringAt("origin", "kind") {
		case string(OriginNativeObservation):
			required = append(required, "binding_id", "source_epoch_id", "driver_generation")
		case string(OriginDerived):
			required = append(required, "derivation")
		case string(OriginProjection):
			required = append(required, "causal")
		}
	}
	var errs []error
	for _, name := range required {
		member, present := nodeMember(node, name)
		if !present || member.kind == 'n' {
			errs = append(errs, errID(MissingMember, name))
		}
	}
	return errs
}
