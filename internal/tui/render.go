package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/H4fizWasabie/yen/internal/extensions"
	"github.com/H4fizWasabie/yen/internal/session"
)

// RenderMessage applies an extension renderer to a session message for the
// terminal scrollback. String results and text components stay readable;
// other structured results use their JSON representation.
func RenderMessage(registry *extensions.Registry, message session.Message) (string, bool) {
	if registry == nil {
		return "", false
	}
	renderer, ok := registry.MessageRenderer(message.Role)
	if !ok {
		return "", false
	}
	value, ok := renderer(message.Content, extensions.RenderOptions{})
	if !ok {
		return "", false
	}
	if text, ok := value.(string); ok {
		return text, true
	}
	if component, ok := value.(map[string]any); ok {
		text, textOK := component["text"].(string)
		if textOK && (component["type"] == "text" || component["paddingX"] != nil && component["paddingY"] != nil) {
			return renderTextComponent(text, component["paddingX"], component["paddingY"]), true
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value), false
	}
	return string(data), true
}

func renderTextComponent(text string, rawPaddingX, rawPaddingY any) string {
	paddingX := componentPadding(rawPaddingX)
	paddingY := componentPadding(rawPaddingY)
	if paddingX == 0 && paddingY == 0 {
		return text
	}
	lines := make([]string, 0, len(strings.Split(text, "\n"))+2*paddingY)
	blank := strings.Repeat(" ", paddingX)
	for i := 0; i < paddingY; i++ {
		lines = append(lines, "")
	}
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, blank+line+blank)
	}
	for i := 0; i < paddingY; i++ {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func componentPadding(value any) int {
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case int:
		number = float64(value)
	default:
		return 0
	}
	if number <= 0 {
		return 0
	}
	return int(number)
}
