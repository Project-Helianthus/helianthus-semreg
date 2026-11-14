package semreg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

type jsonNode struct {
	scalar any
	object []jsonMember
	array  []jsonNode
	kind   byte
}
type jsonMember struct {
	key   string
	value jsonNode
}

func validateUnicodeEscapes(raw []byte) bool {
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if !inString {
			if b == '"' {
				inString = true
			}
			continue
		}
		if escaped {
			escaped = false
			if b != 'u' {
				continue
			}
			if i+4 >= len(raw) {
				return false
			}
			n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
			if err != nil {
				return false
			}
			i += 4
			if n >= 0xD800 && n <= 0xDBFF {
				if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
					return false
				}
				m, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
				if err != nil || m < 0xDC00 || m > 0xDFFF {
					return false
				}
				i += 6
			} else if n >= 0xDC00 && n <= 0xDFFF {
				return false
			}
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == '"' {
			inString = false
		}
	}
	return !inString && !escaped
}
func parseJSON(raw []byte) (jsonNode, bool, error) {
	if len(raw) == 0 || bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) || !utf8.Valid(raw) || !validateUnicodeEscapes(raw) {
		return jsonNode{}, false, errID(InvalidJSON, "utf-8 json")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	duplicate := false
	node, err := parseJSONNode(decoder, &duplicate)
	if err != nil {
		return jsonNode{}, duplicate, errID(InvalidJSON, "json value")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return jsonNode{}, duplicate, errID(InvalidJSON, "trailing data")
	}
	return node, duplicate, nil
}
func parseJSONNode(decoder *json.Decoder, duplicate *bool) (jsonNode, error) {
	token, err := decoder.Token()
	if err != nil {
		return jsonNode{}, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			node := jsonNode{kind: 'o'}
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return jsonNode{}, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return jsonNode{}, fmt.Errorf("object key")
				}
				child, err := parseJSONNode(decoder, duplicate)
				if err != nil {
					return jsonNode{}, err
				}
				if seen[key] {
					*duplicate = true
				}
				seen[key] = true
				node.object = append(node.object, jsonMember{key, child})
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return jsonNode{}, fmt.Errorf("object end")
			}
			return node, nil
		case '[':
			node := jsonNode{kind: 'a'}
			for decoder.More() {
				child, err := parseJSONNode(decoder, duplicate)
				if err != nil {
					return jsonNode{}, err
				}
				node.array = append(node.array, child)
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return jsonNode{}, fmt.Errorf("array end")
			}
			return node, nil
		default:
			return jsonNode{}, fmt.Errorf("delimiter")
		}
	case nil:
		return jsonNode{kind: 'n'}, nil
	default:
		return jsonNode{kind: 's', scalar: value}, nil
	}
}
func validateShape(node jsonNode, t reflect.Type, errors *[]error) {
	validateShapeWithNumberDomain(node, t, errors, InvalidValue)
}

