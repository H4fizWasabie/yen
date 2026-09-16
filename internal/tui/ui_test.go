package tui

import (
	"bufio"
	"strings"
	"testing"
)

func TestHandleExtensionUISelectReturnsProtocolResponse(t *testing.T) {
	response, err := HandleExtensionUI(nil, map[string]any{
		"id": "ui-1", "method": "select", "title": "Pick", "options": []any{"first", "second"},
	}, bufio.NewReader(strings.NewReader("2\n")), &strings.Builder{})
	if err != nil || response["id"] != "ui-1" || response["value"] != "second" {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestSelectJThenEnterReturnsHighlightedOption(t *testing.T) {
	var output strings.Builder
	selected, err := Select(bufio.NewReader(strings.NewReader("j\n\n")), &output, "Pick", []string{"first", "second"})
	if err != nil || selected != 1 {
		t.Fatalf("selected=%d err=%v", selected, err)
	}
	if !strings.Contains(output.String(), "> 2) second") {
		t.Fatalf("highlighted option missing from output: %q", output.String())
	}
}
