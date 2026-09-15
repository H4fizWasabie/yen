package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
	"github.com/H4fizWasabie/yen/internal/auth"
)

var providerDefaults = map[string]string{
	"amazon-bedrock":             "",
	"ant-ling":                   "https://api.ant-ling.com/v1",
	"baseten":                    "https://inference.baseten.co/v1",
	"cerebras":                   "https://api.cerebras.ai/v1",
	"deepseek":                   "https://api.deepseek.com/v1",
	"fireworks":                  "https://api.fireworks.ai/inference/v1",
	"groq":                       "https://api.groq.com/openai/v1",
	"huggingface":                "https://router.huggingface.co/v1",
	"kimi-coding":                "https://api.kimi.com/coding",
	"moonshotai":                 "https://api.moonshot.ai/v1",
	"moonshotai-cn":              "https://api.moonshot.cn/v1",
	"mistral":                    "https://api.mistral.ai/v1",
	"minimax":                    "https://api.minimax.io/anthropic",
	"minimax-cn":                 "https://api.minimaxi.com/anthropic",
	"nvidia":                     "https://integrate.api.nvidia.com/v1",
	"openai":                     "https://api.openai.com/v1",
	"openrouter":                 "https://openrouter.ai/api/v1",
	"opencode":                   "https://opencode.ai/zen/v1",
	"opencode-go":                "https://opencode.ai/zen/go/v1",
	"qwen-token-plan":            "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
	"qwen-token-plan-cn":         "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
	"qwen-token-plan-individual": "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
	"radius":                     "",
	"together":                   "https://api.together.ai/v1",
	"xiaomi":                     "https://api.xiaomimimo.com/v1",
	"xiaomi-token-plan-ams":      "https://token-plan-ams.xiaomimimo.com/v1",
	"xiaomi-token-plan-cn":       "https://token-plan-cn.xiaomimimo.com/v1",
	"xiaomi-token-plan-sgp":      "https://token-plan-sgp.xiaomimimo.com/v1",
	"xai":                        "https://api.x.ai/v1",
	"zai":                        "https://api.z.ai/api/coding/paas/v4",
	"zai-coding-cn":              "https://open.bigmodel.cn/api/coding/paas/v4",
	"vercel-ai-gateway":          "https://ai-gateway.vercel.sh",
	"cloudflare-workers-ai":      "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai/v1",
	"cloudflare-ai-gateway":      "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/compat",
	"github-copilot":             "https://api.individual.githubcopilot.com",
	"openai-codex":               "https://chatgpt.com/backend-api/codex",
	"azure-openai-responses":     "",
}

var fireworksAnthropicModels = map[string]struct{}{
	"accounts/fireworks/models/deepseek-v4-flash-0731":         {},
	"accounts/fireworks/models/deepseek-v4-flash-vision-exp":   {},
	"accounts/fireworks/models/deepseek-v4-pro-0813":           {},
	"accounts/fireworks/models/deepseek-v4p1-flash":            {},
	"accounts/fireworks/models/glm-5p3":                        {},
	"accounts/fireworks/models/glm-5p3-flash":                  {},
	"accounts/fireworks/models/gpt-oss-120b":                   {},
	"accounts/fireworks/models/inkling":                        {},
	"accounts/fireworks/models/kimi-k2p6":                      {},
	"accounts/fireworks/models/kimi-k2p7-code":                 {},
	"accounts/fireworks/models/minimax-m3":                     {},
	"accounts/fireworks/models/mistral-large-3-fp8":            {},
	"accounts/fireworks/models/muse-glimmer-30b":               {},
	"accounts/fireworks/models/nemotron-3-ultra-nvfp4":         {},
	"accounts/fireworks/models/nemotron-lightning-3p5-30b-a3b": {},
	"accounts/fireworks/models/qwen3p7-plus":                   {},
	"accounts/fireworks/models/qwen3p8-2p4t-a95b":              {},
	"accounts/fireworks/models/qwen3p8-max":                    {},
	"accounts/fireworks/routers/glm-5p3-fast":                  {},
}

