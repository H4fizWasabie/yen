package codingagent

import (
	"context"
	"fmt"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/session"
)

type workingNoteTool struct{ session *session.Session }

func (workingNoteTool) Name() string { return "working_note" }

func (t workingNoteTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if clear, _ := args["clear"].(bool); clear {
		if _, err := t.session.ClearWorkingNote(); err != nil {
			return "", err
		}
		return "Working Note cleared.", nil
	}
	note, ok := args["note"].(string)
	if !ok {
		return "Provide `note`, or set `clear: true` to clear the Working Note.", nil
	}
	if _, err := t.session.AppendWorkingNote(note); err != nil {
		return "", err
	}
	return "Working Note updated.", nil
}

func newWorkingNoteTool(current *session.Session) agent.Tool {
	return workingNoteTool{session: current}
}

type operationalNotesTool struct{ path string }

func (operationalNotesTool) Name() string { return "note_operations" }

func (t operationalNotesTool) Execute(_ context.Context, args map[string]any) (string, error) {
	section, ok := args["section"].(string)
	if !ok || section == "" {
		return "", fmt.Errorf("section is required")
	}
	content, ok := args["content"].(string)
	if !ok || content == "" {
		return "", fmt.Errorf("content is required")
	}
	return appendOperationalNote(t.path, section, content)
}
