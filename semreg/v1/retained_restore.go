package semreg

// AcceptedPublication is one previously accepted log entry, including the exact
// explicit evaluation point used for that publication. The persistence owner
// must establish that the log is trusted and complete before calling restore.
type AcceptedPublication struct {
	Batch                PublicationBatch
	PublicationMonotonic MonotonicPoint
}

// RestoreAccepted atomically replaces an empty kernel from its complete accepted
// batch log. It performs ordinary structural, ownership, digest, lifecycle,
// sequence and pack validation at every intermediate state, but does not apply
// the newer reservation admission rule to historical acceptances. This preserves
// legacy debt and reconstructs current-tuple replay results and cursors exactly.
// New publication must use Apply, which cannot bypass admission. Existing kernels
// cannot be reset through restore; restore into a detached empty kernel instead.
func (k *PublicationKernel) RestoreAccepted(log []AcceptedPublication) error {
	if k == nil {
		return errID(InvalidValue, "publication kernel")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.current != nil {
		return errID(InvalidValue, "restore requires empty publication kernel")
	}
	if len(log) == 0 {
		return errID(MissingMember, "accepted publication log")
	}
	working := &PublicationKernel{asset: k.asset, registry: k.registry, replays: make(map[cursorKey]publicationResult)}
	for _, entry := range log {
		if _, _, err := working.applyLocked(entry.Batch, entry.PublicationMonotonic, false); err != nil {
			return err
		}
	}
	k.current = working.current
	k.canonical = working.canonical
	k.replays = working.replays
	return nil
}
