//go:build !linux

package tui

import "io"

func EnableRawInput(io.Reader) (func() error, bool, error) {
	return func() error { return nil }, false, nil
}
