package projection

import (
	"sync"

	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/graph"
)

// Project constructs a detached projection report from one supplied immutable
// snapshot. lens is the optional lens result the resource and relation
// citations were read from; the result is not part of the report (TGT-5).
// Pass none or one. Project validates only semantic report accounting: it
// creates no facts, capability instances, authority, intent, binding, route,
// or retained state.
func Project(snapshot semreg.Snapshot, manifest ProjectionManifest, requested []RequestedItem, dispositions []ProjectionDisposition, causal *semreg.CausalContext, lens ...*graph.LensResult) (ProjectionReport, error) {
	source, err := suppliedLens(lens)
	if err != nil {
		return ProjectionReport{}, err
	}
	report := ProjectionReport{
		Contract:     ContractProjectionV1,
		Manifest:     manifest,
		SnapshotID:   snapshot.SnapshotID,
		Revisions:    snapshot.Revisions,
		Requested:    requested,
		Dispositions: dispositions,
		Causal:       causal,
	}
	if err := ranked(snapshot.Validate(), report.Validate(), sourceResolutionError(snapshot, report.Dispositions), citationError(snapshot, report, source)); err != nil {
		return ProjectionReport{}, err
	}
	return detached(report)
}

// ValidateReport verifies that a report is bound exactly to the supplied
// immutable snapshot, returning a detached report on success. lens is the
// optional lens result the report's citations were read from.
func ValidateReport(snapshot semreg.Snapshot, report ProjectionReport, lens ...*graph.LensResult) (ProjectionReport, error) {
	source, err := suppliedLens(lens)
	if err != nil {
		return ProjectionReport{}, err
	}
	errs := []error{snapshot.Validate(), report.Validate()}
	if snapshot.SnapshotID != report.SnapshotID {
		errs = append(errs, errorf(semreg.DanglingReference, "projection snapshot"))
	} else if snapshot.Revisions != report.Revisions {
		errs = append(errs, errorf(semreg.RevisionConflict, "projection revisions"))
	}
	errs = append(errs, sourceResolutionError(snapshot, report.Dispositions), citationError(snapshot, report, source))
	if err := ranked(errs...); err != nil {
		return ProjectionReport{}, err
	}
	return detached(report)
}

func suppliedLens(lens []*graph.LensResult) (*graph.LensResult, error) {
	switch len(lens) {
	case 0:
		return nil, nil
	case 1:
		return lens[0], nil
	default:
		return nil, errorf(semreg.InvalidValue, "projection lens")
	}
}

// citationError resolves resource and relation citations against the lens
// result. A cited relation that is not coherent in that result is
// projection_incomplete (G-NEG-TGT-1). Citations or a named lens with no
// result are incomplete: the report cannot show what it was made from.
func citationError(snapshot semreg.Snapshot, report ProjectionReport, source *graph.LensResult) error {
	if source == nil {
		if report.Manifest.Lens != nil || hasCitations(report) {
			return errorf(semreg.ProjectionIncomplete, "projection lens")
		}
		return nil
	}
	var errs []error
	errs = append(errs, source.Validate())
	if report.Manifest.Lens == nil || *report.Manifest.Lens != source.Lens {
		errs = append(errs, errorf(semreg.ProjectionIncomplete, "projection lens"))
	}
	catalog, err := graph.Embedded()
	if err != nil {
		errs = append(errs, err)
	} else if source.Catalog != catalog.Digest() {
		errs = append(errs, errorf(semreg.DigestMismatch, "projection lens catalog"))
	}
	if !cutHoldsSnapshot(source.Cut, snapshot) {
		errs = append(errs, errorf(semreg.RevisionConflict, "projection cut"))
	}
	errs = append(errs, citationResolutionError(source, report.Dispositions))
	return ranked(errs...)
}

func hasCitations(report ProjectionReport) bool {
	for _, disposition := range report.Dispositions {
		if len(disposition.Resources) > 0 || len(disposition.Relations) > 0 {
			return true
		}
	}
	return false
}

func cutHoldsSnapshot(cut graph.CutRef, snapshot semreg.Snapshot) bool {
	for _, member := range cut.Members {
		if member.AssetID == snapshot.AssetID && member.SnapshotID == snapshot.SnapshotID {
			return true
		}
	}
	return false
}

