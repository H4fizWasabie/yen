package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// HandleExtensionUI adapts the shared extension_ui_request protocol to the
// interactive CLI's line-driven screen.
func HandleExtensionUI(_ context.Context, request map[string]any, reader *bufio.Reader, writer io.Writer) (map[string]any, error) {
	response := map[string]any{"id": request["id"]}
	switch method := request["method"].(string); method {
	case "select":
		raw, _ := request["options"].([]any)
		options := make([]string, len(raw))
		for i, option := range raw {
			options[i] = fmt.Sprint(option)
		}
		selected, err := Select(reader, writer, fmt.Sprint(request["title"]), options)
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
	case "input", "editor", "set_editor_text":
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
	case "notify", "setStatus", "setWidget", "setTitle":
		if _, err := fmt.Fprintf(writer, "[%s] %v\n", method, requestValue(request, method)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported extension UI method %q", method)
	}
	return response, nil
}

func requestValue(request map[string]any, method string) any {
	for _, key := range []string{"message", "statusText", "title", "widgetLines"} {
		if value, ok := request[key]; ok {
			return value
		}
	}
	return method
}
