package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/conversation"
	"github.com/H4fizWasabie/yen/internal/runtime"
	"github.com/H4fizWasabie/yen/internal/session"
)

type rpcProvider struct{}

func (rpcProvider) Next(context.Context, []agent.Message, []string) (agent.Response, error) {
	return agent.Response{Text: "rpc reply", StopReason: "stop"}, nil
}

func TestServePromptStateAndMessagesUseJSONLProtocol(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(turn conversation.Turn) string { return filepath.Join(dir, turn.ConversationID+".jsonl") }
	link := conversation.Link{Adapter: "rpc", AdapterKey: "test", ConversationID: "conv-rpc", WorkspaceID: dir}
	input := strings.NewReader(`{"id":"1","type":"prompt","message":"hello"}` + "\n" + `{"id":"2","type":"get_messages"}` + "\n" + `{"id":"3","type":"get_tree"}` + "\n" + `{"id":"4","type":"get_last_assistant_text"}` + "\n")
	var output bytes.Buffer
	server := Server{Runner: runner, Link: link}
	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if len(records) < 6 || records[0]["type"] != "response" || records[0]["id"] != "1" {
		t.Fatalf("records=%#v", records)
	}
	if !strings.Contains(output.String(), `"event":"done"`) || !strings.Contains(output.String(), "rpc reply") {
		t.Fatalf("output=%s", output.String())
	}
}

func TestServeRejectsUnknownCommand(t *testing.T) {
	dir := t.TempDir()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Runner: runtime.New(queue, rpcProvider{}, nil), Link: conversation.Link{ConversationID: "conv-rpc"}}
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(`{"type":"nope"}`+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"success":false`) || !strings.Contains(output.String(), "unsupported rpc command") {
		t.Fatalf("output=%s", output.String())
	}
}

func TestBranchCommandPersistsActiveLeaf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv-rpc.jsonl")
	saved := session.New(path, session.Header{ID: "conv-rpc", ConversationID: "conv-rpc", CWD: dir})
	if _, err := saved.Append(session.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	entryID, err := saved.Append(session.Message{Role: "assistant", Content: "reply"})
	if err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(conversation.Turn) string { return path }
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "conv-rpc", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "branch", EntryID: entryID}); err != nil {
		t.Fatal(err)
	}
	opened, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened.LeafID() == entryID || !strings.Contains(output.String(), `"success":true`) {
		t.Fatalf("leaf=%q output=%s", opened.LeafID(), output.String())
	}
}

func TestSetSessionNamePersistsAndReturnsName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "named.jsonl")
	created := session.New(path, session.Header{ID: "named", ConversationID: "named", CWD: dir})
	if _, err := created.Append(session.Message{Role: "assistant", Content: "ready"}); err != nil {
		t.Fatal(err)
	}
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(conversation.Turn) string { return path }
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "named", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "set_session_name", Name: "  nightly\ncheck  "}); err != nil {
		t.Fatal(err)
	}
	opened, err := session.Open(path)
	if err != nil || opened.SessionName() != "nightly check" || !strings.Contains(output.String(), `"name":"nightly check"`) {
		t.Fatalf("name=%q output=%s err=%v", opened.SessionName(), output.String(), err)
	}
}

func TestForkCommandCreatesDurableSessionCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.jsonl")
	saved := session.New(path, session.Header{ID: "source", ConversationID: "source", CWD: dir})
	if _, err := saved.Append(session.Message{Role: "assistant", Content: "ready"}); err != nil {
		t.Fatal(err)
	}
	entryID := saved.LeafID()
	queue, err := conversation.OpenQueue(filepath.Join(dir, "queue.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(queue, rpcProvider{}, nil)
	runner.SessionPath = func(conversation.Turn) string { return path }
	forkPath := filepath.Join(dir, "fork.jsonl")
	server := Server{Runner: runner, Link: conversation.Link{ConversationID: "source", WorkspaceID: dir}}
	var output bytes.Buffer
	if err := server.handle(context.Background(), &output, command{ID: "1", Type: "fork", EntryID: entryID, Path: forkPath}); err != nil {
		t.Fatal(err)
	}
	forked, err := session.Open(forkPath)
	if err != nil || len(forked.Messages()) != 1 || !strings.Contains(output.String(), `"success":true`) {
		t.Fatalf("messages=%#v output=%s err=%v", forked.Messages(), output.String(), err)
	}
}
