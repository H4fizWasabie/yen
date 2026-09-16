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

func TestOpenAICompletionsAppliesProviderHeaderHook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Extension") != "provider-trace" || r.Header.Get("Authorization") != "" {
			t.Fatalf("headers=%v", r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	ctx := agent.WithProviderHeaderHook(context.Background(), func(_ context.Context, headers map[string][]string) {
		headers["X-Extension"] = []string{"provider-trace"}
		delete(headers, "Authorization")
	})
	result, err := NewOpenAICompletions(server.URL, "key", "model").Next(ctx, nil, nil)
	if err != nil || result.Text != "ok" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestOpenAICompletionsUsesChoiceUsageWhenTopLevelUsageIsAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"usage":{"prompt_tokens":12,"completion_tokens":5,"completion_tokens_details":{"reasoning_tokens":2}}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()
	result, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), nil, nil)
	if err != nil || result.Usage.Input != 12 || result.Usage.Output != 5 || result.Usage.Reasoning != 2 || result.Usage.TotalTokens != 17 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestOpenAICompletionsListsModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("request=%s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":""},{"id":"model-b"}]}`))
	}))
	defer server.Close()
	client := NewOpenAICompletions(server.URL, "key", "model-a")
	client.ProviderName = "fixture"
	models, err := client.ListModels(context.Background())
	if err != nil || len(models) != 2 || models[1].ID != "model-b" || models[0].Provider != "fixture" {
		t.Fatalf("models=%#v err=%v", models, err)
	}
}

func TestGitHubCopilotCatalogFiltersUnavailableModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"disabled","model_picker_enabled":true,"policy":{"state":"disabled"}},
			{"id":"available","model_picker_enabled":true,"policy":{"state":"enabled"}},
			{"id":"policy-only","model_picker_enabled":false,"policy":{"state":"enabled"}},
			{"id":"no-tools","model_picker_enabled":true,"policy":{"state":"enabled"},"capabilities":{"supports":{"tool_calls":false}}}
		]}`))
	}))
	defer server.Close()
	client := NewOpenAICompletions(server.URL, "key", "model")
	client.ProviderName = "github-copilot"
	models, err := client.ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "available" {
		t.Fatalf("models=%#v err=%v", models, err)
	}
}

