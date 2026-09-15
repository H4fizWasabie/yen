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

func TestOpenAIResponsesStreamsTextAndFunctionCall(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer responses-key" {
			t.Fatalf("path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-1\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc-1\",\"call_id\":\"call-1\",\"name\":\"read\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":1,\"call_id\":\"call-1\",\"delta\":\"{\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.function_call_arguments.done\",\"output_index\":1,\"call_id\":\"call-1\",\"arguments\":\"{\\\"path\\\":\\\"x\\\"}\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc-1\",\"call_id\":\"call-1\",\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"x\\\"}\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"model\":\"gpt-4.1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":4,\"total_tokens\":14}}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponses(server.URL+"/v1", "responses-key", "gpt-4.1")
	result, err := provider.Next(context.Background(), []agent.Message{{Role: "system", Content: "be concise"}, {Role: "user", Content: "read x", Images: []string{"data:image/png;base64,AA=="}}}, []string{"read"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello" || result.ResponseID != "resp-1" || result.ResponseModel != "gpt-4.1" || result.StopReason != "toolUse" || len(result.ToolCalls) != 1 || result.ToolCalls[0].Args["path"] != "x" {
		t.Fatalf("result=%#v", result)
	}
	if result.Usage.TotalTokens != 14 || !strings.Contains(string(mustJSON(t, request["input"])), "input_image") {
		t.Fatalf("usage=%#v request=%#v", result.Usage, request)
	}
	if _, ok := request["tools"]; !ok {
		t.Fatalf("tools missing: %#v", request)
	}
}

func TestOpenAIResponsesPersistsReasoningItemSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"plan\"}],\"encrypted_content\":\"sig-1\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"model\":\"gpt-5\",\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponses(server.URL, "responses-key", "gpt-5")
	result, err := provider.Next(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Thinking != "plan" || result.ThinkingSignature == "" || !strings.Contains(result.ThinkingSignature, "sig-1") {
		t.Fatalf("result=%#v", result)
	}
}

func TestOpenAIResponsesReturnsResponseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"upstream failed\"}}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponses(server.URL, "responses-key", "gpt-5")
	if _, err := provider.Next(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "server_error: upstream failed") {
		t.Fatalf("error=%v, want provider failure", err)
	}
}

func TestOpenAIResponsesUsesYenConfiguration(t *testing.T) {
	t.Setenv("YEN_PROVIDER", "openai-responses")
	t.Setenv("YEN_MODEL", "responses-model")
	t.Setenv("YEN_OPENAI_API_KEY", "responses-key")
	configured := ConfiguredFromEnv()
	client, ok := configured.(OpenAIResponses)
	if !ok || client.ProviderName != "openai-responses" || client.Model != "responses-model" || client.APIKey != "responses-key" {
		t.Fatalf("configured=%#v", configured)
	}
}

func TestAzureResponsesUsesAzureRouteAndAPIKeyHeader(t *testing.T) {
	t.Setenv("YEN_PROVIDER", "azure-openai-responses")
	t.Setenv("YEN_MODEL", "deployment-1")
	t.Setenv("YEN_AZURE_OPENAI_BASE_URL", "http://fixture/openai")
	t.Setenv("YEN_AZURE_OPENAI_API_KEY", "azure-key")
	client, ok := ConfiguredFromEnv().(OpenAIResponses)
	if !ok || client.ProviderName != "azure-openai-responses" || client.BaseURL != "http://fixture/openai/v1" || client.APIKeyHeader != "api-key" || client.APIKey != "azure-key" {
		t.Fatalf("client=%#v", client)
	}
}

func TestOpenAIAndXAIUseResponsesProtocolConfiguration(t *testing.T) {
	t.Setenv("YEN_OPENAI_API_KEY", "openai-key")
	openai, err := NewConfigured("openai", "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	openAIClient, ok := openai.(OpenAIResponses)
	if !ok || openAIClient.ProviderName != "openai" || openAIClient.BaseURL != "https://api.openai.com/v1" || openAIClient.APIKey != "openai-key" {
		t.Fatalf("openai=%#v", openai)
	}
	t.Setenv("YEN_XAI_API_KEY", "xai-key")
	t.Setenv("YEN_XAI_BASE_URL", "http://fixture/v1")
	xai, err := NewConfigured("xai", "grok-4.6")
	if err != nil {
		t.Fatal(err)
	}
	xaiClient, ok := xai.(OpenAIResponses)
	if !ok || xaiClient.ProviderName != "xai" || xaiClient.BaseURL != "http://fixture/v1" || xaiClient.APIKey != "xai-key" {
		t.Fatalf("xai=%#v", xai)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
