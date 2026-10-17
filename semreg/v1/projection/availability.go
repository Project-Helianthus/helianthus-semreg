package projection

import (
	"bytes"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/evse"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/infrastructure"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/pv"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/storage"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/packs/thermal"
)

// PackAvailabilityView is a pre-stable, derived read view, not a serialized
// contract or an operation admission result. Its identity includes evaluation
// time as well as the complete immutable publication identity.
type PackAvailabilityView struct {
	AssetID          semreg.AssetID
	SnapshotID       semreg.SnapshotID
	Revisions        semreg.RevisionVector
	Context          semreg.EvaluationContext
	EvaluationDigest semreg.Digest
	Packs            []PackAvailability
}

// PackAvailability preserves current instances and facts separately from
// historical observations. An empty row only declares metadata; it makes no
// claim about native support or availability. There is no aggregate available
// flag and no inferred capability instance.
type PackAvailability struct {
	Pack         semreg.PackRef
	Services     []semreg.ServiceInstance
	Capabilities []semreg.CapabilityInstance
	Facts        []AvailabilityFact
	Retained     []semreg.EvaluatedRetainedObservation
}

// AvailabilityFact keeps all stored candidates, conflicts and quality reasons
// alongside their exact time-evaluated revisions. Withholding belongs in the
// existing ProjectionReport and never rewrites these source records.
type AvailabilityFact struct {
	Envelope   semreg.FactEnvelope
	Evaluation []semreg.EvaluatedFact
}

// DerivePackAvailability derives exactly the five shipped packs from explicit
// immutable inputs. It has no clock, publication, catalog or admission effects.
// Other packs remain outside this view. All returned collections are detached.
func DerivePackAvailability(metadata *semreg.PackMetadataRegistry, snapshot semreg.Snapshot, evaluation semreg.EvaluationView) (PackAvailabilityView, error) {
	if metadata == nil {
		return PackAvailabilityView{}, errorf(semreg.MissingMember, "availability metadata")
	}
	if err := ranked(snapshot.Validate(), evaluation.Validate()); err != nil {
		return PackAvailabilityView{}, err
	}
	if snapshot.SnapshotID != evaluation.SnapshotID || snapshot.Revisions != evaluation.Revisions {
		return PackAvailabilityView{}, errorf(semreg.RevisionConflict, "availability snapshot evaluation")
	}
	expected, err := semreg.EvaluateSnapshot(snapshot, evaluation.Context)
	if err != nil {
		return PackAvailabilityView{}, err
	}
	if err := availabilityEvaluationMatches(expected, evaluation); err != nil {
		return PackAvailabilityView{}, err
	}

	// The existing wire codec supplies deep copies while preserving canonical
	// nested ordering, exact values, lineage and all optional time information.
	raw, err := semreg.CanonicalJSON(snapshot)
	if err != nil {
		return PackAvailabilityView{}, err
	}
	snapshot, err = semreg.Decode[semreg.Snapshot](raw)
	if err != nil {
		return PackAvailabilityView{}, err
	}
	raw, err = semreg.CanonicalJSON(evaluation)
	if err != nil {
		return PackAvailabilityView{}, err
	}
	evaluation, err = semreg.Decode[semreg.EvaluationView](raw)
	if err != nil {
		return PackAvailabilityView{}, err
	}

	validators := map[semreg.PackRef]semreg.PackValidator{}
	for _, validator := range []semreg.PackValidator{thermal.New(), pv.New(), storage.New(), evse.New(), infrastructure.New()} {
		validators[validator.Pack()] = validator
	}
	result := PackAvailabilityView{AssetID: snapshot.AssetID, SnapshotID: snapshot.SnapshotID, Revisions: snapshot.Revisions, Context: evaluation.Context, EvaluationDigest: evaluation.EvaluationDigest, Packs: []PackAvailability{}}
	rows := map[semreg.PackRef]int{}
	for _, pack := range metadata.Packs() {
		if _, ok := validators[pack]; !ok {
			continue
		}
		rows[pack] = len(result.Packs)
		result.Packs = append(result.Packs, PackAvailability{Pack: pack, Services: []semreg.ServiceInstance{}, Capabilities: []semreg.CapabilityInstance{}, Facts: []AvailabilityFact{}, Retained: []semreg.EvaluatedRetainedObservation{}})
	}
	if len(rows) != len(validators) {
		return PackAvailabilityView{}, errorf(semreg.DefinitionOwnerMissing, "availability five-pack metadata")
	}
	services := map[semreg.ServiceInstanceID]semreg.ServiceInstance{}
	for _, service := range snapshot.Services {
		services[service.InstanceID] = service
		if index, ok := rows[service.Definition.Pack]; ok {
			if !metadata.HasDefinition(service.Definition) {
				return PackAvailabilityView{}, errorf(semreg.DefinitionOwnerMissing, "availability service")
			}
			if err := validators[service.Definition.Pack].ValidateService(service); err != nil {
				return PackAvailabilityView{}, err
			}
			result.Packs[index].Services = append(result.Packs[index].Services, service)
		}
	}
	for _, capability := range snapshot.Capabilities {
		if index, ok := rows[capability.Definition.Pack]; ok {
			if !metadata.HasDefinition(capability.Definition) || !metadata.ServiceOwnsCapability(services[capability.ServiceInstance].Definition, capability.Definition) {
				return PackAvailabilityView{}, errorf(semreg.DefinitionOwnerMissing, "availability capability service")
			}
			validator := validators[capability.Definition.Pack]
			// Actionability is source state in this read view. Validate structure on
			// the already detached snapshot copy with only those axes normalized;
			// the returned capability retains its original state and evidence.
			structural := capability
			structural.Qualification = semreg.QualificationQualified
			structural.Availability = semreg.AvailabilityAvailable
			if err := validator.ValidateCapability(structural); err != nil {
				return PackAvailabilityView{}, err
			}
			result.Packs[index].Capabilities = append(result.Packs[index].Capabilities, capability)
		}
	}
	evaluated := map[semreg.CandidateID]semreg.EvaluatedFact{}
	for _, fact := range evaluation.Facts {
		evaluated[fact.CandidateID] = fact
	}
	for _, envelope := range snapshot.Facts {
		pack := semreg.PackRef{ID: envelope.Key.PackID, Version: envelope.Key.PackVersion}
		if index, ok := rows[pack]; ok {
			row := AvailabilityFact{Envelope: envelope, Evaluation: []semreg.EvaluatedFact{}}
			for _, candidate := range envelope.Candidates {
				if err := availabilityFactValid(metadata, validators[pack], candidate); err != nil {
					return PackAvailabilityView{}, err
				}
				row.Evaluation = append(row.Evaluation, evaluated[candidate.CandidateID])
			}
			result.Packs[index].Facts = append(result.Packs[index].Facts, row)
		}
	}
	for _, retained := range evaluation.Retained {
		key := retained.Observation.Candidate.Key
		pack := semreg.PackRef{ID: key.PackID, Version: key.PackVersion}
		if index, ok := rows[pack]; ok {
			if err := availabilityFactValid(metadata, validators[pack], retained.Observation.Candidate); err != nil {
				return PackAvailabilityView{}, err
			}
			result.Packs[index].Retained = append(result.Packs[index].Retained, retained)
		}
	}
	return result, nil
}

