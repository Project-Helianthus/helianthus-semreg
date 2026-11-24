package life

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// controlFileName is the control-record file under the installation's state
// directory.
const controlFileName = "runtime-control.json"

// fsHooks are the filesystem operations of one atomic write cycle, in this
// order: WriteFile, FsyncFile, Rename, FsyncDir; Unlink only after a failed
// cycle.
type fsHooks interface {
	WriteFile(path string, data []byte, perm uint32) error
	FsyncFile(path string) error
	Rename(oldpath, newpath string) error
	FsyncDir(path string) error
	Unlink(path string) error
}

type osHooks struct{}

func (osHooks) WriteFile(path string, data []byte, perm uint32) error {
	return os.WriteFile(path, data, os.FileMode(perm))
}

func (osHooks) FsyncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

func (osHooks) Rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

func (osHooks) FsyncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

func (osHooks) Unlink(path string) error { return os.Remove(path) }

type filePersister struct {
	path  string
	hooks fsHooks
}

func (f *filePersister) read() ([]byte, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// commit executes the atomic temp-and-rename sequence. The parent-directory
// fsync is required: an error after the rename is ambiguous persistence.
func (f *filePersister) commit(data []byte) (bool, error) {
	dir := filepath.Dir(f.path)
	temp := filepath.Join(dir, filepath.Base(f.path)+".tmp")
	if err := f.hooks.WriteFile(temp, data, 0o644); err != nil {
		_ = f.hooks.Unlink(temp)
		return false, err
	}
	if err := f.hooks.FsyncFile(temp); err != nil {
		_ = f.hooks.Unlink(temp)
		return false, err
	}
	if err := f.hooks.Rename(temp, f.path); err != nil {
		_ = f.hooks.Unlink(temp)
		return false, err
	}
	if err := f.hooks.FsyncDir(dir); err != nil {
		return true, err
	}
	return true, nil
}

func (f *filePersister) syncDir() error { return f.hooks.FsyncDir(filepath.Dir(f.path)) }

// FileStore is the durable control store. It persists the control records
// under the installation's state directory; every write is durable before it
// returns and a read returns committed state only. It holds the exclusive
// cross-process lock of the directory; a second opener that cannot take the
// lock reads the last committed state read-only and writes nothing.
type FileStore struct {
	*controlStore
	persister *filePersister

	lockMu sync.Mutex
	lock   *storeLock
	// lockErr is the reason the exclusive lock was not taken, nil when held.
	lockErr error
}

// OpenFileStore opens the durable control store under dir and takes its
// exclusive lock. The directory must exist.
func OpenFileStore(dir string) (*FileStore, error) {
	return openFileStore(dir, osHooks{}, acquireStoreLock)
}

func openFileStore(dir string, hooks fsHooks, acquire func(path string) (*storeLock, error)) (*FileStore, error) {
	if dir == "" {
		return nil, errors.New("life: a state directory is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("life: state directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("life: state directory %q is not a directory", dir)
	}
	p := &filePersister{path: filepath.Join(dir, controlFileName), hooks: hooks}
	c := newControlStore(p)
	s := &FileStore{controlStore: c, persister: p}
	lock, lockErr := acquire(p.path + ".lock")
	if lockErr != nil {
		c.loadReadOnly(lockErr)
		s.lockErr = lockErr
		return s, nil
	}
	s.lock = lock
	c.load()
	return s, nil
}

// Exclusive reports that this process holds the store lock.
func (s *FileStore) Exclusive() bool {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	return s.lock != nil
}

// Close ends the store's use. The lock stays held until Release, so a native
// owner that can still publish is never joined by a second host.
func (s *FileStore) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// Release relinquishes the store lock.
func (s *FileStore) Release() {
	s.lockMu.Lock()
	lock := s.lock
	s.lock = nil
	s.lockMu.Unlock()
	if lock != nil {
		lock.release()
	}
}
