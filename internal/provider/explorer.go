package provider

import "os"

const explorerModel = "deepseek/deepseek-v4-flash-0731"

// NewExplorerProvider returns the pinned, isolated provider used by the
// explorer sub-agent. The main conversation provider is intentionally not
// reused: Theoses2 gives scouting its own cheap OpenRouter model and routing.
func NewExplorerProvider() OpenAICompletions {
	return OpenAICompletions{
		BaseURL:      providerDefaults["openrouter"],
		APIKey:       os.Getenv("YEN_OPENROUTER_API_KEY"),
		Model:        explorerModel,
		ProviderName: "openrouter",
		MaxTokens:    8000,
		ProviderRouting: map[string]any{
			"order":           []string{"OpenInference", "BaseTen", "GMICloud"},
			"quantizations":   []string{"fp8"},
			"allow_fallbacks": false,
		},
	}
}
