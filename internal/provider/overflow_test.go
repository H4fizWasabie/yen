package provider

import "testing"

func TestIsContextOverflowErrorMatchesProviderPatterns(t *testing.T) {
	for _, message := range []string{
		"400 prompt too long; exceeded max context length",
		"Input length (265330) exceeds model's maximum context length (262144)",
		"This endpoint's maximum context length is 131072 tokens",
		"Range of input length should be [1, 32768]",
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
