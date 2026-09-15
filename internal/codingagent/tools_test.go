package codingagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/H4fizWasabie/yen/internal/session"
)

func TestNewToolsMatchesCodingToolCore(t *testing.T) {
	got := NewTools(t.TempDir())
	want := []string{"read", "bash", "powershell", "edit", "write", "grep", "find", "ls", "convert_doc", "web_search", "generate_image"}
	if len(got) != len(want) {
		t.Fatalf("tool count=%d, want %d", len(got), len(want))
	}
	for i, tool := range got {
		if tool.Name() != want[i] {
			t.Errorf("tool %d=%q, want %q", i, tool.Name(), want[i])
		}
	}
}

func TestSessionToolsLoadShellSettings(t *testing.T) {
	workspace := t.TempDir()
	settingsPath := filepath.Join(workspace, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"shellCommandPrefix":"export YEN_BASH_PREFIX=from-settings"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_SETTINGS_FILE", settingsPath)
	s := session.New(filepath.Join(workspace, "session.jsonl"), session.Header{ID: "shell", CWD: workspace})
	for _, tool := range NewToolsForSession(workspace, s) {
		if tool.Name() != "bash" {
			continue
		}
		result, err := tool.Execute(context.Background(), map[string]any{"command": `printf "$YEN_BASH_PREFIX"`})
		if err != nil || result != "from-settings" {
			t.Fatalf("result=%q err=%v", result, err)
		}
		return
	}
	t.Fatal("bash tool missing")
}
