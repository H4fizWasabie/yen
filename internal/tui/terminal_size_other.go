//go:build !linux

package tui

import "io"

func terminalSize(io.Writer) (int, int) { return 1 << 20, 1 << 20 }
