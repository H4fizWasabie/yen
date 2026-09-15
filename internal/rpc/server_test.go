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
	input := strings.NewReader(`{"id":"1","type":"prompt","message":"hello"}` + "\n" + `{"id":"2","type":"get_messages"}` + "\n")
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
	if len(records) < 4 || records[0]["type"] != "response" || records[0]["id"] != "1" {
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
