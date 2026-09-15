package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestTheosesMessagesStreamsTextToolCallAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("Authorization") != "Bearer radius-key" {
			t.Fatalf("path=%q authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request struct {
			Model   string `json:"model"`
			Context struct {
				Messages []struct {
					Role string `json:"role"`
				} `json:"messages"`
				Tools []map[string]any `json:"tools"`
			} `json:"context"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "auto" || len(request.Context.Messages) != 1 || request.Context.Messages[0].Role != "user" || len(request.Context.Tools) != 1 || request.Context.Tools[0]["name"] != "read" {
			t.Fatalf("request=%#v", request)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"start\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"text_delta\",\"contentIndex\":0,\"delta\":\"ok\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"toolcall_start\",\"contentIndex\":1,\"id\":\"call-1\",\"toolName\":\"read\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"toolcall_delta\",\"contentIndex\":1,\"delta\":\"{\\\"path\\\":\\\"a.txt\\\"}\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"done\",\"reason\":\"toolUse\",\"responseId\":\"resp-1\",\"usage\":{\"input\":2,\"output\":3,\"totalTokens\":5}}\n\n"))
	}))
	defer server.Close()

	provider := NewTheosesMessages(server.URL+"/v1", "radius-key", "auto")
	result, err := provider.Next(context.Background(), []agent.Message{{Role: "user", Content: "read it"}}, []string{"read"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "ok" || result.StopReason != "toolUse" || result.ResponseID != "resp-1" || result.Usage.TotalTokens != 5 {
		t.Fatalf("result=%#v", result)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Args["path"] != "a.txt" {
		t.Fatalf("tool calls=%#v", result.ToolCalls)
	}
}

func TestTheosesMessagesPreservesThinkingSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"thinking_start\",\"contentIndex\":0}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"thinking_delta\",\"contentIndex\":0,\"delta\":\"plan\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"thinking_end\",\"contentIndex\":0,\"content\":\"plan\",\"contentSignature\":\"sig-1\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"done\",\"reason\":\"stop\",\"usage\":{}}\n\n"))
	}))
	defer server.Close()

	provider := NewTheosesMessages(server.URL, "radius-key", "auto")
	result, err := provider.Next(context.Background(), []agent.Message{{Role: "user", Content: "think"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Thinking != "plan" || result.ThinkingSignature != "sig-1" {
		t.Fatalf("result=%#v", result)
	}
}

func TestTheosesMessagesPreservesErrorMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"error\",\"reason\":\"error\",\"responseId\":\"resp-error\",\"errorMessage\":\"upstream failed\",\"usage\":{\"input\":4,\"output\":2,\"totalTokens\":6}}\n\n"))
	}))
	defer server.Close()

	result, err := NewTheosesMessages(server.URL, "radius-key", "auto").Next(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != "error" || result.ErrorMessage != "upstream failed" || result.ResponseID != "resp-error" || result.Usage.TotalTokens != 6 {
		t.Fatalf("result=%#v", result)
	}
}

func TestTheosesMessagesPreservesAbortedErrorReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"error\",\"reason\":\"aborted\",\"errorMessage\":\"cancelled\"}\n\n"))
	}))
	defer server.Close()

	result, err := NewTheosesMessages(server.URL, "radius-key", "auto").Next(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != "aborted" {
		t.Fatalf("stop reason=%q", result.StopReason)
	}
}
func TestTheosesMessagesListsGatewayModelsDeterministically(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/config" || r.Header.Get("Authorization") != "Bearer radius-key" {
			t.Fatalf("path=%q authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"models":[{"id":"z"},{"id":""},{"id":"a"}]}`))
	}))
	defer server.Close()

	provider := NewTheosesMessages("", "radius-key", "auto")
	provider.GatewayURL = server.URL + "/"
	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "a" || models[1].ID != "z" || models[0].Provider != "radius" {
		t.Fatalf("models=%#v", models)
	}
}

func TestRadiusUsesYenOwnedConfiguration(t *testing.T) {
	t.Setenv("YEN_PROVIDER", "radius")
	t.Setenv("YEN_MODEL", "radius-model")
	t.Setenv("YEN_RADIUS_BASE_URL", "http://radius.example/v1")
	t.Setenv("YEN_RADIUS_GATEWAY", "http://gateway.example")
	t.Setenv("YEN_RADIUS_API_KEY", "radius-key")
	t.Setenv("RADIUS_API_KEY", "other-key")

	configured := ConfiguredFromEnv()
	client, ok := configured.(TheosesMessages)
	if !ok || client.BaseURL != "http://radius.example/v1" || client.GatewayURL != "http://gateway.example" || client.APIKey != "radius-key" || client.Model != "radius-model" {
		t.Fatalf("configured=%#v", configured)
	}
	name, model := Describe(client)
	if name != "radius" || model != "radius-model" {
		t.Fatalf("provider=%q model=%q", name, model)
	}
}
