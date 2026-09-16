package provider

import (
	"context"
	"os"
	"strings"
)

type sessionIDContextKey struct{}

func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDContextKey{}, strings.TrimSpace(id))
}

func SessionID(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDContextKey{}).(string)
	return id
}

func promptCacheSettings(ctx context.Context) (string, string) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("YEN_CACHE_RETENTION")), "none") {
		return "", ""
	}
	id := SessionID(ctx)
	if id == "" {
		return "", ""
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("YEN_CACHE_RETENTION")), "long") {
		return clampPromptCacheKey(id), "24h"
	}
	return clampPromptCacheKey(id), ""
}

func clampPromptCacheKey(key string) string {
	runes := []rune(key)
	if len(runes) > 64 {
		runes = runes[:64]
	}
	return string(runes)
}

func supportsPromptCaching(providerName, baseURL string) bool {
	return providerName == "openrouter" || providerName == "openai" ||
		providerName == "openai-responses" || providerName == "azure-openai-responses" ||
		strings.Contains(baseURL, "api.openai.com")
}
