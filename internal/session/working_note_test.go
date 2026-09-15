package session

import (
	"strings"
	"testing"
)

func TestWorkingNotePersistsAndIsBounded(t *testing.T) {
	path := t.TempDir() + "/session.jsonl"
	s := New(path, Header{ID: "s1", CWD: "."})
	if _, err := s.AppendWorkingNote("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendWorkingNote(strings.Repeat("x", workingNoteWriteCap)); err != nil {
		t.Fatal(err)
	}
	if len(s.WorkingNote()) != workingNoteWriteCap || !strings.HasSuffix(s.WorkingNote(), strings.Repeat("x", 20)) {
		t.Fatalf("working note length/content = %d/%q", len(s.WorkingNote()), s.WorkingNote())
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.WorkingNote() != s.WorkingNote() {
		t.Fatalf("reopened note differs")
	}
	if _, err := reopened.ClearWorkingNote(); err != nil {
		t.Fatal(err)
	}
	if reopened.WorkingNote() != "" {
		t.Fatalf("note=%q after clear", reopened.WorkingNote())
	}
}
