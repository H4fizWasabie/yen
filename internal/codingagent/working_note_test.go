package codingagent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/session"
)

func TestSessionToolsOwnWorkingNoteAndOperationalNotes(t *testing.T) {
	s := session.New(t.TempDir()+"/session.jsonl", session.Header{ID: "s1", CWD: "."})
	tools := NewToolsForSession(t.TempDir(), s)
	var working, notes interface {
		Name() string
		Execute(context.Context, map[string]any) (string, error)
	}
	for _, tool := range tools {
		switch tool.Name() {
		case "working_note":
			working = tool
		case "note_operations":
			notes = tool
		}
	}
	if working == nil || notes == nil {
		t.Fatalf("session tools missing: working=%v notes=%v", working != nil, notes != nil)
	}
	for _, tool := range tools {
		if tool.Name() == "bash" {
			if _, err := tool.Execute(context.Background(), map[string]any{"command": "printf ok"}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(s.WorkingNote(), "ran: printf ok") {
				t.Fatalf("bash note=%q", s.WorkingNote())
			}
			messages := s.Messages()
			if len(messages) != 1 || messages[0].Role != "bashExecution" || messages[0].Output != "ok" || messages[0].ExitCode == nil || *messages[0].ExitCode != 0 {
				t.Fatalf("bash execution=%#v", messages)
			}
			if _, err := tool.Execute(context.Background(), map[string]any{"command": "printf failed >&2; exit 7", "excludeFromContext": true}); err == nil {
				t.Fatal("expected failed bash command")
			}
			messages = s.Messages()
			if len(messages) != 2 || messages[1].ExitCode == nil || *messages[1].ExitCode != 7 || messages[1].Output != "failed" || !messages[1].ExcludeFromContext {
				t.Fatalf("failed bash execution=%#v", messages)
			}
		}
	}
	if _, err := working.Execute(context.Background(), map[string]any{"note": "remember path"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.WorkingNote(), "remember path") {
		t.Fatalf("note=%q", s.WorkingNote())
	}
	if _, err := notes.Execute(context.Background(), map[string]any{"section": "System Status", "content": "healthy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := working.Execute(context.Background(), map[string]any{"clear": true}); err != nil {
		t.Fatal(err)
	}
	if s.WorkingNote() != "" || strings.Contains(s.WorkingNote(), "remember") {
		t.Fatalf("note=%q after clear", s.WorkingNote())
	}
}

func TestBashSessionSpillsLargeOutputAndKeepsTailBounded(t *testing.T) {
	s := session.New(t.TempDir()+"/session.jsonl", session.Header{ID: "large", CWD: t.TempDir()})
	var bash interface {
		Name() string
		Execute(context.Context, map[string]any) (string, error)
	}
	for _, candidate := range NewToolsForSession(t.TempDir(), s) {
		if candidate.Name() == "bash" {
			bash = candidate
			break
		}
	}
	if bash == nil {
		t.Fatal("bash tool missing")
	}
	if _, err := bash.Execute(context.Background(), map[string]any{"command": "head -c 20000 /dev/zero | tr '\\0' x"}); err != nil {
		t.Fatal(err)
	}
	messages := s.Messages()
	if len(messages) != 1 || !messages[0].Truncated || len(messages[0].Output) > 12*1024 || messages[0].FullOutputPath == "" {
		t.Fatalf("bash message=%#v", messages)
	}
	stat, err := os.Stat(messages[0].FullOutputPath)
	if err != nil || stat.Size() < 20000 {
		t.Fatalf("full output path=%q stat=%v err=%v", messages[0].FullOutputPath, stat, err)
	}
	_ = os.Remove(messages[0].FullOutputPath)
}
