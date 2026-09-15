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
	"ant-ling":                   "https://api.ant-ling.com/v1",
	"baseten":                    "https://inference.baseten.co/v1",
	"cerebras":                   "https://api.cerebras.ai/v1",
	"deepseek":                   "https://api.deepseek.com/v1",
	"fireworks":                  "https://api.fireworks.ai/inference",
	"groq":                       "https://api.groq.com/openai/v1",
	"huggingface":                "https://router.huggingface.co/v1",
	"kimi-coding":                "https://api.kimi.com/coding",
	"moonshotai":                 "https://api.moonshot.ai/v1",
	"moonshotai-cn":              "https://api.moonshot.cn/v1",
	"mistral":                    "https://api.mistral.ai/v1",
	"nvidia":                     "https://integrate.api.nvidia.com/v1",
	"openai":                     "https://api.openai.com/v1",
	"openrouter":                 "https://openrouter.ai/api/v1",
	"qwen-token-plan":            "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
	"qwen-token-plan-cn":         "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
	"qwen-token-plan-individual": "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
	"together":                   "https://api.together.ai/v1",
	"xiaomi":                     "https://api.xiaomimimo.com/v1",
	"xiaomi-token-plan-ams":      "https://token-plan-ams.xiaomimimo.com/v1",
	"xiaomi-token-plan-cn":       "https://token-plan-cn.xiaomimimo.com/v1",
	"xiaomi-token-plan-sgp":      "https://token-plan-sgp.xiaomimimo.com/v1",
	"xai":                        "https://api.x.ai/v1",
	"zai":                        "https://api.z.ai/api/coding/paas/v4",
	"zai-coding-cn":              "https://open.bigmodel.cn/api/coding/paas/v4",
}

var providerKeyEnvs = map[string]string{
	"ant-ling":                   "YEN_ANT_LING_API_KEY",
	"baseten":                    "YEN_BASETEN_API_KEY",
	"cerebras":                   "YEN_CEREBRAS_API_KEY",
	"deepseek":                   "YEN_DEEPSEEK_API_KEY",
	"fireworks":                  "YEN_FIREWORKS_API_KEY",
	"groq":                       "YEN_GROQ_API_KEY",
	"huggingface":                "YEN_HF_TOKEN",
	"kimi-coding":                "YEN_KIMI_API_KEY",
	"mistral":                    "YEN_MISTRAL_API_KEY",
	"moonshotai":                 "YEN_MOONSHOT_API_KEY",
	"moonshotai-cn":              "YEN_MOONSHOT_API_KEY",
	"nvidia":                     "YEN_NVIDIA_API_KEY",
	"openai":                     "YEN_OPENAI_API_KEY",
	"openrouter":                 "YEN_OPENROUTER_API_KEY",
	"qwen-token-plan":            "YEN_QWEN_TOKEN_PLAN_API_KEY",
	"qwen-token-plan-cn":         "YEN_QWEN_TOKEN_PLAN_CN_API_KEY",
	"qwen-token-plan-individual": "YEN_QWEN_TOKEN_PLAN_API_KEY",
	"together":                   "YEN_TOGETHER_API_KEY",
	"xiaomi":                     "YEN_XIAOMI_API_KEY",
	"xiaomi-token-plan-ams":      "YEN_XIAOMI_TOKEN_PLAN_AMS_API_KEY",
	"xiaomi-token-plan-cn":       "YEN_XIAOMI_TOKEN_PLAN_CN_API_KEY",
	"xiaomi-token-plan-sgp":      "YEN_XIAOMI_TOKEN_PLAN_SGP_API_KEY",
	"xai":                        "YEN_XAI_API_KEY",
	"zai":                        "YEN_ZAI_API_KEY",
	"zai-coding-cn":              "YEN_ZAI_CODING_CN_API_KEY",
}

func NewFromEnv() OpenAICompletions {
	providerID := strings.ToLower(strings.TrimSpace(os.Getenv("YEN_PROVIDER")))
	model := os.Getenv("YEN_MODEL")
	if strings.Contains(model, "/") && providerID == "" {
		providerID = "openrouter"
	}
	if providerID == "" {
		providerID = "openai"
	}
	baseURL := os.Getenv("YEN_OPENAI_BASE_URL")
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
		key = os.Getenv("YEN_API_KEY")
	}
	client := NewOpenAICompletions(baseURL, key, model)
	client.ProviderName = providerID
	client.ReasoningEffort = os.Getenv("YEN_REASONING_EFFORT")
	return client
}

func ConfiguredFromEnv() agent.Provider {
	providerID := strings.ToLower(strings.TrimSpace(os.Getenv("YEN_PROVIDER")))
	if providerID == "google" {
		baseURL := os.Getenv("YEN_GOOGLE_BASE_URL")
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com/v1beta"
		}
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "gemini-2.5-flash"
		}
		return NewGoogleGenerativeAI(baseURL, os.Getenv("YEN_GOOGLE_API_KEY"), model)
	}
	if providerID != "anthropic" {
		return NewFromEnv()
	}
	baseURL := os.Getenv("YEN_ANTHROPIC_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	model := os.Getenv("YEN_MODEL")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	key := os.Getenv("YEN_ANTHROPIC_API_KEY")
	return NewAnthropicMessages(baseURL, key, model)
}

func NewConfigured(providerID, model string) (agent.Provider, error) {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	model = strings.TrimSpace(model)
	if providerID == "google" {
		if model == "" {
			model = "gemini-2.5-flash"
		}
		baseURL := os.Getenv("YEN_GOOGLE_BASE_URL")
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com/v1beta"
		}
		return NewGoogleGenerativeAI(baseURL, os.Getenv("YEN_GOOGLE_API_KEY"), model), nil
	}
	if providerID == "anthropic" {
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		baseURL := os.Getenv("YEN_ANTHROPIC_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.anthropic.com/v1"
		}
		return NewAnthropicMessages(baseURL, os.Getenv("YEN_ANTHROPIC_API_KEY"), model), nil
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
		key = os.Getenv("YEN_API_KEY")
	}
	client := NewOpenAICompletions(baseURL, key, model)
	client.ProviderName = providerID
	client.ReasoningEffort = os.Getenv("YEN_REASONING_EFFORT")
	return client, nil
}

func Describe(p agent.Provider) (string, string) {
	switch client := p.(type) {
	case OpenAICompletions:
		return client.ProviderName, client.Model
	case AnthropicMessages:
		return "anthropic", client.Model
	case GoogleGenerativeAI:
		return "google", client.Model
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
	case GoogleGenerativeAI:
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
	case GoogleGenerativeAI:
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
	case GoogleGenerativeAI:
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
	case GoogleGenerativeAI:
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
	case GoogleGenerativeAI:
		return client.MaxRetries > 0
	default:
		return false
	}
}
