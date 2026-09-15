package codingagent

import (
	"context"
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
	if _, err := working.Execute(context.Background(), map[string]any{"note": "remember path"}); err != nil {
		t.Fatal(err)
	}
	if s.WorkingNote() != "remember path" {
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
