package provider

import (
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
)

func TestIsContextOverflowErrorMatchesProviderPatterns(t *testing.T) {
	for _, message := range []string{
		"400 prompt too long; exceeded max context length",
		"Input length (265330) exceeds model's maximum context length (262144)",
		"This endpoint's maximum context length is 131072 tokens",
		"Range of input length should be [1, 32768]",
		"The input is too long for requested model",
		"The input token count 1196265 exceeds the maximum number of tokens allowed",
		"This model's maximum prompt length is 131072",
		"Please reduce the length of the messages",
		"prompt token count of 10 exceeds the limit of 8",
		"the request exceeds the available context size",
		"tokens to keep from the initial prompt is greater than the context length",
		"invalid params, context window exceeds limit",
		"Your request exceeded model token limit: 10",
		"Prompt contains 10 tokens and is too large for model with 8 maximum context length",
		"Prompt has 10 tokens, but the configured context size is 8 tokens",
		"model_context_window_exceeded",
		"context_length_exceeded",
	} {
		if !IsContextOverflowError(message) {
			t.Fatalf("did not match overflow message %q", message)
		}
	}
}

func TestIsContextOverflowErrorRejectsTransientErrors(t *testing.T) {
	for _, message := range []string{
		"429 too many requests",
		"rate limit exceeded",
		"502 service unavailable",
	} {
		if IsContextOverflowError(message) {
			t.Fatalf("incorrectly matched transient message %q", message)
		}
	}
}

func TestIsContextOverflowResponseDetectsSilentProviderOverflow(t *testing.T) {
	tests := []struct {
		name    string
		message agent.Message
		window  int
		want    bool
	}{
		{
			name:    "successful input over context",
			message: agent.Message{StopReason: "stop", Usage: &agent.Usage{Input: 190, CacheRead: 11}},
			window:  200,
			want:    true,
		},
		{
			name:    "length stop fills context with no output",
			message: agent.Message{StopReason: "length", Usage: &agent.Usage{Input: 198, Output: 0}},
			window:  200,
			want:    true,
		},
		{
			name:    "length stop leaves output room",
			message: agent.Message{StopReason: "length", Usage: &agent.Usage{Input: 150, Output: 0}},
			window:  200,
			want:    false,
		},
		{
			name:    "disabled without context window",
			message: agent.Message{StopReason: "stop", Usage: &agent.Usage{Input: 1000}},
			window:  0,
			want:    false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsContextOverflowMessage(test.message, test.window); got != test.want {
				t.Fatalf("overflow=%v, want %v", got, test.want)
			}
		})
	}
}
