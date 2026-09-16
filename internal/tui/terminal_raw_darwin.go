//go:build darwin

package tui

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func EnableRawInput(r io.Reader) (func() error, bool, error) {
	file, ok := r.(*os.File)
	if !ok {
		return func() error { return nil }, false, nil
	}
	original, err := unix.IoctlGetTermios(int(file.Fd()), unix.TIOCGETA)
	if err != nil {
		return func() error { return nil }, false, nil
	}
	raw := *original
	raw.Lflag &^= unix.ICANON | unix.ECHO | unix.ECHONL
	raw.Iflag &^= unix.ICRNL | unix.INLCR | unix.IXON
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(int(file.Fd()), unix.TIOCSETA, &raw); err != nil {
		return nil, false, err
	}
	return func() error { return unix.IoctlSetTermios(int(file.Fd()), unix.TIOCSETA, original) }, true, nil
}