func isFireworksAnthropicModel(model string) bool {
	_, ok := fireworksAnthropicModels[model]
	return ok
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
	"minimax":                    "YEN_MINIMAX_API_KEY",
	"minimax-cn":                 "YEN_MINIMAX_CN_API_KEY",
	"moonshotai":                 "YEN_MOONSHOT_API_KEY",
	"moonshotai-cn":              "YEN_MOONSHOT_API_KEY",
	"nvidia":                     "YEN_NVIDIA_API_KEY",
	"openai":                     "YEN_OPENAI_API_KEY",
	"openrouter":                 "YEN_OPENROUTER_API_KEY",
	"opencode":                   "YEN_OPENCODE_API_KEY",
	"opencode-go":                "YEN_OPENCODE_API_KEY",
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
	"vercel-ai-gateway":          "YEN_VERCEL_AI_GATEWAY_API_KEY",
	"cloudflare-workers-ai":      "YEN_CLOUDFLARE_API_KEY",
	"cloudflare-ai-gateway":      "YEN_CLOUDFLARE_API_KEY",
	"google-vertex":              "YEN_GOOGLE_CLOUD_API_KEY",
	"github-copilot":             "YEN_COPILOT_GITHUB_TOKEN",
	"openai-codex":               "YEN_OPENAI_CODEX_ACCESS_TOKEN",
	"radius":                     "YEN_RADIUS_API_KEY",
	"azure-openai-responses":     "YEN_AZURE_OPENAI_API_KEY",
}

func azureConfigured(model string) OpenAIResponses {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("YEN_AZURE_OPENAI_BASE_URL")), "/")
	if baseURL == "" {
		resource := strings.TrimSpace(os.Getenv("YEN_AZURE_OPENAI_RESOURCE_NAME"))
		if resource != "" {
			baseURL = "https://" + resource + ".openai.azure.com/openai/v1"
		}
	}
	if baseURL == "" {
		baseURL = "https://{AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1"
	}
	if !strings.HasSuffix(baseURL, "/openai/v1") && strings.HasSuffix(baseURL, "/openai") {
		baseURL += "/v1"
	}
	key := os.Getenv("YEN_AZURE_OPENAI_API_KEY")
	if key == "" {
		key = storedCredentialKey("azure-openai-responses")
	}
	client := NewOpenAIResponses(baseURL, key, model)
	client.ProviderName = "azure-openai-responses"
	client.APIKeyHeader = "api-key"
	client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
	return client
}

func codexAccountID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil {
		return ""
	}
	claims, _ := payload["https://api.openai.com/auth"].(map[string]any)
	account, _ := claims["chatgpt_account_id"].(string)
	return account
}

func googleVertexConfigured(model string) GoogleGenerativeAI {
	baseURL := os.Getenv("YEN_GOOGLE_VERTEX_BASE_URL")
	if baseURL == "" {
		project := strings.TrimSpace(os.Getenv("YEN_GOOGLE_CLOUD_PROJECT"))
		location := strings.TrimSpace(os.Getenv("YEN_GOOGLE_CLOUD_LOCATION"))
		baseURL = "https://aiplatform.googleapis.com/v1/projects/" + project + "/locations/" + location + "/publishers/google"
	}
	key := os.Getenv("YEN_GOOGLE_CLOUD_API_KEY")
	if key == "" {
		key = storedCredentialKey("google-vertex")
	}
	client := NewGoogleGenerativeAI(baseURL, key, model)
	client.ProviderName = "google-vertex"
	client.BearerToken = os.Getenv("YEN_GOOGLE_VERTEX_ACCESS_TOKEN")
	if client.BearerToken != "" {
		client.APIKey = ""
	} else if client.APIKey == "" {
		path := strings.TrimSpace(os.Getenv("YEN_GOOGLE_APPLICATION_CREDENTIALS"))
		if path == "" {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
			}
		}
		if path != "" {
			client.BearerSource = vertexServiceAccountSource(path)
		}
	}
	client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
	return client
}

