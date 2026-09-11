package semreg

import (
	"bytes"
	"sort"
)

// FieldMetadata describes one pack-owned fact field and its exact value shape.
// CanonicalUnit is nil for a non-quantity field.
type FieldMetadata struct {
	Ref           DefinitionRef
	CanonicalUnit *DefinitionRef
	Dimension     DefinitionRef
}

// ServiceMetadata describes the sole fact-key dimension of a service.
type ServiceMetadata struct {
	Ref              DefinitionRef
	FactKeyDimension DefinitionRef
}

// CapabilityMetadata describes the service that owns a capability.
type CapabilityMetadata struct {
	Ref     DefinitionRef
	Service DefinitionRef
}

// OperationMetadata describes one exact, pack-owned operation shape.
type OperationMetadata struct {
	Ref        DefinitionRef
	Capability DefinitionRef
	Service    DefinitionRef
	Argument   DefinitionRef
	Effect     DefinitionRef
}

// PackMetadata is a fresh, protocol-neutral description exported by one pack.
// The registry copies and validates it before accepting it.
type PackMetadata struct {
	Pack         PackRef
	Units        []DefinitionRef
	Fields       []FieldMetadata
	Services     []ServiceMetadata
	Capabilities []CapabilityMetadata
	Operations   []OperationMetadata
}

// PackMetadataSource binds metadata to the validator that owns its private
// tables. The registry never retains either source slice.
type PackMetadataSource struct {
	Validator PackValidator
	Metadata  PackMetadata
}

// PackMetadataRegistry is the immutable query index for accepted pack metadata.
type PackMetadataRegistry struct {
	packs       []PackRef
	definitions map[DefinitionRef]struct{}
	units       map[DefinitionRef]struct{}
	fields      map[DefinitionRef]FieldMetadata
	services    map[DefinitionRef]ServiceMetadata
	caps        map[DefinitionRef]DefinitionRef
	operations  map[operationMetadataKey]struct{}
}

type operationMetadataKey struct {
	operation  DefinitionRef
	capability DefinitionRef
	service    DefinitionRef
	argument   DefinitionRef
	effect     DefinitionRef
}

// NewPackMetadataRegistry validates and freezes metadata from independent pack
// validators. It accepts reordered equivalent inputs, but rejects every duplicate,
// ambiguous, cross-pack, or wrong-version relation.
func NewPackMetadataRegistry(sources ...PackMetadataSource) (*PackMetadataRegistry, error) {
	r := &PackMetadataRegistry{
		definitions: map[DefinitionRef]struct{}{},
		units:       map[DefinitionRef]struct{}{},
		fields:      map[DefinitionRef]FieldMetadata{},
		services:    map[DefinitionRef]ServiceMetadata{},
		caps:        map[DefinitionRef]DefinitionRef{},
		operations:  map[operationMetadataKey]struct{}{},
	}
	seenPacks := map[PackRef]struct{}{}
	for _, source := range sources {
		if source.Validator == nil {
			return nil, errID(DefinitionOwnerConflict, "nil pack metadata validator")
		}
		metadata := clonePackMetadata(source.Metadata)
		index := cloneIndex(source.Validator.Definitions())
		if err := bestError(metadata.Pack.Validate(), index.Validate()); err != nil {
			return nil, err
		}
		if metadata.Pack != source.Validator.Pack() || index.Pack != metadata.Pack {
			return nil, errID(DefinitionOwnerConflict, "metadata validator pack")
		}
		if _, exists := seenPacks[metadata.Pack]; exists {
			return nil, errID(DefinitionOwnerConflict, "duplicate metadata pack")
		}
		if err := r.registerPack(metadata, index); err != nil {
			return nil, err
		}
		seenPacks[metadata.Pack] = struct{}{}
		r.packs = append(r.packs, metadata.Pack)
	}
	sort.Slice(r.packs, func(i, j int) bool { return comparePackMetadataRef(r.packs[i], r.packs[j]) < 0 })
	return r, nil
}

