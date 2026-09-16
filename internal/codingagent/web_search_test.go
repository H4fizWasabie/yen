package codingagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/extensions"
)

func TestWebSearchSnapshotsKeysAndFallsBack(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Header.Get("Authorization") == "Bearer first" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var request struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Query != "yen" || request.MaxResults != 5 {
			t.Fatalf("request=%#v err=%v", request, err)
		}
		_, _ = w.Write([]byte(`{"answer":"summary","results":[{"title":"title","url":"https://example.test","content":"content"}]}`))
	}))
	defer server.Close()

	tool := webSearchTool{client: server.Client(), endpoint: server.URL, keys: []string{"first", "second"}}
	result, err := tool.Execute(context.Background(), map[string]any{"query": "yen"})
	if err != nil || result != "summary\n\ntitle\nhttps://example.test\ncontent" {
		t.Fatalf("result=%q err=%v", result, err)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d, want 2", attempts)
	}
}

type webSearchHookProvider struct {
	seen  []agent.Message
	calls int
}

func (p *webSearchHookProvider) Next(_ context.Context, messages []agent.Message, _ []string) (agent.Response, error) {
	p.seen = append([]agent.Message(nil), messages...)
	p.calls++
	if p.calls == 1 {
		return agent.Response{ToolCalls: []agent.ToolCall{{ID: "search-1", Name: "web_search", Args: map[string]any{"query": "yen"}}}, StopReason: "toolUse"}, nil
	}
	return agent.Response{Text: "done", StopReason: "stop"}, nil
}

func TestWebSearchUsesExtensionToolResultInterception(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"answer":"provider result","results":[]}`))
	}))
	defer server.Close()
	registry := extensions.New()
	if err := registry.RegisterHooks(extensions.Hooks{AfterTool: func(_ context.Context, _ agent.Message, _ agent.ToolCall, result agent.ToolResult, _ bool) (agent.ToolResult, bool, error) {
		result.Text = "intercepted result"
		return result, false, nil
	}}); err != nil {
		t.Fatal(err)
	}
	provider := &webSearchHookProvider{}
	_, err := agent.RunFromWithQueuesAndEventsAndImagesAndHooks(context.Background(), provider, []agent.Tool{
		webSearchTool{client: server.Client(), endpoint: server.URL, keys: []string{"key"}},
	}, nil, "search", nil, nil, nil, nil, registry.AgentHooks(nil))
	if err != nil || provider.calls != 2 {
		t.Fatalf("calls=%d err=%v", provider.calls, err)
	}
	if len(provider.seen) < 3 || provider.seen[len(provider.seen)-1].Content != "intercepted result" {
		t.Fatalf("messages=%#v", provider.seen)
	}
}

func TestWebSearchDoesNotReadEnvironmentAfterConstruction(t *testing.T) {
	t.Setenv("YEN_TAVILY_API_KEY", "before")
	t.Setenv("YEN_TAVILY_API_KEY_2", "")
	tool := NewWebSearchTool().(webSearchTool)
	t.Setenv("YEN_TAVILY_API_KEY", "after")
	if len(tool.keys) != 1 || tool.keys[0] != "before" {
		t.Fatalf("keys=%v", tool.keys)
	}
}