func bedrockConfigured(model string) BedrockConverse {
	region := strings.TrimSpace(os.Getenv("AWS_REGION"))
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
	}
	client := NewBedrockConverse(region, model)
	client.Profile = strings.TrimSpace(os.Getenv("AWS_PROFILE"))
	client.BaseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("YEN_AWS_BEDROCK_BASE_URL")), "/")
	client.BearerToken = strings.TrimSpace(os.Getenv("YEN_AWS_BEARER_TOKEN_BEDROCK"))
	client.SkipAuth = os.Getenv("YEN_AWS_BEDROCK_SKIP_AUTH") == "1"
	return client
}

func radiusConfigured(model string) TheosesMessages {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("YEN_RADIUS_BASE_URL")), "/")
	client := NewTheosesMessages(baseURL, os.Getenv("YEN_RADIUS_API_KEY"), model)
	client.GatewayURL = strings.TrimRight(strings.TrimSpace(os.Getenv("YEN_RADIUS_GATEWAY")), "/")
	if client.APIKey == "" {
		client.APIKey = storedCredentialKey("radius")
	}
	return client
}

func codexConfigured(model string) (OpenAIResponses, error) {
	token := os.Getenv("YEN_OPENAI_CODEX_ACCESS_TOKEN")
	if token == "" {
		token = storedCredentialKey("openai-codex")
	}
	if token == "" {
		return OpenAIResponses{}, errors.New("openai codex access token is required")
	}
	accountID := codexAccountID(token)
	if accountID == "" {
		return OpenAIResponses{}, errors.New("openai codex token has no ChatGPT account ID")
	}
	baseURL := os.Getenv("YEN_OPENAI_CODEX_BASE_URL")
	if baseURL == "" {
		baseURL = providerDefaults["openai-codex"]
	}
	client := NewOpenAIResponses(baseURL, token, model)
	client.ProviderName = "openai-codex"
	client.Headers = map[string]string{
		"chatgpt-account-id": accountID,
		"originator":         "theoses",
		"OpenAI-Beta":        "responses=experimental",
		"Accept":             "text/event-stream",
	}
	client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
	return client, nil
}

func nativeResponsesConfigured(providerID, model string) OpenAIResponses {
	baseURL := os.Getenv("YEN_RESPONSES_BASE_URL")
	if providerID == "xai" {
		baseURL = os.Getenv("YEN_XAI_BASE_URL")
	}
	if baseURL == "" {
		baseURL = os.Getenv("YEN_OPENAI_BASE_URL")
	}
	if baseURL == "" {
		baseURL = providerDefaults[providerID]
	}
	key := os.Getenv(providerKeyEnvs[providerID])
	if key == "" {
		key = storedCredentialKey(providerID)
	}
	client := NewOpenAIResponses(baseURL, key, model)
	client.ProviderName = providerID
	client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
	return client
}

func cloudflareConfigured(providerID, model string) OpenAICompletions {
	baseURL := os.Getenv("YEN_CLOUDFLARE_BASE_URL")
	if baseURL == "" {
		baseURL = providerDefaults[providerID]
	}
	baseURL = strings.ReplaceAll(baseURL, "{CLOUDFLARE_ACCOUNT_ID}", os.Getenv("YEN_CLOUDFLARE_ACCOUNT_ID"))
	baseURL = strings.ReplaceAll(baseURL, "{CLOUDFLARE_GATEWAY_ID}", os.Getenv("YEN_CLOUDFLARE_GATEWAY_ID"))
	key := os.Getenv("YEN_CLOUDFLARE_API_KEY")
	client := NewOpenAICompletions(baseURL, key, model)
	client.ProviderName = providerID
	if providerID == "cloudflare-ai-gateway" {
		client.APIKey = ""
		client.Headers = map[string]string{"cf-aig-authorization": "Bearer " + key}
	}
	client.ReasoningEffort = os.Getenv("YEN_REASONING_EFFORT")
	return client
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
	if providerID == "cloudflare-workers-ai" || providerID == "cloudflare-ai-gateway" {
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "@cf/meta/llama-3.3-70b-instruct-fp8-fast"
		}
		return cloudflareConfigured(providerID, model)
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
	if key == "" {
		key = storedCredentialKey(providerID)
	}
	client := NewOpenAICompletions(baseURL, key, model)
	client.ProviderName = providerID
	client.ReasoningEffort = os.Getenv("YEN_REASONING_EFFORT")
	return client
}

