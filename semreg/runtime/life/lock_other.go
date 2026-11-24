//go:build !unix

package life

import "errors"

// storeLock is the exclusive cross-process lock of a control store.
type storeLock struct{}

func acquireStoreLock(string) (*storeLock, error) {
	return nil, errors.New("store lock unavailable on this platform")
}

func (*storeLock) release() {}
