package codingagent

import "testing"

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
