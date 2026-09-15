package provider

import (
	"regexp"
	"strings"

	"github.com/H4fizWasabie/yen/internal/agent"
)

var (
	nonRetryableProviderLimit = regexp.MustCompile(`(?i)GoUsageLimitError|FreeUsageLimitError|Monthly usage limit reached|available balance|insufficient_quota|out of budget|quota exceeded|billing`)
	retryableProviderError    = regexp.MustCompile(`(?i)overloaded|rate.?limit|too many requests|\b429\b|\b5(?:00|02|03|04|24)\b|service.?unavailable|server.?error|internal.?error|provider.?returned.?error|network.?error|connection.?error|connection.?refused|connection.?lost|other side closed|fetch failed|getaddrinfo|ENOTFOUND|EAI_AGAIN|upstream.?connect|reset before headers|socket hang up|socket connection was closed|timed? out|timeout|terminated|websocket.?closed|websocket.?error|ended without|stream ended before|http2 request did not get a response|retry delay|you can retry your request|try your request again|please retry your request|ResourceExhausted`)
)

// IsRetryableAssistantError classifies transient assistant failures. Context
// overflow is deliberately excluded because runtime handles it by compaction.
func IsRetryableAssistantError(message agent.Message) bool {
	return message.StopReason == "error" && IsRetryableProviderError(message.ErrorMessage)
}

func IsRetryableProviderError(message string) bool {
	if strings.TrimSpace(message) == "" || nonRetryableProviderLimit.MatchString(message) {
		return false
	}
	if IsContextOverflowError(message) {
		return false
	}
	return retryableProviderError.MatchString(message)
}
