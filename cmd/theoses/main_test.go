package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

func TestRunRejectsMissingPrompt(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, &bytes.Buffer{}, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestRunMigrationIsExplicit(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "legacy.md"), []byte("---\nid: legacy\ntype: semantic\nsubject: legacy note\nat: 2026-01-01T00:00:00Z\nedges: []\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-migrate-semantic", source, "-memory-dir", target, "-scope", "owner", "-owner", "owner-1"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "migrated 1 semantic nodes") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(source, "legacy.md")); err != nil {
		t.Fatalf("source changed: %v", err)
	}
}

func TestRunPersistsProviderErrorAndReturnsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider failed", http.StatusBadGateway)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	t.Setenv("THEOSES_SESSION_FILE", path)
	t.Setenv("THEOSES_CANONICAL_CONVERSATION_ID", "conv-test")
	t.Setenv("THEOSES_OPENAI_BASE_URL", server.URL)

	var stderr bytes.Buffer
	if code := run([]string{"-p", "hello"}, &bytes.Buffer{}, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	current, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	messages := current.Messages()
	if len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" || messages[1].StopReason != "error" {
		t.Fatalf("messages = %#v", messages)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `"conversationId":"conv-`) {
		t.Fatalf("session identity missing: err=%v data=%s", err, data)
	}
}

func TestRunEndToEndToolTurnUsesSharedRunnerAndMemory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("fixture README\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldCWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if requests.Add(1) == 1 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"read-1","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`)
		} else {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"fixture read"},"finish_reason":"stop"}]}`)
		}
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()
	dataDir := filepath.Join(dir, "data")
	path := filepath.Join(dataDir, "sessions", "session.jsonl")
	t.Setenv("THEOSES_SESSION_FILE", path)
	t.Setenv("THEOSES_DATA_DIR", dataDir)
	t.Setenv("THEOSES_CANONICAL_CONVERSATION_ID", "conv-test")
	t.Setenv("THEOSES_OPENAI_BASE_URL", server.URL)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-p", "read README"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "fixture read\n" {
		t.Fatalf("stdout=%q", stdout.String())
	}
	stored, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Messages()) != 4 {
		t.Fatalf("messages=%#v", stored.Messages())
	}
	if data, err := os.ReadFile(filepath.Join(dataDir, "consolidation-checkpoints.json")); err != nil || !strings.Contains(string(data), "conv-") {
		t.Fatalf("checkpoint err=%v data=%s", err, data)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "conversation-queue.jsonl")); err != nil {
		t.Fatalf("shared queue missing: %v", err)
	}
}

func TestInteractiveSessionCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	current := session.New(path, session.Header{ID: "session-1", ConversationID: "conv-1"})
	runner := &runtime.Runner{}
	link := conversation.Link{ConversationID: "conv-1"}
	var output bytes.Buffer
	handled, err := handleInteractiveCommand("/name evening", current, runner, link, &output)
	if err != nil || !handled || current.SessionName() != "evening" {
		t.Fatalf("name command handled=%v err=%v name=%q", handled, err, current.SessionName())
	}
	handled, err = handleInteractiveCommand("/session", current, runner, link, &output)
	if err != nil || !handled || !strings.Contains(output.String(), "session-1") {
		t.Fatalf("session command handled=%v err=%v output=%q", handled, err, output.String())
	}
}