func TestOpenAICompletionsPreservesResponseMetadataAndFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"id":"resp-1","model":"served-model","choices":[{"delta":{"content":"blocked"},"finish_reason":null}]}`)
		fmt.Fprintln(w, `data: {"id":"resp-1","model":"served-model","choices":[{"delta":{},"finish_reason":"content_filter"}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	result, err := NewOpenAICompletions(server.URL, "key", "requested-model").Next(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseID != "resp-1" || result.ResponseModel != "served-model" || result.RawStopReason != "content_filter" {
		t.Fatalf("metadata=%#v", result)
	}
	if result.StopReason != "error" || result.ErrorMessage != "Provider finish_reason: content_filter" {
		t.Fatalf("finish=%#v", result)
	}
}

func TestOpenAICompletionsTreatsNullFinishReasonAsStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":null}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	result, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), nil, nil)
	if err != nil || result.StopReason != "stop" || result.Text != "done" {
		t.Fatalf("result=%#v err=%v", result, err)
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

func TestOpenAICompletionsSendsProviderRouting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Provider  map[string]any `json:"provider"`
			MaxTokens int            `json:"max_completion_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		order, ok := payload.Provider["order"].([]any)
		if !ok || len(order) != 3 || order[0] != "OpenInference" {
			t.Fatalf("provider routing=%#v", payload.Provider)
		}
		if payload.Provider["allow_fallbacks"] != false {
			t.Fatalf("provider routing=%#v", payload.Provider)
		}
		if payload.MaxTokens != 8000 {
			t.Fatalf("max tokens=%d", payload.MaxTokens)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client := NewOpenAICompletions(server.URL, "key", "deepseek/deepseek-v4-flash-0731")
	client.ProviderRouting = map[string]any{
		"order": []string{"OpenInference", "BaseTen", "GMICloud"}, "quantizations": []string{"fp8"}, "allow_fallbacks": false,
	}
	client.MaxTokens = 8000
	if _, err := client.Next(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMistralUsesNativeReasoningEffortField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Reasoning       map[string]string `json:"reasoning"`
			ReasoningEffort string            `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ReasoningEffort != "high" || payload.Reasoning != nil {
			t.Fatalf("reasoning=%#v effort=%q", payload.Reasoning, payload.ReasoningEffort)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client := NewOpenAICompletions(server.URL, "", "mistral-model")
	client.ProviderName = "mistral"
	client.ReasoningEffort = "high"
	if _, err := client.Next(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMistralSerializesNativeReplayFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if got := payload.Messages[0]["content"].([]any)[1].(map[string]any)["image_url"]; got != "data:image/png;base64,AQ==" {
			t.Fatalf("image=%#v", got)
		}
		assistant := payload.Messages[1]
		if assistant["prefix"] != false {
			t.Fatalf("prefix=%#v", assistant["prefix"])
		}
		call := assistant["tool_calls"].([]any)[0].(map[string]any)
		if call["index"] != float64(0) {
			t.Fatalf("index=%#v", call["index"])
		}
		tool := payload.Messages[2]
		if tool["name"] != "lookup" || tool["tool_call_id"] != "abc123456" {
			t.Fatalf("tool=%#v", tool)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := NewOpenAICompletions(server.URL, "", "mistral-model")
	client.ProviderName = "mistral"
	messages := []agent.Message{
		{Role: "user", Content: "describe", Images: []string{"data:image/png;base64,AQ=="}},
		{Role: "assistant", Content: "answer", Thinking: "reason", ToolCalls: []agent.ToolCall{{ID: "abc123456", Name: "lookup", Args: map[string]any{"q": "pi"}}}},
		{Role: "tool", ToolCallID: "abc123456", ToolName: "lookup", Content: "found"},
	}
	if _, err := client.Next(context.Background(), messages, nil); err != nil {
		t.Fatal(err)
	}
}

func TestZAIUsesThinkingAndToolStreamFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Reasoning  map[string]string `json:"reasoning"`
			Thinking   map[string]any    `json:"thinking"`
			ToolStream bool              `json:"tool_stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Reasoning != nil || payload.Thinking["type"] != "enabled" || payload.Thinking["clear_thinking"] != false || !payload.ToolStream {
			t.Fatalf("reasoning=%#v thinking=%#v tool_stream=%v", payload.Reasoning, payload.Thinking, payload.ToolStream)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client := NewOpenAICompletions(server.URL, "", "glm-5.3-flash")
	client.ProviderName = "zai"
	client.ReasoningEffort = "high"
	if _, err := client.Next(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompatibleProvidersUseNativeThinkingFields(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		check    func(t *testing.T, payload map[string]json.RawMessage)
	}{
		{name: "qwen", provider: "qwen-token-plan", check: func(t *testing.T, payload map[string]json.RawMessage) {
			var enabled bool
			if json.Unmarshal(payload["enable_thinking"], &enabled) != nil || !enabled || payload["reasoning"] != nil {
				t.Fatalf("payload=%s", payloadJSON(payload))
			}
		}},
		{name: "deepseek", provider: "deepseek", check: func(t *testing.T, payload map[string]json.RawMessage) {
			var thinking map[string]string
			if json.Unmarshal(payload["thinking"], &thinking) != nil || thinking["type"] != "enabled" || payload["reasoning"] != nil {
				t.Fatalf("payload=%s", payloadJSON(payload))
			}
		}},
		{name: "together", provider: "together", check: func(t *testing.T, payload map[string]json.RawMessage) {
			var reasoning map[string]bool
			var effort string
			if json.Unmarshal(payload["reasoning"], &reasoning) != nil || !reasoning["enabled"] || json.Unmarshal(payload["reasoning_effort"], &effort) != nil || effort != "high" {
				t.Fatalf("payload=%s", payloadJSON(payload))
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload := make(map[string]json.RawMessage)
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				test.check(t, payload)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			client := NewOpenAICompletions(server.URL, "", "test-model")
			client.ProviderName = test.provider
			client.ReasoningEffort = "high"
			if _, err := client.Next(context.Background(), nil, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBasetenChatTemplateModelsUseEnableThinkingArgument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			ChatTemplateArgs map[string]any `json:"chat_template_args"`
			Reasoning        map[string]any `json:"reasoning"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ChatTemplateArgs["enable_thinking"] != true || payload.Reasoning != nil {
			t.Fatalf("chat_template_args=%#v reasoning=%#v", payload.ChatTemplateArgs, payload.Reasoning)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	client := NewOpenAICompletions(server.URL, "", "moonshotai/Kimi-K2.5")
	client.ProviderName = "baseten"
	client.ReasoningEffort = "high"
	if _, err := client.Next(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
}

func payloadJSON(payload map[string]json.RawMessage) string {
	data, _ := json.Marshal(payload)
	return string(data)
}

func TestMistralParsesThinkingChunksAndNormalizesToolIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				ToolCallID string `json:"tool_call_id"`
				ToolCalls  []struct {
					ID string `json:"id"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Messages) != 2 || len(payload.Messages[0].ToolCalls) != 1 || payload.Messages[0].ToolCalls[0].ID != "16k0n7o19" || payload.Messages[1].ToolCallID != "16k0n7o19" {
			t.Fatalf("messages=%#v", payload.Messages)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"plan"}]},{"type":"text","text":"answer"}]},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := NewOpenAICompletions(server.URL, "", "mistral-model")
	client.ProviderName = "mistral"
	result, err := client.Next(context.Background(), []agent.Message{
		{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-with-too-many-chars", Name: "read", Args: map[string]any{"path": "x"}}}},
		{Role: "tool", ToolCallID: "call-with-too-many-chars", Content: "done"},
	}, nil)
	if err != nil || result.Thinking != "plan" || result.Text != "answer" || result.StopReason != "stop" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestMistralReplaysThinkingAsNativeContentChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		var content []map[string]any
		if len(payload.Messages) != 1 || json.Unmarshal(payload.Messages[0].Content, &content) != nil || content[0]["type"] != "thinking" || content[0]["thinking"].([]any)[0].(map[string]any)["text"] != "plan" {
			t.Fatalf("messages=%#v", payload.Messages)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := NewOpenAICompletions(server.URL, "", "mistral-model")
	client.ProviderName = "mistral"
	if _, err := client.Next(context.Background(), []agent.Message{{Role: "assistant", Content: "answer", Thinking: "plan"}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMistralUsesFallbackIDAndObjectToolArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"read","arguments":{"path":"x"}}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()

	client := NewOpenAICompletions(server.URL, "", "mistral-model")
	client.ProviderName = "mistral"
	result, err := client.Next(context.Background(), nil, nil)
	if err != nil || result.StopReason != "toolUse" || len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "toolcall0" || result.ToolCalls[0].Args["path"] != "x" {
		t.Fatalf("result=%#v err=%v", result, err)
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

func TestOpenAICompletionsEmitsPartialMessageEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"a"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"b"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	var events []agent.StreamEvent
	result, err := NewOpenAICompletions(server.URL, "", "test-model").NextWithEvents(context.Background(), nil, nil, func(event agent.StreamEvent) {
		events = append(events, event)
	})
	if err != nil || result.Text != "ab" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(events) != 4 || events[0].Type != "text_start" || events[1].Type != "text_delta" || events[1].Delta != "a" || events[2].Delta != "b" || events[3].Type != "text_end" || events[3].Partial.Content != "ab" {
		t.Fatalf("events=%#v", events)
	}
}

func TestOpenAICompletionsEmitsThinkingEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"reasoning_content":"think"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"answer"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	var events []agent.StreamEvent
	result, err := NewOpenAICompletions(server.URL, "", "test-model").NextWithEvents(context.Background(), nil, nil, func(event agent.StreamEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Thinking != "think" || result.ThinkingSignature != "reasoning_content" || result.Text != "answer" {
		t.Fatalf("result=%#v", result)
	}
	if len(events) != 6 || events[0].Type != "thinking_start" || events[1].Type != "thinking_delta" || events[1].Delta != "think" || events[2].Type != "text_start" || events[3].Type != "text_delta" || events[4].Type != "text_end" || events[5].Type != "thinking_end" || events[5].Partial.Thinking != "think" {
		t.Fatalf("events=%#v", events)
	}
}

func TestOpenAICompletionsReplaysThinkingField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Messages[0]["reasoning"] != "plan" {
			t.Fatalf("messages=%#v", payload.Messages)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	message := agent.Message{Role: "assistant", Content: "answer", Thinking: "plan", ThinkingSignature: "reasoning"}
	if _, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), []agent.Message{message}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAICompletionsPreservesAndReplaysReasoningDetails(t *testing.T) {
	detail := `{"type":"reasoning.summary","summary":"plan"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Messages[0]["reasoning_details"].([]any)) != 1 {
			t.Fatalf("messages=%#v", payload.Messages)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"reasoning_details\":[%s]},\"finish_reason\":\"stop\"}]}\n", detail)
	}))
	defer server.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"reasoning_details\":[%s]},\"finish_reason\":\"stop\"}]}\n", detail)
	}))
	defer first.Close()
	result, err := NewOpenAICompletions(first.URL, "", "test-model").Next(context.Background(), nil, nil)
	if err != nil || result.ThinkingSignature == "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), []agent.Message{{Role: "assistant", ThinkingSignature: result.ThinkingSignature}}, nil); err != nil {
		t.Fatal(err)
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

func TestOpenAICompletionsSendsSchemasForCodingTools(t *testing.T) {
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
		for _, tool := range payload.Tools {
			properties, ok := tool.Function.Parameters["properties"].(map[string]any)
			if !ok || len(properties) == 0 {
				t.Errorf("%s has no properties: %#v", tool.Function.Name, tool.Function.Parameters)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	tools := []string{"read", "bash", "powershell", "edit", "write", "grep", "find", "ls", "working_note", "note_operations", "remember", "save_note", "recall_turns"}
	if _, err := NewOpenAICompletions(server.URL, "", "test-model").Next(context.Background(), nil, tools); err != nil {
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
