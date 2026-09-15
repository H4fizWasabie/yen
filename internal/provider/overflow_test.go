package provider

import "testing"

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