func (r *PackMetadataRegistry) registerPack(metadata PackMetadata, index DefinitionIndex) error {
	pack := metadata.Pack
	indexRefs := map[DefinitionRef]DefinitionKind{}
	for _, group := range []struct {
		kind DefinitionKind
		refs []DefinitionRef
	}{
		{DefinitionField, index.Fields}, {DefinitionService, index.Services},
		{DefinitionCapability, index.Capabilities}, {DefinitionOperation, index.Operations}, {DefinitionEffectRule, index.EffectRules},
	} {
		for _, ref := range group.refs {
			if ref.Pack != pack {
				return errID(DefinitionOwnerConflict, "definition index pack")
			}
			if prior, exists := indexRefs[ref]; exists && prior != group.kind {
				return errID(DefinitionOwnerConflict, "ambiguous metadata definition")
			}
			indexRefs[ref] = group.kind
		}
	}
	for _, unit := range metadata.Units {
		if err := metadataRefOwns(pack, unit); err != nil {
			return err
		}
		if _, duplicate := r.definitions[unit]; duplicate {
			return errID(DefinitionOwnerConflict, "duplicate metadata unit")
		}
		if _, duplicate := r.units[unit]; duplicate {
			return errID(DefinitionOwnerConflict, "duplicate metadata unit")
		}
		r.definitions[unit], r.units[unit] = struct{}{}, struct{}{}
	}
	for ref := range indexRefs {
		if _, exists := r.definitions[ref]; exists {
			return errID(DefinitionOwnerConflict, "duplicate metadata definition")
		}
		r.definitions[ref] = struct{}{}
	}
	if err := registerFields(r, pack, metadata.Fields, indexRefs); err != nil {
		return err
	}
	if err := registerServices(r, pack, metadata.Services, indexRefs); err != nil {
		return err
	}
	if err := registerCapabilities(r, pack, metadata.Capabilities, indexRefs); err != nil {
		return err
	}
	return registerOperations(r, pack, metadata.Operations, indexRefs)
}

func registerFields(r *PackMetadataRegistry, pack PackRef, fields []FieldMetadata, index map[DefinitionRef]DefinitionKind) error {
	seen := map[DefinitionRef]struct{}{}
	for _, field := range fields {
		if err := metadataRefOwns(pack, field.Ref); err != nil {
			return err
		}
		if index[field.Ref] != DefinitionField {
			return errID(DefinitionOwnerMissing, "metadata field")
		}
		if _, duplicate := seen[field.Ref]; duplicate {
			return errID(DefinitionOwnerConflict, "duplicate metadata field")
		}
		if err := metadataRefOwns(pack, field.Dimension); err != nil {
			return err
		}
		if field.CanonicalUnit != nil {
			if err := metadataRefOwns(pack, *field.CanonicalUnit); err != nil {
				return err
			}
			if _, ok := r.units[*field.CanonicalUnit]; !ok {
				return errID(DefinitionOwnerMissing, "metadata canonical unit")
			}
		}
		seen[field.Ref] = struct{}{}
		r.fields[field.Ref] = cloneFieldMetadata(field)
	}
	if len(seen) != countKind(index, DefinitionField) {
		return errID(DefinitionOwnerMissing, "metadata fields")
	}
	return nil
}

func registerServices(r *PackMetadataRegistry, pack PackRef, services []ServiceMetadata, index map[DefinitionRef]DefinitionKind) error {
	seen := map[DefinitionRef]struct{}{}
	for _, service := range services {
		if err := bestError(metadataRefOwns(pack, service.Ref), metadataRefOwns(pack, service.FactKeyDimension)); err != nil {
			return err
		}
		if index[service.Ref] != DefinitionService {
			return errID(DefinitionOwnerMissing, "metadata service")
		}
		if _, duplicate := seen[service.Ref]; duplicate {
			return errID(DefinitionOwnerConflict, "duplicate metadata service")
		}
		seen[service.Ref] = struct{}{}
		r.services[service.Ref] = service
	}
	if len(seen) != countKind(index, DefinitionService) {
		return errID(DefinitionOwnerMissing, "metadata services")
	}
	return nil
}

func registerCapabilities(r *PackMetadataRegistry, pack PackRef, caps []CapabilityMetadata, index map[DefinitionRef]DefinitionKind) error {
	seen := map[DefinitionRef]struct{}{}
	for _, capability := range caps {
		if err := bestError(metadataRefOwns(pack, capability.Ref), metadataRefOwns(pack, capability.Service)); err != nil {
			return err
		}
		if index[capability.Ref] != DefinitionCapability {
			return errID(DefinitionOwnerMissing, "metadata capability")
		}
		if _, ok := r.services[capability.Service]; !ok {
			return errID(DefinitionOwnerMissing, "metadata capability service")
		}
		if _, duplicate := seen[capability.Ref]; duplicate {
			return errID(DefinitionOwnerConflict, "duplicate metadata capability")
		}
		seen[capability.Ref] = struct{}{}
		r.caps[capability.Ref] = capability.Service
	}
	if len(seen) != countKind(index, DefinitionCapability) {
		return errID(DefinitionOwnerMissing, "metadata capabilities")
	}
	return nil
}

