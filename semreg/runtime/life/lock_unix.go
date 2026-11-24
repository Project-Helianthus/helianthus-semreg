//go:build unix

package life

import (
	"fmt"
	"os"
	"syscall"
)

// storeLock is the exclusive cross-process lock of a control store.
type storeLock struct{ f *os.File }

func acquireStoreLock(path string) (*storeLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open store lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("acquire store lock: %w", err)
	}
	return &storeLock{f: f}, nil
}

func (l *storeLock) release() {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}
