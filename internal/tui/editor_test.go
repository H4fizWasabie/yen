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
