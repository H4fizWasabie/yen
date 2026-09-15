package provider

import (
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestIsRetryableProviderErrorMatchesTransientFailures(t *testing.T) {
	for _, message := range []string{"overloaded_error", "Provider finish_reason: network_error", "502 upstream connect error", "socket hang up"} {
		if !IsRetryableProviderError(message) {
			t.Fatalf("message %q was not retryable", message)
		}
	}
}

func TestIsRetryableAssistantErrorExcludesOverflowAndQuota(t *testing.T) {
	for _, message := range []string{"prompt too long; exceeded max context length", "insufficient_quota", "billing limit reached"} {
		if IsRetryableAssistantError(agent.Message{StopReason: "error", ErrorMessage: message}) {
			t.Fatalf("message %q was incorrectly retryable", message)
		}
	}
	if !IsRetryableAssistantError(agent.Message{StopReason: "error", ErrorMessage: "service unavailable"}) {
		t.Fatal("transient assistant error was not retryable")
	}
}
