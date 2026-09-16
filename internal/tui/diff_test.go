package tui

import (
	"strings"
	"testing"
)

func TestRenderDiffAppliesIntraLineHighlightForSingleLineModification(t *testing.T) {
	input := strings.Join([]string{
		" 1 context",
		"-2 foo bar",
		"+2 foo baz",
	}, "\n")
	got := RenderDiff(input)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines=%d want 3: %q", len(lines), got)
	}
	if lines[0] != " 1 context" {
		t.Fatalf("context line changed: %q", lines[0])
	}
	wantRemoved := "-2 foo " + "\x1b[7mbar\x1b[27m"
	wantAdded := "+2 foo " + "\x1b[7mbaz\x1b[27m"
	if lines[1] != wantRemoved {
		t.Fatalf("removed=%q want=%q", lines[1], wantRemoved)
	}
	if lines[2] != wantAdded {
		t.Fatalf("added=%q want=%q", lines[2], wantAdded)
	}
}

func TestRenderDiffShowsMultiLineBlocksAsIsWithoutIntraLineHighlight(t *testing.T) {
	input := strings.Join([]string{
		"-1 removed only",
		"+2 added only",
		"+3 also added",
	}, "\n")
	got := RenderDiff(input)
	if got != input {
		t.Fatalf("got=%q want=%q", got, input)
	}
}

func TestRenderDiffPreservesContextLinesVerbatim(t *testing.T) {
	input := " 1 unchanged\n 2 also unchanged"
	got := RenderDiff(input)
	if got != input {
		t.Fatalf("got=%q want=%q", got, input)
	}
}

func TestRenderDiffReplacesTabsWithSpaces(t *testing.T) {
	input := "-1 a\tb"
	got := RenderDiff(input)
	if strings.Contains(got, "\t") {
		t.Fatalf("expected tabs replaced: %q", got)
	}
}

func TestRenderDiffPassesThroughUnparsableLines(t *testing.T) {
	input := "not a diff line at all"
	got := RenderDiff(input)
	if got != input {
		t.Fatalf("got=%q want=%q", got, input)
	}
}
