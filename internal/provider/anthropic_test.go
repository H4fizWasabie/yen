package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestAnthropicMessagesStreamsTextAndThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "anthropic-key" || r.Header.Get("anthropic-version") == "" {
			t.Fatalf("headers=%v", r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, "event: message_start")
		fmt.Fprintln(w, `data: {"type":"message_start","message":{"id":"msg-1","usage":{"input_tokens":12}}}`)
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"plan"}}`)
		fmt.Fprintln(w, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`)
		fmt.Fprintln(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn","output_tokens":4}}`)
	}))
	defer server.Close()
	provider := NewAnthropicMessages(server.URL, "anthropic-key", "claude-test")
	var updates []string
	result, err := provider.NextWithUpdates(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil, func(text string) { updates = append(updates, text) })
	if err != nil || result.Text != "hello" || result.Thinking != "plan" || result.ResponseID != "msg-1" || result.StopReason != "end_turn" || strings.Join(updates, "") != "hello" {
		t.Fatalf("result=%#v updates=%#v err=%v", result, updates, err)
	}
}

func TestAnthropicMessagesReconstructsToolCallsAndImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-1","name":"read"}}`)
		fmt.Fprintln(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"README.md\"}"}}`)
		fmt.Fprintln(w, `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`)
	}))
	defer server.Close()
	provider := NewAnthropicMessages(server.URL, "key", "claude-test")
	image := "data:image/png;base64,AA=="
	result, err := provider.Next(context.Background(), []agent.Message{{Role: "user", Content: "inspect", Images: []string{image}}}, []string{"read"})
	if err != nil || len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "read" || result.ToolCalls[0].Args["path"] != "README.md" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
