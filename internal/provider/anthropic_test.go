package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
		fmt.Fprintln(w, `data: {"type":"message_start","message":{"id":"msg-1","model":"claude-served","usage":{"input_tokens":12,"cache_read_input_tokens":3,"cache_creation_input_tokens":5}}}`)
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"plan"}}`)
		fmt.Fprintln(w, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`)
		fmt.Fprintln(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn","output_tokens":4}}`)
	}))
	defer server.Close()
	provider := NewAnthropicMessages(server.URL, "anthropic-key", "claude-test")
	var updates []string
	result, err := provider.NextWithUpdates(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil, func(text string) { updates = append(updates, text) })
	if err != nil || result.Text != "hello" || result.Thinking != "plan" || result.ResponseID != "msg-1" || result.ResponseModel != "claude-served" || result.RawStopReason != "end_turn" || result.StopReason != "stop" || result.Usage.Input != 12 || result.Usage.Output != 4 || result.Usage.CacheRead != 3 || result.Usage.CacheWrite != 5 || result.Usage.TotalTokens != 24 || strings.Join(updates, "") != "hello" {
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

func TestAnthropicMessagesUsesOracleContentShapeForImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Messages) != 2 {
			t.Fatalf("messages=%#v", payload.Messages)
		}
		imageBlocks, ok := payload.Messages[0].Content.([]any)
		if !ok || len(imageBlocks) != 2 || imageBlocks[0].(map[string]any)["text"] != "(see attached image)" {
			t.Fatalf("image content=%#v", payload.Messages[0].Content)
		}
		if text, ok := payload.Messages[1].Content.(string); !ok || text != "text only" {
			t.Fatalf("text content=%#v", payload.Messages[1].Content)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`)
	}))
	defer server.Close()
	provider := NewAnthropicMessages(server.URL, "key", "claude-test")
	_, err := provider.Next(context.Background(), []agent.Message{
		{Role: "user", Images: []string{"data:image/png;base64,AA=="}},
		{Role: "user", Content: "text only"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropicMessagesRetriesTransientResponses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`)
	}))
	defer server.Close()
	provider := NewAnthropicMessages(server.URL, "key", "claude-test")
	provider.MaxRetries = 1
	if _, err := provider.Next(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil || calls.Load() != 2 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}