func ConfiguredFromEnv() agent.Provider {
	providerID := strings.ToLower(strings.TrimSpace(os.Getenv("YEN_PROVIDER")))
	if providerID == "radius" {
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "auto"
		}
		return radiusConfigured(model)
	}
	if providerID == "amazon-bedrock" {
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "us.anthropic.claude-opus-4-6-v1"
		}
		return bedrockConfigured(model)
	}
	if providerID == "azure-openai-responses" {
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "gpt-4.1"
		}
		return azureConfigured(model)
	}
	if providerID == "openai-codex" {
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "gpt-5"
		}
		client, err := codexConfigured(model)
		if err != nil {
			return NewFromEnv()
		}
		return client
	}
	if providerID == "openai" || providerID == "xai" {
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "gpt-5.5"
			if providerID == "xai" {
				model = "grok-4.6"
			}
		}
		return nativeResponsesConfigured(providerID, model)
	}
	if providerID == "openai-responses" || providerID == "azure-openai-responses" {
		baseURL := os.Getenv("YEN_RESPONSES_BASE_URL")
		if baseURL == "" {
			baseURL = os.Getenv("YEN_OPENAI_BASE_URL")
		}
		if providerID == "azure-openai-responses" && baseURL == "" {
			baseURL = os.Getenv("YEN_AZURE_OPENAI_BASE_URL")
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "gpt-4.1"
		}
		key := os.Getenv("YEN_OPENAI_API_KEY")
		if providerID == "azure-openai-responses" {
			key = os.Getenv("YEN_AZURE_OPENAI_API_KEY")
		}
		if key == "" {
			key = storedCredentialKey(providerID)
		}
		client := NewOpenAIResponses(baseURL, key, model)
		client.ProviderName = providerID
		client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
		return client
	}
	if providerID == "google" {
		baseURL := os.Getenv("YEN_GOOGLE_BASE_URL")
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com/v1beta"
		}
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "gemini-2.5-flash"
		}
		key := os.Getenv("YEN_GOOGLE_API_KEY")
		if key == "" {
			key = storedCredentialKey("google")
		}
		client := NewGoogleGenerativeAI(baseURL, key, model)
		client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
		return client
	}
	if providerID == "google-vertex" {
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "gemini-2.5-flash"
		}
		return googleVertexConfigured(model)
	}
	if providerID == "github-copilot" {
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "gpt-4o"
		}
		baseURL := os.Getenv("YEN_COPILOT_BASE_URL")
		if baseURL == "" {
			baseURL = providerDefaults[providerID]
		}
		key := os.Getenv("YEN_COPILOT_GITHUB_TOKEN")
		if key == "" {
			key = storedCredentialKey(providerID)
		}
		client := NewOpenAICompletions(baseURL, key, model)
		client.ProviderName = providerID
		client.ReasoningEffort = os.Getenv("YEN_REASONING_EFFORT")
		return client
	}
	if providerID == "minimax" || providerID == "minimax-cn" || providerID == "vercel-ai-gateway" {
		baseURL := providerDefaults[providerID]
		model := os.Getenv("YEN_MODEL")
		if model == "" {
			model = "MiniMax-M2.5"
			if providerID == "vercel-ai-gateway" {
				model = "claude-sonnet-4"
			}
		}
		key := os.Getenv(providerKeyEnvs[providerID])
		if key == "" {
			key = storedCredentialKey(providerID)
		}
		client := NewAnthropicMessages(baseURL, key, model)
		client.ProviderName = providerID
		client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
		return client
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
	if key == "" {
		key = storedCredentialKey("anthropic")
	}
	client := NewAnthropicMessages(baseURL, key, model)
	client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
	return client
}

