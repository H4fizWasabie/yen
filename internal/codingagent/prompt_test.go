package codingagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkingNoteMessageBoundsAndLabelsNote(t *testing.T) {
	note := "a" + string(make([]byte, 2500)) + "z"
	message := WorkingNoteMessage(note)
	if message.Role != "system" {
		t.Fatalf("role=%q", message.Role)
	}
	if len([]rune(message.Content)) > 2200 || len(message.Content) < 20 {
		t.Fatalf("content length=%d", len([]rune(message.Content)))
	}
	if message.Content[:len("<working_note>")] != "<working_note>" {
		t.Fatalf("content=%q", message.Content)
	}
}

func TestContextMessageLoadsAncestorGuidanceInOrder(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "project", "pkg")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "CONTEXT.md"), []byte("local guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	message, ok := ContextMessage(workspace)
	if !ok || message.Role != "system" || !strings.Contains(message.Content, "root guidance") || !strings.Contains(message.Content, "local guidance") || strings.Index(message.Content, "root guidance") > strings.Index(message.Content, "local guidance") {
		t.Fatalf("message=%#v", message)
	}
}