func validateShapeWithNumberDomain(node jsonNode, t reflect.Type, errors *[]error, numberRangeError ErrorID) {
	if t.Kind() == reflect.Pointer {
		if node.kind == 'n' {
			*errors = append(*errors, errID(MissingMember, "null"))
			return
		}
		validateShapeWithNumberDomain(node, t.Elem(), errors, numberRangeError)
		return
	}
	if node.kind == 'n' {
		*errors = append(*errors, errID(MissingMember, "null"))
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		if node.kind != 'o' {
			*errors = append(*errors, errID(InvalidValue, "object token"))
			return
		}
		fields := map[string]struct {
			t        reflect.Type
			optional bool
		}{}
		for name, field := range effectiveJSONFields(t) {
			fields[name] = struct {
				t        reflect.Type
				optional bool
			}{field.Type, jsonFieldOptional(field)}
		}
		// Only public document records own a contract discriminator. A decoded
		// EvidenceRef still treats contract as ordinary required metadata. Child
		// packages opt in through ContractDiscriminator so this root package does
		// not depend on a projection, compatibility, transport, or vendor package.
		discriminatorMember, discriminatorExpected, document := documentDiscriminator(t)
		discriminatorValid := !document || validDiscriminatorMetadata(t, discriminatorMember, discriminatorExpected)
		if !discriminatorValid {
			*errors = append(*errors, errID(InvalidContract, "contract discriminator metadata"))
		}
		seen := map[string]bool{}
		for _, member := range node.object {
			field, ok := fields[member.key]
			if !ok {
				*errors = append(*errors, errID(UnknownMember, member.key))
				continue
			}
			seen[member.key] = true
			if document && member.key == discriminatorMember {
				value, ok := member.value.scalar.(string)
				if member.value.kind != 's' || !ok || ContractVersion(value) != discriminatorExpected {
					*errors = append(*errors, errID(InvalidContract, discriminatorMember))
				}
				continue
			}
			fieldRangeError := InvalidValue
			if t == reflect.TypeOf(Decimal{}) && member.key == "exponent10" {
				fieldRangeError = InvalidDecimal
			}
			if t == reflect.TypeOf(CausalContext{}) && (member.key == "hop_count" || member.key == "max_hops") {
				fieldRangeError = CausalBudgetExceeded
			}
			validateShapeWithNumberDomain(member.value, field.t, errors, fieldRangeError)
		}
		for name, field := range fields {
			if !field.optional && !seen[name] {
				id := MissingMember
				if document && name == discriminatorMember {
					id = InvalidContract
				}
				*errors = append(*errors, errID(id, name))
			}
		}
		// Opting into a public document discriminator makes that member
		// unconditional. A child cannot weaken it with omitempty or name a
		// phantom field that binding would silently synthesize or discard.
		if document && !seen[discriminatorMember] {
			*errors = append(*errors, errID(InvalidContract, discriminatorMember))
		}
		// An optional graph member is omitted when empty; binding a present
		// empty one would silently change the canonical bytes.
		*errors = append(*errors, graphPresentEmptyErrors(node, t)...)
	case reflect.Slice:
		if node.kind != 'a' {
			*errors = append(*errors, errID(InvalidValue, "array token"))
			return
		}
		for _, child := range node.array {
			validateShapeWithNumberDomain(child, t.Elem(), errors, numberRangeError)
		}
	case reflect.String:
		if node.kind != 's' {
			*errors = append(*errors, errID(InvalidValue, "string token"))
			return
		}
		if _, ok := node.scalar.(string); !ok {
			*errors = append(*errors, errID(InvalidValue, "string token"))
		}
	case reflect.Bool:
		if node.kind != 's' {
			*errors = append(*errors, errID(InvalidValue, "boolean token"))
			return
		}
		if _, ok := node.scalar.(bool); !ok {
			*errors = append(*errors, errID(InvalidValue, "boolean token"))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if node.kind != 's' {
			*errors = append(*errors, errID(InvalidValue, "number token"))
			return
		}
		number, ok := node.scalar.(json.Number)
		if !ok {
			*errors = append(*errors, errID(InvalidValue, "number token"))
			return
		}
		if t.Kind() >= reflect.Int && t.Kind() <= reflect.Int64 {
			if _, err := strconv.ParseInt(number.String(), 10, t.Bits()); err != nil {
				id := InvalidValue
				if numberRangeError != InvalidValue {
					if _, ok := new(big.Int).SetString(number.String(), 10); ok {
						id = numberRangeError
					}
				}
				*errors = append(*errors, errID(id, "integer token"))
			}
		} else if _, err := strconv.ParseUint(number.String(), 10, t.Bits()); err != nil {
			id := InvalidValue
			if numberRangeError != InvalidValue && i64RE.MatchString(number.String()) {
				id = numberRangeError
			}
			*errors = append(*errors, errID(id, "integer token"))
		}
	default:
		*errors = append(*errors, errID(InvalidValue, "unsupported wire type"))
	}
}

func documentDiscriminator(t reflect.Type) (string, ContractVersion, bool) {
	switch t {
	case reflect.TypeOf(PublicationBatch{}), reflect.TypeOf(Snapshot{}):
		return "contract", ContractKernelV1, true
	case reflect.TypeOf(EvaluationView{}):
		return "contract", ContractEvaluationV1, true
	case reflect.TypeOf(Selection{}):
		return "contract", ContractSelectionV1, true
	}
	if reflect.PointerTo(t).Implements(reflect.TypeOf((*ContractDiscriminator)(nil)).Elem()) {
		member, expected := reflect.New(t).Interface().(ContractDiscriminator).ContractDiscriminator()
		return member, expected, true
	}
	if t.Implements(reflect.TypeOf((*ContractDiscriminator)(nil)).Elem()) {
		member, expected := reflect.Zero(t).Interface().(ContractDiscriminator).ContractDiscriminator()
		return member, expected, true
	}
	return "", "", false
}

func validDiscriminatorMetadata(t reflect.Type, member string, expected ContractVersion) bool {
	if t.Kind() != reflect.Struct || member == "" || expected.Validate() != nil {
		return false
	}
	field, found := effectiveJSONFields(t)[member]
	if !found || field.Type != reflect.TypeOf(ContractVersion("")) {
		return false
	}
	// Contract metadata remains a direct, explicitly tagged member. The shared
	// resolver determines which direct candidate actually binds before this
	// narrower metadata contract is applied.
	if len(field.Index) != 1 || field.Anonymous {
		return false
	}
	parts := strings.Split(field.Tag.Get("json"), ",")
	if len(parts) == 0 || parts[0] != member || field.Tag.Get("json") == "-" || !validJSONTagName(parts[0]) {
		return false
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			return false
		}
	}
	return true
}

