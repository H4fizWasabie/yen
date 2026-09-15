package provider

import (
	"context"
	"errors"
	"fmt"
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

func NewConfigured(providerID, model string) (agent.Provider, error) {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	model = strings.TrimSpace(model)
	if providerID == "anthropic" {
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		baseURL := firstEnv("YEN_ANTHROPIC_BASE_URL", "THEOSES_ANTHROPIC_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.anthropic.com/v1"
		}
		return NewAnthropicMessages(baseURL, firstEnv("YEN_ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY"), model), nil
	}
	baseURL, ok := providerDefaults[providerID]
	if !ok {
		return nil, fmt.Errorf("unsupported provider %q", providerID)
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
	return client, nil
}

func Describe(p agent.Provider) (string, string) {
	switch client := p.(type) {
	case OpenAICompletions:
		return client.ProviderName, client.Model
	case AnthropicMessages:
		return "anthropic", client.Model
	default:
		return "", ""
	}
}

type ModelInfo struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

type ModelLister interface {
	ListModels(context.Context) ([]ModelInfo, error)
}

func SetModel(p agent.Provider, model string) (agent.Provider, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("model is required")
	}
	switch client := p.(type) {
	case OpenAICompletions:
		client.Model = model
		return client, nil
	case AnthropicMessages:
		client.Model = model
		return client, nil
	default:
		return nil, errors.New("provider does not support model selection")
	}
}

func AvailableModels(ctx context.Context, p agent.Provider) ([]ModelInfo, error) {
	if lister, ok := p.(ModelLister); ok {
		return lister.ListModels(ctx)
	}
	name, model := Describe(p)
	if name == "" || model == "" {
		return nil, errors.New("provider does not expose a model catalog")
	}
	return []ModelInfo{{Provider: name, ID: model}}, nil
}

var ThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

func SetThinkingLevel(p agent.Provider, level string) (agent.Provider, error) {
	level = strings.ToLower(strings.TrimSpace(level))
	valid := false
	for _, candidate := range ThinkingLevels {
		if candidate == level {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("unsupported thinking level %q", level)
	}
	switch client := p.(type) {
	case OpenAICompletions:
		client.ReasoningEffort = ""
		if level != "off" {
			client.ReasoningEffort = level
		}
		return client, nil
	case AnthropicMessages:
		client.ThinkingLevel = level
		return client, nil
	default:
		return nil, errors.New("provider does not support thinking levels")
	}
}

func ThinkingLevel(p agent.Provider) string {
	switch client := p.(type) {
	case OpenAICompletions:
		if client.ReasoningEffort == "" {
			return "off"
		}
		return client.ReasoningEffort
	case AnthropicMessages:
		if client.ThinkingLevel == "" {
			return "off"
		}
		return client.ThinkingLevel
	default:
		return "off"
	}
}

func SetRetryEnabled(p agent.Provider, enabled bool) (agent.Provider, error) {
	const defaultRetries = 3
	switch client := p.(type) {
	case OpenAICompletions:
		if enabled && client.MaxRetries == 0 {
			client.MaxRetries = defaultRetries
		}
		if !enabled {
			client.MaxRetries = 0
		}
		return client, nil
	case AnthropicMessages:
		if enabled && client.MaxRetries == 0 {
			client.MaxRetries = defaultRetries
		}
		if !enabled {
			client.MaxRetries = 0
		}
		return client, nil
	default:
		return nil, errors.New("provider does not support retry control")
	}
}

func RetryEnabled(p agent.Provider) bool {
	switch client := p.(type) {
	case OpenAICompletions:
		return client.MaxRetries > 0
	case AnthropicMessages:
		return client.MaxRetries > 0
	default:
		return false
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
