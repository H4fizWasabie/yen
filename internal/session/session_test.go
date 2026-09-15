package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	if nextID == "" || nextID == userID {
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

func TestOpenSkipsMalformedLinesWithoutLosingSessionEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := New(path, Header{ID: "session-1", CWD: "/workspace", Channel: "cli", ChannelSessionID: "/workspace"})
	if _, err := s.Append(Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(Message{Role: "assistant", Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("not-json\n")...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Messages()) != 2 {
		t.Fatalf("messages = %#v", reopened.Messages())
	}
	if _, err := reopened.Append(Message{Role: "user", Content: "again"}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenMigratesLegacyTypeScriptSessionToV3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	raw := strings.Join([]string{
		`{"type":"session","id":"legacy","timestamp":"2026-01-01T00:00:00Z","cwd":"/workspace"}`,
		`{"type":"message","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":"old"}}`,
		`{"type":"message","timestamp":"2026-01-01T00:00:02Z","message":{"role":"hookMessage","content":"note","provider":"legacy-provider","model":"legacy-model"}}`,
		`{"type":"custom","timestamp":"2026-01-01T00:00:02Z","customType":"extension-state","data":{"enabled":true}}`,
		`{"type":"compaction","timestamp":"2026-01-01T00:00:03Z","firstKeptEntryIndex":1,"summary":"older history","tokensBefore":9}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Messages()) != 2 || opened.Messages()[1].Role != "custom" {
		t.Fatalf("messages=%#v", opened.Messages())
	}
	context := opened.ContextMessages()
	if len(context) != 3 || !strings.Contains(context[0].Content.(string), "older history") || context[1].Content != "old" {
		t.Fatalf("context=%#v", context)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version":3`) || !strings.Contains(string(data), `"firstKeptEntryId"`) || !strings.Contains(string(data), `"customType":"extension-state"`) || !strings.Contains(string(data), `"provider":"legacy-provider"`) || !strings.Contains(string(data), `"model":"legacy-model"`) {
		t.Fatalf("session was not rewritten as v3: %s", data)
	}
	if strings.Contains(string(data), "firstKeptEntryIndex") || strings.Contains(string(data), "hookMessage") {
		t.Fatalf("legacy fields remain: %s", data)
	}
}

func TestSessionReadbackPreservesToolTurnBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := New(path, Header{ID: "session-1", CWD: "/workspace", Channel: "cli", ChannelSessionID: "/workspace"})
	for _, message := range []Message{
		{Role: "user", Content: "read README"},
		{Role: "assistant", Content: []ContentPart{{Type: "toolCall", ID: "calc-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}, StopReason: "toolUse"},
		{Role: "toolResult", ToolCallID: "calc-1", Content: []ContentPart{{Type: "text", Text: "README contents"}}},
		{Role: "assistant", Content: "done", StopReason: "stop"},
	} {
		if _, err := s.Append(message); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []map[string]any
	for _, raw := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 5 {
		t.Fatalf("persisted lines = %d", len(lines))
	}
	toolCall := lines[2]["message"].(map[string]any)
	if toolCall["role"] != "assistant" || toolCall["stopReason"] != "toolUse" {
		t.Fatalf("tool call = %#v", toolCall)
	}
	toolResult := lines[3]["message"].(map[string]any)
	if toolResult["role"] != "toolResult" || toolResult["toolCallId"] != "calc-1" {
		t.Fatalf("tool result = %#v", toolResult)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Messages()) != 4 {
		t.Fatalf("readback messages = %#v", reopened.Messages())
	}
}

func TestSessionReadbackPreservesAssistantUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	session := New(path, Header{ID: "usage", CWD: t.TempDir(), Channel: "cli"})
	if _, err := session.Append(Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	usage := &Usage{Input: 4, Output: 2, TotalTokens: 6}
	if _, err := session.Append(Message{Role: "assistant", Content: "done", Usage: usage}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	messages := reopened.Messages()
	if len(messages) != 2 || messages[1].Usage == nil || *messages[1].Usage != *usage {
		t.Fatalf("messages=%#v", messages)
	}
}

func TestSessionContextUsesCompactionBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	session := New(path, Header{ID: "compact", CWD: t.TempDir(), Channel: "cli"})
	if _, err := session.Append(Message{Role: "user", Content: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append(Message{Role: "assistant", Content: "old reply"}); err != nil {
		t.Fatal(err)
	}
	keptID, err := session.Append(Message{Role: "user", Content: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append(Message{Role: "assistant", Content: "keep reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.AppendCompaction("old summary", keptID, 42, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append(Message{Role: "user", Content: "new"}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Append(Message{Role: "assistant", Content: "new reply"}); err != nil {
		t.Fatal(err)
	}

	messages := session.ContextMessages()
	if len(messages) != 5 || messages[0].Role != "user" || messages[1].Content != "keep" || messages[4].Content != "new reply" {
		t.Fatalf("context messages=%#v", messages)
	}
	if !strings.Contains(messages[0].Content.(string), "old summary") {
		t.Fatalf("summary message=%#v", messages[0])
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.ContextMessages(); len(got) != 5 || got[1].Content != "keep" {
		t.Fatalf("reopened context=%#v", got)
	}
}

func TestSessionPreparesCompactionFromRecentTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	session := New(path, Header{ID: "plan", CWD: t.TempDir(), Channel: "cli"})
	for _, content := range []string{"one", "one reply", "two", "two reply", "three", "three reply"} {
		role := "user"
		if strings.HasSuffix(content, "reply") {
			role = "assistant"
		}
		if _, err := session.Append(Message{Role: role, Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := session.PrepareCompaction(2)
	if err != nil {
		t.Fatal(err)
	}
	if plan.FirstKeptEntryID == "" || len(plan.Messages) != 2 || plan.Messages[0].Content != "one" || plan.Messages[1].Content != "one reply" {
		t.Fatalf("plan=%#v", plan)
	}
	if plan.TokensBefore == 0 {
		t.Fatalf("plan tokens=%d", plan.TokensBefore)
	}
}
