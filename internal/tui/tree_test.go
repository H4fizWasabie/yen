package tui

import (
	"bufio"
	"fmt"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/session"
)

func TestRenderTreeDrawsConnectorsAndGutters(t *testing.T) {
	root, childOne := "root", "child-1"
	entries := []session.TreeEntry{
		{ID: "root", Name: "root"},
		{ID: "child-1", ParentID: &root, Name: "child-1"},
		{ID: "grandchild", ParentID: &childOne, Name: "grandchild"},
		{ID: "child-2", ParentID: &root, Name: "child-2"},
	}
	lines := RenderTree(entries, "grandchild")
	got := strings.Join(lines, "\n")
	want := strings.Join([]string{
		"root",
		"├─ child-1",
		"│  └─ grandchild (current)",
		"└─ child-2",
	}, "\n")
	if got != want {
		t.Fatalf("tree=\n%s\nwant=\n%s", got, want)
	}
}

func TestRenderTreeHandlesMultipleRoots(t *testing.T) {
	entries := []session.TreeEntry{
		{ID: "a"},
		{ID: "b"},
	}
	lines := RenderTree(entries, "")
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" {
		t.Fatalf("lines=%#v", lines)
	}
}

func TestSelectTreeReturnsNavigatedEntry(t *testing.T) {
	root := "root"
	entries := []session.TreeEntry{
		{ID: root, Name: "root"},
		{ID: "child", ParentID: &root, Name: "child"},
	}
	selected, err := SelectTree(bufio.NewReader(strings.NewReader("j\n\n")), &strings.Builder{}, "Tree", entries, false)
	if err != nil || selected != "child" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestSelectTreeRawUsesRawByteNavigation(t *testing.T) {
	root := "root"
	entries := []session.TreeEntry{
		{ID: root, Name: "root"},
		{ID: "child", ParentID: &root, Name: "child"},
	}
	selected, err := SelectTree(bufio.NewReader(strings.NewReader("j\r")), &strings.Builder{}, "Tree", entries, true)
	if err != nil || selected != "child" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestSelectTreeRawFoldHidesDescendants(t *testing.T) {
	root := "root"
	child := "child"
	entries := []session.TreeEntry{
		{ID: root, Name: "root"},
		{ID: child, ParentID: &root, Name: "child"},
		{ID: "grandchild", ParentID: &child, Name: "grandchild"},
	}
	var out strings.Builder
	// Left arrow folds the highlighted root, hiding its descendants, so a
	// subsequent "j" (move down) has nothing else to select and Enter
	// confirms root itself instead of a hidden descendant.
	selected, err := SelectTree(bufio.NewReader(strings.NewReader("\x1b[Dj\r")), &out, "Tree", entries, true)
	if err != nil || selected != root {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
	if !strings.Contains(out.String(), "⊞") {
		t.Fatalf("expected fold indicator in rendered output: %q", out.String())
	}
}

func TestSelectTreeRawUnfoldRestoresDescendants(t *testing.T) {
	root := "root"
	child := "child"
	entries := []session.TreeEntry{
		{ID: root, Name: "root"},
		{ID: child, ParentID: &root, Name: "child"},
	}
	// Fold then unfold root, then move down onto the restored child and confirm.
	selected, err := SelectTree(bufio.NewReader(strings.NewReader("\x1b[D\x1b[Cj\r")), &strings.Builder{}, "Tree", entries, true)
	if err != nil || selected != child {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestSelectTreeLineBufferedFoldAndUnfold(t *testing.T) {
	root := "root"
	child := "child"
	entries := []session.TreeEntry{
		{ID: root, Name: "root"},
		{ID: child, ParentID: &root, Name: "child"},
	}
	var out strings.Builder
	// "f" folds root (line-mode approximation of ctrl+left), confirmed by
	// the child no longer being reachable via "j".
	selected, err := SelectTree(bufio.NewReader(strings.NewReader("f\nj\n\n")), &out, "Tree", entries, false)
	if err != nil || selected != root {
		t.Fatalf("selected=%q err=%v output=%q", selected, err, out.String())
	}
}

func numberedEntries(count int) []session.TreeEntry {
	entries := make([]session.TreeEntry, count)
	for i := range entries {
		entries[i] = session.TreeEntry{ID: fmt.Sprintf("n%d", i), Name: fmt.Sprintf("n%d", i)}
	}
	return entries
}

func TestSelectTreeRawPageDownAdvancesByViewportHeight(t *testing.T) {
	entries := numberedEntries(10)
	// PageDown (\x1b[6~) with a 3-row viewport should move three rows down.
	selected, err := SelectTreeAt(bufio.NewReader(strings.NewReader("\x1b[6~\r")), &strings.Builder{}, "Tree", entries, true, 3)
	if err != nil || selected != "n3" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestSelectTreeRawPageUpRetreatsByViewportHeight(t *testing.T) {
	entries := numberedEntries(10)
	// Two PageDowns then one PageUp, each moving three rows, should land on n3.
	selected, err := SelectTreeAt(bufio.NewReader(strings.NewReader("\x1b[6~\x1b[6~\x1b[5~\r")), &strings.Builder{}, "Tree", entries, true, 3)
	if err != nil || selected != "n3" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestSelectTreeLineBufferedPaging(t *testing.T) {
	entries := numberedEntries(10)
	selected, err := SelectTreeAt(bufio.NewReader(strings.NewReader("pgdn\npgdn\npgup\n\n")), &strings.Builder{}, "Tree", entries, false, 3)
	if err != nil || selected != "n3" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestSelectTreeRawWindowedNumericSelectionUsesVisibleOffset(t *testing.T) {
	entries := numberedEntries(5)
	// With a 2-row viewport, moving down three times (j x3) scrolls the
	// window so index 2 becomes the first visible row; typing "1" then
	// Enter must select that windowed row (n2), not the global first entry.
	selected, err := SelectTreeAt(bufio.NewReader(strings.NewReader("jjj1\r")), &strings.Builder{}, "Tree", entries, true, 2)
	if err != nil || selected != "n2" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestSelectTreeDefaultsToRealTerminalViewportHeight(t *testing.T) {
	entries := numberedEntries(3)
	// A non-*os.File writer (like strings.Builder) reports an effectively
	// unbounded viewport, so plain SelectTree must not window/truncate.
	selected, err := SelectTree(bufio.NewReader(strings.NewReader("\r")), &strings.Builder{}, "Tree", entries, true)
	if err != nil || selected != "n0" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}