func availabilityFactValid(metadata *semreg.PackMetadataRegistry, validator semreg.PackValidator, candidate semreg.FactCandidate) error {
	// Resolve field versions from the shipped metadata, never from an ID prefix or
	// an assumed equality between pack and definition versions.
	if err := candidate.Key.Validate(); err != nil {
		return err
	}
	for _, field := range validator.(semreg.PackMetadataProvider).Metadata().Fields {
		if field.Ref.ID != candidate.Key.FactID {
			continue
		}
		if !metadata.HasDefinition(field.Ref) {
			return errorf(semreg.DefinitionOwnerMissing, "availability field")
		}
		// All five shipped packs require one text dimension. Use their field
		// metadata for its ID even when a tombstone has no value to validate.
		dimensions := candidate.Key.Dimensions
		if len(dimensions) != 1 || dimensions[0].ID != field.Dimension.ID || dimensions[0].Value.Kind != semreg.ValueText || dimensions[0].Value.Text == nil {
			return errorf(semreg.InvalidValue, "availability fact dimensions")
		}
		if candidate.Value == nil {
			return nil
		}
		return validator.ValidateFact(candidate.Key, candidate.Value)
	}
	return errorf(semreg.DefinitionOwnerMissing, "availability field")
}

func availabilityEvaluationMatches(expected, supplied semreg.EvaluationView) error {
	facts := map[semreg.CandidateID]semreg.EvaluatedFact{}
	for _, fact := range expected.Facts {
		facts[fact.CandidateID] = fact
	}
	if len(expected.Facts) != len(supplied.Facts) {
		return errorf(semreg.DanglingReference, "availability evaluated candidates")
	}
	for _, fact := range supplied.Facts {
		current, ok := facts[fact.CandidateID]
		if !ok {
			return errorf(semreg.DanglingReference, "availability evaluated candidate")
		}
		if current.CandidateRevision != fact.CandidateRevision {
			return errorf(semreg.RevisionConflict, "availability evaluated revision")
		}
	}
	a, err := semreg.CanonicalJSON(expected)
	if err != nil {
		return err
	}
	b, err := semreg.CanonicalJSON(supplied)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return errorf(semreg.InvalidValue, "availability evaluation differs from kernel")
	}
	return nil
}
