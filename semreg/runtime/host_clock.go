package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
)

// processClock is the ordinary host's default installation clock: wall time
// from the process, monotonic samples since the clock was made, and anchors
// that only the process that made them can verify. Continuity across a restart
// is never inferred from a civil date: Elapsed fails for an anchor another
// process made.
type processClock struct {
	start time.Time
	token string
}

var _ life.Clock = (*processClock)(nil)

// newProcessClock returns the default process clock.
func newProcessClock() (*processClock, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	return &processClock{start: time.Now(), token: hex.EncodeToString(raw[:])}, nil
}

// Now is the process wall time.
func (c *processClock) Now() time.Time { return time.Now() }

// Sample is the monotonic time since the clock was made, in nanoseconds.
func (c *processClock) Sample() (uint64, error) {
	return uint64(time.Since(c.start).Nanoseconds()), nil
}

type processAnchor struct {
	Token  string `json:"token"`
	Sample uint64 `json:"sample"`
}

// Anchor records a sample with this process's token.
func (c *processClock) Anchor(sample uint64) ([]byte, error) {
	return json.Marshal(processAnchor{Token: c.token, Sample: sample})
}

// Elapsed returns the time since the anchor, or an error when the anchor is
// foreign or later than the sample.
func (c *processClock) Elapsed(anchor []byte, sample uint64) (uint64, error) {
	var a processAnchor
	if err := json.Unmarshal(anchor, &a); err != nil {
		return 0, err
	}
	if a.Token != c.token {
		return 0, errors.New("runtime: the anchor belongs to another process, so elapsed time cannot be verified")
	}
	if sample < a.Sample {
		return 0, errors.New("runtime: the sample precedes the anchor")
	}
	return sample - a.Sample, nil
}
