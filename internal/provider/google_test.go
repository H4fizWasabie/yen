package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestGoogleGenerativeAIStreamsNativeTextThinkingAndToolCall(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models/gemini-2.5-flash:streamGenerateContent") || r.URL.Query().Get("key") != "google-key" {
			t.Fatalf("request=%s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"responseId\":\"g-1\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"think\",\"thought\":true}]} }]}\n\n"))
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"},{\"functionCall\":{\"name\":\"read\",\"args\":{\"path\":\"x\"}}}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":5,\"thoughtsTokenCount\":2,\"totalTokenCount\":17}}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleGenerativeAI(server.URL, "google-key", "gemini-2.5-flash")
	provider.ThinkingLevel = "high"
	var events []agent.StreamEvent
	result, err := provider.NextWithEvents(context.Background(), []agent.Message{{Role: "user", Content: "hello", Images: []string{"data:image/png;base64,AA=="}}}, []string{"read"}, func(event agent.StreamEvent) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello" || result.Thinking != "think" || result.ResponseID != "g-1" || result.StopReason != "stop" || result.RawStopReason != "STOP" || len(result.ToolCalls) != 1 || result.Usage.Reasoning != 2 {
		t.Fatalf("result=%#v", result)
	}
	contents, ok := request["contents"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("contents=%#v", request["contents"])
	}
	parts := contents[0].(map[string]any)["parts"].([]any)
	if _, ok := parts[1].(map[string]any)["inlineData"]; !ok {
		t.Fatalf("parts=%#v", parts)
	}
	if _, ok := request["tools"]; !ok || len(events) < 4 {
		t.Fatalf("request=%#v events=%#v", request, events)
	}
}

func TestGoogleGenerativeAIListsGenerativeModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "google-key" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-2.5-flash","supportedGenerationMethods":["generateContent"]},{"name":"models/embed","supportedGenerationMethods":["embedContent"]}]}`))
	}))
	defer server.Close()
	models, err := NewGoogleGenerativeAI(server.URL, "google-key", "model").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Provider != "google" || models[0].ID != "gemini-2.5-flash" {
		t.Fatalf("models=%#v", models)
	}
}

func TestGoogleGenerativeAIListsAllCatalogPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := r.URL.Query().Get("pageToken"); token == "" {
			_, _ = w.Write([]byte(`{"models":[{"name":"models/first","supportedGenerationMethods":["generateContent"]}],"nextPageToken":"next"}`))
		} else if token == "next" {
			_, _ = w.Write([]byte(`{"models":[{"name":"models/second","supportedGenerationMethods":["generateContent"]}]}`))
		} else {
			t.Fatalf("unexpected page token %q", token)
		}
	}))
	defer server.Close()

	models, err := NewGoogleGenerativeAI(server.URL, "google-key", "model").ListModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "first" || models[1].ID != "second" {
		t.Fatalf("models=%#v err=%v", models, err)
	}
}

func TestGoogleVertexCatalogPreservesProviderID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"publishers/google/models/gemini-test","supportedGenerationMethods":["generateContent"]}]}`))
	}))
	defer server.Close()

	client := NewGoogleGenerativeAI(server.URL, "", "model")
	client.ProviderName = "google-vertex"
	client.BearerToken = "access-token"
	models, err := client.ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].Provider != "google-vertex" || models[0].ID != "publishers/google/models/gemini-test" {
		t.Fatalf("models=%#v err=%v", models, err)
	}
}

func TestGoogleGenerativeAIUsesYenConfiguration(t *testing.T) {
	t.Setenv("YEN_PROVIDER", "google")
	t.Setenv("YEN_MODEL", "gemini-test")
	t.Setenv("YEN_GOOGLE_API_KEY", "google-key")
	configured := ConfiguredFromEnv()
	client, ok := configured.(GoogleGenerativeAI)
	if !ok || client.Model != "gemini-test" || client.APIKey != "google-key" {
		t.Fatalf("configured=%#v", configured)
	}
}
