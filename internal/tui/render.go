package tui

import (
	"encoding/json"
	"fmt"

	"github.com/H4fizWasabie/yen/internal/extensions"
	"github.com/H4fizWasabie/yen/internal/session"
)

// RenderMessage applies an extension renderer to a session message for the
// terminal scrollback. String results stay readable; structured results use
// their JSON representation.
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
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value), false
	}
	return string(data), true
}
