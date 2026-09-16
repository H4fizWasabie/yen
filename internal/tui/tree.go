package tui

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/H4fizWasabie/yen/internal/session"
)

// RenderTree lays out a session's entries as a git-log-style ASCII branch
// tree, matching the oracle's TreeSelectorComponent connector/gutter layout
// (packages/coding-agent/src/modes/interactive/components/tree-selector.ts:664-743,696-729):
// each row gets a "├─ "/"└─ " connector against its parent, ancestor branches
// that continue past a row keep a "│  " gutter, and the active leaf is
// marked "(current)".
func RenderTree(entries []session.TreeEntry, activeID string) []string {
	byID := make(map[string]session.TreeEntry, len(entries))
	children := make(map[string][]string)
	var roots []string
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	for _, entry := range entries {
		if entry.ParentID != nil {
			if _, ok := byID[*entry.ParentID]; ok {
				children[*entry.ParentID] = append(children[*entry.ParentID], entry.ID)
				continue
			}
		}
		roots = append(roots, entry.ID)
	}

	var lines []string
	var walk func(id, prefix string, isLast, isRoot bool)
	walk = func(id, prefix string, isLast, isRoot bool) {
		node := byID[id]
		label := node.Name
		if label == "" {
			label = node.ID
		}
		if activeID != "" && node.ID == activeID {
			label += " (current)"
		}
		connector := ""
		childPrefix := prefix
		if !isRoot {
			if isLast {
				connector = "└─ "
				childPrefix += "   "
			} else {
				connector = "├─ "
				childPrefix += "│  "
			}
		}
		lines = append(lines, prefix+connector+label)
		kids := children[id]
		for i, kid := range kids {
			walk(kid, childPrefix, i == len(kids)-1, false)
		}
	}
	for i, root := range roots {
		walk(root, "", i == len(roots)-1, true)
	}
	return lines
}

// SelectTree presents the session tree as a navigable selector and returns
// the selected entry ID. It keeps the static RenderTree output available for
// callers that only need a report.
//
// A highlighted node with children can be folded to hide its descendants
// from the option list and unfolded to restore them, matching the oracle's
// app.tree.foldOrUp/app.tree.unfoldOrDown toggle
// (packages/coding-agent/src/modes/interactive/components/tree-selector.ts:1002-1017,1109-1116).
// Those actions default to ctrl+left/alt+left and ctrl+right/alt+right
// (packages/coding-agent/src/core/keybindings.ts:145-152); this slice covers
// only the fold/unfold toggle, not the oracle's additional "move to nearest
// branch segment" fallback when the highlighted node isn't foldable. The Go
// raw-byte reader does not yet parse ctrl/alt modifier escape sequences, so
// raw terminals use plain Left/Right arrows and line-buffered
// (scripted/piped) callers use "f"/"u" as documented approximations of the
// oracle key bindings.
func SelectTree(r *bufio.Reader, w io.Writer, title string, entries []session.TreeEntry, rawInput bool) (string, error) {
	if len(entries) == 0 {
		return "", nil
	}
	byID := make(map[string]session.TreeEntry, len(entries))
	childrenOf := make(map[string][]string)
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	for _, entry := range entries {
		if entry.ParentID != nil {
			childrenOf[*entry.ParentID] = append(childrenOf[*entry.ParentID], entry.ID)
		}
	}
	depthOf := func(entry session.TreeEntry) int {
		depth := 0
		for steps, parent := 0, entry.ParentID; parent != nil && steps < len(entries); steps, parent = steps+1, byID[*parent].ParentID {
			if _, ok := byID[*parent]; !ok {
				break
			}
			depth++
		}
		return depth
	}
	folded := make(map[string]bool)
	visible := func() []session.TreeEntry {
		skip := make(map[string]bool)
		for _, entry := range entries {
			if entry.ParentID != nil && (folded[*entry.ParentID] || skip[*entry.ParentID]) {
				skip[entry.ID] = true
			}
		}
		out := make([]session.TreeEntry, 0, len(entries))
		for _, entry := range entries {
			if !skip[entry.ID] {
				out = append(out, entry)
			}
		}
		return out
	}
	optionFor := func(entry session.TreeEntry) string {
		label := entry.Name
		if label == "" {
			label = entry.ID
		}
		marker := ""
		if folded[entry.ID] {
			marker = "⊞ "
		}
		return strings.Repeat("  ", depthOf(entry)) + marker + label
	}
	fold := func(id string) {
		if len(childrenOf[id]) > 0 {
			folded[id] = true
		}
	}
	unfold := func(id string) {
		delete(folded, id)
	}

	selected := 0
	if rawInput {
		return selectTreeRaw(r, w, title, visible, optionFor, fold, unfold, &selected)
	}
	return selectTreeLine(r, w, title, visible, optionFor, fold, unfold, &selected)
}

