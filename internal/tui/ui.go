package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

const maxWidgetLines = 10

// HandleExtensionUI adapts the shared extension_ui_request protocol to the
// interactive CLI's line-driven screen.
func HandleExtensionUI(_ context.Context, request map[string]any, reader *bufio.Reader, writer io.Writer) (map[string]any, error) {
	return HandleExtensionUIWithScreenMode(nil, request, reader, writer, nil, false)
}

// HandleExtensionUIWithScreen applies presentation requests to screen before
// redrawing it. A nil screen preserves the line-oriented fallback behavior.
func HandleExtensionUIWithScreen(_ context.Context, request map[string]any, reader *bufio.Reader, writer io.Writer, screen *Screen) (map[string]any, error) {
	return HandleExtensionUIWithScreenMode(nil, request, reader, writer, screen, false)
}

// HandleExtensionUIWithScreenMode uses raw terminal selectors when rawInput is enabled.
func HandleExtensionUIWithScreenMode(_ context.Context, request map[string]any, reader *bufio.Reader, writer io.Writer, screen *Screen, rawInput bool) (map[string]any, error) {
	response := map[string]any{"id": request["id"]}
	switch method := request["method"].(string); method {
	case "select":
		raw, _ := request["options"].([]any)
		options := make([]string, len(raw))
		for i, option := range raw {
			options[i] = fmt.Sprint(option)
		}
		selectFn := Select
		if rawInput {
			selectFn = SelectRaw
		}
		selected, err := selectFn(reader, writer, fmt.Sprint(request["title"]), options)
		if err != nil {
			return nil, err
		}
		if selected < 0 {
			response["cancelled"] = true
		} else {
			response["value"] = options[selected]
		}
	case "confirm":
		if _, err := fmt.Fprintf(writer, "%s\n%s [y/N] ", request["title"], request["message"]); err != nil {
			return nil, err
		}
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return nil, err
		}
		value := strings.ToLower(strings.TrimSpace(line))
		if value == "q" {
			response["cancelled"] = true
		} else {
			response["confirmed"] = value == "y" || value == "yes"
		}
	case "input", "editor":
		if _, err := fmt.Fprintf(writer, "%s\n> ", request["title"]); err != nil {
			return nil, err
		}
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return nil, err
		}
		value := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if value == "q" {
			response["cancelled"] = true
		} else {
			response["value"] = value
		}
	case "notify":
		if screen != nil {
			screen.Scrollback = append(screen.Scrollback, fmt.Sprint(request["message"]))
			if err := screen.Render(writer); err != nil {
				return nil, err
			}
		} else if _, err := fmt.Fprintf(writer, "[%s] %v\n", method, requestValue(request, method)); err != nil {
			return nil, err
		}
	case "setStatus":
		if screen != nil {
			if request["statusKey"] == nil {
				screen.Status = fmt.Sprint(request["statusText"])
			} else {
				if screen.ExtensionStatuses == nil {
					screen.ExtensionStatuses = make(map[string]string)
				}
				key := fmt.Sprint(request["statusKey"])
				if request["statusText"] == nil {
					delete(screen.ExtensionStatuses, key)
				} else {
					screen.ExtensionStatuses[key] = fmt.Sprint(request["statusText"])
				}
			}
			if err := screen.Render(writer); err != nil {
				return nil, err
			}
		} else if _, err := fmt.Fprintf(writer, "[%s] %v\n", method, requestValue(request, method)); err != nil {
			return nil, err
		}
	case "setWidget":
		if screen != nil {
			key := fmt.Sprint(request["widgetKey"])
			delete(screen.WidgetsAbove, key)
			delete(screen.WidgetsBelow, key)
			placement := screen.WidgetsBelow
			if request["widgetPlacement"] == "aboveEditor" {
				placement = screen.WidgetsAbove
			}
			if placement == nil {
				placement = make(map[string][]string)
				if request["widgetPlacement"] == "aboveEditor" {
					screen.WidgetsAbove = placement
				} else {
					screen.WidgetsBelow = placement
				}
			}
			if request["widgetLines"] == nil {
				delete(placement, key)
			} else {
				lines := requestLines(request["widgetLines"])
				if len(lines) > maxWidgetLines {
					lines = append(lines[:maxWidgetLines], "... (widget truncated)")
				}
				placement[key] = lines
			}
			if err := screen.Render(writer); err != nil {
				return nil, err
			}
		} else if _, err := fmt.Fprintf(writer, "[%s] %v\n", method, requestValue(request, method)); err != nil {
			return nil, err
		}
	case "setTitle":
		if screen != nil {
			screen.Title = fmt.Sprint(request["title"])
			if err := screen.Render(writer); err != nil {
				return nil, err
			}
		} else if _, err := fmt.Fprintf(writer, "[%s] %v\n", method, requestValue(request, method)); err != nil {
			return nil, err
		}
	case "set_editor_text":
		if screen != nil {
			screen.Input = fmt.Sprint(request["text"])
			if err := screen.Render(writer); err != nil {
				return nil, err
			}
		} else {
			return response, nil
		}
	default:
		return nil, fmt.Errorf("unsupported extension UI method %q", method)
	}
	return response, nil
}

func requestLines(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	lines := make([]string, len(values))
	for i, value := range values {
		lines[i] = fmt.Sprint(value)
	}
	return lines
}

func requestValue(request map[string]any, method string) any {
	for _, key := range []string{"message", "statusText", "title", "widgetLines"} {
		if value, ok := request[key]; ok {
			return value
		}
	}
	return method
}
