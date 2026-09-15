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
