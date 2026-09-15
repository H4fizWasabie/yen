package session

import "testing"

func TestOperationOutcomeAndWorkingNoteStaleness(t *testing.T) {
	s := New(t.TempDir()+"/session.jsonl", Header{ID: "s1", CWD: "."})
	if _, err := s.AppendWorkingNote("keep this"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := s.Append(Message{Role: "user", Content: "turn"}); err != nil {
			t.Fatal(err)
		}
	}
	if !s.IsWorkingNoteStale() {
		t.Fatal("working note should be stale after six user turns")
	}
	if _, err := s.AppendOperationFinished("aborted"); err != nil {
		t.Fatal(err)
	}
	if s.LastOperationOutcome() != "aborted" {
		t.Fatalf("outcome=%q", s.LastOperationOutcome())
	}
}
