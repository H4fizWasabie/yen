package auth

import (
	"os"
	"path/filepath"
	"syscall"
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
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	// ponytail: one credential-file lock; split per provider only if contention is measured.
	if err := syscall.Flock(int(lock.Fd()), mode); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn()
}
