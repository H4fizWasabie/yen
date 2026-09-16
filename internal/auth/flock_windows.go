//go:build windows

package auth

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func (s *Store) withFileLock(exclusive bool, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	var flags uint32
	if exclusive {
		flags = windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	overlap := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(lock.Fd()), flags, 0, 1, 0, overlap); err != nil {
		return err
	}
	defer windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, overlap)
	return fn()
}
