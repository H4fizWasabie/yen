//go:build linux

package tui

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func terminalSize(w io.Writer) (int, int) {
	file, ok := w.(*os.File)
	if !ok {
		return 1 << 20, 1 << 20
	}
	size, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
	if err != nil || size.Col == 0 || size.Row == 0 {
		return 1 << 20, 1 << 20
	}
	return int(size.Col), int(size.Row)
}
