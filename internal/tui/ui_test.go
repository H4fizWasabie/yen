package tui

import (
	"bufio"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/extensions"
	"github.com/H4fizWasabie/yen/internal/session"
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

func TestRenderMessageUsesExtensionRenderer(t *testing.T) {
	registry := extensions.New()
	if err := registry.RegisterMessageRenderer("custom", func(value any, _ extensions.RenderOptions) (any, bool) {
		return "rendered: " + value.(string), true
	}); err != nil {
		t.Fatal(err)
	}
	rendered, ok := RenderMessage(registry, session.Message{Role: "custom", Content: "payload"})
	if !ok || rendered != "rendered: payload" {
		t.Fatalf("rendered=%q ok=%v", rendered, ok)
	}
}