func NewConfigured(providerID, model string) (agent.Provider, error) {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	model = strings.TrimSpace(model)
	if providerID == "radius" {
		if model == "" {
			model = "auto"
		}
		client := radiusConfigured(model)
		if client.BaseURL == "" {
			return nil, errors.New("radius requires YEN_RADIUS_BASE_URL")
		}
		return client, nil
	}
	if providerID == "amazon-bedrock" {
		if model == "" {
			model = "us.anthropic.claude-opus-4-6-v1"
		}
		return bedrockConfigured(model), nil
	}
	if providerID == "azure-openai-responses" {
		if model == "" {
			model = "gpt-4.1"
		}
		return azureConfigured(model), nil
	}
	if providerID == "openai-codex" {
		if model == "" {
			model = "gpt-5"
		}
		return codexConfigured(model)
	}
	if providerID == "openai" || providerID == "xai" {
		if model == "" {
			model = "gpt-5.5"
			if providerID == "xai" {
				model = "grok-4.6"
			}
		}
		return nativeResponsesConfigured(providerID, model), nil
	}
	if providerID == "cloudflare-workers-ai" || providerID == "cloudflare-ai-gateway" {
		if model == "" {
			model = "@cf/meta/llama-3.3-70b-instruct-fp8-fast"
		}
		return cloudflareConfigured(providerID, model), nil
	}
	if providerID == "google-vertex" {
		if model == "" {
			model = "gemini-2.5-flash"
		}
		if os.Getenv("YEN_GOOGLE_VERTEX_BASE_URL") == "" && (strings.TrimSpace(os.Getenv("YEN_GOOGLE_CLOUD_PROJECT")) == "" || strings.TrimSpace(os.Getenv("YEN_GOOGLE_CLOUD_LOCATION")) == "") {
			return nil, errors.New("google vertex requires YEN_GOOGLE_CLOUD_PROJECT and YEN_GOOGLE_CLOUD_LOCATION")
		}
		return googleVertexConfigured(model), nil
	}
	if providerID == "github-copilot" {
		if model == "" {
			model = "gpt-4o"
		}
		baseURL := os.Getenv("YEN_COPILOT_BASE_URL")
		if baseURL == "" {
			baseURL = providerDefaults[providerID]
		}
		key := os.Getenv("YEN_COPILOT_GITHUB_TOKEN")
		if key == "" {
			key = storedCredentialKey(providerID)
		}
		client := NewOpenAICompletions(baseURL, key, model)
		client.ProviderName = providerID
		client.ReasoningEffort = os.Getenv("YEN_REASONING_EFFORT")
		return client, nil
	}
	if providerID == "openai-responses" || providerID == "azure-openai-responses" {
		if model == "" {
			model = "gpt-4.1"
		}
		baseURL := os.Getenv("YEN_RESPONSES_BASE_URL")
		if baseURL == "" {
			baseURL = os.Getenv("YEN_OPENAI_BASE_URL")
		}
		key := os.Getenv("YEN_OPENAI_API_KEY")
		if providerID == "azure-openai-responses" {
			if baseURL == "" {
				baseURL = os.Getenv("YEN_AZURE_OPENAI_BASE_URL")
			}
			key = os.Getenv("YEN_AZURE_OPENAI_API_KEY")
		}
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		client := NewOpenAIResponses(baseURL, key, model)
		client.ProviderName = providerID
		client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
		return client, nil
	}
	if providerID == "google" {
		if model == "" {
			model = "gemini-2.5-flash"
		}
		baseURL := os.Getenv("YEN_GOOGLE_BASE_URL")
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com/v1beta"
		}
		key := os.Getenv("YEN_GOOGLE_API_KEY")
		if key == "" {
			key = storedCredentialKey("google")
		}
		client := NewGoogleGenerativeAI(baseURL, key, model)
		client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
		return client, nil
	}
	if providerID == "minimax" || providerID == "minimax-cn" || providerID == "vercel-ai-gateway" {
		if model == "" {
			model = "MiniMax-M2.5"
			if providerID == "vercel-ai-gateway" {
				model = "claude-sonnet-4"
			}
		}
		key := os.Getenv(providerKeyEnvs[providerID])
		if key == "" {
			key = storedCredentialKey(providerID)
		}
		client := NewAnthropicMessages(providerDefaults[providerID], key, model)
		client.ProviderName = providerID
		client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
		return client, nil
	}
	if providerID == "kimi-coding" {
		if model == "" {
			model = "kimi-for-coding"
		}
		key := os.Getenv(providerKeyEnvs[providerID])
		if key == "" {
			key = storedCredentialKey(providerID)
		}
		client := NewAnthropicMessages(providerDefaults[providerID], key, model)
		client.ProviderName = providerID
		client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
		return client, nil
	}
	if providerID == "fireworks" && isFireworksAnthropicModel(model) {
		key := os.Getenv(providerKeyEnvs[providerID])
		if key == "" {
			key = storedCredentialKey(providerID)
		}
		client := NewAnthropicMessages(strings.TrimSuffix(providerDefaults[providerID], "/v1"), key, model)
		client.ProviderName = providerID
		client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
		return client, nil
	}
	if providerID == "anthropic" {
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		baseURL := os.Getenv("YEN_ANTHROPIC_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.anthropic.com/v1"
		}
		key := os.Getenv("YEN_ANTHROPIC_API_KEY")
		if key == "" {
			key = storedCredentialKey("anthropic")
		}
		client := NewAnthropicMessages(baseURL, key, model)
		client.ThinkingLevel = os.Getenv("YEN_REASONING_EFFORT")
		return client, nil
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
	if key == "" {
		key = storedCredentialKey(providerID)
	}
	client := NewOpenAICompletions(baseURL, key, model)
	client.ProviderName = providerID
	client.ReasoningEffort = os.Getenv("YEN_REASONING_EFFORT")
	return client, nil
}