func validJSONTagName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", char):
		case unicode.IsLetter(char), unicode.IsDigit(char):
		default:
			return false
		}
	}
	return true
}

// effectiveJSONFields mirrors encoding/json's effective-field selection for
// the strict decoder. It deliberately caches only immutable metadata: callers
// must treat the returned map and StructField indexes as read-only.
type jsonFieldMetadata struct {
	fields map[string]reflect.StructField
}

type jsonFieldCandidate struct {
	field  reflect.StructField
	name   string
	tagged bool
}

type jsonFieldScan struct {
	t     reflect.Type
	index []int
}

var jsonFieldMetadataCache sync.Map // map[reflect.Type]*jsonFieldMetadata

func effectiveJSONFields(t reflect.Type) map[string]reflect.StructField {
	return cachedJSONFieldMetadata(t).fields
}

func cachedJSONFieldMetadata(t reflect.Type) *jsonFieldMetadata {
	if cached, ok := jsonFieldMetadataCache.Load(t); ok {
		return cached.(*jsonFieldMetadata)
	}
	metadata := buildJSONFieldMetadata(t)
	actual, _ := jsonFieldMetadataCache.LoadOrStore(t, metadata)
	return actual.(*jsonFieldMetadata)
}

func buildJSONFieldMetadata(t reflect.Type) *jsonFieldMetadata {
	metadata := &jsonFieldMetadata{fields: map[string]reflect.StructField{}}
	if t.Kind() != reflect.Struct {
		return metadata
	}
	current := []jsonFieldScan{}
	next := []jsonFieldScan{{t: t}}
	visited := map[reflect.Type]bool{}
	var count, nextCount map[reflect.Type]int
	var candidates []jsonFieldCandidate
	for len(next) != 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, map[reflect.Type]int{}
		for _, parent := range current {
			if visited[parent.t] {
				continue
			}
			visited[parent.t] = true
			for i := 0; i < parent.t.NumField(); i++ {
				field := parent.t.Field(i)
				if field.Anonymous {
					embedded := field.Type
					if embedded.Kind() == reflect.Pointer {
						embedded = embedded.Elem()
					}
					if field.PkgPath != "" && embedded.Kind() != reflect.Struct {
						continue
					}
				} else if field.PkgPath != "" {
					continue
				}
				tag := field.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name := strings.Split(tag, ",")[0]
				if !validJSONTagName(name) {
					name = ""
				}
				index := append(append([]int(nil), parent.index...), i)
				field.Index = index
				wireType := field.Type
				if wireType.Name() == "" && wireType.Kind() == reflect.Pointer {
					wireType = wireType.Elem()
				}
				if name != "" || !field.Anonymous || wireType.Kind() != reflect.Struct {
					if name == "" {
						name = field.Name
					}
					candidate := jsonFieldCandidate{field: field, name: name, tagged: strings.Split(tag, ",")[0] != "" && validJSONTagName(strings.Split(tag, ",")[0])}
					candidates = append(candidates, candidate)
					if count[parent.t] > 1 {
						candidates = append(candidates, candidate)
					}
					continue
				}
				nextCount[wireType]++
				if nextCount[wireType] == 1 {
					next = append(next, jsonFieldScan{t: wireType, index: index})
				}
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.name != right.name {
			return left.name < right.name
		}
		if len(left.field.Index) != len(right.field.Index) {
			return len(left.field.Index) < len(right.field.Index)
		}
		if left.tagged != right.tagged {
			return left.tagged
		}
		for index := range left.field.Index {
			if left.field.Index[index] != right.field.Index[index] {
				return left.field.Index[index] < right.field.Index[index]
			}
		}
		return false
	})
	for start := 0; start < len(candidates); {
		end := start + 1
		for end < len(candidates) && candidates[end].name == candidates[start].name {
			end++
		}
		winner := candidates[start]
		if end == start+1 || len(winner.field.Index) != len(candidates[start+1].field.Index) || winner.tagged != candidates[start+1].tagged {
			metadata.fields[winner.name] = winner.field
		}
		start = end
	}
	return metadata
}