func citationResolutionError(source *graph.LensResult, dispositions []ProjectionDisposition) error {
	resources := make(map[semreg.ResourceID]struct{}, len(source.Resources))
	for _, row := range source.Resources {
		resources[row.Resource.ResourceID] = struct{}{}
	}
	coherent := make(map[string]bool, len(source.Relations))
	var errs []error
	for _, relation := range source.Relations {
		raw, err := semreg.CanonicalJSON(semreg.RelationKey{Definition: relation.Definition, From: relation.From, To: relation.To})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if relation.Coherence == graph.CoherenceCoherent {
			coherent[string(raw)] = true
		} else if !coherent[string(raw)] {
			coherent[string(raw)] = false
		}
	}
	for _, disposition := range dispositions {
		for _, id := range disposition.Resources {
			if id.Validate() != nil {
				continue
			}
			if _, ok := resources[id]; !ok {
				errs = append(errs, errorf(semreg.DanglingReference, "projection resource"))
			}
		}
		for _, key := range disposition.Relations {
			raw, err := semreg.CanonicalJSON(key)
			if err != nil {
				continue
			}
			if !coherent[string(raw)] {
				errs = append(errs, errorf(semreg.ProjectionIncomplete, "projection relation"))
			}
		}
	}
	return ranked(errs...)
}

var projectionCatalogOnce struct {
	sync.Once
	lenses    map[semreg.DefinitionID]struct{}
	relations map[semreg.DefinitionID]struct{}
	err       error
}

func projectionCatalog() (map[semreg.DefinitionID]struct{}, map[semreg.DefinitionID]struct{}, error) {
	projectionCatalogOnce.Do(func() {
		catalog, err := graph.Embedded()
		if err != nil {
			projectionCatalogOnce.err = err
			return
		}
		lenses := catalog.Lenses()
		projectionCatalogOnce.lenses = make(map[semreg.DefinitionID]struct{}, len(lenses))
		for _, lens := range lenses {
			projectionCatalogOnce.lenses[lens.ID] = struct{}{}
		}
		projectionCatalogOnce.relations = map[semreg.DefinitionID]struct{}{}
		for _, element := range catalog.Elements() {
			if element.ElementKind == graph.KindRelation && element.Disposition == graph.DispositionMember {
				projectionCatalogOnce.relations[element.Ref.ID] = struct{}{}
			}
		}
	})
	return projectionCatalogOnce.lenses, projectionCatalogOnce.relations, projectionCatalogOnce.err
}

func knownLens(id semreg.DefinitionID) (bool, error) {
	lenses, _, err := projectionCatalog()
	if err != nil {
		return false, err
	}
	_, ok := lenses[id]
	return ok, nil
}

func relationDefinitionError(kind ItemKind, id semreg.DefinitionID) error {
	if kind != ItemRelation || id.Validate() != nil {
		return nil
	}
	_, relations, err := projectionCatalog()
	if err != nil {
		return err
	}
	if _, ok := relations[id]; !ok {
		return errorf(semreg.MembershipMissing, "projection relation definition")
	}
	return nil
}

// sourceResolutionError resolves only complete, valid canonical FactKeys. An
// invalid key retains its independently knowable member error and never gains a
// fallback identity; an absent valid key is a snapshot-bound dangling reference.
func sourceResolutionError(snapshot semreg.Snapshot, dispositions []ProjectionDisposition) error {
	facts := make(map[string]struct{}, len(snapshot.Facts))
	for _, envelope := range snapshot.Facts {
		canonical, err := semreg.CanonicalJSON(envelope.Key)
		if err == nil {
			facts[string(canonical)] = struct{}{}
		}
	}
	for _, disposition := range dispositions {
		for _, key := range disposition.SourceKeys {
			canonical, err := semreg.CanonicalJSON(key)
			if err != nil {
				continue
			}
			if _, ok := facts[string(canonical)]; !ok {
				return errorf(semreg.DanglingReference, "projection source key")
			}
		}
	}
	return nil
}

func detached(report ProjectionReport) (ProjectionReport, error) {
	raw, err := semreg.CanonicalJSON(report)
	if err != nil {
		return ProjectionReport{}, err
	}
	return semreg.Decode[ProjectionReport](raw)
}
