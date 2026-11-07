package semreg

// RetainedCapacity describes committed reservation debt, independently of
// freshness evaluation and qualification. DowngradeBlocked means the former
// 32-record implementation cannot represent the retained history.
type RetainedCapacity struct {
	Retained          int
	CurrentObserved   int
	FenceReservations int
	RecoveryBlocked   bool
	DowngradeBlocked  bool
}

func retainedCapacity(s Snapshot) RetainedCapacity {
	result := RetainedCapacity{Retained: len(s.Retained), DowngradeBlocked: len(s.Retained) > 32}
	tuples := make(map[cursorKey]struct{})
	for _, f := range s.Fences {
		tuples[cursorKey{f.SourceID, f.SourceEpochID, f.DriverGeneration}] = struct{}{}
	}
	current := make(map[NativeBindingID]bool)
	for _, b := range s.Bindings {
		if b.State == BindingCurrent {
			current[b.BindingID] = true
			tuples[cursorKey{b.SourceID, b.SourceEpochID, b.DriverGeneration}] = struct{}{}
		}
	}
	// A live cursor can be fenced even when its generation has no bindings yet.
	liveSources := make(map[sourceKey]bool)
	for _, source := range s.Sources {
		if source.State == SourceCurrent {
			liveSources[sourceKey{source.SourceID, source.SourceEpochID}] = true
		}
	}
	for _, c := range s.Cursors {
		if !c.Fenced && liveSources[sourceKey{c.SourceID, c.SourceEpochID}] {
			tuples[cursorKey{c.SourceID, c.SourceEpochID, c.DriverGeneration}] = struct{}{}
		}
	}
	for _, e := range s.Facts {
		for _, c := range e.Candidates {
			if c.Quality.Assertion == AssertionObserved && c.BindingID != nil && current[*c.BindingID] {
				result.CurrentObserved++
			}
		}
	}
	result.FenceReservations = len(tuples)
	result.RecoveryBlocked = result.Retained+result.CurrentObserved > maxSnapshotRetained || result.FenceReservations > maxSnapshotFences
	return result
}

func retainedAdmissionError(s Snapshot) error {
	if retainedCapacity(s).RecoveryBlocked {
		return errID(BoundsExceeded, "retained admission reservation")
	}
	return nil
}

// RetainedCapacity returns a point-in-time diagnosis. It is not an admission
// token: Apply always recomputes capacity under its publication lock.
func (k *PublicationKernel) RetainedCapacity() (RetainedCapacity, error) {
	if k == nil {
		return RetainedCapacity{}, errID(InvalidValue, "publication kernel")
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.current == nil {
		return RetainedCapacity{}, nil
	}
	return retainedCapacity(*k.current), nil
}
