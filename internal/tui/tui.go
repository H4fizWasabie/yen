package tui

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Screen is the small presentation model shared by the interactive CLI.
type Screen struct {
	Scrollback        []string
	Status            string
	ExtensionStatuses map[string]string
	Input             string
	Title             string
	WidgetsAbove      map[string][]string
	WidgetsBelow      map[string][]string
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
	if s.Title != "" {
		if _, err := fmt.Fprintf(w, "\n%s", s.Title); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\nStatus: %s\n", s.Status); err != nil {
		return err
	}
	if len(s.ExtensionStatuses) > 0 {
		keys := make([]string, 0, len(s.ExtensionStatuses))
		for key := range s.ExtensionStatuses {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		statuses := make([]string, 0, len(keys))
		for _, key := range keys {
			statuses = append(statuses, s.ExtensionStatuses[key])
		}
		if _, err := fmt.Fprintln(w, strings.Join(statuses, " ")); err != nil {
			return err
		}
	}
	if err := renderWidgets(w, s.WidgetsAbove); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Input\n> %s", strings.ReplaceAll(s.Input, "\n", " ")); err != nil {
		return err
	}
	if err := renderWidgets(w, s.WidgetsBelow); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

func renderWidgets(w io.Writer, widgets map[string][]string) error {
	keys := make([]string, 0, len(widgets))
	for key := range widgets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, line := range widgets[key] {
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}

// Select presents numbered options and accepts a number, j/k, or arrow keys.
// Enter confirms the current selection; q or Escape cancels.
func Select(r *bufio.Reader, w io.Writer, title string, options []string) (int, error) {
	if _, err := fmt.Fprintln(w, title); err != nil {
		return -1, err
	}
	selected := 0
	render := func() error {
		for i, option := range options {
			marker := "  "
			if i == selected {
				marker = "> "
			}
			if _, err := fmt.Fprintf(w, "%s%d) %s\n", marker, i+1, option); err != nil {
				return err
			}
		}
		return nil
	}
	if err := render(); err != nil {
		return -1, err
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil && len(line) == 0 {
			return -1, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return selected, nil
		}
		if strings.EqualFold(line, "q") || line == "\x1b" {
			return -1, nil
		}
		if n, parseErr := strconv.Atoi(line); parseErr == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		if line == "j" || line == "\x1b[B" {
			if selected < len(options)-1 {
				selected++
			}
			if err := render(); err != nil {
				return -1, err
			}
			continue
		}
		if line == "k" || line == "\x1b[A" {
			if selected > 0 {
				selected--
			}
			if err := render(); err != nil {
				return -1, err
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
