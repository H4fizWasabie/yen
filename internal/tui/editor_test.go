package tui

import (
	"bufio"
	"os"
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

func TestEnableRawInputLeavesPipesUntouched(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	restore, enabled, err := EnableRawInput(reader)
	if err != nil || enabled {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
}

func TestReadLineWithOutputRedrawsCursor(t *testing.T) {
	var output strings.Builder
	got, err := ReadLineWithOutput(bufio.NewReader(strings.NewReader("ab\x1b[Da\n")), &output, "> ")
	if err != nil || got != "aab" {
		t.Fatalf("line=%q err=%v", got, err)
	}
	if !strings.Contains(output.String(), "\x1b[2K> aab") || !strings.Contains(output.String(), "\x1b[1D") {
		t.Fatalf("redraw=%q", output.String())
	}
}