func registerOperations(r *PackMetadataRegistry, pack PackRef, operations []OperationMetadata, index map[DefinitionRef]DefinitionKind) error {
	seen := map[DefinitionRef]struct{}{}
	for _, operation := range operations {
		for _, ref := range []DefinitionRef{operation.Ref, operation.Capability, operation.Service, operation.Argument, operation.Effect} {
			if err := metadataRefOwns(pack, ref); err != nil {
				return err
			}
		}
		if index[operation.Ref] != DefinitionOperation || index[operation.Argument] != DefinitionField || index[operation.Effect] != DefinitionEffectRule {
			return errID(DefinitionOwnerMissing, "metadata operation definition")
		}
		if owner, ok := r.caps[operation.Capability]; !ok || owner != operation.Service {
			return errID(DefinitionOwnerConflict, "metadata operation capability")
		}
		if _, duplicate := seen[operation.Ref]; duplicate {
			return errID(DefinitionOwnerConflict, "duplicate metadata operation")
		}
		seen[operation.Ref] = struct{}{}
		r.operations[operationMetadataKey{operation.Ref, operation.Capability, operation.Service, operation.Argument, operation.Effect}] = struct{}{}
	}
	if len(seen) != countKind(index, DefinitionOperation) {
		return errID(DefinitionOwnerMissing, "metadata operations")
	}
	return nil
}

func metadataRefOwns(pack PackRef, ref DefinitionRef) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if ref.Pack != pack {
		return errID(DefinitionOwnerConflict, "metadata reference pack")
	}
	return nil
}

func countKind(index map[DefinitionRef]DefinitionKind, kind DefinitionKind) int {
	n := 0
	for _, candidate := range index {
		if candidate == kind {
			n++
		}
	}
	return n
}

// Packs returns a bytewise-canonical, detached copy of accepted pack refs.
func (r *PackMetadataRegistry) Packs() []PackRef {
	if r == nil {
		return nil
	}
	return append([]PackRef(nil), r.packs...)
}

// HasDefinition reports whether ref is an exact exported definition or unit.
func (r *PackMetadataRegistry) HasDefinition(ref DefinitionRef) bool {
	if r == nil || ref.Validate() != nil {
		return false
	}
	_, ok := r.definitions[ref]
	return ok
}

// CanonicalUnit returns the exact canonical unit for a quantity field.
func (r *PackMetadataRegistry) CanonicalUnit(field DefinitionRef) (DefinitionRef, bool) {
	if r == nil || field.Validate() != nil {
		return DefinitionRef{}, false
	}
	metadata, ok := r.fields[field]
	if !ok || metadata.CanonicalUnit == nil {
		return DefinitionRef{}, false
	}
	return *metadata.CanonicalUnit, true
}

// ServiceOwnsCapability reports exact same-pack capability ownership.
func (r *PackMetadataRegistry) ServiceOwnsCapability(service, capability DefinitionRef) bool {
	if r == nil || service.Validate() != nil || capability.Validate() != nil {
		return false
	}
	owner, ok := r.caps[capability]
	return ok && owner == service
}

// FieldMatches reports whether a field and capability belong to the service and
// use that service's exact fact-key dimension.
func (r *PackMetadataRegistry) FieldMatches(field, service, capability DefinitionRef) bool {
	if r == nil || field.Validate() != nil || service.Validate() != nil || capability.Validate() != nil || !r.ServiceOwnsCapability(service, capability) {
		return false
	}
	f, fieldOK := r.fields[field]
	s, serviceOK := r.services[service]
	return fieldOK && serviceOK && f.Dimension == s.FactKeyDimension
}

// OperationMatches reports whether all five references are one exact exported
// operation shape.
func (r *PackMetadataRegistry) OperationMatches(operation, capability, service, argument, effect DefinitionRef) bool {
	if r == nil {
		return false
	}
	for _, ref := range []DefinitionRef{operation, capability, service, argument, effect} {
		if ref.Validate() != nil {
			return false
		}
	}
	_, ok := r.operations[operationMetadataKey{operation, capability, service, argument, effect}]
	return ok
}

func clonePackMetadata(in PackMetadata) PackMetadata {
	out := in
	out.Units = append([]DefinitionRef(nil), in.Units...)
	out.Fields = make([]FieldMetadata, len(in.Fields))
	for i, field := range in.Fields {
		out.Fields[i] = cloneFieldMetadata(field)
	}
	out.Services = append([]ServiceMetadata(nil), in.Services...)
	out.Capabilities = append([]CapabilityMetadata(nil), in.Capabilities...)
	out.Operations = append([]OperationMetadata(nil), in.Operations...)
	return out
}

func cloneFieldMetadata(in FieldMetadata) FieldMetadata {
	out := in
	if in.CanonicalUnit != nil {
		unit := *in.CanonicalUnit
		out.CanonicalUnit = &unit
	}
	return out
}

func comparePackMetadataRef(a, b PackRef) int {
	if c := bytes.Compare([]byte(a.ID), []byte(b.ID)); c != 0 {
		return c
	}
	return bytes.Compare([]byte(a.Version), []byte(b.Version))
}
