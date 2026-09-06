package projection

import (
	semreg "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
)

// Project constructs a detached projection report from one supplied immutable
// snapshot. It validates only semantic report accounting: it creates no facts,
// capability instances, authority, intent, binding, route, or retained state.
func Project(snapshot semreg.Snapshot, manifest ProjectionManifest, requested []RequestedItem, dispositions []ProjectionDisposition, causal *semreg.CausalContext) (ProjectionReport, error) {
	report := ProjectionReport{
		Contract:     ContractProjectionV1,
		Manifest:     manifest,
		SnapshotID:   snapshot.SnapshotID,
		Revisions:    snapshot.Revisions,
		Requested:    requested,
		Dispositions: dispositions,
		Causal:       causal,
	}
	if err := ranked(snapshot.Validate(), report.Validate(), sourceResolutionError(snapshot, report.Dispositions)); err != nil {
		return ProjectionReport{}, err
	}
	return detached(report)
}

// ValidateReport verifies that a report is bound exactly to the supplied
// immutable snapshot, returning a detached report on success.
func ValidateReport(snapshot semreg.Snapshot, report ProjectionReport) (ProjectionReport, error) {
	errs := []error{snapshot.Validate(), report.Validate()}
	if snapshot.SnapshotID != report.SnapshotID {
		errs = append(errs, errorf(semreg.DanglingReference, "projection snapshot"))
	} else if snapshot.Revisions != report.Revisions {
		errs = append(errs, errorf(semreg.RevisionConflict, "projection revisions"))
	}
	errs = append(errs, sourceResolutionError(snapshot, report.Dispositions))
	if err := ranked(errs...); err != nil {
		return ProjectionReport{}, err
	}
	return detached(report)
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
