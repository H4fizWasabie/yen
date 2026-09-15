package codingagent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPromptTemplateExpansionAndDiscovery(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".theoses", "prompts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "review.md")
	if err := os.WriteFile(path, []byte("---\ndescription: Review the supplied target\n---\nReview $1 with $ARGUMENTS."), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ExpandPrompt(workspace, "/review src/main.go carefully"); got != "Review src/main.go with src/main.go carefully." {
		t.Fatalf("expanded=%q", got)
	}
	commands := PromptCommands(workspace)
	if len(commands) != 1 || commands[0]["name"] != "review" || commands[0]["source"] != "prompt" {
		t.Fatalf("commands=%#v", commands)
	}
	if commands[0]["sourceInfo"].(map[string]string)["path"] != path {
		t.Fatalf("source info=%#v", commands[0]["sourceInfo"])
	}
}

func TestExpandPromptLeavesUnknownAndNonPromptTextUntouched(t *testing.T) {
	workspace := t.TempDir()
	if got := ExpandPrompt(workspace, "hello"); got != "hello" {
		t.Fatalf("non-prompt=%q", got)
	}
	if got := ExpandPrompt(workspace, "/missing value"); got != "/missing value" {
		t.Fatalf("unknown=%q", got)
	}
}
