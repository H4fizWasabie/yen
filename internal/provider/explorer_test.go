package provider

import "testing"

func TestNewExplorerProviderUsesPinnedOpenRouterModelAndRouting(t *testing.T) {
	t.Setenv("YEN_OPENROUTER_API_KEY", "explorer-key")
	client := NewExplorerProvider()
	if client.ProviderName != "openrouter" || client.Model != explorerModel || client.APIKey != "explorer-key" || client.BaseURL != providerDefaults["openrouter"] {
		t.Fatalf("client=%#v", client)
	}
	if client.ProviderRouting["allow_fallbacks"] != false {
		t.Fatalf("routing=%#v", client.ProviderRouting)
	}
}
