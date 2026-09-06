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
	if err := ranked(snapshot.Validate(), report.Validate()); err != nil {
		return ProjectionReport{}, err
	}
	return detached(report)
}

// ValidateReport verifies that a report is bound exactly to the supplied
// immutable snapshot, returning a detached report on success.
func ValidateReport(snapshot semreg.Snapshot, report ProjectionReport) (ProjectionReport, error) {
	errs := []error{snapshot.Validate(), report.Validate()}
	if snapshot.SnapshotID != report.SnapshotID || snapshot.Revisions != report.Revisions {
		errs = append(errs, errorf(semreg.DanglingReference, "projection snapshot"))
	}
	if err := ranked(errs...); err != nil {
		return ProjectionReport{}, err
	}
	return detached(report)
}

func detached(report ProjectionReport) (ProjectionReport, error) {
	raw, err := semreg.CanonicalJSON(report)
	if err != nil {
		return ProjectionReport{}, err
	}
	return semreg.Decode[ProjectionReport](raw)
}
