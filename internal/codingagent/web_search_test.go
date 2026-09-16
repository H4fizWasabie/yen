package codingagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestWebSearchRegistryCanReplaceProviderCall(t *testing.T) {
	registry := extensions.New()
	if err := registry.RegisterHooks(extensions.Hooks{BeforeWebSearch: func(_ context.Context, query string) (string, bool, error) {
		return "extension result for " + query, true, nil
	}}); err != nil {
		t.Fatal(err)
	}
	tool := NewWebSearchToolWithRegistry(registry)
	result, err := tool.Execute(context.Background(), map[string]any{"query": "yen"})
	if err != nil || result != "extension result for yen" {
		t.Fatalf("result=%q err=%v", result, err)
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

func TestWebSearchUsesInjectedSearchOperation(t *testing.T) {
	called := false
	tool := webSearchTool{keys: []string{"configured"}, search: func(_ context.Context, query string) (tavilyResponse, error) {
		called = true
		if query != "injected" {
			t.Fatalf("query=%q", query)
		}
		return tavilyResponse{Answer: "intercepted"}, nil
	}}
	result, err := tool.Execute(context.Background(), map[string]any{"query": "injected"})
	if err != nil || result != "intercepted" || !called {
		t.Fatalf("result=%q err=%v called=%v", result, err, called)
	}
}
