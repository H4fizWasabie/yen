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

func TestNewFromEnvPreservesExplicitLegacyBaseURL(t *testing.T) {
	t.Setenv("YEN_PROVIDER", "")
	t.Setenv("YEN_MODEL", "")
	t.Setenv("THEOSES_OPENAI_BASE_URL", "http://fixture/v1")
	t.Setenv("OPENAI_API_KEY", "fixture-key")
	client := NewFromEnv()
	if client.BaseURL != "http://fixture/v1" || client.APIKey != "fixture-key" || client.Model != "gpt-4o-mini" {
		t.Fatalf("client=%#v", client)
	}
}
