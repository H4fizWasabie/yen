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
	selected, err := SelectTree(bufio.NewReader(strings.NewReader("j\n\n")), &strings.Builder{}, "Tree", entries)
	if err != nil || selected != "child" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}
