package provider

import (
	"os"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
)

var providerDefaults = map[string]string{
	"openai":     "https://api.openai.com/v1",
	"openrouter": "https://openrouter.ai/api/v1",
	"deepseek":   "https://api.deepseek.com/v1",
	"groq":       "https://api.groq.com/openai/v1",
	"mistral":    "https://api.mistral.ai/v1",
	"moonshotai": "https://api.moonshot.ai/v1",
	"xai":        "https://api.x.ai/v1",
	"zai":        "https://api.z.ai/api/paas/v4",
}

var providerKeyEnvs = map[string]string{
	"openai":     "YEN_OPENAI_API_KEY",
	"openrouter": "YEN_OPENROUTER_API_KEY",
	"deepseek":   "YEN_DEEPSEEK_API_KEY",
	"groq":       "YEN_GROQ_API_KEY",
	"mistral":    "YEN_MISTRAL_API_KEY",
	"moonshotai": "YEN_MOONSHOT_API_KEY",
	"xai":        "YEN_XAI_API_KEY",
	"zai":        "YEN_ZAI_API_KEY",
}

func NewFromEnv() OpenAICompletions {
	providerID := strings.ToLower(strings.TrimSpace(firstEnv("YEN_PROVIDER", "THEOSES_PROVIDER")))
	model := firstEnv("YEN_MODEL", "THEOSES_MODEL")
	if strings.Contains(model, "/") && providerID == "" {
		providerID = "openrouter"
	}
	if providerID == "" {
		providerID = "openai"
	}
	baseURL := firstEnv("YEN_OPENAI_BASE_URL", "THEOSES_OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = providerDefaults[providerID]
	}
	if baseURL == "" {
		baseURL = providerDefaults["openai"]
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	key := ""
	if env := providerKeyEnvs[providerID]; env != "" {
		key = os.Getenv(env)
	}
	if key == "" {
		key = firstEnv("YEN_API_KEY", "OPENROUTER_API_KEY", "OPENAI_API_KEY")
	}
	client := NewOpenAICompletions(baseURL, key, model)
	client.ProviderName = providerID
	client.ReasoningEffort = firstEnv("YEN_REASONING_EFFORT", "THEOSES_REASONING_EFFORT")
	return client
}

func ConfiguredFromEnv() agent.Provider {
	providerID := strings.ToLower(strings.TrimSpace(firstEnv("YEN_PROVIDER", "THEOSES_PROVIDER")))
	if providerID != "anthropic" {
		return NewFromEnv()
	}
	baseURL := firstEnv("YEN_ANTHROPIC_BASE_URL", "THEOSES_ANTHROPIC_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	model := firstEnv("YEN_MODEL", "THEOSES_MODEL")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	key := firstEnv("YEN_ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY")
	return NewAnthropicMessages(baseURL, key, model)
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
