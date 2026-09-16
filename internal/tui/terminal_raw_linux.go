//go:build linux

package tui

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// EnableRawInput enables character-at-a-time input for a terminal. Non-tty
// readers are left untouched so scripted and piped input retain line behavior.
func EnableRawInput(r io.Reader) (restore func() error, enabled bool, err error) {
	file, ok := r.(*os.File)
	if !ok {
		return func() error { return nil }, false, nil
	}
	original, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	if err != nil {
		return func() error { return nil }, false, nil
	}
	raw := *original
	raw.Lflag &^= unix.ICANON | unix.ECHO | unix.ECHONL
	raw.Iflag &^= unix.ICRNL | unix.INLCR | unix.IXON
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, &raw); err != nil {
		return nil, false, err
	}
	return func() error { return unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, original) }, true, nil
}
