package provider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/auth"
)

func TestCloudflareProvidersUseYenCredentialsAndRouting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account/gateway/chat/completions" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if r.Header.Get("cf-aig-authorization") != "Bearer gateway-key" {
			t.Fatalf("gateway auth=%q", r.Header.Get("cf-aig-authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	t.Setenv("YEN_CLOUDFLARE_API_KEY", "gateway-key")
	t.Setenv("YEN_CLOUDFLARE_BASE_URL", server.URL+"/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}")
	t.Setenv("YEN_CLOUDFLARE_ACCOUNT_ID", "account")
	t.Setenv("YEN_CLOUDFLARE_GATEWAY_ID", "gateway")
	configured, err := NewConfigured("cloudflare-ai-gateway", "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	client := configured.(OpenAICompletions)
	client.Client = server.Client()
	result, err := client.Next(context.Background(), nil, nil)
	if err != nil || result.Text != "ok" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if client.BaseURL != server.URL+"/v1/account/gateway" {
		t.Fatalf("base URL=%q", client.BaseURL)
	}
}

func TestGitHubCopilotUsesStoredYenCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if _, err := auth.Open(path).Modify("github-copilot", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: "oauth", Access: "stored-copilot-token"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_AUTH_FILE", path)
	t.Setenv("YEN_PROVIDER", "github-copilot")
	t.Setenv("YEN_COPILOT_GITHUB_TOKEN", "")
	configured, err := NewConfigured("github-copilot", "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	client, ok := configured.(OpenAICompletions)
	if !ok || client.APIKey != "stored-copilot-token" {
		t.Fatalf("configured=%#v", configured)
	}
}

func TestGoogleVertexUsesProjectEndpointAndYenAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/project/locations/asia-southeast1/publishers/google/models/fixture-model:streamGenerateContent" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "vertex-key" {
			t.Fatalf("key=%q", r.URL.Query().Get("key"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()
	t.Setenv("YEN_GOOGLE_VERTEX_BASE_URL", server.URL+"/v1/projects/project/locations/asia-southeast1/publishers/google")
	t.Setenv("YEN_GOOGLE_CLOUD_API_KEY", "vertex-key")
	configured, err := NewConfigured("google-vertex", "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	client := configured.(GoogleGenerativeAI)
	client.Client = server.Client()
	result, err := client.Next(context.Background(), []agent.Message{{Role: "user", Content: "hello"}}, nil)
	if err != nil || result.Provider != "google-vertex" || result.Text != "ok" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestGoogleVertexUsesYenBearerTokenWithoutAPIKeyQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "" || r.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("query=%q authorization=%q", r.URL.Query().Get("key"), r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()
	t.Setenv("YEN_GOOGLE_VERTEX_BASE_URL", server.URL)
	t.Setenv("YEN_GOOGLE_VERTEX_ACCESS_TOKEN", "access-token")
	configured, err := NewConfigured("google-vertex", "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	client := configured.(GoogleGenerativeAI)
	client.Client = server.Client()
	if _, err := client.Next(context.Background(), []agent.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestVertexServiceAccountExchangesJWTForBearerToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	var exchanges atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatalf("request=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatalf("form=%v err=%v", r.Form, err)
		}
		parts := strings.Split(r.Form.Get("assertion"), ".")
		if len(parts) != 3 {
			t.Fatalf("assertion=%q", r.Form.Get("assertion"))
		}
		_, _ = fmt.Fprint(w, `{"access_token":"vertex-access","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "vertex@example.com",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})),
		"token_uri":   tokenServer.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "service-account.json")
	if err := os.WriteFile(path, credentials, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_GOOGLE_APPLICATION_CREDENTIALS", path)
	t.Setenv("YEN_GOOGLE_VERTEX_BASE_URL", tokenServer.URL)
	client := googleVertexConfigured("fixture-model")
	configuredToken, err := client.bearerToken(context.Background())
	if err != nil || configuredToken != "vertex-access" {
		t.Fatalf("configured token=%q err=%v", configuredToken, err)
	}
	source := vertexServiceAccountSource(path)
	token, err := source(context.Background())
	if err != nil || token != "vertex-access" {
		t.Fatalf("token=%q err=%v", token, err)
	}
	if token, err = source(context.Background()); err != nil || token != "vertex-access" {
		t.Fatalf("cached token=%q err=%v", token, err)
	}
	if exchanges.Load() != 2 {
		t.Fatalf("token exchanges=%d, want 2 including configured source", exchanges.Load())
	}
}

func TestVertexAuthorizedUserRefreshesADCForBearerToken(t *testing.T) {
	var exchanges atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("client_id") != "client-id" || r.Form.Get("client_secret") != "client-secret" || r.Form.Get("refresh_token") != "refresh-token" {
			t.Fatalf("form=%v err=%v", r.Form, err)
		}
		_, _ = fmt.Fprint(w, `{"access_token":"adc-access","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	credentials, err := json.Marshal(map[string]string{
		"type": "authorized_user", "client_id": "client-id", "client_secret": "client-secret",
		"refresh_token": "refresh-token", "token_uri": tokenServer.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "application-default-credentials.json")
	if err := os.WriteFile(path, credentials, 0o600); err != nil {
		t.Fatal(err)
	}
	source := vertexServiceAccountSource(path)
	for i := 0; i < 2; i++ {
		token, err := source(context.Background())
		if err != nil || token != "adc-access" {
			t.Fatalf("token=%q err=%v", token, err)
		}
	}
	if exchanges.Load() != 1 {
		t.Fatalf("token exchanges=%d, want 1", exchanges.Load())
	}
}

func TestGitHubCopilotUsesDynamicHeadersAndYenToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer copilot-token" || r.Header.Get("X-Initiator") != "agent" || r.Header.Get("Openai-Intent") != "conversation-edits" || r.Header.Get("Copilot-Vision-Request") != "true" {
			t.Fatalf("path=%q authorization=%q initiator=%q intent=%q vision=%q", r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Initiator"), r.Header.Get("Openai-Intent"), r.Header.Get("Copilot-Vision-Request"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	t.Setenv("YEN_COPILOT_BASE_URL", server.URL)
	t.Setenv("YEN_COPILOT_GITHUB_TOKEN", "copilot-token")
	configured, err := NewConfigured("github-copilot", "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	client := configured.(OpenAICompletions)
	client.Client = server.Client()
	result, err := client.Next(context.Background(), []agent.Message{{Role: "assistant", Content: "prior", Images: []string{"data:image/png;base64,AA=="}}}, nil)
	if err != nil || result.Provider != "github-copilot" || result.Text != "ok" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestGitHubCopilotUsesStoredAPIKeyCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if _, err := auth.Open(path).Modify("github-copilot", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: "api_key", Key: "stored-copilot"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_AUTH_FILE", path)
	t.Setenv("YEN_PROVIDER", "github-copilot")
	t.Setenv("YEN_COPILOT_GITHUB_TOKEN", "")
	client := ConfiguredFromEnv()
	configured, ok := client.(OpenAICompletions)
	if !ok || configured.APIKey != "stored-copilot" {
		t.Fatalf("configured=%#v", client)
	}
}

func TestOpenAICodexUsesAccountAndExperimentalHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/codex/responses" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer header.") || r.Header.Get("chatgpt-account-id") != "acct-1" || r.Header.Get("originator") != "theoses" || r.Header.Get("OpenAI-Beta") != "responses=experimental" {
			t.Fatalf("path=%q auth=%q account=%q originator=%q beta=%q", r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("chatgpt-account-id"), r.Header.Get("originator"), r.Header.Get("OpenAI-Beta"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"model\":\"gpt-5\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
	}))
	defer server.Close()
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-1"}}`))
	t.Setenv("YEN_OPENAI_CODEX_ACCESS_TOKEN", "header."+payload+".signature")
	t.Setenv("YEN_OPENAI_CODEX_BASE_URL", server.URL+"/codex")
	configured, err := NewConfigured("openai-codex", "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	client := configured.(OpenAIResponses)
	client.Client = server.Client()
	result, err := client.Next(context.Background(), []agent.Message{{Role: "user", Content: "hello"}}, nil)
	if err != nil || result.Provider != "openai-codex" || result.Text != "ok" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestOpenAICodexUsesStoredYenCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	token := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-stored"}}`)) + ".signature"
	if _, err := auth.Open(path).Modify("openai-codex", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: "oauth", Access: token}, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_AUTH_FILE", path)
	t.Setenv("YEN_OPENAI_CODEX_ACCESS_TOKEN", "")
	configured, err := NewConfigured("openai-codex", "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	client, ok := configured.(OpenAIResponses)
	if !ok || client.APIKey != token || client.Headers["chatgpt-account-id"] != "acct-stored" {
		t.Fatalf("configured=%#v", configured)
	}
}

func TestNewFromEnvPrefersYenProviderCredentials(t *testing.T) {
	t.Setenv("YEN_PROVIDER", "openrouter")
	t.Setenv("YEN_MODEL", "z-ai/glm-5.3-flash")
	t.Setenv("YEN_OPENROUTER_API_KEY", "yen-key")
	t.Setenv("OPENAI_API_KEY", "other-key")
	client := NewFromEnv()
	if client.ProviderName != "openrouter" || client.BaseURL != providerDefaults["openrouter"] || client.APIKey != "yen-key" || client.Model != "z-ai/glm-5.3-flash" {
		t.Fatalf("client=%#v", client)
	}
}

func TestNewFromEnvUsesOnlyYenOwnedEnvironment(t *testing.T) {
	t.Setenv("YEN_PROVIDER", "")
	t.Setenv("YEN_MODEL", "")
	t.Setenv("YEN_API_KEY", "")
	t.Setenv("YEN_OPENAI_BASE_URL", "http://fixture/v1")
	t.Setenv("OPENAI_API_KEY", "fixture-key")
	client := NewFromEnv()
	if client.BaseURL != "http://fixture/v1" || client.APIKey != "" || client.Model != "gpt-4o-mini" {
		t.Fatalf("client=%#v", client)
	}
}

func TestNewConfiguredSupportsOpenAICompatibleAndAnthropicProviders(t *testing.T) {
	t.Setenv("YEN_OPENROUTER_API_KEY", "yen-router")
	configured, err := NewConfigured("openrouter", "router-model")
	if err != nil {
		t.Fatal(err)
	}
	name, model := Describe(configured)
	if name != "openrouter" || model != "router-model" {
		t.Fatalf("provider=%q model=%q", name, model)
	}
	configured, err = NewConfigured("anthropic", "claude-model")
	if err != nil {
		t.Fatal(err)
	}
	name, model = Describe(configured)
	if name != "anthropic" || model != "claude-model" {
		t.Fatalf("provider=%q model=%q", name, model)
	}
}

func TestNewConfiguredSupportsPinnedOpenAICompatibleProviders(t *testing.T) {
	providers := []struct {
		id  string
		env string
	}{
		{"ant-ling", "YEN_ANT_LING_API_KEY"},
		{"baseten", "YEN_BASETEN_API_KEY"},
		{"cerebras", "YEN_CEREBRAS_API_KEY"},
		{"huggingface", "YEN_HF_TOKEN"},
		{"moonshotai-cn", "YEN_MOONSHOT_API_KEY"},
		{"nvidia", "YEN_NVIDIA_API_KEY"},
		{"opencode", "YEN_OPENCODE_API_KEY"},
		{"opencode-go", "YEN_OPENCODE_API_KEY"},
		{"qwen-token-plan", "YEN_QWEN_TOKEN_PLAN_API_KEY"},
		{"qwen-token-plan-cn", "YEN_QWEN_TOKEN_PLAN_CN_API_KEY"},
		{"together", "YEN_TOGETHER_API_KEY"},
		{"xiaomi-token-plan-sgp", "YEN_XIAOMI_TOKEN_PLAN_SGP_API_KEY"},
		{"zai-coding-cn", "YEN_ZAI_CODING_CN_API_KEY"},
	}
	for _, test := range providers {
		t.Run(test.id, func(t *testing.T) {
			t.Setenv(test.env, "provider-key")
			configured, err := NewConfigured(test.id, "fixture-model")
			if err != nil {
				t.Fatal(err)
			}
			client, ok := configured.(OpenAICompletions)
			if !ok || client.ProviderName != test.id || client.BaseURL != providerDefaults[test.id] || client.APIKey != "provider-key" {
				t.Fatalf("provider=%#v", configured)
			}
		})
	}
}

func TestNewConfiguredSelectsFireworksProtocolByModel(t *testing.T) {
	t.Setenv("YEN_FIREWORKS_API_KEY", "fireworks-key")
	t.Setenv("YEN_REASONING_EFFORT", "high")
	configured, err := NewConfigured("fireworks", "accounts/fireworks/models/glm-5p3-flash")
	if err != nil {
		t.Fatal(err)
	}
	anthropic, ok := configured.(AnthropicMessages)
	if !ok || anthropic.ProviderName != "fireworks" || anthropic.BaseURL != "https://api.fireworks.ai/inference" || anthropic.APIKey != "fireworks-key" || anthropic.ThinkingLevel != "high" {
		t.Fatalf("provider=%#v", configured)
	}

	configured, err = NewConfigured("fireworks", "accounts/fireworks/models/glm-5p2")
	if err != nil {
		t.Fatal(err)
	}
	openAI, ok := configured.(OpenAICompletions)
	if !ok || openAI.ProviderName != "fireworks" || openAI.BaseURL != providerDefaults["fireworks"] {
		t.Fatalf("openai provider=%#v", configured)
	}
}

func TestNewConfiguredSupportsMiniMaxAnthropicProviders(t *testing.T) {
	for _, test := range []struct {
		id  string
		env string
	}{
		{"minimax", "YEN_MINIMAX_API_KEY"},
		{"minimax-cn", "YEN_MINIMAX_CN_API_KEY"},
	} {
		t.Run(test.id, func(t *testing.T) {
			t.Setenv(test.env, "provider-key")
			configured, err := NewConfigured(test.id, "fixture-model")
			if err != nil {
				t.Fatal(err)
			}
			client, ok := configured.(AnthropicMessages)
			if !ok || client.ProviderName != test.id || client.BaseURL != providerDefaults[test.id] || client.APIKey != "provider-key" || client.Model != "fixture-model" {
				t.Fatalf("provider=%#v", configured)
			}
		})
	}
}

func TestNewConfiguredUsesAnthropicProtocolForKimiCoding(t *testing.T) {
	t.Setenv("YEN_KIMI_API_KEY", "kimi-key")
	t.Setenv("YEN_REASONING_EFFORT", "high")
	configured, err := NewConfigured("kimi-coding", "kimi-for-coding")
	if err != nil {
		t.Fatal(err)
	}
	client, ok := configured.(AnthropicMessages)
	if !ok || client.ProviderName != "kimi-coding" || client.BaseURL != providerDefaults["kimi-coding"] || client.APIKey != "kimi-key" || client.ThinkingLevel != "high" {
		t.Fatalf("provider=%#v", configured)
	}
}

func TestNewConfiguredSupportsVercelAIGateway(t *testing.T) {
	t.Setenv("YEN_VERCEL_AI_GATEWAY_API_KEY", "gateway-key")
	configured, err := NewConfigured("vercel-ai-gateway", "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	client, ok := configured.(AnthropicMessages)
	if !ok || client.ProviderName != "vercel-ai-gateway" || client.BaseURL != providerDefaults["vercel-ai-gateway"] || client.APIKey != "gateway-key" || client.Model != "fixture-model" {
		t.Fatalf("provider=%#v", configured)
	}
}

func TestNewConfiguredReadsOnlyExplicitYenAuthFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if _, err := auth.Open(path).Modify("openrouter", func(*auth.Credential) (*auth.Credential, error) {
		return &auth.Credential{Type: "oauth", Access: "stored-access"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YEN_AUTH_FILE", path)
	t.Setenv("YEN_OPENROUTER_API_KEY", "")
	t.Setenv("YEN_API_KEY", "")
	configured, err := NewConfigured("openrouter", "stored-model")
	if err != nil {
		t.Fatal(err)
	}
	client, ok := configured.(OpenAICompletions)
	if !ok || client.APIKey != "stored-access" {
		t.Fatalf("client=%#v", configured)
	}
}

func TestSetThinkingLevelShapesProviderState(t *testing.T) {
	openai := NewOpenAICompletions("http://fixture", "key", "model")
	configured, err := SetThinkingLevel(openai, "high")
	if err != nil || ThinkingLevel(configured) != "high" {
		t.Fatalf("provider=%#v err=%v", configured, err)
	}
	anthropic := NewAnthropicMessages("http://fixture", "key", "model")
	configured, err = SetThinkingLevel(anthropic, "medium")
	if err != nil || ThinkingLevel(configured) != "medium" {
		t.Fatalf("provider=%#v err=%v", configured, err)
	}
}

func TestConfiguredNativeProvidersLoadReasoningEffort(t *testing.T) {
	t.Setenv("YEN_REASONING_EFFORT", "high")
	t.Setenv("YEN_GOOGLE_API_KEY", "google-key")
	configured, err := NewConfigured("google", "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	if ThinkingLevel(configured) != "high" {
		t.Fatalf("google thinking=%q", ThinkingLevel(configured))
	}
	t.Setenv("YEN_ANTHROPIC_API_KEY", "anthropic-key")
	configured, err = NewConfigured("anthropic", "claude-test")
	if err != nil {
		t.Fatal(err)
	}
	if ThinkingLevel(configured) != "high" {
		t.Fatalf("anthropic thinking=%q", ThinkingLevel(configured))
	}
}

func TestSetRetryEnabledControlsSupportedProviders(t *testing.T) {
	configured, err := SetRetryEnabled(NewOpenAICompletions("http://fixture", "key", "model"), true)
	if err != nil || !RetryEnabled(configured) {
		t.Fatalf("provider=%#v err=%v", configured, err)
	}
	configured, err = SetRetryEnabled(configured, false)
	if err != nil || RetryEnabled(configured) {
		t.Fatalf("provider=%#v err=%v", configured, err)
	}
}
