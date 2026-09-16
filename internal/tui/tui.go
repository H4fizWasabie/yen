package tui

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
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

// Select presents numbered options and accepts a number, j/k, or arrow keys.
// A blank line or q cancels without changing the current selection.
func Select(r *bufio.Reader, w io.Writer, title string, options []string) (int, error) {
	if _, err := fmt.Fprintln(w, title); err != nil {
		return -1, err
	}
	for i, option := range options {
		if _, err := fmt.Fprintf(w, "%d) %s\n", i+1, option); err != nil {
			return -1, err
		}
	}
	selected := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil && len(line) == 0 {
			return -1, err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "q") || line == "\x1b" {
			return -1, nil
		}
		if n, parseErr := strconv.Atoi(line); parseErr == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		if line == "j" || line == "\x1b[B" {
			if selected < len(options)-1 {
				selected++
			}
			continue
		}
		if line == "k" || line == "\x1b[A" {
			if selected > 0 {
				selected--
			}
			continue
		}
		if _, err := fmt.Fprintln(w, "Select a number, or q to cancel"); err != nil {
			return -1, err
		}
		if err != nil {
			return -1, err
		}
	}
}
