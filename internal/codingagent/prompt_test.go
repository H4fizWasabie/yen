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

func TestContextMessageLoadsClaudeAndUppercaseAgentsNames(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.MD"), []byte("uppercase guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "CLAUDE.md"), []byte("claude guidance"), 0o600); err != nil {
		t.Fatal(err)
	}
	message, ok := ContextMessage(workspace)
	if !ok || !strings.Contains(message.Content, "uppercase guidance") || !strings.Contains(message.Content, "claude guidance") {
		t.Fatalf("message=%#v", message)
	}
}

func TestSkillsMessageListsLazySkillFiles(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, ".agents", "skills", "release")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: release\ndescription: Ship carefully\n---\nDetailed instructions"), 0o600); err != nil {
		t.Fatal(err)
	}
	message, ok := SkillsMessage(workspace)
	if !ok || !strings.Contains(message.Content, "<name>release</name>") || !strings.Contains(message.Content, "Ship carefully") {
		t.Fatalf("message=%#v", message)
	}
	if strings.Contains(message.Content, "Detailed instructions") {
		t.Fatal("skill body should remain lazy")
	}
}

func TestSkillsUseParentDirectoryWhenNameIsOmitted(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, ".agents", "skills", "release")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\ndescription: Ship carefully\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	message, ok := SkillsMessage(workspace)
	if !ok || !strings.Contains(message.Content, "<name>release</name>") {
		t.Fatalf("message=%#v", message)
	}
	commands := SkillCommands(workspace)
	if len(commands) != 1 || commands[0]["name"] != "skill:release" {
		t.Fatalf("commands=%#v", commands)
	}
	if !strings.Contains(ExpandPrompt(workspace, "/skill:release"), "<skill name=\"release\"") {
		t.Fatal("skill command did not expand")
	}
}
