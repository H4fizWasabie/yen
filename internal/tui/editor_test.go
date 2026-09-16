package tui

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadLineAppliesCursorEditingKeys(t *testing.T) {
	got, err := ReadLine(bufio.NewReader(strings.NewReader("hello\x1b[D\x7f\n")))
	if err != nil {
		t.Fatal(err)
	}
	if got != "helo" {
		t.Fatalf("line=%q", got)
	}
}

func TestReadLineWithHistoryNavigatesPreviousEntries(t *testing.T) {
	history := NewLineHistory()
	reader := bufio.NewReader(strings.NewReader("first\nsecond\n\x1b[A\n"))
	for _, want := range []string{"first", "second", "second"} {
		got, err := ReadLineWithHistory(reader, history)
		if err != nil || got != want {
			t.Fatalf("line=%q want=%q err=%v", got, want, err)
		}
	}
}
