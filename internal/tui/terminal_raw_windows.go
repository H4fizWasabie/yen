//go:build windows

package tui

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

func EnableRawInput(r io.Reader) (func() error, bool, error) {
	file, ok := r.(*os.File)
	if !ok {
		return func() error { return nil }, false, nil
	}
	handle := windows.Handle(file.Fd())
	var original uint32
	if err := windows.GetConsoleMode(handle, &original); err != nil {
		return func() error { return nil }, false, nil
	}
	raw := original &^ (windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT)
	raw |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(handle, raw); err != nil {
		return nil, false, err
	}
	return func() error { return windows.SetConsoleMode(handle, original) }, true, nil
}
