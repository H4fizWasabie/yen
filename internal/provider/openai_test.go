package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestOpenAICompletionsReadsTextSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hello"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},"completion_tokens_details":{"reasoning_tokens":3}}}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	result, err := NewOpenAICompletions(server.URL, "test-key", "test-model").Next(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello world" || result.StopReason != "stop" || result.Usage.Input != 7 || result.Usage.Output != 4 || result.Usage.Reasoning != 3 || result.Usage.CacheRead != 2 || result.Usage.CacheWrite != 1 || result.Usage.TotalTokens != 14 {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenAICompletionsJSONModeSetsResponseFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			ResponseFormat map[string]string `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ResponseFormat["type"] != "json_object" {
			t.Fatalf("response_format=%#v", payload.ResponseFormat)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"{}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	if _, err := NewOpenAICompletions(server.URL, "", "test-model").NextJSON(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompletionsSendsReasoningEffort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Reasoning map[string]string `json:"reasoning"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Reasoning["effort"] != "high" {
			t.Fatalf("reasoning=%#v", payload.Reasoning)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client := NewOpenAICompletions(server.URL, "", "test-model")
	client.ReasoningEffort = "high"
	if _, err := client.Next(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompletionsReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider failed", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "502 Bad Gateway") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenAICompletionsHonorsAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewOpenAICompletions("http://127.0.0.1:1", "", "test-model").Next(ctx, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestOpenAICompletionsRetriesRetryableHTTPError(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("x-should-retry", "true")
			w.Header().Set("retry-after-ms", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := NewOpenAICompletions(server.URL, "", "test-model")
	client.MaxRetries = 1
	result, err := client.Next(context.Background(), nil, nil)
	if err != nil || result.Text != "ok" || requests.Load() != 2 {
		t.Fatalf("result=%#v err=%v requests=%d", result, err, requests.Load())
	}
}

func TestOpenAICompletionsEmitsTextUpdates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"a"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"b"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	var updates []string
	result, err := NewOpenAICompletions(server.URL, "", "test-model").NextWithUpdates(context.Background(), nil, nil, func(text string) {
		updates = append(updates, text)
	})
	if err != nil || result.Text != "ab" || !reflect.DeepEqual(updates, []string{"a", "b"}) {
		t.Fatalf("result=%#v updates=%#v err=%v", result, updates, err)
	}
}

func TestOpenAICompletionsCombinesToolCallDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"read-1","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	result, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), nil, []string{"read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 || result.StopReason != "toolUse" || result.ToolCalls[0].ID != "read-1" || result.ToolCalls[0].Name != "read" || result.ToolCalls[0].Args["path"] != "README.md" {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
}

func TestOpenAICompletionsSendsReadToolSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []struct {
				Function struct {
					Name       string         `json:"name"`
					Parameters map[string]any `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Function.Name != "read" || payload.Tools[0].Function.Parameters["properties"] == nil {
			t.Fatalf("tools=%#v", payload.Tools)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	if _, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), nil, []string{"read"}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompletionsSendsImageContentParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		parts, ok := payload.Messages[0].Content.([]any)
		if !ok || len(parts) != 2 || parts[0].(map[string]any)["text"] != "look" || parts[1].(map[string]any)["type"] != "image_url" {
			t.Fatalf("content=%#v", payload.Messages[0].Content)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"seen"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	_, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), []agent.Message{{Role: "user", Content: "look", Images: []string{"data:image/jpeg;base64,AA=="}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompletionsRejectsStreamWithoutFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"partial"}}]}`)
	}))
	defer server.Close()

	_, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected missing finish_reason error")
	}
}

func TestOpenAICompletionsRejectsMalformedSSEJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {not-json}`)
	}))
	defer server.Close()

	_, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected malformed SSE error")
	}
}