func selectTreeRaw(r *bufio.Reader, w io.Writer, title string, visible func() []session.TreeEntry, optionFor func(session.TreeEntry) string, fold, unfold func(string), selected *int) (string, error) {
	if _, err := fmt.Fprintln(w, title); err != nil {
		return "", err
	}
	rendered := 0
	render := func() error {
		vis := visible()
		clampSelected(selected, len(vis))
		if rendered > 0 {
			if _, err := fmt.Fprintf(w, "\x1b[%dA", rendered); err != nil {
				return err
			}
		}
		for i, entry := range vis {
			marker := "  "
			if i == *selected {
				marker = "> "
			}
			if rendered > 0 {
				if _, err := io.WriteString(w, "\r\x1b[2K"); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "%s%d) %s\n", marker, i+1, optionFor(entry)); err != nil {
				return err
			}
		}
		rendered = len(vis)
		return nil
	}
	if err := render(); err != nil {
		return "", err
	}
	var number strings.Builder
	for {
		vis := visible()
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		switch b {
		case '\r', '\n':
			if number.Len() == 0 {
				if len(vis) == 0 {
					return "", nil
				}
				return vis[*selected].ID, nil
			}
			n, parseErr := strconv.Atoi(number.String())
			if parseErr == nil && n >= 1 && n <= len(vis) {
				return vis[n-1].ID, nil
			}
			number.Reset()
		case 'q', 'Q':
			return "", nil
		case 'j':
			number.Reset()
			if *selected < len(vis)-1 {
				*selected++
			}
			if err := render(); err != nil {
				return "", err
			}
		case 'k':
			number.Reset()
			if *selected > 0 {
				*selected--
			}
			if err := render(); err != nil {
				return "", err
			}
		case '\x1b':
			next, err := r.ReadByte()
			if err != nil {
				return "", err
			}
			if next != '[' {
				return "", nil
			}
			direction, err := r.ReadByte()
			if err != nil {
				return "", err
			}
			number.Reset()
			switch direction {
			case 'B':
				if *selected < len(vis)-1 {
					*selected++
				}
			case 'A':
				if *selected > 0 {
					*selected--
				}
			case 'D':
				if len(vis) > 0 {
					fold(vis[*selected].ID)
				}
			case 'C':
				if len(vis) > 0 {
					unfold(vis[*selected].ID)
				}
			default:
				continue
			}
			if err := render(); err != nil {
				return "", err
			}
		default:
			if b >= '0' && b <= '9' {
				number.WriteByte(b)
			} else {
				number.Reset()
			}
		}
	}
}

func selectTreeLine(r *bufio.Reader, w io.Writer, title string, visible func() []session.TreeEntry, optionFor func(session.TreeEntry) string, fold, unfold func(string), selected *int) (string, error) {
	if _, err := fmt.Fprintln(w, title); err != nil {
		return "", err
	}
	render := func() error {
		vis := visible()
		clampSelected(selected, len(vis))
		for i, entry := range vis {
			marker := "  "
			if i == *selected {
				marker = "> "
			}
			if _, err := fmt.Fprintf(w, "%s%d) %s\n", marker, i+1, optionFor(entry)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := render(); err != nil {
		return "", err
	}
	for {
		vis := visible()
		line, err := r.ReadString('\n')
		if err != nil && len(line) == 0 {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if len(vis) == 0 {
				return "", nil
			}
			return vis[*selected].ID, nil
		}
		if strings.EqualFold(line, "q") || line == "\x1b" {
			return "", nil
		}
		if n, parseErr := strconv.Atoi(line); parseErr == nil && n >= 1 && n <= len(vis) {
			return vis[n-1].ID, nil
		}
		switch line {
		case "j":
			if *selected < len(vis)-1 {
				*selected++
			}
			if err := render(); err != nil {
				return "", err
			}
		case "k":
			if *selected > 0 {
				*selected--
			}
			if err := render(); err != nil {
				return "", err
			}
		case "f":
			if len(vis) > 0 {
				fold(vis[*selected].ID)
			}
			if err := render(); err != nil {
				return "", err
			}
		case "u":
			if len(vis) > 0 {
				unfold(vis[*selected].ID)
			}
			if err := render(); err != nil {
				return "", err
			}
		default:
			if _, err := fmt.Fprintln(w, "Select a number, or q to cancel"); err != nil {
				return "", err
			}
		}
	}
}

func clampSelected(selected *int, count int) {
	if *selected >= count {
		*selected = count - 1
	}
	if *selected < 0 {
		*selected = 0
	}
}
