//go:build darwin

package tui

import (
	"errors"
	"os"
)

func openPTY() (master, slave *os.File, err error) {
	return nil, nil, errors.New("live terminal acceptance is unavailable on darwin")
}
