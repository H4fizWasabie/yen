package tui

import (
	"bufio"
	"fmt"
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

func TestScreenRenderAtKeepsViewportAndRegions(t *testing.T) {
	screen := Screen{Scrollback: []string{"one", "two", "three"}, Status: "Ready", Input: "draft"}
	var output strings.Builder
	if err := screen.RenderAt(&output, 20, 6); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "one") || !strings.Contains(got, "two") || !strings.Contains(got, "three") || !strings.Contains(got, "Status: Ready") || !strings.Contains(got, "> draft") {
		t.Fatalf("viewport output=%q", got)
	}
}

func TestHandleExtensionUIKeepsExtensionStatusesByKey(t *testing.T) {
	screen := &Screen{Status: "Ready"}
	var output strings.Builder
	for _, request := range []map[string]any{
		{"method": "setStatus", "statusKey": "z", "statusText": "zeta"},
		{"method": "setStatus", "statusKey": "a", "statusText": "alpha"},
	} {
		if _, err := HandleExtensionUIWithScreen(nil, request, bufio.NewReader(strings.NewReader("")), &output, screen); err != nil {
			t.Fatal(err)
		}
	}
	if got := screen.ExtensionStatuses["a"]; got != "alpha" {
		t.Fatalf("a status=%q", got)
	}
	if !strings.Contains(output.String(), "alpha zeta") {
		t.Fatalf("sorted extension statuses missing: %q", output.String())
	}
	if _, err := HandleExtensionUIWithScreen(nil, map[string]any{
		"method": "setStatus", "statusKey": "a", "statusText": nil,
	}, bufio.NewReader(strings.NewReader("")), &output, screen); err != nil {
		t.Fatal(err)
	}
	if _, ok := screen.ExtensionStatuses["a"]; ok {
		t.Fatalf("cleared extension status remains: %#v", screen.ExtensionStatuses)
	}
}

func TestHandleExtensionUITruncatesLongWidgets(t *testing.T) {
	screen := &Screen{}
	lines := make([]any, 11)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%d", i)
	}
	if _, err := HandleExtensionUIWithScreen(nil, map[string]any{
		"method": "setWidget", "widgetKey": "hint", "widgetLines": lines, "widgetPlacement": "aboveEditor",
	}, bufio.NewReader(strings.NewReader("")), &strings.Builder{}, screen); err != nil {
		t.Fatal(err)
	}
	got := screen.WidgetsAbove["hint"]
	if len(got) != 11 || got[9] != "line-9" || got[10] != "... (widget truncated)" {
		t.Fatalf("widget lines=%#v", got)
	}
}

func TestHandleExtensionUIReplacesWidgetAcrossPlacements(t *testing.T) {
	screen := &Screen{}
	for _, request := range []map[string]any{
		{"method": "setWidget", "widgetKey": "hint", "widgetLines": []any{"above"}, "widgetPlacement": "aboveEditor"},
		{"method": "setWidget", "widgetKey": "hint", "widgetLines": []any{"below"}, "widgetPlacement": "belowEditor"},
	} {
		if _, err := HandleExtensionUIWithScreen(nil, request, bufio.NewReader(strings.NewReader("")), &strings.Builder{}, screen); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := screen.WidgetsAbove["hint"]; ok {
		t.Fatalf("old widget placement remains: %#v", screen.WidgetsAbove)
	}
	if got := screen.WidgetsBelow["hint"]; len(got) != 1 || got[0] != "below" {
		t.Fatalf("new widget placement=%#v", got)
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

func TestSelectWithNoOptionsCancels(t *testing.T) {
	selected, err := Select(bufio.NewReader(strings.NewReader("\n")), &strings.Builder{}, "Pick", nil)
	if err != nil || selected != -1 {
		t.Fatalf("selected=%d err=%v", selected, err)
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

func TestRenderMessageFormatsSerializedTextComponent(t *testing.T) {
	registry := extensions.New()
	if err := registry.RegisterMessageRenderer("custom", func(any, extensions.RenderOptions) (any, bool) {
		return map[string]any{"text": "serialized component", "paddingX": float64(0), "paddingY": float64(0)}, true
	}); err != nil {
		t.Fatal(err)
	}
	rendered, ok := RenderMessage(registry, session.Message{Role: "custom"})
	if !ok || rendered != "serialized component" {
		t.Fatalf("rendered=%q ok=%v", rendered, ok)
	}
}

func TestRenderMessagePreservesSerializedTextComponentPadding(t *testing.T) {
	registry := extensions.New()
	if err := registry.RegisterMessageRenderer("custom", func(any, extensions.RenderOptions) (any, bool) {
		return map[string]any{
			"text": "hello\nworld", "paddingX": float64(1), "paddingY": float64(1),
		}, true
	}); err != nil {
		t.Fatal(err)
	}
	rendered, ok := RenderMessage(registry, session.Message{Role: "custom"})
	if !ok || rendered != "\n hello \n world \n" {
		t.Fatalf("rendered=%q ok=%v", rendered, ok)
	}
}

func TestRenderMessageFormatsSerializedBoxComponent(t *testing.T) {
	registry := extensions.New()
	if err := registry.RegisterMessageRenderer("custom", func(any, extensions.RenderOptions) (any, bool) {
		return map[string]any{
			"type":     "box",
			"paddingX": float64(1),
			"paddingY": float64(1),
			"children": []any{map[string]any{"type": "text", "text": "hello\nworld"}},
		}, true
	}); err != nil {
		t.Fatal(err)
	}
	rendered, ok := RenderMessage(registry, session.Message{Role: "custom"})
	if !ok || rendered != "\n hello \n world \n" {
		t.Fatalf("rendered=%q ok=%v", rendered, ok)
	}
}
