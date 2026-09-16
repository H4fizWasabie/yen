//go:build windows

package tui

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

func terminalSize(w io.Writer) (int, int) {
	file, ok := w.(*os.File)
	if !ok {
		return 1 << 20, 1 << 20
	}
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(file.Fd()), &info); err != nil {
		return 1 << 20, 1 << 20
	}
	return int(info.Window.Right - info.Window.Left + 1), int(info.Window.Bottom - info.Window.Top + 1)
}
