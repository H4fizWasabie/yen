package session

import (
	"path/filepath"
	"sync"
)

var sessionPathLocks sync.Map

// WithPathLock serializes in-process mutations of one session file across
// independently opened Session handles.
func WithPathLock(path string, fn func() error) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	value, _ := sessionPathLocks.LoadOrStore(path, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	return fn()
}
