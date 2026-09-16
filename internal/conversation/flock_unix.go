//go:build !windows

package conversation

import (
	"os"
	"syscall"
)

func flock(file *os.File) error   { return syscall.Flock(int(file.Fd()), syscall.LOCK_EX) }
func funlock(file *os.File) error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