func storedCredentialKey(providerID string) string {
	path := strings.TrimSpace(os.Getenv("YEN_AUTH_FILE"))
	if path == "" {
		return ""
	}
	credential, ok, err := auth.Open(path).Read(providerID)
	if err != nil || !ok {
		return ""
	}
	if credential.Type == "oauth" {
		return credential.Access
	}
	return credential.Key
}

func Describe(p agent.Provider) (string, string) {
	switch client := p.(type) {
	case OpenAICompletions:
		return client.ProviderName, client.Model
	case AnthropicMessages:
		name := client.ProviderName
		if name == "" {
			name = "anthropic"
		}
		return name, client.Model
	case GoogleGenerativeAI:
		name := client.ProviderName
		if name == "" {
			name = "google"
		}
		return name, client.Model
	case OpenAIResponses:
		return client.name(), client.Model
	case BedrockConverse:
		name := client.ProviderName
		if name == "" {
			name = "amazon-bedrock"
		}
		return name, client.Model
	case TheosesMessages:
		return client.providerName(), client.Model
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
	case OpenAIResponses:
		client.Model = model
		return client, nil
	case BedrockConverse:
		client.Model = model
		return client, nil
	case TheosesMessages:
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
	case OpenAIResponses:
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
	case OpenAIResponses:
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
	case OpenAIResponses:
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

func SetRetryMax(p agent.Provider, retries int) (agent.Provider, error) {
	if retries < 0 {
		return nil, errors.New("retry count cannot be negative")
	}
	switch client := p.(type) {
	case OpenAICompletions:
		client.MaxRetries = retries
		return client, nil
	case AnthropicMessages:
		client.MaxRetries = retries
		return client, nil
	case GoogleGenerativeAI:
		client.MaxRetries = retries
		return client, nil
	case OpenAIResponses:
		client.MaxRetries = retries
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
	case OpenAIResponses:
		return client.MaxRetries > 0
	default:
		return false
	}
}
