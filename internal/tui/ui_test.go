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

func TestHandleExtensionUIUpdatesScreenPresentation(t *testing.T) {
	screen := &Screen{Status: "Ready"}
	var output strings.Builder
	for _, request := range []map[string]any{
		{"id": "status", "method": "setStatus", "statusText": "Working"},
		{"id": "widget", "method": "setWidget", "widgetKey": "hint", "widgetLines": []any{"waiting"}, "widgetPlacement": "aboveEditor"},
		{"id": "title", "method": "setTitle", "title": "Yen"},
	} {
		if _, err := HandleExtensionUIWithScreen(nil, request, bufio.NewReader(strings.NewReader("")), &output, screen); err != nil {
			t.Fatal(err)
		}
	}
	if screen.Status != "Working" || screen.Title != "Yen" || len(screen.WidgetsAbove["hint"]) != 1 || screen.WidgetsAbove["hint"][0] != "waiting" {
		t.Fatalf("screen=%#v", screen)
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

func TestRenderMessageFormatsStructuredTextComponent(t *testing.T) {
	registry := extensions.New()
	if err := registry.RegisterMessageRenderer("custom", func(any, extensions.RenderOptions) (any, bool) {
		return map[string]any{"type": "text", "text": "rendered component"}, true
	}); err != nil {
		t.Fatal(err)
	}
	rendered, ok := RenderMessage(registry, session.Message{Role: "custom"})
	if !ok || rendered != "rendered component" {
		t.Fatalf("rendered=%q ok=%v", rendered, ok)
	}
}
