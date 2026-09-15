package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type RememberTool struct {
	Engine  *Engine
	Context Context
}

func (RememberTool) Name() string { return "remember" }

func (t RememberTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", errors.New("query is required")
	}
	hits, err := t.Engine.Remember(query, t.Context)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "No matching memory.", nil
	}
	var out strings.Builder
	for _, hit := range hits {
		fmt.Fprintf(&out, "- [%s] %s", hit.ID, hit.Subject)
		if hit.Body != "" {
			fmt.Fprintf(&out, ": %s", hit.Body)
		}
		fmt.Fprintf(&out, " (scope=%s", hit.Scope)
		if hit.OwnerID != "" {
			fmt.Fprintf(&out, ", owner=%s", hit.OwnerID)
		}
		if hit.WorkspaceID != "" {
			fmt.Fprintf(&out, ", workspace=%s", hit.WorkspaceID)
		}
		if hit.ConversationID != "" {
			fmt.Fprintf(&out, ", conversation=%s", hit.ConversationID)
		}
		out.WriteString(")\n")
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

type SaveNoteTool struct {
	Engine  *Engine
	Context Context
}

func (SaveNoteTool) Name() string { return "save_note" }

func (t SaveNoteTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	text, ok := args["note"].(string)
	if !ok {
		text, ok = args["text"].(string)
	}
	if !ok || strings.TrimSpace(text) == "" {
		return "", errors.New("note is required")
	}
	_, err := t.Engine.SaveNote(text, t.Context)
	if err != nil {
		return "", err
	}
	return "Durable note saved.", nil
}
