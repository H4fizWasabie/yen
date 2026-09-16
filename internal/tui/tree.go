package tui

import (
	"bufio"
	"io"
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
// callers that only need a report. When rawInput is true it reuses SelectRaw
// so byte-at-a-time terminals get immediate j/k/arrow navigation; otherwise
// it uses the line-buffered Select for scripted and piped callers.
func SelectTree(r *bufio.Reader, w io.Writer, title string, entries []session.TreeEntry, rawInput bool) (string, error) {
	if len(entries) == 0 {
		return "", nil
	}
	byID := make(map[string]session.TreeEntry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	options := make([]string, 0, len(entries))
	for _, entry := range entries {
		depth := 0
		for steps, parent := 0, entry.ParentID; parent != nil && steps < len(entries); steps, parent = steps+1, byID[*parent].ParentID {
			if _, ok := byID[*parent]; !ok {
				break
			}
			depth++
		}
		label := entry.Name
		if label == "" {
			label = entry.ID
		}
		options = append(options, strings.Repeat("  ", depth)+label)
	}
	selectFn := Select
	if rawInput {
		selectFn = SelectRaw
	}
	selected, err := selectFn(r, w, title, options)
	if err != nil || selected < 0 {
		return "", err
	}
	return entries[selected].ID, nil
}
