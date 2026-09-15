package provider

import "testing"

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
