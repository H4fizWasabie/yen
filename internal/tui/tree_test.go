package tui

import (
	"bufio"
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
