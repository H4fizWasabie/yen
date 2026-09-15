package codingagent

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSkillPromptExpansionLoadsBodyAndArguments(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, ".agents", "skills", "release")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: release\ndescription: Release safely\n---\nRun the release checks."), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ExpandPrompt(workspace, "/skill:release v1.2")
	if !strings.Contains(got, `<skill name="release"`) || !strings.Contains(got, "References are relative to "+root) || !strings.Contains(got, "Run the release checks.") || !strings.HasSuffix(got, "\n\nv1.2") {
		t.Fatalf("expanded=%q", got)
	}
}

func TestPromptTemplateSubstitutionSupportsQuotedArgsDefaultsAndSlices(t *testing.T) {
	got := substitutePromptArgs("$1|$10|$@|${3:-fallback}|${@:2:2}|${@:3}", []string{"one", "two words", "three", "four", "five", "six", "seven", "eight", "nine", "ten"})
	want := "one|ten|one two words three four five six seven eight nine ten|three|two words three|three four five six seven eight nine ten"
	if got != want {
		t.Fatalf("substitution=%q want %q", got, want)
	}
	args := parsePromptArgs(`review "two words" 'three words'`)
	if len(args) != 3 || args[1] != "two words" || args[2] != "three words" {
		t.Fatalf("args=%#v", args)
	}
}