func jsonFieldOptional(field reflect.StructField) bool {
	for _, option := range strings.Split(field.Tag.Get("json"), ",")[1:] {
		if option == "omitempty" {
			return true
		}
	}
	return false
}

func uniqueEffectiveJSONFields(t reflect.Type) map[string]reflect.StructField {
	return effectiveJSONFields(t)
}

func discriminatorWireErrors(node jsonNode, t reflect.Type) []error {
	member, expected, document := documentDiscriminator(t)
	if !document {
		return nil
	}
	if !validDiscriminatorMetadata(t, member, expected) {
		return []error{errID(InvalidContract, "contract discriminator metadata")}
	}
	value, present := nodeMember(node, member)
	if !present {
		return nil
	}
	if value.kind != 's' {
		return []error{errID(InvalidContract, member)}
	}
	stringValue, ok := value.scalar.(string)
	if !ok || ContractVersion(stringValue) != expected {
		return []error{errID(InvalidContract, member)}
	}
	return nil
}

func fixedContractWireErrors(node jsonNode, t reflect.Type) []error {
	if t.Kind() != reflect.Struct || !reflect.PointerTo(t).Implements(reflect.TypeOf((*WireFixedContract)(nil)).Elem()) {
		return nil
	}
	member, expected := reflect.New(t).Interface().(WireFixedContract).WireFixedContract()
	if !validDiscriminatorMetadata(t, member, expected) {
		return []error{errID(InvalidContract, "fixed contract metadata")}
	}
	value, present := nodeMember(node, member)
	if !present {
		return nil
	}
	if value.kind == 'n' {
		return nil
	}
	stringValue, ok := value.scalar.(string)
	if value.kind != 's' || !ok || ContractVersion(stringValue) != expected {
		return []error{errID(InvalidContract, member)}
	}
	return nil
}

var recordInterface = reflect.TypeOf((*Record)(nil)).Elem()

func independentlyKnowableErrors(node jsonNode, t reflect.Type) []error {
	if node.kind == 'n' {
		return nil
	}
	if t.Kind() == reflect.Pointer {
		return independentlyKnowableErrors(node, t.Elem())
	}
	var shapeErrors []error
	validateShape(node, t, &shapeErrors)
	bindable := true
	for _, err := range shapeErrors {
		if ErrorIdentifier(err) != UnknownMember {
			bindable = false
		}
	}
	if bindable && (t.Implements(recordInterface) || reflect.PointerTo(t).Implements(recordInterface)) {
		encoded, err := json.Marshal(jsonNodeValue(node))
		if err == nil {
			value := reflect.New(t)
			if json.Unmarshal(encoded, value.Interface()) == nil {
				if record, ok := value.Elem().Interface().(Record); ok {
					return []error{record.Validate(), recordGraphMembersError(record)}
				}
				if record, ok := value.Interface().(Record); ok {
					return []error{record.Validate(), recordGraphMembersError(record)}
				}
			}
		}
	}
	var errs []error
	errs = append(errs, conditionalMemberErrors(node, t)...)
	errs = append(errs, discriminatorWireErrors(node, t)...)
	errs = append(errs, fixedContractWireErrors(node, t)...)
	errs = append(errs, enclosingWireErrors(node, t)...)
	switch t.Kind() {
	case reflect.Struct:
		fields := map[string]reflect.Type{}
		for name, field := range effectiveJSONFields(t) {
			fields[name] = field.Type
		}
		for _, member := range node.object {
			if fieldType, ok := fields[member.key]; ok {
				errs = append(errs, fieldSemanticErrors(t, member.key, member.value, fieldType)...)
			}
		}
	case reflect.Slice:
		errs = append(errs, wireCollectionErrors(node, t.Elem())...)
		for _, child := range node.array {
			errs = append(errs, independentlyKnowableErrors(child, t.Elem())...)
		}
	}
	return errs
}

