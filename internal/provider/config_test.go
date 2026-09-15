package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
		{"fireworks", "YEN_FIREWORKS_API_KEY"},
		{"huggingface", "YEN_HF_TOKEN"},
		{"kimi-coding", "YEN_KIMI_API_KEY"},
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
