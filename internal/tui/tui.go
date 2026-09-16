package tui

import (
	"fmt"
	"io"
	"strings"
)

// Screen is the small presentation model shared by the interactive CLI.
type Screen struct {
	Scrollback []string
	Status     string
	Input      string
}

func (s Screen) Render(w io.Writer) error {
	if _, err := io.WriteString(w, "\x1b[2J\x1b[H"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Scrollback"); err != nil {
		return err
	}
	for _, entry := range s.Scrollback {
		if _, err := fmt.Fprintln(w, entry); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\nStatus: %s\nInput\n> %s", s.Status, strings.ReplaceAll(s.Input, "\n", " ")); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}