func jsonNodeValue(node jsonNode) interface{} {
	switch node.kind {
	case 'o':
		value := make(map[string]interface{}, len(node.object))
		for _, member := range node.object {
			value[member.key] = jsonNodeValue(member.value)
		}
		return value
	case 'a':
		value := make([]interface{}, len(node.array))
		for i, child := range node.array {
			value[i] = jsonNodeValue(child)
		}
		return value
	case 'n':
		return nil
	default:
		return node.scalar
	}
}

func decodeRecord[T Record](raw []byte) (T, error) {
	var zero T
	node, duplicate, syntax := parseJSON(raw)
	if syntax != nil {
		return zero, syntax
	}
	var errs []error
	if duplicate {
		errs = append(errs, errID(DuplicateKey, "member"))
	}
	targetType := reflect.TypeOf((*T)(nil)).Elem()
	validateShape(node, targetType, &errs)
	// Validate the original tree, including presence, before binding. Partial
	// unmarshalling synthesizes missing keys and may leave a typed nil receiver.
	errs = append(errs, independentlyKnowableErrors(node, targetType)...)
	if err := bestError(errs...); err != nil {
		return zero, err
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, errID(InvalidValue, "record binding")
	}
	return result, nil
}
func Decode[T Record](raw []byte) (T, error)                  { return decodeRecord[T](raw) }
func DecodeDecimal(raw []byte) (Decimal, error)               { return Decode[Decimal](raw) }
func DecodeValue(raw []byte) (Value, error)                   { return Decode[Value](raw) }
func DecodeFactCandidate(raw []byte) (FactCandidate, error)   { return Decode[FactCandidate](raw) }
func DecodeFactEnvelope(raw []byte) (FactEnvelope, error)     { return Decode[FactEnvelope](raw) }
func DecodeEvaluationView(raw []byte) (EvaluationView, error) { return Decode[EvaluationView](raw) }
func DecodeSelection(raw []byte) (Selection, error)           { return Decode[Selection](raw) }
func DecodeCapabilityInstance(raw []byte) (CapabilityInstance, error) {
	return Decode[CapabilityInstance](raw)
}

func CanonicalJSON(record Record) ([]byte, error) {
	if record == nil {
		return nil, errID(InvalidValue, "record")
	}
	value := reflect.ValueOf(record)
	if (value.Kind() == reflect.Ptr || value.Kind() == reflect.Map || value.Kind() == reflect.Slice || value.Kind() == reflect.Interface) && value.IsNil() {
		return nil, errID(InvalidValue, "nil record")
	}
	if err := bestError(record.Validate(), recordGraphMembersError(record)); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, errID(InvalidJSON, "marshal")
	}
	return canonicalize(raw)
}
func DigestRecord(record Record) (Digest, error) {
	canonical, err := CanonicalJSON(record)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}
func canonicalize(raw []byte) ([]byte, error) {
	node, duplicate, err := parseJSON(raw)
	if err != nil {
		return nil, err
	}
	if duplicate {
		return nil, errID(DuplicateKey, "member")
	}
	var out bytes.Buffer
	if err := writeCanonical(&out, node); err != nil {
		return nil, errID(InvalidJSON, "canonical json")
	}
	return out.Bytes(), nil
}
func compareJCSKey(a, b string) int {
	au, bu := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(au) && i < len(bu); i++ {
		if au[i] < bu[i] {
			return -1
		}
		if au[i] > bu[i] {
			return 1
		}
	}
	if len(au) < len(bu) {
		return -1
	}
	if len(au) > len(bu) {
		return 1
	}
	return 0
}
func writeJSONString(out *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("invalid utf-8")
	}
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return nil
}
func writeCanonical(out *bytes.Buffer, node jsonNode) error {
	switch node.kind {
	case 'n':
		out.WriteString("null")
	case 's':
		switch value := node.scalar.(type) {
		case string:
			return writeJSONString(out, value)
		case bool:
			out.WriteString(strconv.FormatBool(value))
		case json.Number:
			out.WriteString(value.String())
		default:
			return fmt.Errorf("scalar")
		}
	case 'a':
		out.WriteByte('[')
		for i, child := range node.array {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, child); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case 'o':
		members := append([]jsonMember(nil), node.object...)
		sort.Slice(members, func(i, j int) bool { return compareJCSKey(members[i].key, members[j].key) < 0 })
		out.WriteByte('{')
		for i, member := range members {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeJSONString(out, member.key); err != nil {
				return err
			}
			out.WriteByte(':')
			if err := writeCanonical(out, member.value); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("node")
	}
	return nil
}
