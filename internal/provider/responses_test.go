package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/auth"
	"github.com/coder/websocket"
	"github.com/klauspost/compress/zstd"
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

func TestOpenAICodexSSECompressesRequestAndDecodesResponse(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "zstd" {
			t.Fatalf("content-encoding=%q", r.Header.Get("Content-Encoding"))
		}
		compressed, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		decoder, err := zstd.NewReader(bytes.NewReader(compressed))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decoder.DecodeAll(compressed, nil)
		decoder.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(decoded, &request); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Encoding", "zstd")
		encoder, err := zstd.NewWriter(nil)
		if err != nil {
			t.Fatal(err)
		}
		body := encoder.EncodeAll([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"codex-1\",\"model\":\"gpt-5\",\"status\":\"completed\"}}\n\n"), nil)
		encoder.Close()
		_, _ = w.Write(body)
	}))
	defer server.Close()

	provider := NewOpenAIResponses(server.URL, "codex-token", "gpt-5")
	provider.ProviderName = "openai-codex"
	result, err := provider.Next(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseID != "codex-1" || request["store"] != false {
		t.Fatalf("result=%#v request=%#v", result, request)
	}
}

func TestOpenAICodexWebSocketStreamsResponseCreate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close(websocket.StatusNormalClosure, "done")
		typ, body, err := connection.Read(r.Context())
		if err != nil || typ != websocket.MessageText {
			t.Fatalf("read type=%v err=%v", typ, err)
		}
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		if request["type"] != "response.create" || request["model"] != "gpt-5" {
			t.Fatalf("request=%#v", request)
		}
		if r.Header.Get("OpenAI-Beta") != "responses_websockets=2026-02-06" {
			t.Fatalf("beta=%q", r.Header.Get("OpenAI-Beta"))
		}
		if r.Header.Get("session-id") == "" || r.Header.Get("x-client-request-id") == "" {
			t.Fatalf("session-id=%q request-id=%q", r.Header.Get("session-id"), r.Header.Get("x-client-request-id"))
		}
		for _, event := range []string{
			`{"type":"response.output_text.delta","delta":"hello"}`,
			`{"type":"response.completed","response":{"id":"ws-1","model":"gpt-5","status":"completed"}}`,
		} {
			if err := connection.Write(r.Context(), websocket.MessageText, []byte(event)); err != nil {
				t.Fatal(err)
			}
		}
	}))
	defer server.Close()

	provider := NewOpenAIResponses(server.URL, "codex-token", "gpt-5")
	provider.ProviderName = "openai-codex"
	provider.Transport = "websocket"
	provider.Headers = map[string]string{"chatgpt-account-id": "acct-1"}
	result, err := provider.Next(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hello" || result.ResponseID != "ws-1" {
		t.Fatalf("result=%#v", result)
	}
}

func TestOpenAICodexFallsBackToSSEWhenWebSocketHandshakeFails(t *testing.T) {
	var websocketAttempts, sseAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			websocketAttempts++
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		sseAttempts++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"sse-fallback\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	provider := NewOpenAIResponses(server.URL, "codex-token", "gpt-5")
	provider.ProviderName = "openai-codex"
	provider.Transport = "websocket"
	result, err := provider.Next(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseID != "sse-fallback" || websocketAttempts != 1 || sseAttempts != 1 {
		t.Fatalf("result=%#v websocket=%d sse=%d", result, websocketAttempts, sseAttempts)
	}
}

func TestOpenAIResponsesListsModelsWithConfiguredHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Header.Get("api-key") != "azure-key" || r.Header.Get("X-Account") != "account-1" || r.Header.Get("Authorization") != "" {
			t.Fatalf("path=%q api-key=%q account=%q authorization=%q", r.URL.Path, r.Header.Get("api-key"), r.Header.Get("X-Account"), r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"z-model"},{"id":""},{"id":"a-model"}]}`))
	}))
	defer server.Close()

	provider := NewOpenAIResponses(server.URL, "azure-key", "model")
	provider.ProviderName = "azure-openai-responses"
	provider.APIKeyHeader = "api-key"
	provider.Headers = map[string]string{"X-Account": "account-1"}
	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "z-model" || models[1].Provider != "azure-openai-responses" {
		t.Fatalf("models=%#v", models)
	}
}

func TestOpenAIResponsesListsAllCatalogPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("after") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"id":"first"}],"has_more":true,"last_id":"first"}`))
		case "first":
			_, _ = w.Write([]byte(`{"data":[{"id":"second"}],"has_more":false}`))
		default:
			t.Fatalf("unexpected after=%q", r.URL.Query().Get("after"))
		}
	}))
	defer server.Close()

	models, err := NewOpenAIResponses(server.URL, "key", "model").ListModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "first" || models[1].ID != "second" {
		t.Fatalf("models=%#v err=%v", models, err)
	}
}

func TestOpenAICodexListsPaginatedCatalogWithAccountHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer codex-token" || r.Header.Get("chatgpt-account-id") != "acct-1" {
			t.Fatalf("authorization=%q account=%q", r.Header.Get("Authorization"), r.Header.Get("chatgpt-account-id"))
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5"}],"has_more":true,"last_id":"gpt-5"}`)
			return
		}
		if r.URL.Query().Get("after") != "gpt-5" {
			t.Fatalf("after=%q", r.URL.Query().Get("after"))
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5-mini"}],"has_more":false}`)
	}))
	defer server.Close()

	provider := NewOpenAIResponses(server.URL, "codex-token", "gpt-5")
	provider.ProviderName = "openai-codex"
	provider.Headers = map[string]string{"chatgpt-account-id": "acct-1"}
	models, err := provider.ListModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "gpt-5" || models[1].ID != "gpt-5-mini" || models[0].Provider != "openai-codex" {
		t.Fatalf("models=%#v err=%v", models, err)
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

func TestOpenAIResponsesBackfillsReasoningSignatureFromTerminalOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"plan\"}]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"output\":[{\"type\":\"reasoning\",\"id\":\"rs-1\",\"encrypted_content\":\"sig-terminal\"}]}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponses(server.URL, "responses-key", "gpt-5")
	result, err := provider.Next(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ThinkingSignature, "sig-terminal") {
		t.Fatalf("signature=%q", result.ThinkingSignature)
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

func TestOpenAIResponsesUsesStoredYenCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if _, err := auth.Open(path).Modify("openai-responses", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: "oauth", Access: "stored-responses-token"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_AUTH_FILE", path)
	t.Setenv("YEN_OPENAI_API_KEY", "")
	configured, err := NewConfigured("openai-responses", "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	client, ok := configured.(OpenAIResponses)
	if !ok || client.APIKey != "stored-responses-token" {
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
