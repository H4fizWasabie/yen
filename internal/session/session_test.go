package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionDefersFirstWriteUntilAssistantMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := New(path, Header{ID: "session-1", CWD: "/workspace", Channel: "cli", ChannelSessionID: "/workspace"})

	userID, err := s.Append(Message{Role: "user", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session should not be published before an assistant message, stat error=%v", err)
	}

	assistantID, err := s.Append(Message{Role: "assistant", Content: []ContentPart{{Type: "text", Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var entries []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want header plus two messages", len(entries))
	}
	if entries[0]["type"] != "session" || entries[0]["version"] != float64(3) {
		t.Fatalf("header = %#v", entries[0])
	}
	if _, present := entries[0]["parentId"]; present {
		t.Fatalf("header unexpectedly contains parentId: %#v", entries[0])
	}
	if entries[1]["id"] != userID || entries[1]["parentId"] != nil {
		t.Fatalf("user entry = %#v", entries[1])
	}
	if entries[2]["id"] != assistantID || entries[2]["parentId"] != userID {
		t.Fatalf("assistant entry = %#v", entries[2])
	}
}

func TestOpenSessionContinuesParentChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := New(path, Header{ID: "session-1", CWD: "/workspace", Channel: "cli", ChannelSessionID: "/workspace"})
	userID, err := s.Append(Message{Role: "user", Content: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(Message{Role: "assistant", Content: "hi"}); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	nextID, err := reopened.Append(Message{Role: "user", Content: "again"})
	if err != nil {
		t.Fatal(err)
	}
	if nextID != "entry-3" {
		t.Fatalf("next id = %q", nextID)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || userID == "" {
		t.Fatal("session was not persisted")
	}
}
