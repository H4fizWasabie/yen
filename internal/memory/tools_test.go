package memory

import (
	"context"
	"strings"
	"testing"
)

func TestMemoryToolsUseCanonicalScope(t *testing.T) {
	engine, err := OpenEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	ctx := Context{WorkspaceID: "work-1", ConversationID: "conv-1"}
	save := SaveNoteTool{Engine: engine, Context: ctx}
	if _, err := save.Execute(context.Background(), map[string]any{"note": "user prefers concise replies"}); err != nil {
		t.Fatal(err)
	}
	remember := RememberTool{Engine: engine, Context: ctx}
	result, err := remember.Execute(context.Background(), map[string]any{"query": "concise replies"})
	if err != nil || !strings.Contains(result, "user prefers concise replies") || !strings.Contains(result, "workspace=work-1") {
		t.Fatalf("result=%q err=%v", result, err)
	}
	shared, err := (RememberTool{Engine: engine, Context: Context{WorkspaceID: "work-1", ConversationID: "other"}}).Execute(context.Background(), map[string]any{"query": "concise replies"})
	if err != nil || !strings.Contains(shared, "user prefers concise replies") {
		t.Fatalf("workspace result=%q err=%v", shared, err)
	}
	other, err := (RememberTool{Engine: engine, Context: Context{WorkspaceID: "other", ConversationID: "other"}}).Execute(context.Background(), map[string]any{"query": "concise replies"})
	if err != nil || other != "No matching memory." {
		t.Fatalf("cross-scope result=%q err=%v", other, err)
	}
}
