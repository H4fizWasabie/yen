package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBranchMovesActiveContextWithoutDeletingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := New(path, Header{ID: "s1", CWD: "."})
	first, err := s.Append(Message{Role: "user", Content: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(Message{Role: "assistant", Content: "old reply"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Branch(first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(Message{Role: "assistant", Content: "new reply"}); err != nil {
		t.Fatal(err)
	}
	context := s.ContextMessages()
	if len(context) != 2 || context[1].Content != "new reply" {
		t.Fatalf("context=%#v", context)
	}
	if len(s.Messages()) != 3 {
		t.Fatalf("history=%#v", s.Messages())
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reopenedContext := reopened.ContextMessages()
	if len(reopenedContext) != 2 || reopenedContext[1].Content != "new reply" {
		t.Fatalf("reopened context=%#v", reopenedContext)
	}
}

func TestForkCopiesActivePrefixAndRecordsParent(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.jsonl")
	s := New(sourcePath, Header{ID: "source", CWD: "."})
	entry, err := s.Append(Message{Role: "user", Content: "question"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(Message{Role: "assistant", Content: "answer"}); err != nil {
		t.Fatal(err)
	}
	fork, err := s.Fork(filepath.Join(dir, "fork.jsonl"), entry, Header{ID: "fork", CWD: "."})
	if err != nil {
		t.Fatal(err)
	}
	if len(fork.Messages()) != 1 {
		t.Fatalf("fork messages=%#v", fork.Messages())
	}
	data, err := os.ReadFile(filepath.Join(dir, "fork.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]any
	if err := json.Unmarshal(data[:bytesIndex(data, '\n')], &header); err != nil {
		t.Fatal(err)
	}
	if header["parentSession"] != sourcePath {
		t.Fatalf("header=%#v", header)
	}
}

func bytesIndex(data []byte, target byte) int {
	for i, value := range data {
		if value == target {
			return i
		}
	}
	return len(data)
}
