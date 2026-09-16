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
	width, height := terminalSize(w)
	return s.RenderAt(w, width, height)
}

// RenderAt draws the screen within the supplied terminal viewport.
func (s Screen) RenderAt(w io.Writer, width, height int) error {
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}
	lines := s.layout(width, height)
	if _, err := io.WriteString(w, "\x1b[2J\x1b[H"); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func (s Screen) layout(width, height int) []string {
	footer := make([]string, 0)
	if s.Title != "" {
		footer = appendWrapped(footer, s.Title, width)
	}
	footer = appendWrapped(footer, "Status: "+s.Status, width)
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
		footer = appendWrapped(footer, strings.Join(statuses, " "), width)
	}
	footer = appendWidgetLines(footer, s.WidgetsAbove, width)
	footer = appendWrapped(footer, "Input", width)
	footer = appendWrapped(footer, "> "+strings.ReplaceAll(s.Input, "\n", " "), width)
	footer = appendWidgetLines(footer, s.WidgetsBelow, width)
	if len(footer) > height {
		footer = footer[len(footer)-height:]
	}

	available := height - len(footer) - 1
	if available < 0 {
		available = 0
	}
	scrollback := []string{"Scrollback"}
	for _, entry := range s.Scrollback {
		scrollback = appendWrapped(scrollback, entry, width)
	}
	if len(scrollback) > available {
		scrollback = scrollback[len(scrollback)-available:]
	}
	return append(scrollback, footer...)
}

func appendWidgetLines(lines []string, widgets map[string][]string, width int) []string {
	keys := make([]string, 0, len(widgets))
	for key := range widgets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, line := range widgets[key] {
			lines = appendWrapped(lines, line, width)
		}
	}
	return lines
}

func appendWrapped(lines []string, value string, width int) []string {
	value = strings.ReplaceAll(value, "\r", "")
	for _, part := range strings.Split(value, "\n") {
		runes := []rune(part)
		if len(runes) == 0 {
			lines = append(lines, "")
			continue
		}
		for len(runes) > width {
			lines = append(lines, string(runes[:width]))
			runes = runes[width:]
		}
		lines = append(lines, string(runes))
	}
	return lines
}

// Select presents numbered options and accepts a number, j/k, or arrow keys.
// Enter confirms the current selection; q or Escape cancels.
func Select(r *bufio.Reader, w io.Writer, title string, options []string) (int, error) {
	if _, err := fmt.Fprintln(w, title); err != nil {
		return -1, err
	}
	if len(options) == 0 {
		return -1, nil
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
