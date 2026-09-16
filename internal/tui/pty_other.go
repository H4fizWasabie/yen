//go:build !linux && !darwin

package tui

import (
	"errors"
	"os"
)

func openPTY() (master, slave *os.File, err error) {
	return nil, nil, errors.New("live terminal acceptance is only supported on linux")
}
